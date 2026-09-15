// Package ingestion provides a persistent job queue with backpressure,
// idempotency, and dead-letter handling for async data processing.
//
// R5-002/R5-003: idempotency is arbitrated by the database (unique
// idempotency_key + ON CONFLICT), never by nanotime nonces or in-memory
// maps alone; the queue cap counts in-flight jobs only (terminal jobs are
// pruned); startup recovery drains ALL pending rows, not just the first 1000.
package ingestion

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	MaxQueueSize   = 10000 // in-flight (pending/in_progress) jobs
	DefaultRetries = 3

	recoveryPageSize          = 1000
	maxConcurrentJobProcessor = 32
)

// JobStatus represents the lifecycle state of an ingestion job.
type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusProcessing JobStatus = "in_progress"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
	StatusDead       JobStatus = "dead_letter"
)

// Job represents an ingestion job.
type Job struct {
	ID             string                 `json:"id"`
	Type           string                 `json:"type"`
	Status         JobStatus              `json:"status"`
	Payload        map[string]interface{} `json:"payload"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Retries        int                    `json:"retries"`
	MaxRetries     int                    `json:"max_retries"`
	Error          string                 `json:"error,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	ProcessedAt    *time.Time             `json:"processed_at,omitempty"`
}

// Service provides ingestion queue operations.
type Service struct {
	db         *sql.DB
	mu         sync.Mutex
	queue      []Job
	processors map[string]ProcessorFunc
	procSem    chan struct{}
	wg         sync.WaitGroup
}

// ProcessorFunc handles a specific job type.
type ProcessorFunc func(ctx context.Context, payload map[string]interface{}) error

// NewService creates a new ingestion service.
func NewService(db *sql.DB) *Service {
	s := &Service{
		db:         db,
		queue:      make([]Job, 0, 1000),
		processors: make(map[string]ProcessorFunc),
		procSem:    make(chan struct{}, maxConcurrentJobProcessor),
	}
	// Register default processors
	s.RegisterProcessor("result_import", s.processResultImport)
	return s
}

// RegisterProcessor adds a job type handler.
func (s *Service) RegisterProcessor(jobType string, fn ProcessorFunc) {
	s.processors[jobType] = fn
}

// jobIDForKey derives a deterministic job ID from the idempotency key, so the
// same logical job has the same identity across retries, restarts and replicas.
func jobIDForKey(idempotencyKey string) string {
	h := sha256.Sum256([]byte("ingestion-job:" + idempotencyKey))
	return "job_" + hex.EncodeToString(h[:16])
}

// Enqueue adds a job to the queue with backpressure.
//
// The idempotency key is REQUIRED and must be client-supplied — silent
// server-side key generation (e.g. nanotime nonces) makes every retry a new
// job and is rejected explicitly (R5-002). The database is the idempotency
// arbiter: if the key already exists, the existing job is returned with
// duplicate=true and is NOT processed again.
func (s *Service) Enqueue(ctx context.Context, jobType string, payload map[string]interface{}, idempotencyKey string) (job *Job, duplicate bool, err error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, false, fmt.Errorf("idempotency_key is required (client-supplied); refusing to auto-generate one")
	}

	s.mu.Lock()
	inFlight := 0
	for _, j := range s.queue {
		if j.Status == StatusPending || j.Status == StatusProcessing {
			inFlight++
		}
	}
	s.mu.Unlock()
	if inFlight >= MaxQueueSize {
		return nil, false, fmt.Errorf("queue full (%d/%d in flight) — backpressure active", inFlight, MaxQueueSize)
	}

	newJob := Job{
		ID:             jobIDForKey(idempotencyKey),
		Type:           jobType,
		Status:         StatusPending,
		Payload:        payload,
		IdempotencyKey: idempotencyKey,
		MaxRetries:     DefaultRetries,
		CreatedAt:      time.Now(),
	}

	payloadJSON, _ := json.Marshal(payload)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ingestion_jobs (id, job_type, payload, idempotency_key, status, max_retries, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (idempotency_key) DO NOTHING`,
		newJob.ID, newJob.Type, string(payloadJSON), newJob.IdempotencyKey, string(newJob.Status), newJob.MaxRetries, newJob.CreatedAt)
	if err != nil {
		return nil, false, fmt.Errorf("persist job: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		// Key already exists (earlier enqueue, restart, or another replica):
		// return the persisted job WITHOUT re-processing it (R5-002).
		existing, loadErr := s.loadJobByKey(ctx, idempotencyKey)
		if loadErr != nil {
			return nil, false, fmt.Errorf("load existing job for idempotency key: %w", loadErr)
		}
		return existing, true, nil
	}

	s.mu.Lock()
	s.queue = append(s.queue, newJob)
	s.mu.Unlock()

	s.runProcessJob(newJob.ID)

	log.Info().Str("job_id", newJob.ID).Str("type", jobType).Msg("Job enqueued")
	return &newJob, false, nil
}

func (s *Service) loadJobByKey(ctx context.Context, idempotencyKey string) (*Job, error) {
	var j Job
	var payloadStr string
	var processedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, job_type, status, payload, idempotency_key, COALESCE(retries,0), max_retries, processed_at
		 FROM ingestion_jobs WHERE idempotency_key = $1`, idempotencyKey).
		Scan(&j.ID, &j.Type, &j.Status, &payloadStr, &j.IdempotencyKey, &j.Retries, &j.MaxRetries, &processedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(payloadStr), &j.Payload)
	if processedAt.Valid {
		t := processedAt.Time
		j.ProcessedAt = &t
	}
	return &j, nil
}

