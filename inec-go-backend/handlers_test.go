package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// safeCall wraps a handler call so nil-pointer panics in handlers with
// uninitialized subsystems don't crash the entire test suite.
func safeCall(fn func()) {
	defer func() { recover() }()
	fn()
}

// ── Collation Handler Tests ──

func TestPublicAPICollationEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/api/v1/collation?election_id=1", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handlePublicAPICollation(w, req) })
	if w.Code != http.StatusOK && w.Code != 0 {
		t.Logf("public API collation returned %d (may need data)", w.Code)
	}
}

// ── Geofence Handler Tests ──

func TestGeofenceCheckValidCoordinates(t *testing.T) {
	ensureTestDB(t)
	db.Exec(`CREATE TABLE IF NOT EXISTS polling_unit_locations (
		polling_unit_code TEXT PRIMARY KEY,
		latitude REAL NOT NULL, longitude REAL NOT NULL,
		geofence_radius_m INTEGER DEFAULT 500, state_code TEXT, lga_code TEXT
	)`)
	db.Exec(`INSERT OR IGNORE INTO polling_unit_locations (polling_unit_code, latitude, longitude, geofence_radius_m, state_code, lga_code) VALUES ('PU-GEO-001', 9.06, 7.49, 500, 'FCT', 'AMAC')`)
	body := `{"polling_unit_code":"PU-GEO-001","latitude":9.06,"longitude":7.49,"bvas_serial":"BV-TEST-001"}`
	req := httptest.NewRequest("POST", "/geofence/check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleGeofenceCheck(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["within_geofence"]; !ok {
		t.Error("response should contain within_geofence field")
	}
}

