package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

func initSMSUSSDTables(database *sql.DB) {
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS sms_verifications (
		id SERIAL PRIMARY KEY,
		phone TEXT NOT NULL,
		polling_unit_code TEXT,
		election_id INTEGER,
		request_type TEXT NOT NULL CHECK(request_type IN ('result','status','verify')),
		response_text TEXT,
		channel TEXT NOT NULL DEFAULT 'sms' CHECK(channel IN ('sms','ussd')),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Warn().Err(err).Msg("sms: failed to create sms_verifications table")
	}
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS ussd_sessions (
		id TEXT PRIMARY KEY,
		phone TEXT NOT NULL,
		stage TEXT NOT NULL DEFAULT 'main_menu',
		data TEXT DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Warn().Err(err).Msg("sms: failed to create ussd_sessions table")
	}
	if _, err := database.Exec(`CREATE INDEX IF NOT EXISTS idx_sms_phone ON sms_verifications(phone)`); err != nil {
		log.Warn().Err(err).Msg("sms: failed to create idx_sms_phone index")
	}
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

// decodeSMSRequest accepts both JSON bodies and the
// application/x-www-form-urlencoded payloads posted by SMS aggregators
// (Africa's Talking MO webhook fields: from/text; Twilio: From/Body).
func decodeSMSRequest(r *http.Request) (SMSRequest, error) {
	var req SMSRequest
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/x-www-form-urlencoded") || strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseForm(); err != nil {
			return req, err
		}
		req.Phone = firstNonEmpty(r.FormValue("from"), r.FormValue("From"), r.FormValue("phone"), r.FormValue("msisdn"))
		req.Message = firstNonEmpty(r.FormValue("text"), r.FormValue("Body"), r.FormValue("message"))
		return req, nil
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	return req, err
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func handleSMSVerify(w http.ResponseWriter, r *http.Request) {
	req, err := decodeSMSRequest(r)
	if err != nil {
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
	return getSMSResultL(puCode, electionID, "en")
}

func getSMSResultL(puCode string, electionID int, lang string) string {
	puCode = strings.TrimSpace(puCode)
	var puName string
	err := db.QueryRow("SELECT name FROM polling_units WHERE code=?", puCode).Scan(&puName)
	if err != nil {
		return fmt.Sprintf(ussdText(lang, "pu_not_found"), puCode)
	}

	rows, err := db.Query(`SELECT p.abbreviation, rv.votes FROM result_votes rv
		JOIN results res ON rv.result_id=res.id
		JOIN parties p ON rv.party_id=p.id
		WHERE res.polling_unit_code=? AND res.election_id=?
		ORDER BY rv.votes DESC`, puCode, electionID)
	if err != nil {
		return fmt.Sprintf(ussdText(lang, "no_results"), puName)
	}
	defer rows.Close()

	var lines []string
	lines = append(lines, fmt.Sprintf("RESULTS: %s (%s)", puName, puCode))
	total := 0
	for rows.Next() {
		var party string
		var votes int
		if err := rows.Scan(&party, &votes); err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %d", party, votes))
		total += votes
	}
	if len(lines) == 1 {
		return fmt.Sprintf(ussdText(lang, "no_results"), puName)
	}
	lines = append(lines, fmt.Sprintf(ussdText(lang, "total_votes"), total))
	return strings.Join(lines, "\n")
}

func getSMSVerify(puCode string, electionID int) string {
	return getSMSVerifyL(puCode, electionID, "en")
}

func getSMSVerifyL(puCode string, electionID int, lang string) string {
	puCode = strings.TrimSpace(puCode)
	var puName, status, tbStatus, hlStatus string
	err := db.QueryRow(`SELECT pu.name, r.status, r.tigerbeetle_status, r.hyperledger_status
		FROM results r JOIN polling_units pu ON r.polling_unit_code=pu.code
		WHERE r.polling_unit_code=? AND r.election_id=?`, puCode, electionID).Scan(&puName, &status, &tbStatus, &hlStatus)
	if err != nil {
		return fmt.Sprintf(ussdText(lang, "no_result_verify"), puCode)
	}

	verified := "NOT VERIFIED"
	if tbStatus == "POSTED" && hlStatus == "CONFIRMED" {
		verified = "VERIFIED (Dual-Ledger Confirmed)"
	} else if tbStatus == "POSTED" {
		verified = "PARTIAL (TigerBeetle only)"
	}

	return fmt.Sprintf("VERIFY: %s\nStatus: %s\nTigerBeetle: %s\nHyperledger: %s\nResult: %s", puName, status, tbStatus, hlStatus, verified)
}

// ussdSupportedLangs are the languages offered at the top of the voter USSD
// menu (level-0 language selection, R5-077). Order matches menu digits 1-5.
var ussdSupportedLangs = []string{"en", "ha", "yo", "ig", "pcm"}

// ussdStrings holds the localized USSD menu chrome. Result detail lines
// (party codes, vote counts, ledger statuses) are language-neutral and stay
// as-is; everything the voter *navigates* by is translated.
var ussdStrings = map[string]map[string]string{
	"en": {
		"lang_menu":        "CON Select Language / Zabi Harshe / Yan Ede / Horo Asusu\n1. English\n2. Hausa\n3. Yoruba\n4. Igbo\n5. Naija (Pidgin)",
		"main_menu":        "CON INEC Result Verification\n1. Check Result by PU Code\n2. Election Status\n3. Verify Result\n4. Report Incident\n0. Exit",
		"enter_pu":         "CON Enter Polling Unit Code\n(e.g. AB-001-W001-PU001):",
		"enter_pu_verify":  "CON Enter PU Code to verify:",
		"enter_incident":   "CON Describe the incident briefly:",
		"goodbye":          "END Thank you for using INEC services.",
		"invalid":          "END Invalid option. Please dial again.",
		"invalid_lang":     "END Invalid selection. Dial again and choose 1-5 for language.",
		"incident_ok":      "END Incident reported. Reference: %s\nThank you for protecting Nigeria's democracy.",
		"incident_fail":    "END Service unavailable - your report was NOT recorded. Please try again.",
		"pu_not_found":     "Polling unit %s not found. Check code and try again.",
		"no_results":       "%s: No results submitted yet.",
		"total_votes":      "TOTAL: %d votes",
		"no_result_verify": "No result to verify for %s",
		"status_tpl":       "ELECTION: %s\nStatus: %s\nResults: %d/%d (%.1f%%)\nFinalized: %d",
	},
	"ha": {
		"lang_menu":        "CON Select Language / Zabi Harshe / Yan Ede / Horo Asusu\n1. English\n2. Hausa\n3. Yoruba\n4. Igbo\n5. Naija (Pidgin)",
		"main_menu":        "CON Tabbatar da Sakamakon INEC\n1. Duba sakamako da lambar rumfa\n2. Matsayin zabe\n3. Tabbatar da sakamako\n4. Ba da rahoton lamari\n0. Fita",
		"enter_pu":         "CON Shigar da lambar rumfar zabe\n(misali AB-001-W001-PU001):",
		"enter_pu_verify":  "CON Shigar da lambar rumfa don tabbatarwa:",
		"enter_incident":   "CON Bayyana lamarin a takaice:",
		"goodbye":          "END Na gode da amfani da sabis na INEC.",
		"invalid":          "END Zabi ba daidai ba. Sake dailawa.",
		"invalid_lang":     "END Zabi ba daidai ba. Sake dailawa ka zaɓi 1-5 don harshe.",
		"incident_ok":      "END An karbi rahoton. Lambar tunawa: %s\nNa gode da kare dimokuradiyyar Najeriya.",
		"incident_fail":    "END Sabis ba ya aiki - ba a rubuta rahotonku ba. Sake gwadawa.",
		"pu_not_found":     "Ba a sami rumfar %s ba. Duba lambar kuma sake gwadawa.",
		"no_results":       "%s: Ba a gabatar da sakamako ba tukuna.",
		"total_votes":      "JIMILLA: %d kuri'u",
		"no_result_verify": "Babu sakamako da za a tabbatar don %s",
		"status_tpl":       "ZABE: %s\nMatsayi: %s\nSakamako: %d/%d (%.1f%%)\nAn kammala: %d",
	},
	"yo": {
		"lang_menu":        "CON Select Language / Zabi Harshe / Yan Ede / Horo Asusu\n1. English\n2. Hausa\n3. Yoruba\n4. Igbo\n5. Naija (Pidgin)",
		"main_menu":        "CON Ifayewo Abajade INEC\n1. Sayewo abajade pelu koodu ile-idibo\n2. Ipo idibo\n3. Jerisi abajade\n4. Jabo isele\n0. Jade",
		"enter_pu":         "CON Te koodu ile-idibo sii\n(fun apeere AB-001-W001-PU001):",
		"enter_pu_verify":  "CON Te koodu ile-idibo lati jerisi:",
		"enter_incident":   "CON Salaye isele naa ni soki:",
		"goodbye":          "END E dupe fun lilo ise INEC.",
		"invalid":          "END Asayan ko to. Tun pe pada.",
		"invalid_lang":     "END Asayan ko to. Tun pe ki o yan 1-5 fun ede.",
		"incident_ok":      "END A ti gba ijabo re. Nomba itokasi: %s\nE dupe fun idaabobo ijoba tiwantiwa Naijiria.",
		"incident_fail":    "END Ise ko wa - a ko gbasile ijabo re. Gbiyanju leekansi.",
		"pu_not_found":     "A ko ri ile-idibo %s. Sayewo koodu ki o tun gbiyanju.",
		"no_results":       "%s: A ko ti fi abajade kankan sile.",
		"total_votes":      "LAPAPO: %d ibo",
		"no_result_verify": "Ko si abajade lati jerisi fun %s",
		"status_tpl":       "IDIBO: %s\nIpo: %s\nAbajade: %d/%d (%.1f%%)\nTi pari: %d",
	},
	"ig": {
		"lang_menu":        "CON Select Language / Zabi Harshe / Yan Ede / Horo Asusu\n1. English\n2. Hausa\n3. Yoruba\n4. Igbo\n5. Naija (Pidgin)",
		"main_menu":        "CON Nnyocha Nsonaazu INEC\n1. Lelee nsonaazu site na koodu ebe ntuli aka\n2. Onodu ntuli aka\n3. Kwenye nsonaazu\n4. Koo ihe mere\n0. Puo",
		"enter_pu":         "CON Tinye koodu ebe ntuli aka\n(dika AB-001-W001-PU001):",
		"enter_pu_verify":  "CON Tinye koodu ebe ntuli aka iji kwenye:",
		"enter_incident":   "CON Kowaa ihe mere nkenke:",
		"goodbye":          "END Daalu maka iji oru INEC.",
		"invalid":          "END Nhoro ezighi ezi. Biko kpoo ozo.",
		"invalid_lang":     "END Nhoro ezighi ezi. Kpoo ozo horo 1-5 maka asusu.",
		"incident_ok":      "END Enatabara akuko gi. Nomba ntụaka: %s\nDaalu maka ichekwa ochicho onye kwuo uche ya Naijiria.",
		"incident_fail":    "END Oru adighi - e jibeghi dekoo akuko gi. Nwaa ozo.",
		"pu_not_found":     "Achotaghi ebe ntuli aka %s. Lelee koodu wee nwaa ozo.",
		"no_results":       "%s: Enyibeghi nsonaazu o bula.",
		"total_votes":      "NGUKOTA: %d vootu",
		"no_result_verify": "Enweghi nsonaazu akwenyere maka %s",
		"status_tpl":       "NTULI AKA: %s\nOnodu: %s\nNsonaazu: %d/%d (%.1f%%)\nEmechaala: %d",
	},
	"pcm": {
		"lang_menu":        "CON Select Language / Zabi Harshe / Yan Ede / Horo Asusu\n1. English\n2. Hausa\n3. Yoruba\n4. Igbo\n5. Naija (Pidgin)",
		"main_menu":        "CON INEC Result Check\n1. Check result with PU code\n2. Election status\n3. Verify result\n4. Report wahala\n0. Comot",
		"enter_pu":         "CON Enter di Polling Unit Code\n(e.g. AB-001-W001-PU001):",
		"enter_pu_verify":  "CON Enter PU code make we verify am:",
		"enter_incident":   "CON Describe di wahala small:",
		"goodbye":          "END Thank you for using INEC service.",
		"invalid":          "END Dat one no correct. Dial again.",
		"invalid_lang":     "END Dat one no correct. Dial again pick 1-5 for language.",
		"incident_ok":      "END We don receive your report. Reference: %s\nThank you for protecting Nigeria democracy.",
		"incident_fail":    "END Service no dey - we no record your report. Try again.",
		"pu_not_found":     "We no see polling unit %s. Check di code try again.",
		"no_results":       "%s: Dem never submit any result yet.",
		"total_votes":      "TOTAL: %d votes",
		"no_result_verify": "No result dey to verify for %s",
		"status_tpl":       "ELECTION: %s\nStatus: %s\nResults: %d/%d (%.1f%%)\nFinalized: %d",
	},
}

// ussdText returns the localized string for a USSD menu key, falling back to
// English for any missing key (never returns empty).
func ussdText(lang, key string) string {
	if d, ok := ussdStrings[lang]; ok {
		if s, ok := d[key]; ok && s != "" {
			return s
		}
	}
	return ussdStrings["en"][key]
}

func getSMSStatus(electionID int) string {
	return getSMSStatusL(electionID, "en")
}

func getSMSStatusL(electionID int, lang string) string {
	// INTEGRITY: never emit a fabricated "0/176K (0.0%)" status when the
	// database is unreachable — report the status as unavailable instead.
	if db == nil {
		return "Election status unavailable: service temporarily down. Please try again later."
	}
	var name, status string
	var totalPUs int
	if err := db.QueryRow("SELECT title, status FROM elections WHERE id=?", electionID).Scan(&name, &status); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Sprintf("Election status unavailable: election %d not found.", electionID)
		}
		log.Error().Err(err).Int("election_id", electionID).Msg("sms status: election query failed")
		return "Election status unavailable: service error. Please try again later."
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM polling_units").Scan(&totalPUs); err != nil {
		log.Error().Err(err).Msg("sms status: polling unit count failed")
		return "Election status unavailable: service error. Please try again later."
	}

	var submitted, finalized int
	if err := db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=?", electionID).Scan(&submitted); err != nil {
		log.Error().Err(err).Int("election_id", electionID).Msg("sms status: results count failed")
		return "Election status unavailable: service error. Please try again later."
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=? AND status='finalized'", electionID).Scan(&finalized); err != nil {
		log.Error().Err(err).Int("election_id", electionID).Msg("sms status: finalized count failed")
		return "Election status unavailable: service error. Please try again later."
	}

	pct := 0.0
	if totalPUs > 0 {
		pct = float64(submitted) / float64(totalPUs) * 100
	}

	return fmt.Sprintf(ussdText(lang, "status_tpl"), name, status, submitted, totalPUs, pct, finalized)
}

