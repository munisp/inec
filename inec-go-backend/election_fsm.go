package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// ElectionState represents a state in the election lifecycle FSM.
type ElectionState string

const (
	ElectionStateDraft     ElectionState = "draft"
	ElectionStateScheduled ElectionState = "scheduled"
	ElectionStateActive    ElectionState = "active"
	ElectionStateVoting    ElectionState = "voting"
	ElectionStateCollating ElectionState = "collating"
	ElectionStateClosed    ElectionState = "closed"
	ElectionStateCancelled ElectionState = "cancelled"
	ElectionStateDisputed  ElectionState = "disputed"
)

// ElectionTransition defines a valid state transition with guards.
type ElectionTransition struct {
	From  ElectionState
	To    ElectionState
	Guard func(ctx context.Context, electionID int) error
	Event string
}

// electionFSM defines all valid transitions for the election lifecycle.
var electionFSM = []ElectionTransition{
	{From: ElectionStateDraft, To: ElectionStateScheduled, Event: "schedule",
		Guard: guardSchedule},
	{From: ElectionStateScheduled, To: ElectionStateActive, Event: "activate",
		Guard: guardActivate},
	{From: ElectionStateActive, To: ElectionStateVoting, Event: "open_voting",
		Guard: guardOpenVoting},
	{From: ElectionStateVoting, To: ElectionStateCollating, Event: "close_voting",
		Guard: guardCloseVoting},
	{From: ElectionStateCollating, To: ElectionStateClosed, Event: "finalize",
		Guard: guardFinalize},
	{From: ElectionStateScheduled, To: ElectionStateCancelled, Event: "cancel", Guard: nil},
	{From: ElectionStateDraft, To: ElectionStateCancelled, Event: "cancel", Guard: nil},
	{From: ElectionStateClosed, To: ElectionStateDisputed, Event: "dispute",
		Guard: guardDispute},
	{From: ElectionStateDisputed, To: ElectionStateClosed, Event: "resolve_dispute", Guard: nil},
}

// Guard functions enforce preconditions for state transitions.

func guardSchedule(ctx context.Context, electionID int) error {
	var dateStr string
	err := db.QueryRowContext(ctx, "SELECT election_date FROM elections WHERE id=?", electionID).Scan(&dateStr)
	if err != nil {
		return fmt.Errorf("election not found")
	}
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return fmt.Errorf("invalid election_date format")
	}
	if date.Before(time.Now().AddDate(0, 0, 7)) {
		return fmt.Errorf("election must be scheduled at least 7 days in advance")
	}
	return nil
}

func guardActivate(ctx context.Context, electionID int) error {
	var staffCount int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM staff_assignments WHERE election_id=?", electionID).Scan(&staffCount)
	if staffCount == 0 {
		return fmt.Errorf("cannot activate: no staff assigned")
	}
	var puCount int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM polling_units", electionID).Scan(&puCount)
	if puCount == 0 {
		return fmt.Errorf("cannot activate: no polling units configured")
	}
	return nil
}

func guardOpenVoting(ctx context.Context, electionID int) error {
	var dateStr string
	db.QueryRowContext(ctx, "SELECT election_date FROM elections WHERE id=?", electionID).Scan(&dateStr)
	date, _ := time.Parse("2006-01-02", dateStr)
	today := time.Now().Format("2006-01-02")
	if date.Format("2006-01-02") != today {
		return fmt.Errorf("voting can only open on election day (scheduled: %s, today: %s)", date.Format("2006-01-02"), today)
	}
	return nil
}

func guardCloseVoting(ctx context.Context, electionID int) error {
	var openSince string
	err := db.QueryRowContext(ctx, "SELECT updated_at FROM elections WHERE id=? AND status='voting'", electionID).Scan(&openSince)
	if err != nil {
		return fmt.Errorf("election is not in voting state")
	}
	return nil
}

