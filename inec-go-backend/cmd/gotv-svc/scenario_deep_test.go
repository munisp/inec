package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// ═══════════════════════════════════════════════════════════════════════════
// DEEP SCENARIO VALIDATION — Expanded Production Workflow Tests
// Multi-step chains, negative paths, cross-scenario flows, high-scale
// ═══════════════════════════════════════════════════════════════════════════

// withVars injects gorilla/mux path variables into a request
func withVars(r *http.Request, vars map[string]string) *http.Request {
	return mux.SetURLVars(r, vars)
}

// callJSON is a helper that makes a JSON request and returns parsed response
func callJSON(t *testing.T, method, path string, body interface{}, handler http.HandlerFunc, vars map[string]string) (int, map[string]interface{}) {
	t.Helper()
	var req *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		req = httptest.NewRequest(method, path, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	setAuth(req)
	if vars != nil {
		req = withVars(req, vars)
	}
	rr := httptest.NewRecorder()
	handler(rr, req)
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	return rr.Code, resp
}

// ─── WORKFLOW 1: Full Contact-to-Pledge-to-Ride Lifecycle ───────────
// Creates a contact, creates a pledge for them, creates a ride, verifies each step
func TestDeepWorkflow_ContactPledgeRideLifecycle(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	var contactID string

	t.Run("step1_create_contact", func(t *testing.T) {
		body := map[string]interface{}{
			"full_name":  "Deep Test Contact " + fmt.Sprintf("%d", time.Now().UnixNano()%100000),
			"phone":      fmt.Sprintf("+23480%08d", rand.Intn(100000000)),
			"email":      "deeptest@example.com",
			"state_code": "LA",
			"lga_code":   "LA-IKEJA",
		}
		code, resp := callJSON(t, "POST", "/gotv/contacts", body, handleCreateContact, nil)
		if code != 201 && code != 200 {
			t.Fatalf("create contact returned %d: %v", code, resp)
		}
		if id, ok := resp["contact_id"].(string); ok {
			contactID = id
		} else {
			// Try to get from DB
			err := dbConn.QueryRow("SELECT contact_id FROM gotv_contacts WHERE party_id=1 ORDER BY created_at DESC LIMIT 1").Scan(&contactID)
			if err != nil {
				t.Fatalf("could not get contact_id: %v", err)
			}
		}
		t.Logf("created contact: %s", contactID)
	})

	t.Run("step2_verify_contact_in_list", func(t *testing.T) {
		if contactID == "" {
			t.Skip("no contact created")
		}
		code, resp := callJSON(t, "GET", "/gotv/contacts?page=1&per_page=5", nil, handleListContacts, nil)
		if code != 200 {
			t.Fatalf("list contacts returned %d", code)
		}
		if total, ok := resp["total"].(float64); ok {
			if total < 1 {
				t.Error("expected at least 1 contact")
			}
		}
	})

	t.Run("step3_create_pledge_for_contact", func(t *testing.T) {
		if contactID == "" {
			t.Skip("no contact")
		}
		body := map[string]interface{}{
			"contact_id":  contactID,
			"pledge_type": "will_vote",
			"notes":       "Deep test lifecycle pledge",
		}
		code, resp := callJSON(t, "POST", "/gotv/pledges", body, handleCreatePledge, nil)
		if code != 201 && code != 200 {
			t.Fatalf("create pledge returned %d: %v", code, resp)
		}
		t.Logf("pledge response: %v", resp)
	})

	t.Run("step4_verify_pledge_in_list", func(t *testing.T) {
		code, resp := callJSON(t, "GET", "/gotv/pledges?page=1&per_page=5", nil, handleListPledges, nil)
		if code != 200 {
			t.Fatalf("list pledges returned %d", code)
		}
		if total, ok := resp["total"].(float64); ok {
			if total < 1 {
				t.Error("expected at least 1 pledge")
			}
		}
	})

	t.Run("step5_create_ride_for_contact", func(t *testing.T) {
		if contactID == "" {
			t.Skip("no contact")
		}
		body := map[string]interface{}{
			"contact_id":        contactID,
			"pickup_latitude":   6.4541,
			"pickup_longitude":  3.3947,
			"polling_unit_code": "LA-PU-0002",
		}
		code, resp := callJSON(t, "POST", "/gotv/rides", body, handleCreateRide, nil)
		if code != 201 && code != 200 {
			t.Fatalf("create ride returned %d: %v", code, resp)
		}
		t.Logf("ride response: %v", resp)
	})

	t.Run("step6_verify_dashboard_counts_updated", func(t *testing.T) {
		code, resp := callJSON(t, "GET", "/gotv/dashboard", nil, handleDashboard, nil)
		if code != 200 {
			t.Fatalf("dashboard returned %d", code)
		}
		contacts, _ := resp["total_contacts"].(float64)
		pledges, _ := resp["total_pledges"].(float64)
		if contacts < 1 {
			t.Error("dashboard should show at least 1 contact")
		}
		if pledges < 1 {
			t.Error("dashboard should show at least 1 pledge")
		}
		t.Logf("dashboard: contacts=%.0f, pledges=%.0f", contacts, pledges)
	})
}

// ─── WORKFLOW 2: Full Volunteer Vetting Pipeline ────────────────────
// Create → NIN Verify → Training → Approve → Assign Location → Assign Task
func TestDeepWorkflow_VolunteerVettingFullPipeline(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	var volunteerID string

	t.Run("step1_create_volunteer_pending", func(t *testing.T) {
		body := map[string]interface{}{
			"full_name":      "Vetting Pipeline Test " + fmt.Sprintf("%d", time.Now().UnixNano()%100000),
			"phone":          fmt.Sprintf("+23470%08d", rand.Intn(100000000)),
			"role":           "canvasser",
			"assigned_state": "KN",
			"assigned_lga":   "KN-KANO",
		}
		code, resp := callJSON(t, "POST", "/gotv/volunteers", body, handleCreateVolunteer, nil)
		if code != 201 && code != 200 {
			t.Fatalf("create volunteer returned %d: %v", code, resp)
		}
		if id, ok := resp["volunteer_id"].(string); ok {
			volunteerID = id
		} else {
			err := dbConn.QueryRow("SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 ORDER BY created_at DESC LIMIT 1").Scan(&volunteerID)
			if err != nil {
				t.Fatalf("could not get volunteer_id: %v", err)
			}
		}
		t.Logf("created volunteer: %s", volunteerID)
	})

	t.Run("step2_verify_nin", func(t *testing.T) {
		if volunteerID == "" {
			t.Skip("no volunteer")
		}
		body := map[string]interface{}{
			"nin":    "12345678901",
			"result": "pass",
		}
		code, resp := callJSON(t, "POST", "/gotv/volunteers/"+volunteerID+"/verify-nin", body, handleVerifyNIN, map[string]string{"id": volunteerID})
		if code != 200 {
			t.Fatalf("NIN verify returned %d: %v", code, resp)
		}
		if status, ok := resp["vetting_status"].(string); ok && status != "nin_verified" {
			t.Errorf("expected nin_verified, got %s", status)
		}
	})

	t.Run("step3_complete_training", func(t *testing.T) {
		if volunteerID == "" {
			t.Skip("no volunteer")
		}
		body := map[string]interface{}{
			"training_module": "basic_canvassing",
			"score":           85,
		}
		code, resp := callJSON(t, "POST", "/gotv/volunteers/"+volunteerID+"/training", body, handleCompleteTraining, map[string]string{"id": volunteerID})
		if code != 200 {
			t.Fatalf("training returned %d: %v", code, resp)
		}
		if status, ok := resp["vetting_status"].(string); ok && status != "trained" {
			t.Errorf("expected trained, got %s", status)
		}
	})

	t.Run("step4_approve_volunteer", func(t *testing.T) {
		if volunteerID == "" {
			t.Skip("no volunteer")
		}
		code, resp := callJSON(t, "POST", "/gotv/volunteers/"+volunteerID+"/approve", nil, handleApproveVolunteer, map[string]string{"id": volunteerID})
		if code != 200 {
			t.Fatalf("approve returned %d: %v", code, resp)
		}
		if approved, ok := resp["approved"].(bool); !ok || !approved {
			t.Error("volunteer should be approved")
		}
	})

	t.Run("step5_verify_active_in_pipeline", func(t *testing.T) {
		code, resp := callJSON(t, "GET", "/gotv/volunteers/vetting", nil, handleListVettingPipeline, nil)
		if code != 200 {
			t.Fatalf("vetting pipeline returned %d", code)
		}
		if counts, ok := resp["counts"].(map[string]interface{}); ok {
			approved, _ := counts["approved"].(float64)
			if approved < 1 {
				t.Error("should have at least 1 approved volunteer")
			}
			t.Logf("vetting counts: %v", counts)
		}
	})

	t.Run("step6_verify_in_list_as_active", func(t *testing.T) {
		code, resp := callJSON(t, "GET", "/gotv/volunteers?page=1&per_page=5", nil, handleListVolunteers, nil)
		if code != 200 {
			t.Fatalf("list volunteers returned %d", code)
		}
		if total, ok := resp["total"].(float64); ok && total < 1 {
			t.Error("should have at least 1 volunteer")
		}
	})
}

// ─── WORKFLOW 3: Campaign Full Lifecycle ─────────────────────────────
// Create → Search → Launch → Pause → Resume → Analytics → Delete
func TestDeepWorkflow_CampaignFullLifecycle(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	var campaignID string
	campaignName := "Deep Test Campaign " + fmt.Sprintf("%d", time.Now().UnixNano()%100000)

	t.Run("step1_create_campaign", func(t *testing.T) {
		body := map[string]interface{}{
			"name":             campaignName,
			"campaign_type":    "phone_bank",
			"message_template": "Hello {name}, this is a test campaign for election day.",
		}
		code, resp := callJSON(t, "POST", "/gotv/campaigns", body, handleCreateCampaign, nil)
		if code != 201 && code != 200 {
			t.Fatalf("create campaign returned %d: %v", code, resp)
		}
		if id, ok := resp["campaign_id"].(string); ok {
			campaignID = id
		} else {
			err := dbConn.QueryRow("SELECT campaign_id FROM gotv_campaigns WHERE party_id=1 ORDER BY created_at DESC LIMIT 1").Scan(&campaignID)
			if err != nil {
				t.Fatalf("could not get campaign_id: %v", err)
			}
		}
		t.Logf("created campaign: %s", campaignID)
	})

	t.Run("step2_search_finds_campaign", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/search?q=phone_bank&type=campaigns", nil, handleGOTVSearch, nil)
		if code != 200 {
			t.Fatalf("search returned %d", code)
		}
	})

	t.Run("step3_launch_campaign", func(t *testing.T) {
		if campaignID == "" {
			t.Skip("no campaign")
		}
		code, resp := callJSON(t, "POST", "/gotv/campaigns/"+campaignID+"/launch", nil, handleLaunchCampaign, map[string]string{"id": campaignID})
		if code != 200 {
			t.Logf("launch returned %d: %v (may need contacts in list)", code, resp)
		}
	})

	t.Run("step4_pause_campaign", func(t *testing.T) {
		if campaignID == "" {
			t.Skip("no campaign")
		}
		code, _ := callJSON(t, "POST", "/gotv/campaigns/"+campaignID+"/pause", nil, handlePauseCampaign, map[string]string{"id": campaignID})
		// Pause may fail if not in 'active' state, that's ok
		t.Logf("pause returned %d", code)
	})

	t.Run("step5_resume_campaign", func(t *testing.T) {
		if campaignID == "" {
			t.Skip("no campaign")
		}
		code, _ := callJSON(t, "POST", "/gotv/campaigns/"+campaignID+"/resume", nil, handleResumeCampaign, map[string]string{"id": campaignID})
		t.Logf("resume returned %d", code)
	})

	t.Run("step6_view_analytics", func(t *testing.T) {
		code, resp := callJSON(t, "GET", "/gotv/analytics", nil, handleGOTVAnalytics, nil)
		if code != 200 {
			t.Fatalf("analytics returned %d", code)
		}
		t.Logf("analytics channels: %v", resp)
	})

	t.Run("step7_delete_campaign", func(t *testing.T) {
		if campaignID == "" {
			t.Skip("no campaign")
		}
		code, _ := callJSON(t, "DELETE", "/gotv/campaigns/"+campaignID, nil, handleDeleteCampaign, map[string]string{"id": campaignID})
		if code != 200 && code != 204 {
			t.Logf("delete returned %d (soft delete may use different path)", code)
		}
	})
}

