package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// --- Election FSM Tests ---

func TestFSMTransitionDefinitions(t *testing.T) {
	if len(electionFSM) < 8 {
		t.Errorf("expected at least 8 FSM transitions, got %d", len(electionFSM))
	}
	events := map[string]bool{}
	for _, tr := range electionFSM {
		events[tr.Event] = true
	}
	required := []string{"schedule", "activate", "open_voting", "close_voting", "finalize", "cancel", "dispute", "resolve_dispute"}
	for _, ev := range required {
		if !events[ev] {
			t.Errorf("missing FSM event: %s", ev)
		}
	}
}

func TestFSMInvalidTransitionReturnsError(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	// Create a test election in 'draft' state
	res, err := db.Exec("INSERT INTO elections (title, election_type, election_date, status) VALUES ('Test FSM Election','presidential','2026-12-01','draft')")
	if err != nil {
		t.Fatalf("failed to create test election: %v", err)
	}
	elID, _ := res.LastInsertId()

	// Try invalid transition: draft → voting (not allowed)
	err = TransitionElection(context.Background(), int(elID), "open_voting", "test_admin")
	if err == nil {
		t.Error("expected error for invalid transition draft→voting, got nil")
	}

	// Clean up
	db.Exec("DELETE FROM elections WHERE id=?", elID)
}

func TestFSMCancelFromDraft(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	res, err := db.Exec("INSERT INTO elections (title, election_type, election_date, status) VALUES ('Test Cancel','gubernatorial','2026-12-01','draft')")
	if err != nil {
		t.Fatalf("failed to create test election: %v", err)
	}
	elID, _ := res.LastInsertId()

	err = TransitionElection(context.Background(), int(elID), "cancel", "test_admin")
	if err != nil {
		t.Errorf("expected cancel from draft to succeed, got: %v", err)
	}

	var status string
	db.QueryRow("SELECT status FROM elections WHERE id=?", elID).Scan(&status)
	if status != "cancelled" {
		t.Errorf("expected status 'cancelled', got '%s'", status)
	}

	db.Exec("DELETE FROM elections WHERE id=?", elID)
	db.Exec("DELETE FROM election_state_log WHERE election_id=?", elID)
}

func TestFSMScheduleGuardRejectsEarlyDate(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	// Create election with tomorrow's date (< 7 days)
	tomorrow := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	res, err := db.Exec("INSERT INTO elections (title, election_type, election_date, status) VALUES ('Test Schedule','presidential',?,'draft')", tomorrow)
	if err != nil {
		t.Fatalf("failed to create test election: %v", err)
	}
	elID, _ := res.LastInsertId()

	err = TransitionElection(context.Background(), int(elID), "schedule", "test_admin")
	if err == nil {
		t.Error("expected guard to reject scheduling < 7 days in advance")
	}

	db.Exec("DELETE FROM elections WHERE id=?", elID)
}

func TestFSMDiagramEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	res, _ := db.Exec("INSERT INTO elections (title, election_type, election_date, status) VALUES ('Test Diagram','presidential','2026-12-01','draft')")
	elID, _ := res.LastInsertId()

	r := mux.NewRouter()
	r.HandleFunc("/ems/elections/{id}/fsm/diagram", handleElectionFSMDiagram).Methods("GET")

	req := httptest.NewRequest("GET", fmt.Sprintf("/ems/elections/%d/fsm/diagram", elID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["current_state"] != "draft" {
		t.Errorf("expected current_state 'draft', got '%v'", body["current_state"])
	}
	transitions, ok := body["transitions"].([]interface{})
	if !ok || len(transitions) < 8 {
		t.Errorf("expected at least 8 transitions in diagram")
	}

	db.Exec("DELETE FROM elections WHERE id=?", elID)
}

func TestFSMTransitionEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}
	res, _ := db.Exec("INSERT INTO elections (title, election_type, election_date, status) VALUES ('Test Transition','presidential','2026-12-01','draft')")
	elID, _ := res.LastInsertId()

	r := mux.NewRouter()
	r.HandleFunc("/ems/elections/{id}/fsm/transition", handleElectionFSMTransition).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})
	body, _ := json.Marshal(map[string]string{"event": "cancel"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/ems/elections/%d/fsm/transition", elID), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200 for cancel transition, got %d: %s", w.Code, w.Body.String())
	}

	db.Exec("DELETE FROM elections WHERE id=?", elID)
	db.Exec("DELETE FROM election_state_log WHERE election_id=?", elID)
}

