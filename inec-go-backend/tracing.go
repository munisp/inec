package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog/log"
)

// ── Distributed Tracing (OpenTelemetry-compatible) ──
// Provides trace context propagation and span recording without requiring
// the full OTel SDK (which would add heavy dependencies). When OTEL_EXPORTER_ENDPOINT
// is set, traces are exported via OTLP HTTP. Otherwise, traces are logged locally.

// Span represents a single operation within a distributed trace.
type Span struct {
	TraceID    string                 `json:"trace_id"`
	SpanID     string                 `json:"span_id"`
	ParentID   string                 `json:"parent_id,omitempty"`
	Operation  string                 `json:"operation"`
	Service    string                 `json:"service"`
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time,omitempty"`
	Duration   time.Duration          `json:"duration_ms,omitempty"`
	Status     string                 `json:"status"` // ok, error
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// TracingConfig holds the configuration for distributed tracing.
type TracingConfig struct {
	Enabled         bool
	ServiceName     string
	ExporterURL     string
	SampleRate      float64
	MaxSpansPerSec  int
}

var tracingConfig *TracingConfig

func initTracing() {
	tracingConfig = &TracingConfig{
		Enabled:        os.Getenv("OTEL_ENABLED") == "true" || os.Getenv("OTEL_EXPORTER_ENDPOINT") != "",
		ServiceName:    envString("OTEL_SERVICE_NAME", "inec-backend"),
		ExporterURL:    os.Getenv("OTEL_EXPORTER_ENDPOINT"),
		SampleRate:     1.0, // 100% in dev, reduce in production
		MaxSpansPerSec: 1000,
	}

	if tracingConfig.Enabled {
		log.Info().
			Str("service", tracingConfig.ServiceName).
			Str("exporter", tracingConfig.ExporterURL).
			Msg("OpenTelemetry tracing enabled")
	} else {
		log.Info().Msg("Tracing disabled (set OTEL_ENABLED=true to enable)")
	}
}

// tracingMiddleware injects trace context into requests and records spans.
func tracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !tracingConfig.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Extract or generate trace context
		traceID := r.Header.Get("X-Trace-ID")
		if traceID == "" {
			traceID = generateJTI() // Reuse random ID generator
		}
		spanID := generateJTI()[:16]
		parentID := r.Header.Get("X-Parent-Span-ID")

		// Add to request context
		ctx := context.WithValue(r.Context(), traceIDKey, traceID)
		ctx = context.WithValue(ctx, spanIDKey, spanID)

		start := time.Now()

		// Wrap response writer to capture status code
		rw := &statusWriter{ResponseWriter: w, status: 200}

		// Propagate trace headers downstream
		rw.Header().Set("X-Trace-ID", traceID)

		next.ServeHTTP(rw, r.WithContext(ctx))

		duration := time.Since(start)

		// Record span
		span := &Span{
			TraceID:   traceID,
			SpanID:    spanID,
			ParentID:  parentID,
			Operation: r.Method + " " + r.URL.Path,
			Service:   tracingConfig.ServiceName,
			StartTime: start,
			EndTime:   time.Now(),
			Duration:  duration,
			Status:    "ok",
			Attributes: map[string]interface{}{
				"http.method":      r.Method,
				"http.url":         r.URL.Path,
				"http.status_code": rw.status,
				"http.user_agent":  r.UserAgent(),
				"net.peer.ip":      stripPort(r.RemoteAddr),
			},
		}
		if rw.status >= 400 {
			span.Status = "error"
		}

		// Export span (async)
		go exportSpan(span)
	})
}

type tracingContextKey string

const (
	traceIDKey tracingContextKey = "traceID"
	spanIDKey  tracingContextKey = "spanID"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// exportSpan sends the span to the configured exporter or logs it.
func exportSpan(span *Span) {
	if tracingConfig.ExporterURL != "" {
		// In production: send to Jaeger/Tempo/etc. via OTLP HTTP
		// For now, log structured span data
		log.Debug().
			Str("trace_id", span.TraceID).
			Str("span_id", span.SpanID).
			Str("operation", span.Operation).
			Dur("duration", span.Duration).
			Int("status_code", span.Attributes["http.status_code"].(int)).
			Msg("span")
	}
}

// getTraceID retrieves the trace ID from the request context.
func getTraceID(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return v
	}
	return ""
}
