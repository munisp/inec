package main

// architecture.go — Service decomposition layer.
//
// This file bridges the existing monolith to a clean service-oriented
// architecture.  Each domain is isolated behind an interface and wired
// through a central ServiceRegistry.  Circuit breakers protect every
// external call, and an event bus decouples services.
//
// Domain boundaries:
//   auth          — authentication, sessions, token management
//   election      — election lifecycle, FSM, results, collation
//   biometric     — fingerprint/face verification, vault, ABIS
//   geo           — PostGIS queries, tracking, geofencing, landmarks
//   middleware    — Keycloak, Redis, Kafka, TigerBeetle, etc.
//   observability — tracing, metrics, logging, alerting

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"inec-go-backend/internal/circuit"
	"inec-go-backend/internal/eventbus"
	"inec-go-backend/internal/middleware"
	"inec-go-backend/internal/observability"

	"github.com/rs/zerolog/log"
)

// ── Service interfaces ──
// Each interface represents a bounded context that can be extracted to its own
// binary.  The current implementation is in-process; the interface stays the
// same when switching to gRPC/HTTP calls between microservices.

// AuthServiceIF handles authentication and session management.
// Existing implementations: handleLogin, handleLogout, handleRefresh in handlers.go
type AuthServiceIF interface {
	Login(ctx context.Context, username, password string) (map[string]interface{}, error)
	Logout(ctx context.Context, userID int, jti string) error
	ValidateToken(ctx context.Context, token string) (map[string]interface{}, error)
	RefreshToken(ctx context.Context, refreshToken string) (map[string]interface{}, error)
}

// ElectionServiceIF manages election lifecycle.
// Existing implementations: handleListElections, handleElectionFSMTransition in handlers.go/election_fsm.go
type ElectionServiceIF interface {
	ListElections(ctx context.Context) ([]map[string]interface{}, error)
	GetElection(ctx context.Context, id int) (map[string]interface{}, error)
	TransitionState(ctx context.Context, electionID int, action string) error
	SubmitResult(ctx context.Context, result map[string]interface{}) error
}

// BiometricServiceIF handles identity verification.
// Existing implementations: biometric_engine.go, biometric_advanced.go
type BiometricServiceIF interface {
	Verify(ctx context.Context, voterVIN string, template []byte, modality string) (map[string]interface{}, error)
	Enroll(ctx context.Context, voterVIN string, templates map[string][]byte) error
}

// GeoServiceIF handles geospatial operations.
// Existing implementations: geospatial_enhanced.go, geo_advanced.go
type GeoServiceIF interface {
	NearbyPUs(ctx context.Context, lat, lng, radiusKM float64) ([]map[string]interface{}, error)
	TrackOfficial(ctx context.Context, officialID string, lat, lng float64) error
	GetLandmarks(ctx context.Context) ([]map[string]interface{}, error)
	CheckGeofence(ctx context.Context, officialID string, lat, lng float64) (map[string]interface{}, error)
}

// ── Global registry (replaces scattered global vars) ──

var serviceRegistry *middleware.ServiceRegistry