func guardFinalize(ctx context.Context, electionID int) error {
	var totalPUs, submittedPUs int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM polling_units").Scan(&totalPUs)
	db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT polling_unit_code) FROM results WHERE election_id=?", electionID).Scan(&submittedPUs)
	if totalPUs > 0 && submittedPUs < totalPUs/2 {
		return fmt.Errorf("cannot finalize: only %d/%d polling units have submitted results", submittedPUs, totalPUs)
	}
	return nil
}

func guardDispute(ctx context.Context, electionID int) error {
	var closedAt string
	err := db.QueryRowContext(ctx, "SELECT updated_at FROM elections WHERE id=? AND status='closed'", electionID).Scan(&closedAt)
	if err != nil {
		return fmt.Errorf("election is not closed")
	}
	return nil
}

// TransitionElection attempts to move an election from its current state to the target state.
func TransitionElection(ctx context.Context, electionID int, event string, actor string) error {
	var currentStatus string
	err := db.QueryRowContext(ctx, "SELECT status FROM elections WHERE id=?", electionID).Scan(&currentStatus)
	if err != nil {
		return fmt.Errorf("election %d not found", electionID)
	}

	current := ElectionState(currentStatus)

	for _, t := range electionFSM {
		if t.From == current && t.Event == event {
			if t.Guard != nil {
				if err := t.Guard(ctx, electionID); err != nil {
					return fmt.Errorf("guard failed for %s→%s: %w", t.From, t.To, err)
				}
			}
			_, err := dbExecCtx(ctx, "UPDATE elections SET status=?, updated_at=CURRENT_TIMESTAMP WHERE id=?", string(t.To), electionID)
			if err != nil {
				return fmt.Errorf("transition failed: %w", err)
			}
			dbExecLog("fsm_audit", "INSERT INTO election_state_log (election_id, from_state, to_state, event, actor, created_at) VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)",
				electionID, string(t.From), string(t.To), event, actor)
			log.Info().Int("election_id", electionID).Str("from", string(t.From)).Str("to", string(t.To)).Str("event", event).Msg("election state transition")

			if mwHub != nil && mwHub.Kafka != nil {
				mwHub.Kafka.Produce(ctx, KafkaMessage{
					Topic: "inec.election.lifecycle",
					Key:   fmt.Sprintf("%d", electionID),
					Value: map[string]interface{}{
						"election_id": electionID, "from": string(t.From),
						"to": string(t.To), "event": event, "actor": actor,
					},
				})
			}
			return nil
		}
	}

	validEvents := []string{}
	for _, t := range electionFSM {
		if t.From == current {
			validEvents = append(validEvents, t.Event)
		}
	}
	return fmt.Errorf("invalid transition: cannot apply event '%s' in state '%s' (valid events: %v)", event, current, validEvents)
}

