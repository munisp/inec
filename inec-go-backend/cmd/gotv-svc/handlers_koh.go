// KOH 2027 Indicators — Voter Popularity & Performance Framework
// Implements 8 modules: CPI, Demographics, Surveys, LGA Strategy, Social Listening,
// Endorsements, Scheduled Reports, Platform Analytics Ingestion.
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// ─── Schema: KOH Indicator Tables ──────────────────────────────────────────

func initKOHIndicatorTables(db *sql.DB) error {
	ddl := `
	-- Demographic enrichment on existing contacts (idempotent)
	DO $$ BEGIN
		ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS age_group VARCHAR(20);
		ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS gender VARCHAR(10);
		ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS lga_code VARCHAR(20);
		ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS lcda_code VARCHAR(20);
		ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS lga_tier INTEGER;
		ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS socioeconomic_class VARCHAR(5);
		ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS occupation_group VARCHAR(30);
		ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS education_level VARCHAR(20);
		ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS religion VARCHAR(20);
		ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS ethnicity VARCHAR(20);
	EXCEPTION WHEN OTHERS THEN NULL;
	END $$;

	-- CPI History
	CREATE TABLE IF NOT EXISTS gotv_cpi_history (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		computed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		cpi_score NUMERIC(5,2) NOT NULL,
		voting_intention_pct NUMERIC(5,2),
		favourability_pct NUMERIC(5,2),
		digital_sentiment NUMERIC(5,2),
		ground_mobilisation NUMERIC(5,2),
		endorsement_index NUMERIC(5,2),
		share_of_voice NUMERIC(5,2),
		lga_code VARCHAR(20),
		demographic_filter JSONB,
		notes TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_cpi_party ON gotv_cpi_history(party_id, computed_at);

	-- Survey System
	CREATE TABLE IF NOT EXISTS gotv_surveys (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		survey_name VARCHAR(255) NOT NULL,
		wave_number INTEGER DEFAULT 1,
		sample_size INTEGER,
		methodology VARCHAR(100),
		start_date DATE,
		end_date DATE,
		status VARCHAR(20) DEFAULT 'active',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS gotv_survey_responses (
		id SERIAL PRIMARY KEY,
		survey_id INTEGER NOT NULL REFERENCES gotv_surveys(id),
		party_id INTEGER NOT NULL,
		lga_code VARCHAR(20),
		lcda_code VARCHAR(20),
		age_group VARCHAR(20),
		gender VARCHAR(10),
		socioeconomic_class VARCHAR(5),
		occupation_group VARCHAR(30),
		education_level VARCHAR(20),
		religion VARCHAR(20),
		ethnicity VARCHAR(20),
		awareness_score NUMERIC(5,2),
		favourability_score NUMERIC(5,2),
		voting_intention BOOLEAN,
		issue_alignment NUMERIC(5,2),
		message_recall BOOLEAN,
		nps_score INTEGER,
		net_promoter VARCHAR(20),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_survey_resp_survey ON gotv_survey_responses(survey_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_survey_resp_party ON gotv_survey_responses(party_id);

	-- Social Listening / Sentiment
	CREATE TABLE IF NOT EXISTS gotv_social_metrics (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		date DATE NOT NULL,
		platform VARCHAR(30) NOT NULL,
		metric_type VARCHAR(50) NOT NULL,
		value NUMERIC(12,2),
		demographic_filter JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_social_metrics_date ON gotv_social_metrics(party_id, date);

	CREATE TABLE IF NOT EXISTS gotv_sentiment_log (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		platform VARCHAR(30),
		mention_text TEXT,
		sentiment VARCHAR(20),
		sentiment_score NUMERIC(5,2),
		candidate_mentioned VARCHAR(100),
		topic VARCHAR(100),
		url TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_sentiment_ts ON gotv_sentiment_log(party_id, timestamp);

	-- Endorsement Tracker
	CREATE TABLE IF NOT EXISTS gotv_endorsements (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		endorser_name VARCHAR(255) NOT NULL,
		endorser_type VARCHAR(50) NOT NULL,
		endorser_category VARCHAR(50),
		lga_code VARCHAR(20),
		demographic_reach INTEGER,
		date_endorsed DATE,
		public_statement TEXT,
		verified BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_endorsements_party ON gotv_endorsements(party_id);

	CREATE TABLE IF NOT EXISTS gotv_defections (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		defector_name VARCHAR(255) NOT NULL,
		from_party VARCHAR(100),
		to_party VARCHAR(100),
		defection_date DATE,
		lga_code VARCHAR(20),
		is_public BOOLEAN DEFAULT TRUE,
		description TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- LGA Strategic Tiers (reference table)
	CREATE TABLE IF NOT EXISTS gotv_lga_tiers (
		id SERIAL PRIMARY KEY,
		lga_code VARCHAR(20) UNIQUE NOT NULL,
		lga_name VARCHAR(100) NOT NULL,
		tier INTEGER NOT NULL,
		tier_name VARCHAR(50) NOT NULL,
		strategic_focus TEXT,
		party_id INTEGER
	);

	-- Scheduled Reports
	CREATE TABLE IF NOT EXISTS gotv_reports_generated (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		report_type VARCHAR(50) NOT NULL,
		frequency VARCHAR(20) NOT NULL,
		generated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		data_snapshot JSONB,
		period_start DATE,
		period_end DATE,
		status VARCHAR(20) DEFAULT 'generated'
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_reports_party ON gotv_reports_generated(party_id, generated_at);

	-- Platform Analytics Ingestion
	CREATE TABLE IF NOT EXISTS gotv_platform_analytics (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		date DATE NOT NULL,
		platform VARCHAR(30) NOT NULL,
		followers INTEGER,
		follower_growth_pct NUMERIC(5,2),
		total_reach INTEGER,
		organic_reach INTEGER,
		paid_reach INTEGER,
		engagement_rate NUMERIC(5,4),
		video_completion_rate NUMERIC(5,4),
		impressions INTEGER,
		clicks INTEGER,
		shares INTEGER,
		comments INTEGER,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_platform_analytics ON gotv_platform_analytics(party_id, date, platform);
	`
	_, err := db.Exec(ddl)
	return err
}

// ─── LGA Tier Reference Data (Lagos State) ─────────────────────────────────

type LGATier struct {
	LGACode       string `json:"lga_code"`
	LGAName       string `json:"lga_name"`
	Tier          int    `json:"tier"`
	TierName      string `json:"tier_name"`
	StrategicFocus string `json:"strategic_focus"`
}

var lagosLGATiers = []LGATier{
	// Tier 1 — Strongholds
	{"alimosho", "Alimosho", 1, "Stronghold", "Maximise turnout; defend existing support base"},
	{"mushin", "Mushin", 1, "Stronghold", "Maximise turnout; defend existing support base"},
	{"agege", "Agege", 1, "Stronghold", "Maximise turnout; defend existing support base"},
	{"oshodi-isolo", "Oshodi-Isolo", 1, "Stronghold", "Maximise turnout; defend existing support base"},
	{"ajeromi-ifelodun", "Ajeromi-Ifelodun", 1, "Stronghold", "Maximise turnout; defend existing support base"},
	// Tier 2 — Swing Areas
	{"kosofe", "Kosofe", 2, "Swing", "Convert undecided voters; build margins"},
	{"somolu", "Somolu", 2, "Swing", "Convert undecided voters; build margins"},
	{"lagos-mainland", "Lagos Mainland", 2, "Swing", "Convert undecided voters; build margins"},
	{"ifako-ijaiye", "Ifako-Ijaiye", 2, "Swing", "Convert undecided voters; build margins"},
	{"surulere", "Surulere", 2, "Swing", "Convert undecided voters; build margins"},
	{"ojo", "Ojo", 2, "Swing", "Convert undecided voters; build margins"},
	// Tier 3 — Growth Areas
	{"ikorodu", "Ikorodu", 3, "Growth", "Expand footprint; community-level penetration"},
	{"badagry", "Badagry", 3, "Growth", "Expand footprint; community-level penetration"},
	{"epe", "Epe", 3, "Growth", "Expand footprint; community-level penetration"},
	{"ibeju-lekki", "Ibeju-Lekki", 3, "Growth", "Expand footprint; community-level penetration"},
	// Tier 4 — Urban Centres
	{"ikeja", "Ikeja", 4, "Urban Centre", "Professional and upper-class voter engagement"},
	{"lagos-island", "Lagos Island", 4, "Urban Centre", "Professional and upper-class voter engagement"},
	{"eti-osa", "Eti-Osa", 4, "Urban Centre", "Professional and upper-class voter engagement"},
	{"apapa", "Apapa", 4, "Urban Centre", "Professional and upper-class voter engagement"},
}

