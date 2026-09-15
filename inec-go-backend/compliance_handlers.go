package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// dataSubjectNIN extracts the subject identifier from the path ({nin}) or a
// query parameter, so these handlers work regardless of how they are routed.
func dataSubjectNIN(r *http.Request) string {
	if nin := mux.Vars(r)["nin"]; nin != "" {
		return nin
	}
	return r.URL.Query().Get("nin")
}

// HandleDataSubjectAccess handles NDPR right to access.
// INTEGRITY: returns the actual records held about the subject — never a bare
// "success" acknowledgement for a request that did nothing.
func HandleDataSubjectAccess(w http.ResponseWriter, r *http.Request) {
	nin := dataSubjectNIN(r)
	if nin == "" {
		writeError(w, 400, "subject identifier (nin) is required")
		return
	}
	if db == nil {
		writeError(w, 503, "database unavailable")
		return
	}

	data := map[string]interface{}{
		"subject_id":   nin,
		"request_type": "access",
		"generated_at": time.Now().UTC(),
		"data_held":    map[string]interface{}{},
		"partial":      false,
	}
	held := data["data_held"].(map[string]interface{})

	// Voter registry record
	var firstName, lastName, phone, email, stateCode, lgaCode, wardCode, puCode, status string
	err := db.QueryRowContext(r.Context(),
		`SELECT first_name, last_name, COALESCE(phone,''), COALESCE(email,''),
		        state_code, lga_code, ward_code, polling_unit_code, status
		 FROM voters WHERE nin=$1`, nin).
		Scan(&firstName, &lastName, &phone, &email, &stateCode, &lgaCode, &wardCode, &puCode, &status)
	switch {
	case err == sql.ErrNoRows:
		// Not an error — the honest answer is that we hold no voter record.
		held["voter_record"] = nil
	case err != nil:
		log.Error().Err(err).Msg("data subject access: voter query failed")
		writeError(w, 500, "access request failed")
		return
	default:
		held["voter_record"] = map[string]interface{}{
			"first_name":        firstName,
			"last_name":         lastName,
			"phone":             phone,
			"email":             email,
			"state_code":        stateCode,
			"lga_code":          lgaCode,
			"ward_code":         wardCode,
			"polling_unit_code": puCode,
			"status":            status,
		}
	}

	// Biometric verification records (count only — templates are never exported)
	var bioCount int
	if err := db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM biometric_verifications bv
		 JOIN voters v ON v.vin = bv.voter_vin WHERE v.nin=$1`, nin).Scan(&bioCount); err != nil {
		log.Error().Err(err).Msg("data subject access: biometric count failed")
		data["partial"] = true
	} else {
		held["biometric_verification_records"] = bioCount
	}

	// Consent records
	var consentCount int
	if err := db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM consent_records WHERE subject_id=$1`, nin).Scan(&consentCount); err != nil {
		log.Error().Err(err).Msg("data subject access: consent count failed")
		data["partial"] = true
	} else {
		held["consent_records"] = consentCount
	}

	// Audit the access request itself (best-effort; does not fail the request).
	requestID := fmt.Sprintf("dsr-%d", time.Now().UnixNano())
	if _, err := db.ExecContext(r.Context(),
		`INSERT INTO data_subject_requests (request_id, subject_id, request_type, status)
		 VALUES ($1, $2, 'access', 'completed')`, requestID, nin); err != nil {
		log.Warn().Err(err).Msg("data subject access: audit insert failed")
	} else {
		data["request_id"] = requestID
	}

	writeJSON(w, 200, data)
}

// HandleDataSubjectErasure handles NDPR right to erasure (right to be forgotten).
// INTEGRITY: performs a real anonymize/delete of the subject's rows inside a
// transaction and records an audit entry. Full platform-wide coverage is not
// claimed — the response is explicitly partial and lists the tables processed.
func HandleDataSubjectErasure(w http.ResponseWriter, r *http.Request) {
	nin := dataSubjectNIN(r)
	if nin == "" {
		writeError(w, 400, "subject identifier (nin) is required")
		return
	}
	if db == nil {
		writeError(w, 503, "database unavailable")
		return
	}

	ctx := r.Context()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Error().Err(err).Msg("data subject erasure: begin tx failed")
		writeError(w, 500, "erasure request failed")
		return
	}
	defer tx.Rollback() // no-op after Commit

	processed := []string{}

	// Anonymize the voter registry row (the row itself is retained for the
	// statutory electoral register; personal contact/biometric data is wiped).
	res, err := tx.ExecContext(ctx,
		`UPDATE voters SET phone=NULL, email=NULL, address=NULL,
		        biometric_hash=NULL, photo_hash=NULL, updated_at=CURRENT_TIMESTAMP
		 WHERE nin=$1`, nin)
	if err != nil {
		log.Error().Err(err).Msg("data subject erasure: voter anonymize failed")
		writeError(w, 500, "erasure request failed")
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		writeError(w, 404, "no records found for subject")
		return
	}
	processed = append(processed, "voters")

	// Delete biometric verification history for the subject.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM biometric_verifications WHERE voter_vin IN
		 (SELECT vin FROM voters WHERE nin=$1)`, nin); err != nil {
		log.Error().Err(err).Msg("data subject erasure: biometric delete failed")
		writeError(w, 500, "erasure request failed")
		return
	}
	processed = append(processed, "biometric_verifications")

	// R5-053: the audit entry is written AFTER commit through the hash-chained
	// logAuditCtx — a direct INSERT here bypassed the chain (NULL block_hash).
	// Chaining cannot happen inside this transaction because the chain requires
	// a serialized prev-hash read (see logAuditCtx); a committed erasure without
	// a chained audit row is logged loudly below on failure.
	details := fmt.Sprintf("NDPR erasure: anonymized voter row and deleted biometric verification records for subject; tables=%v", processed)

	// Record the data subject request.
	requestID := fmt.Sprintf("dsr-%d", time.Now().UnixNano())
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO data_subject_requests (request_id, subject_id, request_type, status)
		 VALUES ($1, $2, 'erasure', 'completed')`, requestID, nin); err != nil {
		log.Error().Err(err).Msg("data subject erasure: dsr insert failed")
		writeError(w, 500, "erasure request failed")
		return
	}
	processed = append(processed, "data_subject_requests")

	if err := tx.Commit(); err != nil {
		log.Error().Err(err).Msg("data subject erasure: commit failed")
		writeError(w, 500, "erasure request failed")
		return
	}

	// Chained audit entry (R5-053). logAuditCtx never fails silently — it logs
	// at error level on write failure.
	logAuditCtx(ctx, "data_subject_erasure", "voter", nin, 0, map[string]interface{}{"details": details, "request_id": requestID})
	processed = append(processed, "audit_log")

	log.Info().Str("request_id", requestID).Msg("NDPR data subject erasure completed")
	writeJSON(w, 200, map[string]interface{}{
		"status":           "completed",
		"request_id":       requestID,
		"subject_id":       nin,
		"partial":          true,
		"tables_processed": processed,
		"note": "Erasure covered the voter registry (anonymized) and biometric " +
			"verification history (deleted), with an audit entry. Electoral Act " +
			"retention requirements apply to the core register entry; any data " +
			"outside the listed tables requires a separate DPO review.",
	})
}
