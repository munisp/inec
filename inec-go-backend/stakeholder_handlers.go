package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ─── Stakeholder Recommendation Engine Handlers ───────────────────────────────

// handleStakeholderRecommend proxies a validated request to the configured
// campaign-planning service. It never fabricates recommendations when that
// service is unavailable.
func handleStakeholderRecommend(w http.ResponseWriter, r *http.Request) {
	type StakeholderReq struct {
		CandidateName string `json:"candidate_name"`
		StateCode     string `json:"state_code"`
		OfficeType    string `json:"office_type"`
		PartyCode     string `json:"party_code"`
		Religion      string `json:"religion,omitempty"`
		Ethnicity     string `json:"ethnicity,omitempty"`
		Gender        string `json:"gender,omitempty"`
		TopN          int    `json:"top_n,omitempty"`
	}
	var req StakeholderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CandidateName == "" || req.StateCode == "" || req.OfficeType == "" {
		writeError(w, http.StatusBadRequest, "candidate_name, state_code, and office_type are required")
		return
	}
	if req.TopN == 0 {
		req.TopN = 15
	}
	validOffices := map[string]bool{
		"President": true, "Governor": true, "Senator": true, "House": true, "LGA": true,
	}
	if !validOffices[req.OfficeType] {
		writeError(w, http.StatusBadRequest, "office_type must be one of: President, Governor, Senator, House, LGA")
		return
	}

	campaignSvcURL := strings.TrimRight(strings.TrimSpace(os.Getenv("CAMPAIGN_PLANNING_URL")), "/")
	if campaignSvcURL == "" {
		writeError(w, http.StatusServiceUnavailable, "CAMPAIGN_PLANNING_URL is not configured")
		return
	}
	body, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode stakeholder request")
		return
	}
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, campaignSvcURL+"/api/v1/campaign/stakeholders", strings.NewReader(string(body)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "build stakeholder service request")
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(httpReq)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "campaign-planning service unavailable")
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// handleStakeholderCategories returns canonical stakeholder category metadata.
func handleStakeholderCategories(w http.ResponseWriter, r *http.Request) {
	categories := map[string]interface{}{
		"categories": map[string][]string{
			"Youth":               {"Student Body", "National Service", "Civic Engagement", "Vocational", "Transport Workers"},
			"Women":               {"National Body", "Religious Women", "Traders"},
			"Traditional Leaders": {"Royal Council", "Grassroots"},
			"Religious":           {"Islamic", "Christian", "Christian — Pentecostal"},
			"Professional":        {"Legal", "Healthcare", "Education"},
			"Labour":              {"General Labour", "Energy Workers"},
			"Civil Society":       {"Advocacy"},
			"Agriculture":         {"Farmers"},
			"Commerce":            {"Traders", "Igbo Traders"},
			"Diaspora":            {"Overseas Nigerians"},
			"Inclusion":           {"Disability Rights"},
			"Ethnic/Regional":     {"Yoruba Socio-Political", "Igbo Socio-Political", "Northern Socio-Political", "Middle Belt Socio-Political", "Niger Delta Advocacy"},
		},
		"zone_specific_zones":          []string{"North-West", "North-East", "North-Central", "South-West", "South-East", "South-South"},
		"total_universal_stakeholders": 20,
		"total_zone_specific":          8,
		"coverage":                     "All 36 states + FCT",
	}
	writeJSON(w, http.StatusOK, categories)
}

// handleStakeholderStatesMeta returns canonical state metadata for all 36 states + FCT.
func handleStakeholderStatesMeta(w http.ResponseWriter, r *http.Request) {
	states := []map[string]interface{}{
		{"code": "FCT", "name": "FCT — Abuja", "zone": "North-Central", "lgas": 6},
		{"code": "ABI", "name": "Abia", "zone": "South-East", "lgas": 17},
		{"code": "ADA", "name": "Adamawa", "zone": "North-East", "lgas": 21},
		{"code": "AKW", "name": "Akwa Ibom", "zone": "South-South", "lgas": 31},
		{"code": "ANM", "name": "Anambra", "zone": "South-East", "lgas": 21},
		{"code": "BAU", "name": "Bauchi", "zone": "North-East", "lgas": 20},
		{"code": "BAY", "name": "Bayelsa", "zone": "South-South", "lgas": 8},
		{"code": "BNU", "name": "Benue", "zone": "North-Central", "lgas": 23},
		{"code": "BOR", "name": "Borno", "zone": "North-East", "lgas": 27},
		{"code": "CRS", "name": "Cross River", "zone": "South-South", "lgas": 18},
		{"code": "DEL", "name": "Delta", "zone": "South-South", "lgas": 25},
		{"code": "EBO", "name": "Ebonyi", "zone": "South-East", "lgas": 13},
		{"code": "EDO", "name": "Edo", "zone": "South-South", "lgas": 18},
		{"code": "EKI", "name": "Ekiti", "zone": "South-West", "lgas": 16},
		{"code": "ENU", "name": "Enugu", "zone": "South-East", "lgas": 17},
		{"code": "GOM", "name": "Gombe", "zone": "North-East", "lgas": 11},
		{"code": "IMO", "name": "Imo", "zone": "South-East", "lgas": 27},
		{"code": "JIG", "name": "Jigawa", "zone": "North-West", "lgas": 27},
		{"code": "KAN", "name": "Kano", "zone": "North-West", "lgas": 44},
		{"code": "KAT", "name": "Katsina", "zone": "North-West", "lgas": 34},
		{"code": "KBB", "name": "Kebbi", "zone": "North-West", "lgas": 21},
		{"code": "KOG", "name": "Kogi", "zone": "North-Central", "lgas": 21},
		{"code": "KWA", "name": "Kwara", "zone": "North-Central", "lgas": 16},
		{"code": "LAG", "name": "Lagos", "zone": "South-West", "lgas": 20},
		{"code": "NAS", "name": "Nasarawa", "zone": "North-Central", "lgas": 13},
		{"code": "NGR", "name": "Niger", "zone": "North-Central", "lgas": 25},
		{"code": "OGU", "name": "Ogun", "zone": "South-West", "lgas": 20},
		{"code": "OND", "name": "Ondo", "zone": "South-West", "lgas": 18},
		{"code": "OSU", "name": "Osun", "zone": "South-West", "lgas": 30},
		{"code": "OYO", "name": "Oyo", "zone": "South-West", "lgas": 33},
		{"code": "PLT", "name": "Plateau", "zone": "North-Central", "lgas": 17},
		{"code": "RIV", "name": "Rivers", "zone": "South-South", "lgas": 23},
		{"code": "SOK", "name": "Sokoto", "zone": "North-West", "lgas": 23},
		{"code": "TAR", "name": "Taraba", "zone": "North-East", "lgas": 16},
		{"code": "YOB", "name": "Yobe", "zone": "North-East", "lgas": 17},
		{"code": "ZAM", "name": "Zamfara", "zone": "North-West", "lgas": 14},
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"states": states,
		"total":  len(states),
		"zones":  []string{"North-West", "North-East", "North-Central", "South-West", "South-East", "South-South"},
	})
}