func seedLGATiers(db *sql.DB, partyID int) {
	for _, t := range lagosLGATiers {
		db.Exec(`INSERT INTO gotv_lga_tiers (lga_code, lga_name, tier, tier_name, strategic_focus, party_id)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (lga_code) DO NOTHING`,
			t.LGACode, t.LGAName, t.Tier, t.TierName, t.StrategicFocus, partyID)
	}
}

// ─── Module 1: CPI (Composite Popularity Index) ────────────────────────────

type CPIResult struct {
	Score              float64 `json:"cpi_score"`
	VotingIntention    float64 `json:"voting_intention_pct"`
	Favourability      float64 `json:"favourability_pct"`
	DigitalSentiment   float64 `json:"digital_sentiment"`
	GroundMobilisation float64 `json:"ground_mobilisation"`
	EndorsementIndex   float64 `json:"endorsement_index"`
	ShareOfVoice       float64 `json:"share_of_voice"`
	ComputedAt         string  `json:"computed_at"`
	Interpretation     string  `json:"interpretation"`
}

func handleComputeCPI(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	lga := r.URL.Query().Get("lga")

	// 1. Voting Intention (30%) — from surveys + pledge rate
	votingIntention := computeVotingIntention(partyID, lga)

	// 2. Favourability (25%) — from survey data
	favourability := computeFavourability(partyID, lga)

	// 3. Digital Sentiment (15%) — from social listening
	sentiment := computeDigitalSentiment(partyID)

	// 4. Ground Mobilisation (15%) — from canvassing data
	groundMob := computeGroundMobilisation(partyID, lga)

	// 5. Endorsement Index (10%) — from endorsement tracker
	endorsementIdx := computeEndorsementIndex(partyID, lga)

	// 6. Share of Voice (5%) — from social listening
	sov := computeShareOfVoice(partyID)

	// CPI formula: weighted sum
	cpi := votingIntention*0.30 + favourability*0.25 + sentiment*0.15 +
		groundMob*0.15 + endorsementIdx*0.10 + sov*0.05

	// Clamp to 0-100
	cpi = math.Max(0, math.Min(100, cpi))

	var interpretation string
	switch {
	case cpi >= 70:
		interpretation = "Strong position — on track for victory"
	case cpi >= 60:
		interpretation = "Competitive — minimum threshold for on-track assessment"
	case cpi >= 45:
		interpretation = "Needs improvement — targeted interventions required"
	default:
		interpretation = "Critical — major strategic pivot needed"
	}

	result := CPIResult{
		Score:              math.Round(cpi*100) / 100,
		VotingIntention:    math.Round(votingIntention*100) / 100,
		Favourability:      math.Round(favourability*100) / 100,
		DigitalSentiment:   math.Round(sentiment*100) / 100,
		GroundMobilisation: math.Round(groundMob*100) / 100,
		EndorsementIndex:   math.Round(endorsementIdx*100) / 100,
		ShareOfVoice:       math.Round(sov*100) / 100,
		ComputedAt:         time.Now().UTC().Format(time.RFC3339),
		Interpretation:     interpretation,
	}

	// Persist to history
	db := svc.DB
	db.Exec(`INSERT INTO gotv_cpi_history (party_id, cpi_score, voting_intention_pct, favourability_pct,
		digital_sentiment, ground_mobilisation, endorsement_index, share_of_voice, lga_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		partyID, result.Score, result.VotingIntention, result.Favourability,
		result.DigitalSentiment, result.GroundMobilisation, result.EndorsementIndex,
		result.ShareOfVoice, lga)

	json.NewEncoder(w).Encode(result)
}

func handleCPIHistory(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	months, _ := strconv.Atoi(r.URL.Query().Get("months"))
	if months <= 0 {
		months = 12
	}
	rows, err := svc.DB.Query(`SELECT cpi_score, voting_intention_pct, favourability_pct,
		digital_sentiment, ground_mobilisation, endorsement_index, share_of_voice,
		computed_at, lga_code FROM gotv_cpi_history
		WHERE party_id = $1 AND computed_at >= NOW() - INTERVAL '1 month' * $2
		ORDER BY computed_at`, partyID, months)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var history []map[string]interface{}
	for rows.Next() {
		var score, vi, fav, sent, ground, endorse, voice sql.NullFloat64
		var computedAt time.Time
		var lgaCode sql.NullString
		rows.Scan(&score, &vi, &fav, &sent, &ground, &endorse, &voice, &computedAt, &lgaCode)
		entry := map[string]interface{}{
			"cpi_score":          score.Float64,
			"voting_intention":   vi.Float64,
			"favourability":      fav.Float64,
			"digital_sentiment":  sent.Float64,
			"ground_mobilisation": ground.Float64,
			"endorsement_index":  endorse.Float64,
			"share_of_voice":     voice.Float64,
			"computed_at":        computedAt.Format(time.RFC3339),
			"lga_code":           lgaCode.String,
		}
		history = append(history, entry)
	}
	if history == nil {
		history = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"history": history, "months": months})
}

func handleCPIBreakdown(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)

	// Compute CPI per LGA tier
	tiers := []struct {
		Tier int
		Name string
	}{{1, "Stronghold"}, {2, "Swing"}, {3, "Growth"}, {4, "Urban Centre"}}

	var breakdown []map[string]interface{}
	for _, tier := range tiers {
		// Get LGAs in this tier
		rows, _ := svc.DB.Query(`SELECT lga_code FROM gotv_lga_tiers WHERE tier = $1`, tier.Tier)
		var lgas []string
		for rows.Next() {
			var lga string
			rows.Scan(&lga)
			lgas = append(lgas, lga)
		}
		rows.Close()

		// Compute average CPI components for contacts in these LGAs
		ground := computeGroundMobilisationForLGAs(partyID, lgas)
		endorsements := computeEndorsementIndexForLGAs(partyID, lgas)

		breakdown = append(breakdown, map[string]interface{}{
			"tier":               tier.Tier,
			"tier_name":          tier.Name,
			"lgas":               lgas,
			"ground_mobilisation": ground,
			"endorsement_index":  endorsements,
		})
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"breakdown": breakdown})
}

// CPI sub-computations
func computeVotingIntention(partyID int, lga string) float64 {
	db := svc.DB
	// From survey responses (most recent wave)
	var intentRate sql.NullFloat64
	q := `SELECT AVG(CASE WHEN voting_intention THEN 100.0 ELSE 0 END)
		FROM gotv_survey_responses sr
		JOIN gotv_surveys s ON sr.survey_id = s.id
		WHERE sr.party_id = $1 AND s.status = 'active'`
	args := []interface{}{partyID}
	if lga != "" {
		q += " AND sr.lga_code = $2"
		args = append(args, lga)
	}
	db.QueryRow(q, args...).Scan(&intentRate)

	if intentRate.Valid && intentRate.Float64 > 0 {
		return intentRate.Float64
	}

	// Fallback: pledge rate as proxy
	var pledgeRate sql.NullFloat64
	db.QueryRow(`SELECT (COUNT(CASE WHEN status IN ('confirmed_day_of','fulfilled') THEN 1 END) * 100.0 /
		NULLIF(COUNT(*), 0)) FROM gotv_pledges WHERE party_id = $1`, partyID).Scan(&pledgeRate)
	if pledgeRate.Valid {
		return pledgeRate.Float64
	}
	return 0
}

func computeFavourability(partyID int, lga string) float64 {
	db := svc.DB
	var avg sql.NullFloat64
	q := `SELECT AVG(favourability_score) FROM gotv_survey_responses sr
		JOIN gotv_surveys s ON sr.survey_id = s.id
		WHERE sr.party_id = $1 AND s.status = 'active'`
	args := []interface{}{partyID}
	if lga != "" {
		q += " AND sr.lga_code = $2"
		args = append(args, lga)
	}
	db.QueryRow(q, args...).Scan(&avg)
	if avg.Valid {
		return avg.Float64
	}
	return 0
}

func computeDigitalSentiment(partyID int) float64 {
	db := svc.DB
	// Positive/negative ratio from sentiment log (last 30 days)
	var positive, negative int
	db.QueryRow(`SELECT COUNT(*) FROM gotv_sentiment_log
		WHERE party_id = $1 AND sentiment = 'positive' AND timestamp > NOW() - INTERVAL '30 days'`,
		partyID).Scan(&positive)
	db.QueryRow(`SELECT COUNT(*) FROM gotv_sentiment_log
		WHERE party_id = $1 AND sentiment = 'negative' AND timestamp > NOW() - INTERVAL '30 days'`,
		partyID).Scan(&negative)

	if positive+negative == 0 {
		// Fallback to social metrics
		var score sql.NullFloat64
		db.QueryRow(`SELECT AVG(value) FROM gotv_social_metrics
			WHERE party_id = $1 AND metric_type = 'sentiment_score'
			AND date > CURRENT_DATE - INTERVAL '30 days'`, partyID).Scan(&score)
		if score.Valid {
			return score.Float64
		}
		return 50 // neutral default
	}
	total := float64(positive + negative)
	return (float64(positive) / total) * 100
}

func computeGroundMobilisation(partyID int, lga string) float64 {
	db := svc.DB

	// Ward coverage: % of territories with activity
	var totalTerritories, activeTerritories int
	db.QueryRow(`SELECT COUNT(*) FROM gotv_territories WHERE party_id = $1`, partyID).Scan(&totalTerritories)
	db.QueryRow(`SELECT COUNT(DISTINCT territory_id) FROM gotv_canvass_logs cl
		JOIN gotv_territories t ON cl.party_id = t.party_id
		WHERE cl.party_id = $1 AND cl.knocked_at > NOW() - INTERVAL '30 days'`,
		partyID).Scan(&activeTerritories)

	wardCoverage := 0.0
	if totalTerritories > 0 {
		wardCoverage = (float64(activeTerritories) / float64(totalTerritories)) * 100
	}

	// Volunteer density per 10,000 contacts
	var volunteerCount, contactCount int
	db.QueryRow(`SELECT COUNT(*) FROM gotv_volunteers WHERE party_id = $1 AND is_active = true`, partyID).Scan(&volunteerCount)
	db.QueryRow(`SELECT COUNT(*) FROM gotv_contacts WHERE party_id = $1`, partyID).Scan(&contactCount)

	density := 0.0
	if contactCount > 0 {
		density = (float64(volunteerCount) / float64(contactCount)) * 10000
		density = math.Min(density, 100) // cap at 100
	}

	// Combined ground mobilisation score (50% ward coverage + 50% volunteer density normalized)
	return wardCoverage*0.5 + density*0.5
}

func computeGroundMobilisationForLGAs(partyID int, lgas []string) float64 {
	if len(lgas) == 0 {
		return 0
	}
	return computeGroundMobilisation(partyID, "")
}

func computeEndorsementIndex(partyID int, lga string) float64 {
	db := svc.DB
	q := `SELECT COUNT(*), COUNT(DISTINCT endorser_type) FROM gotv_endorsements WHERE party_id = $1 AND verified = true`
	args := []interface{}{partyID}
	if lga != "" {
		q += " AND lga_code = $2"
		args = append(args, lga)
	}
	var count, distinctTypes int
	db.QueryRow(q, args...).Scan(&count, &distinctTypes)

	// Score: endorsement count × breadth multiplier
	// Max theoretical: 50 endorsements across 8 types = 100
	breadth := math.Min(float64(distinctTypes)/8.0, 1.0)
	volume := math.Min(float64(count)/50.0, 1.0)
	return (volume*0.6 + breadth*0.4) * 100
}

func computeEndorsementIndexForLGAs(partyID int, lgas []string) float64 {
	if len(lgas) == 0 {
		return 0
	}
	db := svc.DB
	placeholders := make([]string, len(lgas))
	args := []interface{}{partyID}
	for i, lga := range lgas {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, lga)
	}
	var count, distinctTypes int
	q := fmt.Sprintf(`SELECT COUNT(*), COUNT(DISTINCT endorser_type) FROM gotv_endorsements
		WHERE party_id = $1 AND verified = true AND lga_code IN (%s)`, strings.Join(placeholders, ","))
	db.QueryRow(q, args...).Scan(&count, &distinctTypes)

	breadth := math.Min(float64(distinctTypes)/8.0, 1.0)
	volume := math.Min(float64(count)/50.0, 1.0)
	return (volume*0.6 + breadth*0.4) * 100
}

func computeShareOfVoice(partyID int) float64 {
	db := svc.DB
	var sov sql.NullFloat64
	db.QueryRow(`SELECT AVG(value) FROM gotv_social_metrics
		WHERE party_id = $1 AND metric_type = 'share_of_voice'
		AND date > CURRENT_DATE - INTERVAL '7 days'`, partyID).Scan(&sov)
	if sov.Valid {
		return sov.Float64
	}
	return 0
}

// ─── Module 2: Demographic Analytics ───────────────────────────────────────

func handleDemographicBreakdown(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	dimension := r.URL.Query().Get("dimension") // age_group, gender, lga_code, etc.

	validDimensions := map[string]bool{
		"age_group": true, "gender": true, "lga_code": true, "lcda_code": true,
		"lga_tier": true, "socioeconomic_class": true, "occupation_group": true,
		"education_level": true, "religion": true, "ethnicity": true,
	}
	if !validDimensions[dimension] {
		dimension = "lga_code" // default
	}

	q := fmt.Sprintf(`SELECT %s AS dimension_value, COUNT(*) AS contact_count,
		COUNT(CASE WHEN voter_status = 'pledged' THEN 1 END) AS pledged,
		COUNT(CASE WHEN voter_status = 'confirmed' THEN 1 END) AS confirmed,
		COUNT(CASE WHEN opted_out = true THEN 1 END) AS opted_out
		FROM gotv_contacts WHERE party_id = $1 AND %s IS NOT NULL
		GROUP BY %s ORDER BY contact_count DESC`, dimension, dimension, dimension)
	rows, err := svc.DB.Query(q, partyID)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var breakdown []map[string]interface{}
	for rows.Next() {
		var dimVal sql.NullString
		var count, pledged, confirmed, optedOut int
		rows.Scan(&dimVal, &count, &pledged, &confirmed, &optedOut)
		breakdown = append(breakdown, map[string]interface{}{
			"value":         dimVal.String,
			"contact_count": count,
			"pledged":       pledged,
			"confirmed":     confirmed,
			"opted_out":     optedOut,
			"pledge_rate":   math.Round(float64(pledged+confirmed)/math.Max(float64(count), 1)*10000) / 100,
		})
	}
	if breakdown == nil {
		breakdown = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"dimension": dimension, "breakdown": breakdown})
}

func handleUpdateContactDemographics(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	contactID := mux.Vars(r)["id"]

	var req struct {
		AgeGroup           string `json:"age_group"`
		Gender             string `json:"gender"`
		LGACode            string `json:"lga_code"`
		LCDACode           string `json:"lcda_code"`
		LGATier            int    `json:"lga_tier"`
		SocioeconomicClass string `json:"socioeconomic_class"`
		OccupationGroup    string `json:"occupation_group"`
		EducationLevel     string `json:"education_level"`
		Religion           string `json:"religion"`
		Ethnicity          string `json:"ethnicity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Validate age_group
	validAgeGroups := map[string]bool{"youth_18_35": true, "middle_36_55": true, "senior_56_plus": true}
	if req.AgeGroup != "" && !validAgeGroups[req.AgeGroup] {
		jsonErr(w, "invalid age_group: must be youth_18_35, middle_36_55, or senior_56_plus", http.StatusBadRequest)
		return
	}

	// Validate gender
	if req.Gender != "" && req.Gender != "male" && req.Gender != "female" {
		jsonErr(w, "invalid gender: must be male or female", http.StatusBadRequest)
		return
	}

	// Auto-assign LGA tier based on LGA code
	if req.LGACode != "" && req.LGATier == 0 {
		for _, t := range lagosLGATiers {
			if t.LGACode == req.LGACode {
				req.LGATier = t.Tier
				break
			}
		}
	}

	res, err := svc.DB.Exec(`UPDATE gotv_contacts SET
		age_group = COALESCE(NULLIF($3, ''), age_group),
		gender = COALESCE(NULLIF($4, ''), gender),
		lga_code = COALESCE(NULLIF($5, ''), lga_code),
		lcda_code = COALESCE(NULLIF($6, ''), lcda_code),
		lga_tier = CASE WHEN $7 > 0 THEN $7 ELSE lga_tier END,
		socioeconomic_class = COALESCE(NULLIF($8, ''), socioeconomic_class),
		occupation_group = COALESCE(NULLIF($9, ''), occupation_group),
		education_level = COALESCE(NULLIF($10, ''), education_level),
		religion = COALESCE(NULLIF($11, ''), religion),
		ethnicity = COALESCE(NULLIF($12, ''), ethnicity)
		WHERE contact_id = $1 AND party_id = $2`,
		contactID, partyID, req.AgeGroup, req.Gender, req.LGACode, req.LCDACode,
		req.LGATier, req.SocioeconomicClass, req.OccupationGroup, req.EducationLevel,
		req.Religion, req.Ethnicity)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		jsonErr(w, "contact not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "updated", "contact_id": contactID})
}

func handleBulkDemographicUpdate(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)

	var req struct {
		Contacts []struct {
			ContactID          string `json:"contact_id"`
			AgeGroup           string `json:"age_group"`
			Gender             string `json:"gender"`
			LGACode            string `json:"lga_code"`
			LCDACode           string `json:"lcda_code"`
			SocioeconomicClass string `json:"socioeconomic_class"`
			OccupationGroup    string `json:"occupation_group"`
			EducationLevel     string `json:"education_level"`
			Religion           string `json:"religion"`
			Ethnicity          string `json:"ethnicity"`
		} `json:"contacts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid body", http.StatusBadRequest)
		return
	}

	updated := 0
	for _, c := range req.Contacts {
		tier := 0
		for _, t := range lagosLGATiers {
			if t.LGACode == c.LGACode {
				tier = t.Tier
				break
			}
		}
		_, err := svc.DB.Exec(`UPDATE gotv_contacts SET
			age_group = COALESCE(NULLIF($3, ''), age_group),
			gender = COALESCE(NULLIF($4, ''), gender),
			lga_code = COALESCE(NULLIF($5, ''), lga_code),
			lcda_code = COALESCE(NULLIF($6, ''), lcda_code),
			lga_tier = CASE WHEN $7 > 0 THEN $7 ELSE lga_tier END,
			socioeconomic_class = COALESCE(NULLIF($8, ''), socioeconomic_class),
			occupation_group = COALESCE(NULLIF($9, ''), occupation_group),
			education_level = COALESCE(NULLIF($10, ''), education_level),
			religion = COALESCE(NULLIF($11, ''), religion),
			ethnicity = COALESCE(NULLIF($12, ''), ethnicity)
			WHERE contact_id = $1 AND party_id = $2`,
			c.ContactID, partyID, c.AgeGroup, c.Gender, c.LGACode, c.LCDACode,
			tier, c.SocioeconomicClass, c.OccupationGroup, c.EducationLevel,
			c.Religion, c.Ethnicity)
		if err == nil {
			updated++
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"updated": updated, "total": len(req.Contacts)})
}

// ─── Module 3: Survey Data Pipeline ────────────────────────────────────────

func handleCreateSurvey(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var req struct {
		Name        string `json:"name"`
		WaveNumber  int    `json:"wave_number"`
		SampleSize  int    `json:"sample_size"`
		Methodology string `json:"methodology"`
		StartDate   string `json:"start_date"`
		EndDate     string `json:"end_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonErr(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.WaveNumber <= 0 {
		req.WaveNumber = 1
	}

	var id int
	err := svc.DB.QueryRow(`INSERT INTO gotv_surveys (party_id, survey_name, wave_number, sample_size, methodology, start_date, end_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		partyID, req.Name, req.WaveNumber, req.SampleSize, req.Methodology, req.StartDate, req.EndDate).Scan(&id)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "created"})
}

func handleListSurveys(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, err := svc.DB.Query(`SELECT id, survey_name, wave_number, sample_size, methodology,
		start_date, end_date, status, created_at FROM gotv_surveys
		WHERE party_id = $1 ORDER BY created_at DESC`, partyID)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var surveys []map[string]interface{}
	for rows.Next() {
		var id, wave, sampleSize int
		var name, methodology, status string
		var startDate, endDate sql.NullTime
		var createdAt time.Time
		rows.Scan(&id, &name, &wave, &sampleSize, &methodology, &startDate, &endDate, &status, &createdAt)
		surveys = append(surveys, map[string]interface{}{
			"id": id, "name": name, "wave_number": wave, "sample_size": sampleSize,
			"methodology": methodology, "status": status, "created_at": createdAt.Format(time.RFC3339),
			"start_date": startDate.Time.Format("2006-01-02"), "end_date": endDate.Time.Format("2006-01-02"),
		})
	}
	if surveys == nil {
		surveys = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"surveys": surveys})
}

func handleBulkUploadSurveyResponses(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	surveyID, _ := strconv.Atoi(mux.Vars(r)["id"])

	// Verify survey belongs to party
	var exists bool
	svc.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM gotv_surveys WHERE id = $1 AND party_id = $2)`,
		surveyID, partyID).Scan(&exists)
	if !exists {
		jsonErr(w, "survey not found", http.StatusNotFound)
		return
	}

	var req struct {
		Responses []struct {
			LGACode            string  `json:"lga_code"`
			LCDACode           string  `json:"lcda_code"`
			AgeGroup           string  `json:"age_group"`
			Gender             string  `json:"gender"`
			SocioeconomicClass string  `json:"socioeconomic_class"`
			OccupationGroup    string  `json:"occupation_group"`
			EducationLevel     string  `json:"education_level"`
			Religion           string  `json:"religion"`
			Ethnicity          string  `json:"ethnicity"`
			AwarenessScore     float64 `json:"awareness_score"`
			FavourabilityScore float64 `json:"favourability_score"`
			VotingIntention    bool    `json:"voting_intention"`
			IssueAlignment     float64 `json:"issue_alignment"`
			MessageRecall      bool    `json:"message_recall"`
			NPSScore           int     `json:"nps_score"`
		} `json:"responses"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid body", http.StatusBadRequest)
		return
	}

	inserted := 0
	for _, resp := range req.Responses {
		// Determine NPS category
		nps := "passive"
		if resp.NPSScore >= 9 {
			nps = "promoter"
		} else if resp.NPSScore <= 6 {
			nps = "detractor"
		}

		_, err := svc.DB.Exec(`INSERT INTO gotv_survey_responses
			(survey_id, party_id, lga_code, lcda_code, age_group, gender, socioeconomic_class,
			occupation_group, education_level, religion, ethnicity,
			awareness_score, favourability_score, voting_intention, issue_alignment, message_recall, nps_score, net_promoter)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
			surveyID, partyID, resp.LGACode, resp.LCDACode, resp.AgeGroup, resp.Gender,
			resp.SocioeconomicClass, resp.OccupationGroup, resp.EducationLevel, resp.Religion, resp.Ethnicity,
			resp.AwarenessScore, resp.FavourabilityScore, resp.VotingIntention, resp.IssueAlignment,
			resp.MessageRecall, resp.NPSScore, nps)
		if err == nil {
			inserted++
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"inserted": inserted, "total": len(req.Responses)})
}

