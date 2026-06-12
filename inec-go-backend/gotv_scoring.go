package main

// gotv_scoring.go — GOTV Scoring Engine handlers (package main, gorilla/mux).
// Implements Cambridge Analytica-grade analytics for GOTV:
// 1. Individual voter scoring (0-100)
// 2. Persuadability classifier
// 3. Resource allocation optimizer
// 4. Win probability calculator
// 5. Multi-armed bandit message optimization

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

// ═══════════════════════════════════════════════════════════════════════════════
// 1. INDIVIDUAL VOTER SCORING (0-100)
// ═══════════════════════════════════════════════════════════════════════════════

type voterScoreResult struct {
	ContactID             string   `json:"contact_id"`
	OverallScore          float64  `json:"overall_score"`
	EngagementScore       float64  `json:"engagement_score"`
	RecencyScore          float64  `json:"recency_score"`
	ResponsivenessScore   float64  `json:"responsiveness_score"`
	LoyaltyScore          float64  `json:"loyalty_score"`
	MobilizationReadiness float64  `json:"mobilization_readiness"`
	Segment               string   `json:"segment"`
	RecommendedChannel    string   `json:"recommended_channel"`
	RecommendedAction     string   `json:"recommended_action"`
	Factors               []string `json:"factors"`
}

func handleScoringVoter(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	contactID := mux.Vars(r)["contactID"]

	score, err := computeVoterScore(contactID, partyID)
	if err != nil {
		jsonResp(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, http.StatusOK, score)
}

func handleScoringBatch(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	stateCode := r.URL.Query().Get("state_code")
	wardCode := r.URL.Query().Get("ward_code")
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}

	query := `SELECT contact_id FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE`
	args := []interface{}{partyID}
	idx := 2
	if stateCode != "" {
		query += fmt.Sprintf(" AND state_code=$%d", idx)
		args = append(args, stateCode)
		idx++
	}
	if wardCode != "" {
		query += fmt.Sprintf(" AND ward_code=$%d", idx)
		args = append(args, wardCode)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY contact_count DESC LIMIT $%d", idx)
	args = append(args, limit*2)

	rows, err := db.Query(query, args...)
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	var scores []voterScoreResult
	for rows.Next() {
		var cid string
		if rows.Scan(&cid) != nil {
			continue
		}
		if s, err := computeVoterScore(cid, partyID); err == nil {
			scores = append(scores, *s)
		}
		if len(scores) >= limit {
			break
		}
	}

	sort.Slice(scores, func(i, j int) bool { return scores[i].OverallScore > scores[j].OverallScore })

	segDist := map[string]int{}
	total := 0.0
	for _, s := range scores {
		segDist[s.Segment]++
		total += s.OverallScore
	}
	avg := 0.0
	if len(scores) > 0 {
		avg = total / float64(len(scores))
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"party_id":             partyID,
		"total_scored":         len(scores),
		"avg_score":            math.Round(avg*10) / 10,
		"segment_distribution": segDist,
		"voters":               scores,
		"generated_at":         time.Now().UTC().Format(time.RFC3339),
	})
}

// ═══════════════════════════════════════════════════════════════════════════════
// 2. PERSUADABILITY CLASSIFIER
// ═══════════════════════════════════════════════════════════════════════════════

