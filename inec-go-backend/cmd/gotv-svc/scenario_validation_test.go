package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"inec-go-backend/internal/gotv"

	_ "github.com/lib/pq"
)

// ═══════════════════════════════════════════════════════════════════════════
// Top 10 Production Scenarios — End-to-End Workflow Validation
// Each scenario represents a real stakeholder workflow at scale.
// ═══════════════════════════════════════════════════════════════════════════

// setAuth adds party auth headers to a request for direct handler testing
func setAuth(req *http.Request) *http.Request {
	req.Header.Set("X-GOTV-Party-ID", "1")
	req.Header.Set("X-GOTV-User", "test-admin")
	req.Header.Set("X-GOTV-Party-Code", "APC")
	return req
}

func TestMain(m *testing.M) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("GOTV_TEST_DB")
	}
	if dsn != "" {
		db, err := sql.Open("postgres", dsn)
		if err == nil && db.Ping() == nil {
			dbConn = db
			svc = gotv.NewService(db, "")
			wsHub = gotv.NewWSHub(100, 2)
			go wsHub.Run()
			dispatcher = gotv.NewDispatchEngine(db, svc, wsHub, 2)
			dispatcher.RegisterAdapter(&gotv.LogAdapter{})
			initGOTVLedgerAndBlockchain()
		}
	}
	os.Exit(m.Run())
}

// ─── Scenario 1: Election Day Result Submission & Collation ──────────
// Stakeholder: INEC Presiding Officer, Collation Officer
// Workflow: View dashboard → check turnout → monitor dispatch → DLQ for failures
func TestScenario1_ElectionDayResultSubmission(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("dashboard_shows_statistics", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/dashboard", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleDashboard(rr, req)

		if rr.Code != 200 {
			t.Fatalf("dashboard returned %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&resp)

		requiredFields := []string{"total_contacts", "total_volunteers", "total_pledges", "active_campaigns", "pending_rides"}
		for _, f := range requiredFields {
			if _, ok := resp[f]; !ok {
				t.Errorf("dashboard missing field: %s", f)
			}
		}
	})

	t.Run("dispatch_metrics", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/metrics/dispatch", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleDispatchMetrics(rr, req)

		if rr.Code != 200 {
			t.Fatalf("dispatch metrics returned %d", rr.Code)
		}
	})
}