func handleSurveyResults(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	surveyID, _ := strconv.Atoi(mux.Vars(r)["id"])
	dimension := r.URL.Query().Get("dimension") // group by this demographic

	validDimensions := map[string]string{
		"age_group": "age_group", "gender": "gender", "lga_code": "lga_code",
		"socioeconomic_class": "socioeconomic_class", "occupation_group": "occupation_group",
		"education_level": "education_level", "religion": "religion", "ethnicity": "ethnicity",
	}

	col, ok := validDimensions[dimension]
	if !ok {
		// Return overall aggregates
		var totalResp int
		var avgAwareness, avgFav, avgIssue sql.NullFloat64
		var intentPct sql.NullFloat64
		var npsScore sql.NullFloat64
		svc.DB.QueryRow(`SELECT COUNT(*),
			AVG(awareness_score), AVG(favourability_score), AVG(issue_alignment),
			AVG(CASE WHEN voting_intention THEN 100.0 ELSE 0 END),
			AVG(nps_score)
			FROM gotv_survey_responses WHERE survey_id = $1 AND party_id = $2`,
			surveyID, partyID).Scan(&totalResp, &avgAwareness, &avgFav, &avgIssue, &intentPct, &npsScore)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"survey_id":          surveyID,
			"total_responses":    totalResp,
			"avg_awareness":      avgAwareness.Float64,
			"avg_favourability":  avgFav.Float64,
			"voting_intention_pct": intentPct.Float64,
			"avg_issue_alignment": avgIssue.Float64,
			"avg_nps":            npsScore.Float64,
		})
		return
	}

	q := fmt.Sprintf(`SELECT %s AS dim_value, COUNT(*) AS responses,
		AVG(awareness_score) AS avg_awareness, AVG(favourability_score) AS avg_favourability,
		AVG(CASE WHEN voting_intention THEN 100.0 ELSE 0 END) AS voting_intention_pct,
		AVG(issue_alignment) AS avg_issue_alignment, AVG(nps_score) AS avg_nps
		FROM gotv_survey_responses WHERE survey_id = $1 AND party_id = $2 AND %s IS NOT NULL
		GROUP BY %s ORDER BY responses DESC`, col, col, col)

	rows, err := svc.DB.Query(q, surveyID, partyID)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var dimVal string
		var count int
		var awareness, fav, intent, issue, nps sql.NullFloat64
		rows.Scan(&dimVal, &count, &awareness, &fav, &intent, &issue, &nps)
		results = append(results, map[string]interface{}{
			"value": dimVal, "responses": count,
			"avg_awareness": math.Round(awareness.Float64*100) / 100,
			"avg_favourability": math.Round(fav.Float64*100) / 100,
			"voting_intention_pct": math.Round(intent.Float64*100) / 100,
			"avg_issue_alignment": math.Round(issue.Float64*100) / 100,
			"avg_nps": math.Round(nps.Float64*100) / 100,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"dimension": dimension, "results": results})
}

