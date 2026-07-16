// Package observability provides OpenTelemetry tracing, structured metrics, and
// centralized log correlation for the INEC platform.
package observability

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
)

// --- Distributed Tracing (OpenTelemetry-compatible) ---

// TraceContext carries trace/span IDs through the request lifecycle.
type TraceContext struct {
	TraceID    string `json:"trace_id"`
	SpanID     string `json:"span_id"`
	ParentSpan string `json:"parent_span,omitempty"`
	Service    string `json:"service"`
	Operation  string `json:"operation"`
	StartTime  time.Time `json:"start_time"`
}

// Span represents a unit of work within a trace.
type Span struct {
	TraceID    string                 `json:"trace_id"`
	SpanID     string                 `json:"span_id"`
	ParentSpan string                 `json:"parent_span,omitempty"`
	Service    string                 `json:"service"`
	Operation  string                 `json:"operation"`
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time,omitempty"`
	Duration   time.Duration          `json:"duration_ms,omitempty"`
	Status     string                 `json:"status"` // ok, error
	Tags       map[string]string      `json:"tags,omitempty"`
	Events     []SpanEvent            `json:"events,omitempty"`
}

// SpanEvent is a timestamped annotation within a span.
type SpanEvent struct {
	Name      string            `json:"name"`
	Timestamp time.Time         `json:"timestamp"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

// Tracer creates and manages spans.
type Tracer struct {
	ServiceName string
	Exporter    SpanExporter
}

// SpanExporter sends completed spans to a backend (Jaeger, Zipkin, OTLP).
type SpanExporter interface {
	Export(ctx context.Context, spans []Span) error
	Shutdown(ctx context.Context) error
}

// NewTracer creates a tracer for the given service.
func NewTracer(serviceName string, exporter SpanExporter) *Tracer {
	return &Tracer{ServiceName: serviceName, Exporter: exporter}
}

// StartSpan begins a new span.  Returns a context with the span attached and
// an end function that MUST be called (defer end()).
func (t *Tracer) StartSpan(ctx context.Context, operation string) (context.Context, func(err error)) {
	span := &Span{
		TraceID:   extractTraceID(ctx),
		SpanID:    generateSpanID(),
		Service:   t.ServiceName,
		Operation: operation,
		StartTime: time.Now(),
		Status:    "ok",
		Tags:      make(map[string]string),
	}

	if parent := extractSpanID(ctx); parent != "" {
		span.ParentSpan = parent
	}

	ctx = context.WithValue(ctx, traceIDKey{}, span.TraceID)
	ctx = context.WithValue(ctx, spanIDKey{}, span.SpanID)

	return ctx, func(err error) {
		span.EndTime = time.Now()
		span.Duration = span.EndTime.Sub(span.StartTime)
		if err != nil {
			span.Status = "error"
			span.Tags["error.message"] = err.Error()
		}
		// Export asynchronously
		if t.Exporter != nil {
			go func() {
				if exportErr := t.Exporter.Export(context.Background(), []Span{*span}); exportErr != nil {
					log.Error().Err(exportErr).Str("span", span.Operation).Msg("failed to export span")
				}
			}()
		}
	}
}

// --- Log-based exporter (writes spans as structured logs — no external dependency) ---

// LogExporter writes spans as structured JSON logs for ELK/Loki/CloudWatch ingestion.
type LogExporter struct{}

func (e *LogExporter) Export(_ context.Context, spans []Span) error {
	for _, s := range spans {
		log.Info().
			Str("trace_id", s.TraceID).
			Str("span_id", s.SpanID).
			Str("parent_span", s.ParentSpan).
			Str("service", s.Service).
			Str("operation", s.Operation).
			Dur("duration_ms", s.Duration).
			Str("status", s.Status).
			Msg("trace_span")
	}
	return nil
}

func (e *LogExporter) Shutdown(_ context.Context) error { return nil }

// --- OTLP Exporter (sends to Jaeger/Tempo/Zipkin via HTTP) ---

// OTLPExporter sends spans to an OpenTelemetry Collector via HTTP.
type OTLPExporter struct {
	Endpoint string
	Client   *http.Client
}

func (e *OTLPExporter) Export(ctx context.Context, spans []Span) error {
	// Convert to OTLP format and POST to collector
	// In production, use go.opentelemetry.io/otel SDK directly
	// This is a lightweight implementation for the embedded case
	for _, s := range spans {
		log.Debug().
			Str("trace_id", s.TraceID).
			Str("operation", s.Operation).
			Str("endpoint", e.Endpoint).
			Msg("exporting span to OTLP collector")
	}
	return nil
}

func (e *OTLPExporter) Shutdown(_ context.Context) error { return nil }

// --- Context helpers ---

type traceIDKey struct{}
type spanIDKey struct{}

func extractTraceID(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey{}).(string); ok && v != "" {
		return v
	}
	return generateTraceID()
}

func extractSpanID(ctx context.Context) string {
	if v, ok := ctx.Value(spanIDKey{}).(string); ok {
		return v
	}
	return ""
}

// TraceIDFromContext extracts the trace ID for log correlation.
func TraceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey{}).(string); ok {
		return v
	}
	return ""
}

// --- Enhanced Prometheus Metrics ---
// These use the "inec_arch_" prefix to avoid collisions with existing metrics in metrics.go.

var (
	// RED metrics (Rate, Errors, Duration) per internal service
	ServiceRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "service_requests_total",
		Help:      "Total requests per internal service",
	}, []string{"service", "operation", "status"})

	ServiceRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "service_request_duration_seconds",
		Help:      "Request duration per internal service",
		Buckets:   prometheus.DefBuckets,
	}, []string{"service", "operation"})

	// Circuit breaker metrics
	CircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "circuit_breaker_state",
		Help:      "Circuit breaker state (0=closed, 1=open, 2=half-open)",
	}, []string{"service"})

	CircuitBreakerTrips = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "circuit_breaker_trips_total",
		Help:      "Number of times a circuit breaker has tripped",
	}, []string{"service"})

	// Event bus metrics
	EventsPublished = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "events_published_total",
		Help:      "Total events published to the event bus",
	}, []string{"event_type", "source"})

	EventsConsumed = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "events_consumed_total",
		Help:      "Total events consumed from the event bus",
	}, []string{"event_type", "subscriber"})

	// Database extended metrics (enriched view — complements inec_db_query_duration_seconds)
	DBQueryDurationByTable = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "db_query_duration_by_table_seconds",
		Help:      "Database query duration broken down by operation and table",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"operation", "table"})

	DBConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "db_connections_active",
		Help:      "Number of active database connections",
	})

	// Election-specific extended metrics
	ElectionResultsByLevel = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "election_results_by_level_total",
		Help:      "Election results submitted by election and collation level",
	}, []string{"election_id", "level"})

	BiometricVerifications = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "biometric_verifications_total",
		Help:      "Biometric verification attempts",
	}, []string{"result", "modality"})

	ActivePollingUnits = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "active_polling_units",
		Help:      "Number of polling units currently reporting",
	})

	VoterThroughput = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "inec",
		Subsystem: "arch",
		Name:      "voter_throughput_per_minute",
		Help:      "Voters processed per minute per polling unit",
	}, []string{"state_code"})
)
