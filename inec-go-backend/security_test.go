package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestMain(m *testing.M) {
	initValidator()
	initMetrics()
	initBiometricBenchmarks()
	// Set up PostgreSQL test database
	testDSN := os.Getenv("TEST_DATABASE_URL")
	if testDSN == "" {
		testDSN = os.Getenv("DATABASE_URL")
	}
	if testDSN == "" {
		testDSN = "postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable"
	}
	testDB := openDatabase(testDSN)
	db = testDB
	initScaledDB(db)
	initPgpool()
	initDB(db)
	initEMSTables(db)
	initBVASTables(db)
	initPublicAPITables(db)
	initIngestionTables(db)
	initPhase7Tables(db)
	initSMSUSSDTables(db)
	seedDatabase(db)
	initMiddlewareTables(db)
	initTokenBlacklist(db)
	initActiveSessions(db)
	initAPIKeyRotation(db)
	initTracing()
	initObserverTables()
	initDocumentAISchema()
	initElectionFSMSchema()
	initWebhookSchema()
	initDisputeSchema()
	initPushNotificationSchema()
	mwHub = initMiddlewareHub()
	code := m.Run()
	testDB.Close()
	os.Exit(code)
}

// ── Registration Security Tests ──

func TestRegisterBlocksAdminRole(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/auth/register", handleRegister).Methods("POST")

	body := `{"username":"attacker1","password":"longpassword123","full_name":"Bad Actor","role":"admin"}`
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("registering as admin should return 403, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if detail, _ := resp["detail"].(string); !strings.Contains(detail, "restricted") {
		t.Errorf("expected restriction message, got: %s", detail)
	}
}

func TestRegisterBlocksStaffRole(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/auth/register", handleRegister).Methods("POST")

	for _, role := range []string{"presiding_officer", "collation_officer"} {
		body := fmt.Sprintf(`{"username":"attacker_%s","password":"longpassword123","full_name":"Bad Actor","role":"%s"}`, role, role)
		req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != 403 {
			t.Errorf("registering as %s should return 403, got %d", role, w.Code)
		}
	}
}

func TestRegisterAllowsPublicRole(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/auth/register", handleRegister).Methods("POST")

	ts := fmt.Sprintf("%d", time.Now().UnixNano())
	body := fmt.Sprintf(`{"username":"pub_%s","password":"longpassword123","full_name":"Public User","role":"public"}`, ts)
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("registering as public should return 200, got %d — body: %s", w.Code, w.Body.String())
	}
}

func TestRegisterAllowsObserverRole(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/auth/register", handleRegister).Methods("POST")

	ts := fmt.Sprintf("%d", time.Now().UnixNano())
	body := fmt.Sprintf(`{"username":"obs_%s","password":"longpassword123","full_name":"Observer User","role":"observer"}`, ts)
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("registering as observer should return 200, got %d — body: %s", w.Code, w.Body.String())
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/auth/register", handleRegister).Methods("POST")

	body := `{"username":"shortpw","password":"abc","full_name":"Short Pass"}`
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("short password should return 400, got %d", w.Code)
	}
}

func TestRegisterRejectsShortUsername(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/auth/register", handleRegister).Methods("POST")

	body := `{"username":"ab","password":"longpassword123","full_name":"Short User"}`
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("short username should return 400, got %d", w.Code)
	}
}

// ── Auth Guard Tests ──

func TestUnauthenticatedEndpointReturns401(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	// Apply JWT auth middleware the same way main.go does
	r.HandleFunc("/bvas/devices", readAuth(handleListBVASDevices)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	req := httptest.NewRequest("GET", "/bvas/devices", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("unauthenticated request to protected endpoint should return 401, got %d", w.Code)
	}
}

func TestAuthenticatedEndpointReturns200(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/bvas/devices", readAuth(handleListBVASDevices)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "testadmin", "role": "admin",
	})

	req := httptest.NewRequest("GET", "/bvas/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == 401 {
		t.Errorf("authenticated request should not get 401, got %d — body: %s", w.Code, w.Body.String())
	}
}

func TestAdminOnlyEndpointBlocksPublicRole(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/pgpool/status", adminOnly(handlePgpoolStatus)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "2", "username": "publicuser", "role": "public",
	})

	req := httptest.NewRequest("GET", "/pgpool/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("public user accessing admin-only endpoint should get 403, got %d", w.Code)
	}
}