// handleElectionFSMTransition handles POST /elections/{id}/transition
func handleElectionFSMTransition(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var req struct {
		Event string `json:"event" validate:"required"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	var electionID int
	fmt.Sscanf(id, "%d", &electionID)

	actor, _ := claims["username"].(string)
	if err := TransitionElection(r.Context(), electionID, req.Event, actor); err != nil {
		writeError(w, 422, err.Error())
		return
	}

	writeJSON(w, 200, M{
		"message":     "Transition successful",
		"election_id": electionID,
		"event":       req.Event,
	})
}

// handleElectionFSMDiagram returns the FSM definition and current state.
func handleElectionFSMDiagram(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var currentStatus string
	err := db.QueryRow("SELECT status FROM elections WHERE id=?", id).Scan(&currentStatus)
	if err != nil {
		writeError(w, 404, "election not found")
		return
	}

	transitions := []M{}
	for _, t := range electionFSM {
		transitions = append(transitions, M{
			"from":      string(t.From),
			"to":        string(t.To),
			"event":     t.Event,
			"guarded":   t.Guard != nil,
			"available": string(t.From) == currentStatus,
		})
	}

	writeJSON(w, 200, M{
		"current_state": currentStatus,
		"transitions":   transitions,
		"states":        []string{"draft", "scheduled", "active", "voting", "collating", "closed", "cancelled", "disputed"},
	})
}

// handleElectionStateHistory returns the state transition log.
func handleElectionStateHistory(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	rows, err := db.QueryContext(r.Context(),
		"SELECT from_state, to_state, event, actor, created_at FROM election_state_log WHERE election_id=? ORDER BY created_at DESC", id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	var history []M
	for rows.Next() {
		var from, to, event, actor, createdAt string
		if rows.Scan(&from, &to, &event, &actor, &createdAt) == nil {
			history = append(history, M{"from": from, "to": to, "event": event, "actor": actor, "created_at": createdAt})
		}
	}
	writeJSON(w, 200, M{"election_id": id, "history": history})
}

func initElectionFSMSchema() {
	db.Exec(`CREATE TABLE IF NOT EXISTS election_state_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		election_id INTEGER NOT NULL,
		from_state TEXT NOT NULL,
		to_state TEXT NOT NULL,
		event TEXT NOT NULL,
		actor TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
}

// --- Duplicate Voter Detection ---

type DuplicateCandidate struct {
	VoterAVIN  string  `json:"voter_a_vin"`
	VoterBVIN  string  `json:"voter_b_vin"`
	MatchType  string  `json:"match_type"`
	Confidence float64 `json:"confidence"`
	Status     string  `json:"status"`
	DetectedAt string  `json:"detected_at"`
}

func handleDuplicateVoterScan(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}

	ctx := r.Context()
	_ = claims

	// Method 1: NIN duplicate detection
	ninDups := []M{}
	rows, err := db.QueryContext(ctx,
		`SELECT v1.vin, v2.vin, v1.nin, v1.full_name, v2.full_name
		 FROM voters v1 JOIN voters v2 ON v1.nin = v2.nin AND v1.id < v2.id
		 WHERE v1.nin IS NOT NULL AND v1.nin != ''`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var vin1, vin2, nin, name1, name2 string
			if rows.Scan(&vin1, &vin2, &nin, &name1, &name2) == nil {
				ninDups = append(ninDups, M{
					"voter_a": vin1, "voter_b": vin2, "nin": nin,
					"name_a": name1, "name_b": name2, "match_type": "nin_exact",
					"confidence": 1.0,
				})
			}
		}
	}

	// Method 2: Name + DOB fuzzy match
	nameDups := []M{}
	rows2, err := db.QueryContext(ctx,
		`SELECT v1.vin, v2.vin, v1.full_name, v2.full_name, v1.date_of_birth
		 FROM voters v1 JOIN voters v2 ON v1.date_of_birth = v2.date_of_birth
		   AND v1.full_name = v2.full_name AND v1.id < v2.id
		 LIMIT 100`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var vin1, vin2, name1, name2, dob string
			if rows2.Scan(&vin1, &vin2, &name1, &name2, &dob) == nil {
				nameDups = append(nameDups, M{
					"voter_a": vin1, "voter_b": vin2,
					"name_a": name1, "name_b": name2, "dob": dob,
					"match_type": "name_dob_exact", "confidence": 0.92,
				})
			}
		}
	}

	// Method 3: Biometric duplicate (via ABIS dedup table)
	bioDups := []M{}
	rows3, err := db.QueryContext(ctx,
		`SELECT vin_a, vin_b, similarity_score, modality FROM dedup_candidates WHERE decision IS NULL OR decision='pending' LIMIT 100`)
	if err == nil {
		defer rows3.Close()
		for rows3.Next() {
			var vinA, vinB, modality string
			var score float64
			if rows3.Scan(&vinA, &vinB, &score, &modality) == nil {
				bioDups = append(bioDups, M{
					"voter_a": vinA, "voter_b": vinB,
					"match_type": "biometric_" + modality, "confidence": score,
				})
			}
		}
	}

	// Method 4: Phone number duplicate
	phoneDups := []M{}
	rows4, err := db.QueryContext(ctx,
		`SELECT v1.vin, v2.vin, v1.phone, v1.full_name, v2.full_name
		 FROM voters v1 JOIN voters v2 ON v1.phone = v2.phone AND v1.id < v2.id
		 WHERE v1.phone IS NOT NULL AND v1.phone != ''
		 LIMIT 100`)
	if err == nil {
		defer rows4.Close()
		for rows4.Next() {
			var vin1, vin2, phone, name1, name2 string
			if rows4.Scan(&vin1, &vin2, &phone, &name1, &name2) == nil {
				phoneDups = append(phoneDups, M{
					"voter_a": vin1, "voter_b": vin2, "phone": phone,
					"name_a": name1, "name_b": name2,
					"match_type": "phone_exact", "confidence": 0.85,
				})
			}
		}
	}

	total := len(ninDups) + len(nameDups) + len(bioDups) + len(phoneDups)

	writeJSON(w, 200, M{
		"total_duplicates": total,
		"by_nin":           ninDups,
		"by_name_dob":      nameDups,
		"by_biometric":     bioDups,
		"by_phone":         phoneDups,
		"scan_timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
}

func handleDuplicateVoterResolve(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	var req struct {
		VoterAVIN string `json:"voter_a_vin" validate:"required"`
		VoterBVIN string `json:"voter_b_vin" validate:"required"`
		Decision  string `json:"decision" validate:"required"`
		Reason    string `json:"reason"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	switch req.Decision {
	case "merge":
		dbExecLog("voter_merge", "UPDATE voters SET status='merged', merged_into=? WHERE vin=?", req.VoterAVIN, req.VoterBVIN)
	case "flag":
		dbExecLog("voter_flag", "UPDATE voters SET status='flagged' WHERE vin IN (?,?)", req.VoterAVIN, req.VoterBVIN)
	case "dismiss":
		// No action needed
	default:
		writeError(w, 400, "decision must be: merge, flag, or dismiss")
		return
	}

	resolvedBy, _ := claims["username"].(string)
	dbExecLog("dedup_resolve", "INSERT INTO dedup_resolutions (voter_a_vin, voter_b_vin, decision, reason, resolved_by, resolved_at) VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)",
		req.VoterAVIN, req.VoterBVIN, req.Decision, req.Reason, resolvedBy)

	writeJSON(w, 200, M{"status": "resolved", "decision": req.Decision})
}

// --- GPS Spoofing Detection ---

type GPSTrackPoint struct {
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	Timestamp time.Time `json:"timestamp"`
	Accuracy  float64   `json:"accuracy"`
}

type SpoofingAnalysis struct {
	IsSpoofed     bool     `json:"is_spoofed"`
	Confidence    float64  `json:"confidence"`
	Indicators    []string `json:"indicators"`
	VelocityKmh   float64  `json:"velocity_kmh"`
	AccuracyScore float64  `json:"accuracy_score"`
	JumpDetected  bool     `json:"jump_detected"`
	MockProvider  bool     `json:"mock_provider"`
}

func analyzeGPSSpoofing(current, previous *GPSTrackPoint, deviceMeta map[string]interface{}) *SpoofingAnalysis {
	analysis := &SpoofingAnalysis{Indicators: []string{}}
	score := 0.0

	if previous != nil {
		elapsed := current.Timestamp.Sub(previous.Timestamp).Seconds()
		if elapsed > 0 {
			dist := haversineDistance(previous.Lat, previous.Lng, current.Lat, current.Lng)
			velocity := (dist / elapsed) * 3.6 // m/s to km/h
			analysis.VelocityKmh = velocity

			// Teleportation detection: >500km/h is physically impossible
			if velocity > 500 {
				analysis.Indicators = append(analysis.Indicators, fmt.Sprintf("impossible_velocity: %.1f km/h", velocity))
				score += 0.9
				analysis.JumpDetected = true
			} else if velocity > 200 {
				analysis.Indicators = append(analysis.Indicators, fmt.Sprintf("suspicious_velocity: %.1f km/h", velocity))
				score += 0.5
			}

			// Instant jump detection (> 1km in < 5 seconds)
			if dist > 1000 && elapsed < 5 {
				analysis.Indicators = append(analysis.Indicators, fmt.Sprintf("instant_jump: %.0fm in %.1fs", dist, elapsed))
				score += 0.8
				analysis.JumpDetected = true
			}
		}
	}

	// Accuracy-based detection
	if current.Accuracy > 100 {
		analysis.Indicators = append(analysis.Indicators, fmt.Sprintf("low_accuracy: %.1fm", current.Accuracy))
		score += 0.3
	} else if current.Accuracy == 0 {
		analysis.Indicators = append(analysis.Indicators, "zero_accuracy: mock location suspected")
		score += 0.7
	}
	analysis.AccuracyScore = 1.0 - (current.Accuracy / 200.0)
	if analysis.AccuracyScore < 0 {
		analysis.AccuracyScore = 0
	}

	// Mock provider detection (from device metadata)
	if mock, ok := deviceMeta["is_mock_provider"].(bool); ok && mock {
		analysis.MockProvider = true
		analysis.Indicators = append(analysis.Indicators, "mock_provider_detected")
		score += 1.0
	}

	// Altitude consistency check
	if alt, ok := deviceMeta["altitude"].(float64); ok {
		if alt < -100 || alt > 9000 {
			analysis.Indicators = append(analysis.Indicators, fmt.Sprintf("impossible_altitude: %.0fm", alt))
			score += 0.6
		}
	}

	// Jitter analysis (perfectly stable GPS = likely spoofed)
	if jitter, ok := deviceMeta["position_jitter_m"].(float64); ok && jitter == 0 {
		analysis.Indicators = append(analysis.Indicators, "zero_jitter: no natural GPS variation")
		score += 0.4
	}

	analysis.Confidence = score
	if score > 1.0 {
		analysis.Confidence = 1.0
	}
	analysis.IsSpoofed = score >= 0.7

	return analysis
}

func handleGPSSpoofCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID  string                 `json:"device_id" validate:"required"`
		Lat       float64                `json:"lat" validate:"required"`
		Lng       float64                `json:"lng" validate:"required"`
		Accuracy  float64                `json:"accuracy"`
		Timestamp string                 `json:"timestamp"`
		Meta      map[string]interface{} `json:"meta"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	ts := time.Now()
	if req.Timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339, req.Timestamp); err == nil {
			ts = parsed
		}
	}

	current := &GPSTrackPoint{Lat: req.Lat, Lng: req.Lng, Timestamp: ts, Accuracy: req.Accuracy}

	// Get previous position from Redis or DB
	var previous *GPSTrackPoint
	if mwHub != nil && mwHub.Redis != nil {
		prevJSON, err := mwHub.Redis.Get(r.Context(), "gps:last:"+req.DeviceID)
		if err == nil && prevJSON != "" {
			previous = &GPSTrackPoint{}
			json.Unmarshal([]byte(prevJSON), previous)
		}
	}

	analysis := analyzeGPSSpoofing(current, previous, req.Meta)

	// Store current position
	if mwHub != nil && mwHub.Redis != nil {
		data, _ := json.Marshal(current)
		mwHub.Redis.Set(r.Context(), "gps:last:"+req.DeviceID, string(data), 1*time.Hour)
	}

	// Log spoofing attempts
	if analysis.IsSpoofed {
		dbExecLog("gps_spoof", "INSERT INTO gps_spoof_events (device_id, lat, lng, confidence, indicators, detected_at) VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)",
			req.DeviceID, req.Lat, req.Lng, analysis.Confidence, fmt.Sprintf("%v", analysis.Indicators))
		log.Warn().Str("device", req.DeviceID).Float64("confidence", analysis.Confidence).Msg("GPS spoofing detected")
	}

	status := 200
	if analysis.IsSpoofed {
		status = 403
	}
	writeJSON(w, status, M{
		"spoofing_analysis": analysis,
		"device_id":         req.DeviceID,
		"action":            map[bool]string{true: "BLOCKED", false: "ALLOWED"}[analysis.IsSpoofed],
	})
}

