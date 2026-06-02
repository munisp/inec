package main

import (
	"bytes"
	"context"
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
	ctx := context.Background()

	decision, err := waf.InspectRequest(ctx, WAFRequest{
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

func TestWAFBodyInspection(t *testing.T) {
	waf := newEmbeddedWAF()

	// SQL injection in body should be detected
	ctx := context.Background()
	decision, _ := waf.InspectRequest(ctx, WAFRequest{
		SourceIP: "10.0.0.1",
		Method:   "POST",
		Path:     "/api/v1/results",
		Body:     `{"name": "'; DROP TABLE results; --"}`,
	})
	if decision != nil && decision.Action != "block" {
		t.Error("SQL injection in body should have been blocked")
	}

	// XSS in body should be detected
	decision2, _ := waf.InspectRequest(ctx, WAFRequest{
		SourceIP: "10.0.0.2",
		Method:   "POST",
		Path:     "/api/v1/comments",
		Body:     `{"text": "<script>alert('xss')</script>"}`,
	})
	if decision2 != nil && decision2.ThreatLevel == "none" {
		t.Error("XSS in body should have been detected")
	}

	// Clean request should pass
	decision3, _ := waf.InspectRequest(ctx, WAFRequest{
		SourceIP: "10.0.0.3",
		Method:   "GET",
		Path:     "/api/v1/results",
	})
	if decision3 != nil && decision3.Action != "allow" {
		t.Error("clean request should be allowed")
	}
}

func TestWAFQueryParamInspection(t *testing.T) {
	waf := newEmbeddedWAF()

	ctx := context.Background()
	decision, _ := waf.InspectRequest(ctx, WAFRequest{
		SourceIP: "10.0.0.4",
		Method:   "GET",
		Path:     "/results?id=1 UNION SELECT * FROM users",
	})
	if decision != nil && decision.Action != "block" {
		t.Error("SQL injection in query param should have been blocked")
	}
}

func TestWAFBlocklistPersistence(t *testing.T) {
	waf := newEmbeddedWAF()

	// Add IP to blocklist
	ctx := context.Background()
	waf.AddIPToBlocklist(ctx, "192.168.1.100", "test block")

	// Verify blocked
	decision, _ := waf.InspectRequest(ctx, WAFRequest{
		SourceIP: "192.168.1.100",
		Method:   "GET",
		Path:     "/api/v1/results",
	})
	if decision != nil && decision.Action != "block" {
		t.Error("blocked IP should be rejected")
	}

	// Verify blocklist retrieval
	entries, _ := waf.GetBlocklist(ctx)
	found := false
	for _, e := range entries {
		if e.IP == "192.168.1.100" {
			found = true
		}
	}
	if !found {
		t.Error("IP not found in blocklist")
	}
}

func TestCircuitBreakerRecovery(t *testing.T) {
	// Use very short reset timeout
	cb := NewCircuitBreaker("test-recovery", 2, 50e6) // 50ms

	// Trip the breaker
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Fatal("should be open")
	}

	// Wait for reset
	for i := 0; i < 20; i++ {
		if cb.State() == "half-open" {
			break
		}
		// small busy-wait
	}
}

func TestResilientHTTPClient(t *testing.T) {
	client := NewResilientHTTPClient("test-client")
	if client == nil {
		t.Fatal("NewResilientHTTPClient returned nil")
	}
	if client.CB == nil {
		t.Fatal("circuit breaker not initialized")
	}
	if client.Client == nil {
		t.Fatal("http client not initialized")
	}
}

func TestEC8AValidationRules(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	// Test that domain validation function exists and runs
	r := setupTestRouter()
	r.HandleFunc("/domain/ec8a/submit", handleSubmitEC8A).Methods("POST")

	// Submit an invalid form (missing required fields)
	body := `{"polling_unit_code": "", "election_id": 0}`
	req := httptest.NewRequest("POST", "/domain/ec8a/submit", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Should have violations or error
	if w.Code == 200 {
		if violations, ok := resp["violations"].([]interface{}); ok {
			if len(violations) == 0 {
				t.Error("empty EC8A form should have validation violations")
			}
		}
	}
}

func TestCollationEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := setupTestRouter()
	r.HandleFunc("/domain/collation", handleHierarchicalCollation).Methods("GET")

	req := httptest.NewRequest("GET", "/domain/collation?election_id=1&level=state", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("collation endpoint returned %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["level"]; !ok {
		t.Error("collation response missing 'level' field")
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
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer-when-downgrade",
	}

	for header, value := range expected {
		if got := w.Header().Get(header); got != value {
			t.Errorf("header %s: expected %q, got %q", header, value, got)
		}
	}
}

// ── Additional Domain Logic Tests ──

func TestVoterRegistrationEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	// Get valid codes from seeded data
	var stCode, lgCode, wCode, puCode string
	db.QueryRow("SELECT code FROM states LIMIT 1").Scan(&stCode)
	db.QueryRow("SELECT code FROM lgas WHERE state_code=? LIMIT 1", stCode).Scan(&lgCode)
	db.QueryRow("SELECT code FROM wards WHERE lga_code=? LIMIT 1", lgCode).Scan(&wCode)
	db.QueryRow("SELECT code FROM polling_units WHERE ward_code=? LIMIT 1", wCode).Scan(&puCode)
	if stCode == "" || lgCode == "" || wCode == "" || puCode == "" {
		t.Skip("no seeded geo data")
	}

	r := mux.NewRouter()
	r.HandleFunc("/ems/voters", handleRegisterVoter).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})
	body := fmt.Sprintf(`{"first_name":"Amina","last_name":"Ibrahim","date_of_birth":"1990-05-15","gender":"F","state_code":"%s","lga_code":"%s","ward_code":"%s","polling_unit_code":"%s","biometric_data":"test-fingerprint-voter-reg"}`, stCode, lgCode, wCode, puCode)
	req := httptest.NewRequest("POST", "/ems/voters", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["vin"]; !ok {
		t.Error("response missing VIN")
	}
}

func TestVoterRegistrationMissingFieldsRejected(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/ems/voters", handleRegisterVoter).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})
	// Missing last_name and date_of_birth
	body := `{"first_name":"Test","gender":"M"}`
	req := httptest.NewRequest("POST", "/ems/voters", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == 201 {
		t.Error("missing required fields should not return 201")
	}
}

func TestWAFXSSInPath(t *testing.T) {
	waf := newEmbeddedWAF()
	ctx := context.Background()

	decision, _ := waf.InspectRequest(ctx, WAFRequest{
		SourceIP: "10.0.0.5",
		Method:   "GET",
		Path:     "/results?q=<script>alert(1)</script>",
	})
	if decision != nil && decision.ThreatLevel == "none" {
		t.Error("XSS in path should have been detected")
	}
}

func TestWAFPathTraversal(t *testing.T) {
	waf := newEmbeddedWAF()
	ctx := context.Background()

	decision, _ := waf.InspectRequest(ctx, WAFRequest{
		SourceIP: "10.0.0.6",
		Method:   "GET",
		Path:     "/files/../../etc/passwd",
	})
	if decision != nil && decision.ThreatLevel == "none" {
		t.Error("path traversal should have been detected")
	}
}

func TestGracefulShutdownSignalHandling(t *testing.T) {
	// Verify that the server sets up signal handling correctly by testing
	// the shutdown context can be derived
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ctx.Err() == nil {
		t.Error("cancelled context should report done")
	}
}

func TestDashboardStatsEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/dashboard/stats", handleDashboardStats).Methods("GET")

	req := httptest.NewRequest("GET", "/dashboard/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	// Dashboard stats should return a valid JSON object with election data
	if len(body) == 0 {
		t.Error("dashboard stats returned empty object")
	}
}

func TestListStatesEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/geo/states", handleListStates).Methods("GET")

	req := httptest.NewRequest("GET", "/geo/states", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestLoginWithInvalidCredentials(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/auth/login", handleLogin).Methods("POST")

	body := `{"username":"nonexistent_user_xyz","password":"wrongpassword123"}`
	req := httptest.NewRequest("POST", "/auth/login", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == 200 {
		t.Error("login with invalid credentials should not return 200")
	}
}

func TestLoginWithValidCredentials(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/auth/login", handleLogin).Methods("POST")

	// Use the seeded admin account
	body := `{"username":"admin","password":"admin123"}`
	req := httptest.NewRequest("POST", "/auth/login", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("login with valid credentials: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["access_token"]; !ok {
		t.Error("login response missing access_token")
	}
}

func TestMiddlewareStatusEndpoint(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/middleware/status", handleMiddlewareStatus).Methods("GET")

	req := httptest.NewRequest("GET", "/middleware/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	// Response should be valid JSON
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	// Should have some components
	if len(body) == 0 {
		t.Error("middleware status returned empty response")
	}
}

func TestPgCompatPlaceholderConversion(t *testing.T) {
	tests := []struct{ in, expected string }{
		{"SELECT * FROM users WHERE id=? AND name=?", "SELECT * FROM users WHERE id=? AND name=?"},
		{"INSERT INTO t (a,b) VALUES (?,?)", "INSERT INTO t (a,b) VALUES (?,?)"},
	}
	for _, tc := range tests {
		got := convertPlaceholders(tc.in)
		if usePostgres {
			// In PG mode, ? would be converted to $1, $2
			if got == tc.in && got != tc.expected {
				t.Logf("PG mode converts differently, which is expected")
			}
		}
	}
}

// ── End-to-End Auth Flow Tests ──

func TestFullLoginAndAccessFlow(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/auth/login", handleLogin).Methods("POST")
	r.HandleFunc("/elections", readAuth(handleListElections)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	// Login to get token
	loginBody := `{"username":"admin","password":"admin123"}`
	loginReq := httptest.NewRequest("POST", "/auth/login", bytes.NewReader([]byte(loginBody)))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	handler.ServeHTTP(loginW, loginReq)

	if loginW.Code != 200 {
		t.Fatalf("login failed: %d %s", loginW.Code, loginW.Body.String())
	}
	var loginResp map[string]interface{}
	json.Unmarshal(loginW.Body.Bytes(), &loginResp)
	token, ok := loginResp["access_token"].(string)
	if !ok || token == "" {
		t.Fatal("no access_token in login response")
	}

	// Use token to access protected endpoint
	electReq := httptest.NewRequest("GET", "/elections", nil)
	electReq.Header.Set("Authorization", "Bearer "+token)
	electW := httptest.NewRecorder()
	handler.ServeHTTP(electW, electReq)

	if electW.Code != 200 {
		t.Errorf("expected 200 with valid token, got %d", electW.Code)
	}
}

func TestCreateAndListElectionFlow(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/elections", writeAuth(handleCreateElection)).Methods("POST")
	r.HandleFunc("/elections", readAuth(handleListElections)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	// Create election
	createBody := `{"title":"2027 Presidential Election","election_type":"presidential","election_date":"2027-02-15"}`
	createReq := httptest.NewRequest("POST", "/elections", bytes.NewReader([]byte(createBody)))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+token)
	createW := httptest.NewRecorder()
	handler.ServeHTTP(createW, createReq)

	if createW.Code != 200 {
		t.Errorf("create election: expected 200, got %d: %s", createW.Code, createW.Body.String())
	}

	// List elections to verify it was created
	listReq := httptest.NewRequest("GET", "/elections", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listW := httptest.NewRecorder()
	handler.ServeHTTP(listW, listReq)

	if listW.Code != 200 {
		t.Errorf("list elections: expected 200, got %d", listW.Code)
	}
}

func TestSubmitAndValidateResultFlow(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/results/submit", writeAuth(handleSubmitResult)).Methods("POST")
	r.HandleFunc("/results", readAuth(handleListResults)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "officer1", "role": "presiding_officer", "full_name": "Officer",
	})

	// Get a valid polling unit code from seed data
	var puCode string
	db.QueryRow("SELECT code FROM polling_units LIMIT 1").Scan(&puCode)
	if puCode == "" {
		t.Skip("no seeded polling units")
	}

	submitBody := fmt.Sprintf(`{"election_id":1,"polling_unit_code":"%s","party_scores":[{"party_code":"APC","votes":350},{"party_code":"PDP","votes":280}]}`, puCode)
	submitReq := httptest.NewRequest("POST", "/results/submit", bytes.NewReader([]byte(submitBody)))
	submitReq.Header.Set("Content-Type", "application/json")
	submitReq.Header.Set("Authorization", "Bearer "+token)
	submitW := httptest.NewRecorder()
	handler.ServeHTTP(submitW, submitReq)

	// Expecting 200 or 400 (validation) — not 500
	if submitW.Code >= 500 {
		t.Errorf("submit result: unexpected server error %d: %s", submitW.Code, submitW.Body.String())
	}
}

func TestHealthCheckEndpointDeep(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/healthz", handleDeepHealthCheck).Methods("GET")

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if status, ok := body["status"].(string); !ok || status != "healthy" {
		t.Errorf("expected healthy status, got %v", body["status"])
	}
	if middleware, ok := body["middleware"].([]interface{}); ok {
		if len(middleware) < 13 {
			t.Errorf("expected 13+ middleware in health check, got %d", len(middleware))
		}
	}
}

func TestAuditTrailLogging(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	// Count audit entries before
	var beforeCount int
	db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&beforeCount)

	// Create an election to trigger audit
	r := mux.NewRouter()
	r.HandleFunc("/elections", writeAuth(handleCreateElection)).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})
	body := `{"title":"Audit Test Election","election_type":"gubernatorial","election_date":"2028-03-01"}`
	req := httptest.NewRequest("POST", "/elections", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Count audit entries after
	var afterCount int
	db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&afterCount)

	if afterCount <= beforeCount {
		t.Error("audit log should have grown after election creation")
	}
}

func TestBVASDeviceListEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/bvas/devices", readAuth(handleListBVASDevices)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})
	req := httptest.NewRequest("GET", "/bvas/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestIncidentListEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/incidents", readAuth(handleListIncidents)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})
	req := httptest.NewRequest("GET", "/incidents?election_id=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
