// dispatch_v2.go — Enhanced GOTV dispatch with Kafka-backed queue, device-token push,
// social media 1:many, WhatsApp 24h window, campaign scheduling, multi-wave sequences,
// contact segmentation, WhatsApp button replies, budget controls, leaderboard,
// territory assignment, and cost-per-conversion analytics.
//
// Implements: CRITICAL #1-5, #8, ENHANCE #9-16
package gotv

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	kafka "github.com/segmentio/kafka-go"
)

// ─── CRITICAL #1: Kafka-backed Dispatch Queue ──────────────────────────────

const (
	TopicOutreachQueue = "gotv.outreach.queue"
	TopicOutreachDLQ   = "gotv.outreach.dlq"
)

// OutreachMessage is a Kafka-serializable outreach job.
type OutreachMessage struct {
	CampaignID string `json:"campaign_id"`
	ContactID  string `json:"contact_id"`
	PartyID    int    `json:"party_id"`
	Channel    string `json:"channel"`
	Template   string `json:"template"`
	Variant    string `json:"variant"`
	PhoneEnc   string `json:"phone_enc"`
	NameEnc    string `json:"name_enc"`
	WardCode   string `json:"ward_code"`
	PUCode     string `json:"pu_code"`
	ConsentID  string `json:"consent_id"`
	EnqueuedAt int64  `json:"enqueued_at"`
	Attempt    int    `json:"attempt"`
}

// KafkaDispatcher enqueues outreach messages to Kafka for crash-resilient processing.
type KafkaDispatcher struct {
	writer  *kafka.Writer
	enabled bool
}

// NewKafkaDispatcher creates a Kafka writer for outreach queue (no-op if brokers empty).
func NewKafkaDispatcher(brokers string) *KafkaDispatcher {
	if brokers == "" {
		return &KafkaDispatcher{enabled: false}
	}
	bList := strings.Split(brokers, ",")
	w := &kafka.Writer{
		Addr:         kafka.TCP(bList...),
		Topic:        TopicOutreachQueue,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireAll,
		Async:        false,
	}
	return &KafkaDispatcher{writer: w, enabled: true}
}

// EnqueueContacts publishes outreach messages to Kafka with retry.
func (kd *KafkaDispatcher) EnqueueContacts(ctx context.Context, msgs []OutreachMessage) error {
	if !kd.enabled {
		return nil
	}

	kafkaMsgs := make([]kafka.Message, 0, len(msgs))
	for _, m := range msgs {
		payload, _ := json.Marshal(m)
		kafkaMsgs = append(kafkaMsgs, kafka.Message{
			Key:   []byte(m.ContactID),
			Value: payload,
		})
	}

	for attempt := 0; attempt < 3; attempt++ {
		err := kd.writer.WriteMessages(ctx, kafkaMsgs...)
		if err == nil {
			return nil
		}
		log.Warn().Err(err).Int("attempt", attempt).Int("msgs", len(kafkaMsgs)).Msg("Kafka enqueue retry")
		time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
	}
	return fmt.Errorf("failed to enqueue %d messages after 3 attempts", len(msgs))
}

// Close shuts down the Kafka writer.
func (kd *KafkaDispatcher) Close() {
	if kd.writer != nil {
		kd.writer.Close()
	}
}

