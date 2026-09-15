package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// ── Robust Ingestion Engine ──
//
// R5-001..R5-010: every ingestion path now terminates in the canonical result
// write (results + result_party_scores + integrity evidence event + audit row),
// idempotent on (election_id, polling_unit_code). "Synced" means "applied to the
// canonical store", never "a tombstone row was written".

type IngestionJob struct {
	ID             string                 `json:"id"`
	Type           string                 `json:"type"`
	Status         string                 `json:"status"`
	Payload        map[string]interface{} `json:"payload"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Retries        int                    `json:"retries"`
	MaxRetries     int                    `json:"max_retries"`
	Error          string                 `json:"error,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	ProcessedAt    *time.Time             `json:"processed_at,omitempty"`
}

type DeadLetterEntry struct {
	ID          string                 `json:"id"`
	JobID       string                 `json:"job_id"`
	JobType     string                 `json:"job_type"`
	Error       string                 `json:"error"`
	Payload     map[string]interface{} `json:"payload"`
	FailedAt    time.Time              `json:"failed_at"`
	Reprocessed bool                   `json:"reprocessed"`
}

type IngestionStats struct {
	TotalJobs       int     `json:"total_jobs"`
	Processed       int     `json:"processed"`
	Failed          int     `json:"failed"`
	Pending         int     `json:"pending"`
	InProgress      int     `json:"in_progress"`
	DeadLetterCount int     `json:"dead_letter_count"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	Throughput      float64 `json:"throughput_per_sec"`
}

const (
	// maxIngestionQueueSize bounds IN-FLIGHT (pending/in_progress) jobs only.
	// Terminal jobs are pruned from the in-memory queue, so the cap no longer
	// counts lifetime jobs (R5-003).
	maxIngestionQueueSize = 10000
	// maxConcurrentJobProcessors bounds parallel job execution (incl. recovery).
	maxConcurrentJobProcessors = 32
	// ingestionRecoveryPageSize pages DB recovery; recovery loops until drained.
	ingestionRecoveryPageSize = 1000

	// offlineSyncMaxItems mirrors the /ingestion/batch cap (R5-006).
	offlineSyncMaxItems = 500
	// offlineSyncMaxItemAge bounds how far back an offline-captured item may be
	// dated (R5-004/R5-010): accredited offline capture is accepted up to 72h.
	offlineSyncMaxItemAge = 72 * time.Hour
	// offlineSyncMaxFutureSkew rejects items dated in the future (anti-replay).
	offlineSyncMaxFutureSkew = 10 * time.Minute
)

var (
	ingestionQueue   []IngestionJob
	deadLetterQueue  []DeadLetterEntry
	idempotencyStore = make(map[string]string)
	ingestionMu      sync.RWMutex
	ingestionNextID  int64 = 1
	dlqNextID        int64 = 1

	ingestionProcessed int64
	ingestionFailed    int64
	ingestionStartTime = time.Now()

	// ingestionProcSem bounds concurrent job processors; ingestionWg tracks
	// detached job goroutines so shutdown can drain them (R5-008).
	ingestionProcSem = make(chan struct{}, maxConcurrentJobProcessors)
	ingestionWg      sync.WaitGroup
)

func initIngestionTables(database *sql.DB) {
	schema := `
	CREATE TABLE IF NOT EXISTS ingestion_jobs (
		id TEXT PRIMARY KEY,
		job_type TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','in_progress','completed','failed','dead_letter')),
		payload TEXT NOT NULL,
		idempotency_key TEXT UNIQUE,
		retries INTEGER DEFAULT 0,
		max_retries INTEGER DEFAULT 3,
		error_message TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		processed_at TIMESTAMP,
		latency_ms REAL
	);
	CREATE TABLE IF NOT EXISTS dead_letter_queue (
		id TEXT PRIMARY KEY,
		job_id TEXT NOT NULL,
		job_type TEXT NOT NULL,
		error_message TEXT NOT NULL,
		payload TEXT NOT NULL,
		failed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		reprocessed INTEGER DEFAULT 0,
		reprocessed_at TIMESTAMP,
		FOREIGN KEY (job_id) REFERENCES ingestion_jobs(id)
	);
	CREATE TABLE IF NOT EXISTS offline_sync_queue (
		id SERIAL PRIMARY KEY,
		device_id TEXT NOT NULL,
		sync_type TEXT NOT NULL CHECK(sync_type IN ('result','accreditation','incident')),
		payload TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','syncing','synced','failed')),
		idempotency_key TEXT UNIQUE,
		error_message TEXT,
		result_id INTEGER,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		synced_at TIMESTAMP,
		retries INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_ingestion_status ON ingestion_jobs(status);
	CREATE INDEX IF NOT EXISTS idx_ingestion_idem ON ingestion_jobs(idempotency_key);
	CREATE INDEX IF NOT EXISTS idx_dlq_reprocessed ON dead_letter_queue(reprocessed);
	CREATE INDEX IF NOT EXISTS idx_offline_status ON offline_sync_queue(status);
	CREATE INDEX IF NOT EXISTS idx_offline_idem ON offline_sync_queue(idempotency_key);
	`
	execMulti(database, schema)
}

// canonicalPayloadJSON renders a payload deterministically (encoding/json sorts
// map keys), so identical content always hashes to the same idempotency key.
func canonicalPayloadJSON(payload map[string]interface{}) string {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("%v", payload)
	}
	return string(b)
}

// generateIdempotencyKey derives a deterministic key from job type + canonical
// payload content. Used only when the caller supplies no key; the derived key
// is always returned to the caller so retries can reuse it explicitly.
func generateIdempotencyKey(jobType string, payload map[string]interface{}) string {
	h := sha256.Sum256([]byte(jobType + ":" + canonicalPayloadJSON(payload)))
	return hex.EncodeToString(h[:16])
}

// payloadString / payloadInt tolerate JSON-decoded numbers (float64) and strings.
func payloadString(payload map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := payload[k]; ok {
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					return strings.TrimSpace(t)
				}
			case float64:
				return strconv.FormatFloat(t, 'f', -1, 64)
			}
		}
	}
	return ""
}

func payloadInt(payload map[string]interface{}, keys ...string) (int, bool) {
	for _, k := range keys {
		if v, ok := payload[k]; ok {
			switch t := v.(type) {
			case float64:
				return int(t), true
			case int:
				return t, true
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
					return n, true
				}
			}
		}
	}
	return 0, false
}

// deriveOfflineSyncKey builds a stable idempotency key for an offline-sync item.
// It prefers the semantic identity of the item (election + polling unit [+ voter])
// over a whole-payload hash, so re-syncing the same capture after an unrelated
// field change is still deduplicated (R5-002).
func deriveOfflineSyncKey(deviceID, syncType string, payload map[string]interface{}) string {
	electionID, _ := payloadInt(payload, "election_id")
	puCode := payloadString(payload, "polling_unit_code", "pu_code")
	var identity string
	switch syncType {
	case "result":
		identity = fmt.Sprintf("result:%d:%s", electionID, puCode)
	case "accreditation":
		voter := payloadString(payload, "voter_pvc_hash", "voter_vin")
		identity = fmt.Sprintf("accreditation:%d:%s:%s", electionID, puCode, voter)
	default:
		identity = syncType + ":" + canonicalPayloadJSON(payload)
	}
	h := sha256.Sum256([]byte("offline-sync:" + deviceID + ":" + identity))
	return hex.EncodeToString(h[:16])
}

// ── Startup recovery (R5-003): drain ALL pending jobs, reset stale in_progress ──

func recoverPendingJobs() {
	// Jobs left 'in_progress' by a crashed process would otherwise be stranded
	// forever (recovery previously only rescued 'pending' and capped at 1000).
	if _, err := db.Exec("UPDATE ingestion_jobs SET status='pending' WHERE status='in_progress'"); err != nil {
		log.Warn().Err(err).Msg("ingestion: failed to reset stale in_progress jobs")
	}

	// Seed the ID counter past any persisted job ID so restarts cannot collide.
	var maxID sql.NullString
	db.QueryRow("SELECT MAX(id) FROM ingestion_jobs WHERE id LIKE 'ING-%'").Scan(&maxID)
	if maxID.Valid {
		if n, err := strconv.ParseInt(strings.TrimPrefix(maxID.String, "ING-"), 10, 64); err == nil && n >= ingestionNextID {
			ingestionNextID = n + 1
		}
	}

	// Keyset pagination over the immutable job id: processors mutate statuses
	// concurrently, so OFFSET paging could skip rows.
	recovered := 0
	lastID := ""
	for {
		rows, err := db.Query(
			"SELECT id, job_type, payload, idempotency_key, retries, max_retries FROM ingestion_jobs WHERE status = 'pending' AND id > ? ORDER BY id ASC LIMIT ?",
			lastID, ingestionRecoveryPageSize)
		if err != nil {
			log.Warn().Err(err).Msg("ingestion: failed to recover pending jobs from DB")
			return
		}
		page := 0
		for rows.Next() {
			var id, jobType, payloadStr string
			var idemKey sql.NullString
			var retries, maxRetries int
			if err := rows.Scan(&id, &jobType, &payloadStr, &idemKey, &retries, &maxRetries); err != nil {
				continue
			}
			page++
			lastID = id
			var payload map[string]interface{}
			json.Unmarshal([]byte(payloadStr), &payload)

			ingestionMu.Lock()
			_, alreadyQueued := idempotencyStore[idemKey.String]
			if idemKey.Valid && idemKey.String != "" && alreadyQueued {
				ingestionMu.Unlock()
				continue
			}
			job := IngestionJob{
				ID: id, Type: jobType, Status: "pending", Payload: payload,
				IdempotencyKey: idemKey.String, Retries: retries, MaxRetries: maxRetries,
				CreatedAt: time.Now(),
			}
			ingestionQueue = append(ingestionQueue, job)
			if idemKey.Valid && idemKey.String != "" {
				idempotencyStore[idemKey.String] = id
			}
			ingestionMu.Unlock()
			recovered++
			runProcessJob(id)
		}
		rows.Close()
		if page < ingestionRecoveryPageSize {
			break
		}
	}
	if recovered > 0 {
		log.Info().Int("count", recovered).Msg("ingestion: recovered pending jobs from DB")
	}
}

// runProcessJob launches a tracked, concurrency-bounded job processor (R5-008).
func runProcessJob(jobID string) {
	ingestionWg.Add(1)
	go func() {
		defer ingestionWg.Done()
		ingestionProcSem <- struct{}{}
		defer func() { <-ingestionProcSem }()
		processJob(jobID)
	}()
}

// waitForIngestionDrain blocks until all in-flight job goroutines finish or the
// timeout elapses. Wired into graceful shutdown (see main.go handoff — R5-008).
func waitForIngestionDrain(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		ingestionWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// countInFlightLocked counts non-terminal jobs; the caller must hold ingestionMu.
func countInFlightLocked() int {
	n := 0
	for _, j := range ingestionQueue {
		if j.Status == "pending" || j.Status == "in_progress" {
			n++
		}
	}
	return n
}

// removeJobFromQueueLocked drops a terminal job from the in-memory queue; the
// database remains the source of truth for history and idempotency (R5-003).
func removeJobFromQueueLocked(jobID string) {
	for i := range ingestionQueue {
		if ingestionQueue[i].ID == jobID {
			ingestionQueue = append(ingestionQueue[:i], ingestionQueue[i+1:]...)
			return
		}
	}
}

// enqueueJob persists and schedules a job. It returns (job, duplicate, error):
// duplicate=true means the idempotency key already exists and the EXISTING job
// is returned without re-processing (R5-002 — no process-after-ON-CONFLICT).
func enqueueJob(jobType string, payload map[string]interface{}, idempotencyKey string) (*IngestionJob, bool, error) {
	if idempotencyKey == "" {
		// Explicit deterministic derivation, returned to the caller — never a
		// nanotime nonce that makes every retry a new job (R5-002).
		idempotencyKey = generateIdempotencyKey(jobType, payload)
	}

	ingestionMu.Lock()
	if existingID, ok := idempotencyStore[idempotencyKey]; ok {
		for i := range ingestionQueue {
			if ingestionQueue[i].ID == existingID {
				job := ingestionQueue[i]
				ingestionMu.Unlock()
				return &job, true, nil
			}
		}
	}
	inFlight := countInFlightLocked()
	ingestionMu.Unlock()

	// Backpressure: cap IN-FLIGHT jobs only (terminal jobs are pruned).
	if inFlight >= maxIngestionQueueSize {
		return nil, false, fmt.Errorf("ingestion queue full (%d jobs in flight) — try again later", maxIngestionQueueSize)
	}

	ingestionMu.Lock()
	id := fmt.Sprintf("ING-%06d", ingestionNextID)
	ingestionNextID++
	ingestionMu.Unlock()

	job := IngestionJob{
		ID:             id,
		Type:           jobType,
		Status:         "pending",
		Payload:        payload,
		IdempotencyKey: idempotencyKey,
		MaxRetries:     3,
		CreatedAt:      time.Now(),
	}

	payloadJSON, _ := json.Marshal(payload)
	// The database is the idempotency arbiter. If the key already exists
	// (restart, another replica, earlier completed job), nothing is inserted
	// and we must NOT process again — return the persisted job instead.
	res, err := db.Exec(
		"INSERT INTO ingestion_jobs (id, job_type, status, payload, idempotency_key, max_retries) VALUES (?,?,?,?,?,?) ON CONFLICT (idempotency_key) DO NOTHING",
		id, jobType, "pending", string(payloadJSON), idempotencyKey, 3)
	if err != nil {
		return nil, false, fmt.Errorf("persist ingestion job: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		var existing IngestionJob
		var payloadStr string
		var processedAt sql.NullTime
		rowErr := db.QueryRow(
			"SELECT id, job_type, status, payload, idempotency_key, retries, max_retries, processed_at FROM ingestion_jobs WHERE idempotency_key=?",
			idempotencyKey).Scan(&existing.ID, &existing.Type, &existing.Status, &payloadStr, &existing.IdempotencyKey, &existing.Retries, &existing.MaxRetries, &processedAt)
		if rowErr != nil {
			return nil, false, fmt.Errorf("load existing ingestion job for idempotency key: %w", rowErr)
		}
		json.Unmarshal([]byte(payloadStr), &existing.Payload)
		if processedAt.Valid {
			t := processedAt.Time
			existing.ProcessedAt = &t
		}
		return &existing, true, nil
	}

	ingestionMu.Lock()
	ingestionQueue = append(ingestionQueue, job)
	idempotencyStore[idempotencyKey] = id
	ingestionMu.Unlock()

	runProcessJob(id)
	return &job, false, nil
}

func processJob(jobID string) {
	ingestionMu.Lock()
	var job *IngestionJob
	for i := range ingestionQueue {
		if ingestionQueue[i].ID == jobID {
			ingestionQueue[i].Status = "in_progress"
			job = &ingestionQueue[i]
			break
		}
	}
	ingestionMu.Unlock()

	if job == nil {
		return
	}

	dbExecLog("ingestion_jobs", "UPDATE ingestion_jobs SET status='in_progress' WHERE id=?", jobID)
	start := time.Now()

	var err error
	for attempt := 0; attempt <= job.MaxRetries; attempt++ {
		err = executeIngestionJob(job)
		if err == nil {
			break
		}
		job.Retries = attempt + 1
		backoff := time.Duration(1<<uint(attempt)) * 100 * time.Millisecond
		time.Sleep(backoff)
	}

	latency := time.Since(start).Milliseconds()

	ingestionMu.Lock()
	if err != nil {
		job.Status = "dead_letter"
		job.Error = err.Error()
		ingestionFailed++

		dlID := fmt.Sprintf("DLQ-%06d", dlqNextID)
		dlqNextID++
		deadLetterQueue = append(deadLetterQueue, DeadLetterEntry{
			ID: dlID, JobID: jobID, JobType: job.Type, Error: err.Error(),
			Payload: job.Payload, FailedAt: time.Now(),
		})
		payloadJSON, _ := json.Marshal(job.Payload)
		dbExecLog("dead_letter_queue", "INSERT INTO dead_letter_queue (id, job_id, job_type, error_message, payload) VALUES (?,?,?,?,?)",
			dlID, jobID, job.Type, err.Error(), string(payloadJSON))
		dbExecLog("ingestion_jobs", "UPDATE ingestion_jobs SET status='dead_letter', error_message=?, retries=?, latency_ms=? WHERE id=?",
			err.Error(), job.Retries, float64(latency), jobID)
	} else {
		job.Status = "completed"
		now := time.Now()
		job.ProcessedAt = &now
		ingestionProcessed++
		dbExecLog("ingestion_jobs", "UPDATE ingestion_jobs SET status='completed', processed_at=CURRENT_TIMESTAMP, retries=?, latency_ms=? WHERE id=?",
			job.Retries, float64(latency), jobID)
	}
	// Terminal jobs leave the in-memory queue; history + idempotency live in the DB.
	delete(idempotencyStore, job.IdempotencyKey)
	removeJobFromQueueLocked(jobID)
	ingestionMu.Unlock()
}

func executeIngestionJob(job *IngestionJob) error {
	switch job.Type {
	case "result_submission":
		return processResultIngestion(job)
	case "batch_result_upload":
		return processBatchResultIngestion(job)
	case "accreditation_sync":
		return processAccreditationSync(job)
	case "offline_result_sync":
		return processOfflineSync(job)
	default:
		return fmt.Errorf("unknown job type: %s", job.Type)
	}
}

// ── Canonical apply path (R5-001) ──
//
// applyResultTx is the shared, idempotent result-write used by live-equivalent
// ingestion paths (offline sync, batch upload, device gateway result_capture).
// It mirrors handleSubmitResult's canonical transaction: polling-unit lookup,
// election-state gate, EC8A validation, results insert keyed on
// (election_id, polling_unit_code), party scores, and the immutable integrity
// evidence event — plus an audit row written by the caller wrapper.

type resultApplyOutcome string

const (
	resultApplied   resultApplyOutcome = "applied"
	resultDuplicate resultApplyOutcome = "duplicate" // identical figures already recorded
	resultConflict  resultApplyOutcome = "conflict"  // different figures already recorded
)

type ingestedResult struct {
	ElectionID       int
	PollingUnitCode  string
	PartyScores      []PartyVoteEntry
	AccreditedVoters int
	RejectedVotes    int
	SubmittedBy      int    // 0 when device-originated (no user identity)
	Source           string // offline_sync | batch | device_gateway | ingestion_api
	SourceRef        string // device_id / batch_id / correlation id
}

// parseIngestedResult extracts and validates a result payload from a job map.
func parseIngestedResult(payload map[string]interface{}) (ingestedResult, error) {
	var res ingestedResult
	electionID, ok := payloadInt(payload, "election_id")
	if !ok || electionID <= 0 {
		return res, fmt.Errorf("election_id is required")
	}
	res.ElectionID = electionID
	res.PollingUnitCode = payloadString(payload, "polling_unit_code", "pu_code")
	if res.PollingUnitCode == "" {
		return res, fmt.Errorf("polling_unit_code is required")
	}
	res.AccreditedVoters, _ = payloadInt(payload, "accredited_voters")
	res.RejectedVotes, _ = payloadInt(payload, "rejected_votes")
	res.SubmittedBy, _ = payloadInt(payload, "submitted_by")
	res.Source = payloadString(payload, "source")
	res.SourceRef = payloadString(payload, "source_ref", "device_id", "batch_id")

	// party_scores may arrive JSON-decoded ([]interface{}) or as a typed slice
	// when constructed in-process (batch upload) — normalize via JSON.
	var rawScores []interface{}
	switch v := payload["party_scores"].(type) {
	case []interface{}:
		rawScores = v
	default:
		b, err := json.Marshal(v)
		if err != nil || json.Unmarshal(b, &rawScores) != nil {
			rawScores = nil
		}
	}
	if len(rawScores) == 0 {
		return res, fmt.Errorf("party_scores is required")
	}
	for _, raw := range rawScores {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			return res, fmt.Errorf("party_scores entries must be objects")
		}
		code := payloadString(entry, "party_code")
		votes, ok := payloadInt(entry, "votes")
		if code == "" || !ok || votes < 0 {
			return res, fmt.Errorf("party_scores entries require party_code and non-negative votes")
		}
		res.PartyScores = append(res.PartyScores, PartyVoteEntry{PartyCode: code, Votes: votes})
	}
	if res.AccreditedVoters < 0 || res.RejectedVotes < 0 {
		return res, fmt.Errorf("accredited_voters and rejected_votes must be non-negative")
	}
	return res, nil
}

// applyResultTx applies one result inside tx. Idempotent on
// (election_id, polling_unit_code): identical re-applies are duplicates,
// divergent re-applies are conflicts (first-writer-wins, never overwritten).
func applyResultTx(ctx context.Context, tx *sql.Tx, res ingestedResult) (resultApplyOutcome, int64, error) {
	var regVoters int
	if err := tx.QueryRowContext(ctx, convertPlaceholders(
		"SELECT registered_voters FROM polling_units WHERE code=?"), res.PollingUnitCode).Scan(&regVoters); err != nil {
		return "", 0, fmt.Errorf("polling unit not found: %s", res.PollingUnitCode)
	}

	// Offline captures legitimately arrive while the election is open or being
	// collated; terminal elections never accept new results. (W2 owns the FSM;
	// keep this gate aligned with its lifecycle fix for R5-012.)
	var electionStatus string
	if err := tx.QueryRowContext(ctx, convertPlaceholders(
		"SELECT status FROM elections WHERE id=?"), res.ElectionID).Scan(&electionStatus); err != nil {
		return "", 0, fmt.Errorf("election not found: %d", res.ElectionID)
	}
	if electionStatus != "active" && electionStatus != "voting" && electionStatus != "collating" {
		return "", 0, fmt.Errorf("election %d is not accepting results (status=%s)", res.ElectionID, electionStatus)
	}

	totalValid := 0
	for _, ps := range res.PartyScores {
		totalValid += ps.Votes
	}
	totalCast := totalValid + res.RejectedVotes

	ec8aForm := &FormEC8A{
		ElectionID:       res.ElectionID,
		PollingUnitCode:  res.PollingUnitCode,
		RegisteredVoters: regVoters,
		AccreditedVoters: res.AccreditedVoters,
		TotalVotesPolled: totalCast,
		RejectedBallots:  res.RejectedVotes,
		TotalValidVotes:  totalValid,
		PartyResults:     res.PartyScores,
	}
	if violations := ValidateEC8A(ec8aForm); len(violations) > 0 {
		return "", 0, fmt.Errorf("EC8A validation failed: %s", strings.Join(violations, "; "))
	}

	ec8aHash := computeEC8AHash(res.ElectionID, res.PollingUnitCode, res.PartyScores, res.AccreditedVoters, res.RejectedVotes)

	insertRes, err := tx.ExecContext(ctx, convertPlaceholders(`INSERT INTO results
		(election_id, polling_unit_code, presiding_officer_id, status,
		 total_valid_votes, rejected_votes, total_votes_cast, accredited_voters,
		 ec8a_hash, tigerbeetle_transfer_id, tigerbeetle_status, hyperledger_status)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (election_id, polling_unit_code) DO NOTHING`),
		res.ElectionID, res.PollingUnitCode, nullableInt(res.SubmittedBy), "pending",
		totalValid, res.RejectedVotes, totalCast, res.AccreditedVoters,
		ec8aHash, nil, "NOT_APPLICABLE", "PENDING")
	if err != nil {
		return "", 0, fmt.Errorf("insert result: %w", err)
	}

	var resultID int64
	var existingHash sql.NullString
	if affected, _ := insertRes.RowsAffected(); affected == 0 {
		// A result already exists for this (election, PU). Idempotent retry with
		// identical figures → duplicate (safe no-op). Divergent figures → conflict:
		// first-writer-wins, nothing overwritten, caller records an audit row.
		if err := tx.QueryRowContext(ctx, convertPlaceholders(
			"SELECT id, ec8a_hash FROM results WHERE election_id=? AND polling_unit_code=?"),
			res.ElectionID, res.PollingUnitCode).Scan(&resultID, &existingHash); err != nil {
			return "", 0, fmt.Errorf("load existing result: %w", err)
		}
		if existingHash.Valid && existingHash.String == ec8aHash {
			return resultDuplicate, resultID, nil
		}
		return resultConflict, resultID, nil
	}

	if err := tx.QueryRowContext(ctx, convertPlaceholders(
		"SELECT id FROM results WHERE election_id=? AND polling_unit_code=?"),
		res.ElectionID, res.PollingUnitCode).Scan(&resultID); err != nil {
		return "", 0, fmt.Errorf("load inserted result id: %w", err)
	}

	scores := make([]struct {
		PartyCode string `json:"party_code"`
		Votes     int    `json:"votes"`
	}, len(res.PartyScores))
	for i, ps := range res.PartyScores {
		scores[i].PartyCode = ps.PartyCode
		scores[i].Votes = ps.Votes
	}
	if err := batchInsertPartyScores(tx, resultID, scores); err != nil {
		return "", 0, fmt.Errorf("insert party scores: %w", err)
	}

	// Canonical integrity evidence — same control as the live submit path.
	policyVersionID, err := requirePolicyVersion(ctx, tx, res.ElectionID)
	if err != nil {
		return "", 0, err
	}
	if _, err := recordIntegrityEventTx(ctx, tx, integrityEventInput{
		ResultID:        resultID,
		EventType:       "RESULT_SUBMITTED",
		PolicyVersionID: policyVersionID,
		Visibility:      integrityVisibilityObserver,
		CreatedBy:       res.SubmittedBy,
		PublicPayload: M{
			"polling_unit_code": res.PollingUnitCode,
			"status":            "pending",
			"total_votes_cast":  totalCast,
			"source":            res.Source,
		},
		PrivatePayload: M{
			"election_id":       res.ElectionID,
			"polling_unit_code": res.PollingUnitCode,
			"party_scores":      res.PartyScores,
			"accredited_voters": res.AccreditedVoters,
			"rejected_votes":    res.RejectedVotes,
			"ec8a_hash":         ec8aHash,
			"source":            res.Source,
			"source_ref":        res.SourceRef,
		},
	}); err != nil {
		return "", 0, fmt.Errorf("result evidence could not be recorded: %w", err)
	}
	return resultApplied, resultID, nil
}

func nullableInt(v int) interface{} {
	if v <= 0 {
		return nil
	}
	return v
}

// applyIngestedResult runs applyResultTx in its own transaction and writes the
// audit trail for every outcome — including conflicts, which were previously
// invisible (cf. R5-016 handoff for the live-path twin in handlers.go).
func applyIngestedResult(ctx context.Context, res ingestedResult) (resultApplyOutcome, int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()

	outcome, resultID, err := applyResultTx(ctx, tx, res)
	if err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}

	entityID := fmt.Sprintf("%d", resultID)
	switch outcome {
	case resultApplied:
		logAudit("RESULT_SUBMITTED", "result", entityID, res.SubmittedBy, map[string]interface{}{
			"phase": "Pre-Validation", "polling_unit": res.PollingUnitCode,
			"source": res.Source, "source_ref": res.SourceRef,
		})
		if mwHub != nil && mwHub.Kafka != nil {
			go publishResultEvent(TopicResultSubmitted, resultID, res.PollingUnitCode, res.ElectionID, res.SubmittedBy,
				map[string]interface{}{"phase": "Pre-Validation", "source": res.Source})
		}
	case resultConflict:
		// A rejected divergent re-submission must be auditable.
		logAudit("RESULT_SYNC_CONFLICT", "result", entityID, res.SubmittedBy, map[string]interface{}{
			"election_id": res.ElectionID, "polling_unit": res.PollingUnitCode,
			"source": res.Source, "source_ref": res.SourceRef,
			"detail": "conflicting figures for an existing result; first-writer-wins, nothing overwritten",
		})
	}
	return outcome, resultID, nil
}

// applyIngestedAccreditation persists one offline-synced accreditation with
// dedupe on (voter_pvc_hash, election_id, polling_unit_code).
func applyIngestedAccreditation(ctx context.Context, payload map[string]interface{}) (resultApplyOutcome, error) {
	deviceID := payloadString(payload, "device_id")
	electionID, ok := payloadInt(payload, "election_id")
	if !ok || electionID <= 0 {
		return "", fmt.Errorf("election_id is required")
	}
	puCode := payloadString(payload, "polling_unit_code", "pu_code")
	if puCode == "" {
		return "", fmt.Errorf("polling_unit_code is required")
	}
	pvcHash, err := normalizeLowerHex64(payloadString(payload, "voter_pvc_hash"))
	if err != nil {
		return "", fmt.Errorf("voter_pvc_hash %w", err)
	}
	method := payloadString(payload, "method")
	if method == "" {
		method = "biometric"
	}
	if !map[string]bool{"biometric": true, "manual": true, "override": true}[method] {
		return "", fmt.Errorf("invalid accreditation method")
	}
	biometricMatch := payloadString(payload, "biometric_match") == "true"
	if v, ok := payload["biometric_match"].(bool); ok {
		biometricMatch = v
	}
	pvcVerified := payloadString(payload, "pvc_verified") == "true"
	if v, ok := payload["pvc_verified"].(bool); ok {
		pvcVerified = v
	}

	res, err := db.ExecContext(ctx,
		`INSERT INTO bvas_accreditations (device_id,election_id,polling_unit_code,voter_pvc_hash,biometric_match,pvc_verified,method,accredited_at,synced_at)
		 VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
		 ON CONFLICT (voter_pvc_hash, election_id, polling_unit_code) DO NOTHING`,
		deviceID, electionID, puCode, pvcHash, boolToInt(biometricMatch), boolToInt(pvcVerified), method)
	if err != nil {
		return "", fmt.Errorf("persist accreditation: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return resultDuplicate, nil
	}
	logAudit("BVAS_ACCREDITATION_RECORDED", "bvas_accreditation", pvcHash, 0, map[string]interface{}{
		"election_id": electionID, "polling_unit": puCode, "device_id": deviceID, "source": "offline_sync",
	})
	return resultApplied, nil
}

// applyIngestedIncident persists one offline-synced incident report.
func applyIngestedIncident(ctx context.Context, payload map[string]interface{}) error {
	electionID, ok := payloadInt(payload, "election_id")
	if !ok || electionID <= 0 {
		return fmt.Errorf("election_id is required")
	}
	incidentType := payloadString(payload, "incident_type")
	description := payloadString(payload, "description")
	if incidentType == "" || description == "" {
		return fmt.Errorf("incident_type and description are required")
	}
	severity := payloadString(payload, "severity")
	if !map[string]bool{"low": true, "medium": true, "high": true, "critical": true}[severity] {
		severity = "medium"
	}
	puCode := payloadString(payload, "polling_unit_code", "pu_code")
	reportedBy, _ := payloadInt(payload, "submitted_by")

	// Idempotency: an identical open incident from the same source already
	// recorded means this is a retry — do not double-report.
	var existing int
	db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM incidents WHERE election_id=? AND COALESCE(polling_unit_code,'')=? AND incident_type=? AND description=?",
		electionID, puCode, incidentType, description).Scan(&existing)
	if existing > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO incidents (election_id, polling_unit_code, reported_by, incident_type, description, severity) VALUES (?,?,?,?,?,?)",
		electionID, nullableString(puCode), nullableInt(reportedBy), incidentType, description, severity); err != nil {
		return fmt.Errorf("persist incident: %w", err)
	}
	logAudit("INCIDENT_SYNCED", "incident", incidentType, reportedBy, map[string]interface{}{
		"election_id": electionID, "polling_unit": puCode, "source": "offline_sync",
	})
	return nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// ── Job processors: every path terminates in a real apply (R5-001) ──

func processResultIngestion(job *IngestionJob) error {
	res, err := parseIngestedResult(job.Payload)
	if err != nil {
		return err
	}
	if res.Source == "" {
		res.Source = "ingestion_api"
	}
	outcome, _, err := applyIngestedResult(context.Background(), res)
	if err != nil {
		return err
	}
	if outcome == resultConflict {
		// Conflicts are recorded (audit row written) but not fatal: the item is
		// durably accounted for, so the job completes rather than retry-looping.
		log.Info().Str("pu", res.PollingUnitCode).Int("election", res.ElectionID).Msg("ingestion: result conflict recorded")
	}
	return nil
}

func processBatchResultIngestion(job *IngestionJob) error {
	results, ok := job.Payload["results"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid batch payload: missing results array")
	}
	var applyErrs []string
	for i, r := range results {
		result, ok := r.(map[string]interface{})
		if !ok {
			applyErrs = append(applyErrs, fmt.Sprintf("item %d: not an object", i))
			continue
		}
		idemKey := fmt.Sprintf("batch-%v-%d", job.Payload["batch_id"], i)
		if _, _, err := enqueueJob("result_submission", result, idemKey); err != nil {
			applyErrs = append(applyErrs, fmt.Sprintf("item %d: %v", i, err))
		}
	}
	if len(applyErrs) > 0 {
		return fmt.Errorf("batch enqueue failures: %s", strings.Join(applyErrs, "; "))
	}
	return nil
}

func processAccreditationSync(job *IngestionJob) error {
	_, err := applyIngestedAccreditation(context.Background(), job.Payload)
	return err
}

// processOfflineSync records the queue row AND applies the item into the
// canonical store; the row is only marked 'synced' after the apply succeeds.
func processOfflineSync(job *IngestionJob) error {
	payload := job.Payload
	syncType, _ := payload["sync_type"].(string)
	deviceID, _ := payload["device_id"].(string)
	payloadJSON, _ := json.Marshal(payload)

	// Legacy jobs recovered without a key get an explicit deterministic one now.
	idemKey := job.IdempotencyKey
	if idemKey == "" {
		idemKey = deriveOfflineSyncKey(deviceID, syncType, payload)
	}

	// Reuse the queue row for this idempotency key when it exists (job retry),
	// otherwise record it as 'syncing'.
	var queueID int64
	err := db.QueryRow("SELECT id FROM offline_sync_queue WHERE idempotency_key=?", idemKey).Scan(&queueID)
	if err == sql.ErrNoRows {
		err = db.QueryRow(
			"INSERT INTO offline_sync_queue (device_id, sync_type, payload, status, idempotency_key) VALUES (?,?,?,'syncing',?) RETURNING id",
			deviceID, syncType, string(payloadJSON), idemKey).Scan(&queueID)
	}
	if err != nil {
		return fmt.Errorf("record offline sync queue row: %w", err)
	}

	var applyErr error
	switch syncType {
	case "result":
		var res ingestedResult
		res, applyErr = parseIngestedResult(payload)
		if applyErr == nil {
			res.Source = "offline_sync"
			res.SourceRef = deviceID
			_, _, applyErr = applyIngestedResult(context.Background(), res)
		}
	case "accreditation":
		_, applyErr = applyIngestedAccreditation(context.Background(), payload)
	case "incident":
		applyErr = applyIngestedIncident(context.Background(), payload)
	default:
		applyErr = fmt.Errorf("unknown sync_type: %s", syncType)
	}

	if applyErr != nil {
		dbExecLog("offline_sync_queue",
			"UPDATE offline_sync_queue SET status='failed', error_message=?, retries=retries+1 WHERE id=?",
			applyErr.Error(), queueID)
		return applyErr
	}
	dbExecLog("offline_sync_queue",
		"UPDATE offline_sync_queue SET status='synced', synced_at=CURRENT_TIMESTAMP, error_message=NULL WHERE id=?",
		queueID)
	return nil
}

// ── Ingestion API Handlers ──

func handleIngestionSubmit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type           string                 `json:"type"`
		Payload        map[string]interface{} `json:"payload"`
		IdempotencyKey string                 `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Type == "" {
		req.Type = "result_submission"
	}

	job, duplicate, err := enqueueJob(req.Type, req.Payload, req.IdempotencyKey)
	if err != nil {
		if strings.Contains(err.Error(), "queue full") {
			w.Header().Set("Retry-After", "30")
			writeError(w, 503, err.Error())
		} else {
			writeError(w, 500, err.Error())
		}
		return
	}
	status := job.Status
	if duplicate {
		status = "duplicate"
	}
	writeJSON(w, 200, M{"job_id": job.ID, "status": status, "idempotency_key": job.IdempotencyKey})
}

func handleBatchUpload(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "presiding_officer", "collation_officer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}

	var req struct {
		ElectionID int `json:"election_id"`
		Results    []struct {
			PollingUnitCode string `json:"polling_unit_code"`
			PartyScores     []struct {
				PartyCode string `json:"party_code"`
				Votes     int    `json:"votes"`
			} `json:"party_scores"`
			AccreditedVoters int `json:"accredited_voters"`
			RejectedVotes    int `json:"rejected_votes"`
		} `json:"results"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}

	if len(req.Results) == 0 {
		writeError(w, 400, "No results in batch")
		return
	}
	if len(req.Results) > 500 {
		writeError(w, 400, "Batch size exceeds maximum of 500")
		return
	}

	batchID := fmt.Sprintf("BATCH-%d-%d", time.Now().UnixNano(), len(req.Results))
	userSub, _ := user["sub"].(string)
	userID, _ := strconv.Atoi(userSub)

	var accepted, rejected, duplicate int
	var jobIDs []string
	itemOutcomes := make([]M, 0, len(req.Results))

	for i, result := range req.Results {
		idemKey := fmt.Sprintf("result-%d-%s", req.ElectionID, result.PollingUnitCode)
		item := M{"index": i, "polling_unit_code": result.PollingUnitCode, "idempotency_key": idemKey}

		payload := map[string]interface{}{
			"election_id":       req.ElectionID,
			"polling_unit_code": result.PollingUnitCode,
			"party_scores":      result.PartyScores,
			"accredited_voters": result.AccreditedVoters,
			"rejected_votes":    result.RejectedVotes,
			"submitted_by":      userID,
			"batch_id":          batchID,
			"source":            "batch",
			"source_ref":        batchID,
		}

		job, dup, err := enqueueJob("result_submission", payload, idemKey)
		switch {
		case err != nil:
			rejected++
			item["status"] = "rejected"
			item["error"] = err.Error()
		case dup:
			duplicate++
			item["status"] = "duplicate"
			item["job_id"] = job.ID
		default:
			accepted++
			jobIDs = append(jobIDs, job.ID)
			item["status"] = "queued"
			item["job_id"] = job.ID
		}
		itemOutcomes = append(itemOutcomes, item)
	}

	logAudit("BATCH_UPLOAD", "ingestion", batchID, userID, map[string]interface{}{
		"total": len(req.Results), "accepted": accepted, "rejected": rejected, "duplicate": duplicate,
	})

	writeJSON(w, 200, M{
		"batch_id":  batchID,
		"total":     len(req.Results),
		"accepted":  accepted,
		"rejected":  rejected,
		"duplicate": duplicate,
		"job_ids":   jobIDs,
		"items":     itemOutcomes,
	})
}

// validateOfflineSyncTimestamp enforces the offline capture window (R5-006/R5-010):
// client timestamps are accepted up to 72h back (accredited offline operation)
// and at most 10 minutes in the future; anything else is rejected explicitly.
func validateOfflineSyncTimestamp(ts string) error {
	if ts == "" {
		return nil // server receipt time is authoritative; capture time optional
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return fmt.Errorf("timestamp must be RFC3339")
	}
	now := time.Now().UTC()
	if parsed.After(now.Add(offlineSyncMaxFutureSkew)) {
		return fmt.Errorf("timestamp is in the future beyond the permitted skew")
	}
	if now.Sub(parsed) > offlineSyncMaxItemAge {
		return fmt.Errorf("timestamp is older than the %v offline sync window", offlineSyncMaxItemAge)
	}
	return nil
}

func handleOfflineSync(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "presiding_officer", "collation_officer", "ict_officer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}

	var req struct {
		DeviceID string `json:"device_id"`
		SyncType string `json:"sync_type"`
		Items    []struct {
			Payload        map[string]interface{} `json:"payload"`
			Timestamp      string                 `json:"timestamp"`
			IdempotencyKey string                 `json:"idempotency_key"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}

	if req.DeviceID == "" {
		writeError(w, 400, "device_id required")
		return
	}
	// R5-006: the device must be a registered BVAS device — arbitrary caller-
	// supplied device IDs are rejected before any state is touched.
	var deviceExists int
	db.QueryRow("SELECT COUNT(*) FROM bvas_devices WHERE id=?", req.DeviceID).Scan(&deviceExists)
	if deviceExists == 0 {
		writeError(w, 404, "device not registered")
		return
	}
	switch req.SyncType {
	case "result", "accreditation", "incident":
	default:
		writeError(w, 400, "sync_type must be one of result, accreditation, incident")
		return
	}
	if len(req.Items) == 0 {
		writeError(w, 400, "no items to sync")
		return
	}
	if len(req.Items) > offlineSyncMaxItems {
		writeError(w, 400, fmt.Sprintf("offline sync batch size exceeds maximum of %d", offlineSyncMaxItems))
		return
	}

	userSub, _ := user["sub"].(string)
	userID, _ := strconv.Atoi(userSub)

	var synced, failed, duplicates int
	itemOutcomes := make([]M, 0, len(req.Items))

	for i, item := range req.Items {
		outcome := M{"index": i}
		if item.Payload == nil {
			item.Payload = map[string]interface{}{}
		}
		if err := validateOfflineSyncTimestamp(item.Timestamp); err != nil {
			failed++
			outcome["status"] = "rejected"
			outcome["error"] = err.Error()
			itemOutcomes = append(itemOutcomes, outcome)
			continue
		}

		item.Payload["device_id"] = req.DeviceID
		item.Payload["sync_type"] = req.SyncType
		// Client capture time is preserved as evidence; the server stamps its
		// own authoritative receipt time (R5-010).
		item.Payload["original_timestamp"] = item.Timestamp
		item.Payload["server_received_at"] = time.Now().UTC().Format(time.RFC3339)
		if _, ok := item.Payload["submitted_by"]; !ok && userID > 0 {
			item.Payload["submitted_by"] = userID
		}

		idemKey := item.IdempotencyKey
		if idemKey == "" {
			idemKey = deriveOfflineSyncKey(req.DeviceID, req.SyncType, item.Payload)
		}
		outcome["idempotency_key"] = idemKey

		job, dup, err := enqueueJob("offline_result_sync", item.Payload, idemKey)
		switch {
		case err != nil:
			failed++
			outcome["status"] = "rejected"
			outcome["error"] = err.Error()
		case dup:
			duplicates++
			outcome["status"] = "duplicate"
			outcome["job_id"] = job.ID
		default:
			synced++
			outcome["status"] = "queued"
			outcome["job_id"] = job.ID
		}
		itemOutcomes = append(itemOutcomes, outcome)
	}

	dbExecLog("bvas_devices", "UPDATE bvas_devices SET last_sync_at=CURRENT_TIMESTAMP WHERE id=?", req.DeviceID)
	logAudit("OFFLINE_SYNC", "bvas_device", req.DeviceID, userID, map[string]interface{}{
		"sync_type": req.SyncType, "total": len(req.Items), "synced": synced, "failed": failed, "duplicates": duplicates,
	})

	// R5-007: per-item outcome protocol — devices reconcile item-by-item instead
	// of re-sending (duplicates) or deleting (loss) their whole outbox.
	writeJSON(w, 200, M{
		"device_id":  req.DeviceID,
		"total":      len(req.Items),
		"synced":     synced,
		"failed":     failed,
		"duplicates": duplicates,
		"items":      itemOutcomes,
	})
}

func handleIngestionStats(w http.ResponseWriter, r *http.Request) {
	var total, completed, failed, pending, inProgress, dlqCount int
	var avgLatency sql.NullFloat64

	db.QueryRow("SELECT COUNT(*) FROM ingestion_jobs").Scan(&total)
	db.QueryRow("SELECT COUNT(*) FROM ingestion_jobs WHERE status='completed'").Scan(&completed)
	db.QueryRow("SELECT COUNT(*) FROM ingestion_jobs WHERE status='failed' OR status='dead_letter'").Scan(&failed)
	db.QueryRow("SELECT COUNT(*) FROM ingestion_jobs WHERE status='pending'").Scan(&pending)
	db.QueryRow("SELECT COUNT(*) FROM ingestion_jobs WHERE status='in_progress'").Scan(&inProgress)
	db.QueryRow("SELECT COUNT(*) FROM dead_letter_queue WHERE reprocessed=0").Scan(&dlqCount)
	db.QueryRow("SELECT AVG(latency_ms) FROM ingestion_jobs WHERE status='completed' AND latency_ms IS NOT NULL").Scan(&avgLatency)

	elapsed := time.Since(ingestionStartTime).Seconds()
	throughput := 0.0
	if elapsed > 0 {
		throughput = float64(completed) / elapsed
	}

	avgLat := 0.0
	if avgLatency.Valid {
		avgLat = avgLatency.Float64
	}

	writeJSON(w, 200, M{
		"total_jobs":         total,
		"completed":          completed,
		"failed":             failed,
		"pending":            pending,
		"in_progress":        inProgress,
		"dead_letter_count":  dlqCount,
		"avg_latency_ms":     round2(avgLat),
		"throughput_per_sec": round2(throughput),
	})
}

func handleDeadLetterQueue(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT * FROM dead_letter_queue WHERE reprocessed=0 ORDER BY failed_at DESC LIMIT 100")
	writeJSON(w, 200, scanRows(rows))
}

func handleReprocessDLQ(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]

	var payload string
	var jobType string
	err := db.QueryRow("SELECT payload, job_type FROM dead_letter_queue WHERE id=? AND reprocessed=0", id).Scan(&payload, &jobType)
	if err != nil {
		writeError(w, 404, "DLQ entry not found or already reprocessed")
		return
	}

	var payloadMap map[string]interface{}
	json.Unmarshal([]byte(payload), &payloadMap)

	job, _, err := enqueueJob(jobType, payloadMap, "")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	dbExecLog("dead_letter_queue", "UPDATE dead_letter_queue SET reprocessed=1, reprocessed_at=CURRENT_TIMESTAMP WHERE id=?", id)

	writeJSON(w, 200, M{"message": "Reprocessed", "new_job_id": job.ID})
}

func handleIngestionJobs(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit := queryParamInt(r, "limit", 50)

	q := "SELECT id, job_type, status, idempotency_key, retries, error_message, created_at, processed_at, latency_ms FROM ingestion_jobs"
	var params []interface{}
	if status != "" {
		q += " WHERE status=?"
		params = append(params, status)
	}
	q += " ORDER BY created_at DESC LIMIT ?"
	params = append(params, limit)

	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleOfflineSyncQueue(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	q := "SELECT * FROM offline_sync_queue"
	var params []interface{}
	if status != "" {
		q += " WHERE status=?"
		params = append(params, status)
	}
	q += " ORDER BY created_at DESC LIMIT 100"
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}