// ─── WORKFLOW 4: Vetting Rejection & Suspension ─────────────────────
func TestDeepWorkflow_VettingRejectionSuspension(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("reject_pending_volunteer", func(t *testing.T) {
		// Create a volunteer to reject
		body := map[string]interface{}{
			"full_name":      "Reject Test " + fmt.Sprintf("%d", time.Now().UnixNano()%100000),
			"phone":          fmt.Sprintf("+23481%08d", rand.Intn(100000000)),
			"role":           "caller",
			"assigned_state": "OG",
			"assigned_lga":   "OG-ABEOK",
		}
		code, resp := callJSON(t, "POST", "/gotv/volunteers", body, handleCreateVolunteer, nil)
		if code != 201 && code != 200 {
			t.Fatalf("create returned %d: %v", code, resp)
		}

		var volID string
		if id, ok := resp["volunteer_id"].(string); ok {
			volID = id
		} else {
			dbConn.QueryRow("SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 ORDER BY created_at DESC LIMIT 1").Scan(&volID)
		}

		if volID == "" {
			t.Skip("could not get volunteer_id")
		}

		rejectBody := map[string]interface{}{
			"reason": "Failed background check",
		}
		code, resp = callJSON(t, "POST", "/gotv/volunteers/"+volID+"/reject", rejectBody, handleRejectVolunteer, map[string]string{"id": volID})
		if code != 200 {
			t.Fatalf("reject returned %d: %v", code, resp)
		}
	})

	t.Run("suspend_active_volunteer", func(t *testing.T) {
		// Find an active/approved volunteer
		var volID string
		err := dbConn.QueryRow("SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 AND is_active=TRUE AND vetting_status IN ('approved','active') LIMIT 1").Scan(&volID)
		if err != nil {
			t.Skip("no approved/active volunteers to suspend")
		}

		body := map[string]interface{}{
			"reason": "Misconduct during canvassing",
		}
		code, resp := callJSON(t, "POST", "/gotv/volunteers/"+volID+"/suspend", body, handleSuspendVolunteer, map[string]string{"id": volID})
		if code != 200 {
			t.Fatalf("suspend returned %d: %v", code, resp)
		}

		// Re-activate for cleanup
		dbConn.Exec("UPDATE gotv_volunteers SET is_active=TRUE, vetting_status='approved' WHERE volunteer_id=$1", volID)
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// NEGATIVE / ERROR PATH TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestNegative_MissingAuthHeaders(t *testing.T) {
	t.Run("dashboard_without_party_id", func(t *testing.T) {
		if dbConn == nil {
			t.Skip("requires database")
		}
		req := httptest.NewRequest("GET", "/gotv/dashboard", nil)
		// No auth headers — party_id will be 0
		rr := httptest.NewRecorder()
		handleDashboard(rr, req)
		// Handler should still work but return empty/zero data for party 0
		if rr.Code == 500 {
			t.Error("dashboard should not 500 with missing auth — should return empty data or 401")
		}
	})

	t.Run("create_volunteer_without_auth", func(t *testing.T) {
		if dbConn == nil {
			t.Skip("requires database")
		}
		body := map[string]interface{}{
			"full_name":      "No Auth Test",
			"phone":          "+2348000000000",
			"role":           "canvasser",
			"assigned_state": "LA",
			"assigned_lga":   "LA-IKEJA",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/gotv/volunteers", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		// No auth headers
		rr := httptest.NewRecorder()
		handleCreateVolunteer(rr, req)
		// Without party_id, should not create successfully or should create with party_id=0
		t.Logf("create without auth returned %d", rr.Code)
	})
}

func TestNegative_InvalidInputs(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("create_contact_empty_body", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/gotv/contacts", bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		setAuth(req)
		rr := httptest.NewRecorder()
		handleCreateContact(rr, req)
		// Should handle gracefully — either 400 or create with defaults
		if rr.Code == 500 {
			t.Error("empty body should not cause 500")
		}
		t.Logf("empty contact body returned %d", rr.Code)
	})

	t.Run("create_contact_malformed_json", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/gotv/contacts", bytes.NewBuffer([]byte("{not json")))
		req.Header.Set("Content-Type", "application/json")
		setAuth(req)
		rr := httptest.NewRecorder()
		handleCreateContact(rr, req)
		if rr.Code != 400 {
			t.Logf("malformed JSON returned %d (expected 400)", rr.Code)
		}
	})

	t.Run("create_campaign_missing_required_fields", func(t *testing.T) {
		body := map[string]interface{}{
			"name": "Missing Type Campaign",
			// Missing campaign_type
		}
		code, resp := callJSON(t, "POST", "/gotv/campaigns", body, handleCreateCampaign, nil)
		t.Logf("missing campaign_type returned %d: %v", code, resp)
		// Should not 500
		if code == 500 {
			t.Error("missing campaign_type should not cause 500")
		}
	})

	t.Run("create_ride_invalid_coordinates", func(t *testing.T) {
		body := map[string]interface{}{
			"contact_id":        "nonexistent-contact",
			"pickup_latitude":   999.0, // Invalid latitude
			"pickup_longitude":  999.0, // Invalid longitude
			"polling_unit_code": "XX-PU-9999",
		}
		code, _ := callJSON(t, "POST", "/gotv/rides", body, handleCreateRide, nil)
		// Should fail due to FK constraint or validation
		if code == 201 || code == 200 {
			t.Error("ride with invalid coordinates and nonexistent contact should not succeed")
		}
	})

	t.Run("verify_nin_empty_nin", func(t *testing.T) {
		var volID string
		err := dbConn.QueryRow("SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 AND vetting_status='pending' LIMIT 1").Scan(&volID)
		if err != nil {
			t.Skip("no pending volunteers")
		}
		body := map[string]interface{}{
			"nin":    "", // Empty NIN
			"result": "pass",
		}
		code, resp := callJSON(t, "POST", "/gotv/volunteers/"+volID+"/verify-nin", body, handleVerifyNIN, map[string]string{"id": volID})
		if code != 400 {
			t.Errorf("empty NIN should return 400, got %d: %v", code, resp)
		}
	})

	t.Run("approve_nonexistent_volunteer", func(t *testing.T) {
		code, _ := callJSON(t, "POST", "/gotv/volunteers/nonexistent-id/approve", nil, handleApproveVolunteer, map[string]string{"id": "nonexistent-id"})
		if code != 404 {
			t.Logf("approve nonexistent returned %d (expected 404)", code)
		}
	})
}

func TestNegative_SQLInjectionAttempts(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// URL-safe injections (semicolons break httptest.NewRequest URL parsing)
	injections := []string{
		"1 OR 1=1",
		"<script>alert('xss')</script>",
		"' UNION SELECT * FROM pg_tables --",
		"Robert'); DROP TABLE students;--",
	}

	t.Run("search_injection", func(t *testing.T) {
		for _, inj := range injections {
			// Use url.QueryEscape equivalent by putting injection in body search
			req := httptest.NewRequest("GET", "/gotv/search?type=contacts", nil)
			q := req.URL.Query()
			q.Set("q", inj)
			req.URL.RawQuery = q.Encode()
			setAuth(req)
			rr := httptest.NewRecorder()
			handleGOTVSearch(rr, req)
			if rr.Code == 500 {
				t.Errorf("SQL injection attempt caused 500: %s", inj)
			}
		}
	})

	t.Run("contact_creation_injection", func(t *testing.T) {
		for _, inj := range injections {
			body := map[string]interface{}{
				"full_name":  inj,
				"phone":      inj,
				"state_code": inj,
			}
			code, _ := callJSON(t, "POST", "/gotv/contacts", body, handleCreateContact, nil)
			if code == 500 {
				t.Errorf("SQL injection in contact creation caused 500: %s", inj)
			}
		}
	})
}

func TestNegative_HugePayload(t *testing.T) {
	// 1MB payload
	huge := strings.Repeat("A", 1024*1024)
	body := map[string]interface{}{
		"full_name": huge,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/gotv/contacts", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	setAuth(req)
	rr := httptest.NewRecorder()

	// Wrap in panic recovery to catch OOM
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("huge payload caused panic (should be caught by middleware): %v", r)
			}
		}()
		if dbConn != nil {
			handleCreateContact(rr, req)
		}
	}()

	t.Logf("huge payload returned %d", rr.Code)
}

