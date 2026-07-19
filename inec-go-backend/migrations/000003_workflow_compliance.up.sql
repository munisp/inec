-- ─── Stakeholder Management ────────────────────────────────────
CREATE TABLE IF NOT EXISTS staff_assignments (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    election_id INTEGER NOT NULL REFERENCES elections(id),
    role TEXT NOT NULL,
    state_code TEXT,
    lga_code TEXT,
    ward_code TEXT,
    polling_unit_code TEXT,
    assigned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_staff_assignments_election ON staff_assignments(election_id);
CREATE INDEX idx_staff_assignments_user ON staff_assignments(user_id);

CREATE TABLE IF NOT EXISTS voter_registrations (
    vin TEXT PRIMARY KEY,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    date_of_birth DATE NOT NULL,
    gender TEXT NOT NULL,
    occupation TEXT,
    state_code TEXT NOT NULL REFERENCES states(code),
    lga_code TEXT NOT NULL REFERENCES lgas(code),
    ward_code TEXT NOT NULL REFERENCES wards(code),
    polling_unit_code TEXT NOT NULL REFERENCES polling_units(code),
    status TEXT DEFAULT 'active' CHECK(status IN ('active', 'suspended', 'deceased')),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_voter_registrations_pu ON voter_registrations(polling_unit_code);

-- ─── Workflow and Compliance ───────────────────────────────────
CREATE TABLE IF NOT EXISTS workflow_instances (
    id SERIAL PRIMARY KEY,
    workflow_id TEXT UNIQUE NOT NULL,
    workflow_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP
);

CREATE INDEX idx_workflow_instances_entity ON workflow_instances(entity_type, entity_id);

CREATE TABLE IF NOT EXISTS compliance_records (
    id SERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    polling_unit_code TEXT NOT NULL REFERENCES polling_units(code),
    check_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pass', 'fail', 'warning')),
    details JSONB,
    checked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_compliance_records_election ON compliance_records(election_id);

CREATE TABLE IF NOT EXISTS dispute_cases (
    id SERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    polling_unit_code TEXT REFERENCES polling_units(code),
    ward_code TEXT REFERENCES wards(code),
    lga_code TEXT REFERENCES lgas(code),
    state_code TEXT REFERENCES states(code),
    raised_by TEXT NOT NULL,
    category TEXT NOT NULL,
    description TEXT NOT NULL,
    status TEXT DEFAULT 'open' CHECK(status IN ('open', 'investigating', 'resolved', 'dismissed')),
    resolution_notes TEXT,
    resolved_by TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_dispute_cases_election ON dispute_cases(election_id);

-- ─── Middleware Logs ───────────────────────────────────────────
CREATE TABLE IF NOT EXISTS permify_audit_log (
    id SERIAL PRIMARY KEY,
    subject TEXT NOT NULL,
    subject_type TEXT NOT NULL,
    permission TEXT NOT NULL,
    resource TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    decision BOOLEAN NOT NULL,
    latency_ms DOUBLE PRECISION,
    checked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS openappsec_events (
    id SERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    source_ip TEXT NOT NULL,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    action TEXT NOT NULL,
    threat_level TEXT NOT NULL,
    rule_matched TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS stablecoin_transactions (
    id SERIAL PRIMARY KEY,
    transaction_id TEXT UNIQUE NOT NULL,
    from_account TEXT NOT NULL,
    to_account TEXT NOT NULL,
    amount DOUBLE PRECISION NOT NULL,
    currency TEXT DEFAULT 'eNGN',
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'completed', 'failed')),
    purpose TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP
);

-- COMMIT removed: the migration runner wraps each file in its own transaction
