-- W9b handoff: server-side ledger for GOTV mobile sync conflicts.
-- gotv-svc's InitTables creates the same table in dev; this migration is the
-- PG-deployments-of-record copy. DDL must stay identical to
-- internal/gotv/service.go InitTables.
CREATE TABLE IF NOT EXISTS gotv_sync_conflicts (
	conflict_id TEXT PRIMARY KEY,
	party_id INTEGER NOT NULL,
	volunteer_id TEXT,
	entity_type TEXT NOT NULL,
	local_id TEXT NOT NULL,
	server_id TEXT,
	conflict_reason TEXT,
	client_payload TEXT,
	status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','resolved_client','resolved_server','discarded')),
	resolution_note TEXT,
	resolved_by TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	resolved_at TIMESTAMP,
	UNIQUE (party_id, volunteer_id, entity_type, local_id)
);
CREATE INDEX IF NOT EXISTS idx_gotv_sync_conflicts_party ON gotv_sync_conflicts(party_id, status);