func TestFSMRejectsNonAdmin(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/ems/elections/{id}/fsm/transition", adminOnly(handleElectionFSMTransition)).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "observer", "role": "observer", "full_name": "Obs",
	})
	body, _ := json.Marshal(map[string]string{"event": "cancel"})
	req := httptest.NewRequest("POST", "/ems/elections/1/fsm/transition", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("expected 403 for observer, got %d", w.Code)
	}
}

// --- GPS Spoofing Detection Tests ---

func TestGPSSpoofingTeleportation(t *testing.T) {
	now := time.Now()
	current := &GPSTrackPoint{Lat: 6.5244, Lng: 3.3792, Timestamp: now, Accuracy: 5}
	previous := &GPSTrackPoint{Lat: 9.0579, Lng: 7.4951, Timestamp: now.Add(-5 * time.Second), Accuracy: 5}

	analysis := analyzeGPSSpoofing(current, previous, map[string]interface{}{})

	if !analysis.IsSpoofed {
		t.Error("expected spoofing detection for teleportation (Lagos→Abuja in 5s)")
	}
	if analysis.VelocityKmh < 500 {
		t.Errorf("expected velocity > 500 km/h, got %.1f", analysis.VelocityKmh)
	}
	if !analysis.JumpDetected {
		t.Error("expected jump_detected=true")
	}
}

func TestGPSSpoofingNormalMovement(t *testing.T) {
	now := time.Now()
	current := &GPSTrackPoint{Lat: 9.0580, Lng: 7.4952, Timestamp: now, Accuracy: 5}
	previous := &GPSTrackPoint{Lat: 9.0579, Lng: 7.4951, Timestamp: now.Add(-60 * time.Second), Accuracy: 5}

	analysis := analyzeGPSSpoofing(current, previous, map[string]interface{}{})

	if analysis.IsSpoofed {
		t.Errorf("normal movement should not be flagged as spoofing (velocity: %.1f km/h)", analysis.VelocityKmh)
	}
}

func TestGPSSpoofingMockProvider(t *testing.T) {
	current := &GPSTrackPoint{Lat: 9.0, Lng: 7.0, Timestamp: time.Now(), Accuracy: 0}
	meta := map[string]interface{}{"is_mock_provider": true}

	analysis := analyzeGPSSpoofing(current, nil, meta)

	if !analysis.MockProvider {
		t.Error("expected mock_provider=true")
	}
	if !analysis.IsSpoofed {
		t.Error("expected spoofing detection for mock provider + zero accuracy")
	}
}

func TestGPSSpoofingZeroAccuracy(t *testing.T) {
	current := &GPSTrackPoint{Lat: 9.0, Lng: 7.0, Timestamp: time.Now(), Accuracy: 0}

	analysis := analyzeGPSSpoofing(current, nil, map[string]interface{}{})

	if analysis.Confidence < 0.7 {
		t.Errorf("zero accuracy should have high confidence, got %.2f", analysis.Confidence)
	}
}

func TestGPSSpoofingImpossibleAltitude(t *testing.T) {
	current := &GPSTrackPoint{Lat: 9.0, Lng: 7.0, Timestamp: time.Now(), Accuracy: 5}
	meta := map[string]interface{}{"altitude": float64(-500)}

	analysis := analyzeGPSSpoofing(current, nil, meta)

	foundIndicator := false
	for _, ind := range analysis.Indicators {
		if len(ind) > 0 {
			foundIndicator = true
		}
	}
	if !foundIndicator {
		t.Error("expected altitude indicator for -500m")
	}
}

func TestGPSSpoofingZeroJitter(t *testing.T) {
	current := &GPSTrackPoint{Lat: 9.0, Lng: 7.0, Timestamp: time.Now(), Accuracy: 5}
	meta := map[string]interface{}{"position_jitter_m": float64(0)}

	analysis := analyzeGPSSpoofing(current, nil, meta)

	found := false
	for _, ind := range analysis.Indicators {
		if ind == "zero_jitter: no natural GPS variation" {
			found = true
		}
	}
	if !found {
		t.Error("expected zero_jitter indicator")
	}
}

func TestGPSSpoofCheckEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/geo/spoof-check", handleGPSSpoofCheck).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "officer", "role": "officer", "full_name": "Officer",
	})

	body, _ := json.Marshal(map[string]interface{}{
		"device_id": "BVAS-001",
		"lat":       9.0579,
		"lng":       7.4951,
		"accuracy":  5.0,
		"meta":      map[string]interface{}{},
	})
	req := httptest.NewRequest("POST", "/geo/spoof-check", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200 for valid GPS, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Duplicate Voter Detection Tests ---

func TestDuplicateVoterScanEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/voters/duplicates/scan", handleDuplicateVoterScan).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	req := httptest.NewRequest("POST", "/voters/duplicates/scan", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if _, ok := body["total_duplicates"]; !ok {
		t.Error("expected total_duplicates in response")
	}
	if _, ok := body["by_nin"]; !ok {
		t.Error("expected by_nin in response")
	}
	if _, ok := body["by_biometric"]; !ok {
		t.Error("expected by_biometric in response")
	}
}

func TestDuplicateVoterResolveEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	// Create dedup_resolutions table
	db.Exec(`CREATE TABLE IF NOT EXISTS dedup_resolutions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		voter_a_vin TEXT, voter_b_vin TEXT,
		decision TEXT, reason TEXT,
		resolved_by TEXT, resolved_at TIMESTAMP
	)`)

	r := mux.NewRouter()
	r.HandleFunc("/voters/duplicates/resolve", handleDuplicateVoterResolve).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	body, _ := json.Marshal(map[string]interface{}{
		"voter_a_vin": "VIN-001",
		"voter_b_vin": "VIN-002",
		"decision":    "dismiss",
		"reason":      "Different persons with same name",
	})
	req := httptest.NewRequest("POST", "/voters/duplicates/resolve", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDuplicateVoterResolveInvalidDecision(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/voters/duplicates/resolve", handleDuplicateVoterResolve).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	body, _ := json.Marshal(map[string]interface{}{
		"voter_a_vin": "VIN-001",
		"voter_b_vin": "VIN-002",
		"decision":    "invalid_decision",
	})
	req := httptest.NewRequest("POST", "/voters/duplicates/resolve", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400 for invalid decision, got %d", w.Code)
	}
}

// --- Export Tests ---

func TestExportResultsJSON(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/export/results", handleExportResults).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	req := httptest.NewRequest("GET", "/export/results?format=json", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
}

func TestExportResultsCSV(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/export/results", handleExportResults).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	req := httptest.NewRequest("GET", "/export/results?format=csv", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	disp := w.Header().Get("Content-Disposition")
	if disp == "" {
		t.Error("expected Content-Disposition header for CSV download")
	}
}

func TestExportVotersRequiresAdmin(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/export/voters", adminOnly(handleExportVoters)).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "observer", "role": "observer", "full_name": "Obs",
	})

	req := httptest.NewRequest("GET", "/export/voters", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("expected 403 for observer role, got %d", w.Code)
	}
}

