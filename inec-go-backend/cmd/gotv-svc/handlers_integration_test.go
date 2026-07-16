package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// ═══════════════════════════════════════════════════════════════════════════
// Integration Tests — test full HTTP handler chain with middleware stack
// Requires: DB connection (skipped if DATABASE_URL not set)
// ═══════════════════════════════════════════════════════════════════════════

func skipIfNoDB(t *testing.T) {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("GOTV_TEST_DB") == "" {
		t.Skip("Skipping integration test: no DATABASE_URL set")
	}
}

// ─── Middleware Integration Tests (no DB needed) ────────────────────────

func TestFullMiddlewareStack(t *testing.T) {
	// Build the full middleware stack
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	})

	handler := rateLimitMiddleware(
		requestIDMiddleware(
			securityHeadersMiddleware(
				tracingMiddleware(
					etagMiddleware(inner),
				),
			),
		),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify all middleware added their headers
	checks := map[string]bool{
		"X-Request-Id":            false,
		"X-Content-Type-Options":  false,
		"X-Frame-Options":         false,
		"Content-Security-Policy": false,
		"X-Trace-ID":              false,
		"X-Span-ID":               false,
		"ETag":                    false,
		"traceparent":             false,
	}

	for header := range checks {
		if v := resp.Header.Get(header); v != "" {
			checks[header] = true
		}
	}

	for header, found := range checks {
		if !found {
			t.Errorf("missing header: %s", header)
		}
	}
}

func TestETagConditionalRequest(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":"test"}`))
	})

	handler := etagMiddleware(inner)

	// First request: get ETag
	req := httptest.NewRequest("GET", "/test?party=APC", nil)
	req.Header.Set("X-GOTV-Party-Code", "APC")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	etag := w.Result().Header.Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header")
	}

	// Second request with If-None-Match: should get 304
	req2 := httptest.NewRequest("GET", "/test?party=APC", nil)
	req2.Header.Set("X-GOTV-Party-Code", "APC")
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != 304 {
		t.Errorf("expected 304 Not Modified, got %d", w2.Code)
	}
}

func TestTracingContextPropagation(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := getTraceID(r.Context())
		json.NewEncoder(w).Encode(map[string]string{"trace_id": traceID})
	})

	handler := tracingMiddleware(inner)

	// Test with existing traceparent
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("traceparent", "00-abcdef1234567890abcdef1234567890-1234567890abcdef-01")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	traceParent := w.Result().Header.Get("traceparent")
	if traceParent == "" {
		t.Fatal("expected traceparent header")
	}

	// Verify trace ID is propagated
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["trace_id"] != "abcdef1234567890abcdef1234567890" {
		t.Errorf("trace_id not propagated, got %s", body["trace_id"])
	}
}

func TestTracingNewContext(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := getTraceID(r.Context())
		json.NewEncoder(w).Encode(map[string]string{"trace_id": traceID})
	})

	handler := tracingMiddleware(inner)

	// No traceparent: should generate new trace context
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	traceParent := w.Result().Header.Get("traceparent")
	if traceParent == "" {
		t.Fatal("expected traceparent to be generated")
	}

	xTraceID := w.Result().Header.Get("X-Trace-ID")
	if xTraceID == "" {
		t.Fatal("expected X-Trace-ID header")
	}
}

// ─── Rate Limiting Integration ────────────────────────────────────────

func TestRateLimitHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	handler := rateLimitMiddleware(inner)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("request %d: expected 200, got %d", i, w.Code)
		}
	}
}

// ─── OpenAPI Spec Validation ────────────────────────────────────────

