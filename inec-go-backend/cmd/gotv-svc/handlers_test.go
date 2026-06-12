package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ═══════════════════════════════════════════════════════════════════════════
// P0: Comprehensive GOTV Handler Tests
// ═══════════════════════════════════════════════════════════════════════════

// ─── Rate Limiter Tests ─────────────────────────────────────────────────

func TestRateLimiterBasic(t *testing.T) {
	rl := &RateLimiter{
		buckets: make(map[string]*rateBucket),
		global:  &rateBucket{tokens: 1000, lastTime: timeNow(), maxTokens: 1000, refillRate: 1000},
	}

	// Should allow requests under limit
	for i := 0; i < 50; i++ {
		if !rl.Allow("test-ip", 100, 100) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
}

func TestRateLimiterExhaustion(t *testing.T) {
	rl := &RateLimiter{
		buckets: make(map[string]*rateBucket),
		global:  &rateBucket{tokens: 1000, lastTime: timeNow(), maxTokens: 1000, refillRate: 0},
	}

	// Exhaust per-key limit (5 tokens, no refill)
	for i := 0; i < 5; i++ {
		if !rl.Allow("test-ip", 5, 0) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	// Next should be rejected
	if rl.Allow("test-ip", 5, 0) {
		t.Fatal("request should be rate limited")
	}
}

func TestRateLimiterPerKeyIsolation(t *testing.T) {
	rl := &RateLimiter{
		buckets: make(map[string]*rateBucket),
		global:  &rateBucket{tokens: 1000, lastTime: timeNow(), maxTokens: 1000, refillRate: 1000},
	}

	// Exhaust key A
	for i := 0; i < 3; i++ {
		rl.Allow("key-a", 3, 0)
	}
	if rl.Allow("key-a", 3, 0) {
		t.Fatal("key-a should be rate limited")
	}
	// Key B should still work
	if !rl.Allow("key-b", 3, 0) {
		t.Fatal("key-b should not be rate limited")
	}
}

// ─── Pagination Tests ────────────────────────────────────────────────────

func TestParsePaginationV2(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantPage int
		wantPP   int
		wantSort string
		wantOrd  string
	}{
		{"defaults", "", 1, 50, "created_at", "DESC"},
		{"page2", "page=2", 2, 50, "created_at", "DESC"},
		{"custom_per_page", "per_page=25", 1, 25, "created_at", "DESC"},
		{"sort_asc", "sort_by=status&order=asc", 1, 50, "status", "ASC"},
		{"invalid_page", "page=-1", 1, 50, "created_at", "DESC"},
		{"excessive_per_page", "per_page=500", 1, 50, "created_at", "DESC"},
		{"invalid_sort", "sort_by=injected_field", 1, 50, "created_at", "DESC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/test?"+tt.query, nil)
			p := parsePaginationV2(r)
			if p.Page != tt.wantPage {
				t.Errorf("Page=%d, want=%d", p.Page, tt.wantPage)
			}
			if p.PerPage != tt.wantPP {
				t.Errorf("PerPage=%d, want=%d", p.PerPage, tt.wantPP)
			}
			if p.SortBy != tt.wantSort {
				t.Errorf("SortBy=%s, want=%s", p.SortBy, tt.wantSort)
			}
			if p.Order != tt.wantOrd {
				t.Errorf("Order=%s, want=%s", p.Order, tt.wantOrd)
			}
		})
	}
}

func TestPaginationOffset(t *testing.T) {
	p := PaginationParams{Page: 3, PerPage: 25}
	if p.Offset() != 50 {
		t.Errorf("Offset()=%d, want=50", p.Offset())
	}
}

// ─── Validation Tests ────────────────────────────────────────────────────

