// Package auth provides authentication, session management, and token lifecycle.
// This is a bounded context that can be extracted to its own microservice.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"
)

// Config holds authentication service configuration.
type Config struct {
	JWTSecret []byte
	// JWTSecretPrevious (R5-047): the outgoing key during a rotation window —
	// accepted for VERIFICATION only, never for signing. Sourced from
	// JWT_SECRET_PREVIOUS by the service entrypoint.
	JWTSecretPrevious []byte
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	BcryptCost        int
	MaxLoginAttempts  int
	LockoutDuration   time.Duration
	TokenIssuer       string
}

// jwtKID is a public fingerprint of a signing key (truncated SHA-256) —
// safe to emit in token headers, identifies the key without revealing it.
func jwtKID(key []byte) string {
	h := sha256.Sum256(key)
	return hex.EncodeToString(h[:])[:16]
}

func (c Config) currentKID() string { return jwtKID(c.JWTSecret) }

// verificationKey resolves the HMAC key for a token from its kid header
// (R5-047). Legacy kid-less tokens verify against the current key; an
// unknown kid fails closed.
func (c Config) verificationKey(t *jwt.Token) ([]byte, error) {
	kid, _ := t.Header["kid"].(string)
	switch {
	case kid == "" || kid == c.currentKID():
		return c.JWTSecret, nil
	case len(c.JWTSecretPrevious) > 0 && kid == jwtKID(c.JWTSecretPrevious):
		return c.JWTSecretPrevious, nil
	default:
		return nil, fmt.Errorf("unknown JWT key id — token rejected")
	}
}

// sign stamps the current kid and signs with the current key.
func (c Config) sign(claims jwt.Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = c.currentKID()
	return token.SignedString(c.JWTSecret)
}

// DefaultConfig returns production-safe defaults.
func DefaultConfig(secret []byte) Config {
	return Config{
		JWTSecret:        secret,
		AccessTokenTTL:   1 * time.Hour,
		RefreshTokenTTL:  7 * 24 * time.Hour,
		BcryptCost:       12,
		MaxLoginAttempts: 5,
		LockoutDuration:  15 * time.Minute,
		TokenIssuer:      "inec-platform",
	}
}

// User represents an authenticated user.
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	StaffID  string `json:"staff_id,omitempty"`
	State    string `json:"state_code,omitempty"`
}

// TokenPair contains access and refresh tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	User         *User  `json:"user"`
}

// Claims extends JWT standard claims with INEC-specific fields.
type Claims struct {
	jwt.RegisteredClaims
	Username string `json:"username"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	Type     string `json:"type"` // "access" or "refresh"
	JTI      string `json:"jti"`
}

// Service provides authentication operations.
type Service struct {
	db     *sql.DB
	config Config
}

// NewService creates a new auth service with the given database and config.
func NewService(db *sql.DB, cfg Config) *Service {
	return &Service{db: db, config: cfg}
}

// InitTables ensures the account-lockout columns backing MaxLoginAttempts /
// LockoutDuration exist (they are also added by migration 000029). Idempotent.
func (s *Service) InitTables(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		ALTER TABLE users ADD COLUMN IF NOT EXISTS failed_login_attempts INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
	`)
	return err
}