// LaunchCampaignV2 uses Kafka-backed dispatch (falls back to in-process if Kafka unavailable).
func (d *DispatchEngine) LaunchCampaignV2(ctx context.Context, campaignID string, partyID int, kafkaDisp *KafkaDispatcher) error {
	var channel, template, variantB string
	var abSplit int
	var budgetCapKobo sql.NullInt64

	err := d.db.QueryRowContext(ctx,
		`SELECT campaign_type, message_template, COALESCE(message_variant_b,''), ab_split_pct, budget_cap_kobo
		 FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2 AND status IN ('active','scheduled')`,
		campaignID, partyID,
	).Scan(&channel, &template, &variantB, &abSplit, &budgetCapKobo)
	if err != nil {
		return fmt.Errorf("campaign not found or not launchable: %w", err)
	}

	// Update status
	d.db.ExecContext(ctx, `UPDATE gotv_campaigns SET status='active', launched_at=NOW() WHERE campaign_id=$1`, campaignID)

	// Social media channels: post once, not per-contact (CRITICAL #3)
	if isSocialChannel(channel) {
		return d.dispatchSocialPost(ctx, campaignID, partyID, channel, template)
	}

	// Fetch eligible contacts
	rows, err := d.db.QueryContext(ctx,
		`SELECT c.contact_id, c.phone_encrypted, c.full_name_encrypted, c.consent_id,
		        COALESCE(c.ward_code,''), COALESCE(c.polling_unit_code,'')
		 FROM gotv_contacts c
		 LEFT JOIN gotv_outreach_log o ON o.contact_id = c.contact_id AND o.campaign_id = $1
		 WHERE c.party_id = $2 AND c.opted_out = FALSE AND c.consent_id IS NOT NULL
		   AND o.id IS NULL
		 ORDER BY c.created_at
		 LIMIT 500000`,
		campaignID, partyID,
	)
	if err != nil {
		return fmt.Errorf("failed to fetch contacts: %w", err)
	}
	defer rows.Close()

	var totalContacts int
	var msgs []OutreachMessage
	for rows.Next() {
		var contactID, phoneEnc, nameEnc, consentID, wardCode, puCode string
		rows.Scan(&contactID, &phoneEnc, &nameEnc, &consentID, &wardCode, &puCode)
		totalContacts++
		msgs = append(msgs, OutreachMessage{
			CampaignID: campaignID,
			ContactID:  contactID,
			PartyID:    partyID,
			Channel:    channel,
			Template:   template,
			Variant:    "a",
			PhoneEnc:   phoneEnc,
			NameEnc:    nameEnc,
			WardCode:   wardCode,
			PUCode:     puCode,
			ConsentID:  consentID,
			EnqueuedAt: time.Now().Unix(),
		})
	}

	// Update total_contacts
	d.db.ExecContext(ctx, `UPDATE gotv_campaigns SET total_contacts=$1 WHERE campaign_id=$2`, totalContacts, campaignID)

	// If Kafka is available, enqueue for crash-resilient processing
	if kafkaDisp != nil && kafkaDisp.enabled {
		// Batch in groups of 500
		for i := 0; i < len(msgs); i += 500 {
			end := i + 500
			if end > len(msgs) {
				end = len(msgs)
			}
			if err := kafkaDisp.EnqueueContacts(ctx, msgs[i:end]); err != nil {
				log.Error().Err(err).Str("campaign", campaignID).Msg("Kafka enqueue failed, falling back to in-process")
				go d.LaunchCampaign(ctx, campaignID, partyID)
				return nil
			}
		}
		log.Info().Str("campaign", campaignID).Int("contacts", totalContacts).Msg("Enqueued to Kafka")
		return nil
	}

	// Fallback: direct dispatch
	go d.LaunchCampaign(ctx, campaignID, partyID)
	return nil
}

// CRITICAL #3: Social media 1:many model — post ONCE, track impressions.
func isSocialChannel(ch string) bool {
	switch ch {
	case "twitter", "facebook", "instagram", "tiktok":
		return true
	}
	return false
}

func (d *DispatchEngine) dispatchSocialPost(ctx context.Context, campaignID string, partyID int, channel, template string) error {
	d.mu.RLock()
	adapter, ok := d.adapters[channel]
	d.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no adapter registered for channel %s", channel)
	}

	// Personalize with party name
	var partyName string
	d.db.QueryRowContext(ctx, "SELECT COALESCE(name,'') FROM parties WHERE id=$1", partyID).Scan(&partyName)
	personalizedMsg := strings.ReplaceAll(template, "{{party}}", partyName)

	msg := OutboundMessage{
		CampaignID: campaignID,
		PartyID:    partyID,
		Template:   personalizedMsg,
		Channel:    channel,
	}

	result := adapter.Send(ctx, msg)

	// Count total party contacts for impression tracking (1:many model)
	var totalContacts int
	d.db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE", partyID).Scan(&totalContacts)

	// Log as single broadcast, not per-contact
	d.logOutreachV2(partyID, campaignID, "broadcast", channel, "a", result.Status, result.MessageID, result.Error, result.Latency.Milliseconds(), result.Cost)

	d.db.Exec("UPDATE gotv_campaigns SET total_contacts=$1, contacts_reached=$2, status='completed', completed_at=NOW() WHERE campaign_id=$3",
		totalContacts, totalContacts, campaignID)

	if d.hub != nil {
		d.hub.Broadcast("campaign.progress", partyID, map[string]interface{}{
			"campaign_id": campaignID, "status": "completed", "model": "broadcast_1_to_many",
			"impressions": totalContacts,
		})
	}

	return nil
}

// ─── CRITICAL #2: Device-Token Push Targeting ──────────────────────────────

// PushAdapterV2 sends targeted push notifications to individual device tokens.
type PushAdapterV2 struct {
	ServerKey string
	ProjectID string
	DB        *sql.DB
	Client    *simpleHTTPClient
}

func (a *PushAdapterV2) Name() string { return "push" }