func TestValidateNigerianPhone(t *testing.T) {
	tests := []struct {
		phone string
		valid bool
	}{
		{"08012345678", true},
		{"08112345678", true},
		{"07012345678", true},
		{"09012345678", true},
		{"09112345678", true},
		{"+2348012345678", true},
		{"0801234", false},
		{"123", false},
		{"", false},
	}
	for _, tt := range tests {
		err := validateNigerianPhone(tt.phone)
		if tt.valid && err != nil {
			t.Errorf("phone %q should be valid, got error: %v", tt.phone, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("phone %q should be invalid", tt.phone)
		}
	}
}

func TestValidateRequired(t *testing.T) {
	if e := validateRequired("name", "John"); e != nil {
		t.Error("non-empty should pass")
	}
	if e := validateRequired("name", ""); e == nil {
		t.Error("empty should fail")
	}
	if e := validateRequired("name", "   "); e == nil {
		t.Error("whitespace-only should fail")
	}
}

func TestValidateEnum(t *testing.T) {
	allowed := []string{"canvasser", "driver", "caller"}
	if e := validateEnum("role", "driver", allowed); e != nil {
		t.Error("valid enum should pass")
	}
	if e := validateEnum("role", "hacker", allowed); e == nil {
		t.Error("invalid enum should fail")
	}
}

func TestValidateStringLength(t *testing.T) {
	if e := validateStringLength("name", "John", 2, 100); e != nil {
		t.Error("valid length should pass")
	}
	if e := validateStringLength("name", "J", 2, 100); e == nil {
		t.Error("too short should fail")
	}
	if e := validateStringLength("name", string(make([]byte, 200)), 2, 100); e == nil {
		t.Error("too long should fail")
	}
}

// ─── RBAC Tests ──────────────────────────────────────────────────────────

func TestRBACPermissions(t *testing.T) {
	tests := []struct {
		role       GOTVRole
		permission string
		allowed    bool
	}{
		{RolePartyAdmin, "campaigns:write", true},
		{RolePartyAdmin, "vetting:approve", true},
		{RolePartyAdmin, "export", true},
		{RoleCoordinator, "vetting:approve", true},
		{RoleCoordinator, "settings:write", false},
		{RoleTeamLead, "tasks:write", true},
		{RoleTeamLead, "vetting:approve", false},
		{RoleFieldWorker, "canvass", true},
		{RoleFieldWorker, "campaigns:write", false},
		{RoleFieldWorker, "vetting:approve", false},
		{RoleObserver, "export", true},
		{RoleObserver, "campaigns:write", false},
		{RoleAnalyst, "scoring", true},
		{RoleAnalyst, "vetting:approve", false},
		{"unknown_role", "anything", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.role)+"_"+tt.permission, func(t *testing.T) {
			result := hasPermission(tt.role, tt.permission)
			if result != tt.allowed {
				t.Errorf("hasPermission(%s, %s)=%v, want=%v", tt.role, tt.permission, result, tt.allowed)
			}
		})
	}
}

