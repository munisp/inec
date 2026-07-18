-- Migration 000020: Missing service tables discovered in schema audit
-- Tables referenced by running code but never created anywhere:
--   audit_trail (mw_opensearch), officials + geofences (internal/geo),
--   gotv_canvass_logs (gotv-svc KOH indicators), mw_mojaloop_callbacks (mw_mojaloop),
--   ndpr_consent / ndpr_dsr_requests / ndpr_breaches (internal/compliance)

-- ─── Audit trail (queried by OpenSearch sync) ───────────────────────
CREATE TABLE IF NOT EXISTS audit_trail (
    id BIGSERIAL PRIMARY KEY,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT,
    user_id TEXT,
    details JSONB DEFAULT '{}'::jsonb,
    ip_address TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_audit_trail_entity ON audit_trail (entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_trail_created ON audit_trail (created_at DESC);

-- ─── Field officials GPS tracking (internal/geo) ────────────────────
CREATE TABLE IF NOT EXISTS officials (
    id BIGSERIAL PRIMARY KEY,
    staff_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'field_officer',
    state_code TEXT,
    lga_code TEXT,
    speed DOUBLE PRECISION DEFAULT 0,
    heading DOUBLE PRECISION DEFAULT 0,
    battery_level INT DEFAULT 100,
    status TEXT NOT NULL DEFAULT 'active',
    last_seen TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_officials_state ON officials (state_code);
CREATE INDEX IF NOT EXISTS idx_officials_last_seen ON officials (last_seen DESC);

DO $$
BEGIN
    ALTER TABLE officials ADD COLUMN IF NOT EXISTS location GEOGRAPHY(POINT, 4326);
    CREATE INDEX IF NOT EXISTS idx_officials_location ON officials USING GIST (location);
EXCEPTION WHEN OTHERS THEN
    RAISE WARNING 'officials.location skipped: %', SQLERRM;
END $$;

-- ─── Geofences (internal/geo) ───────────────────────────────────────
CREATE TABLE IF NOT EXISTS geofences (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'restricted',
    radius_meters DOUBLE PRECISION NOT NULL DEFAULT 500,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_geofences_active ON geofences (active) WHERE active = TRUE;

DO $$
BEGIN
    ALTER TABLE geofences ADD COLUMN IF NOT EXISTS center GEOGRAPHY(POINT, 4326) NOT NULL DEFAULT 'POINT(0 0)';
    CREATE INDEX IF NOT EXISTS idx_geofences_center ON geofences USING GIST (center);
    ALTER TABLE geofences ALTER COLUMN center DROP DEFAULT;
EXCEPTION WHEN OTHERS THEN
    RAISE WARNING 'geofences.center skipped: %', SQLERRM;
END $$;

-- ─── GOTV canvass logs (KOH ground-mobilisation indicators) ────────
CREATE TABLE IF NOT EXISTS gotv_canvass_logs (
    id BIGSERIAL PRIMARY KEY,
    party_id INT NOT NULL,
    territory_id INT,
    contact_id TEXT,
    volunteer_id TEXT,
    outcome TEXT DEFAULT 'knocked',
    notes TEXT,
    knocked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_canvass_party ON gotv_canvass_logs (party_id, knocked_at DESC);
CREATE INDEX IF NOT EXISTS idx_canvass_territory ON gotv_canvass_logs (territory_id);

-- ─── Mojaloop callback journal (mw_mojaloop) ────────────────────────
CREATE TABLE IF NOT EXISTS mw_mojaloop_callbacks (
    id BIGSERIAL PRIMARY KEY,
    type TEXT NOT NULL,
    resource_id TEXT,
    payload JSONB DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'received',
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_mojaloop_callbacks_resource ON mw_mojaloop_callbacks (resource_id);

-- ─── NDPR compliance: consent (internal/compliance) ─────────────────
CREATE TABLE IF NOT EXISTS ndpr_consent (
    id BIGSERIAL PRIMARY KEY,
    subject_nin TEXT NOT NULL,
    purpose TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'granted',
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    withdrawn_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_ndpr_consent_subject ON ndpr_consent (subject_nin, status);

-- ─── NDPR compliance: data subject requests ─────────────────────────
CREATE TABLE IF NOT EXISTS ndpr_dsr_requests (
    id BIGSERIAL PRIMARY KEY,
    subject_nin TEXT NOT NULL,
    right_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    details TEXT,
    response TEXT
);
CREATE INDEX IF NOT EXISTS idx_ndpr_dsr_subject ON ndpr_dsr_requests (subject_nin, status);

-- ─── NDPR compliance: breach register ───────────────────────────────
CREATE TABLE IF NOT EXISTS ndpr_breaches (
    id BIGSERIAL PRIMARY KEY,
    severity TEXT NOT NULL,
    description TEXT NOT NULL,
    affected_count INT NOT NULL DEFAULT 0,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    contained_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'detected'
);