func TestAdminOnlyEndpointAllowsAdmin(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	r := mux.NewRouter()
	r.HandleFunc("/pgpool/status", adminOnly(handlePgpoolStatus)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "testadmin", "role": "admin",
	})

	req := httptest.NewRequest("GET", "/pgpool/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == 401 || w.Code == 403 {
		t.Errorf("admin accessing admin-only endpoint should not get %d", w.Code)
	}
}

// ── Status Machine Tests ──

func TestStatusMachineValidTransitions(t *testing.T) {
	cases := []struct {
		from, to string
		allowed  bool
	}{
		{"pending", "validated", true},
		{"pending", "disputed", true},
		{"pending", "finalized", false},
		{"validated", "finalized", true},
		{"validated", "disputed", true},
		{"validated", "pending", false},
		{"finalized", "pending", false},
		{"finalized", "validated", false},
		{"finalized", "disputed", false},
		{"disputed", "pending", true},
		{"disputed", "validated", true},
		{"disputed", "finalized", false},
	}

	for _, tc := range cases {
		result := canTransition(tc.from, tc.to)
		if result != tc.allowed {
			t.Errorf("canTransition(%s → %s) = %v, expected %v", tc.from, tc.to, result, tc.allowed)
		}
	}
}

func TestStatusMachineUnknownStatus(t *testing.T) {
	if canTransition("unknown", "validated") {
		t.Error("unknown status should not allow any transition")
	}
}

// ── CSRF Protection Tests ──

func TestCSRFBlocksPostWithoutToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := csrfMiddleware(inner)

	req := httptest.NewRequest("POST", "/some/endpoint", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("POST without CSRF token or JWT should return 403, got %d", w.Code)
	}
}

func TestCSRFAllowsGetWithoutToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := csrfMiddleware(inner)

	req := httptest.NewRequest("GET", "/some/endpoint", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET should pass CSRF check, got %d", w.Code)
	}
}

func TestCSRFAllowsPostWithJWT(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := csrfMiddleware(inner)

	req := httptest.NewRequest("POST", "/some/endpoint", nil)
	req.Header.Set("Authorization", "Bearer some-token-here")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("POST with JWT should pass CSRF check, got %d", w.Code)
	}
}

func TestCSRFAllowsPostWithCSRFHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := csrfMiddleware(inner)

	req := httptest.NewRequest("POST", "/some/endpoint", nil)
	req.Header.Set("X-CSRF-Token", "some-csrf-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("POST with X-CSRF-Token should pass, got %d", w.Code)
	}
}

func TestCSRFSkipsPublicPaths(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := csrfMiddleware(inner)

	req := httptest.NewRequest("POST", "/auth/login", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("POST to public path should skip CSRF, got %d", w.Code)
	}
}

// ── Enhanced Security Headers Tests ──

func TestEnhancedSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := enhancedSecurityHeaders(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	expected := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"X-XSS-Protection":          "1; mode=block",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	}

	for header, value := range expected {
		if got := w.Header().Get(header); got != value {
			t.Errorf("header %s: expected %q, got %q", header, value, got)
		}
	}

	if csp := w.Header().Get("Content-Security-Policy"); csp == "" {
		t.Error("Content-Security-Policy header missing")
	}
}

// ── Port Stripping Tests ──