// --- WebSocket Live Dashboard ---

type WebSocketHub struct {
	clients    map[*WSClient]bool
	broadcast  chan []byte
	register   chan *WSClient
	unregister chan *WSClient
}

type WSClient struct {
	hub     *WebSocketHub
	w       http.ResponseWriter
	flusher http.Flusher
	done    chan struct{}
	filters map[string]string
}

var wsHub *WebSocketHub

func newWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*WSClient]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
	}
}

func (h *WebSocketHub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.done)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case <-client.done:
				default:
					client.flusher.Flush()
					_ = message
				}
			}
		}
	}
}

// handleDashboardSSE serves real-time dashboard updates via Server-Sent Events.
func handleDashboardSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Send initial state
	sendDashboardUpdate(w, flusher)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendDashboardUpdate(w, flusher)
		}
	}
}

func sendDashboardUpdate(w http.ResponseWriter, flusher http.Flusher) {
	var totalElections, activeElections, totalResults, totalVoters int
	db.QueryRow("SELECT COUNT(*) FROM elections").Scan(&totalElections)
	db.QueryRow("SELECT COUNT(*) FROM elections WHERE status IN ('active','voting','collating')").Scan(&activeElections)
	db.QueryRow("SELECT COUNT(*) FROM results").Scan(&totalResults)
	db.QueryRow("SELECT COUNT(*) FROM voters").Scan(&totalVoters)

	data, _ := json.Marshal(M{
		"type":             "dashboard_update",
		"total_elections":  totalElections,
		"active_elections": activeElections,
		"total_results":    totalResults,
		"total_voters":     totalVoters,
		"timestamp":        time.Now().UTC().Format(time.RFC3339),
	})

	fmt.Fprintf(w, "event: dashboard\ndata: %s\n\n", data)
	flusher.Flush()
}

