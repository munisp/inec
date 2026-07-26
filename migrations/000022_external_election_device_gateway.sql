-- P0–P2 external election-system gateway.
-- Stores device trust and integration provenance; raw biometrics, voter identity values,
-- EC8A media, private keys, and external credentials are deliberately excluded.

CREATE TABLE IF NOT EXISTS bvas_device_enrollments (
    device_id TEXT PRIMARY KEY REFERENCES bvas_devices(id) ON DELETE RESTRICT,
    election_id INTEGER NOT NULL REFERENCES elections(id) ON DELETE RESTRICT,
    polling_unit_code TEXT NOT NULL REFERENCES polling_units(code) ON DELETE RESTRICT,
    public_key_base64 TEXT NOT NULL CHECK (length(public_key_base64) >= 40),
    certificate_fingerprint_sha256 TEXT NOT NULL CHECK (certificate_fingerprint_sha256 ~ '^[a-f0-9]{64}$'),
    firmware_allowlist JSONB NOT NULL DEFAULT '[]'::jsonb,
    attestation_policy_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','suspended','revoked','expired')),
    enrolled_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    activated_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revocation_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bvas_device_gateway_inbox (
    id BIGSERIAL PRIMARY KEY,
    device_id TEXT NOT NULL REFERENCES bvas_device_enrollments(device_id) ON DELETE RESTRICT,
    election_id INTEGER NOT NULL REFERENCES elections(id) ON DELETE RESTRICT,
    polling_unit_code TEXT NOT NULL REFERENCES polling_units(code) ON DELETE RESTRICT,
    event_type TEXT NOT NULL CHECK (event_type IN ('accreditation','result_capture','heartbeat','incident')),
    sequence_no BIGINT NOT NULL CHECK (sequence_no > 0),
    nonce_sha256 TEXT NOT NULL CHECK (nonce_sha256 ~ '^[a-f0-9]{64}$'),
    payload_sha256 TEXT NOT NULL CHECK (payload_sha256 ~ '^[a-f0-9]{64}$'),
    envelope_sha256 TEXT NOT NULL CHECK (envelope_sha256 ~ '^[a-f0-9]{64}$'),
    signature_base64 TEXT NOT NULL,
    verifier_attestation JSONB NOT NULL,
    edge_certificate_fingerprint_sha256 TEXT NOT NULL CHECK (edge_certificate_fingerprint_sha256 ~ '^[a-f0-9]{64}$'),
    observed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    correlation_id UUID NOT NULL,
    processing_status TEXT NOT NULL DEFAULT 'accepted' CHECK (processing_status IN ('accepted','rejected','quarantined','processed')),
    rejection_reason TEXT,
    UNIQUE (device_id, sequence_no),
    UNIQUE (device_id, nonce_sha256),
    UNIQUE (envelope_sha256)
);

CREATE INDEX IF NOT EXISTS idx_bvas_gateway_inbox_election_pu ON bvas_device_gateway_inbox(election_id, polling_unit_code, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_bvas_gateway_inbox_status ON bvas_device_gateway_inbox(processing_status, received_at);

CREATE TABLE IF NOT EXISTS external_integration_outbox (
    id BIGSERIAL PRIMARY KEY,
    correlation_id UUID NOT NULL,
    source_type TEXT NOT NULL CHECK (source_type IN ('bvas_gateway','irev_adapter','spatial_control','settlement')),
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    event_version TEXT NOT NULL DEFAULT 'v1',
    partition_key TEXT NOT NULL,
    payload_redacted JSONB NOT NULL,
    payload_sha256 TEXT NOT NULL CHECK (payload_sha256 ~ '^[a-f0-9]{64}$'),
    required_sinks JSONB NOT NULL DEFAULT '[]'::jsonb,
    delivery_status TEXT NOT NULL DEFAULT 'pending' CHECK (delivery_status IN ('pending','delivering','delivered','failed','quarantined')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at TIMESTAMPTZ,
    UNIQUE (event_type, aggregate_type, aggregate_id, payload_sha256)
);

CREATE INDEX IF NOT EXISTS idx_external_outbox_delivery ON external_integration_outbox(delivery_status, next_attempt_at, id);
CREATE INDEX IF NOT EXISTS idx_external_outbox_correlation ON external_integration_outbox(correlation_id);

CREATE TABLE IF NOT EXISTS external_portal_integrations (
    portal_code TEXT PRIMARY KEY CHECK (portal_code IN ('irev')),
    integration_status TEXT NOT NULL DEFAULT 'unconfigured' CHECK (integration_status IN ('unconfigured','onboarding','sandbox','authorized','suspended','revoked')),
    endpoint_origin TEXT,
    api_version TEXT,
    authentication_mode TEXT,
    callback_verification_policy TEXT,
    configuration_hash TEXT,
    authorization_reference TEXT,
    approved_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ,
    last_verified_at TIMESTAMPTZ,
    failure_reason TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO external_portal_integrations (portal_code) VALUES ('irev') ON CONFLICT (portal_code) DO NOTHING;

CREATE TABLE IF NOT EXISTS external_portal_submission_attempts (
    id BIGSERIAL PRIMARY KEY,
    portal_code TEXT NOT NULL REFERENCES external_portal_integrations(portal_code) ON DELETE RESTRICT,
    result_id INTEGER REFERENCES results(id) ON DELETE RESTRICT,
    artifact_id BIGINT REFERENCES evidence_artifacts(id) ON DELETE RESTRICT,
    correlation_id UUID NOT NULL,
    request_hash TEXT NOT NULL CHECK (request_hash ~ '^[a-f0-9]{64}$'),
    request_signature TEXT NOT NULL,
    external_reference TEXT,
    receipt_hash TEXT,
    receipt_payload_redacted JSONB,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','accepted','rejected','unavailable','failed','quarantined')),
    response_code INTEGER,
    failure_reason TEXT,
    submitted_at TIMESTAMPTZ,
    received_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (portal_code, request_hash)
);

CREATE INDEX IF NOT EXISTS idx_portal_attempt_state ON external_portal_submission_attempts(portal_code, state, created_at DESC);

-- Immutable ingress evidence: content fields cannot be changed after receipt.
CREATE OR REPLACE FUNCTION reject_bvas_gateway_inbox_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'bvas device gateway inbox is immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_bvas_gateway_inbox_immutable ON bvas_device_gateway_inbox;
CREATE TRIGGER trg_bvas_gateway_inbox_immutable
BEFORE UPDATE OR DELETE ON bvas_device_gateway_inbox
FOR EACH ROW EXECUTE FUNCTION reject_bvas_gateway_inbox_mutation();

CREATE OR REPLACE FUNCTION reject_portal_receipt_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.state IN ('accepted','rejected','quarantined') THEN
        RAISE EXCEPTION 'terminal external portal receipt cannot be modified';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_portal_receipt_terminal ON external_portal_submission_attempts;
CREATE TRIGGER trg_portal_receipt_terminal
BEFORE UPDATE OR DELETE ON external_portal_submission_attempts
FOR EACH ROW EXECUTE FUNCTION reject_portal_receipt_mutation();