// initArchitecture initializes the service-oriented architecture layer.
func initArchitecture() {
	serviceRegistry = middleware.NewServiceRegistry()

	// Configure circuit breakers for all external dependencies
	externalServices := []struct {
		name        string
		maxFail     int
		cooldown    time.Duration
	}{
		{"redis", 5, 30 * time.Second},
		{"kafka", 5, 60 * time.Second},
		{"keycloak", 3, 30 * time.Second},
		{"tigerbeetle", 5, 30 * time.Second},
		{"temporal", 5, 60 * time.Second},
		{"opensearch", 5, 30 * time.Second},
		{"postgresql", 3, 15 * time.Second},
		{"dapr", 5, 30 * time.Second},
		{"fluvio", 5, 60 * time.Second},
		{"apisix", 5, 30 * time.Second},
		{"mojaloop", 5, 60 * time.Second},
		{"openappsec", 5, 30 * time.Second},
		{"permify", 5, 30 * time.Second},
		{"document-ai", 5, 30 * time.Second},
	}

	for _, svc := range externalServices {
		cfg := circuit.Config{
			Name:             svc.name,
			MaxFailures:      svc.maxFail,
			CooldownDuration: svc.cooldown,
			OnStateChange: func(name string, from, to circuit.State) {
				// Update Prometheus metric
				observability.CircuitBreakerState.WithLabelValues(name).Set(float64(to))
				if to == circuit.Open {
					observability.CircuitBreakerTrips.WithLabelValues(name).Inc()
				}
				// Publish event
				if serviceRegistry != nil && serviceRegistry.EventBus != nil {
					serviceRegistry.EventBus.Publish(context.Background(), eventbus.Event{
						Type:      "circuit.state_changed",
						Source:    "architecture",
						Timestamp: time.Now(),
						Data:      map[string]interface{}{"service": name, "from": from.String(), "to": to.String()},
					})
				}
			},
		}
		serviceRegistry.Breakers.Register(cfg)
	}

	// Set up event bus — Redis-backed in production, local in dev
	if mwHub != nil && mwHub.Redis != nil {
		serviceRegistry.EventBus = eventbus.NewRedis(
			func(channel string, data []byte) error {
				return mwHub.Redis.Publish(context.Background(), channel, data)
			},
			nil, // Redis subscribe handled by middleware layer
		)
		log.Info().Msg("Event bus: Redis-backed (multi-pod safe)")
	} else {
		serviceRegistry.EventBus = eventbus.NewLocal()
		log.Info().Msg("Event bus: local (single-pod)")
	}

	// Set up distributed tracing
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint != "" {
		serviceRegistry.Tracer = observability.NewTracer("inec-backend", &observability.OTLPExporter{
			Endpoint: otlpEndpoint,
			Client:   &http.Client{Timeout: 5 * time.Second},
		})
		log.Info().Str("endpoint", otlpEndpoint).Msg("Tracing: OTLP exporter")
	} else {
		serviceRegistry.Tracer = observability.NewTracer("inec-backend", &observability.LogExporter{})
		log.Info().Msg("Tracing: log-based (set OTEL_EXPORTER_OTLP_ENDPOINT for Jaeger/Tempo)")
	}

	// Register known services
	registerMiddlewareServices()

	log.Info().Int("breakers", len(externalServices)).Msg("Architecture layer initialized")
}

func registerMiddlewareServices() {
	if mwHub == nil {
		return
	}

	services := []struct {
		name string
		check func() bool
		mode  string
	}{
		{"redis", func() bool { return mwHub.Redis != nil }, ""},
		{"kafka", func() bool { return mwHub.Kafka != nil }, ""},
		{"keycloak", func() bool { return mwHub.Keycloak != nil }, ""},
		{"tigerbeetle", func() bool { return mwHub.TigerBeetle != nil }, ""},
		{"temporal", func() bool { return mwHub.Temporal != nil }, ""},
		{"opensearch", func() bool { return mwHub.OpenSearch != nil }, ""},
		{"dapr", func() bool { return mwHub.Dapr != nil }, ""},
		{"fluvio", func() bool { return mwHub.Fluvio != nil }, ""},
		{"apisix", func() bool { return mwHub.APISIX != nil }, ""},
		{"mojaloop", func() bool { return mwHub.Mojaloop != nil }, ""},
		{"openappsec", func() bool { return mwHub.OpenAppSec != nil }, ""},
		{"permify", func() bool { return mwHub.Permify != nil }, ""},
	}

	for _, svc := range services {
		connected := svc.check()
		mode := "native"
		if !connected {
			mode = "embedded"
		}
		serviceRegistry.Register(middleware.ServiceInfo{
			Name:      svc.name,
			Mode:      mode,
			Connected: connected,
		})
	}
}

// ── Architecture health endpoint ──

