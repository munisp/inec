-- Biometric vault service schema (embedded, applied at startup by src/db.rs).
-- R4-39c fix: previously this file created only encrypted_templates and
-- template_audit_log (neither of which the code queries) while vault.rs and
-- cancelable.rs query vault_keys / vault_templates / vault_audit_log /
-- cancelable_transforms — so a fresh database broke on the first vault call.
-- Every statement is idempotent (IF NOT EXISTS) so startup re-application is safe.

-- ─── Vault keys ─────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS vault_keys (
    key_id          TEXT PRIMARY KEY,
    purpose         TEXT NOT NULL CHECK (purpose IN ('template_encryption', 'integrity_hmac', 'key_wrapping')),
    encrypted_key   BYTEA NOT NULL,
    key_version     INTEGER NOT NULL DEFAULT 1,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    is_revoked      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_vault_keys_active ON vault_keys (is_active, is_revoked) WHERE is_active = TRUE AND is_revoked = FALSE;

-- ─── Encrypted biometric templates ──────────────────────────────
CREATE TABLE IF NOT EXISTS vault_templates (
    template_id     TEXT PRIMARY KEY,
    voter_vin       TEXT NOT NULL,
    modality        TEXT NOT NULL CHECK (modality IN ('fingerprint', 'face', 'iris')),
    key_id          TEXT NOT NULL REFERENCES vault_keys(key_id),
    ciphertext      BYTEA NOT NULL,
    nonce           BYTEA NOT NULL,
    integrity_hash  TEXT NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_vault_templates_voter ON vault_templates (voter_vin, modality);
CREATE INDEX IF NOT EXISTS idx_vault_templates_key ON vault_templates (key_id);

-- ─── Vault audit log ────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS vault_audit_log (
    id              TEXT PRIMARY KEY,
    operation       TEXT NOT NULL,
    key_id          TEXT,
    voter_vin       TEXT,
    modality        TEXT,
    actor           TEXT NOT NULL,
    success         BOOLEAN NOT NULL,
    error_detail    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_vault_audit_time ON vault_audit_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_vault_audit_voter ON vault_audit_log (voter_vin) WHERE voter_vin IS NOT NULL;

-- ─── Cancelable biometric transforms ────────────────────────────
CREATE TABLE IF NOT EXISTS cancelable_transforms (
    transform_id    TEXT PRIMARY KEY,
    voter_vin       TEXT NOT NULL,
    modality        TEXT NOT NULL CHECK (modality IN ('fingerprint', 'face', 'iris')),
    transform_type  TEXT NOT NULL CHECK (transform_type IN ('BioHashing', 'RandomProjection', 'BloomFilter')),
    version         INTEGER NOT NULL DEFAULT 1,
    is_revoked      BOOLEAN NOT NULL DEFAULT FALSE,
    seed            BYTEA NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_cancelable_voter ON cancelable_transforms (voter_vin, modality);
CREATE INDEX IF NOT EXISTS idx_cancelable_active ON cancelable_transforms (is_revoked) WHERE is_revoked = FALSE;

-- ─── Legacy tables (retained; referenced by older tooling) ──────
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
CREATE INDEX IF NOT EXISTS idx_template_audit_vin ON template_audit_log(vin);
