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
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// r4ScratchDBName is the dedicated scratch database for THIS suite. The R4
// suites in the monolith root, internal/auth and internal/gotv historically
// shared a single scratch database via R4_TEST_DB, but their schemas are
// mutually incompatible (e.g. parties.is_active is INTEGER in the canonical
// monolith migration 000001 while the gotv-svc tests create a BOOLEAN
// variant), so a shared database made `go test ./...` order-dependent.
// Each suite therefore provisions its own database.
const r4ScratchDBName = "r4_wavea_monolith"

var (
	r4ScratchOnce sync.Once
	r4ScratchDSN  string
	r4ScratchErr  error
)

// r4WithDBName retargets a lib/pq DSN at a different database name. Supports
// both URL ("postgres://...") and keyword/value ("host=... dbname=...")
// forms.
func r4WithDBName(dsn, name string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("parse DSN URL: %w", err)
		}
		u.Path = "/" + name
		return u.String(), nil
	}
	fields := strings.Fields(dsn)
	replaced := false
	for i, f := range fields {
		if strings.HasPrefix(f, "dbname=") {
			fields[i] = "dbname=" + name
			replaced = true
		}
	}
	if !replaced {
		fields = append(fields, "dbname="+name)
	}
	return strings.Join(fields, " "), nil
}

// r4ProvisionScratch (re)creates this suite's scratch database from a clean
// slate and applies the CANONICAL migration files that define the tables the
// monolith tests touch (000001: users/parties with is_active INTEGER; 000031:
// users.party_id membership binding). Migrations are applied verbatim through
// a raw lib/pq connection — correctness by construction instead of
// hand-rolled DDL that can drift from the production schema.
func r4ProvisionScratch(baseDSN string) {
	r4ScratchOnce.Do(func() {
		admin, err := sql.Open("postgres", baseDSN)
		if err != nil {
			r4ScratchErr = fmt.Errorf("open admin: %w", err)
			return
		}
		defer admin.Close()
		if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + r4ScratchDBName + ` WITH (FORCE)`); err != nil {
			r4ScratchErr = fmt.Errorf("drop scratch db: %w", err)
			return
		}
		if _, err := admin.Exec(`CREATE DATABASE ` + r4ScratchDBName); err != nil {
			r4ScratchErr = fmt.Errorf("create scratch db: %w", err)
			return
		}
		dsn, err := r4WithDBName(baseDSN, r4ScratchDBName)
		if err != nil {
			r4ScratchErr = err
			return
		}
		raw, err := sql.Open("postgres", dsn)
		if err != nil {
			r4ScratchErr = fmt.Errorf("open scratch: %w", err)
			return
		}
		defer raw.Close()
		for _, f := range []string{
			"migrations/000001_initial_schema.up.sql",
			"migrations/000031_users_party_membership.up.sql",
		} {
			ddl, err := os.ReadFile(f)
			if err != nil {
				r4ScratchErr = fmt.Errorf("read %s: %w", f, err)
				return
			}
			if _, err := raw.Exec(string(ddl)); err != nil {
				r4ScratchErr = fmt.Errorf("apply %s: %w", f, err)
				return
			}
		}
		r4ScratchDSN = dsn
	})
}

// r4MonolithTestDB returns a scratch PostgreSQL handle wrapped in the SAME
// pgcompat shim the production monolith uses (placeholder/dialect conversion
// happens at the driver-connector level), and points the db/dbReader/dbWriter
// globals at it. Skips unless R4_TEST_DB is set. R4_TEST_DB is used as the
// admin/server DSN; the suite's own scratch database (r4ScratchDBName) is
// provisioned underneath it so suites never share schema state.
func r4MonolithTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("R4_TEST_DB")
	if dsn == "" {
		t.Skip("R4_TEST_DB not set — skipping PG-backed regression test")
	}
	r4ProvisionScratch(dsn)
	if r4ScratchErr != nil {
		t.Fatalf("provision scratch db: %v", r4ScratchErr)
	}
	testDB := openPgCompat(r4ScratchDSN)
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

	// elections comes from the canonical migration 000001 schema applied by
	// the suite provisioning; election_state_log is auxiliary test DDL.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS election_state_log (
		id SERIAL PRIMARY KEY, election_id INTEGER, from_state TEXT, to_state TEXT,
		event TEXT, actor TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("ddl state log: %v", err)
	}
	// 'cancel' from draft has no guard → pure read-check-update race surface.
	// NOTE: the scratch DB carries the canonical elections table (migration
	// 000001), whose CHECK constraints require a valid election_type/status.
	var eid int
	if err := db.QueryRow(`INSERT INTO elections (title, election_type, election_date, status)
		VALUES ('r421', 'presidential', '2030-01-01', 'draft') RETURNING id`).Scan(&eid); err != nil {
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

	// The suite scratch DB carries the canonical schema: users and parties
	// from migration 000001 (parties.is_active INTEGER) plus users.party_id
	// from migration 000031 — exactly what the production middleware queries.
	if _, err := db.Exec(`INSERT INTO parties (id, code, name) VALUES (999051,'A','Party A'), (999052,'B','Party B') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("seed parties: %v", err)
	}
	// users.username is not UNIQUE in the canonical schema, so reseed
	// idempotently via delete-then-insert. The role column must satisfy the
	// canonical users_role_check; the middleware authorizes on the JWT role
	// (party_admin below), not on this row.
	if _, err := db.Exec(`DELETE FROM users WHERE username='r405admin'`); err != nil {
		t.Fatalf("reseed user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, full_name, role, party_id)
		VALUES ('r405admin', 'x', 'R4 Admin', 'public', 999051)`); err != nil {
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
