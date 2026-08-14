-- Vault schema actually queried by src/vault.rs and src/cancelable.rs.
-- 001_biometric_tables.sql created `encrypted_templates`/`template_audit_log`,
-- which no code path reads — this migration defines the real tables.
-- Applied with sqlx::raw_sql (simple query protocol) so multi-statement files work.

CREATE TABLE IF NOT EXISTS vault_keys (
    key_id         TEXT PRIMARY KEY,
    purpose        TEXT NOT NULL,
    encrypted_key  BYTEA NOT NULL,
    key_version    INTEGER NOT NULL DEFAULT 1,
    is_active      BOOLEAN NOT NULL DEFAULT TRUE,
    is_revoked     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at     TIMESTAMPTZ,
    revoked_at     TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS vault_templates (
    template_id    TEXT PRIMARY KEY,
    voter_vin      TEXT NOT NULL,
    modality       TEXT NOT NULL,
    key_id         TEXT NOT NULL REFERENCES vault_keys(key_id),
    ciphertext     BYTEA NOT NULL,
    nonce          BYTEA NOT NULL,
    integrity_hash TEXT NOT NULL,
    version        INTEGER NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_vault_templates_key ON vault_templates(key_id);
CREATE INDEX IF NOT EXISTS idx_vault_templates_vin ON vault_templates(voter_vin);

CREATE TABLE IF NOT EXISTS vault_audit_log (
    id           TEXT PRIMARY KEY,
    operation    TEXT NOT NULL,
    key_id       TEXT,
    voter_vin    TEXT,
    modality     TEXT,
    actor        TEXT NOT NULL,
    success      BOOLEAN NOT NULL,
    error_detail TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_vault_audit_created ON vault_audit_log(created_at DESC);

CREATE TABLE IF NOT EXISTS cancelable_transforms (
    transform_id   TEXT PRIMARY KEY,
    voter_vin      TEXT NOT NULL,
    modality       TEXT NOT NULL,
    transform_type TEXT NOT NULL,
    version        INTEGER NOT NULL DEFAULT 1,
    is_revoked     BOOLEAN NOT NULL DEFAULT FALSE,
    seed           BYTEA NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at     TIMESTAMPTZ
);
