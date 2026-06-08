-- Security: encryption keys, HSM, audit events, access policies

CREATE TABLE IF NOT EXISTS active_sessions (
    id integer NOT NULL,
    jti text NOT NULL,
    user_id integer NOT NULL,
    ip_address text,
    user_agent text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone NOT NULL,
    last_activity timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sessions_jti ON active_sessions USING btree (jti);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON active_sessions USING btree (user_id);

CREATE TABLE IF NOT EXISTS data_classification (
    id integer NOT NULL,
    table_name text NOT NULL,
    column_name text NOT NULL,
    classification text NOT NULL,
    encryption_required integer DEFAULT 0,
    pii integer DEFAULT 0,
    retention_days integer DEFAULT 365,
    CONSTRAINT data_classification_classification_check CHECK ((classification = ANY (ARRAY['public'::text, 'internal'::text, 'confidential'::text, 'restricted'::text])))
);

CREATE TABLE IF NOT EXISTS data_encryption_keys (
    id integer NOT NULL,
    key_id text NOT NULL,
    algorithm text DEFAULT 'AES-256-GCM'::text NOT NULL,
    purpose text NOT NULL,
    key_version integer DEFAULT 1,
    status text DEFAULT 'active'::text,
    rotated_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT data_encryption_keys_purpose_check CHECK ((purpose = ANY (ARRAY['pii_encryption'::text, 'biometric_encryption'::text, 'result_signing'::text, 'api_key_encryption'::text, 'backup_encryption'::text]))),
    CONSTRAINT data_encryption_keys_status_check CHECK ((status = ANY (ARRAY['active'::text, 'rotating'::text, 'retired'::text, 'compromised'::text])))
);

CREATE INDEX IF NOT EXISTS idx_dek_purpose ON data_encryption_keys USING btree (purpose, status);

CREATE TABLE IF NOT EXISTS device_keys (
    id integer NOT NULL,
    user_id text NOT NULL,
    device_id text NOT NULL,
    device_secret text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS device_locations (
    id integer NOT NULL,
    device_id text NOT NULL,
    ip_address text,
    latitude real,
    longitude real,
    last_seen timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_device_locations_device ON device_locations USING btree (device_id);

CREATE TABLE IF NOT EXISTS export_audit_log (
    id integer NOT NULL,
    user_id text NOT NULL,
    export_type text,
    format text,
    ip_address text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_export_audit_user ON export_audit_log USING btree (user_id, "timestamp");

CREATE TABLE IF NOT EXISTS hsm_audit (
    id integer NOT NULL,
    operation text NOT NULL,
    key_id text,
    hsm_slot integer,
    success integer DEFAULT 1,
    latency_us integer DEFAULT 0,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS hsm_keys (
    id integer NOT NULL,
    key_id text NOT NULL,
    key_type text DEFAULT 'AES-256-GCM'::text NOT NULL,
    purpose text NOT NULL,
    algorithm text DEFAULT 'AES'::text NOT NULL,
    key_size integer DEFAULT 256 NOT NULL,
    encrypted_material bytea NOT NULL,
    wrapping_key_id text,
    pkcs11_label text,
    pkcs11_id text,
    usage_count integer DEFAULT 0,
    max_usage integer DEFAULT 0,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone,
    last_used_at timestamp without time zone,
    rotation_schedule text DEFAULT 'monthly'::text,
    metadata text DEFAULT '{}'::text
);

CREATE INDEX IF NOT EXISTS idx_hsm_keys_purpose ON hsm_keys USING btree (purpose, status);

CREATE TABLE IF NOT EXISTS hsm_operations (
    id integer NOT NULL,
    operation text NOT NULL,
    key_id text NOT NULL,
    algorithm text,
    input_hash text,
    output_hash text,
    latency_us integer,
    success integer DEFAULT 1 NOT NULL,
    error_detail text,
    actor text,
    ip_address text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_hsm_ops_key ON hsm_operations USING btree (key_id, "timestamp");

CREATE TABLE IF NOT EXISTS hsm_slot_keys (
    slot_id integer NOT NULL,
    key_material text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS privacy_preserving_ops (
    id integer NOT NULL,
    operation_type text NOT NULL,
    encryption_scheme text DEFAULT 'paillier'::text NOT NULL,
    voter_vin text,
    modality text,
    computation_time_ms integer DEFAULT 0,
    template_never_decrypted integer DEFAULT 1,
    result_encrypted integer DEFAULT 1,
    status text DEFAULT 'completed'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS row_access_policies (
    id integer NOT NULL,
    table_name text NOT NULL,
    policy_name text NOT NULL,
    role text NOT NULL,
    condition_column text NOT NULL,
    condition_value text NOT NULL,
    permission text DEFAULT 'read'::text NOT NULL
);

CREATE TABLE IF NOT EXISTS security_audit_events (
    id integer NOT NULL,
    event_type text NOT NULL,
    severity text NOT NULL,
    source text NOT NULL,
    user_id integer,
    ip_address text,
    details text DEFAULT '{}'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT security_audit_events_severity_check CHECK ((severity = ANY (ARRAY['info'::text, 'low'::text, 'medium'::text, 'high'::text, 'critical'::text])))
);

CREATE INDEX IF NOT EXISTS idx_security_events_type ON security_audit_events USING btree (event_type, severity, created_at);

CREATE TABLE IF NOT EXISTS security_threats (
    id integer NOT NULL,
    threat_type text NOT NULL,
    location text NOT NULL,
    latitude real,
    longitude real,
    severity text DEFAULT 'medium'::text NOT NULL,
    confidence real DEFAULT 0.5,
    source text,
    description text,
    affected_pus integer DEFAULT 0,
    status text DEFAULT 'active'::text NOT NULL,
    detected_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    resolved_at timestamp without time zone,
    CONSTRAINT security_threats_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))),
    CONSTRAINT security_threats_status_check CHECK ((status = ANY (ARRAY['active'::text, 'monitoring'::text, 'mitigated'::text, 'resolved'::text, 'false_alarm'::text]))),
    CONSTRAINT security_threats_threat_type_check CHECK ((threat_type = ANY (ARRAY['violence'::text, 'protest'::text, 'road_blockage'::text, 'device_theft'::text, 'cyber_attack'::text, 'impersonation'::text, 'other'::text])))
);

CREATE INDEX IF NOT EXISTS idx_security ON security_threats USING btree (status, severity);


