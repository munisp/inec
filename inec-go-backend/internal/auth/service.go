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

// Login authenticates a user and returns a token pair.
func (s *Service) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password are required")
	}

	var user User
	var passwordHash string
	var isActive int

	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, full_name, role, COALESCE(staff_id,''), COALESCE(state_code,''), COALESCE(is_active, 1)
		 FROM users WHERE LOWER(username) = $1`, username).
		Scan(&user.ID, &user.Username, &passwordHash, &user.FullName, &user.Role, &user.StaffID, &user.State, &isActive)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid credentials")
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	if isActive != 1 {
		return nil, fmt.Errorf("account is disabled")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Update login count
	_, _ = s.db.ExecContext(ctx, `UPDATE users SET login_count = COALESCE(login_count,0) + 1 WHERE id = $1`, user.ID)

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
