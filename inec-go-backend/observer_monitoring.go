package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// ── Party Observer Monitoring System ──
// Enables party agents to monitor election results in real-time from every voting location.

const (
	TopicObserverCheckIn = "inec.observer.checkin"
	TopicObserverReport  = "inec.observer.report"
	TopicObserverAlert   = "inec.observer.alert"
)

// SSE (Server-Sent Events) hub for pushing real-time result updates to connected observers
const (
	MaxGlobalSSEConnections = 10000 // Global limit across all users
	MaxPerUserSSEConnections = 5    // Per-user limit to prevent abuse
)

type SSEHub struct {
	mu          sync.RWMutex
	subscribers map[string]*SSESubscriber
}

type SSESubscriber struct {
	ID        string
	UserID    int
	PartyCode string // Filter by party (empty = all parties)
	StateCode string // Filter by state (empty = all states)
	LGACode   string // Filter by LGA (empty = all LGAs)
	Channel   chan SSEEvent
	CreatedAt time.Time
}

type SSEEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

var sseHub = &SSEHub{
	subscribers: make(map[string]*SSESubscriber),
}

// subscribe adds a subscriber if connection limits allow. Returns false if rejected.
func (h *SSEHub) subscribe(sub *SSESubscriber) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check global limit
	if len(h.subscribers) >= MaxGlobalSSEConnections {
		log.Warn().Int("global_count", len(h.subscribers)).Msg("SSE global connection limit reached")
		return false
	}

	// Check per-user limit
	userCount := 0
	for _, s := range h.subscribers {
		if s.UserID == sub.UserID {
			userCount++
		}
	}
	if userCount >= MaxPerUserSSEConnections {
		log.Warn().Int("user_id", sub.UserID).Int("user_count", userCount).Msg("SSE per-user connection limit reached")
		return false
	}

	h.subscribers[sub.ID] = sub
	return true
}

// connectionCount returns global and per-user counts.
func (h *SSEHub) connectionCount(userID int) (global int, perUser int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	global = len(h.subscribers)
	for _, s := range h.subscribers {
		if s.UserID == userID {
			perUser++
		}
	}
	return
}

