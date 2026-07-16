-- Ensure PostGIS is available before using geometry types
CREATE EXTENSION IF NOT EXISTS postgis;

-- Core extended tables: voters, audit, collation, validation

CREATE TABLE IF NOT EXISTS voters (
    id SERIAL PRIMARY KEY,
    vin text NOT NULL,
    first_name text NOT NULL,
    last_name text NOT NULL,
    middle_name text,
    date_of_birth text NOT NULL,
    gender text NOT NULL,
    phone text,
    email text,
    address text,
    state_code text NOT NULL,
    lga_code text NOT NULL,
    ward_code text NOT NULL,
    polling_unit_code text NOT NULL,
    registration_center text,
    biometric_hash text,
    photo_hash text,
    pvc_number text,
    pvc_collected integer DEFAULT 0,
    pvc_collected_at timestamp without time zone,
    status text DEFAULT 'registered'::text NOT NULL,
    registered_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    verified_at timestamp without time zone,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT voters_gender_check CHECK ((gender = ANY (ARRAY['M'::text, 'F'::text]))),
    CONSTRAINT voters_status_check CHECK ((status = ANY (ARRAY['registered'::text, 'verified'::text, 'active'::text, 'suspended'::text, 'deceased'::text, 'transferred'::text])))
);

CREATE INDEX IF NOT EXISTS idx_voters_pu ON voters USING btree (polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_voters_pvc ON voters USING btree (pvc_number);
CREATE INDEX IF NOT EXISTS idx_voters_state ON voters USING btree (state_code);
CREATE INDEX IF NOT EXISTS idx_voters_vin ON voters USING btree (vin);

CREATE TABLE IF NOT EXISTS audit_log (
    id SERIAL PRIMARY KEY,
    action text NOT NULL,
    entity_type text NOT NULL,
    entity_id text,
    user_id integer,
    details text,
    block_hash text,
    prev_block_hash text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log USING btree (action);
CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_log USING btree (entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log USING btree ("timestamp");
CREATE INDEX IF NOT EXISTS idx_audit_user_action ON audit_log USING btree (user_id, action);

CREATE TABLE IF NOT EXISTS collation_results (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    level text NOT NULL,
    area_code text NOT NULL,
    area_name text NOT NULL,
    total_registered_voters integer DEFAULT 0,
    total_accredited_voters integer DEFAULT 0,
    total_valid_votes integer DEFAULT 0,
    total_rejected_votes integer DEFAULT 0,
    total_votes_cast integer DEFAULT 0,
    polling_units_reported integer DEFAULT 0,
    polling_units_total integer DEFAULT 0,
    status text DEFAULT 'in_progress'::text,
    last_updated timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT collation_results_level_check CHECK ((level = ANY (ARRAY['ward'::text, 'lga'::text, 'state'::text, 'national'::text]))),
    CONSTRAINT collation_results_status_check CHECK ((status = ANY (ARRAY['in_progress'::text, 'completed'::text, 'disputed'::text])))
);

CREATE INDEX IF NOT EXISTS idx_collation_election ON collation_results USING btree (election_id, level);
CREATE INDEX IF NOT EXISTS idx_collation_election_level ON collation_results USING btree (election_id, level, area_code);
CREATE INDEX IF NOT EXISTS idx_collation_level_area ON collation_results USING btree (level, area_code);

CREATE TABLE IF NOT EXISTS collation_party_scores (
    id SERIAL PRIMARY KEY,
    collation_result_id integer NOT NULL,
    party_code text NOT NULL,
    votes integer DEFAULT 0 NOT NULL
);

CREATE TABLE IF NOT EXISTS validation_rules (
    id SERIAL PRIMARY KEY,
    rule_name text NOT NULL,
    rule_type text NOT NULL,
    entity_type text NOT NULL,
    expression text NOT NULL,
    severity text DEFAULT 'error'::text NOT NULL,
    is_active integer DEFAULT 1,
    description text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT validation_rules_entity_type_check CHECK ((entity_type = ANY (ARRAY['result'::text, 'accreditation'::text, 'voter'::text, 'incident'::text]))),
    CONSTRAINT validation_rules_rule_type_check CHECK ((rule_type = ANY (ARRAY['format'::text, 'range'::text, 'cross_reference'::text, 'statistical'::text, 'business'::text, 'custom'::text]))),
    CONSTRAINT validation_rules_severity_check CHECK ((severity = ANY (ARRAY['info'::text, 'warning'::text, 'error'::text, 'critical'::text])))
);

CREATE TABLE IF NOT EXISTS validation_results (
    id SERIAL PRIMARY KEY,
    entity_type text NOT NULL,
    entity_id text NOT NULL,
    rule_id integer NOT NULL,
    passed integer NOT NULL,
    severity text NOT NULL,
    message text,
    details text,
    validated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_validation_entity ON validation_results USING btree (entity_type, entity_id);

CREATE TABLE IF NOT EXISTS anomaly_escalations (
    id SERIAL PRIMARY KEY,
    anomaly_id text,
    severity text NOT NULL,
    state_code text,
    pu_code text,
    action_taken text,
    escalated_to text,
    collation_paused integer DEFAULT 0,
    resolved integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS citizen_verifications (
    id SERIAL PRIMARY KEY,
    pu_code text NOT NULL,
    ip_hash text,
    verified_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS command_alerts (
    id SERIAL PRIMARY KEY,
    level text NOT NULL,
    state_code text,
    message text NOT NULL,
    auto_action text,
    resolved integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS command_center_config (
    key text NOT NULL,
    value text NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS escalation_log (
    id SERIAL PRIMARY KEY,
    rule_name text NOT NULL,
    level text NOT NULL,
    state_code text,
    action_taken text,
    details text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS escalation_rules (
    id SERIAL PRIMARY KEY,
    name text NOT NULL,
    condition text NOT NULL,
    level text NOT NULL,
    action text NOT NULL,
    cooldown_seconds integer DEFAULT 300,
    enabled integer DEFAULT 1,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS gps_spoof_events (
    id SERIAL PRIMARY KEY,
    device_id text NOT NULL,
    lat real NOT NULL,
    lng real NOT NULL,
    confidence real NOT NULL,
    indicators text,
    detected_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS grievances (
    id SERIAL PRIMARY KEY,
    stakeholder_id integer NOT NULL,
    grievance_type text NOT NULL,
    subject text NOT NULL,
    description text NOT NULL,
    evidence_urls text,
    priority text DEFAULT 'normal'::text NOT NULL,
    status text DEFAULT 'filed'::text NOT NULL,
    assigned_to text,
    resolution text,
    filed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    resolved_at timestamp without time zone,
    CONSTRAINT grievances_grievance_type_check CHECK ((grievance_type = ANY (ARRAY['result_dispute'::text, 'process_complaint'::text, 'staff_misconduct'::text, 'access_denial'::text, 'equipment_issue'::text, 'other'::text]))),
    CONSTRAINT grievances_priority_check CHECK ((priority = ANY (ARRAY['low'::text, 'normal'::text, 'high'::text, 'urgent'::text]))),
    CONSTRAINT grievances_status_check CHECK ((status = ANY (ARRAY['filed'::text, 'under_review'::text, 'hearing_scheduled'::text, 'resolved'::text, 'appealed'::text, 'dismissed'::text])))
);

CREATE INDEX IF NOT EXISTS idx_grievance_status ON grievances USING btree (status);

CREATE TABLE IF NOT EXISTS metrics_client (
    id SERIAL PRIMARY KEY,
    ts text,
    event text,
    data text,
    ua text,
    ip text
);

CREATE TABLE IF NOT EXISTS mojaloop_transactions (
    id SERIAL PRIMARY KEY,
    transaction_id text,
    payer text,
    payee text,
    amount real,
    currency text,
    status text DEFAULT 'pending'::text,
    ilp_packet text,
    condition text,
    fulfilment text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    completed_at timestamp without time zone
);

CREATE TABLE IF NOT EXISTS opensearch_documents (
    id SERIAL PRIMARY KEY,
    index_name text NOT NULL,
    doc_id text NOT NULL,
    body text NOT NULL,
    indexed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_opensearch_idx ON opensearch_documents USING btree (index_name);

CREATE TABLE IF NOT EXISTS tb_accounts (
    id text NOT NULL,
    ledger integer NOT NULL,
    code integer NOT NULL,
    credits_posted integer DEFAULT 0,
    debits_posted integer DEFAULT 0,
    credits_pending integer DEFAULT 0,
    debits_pending integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tb_journal (
    id SERIAL PRIMARY KEY,
    transfer_id text NOT NULL,
    event_type text NOT NULL,
    debit_account text NOT NULL,
    credit_account text NOT NULL,
    amount integer NOT NULL,
    running_balance_debit integer DEFAULT 0,
    running_balance_credit integer DEFAULT 0,
    idempotency_key text,
    batch_id text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tb_journal ON tb_journal USING btree (transfer_id, created_at);

CREATE TABLE IF NOT EXISTS tb_transfers (
    id text NOT NULL,
    debit_account_id text NOT NULL,
    credit_account_id text NOT NULL,
    amount integer NOT NULL,
    ledger integer NOT NULL,
    code integer NOT NULL,
    status text DEFAULT 'PENDING'::text NOT NULL,
    user_data text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    posted_at timestamp without time zone
);

CREATE INDEX IF NOT EXISTS idx_tb_transfers_credit ON tb_transfers USING btree (credit_account_id);
CREATE INDEX IF NOT EXISTS idx_tb_transfers_debit ON tb_transfers USING btree (debit_account_id);
CREATE INDEX IF NOT EXISTS idx_tb_transfers_status ON tb_transfers USING btree (status);

CREATE TABLE IF NOT EXISTS token_blacklist (
    jti text NOT NULL,
    user_id integer NOT NULL,
    revoked_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone NOT NULL,
    reason text DEFAULT ''::text
);

CREATE TABLE IF NOT EXISTS polling_unit_locations (
    polling_unit_code text NOT NULL,
    latitude real NOT NULL,
    longitude real NOT NULL,
    geofence_radius_m integer DEFAULT 500,
    state_code text,
    lga_code text
);

-- Add geometry column safely (table may already exist without it)
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name='polling_unit_locations' AND column_name='geom'
    ) THEN
        ALTER TABLE polling_unit_locations ADD COLUMN geom public.geometry(Point,4326);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_pu_locations_geom ON polling_unit_locations USING gist (geom);


