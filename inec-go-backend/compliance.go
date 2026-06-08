package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// NDPR Compliance — Data Subject Rights, Consent Management, Breach Register

func initComplianceTables() {
	tables := `
	CREATE TABLE IF NOT EXISTS consent_records (
		id SERIAL PRIMARY KEY,
		consent_id TEXT UNIQUE NOT NULL,
		subject_id TEXT NOT NULL,
		purpose TEXT NOT NULL CHECK(purpose IN ('biometric_verification','voter_registration','official_tracking','analytics','communication')),
		legal_basis TEXT NOT NULL CHECK(legal_basis IN ('legal_obligation','legitimate_interest','consent','public_interest')),
		granted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP,
		withdrawn_at TIMESTAMP,
		withdrawal_available BOOLEAN DEFAULT TRUE,
		ip_address TEXT,
		user_agent TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_consent_subject ON consent_records(subject_id);
	CREATE INDEX IF NOT EXISTS idx_consent_purpose ON consent_records(purpose);

	CREATE TABLE IF NOT EXISTS data_subject_requests (
		id SERIAL PRIMARY KEY,
		request_id TEXT UNIQUE NOT NULL,
		subject_id TEXT NOT NULL,
		request_type TEXT NOT NULL CHECK(request_type IN ('access','rectification','erasure','portability','restriction','objection')),
		status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','completed','rejected')),
		requested_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMP,
		response_data JSONB,
		processed_by TEXT,
		notes TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_dsr_subject ON data_subject_requests(subject_id);

	CREATE TABLE IF NOT EXISTS data_breach_register (
		id SERIAL PRIMARY KEY,
		breach_id TEXT UNIQUE NOT NULL,
		detected_at TIMESTAMP NOT NULL,
		assessed_at TIMESTAMP,
		notified_nitda_at TIMESTAMP,
		notified_subjects_at TIMESTAMP,
		breach_type TEXT NOT NULL CHECK(breach_type IN ('confidentiality','integrity','availability')),
		affected_categories TEXT[] NOT NULL,
		estimated_subjects INTEGER,
		description TEXT NOT NULL,
		consequences TEXT,
		measures_taken TEXT,
		nitda_reference TEXT,
		status TEXT NOT NULL DEFAULT 'detected' CHECK(status IN ('detected','assessing','notified','remediated','closed')),
		dpo_sign_off BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS data_processing_register (
		id SERIAL PRIMARY KEY,
		processing_activity TEXT NOT NULL,
		purpose TEXT NOT NULL,
		legal_basis TEXT NOT NULL,
		data_categories TEXT[] NOT NULL,
		data_subjects TEXT NOT NULL,
		retention_period TEXT NOT NULL,
		recipients TEXT[],
		cross_border_transfer BOOLEAN DEFAULT FALSE,
		safeguards TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.Exec(tables)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create compliance tables")
	}
}

func registerComplianceRoutes(r *mux.Router) {
	// Consent management
	r.HandleFunc("/compliance/consent", adminOnly(handleRecordConsent)).Methods("POST")
	r.HandleFunc("/compliance/consent/{subject_id}", adminOnly(handleGetConsent)).Methods("GET")
	r.HandleFunc("/compliance/consent/{consent_id}/withdraw", adminOnly(handleWithdrawConsent)).Methods("POST")

	// Data subject rights
	r.HandleFunc("/compliance/data-subject/{nin}", adminOnly(handleDataSubjectAccess)).Methods("GET")
	r.HandleFunc("/compliance/data-subject/{nin}", adminOnly(handleDataSubjectRectification)).Methods("PUT")
	r.HandleFunc("/compliance/data-subject/{nin}", adminOnly(handleDataSubjectErasure)).Methods("DELETE")
	r.HandleFunc("/compliance/data-subject/{nin}/export", adminOnly(handleDataSubjectPortability)).Methods("GET")
	r.HandleFunc("/compliance/data-subject/{nin}/restrict", adminOnly(handleDataSubjectRestriction)).Methods("PUT")
	r.HandleFunc("/compliance/data-subject/{nin}/object", adminOnly(handleDataSubjectObjection)).Methods("POST")

	// Data breach register
	r.HandleFunc("/compliance/breaches", adminOnly(handleListBreaches)).Methods("GET")
	r.HandleFunc("/compliance/breaches", adminOnly(handleReportBreach)).Methods("POST")
	r.HandleFunc("/compliance/breaches/{breach_id}", adminOnly(handleGetBreach)).Methods("GET")
	r.HandleFunc("/compliance/breaches/{breach_id}/assess", adminOnly(handleAssessBreach)).Methods("PUT")
	r.HandleFunc("/compliance/breaches/{breach_id}/notify", adminOnly(handleNotifyBreach)).Methods("POST")

	// Processing register
	r.HandleFunc("/compliance/processing-register", adminOnly(handleProcessingRegister)).Methods("GET")

	// Compliance dashboard
	r.HandleFunc("/compliance/dashboard", adminOnly(handleComplianceDashboard)).Methods("GET")
}

// --- Consent Management ---

func handleRecordConsent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SubjectID   string  `json:"subject_id"`
		Purpose     string  `json:"purpose"`
		LegalBasis  string  `json:"legal_basis"`
		ExpiresAt   *string `json:"expires_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, 400)
		return
	}
	if req.SubjectID == "" || req.Purpose == "" || req.LegalBasis == "" {
		http.Error(w, `{"error":"subject_id, purpose, and legal_basis are required"}`, 400)
		return
	}

	consentID := fmt.Sprintf("consent-%d", time.Now().UnixNano())
	ip := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip = xff
	}
	ua := r.Header.Get("User-Agent")

	_, err := db.ExecContext(r.Context(),
		`INSERT INTO consent_records (consent_id, subject_id, purpose, legal_basis, ip_address, user_agent)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		consentID, req.SubjectID, req.Purpose, req.LegalBasis, ip, ua)
	if err != nil {
		log.Error().Err(err).Msg("Failed to record consent")
		http.Error(w, `{"error":"failed to record consent"}`, 500)
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"consent_id": consentID,
		"subject_id": req.SubjectID,
		"purpose":    req.Purpose,
		"status":     "granted",
	})
}

func handleGetConsent(w http.ResponseWriter, r *http.Request) {
	subjectID := mux.Vars(r)["subject_id"]
	rows, err := db.QueryContext(r.Context(),
		`SELECT consent_id, purpose, legal_basis, granted_at, withdrawn_at, withdrawal_available
		 FROM consent_records WHERE subject_id=$1 ORDER BY granted_at DESC`, subjectID)
	if err != nil {
		http.Error(w, `{"error":"query failed"}`, 500)
		return
	}
	defer rows.Close()

	var records []map[string]interface{}
	for rows.Next() {
		var consentID, purpose, legalBasis string
		var grantedAt time.Time
		var withdrawnAt *time.Time
		var withdrawalAvailable bool
		if err := rows.Scan(&consentID, &purpose, &legalBasis, &grantedAt, &withdrawnAt, &withdrawalAvailable); err != nil {
			continue
		}
		rec := map[string]interface{}{
			"consent_id":           consentID,
			"purpose":              purpose,
			"legal_basis":          legalBasis,
			"granted_at":           grantedAt,
			"withdrawal_available": withdrawalAvailable,
			"active":               withdrawnAt == nil,
		}
		if withdrawnAt != nil {
			rec["withdrawn_at"] = withdrawnAt
		}
		records = append(records, rec)
	}
	writeJSON(w, 200, map[string]interface{}{"subject_id": subjectID, "consents": records})
}

func handleWithdrawConsent(w http.ResponseWriter, r *http.Request) {
	consentID := mux.Vars(r)["consent_id"]
	result, err := db.ExecContext(r.Context(),
		`UPDATE consent_records SET withdrawn_at=CURRENT_TIMESTAMP
		 WHERE consent_id=$1 AND withdrawn_at IS NULL AND withdrawal_available=TRUE`, consentID)
	if err != nil {
		http.Error(w, `{"error":"withdrawal failed"}`, 500)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, `{"error":"consent not found or already withdrawn or not withdrawable"}`, 404)
		return
	}
	writeJSON(w, 200, map[string]interface{}{"consent_id": consentID, "status": "withdrawn"})
}

// --- Data Subject Rights ---

func handleDataSubjectAccess(w http.ResponseWriter, r *http.Request) {
	nin := mux.Vars(r)["nin"]

	requestID := fmt.Sprintf("dsr-%d", time.Now().UnixNano())
	db.ExecContext(r.Context(),
		`INSERT INTO data_subject_requests (request_id, subject_id, request_type, status)
		 VALUES ($1, $2, 'access', 'completed')`, requestID, nin)

	data := map[string]interface{}{
		"request_id":   requestID,
		"subject_id":   nin,
		"request_type": "access",
		"data_held":    map[string]interface{}{},
	}

	// Collect voter data
	var fullName, phone, stateCode, lgaCode string
	err := db.QueryRowContext(r.Context(),
		`SELECT full_name, COALESCE(phone,''), COALESCE(state_code,''), COALESCE(lga_code,'')
		 FROM voters WHERE nin=$1`, nin).Scan(&fullName, &phone, &stateCode, &lgaCode)
	if err == nil {
		data["data_held"].(map[string]interface{})["voter_record"] = map[string]interface{}{
			"full_name":  fullName,
			"phone":      phone,
			"state_code": stateCode,
			"lga_code":   lgaCode,
		}
	}

	// Check biometric data
	var bioCount int
	db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM biometric_verifications WHERE voter_id=$1`, nin).Scan(&bioCount)
	data["data_held"].(map[string]interface{})["biometric_records"] = bioCount

	// Check consent records
	var consentCount int
	db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM consent_records WHERE subject_id=$1`, nin).Scan(&consentCount)
	data["data_held"].(map[string]interface{})["consent_records"] = consentCount

	writeJSON(w, 200, data)
}

func handleDataSubjectRectification(w http.ResponseWriter, r *http.Request) {
	nin := mux.Vars(r)["nin"]
	var req struct {
		Field    string `json:"field"`
		NewValue string `json:"new_value"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, 400)
		return
	}

	requestID := fmt.Sprintf("dsr-%d", time.Now().UnixNano())
	db.ExecContext(r.Context(),
		`INSERT INTO data_subject_requests (request_id, subject_id, request_type, status, notes)
		 VALUES ($1, $2, 'rectification', 'pending', $3)`, requestID, nin, req.Reason)

	writeJSON(w, 200, map[string]interface{}{
		"request_id": requestID,
		"status":     "pending",
		"message":    "Rectification request recorded. Will be processed within 30 days per NDPR.",
	})
}