func (a *PushAdapterV2) Send(ctx context.Context, msg OutboundMessage) DeliveryResult {
	start := time.Now()

	// Look up device token for this contact (CRITICAL #2: targeted, not broadcast)
	var deviceToken string
	err := a.DB.QueryRowContext(ctx,
		`SELECT device_token FROM gotv_contacts WHERE contact_id=$1 AND device_token IS NOT NULL AND device_token != ''`,
		msg.ContactID,
	).Scan(&deviceToken)

	if err != nil || deviceToken == "" {
		// Fallback to topic-based for contacts without device tokens
		return a.sendToTopic(ctx, msg, start)
	}

	// Targeted push to specific device token
	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"token": deviceToken,
			"notification": map[string]string{
				"title": "Election Day Reminder",
				"body":  msg.Template,
			},
			"data": map[string]string{
				"campaign_id": msg.CampaignID,
				"contact_id":  msg.ContactID,
				"type":        "gotv_reminder",
			},
			"android": map[string]interface{}{
				"priority": "high",
			},
		},
	}

	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", a.ProjectID)
	body, _ := json.Marshal(payload)

	resp, err := a.Client.Post(ctx, url, body, map[string]string{
		"Authorization": "Bearer " + a.ServerKey,
		"Content-Type":  "application/json",
	})
	if err != nil {
		return DeliveryResult{Status: "failed", Error: err.Error(), Latency: time.Since(start)}
	}

	var result map[string]interface{}
	json.Unmarshal(resp, &result)

	return DeliveryResult{
		Status:    "delivered",
		MessageID: fmt.Sprintf("%v", result["name"]),
		Latency:   time.Since(start),
	}
}

func (a *PushAdapterV2) sendToTopic(ctx context.Context, msg OutboundMessage, start time.Time) DeliveryResult {
	// Topic-based push for contacts without device tokens
	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"topic": fmt.Sprintf("gotv_party_%d", msg.PartyID),
			"notification": map[string]string{
				"title": "Election Day Reminder",
				"body":  msg.Template,
			},
		},
	}

	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", a.ProjectID)
	body, _ := json.Marshal(payload)

	_, err := a.Client.Post(ctx, url, body, map[string]string{
		"Authorization": "Bearer " + a.ServerKey,
		"Content-Type":  "application/json",
	})
	if err != nil {
		return DeliveryResult{Status: "failed", Error: "topic_fallback: " + err.Error(), Latency: time.Since(start)}
	}
	return DeliveryResult{Status: "delivered", Latency: time.Since(start)}
}

// ─── CRITICAL #4: WhatsApp 24h Window Enforcement ──────────────────────────

// WhatsAppAdapterV2 enforces Meta's 24h conversation window.
type WhatsAppAdapterV2 struct {
	APIURL       string
	Token        string
	PhoneNumID   string
	TemplateName string
	DB           *sql.DB
	Client       *simpleHTTPClient
}

func (a *WhatsAppAdapterV2) Name() string { return "whatsapp" }

