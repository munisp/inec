-- W2 (found while landing R5-016): the evidence-integrity tables only ever
-- existed in the SQLite/dev auto-schema; initEvidenceIntegritySchema returns
-- early on PostgreSQL, so EVERY integrity-gated write path (submit, validate,
-- finalize, dispute, correction) failed on production PG with 42P01.
-- Create the tables in PG to the same logical shape.
CREATE TABLE IF NOT EXISTS election_policy_versions (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    version text NOT NULL,
    status text NOT NULL DEFAULT 'draft',
    legal_basis text NOT NULL,
    rules_json text NOT NULL DEFAULT '{}',
    rules_sha256 text NOT NULL,
    approved_by integer,
    approved_at timestamp,
    effective_from timestamp DEFAULT CURRENT_TIMESTAMP,
    effective_until timestamp,
    created_at timestamp DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(election_id, version)
);

CREATE TABLE IF NOT EXISTS evidence_artifacts (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    result_id integer,
    artifact_kind text NOT NULL,
    content_sha256 text NOT NULL UNIQUE,
    media_type text NOT NULL DEFAULT 'application/octet-stream',
    storage_uri text,
    original_filename text,
    byte_size integer NOT NULL DEFAULT 0,
    metadata_json text NOT NULL DEFAULT '{}',
    policy_version_id integer,
    uploaded_by integer,
    created_at timestamp DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS result_evidence_events (
    id SERIAL PRIMARY KEY,
    result_id integer NOT NULL,
    sequence_no integer NOT NULL,
    event_type text NOT NULL,
    prior_event_hash text,
    event_hash text NOT NULL UNIQUE,
    payload_sha256 text NOT NULL,
    signature text,
    signer_key_id text,
    signer_status text NOT NULL DEFAULT 'not_required',
    artifact_id integer,
    policy_version_id integer,
    visibility text NOT NULL DEFAULT 'restricted',
    public_payload text NOT NULL DEFAULT '{}',
    private_payload text NOT NULL DEFAULT '{}',
    created_by integer,
    created_at timestamp DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(result_id, sequence_no)
);
CREATE INDEX IF NOT EXISTS idx_result_evidence_events_result ON result_evidence_events(result_id);

CREATE TABLE IF NOT EXISTS reconciliation_cases (
    id SERIAL PRIMARY KEY,
    result_id integer NOT NULL,
    election_id integer NOT NULL,
    case_type text NOT NULL,
    severity text NOT NULL DEFAULT 'medium',
    status text NOT NULL DEFAULT 'open',
    blocking integer NOT NULL DEFAULT 1,
    expected_value text NOT NULL DEFAULT '{}',
    observed_value text NOT NULL DEFAULT '{}',
    reason_code text NOT NULL,
    description text NOT NULL,
    evidence_artifact_id integer,
    policy_version_id integer,
    opened_by integer,
    opened_at timestamp DEFAULT CURRENT_TIMESTAMP,
    resolution_reason text,
    resolution_evidence_artifact_id integer,
    resolved_by integer,
    resolved_at timestamp
);

CREATE TABLE IF NOT EXISTS collation_evidence_bundles (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    level text NOT NULL,
    area_code text NOT NULL,
    bundle_version integer NOT NULL DEFAULT 1,
    child_results_sha256 text NOT NULL,
    aggregate_sha256 text NOT NULL,
    event_root_sha256 text,
    policy_version_id integer,
    artifact_id integer,
    status text NOT NULL DEFAULT 'draft',
    created_by integer,
    created_at timestamp DEFAULT CURRENT_TIMESTAMP,
    published_at timestamp,
    UNIQUE(election_id, level, area_code, bundle_version)
);

CREATE TABLE IF NOT EXISTS document_integrity_assessments (
    id SERIAL PRIMARY KEY,
    result_id integer,
    report_id integer,
    artifact_id integer NOT NULL,
    assessment_status text NOT NULL,
    combined_confidence double precision,
    requires_manual_review integer NOT NULL DEFAULT 1,
    manifest_sha256 text NOT NULL,
    engine_versions text NOT NULL DEFAULT '{}',
    assessment_json text NOT NULL DEFAULT '{}',
    created_at timestamp DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(artifact_id, manifest_sha256)
);