func handleArchitectureHealth(w http.ResponseWriter, r *http.Request) {
	if serviceRegistry == nil {
		writeJSON(w, 503, M{"status": "not_initialized"})
		return
	}

	services := serviceRegistry.All()
	breakers := serviceRegistry.Breakers.All()

	writeJSON(w, 200, M{
		"status":           "healthy",
		"architecture":     "service-oriented-monolith",
		"services":         services,
		"circuit_breakers": breakers,
		"event_bus":        fmt.Sprintf("%T", serviceRegistry.EventBus),
		"tracing":          fmt.Sprintf("%T", serviceRegistry.Tracer.Exporter),
		"domain_services": []string{
			"auth", "election", "biometric", "geo",
			"middleware", "observability",
		},
	})
}

// ── Circuit breaker admin endpoint ──

func handleCircuitBreakers(w http.ResponseWriter, r *http.Request) {
	if serviceRegistry == nil {
		writeJSON(w, 503, M{"error": "not initialized"})
		return
	}
	writeJSON(w, 200, serviceRegistry.Breakers.All())
}

// ── Tracing middleware ──
// Wraps every request in a span for distributed tracing.

func otelTracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serviceRegistry == nil || serviceRegistry.Tracer == nil {
			next.ServeHTTP(w, r)
			return
		}

		ctx, endSpan := serviceRegistry.Tracer.StartSpan(r.Context(), r.Method+" "+r.URL.Path)
		r = r.WithContext(ctx)

		// Wrap response writer to capture status code
		rw := &statusCapture{ResponseWriter: w, code: 200}

		next.ServeHTTP(rw, r)

		var err error
		if rw.code >= 500 {
			err = fmt.Errorf("HTTP %d", rw.code)
		}
		endSpan(err)

		// Record RED metrics per service
		observability.ServiceRequestsTotal.WithLabelValues(
			"http", r.URL.Path, fmt.Sprintf("%d", rw.code),
		).Inc()
	})
}

type statusCapture struct {
	http.ResponseWriter
	code int
}

func (s *statusCapture) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush forwards http.Flusher so SSE streaming works through the wrapper.
func (s *statusCapture) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ── Event publishing helper ──

func publishEvent(eventType, source string, data map[string]interface{}) {
	if serviceRegistry == nil || serviceRegistry.EventBus == nil {
		return
	}
	event := eventbus.Event{
		ID:        fmt.Sprintf("%s-%d", eventType, time.Now().UnixNano()),
		Type:      eventType,
		Source:    source,
		Timestamp: time.Now(),
		Data:      data,
	}
	if err := serviceRegistry.EventBus.Publish(context.Background(), event); err != nil {
		log.Error().Err(err).Str("event", eventType).Msg("failed to publish event")
	}
	observability.EventsPublished.WithLabelValues(eventType, source).Inc()
}

// ── X-Forwarded-For aware IP extraction ──
// Fixes the rate limiter IP extraction to work behind load balancers.

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For (set by load balancers/CDNs)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP (client's real IP before any proxies)
		// X-Forwarded-For: client, proxy1, proxy2
		parts := splitTrim(xff, ",")
		if len(parts) > 0 {
			ip := parts[0]
			// Validate it looks like an IP (prevent header injection)
			if isValidIP(ip) {
				return ip
			}
		}
	}
	// Check X-Real-IP (set by nginx)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if isValidIP(xri) {
			return xri
		}
	}
	// Fall back to RemoteAddr
	return stripPort(r.RemoteAddr)
}

func splitTrim(s, sep string) []string {
	parts := make([]string, 0)
	for _, p := range splitString(s, sep) {
		trimmed := trimSpace(p)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func splitString(s, sep string) []string {
	result := make([]string, 0)
	for {
		idx := indexOf(s, sep)
		if idx < 0 {
			result = append(result, s)
			break
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func isValidIP(ip string) bool {
	// Simple validation: contains only digits, dots, colons (IPv6), and hex chars
	if len(ip) < 2 || len(ip) > 45 {
		return false
	}
	for _, c := range ip {
		if !((c >= '0' && c <= '9') || c == '.' || c == ':' || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
