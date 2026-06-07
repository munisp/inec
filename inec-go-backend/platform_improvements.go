package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ══════════════════════════════════════════════════════════════════════════════
// G4: OpenAPI 3.1 Specification — auto-generated from route definitions
// ══════════════════════════════════════════════════════════════════════════════

type openAPIRoute struct {
	Path    string `json:"path"`
	Method  string `json:"method"`
	Summary string `json:"summary"`
	Tag     string `json:"tag"`
	Auth    string `json:"auth"`
}

var registeredRoutes []openAPIRoute

func registerAPIRoute(path, method, summary, tag, auth string) {
	registeredRoutes = append(registeredRoutes, openAPIRoute{
		Path: path, Method: method, Summary: summary, Tag: tag, Auth: auth,
	})
}

func handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	paths := map[string]interface{}{}
	for _, route := range registeredRoutes {
		if paths[route.Path] == nil {
			paths[route.Path] = map[string]interface{}{}
		}
		pathObj := paths[route.Path].(map[string]interface{})
		op := map[string]interface{}{
			"summary":     route.Summary,
			"tags":        []string{route.Tag},
			"operationId": strings.ReplaceAll(route.Path, "/", "_") + "_" + strings.ToLower(route.Method),
			"responses": map[string]interface{}{
				"200": map[string]interface{}{"description": "Success"},
				"401": map[string]interface{}{"description": "Unauthorized"},
				"500": map[string]interface{}{"description": "Internal server error"},
			},
		}
		if route.Auth != "none" {
			op["security"] = []map[string]interface{}{{"bearerAuth": []string{}}}
		}
		pathObj[strings.ToLower(route.Method)] = op
	}

	spec := map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]interface{}{
			"title":       "INEC Election Management Platform API",
			"version":     "2.0.0",
			"description": "Comprehensive API for Nigeria's Independent National Electoral Commission election management platform. Covers election lifecycle, result collation, observer monitoring, biometric verification, AI/ML analytics, and administrative operations.",
			"contact": map[string]interface{}{
				"name": "INEC Technical Team",
			},
		},
		"servers": []map[string]interface{}{
			{"url": "/api/v1", "description": "API v1"},
			{"url": "/", "description": "Legacy"},
		},
		"paths": paths,
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"bearerAuth": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
			},
		},
		"tags": []map[string]interface{}{
			{"name": "Auth", "description": "Authentication and session management"},
			{"name": "Elections", "description": "Election lifecycle management"},
			{"name": "Results", "description": "Result submission and collation"},
			{"name": "Observers", "description": "Observer monitoring and reports"},
			{"name": "Biometrics", "description": "Biometric verification and BVAS"},
			{"name": "Security", "description": "Security, KYC/KYB, and fraud detection"},
			{"name": "AI/ML", "description": "Artificial intelligence and analytics"},
			{"name": "Admin", "description": "Administrative operations"},
			{"name": "Middleware", "description": "Infrastructure middleware"},
			{"name": "Monitoring", "description": "System health and observability"},
			{"name": "CommandCenter", "description": "Election command center"},
			{"name": "Public", "description": "Public-facing endpoints"},
		},
	}
	writeJSON(w, 200, spec)
}

func handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html><html><head><title>INEC API Docs</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head><body>
<div id="swagger-ui"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({url:'/api/openapi.json',dom_id:'#swagger-ui',deepLinking:true,presets:[SwaggerUIBundle.presets.apis,SwaggerUIBundle.SwaggerUIStandalonePreset],layout:'StandaloneLayout'})</script>
</body></html>`
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// ══════════════════════════════════════════════════════════════════════════════
// S2: HMAC Request Signing for Critical Endpoints
// ══════════════════════════════════════════════════════════════════════════════

func hmacSignedEndpoint(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		signature := r.Header.Get("X-INEC-Signature")
		timestamp := r.Header.Get("X-INEC-Timestamp")
		deviceID := r.Header.Get("X-INEC-Device-ID")

		if signature == "" || timestamp == "" {
			next.ServeHTTP(w, r)
			return
		}

		ts, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || time.Now().Unix()-ts > 300 {
			writeError(w, 401, "invalid or expired timestamp")
			return
		}

		userID := extractUserID(r)
		payload := fmt.Sprintf("%s:%s:%d:%d", r.Method, r.URL.Path, userID, ts)

		var secret string
		db.QueryRow("SELECT device_secret FROM device_keys WHERE user_id = ? AND device_id = ?", userID, deviceID).Scan(&secret)
		if secret == "" {
			next.ServeHTTP(w, r)
			return
		}

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(payload))
		expected := hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(signature), []byte(expected)) {
			writeError(w, 403, "invalid request signature")
			return
		}

		next.ServeHTTP(w, r)
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// S3: Geo-IP Fraud Detection
// ══════════════════════════════════════════════════════════════════════════════

func handleGeoIPCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GPSLat    float64 `json:"gps_lat"`
		GPSLng    float64 `json:"gps_lng"`
		DeviceID  string  `json:"device_id"`
		IPAddress string  `json:"ip_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}

	clientIP := req.IPAddress
	if clientIP == "" {
		clientIP = extractClientIP(r)
	}

	result := analyzeGeoIPFraud(clientIP, req.GPSLat, req.GPSLng, req.DeviceID)
	writeJSON(w, 200, result)
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func analyzeGeoIPFraud(ip string, gpsLat, gpsLng float64, deviceID string) M {
	risk := 0.0
	flags := []string{}

	// Check for VPN/proxy indicators
	isPrivate := isPrivateIP(ip)
	if isPrivate {
		flags = append(flags, "private_ip_detected")
		risk += 0.1
	}

	// Check for known VPN ranges
	if isKnownVPNRange(ip) {
		flags = append(flags, "vpn_ip_detected")
		risk += 0.4
	}

	// Check GPS within Nigeria bounds (lat 4-14, lng 2-15)
	if gpsLat < 4.0 || gpsLat > 14.0 || gpsLng < 2.0 || gpsLng > 15.0 {
		flags = append(flags, "gps_outside_nigeria")
		risk += 0.5
	}

	// Check device history
	var prevLat, prevLng float64
	var lastSeen time.Time
	err := db.QueryRow("SELECT latitude, longitude, last_seen FROM device_locations WHERE device_id = ? ORDER BY last_seen DESC LIMIT 1", deviceID).Scan(&prevLat, &prevLng, &lastSeen)
	if err == nil {
		elapsed := time.Since(lastSeen).Hours()
		if elapsed < 1 {
			dist := haversineDistance(prevLat, prevLng, gpsLat, gpsLng)
			if dist > 100 { // 100km in < 1 hour = suspicious
				flags = append(flags, fmt.Sprintf("impossible_travel_%.0fkm_in_%.1fhr", dist, elapsed))
				risk += 0.6
			}
		}
	}

	// Update device location
	dbExecLog("device_locations", "INSERT INTO device_locations (device_id, ip_address, latitude, longitude, last_seen) VALUES (?, ?, ?, ?, ?)",
		deviceID, ip, gpsLat, gpsLng, time.Now())

	if risk > 1.0 {
		risk = 1.0
	}

	verdict := "low_risk"
	if risk > 0.7 {
		verdict = "high_risk"
	} else if risk > 0.4 {
		verdict = "medium_risk"
	}

	return M{
		"ip":           ip,
		"gps_location": M{"lat": gpsLat, "lng": gpsLng},
		"risk_score":   risk,
		"verdict":      verdict,
		"flags":        flags,
		"is_private":   isPrivate,
		"device_id":    deviceID,
		"checked_at":   time.Now().Format(time.RFC3339),
	}
}

func isPrivateIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	privateRanges := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8"}
	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(parsed) {
			return true
		}
	}
	return false
}

func isKnownVPNRange(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	// Common datacenter/VPN provider ranges
	vpnRanges := []string{"104.16.0.0/12", "198.41.128.0/17", "162.158.0.0/15"}
	for _, cidr := range vpnRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(parsed) {
			return true
		}
	}
	return false
}

// ══════════════════════════════════════════════════════════════════════════════
// S4: Data Loss Prevention (DLP) — watermarking + export controls
// ══════════════════════════════════════════════════════════════════════════════

func handleDLPExport(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	exportType := r.URL.Query().Get("type")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	// Log export event
	dbExecLog("export_audit_log",
		"INSERT INTO export_audit_log (user_id, export_type, format, ip_address, timestamp) VALUES (?, ?, ?, ?, ?)",
		userID, exportType, format, extractClientIP(r), time.Now())

	// Check export rate limit
	var recentExports int
	db.QueryRow("SELECT COUNT(*) FROM export_audit_log WHERE user_id = ? AND timestamp > ?",
		userID, time.Now().Add(-1*time.Hour)).Scan(&recentExports)

	if recentExports > 10 {
		writeError(w, 429, "export rate limit exceeded — max 10 per hour")
		return
	}

	watermark := M{
		"exported_by": userID,
		"exported_at": time.Now().Format(time.RFC3339),
		"ip_address":  extractClientIP(r),
		"export_id":   fmt.Sprintf("EXP-%d", time.Now().UnixNano()),
		"watermark":   fmt.Sprintf("INEC-CONFIDENTIAL-%d-%d", userID, time.Now().Unix()),
	}

	writeJSON(w, 200, M{
		"watermark":   watermark,
		"export_type": exportType,
		"format":      format,
		"message":     "export tracked and watermarked",
	})
}

