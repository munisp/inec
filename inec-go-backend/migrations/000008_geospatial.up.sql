-- Geospatial: landmarks, tracking, geofences, crowd
-- Geometry columns and GiST indexes are applied in exception-tolerant blocks
-- so the migration degrades gracefully where PostGIS is unavailable (dev/CI).

CREATE TABLE IF NOT EXISTS landmarks (
    id SERIAL PRIMARY KEY,
    name text NOT NULL,
    category text NOT NULL,
    latitude real NOT NULL,
    longitude real NOT NULL,
    state_code text,
    lga_code text,
    address text,
    description text,
    icon text DEFAULT 'marker'::text,
    importance integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_landmarks_category ON landmarks USING btree (category);
CREATE INDEX IF NOT EXISTS idx_landmarks_state ON landmarks USING btree (state_code);

DO $$
BEGIN
    ALTER TABLE landmarks ADD COLUMN IF NOT EXISTS geom public.geometry(Point,4326);
    CREATE INDEX IF NOT EXISTS idx_landmarks_geom ON landmarks USING gist (geom);
EXCEPTION WHEN OTHERS THEN
    RAISE WARNING 'landmarks.geom skipped: %', SQLERRM;
END $$;

CREATE TABLE IF NOT EXISTS official_tracking (
    staff_id text NOT NULL,
    role text DEFAULT 'field_officer'::text NOT NULL,
    latitude real NOT NULL,
    longitude real NOT NULL,
    pu_code text,
    activity text DEFAULT 'patrol'::text,
    battery_pct integer DEFAULT 100,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_official_tracking_role ON official_tracking USING btree (role);
CREATE INDEX IF NOT EXISTS idx_official_tracking_updated ON official_tracking USING btree (updated_at DESC);

CREATE TABLE IF NOT EXISTS official_tracking_history (
    id bigint NOT NULL,
    staff_id text NOT NULL,
    role text NOT NULL,
    latitude double precision NOT NULL,
    longitude double precision NOT NULL,
    pu_code text,
    activity text,
    battery_pct integer DEFAULT 100,
    speed_kmh double precision DEFAULT 0,
    heading double precision DEFAULT 0,
    accuracy_m double precision DEFAULT 0,
    recorded_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tracking_hist_staff ON official_tracking_history USING btree (staff_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_tracking_hist_time ON official_tracking_history USING btree (recorded_at DESC);

DO $$
BEGIN
    ALTER TABLE official_tracking_history ADD COLUMN IF NOT EXISTS geom public.geometry(Point,4326);
    CREATE INDEX IF NOT EXISTS idx_tracking_hist_geom ON official_tracking_history USING gist (geom);
EXCEPTION WHEN OTHERS THEN
    RAISE WARNING 'official_tracking_history.geom skipped: %', SQLERRM;
END $$;

CREATE TABLE IF NOT EXISTS crowd_density (
    id SERIAL PRIMARY KEY,
    pu_code text NOT NULL,
    latitude real,
    longitude real,
    head_count integer DEFAULT 0,
    density_level text DEFAULT 'moderate'::text,
    queue_length integer DEFAULT 0,
    wait_time_min integer DEFAULT 0,
    notes text,
    reporter_id text,
    reported_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_crowd_density_level ON crowd_density USING btree (density_level);
CREATE INDEX IF NOT EXISTS idx_crowd_density_pu ON crowd_density USING btree (pu_code);
CREATE INDEX IF NOT EXISTS idx_crowd_density_reported ON crowd_density USING btree (reported_at DESC);

CREATE TABLE IF NOT EXISTS crowd_alerts (
    id SERIAL PRIMARY KEY,
    pu_code text NOT NULL,
    alert_type text NOT NULL,
    severity text DEFAULT 'warning'::text,
    head_count integer,
    density_level text,
    message text,
    acknowledged boolean DEFAULT false,
    acknowledged_by text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_crowd_alerts_created ON crowd_alerts USING btree (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_crowd_alerts_severity ON crowd_alerts USING btree (severity);

CREATE TABLE IF NOT EXISTS geo_analytics_cache (
    id text NOT NULL,
    data jsonb NOT NULL,
    computed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone
);

CREATE TABLE IF NOT EXISTS geo_events (
    id SERIAL PRIMARY KEY,
    polling_unit_code text NOT NULL,
    event_type text NOT NULL,
    latitude real,
    longitude real,
    payload jsonb,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_geo_events_created ON geo_events USING btree (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_geo_events_pu ON geo_events USING btree (polling_unit_code);

CREATE TABLE IF NOT EXISTS geofence_attestations (
    id SERIAL PRIMARY KEY,
    staff_id text NOT NULL,
    pu_code text NOT NULL,
    latitude double precision NOT NULL,
    longitude double precision NOT NULL,
    within_geofence boolean NOT NULL,
    distance_m double precision,
    signature_hash text NOT NULL,
    blockchain_tx text,
    attested_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_geofence_att_staff ON geofence_attestations USING btree (staff_id, attested_at DESC);

CREATE TABLE IF NOT EXISTS geofence_zones (
    id SERIAL PRIMARY KEY,
    pu_code text NOT NULL,
    center_lat double precision NOT NULL,
    center_lng double precision NOT NULL,
    radius_m double precision DEFAULT 500,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_geofence_zones_pu ON geofence_zones USING btree (pu_code);

DO $$
BEGIN
    ALTER TABLE geofence_zones ADD COLUMN IF NOT EXISTS geom public.geometry(Polygon,4326);
    CREATE INDEX IF NOT EXISTS idx_geofence_zones_geom ON geofence_zones USING gist (geom);
EXCEPTION WHEN OTHERS THEN
    RAISE WARNING 'geofence_zones.geom skipped: %', SQLERRM;
END $$;

CREATE TABLE IF NOT EXISTS geofenced_submissions (
    id SERIAL PRIMARY KEY,
    result_id integer,
    officer_lat real NOT NULL,
    officer_lng real NOT NULL,
    pu_lat real,
    pu_lng real,
    distance_meters real,
    within_boundary integer DEFAULT 0,
    override_by integer,
    override_reason text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS incident_locations (
    id SERIAL PRIMARY KEY,
    incident_id integer,
    latitude double precision NOT NULL,
    longitude double precision NOT NULL,
    severity text DEFAULT 'medium'::text,
    incident_type text,
    description text,
    resolved boolean DEFAULT false,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

DO $$
BEGIN
    ALTER TABLE incident_locations ADD COLUMN IF NOT EXISTS geom public.geometry(Point,4326);
    CREATE INDEX IF NOT EXISTS idx_incident_loc_geom ON incident_locations USING gist (geom);
EXCEPTION WHEN OTHERS THEN
    RAISE WARNING 'incident_locations.geom skipped: %', SQLERRM;
END $$;

CREATE TABLE IF NOT EXISTS pu_photos (
    id SERIAL PRIMARY KEY,
    pu_code text NOT NULL,
    photo_url text NOT NULL,
    caption text,
    photo_type text DEFAULT 'verification'::text,
    latitude double precision,
    longitude double precision,
    uploaded_by text,
    verified boolean DEFAULT false,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pu_photos_code ON pu_photos USING btree (pu_code);

DO $$
BEGIN
    ALTER TABLE pu_photos ADD COLUMN IF NOT EXISTS geom public.geometry(Point,4326);
    CREATE INDEX IF NOT EXISTS idx_pu_photos_geom ON pu_photos USING gist (geom);
EXCEPTION WHEN OTHERS THEN
    RAISE WARNING 'pu_photos.geom skipped: %', SQLERRM;
END $$;