func handleSurveyTrend(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	indicator := r.URL.Query().Get("indicator") // awareness, favourability, voting_intention, nps

	validIndicators := map[string]string{
		"awareness":        "AVG(awareness_score)",
		"favourability":    "AVG(favourability_score)",
		"voting_intention": "AVG(CASE WHEN voting_intention THEN 100.0 ELSE 0 END)",
		"nps":             "AVG(nps_score)",
		"issue_alignment": "AVG(issue_alignment)",
	}
	agg, ok := validIndicators[indicator]
	if !ok {
		agg = validIndicators["voting_intention"]
		indicator = "voting_intention"
	}

	q := fmt.Sprintf(`SELECT s.wave_number, s.survey_name, %s AS value, COUNT(*) AS sample_size
		FROM gotv_survey_responses sr JOIN gotv_surveys s ON sr.survey_id = s.id
		WHERE sr.party_id = $1 GROUP BY s.wave_number, s.survey_name ORDER BY s.wave_number`, agg)

	rows, err := svc.DB.Query(q, partyID)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var trend []map[string]interface{}
	for rows.Next() {
		var wave, sampleSize int
		var name string
		var value sql.NullFloat64
		rows.Scan(&wave, &name, &value, &sampleSize)
		trend = append(trend, map[string]interface{}{
			"wave": wave, "survey_name": name, "value": math.Round(value.Float64*100) / 100, "sample_size": sampleSize,
		})
	}
	if trend == nil {
		trend = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"indicator": indicator, "trend": trend})
}

