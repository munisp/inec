package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// ── Robust Ingestion Engine ──

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
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		synced_at TIMESTAMP,
		retries INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_ingestion_status ON ingestion_jobs(status);
	CREATE INDEX IF NOT EXISTS idx_ingestion_idem ON ingestion_jobs(idempotency_key);
	CREATE INDEX IF NOT EXISTS idx_dlq_reprocessed ON dead_letter_queue(reprocessed);
	CREATE INDEX IF NOT EXISTS idx_offline_status ON offline_sync_queue(status);
	`
	execMulti(database, schema)
}

func generateIdempotencyKey(jobType string, payload map[string]interface{}) string {
	data := fmt.Sprintf("%s:%v", jobType, payload)
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:16])
}

func enqueueJob(jobType string, payload map[string]interface{}, idempotencyKey string) (*IngestionJob, error) {
	ingestionMu.Lock()
	defer ingestionMu.Unlock()

	if idempotencyKey == "" {
		idempotencyKey = generateIdempotencyKey(jobType, payload)
	}

	if existingID, ok := idempotencyStore[idempotencyKey]; ok {
		for _, j := range ingestionQueue {
			if j.ID == existingID {
				return &j, nil
			}
		}
	}

	var dbCount int
	db.QueryRow("SELECT COUNT(*) FROM ingestion_jobs WHERE idempotency_key=?", idempotencyKey).Scan(&dbCount)
	if dbCount > 0 {
		row, _ := querySingleRow("SELECT * FROM ingestion_jobs WHERE idempotency_key=?", idempotencyKey)
		if row != nil {
			return &IngestionJob{
				ID:             fmt.Sprintf("%v", row["id"]),
				Type:           fmt.Sprintf("%v", row["job_type"]),
				Status:         fmt.Sprintf("%v", row["status"]),
				IdempotencyKey: idempotencyKey,
			}, nil
		}
	}

	id := fmt.Sprintf("ING-%06d", ingestionNextID)
	ingestionNextID++

	job := IngestionJob{
		ID:             id,
		Type:           jobType,
		Status:         "pending",
		Payload:        payload,
		IdempotencyKey: idempotencyKey,
		MaxRetries:     3,
		CreatedAt:      time.Now(),
	}
	ingestionQueue = append(ingestionQueue, job)
	idempotencyStore[idempotencyKey] = id

	payloadJSON, _ := json.Marshal(payload)
	db.Exec("INSERT INTO ingestion_jobs (id, job_type, status, payload, idempotency_key, max_retries) VALUES (?,?,?,?,?,?)",
		id, jobType, "pending", string(payloadJSON), idempotencyKey, 3)

	go processJob(id)

	return &job, nil
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

	db.Exec("UPDATE ingestion_jobs SET status='in_progress' WHERE id=?", jobID)
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
		db.Exec("INSERT INTO dead_letter_queue (id, job_id, job_type, error_message, payload) VALUES (?,?,?,?,?)",
			dlID, jobID, job.Type, err.Error(), string(payloadJSON))
		db.Exec("UPDATE ingestion_jobs SET status='dead_letter', error_message=?, retries=?, latency_ms=? WHERE id=?",
			err.Error(), job.Retries, float64(latency), jobID)
	} else {
		job.Status = "completed"
		now := time.Now()
		job.ProcessedAt = &now
		ingestionProcessed++
		db.Exec("UPDATE ingestion_jobs SET status='completed', processed_at=CURRENT_TIMESTAMP, retries=?, latency_ms=? WHERE id=?",
			job.Retries, float64(latency), jobID)
	}
	ingestionMu.Unlock()
}

func executeIngestionJob(job *IngestionJob) error {
	switch job.Type {
	case "result_submission":
		return processResultIngestion(job.Payload)
	case "batch_result_upload":
		return processBatchResultIngestion(job.Payload)
	case "accreditation_sync":
		return processAccreditationSync(job.Payload)
	case "offline_result_sync":
		return processOfflineSync(job.Payload)
	default:
		return fmt.Errorf("unknown job type: %s", job.Type)
	}
}

func processResultIngestion(payload map[string]interface{}) error {
	go publishResultEvent("inec.ingestion.result", 0, "", 0, 0, payload)
	return nil
}

func processBatchResultIngestion(payload map[string]interface{}) error {
	results, ok := payload["results"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid batch payload: missing results array")
	}
	for i, r := range results {
		result, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		idemKey := fmt.Sprintf("batch-%v-%d", payload["batch_id"], i)
		enqueueJob("result_submission", result, idemKey)
	}
	return nil
}

func processAccreditationSync(payload map[string]interface{}) error {
	go publishResultEvent("inec.ingestion.accreditation", 0, "", 0, 0, payload)
	return nil
}

func processOfflineSync(payload map[string]interface{}) error {
	syncType, _ := payload["sync_type"].(string)
	deviceID, _ := payload["device_id"].(string)
	payloadJSON, _ := json.Marshal(payload)
	db.Exec("INSERT INTO offline_sync_queue (device_id, sync_type, payload, status) VALUES (?,?,?,'synced')",
		deviceID, syncType, string(payloadJSON))
	return nil
}

// ── Ingestion API Handlers ──

func handleIngestionSubmit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type           string                 `json:"type"`
		Payload        map[string]interface{} `json:"payload"`
		IdempotencyKey string                 `json:"idempotency_key"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Type == "" {
		req.Type = "result_submission"
	}

	job, err := enqueueJob(req.Type, req.Payload, req.IdempotencyKey)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, M{"job_id": job.ID, "status": job.Status, "idempotency_key": job.IdempotencyKey})
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
	json.NewDecoder(r.Body).Decode(&req)

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

	for _, result := range req.Results {
		idemKey := fmt.Sprintf("result-%d-%s", req.ElectionID, result.PollingUnitCode)

		var dupCheck int
		db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=? AND polling_unit_code=?",
			req.ElectionID, result.PollingUnitCode).Scan(&dupCheck)
		if dupCheck > 0 {
			duplicate++
			continue
		}

		payload := map[string]interface{}{
			"election_id":       req.ElectionID,
			"polling_unit_code": result.PollingUnitCode,
			"party_scores":      result.PartyScores,
			"accredited_voters": result.AccreditedVoters,
			"rejected_votes":    result.RejectedVotes,
			"submitted_by":      userID,
			"batch_id":          batchID,
		}

		job, err := enqueueJob("result_submission", payload, idemKey)
		if err != nil {
			rejected++
			continue
		}
		accepted++
		jobIDs = append(jobIDs, job.ID)
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
	})
}

func handleOfflineSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string `json:"device_id"`
		SyncType string `json:"sync_type"`
		Items    []struct {
			Payload   map[string]interface{} `json:"payload"`
			Timestamp string                 `json:"timestamp"`
		} `json:"items"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.DeviceID == "" {
		writeError(w, 400, "device_id required")
		return
	}

	var synced, failed int
	for _, item := range req.Items {
		item.Payload["device_id"] = req.DeviceID
		item.Payload["sync_type"] = req.SyncType
		item.Payload["original_timestamp"] = item.Timestamp

		_, err := enqueueJob("offline_result_sync", item.Payload, "")
		if err != nil {
			failed++
		} else {
			synced++
		}
	}

	db.Exec("UPDATE bvas_devices SET last_sync_at=CURRENT_TIMESTAMP WHERE id=?", req.DeviceID)

	writeJSON(w, 200, M{
		"device_id": req.DeviceID,
		"total":     len(req.Items),
		"synced":    synced,
		"failed":    failed,
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
		"total_jobs":        total,
		"completed":         completed,
		"failed":            failed,
		"pending":           pending,
		"in_progress":       inProgress,
		"dead_letter_count": dlqCount,
		"avg_latency_ms":    round2(avgLat),
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

	job, _ := enqueueJob(jobType, payloadMap, "")
	db.Exec("UPDATE dead_letter_queue SET reprocessed=1, reprocessed_at=CURRENT_TIMESTAMP WHERE id=?", id)

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
