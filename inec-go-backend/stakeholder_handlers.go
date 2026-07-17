package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// ─── Stakeholder Recommendation Engine Handlers ───────────────────────────────

// handleStakeholderRecommend proxies to the campaign-planning Python service
// or returns a rich fallback response when the service is unavailable.
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
		writeError(w, 400, "invalid request body")
		return
	}
	if req.CandidateName == "" || req.StateCode == "" || req.OfficeType == "" {
		writeError(w, 400, "candidate_name, state_code, and office_type are required")
		return
	}
	if req.TopN == 0 {
		req.TopN = 15
	}
	validOffices := map[string]bool{
		"President": true, "Governor": true, "Senator": true, "House": true, "LGA": true,
	}
	if !validOffices[req.OfficeType] {
		writeError(w, 400, "office_type must be one of: President, Governor, Senator, House, LGA")
		return
	}

	campaignSvcURL := envString("CAMPAIGN_PLANNING_URL", "http://campaign-planning:8007")
	proxyURL := campaignSvcURL + "/api/v1/campaign/stakeholders"

	body, _ := json.Marshal(req)
	httpClient := &http.Client{Timeout: 10 * 1000000000} // 10s
	httpReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, proxyURL, strings.NewReader(string(body)))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		log.Warn().Err(err).Msg("campaign-planning service unavailable, returning fallback stakeholder guidance")
		fallback := buildStakeholderFallback(req.CandidateName, req.StateCode, req.OfficeType, req.PartyCode, req.Gender)
		writeJSON(w, 200, fallback)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleStakeholderCategories returns all stakeholder category metadata.
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
	writeJSON(w, 200, categories)
}

// handleStakeholderStatesMeta returns state metadata for all 36 states + FCT.
func handleStakeholderStatesMeta(w http.ResponseWriter, r *http.Request) {
	states := []map[string]interface{}{
		{"code": "FCT", "name": "FCT — Abuja",  "zone": "North-Central", "lgas": 6},
		{"code": "ABI", "name": "Abia",          "zone": "South-East",    "lgas": 17},
		{"code": "ADA", "name": "Adamawa",       "zone": "North-East",    "lgas": 21},
		{"code": "AKW", "name": "Akwa Ibom",     "zone": "South-South",   "lgas": 31},
		{"code": "ANM", "name": "Anambra",       "zone": "South-East",    "lgas": 21},
		{"code": "BAU", "name": "Bauchi",        "zone": "North-East",    "lgas": 20},
		{"code": "BAY", "name": "Bayelsa",       "zone": "South-South",   "lgas": 8},
		{"code": "BNU", "name": "Benue",         "zone": "North-Central", "lgas": 23},
		{"code": "BOR", "name": "Borno",         "zone": "North-East",    "lgas": 27},
		{"code": "CRS", "name": "Cross River",   "zone": "South-South",   "lgas": 18},
		{"code": "DEL", "name": "Delta",         "zone": "South-South",   "lgas": 25},
		{"code": "EBO", "name": "Ebonyi",        "zone": "South-East",    "lgas": 13},
		{"code": "EDO", "name": "Edo",           "zone": "South-South",   "lgas": 18},
		{"code": "EKI", "name": "Ekiti",         "zone": "South-West",    "lgas": 16},
		{"code": "ENU", "name": "Enugu",         "zone": "South-East",    "lgas": 17},
		{"code": "GOM", "name": "Gombe",         "zone": "North-East",    "lgas": 11},
		{"code": "IMO", "name": "Imo",           "zone": "South-East",    "lgas": 27},
		{"code": "JIG", "name": "Jigawa",        "zone": "North-West",    "lgas": 27},
		{"code": "KAN", "name": "Kano",          "zone": "North-West",    "lgas": 44},
		{"code": "KAT", "name": "Katsina",       "zone": "North-West",    "lgas": 34},
		{"code": "KBB", "name": "Kebbi",         "zone": "North-West",    "lgas": 21},
		{"code": "KOG", "name": "Kogi",          "zone": "North-Central", "lgas": 21},
		{"code": "KWA", "name": "Kwara",         "zone": "North-Central", "lgas": 16},
		{"code": "LAG", "name": "Lagos",         "zone": "South-West",    "lgas": 20},
		{"code": "NAS", "name": "Nasarawa",      "zone": "North-Central", "lgas": 13},
		{"code": "NGR", "name": "Niger",         "zone": "North-Central", "lgas": 25},
		{"code": "OGU", "name": "Ogun",          "zone": "South-West",    "lgas": 20},
		{"code": "OND", "name": "Ondo",          "zone": "South-West",    "lgas": 18},
		{"code": "OSU", "name": "Osun",          "zone": "South-West",    "lgas": 30},
		{"code": "OYO", "name": "Oyo",           "zone": "South-West",    "lgas": 33},
		{"code": "PLT", "name": "Plateau",       "zone": "North-Central", "lgas": 17},
		{"code": "RIV", "name": "Rivers",        "zone": "South-South",   "lgas": 23},
		{"code": "SOK", "name": "Sokoto",        "zone": "North-West",    "lgas": 23},
		{"code": "TAR", "name": "Taraba",        "zone": "North-East",    "lgas": 16},
		{"code": "YOB", "name": "Yobe",          "zone": "North-East",    "lgas": 17},
		{"code": "ZAM", "name": "Zamfara",       "zone": "North-West",    "lgas": 14},
	}
	writeJSON(w, 200, map[string]interface{}{
		"states": states,
		"total":  len(states),
		"zones":  []string{"North-West", "North-East", "North-Central", "South-West", "South-East", "South-South"},
	})
}