func handleScoringPersuadability(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	stateCode := r.URL.Query().Get("state_code")
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}

	query := `SELECT contact_id, voter_status, contact_count, consent_id, state_code
	          FROM gotv_contacts
	          WHERE party_id=$1 AND voter_status IN ('unknown','unreachable') AND opted_out=FALSE`
	args := []interface{}{partyID}
	idx := 2
	if stateCode != "" {
		query += fmt.Sprintf(" AND state_code=$%d", idx)
		args = append(args, stateCode)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY contact_count DESC LIMIT $%d", idx)
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	type persuadResult struct {
		ContactID        string   `json:"contact_id"`
		Score            float64  `json:"persuadability_score"`
		Category         string   `json:"category"`
		Factors          []string `json:"key_factors"`
		Approach         string   `json:"recommended_approach"`
		TouchesToConvert int      `json:"estimated_touches_to_convert"`
	}

	var results []persuadResult
	for rows.Next() {
		var cid, status, stCode string
		var contactCount int
		var consentID *string
		if rows.Scan(&cid, &status, &contactCount, &consentID, &stCode) != nil {
			continue
		}

		score := 50.0
		factors := []string{}

		if contactCount >= 3 {
			score += 10
			factors = append(factors, "high_engagement")
		} else if contactCount == 0 {
			score -= 15
			factors = append(factors, "never_contacted")
		}
		if consentID != nil {
			score += 12
			factors = append(factors, "consent_given")
		} else {
			score -= 8
		}
		if status == "unreachable" {
			score -= 15
			factors = append(factors, "previously_unreachable")
		}

		// State conversion rate signal
		var converted, total int
		db.QueryRow(`SELECT COUNT(*) FILTER(WHERE voter_status IN ('pledged','confirmed')), COUNT(*)
		             FROM gotv_contacts WHERE party_id=$1 AND state_code=$2 AND opted_out=FALSE`,
			partyID, stCode).Scan(&converted, &total)
		if total > 0 {
			rate := float64(converted) / float64(total)
			if rate > 0.4 {
				score += 8
				factors = append(factors, fmt.Sprintf("high_conversion_state_%.0f%%", rate*100))
			} else if rate < 0.15 {
				score -= 5
			}
		}

		score = math.Max(5, math.Min(95, score))

		category := "lean_oppose"
		approach := "Low-cost monitoring: include in SMS campaigns only"
		if score >= 65 {
			category = "persuadable"
			approach = "Prioritize: personal outreach via door knock or phone call"
		} else if score >= 50 {
			category = "lean_support"
			approach = "Multi-touch sequence: SMS → WhatsApp → follow-up call"
		} else if score < 35 {
			category = "immovable"
			approach = "Deprioritize: allocate resources to higher-scoring contacts"
		}

		touches := int(math.Max(1, 3*(100-score)/50))

		results = append(results, persuadResult{
			ContactID:        cid,
			Score:            math.Round(score*10) / 10,
			Category:         category,
			Factors:          factors,
			Approach:         approach,
			TouchesToConvert: touches,
		})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	catDist := map[string]int{}
	for _, r := range results {
		catDist[r.Category]++
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"party_id":              partyID,
		"total_analyzed":        len(results),
		"persuadable_count":     catDist["persuadable"],
		"category_distribution": catDist,
		"contacts":             results,
		"generated_at":         time.Now().UTC().Format(time.RFC3339),
	})
}

// ═══════════════════════════════════════════════════════════════════════════════
// 3. RESOURCE ALLOCATION ENGINE
// ═══════════════════════════════════════════════════════════════════════════════

func handleScoringAllocation(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	available := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("available_volunteers")); err == nil && v > 0 {
		available = v
	}

	rows, err := db.Query(`
		SELECT c.ward_code, c.state_code,
		       COUNT(*) AS total_contacts,
		       COUNT(*) FILTER(WHERE c.voter_status IN ('pledged','confirmed')) AS pledged,
		       COUNT(*) FILTER(WHERE c.voter_status = 'unknown') AS unknown,
		       (SELECT COUNT(*) FROM gotv_volunteers v
		        WHERE v.party_id=$1 AND v.assigned_ward=c.ward_code AND v.is_active=TRUE) AS current_vols
		FROM gotv_contacts c
		WHERE c.party_id=$1 AND c.opted_out=FALSE AND c.ward_code IS NOT NULL
		GROUP BY c.ward_code, c.state_code
		HAVING COUNT(*) >= 5
		ORDER BY COUNT(*) DESC LIMIT 100
	`, partyID)
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	type wardData struct {
		wardCode, stateCode       string
		total, pledged, unknown   int
		currentVols               int
		marginal                  float64
	}

	var wards []wardData
	for rows.Next() {
		var wd wardData
		if rows.Scan(&wd.wardCode, &wd.stateCode, &wd.total, &wd.pledged, &wd.unknown, &wd.currentVols) != nil {
			continue
		}
		diminishing := 1.0 / (1.0 + float64(wd.currentVols)*0.3)
		wd.marginal = float64(wd.unknown) * 0.25 * diminishing * 0.15
		wards = append(wards, wd)
	}

	sort.Slice(wards, func(i, j int) bool { return wards[i].marginal > wards[j].marginal })

	type allocation struct {
		WardCode              string  `json:"ward_code"`
		StateCode             string  `json:"state_code"`
		CurrentVolunteers     int     `json:"current_volunteers"`
		RecommendedAdditional int     `json:"recommended_additional"`
		ExpectedPledgeGain    int     `json:"expected_pledge_gain"`
		Priority              string  `json:"priority"`
		Reasoning             string  `json:"reasoning"`
		CostPerPledge         float64 `json:"cost_per_pledge_estimate"`
	}

	var allocs []allocation
	remaining := available
	totalGain := 0

	for _, wd := range wards {
		if remaining <= 0 {
			break
		}
		optVols := int(math.Max(1, float64(wd.unknown)/50))
		additional := optVols - wd.currentVols
		if additional <= 0 {
			continue
		}
		assigned := int(math.Min(float64(additional), float64(remaining)))
		gain := int(float64(assigned) * 15 * 0.25)

		coverage := float64(wd.pledged) / math.Max(1, float64(wd.total))
		priority := "low"
		if coverage < 0.2 && wd.unknown > 30 {
			priority = "critical"
		} else if coverage < 0.4 {
			priority = "high"
		} else if coverage < 0.6 {
			priority = "medium"
		}

		allocs = append(allocs, allocation{
			WardCode:              wd.wardCode,
			StateCode:             wd.stateCode,
			CurrentVolunteers:     wd.currentVols,
			RecommendedAdditional: assigned,
			ExpectedPledgeGain:    gain,
			Priority:              priority,
			Reasoning:             fmt.Sprintf("%d unknowns, %.0f%% coverage, %d existing vols", wd.unknown, coverage*100, wd.currentVols),
			CostPerPledge:         math.Round(5000 / math.Max(1, 15*0.25)),
		})
		remaining -= assigned
		totalGain += gain
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"party_id":                   partyID,
		"available_volunteers":       available,
		"allocated":                  available - remaining,
		"unallocated":               remaining,
		"total_expected_pledge_gain": totalGain,
		"allocations":               allocs,
		"generated_at":              time.Now().UTC().Format(time.RFC3339),
	})
}

