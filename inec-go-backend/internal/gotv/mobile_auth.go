// Package gotv — Standalone mobile auth for GOTV canvasser/volunteer app.
// Separate from INEC portal auth (Keycloak/JWT). Uses phone+OTP,
// party-scoped user registration, and self-issued JWT tokens.
package gotv

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// MobileAuth handles standalone GOTV mobile authentication.
type MobileAuth struct {
	db        *sql.DB
	svc       *Service
	jwtSecret []byte // HMAC-SHA256 key for self-issued JWT
}

// NewMobileAuth creates a mobile auth handler.
func NewMobileAuth(db *sql.DB, svc *Service, jwtSecretHex string) *MobileAuth {
	secret, _ := hex.DecodeString(jwtSecretHex)
	if len(secret) < 32 {
		secret = make([]byte, 32)
		rand.Read(secret)
		log.Warn().Msg("GOTV Mobile Auth: using random JWT secret (set GOTV_MOBILE_JWT_SECRET for production)")
	}
	return &MobileAuth{db: db, svc: svc, jwtSecret: secret}
}

// ─── OTP Request ─────────────────────────────────────────────────────────

type otpRequest struct {
	Phone     string `json:"phone"`
	PartyCode string `json:"party_code"`
	Name      string `json:"name"`
}

type otpResponse struct {
	SessionID string `json:"session_id"`
	ExpiresIn int    `json:"expires_in"`
	Message   string `json:"message"`
}

// HandleRequestOTP initiates phone+OTP authentication.
// POST /gotv/mobile/auth/request-otp
func (ma *MobileAuth) HandleRequestOTP(w http.ResponseWriter, r *http.Request) {
	var req otpRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		mobileJSONErr(w, "invalid request body", http.StatusBadRequest)
		return
	}

	phone := NormalizePhone(req.Phone)
	if len(phone) < 10 || len(phone) > 15 {
		mobileJSONErr(w, "invalid phone number", http.StatusBadRequest)
		return
	}
	if req.PartyCode == "" {
		mobileJSONErr(w, "party_code is required", http.StatusBadRequest)
		return
	}

	// Look up party by code
	var partyID int
	err := ma.db.QueryRow("SELECT id FROM parties WHERE LOWER(code)=LOWER($1)", req.PartyCode).Scan(&partyID)
	if err != nil {
		mobileJSONErr(w, "party not found", http.StatusNotFound)
		return
	}

	phoneHash := ma.svc.PhoneHash(phone)
	phoneEnc, err := ma.svc.Encrypt(phone)
	if err != nil {
		mobileJSONErr(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Generate 6-digit OTP
	otp, err := generateOTP(6)
	if err != nil {
		mobileJSONErr(w, "internal error", http.StatusInternalServerError)
		return
	}
	otpHash := hashOTP(otp, ma.jwtSecret)
	expiresAt := time.Now().Add(10 * time.Minute)

	// Rate limit: max 3 OTP requests per phone per hour
	var recentOTPs int
	ma.db.QueryRow(
		"SELECT COUNT(*) FROM gotv_mobile_users WHERE party_id=$1 AND phone_hash=$2 AND otp_expires_at > NOW() - INTERVAL '1 hour'",
		partyID, phoneHash,
	).Scan(&recentOTPs)
	if recentOTPs >= 5 {
		mobileJSONErr(w, "too many OTP requests, try again later", http.StatusTooManyRequests)
		return
	}

	// Upsert user record
	sessionID := "msess-" + randHex(16)
	displayName := req.Name
	if displayName == "" {
		displayName = "GOTV User"
	}

	_, err = ma.db.Exec(`
		INSERT INTO gotv_mobile_users (user_id, party_id, phone_hash, phone_encrypted, display_name, otp_code_hash, otp_expires_at, otp_attempts, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, NOW())
		ON CONFLICT (party_id, phone_hash)
		DO UPDATE SET otp_code_hash=$6, otp_expires_at=$7, otp_attempts=0, updated_at=NOW()`,
		sessionID, partyID, phoneHash, phoneEnc, displayName, otpHash, expiresAt,
	)
	if err != nil {
		log.Error().Err(err).Msg("GOTV mobile: failed to upsert user for OTP")
		mobileJSONErr(w, "internal error", http.StatusInternalServerError)
		return
	}

	// In production, send OTP via SMS (AfricasTalking, etc.)
	// For dev/demo, log it
	log.Info().Str("phone", phone[:4]+"****").Str("otp", otp).Int("party", partyID).Msg("GOTV Mobile OTP generated (dev mode — would send via SMS in production)")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(otpResponse{
		SessionID: sessionID,
		ExpiresIn: 600,
		Message:   "OTP sent to your phone number",
	})
}

// ─── OTP Verification ────────────────────────────────────────────────────

type verifyOTPRequest struct {
	Phone     string `json:"phone"`
	PartyCode string `json:"party_code"`
	OTPCode   string `json:"otp_code"`
}

type verifyOTPResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	UserID       string `json:"user_id"`
	PartyID      int    `json:"party_id"`
	Role         string `json:"role"`
	DisplayName  string `json:"display_name"`
}