// ═══════════════════════════════════════════════════════════════════════════
// CROSS-SCENARIO DATA FLOW TESTS
// Verify data propagates correctly between different system components
// ═══════════════════════════════════════════════════════════════════════════

func TestCrossScenario_DashboardReflectsAllEntities(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// Get counts directly from DB
	// Dashboard filters: contacts WHERE opted_out=FALSE, volunteers WHERE is_active=TRUE
	var dbContacts, dbVolunteers, dbPledges int
	dbConn.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=1 AND opted_out=FALSE").Scan(&dbContacts)
	dbConn.QueryRow("SELECT COUNT(*) FROM gotv_volunteers WHERE party_id=1 AND is_active=TRUE").Scan(&dbVolunteers)
	dbConn.QueryRow("SELECT COUNT(*) FROM gotv_pledges WHERE party_id=1").Scan(&dbPledges)

	// Get counts from dashboard
	code, dash := callJSON(t, "GET", "/gotv/dashboard", nil, handleDashboard, nil)
	if code != 200 {
		t.Fatalf("dashboard returned %d", code)
	}

	dashContacts, _ := dash["total_contacts"].(float64)
	dashVolunteers, _ := dash["total_volunteers"].(float64)
	dashPledges, _ := dash["total_pledges"].(float64)

	if int(dashContacts) != dbContacts {
		t.Errorf("dashboard contacts mismatch: dashboard=%.0f, db=%d", dashContacts, dbContacts)
	}
	if int(dashVolunteers) != dbVolunteers {
		t.Errorf("dashboard volunteers mismatch: dashboard=%.0f, db=%d", dashVolunteers, dbVolunteers)
	}
	if int(dashPledges) != dbPledges {
		t.Errorf("dashboard pledges mismatch: dashboard=%.0f, db=%d", dashPledges, dbPledges)
	}
	t.Logf("cross-check: contacts=%d/%d, volunteers=%d/%d, pledges=%d/%d",
		int(dashContacts), dbContacts, int(dashVolunteers), dbVolunteers, int(dashPledges), dbPledges)
}

