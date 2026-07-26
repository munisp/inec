-- 000019: Evidence-based result lifecycle, reconciliation, and policy provenance.
-- Every material result transition is represented by an immutable event, a content
-- addressed artifact reference, and an election policy version.

CREATE TABLE IF NOT EXISTS election_policy_versions (
    id BIGSERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'approved', 'superseded', 'revoked')),
    legal_basis TEXT NOT NULL,
    rules_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    rules_sha256 CHAR(64) NOT NULL,
    approved_by INTEGER REFERENCES users(id),
    approved_at TIMESTAMPTZ,
    effective_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    effective_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (election_id, version)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_election_policy_active
    ON election_policy_versions (election_id)
    WHERE status = 'approved' AND effective_until IS NULL;

CREATE TABLE IF NOT EXISTS evidence_artifacts (
    id BIGSERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    result_id INTEGER REFERENCES results(id) ON DELETE SET NULL,
    artifact_kind TEXT NOT NULL CHECK (artifact_kind IN (
        'ec8a_image', 'ec8b_image', 'ec8c_image', 'analysis_manifest',
        'collation_bundle', 'dispute_evidence', 'material_manifest', 'other'
    )),
    content_sha256 CHAR(64) NOT NULL,
    media_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    storage_uri TEXT,
    original_filename TEXT,
    byte_size BIGINT NOT NULL DEFAULT 0 CHECK (byte_size >= 0),
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    policy_version_id BIGINT REFERENCES election_policy_versions(id) ON DELETE SET NULL,
    uploaded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (content_sha256)
);
CREATE INDEX IF NOT EXISTS idx_evidence_artifacts_result ON evidence_artifacts(result_id, created_at);
CREATE INDEX IF NOT EXISTS idx_evidence_artifacts_election ON evidence_artifacts(election_id, artifact_kind, created_at);

CREATE TABLE IF NOT EXISTS result_evidence_events (
    id BIGSERIAL PRIMARY KEY,
    result_id INTEGER NOT NULL REFERENCES results(id) ON DELETE CASCADE,
    sequence_no INTEGER NOT NULL CHECK (sequence_no > 0),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'RESULT_SUBMITTED', 'ANALYSIS_ATTACHED', 'QUALITY_EXCEPTION',
        'RECONCILIATION_CASE_OPENED', 'RECONCILIATION_CASE_RESOLVED',
        'RESULT_VALIDATED', 'RESULT_FINALIZED', 'RESULT_DISPUTED',
        'COLLATION_BUNDLED', 'PUBLISHED'
    )),
    prior_event_hash CHAR(64),
    event_hash CHAR(64) NOT NULL,
    payload_sha256 CHAR(64) NOT NULL,
    signature TEXT,
    signer_key_id TEXT,
    signer_status TEXT NOT NULL DEFAULT 'not_required'
        CHECK (signer_status IN ('signed', 'not_required', 'unavailable')),
    artifact_id BIGINT REFERENCES evidence_artifacts(id) ON DELETE SET NULL,
    policy_version_id BIGINT REFERENCES election_policy_versions(id) ON DELETE SET NULL,
    visibility TEXT NOT NULL DEFAULT 'restricted'
        CHECK (visibility IN ('public', 'observer', 'restricted')),
    public_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    private_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (result_id, sequence_no),
    UNIQUE (event_hash)
);
CREATE INDEX IF NOT EXISTS idx_result_evidence_events_result ON result_evidence_events(result_id, sequence_no);
CREATE INDEX IF NOT EXISTS idx_result_evidence_events_policy ON result_evidence_events(policy_version_id, created_at);

