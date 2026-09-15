// R5-046 regression tests: API keys hashed at rest, rotation revokes the old
// key in the LIVE table, URL query-param keys rejected.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestR5046_RawKeysMigratedToHashes(t *testing.T) {
	tdb := r4MonolithTestDB(t)
	if _, err := tdb.Exec(`DROP TABLE IF EXISTS api_keys`); err != nil {
		t.Fatal(err)
	}
	if _, err := tdb.Exec(`CREATE TABLE api_keys (
		id SERIAL PRIMARY KEY, key_hash TEXT UNIQUE NOT NULL, name TEXT NOT NULL,
		owner TEXT NOT NULL, permissions TEXT NOT NULL DEFAULT 'read',
		rate_limit INTEGER NOT NULL DEFAULT 100, is_active INTEGER NOT NULL DEFAULT 1,
		last_used_at TIMESTAMP, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	// Legacy row: raw key stored directly.
	if _, err := tdb.Exec(`INSERT INTO api_keys (key_hash, name, owner) VALUES ('raw-secret-key-1', 'legacy', 'system')`); err != nil {
		t.Fatal(err)
	}
	migrateAPIKeysToHashed(tdb)

	var stored string
	if err := tdb.QueryRow(`SELECT key_hash FROM api_keys WHERE name='legacy'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "raw-secret-key-1" {
		t.Fatal("R5-046: raw API key still stored in plaintext")
	}
	if stored != hashAPIKey("raw-secret-key-1") {
		t.Fatalf("migration produced wrong hash: %s", stored)
	}
	// Idempotent: a second run must not double-hash.
	migrateAPIKeysToHashed(tdb)
	tdb.QueryRow(`SELECT key_hash FROM api_keys WHERE name='legacy'`).Scan(&stored)
	if stored != hashAPIKey("raw-secret-key-1") {
		t.Fatal("migration is not idempotent — double-hashed")
	}
}

func TestR5046_QueryParamKeyRejected(t *testing.T) {
	r4MonolithTestDB(t)
	called := false
	handler := apiKeyAuth(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest("GET", "/api/v1/results?api_key=whatever", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != 401 || called {
		t.Fatalf("query-param API key must be rejected, got %d", w.Code)
	}
}

func TestR5046_RotationRevokesOldKeyInLiveTable(t *testing.T) {
	tdb := r4MonolithTestDB(t)
	tdb.Exec(`DROP TABLE IF EXISTS api_keys`)
	tdb.Exec(`CREATE TABLE api_keys (
		id SERIAL PRIMARY KEY, key_hash TEXT UNIQUE NOT NULL, name TEXT NOT NULL,
		owner TEXT NOT NULL, permissions TEXT NOT NULL DEFAULT 'read',
		rate_limit INTEGER NOT NULL DEFAULT 100, is_active INTEGER NOT NULL DEFAULT 1,
		last_used_at TIMESTAMP, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`)
	tdb.Exec(`DROP TABLE IF EXISTS api_key_metadata`)
	tdb.Exec(`CREATE TABLE api_key_metadata (
		id SERIAL PRIMARY KEY, key_hash TEXT UNIQUE NOT NULL, name TEXT NOT NULL,
		owner_id INTEGER NOT NULL, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP, rotated_from TEXT, is_active BOOLEAN DEFAULT TRUE,
		last_used_at TIMESTAMP, usage_count INTEGER DEFAULT 0)`)

	oldHash := hashAPIKey("old-live-key")
	if _, err := tdb.Exec(`INSERT INTO api_keys (key_hash, name, owner) VALUES ($1, 'live', 'system')`, oldHash); err != nil {
		t.Fatal(err)
	}

	newKey, err := rotateAPIKey(oldHash, 7, "live-rotated")
	if err != nil {
		t.Fatalf("rotation failed: %v", err)
	}
	if newKey == "" {
		t.Fatal("rotation returned empty key")
	}
	// Old key must be dead in the LIVE table.
	var oldActive int
	tdb.QueryRow(`SELECT is_active FROM api_keys WHERE key_hash=$1`, oldHash).Scan(&oldActive)
	if oldActive != 0 {
		t.Fatal("R5-046: old key still active after rotation (rotation theater)")
	}
	// New key must authenticate via the hash path.
	var newActive int
	err = tdb.QueryRow(`SELECT is_active FROM api_keys WHERE key_hash=$1`, hashAPIKey(newKey)).Scan(&newActive)
	if err != nil || newActive != 1 {
		t.Fatalf("new key not active in live table: err=%v active=%d", err, newActive)
	}
	// Rotating an unknown old key fails loudly.
	if _, err := rotateAPIKey(hashAPIKey("nonexistent"), 7, "ghost"); err == nil {
		t.Fatal("rotation with unknown old key must fail")
	}
	_ = json.Marshal
	_ = strings.TrimSpace
}
