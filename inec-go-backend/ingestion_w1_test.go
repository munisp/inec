// W1 (R5-001..R5-010) regression tests for the ingestion apply path,
// idempotency, queue accounting, recovery, and offline time windows.
// PG-backed tests skip unless R4_TEST_DB is set (same convention as r4 suites).
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const w1ScratchDBName = "w1_ingestion"

var (
	w1ScratchOnce sync.Once
	w1ScratchDSN  string
	w1ScratchErr  error
)

func w1ProvisionScratch(baseDSN string) {
	w1ScratchOnce.Do(func() {
		admin, err := sql.Open("postgres", baseDSN)
		if err != nil {
			w1ScratchErr = fmt.Errorf("open admin: %w", err)
			return
		}
		defer admin.Close()
		if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + w1ScratchDBName + ` WITH (FORCE)`); err != nil {
			w1ScratchErr = fmt.Errorf("drop scratch db: %w", err)
			return
		}
		if _, err := admin.Exec(`CREATE DATABASE ` + w1ScratchDBName); err != nil {
			w1ScratchErr = fmt.Errorf("create scratch db: %w", err)
			return
		}
		dsn, err := r4WithDBName(baseDSN, w1ScratchDBName)
		if err != nil {
			w1ScratchErr = err
			return
		}
		raw, err := sql.Open("postgres", dsn)
		if err != nil {
			w1ScratchErr = fmt.Errorf("open scratch: %w", err)
			return
		}
		defer raw.Close()
		for _, f := range []string{
			"migrations/000001_initial_schema.up.sql",     // elections, polling_units, results, parties
			"migrations/000006_bvas_extended.up.sql",      // bvas_accreditations
			"migrations/000012_api_integrations.up.sql",   // ingestion_jobs, offline_sync_queue, dead_letter_queue
			"migrations/000021_core_extended.up.sql",      // audit_log
			"migrations/000022_election_management.up.sql", // result_party_scores
			"migrations/000028_results_unique_constraint.up.sql",
			"../migrations/000019_election_evidence_integrity.sql", // result_evidence_events
			"migrations/000023_biometric.up.sql",                   // biometric_profiles, offline_enrollment_queue
			"migrations/000033_ingestion_apply_and_idempotency.up.sql",
		} {
			ddl, err := os.ReadFile(f)
			if err != nil {
				w1ScratchErr = fmt.Errorf("read %s: %w", f, err)
				return
			}
			if _, err := raw.Exec(string(ddl)); err != nil {
				w1ScratchErr = fmt.Errorf("apply %s: %w", f, err)
				return
			}
		}
		// Dev-parity init (no-op on already-migrated tables; CREATE IF NOT EXISTS).
		compat := openPgCompat(dsn)
		defer compat.Close()
		initIngestionTables(compat)
		// Seed: one active election, one PU, parties.
		seed := `
		INSERT INTO parties (code, name, abbreviation) VALUES ('APC','All Progressives','APC'),('PDP','Peoples Democratic','PDP') ON CONFLICT (code) DO NOTHING;
		INSERT INTO elections (id, title, election_type, election_date, status) VALUES (1,'W1 Test','presidential','2027-01-01','active') ON CONFLICT DO NOTHING;
		INSERT INTO polling_units (code, name, ward_code, registered_voters) VALUES
			('PU-W1-001','W1 PU 1','W-001',1000),
			('PU-W1-002','W1 PU 2','W-001',1000),
			('PU-W1-003','W1 PU 3','W-001',1000),
			('PU-W1-004','W1 PU 4','W-001',1000),
			('PU-W1-005','W1 PU 5','W-001',1000),
			('PU-W1-006','W1 PU 6','W-001',1000),
			('PU-W1-007','W1 PU 7','W-001',1000),
			('PU-W1-008','W1 PU 8','W-001',1000),
			('PU-W1-R01','W1 PU R1','W-001',1000),
			('PU-W1-R02','W1 PU R2','W-001',1000),
			('PU-W1-R03','W1 PU R3','W-001',1000),
			('PU-W1-R04','W1 PU R4','W-001',1000),
			('PU-W1-R05','W1 PU R5','W-001',1000),
			('PU-W1-R06','W1 PU R6','W-001',1000),
			('PU-W1-R07','W1 PU R7','W-001',1000),
			('PU-W1-R08','W1 PU R8','W-001',1000)
		ON CONFLICT (code) DO NOTHING;
		`
		if _, err := compat.Exec(seed); err != nil {
			w1ScratchErr = fmt.Errorf("seed: %w", err)
			return
		}
		w1ScratchDSN = dsn
	})
}