// Login authenticates a user and returns a token pair.
//
// Brute-force defense (R4-08): MaxLoginAttempts and LockoutDuration were
// configured but never enforced. Now each password failure increments
// users.failed_login_attempts; reaching the threshold locks the account for
// LockoutDuration (users.locked_until); a successful login clears both.
// Enforcement fails CLOSED: if the lockout state cannot be read or written,
// the login is rejected rather than silently skipping the control.
func (s *Service) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password are required")
	}

	var user User
	var passwordHash string
	var isActive int
	var failedAttempts int
	var lockedUntil sql.NullTime

	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, full_name, role, COALESCE(staff_id,''), COALESCE(state_code,''), COALESCE(is_active, 1),
		        COALESCE(failed_login_attempts, 0), locked_until
		 FROM users WHERE LOWER(username) = $1`, username).
		Scan(&user.ID, &user.Username, &passwordHash, &user.FullName, &user.Role, &user.StaffID, &user.State, &isActive,
			&failedAttempts, &lockedUntil)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid credentials")
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	if isActive != 1 {
		return nil, fmt.Errorf("account is disabled")
	}
	if lockedUntil.Valid && time.Now().Before(lockedUntil.Time) {
		return nil, fmt.Errorf("account is temporarily locked until %s after repeated failed login attempts",
			lockedUntil.Time.UTC().Format(time.RFC3339))
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		lockAfter := s.config.MaxLoginAttempts
		if lockAfter <= 0 {
			lockAfter = 5
		}
		lockSecs := int(s.config.LockoutDuration / time.Second)
		if lockSecs <= 0 {
			lockSecs = 900
		}
		if _, dbErr := s.db.ExecContext(ctx,
			`UPDATE users SET
			   failed_login_attempts = CASE WHEN COALESCE(failed_login_attempts,0) + 1 >= $2 THEN 0 ELSE COALESCE(failed_login_attempts,0) + 1 END,
			   locked_until = CASE WHEN COALESCE(failed_login_attempts,0) + 1 >= $2 THEN NOW() + make_interval(secs => $3) ELSE locked_until END
			 WHERE id = $1`, user.ID, lockAfter, lockSecs); dbErr != nil {
			// Fail closed: an unenforceable lockout must not become a silent
			// bypass of the brute-force control.
			log.Error().Err(dbErr).Int("user_id", user.ID).Msg("failed to record login failure / lockout state")
			return nil, fmt.Errorf("authentication service error")
		}
		if failedAttempts+1 >= lockAfter {
			return nil, fmt.Errorf("account locked for %s after %d failed login attempts", s.config.LockoutDuration, lockAfter)
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	// Success: clear lockout state and update login count.
	if _, dbErr := s.db.ExecContext(ctx,
		`UPDATE users SET login_count = COALESCE(login_count,0) + 1, failed_login_attempts = 0, locked_until = NULL WHERE id = $1`,
		user.ID); dbErr != nil {
		log.Error().Err(dbErr).Int("user_id", user.ID).Msg("failed to clear lockout state on successful login")
		return nil, fmt.Errorf("authentication service error")
	}

	pair, err := s.issueTokenPair(&user)
	if err != nil {
		return nil, fmt.Errorf("token generation failed: %w", err)
	}

	return pair, nil
}

// ValidateToken validates a JWT and returns its claims.
//
// R5-041: the revocation blacklist is now actually CHECKED here — previously
// nothing in internal/auth read token_blacklist, so revoked tokens stayed
// valid until expiry. Fail closed: a blacklist lookup error rejects the
// token rather than silently skipping the revocation control.
func (s *Service) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.config.verificationKey(t)
	})
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	if claims.JTI != "" && s.db != nil {
		var revoked int
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM token_blacklist WHERE jti=$1 AND expires_at > NOW()`, claims.JTI,
		).Scan(&revoked); err != nil {
			log.Error().Err(err).Msg("SECURITY: token blacklist check failed (fail closed)")
			return nil, fmt.Errorf("token validation unavailable")
		}
		if revoked > 0 {
			return nil, fmt.Errorf("token has been revoked")
		}
	}
	return claims, nil
}

// RefreshToken issues a new token pair from a valid refresh token.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := s.ValidateToken(refreshToken)
	if err != nil {
		return nil, err
	}
	if claims.Type != "refresh" {
		return nil, fmt.Errorf("not a refresh token")
	}

	var user User
	err = s.db.QueryRowContext(ctx,
		`SELECT id, username, full_name, role, COALESCE(staff_id,''), COALESCE(state_code,'')
		 FROM users WHERE id = $1 AND COALESCE(is_active,1) = 1`,
		claims.Subject).
		Scan(&user.ID, &user.Username, &user.FullName, &user.Role, &user.StaffID, &user.State)
	if err != nil {
		return nil, fmt.Errorf("user not found or disabled")
	}

	// R5-038/041 rotation: the presented refresh token is single-use —
	// blacklist it as part of the exchange so a stolen predecessor dies on
	// first replay. ValidateToken (above) already rejects blacklisted jtis.
	if claims.JTI != "" && claims.ExpiresAt != nil {
		userID, _ := strconv.Atoi(claims.Subject)
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO token_blacklist (jti, user_id, expires_at, reason) VALUES ($1, $2, $3, 'refresh_rotation')
			 ON CONFLICT (jti) DO NOTHING`,
			claims.JTI, userID, claims.ExpiresAt.Time); err != nil {
			// Fail closed: without rotation persistence a stolen refresh
			// token is reusable — refuse to issue rather than skip.
			log.Error().Err(err).Msg("SECURITY: refresh rotation persist failed (fail closed)")
			return nil, fmt.Errorf("refresh failed: rotation unavailable")
		}
	}

	return s.issueTokenPair(&user)
}

// HashPassword creates a bcrypt hash of the given password.
func (s *Service) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.config.BcryptCost)
	if err != nil {
		return "", fmt.Errorf("password hashing failed: %w", err)
	}
	return string(hash), nil
}

// issueTokenPair generates signed access and refresh tokens.
func (s *Service) issueTokenPair(user *User) (*TokenPair, error) {
	now := time.Now()
	jti := generateJTI()

	accessClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", user.ID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.AccessTokenTTL)),
			Issuer:    s.config.TokenIssuer,
			ID:        jti,
		},
		Username: user.Username,
		FullName: user.FullName,
		Role:     user.Role,
		Type:     "access",
		JTI:      jti,
	}

	accessStr, err := s.config.sign(accessClaims)
	if err != nil {
		return nil, err
	}

	refreshJTI := generateJTI()
	refreshClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", user.ID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.RefreshTokenTTL)),
			Issuer:    s.config.TokenIssuer,
			ID:        refreshJTI,
		},
		Username: user.Username,
		FullName: user.FullName,
		Role:     user.Role,
		Type:     "refresh",
		JTI:      refreshJTI,
	}

	refreshStr, err := s.config.sign(refreshClaims)
	if err != nil {
		return nil, err
	}

	log.Info().Str("user", user.Username).Str("jti", jti).Msg("Token pair issued")

	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		TokenType:    "bearer",
		ExpiresIn:    int64(s.config.AccessTokenTTL.Seconds()),
		User:         user,
	}, nil
}

func generateJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Authenticate verifies credentials and returns the user (without issuing tokens).
func (s *Service) Authenticate(ctx context.Context, username, password string) (*User, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	var user User
	var passwordHash string
	var isActive int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, full_name, role, COALESCE(staff_id,''), COALESCE(state_code,''), COALESCE(is_active, 1)
		 FROM users WHERE LOWER(username) = $1`, username).
		Scan(&user.ID, &user.Username, &passwordHash, &user.FullName, &user.Role, &user.StaffID, &user.State, &isActive)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	if isActive != 1 {
		return nil, fmt.Errorf("account is disabled")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	return &user, nil
}