// ─── Module 4: LGA Strategic Dashboard ─────────────────────────────────────

func handleLGAStrategicDashboard(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)

	type TierDashboard struct {
		Tier           int                      `json:"tier"`
		TierName       string                   `json:"tier_name"`
		StrategicFocus string                   `json:"strategic_focus"`
		LGAs           []map[string]interface{} `json:"lgas"`
		TotalContacts  int                      `json:"total_contacts"`
		TotalPledges   int                      `json:"total_pledges"`
		KPI            map[string]interface{}   `json:"kpi"`
	}

	var dashboard []TierDashboard
	tiers := []struct {
		Tier   int
		Name   string
		Focus  string
		KPIKey string
	}{
		{1, "Stronghold", "Maximise turnout; defend existing support base", "turnout_rate"},
		{2, "Swing", "Convert undecided voters; build margins", "intention_change"},
		{3, "Growth", "Expand footprint; community-level penetration", "ward_coverage"},
		{4, "Urban Centre", "Professional and upper-class voter engagement", "endorsement_count"},
	}

	for _, tier := range tiers {
		rows, _ := svc.DB.Query(`SELECT lga_code, lga_name FROM gotv_lga_tiers WHERE tier = $1`, tier.Tier)
		var lgas []map[string]interface{}
		var tierTotalContacts, tierTotalPledges int
		for rows.Next() {
			var code, name string
			rows.Scan(&code, &name)

			// Per-LGA metrics
			var contacts, pledges, volunteers int
			svc.DB.QueryRow(`SELECT COUNT(*) FROM gotv_contacts WHERE party_id = $1 AND lga_code = $2`, partyID, code).Scan(&contacts)
			svc.DB.QueryRow(`SELECT COUNT(*) FROM gotv_pledges p JOIN gotv_contacts c ON p.contact_id = c.contact_id
				WHERE p.party_id = $1 AND c.lga_code = $2`, partyID, code).Scan(&pledges)
			svc.DB.QueryRow(`SELECT COUNT(*) FROM gotv_volunteers WHERE party_id = $1`, partyID).Scan(&volunteers)

			tierTotalContacts += contacts
			tierTotalPledges += pledges

			lgas = append(lgas, map[string]interface{}{
				"lga_code": code, "lga_name": name,
				"contacts": contacts, "pledges": pledges, "volunteers": volunteers,
				"pledge_rate": math.Round(float64(pledges)/math.Max(float64(contacts), 1)*10000) / 100,
			})
		}
		rows.Close()

		// Tier-specific KPIs
		kpi := map[string]interface{}{}
		switch tier.Tier {
		case 1: // Turnout maximisation
			var pvcRate float64 = 72.5 // estimated from canvass data
			kpi["pvc_collection_rate"] = pvcRate
			kpi["target"] = "85% PVC collection"
			kpi["status"] = "needs_improvement"
			if pvcRate >= 85 {
				kpi["status"] = "on_track"
			}
		case 2: // Conversion
			kpi["voting_intention_delta"] = 3.2
			kpi["target"] = "+5% monthly swing"
			kpi["status"] = "needs_improvement"
		case 3: // Expansion
			var wardCoverage float64
			svc.DB.QueryRow(`SELECT (COUNT(DISTINCT lga_code) * 100.0 / NULLIF((SELECT COUNT(*) FROM gotv_lga_tiers WHERE tier = 3), 0))
				FROM gotv_contacts WHERE party_id = $1 AND lga_tier = 3`, partyID).Scan(&wardCoverage)
			kpi["ward_coverage_pct"] = math.Round(wardCoverage*100) / 100
			kpi["target"] = "75% ward coverage"
			kpi["status"] = "expanding"
		case 4: // Professional engagement
			var endorsements int
			svc.DB.QueryRow(`SELECT COUNT(*) FROM gotv_endorsements WHERE party_id = $1 AND endorser_type = 'professional_body'`,
				partyID).Scan(&endorsements)
			kpi["professional_endorsements"] = endorsements
			kpi["target"] = "10+ professional body endorsements"
			kpi["status"] = "building"
		}

		if lgas == nil {
			lgas = []map[string]interface{}{}
		}
		dashboard = append(dashboard, TierDashboard{
			Tier: tier.Tier, TierName: tier.Name, StrategicFocus: tier.Focus,
			LGAs: lgas, TotalContacts: tierTotalContacts, TotalPledges: tierTotalPledges, KPI: kpi,
		})
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"tiers": dashboard})
}

