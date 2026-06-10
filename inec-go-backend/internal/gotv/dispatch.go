// Package gotv — Campaign dispatch engine.
// Handles async message delivery via SMS, push, WhatsApp, USSD, and email
// with A/B variant routing, per-party rate limiting, and delivery tracking.
package gotv

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// ChannelAdapter sends messages via a specific channel (SMS, push, etc.).
type ChannelAdapter interface {
	Name() string
	Send(ctx context.Context, msg OutboundMessage) DeliveryResult
}

// OutboundMessage is a single outreach message to dispatch.
type OutboundMessage struct {
	CampaignID  string
	ContactID   string
	PartyID     int
	Phone       string
	FullName    string
	Template    string
	Variant     string // "a" or "b"
	Channel     string // sms, push, whatsapp, ussd, email
	ConsentID   string
}

// DeliveryResult captures the outcome of a single send attempt.
type DeliveryResult struct {
	Status    string // delivered, failed, pending, opted_out
	MessageID string
	Error     string
	Latency   time.Duration
}

// DispatchEngine runs campaign message delivery.
type DispatchEngine struct {
	db       *sql.DB
	adapters map[string]ChannelAdapter
	workers  int
	hub      *WSHub
	mu       sync.RWMutex
	running  map[string]context.CancelFunc // campaignID -> cancel
}

// NewDispatchEngine creates a dispatch engine with configured adapters.
func NewDispatchEngine(db *sql.DB, hub *WSHub, workers int) *DispatchEngine {
	if workers == 0 {
		workers = 10
	}
	return &DispatchEngine{
		db:       db,
		adapters: make(map[string]ChannelAdapter),
		workers:  workers,
		hub:      hub,
		running:  make(map[string]context.CancelFunc),
	}
}

// RegisterAdapter adds a channel adapter.
func (d *DispatchEngine) RegisterAdapter(adapter ChannelAdapter) {
	d.mu.Lock()
	d.adapters[adapter.Name()] = adapter
	d.mu.Unlock()
}

// LaunchCampaign starts async delivery for a campaign.
func (d *DispatchEngine) LaunchCampaign(ctx context.Context, campaignID string, partyID int) error {
	var channel, template, variantB string
	var abSplit int
	var rateLimit int

	err := d.db.QueryRowContext(ctx,
		`SELECT campaign_type, message_template, COALESCE(message_variant_b,''), ab_split_pct
		 FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2 AND status='active'`,
		campaignID, partyID,
	).Scan(&channel, &template, &variantB, &abSplit)
	if err != nil {
		return fmt.Errorf("campaign not found or not active: %w", err)
	}

	err = d.db.QueryRowContext(ctx,
		`SELECT COALESCE(rate_limit_per_hour, 1000) FROM gotv_party_access WHERE party_id=$1 AND is_active=TRUE`,
		partyID,
	).Scan(&rateLimit)
	if err != nil {
		rateLimit = 1000
	}

	d.mu.RLock()
	adapter, ok := d.adapters[channel]
	d.mu.RUnlock()
	if !ok {
		adapter = &LogAdapter{} // fallback: log-only adapter
	}

	campaignCtx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.running[campaignID] = cancel
	d.mu.Unlock()

	go d.dispatch(campaignCtx, campaignID, partyID, adapter, template, variantB, abSplit, rateLimit)

	return nil
}

// PauseCampaign cancels a running dispatch.
func (d *DispatchEngine) PauseCampaign(campaignID string) {
	d.mu.Lock()
	if cancel, ok := d.running[campaignID]; ok {
		cancel()
		delete(d.running, campaignID)
	}
	d.mu.Unlock()
}