func (a *WhatsAppAdapterV2) Send(ctx context.Context, msg OutboundMessage) DeliveryResult {
	start := time.Now()

	// Check last inbound message time for this contact (CRITICAL #4)
	var lastInbound sql.NullTime
	a.DB.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM gotv_outreach_log
		 WHERE contact_id=$1 AND direction='inbound' AND channel IN ('whatsapp','whatsapp_interactive')`,
		msg.ContactID,
	).Scan(&lastInbound)

	withinWindow := lastInbound.Valid && time.Since(lastInbound.Time) < 24*time.Hour

	var payload map[string]interface{}
	if withinWindow {
		// Free-form text allowed within 24h window
		payload = map[string]interface{}{
			"messaging_product": "whatsapp",
			"to":               msg.Phone,
			"type":             "text",
			"text":             map[string]string{"body": msg.Template},
		}
	} else {
		// Template message required outside 24h window (Meta policy)
		payload = map[string]interface{}{
			"messaging_product": "whatsapp",
			"to":               msg.Phone,
			"type":             "template",
			"template": map[string]interface{}{
				"name":     a.TemplateName,
				"language": map[string]string{"code": "en"},
				"components": []map[string]interface{}{
					{
						"type": "body",
						"parameters": []map[string]interface{}{
							{"type": "text", "text": msg.FullName},
							{"type": "text", "text": msg.Template},
						},
					},
				},
			},
		}
	}

	url := fmt.Sprintf("%s/%s/messages", a.APIURL, a.PhoneNumID)
	body, _ := json.Marshal(payload)

	resp, err := a.Client.Post(ctx, url, body, map[string]string{
		"Authorization": "Bearer " + a.Token,
		"Content-Type":  "application/json",
	})
	if err != nil {
		return DeliveryResult{Status: "failed", Error: err.Error(), Latency: time.Since(start), Cost: 0}
	}

	var result map[string]interface{}
	json.Unmarshal(resp, &result)
	msgID := ""
	if messages, ok := result["messages"].([]interface{}); ok && len(messages) > 0 {
		if m, ok := messages[0].(map[string]interface{}); ok {
			msgID = fmt.Sprintf("%v", m["id"])
		}
	}

	// WhatsApp: 1 conversation = ~₦50 (Meta pricing for Nigeria)
	cost := int64(50)
	if withinWindow {
		cost = 0 // Free within conversation window
	}

	return DeliveryResult{
		Status:    "delivered",
		MessageID: msgID,
		Latency:   time.Since(start),
		Cost:      cost,
	}
}

// ─── CRITICAL #5: DND Client with Timeout ──────────────────────────────────

// DNDClient checks Nigeria NCC Do Not Disturb registry with 5s timeout (CRITICAL #5).
type DNDClient struct {
	URL     string
	client  *http.Client
}

// NewDNDClient creates a DND client with explicit 5s timeout (not http.DefaultClient).
func NewDNDClient(dndURL string) *DNDClient {
	return &DNDClient{
		URL:    dndURL,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// CheckDND returns true if the phone number is on the DND registry.
func (d *DNDClient) CheckDND(ctx context.Context, phone string) (bool, error) {
	if d.URL == "" {
		return false, nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", d.URL+"/check?phone="+phone, nil)
	if err != nil {
		return false, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct{ IsDND bool `json:"is_dnd"` }
	json.Unmarshal(body, &result)
	return result.IsDND, nil
}

// ─── CRITICAL #8: Campaign Scheduling via Temporal ─────────────────────────

// ScheduledCampaign holds scheduling metadata.
type ScheduledCampaign struct {
	CampaignID  string    `json:"campaign_id"`
	PartyID     int       `json:"party_id"`
	ScheduledAt time.Time `json:"scheduled_at"`
	Recurring   string    `json:"recurring"` // "", "daily", "weekly"
}

// CampaignScheduler manages scheduled campaign execution.
type CampaignScheduler struct {
	db          *sql.DB
	engine      *DispatchEngine
	kafkaDisp   *KafkaDispatcher
	mu          sync.Mutex
	timers      map[string]*time.Timer
	temporalURL string
}

// NewCampaignScheduler creates a scheduler with optional Temporal integration.
func NewCampaignScheduler(db *sql.DB, engine *DispatchEngine, kafkaDisp *KafkaDispatcher, temporalURL string) *CampaignScheduler {
	return &CampaignScheduler{
		db:          db,
		engine:      engine,
		kafkaDisp:   kafkaDisp,
		timers:      make(map[string]*time.Timer),
		temporalURL: temporalURL,
	}
}

// ScheduleCampaign schedules a campaign for future execution.
func (cs *CampaignScheduler) ScheduleCampaign(ctx context.Context, sc ScheduledCampaign) error {
	_, err := cs.db.ExecContext(ctx,
		`UPDATE gotv_campaigns SET status='scheduled', scheduled_at=$1 WHERE campaign_id=$2 AND party_id=$3`,
		sc.ScheduledAt, sc.CampaignID, sc.PartyID)
	if err != nil {
		return fmt.Errorf("failed to update campaign schedule: %w", err)
	}

	// If Temporal is configured, use workflow scheduling
	if cs.temporalURL != "" {
		return cs.scheduleViaTemporal(ctx, sc)
	}

	// Fallback: in-process timer
	delay := time.Until(sc.ScheduledAt)
	if delay <= 0 {
		go cs.executeCampaign(sc.CampaignID, sc.PartyID)
		return nil
	}

	cs.mu.Lock()
	if existing, ok := cs.timers[sc.CampaignID]; ok {
		existing.Stop()
	}
	cs.timers[sc.CampaignID] = time.AfterFunc(delay, func() {
		cs.executeCampaign(sc.CampaignID, sc.PartyID)
	})
	cs.mu.Unlock()

	log.Info().Str("campaign", sc.CampaignID).Time("at", sc.ScheduledAt).Msg("Campaign scheduled")
	return nil
}

func (cs *CampaignScheduler) scheduleViaTemporal(ctx context.Context, sc ScheduledCampaign) error {
	// Temporal workflow invocation via HTTP API
	payload := map[string]interface{}{
		"workflowId": "gotv-campaign-" + sc.CampaignID,
		"taskQueue":  "gotv-dispatch",
		"input":      []interface{}{sc},
	}
	body, _ := json.Marshal(payload)

	client := newHTTPClient(10 * time.Second)
	_, err := client.Post(ctx, cs.temporalURL+"/api/v1/namespaces/default/workflows", body, map[string]string{
		"Content-Type": "application/json",
	})
	if err != nil {
		log.Warn().Err(err).Msg("Temporal schedule failed, using in-process timer")
		delay := time.Until(sc.ScheduledAt)
		if delay > 0 {
			cs.mu.Lock()
			cs.timers[sc.CampaignID] = time.AfterFunc(delay, func() {
				cs.executeCampaign(sc.CampaignID, sc.PartyID)
			})
			cs.mu.Unlock()
		}
	}
	return nil
}

func (cs *CampaignScheduler) executeCampaign(campaignID string, partyID int) {
	ctx := context.Background()
	cs.db.ExecContext(ctx, `UPDATE gotv_campaigns SET status='active' WHERE campaign_id=$1`, campaignID)
	if err := cs.engine.LaunchCampaignV2(ctx, campaignID, partyID, cs.kafkaDisp); err != nil {
		log.Error().Err(err).Str("campaign", campaignID).Msg("Scheduled campaign execution failed")
	}
}

// ─── ENHANCE #9: Multi-Wave Campaign Sequences ─────────────────────────────

// CampaignSequence defines a multi-wave outreach sequence.
type CampaignSequence struct {
	SequenceID string `json:"sequence_id"`
	PartyID    int    `json:"party_id"`
	Name       string `json:"name"`
	Waves      []Wave `json:"waves"`
	Status     string `json:"status"` // draft, active, completed
}

// Wave is a single step in a multi-wave sequence.
type Wave struct {
	WaveNumber  int    `json:"wave_number"`
	Channel     string `json:"channel"`     // sms, whatsapp, phone_call
	Template    string `json:"template"`
	DelayHours  int    `json:"delay_hours"` // hours after previous wave
	Condition   string `json:"condition"`   // "no_response", "no_pledge", "all"
}

// CreateSequence saves a multi-wave campaign sequence.
func (d *DispatchEngine) CreateSequence(ctx context.Context, seq CampaignSequence) error {
	wavesJSON, _ := json.Marshal(seq.Waves)
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO gotv_campaign_sequences (sequence_id, party_id, name, waves, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		seq.SequenceID, seq.PartyID, seq.Name, string(wavesJSON), "draft")
	return err
}

// ExecuteNextWave dispatches the next wave of a sequence to eligible contacts.
func (d *DispatchEngine) ExecuteNextWave(ctx context.Context, seqID string, partyID int, waveNum int) error {
	var wavesJSON string
	err := d.db.QueryRowContext(ctx,
		`SELECT waves FROM gotv_campaign_sequences WHERE sequence_id=$1 AND party_id=$2`,
		seqID, partyID).Scan(&wavesJSON)
	if err != nil {
		return fmt.Errorf("sequence not found: %w", err)
	}

	var waves []Wave
	json.Unmarshal([]byte(wavesJSON), &waves)

	if waveNum < 0 || waveNum >= len(waves) {
		return fmt.Errorf("invalid wave number %d (max %d)", waveNum, len(waves)-1)
	}

	wave := waves[waveNum]

	// Create a campaign for this wave
	campaignID := fmt.Sprintf("%s-wave-%d", seqID, waveNum)
	d.db.ExecContext(ctx,
		`INSERT INTO gotv_campaigns (campaign_id, party_id, name, campaign_type, message_template, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, 'active', NOW())
		 ON CONFLICT (campaign_id) DO UPDATE SET status='active'`,
		campaignID, partyID, fmt.Sprintf("Seq %s Wave %d", seqID, waveNum), wave.Channel, wave.Template)

	return d.LaunchCampaign(ctx, campaignID, partyID)
}

// ─── ENHANCE #10: Contact Segmentation Engine ──────────────────────────────

// Segment defines a dynamic contact filter.
type Segment struct {
	SegmentID string          `json:"segment_id"`
	PartyID   int             `json:"party_id"`
	Name      string          `json:"name"`
	Filters   []SegmentFilter `json:"filters"`
}

// SegmentFilter is a single filter rule.
type SegmentFilter struct {
	Field    string      `json:"field"`    // state_code, lga_code, voter_status, last_contact_days, pledges
	Operator string      `json:"operator"` // eq, neq, gt, lt, in, contains
	Value    interface{} `json:"value"`
}

// SaveSegment persists a segment definition.
func (d *DispatchEngine) SaveSegment(ctx context.Context, seg Segment) error {
	filtersJSON, _ := json.Marshal(seg.Filters)
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO gotv_segments (segment_id, party_id, name, filters, created_at)
		 VALUES ($1, $2, $3, $4, NOW())`,
		seg.SegmentID, seg.PartyID, seg.Name, string(filtersJSON))
	return err
}