func TestExportCollation(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/export/collation", handleExportCollation).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	req := httptest.NewRequest("GET", "/export/collation?election_id=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuditExport(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/export/audit", handleAuditExport).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	req := httptest.NewRequest("GET", "/export/audit?format=json", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Webhook Tests ---

func TestWebhookCRUD(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	initWebhookSchema()

	r := mux.NewRouter()
	r.HandleFunc("/api/v1/webhooks", handleWebhookCreate).Methods("POST")
	r.HandleFunc("/api/v1/webhooks", handleWebhookList).Methods("GET")
	r.HandleFunc("/api/v1/webhooks/{id}", handleWebhookDelete).Methods("DELETE")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	// Create
	body, _ := json.Marshal(map[string]interface{}{
		"url":    "https://example.com/webhook",
		"events": []string{"result.submitted", "election.status_changed"},
		"secret": "test-hmac-secret",
	})
	req := httptest.NewRequest("POST", "/api/v1/webhooks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 201 && w.Code != 200 {
		t.Errorf("expected 200/201 for webhook create, got %d: %s", w.Code, w.Body.String())
	}

	// List
	req = httptest.NewRequest("GET", "/api/v1/webhooks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200 for webhook list, got %d", w.Code)
	}
}

func TestHMACComputation(t *testing.T) {
	hmac1 := computeHMAC([]byte(`{"event":"test"}`), "secret123")
	hmac2 := computeHMAC([]byte(`{"event":"test"}`), "secret123")
	if hmac1 != hmac2 {
		t.Error("HMAC should be deterministic")
	}

	hmac3 := computeHMAC([]byte(`{"event":"test"}`), "different-secret")
	if hmac1 == hmac3 {
		t.Error("different secrets should produce different HMACs")
	}
}

// --- SSE Dashboard Tests ---

func TestDashboardSSEEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	// Use a cancellable context so we can cleanly stop the SSE handler.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	r := mux.NewRouter()
	r.HandleFunc("/dashboard/stream", handleDashboardSSE).Methods("GET")

	req := httptest.NewRequest("GET", "/dashboard/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	// ServeHTTP will block until the context is cancelled.
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- OIDC Discovery Tests ---

func TestOIDCDiscoveryEndpoint(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/.well-known/openid-configuration", handleOIDCDiscovery).Methods("GET")

	req := httptest.NewRequest("GET", "/.well-known/openid-configuration", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if _, ok := body["issuer"]; !ok {
		t.Error("OIDC discovery must include 'issuer'")
	}
	if _, ok := body["authorization_endpoint"]; !ok {
		t.Error("OIDC discovery must include 'authorization_endpoint'")
	}
	if _, ok := body["token_endpoint"]; !ok {
		t.Error("OIDC discovery must include 'token_endpoint'")
	}
}

// --- Middleware Workflows Tests ---

func TestPublishToFluvioNilHub(t *testing.T) {
	oldHub := mwHub
	mwHub = nil
	defer func() { mwHub = oldHub }()

	err := PublishToFluvio(context.Background(), "test-topic", "test-data")
	if err != nil {
		t.Errorf("expected nil error when hub is nil, got: %v", err)
	}
}

func TestSaveElectionStateNilHub(t *testing.T) {
	oldHub := mwHub
	mwHub = nil
	defer func() { mwHub = oldHub }()

	err := SaveElectionState(context.Background(), 1, map[string]interface{}{"status": "active"})
	if err != nil {
		t.Errorf("expected nil error when hub is nil, got: %v", err)
	}
}

func TestPublishElectionEventNilHub(t *testing.T) {
	oldHub := mwHub
	mwHub = nil
	defer func() { mwHub = oldHub }()

	err := PublishElectionEvent(context.Background(), "test-topic", map[string]interface{}{})
	if err != nil {
		t.Errorf("expected nil error when hub is nil, got: %v", err)
	}
}

func TestCheckPermissionNilHub(t *testing.T) {
	oldHub := mwHub
	mwHub = nil
	defer func() { mwHub = oldHub }()

	allowed, err := CheckPermission(context.Background(), "admin", "manage", "election-1")
	if err != nil {
		t.Errorf("expected nil error when hub is nil, got: %v", err)
	}
	if !allowed {
		t.Error("fallback should allow (RBAC bypass)")
	}
}

func TestRegisterAPIRouteNilHub(t *testing.T) {
	oldHub := mwHub
	mwHub = nil
	defer func() { mwHub = oldHub }()

	err := RegisterAPIRoute(context.Background(), "/api/test", "localhost:8080", 100)
	if err != nil {
		t.Errorf("expected nil error when hub is nil, got: %v", err)
	}
}

func TestReportThreatNilHub(t *testing.T) {
	oldHub := mwHub
	mwHub = nil
	defer func() { mwHub = oldHub }()

	// Should not panic
	ReportThreatToOpenAppSec(context.Background(), "sqli", "1.2.3.4", "test threat")
}

// --- WebSocket Hub Tests ---

func TestWebSocketHubCreation(t *testing.T) {
	hub := newWebSocketHub()
	if hub == nil {
		t.Fatal("newWebSocketHub returned nil")
	}
	if hub.broadcast == nil {
		t.Error("broadcast channel is nil")
	}
	if hub.register == nil {
		t.Error("register channel is nil")
	}
	if hub.unregister == nil {
		t.Error("unregister channel is nil")
	}
}

// --- Election State Constants Tests ---

func TestElectionStatesAreDistinct(t *testing.T) {
	states := []ElectionState{
		ElectionStateDraft, ElectionStateScheduled, ElectionStateActive,
		ElectionStateVoting, ElectionStateCollating, ElectionStateClosed,
		ElectionStateCancelled, ElectionStateDisputed,
	}
	seen := map[ElectionState]bool{}
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate state: %s", s)
		}
		seen[s] = true
	}
	if len(states) != 8 {
		t.Errorf("expected 8 states, got %d", len(states))
	}
}

// --- Dispute Resolution Tests ---

func TestFileDisputeEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	// Create election for dispute
	res, _ := db.Exec("INSERT INTO elections (title, election_type, election_date, status) VALUES ('Dispute Test','presidential','2026-12-01','active')")
	elID, _ := res.LastInsertId()
	defer db.Exec("DELETE FROM elections WHERE id=?", elID)

	r := mux.NewRouter()
	r.HandleFunc("/disputes", handleFileDispute).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	body, _ := json.Marshal(map[string]interface{}{
		"election_id": elID,
		"category":    "overvoting",
		"description": "More votes than registered voters at PU 001",
		"party":       "APC",
	})
	req := httptest.NewRequest("POST", "/disputes", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "filed" {
		t.Errorf("expected status 'filed', got '%v'", resp["status"])
	}

	// Clean up
	if id, ok := resp["dispute_id"].(float64); ok {
		db.Exec("DELETE FROM disputes WHERE id=?", int(id))
	}
}

func TestFileDisputeInvalidCategory(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/disputes", handleFileDispute).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	body, _ := json.Marshal(map[string]interface{}{
		"election_id": 999,
		"category":    "invalid_category",
		"description": "test",
	})
	req := httptest.NewRequest("POST", "/disputes", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400 for invalid category, got %d", w.Code)
	}
}

func TestDisputeStatsEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/disputes/stats", handleDisputeStats).Methods("GET")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	req := httptest.NewRequest("GET", "/disputes/stats", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["categories"]; !ok {
		t.Error("expected categories in response")
	}
}

func TestDisputeResolveWorkflow(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	// Insert a dispute directly
	res, err := db.Exec("INSERT INTO disputes (election_id, filed_by, category, description) VALUES (1, 'observer1', 'overvoting', 'Test dispute')")
	if err != nil {
		t.Fatalf("failed to insert dispute: %v", err)
	}
	disputeID, _ := res.LastInsertId()
	defer db.Exec("DELETE FROM disputes WHERE id=?", disputeID)

	r := mux.NewRouter()
	r.HandleFunc("/disputes/{id}/resolve", handleResolveDispute).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "admin", "role": "admin", "full_name": "Admin",
	})

	// Step 1: Review
	body, _ := json.Marshal(map[string]string{"action": "review", "assign_to": "officer1"})
	req := httptest.NewRequest("POST", fmt.Sprintf("/disputes/%d/resolve", disputeID), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("review: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify status changed
	var status string
	db.QueryRow("SELECT status FROM disputes WHERE id=?", disputeID).Scan(&status)
	if status != "under_review" {
		t.Errorf("expected 'under_review', got '%s'", status)
	}

	// Step 2: Resolve
	body, _ = json.Marshal(map[string]string{"action": "resolve", "resolution": "Votes recounted and verified"})
	req = httptest.NewRequest("POST", fmt.Sprintf("/disputes/%d/resolve", disputeID), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("resolve: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	db.QueryRow("SELECT status FROM disputes WHERE id=?", disputeID).Scan(&status)
	if status != "resolved" {
		t.Errorf("expected 'resolved', got '%s'", status)
	}
}

// --- Push Device Registration Tests ---

func TestRegisterDeviceEndpoint(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/push/devices", handleRegisterDevice).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "observer", "role": "observer", "full_name": "Observer",
	})

	body, _ := json.Marshal(map[string]string{
		"device_token": "test-token-123",
		"platform":     "android",
		"app_version":  "1.0.0",
	})
	req := httptest.NewRequest("POST", "/push/devices", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["platform"] != "android" {
		t.Errorf("expected platform 'android', got '%v'", resp["platform"])
	}

	db.Exec("DELETE FROM push_devices WHERE device_token='test-token-123'")
}

func TestRegisterDeviceInvalidPlatform(t *testing.T) {
	if db == nil {
		t.Skip("database not initialized")
	}

	r := mux.NewRouter()
	r.HandleFunc("/push/devices", handleRegisterDevice).Methods("POST")
	handler := jwtAuthMiddleware(r)

	token, _ := createAccessToken(map[string]interface{}{
		"sub": "1", "username": "observer", "role": "observer", "full_name": "Observer",
	})

	body, _ := json.Marshal(map[string]string{
		"device_token": "test-token-456",
		"platform":     "blackberry",
	})
	req := httptest.NewRequest("POST", "/push/devices", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400 for invalid platform, got %d", w.Code)
	}
}
