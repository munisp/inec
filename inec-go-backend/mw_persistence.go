package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
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
	log.Println("Middleware persistence tables initialized")
}

// Cache operations backed by mw_cache
func persistCacheSet(ctx context.Context, key, value string, ttlSeconds int) error {
	var expiresAt interface{}
	if ttlSeconds > 0 {
		expiresAt = time.Now().Add(time.Duration(ttlSeconds) * time.Second)
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO mw_cache (key, value, ttl_seconds, expires_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, ttl_seconds=excluded.ttl_seconds, expires_at=excluded.expires_at, created_at=CURRENT_TIMESTAMP`,
		key, value, ttlSeconds, expiresAt)
	return err
}

func persistCacheGet(ctx context.Context, key string) (string, bool) {
	var val string
	err := db.QueryRowContext(ctx,
		`SELECT value FROM mw_cache WHERE key=? AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)`, key).Scan(&val)
	if err != nil {
		return "", false
	}
	return val, true
}

func persistCacheDel(ctx context.Context, key string) {
	db.ExecContext(ctx, `DELETE FROM mw_cache WHERE key=?`, key)
}

// Event stream operations backed by mw_events
func persistEventPublish(ctx context.Context, topic, key, value string, headers map[string]string) error {
	headersJSON, _ := json.Marshal(headers)
	var maxOffset int
	db.QueryRowContext(ctx, `SELECT COALESCE(MAX(offset_id), 0) FROM mw_events WHERE topic=?`, topic).Scan(&maxOffset)
	_, err := db.ExecContext(ctx,
		`INSERT INTO mw_events (topic, key, value, headers, offset_id) VALUES (?, ?, ?, ?, ?)`,
		topic, key, value, string(headersJSON), maxOffset+1)
	return err
}

func persistEventConsume(ctx context.Context, topic string, fromOffset, limit int) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, topic, key, value, headers, offset_id, created_at FROM mw_events
		 WHERE topic=? AND offset_id > ? ORDER BY offset_id LIMIT ?`,
		topic, fromOffset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var id, offsetID int
		var t, k, v, h string
		var createdAt time.Time
		if err := rows.Scan(&id, &t, &k, &v, &h, &offsetID, &createdAt); err != nil {
			continue
		}
		results = append(results, map[string]interface{}{
			"id": id, "topic": t, "key": k, "value": v,
			"headers": h, "offset": offsetID, "timestamp": createdAt,
		})
	}
	return results, nil
}

// State store operations backed by mw_state
func persistStateSet(ctx context.Context, store, key, value string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO mw_state (store_name, key, value) VALUES (?, ?, ?)
		 ON CONFLICT(store_name, key) DO UPDATE SET value=excluded.value, version=version+1, updated_at=CURRENT_TIMESTAMP`,
		store, key, value)
	return err
}

func persistStateGet(ctx context.Context, store, key string) (string, bool) {
	var val string
	err := db.QueryRowContext(ctx, `SELECT value FROM mw_state WHERE store_name=? AND key=?`, store, key).Scan(&val)
	if err != nil {
		return "", false
	}
	return val, true
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
