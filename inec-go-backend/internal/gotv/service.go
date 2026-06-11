// Package gotv implements the GOTV (Get Out The Vote) domain service.
// It provides party-scoped voter mobilization tooling: campaigns, contacts,
// volunteers, pledges, ride-to-polls, and aggregate turnout intelligence.
package gotv

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/rs/zerolog/log"
)

// Service holds the GOTV domain logic and dependencies.
type Service struct {
	DB            *sql.DB
	EncryptionKey []byte // 32-byte AES-256 key
}

// NewService creates a GOTV service with the given database and encryption key.
func NewService(db *sql.DB, encKeyHex string) *Service {
	svc := &Service{DB: db}
	if encKeyHex == "" {
		svc.EncryptionKey = make([]byte, 32)
		rand.Read(svc.EncryptionKey)
		log.Warn().Msg("GOTV: using random encryption key (set GOTV_ENCRYPTION_KEY for production)")
	} else {
		key, err := hex.DecodeString(encKeyHex)
		if err != nil || len(key) != 32 {
			log.Fatal().Msg("GOTV_ENCRYPTION_KEY must be 64 hex chars (32 bytes AES-256)")
		}
		svc.EncryptionKey = key
	}
	return svc
}

// InitTables creates GOTV schema if not present.
func (s *Service) InitTables(ctx context.Context) error {
	tables := `
	CREATE TABLE IF NOT EXISTS gotv_party_access (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		api_key_hash TEXT NOT NULL,
		created_by TEXT NOT NULL,
		is_active BOOLEAN DEFAULT TRUE,
		rate_limit_per_hour INTEGER DEFAULT 1000,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP,
		UNIQUE(party_id)
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_party_access_party ON gotv_party_access(party_id);

	CREATE TABLE IF NOT EXISTS gotv_campaigns (
		id SERIAL PRIMARY KEY,
		campaign_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		name TEXT NOT NULL,
		description TEXT,
		campaign_type TEXT NOT NULL CHECK(campaign_type IN ('sms','ussd','push','whatsapp','email','door_to_door','phone_bank','ride_to_polls','twitter','facebook','instagram')),
		status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','scheduled','active','paused','completed','cancelled')),
		target_state TEXT,
		target_lga TEXT,
		target_ward TEXT,
		target_polling_unit TEXT,
		message_template TEXT,
		message_variant_b TEXT,
		ab_split_pct INTEGER DEFAULT 50 CHECK(ab_split_pct >= 0 AND ab_split_pct <= 100),
		scheduled_at TIMESTAMP,
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		total_contacts INTEGER DEFAULT 0,
		contacts_reached INTEGER DEFAULT 0,
		contacts_responded INTEGER DEFAULT 0,
		created_by TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_campaigns_party ON gotv_campaigns(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_campaigns_status ON gotv_campaigns(status);

	CREATE TABLE IF NOT EXISTS gotv_contacts (
		id SERIAL PRIMARY KEY,
		contact_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		phone_encrypted TEXT NOT NULL,
		phone_hash TEXT NOT NULL,
		full_name_encrypted TEXT,
		state_code TEXT,
		lga_code TEXT,
		ward_code TEXT,
		polling_unit_code TEXT,
		voter_status TEXT DEFAULT 'unknown' CHECK(voter_status IN ('unknown','pledged','confirmed','declined','unreachable')),
		tags TEXT[] DEFAULT '{}',
		consent_id TEXT,
		opted_out BOOLEAN DEFAULT FALSE,
		opted_out_at TIMESTAMP,
		last_contacted_at TIMESTAMP,
		contact_count INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(party_id, phone_hash)
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_contacts_party ON gotv_contacts(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_contacts_geo ON gotv_contacts(state_code, lga_code, ward_code);

	CREATE TABLE IF NOT EXISTS gotv_volunteers (
		id SERIAL PRIMARY KEY,
		volunteer_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		user_id INTEGER,
		full_name TEXT NOT NULL,
		phone TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'canvasser' CHECK(role IN ('canvasser','driver','coordinator','phone_banker','team_lead','caller','observer')),
		assigned_state TEXT,
		assigned_lga TEXT,
		assigned_ward TEXT,
		assigned_polling_unit TEXT,
		is_active BOOLEAN DEFAULT TRUE,
		has_vehicle BOOLEAN DEFAULT FALSE,
		vehicle_capacity INTEGER DEFAULT 0,
		latitude REAL,
		longitude REAL,
		last_checkin_at TIMESTAMP,
		doors_knocked INTEGER DEFAULT 0,
		calls_made INTEGER DEFAULT 0,
		rides_given INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_volunteers_party ON gotv_volunteers(party_id);

	CREATE TABLE IF NOT EXISTS gotv_pledges (
		id SERIAL PRIMARY KEY,
		pledge_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		contact_id TEXT NOT NULL REFERENCES gotv_contacts(contact_id),
		election_id INTEGER,
		pledge_type TEXT NOT NULL DEFAULT 'will_vote' CHECK(pledge_type IN ('will_vote','needs_ride','needs_info','will_volunteer')),
		status TEXT NOT NULL DEFAULT 'pledged' CHECK(status IN ('pledged','reminded','confirmed_day_of','fulfilled','broken')),
		reminder_sent BOOLEAN DEFAULT FALSE,
		reminder_sent_at TIMESTAMP,
		fulfilled_at TIMESTAMP,
		notes TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_pledges_party ON gotv_pledges(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_pledges_contact ON gotv_pledges(contact_id);

	CREATE TABLE IF NOT EXISTS gotv_ride_requests (
		id SERIAL PRIMARY KEY,
		request_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		contact_id TEXT NOT NULL REFERENCES gotv_contacts(contact_id),
		volunteer_id TEXT REFERENCES gotv_volunteers(volunteer_id),
		pickup_latitude REAL NOT NULL,
		pickup_longitude REAL NOT NULL,
		polling_unit_code TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','matched','en_route','picked_up','dropped_off','cancelled','no_show')),
		requested_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		matched_at TIMESTAMP,
		picked_up_at TIMESTAMP,
		dropped_off_at TIMESTAMP,
		distance_km REAL
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_rides_party ON gotv_ride_requests(party_id);

	CREATE TABLE IF NOT EXISTS gotv_outreach_log (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		campaign_id TEXT,
		contact_id TEXT,
		channel TEXT NOT NULL CHECK(channel IN ('sms','ussd','push','whatsapp','email','door_knock','phone_call','log','twitter','facebook','instagram')),
		direction TEXT NOT NULL DEFAULT 'outbound' CHECK(direction IN ('outbound','inbound')),
		message_variant TEXT DEFAULT 'a',
		status TEXT NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','sent','delivered','pending','read','responded','failed','opted_out','dnd_blocked','deferred')),
		message_id TEXT,
		error_detail TEXT,
		latency_ms INTEGER DEFAULT 0,
		cost_kobo INTEGER DEFAULT 0,
		response_text TEXT,
		sent_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		delivered_at TIMESTAMP,
		volunteer_id TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_outreach_party ON gotv_outreach_log(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_outreach_campaign ON gotv_outreach_log(campaign_id);

	CREATE TABLE IF NOT EXISTS gotv_audit_log (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		actor TEXT NOT NULL,
		action TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		resource_id TEXT,
		details JSONB,
		ip_address TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_audit_party ON gotv_audit_log(party_id);

	CREATE TABLE IF NOT EXISTS gotv_door_knocks (
		id SERIAL PRIMARY KEY,
		knock_id TEXT UNIQUE,
		party_id INTEGER NOT NULL,
		volunteer_id TEXT NOT NULL,
		contact_id TEXT,
		shift_id TEXT,
		latitude REAL NOT NULL DEFAULT 0,
		longitude REAL NOT NULL DEFAULT 0,
		outcome TEXT NOT NULL CHECK(outcome IN ('home','not_home','refused','pledged','already_voted','moved','callback')),
		notes TEXT,
		speed_kmh REAL DEFAULT 0,
		is_suspicious BOOLEAN DEFAULT FALSE,
		knocked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_knocks_party ON gotv_door_knocks(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_knocks_vol ON gotv_door_knocks(volunteer_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_knocks_shift ON gotv_door_knocks(shift_id);

	CREATE TABLE IF NOT EXISTS gotv_shifts (
		id SERIAL PRIMARY KEY,
		shift_id TEXT UNIQUE,
		party_id INTEGER NOT NULL,
		volunteer_id TEXT NOT NULL,
		start_lat REAL,
		start_lng REAL,
		end_lat REAL,
		end_lng REAL,
		started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		ended_at TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_shifts_vol ON gotv_shifts(volunteer_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_shifts_shift ON gotv_shifts(shift_id);

	CREATE TABLE IF NOT EXISTS gotv_import_log (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		import_count INTEGER NOT NULL,
		imported_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_import_party ON gotv_import_log(party_id);

	CREATE TABLE IF NOT EXISTS gotv_mobile_users (
		id SERIAL PRIMARY KEY,
		user_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
		phone_hash TEXT NOT NULL,
		phone_encrypted TEXT NOT NULL,
		display_name TEXT NOT NULL,
		volunteer_id TEXT,
		role TEXT DEFAULT 'canvasser' CHECK(role IN ('canvasser','driver','coordinator','observer','caller')),
		is_active BOOLEAN DEFAULT TRUE,
		otp_code_hash TEXT,
		otp_expires_at TIMESTAMP,
		otp_attempts INTEGER DEFAULT 0,
		jwt_refresh_token TEXT,
		jwt_expires_at TIMESTAMP,
		last_login_at TIMESTAMP,
		last_sync_at TIMESTAMP,
		device_info JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(party_id, phone_hash)
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_mobile_users_party ON gotv_mobile_users(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_mobile_users_phone ON gotv_mobile_users(party_id, phone_hash);
	`
	_, err := s.DB.ExecContext(ctx, tables)
	if err != nil {
		log.Error().Err(err).Msg("GOTV: failed to initialize tables")
	}
	return err
}

