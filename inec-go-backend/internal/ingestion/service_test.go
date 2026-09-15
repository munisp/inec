// W1 (R5-002/R5-003) tests for the split ingestion service: client-supplied
// idempotency enforcement, DB-arbitrated dedupe (no re-processing), and
// in-flight-only queue accounting. PG-backed; skips unless R4_TEST_DB is set.
package ingestion

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

const svcScratchDBName = "w1_ingestion_svc"

var (
	svcScratchOnce sync.Once
	svcScratchDSN  string
	svcScratchErr  error
)

func svcProvision(baseDSN string) {
	svcScratchOnce.Do(func() {
		admin, err := sql.Open("postgres", baseDSN)
		if err != nil {
			svcScratchErr = err
			return
		}
		defer admin.Close()
		if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + svcScratchDBName + ` WITH (FORCE)`); err != nil {
			svcScratchErr = err
			return
		}
		if _, err := admin.Exec(`CREATE DATABASE ` + svcScratchDBName); err != nil {
			svcScratchErr = err
			return
		}
		// Retarget the admin DSN at the scratch database (keyword or URL form).
		base := os.Getenv("R4_TEST_DB")
		if strings.HasPrefix(base, "postgres://") || strings.HasPrefix(base, "postgresql://") {
			u, err := url.Parse(base)
			if err != nil {
				svcScratchErr = err
				return
			}
			u.Path = "/" + svcScratchDBName
			svcScratchDSN = u.String()
			return
		}
		fields := strings.Fields(base)
		replaced := false
		for i, f := range fields {
			if strings.HasPrefix(f, "dbname=") {
				fields[i] = "dbname=" + svcScratchDBName
				replaced = true
			}
		}
		if !replaced {
			fields = append(fields, "dbname="+svcScratchDBName)
		}
		svcScratchDSN = strings.Join(fields, " ")
	})
}