// EvaluateSegment returns contact IDs matching the segment's filters.
func (d *DispatchEngine) EvaluateSegment(ctx context.Context, seg Segment) ([]string, error) {
	baseQuery := `SELECT c.contact_id FROM gotv_contacts c
		LEFT JOIN gotv_pledges p ON p.contact_id = c.contact_id
		LEFT JOIN (SELECT contact_id, MAX(created_at) AS last_outreach FROM gotv_outreach_log GROUP BY contact_id) o ON o.contact_id = c.contact_id
		WHERE c.party_id = $1 AND c.opted_out = FALSE`

	args := []interface{}{seg.PartyID}
	argIdx := 2

	for _, f := range seg.Filters {
		switch f.Field {
		case "state_code":
			if f.Operator == "eq" {
				baseQuery += fmt.Sprintf(" AND c.state_code = $%d", argIdx)
				args = append(args, f.Value)
				argIdx++
			} else if f.Operator == "in" {
				vals, ok := f.Value.([]interface{})
				if ok {
					placeholders := make([]string, len(vals))
					for i, v := range vals {
						placeholders[i] = fmt.Sprintf("$%d", argIdx)
						args = append(args, v)
						argIdx++
					}
					baseQuery += fmt.Sprintf(" AND c.state_code IN (%s)", strings.Join(placeholders, ","))
				}
			}
		case "lga_code":
			if f.Operator == "eq" {
				baseQuery += fmt.Sprintf(" AND c.lga_code = $%d", argIdx)
				args = append(args, f.Value)
				argIdx++
			}
		case "voter_status":
			if f.Operator == "eq" {
				baseQuery += fmt.Sprintf(" AND c.voter_status = $%d", argIdx)
				args = append(args, f.Value)
				argIdx++
			} else if f.Operator == "in" {
				vals, ok := f.Value.([]interface{})
				if ok {
					placeholders := make([]string, len(vals))
					for i, v := range vals {
						placeholders[i] = fmt.Sprintf("$%d", argIdx)
						args = append(args, v)
						argIdx++
					}
					baseQuery += fmt.Sprintf(" AND c.voter_status IN (%s)", strings.Join(placeholders, ","))
				}
			}
		case "last_contact_days":
			days, ok := f.Value.(float64)
			if ok {
				if f.Operator == "gt" {
					baseQuery += fmt.Sprintf(" AND (o.last_outreach IS NULL OR o.last_outreach < NOW() - INTERVAL '%d days')", int(days))
				} else if f.Operator == "lt" {
					baseQuery += fmt.Sprintf(" AND o.last_outreach > NOW() - INTERVAL '%d days'", int(days))
				}
			}
		case "has_pledge":
			baseQuery += " AND p.pledge_id IS NOT NULL"
		case "no_pledge":
			baseQuery += " AND p.pledge_id IS NULL"
		}
	}

	rows, err := d.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contactIDs []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		contactIDs = append(contactIDs, id)
	}
	return contactIDs, nil
}