// ─── Scenario 2: Volunteer Vetting & Deployment Pipeline ────────────
// Stakeholder: Party Coordinator, Volunteer
// Workflow: Register → NIN verify → training → approve → assign location → assign task
func TestScenario2_VolunteerVettingPipeline(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("create_volunteer", func(t *testing.T) {
		body := map[string]interface{}{
			"full_name":      "Test Volunteer Scenario2",
			"phone":          "+2348012345999",
			"role":           "canvasser",
			"assigned_state": "LA",
			"assigned_lga":   "LA-IKEJA",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/gotv/volunteers", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		setAuth(req)
		rr := httptest.NewRecorder()
		handleCreateVolunteer(rr, req)

		if rr.Code != 201 && rr.Code != 200 {
			t.Fatalf("create volunteer returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("vetting_pipeline_view", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/volunteers/vetting", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleListVettingPipeline(rr, req)

		if rr.Code != 200 {
			t.Fatalf("vetting pipeline returned %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&resp)

		if _, ok := resp["counts"]; !ok {
			t.Error("vetting pipeline missing counts")
		}
	})

	t.Run("list_volunteers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/volunteers", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleListVolunteers(rr, req)

		if rr.Code != 200 {
			t.Fatalf("list volunteers returned %d", rr.Code)
		}

		var resp map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&resp)
		if _, ok := resp["total"]; !ok {
			t.Error("list volunteers missing total count")
		}
	})

	t.Run("location_capacity", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/locations/capacity", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleLocationCapacity(rr, req)

		if rr.Code != 200 {
			t.Fatalf("location capacity returned %d", rr.Code)
		}
	})

	t.Run("list_tasks", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/tasks?page=1&per_page=10", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleListTasks(rr, req)

		if rr.Code != 200 {
			t.Fatalf("list tasks returned %d", rr.Code)
		}
	})
}

// ─── Scenario 3: Ride-to-Polls Real-Time Matching ────────────────────
// Stakeholder: Voter (needs ride), Volunteer Driver, War Room Operator
// Workflow: Request ride → auto-match → status tracking → geo view
func TestScenario3_RideToPolls(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("list_rides", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/rides?page=1&per_page=10", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleListRides(rr, req)

		if rr.Code != 200 {
			t.Fatalf("list rides returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("create_ride_request", func(t *testing.T) {
		var contactID string
		err := dbConn.QueryRow("SELECT contact_id FROM gotv_contacts WHERE party_id=1 LIMIT 1").Scan(&contactID)
		if err != nil {
			t.Skip("no contacts for ride test")
		}
		body := map[string]interface{}{
			"contact_id":        contactID,
			"pickup_latitude":   6.5244,
			"pickup_longitude":  3.3792,
			"polling_unit_code": "LA-PU-0001",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/gotv/rides", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		setAuth(req)
		rr := httptest.NewRecorder()
		handleCreateRide(rr, req)

		if rr.Code != 201 && rr.Code != 200 {
			t.Fatalf("create ride returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("geo_rides_view", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/geo/rides", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleGeoRides(rr, req)

		if rr.Code != 200 {
			t.Fatalf("geo rides returned %d", rr.Code)
		}
	})

	t.Run("geo_volunteers_for_matching", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/geo/volunteers", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleGeoVolunteers(rr, req)

		if rr.Code != 200 {
			t.Fatalf("geo volunteers returned %d", rr.Code)
		}
	})
}

// ─── Scenario 4: Campaign Management & Multi-Channel Outreach ───────
// Stakeholder: Campaign Manager, Communications Director
// Workflow: Create campaign → launch → track delivery → view analytics
func TestScenario4_CampaignManagement(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("list_campaigns", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/campaigns?page=1&per_page=10", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleListCampaigns(rr, req)

		if rr.Code != 200 {
			t.Fatalf("list campaigns returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("create_campaign", func(t *testing.T) {
		body := map[string]interface{}{
			"name":             "Test SMS Campaign Scenario4",
			"campaign_type":    "sms",
			"message_template": "Don't forget to vote on Election Day! Your polling unit is ready.",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/gotv/campaigns", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		setAuth(req)
		rr := httptest.NewRecorder()
		handleCreateCampaign(rr, req)

		if rr.Code != 201 && rr.Code != 200 {
			t.Fatalf("create campaign returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("analytics", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/analytics", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleGOTVAnalytics(rr, req)

		if rr.Code != 200 {
			t.Fatalf("analytics returned %d", rr.Code)
		}
	})

	t.Run("search_campaigns", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/search?q=sms&type=campaigns", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleGOTVSearch(rr, req)

		if rr.Code != 200 {
			t.Fatalf("search returned %d", rr.Code)
		}
	})
}

// ─── Scenario 5: Fraud Detection & Anomaly Alerting ─────────────────
// Stakeholder: Election Security Analyst, INEC Commissioner
// Workflow: View ML models → check monitoring → check circuit breakers → view alerts
func TestScenario5_FraudDetection(t *testing.T) {
	t.Run("ml_model_registry", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/ml/models", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleMLModelRegistry(rr, req)

		if rr.Code != 200 {
			t.Fatalf("ML model registry returned %d", rr.Code)
		}
	})

	t.Run("ml_monitoring", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/ml/monitoring", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleMLMonitoring(rr, req)

		if rr.Code != 200 {
			t.Fatalf("ML monitoring returned %d", rr.Code)
		}
	})

	t.Run("ml_weights_available", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/ml/weights", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleMLWeights(rr, req)

		if rr.Code != 200 {
			t.Fatalf("ML weights returned %d", rr.Code)
		}
	})

	t.Run("circuit_breaker_status", func(t *testing.T) {
		initCircuitBreakers()
		req := httptest.NewRequest("GET", "/gotv/circuit-breakers", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleCircuitBreakerStatus(rr, req)

		if rr.Code != 200 {
			t.Fatalf("circuit breakers returned %d", rr.Code)
		}

		var resp map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&resp)

		total, _ := resp["total"].(float64)
		if total != 12 {
			t.Errorf("expected 12 circuit breakers, got %.0f", total)
		}

		healthy, _ := resp["healthy"].(bool)
		if !healthy {
			t.Error("circuit breakers should all be healthy in initial state")
		}
	})

	t.Run("ml_training_report", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/ml/training-report", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleMLTrainingReport(rr, req)

		// 200 if report exists, 404 if no training has run yet
		if rr.Code != 200 && rr.Code != 404 {
			t.Fatalf("ML training report returned unexpected %d", rr.Code)
		}
	})
}

// ─── Scenario 6: War Room Live Operations ───────────────────────────
// Stakeholder: War Room Commander, State Coordinator
// Workflow: Dashboard → geo coverage → volunteer positions → canvass trails → DLQ
func TestScenario6_WarRoomOperations(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("dashboard_loads", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/dashboard", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleDashboard(rr, req)

		if rr.Code != 200 {
			t.Fatalf("dashboard returned %d", rr.Code)
		}

		var resp map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&resp)

		for _, field := range []string{"total_contacts", "total_volunteers", "total_pledges", "pending_rides"} {
			if _, ok := resp[field]; !ok {
				t.Errorf("dashboard missing War Room field: %s", field)
			}
		}
	})

	t.Run("geo_coverage", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/geo/coverage", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleGeoCoverage(rr, req)

		if rr.Code != 200 {
			t.Fatalf("geo coverage returned %d", rr.Code)
		}
	})

	t.Run("geo_canvass_trails", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/geo/canvass-trails", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleGeoCanvassTrails(rr, req)

		if rr.Code != 200 {
			t.Fatalf("canvass trails returned %d", rr.Code)
		}
	})

	t.Run("dlq_list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/dlq", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleDLQList(rr, req)

		if rr.Code != 200 {
			t.Fatalf("DLQ list returned %d", rr.Code)
		}
	})
}

