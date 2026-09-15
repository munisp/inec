package main

// ivr_voter_assistance.go
//
// Innovation 8: Voice-Based IVR Voter Assistance System
// ======================================================
// Provides a multi-lingual Interactive Voice Response (IVR) system that
// allows voters to:
//   1. Look up their polling unit by NIN or voter card number
//   2. Confirm their registration status
//   3. Report election incidents via voice
//   4. Get real-time updates on election results in their area
//   5. Access voter education content in Hausa, Yoruba, Igbo, and English
//
// Architecture:
//   - Integrates with Africa's Talking (AT) Voice API (open-source compatible)
//   - Uses Asterisk/FreeSWITCH for on-premise telephony (no vendor lock-in)
//   - Speech synthesis via Coqui TTS (open-source)
//   - Speech recognition via Whisper (open-source)
//   - Supports USSD fallback for feature phones

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

// IVRSession represents an active voice call session.
type IVRSession struct {
	SessionID   string    `json:"session_id"`
	CallerPhone string    `json:"caller_phone"`
	Language    string    `json:"language"` // en, ha, yo, ig
	State       string    `json:"state"`    // menu, lookup, incident, results
	VoterID     string    `json:"voter_id,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	LastAction  time.Time `json:"last_action"`
}

// IVRAction represents a caller's DTMF input or voice command.
type IVRAction struct {
	SessionID string `json:"session_id"`
	Input     string `json:"input"`      // DTMF digits or transcribed speech
	InputType string `json:"input_type"` // dtmf | voice
}

// IVRResponse is sent back to the telephony platform.
type IVRResponse struct {
	Action    string `json:"action"` // say | gather | redirect | hangup
	Text      string `json:"text"`
	Language  string `json:"language"`
	MaxDigits int    `json:"max_digits,omitempty"`
	Timeout   int    `json:"timeout,omitempty"`
}

// IVR sessions are persisted in the `ivr_sessions` table (migration
// 000038_ivr_sessions) instead of a process-local map, so telephony
// callbacks keep working across replicas and restarts (R5-120 sub-action).
func ivrSaveSession(s *IVRSession) error {
	if db == nil {
		return fmt.Errorf("database unavailable")
	}
	_, err := db.Exec(`INSERT INTO ivr_sessions (session_id, caller_phone, language, state, started_at, last_action)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT (session_id) DO UPDATE SET language=EXCLUDED.language, state=EXCLUDED.state, last_action=EXCLUDED.last_action`,
		s.SessionID, s.CallerPhone, s.Language, s.State, s.StartedAt.UTC(), s.LastAction.UTC())
	return err
}

func ivrLoadSession(sessionID string) (*IVRSession, error) {
	if db == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	var s IVRSession
	err := db.QueryRow(`SELECT session_id, COALESCE(caller_phone,''), language, state, started_at, last_action
		FROM ivr_sessions WHERE session_id=?`, sessionID).
		Scan(&s.SessionID, &s.CallerPhone, &s.Language, &s.State, &s.StartedAt, &s.LastAction)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ivrUssdStrings localizes the voter-services USSD menu (find PU, check
// registration, report incident, results). Diacritics are intentionally
// avoided: GSM-7 USSD transport cannot carry them reliably.
var ivrUssdStrings = map[string]map[string]string{
	"en": {
		"vs_main_menu":    "CON INEC Voter Services\n1. Find Polling Unit\n2. Check Registration\n3. Report Incident\n4. Election Results",
		"vs_enter_nin":    "CON Enter your NIN (11 digits):",
		"vs_enter_vin":    "CON Enter your Voter Card Number:",
		"vs_pu_found":     "END Your polling unit: %s\nWard: %s\nVoting: 8am - 5pm",
		"vs_not_found":    "END No registration found. Please visit your nearest INEC office with your National ID.",
		"vs_reg_found":    "END Registration found (status: %s). Ward: %s, LGA: %s.",
		"vs_service_down": "END Service unavailable, please try again later.",
		"vs_results_info": "END Results are being collated. Visit inec.gov.ng for live updates.",
	},
	"ha": {
		"vs_main_menu":    "CON Sabis na Masu Jefa Kuri'a na INEC\n1. Nemo rumfar zabe\n2. Duba rajista\n3. Ba da rahoton lamari\n4. Sakamakon zabe",
		"vs_enter_nin":    "CON Shigar da lambar NIN (lambobi 11):",
		"vs_enter_vin":    "CON Shigar da lambar katin zabenka:",
		"vs_pu_found":     "END Rumfar zabenka: %s\nWardi: %s\nJefa kuri'a: 8am - 5pm",
		"vs_not_found":    "END Ba a sami rajista ba. Ziyarci ofishin INEC mafi kusa da katin shaida.",
		"vs_reg_found":    "END An sami rajista (matsayi: %s). Wardi: %s, Karamar hukuma: %s.",
		"vs_service_down": "END Sabis ba ya aiki yanzu, sake gwadawa daga baya.",
		"vs_results_info": "END Ana tattara sakamakon. Duba inec.gov.ng don sabuntawa.",
	},
	"yo": {
		"vs_main_menu":    "CON Ise INEC fun Oludibo\n1. Wa ile-idibo re\n2. Sayewo iforukosile re\n3. Jabo isele\n4. Abajade idibo",
		"vs_enter_nin":    "CON Te nomba NIN re (onka 11):",
		"vs_enter_vin":    "CON Te nomba kaadi oludibo re:",
		"vs_pu_found":     "END Ile-idibo re: %s\nWadi: %s\nIdibo: 8am - 5pm",
		"vs_not_found":    "END A ko ri iforukosile. Sabewo si ofisi INEC to sunmo pelu kaadi idanimo.",
		"vs_reg_found":    "END A ri iforukosile (ipo: %s). Wadi: %s, Ijoba ibile: %s.",
		"vs_service_down": "END Ise ko wa fun igba die, gbiyanju leekansi.",
		"vs_results_info": "END Won n ka awon abajade jo. Wo inec.gov.ng fun imudojuiwon.",
	},
	"ig": {
		"vs_main_menu":    "CON Oru INEC nke Ndi Vootu\n1. Chota ebe ntuli aka gi\n2. Lelee ndebanye aha gi\n3. Koo ihe mere\n4. Nsonaazu ntuli aka",
		"vs_enter_nin":    "CON Tinye nomba NIN gi (onu ogugu 11):",
		"vs_enter_vin":    "CON Tinye nomba kaadi vootu gi:",
		"vs_pu_found":     "END Ebe ntuli aka gi: %s\nWard: %s\nNtuli aka: 8am - 5pm",
		"vs_not_found":    "END Achotaghi ndebanye aha. Gaa ulo oru INEC kacha nso gi na kaadi njirimara mba.",
		"vs_reg_found":    "END Achotala ndebanye aha (onodu: %s). Ward: %s, LGA: %s.",
		"vs_service_down": "END Oru adighi ugbu a, nwaa ozo ma e mechaa.",
		"vs_results_info": "END Ana aguko nsonaazu. Lelee inec.gov.ng maka mmelite.",
	},
	"pcm": {
		"vs_main_menu":    "CON INEC Voter Service\n1. Find your polling unit\n2. Check your registration\n3. Report wahala\n4. Election results",
		"vs_enter_nin":    "CON Enter your NIN (11 digits):",
		"vs_enter_vin":    "CON Enter your Voter Card Number:",
		"vs_pu_found":     "END Your polling unit: %s\nWard: %s\nVoting na 8am - 5pm",
		"vs_not_found":    "END We no see your registration. Go di INEC office near you with your National ID.",
		"vs_reg_found":    "END We see your registration (status: %s). Ward: %s, LGA: %s.",
		"vs_service_down": "END Service no dey now, try again later.",
		"vs_results_info": "END Dem dey collate results. Check inec.gov.ng for updates.",
	},
}

func ivrUssdText(lang, key string) string {
	if d, ok := ivrUssdStrings[lang]; ok {
		if s, ok := d[key]; ok && s != "" {
			return s
		}
	}
	return ivrUssdStrings["en"][key]
}

// IVR menu prompts in all four languages
var ivrPrompts = map[string]map[string]string{
	"welcome": {
		"en": "Welcome to INEC Voter Assistance. Press 1 to find your polling unit. Press 2 to check your registration. Press 3 to report an incident. Press 4 for election results. Press 5 to change language.",
		"ha": "Barka da zuwa INEC Taimakon Masu Jefa Kuri'a. Danna 1 don nemo rumfar zabenku. Danna 2 don duba rajistanku. Danna 3 don ba da rahoton lamari. Danna 4 don sakamakon zabe.",
		"yo": "E kaabo si INEC Iranlowo Awon Oludibo. Tẹ 1 lati wa ile-idibo rẹ. Tẹ 2 lati ṣayẹwo iforukọsilẹ rẹ. Tẹ 3 lati jabo iṣẹlẹ kan. Tẹ 4 fun awọn abajade idibo.",
		"ig": "Nnọọ na INEC Enyemaka Ndị Ntuli Aka. Pịa 1 iji chọta ebe ntuli aka gị. Pịa 2 iji lelee ndebanye aha gị. Pịa 3 iji kọọ ihe mere. Pịa 4 maka nsonaazụ ntuli aka.",
	},
	"polling_unit_found": {
		"en": "Your polling unit is %s, located at %s. Voting takes place from 8am to 5pm.",
		"ha": "Rumfar zabenku ita ce %s, a %s. Ana jefa kuri'a daga karfe 8 na safe zuwa 5 na yamma.",
		"yo": "Ile-idibo rẹ ni %s, ti o wa ni %s. Idibo waye lati 8 owurọ si 5 irọlẹ.",
		"ig": "Ebe ntuli aka gị bụ %s, dị na %s. Ntuli aka na-eme site 8 n'ụtụtụ ruo 5 n'anyasị.",
	},
	"not_registered": {
		"en": "We could not find your registration. Please visit your nearest INEC office with your National ID card.",
		"ha": "Ba mu sami rajistanku ba. Da fatan za a ziyarci ofishin INEC mafi kusa da katin shaida na kasa.",
		"yo": "A ko le ri iforukọsilẹ rẹ. Jọwọ ṣabẹwo si ọfiisi INEC ti o sunmọ rẹ pẹlu kaadi idanimọ orilẹ-ede rẹ.",
		"ig": "Anyị enweghị ike ịchọta ndebanye aha gị. Biko gaa ụlọ ọrụ INEC kacha nso gị na kaadị njirimara mba gị.",
	},
	"incident_received": {
		"en": "Your incident report has been recorded. Reference number: %s. Thank you for helping protect Nigeria's democracy.",
		"ha": "An rubuta rahoton lamarin ku. Lambar tunawa: %s. Na gode da taimakawa wajen kare dimokuradiyyar Najeriya.",
		"yo": "Ijabọ iṣẹlẹ rẹ ti gbasilẹ. Nọmba itọkasi: %s. E dupe fun iranlọwọ rẹ ni aabo ijọba tiwantiwa Nigeria.",
		"ig": "Akọwapụtara akụkọ ihe mere gị. Nọmba ntụaka: %s. Daalụ maka inyere aka ichekwa ọchịchọ onye kwuo uche ya nke Naịjirịa.",
	},
}

// serviceUnavailablePrompts is played when the voter registry cannot be queried.
var serviceUnavailablePrompts = map[string]string{
	"en": "This service is temporarily unavailable. Please try again later.",
	"ha": "Wannan sabis na wucin gadi ba ya aiki yanzu. Da fatan za a sake gwadawa daga baya.",
	"yo": "Iṣẹ yii ko wa fun igba diẹ. Jọwọ gbiyanju lẹẹkansi nigbamii.",
	"ig": "Ọrụ a anaghị arụ ọrụ ugbu a. Biko nwaa ọzọ ma e mechaa.",
}

// lookupVoterPU queries the voter registry for a voter's polling unit by NIN
// or by voter card number (PVC number / VIN). INTEGRITY: this is a real
// registry lookup — found=false means no registration exists, and err!=nil
// means the lookup itself failed and the caller must say so honestly.
func lookupVoterPU(byNIN bool, value string) (puCode, puName, wardName string, found bool, err error) {
	if db == nil {
		return "", "", "", false, fmt.Errorf("voter registry unavailable")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", "", false, nil
	}
	where := "v.nin = $1"
	if !byNIN {
		where = "(v.pvc_number = $1 OR v.vin = $1)"
	}
	row := db.QueryRow(`SELECT v.polling_unit_code, COALESCE(pu.name,''), COALESCE(w.name, v.ward_code)
		FROM voters v
		LEFT JOIN polling_units pu ON pu.code = v.polling_unit_code
		LEFT JOIN wards w ON w.code = v.ward_code
		WHERE `+where, value) // #nosec G201 -- 'where' is a hardcoded literal from the branch above, value is parameterized
	err = row.Scan(&puCode, &puName, &wardName)
	if err == sql.ErrNoRows {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, err
	}
	return puCode, puName, wardName, true, nil
}

// lookupVoterRegistration returns a voter's registration status and location
// by voter card number (PVC / VIN). Never reports a confirmation without a row.
func lookupVoterRegistration(value string) (status, wardName, lgaCode string, found bool, err error) {
	if db == nil {
		return "", "", "", false, fmt.Errorf("voter registry unavailable")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", "", false, nil
	}
	err = db.QueryRow(`SELECT v.status, COALESCE(w.name, v.ward_code), v.lga_code
		FROM voters v
		LEFT JOIN wards w ON w.code = v.ward_code
		WHERE v.pvc_number = $1 OR v.vin = $1`, value).Scan(&status, &wardName, &lgaCode)
	if err == sql.ErrNoRows {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, err
	}
	return status, wardName, lgaCode, true, nil
}

// persistIVRIncident writes a voter-reported incident into the shared public
// incident pipeline (public_incidents.go) and returns its reference ID. A
// non-nil error means the report was NOT recorded — callers must not claim
// it was.
func persistIVRIncident(incidentType, description, severity string) (string, error) {
	source := "ivr"
	if incidentType == "ussd_report" {
		source = "ussd"
	}
	return persistPublicIncident(incidentType, description, severity, "", "", "", "", source)
}

// IVRStartHandler — initiates a new IVR session (called by telephony platform).
func IVRStartHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID   string `json:"session_id"`
		CallerPhone string `json:"caller_phone"`
		Language    string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	lang := req.Language
	if lang == "" {
		lang = "en"
	}

	session := &IVRSession{
		SessionID:   req.SessionID,
		CallerPhone: req.CallerPhone,
		Language:    lang,
		State:       "menu",
		StartedAt:   time.Now().UTC(),
		LastAction:  time.Now().UTC(),
	}
	if err := ivrSaveSession(session); err != nil {
		log.Error().Err(err).Msg("IVR session persistence failed")
		http.Error(w, "session store unavailable", http.StatusServiceUnavailable)
		return
	}

	log.Info().Str("session", req.SessionID).Str("caller", req.CallerPhone).Msg("IVR session started")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(IVRResponse{
		Action:    "gather",
		Text:      ivrPrompts["welcome"][lang],
		Language:  lang,
		MaxDigits: 1,
		Timeout:   10,
	})
}

// IVRActionHandler — processes a caller's DTMF input.
func IVRActionHandler(w http.ResponseWriter, r *http.Request) {
	var action IVRAction
	if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	session, lerr := ivrLoadSession(action.SessionID)
	if lerr != nil {
		if lerr == sql.ErrNoRows {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		log.Error().Err(lerr).Msg("IVR session load failed")
		http.Error(w, "session store unavailable", http.StatusServiceUnavailable)
		return
	}
	session.LastAction = time.Now().UTC()
	defer func() {
		if err := ivrSaveSession(session); err != nil {
			log.Error().Err(err).Msg("IVR session save failed")
		}
	}()

	var resp IVRResponse
	lang := session.Language

	switch action.Input {
	case "1": // Find polling unit
		resp = IVRResponse{
			Action:    "gather",
			Text:      map[string]string{"en": "Please enter your 11-digit NIN.", "ha": "Da fatan za a shigar da lambar NIN ta lambobi 11.", "yo": "Jọwọ tẹ nọmba NIN rẹ ti o ni awọn nọmba 11.", "ig": "Biko tinye nọmba NIN gị nke nwere ọnụọgụ 11."}[lang],
			Language:  lang,
			MaxDigits: 11,
			Timeout:   15,
		}
		session.State = "lookup_nin"

	case "2": // Check registration
		resp = IVRResponse{
			Action:    "gather",
			Text:      map[string]string{"en": "Please enter your voter card number.", "ha": "Da fatan za a shigar da lambar katin zabenku.", "yo": "Jọwọ tẹ nọmba kaadi oludibo rẹ.", "ig": "Biko tinye nọmba kaadị ntuli aka gị."}[lang],
			Language:  lang,
			MaxDigits: 19,
			Timeout:   15,
		}
		session.State = "lookup_vin"

	case "3": // Report incident
		// INTEGRITY: persist the report to the incidents table; only confirm
		// receipt when the row actually exists.
		incidentID, err := persistPublicIncident("ivr_report", "incident reported via IVR by "+session.CallerPhone, "medium", "", session.CallerPhone, "", "", "ivr")
		if err != nil {
			log.Error().Err(err).Msg("IVR incident persistence failed")
			resp = IVRResponse{
				Action:   "say",
				Text:     serviceUnavailablePrompts[lang],
				Language: lang,
			}
		} else {
			resp = IVRResponse{
				Action:   "say",
				Text:     fmt.Sprintf(ivrPrompts["incident_received"][lang], incidentID),
				Language: lang,
			}
		}

	case "4": // Election results
		resp = IVRResponse{
			Action:   "say",
			Text:     map[string]string{"en": "Results are being collated. Please check the INEC website at inec.gov.ng for live updates.", "ha": "Ana tattara sakamakon. Da fatan za a duba shafin yanar gizon INEC a inec.gov.ng don sabuntawa kai tsaye.", "yo": "Awọn abajade n wa ni iṣiro. Jọwọ ṣayẹwo oju opo wẹẹbu INEC ni inec.gov.ng fun awọn imudojuiwọn laaye.", "ig": "Ana enye nsonaazụ. Biko lelee webụsaịtị INEC na inec.gov.ng maka mmelite dị ndụ."}[lang],
			Language: lang,
		}

	case "5": // Change language
		langs := []string{"en", "ha", "yo", "ig"}
		for i, l := range langs {
			if l == lang {
				session.Language = langs[(i+1)%len(langs)]
				break
			}
		}
		resp = IVRResponse{
			Action:    "gather",
			Text:      ivrPrompts["welcome"][session.Language],
			Language:  session.Language,
			MaxDigits: 1,
			Timeout:   10,
		}

	default:
		// Handle NIN/VIN lookup responses against the real voter registry.
		// INTEGRITY: never invent a polling unit — a lookup failure is stated
		// honestly and a missing registration says "not found".
		if session.State == "lookup_nin" || session.State == "lookup_vin" {
			puCode, puName, wardName, found, err := lookupVoterPU(session.State == "lookup_nin", action.Input)
			switch {
			case err != nil:
				log.Error().Err(err).Str("state", session.State).Msg("IVR voter lookup failed")
				resp = IVRResponse{
					Action:   "say",
					Text:     serviceUnavailablePrompts[lang],
					Language: lang,
				}
			case !found:
				resp = IVRResponse{
					Action:   "say",
					Text:     ivrPrompts["not_registered"][lang],
					Language: lang,
				}
			default:
				label := puName
				if label == "" {
					label = puCode
				}
				resp = IVRResponse{
					Action:   "say",
					Text:     fmt.Sprintf(ivrPrompts["polling_unit_found"][lang], label, wardName),
					Language: lang,
				}
			}
			session.State = "menu"
		} else {
			resp = IVRResponse{
				Action:    "gather",
				Text:      ivrPrompts["welcome"][lang],
				Language:  lang,
				MaxDigits: 1,
				Timeout:   10,
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// IVRIncidentsHandler — returns incidents reported through the public voice/
// text channels (IVR, USSD, WhatsApp, web) from the durable incident pipeline.
// Intended for staff triage UIs; register behind readAuth.
func IVRIncidentsHandler(w http.ResponseWriter, r *http.Request) {
	if db == nil {
		writeError(w, 503, "database unavailable")
		return
	}
	rows, err := db.Query(`SELECT id, incident_type, description, severity, COALESCE(reporter_phone,''), reported_at, status, source
		FROM incidents WHERE source IN ('ivr','ussd','whatsapp','public_web')
		ORDER BY reported_at DESC LIMIT 200`)
	if err != nil {
		log.Error().Err(err).Msg("IVR incidents query failed")
		writeError(w, 500, "failed to list channel incidents")
		return
	}
	defer rows.Close()
	incidents := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var itype, desc, sev, phone, status, source string
		var reportedAt time.Time
		if err := rows.Scan(&id, &itype, &desc, &sev, &phone, &reportedAt, &status, &source); err != nil {
			continue
		}
		incidents = append(incidents, map[string]interface{}{
			"id":           fmt.Sprintf("INC-%d", id),
			"type":         itype,
			"description":  desc,
			"severity":     sev,
			"caller_phone": phone,
			"reported_at":  reportedAt,
			"status":       status,
			"source":       source,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total":     len(incidents),
		"incidents": incidents,
	})
}

// USSDHandler — USSD fallback for feature phones (Africa's Talking form
// format). Level 0 is a language menu (English/Hausa/Yorùbá/Igbo/Naija);
// the chosen language travels in the cumulative session text so the handler
// stays stateless (R5-077).
func USSDHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	sessionID := r.FormValue("sessionId")
	phoneNumber := r.FormValue("phoneNumber")
	text := r.FormValue("text")

	var response string
	parts := strings.Split(text, "*")
	level := len(parts)
	if text == "" {
		level = 0
	}

	switch {
	case level == 0:
		response = ussdText("en", "lang_menu")
		dbExecLog("db_op", `INSERT INTO ussd_sessions (id, phone, stage, data) VALUES (?,?,'lang_menu','{}') ON CONFLICT (id) DO NOTHING`,
			firstNonEmpty(sessionID, fmt.Sprintf("USSD-%d", time.Now().UnixNano())), phoneNumber)
	case level == 1:
		langIdx, err := strconv.Atoi(parts[0])
		if err != nil || langIdx < 1 || langIdx > len(ussdSupportedLangs) {
			response = ussdText("en", "invalid_lang")
		} else {
			response = ivrUssdText(ussdSupportedLangs[langIdx-1], "vs_main_menu")
		}
	case level == 2:
		lang := ussdLangOf(parts[0])
		switch parts[1] {
		case "1":
			response = ivrUssdText(lang, "vs_enter_nin")
		case "2":
			response = ivrUssdText(lang, "vs_enter_vin")
		case "3":
			response = ussdText(lang, "enter_incident")
		case "4":
			response = ivrUssdText(lang, "vs_results_info")
		default:
			response = ussdText(lang, "invalid")
		}
	case level >= 3:
		lang := ussdLangOf(parts[0])
		switch parts[1] {
		case "1":
			// INTEGRITY: real registry lookup — no hardcoded polling unit.
			puCode, puName, wardName, found, err := lookupVoterPU(true, parts[2])
			switch {
			case err != nil:
				log.Error().Err(err).Msg("USSD NIN lookup failed")
				response = ivrUssdText(lang, "vs_service_down")
			case !found:
				response = ivrUssdText(lang, "vs_not_found")
			default:
				label := puName
				if label == "" {
					label = puCode
				}
				response = fmt.Sprintf(ivrUssdText(lang, "vs_pu_found"), label, wardName)
			}
		case "2":
			// INTEGRITY: only confirm a registration when a real row exists.
			status, wardName, lgaCode, found, err := lookupVoterRegistration(parts[2])
			switch {
			case err != nil:
				log.Error().Err(err).Msg("USSD voter-card lookup failed")
				response = ivrUssdText(lang, "vs_service_down")
			case !found:
				response = ivrUssdText(lang, "vs_not_found")
			default:
				response = fmt.Sprintf(ivrUssdText(lang, "vs_reg_found"), status, wardName, lgaCode)
			}
		case "3":
			// INTEGRITY: persist the incident; never issue a reference for a
			// report that was not recorded.
			incidentID, err := persistPublicIncident("ussd_report", parts[2], "medium", "", phoneNumber, "", "", "ussd")
			if err != nil {
				log.Error().Err(err).Msg("USSD incident persistence failed")
				response = ussdText(lang, "incident_fail")
			} else {
				response = fmt.Sprintf(ussdText(lang, "incident_ok"), incidentID)
			}
		default:
			response = ussdText(lang, "invalid")
		}
	default:
		response = ussdText("en", "invalid")
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(response))
}

// ussdLangOf resolves the language digit at the head of the cumulative USSD
// text to a language code, defaulting to English on anything unexpected.
func ussdLangOf(langDigit string) string {
	if langIdx, err := strconv.Atoi(langDigit); err == nil && langIdx >= 1 && langIdx <= len(ussdSupportedLangs) {
		return ussdSupportedLangs[langIdx-1]
	}
	return "en"
}
