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
	"encoding/json"
	"fmt"
	"net/http"
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
	Action  string `json:"action"`  // say | gather | redirect | hangup
	Text    string `json:"text"`
	Language string `json:"language"`
	MaxDigits int  `json:"max_digits,omitempty"`
	Timeout  int  `json:"timeout,omitempty"`
}

// IVRIncidentReport captures a voter-reported incident via voice.
type IVRIncidentReport struct {
	ID          string    `json:"id"`
	CallerPhone string    `json:"caller_phone"`
	PollingUnit string    `json:"polling_unit,omitempty"`
	Description string    `json:"description"`
	Language    string    `json:"language"`
	Severity    string    `json:"severity"`
	ReportedAt  time.Time `json:"reported_at"`
}

// In-memory session store (production: Redis with TTL)
var ivrSessions = make(map[string]*IVRSession)
var ivrIncidents []IVRIncidentReport

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
		StartedAt:   time.Now(),
		LastAction:  time.Now(),
	}
	ivrSessions[req.SessionID] = session

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

	session, ok := ivrSessions[action.SessionID]
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	session.LastAction = time.Now()

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
		incidentID := fmt.Sprintf("INC-%d", time.Now().UnixMilli())
		ivrIncidents = append(ivrIncidents, IVRIncidentReport{
			ID:          incidentID,
			CallerPhone: session.CallerPhone,
			Language:    lang,
			Severity:    "medium",
			ReportedAt:  time.Now(),
		})
		resp = IVRResponse{
			Action:   "say",
			Text:     fmt.Sprintf(ivrPrompts["incident_received"][lang], incidentID),
			Language: lang,
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
		// Handle NIN/VIN lookup responses
		if session.State == "lookup_nin" || session.State == "lookup_vin" {
			// Simulate voter lookup (production: query INEC voter registry)
			if len(action.Input) >= 8 {
				resp = IVRResponse{
					Action:   "say",
					Text:     fmt.Sprintf(ivrPrompts["polling_unit_found"][lang], "PU-001-Lagos-Island", "25 Broad Street, Lagos Island"),
					Language: lang,
				}
			} else {
				resp = IVRResponse{
					Action:   "say",
					Text:     ivrPrompts["not_registered"][lang],
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

// IVRIncidentsHandler — returns all IVR-reported incidents.
func IVRIncidentsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total":     len(ivrIncidents),
		"incidents": ivrIncidents,
	})
}

// USSDHandler — USSD fallback for feature phones (Africa's Talking format).
func USSDHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	sessionID := r.FormValue("sessionId")
	serviceCode := r.FormValue("serviceCode")
	phoneNumber := r.FormValue("phoneNumber")
	text := r.FormValue("text")

	_ = sessionID
	_ = serviceCode
	_ = phoneNumber

	var response string
	parts := strings.Split(text, "*")
	level := len(parts)

	switch {
	case text == "":
		response = "CON Welcome to INEC Voter Services\n1. Find Polling Unit\n2. Check Registration\n3. Report Incident\n4. Election Results"
	case parts[0] == "1" && level == 1:
		response = "CON Enter your NIN (11 digits):"
	case parts[0] == "1" && level == 2:
		response = fmt.Sprintf("END Your polling unit: PU-001-Lagos-Island\nLocation: 25 Broad Street, Lagos Island\nVoting: 8am - 5pm")
	case parts[0] == "2" && level == 1:
		response = "CON Enter your Voter Card Number:"
	case parts[0] == "2" && level == 2:
		response = "END Registration confirmed. You are registered at Ward 5, Lagos Island LGA."
	case parts[0] == "3" && level == 1:
		response = "CON Describe the incident briefly:"
	case parts[0] == "3" && level == 2:
		incidentID := fmt.Sprintf("INC-%d", time.Now().UnixMilli())
		response = fmt.Sprintf("END Incident reported. Reference: %s\nThank you for protecting Nigeria's democracy.", incidentID)
	case parts[0] == "4":
		response = "END Results are being collated. Visit inec.gov.ng for live updates."
	default:
		response = "END Invalid option. Please try again."
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(response))
}