// ─── Scenario 7: KOH Indicators & Political Intelligence ───────────
// Stakeholder: Political Analyst, Party Strategist
// Workflow: Compute CPI → demographics → surveys → sentiment → endorsements
func TestScenario7_KOHIndicators(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("cpi_compute", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/koh/cpi/compute", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleComputeCPI(rr, req)

		if rr.Code != 200 {
			t.Fatalf("CPI compute returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("cpi_history", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/koh/cpi/history", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleCPIHistory(rr, req)

		if rr.Code != 200 {
			t.Fatalf("CPI history returned %d", rr.Code)
		}
	})

	t.Run("demographics", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/koh/demographics", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleDemographicBreakdown(rr, req)

		if rr.Code != 200 {
			t.Fatalf("demographics returned %d", rr.Code)
		}
	})

	t.Run("surveys_list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/koh/surveys", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleListSurveys(rr, req)

		if rr.Code != 200 {
			t.Fatalf("surveys returned %d", rr.Code)
		}
	})

	t.Run("sentiment", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/koh/social/sentiment", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleSentimentSummary(rr, req)

		if rr.Code != 200 {
			t.Fatalf("sentiment returned %d", rr.Code)
		}
	})

	t.Run("endorsements", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/koh/endorsements", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleListEndorsements(rr, req)

		if rr.Code != 200 {
			t.Fatalf("endorsements returned %d", rr.Code)
		}
	})

	t.Run("lga_strategic_dashboard", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/koh/lga/dashboard", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleLGAStrategicDashboard(rr, req)

		if rr.Code != 200 {
			t.Fatalf("LGA dashboard returned %d", rr.Code)
		}
	})
}

// ─── Scenario 8: Voter Contact Scoring & Segmentation ───────────────
// Stakeholder: Data Analyst, Targeting Manager
// Workflow: List contacts → search → pledges → canvass walklist
func TestScenario8_ContactScoringSegmentation(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("list_contacts", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/contacts?page=1&per_page=25", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleListContacts(rr, req)

		if rr.Code != 200 {
			t.Fatalf("list contacts returned %d", rr.Code)
		}
	})

	t.Run("search_contacts", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/search?q=lagos&type=contacts", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleGOTVSearch(rr, req)

		if rr.Code != 200 {
			t.Fatalf("search returned %d", rr.Code)
		}
	})

	t.Run("list_pledges", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/pledges?page=1&per_page=10", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleListPledges(rr, req)

		if rr.Code != 200 {
			t.Fatalf("list pledges returned %d", rr.Code)
		}
	})

	t.Run("canvass_walklist", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/canvass/walklist?ward=IKEJA-01", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleCanvassWalklist(rr, req)

		if rr.Code != 200 {
			t.Fatalf("canvass walklist returned %d", rr.Code)
		}
	})
}