func svcTestDB(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("R4_TEST_DB") == "" {
		t.Skip("R4_TEST_DB not set — skipping PG-backed regression test")
	}
	svcProvision(os.Getenv("R4_TEST_DB"))
	if svcScratchErr != nil {
		t.Fatalf("provision: %v", svcScratchErr)
	}
	testDB, err := sql.Open("postgres", svcScratchDSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schema := `
	CREATE TABLE IF NOT EXISTS ingestion_jobs (
		id TEXT PRIMARY KEY,
		job_type TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		payload TEXT NOT NULL,
		idempotency_key TEXT UNIQUE,
		retries INTEGER DEFAULT 0,
		max_retries INTEGER DEFAULT 3,
		error_message TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		processed_at TIMESTAMP,
		latency_ms REAL
	);
	CREATE TABLE IF NOT EXISTS results (
		id SERIAL PRIMARY KEY,
		election_id INTEGER NOT NULL,
		polling_unit_code TEXT NOT NULL,
		presiding_officer_id INTEGER,
		status TEXT NOT NULL DEFAULT 'pending',
		total_valid_votes INTEGER DEFAULT 0,
		rejected_votes INTEGER DEFAULT 0,
		total_votes_cast INTEGER DEFAULT 0,
		accredited_voters INTEGER DEFAULT 0,
		tigerbeetle_transfer_id TEXT,
		tigerbeetle_status TEXT DEFAULT 'PENDING',
		hyperledger_status TEXT DEFAULT 'PENDING',
		submitted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(election_id, polling_unit_code)
	);
	CREATE TABLE IF NOT EXISTS result_party_scores (
		id SERIAL PRIMARY KEY,
		result_id INTEGER NOT NULL,
		party_code TEXT NOT NULL,
		votes INTEGER NOT NULL DEFAULT 0
	);
	`
	if _, err := testDB.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Cleanup(func() { testDB.Close() })
	return testDB
}

func resultPayload(pu string) map[string]interface{} {
	return map[string]interface{}{
		"election_id":       float64(1),
		"polling_unit_code": pu,
		"party_scores": []interface{}{
			map[string]interface{}{"party_code": "APC", "votes": float64(10)},
			map[string]interface{}{"party_code": "PDP", "votes": float64(20)},
		},
		"accredited_voters": float64(100),
		"rejected_votes":    float64(1),
	}
}

func TestSvcEnqueueRequiresClientIdempotencyKey(t *testing.T) {
	testDB := svcTestDB(t)
	svc := NewService(testDB)
	if _, _, err := svc.Enqueue(context.Background(), "result_import", resultPayload("PU-S1"), ""); err == nil {
		t.Fatal("R5-002: empty idempotency key accepted (nanotime-style auto keys must be rejected)")
	}
}

func TestSvcEnqueueDuplicateNotReprocessed(t *testing.T) {
	testDB := svcTestDB(t)
	svc := NewService(testDB)

	if _, dup, err := svc.Enqueue(context.Background(), "result_import", resultPayload("PU-S2"), "svc-key-1"); err != nil || dup {
		t.Fatalf("first enqueue: dup=%v err=%v", dup, err)
	}
	shutdownSvc(t, svc)

	// Same key again — possibly "another replica" (fresh service, empty memory).
	svc2 := NewService(testDB)
	job, dup, err := svc2.Enqueue(context.Background(), "result_import", resultPayload("PU-S2"), "svc-key-1")
	if err != nil || !dup {
		t.Fatalf("retry enqueue: dup=%v err=%v", dup, err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("expected existing completed job, got %s", job.Status)
	}
	shutdownSvc(t, svc2)

	var count int
	testDB.QueryRow("SELECT COUNT(*) FROM results WHERE polling_unit_code='PU-S2'").Scan(&count)
	if count != 1 {
		t.Fatalf("R5-002 regression: duplicate key re-processed (%d result rows)", count)
	}
}

func TestSvcResultImportActuallyWritesResults(t *testing.T) {
	testDB := svcTestDB(t)
	svc := NewService(testDB)
	if _, _, err := svc.Enqueue(context.Background(), "result_import", resultPayload("PU-S3"), "svc-key-2"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	shutdownSvc(t, svc)

	var totalValid, accredited int
	if err := testDB.QueryRow("SELECT total_valid_votes, accredited_voters FROM results WHERE polling_unit_code='PU-S3'").Scan(&totalValid, &accredited); err != nil {
		t.Fatalf("R5-001 regression: result_import wrote no results row: %v", err)
	}
	if totalValid != 30 || accredited != 100 {
		t.Fatalf("wrong figures: valid=%d accredited=%d", totalValid, accredited)
	}
	var scores int
	testDB.QueryRow("SELECT COUNT(*) FROM result_party_scores rps JOIN results r ON r.id=rps.result_id WHERE r.polling_unit_code='PU-S3'").Scan(&scores)
	if scores != 2 {
		t.Fatalf("party scores not written: %d", scores)
	}
}

func TestSvcRecoverPendingDrainsAndResetsStale(t *testing.T) {
	testDB := svcTestDB(t)
	for i := 0; i < 5; i++ {
		status := "pending"
		if i == 4 {
			status = "in_progress" // stale — must be rescued
		}
		payload := fmt.Sprintf(`{"election_id":1,"polling_unit_code":"PU-R%d","party_scores":[{"party_code":"APC","votes":1}],"accredited_voters":10,"rejected_votes":0}`, i)
		if _, err := testDB.Exec(
			"INSERT INTO ingestion_jobs (id, job_type, status, payload, idempotency_key, max_retries) VALUES ($1,$2,$3,$4,$5,3)",
			fmt.Sprintf("job_rec_%d", i), "result_import", status, payload, fmt.Sprintf("svc-rec-%d", i)); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	svc := NewService(testDB)
	if n := svc.RecoverPending(context.Background()); n != 5 {
		t.Fatalf("recovered %d jobs, want 5", n)
	}
	shutdownSvc(t, svc)

	var incomplete int
	testDB.QueryRow("SELECT COUNT(*) FROM ingestion_jobs WHERE status IN ('pending','in_progress')").Scan(&incomplete)
	if incomplete != 0 {
		t.Fatalf("R5-003 regression: %d jobs stranded after recovery", incomplete)
	}
	var applied int
	testDB.QueryRow("SELECT COUNT(*) FROM results WHERE polling_unit_code LIKE 'PU-R%'").Scan(&applied)
	if applied != 5 {
		t.Fatalf("expected 5 recovered results applied, got %d", applied)
	}
}

// shutdownSvc drains the service with a timeout and fails the test on timeout.
func shutdownSvc(t *testing.T, svc *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown drain: %v", err)
	}
}
