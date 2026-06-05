package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Distributed tracing middleware using W3C Trace Context (traceparent header).
// When OTEL_EXPORTER_OTLP_ENDPOINT is set, traces are exported via OTLP/HTTP.
// Otherwise, trace IDs are propagated through headers and logged for correlation.

var (
	otelEndpoint string
	otelEnabled  bool
	serviceName  string
)

func initTracing() {
	otelEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	serviceName = envOrDefault("OTEL_SERVICE_NAME", "inec-backend")
	otelEnabled = otelEndpoint != ""
	if otelEnabled {
		log.Info().Str("endpoint", otelEndpoint).Str("service", serviceName).Msg("OpenTelemetry tracing enabled")
	} else {
		log.Info().Msg("OpenTelemetry tracing: header propagation only (set OTEL_EXPORTER_OTLP_ENDPOINT to enable export)")
	}
}

type traceContextKey struct{}

type TraceContext struct {
	TraceID  string
	SpanID   string
	ParentID string
}

func generateTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateSpanID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// tracingMiddleware injects/propagates W3C traceparent headers and logs trace context.
func tracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		tc := TraceContext{SpanID: generateSpanID()}

		// Parse incoming traceparent: version-traceId-parentId-flags
		if tp := r.Header.Get("traceparent"); tp != "" {
			parts := strings.Split(tp, "-")
			if len(parts) >= 4 && len(parts[1]) == 32 && len(parts[2]) == 16 {
				tc.TraceID = parts[1]
				tc.ParentID = parts[2]
			}
		}
		if tc.TraceID == "" {
			tc.TraceID = generateTraceID()
		}

		// Set outgoing traceparent
		traceparent := fmt.Sprintf("00-%s-%s-01", tc.TraceID, tc.SpanID)
		w.Header().Set("traceparent", traceparent)

		// Inject into context for downstream use
		ctx := context.WithValue(r.Context(), traceContextKey{}, &tc)
		r = r.WithContext(ctx)

		// Wrap response writer to capture status code
		rw := &statusResponseWriter{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(rw, r)

		duration := time.Since(start)

		// Structured log with trace correlation
		log.Info().
			Str("trace_id", tc.TraceID).
			Str("span_id", tc.SpanID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rw.statusCode).
			Dur("duration", duration).
			Str("remote_addr", r.RemoteAddr).
			Msg("request")

		// Export span to OTLP collector if enabled
		if otelEnabled {
			go exportSpan(tc, r.Method, r.URL.Path, rw.statusCode, duration)
		}
	})
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (w *statusResponseWriter) WriteHeader(code int) {
	if !w.written {
		w.statusCode = code
		w.written = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// GetTraceContext extracts trace context from request context.
func GetTraceContext(ctx context.Context) *TraceContext {
	if tc, ok := ctx.Value(traceContextKey{}).(*TraceContext); ok {
		return tc
	}
	return nil
}

// exportSpan sends a span to the OTLP collector via HTTP/JSON.
func exportSpan(tc TraceContext, method, path string, status int, duration time.Duration) {
	if otelEndpoint == "" {
		return
	}

	spanJSON := fmt.Sprintf(`{
		"resourceSpans": [{
			"resource": {"attributes": [{"key": "service.name", "value": {"stringValue": %q}}]},
			"scopeSpans": [{
				"spans": [{
					"traceId": %q,
					"spanId": %q,
					"parentSpanId": %q,
					"name": "%s %s",
					"kind": 2,
					"startTimeUnixNano": %d,
					"endTimeUnixNano": %d,
					"status": {"code": %d},
					"attributes": [
						{"key": "http.method", "value": {"stringValue": %q}},
						{"key": "http.url", "value": {"stringValue": %q}},
						{"key": "http.status_code", "value": {"intValue": %d}}
					]
				}]
			}]
		}]
	}`, serviceName, tc.TraceID, tc.SpanID, tc.ParentID,
		method, path,
		time.Now().Add(-duration).UnixNano(), time.Now().UnixNano(),
		otelStatusCode(status),
		method, path, status)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", otelEndpoint+"/v1/traces",
		strings.NewReader(spanJSON))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	traceClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := traceClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func otelStatusCode(httpStatus int) int {
	if httpStatus >= 400 {
		return 2 // ERROR
	}
	return 1 // OK
}