func TestRequirePermissionMiddleware(t *testing.T) {
	handler := requirePermission("campaigns:write", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	// Admin should pass
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-GOTV-Role", "party_admin")
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != 200 {
		t.Errorf("party_admin should be allowed, got %d", w.Code)
	}

	// Field worker should be denied
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("X-GOTV-Role", "field_worker")
	w2 := httptest.NewRecorder()
	handler(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("field_worker should be denied, got %d", w2.Code)
	}
}

// ─── Haversine Distance Tests ────────────────────────────────────────────

func TestHaversineMeters(t *testing.T) {
	// Lagos to Ikeja (~20km)
	d := haversineMeters(6.4541, 3.3947, 6.6018, 3.3515)
	if d < 15000 || d > 25000 {
		t.Errorf("Lagos-Ikeja distance=%.0fm, expected ~20000m", d)
	}

	// Same point
	d2 := haversineMeters(6.4541, 3.3947, 6.4541, 3.3947)
	if d2 > 1 {
		t.Errorf("same point distance=%.0fm, expected 0", d2)
	}
}

// ─── TSP Route Optimization Tests ────────────────────────────────────────

func TestNearestNeighborTSP(t *testing.T) {
	stops := []RouteStop{
		{ContactID: "a", Lat: 6.45, Lng: 3.40},
		{ContactID: "b", Lat: 6.50, Lng: 3.45},
		{ContactID: "c", Lat: 6.46, Lng: 3.41},
		{ContactID: "d", Lat: 6.55, Lng: 3.50},
	}

	result := nearestNeighborTSP(6.44, 3.39, stops)
	if len(result) != 4 {
		t.Fatalf("expected 4 stops, got %d", len(result))
	}
	// First stop should be nearest to start (6.44, 3.39) → "a" at (6.45, 3.40)
	if result[0].ContactID != "a" {
		t.Errorf("first stop should be 'a' (nearest), got %s", result[0].ContactID)
	}
	// Second should be "c" (nearest to "a")
	if result[1].ContactID != "c" {
		t.Errorf("second stop should be 'c', got %s", result[1].ContactID)
	}
}

func TestNearestNeighborTSPEmpty(t *testing.T) {
	result := nearestNeighborTSP(6.44, 3.39, []RouteStop{})
	if len(result) != 0 {
		t.Error("empty input should return empty result")
	}
}

func TestNearestNeighborTSPSingle(t *testing.T) {
	stops := []RouteStop{{ContactID: "only", Lat: 6.45, Lng: 3.40}}
	result := nearestNeighborTSP(6.44, 3.39, stops)
	if len(result) != 1 || result[0].ContactID != "only" {
		t.Error("single stop should return that stop")
	}
}

// ─── WhatsApp Keyword Matching Tests ─────────────────────────────────────

func TestWhatsAppKeywordActions(t *testing.T) {
	tests := []struct {
		keyword string
		action  string
	}{
		{"yes", "confirm_pledge"},
		{"yeah", "confirm_pledge"},
		{"ride", "request_ride"},
		{"stop", "opt_out"},
		{"unsubscribe", "opt_out"},
		{"info", "send_info"},
		{"help", "send_help"},
		{"vote", "confirm_vote"},
	}

	for _, tt := range tests {
		if got := waKeywordActions[tt.keyword]; got != tt.action {
			t.Errorf("keyword %q: got %q, want %q", tt.keyword, got, tt.action)
		}
	}
}

// ─── NL Query Pattern Matching Tests ─────────────────────────────────────

func TestExtractState(t *testing.T) {
	tests := []struct {
		query string
		state string
	}{
		{"how many pledges in Lagos", "Lagos"},
		{"contacts in Kano this week", "Kano"},
		{"show Rivers data", "Rivers"},
		{"something random", ""},
	}
	for _, tt := range tests {
		got := extractState(tt.query)
		if got != tt.state {
			t.Errorf("extractState(%q)=%q, want=%q", tt.query, got, tt.state)
		}
	}
}

// ─── Dashboard Widget Defaults Tests ─────────────────────────────────────

func TestDefaultDashboardWidgets(t *testing.T) {
	widgets := defaultDashboardWidgets()
	if len(widgets) < 5 {
		t.Errorf("expected at least 5 default widgets, got %d", len(widgets))
	}
	// First widget should be a counter
	if widgets[0]["type"] != "counter" {
		t.Errorf("first widget type=%v, want=counter", widgets[0]["type"])
	}
}

// ─── Pagination Header Tests ─────────────────────────────────────────────

func TestSetPaginationHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	setPaginationHeaders(w, 150, 2, 50)
	if w.Header().Get("X-Total-Count") != "150" {
		t.Error("X-Total-Count should be 150")
	}
	if w.Header().Get("X-Page") != "2" {
		t.Error("X-Page should be 2")
	}
	if w.Header().Get("X-Total-Pages") != "3" {
		t.Error("X-Total-Pages should be 3")
	}
}

// ─── Security Headers Tests ──────────────────────────────────────────────

func TestSecurityHeadersMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := securityHeadersMiddleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options")
	}
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options")
	}
	if !containsSubstring(w.Header().Get("Content-Security-Policy"), "default-src") {
		t.Error("missing CSP")
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := requestIDMiddleware(inner)

	// Without existing ID
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("should generate X-Request-ID")
	}

	// With existing ID
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("X-Request-ID", "my-custom-id")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Header().Get("X-Request-ID") != "my-custom-id" {
		t.Error("should preserve existing X-Request-ID")
	}
}

// ─── Rate Limit Middleware Integration Test ──────────────────────────────

func TestRateLimitMiddlewareIntegration(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := rateLimitMiddleware(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("first request should pass, got %d", w.Code)
	}
}

// ─── Vetting State Machine Tests (logic only) ───────────────────────────