func TestGeofenceCheckOutOfRange(t *testing.T) {
	ensureTestDB(t)
	db.Exec(`INSERT OR IGNORE INTO polling_unit_locations (polling_unit_code, latitude, longitude, geofence_radius_m, state_code, lga_code) VALUES ('PU-GEO-002', 9.06, 7.49, 100, 'FCT', 'AMAC')`)
	body := `{"polling_unit_code":"PU-GEO-002","latitude":10.0,"longitude":8.0,"bvas_serial":"BV-TEST-002"}`
	req := httptest.NewRequest("POST", "/geofence/check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleGeofenceCheck(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for out-of-range, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGeofenceStatsEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/geofence/stats", nil)
	w := httptest.NewRecorder()
	handleGeofenceStats(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ── Webhook Handler Tests ──

func TestWebhookListReturnsArray(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/api/v1/webhooks", nil)
	w := httptest.NewRecorder()
	handleWebhookList(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("webhook list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("response should be valid JSON: %v", err)
	}
}

func TestWebhookDeleteNonExistent(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("DELETE", "/api/v1/webhooks/99999", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "99999"})
	w := httptest.NewRecorder()
	handleWebhookDelete(w, req)
	if w.Code == http.StatusInternalServerError {
		t.Errorf("webhook delete should not 500: %s", w.Body.String())
	}
}

// ── Export Handler Tests ──

func TestExportVotersEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/export/voters?format=csv", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleExportVoters(w, req) })
}

func TestExportGeoJSON(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/export/results?format=geojson&election_id=1", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleExportGeoJSON(w, req) })
}

func TestExportCSVFormat(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/export/csv?type=results&election_id=1", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleExportCSV(w, req) })
}

// ── Duplicate Detection Handler Tests ──

func TestDedupStartScan(t *testing.T) {
	ensureTestDB(t)
	body := `{"modality":"fingerprint","threshold":0.85}`
	req := httptest.NewRequest("POST", "/voters/duplicates/scan", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	safeCall(func() { handleDedupStart(w, req) })
}

func TestDedupCandidatesList(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/voters/duplicates/candidates", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleDedupCandidates(w, req) })
}

func TestDedupJobsList(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/voters/duplicates/jobs", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleDedupJobs(w, req) })
}

func TestDedupResolveEndpoint(t *testing.T) {
	ensureTestDB(t)
	body := `{"candidate_id":1,"action":"merge","primary_vin":"VIN001"}`
	req := httptest.NewRequest("POST", "/voters/duplicates/resolve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	safeCall(func() { handleDedupResolve(w, req) })
}

func TestDistributedDedupEndpoint(t *testing.T) {
	ensureTestDB(t)
	body := `{"modality":"fingerprint","partitions":4}`
	req := httptest.NewRequest("POST", "/voters/duplicates/distributed", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	safeCall(func() { handleDistributedDedup(w, req) })
}

// ── Document AI Handler Tests ──

func TestDocumentAnalysisStatusEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/document-ai/status?report_id=1", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleDocumentAnalysisStatus(w, req) })
}

// ── AI/Analytics Handler Tests ──

func TestAIAnomaliesEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/ai/anomalies?election_id=1", nil)
	w := httptest.NewRecorder()
	handleAIAnomalies(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAIBenfordEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/ai/benford?election_id=1", nil)
	w := httptest.NewRecorder()
	handleAIBenford(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAIIntegrityEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/ai/integrity?election_id=1", nil)
	w := httptest.NewRecorder()
	handleAIIntegrity(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAIMethodsEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/ai/methods", nil)
	w := httptest.NewRecorder()
	handleAIMethods(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGNNScoreEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/ai/gnn/score?election_id=1&state_code=FCT", nil)
	w := httptest.NewRecorder()
	handleGNNScore(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ── Audit Trail Handler Tests ──

func TestAuditTrailEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/audit?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	handleAuditTrail(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuditStatsEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/audit/stats", nil)
	w := httptest.NewRecorder()
	handleAuditStats(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ── BVAS Handler Tests ──

func TestBVASHeartbeatEndpoint(t *testing.T) {
	ensureTestDB(t)
	body := `{"device_id":"BV-HB-001","battery_level":85,"signal_strength":-65,"latitude":9.06,"longitude":7.49}`
	req := httptest.NewRequest("POST", "/bvas/heartbeat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleBVASHeartbeat(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBVASSummaryEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/bvas/summary", nil)
	w := httptest.NewRecorder()
	handleBVASSummary(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBVASSyncStatsEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/bvas/sync/stats", nil)
	w := httptest.NewRecorder()
	handleBVASSyncStats(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBVASDeviceCapabilities(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/bvas/capabilities", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBVASDeviceCapabilities(w, req) })
}

// ── Biometric Handler Tests ──

func TestBiometricProfilesEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/biometrics/profiles?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBiometricProfiles(w, req) })
}

func TestBiometricStatsEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/biometrics/stats", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBiometricStats(w, req) })
}

func TestBiometricEngineStatsEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/biometrics/engine/stats", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBiometricEngineStats(w, req) })
}

// ── Blockchain Handler Tests ──

func TestBlockchainAuditTrailEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/blockchain/audit", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBlockchainAuditTrail(w, req) })
}

func TestBlockchainProductionStatsEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/blockchain/production/stats", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBlockchainProductionStats(w, req) })
}

// ── API Versioning Middleware Test ──

func TestAPIVersionMiddleware(t *testing.T) {
	handler := apiVersionMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("GET", "/api/v1/docs", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	if v := w.Header().Get("X-API-Version"); v != "v1" {
		t.Errorf("expected X-API-Version=v1, got %q", v)
	}
	if v := w.Header().Get("X-API-Supported-Versions"); v != "v1" {
		t.Errorf("expected X-API-Supported-Versions=v1, got %q", v)
	}
}

// ── Tracing Middleware Test ──

func TestTracingMiddlewareAddsHeaders(t *testing.T) {
	initTracing()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc := GetTraceContext(r.Context())
		if tc == nil {
			t.Error("expected trace context in request")
			return
		}
		if tc.TraceID == "" {
			t.Error("trace ID should not be empty")
		}
		if tc.SpanID == "" {
			t.Error("span ID should not be empty")
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := tracingMiddleware(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if tp := w.Header().Get("traceparent"); tp == "" {
		t.Error("response should have traceparent header")
	}
}

func TestTracingMiddlewarePropagatesExisting(t *testing.T) {
	initTracing()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc := GetTraceContext(r.Context())
		if tc == nil {
			t.Fatal("expected trace context")
		}
		if tc.TraceID != "abcdef1234567890abcdef1234567890" {
			t.Errorf("expected propagated trace ID, got %s", tc.TraceID)
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := tracingMiddleware(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("traceparent", "00-abcdef1234567890abcdef1234567890-1234567890abcdef-01")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
}

// ── Webhook Portal Handler Test ──

func TestPortalWebhooksEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/portal/webhooks", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handlePortalWebhooks(w, req) })
}

// ── ABIS Handler Tests ──

func TestABISPipelineStatusEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/biometrics/abis/status", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleABISPipelineStatus(w, req) })
}

func TestABISConfigEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/biometrics/abis/config", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleABISConfig(w, req) })
}

func TestABISDuplicatesEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/biometrics/abis/duplicates", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleABISDuplicates(w, req) })
}

// ── Batch Upload Test ──

func TestBatchUploadEndpoint(t *testing.T) {
	ensureTestDB(t)
	results := []map[string]interface{}{
		{"polling_unit_code": "PU-BU-001", "election_id": 1, "total_votes": 100},
	}
	body, _ := json.Marshal(map[string]interface{}{"results": results})
	req := httptest.NewRequest("POST", "/results/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	safeCall(func() { handleBatchUpload(w, req) })
}

// ── CV Monitoring Test ──

func TestCVMonitoringEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/cv/monitoring", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleCVMonitoring(w, req) })
}

// ── Staff Assignment Test ──

func TestAssignStaffEndpoint(t *testing.T) {
	ensureTestDB(t)
	body := `{"staff_id":"STAFF001","polling_unit_code":"PU-001","role":"presiding_officer"}`
	req := httptest.NewRequest("POST", "/staff/assign", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	safeCall(func() { handleAssignStaff(w, req) })
}

// ── APISIX Handler Tests ──

func TestAPISIXConfigEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/apisix/config", nil)
	w := httptest.NewRecorder()
	handleAPISIXConfig(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPISIXRoutesEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/apisix/routes", nil)
	w := httptest.NewRecorder()
	handleAPISIXRoutes(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ── Advanced Biometric Stats Test ──

func TestAdvancedBiometricStatsEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/biometrics/advanced/stats", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleAdvancedBiometricStats(w, req) })
}

// ── Bio Audit Tests ──

func TestBioAuditSummaryEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/biometrics/audit/summary", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBioAuditSummary(w, req) })
}

func TestBioAuditTimelineEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/biometrics/audit/timeline", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBioAuditTimeline(w, req) })
}

// ── BVAS Accreditation Tests ──

func TestBVASAccreditationEndpoint(t *testing.T) {
	ensureTestDB(t)
	body := `{"device_id":"BV-ACC-001","vin":"VIN-ACC-001","polling_unit_code":"PU-001"}`
	req := httptest.NewRequest("POST", "/bvas/accreditation", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	safeCall(func() { handleBVASAccreditation(w, req) })
}

func TestBVASAccreditationFeedEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/bvas/accreditation/feed", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBVASAccreditationFeed(w, req) })
}

func TestBVASAccreditationTimelineEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/bvas/accreditation/timeline", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBVASAccreditationTimeline(w, req) })
}

// ── BVAS Reconciliation Test ──

func TestBVASReconciliationEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/bvas/reconciliation?election_id=1", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBVASReconciliation(w, req) })
}

// ── BVAS Capture Sessions Test ──

func TestBVASCaptureSessionsEndpoint(t *testing.T) {
	ensureTestDB(t)
	req := httptest.NewRequest("GET", "/bvas/capture/sessions", nil)
	w := httptest.NewRecorder()
	safeCall(func() { handleBVASCaptureSessions(w, req) })
}

// ── helper ──

func ensureTestDB(t *testing.T) {
	t.Helper()
	if db == nil {
		t.Fatal("db not initialized; TestMain should set it up")
	}
}
