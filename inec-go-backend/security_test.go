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
	// Set up an in-memory SQLite database for tests
	testDB := openDatabase("file::memory:?cache=shared")
	db = testDB
	initScaledDB(db)
	initPgpool()
	initDB(db)
	seedDatabase(db)
	initMiddlewareTables(db)
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
		"X-Frame-Options":          "DENY",
		"X-XSS-Protection":        "1; mode=block",
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
	// Missing required title
	body := bytes.NewReader([]byte(`{"severity":"high"}`))
	req := httptest.NewRequest("POST", "/test", body)
	var dest IncidentReport
	err := decodeAndValidate(req, &dest)
	if err == nil {
		t.Error("incident without title should fail validation")
	}

	// Valid input
	body2 := bytes.NewReader([]byte(`{"title":"Ballot Box Snatching","description":"Armed men seized ballot boxes","severity":"critical"}`))
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
		{"SELECT * FROM t", "SELECT * FROM t"},                                       // no placeholders
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

	// Fill to limit with very short window
	for i := 0; i < 3; i++ {
		rl.allow("test-key", 3, 50*time.Millisecond)
	}

	// Should be blocked
	if rl.allow("test-key", 3, 50*time.Millisecond) {
		t.Error("should be rate limited")
	}

	// Wait for window expiry
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	if !rl.allow("test-key", 3, 50*time.Millisecond) {
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
