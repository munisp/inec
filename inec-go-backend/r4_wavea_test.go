// Wave-A Round-4 regression tests for the monolith (R4-05, R4-07, R4-20,
// R4-21, R4-23, R4-42). PG-backed tests skip unless R4_TEST_DB is set to a
// lib/pq DSN for a scratch database.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// r4MonolithTestDB returns a scratch PostgreSQL handle wrapped in the SAME
// pgcompat shim the production monolith uses (placeholder/dialect conversion
// happens at the driver-connector level), and points the db/dbReader/dbWriter
// globals at it. Skips unless R4_TEST_DB is set.
func r4MonolithTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("R4_TEST_DB")
	if dsn == "" {
		t.Skip("R4_TEST_DB not set — skipping PG-backed regression test")
	}
	testDB := openPgCompat(dsn)
	if err := testDB.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	prevDB, prevReader, prevWriter := db, dbReader, dbWriter
	db, dbReader, dbWriter = testDB, testDB, testDB
	if dbMetrics == nil {
		dbMetrics = newDBMetrics()
	}
	t.Cleanup(func() {
		db, dbReader, dbWriter = prevDB, prevReader, prevWriter
		testDB.Close()
	})
	return testDB
}

// ─── R4-42: JWT secret fail-closed policy ───────────────────────────────────

func TestR442_JWTSecretPolicy(t *testing.T) {
	// Missing secret outside development → error (previously: logged and
	// booted with an EMPTY HMAC key).
	if _, err := resolveJWTSecret("", "production", false); err == nil {
		t.Fatal("R4-42: missing JWT_SECRET in production must be fatal")
	}
	if _, err := resolveJWTSecret("", "staging", false); err == nil {
		t.Fatal("R4-42: missing JWT_SECRET outside development must be fatal")
	}
	// Short secret outside development → error.
	if _, err := resolveJWTSecret("short", "production", false); err == nil {
		t.Fatal("R4-42: short JWT_SECRET in production must be fatal")
	}
	// Development / test binaries → ephemeral key allowed.
	if key, err := resolveJWTSecret("", "development", false); err != nil || len(key) < 32 {
		t.Fatalf("R4-42: dev ephemeral key failed: %v", err)
	}
	if key, err := resolveJWTSecret("", "", true); err != nil || len(key) < 32 {
		t.Fatalf("R4-42: test-binary ephemeral key failed: %v", err)
	}
	// Strong secret → used as-is.
	if key, err := resolveJWTSecret(strings.Repeat("k", 40), "production", false); err != nil || string(key) != strings.Repeat("k", 40) {
		t.Fatalf("R4-42: valid secret rejected: %v", err)
	}
}

// ─── R4-23: ec8a_hash binds vote content ────────────────────────────────────

func TestR423_EC8AHashBindsContent(t *testing.T) {
	scoresA := []PartyVoteEntry{{PartyCode: "APC", Votes: 100}, {PartyCode: "PDP", Votes: 80}}
	scoresB := []PartyVoteEntry{{PartyCode: "APC", Votes: 101}, {PartyCode: "PDP", Votes: 80}}

	h1 := computeEC8AHash(1, "PU-001", scoresA, 200, 5)
	h2 := computeEC8AHash(1, "PU-001", scoresB, 200, 5)
	if h1 == h2 {
		t.Fatal("R4-23: different vote totals must produce different hashes")
	}
	// Same content, different input order → identical hash (canonical).
	scoresAReordered := []PartyVoteEntry{{PartyCode: "PDP", Votes: 80}, {PartyCode: "APC", Votes: 100}}
	if h3 := computeEC8AHash(1, "PU-001", scoresAReordered, 200, 5); h3 != h1 {
		t.Fatal("R4-23: hash must be order-independent (canonical sort)")
	}
	// Election and PU are bound too.
	if h4 := computeEC8AHash(2, "PU-001", scoresA, 200, 5); h4 == h1 {
		t.Fatal("R4-23: hash must bind election_id")
	}
	if h5 := computeEC8AHash(1, "PU-002", scoresA, 200, 5); h5 == h1 {
		t.Fatal("R4-23: hash must bind polling_unit_code")
	}
	if !strings.HasPrefix(h1, "sha256:") || len(h1) != 7+64 {
		t.Fatalf("R4-23: hash format broken: %q", h1)
	}
	// Old behavior was sha256(PU code) only — identical across elections.
	old1 := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("PU-001")))
	if h1 == old1 {
		t.Fatal("R4-23: hash must not equal the legacy PU-code-only hash")
	}
}

