// Ingestion Service — independently deployable async job processing service.
// Handles: Batch imports, backpressure, dead-letter queue, retry with exponential backoff.
//
// Usage:
//   go run ./cmd/ingestion-svc --port=8095 --db=postgres://...
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
	"syscall"
	"time"

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
		*dbURL = "postgres://ngapp:ngapp123@localhost:5432/ngapp?sslmode=disable"
	}

	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()

	svc := ingestion.NewService(db)

	// Recover pending jobs from previous runs
	recovered := svc.RecoverPending(context.Background())
	log.Info().Int("recovered", recovered).Msg("Startup recovery complete")

	r := mux.NewRouter()

	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
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
		if req.IdempotencyKey == "" {
			req.IdempotencyKey = fmt.Sprintf("%s_%d", req.Type, time.Now().UnixNano())
		}

		job, err := svc.Enqueue(r.Context(), req.Type, req.Payload, req.IdempotencyKey)
		if err != nil {
			if err.Error()[:10] == "queue full" {
				w.Header().Set("Retry-After", "30")
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 503)
			} else {
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 500)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(job)
	}
}