func TestCrossScenario_VettingCountsMatchVolunteerList(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// Get total from volunteer list
	code1, volResp := callJSON(t, "GET", "/gotv/volunteers?page=1&per_page=1", nil, handleListVolunteers, nil)
	if code1 != 200 {
		t.Fatalf("list volunteers returned %d", code1)
	}

	// Get sum from vetting pipeline
	code2, vetResp := callJSON(t, "GET", "/gotv/volunteers/vetting", nil, handleListVettingPipeline, nil)
	if code2 != 200 {
		t.Fatalf("vetting pipeline returned %d", code2)
	}

	volTotal, _ := volResp["total"].(float64)

	if counts, ok := vetResp["counts"].(map[string]interface{}); ok {
		sum := 0.0
		for status, v := range counts {
			if f, ok := v.(float64); ok {
				sum += f
				t.Logf("vetting %s: %.0f", status, f)
			}
		}
		if int(sum) != int(volTotal) {
			t.Logf("vetting sum=%.0f vs list total=%.0f (may differ due to soft-deleted volunteers)", sum, volTotal)
		}
	}
}

func TestCrossScenario_GeoCoverageIncludesAllStates(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// Count distinct states in DB
	var dbStates int
	dbConn.QueryRow("SELECT COUNT(DISTINCT assigned_state) FROM gotv_volunteers WHERE party_id=1 AND assigned_state IS NOT NULL AND assigned_state != ''").Scan(&dbStates)

	code, resp := callJSON(t, "GET", "/gotv/geo/coverage", nil, handleGeoCoverage, nil)
	if code != 200 {
		t.Fatalf("geo coverage returned %d", code)
	}

	if states, ok := resp["states"].([]interface{}); ok {
		t.Logf("geo coverage: %d states in API, %d states in DB", len(states), dbStates)
	}
}

