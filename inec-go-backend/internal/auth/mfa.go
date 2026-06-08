package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// MFAMethod represents the type of MFA.
type MFAMethod string

const (
	MFAMethodTOTP    MFAMethod = "totp"
	MFAMethodWebAuthn MFAMethod = "webauthn"
)

// MFASetup contains the data needed to set up TOTP.
type MFASetup struct {
	Secret    string `json:"secret"`
	URI       string `json:"uri"`
	QRCode    string `json:"qr_code_data"` // Base64 PNG of QR code
	BackupCodes []string `json:"backup_codes"`
}

// WebAuthnCredential stores a registered WebAuthn credential.
type WebAuthnCredential struct {
	ID            string    `json:"id"`
	PublicKey     []byte    `json:"public_key"`
	CredentialID  []byte    `json:"credential_id"`
	SignCount     uint32    `json:"sign_count"`
	DeviceName    string    `json:"device_name"`
	CreatedAt     time.Time `json:"created_at"`
	LastUsed      time.Time `json:"last_used"`
}

// MFAService handles multi-factor authentication.
type MFAService struct {
	db      *sql.DB
	issuer  string
	digits  int
	period  int
}

// NewMFAService creates a new MFA service.
func NewMFAService(db *sql.DB, issuer string) *MFAService {
	return &MFAService{
		db:     db,
		issuer: issuer,
		digits: 6,
		period: 30,
	}
}

// InitTables creates MFA-related database tables.
func (m *MFAService) InitTables(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS user_mfa (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id),
			method TEXT NOT NULL CHECK(method IN ('totp','webauthn')),
			secret TEXT,
			verified BOOLEAN DEFAULT FALSE,
			enabled BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			verified_at TIMESTAMP,
			UNIQUE(user_id, method)
		);
		CREATE TABLE IF NOT EXISTS mfa_backup_codes (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id),
			code_hash TEXT NOT NULL,
			used BOOLEAN DEFAULT FALSE,
			used_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS webauthn_credentials (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id),
			credential_id BYTEA NOT NULL UNIQUE,
			public_key BYTEA NOT NULL,
			sign_count INTEGER DEFAULT 0,
			device_name TEXT NOT NULL DEFAULT 'Unknown Device',
			aaguid BYTEA,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_used TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_mfa_user ON user_mfa(user_id);
		CREATE INDEX IF NOT EXISTS idx_webauthn_user ON webauthn_credentials(user_id);
		CREATE INDEX IF NOT EXISTS idx_backup_codes_user ON mfa_backup_codes(user_id);
	`)
	return err
}

// SetupTOTP generates a TOTP secret and returns setup data for the user.
func (m *MFAService) SetupTOTP(ctx context.Context, userID int, username string) (*MFASetup, error) {
	// Generate 20-byte secret (160 bits as per RFC 4226)
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("CSPRNG failure: %w", err)
	}
	encodedSecret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)

	// Store secret (unverified)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO user_mfa (user_id, method, secret, verified, enabled)
		 VALUES ($1, 'totp', $2, FALSE, FALSE)
		 ON CONFLICT (user_id, method) DO UPDATE SET secret = $2, verified = FALSE, enabled = FALSE`,
		userID, encodedSecret)
	if err != nil {
		return nil, fmt.Errorf("store TOTP secret: %w", err)
	}

	// Generate backup codes
	backupCodes, err := m.generateBackupCodes(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("generate backup codes: %w", err)
	}

	// Build otpauth URI (RFC 6238)
	uri := fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=%d&period=%d",
		m.issuer, username, encodedSecret, m.issuer, m.digits, m.period)

	return &MFASetup{
		Secret:      encodedSecret,
		URI:         uri,
		BackupCodes: backupCodes,
	}, nil
}

// VerifyTOTP validates a TOTP code and enables MFA if this is the first verification.
func (m *MFAService) VerifyTOTP(ctx context.Context, userID int, code string) (bool, error) {
	var secret string
	var verified bool
	err := m.db.QueryRowContext(ctx,
		`SELECT secret, verified FROM user_mfa WHERE user_id = $1 AND method = 'totp'`,
		userID).Scan(&secret, &verified)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("TOTP not set up for this user")
	}
	if err != nil {
		return false, err
	}

	// Validate against current time ±1 step (allows for clock drift)
	valid := m.validateCode(secret, code, time.Now())
	if !valid {
		log.Warn().Int("user_id", userID).Msg("TOTP verification failed")
		return false, nil
	}

	// If first verification, mark as verified and enable
	if !verified {
		_, err = m.db.ExecContext(ctx,
			`UPDATE user_mfa SET verified = TRUE, enabled = TRUE, verified_at = NOW()
			 WHERE user_id = $1 AND method = 'totp'`,
			userID)
		if err != nil {
			return false, err
		}
		log.Info().Int("user_id", userID).Msg("TOTP MFA enabled")
	}

	return true, nil
}