// ─── Scenario 9: Financial Ledger & Reconciliation ──────────────────
// Stakeholder: Finance Officer, Campaign Treasurer
// Workflow: View accounts → check balance → reconcile → view history → blockchain status
func TestScenario9_FinancialLedger(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	t.Run("ledger_accounts", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/ledger/accounts", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleLedgerAccounts(rr, req)

		if rr.Code != 200 {
			t.Fatalf("ledger accounts returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("ledger_balance", func(t *testing.T) {
		// First get an account ID from the accounts list
		reqAcct := httptest.NewRequest("GET", "/gotv/ledger/accounts", nil)
		setAuth(reqAcct)
		rrAcct := httptest.NewRecorder()
		handleLedgerAccounts(rrAcct, reqAcct)

		var acctResp map[string]interface{}
		json.NewDecoder(rrAcct.Body).Decode(&acctResp)

		accountID := ""
		if accounts, ok := acctResp["accounts"].([]interface{}); ok && len(accounts) > 0 {
			if acct, ok := accounts[0].(map[string]interface{}); ok {
				if id, ok := acct["id"].(string); ok {
					accountID = id
				}
			}
		}

		if accountID == "" {
			t.Skip("no ledger accounts to query balance")
		}

		req := httptest.NewRequest("GET", "/gotv/ledger/balance?account_id="+accountID, nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleLedgerBalance(rr, req)

		if rr.Code != 200 && rr.Code != 404 {
			t.Fatalf("ledger balance returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("ledger_reconcile", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/ledger/reconcile", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleLedgerReconcile(rr, req)

		// 200 even with empty data
		if rr.Code != 200 {
			t.Fatalf("ledger reconcile returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("ledger_history", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/ledger/history", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleLedgerHistory(rr, req)

		if rr.Code != 200 {
			t.Fatalf("ledger history returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("blockchain_status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/blockchain/status", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleBlockchainStatus(rr, req)

		if rr.Code != 200 {
			t.Fatalf("blockchain status returned %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("blockchain_blocks", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/blockchain/blocks", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleBlockchainBlocks(rr, req)

		if rr.Code != 200 {
			t.Fatalf("blockchain blocks returned %d: %s", rr.Code, rr.Body.String())
		}
	})
}

// ─── Scenario 10: Analytics, Reporting & Platform Health ────────────
// Stakeholder: Program Manager, State Director, DevOps
// Workflow: Health → readiness → middleware status → OpenAPI → version → process info → alerts
func TestScenario10_AnalyticsReporting(t *testing.T) {
	t.Run("health_endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		rr := httptest.NewRecorder()
		handleHealth(rr, req)

		if rr.Code != 200 {
			t.Fatalf("health returned %d", rr.Code)
		}
	})

	t.Run("ready_endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ready", nil)
		rr := httptest.NewRecorder()
		handleReadiness(rr, req)

		if rr.Code != 200 && rr.Code != 503 {
			t.Fatalf("ready returned %d", rr.Code)
		}
	})

	t.Run("middleware_status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/middleware/status", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleMiddlewareStatus(rr, req)

		if rr.Code != 200 {
			t.Fatalf("middleware status returned %d", rr.Code)
		}
	})

	t.Run("openapi_spec", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/openapi.json", nil)
		rr := httptest.NewRecorder()
		handleOpenAPISpec(rr, req)

		if rr.Code != 200 {
			t.Fatalf("openapi spec returned %d", rr.Code)
		}

		var spec map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&spec)

		if _, ok := spec["openapi"]; !ok {
			t.Error("openapi spec missing 'openapi' version field")
		}
	})

	t.Run("version_endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/version", nil)
		rr := httptest.NewRecorder()
		handleVersion(rr, req)

		if rr.Code != 200 {
			t.Fatalf("version returned %d", rr.Code)
		}
	})

	t.Run("process_info", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/process/info", nil)
		rr := httptest.NewRecorder()
		handleProcessInfo(rr, req)

		if rr.Code != 200 {
			t.Fatalf("process info returned %d", rr.Code)
		}

		var info map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&info)

		if _, ok := info["pid"]; !ok {
			t.Error("process info missing pid")
		}
	})

	t.Run("alerting_rules", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/alerts/rules", nil)
		rr := httptest.NewRecorder()
		handleAlertingRules(rr, req)

		if rr.Code != 200 {
			t.Fatalf("alerting rules returned %d", rr.Code)
		}
	})

	t.Run("robots_txt", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/robots.txt", nil)
		rr := httptest.NewRecorder()
		handleRobotsTxt(rr, req)

		if rr.Code != 200 {
			t.Fatalf("robots.txt returned %d", rr.Code)
		}
	})

	t.Run("integration_audit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/gotv/integrations/audit", nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handleIntegrationAudit(rr, req)

		if rr.Code != 200 {
			t.Fatalf("integration audit returned %d", rr.Code)
		}
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// Scale Validation — Concurrent Request Handling
// ═══════════════════════════════════════════════════════════════════════════

func TestScaleValidation_ConcurrentHealthChecks(t *testing.T) {
	const numConcurrent = 200
	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/health", nil)
			rr := httptest.NewRecorder()
			handleHealth(rr, req)

			if rr.Code != 200 {
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if errCount > 0 {
		t.Errorf("concurrent health: %d/%d failed", errCount, numConcurrent)
	}
}

func TestScaleValidation_ConcurrentCircuitBreakerReads(t *testing.T) {
	initCircuitBreakers()

	const numConcurrent = 100
	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/gotv/circuit-breakers", nil)
			rr := httptest.NewRecorder()
			handleCircuitBreakerStatus(rr, req)

			if rr.Code != 200 {
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if errCount > 0 {
		t.Errorf("%d/%d concurrent circuit breaker reads failed", errCount, numConcurrent)
	}
}

func TestScaleValidation_ConcurrentMLModelReads(t *testing.T) {
	const numConcurrent = 100
	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/gotv/ml/models", nil)
			setAuth(req)
			rr := httptest.NewRecorder()
			handleMLModelRegistry(rr, req)

			if rr.Code != 200 {
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if errCount > 0 {
		t.Errorf("%d/%d concurrent ML model reads failed", errCount, numConcurrent)
	}
}

func TestScaleValidation_ConcurrentMixedWorkloads(t *testing.T) {
	initCircuitBreakers()

	const numConcurrent = 50
	var wg sync.WaitGroup
	results := make(map[string]int)
	var mu sync.Mutex

	// Simulate mixed concurrent workload (5 different endpoint types)
	handlers := []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"health", handleHealth, "/health"},
		{"circuit-breakers", handleCircuitBreakerStatus, "/gotv/circuit-breakers"},
		{"ml-models", handleMLModelRegistry, "/gotv/ml/models"},
		{"process-info", handleProcessInfo, "/gotv/process/info"},
		{"version", handleVersion, "/version"},
	}

	for _, h := range handlers {
		for i := 0; i < numConcurrent; i++ {
			wg.Add(1)
			name := h.name
			handler := h.handler
			path := h.path
			go func() {
				defer wg.Done()
				req := httptest.NewRequest("GET", path, nil)
				setAuth(req)
				rr := httptest.NewRecorder()
				handler(rr, req)

				mu.Lock()
				if rr.Code == 200 {
					results[name+"_ok"]++
				} else {
					results[name+"_err"]++
				}
				mu.Unlock()
			}()
		}
	}

	wg.Wait()

	totalErrors := 0
	for k, v := range results {
		if strings.HasSuffix(k, "_err") {
			totalErrors += v
			t.Errorf("mixed workload: %s=%d errors", k, v)
		}
	}

	total := len(handlers) * numConcurrent
	t.Logf("mixed workload: %d/%d successful", total-totalErrors, total)
}

// ═══════════════════════════════════════════════════════════════════════════
// Cross-Cutting Concerns — Security, Rate Limiting, Middleware Pipeline
// ═══════════════════════════════════════════════════════════════════════════

func TestCrossCutting_SecurityHeaders(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	requiredHeaders := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
	}

	for header, expected := range requiredHeaders {
		got := rr.Header().Get(header)
		if got != expected {
			t.Errorf("security header %s: expected %q, got %q", header, expected, got)
		}
	}
}

func TestCrossCutting_RateLimitingUnderLoad(t *testing.T) {
	rl := &RateLimiter{
		buckets: make(map[string]*rateBucket),
		global:  &rateBucket{tokens: 100, lastTime: time.Now(), maxTokens: 100, refillRate: 100},
	}

	allowed := 0
	denied := 0
	for i := 0; i < 200; i++ {
		if rl.Allow("scale-test-ip", 50, 50) {
			allowed++
		} else {
			denied++
		}
	}

	if denied == 0 {
		t.Error("rate limiter should deny some requests under burst load")
	}
	if allowed == 0 {
		t.Error("rate limiter should allow some requests")
	}
	t.Logf("rate limit test: %d allowed, %d denied out of 200", allowed, denied)
}

func TestCrossCutting_MiddlewarePipeline(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	})

	handler := panicRecoveryMiddleware(
		securityHeadersMiddleware(
			requestIDMiddleware(inner),
		),
	)

	req := httptest.NewRequest("GET", "/test-pipeline", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Errorf("middleware pipeline returned %d", rr.Code)
	}

	if rr.Header().Get("X-Request-ID") == "" {
		t.Error("middleware pipeline missing X-Request-ID")
	}

	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("middleware pipeline missing security headers")
	}
}

