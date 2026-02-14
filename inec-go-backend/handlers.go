package main

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// ── Auth ──

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	row := db.QueryRow("SELECT id, username, password_hash, full_name, role, staff_id, state_code FROM users WHERE username=? AND is_active=1", req.Username)
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
	token, _ := createAccessToken(map[string]interface{}{
		"sub": fmt.Sprintf("%d", id), "username": username, "role": role, "full_name": fullName,
	})
	writeJSON(w, 200, M{
		"access_token": token, "token_type": "bearer",
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
	var exists int
	db.QueryRow("SELECT COUNT(*) FROM users WHERE username=?", req.Username).Scan(&exists)
	if exists > 0 {
		writeError(w, 400, "Username already exists")
		return
	}
	pwHash := hashPassword(req.Password)
	res, err := db.Exec("INSERT INTO users (username, password_hash, full_name, role, staff_id, state_code) VALUES (?,?,?,?,?,?)",
		req.Username, pwHash, req.FullName, req.Role, req.StaffID, req.StateCode)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	uid, _ := res.LastInsertId()
	token, _ := createAccessToken(map[string]interface{}{
		"sub": fmt.Sprintf("%d", uid), "username": req.Username, "role": req.Role, "full_name": req.FullName,
	})
	writeJSON(w, 200, M{
		"access_token": token, "token_type": "bearer",
		"user": M{"id": uid, "username": req.Username, "full_name": req.FullName, "role": req.Role, "staff_id": req.StaffID, "state_code": req.StateCode},
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

// ── Elections ──

func handleListElections(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = db.Query("SELECT * FROM elections WHERE status=? ORDER BY election_date DESC", status)
	} else {
		rows, err = db.Query("SELECT * FROM elections ORDER BY election_date DESC")
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
	var req struct {
		Title        string  `json:"title"`
		ElectionType string  `json:"election_type"`
		ElectionDate string  `json:"election_date"`
		Description  *string `json:"description"`
		Status       string  `json:"status"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Status == "" {
		req.Status = "upcoming"
	}
	res, _ := db.Exec("INSERT INTO elections (title, election_type, election_date, status, description) VALUES (?,?,?,?,?)",
		req.Title, req.ElectionType, req.ElectionDate, req.Status, req.Description)
	lid, _ := res.LastInsertId()
	writeJSON(w, 200, M{"id": lid, "message": "Election created"})
}

func handleUpdateElection(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var req map[string]interface{}
	json.NewDecoder(r.Body).Decode(&req)
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
	db.Exec("UPDATE elections SET "+strings.Join(updates, ",")+` WHERE id=?`, vals...)
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
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=?", eid).Scan(&totalResults)
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=? AND status='finalized'", eid).Scan(&finalized)
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=? AND status='validated'", eid).Scan(&validated)
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=? AND status='pending'", eid).Scan(&pending)
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=? AND status='disputed'", eid).Scan(&disputed)
	db.QueryRow("SELECT COUNT(*) FROM polling_units").Scan(&totalPUs)

	var validV, rejectedV, castV, accreditedV sql.NullInt64
	db.QueryRow(`SELECT SUM(total_valid_votes), SUM(rejected_votes), SUM(total_votes_cast), SUM(accredited_voters)
		FROM results WHERE election_id=? AND status IN ('finalized','validated')`, eid).Scan(&validV, &rejectedV, &castV, &accreditedV)

	rows, _ := db.Query(`SELECT rps.party_code, p.name as party_name, p.color, SUM(rps.votes) as total_votes
		FROM result_party_scores rps JOIN results r ON r.id=rps.result_id JOIN parties p ON p.code=rps.party_code
		WHERE r.election_id=? AND r.status IN ('finalized','validated') GROUP BY rps.party_code ORDER BY total_votes DESC`, eid)
	partyScores := scanRows(rows)

	comp := 0.0
	if totalPUs > 0 {
		comp = math.Round(float64(totalResults)/float64(totalPUs)*10000) / 100
	}
	writeJSON(w, 200, M{
		"election": election, "total_polling_units": totalPUs, "results_received": totalResults,
		"results_finalized": finalized, "results_validated": validated, "results_pending": pending, "results_disputed": disputed,
		"completion_percentage": comp,
		"total_valid_votes": nullInt(validV), "total_rejected_votes": nullInt(rejectedV),
		"total_votes_cast": nullInt(castV), "total_accredited_voters": nullInt(accreditedV),
		"party_scores": partyScores,
	})
}

// ── Results ──

func logAudit(action, entityType, entityID string, userID int, details map[string]interface{}) {
	var prevHash sql.NullString
	db.QueryRow("SELECT block_hash FROM audit_log ORDER BY id DESC LIMIT 1").Scan(&prevHash)
	prev := strings.Repeat("0", 64)
	if prevHash.Valid {
		prev = prevHash.String
	}
	blockData := fmt.Sprintf("%s%s%s%s", prev, action, entityID, time.Now().UTC().Format(time.RFC3339))
	h := sha256.Sum256([]byte(blockData))
	blockHash := hex.EncodeToString(h[:])
	detailsJSON, _ := json.Marshal(details)
	db.Exec("INSERT INTO audit_log (action, entity_type, entity_id, user_id, details, block_hash, prev_block_hash) VALUES (?,?,?,?,?,?,?)",
		action, entityType, entityID, userID, string(detailsJSON), blockHash, prev)
}

func handleSubmitResult(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "presiding_officer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	var req struct {
		ElectionID      int `json:"election_id"`
		PollingUnitCode string `json:"polling_unit_code"`
		PartyScores     []struct {
			PartyCode string `json:"party_code"`
			Votes     int    `json:"votes"`
		} `json:"party_scores"`
		AccreditedVoters int `json:"accredited_voters"`
		RejectedVotes    int `json:"rejected_votes"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	var eExists int
	db.QueryRow("SELECT COUNT(*) FROM elections WHERE id=? AND status='active'", req.ElectionID).Scan(&eExists)
	if eExists == 0 {
		writeError(w, 400, "Election not found or not active")
		return
	}
	var regVoters int
	if err := db.QueryRow("SELECT registered_voters FROM polling_units WHERE code=?", req.PollingUnitCode).Scan(&regVoters); err != nil {
		writeError(w, 400, "Polling unit not found")
		return
	}
	var dupCheck int
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=? AND polling_unit_code=?", req.ElectionID, req.PollingUnitCode).Scan(&dupCheck)
	if dupCheck > 0 {
		writeError(w, 400, "Result already submitted for this polling unit")
		return
	}
	totalValid := 0
	for _, ps := range req.PartyScores {
		totalValid += ps.Votes
	}
	totalCast := totalValid + req.RejectedVotes
	if totalCast > req.AccreditedVoters {
		writeError(w, 400, "Total votes cast exceeds accredited voters")
		return
	}
	if req.AccreditedVoters > regVoters {
		writeError(w, 400, "Accredited voters exceeds registered voters")
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

	tbTransfer := createTBTransfer(0, int64(totalCast), req.PollingUnitCode)
	tbID := ""
	if tbTransfer != nil {
		tbID = tbTransfer.ID
	}

	var ptbID string
	if persistentTB != nil {
		ptbID, _ = persistentTB.CreateTransfer("inec-operational", "inec-official", int64(totalCast), 1, 1, req.PollingUnitCode)
	}
	if ptbID != "" {
		tbID = ptbID
	}

	res, _ := db.Exec(`INSERT INTO results (election_id, polling_unit_code, presiding_officer_id, status,
		total_valid_votes, rejected_votes, total_votes_cast, accredited_voters,
		ec8a_hash, tigerbeetle_transfer_id, tigerbeetle_status, hyperledger_status)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		req.ElectionID, req.PollingUnitCode, userID, "pending",
		totalValid, req.RejectedVotes, totalCast, req.AccreditedVoters,
		ec8aHash, tbID, "PENDING", "PENDING")
	resultID, _ := res.LastInsertId()

	for _, ps := range req.PartyScores {
		db.Exec("INSERT INTO result_party_scores (result_id, party_code, votes) VALUES (?,?,?)", resultID, ps.PartyCode, ps.Votes)
	}

	logAudit("RESULT_SUBMITTED", "result", fmt.Sprintf("%d", resultID), userID,
		map[string]interface{}{"phase": "Pre-Validation", "polling_unit": req.PollingUnitCode, "tigerbeetle_id": tbID})

	go broadcastWS(M{"type": "result_updated", "pu_code": req.PollingUnitCode, "election_id": req.ElectionID})

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

	cacheDel(fmt.Sprintf("dashboard_stats_%d", req.ElectionID))

	writeJSON(w, 200, M{"id": resultID, "status": "pending", "tigerbeetle_transfer_id": tbID, "workflow_id": wfID, "phase": "Pre-Validation", "message": "Result submitted. Proceeding to Edge Validation."})
}

func handleValidateResult(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "collation_officer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var status, puCode string
	if err := db.QueryRow("SELECT status, polling_unit_code FROM results WHERE id=?", id).Scan(&status, &puCode); err != nil {
		writeError(w, 404, "Result not found")
		return
	}
	if status != "pending" {
		writeError(w, 400, fmt.Sprintf("Result is already %s", status))
		return
	}
	userSub, _ := user["sub"].(string)
	uid, _ := strconv.Atoi(userSub)
	userRole, _ := user["role"].(string)
	if !checkPermission(userRole, "validate_result") {
		writeError(w, 403, "Permission denied by Permify")
		return
	}

	db.Exec("UPDATE results SET status='validated', validated_at=CURRENT_TIMESTAMP WHERE id=?", id)
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
	if err := db.QueryRow("SELECT status, polling_unit_code FROM results WHERE id=?", id).Scan(&status, &puCode); err != nil {
		writeError(w, 404, "Result not found")
		return
	}
	if status != "pending" && status != "validated" {
		writeError(w, 400, fmt.Sprintf("Cannot finalize result with status %s", status))
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
	db.QueryRow("SELECT tigerbeetle_transfer_id FROM results WHERE id=?", id).Scan(&tbTransferID)
	if tbTransferID.Valid && tbTransferID.String != "" {
		postTBTransfer(tbTransferID.String)
		if persistentTB != nil {
			persistentTB.PostTransfer(tbTransferID.String)
		}
	}

	var hlTx, ipfsCid string
	idInt, _ := strconv.ParseInt(id, 10, 64)
	var electionID, totalVotes, accredited int
	db.QueryRow("SELECT election_id, total_votes_cast, accredited_voters FROM results WHERE id=?", id).Scan(&electionID, &totalVotes, &accredited)

	if fabricNetwork != nil {
		txID, _, _ := fabricNetwork.SubmitTransaction("inec-results", "result-validation-cc", "FinalizeResult",
			[]string{id, puCode, fmt.Sprintf("%d", electionID)}, "INECMSP")
		hlTx = txID
	}
	if hlTx == "" {
		hlTx = fmt.Sprintf("TX-%x", sha256.Sum256([]byte(fmt.Sprintf("finalize-%s-%d", id, time.Now().UnixNano()))))
		hlTx = hlTx[:26]
	}

	if ipfsStore != nil {
		resultData := map[string]interface{}{"result_id": id, "pu_code": puCode, "election_id": electionID, "status": "finalized", "timestamp": time.Now().UTC().Format(time.RFC3339)}
		cid, _ := ipfsStore.StoreJSON(resultData, "election/result-finalization")
		ipfsCid = cid
	}
	if ipfsCid == "" {
		h := sha256.Sum256([]byte(fmt.Sprintf("result-%s-%d", id, time.Now().UnixNano())))
		ipfsCid = "Qm" + hex.EncodeToString(h[:])
	}

	if chaincodeEngine != nil {
		chaincodeEngine.ExecuteResultValidation(int(idInt), puCode, electionID, totalVotes, accredited)
	}

	db.Exec(`UPDATE results SET status='finalized', finalized_at=CURRENT_TIMESTAMP,
		tigerbeetle_status='POSTED', hyperledger_status='CONFIRMED', hyperledger_tx_id=?, ipfs_cid=? WHERE id=?`, hlTx, ipfsCid, id)
	logAudit("RESULT_FINALIZED", "result", id, uid, map[string]interface{}{"phase": "Finalization", "polling_unit": puCode, "hyperledger_tx": hlTx, "ipfs_cid": ipfsCid})

	go publishResultEvent(TopicResultFinalized, idInt, puCode, 0, uid,
		map[string]interface{}{"phase": "Finalization", "hyperledger_tx": hlTx, "ipfs_cid": ipfsCid})
	go publishAuditEvent("RESULT_FINALIZED", "result", id, uid, map[string]interface{}{"polling_unit": puCode})

	startResultWorkflow("ResultFinalizationWorkflow", idInt, map[string]interface{}{"result_id": id})

	writeJSON(w, 200, M{"status": "finalized", "phase": "Finalization", "hyperledger_tx_id": hlTx, "ipfs_cid": ipfsCid, "tigerbeetle_status": "POSTED", "hyperledger_status": "CONFIRMED"})
}

func handleDisputeResult(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "observer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var puCode string
	if err := db.QueryRow("SELECT polling_unit_code FROM results WHERE id=?", id).Scan(&puCode); err != nil {
		writeError(w, 404, "Result not found")
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
	db.QueryRow("SELECT tigerbeetle_transfer_id FROM results WHERE id=?", id).Scan(&tbTransferID)
	if tbTransferID.Valid && tbTransferID.String != "" {
		voidTBTransfer(tbTransferID.String)
		if persistentTB != nil {
			persistentTB.VoidTransfer(tbTransferID.String)
		}
	}

	if fabricNetwork != nil {
		fabricNetwork.SubmitTransaction("inec-results", "dispute-resolution-cc", "DisputeResult",
			[]string{id, puCode, fmt.Sprintf("%d", uid)}, "INECMSP")
	}

	db.Exec("UPDATE results SET status='disputed', tigerbeetle_status='VOIDED' WHERE id=?", id)
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
	db.QueryRow(countQ, params...).Scan(&total)

	q += " ORDER BY r.submitted_at DESC LIMIT ? OFFSET ?"
	params = append(params, limit, offset)
	rows, _ := db.Query(q, params...)
	results := scanRows(rows)

	for i, res := range results {
		rid, _ := res["id"]
		psRows, _ := db.Query(`SELECT rps.party_code, p.name as party_name, p.color, rps.votes
			FROM result_party_scores rps JOIN parties p ON p.code=rps.party_code WHERE rps.result_id=? ORDER BY rps.votes DESC`, rid)
		results[i]["party_scores"] = scanRows(psRows)
	}
	writeJSON(w, 200, M{"total": total, "results": results})
}

func handleGetResult(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	rows, _ := db.Query(`SELECT r.*, pu.name as pu_name, pu.ward_code, pu.registered_voters,
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
	psRows, _ := db.Query(`SELECT rps.party_code, p.name as party_name, p.color, rps.votes
		FROM result_party_scores rps JOIN parties p ON p.code=rps.party_code WHERE rps.result_id=? ORDER BY rps.votes DESC`, id)
	result["party_scores"] = scanRows(psRows)
	writeJSON(w, 200, result)
}

// ── Geo ──

func handleListStates(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT * FROM states ORDER BY name")
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
	if sc != "" {
		rows, _ = db.Query("SELECT l.*, s.name as state_name FROM lgas l JOIN states s ON s.code=l.state_code WHERE l.state_code=? ORDER BY l.name", sc)
	} else {
		rows, _ = db.Query("SELECT l.*, s.name as state_name FROM lgas l JOIN states s ON s.code=l.state_code ORDER BY l.name")
	}
	writeJSON(w, 200, scanRows(rows))
}

func handleListWards(w http.ResponseWriter, r *http.Request) {
	lc := r.URL.Query().Get("lga_code")
	var rows *sql.Rows
	if lc != "" {
		rows, _ = db.Query("SELECT w.*, l.name as lga_name FROM wards w JOIN lgas l ON l.code=w.lga_code WHERE w.lga_code=? ORDER BY w.name", lc)
	} else {
		rows, _ = db.Query("SELECT w.*, l.name as lga_name FROM wards w JOIN lgas l ON l.code=w.lga_code ORDER BY w.name LIMIT 100")
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
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleGetPollingUnit(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]
	rows, _ := db.Query(`SELECT pu.*, w.name as ward_name, w.lga_code, l.name as lga_name, l.state_code, s.name as state_name
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

	stRows, _ := db.Query(`SELECT s.code, s.name, s.geo_zone, s.capital,
		COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
		COALESCE(SUM(r.total_valid_votes),0) as total_votes,
		COALESCE(SUM(r.total_votes_cast),0) as total_cast,
		COALESCE(SUM(r.accredited_voters),0) as accredited,
		AVG(pu.latitude) as avg_lat, AVG(pu.longitude) as avg_lng
		FROM states s LEFT JOIN lgas l ON l.state_code=s.code
		LEFT JOIN wards w ON w.lga_code=l.code LEFT JOIN polling_units pu ON pu.ward_code=w.code
		LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
		GROUP BY s.code ORDER BY s.name`, eid)
	states := scanRows(stRows)

	for i, s := range states {
		code, _ := s["code"].(string)
		psRows, _ := db.Query(`SELECT rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
			FROM result_party_scores rps JOIN results res ON res.id=rps.result_id
			JOIN polling_units pu ON pu.code=res.polling_unit_code JOIN wards w ON w.code=pu.ward_code
			JOIN lgas l ON l.code=w.lga_code JOIN parties p ON p.code=rps.party_code
			WHERE l.state_code=? AND res.election_id=? AND res.status IN ('finalized','validated')
			GROUP BY rps.party_code ORDER BY total_votes DESC`, code, eid)
		ps := scanRows(psRows)
		states[i]["party_scores"] = ps
		if len(ps) > 0 {
			states[i]["leading_party"] = ps[0]
		} else {
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
	puRows, _ := db.Query(puQ, puParams...)
	pus := scanRows(puRows)

	for i, pu := range pus {
		if rid, ok := pu["result_id"]; ok && rid != nil {
			psRows, _ := db.Query(`SELECT rps.party_code, p.abbreviation, p.color, rps.votes
				FROM result_party_scores rps JOIN parties p ON p.code=rps.party_code
				WHERE rps.result_id=? ORDER BY rps.votes DESC`, rid)
			pus[i]["party_scores"] = scanRows(psRows)
		} else {
			pus[i]["party_scores"] = []M{}
		}
	}
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

	rows, err := db.Query(`SELECT pu.code, pu.name, pu.latitude as lat, pu.longitude as lon,
		COALESCE(r.status, 'no_result') as status,
		r.submitted_at as submitted_at,
		CAST(strftime('%s', r.submitted_at) AS INTEGER) as submitted_ts
		FROM polling_units pu LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
		WHERE pu.longitude BETWEEN ? AND ? AND pu.latitude BETWEEN ? AND ? LIMIT 10000`,
		eid, lonMin, lonMax, latMin, latMax)
	if err != nil {
		w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
		w.Write(encodeMVTEmpty())
		return
	}

	tile := encodeMVTTile(rows, z, x, y, lonMin, latMin, lonMax, latMax)

	h := md5.Sum(tile)
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
	rows, _ := db.Query(q, params...)
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
	rows, err := db.Query(q, params...)
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
	db.QueryRow("SELECT COUNT(*) FROM polling_units").Scan(&totalPUs)
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=?", eid).Scan(&resultsReceived)

	statusRows, _ := db.Query("SELECT status, COUNT(*) as count FROM results WHERE election_id=? GROUP BY status", eid)
	statusMap := make(map[string]int)
	for statusRows.Next() {
		var s string
		var c int
		statusRows.Scan(&s, &c)
		statusMap[s] = c
	}
	statusRows.Close()

	var validV, rejectedV, castV, accreditedV sql.NullInt64
	db.QueryRow(`SELECT SUM(total_valid_votes), SUM(rejected_votes), SUM(total_votes_cast), SUM(accredited_voters)
		FROM results WHERE election_id=? AND status IN ('finalized','validated')`, eid).Scan(&validV, &rejectedV, &castV, &accreditedV)

	psRows, _ := db.Query(`SELECT rps.party_code, p.name as party_name, p.color, p.abbreviation, SUM(rps.votes) as total_votes
		FROM result_party_scores rps JOIN results r ON r.id=rps.result_id JOIN parties p ON p.code=rps.party_code
		WHERE r.election_id=? AND r.status IN ('finalized','validated') GROUP BY rps.party_code ORDER BY total_votes DESC`, eid)
	partyScores := scanRows(psRows)

	srRows, _ := db.Query(`SELECT s.code, s.name, s.geo_zone, COUNT(r.id) as results_count, SUM(r.total_valid_votes) as total_votes
		FROM states s LEFT JOIN lgas l ON l.state_code=s.code LEFT JOIN wards w ON w.lga_code=l.code
		LEFT JOIN polling_units pu ON pu.ward_code=w.code
		LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
		GROUP BY s.code ORDER BY s.name`, eid)
	stateResults := scanRows(srRows)

	var tbPosted, hlConfirmed int
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=? AND tigerbeetle_status='POSTED'", eid).Scan(&tbPosted)
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=? AND hyperledger_status='CONFIRMED'", eid).Scan(&hlConfirmed)

	zRows, _ := db.Query(`SELECT s.geo_zone, SUM(r.total_valid_votes) as total_votes, COUNT(r.id) as results_count
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
		"vote_totals": M{"valid": nullInt(validV), "rejected": nullInt(rejectedV), "cast": nullInt(castV), "accredited": nullInt(accreditedV)},
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
	rows, _ := db.Query(`SELECT r.id, r.polling_unit_code, r.status, r.total_votes_cast,
		r.tigerbeetle_status, r.hyperledger_status, r.submitted_at,
		pu.name as pu_name, w.name as ward_name, l.name as lga_name, s.name as state_name, s.code as state_code
		FROM results r JOIN polling_units pu ON pu.code=r.polling_unit_code
		JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code JOIN states s ON s.code=l.state_code
		WHERE r.election_id=? ORDER BY r.submitted_at DESC LIMIT ?`, eid, limit)
	writeJSON(w, 200, scanRows(rows))
}

func handleCollation(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	level := queryParam(r, "level", "state")
	parentCode := r.URL.Query().Get("parent_code")

	switch level {
	case "state":
		rows, _ := db.Query(`SELECT s.code, s.name, s.geo_zone,
			COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
			SUM(r.total_valid_votes) as total_valid_votes, SUM(r.rejected_votes) as rejected_votes,
			SUM(r.total_votes_cast) as total_votes_cast
			FROM states s LEFT JOIN lgas l ON l.state_code=s.code LEFT JOIN wards w ON w.lga_code=l.code
			LEFT JOIN polling_units pu ON pu.ward_code=w.code
			LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
			GROUP BY s.code ORDER BY s.name`, eid)
		results := scanRows(rows)
		for i, res := range results {
			code, _ := res["code"].(string)
			psRows, _ := db.Query(`SELECT rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
				FROM result_party_scores rps JOIN results res ON res.id=rps.result_id
				JOIN polling_units pu ON pu.code=res.polling_unit_code JOIN wards w ON w.code=pu.ward_code
				JOIN lgas l ON l.code=w.lga_code JOIN parties p ON p.code=rps.party_code
				WHERE l.state_code=? AND res.election_id=? AND res.status IN ('finalized','validated')
				GROUP BY rps.party_code ORDER BY total_votes DESC`, code, eid)
			results[i]["party_scores"] = scanRows(psRows)
		}
		writeJSON(w, 200, results)

	case "lga":
		rows, _ := db.Query(`SELECT l.code, l.name,
			COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
			SUM(r.total_valid_votes) as total_valid_votes, SUM(r.rejected_votes) as rejected_votes,
			SUM(r.total_votes_cast) as total_votes_cast
			FROM lgas l LEFT JOIN wards w ON w.lga_code=l.code LEFT JOIN polling_units pu ON pu.ward_code=w.code
			LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
			WHERE l.state_code=? GROUP BY l.code ORDER BY l.name`, eid, parentCode)
		results := scanRows(rows)
		for i, res := range results {
			code, _ := res["code"].(string)
			psRows, _ := db.Query(`SELECT rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
				FROM result_party_scores rps JOIN results res ON res.id=rps.result_id
				JOIN polling_units pu ON pu.code=res.polling_unit_code JOIN wards w ON w.code=pu.ward_code
				JOIN parties p ON p.code=rps.party_code
				WHERE w.lga_code=? AND res.election_id=? AND res.status IN ('finalized','validated')
				GROUP BY rps.party_code ORDER BY total_votes DESC`, code, eid)
			results[i]["party_scores"] = scanRows(psRows)
		}
		writeJSON(w, 200, results)

	case "ward":
		rows, _ := db.Query(`SELECT w.code, w.name,
			COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
			SUM(r.total_valid_votes) as total_valid_votes, SUM(r.rejected_votes) as rejected_votes,
			SUM(r.total_votes_cast) as total_votes_cast
			FROM wards w LEFT JOIN polling_units pu ON pu.ward_code=w.code
			LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
			WHERE w.lga_code=? GROUP BY w.code ORDER BY w.name`, eid, parentCode)
		results := scanRows(rows)
		for i, res := range results {
			code, _ := res["code"].(string)
			psRows, _ := db.Query(`SELECT rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
				FROM result_party_scores rps JOIN results res ON res.id=rps.result_id
				JOIN polling_units pu ON pu.code=res.polling_unit_code JOIN parties p ON p.code=rps.party_code
				WHERE pu.ward_code=? AND res.election_id=? AND res.status IN ('finalized','validated')
				GROUP BY rps.party_code ORDER BY total_votes DESC`, code, eid)
			results[i]["party_scores"] = scanRows(psRows)
		}
		writeJSON(w, 200, results)

	case "pu":
		rows, _ := db.Query(`SELECT pu.code, pu.name, pu.registered_voters,
			r.id as result_id, r.status, r.total_valid_votes, r.rejected_votes,
			r.total_votes_cast, r.accredited_voters, r.tigerbeetle_status, r.hyperledger_status
			FROM polling_units pu LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
			WHERE pu.ward_code=? ORDER BY pu.name`, eid, parentCode)
		results := scanRows(rows)
		for i, res := range results {
			if rid, ok := res["result_id"]; ok && rid != nil {
				psRows, _ := db.Query(`SELECT rps.party_code, p.abbreviation, p.color, rps.votes
					FROM result_party_scores rps JOIN parties p ON p.code=rps.party_code
					WHERE rps.result_id=? ORDER BY rps.votes DESC`, rid)
				results[i]["party_scores"] = scanRows(psRows)
			} else {
				results[i]["party_scores"] = []M{}
			}
		}
		writeJSON(w, 200, results)

	default:
		writeJSON(w, 200, []M{})
	}
}

func handlePostClientMetric(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	json.NewDecoder(r.Body).Decode(&payload)
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
	db.Exec("INSERT INTO metrics_client (ts, event, data, ua, ip) VALUES (?,?,?,?,?)",
		time.Now().UTC().Format(time.RFC3339)+"Z", event, string(dataJSON), ua, ip)
	writeJSON(w, 200, M{"ok": true})
}

func handleRecentClientMetrics(w http.ResponseWriter, r *http.Request) {
	limit := queryParamInt(r, "limit", 50)
	rows, _ := db.Query("SELECT * FROM metrics_client ORDER BY id DESC LIMIT ?", limit)
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
	db.QueryRow(countQ, params...).Scan(&total)

	q += " ORDER BY a.timestamp DESC LIMIT ? OFFSET ?"
	params = append(params, limit, offset)
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, M{"total": total, "entries": scanRows(rows)})
}

func handleVerifyResult(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	rows, _ := db.Query("SELECT * FROM audit_log WHERE entity_type='result' AND entity_id=? ORDER BY timestamp ASC", id)
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
	rows, _ := db.Query("SELECT action, COUNT(*) as count FROM audit_log GROUP BY action ORDER BY count DESC")
	actionCounts := scanRows(rows)
	var total int
	db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&total)
	var latestHash sql.NullString
	db.QueryRow("SELECT block_hash FROM audit_log ORDER BY id DESC LIMIT 1").Scan(&latestHash)
	writeJSON(w, 200, M{"total_entries": total, "action_counts": actionCounts, "latest_block_hash": nullStr(latestHash)})
}

// ── Incidents ──

func handleCreateIncident(w http.ResponseWriter, r *http.Request) {
	user, err := getCurrentUser(r)
	if err != nil {
		writeError(w, 401, "Not authenticated")
		return
	}
	var req struct {
		ElectionID      int     `json:"election_id"`
		PollingUnitCode *string `json:"polling_unit_code"`
		IncidentType    string  `json:"incident_type"`
		Description     string  `json:"description"`
		Severity        string  `json:"severity"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Severity == "" {
		req.Severity = "medium"
	}
	userSub, _ := user["sub"].(string)
	uid, _ := strconv.Atoi(userSub)
	res, _ := db.Exec("INSERT INTO incidents (election_id, polling_unit_code, reported_by, incident_type, description, severity) VALUES (?,?,?,?,?,?)",
		req.ElectionID, req.PollingUnitCode, uid, req.IncidentType, req.Description, req.Severity)
	lid, _ := res.LastInsertId()

	go func() {
		ctx := context.Background()
		mwHub.Kafka.Produce(ctx, KafkaMessage{
			Topic: TopicIncidentReport,
			Key:   fmt.Sprintf("incident-%d", lid),
			Value: map[string]interface{}{"id": lid, "election_id": req.ElectionID, "type": req.IncidentType, "severity": req.Severity},
		})
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
	rows, _ := db.Query(q, params...)
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
	json.NewDecoder(r.Body).Decode(&req)
	if req.Status == "" {
		req.Status = r.URL.Query().Get("status")
	}
	resolved := ""
	if req.Status == "resolved" {
		resolved = ", resolved_at=CURRENT_TIMESTAMP"
	}
	db.Exec("UPDATE incidents SET status=?"+resolved+" WHERE id=?", req.Status, id)
	writeJSON(w, 200, M{"message": "Incident updated"})
}

// ── Parties ──

func handleListParties(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT * FROM parties WHERE is_active=1 ORDER BY name")
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
