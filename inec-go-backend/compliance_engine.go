package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// ══════════════════════════════════════════════════════════════════════════════
// NIN/NIMC API Integration — Real National Identity Management Commission
// ══════════════════════════════════════════════════════════════════════════════

type NIMCClient struct {
	baseURL   string
	apiKey    string
	secretKey string
	client    *ResilientHTTPClient
}

type NINVerifyRequest struct {
	NIN         string `json:"nin" validate:"required,len=11"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	DateOfBirth string `json:"date_of_birth"`
	Phone       string `json:"phone"`
}

type NINVerifyResponse struct {
	Verified    bool    `json:"verified"`
	NIN         string  `json:"nin"`
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	MiddleName  string  `json:"middle_name"`
	Gender      string  `json:"gender"`
	Phone       string  `json:"phone"`
	DateOfBirth string  `json:"date_of_birth"`
	Photo       string  `json:"photo"`
	Address     string  `json:"address"`
	MatchScore  float64 `json:"match_score"`
	Source      string  `json:"source"`
}

func NewNIMCClient() *NIMCClient {
	return &NIMCClient{
		baseURL:   envOrDefault("NIMC_API_URL", "https://api.nimc.gov.ng/v1"),
		apiKey:    os.Getenv("NIMC_API_KEY"),
		secretKey: os.Getenv("NIMC_SECRET_KEY"),
		client:    NewResilientHTTPClient("nimc"),
	}
}

func (n *NIMCClient) VerifyNIN(ctx context.Context, req NINVerifyRequest) (*NINVerifyResponse, error) {
	if n.apiKey == "" {
		return n.verifyNINFromDB(ctx, req)
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", n.baseURL+"/verify/nin", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+n.apiKey)
	httpReq.Header.Set("X-API-Secret", n.secretKey)

	resp, err := n.client.Do(httpReq)
	if err != nil {
		log.Warn().Err(err).Msg("NIMC API unreachable, falling back to DB lookup")
		return n.verifyNINFromDB(ctx, req)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Warn().Int("status", resp.StatusCode).Str("body", string(respBody)).Msg("NIMC API error")
		return n.verifyNINFromDB(ctx, req)
	}

	var result NINVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	result.Source = "nimc_api"

	// Cache to DB
	n.cacheNINResult(req.NIN, &result)
	return &result, nil
}

func (n *NIMCClient) verifyNINFromDB(ctx context.Context, req NINVerifyRequest) (*NINVerifyResponse, error) {
	_ = ctx
	var firstName, lastName, dob, phone, gender string
	err := db.QueryRow(`SELECT first_name, last_name, date_of_birth, phone, gender 
		FROM voters WHERE nin = $1`, req.NIN).Scan(&firstName, &lastName, &dob, &phone, &gender)
	if err != nil {
		return &NINVerifyResponse{Verified: false, NIN: req.NIN, MatchScore: 0, Source: "db_lookup"}, nil
	}

	score := 0.0
	if strings.EqualFold(firstName, req.FirstName) {
		score += 0.3
	}
	if strings.EqualFold(lastName, req.LastName) {
		score += 0.3
	}
	if dob == req.DateOfBirth {
		score += 0.25
	}
	if phone == req.Phone {
		score += 0.15
	}

	return &NINVerifyResponse{
		Verified:    score >= 0.6,
		NIN:         req.NIN,
		FirstName:   firstName,
		LastName:    lastName,
		Gender:      gender,
		Phone:       phone,
		DateOfBirth: dob,
		MatchScore:  score,
		Source:      "db_lookup",
	}, nil
}

func (n *NIMCClient) cacheNINResult(nin string, result *NINVerifyResponse) {
	data, _ := json.Marshal(result)
	dbExecLog("nin_cache", `INSERT INTO nin_verification_cache (nin_hash, result_data, verified, match_score, verified_at)
		VALUES ($1, $2, $3, $4, NOW()) ON CONFLICT (nin_hash) DO UPDATE SET result_data=$2, verified=$3, match_score=$4, verified_at=NOW()`,
		hashNIN(nin), string(data), result.Verified, result.MatchScore)
}

func hashNIN(nin string) string {
	h := sha256.Sum256([]byte(nin))
	return hex.EncodeToString(h[:])
}

// ══════════════════════════════════════════════════════════════════════════════
// BVN (Bank Verification Number) Cross-Check via NIBSS
// ══════════════════════════════════════════════════════════════════════════════

type NIBSSClient struct {
	baseURL string
	apiKey  string
	client  *ResilientHTTPClient
}

type BVNVerifyResponse struct {
	Verified    bool    `json:"verified"`
	BVN         string  `json:"bvn"`
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	Phone       string  `json:"phone"`
	DateOfBirth string  `json:"date_of_birth"`
	MatchScore  float64 `json:"match_score"`
	Source      string  `json:"source"`
}

func NewNIBSSClient() *NIBSSClient {
	return &NIBSSClient{
		baseURL: envOrDefault("NIBSS_API_URL", "https://api.nibss-plc.com.ng/v1"),
		apiKey:  os.Getenv("NIBSS_API_KEY"),
		client:  NewResilientHTTPClient("nibss"),
	}
}

func (n *NIBSSClient) VerifyBVN(ctx context.Context, bvn string) (*BVNVerifyResponse, error) {
	if n.apiKey == "" {
		return &BVNVerifyResponse{Verified: false, BVN: bvn, Source: "unavailable"}, nil
	}
	body, _ := json.Marshal(map[string]string{"bvn": bvn})
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", n.baseURL+"/bvn/verify", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+n.apiKey)
	resp, err := n.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("nibss api: %w", err)
	}
	defer resp.Body.Close()
	var result BVNVerifyResponse
	json.NewDecoder(resp.Body).Decode(&result)
	result.Source = "nibss_api"
	return &result, nil
}

// ══════════════════════════════════════════════════════════════════════════════
// CAC (Corporate Affairs Commission) — KYB Business Verification
// ══════════════════════════════════════════════════════════════════════════════

type CACClient struct {
	baseURL string
	apiKey  string
	client  *ResilientHTTPClient
}

type CACVerifyRequest struct {
	RCNumber   string `json:"rc_number" validate:"required"`
	EntityName string `json:"entity_name"`
	EntityType string `json:"entity_type"`
}

type CACVerifyResponse struct {
	Verified         bool     `json:"verified"`
	RCNumber         string   `json:"rc_number"`
	CompanyName      string   `json:"company_name"`
	CompanyType      string   `json:"company_type"`
	RegistrationDate string   `json:"registration_date"`
	Status           string   `json:"status"`
	Address          string   `json:"address"`
	Directors        []string `json:"directors"`
	MatchScore       float64  `json:"match_score"`
	Source           string   `json:"source"`
}

func NewCACClient() *CACClient {
	return &CACClient{
		baseURL: envOrDefault("CAC_API_URL", "https://api.cac.gov.ng/v1"),
		apiKey:  os.Getenv("CAC_API_KEY"),
		client:  NewResilientHTTPClient("cac"),
	}
}

func (c *CACClient) VerifyBusiness(ctx context.Context, req CACVerifyRequest) (*CACVerifyResponse, error) {
	if c.apiKey == "" {
		return c.verifyFromDB(ctx, req)
	}
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/company/verify", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.client.Do(httpReq)
	if err != nil {
		log.Warn().Err(err).Msg("CAC API unreachable, falling back to DB")
		return c.verifyFromDB(ctx, req)
	}
	defer resp.Body.Close()
	var result CACVerifyResponse
	json.NewDecoder(resp.Body).Decode(&result)
	result.Source = "cac_api"
	return &result, nil
}

func (c *CACClient) verifyFromDB(_ context.Context, req CACVerifyRequest) (*CACVerifyResponse, error) {
	var name, status string
	var compScore float64
	err := db.QueryRow(`SELECT entity_name, status, compliance_score FROM kyb_verifications 
		WHERE registration_number=$1 ORDER BY verified_at DESC LIMIT 1`,
		req.RCNumber).Scan(&name, &status, &compScore)
	if err != nil {
		return &CACVerifyResponse{Verified: false, RCNumber: req.RCNumber, Source: "db_lookup"}, nil
	}
	score := 0.0
	if strings.EqualFold(name, req.EntityName) {
		score = 0.8
	} else if strings.Contains(strings.ToLower(name), strings.ToLower(req.EntityName)) {
		score = 0.6
	}
	return &CACVerifyResponse{
		Verified: score >= 0.6, RCNumber: req.RCNumber, CompanyName: name,
		Status: status, MatchScore: score, Source: "db_lookup",
	}, nil
}

// ══════════════════════════════════════════════════════════════════════════════
// Sanctions / PEP / EFCC Screening Engine
// ══════════════════════════════════════════════════════════════════════════════

type SanctionsEngine struct {
	db     *sql.DB
	client *ResilientHTTPClient
}

type ScreeningRequest struct {
	FullName    string `json:"full_name" validate:"required"`
	DateOfBirth string `json:"date_of_birth"`
	Nationality string `json:"nationality"`
	IDNumber    string `json:"id_number"`
}

type ScreeningResult struct {
	ScreenID    string         `json:"screen_id"`
	Status      string         `json:"status"`
	RiskLevel   string         `json:"risk_level"`
	Matches     []ScreenMatch  `json:"matches"`
	TotalChecks int            `json:"total_checks"`
	CheckedAt   time.Time      `json:"checked_at"`
	Lists       []string       `json:"lists_checked"`
	Score       float64        `json:"score"`
}

type ScreenMatch struct {
	ListName   string  `json:"list_name"`
	MatchedName string `json:"matched_name"`
	MatchScore float64 `json:"match_score"`
	Category   string  `json:"category"`
	Details    string  `json:"details"`
	ListedDate string  `json:"listed_date"`
}

func NewSanctionsEngine(database *sql.DB) *SanctionsEngine {
	database.Exec(`CREATE TABLE IF NOT EXISTS sanctions_list (
		id SERIAL PRIMARY KEY,
		list_name TEXT NOT NULL,
		full_name TEXT NOT NULL,
		aliases TEXT DEFAULT '',
		category TEXT NOT NULL,
		nationality TEXT DEFAULT '',
		date_of_birth TEXT DEFAULT '',
		listed_date TEXT DEFAULT '',
		details TEXT DEFAULT '',
		name_lower TEXT NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	database.Exec(`CREATE INDEX IF NOT EXISTS idx_sanctions_name_lower ON sanctions_list(name_lower)`)
	database.Exec(`CREATE TABLE IF NOT EXISTS screening_results (
		id SERIAL PRIMARY KEY,
		screen_id TEXT UNIQUE NOT NULL,
		full_name TEXT NOT NULL,
		risk_level TEXT NOT NULL,
		match_count INTEGER DEFAULT 0,
		score REAL DEFAULT 0,
		result_data JSONB,
		checked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	database.Exec(`CREATE TABLE IF NOT EXISTS nin_verification_cache (
		id SERIAL PRIMARY KEY,
		nin_hash TEXT UNIQUE NOT NULL,
		result_data JSONB,
		verified BOOLEAN DEFAULT FALSE,
		match_score REAL DEFAULT 0,
		verified_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)

	se := &SanctionsEngine{db: database, client: NewResilientHTTPClient("sanctions")}
	se.seedSanctionsList()
	return se
}

func (s *SanctionsEngine) seedSanctionsList() {
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM sanctions_list`).Scan(&count)
	if count > 0 {
		return
	}
	lists := []struct {
		listName, fullName, category, nationality, details string
	}{
		{"UN_SANCTIONS", "Boko Haram", "terrorist_organization", "NG", "UN Security Council designated terrorist group"},
		{"UN_SANCTIONS", "ISWAP", "terrorist_organization", "NG", "Islamic State West Africa Province"},
		{"EFCC_WATCHLIST", "EFCC WATCHLIST ENTRY 1", "fraud", "NG", "Economic and Financial Crimes Commission watchlist"},
		{"OFAC_SDN", "OFAC SDN ENTRY 1", "sanctions", "", "US Treasury OFAC Specially Designated Nationals"},
		{"EU_SANCTIONS", "EU SANCTIONS ENTRY 1", "sanctions", "", "European Union consolidated sanctions list"},
		{"PEP_REGISTER", "PEP REGISTER ENTRY 1", "pep", "NG", "Politically Exposed Persons registry"},
		{"INTERPOL_RED", "INTERPOL RED NOTICE 1", "wanted", "", "Interpol Red Notice"},
	}
	for _, l := range lists {
		s.db.Exec(`INSERT INTO sanctions_list (list_name, full_name, aliases, category, nationality, details, name_lower)
			VALUES ($1, $2, '', $3, $4, $5, $6)`,
			l.listName, l.fullName, l.category, l.nationality, l.details, strings.ToLower(l.fullName))
	}
	log.Info().Int("seeded", len(lists)).Msg("Sanctions list seeded")
}

func (s *SanctionsEngine) Screen(ctx context.Context, req ScreeningRequest) (*ScreeningResult, error) {
	_ = ctx
	screenID := fmt.Sprintf("SCR-%x", sha256.Sum256([]byte(req.FullName+time.Now().String())))[:16]
	nameLower := strings.ToLower(req.FullName)
	nameParts := strings.Fields(nameLower)

	listsChecked := []string{"UN_SANCTIONS", "EFCC_WATCHLIST", "OFAC_SDN", "EU_SANCTIONS", "PEP_REGISTER", "INTERPOL_RED"}
	var matches []ScreenMatch

	rows, err := s.db.Query(`SELECT list_name, full_name, category, details, listed_date, name_lower 
		FROM sanctions_list ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query sanctions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var listName, fullName, category, details, listedDate, dbNameLower string
		rows.Scan(&listName, &fullName, &category, &details, &listedDate, &dbNameLower)

		score := fuzzyNameMatch(nameParts, strings.Fields(dbNameLower))
		if score >= 0.65 {
			matches = append(matches, ScreenMatch{
				ListName:    listName,
				MatchedName: fullName,
				MatchScore:  score,
				Category:    category,
				Details:     details,
				ListedDate:  listedDate,
			})
		}
	}

	riskLevel := "clear"
	overallScore := 0.0
	if len(matches) > 0 {
		maxScore := 0.0
		for _, m := range matches {
			if m.MatchScore > maxScore {
				maxScore = m.MatchScore
			}
		}
		overallScore = maxScore
		if maxScore >= 0.9 {
			riskLevel = "high"
		} else if maxScore >= 0.75 {
			riskLevel = "medium"
		} else {
			riskLevel = "low"
		}
	}

	result := &ScreeningResult{
		ScreenID:    screenID,
		Status:      "completed",
		RiskLevel:   riskLevel,
		Matches:     matches,
		TotalChecks: len(listsChecked),
		CheckedAt:   time.Now(),
		Lists:       listsChecked,
		Score:       overallScore,
	}

	resultData, _ := json.Marshal(result)
	s.db.Exec(`INSERT INTO screening_results (screen_id, full_name, risk_level, match_count, score, result_data)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		screenID, req.FullName, riskLevel, len(matches), overallScore, string(resultData))

	return result, nil
}

func fuzzyNameMatch(queryParts, candidateParts []string) float64 {
	if len(queryParts) == 0 || len(candidateParts) == 0 {
		return 0
	}
	matchCount := 0
	for _, qp := range queryParts {
		for _, cp := range candidateParts {
			if qp == cp || levenshteinDistance(qp, cp) <= 2 {
				matchCount++
				break
			}
		}
	}
	return float64(matchCount) / math.Max(float64(len(queryParts)), float64(len(candidateParts)))
}

func levenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	matrix := make([][]int, len(a)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(b)+1)
		matrix[i][0] = i
	}
	for j := 1; j <= len(b); j++ {
		matrix[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,
				min(matrix[i][j-1]+1, matrix[i-1][j-1]+cost),
			)
		}
	}
	return matrix[len(a)][len(b)]
}

// ══════════════════════════════════════════════════════════════════════════════
// HTTP Handlers for Compliance
// ══════════════════════════════════════════════════════════════════════════════

var (
	nimcClient      *NIMCClient
	nibssClient     *NIBSSClient
	cacClient       *CACClient
	sanctionsEngine *SanctionsEngine
)

func initComplianceEngine() {
	nimcClient = NewNIMCClient()
	nibssClient = NewNIBSSClient()
	cacClient = NewCACClient()
	sanctionsEngine = NewSanctionsEngine(db)
	log.Info().
		Bool("nimc_configured", nimcClient.apiKey != "").
		Bool("nibss_configured", nibssClient.apiKey != "").
		Bool("cac_configured", cacClient.apiKey != "").
		Msg("Compliance engine initialized")
}

func handleNINVerify(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin", "presiding_officer")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}
	var req NINVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if len(req.NIN) != 11 {
		writeError(w, 400, "NIN must be exactly 11 digits")
		return
	}
	result, err := nimcClient.VerifyNIN(r.Context(), req)
	if err != nil {
		writeError(w, 500, "NIN verification failed: "+err.Error())
		return
	}

	logAudit("NIN_VERIFIED", "compliance", req.NIN, 0, map[string]interface{}{"verified": result.Verified, "score": result.MatchScore, "source": result.Source})
	writeJSON(w, 200, result)
}

func handleBVNVerify(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin", "presiding_officer")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}
	var req struct {
		BVN string `json:"bvn" validate:"required,len=11"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	result, err := nibssClient.VerifyBVN(r.Context(), req.BVN)
	if err != nil {
		writeError(w, 500, "BVN verification failed: "+err.Error())
		return
	}
	writeJSON(w, 200, result)
}

func handleCACVerify(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}
	var req CACVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	result, err := cacClient.VerifyBusiness(r.Context(), req)
	if err != nil {
		writeError(w, 500, "CAC verification failed: "+err.Error())
		return
	}
	logAudit("CAC_VERIFIED", "compliance", req.RCNumber, 0, map[string]interface{}{"verified": result.Verified, "source": result.Source})
	writeJSON(w, 200, result)
}

func handleSanctionsScreen(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin", "presiding_officer", "collation_officer")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}
	var req ScreeningRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	result, err := sanctionsEngine.Screen(r.Context(), req)
	if err != nil {
		writeError(w, 500, "screening failed: "+err.Error())
		return
	}

	mwHub.Kafka.Produce(r.Context(), KafkaMessage{
		Topic: "inec.compliance.screening",
		Key:   result.ScreenID,
		Value: M{"event": "sanctions_screened", "risk_level": result.RiskLevel, "matches": len(result.Matches)},
	})

	writeJSON(w, 200, result)
}

func handleComplianceDashboardEngine(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}

	var totalScreenings, highRisk, mediumRisk, clear int
	db.QueryRow(`SELECT COUNT(*) FROM screening_results`).Scan(&totalScreenings)
	db.QueryRow(`SELECT COUNT(*) FROM screening_results WHERE risk_level='high'`).Scan(&highRisk)
	db.QueryRow(`SELECT COUNT(*) FROM screening_results WHERE risk_level='medium'`).Scan(&mediumRisk)
	db.QueryRow(`SELECT COUNT(*) FROM screening_results WHERE risk_level='clear'`).Scan(&clear)

	var ninVerifications, ninVerified int
	db.QueryRow(`SELECT COUNT(*) FROM nin_verification_cache`).Scan(&ninVerifications)
	db.QueryRow(`SELECT COUNT(*) FROM nin_verification_cache WHERE verified=true`).Scan(&ninVerified)

	var sanctionsEntries int
	db.QueryRow(`SELECT COUNT(*) FROM sanctions_list`).Scan(&sanctionsEntries)

	writeJSON(w, 200, M{
		"screenings":       M{"total": totalScreenings, "high_risk": highRisk, "medium_risk": mediumRisk, "clear": clear},
		"nin_verifications": M{"total": ninVerifications, "verified": ninVerified},
		"sanctions_lists":  M{"total_entries": sanctionsEntries},
		"services": M{
			"nimc":  M{"configured": nimcClient.apiKey != "", "url": nimcClient.baseURL},
			"nibss": M{"configured": nibssClient.apiKey != "", "url": nibssClient.baseURL},
			"cac":   M{"configured": cacClient.apiKey != "", "url": cacClient.baseURL},
		},
	})
}
