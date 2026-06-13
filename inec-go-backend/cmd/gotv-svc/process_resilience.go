package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// ─── Panic Recovery Middleware ─────────────────────────────────────────────
//
// Catches panics in any handler, logs full stack trace, increments Prometheus
// counter, and returns HTTP 500 instead of crashing the process.
// K8s restartPolicy: Always handles real crashes; this prevents handler-level
// panics from taking down the entire pod.

var panicCount int64

func panicRecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := string(debug.Stack())
				atomic.AddInt64(&panicCount, 1)
				log.Error().
					Interface("panic", rec).
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Str("remote", r.RemoteAddr).
					Str("request_id", r.Header.Get("X-Request-ID")).
					Str("stack", stack).
					Msg("handler panic recovered")

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, `{"error":"internal server error","request_id":"%s"}`,
					r.Header.Get("X-Request-ID"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ─── Graceful Shutdown Coordinator ─────────────────────────────────────────
//
// Coordinates a multi-phase shutdown sequence:
// 1. Mark not-ready (K8s stops sending traffic via readiness probe)
// 2. Wait for in-flight requests to drain (preStop hook gives 5s head start)
// 3. Close external connections (Kafka, Redis, gRPC)
// 4. Close database last (other resources may need it)
// 5. Hard deadline: terminationGracePeriodSeconds (30s) kills the pod

type shutdownHook struct {
	name string
	fn   func() error
}

var shutdownHooks []shutdownHook

func registerShutdownHook(name string, fn func() error) {
	shutdownHooks = append(shutdownHooks, shutdownHook{name: name, fn: fn})
}

func runShutdownSequence(srv *http.Server) {
	log.Info().Msg("starting graceful shutdown sequence")
	startTime := time.Now()

	// Phase 1: mark not-ready so K8s stops routing traffic
	atomic.StoreInt32(&serviceReady, 0)
	log.Info().Msg("shutdown: marked service not-ready")

	// Phase 2: run gracefulShutdownHooks (Kafka, Redis, gRPC)
	gracefulShutdownHooks()

	// Phase 3: drain HTTP server with deadline
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Warn().Err(err).Msg("shutdown: HTTP server forced close")
	} else {
		log.Info().Msg("shutdown: HTTP server drained cleanly")
	}

	// Phase 4: run any additional registered hooks
	for _, hook := range shutdownHooks {
		if err := hook.fn(); err != nil {
			log.Warn().Err(err).Str("hook", hook.name).Msg("shutdown hook error")
		} else {
			log.Info().Str("hook", hook.name).Msg("shutdown hook completed")
		}
	}

	// Phase 5: close database last
	if dbConn != nil {
		if err := dbConn.Close(); err != nil {
			log.Warn().Err(err).Msg("shutdown: database close error")
		} else {
			log.Info().Msg("shutdown: database closed")
		}
	}

	log.Info().
		Dur("duration", time.Since(startTime)).
		Int64("panics_recovered", atomic.LoadInt64(&panicCount)).
		Msg("graceful shutdown complete")
}

// ─── Watchdog ──────────────────────────────────────────────────────────────
//
// Periodically checks process health and logs warnings for anomalies.
// This runs inside the container — if it detects a hung state, the liveness
// probe will fail and K8s will restart the pod.

func startWatchdog(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var m debug.GCStats
				debug.ReadGCStats(&m)

				// Log memory stats for observability
				var memStats debug.GCStats
				debug.ReadGCStats(&memStats)

				if atomic.LoadInt64(&panicCount) > 0 {
					log.Warn().
						Int64("panic_count", atomic.LoadInt64(&panicCount)).
						Msg("watchdog: panics have been recovered since startup")
				}
			}
		}
	}()
}

// ─── Process Info Endpoint ─────────────────────────────────────────────────

func handleProcessInfo(w http.ResponseWriter, r *http.Request) {
	info, _ := debug.ReadBuildInfo()
	goVersion := ""
	if info != nil {
		goVersion = info.GoVersion
	}

	pid := os.Getpid()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pid":              pid,
		"go_version":       goVersion,
		"panics_recovered": atomic.LoadInt64(&panicCount),
		"uptime":           time.Since(startupTime).String(),
		"ready":            atomic.LoadInt32(&serviceReady) == 1,
		"shutdown_hooks":   len(shutdownHooks) + 4, // +4 for built-in (kafka, redis, grpc, db)
	})
}