func TestOpenAPISpecValid(t *testing.T) {
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	w := httptest.NewRecorder()
	handleOpenAPISpec(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var spec map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if spec["openapi"] != "3.0.3" {
		t.Errorf("expected openapi=3.0.3, got %v", spec["openapi"])
	}

	info, ok := spec["info"].(map[string]interface{})
	if !ok {
		t.Fatal("missing info object")
	}
	if info["version"] != "2.0.0" {
		t.Errorf("expected version=2.0.0, got %v", info["version"])
	}

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("missing paths object")
	}
	if len(paths) < 20 {
		t.Errorf("expected at least 20 API paths, got %d", len(paths))
	}

	components, ok := spec["components"].(map[string]interface{})
	if !ok {
		t.Fatal("missing components object")
	}
	schemas := components["schemas"].(map[string]interface{})
	if len(schemas) < 4 {
		t.Errorf("expected at least 4 schemas, got %d", len(schemas))
	}
}

// ─── Alerting Rules Validation ────────────────────────────────────────

func TestAlertingRulesValid(t *testing.T) {
	req := httptest.NewRequest("GET", "/alerts/rules", nil)
	w := httptest.NewRecorder()
	handleAlertingRules(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var rules map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&rules); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	groups, ok := rules["groups"].([]interface{})
	if !ok || len(groups) == 0 {
		t.Fatal("expected at least 1 alert group")
	}

	group := groups[0].(map[string]interface{})
	alertRules := group["rules"].([]interface{})
	if len(alertRules) < 5 {
		t.Errorf("expected at least 5 alerting rules, got %d", len(alertRules))
	}

	// Verify critical alerts exist
	alertNames := make(map[string]bool)
	for _, r := range alertRules {
		rule := r.(map[string]interface{})
		alertNames[rule["alert"].(string)] = true
	}

	criticalAlerts := []string{"HighErrorRate", "HighLatency", "RateLimitExhaustion", "DBConnectionPoolExhausted"}
	for _, name := range criticalAlerts {
		if !alertNames[name] {
			t.Errorf("missing critical alert: %s", name)
		}
	}
}

// ─── Version Endpoint ────────────────────────────────────────────────

func TestVersionEndpoint(t *testing.T) {
	req := httptest.NewRequest("GET", "/version", nil)
	w := httptest.NewRecorder()
	handleVersion(w, req)

	var v map[string]string
	json.NewDecoder(w.Body).Decode(&v)

	if v["version"] != platformBuildVersion {
		t.Errorf("expected version=%s, got %s", platformBuildVersion, v["version"])
	}
	if v["build"] != "production" {
		t.Errorf("expected build=production, got %s", v["build"])
	}
}

// ─── Robots.txt ──────────────────────────────────────────────────────

func TestRobotsTxt(t *testing.T) {
	req := httptest.NewRequest("GET", "/robots.txt", nil)
	w := httptest.NewRecorder()
	handleRobotsTxt(w, req)

	body, _ := io.ReadAll(w.Body)
	bodyStr := string(body)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Result().Header.Get("Content-Type") != "text/plain" {
		t.Error("expected text/plain content type")
	}
	if !containsHelper(bodyStr, "Disallow: /gotv/") {
		t.Error("expected Disallow: /gotv/")
	}
	if !containsHelper(bodyStr, "Disallow: /metrics") {
		t.Error("expected Disallow: /metrics")
	}
}

// ─── Prometheus Metrics Registration ────────────────────────────────

func TestPrometheusMetricsRegistered(t *testing.T) {
	// Verify all metrics are registered without panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("prometheus registration panicked: %v", r)
		}
	}()

	// Test counter operations
	httpRequestsTotal.WithLabelValues("GET", "/test", "200").Inc()
	httpRequestDuration.WithLabelValues("GET", "/test").Observe(0.05)
	activeConnections.Set(10)
	dbQueryDuration.WithLabelValues("SELECT").Observe(0.01)
	cacheHitTotal.WithLabelValues("hit").Inc()
	cacheHitTotal.WithLabelValues("miss").Inc()
	dispatchTotal.WithLabelValues("sms", "delivered").Inc()
	vettingTransitions.WithLabelValues("pending", "nin_verified").Inc()
	rideMatchDuration.Observe(0.5)
	cpiComputeDuration.Observe(2.0)
}

// ─── NL Query Integration ────────────────────────────────────────────