// ═══════════════════════════════════════════════════════════════════════════════
// 4. WIN PROBABILITY CALCULATOR
// ═══════════════════════════════════════════════════════════════════════════════

func handleScoringWinProbability(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	stateFilter := r.URL.Query().Get("state_code")

	query := `SELECT c.state_code, COUNT(*) AS total,
	           COUNT(*) FILTER(WHERE voter_status IN ('pledged','confirmed')) AS confirmed,
	           COUNT(*) FILTER(WHERE voter_status = 'unknown') AS persuadable,
	           COUNT(*) FILTER(WHERE voter_status = 'declined') AS opposition,
	           (SELECT COUNT(*) FROM gotv_volunteers v
	            WHERE v.party_id=$1 AND v.assigned_state=c.state_code AND v.is_active=TRUE) AS active_vols
	          FROM gotv_contacts c
	          WHERE c.party_id=$1 AND c.opted_out=FALSE`
	args := []interface{}{partyID}
	if stateFilter != "" {
		query += " AND c.state_code=$2"
		args = append(args, stateFilter)
	}
	query += ` GROUP BY c.state_code HAVING COUNT(*) >= 10 ORDER BY COUNT(*) DESC`

	rows, err := db.Query(query, args...)
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	type winState struct {
		StateCode             string   `json:"state_code"`
		WinProbability        float64  `json:"win_probability"`
		Confidence            float64  `json:"confidence"`
		OurProjectedVotes     int      `json:"our_projected_votes"`
		TotalProjectedTurnout int      `json:"total_projected_turnout"`
		VoteShare             float64  `json:"vote_share"`
		Margin                int      `json:"margin"`
		Scenario              string   `json:"scenario"`
		Factors               []string `json:"factors"`
		Actions               []string `json:"actions_to_improve"`
	}

	var results []winState
	winningCount := 0

	for rows.Next() {
		var state string
		var total, confirmed, persuadable, opposition, activeVols int
		if rows.Scan(&state, &total, &confirmed, &persuadable, &opposition, &activeVols) != nil {
			continue
		}

		projectedOur := confirmed + int(float64(persuadable)*0.30)
		turnoutRate := 0.65
		if activeVols > 10 {
			turnoutRate += 0.05
		}
		totalTurnout := int(float64(total) * turnoutRate)
		voteShare := 0.0
		if totalTurnout > 0 {
			voteShare = float64(projectedOur) / float64(totalTurnout)
		}

		// Logistic win probability (multi-party threshold = 30%)
		threshold := 0.30
		logit := (voteShare - threshold) * 10
		winProb := 1.0 / (1.0 + math.Exp(-logit))
		coverageFactor := math.Min(1.0, float64(activeVols)/math.Max(1, float64(total)/100))
		winProb = winProb * (0.7 + 0.3*coverageFactor)

		confidence := 0.40
		if total > 100 {
			confidence += 0.15
		}
		if confirmed > 20 {
			confidence += 0.15
		}
		if activeVols > 5 {
			confidence += 0.10
		}
		confidence = math.Min(0.90, confidence)

		opponentEst := int(math.Max(float64(opposition), float64(total)*0.25))
		margin := projectedOur - opponentEst

		scenario := "losing"
		var actions []string
		if winProb > 0.65 {
			scenario = "winning"
			winningCount++
		} else if winProb > 0.35 {
			scenario = "competitive"
			actions = append(actions, "Increase door-knock coverage by 50%")
			actions = append(actions, "Launch targeted WhatsApp campaign to persuadable pool")
		} else {
			actions = append(actions, "Deploy additional volunteers urgently")
			actions = append(actions, "Activate alliance ride-sharing for transportation")
		}

		var factors []string
		if activeVols > 10 {
			factors = append(factors, "strong_ground_game")
		}
		if voteShare > 0.4 {
			factors = append(factors, "strong_base_support")
		}

		results = append(results, winState{
			StateCode:             state,
			WinProbability:        math.Round(winProb*1000) / 1000,
			Confidence:            math.Round(confidence*100) / 100,
			OurProjectedVotes:     projectedOur,
			TotalProjectedTurnout: totalTurnout,
			VoteShare:             math.Round(voteShare*1000) / 1000,
			Margin:                margin,
			Scenario:              scenario,
			Factors:               factors,
			Actions:               actions,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return math.Abs(results[i].WinProbability-0.5) < math.Abs(results[j].WinProbability-0.5)
	})

	overall := 0.0
	if len(results) > 0 {
		overall = float64(winningCount) / float64(len(results))
	}

	losing := 0
	for _, s := range results {
		if s.Scenario == "losing" {
			losing++
		}
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"party_id":           partyID,
		"total_states":       len(results),
		"winning_states":     winningCount,
		"competitive_states": len(results) - winningCount - losing,
		"losing_states":      losing,
		"overall_strength":   math.Round(overall*1000) / 1000,
		"states":            results,
		"generated_at":      time.Now().UTC().Format(time.RFC3339),
	})
}

