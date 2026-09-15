package main

// Focused tests for W4 fixes: R5-054 (ed25519 signing, append-only),
// R5-055 (retention legal hold + real archive), R5-068 (Merkle event root).
//
// DB-backed tests require PostgreSQL (socket /home/kimi/pgdata or
// W4_TEST_DSN); they skip cleanly when no database is reachable.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func w4TestDB(t *testing.T) {
	t.Helper()
	if db != nil {
		return
	}
	dsn := os.Getenv("W4_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://postgres@/postgres?host=/home/kimi/pgdata&sslmode=disable"
	}
	opened := openDatabase(dsn)
	if err := opened.Ping(); err != nil {
		t.Skipf("no test database reachable: %v", err)
	}
	db = opened
	dbReader = opened
	dbWriter = opened
}

func w4EnsureSchema(t *testing.T) {
	t.Helper()
	w4TestDB(t)
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS audit_log (
			id SERIAL PRIMARY KEY,
			action text NOT NULL, entity_type text NOT NULL, entity_id text,
			user_id integer, details text, block_hash text, prev_block_hash text,
			"timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS result_signatures (
			id SERIAL PRIMARY KEY, result_id integer NOT NULL, officer_pubkey text NOT NULL,
			signature text NOT NULL, prev_hash text, result_hash text NOT NULL,
			chain_position integer DEFAULT 0,
			signed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
			algorithm text, signature_hash text, signer_id integer, signer_role text, verification_status text)`,
		`CREATE TABLE IF NOT EXISTS legal_holds (
			id SERIAL PRIMARY KEY, table_name text NOT NULL, reason text NOT NULL,
			placed_by text, placed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
			released_at timestamp without time zone, UNIQUE(table_name, reason))`,
		`CREATE TABLE IF NOT EXISTS retention_archive_log (
			id SERIAL PRIMARY KEY, table_name text NOT NULL, policy_name text NOT NULL,
			cutoff_date text NOT NULL, row_count integer NOT NULL, file_path text NOT NULL,
			sha256 text NOT NULL, legal_hold integer NOT NULL DEFAULT 0,
			archived_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS official_tracking_history (
			id SERIAL PRIMARY KEY, official_id integer, latitude real, longitude real,
			recorded_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema setup: %v", err)
		}
	}
	// Apply W4 migration 000034 (audit/signature/retention schema, idempotent).
	// NOTE: 000033 is W1's ingestion migration (renumbered) — it requires the
	// full production schema (ingestion_jobs from 000012) and must not be
	// applied to this minimal harness.
	for _, version := range []int{34} {
		migrations, err := loadMigrations()
		if err != nil {
			t.Fatalf("load migrations: %v", err)
		}
		for _, m := range migrations {
			if m.Version == version {
				if _, err := db.Exec(m.UpSQL); err != nil && !strings.Contains(err.Error(), "already exists") {
					t.Fatalf("migration %d: %v", version, err)
				}
			}
		}
	}
}

// R5-054: signatures are real ed25519 and verify against the stored preimage.
func TestResultSigningEd25519Verify(t *testing.T) {
	t.Setenv("RESULT_SIGNING_KEY", "")
	priv, pubHex := resultSigningKey()
	pub, err := hex.DecodeString(pubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("bad public key: %v", err)
	}
	preimage := resultSignaturePreimage("abc123", "", 42)
	sig := ed25519.Sign(priv, []byte(preimage))
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(preimage), sig) {
		t.Fatal("signature does not verify")
	}
	// Forgery check: a signature over a different preimage must not verify.
	if ed25519.Verify(ed25519.PublicKey(pub), []byte(resultSignaturePreimage("abc123", "", 43)), sig) {
		t.Fatal("signature verified against a tampered preimage")
	}
}