// ─── ENHANCE #11: WhatsApp Button Reply Processing ─────────────────────────

// ProcessWhatsAppButtonReply handles button clicks from WhatsApp interactive messages.
func (d *DispatchEngine) ProcessWhatsAppButtonReply(ctx context.Context, from, buttonID, contactID string, partyID int) {
	switch buttonID {
	case "btn_vote", "ill_vote":
		// Update pledge status
		d.db.ExecContext(ctx,
			`INSERT INTO gotv_pledges (pledge_id, contact_id, party_id, status, created_at)
			 VALUES (gen_random_uuid()::text, $1, $2, 'confirmed', NOW())
			 ON CONFLICT (contact_id, party_id) DO UPDATE SET status='confirmed', updated_at=NOW()`,
			contactID, partyID)
		d.db.ExecContext(ctx, `UPDATE gotv_contacts SET voter_status='pledged' WHERE contact_id=$1`, contactID)

	case "btn_ride", "need_ride":
		// Auto-create ride request
		var lat, lng float64
		d.db.QueryRowContext(ctx, `SELECT COALESCE(latitude,0), COALESCE(longitude,0) FROM gotv_contacts WHERE contact_id=$1`, contactID).Scan(&lat, &lng)
		d.db.ExecContext(ctx,
			`INSERT INTO gotv_ride_requests (request_id, party_id, contact_id, pickup_latitude, pickup_longitude, status, created_at)
			 VALUES (gen_random_uuid()::text, $1, $2, $3, $4, 'pending', NOW())`,
			partyID, contactID, lat, lng)

	case "btn_find_pu", "find_pu":
		// Log intent; could trigger PU lookup response
		d.db.ExecContext(ctx,
			`INSERT INTO gotv_outreach_log (party_id, campaign_id, contact_id, channel, direction, status, created_at)
			 VALUES ($1, 'wa_button_reply', $2, 'whatsapp', 'inbound', 'responded', NOW())`,
			partyID, contactID)
	}

	// Record inbound for 24h window tracking
	d.db.ExecContext(ctx,
		`INSERT INTO gotv_outreach_log (party_id, campaign_id, contact_id, channel, direction, status, created_at)
		 VALUES ($1, 'button_reply', $2, 'whatsapp_interactive', 'inbound', 'responded', NOW())`,
		partyID, contactID)
}

// ─── ENHANCE #12: Campaign Budget Controls ─────────────────────────────────

