package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func initSMSUSSDTables(database *sql.DB) {
	database.Exec(`CREATE TABLE IF NOT EXISTS sms_verifications (
		id SERIAL PRIMARY KEY,
		phone TEXT NOT NULL,
		polling_unit_code TEXT,
		election_id INTEGER,
		request_type TEXT NOT NULL CHECK(request_type IN ('result','status','verify')),
		response_text TEXT,
		channel TEXT NOT NULL DEFAULT 'sms' CHECK(channel IN ('sms','ussd')),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	database.Exec(`CREATE TABLE IF NOT EXISTS ussd_sessions (
		id TEXT PRIMARY KEY,
		phone TEXT NOT NULL,
		stage TEXT NOT NULL DEFAULT 'main_menu',
		data TEXT DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	database.Exec(`CREATE INDEX IF NOT EXISTS idx_sms_phone ON sms_verifications(phone)`)
}

type SMSRequest struct {
	Phone           string `json:"phone"`
	Message         string `json:"message"`
	PollingUnitCode string `json:"polling_unit_code,omitempty"`
	ElectionID      int    `json:"election_id,omitempty"`
}

type USSDRequest struct {
	SessionID   string `json:"session_id"`
	PhoneNumber string `json:"phone_number"`
	Text        string `json:"text"`
	ServiceCode string `json:"service_code"`
}

func handleSMSVerify(w http.ResponseWriter, r *http.Request) {
	var req SMSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}

	if req.Phone == "" {
		writeError(w, 400, "phone is required")
		return
	}

	msg := strings.TrimSpace(strings.ToUpper(req.Message))
	electionID := req.ElectionID
	if electionID == 0 {
		electionID = 1
	}

	var response string
	var reqType string

	switch {
	case strings.HasPrefix(msg, "RESULT "):
		puCode := strings.TrimPrefix(msg, "RESULT ")
		reqType = "result"
		response = getSMSResult(puCode, electionID)
	case strings.HasPrefix(msg, "VERIFY "):
		puCode := strings.TrimPrefix(msg, "VERIFY ")
		reqType = "verify"
		response = getSMSVerify(puCode, electionID)
	case msg == "STATUS" || msg == "HELP":
		reqType = "status"
		response = getSMSStatus(electionID)
	default:
		reqType = "status"
		response = "INEC Result Verification\nSend:\nRESULT <PU-CODE> - Get results\nVERIFY <PU-CODE> - Verify result\nSTATUS - Election status\nExample: RESULT AB-001-W001-PU001"
	}

	dbExecLog("db_op", `INSERT INTO sms_verifications (phone, polling_unit_code, election_id, request_type, response_text, channel)
		VALUES (?,?,?,?,?,'sms')`, req.Phone, req.PollingUnitCode, electionID, reqType, response)

	writeJSON(w, 200, M{"response": response, "phone": req.Phone, "channel": "sms"})
}

func getSMSResult(puCode string, electionID int) string {
	puCode = strings.TrimSpace(puCode)
	var puName string
	err := db.QueryRow("SELECT name FROM polling_units WHERE code=?", puCode).Scan(&puName)
	if err != nil {
		return fmt.Sprintf("Polling unit %s not found. Check code and try again.", puCode)
	}

	rows, err := db.Query(`SELECT p.abbreviation, rv.votes FROM result_votes rv
		JOIN results res ON rv.result_id=res.id
		JOIN parties p ON rv.party_id=p.id
		WHERE res.polling_unit_code=? AND res.election_id=?
		ORDER BY rv.votes DESC`, puCode, electionID)
	if err != nil {
		return fmt.Sprintf("%s: No results submitted yet.", puName)
	}
	defer rows.Close()

	var lines []string
	lines = append(lines, fmt.Sprintf("RESULTS: %s (%s)", puName, puCode))
	total := 0
	for rows.Next() {
		var party string
		var votes int
		rows.Scan(&party, &votes)
		lines = append(lines, fmt.Sprintf("%s: %d", party, votes))
		total += votes
	}
	if len(lines) == 1 {
		return fmt.Sprintf("%s: No results submitted yet.", puName)
	}
	lines = append(lines, fmt.Sprintf("TOTAL: %d votes", total))
	return strings.Join(lines, "\n")
}

func getSMSVerify(puCode string, electionID int) string {
	puCode = strings.TrimSpace(puCode)
	var puName, status, tbStatus, hlStatus string
	err := db.QueryRow(`SELECT pu.name, r.status, r.tigerbeetle_status, r.hyperledger_status
		FROM results r JOIN polling_units pu ON r.polling_unit_code=pu.code
		WHERE r.polling_unit_code=? AND r.election_id=?`, puCode, electionID).Scan(&puName, &status, &tbStatus, &hlStatus)
	if err != nil {
		return fmt.Sprintf("No result to verify for %s", puCode)
	}

	verified := "NOT VERIFIED"
	if tbStatus == "POSTED" && hlStatus == "CONFIRMED" {
		verified = "VERIFIED (Dual-Ledger Confirmed)"
	} else if tbStatus == "POSTED" {
		verified = "PARTIAL (TigerBeetle only)"
	}

	return fmt.Sprintf("VERIFY: %s\nStatus: %s\nTigerBeetle: %s\nHyperledger: %s\nResult: %s", puName, status, tbStatus, hlStatus, verified)
}

func getSMSStatus(electionID int) string {
	var name, status string
	var totalPUs int
	db.QueryRow("SELECT name, status FROM elections WHERE id=?", electionID).Scan(&name, &status)
	db.QueryRow("SELECT COUNT(*) FROM polling_units").Scan(&totalPUs)

	var submitted, finalized int
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=?", electionID).Scan(&submitted)
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=? AND status='finalized'", electionID).Scan(&finalized)

	pct := 0.0
	if totalPUs > 0 {
		pct = float64(submitted) / float64(totalPUs) * 100
	}

	return fmt.Sprintf("ELECTION: %s\nStatus: %s\nResults: %d/%d (%.1f%%)\nFinalized: %d\nSend RESULT <PU-CODE> for details", name, status, submitted, totalPUs, pct, finalized)
}

func handleUSSDGateway(w http.ResponseWriter, r *http.Request) {
	var req USSDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("USSD-%d", time.Now().UnixNano())
	}

	text := strings.TrimSpace(req.Text)
	parts := strings.Split(text, "*")

	var response string
	continueSession := true

	if text == "" {
		response = "CON Welcome to INEC Result Verification\n1. Check Result by PU Code\n2. Election Status\n3. Verify Result\n0. Exit"
		dbExecLog("db_op", `INSERT INTO ussd_sessions (id, phone, stage, data) VALUES (?,?,'main_menu','{}')`,
			sessionID, req.PhoneNumber)
	} else if len(parts) == 1 {
		switch parts[0] {
		case "1":
			response = "CON Enter Polling Unit Code\n(e.g. AB-001-W001-PU001):"
		case "2":
			response = "END " + getSMSStatus(1)
			continueSession = false
		case "3":
			response = "CON Enter PU Code to verify:"
		case "0":
			response = "END Thank you for using INEC Verification."
			continueSession = false
		default:
			response = "END Invalid option. Dial again."
			continueSession = false
		}
	} else if len(parts) == 2 {
		switch parts[0] {
		case "1":
			response = "END " + getSMSResult(parts[1], 1)
			continueSession = false
		case "3":
			response = "END " + getSMSVerify(parts[1], 1)
			continueSession = false
		default:
			response = "END Invalid input."
			continueSession = false
		}
	} else {
		response = "END Invalid input. Please dial again."
		continueSession = false
	}

	dbExecLog("db_op", `INSERT INTO sms_verifications (phone, request_type, response_text, channel)
		VALUES (?,?,?,'ussd')`, req.PhoneNumber, "ussd", response)

	writeJSON(w, 200, M{
		"response":         response,
		"session_id":       sessionID,
		"continue_session": continueSession,
	})
}

func handleSMSStats(w http.ResponseWriter, r *http.Request) {
	var totalSMS, totalUSSD int
	db.QueryRow("SELECT COUNT(*) FROM sms_verifications WHERE channel='sms'").Scan(&totalSMS)
	db.QueryRow("SELECT COUNT(*) FROM sms_verifications WHERE channel='ussd'").Scan(&totalUSSD)

	var today int
	db.QueryRow("SELECT COUNT(*) FROM sms_verifications WHERE created_at >= CURRENT_DATE").Scan(&today)

	rows, _ := db.Query(`SELECT request_type, COUNT(*) as cnt FROM sms_verifications
		GROUP BY request_type ORDER BY cnt DESC`)
	defer rows.Close()
	byType := []M{}
	for rows.Next() {
		var rt string
		var cnt int
		rows.Scan(&rt, &cnt)
		byType = append(byType, M{"type": rt, "count": cnt})
	}

	writeJSON(w, 200, M{
		"total_sms":  totalSMS,
		"total_ussd": totalUSSD,
		"today":      today,
		"by_type":    byType,
	})
}
