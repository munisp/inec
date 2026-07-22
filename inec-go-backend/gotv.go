package main

// GOTV (Get Out The Vote) — Party engagement and voter mobilization module.
// Architectural principle: INEC provides equal-access tooling to ALL registered parties.
// Parties manage THEIR OWN contact lists (not the voter register).
// Only aggregate PU-level turnout data is exposed — never individual voter status.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
	"github.com/rs/zerolog/log"
)

// ─── Schema ────────────────────────────────────────────────────────────────

func initGOTVTables() {
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
		campaign_type TEXT NOT NULL CHECK(campaign_type IN ('sms','ussd','push','whatsapp','whatsapp_interactive','email','door_to_door','phone_bank','ride_to_polls','twitter','facebook','instagram','tiktok')),
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
	CREATE INDEX IF NOT EXISTS idx_gotv_contacts_status ON gotv_contacts(voter_status);
	CREATE INDEX IF NOT EXISTS idx_gotv_contacts_consent ON gotv_contacts(consent_id);

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
	CREATE INDEX IF NOT EXISTS idx_gotv_volunteers_geo ON gotv_volunteers(assigned_state, assigned_lga, assigned_ward);
	CREATE INDEX IF NOT EXISTS idx_gotv_volunteers_role ON gotv_volunteers(role);

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
	CREATE INDEX IF NOT EXISTS idx_gotv_pledges_status ON gotv_pledges(status);

	CREATE TABLE IF NOT EXISTS gotv_outreach_log (
		id SERIAL PRIMARY KEY,
		log_id TEXT UNIQUE NOT NULL,
		campaign_id TEXT REFERENCES gotv_campaigns(campaign_id),
		party_id INTEGER NOT NULL REFERENCES parties(id),
		contact_id TEXT REFERENCES gotv_contacts(contact_id),
		volunteer_id TEXT REFERENCES gotv_volunteers(volunteer_id),
		channel TEXT NOT NULL CHECK(channel IN ('sms','ussd','push','whatsapp','whatsapp_interactive','email','door_knock','phone_call','log','twitter','facebook','instagram','tiktok')),
		direction TEXT NOT NULL DEFAULT 'outbound' CHECK(direction IN ('outbound','inbound')),
		message_text TEXT,
		message_variant TEXT DEFAULT 'A' CHECK(message_variant IN ('A','B')),
		status TEXT NOT NULL DEFAULT 'sent' CHECK(status IN ('queued','sent','delivered','read','responded','failed','opted_out')),
		response_text TEXT,
		sent_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		delivered_at TIMESTAMP,
		responded_at TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_outreach_campaign ON gotv_outreach_log(campaign_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_outreach_party ON gotv_outreach_log(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_outreach_status ON gotv_outreach_log(status);

	CREATE TABLE IF NOT EXISTS gotv_turnout_snapshots (
		id SERIAL PRIMARY KEY,
		election_id INTEGER NOT NULL,
		polling_unit_code TEXT NOT NULL,
		accredited_count INTEGER DEFAULT 0,
		registered_voters INTEGER DEFAULT 0,
		turnout_pct REAL DEFAULT 0,
		snapshot_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(election_id, polling_unit_code, snapshot_at)
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_turnout_election ON gotv_turnout_snapshots(election_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_turnout_pu ON gotv_turnout_snapshots(polling_unit_code);

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
	CREATE INDEX IF NOT EXISTS idx_gotv_rides_status ON gotv_ride_requests(status);
	CREATE INDEX IF NOT EXISTS idx_gotv_rides_volunteer ON gotv_ride_requests(volunteer_id);

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
	CREATE INDEX IF NOT EXISTS idx_gotv_audit_action ON gotv_audit_log(action);

	CREATE TABLE IF NOT EXISTS gotv_door_knocks (
		id SERIAL PRIMARY KEY,
		knock_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		volunteer_id TEXT NOT NULL REFERENCES gotv_volunteers(volunteer_id),
		contact_id TEXT REFERENCES gotv_contacts(contact_id),
		latitude REAL NOT NULL,
		longitude REAL NOT NULL,
		result TEXT NOT NULL DEFAULT 'not_home' CHECK(result IN ('not_home','refused','pledged','confirmed','needs_ride','moved','deceased')),
		notes TEXT,
		knocked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_knocks_party ON gotv_door_knocks(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_knocks_volunteer ON gotv_door_knocks(volunteer_id);

	CREATE TABLE IF NOT EXISTS gotv_shifts (
		id SERIAL PRIMARY KEY,
		shift_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		volunteer_id TEXT NOT NULL REFERENCES gotv_volunteers(volunteer_id),
		start_latitude REAL,
		start_longitude REAL,
		end_latitude REAL,
		end_longitude REAL,
		started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		ended_at TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_shifts_volunteer ON gotv_shifts(volunteer_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_shifts_open ON gotv_shifts(volunteer_id, ended_at);

	CREATE TABLE IF NOT EXISTS gotv_webhooks (
		id SERIAL PRIMARY KEY,
		webhook_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		url TEXT NOT NULL,
		events TEXT[] DEFAULT '{}',
		secret TEXT NOT NULL,
		is_active BOOLEAN DEFAULT TRUE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_webhooks_party ON gotv_webhooks(party_id);

	CREATE TABLE IF NOT EXISTS gotv_tasks (
		id SERIAL PRIMARY KEY,
		task_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		volunteer_id TEXT REFERENCES gotv_volunteers(volunteer_id),
		task_type TEXT NOT NULL DEFAULT 'canvass' CHECK(task_type IN ('canvass','phone_bank','ride','data_entry','training','other')),
		description TEXT,
		target_state TEXT,
		target_lga TEXT,
		target_ward TEXT,
		target_count INTEGER DEFAULT 0,
		completed_count INTEGER DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'unassigned' CHECK(status IN ('unassigned','assigned','in_progress','completed','cancelled')),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_tasks_party ON gotv_tasks(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_tasks_volunteer ON gotv_tasks(volunteer_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_tasks_status ON gotv_tasks(status);

	CREATE TABLE IF NOT EXISTS gotv_aspirants (
		id SERIAL PRIMARY KEY,
		aspirant_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		election_id INTEGER NOT NULL,
		full_name TEXT NOT NULL,
		position TEXT,
		state_of_origin TEXT,
		screening_status TEXT NOT NULL DEFAULT 'pending' CHECK(screening_status IN ('pending','cleared','disqualified')),
		status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','withdrawn')),
		manifesto_url TEXT,
		deposit_amount NUMERIC DEFAULT 0,
		deposit_paid BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_aspirants_election ON gotv_aspirants(election_id);

	CREATE TABLE IF NOT EXISTS gotv_delegates (
		id SERIAL PRIMARY KEY,
		delegate_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		election_id INTEGER NOT NULL,
		full_name TEXT NOT NULL,
		delegate_type TEXT NOT NULL DEFAULT 'ward' CHECK(delegate_type IN ('ward','statutory','ex_officio')),
		state_code TEXT,
		accreditation_status TEXT NOT NULL DEFAULT 'pending' CHECK(accreditation_status IN ('pending','accredited')),
		is_checked_in BOOLEAN DEFAULT FALSE,
		has_voted BOOLEAN DEFAULT FALSE,
		is_remote BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_delegates_election ON gotv_delegates(election_id);

	CREATE TABLE IF NOT EXISTS gotv_primary_rounds (
		id SERIAL PRIMARY KEY,
		round_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		election_id INTEGER NOT NULL,
		round_number INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','active','closed','certified')),
		voting_method TEXT NOT NULL DEFAULT 'in_person' CHECK(voting_method IN ('in_person','remote','hybrid')),
		total_votes INTEGER DEFAULT 0,
		total_eligible INTEGER DEFAULT 0,
		started_at TIMESTAMP,
		ended_at TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_rounds_election ON gotv_primary_rounds(election_id);

	CREATE TABLE IF NOT EXISTS gotv_segments (
		id SERIAL PRIMARY KEY,
		segment_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		name TEXT NOT NULL,
		filters JSONB DEFAULT '[]',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_segments_party ON gotv_segments(party_id);

	CREATE TABLE IF NOT EXISTS gotv_ai_variants (
		id SERIAL PRIMARY KEY,
		variant_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		campaign_id TEXT,
		base_message TEXT,
		variant_text TEXT,
		target_state TEXT,
		channel TEXT,
		variant_index INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_ai_variants_party ON gotv_ai_variants(party_id);
	`
	dbExecLog("gotv-schema", tables)

	// handleScoringMessageOptimize/handleScoringSelectVariant (gotv_scoring.go)
	// join outreach_log to gotv_ai_variants via a column the base schema never had.
	dbExecLog("gotv-schema-outreach-variant", "ALTER TABLE gotv_outreach_log ADD COLUMN message_variant_id TEXT")

	// gotvAuthMiddleware resolves a user's party via users.party_id when no
	// X-Party-ID header is sent; the column was never added to the base schema.
	dbExecLog("gotv-schema-users", "ALTER TABLE users ADD COLUMN party_id INTEGER REFERENCES parties(id)")

	// Volunteer vetting pipeline columns — added post-launch, not in the
	// original gotv_volunteers CREATE TABLE above.
	dbExecLog("gotv-schema-vetting-1", "ALTER TABLE gotv_volunteers ADD COLUMN vetting_status TEXT NOT NULL DEFAULT 'pending' CHECK(vetting_status IN ('pending','nin_verified','trained','approved','rejected','suspended'))")
	dbExecLog("gotv-schema-vetting-2", "ALTER TABLE gotv_volunteers ADD COLUMN nin_encrypted TEXT")
	dbExecLog("gotv-schema-vetting-3", "ALTER TABLE gotv_volunteers ADD COLUMN nin_verified_at TIMESTAMP")
	dbExecLog("gotv-schema-vetting-4", "ALTER TABLE gotv_volunteers ADD COLUMN training_completed_at TIMESTAMP")
	dbExecLog("gotv-schema-vetting-5", "ALTER TABLE gotv_volunteers ADD COLUMN approved_at TIMESTAMP")
	dbExecLog("gotv-schema-vetting-6", "ALTER TABLE gotv_volunteers ADD COLUMN rejected_reason TEXT")
	dbExecLog("gotv-schema-vetting-7", "ALTER TABLE gotv_volunteers ADD COLUMN suspended_reason TEXT")

	// Register GOTV in data processing register for NDPR compliance
	dbExecLog("gotv-ndpr", `
		INSERT INTO data_processing_register (processing_activity, purpose, legal_basis, data_categories, data_subjects, retention_period, recipients, cross_border_transfer, safeguards)
		VALUES (
			'GOTV Party Voter Mobilization',
			'Enable registered political parties to coordinate voter outreach using their own contact lists',
			'consent',
			ARRAY['phone_number','full_name','geo_location','voting_pledge'],
			'Party supporters who have given consent',
			'6 months after election',
			ARRAY['party_administrators','party_volunteers'],
			FALSE,
			'Field-level AES-256-GCM encryption, party-scoped row isolation, consent-gated outreach, automatic opt-out processing'
		) ON CONFLICT DO NOTHING
	`)

	log.Info().Msg("GOTV tables initialized")
}

// ─── Field-Level Encryption ────────────────────────────────────────────────

var gotvEncryptionKey []byte

func initGOTVEncryption() {
	keyHex := envOrDefault("GOTV_ENCRYPTION_KEY", "")
	if keyHex == "" {
		// Generate a key for dev mode
		gotvEncryptionKey = make([]byte, 32)
		rand.Read(gotvEncryptionKey)
		log.Warn().Msg("GOTV: using random encryption key (set GOTV_ENCRYPTION_KEY for production)")
	} else {
		var err error
		gotvEncryptionKey, err = hex.DecodeString(keyHex)
		if err != nil || len(gotvEncryptionKey) != 32 {
			log.Fatal().Msg("GOTV_ENCRYPTION_KEY must be 64 hex chars (32 bytes AES-256)")
		}
	}
}

func gotvEncrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(gotvEncryptionKey)
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

func gotvDecrypt(cipherHex string) (string, error) {
	data, err := hex.DecodeString(cipherHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(gotvEncryptionKey)
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

func gotvPhoneHash(phone string) string {
	mac := hmac.New(sha256.New, gotvEncryptionKey)
	mac.Write([]byte(normalizePhone(phone)))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")
	if strings.HasPrefix(phone, "0") && len(phone) == 11 {
		phone = "+234" + phone[1:]
	}
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}
	return phone
}

// ─── Party Auth Middleware ──────────────────────────────────────────────────

type gotvPartyContext struct {
	PartyID   int
	PartyCode string
	PartyName string
	UserID    int
	Username  string
	Role      string
}

func gotvAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract user from JWT (reuse existing auth)
		claims, err := getCurrentUser(r)
		if err != nil {
			http.Error(w, `{"error":"unauthorized","message":"valid authentication required"}`, http.StatusUnauthorized)
			return
		}
		username, _ := claims["username"].(string)
		role, _ := claims["role"].(string)

		// Get party_id from user's party association or from header
		partyIDStr := r.Header.Get("X-Party-ID")
		if partyIDStr == "" {
			// Try to get from user's profile
			var pid sql.NullInt64
			db.QueryRow("SELECT party_id FROM users WHERE username=$1", username).Scan(&pid)
			if !pid.Valid {
				http.Error(w, `{"error":"no_party","message":"user not associated with a party"}`, http.StatusForbidden)
				return
			}
			partyIDStr = strconv.FormatInt(pid.Int64, 10)
		}

		partyID, err := strconv.Atoi(partyIDStr)
		if err != nil {
			http.Error(w, `{"error":"invalid_party_id"}`, http.StatusBadRequest)
			return
		}

		// Verify party exists and is active
		var partyCode, partyName string
		var isActive int
		err = db.QueryRow("SELECT code, name, is_active FROM parties WHERE id=$1", partyID).Scan(&partyCode, &partyName, &isActive)
		if err != nil {
			http.Error(w, `{"error":"party_not_found"}`, http.StatusNotFound)
			return
		}
		if isActive != 1 {
			http.Error(w, `{"error":"party_inactive","message":"party registration is not active"}`, http.StatusForbidden)
			return
		}

		// Verify user has GOTV access for this party (party_admin role or admin)
		if role != "admin" && role != "party_admin" {
			http.Error(w, `{"error":"insufficient_role","message":"party_admin or admin role required"}`, http.StatusForbidden)
			return
		}

		// Store context and audit
		r.Header.Set("X-GOTV-Party-ID", strconv.Itoa(partyID))
		r.Header.Set("X-GOTV-Party-Code", partyCode)
		r.Header.Set("X-GOTV-Party-Name", partyName)
		r.Header.Set("X-GOTV-User", username)

		next(w, r)
	}
}

func getGOTVParty(r *http.Request) (int, string, string) {
	pid, _ := strconv.Atoi(r.Header.Get("X-GOTV-Party-ID"))
	return pid, r.Header.Get("X-GOTV-Party-Code"), r.Header.Get("X-GOTV-Party-Name")
}

func gotvAudit(partyID int, actor, action, resourceType, resourceID string, details interface{}, r *http.Request) {
	detailsJSON, _ := json.Marshal(details)
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	db.Exec(`INSERT INTO gotv_audit_log (party_id, actor, action, resource_type, resource_id, details, ip_address) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		partyID, actor, action, resourceType, resourceID, detailsJSON, ip)
}

// ─── Route Registration ────────────────────────────────────────────────────

func registerGOTVRoutes(r *mux.Router) {
	gotv := r.PathPrefix("/gotv").Subrouter()

	// Public: aggregate turnout (no party auth needed)
	gotv.HandleFunc("/turnout/{election_id}", handleGOTVTurnout).Methods("GET")
	gotv.HandleFunc("/turnout/{election_id}/{state_code}", handleGOTVTurnoutByState).Methods("GET")

	// Party-scoped endpoints (require party auth)
	// Campaigns
	gotv.HandleFunc("/campaigns", gotvAuthMiddleware(handleGOTVListCampaigns)).Methods("GET")
	gotv.HandleFunc("/campaigns", gotvAuthMiddleware(handleGOTVCreateCampaign)).Methods("POST")
	gotv.HandleFunc("/campaigns/{id}", gotvAuthMiddleware(handleGOTVGetCampaign)).Methods("GET")
	gotv.HandleFunc("/campaigns/{id}", gotvAuthMiddleware(handleGOTVUpdateCampaign)).Methods("PUT")
	gotv.HandleFunc("/campaigns/{id}", gotvAuthMiddleware(handleGOTVDeleteCampaign)).Methods("DELETE")
	gotv.HandleFunc("/campaigns/{id}/launch", gotvAuthMiddleware(handleGOTVLaunchCampaign)).Methods("POST")
	gotv.HandleFunc("/campaigns/{id}/pause", gotvAuthMiddleware(handleGOTVPauseCampaign)).Methods("POST")
	gotv.HandleFunc("/campaigns/{id}/resume", gotvAuthMiddleware(handleGOTVResumeCampaign)).Methods("POST")
	gotv.HandleFunc("/campaigns/{id}/stats", gotvAuthMiddleware(handleGOTVCampaignStats)).Methods("GET")

	// Contacts
	gotv.HandleFunc("/contacts", gotvAuthMiddleware(handleGOTVListContacts)).Methods("GET")
	gotv.HandleFunc("/contacts", gotvAuthMiddleware(handleGOTVCreateContact)).Methods("POST")
	gotv.HandleFunc("/contacts/import", gotvAuthMiddleware(handleGOTVImportContacts)).Methods("POST")
	gotv.HandleFunc("/contacts/{id}", gotvAuthMiddleware(handleGOTVGetContact)).Methods("GET")
	gotv.HandleFunc("/contacts/{id}", gotvAuthMiddleware(handleGOTVUpdateContact)).Methods("PUT")
	gotv.HandleFunc("/contacts/{id}/opt-out", gotvAuthMiddleware(handleGOTVOptOut)).Methods("POST")

	// Volunteers
	gotv.HandleFunc("/volunteers", gotvAuthMiddleware(handleGOTVListVolunteers)).Methods("GET")
	gotv.HandleFunc("/volunteers", gotvAuthMiddleware(handleGOTVCreateVolunteer)).Methods("POST")
	gotv.HandleFunc("/volunteers/{id}", gotvAuthMiddleware(handleGOTVUpdateVolunteer)).Methods("PUT")
	gotv.HandleFunc("/volunteers/{id}/checkin", gotvAuthMiddleware(handleGOTVVolunteerCheckin)).Methods("POST")
	gotv.HandleFunc("/volunteers/{id}/location", gotvAuthMiddleware(handleGOTVVolunteerCheckin)).Methods("POST")

	// Pledges
	gotv.HandleFunc("/pledges", gotvAuthMiddleware(handleGOTVListPledges)).Methods("GET")
	gotv.HandleFunc("/pledges", gotvAuthMiddleware(handleGOTVCreatePledge)).Methods("POST")
	gotv.HandleFunc("/pledges/{id}", gotvAuthMiddleware(handleGOTVUpdatePledgeStatus)).Methods("PATCH")
	gotv.HandleFunc("/pledges/{id}/remind", gotvAuthMiddleware(handleGOTVRemindPledge)).Methods("POST")

	// Ride-to-Polls
	gotv.HandleFunc("/rides", gotvAuthMiddleware(handleGOTVListRides)).Methods("GET")
	gotv.HandleFunc("/rides", gotvAuthMiddleware(handleGOTVRequestRide)).Methods("POST")
	gotv.HandleFunc("/rides/{id}/match", gotvAuthMiddleware(handleGOTVMatchRide)).Methods("POST")
	gotv.HandleFunc("/rides/{id}/status", gotvAuthMiddleware(handleGOTVUpdateRideStatus)).Methods("PUT", "PATCH")

	// Dashboard / Analytics
	gotv.HandleFunc("/dashboard", gotvAuthMiddleware(handleGOTVDashboard)).Methods("GET")
	gotv.HandleFunc("/analytics/outreach", gotvAuthMiddleware(handleGOTVOutreachAnalytics)).Methods("GET")
	gotv.HandleFunc("/analytics/geo", gotvAuthMiddleware(handleGOTVGeoAnalytics)).Methods("GET")

	// Geo map layers
	gotv.HandleFunc("/geo/volunteers", gotvAuthMiddleware(handleGOTVGeoVolunteers)).Methods("GET")
	gotv.HandleFunc("/geo/rides", gotvAuthMiddleware(handleGOTVGeoRides)).Methods("GET")
	gotv.HandleFunc("/geo/coverage", gotvAuthMiddleware(handleGOTVGeoCoverage)).Methods("GET")
	gotv.HandleFunc("/geo/canvass-trails", gotvAuthMiddleware(handleGOTVGeoTrails)).Methods("GET")

	// Canvass workflow
	gotv.HandleFunc("/canvass/walklist", gotvAuthMiddleware(handleGOTVWalklist)).Methods("GET")
	gotv.HandleFunc("/canvass/knock", gotvAuthMiddleware(handleGOTVRecordDoorKnock)).Methods("POST")
	gotv.HandleFunc("/canvass/shift/start", gotvAuthMiddleware(handleGOTVStartShift)).Methods("POST")
	gotv.HandleFunc("/canvass/shift/end", gotvAuthMiddleware(handleGOTVEndShift)).Methods("POST")

	// Volunteer vetting
	gotv.HandleFunc("/volunteers/vetting", gotvAuthMiddleware(handleGOTVVettingPipeline)).Methods("GET")
	gotv.HandleFunc("/volunteers/{id}/vetting", gotvAuthMiddleware(handleGOTVGetVolunteerVetting)).Methods("GET")
	gotv.HandleFunc("/volunteers/{id}/verify-nin", gotvAuthMiddleware(handleGOTVVerifyVolunteerNIN)).Methods("POST")
	gotv.HandleFunc("/volunteers/{id}/training", gotvAuthMiddleware(handleGOTVCompleteVolunteerTraining)).Methods("POST")
	gotv.HandleFunc("/volunteers/{id}/approve", gotvAuthMiddleware(handleGOTVApproveVolunteer)).Methods("POST")
	gotv.HandleFunc("/volunteers/{id}/reject", gotvAuthMiddleware(handleGOTVRejectVolunteer)).Methods("POST")
	gotv.HandleFunc("/volunteers/{id}/suspend", gotvAuthMiddleware(handleGOTVSuspendVolunteer)).Methods("POST")

	// Location assignment
	gotv.HandleFunc("/volunteers/{id}/assign-location", gotvAuthMiddleware(handleGOTVAssignVolunteerLocation)).Methods("POST")
	gotv.HandleFunc("/volunteers/bulk-assign-locations", gotvAuthMiddleware(handleGOTVBulkAssignLocations)).Methods("POST")
	gotv.HandleFunc("/volunteers/auto-assign-locations", gotvAuthMiddleware(handleGOTVAutoAssignLocations)).Methods("POST")
	gotv.HandleFunc("/locations/capacity", gotvAuthMiddleware(handleGOTVLocationCapacity)).Methods("GET")

	// Tasks
	gotv.HandleFunc("/tasks", gotvAuthMiddleware(handleGOTVListTasks)).Methods("GET")
	gotv.HandleFunc("/tasks", gotvAuthMiddleware(handleGOTVCreateTask)).Methods("POST")
	gotv.HandleFunc("/tasks/auto-assign", gotvAuthMiddleware(handleGOTVAutoAssignTasks)).Methods("POST")
	gotv.HandleFunc("/tasks/{id}/assign", gotvAuthMiddleware(handleGOTVAssignTask)).Methods("POST")
	gotv.HandleFunc("/tasks/{id}/status", gotvAuthMiddleware(handleGOTVUpdateTaskStatus)).Methods("PATCH")

	// Webhooks
	gotv.HandleFunc("/webhooks", gotvAuthMiddleware(handleGOTVListWebhooks)).Methods("GET")
	gotv.HandleFunc("/webhooks", gotvAuthMiddleware(handleGOTVCreateWebhook)).Methods("POST")
	gotv.HandleFunc("/webhooks/{id}", gotvAuthMiddleware(handleGOTVDeleteWebhook)).Methods("DELETE")

	// Party Primaries (lightweight)
	gotv.HandleFunc("/primaries/elections/{election_id}/dashboard", gotvAuthMiddleware(handleGOTVPrimariesDashboard)).Methods("GET")
	gotv.HandleFunc("/primaries/aspirants", gotvAuthMiddleware(handleGOTVPrimariesAspirants)).Methods("GET")
	gotv.HandleFunc("/primaries/delegates", gotvAuthMiddleware(handleGOTVPrimariesDelegates)).Methods("GET")
	gotv.HandleFunc("/primaries/elections/{election_id}/rounds", gotvAuthMiddleware(handleGOTVPrimariesRounds)).Methods("GET")
	gotv.HandleFunc("/primaries/elections/{election_id}/crypto/audit", gotvAuthMiddleware(handleGOTVPrimariesCryptoAudit)).Methods("GET")
	gotv.HandleFunc("/primaries/remote/verify", gotvAuthMiddleware(handleGOTVPrimariesRemoteVerify)).Methods("GET")

	// Leaderboard / Segments / War Room
	gotv.HandleFunc("/leaderboard", gotvAuthMiddleware(handleGOTVLeaderboard)).Methods("GET")
	gotv.HandleFunc("/segments", gotvAuthMiddleware(handleGOTVListSegments)).Methods("GET")
	gotv.HandleFunc("/segments", gotvAuthMiddleware(handleGOTVCreateSegment)).Methods("POST")
	gotv.HandleFunc("/segments/{id}", gotvAuthMiddleware(handleGOTVDeleteSegment)).Methods("DELETE")
	gotv.HandleFunc("/warroom/summary", gotvAuthMiddleware(handleGOTVWarRoomSummary)).Methods("GET")
	gotv.HandleFunc("/warroom/stream", gotvAuthMiddleware(handleGOTVWarRoomStream)).Methods("GET")
	gotv.HandleFunc("/warroom/ai-alerts", gotvAuthMiddleware(handleGOTVWarRoomAIAlerts)).Methods("GET")

	// Analytics extras
	gotv.HandleFunc("/ai/variants", gotvAuthMiddleware(handleGOTVAIVariants)).Methods("GET")
	gotv.HandleFunc("/roi/channels", gotvAuthMiddleware(handleGOTVROIChannels)).Methods("GET")
	gotv.HandleFunc("/turnout/predict", gotvAuthMiddleware(handleGOTVTurnoutPredict)).Methods("GET")

	// GOTV-scoped ledger/blockchain (placeholders)
	gotv.HandleFunc("/ledger/accounts", gotvAuthMiddleware(handleGOTVLedgerAccounts)).Methods("GET")
	gotv.HandleFunc("/ledger/history", gotvAuthMiddleware(handleGOTVLedgerHistory)).Methods("GET")
	gotv.HandleFunc("/ledger/reconcile", gotvAuthMiddleware(handleGOTVLedgerReconcile)).Methods("GET")
	gotv.HandleFunc("/blockchain/status", gotvAuthMiddleware(handleGOTVBlockchainStatus)).Methods("GET")
	gotv.HandleFunc("/blockchain/blocks", gotvAuthMiddleware(handleGOTVBlockchainBlocks)).Methods("GET")
	gotv.HandleFunc("/blockchain/anchor", gotvAuthMiddleware(handleGOTVBlockchainAnchor)).Methods("POST")

	// Platform tab
	gotv.HandleFunc("/teams/leaderboard", gotvAuthMiddleware(handleGOTVTeamsLeaderboard)).Methods("GET")
	gotv.HandleFunc("/experiments", gotvAuthMiddleware(handleGOTVExperiments)).Methods("GET")
	gotv.HandleFunc("/nl/query", gotvAuthMiddleware(handleGOTVNLQuery)).Methods("POST")
	gotv.HandleFunc("/simulation", gotvAuthMiddleware(handleGOTVSimulation)).Methods("POST")

	// KOH indicators (placeholders)
	gotv.HandleFunc("/koh/cpi/compute", gotvAuthMiddleware(handleGOTVKOHCPICompute)).Methods("GET")
	gotv.HandleFunc("/koh/cpi/history", gotvAuthMiddleware(gotvKOHEmpty("history"))).Methods("GET")
	gotv.HandleFunc("/koh/demographics", gotvAuthMiddleware(handleGOTVKOHDemographics)).Methods("GET")
	gotv.HandleFunc("/koh/lga/dashboard", gotvAuthMiddleware(handleGOTVKOHLGADashboard)).Methods("GET")
	gotv.HandleFunc("/koh/social/sentiment", gotvAuthMiddleware(handleGOTVKOHSocialSentiment)).Methods("GET")
	gotv.HandleFunc("/koh/social/share-of-voice", gotvAuthMiddleware(handleGOTVKOHShareOfVoice)).Methods("GET")
	gotv.HandleFunc("/koh/endorsements", gotvAuthMiddleware(gotvKOHEmpty("endorsements"))).Methods("GET")
	gotv.HandleFunc("/koh/endorsements/score", gotvAuthMiddleware(handleGOTVKOHEndorsementScore)).Methods("GET")
	gotv.HandleFunc("/koh/reports", gotvAuthMiddleware(gotvKOHEmpty("reports"))).Methods("GET")
	gotv.HandleFunc("/koh/reports/generate/{type}", gotvAuthMiddleware(handleGOTVKOHReportGenerate)).Methods("POST")
	gotv.HandleFunc("/koh/surveys", gotvAuthMiddleware(gotvKOHEmpty("surveys"))).Methods("GET")
	gotv.HandleFunc("/koh/surveys/trend", gotvAuthMiddleware(gotvKOHEmpty("trend"))).Methods("GET")
	gotv.HandleFunc("/koh/analytics/summary", gotvAuthMiddleware(handleGOTVKOHAnalyticsSummary)).Methods("GET")

	// Scoring Engine (Cambridge Analytica-grade analytics)
	gotv.HandleFunc("/scoring/voter/{contactID}", gotvAuthMiddleware(handleScoringVoter)).Methods("GET")
	gotv.HandleFunc("/scoring/voters/batch", gotvAuthMiddleware(handleScoringBatch)).Methods("POST")
	gotv.HandleFunc("/scoring/persuadability", gotvAuthMiddleware(handleScoringPersuadability)).Methods("POST")
	gotv.HandleFunc("/scoring/allocation/optimize", gotvAuthMiddleware(handleScoringAllocation)).Methods("GET")
	gotv.HandleFunc("/scoring/win-probability", gotvAuthMiddleware(handleScoringWinProbability)).Methods("GET")
	gotv.HandleFunc("/scoring/optimize/messages", gotvAuthMiddleware(handleScoringMessageOptimize)).Methods("GET")
	gotv.HandleFunc("/scoring/optimize/select-variant", gotvAuthMiddleware(handleScoringSelectVariant)).Methods("POST")
	gotv.HandleFunc("/scoring/summary", gotvAuthMiddleware(handleScoringSummary)).Methods("GET")
}

// ─── Campaign Handlers ─────────────────────────────────────────────────────

func handleGOTVListCampaigns(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	status := r.URL.Query().Get("status")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 20
	offset := (page - 1) * limit

	query := `SELECT campaign_id, name, description, campaign_type, status, target_state, target_lga, target_ward, target_polling_unit, message_template, message_variant_b, ab_split_pct, scheduled_at, started_at, completed_at, total_contacts, contacts_reached, contacts_responded, created_by, created_at FROM gotv_campaigns WHERE party_id=$1`
	args := []interface{}{partyID}
	argIdx := 2
	if status != "" {
		query += fmt.Sprintf(" AND status=$%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	query += " ORDER BY created_at DESC"
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var campaigns []map[string]interface{}
	for rows.Next() {
		var cid, name, ctype, cstatus, createdBy string
		var desc, tState, tLGA, tWard, tPU, msgTemplate, msgVariantB sql.NullString
		var abSplit, totalContacts, reached, responded int
		var scheduledAt, startedAt, completedAt sql.NullTime
		var createdAt time.Time
		err := rows.Scan(&cid, &name, &desc, &ctype, &cstatus, &tState, &tLGA, &tWard, &tPU, &msgTemplate, &msgVariantB, &abSplit, &scheduledAt, &startedAt, &completedAt, &totalContacts, &reached, &responded, &createdBy, &createdAt)
		if err != nil {
			continue
		}
		c := map[string]interface{}{
			"campaign_id":        cid,
			"name":               name,
			"description":        gotvNullStr(desc),
			"campaign_type":      ctype,
			"status":             cstatus,
			"target_state":       gotvNullStr(tState),
			"target_lga":         gotvNullStr(tLGA),
			"target_ward":        gotvNullStr(tWard),
			"target_polling_unit": gotvNullStr(tPU),
			"message_template":   gotvNullStr(msgTemplate),
			"message_variant_b":  gotvNullStr(msgVariantB),
			"ab_split_pct":       abSplit,
			"scheduled_at":       gotvNullTime(scheduledAt),
			"started_at":         gotvNullTime(startedAt),
			"completed_at":       gotvNullTime(completedAt),
			"total_contacts":     totalContacts,
			"contacts_reached":   reached,
			"contacts_responded": responded,
			"created_by":         createdBy,
			"created_at":         createdAt,
		}
		campaigns = append(campaigns, c)
	}

	var total int
	countQ := "SELECT COUNT(*) FROM gotv_campaigns WHERE party_id=$1"
	if status != "" {
		countQ += " AND status='" + status + "'"
	}
	db.QueryRow(countQ, partyID).Scan(&total)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"campaigns":  campaigns,
		"total":      total,
		"page":       page,
		"per_page":   limit,
		"total_pages": int(math.Ceil(float64(total) / float64(limit))),
	})
}

func handleGOTVCreateCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		Name             string  `json:"name"`
		Description      string  `json:"description"`
		CampaignType     string  `json:"campaign_type"`
		TargetState      string  `json:"target_state"`
		TargetLGA        string  `json:"target_lga"`
		TargetWard       string  `json:"target_ward"`
		TargetPU         string  `json:"target_polling_unit"`
		MessageTemplate  string  `json:"message_template"`
		MessageVariantB  string  `json:"message_variant_b"`
		ABSplitPct       int     `json:"ab_split_pct"`
		ScheduledAt      *string `json:"scheduled_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.CampaignType == "" {
		http.Error(w, `{"error":"name and campaign_type required"}`, http.StatusBadRequest)
		return
	}

	validTypes := map[string]bool{"sms": true, "ussd": true, "push": true, "whatsapp": true, "whatsapp_interactive": true, "email": true, "door_to_door": true, "phone_bank": true, "ride_to_polls": true, "twitter": true, "facebook": true, "instagram": true, "tiktok": true}
	if !validTypes[req.CampaignType] {
		http.Error(w, `{"error":"invalid campaign_type"}`, http.StatusBadRequest)
		return
	}
	if req.ABSplitPct == 0 {
		req.ABSplitPct = 50
	}

	campaignID := "gotv-camp-" + uuid.New().String()[:8]

	var scheduledAt *time.Time
	if req.ScheduledAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ScheduledAt)
		if err == nil {
			scheduledAt = &t
		}
	}

	// Count matching contacts for this geo scope
	contactQuery := "SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE"
	contactArgs := []interface{}{partyID}
	idx := 2
	if req.TargetState != "" {
		contactQuery += fmt.Sprintf(" AND state_code=$%d", idx)
		contactArgs = append(contactArgs, req.TargetState)
		idx++
	}
	if req.TargetLGA != "" {
		contactQuery += fmt.Sprintf(" AND lga_code=$%d", idx)
		contactArgs = append(contactArgs, req.TargetLGA)
		idx++
	}
	if req.TargetWard != "" {
		contactQuery += fmt.Sprintf(" AND ward_code=$%d", idx)
		contactArgs = append(contactArgs, req.TargetWard)
		idx++
	}
	if req.TargetPU != "" {
		contactQuery += fmt.Sprintf(" AND polling_unit_code=$%d", idx)
		contactArgs = append(contactArgs, req.TargetPU)
		idx++
	}
	var totalContacts int
	db.QueryRow(contactQuery, contactArgs...).Scan(&totalContacts)

	_, err := db.Exec(`INSERT INTO gotv_campaigns (campaign_id, party_id, name, description, campaign_type, target_state, target_lga, target_ward, target_polling_unit, message_template, message_variant_b, ab_split_pct, scheduled_at, total_contacts, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		campaignID, partyID, req.Name, req.Description, req.CampaignType,
		gotvNullIfEmpty(req.TargetState), gotvNullIfEmpty(req.TargetLGA), gotvNullIfEmpty(req.TargetWard), gotvNullIfEmpty(req.TargetPU),
		req.MessageTemplate, gotvNullIfEmpty(req.MessageVariantB), req.ABSplitPct, scheduledAt, totalContacts, username)
	if err != nil {
		log.Error().Err(err).Msg("GOTV: failed to create campaign")
		http.Error(w, `{"error":"failed to create campaign"}`, http.StatusInternalServerError)
		return
	}

	gotvAudit(partyID, username, "create_campaign", "campaign", campaignID, req, r)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"campaign_id":    campaignID,
		"total_contacts": totalContacts,
		"status":         "draft",
	})
}

func handleGOTVGetCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var cid, name, ctype, cstatus, createdBy string
	var desc, tState, tLGA, tWard, tPU, msgTemplate, msgVariantB sql.NullString
	var abSplit, totalContacts, reached, responded int
	var scheduledAt, startedAt, completedAt sql.NullTime
	var createdAt time.Time

	err := db.QueryRow(`SELECT campaign_id, name, description, campaign_type, status, target_state, target_lga, target_ward, target_polling_unit, message_template, message_variant_b, ab_split_pct, scheduled_at, started_at, completed_at, total_contacts, contacts_reached, contacts_responded, created_by, created_at FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2`, id, partyID).
		Scan(&cid, &name, &desc, &ctype, &cstatus, &tState, &tLGA, &tWard, &tPU, &msgTemplate, &msgVariantB, &abSplit, &scheduledAt, &startedAt, &completedAt, &totalContacts, &reached, &responded, &createdBy, &createdAt)
	if err != nil {
		http.Error(w, `{"error":"campaign not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"campaign_id": cid, "name": name, "description": gotvNullStr(desc),
		"campaign_type": ctype, "status": cstatus,
		"target_state": gotvNullStr(tState), "target_lga": gotvNullStr(tLGA),
		"target_ward": gotvNullStr(tWard), "target_polling_unit": gotvNullStr(tPU),
		"message_template": gotvNullStr(msgTemplate), "message_variant_b": gotvNullStr(msgVariantB),
		"ab_split_pct": abSplit, "scheduled_at": gotvNullTime(scheduledAt),
		"started_at": gotvNullTime(startedAt), "completed_at": gotvNullTime(completedAt),
		"total_contacts": totalContacts, "contacts_reached": reached,
		"contacts_responded": responded, "created_by": createdBy, "created_at": createdAt,
	})
}

func handleGOTVUpdateCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		MessageTemplate string `json:"message_template"`
		MessageVariantB string `json:"message_variant_b"`
		ABSplitPct      int    `json:"ab_split_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	// Only allow updates on draft/paused campaigns
	var currentStatus string
	db.QueryRow("SELECT status FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2", id, partyID).Scan(&currentStatus)
	if currentStatus != "draft" && currentStatus != "paused" {
		http.Error(w, `{"error":"can only update draft or paused campaigns"}`, http.StatusConflict)
		return
	}

	_, err := db.Exec(`UPDATE gotv_campaigns SET name=COALESCE(NULLIF($1,''),name), description=COALESCE(NULLIF($2,''),description), message_template=COALESCE(NULLIF($3,''),message_template), message_variant_b=$4, ab_split_pct=CASE WHEN $5>0 THEN $5 ELSE ab_split_pct END, updated_at=NOW() WHERE campaign_id=$6 AND party_id=$7`,
		req.Name, req.Description, req.MessageTemplate, gotvNullIfEmpty(req.MessageVariantB), req.ABSplitPct, id, partyID)
	if err != nil {
		http.Error(w, `{"error":"update failed"}`, http.StatusInternalServerError)
		return
	}

	gotvAudit(partyID, username, "update_campaign", "campaign", id, req, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func handleGOTVLaunchCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var status, ctype, msgTemplate string
	var totalContacts int
	err := db.QueryRow("SELECT status, campaign_type, message_template, total_contacts FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2", id, partyID).
		Scan(&status, &ctype, &msgTemplate, &totalContacts)
	if err != nil {
		http.Error(w, `{"error":"campaign not found"}`, http.StatusNotFound)
		return
	}
	if status != "draft" && status != "paused" {
		http.Error(w, fmt.Sprintf(`{"error":"cannot launch campaign in '%s' status"}`, status), http.StatusConflict)
		return
	}
	if msgTemplate == "" {
		http.Error(w, `{"error":"message_template required before launch"}`, http.StatusBadRequest)
		return
	}
	if totalContacts == 0 {
		http.Error(w, `{"error":"no contacts in target area"}`, http.StatusBadRequest)
		return
	}

	now := time.Now()
	_, err = db.Exec("UPDATE gotv_campaigns SET status='active', started_at=$1, updated_at=$1 WHERE campaign_id=$2 AND party_id=$3", now, id, partyID)
	if err != nil {
		http.Error(w, `{"error":"launch failed"}`, http.StatusInternalServerError)
		return
	}

	// Dispatch outreach asynchronously
	go dispatchCampaignOutreach(partyID, id, ctype, msgTemplate, "")

	gotvAudit(partyID, username, "launch_campaign", "campaign", id, nil, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "active",
		"total_contacts": totalContacts,
		"started_at":     now,
	})
}

func handleGOTVPauseCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	result, err := db.Exec("UPDATE gotv_campaigns SET status='paused', updated_at=NOW() WHERE campaign_id=$1 AND party_id=$2 AND status='active'", id, partyID)
	if err != nil {
		http.Error(w, `{"error":"pause failed"}`, http.StatusInternalServerError)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, `{"error":"campaign not active or not found"}`, http.StatusNotFound)
		return
	}
	gotvAudit(partyID, username, "pause_campaign", "campaign", id, nil, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "paused"})
}

func handleGOTVCampaignStats(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var totalContacts, reached, responded int
	db.QueryRow("SELECT total_contacts, contacts_reached, contacts_responded FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2", id, partyID).
		Scan(&totalContacts, &reached, &responded)

	// Channel breakdown
	rows, _ := db.Query("SELECT channel, status, COUNT(*) FROM gotv_outreach_log WHERE campaign_id=$1 AND party_id=$2 GROUP BY channel, status", id, partyID)
	channelStats := map[string]map[string]int{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var ch, st string
			var cnt int
			rows.Scan(&ch, &st, &cnt)
			if channelStats[ch] == nil {
				channelStats[ch] = map[string]int{}
			}
			channelStats[ch][st] = cnt
		}
	}

	// A/B test results
	var variantAResp, variantBResp int
	db.QueryRow("SELECT COUNT(*) FROM gotv_outreach_log WHERE campaign_id=$1 AND party_id=$2 AND message_variant='A' AND status='responded'", id, partyID).Scan(&variantAResp)
	db.QueryRow("SELECT COUNT(*) FROM gotv_outreach_log WHERE campaign_id=$1 AND party_id=$2 AND message_variant='B' AND status='responded'", id, partyID).Scan(&variantBResp)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"campaign_id":      id,
		"total_contacts":   totalContacts,
		"reached":          reached,
		"responded":        responded,
		"reach_rate":       gotvSafeDiv(float64(reached), float64(totalContacts)),
		"response_rate":    gotvSafeDiv(float64(responded), float64(reached)),
		"channel_stats":    channelStats,
		"ab_test": map[string]interface{}{
			"variant_a_responses": variantAResp,
			"variant_b_responses": variantBResp,
		},
	})
}

// ─── Contact Handlers ──────────────────────────────────────────────────────

func handleGOTVCreateContact(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		Phone            string   `json:"phone"`
		FullName         string   `json:"full_name"`
		StateCode        string   `json:"state_code"`
		LGACode          string   `json:"lga_code"`
		WardCode         string   `json:"ward_code"`
		PollingUnitCode  string   `json:"polling_unit_code"`
		Tags             []string `json:"tags"`
		ConsentPurpose   string   `json:"consent_purpose"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.Phone == "" {
		http.Error(w, `{"error":"phone required"}`, http.StatusBadRequest)
		return
	}

	// Consent enforcement — every contact must have consent
	consentID := "gotv-consent-" + uuid.New().String()[:8]
	purpose := req.ConsentPurpose
	if purpose == "" {
		purpose = "communication"
	}
	db.Exec(`INSERT INTO consent_records (consent_id, subject_id, purpose, legal_basis, granted_at) VALUES ($1, $2, $3, 'consent', NOW())`,
		consentID, normalizePhone(req.Phone), purpose)

	// Encrypt PII
	phoneEnc, err := gotvEncrypt(normalizePhone(req.Phone))
	if err != nil {
		http.Error(w, `{"error":"encryption failed"}`, http.StatusInternalServerError)
		return
	}
	pHash := gotvPhoneHash(req.Phone)

	var nameEnc sql.NullString
	if req.FullName != "" {
		enc, err := gotvEncrypt(req.FullName)
		if err == nil {
			nameEnc = sql.NullString{String: enc, Valid: true}
		}
	}

	contactID := "gotv-contact-" + uuid.New().String()[:8]
	tags := "{}"
	if len(req.Tags) > 0 {
		tags = "{" + strings.Join(req.Tags, ",") + "}"
	}

	_, err = db.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, full_name_encrypted, state_code, lga_code, ward_code, polling_unit_code, tags, consent_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		contactID, partyID, phoneEnc, pHash, nameEnc,
		gotvNullIfEmpty(req.StateCode), gotvNullIfEmpty(req.LGACode), gotvNullIfEmpty(req.WardCode), gotvNullIfEmpty(req.PollingUnitCode),
		tags, consentID)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			http.Error(w, `{"error":"contact already exists for this party"}`, http.StatusConflict)
			return
		}
		log.Error().Err(err).Msg("GOTV: failed to create contact")
		http.Error(w, `{"error":"failed to create contact"}`, http.StatusInternalServerError)
		return
	}

	gotvAudit(partyID, username, "create_contact", "contact", contactID, map[string]string{"state": req.StateCode}, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"contact_id": contactID,
		"consent_id": consentID,
	})
}

func handleGOTVListContacts(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 50
	offset := (page - 1) * limit
	stateFilter := r.URL.Query().Get("state")
	statusFilter := r.URL.Query().Get("voter_status")

	query := "SELECT contact_id, phone_encrypted, full_name_encrypted, state_code, lga_code, ward_code, polling_unit_code, voter_status, tags, opted_out, last_contacted_at, contact_count, created_at FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE"
	args := []interface{}{partyID}
	idx := 2
	if stateFilter != "" {
		query += fmt.Sprintf(" AND state_code=$%d", idx)
		args = append(args, stateFilter)
		idx++
	}
	if statusFilter != "" {
		query += fmt.Sprintf(" AND voter_status=$%d", idx)
		args = append(args, statusFilter)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var contacts []map[string]interface{}
	for rows.Next() {
		var cid, phoneEnc, vStatus string
		var nameEnc, state, lga, ward, pu sql.NullString
		var tags []string
		var optedOut bool
		var lastContacted sql.NullTime
		var contactCount int
		var createdAt time.Time
		if err := rows.Scan(&cid, &phoneEnc, &nameEnc, &state, &lga, &ward, &pu, &vStatus, (*pq.StringArray)(&tags), &optedOut, &lastContacted, &contactCount, &createdAt); err != nil {
			continue
		}

		// Decrypt phone for display (masked)
		phone, _ := gotvDecrypt(phoneEnc)
		maskedPhone := gotvMaskPhone(phone)

		var fullName string
		if nameEnc.Valid {
			fullName, _ = gotvDecrypt(nameEnc.String)
		}

		contacts = append(contacts, map[string]interface{}{
			"contact_id":       cid,
			"phone_masked":     maskedPhone,
			"full_name":        fullName,
			"state_code":       gotvNullStr(state),
			"lga_code":         gotvNullStr(lga),
			"ward_code":        gotvNullStr(ward),
			"polling_unit_code": gotvNullStr(pu),
			"voter_status":     vStatus,
			"tags":             tags,
			"last_contacted_at": gotvNullTime(lastContacted),
			"contact_count":    contactCount,
			"created_at":       createdAt,
		})
	}

	var total int
	db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE", partyID).Scan(&total)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"contacts":    contacts,
		"total":       total,
		"page":        page,
		"per_page":    limit,
		"total_pages": int(math.Ceil(float64(total) / float64(limit))),
	})
}

func handleGOTVGetContact(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var phoneEnc, vStatus string
	var nameEnc, state, lga, ward, pu, consentID sql.NullString
	var optedOut bool
	var contactCount int
	var createdAt time.Time
	err := db.QueryRow(`SELECT phone_encrypted, full_name_encrypted, state_code, lga_code, ward_code, polling_unit_code, voter_status, opted_out, consent_id, contact_count, created_at FROM gotv_contacts WHERE contact_id=$1 AND party_id=$2`, id, partyID).
		Scan(&phoneEnc, &nameEnc, &state, &lga, &ward, &pu, &vStatus, &optedOut, &consentID, &contactCount, &createdAt)
	if err != nil {
		http.Error(w, `{"error":"contact not found"}`, http.StatusNotFound)
		return
	}

	phone, _ := gotvDecrypt(phoneEnc)
	var fullName string
	if nameEnc.Valid {
		fullName, _ = gotvDecrypt(nameEnc.String)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"contact_id": id, "phone_masked": gotvMaskPhone(phone), "full_name": fullName,
		"state_code": gotvNullStr(state), "lga_code": gotvNullStr(lga),
		"ward_code": gotvNullStr(ward), "polling_unit_code": gotvNullStr(pu),
		"voter_status": vStatus, "opted_out": optedOut,
		"consent_id": gotvNullStr(consentID), "contact_count": contactCount,
		"created_at": createdAt,
	})
}

func handleGOTVUpdateContact(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		VoterStatus string   `json:"voter_status"`
		Tags        []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	if req.VoterStatus != "" {
		validStatuses := map[string]bool{"unknown": true, "pledged": true, "confirmed": true, "declined": true, "unreachable": true}
		if !validStatuses[req.VoterStatus] {
			http.Error(w, `{"error":"invalid voter_status"}`, http.StatusBadRequest)
			return
		}
		db.Exec("UPDATE gotv_contacts SET voter_status=$1, updated_at=NOW() WHERE contact_id=$2 AND party_id=$3", req.VoterStatus, id, partyID)
	}
	if len(req.Tags) > 0 {
		tags := "{" + strings.Join(req.Tags, ",") + "}"
		db.Exec("UPDATE gotv_contacts SET tags=$1, updated_at=NOW() WHERE contact_id=$2 AND party_id=$3", tags, id, partyID)
	}

	gotvAudit(partyID, username, "update_contact", "contact", id, req, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func handleGOTVOptOut(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	// NDPR right to object — immediate opt-out
	result, err := db.Exec("UPDATE gotv_contacts SET opted_out=TRUE, opted_out_at=NOW(), updated_at=NOW() WHERE contact_id=$1 AND party_id=$2 AND opted_out=FALSE", id, partyID)
	if err != nil {
		http.Error(w, `{"error":"opt-out failed"}`, http.StatusInternalServerError)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, `{"error":"contact not found or already opted out"}`, http.StatusNotFound)
		return
	}

	// Record DSR
	db.Exec(`INSERT INTO data_subject_requests (request_id, subject_id, request_type, status, completed_at, processed_by, notes) VALUES ($1, $2, 'objection', 'completed', NOW(), $3, 'GOTV opt-out')`,
		"gotv-dsr-"+uuid.New().String()[:8], id, username)

	gotvAudit(partyID, username, "opt_out_contact", "contact", id, nil, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "opted_out"})
}

// ─── CSV Import ────────────────────────────────────────────────────────────

func handleGOTVImportContacts(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	// Max 10MB CSV
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, `{"error":"file too large (max 10MB)"}`, http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"file field required"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	stateCode := r.FormValue("state_code")
	lgaCode := r.FormValue("lga_code")
	wardCode := r.FormValue("ward_code")

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		http.Error(w, `{"error":"invalid CSV format"}`, http.StatusBadRequest)
		return
	}

	// Map column indices
	colMap := map[string]int{}
	for i, h := range header {
		colMap[strings.ToLower(strings.TrimSpace(h))] = i
	}

	phoneCol, hasPhone := colMap["phone"]
	if !hasPhone {
		// Try alternate names
		for _, alt := range []string{"phone_number", "mobile", "tel", "telephone"} {
			if idx, ok := colMap[alt]; ok {
				phoneCol = idx
				hasPhone = true
				break
			}
		}
	}
	if !hasPhone {
		http.Error(w, `{"error":"CSV must have a 'phone' column"}`, http.StatusBadRequest)
		return
	}
	nameCol := colMap["name"]
	if _, ok := colMap["full_name"]; ok {
		nameCol = colMap["full_name"]
	}

	imported, duplicates, errors := 0, 0, 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errors++
			continue
		}
		if phoneCol >= len(record) {
			errors++
			continue
		}

		phone := strings.TrimSpace(record[phoneCol])
		if phone == "" {
			errors++
			continue
		}

		var fullName string
		if nameCol > 0 && nameCol < len(record) {
			fullName = strings.TrimSpace(record[nameCol])
		}

		// Create consent record
		consentID := "gotv-consent-" + uuid.New().String()[:8]
		db.Exec(`INSERT INTO consent_records (consent_id, subject_id, purpose, legal_basis, granted_at) VALUES ($1, $2, 'communication', 'consent', NOW()) ON CONFLICT DO NOTHING`,
			consentID, normalizePhone(phone))

		phoneEnc, err := gotvEncrypt(normalizePhone(phone))
		if err != nil {
			errors++
			continue
		}
		pHash := gotvPhoneHash(phone)

		var nameEnc sql.NullString
		if fullName != "" {
			enc, err := gotvEncrypt(fullName)
			if err == nil {
				nameEnc = sql.NullString{String: enc, Valid: true}
			}
		}

		contactID := "gotv-contact-" + uuid.New().String()[:8]
		_, err = db.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, full_name_encrypted, state_code, lga_code, ward_code, consent_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (party_id, phone_hash) DO NOTHING`,
			contactID, partyID, phoneEnc, pHash, nameEnc,
			gotvNullIfEmpty(stateCode), gotvNullIfEmpty(lgaCode), gotvNullIfEmpty(wardCode), consentID)
		if err != nil {
			errors++
			continue
		}
		// Check if actually inserted (ON CONFLICT DO NOTHING)
		var exists int
		db.QueryRow("SELECT 1 FROM gotv_contacts WHERE contact_id=$1", contactID).Scan(&exists)
		if exists == 1 {
			imported++
		} else {
			duplicates++
		}
	}

	gotvAudit(partyID, username, "import_contacts", "contacts", "", map[string]int{"imported": imported, "duplicates": duplicates, "errors": errors}, r)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"imported":   imported,
		"duplicates": duplicates,
		"errors":     errors,
		"total":      imported + duplicates + errors,
	})
}

// ─── Volunteer Handlers ────────────────────────────────────────────────────

func handleGOTVListVolunteers(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	role := r.URL.Query().Get("role")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 50
	offset := (page - 1) * limit

	query := "SELECT volunteer_id, full_name, phone, role, assigned_state, assigned_lga, assigned_ward, assigned_polling_unit, is_active, has_vehicle, vehicle_capacity, latitude, longitude, last_checkin_at, doors_knocked, calls_made, rides_given, created_at FROM gotv_volunteers WHERE party_id=$1"
	args := []interface{}{partyID}
	idx := 2
	if role != "" {
		query += fmt.Sprintf(" AND role=$%d", idx)
		args = append(args, role)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var volunteers []map[string]interface{}
	for rows.Next() {
		var vid, name, phone, vrole string
		var aState, aLGA, aWard, aPU sql.NullString
		var isActive, hasVehicle bool
		var vehicleCap, doorsKnocked, callsMade, ridesGiven int
		var lat, lng sql.NullFloat64
		var lastCheckin sql.NullTime
		var createdAt time.Time
		if err := rows.Scan(&vid, &name, &phone, &vrole, &aState, &aLGA, &aWard, &aPU, &isActive, &hasVehicle, &vehicleCap, &lat, &lng, &lastCheckin, &doorsKnocked, &callsMade, &ridesGiven, &createdAt); err != nil {
			continue
		}
		volunteers = append(volunteers, map[string]interface{}{
			"volunteer_id": vid, "full_name": name, "phone": gotvMaskPhone(phone), "role": vrole,
			"assigned_state": gotvNullStr(aState), "assigned_lga": gotvNullStr(aLGA),
			"assigned_ward": gotvNullStr(aWard), "assigned_polling_unit": gotvNullStr(aPU),
			"is_active": isActive, "has_vehicle": hasVehicle, "vehicle_capacity": vehicleCap,
			"latitude": gotvNullFloat(lat), "longitude": gotvNullFloat(lng),
			"last_checkin_at": gotvNullTime(lastCheckin),
			"doors_knocked": doorsKnocked, "calls_made": callsMade, "rides_given": ridesGiven,
			"created_at": createdAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"volunteers": volunteers, "page": page})
}

func handleGOTVCreateVolunteer(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		FullName    string  `json:"full_name"`
		Phone       string  `json:"phone"`
		Role        string  `json:"role"`
		State       string  `json:"assigned_state"`
		LGA         string  `json:"assigned_lga"`
		Ward        string  `json:"assigned_ward"`
		PU          string  `json:"assigned_polling_unit"`
		HasVehicle  bool    `json:"has_vehicle"`
		VehicleCap  int     `json:"vehicle_capacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.FullName == "" || req.Phone == "" {
		http.Error(w, `{"error":"full_name and phone required"}`, http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = "canvasser"
	}
	validRoles := map[string]bool{"canvasser": true, "driver": true, "coordinator": true, "phone_banker": true, "team_lead": true, "caller": true, "observer": true}
	if !validRoles[req.Role] {
		http.Error(w, `{"error":"invalid role"}`, http.StatusBadRequest)
		return
	}

	vid := "gotv-vol-" + uuid.New().String()[:8]
	_, err := db.Exec(`INSERT INTO gotv_volunteers (volunteer_id, party_id, full_name, phone, role, assigned_state, assigned_lga, assigned_ward, assigned_polling_unit, has_vehicle, vehicle_capacity)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		vid, partyID, req.FullName, req.Phone, req.Role,
		gotvNullIfEmpty(req.State), gotvNullIfEmpty(req.LGA), gotvNullIfEmpty(req.Ward), gotvNullIfEmpty(req.PU),
		req.HasVehicle, req.VehicleCap)
	if err != nil {
		http.Error(w, `{"error":"failed to create volunteer"}`, http.StatusInternalServerError)
		return
	}

	gotvAudit(partyID, username, "create_volunteer", "volunteer", vid, req, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"volunteer_id": vid})
}

func handleGOTVUpdateVolunteer(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		Role       string `json:"role"`
		State      string `json:"assigned_state"`
		LGA        string `json:"assigned_lga"`
		Ward       string `json:"assigned_ward"`
		PU         string `json:"assigned_polling_unit"`
		IsActive   *bool  `json:"is_active"`
		HasVehicle *bool  `json:"has_vehicle"`
		VehicleCap *int   `json:"vehicle_capacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	// Build dynamic update
	sets := []string{}
	args := []interface{}{}
	idx := 1
	if req.Role != "" {
		sets = append(sets, fmt.Sprintf("role=$%d", idx))
		args = append(args, req.Role)
		idx++
	}
	if req.State != "" {
		sets = append(sets, fmt.Sprintf("assigned_state=$%d", idx))
		args = append(args, req.State)
		idx++
	}
	if req.LGA != "" {
		sets = append(sets, fmt.Sprintf("assigned_lga=$%d", idx))
		args = append(args, req.LGA)
		idx++
	}
	if req.Ward != "" {
		sets = append(sets, fmt.Sprintf("assigned_ward=$%d", idx))
		args = append(args, req.Ward)
		idx++
	}
	if req.PU != "" {
		sets = append(sets, fmt.Sprintf("assigned_polling_unit=$%d", idx))
		args = append(args, req.PU)
		idx++
	}
	if req.IsActive != nil {
		sets = append(sets, fmt.Sprintf("is_active=$%d", idx))
		args = append(args, *req.IsActive)
		idx++
	}
	if req.HasVehicle != nil {
		sets = append(sets, fmt.Sprintf("has_vehicle=$%d", idx))
		args = append(args, *req.HasVehicle)
		idx++
	}
	if req.VehicleCap != nil {
		sets = append(sets, fmt.Sprintf("vehicle_capacity=$%d", idx))
		args = append(args, *req.VehicleCap)
		idx++
	}

	if len(sets) == 0 {
		http.Error(w, `{"error":"no fields to update"}`, http.StatusBadRequest)
		return
	}

	args = append(args, id, partyID)
	query := fmt.Sprintf("UPDATE gotv_volunteers SET %s WHERE volunteer_id=$%d AND party_id=$%d", strings.Join(sets, ","), idx, idx+1)
	db.Exec(query, args...)

	gotvAudit(partyID, username, "update_volunteer", "volunteer", id, req, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func handleGOTVVolunteerCheckin(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	db.Exec("UPDATE gotv_volunteers SET latitude=$1, longitude=$2, last_checkin_at=NOW() WHERE volunteer_id=$3 AND party_id=$4",
		req.Latitude, req.Longitude, id, partyID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "checked_in"})
}

// ─── Pledge Handlers ───────────────────────────────────────────────────────

func handleGOTVListPledges(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	status := r.URL.Query().Get("status")

	query := "SELECT p.pledge_id, p.contact_id, p.election_id, p.pledge_type, p.status, p.reminder_sent, p.created_at FROM gotv_pledges p WHERE p.party_id=$1"
	args := []interface{}{partyID}
	if status != "" {
		query += " AND p.status=$2"
		args = append(args, status)
	}
	query += " ORDER BY p.created_at DESC LIMIT 100"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var pledges []map[string]interface{}
	for rows.Next() {
		var pid, cid, ptype, pstatus string
		var electionID sql.NullInt64
		var reminderSent bool
		var createdAt time.Time
		if err := rows.Scan(&pid, &cid, &electionID, &ptype, &pstatus, &reminderSent, &createdAt); err != nil {
			continue
		}
		pledges = append(pledges, map[string]interface{}{
			"pledge_id": pid, "contact_id": cid, "election_id": gotvNullInt(electionID),
			"pledge_type": ptype, "status": pstatus, "reminder_sent": reminderSent,
			"created_at": createdAt,
		})
	}

	// Summary stats
	var totalPledges, confirmed, needsRide int
	db.QueryRow("SELECT COUNT(*) FROM gotv_pledges WHERE party_id=$1", partyID).Scan(&totalPledges)
	db.QueryRow("SELECT COUNT(*) FROM gotv_pledges WHERE party_id=$1 AND status='confirmed_day_of'", partyID).Scan(&confirmed)
	db.QueryRow("SELECT COUNT(*) FROM gotv_pledges WHERE party_id=$1 AND pledge_type='needs_ride'", partyID).Scan(&needsRide)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pledges": pledges,
		"summary": map[string]int{
			"total": totalPledges, "confirmed_day_of": confirmed, "needs_ride": needsRide,
		},
	})
}

func handleGOTVCreatePledge(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		ContactID  string `json:"contact_id"`
		ElectionID int    `json:"election_id"`
		PledgeType string `json:"pledge_type"`
		Notes      string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.ContactID == "" {
		http.Error(w, `{"error":"contact_id required"}`, http.StatusBadRequest)
		return
	}
	if req.PledgeType == "" {
		req.PledgeType = "will_vote"
	}

	// Verify contact belongs to this party and hasn't opted out
	var optedOut bool
	err := db.QueryRow("SELECT opted_out FROM gotv_contacts WHERE contact_id=$1 AND party_id=$2", req.ContactID, partyID).Scan(&optedOut)
	if err != nil {
		http.Error(w, `{"error":"contact not found"}`, http.StatusNotFound)
		return
	}
	if optedOut {
		http.Error(w, `{"error":"contact has opted out"}`, http.StatusForbidden)
		return
	}

	pledgeID := "gotv-pledge-" + uuid.New().String()[:8]
	_, err = db.Exec(`INSERT INTO gotv_pledges (pledge_id, party_id, contact_id, election_id, pledge_type, notes) VALUES ($1,$2,$3,$4,$5,$6)`,
		pledgeID, partyID, req.ContactID, req.ElectionID, req.PledgeType, gotvNullIfEmpty(req.Notes))
	if err != nil {
		http.Error(w, `{"error":"failed to create pledge"}`, http.StatusInternalServerError)
		return
	}

	// Update contact status
	db.Exec("UPDATE gotv_contacts SET voter_status='pledged', updated_at=NOW() WHERE contact_id=$1 AND party_id=$2", req.ContactID, partyID)

	gotvAudit(partyID, username, "create_pledge", "pledge", pledgeID, req, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"pledge_id": pledgeID})
}

func handleGOTVRemindPledge(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var contactID string
	err := db.QueryRow("SELECT contact_id FROM gotv_pledges WHERE pledge_id=$1 AND party_id=$2", id, partyID).Scan(&contactID)
	if err != nil {
		http.Error(w, `{"error":"pledge not found"}`, http.StatusNotFound)
		return
	}

	// Get contact phone for reminder
	var phoneEnc string
	db.QueryRow("SELECT phone_encrypted FROM gotv_contacts WHERE contact_id=$1 AND party_id=$2 AND opted_out=FALSE", contactID, partyID).Scan(&phoneEnc)
	if phoneEnc == "" {
		http.Error(w, `{"error":"contact opted out or not found"}`, http.StatusForbidden)
		return
	}

	// Mark reminder sent
	db.Exec("UPDATE gotv_pledges SET reminder_sent=TRUE, reminder_sent_at=NOW() WHERE pledge_id=$1", id)
	db.Exec("UPDATE gotv_contacts SET last_contacted_at=NOW(), contact_count=contact_count+1 WHERE contact_id=$1 AND party_id=$2", contactID, partyID)

	// Log outreach
	logID := "gotv-log-" + uuid.New().String()[:8]
	db.Exec(`INSERT INTO gotv_outreach_log (log_id, party_id, contact_id, channel, direction, message_text, status) VALUES ($1,$2,$3,'sms','outbound','Voting reminder: Remember to vote tomorrow!','sent')`,
		logID, partyID, contactID)

	gotvAudit(partyID, username, "remind_pledge", "pledge", id, nil, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "reminder_sent"})
}

// ─── Ride-to-Polls Handlers ───────────────────────────────────────────────

func handleGOTVListRides(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	status := r.URL.Query().Get("status")

	query := "SELECT request_id, contact_id, volunteer_id, pickup_latitude, pickup_longitude, polling_unit_code, status, requested_at, matched_at, picked_up_at, dropped_off_at, distance_km FROM gotv_ride_requests WHERE party_id=$1"
	args := []interface{}{partyID}
	if status != "" {
		query += " AND status=$2"
		args = append(args, status)
	}
	query += " ORDER BY requested_at DESC LIMIT 100"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rides []map[string]interface{}
	for rows.Next() {
		var rid, cid, rstatus, puCode string
		var vid sql.NullString
		var lat, lng float64
		var distKm sql.NullFloat64
		var requestedAt time.Time
		var matchedAt, pickedUpAt, droppedOffAt sql.NullTime
		if err := rows.Scan(&rid, &cid, &vid, &lat, &lng, &puCode, &rstatus, &requestedAt, &matchedAt, &pickedUpAt, &droppedOffAt, &distKm); err != nil {
			continue
		}
		rides = append(rides, map[string]interface{}{
			"request_id": rid, "contact_id": cid, "volunteer_id": gotvNullStr(vid),
			"pickup_latitude": lat, "pickup_longitude": lng, "polling_unit_code": puCode,
			"status": rstatus, "requested_at": requestedAt,
			"matched_at": gotvNullTime(matchedAt), "picked_up_at": gotvNullTime(pickedUpAt),
			"dropped_off_at": gotvNullTime(droppedOffAt), "distance_km": gotvNullFloat(distKm),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"rides": rides})
}

func handleGOTVRequestRide(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		ContactID       string  `json:"contact_id"`
		PickupLatitude  float64 `json:"pickup_latitude"`
		PickupLongitude float64 `json:"pickup_longitude"`
		PollingUnitCode string  `json:"polling_unit_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.ContactID == "" || req.PollingUnitCode == "" {
		http.Error(w, `{"error":"contact_id and polling_unit_code required"}`, http.StatusBadRequest)
		return
	}
	if req.PickupLatitude == 0 || req.PickupLongitude == 0 {
		http.Error(w, `{"error":"pickup coordinates required"}`, http.StatusBadRequest)
		return
	}

	requestID := "gotv-ride-" + uuid.New().String()[:8]
	_, err := db.Exec(`INSERT INTO gotv_ride_requests (request_id, party_id, contact_id, pickup_latitude, pickup_longitude, polling_unit_code) VALUES ($1,$2,$3,$4,$5,$6)`,
		requestID, partyID, req.ContactID, req.PickupLatitude, req.PickupLongitude, req.PollingUnitCode)
	if err != nil {
		http.Error(w, `{"error":"failed to create ride request"}`, http.StatusInternalServerError)
		return
	}

	gotvAudit(partyID, username, "request_ride", "ride", requestID, req, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"request_id": requestID, "status": "pending"})
}

func handleGOTVMatchRide(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		VolunteerID string `json:"volunteer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VolunteerID == "" {
		// Auto-match: find nearest available driver
		var lat, lng float64
		db.QueryRow("SELECT pickup_latitude, pickup_longitude FROM gotv_ride_requests WHERE request_id=$1 AND party_id=$2", id, partyID).Scan(&lat, &lng)

		var nearestVID string
		db.QueryRow(`SELECT volunteer_id FROM gotv_volunteers WHERE party_id=$1 AND role='driver' AND is_active=TRUE AND has_vehicle=TRUE AND latitude IS NOT NULL
			ORDER BY ((latitude-$2)*(latitude-$2) + (longitude-$3)*(longitude-$3)) LIMIT 1`, partyID, lat, lng).Scan(&nearestVID)
		if nearestVID == "" {
			http.Error(w, `{"error":"no available drivers nearby"}`, http.StatusNotFound)
			return
		}
		req.VolunteerID = nearestVID
	}

	// Calculate distance
	var pickupLat, pickupLng float64
	db.QueryRow("SELECT pickup_latitude, pickup_longitude FROM gotv_ride_requests WHERE request_id=$1 AND party_id=$2", id, partyID).Scan(&pickupLat, &pickupLng)

	var driverLat, driverLng sql.NullFloat64
	db.QueryRow("SELECT latitude, longitude FROM gotv_volunteers WHERE volunteer_id=$1 AND party_id=$2", req.VolunteerID, partyID).Scan(&driverLat, &driverLng)

	var distKm float64
	if driverLat.Valid && driverLng.Valid {
		distKm = gotvHaversine(pickupLat, pickupLng, driverLat.Float64, driverLng.Float64)
	}

	db.Exec("UPDATE gotv_ride_requests SET volunteer_id=$1, status='matched', matched_at=NOW(), distance_km=$2 WHERE request_id=$3 AND party_id=$4",
		req.VolunteerID, distKm, id, partyID)

	gotvAudit(partyID, username, "match_ride", "ride", id, map[string]string{"volunteer_id": req.VolunteerID}, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "matched", "volunteer_id": req.VolunteerID, "distance_km": math.Round(distKm*100) / 100,
	})
}

func handleGOTVUpdateRideStatus(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	validStatuses := map[string]bool{"en_route": true, "picked_up": true, "dropped_off": true, "cancelled": true, "no_show": true}
	if !validStatuses[req.Status] {
		http.Error(w, `{"error":"invalid status"}`, http.StatusBadRequest)
		return
	}

	// Update timestamp columns based on status
	switch req.Status {
	case "picked_up":
		db.Exec("UPDATE gotv_ride_requests SET status=$1, picked_up_at=NOW() WHERE request_id=$2 AND party_id=$3", req.Status, id, partyID)
	case "dropped_off":
		db.Exec("UPDATE gotv_ride_requests SET status=$1, dropped_off_at=NOW() WHERE request_id=$2 AND party_id=$3", req.Status, id, partyID)
		// Increment driver's rides_given
		var vid sql.NullString
		db.QueryRow("SELECT volunteer_id FROM gotv_ride_requests WHERE request_id=$1", id).Scan(&vid)
		if vid.Valid {
			db.Exec("UPDATE gotv_volunteers SET rides_given=rides_given+1 WHERE volunteer_id=$1", vid.String)
		}
	default:
		db.Exec("UPDATE gotv_ride_requests SET status=$1 WHERE request_id=$2 AND party_id=$3", req.Status, id, partyID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": req.Status})
}

// ─── Turnout Dashboard (Public — aggregate only) ───────────────────────────

func handleGOTVTurnout(w http.ResponseWriter, r *http.Request) {
	electionID := mux.Vars(r)["election_id"]

	// Aggregate turnout per state from BVAS accreditation data
	rows, err := db.Query(`
		SELECT s.code, s.name, s.geo_zone,
			COALESCE(SUM(pu.registered_voters), 0) as registered,
			COALESCE(SUM(ts.accredited_count), 0) as accredited
		FROM states s
		LEFT JOIN lgas l ON l.state_code = s.code
		LEFT JOIN wards w ON w.lga_code = l.code
		LEFT JOIN polling_units pu ON pu.ward_code = w.code
		LEFT JOIN gotv_turnout_snapshots ts ON ts.polling_unit_code = pu.code AND ts.election_id = $1
		GROUP BY s.code, s.name, s.geo_zone
		ORDER BY s.name`, electionID)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var states []map[string]interface{}
	var totalReg, totalAcc int
	for rows.Next() {
		var code, name, geoZone string
		var registered, accredited int
		rows.Scan(&code, &name, &geoZone, &registered, &accredited)
		pct := gotvSafeDiv(float64(accredited), float64(registered)) * 100
		states = append(states, map[string]interface{}{
			"state_code": code, "state_name": name, "geo_zone": geoZone,
			"registered_voters": registered, "accredited": accredited,
			"turnout_pct": math.Round(pct*100) / 100,
		})
		totalReg += registered
		totalAcc += accredited
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"election_id":     electionID,
		"states":          states,
		"national_total":  totalReg,
		"national_accredited": totalAcc,
		"national_turnout_pct": math.Round(gotvSafeDiv(float64(totalAcc), float64(totalReg))*10000) / 100,
		"snapshot_at":     time.Now(),
	})
}

func handleGOTVTurnoutByState(w http.ResponseWriter, r *http.Request) {
	electionID := mux.Vars(r)["election_id"]
	stateCode := mux.Vars(r)["state_code"]

	rows, err := db.Query(`
		SELECT l.code, l.name,
			COALESCE(SUM(pu.registered_voters), 0) as registered,
			COALESCE(SUM(ts.accredited_count), 0) as accredited
		FROM lgas l
		LEFT JOIN wards w ON w.lga_code = l.code
		LEFT JOIN polling_units pu ON pu.ward_code = w.code
		LEFT JOIN gotv_turnout_snapshots ts ON ts.polling_unit_code = pu.code AND ts.election_id = $1
		WHERE l.state_code = $2
		GROUP BY l.code, l.name
		ORDER BY l.name`, electionID, stateCode)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var lgas []map[string]interface{}
	for rows.Next() {
		var code, name string
		var registered, accredited int
		rows.Scan(&code, &name, &registered, &accredited)
		pct := gotvSafeDiv(float64(accredited), float64(registered)) * 100
		lgas = append(lgas, map[string]interface{}{
			"lga_code": code, "lga_name": name,
			"registered_voters": registered, "accredited": accredited,
			"turnout_pct": math.Round(pct*100) / 100,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"election_id": electionID,
		"state_code":  stateCode,
		"lgas":        lgas,
	})
}

// ─── Party Dashboard ───────────────────────────────────────────────────────

func handleGOTVDashboard(w http.ResponseWriter, r *http.Request) {
	partyID, _, partyName := getGOTVParty(r)

	var totalContacts, pledgedContacts, confirmedContacts, optedOut int
	var totalVolunteers, activeVolunteers, totalCampaigns, activeCampaigns int
	var totalRides, pendingRides, completedRides int

	db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE", partyID).Scan(&totalContacts)
	db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND voter_status='pledged'", partyID).Scan(&pledgedContacts)
	db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND voter_status='confirmed'", partyID).Scan(&confirmedContacts)
	db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=TRUE", partyID).Scan(&optedOut)
	db.QueryRow("SELECT COUNT(*) FROM gotv_volunteers WHERE party_id=$1", partyID).Scan(&totalVolunteers)
	db.QueryRow("SELECT COUNT(*) FROM gotv_volunteers WHERE party_id=$1 AND is_active=TRUE", partyID).Scan(&activeVolunteers)
	db.QueryRow("SELECT COUNT(*) FROM gotv_campaigns WHERE party_id=$1", partyID).Scan(&totalCampaigns)
	db.QueryRow("SELECT COUNT(*) FROM gotv_campaigns WHERE party_id=$1 AND status='active'", partyID).Scan(&activeCampaigns)
	db.QueryRow("SELECT COUNT(*) FROM gotv_ride_requests WHERE party_id=$1", partyID).Scan(&totalRides)
	db.QueryRow("SELECT COUNT(*) FROM gotv_ride_requests WHERE party_id=$1 AND status='pending'", partyID).Scan(&pendingRides)
	db.QueryRow("SELECT COUNT(*) FROM gotv_ride_requests WHERE party_id=$1 AND status='dropped_off'", partyID).Scan(&completedRides)

	// Geo breakdown
	rows, _ := db.Query("SELECT state_code, COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE AND state_code IS NOT NULL GROUP BY state_code ORDER BY COUNT(*) DESC LIMIT 10", partyID)
	var geoBreakdown []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var state string
			var cnt int
			rows.Scan(&state, &cnt)
			geoBreakdown = append(geoBreakdown, map[string]interface{}{"state": state, "contacts": cnt})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"party_name": partyName,
		"contacts": map[string]int{
			"total": totalContacts, "pledged": pledgedContacts, "confirmed": confirmedContacts, "opted_out": optedOut,
		},
		"volunteers": map[string]int{
			"total": totalVolunteers, "active": activeVolunteers,
		},
		"campaigns": map[string]int{
			"total": totalCampaigns, "active": activeCampaigns,
		},
		"rides": map[string]int{
			"total": totalRides, "pending": pendingRides, "completed": completedRides,
		},
		"geo_breakdown": geoBreakdown,
	})
}

func handleGOTVOutreachAnalytics(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)

	// Outreach by channel
	rows, _ := db.Query("SELECT channel, COUNT(*), SUM(CASE WHEN status='delivered' THEN 1 ELSE 0 END), SUM(CASE WHEN status='responded' THEN 1 ELSE 0 END) FROM gotv_outreach_log WHERE party_id=$1 GROUP BY channel", partyID)
	var channelAnalytics []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var ch string
			var total, delivered, responded int
			rows.Scan(&ch, &total, &delivered, &responded)
			channelAnalytics = append(channelAnalytics, map[string]interface{}{
				"channel": ch, "total": total, "delivered": delivered, "responded": responded,
				"delivery_rate": gotvSafeDiv(float64(delivered), float64(total)),
				"response_rate": gotvSafeDiv(float64(responded), float64(delivered)),
			})
		}
	}

	// Daily outreach trend (last 14 days)
	trendRows, _ := db.Query(`SELECT DATE(sent_at) as day, COUNT(*) FROM gotv_outreach_log WHERE party_id=$1 AND sent_at > NOW() - INTERVAL '14 days' GROUP BY DATE(sent_at) ORDER BY day`, partyID)
	var dailyTrend []map[string]interface{}
	if trendRows != nil {
		defer trendRows.Close()
		for trendRows.Next() {
			var day time.Time
			var cnt int
			trendRows.Scan(&day, &cnt)
			dailyTrend = append(dailyTrend, map[string]interface{}{"date": day.Format("2006-01-02"), "outreach_count": cnt})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"channel_analytics": channelAnalytics,
		"daily_trend":       dailyTrend,
	})
}

func handleGOTVGeoAnalytics(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)

	rows, _ := db.Query(`
		SELECT c.state_code, s.name,
			COUNT(c.id) as contacts,
			SUM(CASE WHEN c.voter_status='pledged' THEN 1 ELSE 0 END) as pledged,
			SUM(CASE WHEN c.voter_status='confirmed' THEN 1 ELSE 0 END) as confirmed
		FROM gotv_contacts c
		LEFT JOIN states s ON s.code = c.state_code
		WHERE c.party_id=$1 AND c.opted_out=FALSE AND c.state_code IS NOT NULL
		GROUP BY c.state_code, s.name
		ORDER BY contacts DESC`, partyID)
	var geoStats []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var code string
			var name sql.NullString
			var contacts, pledged, confirmed int
			rows.Scan(&code, &name, &contacts, &pledged, &confirmed)
			geoStats = append(geoStats, map[string]interface{}{
				"state_code": code, "state_name": gotvNullStr(name),
				"contacts": contacts, "pledged": pledged, "confirmed": confirmed,
				"pledge_rate": gotvSafeDiv(float64(pledged), float64(contacts)),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"geo_analytics": geoStats})
}

// ─── Campaign lifecycle: delete / resume ──────────────────────────────────

func handleGOTVDeleteCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	db.Exec("DELETE FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2", id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func handleGOTVResumeCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	db.Exec("UPDATE gotv_campaigns SET status='active', updated_at=NOW() WHERE campaign_id=$1 AND party_id=$2 AND status='paused'", id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "active"})
}

// ─── Pledge status update ──────────────────────────────────────────────────

func handleGOTVUpdatePledgeStatus(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		Status string `json:"status"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Status == "" {
		http.Error(w, `{"error":"status required"}`, http.StatusBadRequest)
		return
	}
	db.Exec("UPDATE gotv_pledges SET status=$1 WHERE pledge_id=$2 AND party_id=$3", req.Status, id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": req.Status})
}

// ─── Geo map layers ────────────────────────────────────────────────────────

func handleGOTVGeoVolunteers(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	rows, _ := db.Query("SELECT volunteer_id, full_name, role, latitude, longitude, is_active FROM gotv_volunteers WHERE party_id=$1 AND latitude IS NOT NULL", partyID)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var vid, name, role string
			var lat, lng float64
			var active bool
			if rows.Scan(&vid, &name, &role, &lat, &lng, &active) == nil {
				out = append(out, map[string]interface{}{
					"volunteer_id": vid, "full_name": name, "role": role,
					"latitude": lat, "longitude": lng, "is_active": active,
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"volunteers": out})
}

func handleGOTVGeoRides(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	rows, _ := db.Query("SELECT request_id, pickup_latitude, pickup_longitude, status FROM gotv_ride_requests WHERE party_id=$1", partyID)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var rid, status string
			var lat, lng float64
			if rows.Scan(&rid, &lat, &lng, &status) == nil {
				out = append(out, map[string]interface{}{
					"request_id": rid, "latitude": lat, "longitude": lng, "status": status,
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"rides": out})
}

func handleGOTVGeoCoverage(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	rows, _ := db.Query(`
		SELECT c.ward_code, COUNT(DISTINCT c.contact_id) as contacts, COUNT(DISTINCT v.volunteer_id) as volunteers
		FROM gotv_contacts c
		LEFT JOIN gotv_volunteers v ON v.assigned_ward = c.ward_code AND v.party_id = c.party_id
		WHERE c.party_id=$1 AND c.ward_code IS NOT NULL
		GROUP BY c.ward_code`, partyID)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var ward string
			var contacts, volunteers int
			if rows.Scan(&ward, &contacts, &volunteers) == nil {
				out = append(out, map[string]interface{}{
					"ward_code": ward, "contacts": contacts, "volunteers": volunteers,
					"coverage_ratio": gotvSafeDiv(float64(volunteers), float64(contacts)),
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"coverage": out})
}

func handleGOTVGeoTrails(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	volunteerID := r.URL.Query().Get("volunteer_id")
	query := "SELECT knock_id, volunteer_id, latitude, longitude, result, knocked_at FROM gotv_door_knocks WHERE party_id=$1"
	args := []interface{}{partyID}
	if volunteerID != "" {
		query += " AND volunteer_id=$2"
		args = append(args, volunteerID)
	}
	query += " ORDER BY knocked_at DESC LIMIT 500"
	rows, _ := db.Query(query, args...)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var kid, vid, result string
			var lat, lng float64
			var knockedAt time.Time
			if rows.Scan(&kid, &vid, &lat, &lng, &result, &knockedAt) == nil {
				out = append(out, map[string]interface{}{
					"knock_id": kid, "volunteer_id": vid, "latitude": lat, "longitude": lng,
					"result": result, "knocked_at": knockedAt,
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"trails": out})
}

// ─── Canvass workflow ──────────────────────────────────────────────────────

func handleGOTVWalklist(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	volunteerID := r.URL.Query().Get("volunteer_id")

	var ward sql.NullString
	db.QueryRow("SELECT assigned_ward FROM gotv_volunteers WHERE volunteer_id=$1 AND party_id=$2", volunteerID, partyID).Scan(&ward)

	query := `SELECT contact_id, state_code, lga_code, ward_code, polling_unit_code, voter_status
		FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE`
	args := []interface{}{partyID}
	if ward.Valid {
		query += " AND ward_code=$2"
		args = append(args, ward.String)
	}
	query += " ORDER BY contact_id LIMIT 50"

	rows, _ := db.Query(query, args...)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var cid, status string
			var state, lga, wcode, pu sql.NullString
			if rows.Scan(&cid, &state, &lga, &wcode, &pu, &status) == nil {
				out = append(out, map[string]interface{}{
					"contact_id": cid, "state_code": gotvNullStr(state), "lga_code": gotvNullStr(lga),
					"ward_code": gotvNullStr(wcode), "polling_unit_code": gotvNullStr(pu), "voter_status": status,
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"walklist": out})
}

func handleGOTVRecordDoorKnock(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)

	var req struct {
		VolunteerID string  `json:"volunteer_id"`
		ContactID   string  `json:"contact_id"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Result      string  `json:"result"`
		Notes       string  `json:"notes"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Result == "" {
		req.Result = "not_home"
	}

	knockID := "gotv-knock-" + uuid.New().String()[:8]
	db.Exec(`INSERT INTO gotv_door_knocks (knock_id, party_id, volunteer_id, contact_id, latitude, longitude, result, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		knockID, partyID, req.VolunteerID, gotvNullIfEmpty(req.ContactID), req.Latitude, req.Longitude, req.Result, gotvNullIfEmpty(req.Notes))
	db.Exec("UPDATE gotv_volunteers SET doors_knocked=doors_knocked+1 WHERE volunteer_id=$1", req.VolunteerID)
	if req.ContactID != "" && (req.Result == "pledged" || req.Result == "confirmed") {
		db.Exec("UPDATE gotv_contacts SET voter_status=$1, updated_at=NOW() WHERE contact_id=$2 AND party_id=$3", req.Result, req.ContactID, partyID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"knock_id": knockID})
}

func handleGOTVStartShift(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	var req struct {
		VolunteerID string  `json:"volunteer_id"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	shiftID := "gotv-shift-" + uuid.New().String()[:8]
	db.Exec(`INSERT INTO gotv_shifts (shift_id, party_id, volunteer_id, start_latitude, start_longitude) VALUES ($1,$2,$3,$4,$5)`,
		shiftID, partyID, req.VolunteerID, req.Latitude, req.Longitude)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"shift_id": shiftID})
}

func handleGOTVEndShift(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	var req struct {
		VolunteerID string  `json:"volunteer_id"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	db.Exec(`UPDATE gotv_shifts SET ended_at=NOW(), end_latitude=$1, end_longitude=$2
		WHERE volunteer_id=$3 AND party_id=$4 AND ended_at IS NULL`,
		req.Latitude, req.Longitude, req.VolunteerID, partyID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "shift_ended"})
}

// ─── Volunteer vetting pipeline ────────────────────────────────────────────

func handleGOTVVettingPipeline(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	status := r.URL.Query().Get("status")
	query := "SELECT volunteer_id, full_name, role, vetting_status, created_at FROM gotv_volunteers WHERE party_id=$1"
	args := []interface{}{partyID}
	if status != "" {
		query += " AND vetting_status=$2"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC LIMIT 200"
	rows, _ := db.Query(query, args...)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var vid, name, role, vstatus string
			var createdAt time.Time
			if rows.Scan(&vid, &name, &role, &vstatus, &createdAt) == nil {
				out = append(out, map[string]interface{}{
					"volunteer_id": vid, "full_name": name, "role": role,
					"vetting_status": vstatus, "created_at": createdAt,
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"pipeline": out})
}

func handleGOTVGetVolunteerVetting(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var vstatus string
	var ninVerifiedAt, trainingAt, approvedAt sql.NullTime
	var rejectedReason, suspendedReason sql.NullString
	err := db.QueryRow(`SELECT vetting_status, nin_verified_at, training_completed_at, approved_at, rejected_reason, suspended_reason
		FROM gotv_volunteers WHERE volunteer_id=$1 AND party_id=$2`, id, partyID).
		Scan(&vstatus, &ninVerifiedAt, &trainingAt, &approvedAt, &rejectedReason, &suspendedReason)
	if err != nil {
		http.Error(w, `{"error":"volunteer not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"volunteer_id": id, "vetting_status": vstatus,
		"nin_verified_at": gotvNullTime(ninVerifiedAt), "training_completed_at": gotvNullTime(trainingAt),
		"approved_at": gotvNullTime(approvedAt), "rejected_reason": gotvNullStr(rejectedReason),
		"suspended_reason": gotvNullStr(suspendedReason),
	})
}

func handleGOTVVerifyVolunteerNIN(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		NIN    string `json:"nin"`
		Result string `json:"result"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	ninEnc, err := gotvEncrypt(req.NIN)
	if err == nil {
		db.Exec("UPDATE gotv_volunteers SET nin_encrypted=$1 WHERE volunteer_id=$2 AND party_id=$3", ninEnc, id, partyID)
	}
	if req.Result == "verified" {
		db.Exec("UPDATE gotv_volunteers SET vetting_status='nin_verified', nin_verified_at=NOW() WHERE volunteer_id=$1 AND party_id=$2", id, partyID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "nin_checked"})
}

func handleGOTVCompleteVolunteerTraining(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	db.Exec("UPDATE gotv_volunteers SET vetting_status='trained', training_completed_at=NOW() WHERE volunteer_id=$1 AND party_id=$2", id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "trained"})
}

func handleGOTVApproveVolunteer(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	db.Exec("UPDATE gotv_volunteers SET vetting_status='approved', approved_at=NOW() WHERE volunteer_id=$1 AND party_id=$2", id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "approved"})
}

func handleGOTVRejectVolunteer(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	db.Exec("UPDATE gotv_volunteers SET vetting_status='rejected', rejected_reason=$1 WHERE volunteer_id=$2 AND party_id=$3", req.Reason, id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "rejected"})
}

func handleGOTVSuspendVolunteer(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	db.Exec("UPDATE gotv_volunteers SET vetting_status='suspended', suspended_reason=$1, is_active=FALSE WHERE volunteer_id=$2 AND party_id=$3", req.Reason, id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "suspended"})
}

// ─── Location assignment ───────────────────────────────────────────────────

func handleGOTVAssignVolunteerLocation(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		State string `json:"assigned_state"`
		LGA   string `json:"assigned_lga"`
		Ward  string `json:"assigned_ward"`
		PU    string `json:"assigned_polling_unit"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	db.Exec(`UPDATE gotv_volunteers SET assigned_state=$1, assigned_lga=$2, assigned_ward=$3, assigned_polling_unit=$4
		WHERE volunteer_id=$5 AND party_id=$6`,
		gotvNullIfEmpty(req.State), gotvNullIfEmpty(req.LGA), gotvNullIfEmpty(req.Ward), gotvNullIfEmpty(req.PU), id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "assigned"})
}

func handleGOTVBulkAssignLocations(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	var req struct {
		Assignments []struct {
			VolunteerID string `json:"volunteer_id"`
			State       string `json:"assigned_state"`
			LGA         string `json:"assigned_lga"`
			Ward        string `json:"assigned_ward"`
			PU          string `json:"assigned_polling_unit"`
		} `json:"assignments"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	for _, a := range req.Assignments {
		db.Exec(`UPDATE gotv_volunteers SET assigned_state=$1, assigned_lga=$2, assigned_ward=$3, assigned_polling_unit=$4
			WHERE volunteer_id=$5 AND party_id=$6`,
			gotvNullIfEmpty(a.State), gotvNullIfEmpty(a.LGA), gotvNullIfEmpty(a.Ward), gotvNullIfEmpty(a.PU), a.VolunteerID, partyID)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"assigned": len(req.Assignments)})
}

func handleGOTVAutoAssignLocations(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)

	rows, _ := db.Query("SELECT DISTINCT ward_code, state_code, lga_code FROM gotv_contacts WHERE party_id=$1 AND ward_code IS NOT NULL LIMIT 100", partyID)
	type wardRef struct{ Ward, State, LGA string }
	var wards []wardRef
	if rows != nil {
		for rows.Next() {
			var wref wardRef
			if rows.Scan(&wref.Ward, &wref.State, &wref.LGA) == nil {
				wards = append(wards, wref)
			}
		}
		rows.Close()
	}

	volRows, _ := db.Query("SELECT volunteer_id FROM gotv_volunteers WHERE party_id=$1 AND assigned_ward IS NULL AND is_active=TRUE", partyID)
	var volunteerIDs []string
	if volRows != nil {
		for volRows.Next() {
			var vid string
			if volRows.Scan(&vid) == nil {
				volunteerIDs = append(volunteerIDs, vid)
			}
		}
		volRows.Close()
	}

	assigned := 0
	for i, vid := range volunteerIDs {
		if len(wards) == 0 {
			break
		}
		w := wards[i%len(wards)]
		db.Exec("UPDATE gotv_volunteers SET assigned_state=$1, assigned_lga=$2, assigned_ward=$3 WHERE volunteer_id=$4 AND party_id=$5",
			w.State, w.LGA, w.Ward, vid, partyID)
		assigned++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"assigned": assigned})
}

func handleGOTVLocationCapacity(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	state := r.URL.Query().Get("state")

	query := `SELECT c.state_code, COUNT(DISTINCT c.contact_id) as contacts, COUNT(DISTINCT v.volunteer_id) as volunteers
		FROM gotv_contacts c
		LEFT JOIN gotv_volunteers v ON v.assigned_state = c.state_code AND v.party_id = c.party_id
		WHERE c.party_id=$1 AND c.state_code IS NOT NULL`
	args := []interface{}{partyID}
	if state != "" {
		query += " AND c.state_code=$2"
		args = append(args, state)
	}
	query += " GROUP BY c.state_code"

	rows, _ := db.Query(query, args...)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var st string
			var contacts, volunteers int
			if rows.Scan(&st, &contacts, &volunteers) == nil {
				out = append(out, map[string]interface{}{
					"state_code": st, "contacts": contacts, "volunteers": volunteers,
					"contacts_per_volunteer": gotvSafeDiv(float64(contacts), float64(volunteers)),
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"capacity": out})
}

// ─── Tasks ──────────────────────────────────────────────────────────────────

func handleGOTVListTasks(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	status := r.URL.Query().Get("status")
	volunteerID := r.URL.Query().Get("volunteer_id")

	query := "SELECT task_id, volunteer_id, task_type, description, target_count, completed_count, status, created_at FROM gotv_tasks WHERE party_id=$1"
	args := []interface{}{partyID}
	idx := 2
	if status != "" {
		query += fmt.Sprintf(" AND status=$%d", idx)
		args = append(args, status)
		idx++
	}
	if volunteerID != "" {
		query += fmt.Sprintf(" AND volunteer_id=$%d", idx)
		args = append(args, volunteerID)
		idx++
	}
	query += " ORDER BY created_at DESC LIMIT 200"

	rows, _ := db.Query(query, args...)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var tid, ttype, tstatus string
			var vid sql.NullString
			var desc sql.NullString
			var target, completed int
			var createdAt time.Time
			if rows.Scan(&tid, &vid, &ttype, &desc, &target, &completed, &tstatus, &createdAt) == nil {
				out = append(out, map[string]interface{}{
					"task_id": tid, "volunteer_id": gotvNullStr(vid), "task_type": ttype,
					"description": gotvNullStr(desc), "target_count": target, "completed_count": completed,
					"status": tstatus, "created_at": createdAt,
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"tasks": out})
}

func handleGOTVCreateTask(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	var req struct {
		TaskType    string `json:"task_type"`
		Description string `json:"description"`
		State       string `json:"target_state"`
		LGA         string `json:"target_lga"`
		Ward        string `json:"target_ward"`
		TargetCount int    `json:"target_count"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.TaskType == "" {
		req.TaskType = "canvass"
	}
	taskID := "gotv-task-" + uuid.New().String()[:8]
	db.Exec(`INSERT INTO gotv_tasks (task_id, party_id, task_type, description, target_state, target_lga, target_ward, target_count)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		taskID, partyID, req.TaskType, gotvNullIfEmpty(req.Description), gotvNullIfEmpty(req.State), gotvNullIfEmpty(req.LGA), gotvNullIfEmpty(req.Ward), req.TargetCount)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"task_id": taskID})
}

func handleGOTVAssignTask(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		VolunteerID string `json:"volunteer_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	db.Exec("UPDATE gotv_tasks SET volunteer_id=$1, status='assigned', updated_at=NOW() WHERE task_id=$2 AND party_id=$3", req.VolunteerID, id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "assigned"})
}

func handleGOTVUpdateTaskStatus(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		Status         string `json:"status"`
		CompletedCount int    `json:"completed_count"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	db.Exec("UPDATE gotv_tasks SET status=$1, completed_count=$2, updated_at=NOW() WHERE task_id=$3 AND party_id=$4",
		req.Status, req.CompletedCount, id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": req.Status})
}

func handleGOTVAutoAssignTasks(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)

	taskRows, _ := db.Query("SELECT task_id FROM gotv_tasks WHERE party_id=$1 AND status='unassigned'", partyID)
	var taskIDs []string
	if taskRows != nil {
		for taskRows.Next() {
			var tid string
			if taskRows.Scan(&tid) == nil {
				taskIDs = append(taskIDs, tid)
			}
		}
		taskRows.Close()
	}

	volRows, _ := db.Query("SELECT volunteer_id FROM gotv_volunteers WHERE party_id=$1 AND is_active=TRUE", partyID)
	var volunteerIDs []string
	if volRows != nil {
		for volRows.Next() {
			var vid string
			if volRows.Scan(&vid) == nil {
				volunteerIDs = append(volunteerIDs, vid)
			}
		}
		volRows.Close()
	}

	assigned := 0
	for i, tid := range taskIDs {
		if len(volunteerIDs) == 0 {
			break
		}
		vid := volunteerIDs[i%len(volunteerIDs)]
		db.Exec("UPDATE gotv_tasks SET volunteer_id=$1, status='assigned', updated_at=NOW() WHERE task_id=$2 AND party_id=$3", vid, tid, partyID)
		assigned++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"assigned": assigned})
}

// ─── Webhooks ───────────────────────────────────────────────────────────────

func handleGOTVListWebhooks(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	rows, _ := db.Query("SELECT webhook_id, url, events, is_active, created_at FROM gotv_webhooks WHERE party_id=$1", partyID)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var wid, url string
			var events pq.StringArray
			var active bool
			var createdAt time.Time
			if rows.Scan(&wid, &url, &events, &active, &createdAt) == nil {
				out = append(out, map[string]interface{}{
					"webhook_id": wid, "url": url, "events": []string(events),
					"is_active": active, "created_at": createdAt,
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"webhooks": out})
}

func handleGOTVCreateWebhook(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.URL == "" {
		http.Error(w, `{"error":"url required"}`, http.StatusBadRequest)
		return
	}

	secretBytes := make([]byte, 24)
	rand.Read(secretBytes)
	secret := hex.EncodeToString(secretBytes)

	webhookID := "gotv-webhook-" + uuid.New().String()[:8]
	db.Exec("INSERT INTO gotv_webhooks (webhook_id, party_id, url, events, secret) VALUES ($1,$2,$3,$4,$5)",
		webhookID, partyID, req.URL, pq.Array(req.Events), secret)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"webhook_id": webhookID, "secret": secret})
}

func handleGOTVDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	db.Exec("DELETE FROM gotv_webhooks WHERE webhook_id=$1 AND party_id=$2", id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// ─── Party Primaries (lightweight — no crypto/mix-net; see cmd/gotv-svc for that) ──

func handleGOTVPrimariesDashboard(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	electionID := mux.Vars(r)["election_id"]

	var totalDelegates, accredited, checkedIn, votedCount, aspirantsCleared int
	db.QueryRow("SELECT COUNT(*) FROM gotv_delegates WHERE party_id=$1 AND election_id=$2", partyID, electionID).Scan(&totalDelegates)
	db.QueryRow("SELECT COUNT(*) FROM gotv_delegates WHERE party_id=$1 AND election_id=$2 AND accreditation_status='accredited'", partyID, electionID).Scan(&accredited)
	db.QueryRow("SELECT COUNT(*) FROM gotv_delegates WHERE party_id=$1 AND election_id=$2 AND is_checked_in=TRUE", partyID, electionID).Scan(&checkedIn)
	db.QueryRow("SELECT COUNT(*) FROM gotv_delegates WHERE party_id=$1 AND election_id=$2 AND has_voted=TRUE", partyID, electionID).Scan(&votedCount)
	db.QueryRow("SELECT COUNT(*) FROM gotv_aspirants WHERE party_id=$1 AND election_id=$2 AND screening_status='cleared'", partyID, electionID).Scan(&aspirantsCleared)

	var currentRound sql.NullInt64
	db.QueryRow("SELECT round_number FROM gotv_primary_rounds WHERE party_id=$1 AND election_id=$2 AND status='active' ORDER BY round_number DESC LIMIT 1", partyID, electionID).Scan(&currentRound)

	rows, _ := db.Query(`SELECT state_code, COUNT(*), SUM(CASE WHEN accreditation_status='accredited' THEN 1 ELSE 0 END)
		FROM gotv_delegates WHERE party_id=$1 AND election_id=$2 AND state_code IS NOT NULL GROUP BY state_code`, partyID, electionID)
	var stateBreakdown []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var state string
			var cnt, acc int
			if rows.Scan(&state, &cnt, &acc) == nil {
				stateBreakdown = append(stateBreakdown, map[string]interface{}{"state": state, "count": cnt, "accredited": acc})
			}
		}
	}
	if stateBreakdown == nil {
		stateBreakdown = []map[string]interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_delegates": totalDelegates, "accredited": accredited, "checked_in": checkedIn,
		"turnout_pct": gotvSafeDiv(float64(votedCount), float64(totalDelegates)) * 100,
		"quorum_met": totalDelegates > 0 && checkedIn >= totalDelegates/2,
		"current_round": gotvNullInt(currentRound), "aspirants_cleared": aspirantsCleared,
		"state_breakdown": stateBreakdown,
	})
}

func handleGOTVPrimariesAspirants(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	electionID := r.URL.Query().Get("election_id")
	rows, _ := db.Query(`SELECT aspirant_id, full_name, position, state_of_origin, screening_status, status, manifesto_url, deposit_amount, deposit_paid
		FROM gotv_aspirants WHERE party_id=$1 AND election_id=$2 ORDER BY created_at`, partyID, electionID)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var aid, name, position, state, screening, status string
			var manifesto sql.NullString
			var deposit float64
			var depositPaid bool
			if rows.Scan(&aid, &name, &position, &state, &screening, &status, &manifesto, &deposit, &depositPaid) == nil {
				out = append(out, map[string]interface{}{
					"aspirant_id": aid, "full_name": name, "position": position, "state_of_origin": state,
					"screening_status": screening, "status": status, "manifesto_url": gotvNullStr(manifesto),
					"deposit_amount": deposit, "deposit_paid": depositPaid,
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"aspirants": out})
}

func handleGOTVPrimariesDelegates(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	electionID := r.URL.Query().Get("election_id")
	rows, _ := db.Query(`SELECT delegate_id, full_name, delegate_type, state_code, accreditation_status, has_voted, is_remote
		FROM gotv_delegates WHERE party_id=$1 AND election_id=$2 ORDER BY created_at`, partyID, electionID)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var did, name, dtype, state, accStatus string
			var hasVoted, isRemote bool
			if rows.Scan(&did, &name, &dtype, &state, &accStatus, &hasVoted, &isRemote) == nil {
				out = append(out, map[string]interface{}{
					"delegate_id": did, "full_name": name, "delegate_type": dtype, "state_code": state,
					"accreditation_status": accStatus, "has_voted": hasVoted, "is_remote": isRemote,
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"delegates": out})
}

func handleGOTVPrimariesRounds(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	electionID := mux.Vars(r)["election_id"]
	rows, _ := db.Query(`SELECT round_id, round_number, status, voting_method, total_votes, total_eligible, started_at, ended_at
		FROM gotv_primary_rounds WHERE party_id=$1 AND election_id=$2 ORDER BY round_number`, partyID, electionID)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var rid, status, method string
			var num, votes, eligible int
			var startedAt, endedAt sql.NullTime
			if rows.Scan(&rid, &num, &status, &method, &votes, &eligible, &startedAt, &endedAt) == nil {
				out = append(out, map[string]interface{}{
					"round_id": rid, "round_number": num, "status": status, "voting_method": method,
					"total_votes": votes, "total_eligible": eligible,
					"started_at": gotvNullTime(startedAt), "ended_at": gotvNullTime(endedAt),
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"rounds": out})
}

func handleGOTVPrimariesCryptoAudit(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	electionID := mux.Vars(r)["election_id"]

	var totalBallots, remoteBallots int
	db.QueryRow("SELECT COUNT(*) FROM gotv_delegates WHERE party_id=$1 AND election_id=$2 AND has_voted=TRUE", partyID, electionID).Scan(&totalBallots)
	db.QueryRow("SELECT COUNT(*) FROM gotv_delegates WHERE party_id=$1 AND election_id=$2 AND has_voted=TRUE AND is_remote=TRUE", partyID, electionID).Scan(&remoteBallots)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_keys": 0, "total_shuffles": 0, "total_ballots": totalBallots,
		"remote_ballots": remoteBallots, "decoy_ballots": 0, "verified_decryptions": 0,
	})
}

func handleGOTVPrimariesRemoteVerify(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("confirmation_code")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"verified": false, "confirmation_code": code, "message": "remote ballot verification is not enabled for this demo",
	})
}

// ─── Leaderboard ────────────────────────────────────────────────────────────

func handleGOTVLeaderboard(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	rows, _ := db.Query(`SELECT volunteer_id, full_name, role, doors_knocked, calls_made, rides_given
		FROM gotv_volunteers WHERE party_id=$1
		ORDER BY (doors_knocked + calls_made*2 + rides_given*3) DESC LIMIT $2`, partyID, limit)
	var out []map[string]interface{}
	rank := 0
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var vid, name, role string
			var doors, calls, rides int
			if rows.Scan(&vid, &name, &role, &doors, &calls, &rides) == nil {
				rank++
				badge := "bronze"
				if rank == 1 {
					badge = "gold"
				} else if rank <= 3 {
					badge = "silver"
				}
				out = append(out, map[string]interface{}{
					"volunteer_id": vid, "full_name": name, "role": role,
					"score": doors + calls*2 + rides*3, "rank": rank, "badge": badge,
					"doors_knocked": doors, "calls_made": calls, "rides_given": rides,
				})
			}
		}
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"leaderboard": out})
}

// ─── Segments ───────────────────────────────────────────────────────────────

func handleGOTVListSegments(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	rows, _ := db.Query("SELECT segment_id, name, filters, created_at FROM gotv_segments WHERE party_id=$1 ORDER BY created_at DESC", partyID)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var sid, name string
			var filtersRaw []byte
			var createdAt time.Time
			if rows.Scan(&sid, &name, &filtersRaw, &createdAt) == nil {
				var filters interface{}
				json.Unmarshal(filtersRaw, &filters)
				out = append(out, map[string]interface{}{"segment_id": sid, "name": name, "filters": filters, "created_at": createdAt})
			}
		}
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"segments": out})
}

func handleGOTVCreateSegment(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	var req struct {
		Name    string      `json:"name"`
		Filters interface{} `json:"filters"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	filtersJSON, _ := json.Marshal(req.Filters)
	segID := "gotv-segment-" + uuid.New().String()[:8]
	db.Exec("INSERT INTO gotv_segments (segment_id, party_id, name, filters) VALUES ($1,$2,$3,$4)", segID, partyID, req.Name, filtersJSON)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"segment_id": segID})
}

func handleGOTVDeleteSegment(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	db.Exec("DELETE FROM gotv_segments WHERE segment_id=$1 AND party_id=$2", id, partyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// ─── War Room ───────────────────────────────────────────────────────────────

func handleGOTVWarRoomSummary(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)

	var activeCampaigns, activeVolunteers, pendingRides, dispatchesLastHour, pledgesToday int
	db.QueryRow("SELECT COUNT(*) FROM gotv_campaigns WHERE party_id=$1 AND status='active'", partyID).Scan(&activeCampaigns)
	db.QueryRow("SELECT COUNT(*) FROM gotv_volunteers WHERE party_id=$1 AND is_active=TRUE", partyID).Scan(&activeVolunteers)
	db.QueryRow("SELECT COUNT(*) FROM gotv_ride_requests WHERE party_id=$1 AND status='pending'", partyID).Scan(&pendingRides)
	db.QueryRow("SELECT COUNT(*) FROM gotv_outreach_log WHERE party_id=$1 AND sent_at > NOW() - interval '1 hour'", partyID).Scan(&dispatchesLastHour)
	db.QueryRow("SELECT COUNT(*) FROM gotv_pledges WHERE party_id=$1 AND created_at::date = CURRENT_DATE", partyID).Scan(&pledgesToday)

	rows, _ := db.Query(`SELECT c.state_code, COUNT(DISTINCT v.volunteer_id), COUNT(DISTINCT c.contact_id),
			SUM(CASE WHEN c.voter_status='pledged' THEN 1 ELSE 0 END)
		FROM gotv_contacts c
		LEFT JOIN gotv_volunteers v ON v.assigned_state = c.state_code AND v.party_id = c.party_id
		WHERE c.party_id=$1 AND c.state_code IS NOT NULL GROUP BY c.state_code`, partyID)
	var coverage []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var state string
			var volunteers, contacts, pledges int
			if rows.Scan(&state, &volunteers, &contacts, &pledges) == nil {
				coverage = append(coverage, map[string]interface{}{
					"state_code": state, "volunteers": volunteers, "contacts": contacts, "pledges": pledges,
					"coverage_pct": gotvSafeDiv(float64(volunteers), float64(contacts)) * 100,
				})
			}
		}
	}
	if coverage == nil {
		coverage = []map[string]interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"timestamp": time.Now(),
		"ops": map[string]interface{}{
			"active_campaigns": activeCampaigns, "active_volunteers": activeVolunteers, "pending_rides": pendingRides,
			"dispatches_last_hour": dispatchesLastHour, "pledges_today": pledgesToday,
		},
		"alerts":   []map[string]interface{}{},
		"coverage": coverage,
	})
}

func handleGOTVWarRoomStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming unsupported"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, "data: {}\n\n")
			flusher.Flush()
		}
	}
}

func handleGOTVWarRoomAIAlerts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"alerts": []map[string]interface{}{}})
}