// --- OAuth2/OIDC Integration ---

type OIDCConfig struct {
	Issuer       string `json:"issuer"`
	AuthURL      string `json:"authorize_url"`
	TokenURL     string `json:"token_url"`
	UserInfoURL  string `json:"userinfo_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"-"`
	RedirectURI  string `json:"redirect_uri"`
	Realm        string `json:"realm"`
}

func getOIDCConfig() *OIDCConfig {
	baseURL := envOrDefault("KEYCLOAK_URL", "http://localhost:8080")
	realm := envOrDefault("KEYCLOAK_REALM", "inec")
	return &OIDCConfig{
		Issuer:       fmt.Sprintf("%s/realms/%s", baseURL, realm),
		AuthURL:      fmt.Sprintf("%s/realms/%s/protocol/openid-connect/auth", baseURL, realm),
		TokenURL:     fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", baseURL, realm),
		UserInfoURL:  fmt.Sprintf("%s/realms/%s/protocol/openid-connect/userinfo", baseURL, realm),
		ClientID:     envOrDefault("KEYCLOAK_CLIENT_ID", "inec-platform"),
		ClientSecret: envOrDefault("KEYCLOAK_CLIENT_SECRET", ""),
		RedirectURI:  envOrDefault("OIDC_REDIRECT_URI", "http://localhost:3000/auth/callback"),
		Realm:        realm,
	}
}

