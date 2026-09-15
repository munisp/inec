// W10 PG integration tests (owed from W3): refresh-after-disable rejection,
// EC8A out-of-scope rejection. Each FAILS on the pre-fix code.
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustToken(t *testing.T, role, state string) string {
	t.Helper()
	tok, err := createAccessToken(map[string]interface{}{
		"sub": "9", "username": "officer", "role": role, "state_code": state,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// r5CoreSchema creates the minimal monolith tables the auth/EC8A paths touch.
func r5CoreSchema(t *testing.T) {
	t.Helper()
	if validate == nil {
		initValidator()
	}
	stmts := []string{
		`DROP TABLE IF EXISTS users CASCADE`,
		`DROP TABLE IF EXISTS audit_log CASCADE`,
		`DROP TABLE IF EXISTS token_blacklist CASCADE`,
		`DROP TABLE IF EXISTS active_sessions CASCADE`,
		`DROP TABLE IF EXISTS elections CASCADE`,
		`DROP TABLE IF EXISTS polling_units CASCADE`,
		`DROP TABLE IF EXISTS wards CASCADE`,
		`DROP TABLE IF EXISTS lgas CASCADE`,
		`DROP TABLE IF EXISTS states CASCADE`,
		`CREATE TABLE users (
			id SERIAL PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL DEFAULT '',
			full_name TEXT NOT NULL DEFAULT '', role TEXT NOT NULL, staff_id TEXT, state_code TEXT,
			is_active INTEGER NOT NULL DEFAULT 1, party_id INTEGER, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE audit_log (
			id SERIAL PRIMARY KEY, action TEXT, entity_type TEXT, entity_id TEXT, user_id INTEGER,
			details TEXT, block_hash TEXT, prev_block_hash TEXT, timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE token_blacklist (
			id SERIAL PRIMARY KEY, jti TEXT UNIQUE NOT NULL, user_id INTEGER NOT NULL DEFAULT 0,
			revoked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, expires_at TIMESTAMP NOT NULL, reason TEXT)`,
		`CREATE TABLE active_sessions (
			id SERIAL PRIMARY KEY, jti TEXT NOT NULL, user_id INTEGER NOT NULL,
			ip_address TEXT, user_agent TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL, last_activity TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE states (code TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE lgas (code TEXT PRIMARY KEY, state_code TEXT, name TEXT)`,
		`CREATE TABLE wards (code TEXT PRIMARY KEY, lga_code TEXT, name TEXT)`,
		`CREATE TABLE polling_units (code TEXT PRIMARY KEY, ward_code TEXT, name TEXT)`,
		`CREATE TABLE elections (id SERIAL PRIMARY KEY, status TEXT, election_kind TEXT, name TEXT)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
}

// TestR5038_RefreshAfterDisableRejected: a disabled account must not mint new
// tokens via /auth/refresh (previously claims were copied verbatim).
func TestR5038_RefreshAfterDisableRejected(t *testing.T) {
	r4MonolithTestDB(t)
	r5CoreSchema(t)

	if _, err := db.Exec(`INSERT INTO users (username, password_hash, full_name, role, staff_id, state_code, is_active)
		VALUES ('officer1', 'x', 'Officer One', 'presiding_officer', 'PO-1', 'LA', 1)`); err != nil {
		t.Fatal(err)
	}
	refresh, err := createRefreshToken(map[string]interface{}{
		"sub": "1", "username": "officer1", "role": "presiding_officer", "staff_id": "PO-1", "state_code": "LA",
	})
	if err != nil {
		t.Fatal(err)
	}

	call := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"refresh_token": refresh})
		req := httptest.NewRequest("POST", "/auth/refresh", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handleRefreshToken(w, req)
		return w
	}

	// Active account: refresh succeeds (and ROTATES — the old refresh token
	// is single-use now).
	w := call()
	if w.Code != 200 {
		t.Fatalf("active account refresh must succeed, got %d: %s", w.Code, w.Body.String())
	}
	var rotated struct {
		RefreshToken string `json:"refresh_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &rotated)

	// Replay of the rotated-out token must fail.
	if w := call(); w.Code != 401 {
		t.Fatalf("rotated (replayed) refresh token must be rejected, got %d", w.Code)
	}

	// Disable the account — the rotated refresh must now be rejected.
	if _, err := db.Exec(`UPDATE users SET is_active=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": rotated.RefreshToken})
	req := httptest.NewRequest("POST", "/auth/refresh", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handleRefreshToken(w, req)
	if w.Code != 401 {
		t.Fatalf("R5-038: refresh after disable must be 401, got %d: %s", w.Code, w.Body.String())
	}

	// Demotion: re-enable but demote to 'public' — the new token must carry
	// the DB role, not the old claim.
	db.Exec(`UPDATE users SET is_active=1, role='public' WHERE id=1`)
	req = httptest.NewRequest("POST", "/auth/refresh", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handleRefreshToken(w, req)
	if w.Code != 200 {
		t.Fatalf("re-enabled account refresh must succeed, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	claims, err := decodeToken(resp.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if role, _ := claims["role"].(string); role != "public" {
		t.Fatalf("R5-038: demotion not honored — refreshed token role=%q, want 'public'", role)
	}
}

// TestR5037_EC8AOutOfScopeRejected: a presiding officer from one state must
// not submit an EC8A for a polling unit in another state; a collation
// officer must not submit EC8As at all.
func TestR5037_EC8AOutOfScopeRejected(t *testing.T) {
	r4MonolithTestDB(t)
	r5CoreSchema(t)

	if _, err := db.Exec(`INSERT INTO states (code, name) VALUES ('KN','Kano'),('LA','Lagos')`); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO lgas (code, state_code, name) VALUES ('KN1','KN','Kano LGA')`)
	db.Exec(`INSERT INTO wards (code, lga_code, name) VALUES ('KN1W1','KN1','Ward 1')`)
	db.Exec(`INSERT INTO polling_units (code, ward_code, name) VALUES ('PU-KN-001','KN1W1','PU 1')`)
	db.Exec(`INSERT INTO elections (id, status, election_kind, name) VALUES (1,'voting','general','Test')`)

	form := map[string]interface{}{
		"election_id": 1, "polling_unit_code": "PU-KN-001", "presiding_officer_id": "PO-1",
		"registered_voters": 100, "accredited_voters": 50, "total_votes_polled": 40,
		"rejected_ballots": 5, "total_valid_votes": 35,
		"party_results": []map[string]interface{}{{"party_code": "APC", "votes": 35}},
	}
	body, _ := json.Marshal(form)

	call := func(role, state string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/inec/ec8a/submit", bytes.NewReader(body))
		// Route through the JWT middleware exactly as production does.
		req.Header.Set("Authorization", "Bearer "+mustToken(t, role, state))
		w := httptest.NewRecorder()
		jwtAuthMiddleware(http.HandlerFunc(handleSubmitEC8A)).ServeHTTP(w, req)
		return w
	}

	// Collation officer: forbidden outright (insider must never create PU results).
	if w := call("collation_officer", "KN"); w.Code != 403 {
		t.Fatalf("R5-037: collation_officer must be 403, got %d: %s", w.Code, w.Body.String())
	}
	// Presiding officer from a DIFFERENT state: tenancy rejects.
	if w := call("presiding_officer", "LA"); w.Code != 403 {
		t.Fatalf("R5-037: out-of-state PO must be 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestR5037_EC8AElectionClosedRejected: results are not accepted for an
// election that is not open for capture.
func TestR5037_EC8AElectionClosedRejected(t *testing.T) {
	r4MonolithTestDB(t)
	r5CoreSchema(t)
	db.Exec(`INSERT INTO states (code, name) VALUES ('LA','Lagos')`)
	db.Exec(`INSERT INTO lgas (code, state_code, name) VALUES ('LA1','LA','Lagos LGA')`)
	db.Exec(`INSERT INTO wards (code, lga_code, name) VALUES ('LA1W1','LA1','Ward 1')`)
	db.Exec(`INSERT INTO polling_units (code, ward_code, name) VALUES ('PU-LA-001','LA1W1','PU 1')`)
	db.Exec(`INSERT INTO elections (id, status, election_kind, name) VALUES (1,'completed','general','Done')`)

	form := map[string]interface{}{
		"election_id": 1, "polling_unit_code": "PU-LA-001", "presiding_officer_id": "PO-1",
		"registered_voters": 100, "accredited_voters": 50, "total_votes_polled": 40,
		"rejected_ballots": 5, "total_valid_votes": 35,
		"party_results": []map[string]interface{}{{"party_code": "APC", "votes": 35}},
	}
	body, _ := json.Marshal(form)
	req := httptest.NewRequest("POST", "/inec/ec8a/submit", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+mustToken(t, "presiding_officer", "LA"))
	w := httptest.NewRecorder()
	jwtAuthMiddleware(http.HandlerFunc(handleSubmitEC8A)).ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatalf("R5-037: closed election must reject with 409, got %d: %s", w.Code, w.Body.String())
	}
	var _ = http.StatusOK
}
