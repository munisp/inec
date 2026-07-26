-- P1 authorized IReV integration. Credentials and certificates remain external secret references.

CREATE TABLE IF NOT EXISTS authorized_portal_connections (
    id BIGSERIAL PRIMARY KEY,
    portal_code TEXT NOT NULL UNIQUE CHECK (portal_code IN ('irev')),
    base_url TEXT NOT NULL,
    submit_path TEXT NOT NULL,
    token_url TEXT,
    client_id_reference TEXT,
    client_secret_reference TEXT,
    client_cert_reference TEXT,
    client_key_reference TEXT,
    ca_cert_reference TEXT,
    audience TEXT,
    scope TEXT,
    status TEXT NOT NULL DEFAULT 'unconfigured' CHECK (status IN ('unconfigured','configured','active','suspended','revoked')),
    approved_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ,
    last_health_at TIMESTAMPTZ,
    last_health_status TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS irev_submission_receipts (
    id BIGSERIAL PRIMARY KEY,
    result_id INTEGER NOT NULL REFERENCES results(id) ON DELETE RESTRICT,
    portal_connection_id BIGINT REFERENCES authorized_portal_connections(id) ON DELETE SET NULL,
    idempotency_key UUID NOT NULL UNIQUE,
    evidence_event_hash CHAR(64) NOT NULL,
    payload_sha256 CHAR(64) NOT NULL,
    submission_status TEXT NOT NULL CHECK (submission_status IN ('pending','submitted','acknowledged','rejected','unavailable','failed','reconciliation_required')),
    external_receipt_id TEXT,
    external_transaction_id TEXT,
    external_status TEXT,
    external_response_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
    submitted_at TIMESTAMPTZ,
    acknowledged_at TIMESTAMPTZ,
    last_error_code TEXT,
    last_error_detail TEXT,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (result_id, evidence_event_hash)
);
CREATE INDEX IF NOT EXISTS idx_irev_receipts_result ON irev_submission_receipts(result_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_irev_receipts_status ON irev_submission_receipts(submission_status, updated_at);

CREATE TABLE IF NOT EXISTS irev_webhook_receipts (
    id BIGSERIAL PRIMARY KEY,
    portal_connection_id BIGINT REFERENCES authorized_portal_connections(id) ON DELETE SET NULL,
    external_receipt_id TEXT NOT NULL,
    delivery_id TEXT,
    payload_sha256 CHAR(64) NOT NULL,
    signature_valid BOOLEAN NOT NULL,
    payload_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
    received_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (portal_connection_id, external_receipt_id, payload_sha256)
);

CREATE OR REPLACE FUNCTION reject_irev_receipt_immutable_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'IReV submission receipts are append-only and may not be deleted';
    END IF;
    IF OLD.result_id <> NEW.result_id
       OR OLD.idempotency_key <> NEW.idempotency_key
       OR OLD.evidence_event_hash <> NEW.evidence_event_hash
       OR OLD.payload_sha256 <> NEW.payload_sha256
       OR OLD.external_receipt_id IS DISTINCT FROM NEW.external_receipt_id
       OR OLD.external_transaction_id IS DISTINCT FROM NEW.external_transaction_id THEN
        RAISE EXCEPTION 'IReV receipt identity and evidence binding are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_irev_receipts_immutable ON irev_submission_receipts;
CREATE TRIGGER trg_irev_receipts_immutable
BEFORE UPDATE OR DELETE ON irev_submission_receipts
FOR EACH ROW EXECUTE FUNCTION reject_irev_receipt_immutable_mutation();
