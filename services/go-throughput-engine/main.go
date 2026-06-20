// Package main implements a high-throughput transaction engine optimized for
// millions of TPS across Kafka, Redis, TigerBeetle, Postgres, Mojaloop,
// Temporal, APISIX, Dapr, Permify, and OpenSearch.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg := LoadConfig()

	// Initialize all optimized middleware pipelines
	engine, err := NewThroughputEngine(cfg, logger)
	if err != nil {
		logger.Fatal("failed to initialize engine", zap.Error(err))
	}
	defer engine.Shutdown()

	// Start worker pools
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine.Start(ctx)

	// HTTP health/metrics server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", engine.HealthHandler)
	mux.HandleFunc("/metrics", engine.MetricsHandler)
	mux.HandleFunc("/api/v1/ingest", engine.IngestHandler)
	mux.HandleFunc("/api/v1/batch", engine.BatchHandler)
	mux.HandleFunc("/api/v1/stats", engine.StatsHandler)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Info("throughput engine started", zap.Int("port", cfg.Port))
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
}