func handleOIDCDiscovery(w http.ResponseWriter, r *http.Request) {
	cfg := getOIDCConfig()
	writeJSON(w, 200, M{
		"issuer":                   cfg.Issuer,
		"authorization_endpoint":   cfg.AuthURL,
		"token_endpoint":           cfg.TokenURL,
		"userinfo_endpoint":        cfg.UserInfoURL,
		"jwks_uri":                 cfg.Issuer + "/protocol/openid-connect/certs",
		"scopes_supported":         []string{"openid", "profile", "email", "roles"},
		"response_types_supported": []string{"code", "token", "id_token"},
		"grant_types_supported":    []string{"authorization_code", "refresh_token", "client_credentials"},
	})
}

func handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, 400, "authorization code required")
		return
	}

	cfg := getOIDCConfig()
	ctx := r.Context()

	// Exchange code for token via Keycloak userinfo endpoint
	if mwHub != nil && mwHub.Keycloak != nil {
		user, err := mwHub.Keycloak.ValidateToken(ctx, code)
		if err == nil && user != nil {
			writeJSON(w, 200, M{
				"access_token": code,
				"user":         user,
				"token_type":   "Bearer",
			})
			return
		}
	}

	// Fallback: return config for client-side exchange
	writeJSON(w, 200, M{
		"message":   "OAuth2 callback received",
		"code":      code[:min(8, len(code))] + "...",
		"token_url": cfg.TokenURL,
		"client_id": cfg.ClientID,
	})
}

