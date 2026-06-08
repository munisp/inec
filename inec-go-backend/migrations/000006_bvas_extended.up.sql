-- BVAS extended: heartbeats, sync, capabilities, location

CREATE TABLE IF NOT EXISTS bvas_accreditations (
    id integer NOT NULL,
    device_id text NOT NULL,
    election_id integer NOT NULL,
    polling_unit_code text NOT NULL,
    voter_pvc_hash text NOT NULL,
    biometric_match integer DEFAULT 0 NOT NULL,
    pvc_verified integer DEFAULT 0 NOT NULL,
    method text DEFAULT 'biometric'::text NOT NULL,
    accredited_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    synced_at timestamp without time zone,
    CONSTRAINT bvas_accreditations_method_check CHECK ((method = ANY (ARRAY['biometric'::text, 'manual'::text, 'override'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bvas_acc_device ON bvas_accreditations USING btree (device_id);
CREATE INDEX IF NOT EXISTS idx_bvas_acc_pu ON bvas_accreditations USING btree (polling_unit_code, election_id);

CREATE TABLE IF NOT EXISTS bvas_device_capabilities (
    id integer NOT NULL,
    device_id text NOT NULL,
    firmware_version text NOT NULL,
    supported_modalities text DEFAULT 'fingerprint'::text NOT NULL,
    fingerprint_sensor text,
    fingerprint_fap_level text DEFAULT 'FAP30'::text,
    camera_resolution text,
    iris_sensor_type text,
    nfc_capable integer DEFAULT 0,
    secure_element text,
    tls_version text DEFAULT 'TLS1.3'::text,
    max_template_size integer DEFAULT 0,
    capture_quality_threshold real DEFAULT 0.7,
    last_calibrated_at timestamp without time zone,
    registered_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    status text DEFAULT 'active'::text,
    CONSTRAINT bvas_device_capabilities_status_check CHECK ((status = ANY (ARRAY['active'::text, 'maintenance'::text, 'decommissioned'::text])))
);

CREATE TABLE IF NOT EXISTS bvas_heartbeats (
    id integer NOT NULL,
    device_id text NOT NULL,
    battery_level integer,
    signal_strength integer,
    gps_latitude real,
    gps_longitude real,
    sync_queue_size integer DEFAULT 0,
    firmware_version text,
    uptime_seconds integer,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bvas_heartbeat_device ON bvas_heartbeats USING btree (device_id, "timestamp");

CREATE TABLE IF NOT EXISTS bvas_location_logs (
    id integer NOT NULL,
    bvas_serial text NOT NULL,
    polling_unit_code text NOT NULL,
    latitude real NOT NULL,
    longitude real NOT NULL,
    distance_from_pu_m real,
    within_geofence boolean,
    logged_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bvas_loc_pu ON bvas_location_logs USING btree (polling_unit_code);
CREATE INDEX IF NOT EXISTS idx_bvas_loc_serial ON bvas_location_logs USING btree (bvas_serial);

CREATE TABLE IF NOT EXISTS bvas_sync_queue (
    id integer NOT NULL,
    device_id text NOT NULL,
    sync_type text NOT NULL,
    payload text NOT NULL,
    priority integer DEFAULT 5,
    status text DEFAULT 'queued'::text NOT NULL,
    conflict_resolution text,
    conflict_data text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    synced_at timestamp without time zone,
    retry_count integer DEFAULT 0,
    max_retries integer DEFAULT 5,
    CONSTRAINT bvas_sync_queue_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'syncing'::text, 'synced'::text, 'conflict'::text, 'failed'::text, 'resolved'::text]))),
    CONSTRAINT bvas_sync_queue_sync_type_check CHECK ((sync_type = ANY (ARRAY['accreditation'::text, 'result'::text, 'heartbeat'::text, 'config'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bvas_sync_status ON bvas_sync_queue USING btree (status, device_id);