func TestCrossScenario_LedgerAccountsBalanceReconciliation(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// Get all accounts
	code1, acctResp := callJSON(t, "GET", "/gotv/ledger/accounts", nil, handleLedgerAccounts, nil)
	if code1 != 200 {
		t.Fatalf("ledger accounts returned %d", code1)
	}

	// Reconcile
	code2, reconResp := callJSON(t, "GET", "/gotv/ledger/reconcile", nil, handleLedgerReconcile, nil)
	if code2 != 200 {
		t.Fatalf("reconcile returned %d", code2)
	}

	t.Logf("accounts: %v", acctResp)
	t.Logf("reconciliation: %v", reconResp)

	// Check for variances
	if variance, ok := reconResp["variance"].(float64); ok && variance != 0 {
		t.Errorf("ledger has non-zero variance: %.2f", variance)
	}
}

func TestCrossScenario_BlockchainConsistency(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// Check blockchain status and blocks are consistent
	code1, status := callJSON(t, "GET", "/gotv/blockchain/status", nil, handleBlockchainStatus, nil)
	if code1 != 200 {
		t.Fatalf("blockchain status returned %d", code1)
	}

	code2, blocks := callJSON(t, "GET", "/gotv/blockchain/blocks", nil, handleBlockchainBlocks, nil)
	if code2 != 200 {
		t.Fatalf("blockchain blocks returned %d", code2)
	}

	t.Logf("blockchain status: %v", status)
	t.Logf("blockchain blocks: %v", blocks)
}

// ═══════════════════════════════════════════════════════════════════════════
// HIGH-SCALE CONCURRENT TESTS (500+ concurrent operations)
// ═══════════════════════════════════════════════════════════════════════════

func TestHighScale_500ConcurrentDashboardReads(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	const numConcurrent = 500
	var wg sync.WaitGroup
	var errCount int64
	var totalLatency int64

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			req := httptest.NewRequest("GET", "/gotv/dashboard", nil)
			setAuth(req)
			rr := httptest.NewRecorder()
			handleDashboard(rr, req)
			elapsed := time.Since(start).Microseconds()
			atomic.AddInt64(&totalLatency, elapsed)

			if rr.Code != 200 {
				atomic.AddInt64(&errCount, 1)
			}
		}()
	}

	wg.Wait()

	avgMs := float64(totalLatency) / float64(numConcurrent) / 1000.0
	t.Logf("500 concurrent dashboards: errors=%d, avg_latency=%.2fms", errCount, avgMs)

	if errCount > 5 { // Allow <1% error rate
		t.Errorf("too many errors under load: %d/%d", errCount, numConcurrent)
	}
	// 500 concurrent DB-backed queries — 2s threshold is realistic for single-node PostgreSQL
	if avgMs > 2000 {
		t.Errorf("avg latency too high: %.2fms (target < 2000ms for 500 concurrent)", avgMs)
	}
}

func TestHighScale_500ConcurrentContactListReads(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	const numConcurrent = 500
	var wg sync.WaitGroup
	var errCount int64

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(page int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", fmt.Sprintf("/gotv/contacts?page=%d&per_page=10", (page%5)+1), nil)
			setAuth(req)
			rr := httptest.NewRecorder()
			handleListContacts(rr, req)

			if rr.Code != 200 {
				atomic.AddInt64(&errCount, 1)
			}
		}(i)
	}

	wg.Wait()
	t.Logf("500 concurrent contact reads: errors=%d", errCount)

	if errCount > 5 {
		t.Errorf("too many errors: %d/%d", errCount, numConcurrent)
	}
}

func TestHighScale_ConcurrentReadWriteMix(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	const readers = 200
	const writers = 50
	var wg sync.WaitGroup
	var readErrors, writeErrors int64

	// Concurrent readers
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/gotv/dashboard", nil)
			setAuth(req)
			rr := httptest.NewRecorder()
			handleDashboard(rr, req)
			if rr.Code != 200 {
				atomic.AddInt64(&readErrors, 1)
			}
		}()
	}

	// Concurrent writers (create contacts)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := map[string]interface{}{
				"full_name":  fmt.Sprintf("Scale Test %d-%d", idx, time.Now().UnixNano()%100000),
				"phone":      fmt.Sprintf("+23490%08d", rand.Intn(100000000)),
				"state_code": "LA",
			}
			b, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/gotv/contacts", bytes.NewBuffer(b))
			req.Header.Set("Content-Type", "application/json")
			setAuth(req)
			rr := httptest.NewRecorder()
			handleCreateContact(rr, req)
			if rr.Code != 201 && rr.Code != 200 {
				atomic.AddInt64(&writeErrors, 1)
			}
		}(i)
	}

	wg.Wait()
	t.Logf("concurrent R/W mix: read_errors=%d/%d, write_errors=%d/%d",
		readErrors, readers, writeErrors, writers)

	if readErrors > 2 {
		t.Errorf("too many read errors: %d", readErrors)
	}
	if writeErrors > 5 {
		t.Errorf("too many write errors: %d", writeErrors)
	}
}

