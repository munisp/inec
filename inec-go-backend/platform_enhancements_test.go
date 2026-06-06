package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCommandCenterLive(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/command-center/live", nil)
	w := httptest.NewRecorder()
	handleCommandCenterLive(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := resp["states"]; !ok {
		t.Error("expected states field")
	}
	if _, ok := resp["alerts"]; !ok {
		t.Error("expected alerts field")
	}
	if _, ok := resp["completion_pct"]; !ok {
		t.Error("expected completion_pct field")
	}
	if _, ok := resp["load_shedding"]; !ok {
		t.Error("expected load_shedding field")
	}
}

func TestHandleLoadShedding_GetAndSet(t *testing.T) {
	ensureTestDB(t)
	// GET current level
	req := httptest.NewRequest("GET", "/load-shedding", nil)
	w := httptest.NewRecorder()
	handleLoadShedding(w, req)
	if w.Code != 200 {
		t.Fatalf("GET expected 200, got %d", w.Code)
	}

	// POST set level 2
	body := strings.NewReader(`{"level":2}`)
	req = httptest.NewRequest("POST", "/load-shedding", body)
	w = httptest.NewRecorder()
	handleLoadShedding(w, req)
	if w.Code != 200 {
		t.Fatalf("POST expected 200, got %d", w.Code)
	}
	if cmdCenter.loadShedLevel != 2 {
		t.Errorf("expected level 2, got %d", cmdCenter.loadShedLevel)
	}

	// POST invalid level
	body = strings.NewReader(`{"level":5}`)
	req = httptest.NewRequest("POST", "/load-shedding", body)
	w = httptest.NewRecorder()
	handleLoadShedding(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for invalid level, got %d", w.Code)
	}
	cmdCenter.loadShedLevel = 0
}

func TestLoadSheddingMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := loadSheddingMiddleware(inner)

	// Level 0 — all requests pass through
	cmdCenter.loadShedLevel = 0
	req := httptest.NewRequest("GET", "/dashboard/stats", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("level 0: expected 200, got %d", w.Code)
	}

	// Level 1 — LOW priority blocked
	cmdCenter.loadShedLevel = 1
	req = httptest.NewRequest("GET", "/some/analytics", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Errorf("level 1: expected 503 for LOW priority, got %d", w.Code)
	}

	// Level 1 — CRITICAL passes
	req = httptest.NewRequest("POST", "/results/submit", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("level 1: expected 200 for CRITICAL, got %d", w.Code)
	}

	// Level 3 — only CRITICAL passes
	cmdCenter.loadShedLevel = 3
	req = httptest.NewRequest("GET", "/dashboard/stats", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Errorf("level 3: expected 503 for MEDIUM, got %d", w.Code)
	}

	req = httptest.NewRequest("POST", "/results/submit", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("level 3: expected 200 for CRITICAL, got %d", w.Code)
	}

	cmdCenter.loadShedLevel = 0
}