// IssueTokens generates access and refresh tokens for a user.
func (s *Service) IssueTokens(ctx context.Context, user *User) (string, string, error) {
	pair, err := s.issueTokenPair(user)
	if err != nil {
		return "", "", err
	}
	return pair.AccessToken, pair.RefreshToken, nil
}

// Register creates a new user account.
func (s *Service) Register(ctx context.Context, username, password, fullName, role string) (*User, error) {
	hash, err := s.HashPassword(password)
	if err != nil {
		return nil, err
	}
	if role == "" {
		role = "observer"
	}
	var id int
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, password_hash, full_name, role, is_active) VALUES ($1, $2, $3, $4, 1) RETURNING id`,
		strings.TrimSpace(strings.ToLower(username)), hash, fullName, role).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("registration failed: %w", err)
	}
	return &User{ID: id, Username: username, FullName: fullName, Role: role}, nil
}

// Revoke blacklists a token JTI. R5-041: the previous INSERT omitted
// user_id against a NOT NULL column (every revocation silently failed) and
// the error was discarded. user_id is now taken from the token subject,
// the reason is recorded, and failures are returned + logged loudly.
func (s *Service) Revoke(ctx context.Context, tokenStr string) error {
	// Parse WITHOUT the blacklist check: revoking an already-revoked token
	// must stay idempotent, not error.
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.config.verificationKey(t)
	})
	if err != nil {
		return fmt.Errorf("revoke: token parse failed: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return fmt.Errorf("revoke: invalid token claims")
	}
	if claims.JTI == "" || claims.ExpiresAt == nil {
		return fmt.Errorf("revoke: token lacks jti/expiry")
	}
	userID, _ := strconv.Atoi(claims.Subject)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO token_blacklist (jti, user_id, expires_at, reason) VALUES ($1, $2, $3, 'logout')
		 ON CONFLICT (jti) DO UPDATE SET revoked_at=CURRENT_TIMESTAMP, reason='logout'`,
		claims.JTI, userID, claims.ExpiresAt.Time); err != nil {
		log.Error().Err(err).Str("jti", claims.JTI).Msg("SECURITY: token revocation persist failed")
		return fmt.Errorf("revoke: persist failed: %w", err)
	}
	return nil
}

// RevokeAllForUser blacklists every outstanding session JTI recorded for a
// user ("revoke all sessions"). R5-041: this path previously did nothing at
// all — there was no writer to token_blacklist that ValidateToken would
// honor. Returns the number of sessions revoked.
func (s *Service) RevokeAllForUser(ctx context.Context, userID int) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO token_blacklist (jti, user_id, expires_at, reason)
		 SELECT jti, $1, expires_at, 'bulk_revocation' FROM active_sessions
		 WHERE user_id=$1 AND expires_at > NOW()
		 ON CONFLICT (jti) DO UPDATE SET revoked_at=CURRENT_TIMESTAMP, reason='bulk_revocation'`,
		userID)
	if err != nil {
		log.Error().Err(err).Int("user_id", userID).Msg("SECURITY: revoke-all persist failed")
		return 0, fmt.Errorf("revoke all: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