func (d *DispatchEngine) dispatch(ctx context.Context, campaignID string, partyID int,
	adapter ChannelAdapter, template, variantB string, abSplit, rateLimit int) {

	log.Info().Str("campaign", campaignID).Int("party", partyID).
		Str("channel", adapter.Name()).Int("rate_limit", rateLimit).
		Msg("GOTV dispatch started")

	defer func() {
		d.mu.Lock()
		delete(d.running, campaignID)
		d.mu.Unlock()
		d.db.Exec("UPDATE gotv_campaigns SET status='completed', completed_at=NOW() WHERE campaign_id=$1", campaignID)
		if d.hub != nil {
			d.hub.Broadcast("campaign.progress", partyID, map[string]interface{}{
				"campaign_id": campaignID, "status": "completed",
			})
		}
	}()

	// Fetch eligible contacts (not opted out, with consent)
	rows, err := d.db.QueryContext(ctx,
		`SELECT c.contact_id, c.phone_encrypted, c.full_name_encrypted, c.consent_id
		 FROM gotv_contacts c
		 LEFT JOIN gotv_outreach_log o ON o.contact_id = c.contact_id AND o.campaign_id = $1
		 WHERE c.party_id = $2 AND c.opted_out = FALSE AND c.consent_id IS NOT NULL
		   AND o.id IS NULL
		 ORDER BY c.created_at
		 LIMIT 100000`,
		campaignID, partyID,
	)
	if err != nil {
		log.Error().Err(err).Str("campaign", campaignID).Msg("dispatch: failed to fetch contacts")
		return
	}
	defer rows.Close()

	type contactRow struct {
		ContactID     string
		PhoneEnc      string
		NameEnc       string
		ConsentID     string
	}
	var contacts []contactRow
	for rows.Next() {
		var c contactRow
		if err := rows.Scan(&c.ContactID, &c.PhoneEnc, &c.NameEnc, &c.ConsentID); err != nil {
			continue
		}
		contacts = append(contacts, c)
	}

	// Update total_contacts only if handler didn't already set it
	d.db.Exec("UPDATE gotv_campaigns SET total_contacts=$1 WHERE campaign_id=$2 AND (total_contacts IS NULL OR total_contacts=0)", len(contacts), campaignID)

	// Rate limiter: token bucket
	interval := time.Hour / time.Duration(rateLimit)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var reached, responded int64
	work := make(chan contactRow, d.workers*2)
	var wg sync.WaitGroup

	for i := 0; i < d.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				variant := "a"
				msgTemplate := template
				if variantB != "" {
					n, _ := rand.Int(rand.Reader, big.NewInt(100))
					if int(n.Int64()) >= abSplit {
						variant = "b"
						msgTemplate = variantB
					}
				}

				result := adapter.Send(ctx, OutboundMessage{
					CampaignID: campaignID,
					ContactID:  c.ContactID,
					PartyID:    partyID,
					Phone:      c.PhoneEnc,
					FullName:   c.NameEnc,
					Template:   msgTemplate,
					Variant:    variant,
					Channel:    adapter.Name(),
					ConsentID:  c.ConsentID,
				})

				d.db.Exec(
					`INSERT INTO gotv_outreach_log
					 (party_id, campaign_id, contact_id, channel, direction, status, message_variant, message_id, error_detail, latency_ms, created_at)
					 VALUES ($1,$2,$3,$4,'outbound',$5,$6,$7,$8,$9,NOW())`,
					partyID, campaignID, c.ContactID, adapter.Name(),
					result.Status, variant, result.MessageID, result.Error,
					result.Latency.Milliseconds(),
				)

				if result.Status == "delivered" || result.Status == "pending" {
					atomic.AddInt64(&reached, 1)
				}

				cur := atomic.LoadInt64(&reached)
				if cur%100 == 0 && d.hub != nil {
					d.hub.Broadcast("campaign.progress", partyID, map[string]interface{}{
						"campaign_id": campaignID,
						"reached":     cur,
						"total":       len(contacts),
					})
				}
			}
		}()
	}

	for _, c := range contacts {
		select {
		case <-ctx.Done():
			close(work)
			wg.Wait()
			d.db.Exec("UPDATE gotv_campaigns SET status='paused' WHERE campaign_id=$1", campaignID)
			return
		case <-ticker.C:
			work <- c
		}
	}
	close(work)
	wg.Wait()

	d.db.Exec("UPDATE gotv_campaigns SET contacts_reached=$1, contacts_responded=$2 WHERE campaign_id=$3",
		atomic.LoadInt64(&reached), atomic.LoadInt64(&responded), campaignID)
}

// ─── Channel Adapters ──────────────────────────────────────────────────────

// LogAdapter logs messages without sending (development/testing).
type LogAdapter struct{}

func (a *LogAdapter) Name() string { return "log" }
func (a *LogAdapter) Send(_ context.Context, msg OutboundMessage) DeliveryResult {
	log.Debug().Str("contact", msg.ContactID).Str("variant", msg.Variant).Msg("GOTV dispatch (log)")
	return DeliveryResult{Status: "delivered", MessageID: "log-" + msg.ContactID}
}

// SMSAdapter sends SMS via Africa's Talking or Twilio.
type SMSAdapter struct {
	Provider string // "africastalking" or "twilio"
	APIURL   string
	APIKey   string
	SenderID string
	client   *http.Client
}