func (h *SSEHub) unsubscribe(id string) {
	h.mu.Lock()
	if sub, ok := h.subscribers[id]; ok {
		close(sub.Channel)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()
}

func (h *SSEHub) broadcast(event SSEEvent, stateCode, lgaCode, partyCode string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, sub := range h.subscribers {
		// Apply filters — subscriber only gets events matching their filters
		if sub.StateCode != "" && stateCode != "" && sub.StateCode != stateCode {
			continue
		}
		if sub.LGACode != "" && lgaCode != "" && sub.LGACode != lgaCode {
			continue
		}
		if sub.PartyCode != "" && partyCode != "" && sub.PartyCode != partyCode {
			continue
		}
		select {
		case sub.Channel <- event:
		default:
			// Slow consumer, skip
		}
	}
}

// NotifyResultSubmission broadcasts a new result to all matching SSE subscribers
func NotifyResultSubmission(result map[string]interface{}) {
	stateCode, _ := result["state_code"].(string)
	lgaCode, _ := result["lga_code"].(string)
	partyCode := "" // Results contain all parties

	sseHub.broadcast(SSEEvent{
		Type: "result_submitted",
		Data: result,
	}, stateCode, lgaCode, partyCode)
}

// ── Observer Alert System ──

type AlertRule struct {
	ID          int       `json:"id"`
	UserID      int       `json:"user_id"`
	PartyCode   string    `json:"party_code"`
	StateCode   string    `json:"state_code,omitempty"`
	LGACode     string    `json:"lga_code,omitempty"`
	AlertType   string    `json:"alert_type"` // result_submitted, anomaly_detected, geofence_violation
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	TriggeredAt *string   `json:"triggered_at,omitempty"`
}

type ObserverReport struct {
	ID              int       `json:"id"`
	ObserverID      int       `json:"observer_id"`
	PollingUnitCode string    `json:"polling_unit_code"`
	ElectionID      int       `json:"election_id"`
	ReportType      string    `json:"report_type"` // result_photo, irregularity, observation
	Description     string    `json:"description"`
	PhotoURL        string    `json:"photo_url,omitempty"`
	Latitude        float64   `json:"latitude,omitempty"`
	Longitude       float64   `json:"longitude,omitempty"`
	Status          string    `json:"status"` // pending, reviewed, flagged
	CreatedAt       time.Time `json:"created_at"`
}

// ── SSE Streaming Endpoint ──

func handleSSEStream(w http.ResponseWriter, r *http.Request) {
	// SSE uses EventSource which cannot set headers — accept token as query param
	var claims map[string]interface{}
	if c, ok := getUserFromContext(r); ok {
		claims = c
	} else if tokenStr := r.URL.Query().Get("token"); tokenStr != "" {
		c, err := decodeToken(tokenStr)
		if err != nil {
			writeError(w, 401, "invalid token")
			return
		}
		claims = c
	} else {
		writeError(w, 401, "authentication required (pass ?token= for SSE)")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "streaming not supported")
		return
	}

	userIDStr, _ := claims["sub"].(string)
	userID, _ := strconv.Atoi(userIDStr)

	subID := fmt.Sprintf("sse-%d-%d", userID, time.Now().UnixNano())
	// Security: sanitize filter values to prevent SSE event injection
	sanitizeSSE := func(s string) string {
		s = strings.ReplaceAll(s, "\n", "")
		s = strings.ReplaceAll(s, "\r", "")
		s = strings.ReplaceAll(s, "\"", "")
		if len(s) > 50 {
			s = s[:50]
		}
		return s
	}
	sub := &SSESubscriber{
		ID:        subID,
		UserID:    userID,
		PartyCode: sanitizeSSE(r.URL.Query().Get("party")),
		StateCode: sanitizeSSE(r.URL.Query().Get("state")),
		LGACode:   sanitizeSSE(r.URL.Query().Get("lga")),
		Channel:   make(chan SSEEvent, 100),
		CreatedAt: time.Now(),
	}

	if !sseHub.subscribe(sub) {
		writeError(w, 429, "SSE connection limit reached")
		return
	}
	defer sseHub.unsubscribe(subID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send initial connection event — sanitize user-supplied filter values
	connData, _ := json.Marshal(map[string]interface{}{
		"subscriber_id": subID,
		"filters": map[string]string{
			"party": sub.PartyCode,
			"state": sub.StateCode,
			"lga":   sub.LGACode,
		},
	})
	fmt.Fprintf(w, "event: connected\ndata: %s\n\n", string(connData))
	flusher.Flush()

	// Keep-alive ticker
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Channel:
			if !ok {
				return
			}
			data, _ := json.Marshal(event.Data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(data))
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// ── Photo/Scan Upload ──

func handleObserverPhotoUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardAuth(w, r)
	if !ok {
		return
	}

	userIDStr, _ := claims["sub"].(string)
	userID, _ := strconv.Atoi(userIDStr)

	// Parse multipart form (max 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, 400, "file too large (max 10MB)")
		return
	}

	file, header, err := r.FormFile("photo")
	if err != nil {
		writeError(w, 400, "photo field required")
		return
	}
	defer file.Close()

	// Validate file type
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".pdf" {
		writeError(w, 400, "invalid file type (allowed: jpg, jpeg, png, pdf)")
		return
	}

	pollingUnitCode := r.FormValue("polling_unit_code")
	electionIDStr := r.FormValue("election_id")
	reportType := r.FormValue("report_type")
	description := r.FormValue("description")
	latStr := r.FormValue("latitude")
	lonStr := r.FormValue("longitude")

	if pollingUnitCode == "" || electionIDStr == "" {
		writeError(w, 400, "polling_unit_code and election_id required")
		return
	}

	electionID, _ := strconv.Atoi(electionIDStr)
	lat, _ := strconv.ParseFloat(latStr, 64)
	lon, _ := strconv.ParseFloat(lonStr, 64)
	if reportType == "" {
		reportType = "result_photo"
	}

	// Save file to uploads directory
	uploadDir := filepath.Join("uploads", "observer-reports")
	os.MkdirAll(uploadDir, 0750)
	// Sanitize filename components to prevent path traversal
	safePU := filepath.Base(strings.ReplaceAll(pollingUnitCode, "..", ""))
	filename := fmt.Sprintf("%d_%s_%d%s", userID, safePU, time.Now().UnixNano(), ext)
	filePath := filepath.Join(uploadDir, filename)

	dst, err := os.Create(filePath)
	if err != nil {
		writeError(w, 500, "failed to save file")
		return
	}
	defer dst.Close()
	io.Copy(dst, file)

	photoURL := fmt.Sprintf("/uploads/observer-reports/%s", filename)

	// Persist report to DB
	result, err := db.Exec(
		"INSERT INTO observer_reports (observer_id, polling_unit_code, election_id, report_type, description, photo_url, latitude, longitude, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', CURRENT_TIMESTAMP)",
		userID, pollingUnitCode, electionID, reportType, description, photoURL, lat, lon)
	if err != nil {
		writeError(w, 500, "failed to save report")
		return
	}

	reportID, _ := result.LastInsertId()

	// Broadcast to SSE subscribers
	sseHub.broadcast(SSEEvent{
		Type: "observer_report",
		Data: M{
			"report_id":         reportID,
			"observer_id":       userID,
			"polling_unit_code": pollingUnitCode,
			"report_type":       reportType,
			"photo_url":         photoURL,
		},
	}, "", "", "")

	writeJSON(w, 201, M{
		"report_id": reportID,
		"photo_url": photoURL,
		"status":    "pending",
		"message":   "Report submitted successfully",
	})
}

