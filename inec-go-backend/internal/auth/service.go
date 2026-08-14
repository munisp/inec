// Package auth provides authentication, session management, and token lifecycle.
// This is a bounded context that can be extracted to its own microservice.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"
)

// Config holds authentication service configuration.
type Config struct {
	JWTSecret          []byte
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	BcryptCost         int
	MaxLoginAttempts   int
	LockoutDuration    time.Duration
	TokenIssuer        string
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
func (s *Service) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.config.JWTSecret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
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

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString(s.config.JWTSecret)
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

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString(s.config.JWTSecret)
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

// Revoke blacklists a token JTI.
func (s *Service) Revoke(ctx context.Context, tokenStr string) {
	claims, err := s.ValidateToken(tokenStr)
	if err != nil {
		return
	}
	s.db.ExecContext(ctx, `INSERT INTO token_blacklist (jti, expires_at) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.JTI, claims.ExpiresAt.Time)
}
