-- Biometric services PostgreSQL schema
-- All in-memory state persisted to durable storage

-- ─── Rust Vault Service Tables ──────────────────────────────────

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

CREATE INDEX idx_vault_keys_active ON vault_keys (is_active, is_revoked) WHERE is_active = TRUE AND is_revoked = FALSE;

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

CREATE INDEX idx_vault_templates_voter ON vault_templates (voter_vin, modality);
CREATE INDEX idx_vault_templates_key ON vault_templates (key_id);

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

CREATE INDEX idx_vault_audit_time ON vault_audit_log (created_at DESC);
CREATE INDEX idx_vault_audit_voter ON vault_audit_log (voter_vin) WHERE voter_vin IS NOT NULL;

-- ─── Rust Cancelable Biometrics Tables ──────────────────────────

CREATE TABLE IF NOT EXISTS cancelable_transforms (
    transform_id    TEXT PRIMARY KEY,
    voter_vin       TEXT NOT NULL,
    modality        TEXT NOT NULL,
    transform_type  TEXT NOT NULL CHECK (transform_type IN ('BioHashing', 'RandomProjection', 'BloomFilter')),
    version         INTEGER NOT NULL DEFAULT 1,
    is_revoked      BOOLEAN NOT NULL DEFAULT FALSE,
    seed            BYTEA NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX idx_cancelable_voter ON cancelable_transforms (voter_vin, modality);
CREATE INDEX idx_cancelable_active ON cancelable_transforms (is_revoked) WHERE is_revoked = FALSE;

-- ─── Go BVAS Device Registry Tables ────────────────────────────

CREATE TABLE IF NOT EXISTS bvas_devices (
    device_id           TEXT PRIMARY KEY,
    firmware_version    TEXT NOT NULL,
    fingerprint_fap     TEXT NOT NULL DEFAULT 'FAP30',
    supported_modalities TEXT[] NOT NULL DEFAULT '{}',
    has_nfc             BOOLEAN NOT NULL DEFAULT FALSE,
    has_secure_element  BOOLEAN NOT NULL DEFAULT FALSE,
    tls_version         TEXT NOT NULL DEFAULT 'TLS1.3',
    quality_threshold   DOUBLE PRECISION NOT NULL DEFAULT 0.7,
    max_template_size   INTEGER NOT NULL DEFAULT 102400,
    status              TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'maintenance', 'revoked')),
    last_heartbeat      TIMESTAMPTZ,
    location_lat        DOUBLE PRECISION,
    location_lng        DOUBLE PRECISION,
    location_accuracy   DOUBLE PRECISION,
    assigned_pu         TEXT,
    registered_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bvas_status ON bvas_devices (status);
CREATE INDEX idx_bvas_assigned_pu ON bvas_devices (assigned_pu) WHERE assigned_pu IS NOT NULL;