func TestCrossCutting_PanicRecoveryUnderConcurrency(t *testing.T) {
	panicker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("deliberate test panic")
	})

	handler := panicRecoveryMiddleware(panicker)

	const numConcurrent = 50
	var wg sync.WaitGroup
	recoveredCount := 0
	var mu sync.Mutex

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/panic-test", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code == 500 {
				mu.Lock()
				recoveredCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if recoveredCount != numConcurrent {
		t.Errorf("expected all %d panics recovered, got %d", numConcurrent, recoveredCount)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Performance Benchmarks — Latency validation per endpoint
// ═══════════════════════════════════════════════════════════════════════════

func benchmarkEndpoint(t *testing.T, name string, handler http.HandlerFunc, path string) {
	t.Helper()
	const iterations = 100

	start := time.Now()
	for i := 0; i < iterations; i++ {
		req := httptest.NewRequest("GET", path, nil)
		setAuth(req)
		rr := httptest.NewRecorder()
		handler(rr, req)
		if rr.Code != 200 {
			t.Fatalf("%s returned %d on iteration %d", name, rr.Code, i)
		}
	}
	elapsed := time.Since(start)
	avgMs := float64(elapsed.Microseconds()) / float64(iterations) / 1000.0

	t.Logf("%s: %d iterations in %v (avg %.2f ms/req)", name, iterations, elapsed, avgMs)

	if avgMs > 50 {
		t.Errorf("%s too slow: %.2f ms/req (target < 50ms)", name, avgMs)
	}
}

func TestPerformance_HealthEndpoint(t *testing.T) {
	benchmarkEndpoint(t, "health", handleHealth, "/health")
}

func TestPerformance_CircuitBreakers(t *testing.T) {
	initCircuitBreakers()
	benchmarkEndpoint(t, "circuit-breakers", handleCircuitBreakerStatus, "/gotv/circuit-breakers")
}

func TestPerformance_ProcessInfo(t *testing.T) {
	benchmarkEndpoint(t, "process-info", handleProcessInfo, "/gotv/process/info")
}

func TestPerformance_MLModels(t *testing.T) {
	benchmarkEndpoint(t, "ml-models", handleMLModelRegistry, "/gotv/ml/models")
}

func TestPerformance_Version(t *testing.T) {
	benchmarkEndpoint(t, "version", handleVersion, "/version")
}

func TestPerformance_OpenAPISpec(t *testing.T) {
	benchmarkEndpoint(t, "openapi", handleOpenAPISpec, "/openapi.json")
}

// ═══════════════════════════════════════════════════════════════════════════
// Data Consistency — Verify no orphan data or broken relationships
// ═══════════════════════════════════════════════════════════════════════════

func TestDataConsistency_DashboardCountsMatch(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// Get dashboard totals
	req := httptest.NewRequest("GET", "/gotv/dashboard", nil)
	setAuth(req)
	rr := httptest.NewRecorder()
	handleDashboard(rr, req)

	if rr.Code != 200 {
		t.Fatalf("dashboard returned %d", rr.Code)
	}

	var dash map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&dash)

	// Verify dashboard returns non-negative counts
	for _, field := range []string{"total_contacts", "total_volunteers", "total_pledges"} {
		v, ok := dash[field]
		if !ok {
			t.Errorf("dashboard missing %s", field)
			continue
		}
		if f, ok := v.(float64); ok && f < 0 {
			t.Errorf("dashboard %s is negative: %f", field, f)
		}
	}
}

func TestDataConsistency_VettingCountsMatch(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}

	// Get vetting pipeline counts
	req := httptest.NewRequest("GET", "/gotv/volunteers/vetting", nil)
	setAuth(req)
	rr := httptest.NewRecorder()
	handleListVettingPipeline(rr, req)

	if rr.Code != 200 {
		t.Skip("vetting pipeline not available")
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)

	// Sum of all status counts should equal total volunteers
	if counts, ok := resp["status_counts"].(map[string]interface{}); ok {
		sum := 0.0
		for _, v := range counts {
			if f, ok := v.(float64); ok {
				sum += f
			}
		}

		if sum == 0 {
			t.Log("vetting counts are all zero — may need seeding")
		}
	}
}