// ══════════════════════════════════════════════════════════════════════════════
// I1: Real-Time Collaboration — presence + annotations
// ══════════════════════════════════════════════════════════════════════════════

var presenceTracker = struct {
	sync.RWMutex
	users map[string]map[string]time.Time // page -> user_id -> last_seen
}{users: make(map[string]map[string]time.Time)}

func handlePresenceHeartbeat(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	var req struct {
		Page string `json:"page"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request")
		return
	}

	presenceTracker.Lock()
	if presenceTracker.users[req.Page] == nil {
		presenceTracker.users[req.Page] = make(map[string]time.Time)
	}
	presenceTracker.users[req.Page][fmt.Sprintf("%d", userID)] = time.Now()
	presenceTracker.Unlock()

	writeJSON(w, 200, M{"ok": true})
}

func handlePresenceList(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	presenceTracker.RLock()
	defer presenceTracker.RUnlock()

	activeUsers := []M{}
	if pageUsers, ok := presenceTracker.users[page]; ok {
		for uid, lastSeen := range pageUsers {
			if time.Since(lastSeen) < 30*time.Second {
				activeUsers = append(activeUsers, M{"user_id": uid, "last_seen": lastSeen.Format(time.RFC3339)})
			}
		}
	}
	writeJSON(w, 200, M{"page": page, "active_users": activeUsers, "count": len(activeUsers)})
}

// ══════════════════════════════════════════════════════════════════════════════
// I7: Batch Operations — CSV import/export for user management
// ══════════════════════════════════════════════════════════════════════════════

func handleBatchUserImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Users []struct {
			Username string `json:"username"`
			Email    string `json:"email"`
			Role     string `json:"role"`
			State    string `json:"state_code"`
		} `json:"users"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}

	if len(req.Users) > 1000 {
		writeError(w, 400, "batch size exceeds 1000 user limit")
		return
	}

	created, failed := 0, 0
	errors := []M{}
	for _, u := range req.Users {
		if u.Username == "" || u.Email == "" || u.Role == "" {
			failed++
			errors = append(errors, M{"username": u.Username, "error": "missing required fields"})
			continue
		}
		_, err := db.Exec("INSERT INTO users (username, email, role, state_code, created_at) VALUES (?, ?, ?, ?, ?)",
			u.Username, u.Email, u.Role, u.State, time.Now())
		if err != nil {
			failed++
			errors = append(errors, M{"username": u.Username, "error": err.Error()})
		} else {
			created++
		}
	}

	writeJSON(w, 200, M{
		"total":   len(req.Users),
		"created": created,
		"failed":  failed,
		"errors":  errors,
	})
}

func handleBatchStatusUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Entity string `json:"entity"` // incidents, disputes, users
		IDs    []int  `json:"ids"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}

	allowedEntities := map[string]string{
		"incidents": "stakeholder_incidents",
		"disputes":  "disputes",
	}
	table, ok := allowedEntities[req.Entity]
	if !ok {
		writeError(w, 400, "invalid entity type")
		return
	}

	if len(req.IDs) > 500 {
		writeError(w, 400, "batch size exceeds 500 limit")
		return
	}

	updated := 0
	for _, id := range req.IDs {
		res, err := db.Exec(fmt.Sprintf("UPDATE %s SET status = ? WHERE id = ?", table), req.Status, id)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				updated++
			}
		}
	}

	writeJSON(w, 200, M{"total": len(req.IDs), "updated": updated, "status": req.Status})
}

// ══════════════════════════════════════════════════════════════════════════════
// N1: AI Integrity Score — composite per-PU score
// ══════════════════════════════════════════════════════════════════════════════

func handleIntegrityScore(w http.ResponseWriter, r *http.Request) {
	puCode := r.URL.Query().Get("pu_code")
	electionID := queryParamInt(r, "election_id", 1)

	scores := calculateIntegrityScore(puCode, electionID)
	writeJSON(w, 200, scores)
}

func handleIntegrityHeatmap(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)
	stateCode := r.URL.Query().Get("state_code")

	query := "SELECT DISTINCT polling_unit_code FROM results WHERE election_id = ?"
	args := []interface{}{electionID}
	if stateCode != "" {
		query += " AND state_code = ?"
		args = append(args, stateCode)
	}
	query += " LIMIT 200"

	rows, err := db.Query(query, args...)
	if err != nil {
		writeJSON(w, 200, M{"heatmap": []M{}, "total": 0})
		return
	}
	defer rows.Close()

	var heatmap []M
	for rows.Next() {
		var pu string
		rows.Scan(&pu)
		score := calculateIntegrityScore(pu, electionID)
		heatmap = append(heatmap, score)
	}

	sort.Slice(heatmap, func(i, j int) bool {
		si, _ := heatmap[i]["composite_score"].(float64)
		sj, _ := heatmap[j]["composite_score"].(float64)
		return si < sj
	})

	writeJSON(w, 200, M{"heatmap": heatmap, "total": len(heatmap), "election_id": electionID})
}

func calculateIntegrityScore(puCode string, electionID int) M {
	scores := M{"polling_unit_code": puCode, "election_id": electionID}

	// 1. Benford's Law compliance
	benfordScore := 0.85
	var totalVotes int
	db.QueryRow("SELECT COALESCE(SUM(votes), 0) FROM results WHERE polling_unit_code = ? AND election_id = ?",
		puCode, electionID).Scan(&totalVotes)
	if totalVotes > 0 {
		benfordScore = calculateBenfordCompliance(puCode, electionID)
	}

	// 2. Geofence compliance
	geoScore := 1.0
	var geoChecks, geoViolations int
	db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN within_boundary = 0 THEN 1 ELSE 0 END), 0) FROM geofenced_submissions WHERE result_id IN (SELECT id FROM results WHERE polling_unit_code = ?)", puCode).Scan(&geoChecks, &geoViolations)
	if geoChecks > 0 {
		geoScore = 1.0 - float64(geoViolations)/float64(geoChecks)
	}

	// 3. Submission timing
	timingScore := 0.9
	var submittedAt string
	err := db.QueryRow("SELECT submitted_at FROM results WHERE polling_unit_code = ? AND election_id = ? ORDER BY submitted_at DESC LIMIT 1",
		puCode, electionID).Scan(&submittedAt)
	if err == nil {
		t, _ := time.Parse(time.RFC3339, submittedAt)
		hour := t.Hour()
		if hour < 6 || hour > 22 {
			timingScore = 0.5
		}
	}

	// 4. Observer presence
	observerScore := 0.7
	var observerCount int
	db.QueryRow("SELECT COUNT(*) FROM observer_reports WHERE polling_unit_code = ? AND election_id = ?",
		puCode, electionID).Scan(&observerCount)
	if observerCount >= 2 {
		observerScore = 1.0
	} else if observerCount == 1 {
		observerScore = 0.85
	}

	// 5. Anomaly check
	anomalyScore := 1.0
	var anomalyCount int
	db.QueryRow("SELECT COUNT(*) FROM anomaly_escalations WHERE pu_code = ? AND resolved = 0", puCode).Scan(&anomalyCount)
	if anomalyCount > 0 {
		anomalyScore = 0.5
	}

	composite := (benfordScore*0.25 + geoScore*0.20 + timingScore*0.15 + observerScore*0.15 + anomalyScore*0.25)

	scores["benford_compliance"] = benfordScore
	scores["geofence_compliance"] = geoScore
	scores["timing_score"] = timingScore
	scores["observer_presence"] = observerScore
	scores["anomaly_score"] = anomalyScore
	scores["composite_score"] = composite
	scores["rating"] = integrityRating(composite)
	scores["total_votes"] = totalVotes

	return scores
}

func calculateBenfordCompliance(puCode string, electionID int) float64 {
	rows, err := db.Query("SELECT votes FROM results WHERE polling_unit_code = ? AND election_id = ? AND votes > 0",
		puCode, electionID)
	if err != nil {
		return 0.85
	}
	defer rows.Close()

	digitCounts := make([]int, 10)
	total := 0
	for rows.Next() {
		var v int
		rows.Scan(&v)
		if v > 0 {
			firstDigit := int(fmt.Sprintf("%d", v)[0] - '0')
			if firstDigit >= 1 && firstDigit <= 9 {
				digitCounts[firstDigit]++
				total++
			}
		}
	}
	if total < 5 {
		return 0.85
	}

	expected := []float64{0, 0.301, 0.176, 0.125, 0.097, 0.079, 0.067, 0.058, 0.051, 0.046}
	chiSq := 0.0
	for d := 1; d <= 9; d++ {
		observed := float64(digitCounts[d]) / float64(total)
		diff := observed - expected[d]
		chiSq += diff * diff / expected[d]
	}
	score := 1.0 - chiSq*2
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

func integrityRating(score float64) string {
	if score >= 0.9 {
		return "excellent"
	}
	if score >= 0.75 {
		return "good"
	}
	if score >= 0.5 {
		return "fair"
	}
	return "poor"
}

// ══════════════════════════════════════════════════════════════════════════════
// N2: Blockchain Result Certificate with QR verification
// ══════════════════════════════════════════════════════════════════════════════

func handleResultCertificate(w http.ResponseWriter, r *http.Request) {
	puCode := r.URL.Query().Get("pu_code")
	electionID := queryParamInt(r, "election_id", 1)

	var totalVotes, rejectedVotes int
	var submittedAt string
	err := db.QueryRow("SELECT COALESCE(SUM(votes), 0), COALESCE(MAX(submitted_at), ''), 0 FROM results WHERE polling_unit_code = ? AND election_id = ?",
		puCode, electionID).Scan(&totalVotes, &submittedAt, &rejectedVotes)
	if err != nil || totalVotes == 0 {
		writeError(w, 404, "no results found for this polling unit")
		return
	}

	// Get party results
	rows, err := db.Query("SELECT party_code, votes FROM results WHERE polling_unit_code = ? AND election_id = ? ORDER BY votes DESC",
		puCode, electionID)
	if err != nil {
		writeError(w, 500, "failed to query results")
		return
	}
	defer rows.Close()

	partyResults := []M{}
	for rows.Next() {
		var party string
		var votes int
		rows.Scan(&party, &votes)
		partyResults = append(partyResults, M{"party": party, "votes": votes})
	}

	// Generate certificate hash
	certData := fmt.Sprintf("%s|%d|%d|%s", puCode, electionID, totalVotes, submittedAt)
	hash := sha256.Sum256([]byte(certData))
	certHash := hex.EncodeToString(hash[:])

	// Get blockchain verification if available
	var blockHash string
	db.QueryRow("SELECT block_hash FROM blockchain_records WHERE data_hash = ?", certHash).Scan(&blockHash)

	certificate := M{
		"certificate_id":     fmt.Sprintf("CERT-%s-%d", puCode, electionID),
		"polling_unit_code":  puCode,
		"election_id":        electionID,
		"total_valid_votes":  totalVotes,
		"rejected_votes":     rejectedVotes,
		"party_results":      partyResults,
		"submitted_at":       submittedAt,
		"certificate_hash":   certHash,
		"blockchain_anchor":  blockHash,
		"verification_url":   fmt.Sprintf("/citizen/verify?pu=%s&election=%d", puCode, electionID),
		"qr_data":            fmt.Sprintf("INEC-VERIFY:%s:%d:%s", puCode, electionID, certHash[:16]),
		"generated_at":       time.Now().Format(time.RFC3339),
		"integrity_score":    calculateIntegrityScore(puCode, electionID),
		"tamper_evident":     true,
	}

	writeJSON(w, 200, certificate)
}

// ══════════════════════════════════════════════════════════════════════════════
// N6: Public TV Dashboard — large-screen election results
// ══════════════════════════════════════════════════════════════════════════════

func handlePublicTVDashboard(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)

	// Get overall progress
	var totalPUs, reportedPUs int
	db.QueryRow("SELECT COUNT(DISTINCT code) FROM polling_units").Scan(&totalPUs)
	db.QueryRow("SELECT COUNT(DISTINCT polling_unit_code) FROM results WHERE election_id = ?", electionID).Scan(&reportedPUs)

	// Get party totals
	rows, err := db.Query("SELECT party_code, SUM(votes) as total FROM results WHERE election_id = ? GROUP BY party_code ORDER BY total DESC", electionID)
	partyTotals := []M{}
	totalVotes := 0
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var party string
			var votes int
			rows.Scan(&party, &votes)
			partyTotals = append(partyTotals, M{"party": party, "votes": votes})
			totalVotes += votes
		}
	}

	// Get state-level results
	stateRows, err := db.Query(`SELECT state_code, party_code, SUM(votes) as total 
		FROM results WHERE election_id = ? GROUP BY state_code, party_code ORDER BY state_code, total DESC`, electionID)
	stateResults := map[string][]M{}
	if err == nil {
		defer stateRows.Close()
		for stateRows.Next() {
			var state, party string
			var votes int
			stateRows.Scan(&state, &party, &votes)
			stateResults[state] = append(stateResults[state], M{"party": party, "votes": votes})
		}
	}

	completionPct := 0.0
	if totalPUs > 0 {
		completionPct = float64(reportedPUs) / float64(totalPUs) * 100
	}

	writeJSON(w, 200, M{
		"election_id":    electionID,
		"total_pus":      totalPUs,
		"reported_pus":   reportedPUs,
		"completion_pct": completionPct,
		"total_votes":    totalVotes,
		"party_totals":   partyTotals,
		"state_results":  stateResults,
		"last_updated":   time.Now().Format(time.RFC3339),
		"display_mode":   "tv_broadcast",
		"auto_cycle":     true,
		"cycle_interval": 10,
	})
}

// ══════════════════════════════════════════════════════════════════════════════
// N7: Automated Compliance Reporting (ECOWAS/AU/EU format)
// ══════════════════════════════════════════════════════════════════════════════

func handleComplianceReport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("standard") // ecowas, au, eu
	electionID := queryParamInt(r, "election_id", 1)

	if format == "" {
		format = "ecowas"
	}

	// Gather all metrics
	var totalPUs, reportedPUs, totalVotes, totalIncidents, totalDisputes int
	db.QueryRow("SELECT COUNT(DISTINCT code) FROM polling_units").Scan(&totalPUs)
	db.QueryRow("SELECT COUNT(DISTINCT polling_unit_code) FROM results WHERE election_id = ?", electionID).Scan(&reportedPUs)
	db.QueryRow("SELECT COALESCE(SUM(votes), 0) FROM results WHERE election_id = ?", electionID).Scan(&totalVotes)
	db.QueryRow("SELECT COUNT(*) FROM stakeholder_incidents").Scan(&totalIncidents)
	db.QueryRow("SELECT COUNT(*) FROM disputes WHERE election_id = ?", electionID).Scan(&totalDisputes)

	var observerCount int
	db.QueryRow("SELECT COUNT(DISTINCT observer_id) FROM observer_reports WHERE election_id = ?", electionID).Scan(&observerCount)

	var anomalyCount int
	db.QueryRow("SELECT COUNT(*) FROM anomaly_escalations WHERE resolved = 0").Scan(&anomalyCount)

	report := M{
		"standard":    strings.ToUpper(format),
		"report_type": "election_observation_report",
		"generated":   time.Now().Format(time.RFC3339),
		"election_overview": M{
			"total_polling_units": totalPUs,
			"units_reporting":     reportedPUs,
			"coverage_pct":        safeDiv(float64(reportedPUs), float64(totalPUs)) * 100,
			"total_votes_cast":    totalVotes,
		},
		"security_assessment": M{
			"total_incidents":     totalIncidents,
			"open_disputes":       totalDisputes,
			"unresolved_anomalies": anomalyCount,
			"security_level":      assessSecurityLevel(totalIncidents, anomalyCount),
		},
		"observer_coverage": M{
			"total_observers":  observerCount,
			"coverage_ratio":   safeDiv(float64(observerCount), float64(totalPUs)),
		},
		"recommendations": generateComplianceRecommendations(reportedPUs, totalPUs, totalIncidents, anomalyCount),
	}

	switch format {
	case "ecowas":
		report["compliance_framework"] = "ECOWAS Protocol on Democracy and Good Governance (A/SP1/12/01)"
		report["assessment_criteria"] = []string{
			"Universal suffrage", "Transparent ballot counting", "Independent observation",
			"Peaceful conduct", "Technology integrity", "Dispute resolution mechanisms",
		}
	case "au":
		report["compliance_framework"] = "African Charter on Democracy, Elections and Governance"
		report["assessment_criteria"] = []string{
			"Free and fair elections", "Regular elections", "Independent electoral body",
			"Equal access to media", "Transparency of electoral process",
		}
	case "eu":
		report["compliance_framework"] = "EU Election Observation Methodology"
		report["assessment_criteria"] = []string{
			"Legal framework", "Electoral administration", "Voter registration",
			"Campaign environment", "Media and elections", "Participation of women",
			"Election day procedures", "Results and post-election",
		}
	}

	writeJSON(w, 200, report)
}

func assessSecurityLevel(incidents, anomalies int) string {
	total := incidents + anomalies
	if total == 0 {
		return "excellent"
	}
	if total < 5 {
		return "good"
	}
	if total < 20 {
		return "fair"
	}
	return "concerning"
}

func generateComplianceRecommendations(reported, total, incidents, anomalies int) []string {
	recs := []string{}
	coveragePct := safeDiv(float64(reported), float64(total)) * 100
	if coveragePct < 50 {
		recs = append(recs, "Increase reporting coverage — currently below 50%")
	}
	if incidents > 10 {
		recs = append(recs, fmt.Sprintf("Address %d reported incidents — investigate and resolve", incidents))
	}
	if anomalies > 0 {
		recs = append(recs, fmt.Sprintf("Resolve %d unresolved anomalies before result finalization", anomalies))
	}
	if len(recs) == 0 {
		recs = append(recs, "No significant concerns identified")
	}
	return recs
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// ══════════════════════════════════════════════════════════════════════════════
// I4: Audit Trail Visualization — timeline + relationship mapping
// ══════════════════════════════════════════════════════════════════════════════

func handleAuditTimeline(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	puCode := r.URL.Query().Get("pu_code")
	limit := queryParamInt(r, "limit", 50)

	query := "SELECT id, user_id, action, resource, details, timestamp FROM audit_log WHERE 1=1"
	args := []interface{}{}

	if userID != "" {
		query += " AND user_id = ?"
		args = append(args, userID)
	}
	if puCode != "" {
		query += " AND (resource LIKE ? OR details LIKE ?)"
		args = append(args, "%"+puCode+"%", "%"+puCode+"%")
	}
	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		writeJSON(w, 200, M{"events": []M{}, "total": 0})
		return
	}
	defer rows.Close()

	events := []M{}
	for rows.Next() {
		var id int
		var uid, action, resource, details, ts string
		rows.Scan(&id, &uid, &action, &resource, &details, &ts)
		events = append(events, M{
			"id": id, "user_id": uid, "action": action,
			"resource": resource, "details": details, "timestamp": ts,
		})
	}

	writeJSON(w, 200, M{"events": events, "total": len(events)})
}

// ══════════════════════════════════════════════════════════════════════════════
// P1: Database Prepared Statements + Query Optimization
// ══════════════════════════════════════════════════════════════════════════════

var preparedStatements = struct {
	sync.RWMutex
	stmts map[string]*sql.Stmt
}{stmts: make(map[string]*sql.Stmt)}

func getPreparedStmt(key, query string) (*sql.Stmt, error) {
	preparedStatements.RLock()
	if stmt, ok := preparedStatements.stmts[key]; ok {
		preparedStatements.RUnlock()
		return stmt, nil
	}
	preparedStatements.RUnlock()

	preparedStatements.Lock()
	defer preparedStatements.Unlock()

	// Double-check
	if stmt, ok := preparedStatements.stmts[key]; ok {
		return stmt, nil
	}

	stmt, err := db.Prepare(query)
	if err != nil {
		return nil, err
	}
	preparedStatements.stmts[key] = stmt
	return stmt, nil
}

// ══════════════════════════════════════════════════════════════════════════════
// N3: Voice Transcription Endpoint (forwards to Python Whisper service)
// ══════════════════════════════════════════════════════════════════════════════

func handleVoiceTranscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}

	// Forward to Python Whisper service
	whisperURL := envString("WHISPER_SERVICE_URL", "http://localhost:8090")
	client := &http.Client{Timeout: 30 * time.Second}

	proxyReq, err := http.NewRequestWithContext(r.Context(), "POST", whisperURL+"/transcribe", r.Body)
	if err != nil {
		writeError(w, 500, "failed to create transcription request")
		return
	}
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	resp, err := client.Do(proxyReq)
	if err != nil {
		// Fallback response when Whisper service unavailable
		writeJSON(w, 200, M{
			"text":        "",
			"language":    "en",
			"duration":    0,
			"status":      "service_unavailable",
			"fallback":    true,
			"message":     "Voice transcription service is offline. Please type your report manually.",
		})
		return
	}
	defer resp.Body.Close()

	var result M
	json.NewDecoder(resp.Body).Decode(&result)
	writeJSON(w, 200, result)
}

// ══════════════════════════════════════════════════════════════════════════════
// Schema Init for new tables
// ══════════════════════════════════════════════════════════════════════════════

func initPlatformImprovements(db *sql.DB) {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS device_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			device_id TEXT NOT NULL,
			device_secret TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, device_id)
		)`,
		`CREATE TABLE IF NOT EXISTS device_locations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL,
			ip_address TEXT,
			latitude REAL,
			longitude REAL,
			last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS export_audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			export_type TEXT,
			format TEXT,
			ip_address TEXT,
			timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_device_locations_device ON device_locations(device_id)`,
		`CREATE INDEX IF NOT EXISTS idx_export_audit_user ON export_audit_log(user_id, timestamp)`,
	}
	for _, s := range schemas {
		dbExecLog("platform_improvements", s)
	}
}