// R5-054: signature records are append-only — overwrite is rejected.
func TestResultSignatureOverwriteRejected(t *testing.T) {
	w4EnsureSchema(t)
	db.Exec(`DELETE FROM result_signatures WHERE result_id = 900001`)
	priv, pubHex := resultSigningKey()
	preimage := resultSignaturePreimage("hash-a", "", 7)
	sig := hex.EncodeToString(ed25519.Sign(priv, []byte(preimage)))
	insert := `INSERT INTO result_signatures (result_id, officer_pubkey, signature, prev_hash, result_hash, chain_position, algorithm, signer_id, signer_role, verification_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	if _, err := db.Exec(insert, 900001, pubHex, sig, "", "hash-a", 900001, "ed25519", 7, "admin", "verified"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Same result, different signature attempt — must be rejected.
	if _, err := db.Exec(insert, 900001, pubHex, sig, "", "hash-b", 900002, "ed25519", 7, "admin", "verified"); !isUniqueViolation(err) {
		t.Fatalf("expected unique violation on re-sign, got: %v", err)
	}
	// Append-only trigger: UPDATE and DELETE must be rejected.
	if _, err := db.Exec(`UPDATE result_signatures SET signature='forged' WHERE result_id=900001`); err == nil {
		t.Fatal("UPDATE on result_signatures succeeded — append-only trigger missing")
	}
	if _, err := db.Exec(`DELETE FROM result_signatures WHERE result_id=900001`); err == nil {
		t.Fatal("DELETE on result_signatures succeeded — append-only trigger missing")
	}
	db.Exec(`DROP TRIGGER IF EXISTS trg_result_signatures_no_delete ON result_signatures`)
	db.Exec(`DELETE FROM result_signatures WHERE result_id = 900001`)
	// restore trigger
	db.Exec(`CREATE TRIGGER trg_result_signatures_no_delete BEFORE DELETE ON result_signatures FOR EACH ROW EXECUTE FUNCTION reject_result_signature_mutation()`)
}

// R5-055: legal-hold tables are exported but never deleted; non-hold tables
// are archived (real file) then purged.
func TestRetentionLegalHoldAndRealArchive(t *testing.T) {
	w4EnsureSchema(t)
	dir := t.TempDir()
	t.Setenv("RETENTION_ARCHIVE_DIR", dir)
	t.Setenv("RETENTION_AUDIT_DAYS", "1") // everything older than 1 day is "expired" for the test
	t.Setenv("RETENTION_TRACKING_DAYS", "1")

	// Temporarily drop the audit_log no-delete trigger so we can clean up the
	// fixture afterwards; the retention worker itself must NOT delete.
	db.Exec(`DROP TRIGGER IF EXISTS trg_audit_log_no_delete ON audit_log`)
	db.Exec(`DELETE FROM audit_log WHERE action LIKE 'w4test%'`)
	db.Exec(`DELETE FROM official_tracking_history WHERE official_id=999001`)
	old := time.Now().AddDate(0, 0, -10).Format("2006-01-02 15:04:05")
	if _, err := db.Exec(`INSERT INTO audit_log (action, entity_type, entity_id, "timestamp") VALUES ('w4test-hold','test','1',$1)`, old); err != nil {
		t.Fatalf("seed audit_log: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO official_tracking_history (official_id, recorded_at) VALUES (999001, $1)`, old); err != nil {
		t.Fatalf("seed official_tracking_history: %v", err)
	}

	RunDataRetention(false)

	// Legal hold: audit_log row must STILL be present.
	var heldCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='w4test-hold'`).Scan(&heldCount); err != nil {
		t.Fatalf("count held: %v", err)
	}
	if heldCount != 1 {
		t.Fatalf("legal-hold row was deleted (count=%d) — retention must never purge audit_log", heldCount)
	}
	// But it must have been exported (export-then-mark) and registered.
	var holdArchiveRows int
	db.QueryRow(`SELECT COUNT(*) FROM retention_archive_log WHERE table_name='audit_log'`).Scan(&holdArchiveRows)
	if holdArchiveRows == 0 {
		t.Fatal("legal-hold export was not registered in retention_archive_log")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "audit_log", "*.jsonl")); len(matches) == 0 {
		t.Fatal("legal-hold export file missing — archive must be a real export")
	}

	// Non-hold ArchiveFirst table: official_tracking_history row must be
	// archived (real export) AND deleted.
	var trkCount int
	db.QueryRow(`SELECT COUNT(*) FROM official_tracking_history WHERE official_id=999001`).Scan(&trkCount)
	if trkCount != 0 {
		t.Fatalf("non-hold expired rows not purged (count=%d)", trkCount)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "official_tracking_history", "*.jsonl")); len(matches) == 0 {
		t.Fatal("non-hold archive file missing — delete must never happen without a real export")
	}

	// Cleanup fixture.
	db.Exec(`DELETE FROM audit_log WHERE action LIKE 'w4test%'`)
	db.Exec(`CREATE TRIGGER trg_audit_log_no_delete BEFORE DELETE ON audit_log FOR EACH ROW EXECUTE FUNCTION reject_audit_log_mutation()`)
	db.Exec(`DELETE FROM retention_archive_log WHERE table_name IN ('audit_log','official_tracking_history')`)
	_ = context.Background()
}

// R5-053/R5-056: audit_log is immutable at the database layer.
func TestAuditLogImmutable(t *testing.T) {
	w4EnsureSchema(t)
	db.Exec(`DROP TRIGGER IF EXISTS trg_audit_log_no_delete ON audit_log`)
	db.Exec(`DELETE FROM audit_log WHERE action='w4test-immutable'`)
	if _, err := db.Exec(`CREATE TRIGGER trg_audit_log_no_delete BEFORE DELETE ON audit_log FOR EACH ROW EXECUTE FUNCTION reject_audit_log_mutation()`); err != nil {
		t.Fatalf("recreate no-delete trigger: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO audit_log (action, entity_type) VALUES ('w4test-immutable','test')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Exec(`UPDATE audit_log SET details='tampered' WHERE action='w4test-immutable'`); err == nil {
		t.Fatal("UPDATE on audit_log succeeded — immutability trigger missing")
	}
	if _, err := db.Exec(`DELETE FROM audit_log WHERE action='w4test-immutable'`); err == nil {
		t.Fatal("DELETE on audit_log succeeded — immutability trigger missing")
	}
	// cleanup (drop trigger, delete, restore)
	db.Exec(`DROP TRIGGER IF EXISTS trg_audit_log_no_delete ON audit_log`)
	db.Exec(`DELETE FROM audit_log WHERE action='w4test-immutable'`)
	db.Exec(`CREATE TRIGGER trg_audit_log_no_delete BEFORE DELETE ON audit_log FOR EACH ROW EXECUTE FUNCTION reject_audit_log_mutation()`)
}

// R5-068: Merkle root changes when any child is dropped or substituted.
func TestMerkleRootCompleteness(t *testing.T) {
	children := []M{
		{"last_event_hash": "aaa"},
		{"last_event_hash": "bbb"},
		{"last_event_hash": "ccc"},
	}
	root := merkleRootEventHashes(children)
	if root == "" {
		t.Fatal("empty root")
	}
	if merkleRootEventHashes(children[:2]) == root {
		t.Fatal("dropping a child did not change the root — completeness not provable")
	}
	children[1]["last_event_hash"] = "xxx"
	if merkleRootEventHashes(children) == root {
		t.Fatal("substituting a child did not change the root")
	}
	// Order-independent (canonical sort).
	perm := []M{{"last_event_hash": "ccc"}, {"last_event_hash": "xxx"}, {"last_event_hash": "aaa"}}
	if merkleRootEventHashes(perm) != merkleRootEventHashes(children) {
		t.Fatal("root is not order-independent")
	}
}