// w1TestDB points the db globals at the W1 scratch database and resets the
// in-memory ingestion state.
func w1TestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("R4_TEST_DB")
	if dsn == "" {
		t.Skip("R4_TEST_DB not set — skipping PG-backed regression test")
	}
	w1ProvisionScratch(dsn)
	if w1ScratchErr != nil {
		t.Fatalf("provision scratch db: %v", w1ScratchErr)
	}
	testDB := openPgCompat(w1ScratchDSN)
	if err := testDB.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	prevDB, prevReader, prevWriter := db, dbReader, dbWriter
	db, dbReader, dbWriter = testDB, testDB, testDB
	if dbMetrics == nil {
		dbMetrics = newDBMetrics()
	}
	ingestionMu.Lock()
	ingestionQueue = nil
	idempotencyStore = make(map[string]string)
	ingestionMu.Unlock()
	t.Cleanup(func() {
		waitForIngestionDrain(15 * time.Second)
		db, dbReader, dbWriter = prevDB, prevReader, prevWriter
		testDB.Close()
	})
	return testDB
}

func w1ResultPayload(pu string, apc, pdp, accredited, rejected int) map[string]interface{} {
	return map[string]interface{}{
		"election_id":       1,
		"polling_unit_code": pu,
		"party_scores": []interface{}{
			map[string]interface{}{"party_code": "APC", "votes": float64(apc)},
			map[string]interface{}{"party_code": "PDP", "votes": float64(pdp)},
		},
		"accredited_voters": accredited,
		"rejected_votes":    rejected,
	}
}

// ─── R5-001: offline sync actually lands in results ─────────────────────────

func TestW1OfflineSyncAppliesResultToCanonicalStore(t *testing.T) {
	w1TestDB(t)
	payload := w1ResultPayload("PU-W1-001", 200, 250, 500, 10)
	payload["device_id"] = "BVAS-T1"
	payload["sync_type"] = "result"

	job := &IngestionJob{ID: "ING-T00001", Type: "offline_result_sync", Payload: payload, IdempotencyKey: "w1-key-001"}
	if err := processOfflineSync(job); err != nil {
		t.Fatalf("processOfflineSync: %v", err)
	}

	var totalValid, totalCast, accredited int
	var status string
	if err := db.QueryRow("SELECT total_valid_votes, total_votes_cast, accredited_voters, status FROM results WHERE election_id=1 AND polling_unit_code='PU-W1-001'").
		Scan(&totalValid, &totalCast, &accredited, &status); err != nil {
		t.Fatalf("result row missing after offline sync — R5-001 regression: %v", err)
	}
	if totalValid != 450 || totalCast != 460 || accredited != 500 {
		t.Fatalf("wrong figures: valid=%d cast=%d accredited=%d", totalValid, totalCast, accredited)
	}
	var apcVotes int
	if err := db.QueryRow("SELECT votes FROM result_party_scores rps JOIN results r ON r.id=rps.result_id WHERE r.polling_unit_code='PU-W1-001' AND rps.party_code='APC'").Scan(&apcVotes); err != nil || apcVotes != 200 {
		t.Fatalf("party scores missing: votes=%d err=%v", apcVotes, err)
	}
	var queueStatus string
	if err := db.QueryRow("SELECT status FROM offline_sync_queue WHERE idempotency_key='w1-key-001'").Scan(&queueStatus); err != nil || queueStatus != "synced" {
		t.Fatalf("queue row not synced: %q err=%v", queueStatus, err)
	}
	// Integrity evidence event recorded (canonical path parity).
	var evCount int
	db.QueryRow("SELECT COUNT(*) FROM result_evidence_events re JOIN results r ON r.id=re.result_id WHERE r.polling_unit_code='PU-W1-001' AND re.event_type='RESULT_SUBMITTED'").Scan(&evCount)
	if evCount != 1 {
		t.Fatalf("expected 1 integrity event, got %d", evCount)
	}
	// Audit row recorded.
	var auditCount int
	db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action='RESULT_SUBMITTED'").Scan(&auditCount)
	if auditCount == 0 {
		t.Fatal("no audit row for synced result")
	}
}