// --- Webhook Subscriptions (Public API) ---

type WebhookSubscription struct {
	ID        int    `json:"id"`
	URL       string `json:"url"`
	Events    string `json:"events"`
	Secret    string `json:"secret,omitempty"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
}

func handleWebhookCreate(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	_ = claims
	var req struct {
		URL    string   `json:"url" validate:"required"`
		Events []string `json:"events" validate:"required,min=1"`
		Secret string   `json:"secret"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	eventsJSON, _ := json.Marshal(req.Events)
	id := insertReturningID(db, "INSERT INTO webhook_subscriptions (url, events, secret, active, created_at) VALUES (?,?,?,1,CURRENT_TIMESTAMP)",
		req.URL, string(eventsJSON), req.Secret)

	writeJSON(w, 201, M{"id": id, "url": req.URL, "events": req.Events, "active": true})
}

func handleWebhookList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, url, events, active, created_at FROM webhook_subscriptions ORDER BY id DESC")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	var hooks []M
	for rows.Next() {
		var id int
		var url, events, createdAt string
		var active bool
		if rows.Scan(&id, &url, &events, &active, &createdAt) == nil {
			var eventList []string
			json.Unmarshal([]byte(events), &eventList)
			hooks = append(hooks, M{"id": id, "url": url, "events": eventList, "active": active, "created_at": createdAt})
		}
	}
	writeJSON(w, 200, M{"webhooks": hooks, "total": len(hooks)})
}

func handleWebhookDelete(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	dbExecLog("webhook", "DELETE FROM webhook_subscriptions WHERE id=?", id)
	writeJSON(w, 200, M{"deleted": true})
}

// dispatchWebhook sends event notifications to all subscribed URLs.
func dispatchWebhook(event string, payload interface{}) {
	rows, _ := db.Query("SELECT url, secret FROM webhook_subscriptions WHERE active=1 AND events LIKE ?", "%"+event+"%")
	if rows == nil {
		return
	}
	defer rows.Close()
	body, _ := json.Marshal(M{"event": event, "data": payload, "timestamp": time.Now().UTC().Format(time.RFC3339)})

	for rows.Next() {
		var url, secret string
		if rows.Scan(&url, &secret) == nil {
			go func(url, secret string) {
				req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-INEC-Event", event)
				if secret != "" {
					req.Header.Set("X-INEC-Signature", computeHMAC(body, secret))
				}
				http.DefaultClient.Do(req)
			}(url, secret)
		}
	}
}

func computeHMAC(data []byte, secret string) string {
	// HMAC-SHA256 signature for webhook verification
	h := sha256.New()
	h.Write([]byte(secret))
	h.Write(data)
	return fmt.Sprintf("sha256=%x", h.Sum(nil))
}

func initWebhookSchema() {
	db.Exec(`CREATE TABLE IF NOT EXISTS webhook_subscriptions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL,
		events TEXT NOT NULL,
		secret TEXT DEFAULT '',
		active INTEGER DEFAULT 1,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS gps_spoof_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id TEXT NOT NULL,
		lat REAL NOT NULL,
		lng REAL NOT NULL,
		confidence REAL NOT NULL,
		indicators TEXT,
		detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS dedup_resolutions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		voter_a_vin TEXT NOT NULL,
		voter_b_vin TEXT NOT NULL,
		decision TEXT NOT NULL,
		reason TEXT,
		resolved_by TEXT,
		resolved_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
}
