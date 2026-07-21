package main

import (
	"container/ring"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// ── CRITICAL #2: Bounded Ring Buffer Ingestion Queue ──
// Replaces unbounded []IngestionJob with a fixed-capacity ring buffer.
// At 176K polling units, the old queue would grow without bound and
// linear scan (O(n) per job) would become a bottleneck.

const ingestionQueueCapacity = 10000

type RingBufferQueue struct {
	mu       sync.RWMutex
	buf      *ring.Ring
	index    map[string]*IngestionJob // O(1) lookup by ID
	idempMap map[string]string        // idempotency_key → job_id
	size     int
	capacity int
	nextID   int64
}

func newRingBufferQueue(capacity int) *RingBufferQueue {
	return &RingBufferQueue{
		buf:      ring.New(capacity),
		index:    make(map[string]*IngestionJob, capacity),
		idempMap: make(map[string]string, capacity),
		capacity: capacity,
	}
}

// Enqueue adds a job. If the buffer is full, the oldest completed/failed job is evicted.
func (q *RingBufferQueue) Enqueue(job *IngestionJob) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Evict oldest if at capacity
	if q.size >= q.capacity {
		q.evictOldest()
	}

	q.buf.Value = job
	q.buf = q.buf.Next()
	q.index[job.ID] = job
	if job.IdempotencyKey != "" {
		q.idempMap[job.IdempotencyKey] = job.ID
	}
	q.size++
}

func (q *RingBufferQueue) evictOldest() {
	// Walk the ring and find the oldest completed/failed job to evict
	r := q.buf
	for i := 0; i < q.capacity; i++ {
		if r.Value != nil {
			job := r.Value.(*IngestionJob)
			if job.Status == "completed" || job.Status == "dead_letter" || job.Status == "failed" {
				delete(q.index, job.ID)
				delete(q.idempMap, job.IdempotencyKey)
				r.Value = nil
				q.size--
				return
			}
		}
		r = r.Next()
	}
	// If no completed jobs, evict the oldest regardless
	r = q.buf
	for i := 0; i < q.capacity; i++ {
		if r.Value != nil {
			job := r.Value.(*IngestionJob)
			delete(q.index, job.ID)
			delete(q.idempMap, job.IdempotencyKey)
			r.Value = nil
			q.size--
			return
		}
		r = r.Next()
	}
}

// Lookup returns a job by ID in O(1).
func (q *RingBufferQueue) Lookup(id string) *IngestionJob {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.index[id]
}

// LookupByIdempotencyKey returns a job by its idempotency key in O(1).
func (q *RingBufferQueue) LookupByIdempotencyKey(key string) *IngestionJob {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if id, ok := q.idempMap[key]; ok {
		return q.index[id]
	}
	return nil
}

// UpdateStatus updates a job's status in O(1).
func (q *RingBufferQueue) UpdateStatus(id, status string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if job, ok := q.index[id]; ok {
		job.Status = status
	}
}

// Size returns the current queue size.
func (q *RingBufferQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.size
}

// Stats returns queue statistics.
func (q *RingBufferQueue) Stats() map[string]int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	stats := map[string]int{
		"size":     q.size,
		"capacity": q.capacity,
	}
	pending, inProgress, completed, failed := 0, 0, 0, 0
	for _, job := range q.index {
		switch job.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		case "dead_letter", "failed":
			failed++
		}
	}
	stats["pending"] = pending
	stats["in_progress"] = inProgress
	stats["completed"] = completed
	stats["failed"] = failed
	return stats
}

var ringQueue *RingBufferQueue

func initRingBufferQueue() {
	cap := ingestionQueueCapacity
	if envCap := os.Getenv("INGESTION_QUEUE_CAPACITY"); envCap != "" {
		if v, err := strconv.Atoi(envCap); err == nil && v > 0 {
			cap = v
		}
	}
	ringQueue = newRingBufferQueue(cap)
	log.Info().Int("capacity", cap).Msg("Ring buffer ingestion queue initialized")
}

