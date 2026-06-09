// Package ingestion provides a persistent job queue with backpressure,
// idempotency, and dead-letter handling for async data processing.
package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	MaxQueueSize   = 10000
	DefaultRetries = 3
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
	db             *sql.DB
	mu             sync.Mutex
	queue          []Job
	idempotency    map[string]string
	processors     map[string]ProcessorFunc
}

// ProcessorFunc handles a specific job type.
type ProcessorFunc func(ctx context.Context, payload map[string]interface{}) error

// NewService creates a new ingestion service.
func NewService(db *sql.DB) *Service {
	s := &Service{
		db:          db,
		queue:       make([]Job, 0, 1000),
		idempotency: make(map[string]string),
		processors:  make(map[string]ProcessorFunc),
	}
	// Register default processors
	s.RegisterProcessor("result_import", s.processResultImport)
	s.RegisterProcessor("biometric_batch", s.processBiometricBatch)
	s.RegisterProcessor("geo_sync", s.processGeoSync)
	return s
}

// RegisterProcessor adds a job type handler.
func (s *Service) RegisterProcessor(jobType string, fn ProcessorFunc) {
	s.processors[jobType] = fn
}

// Enqueue adds a job to the queue with backpressure.
func (s *Service) Enqueue(ctx context.Context, jobType string, payload map[string]interface{}, idempotencyKey string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Backpressure check
	if len(s.queue) >= MaxQueueSize {
		return nil, fmt.Errorf("queue full (%d/%d) — backpressure active", len(s.queue), MaxQueueSize)
	}

	// Idempotency check
	if existingID, ok := s.idempotency[idempotencyKey]; ok {
		for _, j := range s.queue {
			if j.ID == existingID {
				return &j, nil
			}
		}
	}

	job := Job{
		ID:             fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), len(s.queue)),
		Type:           jobType,
		Status:         StatusPending,
		Payload:        payload,
		IdempotencyKey: idempotencyKey,
		MaxRetries:     DefaultRetries,
		CreatedAt:      time.Now(),
	}

	// Persist to DB
	payloadJSON, _ := json.Marshal(payload)
	s.db.ExecContext(ctx,
		`INSERT INTO ingestion_jobs (id, job_type, payload, idempotency_key, status, max_retries, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (idempotency_key) DO NOTHING`,
		job.ID, job.Type, string(payloadJSON), job.IdempotencyKey, string(job.Status), job.MaxRetries, job.CreatedAt)

	s.queue = append(s.queue, job)
	s.idempotency[idempotencyKey] = job.ID

	// Process async
	go s.processJob(job.ID)

	log.Info().Str("job_id", job.ID).Str("type", jobType).Int("queue_size", len(s.queue)).Msg("Job enqueued")
	return &job, nil
}

// QueueStats returns current queue statistics.
func (s *Service) QueueStats() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	pending, processing, completed, failed := 0, 0, 0, 0
	for _, j := range s.queue {
		switch j.Status {
		case StatusPending:
			pending++
		case StatusProcessing:
			processing++
		case StatusCompleted:
			completed++
		case StatusFailed, StatusDead:
			failed++
		}
	}
	return map[string]interface{}{
		"total":      len(s.queue),
		"capacity":   MaxQueueSize,
		"pending":    pending,
		"processing": processing,
		"completed":  completed,
		"failed":     failed,
		"utilization_pct": float64(len(s.queue)) / float64(MaxQueueSize) * 100,
	}
}

// RecoverPending loads incomplete jobs from DB on startup.
func (s *Service) RecoverPending(ctx context.Context) int {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_type, payload, idempotency_key, COALESCE(retries,0), max_retries
		 FROM ingestion_jobs WHERE status IN ('pending','in_progress')
		 ORDER BY created_at ASC LIMIT 1000`)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to recover pending jobs")
		return 0
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	recovered := 0
	for rows.Next() {
		var id, jobType, payloadStr, idemKey string
		var retries, maxRetries int
		if rows.Scan(&id, &jobType, &payloadStr, &idemKey, &retries, &maxRetries) != nil {
			continue
		}
		var payload map[string]interface{}
		json.Unmarshal([]byte(payloadStr), &payload)

		job := Job{
			ID: id, Type: jobType, Status: StatusPending, Payload: payload,
			IdempotencyKey: idemKey, Retries: retries, MaxRetries: maxRetries,
			CreatedAt: time.Now(),
		}
		s.queue = append(s.queue, job)
		s.idempotency[idemKey] = id
		recovered++
		go s.processJob(id)
	}
	if recovered > 0 {
		log.Info().Int("count", recovered).Msg("Recovered pending jobs from DB")
	}
	return recovered
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
				} else {
					s.queue[i].Status = StatusPending
					go func() {
						time.Sleep(time.Duration(s.queue[i].Retries) * 5 * time.Second)
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
	s.mu.Unlock()
	s.db.Exec(`UPDATE ingestion_jobs SET status='failed', error=$2 WHERE id=$1`, jobID, errMsg)
}

// Default processors
func (s *Service) processResultImport(_ context.Context, payload map[string]interface{}) error {
	log.Info().Interface("payload", payload).Msg("Processing result import")
	return nil
}

func (s *Service) processBiometricBatch(_ context.Context, payload map[string]interface{}) error {
	log.Info().Interface("payload", payload).Msg("Processing biometric batch")
	return nil
}

func (s *Service) processGeoSync(_ context.Context, payload map[string]interface{}) error {
	log.Info().Interface("payload", payload).Msg("Processing geo sync")
	return nil
}