CREATE TABLE IF NOT EXISTS bvas_capture_sessions (
    session_id      TEXT PRIMARY KEY,
    device_id       TEXT NOT NULL REFERENCES bvas_devices(device_id),
    voter_vin       TEXT NOT NULL,
    modality        TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'capturing' CHECK (status IN ('capturing', 'completed', 'failed', 'timeout')),
    quality_score   DOUBLE PRECISION,
    nfiq2_score     INTEGER,
    image_width     INTEGER,
    image_height    INTEGER,
    error_code      TEXT,
    processing_ms   BIGINT,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX idx_capture_device ON bvas_capture_sessions (device_id);
CREATE INDEX idx_capture_voter ON bvas_capture_sessions (voter_vin);
CREATE INDEX idx_capture_status ON bvas_capture_sessions (status);

-- ─── Go Biometric Gallery Tables ───────────────────────────────

CREATE TABLE IF NOT EXISTS biometric_gallery (
    id              BIGSERIAL PRIMARY KEY,
    voter_vin       TEXT NOT NULL,
    modality        TEXT NOT NULL CHECK (modality IN ('fingerprint', 'face', 'iris')),
    template_hash   TEXT NOT NULL,
    quality_score   DOUBLE PRECISION NOT NULL,
    minutiae_json   JSONB,
    embedding       DOUBLE PRECISION[],
    iris_code       BYTEA,
    iris_mask       BYTEA,
    enrolled_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (voter_vin, modality)
);

CREATE INDEX idx_gallery_voter ON biometric_gallery (voter_vin);
CREATE INDEX idx_gallery_hash ON biometric_gallery (template_hash);
CREATE INDEX idx_gallery_modality ON biometric_gallery (modality);

-- ─── Go LSH Index Tables ───────────────────────────────────────

CREATE TABLE IF NOT EXISTS lsh_index (
    id              BIGSERIAL PRIMARY KEY,
    table_num       INTEGER NOT NULL,
    hash_value      BIGINT NOT NULL,
    voter_vin       TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_lsh_lookup ON lsh_index (table_num, hash_value);
CREATE INDEX idx_lsh_voter ON lsh_index (voter_vin);

-- ─── Go Audit Log ──────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS abis_audit_log (
    id              TEXT PRIMARY KEY,
    operation       TEXT NOT NULL,
    voter_vin       TEXT NOT NULL,
    modality        TEXT NOT NULL,
    detail          TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_abis_audit_time ON abis_audit_log (created_at DESC);
CREATE INDEX idx_abis_audit_voter ON abis_audit_log (voter_vin);

-- ─── Python Biometric Processing Audit ─────────────────────────

CREATE TABLE IF NOT EXISTS biometric_processing_log (
    id              BIGSERIAL PRIMARY KEY,
    request_id      TEXT NOT NULL,
    operation       TEXT NOT NULL CHECK (operation IN ('extract_fingerprint', 'extract_face', 'extract_iris', 'match_fingerprint', 'match_face', 'match_iris', 'quality_assess', 'pad_check', 'multimodal_match')),
    voter_vin       TEXT,
    modality        TEXT,
    quality_score   DOUBLE PRECISION,
    match_score     DOUBLE PRECISION,
    match_decision  TEXT,
    latency_ms      DOUBLE PRECISION NOT NULL,
    success         BOOLEAN NOT NULL DEFAULT TRUE,
    error_detail    TEXT,
    metadata_json   JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_processing_log_time ON biometric_processing_log (created_at DESC);
CREATE INDEX idx_processing_log_voter ON biometric_processing_log (voter_vin) WHERE voter_vin IS NOT NULL;
CREATE INDEX idx_processing_log_op ON biometric_processing_log (operation);

-- ─── Enrollment State (TypeScript kiosk → Go pipeline) ─────────

CREATE TABLE IF NOT EXISTS enrollment_sessions (
    session_id      TEXT PRIMARY KEY,
    voter_vin       TEXT NOT NULL,
    device_id       TEXT,
    current_step    TEXT NOT NULL DEFAULT 'consent',
    status          TEXT NOT NULL DEFAULT 'in_progress' CHECK (status IN ('in_progress', 'completed', 'failed', 'cancelled')),
    fingerprint_captured    BOOLEAN NOT NULL DEFAULT FALSE,
    face_captured           BOOLEAN NOT NULL DEFAULT FALSE,
    iris_captured           BOOLEAN NOT NULL DEFAULT FALSE,
    fingerprint_quality     DOUBLE PRECISION,
    face_quality            DOUBLE PRECISION,
    iris_quality            DOUBLE PRECISION,
    consent_given           BOOLEAN NOT NULL DEFAULT FALSE,
    consent_timestamp       TIMESTAMPTZ,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    metadata_json   JSONB
);

CREATE INDEX idx_enrollment_voter ON enrollment_sessions (voter_vin);
CREATE INDEX idx_enrollment_status ON enrollment_sessions (status);