// HandleVerifyOTP verifies OTP and issues JWT.
// POST /gotv/mobile/auth/verify-otp
func (ma *MobileAuth) HandleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req verifyOTPRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		mobileJSONErr(w, "invalid request body", http.StatusBadRequest)
		return
	}

	phone := NormalizePhone(req.Phone)
	if req.OTPCode == "" || len(req.OTPCode) != 6 {
		mobileJSONErr(w, "invalid OTP code", http.StatusBadRequest)
		return
	}

	var partyID int
	if err := ma.db.QueryRow("SELECT id FROM parties WHERE LOWER(code)=LOWER($1)", req.PartyCode).Scan(&partyID); err != nil {
		mobileJSONErr(w, "party not found", http.StatusNotFound)
		return
	}

	phoneHash := ma.svc.PhoneHash(phone)

	var userID, storedOTPHash, displayName, role string
	var otpExpiresAt time.Time
	var attempts int
	err := ma.db.QueryRow(
		`SELECT user_id, otp_code_hash, otp_expires_at, otp_attempts, display_name, role
		 FROM gotv_mobile_users WHERE party_id=$1 AND phone_hash=$2`,
		partyID, phoneHash,
	).Scan(&userID, &storedOTPHash, &otpExpiresAt, &attempts, &displayName, &role)
	if err == sql.ErrNoRows {
		mobileJSONErr(w, "no OTP request found for this phone", http.StatusNotFound)
		return
	}
	if err != nil {
		mobileJSONErr(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Check attempts (max 5)
	if attempts >= 5 {
		mobileJSONErr(w, "too many failed attempts, request a new OTP", http.StatusTooManyRequests)
		return
	}

	// Check expiry
	if time.Now().After(otpExpiresAt) {
		mobileJSONErr(w, "OTP expired, request a new one", http.StatusGone)
		return
	}

	// Verify OTP (constant-time)
	incomingHash := hashOTP(req.OTPCode, ma.jwtSecret)
	if !hmacEqual(storedOTPHash, incomingHash) {
		ma.db.Exec("UPDATE gotv_mobile_users SET otp_attempts=otp_attempts+1 WHERE party_id=$1 AND phone_hash=$2", partyID, phoneHash)
		mobileJSONErr(w, "invalid OTP code", http.StatusUnauthorized)
		return
	}

	// OTP valid — issue JWT
	token, expiresAt := ma.issueJWT(userID, partyID, role)
	refreshToken := randHex(32)
	refreshHash := sha256Hex(refreshToken)

	ma.db.Exec(`UPDATE gotv_mobile_users SET
		otp_code_hash=NULL, otp_expires_at=NULL, otp_attempts=0,
		jwt_refresh_token=$1, jwt_expires_at=$2,
		last_login_at=NOW(), updated_at=NOW()
		WHERE party_id=$3 AND phone_hash=$4`,
		refreshHash, expiresAt, partyID, phoneHash,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(verifyOTPResponse{
		Token:        token,
		RefreshToken: refreshToken,
		ExpiresIn:    86400,
		UserID:       userID,
		PartyID:      partyID,
		Role:         role,
		DisplayName:  displayName,
	})
}

// ─── Token Refresh ───────────────────────────────────────────────────────

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// HandleRefreshToken issues a new JWT given a valid refresh token.
// POST /gotv/mobile/auth/refresh
func (ma *MobileAuth) HandleRefreshToken(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		mobileJSONErr(w, "invalid request body", http.StatusBadRequest)
		return
	}

	refreshHash := sha256Hex(req.RefreshToken)

	var userID, role string
	var partyID int
	err := ma.db.QueryRow(
		`SELECT user_id, party_id, role FROM gotv_mobile_users WHERE jwt_refresh_token=$1 AND is_active=TRUE`,
		refreshHash,
	).Scan(&userID, &partyID, &role)
	if err != nil {
		mobileJSONErr(w, "invalid or expired refresh token", http.StatusUnauthorized)
		return
	}

	token, expiresAt := ma.issueJWT(userID, partyID, role)
	newRefresh := randHex(32)
	newRefreshHash := sha256Hex(newRefresh)

	ma.db.Exec("UPDATE gotv_mobile_users SET jwt_refresh_token=$1, jwt_expires_at=$2, updated_at=NOW() WHERE user_id=$3",
		newRefreshHash, expiresAt, userID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":         token,
		"refresh_token": newRefresh,
		"expires_in":    86400,
	})
}