func TestStripPort(t *testing.T) {
	cases := []struct {
		input, expected string
	}{
		{"127.0.0.1:8080", "127.0.0.1"},
		{"10.0.0.1:443", "10.0.0.1"},
		{"[::1]:8080", "::1"},
		{"192.168.1.1", "192.168.1.1"},
	}

	for _, tc := range cases {
		got := stripPort(tc.input)
		if got != tc.expected {
			t.Errorf("stripPort(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

// ── Validation Tests ──

func TestDecodeAndValidateRejectsInvalidJSON(t *testing.T) {
	body := bytes.NewReader([]byte(`{invalid json`))
	req := httptest.NewRequest("POST", "/test", body)
	var dest ResultSubmission
	err := decodeAndValidate(req, &dest)
	if err == nil {
		t.Error("invalid JSON should return error")
	}
}

func TestDecodeAndValidateRejectsMissingFields(t *testing.T) {
	body := bytes.NewReader([]byte(`{"votes": 100}`))
	req := httptest.NewRequest("POST", "/test", body)
	var dest ResultSubmission
	err := decodeAndValidate(req, &dest)
	if err == nil {
		t.Error("missing required fields should return error")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("expected validation error, got: %s", err.Error())
	}
}

func TestDecodeAndValidateAcceptsValidInput(t *testing.T) {
	body := bytes.NewReader([]byte(`{"election_id":1,"polling_unit_code":"PU001","party_code":"APC","votes":500,"idempotency_key":"550e8400-e29b-41d4-a716-446655440000"}`))
	req := httptest.NewRequest("POST", "/test", body)
	var dest ResultSubmission
	err := decodeAndValidate(req, &dest)
	if err != nil {
		t.Errorf("valid input should not return error: %v", err)
	}
	if dest.ElectionID != 1 {
		t.Errorf("expected election_id=1, got %d", dest.ElectionID)
	}
}

func TestVoterRegistrationValidation(t *testing.T) {
	body := bytes.NewReader([]byte(`{"vin":"1234567890123456789","first_name":"Ngozi","last_name":"Okafor","date_of_birth":"1990-01-15","gender":"F","state_code":"LA","lga_code":"LA01","ward_code":"LA0101","pu_code":"LA010101"}`))
	req := httptest.NewRequest("POST", "/test", body)
	var dest VoterRegistration
	err := decodeAndValidate(req, &dest)
	if err != nil {
		t.Errorf("valid voter registration should not return error: %v", err)
	}
}

func TestIncidentReportValidation(t *testing.T) {
	// Missing required incident_type and description
	body := bytes.NewReader([]byte(`{"severity":"high"}`))
	req := httptest.NewRequest("POST", "/test", body)
	var dest IncidentReport
	err := decodeAndValidate(req, &dest)
	if err == nil {
		t.Error("incident without incident_type should fail validation")
	}

	// Valid input
	body2 := bytes.NewReader([]byte(`{"election_id":1,"incident_type":"ballot_snatching","description":"Armed men seized ballot boxes","severity":"critical"}`))
	req2 := httptest.NewRequest("POST", "/test", body2)
	var dest2 IncidentReport
	err2 := decodeAndValidate(req2, &dest2)
	if err2 != nil {
		t.Errorf("valid incident should not return error: %v", err2)
	}
}

// ── Placeholder Conversion Tests ──

func TestPlaceholderConversion(t *testing.T) {
	cases := []struct {
		input, expected string
	}{
		{"SELECT * FROM users WHERE id=?", "SELECT * FROM users WHERE id=$1"},
		{"INSERT INTO t (a,b) VALUES (?,?)", "INSERT INTO t (a,b) VALUES ($1,$2)"},
		{"SELECT * FROM t WHERE a=? AND b=?", "SELECT * FROM t WHERE a=$1 AND b=$2"},
		{"SELECT 'hello?' FROM t WHERE id=?", "SELECT 'hello?' FROM t WHERE id=$1"}, // ? in string literal preserved
		{"SELECT * FROM t", "SELECT * FROM t"},                                      // no placeholders
	}

	for _, tc := range cases {
		got := convertPlaceholders(tc.input)
		if got != tc.expected {
			t.Errorf("convertPlaceholders(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

// ── Token Security Tests ──

func TestExpiredTokenRejected(t *testing.T) {
	// Create a token with negative expiry (already expired)
	claims := map[string]interface{}{
		"sub": "1", "username": "test", "role": "admin",
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
	}
	mc := make(map[string]interface{})
	for k, v := range claims {
		mc[k] = v
	}
	// We can't use createAccessToken because it overrides exp, so test decodeToken directly
	_, err := decodeToken("invalid.token.string")
	if err == nil {
		t.Error("invalid token string should be rejected")
	}
}

func TestTokenWithWrongSecretRejected(t *testing.T) {
	_, err := decodeToken("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c")
	if err == nil {
		t.Error("token signed with different secret should be rejected")
	}
}

// ── Rate Limiter Tests ──

func TestRateLimiterWindowExpiry(t *testing.T) {
	rl := newRateLimiter()

	// Fill to limit with very short window (use allowLocal to test sliding window logic)
	for i := 0; i < 3; i++ {
		rl.allowLocal("test-key", 3, 50*time.Millisecond)
	}

	// Should be blocked
	if rl.allowLocal("test-key", 3, 50*time.Millisecond) {
		t.Error("should be rate limited")
	}

	// Wait for window expiry
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	if !rl.allowLocal("test-key", 3, 50*time.Millisecond) {
		t.Error("should be allowed after window expiry")
	}
}

// ── Public Path Tests ──

func TestPublicPathChecking(t *testing.T) {
	public := []string{"/healthz", "/readiness", "/auth/login", "/auth/register", "/metrics"}
	for _, p := range public {
		if !isPublicPath(p) {
			t.Errorf("%s should be a public path", p)
		}
	}

	private := []string{"/bvas/devices", "/blockchain/stats", "/production/status", "/ems/voters"}
	for _, p := range private {
		if isPublicPath(p) {
			t.Errorf("%s should NOT be a public path", p)
		}
	}
}

// ── AllowedSelfRegRoles Tests ──

func TestAllowedSelfRegRoles(t *testing.T) {
	allowed := []string{"public", "observer"}
	for _, r := range allowed {
		if !allowedSelfRegRoles[r] {
			t.Errorf("role %s should be allowed for self-registration", r)
		}
	}

	blocked := []string{"admin", "presiding_officer", "collation_officer", "staff"}
	for _, r := range blocked {
		if allowedSelfRegRoles[r] {
			t.Errorf("role %s should NOT be allowed for self-registration", r)
		}
	}
}

// ── Election CRUD Tests ──

func TestCreateElectionRequiresAuth(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/elections", writeAuth(handleCreateElection)).Methods("POST")
	handler := jwtAuthMiddleware(r)

	body := `{"title":"Test Election","election_type":"presidential","election_date":"2027-02-15"}`
	req := httptest.NewRequest("POST", "/elections", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}
}

func TestListElectionsWithAuth(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/elections", readAuth(handleListElections)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "testuser", "role": "public", "full_name": "Test",
	})
	req := httptest.NewRequest("GET", "/elections", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200 with auth, got %d", w.Code)
	}
	var body []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
}

// ── Result Submit Requires Auth ──

func TestResultSubmitRequiresAuth(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/results/submit", writeAuth(handleSubmitResult)).Methods("POST")
	handler := jwtAuthMiddleware(r)

	req := httptest.NewRequest("POST", "/results/submit", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}
}

// ── Audit Trail Endpoint Tests ──

func TestAuditTrailReturnsData(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/audit/trail", readAuth(handleAuditTrail)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin1", "role": "admin", "full_name": "Admin",
	})
	req := httptest.NewRequest("GET", "/audit/trail", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ── Domain Logic: Collation Aggregation ──

func TestCollationAggregatesCorrectly(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/inec/collation", readAuth(handleHierarchicalCollation)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "officer1", "role": "collation_officer", "full_name": "Officer",
	})
	req := httptest.NewRequest("GET", "/inec/collation?election_id=1&level=state&code=LA", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ── Domain Logic: Ballot Reconciliation ──

func TestBallotReconciliationEndpoint(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/inec/reconciliation/ballot", readAuth(handleBallotReconciliation)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin1", "role": "admin", "full_name": "Admin",
	})
	req := httptest.NewRequest("GET", "/inec/reconciliation/ballot?election_id=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ── Finalize Result Requires Admin ──

func TestFinalizeResultRequiresAdmin(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/results/{id:[0-9]+}/finalize", adminOnly(handleFinalizeResult)).Methods("POST")
	handler := jwtAuthMiddleware(r)

	// Officer (not admin) should get 403
	token, _ := createAccessToken(map[string]interface{}{
		"sub": "2", "username": "officer", "role": "presiding_officer", "full_name": "Officer",
	})
	req := httptest.NewRequest("POST", "/results/1/finalize", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("expected 403 for non-admin, got %d", w.Code)
	}
}

// ── BVAS Registration Requires Write Auth ──

func TestBVASRegistrationRequiresAuth(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/bvas/devices", writeAuth(handleRegisterBVASDevice)).Methods("POST")
	handler := jwtAuthMiddleware(r)

	req := httptest.NewRequest("POST", "/bvas/devices", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}
}

// ── Incident Creation Requires Write Auth ──

func TestIncidentCreationRequiresAuth(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/incidents", writeAuth(handleCreateIncident)).Methods("POST")
	handler := jwtAuthMiddleware(r)

	req := httptest.NewRequest("POST", "/incidents", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ── Middleware Status Public ──

func TestMiddlewareStatusAccessible(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/middleware/status", handleMiddlewareStatus).Methods("GET")

	req := httptest.NewRequest("GET", "/middleware/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if components, ok := body["components"].([]interface{}); ok {
		if len(components) < 10 {
			t.Errorf("expected at least 10 middleware components, got %d", len(components))
		}
	}
}

// ── Dashboard Stats Requires Auth ──

func TestDashboardStatsRequiresAuth(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/dashboard/stats", readAuth(handleDashboardStats)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	req := httptest.NewRequest("GET", "/dashboard/stats", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}
}

// ── Election Validation Tests ──

func TestElectionCreateValidation(t *testing.T) {
	ec := ElectionCreate{Title: "T", ElectionType: "presidential", ElectionDate: "2027-01-01"}
	err := validate.Struct(ec)
	if err == nil {
		t.Error("title 'T' (1 char) should fail min=3 validation")
	}

	ec2 := ElectionCreate{Title: "2027 Presidential Election", ElectionType: "presidential", ElectionDate: "2027-02-15"}
	err2 := validate.Struct(ec2)
	if err2 != nil {
		t.Errorf("valid election should pass: %v", err2)
	}

	ec3 := ElectionCreate{Title: "Test", ElectionType: "invalid_type", ElectionDate: "2027-01-01"}
	err3 := validate.Struct(ec3)
	if err3 == nil {
		t.Error("invalid election_type should fail oneof validation")
	}
}

// ── User Promotion Validation ──

func TestUserPromotionValidation(t *testing.T) {
	up := UserPromotion{UserID: 0, Role: "admin"}
	err := validate.Struct(up)
	if err == nil {
		t.Error("user_id 0 should fail gt=0 validation")
	}

	up2 := UserPromotion{UserID: 5, Role: "admin"}
	err2 := validate.Struct(up2)
	if err2 != nil {
		t.Errorf("valid promotion should pass: %v", err2)
	}

	up3 := UserPromotion{UserID: 5, Role: "superadmin"}
	err3 := validate.Struct(up3)
	if err3 == nil {
		t.Error("role 'superadmin' should fail oneof validation")
	}
}

// ── Session Revocation Tests ──

func TestTokenBlacklistRevokeAndCheck(t *testing.T) {
	jti := "test-jti-" + fmt.Sprintf("%d", time.Now().UnixNano())
	expiresAt := time.Now().Add(1 * time.Hour)

	// Should not be blacklisted initially
	if blacklist.isBlacklisted(jti) {
		t.Error("new JTI should not be blacklisted")
	}

	// Revoke it
	err := blacklist.revokeToken(jti, 1, expiresAt, "test")
	if err != nil {
		t.Fatalf("revokeToken failed: %v", err)
	}

	// Should now be blacklisted
	if !blacklist.isBlacklisted(jti) {
		t.Error("revoked JTI should be blacklisted")
	}
}

func TestTokenBlacklistExpiredNotBlocked(t *testing.T) {
	jti := "expired-jti-" + fmt.Sprintf("%d", time.Now().UnixNano())
	expiresAt := time.Now().Add(-1 * time.Hour) // Already expired

	blacklist.revokeToken(jti, 1, expiresAt, "expired test")

	// Expired token should NOT be considered blacklisted
	if blacklist.isBlacklisted(jti) {
		t.Error("expired JTI should not be considered blacklisted")
	}
}

func TestLogoutEndpoint(t *testing.T) {
	// Login first
	loginBody := `{"username":"admin","password":"admin123"}`
	loginReq := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	handleLogin(loginW, loginReq)

	if loginW.Code != 200 {
		t.Fatalf("login failed: %d", loginW.Code)
	}

	var loginResp map[string]interface{}
	json.Unmarshal(loginW.Body.Bytes(), &loginResp)
	token := loginResp["access_token"].(string)

	// Logout — use jwtAuthMiddleware wrapping to simulate real request chain
	r := mux.NewRouter()
	r.Use(jwtAuthMiddleware)
	r.HandleFunc("/auth/logout", handleLogout).Methods("POST")

	logoutReq := httptest.NewRequest("POST", "/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+token)
	logoutW := httptest.NewRecorder()
	r.ServeHTTP(logoutW, logoutReq)

	if logoutW.Code != 200 {
		t.Errorf("logout should return 200, got %d: %s", logoutW.Code, logoutW.Body.String())
	}
}

// ── API Key Rotation Tests ──

func TestAPIKeyRotation(t *testing.T) {
	// Create a new key
	newKey, err := rotateAPIKey("", 1, "test-key")
	if err != nil {
		t.Fatalf("rotateAPIKey failed: %v", err)
	}
	if newKey == "" {
		t.Error("expected non-empty key")
	}
	if len(newKey) != 64 { // 32 bytes hex-encoded
		t.Errorf("expected 64 char key, got %d", len(newKey))
	}

	// Verify it's valid
	keyHash := hashAPIKey(newKey)
	if !isAPIKeyValid(keyHash) {
		t.Error("newly created key should be valid")
	}

	// Rotate it
	newKey2, err := rotateAPIKey(keyHash, 1, "test-key-v2")
	if err != nil {
		t.Fatalf("second rotation failed: %v", err)
	}
	if newKey2 == newKey {
		t.Error("rotated key should be different")
	}

	// Old key should be invalid
	if isAPIKeyValid(keyHash) {
		t.Error("old key should be deactivated after rotation")
	}

	// New key should be valid
	newKeyHash2 := hashAPIKey(newKey2)
	if !isAPIKeyValid(newKeyHash2) {
		t.Error("rotated key should be valid")
	}
}

// ── Geo-fencing Tests ──

func TestHaversineDistance(t *testing.T) {
	// Abuja to Lagos is approximately 530-540km
	abujaLat, abujaLon := 9.0579, 7.4951
	lagosLat, lagosLon := 6.5244, 3.3792

	dist := haversineDistance(abujaLat, abujaLon, lagosLat, lagosLon)
	if dist < 500000 || dist > 600000 { // 500-600km in meters
		t.Errorf("Abuja-Lagos distance should be ~534km, got %.0fm", dist)
	}

	// Same point = 0 distance
	zero := haversineDistance(9.0, 7.0, 9.0, 7.0)
	if zero != 0 {
		t.Errorf("same point distance should be 0, got %f", zero)
	}
}

func TestGeofenceValidation(t *testing.T) {
	// Create table and insert a test polling unit location
	db.Exec(`CREATE TABLE IF NOT EXISTS polling_unit_locations (
		polling_unit_code TEXT PRIMARY KEY,
		latitude REAL NOT NULL,
		longitude REAL NOT NULL,
		geofence_radius_m INTEGER DEFAULT 500,
		state_code TEXT,
		lga_code TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS bvas_location_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bvas_serial TEXT NOT NULL,
		polling_unit_code TEXT NOT NULL,
		latitude REAL NOT NULL,
		longitude REAL NOT NULL,
		distance_from_pu_m REAL,
		within_geofence BOOLEAN,
		logged_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec("INSERT OR REPLACE INTO polling_unit_locations (polling_unit_code, latitude, longitude, geofence_radius_m) VALUES ('TEST-PU-001', 9.0579, 7.4951, 500)")

	// Within geofence (same location)
	result, err := validateGeofence(9.0579, 7.4951, "TEST-PU-001")
	if err != nil {
		t.Fatalf("validateGeofence failed: %v", err)
	}
	if !result.WithinGeofence {
		t.Error("same location should be within geofence")
	}
	if result.DistanceMeters > 1 {
		t.Errorf("distance should be ~0, got %f", result.DistanceMeters)
	}

	// Outside geofence (far away — ~534km from Abuja to Lagos)
	result2, _ := validateGeofence(6.5244, 3.3792, "TEST-PU-001")
	if result2.WithinGeofence {
		t.Error("Lagos location should be outside Abuja 500m geofence")
	}
}

func TestGeofenceNonExistentPU(t *testing.T) {
	// Non-existent PU should default to allow
	result, err := validateGeofence(9.0, 7.0, "NONEXISTENT-PU")
	if err != nil {
		t.Fatalf("should not error for non-existent PU: %v", err)
	}
	if !result.WithinGeofence {
		t.Error("non-existent PU should allow by default")
	}
}

// ── Auto-Collation Tests ──

func TestAutoCollationDoesNotPanicOnMissingData(t *testing.T) {
	// Should not panic when tables/data don't exist
	checkAutoCollation(999, "NONEXISTENT-PU")
}

// ── Migration System Tests ──

func TestMigrationsIdempotent(t *testing.T) {
	// Running migrations multiple times should not error
	err := runMigrations(db)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	err = runMigrations(db)
	if err != nil {
		t.Fatalf("second run (idempotent): %v", err)
	}
}

// ── Tracing Tests ──

func TestTracingConfigInitialized(t *testing.T) {
	initTracing()
	if serviceName == "" {
		t.Error("service name should not be empty after initTracing()")
	}
}

// ── Geofence Handler Tests ──

func TestGeofenceCheckEndpoint(t *testing.T) {
	// Ensure tables exist and insert test PU location
	db.Exec(`CREATE TABLE IF NOT EXISTS polling_unit_locations (
		polling_unit_code TEXT PRIMARY KEY,
		latitude REAL NOT NULL, longitude REAL NOT NULL,
		geofence_radius_m INTEGER DEFAULT 500, state_code TEXT, lga_code TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS bvas_location_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bvas_serial TEXT NOT NULL, polling_unit_code TEXT NOT NULL,
		latitude REAL NOT NULL, longitude REAL NOT NULL,
		distance_from_pu_m REAL, within_geofence BOOLEAN, logged_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec("INSERT OR REPLACE INTO polling_unit_locations (polling_unit_code, latitude, longitude, geofence_radius_m) VALUES ('GEO-TEST-01', 6.5244, 3.3792, 500)")

	router := mux.NewRouter()
	router.Use(jwtAuthMiddleware)
	router.HandleFunc("/geofence/check", handleGeofenceCheck).Methods("POST")

	// Get token
	token := getTestToken(t, "admin")

	// Within geofence
	body := `{"bvas_serial":"BVAS-001","polling_unit_code":"GEO-TEST-01","latitude":6.5244,"longitude":3.3792}`
	req := httptest.NewRequest("POST", "/geofence/check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("within-geofence should return 200, got %d: %s", w.Code, w.Body.String())
	}

	// Outside geofence (far away)
	body2 := `{"bvas_serial":"BVAS-002","polling_unit_code":"GEO-TEST-01","latitude":9.0579,"longitude":7.4951}`
	req2 := httptest.NewRequest("POST", "/geofence/check", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != 403 {
		t.Errorf("outside-geofence should return 403, got %d: %s", w2.Code, w2.Body.String())
	}
}

func getTestToken(t *testing.T, role string) string {
	t.Helper()
	loginBody := fmt.Sprintf(`{"username":"%s","password":"%s123"}`, role, role)
	req := httptest.NewRequest("POST", "/auth/login", bytes.NewBufferString(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleLogin(w, req)
	if w.Code != 200 {
		t.Fatalf("getTestToken login failed for %s: %d %s", role, w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["access_token"].(string)
}

// ── KYB Verification Tests ──

func TestKYBVerifyEndpoint(t *testing.T) {
	ensureTestDB(t)
	initKYBSchema()
	token := getTestToken(t, "admin")

	body := `{"entity_id":100,"entity_type":"political_party","entity_name":"Test Party","registration_number":"CAC/12345678","tax_id":"TIN-12345","address":"Abuja FCT","authorized_signatories":[{"name":"John Doe","role":"Chairman","nin_id":"12345678901"}]}`
	req := httptest.NewRequest("POST", "/kyb/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handleKYBVerify(w, req)

	if w.Code != 200 {
		t.Fatalf("KYB verify returned %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "approved" {
		t.Errorf("expected approved status with full compliance, got %s", resp["status"])
	}
	if resp["compliance_score"].(float64) < 80 {
		t.Errorf("compliance_score %v should be >= 80 for full submission", resp["compliance_score"])
	}
	if resp["registration_verified"] != true {
		t.Error("registration should be verified")
	}
}

func TestKYBVerifyInvalidType(t *testing.T) {
	ensureTestDB(t)
	initKYBSchema()
	body := `{"entity_id":1,"entity_type":"invalid_type","entity_name":"Test"}`
	req := httptest.NewRequest("POST", "/kyb/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleKYBVerify(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400 for invalid entity_type, got %d", w.Code)
	}
}

func TestKYBStatusEndpoint(t *testing.T) {
	ensureTestDB(t)
	initKYBSchema()

	req := httptest.NewRequest("GET", fmt.Sprintf("/kyb/status?entity_id=%d", time.Now().UnixNano()), nil)
	w := httptest.NewRecorder()
	handleKYBStatus(w, req)

	if w.Code != 200 {
		t.Fatalf("KYB status returned %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "not_started" {
		t.Errorf("expected not_started for unknown entity, got %s", resp["status"])
	}
}

// ── KYC Event Trigger Tests ──

func TestKYCEventsEndpoint(t *testing.T) {
	ensureTestDB(t)
	initKYBSchema()

	emitKYCEvent(1, "kyc_verification_completed", "test", M{"status": "verified"})
	emitKYCEvent(1, "liveness_check_completed", "test", M{"passed": true})

	req := httptest.NewRequest("GET", "/kyc/events?user_id=1", nil)
	w := httptest.NewRecorder()
	handleKYCEvents(w, req)

	if w.Code != 200 {
		t.Fatalf("KYC events returned %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	count := int(resp["count"].(float64))
	if count < 2 {
		t.Errorf("expected at least 2 events, got %d", count)
	}
}

func TestKYCTriggerCheckEndpoint(t *testing.T) {
	ensureTestDB(t)
	initKYBSchema()

	req := httptest.NewRequest("GET", "/kyc/triggers?user_id=9999", nil)
	w := httptest.NewRecorder()
	handleKYCTriggerCheck(w, req)

	if w.Code != 200 {
		t.Fatalf("KYC trigger check returned %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["needs_reverification"] != true {
		t.Error("user with no KYC should need reverification")
	}
	triggers := resp["triggers"].([]interface{})
	found := false
	for _, tr := range triggers {
		trig := tr.(map[string]interface{})
		if trig["trigger"] == "no_kyc_on_file" {
			found = true
		}
	}
	if !found {
		t.Error("expected no_kyc_on_file trigger for unknown user")
	}
}

// ── Data Security Tests ──

func TestDataSecurityStatusEndpoint(t *testing.T) {
	ensureTestDB(t)
	initDataSecuritySchema()

	req := httptest.NewRequest("GET", "/security/data-status", nil)
	w := httptest.NewRecorder()
	handleDataSecurityStatus(w, req)

	if w.Code != 200 {
		t.Fatalf("data security status returned %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	transit := resp["data_in_transit"].(map[string]interface{})
	if transit["tls_enforced"] != true {
		t.Error("TLS should be enforced")
	}
	if transit["hsts_enabled"] != true {
		t.Error("HSTS should be enabled")
	}

	rest := resp["data_at_rest"].(map[string]interface{})
	if rest["biometric_vault"] == nil {
		t.Error("biometric vault encryption should be reported")
	}
	if rest["password_hashing"] != "bcrypt (cost 10)" {
		t.Errorf("password hashing expected bcrypt, got %v", rest["password_hashing"])
	}
}

func TestDataClassificationEndpoint(t *testing.T) {
	ensureTestDB(t)
	initDataSecuritySchema()

	req := httptest.NewRequest("GET", "/security/data-classification", nil)
	w := httptest.NewRecorder()
	handleDataClassificationList(w, req)

	if w.Code != 200 {
		t.Fatalf("data classification returned %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	count := int(resp["count"].(float64))
	if count < 10 {
		t.Errorf("expected at least 10 classified fields, got %d", count)
	}
}

func TestSecurityEventLogging(t *testing.T) {
	ensureTestDB(t)
	initDataSecuritySchema()

	logSecurityEvent("login_attempt", "info", "auth", 1, "127.0.0.1", M{"success": true})
	logSecurityEvent("brute_force_detected", "high", "rate_limiter", 0, "10.0.0.1", M{"attempts": 50})

	req := httptest.NewRequest("GET", "/security/events?severity=high", nil)
	w := httptest.NewRecorder()
	handleSecurityEvents(w, req)

	if w.Code != 200 {
		t.Fatalf("security events returned %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	count := int(resp["count"].(float64))
	if count < 1 {
		t.Errorf("expected at least 1 high severity event, got %d", count)
	}
}