// decodeUSSDRequest accepts both JSON bodies and the
// application/x-www-form-urlencoded session posts used by Africa's Talking
// (sessionId/phoneNumber/serviceCode/text) and Twilio-style gateways.
func decodeUSSDRequest(r *http.Request) (USSDRequest, error) {
	var req USSDRequest
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/x-www-form-urlencoded") || strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseForm(); err != nil {
			return req, err
		}
		req.SessionID = firstNonEmpty(r.FormValue("sessionId"), r.FormValue("session_id"), r.FormValue("SessionId"))
		req.PhoneNumber = firstNonEmpty(r.FormValue("phoneNumber"), r.FormValue("phone_number"), r.FormValue("From"), r.FormValue("msisdn"))
		req.ServiceCode = firstNonEmpty(r.FormValue("serviceCode"), r.FormValue("service_code"))
		req.Text = firstNonEmpty(r.FormValue("text"), r.FormValue("Body"))
		return req, nil
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	return req, err
}

func handleUSSDGateway(w http.ResponseWriter, r *http.Request) {
	req, err := decodeUSSDRequest(r)
	if err != nil {
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

	// Flow: level 0 = language selection (1-5), level 1 = main menu in the
	// chosen language, level 2+ = action inputs. The session language travels
	// in the cumulative USSD text, so the gateway stays stateless.
	if text == "" {
		response = ussdText("en", "lang_menu")
		dbExecLog("db_op", `INSERT INTO ussd_sessions (id, phone, stage, data) VALUES (?,?,'lang_menu','{}')`,
			sessionID, req.PhoneNumber)
	} else if len(parts) == 1 {
		langIdx := 0
		if n, cerr := fmt.Sscanf(parts[0], "%d", &langIdx); n != 1 || cerr != nil || langIdx < 1 || langIdx > len(ussdSupportedLangs) {
			response = ussdText("en", "invalid_lang")
			continueSession = false
		} else {
			response = ussdText(ussdSupportedLangs[langIdx-1], "main_menu")
		}
	} else if len(parts) >= 2 {
		lang := "en"
		if langIdx, cerr := strconv.Atoi(parts[0]); cerr == nil && langIdx >= 1 && langIdx <= len(ussdSupportedLangs) {
			lang = ussdSupportedLangs[langIdx-1]
		}
		if len(parts) == 2 {
			switch parts[1] {
			case "1":
				response = ussdText(lang, "enter_pu")
			case "2":
				response = "END " + getSMSStatusL(1, lang)
				continueSession = false
			case "3":
				response = ussdText(lang, "enter_pu_verify")
			case "4":
				response = ussdText(lang, "enter_incident")
			case "0":
				response = ussdText(lang, "goodbye")
				continueSession = false
			default:
				response = ussdText(lang, "invalid")
				continueSession = false
			}
		} else {
			switch parts[1] {
			case "1":
				response = "END " + getSMSResultL(parts[2], 1, lang)
				continueSession = false
			case "3":
				response = "END " + getSMSVerifyL(parts[2], 1, lang)
				continueSession = false
			case "4":
				// INTEGRITY: persist the report; never confirm a report that
				// was not recorded (same guarantees as the IVR/USSD flows).
				incidentID, ierr := persistPublicIncident("ussd_report", parts[2], "medium", "", req.PhoneNumber, "", "", "ussd")
				if ierr != nil {
					log.Error().Err(ierr).Msg("ussd: public incident persistence failed")
					response = ussdText(lang, "incident_fail")
				} else {
					response = fmt.Sprintf(ussdText(lang, "incident_ok"), incidentID)
				}
				continueSession = false
			default:
				response = ussdText(lang, "invalid")
				continueSession = false
			}
		}
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
	_ = db.QueryRow("SELECT COUNT(*) FROM sms_verifications WHERE channel='sms'").Scan(&totalSMS)
	_ = db.QueryRow("SELECT COUNT(*) FROM sms_verifications WHERE channel='ussd'").Scan(&totalUSSD)

	var today int
	_ = db.QueryRow("SELECT COUNT(*) FROM sms_verifications WHERE created_at >= CURRENT_DATE").Scan(&today)

	rows, _ := db.Query(`SELECT request_type, COUNT(*) as cnt FROM sms_verifications
		GROUP BY request_type ORDER BY cnt DESC`)
	defer rows.Close()
	byType := []M{}
	for rows.Next() {
		var rt string
		var cnt int
		if err := rows.Scan(&rt, &cnt); err != nil {
			continue
		}
		byType = append(byType, M{"type": rt, "count": cnt})
	}

	writeJSON(w, 200, M{
		"total_sms":  totalSMS,
		"total_ussd": totalUSSD,
		"today":      today,
		"by_type":    byType,
	})
}

func initUSSDEngine() {
	initSMSUSSDTables(db)
}

func handleUSSDSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, 400, "session_id is required")
		return
	}
	var phone, stage, data, created, updated string
	err := db.QueryRow(`SELECT phone, stage, data, created_at, updated_at FROM ussd_sessions WHERE id=?`, sessionID).Scan(&phone, &stage, &data, &created, &updated)
	if err != nil {
		writeError(w, 404, "session not found")
		return
	}
	writeJSON(w, 200, M{
		"session_id": sessionID,
		"phone":      phone,
		"stage":      stage,
		"data":       data,
		"created_at": created,
		"updated_at": updated,
	})
}

func handleUSSDDashboard(w http.ResponseWriter, r *http.Request) {
	var totalSessions, activeSessions int
	db.QueryRow(`SELECT COUNT(*) FROM ussd_sessions`).Scan(&totalSessions)
	db.QueryRow(`SELECT COUNT(*) FROM ussd_sessions WHERE updated_at > datetime('now', '-5 minutes')`).Scan(&activeSessions)

	var totalUSSD int
	db.QueryRow(`SELECT COUNT(*) FROM sms_verifications WHERE channel='ussd'`).Scan(&totalUSSD)

	rows, _ := db.Query(`SELECT stage, COUNT(*) FROM ussd_sessions GROUP BY stage`)
	byStage := []M{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var s string
			var c int
			rows.Scan(&s, &c)
			byStage = append(byStage, M{"stage": s, "count": c})
		}
	}

	writeJSON(w, 200, M{
		"total_sessions":      totalSessions,
		"active_sessions":     activeSessions,
		"total_ussd_requests": totalUSSD,
		"by_stage":            byStage,
	})
}
