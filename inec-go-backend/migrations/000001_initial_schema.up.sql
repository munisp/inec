-- INEC 2027 Election Platform: Initial Schema Migration
-- This migration creates all core tables needed for the platform.

BEGIN;

-- Core identity tables
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    full_name TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('admin','presiding_officer','collation_officer','observer','public')),
    staff_id TEXT UNIQUE,
    state_code TEXT,
    lga_code TEXT,
    polling_unit_code TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    is_active BOOLEAN DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS elections (
    id SERIAL PRIMARY KEY,
    title TEXT NOT NULL,
    election_type TEXT NOT NULL CHECK(election_type IN ('presidential','gubernatorial','senatorial','house_of_reps','state_assembly','local_government')),
    election_date TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'upcoming' CHECK(status IN ('upcoming','active','completed','cancelled')),
    description TEXT,
    total_registered_voters INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS parties (
    id SERIAL PRIMARY KEY,
    code TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    abbreviation TEXT NOT NULL,
    logo_url TEXT,
    color TEXT,
    is_active BOOLEAN DEFAULT TRUE
);

-- Geography
CREATE TABLE IF NOT EXISTS states (
    id SERIAL PRIMARY KEY,
    code TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    geo_zone TEXT NOT NULL,
    capital TEXT
);

CREATE TABLE IF NOT EXISTS lgas (
    id SERIAL PRIMARY KEY,
    code TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    state_code TEXT NOT NULL REFERENCES states(code)
);

CREATE TABLE IF NOT EXISTS wards (
    id SERIAL PRIMARY KEY,
    code TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    lga_code TEXT NOT NULL REFERENCES lgas(code)
);

CREATE TABLE IF NOT EXISTS polling_units (
    id SERIAL PRIMARY KEY,
    code TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    ward_code TEXT NOT NULL REFERENCES wards(code),
    state_code TEXT NOT NULL REFERENCES states(code),
    lga_code TEXT NOT NULL REFERENCES lgas(code),
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    registered_voters INTEGER DEFAULT 0,
    pu_type TEXT DEFAULT 'standard'
);

-- Results
CREATE TABLE IF NOT EXISTS results (
    id SERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    polling_unit_code TEXT NOT NULL REFERENCES polling_units(code),
    party_code TEXT NOT NULL REFERENCES parties(code),
    votes INTEGER NOT NULL DEFAULT 0,
    is_valid BOOLEAN DEFAULT TRUE,
    submitted_by TEXT,
    verified_by TEXT,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending','verified','disputed','rejected')),
    idempotency_key TEXT UNIQUE,
    submitted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    verified_at TIMESTAMP
);

-- Audit trail
CREATE TABLE IF NOT EXISTS audit_trail (
    id SERIAL PRIMARY KEY,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT,
    user_id TEXT,
    details JSONB,
    ip_address TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Incidents
CREATE TABLE IF NOT EXISTS incidents (
    id SERIAL PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    severity TEXT NOT NULL CHECK(severity IN ('low','medium','high','critical')),
    status TEXT DEFAULT 'open' CHECK(status IN ('open','investigating','resolved','closed')),
    polling_unit_code TEXT,
    state_code TEXT,
    reported_by TEXT,
    assigned_to TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP
);

-- BVAS devices
CREATE TABLE IF NOT EXISTS bvas_devices (
    id SERIAL PRIMARY KEY,
    device_id TEXT UNIQUE NOT NULL,
    serial_number TEXT NOT NULL,
    polling_unit_code TEXT,
    state_code TEXT,
    status TEXT DEFAULT 'active' CHECK(status IN ('active','inactive','maintenance','lost')),
    firmware_version TEXT,
    battery_level INTEGER DEFAULT 100,
    last_sync TIMESTAMP,
    accreditations INTEGER DEFAULT 0,
    biometric_captures INTEGER DEFAULT 0,
    match_rate DOUBLE PRECISION DEFAULT 0.0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for production performance
CREATE INDEX IF NOT EXISTS idx_results_election ON results(election_id);
CREATE INDEX IF NOT EXISTS idx_results_pu ON results(polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_results_party ON results(party_code);
CREATE INDEX IF NOT EXISTS idx_results_status ON results(status);
CREATE INDEX IF NOT EXISTS idx_results_idempotency ON results(idempotency_key);
CREATE INDEX IF NOT EXISTS idx_polling_units_state ON polling_units(state_code);
CREATE INDEX IF NOT EXISTS idx_polling_units_lga ON polling_units(lga_code);
CREATE INDEX IF NOT EXISTS idx_polling_units_ward ON polling_units(ward_code);
CREATE INDEX IF NOT EXISTS idx_lgas_state ON lgas(state_code);
CREATE INDEX IF NOT EXISTS idx_wards_lga ON wards(lga_code);
CREATE INDEX IF NOT EXISTS idx_audit_trail_entity ON audit_trail(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_trail_user ON audit_trail(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_trail_created ON audit_trail(created_at);
CREATE INDEX IF NOT EXISTS idx_incidents_status ON incidents(status);
CREATE INDEX IF NOT EXISTS idx_incidents_severity ON incidents(severity);
CREATE INDEX IF NOT EXISTS idx_incidents_state ON incidents(state_code);
CREATE INDEX IF NOT EXISTS idx_bvas_polling_unit ON bvas_devices(polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_bvas_state ON bvas_devices(state_code);
CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
CREATE INDEX IF NOT EXISTS idx_users_state ON users(state_code);

COMMIT;