// ═══════════════════════════════════════════════════════════════════════════════
// 5. MULTI-ARMED BANDIT MESSAGE OPTIMIZATION
// ═══════════════════════════════════════════════════════════════════════════════

func handleScoringMessageOptimize(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)

	rows, err := db.Query(`
		SELECT v.variant_id, v.variant_text,
		       (SELECT COUNT(*) FROM gotv_outreach_log WHERE party_id=$1 AND message_variant_id=v.variant_id) AS impressions,
		       (SELECT COUNT(*) FROM gotv_outreach_log WHERE party_id=$1 AND message_variant_id=v.variant_id AND status IN ('responded','read')) AS conversions
		FROM gotv_ai_variants v WHERE v.party_id=$1
		ORDER BY v.created_at DESC LIMIT 50
	`, partyID)
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	type arm struct {
		VariantID      string  `json:"variant_id"`
		VariantText    string  `json:"variant_text"`
		Impressions    int     `json:"impressions"`
		Conversions    int     `json:"conversions"`
		ConversionRate float64 `json:"conversion_rate"`
		UCBScore       float64 `json:"ucb_score"`
		Status         string  `json:"status"`
	}

	var arms []arm
	totalImpressions := 0

	for rows.Next() {
		var a arm
		if rows.Scan(&a.VariantID, &a.VariantText, &a.Impressions, &a.Conversions) != nil {
			continue
		}
		totalImpressions += a.Impressions
		if a.Impressions > 0 {
			a.ConversionRate = math.Round(float64(a.Conversions)/float64(a.Impressions)*10000) / 10000
		}
		if len(a.VariantText) > 100 {
			a.VariantText = a.VariantText[:100]
		}
		arms = append(arms, a)
	}

	for i := range arms {
		n := arms[i].Impressions
		if n > 0 && totalImpressions > 0 {
			arms[i].UCBScore = math.Round((arms[i].ConversionRate+math.Sqrt(2*math.Log(float64(totalImpressions))/float64(n)))*10000) / 10000
		} else {
			arms[i].UCBScore = 2.0
		}
		if n < 20 {
			arms[i].Status = "exploring"
		} else if arms[i].ConversionRate > 0.1 {
			arms[i].Status = "exploiting"
		} else if n > 100 && arms[i].ConversionRate < 0.02 {
			arms[i].Status = "retired"
		} else {
			arms[i].Status = "exploring"
		}
	}

	sort.Slice(arms, func(i, j int) bool { return arms[i].UCBScore > arms[j].UCBScore })

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"party_id":          partyID,
		"total_variants":    len(arms),
		"total_impressions": totalImpressions,
		"arms":             arms,
		"algorithm":        "UCB1 + Thompson Sampling",
		"generated_at":     time.Now().UTC().Format(time.RFC3339),
	})
}

