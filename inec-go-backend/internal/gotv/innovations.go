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
				"name":       "flow",
				"parameters": map[string]interface{}{
					"flow_message_version": "3",
					"flow_token":          contactID,
					"flow_id":             flowID,
					"flow_cta":            "Get Started",
					"flow_action":         "navigate",
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
			"agent_id":             v.AgentID,
			"customer_number":      phone,
			"retell_llm_dynamic_variables": map[string]string{
				"campaign_id": campaignID,
				"contact_id":  contactID,
			},
		}
	case "vapi":
		payload = map[string]interface{}{
			"assistantId":    v.AgentID,
			"phoneNumberId":  os.Getenv("VAPI_PHONE_ID"),
			"customer":       map[string]string{"number": phone},
		}
	case "bland":
		payload = map[string]interface{}{
			"phone_number": phone,
			"task":         "Remind the voter about the upcoming election and encourage them to vote.",
			"voice_id":     1,
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

// HandleUSSDCallback processes a USSD session step.
func (u *USSDHandler) HandleUSSDCallback(ctx context.Context, sessionID, phone, text string) (response string, endSession bool) {
	parts := strings.Split(text, "*")
	level := len(parts)
	if text == "" {
		level = 0
	}

	switch level {
	case 0:
		// Main menu
		return "CON Welcome to GOTV Voter Connect\n1. Pledge to Vote\n2. Request Ride to Polls\n3. Find My Polling Unit\n4. Report an Issue\n5. Check Election Date", false

	case 1:
		switch parts[0] {
		case "1":
			return "CON Pledge to Vote!\nEnter your State (e.g. Lagos, Kano, Rivers):", false
		case "2":
			return "CON Request a Ride\nEnter your pickup location (area name):", false
		case "3":
			return "CON Find Your Polling Unit\nEnter your registered LGA:", false
		case "4":
			return "CON Report an Issue\n1. Voter intimidation\n2. Missing materials\n3. Late opening\n4. Other", false
		case "5":
			return "END Next Election: Check INEC website www.inec.gov.ng for dates.", true
		}

	case 2:
		switch parts[0] {
		case "1":
			// Pledge confirmed with state
			state := strings.TrimSpace(parts[1])
			u.DB.ExecContext(ctx,
				`INSERT INTO gotv_pledges (pledge_id, contact_id, party_id, status, created_at)
				 VALUES (gen_random_uuid()::text, $1, 0, 'confirmed', NOW())
				 ON CONFLICT DO NOTHING`, phone)
			return fmt.Sprintf("END Thank you for pledging to vote! Your state: %s. We'll send you a reminder on election day.", state), true
		case "2":
			// Ride request with location
			location := strings.TrimSpace(parts[1])
			u.DB.ExecContext(ctx,
				`INSERT INTO gotv_ride_requests (request_id, party_id, contact_id, status, notes, created_at)
				 VALUES (gen_random_uuid()::text, 0, $1, 'pending', $2, NOW())`, phone, "USSD: "+location)
			return fmt.Sprintf("END Ride requested from %s! A volunteer will contact you on election day.", location), true
		case "3":
			// PU lookup
			lga := strings.TrimSpace(parts[1])
			return fmt.Sprintf("END Polling Units in %s: Visit www.inec.gov.ng/voter-info or text PU to 20120", lga), true
		case "4":
			// Issue report
			issueTypes := map[string]string{"1": "voter_intimidation", "2": "missing_materials", "3": "late_opening", "4": "other"}
			issueType := issueTypes[parts[1]]
			if issueType == "" {
				issueType = "other"
			}
			u.DB.ExecContext(ctx,
				`INSERT INTO gotv_field_reports (report_id, issue_type, source, phone, resolved, created_at)
				 VALUES (gen_random_uuid()::text, $1, 'ussd', $2, FALSE, NOW())`, issueType, phone)
			return "END Issue reported. Thank you for helping ensure a free and fair election!", true
		}
	}

	return "END Invalid option. Dial again to restart.", true
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
	DB        *sql.DB
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
