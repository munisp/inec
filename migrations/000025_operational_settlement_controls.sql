-- P2 operational settlement controls for BVAS logistics and reimbursement commitments.
-- This schema deliberately has no voter, PVC, result, ballot, biometric, or portal-result fields.
-- Amounts are integer minor units and remain inside PostgreSQL/TigerBeetle/Mojaloop controls;
-- only redacted commitment state may enter external integration outboxes.

CREATE TABLE IF NOT EXISTS operational_settlement_commitments (
    id UUID PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id) ON DELETE RESTRICT,
    device_id TEXT NOT NULL REFERENCES bvas_device_enrollments(device_id) ON DELETE RESTRICT,
    commitment_kind TEXT NOT NULL CHECK (commitment_kind IN ('device_logistics','device_reimbursement','device_repair')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    recipient_reference_sha256 CHAR(64) NOT NULL CHECK (recipient_reference_sha256 ~ '^[a-f0-9]{64}$'),
    evidence_sha256 CHAR(64) NOT NULL CHECK (evidence_sha256 ~ '^[a-f0-9]{64}$'),
    purpose_reference_sha256 CHAR(64) NOT NULL CHECK (purpose_reference_sha256 ~ '^[a-f0-9]{64}$'),
    idempotency_key UUID NOT NULL UNIQUE,
    requested_by TEXT NOT NULL,
    approved_by TEXT,
    approved_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'requested' CHECK (status IN ('requested','approved','ledger_pending','ledger_committed','mojaloop_unavailable','mojaloop_pending','settled','voided','failed')),
    tigerbeetle_debit_account_id TEXT NOT NULL,
    tigerbeetle_credit_account_id TEXT NOT NULL,
    tigerbeetle_transfer_id TEXT UNIQUE,
    mojaloop_quote_id TEXT,
    mojaloop_transfer_id TEXT,
    external_receipt_sha256 CHAR(64) CHECK (external_receipt_sha256 IS NULL OR external_receipt_sha256 ~ '^[a-f0-9]{64}$'),
    failure_code TEXT,
    failure_detail_redacted TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (approved_by IS NULL OR approved_by <> requested_by),
    CHECK ((approved_by IS NULL AND approved_at IS NULL) OR (approved_by IS NOT NULL AND approved_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_operational_settlement_commitments_device
    ON operational_settlement_commitments(device_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_operational_settlement_commitments_status
    ON operational_settlement_commitments(status, updated_at);

CREATE TABLE IF NOT EXISTS operational_settlement_audit (
    id BIGSERIAL PRIMARY KEY,
    commitment_id UUID NOT NULL REFERENCES operational_settlement_commitments(id) ON DELETE RESTRICT,
    action TEXT NOT NULL CHECK (action IN ('requested','approved','ledger_pending','ledger_committed','mojaloop_unavailable','mojaloop_pending','settled','voided','failed')),
    actor TEXT NOT NULL,
    evidence_sha256 CHAR(64) NOT NULL CHECK (evidence_sha256 ~ '^[a-f0-9]{64}$'),
    details_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_operational_settlement_audit_commitment
    ON operational_settlement_audit(commitment_id, id);

CREATE OR REPLACE FUNCTION guard_operational_settlement_commitment()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'operational settlement commitments may not be deleted';
    END IF;

    IF OLD.election_id <> NEW.election_id
       OR OLD.device_id <> NEW.device_id
       OR OLD.commitment_kind <> NEW.commitment_kind
       OR OLD.amount_minor <> NEW.amount_minor
       OR OLD.currency <> NEW.currency
       OR OLD.recipient_reference_sha256 <> NEW.recipient_reference_sha256
       OR OLD.evidence_sha256 <> NEW.evidence_sha256
       OR OLD.purpose_reference_sha256 <> NEW.purpose_reference_sha256
       OR OLD.idempotency_key <> NEW.idempotency_key
       OR OLD.requested_by <> NEW.requested_by
       OR OLD.tigerbeetle_debit_account_id <> NEW.tigerbeetle_debit_account_id
       OR OLD.tigerbeetle_credit_account_id <> NEW.tigerbeetle_credit_account_id THEN
        RAISE EXCEPTION 'operational settlement identity and evidence binding are immutable';
    END IF;

    IF OLD.approved_by IS NOT NULL
       AND (OLD.approved_by IS DISTINCT FROM NEW.approved_by OR OLD.approved_at IS DISTINCT FROM NEW.approved_at) THEN
        RAISE EXCEPTION 'operational settlement approval is immutable once recorded';
    END IF;

    IF NEW.tigerbeetle_transfer_id IS NOT NULL AND NEW.approved_by IS NULL THEN
        RAISE EXCEPTION 'TigerBeetle transfer may be recorded only after independent approval';
    END IF;

    IF OLD.status IN ('settled','voided') AND OLD.status <> NEW.status THEN
        RAISE EXCEPTION 'terminal operational settlement status may not change';
    END IF;

    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_operational_settlement_commitment_guard ON operational_settlement_commitments;
CREATE TRIGGER trg_operational_settlement_commitment_guard
BEFORE UPDATE OR DELETE ON operational_settlement_commitments
FOR EACH ROW EXECUTE FUNCTION guard_operational_settlement_commitment();

CREATE OR REPLACE FUNCTION reject_operational_settlement_audit_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'operational settlement audit entries are append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_operational_settlement_audit_immutable ON operational_settlement_audit;
CREATE TRIGGER trg_operational_settlement_audit_immutable
BEFORE UPDATE OR DELETE ON operational_settlement_audit
FOR EACH ROW EXECUTE FUNCTION reject_operational_settlement_audit_mutation();
