package main

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// ── Database Migration System ──
// Provides versioned, ordered schema migrations with rollback support.
// Migrations are embedded in the binary (no external files needed).

type Migration struct {
	Version     int
	Description string
	Up          string
	Down        string
}

// migrations is the ordered list of all schema migrations.
var migrations = []Migration{
	{
		Version:     1,
		Description: "Core tables (users, elections, results, parties, polling_units)",
		Up: `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	description TEXT,
	applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT UNIQUE NOT NULL,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT 'public',
	full_name TEXT,
	email TEXT,
	phone TEXT,
	state_code TEXT,
	lga_code TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	is_active BOOLEAN DEFAULT 1
);
CREATE TABLE IF NOT EXISTS elections (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	election_type TEXT NOT NULL,
	election_date TEXT NOT NULL,
	status TEXT DEFAULT 'upcoming',
	created_by INTEGER,
	total_polling_units INTEGER DEFAULT 0,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	CONSTRAINT chk_election_type CHECK (election_type IN ('presidential','gubernatorial','senatorial','house_of_reps','state_assembly','local_government')),
	CONSTRAINT chk_election_status CHECK (status IN ('upcoming','active','completed','cancelled','draft','scheduled','voting','collating','closed','disputed'))
);
CREATE TABLE IF NOT EXISTS results (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	election_id INTEGER NOT NULL,
	polling_unit_code TEXT NOT NULL,
	total_votes INTEGER DEFAULT 0,
	total_valid INTEGER DEFAULT 0,
	rejected_ballots INTEGER DEFAULT 0,
	accredited_voters INTEGER DEFAULT 0,
	registered_voters INTEGER DEFAULT 0,
	status TEXT DEFAULT 'pending',
	submitted_by INTEGER,
	submitted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	validated_by INTEGER,
	validated_at TIMESTAMP,
	CONSTRAINT chk_result_status CHECK (status IN ('pending','validated','finalized','disputed'))
);
CREATE TABLE IF NOT EXISTS result_party_scores (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	result_id INTEGER NOT NULL,
	party_code TEXT NOT NULL,
	votes INTEGER DEFAULT 0,
	FOREIGN KEY (result_id) REFERENCES results(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_results_election ON results(election_id);
CREATE INDEX IF NOT EXISTS idx_results_pu ON results(polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_results_status ON results(status);
`,
		Down: `-- Down migrations for core tables are intentionally disabled in production.
-- Dropping users, elections, results would destroy all election data.
-- To rollback, use point-in-time recovery from database backups.
SELECT 1;
`,
	},
	{
		Version:     2,
		Description: "Security tables (token_blacklist, active_sessions, api_key_metadata)",
		Up: `
CREATE TABLE IF NOT EXISTS token_blacklist (
	jti TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL,
	revoked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	expires_at TIMESTAMP NOT NULL,
	reason TEXT DEFAULT ''
);
CREATE TABLE IF NOT EXISTS active_sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	jti TEXT UNIQUE NOT NULL,
	user_id INTEGER NOT NULL,
	ip_address TEXT,
	user_agent TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	expires_at TIMESTAMP NOT NULL,
	last_activity TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON active_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_jti ON active_sessions(jti);
CREATE TABLE IF NOT EXISTS api_key_metadata (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	key_hash TEXT UNIQUE NOT NULL,
	name TEXT NOT NULL,
	owner_id INTEGER NOT NULL,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	expires_at TIMESTAMP,
	rotated_from TEXT,
	is_active BOOLEAN DEFAULT 1,
	last_used_at TIMESTAMP,
	usage_count INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_apikey_active ON api_key_metadata(is_active, expires_at);
`,
		Down: `
DROP TABLE IF EXISTS api_key_metadata;
DROP TABLE IF EXISTS active_sessions;
DROP TABLE IF EXISTS token_blacklist;
`,
	},
	{
		Version:     3,
		Description: "Middleware persistence tables",
		Up: `
CREATE TABLE IF NOT EXISTS mojaloop_transactions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	transaction_id TEXT UNIQUE,
	payer TEXT, payee TEXT, amount REAL, currency TEXT,
	status TEXT DEFAULT 'pending',
	ilp_packet TEXT, condition TEXT, fulfilment TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	completed_at TIMESTAMP
);
CREATE TABLE IF NOT EXISTS opensearch_documents (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	index_name TEXT NOT NULL,
	doc_id TEXT NOT NULL,
	body TEXT NOT NULL,
	indexed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(index_name, doc_id)
);
CREATE TABLE IF NOT EXISTS waf_blocklist (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ip_address TEXT UNIQUE NOT NULL,
	reason TEXT,
	blocked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	expires_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_waf_ip ON waf_blocklist(ip_address);
CREATE INDEX IF NOT EXISTS idx_opensearch_idx ON opensearch_documents(index_name);
`,
		Down: `
DROP TABLE IF EXISTS waf_blocklist;
DROP TABLE IF EXISTS opensearch_documents;
DROP TABLE IF EXISTS mojaloop_transactions;
`,
	},
	{
		Version:     4,
		Description: "Geo-fencing and location validation",
		Up: `
CREATE TABLE IF NOT EXISTS polling_unit_locations (
	polling_unit_code TEXT PRIMARY KEY,
	latitude REAL NOT NULL,
	longitude REAL NOT NULL,
	geofence_radius_m INTEGER DEFAULT 500,
	state_code TEXT,
	lga_code TEXT
);
CREATE TABLE IF NOT EXISTS bvas_location_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	bvas_serial TEXT NOT NULL,
	polling_unit_code TEXT NOT NULL,
	latitude REAL NOT NULL,
	longitude REAL NOT NULL,
	distance_from_pu_m REAL,
	within_geofence BOOLEAN,
	logged_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_bvas_loc_pu ON bvas_location_logs(polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_bvas_loc_serial ON bvas_location_logs(bvas_serial);
`,
		Down: `
DROP TABLE IF EXISTS bvas_location_logs;
DROP TABLE IF EXISTS polling_unit_locations;
`,
	},
	{
		Version:     5,
		Description: "Row-level security policies (application-enforced) + expand election FSM states",
		Up: `
CREATE TABLE IF NOT EXISTS row_access_policies (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	table_name TEXT NOT NULL,
	policy_name TEXT NOT NULL,
	role TEXT NOT NULL,
	condition_column TEXT NOT NULL,
	condition_value TEXT NOT NULL,
	permission TEXT NOT NULL DEFAULT 'read',
	UNIQUE(table_name, policy_name, role)
);
INSERT OR IGNORE INTO row_access_policies (table_name, policy_name, role, condition_column, condition_value, permission) VALUES
	('results', 'state_officer_read', 'presiding_officer', 'polling_unit_code', 'ASSIGNED_PU', 'read'),
	('results', 'state_officer_write', 'presiding_officer', 'polling_unit_code', 'ASSIGNED_PU', 'write'),
	('results', 'collation_officer_state', 'collation_officer', 'state_code', 'ASSIGNED_STATE', 'read'),
	('incidents', 'reporter_own', 'observer', 'reported_by', 'SELF', 'read');
`,
		Down: `DROP TABLE IF EXISTS row_access_policies;`,
	},
	{
		Version:     6,
		Description: "Election FSM lifecycle states, dedup resolution, GPS spoofing, webhooks",
		Up: `
CREATE TABLE IF NOT EXISTS election_state_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	election_id INTEGER NOT NULL,
	from_state TEXT NOT NULL,
	to_state TEXT NOT NULL,
	event TEXT NOT NULL,
	actor TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS dedup_resolutions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	voter_a_vin TEXT NOT NULL,
	voter_b_vin TEXT NOT NULL,
	decision TEXT NOT NULL,
	reason TEXT,
	resolved_by TEXT,
	resolved_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS gps_spoof_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	device_id TEXT NOT NULL,
	lat REAL NOT NULL,
	lng REAL NOT NULL,
	confidence REAL NOT NULL,
	indicators TEXT,
	detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS webhook_subscriptions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	url TEXT NOT NULL,
	events TEXT NOT NULL,
	secret TEXT NOT NULL,
	created_by TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	is_active INTEGER DEFAULT 1
);
`,
		Down: `
DROP TABLE IF EXISTS webhook_subscriptions;
DROP TABLE IF EXISTS gps_spoof_events;
DROP TABLE IF EXISTS dedup_resolutions;
DROP TABLE IF EXISTS election_state_log;
`,
	},
	{
		Version:     7,
		Description: "Scale optimization indexes for election day (176K polling units)",
		Up: `
CREATE INDEX IF NOT EXISTS idx_results_election_status ON results(election_id, status);
CREATE INDEX IF NOT EXISTS idx_results_pu_election ON results(polling_unit_code, election_id);
CREATE INDEX IF NOT EXISTS idx_rps_result ON result_party_scores(result_id);
CREATE INDEX IF NOT EXISTS idx_rps_party ON result_party_scores(party_code);
CREATE INDEX IF NOT EXISTS idx_rps_result_party ON result_party_scores(result_id, party_code);
CREATE INDEX IF NOT EXISTS idx_polling_units_ward ON polling_units(ward_code);
CREATE INDEX IF NOT EXISTS idx_wards_lga ON wards(lga_code);
CREATE INDEX IF NOT EXISTS idx_lgas_state ON lgas(state_code);
CREATE INDEX IF NOT EXISTS idx_collation_election_level ON collation_results(election_id, level, area_code);
CREATE INDEX IF NOT EXISTS idx_collation_level_area ON collation_results(level, area_code);
CREATE INDEX IF NOT EXISTS idx_results_submitted_at ON results(submitted_at);
CREATE INDEX IF NOT EXISTS idx_results_election_pu ON results(election_id, polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
CREATE INDEX IF NOT EXISTS idx_ingestion_status ON ingestion_jobs(status);
CREATE INDEX IF NOT EXISTS idx_ingestion_idem ON ingestion_jobs(idempotency_key);
`,
		Down: `
DROP INDEX IF EXISTS idx_results_election_status;
DROP INDEX IF EXISTS idx_results_pu_election;
DROP INDEX IF EXISTS idx_rps_result;
DROP INDEX IF EXISTS idx_rps_party;
DROP INDEX IF EXISTS idx_rps_result_party;
DROP INDEX IF EXISTS idx_polling_units_ward;
DROP INDEX IF EXISTS idx_wards_lga;
DROP INDEX IF EXISTS idx_lgas_state;
DROP INDEX IF EXISTS idx_collation_election_level;
DROP INDEX IF EXISTS idx_collation_level_area;
DROP INDEX IF EXISTS idx_results_submitted_at;
DROP INDEX IF EXISTS idx_results_election_pu;
DROP INDEX IF EXISTS idx_audit_timestamp;
DROP INDEX IF EXISTS idx_audit_action;
DROP INDEX IF EXISTS idx_ingestion_status;
DROP INDEX IF EXISTS idx_ingestion_idem;
`,
	},
}

