package main

// Focused PG-backed tests for R5-095: /ems/portals/{id}/sync re-wired behind
// the IReV configuration gate. Verifies: auth required, unknown portal 404,
// non-IReV portal types fail closed (503), inactive portal 409, and an IReV
// portal without EXTERNAL IRev credentials fails closed (503) instead of
// counting local records as a completed exchange.

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

var portalSyncTestReady bool

func setupPortalSyncTestDB(t *testing.T) {
	t.Helper()
	if portalSyncTestReady && db != nil {
		return
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "host=/home/kimi/pgdata user=postgres dbname=postgres sslmode=disable"
	}
	admin := openDatabase(dsn)
	defer admin.Close()
	dbName := "w7_portal_sync_test"
	admin.Exec("DROP DATABASE IF EXISTS " + dbName)
	if _, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Skipf("cannot create test database (PG unavailable?): %v", err)
	}
	testDSN := strings.Replace(dsn, "dbname=postgres", "dbname="+dbName, 1)
	if !strings.Contains(testDSN, "dbname=") {
		testDSN = dsn + " dbname=" + dbName
	}
	db = openDatabase(testDSN)
	initScaledDB(db)
	if err := runMigrations(db); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}
	initEMSTables(db)
	initIReVSchema(db)
	portalSyncTestReady = true
}

func TestPortalSyncFailsClosed(t *testing.T) {
	setupPortalSyncTestDB(t)
	// IReV credentials are EXTERNAL; ensure they are absent for this test.
	for _, env := range []string{"IREV_API_BASE_URL", "IREV_SUBMIT_PATH", "IREV_OAUTH_TOKEN_URL", "IREV_OAUTH_CLIENT_ID", "IREV_OAUTH_CLIENT_SECRET", "IREV_MTLS_CLIENT_CERT_FILE", "IREV_MTLS_CLIENT_KEY_FILE", "IREV_CA_CERT_FILE"} {
		t.Setenv(env, "")
	}

	var pressID, irevID, inactiveID int64
	pressID = insertReturningID(db, `INSERT INTO portal_connections (portal_name, portal_type, base_url, status) VALUES ('t-press','press','https://press.example','active')`)
	irevID = insertReturningID(db, `INSERT INTO portal_connections (portal_name, portal_type, base_url, status) VALUES ('t-irev','irev','https://irev.example','active')`)
	inactiveID = insertReturningID(db, `INSERT INTO portal_connections (portal_name, portal_type, base_url, status) VALUES ('t-inactive','irev','https://irev2.example','inactive')`)
	if pressID == 0 || irevID == 0 || inactiveID == 0 {
		t.Fatalf("seed portal connections failed: %d %d %d", pressID, irevID, inactiveID)
	}

	idVars := func(id int64) map[string]string {
		return map[string]string{"id": strconv.FormatInt(id, 10)}
	}

	// 1. No authenticated claims → 403.
	w := doRequest(handlePortalSync, "POST", "/ems/portals/1/sync", "{}", nil, map[string]string{"id": "1"})
	if w.Code != 403 {
		t.Fatalf("unauthenticated portal sync: expected 403, got %d (%s)", w.Code, w.Body.String())
	}

	// 2. Unknown portal id → 404.
	w = doRequest(handlePortalSync, "POST", "/ems/portals/999999/sync", "{}", adminClaims, map[string]string{"id": "999999"})
	if w.Code != 404 {
		t.Fatalf("unknown portal: expected 404, got %d (%s)", w.Code, w.Body.String())
	}

	// 3. Non-IReV portal type fails closed → 503.
	w = doRequest(handlePortalSync, "POST", "/ems/portals/x/sync", "{}", adminClaims, idVars(pressID))
	if w.Code != 503 {
		t.Fatalf("non-irev portal: expected 503, got %d (%s)", w.Code, w.Body.String())
	}

	// 4. Inactive portal → 409.
	w = doRequest(handlePortalSync, "POST", "/ems/portals/x/sync", "{}", adminClaims, idVars(inactiveID))
	if w.Code != 409 {
		t.Fatalf("inactive portal: expected 409, got %d (%s)", w.Code, w.Body.String())
	}

	// 5. IReV portal without EXTERNAL IReV credentials → 503 (config gate).
	w = doRequest(handlePortalSync, "POST", "/ems/portals/x/sync", "{}", adminClaims, idVars(irevID))
	if w.Code != 503 {
		t.Fatalf("unconfigured irev portal: expected 503, got %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not configured") {
		t.Fatalf("expected configuration-gate message, got %s", w.Body.String())
	}

	// No portal_sync_log row may be written when the config gate rejects.
	var logCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM portal_sync_log WHERE portal_id=?`, irevID).Scan(&logCount); err != nil {
		t.Fatalf("count portal_sync_log: %v", err)
	}
	if logCount != 0 {
		t.Fatalf("config-gated sync must not write portal_sync_log rows, found %d", logCount)
	}
}