// ── Observer Reports CRUD ──

func handleListObserverReports(w http.ResponseWriter, r *http.Request) {
	electionID := r.URL.Query().Get("election_id")
	puCode := r.URL.Query().Get("polling_unit_code")
	status := r.URL.Query().Get("status")
	limit := queryParamInt(r, "limit", 50)

	q := "SELECT id, observer_id, polling_unit_code, election_id, report_type, description, photo_url, latitude, longitude, status, created_at FROM observer_reports WHERE 1=1"
	var params []interface{}

	if electionID != "" {
		q += " AND election_id=?"
		params = append(params, electionID)
	}
	if puCode != "" {
		q += " AND polling_unit_code=?"
		params = append(params, puCode)
	}
	if status != "" {
		q += " AND status=?"
		params = append(params, status)
	}
	q += " ORDER BY created_at DESC LIMIT ?"
	params = append(params, limit)

	rows, err := db.Query(q, params...)
	if err != nil {
		writeJSON(w, 200, []interface{}{})
		return
	}
	writeJSON(w, 200, scanRows(rows))
}

func handleReviewObserverReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"` // reviewed, flagged
		Notes  string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Status != "reviewed" && req.Status != "flagged" {
		writeError(w, 400, "status must be 'reviewed' or 'flagged'")
		return
	}

	id := extractPathParam(r, "id")
	result, err := db.Exec("UPDATE observer_reports SET status=?, review_notes=?, reviewed_at=CURRENT_TIMESTAMP WHERE id=?", req.Status, req.Notes, id)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("failed to update observer report")
		writeError(w, 500, "failed to update report")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, 404, "report not found")
		return
	}
	writeJSON(w, 200, M{"message": "Report updated", "status": req.Status})
}

// ── Alert Rule Management ──

