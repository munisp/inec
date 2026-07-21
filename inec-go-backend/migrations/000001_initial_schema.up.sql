-- INEC 2027 Election Platform: Initial Schema
-- Auto-generated from actual PostgreSQL schema

CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username text NOT NULL,
    password_hash text NOT NULL,
    full_name text NOT NULL,
    role text NOT NULL,
    staff_id text,
    state_code text,
    lga_code text,
    polling_unit_code text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    is_active integer DEFAULT 1,
    kyc_status text DEFAULT 'not_started'::text,
    CONSTRAINT users_role_check CHECK ((role = ANY (ARRAY['admin'::text, 'presiding_officer'::text, 'collation_officer'::text, 'observer'::text, 'public'::text])))
);

CREATE INDEX IF NOT EXISTS idx_users_role ON users USING btree (role);
CREATE INDEX IF NOT EXISTS idx_users_state ON users USING btree (state_code);
CREATE INDEX IF NOT EXISTS idx_users_username ON users USING btree (username);

CREATE TABLE IF NOT EXISTS elections (
    id SERIAL PRIMARY KEY,
    title text NOT NULL,
    election_type text NOT NULL,
    election_date text NOT NULL,
    status text DEFAULT 'upcoming'::text NOT NULL,
    description text,
    total_registered_voters integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT elections_election_type_check CHECK ((election_type = ANY (ARRAY['presidential'::text, 'gubernatorial'::text, 'senatorial'::text, 'house_of_reps'::text, 'state_assembly'::text, 'local_government'::text]))),
    CONSTRAINT elections_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'scheduled'::text, 'upcoming'::text, 'active'::text, 'voting'::text, 'collating'::text, 'closed'::text, 'completed'::text, 'cancelled'::text, 'disputed'::text])))
);

CREATE INDEX IF NOT EXISTS idx_elections_date ON elections USING btree (election_date);
CREATE INDEX IF NOT EXISTS idx_elections_status ON elections USING btree (status);

CREATE TABLE IF NOT EXISTS parties (
    id SERIAL PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    abbreviation text NOT NULL,
    logo_url text,
    color text,
    is_active integer DEFAULT 1
);

CREATE TABLE IF NOT EXISTS states (
    id SERIAL PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    geo_zone text NOT NULL,
    capital text
);

CREATE TABLE IF NOT EXISTS lgas (
    id SERIAL PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    state_code text NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_lgas_state ON lgas USING btree (state_code);

CREATE TABLE IF NOT EXISTS wards (
    id SERIAL PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    lga_code text NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_wards_lga ON wards USING btree (lga_code);

CREATE TABLE IF NOT EXISTS polling_units (
    id SERIAL PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    ward_code text NOT NULL,
    registered_voters integer DEFAULT 0,
    latitude real,
    longitude real
);

CREATE INDEX IF NOT EXISTS idx_polling_units_ward ON polling_units USING btree (ward_code);
CREATE INDEX IF NOT EXISTS idx_pu_lonlat ON polling_units USING btree (longitude, latitude);

CREATE TABLE IF NOT EXISTS results (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    polling_unit_code text NOT NULL,
    presiding_officer_id integer,
    status text DEFAULT 'pending'::text NOT NULL,
    total_valid_votes integer DEFAULT 0,
    rejected_votes integer DEFAULT 0,
    total_votes_cast integer DEFAULT 0,
    accredited_voters integer DEFAULT 0,
    ec8a_hash text,
    tigerbeetle_transfer_id text,
    hyperledger_tx_id text,
    tigerbeetle_status text DEFAULT 'PENDING'::text,
    hyperledger_status text DEFAULT 'PENDING'::text,
    ipfs_cid text,
    submitted_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    validated_at timestamp without time zone,
    finalized_at timestamp without time zone,
    CONSTRAINT results_hyperledger_status_check CHECK ((hyperledger_status = ANY (ARRAY['PENDING'::text, 'CONFIRMED'::text, 'FAILED'::text]))),
    CONSTRAINT results_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'validated'::text, 'finalized'::text, 'disputed'::text, 'voided'::text]))),
    CONSTRAINT results_tigerbeetle_status_check CHECK ((tigerbeetle_status = ANY (ARRAY['PENDING'::text, 'POSTED'::text, 'VOIDED'::text])))
);

CREATE INDEX IF NOT EXISTS idx_results_election ON results USING btree (election_id);
CREATE INDEX IF NOT EXISTS idx_results_election_pu ON results USING btree (election_id, polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_results_election_status ON results USING btree (election_id, status);
CREATE INDEX IF NOT EXISTS idx_results_pu ON results USING btree (polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_results_pu_election ON results USING btree (polling_unit_code, election_id);
CREATE INDEX IF NOT EXISTS idx_results_status ON results USING btree (status);
CREATE INDEX IF NOT EXISTS idx_results_submitted_at ON results USING btree (submitted_at);

CREATE TABLE IF NOT EXISTS incidents (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL,
    polling_unit_code text,
    reported_by integer,
    incident_type text NOT NULL,
    description text NOT NULL,
    severity text NOT NULL,
    status text DEFAULT 'reported'::text NOT NULL,
    reported_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    resolved_at timestamp without time zone,
    CONSTRAINT incidents_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))),
    CONSTRAINT incidents_status_check CHECK ((status = ANY (ARRAY['reported'::text, 'investigating'::text, 'resolved'::text, 'dismissed'::text])))
);

CREATE TABLE IF NOT EXISTS bvas_devices (
    id text NOT NULL,
    serial_number text NOT NULL,
    polling_unit_code text,
    election_id integer,
    status text DEFAULT 'registered'::text NOT NULL,
    battery_level integer DEFAULT 100,
    firmware_version text DEFAULT '3.2.1'::text,
    last_sync_at timestamp without time zone,
    latitude real,
    longitude real,
    assigned_officer integer,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT bvas_devices_status_check CHECK ((status = ANY (ARRAY['registered'::text, 'deployed'::text, 'active'::text, 'offline'::text, 'faulty'::text, 'decommissioned'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bvas_devices_pu ON bvas_devices USING btree (polling_unit_code);

