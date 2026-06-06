package main

import "database/sql"

func initDB(db *sql.DB) {
	// PostgreSQL handles this natively
	// PostgreSQL handles this natively

	schema := `
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
		is_active INTEGER DEFAULT 1
	);
	CREATE TABLE IF NOT EXISTS elections (
		id SERIAL PRIMARY KEY,
		title TEXT NOT NULL,
		election_type TEXT NOT NULL CHECK(election_type IN ('presidential','gubernatorial','senatorial','house_of_reps','state_assembly','local_government')),
		election_date TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'upcoming' CHECK(status IN ('upcoming','active','completed','cancelled','draft','scheduled','voting','collating','closed','disputed')),
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
		is_active INTEGER DEFAULT 1
	);
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
		state_code TEXT NOT NULL,
		FOREIGN KEY (state_code) REFERENCES states(code)
	);
	CREATE TABLE IF NOT EXISTS wards (
		id SERIAL PRIMARY KEY,
		code TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		lga_code TEXT NOT NULL,
		FOREIGN KEY (lga_code) REFERENCES lgas(code)
	);
	CREATE TABLE IF NOT EXISTS polling_units (
		id SERIAL PRIMARY KEY,
		code TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		ward_code TEXT NOT NULL,
		registered_voters INTEGER DEFAULT 0,
		latitude REAL,
		longitude REAL,
		FOREIGN KEY (ward_code) REFERENCES wards(code)
	);
	CREATE TABLE IF NOT EXISTS results (
		id SERIAL PRIMARY KEY,
		election_id INTEGER NOT NULL,
		polling_unit_code TEXT NOT NULL,
		presiding_officer_id INTEGER,
		status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','validated','finalized','disputed','voided')),
		total_valid_votes INTEGER DEFAULT 0,
		rejected_votes INTEGER DEFAULT 0,
		total_votes_cast INTEGER DEFAULT 0,
		accredited_voters INTEGER DEFAULT 0,
		ec8a_hash TEXT,
		tigerbeetle_transfer_id TEXT,
		hyperledger_tx_id TEXT,
		tigerbeetle_status TEXT DEFAULT 'PENDING' CHECK(tigerbeetle_status IN ('PENDING','POSTED','VOIDED')),
		hyperledger_status TEXT DEFAULT 'PENDING' CHECK(hyperledger_status IN ('PENDING','CONFIRMED','FAILED')),
		ipfs_cid TEXT,
		submitted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		validated_at TIMESTAMP,
		finalized_at TIMESTAMP,
		FOREIGN KEY (election_id) REFERENCES elections(id),
		FOREIGN KEY (polling_unit_code) REFERENCES polling_units(code),
		FOREIGN KEY (presiding_officer_id) REFERENCES users(id),
		UNIQUE(election_id, polling_unit_code)
	);
	CREATE TABLE IF NOT EXISTS result_party_scores (
		id SERIAL PRIMARY KEY,
		result_id INTEGER NOT NULL,
		party_code TEXT NOT NULL,
		votes INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (result_id) REFERENCES results(id),
		FOREIGN KEY (party_code) REFERENCES parties(code),
		UNIQUE(result_id, party_code)
	);
	CREATE TABLE IF NOT EXISTS audit_log (
		id SERIAL PRIMARY KEY,
		action TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT,
		user_id INTEGER,
		details TEXT,
		block_hash TEXT,
		prev_block_hash TEXT,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id)
	);
	CREATE TABLE IF NOT EXISTS incidents (
		id SERIAL PRIMARY KEY,
		election_id INTEGER NOT NULL,
		polling_unit_code TEXT,
		reported_by INTEGER,
		incident_type TEXT NOT NULL,
		description TEXT NOT NULL,
		severity TEXT NOT NULL CHECK(severity IN ('low','medium','high','critical')),
		status TEXT NOT NULL DEFAULT 'reported' CHECK(status IN ('reported','investigating','resolved','dismissed')),
		reported_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		resolved_at TIMESTAMP,
		FOREIGN KEY (election_id) REFERENCES elections(id),
		FOREIGN KEY (reported_by) REFERENCES users(id)
	);
	CREATE TABLE IF NOT EXISTS collation_results (
		id SERIAL PRIMARY KEY,
		election_id INTEGER NOT NULL,
		level TEXT NOT NULL CHECK(level IN ('ward','lga','state','national')),
		area_code TEXT NOT NULL,
		area_name TEXT NOT NULL,
		total_registered_voters INTEGER DEFAULT 0,
		total_accredited_voters INTEGER DEFAULT 0,
		total_valid_votes INTEGER DEFAULT 0,
		total_rejected_votes INTEGER DEFAULT 0,
		total_votes_cast INTEGER DEFAULT 0,
		polling_units_reported INTEGER DEFAULT 0,
		polling_units_total INTEGER DEFAULT 0,
		status TEXT DEFAULT 'in_progress' CHECK(status IN ('in_progress','completed','disputed')),
		last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (election_id) REFERENCES elections(id),
		UNIQUE(election_id, level, area_code)
	);
	CREATE TABLE IF NOT EXISTS collation_party_scores (
		id SERIAL PRIMARY KEY,
		collation_result_id INTEGER NOT NULL,
		party_code TEXT NOT NULL,
		votes INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (collation_result_id) REFERENCES collation_results(id),
		FOREIGN KEY (party_code) REFERENCES parties(code),
		UNIQUE(collation_result_id, party_code)
	);
	CREATE TABLE IF NOT EXISTS metrics_client (
		id SERIAL PRIMARY KEY,
		ts TEXT,
		event TEXT,
		data TEXT,
		ua TEXT,
		ip TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_results_election ON results(election_id);
	CREATE INDEX IF NOT EXISTS idx_results_pu ON results(polling_unit_code);
	CREATE INDEX IF NOT EXISTS idx_results_status ON results(status);
	CREATE INDEX IF NOT EXISTS idx_polling_units_ward ON polling_units(ward_code);
	CREATE INDEX IF NOT EXISTS idx_wards_lga ON wards(lga_code);
	CREATE INDEX IF NOT EXISTS idx_lgas_state ON lgas(state_code);
	CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_log(entity_type, entity_id);
	CREATE INDEX IF NOT EXISTS idx_collation_election ON collation_results(election_id, level);
	CREATE INDEX IF NOT EXISTS idx_pu_lonlat ON polling_units(longitude, latitude);
	CREATE INDEX IF NOT EXISTS idx_rps_result ON result_party_scores(result_id);
	CREATE INDEX IF NOT EXISTS idx_rps_party ON result_party_scores(party_code);
	CREATE INDEX IF NOT EXISTS idx_results_election_status ON results(election_id, status);
	CREATE INDEX IF NOT EXISTS idx_results_pu_election ON results(polling_unit_code, election_id);
	CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
	CREATE INDEX IF NOT EXISTS idx_users_state ON users(state_code);
	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	CREATE INDEX IF NOT EXISTS idx_audit_user_action ON audit_log(user_id, action);
	CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(created_at);
	CREATE INDEX IF NOT EXISTS idx_elections_status ON elections(status);
	CREATE INDEX IF NOT EXISTS idx_elections_date ON elections(election_date);
	`
	execMulti(db, schema)
	initBVASTables(db)
	initIngestionTables(db)
	initSMSUSSDTables(db)
	initPublicAPITables(db)
}