func NewSMSAdapter(provider, apiURL, apiKey, senderID string) *SMSAdapter {
	return &SMSAdapter{
		Provider: provider,
		APIURL:   apiURL,
		APIKey:   apiKey,
		SenderID: senderID,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *SMSAdapter) Name() string { return "sms" }

func (a *SMSAdapter) Send(ctx context.Context, msg OutboundMessage) DeliveryResult {
	start := time.Now()

	var reqBody string
	var req *http.Request
	var err error

	switch a.Provider {
	case "africastalking":
		reqBody = fmt.Sprintf("username=sandbox&to=%s&message=%s&from=%s",
			msg.Phone, msg.Template, a.SenderID)
		req, err = http.NewRequestWithContext(ctx, "POST", a.APIURL+"/messaging", strings.NewReader(reqBody))
		if err != nil {
			return DeliveryResult{Status: "failed", Error: err.Error(), Latency: time.Since(start)}
		}
		req.Header.Set("apiKey", a.APIKey)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	case "twilio":
		reqBody = fmt.Sprintf("To=%s&From=%s&Body=%s", msg.Phone, a.SenderID, msg.Template)
		req, err = http.NewRequestWithContext(ctx, "POST", a.APIURL, strings.NewReader(reqBody))
		if err != nil {
			return DeliveryResult{Status: "failed", Error: err.Error(), Latency: time.Since(start)}
		}
		req.SetBasicAuth(a.SenderID, a.APIKey)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	default:
		return DeliveryResult{Status: "failed", Error: "unknown SMS provider: " + a.Provider, Latency: time.Since(start)}
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return DeliveryResult{Status: "failed", Error: err.Error(), Latency: time.Since(start)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return DeliveryResult{Status: "pending", MessageID: resp.Header.Get("X-Message-ID"), Latency: time.Since(start)}
	}
	return DeliveryResult{Status: "failed", Error: fmt.Sprintf("HTTP %d", resp.StatusCode), Latency: time.Since(start)}
}

// PushAdapter sends push notifications via FCM.
type PushAdapter struct {
	ServerKey  string
	ProjectID  string
	client     *http.Client
}

func NewPushAdapter(serverKey, projectID string) *PushAdapter {
	return &PushAdapter{
		ServerKey: serverKey,
		ProjectID: projectID,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *PushAdapter) Name() string { return "push" }

func (a *PushAdapter) Send(ctx context.Context, msg OutboundMessage) DeliveryResult {
	start := time.Now()
	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"topic": fmt.Sprintf("gotv-party-%d", msg.PartyID),
			"notification": map[string]string{
				"title": "Election Day Reminder",
				"body":  msg.Template,
			},
			"data": map[string]string{
				"campaign_id": msg.CampaignID,
				"contact_id":  msg.ContactID,
				"variant":     msg.Variant,
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", a.ProjectID),
		strings.NewReader(string(body)))
	if err != nil {
		return DeliveryResult{Status: "failed", Error: err.Error(), Latency: time.Since(start)}
	}
	req.Header.Set("Authorization", "Bearer "+a.ServerKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return DeliveryResult{Status: "failed", Error: err.Error(), Latency: time.Since(start)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return DeliveryResult{Status: "pending", Latency: time.Since(start)}
	}
	return DeliveryResult{Status: "failed", Error: fmt.Sprintf("FCM HTTP %d", resp.StatusCode), Latency: time.Since(start)}
}

// WhatsAppAdapter sends via WhatsApp Business API.
type WhatsAppAdapter struct {
	APIURL     string
	Token      string
	PhoneNumID string
	client     *http.Client
}

func NewWhatsAppAdapter(apiURL, token, phoneNumID string) *WhatsAppAdapter {
	return &WhatsAppAdapter{
		APIURL:     apiURL,
		Token:      token,
		PhoneNumID: phoneNumID,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *WhatsAppAdapter) Name() string { return "whatsapp" }

func (a *WhatsAppAdapter) Send(ctx context.Context, msg OutboundMessage) DeliveryResult {
	start := time.Now()
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                msg.Phone,
		"type":              "text",
		"text":              map[string]string{"body": msg.Template},
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/%s/messages", a.APIURL, a.PhoneNumID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return DeliveryResult{Status: "failed", Error: err.Error(), Latency: time.Since(start)}
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return DeliveryResult{Status: "failed", Error: err.Error(), Latency: time.Since(start)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return DeliveryResult{Status: "pending", Latency: time.Since(start)}
	}
	return DeliveryResult{Status: "failed", Error: fmt.Sprintf("WhatsApp HTTP %d", resp.StatusCode), Latency: time.Since(start)}
}