// ─── Analytics extras ────────────────────────────────────────────────────────

func handleGOTVAIVariants(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	rows, _ := db.Query(`SELECT variant_id, base_message, variant_text, target_state, channel, variant_index
		FROM gotv_ai_variants WHERE party_id=$1 ORDER BY created_at DESC LIMIT 100`, partyID)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var vid, baseMsg, text, state, channel string
			var idx int
			if rows.Scan(&vid, &baseMsg, &text, &state, &channel, &idx) == nil {
				out = append(out, map[string]interface{}{
					"variant_id": vid, "base_message": baseMsg, "variant_text": text,
					"target_state": state, "channel": channel, "variant_index": idx,
				})
			}
		}
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"variants": out})
}

func handleGOTVROIChannels(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	rows, _ := db.Query(`SELECT channel,
			SUM(CASE WHEN status IN ('sent','delivered','read','responded') THEN 1 ELSE 0 END),
			SUM(CASE WHEN status IN ('delivered','read','responded') THEN 1 ELSE 0 END),
			SUM(CASE WHEN status='responded' THEN 1 ELSE 0 END)
		FROM gotv_outreach_log WHERE party_id=$1 GROUP BY channel`, partyID)
	var out []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var channel string
			var sent, delivered, responded int
			if rows.Scan(&channel, &sent, &delivered, &responded) == nil {
				responseRate := gotvSafeDiv(float64(responded), float64(delivered))
				recommendation := "INSUFFICIENT_DATA: not enough outreach volume yet"
				if sent >= 10 {
					switch {
					case responseRate >= 0.2:
						recommendation = "SCALE_UP: response rate is strong, increase volume"
					case responseRate >= 0.05:
						recommendation = "OPTIMIZE: response rate is average, test new messaging"
					default:
						recommendation = "REDUCE: response rate is low, reallocate budget"
					}
				}
				out = append(out, map[string]interface{}{
					"channel": channel, "total_sent": sent, "total_delivered": delivered, "total_responded": responded,
					"total_pledged": 0, "total_cost_kobo": 0, "cost_per_send": 0, "cost_per_deliver": 0,
					"cost_per_respond": 0, "cost_per_pledge": 0,
					"delivery_rate":  gotvSafeDiv(float64(delivered), float64(sent)),
					"response_rate":  responseRate,
					"recommendation": recommendation,
				})
			}
		}
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"channels": out})
}

