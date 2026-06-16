-- API keys, webhooks, portal, push notifications

CREATE TABLE IF NOT EXISTS api_keys (
    id SERIAL PRIMARY KEY,
    key_hash text NOT NULL,
    name text NOT NULL,
    owner text NOT NULL,
    permissions text DEFAULT 'read'::text NOT NULL,
    rate_limit integer DEFAULT 100 NOT NULL,
    is_active integer DEFAULT 1 NOT NULL,
    last_used_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys USING btree (key_hash);

CREATE TABLE IF NOT EXISTS api_usage (
    id SERIAL PRIMARY KEY,
    api_key_id integer,
    endpoint text NOT NULL,
    method text NOT NULL,
    status_code integer,
    response_ms real,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_api_usage_key ON api_usage USING btree (api_key_id);

CREATE TABLE IF NOT EXISTS api_key_metadata (
    id SERIAL PRIMARY KEY,
    key_hash text NOT NULL,
    name text NOT NULL,
    owner_id integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone,
    rotated_from text,
    is_active boolean DEFAULT true,
    last_used_at timestamp without time zone,
    usage_count integer DEFAULT 0
);

CREATE TABLE IF NOT EXISTS dead_letter_queue (
    id text NOT NULL,
    job_id text NOT NULL,
    job_type text NOT NULL,
    error_message text NOT NULL,
    payload text NOT NULL,
    failed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    reprocessed integer DEFAULT 0,
    reprocessed_at timestamp without time zone
);

CREATE INDEX IF NOT EXISTS idx_dlq_reprocessed ON dead_letter_queue USING btree (reprocessed);

CREATE TABLE IF NOT EXISTS ingestion_jobs (
    id text NOT NULL,
    job_type text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    payload text NOT NULL,
    idempotency_key text,
    retries integer DEFAULT 0,
    max_retries integer DEFAULT 3,
    error_message text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    processed_at timestamp without time zone,
    latency_ms real,
    CONSTRAINT ingestion_jobs_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'in_progress'::text, 'completed'::text, 'failed'::text, 'dead_letter'::text])))
);

CREATE INDEX IF NOT EXISTS idx_ingestion_idem ON ingestion_jobs USING btree (idempotency_key);
CREATE INDEX IF NOT EXISTS idx_ingestion_status ON ingestion_jobs USING btree (status);

CREATE TABLE IF NOT EXISTS offline_sync_queue (
    id SERIAL PRIMARY KEY,
    device_id text NOT NULL,
    sync_type text NOT NULL,
    payload text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    synced_at timestamp without time zone,
    retries integer DEFAULT 0,
    CONSTRAINT offline_sync_queue_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'syncing'::text, 'synced'::text, 'failed'::text]))),
    CONSTRAINT offline_sync_queue_sync_type_check CHECK ((sync_type = ANY (ARRAY['result'::text, 'accreditation'::text, 'incident'::text])))
);

CREATE INDEX IF NOT EXISTS idx_offline_status ON offline_sync_queue USING btree (status);

CREATE TABLE IF NOT EXISTS portal_connections (
    id SERIAL PRIMARY KEY,
    portal_name text NOT NULL,
    portal_type text NOT NULL,
    base_url text NOT NULL,
    api_key_hash text,
    status text DEFAULT 'active'::text NOT NULL,
    last_sync_at timestamp without time zone,
    sync_interval_seconds integer DEFAULT 300,
    webhook_url text,
    metadata text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT portal_connections_portal_type_check CHECK ((portal_type = ANY (ARRAY['irev'::text, 'icnp'::text, 'press'::text, 'croms'::text, 'bvas_portal'::text, 'custom'::text]))),
    CONSTRAINT portal_connections_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'error'::text, 'maintenance'::text])))
);

CREATE TABLE IF NOT EXISTS portal_sync_log (
    id SERIAL PRIMARY KEY,
    portal_id integer NOT NULL,
    sync_type text NOT NULL,
    entity_type text NOT NULL,
    records_synced integer DEFAULT 0,
    records_failed integer DEFAULT 0,
    status text DEFAULT 'completed'::text NOT NULL,
    error_message text,
    started_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    completed_at timestamp without time zone,
    CONSTRAINT portal_sync_log_status_check CHECK ((status = ANY (ARRAY['in_progress'::text, 'completed'::text, 'failed'::text, 'partial'::text]))),
    CONSTRAINT portal_sync_log_sync_type_check CHECK ((sync_type = ANY (ARRAY['push'::text, 'pull'::text, 'webhook'::text])))
);

CREATE INDEX IF NOT EXISTS idx_portal_sync ON portal_sync_log USING btree (portal_id, started_at);

CREATE TABLE IF NOT EXISTS portal_webhooks (
    id SERIAL PRIMARY KEY,
    portal_id integer NOT NULL,
    event_type text NOT NULL,
    payload text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    retry_count integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    delivered_at timestamp without time zone,
    CONSTRAINT portal_webhooks_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'delivered'::text, 'failed'::text, 'retrying'::text])))
);

CREATE TABLE IF NOT EXISTS push_devices (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    device_token text NOT NULL,
    platform text DEFAULT 'android'::text NOT NULL,
    app_version text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    last_active timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    is_active integer DEFAULT 1
);

CREATE TABLE IF NOT EXISTS push_notifications (
    id SERIAL PRIMARY KEY,
    target_type text NOT NULL,
    target_value text,
    title text NOT NULL,
    body text NOT NULL,
    notification_type text DEFAULT 'info'::text NOT NULL,
    sent_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    read_count integer DEFAULT 0,
    total_recipients integer DEFAULT 0,
    CONSTRAINT push_notifications_notification_type_check CHECK ((notification_type = ANY (ARRAY['info'::text, 'alert'::text, 'update'::text, 'emergency'::text]))),
    CONSTRAINT push_notifications_target_type_check CHECK ((target_type = ANY (ARRAY['all'::text, 'stakeholder_type'::text, 'individual'::text, 'area'::text])))
);

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id SERIAL PRIMARY KEY,
    url text NOT NULL,
    events text NOT NULL,
    secret text NOT NULL,
    created_by text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    is_active integer DEFAULT 1
);


