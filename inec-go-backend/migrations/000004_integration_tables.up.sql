-- Missing Tables for Middleware Integrations

CREATE TABLE IF NOT EXISTS keycloak_sessions (
    id SERIAL PRIMARY KEY,
    session_id TEXT UNIQUE NOT NULL,
    user_id TEXT NOT NULL,
    ip_address TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS temporal_workflow_history (
    id SERIAL PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    status TEXT NOT NULL,
    event_type TEXT NOT NULL,
    details JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_temporal_wf ON temporal_workflow_history(workflow_id);

CREATE TABLE IF NOT EXISTS fluvio_stream_offsets (
    topic TEXT PRIMARY KEY,
    partition_id INTEGER NOT NULL DEFAULT 0,
    current_offset BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tigerbeetle_account_registry (
    id SERIAL PRIMARY KEY,
    tb_account_id BIGINT UNIQUE NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_tb_entity ON tigerbeetle_account_registry(entity_type, entity_id);

CREATE TABLE IF NOT EXISTS permify_policy_versions (
    version_id TEXT PRIMARY KEY,
    schema_text TEXT NOT NULL,
    deployed_by TEXT,
    deployed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS apisix_route_audit (
    id SERIAL PRIMARY KEY,
    route_id TEXT NOT NULL,
    action TEXT NOT NULL,
    changed_by TEXT,
    changed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS openappsec_threat_intelligence (
    ip_address TEXT PRIMARY KEY,
    risk_score INTEGER NOT NULL DEFAULT 0,
    last_attack_type TEXT,
    blocked_until TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS dapr_state_snapshots (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    etag TEXT,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS redis_cache_invalidation_log (
    id SERIAL PRIMARY KEY,
    pattern TEXT NOT NULL,
    triggered_by TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS election_collation_sheets (
    id SERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    level TEXT NOT NULL CHECK(level IN ('ward', 'lga', 'state', 'national')),
    code TEXT NOT NULL,
    document_url TEXT,
    verified_by TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS candidate_registrations (
    id SERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    party_code TEXT NOT NULL REFERENCES parties(code),
    candidate_name TEXT NOT NULL,
    position TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS accreditation_records (
    id SERIAL PRIMARY KEY,
    voter_vin TEXT NOT NULL,
    polling_unit_code TEXT NOT NULL,
    bvas_device_id TEXT NOT NULL,
    auth_method TEXT NOT NULL CHECK(auth_method IN ('fingerprint', 'facial', 'manual')),
    accredited_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_accreditation_pu ON accreditation_records(polling_unit_code);
