// Ingestion Service — independently deployable async job processing service.
// Handles: Batch imports, backpressure, dead-letter queue, retry with exponential backoff.
//
// Usage:
//
//	go run ./cmd/ingestion-svc --port=8095 --db=postgres://...
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"inec-go-backend/internal/authmw"
	"inec-go-backend/internal/ingestion"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	port := flag.Int("port", 8095, "HTTP port")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	if *dbURL == "" {
		log.Fatal().Msg("DATABASE_URL environment variable is required")
	}

	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	svc := ingestion.NewService(db)

	// Recover pending jobs from previous runs
	recovered := svc.RecoverPending(context.Background())
	log.Info().Int("recovered", recovered).Msg("Startup recovery complete")

	r := mux.NewRouter()
	// JWT authentication on all routes except /health.
	r.Use(authmw.Middleware("/health"))

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// Readiness: database must be reachable (2s timeout), 503 on failure.
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(pingCtx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"service": "ingestion-svc", "status": "unhealthy", "version": "1.0.0",
			})
			return
		}
		stats := svc.QueueStats()
		stats["service"] = "ingestion-svc"
		stats["version"] = "1.0.0"
		status := "healthy"
		if util, ok := stats["utilization_pct"].(float64); ok && util > 90 {
			status = "degraded"
		}
		stats["status"] = status
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}).Methods("GET")

	// Enqueue job
	r.HandleFunc("/ingestion/submit", submitJob(svc)).Methods("POST")

	// Queue stats
	r.HandleFunc("/ingestion/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svc.QueueStats())
	}).Methods("GET")

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Info().Int("port", *port).Msg("Ingestion service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)

	// R5-008: drain in-flight job goroutines before exit; unfinished jobs stay
	// 'in_progress' and are rescued by RecoverPending on next start.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer drainCancel()
	if err := svc.Shutdown(drainCtx); err != nil {
		log.Warn().Err(err).Msg("Job drain incomplete at shutdown; pending jobs will be recovered on restart")
	}
}

func submitJob(svc *ingestion.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Type           string                 `json:"type"`
			Payload        map[string]interface{} `json:"payload"`
			IdempotencyKey string                 `json:"idempotency_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, 400)
			return
		}
		// R5-002: idempotency keys are client-supplied and required. The old
		// type+UnixNano fallback made every retry a brand-new job.
		if req.IdempotencyKey == "" {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"idempotency_key is required"}`, 400)
			return
		}

		job, duplicate, err := svc.Enqueue(r.Context(), req.Type, req.Payload, req.IdempotencyKey)
		if err != nil {
			if strings.HasPrefix(err.Error(), "queue full") {
				w.Header().Set("Retry-After", "30")
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 503)
			} else if strings.Contains(err.Error(), "idempotency_key") {
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 400)
			} else {
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 500)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if duplicate {
			// 200 (not 202): nothing new was accepted; the existing job is
			// returned so clients can reconcile state.
			w.WriteHeader(200)
		} else {
			w.WriteHeader(202)
		}
		json.NewEncoder(w).Encode(job)
	}
}
