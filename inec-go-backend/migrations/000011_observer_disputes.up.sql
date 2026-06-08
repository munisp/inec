-- Observer monitoring and dispute resolution

CREATE TABLE IF NOT EXISTS observer_alert_rules (
    id integer NOT NULL,
    user_id integer NOT NULL,
    party_code text,
    state_code text,
    lga_code text,
    alert_type text DEFAULT 'result_submitted'::text NOT NULL,
    is_active integer DEFAULT 1 NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS observer_check_ins (
    id integer NOT NULL,
    observer_id integer NOT NULL,
    polling_unit_code text NOT NULL,
    latitude real,
    longitude real,
    device_info text,
    within_geofence boolean,
    checked_in_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_observer_checkins_observer ON observer_check_ins USING btree (observer_id);

CREATE TABLE IF NOT EXISTS observer_photo_verifications (
    id integer NOT NULL,
    observer_id integer,
    pu_code text,
    photo_hash text,
    gps_lat real,
    gps_lng real,
    timestamp_watermark text,
    consensus_score real DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS observer_reports (
    id integer NOT NULL,
    observer_id integer NOT NULL,
    polling_unit_code text NOT NULL,
    election_id integer NOT NULL,
    report_type text DEFAULT 'observation'::text NOT NULL,
    description text,
    photo_url text,
    latitude real,
    longitude real,
    status text DEFAULT 'pending'::text NOT NULL,
    review_notes text,
    reviewed_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_observer_reports_election ON observer_reports USING btree (election_id);
CREATE INDEX IF NOT EXISTS idx_observer_reports_pu ON observer_reports USING btree (polling_unit_code);

CREATE TABLE IF NOT EXISTS disputes (
    id integer NOT NULL,
    election_id integer NOT NULL,
    polling_unit_code text,
    filed_by text NOT NULL,
    party text,
    category text NOT NULL,
    description text NOT NULL,
    evidence text,
    status text DEFAULT 'filed'::text NOT NULL,
    assigned_to text,
    resolution text,
    resolved_by text,
    filed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    resolved_at timestamp without time zone,
    priority text DEFAULT 'medium'::text
);

CREATE TABLE IF NOT EXISTS dispute_comments (
    id integer NOT NULL,
    dispute_id integer NOT NULL,
    author text NOT NULL,
    content text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


