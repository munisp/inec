-- INEC Election Platform — Complete Database Schema
-- Auto-generated from migrations 000001-000014
-- Run: psql -U ngapp -d ngapp -f init.sql

SET statement_timeout = 0;
SET lock_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;

-- INEC 2027 Election Platform: Initial Schema
-- Auto-generated from actual PostgreSQL schema

CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username text NOT NULL,
    password_hash text NOT NULL,
    full_name text NOT NULL,
    role text NOT NULL,
    staff_id text,
    state_code text,
    lga_code text,
    polling_unit_code text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    is_active integer DEFAULT 1,
    kyc_status text DEFAULT 'not_started'::text,
    CONSTRAINT users_role_check CHECK ((role = ANY (ARRAY['admin'::text, 'presiding_officer'::text, 'collation_officer'::text, 'observer'::text, 'public'::text])))
);

CREATE INDEX IF NOT EXISTS idx_users_role ON users USING btree (role);
CREATE INDEX IF NOT EXISTS idx_users_state ON users USING btree (state_code);
CREATE INDEX IF NOT EXISTS idx_users_username ON users USING btree (username);

CREATE TABLE IF NOT EXISTS elections (
    id SERIAL PRIMARY KEY,
    title text NOT NULL,
    election_type text NOT NULL,
    election_date text NOT NULL,
    status text DEFAULT 'upcoming'::text NOT NULL,
    description text,
    total_registered_voters integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT elections_election_type_check CHECK ((election_type = ANY (ARRAY['presidential'::text, 'gubernatorial'::text, 'senatorial'::text, 'house_of_reps'::text, 'state_assembly'::text, 'local_government'::text]))),
    CONSTRAINT elections_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'scheduled'::text, 'upcoming'::text, 'active'::text, 'voting'::text, 'collating'::text, 'closed'::text, 'completed'::text, 'cancelled'::text, 'disputed'::text])))
);

CREATE INDEX IF NOT EXISTS idx_elections_date ON elections USING btree (election_date);
CREATE INDEX IF NOT EXISTS idx_elections_status ON elections USING btree (status);

CREATE TABLE IF NOT EXISTS parties (
    id SERIAL PRIMARY KEY,
    code text NOT NULL,
    name text NOT NULL,
    abbreviation text NOT NULL,
    logo_url text,
    color text,
    is_active integer DEFAULT 1
);

CREATE TABLE IF NOT EXISTS states (
    id SERIAL PRIMARY KEY,
    code text NOT NULL,
    name text NOT NULL,
    geo_zone text NOT NULL,
    capital text
);

CREATE TABLE IF NOT EXISTS lgas (
    id SERIAL PRIMARY KEY,
    code text NOT NULL,
    name text NOT NULL,
    state_code text NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_lgas_state ON lgas USING btree (state_code);

CREATE TABLE IF NOT EXISTS wards (
    id SERIAL PRIMARY KEY,
    code text NOT NULL,
    name text NOT NULL,
    lga_code text NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_wards_lga ON wards USING btree (lga_code);

CREATE TABLE IF NOT EXISTS polling_units (
    id SERIAL PRIMARY KEY,
    code text NOT NULL,
    name text NOT NULL,
    ward_code text NOT NULL,
    registered_voters integer DEFAULT 0,
    latitude real,
    longitude real
);

CREATE INDEX IF NOT EXISTS idx_polling_units_ward ON polling_units USING btree (ward_code);
CREATE INDEX IF NOT EXISTS idx_pu_lonlat ON polling_units USING btree (longitude, latitude);

CREATE TABLE IF NOT EXISTS results (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    polling_unit_code text NOT NULL,
    presiding_officer_id integer,
    status text DEFAULT 'pending'::text NOT NULL,
    total_valid_votes integer DEFAULT 0,
    rejected_votes integer DEFAULT 0,
    total_votes_cast integer DEFAULT 0,
    accredited_voters integer DEFAULT 0,
    ec8a_hash text,
    tigerbeetle_transfer_id text,
    hyperledger_tx_id text,
    tigerbeetle_status text DEFAULT 'PENDING'::text,
    hyperledger_status text DEFAULT 'PENDING'::text,
    ipfs_cid text,
    submitted_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    validated_at timestamp without time zone,
    finalized_at timestamp without time zone,
    CONSTRAINT results_hyperledger_status_check CHECK ((hyperledger_status = ANY (ARRAY['PENDING'::text, 'CONFIRMED'::text, 'FAILED'::text]))),
    CONSTRAINT results_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'validated'::text, 'finalized'::text, 'disputed'::text, 'voided'::text]))),
    CONSTRAINT results_tigerbeetle_status_check CHECK ((tigerbeetle_status = ANY (ARRAY['PENDING'::text, 'POSTED'::text, 'VOIDED'::text])))
);

CREATE INDEX IF NOT EXISTS idx_results_election ON results USING btree (election_id);
CREATE INDEX IF NOT EXISTS idx_results_election_pu ON results USING btree (election_id, polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_results_election_status ON results USING btree (election_id, status);
CREATE INDEX IF NOT EXISTS idx_results_pu ON results USING btree (polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_results_pu_election ON results USING btree (polling_unit_code, election_id);
CREATE INDEX IF NOT EXISTS idx_results_status ON results USING btree (status);
CREATE INDEX IF NOT EXISTS idx_results_submitted_at ON results USING btree (submitted_at);

CREATE TABLE IF NOT EXISTS incidents (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    polling_unit_code text,
    reported_by integer,
    incident_type text NOT NULL,
    description text NOT NULL,
    severity text NOT NULL,
    status text DEFAULT 'reported'::text NOT NULL,
    reported_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    resolved_at timestamp without time zone,
    CONSTRAINT incidents_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))),
    CONSTRAINT incidents_status_check CHECK ((status = ANY (ARRAY['reported'::text, 'investigating'::text, 'resolved'::text, 'dismissed'::text])))
);

CREATE TABLE IF NOT EXISTS bvas_devices (
    id text NOT NULL,
    serial_number text NOT NULL,
    polling_unit_code text,
    election_id integer,
    status text DEFAULT 'registered'::text NOT NULL,
    battery_level integer DEFAULT 100,
    firmware_version text DEFAULT '3.2.1'::text,
    last_sync_at timestamp without time zone,
    latitude real,
    longitude real,
    assigned_officer integer,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT bvas_devices_status_check CHECK ((status = ANY (ARRAY['registered'::text, 'deployed'::text, 'active'::text, 'offline'::text, 'faulty'::text, 'decommissioned'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bvas_devices_pu ON bvas_devices USING btree (polling_unit_code);

-- Middleware persistence tables
-- Auto-generated from actual PostgreSQL schema

CREATE TABLE IF NOT EXISTS waf_blocklist (
    id SERIAL PRIMARY KEY,
    ip_address text NOT NULL,
    reason text,
    blocked_at text
);

CREATE INDEX IF NOT EXISTS idx_waf_ip ON waf_blocklist USING btree (ip_address);

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


-- Election management: lifecycle, materials, staff, EMS

CREATE TABLE IF NOT EXISTS election_archive (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    archived_data text NOT NULL,
    checksum text NOT NULL,
    is_immutable integer DEFAULT 1,
    archived_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS election_lifecycle (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    phase text NOT NULL,
    transitioned_by integer,
    notes text,
    transitioned_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT election_lifecycle_phase_check CHECK ((phase = ANY (ARRAY['created'::text, 'configured'::text, 'staff_deployed'::text, 'materials_deployed'::text, 'monitoring'::text, 'voting_open'::text, 'voting_closed'::text, 'collation'::text, 'declaration'::text, 'certified'::text, 'archived'::text])))
);

CREATE INDEX IF NOT EXISTS idx_lifecycle_election ON election_lifecycle USING btree (election_id);

CREATE TABLE IF NOT EXISTS election_materials (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    material_type text NOT NULL,
    quantity integer NOT NULL,
    destination_type text NOT NULL,
    destination_code text NOT NULL,
    status text DEFAULT 'allocated'::text NOT NULL,
    tracking_number text,
    dispatched_at timestamp without time zone,
    delivered_at timestamp without time zone,
    acknowledged_at timestamp without time zone,
    CONSTRAINT election_materials_destination_type_check CHECK ((destination_type = ANY (ARRAY['state'::text, 'lga'::text, 'ward'::text, 'polling_unit'::text]))),
    CONSTRAINT election_materials_material_type_check CHECK ((material_type = ANY (ARRAY['ballot_paper'::text, 'result_sheet'::text, 'stamp'::text, 'ink'::text, 'seal'::text, 'bvas_device'::text, 'generator'::text, 'tent'::text]))),
    CONSTRAINT election_materials_status_check CHECK ((status = ANY (ARRAY['allocated'::text, 'dispatched'::text, 'in_transit'::text, 'delivered'::text, 'acknowledged'::text, 'returned'::text])))
);

CREATE INDEX IF NOT EXISTS idx_materials_election ON election_materials USING btree (election_id);

CREATE TABLE IF NOT EXISTS election_staff_assignments (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    user_id integer NOT NULL,
    role text NOT NULL,
    area_type text NOT NULL,
    area_code text NOT NULL,
    status text DEFAULT 'assigned'::text NOT NULL,
    assigned_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    deployed_at timestamp without time zone,
    CONSTRAINT election_staff_assignments_area_type_check CHECK ((area_type = ANY (ARRAY['national'::text, 'state'::text, 'lga'::text, 'ward'::text, 'polling_unit'::text]))),
    CONSTRAINT election_staff_assignments_status_check CHECK ((status = ANY (ARRAY['assigned'::text, 'deployed'::text, 'active'::text, 'completed'::text, 'withdrawn'::text])))
);

CREATE INDEX IF NOT EXISTS idx_staff_election ON election_staff_assignments USING btree (election_id);

CREATE TABLE IF NOT EXISTS election_state_log (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    from_state text NOT NULL,
    to_state text NOT NULL,
    event text NOT NULL,
    actor text DEFAULT ''::text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS election_templates (
    id SERIAL PRIMARY KEY,
    election_type text NOT NULL,
    template_name text NOT NULL,
    party_count integer DEFAULT 18,
    form_fields text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ems_workflow_phases (
    id SERIAL PRIMARY KEY,
    workflow_id integer NOT NULL,
    phase text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    started_at timestamp without time zone,
    completed_at timestamp without time zone,
    notes text,
    completed_by integer,
    CONSTRAINT ems_workflow_phases_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'in_progress'::text, 'completed'::text, 'skipped'::text, 'failed'::text])))
);

CREATE TABLE IF NOT EXISTS ems_workflows (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    workflow_type text NOT NULL,
    current_phase text DEFAULT 'planning'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    started_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    completed_at timestamp without time zone,
    metadata text,
    CONSTRAINT ems_workflows_current_phase_check CHECK ((current_phase = ANY (ARRAY['planning'::text, 'registration'::text, 'accreditation'::text, 'voting'::text, 'collation'::text, 'declaration'::text, 'certification'::text, 'archived'::text]))),
    CONSTRAINT ems_workflows_status_check CHECK ((status = ANY (ARRAY['active'::text, 'paused'::text, 'completed'::text, 'cancelled'::text]))),
    CONSTRAINT ems_workflows_workflow_type_check CHECK ((workflow_type = ANY (ARRAY['full_election'::text, 'by_election'::text, 'rerun'::text, 'supplementary'::text])))
);

CREATE TABLE IF NOT EXISTS dedup_candidates (
    id SERIAL PRIMARY KEY,
    job_id integer NOT NULL,
    source_vin text NOT NULL,
    candidate_vin text NOT NULL,
    fingerprint_score real DEFAULT 0,
    facial_score real DEFAULT 0,
    iris_score real DEFAULT 0,
    fused_score real DEFAULT 0 NOT NULL,
    fusion_method text DEFAULT 'weighted_sum'::text,
    decision text DEFAULT 'pending'::text NOT NULL,
    reviewed_by text,
    reviewed_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT dedup_candidates_decision_check CHECK ((decision = ANY (ARRAY['pending'::text, 'duplicate'::text, 'not_duplicate'::text, 'needs_review'::text])))
);

CREATE INDEX IF NOT EXISTS idx_dedup_cand ON dedup_candidates USING btree (job_id, fused_score);

CREATE TABLE IF NOT EXISTS dedup_jobs (
    id SERIAL PRIMARY KEY,
    job_type text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    total_comparisons integer DEFAULT 0,
    duplicates_found integer DEFAULT 0,
    false_positives integer DEFAULT 0,
    progress_percent real DEFAULT 0,
    modalities text DEFAULT 'fingerprint'::text NOT NULL,
    threshold real DEFAULT 0.85 NOT NULL,
    blocking_strategy text DEFAULT 'locality_sensitive_hash'::text,
    started_at timestamp without time zone,
    completed_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    error_detail text,
    CONSTRAINT dedup_jobs_job_type_check CHECK ((job_type = ANY (ARRAY['full_scan'::text, 'incremental'::text, 'targeted'::text]))),
    CONSTRAINT dedup_jobs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'completed'::text, 'failed'::text, 'cancelled'::text])))
);