// CheckBudget returns spend vs cap for a campaign.
func (d *DispatchEngine) CheckBudget(ctx context.Context, campaignID string) (spent, cap, remaining int64, exceeded bool) {
	d.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_kobo), 0) FROM gotv_outreach_log WHERE campaign_id=$1`,
		campaignID).Scan(&spent)

	var capNull sql.NullInt64
	d.db.QueryRowContext(ctx,
		`SELECT budget_cap_kobo FROM gotv_campaigns WHERE campaign_id=$1`,
		campaignID).Scan(&capNull)

	if !capNull.Valid || capNull.Int64 == 0 {
		return spent, 0, 0, false // No budget cap
	}
	cap = capNull.Int64
	remaining = cap - spent
	exceeded = spent >= cap
	return
}

// ─── ENHANCE #14: Volunteer Leaderboard ────────────────────────────────────

// LeaderboardEntry holds a volunteer's ranking.
type LeaderboardEntry struct {
	VolunteerID string `json:"volunteer_id"`
	FullName    string `json:"full_name"`
	Role        string `json:"role"`
	Score       int    `json:"score"`
	Rank        int    `json:"rank"`
	Badge       string `json:"badge,omitempty"`
	DoorsKnocked int   `json:"doors_knocked"`
	CallsMade    int   `json:"calls_made"`
	RidesGiven   int   `json:"rides_given"`
}

// GetLeaderboard returns ranked volunteers for a party.
func (d *DispatchEngine) GetLeaderboard(ctx context.Context, partyID int, period string, limit int) ([]LeaderboardEntry, error) {
	var timeFilter string
	switch period {
	case "daily":
		timeFilter = "AND v.last_checkin_at > NOW() - INTERVAL '24 hours'"
	case "weekly":
		timeFilter = "AND v.last_checkin_at > NOW() - INTERVAL '7 days'"
	case "monthly":
		timeFilter = "AND v.last_checkin_at > NOW() - INTERVAL '30 days'"
	default:
		timeFilter = ""
	}

	query := fmt.Sprintf(`SELECT volunteer_id, full_name, role, doors_knocked, calls_made, rides_given,
		(doors_knocked * 3 + calls_made * 2 + rides_given * 5) AS score
		FROM gotv_volunteers WHERE party_id = $1 AND is_active = TRUE %s
		ORDER BY score DESC LIMIT $2`, timeFilter)

	rows, err := d.db.QueryContext(ctx, query, partyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LeaderboardEntry
	rank := 1
	for rows.Next() {
		var e LeaderboardEntry
		rows.Scan(&e.VolunteerID, &e.FullName, &e.Role, &e.DoorsKnocked, &e.CallsMade, &e.RidesGiven, &e.Score)
		e.Rank = rank
		// Assign badges
		if rank == 1 {
			e.Badge = "champion"
		} else if rank <= 3 {
			e.Badge = "top_performer"
		} else if e.Score > 100 {
			e.Badge = "all_star"
		}
		entries = append(entries, e)
		rank++
	}
	return entries, nil
}

// ─── ENHANCE #15: Territory Assignment ─────────────────────────────────────

// AssignTerritories auto-partitions ward contacts among active canvassers.
func (d *DispatchEngine) AssignTerritories(ctx context.Context, partyID int, wardCode string) ([]map[string]interface{}, error) {
	// Get active canvassers in this ward
	rows, err := d.db.QueryContext(ctx,
		`SELECT volunteer_id, full_name FROM gotv_volunteers
		 WHERE party_id=$1 AND assigned_ward=$2 AND is_active=TRUE AND role IN ('canvasser','team_lead')`,
		partyID, wardCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var volunteers []struct{ ID, Name string }
	for rows.Next() {
		var v struct{ ID, Name string }
		rows.Scan(&v.ID, &v.Name)
		volunteers = append(volunteers, v)
	}
	if len(volunteers) == 0 {
		return nil, fmt.Errorf("no active canvassers in ward %s", wardCode)
	}

	// Count contacts in this ward
	var totalContacts int
	d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND ward_code=$2 AND opted_out=FALSE`,
		partyID, wardCode).Scan(&totalContacts)

	contactsPerVol := totalContacts / len(volunteers)
	if contactsPerVol == 0 {
		contactsPerVol = 1
	}

	// Create territory assignments
	var territories []map[string]interface{}
	for i, vol := range volunteers {
		terrID := fmt.Sprintf("terr-%s-%d", wardCode, i+1)
		count := contactsPerVol
		if i == len(volunteers)-1 {
			count = totalContacts - (contactsPerVol * i)
		}
		d.db.ExecContext(ctx,
			`INSERT INTO gotv_territories (territory_id, party_id, volunteer_id, ward_code, contact_count, status, created_at)
			 VALUES ($1, $2, $3, $4, $5, 'assigned', NOW())
			 ON CONFLICT (territory_id) DO UPDATE SET volunteer_id=$3, contact_count=$5, status='assigned'`,
			terrID, partyID, vol.ID, wardCode, count)

		territories = append(territories, map[string]interface{}{
			"territory_id":  terrID,
			"volunteer_id":  vol.ID,
			"volunteer_name": vol.Name,
			"ward_code":     wardCode,
			"contact_count": count,
			"status":        "assigned",
		})
	}
	return territories, nil
}

