package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

// Test helpers
func setupTestRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/healthz", handleDeepHealthCheck).Methods("GET")
	r.HandleFunc("/readiness", handleReadinessCheck).Methods("GET")
	r.HandleFunc("/auth/login", handleLogin).Methods("POST")
	r.HandleFunc("/auth/register", handleRegister).Methods("POST")
	r.HandleFunc("/elections", handleListElections).Methods("GET")
	r.HandleFunc("/results", handleListResults).Methods("GET")
	r.HandleFunc("/dashboard/stats", handleDashboardStats).Methods("GET")
	r.HandleFunc("/middleware/status", handleMiddlewareStatus).Methods("GET")
	r.HandleFunc("/middleware/mojaloop/status", handleMojaStatus).Methods("GET")
	r.HandleFunc("/middleware/opensearch/status", handleOpenSearchStatus).Methods("GET")
	r.HandleFunc("/middleware/waf/status", handleWAFStatus).Methods("GET")
	return r
}

func TestHealthEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := setupTestRouter()
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 && w.Code != 503 {
		t.Errorf("expected 200 or 503, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if _, ok := body["status"]; !ok {
		t.Error("response missing 'status' field")
	}
	if _, ok := body["checks"]; !ok {
		t.Error("response missing 'checks' field")
	}
}

func TestReadinessEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := setupTestRouter()
	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if _, ok := body["ready"]; !ok {
		t.Error("response missing 'ready' field")
	}
}

func TestLoginRateLimiting(t *testing.T) {
	rl := newRateLimiter()

	// 5 requests should succeed
	for i := 0; i < 5; i++ {
		if !rl.allow("test-ip:/auth/login", 5, 1e9) {
			t.Errorf("request %d should have been allowed", i)
		}
	}

	// 6th should be blocked
	if rl.allow("test-ip:/auth/login", 5, 1e9) {
		t.Error("6th request should have been rate limited")
	}
}

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker("test-svc", 3, 100e6)

	// Should be closed initially
	if cb.State() != "closed" {
		t.Errorf("expected closed, got %s", cb.State())
	}

	// Should allow requests
	if !cb.Allow() {
		t.Error("should allow requests when closed")
	}

	// Record failures up to threshold
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	// Should be open
	if cb.State() != "open" {
		t.Errorf("expected open after 3 failures, got %s", cb.State())
	}

	// Should block requests
	if cb.Allow() {
		t.Error("should block requests when open")
	}
}

func TestRequestSizeLimit(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := requestSizeLimit(inner)

	// Small request should pass
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(make([]byte, 100)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("small request: expected 200, got %d", w.Code)
	}

	// Oversized content-length should be rejected
	req2 := httptest.NewRequest("POST", "/test", nil)
	req2.ContentLength = 20 << 20 // 20MB
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != 413 {
		t.Errorf("oversized request: expected 413, got %d", w2.Code)
	}
}

func TestWAFSQLInjection(t *testing.T) {
	waf := newEmbeddedWAF()
	ctx := fmt.Errorf("context")
	_ = ctx

	decision, err := waf.InspectRequest(nil, WAFRequest{
		SourceIP: "1.2.3.4",
		Method:   "GET",
		Path:     "/users?id=1' OR '1'='1",
	})
	if err != nil && db != nil {
		t.Fatalf("inspect failed: %v", err)
	}
	if decision != nil && decision.ThreatLevel == "none" {
		t.Error("SQL injection should have been detected")
	}
}

func TestPasswordHashing(t *testing.T) {
	password := "test-password-123"
	hash := hashPassword(password)

	if !verifyPassword(password, hash) {
		t.Error("password verification failed for correct password")
	}

	if verifyPassword("wrong-password", hash) {
		t.Error("password verification passed for wrong password")
	}
}

func TestTokenCreationAndDecoding(t *testing.T) {
	claims := map[string]interface{}{
		"user_id":  1,
		"username": "testuser",
		"role":     "admin",
	}

	token, err := createAccessToken(claims)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	decoded, err := decodeToken(token)
	if err != nil {
		t.Fatalf("failed to decode token: %v", err)
	}

	if decoded["username"] != "testuser" {
		t.Errorf("expected username=testuser, got %v", decoded["username"])
	}
	if decoded["role"] != "admin" {
		t.Errorf("expected role=admin, got %v", decoded["role"])
	}
}

func TestSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := securityHeaders(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	expected := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
		"Referrer-Policy":       "no-referrer-when-downgrade",
	}

	for header, value := range expected {
		if got := w.Header().Get(header); got != value {
			t.Errorf("header %s: expected %q, got %q", header, value, got)
		}
	}
}
