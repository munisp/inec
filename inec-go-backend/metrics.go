package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inec_http_requests_total",
			Help: "Total HTTP requests by method, path, and status",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "inec_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	activeConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "inec_active_connections",
			Help: "Currently active HTTP connections",
		},
	)

	resultsSubmitted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inec_results_submitted_total",
			Help: "Election results submitted by state and status",
		},
		[]string{"state_code", "status"},
	)

	middlewareHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "inec_middleware_healthy",
			Help: "Middleware component health (1=healthy, 0=unhealthy)",
		},
		[]string{"component"},
	)

	dbQueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "inec_db_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: []float64{.001, .005, .01, .05, .1, .5, 1, 5},
		},
		[]string{"query_type"},
	)
)

func initMetrics() {
	prometheus.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		activeConnections,
		resultsSubmitted,
		middlewareHealth,
		dbQueryDuration,
	)
}

func metricsHandler() http.Handler {
	return promhttp.Handler()
}

func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeConnections.Inc()
		defer activeConnections.Dec()

		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)

		path := sanitizePath(r.URL.Path)
		httpRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(rw.status)).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration.Seconds())
	})
}

// sanitizePath normalizes high-cardinality path segments to prevent metric explosion.
func sanitizePath(path string) string {
	// Collapse IDs to placeholders
	segments := []string{
		"/elections/", "/results/", "/incidents/", "/bvas/",
		"/blockchain/verify/", "/stakeholders/",
	}
	for _, seg := range segments {
		if len(path) > len(seg) && path[:len(seg)] == seg {
			return seg + ":id"
		}
	}
	return path
}