// QueueStats returns current queue statistics.
func (s *Service) QueueStats() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	pending, processing, failed := 0, 0, 0
	for _, j := range s.queue {
		switch j.Status {
		case StatusPending:
			pending++
		case StatusProcessing:
			processing++
		case StatusFailed, StatusDead:
			failed++
		}
	}
	inFlight := pending + processing
	return map[string]interface{}{
		"in_flight":       inFlight,
		"capacity":        MaxQueueSize,
		"pending":         pending,
		"processing":      processing,
		"failed":          failed,
		"utilization_pct": float64(inFlight) / float64(MaxQueueSize) * 100,
	}
}

// RecoverPending loads incomplete jobs from DB on startup. It resets stale
// 'in_progress' rows (crashed workers) and pages the table with keyset
// pagination until EVERY pending job is requeued — no 1000-row cap (R5-003).
func (s *Service) RecoverPending(ctx context.Context) int {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE ingestion_jobs SET status='pending' WHERE status='in_progress'`); err != nil {
		log.Warn().Err(err).Msg("Failed to reset stale in_progress jobs")
	}

	recovered := 0
	lastID := ""
	for {
		rows, err := s.db.QueryContext(ctx,
			`SELECT id, job_type, payload, idempotency_key, COALESCE(retries,0), max_retries
			 FROM ingestion_jobs WHERE status = 'pending' AND id > $1
			 ORDER BY id ASC LIMIT $2`, lastID, recoveryPageSize)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to recover pending jobs")
			return recovered
		}
		page := 0
		for rows.Next() {
			var id, jobType, payloadStr, idemKey string
			var retries, maxRetries int
			if rows.Scan(&id, &jobType, &payloadStr, &idemKey, &retries, &maxRetries) != nil {
				continue
			}
			page++
			lastID = id
			var payload map[string]interface{}
			json.Unmarshal([]byte(payloadStr), &payload)

			s.mu.Lock()
			already := false
			for _, j := range s.queue {
				if j.ID == id {
					already = true
					break
				}
			}
			if !already {
				s.queue = append(s.queue, Job{
					ID: id, Type: jobType, Status: StatusPending, Payload: payload,
					IdempotencyKey: idemKey, Retries: retries, MaxRetries: maxRetries,
					CreatedAt: time.Now(),
				})
				recovered++
				s.runProcessJob(id)
			}
			s.mu.Unlock()
		}
		rows.Close()
		if page < recoveryPageSize {
			break
		}
	}
	if recovered > 0 {
		log.Info().Int("count", recovered).Msg("Recovered pending jobs from DB")
	}
	return recovered
}

// Shutdown waits for in-flight job goroutines to drain (R5-008). Jobs that
// have not finished when ctx expires stay 'in_progress' and are rescued by
// RecoverPending on the next start.
func (s *Service) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) runProcessJob(jobID string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.procSem <- struct{}{}
		defer func() { <-s.procSem }()
		s.processJob(jobID)
	}()
}

// removeJob drops a terminal job from the in-memory queue; the database is the
// source of truth for history and idempotency.
func (s *Service) removeJob(jobID string) {
	for i := range s.queue {
		if s.queue[i].ID == jobID {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			return
		}
	}
}

func (s *Service) processJob(jobID string) {
	s.mu.Lock()
	var job *Job
	for i := range s.queue {
		if s.queue[i].ID == jobID {
			s.queue[i].Status = StatusProcessing
			job = &s.queue[i]
			break
		}
	}
	s.mu.Unlock()

	if job == nil {
		return
	}

	processor, ok := s.processors[job.Type]
	if !ok {
		s.markFailed(jobID, "no processor registered for type: "+job.Type)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := processor(ctx, job.Payload); err != nil {
		s.mu.Lock()
		for i := range s.queue {
			if s.queue[i].ID == jobID {
				s.queue[i].Retries++
				if s.queue[i].Retries >= s.queue[i].MaxRetries {
					s.queue[i].Status = StatusDead
					s.queue[i].Error = err.Error()
					s.db.Exec(`UPDATE ingestion_jobs SET status='dead_letter', error_message=$2, retries=$3 WHERE id=$1`, jobID, err.Error(), s.queue[i].Retries)
					s.removeJob(jobID)
				} else {
					s.queue[i].Status = StatusPending
					retries := s.queue[i].Retries
					s.db.Exec(`UPDATE ingestion_jobs SET status='pending', retries=$2 WHERE id=$1`, jobID, retries)
					s.wg.Add(1)
					go func() {
						defer s.wg.Done()
						time.Sleep(time.Duration(retries) * 5 * time.Second)
						s.procSem <- struct{}{}
						defer func() { <-s.procSem }()
						s.processJob(jobID)
					}()
				}
				break
			}
		}
		s.mu.Unlock()
		return
	}

	now := time.Now()
	s.mu.Lock()
	for i := range s.queue {
		if s.queue[i].ID == jobID {
			s.queue[i].Status = StatusCompleted
			s.queue[i].ProcessedAt = &now
			break
		}
	}
	s.removeJob(jobID)
	s.mu.Unlock()

	s.db.Exec(`UPDATE ingestion_jobs SET status='completed', processed_at=NOW() WHERE id=$1`, jobID)
}

func (s *Service) markFailed(jobID, errMsg string) {
	s.mu.Lock()
	for i := range s.queue {
		if s.queue[i].ID == jobID {
			s.queue[i].Status = StatusFailed
			s.queue[i].Error = errMsg
			break
		}
	}
	s.removeJob(jobID)
	s.mu.Unlock()
	s.db.Exec(`UPDATE ingestion_jobs SET status='failed', error_message=$2 WHERE id=$1`, jobID, errMsg)
}

// processResultImport applies a result payload into the canonical results
// store (R5-001): idempotent on (election_id, polling_unit_code) — an
// identical re-apply is a no-op, a divergent one is a logged conflict and
// never overwrites the first writer.
func (s *Service) processResultImport(ctx context.Context, payload map[string]interface{}) error {
	electionID, err := payloadInt(payload, "election_id")
	if err != nil || electionID <= 0 {
		return fmt.Errorf("election_id is required")
	}
	puCode, err := payloadStringE(payload, "polling_unit_code")
	if err != nil {
		return err
	}
	accredited, _ := payloadInt(payload, "accredited_voters")
	rejected, _ := payloadInt(payload, "rejected_votes")
	submittedBy, _ := payloadInt(payload, "submitted_by")

	scoresRaw, ok := payload["party_scores"].([]interface{})
	if !ok || len(scoresRaw) == 0 {
		return fmt.Errorf("party_scores is required")
	}
	type partyScore struct {
		code  string
		votes int
	}
	scores := make([]partyScore, 0, len(scoresRaw))
	totalValid := 0
	for _, raw := range scoresRaw {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("party_scores entries must be objects")
		}
		code, _ := entry["party_code"].(string)
		votesF, _ := entry["votes"].(float64)
		if code == "" {
			return fmt.Errorf("party_scores entries require party_code")
		}
		scores = append(scores, partyScore{code: code, votes: int(votesF)})
		totalValid += int(votesF)
	}
	totalCast := totalValid + rejected

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var officerArg interface{}
	if submittedBy > 0 {
		officerArg = submittedBy
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO results (election_id, polling_unit_code, presiding_officer_id, status,
			total_valid_votes, rejected_votes, total_votes_cast, accredited_voters,
			tigerbeetle_transfer_id, tigerbeetle_status, hyperledger_status)
		 VALUES ($1,$2,$3,'pending',$4,$5,$6,$7,NULL,'NOT_APPLICABLE','PENDING')
		 ON CONFLICT (election_id, polling_unit_code) DO NOTHING`,
		electionID, puCode, officerArg, totalValid, rejected, totalCast, accredited)
	if err != nil {
		return fmt.Errorf("insert result: %w", err)
	}
	var resultID int64
	if affected, _ := res.RowsAffected(); affected == 0 {
		// Duplicate: identical re-apply is a safe no-op. Divergent figures are
		// a conflict — first-writer-wins, nothing overwritten, loudly logged.
		var existingValid, existingRejected, existingAccredited int
		if err := tx.QueryRowContext(ctx,
			`SELECT id, total_valid_votes, rejected_votes, accredited_voters FROM results WHERE election_id=$1 AND polling_unit_code=$2`,
			electionID, puCode).Scan(&resultID, &existingValid, &existingRejected, &existingAccredited); err != nil {
			return fmt.Errorf("load existing result: %w", err)
		}
		if existingValid != totalValid || existingRejected != rejected || existingAccredited != accredited {
			log.Warn().Int("election_id", electionID).Str("pu", puCode).
				Msg("result_import conflict: divergent figures for existing result; first-writer-wins")
		}
		return tx.Commit()
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM results WHERE election_id=$1 AND polling_unit_code=$2`,
		electionID, puCode).Scan(&resultID); err != nil {
		return fmt.Errorf("load inserted result id: %w", err)
	}
	for _, sc := range scores {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO result_party_scores (result_id, party_code, votes) VALUES ($1,$2,$3)`,
			resultID, sc.code, sc.votes); err != nil {
			return fmt.Errorf("insert party score: %w", err)
		}
	}
	return tx.Commit()
}

func payloadInt(payload map[string]interface{}, key string) (int, error) {
	v, ok := payload[key]
	if !ok {
		return 0, fmt.Errorf("%s is required", key)
	}
	switch t := v.(type) {
	case float64:
		return int(t), nil
	case int:
		return t, nil
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("%s must be a number", key)
}

func payloadStringE(payload map[string]interface{}, key string) (string, error) {
	v, ok := payload[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return strings.TrimSpace(s), nil
}