CREATE INDEX IF NOT EXISTS idx_dedup_job ON dedup_jobs USING btree (status, created_at);

CREATE TABLE IF NOT EXISTS dedup_resolutions (
    id SERIAL PRIMARY KEY,
    voter_a_vin text NOT NULL,
    voter_b_vin text NOT NULL,
    decision text NOT NULL,
    reason text,
    resolved_by text,
    resolved_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS kiosk_sessions (
    id SERIAL PRIMARY KEY,
    session_id text NOT NULL,
    device_id text NOT NULL,
    voter_vin text,
    current_step integer DEFAULT 1,
    total_steps integer DEFAULT 8,
    step_name text DEFAULT 'identity_verification'::text,
    modalities_completed text DEFAULT ''::text,
    quality_feedback text,
    guidance_messages text,
    status text DEFAULT 'in_progress'::text,
    started_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    completed_at timestamp without time zone
);

CREATE INDEX IF NOT EXISTS idx_kiosk_session ON kiosk_sessions USING btree (session_id, status);

CREATE TABLE IF NOT EXISTS result_party_scores (
    id SERIAL PRIMARY KEY,
    result_id integer NOT NULL,
    party_code text NOT NULL,
    votes integer DEFAULT 0 NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_rps_party ON result_party_scores USING btree (party_code);
CREATE INDEX IF NOT EXISTS idx_rps_result ON result_party_scores USING btree (result_id);
CREATE INDEX IF NOT EXISTS idx_rps_result_party ON result_party_scores USING btree (result_id, party_code);

CREATE TABLE IF NOT EXISTS result_signatures (
    id SERIAL PRIMARY KEY,
    result_id integer NOT NULL,
    officer_pubkey text NOT NULL,
    signature text NOT NULL,
    prev_hash text,
    result_hash text NOT NULL,
    chain_position integer DEFAULT 0,
    signed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS stakeholder_incidents (
    id SERIAL PRIMARY KEY,
    reporter_id integer NOT NULL,
    incident_type text NOT NULL,
    description text NOT NULL,
    severity text DEFAULT 'medium'::text NOT NULL,
    latitude real,
    longitude real,
    polling_unit_code text,
    media_urls text,
    status text DEFAULT 'reported'::text NOT NULL,
    assigned_to integer,
    resolution_notes text,
    reported_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    resolved_at timestamp without time zone,
    CONSTRAINT stakeholder_incidents_incident_type_check CHECK ((incident_type = ANY (ARRAY['violence'::text, 'intimidation'::text, 'ballot_stuffing'::text, 'equipment_failure'::text, 'process_violation'::text, 'other'::text]))),
    CONSTRAINT stakeholder_incidents_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))),
    CONSTRAINT stakeholder_incidents_status_check CHECK ((status = ANY (ARRAY['reported'::text, 'acknowledged'::text, 'investigating'::text, 'resolved'::text, 'escalated'::text, 'dismissed'::text])))
);

CREATE TABLE IF NOT EXISTS stakeholders (
    id SERIAL PRIMARY KEY,
    name text NOT NULL,
    organization text,
    stakeholder_type text NOT NULL,
    email text,
    phone text,
    credential_id text,
    credential_qr text,
    nfc_tag text,
    accreditation_status text DEFAULT 'pending'::text NOT NULL,
    election_id integer,
    assigned_area text,
    photo_url text,
    registered_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT stakeholders_accreditation_status_check CHECK ((accreditation_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'suspended'::text, 'expired'::text]))),
    CONSTRAINT stakeholders_stakeholder_type_check CHECK ((stakeholder_type = ANY (ARRAY['party_agent'::text, 'observer'::text, 'media'::text, 'cso'::text, 'diplomat'::text, 'security'::text, 'candidate'::text, 'legal'::text])))
);

CREATE INDEX IF NOT EXISTS idx_stakeholder_type ON stakeholders USING btree (stakeholder_type);


-- Biometric: profiles, templates, vault, ABIS, quality