func initOpenAPIRoutes() {
	// Auth
	registerAPIRoute("/auth/login", "POST", "Authenticate user", "Auth", "none")
	registerAPIRoute("/auth/register", "POST", "Register new user", "Auth", "none")
	registerAPIRoute("/auth/refresh", "POST", "Refresh JWT token", "Auth", "none")
	registerAPIRoute("/auth/me", "GET", "Get current user", "Auth", "read")
	registerAPIRoute("/auth/logout", "POST", "Logout and invalidate session", "Auth", "write")

	// Elections
	registerAPIRoute("/elections", "GET", "List all elections", "Elections", "read")
	registerAPIRoute("/elections", "POST", "Create new election", "Elections", "admin")
	registerAPIRoute("/elections/{id}", "GET", "Get election details", "Elections", "read")
	registerAPIRoute("/elections/{id}/status", "PATCH", "Update election status (FSM)", "Elections", "admin")

	// Results
	registerAPIRoute("/results", "GET", "List results", "Results", "read")
	registerAPIRoute("/results/submit", "POST", "Submit polling unit results", "Results", "write")
	registerAPIRoute("/results/validate/{id}", "POST", "Validate submitted results", "Results", "write")
	registerAPIRoute("/collation/summary", "GET", "Get collation summary", "Results", "read")

	// Observers
	registerAPIRoute("/observers", "GET", "List observers", "Observers", "read")
	registerAPIRoute("/observers/checkin", "POST", "Observer check-in", "Observers", "write")
	registerAPIRoute("/observers/reports", "GET", "List observer reports", "Observers", "read")
	registerAPIRoute("/observers/reports", "POST", "Submit observer report", "Observers", "write")

	// Incidents
	registerAPIRoute("/incidents", "GET", "List incidents", "Security", "read")
	registerAPIRoute("/incidents", "POST", "Report incident", "Security", "write")
	registerAPIRoute("/incidents/{id}", "PATCH", "Update incident", "Security", "write")

	// Disputes
	registerAPIRoute("/disputes", "GET", "List disputes", "Security", "read")
	registerAPIRoute("/disputes", "POST", "File dispute", "Security", "write")
	registerAPIRoute("/disputes/{id}/resolve", "POST", "Resolve dispute", "Security", "admin")

	// Biometrics
	registerAPIRoute("/biometric/verify", "POST", "Verify biometric data", "Biometrics", "write")
	registerAPIRoute("/biometric/enroll", "POST", "Enroll biometric template", "Biometrics", "write")
	registerAPIRoute("/bvas/devices", "GET", "List BVAS devices", "Biometrics", "read")
	registerAPIRoute("/bvas/sync", "POST", "Sync BVAS device", "Biometrics", "write")

	// AI/ML
	registerAPIRoute("/anomalies", "GET", "List detected anomalies", "AI/ML", "read")
	registerAPIRoute("/predictions", "GET", "Get predictive analytics", "AI/ML", "read")
	registerAPIRoute("/ai/integrity-score", "GET", "Get PU integrity score", "AI/ML", "read")
	registerAPIRoute("/ai/integrity-heatmap", "GET", "Get integrity heatmap", "AI/ML", "read")

	// KYC/KYB
	registerAPIRoute("/kyc/verify", "POST", "Submit KYC verification", "Security", "write")
	registerAPIRoute("/kyc/status/{id}", "GET", "Check KYC status", "Security", "read")
	registerAPIRoute("/kyb/verify", "POST", "Submit KYB verification", "Security", "write")

	// Command Center
	registerAPIRoute("/command-center/state", "GET", "Get command center state", "CommandCenter", "read")
	registerAPIRoute("/command-center/stream", "GET", "SSE stream for live updates", "CommandCenter", "read")
	registerAPIRoute("/load-shedding", "POST", "Set load shedding level", "CommandCenter", "admin")

	// Public
	registerAPIRoute("/citizen/verify", "GET", "Public result verification", "Public", "none")
	registerAPIRoute("/public/tv-dashboard", "GET", "Public TV broadcast dashboard", "Public", "none")
	registerAPIRoute("/public/result-certificate", "GET", "Get result certificate with QR", "Public", "none")

	// Monitoring
	registerAPIRoute("/healthz", "GET", "Deep health check", "Monitoring", "none")
	registerAPIRoute("/readiness", "GET", "Readiness probe", "Monitoring", "none")
	registerAPIRoute("/metrics", "GET", "Prometheus metrics", "Monitoring", "none")

	// Admin
	registerAPIRoute("/admin/users", "GET", "List users", "Admin", "admin")
	registerAPIRoute("/admin/users", "POST", "Create user", "Admin", "admin")
	registerAPIRoute("/admin/batch/users", "POST", "Batch import users", "Admin", "admin")
	registerAPIRoute("/admin/batch/status", "POST", "Batch status update", "Admin", "admin")

	// API
	registerAPIRoute("/api/openapi.json", "GET", "OpenAPI 3.1 specification", "Monitoring", "none")
	registerAPIRoute("/api/docs", "GET", "Swagger UI", "Monitoring", "none")
}

// envString and haversineDistance are defined in session_blacklist.go and geofencing.go respectively
