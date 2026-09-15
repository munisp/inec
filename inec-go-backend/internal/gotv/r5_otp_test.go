// R5-042 regression tests: GOTV mobile OTP — self-enrollment gate, real
// attempt accounting (no reset on re-request), lockout engagement.
package gotv

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func r5OTPSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`DROP TABLE IF EXISTS gotv_mobile_users`,
		`DROP TABLE IF EXISTS gotv_contacts`,
		`DROP TABLE IF EXISTS parties`,
		`CREATE TABLE parties (id SERIAL PRIMARY KEY, code TEXT NOT NULL, name TEXT)`,
		`INSERT INTO parties (code, name) VALUES ('apc', 'Test Party')`,
		`CREATE TABLE gotv_contacts (
			id SERIAL PRIMARY KEY, contact_id TEXT UNIQUE NOT NULL, party_id INTEGER NOT NULL,
			phone_encrypted TEXT NOT NULL, phone_hash TEXT NOT NULL, opted_out BOOLEAN DEFAULT FALSE
		)`,
		`CREATE TABLE gotv_mobile_users (
			id SERIAL PRIMARY KEY, user_id TEXT UNIQUE NOT NULL, party_id INTEGER NOT NULL,
			phone_hash TEXT NOT NULL, phone_encrypted TEXT NOT NULL, display_name TEXT NOT NULL,
			role TEXT DEFAULT 'canvasser', is_active BOOLEAN DEFAULT TRUE,
			otp_code_hash TEXT, otp_expires_at TIMESTAMP, otp_attempts INTEGER DEFAULT 0,
			otp_last_sent_at TIMESTAMP, otp_locked_until TIMESTAMP,
			otp_request_count INTEGER NOT NULL DEFAULT 0, otp_request_window_start TIMESTAMP,
			jwt_refresh_token TEXT, jwt_expires_at TIMESTAMP,
			last_login_at TIMESTAMP, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, UNIQUE(party_id, phone_hash)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
}

func r5MobileAuth(t *testing.T, db *sql.DB) *MobileAuth {
	t.Helper()
	svc := NewService(db, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	return NewMobileAuth(db, svc, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
}

func r5Post(ma http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/gotv/mobile/auth/request-otp", strings.NewReader(body))
	req.RemoteAddr = "10.0.0.1:12345"
	w := httptest.NewRecorder()
	ma(w, req)
	return w
}

// TestR5042_SelfEnrollmentRejected: an unknown phone must not self-enroll.
func TestR5042_SelfEnrollmentRejected(t *testing.T) {
	db := r4TestDB(t)
	r5OTPSchema(t, db)
	ma := r5MobileAuth(t, db)

	w := r5Post(ma.HandleRequestOTP, `{"phone":"08031234567","party_code":"apc"}`)
	if w.Code != 403 {
		t.Fatalf("self-enrollment must be rejected, got %d: %s", w.Code, w.Body.String())
	}
	var userCount int
	db.QueryRow("SELECT COUNT(*) FROM gotv_mobile_users").Scan(&userCount)
	if userCount != 0 {
		t.Fatalf("self-enrollment created a user row")
	}
}

// TestR5042_PreRegisteredPhoneEnrolls: a phone in the party register may enroll.
func TestR5042_PreRegisteredPhoneEnrolls(t *testing.T) {
	db := r4TestDB(t)
	r5OTPSchema(t, db)
	ma := r5MobileAuth(t, db)
	phoneHash := ma.svc.PhoneHash(NormalizePhone("08031234567"))
	phoneEnc, _ := ma.svc.Encrypt(NormalizePhone("08031234567"))
	if _, err := db.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash)
		VALUES ('c1', 1, $1, $2)`, phoneEnc, phoneHash); err != nil {
		t.Fatalf("seed contact: %v", err)
	}

	w := r5Post(ma.HandleRequestOTP, `{"phone":"08031234567","party_code":"apc"}`)
	if w.Code != 200 {
		t.Fatalf("pre-registered phone must enroll, got %d: %s", w.Code, w.Body.String())
	}
}

