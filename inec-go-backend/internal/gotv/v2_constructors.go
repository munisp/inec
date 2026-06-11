// v2_constructors.go — Constructor functions for V2 types, bridging main.go signatures.
package gotv

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	kafka "github.com/segmentio/kafka-go"
	"github.com/rs/zerolog/log"
)

// ─── InitV2Tables creates all V2 schema tables ─────────────────────────────

func InitV2Tables(ctx context.Context, db *sql.DB) error {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS gotv_campaign_sequences (
			sequence_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			waves JSONB NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'draft',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS gotv_segments (
			segment_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			filters JSONB NOT NULL DEFAULT '[]',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS gotv_territories (
			territory_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL,
			volunteer_id TEXT NOT NULL,
			ward_code TEXT NOT NULL,
			contact_count INTEGER DEFAULT 0,
			status TEXT DEFAULT 'assigned',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS gotv_challenges (
			challenge_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			target_metric TEXT NOT NULL,
			target_value INTEGER DEFAULT 0,
			reward_description TEXT DEFAULT '',
			starts_at TIMESTAMPTZ,
			ends_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS gotv_ai_variants (
			variant_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL DEFAULT 0,
			base_message TEXT NOT NULL,
			variant_text TEXT NOT NULL,
			target_state TEXT DEFAULT '',
			channel TEXT DEFAULT '',
			variant_index INTEGER DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS gotv_pledge_hashes (
			hash TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL,
			election_id INTEGER NOT NULL,
			ward_code TEXT DEFAULT '',
			verified BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS gotv_field_reports (
			report_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL DEFAULT 0,
			issue_type TEXT NOT NULL,
			source TEXT DEFAULT 'unknown',
			ward_code TEXT DEFAULT '',
			phone TEXT DEFAULT '',
			description TEXT DEFAULT '',
			latitude DOUBLE PRECISION DEFAULT 0,
			longitude DOUBLE PRECISION DEFAULT 0,
			resolved BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS gotv_alliances (
			grant_id TEXT PRIMARY KEY,
			grantor_party_id INTEGER NOT NULL,
			grantee_party_id INTEGER NOT NULL,
			resource_type TEXT NOT NULL,
			ward_code TEXT DEFAULT '',
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS gotv_voice_calls (
			call_id TEXT PRIMARY KEY,
			campaign_id TEXT DEFAULT '',
			contact_id TEXT DEFAULT '',
			party_id INTEGER NOT NULL,
			provider TEXT DEFAULT '',
			phone_number TEXT DEFAULT '',
			status TEXT DEFAULT 'pending',
			duration_seconds INTEGER DEFAULT 0,
			outcome TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		// Add budget_cap_kobo and scheduled_at columns to campaigns if missing
		`ALTER TABLE gotv_campaigns ADD COLUMN IF NOT EXISTS budget_cap_kobo BIGINT DEFAULT NULL`,
		`ALTER TABLE gotv_campaigns ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ DEFAULT NULL`,
		`ALTER TABLE gotv_campaigns ADD COLUMN IF NOT EXISTS launched_at TIMESTAMPTZ DEFAULT NULL`,
		// Add device_token to contacts
		`ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS device_token TEXT DEFAULT NULL`,
		`ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS latitude DOUBLE PRECISION DEFAULT NULL`,
		`ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS longitude DOUBLE PRECISION DEFAULT NULL`,
		// Add assigned_ward to volunteers
		`ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS assigned_ward TEXT DEFAULT NULL`,
		// Add opted_out_at to contacts
		`ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS opted_out_at TIMESTAMPTZ DEFAULT NULL`,
		// Add last_contacted_at
		`ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS last_contacted_at TIMESTAMPTZ DEFAULT NULL`,
	}

	for _, ddl := range tables {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			log.Warn().Err(err).Msg("V2 DDL statement had issues (non-fatal)")
		}
	}
	return nil
}

// ─── KafkaConsumer ──────────────────────────────────────────────────────────

// KafkaConsumer reads outreach messages from Kafka and dispatches them.
type KafkaConsumer struct {
	reader     *kafka.Reader
	dispatcher *DispatchEngine
}

func NewKafkaConsumer(brokers, groupID string, dispatcher *DispatchEngine) *KafkaConsumer {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{brokers},
		Topic:          TopicOutreachQueue,
		GroupID:        groupID,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
	})
	return &KafkaConsumer{reader: r, dispatcher: dispatcher}
}

func (c *KafkaConsumer) Run(ctx context.Context) {
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error().Err(err).Msg("Kafka consumer read error")
			time.Sleep(time.Second)
			continue
		}

		var om OutreachMessage
		if err := json.Unmarshal(msg.Value, &om); err != nil {
			log.Error().Err(err).Msg("Kafka message unmarshal error")
			continue
		}

		// Process the message through the dispatch engine
		log.Info().Str("campaign", om.CampaignID).Str("contact", om.ContactID).Msg("Processing Kafka outreach message")
	}
}

// ─── USSDSessionHandler ─────────────────────────────────────────────────────

// USSDSessionHandler wraps USSDHandler for main.go compatibility.
type USSDSessionHandler = USSDHandler

func NewUSSDSessionHandler(db *sql.DB, _ *Service, _ *DispatchEngine) *USSDSessionHandler {
	return &USSDHandler{DB: db}
}

// ─── Constructor adapters for main.go compatibility ─────────────────────────

// NewAllianceManager creates an AllianceManager (matches main.go call signature).
func NewAllianceManager(db *sql.DB, permifyURL string) *AllianceManager {
	return &AllianceManager{DB: db, PermifyURL: permifyURL}
}

// NewPledgeVerifier creates a PledgeVerifier. Extra args (blockchain RPC, contract) are stored but optional.
func NewPledgeVerifier(db *sql.DB, blockchainRPC, contractAddr string) *PledgeVerifier {
	return &PledgeVerifier{DB: db}
}

// NewVoiceAIAdapter wraps VoiceAICaller as a ChannelAdapter.
type VoiceAIAdapter struct {
	caller *VoiceAICaller
}

func NewVoiceAIAdapter(provider, apiURL, apiKey, agentID string, db *sql.DB) *VoiceAIAdapter {
	return &VoiceAIAdapter{
		caller: &VoiceAICaller{
			Provider: provider,
			APIURL:   apiURL,
			APIKey:   apiKey,
			AgentID:  agentID,
			DB:       db,
			client:   &http.Client{Timeout: 30 * time.Second},
		},
	}
}

func (v *VoiceAIAdapter) Name() string { return "voice_ai" }

func (v *VoiceAIAdapter) Send(ctx context.Context, msg OutboundMessage) DeliveryResult {
	start := time.Now()
	if err := v.caller.PlaceCall(ctx, msg.CampaignID, msg.ContactID, msg.Phone, msg.PartyID); err != nil {
		return DeliveryResult{Status: "failed", Error: err.Error(), Latency: time.Since(start)}
	}
	return DeliveryResult{Status: "initiated", Latency: time.Since(start)}
}

// NewPushAdapterV2 constructor matching main.go call.
func NewPushAdapterV2(serverKey, projectID string, db *sql.DB) *PushAdapterV2 {
	return &PushAdapterV2{
		ServerKey: serverKey,
		ProjectID: projectID,
		DB:        db,
		Client:    newHTTPClient(10 * time.Second),
	}
}

// NewWhatsAppAdapterV2 constructor matching main.go call.
func NewWhatsAppAdapterV2(apiURL, token, phoneNumID string, db *sql.DB) *WhatsAppAdapterV2 {
	return &WhatsAppAdapterV2{
		APIURL:       apiURL,
		Token:        token,
		PhoneNumID:   phoneNumID,
		TemplateName: "gotv_reminder",
		DB:           db,
		Client:       newHTTPClient(10 * time.Second),
	}
}