// runMigrations executes all pending migrations in order.
func runMigrations(database *sql.DB) error {
	// Ensure migrations table exists
	database.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		description TEXT,
		applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)

	// Get current version
	var currentVersion int
	row := database.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations")
	row.Scan(&currentVersion)

	// Sort migrations by version
	sorted := make([]Migration, len(migrations))
	copy(sorted, migrations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	applied := 0
	for _, m := range sorted {
		if m.Version <= currentVersion {
			continue
		}

		log.Info().Int("version", m.Version).Str("description", m.Description).Msg("Applying migration")

		tx, err := database.Begin()
		if err != nil {
			return err
		}

		upSQL := convertDDLForPostgres(m.Up)
		stmts := strings.Split(upSQL, ";")
		for _, stmt := range stmts {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				trimmed := stmt
				if len(trimmed) > 80 {
					trimmed = trimmed[:80]
				}
				log.Error().Err(err).Int("version", m.Version).Str("stmt", trimmed).Msg("Migration statement failed")
				return err
			}
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version, description, applied_at) VALUES (?, ?, ?)",
			m.Version, m.Description, time.Now()); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		applied++
	}

	if applied > 0 {
		log.Info().Int("applied", applied).Int("current_version", currentVersion+applied).Msg("Migrations complete")
	} else {
		log.Info().Int("current_version", currentVersion).Msg("Database schema up to date")
	}
	return nil
}

// rollbackMigration rolls back the most recent migration.
func rollbackMigration(database *sql.DB) error {
	var currentVersion int
	database.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)

	if currentVersion == 0 {
		log.Info().Msg("No migrations to rollback")
		return nil
	}

	for _, m := range migrations {
		if m.Version == currentVersion {
			log.Info().Int("version", m.Version).Msg("Rolling back migration")
			if _, err := database.Exec(m.Down); err != nil {
				return err
			}
			database.Exec("DELETE FROM schema_migrations WHERE version = ?", currentVersion)
			return nil
		}
	}
	return nil
}