// Encrypt encrypts plaintext using AES-256-GCM.
func (s *Service) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.EncryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(sealed), nil
}

// Decrypt decrypts AES-256-GCM ciphertext.
func (s *Service) Decrypt(cipherHex string) (string, error) {
	data, err := hex.DecodeString(cipherHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.EncryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return "", fmt.Errorf("ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// PhoneHash returns a deterministic HMAC-SHA256 hash for phone deduplication.
func (s *Service) PhoneHash(phone string) string {
	mac := hmac.New(sha256.New, s.EncryptionKey)
	mac.Write([]byte(NormalizePhone(phone)))
	return hex.EncodeToString(mac.Sum(nil))
}

// NormalizePhone strips whitespace and leading zeros, ensures +234 prefix.
func NormalizePhone(phone string) string {
	p := phone
	for _, c := range []string{" ", "-", "(", ")"} {
		p = replaceAll(p, c, "")
	}
	if len(p) > 1 && p[0] == '0' {
		p = "+234" + p[1:]
	}
	if len(p) > 3 && p[:3] == "234" {
		p = "+" + p
	}
	return p
}

func replaceAll(s, old, new string) string {
	for {
		i := indexOf(s, old)
		if i < 0 {
			return s
		}
		s = s[:i] + new + s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Audit logs an action to the GOTV audit trail.
func (s *Service) Audit(partyID int, actor, action, resourceType, resourceID string) {
	_, err := s.DB.Exec(
		`INSERT INTO gotv_audit_log (party_id, actor, action, resource_type, resource_id) VALUES ($1,$2,$3,$4,$5)`,
		partyID, actor, action, resourceType, resourceID,
	)
	if err != nil {
		log.Error().Err(err).Str("action", action).Msg("GOTV audit log failed")
	}
}

// Campaign represents a GOTV campaign.
type Campaign struct {
	ID              int        `json:"id"`
	CampaignID      string     `json:"campaign_id"`
	PartyID         int        `json:"party_id"`
	Name            string     `json:"name"`
	Description     string     `json:"description,omitempty"`
	CampaignType    string     `json:"campaign_type"`
	Status          string     `json:"status"`
	TargetState     string     `json:"target_state,omitempty"`
	TargetLGA       string     `json:"target_lga,omitempty"`
	MessageTemplate string     `json:"message_template,omitempty"`
	ABSplitPct      int        `json:"ab_split_pct"`
	TotalContacts   int        `json:"total_contacts"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
}

// Contact represents a GOTV contact (PII fields encrypted).
type Contact struct {
	ContactID   string    `json:"contact_id"`
	PartyID     int       `json:"party_id"`
	PhoneMasked string    `json:"phone_masked"`
	FullName    string    `json:"full_name,omitempty"`
	StateCode   string    `json:"state_code,omitempty"`
	LGACode     string    `json:"lga_code,omitempty"`
	VoterStatus string    `json:"voter_status"`
	Tags        []string  `json:"tags"`
	OptedOut    bool      `json:"opted_out"`
	CreatedAt   time.Time `json:"created_at"`
}

// Volunteer represents a GOTV volunteer.
type Volunteer struct {
	VolunteerID string    `json:"volunteer_id"`
	PartyID     int       `json:"party_id"`
	FullName    string    `json:"full_name"`
	Role        string    `json:"role"`
	IsActive    bool      `json:"is_active"`
	HasVehicle  bool      `json:"has_vehicle"`
	CreatedAt   time.Time `json:"created_at"`
}