func handleGOTVTurnoutPredict(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"predictions": []map[string]interface{}{}})
}

// ─── GOTV Ledger (placeholder — no real financial ledger wired up for GOTV) ──

func handleGOTVLedgerAccounts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"accounts": []map[string]interface{}{}})
}

func handleGOTVLedgerHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"transfers": []map[string]interface{}{}})
}

func handleGOTVLedgerReconcile(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"party_id": partyID, "account_count": 0, "transfer_count": 0, "posted": 0, "pending": 0, "voided": 0,
		"total_posted_ngn": 0, "balanced": true, "variance": 0,
	})
}

// ─── GOTV Blockchain (placeholder — no real chain wired up for GOTV) ────────

func handleGOTVBlockchainStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_blocks": 0, "total_transactions": 0, "verified_tx": 0, "merkle_anchors": 0,
		"latest_block_hash": "", "latest_block_time": nil, "chain_integrity": true,
	})
}

func handleGOTVBlockchainBlocks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"blocks": []map[string]interface{}{}})
}

func handleGOTVBlockchainAnchor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "anchored", "block_hash": ""})
}

// ─── Platform tab: teams, experiments, NL query, simulation ────────────────

func handleGOTVTeamsLeaderboard(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	rows, _ := db.Query(`SELECT COALESCE(assigned_ward,'Unassigned'), COUNT(*), SUM(doors_knocked), SUM(calls_made), SUM(rides_given)
		FROM gotv_volunteers WHERE party_id=$1 GROUP BY COALESCE(assigned_ward,'Unassigned')`, partyID)
	var out []map[string]interface{}
	rank := 0
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var ward string
			var members, doors, calls, rides int
			if rows.Scan(&ward, &members, &doors, &calls, &rides) == nil {
				rank++
				out = append(out, map[string]interface{}{
					"name": ward, "members": members, "total_doors": doors, "total_calls": calls,
					"total_rides": rides, "points": doors + calls*2 + rides*3, "rank": rank,
				})
			}
		}
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"teams": out})
}

