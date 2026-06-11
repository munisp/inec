// V2 HTTP handlers for GOTV enhanced features.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"inec-go-backend/internal/gotv"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// getPartyID extracts the authenticated party ID from the request header set by auth middleware.
func getPartyID(r *http.Request) int {
	pid, _ := strconv.Atoi(r.Header.Get("X-GOTV-Party-ID"))
	return pid
}

// ─── Campaign Launch V2 (Kafka-backed) ─────────────────────────────────────

func handleLaunchCampaignV2(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	campaignID := mux.Vars(r)["id"]

	if err := dispatcher.LaunchCampaignV2(r.Context(), campaignID, partyID, kafkaDisp); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "launched", "campaign_id": campaignID, "engine": "kafka_v2"})
}

// ─── Campaign Scheduling ────────────────────────────────────────────────────

func handleScheduleCampaign(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	campaignID := mux.Vars(r)["id"]

	var req struct {
		ScheduledAt string `json:"scheduled_at"` // RFC3339
		Recurring   string `json:"recurring"`    // daily, weekly, ""
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	scheduledAt, err := time.Parse(time.RFC3339, req.ScheduledAt)
	if err != nil {
		http.Error(w, `{"error":"scheduled_at must be RFC3339 format"}`, http.StatusBadRequest)
		return
	}

	sc := gotv.ScheduledCampaign{
		CampaignID:  campaignID,
		PartyID:     partyID,
		ScheduledAt: scheduledAt,
		Recurring:   req.Recurring,
	}

	if err := scheduler.ScheduleCampaign(r.Context(), sc); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "scheduled",
		"campaign_id":  campaignID,
		"scheduled_at": scheduledAt.Format(time.RFC3339),
		"recurring":    req.Recurring,
	})
}

// ─── Campaign Budget ────────────────────────────────────────────────────────

func handleCampaignBudget(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	campaignID := mux.Vars(r)["id"]
	_ = partyID

	spent, cap, remaining, exceeded := dispatcher.CheckBudget(r.Context(), campaignID)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"campaign_id":   campaignID,
		"spent_kobo":    spent,
		"cap_kobo":      cap,
		"remaining_kobo": remaining,
		"exceeded":      exceeded,
	})
}

// ─── Campaign Sequences ─────────────────────────────────────────────────────

func handleCreateSequence(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var seq gotv.CampaignSequence
	if err := json.NewDecoder(r.Body).Decode(&seq); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	seq.PartyID = partyID
	seq.SequenceID = "seq-" + uuid.New().String()[:8]
	if err := dispatcher.CreateSequence(r.Context(), seq); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(seq)
}

