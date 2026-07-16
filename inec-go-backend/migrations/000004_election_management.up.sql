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