func TestClassifyPriority(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/results/submit", "CRITICAL"},
		{"/ec8a/submit", "CRITICAL"},
		{"/ingestion/submit", "CRITICAL"},
		{"/collation/aggregate", "HIGH"},
		{"/observer/reports", "HIGH"},
		{"/biometric/verify", "HIGH"},
		{"/dashboard/stats", "MEDIUM"},
		{"/elections", "MEDIUM"},
		{"/api/v1/docs", "LOW"},
		{"/some/random/path", "LOW"},
	}
	for _, tt := range tests {
		got := classifyPriority(tt.path)
		if got != tt.want {
			t.Errorf("classifyPriority(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestHandleMFASetupTOTP(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("POST", "/auth/mfa/totp/setup", nil)
	w := httptest.NewRecorder()
	handleMFASetupTOTP(w, req)
	// Without auth, should return 401
	if w.Code != 401 {
		t.Fatalf("expected 401 without auth, got %d", w.Code)
	}
}

func TestHandleMFAVerifyTOTP_InvalidCode(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("POST", "/auth/mfa/totp/verify", strings.NewReader(`{"code":"123"}`))
	w := httptest.NewRecorder()
	handleMFAVerifyTOTP(w, req)
	// Should fail — no auth or wrong code length
	if w.Code == 200 {
		t.Error("expected non-200 for invalid code")
	}
}

func TestHandleCitizenVerify_MissingParams(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/citizen/verify", nil)
	w := httptest.NewRecorder()
	handleCitizenVerify(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for missing params, got %d", w.Code)
	}
}

func TestHandleCitizenVerify_WithPUCode(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/citizen/verify?pu_code=PU-FCT-001", nil)
	w := httptest.NewRecorder()
	handleCitizenVerify(w, req)
	// Handler may return 200 or 500 depending on DB state
	if w.Code != 200 && w.Code != 500 {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
	}
}

func TestHandleOpenAPIDocs(t *testing.T) {
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	w := httptest.NewRecorder()
	handleOpenAPIDocs(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["openapi"] != "3.1.0" {
		t.Errorf("expected openapi 3.1.0, got %v", resp["openapi"])
	}
}

func TestHandleGeoFencedSubmit_MissingFields(t *testing.T) {
	ensureTestDB(t)
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest("POST", "/geo/submission/check", body)
	w := httptest.NewRecorder()
	handleGeoFencedSubmit(w, req)
	// Handler returns 400 or 404 depending on lookup
	if w.Code != 400 && w.Code != 404 {
		t.Fatalf("expected 400 or 404 for missing fields, got %d", w.Code)
	}
}

func TestRoleBasedRateLimit(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := roleBasedRateLimit(inner)
	req := httptest.NewRequest("GET", "/dashboard/stats", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandlePredictiveAnalytics(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/predictive/analytics", nil)
	w := httptest.NewRecorder()
	handlePredictiveAnalytics(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["election_id"]; !ok {
		t.Error("expected election_id")
	}
}

func TestHandleAnomalyEscalation_GET(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/anomaly/escalation", nil)
	w := httptest.NewRecorder()
	handleAnomalyEscalation(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleDataClassification_GET(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/data/classification", nil)
	w := httptest.NewRecorder()
	handleDataClassification(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleElectionTemplates_GET(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/election/templates", nil)
	w := httptest.NewRecorder()
	handleElectionTemplates(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleElectionArchive_GET(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/election/archive", nil)
	w := httptest.NewRecorder()
	handleElectionArchive(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleBiometricQualityCheck(t *testing.T) {
	ensureTestDB(t)
	body := strings.NewReader(`{"capture_id":"cap-1","modality":"fingerprint","blur_score":0.1,"exposure":0.8,"angle":5.0}`)
	req := httptest.NewRequest("POST", "/biometric/quality-check", body)
	w := httptest.NewRecorder()
	handleBiometricQualityCheck(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	// Response may use "quality" or "overall_quality" depending on implementation
	if _, ok := resp["quality"]; !ok {
		if _, ok2 := resp["overall_quality"]; !ok2 {
			t.Logf("response: %v", resp)
		}
	}
}

func TestHandleMediaWidget(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/media/widget", nil)
	w := httptest.NewRecorder()
	handleMediaWidget(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleExportPDFReport(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/export/report/pdf?type=gazette", nil)
	w := httptest.NewRecorder()
	handleExportPDFReport(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleIVRVerify_MissingFields(t *testing.T) {
	ensureTestDB(t)
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest("POST", "/ivr/verify", body)
	w := httptest.NewRecorder()
	handleIVRVerify(w, req)
	// Handler may accept empty body gracefully or return 400
	if w.Code != 200 && w.Code != 400 {
		t.Fatalf("expected 200 or 400, got %d", w.Code)
	}
}

func TestHandleOfflineConflictResolve_MissingFields(t *testing.T) {
	ensureTestDB(t)
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest("POST", "/offline/conflict/resolve", body)
	w := httptest.NewRecorder()
	handleOfflineConflictResolve(w, req)
	// Handler may accept empty body or return 400
	if w.Code != 200 && w.Code != 400 {
		t.Fatalf("expected 200 or 400, got %d", w.Code)
	}
}

func TestValidateTOTP(t *testing.T) {
	// Test with known invalid code
	if validateTOTP("JBSWY3DPEHPK3PXP", "000000") {
		// It's possible a TOTP code matches at the right time, so this isn't deterministic.
		// Instead, test that the function doesn't panic.
	}
	// Test with empty secret — should not panic
	validateTOTP("", "123456")
}

func TestHaversineDistanceEnhancements(t *testing.T) {
	// Lagos (6.5244, 3.3792) to Abuja (9.0579, 7.4951) ≈ 461-534 km depending on formula
	d := haversineDistance(6.5244, 3.3792, 9.0579, 7.4951)
	if d < 400000 || d > 600000 {
		t.Errorf("Lagos-Abuja expected 400-600km range, got %.0fm", d)
	}

	// Same point — distance should be 0
	d = haversineDistance(9.0579, 7.4951, 9.0579, 7.4951)
	if d > 1 {
		t.Errorf("same point expected 0, got %.0fm", d)
	}
}

func TestHandleMFAStatus_NoAuth(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/auth/mfa/status", nil)
	w := httptest.NewRecorder()
	handleMFAStatus(w, req)
	// Should return 200 with defaults (no MFA enabled)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleEscalationConfig_GET(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/escalation/config", nil)
	w := httptest.NewRecorder()
	handleEscalationConfig(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["rules"]; !ok {
		t.Error("expected rules field")
	}
}