func handleListSequences(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, err := svc.DB.QueryContext(r.Context(),
		`SELECT sequence_id, name, waves, status, created_at FROM gotv_campaign_sequences WHERE party_id=$1 ORDER BY created_at DESC`, partyID)
	if err != nil {
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var seqs []map[string]interface{}
	for rows.Next() {
		var id, name, wavesJSON, status string
		var createdAt time.Time
		rows.Scan(&id, &name, &wavesJSON, &status, &createdAt)
		seqs = append(seqs, map[string]interface{}{
			"sequence_id": id, "name": name, "waves": json.RawMessage(wavesJSON),
			"status": status, "created_at": createdAt,
		})
	}
	if seqs == nil {
		seqs = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(seqs)
}

func handleNextWave(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	seqID := mux.Vars(r)["id"]
	var req struct {
		WaveNumber int `json:"wave_number"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if err := dispatcher.ExecuteNextWave(r.Context(), seqID, partyID, req.WaveNumber); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "wave_executing"})
}

// ─── Contact Segments ───────────────────────────────────────────────────────

func handleCreateSegment(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var seg gotv.Segment
	if err := json.NewDecoder(r.Body).Decode(&seg); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	seg.PartyID = partyID
	seg.SegmentID = "seg-" + uuid.New().String()[:8]
	if err := dispatcher.SaveSegment(r.Context(), seg); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(seg)
}

func handleListSegments(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, err := svc.DB.QueryContext(r.Context(),
		`SELECT segment_id, name, filters, created_at FROM gotv_segments WHERE party_id=$1 ORDER BY created_at DESC`, partyID)
	if err != nil {
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var segs []map[string]interface{}
	for rows.Next() {
		var id, name, filtersJSON string
		var createdAt time.Time
		rows.Scan(&id, &name, &filtersJSON, &createdAt)
		segs = append(segs, map[string]interface{}{
			"segment_id": id, "name": name, "filters": json.RawMessage(filtersJSON), "created_at": createdAt,
		})
	}
	if segs == nil {
		segs = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"segments": segs})
}

func handleEvaluateSegment(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	segID := mux.Vars(r)["id"]

	var filtersJSON string
	var name string
	err := svc.DB.QueryRowContext(r.Context(),
		`SELECT name, filters FROM gotv_segments WHERE segment_id=$1 AND party_id=$2`, segID, partyID).Scan(&name, &filtersJSON)
	if err != nil {
		http.Error(w, `{"error":"segment not found"}`, http.StatusNotFound)
		return
	}

	var filters []gotv.SegmentFilter
	json.Unmarshal([]byte(filtersJSON), &filters)

	contacts, err := dispatcher.EvaluateSegment(r.Context(), gotv.Segment{
		SegmentID: segID, PartyID: partyID, Name: name, Filters: filters,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"segment_id": segID, "name": name, "count": len(contacts), "contact_ids": contacts,
	})
}

// ─── Volunteer Leaderboard ──────────────────────────────────────────────────

func handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	period := r.URL.Query().Get("period") // daily, weekly, monthly, all
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	entries, err := dispatcher.GetLeaderboard(r.Context(), partyID, period, limit)
	if err != nil {
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []gotv.LeaderboardEntry{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"entries": entries})
}

func handleCreateChallenge(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var req struct {
		Name          string `json:"name"`
		TargetMetric  string `json:"target_metric"` // doors_knocked, calls_made, rides_given
		TargetValue   int    `json:"target_value"`
		Reward        string `json:"reward_description"`
		StartsAt      string `json:"starts_at"`
		EndsAt        string `json:"ends_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	startsAt, _ := time.Parse(time.RFC3339, req.StartsAt)
	endsAt, _ := time.Parse(time.RFC3339, req.EndsAt)
	challengeID := "chal-" + uuid.New().String()[:8]

	svc.DB.ExecContext(r.Context(),
		`INSERT INTO gotv_challenges (challenge_id, party_id, name, target_metric, target_value, reward_description, starts_at, ends_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		challengeID, partyID, req.Name, req.TargetMetric, req.TargetValue, req.Reward, startsAt, endsAt)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"challenge_id": challengeID, "status": "created"})
}

func handleListChallenges(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, _ := svc.DB.QueryContext(r.Context(),
		`SELECT challenge_id, name, target_metric, target_value, reward_description, starts_at, ends_at
		 FROM gotv_challenges WHERE party_id=$1 ORDER BY starts_at DESC`, partyID)
	if rows == nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	defer rows.Close()

	var challenges []map[string]interface{}
	for rows.Next() {
		var id, name, metric, reward string
		var value int
		var starts, ends time.Time
		rows.Scan(&id, &name, &metric, &value, &reward, &starts, &ends)
		challenges = append(challenges, map[string]interface{}{
			"challenge_id": id, "name": name, "target_metric": metric,
			"target_value": value, "reward_description": reward,
			"starts_at": starts, "ends_at": ends,
		})
	}
	if challenges == nil {
		challenges = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(challenges)
}

// ─── Territory Assignment ───────────────────────────────────────────────────

func handleAssignTerritories(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var req struct {
		WardCode string `json:"ward_code"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.WardCode == "" {
		http.Error(w, `{"error":"ward_code required"}`, http.StatusBadRequest)
		return
	}
	territories, err := dispatcher.AssignTerritories(r.Context(), partyID, req.WardCode)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(territories)
}

func handleListTerritories(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, _ := svc.DB.QueryContext(r.Context(),
		`SELECT territory_id, volunteer_id, ward_code, contact_count, status FROM gotv_territories WHERE party_id=$1`, partyID)
	if rows == nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	defer rows.Close()

	var territories []map[string]interface{}
	for rows.Next() {
		var id, vol, ward, status string
		var count int
		rows.Scan(&id, &vol, &ward, &count, &status)
		territories = append(territories, map[string]interface{}{
			"territory_id": id, "volunteer_id": vol, "ward_code": ward, "contact_count": count, "status": status,
		})
	}
	if territories == nil {
		territories = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(territories)
}

// ─── Channel ROI ────────────────────────────────────────────────────────────

func handleChannelROI(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	roi, err := dispatcher.GetChannelROI(r.Context(), partyID)
	if err != nil {
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	if roi == nil {
		roi = []gotv.ChannelROI{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"channels": roi})
}

// ─── AI Variants ────────────────────────────────────────────────────────────

func handleAIGenerateVariants(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message     string `json:"message"`
		TargetState string `json:"target_state"`
		Channel     string `json:"channel"`
		Count       int    `json:"count"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Message == "" {
		http.Error(w, `{"error":"message required"}`, http.StatusBadRequest)
		return
	}
	if req.Count == 0 {
		req.Count = 3
	}

	variants, err := aiOptimizer.GenerateVariants(r.Context(), req.Message, req.TargetState, req.Channel, req.Count)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"variants": []string{req.Message}, "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"variants": variants, "count": len(variants)})
}

// ─── WhatsApp Button Reply ──────────────────────────────────────────────────

func handleWhatsAppButtonReply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From      string `json:"from"`
		ButtonID  string `json:"button_id"`
		ContactID string `json:"contact_id"`
		PartyID   int    `json:"party_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.ButtonID == "" || req.ContactID == "" {
		http.Error(w, `{"error":"button_id and contact_id required"}`, http.StatusBadRequest)
		return
	}
	dispatcher.ProcessWhatsAppButtonReply(r.Context(), req.From, req.ButtonID, req.ContactID, req.PartyID)
	json.NewEncoder(w).Encode(map[string]string{"status": "processed"})
}

// ─── WhatsApp Flows ─────────────────────────────────────────────────────────

func handleSendWhatsAppFlow(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	_ = partyID
	var req struct {
		Phone     string `json:"phone"`
		FlowID   string `json:"flow_id"`
		ContactID string `json:"contact_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if waFlows == nil {
		http.Error(w, `{"error":"WhatsApp Flows not configured"}`, http.StatusServiceUnavailable)
		return
	}
	if err := waFlows.SendPledgeFlow(r.Context(), req.Phone, req.FlowID, req.ContactID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "flow_sent"})
}

// ─── USSD Callback ──────────────────────────────────────────────────────────

func handleUSSDCallback(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	sessionID := r.FormValue("sessionId")
	phone := r.FormValue("phoneNumber")
	text := r.FormValue("text")

	response, endSession := ussdHandler.HandleUSSDCallback(r.Context(), sessionID, phone, text)
	if endSession {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, response)
	} else {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, response)
	}
}

// ─── Blockchain Pledge ──────────────────────────────────────────────────────

func handleHashPledge(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	pledgeID := mux.Vars(r)["id"]
	var req struct {
		ElectionID int `json:"election_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	var contactID, wardCode string
	svc.DB.QueryRowContext(r.Context(),
		`SELECT p.contact_id, COALESCE(c.ward_code,'') FROM gotv_pledges p JOIN gotv_contacts c ON c.contact_id=p.contact_id WHERE p.pledge_id=$1 AND p.party_id=$2`,
		pledgeID, partyID).Scan(&contactID, &wardCode)

	hash, err := pledgeVerifier.StorePledgeHash(r.Context(), partyID, contactID, req.ElectionID, wardCode)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"pledge_hash": hash, "status": "stored"})
}

func handleVerifyPledges(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	electionID, _ := strconv.Atoi(mux.Vars(r)["election_id"])

	total, verified, rate, err := pledgeVerifier.VerifyPledgeFulfillment(r.Context(), partyID, electionID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_pledges": total, "verified_wards": verified, "fulfillment_rate": rate,
	})
}

// ─── Alliances ──────────────────────────────────────────────────────────────

func handleCreateAlliance(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var grant gotv.AllianceGrant
	json.NewDecoder(r.Body).Decode(&grant)
	grant.GrantorParty = partyID
	grant.GrantID = "alliance-" + uuid.New().String()[:8]
	if grant.ExpiresAt.IsZero() {
		grant.ExpiresAt = time.Now().Add(30 * 24 * time.Hour)
	}
	if err := allianceMgr.CreateAlliance(r.Context(), grant); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(grant)
}

func handleListAlliances(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, _ := svc.DB.QueryContext(r.Context(),
		`SELECT grant_id, grantor_party_id, grantee_party_id, resource_type, ward_code, expires_at
		 FROM gotv_alliances WHERE grantor_party_id=$1 OR grantee_party_id=$1 ORDER BY created_at DESC`, partyID)
	if rows == nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	defer rows.Close()

	var alliances []map[string]interface{}
	for rows.Next() {
		var id, rType, ward string
		var grantor, grantee int
		var expires time.Time
		rows.Scan(&id, &grantor, &grantee, &rType, &ward, &expires)
		alliances = append(alliances, map[string]interface{}{
			"grant_id": id, "grantor_party_id": grantor, "grantee_party_id": grantee,
			"resource_type": rType, "ward_code": ward, "expires_at": expires,
		})
	}
	if alliances == nil {
		alliances = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(alliances)
}

func handleSharedRides(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rides, err := allianceMgr.GetSharedRides(r.Context(), partyID)
	if err != nil {
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	if rides == nil {
		rides = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(rides)
}

// ─── Field Reports ──────────────────────────────────────────────────────────

func handleListFieldReports(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, _ := svc.DB.QueryContext(r.Context(),
		`SELECT report_id, issue_type, source, resolved, created_at FROM gotv_field_reports WHERE party_id=$1 ORDER BY created_at DESC LIMIT 100`, partyID)
	if rows == nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	defer rows.Close()
	var reports []map[string]interface{}
	for rows.Next() {
		var id, issue, source string
		var resolved bool
		var createdAt time.Time
		rows.Scan(&id, &issue, &source, &resolved, &createdAt)
		reports = append(reports, map[string]interface{}{
			"report_id": id, "issue_type": issue, "source": source, "resolved": resolved, "created_at": createdAt,
		})
	}
	if reports == nil {
		reports = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(reports)
}

// ─── Voice AI Calls ─────────────────────────────────────────────────────────

func handleListVoiceCalls(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, _ := svc.DB.QueryContext(r.Context(),
		`SELECT call_id, campaign_id, contact_id, provider, status, duration_seconds, outcome, created_at
		 FROM gotv_voice_calls WHERE party_id=$1 ORDER BY created_at DESC LIMIT 50`, partyID)
	if rows == nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	defer rows.Close()
	var calls []map[string]interface{}
	for rows.Next() {
		var id, campaign, contact, provider, status, outcome string
		var duration int
		var createdAt time.Time
		rows.Scan(&id, &campaign, &contact, &provider, &status, &duration, &outcome, &createdAt)
		calls = append(calls, map[string]interface{}{
			"call_id": id, "campaign_id": campaign, "contact_id": contact, "provider": provider,
			"status": status, "duration_seconds": duration, "outcome": outcome, "created_at": createdAt,
		})
	}
	if calls == nil {
		calls = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(calls)
}

// ─── War Room (SSE + Summary) ───────────────────────────────────────────────

func handleWarRoomStream(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	// EventSource doesn't support custom headers, so also check query param
	if partyID == 0 {
		if qp, err := strconv.Atoi(r.URL.Query().Get("party_id")); err == nil && qp > 0 {
			partyID = qp
		}
	}
	if partyID == 0 {
		http.Error(w, `{"error":"party_id required"}`, http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// Aggregate real-time metrics
			var totalVolunteers, activeVolunteers, totalRides, pendingRides int
			var totalContacts, reachedContacts int
			svc.DB.QueryRow("SELECT COUNT(*), COUNT(*) FILTER(WHERE last_checkin_at > NOW()-INTERVAL '1 hour') FROM gotv_volunteers WHERE party_id=$1", partyID).Scan(&totalVolunteers, &activeVolunteers)
			svc.DB.QueryRow("SELECT COUNT(*), COUNT(*) FILTER(WHERE status='pending') FROM gotv_ride_requests WHERE party_id=$1", partyID).Scan(&totalRides, &pendingRides)
			svc.DB.QueryRow("SELECT COUNT(*), COUNT(*) FILTER(WHERE voter_status IN ('confirmed','pledged')) FROM gotv_contacts WHERE party_id=$1", partyID).Scan(&totalContacts, &reachedContacts)

			data, _ := json.Marshal(map[string]interface{}{
				"timestamp":         time.Now().Format(time.RFC3339),
				"volunteers_total":  totalVolunteers,
				"volunteers_active": activeVolunteers,
				"rides_total":       totalRides,
				"rides_pending":     pendingRides,
				"contacts_total":    totalContacts,
				"contacts_reached":  reachedContacts,
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func handleWarRoomSummary(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)

	var activeCampaigns, activeVolunteers, pendingRides, dispatchesLastHour, pledgesToday int
	svc.DB.QueryRow("SELECT COUNT(*) FROM gotv_campaigns WHERE party_id=$1 AND status='active'", partyID).Scan(&activeCampaigns)
	svc.DB.QueryRow("SELECT COUNT(*) FROM gotv_volunteers WHERE party_id=$1 AND last_checkin_at > NOW()-INTERVAL '1 hour'", partyID).Scan(&activeVolunteers)
	svc.DB.QueryRow("SELECT COUNT(*) FROM gotv_ride_requests WHERE party_id=$1 AND status='pending'", partyID).Scan(&pendingRides)
	svc.DB.QueryRow("SELECT COUNT(*) FROM gotv_outreach_log WHERE party_id=$1 AND sent_at > NOW()-INTERVAL '1 hour'", partyID).Scan(&dispatchesLastHour)
	svc.DB.QueryRow("SELECT COUNT(*) FROM gotv_pledges WHERE party_id=$1 AND created_at > CURRENT_DATE", partyID).Scan(&pledgesToday)

	// Coverage by state
	rows, _ := svc.DB.QueryContext(r.Context(),
		`SELECT COALESCE(state_code,'Other'), COUNT(*), COUNT(*) FILTER(WHERE voter_status='pledged')
		 FROM gotv_contacts WHERE party_id=$1 GROUP BY state_code ORDER BY COUNT(*) DESC LIMIT 15`, partyID)
	var coverage []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var st string
			var contacts, pledges int
			rows.Scan(&st, &contacts, &pledges)
			coverage = append(coverage, map[string]interface{}{
				"state_code": st, "contacts": contacts, "pledges": pledges, "volunteers": 0,
			})
		}
	}
	if coverage == nil {
		coverage = []map[string]interface{}{}
	}

	// Alerts (low coverage wards)
	var alerts []map[string]interface{}
	alertRows, _ := svc.DB.QueryContext(r.Context(),
		`SELECT COALESCE(ward_code,'unknown'), COUNT(*) as cnt FROM gotv_contacts
		 WHERE party_id=$1 AND voter_status='unknown' GROUP BY ward_code HAVING COUNT(*)>20 ORDER BY cnt DESC LIMIT 5`, partyID)
	if alertRows != nil {
		defer alertRows.Close()
		for alertRows.Next() {
			var ward string
			var cnt int
			alertRows.Scan(&ward, &cnt)
			alerts = append(alerts, map[string]interface{}{
				"level": "warning", "message": fmt.Sprintf("%d uncontacted voters in ward %s", cnt, ward),
				"ward_code": ward, "metric": "uncontacted",
			})
		}
	}
	if alerts == nil {
		alerts = []map[string]interface{}{}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"ops": map[string]interface{}{
			"active_campaigns":    activeCampaigns,
			"active_volunteers":   activeVolunteers,
			"pending_rides":       pendingRides,
			"dispatches_last_hour": dispatchesLastHour,
			"pledges_today":       pledgesToday,
		},
		"alerts":   alerts,
		"coverage": coverage,
	})
}

// ─── Mobile Territory & Map ─────────────────────────────────────────────────

func handleMobileTerritory(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	volID := r.URL.Query().Get("volunteer_id")
	if volID == "" {
		http.Error(w, `{"error":"volunteer_id required"}`, http.StatusBadRequest)
		return
	}
	var territory struct {
		TerritoryID  string `json:"territory_id"`
		WardCode     string `json:"ward_code"`
		ContactCount int    `json:"contact_count"`
		Status       string `json:"status"`
	}
	err := svc.DB.QueryRowContext(r.Context(),
		`SELECT territory_id, ward_code, contact_count, status FROM gotv_territories WHERE party_id=$1 AND volunteer_id=$2 LIMIT 1`,
		partyID, volID).Scan(&territory.TerritoryID, &territory.WardCode, &territory.ContactCount, &territory.Status)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"territory": nil, "contacts": []interface{}{}})
		return
	}

	// Load contacts in the assigned territory
	rows, _ := svc.DB.QueryContext(r.Context(),
		`SELECT contact_id, COALESCE(full_name_encrypted,''), COALESCE(voter_status,'unknown'),
		 COALESCE(latitude,0), COALESCE(longitude,0)
		 FROM gotv_contacts WHERE party_id=$1 AND ward_code=$2 LIMIT 200`,
		partyID, territory.WardCode)
	var contacts []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var cid, name, status string
			var lat, lng float64
			rows.Scan(&cid, &name, &status, &lat, &lng)
			decName, _ := svc.Decrypt(name)
			if decName == "" {
				decName = "(Name unavailable)"
			}
			contacts = append(contacts, map[string]interface{}{
				"contact_id": cid, "name": decName, "voter_status": status,
				"latitude": lat, "longitude": lng,
			})
		}
	}
	if contacts == nil {
		contacts = []map[string]interface{}{}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"territory": territory,
		"contacts":  contacts,
	})
}

func handleMobileLeaderboard(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	entries, _ := dispatcher.GetLeaderboard(r.Context(), partyID, "weekly", 10)
	if entries == nil {
		entries = []gotv.LeaderboardEntry{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"entries": entries})
}

func handleMobileMapTiles(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tile_url":       "https://tiles.stadiamaps.com/tiles/osm_bright/{z}/{x}/{y}.png",
		"style_url":      "https://tiles.stadiamaps.com/styles/osm_bright.json",
		"min_zoom":       10,
		"max_zoom":       18,
		"nigeria_bbox":   []float64{2.676932, 4.272056, 14.680073, 13.892007},
		"cache_strategy": "download_assigned_ward",
	})
}

// ─── AI Variants List ───────────────────────────────────────────────────────

func handleListAIVariants(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, _ := svc.DB.QueryContext(r.Context(),
		`SELECT variant_id, base_message, variant_text, target_state, channel, variant_index, created_at
		 FROM gotv_ai_variants WHERE party_id=$1 ORDER BY created_at DESC LIMIT 100`, partyID)
	if rows == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"variants": []interface{}{}})
		return
	}
	defer rows.Close()
	var variants []map[string]interface{}
	for rows.Next() {
		var id, base, variant, state, channel string
		var idx int
		var createdAt time.Time
		rows.Scan(&id, &base, &variant, &state, &channel, &idx, &createdAt)
		variants = append(variants, map[string]interface{}{
			"variant_id": id, "base_message": base, "variant_text": variant,
			"target_state": state, "channel": channel, "variant_index": idx,
			"created_at": createdAt,
		})
	}
	if variants == nil {
		variants = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"variants": variants})
}