func handleCreateAlertRule(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardAuth(w, r)
	if !ok {
		return
	}
	userIDStr, _ := claims["sub"].(string)
	userID, _ := strconv.Atoi(userIDStr)

	var req struct {
		PartyCode string `json:"party_code"`
		StateCode string `json:"state_code"`
		LGACode   string `json:"lga_code"`
		AlertType string `json:"alert_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.AlertType == "" {
		req.AlertType = "result_submitted"
	}

	result, err := db.Exec(
		"INSERT INTO observer_alert_rules (user_id, party_code, state_code, lga_code, alert_type, is_active, created_at) VALUES (?, ?, ?, ?, ?, 1, CURRENT_TIMESTAMP)",
		userID, req.PartyCode, req.StateCode, req.LGACode, req.AlertType)
	if err != nil {
		writeError(w, 500, "failed to create alert rule")
		return
	}

	ruleID, _ := result.LastInsertId()
	writeJSON(w, 201, M{
		"rule_id":    ruleID,
		"alert_type": req.AlertType,
		"filters": M{
			"party_code": req.PartyCode,
			"state_code": req.StateCode,
			"lga_code":   req.LGACode,
		},
		"message": "Alert rule created",
	})
}

func handleListAlertRules(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardAuth(w, r)
	if !ok {
		return
	}
	userIDStr, _ := claims["sub"].(string)
	userID, _ := strconv.Atoi(userIDStr)

	rows, err := db.Query("SELECT id, user_id, party_code, state_code, lga_code, alert_type, is_active, created_at FROM observer_alert_rules WHERE user_id=? ORDER BY created_at DESC", userID)
	if err != nil {
		writeJSON(w, 200, []interface{}{})
		return
	}
	writeJSON(w, 200, scanRows(rows))
}

func handleDeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardAuth(w, r)
	if !ok {
		return
	}
	userIDStr, _ := claims["sub"].(string)
	userID, _ := strconv.Atoi(userIDStr)
	id := extractPathParam(r, "id")

	result, err := db.Exec("DELETE FROM observer_alert_rules WHERE id=? AND user_id=?", id, userID)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("failed to delete alert rule")
		writeError(w, 500, "failed to delete alert rule")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, 404, "alert rule not found")
		return
	}
	writeJSON(w, 200, M{"message": "Alert rule deleted"})
}

// ── Party-Specific Dashboard ──

func handlePartyDashboard(w http.ResponseWriter, r *http.Request) {
	partyCode := r.URL.Query().Get("party")
	if partyCode == "" {
		writeError(w, 400, "party query parameter required")
		return
	}

	// Get party vote totals across all elections
	var totalVotes, totalPUs int
	db.QueryRow("SELECT COALESCE(SUM(votes), 0), COUNT(DISTINCT polling_unit_code) FROM results WHERE party_code=?", partyCode).Scan(&totalVotes, &totalPUs)

	// Get state-level breakdown
	stateRows, _ := db.Query(`
		SELECT r.state_code, COALESCE(SUM(r.votes), 0) as votes, COUNT(DISTINCT r.polling_unit_code) as pu_count
		FROM results r WHERE r.party_code=?
		GROUP BY r.state_code ORDER BY votes DESC`, partyCode)
	stateBreakdown := scanRows(stateRows)

	// Get recent results for this party
	recentRows, _ := db.Query(`
		SELECT r.id, r.polling_unit_code, r.state_code, r.lga_code, r.votes, r.created_at
		FROM results r WHERE r.party_code=?
		ORDER BY r.created_at DESC LIMIT 20`, partyCode)
	recentResults := scanRows(recentRows)

	// Get observer reports for PUs where this party is competing
	var reportCount int
	db.QueryRow("SELECT COUNT(*) FROM observer_reports").Scan(&reportCount)

	// Get coverage (PUs reported vs total)
	var totalPollingUnits int
	db.QueryRow("SELECT COUNT(*) FROM polling_units").Scan(&totalPollingUnits)
	coverage := 0.0
	if totalPollingUnits > 0 {
		coverage = float64(totalPUs) / float64(totalPollingUnits) * 100
	}

	writeJSON(w, 200, M{
		"party_code":                 partyCode,
		"total_votes":                totalVotes,
		"polling_units_with_results": totalPUs,
		"total_polling_units":        totalPollingUnits,
		"coverage_pct":               round2(coverage),
		"state_breakdown":            stateBreakdown,
		"recent_results":             recentResults,
		"observer_reports":           reportCount,
	})
}

// ── Observer Location Check-In ──

func handleObserverCheckIn(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardAuth(w, r)
	if !ok {
		return
	}
	userIDStr, _ := claims["sub"].(string)
	userID, _ := strconv.Atoi(userIDStr)

	var req struct {
		PollingUnitCode string  `json:"polling_unit_code"`
		Latitude        float64 `json:"latitude"`
		Longitude       float64 `json:"longitude"`
		DeviceInfo      string  `json:"device_info"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.PollingUnitCode == "" {
		writeError(w, 400, "polling_unit_code required")
		return
	}

	// Validate location against PU geofence
	geoResult, _ := validateGeofence(req.Latitude, req.Longitude, req.PollingUnitCode)

	dbExecLog("db_op",
		"INSERT INTO observer_check_ins (observer_id, polling_unit_code, latitude, longitude, device_info, within_geofence, checked_in_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)",
		userID, req.PollingUnitCode, req.Latitude, req.Longitude, req.DeviceInfo, geoResult.WithinGeofence)

	// Broadcast check-in event
	sseHub.broadcast(SSEEvent{
		Type: "observer_checkin",
		Data: M{
			"observer_id":       userID,
			"polling_unit_code": req.PollingUnitCode,
			"within_geofence":   geoResult.WithinGeofence,
			"distance_m":        geoResult.DistanceMeters,
		},
	}, "", "", "")

	// Publish to Kafka
	go publishAuditEvent("OBSERVER_CHECKIN", "observer", fmt.Sprintf("%d", userID), userID,
		map[string]interface{}{"polling_unit_code": req.PollingUnitCode, "within_geofence": geoResult.WithinGeofence})

	writeJSON(w, 200, M{
		"checked_in":      true,
		"within_geofence": geoResult.WithinGeofence,
		"distance_m":      geoResult.DistanceMeters,
		"message":         "Observer checked in successfully",
	})
}

// ── Live Observer Stats ──

func handleObserverStats(w http.ResponseWriter, r *http.Request) {
	var totalObservers, activeCheckIns, reportsToday, alertRules int

	db.QueryRow("SELECT COUNT(*) FROM users WHERE role='observer'").Scan(&totalObservers)
	db.QueryRow("SELECT COUNT(*) FROM observer_check_ins WHERE checked_in_at > datetime('now', '-1 hour')").Scan(&activeCheckIns)
	db.QueryRow("SELECT COUNT(*) FROM observer_reports WHERE created_at > datetime('now', '-24 hours')").Scan(&reportsToday)
	db.QueryRow("SELECT COUNT(*) FROM observer_alert_rules WHERE is_active=1").Scan(&alertRules)

	sseHub.mu.RLock()
	activeStreams := len(sseHub.subscribers)
	sseHub.mu.RUnlock()

	writeJSON(w, 200, M{
		"total_observers":    totalObservers,
		"active_check_ins":   activeCheckIns,
		"reports_today":      reportsToday,
		"active_alert_rules": alertRules,
		"active_sse_streams": activeStreams,
	})
}

// ── Helper ──

func extractPathParam(r *http.Request, key string) string {
	vars := mux.Vars(r)
	return vars[key]
}

// initObserverTables creates the observer monitoring tables
func initObserverTables() {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS observer_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			observer_id INTEGER NOT NULL,
			polling_unit_code TEXT NOT NULL,
			election_id INTEGER NOT NULL,
			report_type TEXT NOT NULL DEFAULT 'observation',
			description TEXT,
			photo_url TEXT,
			latitude REAL,
			longitude REAL,
			status TEXT NOT NULL DEFAULT 'pending',
			review_notes TEXT,
			reviewed_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS observer_alert_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			party_code TEXT,
			state_code TEXT,
			lga_code TEXT,
			alert_type TEXT NOT NULL DEFAULT 'result_submitted',
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS observer_check_ins (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			observer_id INTEGER NOT NULL,
			polling_unit_code TEXT NOT NULL,
			latitude REAL,
			longitude REAL,
			device_info TEXT,
			within_geofence BOOLEAN,
			checked_in_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_observer_reports_pu ON observer_reports(polling_unit_code)`,
		`CREATE INDEX IF NOT EXISTS idx_observer_reports_election ON observer_reports(election_id)`,
		`CREATE INDEX IF NOT EXISTS idx_observer_checkins_observer ON observer_check_ins(observer_id)`,
	}

	for _, stmt := range statements {
		dbExecLog("db_op", stmt)
	}
}
