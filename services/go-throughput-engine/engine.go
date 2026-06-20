package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// ThroughputEngine orchestrates all optimized middleware pipelines.
type ThroughputEngine struct {
	cfg    Config
	logger *zap.Logger

	kafka      *KafkaBatchProducer
	redis      *RedisPipeline
	tb         *TigerBeetleBatch
	pg         *PGBatchPool
	moja       *MojaloopPipeline
	temporal   *TemporalPool
	opensearch *OSBulkIndexer
	dapr       *DaprBulkPublisher
	permify    *PermifyCachedClient
	fluvio     *FluvioPipeline
	apisix     *APISIXOptimizer

	// Metrics
	totalIngested  atomic.Int64
	totalProcessed atomic.Int64
	totalErrors    atomic.Int64
	startTime      time.Time
}

func NewThroughputEngine(cfg Config, logger *zap.Logger) (*ThroughputEngine, error) {
	e := &ThroughputEngine{
		cfg:       cfg,
		logger:    logger,
		startTime: time.Now(),
	}

	e.kafka = NewKafkaBatchProducer(cfg, logger)
	e.redis = NewRedisPipeline(cfg, logger)
	e.tb = NewTigerBeetleBatch(cfg, logger)
	e.pg = NewPGBatchPool(cfg, logger)
	e.moja = NewMojaloopPipeline(cfg, logger)
	e.temporal = NewTemporalPool(cfg, logger)
	e.opensearch = NewOSBulkIndexer(cfg, logger)
	e.dapr = NewDaprBulkPublisher(cfg, logger)
	e.permify = NewPermifyCachedClient(cfg, logger)
	e.fluvio = NewFluvioPipeline(cfg, logger)
	e.apisix = NewAPISIXOptimizer(cfg, logger)

	return e, nil
}

func (e *ThroughputEngine) Start(ctx context.Context) {
	e.kafka.Start(ctx)
	e.redis.Start(ctx)
	e.tb.Start(ctx)
	e.pg.Start(ctx)
	e.opensearch.Start(ctx)
	e.dapr.Start(ctx)
	e.fluvio.Start(ctx)
	e.logger.Info("all pipelines started")
}

func (e *ThroughputEngine) Shutdown() {
	e.kafka.Close()
	e.redis.Close()
	e.tb.Close()
	e.pg.Close()
	e.opensearch.Close()
	e.dapr.Close()
	e.fluvio.Close()
	e.logger.Info("engine shut down")
}

// IngestHandler accepts a single transaction and routes to all pipelines.
func (e *ThroughputEngine) IngestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var tx Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	e.totalIngested.Add(1)

	ctx := r.Context()
	g, gCtx := errgroup.WithContext(ctx)

	// Fan-out to all middleware in parallel
	g.Go(func() error { return e.kafka.Submit(gCtx, tx) })
	g.Go(func() error { return e.redis.CacheResult(gCtx, tx) })
	g.Go(func() error { return e.tb.RecordTransfer(gCtx, tx) })
	g.Go(func() error { return e.pg.Insert(gCtx, tx) })
	g.Go(func() error { return e.opensearch.Index(gCtx, tx) })
	g.Go(func() error { return e.fluvio.Stream(gCtx, tx) })

	if err := g.Wait(); err != nil {
		e.totalErrors.Add(1)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	e.totalProcessed.Add(1)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "id": tx.ID})
}

// BatchHandler accepts bulk transactions for maximum throughput.
func (e *ThroughputEngine) BatchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var batch []Transaction
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	count := int64(len(batch))
	e.totalIngested.Add(count)

	ctx := r.Context()
	g, gCtx := errgroup.WithContext(ctx)

	// Process all middleware in parallel with batched operations
	g.Go(func() error { return e.kafka.SubmitBatch(gCtx, batch) })
	g.Go(func() error { return e.redis.CacheBatch(gCtx, batch) })
	g.Go(func() error { return e.tb.RecordBatch(gCtx, batch) })
	g.Go(func() error { return e.pg.InsertBatch(gCtx, batch) })
	g.Go(func() error { return e.opensearch.BulkIndex(gCtx, batch) })
	g.Go(func() error { return e.fluvio.StreamBatch(gCtx, batch) })
	g.Go(func() error { return e.dapr.PublishBatch(gCtx, batch) })

	if err := g.Wait(); err != nil {
		e.totalErrors.Add(1)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	e.totalProcessed.Add(count)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "accepted",
		"count":     count,
		"processed": e.totalProcessed.Load(),
	})
}