// ─── R5-001/R5-016-adjacent: idempotent apply, audited conflict ─────────────

func TestW1ApplyDuplicateAndConflict(t *testing.T) {
	w1TestDB(t)
	res, err := parseIngestedResult(w1ResultPayload("PU-W1-002", 100, 150, 400, 5))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res.Source = "offline_sync"

	outcome, _, err := applyIngestedResult(context.Background(), res)
	if err != nil || outcome != resultApplied {
		t.Fatalf("first apply: outcome=%s err=%v", outcome, err)
	}
	// Identical re-apply → duplicate, no second row.
	outcome, _, err = applyIngestedResult(context.Background(), res)
	if err != nil || outcome != resultDuplicate {
		t.Fatalf("duplicate apply: outcome=%s err=%v", outcome, err)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=1 AND polling_unit_code='PU-W1-002'").Scan(&count)
	if count != 1 {
		t.Fatalf("duplicate created %d rows", count)
	}
	// Divergent re-apply → conflict, original figures preserved, audit written.
	// (Figures stay EC8A-valid so the divergence reaches the conflict path.)
	conflicting, _ := parseIngestedResult(w1ResultPayload("PU-W1-002", 120, 150, 400, 5))
	conflicting.Source = "offline_sync"
	outcome, _, err = applyIngestedResult(context.Background(), conflicting)
	if err != nil || outcome != resultConflict {
		t.Fatalf("conflict apply: outcome=%s err=%v", outcome, err)
	}
	var totalValid int
	db.QueryRow("SELECT total_valid_votes FROM results WHERE election_id=1 AND polling_unit_code='PU-W1-002'").Scan(&totalValid)
	if totalValid != 250 {
		t.Fatalf("conflict overwrote first writer: valid=%d", totalValid)
	}
	var conflictAudits int
	db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action='RESULT_SYNC_CONFLICT'").Scan(&conflictAudits)
	if conflictAudits == 0 {
		t.Fatal("conflicting re-submission was not audit-logged")
	}
}

// ─── R5-002: DB-arbitrated idempotency, no process-after-ON-CONFLICT ────────

func TestW1EnqueueIdempotencyNoReprocess(t *testing.T) {
	w1TestDB(t)
	payload := w1ResultPayload("PU-W1-003", 10, 20, 100, 1)

	job, dup, err := enqueueJob("result_submission", payload, "w1-idem-A")
	if err != nil || dup {
		t.Fatalf("first enqueue: dup=%v err=%v", dup, err)
	}
	if !waitForIngestionDrain(15 * time.Second) {
		t.Fatal("drain timeout after first enqueue")
	}
	var applied int
	db.QueryRow("SELECT COUNT(*) FROM results WHERE polling_unit_code='PU-W1-003'").Scan(&applied)
	if applied != 1 {
		t.Fatalf("expected result applied once, got %d", applied)
	}

	// Retry with the same key → duplicate, no re-processing.
	_, dup, err = enqueueJob("result_submission", payload, "w1-idem-A")
	if err != nil || !dup {
		t.Fatalf("retry enqueue: dup=%v err=%v", dup, err)
	}
	if !waitForIngestionDrain(15 * time.Second) {
		t.Fatal("drain timeout after retry")
	}
	db.QueryRow("SELECT COUNT(*) FROM results WHERE polling_unit_code='PU-W1-003'").Scan(&applied)
	if applied != 1 {
		t.Fatalf("retry re-processed job: %d rows", applied)
	}

	// Simulate another replica/restart: row exists only in DB (nothing in
	// memory). Enqueue must return the existing job and NOT process it.
	payload2 := w1ResultPayload("PU-W1-004", 1, 2, 50, 0)
	payloadJSON := `{"election_id":1,"polling_unit_code":"PU-W1-004","party_scores":[{"party_code":"APC","votes":1},{"party_code":"PDP","votes":2}],"accredited_voters":50,"rejected_votes":0}`
	if _, err := db.Exec("INSERT INTO ingestion_jobs (id, job_type, status, payload, idempotency_key, max_retries) VALUES ('ING-X00001','result_submission','completed',?,'w1-idem-B',3)", payloadJSON); err != nil {
		t.Fatalf("seed external job: %v", err)
	}
	existing, dup, err := enqueueJob("result_submission", payload2, "w1-idem-B")
	if err != nil || !dup {
		t.Fatalf("external-key enqueue: dup=%v err=%v", dup, err)
	}
	if existing.ID != "ING-X00001" || existing.Status != "completed" {
		t.Fatalf("expected existing completed job returned, got %+v", existing)
	}
	if !waitForIngestionDrain(15 * time.Second) {
		t.Fatal("drain timeout after external-key enqueue")
	}
	var applied2 int
	db.QueryRow("SELECT COUNT(*) FROM results WHERE polling_unit_code='PU-W1-004'").Scan(&applied2)
	if applied2 != 0 {
		t.Fatal("R5-002 regression: job already in DB was processed again")
	}
	_ = job
}

// ─── R5-003: terminal jobs leave the in-memory queue; cap counts in-flight ──

func TestW1QueuePrunesTerminalJobs(t *testing.T) {
	w1TestDB(t)
	for i, pu := range []string{"PU-W1-005", "PU-W1-006", "PU-W1-007"} {
		if _, _, err := enqueueJob("result_submission", w1ResultPayload(pu, 5, 6, 50, 0), fmt.Sprintf("w1-prune-%d", i)); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if !waitForIngestionDrain(15 * time.Second) {
		t.Fatal("drain timeout")
	}
	ingestionMu.RLock()
	remaining := len(ingestionQueue)
	inFlight := 0
	for _, j := range ingestionQueue {
		if j.Status == "pending" || j.Status == "in_progress" {
			inFlight++
		}
	}
	ingestionMu.RUnlock()
	if remaining != 0 {
		t.Fatalf("R5-003 regression: %d terminal jobs still occupy the in-memory queue", remaining)
	}
	if inFlight != 0 {
		t.Fatalf("in-flight count wrong: %d", inFlight)
	}
	// And new enqueues are not rejected after 3 lifetime jobs (cap counts
	// in-flight only) — trivially true here; the regression would show as a
	// "queue full" error after maxIngestionQueueSize lifetime jobs.
	if _, _, err := enqueueJob("result_submission", w1ResultPayload("PU-W1-008", 5, 6, 50, 0), "w1-prune-after"); err != nil {
		t.Fatalf("enqueue after terminal prune rejected: %v", err)
	}
	if !waitForIngestionDrain(15 * time.Second) {
		t.Fatal("drain timeout after prune-after")
	}
}

// ─── R5-003: restart recovery drains ALL pending + resets stale in_progress ──

func TestW1RecoverPendingDrainsEverything(t *testing.T) {
	testDB := w1TestDB(t)
	// Clean slate for the jobs table so recovery sees only our fixtures.
	if _, err := testDB.Exec("DELETE FROM dead_letter_queue"); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec("DELETE FROM ingestion_jobs"); err != nil {
		t.Fatal(err)
	}
	// 7 pending + 1 stale in_progress on PUs dedicated to this test.
	pus := []string{"PU-W1-R01", "PU-W1-R02", "PU-W1-R03", "PU-W1-R04", "PU-W1-R05", "PU-W1-R06", "PU-W1-R07", "PU-W1-R08"}
	for i, pu := range pus {
		status := "pending"
		if i == 7 {
			status = "in_progress" // stale — must be rescued too
		}
		payload := fmt.Sprintf(`{"election_id":1,"polling_unit_code":%q,"party_scores":[{"party_code":"APC","votes":3},{"party_code":"PDP","votes":4}],"accredited_voters":50,"rejected_votes":0}`, pu)
		if _, err := testDB.Exec("INSERT INTO ingestion_jobs (id, job_type, status, payload, idempotency_key, max_retries) VALUES (?,?,?,?,?,3)",
			fmt.Sprintf("ING-R%05d", i), "result_submission", status, payload, fmt.Sprintf("w1-rec-%d", i)); err != nil {
			t.Fatalf("seed job %d: %v", i, err)
		}
	}
	// Ensure scratch has >1000-row semantics covered at small scale: recovery
	// must page until empty (loop), not stop at one page.
	recoverPendingJobs()
	if !waitForIngestionDrain(30 * time.Second) {
		t.Fatal("drain timeout during recovery")
	}
	var incomplete int
	testDB.QueryRow("SELECT COUNT(*) FROM ingestion_jobs WHERE status IN ('pending','in_progress')").Scan(&incomplete)
	if incomplete != 0 {
		t.Fatalf("R5-003 regression: %d jobs stranded after recovery", incomplete)
	}
	var applied int
	testDB.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=1 AND polling_unit_code LIKE 'PU-W1-R%'").Scan(&applied)
	if applied != 8 {
		t.Fatalf("expected 8 recovered results applied, got %d", applied)
	}
}

// ─── R5-004: device gateway offline backdate window ─────────────────────────

func w1GatewayEnvelope(observedAt time.Time) DeviceGatewayEnvelope {
	payload := []byte(`{"battery_level":80}`)
	h := sha256Sum(payload)
	return DeviceGatewayEnvelope{
		Version:         deviceGatewayEnvelopeVersion,
		DeviceID:        "BVAS-00001",
		ElectionID:      1,
		PollingUnitCode: "PU-W1-001",
		EventType:       "heartbeat",
		Sequence:        1,
		Nonce:           base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
		ObservedAt:      observedAt.UTC().Format(time.RFC3339),
		PayloadSHA256:   h,
		Payload:         payload,
		Signature:       base64.StdEncoding.EncodeToString(make([]byte, 64)),
	}
}

func TestW1GatewayOfflineBackdateWindow(t *testing.T) {
	// 2h-old capture (post-blackout sync) must be ACCEPTED.
	env := w1GatewayEnvelope(time.Now().Add(-2 * time.Hour))
	if _, _, _, err := validateDeviceGatewayEnvelope(&env); err != nil {
		t.Fatalf("R5-004: 2h-old offline capture rejected: %v", err)
	}
	// 71h-old still inside the bounded window.
	env = w1GatewayEnvelope(time.Now().Add(-71 * time.Hour))
	if _, _, _, err := validateDeviceGatewayEnvelope(&env); err != nil {
		t.Fatalf("71h-old capture inside 72h window rejected: %v", err)
	}
	// 100h-old is outside the window → rejected.
	env = w1GatewayEnvelope(time.Now().Add(-100 * time.Hour))
	if _, _, _, err := validateDeviceGatewayEnvelope(&env); err == nil {
		t.Fatal("100h-old capture accepted — backdate window unbounded")
	}
	// Future-dated beyond skew → rejected (anti-replay preserved).
	env = w1GatewayEnvelope(time.Now().Add(1 * time.Hour))
	if _, _, _, err := validateDeviceGatewayEnvelope(&env); err == nil {
		t.Fatal("future-dated capture accepted — anti-replay skew check removed")
	}
}

// ─── R5-006/R5-010: offline-sync item timestamp validation ──────────────────

func TestW1OfflineSyncTimestampValidation(t *testing.T) {
	if err := validateOfflineSyncTimestamp(""); err != nil {
		t.Fatalf("empty timestamp should be accepted (server stamps receipt): %v", err)
	}
	if err := validateOfflineSyncTimestamp(time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("2h-old timestamp rejected: %v", err)
	}
	if err := validateOfflineSyncTimestamp(time.Now().Add(-100 * time.Hour).UTC().Format(time.RFC3339)); err == nil {
		t.Fatal("100h-old timestamp accepted")
	}
	if err := validateOfflineSyncTimestamp(time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)); err == nil {
		t.Fatal("future timestamp accepted")
	}
	if err := validateOfflineSyncTimestamp("not-a-time"); err == nil {
		t.Fatal("garbage timestamp accepted")
	}
}

// ─── R5-002: deterministic idempotency keys ─────────────────────────────────

func TestW1IdempotencyKeyDeterministic(t *testing.T) {
	k1 := generateIdempotencyKey("result_submission", map[string]interface{}{"a": 1, "b": "x"})
	k2 := generateIdempotencyKey("result_submission", map[string]interface{}{"b": "x", "a": 1})
	if k1 != k2 {
		t.Fatal("same content produced different idempotency keys")
	}
	// Offline sync keys key on semantic identity, not the whole payload: an
	// unrelated field change must NOT create a new logical item.
	p1 := map[string]interface{}{"election_id": 1, "polling_unit_code": "PU-X", "notes": "v1"}
	p2 := map[string]interface{}{"election_id": 1, "polling_unit_code": "PU-X", "notes": "v2"}
	d1 := deriveOfflineSyncKey("BVAS-1", "result", p1)
	d2 := deriveOfflineSyncKey("BVAS-1", "result", p2)
	if d1 != d2 {
		t.Fatal("offline sync key changed on unrelated payload field — duplicates on retry")
	}
	d3 := deriveOfflineSyncKey("BVAS-1", "result", map[string]interface{}{"election_id": 1, "polling_unit_code": "PU-Y"})
	if d1 == d3 {
		t.Fatal("different PUs produced the same offline sync key")
	}
}

// ─── R5-001: accreditation sync applies with DB-level dedupe ────────────────

func TestW1AccreditationSyncAppliesAndDedupes(t *testing.T) {
	w1TestDB(t)
	hash := strings.Repeat("ab", 32)
	payload := map[string]interface{}{
		"device_id":          "BVAS-T9",
		"election_id":        1,
		"polling_unit_code":  "PU-W1-001",
		"voter_pvc_hash":     hash,
		"biometric_match":    true,
		"pvc_verified":       true,
		"method":             "biometric",
	}
	outcome, err := applyIngestedAccreditation(context.Background(), payload)
	if err != nil || outcome != resultApplied {
		t.Fatalf("first accreditation: outcome=%s err=%v", outcome, err)
	}
	outcome, err = applyIngestedAccreditation(context.Background(), payload)
	if err != nil || outcome != resultDuplicate {
		t.Fatalf("duplicate accreditation: outcome=%s err=%v", outcome, err)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM bvas_accreditations WHERE voter_pvc_hash=?", hash).Scan(&count)
	if count != 1 {
		t.Fatalf("duplicate accreditation rows: %d", count)
	}
}

// ─── R5-009: offline enrollment sync actually applies enrollments ───────────

func TestW1OfflineEnrollmentTriggerSyncApplies(t *testing.T) {
	testDB := w1TestDB(t)
	if _, err := testDB.Exec(`INSERT INTO offline_enrollment_queue (device_id, voter_vin, modality, template_data_hash)
		VALUES ('BVAS-E1','VIN-001','fingerprint','hash-aaa'), ('BVAS-E1','VIN-002','facial','hash-bbb')`); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	q := NewOfflineEnrollmentQueue(testDB)
	out := q.TriggerSync("BVAS-E1")
	if out["synced_count"].(int) != 2 || out["failed_count"].(int) != 0 {
		t.Fatalf("unexpected sync outcome: %+v", out)
	}
	// The enrollment must EXIST in biometric_profiles — not just a status flip.
	var fpHash string
	if err := testDB.QueryRow("SELECT fingerprint_hash FROM biometric_profiles WHERE voter_vin='VIN-001'").Scan(&fpHash); err != nil || fpHash != "hash-aaa" {
		t.Fatalf("R5-009 regression: enrollment not applied (hash=%q err=%v)", fpHash, err)
	}
	var faceHash string
	if err := testDB.QueryRow("SELECT facial_hash FROM biometric_profiles WHERE voter_vin='VIN-002'").Scan(&faceHash); err != nil || faceHash != "hash-bbb" {
		t.Fatalf("facial enrollment not applied: %v", err)
	}
	// Conflict: a different fingerprint template for the same voter is flagged,
	// never overwritten.
	if _, err := testDB.Exec(`INSERT INTO offline_enrollment_queue (device_id, voter_vin, modality, template_data_hash)
		VALUES ('BVAS-E1','VIN-001','fingerprint','hash-DIFFERENT')`); err != nil {
		t.Fatal(err)
	}
	out = q.TriggerSync("BVAS-E1")
	if out["conflict_count"].(int) != 1 {
		t.Fatalf("expected 1 conflict, got %+v", out)
	}
	testDB.QueryRow("SELECT fingerprint_hash FROM biometric_profiles WHERE voter_vin='VIN-001'").Scan(&fpHash)
	if fpHash != "hash-aaa" {
		t.Fatal("conflicting template overwrote the enrolled one")
	}
	var flagged int
	testDB.QueryRow("SELECT conflict_detected FROM offline_enrollment_queue WHERE template_data_hash='hash-DIFFERENT'").Scan(&flagged)
	if flagged != 1 {
		t.Fatal("conflict not flagged for manual review")
	}
}

func sha256Sum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
