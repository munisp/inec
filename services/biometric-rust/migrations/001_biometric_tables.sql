CREATE TABLE IF NOT EXISTS encrypted_templates (
    vin TEXT PRIMARY KEY,
    encrypted_payload BYTEA NOT NULL,
    nonce BYTEA NOT NULL,
    key_version INTEGER NOT NULL,
    modality TEXT NOT NULL,
    quality_score DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS template_audit_log (
    id SERIAL PRIMARY KEY,
    vin TEXT NOT NULL,
    action TEXT NOT NULL,
    performed_by TEXT NOT NULL,
    ip_address TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_template_audit_vin ON template_audit_log(vin);