// ─── Mobile Auth Middleware ──────────────────────────────────────────────

// MobileAuthWrap validates mobile JWT tokens on protected endpoints.
func (ma *MobileAuth) MobileAuthWrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			mobileJSONErr(w, "mobile auth required", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")

		claims, err := ma.validateMobileJWT(token)
		if err != nil {
			mobileJSONErr(w, err.Error(), http.StatusUnauthorized)
			return
		}

		r.Header.Set("X-GOTV-Party-ID", strconv.Itoa(claims.PartyID))
		r.Header.Set("X-GOTV-User", claims.UserID)
		r.Header.Set("X-GOTV-Mobile-Role", claims.Role)
		next(w, r)
	}
}

// ─── JWT Helpers (self-issued, HMAC-SHA256) ──────────────────────────────

type mobileClaims struct {
	UserID  string `json:"sub"`
	PartyID int    `json:"party_id"`
	Role    string `json:"role"`
	Exp     int64  `json:"exp"`
	Iat     int64  `json:"iat"`
}

func (ma *MobileAuth) issueJWT(userID string, partyID int, role string) (string, time.Time) {
	expiresAt := time.Now().Add(24 * time.Hour)
	claims := mobileClaims{
		UserID:  userID,
		PartyID: partyID,
		Role:    role,
		Exp:     expiresAt.Unix(),
		Iat:     time.Now().Unix(),
	}

	header := base64URLEncode([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadB64 := base64URLEncode(payload)

	sigInput := header + "." + payloadB64
	mac := hmac.New(sha256.New, ma.jwtSecret)
	mac.Write([]byte(sigInput))
	sig := base64URLEncode(mac.Sum(nil))

	return sigInput + "." + sig, expiresAt
}

func (ma *MobileAuth) validateMobileJWT(token string) (*mobileClaims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token")
	}

	// Verify signature
	sigInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, ma.jwtSecret)
	mac.Write([]byte(sigInput))
	expectedSig := base64URLEncode(mac.Sum(nil))
	if !hmacEqual(parts[2], expectedSig) {
		return nil, fmt.Errorf("invalid token signature")
	}

	// Decode payload
	payloadBytes, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid token payload")
	}

	var claims mobileClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("invalid token claims")
	}

	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}

	return &claims, nil
}

// ─── Utility Functions ───────────────────────────────────────────────────

func generateOTP(length int) (string, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(length)), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", length, n), nil
}

func hashOTP(otp string, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(otp))
	return hex.EncodeToString(mac.Sum(nil))
}

func hmacEqual(a, b string) bool {
	return len(a) == len(b) && hmac.Equal([]byte(a), []byte(b))
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// mobileJSONErr writes a JSON error response (gotv package helper).
func mobileJSONErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