func handleDataSubjectErasure(w http.ResponseWriter, r *http.Request) {
	nin := mux.Vars(r)["nin"]
	requestID := fmt.Sprintf("dsr-%d", time.Now().UnixNano())
	db.ExecContext(r.Context(),
		`INSERT INTO data_subject_requests (request_id, subject_id, request_type, status, notes)
		 VALUES ($1, $2, 'erasure', 'pending', 'Subject to Electoral Act retention requirements')`,
		requestID, nin)

	writeJSON(w, 200, map[string]interface{}{
		"request_id": requestID,
		"status":     "pending",
		"message":    "Erasure request recorded. Note: Electoral Act requires retention of voter register data for the register's validity period. Non-essential data will be erased within 30 days.",
	})
}

func handleDataSubjectPortability(w http.ResponseWriter, r *http.Request) {
	nin := mux.Vars(r)["nin"]
	requestID := fmt.Sprintf("dsr-%d", time.Now().UnixNano())

	export := map[string]interface{}{
		"request_id":  requestID,
		"subject_id":  nin,
		"format":      "application/json",
		"exported_at": time.Now().UTC(),
	}

	var fullName, phone, stateCode string
	err := db.QueryRowContext(r.Context(),
		`SELECT full_name, COALESCE(phone,''), COALESCE(state_code,'')
		 FROM voters WHERE nin=$1`, nin).Scan(&fullName, &phone, &stateCode)
	if err == nil {
		export["personal_data"] = map[string]interface{}{
			"full_name": fullName, "phone": phone, "state_code": stateCode,
		}
	}

	db.ExecContext(r.Context(),
		`INSERT INTO data_subject_requests (request_id, subject_id, request_type, status)
		 VALUES ($1, $2, 'portability', 'completed')`, requestID, nin)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"data-export-%s.json\"", nin))
	json.NewEncoder(w).Encode(export)
}