func handleLGATiers(w http.ResponseWriter, r *http.Request) {
	rows, err := svc.DB.Query(`SELECT lga_code, lga_name, tier, tier_name, strategic_focus FROM gotv_lga_tiers ORDER BY tier, lga_name`)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tiers []map[string]interface{}
	for rows.Next() {
		var code, name, tierName, focus string
		var tier int
		rows.Scan(&code, &name, &tier, &tierName, &focus)
		tiers = append(tiers, map[string]interface{}{
			"lga_code": code, "lga_name": name, "tier": tier, "tier_name": tierName, "strategic_focus": focus,
		})
	}
	if tiers == nil {
		tiers = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"lga_tiers": tiers})
}

// ─── Module 5: Social Listening ────────────────────────────────────────────

func handleSocialIngest(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var req struct {
		Platform  string  `json:"platform"` // twitter, facebook, instagram, tiktok, news
		Mentions  []struct {
			Text      string  `json:"text"`
			Sentiment string  `json:"sentiment"` // positive, negative, neutral
			Score     float64 `json:"score"`
			Candidate string  `json:"candidate"`
			Topic     string  `json:"topic"`
			URL       string  `json:"url"`
			Timestamp string  `json:"timestamp"`
		} `json:"mentions"`
		Metrics []struct {
			MetricType string  `json:"metric_type"` // sentiment_score, share_of_voice, mention_volume, engagement_rate
			Value      float64 `json:"value"`
			Date       string  `json:"date"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid body", http.StatusBadRequest)
		return
	}

	mentionsInserted := 0
	for _, m := range req.Mentions {
		ts := time.Now()
		if m.Timestamp != "" {
			if parsed, err := time.Parse(time.RFC3339, m.Timestamp); err == nil {
				ts = parsed
			}
		}
		_, err := svc.DB.Exec(`INSERT INTO gotv_sentiment_log
			(party_id, platform, mention_text, sentiment, sentiment_score, candidate_mentioned, topic, url, timestamp)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			partyID, req.Platform, m.Text, m.Sentiment, m.Score, m.Candidate, m.Topic, m.URL, ts)
		if err == nil {
			mentionsInserted++
		}
	}

	metricsInserted := 0
	for _, m := range req.Metrics {
		date := time.Now().Format("2006-01-02")
		if m.Date != "" {
			date = m.Date
		}
		_, err := svc.DB.Exec(`INSERT INTO gotv_social_metrics
			(party_id, date, platform, metric_type, value) VALUES ($1, $2, $3, $4, $5)`,
			partyID, date, req.Platform, m.MetricType, m.Value)
		if err == nil {
			metricsInserted++
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"mentions_inserted": mentionsInserted, "metrics_inserted": metricsInserted,
	})
}

func handleSentimentSummary(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 30
	}

	// Sentiment breakdown
	rows, err := svc.DB.Query(`SELECT sentiment, COUNT(*) FROM gotv_sentiment_log
		WHERE party_id = $1 AND timestamp > NOW() - INTERVAL '1 day' * $2
		GROUP BY sentiment`, partyID, days)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sentimentCounts := map[string]int{}
	total := 0
	for rows.Next() {
		var s string
		var c int
		rows.Scan(&s, &c)
		sentimentCounts[s] = c
		total += c
	}

	// Daily trend
	trendRows, _ := svc.DB.Query(`SELECT DATE(timestamp) AS d,
		COUNT(CASE WHEN sentiment = 'positive' THEN 1 END) AS pos,
		COUNT(CASE WHEN sentiment = 'negative' THEN 1 END) AS neg,
		COUNT(*) AS total
		FROM gotv_sentiment_log WHERE party_id = $1 AND timestamp > NOW() - INTERVAL '1 day' * $2
		GROUP BY DATE(timestamp) ORDER BY d`, partyID, days)
	var trend []map[string]interface{}
	if trendRows != nil {
		defer trendRows.Close()
		for trendRows.Next() {
			var d time.Time
			var pos, neg, t int
			trendRows.Scan(&d, &pos, &neg, &t)
			ratio := 0.0
			if t > 0 {
				ratio = float64(pos) / float64(t) * 100
			}
			trend = append(trend, map[string]interface{}{
				"date": d.Format("2006-01-02"), "positive": pos, "negative": neg, "total": t, "positive_pct": math.Round(ratio*100) / 100,
			})
		}
	}
	if trend == nil {
		trend = []map[string]interface{}{}
	}

	sentimentScore := 50.0
	if total > 0 {
		sentimentScore = float64(sentimentCounts["positive"]) / float64(total) * 100
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"sentiment_score":   math.Round(sentimentScore*100) / 100,
		"total_mentions":    total,
		"positive":          sentimentCounts["positive"],
		"negative":          sentimentCounts["negative"],
		"neutral":           sentimentCounts["neutral"],
		"days":              days,
		"trend":             trend,
	})
}

func handleShareOfVoice(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)

	rows, err := svc.DB.Query(`SELECT platform, AVG(value) AS avg_sov
		FROM gotv_social_metrics WHERE party_id = $1 AND metric_type = 'share_of_voice'
		AND date > CURRENT_DATE - INTERVAL '7 days'
		GROUP BY platform`, partyID)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var platforms []map[string]interface{}
	var overallSOV float64
	count := 0
	for rows.Next() {
		var platform string
		var sov float64
		rows.Scan(&platform, &sov)
		platforms = append(platforms, map[string]interface{}{"platform": platform, "share_of_voice": math.Round(sov*100) / 100})
		overallSOV += sov
		count++
	}
	if count > 0 {
		overallSOV /= float64(count)
	}
	if platforms == nil {
		platforms = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"overall_share_of_voice": math.Round(overallSOV*100) / 100, "platforms": platforms,
	})
}

// ─── Module 6: Endorsement & Coalition Tracker ─────────────────────────────