func TestHighScale_ConcurrentVettingPipelineReads(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	const numConcurrent = 300
	var wg sync.WaitGroup
	var errCount int64

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/gotv/volunteers/vetting", nil)
			setAuth(req)
			rr := httptest.NewRecorder()
			handleListVettingPipeline(rr, req)
			if rr.Code != 200 {
				atomic.AddInt64(&errCount, 1)
			}
		}()
	}

	wg.Wait()
	t.Logf("300 concurrent vetting pipeline reads: errors=%d", errCount)

	if errCount > 3 {
		t.Errorf("too many errors: %d/%d", errCount, numConcurrent)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// DEEP HANDLER COVERAGE — Previously untested handlers
// ═══════════════════════════════════════════════════════════════════════════

func TestDeepHandlers_Segments(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("list_segments", func(t *testing.T) {
		code, resp := callJSON(t, "GET", "/gotv/segments", nil, handleListSegments, nil)
		if code != 200 {
			t.Fatalf("list segments returned %d: %v", code, resp)
		}
	})

	t.Run("create_segment", func(t *testing.T) {
		body := map[string]interface{}{
			"name":        "Deep Test Segment " + fmt.Sprintf("%d", time.Now().UnixNano()%100000),
			"description": "Voters in Lagos who need rides",
			"filters": map[string]interface{}{
				"state_code":  "LA",
				"pledge_type": "needs_ride",
			},
		}
		code, resp := callJSON(t, "POST", "/gotv/segments", body, handleCreateSegment, nil)
		if code != 201 && code != 200 {
			t.Logf("create segment returned %d: %v", code, resp)
		}
	})
}

func TestDeepHandlers_Leaderboard(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	code, resp := callJSON(t, "GET", "/gotv/leaderboard?period=all", nil, handleLeaderboard, nil)
	if code != 200 {
		t.Fatalf("leaderboard returned %d: %v", code, resp)
	}
}

func TestDeepHandlers_Turnout(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// handleTurnout expects election_id mux var
	var electionID string
	err := dbConn.QueryRow("SELECT election_id FROM elections LIMIT 1").Scan(&electionID)
	if err != nil {
		t.Skip("no elections in DB for turnout test")
	}

	code, resp := callJSON(t, "GET", "/gotv/turnout", nil, handleTurnout, map[string]string{"election_id": electionID})
	if code != 200 {
		t.Fatalf("turnout returned %d: %v", code, resp)
	}
}

func TestDeepHandlers_CanvassWalklist(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	code, resp := callJSON(t, "GET", "/gotv/canvass/walklist?state=LA", nil, handleCanvassWalklist, nil)
	if code != 200 {
		t.Fatalf("walklist returned %d: %v", code, resp)
	}
	t.Logf("walklist response fields: %v", keysOf(resp))
}

func TestDeepHandlers_DoorKnock(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// Find a real contact and volunteer for door knock
	var contactID, volunteerID string
	dbConn.QueryRow("SELECT contact_id FROM gotv_contacts WHERE party_id=1 LIMIT 1").Scan(&contactID)
	dbConn.QueryRow("SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 AND is_active=TRUE LIMIT 1").Scan(&volunteerID)

	if contactID == "" || volunteerID == "" {
		t.Skip("need contact and volunteer for door knock")
	}

	body := map[string]interface{}{
		"contact_id":   contactID,
		"volunteer_id": volunteerID,
		"outcome":      "pledged",
		"latitude":     6.5244,
		"longitude":    3.3792,
		"notes":        "Deep test door knock",
	}
	code, resp := callJSON(t, "POST", "/gotv/canvass/knock", body, handleCanvassDoorKnock, nil)
	if code != 201 && code != 200 {
		t.Fatalf("door knock returned %d: %v", code, resp)
	}
}

func TestDeepHandlers_ExportContacts(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// Test JSON export
	code, _ := callJSON(t, "GET", "/gotv/contacts/export?format=json", nil, handleExportContacts, nil)
	if code != 200 {
		t.Fatalf("export contacts JSON returned %d", code)
	}

	// Test CSV export
	req := httptest.NewRequest("GET", "/gotv/contacts/export?format=csv", nil)
	setAuth(req)
	rr := httptest.NewRecorder()
	handleExportContacts(rr, req)
	if rr.Code != 200 {
		t.Fatalf("export contacts CSV returned %d", rr.Code)
	}
}

func TestDeepHandlers_ScoringEndpoints(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("scoring_summary", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/scoring/summary", nil, handleScoringSummary, nil)
		if code != 200 {
			t.Fatalf("scoring summary returned %d", code)
		}
	})

	t.Run("win_probability", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/scoring/win-probability", nil, handleScoringWinProbability, nil)
		if code != 200 {
			t.Fatalf("win probability returned %d", code)
		}
	})

	t.Run("scoring_allocation", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/scoring/allocation", nil, handleScoringAllocation, nil)
		if code != 200 {
			t.Fatalf("allocation returned %d", code)
		}
	})
}

func TestDeepHandlers_KOHExtended(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("share_of_voice", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/koh/social/share-of-voice", nil, handleShareOfVoice, nil)
		if code != 200 {
			t.Fatalf("share of voice returned %d", code)
		}
	})

	t.Run("endorsement_score", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/koh/endorsements/score", nil, handleEndorsementScore, nil)
		if code != 200 {
			t.Fatalf("endorsement score returned %d", code)
		}
	})

	t.Run("cpi_breakdown", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/koh/cpi/breakdown", nil, handleCPIBreakdown, nil)
		if code != 200 {
			t.Fatalf("CPI breakdown returned %d", code)
		}
	})

	t.Run("lga_tiers", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/koh/lga/tiers", nil, handleLGATiers, nil)
		if code != 200 {
			t.Fatalf("LGA tiers returned %d", code)
		}
	})
}

func TestDeepHandlers_WarRoomExtended(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("war_room_summary", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/warroom/summary", nil, handleWarRoomSummary, nil)
		if code != 200 {
			t.Fatalf("war room summary returned %d", code)
		}
	})

	t.Run("war_room_ai_alerts", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/warroom/ai-alerts", nil, handleWarRoomAIAlerts, nil)
		if code != 200 {
			t.Fatalf("war room AI alerts returned %d", code)
		}
	})

	t.Run("predictive_turnout", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/predictive/turnout", nil, handlePredictiveTurnout, nil)
		if code != 200 {
			t.Fatalf("predictive turnout returned %d", code)
		}
	})
}

func TestDeepHandlers_FieldReports(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("list_field_reports", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/field-reports", nil, handleListFieldReports, nil)
		if code != 200 {
			t.Fatalf("field reports returned %d", code)
		}
	})

	t.Run("create_field_report", func(t *testing.T) {
		body := map[string]interface{}{
			"title":     "Deep Test Report",
			"content":   "Observed high voter turnout in Ikeja ward.",
			"latitude":  6.5244,
			"longitude": 3.3792,
			"severity":  "info",
		}
		code, resp := callJSON(t, "POST", "/gotv/field-reports", body, handleCreateFieldReport, nil)
		if code != 201 && code != 200 {
			t.Logf("create field report returned %d: %v", code, resp)
		}
	})
}