CREATE TABLE IF NOT EXISTS biometric_profiles (
    id SERIAL PRIMARY KEY,
    voter_vin text NOT NULL,
    fingerprint_hash text,
    facial_hash text,
    iris_hash text,
    modalities_enrolled text DEFAULT 'fingerprint'::text NOT NULL,
    quality_score real DEFAULT 0,
    enrollment_device text,
    enrollment_date timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    last_verified_at timestamp without time zone,
    match_count integer DEFAULT 0,
    duplicate_flag integer DEFAULT 0,
    duplicate_matched_vin text,
    status text DEFAULT 'active'::text NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT biometric_profiles_status_check CHECK ((status = ANY (ARRAY['active'::text, 'suspended'::text, 'flagged'::text, 'revoked'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bio_voter ON biometric_profiles USING btree (voter_vin);

CREATE TABLE IF NOT EXISTS biometric_templates (
    id SERIAL PRIMARY KEY,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    template_data bytea NOT NULL,
    template_format text DEFAULT 'ISO_19794'::text NOT NULL,
    encryption_key_id text NOT NULL,
    iv bytea NOT NULL,
    quality_score real DEFAULT 0 NOT NULL,
    nfiq_score integer DEFAULT 0,
    minutiae_count integer DEFAULT 0,
    embedding_dim integer DEFAULT 0,
    iris_code_bits integer DEFAULT 0,
    capture_device text,
    capture_timestamp timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    iso_compliance text DEFAULT 'ISO_19794_2'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT biometric_templates_modality_check CHECK ((modality = ANY (ARRAY['fingerprint'::text, 'facial'::text, 'iris'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bio_tmpl_mod ON biometric_templates USING btree (modality);
CREATE INDEX IF NOT EXISTS idx_bio_tmpl_vin ON biometric_templates USING btree (voter_vin);

CREATE TABLE IF NOT EXISTS biometric_verifications (
    id SERIAL PRIMARY KEY,
    voter_vin text NOT NULL,
    device_id text,
    modality text NOT NULL,
    match_score real NOT NULL,
    threshold real DEFAULT 0.85,
    result text NOT NULL,
    latency_ms integer,
    polling_unit_code text,
    election_id integer,
    verified_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT biometric_verifications_modality_check CHECK ((modality = ANY (ARRAY['fingerprint'::text, 'facial'::text, 'iris'::text, 'multi_modal'::text]))),
    CONSTRAINT biometric_verifications_result_check CHECK ((result = ANY (ARRAY['match'::text, 'no_match'::text, 'uncertain'::text, 'spoof_detected'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bio_verif ON biometric_verifications USING btree (voter_vin, verified_at);

CREATE TABLE IF NOT EXISTS biometric_vault_keys (
    id SERIAL PRIMARY KEY,
    key_id text NOT NULL,
    encrypted_key bytea NOT NULL,
    key_type text DEFAULT 'AES-256-GCM'::text NOT NULL,
    purpose text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    rotation_count integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    rotated_at timestamp without time zone,
    expires_at timestamp without time zone,
    CONSTRAINT biometric_vault_keys_purpose_check CHECK ((purpose = ANY (ARRAY['template_encryption'::text, 'signing'::text, 'key_wrapping'::text]))),
    CONSTRAINT biometric_vault_keys_status_check CHECK ((status = ANY (ARRAY['active'::text, 'rotated'::text, 'revoked'::text])))
);

CREATE INDEX IF NOT EXISTS idx_vault_keys ON biometric_vault_keys USING btree (key_id, status);

CREATE TABLE IF NOT EXISTS biometric_vault_audit (
    id SERIAL PRIMARY KEY,
    operation text NOT NULL,
    key_id text,
    voter_vin text,
    modality text,
    actor text,
    ip_address text,
    success integer DEFAULT 1 NOT NULL,
    error_detail text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_vault_audit ON biometric_vault_audit USING btree (voter_vin, "timestamp");

CREATE TABLE IF NOT EXISTS biometric_match_log (
    id SERIAL PRIMARY KEY,
    voter_vin text,
    modality text NOT NULL,
    match_score real DEFAULT 0 NOT NULL,
    is_genuine integer DEFAULT 0 NOT NULL,
    device_id text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_match_log_modality ON biometric_match_log USING btree (modality);

CREATE TABLE IF NOT EXISTS biometric_quality_scores (
    id SERIAL PRIMARY KEY,
    capture_id text,
    modality text,
    blur_score real,
    exposure_score real,
    angle_score real,
    overall_quality real,
    guidance text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS biometric_sdk_providers (
    id SERIAL PRIMARY KEY,
    provider_name text NOT NULL,
    sdk_version text NOT NULL,
    modalities text NOT NULL,
    license_type text DEFAULT 'commercial'::text,
    api_endpoint text,
    status text DEFAULT 'active'::text,
    accuracy_fingerprint real DEFAULT 0,
    accuracy_facial real DEFAULT 0,
    accuracy_iris real DEFAULT 0,
    last_health_check timestamp without time zone,
    registered_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bio_audit_timeline (
    id SERIAL PRIMARY KEY,
    event_type text NOT NULL,
    category text NOT NULL,
    severity text DEFAULT 'info'::text,
    actor text,
    voter_vin text,
    device_id text,
    details text,
    ip_address text,
    geo_location text,
    session_id text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_time ON bio_audit_timeline USING btree ("timestamp", event_type);

CREATE TABLE IF NOT EXISTS bvas_capture_sessions (
    id SERIAL PRIMARY KEY,
    session_id text NOT NULL,
    device_id text NOT NULL,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    capture_quality real DEFAULT 0 NOT NULL,
    nfiq2_score integer DEFAULT 0,
    capture_attempts integer DEFAULT 1,
    max_attempts integer DEFAULT 3,
    image_width integer DEFAULT 0,
    image_height integer DEFAULT 0,
    image_dpi integer DEFAULT 500,
    status text DEFAULT 'captured'::text,
    error_code text,
    processing_time_ms integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT bvas_capture_sessions_status_check CHECK ((status = ANY (ARRAY['initiated'::text, 'capturing'::text, 'captured'::text, 'quality_failed'::text, 'processed'::text, 'error'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bvas_cap ON bvas_capture_sessions USING btree (device_id, voter_vin);

CREATE TABLE IF NOT EXISTS abis_duplicate_checks (
    id SERIAL PRIMARY KEY,
    source_vin text NOT NULL,
    candidate_vin text,
    similarity_score real NOT NULL,
    modality text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    reviewed_by integer,
    reviewed_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT abis_duplicate_checks_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'confirmed_duplicate'::text, 'false_positive'::text, 'resolved'::text])))
);

CREATE TABLE IF NOT EXISTS abis_enrollment_pipeline (
    id SERIAL PRIMARY KEY,
    voter_vin text NOT NULL,
    stage text NOT NULL,
    modality text NOT NULL,
    device_id text,
    quality_passed integer DEFAULT 0,
    template_extracted integer DEFAULT 0,
    dedup_cleared integer DEFAULT 0,
    vault_stored integer DEFAULT 0,
    far_threshold real DEFAULT 0.0001,
    frr_threshold real DEFAULT 0.01,
    error_detail text,
    started_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    completed_at timestamp without time zone,
    CONSTRAINT abis_enrollment_pipeline_stage_check CHECK ((stage = ANY (ARRAY['capture'::text, 'quality_check'::text, 'template_extract'::text, 'dedup_check'::text, 'vault_store'::text, 'complete'::text, 'failed'::text])))
);

CREATE INDEX IF NOT EXISTS idx_abis_pipe ON abis_enrollment_pipeline USING btree (voter_vin, stage);

CREATE TABLE IF NOT EXISTS cancelable_transforms (
    id SERIAL PRIMARY KEY,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    transform_id text NOT NULL,
    transform_type text DEFAULT 'biohashing'::text NOT NULL,
    transform_seed bytea NOT NULL,
    version integer DEFAULT 1,
    revoked integer DEFAULT 0,
    revoked_at timestamp without time zone,
    revocation_reason text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cancel_vin ON cancelable_transforms USING btree (voter_vin, modality);

CREATE TABLE IF NOT EXISTS distributed_dedup_partitions (
    id SERIAL PRIMARY KEY,
    job_id integer NOT NULL,
    partition_key text NOT NULL,
    worker_id text NOT NULL,
    status text DEFAULT 'pending'::text,
    records_count integer DEFAULT 0,
    comparisons integer DEFAULT 0,
    duplicates integer DEFAULT 0,
    started_at timestamp without time zone,
    completed_at timestamp without time zone
);

CREATE TABLE IF NOT EXISTS liveness_checks (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    passed integer DEFAULT 0,
    confidence real,
    method text,
    anti_spoofing_score real,
    checks_json text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS multi_finger_enrollments (
    id SERIAL PRIMARY KEY,
    voter_vin text NOT NULL,
    finger_position text NOT NULL,
    finger_index integer NOT NULL,
    template_hash text NOT NULL,
    quality_score real DEFAULT 0,
    nfiq2_score integer DEFAULT 0,
    is_primary integer DEFAULT 0,
    is_fallback integer DEFAULT 0,
    enrolled_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_multi_finger ON multi_finger_enrollments USING btree (voter_vin, finger_position);

CREATE TABLE IF NOT EXISTS nist_benchmark_results (
    id SERIAL PRIMARY KEY,
    benchmark_type text NOT NULL,
    modality text NOT NULL,
    dataset text NOT NULL,
    total_subjects integer DEFAULT 0,
    total_comparisons integer DEFAULT 0,
    fnmr_at_fmr_001 real DEFAULT 0,
    fnmr_at_fmr_01 real DEFAULT 0,
    fnmr_at_fmr_1 real DEFAULT 0,
    eer real DEFAULT 0,
    throughput_per_sec real DEFAULT 0,
    template_size_bytes integer DEFAULT 0,
    run_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    status text DEFAULT 'completed'::text
);

CREATE TABLE IF NOT EXISTS offline_enrollment_queue (
    id SERIAL PRIMARY KEY,
    device_id text NOT NULL,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    template_data_hash text NOT NULL,
    queued_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    connectivity_status text DEFAULT 'offline'::text,
    sync_status text DEFAULT 'pending'::text,
    sync_attempts integer DEFAULT 0,
    synced_at timestamp without time zone,
    conflict_detected integer DEFAULT 0,
    resolution text
);

CREATE INDEX IF NOT EXISTS idx_offline_sync ON offline_enrollment_queue USING btree (sync_status, device_id);

CREATE TABLE IF NOT EXISTS pad_attack_log (
    id SERIAL PRIMARY KEY,
    voter_vin text,
    modality text NOT NULL,
    attack_type text NOT NULL,
    attack_instrument text,
    detection_score real NOT NULL,
    pad_model_id integer,
    texture_lbp_score real DEFAULT 0,
    frequency_score real DEFAULT 0,
    gradient_score real DEFAULT 0,
    color_hist_score real DEFAULT 0,
    motion_flow_score real DEFAULT 0,
    depth_consistency real DEFAULT 0,
    blocked integer DEFAULT 1 NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pad_attack ON pad_attack_log USING btree (voter_vin, created_at);

CREATE TABLE IF NOT EXISTS pad_models (
    id SERIAL PRIMARY KEY,
    model_name text NOT NULL,
    model_version text NOT NULL,
    modality text NOT NULL,
    attack_types text NOT NULL,
    accuracy real NOT NULL,
    far real NOT NULL,
    frr real NOT NULL,
    training_samples integer DEFAULT 0,
    iso_30107_level text DEFAULT 'level2'::text,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pad_results (
    id SERIAL PRIMARY KEY,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    device_id text,
    liveness_score real NOT NULL,
    texture_score real DEFAULT 0,
    motion_score real DEFAULT 0,
    depth_score real DEFAULT 0,
    spectral_score real DEFAULT 0,
    pad_decision text NOT NULL,
    pad_level text DEFAULT 'level2'::text NOT NULL,
    attack_type text,
    confidence real DEFAULT 0 NOT NULL,
    iso_30107_compliance integer DEFAULT 1,
    checked_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT pad_results_pad_decision_check CHECK ((pad_decision = ANY (ARRAY['live'::text, 'spoof'::text, 'uncertain'::text]))),
    CONSTRAINT pad_results_pad_level_check CHECK ((pad_level = ANY (ARRAY['level1'::text, 'level2'::text, 'level3'::text])))
);

CREATE INDEX IF NOT EXISTS idx_pad_vin ON pad_results USING btree (voter_vin, checked_at);

CREATE TABLE IF NOT EXISTS quality_gateway_rejections (
    id SERIAL PRIMARY KEY,
    device_id text NOT NULL,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    nfiq2_score integer DEFAULT 0,
    quality_score real DEFAULT 0,
    rejection_reason text NOT NULL,
    threshold_applied real DEFAULT 0,
    retry_count integer DEFAULT 0,
    bandwidth_saved_kb real DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS score_normalization_cohorts (
    id SERIAL PRIMARY KEY,
    cohort_id text NOT NULL,
    modality text NOT NULL,
    norm_type text DEFAULT 'z_norm'::text NOT NULL,
    mean_genuine real DEFAULT 0,
    std_genuine real DEFAULT 0,
    mean_impostor real DEFAULT 0,
    std_impostor real DEFAULT 0,
    sample_size integer DEFAULT 0,
    device_id text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS template_aging_records (
    id SERIAL PRIMARY KEY,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    enrolled_at timestamp without time zone NOT NULL,
    age_days integer DEFAULT 0,
    max_age_days integer DEFAULT 1825,
    quality_decay real DEFAULT 0,
    re_enrollment_required integer DEFAULT 0,
    re_enrollment_scheduled timestamp without time zone,
    re_enrollment_completed timestamp without time zone,
    notification_sent integer DEFAULT 0,
    status text DEFAULT 'valid'::text
);

CREATE INDEX IF NOT EXISTS idx_aging_status ON template_aging_records USING btree (status, re_enrollment_required);
CREATE INDEX IF NOT EXISTS idx_aging_vin ON template_aging_records USING btree (voter_vin);

CREATE TABLE IF NOT EXISTS threshold_tuning_runs (
    id SERIAL PRIMARY KEY,
    modality text NOT NULL,
    genuine_pairs integer DEFAULT 0,
    impostor_pairs integer DEFAULT 0,
    optimal_threshold real DEFAULT 0,
    eer real DEFAULT 0,
    far_at_threshold real DEFAULT 0,
    frr_at_threshold real DEFAULT 0,
    auc real DEFAULT 0,
    det_points text,
    roc_points text,
    status text DEFAULT 'completed'::text,
    run_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


-- BVAS extended: heartbeats, sync, capabilities, location

CREATE TABLE IF NOT EXISTS bvas_accreditations (
    id SERIAL PRIMARY KEY,
    device_id text NOT NULL,
    election_id integer NOT NULL,
    polling_unit_code text NOT NULL,
    voter_pvc_hash text NOT NULL,
    biometric_match integer DEFAULT 0 NOT NULL,
    pvc_verified integer DEFAULT 0 NOT NULL,
    method text DEFAULT 'biometric'::text NOT NULL,
    accredited_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    synced_at timestamp without time zone,
    CONSTRAINT bvas_accreditations_method_check CHECK ((method = ANY (ARRAY['biometric'::text, 'manual'::text, 'override'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bvas_acc_device ON bvas_accreditations USING btree (device_id);
CREATE INDEX IF NOT EXISTS idx_bvas_acc_pu ON bvas_accreditations USING btree (polling_unit_code, election_id);

CREATE TABLE IF NOT EXISTS bvas_device_capabilities (
    id SERIAL PRIMARY KEY,
    device_id text NOT NULL,
    firmware_version text NOT NULL,
    supported_modalities text DEFAULT 'fingerprint'::text NOT NULL,
    fingerprint_sensor text,
    fingerprint_fap_level text DEFAULT 'FAP30'::text,
    camera_resolution text,
    iris_sensor_type text,
    nfc_capable integer DEFAULT 0,
    secure_element text,
    tls_version text DEFAULT 'TLS1.3'::text,
    max_template_size integer DEFAULT 0,
    capture_quality_threshold real DEFAULT 0.7,
    last_calibrated_at timestamp without time zone,
    registered_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    status text DEFAULT 'active'::text,
    CONSTRAINT bvas_device_capabilities_status_check CHECK ((status = ANY (ARRAY['active'::text, 'maintenance'::text, 'decommissioned'::text])))
);

CREATE TABLE IF NOT EXISTS bvas_heartbeats (
    id SERIAL PRIMARY KEY,
    device_id text NOT NULL,
    battery_level integer,
    signal_strength integer,
    gps_latitude real,
    gps_longitude real,
    sync_queue_size integer DEFAULT 0,
    firmware_version text,
    uptime_seconds integer,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bvas_heartbeat_device ON bvas_heartbeats USING btree (device_id, "timestamp");

CREATE TABLE IF NOT EXISTS bvas_location_logs (
    id SERIAL PRIMARY KEY,
    bvas_serial text NOT NULL,
    polling_unit_code text NOT NULL,
    latitude real NOT NULL,
    longitude real NOT NULL,
    distance_from_pu_m real,
    within_geofence boolean,
    logged_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bvas_loc_pu ON bvas_location_logs USING btree (polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_bvas_loc_serial ON bvas_location_logs USING btree (bvas_serial);

CREATE TABLE IF NOT EXISTS bvas_sync_queue (
    id SERIAL PRIMARY KEY,
    device_id text NOT NULL,
    sync_type text NOT NULL,
    payload text NOT NULL,
    priority integer DEFAULT 5,
    status text DEFAULT 'queued'::text NOT NULL,
    conflict_resolution text,
    conflict_data text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    synced_at timestamp without time zone,
    retry_count integer DEFAULT 0,
    max_retries integer DEFAULT 5,
    CONSTRAINT bvas_sync_queue_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'syncing'::text, 'synced'::text, 'conflict'::text, 'failed'::text, 'resolved'::text]))),
    CONSTRAINT bvas_sync_queue_sync_type_check CHECK ((sync_type = ANY (ARRAY['accreditation'::text, 'result'::text, 'heartbeat'::text, 'config'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bvas_sync_status ON bvas_sync_queue USING btree (status, device_id);


-- Blockchain: Fabric, IPFS, merkle trees, smart contracts

CREATE TABLE IF NOT EXISTS blockchain_audit_trail (
    id SERIAL PRIMARY KEY,
    action text NOT NULL,
    entity_type text NOT NULL,
    entity_id text NOT NULL,
    actor text,
    prev_state text,
    new_state text,
    tx_hash text NOT NULL,
    block_ref integer,
    ip_address text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS blockchain_results (
    id SERIAL PRIMARY KEY,
    result_id integer NOT NULL,
    ec8a_hash text NOT NULL,
    prev_hash text DEFAULT ''::text NOT NULL,
    block_index integer NOT NULL,
    nonce integer DEFAULT 0,
    block_hash text NOT NULL,
    merkle_root text,
    level text NOT NULL,
    smart_contract_id text,
    validation_status text DEFAULT 'pending'::text NOT NULL,
    validator_count integer DEFAULT 0,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT blockchain_results_level_check CHECK ((level = ANY (ARRAY['polling_unit'::text, 'ward'::text, 'lga'::text, 'state'::text, 'national'::text]))),
    CONSTRAINT blockchain_results_validation_status_check CHECK ((validation_status = ANY (ARRAY['pending'::text, 'validated'::text, 'rejected'::text, 'disputed'::text])))
);

CREATE INDEX IF NOT EXISTS idx_blockchain_result ON blockchain_results USING btree (result_id);

CREATE TABLE IF NOT EXISTS chaincode_events (
    id SERIAL PRIMARY KEY,
    chaincode_id text NOT NULL,
    event_name text NOT NULL,
    tx_id text,
    payload text,
    block_number integer,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS fabric_blocks (
    block_number integer NOT NULL,
    channel_id text NOT NULL,
    prev_hash text NOT NULL,
    data_hash text NOT NULL,
    block_hash text NOT NULL,
    tx_count integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS fabric_chaincode (
    chaincode_id text NOT NULL,
    version text NOT NULL,
    channel_id text NOT NULL,
    endorsement_policy text NOT NULL,
    state_db text DEFAULT '{}'::text,
    install_date timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    status text DEFAULT 'active'::text
);

CREATE TABLE IF NOT EXISTS fabric_endorsement_log (
    id SERIAL PRIMARY KEY,
    tx_id text NOT NULL,
    peer_id text NOT NULL,
    msp_id text NOT NULL,
    signature text NOT NULL,
    proposal_hash text NOT NULL,
    response_status integer DEFAULT 200,
    response_payload text,
    endorsement_time_ms integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fabric_endorse ON fabric_endorsement_log USING btree (tx_id);

CREATE TABLE IF NOT EXISTS fabric_orderers (
    orderer_id text NOT NULL,
    org text NOT NULL,
    endpoint text NOT NULL,
    consensus_type text DEFAULT 'raft'::text,
    status text DEFAULT 'active'::text
);

CREATE TABLE IF NOT EXISTS fabric_peers (
    peer_id text NOT NULL,
    org text NOT NULL,
    msp_id text NOT NULL,
    endpoint text NOT NULL,
    role text DEFAULT 'endorser'::text,
    status text DEFAULT 'active'::text,
    last_seen timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS fabric_signing_keys (
    key_id text NOT NULL,
    private_key_pem text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS fabric_state_db (
    composite_key text NOT NULL,
    channel_id text NOT NULL,
    chaincode_id text NOT NULL,
    key text NOT NULL,
    value text NOT NULL,
    version_block integer DEFAULT 0 NOT NULL,
    version_tx integer DEFAULT 0 NOT NULL,
    is_delete integer DEFAULT 0,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fabric_state ON fabric_state_db USING btree (channel_id, chaincode_id, key);

CREATE TABLE IF NOT EXISTS fabric_transactions (
    tx_id text NOT NULL,
    block_number integer,
    channel_id text NOT NULL,
    chaincode_id text NOT NULL,
    function_name text NOT NULL,
    args text,
    creator_msp text NOT NULL,
    endorsers text,
    endorsement_policy text,
    rw_set text,
    validation_code text DEFAULT 'VALID'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fabric_tx_block ON fabric_transactions USING btree (block_number);
CREATE INDEX IF NOT EXISTS idx_fabric_tx_cc ON fabric_transactions USING btree (chaincode_id);

CREATE TABLE IF NOT EXISTS ipfs_dag_nodes (
    cid text NOT NULL,
    codec text DEFAULT 'dag-cbor'::text NOT NULL,
    multihash text NOT NULL,
    links text DEFAULT '[]'::text,
    data_size integer NOT NULL,
    raw_data bytea,
    pin_status text DEFAULT 'pinned'::text,
    replication_factor integer DEFAULT 3,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ipfs_dag ON ipfs_dag_nodes USING btree (codec, created_at);

CREATE TABLE IF NOT EXISTS ipfs_objects (
    cid text NOT NULL,
    content_type text NOT NULL,
    data_hash text NOT NULL,
    size_bytes integer NOT NULL,
    pinned integer DEFAULT 1,
    pin_count integer DEFAULT 1,
    references_to text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ipfs_type ON ipfs_objects USING btree (content_type);

CREATE TABLE IF NOT EXISTS ipfs_pins (
    cid text NOT NULL,
    node_id text NOT NULL,
    pin_type text DEFAULT 'recursive'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS merkle_trees (
    id SERIAL PRIMARY KEY,
    root_hash text NOT NULL,
    tree_type text NOT NULL,
    leaf_count integer NOT NULL,
    depth integer NOT NULL,
    leaves text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS smart_contracts (
    id SERIAL PRIMARY KEY,
    contract_id text NOT NULL,
    contract_type text NOT NULL,
    level text NOT NULL,
    area_code text NOT NULL,
    election_id integer NOT NULL,
    conditions text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    executed_at timestamp without time zone,
    result_hash text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT smart_contracts_contract_type_check CHECK ((contract_type = ANY (ARRAY['pu_validation'::text, 'ward_aggregation'::text, 'lga_aggregation'::text, 'state_aggregation'::text, 'national_declaration'::text]))),
    CONSTRAINT smart_contracts_status_check CHECK ((status = ANY (ARRAY['active'::text, 'executed'::text, 'failed'::text, 'expired'::text])))
);

CREATE INDEX IF NOT EXISTS idx_smart_contract ON smart_contracts USING btree (election_id, level);


-- Geospatial: landmarks, tracking, geofences, crowd

CREATE TABLE IF NOT EXISTS landmarks (
    id SERIAL PRIMARY KEY,
    name text NOT NULL,
    category text NOT NULL,
    latitude real NOT NULL,
    longitude real NOT NULL,
    geom public.geometry(Point,4326),
    state_code text,
    lga_code text,
    address text,
    description text,
    icon text DEFAULT 'marker'::text,
    importance integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_landmarks_category ON landmarks USING btree (category);
CREATE INDEX IF NOT EXISTS idx_landmarks_geom ON landmarks USING gist (geom);
CREATE INDEX IF NOT EXISTS idx_landmarks_state ON landmarks USING btree (state_code);

CREATE TABLE IF NOT EXISTS official_tracking (
    staff_id text NOT NULL,
    role text DEFAULT 'field_officer'::text NOT NULL,
    latitude real NOT NULL,
    longitude real NOT NULL,
    pu_code text,
    activity text DEFAULT 'patrol'::text,
    battery_pct integer DEFAULT 100,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_official_tracking_role ON official_tracking USING btree (role);
CREATE INDEX IF NOT EXISTS idx_official_tracking_updated ON official_tracking USING btree (updated_at DESC);

CREATE TABLE IF NOT EXISTS official_tracking_history (
    id bigint NOT NULL,
    staff_id text NOT NULL,
    role text NOT NULL,
    latitude double precision NOT NULL,
    longitude double precision NOT NULL,
    pu_code text,
    activity text,
    battery_pct integer DEFAULT 100,
    speed_kmh double precision DEFAULT 0,
    heading double precision DEFAULT 0,
    accuracy_m double precision DEFAULT 0,
    geom public.geometry(Point,4326),
    recorded_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tracking_hist_geom ON official_tracking_history USING gist (geom);
CREATE INDEX IF NOT EXISTS idx_tracking_hist_staff ON official_tracking_history USING btree (staff_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_tracking_hist_time ON official_tracking_history USING btree (recorded_at DESC);

CREATE TABLE IF NOT EXISTS crowd_density (
    id SERIAL PRIMARY KEY,
    pu_code text NOT NULL,
    latitude real,
    longitude real,
    head_count integer DEFAULT 0,
    density_level text DEFAULT 'moderate'::text,
    queue_length integer DEFAULT 0,
    wait_time_min integer DEFAULT 0,
    notes text,
    reporter_id text,
    reported_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_crowd_density_level ON crowd_density USING btree (density_level);
CREATE INDEX IF NOT EXISTS idx_crowd_density_pu ON crowd_density USING btree (pu_code);
CREATE INDEX IF NOT EXISTS idx_crowd_density_reported ON crowd_density USING btree (reported_at DESC);

CREATE TABLE IF NOT EXISTS crowd_alerts (
    id SERIAL PRIMARY KEY,
    pu_code text NOT NULL,
    alert_type text NOT NULL,
    severity text DEFAULT 'warning'::text,
    head_count integer,
    density_level text,
    message text,
    acknowledged boolean DEFAULT false,
    acknowledged_by text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_crowd_alerts_created ON crowd_alerts USING btree (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_crowd_alerts_severity ON crowd_alerts USING btree (severity);

CREATE TABLE IF NOT EXISTS geo_analytics_cache (
    id text NOT NULL,
    data jsonb NOT NULL,
    computed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone
);

CREATE TABLE IF NOT EXISTS geo_events (
    id SERIAL PRIMARY KEY,
    polling_unit_code text NOT NULL,
    event_type text NOT NULL,
    latitude real,
    longitude real,
    payload jsonb,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_geo_events_created ON geo_events USING btree (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_geo_events_pu ON geo_events USING btree (polling_unit_code);

CREATE TABLE IF NOT EXISTS geofence_attestations (
    id SERIAL PRIMARY KEY,
    staff_id text NOT NULL,
    pu_code text NOT NULL,
    latitude double precision NOT NULL,
    longitude double precision NOT NULL,
    within_geofence boolean NOT NULL,
    distance_m double precision,
    signature_hash text NOT NULL,
    blockchain_tx text,
    attested_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_geofence_att_staff ON geofence_attestations USING btree (staff_id, attested_at DESC);

CREATE TABLE IF NOT EXISTS geofence_zones (
    id SERIAL PRIMARY KEY,
    pu_code text NOT NULL,
    center_lat double precision NOT NULL,
    center_lng double precision NOT NULL,
    radius_m double precision DEFAULT 500,
    geom public.geometry(Polygon,4326),
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_geofence_zones_geom ON geofence_zones USING gist (geom);
CREATE INDEX IF NOT EXISTS idx_geofence_zones_pu ON geofence_zones USING btree (pu_code);

CREATE TABLE IF NOT EXISTS geofenced_submissions (
    id SERIAL PRIMARY KEY,
    result_id integer,
    officer_lat real NOT NULL,
    officer_lng real NOT NULL,
    pu_lat real,
    pu_lng real,
    distance_meters real,
    within_boundary integer DEFAULT 0,
    override_by integer,
    override_reason text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS incident_locations (
    id SERIAL PRIMARY KEY,
    incident_id integer,
    latitude double precision NOT NULL,
    longitude double precision NOT NULL,
    geom public.geometry(Point,4326),
    severity text DEFAULT 'medium'::text,
    incident_type text,
    description text,
    resolved boolean DEFAULT false,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_incident_loc_geom ON incident_locations USING gist (geom);

CREATE TABLE IF NOT EXISTS pu_photos (
    id SERIAL PRIMARY KEY,
    pu_code text NOT NULL,
    photo_url text NOT NULL,
    caption text,
    photo_type text DEFAULT 'verification'::text,
    latitude double precision,
    longitude double precision,
    geom public.geometry(Point,4326),
    uploaded_by text,
    verified boolean DEFAULT false,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pu_photos_code ON pu_photos USING btree (pu_code);
CREATE INDEX IF NOT EXISTS idx_pu_photos_geom ON pu_photos USING gist (geom);


-- Middleware persistence: event bus, state, cache, ledger

CREATE TABLE IF NOT EXISTS mw_state (
    store_name text NOT NULL,
    key text NOT NULL,
    value text NOT NULL,
    version integer DEFAULT 1,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mw_cache (
    key text NOT NULL,
    value text NOT NULL,
    ttl_seconds integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone
);

CREATE INDEX IF NOT EXISTS idx_mw_cache_expires ON mw_cache USING btree (expires_at);

CREATE TABLE IF NOT EXISTS mw_events (
    id SERIAL PRIMARY KEY,
    topic text NOT NULL,
    key text,
    value text NOT NULL,
    headers text,
    partition_id integer DEFAULT 0,
    offset_id integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_events_offset ON mw_events USING btree (topic, partition_id, offset_id);
CREATE INDEX IF NOT EXISTS idx_mw_events_topic ON mw_events USING btree (topic, created_at);

CREATE TABLE IF NOT EXISTS mw_pubsub (
    id SERIAL PRIMARY KEY,
    channel text NOT NULL,
    message text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_pubsub_channel ON mw_pubsub USING btree (channel, created_at);

CREATE TABLE IF NOT EXISTS mw_streams (
    id SERIAL PRIMARY KEY,
    topic text NOT NULL,
    key text,
    value text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_streams_topic ON mw_streams USING btree (topic, created_at);

CREATE TABLE IF NOT EXISTS mw_consumer_offsets (
    consumer_group text NOT NULL,
    topic text NOT NULL,
    partition_id integer DEFAULT 0 NOT NULL,
    offset_id integer DEFAULT 0,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mw_search_index (
    id SERIAL PRIMARY KEY,
    index_name text NOT NULL,
    doc_id text NOT NULL,
    body text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_search_index ON mw_search_index USING btree (index_name, doc_id);

CREATE TABLE IF NOT EXISTS mw_workflows (
    id text NOT NULL,
    workflow_type text NOT NULL,
    status text DEFAULT 'running'::text,
    input text,
    result text,
    error_msg text,
    started_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    completed_at timestamp without time zone
);

CREATE TABLE IF NOT EXISTS mw_waf_events (
    id SERIAL PRIMARY KEY,
    request_id text,
    source_ip text,
    method text,
    path text,
    rule_id text,
    action text DEFAULT 'allow'::text,
    threat_level text DEFAULT 'none'::text,
    details text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_waf_created ON mw_waf_events USING btree (created_at);

CREATE TABLE IF NOT EXISTS mw_circuit_breaker_log (
    id SERIAL PRIMARY KEY,
    service text NOT NULL,
    state text NOT NULL,
    failures integer DEFAULT 0,
    details text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mw_ledger_accounts (
    id text NOT NULL,
    debits_pending integer DEFAULT 0,
    debits_posted integer DEFAULT 0,
    credits_pending integer DEFAULT 0,
    credits_posted integer DEFAULT 0,
    ledger integer DEFAULT 1,
    code integer DEFAULT 0,
    flags integer DEFAULT 0,
    user_data text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mw_ledger_transfers (
    id text NOT NULL,
    debit_account_id text NOT NULL,
    credit_account_id text NOT NULL,
    amount integer NOT NULL,
    ledger integer DEFAULT 1,
    code integer DEFAULT 0,
    flags integer DEFAULT 0,
    pending_id text,
    user_data text,
    status text DEFAULT 'posted'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mw_mojaloop_transactions (
    id text NOT NULL,
    payer_fsp text NOT NULL,
    payee_fsp text NOT NULL,
    amount real NOT NULL,
    currency text DEFAULT 'NGN'::text,
    phase text DEFAULT 'discovery'::text,
    quote_id text,
    transfer_id text,
    settlement_id text,
    ilp_packet text,
    condition text,
    fulfilment text,
    error_info text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_mojaloop_phase ON mw_mojaloop_transactions USING btree (phase);

CREATE TABLE IF NOT EXISTS event_bus (
    id SERIAL PRIMARY KEY,
    topic text NOT NULL,
    event_key text,
    payload text NOT NULL,
    offset_id integer DEFAULT 0 NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_event_bus_topic ON event_bus USING btree (topic, offset_id);

CREATE TABLE IF NOT EXISTS event_bus_topics (
    topic text NOT NULL,
    partitions integer DEFAULT 1,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


-- Security: encryption keys, HSM, audit events, access policies

CREATE TABLE IF NOT EXISTS active_sessions (
    id SERIAL PRIMARY KEY,
    jti text NOT NULL,
    user_id integer NOT NULL,
    ip_address text,
    user_agent text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone NOT NULL,
    last_activity timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sessions_jti ON active_sessions USING btree (jti);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON active_sessions USING btree (user_id);

CREATE TABLE IF NOT EXISTS data_classification (
    id SERIAL PRIMARY KEY,
    table_name text NOT NULL,
    column_name text NOT NULL,
    classification text NOT NULL,
    encryption_required integer DEFAULT 0,
    pii integer DEFAULT 0,
    retention_days integer DEFAULT 365,
    CONSTRAINT data_classification_classification_check CHECK ((classification = ANY (ARRAY['public'::text, 'internal'::text, 'confidential'::text, 'restricted'::text])))
);

CREATE TABLE IF NOT EXISTS data_encryption_keys (
    id SERIAL PRIMARY KEY,
    key_id text NOT NULL,
    algorithm text DEFAULT 'AES-256-GCM'::text NOT NULL,
    purpose text NOT NULL,
    key_version integer DEFAULT 1,
    status text DEFAULT 'active'::text,
    rotated_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT data_encryption_keys_purpose_check CHECK ((purpose = ANY (ARRAY['pii_encryption'::text, 'biometric_encryption'::text, 'result_signing'::text, 'api_key_encryption'::text, 'backup_encryption'::text]))),
    CONSTRAINT data_encryption_keys_status_check CHECK ((status = ANY (ARRAY['active'::text, 'rotating'::text, 'retired'::text, 'compromised'::text])))
);

CREATE INDEX IF NOT EXISTS idx_dek_purpose ON data_encryption_keys USING btree (purpose, status);

CREATE TABLE IF NOT EXISTS device_keys (
    id SERIAL PRIMARY KEY,
    user_id text NOT NULL,
    device_id text NOT NULL,
    device_secret text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS device_locations (
    id SERIAL PRIMARY KEY,
    device_id text NOT NULL,
    ip_address text,
    latitude real,
    longitude real,
    last_seen timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_device_locations_device ON device_locations USING btree (device_id);

CREATE TABLE IF NOT EXISTS export_audit_log (
    id SERIAL PRIMARY KEY,
    user_id text NOT NULL,
    export_type text,
    format text,
    ip_address text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_export_audit_user ON export_audit_log USING btree (user_id, "timestamp");

CREATE TABLE IF NOT EXISTS hsm_audit (
    id SERIAL PRIMARY KEY,
    operation text NOT NULL,
    key_id text,
    hsm_slot integer,
    success integer DEFAULT 1,
    latency_us integer DEFAULT 0,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS hsm_keys (
    id SERIAL PRIMARY KEY,
    key_id text NOT NULL,
    key_type text DEFAULT 'AES-256-GCM'::text NOT NULL,
    purpose text NOT NULL,
    algorithm text DEFAULT 'AES'::text NOT NULL,
    key_size integer DEFAULT 256 NOT NULL,
    encrypted_material bytea NOT NULL,
    wrapping_key_id text,
    pkcs11_label text,
    pkcs11_id text,
    usage_count integer DEFAULT 0,
    max_usage integer DEFAULT 0,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone,
    last_used_at timestamp without time zone,
    rotation_schedule text DEFAULT 'monthly'::text,
    metadata text DEFAULT '{}'::text
);

CREATE INDEX IF NOT EXISTS idx_hsm_keys_purpose ON hsm_keys USING btree (purpose, status);

CREATE TABLE IF NOT EXISTS hsm_operations (
    id SERIAL PRIMARY KEY,
    operation text NOT NULL,
    key_id text NOT NULL,
    algorithm text,
    input_hash text,
    output_hash text,
    latency_us integer,
    success integer DEFAULT 1 NOT NULL,
    error_detail text,
    actor text,
    ip_address text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_hsm_ops_key ON hsm_operations USING btree (key_id, "timestamp");

CREATE TABLE IF NOT EXISTS hsm_slot_keys (
    slot_id integer NOT NULL,
    key_material text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS privacy_preserving_ops (
    id SERIAL PRIMARY KEY,
    operation_type text NOT NULL,
    encryption_scheme text DEFAULT 'paillier'::text NOT NULL,
    voter_vin text,
    modality text,
    computation_time_ms integer DEFAULT 0,
    template_never_decrypted integer DEFAULT 1,
    result_encrypted integer DEFAULT 1,
    status text DEFAULT 'completed'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS row_access_policies (
    id SERIAL PRIMARY KEY,
    table_name text NOT NULL,
    policy_name text NOT NULL,
    role text NOT NULL,
    condition_column text NOT NULL,
    condition_value text NOT NULL,
    permission text DEFAULT 'read'::text NOT NULL
);

CREATE TABLE IF NOT EXISTS security_audit_events (
    id SERIAL PRIMARY KEY,
    event_type text NOT NULL,
    severity text NOT NULL,
    source text NOT NULL,
    user_id integer,
    ip_address text,
    details text DEFAULT '{}'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT security_audit_events_severity_check CHECK ((severity = ANY (ARRAY['info'::text, 'low'::text, 'medium'::text, 'high'::text, 'critical'::text])))
);

CREATE INDEX IF NOT EXISTS idx_security_events_type ON security_audit_events USING btree (event_type, severity, created_at);

CREATE TABLE IF NOT EXISTS security_threats (
    id SERIAL PRIMARY KEY,
    threat_type text NOT NULL,
    location text NOT NULL,
    latitude real,
    longitude real,
    severity text DEFAULT 'medium'::text NOT NULL,
    confidence real DEFAULT 0.5,
    source text,
    description text,
    affected_pus integer DEFAULT 0,
    status text DEFAULT 'active'::text NOT NULL,
    detected_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    resolved_at timestamp without time zone,
    CONSTRAINT security_threats_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))),
    CONSTRAINT security_threats_status_check CHECK ((status = ANY (ARRAY['active'::text, 'monitoring'::text, 'mitigated'::text, 'resolved'::text, 'false_alarm'::text]))),
    CONSTRAINT security_threats_threat_type_check CHECK ((threat_type = ANY (ARRAY['violence'::text, 'protest'::text, 'road_blockage'::text, 'device_theft'::text, 'cyber_attack'::text, 'impersonation'::text, 'other'::text])))
);

CREATE INDEX IF NOT EXISTS idx_security ON security_threats USING btree (status, severity);


-- Observer monitoring and dispute resolution

CREATE TABLE IF NOT EXISTS observer_alert_rules (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    party_code text,
    state_code text,
    lga_code text,
    alert_type text DEFAULT 'result_submitted'::text NOT NULL,
    is_active integer DEFAULT 1 NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS observer_check_ins (
    id SERIAL PRIMARY KEY,
    observer_id integer NOT NULL,
    polling_unit_code text NOT NULL,
    latitude real,
    longitude real,
    device_info text,
    within_geofence boolean,
    checked_in_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_observer_checkins_observer ON observer_check_ins USING btree (observer_id);

CREATE TABLE IF NOT EXISTS observer_photo_verifications (
    id SERIAL PRIMARY KEY,
    observer_id integer,
    pu_code text,
    photo_hash text,
    gps_lat real,
    gps_lng real,
    timestamp_watermark text,
    consensus_score real DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS observer_reports (
    id SERIAL PRIMARY KEY,
    observer_id integer NOT NULL,
    polling_unit_code text NOT NULL,
    election_id integer NOT NULL,
    report_type text DEFAULT 'observation'::text NOT NULL,
    description text,
    photo_url text,
    latitude real,
    longitude real,
    status text DEFAULT 'pending'::text NOT NULL,
    review_notes text,
    reviewed_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_observer_reports_election ON observer_reports USING btree (election_id);
CREATE INDEX IF NOT EXISTS idx_observer_reports_pu ON observer_reports USING btree (polling_unit_code);

CREATE TABLE IF NOT EXISTS disputes (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    polling_unit_code text,
    filed_by text NOT NULL,
    party text,
    category text NOT NULL,
    description text NOT NULL,
    evidence text,
    status text DEFAULT 'filed'::text NOT NULL,
    assigned_to text,
    resolution text,
    resolved_by text,
    filed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    resolved_at timestamp without time zone,
    priority text DEFAULT 'medium'::text
);

CREATE TABLE IF NOT EXISTS dispute_comments (
    id SERIAL PRIMARY KEY,
    dispute_id integer NOT NULL,
    author text NOT NULL,
    content text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


-- API keys, webhooks, portal, push notifications

CREATE TABLE IF NOT EXISTS api_keys (
    id SERIAL PRIMARY KEY,
    key_hash text NOT NULL,
    name text NOT NULL,
    owner text NOT NULL,
    permissions text DEFAULT 'read'::text NOT NULL,
    rate_limit integer DEFAULT 100 NOT NULL,
    is_active integer DEFAULT 1 NOT NULL,
    last_used_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys USING btree (key_hash);

CREATE TABLE IF NOT EXISTS api_usage (
    id SERIAL PRIMARY KEY,
    api_key_id integer,
    endpoint text NOT NULL,
    method text NOT NULL,
    status_code integer,
    response_ms real,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_api_usage_key ON api_usage USING btree (api_key_id);

CREATE TABLE IF NOT EXISTS api_key_metadata (
    id SERIAL PRIMARY KEY,
    key_hash text NOT NULL,
    name text NOT NULL,
    owner_id integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone,
    rotated_from text,
    is_active boolean DEFAULT true,
    last_used_at timestamp without time zone,
    usage_count integer DEFAULT 0
);

CREATE TABLE IF NOT EXISTS dead_letter_queue (
    id text NOT NULL,
    job_id text NOT NULL,
    job_type text NOT NULL,
    error_message text NOT NULL,
    payload text NOT NULL,
    failed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    reprocessed integer DEFAULT 0,
    reprocessed_at timestamp without time zone
);

CREATE INDEX IF NOT EXISTS idx_dlq_reprocessed ON dead_letter_queue USING btree (reprocessed);

CREATE TABLE IF NOT EXISTS ingestion_jobs (
    id text NOT NULL,
    job_type text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    payload text NOT NULL,
    idempotency_key text,
    retries integer DEFAULT 0,
    max_retries integer DEFAULT 3,
    error_message text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    processed_at timestamp without time zone,
    latency_ms real,
    CONSTRAINT ingestion_jobs_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'in_progress'::text, 'completed'::text, 'failed'::text, 'dead_letter'::text])))
);

CREATE INDEX IF NOT EXISTS idx_ingestion_idem ON ingestion_jobs USING btree (idempotency_key);
CREATE INDEX IF NOT EXISTS idx_ingestion_status ON ingestion_jobs USING btree (status);

CREATE TABLE IF NOT EXISTS offline_sync_queue (
    id SERIAL PRIMARY KEY,
    device_id text NOT NULL,
    sync_type text NOT NULL,
    payload text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    synced_at timestamp without time zone,
    retries integer DEFAULT 0,
    CONSTRAINT offline_sync_queue_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'syncing'::text, 'synced'::text, 'failed'::text]))),
    CONSTRAINT offline_sync_queue_sync_type_check CHECK ((sync_type = ANY (ARRAY['result'::text, 'accreditation'::text, 'incident'::text])))
);

CREATE INDEX IF NOT EXISTS idx_offline_status ON offline_sync_queue USING btree (status);

CREATE TABLE IF NOT EXISTS portal_connections (
    id SERIAL PRIMARY KEY,
    portal_name text NOT NULL,
    portal_type text NOT NULL,
    base_url text NOT NULL,
    api_key_hash text,
    status text DEFAULT 'active'::text NOT NULL,
    last_sync_at timestamp without time zone,
    sync_interval_seconds integer DEFAULT 300,
    webhook_url text,
    metadata text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT portal_connections_portal_type_check CHECK ((portal_type = ANY (ARRAY['irev'::text, 'icnp'::text, 'press'::text, 'croms'::text, 'bvas_portal'::text, 'custom'::text]))),
    CONSTRAINT portal_connections_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'error'::text, 'maintenance'::text])))
);

CREATE TABLE IF NOT EXISTS portal_sync_log (
    id SERIAL PRIMARY KEY,
    portal_id integer NOT NULL,
    sync_type text NOT NULL,
    entity_type text NOT NULL,
    records_synced integer DEFAULT 0,
    records_failed integer DEFAULT 0,
    status text DEFAULT 'completed'::text NOT NULL,
    error_message text,
    started_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    completed_at timestamp without time zone,
    CONSTRAINT portal_sync_log_status_check CHECK ((status = ANY (ARRAY['in_progress'::text, 'completed'::text, 'failed'::text, 'partial'::text]))),
    CONSTRAINT portal_sync_log_sync_type_check CHECK ((sync_type = ANY (ARRAY['push'::text, 'pull'::text, 'webhook'::text])))
);

CREATE INDEX IF NOT EXISTS idx_portal_sync ON portal_sync_log USING btree (portal_id, started_at);

CREATE TABLE IF NOT EXISTS portal_webhooks (
    id SERIAL PRIMARY KEY,
    portal_id integer NOT NULL,
    event_type text NOT NULL,
    payload text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    retry_count integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    delivered_at timestamp without time zone,
    CONSTRAINT portal_webhooks_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'delivered'::text, 'failed'::text, 'retrying'::text])))
);

CREATE TABLE IF NOT EXISTS push_devices (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    device_token text NOT NULL,
    platform text DEFAULT 'android'::text NOT NULL,
    app_version text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    last_active timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    is_active integer DEFAULT 1
);

CREATE TABLE IF NOT EXISTS push_notifications (
    id SERIAL PRIMARY KEY,
    target_type text NOT NULL,
    target_value text,
    title text NOT NULL,
    body text NOT NULL,
    notification_type text DEFAULT 'info'::text NOT NULL,
    sent_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    read_count integer DEFAULT 0,
    total_recipients integer DEFAULT 0,
    CONSTRAINT push_notifications_notification_type_check CHECK ((notification_type = ANY (ARRAY['info'::text, 'alert'::text, 'update'::text, 'emergency'::text]))),
    CONSTRAINT push_notifications_target_type_check CHECK ((target_type = ANY (ARRAY['all'::text, 'stakeholder_type'::text, 'individual'::text, 'area'::text])))
);

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id SERIAL PRIMARY KEY,
    url text NOT NULL,
    events text NOT NULL,
    secret text NOT NULL,
    created_by text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    is_active integer DEFAULT 1
);


-- AI/ML: predictions, document AI, KYC, sentiment analysis

CREATE TABLE IF NOT EXISTS ai_predictions (
    id SERIAL PRIMARY KEY,
    prediction_type text NOT NULL,
    target_area text NOT NULL,
    target_level text NOT NULL,
    predicted_value real NOT NULL,
    confidence real NOT NULL,
    model_name text NOT NULL,
    features_used text,
    election_id integer,
    predicted_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ai_predictions_prediction_type_check CHECK ((prediction_type = ANY (ARRAY['turnout'::text, 'resource'::text, 'security_threat'::text, 'sentiment'::text, 'misinformation'::text]))),
    CONSTRAINT ai_predictions_target_level_check CHECK ((target_level = ANY (ARRAY['national'::text, 'state'::text, 'lga'::text, 'ward'::text, 'polling_unit'::text])))
);

CREATE INDEX IF NOT EXISTS idx_ai_pred ON ai_predictions USING btree (prediction_type, target_area);

CREATE TABLE IF NOT EXISTS cv_monitoring (
    id SERIAL PRIMARY KEY,
    camera_id text NOT NULL,
    polling_unit_code text,
    event_type text NOT NULL,
    value real,
    description text,
    frame_url text,
    confidence real DEFAULT 0.8,
    detected_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT cv_monitoring_event_type_check CHECK ((event_type = ANY (ARRAY['crowd_size'::text, 'queue_length'::text, 'suspicious_activity'::text, 'equipment_status'::text, 'accessibility_issue'::text])))
);

CREATE TABLE IF NOT EXISTS document_analyses (
    id SERIAL PRIMARY KEY,
    report_id integer,
    analysis_type text DEFAULT 'full'::text NOT NULL,
    ocr_confidence real,
    vlm_tampering_detected integer DEFAULT 0,
    vlm_quality text,
    combined_confidence real,
    requires_review integer DEFAULT 0,
    party_results_json text,
    raw_ocr_text text,
    warnings_json text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS kyb_verifications (
    id SERIAL PRIMARY KEY,
    entity_id integer NOT NULL,
    entity_type text NOT NULL,
    entity_name text NOT NULL,
    registration_number text,
    registration_verified integer DEFAULT 0,
    authorized_signatories text DEFAULT '[]'::text,
    documents_verified integer DEFAULT 0,
    compliance_score real DEFAULT 0,
    risk_level text DEFAULT 'pending'::text,
    status text DEFAULT 'pending'::text,
    reviewed_by integer,
    review_notes text,
    verified_at timestamp without time zone,
    expires_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT kyb_verifications_entity_type_check CHECK ((entity_type = ANY (ARRAY['political_party'::text, 'observer_org'::text, 'media_org'::text, 'ngo'::text, 'inec_partner'::text]))),
    CONSTRAINT kyb_verifications_risk_level_check CHECK ((risk_level = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text, 'pending'::text]))),
    CONSTRAINT kyb_verifications_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'under_review'::text, 'approved'::text, 'rejected'::text, 'suspended'::text, 'expired'::text])))
);

CREATE INDEX IF NOT EXISTS idx_kyb_entity ON kyb_verifications USING btree (entity_id, entity_type);
CREATE INDEX IF NOT EXISTS idx_kyb_status ON kyb_verifications USING btree (status);

CREATE TABLE IF NOT EXISTS kyc_events (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    event_type text NOT NULL,
    trigger_source text NOT NULL,
    details text DEFAULT '{}'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_kyc_events_user ON kyc_events USING btree (user_id, event_type);

CREATE TABLE IF NOT EXISTS kyc_verifications (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    id_type text,
    id_number_hash text,
    identity_match_score real,
    document_verified integer DEFAULT 0,
    face_match_score real,
    liveness_passed integer DEFAULT 0,
    risk_score real,
    checks_json text,
    flags_json text,
    verified_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS media_api_keys (
    id SERIAL PRIMARY KEY,
    api_key text NOT NULL,
    org_name text NOT NULL,
    contact_email text,
    rate_limit integer DEFAULT 600,
    is_active integer DEFAULT 1,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS misinformation_alerts (
    id SERIAL PRIMARY KEY,
    content text NOT NULL,
    source_platform text,
    source_url text,
    classification text NOT NULL,
    confidence real NOT NULL,
    severity text DEFAULT 'medium'::text NOT NULL,
    reach_estimate integer DEFAULT 0,
    status text DEFAULT 'detected'::text NOT NULL,
    fact_check text,
    detected_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT misinformation_alerts_classification_check CHECK ((classification = ANY (ARRAY['fake_result'::text, 'false_claim'::text, 'manipulated_media'::text, 'impersonation'::text, 'incitement'::text, 'other'::text]))),
    CONSTRAINT misinformation_alerts_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))),
    CONSTRAINT misinformation_alerts_status_check CHECK ((status = ANY (ARRAY['detected'::text, 'verified'::text, 'debunked'::text, 'monitoring'::text, 'escalated'::text])))
);

CREATE INDEX IF NOT EXISTS idx_misinfo ON misinformation_alerts USING btree (status, detected_at);

CREATE TABLE IF NOT EXISTS model_metrics (
    id SERIAL PRIMARY KEY,
    model_name text NOT NULL,
    accuracy real NOT NULL,
    latency_ms real,
    sample_count integer,
    evaluated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_model_metrics_name ON model_metrics USING btree (model_name, evaluated_at);

CREATE TABLE IF NOT EXISTS predictive_analytics (
    id SERIAL PRIMARY KEY,
    election_id integer,
    state_code text,
    predicted_turnout real,
    confidence real DEFAULT 0.8,
    model_version text DEFAULT 'v1'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sentiment_analysis (
    id SERIAL PRIMARY KEY,
    source text NOT NULL,
    content_snippet text,
    sentiment text NOT NULL,
    score real NOT NULL,
    topics text,
    location text,
    language text DEFAULT 'en'::text,
    election_id integer,
    analyzed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT sentiment_analysis_sentiment_check CHECK ((sentiment = ANY (ARRAY['positive'::text, 'negative'::text, 'neutral'::text, 'mixed'::text]))),
    CONSTRAINT sentiment_analysis_source_check CHECK ((source = ANY (ARRAY['twitter'::text, 'facebook'::text, 'news'::text, 'radio'::text, 'whatsapp'::text, 'other'::text])))
);

CREATE INDEX IF NOT EXISTS idx_sentiment ON sentiment_analysis USING btree (sentiment, analyzed_at);

CREATE TABLE IF NOT EXISTS video_analyses (
    id SERIAL PRIMARY KEY,
    report_id integer,
    observer_id integer,
    filename text,
    duration_sec real,
    frame_count integer,
    anomaly_count integer,
    ballot_event_count integer,
    integrity_score real,
    summary text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


-- Platform: MFA, SMS/USSD, training, registration

CREATE TABLE IF NOT EXISTS mfa_settings (
    user_id integer NOT NULL,
    totp_enabled integer DEFAULT 0,
    webauthn_enabled integer DEFAULT 0,
    sms_enabled integer DEFAULT 0,
    enforce_on_write integer DEFAULT 1,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mfa_sms_otp (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    phone text NOT NULL,
    code text NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    used integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mfa_totp (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    secret text NOT NULL,
    verified integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mfa_webauthn (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    credential_id text NOT NULL,
    public_key text NOT NULL,
    sign_count integer DEFAULT 0,
    device_name text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sms_delivery_log (
    id SERIAL PRIMARY KEY,
    provider text NOT NULL,
    message_id text,
    phone text NOT NULL,
    message text NOT NULL,
    direction text DEFAULT 'outbound'::text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    cost real DEFAULT 0,
    delivery_report text,
    provider_response text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    delivered_at timestamp without time zone
);

CREATE INDEX IF NOT EXISTS idx_sms_delivery ON sms_delivery_log USING btree (phone, created_at);

CREATE TABLE IF NOT EXISTS sms_verifications (
    id SERIAL PRIMARY KEY,
    phone text NOT NULL,
    polling_unit_code text,
    election_id integer,
    request_type text NOT NULL,
    response_text text,
    channel text DEFAULT 'sms'::text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT sms_verifications_channel_check CHECK ((channel = ANY (ARRAY['sms'::text, 'ussd'::text]))),
    CONSTRAINT sms_verifications_request_type_check CHECK ((request_type = ANY (ARRAY['result'::text, 'status'::text, 'verify'::text])))
);

CREATE INDEX IF NOT EXISTS idx_sms_phone ON sms_verifications USING btree (phone);

CREATE TABLE IF NOT EXISTS ussd_sessions (
    id text NOT NULL,
    phone text NOT NULL,
    stage text DEFAULT 'main_menu'::text NOT NULL,
    data text DEFAULT '{}'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS registration_centers (
    id SERIAL PRIMARY KEY,
    code text NOT NULL,
    name text NOT NULL,
    state_code text NOT NULL,
    lga_code text NOT NULL,
    ward_code text NOT NULL,
    address text,
    latitude real,
    longitude real,
    capacity integer DEFAULT 500,
    status text DEFAULT 'active'::text NOT NULL,
    start_date text,
    end_date text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT registration_centers_status_check CHECK ((status = ANY (ARRAY['active'::text, 'closed'::text, 'suspended'::text])))
);

CREATE TABLE IF NOT EXISTS training_certificates (
    id SERIAL PRIMARY KEY,
    enrollment_id integer NOT NULL,
    user_id integer NOT NULL,
    course_id integer NOT NULL,
    certificate_id text NOT NULL,
    blockchain_hash text NOT NULL,
    score integer NOT NULL,
    issued_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone,
    verification_url text
);

CREATE TABLE IF NOT EXISTS training_courses (
    id SERIAL PRIMARY KEY,
    title text NOT NULL,
    description text,
    course_type text NOT NULL,
    target_role text NOT NULL,
    difficulty text DEFAULT 'beginner'::text NOT NULL,
    duration_minutes integer DEFAULT 60,
    passing_score integer DEFAULT 70,
    modules_count integer DEFAULT 1,
    is_mandatory integer DEFAULT 0,
    is_active integer DEFAULT 1,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT training_courses_course_type_check CHECK ((course_type = ANY (ARRAY['vr_simulation'::text, 'gamified'::text, 'video'::text, 'interactive'::text, 'assessment'::text]))),
    CONSTRAINT training_courses_difficulty_check CHECK ((difficulty = ANY (ARRAY['beginner'::text, 'intermediate'::text, 'advanced'::text, 'expert'::text])))
);

CREATE TABLE IF NOT EXISTS training_enrollments (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    course_id integer NOT NULL,
    progress_percent real DEFAULT 0,
    current_module integer DEFAULT 1,
    score integer,
    status text DEFAULT 'enrolled'::text NOT NULL,
    started_at timestamp without time zone,
    completed_at timestamp without time zone,
    certificate_hash text,
    enrolled_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT training_enrollments_status_check CHECK ((status = ANY (ARRAY['enrolled'::text, 'in_progress'::text, 'completed'::text, 'failed'::text, 'expired'::text])))
);

CREATE INDEX IF NOT EXISTS idx_training_enroll ON training_enrollments USING btree (user_id, course_id);

CREATE TABLE IF NOT EXISTS training_vr_scenarios (
    id SERIAL PRIMARY KEY,
    course_id integer NOT NULL,
    scenario_name text NOT NULL,
    scenario_type text NOT NULL,
    description text,
    max_score integer DEFAULT 100,
    avg_completion_time integer,
    difficulty text DEFAULT 'intermediate'::text,
    is_active integer DEFAULT 1,
    CONSTRAINT training_vr_scenarios_scenario_type_check CHECK ((scenario_type = ANY (ARRAY['election_day'::text, 'emergency'::text, 'crowd_control'::text, 'result_collation'::text, 'equipment_setup'::text, 'conflict_resolution'::text])))
);


