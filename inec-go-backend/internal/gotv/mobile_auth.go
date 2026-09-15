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
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// MobileAuth handles standalone GOTV mobile authentication.
type MobileAuth struct {
	db        *sql.DB
	svc       *Service
	jwtSecret []byte // HMAC-SHA256 key for self-issued JWT
	// R5-042: per-IP fixed-window throttle for the unauthenticated OTP
	// endpoints (per-process; see HANDOFF note re multi-replica deployments).
	ipMu      sync.Mutex
	ipWindows map[string]*ipWindow
}

type ipWindow struct {
	count       int
	windowStart time.Time
}

// allowIP enforces a fixed-window per-IP request limit (limit per hour).
func (ma *MobileAuth) allowIP(r *http.Request, bucket string, limit int) bool {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	key := bucket + "|" + ip
	ma.ipMu.Lock()
	defer ma.ipMu.Unlock()
	if ma.ipWindows == nil {
		ma.ipWindows = make(map[string]*ipWindow)
	}
	win, ok := ma.ipWindows[key]
	if !ok || time.Since(win.windowStart) >= time.Hour {
		ma.ipWindows[key] = &ipWindow{count: 1, windowStart: time.Now()}
		return true
	}
	win.count++
	return win.count <= limit
}

// NewMobileAuth creates a mobile auth handler.
//
// SECURITY: a random per-process JWT key invalidates all sessions on restart
// and masks misconfiguration. In production (APP_ENV=production or
// INEC_ENV=production — see IsProductionEnv) a missing or short
// GOTV_MOBILE_JWT_SECRET is a fatal startup error; the ephemeral random key
// is only allowed outside production.
func NewMobileAuth(db *sql.DB, svc *Service, jwtSecretHex string) *MobileAuth {
	secret, err := hex.DecodeString(strings.TrimSpace(jwtSecretHex))
	if err != nil {
		secret = nil
	}
	if len(secret) < 32 {
		if IsProductionEnv() {
			log.Fatal().Msg("GOTV_MOBILE_JWT_SECRET must be set in production and decode (hex) to at least 32 bytes")
		}
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

	// R5-042: per-IP request throttle (the endpoints are unauthenticated by
	// design, so IP is the only pre-identity signal). In-memory per-process;
	// deployments with multiple replicas should front this with the gateway
	// limiter — see HANDOFF.
	if !ma.allowIP(r, "request-otp", 10) {
		mobileJSONErr(w, "too many requests from this address, try again later", http.StatusTooManyRequests)
		return
	}

	phoneHash := ma.svc.PhoneHash(phone)
	phoneEnc, err := ma.svc.Encrypt(phone)
	if err != nil {
		mobileJSONErr(w, "internal error", http.StatusInternalServerError)
		return
	}

	// R5-042 self-enrollment gate: previously ANY phone+party_code upserted a
	// new account (unauthenticated self-enrollment into arbitrary parties).
	// New enrollments now require a pre-registered, non-opted-out contact
	// with that phone in the party's register; GOTV_MOBILE_OPEN_ENROLLMENT
	// re-enables open enrollment for development and is FORBIDDEN in
	// production (fail closed).
	var existingUser string
	userErr := ma.db.QueryRow(
		"SELECT user_id FROM gotv_mobile_users WHERE party_id=$1 AND phone_hash=$2",
		partyID, phoneHash).Scan(&existingUser)
	if userErr == sql.ErrNoRows {
		enrolled := false
		var contactCount int
		if err := ma.db.QueryRow(
			"SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND phone_hash=$2 AND (opted_out IS NULL OR opted_out=FALSE)",
			partyID, phoneHash).Scan(&contactCount); err == nil && contactCount > 0 {
			enrolled = true
		}
		if !enrolled && os.Getenv("GOTV_MOBILE_OPEN_ENROLLMENT") == "true" && !IsProductionEnv() {
			enrolled = true
		}
		if !enrolled {
			log.Warn().Str("party_code", req.PartyCode).Msg("SECURITY: GOTV mobile self-enrollment rejected (phone not pre-registered)")
			mobileJSONErr(w, "this phone number is not registered with the party; contact your ward coordinator", http.StatusForbidden)
			return
		}
	} else if userErr != nil {
		mobileJSONErr(w, "internal error", http.StatusInternalServerError)
		return
	}

	// R5-042: honor an active lockout BEFORE issuing anything (the old code
	// reset otp_attempts=0 on every re-request — unlimited guesses). The
	// comparisons run in SQL so the database clock is authoritative (the
	// columns are TIMESTAMP without tz; Go-side comparisons would mix
	// session-timezone writes with UTC reads).
	var locked, cooling bool
	ma.db.QueryRow(
		`SELECT (otp_locked_until IS NOT NULL AND otp_locked_until > NOW()),
		        (otp_last_sent_at IS NOT NULL AND otp_last_sent_at > NOW() - INTERVAL '1 minute')
		 FROM gotv_mobile_users WHERE party_id=$1 AND phone_hash=$2`,
		partyID, phoneHash).Scan(&locked, &cooling)
	if locked {
		mobileJSONErr(w, "account temporarily locked after failed attempts, try again later", http.StatusTooManyRequests)
		return
	}
	// Send cooldown: one OTP per 60s per phone.
	if cooling {
		mobileJSONErr(w, "OTP already sent, please wait before requesting another", http.StatusTooManyRequests)
		return
	}

	// R5-042: REAL request rate limit — a counter column in a one-hour
	// window (the previous limit counted rows per phone, which the upsert
	// pinned at 1: dead code).
	var reqCount int
	var windowActive bool
	ma.db.QueryRow(
		`SELECT otp_request_count, (otp_request_window_start IS NOT NULL AND otp_request_window_start > NOW() - INTERVAL '1 hour')
		 FROM gotv_mobile_users WHERE party_id=$1 AND phone_hash=$2`,
		partyID, phoneHash).Scan(&reqCount, &windowActive)
	if windowActive && reqCount >= 5 {
		mobileJSONErr(w, "too many OTP requests, try again later", http.StatusTooManyRequests)
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

	// Upsert user record. R5-042: otp_attempts is NOT reset on re-request —
	// failed guesses survive until success or lockout.
	sessionID := "msess-" + randHex(16)
	displayName := req.Name
	if displayName == "" {
		displayName = "GOTV User"
	}

	_, err = ma.db.Exec(`
		INSERT INTO gotv_mobile_users (user_id, party_id, phone_hash, phone_encrypted, display_name, otp_code_hash, otp_expires_at, otp_attempts, otp_last_sent_at, otp_request_count, otp_request_window_start, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, NOW(), 1, NOW(), NOW())
		ON CONFLICT (party_id, phone_hash)
		DO UPDATE SET otp_code_hash=$6, otp_expires_at=$7, otp_last_sent_at=NOW(),
			otp_request_count = CASE
				WHEN gotv_mobile_users.otp_request_window_start IS NULL
				  OR gotv_mobile_users.otp_request_window_start < NOW() - INTERVAL '1 hour'
				THEN 1 ELSE gotv_mobile_users.otp_request_count + 1 END,
			otp_request_window_start = CASE
				WHEN gotv_mobile_users.otp_request_window_start IS NULL
				  OR gotv_mobile_users.otp_request_window_start < NOW() - INTERVAL '1 hour'
				THEN NOW() ELSE gotv_mobile_users.otp_request_window_start END,
			updated_at=NOW()`,
		sessionID, partyID, phoneHash, phoneEnc, displayName, otpHash, expiresAt,
	)
	if err != nil {
		log.Error().Err(err).Msg("GOTV mobile: failed to upsert user for OTP")
		mobileJSONErr(w, "internal error", http.StatusInternalServerError)
		return
	}

	// In production, send OTP via SMS (AfricasTalking, etc.)
	// Never log the OTP code itself — only log metadata for debugging
	log.Info().Str("phone", phone[:4]+"****").Int("party", partyID).Str("session", sessionID).Msg("GOTV Mobile OTP generated")

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

	// R5-042: per-IP verify throttle — online guessing is distributed, so
	// per-attempt accounting (below) is the real control, but a single
	// address hammering verify is capped too.
	if !ma.allowIP(r, "verify-otp", 30) {
		mobileJSONErr(w, "too many requests from this address, try again later", http.StatusTooManyRequests)
		return
	}

	// R5-042: lockout check FIRST (attempts are no longer reset by
	// re-request, so this actually engages now). DB-side clock comparison.
	var locked bool
	ma.db.QueryRow("SELECT (otp_locked_until IS NOT NULL AND otp_locked_until > NOW()) FROM gotv_mobile_users WHERE party_id=$1 AND phone_hash=$2",
		partyID, phoneHash).Scan(&locked)
	if locked {
		mobileJSONErr(w, "account temporarily locked after failed attempts, try again later", http.StatusTooManyRequests)
		return
	}

	// Check attempts (max 5, then 15-minute lockout + OTP invalidation).
	if attempts >= 5 {
		ma.db.Exec(`UPDATE gotv_mobile_users SET otp_locked_until=NOW() + INTERVAL '15 minutes',
			otp_attempts=0, otp_code_hash=NULL, otp_expires_at=NULL
			WHERE party_id=$1 AND phone_hash=$2`, partyID, phoneHash)
		log.Warn().Str("party_code", req.PartyCode).Msg("SECURITY: GOTV mobile OTP locked after repeated failures")
		mobileJSONErr(w, "too many failed attempts, request a new OTP after the lockout period", http.StatusTooManyRequests)
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
		// R5-042: reaching the attempt cap locks IMMEDIATELY (15 min) and
		// invalidates the OTP — the attacker never gets a 6th guess, and
		// re-requesting does not reopen the window.
		var newAttempts int
		if err := ma.db.QueryRow(`UPDATE gotv_mobile_users SET
			otp_attempts=otp_attempts+1,
			otp_locked_until=CASE WHEN otp_attempts+1 >= 5 THEN NOW() + INTERVAL '15 minutes' ELSE otp_locked_until END,
			otp_code_hash=CASE WHEN otp_attempts+1 >= 5 THEN NULL ELSE otp_code_hash END,
			otp_expires_at=CASE WHEN otp_attempts+1 >= 5 THEN NULL ELSE otp_expires_at END
			WHERE party_id=$1 AND phone_hash=$2
			RETURNING otp_attempts`, partyID, phoneHash).Scan(&newAttempts); err != nil {
			log.Error().Err(err).Msg("GOTV mobile: failed to record OTP failure")
		}
		if newAttempts >= 5 {
			log.Warn().Str("party_code", req.PartyCode).Msg("SECURITY: GOTV mobile OTP locked after repeated failures")
			mobileJSONErr(w, "too many failed attempts, account locked", http.StatusTooManyRequests)
			return
		}
		mobileJSONErr(w, "invalid OTP code", http.StatusUnauthorized)
		return
	}

	// OTP valid — issue JWT
	token, expiresAt := ma.issueJWT(userID, partyID, role)
	refreshToken := randHex(32)
	refreshHash := sha256Hex(refreshToken)

	ma.db.Exec(`UPDATE gotv_mobile_users SET
		otp_code_hash=NULL, otp_expires_at=NULL, otp_attempts=0, otp_locked_until=NULL,
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