// ─── R4-20: PATCH /elections/{id} must not set status directly ──────────────

func TestR420_PatchElectionRejectsStatus(t *testing.T) {
	token, err := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin",
	})
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	req := httptest.NewRequest("PATCH", "/elections/1", strings.NewReader(`{"status":"active","title":"x"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rr := httptest.NewRecorder()
	handleUpdateElection(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("R4-20: PATCH with status returned %d, want 400 (must route through FSM)", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "transition") {
		t.Fatalf("R4-20: error must point at the FSM transition endpoint, got %s", rr.Body.String())
	}
}

func TestR420_EMSLifecycleForwardOnly(t *testing.T) {
	r4MonolithTestDB(t)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS election_lifecycle (
		id SERIAL PRIMARY KEY, election_id INTEGER NOT NULL, phase TEXT NOT NULL,
		transitioned_by INTEGER, notes TEXT, transitioned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	eid := 999020
	db.Exec(`DELETE FROM election_lifecycle WHERE election_id=$1`, eid)

	call := func(phase string) int {
		token, _ := createAccessToken(map[string]interface{}{"sub": "1", "username": "admin", "role": "admin"})
		req := httptest.NewRequest("POST", fmt.Sprintf("/ems/elections/%d/transition", eid),
			strings.NewReader(fmt.Sprintf(`{"phase":%q}`, phase)))
		req.Header.Set("Authorization", "Bearer "+token)
		req = mux.SetURLVars(req, map[string]string{"election_id": fmt.Sprint(eid)})
		rr := httptest.NewRecorder()
		handleTransitionElection(rr, req)
		return rr.Code
	}

	if code := call("bogus_phase"); code != http.StatusBadRequest {
		t.Fatalf("R4-20: unknown phase accepted (%d)", code)
	}
	if code := call("voting_open"); code != http.StatusConflict {
		t.Fatalf("R4-20: skipping ahead from no-phase accepted (%d), want 409", code)
	}
	if code := call("created"); code != http.StatusOK {
		t.Fatalf("R4-20: initial 'created' rejected (%d)", code)
	}
	if code := call("configured"); code != http.StatusOK {
		t.Fatalf("R4-20: forward transition rejected (%d)", code)
	}
	if code := call("created"); code != http.StatusConflict {
		t.Fatalf("R4-20: backward transition accepted (%d), want 409", code)
	}
}

// ─── R4-21: concurrent FSM transitions — exactly one wins ───────────────────

func TestR421_ConcurrentTransitionSerialized(t *testing.T) {
	r4MonolithTestDB(t)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS elections (
		id SERIAL PRIMARY KEY, title TEXT, election_type TEXT, election_date TEXT,
		status TEXT NOT NULL DEFAULT 'draft', description TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("ddl elections: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS election_state_log (
		id SERIAL PRIMARY KEY, election_id INTEGER, from_state TEXT, to_state TEXT,
		event TEXT, actor TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("ddl state log: %v", err)
	}
	// 'cancel' from draft has no guard → pure read-check-update race surface.
	var eid int
	if err := db.QueryRow(`INSERT INTO elections (title, election_type, election_date, status)
		VALUES ('r421', 'test', '2030-01-01', 'draft') RETURNING id`).Scan(&eid); err != nil {
		t.Fatalf("seed election: %v", err)
	}

	const racers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = TransitionElection(context.Background(), eid, "cancel", fmt.Sprintf("racer-%d", i))
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("R4-21: %d concurrent transitions succeeded, want exactly 1", succeeded)
	}
	var finalStatus string
	db.QueryRow(`SELECT status FROM elections WHERE id=$1`, eid).Scan(&finalStatus)
	if finalStatus != "cancelled" {
		t.Fatalf("R4-21: final status %q, want cancelled", finalStatus)
	}
	var logRows int
	db.QueryRow(`SELECT COUNT(*) FROM election_state_log WHERE election_id=$1`, eid).Scan(&logRows)
	if logRows != 1 {
		t.Fatalf("R4-21: %d state-log rows, want exactly 1", logRows)
	}
}

// ─── R4-07: jti revocation end-to-end on real PostgreSQL ────────────────────

func TestR407_RevokedTokenRejectedAndPersisted(t *testing.T) {
	r4MonolithTestDB(t)

	// Real PG DDL (note: NOT via the pgcompat shim — this proves the
	// blacklist SQL is valid PostgreSQL, R4-07c).
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS token_blacklist (
		jti TEXT PRIMARY KEY, user_id INTEGER NOT NULL,
		revoked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP NOT NULL, reason TEXT DEFAULT '')`); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS active_sessions (
		id SERIAL PRIMARY KEY, jti TEXT UNIQUE NOT NULL, user_id INTEGER NOT NULL,
		ip_address TEXT, user_agent TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP NOT NULL, last_activity TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("ddl sessions: %v", err)
	}

	// Mint a token carrying a jti (login path behavior).
	jti := generateJTI()
	token, err := createAccessToken(map[string]interface{}{
		"sub": "42", "username": "officer", "role": "admin", "jti": jti,
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	call := func() int {
		req := httptest.NewRequest("GET", "/api/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		jwtAuthMiddleware(next).ServeHTTP(rr, req)
		return rr.Code
	}

	if code := call(); code != 200 {
		t.Fatalf("valid token rejected: %d", code)
	}

	// Revoke (logout path). Uses the explicit PG ON CONFLICT upsert.
	if err := blacklist.revokeToken(jti, 42, time.Now().Add(time.Hour), "user_logout"); err != nil {
		t.Fatalf("revokeToken: %v", err)
	}
	// Re-revoke must refresh, not error (ON CONFLICT DO UPDATE).
	if err := blacklist.revokeToken(jti, 42, time.Now().Add(2*time.Hour), "admin_revoke"); err != nil {
		t.Fatalf("re-revokeToken: %v", err)
	}

	// Persisted on real PG (old INSERT OR REPLACE dialect could not run on PG).
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM token_blacklist WHERE jti=$1`, jti).Scan(&cnt); err != nil || cnt != 1 {
		t.Fatalf("R4-07: revocation not persisted on PG (count=%d, err=%v)", cnt, err)
	}

	if code := call(); code != http.StatusUnauthorized {
		t.Fatalf("R4-07: revoked token accepted (%d), want 401", code)
	}
}

// ─── R4-05: monolith GOTV party tenancy ──────────────────────────────────────

func TestR405_GOTVPartyHeaderMustMatchMembership(t *testing.T) {
	r4MonolithTestDB(t)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY, username TEXT UNIQUE NOT NULL, party_id INTEGER)`); err != nil {
		t.Fatalf("ddl users: %v", err)
	}
	// The scratch DB may already have a users table from another package's
	// test schema — ensure the membership column exists either way.
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS party_id INTEGER`); err != nil {
		t.Fatalf("alter users: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS parties (
		id SERIAL PRIMARY KEY, code TEXT, name TEXT, is_active INTEGER DEFAULT 1)`); err != nil {
		t.Fatalf("ddl parties: %v", err)
	}
	// Normalize is_active to the monolith's INTEGER convention (the shared
	// scratch DB may carry a BOOLEAN variant created by gotv-svc tests).
	for _, stmt := range []string{
		`ALTER TABLE parties ALTER COLUMN is_active DROP DEFAULT`,
		`ALTER TABLE parties ALTER COLUMN is_active TYPE INTEGER USING CASE WHEN is_active THEN 1 ELSE 0 END`,
		`ALTER TABLE parties ALTER COLUMN is_active SET DEFAULT 1`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("normalize parties.is_active: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO parties (id, code, name) VALUES (999051,'A','Party A'), (999052,'B','Party B') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("seed parties: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (username, party_id) VALUES ('r405admin', 999051) ON CONFLICT (username) DO UPDATE SET party_id=999051`); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "r405admin", "role": "party_admin",
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	call := func(partyHeader string) (int, string) {
		req := httptest.NewRequest("GET", "/gotv/contacts", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if partyHeader != "" {
			req.Header.Set("X-Party-ID", partyHeader)
		}
		rr := httptest.NewRecorder()
		gotvAuthMiddleware(next).ServeHTTP(rr, req)
		return rr.Code, rr.Body.String()
	}

	// Own party → allowed.
	if code, body := call("999051"); code != 200 {
		t.Fatalf("own party rejected: %d %s", code, body)
	}
	// Other party → 403 (was: silently trusted).
	if code, body := call("999052"); code != http.StatusForbidden {
		t.Fatalf("R4-05: cross-party X-Party-ID returned %d, want 403: %s", code, body)
	}
	// No header → falls back to membership party.
	if code, body := call(""); code != 200 {
		t.Fatalf("no-header membership fallback rejected: %d %s", code, body)
	}
}
