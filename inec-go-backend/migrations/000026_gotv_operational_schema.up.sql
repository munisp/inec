-- Migration 000026: GOTV operational schema reconciliation
--
-- The GOTV HTTP service persists scoring, inbox, crowd-estimation, preference,
-- WhatsApp, and federated-learning state. These tables were referenced by
-- handlers but were absent from the versioned schema, causing endpoint failures
-- on a clean deployment.

-- Canonical base GOTV schema migrated from internal/gotv.Service.InitTables.
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
		channel TEXT NOT NULL CHECK(channel IN ('sms','ussd','push','whatsapp','whatsapp_interactive','email','door_knock','phone_call','log','twitter','facebook','instagram','tiktok')),
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

	CREATE TABLE IF NOT EXISTS gotv_dead_letter_queue (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		campaign_id TEXT,
		contact_id TEXT,
		channel TEXT NOT NULL,
		error_detail TEXT,
		message_body TEXT,
		phone_encrypted TEXT,
		retry_count INTEGER DEFAULT 0,
		last_error TEXT,
		resolved BOOLEAN DEFAULT FALSE,
		resolved_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_dlq_party ON gotv_dead_letter_queue(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_dlq_resolved ON gotv_dead_letter_queue(resolved);

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

-- Canonical GOTV v2 schema migrated from internal/gotv.InitV2Tables.
CREATE TABLE IF NOT EXISTS gotv_campaign_sequences (
			sequence_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			waves JSONB NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'draft',
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

CREATE TABLE IF NOT EXISTS gotv_segments (
			segment_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			filters JSONB NOT NULL DEFAULT '[]',
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

CREATE TABLE IF NOT EXISTS gotv_territories (
			territory_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL,
			volunteer_id TEXT NOT NULL,
			ward_code TEXT NOT NULL,
			contact_count INTEGER DEFAULT 0,
			status TEXT DEFAULT 'assigned',
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

CREATE TABLE IF NOT EXISTS gotv_challenges (
			challenge_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			target_metric TEXT NOT NULL,
			target_value INTEGER DEFAULT 0,
			reward_description TEXT DEFAULT '',
			starts_at TIMESTAMPTZ,
			ends_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

CREATE TABLE IF NOT EXISTS gotv_ai_variants (
			variant_id TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL DEFAULT 0,
			base_message TEXT NOT NULL,
			variant_text TEXT NOT NULL,
			target_state TEXT DEFAULT '',
			channel TEXT DEFAULT '',
			variant_index INTEGER DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

CREATE TABLE IF NOT EXISTS gotv_pledge_hashes (
			hash TEXT PRIMARY KEY,
			party_id INTEGER NOT NULL,
			election_id INTEGER NOT NULL,
			ward_code TEXT DEFAULT '',
			verified BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

CREATE TABLE IF NOT EXISTS gotv_field_reports (
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
		);

CREATE TABLE IF NOT EXISTS gotv_alliances (
			grant_id TEXT PRIMARY KEY,
			grantor_party_id INTEGER NOT NULL,
			grantee_party_id INTEGER NOT NULL,
			resource_type TEXT NOT NULL,
			ward_code TEXT DEFAULT '',
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

CREATE TABLE IF NOT EXISTS gotv_voice_calls (
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
		);

ALTER TABLE gotv_campaigns ADD COLUMN IF NOT EXISTS budget_cap_kobo BIGINT DEFAULT NULL;

ALTER TABLE gotv_campaigns ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE gotv_campaigns ADD COLUMN IF NOT EXISTS launched_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS device_token TEXT DEFAULT NULL;

ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS latitude DOUBLE PRECISION DEFAULT NULL;

ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS longitude DOUBLE PRECISION DEFAULT NULL;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS assigned_ward TEXT DEFAULT NULL;

ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS opted_out_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS last_contacted_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE gotv_outreach_log ADD COLUMN IF NOT EXISTS cost_kobo INTEGER DEFAULT 0;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS vetting_status TEXT DEFAULT 'pending';

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS nin_verified BOOLEAN DEFAULT FALSE;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS nin_verified_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS training_completed BOOLEAN DEFAULT FALSE;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS training_completed_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS background_cleared BOOLEAN DEFAULT FALSE;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS approved_by TEXT DEFAULT NULL;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS suspended_reason TEXT DEFAULT NULL;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS assigned_polling_unit TEXT DEFAULT NULL;

CREATE TABLE IF NOT EXISTS gotv_tasks (
			id SERIAL PRIMARY KEY,
			task_id TEXT UNIQUE NOT NULL,
			party_id INTEGER NOT NULL,
			task_type TEXT NOT NULL CHECK(task_type IN ('door_knock','phone_call','ride_duty','event_setup','data_collection','voter_registration','materials_distribution','monitoring')),
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			volunteer_id TEXT,
			ward_code TEXT,
			state_code TEXT,
			lga_code TEXT,
			target_count INTEGER DEFAULT 1,
			completed_count INTEGER DEFAULT 0,
			priority INTEGER DEFAULT 3 CHECK(priority BETWEEN 1 AND 5),
			status TEXT DEFAULT 'unassigned' CHECK(status IN ('unassigned','assigned','in_progress','completed','cancelled','blocked')),
			due_date DATE,
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

CREATE INDEX IF NOT EXISTS idx_gotv_tasks_party ON gotv_tasks(party_id);

CREATE INDEX IF NOT EXISTS idx_gotv_tasks_volunteer ON gotv_tasks(volunteer_id);

CREATE INDEX IF NOT EXISTS idx_gotv_tasks_status ON gotv_tasks(party_id, status);

CREATE TABLE IF NOT EXISTS gotv_dead_letter_queue (
			id SERIAL PRIMARY KEY,
			party_id INTEGER NOT NULL,
			campaign_id TEXT DEFAULT '',
			contact_id TEXT DEFAULT '',
			channel TEXT NOT NULL,
			payload JSONB DEFAULT '{}',
			error_detail TEXT DEFAULT '',
			attempts INTEGER DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

CREATE TABLE IF NOT EXISTS gotv_contact_scores (
    id BIGSERIAL PRIMARY KEY,
    contact_id TEXT NOT NULL REFERENCES gotv_contacts(contact_id) ON DELETE CASCADE,
    party_id INTEGER NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    score NUMERIC(5,2) NOT NULL CHECK (score >= 0 AND score <= 100),
    model_version TEXT NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (contact_id, party_id)
);
CREATE INDEX IF NOT EXISTS idx_gotv_contact_scores_party_score
    ON gotv_contact_scores (party_id, score DESC);
CREATE INDEX IF NOT EXISTS idx_gotv_contact_scores_computed
    ON gotv_contact_scores (computed_at DESC);

CREATE TABLE IF NOT EXISTS gotv_scoring_runs (
    id BIGSERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    model_version TEXT NOT NULL,
    contacts_scored INTEGER NOT NULL DEFAULT 0 CHECK (contacts_scored >= 0),
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_gotv_scoring_runs_party_completed
    ON gotv_scoring_runs (party_id, completed_at DESC);

CREATE TABLE IF NOT EXISTS gotv_whatsapp_inbound (
    id BIGSERIAL PRIMARY KEY,
    phone TEXT NOT NULL,
    message TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT 'unknown',
    media_id TEXT,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_gotv_whatsapp_inbound_phone_created
    ON gotv_whatsapp_inbound (phone, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_gotv_whatsapp_inbound_action_created
    ON gotv_whatsapp_inbound (action, created_at DESC);

CREATE TABLE IF NOT EXISTS gotv_user_preferences (
    id BIGSERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    preference_key TEXT NOT NULL,
    preference_value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (party_id, user_id, preference_key)
);
CREATE INDEX IF NOT EXISTS idx_gotv_user_preferences_lookup
    ON gotv_user_preferences (party_id, user_id, preference_key);

CREATE TABLE IF NOT EXISTS gotv_crowd_estimates (
    id BIGSERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    event_name TEXT NOT NULL,
    state_code TEXT,
    venue_type TEXT NOT NULL,
    venue_area_sqm DOUBLE PRECISION NOT NULL CHECK (venue_area_sqm > 0),
    density_per_sqm DOUBLE PRECISION NOT NULL CHECK (density_per_sqm >= 0),
    estimated_crowd INTEGER NOT NULL CHECK (estimated_crowd >= 0),
    confidence_low INTEGER NOT NULL CHECK (confidence_low >= 0),
    confidence_high INTEGER NOT NULL CHECK (confidence_high >= confidence_low),
    image_url TEXT,
    model_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_gotv_crowd_estimates_party_created
    ON gotv_crowd_estimates (party_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_gotv_crowd_estimates_state_created
    ON gotv_crowd_estimates (state_code, created_at DESC);

CREATE TABLE IF NOT EXISTS gotv_social_inbox (
    id BIGSERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    platform TEXT NOT NULL,
    author TEXT NOT NULL,
    message TEXT NOT NULL,
    sentiment TEXT NOT NULL DEFAULT 'unknown',
    status TEXT NOT NULL DEFAULT 'unread'
        CHECK (status IN ('unread', 'read', 'responded', 'escalated', 'archived')),
    response TEXT,
    responded_at TIMESTAMPTZ,
    source_message_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (party_id, platform, source_message_id)
);
CREATE INDEX IF NOT EXISTS idx_gotv_social_inbox_party_status_created
    ON gotv_social_inbox (party_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_gotv_social_inbox_party_platform_created
    ON gotv_social_inbox (party_id, platform, created_at DESC);

CREATE TABLE IF NOT EXISTS gotv_federated_participants (
    id BIGSERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    opted_in BOOLEAN NOT NULL DEFAULT FALSE,
    public_key TEXT,
    model_version TEXT,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (party_id)
);
CREATE INDEX IF NOT EXISTS idx_gotv_federated_participants_opted_in
    ON gotv_federated_participants (opted_in) WHERE opted_in = TRUE;

CREATE TABLE IF NOT EXISTS gotv_federated_rounds (
    id BIGSERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    round_number INTEGER NOT NULL CHECK (round_number > 0),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    model_version TEXT,
    metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (party_id, round_number)
);
CREATE INDEX IF NOT EXISTS idx_gotv_federated_rounds_status_completed
    ON gotv_federated_rounds (status, completed_at DESC);
CREATE INDEX IF NOT EXISTS idx_gotv_federated_rounds_party_round
    ON gotv_federated_rounds (party_id, round_number DESC);

-- High-frequency election queries use these fields together in dashboards,
-- anomaly detection, and result ingestion. The indexes are additive and safe on
-- clean installs; production deployments should apply this migration during a
-- controlled release window.
CREATE INDEX IF NOT EXISTS idx_results_election_submitted
    ON results (election_id, submitted_at DESC);
CREATE INDEX IF NOT EXISTS idx_results_election_validated
    ON results (election_id, validated_at DESC)
    WHERE status IN ('validated', 'finalized');
CREATE INDEX IF NOT EXISTS idx_incidents_election_status_reported
    ON incidents (election_id, status, reported_at DESC);


-- Runtime schema reconciliation for tables created by the historical service initializers.
ALTER TABLE gotv_field_reports ADD COLUMN IF NOT EXISTS lga_code TEXT;
ALTER TABLE gotv_field_reports ADD COLUMN IF NOT EXISTS media_url TEXT;
ALTER TABLE gotv_field_reports ADD COLUMN IF NOT EXISTS report_type TEXT;
ALTER TABLE gotv_field_reports ADD COLUMN IF NOT EXISTS state_code TEXT;
ALTER TABLE gotv_ride_requests ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE gotv_ride_requests ADD COLUMN IF NOT EXISTS notes TEXT;

CREATE TABLE IF NOT EXISTS stablecoin_wallets (
    wallet_id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    owner_type TEXT NOT NULL DEFAULT 'institution',
    currency TEXT NOT NULL DEFAULT 'eNGN',
    balance DECIMAL(20,4) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    public_key TEXT NOT NULL DEFAULT '',
    private_key_enc TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_wallets_owner ON stablecoin_wallets(owner_id, owner_type);

-- Extend the original integer-keyed transaction table with the stablecoin
-- engine's transaction identifier and accounting fields. The trigger keeps the
-- legacy transaction_id and the engine's tx_id synchronized during coexistence.
ALTER TABLE stablecoin_transactions ADD COLUMN IF NOT EXISTS tx_id TEXT;
ALTER TABLE stablecoin_transactions ADD COLUMN IF NOT EXISTS from_wallet TEXT NOT NULL DEFAULT '';
ALTER TABLE stablecoin_transactions ADD COLUMN IF NOT EXISTS to_wallet TEXT NOT NULL DEFAULT '';
ALTER TABLE stablecoin_transactions ADD COLUMN IF NOT EXISTS tx_type TEXT NOT NULL DEFAULT 'transfer';
ALTER TABLE stablecoin_transactions ADD COLUMN IF NOT EXISTS signature TEXT NOT NULL DEFAULT '';
ALTER TABLE stablecoin_transactions ADD COLUMN IF NOT EXISTS block_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE stablecoin_transactions ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';
ALTER TABLE stablecoin_transactions ADD COLUMN IF NOT EXISTS confirmed_at TIMESTAMP;
UPDATE stablecoin_transactions SET tx_id = transaction_id WHERE tx_id IS NULL;
ALTER TABLE stablecoin_transactions ALTER COLUMN transaction_id DROP NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_stablecoin_transactions_tx_id ON stablecoin_transactions(tx_id);
CREATE INDEX IF NOT EXISTS idx_stablecoin_tx_wallets ON stablecoin_transactions(from_wallet, to_wallet);
CREATE INDEX IF NOT EXISTS idx_stablecoin_tx_status ON stablecoin_transactions(status, created_at DESC);
CREATE OR REPLACE FUNCTION sync_stablecoin_transaction_identifiers()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.tx_id IS NULL OR NEW.tx_id = '' THEN
        NEW.tx_id := NEW.transaction_id;
    END IF;
    IF NEW.transaction_id IS NULL OR NEW.transaction_id = '' THEN
        NEW.transaction_id := NEW.tx_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_sync_stablecoin_transaction_identifiers ON stablecoin_transactions;
CREATE TRIGGER trg_sync_stablecoin_transaction_identifiers
BEFORE INSERT OR UPDATE OF tx_id, transaction_id ON stablecoin_transactions
FOR EACH ROW EXECUTE FUNCTION sync_stablecoin_transaction_identifiers();

CREATE TABLE IF NOT EXISTS stablecoin_ledger (
    id BIGSERIAL PRIMARY KEY,
    tx_id TEXT NOT NULL,
    wallet_id TEXT NOT NULL,
    entry_type TEXT NOT NULL,
    amount DECIMAL(20,4) NOT NULL,
    balance_after DECIMAL(20,4) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_stablecoin_ledger_wallet_created
    ON stablecoin_ledger(wallet_id, created_at DESC);