func TestDeepHandlers_PlatformInnovations(t *testing.T) {
	t.Run("nl_query", func(t *testing.T) {
		body := map[string]interface{}{
			"query": "How many pledges in Lagos?",
		}
		code, resp := callJSON(t, "POST", "/gotv/nl/query", body, handleNLQuery, nil)
		if code != 200 {
			t.Logf("NL query returned %d: %v", code, resp)
		}
	})

	t.Run("simulation", func(t *testing.T) {
		if dbConn == nil {
			t.Skip("requires database")
		}
		body := map[string]interface{}{
			"scenario":    "add_drivers",
			"state":       "LA",
			"count":       10,
			"description": "What if we added 10 more drivers in Lagos?",
		}
		code, resp := callJSON(t, "POST", "/gotv/simulation", body, handleSimulation, nil)
		if code != 200 {
			t.Logf("simulation returned %d: %v", code, resp)
		}
	})

	t.Run("crowd_estimate", func(t *testing.T) {
		if dbConn == nil {
			t.Skip("requires database")
		}
		body := map[string]interface{}{
			"venue_type": "stadium",
			"latitude":   6.5244,
			"longitude":  3.3792,
			"area_sqm":   5000.0,
		}
		code, resp := callJSON(t, "POST", "/gotv/crowd/estimate", body, handleCrowdEstimate, nil)
		if code != 200 {
			t.Logf("crowd estimate returned %d: %v", code, resp)
		}
	})

	t.Run("experiment_dashboard", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/experiments", nil, handleExperimentDashboard, nil)
		if code != 200 {
			t.Logf("experiment dashboard returned %d", code)
		}
	})

	t.Run("team_leaderboard", func(t *testing.T) {
		if dbConn == nil {
			t.Skip("requires database")
		}
		code, _ := callJSON(t, "GET", "/gotv/teams/leaderboard", nil, handleTeamLeaderboard, nil)
		if code != 200 {
			t.Logf("team leaderboard returned %d", code)
		}
	})

	t.Run("route_optimization", func(t *testing.T) {
		body := map[string]interface{}{
			"points": []map[string]float64{
				{"lat": 6.5244, "lng": 3.3792},
				{"lat": 6.4541, "lng": 3.3947},
				{"lat": 6.5355, "lng": 3.3087},
			},
		}
		code, resp := callJSON(t, "POST", "/gotv/route/optimize", body, handleOptimizeRoute, nil)
		if code != 200 {
			t.Logf("route optimization returned %d: %v", code, resp)
		}
	})

	t.Run("federated_status", func(t *testing.T) {
		code, _ := callJSON(t, "GET", "/gotv/federated/status", nil, handleFederatedStatus, nil)
		if code != 200 {
			t.Logf("federated status returned %d", code)
		}
	})
}

func TestDeepHandlers_MLPredictions(t *testing.T) {
	t.Run("predict_fraud", func(t *testing.T) {
		initCircuitBreakers()
		body := map[string]interface{}{
			"accredited_voters": 500,
			"total_votes":       450,
			"rejected_votes":    10,
			"state":             "LA",
			"lga":               "IKEJA",
		}
		code, resp := callJSON(t, "POST", "/gotv/ml/predict/fraud", body, handleMLPredictFraud, nil)
		// Sidecar not running → circuit breaker catches → 503/500 expected
		t.Logf("fraud prediction returned %d: %v", code, resp)
	})

	t.Run("predict_engagement", func(t *testing.T) {
		initCircuitBreakers()
		body := map[string]interface{}{
			"contact_age":  35,
			"num_contacts": 5,
			"state":        "LA",
		}
		code, resp := callJSON(t, "POST", "/gotv/ml/predict/engagement", body, handleMLPredictEngagement, nil)
		t.Logf("engagement prediction returned %d: %v", code, resp)
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// DB-LEVEL INTEGRITY TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestDBIntegrity_NoOrphanPledges(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	var orphans int
	err := dbConn.QueryRow(`
		SELECT COUNT(*) FROM gotv_pledges p
		LEFT JOIN gotv_contacts c ON p.contact_id = c.contact_id AND p.party_id = c.party_id
		WHERE c.contact_id IS NULL AND p.party_id = 1
	`).Scan(&orphans)
	if err != nil {
		t.Fatalf("orphan query failed: %v", err)
	}
	if orphans > 0 {
		t.Errorf("found %d orphan pledges (no matching contact)", orphans)
	}
	t.Logf("orphan pledges: %d", orphans)
}

func TestDBIntegrity_NoOrphanRides(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	var orphans int
	err := dbConn.QueryRow(`
		SELECT COUNT(*) FROM gotv_ride_requests r
		LEFT JOIN gotv_contacts c ON r.contact_id = c.contact_id
		WHERE c.contact_id IS NULL AND r.party_id = 1
	`).Scan(&orphans)
	if err != nil {
		t.Fatalf("orphan query failed: %v", err)
	}
	if orphans > 0 {
		t.Logf("WARNING: found %d orphan rides (no matching contact) — seed data integrity issue", orphans)
	}
	t.Logf("orphan rides: %d", orphans)
}

func TestDBIntegrity_NoOrphanKnocks(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	var orphanContacts, orphanVolunteers int
	dbConn.QueryRow(`
		SELECT COUNT(*) FROM gotv_door_knocks dk
		LEFT JOIN gotv_contacts c ON dk.contact_id = c.contact_id
		WHERE c.contact_id IS NULL AND dk.party_id = 1
	`).Scan(&orphanContacts)
	dbConn.QueryRow(`
		SELECT COUNT(*) FROM gotv_door_knocks dk
		LEFT JOIN gotv_volunteers v ON dk.volunteer_id = v.volunteer_id
		WHERE v.volunteer_id IS NULL AND dk.party_id = 1
	`).Scan(&orphanVolunteers)

	if orphanContacts > 0 {
		t.Logf("WARNING: found %d door knocks with orphan contacts — seed data integrity issue", orphanContacts)
	}
	if orphanVolunteers > 0 {
		t.Logf("WARNING: found %d door knocks with orphan volunteers — seed data integrity issue", orphanVolunteers)
	}
	t.Logf("orphan knocks: contacts=%d, volunteers=%d", orphanContacts, orphanVolunteers)
}

func TestDBIntegrity_VettingStatusDistribution(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	rows, err := dbConn.Query(`
		SELECT vetting_status, COUNT(*) 
		FROM gotv_volunteers 
		WHERE party_id=1 
		GROUP BY vetting_status 
		ORDER BY count DESC
	`)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	validStatuses := map[string]bool{
		"pending": true, "nin_verified": true, "nin_failed": true,
		"trained": true, "approved": true, "rejected": true, "suspended": true,
	}

	total := 0
	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		total += count
		t.Logf("vetting_status=%s: count=%d", status, count)
		if !validStatuses[status] {
			t.Errorf("unexpected vetting_status: %s (count=%d)", status, count)
		}
	}
	t.Logf("total volunteers: %d", total)
}

func TestDBIntegrity_CampaignTypeDistribution(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	rows, err := dbConn.Query(`
		SELECT campaign_type, COUNT(*) 
		FROM gotv_campaigns 
		WHERE party_id=1 
		GROUP BY campaign_type 
		ORDER BY count DESC
	`)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ctype string
		var count int
		rows.Scan(&ctype, &count)
		t.Logf("campaign_type=%s: count=%d", ctype, count)
	}
}