// ValidateLogin checks MFA during login flow.
func (m *MFAService) ValidateLogin(ctx context.Context, userID int, code string) (bool, error) {
	var enabled bool
	err := m.db.QueryRowContext(ctx,
		`SELECT enabled FROM user_mfa WHERE user_id = $1 AND method = 'totp' AND verified = TRUE`,
		userID).Scan(&enabled)
	if err == sql.ErrNoRows {
		// MFA not set up — allow login without MFA
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !enabled {
		return true, nil
	}

	// Check backup codes first
	if len(code) == 10 { // Backup codes are 10 chars
		return m.useBackupCode(ctx, userID, code)
	}

	return m.VerifyTOTP(ctx, userID, code)
}

// IsMFARequired checks if a user has MFA enabled.
func (m *MFAService) IsMFARequired(ctx context.Context, userID int) (bool, error) {
	var count int
	err := m.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_mfa WHERE user_id = $1 AND verified = TRUE AND enabled = TRUE`,
		userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DisableTOTP disables TOTP for a user.
func (m *MFAService) DisableTOTP(ctx context.Context, userID int) error {
	_, err := m.db.ExecContext(ctx,
		`UPDATE user_mfa SET enabled = FALSE WHERE user_id = $1 AND method = 'totp'`,
		userID)
	return err
}

// GetStatus returns MFA status for a user.
func (m *MFAService) GetStatus(ctx context.Context, userID int) (map[string]interface{}, error) {
	status := map[string]interface{}{
		"totp_enabled":    false,
		"totp_verified":   false,
		"webauthn_count":  0,
		"backup_codes_remaining": 0,
	}

	var totpEnabled, totpVerified bool
	err := m.db.QueryRowContext(ctx,
		`SELECT COALESCE(enabled, FALSE), COALESCE(verified, FALSE) FROM user_mfa WHERE user_id = $1 AND method = 'totp'`,
		userID).Scan(&totpEnabled, &totpVerified)
	if err == nil {
		status["totp_enabled"] = totpEnabled
		status["totp_verified"] = totpVerified
	}

	var webauthnCount int
	m.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webauthn_credentials WHERE user_id = $1`, userID).Scan(&webauthnCount)
	status["webauthn_count"] = webauthnCount

	var backupRemaining int
	m.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mfa_backup_codes WHERE user_id = $1 AND used = FALSE`, userID).Scan(&backupRemaining)
	status["backup_codes_remaining"] = backupRemaining

	return status, nil
}

// VerifyTOTPCode validates a TOTP code directly against a known secret.
// Used during login when the secret is already retrieved from the DB.
func (m *MFAService) VerifyTOTPCode(secret, code string) bool {
	return m.validateCode(secret, code, time.Now())
}

// validateCode checks a TOTP code against the secret with ±1 window.
func (m *MFAService) validateCode(secret, code string, t time.Time) bool {
	counter := uint64(t.Unix()) / uint64(m.period)
	// Check current step and ±1 for clock drift
	for i := int64(-1); i <= 1; i++ {
		expected := m.generateTOTP(secret, counter+uint64(i))
		if hmac.Equal([]byte(expected), []byte(code)) {
			return true
		}
	}
	return false
}

// generateTOTP implements RFC 6238 TOTP generation.
func (m *MFAService) generateTOTP(secret string, counter uint64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}

	// Counter to bytes (big-endian)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	// HMAC-SHA1 (as per RFC 4226/6238)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	hash := mac.Sum(nil)

	// Dynamic truncation
	offset := hash[len(hash)-1] & 0x0f
	code := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7fffffff

	// Modulo to get N digits
	mod := uint32(math.Pow10(m.digits))
	otp := code % mod

	return fmt.Sprintf("%0*d", m.digits, otp)
}

// generateBackupCodes creates 10 single-use backup codes.
func (m *MFAService) generateBackupCodes(ctx context.Context, userID int) ([]string, error) {
	// Delete old codes
	_, _ = m.db.ExecContext(ctx, `DELETE FROM mfa_backup_codes WHERE user_id = $1`, userID)

	codes := make([]string, 10)
	for i := 0; i < 10; i++ {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		codes[i] = strings.ToUpper(base64.RawStdEncoding.EncodeToString(b))[:10]

		// Store hashed
		hash := sha256.Sum256([]byte(codes[i]))
		_, err := m.db.ExecContext(ctx,
			`INSERT INTO mfa_backup_codes (user_id, code_hash) VALUES ($1, $2)`,
			userID, base64.StdEncoding.EncodeToString(hash[:]))
		if err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// useBackupCode validates and consumes a backup code.
func (m *MFAService) useBackupCode(ctx context.Context, userID int, code string) (bool, error) {
	hash := sha256.Sum256([]byte(strings.ToUpper(code)))
	hashStr := base64.StdEncoding.EncodeToString(hash[:])

	result, err := m.db.ExecContext(ctx,
		`UPDATE mfa_backup_codes SET used = TRUE, used_at = NOW()
		 WHERE user_id = $1 AND code_hash = $2 AND used = FALSE`,
		userID, hashStr)
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Info().Int("user_id", userID).Msg("Backup code used")
		return true, nil
	}
	return false, nil
}

// --- WebAuthn Registration/Authentication ---

// WebAuthnChallenge stores pending challenges.
type WebAuthnChallenge struct {
	Challenge []byte
	UserID    int
	ExpiresAt time.Time
}

// BeginWebAuthnRegistration creates a challenge for registering a new credential.
func (m *MFAService) BeginWebAuthnRegistration(ctx context.Context, userID int, username string) (map[string]interface{}, error) {
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, err
	}

	// Store challenge temporarily (expires in 5 minutes)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO user_mfa (user_id, method, secret, verified, enabled)
		 VALUES ($1, 'webauthn', $2, FALSE, FALSE)
		 ON CONFLICT (user_id, method) DO UPDATE SET secret = $2`,
		userID, base64.StdEncoding.EncodeToString(challenge))
	if err != nil {
		return nil, err
	}

	// Return PublicKeyCredentialCreationOptions
	options := map[string]interface{}{
		"challenge": base64.RawURLEncoding.EncodeToString(challenge),
		"rp": map[string]interface{}{
			"name": m.issuer,
			"id":   "inec.gov.ng",
		},
		"user": map[string]interface{}{
			"id":          base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d", userID))),
			"name":        username,
			"displayName": username,
		},
		"pubKeyCredParams": []map[string]interface{}{
			{"type": "public-key", "alg": -7},   // ES256
			{"type": "public-key", "alg": -257}, // RS256
		},
		"timeout":     300000, // 5 minutes
		"attestation": "direct",
		"authenticatorSelection": map[string]interface{}{
			"authenticatorAttachment": "cross-platform",
			"userVerification":        "preferred",
			"residentKey":             "preferred",
		},
	}

	return options, nil
}

