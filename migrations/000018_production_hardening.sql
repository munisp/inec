-- 000018: Production hardening — crowd estimates, federated learning,
-- circuit breaker state, and integration audit tables.

-- Crowd estimation history (upgraded from in-memory to DB-backed)
CREATE TABLE IF NOT EXISTS gotv_crowd_estimates (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL,
    event_name TEXT NOT NULL,
    state_code TEXT,
    venue_type TEXT DEFAULT 'open_field',
    venue_area_sqm REAL DEFAULT 5000,
    density_per_sqm REAL DEFAULT 1.5,
    estimated_crowd INTEGER NOT NULL,
    confidence_low INTEGER,
    confidence_high INTEGER,
    image_url TEXT,
    model_version TEXT DEFAULT 'density-area-v2',
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_crowd_estimates_party ON gotv_crowd_estimates(party_id);

-- Federated learning round tracking (upgraded from static stub)
CREATE TABLE IF NOT EXISTS gotv_federated_rounds (
    id SERIAL PRIMARY KEY,
    round_number INTEGER NOT NULL,
    party_id INTEGER NOT NULL,
    model_type TEXT NOT NULL DEFAULT 'turnout_prediction',
    status TEXT NOT NULL DEFAULT 'pending',
    gradient_checksum TEXT,
    epsilon REAL DEFAULT 1.0,
    started_at TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_fed_rounds_party ON gotv_federated_rounds(party_id);

CREATE TABLE IF NOT EXISTS gotv_federated_participants (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL UNIQUE,
    opted_in BOOLEAN DEFAULT false,
    data_contribution_count INTEGER DEFAULT 0,
    last_participated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