func handleDataSubjectRestriction(w http.ResponseWriter, r *http.Request) {
	nin := mux.Vars(r)["nin"]
	requestID := fmt.Sprintf("dsr-%d", time.Now().UnixNano())
	db.ExecContext(r.Context(),
		`INSERT INTO data_subject_requests (request_id, subject_id, request_type, status)
		 VALUES ($1, $2, 'restriction', 'pending')`, requestID, nin)

	writeJSON(w, 200, map[string]interface{}{
		"request_id": requestID, "status": "pending",
		"message": "Processing restriction request. Data processing will be limited pending review.",
	})
}

func handleDataSubjectObjection(w http.ResponseWriter, r *http.Request) {
	nin := mux.Vars(r)["nin"]
	var req struct {
		ProcessingActivity string `json:"processing_activity"`
		Reason             string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	requestID := fmt.Sprintf("dsr-%d", time.Now().UnixNano())
	db.ExecContext(r.Context(),
		`INSERT INTO data_subject_requests (request_id, subject_id, request_type, status, notes)
		 VALUES ($1, $2, 'objection', 'pending', $3)`, requestID, nin, req.Reason)

	writeJSON(w, 200, map[string]interface{}{
		"request_id": requestID, "status": "pending",
		"message": "Objection recorded. Processing will be reviewed within 30 days.",
	})
}

// --- Data Breach Register ---

func handleListBreaches(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryContext(r.Context(),
		`SELECT breach_id, detected_at, breach_type, estimated_subjects, status, description
		 FROM data_breach_register ORDER BY detected_at DESC LIMIT 50`)
	if err != nil {
		http.Error(w, `{"error":"query failed"}`, 500)
		return
	}
	defer rows.Close()

	var breaches []map[string]interface{}
	for rows.Next() {
		var bid, btype, status, desc string
		var detected time.Time
		var subjects *int
		if err := rows.Scan(&bid, &detected, &btype, &subjects, &status, &desc); err != nil {
			continue
		}
		breaches = append(breaches, map[string]interface{}{
			"breach_id": bid, "detected_at": detected, "type": btype,
			"estimated_subjects": subjects, "status": status, "description": desc,
		})
	}
	if breaches == nil {
		breaches = []map[string]interface{}{}
	}
	writeJSON(w, 200, map[string]interface{}{"breaches": breaches, "total": len(breaches)})
}

func handleReportBreach(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BreachType   string   `json:"breach_type"`
		Categories   []string `json:"affected_categories"`
		Subjects     int      `json:"estimated_subjects"`
		Description  string   `json:"description"`
		Consequences string   `json:"consequences"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, 400)
		return
	}

	breachID := fmt.Sprintf("breach-%d", time.Now().UnixNano())
	_, err := db.ExecContext(r.Context(),
		`INSERT INTO data_breach_register (breach_id, detected_at, breach_type, affected_categories, estimated_subjects, description, consequences)
		 VALUES ($1, CURRENT_TIMESTAMP, $2, $3, $4, $5, $6)`,
		breachID, req.BreachType, pqArray(req.Categories), req.Subjects, req.Description, req.Consequences)
	if err != nil {
		log.Error().Err(err).Msg("Failed to record breach")
		http.Error(w, `{"error":"failed to record breach"}`, 500)
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"breach_id": breachID,
		"status":    "detected",
		"next_step": "Assess within 12 hours, notify NITDA within 72 hours",
	})
}

func handleGetBreach(w http.ResponseWriter, r *http.Request) {
	breachID := mux.Vars(r)["breach_id"]
	var btype, status, desc string
	var detected time.Time
	var subjects *int
	err := db.QueryRowContext(r.Context(),
		`SELECT breach_type, detected_at, estimated_subjects, status, description
		 FROM data_breach_register WHERE breach_id=$1`, breachID).Scan(&btype, &detected, &subjects, &status, &desc)
	if err != nil {
		http.Error(w, `{"error":"breach not found"}`, 404)
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"breach_id": breachID, "type": btype, "detected_at": detected,
		"estimated_subjects": subjects, "status": status, "description": desc,
	})
}

func handleAssessBreach(w http.ResponseWriter, r *http.Request) {
	breachID := mux.Vars(r)["breach_id"]
	db.ExecContext(r.Context(),
		`UPDATE data_breach_register SET assessed_at=CURRENT_TIMESTAMP, status='assessing' WHERE breach_id=$1`, breachID)
	writeJSON(w, 200, map[string]interface{}{"breach_id": breachID, "status": "assessing"})
}

func handleNotifyBreach(w http.ResponseWriter, r *http.Request) {
	breachID := mux.Vars(r)["breach_id"]
	var req struct {
		NITDAReference string `json:"nitda_reference"`
		NotifySubjects bool   `json:"notify_subjects"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	db.ExecContext(r.Context(),
		`UPDATE data_breach_register SET notified_nitda_at=CURRENT_TIMESTAMP, nitda_reference=$2, status='notified'
		 WHERE breach_id=$1`, breachID, req.NITDAReference)

	if req.NotifySubjects {
		db.ExecContext(r.Context(),
			`UPDATE data_breach_register SET notified_subjects_at=CURRENT_TIMESTAMP WHERE breach_id=$1`, breachID)
	}

	writeJSON(w, 200, map[string]interface{}{"breach_id": breachID, "status": "notified", "nitda_reference": req.NITDAReference})
}

// --- Processing Register ---

func handleProcessingRegister(w http.ResponseWriter, r *http.Request) {
	register := []map[string]interface{}{
		{"activity": "Voter Registration", "purpose": "Electoral register maintenance", "legal_basis": "Electoral Act 2022 Section 10", "data_categories": []string{"personal", "biometric"}, "retention": "4 years (register validity)", "cross_border": false},
		{"activity": "Biometric Verification", "purpose": "Voter identity verification at polling units", "legal_basis": "Electoral Act 2022 Section 47", "data_categories": []string{"biometric"}, "retention": "Duration of election + 72 hours", "cross_border": false},
		{"activity": "Result Collation", "purpose": "Aggregation of election results", "legal_basis": "Electoral Act 2022 Section 60-68", "data_categories": []string{"election_results"}, "retention": "Permanent (public record)", "cross_border": false},
		{"activity": "Official Location Tracking", "purpose": "Election logistics and security", "legal_basis": "Legitimate interest + consent", "data_categories": []string{"location"}, "retention": "72 hours post-election", "cross_border": false},
		{"activity": "Observer Monitoring", "purpose": "Election transparency", "legal_basis": "Electoral Act 2022 Section 3", "data_categories": []string{"personal", "observation_reports"}, "retention": "1 year", "cross_border": false},
		{"activity": "BVAS Device Telemetry", "purpose": "Device health monitoring", "legal_basis": "Legitimate interest", "data_categories": []string{"device_telemetry"}, "retention": "1 year", "cross_border": false},
		{"activity": "Audit Logging", "purpose": "Security and accountability", "legal_basis": "NDPR Art. 2.7", "data_categories": []string{"access_logs"}, "retention": "7 years", "cross_border": false},
	}
	writeJSON(w, 200, map[string]interface{}{"processing_activities": register, "total": len(register)})
}

// --- Compliance Dashboard ---

func handleComplianceDashboard(w http.ResponseWriter, r *http.Request) {
	dashboard := map[string]interface{}{}

	// Consent stats
	var totalConsents, activeConsents, withdrawnConsents int
	db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM consent_records`).Scan(&totalConsents)
	db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM consent_records WHERE withdrawn_at IS NULL`).Scan(&activeConsents)
	withdrawnConsents = totalConsents - activeConsents
	dashboard["consent"] = map[string]interface{}{
		"total": totalConsents, "active": activeConsents, "withdrawn": withdrawnConsents,
	}

	// DSR stats
	var totalDSR, pendingDSR, completedDSR int
	db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM data_subject_requests`).Scan(&totalDSR)
	db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM data_subject_requests WHERE status='pending'`).Scan(&pendingDSR)
	db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM data_subject_requests WHERE status='completed'`).Scan(&completedDSR)
	dashboard["data_subject_requests"] = map[string]interface{}{
		"total": totalDSR, "pending": pendingDSR, "completed": completedDSR,
	}

	// Breach stats
	var totalBreaches, openBreaches int
	db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM data_breach_register`).Scan(&totalBreaches)
	db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM data_breach_register WHERE status NOT IN ('remediated','closed')`).Scan(&openBreaches)
	dashboard["breaches"] = map[string]interface{}{
		"total": totalBreaches, "open": openBreaches,
	}

	dashboard["ndpr_status"] = map[string]interface{}{
		"dpia_completed":    true,
		"dpo_appointed":     false,
		"nitda_registered":  false,
		"annual_audit_done": false,
		"consent_mechanism": true,
		"dsr_endpoints":     true,
		"breach_procedure":  true,
		"retention_policy":  true,
		"encryption":        true,
	}

	writeJSON(w, 200, dashboard)
}

// pqArray converts a string slice to a PostgreSQL array literal.
func pqArray(ss []string) string {
	if len(ss) == 0 {
		return "{}"
	}
	result := "{"
	for i, s := range ss {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf(`"%s"`, s)
	}
	return result + "}"
}