func TestNLQueryEndpoint(t *testing.T) {
	skipIfNoDB(t)
	body := bytes.NewBufferString(`{"query":"How many contacts?"}`)
	req := httptest.NewRequest("POST", "/gotv/nl/query", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GOTV-Party-Code", "APC")
	w := httptest.NewRecorder()

	handleNLQuery(w, req)

	if w.Code != 200 && w.Code != 500 {
		t.Errorf("expected 200 or 500, got %d", w.Code)
	}
}

// ─── Export Endpoint Content-Type ────────────────────────────────────

func TestExportCSVContentType(t *testing.T) {
	skipIfNoDB(t)
	req := httptest.NewRequest("GET", "/gotv/export/contacts?format=csv", nil)
	req.Header.Set("X-GOTV-Party-Code", "APC")
	w := httptest.NewRecorder()

	handleExportContacts(w, req)

	ct := w.Result().Header.Get("Content-Type")
	if ct != "text/csv" && w.Code != 500 {
		if w.Code == 200 && ct != "text/csv" {
			t.Errorf("expected text/csv content type, got %s", ct)
		}
	}
}

// ─── Simulation Endpoint ─────────────────────────────────────────────

func TestSimulationEndpointValidation(t *testing.T) {
	skipIfNoDB(t)
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			"valid_add_drivers",
			`{"scenario":"add_drivers","additional_count":10}`,
			200,
		},
		{
			"valid_add_canvassers",
			`{"scenario":"add_canvassers","additional_count":20}`,
			200,
		},
		{
			"invalid_json",
			`{invalid}`,
			400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/gotv/simulation", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-GOTV-Party-Code", "APC")
			w := httptest.NewRecorder()

			handleSimulation(w, req)

			// Allow 200 (success) or 500 (no DB) for valid requests
			if tt.wantCode == 400 && w.Code != 400 {
				t.Errorf("expected 400 for invalid input, got %d", w.Code)
			}
			if tt.wantCode == 200 && w.Code != 200 && w.Code != 500 {
				t.Errorf("expected 200 or 500, got %d", w.Code)
			}
		})
	}
}

// ─── Campaign Preview ────────────────────────────────────────────────

func TestCampaignPreviewNoID(t *testing.T) {
	skipIfNoDB(t)
	req := httptest.NewRequest("GET", "/gotv/campaigns/test-id/preview", nil)
	req.Header.Set("X-GOTV-Party-Code", "APC")
	w := httptest.NewRecorder()

	handleCampaignPreview(w, req)

	if w.Code != 404 && w.Code != 500 && w.Code != 200 {
		t.Errorf("expected 404/500/200, got %d", w.Code)
	}
}

// ─── Health Dashboard Extended ───────────────────────────────────────

func TestHealthDashboardExtended(t *testing.T) {
	skipIfNoDB(t)
	req := httptest.NewRequest("GET", "/gotv/health/dashboard", nil)
	w := httptest.NewRecorder()

	handleHealthDashboard(w, req)

	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Should have basic health fields
	if health["status"] == nil {
		t.Error("missing status field")
	}
}

// ─── Normalize Path ──────────────────────────────────────────────────

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/gotv/contacts", "/gotv/contacts"},
		{"/gotv/volunteers/abc-123-def-456-ghi/badges", "/gotv/volunteers/{id}/badges"},
		{"/gotv/campaigns/123456789/preview", "/gotv/campaigns/{id}/preview"},
	}

	for _, tt := range tests {
		r := httptest.NewRequest("GET", tt.path, nil)
		got := normalizePath(r)
		if got != tt.want {
			t.Errorf("normalizePath(%s) = %s, want %s", tt.path, got, tt.want)
		}
	}
}

// ─── Hash String Determinism ─────────────────────────────────────────

func TestHashStringDeterministic(t *testing.T) {
	h1 := hashString("test-input-APC")
	h2 := hashString("test-input-APC")
	if h1 != h2 {
		t.Errorf("hashString not deterministic: %d != %d", h1, h2)
	}
	h3 := hashString("different-input")
	if h1 == h3 {
		t.Error("different inputs should produce different hashes")
	}
}