func handleGOTVExperiments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"variants": []map[string]interface{}{}})
}

func handleGOTVNLQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"answer": "Natural-language querying isn't enabled in this demo environment yet.",
	})
}

func handleGOTVSimulation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scenario        string `json:"scenario"`
		AdditionalCount int    `json:"additional_count"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scenario": req.Scenario, "additional_count": req.AdditionalCount,
		"message": "Simulation modeling isn't enabled in this demo environment yet.",
	})
}

// ─── KOH (Key Opinion Holder) indicators — placeholder, no real data model ──

func gotvKOHEmpty(key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{key: []map[string]interface{}{}})
	}
}

func handleGOTVKOHCPICompute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"cpi": 0, "components": []map[string]interface{}{}})
}

func handleGOTVKOHDemographics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"demographics": []map[string]interface{}{}})
}

func handleGOTVKOHLGADashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"tiers": []map[string]interface{}{}})
}

func handleGOTVKOHSocialSentiment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"positive": 0, "neutral": 0, "negative": 0, "trend": []map[string]interface{}{}})
}

func handleGOTVKOHShareOfVoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"share_pct": 0, "by_platform": []map[string]interface{}{}})
}

func handleGOTVKOHEndorsementScore(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"score": 0, "total_endorsements": 0})
}

func handleGOTVKOHReportGenerate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}

func handleGOTVKOHAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"cpi": 0, "sentiment_score": 0, "share_of_voice_pct": 0, "endorsement_score": 0,
	})
}

// ─── Campaign Dispatch Engine ──────────────────────────────────────────────

func dispatchCampaignOutreach(partyID int, campaignID, channelType, messageTemplate, messageVariantB string) {
	log.Info().Str("campaign", campaignID).Msg("GOTV: dispatching campaign outreach")

	// Get campaign geo targeting
	var tState, tLGA, tWard, tPU sql.NullString
	var abSplit int
	db.QueryRow("SELECT target_state, target_lga, target_ward, target_polling_unit, ab_split_pct FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2",
		campaignID, partyID).Scan(&tState, &tLGA, &tWard, &tPU, &abSplit)

	// Fetch eligible contacts in batches
	query := "SELECT contact_id, phone_encrypted FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE"
	args := []interface{}{partyID}
	idx := 2
	if tState.Valid {
		query += fmt.Sprintf(" AND state_code=$%d", idx)
		args = append(args, tState.String)
		idx++
	}
	if tLGA.Valid {
		query += fmt.Sprintf(" AND lga_code=$%d", idx)
		args = append(args, tLGA.String)
		idx++
	}
	if tWard.Valid {
		query += fmt.Sprintf(" AND ward_code=$%d", idx)
		args = append(args, tWard.String)
		idx++
	}
	if tPU.Valid {
		query += fmt.Sprintf(" AND polling_unit_code=$%d", idx)
		args = append(args, tPU.String)
		idx++
	}
	query += " ORDER BY id LIMIT 10000"

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Error().Err(err).Msg("GOTV: failed to fetch contacts for campaign")
		return
	}
	defer rows.Close()

	sent := 0
	for rows.Next() {
		var contactID, phoneEnc string
		rows.Scan(&contactID, &phoneEnc)

		// Determine A/B variant
		variant := "A"
		msg := messageTemplate
		if messageVariantB != "" && sent%100 >= abSplit {
			variant = "B"
			msg = messageVariantB
		}

		// Log outreach (actual SMS dispatch would go through production SMS gateway)
		logID := "gotv-log-" + uuid.New().String()[:8]
		db.Exec(`INSERT INTO gotv_outreach_log (log_id, campaign_id, party_id, contact_id, channel, direction, message_text, message_variant, status)
			VALUES ($1,$2,$3,$4,$5,'outbound',$6,$7,'sent')`,
			logID, campaignID, partyID, contactID, channelType, msg, variant)

		db.Exec("UPDATE gotv_contacts SET last_contacted_at=NOW(), contact_count=contact_count+1 WHERE contact_id=$1", contactID)
		sent++

		// Rate limit: 100 per second
		if sent%100 == 0 {
			time.Sleep(time.Second)
		}
	}

	// Update campaign stats
	db.Exec("UPDATE gotv_campaigns SET contacts_reached=$1, status=CASE WHEN $1>=total_contacts THEN 'completed' ELSE status END, completed_at=CASE WHEN $1>=total_contacts THEN NOW() ELSE completed_at END, updated_at=NOW() WHERE campaign_id=$2",
		sent, campaignID)

	log.Info().Str("campaign", campaignID).Int("sent", sent).Msg("GOTV: campaign dispatch complete")
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func gotvMaskPhone(phone string) string {
	if len(phone) < 7 {
		return "***"
	}
	return phone[:4] + "****" + phone[len(phone)-3:]
}

func gotvHaversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371 // Earth radius km
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

func gotvSafeDiv(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return num / den
}

func gotvNullStr(ns sql.NullString) interface{} {
	if ns.Valid {
		return ns.String
	}
	return nil
}

func gotvNullTime(nt sql.NullTime) interface{} {
	if nt.Valid {
		return nt.Time
	}
	return nil
}

func gotvNullFloat(nf sql.NullFloat64) interface{} {
	if nf.Valid {
		return nf.Float64
	}
	return nil
}

func gotvNullInt(ni sql.NullInt64) interface{} {
	if ni.Valid {
		return ni.Int64
	}
	return nil
}

func gotvNullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// seedGOTVData populates the GOTV module with realistic demo data for every
// existing party, reusing the same encryption/hashing path as the real API
// (handleGOTVCreateContact) so the rows are indistinguishable from live data.
func seedGOTVData() {
	type partyRef struct {
		ID   int
		Code string
	}
	partyRows, err := db.Query("SELECT id, code FROM parties")
	if err != nil {
		log.Warn().Err(err).Msg("seedGOTVData: parties query failed")
		return
	}
	var partyRefs []partyRef
	for partyRows.Next() {
		var p partyRef
		if partyRows.Scan(&p.ID, &p.Code) == nil {
			partyRefs = append(partyRefs, p)
		}
	}
	partyRows.Close()
	if len(partyRefs) == 0 {
		return
	}

	// Give the seeded admin account a party so gotvAuthMiddleware's
	// header-less fallback (users.party_id) resolves for the demo login.
	// Runs every boot regardless of the bulk-seed gate below.
	db.Exec("UPDATE users SET party_id=$1 WHERE username='admin' AND party_id IS NULL", partyRefs[0].ID)

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM gotv_campaigns").Scan(&count); err != nil {
		log.Warn().Err(err).Msg("seedGOTVData: gotv_campaigns query failed")
		return
	}
	if count > 0 {
		return
	}

	type geoRef struct {
		PU, Ward, LGA, State string
	}
	geoRows, err := db.Query(`
		SELECT p.code, w.code, w.lga_code, l.state_code
		FROM polling_units p
		JOIN wards w ON p.ward_code = w.code
		JOIN lgas l ON w.lga_code = l.code
		LIMIT 100
	`)
	if err != nil {
		log.Warn().Err(err).Msg("seedGOTVData: geo query failed")
		return
	}
	var geoRefs []geoRef
	for geoRows.Next() {
		var g geoRef
		if geoRows.Scan(&g.PU, &g.Ward, &g.LGA, &g.State) == nil {
			geoRefs = append(geoRefs, g)
		}
	}
	geoRows.Close()
	if len(geoRefs) == 0 {
		return
	}

	var electionID int
	_ = db.QueryRow("SELECT id FROM elections ORDER BY id LIMIT 1").Scan(&electionID)

	rng := NewSecureRng()
	campaignTypes := []string{"sms", "whatsapp", "door_to_door", "phone_bank"}
	campaignStatuses := []string{"active", "scheduled", "draft"}
	voterStatuses := []string{"unknown", "pledged", "confirmed", "declined"}
	volunteerRoles := []string{"canvasser", "coordinator", "phone_banker", "team_lead"}

	for _, party := range partyRefs {
		db.Exec(`INSERT INTO gotv_party_access (party_id, api_key_hash, created_by) VALUES ($1,$2,'seed') ON CONFLICT (party_id) DO NOTHING`,
			party.ID, gotvPhoneHash(fmt.Sprintf("seed-api-key-%d", party.ID)))

		numCampaigns := 2 + rng.Intn(2)
		campaignIDs := make([]string, 0, numCampaigns)
		for i := 0; i < numCampaigns; i++ {
			cID := "gotv-camp-" + uuid.New().String()[:8]
			campaignIDs = append(campaignIDs, cID)
			geo := geoRefs[rng.Intn(len(geoRefs))]
			db.Exec(`INSERT INTO gotv_campaigns (campaign_id, party_id, name, description, campaign_type, status, target_state, target_lga, target_ward, message_template, created_by)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'seed')`,
				cID, party.ID, fmt.Sprintf("%s Voter Outreach %d", party.Code, i+1),
				"Seeded demo campaign", campaignTypes[rng.Intn(len(campaignTypes))], campaignStatuses[rng.Intn(len(campaignStatuses))],
				geo.State, geo.LGA, geo.Ward, "Hi {{first_name}}, remember to vote on election day!")
		}

		vettingStatuses := []string{"pending", "nin_verified", "trained", "approved", "rejected"}
		numVolunteers := 3 + rng.Intn(3)
		volunteerIDs := make([]string, 0, numVolunteers)
		for i := 0; i < numVolunteers; i++ {
			vID := "gotv-vol-" + uuid.New().String()[:8]
			volunteerIDs = append(volunteerIDs, vID)
			geo := geoRefs[rng.Intn(len(geoRefs))]
			name := nigerianFirstNames[rng.Intn(len(nigerianFirstNames))] + " " + nigerianLastNames[rng.Intn(len(nigerianLastNames))]
			phone := fmt.Sprintf("080%08d", rng.Intn(100000000))
			db.Exec(`INSERT INTO gotv_volunteers (volunteer_id, party_id, full_name, phone, role, assigned_state, assigned_lga, assigned_ward, is_active, latitude, longitude, vetting_status)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,true,$9,$10,$11)`,
				vID, party.ID, name, phone, volunteerRoles[rng.Intn(len(volunteerRoles))], geo.State, geo.LGA, geo.Ward,
				4.0+rng.Float64()*10, 2.5+rng.Float64()*12, vettingStatuses[rng.Intn(len(vettingStatuses))])

			shiftID := "gotv-shift-" + uuid.New().String()[:8]
			db.Exec(`INSERT INTO gotv_shifts (shift_id, party_id, volunteer_id, start_latitude, start_longitude, end_latitude, end_longitude, started_at, ended_at)
				VALUES ($1,$2,$3,$4,$5,$4,$5,NOW() - interval '1 day', NOW() - interval '1 day' + interval '4 hours')`,
				shiftID, party.ID, vID, 4.0+rng.Float64()*10, 2.5+rng.Float64()*12)
		}

		numTasks := 2 + rng.Intn(3)
		for i := 0; i < numTasks; i++ {
			taskID := "gotv-task-" + uuid.New().String()[:8]
			geo := geoRefs[rng.Intn(len(geoRefs))]
			status := "unassigned"
			var assignedVol interface{}
			if len(volunteerIDs) > 0 && rng.Intn(2) == 0 {
				status = "assigned"
				assignedVol = volunteerIDs[rng.Intn(len(volunteerIDs))]
			}
			db.Exec(`INSERT INTO gotv_tasks (task_id, party_id, volunteer_id, task_type, description, target_state, target_lga, target_ward, target_count, status)
				VALUES ($1,$2,$3,'canvass','Seeded demo canvass task',$4,$5,$6,50,$7)`,
				taskID, party.ID, assignedVol, geo.State, geo.LGA, geo.Ward, status)
		}

		webhookID := "gotv-webhook-" + uuid.New().String()[:8]
		db.Exec(`INSERT INTO gotv_webhooks (webhook_id, party_id, url, events, secret) VALUES ($1,$2,$3,$4,$5)`,
			webhookID, party.ID, "https://example.com/gotv-webhook", pq.Array([]string{"pledge.created", "campaign.launched"}), gotvPhoneHash(webhookID))

		if electionID > 0 {
			positions := []string{"presidential", "gubernatorial", "senatorial"}
			screeningStatuses := []string{"pending", "cleared", "cleared", "disqualified"}
			numAspirants := 2 + rng.Intn(3)
			for i := 0; i < numAspirants; i++ {
				aID := "gotv-aspirant-" + uuid.New().String()[:8]
				name := nigerianFirstNames[rng.Intn(len(nigerianFirstNames))] + " " + nigerianLastNames[rng.Intn(len(nigerianLastNames))]
				geo := geoRefs[rng.Intn(len(geoRefs))]
				db.Exec(`INSERT INTO gotv_aspirants (aspirant_id, party_id, election_id, full_name, position, state_of_origin, screening_status, deposit_amount, deposit_paid)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
					aID, party.ID, electionID, name, positions[rng.Intn(len(positions))], geo.State,
					screeningStatuses[rng.Intn(len(screeningStatuses))], 5000000, rng.Intn(2) == 0)
			}

			delegateTypes := []string{"ward", "statutory", "ex_officio"}
			numDelegates := 20 + rng.Intn(20)
			for i := 0; i < numDelegates; i++ {
				dID := "gotv-delegate-" + uuid.New().String()[:8]
				name := nigerianFirstNames[rng.Intn(len(nigerianFirstNames))] + " " + nigerianLastNames[rng.Intn(len(nigerianLastNames))]
				geo := geoRefs[rng.Intn(len(geoRefs))]
				accredited := rng.Intn(3) != 0
				checkedIn := accredited && rng.Intn(2) == 0
				voted := checkedIn && rng.Intn(2) == 0
				accStatus := "pending"
				if accredited {
					accStatus = "accredited"
				}
				db.Exec(`INSERT INTO gotv_delegates (delegate_id, party_id, election_id, full_name, delegate_type, state_code, accreditation_status, is_checked_in, has_voted, is_remote)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
					dID, party.ID, electionID, name, delegateTypes[rng.Intn(len(delegateTypes))], geo.State,
					accStatus, checkedIn, voted, rng.Intn(5) == 0)
			}

			roundID := "gotv-round-" + uuid.New().String()[:8]
			db.Exec(`INSERT INTO gotv_primary_rounds (round_id, party_id, election_id, round_number, status, voting_method, total_votes, total_eligible, started_at)
				VALUES ($1,$2,$3,1,'active','in_person',$4,$5,NOW() - interval '2 hours')`,
				roundID, party.ID, electionID, numDelegates/3, numDelegates)
		}

		numContacts := 15 + rng.Intn(6)
		contactIDs := make([]string, 0, numContacts)
		for i := 0; i < numContacts; i++ {
			geo := geoRefs[rng.Intn(len(geoRefs))]
			name := nigerianFirstNames[rng.Intn(len(nigerianFirstNames))] + " " + nigerianLastNames[rng.Intn(len(nigerianLastNames))]
			phone := fmt.Sprintf("081%08d", rng.Intn(100000000))

			consentID := "gotv-consent-" + uuid.New().String()[:8]
			db.Exec(`INSERT INTO consent_records (consent_id, subject_id, purpose, legal_basis, granted_at) VALUES ($1,$2,'communication','consent',NOW())`,
				consentID, normalizePhone(phone))

			phoneEnc, err := gotvEncrypt(normalizePhone(phone))
			if err != nil {
				continue
			}
			var nameVal interface{}
			if nameEnc, err := gotvEncrypt(name); err == nil {
				nameVal = nameEnc
			}
			contactID := "gotv-contact-" + uuid.New().String()[:8]

			_, err = db.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, full_name_encrypted, state_code, lga_code, ward_code, polling_unit_code, voter_status, consent_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
				contactID, party.ID, phoneEnc, gotvPhoneHash(phone), nameVal, geo.State, geo.LGA, geo.Ward, geo.PU,
				voterStatuses[rng.Intn(len(voterStatuses))], consentID)
			if err != nil {
				continue
			}

			if rng.Intn(2) == 0 {
				pledgeID := "gotv-pledge-" + uuid.New().String()[:8]
				db.Exec(`INSERT INTO gotv_pledges (pledge_id, party_id, contact_id, election_id, pledge_type, status)
					VALUES ($1,$2,$3,$4,'will_vote','pledged')`,
					pledgeID, party.ID, contactID, electionID)
			}

			if len(campaignIDs) > 0 && len(volunteerIDs) > 0 && rng.Intn(2) == 0 {
				logID := "gotv-log-" + uuid.New().String()[:8]
				db.Exec(`INSERT INTO gotv_outreach_log (log_id, campaign_id, party_id, contact_id, volunteer_id, channel, direction, message_text, status)
					VALUES ($1,$2,$3,$4,$5,'sms','outbound','Reminder: Election day is coming up!','delivered')`,
					logID, campaignIDs[rng.Intn(len(campaignIDs))], party.ID, contactID, volunteerIDs[rng.Intn(len(volunteerIDs))])
			}

			contactIDs = append(contactIDs, contactID)
		}

		knockResults := []string{"not_home", "refused", "pledged", "confirmed", "needs_ride"}
		if len(volunteerIDs) > 0 && len(contactIDs) > 0 {
			numKnocks := 10 + rng.Intn(10)
			for i := 0; i < numKnocks; i++ {
				knockID := "gotv-knock-" + uuid.New().String()[:8]
				db.Exec(`INSERT INTO gotv_door_knocks (knock_id, party_id, volunteer_id, contact_id, latitude, longitude, result, knocked_at)
					VALUES ($1,$2,$3,$4,$5,$6,$7,NOW() - ($8::text || ' hours')::interval)`,
					knockID, party.ID, volunteerIDs[rng.Intn(len(volunteerIDs))], contactIDs[rng.Intn(len(contactIDs))],
					4.0+rng.Float64()*10, 2.5+rng.Float64()*12, knockResults[rng.Intn(len(knockResults))], rng.Intn(72))
			}
		}
	}

	if electionID > 0 {
		for _, geo := range geoRefs {
			var registered int
			db.QueryRow("SELECT registered_voters FROM polling_units WHERE code=$1", geo.PU).Scan(&registered)
			accredited := 0
			turnoutPct := 0.0
			if registered > 0 {
				accredited = rng.Intn(registered)
				turnoutPct = float64(accredited) / float64(registered) * 100
			}
			db.Exec(`INSERT INTO gotv_turnout_snapshots (election_id, polling_unit_code, accredited_count, registered_voters, turnout_pct) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
				electionID, geo.PU, accredited, registered, turnoutPct)
		}
	}

	log.Info().Int("parties", len(partyRefs)).Int("contacts_per_party", 15).Msg("GOTV demo data seeded")
}
