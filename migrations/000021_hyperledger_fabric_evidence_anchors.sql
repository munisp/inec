-- 000021: Hyperledger Fabric hash-anchor outbox and consortium receipt records.
-- Only signed SHA-256 commitments and Fabric receipt metadata are persisted here.
-- Raw EC8A media, voter data, and private reconciliation payloads remain off-chain.

CREATE TABLE IF NOT EXISTS fabric_anchor_requests (
    id BIGSERIAL PRIMARY KEY,
    result_evidence_event_id BIGINT REFERENCES result_evidence_events(id) ON DELETE RESTRICT,
    collation_evidence_bundle_id BIGINT REFERENCES collation_evidence_bundles(id) ON DELETE RESTRICT,
    anchor_type TEXT NOT NULL CHECK (anchor_type IN ('result_event', 'collation_bundle')),
    anchor_id CHAR(64) NOT NULL UNIQUE,
    anchor_payload JSONB NOT NULL,
    anchor_payload_sha256 CHAR(64) NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending', 'submitted', 'committed', 'failed', 'unavailable'
    )),
    fabric_channel TEXT,
    chaincode_id TEXT,
    contract_name TEXT,
    transaction_id TEXT,
    commit_status TEXT,
    receipt_json JSONB,
    receipt_sha256 CHAR(64),
    last_error TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_attempt_at TIMESTAMPTZ,
    committed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fabric_anchor_exactly_one_source CHECK (
        (result_evidence_event_id IS NOT NULL)::integer +
        (collation_evidence_bundle_id IS NOT NULL)::integer = 1
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_fabric_anchor_event_source
    ON fabric_anchor_requests(result_evidence_event_id)
    WHERE result_evidence_event_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_fabric_anchor_bundle_source
    ON fabric_anchor_requests(collation_evidence_bundle_id)
    WHERE collation_evidence_bundle_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_fabric_anchor_requests_status
    ON fabric_anchor_requests(status, created_at);
CREATE INDEX IF NOT EXISTS idx_fabric_anchor_requests_transaction
    ON fabric_anchor_requests(transaction_id)
    WHERE transaction_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS fabric_anchor_attempts (
    id BIGSERIAL PRIMARY KEY,
    fabric_anchor_request_id BIGINT NOT NULL REFERENCES fabric_anchor_requests(id) ON DELETE RESTRICT,
    attempt_no INTEGER NOT NULL CHECK (attempt_no > 0),
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    gateway_endpoint TEXT,
    channel_id TEXT,
    chaincode_id TEXT,
    transaction_id TEXT,
    commit_status TEXT NOT NULL CHECK (commit_status IN ('submitted', 'committed', 'failed', 'unavailable')),
    receipt_json JSONB,
    diagnostic TEXT,
    UNIQUE (fabric_anchor_request_id, attempt_no)
);
CREATE INDEX IF NOT EXISTS idx_fabric_anchor_attempts_request
    ON fabric_anchor_attempts(fabric_anchor_request_id, attempt_no);

CREATE OR REPLACE FUNCTION enforce_fabric_anchor_request_transition()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.result_evidence_event_id IS DISTINCT FROM OLD.result_evidence_event_id
       OR NEW.collation_evidence_bundle_id IS DISTINCT FROM OLD.collation_evidence_bundle_id
       OR NEW.anchor_type IS DISTINCT FROM OLD.anchor_type
       OR NEW.anchor_id IS DISTINCT FROM OLD.anchor_id
       OR NEW.anchor_payload IS DISTINCT FROM OLD.anchor_payload
       OR NEW.anchor_payload_sha256 IS DISTINCT FROM OLD.anchor_payload_sha256
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'Fabric anchor identity and canonical payload are immutable';
    END IF;
    IF OLD.status = 'committed' THEN
        RAISE EXCEPTION 'committed Fabric anchors are immutable';
    END IF;
    IF NEW.attempt_count < OLD.attempt_count THEN
        RAISE EXCEPTION 'Fabric anchor attempt count cannot decrease';
    END IF;
    IF NEW.status NOT IN ('pending', 'submitted', 'committed', 'failed', 'unavailable') THEN
        RAISE EXCEPTION 'invalid Fabric anchor status';
    END IF;
    IF NEW.status = 'committed' AND (NEW.transaction_id IS NULL OR NEW.receipt_sha256 IS NULL OR NEW.committed_at IS NULL) THEN
        RAISE EXCEPTION 'committed Fabric anchors require transaction, receipt hash, and commit timestamp';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_fabric_anchor_request_transition ON fabric_anchor_requests;
CREATE TRIGGER trg_fabric_anchor_request_transition
    BEFORE UPDATE ON fabric_anchor_requests
    FOR EACH ROW EXECUTE FUNCTION enforce_fabric_anchor_request_transition();

CREATE OR REPLACE FUNCTION prevent_fabric_anchor_attempt_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Fabric anchor attempts are append-only';
END;
$$;

DROP TRIGGER IF EXISTS trg_fabric_anchor_attempt_no_update ON fabric_anchor_attempts;
CREATE TRIGGER trg_fabric_anchor_attempt_no_update
    BEFORE UPDATE OR DELETE ON fabric_anchor_attempts
    FOR EACH ROW EXECUTE FUNCTION prevent_fabric_anchor_attempt_mutation();

COMMENT ON TABLE fabric_anchor_requests IS 'Outbox and confirmed Hyperledger Fabric receipts for signed, off-chain evidence commitments.';
COMMENT ON TABLE fabric_anchor_attempts IS 'Append-only diagnostics for real Fabric Gateway submission attempts.';