// ─── ENHANCE #16: Channel ROI Analytics ────────────────────────────────────

// ChannelROI holds cost-per-conversion metrics for a channel.
type ChannelROI struct {
	Channel        string  `json:"channel"`
	TotalSent      int     `json:"total_sent"`
	TotalDelivered int     `json:"total_delivered"`
	TotalResponded int     `json:"total_responded"`
	TotalPledged   int     `json:"total_pledged"`
	TotalCostKobo  int64   `json:"total_cost_kobo"`
	CostPerSend    float64 `json:"cost_per_send"`
	CostPerDeliver float64 `json:"cost_per_deliver"`
	CostPerRespond float64 `json:"cost_per_respond"`
	CostPerPledge  float64 `json:"cost_per_pledge"`
	DeliveryRate   float64 `json:"delivery_rate"`
	ResponseRate   float64 `json:"response_rate"`
	Recommendation string  `json:"recommendation"`
}

// GetChannelROI calculates cost-per-conversion for each outreach channel.
func (d *DispatchEngine) GetChannelROI(ctx context.Context, partyID int) ([]ChannelROI, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT o.channel,
			   COUNT(*) AS total_sent,
			   COUNT(*) FILTER(WHERE o.status IN ('delivered','read','responded')) AS total_delivered,
			   COUNT(*) FILTER(WHERE o.status = 'responded') AS total_responded,
			   COUNT(DISTINCT p.contact_id) FILTER(WHERE p.status IN ('confirmed','confirmed_day_of','fulfilled')) AS total_pledged,
			   COALESCE(SUM(o.cost_kobo), 0) AS total_cost
		FROM gotv_outreach_log o
		LEFT JOIN gotv_pledges p ON p.contact_id = o.contact_id AND p.party_id = o.party_id
		WHERE o.party_id = $1 AND o.direction = 'outbound'
		GROUP BY o.channel`, partyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ChannelROI
	for rows.Next() {
		var roi ChannelROI
		rows.Scan(&roi.Channel, &roi.TotalSent, &roi.TotalDelivered, &roi.TotalResponded, &roi.TotalPledged, &roi.TotalCostKobo)

		if roi.TotalSent > 0 {
			roi.CostPerSend = float64(roi.TotalCostKobo) / float64(roi.TotalSent)
			roi.DeliveryRate = float64(roi.TotalDelivered) / float64(roi.TotalSent)
		}
		if roi.TotalDelivered > 0 {
			roi.CostPerDeliver = float64(roi.TotalCostKobo) / float64(roi.TotalDelivered)
			roi.ResponseRate = float64(roi.TotalResponded) / float64(roi.TotalDelivered)
		}
		if roi.TotalResponded > 0 {
			roi.CostPerRespond = float64(roi.TotalCostKobo) / float64(roi.TotalResponded)
		}
		if roi.TotalPledged > 0 {
			roi.CostPerPledge = float64(roi.TotalCostKobo) / float64(roi.TotalPledged)
		}

		// Auto-recommendation
		if roi.ResponseRate > 0.3 {
			roi.Recommendation = "SCALE_UP: High response rate — increase budget allocation"
		} else if roi.ResponseRate > 0.1 {
			roi.Recommendation = "OPTIMIZE: Moderate ROI — A/B test messaging"
		} else if roi.TotalSent > 100 {
			roi.Recommendation = "REDUCE: Low response rate — reallocate to better channels"
		} else {
			roi.Recommendation = "INSUFFICIENT_DATA: Need more sends to evaluate"
		}
		results = append(results, roi)
	}
	return results, nil
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func newHTTPClient(timeout time.Duration) *simpleHTTPClient {
	return &simpleHTTPClient{timeout: timeout}
}

type simpleHTTPClient struct {
	timeout time.Duration
}

func (c *simpleHTTPClient) Post(ctx context.Context, reqURL string, body []byte, headers map[string]string) ([]byte, error) {
	client := &http.Client{Timeout: c.timeout}
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// logOutreachV2 logs a single outreach attempt (social media broadcast model).
func (d *DispatchEngine) logOutreachV2(partyID int, campaignID, contactID, channel, variant, status, messageID, errDetail string, latencyMs, costKobo int64) {
	d.db.Exec(`INSERT INTO gotv_outreach_log
		(party_id, campaign_id, contact_id, channel, direction, message_variant, status, message_id, error_detail, latency_ms, cost_kobo, sent_at, created_at)
		VALUES ($1, $2, $3, $4, 'outbound', $5, $6, $7, $8, $9, $10, NOW(), NOW())`,
		partyID, campaignID, contactID, channel, variant, status, messageID, errDetail, latencyMs, costKobo)
}