// ─── Turnout Prediction (proxy to Python analytics or local) ────────────────

func handleTurnoutPredict(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)

	var req struct {
		WardCodes  []string `json:"ward_codes"`
		ElectionID int      `json:"election_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Get wards with historical data
	query := `SELECT COALESCE(ward_code,'unknown'), COUNT(*),
		COUNT(*) FILTER(WHERE voter_status='pledged'),
		COUNT(*) FILTER(WHERE voter_status='confirmed')
		FROM gotv_contacts WHERE party_id=$1`
	args := []interface{}{partyID}
	if len(req.WardCodes) > 0 {
		// Build IN clause dynamically
		placeholders := make([]string, len(req.WardCodes))
		for i, wc := range req.WardCodes {
			placeholders[i] = fmt.Sprintf("$%d", i+2)
			args = append(args, wc)
		}
		query += " AND ward_code IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " GROUP BY ward_code ORDER BY ward_code"

	rows, err := svc.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var predictions []map[string]interface{}
	for rows.Next() {
		var ward string
		var total, pledged, confirmed int
		rows.Scan(&ward, &total, &pledged, &confirmed)

		pledgeRate := 0.0
		if total > 0 {
			pledgeRate = float64(pledged+confirmed) / float64(total) * 100
		}
		riskLevel := "low"
		if pledgeRate < 20 {
			riskLevel = "high"
		} else if pledgeRate < 50 {
			riskLevel = "medium"
		}

		predictions = append(predictions, map[string]interface{}{
			"ward_code":         ward,
			"predicted_turnout": pledgeRate,
			"confidence":        0.72,
			"risk_level":        riskLevel,
			"recommended_actions": []string{
				"Increase canvasser presence",
				"Run targeted SMS campaign",
			},
		})
	}
	if predictions == nil {
		predictions = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"predictions": predictions})
}

// ─── Voice AI: Place Call ───────────────────────────────────────────────────

func handlePlaceVoiceCall(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var req struct {
		CampaignID string `json:"campaign_id"`
		ContactID  string `json:"contact_id"`
		Phone      string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if req.CampaignID == "" || req.ContactID == "" || req.Phone == "" {
		http.Error(w, `{"error":"campaign_id, contact_id, and phone are required"}`, http.StatusBadRequest)
		return
	}
	callID := "vc-" + uuid.New().String()[:8]
	// Persist call record regardless of Voice AI availability
	svc.DB.ExecContext(r.Context(),
		`INSERT INTO gotv_voice_calls (call_id, party_id, campaign_id, contact_id, phone_number, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, 'initiated', NOW())`,
		callID, partyID, req.CampaignID, req.ContactID, req.Phone)
	if voiceAI == nil {
		// Queue for later — no voice AI configured
		svc.DB.ExecContext(r.Context(), `UPDATE gotv_voice_calls SET status='queued' WHERE call_id=$1`, callID)
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"call_id": callID, "status": "queued", "note": "Voice AI not configured — call queued"})
		return
	}
	if err := voiceAI.PlaceCall(r.Context(), req.CampaignID, req.ContactID, req.Phone, partyID); err != nil {
		svc.DB.ExecContext(r.Context(), `UPDATE gotv_voice_calls SET status='failed', error_detail=$1 WHERE call_id=$2`, err.Error(), callID)
		http.Error(w, fmt.Sprintf(`{"error":"%s","call_id":"%s"}`, err.Error(), callID), http.StatusInternalServerError)
		return
	}
	svc.DB.ExecContext(r.Context(), `UPDATE gotv_voice_calls SET status='in_progress' WHERE call_id=$1`, callID)
	publishEvent("gotv-voice-calls", callID, map[string]interface{}{"party_id": partyID, "campaign_id": req.CampaignID})
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"call_id": callID, "status": "initiated"})
}

// ─── Field Report Creation ──────────────────────────────────────────────────

func handleCreateFieldReport(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var req struct {
		IssueType   string  `json:"issue_type"`   // voter_intimidation, ballot_irregularity, access_blocked, other
		Description string  `json:"description"`
		WardCode    string  `json:"ward_code"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	// Input validation
	validIssueTypes := map[string]bool{
		"voter_intimidation": true, "ballot_irregularity": true,
		"access_blocked": true, "equipment_failure": true,
		"violence": true, "bribery": true, "other": true,
	}
	if !validIssueTypes[req.IssueType] {
		http.Error(w, `{"error":"invalid issue_type"}`, http.StatusBadRequest)
		return
	}
	if req.Description == "" || len(req.Description) < 10 {
		http.Error(w, `{"error":"description must be at least 10 characters"}`, http.StatusBadRequest)
		return
	}
	if req.Latitude < 4.0 || req.Latitude > 14.0 || req.Longitude < 2.0 || req.Longitude > 15.0 {
		http.Error(w, `{"error":"coordinates outside Nigeria bounds"}`, http.StatusBadRequest)
		return
	}
	reportID := "report-" + uuid.New().String()[:8]
	_, err := svc.DB.ExecContext(r.Context(),
		`INSERT INTO gotv_field_reports (report_id, party_id, issue_type, description, ward_code, latitude, longitude, source, resolved)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'mobile', FALSE)`,
		reportID, partyID, req.IssueType, req.Description, req.WardCode, req.Latitude, req.Longitude)
	if err != nil {
		http.Error(w, `{"error":"failed to create report"}`, http.StatusInternalServerError)
		return
	}
	publishEvent("gotv-field-reports", reportID, map[string]interface{}{
		"party_id": partyID, "issue_type": req.IssueType, "ward_code": req.WardCode,
	})
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"report_id": reportID, "status": "submitted"})
}
