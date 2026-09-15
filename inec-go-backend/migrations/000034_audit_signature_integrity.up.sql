-- R5-053/R5-056: audit_log append-only at the database layer (7-year legal hold).
-- R5-054: result_signatures append-only + one signature per result (no overwrite).
-- R5-055: legal_holds registry — retention worker must never delete held tables.
-- R5-067: NDPR erasure request lifecycle (dual control: request -> review -> execute).
-- R5-055: retention_archive_log — every archive export is recorded with a checksum.

-- ── audit_log immutability ──────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION reject_audit_log_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only (7-year legal hold); UPDATE/DELETE are forbidden';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_audit_log_no_update ON audit_log;
CREATE TRIGGER trg_audit_log_no_update
    BEFORE UPDATE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION reject_audit_log_mutation();

DROP TRIGGER IF EXISTS trg_audit_log_no_delete ON audit_log;
CREATE TRIGGER trg_audit_log_no_delete
    BEFORE DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION reject_audit_log_mutation();

-- ── result_signatures: append-only, one signature per result ────────────────
CREATE OR REPLACE FUNCTION reject_result_signature_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'result_signatures is append-only; re-signing must create a new record, never overwrite';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_result_signatures_no_update ON result_signatures;
CREATE TRIGGER trg_result_signatures_no_update
    BEFORE UPDATE ON result_signatures
    FOR EACH ROW EXECUTE FUNCTION reject_result_signature_mutation();

DROP TRIGGER IF EXISTS trg_result_signatures_no_delete ON result_signatures;
CREATE TRIGGER trg_result_signatures_no_delete
    BEFORE DELETE ON result_signatures
    FOR EACH ROW EXECUTE FUNCTION reject_result_signature_mutation();

-- One signature per result (SQLite dev schema already declares result_id UNIQUE).
CREATE UNIQUE INDEX IF NOT EXISTS uq_result_signatures_result_id ON result_signatures(result_id);

-- ── legal holds registry ────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS legal_holds (
    id SERIAL PRIMARY KEY,
    table_name TEXT NOT NULL,
    reason TEXT NOT NULL,
    placed_by TEXT,
    placed_at TIMESTAMP WITHOUT TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    released_at TIMESTAMP WITHOUT TIME ZONE,
    UNIQUE(table_name, reason)
);

-- The two 7-year legal-hold tables are held by default; the retention worker
-- exports them when due but must never DELETE while a hold is active.
INSERT INTO legal_holds (table_name, reason, placed_by) VALUES
    ('audit_log', '7-year retention — Electoral Act / NDPR legal hold', 'system'),
    ('stakeholder_incidents', '7-year retention — Electoral Act / NDPR legal hold', 'system')
ON CONFLICT DO NOTHING;

-- ── retention archive registry (real export evidence) ───────────────────────
CREATE TABLE IF NOT EXISTS retention_archive_log (
    id SERIAL PRIMARY KEY,
    table_name TEXT NOT NULL,
    policy_name TEXT NOT NULL,
    cutoff_date TEXT NOT NULL,
    row_count INTEGER NOT NULL,
    file_path TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    legal_hold INTEGER NOT NULL DEFAULT 0,
    archived_at TIMESTAMP WITHOUT TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- ── NDPR erasure request lifecycle (dual control) ───────────────────────────
CREATE TABLE IF NOT EXISTS data_erasure_requests (
    id SERIAL PRIMARY KEY,
    vin TEXT NOT NULL,
    reason TEXT NOT NULL,
    requested_by INTEGER,
    requested_at TIMESTAMP WITHOUT TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','executed')),
    reviewed_by INTEGER,
    reviewed_at TIMESTAMP WITHOUT TIME ZONE,
    review_notes TEXT,
    executed_at TIMESTAMP WITHOUT TIME ZONE
);
CREATE INDEX IF NOT EXISTS idx_erasure_requests_status ON data_erasure_requests(status);