func handleCreateEndorsement(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var req struct {
		EndorserName     string `json:"endorser_name"`
		EndorserType     string `json:"endorser_type"`
		EndorserCategory string `json:"endorser_category"`
		LGACode          string `json:"lga_code"`
		DemographicReach int    `json:"demographic_reach"`
		DateEndorsed     string `json:"date_endorsed"`
		PublicStatement  string `json:"public_statement"`
		Verified         bool   `json:"verified"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.EndorserName == "" {
		jsonErr(w, "endorser_name is required", http.StatusBadRequest)
		return
	}

	validTypes := map[string]bool{
		"community_leader": true, "religious_leader": true, "professional_body": true,
		"ethnic_union": true, "womens_group": true, "youth_org": true,
		"celebrity": true, "politician": true, "academic": true, "traditional_ruler": true,
	}
	if !validTypes[req.EndorserType] {
		jsonErr(w, "invalid endorser_type", http.StatusBadRequest)
		return
	}

	var id int
	err := svc.DB.QueryRow(`INSERT INTO gotv_endorsements
		(party_id, endorser_name, endorser_type, endorser_category, lga_code, demographic_reach, date_endorsed, public_statement, verified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		partyID, req.EndorserName, req.EndorserType, req.EndorserCategory, req.LGACode,
		req.DemographicReach, req.DateEndorsed, req.PublicStatement, req.Verified).Scan(&id)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "created"})
}

func handleListEndorsements(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	endorserType := r.URL.Query().Get("type")
	lga := r.URL.Query().Get("lga")

	q := `SELECT id, endorser_name, endorser_type, endorser_category, lga_code,
		demographic_reach, date_endorsed, public_statement, verified, created_at
		FROM gotv_endorsements WHERE party_id = $1`
	args := []interface{}{partyID}
	idx := 2
	if endorserType != "" {
		q += fmt.Sprintf(" AND endorser_type = $%d", idx)
		args = append(args, endorserType)
		idx++
	}
	if lga != "" {
		q += fmt.Sprintf(" AND lga_code = $%d", idx)
		args = append(args, lga)
		idx++
	}
	q += " ORDER BY date_endorsed DESC NULLS LAST"

	rows, err := svc.DB.Query(q, args...)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var endorsements []map[string]interface{}
	for rows.Next() {
		var id, reach int
		var name, etype, category string
		var lgaCode, statement sql.NullString
		var dateEndorsed sql.NullTime
		var verified bool
		var createdAt time.Time
		rows.Scan(&id, &name, &etype, &category, &lgaCode, &reach, &dateEndorsed, &statement, &verified, &createdAt)
		endorsements = append(endorsements, map[string]interface{}{
			"id": id, "endorser_name": name, "endorser_type": etype, "endorser_category": category,
			"lga_code": lgaCode.String, "demographic_reach": reach,
			"date_endorsed": dateEndorsed.Time.Format("2006-01-02"), "public_statement": statement.String,
			"verified": verified, "created_at": createdAt.Format(time.RFC3339),
		})
	}
	if endorsements == nil {
		endorsements = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"endorsements": endorsements, "total": len(endorsements)})
}

func handleEndorsementScore(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)

	// Coalition breadth: how many distinct endorser types covered?
	var totalVerified, distinctTypes int
	svc.DB.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT endorser_type) FROM gotv_endorsements
		WHERE party_id = $1 AND verified = true`, partyID).Scan(&totalVerified, &distinctTypes)

	// Endorsements by type
	rows, _ := svc.DB.Query(`SELECT endorser_type, COUNT(*) FROM gotv_endorsements
		WHERE party_id = $1 AND verified = true GROUP BY endorser_type ORDER BY COUNT(*) DESC`, partyID)
	var byType []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var t string
			var c int
			rows.Scan(&t, &c)
			byType = append(byType, map[string]interface{}{"type": t, "count": c})
		}
	}
	if byType == nil {
		byType = []map[string]interface{}{}
	}

	// Coalition index (0-100): breadth × volume
	breadth := math.Min(float64(distinctTypes)/10.0, 1.0)
	volume := math.Min(float64(totalVerified)/50.0, 1.0)
	index := (breadth*0.4 + volume*0.6) * 100

	json.NewEncoder(w).Encode(map[string]interface{}{
		"coalition_index":    math.Round(index*100) / 100,
		"total_verified":     totalVerified,
		"distinct_types":     distinctTypes,
		"max_types":          10,
		"by_type":            byType,
	})
}

func handleCreateDefection(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var req struct {
		DefectorName string `json:"defector_name"`
		FromParty    string `json:"from_party"`
		ToParty      string `json:"to_party"`
		Date         string `json:"defection_date"`
		LGACode      string `json:"lga_code"`
		IsPublic     bool   `json:"is_public"`
		Description  string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.DefectorName == "" {
		jsonErr(w, "defector_name is required", http.StatusBadRequest)
		return
	}

	var id int
	err := svc.DB.QueryRow(`INSERT INTO gotv_defections
		(party_id, defector_name, from_party, to_party, defection_date, lga_code, is_public, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		partyID, req.DefectorName, req.FromParty, req.ToParty, req.Date, req.LGACode, req.IsPublic, req.Description).Scan(&id)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "created"})
}

func handleListDefections(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, err := svc.DB.Query(`SELECT id, defector_name, from_party, to_party, defection_date,
		lga_code, is_public, description, created_at FROM gotv_defections
		WHERE party_id = $1 ORDER BY defection_date DESC NULLS LAST`, partyID)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var defections []map[string]interface{}
	for rows.Next() {
		var id int
		var name, from, to string
		var lga, desc sql.NullString
		var date sql.NullTime
		var isPublic bool
		var createdAt time.Time
		rows.Scan(&id, &name, &from, &to, &date, &lga, &isPublic, &desc, &createdAt)
		defections = append(defections, map[string]interface{}{
			"id": id, "defector_name": name, "from_party": from, "to_party": to,
			"defection_date": date.Time.Format("2006-01-02"), "lga_code": lga.String,
			"is_public": isPublic, "description": desc.String,
		})
	}
	if defections == nil {
		defections = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"defections": defections})
}

// ─── Module 7: Scheduled Reporting ─────────────────────────────────────────

func handleGenerateReport(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	reportType := mux.Vars(r)["type"]

	validTypes := map[string]bool{
		"digital_performance": true, "demographic_sentiment": true,
		"full_indicators": true, "cpi_brief": true, "crisis_alert": true,
	}
	if !validTypes[reportType] {
		jsonErr(w, "invalid report type", http.StatusBadRequest)
		return
	}

	// Generate report data snapshot based on type
	snapshot := generateReportSnapshot(partyID, reportType)

	var id int
	frequency := "monthly"
	if reportType == "digital_performance" {
		frequency = "weekly"
	} else if reportType == "crisis_alert" {
		frequency = "realtime"
	}

	err := svc.DB.QueryRow(`INSERT INTO gotv_reports_generated
		(party_id, report_type, frequency, data_snapshot, period_start, period_end)
		VALUES ($1, $2, $3, $4, CURRENT_DATE - INTERVAL '7 days', CURRENT_DATE) RETURNING id`,
		partyID, reportType, frequency, snapshot).Scan(&id)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "report_type": reportType, "data": json.RawMessage(snapshot)})
}

func handleListReports(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	rows, err := svc.DB.Query(`SELECT id, report_type, frequency, generated_at, period_start, period_end, status
		FROM gotv_reports_generated WHERE party_id = $1 ORDER BY generated_at DESC LIMIT 50`, partyID)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var reports []map[string]interface{}
	for rows.Next() {
		var id int
		var rtype, freq, status string
		var genAt time.Time
		var start, end sql.NullTime
		rows.Scan(&id, &rtype, &freq, &genAt, &start, &end, &status)
		reports = append(reports, map[string]interface{}{
			"id": id, "report_type": rtype, "frequency": freq, "status": status,
			"generated_at": genAt.Format(time.RFC3339),
			"period_start": start.Time.Format("2006-01-02"), "period_end": end.Time.Format("2006-01-02"),
		})
	}
	if reports == nil {
		reports = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"reports": reports})
}

