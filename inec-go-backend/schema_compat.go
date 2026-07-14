package main

import (
	"database/sql"

	"github.com/rs/zerolog/log"
)

// initSchemaCompatibility creates tables and views that resolve schema gaps
// discovered across the platform. It runs at the end of startup init, after all
// base tables (results, parties, result_party_scores, fabric_blocks,
// election_staff_assignments, ...) have been created, so the views below can
// reference them safely.
//
// Two classes of fixes are applied:
//
//  1. Genuinely missing base tables that are written/read by features but were
//     never created (anomaly_scores, payment_queue, predictions).
//  2. Compatibility views for queries that reference a legacy/divergent table
//     name. The canonical tables are result_party_scores,
//     election_staff_assignments and fabric_blocks; several handlers query
//     party_scores / result_votes / staff_assignments / blockchain_records
//     instead, which silently returned no rows (or errored on a missing table).
//     The views map the divergent names onto the canonical tables so those
//     features return real data without changing their query logic.
func initSchemaCompatibility(database *sql.DB) {
	missingTables := []string{
		// Per-PU anomaly scores backing the geospatial "anomaly" heatmap
		// (geospatial_enhanced.go). Populated by the GNN scan (ai_proxy.go).
		`CREATE TABLE IF NOT EXISTS anomaly_scores (
			id SERIAL PRIMARY KEY,
			polling_unit_code TEXT NOT NULL,
			election_id INTEGER NOT NULL,
			anomaly_score DOUBLE PRECISION NOT NULL DEFAULT 0,
			method TEXT DEFAULT '',
			flagged BOOLEAN DEFAULT FALSE,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (polling_unit_code, election_id)
		)`,
		// Queue of vendor/logistics payments when Mojaloop is unavailable
		// (mw_workflows.go ProcessMaterialPayment).
		`CREATE TABLE IF NOT EXISTS payment_queue (
			id SERIAL PRIMARY KEY,
			election_id INTEGER NOT NULL,
			recipient_id TEXT NOT NULL,
			amount NUMERIC(20,4) NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT 'NGN',
			status TEXT NOT NULL DEFAULT 'queued',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		// Turnout/completion predictions emitted by the analytics pipeline
		// (platform_enhancements.go).
		`CREATE TABLE IF NOT EXISTS predictions (
			id SERIAL PRIMARY KEY,
			election_id INTEGER NOT NULL,
			turnout DOUBLE PRECISION DEFAULT 0,
			completion_pct DOUBLE PRECISION DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, ddl := range missingTables {
		if _, err := database.Exec(ddl); err != nil {
			log.Warn().Err(err).Msg("initSchemaCompatibility: create table")
		}
	}

	compatViews := []string{
		// party_scores -> result_party_scores (platform_enhancements.go).
		`CREATE OR REPLACE VIEW party_scores AS
			SELECT id, result_id, party_code, votes FROM result_party_scores`,
		// result_votes -> result_party_scores, exposing party_id via parties.code
		// for the join in sms_ussd.go (ai_proxy.go also joins on result_id).
		`CREATE OR REPLACE VIEW result_votes AS
			SELECT rps.id, rps.result_id, rps.party_code, rps.votes, p.id AS party_id
			FROM result_party_scores rps
			LEFT JOIN parties p ON p.code = rps.party_code`,
		// staff_assignments -> election_staff_assignments (election_fsm.go,
		// mw_workflows.go count staffing readiness).
		`CREATE OR REPLACE VIEW staff_assignments AS
			SELECT * FROM election_staff_assignments`,
		// blockchain_records -> fabric_blocks (platform_improvements.go verifies a
		// certificate's data_hash against the committed block hash).
		`CREATE OR REPLACE VIEW blockchain_records AS
			SELECT block_hash, data_hash, block_number, channel_id, prev_hash, tx_count
			FROM fabric_blocks`,
	}
	for _, ddl := range compatViews {
		if _, err := database.Exec(ddl); err != nil {
			log.Warn().Err(err).Msg("initSchemaCompatibility: create view")
		}
	}
	log.Info().Msg("Schema compatibility views + missing tables ensured")
}

// persistAnomalyScores upserts per-polling-unit anomaly scores produced by the
// GNN scan so the geospatial anomaly heatmap reflects real detection output.
func persistAnomalyScores(electionID int, scored []M) {
	for _, s := range scored {
		puCode, _ := s["polling_unit_code"].(string)
		if puCode == "" {
			continue
		}
		score, _ := s["anomaly_score"].(float64)
		method, _ := s["method"].(string)
		if method == "" {
			method = "gnn"
		}
		dbExecLog("anomaly_scores", `INSERT INTO anomaly_scores
			(polling_unit_code, election_id, anomaly_score, method, flagged, updated_at)
			VALUES (?,?,?,?,TRUE,CURRENT_TIMESTAMP)
			ON CONFLICT (polling_unit_code, election_id)
			DO UPDATE SET anomaly_score = EXCLUDED.anomaly_score,
			              method = EXCLUDED.method,
			              flagged = TRUE,
			              updated_at = CURRENT_TIMESTAMP`,
			puCode, electionID, score, method)
	}
}
