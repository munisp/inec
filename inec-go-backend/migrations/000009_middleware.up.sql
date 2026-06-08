-- Middleware persistence: event bus, state, cache, ledger

CREATE TABLE IF NOT EXISTS mw_state (
    store_name text NOT NULL,
    key text NOT NULL,
    value text NOT NULL,
    version integer DEFAULT 1,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mw_cache (
    key text NOT NULL,
    value text NOT NULL,
    ttl_seconds integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone
);

CREATE INDEX IF NOT EXISTS idx_mw_cache_expires ON mw_cache USING btree (expires_at);

CREATE TABLE IF NOT EXISTS mw_events (
    id integer NOT NULL,
    topic text NOT NULL,
    key text,
    value text NOT NULL,
    headers text,
    partition_id integer DEFAULT 0,
    offset_id integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_events_offset ON mw_events USING btree (topic, partition_id, offset_id);
CREATE INDEX IF NOT EXISTS idx_mw_events_topic ON mw_events USING btree (topic, created_at);

CREATE TABLE IF NOT EXISTS mw_pubsub (
    id integer NOT NULL,
    channel text NOT NULL,
    message text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_pubsub_channel ON mw_pubsub USING btree (channel, created_at);

CREATE TABLE IF NOT EXISTS mw_streams (
    id integer NOT NULL,
    topic text NOT NULL,
    key text,
    value text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_streams_topic ON mw_streams USING btree (topic, created_at);

CREATE TABLE IF NOT EXISTS mw_consumer_offsets (
    consumer_group text NOT NULL,
    topic text NOT NULL,
    partition_id integer DEFAULT 0 NOT NULL,
    offset_id integer DEFAULT 0,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mw_search_index (
    id integer NOT NULL,
    index_name text NOT NULL,
    doc_id text NOT NULL,
    body text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_search_index ON mw_search_index USING btree (index_name, doc_id);

CREATE TABLE IF NOT EXISTS mw_workflows (
    id text NOT NULL,
    workflow_type text NOT NULL,
    status text DEFAULT 'running'::text,
    input text,
    result text,
    error_msg text,
    started_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    completed_at timestamp without time zone
);

CREATE TABLE IF NOT EXISTS mw_waf_events (
    id integer NOT NULL,
    request_id text,
    source_ip text,
    method text,
    path text,
    rule_id text,
    action text DEFAULT 'allow'::text,
    threat_level text DEFAULT 'none'::text,
    details text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_waf_created ON mw_waf_events USING btree (created_at);

CREATE TABLE IF NOT EXISTS mw_circuit_breaker_log (
    id integer NOT NULL,
    service text NOT NULL,
    state text NOT NULL,
    failures integer DEFAULT 0,
    details text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mw_ledger_accounts (
    id text NOT NULL,
    debits_pending integer DEFAULT 0,
    debits_posted integer DEFAULT 0,
    credits_pending integer DEFAULT 0,
    credits_posted integer DEFAULT 0,
    ledger integer DEFAULT 1,
    code integer DEFAULT 0,
    flags integer DEFAULT 0,
    user_data text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mw_ledger_transfers (
    id text NOT NULL,
    debit_account_id text NOT NULL,
    credit_account_id text NOT NULL,
    amount integer NOT NULL,
    ledger integer DEFAULT 1,
    code integer DEFAULT 0,
    flags integer DEFAULT 0,
    pending_id text,
    user_data text,
    status text DEFAULT 'posted'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mw_mojaloop_transactions (
    id text NOT NULL,
    payer_fsp text NOT NULL,
    payee_fsp text NOT NULL,
    amount real NOT NULL,
    currency text DEFAULT 'NGN'::text,
    phase text DEFAULT 'discovery'::text,
    quote_id text,
    transfer_id text,
    settlement_id text,
    ilp_packet text,
    condition text,
    fulfilment text,
    error_info text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mw_mojaloop_phase ON mw_mojaloop_transactions USING btree (phase);

CREATE TABLE IF NOT EXISTS event_bus (
    id integer NOT NULL,
    topic text NOT NULL,
    event_key text,
    payload text NOT NULL,
    offset_id integer DEFAULT 0 NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_event_bus_topic ON event_bus USING btree (topic, offset_id);

CREATE TABLE IF NOT EXISTS event_bus_topics (
    topic text NOT NULL,
    partitions integer DEFAULT 1,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