// TestR5042_AttemptsNotResetByReRequest: failed verify attempts must survive
// an OTP re-request (previously otp_attempts was zeroed on every re-request).
func TestR5042_AttemptsNotResetByReRequest(t *testing.T) {
	db := r4TestDB(t)
	r5OTPSchema(t, db)
	ma := r5MobileAuth(t, db)
	phoneHash := ma.svc.PhoneHash(NormalizePhone("08031234567"))
	phoneEnc, _ := ma.svc.Encrypt(NormalizePhone("08031234567"))
	db.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash) VALUES ('c1', 1, $1, $2)`, phoneEnc, phoneHash)

	if w := r5Post(ma.HandleRequestOTP, `{"phone":"08031234567","party_code":"apc"}`); w.Code != 200 {
		t.Fatalf("request 1: %d", w.Code)
	}

	// Four wrong guesses.
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest("POST", "/gotv/mobile/auth/verify-otp", strings.NewReader(`{"phone":"08031234567","party_code":"apc","otp_code":"000000"}`))
		req.RemoteAddr = "10.0.0.2:999"
		w := httptest.NewRecorder()
		ma.HandleVerifyOTP(w, req)
		if w.Code != 401 {
			t.Fatalf("wrong OTP should be 401, got %d", w.Code)
		}
	}
	var attempts int
	db.QueryRow("SELECT otp_attempts FROM gotv_mobile_users WHERE party_id=1 AND phone_hash=$1", phoneHash).Scan(&attempts)
	if attempts != 4 {
		t.Fatalf("expected 4 attempts recorded, got %d", attempts)
	}

	// Cooldown blocks an immediate re-request (60s)...
	w := r5Post(ma.HandleRequestOTP, `{"phone":"08031234567","party_code":"apc"}`)
	if w.Code != 429 {
		t.Fatalf("cooldown re-request must be 429, got %d", w.Code)
	}
	// ...simulate cooldown expiry, re-request, and attempts must SURVIVE.
	db.Exec("UPDATE gotv_mobile_users SET otp_last_sent_at=NOW() - INTERVAL '2 minutes' WHERE party_id=1 AND phone_hash=$1", phoneHash)
	if w := r5Post(ma.HandleRequestOTP, `{"phone":"08031234567","party_code":"apc"}`); w.Code != 200 {
		t.Fatalf("re-request after cooldown: %d: %s", w.Code, w.Body.String())
	}
	db.QueryRow("SELECT otp_attempts FROM gotv_mobile_users WHERE party_id=1 AND phone_hash=$1", phoneHash).Scan(&attempts)
	if attempts != 4 {
		t.Fatalf("R5-042: re-request reset attempts (got %d, want 4) — brute-force window reopened", attempts)
	}

	// Fifth wrong guess trips the lockout and invalidates the OTP.
	req := httptest.NewRequest("POST", "/gotv/mobile/auth/verify-otp", strings.NewReader(`{"phone":"08031234567","party_code":"apc","otp_code":"000000"}`))
	req.RemoteAddr = "10.0.0.2:999"
	w = httptest.NewRecorder()
	ma.HandleVerifyOTP(w, req)
	if w.Code != 429 {
		t.Fatalf("5th failure must lock (429), got %d", w.Code)
	}
	var locked interface{}
	db.QueryRow("SELECT otp_locked_until FROM gotv_mobile_users WHERE party_id=1 AND phone_hash=$1", phoneHash).Scan(&locked)
	if locked == nil {
		t.Fatalf("lockout timestamp not set after 5 failures")
	}
}

// TestR5042_RequestRateLimit: >5 requests/hour per phone is capped.
func TestR5042_RequestRateLimit(t *testing.T) {
	db := r4TestDB(t)
	r5OTPSchema(t, db)
	ma := r5MobileAuth(t, db)
	phoneHash := ma.svc.PhoneHash(NormalizePhone("08031234567"))
	phoneEnc, _ := ma.svc.Encrypt(NormalizePhone("08031234567"))
	db.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash) VALUES ('c1', 1, $1, $2)`, phoneEnc, phoneHash)
	// Bypass the send cooldown for the rate-limit test.
	for i := 0; i < 5; i++ {
		db.Exec("UPDATE gotv_mobile_users SET otp_last_sent_at=NOW() - INTERVAL '2 minutes' WHERE party_id=1 AND phone_hash=$1", phoneHash)
		if w := r5Post(ma.HandleRequestOTP, `{"phone":"08031234567","party_code":"apc"}`); w.Code != 200 {
			t.Fatalf("request %d: got %d", i+1, w.Code)
		}
	}
	db.Exec("UPDATE gotv_mobile_users SET otp_last_sent_at=NOW() - INTERVAL '2 minutes' WHERE party_id=1 AND phone_hash=$1", phoneHash)
	if w := r5Post(ma.HandleRequestOTP, `{"phone":"08031234567","party_code":"apc"}`); w.Code != 429 {
		t.Fatalf("6th request in the hour must be 429, got %d", w.Code)
	}
}
