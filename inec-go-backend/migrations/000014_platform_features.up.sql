-- Platform: MFA, SMS/USSD, training, registration

CREATE TABLE IF NOT EXISTS mfa_settings (
    user_id integer NOT NULL,
    totp_enabled integer DEFAULT 0,
    webauthn_enabled integer DEFAULT 0,
    sms_enabled integer DEFAULT 0,
    enforce_on_write integer DEFAULT 1,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mfa_sms_otp (
    id integer NOT NULL,
    user_id integer NOT NULL,
    phone text NOT NULL,
    code text NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    used integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mfa_totp (
    id integer NOT NULL,
    user_id integer NOT NULL,
    secret text NOT NULL,
    verified integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mfa_webauthn (
    id integer NOT NULL,
    user_id integer NOT NULL,
    credential_id text NOT NULL,
    public_key text NOT NULL,
    sign_count integer DEFAULT 0,
    device_name text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sms_delivery_log (
    id integer NOT NULL,
    provider text NOT NULL,
    message_id text,
    phone text NOT NULL,
    message text NOT NULL,
    direction text DEFAULT 'outbound'::text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    cost real DEFAULT 0,
    delivery_report text,
    provider_response text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    delivered_at timestamp without time zone
);

CREATE INDEX IF NOT EXISTS idx_sms_delivery ON sms_delivery_log USING btree (phone, created_at);

CREATE TABLE IF NOT EXISTS sms_verifications (
    id integer NOT NULL,
    phone text NOT NULL,
    polling_unit_code text,
    election_id integer,
    request_type text NOT NULL,
    response_text text,
    channel text DEFAULT 'sms'::text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT sms_verifications_channel_check CHECK ((channel = ANY (ARRAY['sms'::text, 'ussd'::text]))),
    CONSTRAINT sms_verifications_request_type_check CHECK ((request_type = ANY (ARRAY['result'::text, 'status'::text, 'verify'::text])))
);

CREATE INDEX IF NOT EXISTS idx_sms_phone ON sms_verifications USING btree (phone);

CREATE TABLE IF NOT EXISTS ussd_sessions (
    id text NOT NULL,
    phone text NOT NULL,
    stage text DEFAULT 'main_menu'::text NOT NULL,
    data text DEFAULT '{}'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS registration_centers (
    id integer NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    state_code text NOT NULL,
    lga_code text NOT NULL,
    ward_code text NOT NULL,
    address text,
    latitude real,
    longitude real,
    capacity integer DEFAULT 500,
    status text DEFAULT 'active'::text NOT NULL,
    start_date text,
    end_date text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT registration_centers_status_check CHECK ((status = ANY (ARRAY['active'::text, 'closed'::text, 'suspended'::text])))
);

CREATE TABLE IF NOT EXISTS training_certificates (
    id integer NOT NULL,
    enrollment_id integer NOT NULL,
    user_id integer NOT NULL,
    course_id integer NOT NULL,
    certificate_id text NOT NULL,
    blockchain_hash text NOT NULL,
    score integer NOT NULL,
    issued_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp without time zone,
    verification_url text
);

CREATE TABLE IF NOT EXISTS training_courses (
    id integer NOT NULL,
    title text NOT NULL,
    description text,
    course_type text NOT NULL,
    target_role text NOT NULL,
    difficulty text DEFAULT 'beginner'::text NOT NULL,
    duration_minutes integer DEFAULT 60,
    passing_score integer DEFAULT 70,
    modules_count integer DEFAULT 1,
    is_mandatory integer DEFAULT 0,
    is_active integer DEFAULT 1,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT training_courses_course_type_check CHECK ((course_type = ANY (ARRAY['vr_simulation'::text, 'gamified'::text, 'video'::text, 'interactive'::text, 'assessment'::text]))),
    CONSTRAINT training_courses_difficulty_check CHECK ((difficulty = ANY (ARRAY['beginner'::text, 'intermediate'::text, 'advanced'::text, 'expert'::text])))
);

CREATE TABLE IF NOT EXISTS training_enrollments (
    id integer NOT NULL,
    user_id integer NOT NULL,
    course_id integer NOT NULL,
    progress_percent real DEFAULT 0,
    current_module integer DEFAULT 1,
    score integer,
    status text DEFAULT 'enrolled'::text NOT NULL,
    started_at timestamp without time zone,
    completed_at timestamp without time zone,
    certificate_hash text,
    enrolled_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT training_enrollments_status_check CHECK ((status = ANY (ARRAY['enrolled'::text, 'in_progress'::text, 'completed'::text, 'failed'::text, 'expired'::text])))
);

CREATE INDEX IF NOT EXISTS idx_training_enroll ON training_enrollments USING btree (user_id, course_id);

CREATE TABLE IF NOT EXISTS training_vr_scenarios (
    id integer NOT NULL,
    course_id integer NOT NULL,
    scenario_name text NOT NULL,
    scenario_type text NOT NULL,
    description text,
    max_score integer DEFAULT 100,
    avg_completion_time integer,
    difficulty text DEFAULT 'intermediate'::text,
    is_active integer DEFAULT 1,
    CONSTRAINT training_vr_scenarios_scenario_type_check CHECK ((scenario_type = ANY (ARRAY['election_day'::text, 'emergency'::text, 'crowd_control'::text, 'result_collation'::text, 'equipment_setup'::text, 'conflict_resolution'::text])))
);