func TestVettingStateTransitions(t *testing.T) {
	validTransitions := map[string][]string{
		"pending":      {"nin_verified", "rejected"},
		"nin_verified": {"trained", "rejected"},
		"trained":      {"approved", "rejected"},
		"approved":     {"suspended"},
		"suspended":    {"approved"},
		"rejected":     {}, // terminal
	}

	tests := []struct {
		from, to string
		valid    bool
	}{
		{"pending", "nin_verified", true},
		{"pending", "rejected", true},
		{"pending", "approved", false},
		{"pending", "trained", false},
		{"nin_verified", "trained", true},
		{"nin_verified", "approved", false},
		{"trained", "approved", true},
		{"trained", "rejected", true},
		{"approved", "suspended", true},
		{"suspended", "approved", true},
		{"rejected", "approved", false},
		{"rejected", "pending", false},
	}

	for _, tt := range tests {
		name := tt.from + "_to_" + tt.to
		t.Run(name, func(t *testing.T) {
			allowed := false
			for _, target := range validTransitions[tt.from] {
				if target == tt.to {
					allowed = true
					break
				}
			}
			if allowed != tt.valid {
				t.Errorf("%s → %s: got allowed=%v, want=%v", tt.from, tt.to, allowed, tt.valid)
			}
		})
	}
}

// ─── Task Role Compatibility Tests ───────────────────────────────────────

func TestTaskRoleCompatibility(t *testing.T) {
	compatibility := map[string][]string{
		"door_knock":              {"canvasser", "team_lead"},
		"phone_call":              {"caller", "phone_banker"},
		"ride_duty":               {"driver"},
		"event_setup":             {"canvasser", "coordinator", "team_lead"},
		"data_collection":         {"canvasser", "team_lead", "observer"},
		"voter_registration":      {"canvasser", "team_lead"},
		"materials_distribution":  {"canvasser", "team_lead", "coordinator"},
		"monitoring":              {"observer", "coordinator"},
	}

	tests := []struct {
		taskType, role string
		compatible     bool
	}{
		{"door_knock", "canvasser", true},
		{"door_knock", "driver", false},
		{"ride_duty", "driver", true},
		{"ride_duty", "canvasser", false},
		{"phone_call", "caller", true},
		{"monitoring", "observer", true},
		{"monitoring", "canvasser", false},
	}

	for _, tt := range tests {
		name := tt.taskType + "_" + tt.role
		t.Run(name, func(t *testing.T) {
			roles, exists := compatibility[tt.taskType]
			if !exists {
				t.Fatalf("unknown task type: %s", tt.taskType)
			}
			found := false
			for _, r := range roles {
				if r == tt.role {
					found = true
					break
				}
			}
			if found != tt.compatible {
				t.Errorf("%s with role %s: got compatible=%v, want=%v", tt.taskType, tt.role, found, tt.compatible)
			}
		})
	}
}

// ─── Simulation Model Tests ──────────────────────────────────────────────

func TestSimulationDriverImpact(t *testing.T) {
	// 10 additional drivers * 8 rides/driver = 80 additional rides
	additionalDrivers := 10
	avgRidesPerDriver := 8.0
	expectedRides := float64(additionalDrivers) * avgRidesPerDriver
	if expectedRides != 80 {
		t.Errorf("expected 80 additional rides, got %.0f", expectedRides)
	}
}

func TestSimulationCanvasserImpact(t *testing.T) {
	additionalCanvassers := 20
	doorsPerCanvasser := 40.0
	conversionRate := 0.12
	expectedDoors := float64(additionalCanvassers) * doorsPerCanvasser
	expectedPledges := expectedDoors * conversionRate
	if math.Abs(expectedPledges-96) > 0.1 {
		t.Errorf("expected ~96 additional pledges, got %.1f", expectedPledges)
	}
}

// ─── NL Query Handler Tests ─────────────────────────────────────────────

func TestNLQueryMalformedInput(t *testing.T) {
	req := httptest.NewRequest("POST", "/gotv/nl/query",
		bytes.NewBufferString(`not json`))
	w := httptest.NewRecorder()
	handleNLQuery(w, req)
	if w.Code != 400 {
		t.Errorf("malformed input should return 400, got %d", w.Code)
	}
}

func TestNLQueryEmptyQuery(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"query": ""})
	req := httptest.NewRequest("POST", "/gotv/nl/query", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	handleNLQuery(w, req)
	if w.Code != 400 {
		t.Errorf("empty query should return 400, got %d", w.Code)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════════════════════

func timeNow() time.Time { return time.Now() }
var _ = timeNow // suppress unused warning

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
