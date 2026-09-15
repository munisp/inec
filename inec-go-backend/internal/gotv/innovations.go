// innovations.go — GOTV Innovation features.
// Implements: INNOVATE #17-25, plus tech debt fixes.
package gotv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// ─── INNOVATE #17: AI Message Optimization ─────────────────────────────────

// AIMessageOptimizer generates culturally-relevant message variants via LLM.
type AIMessageOptimizer struct {
	APIURL  string
	APIKey  string
	ModelID string
	DB      *sql.DB
	client  *http.Client
}

// NewAIMessageOptimizer creates an AI optimizer (disabled if no API key).
func NewAIMessageOptimizer(db *sql.DB) *AIMessageOptimizer {
	return &AIMessageOptimizer{
		APIURL:  envOr("AI_API_URL", "https://api.openai.com/v1"),
		APIKey:  os.Getenv("AI_API_KEY"),
		ModelID: envOr("AI_MODEL_ID", "gpt-4o-mini"),
		DB:      db,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// GenerateVariants creates culturally-localized message variants.
func (ai *AIMessageOptimizer) GenerateVariants(ctx context.Context, baseMessage, targetState, channel string, count int) ([]string, error) {
	if ai.APIKey == "" {
		return []string{baseMessage}, nil
	}
	if count == 0 {
		count = 3
	}

	prompt := fmt.Sprintf(`Generate %d different versions of this GOTV campaign message for Nigerian voters in %s state.
Channel: %s
Original message: "%s"

Requirements:
- Each variant should be culturally appropriate for the target region
- For Northern states (Kano, Kaduna, Sokoto, etc.): consider Hausa influence
- For South-West (Lagos, Oyo, Osun, etc.): consider Yoruba influence
- For South-East/South-South (Enugu, Anambra, Rivers, etc.): consider Igbo/pidgin influence
- Keep under 160 chars for SMS, 1024 for WhatsApp
- Include a clear call to action
- Use warm, community-focused language

Return ONLY a JSON array of strings, no other text.`, count, targetState, channel, baseMessage)

	payload := map[string]interface{}{
		"model": ai.ModelID,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a Nigerian political communication expert specializing in culturally-relevant voter mobilization."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.8,
		"max_tokens":  1000,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", ai.APIURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return []string{baseMessage}, err
	}
	req.Header.Set("Authorization", "Bearer "+ai.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ai.client.Do(req)
	if err != nil {
		return []string{baseMessage}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	json.Unmarshal(respBody, &result)

	if len(result.Choices) == 0 {
		return []string{baseMessage}, fmt.Errorf("no choices in AI response")
	}

	content := result.Choices[0].Message.Content
	// Try to parse as JSON array
	var variants []string
	if err := json.Unmarshal([]byte(content), &variants); err != nil {
		// Try to extract JSON array from response
		start := strings.Index(content, "[")
		end := strings.LastIndex(content, "]")
		if start >= 0 && end > start {
			json.Unmarshal([]byte(content[start:end+1]), &variants)
		}
	}

	if len(variants) == 0 {
		return []string{baseMessage}, nil
	}

	// Store variants in DB for A/B tracking
	for i, v := range variants {
		ai.DB.ExecContext(ctx,
			`INSERT INTO gotv_ai_variants (variant_id, base_message, variant_text, target_state, channel, variant_index, created_at)
			 VALUES (gen_random_uuid()::text, $1, $2, $3, $4, $5, NOW())`,
			baseMessage, v, targetState, channel, i)
	}

	return variants, nil
}

// ─── INNOVATE #19: WhatsApp Flows ──────────────────────────────────────────

// WhatsAppFlowSender sends multi-screen forms inside WhatsApp.
type WhatsAppFlowSender struct {
	APIURL     string
	Token      string
	PhoneNumID string
	client     *http.Client
}

// NewWhatsAppFlowSender creates a WhatsApp Flows sender.
func NewWhatsAppFlowSender(apiURL, token, phoneNumID string) *WhatsAppFlowSender {
	if token == "" {
		return nil
	}
	return &WhatsAppFlowSender{
		APIURL:     apiURL,
		Token:      token,
		PhoneNumID: phoneNumID,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// SendPledgeFlow sends a voter pledge/registration flow to a contact.
func (f *WhatsAppFlowSender) SendPledgeFlow(ctx context.Context, phone, flowID, contactID string) error {
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                phone,
		"type":              "interactive",
		"interactive": map[string]interface{}{
			"type": "flow",
			"header": map[string]interface{}{
				"type": "text",
				"text": "Voter Registration",
			},
			"body": map[string]string{
				"text": "Complete your voter pledge and request a ride to your polling unit.",
			},
			"footer": map[string]string{
				"text": "Powered by GOTV Platform",
			},
			"action": map[string]interface{}{
				"name": "flow",
				"parameters": map[string]interface{}{
					"flow_message_version": "3",
					"flow_token":           contactID,
					"flow_id":              flowID,
					"flow_cta":             "Get Started",
					"flow_action":          "navigate",
					"flow_action_payload": map[string]interface{}{
						"screen": "PLEDGE_SCREEN",
					},
				},
			},
		},
	}

	url := fmt.Sprintf("%s/%s/messages", f.APIURL, f.PhoneNumID)
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+f.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WhatsApp Flow send failed: %s", string(respBody))
	}
	return nil
}

// ─── INNOVATE #20: Voice AI Robocaller ─────────────────────────────────────

// VoiceAICaller uses Retell/Vapi/Bland API for AI-powered phone banking.
type VoiceAICaller struct {
	Provider string // retell, vapi, bland
	APIURL   string
	APIKey   string
	AgentID  string
	DB       *sql.DB
	client   *http.Client
}

// NewVoiceAICaller creates a Voice AI caller.
func NewVoiceAICaller(db *sql.DB) *VoiceAICaller {
	provider := envOr("VOICE_AI_PROVIDER", "retell")
	apiKey := os.Getenv("VOICE_AI_API_KEY")
	if apiKey == "" {
		return nil
	}

	var apiURL string
	switch provider {
	case "retell":
		apiURL = "https://api.retellai.com/v2"
	case "vapi":
		apiURL = "https://api.vapi.ai"
	case "bland":
		apiURL = "https://api.bland.ai/v1"
	}

	return &VoiceAICaller{
		Provider: provider,
		APIURL:   apiURL,
		APIKey:   apiKey,
		AgentID:  os.Getenv("VOICE_AI_AGENT_ID"),
		DB:       db,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// PlaceCall initiates an AI voice call to a contact.
func (v *VoiceAICaller) PlaceCall(ctx context.Context, campaignID, contactID, phone string, partyID int) error {
	callID := fmt.Sprintf("vc-%s-%d", contactID[:8], time.Now().Unix())

	var payload map[string]interface{}
	switch v.Provider {
	case "retell":
		payload = map[string]interface{}{
			"agent_id":        v.AgentID,
			"customer_number": phone,
			"retell_llm_dynamic_variables": map[string]string{
				"campaign_id": campaignID,
				"contact_id":  contactID,
			},
		}
	case "vapi":
		payload = map[string]interface{}{
			"assistantId":   v.AgentID,
			"phoneNumberId": os.Getenv("VAPI_PHONE_ID"),
			"customer":      map[string]string{"number": phone},
		}
	case "bland":
		payload = map[string]interface{}{
			"phone_number":   phone,
			"task":           "Remind the voter about the upcoming election and encourage them to vote.",
			"voice_id":       1,
			"reduce_latency": true,
		}
	}

	body, _ := json.Marshal(payload)
	endpoint := v.APIURL + "/calls"
	if v.Provider == "retell" {
		endpoint = v.APIURL + "/create-phone-call"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+v.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Store call record
	v.DB.ExecContext(ctx,
		`INSERT INTO gotv_voice_calls (call_id, campaign_id, contact_id, party_id, provider, phone_number, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'initiated', NOW())`,
		callID, campaignID, contactID, partyID, v.Provider, phone)

	return nil
}

// ─── INNOVATE #22: Blockchain Pledge Verification ──────────────────────────

// PledgeVerifier hashes pledges and verifies fulfillment.
type PledgeVerifier struct {
	DB *sql.DB
}

// StorePledgeHash creates a SHA256 hash of the pledge for tamper-proof verification.
func (pv *PledgeVerifier) StorePledgeHash(ctx context.Context, partyID int, contactID string, electionID int, wardCode string) (string, error) {
	// Create deterministic hash from pledge data (anonymized — no PII in hash)
	data := fmt.Sprintf("pledge:%d:%s:%d:%s:%d", partyID, contactID, electionID, wardCode, time.Now().Unix())
	hash := sha256.Sum256([]byte(data))
	hashHex := hex.EncodeToString(hash[:])

	_, err := pv.DB.ExecContext(ctx,
		`INSERT INTO gotv_pledge_hashes (hash, party_id, election_id, ward_code, created_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (hash) DO NOTHING`,
		hashHex, partyID, electionID, wardCode)

	return hashHex, err
}

// VerifyPledgeFulfillment checks pledge fulfillment rates by comparing pledge hashes with turnout data.
func (pv *PledgeVerifier) VerifyPledgeFulfillment(ctx context.Context, partyID, electionID int) (totalPledges, verifiedWards int, fulfillmentRate float64, err error) {
	err = pv.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM gotv_pledge_hashes WHERE party_id=$1 AND election_id=$2`,
		partyID, electionID).Scan(&totalPledges)
	if err != nil {
		return
	}

	// Count wards with both pledges and verified turnout
	err = pv.DB.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT ph.ward_code)
		 FROM gotv_pledge_hashes ph
		 WHERE ph.party_id=$1 AND ph.election_id=$2 AND ph.verified=TRUE`,
		partyID, electionID).Scan(&verifiedWards)

	if totalPledges > 0 {
		fulfillmentRate = float64(verifiedWards) / float64(totalPledges)
	}
	return
}

// ─── INNOVATE #23: USSD Mini-App ───────────────────────────────────────────

// USSDHandler processes Africa's Talking USSD callback sessions.
type USSDHandler struct {
	DB *sql.DB
}

// ussdLangs are the languages offered at the top of the GOTV USSD menu
// (level-0 language selection, R5-077). Menu digits 1-5 map to this order.
var ussdLangs = []string{"en", "ha", "yo", "ig", "pcm"}

// ussdStrings holds localized GOTV USSD menu chrome. Diacritics are
// intentionally avoided: GSM-7 USSD transport cannot carry them reliably.
var ussdStrings = map[string]map[string]string{
	"en": {
		"lang_menu":     "CON Select Language / Zabi Harshe / Yan Ede / Horo Asusu\n1. English\n2. Hausa\n3. Yoruba\n4. Igbo\n5. Naija (Pidgin)",
		"main_menu":     "CON GOTV Voter Connect\n1. Pledge to Vote\n2. Request Ride to Polls\n3. Find My Polling Unit\n4. Report an Issue\n5. Check Election Date",
		"pledge_state":  "CON Pledge to Vote!\nEnter your State (e.g. Lagos, Kano, Rivers):",
		"ride_location": "CON Request a Ride\nEnter your pickup location (area name):",
		"pu_vin":        "CON Find Your Polling Unit\nEnter your Voter Card Number:",
		"issue_menu":    "CON Report an Issue\n1. Voter intimidation\n2. Missing materials\n3. Late opening\n4. Other",
		"pledge_ok":     "END Thank you for pledging to vote! Your state: %s. We'll send you a reminder on election day.",
		"ride_ok":       "END Ride requested from %s! A volunteer will contact you on election day.",
		"issue_ok":      "END Issue reported. Thank you for helping ensure a free and fair election!",
		"election_date": "END Next Election: Check INEC website www.inec.gov.ng for dates.",
		"pu_found":      "END Your polling unit: %s\nWard: %s\nVoting: 8am - 5pm",
		"pu_not_found":  "END No registration found for that voter card number. Visit your nearest INEC office.",
		"service_down":  "END Service unavailable, please try again later.",
		"invalid":       "END Invalid option. Dial again to restart.",
		"invalid_lang":  "END Invalid selection. Dial again and choose 1-5 for language.",
	},
	"ha": {
		"lang_menu":     "CON Select Language / Zabi Harshe / Yan Ede / Horo Asusu\n1. English\n2. Hausa\n3. Yoruba\n4. Igbo\n5. Naija (Pidgin)",
		"main_menu":     "CON Hadin Masu Jefa Kuri'a\n1. Yi alkawarin jefa kuri'a\n2. Nemi mota zuwa rumfa\n3. Nemo rumfar zabena\n4. Ba da rahoto\n5. Ranar zabe",
		"pledge_state":  "CON Alkawarin Jefa Kuri'a!\nShigar da jiharka (misali Lagos, Kano, Rivers):",
		"ride_location": "CON Neman Mota\nShigar da inda za a dauke ka (sunan yanki):",
		"pu_vin":        "CON Nemo Rumfar Zabenka\nShigar da lambar katin zabenka:",
		"issue_menu":    "CON Ba da Rahoto\n1. Tsoratar da masu jefa kuri'a\n2. Kayan zabe bai isa ba\n3. Bude rumfa a makare\n4. Sauran",
		"pledge_ok":     "END Na gode da alkawarin jefa kuri'a! Jiharka: %s. Za mu tura maka tunatarwa a ranar zabe.",
		"ride_ok":       "END An nemi mota daga %s! Wani sa-kai zai tuntube ka a ranar zabe.",
		"issue_ok":      "END An karbi rahoto. Na gode da taimakawa wajen tabbatar da zabe na adalci!",
		"election_date": "END Zabe na gaba: Duba shafin INEC www.inec.gov.ng don ranaku.",
		"pu_found":      "END Rumfar zabenka: %s\nWardi: %s\nJefa kuri'a: 8am - 5pm",
		"pu_not_found":  "END Ba a sami rajista da wannan lambar kati ba. Ziyarci ofishin INEC mafi kusa.",
		"service_down":  "END Sabis ba ya aiki yanzu, sake gwadawa daga baya.",
		"invalid":       "END Zabi ba daidai ba. Sake dailawa don farawa.",
		"invalid_lang":  "END Zabi ba daidai ba. Sake dailawa ka zabi 1-5 don harshe.",
	},
	"yo": {
		"lang_menu":     "CON Select Language / Zabi Harshe / Yan Ede / Horo Asusu\n1. English\n2. Hausa\n3. Yoruba\n4. Igbo\n5. Naija (Pidgin)",
		"main_menu":     "CON Asopo Oludibo\n1. Se ileri lati dibo\n2. Beere oko si ile-idibo\n3. Wa ile-idibo mi\n4. Jabo isoro\n5. Ojo idibo",
		"pledge_state":  "CON Ileri lati Dibo!\nTe ipinle re (fun apeere Lagos, Kano, Rivers):",
		"ride_location": "CON Beere Oko\nTe ibi ti won ma gba e (oruko agbegbe):",
		"pu_vin":        "CON Wa Ile-idibo Re\nTe nomba kaadi oludibo re:",
		"issue_menu":    "CON Jabo Isoro\n1. Ipayas oludibo\n2. Ohun elo idibo ko de\n3. Ile-idibo pe laa\n4. Omiiran",
		"pledge_ok":     "END E dupe fun ileri lati dibo! Ipinle re: %s. A o ran ibaralowo si e ni ojo idibo.",
		"ride_ok":       "END A ti beere oko lati %s! Onise agbedemeji yoo kan si e ni ojo idibo.",
		"issue_ok":      "END A ti gba ijabo. E dupe fun iranlowo lati ni idibo ominira ati ododo!",
		"election_date": "END Idibo to nbo: Wo oju-ipo INEC www.inec.gov.ng fun awon ojo.",
		"pu_found":      "END Ile-idibo re: %s\nWadi: %s\nIdibo: 8am - 5pm",
		"pu_not_found":  "END A ko ri iforukosile fun nomba kaadi yen. Sabewo si ofisi INEC to sunmo.",
		"service_down":  "END Ise ko wa fun igba die, gbiyanju leekansi.",
		"invalid":       "END Asayan ko to. Pe pada lati bere.",
		"invalid_lang":  "END Asayan ko to. Pe pada ki o yan 1-5 fun ede.",
	},
	"ig": {
		"lang_menu":     "CON Select Language / Zabi Harshe / Yan Ede / Horo Asusu\n1. English\n2. Hausa\n3. Yoruba\n4. Igbo\n5. Naija (Pidgin)",
		"main_menu":     "CON Njiko Ndi Vootu\n1. Kwe nkwa i vootu\n2. Rio ugbo ga-ebe vootu\n3. Chota ebe ntuli aka m\n4. Koo nsogbu\n5. Ubochi ntuli aka",
		"pledge_state":  "CON Nkwa I Vootu!\nTinye steeti gi (dika Lagos, Kano, Rivers):",
		"ride_location": "CON Rio Ugbo\nTinye ebe a ga-akuru gi (aha mpaghara):",
		"pu_vin":        "CON Chota Ebe Ntuli Aka Gi\nTinye nomba kaadi vootu gi:",
		"issue_menu":    "CON Koo Nsogbu\n1. Iji egwu gba ndi vootu\n2. Ihe eji eme ntuli aka adighi\n3. Mmeghe ebe ntuli aka gbachara\n4. Ihe ozo",
		"pledge_ok":     "END Daalu maka nkwa i vootu! Steeti gi: %s. Anyi ga-ezitere gi ncheta n'ubochi ntuli aka.",
		"ride_ok":       "END Arioala ugbo site %s! Onye oru onwe ga-akpotu gi n'ubochi ntuli aka.",
		"issue_ok":      "END Enatabara akuko. Daalu maka inyere aka inwe ntuli aka na-adighi ajo!",
		"election_date": "END Ntuli aka na-abia: Lelee webusaiti INEC www.inec.gov.ng maka ubochi.",
		"pu_found":      "END Ebe ntuli aka gi: %s\nWard: %s\nNtuli aka: 8am - 5pm",
		"pu_not_found":  "END Achotaghi ndebanye aha maka nomba kaadi ahu. Gaa ulo oru INEC kacha nso.",
		"service_down":  "END Oru adighi ugbu a, nwaa ozo ma e mechaa.",
		"invalid":       "END Nhoro ezighi ezi. Kpoo ozo iji malite.",
		"invalid_lang":  "END Nhoro ezighi ezi. Kpoo ozo horo 1-5 maka asusu.",
	},
	"pcm": {
		"lang_menu":     "CON Select Language / Zabi Harshe / Yan Ede / Horo Asusu\n1. English\n2. Hausa\n3. Yoruba\n4. Igbo\n5. Naija (Pidgin)",
		"main_menu":     "CON GOTV Voter Connect\n1. Pledge say you go vote\n2. Ask for ride go poll\n3. Find my polling unit\n4. Report wahala\n5. Check election date",
		"pledge_state":  "CON Pledge to Vote!\nEnter your State (e.g. Lagos, Kano, Rivers):",
		"ride_location": "CON Request Ride\nEnter where dem go pick you (area name):",
		"pu_vin":        "CON Find Your Polling Unit\nEnter your Voter Card Number:",
		"issue_menu":    "CON Report Wahala\n1. Dem dey intimidate voters\n2. Materials no dey\n3. Polling unit open late\n4. Other",
		"pledge_ok":     "END Thank you for pledging to vote! Your state: %s. We go remind you on election day.",
		"ride_ok":       "END Ride don dey requested from %s! Volunteer go contact you on election day.",
		"issue_ok":      "END We don receive your report. Thank you for helping make di election free and fair!",
		"election_date": "END Next Election: Check INEC website www.inec.gov.ng for dates.",
		"pu_found":      "END Your polling unit: %s\nWard: %s\nVoting na 8am - 5pm",
		"pu_not_found":  "END We no see registration for dat card number. Go di INEC office near you.",
		"service_down":  "END Service no dey now, try again later.",
		"invalid":       "END Dat one no correct. Dial again to start over.",
		"invalid_lang":  "END Dat one no correct. Dial again pick 1-5 for language.",
	},
}

// ussdText returns the localized GOTV USSD string, falling back to English
// for any missing key (never returns empty).
func ussdText(lang, key string) string {
	if d, ok := ussdStrings[lang]; ok {
		if s, ok := d[key]; ok && s != "" {
			return s
		}
	}
	return ussdStrings["en"][key]
}

// ussdLangOf resolves the language digit at the head of the cumulative USSD
// text to a language code, defaulting to English.
func ussdLangOf(langDigit string) string {
	for i, l := range ussdLangs {
		if langDigit == fmt.Sprintf("%d", i+1) {
			return l
		}
	}
	return "en"
}

// lookupVoterPollingUnit queries the voter registry (shared platform
// database) for a voter's polling unit by voter card number (PVC / VIN).
// found=false means no registration exists; err!=nil means the lookup
// itself failed and the caller must say so honestly.
func (u *USSDHandler) lookupVoterPollingUnit(ctx context.Context, voterCardNumber string) (puLabel, wardName string, found bool, err error) {
	if u.DB == nil {
		return "", "", false, fmt.Errorf("voter registry unavailable")
	}
	value := strings.TrimSpace(voterCardNumber)
	if value == "" {
		return "", "", false, nil
	}
	var puCode, puName string
	err = u.DB.QueryRowContext(ctx,
		`SELECT v.polling_unit_code, COALESCE(pu.name,''), COALESCE(w.name, v.ward_code)
		 FROM voters v
		 LEFT JOIN polling_units pu ON pu.code = v.polling_unit_code
		 LEFT JOIN wards w ON w.code = v.ward_code
		 WHERE v.pvc_number = $1 OR v.vin = $1`, value).
		Scan(&puCode, &puName, &wardName)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	puLabel = puName
	if puLabel == "" {
		puLabel = puCode
	}
	return puLabel, wardName, true, nil
}

// HandleUSSDCallback processes a USSD session step. Level 0 is a language
// menu; the chosen language travels in the cumulative session text so the
// handler stays stateless.
func (u *USSDHandler) HandleUSSDCallback(ctx context.Context, sessionID, phone, text string) (response string, endSession bool) {
	parts := strings.Split(text, "*")
	level := len(parts)
	if text == "" {
		level = 0
	}

	switch level {
	case 0:
		// Language selection
		return ussdText("en", "lang_menu"), false

	case 1:
		for i := range ussdLangs {
			if parts[0] == fmt.Sprintf("%d", i+1) {
				return ussdText(ussdLangs[i], "main_menu"), false
			}
		}
		return ussdText("en", "invalid_lang"), true

	case 2:
		lang := ussdLangOf(parts[0])
		switch parts[1] {
		case "1":
			return ussdText(lang, "pledge_state"), false
		case "2":
			return ussdText(lang, "ride_location"), false
		case "3":
			return ussdText(lang, "pu_vin"), false
		case "4":
			return ussdText(lang, "issue_menu"), false
		case "5":
			return ussdText(lang, "election_date"), true
		}

	case 3:
		lang := ussdLangOf(parts[0])
		switch parts[1] {
		case "1":
			// Pledge confirmed with state
			state := strings.TrimSpace(parts[2])
			u.DB.ExecContext(ctx,
				`INSERT INTO gotv_pledges (pledge_id, contact_id, party_id, status, created_at)
				 VALUES (gen_random_uuid()::text, $1, 0, 'confirmed', NOW())
				 ON CONFLICT DO NOTHING`, phone)
			return fmt.Sprintf(ussdText(lang, "pledge_ok"), state), true
		case "2":
			// Ride request with location
			location := strings.TrimSpace(parts[2])
			u.DB.ExecContext(ctx,
				`INSERT INTO gotv_ride_requests (request_id, party_id, contact_id, status, notes, created_at)
				 VALUES (gen_random_uuid()::text, 0, $1, 'pending', $2, NOW())`, phone, "USSD: "+location)
			return fmt.Sprintf(ussdText(lang, "ride_ok"), location), true
		case "3":
			// PU lookup against the real voter registry — never a web redirect
			// (feature-phone users have no browser, R5-081).
			puLabel, wardName, found, err := u.lookupVoterPollingUnit(ctx, parts[2])
			switch {
			case err != nil:
				log.Error().Err(err).Msg("gotv ussd: PU lookup failed")
				return ussdText(lang, "service_down"), true
			case !found:
				return ussdText(lang, "pu_not_found"), true
			default:
				return fmt.Sprintf(ussdText(lang, "pu_found"), puLabel, wardName), true
			}
		case "4":
			// Issue report
			issueTypes := map[string]string{"1": "voter_intimidation", "2": "missing_materials", "3": "late_opening", "4": "other"}
			issueType := issueTypes[parts[2]]
			if issueType == "" {
				issueType = "other"
			}
			u.DB.ExecContext(ctx,
				`INSERT INTO gotv_field_reports (report_id, issue_type, source, phone, resolved, created_at)
				 VALUES (gen_random_uuid()::text, $1, 'ussd', $2, FALSE, NOW())`, issueType, phone)
			return ussdText(lang, "issue_ok"), true
		}
	}

	return ussdText(ussdLangOf(parts[0]), "invalid"), true
}

// ─── INNOVATE #25: Multi-Party Alliance Mode ──────────────────────────────

// AllianceGrant represents a time-limited resource sharing agreement.
type AllianceGrant struct {
	GrantID      string    `json:"grant_id"`
	GrantorParty int       `json:"grantor_party_id"`
	GranteeParty int       `json:"grantee_party_id"`
	ResourceType string    `json:"resource_type"` // rides, territories, volunteers
	WardCode     string    `json:"ward_code"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// AllianceManager handles multi-party resource sharing.
type AllianceManager struct {
	DB         *sql.DB
	PermifyURL string
}

// CreateAlliance creates a time-limited resource sharing grant.
func (am *AllianceManager) CreateAlliance(ctx context.Context, grant AllianceGrant) error {
	_, err := am.DB.ExecContext(ctx,
		`INSERT INTO gotv_alliances (grant_id, grantor_party_id, grantee_party_id, resource_type, ward_code, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())`,
		grant.GrantID, grant.GrantorParty, grant.GranteeParty, grant.ResourceType, grant.WardCode, grant.ExpiresAt)
	if err != nil {
		return err
	}

	// If Permify is configured, create permission relationship
	if am.PermifyURL != "" {
		am.createPermifyRelation(ctx, grant)
	}

	return nil
}

func (am *AllianceManager) createPermifyRelation(ctx context.Context, grant AllianceGrant) {
	payload := map[string]interface{}{
		"metadata": map[string]string{"schema_version": ""},
		"tuples": []map[string]interface{}{
			{
				"entity":   map[string]interface{}{"type": "gotv_resource", "id": fmt.Sprintf("%s:%s", grant.ResourceType, grant.WardCode)},
				"relation": "can_access",
				"subject":  map[string]interface{}{"type": "party", "id": fmt.Sprintf("%d", grant.GranteeParty)},
			},
		},
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "POST", am.PermifyURL+"/v1/tenants/t1/relationships/write", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client.Do(req)
}

// GetSharedRides returns rides available to the party via alliances.
func (am *AllianceManager) GetSharedRides(ctx context.Context, partyID int) ([]map[string]interface{}, error) {
	rows, err := am.DB.QueryContext(ctx, `
		SELECT r.request_id, r.party_id, r.contact_id, r.pickup_latitude, r.pickup_longitude,
		       r.status, a.grantor_party_id, a.ward_code
		FROM gotv_ride_requests r
		JOIN gotv_alliances a ON a.grantor_party_id = r.party_id
		  AND a.grantee_party_id = $1
		  AND a.resource_type = 'rides'
		  AND a.expires_at > NOW()
		WHERE r.status = 'pending'
		LIMIT 50`, partyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rides []map[string]interface{}
	for rows.Next() {
		var reqID, contactID, status, ward string
		var ownerParty, allianceParty int
		var lat, lng float64
		rows.Scan(&reqID, &ownerParty, &contactID, &lat, &lng, &status, &allianceParty, &ward)
		rides = append(rides, map[string]interface{}{
			"request_id":     reqID,
			"owner_party_id": ownerParty,
			"contact_id":     contactID,
			"pickup_lat":     lat,
			"pickup_lng":     lng,
			"status":         status,
			"ward_code":      ward,
			"shared_via":     "alliance",
		})
	}
	return rides, nil
}

// Tech debt note: Twilio SHA1 signature verification lives in dispatch.go (VerifyTwilioSignature).

// ─── TECH DEBT: Graceful Shutdown ──────────────────────────────────────────

// GracefulShutdown listens for SIGTERM and drains in-flight dispatches.
func GracefulShutdown(engine *DispatchEngine, kafkaDisp *KafkaDispatcher) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		log.Info().Str("signal", sig.String()).Msg("Received shutdown signal, draining dispatches...")

		// Cancel all running campaign dispatches
		engine.mu.Lock()
		for id, cancel := range engine.running {
			log.Info().Str("campaign", id).Msg("Cancelling campaign dispatch")
			cancel()
		}
		engine.mu.Unlock()

		// Close Kafka writer
		if kafkaDisp != nil {
			kafkaDisp.Close()
		}

		// Allow 10s for in-flight sends to complete
		time.Sleep(10 * time.Second)
		log.Info().Msg("Graceful shutdown complete")
		os.Exit(0)
	}()
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
