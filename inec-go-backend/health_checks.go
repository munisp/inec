package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// HealthStatus represents the overall health of the platform.
type HealthStatus struct {
	Status    string                    `json:"status"`
	Timestamp time.Time                 `json:"timestamp"`
	Version   string                    `json:"version"`
	Uptime    string                    `json:"uptime"`
	Checks    map[string]*ComponentHealth `json:"checks"`
	System    SystemInfo                `json:"system"`
}

// ComponentHealth holds the health result for a single dependency.
type ComponentHealth struct {
	Status  string        `json:"status"`
	Latency string        `json:"latency,omitempty"`
	Error   string        `json:"error,omitempty"`
	Details interface{}   `json:"details,omitempty"`
}

// SystemInfo holds runtime metrics.
type SystemInfo struct {
	GoVersion  string `json:"go_version"`
	NumCPU     int    `json:"num_cpu"`
	NumGoroutine int  `json:"num_goroutine"`
	MemAllocMB float64 `json:"mem_alloc_mb"`
}

var startTime = time.Now()

// HealthHandler returns a comprehensive health check response.
func HealthHandler(db *sql.DB, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		checks := make(map[string]*ComponentHealth)
		var mu sync.Mutex
		var wg sync.WaitGroup

		// Check PostgreSQL
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			ch := &ComponentHealth{Status: "healthy"}
			if db != nil {
				if err := db.PingContext(ctx); err != nil {
					ch.Status = "unhealthy"
					ch.Error = err.Error()
				} else {
					stats := db.Stats()
					ch.Details = map[string]interface{}{
						"open_connections": stats.OpenConnections,
						"in_use":           stats.InUse,
						"idle":             stats.Idle,
					}
				}
			} else {
				ch.Status = "unknown"
				ch.Error = "db not initialised"
			}
			ch.Latency = time.Since(start).String()
			mu.Lock()
			checks["postgresql"] = ch
			mu.Unlock()
		}()

		// Check Redis
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			ch := &ComponentHealth{Status: "healthy"}
			if rdb != nil {
				if err := rdb.Ping(ctx).Err(); err != nil {
					ch.Status = "unhealthy"
					ch.Error = err.Error()
				}
			} else {
				ch.Status = "unknown"
				ch.Error = "redis not initialised"
			}
			ch.Latency = time.Since(start).String()
			mu.Lock()
			checks["redis"] = ch
			mu.Unlock()
		}()

		wg.Wait()

		// Determine overall status
		overall := "healthy"
		for _, ch := range checks {
			if ch.Status == "unhealthy" {
				overall = "degraded"
				break
			}
		}

		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		status := HealthStatus{
			Status:    overall,
			Timestamp: time.Now().UTC(),
			Version:   "1.0.0",
			Uptime:    time.Since(startTime).Round(time.Second).String(),
			Checks:    checks,
			System: SystemInfo{
				GoVersion:    runtime.Version(),
				NumCPU:       runtime.NumCPU(),
				NumGoroutine: runtime.NumGoroutine(),
				MemAllocMB:   float64(memStats.Alloc) / 1024 / 1024,
			},
		}

		httpStatus := http.StatusOK
		if overall == "degraded" {
			httpStatus = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		json.NewEncoder(w).Encode(status)
	}
}

// ReadinessHandler is a lightweight probe used by Kubernetes readiness checks.
func ReadinessHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if db != nil {
			if err := db.PingContext(ctx); err != nil {
				http.Error(w, `{"ready":false}`, http.StatusServiceUnavailable)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ready":true}`))
	}
}

// LivenessHandler is a minimal probe — just confirms the process is alive.
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"alive":true}`))
	}
}