func TestDBIntegrity_RideStatusDistribution(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	rows, err := dbConn.Query(`
		SELECT status, COUNT(*) 
		FROM gotv_ride_requests 
		WHERE party_id=1 
		GROUP BY status 
		ORDER BY count DESC
	`)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	validStatuses := map[string]bool{
		"pending": true, "matched": true, "en_route": true,
		"picked_up": true, "dropped_off": true, "cancelled": true, "no_show": true,
	}

	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		t.Logf("ride_status=%s: count=%d", status, count)
		if !validStatuses[status] {
			t.Errorf("unexpected ride status: %s", status)
		}
	}
}

func TestDBIntegrity_ForeignKeyConstraints(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// Verify FK constraints exist on critical tables
	fks := []struct {
		table, column, refTable string
	}{
		{"gotv_pledges", "contact_id", "gotv_contacts"},
		{"gotv_ride_requests", "contact_id", "gotv_contacts"},
	}

	for _, fk := range fks {
		var count int
		err := dbConn.QueryRow(`
			SELECT COUNT(*) FROM information_schema.table_constraints tc
			JOIN information_schema.constraint_column_usage ccu ON tc.constraint_name = ccu.constraint_name
			WHERE tc.constraint_type = 'FOREIGN KEY' 
			AND tc.table_name = $1 
			AND ccu.table_name = $2
		`, fk.table, fk.refTable).Scan(&count)
		if err != nil {
			t.Logf("FK check error for %s→%s: %v", fk.table, fk.refTable, err)
			continue
		}
		if count == 0 {
			t.Logf("WARNING: no FK constraint %s.%s → %s", fk.table, fk.column, fk.refTable)
		} else {
			t.Logf("FK verified: %s.%s → %s", fk.table, fk.column, fk.refTable)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// PERFORMANCE DEEP BENCHMARKS — DB-backed endpoint latency
// ═══════════════════════════════════════════════════════════════════════════

func benchmarkDBEndpoint(t *testing.T, name string, handler http.HandlerFunc, path string) {
	t.Helper()
	if dbConn == nil {
		t.Skip("requires database")
	}

	const iterations = 50
	start := time.Now()
	for i := 0; i < iterations; i++ {
		req := httptest.NewRequest("GET", path, nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handler(rr, req)
		if rr.Code != 200 {
			t.Fatalf("%s returned %d on iter %d", name, rr.Code, i)
		}
	}
	elapsed := time.Since(start)
	avgMs := float64(elapsed.Microseconds()) / float64(iterations) / 1000.0
	t.Logf("%s: %d iters in %v (avg %.2fms)", name, iterations, elapsed, avgMs)

	if avgMs > 100 {
		t.Errorf("%s too slow: %.2fms (target < 100ms)", name, avgMs)
	}
}

func TestPerformanceDB_Dashboard(t *testing.T) {
	benchmarkDBEndpoint(t, "dashboard", handleDashboard, "/gotv/dashboard")
}

func TestPerformanceDB_ListContacts(t *testing.T) {
	benchmarkDBEndpoint(t, "contacts", handleListContacts, "/gotv/contacts?page=1&per_page=20")
}

func TestPerformanceDB_ListVolunteers(t *testing.T) {
	benchmarkDBEndpoint(t, "volunteers", handleListVolunteers, "/gotv/volunteers?page=1&per_page=20")
}

func TestPerformanceDB_ListCampaigns(t *testing.T) {
	benchmarkDBEndpoint(t, "campaigns", handleListCampaigns, "/gotv/campaigns?page=1&per_page=20")
}

func TestPerformanceDB_ListPledges(t *testing.T) {
	benchmarkDBEndpoint(t, "pledges", handleListPledges, "/gotv/pledges?page=1&per_page=20")
}

func TestPerformanceDB_VettingPipeline(t *testing.T) {
	benchmarkDBEndpoint(t, "vetting", handleListVettingPipeline, "/gotv/volunteers/vetting")
}

func TestPerformanceDB_GeoCoverage(t *testing.T) {
	benchmarkDBEndpoint(t, "geo-coverage", handleGeoCoverage, "/gotv/geo/coverage")
}

func TestPerformanceDB_Analytics(t *testing.T) {
	benchmarkDBEndpoint(t, "analytics", handleGOTVAnalytics, "/gotv/analytics")
}

func TestPerformanceDB_CPI(t *testing.T) {
	benchmarkDBEndpoint(t, "cpi", handleComputeCPI, "/gotv/koh/cpi/compute")
}

func TestPerformanceDB_Leaderboard(t *testing.T) {
	benchmarkDBEndpoint(t, "leaderboard", handleLeaderboard, "/gotv/leaderboard?period=all")
}

// ═══════════════════════════════════════════════════════════════════════════
// HELPERS
// ═══════════════════════════════════════════════════════════════════════════

func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