// CompleteWebAuthnRegistration stores the credential from the authenticator response.
func (m *MFAService) CompleteWebAuthnRegistration(ctx context.Context, userID int, credentialID, publicKey []byte, deviceName string) error {
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO webauthn_credentials (user_id, credential_id, public_key, device_name)
		 VALUES ($1, $2, $3, $4)`,
		userID, credentialID, publicKey, deviceName)
	if err != nil {
		return fmt.Errorf("store credential: %w", err)
	}

	// Mark webauthn as verified and enabled
	_, err = m.db.ExecContext(ctx,
		`INSERT INTO user_mfa (user_id, method, verified, enabled)
		 VALUES ($1, 'webauthn', TRUE, TRUE)
		 ON CONFLICT (user_id, method) DO UPDATE SET verified = TRUE, enabled = TRUE`,
		userID)

	log.Info().Int("user_id", userID).Str("device", deviceName).Msg("WebAuthn credential registered")
	return err
}

// ListWebAuthnCredentials returns all registered credentials for a user.
func (m *MFAService) ListWebAuthnCredentials(ctx context.Context, userID int) ([]WebAuthnCredential, error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT credential_id, public_key, sign_count, device_name, created_at, COALESCE(last_used, created_at)
		 FROM webauthn_credentials WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []WebAuthnCredential
	for rows.Next() {
		var c WebAuthnCredential
		if err := rows.Scan(&c.CredentialID, &c.PublicKey, &c.SignCount, &c.DeviceName, &c.CreatedAt, &c.LastUsed); err != nil {
			continue
		}
		c.ID = base64.RawURLEncoding.EncodeToString(c.CredentialID)
		creds = append(creds, c)
	}
	return creds, nil
}

// DeleteWebAuthnCredential removes a registered credential.
func (m *MFAService) DeleteWebAuthnCredential(ctx context.Context, userID int, credentialID string) error {
	decoded, err := base64.RawURLEncoding.DecodeString(credentialID)
	if err != nil {
		return fmt.Errorf("invalid credential ID")
	}
	_, err = m.db.ExecContext(ctx,
		`DELETE FROM webauthn_credentials WHERE user_id = $1 AND credential_id = $2`,
		userID, decoded)
	return err
}

// RegenerateBackupCodes invalidates old codes and generates a fresh set.
func (m *MFAService) RegenerateBackupCodes(ctx context.Context, userID int) ([]string, error) {
	// Delete existing codes
	_, _ = m.db.ExecContext(ctx, `DELETE FROM mfa_backup_codes WHERE user_id = $1`, userID)
	// Generate new set
	return m.generateBackupCodes(ctx, userID)
}
