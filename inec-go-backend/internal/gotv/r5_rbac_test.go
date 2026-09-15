// R5-036 regression tests: the GOTV role must be derived server-side
// (party membership / credential row), never from the client-supplied
// X-GOTV-Role header. Each test FAILS on the pre-fix code and PASSES after.
package gotv

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// r5RBACSchema creates the minimal credential/membership tables the auth
// middleware queries (column-compatible subset of migrations 000026/000041).
func r5RBACSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`DROP TABLE IF EXISTS gotv_party_members`,
		`DROP TABLE IF EXISTS gotv_party_access`,
		`CREATE TABLE gotv_party_access (
			id SERIAL PRIMARY KEY,
			party_id INTEGER NOT NULL,
			api_key_hash TEXT NOT NULL,
			created_by TEXT NOT NULL,
			is_active BOOLEAN DEFAULT TRUE,
			rate_limit_per_hour INTEGER DEFAULT 1000,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP,
			role TEXT NOT NULL DEFAULT 'field_worker'
		)`,
		`CREATE TABLE gotv_party_members (
			id SERIAL PRIMARY KEY,
			party_id INTEGER NOT NULL,
			user_email TEXT NOT NULL,
			role TEXT NOT NULL,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (party_id, user_email)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
}

func r5HashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// TestR5036_APIKeyRoleFromDBNotHeader: an API-key caller whose credential row
// says field_worker must NOT be able to claim party_admin via the header.
func TestR5036_APIKeyRoleFromDBNotHeader(t *testing.T) {
	db := r4TestDB(t)
	r5RBACSchema(t, db)
	if _, err := db.Exec(
		`INSERT INTO gotv_party_access (party_id, api_key_hash, created_by, is_active, role)
		 VALUES (7, $1, 'ops@party.ng', TRUE, 'field_worker')`, r5HashKey("party-key-7")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	am := NewAuthMiddleware(db, AuthConfig{})
	var seenRole string
	handler := am.Wrap(func(w http.ResponseWriter, r *http.Request) {
		seenRole = r.Header.Get("X-GOTV-Role")
		w.WriteHeader(200)
	})

	req := httptest.NewRequest("GET", "/gotv/export/contacts", nil)
	req.Header.Set("X-API-Key", "party-key-7")
	req.Header.Set("X-GOTV-Role", "party_admin") // spoof attempt
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != 200 {
		t.Fatalf("valid API key rejected: %d %s", w.Code, w.Body.String())
	}
	if seenRole != "field_worker" {
		t.Fatalf("R5-036: role must come from the credential row; got %q (client header honored?)", seenRole)
	}
}

// TestR5036_MembershipRoleLookup: a gateway-trusted identity gets the role
// from gotv_party_members; a member with no row gets NO role (fail closed).
func TestR5036_MembershipRoleLookup(t *testing.T) {
	db := r4TestDB(t)
	r5RBACSchema(t, db)
	if _, err := db.Exec(
		`INSERT INTO gotv_party_members (party_id, user_email, role) VALUES (7, 'coord@party.ng', 'coordinator')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Setenv("INTERNAL_SERVICE_SECRET", "gw-secret")
	am := NewAuthMiddleware(db, AuthConfig{})

	call := func(user string) (int, string) {
		var seenRole string
		handler := am.Wrap(func(w http.ResponseWriter, r *http.Request) {
			seenRole = r.Header.Get("X-GOTV-Role")
			w.WriteHeader(200)
		})
		req := httptest.NewRequest("GET", "/gotv/campaigns", nil)
		req.Header.Set("X-Internal-Service", "gateway")
		req.Header.Set("X-Party-ID", "7")
		req.Header.Set("X-Internal-Token", "gw-secret")
		req.Header.Set("X-User", user)
		req.Header.Set("X-GOTV-Role", "party_admin") // spoof attempt
		w := httptest.NewRecorder()
		handler(w, req)
		return w.Code, seenRole
	}

	code, role := call("coord@party.ng")
	if code != 200 || role != "coordinator" {
		t.Fatalf("member role lookup failed: code=%d role=%q", code, role)
	}

	code, role = call("nobody@party.ng")
	if code != 200 {
		t.Fatalf("authenticated non-member should still pass auth, got %d", code)
	}
	if role != "" {
		t.Fatalf("R5-036: non-member must get NO role (fail closed), got %q", role)
	}
}

// TestR5036_DisabledMembershipDenied: a deactivated member loses the role.
func TestR5036_DisabledMembershipDenied(t *testing.T) {
	db := r4TestDB(t)
	r5RBACSchema(t, db)
	if _, err := db.Exec(
		`INSERT INTO gotv_party_members (party_id, user_email, role, is_active) VALUES (7, 'ex@party.ng', 'party_admin', FALSE)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Setenv("INTERNAL_SERVICE_SECRET", "gw-secret")
	am := NewAuthMiddleware(db, AuthConfig{})
	var seenRole = "unset"
	handler := am.Wrap(func(w http.ResponseWriter, r *http.Request) {
		seenRole = r.Header.Get("X-GOTV-Role")
		w.WriteHeader(200)
	})
	req := httptest.NewRequest("GET", "/gotv/campaigns", nil)
	req.Header.Set("X-Internal-Service", "gateway")
	req.Header.Set("X-Party-ID", "7")
	req.Header.Set("X-Internal-Token", "gw-secret")
	req.Header.Set("X-User", "ex@party.ng")
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != 200 || seenRole != "" {
		t.Fatalf("disabled membership must yield no role: code=%d role=%q", w.Code, seenRole)
	}
}

// TestR5036_UnauthenticatedHeaderNeverAccepted: a bare spoofed header without
// any credential is rejected outright.
func TestR5036_UnauthenticatedHeaderNeverAccepted(t *testing.T) {
	am := NewAuthMiddleware(nil, AuthConfig{})
	handler := am.Wrap(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run for unauthenticated request")
	})
	req := httptest.NewRequest("GET", "/gotv/export/contacts", nil)
	req.Header.Set("X-GOTV-Role", "party_admin")
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
}

// TestR5036_NormalizeRole: only the closed GOTV role set is recognized.
func TestR5036_NormalizeRole(t *testing.T) {
	for role, want := range map[string]bool{
		"party_admin": true, "coordinator": true, "team_lead": true,
		"field_worker": true, "observer": true, "analyst": true,
		"admin": false, "superadmin": false, "": false, "PARTY_ADMIN": false,
	} {
		if got := normalizeRole(role) != ""; got != want {
			t.Errorf("normalizeRole(%q) recognized=%v, want %v", role, got, want)
		}
	}
}
