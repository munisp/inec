package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// ── Auth ──

// verifyTOTPCode validates a TOTP code against the stored secret for a user.
func verifyTOTPCode(secret, code string) bool {
	if mfaService == nil {
		return false
	}
	return mfaService.VerifyTOTPCode(secret, code)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	row := dbQueryRowCtx(r.Context(), "SELECT id, username, password_hash, full_name, role, staff_id, state_code FROM users WHERE username=? AND is_active=1", req.Username)
	var id int
	var username, pwHash, fullName, role string
	var staffID, stateCode sql.NullString
	if err := row.Scan(&id, &username, &pwHash, &fullName, &role, &staffID, &stateCode); err != nil {
		writeError(w, 401, "Invalid credentials")
		return
	}
	if !verifyPassword(req.Password, pwHash) {
		writeError(w, 401, "Invalid credentials")
		return
	}

	// Check if user has MFA enabled — require TOTP code if so
	var mfaEnabled bool
	dbQueryRowCtx(r.Context(), "SELECT EXISTS(SELECT 1 FROM mfa_totp WHERE user_id=? AND is_active=1)", id).Scan(&mfaEnabled)
	if mfaEnabled {
		if req.TOTPCode == "" {
			writeJSON(w, 200, M{
				"mfa_required": true,
				"mfa_type":     "totp",
				"message":      "MFA verification required. Re-submit with totp_code field.",
				"user_id":      id,
			})
			return
		}
		var secret string
		if err := dbQueryRowCtx(r.Context(), "SELECT secret FROM mfa_totp WHERE user_id=? AND is_active=1", id).Scan(&secret); err != nil {
			writeError(w, 500, "MFA configuration error")
			return
		}
		if !verifyTOTPCode(secret, req.TOTPCode) {
			writeError(w, 401, "Invalid TOTP code")
			return
		}
	}

	claims := map[string]interface{}{
		"sub": fmt.Sprintf("%d", id), "username": username, "role": role, "full_name": fullName,
	}
	token, _ := createAccessToken(claims)
	refresh, _ := createRefreshToken(claims)

	// Set httpOnly cookies for XSS-resistant auth
	secure := os.Getenv("APP_ENV") == "production" || os.Getenv("APP_ENV") == "staging"
	sameSite := http.SameSiteLaxMode
	if secure {
		sameSite = http.SameSiteStrictMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "inec_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		MaxAge:   3600,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "inec_refresh",
		Value:    refresh,
		Path:     "/auth/refresh",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		MaxAge:   7 * 24 * 3600,
	})

	writeJSON(w, 200, M{
		"access_token": token, "refresh_token": refresh, "token_type": "bearer", "expires_in": 3600,
		"user": M{"id": id, "username": username, "full_name": fullName, "role": role, "staff_id": nullStr(staffID), "state_code": nullStr(stateCode)},
	})
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username  string  `json:"username"`
		Password  string  `json:"password"`
		FullName  string  `json:"full_name"`
		Role      string  `json:"role"`
		StaffID   *string `json:"staff_id"`
		StateCode *string `json:"state_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	if req.Role == "" {
		req.Role = "public"
	}
	// Security: restrict self-registration to safe roles only
	if !allowedSelfRegRoles[req.Role] {
		writeError(w, 403, "self-registration is restricted to 'public' and 'observer' roles; elevated roles require admin assignment")
		return
	}
	if len(req.Username) < 3 || len(req.Username) > 64 {
		writeError(w, 400, "username must be 3-64 characters")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, 400, "password must be at least 8 characters")
		return
	}
	if len(req.Password) > 128 {
		writeError(w, 400, "password must be at most 128 characters")
		return
	}
	hasUpper, hasLower, hasDigit := false, false, false
	for _, c := range req.Password {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		writeError(w, 400, "password must contain uppercase, lowercase, and digit")
		return
	}
	if len(req.FullName) < 2 {
		writeError(w, 400, "full_name is required")
		return
	}
	var exists int
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM users WHERE username=?", req.Username).Scan(&exists)
	if exists > 0 {
		writeError(w, 400, "Username already exists")
		return
	}
	pwHash := hashPassword(req.Password)
	uid := insertReturningID(db, "INSERT INTO users (username, password_hash, full_name, role, staff_id, state_code) VALUES (?,?,?,?,?,?)",
		req.Username, pwHash, req.FullName, req.Role, req.StaffID, req.StateCode)
	if uid == 0 {
		writeError(w, 500, "Failed to create user")
		return
	}
	token, _ := createAccessToken(map[string]interface{}{
		"sub": fmt.Sprintf("%d", uid), "username": req.Username, "role": req.Role, "full_name": req.FullName,
	})
	writeJSON(w, 200, M{
		"access_token": token, "token_type": "bearer",
		"user": M{"id": uid, "username": req.Username, "full_name": req.FullName, "role": req.Role, "staff_id": req.StaffID, "state_code": req.StateCode},
	})
}

func handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, 400, "refresh_token is required")
		return
	}
	claims, err := decodeToken(req.RefreshToken)
	if err != nil {
		writeError(w, 401, "invalid or expired refresh token")
		return
	}
	tokenType, _ := claims["type"].(string)
	if tokenType != "refresh" {
		writeError(w, 401, "not a refresh token")
		return
	}
	// Issue new access + refresh tokens
	baseClaims := map[string]interface{}{
		"sub": claims["sub"], "username": claims["username"], "role": claims["role"], "full_name": claims["full_name"],
	}
	newAccess, _ := createAccessToken(baseClaims)
	newRefresh, _ := createRefreshToken(baseClaims)
	writeJSON(w, 200, M{
		"access_token": newAccess, "refresh_token": newRefresh, "token_type": "bearer", "expires_in": 3600,
	})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := getCurrentUser(r)
	if err != nil {
		writeError(w, 401, "Not authenticated")
		return
	}
	writeJSON(w, 200, user)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardAuth(w, r)
	if !ok {
		return
	}
	// Get JTI from token claims (if present)
	jti, _ := claims["jti"].(string)
	userIDStr, _ := claims["sub"].(string)
	var userID int
	fmt.Sscanf(userIDStr, "%d", &userID)

	if jti != "" {
		expiresAt := time.Now().Add(24 * time.Hour)
		blacklist.revokeToken(jti, userID, expiresAt, "user_logout")
	}

	// Remove all sessions for this user from active sessions
	dbExecLog("active_sessions", convertPlaceholders("DELETE FROM active_sessions WHERE user_id = ?"), userID)

	auditWrite("user_logout", "user", userIDStr, r, nil)

	// Clear httpOnly auth cookies
	http.SetCookie(w, &http.Cookie{Name: "inec_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: "inec_refresh", Value: "", Path: "/auth/refresh", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})

	writeJSON(w, 200, M{"message": "logged out successfully"})
}

// ── Elections ──

func handleListElections(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = dbQueryCtx(r.Context(), "SELECT * FROM elections WHERE status=? ORDER BY election_date DESC LIMIT 500", status)
	} else {
		rows, err = dbQueryCtx(r.Context(), "SELECT * FROM elections ORDER BY election_date DESC LIMIT 500")
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, scanRows(rows))
}

func handleGetElection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	row, err := querySingleRow("SELECT * FROM elections WHERE id=?", id)
	if err != nil {
		writeError(w, 404, "Election not found")
		return
	}
	writeJSON(w, 200, row)
}

func handleCreateElection(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	var req ElectionCreate
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Status == "" {
		// FSM initial state is "draft"; "upcoming" is legacy vocabulary.
		req.Status = "draft"
	}
	lid := insertReturningID(db, "INSERT INTO elections (title, election_type, election_date, status, description) VALUES (?,?,?,?,?)",
		req.Title, req.ElectionType, req.ElectionDate, req.Status, req.Description)
	auditWrite("ELECTION_CREATED", "election", fmt.Sprintf("%d", lid), r, map[string]interface{}{"title": req.Title})
	writeJSON(w, 200, M{"id": lid, "message": "Election created"})
}

func handleUpdateElection(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	var updates []string
	var vals []interface{}
	if v, ok := req["title"]; ok && v != nil {
		updates = append(updates, "title=?")
		vals = append(vals, v)
	}
	if v, ok := req["status"]; ok && v != nil {
		updates = append(updates, "status=?")
		vals = append(vals, v)
	}
	if v, ok := req["description"]; ok && v != nil {
		updates = append(updates, "description=?")
		vals = append(vals, v)
	}
	if len(updates) == 0 {
		writeError(w, 400, "No fields to update")
		return
	}
	updates = append(updates, "updated_at=CURRENT_TIMESTAMP")
	vals = append(vals, id)
	dbExecCtx(r.Context(), "UPDATE elections SET "+strings.Join(updates, ",")+` WHERE id=?`, vals...)
	auditWrite("ELECTION_UPDATED", "election", id, r, req)
	writeJSON(w, 200, M{"message": "Election updated"})
}

func handleElectionStats(w http.ResponseWriter, r *http.Request) {
	eid := mux.Vars(r)["id"]
	election, err := querySingleRow("SELECT * FROM elections WHERE id=?", eid)
	if err != nil {
		writeError(w, 404, "Election not found")
		return
	}
	var totalResults, finalized, validated, pending, disputed, totalPUs int
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=?", eid).Scan(&totalResults)
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=? AND status='finalized'", eid).Scan(&finalized)
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=? AND status='validated'", eid).Scan(&validated)
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=? AND status='pending'", eid).Scan(&pending)
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=? AND status='disputed'", eid).Scan(&disputed)
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM polling_units").Scan(&totalPUs)

	var validV, rejectedV, castV, accreditedV sql.NullInt64
	dbQueryRowCtx(r.Context(), `SELECT SUM(total_valid_votes), SUM(rejected_votes), SUM(total_votes_cast), SUM(accredited_voters)
		FROM results WHERE election_id=? AND status IN ('finalized','validated')`, eid).Scan(&validV, &rejectedV, &castV, &accreditedV)

	rows, _ := dbQueryCtx(r.Context(), `SELECT rps.party_code, p.name as party_name, p.color, SUM(rps.votes) as total_votes
		FROM result_party_scores rps JOIN results r ON r.id=rps.result_id JOIN parties p ON p.code=rps.party_code
		WHERE r.election_id=? AND r.status IN ('finalized','validated') GROUP BY rps.party_code, p.name, p.color ORDER BY total_votes DESC`, eid)
	partyScores := scanRows(rows)

	comp := 0.0
	if totalPUs > 0 {
		comp = math.Round(float64(totalResults)/float64(totalPUs)*10000) / 100
	}
	writeJSON(w, 200, M{
		"election": election, "total_polling_units": totalPUs, "results_received": totalResults,
		"results_finalized": finalized, "results_validated": validated, "results_pending": pending, "results_disputed": disputed,
		"completion_percentage": comp,
		"total_valid_votes":     nullInt(validV), "total_rejected_votes": nullInt(rejectedV),
		"total_votes_cast": nullInt(castV), "total_accredited_voters": nullInt(accreditedV),
		"party_scores": partyScores,
	})
}

// ── Results ──

func logAudit(action, entityType, entityID string, userID int, details map[string]interface{}) {
	logAuditCtx(context.Background(), action, entityType, entityID, userID, details)
}

func logAuditCtx(ctx context.Context, action, entityType, entityID string, userID int, details map[string]interface{}) {
	var prevHash sql.NullString
	dbQueryRowCtx(ctx, "SELECT block_hash FROM audit_log ORDER BY id DESC LIMIT 1").Scan(&prevHash)
	prev := strings.Repeat("0", 64)
	if prevHash.Valid {
		prev = prevHash.String
	}
	blockData := fmt.Sprintf("%s%s%s%s", prev, action, entityID, time.Now().UTC().Format(time.RFC3339))
	h := sha256.Sum256([]byte(blockData))
	blockHash := hex.EncodeToString(h[:])
	detailsJSON, _ := json.Marshal(details)
	dbExecCtx(ctx, "INSERT INTO audit_log (action, entity_type, entity_id, user_id, details, block_hash, prev_block_hash) VALUES (?,?,?,?,?,?,?)",
		action, entityType, entityID, userID, string(detailsJSON), blockHash, prev)
}

func handleSubmitResult(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "presiding_officer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	var req struct {
		ElectionID      int    `json:"election_id"`
		PollingUnitCode string `json:"polling_unit_code"`
		PartyScores     []struct {
			PartyCode string `json:"party_code"`
			Votes     int    `json:"votes"`
		} `json:"party_scores"`
		AccreditedVoters int      `json:"accredited_voters"`
		RejectedVotes    int      `json:"rejected_votes"`
		DeviceLat        *float64 `json:"device_lat"`
		DeviceLng        *float64 `json:"device_lng"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if req.ElectionID == 0 || req.PollingUnitCode == "" || len(req.PartyScores) == 0 {
		writeError(w, 400, "election_id, polling_unit_code, and party_scores are required")
		return
	}

	// Geofence validation — if device location provided, enforce proximity to polling unit
	if req.DeviceLat != nil && req.DeviceLng != nil {
		geoResult, err := validateGeofence(*req.DeviceLat, *req.DeviceLng, req.PollingUnitCode)
		if err == nil && geoResult != nil && !geoResult.WithinGeofence {
			writeError(w, 403, fmt.Sprintf("Geofence violation: device is %.0fm from polling unit (allowed: %dm)", geoResult.DistanceMeters, geoResult.AllowedRadiusM))
			return
		}
	}

	var eExists int
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM elections WHERE id=? AND status='active'", req.ElectionID).Scan(&eExists)
	if eExists == 0 {
		writeError(w, 400, "Election not found or not active")
		return
	}
	var regVoters int
	if err := dbQueryRowCtx(r.Context(), "SELECT registered_voters FROM polling_units WHERE code=?", req.PollingUnitCode).Scan(&regVoters); err != nil {
		writeError(w, 400, "Polling unit not found")
		return
	}
	var dupCheck int
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=? AND polling_unit_code=?", req.ElectionID, req.PollingUnitCode).Scan(&dupCheck)
	if dupCheck > 0 {
		writeError(w, 400, "Result already submitted for this polling unit")
		return
	}
	totalValid := 0
	partyEntries := make([]PartyVoteEntry, len(req.PartyScores))
	for i, ps := range req.PartyScores {
		totalValid += ps.Votes
		partyEntries[i] = PartyVoteEntry{PartyCode: ps.PartyCode, Votes: ps.Votes}
	}
	totalCast := totalValid + req.RejectedVotes

	// Full EC8A validation — enforces all 7 INEC business rules
	ec8aForm := &FormEC8A{
		ElectionID:       req.ElectionID,
		PollingUnitCode:  req.PollingUnitCode,
		RegisteredVoters: regVoters,
		AccreditedVoters: req.AccreditedVoters,
		TotalVotesPolled: totalCast,
		RejectedBallots:  req.RejectedVotes,
		TotalValidVotes:  totalValid,
		PartyResults:     partyEntries,
	}
	if violations := ValidateEC8A(ec8aForm); len(violations) > 0 {
		writeError(w, 400, fmt.Sprintf("EC8A validation failed: %s", strings.Join(violations, "; ")))
		return
	}

	ec8aHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(req.PollingUnitCode)))
	userSub, _ := user["sub"].(string)
	userID, _ := strconv.Atoi(userSub)

	userRole, _ := user["role"].(string)
	if !checkPermission(userRole, "submit_result") {
		writeError(w, 403, "Permission denied by Permify")
		return
	}

	tbTransfer, tbErr := createTBTransfer(0, int64(totalCast), req.PollingUnitCode)
	if tbErr != nil {
		log.Error().Err(tbErr).Str("pu_code", req.PollingUnitCode).Msg("Native TigerBeetle transfer creation failed")
		writeError(w, http.StatusServiceUnavailable, "TigerBeetle ledger is unavailable; result submission was not recorded")
		return
	}
	tbID := tbTransfer.ID

	// Use transaction for atomic result + party scores insert
	tx, txErr := db.BeginTx(r.Context(), nil)
	if txErr != nil {
		writeError(w, 500, "database transaction error")
		return
	}
	resultID := insertReturningID(tx, `INSERT INTO results (election_id, polling_unit_code, presiding_officer_id, status,
		total_valid_votes, rejected_votes, total_votes_cast, accredited_voters,
		ec8a_hash, tigerbeetle_transfer_id, tigerbeetle_status, hyperledger_status)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		req.ElectionID, req.PollingUnitCode, userID, "pending",
		totalValid, req.RejectedVotes, totalCast, req.AccreditedVoters,
		ec8aHash, tbID, "PENDING", "PENDING")

	// Batch insert party scores (single multi-value INSERT on PostgreSQL)
	if err := batchInsertPartyScores(tx, resultID, req.PartyScores); err != nil {
		tx.Rollback()
		writeError(w, 500, "failed to save party scores")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "failed to commit result")
		return
	}

	logAudit("RESULT_SUBMITTED", "result", fmt.Sprintf("%d", resultID), userID,
		map[string]interface{}{"phase": "Pre-Validation", "polling_unit": req.PollingUnitCode, "tigerbeetle_id": tbID})

	// Sharded WS broadcast: resolve PU → state for targeted fan-out
	var puStateCode string
	dbReadQueryRow(r.Context(), `SELECT s.code FROM polling_units pu
		JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code
		JOIN states s ON s.code=l.state_code WHERE pu.code=?`, req.PollingUnitCode).Scan(&puStateCode)
	go broadcastWSSharded(M{"type": "result_updated", "pu_code": req.PollingUnitCode, "election_id": req.ElectionID, "state_code": puStateCode}, puStateCode)

	// Broadcast to SSE observers
	go NotifyResultSubmission(map[string]interface{}{
		"result_id": resultID, "polling_unit_code": req.PollingUnitCode,
		"election_id": req.ElectionID, "total_votes": totalCast,
	})

	// Trigger auto-collation check (non-blocking)
	go checkAutoCollation(req.ElectionID, req.PollingUnitCode)

	go publishResultEvent(TopicResultSubmitted, resultID, req.PollingUnitCode, req.ElectionID, userID,
		map[string]interface{}{"phase": "Pre-Validation", "tigerbeetle_id": tbID})
	go publishAuditEvent("RESULT_SUBMITTED", "result", fmt.Sprintf("%d", resultID), userID,
		map[string]interface{}{"polling_unit": req.PollingUnitCode})

	ws := startResultWorkflow("ResultSubmissionWorkflow", resultID, map[string]interface{}{
		"result_id": resultID, "pu_code": req.PollingUnitCode, "election_id": req.ElectionID,
	})
	wfID := ""
	if ws != nil {
		wfID = ws.WorkflowID
	}

	invalidateCollationCache(req.ElectionID)

	writeJSON(w, 200, M{"id": resultID, "status": "pending", "tigerbeetle_transfer_id": tbID, "workflow_id": wfID, "phase": "Pre-Validation", "message": "Result submitted. Proceeding to Edge Validation."})
}

func handleValidateResult(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "collation_officer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	userSub, _ := user["sub"].(string)
	uid, _ := strconv.Atoi(userSub)
	userRole, _ := user["role"].(string)
	if !checkPermission(userRole, "validate_result") {
		writeError(w, 403, "Permission denied by Permify")
		return
	}

	// Fix #7: Use transaction with SELECT FOR UPDATE to prevent concurrent transitions
	tx, txErr := db.BeginTx(r.Context(), nil)
	if txErr != nil {
		writeError(w, 500, "database transaction error")
		return
	}
	var status, puCode string
	if err := tx.QueryRow(convertPlaceholders("SELECT status, polling_unit_code FROM results WHERE id=? FOR UPDATE"), id).Scan(&status, &puCode); err != nil {
		tx.Rollback()
		writeError(w, 404, "Result not found")
		return
	}
	if !canTransition(status, "validated") {
		tx.Rollback()
		writeError(w, 400, fmt.Sprintf("cannot transition from '%s' to 'validated'; allowed transitions: %v", status, validTransitions[status]))
		return
	}
	tx.Exec(convertPlaceholders("UPDATE results SET status='validated', validated_at=CURRENT_TIMESTAMP WHERE id=?"), id)
	tx.Commit()
	logAudit("RESULT_VALIDATED", "result", id, uid, map[string]interface{}{"phase": "Edge Validation", "polling_unit": puCode})
	go broadcastWS(M{"type": "result_updated", "result_id": id})

	idInt, _ := strconv.ParseInt(id, 10, 64)
	go publishResultEvent(TopicResultValidated, idInt, puCode, 0, uid,
		map[string]interface{}{"phase": "Edge Validation"})
	go publishAuditEvent("RESULT_VALIDATED", "result", id, uid, map[string]interface{}{"polling_unit": puCode})

	startResultWorkflow("ResultValidationWorkflow", idInt, map[string]interface{}{"result_id": id})

	writeJSON(w, 200, M{"status": "validated", "phase": "Edge Validation"})
}

func handleFinalizeResult(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "collation_officer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var status, puCode string
	if err := dbQueryRowCtx(r.Context(), "SELECT status, polling_unit_code FROM results WHERE id=?", id).Scan(&status, &puCode); err != nil {
		writeError(w, 404, "Result not found")
		return
	}
	if !canTransition(status, "finalized") {
		writeError(w, 400, fmt.Sprintf("cannot transition from '%s' to 'finalized'; only 'validated' results can be finalized", status))
		return
	}
	userSub, _ := user["sub"].(string)
	uid, _ := strconv.Atoi(userSub)
	userRole, _ := user["role"].(string)
	if !checkPermission(userRole, "finalize_result") {
		writeError(w, 403, "Permission denied by Permify")
		return
	}

	var tbTransferID sql.NullString
	dbQueryRowCtx(r.Context(), "SELECT tigerbeetle_transfer_id FROM results WHERE id=?", id).Scan(&tbTransferID)
	if tbTransferID.Valid && tbTransferID.String != "" {
		if err := postTBTransfer(tbTransferID.String); err != nil {
			log.Error().Err(err).Str("transfer_id", tbTransferID.String).Msg("Native TigerBeetle transfer posting failed")
			writeError(w, http.StatusServiceUnavailable, "TigerBeetle ledger posting failed; result was not finalized")
			return
		}
	}

	idInt, _ := strconv.ParseInt(id, 10, 64)

	// External Fabric/IPFS anchoring is deliberately not fabricated. The
	// election result is finalized only after its real TigerBeetle transfer is
	// posted; external anchoring remains explicitly unconfigured.
	dbExecCtx(r.Context(), `UPDATE results SET status='finalized', finalized_at=CURRENT_TIMESTAMP,
		tigerbeetle_status='POSTED', hyperledger_status='NOT_CONFIGURED', hyperledger_tx_id=NULL, ipfs_cid=NULL WHERE id=?`, id)
	logAudit("RESULT_FINALIZED", "result", id, uid, map[string]interface{}{"phase": "Finalization", "polling_unit": puCode, "external_anchoring": "not_configured"})

	go publishResultEvent(TopicResultFinalized, idInt, puCode, 0, uid,
		map[string]interface{}{"phase": "Finalization", "external_anchoring": "not_configured"})
	go publishAuditEvent("RESULT_FINALIZED", "result", id, uid, map[string]interface{}{"polling_unit": puCode})

	startResultWorkflow("ResultFinalizationWorkflow", idInt, map[string]interface{}{"result_id": id})

	writeJSON(w, 200, M{"status": "finalized", "phase": "Finalization", "tigerbeetle_status": "POSTED", "hyperledger_status": "NOT_CONFIGURED", "external_anchoring": "not_configured"})
}

func handleDisputeResult(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "observer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var status, puCode string
	if err := dbQueryRowCtx(r.Context(), "SELECT status, polling_unit_code FROM results WHERE id=?", id).Scan(&status, &puCode); err != nil {
		writeError(w, 404, "Result not found")
		return
	}
	if !canTransition(status, "disputed") {
		writeError(w, 400, fmt.Sprintf("cannot dispute result with status '%s'; already in terminal state", status))
		return
	}
	userSub, _ := user["sub"].(string)
	uid, _ := strconv.Atoi(userSub)
	userRole, _ := user["role"].(string)
	if !checkPermission(userRole, "dispute_result") {
		writeError(w, 403, "Permission denied by Permify")
		return
	}

	var tbTransferID sql.NullString
	dbQueryRowCtx(r.Context(), "SELECT tigerbeetle_transfer_id FROM results WHERE id=?", id).Scan(&tbTransferID)
	if tbTransferID.Valid && tbTransferID.String != "" {
		if err := voidTBTransfer(tbTransferID.String); err != nil {
			log.Error().Err(err).Str("transfer_id", tbTransferID.String).Msg("Native TigerBeetle transfer voiding failed")
			writeError(w, http.StatusServiceUnavailable, "TigerBeetle ledger void failed; result was not disputed")
			return
		}
	}

	dbExecCtx(r.Context(), "UPDATE results SET status='disputed', tigerbeetle_status='VOIDED' WHERE id=?", id)
	logAudit("RESULT_DISPUTED", "result", id, uid, map[string]interface{}{"phase": "Dispute", "polling_unit": puCode})
	go broadcastWS(M{"type": "result_updated", "result_id": id})

	idInt, _ := strconv.ParseInt(id, 10, 64)
	go publishResultEvent(TopicResultDisputed, idInt, puCode, 0, uid,
		map[string]interface{}{"phase": "Dispute"})
	go publishAuditEvent("RESULT_DISPUTED", "result", id, uid, map[string]interface{}{"polling_unit": puCode})

	writeJSON(w, 200, M{"status": "disputed", "tigerbeetle_status": "VOIDED"})
}

func handleListResults(w http.ResponseWriter, r *http.Request) {
	eid := queryParam(r, "election_id", "1")
	status := r.URL.Query().Get("status")
	stateCode := r.URL.Query().Get("state_code")
	lgaCode := r.URL.Query().Get("lga_code")
	limit := queryParamInt(r, "limit", 50)
	offset := queryParamInt(r, "offset", 0)

	q := `SELECT r.id, r.election_id, r.polling_unit_code, r.presiding_officer_id, r.status,
		r.total_valid_votes, r.rejected_votes, r.total_votes_cast, r.accredited_voters,
		r.ec8a_hash, r.tigerbeetle_transfer_id, r.hyperledger_tx_id,
		r.tigerbeetle_status, r.hyperledger_status, r.ipfs_cid,
		r.submitted_at, r.validated_at, r.finalized_at,
		pu.name as pu_name, pu.ward_code, w.name as ward_name, w.lga_code,
		l.name as lga_name, l.state_code, s.name as state_name
		FROM results r
		JOIN polling_units pu ON pu.code=r.polling_unit_code
		JOIN wards w ON w.code=pu.ward_code
		JOIN lgas l ON l.code=w.lga_code
		JOIN states s ON s.code=l.state_code
		WHERE r.election_id=?`
	params := []interface{}{eid}
	if status != "" {
		q += " AND r.status=?"
		params = append(params, status)
	}
	if stateCode != "" {
		q += " AND l.state_code=?"
		params = append(params, stateCode)
	}
	if lgaCode != "" {
		q += " AND w.lga_code=?"
		params = append(params, lgaCode)
	}

	countQ := strings.Replace(q, "SELECT r.id, r.election_id, r.polling_unit_code, r.presiding_officer_id, r.status,\n\t\tr.total_valid_votes, r.rejected_votes, r.total_votes_cast, r.accredited_voters,\n\t\tr.ec8a_hash, r.tigerbeetle_transfer_id, r.hyperledger_tx_id,\n\t\tr.tigerbeetle_status, r.hyperledger_status, r.ipfs_cid,\n\t\tr.submitted_at, r.validated_at, r.finalized_at,\n\t\tpu.name as pu_name, pu.ward_code, w.name as ward_name, w.lga_code,\n\t\tl.name as lga_name, l.state_code, s.name as state_name", "SELECT COUNT(*) as total", 1)
	var total int
	dbQueryRowCtx(r.Context(), countQ, params...).Scan(&total)

	q += " ORDER BY r.submitted_at DESC LIMIT ? OFFSET ?"
	params = append(params, limit, offset)
	rows, _ := dbQueryCtx(r.Context(), q, params...)
	results := scanRows(rows)

	for i, res := range results {
		rid, _ := res["id"]
		psRows, _ := dbQueryCtx(r.Context(), `SELECT rps.party_code, p.name as party_name, p.color, rps.votes
			FROM result_party_scores rps JOIN parties p ON p.code=rps.party_code WHERE rps.result_id=? ORDER BY rps.votes DESC`, rid)
		results[i]["party_scores"] = scanRows(psRows)
	}
	writeJSON(w, 200, M{"total": total, "results": results})
}

func handleGetResult(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	rows, _ := dbQueryCtx(r.Context(), `SELECT r.*, pu.name as pu_name, pu.ward_code, pu.registered_voters,
		w.name as ward_name, w.lga_code, l.name as lga_name, l.state_code, s.name as state_name
		FROM results r JOIN polling_units pu ON pu.code=r.polling_unit_code
		JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code JOIN states s ON s.code=l.state_code
		WHERE r.id=?`, id)
	all := scanRows(rows)
	if len(all) == 0 {
		writeError(w, 404, "Result not found")
		return
	}
	result := all[0]
	psRows, _ := dbQueryCtx(r.Context(), `SELECT rps.party_code, p.name as party_name, p.color, rps.votes
		FROM result_party_scores rps JOIN parties p ON p.code=rps.party_code WHERE rps.result_id=? ORDER BY rps.votes DESC`, id)
	result["party_scores"] = scanRows(psRows)
	writeJSON(w, 200, result)
}

// ── Geo ──

func handleListStates(w http.ResponseWriter, r *http.Request) {
	rows, _ := dbQueryCtx(r.Context(), "SELECT * FROM states ORDER BY name")
	writeJSON(w, 200, scanRows(rows))
}

func handleGetState(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]
	row, err := querySingleRow("SELECT * FROM states WHERE code=?", code)
	if err != nil {
		writeJSON(w, 200, M{"error": "State not found"})
		return
	}
	writeJSON(w, 200, row)
}

func handleListLGAs(w http.ResponseWriter, r *http.Request) {
	sc := r.URL.Query().Get("state_code")
	var rows *sql.Rows
	var err error
	if sc != "" {
		rows, err = dbQueryCtx(r.Context(), "SELECT l.*, s.name as state_name FROM lgas l JOIN states s ON s.code=l.state_code WHERE l.state_code=? ORDER BY l.name", sc)
	} else {
		rows, err = dbQueryCtx(r.Context(), "SELECT l.*, s.name as state_name FROM lgas l JOIN states s ON s.code=l.state_code ORDER BY l.name")
	}
	if err != nil {
		log.Error().Err(err).Msg("Failed to query LGAs")
		writeJSON(w, 500, M{"error": "database query failed"})
		return
	}
	writeJSON(w, 200, scanRows(rows))
}

func handleListWards(w http.ResponseWriter, r *http.Request) {
	lc := r.URL.Query().Get("lga_code")
	var rows *sql.Rows
	var err error
	if lc != "" {
		rows, err = dbQueryCtx(r.Context(), "SELECT w.*, l.name as lga_name FROM wards w JOIN lgas l ON l.code=w.lga_code WHERE w.lga_code=? ORDER BY w.name", lc)
	} else {
		rows, err = dbQueryCtx(r.Context(), "SELECT w.*, l.name as lga_name FROM wards w JOIN lgas l ON l.code=w.lga_code ORDER BY w.name LIMIT 100")
	}
	if err != nil {
		log.Error().Err(err).Msg("Failed to query wards")
		writeJSON(w, 500, M{"error": "database query failed"})
		return
	}
	writeJSON(w, 200, scanRows(rows))
}

func handleListPollingUnits(w http.ResponseWriter, r *http.Request) {
	wc := r.URL.Query().Get("ward_code")
	lc := r.URL.Query().Get("lga_code")
	sc := r.URL.Query().Get("state_code")
	limit := queryParamInt(r, "limit", 50)
	offset := queryParamInt(r, "offset", 0)

	q := `SELECT pu.*, w.name as ward_name, w.lga_code, l.name as lga_name, l.state_code, s.name as state_name
		FROM polling_units pu JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code JOIN states s ON s.code=l.state_code`
	var conds []string
	var params []interface{}
	if wc != "" {
		conds = append(conds, "pu.ward_code=?")
		params = append(params, wc)
	}
	if lc != "" {
		conds = append(conds, "w.lga_code=?")
		params = append(params, lc)
	}
	if sc != "" {
		conds = append(conds, "l.state_code=?")
		params = append(params, sc)
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY pu.name LIMIT ? OFFSET ?"
	params = append(params, limit, offset)
	rows, _ := dbQueryCtx(r.Context(), q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleGetPollingUnit(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]
	rows, _ := dbQueryCtx(r.Context(), `SELECT pu.*, w.name as ward_name, w.lga_code, l.name as lga_name, l.state_code, s.name as state_name
		FROM polling_units pu JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code JOIN states s ON s.code=l.state_code
		WHERE pu.code=?`, code)
	all := scanRows(rows)
	if len(all) == 0 {
		writeJSON(w, 200, M{"error": "Polling unit not found"})
		return
	}
	writeJSON(w, 200, all[0])
}

func handleMapData(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	sc := r.URL.Query().Get("state_code")

	cacheKey := fmt.Sprintf("map_data_%d_%s", eid, sc)
	if cached, err := cacheGet(cacheKey); err == nil && cached != "" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(200)
		w.Write([]byte(cached))
		return
	}

	stRows, _ := dbQueryCtx(r.Context(), `SELECT s.code, s.name, s.geo_zone, s.capital,
		COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
		COALESCE(SUM(r.total_valid_votes),0) as total_votes,
		COALESCE(SUM(r.total_votes_cast),0) as total_cast,
		COALESCE(SUM(r.accredited_voters),0) as accredited,
		AVG(pu.latitude) as avg_lat, AVG(pu.longitude) as avg_lng
		FROM states s LEFT JOIN lgas l ON l.state_code=s.code
		LEFT JOIN wards w ON w.lga_code=l.code LEFT JOIN polling_units pu ON pu.ward_code=w.code
		LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
		GROUP BY s.code, s.name, s.geo_zone, s.capital ORDER BY s.name`, eid)
	states := scanRows(stRows)

	codes := make([]string, len(states))
	for i, s := range states {
		codes[i], _ = s["code"].(string)
	}
	psMap := collationPartyScoresBatch(r.Context(), "state_code", codes, eid)
	for i, s := range states {
		code, _ := s["code"].(string)
		if ps, ok := psMap[code]; ok {
			states[i]["party_scores"] = ps
			if len(ps) > 0 {
				states[i]["leading_party"] = ps[0]
			} else {
				states[i]["leading_party"] = nil
			}
		} else {
			states[i]["party_scores"] = []M{}
			states[i]["leading_party"] = nil
		}
	}

	puQ := `SELECT pu.code, pu.name, pu.latitude, pu.longitude, pu.registered_voters,
		w.name as ward_name, l.name as lga_name, l.state_code, s.name as state_name,
		r.id as result_id, r.status, r.total_valid_votes, r.total_votes_cast,
		r.tigerbeetle_status, r.hyperledger_status
		FROM polling_units pu JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code
		JOIN states s ON s.code=l.state_code LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?`
	puParams := []interface{}{eid}
	if sc != "" {
		puQ += " WHERE l.state_code=?"
		puParams = append(puParams, sc)
	}
	puQ += " LIMIT 2000"
	puRows, _ := dbQueryCtx(r.Context(), puQ, puParams...)
	pus := scanRows(puRows)

	rids := make([]interface{}, 0, len(pus))
	ridPH := make([]string, 0, len(pus))
	for _, pu := range pus {
		if rid, ok := pu["result_id"]; ok && rid != nil {
			rids = append(rids, rid)
			ridPH = append(ridPH, "?")
		}
	}
	puPsMap := map[interface{}][]M{}
	if len(rids) > 0 {
		puPsRows, _ := dbQueryCtx(r.Context(), fmt.Sprintf(`SELECT rps.result_id, rps.party_code, p.abbreviation, p.color, rps.votes
			FROM result_party_scores rps JOIN parties p ON p.code=rps.party_code
			WHERE rps.result_id IN (%s) ORDER BY rps.result_id, rps.votes DESC`,
			strings.Join(ridPH, ",")), rids...)
		if puPsRows != nil {
			for puPsRows.Next() {
				var rid interface{}
				var pc, abbr, clr string
				var v int64
				if puPsRows.Scan(&rid, &pc, &abbr, &clr, &v) == nil {
					puPsMap[rid] = append(puPsMap[rid], M{"party_code": pc, "abbreviation": abbr, "color": clr, "votes": v})
				}
			}
			puPsRows.Close()
		}
	}
	for i, pu := range pus {
		if rid, ok := pu["result_id"]; ok && rid != nil {
			if ps, ok := puPsMap[rid]; ok {
				pus[i]["party_scores"] = ps
			} else {
				pus[i]["party_scores"] = []M{}
			}
		} else {
			pus[i]["party_scores"] = []M{}
		}
	}
	w.Header().Set("X-Cache", "MISS")
	cacheSet(cacheKey, M{"states": states, "polling_units": pus}, 15*time.Second)
	writeJSON(w, 200, M{"states": states, "polling_units": pus})
}

func handlePUTile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	z, _ := strconv.Atoi(vars["z"])
	x, _ := strconv.Atoi(vars["x"])
	y, _ := strconv.Atoi(vars["y"])
	eid := queryParamInt(r, "election_id", 1)

	n := math.Pow(2, float64(z))
	lonMin := float64(x)/n*360.0 - 180.0
	lonMax := float64(x+1)/n*360.0 - 180.0
	latMax := math.Atan(math.Sinh(math.Pi*(1-2*float64(y)/n))) * 180.0 / math.Pi
	latMin := math.Atan(math.Sinh(math.Pi*(1-2*float64(y+1)/n))) * 180.0 / math.Pi

	rows, err := dbQueryCtx(r.Context(), `SELECT pu.code, pu.name, pu.latitude as lat, pu.longitude as lon,
		COALESCE(r.status, 'no_result') as status,
		r.submitted_at as submitted_at,
		COALESCE(EXTRACT(EPOCH FROM r.submitted_at)::INTEGER, 0) as submitted_ts
		FROM polling_units pu LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
		WHERE pu.longitude BETWEEN ? AND ? AND pu.latitude BETWEEN ? AND ? LIMIT 10000`,
		eid, lonMin, lonMax, latMin, latMax)
	if err != nil {
		w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
		w.Write(encodeMVTEmpty())
		return
	}

	tile := encodeMVTTile(rows, z, x, y, lonMin, latMin, lonMax, latMax)

	h := sha256.Sum256(tile)
	etag := `W/"` + hex.EncodeToString(h[:]) + `"`
	if inm := r.Header.Get("If-None-Match"); inm == etag {
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=600, stale-while-revalidate=1200")
	w.Write(tile)
}

func handleExportCSV(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	sc := r.URL.Query().Get("state_code")

	q := `SELECT pu.code, pu.name, w.name as ward_name, l.name as lga_name, l.state_code, s.name as state_name,
		COALESCE(r.status,'no_result') as status, pu.registered_voters,
		COALESCE(r.total_valid_votes,0) as total_valid_votes,
		COALESCE(r.total_votes_cast,0) as total_votes_cast, pu.latitude, pu.longitude, r.submitted_at
		FROM polling_units pu JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code
		JOIN states s ON s.code=l.state_code LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?`
	params := []interface{}{eid}
	if sc != "" {
		q += " WHERE l.state_code=?"
		params = append(params, sc)
	}
	rows, _ := dbQueryCtx(r.Context(), q, params...)
	defer rows.Close()

	filename := "polling_units"
	if sc != "" {
		filename += "_" + sc
	}
	filename += ".csv"

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Cache-Control", "no-store")

	writer := csv.NewWriter(w)
	writer.Write([]string{"code", "name", "ward_name", "lga_name", "state_code", "state_name", "status", "registered_voters", "total_valid_votes", "total_votes_cast", "latitude", "longitude", "submitted_at"})

	cols, _ := rows.Columns()
	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		rows.Scan(ptrs...)
		record := make([]string, len(cols))
		for i, v := range vals {
			if v == nil {
				record[i] = ""
			} else {
				record[i] = fmt.Sprintf("%v", v)
			}
		}
		writer.Write(record)
	}
	writer.Flush()
}

func handleExportGeoJSON(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	sc := r.URL.Query().Get("state_code")

	q := `SELECT pu.code, pu.name, pu.latitude, pu.longitude,
		w.name as ward_name, l.name as lga_name, l.state_code, s.name as state_name,
		COALESCE(r.status,'no_result') as status, r.submitted_at
		FROM polling_units pu JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code
		JOIN states s ON s.code=l.state_code LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?`
	params := []interface{}{eid}
	if sc != "" {
		q += " WHERE l.state_code=?"
		params = append(params, sc)
	}
	rows, err := dbQueryCtx(r.Context(), q, params...)
	if err != nil {
		writeError(w, 500, "query failed")
		return
	}
	defer rows.Close()

	filename := "polling_units"
	if sc != "" {
		filename += "_" + sc
	}
	filename += ".geojson"

	w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Cache-Control", "no-store")

	w.Write([]byte(`{"type":"FeatureCollection","features":[`))
	first := true
	enc := json.NewEncoder(w)
	for rows.Next() {
		var code, name, wardName, lgaName, stateCode, stateName, status string
		var lat, lon float64
		var submittedAt sql.NullString
		if err := rows.Scan(&code, &name, &lat, &lon, &wardName, &lgaName, &stateCode, &stateName, &status, &submittedAt); err != nil {
			continue
		}
		if lon == 0 && lat == 0 {
			continue
		}
		if !first {
			w.Write([]byte(","))
		}
		first = false
		sa := ""
		if submittedAt.Valid {
			sa = submittedAt.String
		}
		enc.Encode(M{
			"type":     "Feature",
			"geometry": M{"type": "Point", "coordinates": []float64{lon, lat}},
			"properties": M{
				"code": code, "name": name, "ward": wardName,
				"lga": lgaName, "state": stateName,
				"status": status, "submitted_at": sa,
			},
		})
	}
	w.Write([]byte("]}"))
}

// ── Dashboard ──

func handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)

	cacheKey := fmt.Sprintf("dashboard_stats_%d", eid)
	if cached, err := cacheGet(cacheKey); err == nil && cached != "" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(200)
		w.Write([]byte(cached))
		return
	}

	election, err := querySingleRow("SELECT * FROM elections WHERE id=?", eid)
	if err != nil {
		writeJSON(w, 200, M{"error": "Election not found"})
		return
	}

	var totalPUs, resultsReceived int
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM polling_units").Scan(&totalPUs)
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=?", eid).Scan(&resultsReceived)

	statusRows, _ := dbQueryCtx(r.Context(), "SELECT status, COUNT(*) as count FROM results WHERE election_id=? GROUP BY status", eid)
	statusMap := make(map[string]int)
	for statusRows.Next() {
		var s string
		var c int
		statusRows.Scan(&s, &c)
		statusMap[s] = c
	}
	statusRows.Close()

	var validV, rejectedV, castV, accreditedV sql.NullInt64
	dbQueryRowCtx(r.Context(), `SELECT SUM(total_valid_votes), SUM(rejected_votes), SUM(total_votes_cast), SUM(accredited_voters)
		FROM results WHERE election_id=? AND status IN ('finalized','validated')`, eid).Scan(&validV, &rejectedV, &castV, &accreditedV)

	psRows, _ := dbQueryCtx(r.Context(), `SELECT rps.party_code, p.name as party_name, p.color, p.abbreviation, SUM(rps.votes) as total_votes
		FROM result_party_scores rps JOIN results r ON r.id=rps.result_id JOIN parties p ON p.code=rps.party_code
		WHERE r.election_id=? AND r.status IN ('finalized','validated') GROUP BY rps.party_code, p.name, p.color, p.abbreviation ORDER BY total_votes DESC`, eid)
	partyScores := scanRows(psRows)

	srRows, _ := dbQueryCtx(r.Context(), `SELECT s.code, s.name, s.geo_zone, COUNT(r.id) as results_count, SUM(r.total_valid_votes) as total_votes
		FROM states s LEFT JOIN lgas l ON l.state_code=s.code LEFT JOIN wards w ON w.lga_code=l.code
		LEFT JOIN polling_units pu ON pu.ward_code=w.code
		LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
		GROUP BY s.code, s.name, s.geo_zone, s.capital ORDER BY s.name`, eid)
	stateResults := scanRows(srRows)

	var tbPosted, hlConfirmed int
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=? AND tigerbeetle_status='POSTED'", eid).Scan(&tbPosted)
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=? AND hyperledger_status='CONFIRMED'", eid).Scan(&hlConfirmed)

	zRows, _ := dbQueryCtx(r.Context(), `SELECT s.geo_zone, SUM(r.total_valid_votes) as total_votes, COUNT(r.id) as results_count
		FROM results r JOIN polling_units pu ON pu.code=r.polling_unit_code
		JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code JOIN states s ON s.code=l.state_code
		WHERE r.election_id=? AND r.status IN ('finalized','validated') GROUP BY s.geo_zone`, eid)
	zoneResults := scanRows(zRows)

	comp := 0.0
	if totalPUs > 0 {
		comp = math.Round(float64(resultsReceived)/float64(totalPUs)*10000) / 100
	}
	variance := 0.0
	if resultsReceived > 0 {
		variance = math.Round(math.Abs(float64(tbPosted-hlConfirmed))/float64(resultsReceived)*1000000) / 10000
	}

	result := M{
		"election": election, "total_polling_units": totalPUs, "results_received": resultsReceived,
		"completion_percentage": comp,
		"status_breakdown": M{
			"finalized": statusMap["finalized"], "validated": statusMap["validated"],
			"pending": statusMap["pending"], "disputed": statusMap["disputed"], "voided": statusMap["voided"],
		},
		"vote_totals":  M{"valid": nullInt(validV), "rejected": nullInt(rejectedV), "cast": nullInt(castV), "accredited": nullInt(accreditedV)},
		"party_scores": partyScores, "state_results": stateResults, "zone_results": zoneResults,
		"dual_ledger": M{"tigerbeetle_posted": tbPosted, "hyperledger_confirmed": hlConfirmed, "total_results": resultsReceived, "reconciliation_variance": variance},
	}

	cacheSet(cacheKey, result, 15*time.Second)
	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, 200, result)
}

func handleLiveFeed(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	limit := queryParamInt(r, "limit", 20)
	rows, _ := dbQueryCtx(r.Context(), `SELECT r.id, r.polling_unit_code, r.status, r.total_votes_cast,
		r.tigerbeetle_status, r.hyperledger_status, r.submitted_at,
		pu.name as pu_name, w.name as ward_name, l.name as lga_name, s.name as state_name, s.code as state_code
		FROM results r JOIN polling_units pu ON pu.code=r.polling_unit_code
		JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code JOIN states s ON s.code=l.state_code
		WHERE r.election_id=? ORDER BY r.submitted_at DESC LIMIT ?`, eid, limit)
	writeJSON(w, 200, scanRows(rows))
}

func collationPartyScoresBatch(ctx context.Context, groupCol string, groupCodes []string, eid int) map[string][]M {
	if len(groupCodes) == 0 {
		return map[string][]M{}
	}
	placeholders := make([]string, len(groupCodes))
	args := make([]interface{}, 0, len(groupCodes)+1)
	for i, c := range groupCodes {
		placeholders[i] = "?"
		args = append(args, c)
	}
	args = append(args, eid)

	var q string
	switch groupCol {
	case "state_code":
		q = fmt.Sprintf(`SELECT l.state_code as group_code, rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
			FROM result_party_scores rps JOIN results res ON res.id=rps.result_id
			JOIN polling_units pu ON pu.code=res.polling_unit_code JOIN wards w ON w.code=pu.ward_code
			JOIN lgas l ON l.code=w.lga_code JOIN parties p ON p.code=rps.party_code
			WHERE l.state_code IN (%s) AND res.election_id=? AND res.status IN ('finalized','validated')
			GROUP BY l.state_code, rps.party_code, p.abbreviation, p.color ORDER BY l.state_code, total_votes DESC`,
			strings.Join(placeholders, ","))
	case "lga_code":
		q = fmt.Sprintf(`SELECT w.lga_code as group_code, rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
			FROM result_party_scores rps JOIN results res ON res.id=rps.result_id
			JOIN polling_units pu ON pu.code=res.polling_unit_code JOIN wards w ON w.code=pu.ward_code
			JOIN parties p ON p.code=rps.party_code
			WHERE w.lga_code IN (%s) AND res.election_id=? AND res.status IN ('finalized','validated')
			GROUP BY w.lga_code, rps.party_code, p.abbreviation, p.color ORDER BY w.lga_code, total_votes DESC`,
			strings.Join(placeholders, ","))
	case "ward_code":
		q = fmt.Sprintf(`SELECT pu.ward_code as group_code, rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
			FROM result_party_scores rps JOIN results res ON res.id=rps.result_id
			JOIN polling_units pu ON pu.code=res.polling_unit_code JOIN parties p ON p.code=rps.party_code
			WHERE pu.ward_code IN (%s) AND res.election_id=? AND res.status IN ('finalized','validated')
			GROUP BY pu.ward_code, rps.party_code, p.abbreviation, p.color ORDER BY pu.ward_code, total_votes DESC`,
			strings.Join(placeholders, ","))
	default:
		return map[string][]M{}
	}

	rows, err := dbQueryCtx(ctx, q, args...)
	if err != nil {
		return map[string][]M{}
	}
	result := map[string][]M{}
	for rows.Next() {
		var groupCode, partyCode, abbreviation, color string
		var totalVotes int64
		if rows.Scan(&groupCode, &partyCode, &abbreviation, &color, &totalVotes) == nil {
			result[groupCode] = append(result[groupCode], M{
				"party_code": partyCode, "abbreviation": abbreviation,
				"color": color, "total_votes": totalVotes,
			})
		}
	}
	rows.Close()
	return result
}

func handleCollation(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	level := queryParam(r, "level", "state")
	parentCode := r.URL.Query().Get("parent_code")

	cacheKey := fmt.Sprintf("collation_%s_%d_%s", level, eid, parentCode)
	if cached, err := cacheGet(cacheKey); err == nil && cached != "" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(200)
		w.Write([]byte(cached))
		return
	}

	switch level {
	case "state":
		rows, _ := dbQueryCtx(r.Context(), `SELECT s.code, s.name, s.geo_zone,
			COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
			SUM(r.total_valid_votes) as total_valid_votes, SUM(r.rejected_votes) as rejected_votes,
			SUM(r.total_votes_cast) as total_votes_cast
			FROM states s LEFT JOIN lgas l ON l.state_code=s.code LEFT JOIN wards w ON w.lga_code=l.code
			LEFT JOIN polling_units pu ON pu.ward_code=w.code
			LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
			GROUP BY s.code, s.name, s.geo_zone, s.capital ORDER BY s.name`, eid)
		results := scanRows(rows)
		codes := make([]string, len(results))
		for i, res := range results {
			codes[i], _ = res["code"].(string)
		}
		psMap := collationPartyScoresBatch(r.Context(), "state_code", codes, eid)
		for i, res := range results {
			code, _ := res["code"].(string)
			if ps, ok := psMap[code]; ok {
				results[i]["party_scores"] = ps
			} else {
				results[i]["party_scores"] = []M{}
			}
		}
		w.Header().Set("X-Cache", "MISS")
		cacheSet(cacheKey, results, 15*time.Second)
		writeJSON(w, 200, results)

	case "lga":
		rows, _ := dbQueryCtx(r.Context(), `SELECT l.code, l.name,
			COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
			SUM(r.total_valid_votes) as total_valid_votes, SUM(r.rejected_votes) as rejected_votes,
			SUM(r.total_votes_cast) as total_votes_cast
			FROM lgas l LEFT JOIN wards w ON w.lga_code=l.code LEFT JOIN polling_units pu ON pu.ward_code=w.code
			LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
			WHERE l.state_code=? GROUP BY l.code, l.name ORDER BY l.name`, eid, parentCode)
		results := scanRows(rows)
		codes := make([]string, len(results))
		for i, res := range results {
			codes[i], _ = res["code"].(string)
		}
		psMap := collationPartyScoresBatch(r.Context(), "lga_code", codes, eid)
		for i, res := range results {
			code, _ := res["code"].(string)
			if ps, ok := psMap[code]; ok {
				results[i]["party_scores"] = ps
			} else {
				results[i]["party_scores"] = []M{}
			}
		}
		w.Header().Set("X-Cache", "MISS")
		cacheSet(cacheKey, results, 15*time.Second)
		writeJSON(w, 200, results)

	case "ward":
		rows, _ := dbQueryCtx(r.Context(), `SELECT w.code, w.name,
			COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
			SUM(r.total_valid_votes) as total_valid_votes, SUM(r.rejected_votes) as rejected_votes,
			SUM(r.total_votes_cast) as total_votes_cast
			FROM wards w LEFT JOIN polling_units pu ON pu.ward_code=w.code
			LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
			WHERE w.lga_code=? GROUP BY w.code, w.name ORDER BY w.name`, eid, parentCode)
		results := scanRows(rows)
		codes := make([]string, len(results))
		for i, res := range results {
			codes[i], _ = res["code"].(string)
		}
		psMap := collationPartyScoresBatch(r.Context(), "ward_code", codes, eid)
		for i, res := range results {
			code, _ := res["code"].(string)
			if ps, ok := psMap[code]; ok {
				results[i]["party_scores"] = ps
			} else {
				results[i]["party_scores"] = []M{}
			}
		}
		w.Header().Set("X-Cache", "MISS")
		cacheSet(cacheKey, results, 15*time.Second)
		writeJSON(w, 200, results)

	case "pu":
		rows, _ := dbQueryCtx(r.Context(), `SELECT pu.code, pu.name, pu.registered_voters,
			r.id as result_id, r.status, r.total_valid_votes, r.rejected_votes,
			r.total_votes_cast, r.accredited_voters, r.tigerbeetle_status, r.hyperledger_status
			FROM polling_units pu LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
			WHERE pu.ward_code=? ORDER BY pu.name`, eid, parentCode)
		results := scanRows(rows)
		rids := make([]interface{}, 0, len(results))
		ridPlaceholders := make([]string, 0, len(results))
		for _, res := range results {
			if rid, ok := res["result_id"]; ok && rid != nil {
				rids = append(rids, rid)
				ridPlaceholders = append(ridPlaceholders, "?")
			}
		}
		psMap := map[interface{}][]M{}
		if len(rids) > 0 {
			psRows, _ := dbQueryCtx(r.Context(), fmt.Sprintf(`SELECT rps.result_id, rps.party_code, p.abbreviation, p.color, rps.votes
				FROM result_party_scores rps JOIN parties p ON p.code=rps.party_code
				WHERE rps.result_id IN (%s) ORDER BY rps.result_id, rps.votes DESC`,
				strings.Join(ridPlaceholders, ",")), rids...)
			if psRows != nil {
				for psRows.Next() {
					var rid interface{}
					var partyCode, abbreviation, color string
					var votes int64
					if psRows.Scan(&rid, &partyCode, &abbreviation, &color, &votes) == nil {
						psMap[rid] = append(psMap[rid], M{
							"party_code": partyCode, "abbreviation": abbreviation,
							"color": color, "votes": votes,
						})
					}
				}
				psRows.Close()
			}
		}
		for i, res := range results {
			if rid, ok := res["result_id"]; ok && rid != nil {
				if ps, ok := psMap[rid]; ok {
					results[i]["party_scores"] = ps
				} else {
					results[i]["party_scores"] = []M{}
				}
			} else {
				results[i]["party_scores"] = []M{}
			}
		}
		w.Header().Set("X-Cache", "MISS")
		cacheSet(cacheKey, results, 15*time.Second)
		writeJSON(w, 200, results)

	default:
		writeJSON(w, 200, []M{})
	}
}

func handlePostClientMetric(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	ua := r.Header.Get("User-Agent")
	ip := r.RemoteAddr
	event, _ := payload["event"].(string)
	if event == "" {
		event = "unknown"
	}
	dataJSON, _ := json.Marshal(payload["data"])
	dbExecCtx(r.Context(), "INSERT INTO metrics_client (ts, event, data, ua, ip) VALUES (?,?,?,?,?)",
		time.Now().UTC().Format(time.RFC3339)+"Z", event, string(dataJSON), ua, ip)
	writeJSON(w, 200, M{"ok": true})
}

func handleRecentClientMetrics(w http.ResponseWriter, r *http.Request) {
	limit := queryParamInt(r, "limit", 50)
	rows, _ := dbQueryCtx(r.Context(), "SELECT * FROM metrics_client ORDER BY id DESC LIMIT ?", limit)
	writeJSON(w, 200, scanRows(rows))
}

// ── Audit ──

func handleAuditTrail(w http.ResponseWriter, r *http.Request) {
	et := r.URL.Query().Get("entity_type")
	eid := r.URL.Query().Get("entity_id")
	action := r.URL.Query().Get("action")
	limit := queryParamInt(r, "limit", 50)
	offset := queryParamInt(r, "offset", 0)

	q := "SELECT a.*, u.username, u.full_name FROM audit_log a LEFT JOIN users u ON u.id=a.user_id WHERE 1=1"
	var params []interface{}
	if et != "" {
		q += " AND a.entity_type=?"
		params = append(params, et)
	}
	if eid != "" {
		q += " AND a.entity_id=?"
		params = append(params, eid)
	}
	if action != "" {
		q += " AND a.action=?"
		params = append(params, action)
	}
	countQ := strings.Replace(q, "SELECT a.*, u.username, u.full_name", "SELECT COUNT(*) as total", 1)
	var total int
	dbQueryRowCtx(r.Context(), countQ, params...).Scan(&total)

	q += " ORDER BY a.timestamp DESC LIMIT ? OFFSET ?"
	params = append(params, limit, offset)
	rows, _ := dbQueryCtx(r.Context(), q, params...)
	writeJSON(w, 200, M{"total": total, "entries": scanRows(rows)})
}

func handleVerifyResult(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	rows, _ := dbQueryCtx(r.Context(), "SELECT * FROM audit_log WHERE entity_type='result' AND entity_id=? ORDER BY timestamp ASC", id)
	entries := scanRows(rows)

	chainValid := true
	for i := 1; i < len(entries); i++ {
		prev, _ := entries[i]["prev_block_hash"].(string)
		hash, _ := entries[i-1]["block_hash"].(string)
		if prev != hash {
			chainValid = false
			break
		}
	}

	resRow, err := querySingleRow("SELECT * FROM results WHERE id=?", id)
	var dualLedger interface{}
	if err == nil {
		dualLedger = M{
			"tigerbeetle_status":      resRow["tigerbeetle_status"],
			"hyperledger_status":      resRow["hyperledger_status"],
			"tigerbeetle_transfer_id": resRow["tigerbeetle_transfer_id"],
			"hyperledger_tx_id":       resRow["hyperledger_tx_id"],
		}
	}

	writeJSON(w, 200, M{
		"result_id": id, "audit_entries": entries, "chain_valid": chainValid,
		"result_status": resRow, "dual_ledger": dualLedger,
	})
}

func handleAuditStats(w http.ResponseWriter, r *http.Request) {
	rows, _ := dbQueryCtx(r.Context(), "SELECT action, COUNT(*) as count FROM audit_log GROUP BY action ORDER BY count DESC")
	actionCounts := scanRows(rows)
	var total int
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM audit_log").Scan(&total)
	var latestHash sql.NullString
	dbQueryRowCtx(r.Context(), "SELECT block_hash FROM audit_log ORDER BY id DESC LIMIT 1").Scan(&latestHash)
	writeJSON(w, 200, M{"total_entries": total, "action_counts": actionCounts, "latest_block_hash": nullStr(latestHash)})
}

// ── Incidents ──

func handleCreateIncident(w http.ResponseWriter, r *http.Request) {
	user, err := getCurrentUser(r)
	if err != nil {
		writeError(w, 401, "Not authenticated")
		return
	}
	var req IncidentReport
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Severity == "" {
		req.Severity = "medium"
	}
	userSub, _ := user["sub"].(string)
	uid, _ := strconv.Atoi(userSub)

	tx, txErr := db.Begin()
	if txErr != nil {
		writeError(w, 500, "transaction start failed")
		return
	}
	res, insErr := tx.Exec("INSERT INTO incidents (election_id, polling_unit_code, reported_by, incident_type, description, severity) VALUES (?,?,?,?,?,?)",
		req.ElectionID, req.PollingUnitCode, uid, req.IncidentType, req.Description, req.Severity)
	if insErr != nil {
		tx.Rollback()
		writeError(w, 500, "failed to create incident")
		return
	}
	lid, _ := res.LastInsertId()
	if commitErr := tx.Commit(); commitErr != nil {
		writeError(w, 500, "transaction commit failed")
		return
	}
	auditWrite("INCIDENT_CREATED", "incident", fmt.Sprintf("%d", lid), r, map[string]interface{}{"type": req.IncidentType, "severity": req.Severity})

	go func() {
		ctx := context.Background()
		if err := mwHub.Kafka.Produce(ctx, KafkaMessage{
			Topic: TopicIncidentReport,
			Key:   fmt.Sprintf("incident-%d", lid),
			Value: map[string]interface{}{"id": lid, "election_id": req.ElectionID, "type": req.IncidentType, "severity": req.Severity},
		}); err != nil {
			log.Error().Err(err).Str("topic", TopicIncidentReport).Msg("Kafka produce failed")
		} else {
			log.Info().Str("topic", TopicIncidentReport).Int64("id", lid).Msg("Kafka produce success")
		}
	}()

	writeJSON(w, 200, M{"id": lid, "message": "Incident reported"})
}

func handleListIncidents(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	status := r.URL.Query().Get("status")
	severity := r.URL.Query().Get("severity")
	limit := queryParamInt(r, "limit", 50)
	offset := queryParamInt(r, "offset", 0)

	q := "SELECT i.*, u.full_name as reporter_name FROM incidents i LEFT JOIN users u ON u.id=i.reported_by WHERE i.election_id=?"
	params := []interface{}{eid}
	if status != "" {
		q += " AND i.status=?"
		params = append(params, status)
	}
	if severity != "" {
		q += " AND i.severity=?"
		params = append(params, severity)
	}
	q += " ORDER BY i.reported_at DESC LIMIT ? OFFSET ?"
	params = append(params, limit, offset)
	rows, _ := dbQueryCtx(r.Context(), q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleUpdateIncident(w http.ResponseWriter, r *http.Request) {
	if _, err := getCurrentUser(r); err != nil {
		writeError(w, 401, "Not authenticated")
		return
	}
	id := mux.Vars(r)["id"]
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Status == "" {
		req.Status = r.URL.Query().Get("status")
	}
	if req.Status == "" {
		writeError(w, 400, "status is required")
		return
	}
	resolved := ""
	if req.Status == "resolved" {
		resolved = ", resolved_at=CURRENT_TIMESTAMP"
	}
	dbExecCtx(r.Context(), "UPDATE incidents SET status=?"+resolved+" WHERE id=?", req.Status, id)
	auditWrite("INCIDENT_UPDATED", "incident", id, r, map[string]interface{}{"status": req.Status})
	writeJSON(w, 200, M{"message": "Incident updated"})
}

// ── Parties ──

func handleListParties(w http.ResponseWriter, r *http.Request) {
	rows, _ := dbQueryCtx(r.Context(), "SELECT * FROM parties WHERE is_active=1 ORDER BY name")
	writeJSON(w, 200, scanRows(rows))
}

// ── Helpers ──

func nullStr(ns sql.NullString) interface{} {
	if ns.Valid {
		return ns.String
	}
	return nil
}

func nullInt(ni sql.NullInt64) int64 {
	if ni.Valid {
		return ni.Int64
	}
	return 0
}

func scanRows(rows *sql.Rows) []M {
	if rows == nil {
		return []M{}
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return []M{}
	}
	var result []M
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := make(M, len(cols))
		for i, col := range cols {
			v := vals[i]
			switch vv := v.(type) {
			case []byte:
				row[col] = string(vv)
			default:
				row[col] = vv
			}
		}
		result = append(result, row)
	}
	if result == nil {
		return []M{}
	}
	return result
}