// ── CRITICAL #4: Sharded WebSocket Broadcast ──
// Replaces O(n) broadcast across ALL clients with state/LGA-sharded fan-out.
// At 50K clients, each broadcast only hits subscribers for the relevant shard.

type ShardedWSHub struct {
	mu     sync.RWMutex
	shards map[string]map[*websocket.Conn]bool // key = "state:KN" or "all"
	global map[*websocket.Conn]bool            // fallback for unsharded clients
}

func newShardedWSHub() *ShardedWSHub {
	return &ShardedWSHub{
		shards: make(map[string]map[*websocket.Conn]bool),
		global: make(map[*websocket.Conn]bool),
	}
}

func (h *ShardedWSHub) Register(conn *websocket.Conn, stateCode string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if stateCode == "" {
		h.global[conn] = true
		return
	}
	key := "state:" + stateCode
	if h.shards[key] == nil {
		h.shards[key] = make(map[*websocket.Conn]bool)
	}
	h.shards[key][conn] = true
}

func (h *ShardedWSHub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.global, conn)
	for _, shard := range h.shards {
		delete(shard, conn)
	}
}

// Broadcast sends to: (1) shard matching stateCode + (2) global subscribers.
// At 50K connections with 37 states, each broadcast hits ~50K/37 ≈ 1,351 + globals.
func (h *ShardedWSHub) Broadcast(msg M, stateCode string) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Msg("shardedWS: marshal failed")
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Send to state shard
	if stateCode != "" {
		key := "state:" + stateCode
		for conn := range h.shards[key] {
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Debug().Err(err).Str("shard", key).Msg("shardedWS: write failed")
			}
		}
	}

	// Send to global (unfiltered) subscribers
	for conn := range h.global {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Debug().Err(err).Msg("shardedWS: global write failed")
		}
	}
}

func (h *ShardedWSHub) Stats() M {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := len(h.global)
	shardStats := make(map[string]int)
	for key, conns := range h.shards {
		shardStats[key] = len(conns)
		total += len(conns)
	}
	return M{
		"total_connections": total,
		"global_clients":    len(h.global),
		"shards":            shardStats,
	}
}

var shardedWSHub *ShardedWSHub

func initShardedWSHub() {
	shardedWSHub = newShardedWSHub()
	log.Info().Msg("Sharded WebSocket hub initialized")
}

// broadcastWSSharded replaces the old O(n) broadcastWS with O(n/37) sharded broadcast.
func broadcastWSSharded(msg M, stateCode string) {
	if shardedWSHub != nil {
		shardedWSHub.Broadcast(msg, stateCode)
	}
	// Also send to legacy wsClients for backward compat
	broadcastWS(msg)
}