func (e *ThroughputEngine) HealthHandler(w http.ResponseWriter, _ *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (e *ThroughputEngine) StatsHandler(w http.ResponseWriter, _ *http.Request) {
	uptime := time.Since(e.startTime)
	ingested := e.totalIngested.Load()
	processed := e.totalProcessed.Load()
	errors := e.totalErrors.Load()
	tps := float64(0)
	if uptime.Seconds() > 0 {
		tps = float64(processed) / uptime.Seconds()
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"uptime_sec":      uptime.Seconds(),
		"total_ingested":  ingested,
		"total_processed": processed,
		"total_errors":    errors,
		"current_tps":     fmt.Sprintf("%.0f", tps),
		"pipelines": map[string]interface{}{
			"kafka_queue_depth":      e.kafka.QueueDepth(),
			"redis_pipeline_depth":   e.redis.PipelineDepth(),
			"tb_batch_queue":         e.tb.QueueDepth(),
			"pg_batch_queue":         e.pg.QueueDepth(),
			"os_bulk_queue":          e.opensearch.QueueDepth(),
			"permify_cache_hit_rate": e.permify.HitRate(),
		},
	})
}

func (e *ThroughputEngine) MetricsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "# HELP inec_transactions_total Total transactions processed\n")
	fmt.Fprintf(w, "# TYPE inec_transactions_total counter\n")
	fmt.Fprintf(w, "inec_transactions_total{status=\"ingested\"} %d\n", e.totalIngested.Load())
	fmt.Fprintf(w, "inec_transactions_total{status=\"processed\"} %d\n", e.totalProcessed.Load())
	fmt.Fprintf(w, "inec_transactions_total{status=\"errors\"} %d\n", e.totalErrors.Load())
	fmt.Fprintf(w, "# HELP inec_uptime_seconds Engine uptime\n")
	fmt.Fprintf(w, "# TYPE inec_uptime_seconds gauge\n")
	fmt.Fprintf(w, "inec_uptime_seconds %f\n", time.Since(e.startTime).Seconds())
}

// Transaction is the unified event format flowing through all pipelines.
type Transaction struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"` // result_submission, ballot_cast, incident, accreditation
	Source      string                 `json:"source"`
	Timestamp   time.Time              `json:"timestamp"`
	ElectionID  string                 `json:"election_id"`
	StateCode   string                 `json:"state_code"`
	LGAID       string                 `json:"lga_id"`
	WardID      string                 `json:"ward_id"`
	PUID        string                 `json:"pu_id"`
	Amount      int64                  `json:"amount,omitempty"`
	Data        map[string]interface{} `json:"data"`
	Hash        string                 `json:"hash"`
	Signature   string                 `json:"signature,omitempty"`
}

// WorkerPool manages a fixed set of goroutines processing items from a channel.
type WorkerPool struct {
	name    string
	workers int
	input   chan func()
	wg      sync.WaitGroup
	logger  *zap.Logger
}

func NewWorkerPool(name string, workers, queueSize int, logger *zap.Logger) *WorkerPool {
	wp := &WorkerPool{
		name:    name,
		workers: workers,
		input:   make(chan func(), queueSize),
		logger:  logger,
	}
	return wp
}

func (wp *WorkerPool) Start(ctx context.Context) {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go func() {
			defer wp.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case fn, ok := <-wp.input:
					if !ok {
						return
					}
					fn()
				}
			}
		}()
	}
	wp.logger.Info("worker pool started", zap.String("name", wp.name), zap.Int("workers", wp.workers))
}

func (wp *WorkerPool) Submit(fn func()) {
	wp.input <- fn
}

func (wp *WorkerPool) Close() {
	close(wp.input)
	wp.wg.Wait()
}

func (wp *WorkerPool) QueueDepth() int {
	return len(wp.input)
}
