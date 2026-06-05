package main

import (
	"context"
	"database/sql"
	"github.com/rs/zerolog/log"
	"time"
)

// initMiddlewareTables creates persistence tables for middleware state.
// Uses SERIAL PRIMARY KEY (PostgreSQL syntax) — convertDDLForSQLite handles the SQLite conversion.
func initMiddlewareTables(database *sql.DB) {
	schema := `
	CREATE TABLE IF NOT EXISTS mw_cache (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		ttl_seconds INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS mw_events (
		id SERIAL PRIMARY KEY,
		topic TEXT NOT NULL,
		key TEXT,
		value TEXT NOT NULL,
		headers TEXT,
		partition_id INTEGER DEFAULT 0,
		offset_id INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS mw_consumer_offsets (
		consumer_group TEXT NOT NULL,
		topic TEXT NOT NULL,
		partition_id INTEGER DEFAULT 0,
		offset_id INTEGER DEFAULT 0,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (consumer_group, topic, partition_id)
	);
	CREATE TABLE IF NOT EXISTS mw_ledger_accounts (
		id TEXT PRIMARY KEY,
		debits_pending INTEGER DEFAULT 0,
		debits_posted INTEGER DEFAULT 0,
		credits_pending INTEGER DEFAULT 0,
		credits_posted INTEGER DEFAULT 0,
		ledger INTEGER DEFAULT 1,
		code INTEGER DEFAULT 0,
		flags INTEGER DEFAULT 0,
		user_data TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS mw_ledger_transfers (
		id TEXT PRIMARY KEY,
		debit_account_id TEXT NOT NULL,
		credit_account_id TEXT NOT NULL,
		amount INTEGER NOT NULL,
		ledger INTEGER DEFAULT 1,
		code INTEGER DEFAULT 0,
		flags INTEGER DEFAULT 0,
		pending_id TEXT,
		user_data TEXT,
		status TEXT DEFAULT 'posted',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS mw_workflows (
		id TEXT PRIMARY KEY,
		workflow_type TEXT NOT NULL,
		status TEXT DEFAULT 'running',
		input TEXT,
		result TEXT,
		error_msg TEXT,
		started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS mw_state (
		store_name TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		version INTEGER DEFAULT 1,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (store_name, key)
	);
	CREATE TABLE IF NOT EXISTS mw_streams (
		id SERIAL PRIMARY KEY,
		topic TEXT NOT NULL,
		key TEXT,
		value TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS mw_pubsub (
		id SERIAL PRIMARY KEY,
		channel TEXT NOT NULL,
		message TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS mw_mojaloop_transactions (
		id TEXT PRIMARY KEY,
		payer_fsp TEXT NOT NULL,
		payee_fsp TEXT NOT NULL,
		amount REAL NOT NULL,
		currency TEXT DEFAULT 'NGN',
		phase TEXT DEFAULT 'discovery',
		quote_id TEXT,
		transfer_id TEXT,
		settlement_id TEXT,
		ilp_packet TEXT,
		condition TEXT,
		fulfilment TEXT,
		error_info TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS mw_search_index (
		id SERIAL PRIMARY KEY,
		index_name TEXT NOT NULL,
		doc_id TEXT NOT NULL,
		body TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(index_name, doc_id)
	);
	CREATE TABLE IF NOT EXISTS mw_waf_events (
		id SERIAL PRIMARY KEY,
		request_id TEXT,
		source_ip TEXT,
		method TEXT,
		path TEXT,
		rule_id TEXT,
		action TEXT DEFAULT 'allow',
		threat_level TEXT DEFAULT 'none',
		details TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS waf_blocklist (
		id SERIAL PRIMARY KEY,
		ip_address TEXT UNIQUE NOT NULL,
		reason TEXT,
		blocked_at TEXT
	);
	CREATE TABLE IF NOT EXISTS mw_circuit_breaker_log (
		id SERIAL PRIMARY KEY,
		service TEXT NOT NULL,
		state TEXT NOT NULL,
		failures INTEGER DEFAULT 0,
		details TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_mw_cache_expires ON mw_cache(expires_at);
	CREATE INDEX IF NOT EXISTS idx_mw_events_topic ON mw_events(topic, created_at);
	CREATE INDEX IF NOT EXISTS idx_mw_events_offset ON mw_events(topic, partition_id, offset_id);
	CREATE INDEX IF NOT EXISTS idx_mw_streams_topic ON mw_streams(topic, created_at);
	CREATE INDEX IF NOT EXISTS idx_mw_search_index ON mw_search_index(index_name, doc_id);
	CREATE INDEX IF NOT EXISTS idx_mw_waf_created ON mw_waf_events(created_at);
	CREATE INDEX IF NOT EXISTS idx_mw_pubsub_channel ON mw_pubsub(channel, created_at);
	CREATE INDEX IF NOT EXISTS idx_mw_mojaloop_phase ON mw_mojaloop_transactions(phase);
	`
	execMulti(database, schema)
	log.Info().Msg("Middleware persistence tables initialized")
}
// Cleanup expired cache entries (run periodically)
func cleanupExpiredCache() {
	for {
		time.Sleep(60 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		db.ExecContext(ctx, `DELETE FROM mw_cache WHERE expires_at IS NOT NULL AND expires_at < CURRENT_TIMESTAMP`)
		cancel()
	}
}