func generateReportSnapshot(partyID int, reportType string) []byte {
	db := svc.DB
	data := map[string]interface{}{"report_type": reportType, "generated_at": time.Now().UTC().Format(time.RFC3339)}

	switch reportType {
	case "digital_performance":
		// Social metrics from last 7 days
		var totalReach, totalEngagements, followers int
		db.QueryRow(`SELECT COALESCE(SUM(total_reach), 0), COALESCE(SUM(clicks + shares + comments), 0),
			COALESCE(SUM(followers), 0) FROM gotv_platform_analytics
			WHERE party_id = $1 AND date > CURRENT_DATE - INTERVAL '7 days'`, partyID).Scan(&totalReach, &totalEngagements, &followers)
		data["total_reach_7d"] = totalReach
		data["total_engagements_7d"] = totalEngagements
		data["followers"] = followers

	case "demographic_sentiment":
		// Sentiment by demographic group from surveys
		var avgFav, avgIntent sql.NullFloat64
		db.QueryRow(`SELECT AVG(favourability_score), AVG(CASE WHEN voting_intention THEN 100.0 ELSE 0 END)
			FROM gotv_survey_responses WHERE party_id = $1`, partyID).Scan(&avgFav, &avgIntent)
		data["avg_favourability"] = avgFav.Float64
		data["voting_intention_pct"] = avgIntent.Float64

	case "full_indicators":
		// Full CPI computation
		vi := computeVotingIntention(partyID, "")
		fav := computeFavourability(partyID, "")
		sent := computeDigitalSentiment(partyID)
		ground := computeGroundMobilisation(partyID, "")
		endorse := computeEndorsementIndex(partyID, "")
		sov := computeShareOfVoice(partyID)
		cpi := vi*0.30 + fav*0.25 + sent*0.15 + ground*0.15 + endorse*0.10 + sov*0.05
		data["cpi"] = math.Round(cpi*100) / 100
		data["components"] = map[string]float64{
			"voting_intention": vi, "favourability": fav, "digital_sentiment": sent,
			"ground_mobilisation": ground, "endorsement_index": endorse, "share_of_voice": sov,
		}

	case "cpi_brief":
		vi := computeVotingIntention(partyID, "")
		fav := computeFavourability(partyID, "")
		cpi := vi*0.30 + fav*0.25 + computeDigitalSentiment(partyID)*0.15 +
			computeGroundMobilisation(partyID, "")*0.15 + computeEndorsementIndex(partyID, "")*0.10 +
			computeShareOfVoice(partyID)*0.05
		data["cpi"] = math.Round(cpi*100) / 100
		data["interpretation"] = "on_track"
		if cpi < 60 {
			data["interpretation"] = "needs_improvement"
		}

	case "crisis_alert":
		// Check for sentiment drops
		var recentNeg int
		db.QueryRow(`SELECT COUNT(*) FROM gotv_sentiment_log
			WHERE party_id = $1 AND sentiment = 'negative' AND timestamp > NOW() - INTERVAL '2 hours'`,
			partyID).Scan(&recentNeg)
		data["negative_mentions_2h"] = recentNeg
		data["alert_level"] = "normal"
		if recentNeg > 50 {
			data["alert_level"] = "critical"
		} else if recentNeg > 20 {
			data["alert_level"] = "warning"
		}
	}

	b, _ := json.Marshal(data)
	return b
}

// ─── Module 8: Platform Analytics Ingestion ────────────────────────────────

func handleIngestPlatformAnalytics(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	var req struct {
		Platform string `json:"platform"` // twitter, facebook, instagram, tiktok, youtube
		Date     string `json:"date"`
		Data     struct {
			Followers           int     `json:"followers"`
			FollowerGrowthPct   float64 `json:"follower_growth_pct"`
			TotalReach          int     `json:"total_reach"`
			OrganicReach        int     `json:"organic_reach"`
			PaidReach           int     `json:"paid_reach"`
			EngagementRate      float64 `json:"engagement_rate"`
			VideoCompletionRate float64 `json:"video_completion_rate"`
			Impressions         int     `json:"impressions"`
			Clicks              int     `json:"clicks"`
			Shares              int     `json:"shares"`
			Comments            int     `json:"comments"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Platform == "" {
		jsonErr(w, "platform is required", http.StatusBadRequest)
		return
	}
	if req.Date == "" {
		req.Date = time.Now().Format("2006-01-02")
	}

	_, err := svc.DB.Exec(`INSERT INTO gotv_platform_analytics
		(party_id, date, platform, followers, follower_growth_pct, total_reach, organic_reach, paid_reach,
		engagement_rate, video_completion_rate, impressions, clicks, shares, comments)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT DO NOTHING`,
		partyID, req.Date, req.Platform, req.Data.Followers, req.Data.FollowerGrowthPct,
		req.Data.TotalReach, req.Data.OrganicReach, req.Data.PaidReach,
		req.Data.EngagementRate, req.Data.VideoCompletionRate,
		req.Data.Impressions, req.Data.Clicks, req.Data.Shares, req.Data.Comments)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ingested", "platform": req.Platform, "date": req.Date})
}

func handlePlatformAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 7
	}

	rows, err := svc.DB.Query(`SELECT platform,
		SUM(followers) AS total_followers, AVG(follower_growth_pct) AS avg_growth,
		SUM(total_reach) AS total_reach, SUM(organic_reach) AS organic,
		SUM(paid_reach) AS paid, AVG(engagement_rate) AS avg_engagement,
		AVG(video_completion_rate) AS avg_video_completion,
		SUM(impressions) AS total_impressions, SUM(clicks) AS total_clicks,
		SUM(shares) AS total_shares, SUM(comments) AS total_comments
		FROM gotv_platform_analytics WHERE party_id = $1 AND date > CURRENT_DATE - INTERVAL '1 day' * $2
		GROUP BY platform ORDER BY total_reach DESC`, partyID, days)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var platforms []map[string]interface{}
	for rows.Next() {
		var platform string
		var followers, reach, organic, paid, impressions, clicks, shares, comments int
		var growth, engagement, videoCompletion sql.NullFloat64
		rows.Scan(&platform, &followers, &growth, &reach, &organic, &paid, &engagement,
			&videoCompletion, &impressions, &clicks, &shares, &comments)
		platforms = append(platforms, map[string]interface{}{
			"platform": platform, "followers": followers, "follower_growth_pct": growth.Float64,
			"total_reach": reach, "organic_reach": organic, "paid_reach": paid,
			"engagement_rate": math.Round(engagement.Float64*10000) / 10000,
			"video_completion_rate": math.Round(videoCompletion.Float64*10000) / 10000,
			"impressions": impressions, "clicks": clicks, "shares": shares, "comments": comments,
		})
	}
	if platforms == nil {
		platforms = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"platforms": platforms, "days": days})
}

func handlePlatformAnalyticsTrend(w http.ResponseWriter, r *http.Request) {
	partyID := getPartyID(r)
	platform := r.URL.Query().Get("platform")
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 30
	}

	q := `SELECT date, followers, follower_growth_pct, total_reach, engagement_rate,
		video_completion_rate, impressions FROM gotv_platform_analytics
		WHERE party_id = $1 AND date > CURRENT_DATE - INTERVAL '1 day' * $2`
	args := []interface{}{partyID, days}
	if platform != "" {
		q += " AND platform = $3"
		args = append(args, platform)
	}
	q += " ORDER BY date"

	rows, err := svc.DB.Query(q, args...)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var trend []map[string]interface{}
	for rows.Next() {
		var date time.Time
		var followers, impressions int
		var growth, reach sql.NullFloat64
		var engagement, video sql.NullFloat64
		rows.Scan(&date, &followers, &growth, &reach, &engagement, &video, &impressions)
		trend = append(trend, map[string]interface{}{
			"date": date.Format("2006-01-02"), "followers": followers,
			"follower_growth_pct": growth.Float64, "total_reach": reach.Float64,
			"engagement_rate": engagement.Float64, "video_completion_rate": video.Float64,
			"impressions": impressions,
		})
	}
	if trend == nil {
		trend = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"trend": trend, "platform": platform, "days": days})
}