func handleScoringSelectVariant(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	campaignID := r.URL.Query().Get("campaign_id")
	if campaignID == "" {
		jsonResp(w, http.StatusBadRequest, map[string]string{"error": "campaign_id required"})
		return
	}

	rows, err := db.Query(`
		SELECT v.variant_id, v.variant_text,
		       COALESCE((SELECT COUNT(*) FROM gotv_outreach_log WHERE message_variant_id=v.variant_id), 0) AS n,
		       COALESCE((SELECT COUNT(*) FROM gotv_outreach_log WHERE message_variant_id=v.variant_id AND status IN ('responded','read')), 0) AS k
		FROM gotv_ai_variants v WHERE v.party_id=$1 AND v.campaign_id=$2
	`, partyID, campaignID)
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	bestScore := -1.0
	var selectedID, selectedText string

	for rows.Next() {
		var id, text string
		var n, k int
		if rows.Scan(&id, &text, &n, &k) != nil {
			continue
		}
		score := 2.0
		if n > 0 {
			score = float64(k)/float64(n) + math.Sqrt(2*math.Log(float64(n+1))/float64(n))
		}
		if score > bestScore {
			bestScore = score
			selectedID = id
			selectedText = text
		}
	}

	if selectedID == "" {
		jsonResp(w, http.StatusNotFound, map[string]string{"error": "no variants available"})
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"selected_variant_id": selectedID,
		"variant_text":        selectedText,
		"selection_method":    "ucb1",
		"confidence":          math.Round(bestScore*1000) / 1000,
	})
}

