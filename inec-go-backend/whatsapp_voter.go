package main

// whatsapp_voter.go — Neutral INEC voter self-service over WhatsApp
// (R5-078 / H4-08).
//
// The only two-way WhatsApp bot in the platform lived in the GOTV (party
// campaign) service; INEC voters had nothing on Nigeria's dominant channel.
// This is a Meta WhatsApp Business Cloud API webhook for neutral voter
// intents: check registration, find polling unit, report an incident, help.
//
// Security (fail closed):
//   - GET verification requires WHATSAPP_VERIFY_TOKEN to be configured and
//     match hub.verify_token; otherwise 503/403.
//   - POST payloads require an X-Hub-Signature-256 HMAC over the raw body
//     with WHATSAPP_APP_SECRET (falling back to TELCO_WEBHOOK_SECRET); if no
//     secret is configured the endpoint is disabled (503), never open.
//
// Outbound replies are sent via the Meta Graph API when WHATSAPP_API_TOKEN
// and WHATSAPP_PHONE_NUMBER_ID are configured; every inbound message and the
// computed reply are also persisted to `whatsapp_messages` (migration
// 000036) so nothing is lost when the API credential is absent.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// waWebhook is the subset of the Meta WhatsApp webhook payload we consume.
type waWebhook struct {
	Entry []struct {
		Changes []struct {
			Value struct {
				Messages []struct {
					From string `json:"from"`
					ID   string `json:"id"`
					Type string `json:"type"`
					Text struct {
						Body string `json:"body"`
					} `json:"text"`
				} `json:"messages"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

func whatsappAppSecret() string {
	if s := strings.TrimSpace(os.Getenv("WHATSAPP_APP_SECRET")); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv(telcoWebhookSecretEnv))
}

// handleWhatsAppVoterWebhook serves both the Meta webhook verification
// handshake (GET) and inbound message delivery (POST).
// Route registration (W2, main.go):
//
//	r.HandleFunc("/webhooks/whatsapp", handleWhatsAppVoterWebhook).Methods("GET", "POST")
func handleWhatsAppVoterWebhook(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		verifyToken := strings.TrimSpace(os.Getenv("WHATSAPP_VERIFY_TOKEN"))
		if verifyToken == "" {
			writeError(w, 503, "whatsapp verification is not configured")
			return
		}
		if r.URL.Query().Get("hub.mode") != "subscribe" ||
			r.URL.Query().Get("hub.verify_token") != verifyToken {
			writeError(w, 403, "whatsapp webhook verification failed")
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		io.WriteString(w, r.URL.Query().Get("hub.challenge"))
		return
	case http.MethodPost:
		// handled below
	default:
		writeError(w, 405, "method not allowed")
		return
	}

	secret := whatsappAppSecret()
	if secret == "" {
		writeError(w, 503, "whatsapp webhook verification is not configured")
		return
	}
	body, ok := readAndRestoreBody(w, r)
	if !ok {
		return
	}
	signature := strings.TrimPrefix(strings.TrimSpace(r.Header.Get("X-Hub-Signature-256")), "sha256=")
	if !verifyProviderSignature(secret, string(body), signature) {
		writeError(w, 401, "invalid whatsapp webhook signature")
		return
	}

	var payload waWebhook
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, 400, "invalid whatsapp payload")
		return
	}

	processed := 0
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if msg.Type != "text" || strings.TrimSpace(msg.Text.Body) == "" {
					continue
				}
				reply := processWhatsAppVoterMessage(msg.From, msg.Text.Body)
				persistWhatsAppMessage(msg.ID, msg.From, msg.Text.Body, reply)
				if err := sendWhatsAppReply(msg.From, reply); err != nil {
					// Delivery depends on external Meta credentials; the reply
					// is already persisted for audit/replay, so log and continue.
					log.Warn().Err(err).Str("to", msg.From).Msg("whatsapp: outbound reply not delivered via API")
				}
				processed++
			}
		}
	}
	writeJSON(w, 200, M{"status": "ok", "processed": processed})
}

// processWhatsAppVoterMessage maps a voter's keyword to a real registry/
// pipeline action. All lookups are live; nothing is fabricated.
//
// Keywords (case-insensitive):
//
//	REGISTER <voter-card-number>  — registration status
//	PU <NIN-or-voter-card-number> — find polling unit
//	REPORT <description>          — file an incident into triage
//	HELP                          — usage text
func processWhatsAppVoterMessage(from, text string) string {
	msg := strings.TrimSpace(text)
	upper := strings.ToUpper(msg)

	switch {
	case strings.HasPrefix(upper, "REGISTER "):
		value := strings.TrimSpace(msg[len("REGISTER "):])
		status, wardName, lgaCode, found, err := lookupVoterRegistration(value)
		switch {
		case err != nil:
			log.Error().Err(err).Msg("whatsapp: registration lookup failed")
			return "Service temporarily unavailable. Please try again later."
		case !found:
			return "No registration found for that voter card number. Please visit your nearest INEC office with your National ID."
		default:
			return fmt.Sprintf("Registration found (status: %s).\nWard: %s\nLGA: %s", status, wardName, lgaCode)
		}

	case strings.HasPrefix(upper, "PU "):
		value := strings.TrimSpace(msg[len("PU "):])
		// Try NIN first, then voter card number.
		puCode, puName, wardName, found, err := lookupVoterPU(true, value)
		if err == nil && !found {
			puCode, puName, wardName, found, err = lookupVoterPU(false, value)
		}
		switch {
		case err != nil:
			log.Error().Err(err).Msg("whatsapp: PU lookup failed")
			return "Service temporarily unavailable. Please try again later."
		case !found:
			return "No registration found. Please check the number and try again, or visit your nearest INEC office."
		default:
			label := puName
			if label == "" {
				label = puCode
			}
			return fmt.Sprintf("Your polling unit: %s\nWard: %s\nVoting: 8am - 5pm", label, wardName)
		}

	case strings.HasPrefix(upper, "REPORT "):
		description := strings.TrimSpace(msg[len("REPORT "):])
		ref, err := persistPublicIncident("whatsapp_report", description, "medium", "", from, "", "", "whatsapp")
		if err != nil {
			log.Error().Err(err).Msg("whatsapp: incident persistence failed")
			return "We could not record your report right now — it was NOT saved. Please try again later."
		}
		return fmt.Sprintf("Your incident report has been recorded.\nReference: %s\nThank you for protecting Nigeria's democracy.", ref)

	default:
		return "INEC Voter Services\n\nSend:\nREGISTER <voter card number> - check your registration\nPU <NIN or voter card number> - find your polling unit\nREPORT <description> - report an election incident\nHELP - show this message"
	}
}

// persistWhatsAppMessage stores the inbound message and the computed reply
// for audit and offline delivery reconciliation.
func persistWhatsAppMessage(messageID, from, inbound, reply string) {
	if db == nil {
		return
	}
	if messageID == "" {
		messageID = fmt.Sprintf("local-%d", time.Now().UnixNano())
	}
	dbExecLog("db_op", `INSERT INTO whatsapp_messages (message_id, sender, direction, body, reply, created_at)
		VALUES (?,?,?,?,?,?) ON CONFLICT (message_id) DO NOTHING`,
		messageID, from, "inbound", inbound, reply, time.Now().UTC())
}

// sendWhatsAppReply delivers a text reply through the Meta Graph API. It
// returns an error when API credentials are not configured (reply remains
// persisted in whatsapp_messages for audit).
func sendWhatsAppReply(to, body string) error {
	token := strings.TrimSpace(os.Getenv("WHATSAPP_API_TOKEN"))
	phoneNumberID := strings.TrimSpace(os.Getenv("WHATSAPP_PHONE_NUMBER_ID"))
	if token == "" || phoneNumberID == "" {
		return fmt.Errorf("whatsapp API credentials not configured")
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]string{"body": body},
	})
	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", phoneNumberID),
		bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("whatsapp API returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