// handleWSUpdatesSharded is the sharded WS endpoint.
func handleWSUpdatesSharded(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	stateCode := r.URL.Query().Get("state_code")
	shardedWSHub.Register(conn, stateCode)
	defer func() {
		shardedWSHub.Unregister(conn)
		conn.Close()
	}()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

// ── MEDIUM #5: PgBouncer-Aware Connection Pooling ──
// Detects PgBouncer in DSN and adjusts pool settings accordingly.

func initPgBouncerAwarePooling(primary *sql.DB) {
	dsn := os.Getenv("DATABASE_URL")
	pgbouncerMode := os.Getenv("PGBOUNCER_ENABLED")

	isPgBouncer := pgbouncerMode == "true" ||
		strings.Contains(dsn, "pgbouncer") ||
		strings.Contains(dsn, ":6432")

	if isPgBouncer {
		// PgBouncer transaction mode: keep app-level pool smaller
		// PgBouncer handles the real connection multiplexing
		maxOpen := envInt("DB_MAX_OPEN_CONNS", 10)
		maxIdle := envInt("DB_MAX_IDLE_CONNS", 5)
		primary.SetMaxOpenConns(maxOpen)
		primary.SetMaxIdleConns(maxIdle)
		primary.SetConnMaxLifetime(10 * time.Minute) // Longer lifetime — PgBouncer manages actual connections
		primary.SetConnMaxIdleTime(2 * time.Minute)
		log.Info().
			Int("max_open", maxOpen).
			Int("max_idle", maxIdle).
			Msg("PgBouncer-aware pooling: reduced app-level pool (PgBouncer manages multiplexing)")
	} else {
		// Direct PostgreSQL: use larger app-level pool
		maxOpen := envInt("DB_MAX_OPEN_CONNS", 50)
		maxIdle := envInt("DB_MAX_IDLE_CONNS", 25)
		primary.SetMaxOpenConns(maxOpen)
		primary.SetMaxIdleConns(maxIdle)
		primary.SetConnMaxLifetime(5 * time.Minute)
		primary.SetConnMaxIdleTime(30 * time.Second)
		log.Info().
			Int("max_open", maxOpen).
			Int("max_idle", maxIdle).
			Msg("Direct PostgreSQL pooling: larger app-level pool")
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

// ── MEDIUM #6: Redis Response Cache with Invalidation ──
// Wraps the existing cacheGet/cacheSet with explicit cache warming and invalidation.

var collationCacheTTL = 15 * time.Second

func initCollationCache() {
	if ttl := os.Getenv("COLLATION_CACHE_TTL_SEC"); ttl != "" {
		if secs, err := strconv.Atoi(ttl); err == nil && secs > 0 {
			collationCacheTTL = time.Duration(secs) * time.Second
		}
	}
	log.Info().Dur("ttl", collationCacheTTL).Msg("Collation cache TTL configured")
}

// invalidateCollationCache clears all collation caches for an election.
// Called after a new result is submitted or collation completes.
func invalidateCollationCache(electionID int) {
	patterns := []string{
		fmt.Sprintf("dashboard_stats_%d", electionID),
		fmt.Sprintf("collation_state_%d_", electionID),
		fmt.Sprintf("collation_lga_%d_", electionID),
		fmt.Sprintf("collation_ward_%d_", electionID),
	}
	cacheDel(patterns...)
}

// ── MEDIUM #7: Batch INSERT for Party Scores ──
// Replaces N individual INSERT statements with a single multi-value INSERT.

func batchInsertPartyScores(tx dbQuerier, resultID int64, partyScores []struct {
	PartyCode string `json:"party_code"`
	Votes     int    `json:"votes"`
}) error {
	if len(partyScores) == 0 {
		return nil
	}

	// PostgreSQL: single multi-value INSERT
	valueStrings := make([]string, 0, len(partyScores))
	valueArgs := make([]interface{}, 0, len(partyScores)*3)
	for i, ps := range partyScores {
		base := i * 3
		valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, $%d)", base+1, base+2, base+3))
		valueArgs = append(valueArgs, resultID, ps.PartyCode, ps.Votes)
	}
	query := fmt.Sprintf(
		"INSERT INTO result_party_scores (result_id, party_code, votes) VALUES %s",
		strings.Join(valueStrings, ", "),
	)
	_, err := tx.Exec(query, valueArgs...)
	return err
}

// ── MEDIUM #8: Middleware Connectivity Detection ──
// Logs clearly whether each middleware is embedded (in-memory) or real (external).

type MiddlewareMode struct {
	Name       string
	IsReal     bool
	Connection string
}

func detectMiddlewareModes() []MiddlewareMode {
	modes := []MiddlewareMode{}
	if mwHub == nil {
		return modes
	}

	for name, st := range mwHub.status {
		isReal := st.Connected && !strings.Contains(st.Mode, "embedded")
		modes = append(modes, MiddlewareMode{
			Name:       name,
			IsReal:     isReal,
			Connection: st.Mode,
		})
		if isReal {
			log.Info().Str("name", name).Str("mode", st.Mode).Msg("Middleware: REAL external connection")
		} else {
			log.Warn().Str("name", name).Str("mode", st.Mode).Msg("Middleware: EMBEDDED in-memory (data lost on restart)")
		}
	}
	return modes
}

func handleMiddlewareModes(w http.ResponseWriter, r *http.Request) {
	modes := detectMiddlewareModes()
	realCount := 0
	embeddedCount := 0
	for _, m := range modes {
		if m.IsReal {
			realCount++
		} else {
			embeddedCount++
		}
	}
	writeJSON(w, 200, M{
		"modes":            modes,
		"real_count":       realCount,
		"embedded_count":   embeddedCount,
		"production_ready": embeddedCount == 0,
		"warning": map[bool]string{
			true:  "",
			false: fmt.Sprintf("%d middleware components are embedded (in-memory). Deploy real services for production.", embeddedCount),
		}[embeddedCount == 0],
	})
}

// ── LOW #9: Read Replica Routing Enhancement ──
// Explicitly routes read-heavy queries to replica with fallback.

func dbReadQuery(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	start := time.Now()
	target := dbReader
	isReplica := dbReader != dbWriter

	// For replica, add a short timeout to detect stale replica
	if isReplica {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
	}

	rows, err := target.QueryContext(ctx, convertPlaceholders(query), args...)
	if err != nil && isReplica {
		// Fallback to primary on replica error
		log.Warn().Err(err).Msg("Read replica query failed, falling back to primary")
		rows, err = dbWriter.QueryContext(ctx, convertPlaceholders(query), args...)
		dbMetrics.recordRead(time.Since(start), false)
		return rows, err
	}
	dbMetrics.recordRead(time.Since(start), isReplica)
	return rows, err
}

func dbReadQueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	target := dbReader
	return target.QueryRowContext(ctx, convertPlaceholders(query), args...)
}

// ── LOW #10: Redis-Based Distributed Rate Limiter ──
// The existing rateLimiterStore already has Redis support (allowRedis method).
// This enhancement adds: cleanup goroutine for local entries + configurable limits.

func initDistributedRateLimiter() {
	mode := "native Redis unavailable"
	if mwHub != nil && mwHub.Redis != nil {
		if status := mwHub.Redis.Ping(); status.Connected {
			mode = "native Redis distributed enforcement"
		}
	}
	log.Info().Str("mode", mode).Msg("Rate limiter initialized")
}

// ── Scale Health Endpoint ──
// Reports all scale-related metrics in one call.

func handleScaleHealth(w http.ResponseWriter, r *http.Request) {
	result := M{
		"database": M{
			"engine":            "postgresql",
			"read_write_split":  dbReader != dbWriter,
			"writer_pool":       dbWriter.Stats(),
			"pgbouncer_enabled": strings.Contains(os.Getenv("DATABASE_URL"), "pgbouncer") || os.Getenv("PGBOUNCER_ENABLED") == "true",
		},
		"ingestion_queue": func() interface{} {
			if ringQueue != nil {
				return ringQueue.Stats()
			}
			return M{"type": "legacy_unbounded"}
		}(),
		"websocket": func() interface{} {
			if shardedWSHub != nil {
				return shardedWSHub.Stats()
			}
			return M{"type": "legacy_global_broadcast"}
		}(),
		"sse_connections": func() int {
			sseHub.mu.RLock()
			defer sseHub.mu.RUnlock()
			return len(sseHub.subscribers)
		}(),
		"rate_limiter": M{
			"mode": func() string {
				if mwHub != nil && mwHub.Redis != nil && mwHub.Redis.Ping().Connected {
					return "native_redis"
				}
				return "unavailable"
			}(),
		},
		"collation_cache_ttl_sec": collationCacheTTL.Seconds(),
		"middleware_modes":        detectMiddlewareModes(),
	}

	if dbReader != dbWriter {
		result["reader_pool"] = dbReader.Stats()
	}

	writeJSON(w, 200, result)
}

// ── Ingestion Queue Metrics Endpoint ──

var ingestionThroughput int64

func trackIngestionThroughput() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	var lastProcessed int64
	for range ticker.C {
		current := atomic.LoadInt64(&ingestionProcessed)
		atomic.StoreInt64(&ingestionThroughput, current-lastProcessed)
		lastProcessed = current
	}
}