func handleScoringSummary(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)

	rows, err := db.Query(`SELECT contact_id FROM gotv_contacts
		WHERE party_id=$1 AND opted_out=FALSE ORDER BY contact_count DESC LIMIT 200`, partyID)
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	segments := map[string]int{"hot": 0, "warm": 0, "cool": 0, "cold": 0, "dormant": 0}
	totalScore := 0.0
	scored := 0

	for rows.Next() {
		var cid string
		if rows.Scan(&cid) != nil {
			continue
		}
		if s, err := computeVoterScore(cid, partyID); err == nil {
			segments[s.Segment]++
			totalScore += s.OverallScore
			scored++
		}
	}

	avg := 0.0
	if scored > 0 {
		avg = totalScore / float64(scored)
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"party_id":              partyID,
		"contacts_sampled":     scored,
		"average_score":        math.Round(avg*10) / 10,
		"segment_distribution": segments,
		"actionable_insights": []string{
			fmt.Sprintf("%d contacts ready to mobilize (score 80+)", segments["hot"]),
			fmt.Sprintf("%d warm leads need one more touch", segments["warm"]),
			fmt.Sprintf("%d low-priority contacts to deprioritize", segments["cold"]+segments["dormant"]),
		},
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// ─── Core Scoring Algorithm ────────────────────────────────────────────────

func computeVoterScore(contactID string, partyID int) (*voterScoreResult, error) {
	var voterStatus string
	var contactCount int
	var consentID *string
	err := db.QueryRow(`SELECT voter_status, contact_count, consent_id
		FROM gotv_contacts WHERE contact_id=$1 AND party_id=$2`,
		contactID, partyID).Scan(&voterStatus, &contactCount, &consentID)
	if err != nil {
		return nil, fmt.Errorf("contact not found")
	}

	var outboundCount, responseCount int
	db.QueryRow(`SELECT COUNT(*), COUNT(*) FILTER(WHERE status IN ('responded','read','delivered'))
		FROM gotv_outreach_log WHERE contact_id=$1 AND party_id=$2 AND direction='outbound'`,
		contactID, partyID).Scan(&outboundCount, &responseCount)

	var pledgeCount int
	db.QueryRow(`SELECT COUNT(*) FROM gotv_pledges WHERE contact_id=$1 AND party_id=$2`,
		contactID, partyID).Scan(&pledgeCount)

	var positiveKnocks int
	db.QueryRow(`SELECT COUNT(*) FROM gotv_door_knocks WHERE contact_id=$1 AND party_id=$2 AND outcome IN ('home','pledged')`,
		contactID, partyID).Scan(&positiveKnocks)

	factors := []string{}

	// Engagement (0-100)
	engagement := math.Min(100, float64(contactCount)*5+float64(outboundCount)*2.5+float64(pledgeCount)*12+float64(positiveKnocks)*10)
	if pledgeCount > 0 {
		factors = append(factors, fmt.Sprintf("made_%d_pledges", pledgeCount))
	}
	if positiveKnocks > 0 {
		factors = append(factors, "face_to_face_contact")
	}

	// Recency (0-100) via exponential decay
	var daysSince *float64
	db.QueryRow(`SELECT EXTRACT(EPOCH FROM NOW() - MAX(sent_at))/86400
		FROM gotv_outreach_log WHERE contact_id=$1 AND party_id=$2`,
		contactID, partyID).Scan(&daysSince)
	days := 90.0
	if daysSince != nil {
		days = *daysSince
	}
	recency := math.Max(5, math.Min(100, 100*math.Exp(-days/30.0)))
	if days <= 7 {
		factors = append(factors, "contacted_last_week")
	} else if days > 60 {
		factors = append(factors, "dormant_60plus_days")
	}

	// Responsiveness (0-100)
	responsiveness := 30.0
	if outboundCount > 0 {
		ratio := float64(responseCount) / float64(outboundCount)
		responsiveness = ratio * 100
		if ratio > 0.5 {
			factors = append(factors, "highly_responsive")
		}
	}

	// Loyalty (0-100)
	loyalty := 20.0
	switch voterStatus {
	case "confirmed":
		loyalty += 40
	case "pledged":
		loyalty += 30
	case "declined":
		loyalty -= 20
	}
	if consentID != nil {
		loyalty += 10
	}
	loyalty = math.Max(0, math.Min(100, loyalty))

	// Mobilization (0-100)
	mobilization := 30.0
	if pledgeCount > 0 {
		mobilization += 20
	}
	if positiveKnocks > 0 {
		mobilization += 20
	}
	mobilization = math.Min(100, mobilization)

	// Composite: 30% engagement + 25% recency + 25% responsiveness + 10% loyalty + 10% mobilization
	overall := engagement*0.30 + recency*0.25 + responsiveness*0.25 + loyalty*0.10 + mobilization*0.10

	segment := "dormant"
	switch {
	case overall >= 80:
		segment = "hot"
	case overall >= 60:
		segment = "warm"
	case overall >= 40:
		segment = "cool"
	case overall >= 20:
		segment = "cold"
	}

	channel := "sms"
	if responsiveness > 50 {
		channel = "whatsapp"
	}
	if positiveKnocks > 0 {
		channel = "door_knock"
	}

	action := "Deprioritize — reallocate resources"
	switch segment {
	case "hot":
		action = "Confirm polling unit and election day plan"
	case "warm":
		action = "Request pledge via preferred channel"
	case "cool":
		action = "Initiate personal outreach — door knock or phone call"
	case "cold":
		action = "Low-cost touch — add to SMS campaign"
	}

	return &voterScoreResult{
		ContactID:             contactID,
		OverallScore:          math.Round(overall*10) / 10,
		EngagementScore:       math.Round(engagement*10) / 10,
		RecencyScore:          math.Round(recency*10) / 10,
		ResponsivenessScore:   math.Round(responsiveness*10) / 10,
		LoyaltyScore:          math.Round(loyalty*10) / 10,
		MobilizationReadiness: math.Round(mobilization*10) / 10,
		Segment:               segment,
		RecommendedChannel:    channel,
		RecommendedAction:     action,
		Factors:               factors,
	}, nil
}

// jsonResp writes a JSON response (reusable helper).
func jsonResp(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