CREATE TABLE IF NOT EXISTS reconciliation_cases (
    id BIGSERIAL PRIMARY KEY,
    result_id INTEGER NOT NULL REFERENCES results(id) ON DELETE CASCADE,
    election_id INTEGER NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    case_type TEXT NOT NULL CHECK (case_type IN (
        'artifact_quality', 'ocr_arithmetic', 'structured_entry_mismatch',
        'accreditation_mismatch', 'material_manifest_mismatch',
        'late_submission', 'tampering_indicator', 'other'
    )),
    severity TEXT NOT NULL DEFAULT 'medium'
        CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    status TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'under_review', 'resolved', 'dismissed')),
    blocking BOOLEAN NOT NULL DEFAULT true,
    expected_value JSONB NOT NULL DEFAULT '{}'::jsonb,
    observed_value JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason_code TEXT NOT NULL,
    description TEXT NOT NULL,
    evidence_artifact_id BIGINT REFERENCES evidence_artifacts(id) ON DELETE SET NULL,
    policy_version_id BIGINT REFERENCES election_policy_versions(id) ON DELETE SET NULL,
    opened_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    opened_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolution_reason TEXT,
    resolution_evidence_artifact_id BIGINT REFERENCES evidence_artifacts(id) ON DELETE SET NULL,
    resolved_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    resolved_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_reconciliation_cases_result ON reconciliation_cases(result_id, status, severity);
CREATE INDEX IF NOT EXISTS idx_reconciliation_cases_election ON reconciliation_cases(election_id, status, blocking);

CREATE TABLE IF NOT EXISTS collation_evidence_bundles (
    id BIGSERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    level TEXT NOT NULL CHECK (level IN ('ward', 'lga', 'state', 'national')),
    area_code TEXT NOT NULL,
    bundle_version INTEGER NOT NULL DEFAULT 1 CHECK (bundle_version > 0),
    child_results_sha256 CHAR(64) NOT NULL,
    aggregate_sha256 CHAR(64) NOT NULL,
    event_root_sha256 CHAR(64),
    policy_version_id BIGINT REFERENCES election_policy_versions(id) ON DELETE SET NULL,
    artifact_id BIGINT REFERENCES evidence_artifacts(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'published', 'superseded', 'blocked')),
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    UNIQUE (election_id, level, area_code, bundle_version)
);
CREATE INDEX IF NOT EXISTS idx_collation_evidence_bundle_lookup
    ON collation_evidence_bundles(election_id, level, area_code, status, bundle_version DESC);

CREATE TABLE IF NOT EXISTS document_integrity_assessments (
    id BIGSERIAL PRIMARY KEY,
    result_id INTEGER REFERENCES results(id) ON DELETE SET NULL,
    report_id INTEGER,
    artifact_id BIGINT NOT NULL REFERENCES evidence_artifacts(id) ON DELETE CASCADE,
    assessment_status TEXT NOT NULL CHECK (assessment_status IN (
        'analysis_complete', 'manual_review_required', 'rejected_for_quality', 'analysis_unavailable'
    )),
    combined_confidence DOUBLE PRECISION,
    requires_manual_review BOOLEAN NOT NULL DEFAULT true,
    manifest_sha256 CHAR(64) NOT NULL,
    engine_versions JSONB NOT NULL DEFAULT '{}'::jsonb,
    assessment_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (artifact_id, manifest_sha256)
);
CREATE INDEX IF NOT EXISTS idx_document_integrity_assessments_result
    ON document_integrity_assessments(result_id, assessment_status, created_at);

CREATE TABLE IF NOT EXISTS election_material_manifests (
    id BIGSERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    policy_version_id BIGINT NOT NULL REFERENCES election_policy_versions(id) ON DELETE RESTRICT,
    material_type TEXT NOT NULL CHECK (material_type IN (
        'candidate_list', 'party_list', 'ballot_template', 'ec8a_template', 'ec8b_template', 'ec8c_template'
    )),
    version TEXT NOT NULL,
    manifest_sha256 CHAR(64) NOT NULL,
    artifact_id BIGINT REFERENCES evidence_artifacts(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'approved', 'superseded', 'revoked')),
    effective_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    effective_until TIMESTAMPTZ,
    approved_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (election_id, material_type, version)
);
CREATE INDEX IF NOT EXISTS idx_material_manifest_active
    ON election_material_manifests(election_id, material_type, status, effective_from DESC);

-- Prevent a completed result from losing its auditable policy reference through a
-- missing current policy. Application logic requires an approved policy before validation/finalisation.
COMMENT ON TABLE result_evidence_events IS 'Append-only, hash-chained lifecycle events for election results.';
COMMENT ON TABLE reconciliation_cases IS 'Accountable exception workflow; blocking cases prevent finalisation.';