// buildStakeholderFallback returns a structured fallback when the Python service is unavailable.
func buildStakeholderFallback(name, stateCode, office, party, gender string) map[string]interface{} {
	universalGroups := []map[string]interface{}{
		{"name": "State Council of Traditional Rulers",                    "category": "Traditional Leaders", "priority": 1, "reach_pct": 12.0, "key_ask": "Royal endorsement — the most powerful voter influence in rural Nigeria"},
		{"name": "Market Women Associations (Iyaloja/State Market Union)", "category": "Women",               "priority": 1, "reach_pct": 9.1,  "key_ask": "Market-level voter mobilisation — the most influential ground network"},
		{"name": "Christian Association of Nigeria (CAN) — State Chapter", "category": "Religious",          "priority": 1, "reach_pct": 10.5, "key_ask": "Church-based voter registration and Sunday announcement endorsements"},
		{"name": "Jama'atu Nasril Islam (JNI) — State Chapter",           "category": "Religious",           "priority": 1, "reach_pct": 10.5, "key_ask": "Mosque-based voter education and Friday sermon endorsement"},
		{"name": "All Farmers Association of Nigeria (AFAN) — State",     "category": "Agriculture",         "priority": 1, "reach_pct": 11.2, "key_ask": "Rural voter mobilisation — largest single occupational voter group"},
		{"name": "Nigeria Labour Congress (NLC) — State Council",         "category": "Labour",              "priority": 1, "reach_pct": 5.5,  "key_ask": "Organised labour endorsement covering millions of formal sector workers"},
		{"name": "National Council of Women's Societies (NCWS)",          "category": "Women",               "priority": 1, "reach_pct": 8.4,  "key_ask": "Women's bloc voter mobilisation — the largest single voting bloc"},
		{"name": "National Association of Nigerian Students (NANS)",      "category": "Youth",               "priority": 1, "reach_pct": 6.8,  "key_ask": "Campus voter registration and first-time voter mobilisation"},
		{"name": "Nigeria Union of Teachers (NUT) — State",               "category": "Professional",        "priority": 1, "reach_pct": 3.4,  "key_ask": "Community-level voter education and school-based voter registration"},
		{"name": "Commercial Motorcyclists & Tricycle Operators (NURTW)", "category": "Youth",               "priority": 1, "reach_pct": 7.3,  "key_ask": "Election Day logistics — transporting voters to polling units"},
		{"name": "Village Heads & Ward Heads",                            "category": "Traditional Leaders", "priority": 1, "reach_pct": 15.0, "key_ask": "Polling unit-level voter mobilisation and election day logistics"},
		{"name": "Civil Society Organisations & NGO Networks",            "category": "Civil Society",       "priority": 2, "reach_pct": 2.8,  "key_ask": "Election monitoring neutrality and policy credibility endorsement"},
		{"name": "Nigerian Bar Association (NBA) — State Branch",         "category": "Professional",        "priority": 2, "reach_pct": 1.2,  "key_ask": "Legal credibility endorsement and election petition monitoring"},
		{"name": "Nigerian Medical Association (NMA) — State",            "category": "Professional",        "priority": 2, "reach_pct": 0.8,  "key_ask": "Professional credibility endorsement and healthcare policy co-design"},
		{"name": "NYSC Alumni Association — State Chapter",               "category": "Youth",               "priority": 2, "reach_pct": 4.2,  "key_ask": "Volunteer mobilisation and polling unit coverage on Election Day"},
	}
	genderNote := ""
	if gender == "Female" {
		genderNote = " As a female candidate, prioritise women's associations (NCWS, Market Women, FOMWAN/CWFN) and deploy male surrogates for Emirate palace visits."
	}
	return map[string]interface{}{
		"candidate":                     name,
		"party":                         party,
		"state_code":                    stateCode,
		"office":                        office,
		"note":                          "Campaign planning service offline — returning universal stakeholder framework." + genderNote,
		"top_stakeholders":              universalGroups,
		"total_stakeholders_identified": len(universalGroups),
		"engagement_sequence": []string{
			"1. Traditional Rulers (legitimacy)",
			"2. Religious Bodies — CAN & JNI (moral authority)",
			"3. Market Women & Iyaloja (economic grassroots)",
			"4. Labour Congress — NLC (organised labour)",
			"5. Farmers Association — AFAN (rural majority)",
			"6. NANS & Youth Networks (first-time voters)",
			"7. NUT Teachers (community opinion leaders)",
			"8. Transport Workers — NURTW/RTEAN (Election Day logistics)",
			"9. Professional Bodies — NBA/NMA (credibility)",
			"10. Civil Society — CSOs (transparency signalling)",
		},
	}
}
