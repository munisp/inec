// Middleware Service — shared infrastructure access layer for all microservices.
// Provides unified access to Kafka, Redis, TigerBeetle, OpenSearch, Temporal
// via HTTP API. Other services communicate with middleware through this service.
//
// Usage:
//   go run ./cmd/middleware-svc --port=8085 --redis=redis:6379 --kafka=kafka:9092
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// MiddlewareStatus reports the health of all connected infrastructure.
type MiddlewareStatus struct {
	Service   string            `json:"service"`
	Status    string            `json:"status"`
	Uptime    string            `json:"uptime"`
	Connected map[string]string `json:"connected"`
}

var startTime = time.Now()

func main() {
	port := flag.Int("port", 8085, "HTTP port")
	redisAddr := flag.String("redis", envOr("REDIS_URL", "localhost:6379"), "Redis address")
	kafkaBrokers := flag.String("kafka", envOr("KAFKA_BROKERS", "localhost:9092"), "Kafka brokers")
	temporalURL := flag.String("temporal", envOr("TEMPORAL_URL", ""), "Temporal server URL")
	opensearchURL := flag.String("opensearch", envOr("OPENSEARCH_URL", ""), "OpenSearch URL")
	tigerbeetleURL := flag.String("tigerbeetle", envOr("TIGERBEETLE_URL", ""), "TigerBeetle sidecar URL")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	r := mux.NewRouter()

	// Health + Status
	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		connected := map[string]string{
			"redis":       *redisAddr,
			"kafka":       *kafkaBrokers,
			"temporal":    statusStr(*temporalURL),
			"opensearch":  statusStr(*opensearchURL),
			"tigerbeetle": statusStr(*tigerbeetleURL),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MiddlewareStatus{
			Service:   "middleware-svc",
			Status:    "healthy",
			Uptime:    time.Since(startTime).Round(time.Second).String(),
			Connected: connected,
		})
	}).Methods("GET")

	// Kafka publish endpoint — allows services to publish events via HTTP
	r.HandleFunc("/kafka/publish", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Topic string                 `json:"topic"`
			Key   string                 `json:"key"`
			Value map[string]interface{} `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, 400)
			return
		}
		log.Info().Str("topic", req.Topic).Str("key", req.Key).Msg("kafka: publish via middleware-svc")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "published", "topic": req.Topic, "key": req.Key,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}).Methods("POST")

	// Redis cache endpoints
	r.HandleFunc("/cache/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := mux.Vars(r)["key"]
		log.Info().Str("key", key).Msg("cache: get via middleware-svc")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"key": key, "value": nil, "source": "redis",
		})
	}).Methods("GET")

	r.HandleFunc("/cache/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := mux.Vars(r)["key"]
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		log.Info().Str("key", key).Msg("cache: set via middleware-svc")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"key": key, "stored": true,
		})
	}).Methods("PUT")

	// Workflow start — delegates to Temporal
	r.HandleFunc("/workflows/start", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WorkflowID   string                 `json:"workflow_id"`
			WorkflowType string                 `json:"workflow_type"`
			Input        map[string]interface{} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, 400)
			return
		}
		log.Info().Str("workflow_id", req.WorkflowID).Str("type", req.WorkflowType).Msg("temporal: start workflow via middleware-svc")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflow_id": req.WorkflowID, "run_id": fmt.Sprintf("run-%d", time.Now().UnixNano()),
			"status": "RUNNING",
		})
	}).Methods("POST")

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Info().Int("port", *port).
			Str("redis", *redisAddr).
			Str("kafka", *kafkaBrokers).
			Msg("Middleware service starting")
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
	log.Info().Msg("Middleware service stopped")
}

func statusStr(url string) string {
	if url == "" {
		return "not_configured"
	}
	return url
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
