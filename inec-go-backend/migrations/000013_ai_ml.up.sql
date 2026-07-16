-- AI/ML: predictions, document AI, KYC, sentiment analysis

CREATE TABLE IF NOT EXISTS ai_predictions (
    id SERIAL PRIMARY KEY,
    prediction_type text NOT NULL,
    target_area text NOT NULL,
    target_level text NOT NULL,
    predicted_value real NOT NULL,
    confidence real NOT NULL,
    model_name text NOT NULL,
    features_used text,
    election_id integer,
    predicted_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ai_predictions_prediction_type_check CHECK ((prediction_type = ANY (ARRAY['turnout'::text, 'resource'::text, 'security_threat'::text, 'sentiment'::text, 'misinformation'::text]))),
    CONSTRAINT ai_predictions_target_level_check CHECK ((target_level = ANY (ARRAY['national'::text, 'state'::text, 'lga'::text, 'ward'::text, 'polling_unit'::text])))
);

CREATE INDEX IF NOT EXISTS idx_ai_pred ON ai_predictions USING btree (prediction_type, target_area);

CREATE TABLE IF NOT EXISTS cv_monitoring (
    id SERIAL PRIMARY KEY,
    camera_id text NOT NULL,
    polling_unit_code text,
    event_type text NOT NULL,
    value real,
    description text,
    frame_url text,
    confidence real DEFAULT 0.8,
    detected_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT cv_monitoring_event_type_check CHECK ((event_type = ANY (ARRAY['crowd_size'::text, 'queue_length'::text, 'suspicious_activity'::text, 'equipment_status'::text, 'accessibility_issue'::text])))
);

CREATE TABLE IF NOT EXISTS document_analyses (
    id SERIAL PRIMARY KEY,
    report_id integer,
    analysis_type text DEFAULT 'full'::text NOT NULL,
    ocr_confidence real,
    vlm_tampering_detected integer DEFAULT 0,
    vlm_quality text,
    combined_confidence real,
    requires_review integer DEFAULT 0,
    party_results_json text,
    raw_ocr_text text,
    warnings_json text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS kyb_verifications (
    id SERIAL PRIMARY KEY,
    entity_id integer NOT NULL,
    entity_type text NOT NULL,
    entity_name text NOT NULL,
    registration_number text,
    registration_verified integer DEFAULT 0,
    authorized_signatories text DEFAULT '[]'::text,
    documents_verified integer DEFAULT 0,
    compliance_score real DEFAULT 0,
    risk_level text DEFAULT 'pending'::text,
    status text DEFAULT 'pending'::text,
    reviewed_by integer,
    review_notes text,
    verified_at timestamp without time zone,
    expires_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT kyb_verifications_entity_type_check CHECK ((entity_type = ANY (ARRAY['political_party'::text, 'observer_org'::text, 'media_org'::text, 'ngo'::text, 'inec_partner'::text]))),
    CONSTRAINT kyb_verifications_risk_level_check CHECK ((risk_level = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text, 'pending'::text]))),
    CONSTRAINT kyb_verifications_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'under_review'::text, 'approved'::text, 'rejected'::text, 'suspended'::text, 'expired'::text])))
);

CREATE INDEX IF NOT EXISTS idx_kyb_entity ON kyb_verifications USING btree (entity_id, entity_type);
CREATE INDEX IF NOT EXISTS idx_kyb_status ON kyb_verifications USING btree (status);

CREATE TABLE IF NOT EXISTS kyc_events (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    event_type text NOT NULL,
    trigger_source text NOT NULL,
    details text DEFAULT '{}'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_kyc_events_user ON kyc_events USING btree (user_id, event_type);

CREATE TABLE IF NOT EXISTS kyc_verifications (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    id_type text,
    id_number_hash text,
    identity_match_score real,
    document_verified integer DEFAULT 0,
    face_match_score real,
    liveness_passed integer DEFAULT 0,
    risk_score real,
    checks_json text,
    flags_json text,
    verified_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS media_api_keys (
    id SERIAL PRIMARY KEY,
    api_key text NOT NULL,
    org_name text NOT NULL,
    contact_email text,
    rate_limit integer DEFAULT 600,
    is_active integer DEFAULT 1,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS misinformation_alerts (
    id SERIAL PRIMARY KEY,
    content text NOT NULL,
    source_platform text,
    source_url text,
    classification text NOT NULL,
    confidence real NOT NULL,
    severity text DEFAULT 'medium'::text NOT NULL,
    reach_estimate integer DEFAULT 0,
    status text DEFAULT 'detected'::text NOT NULL,
    fact_check text,
    detected_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT misinformation_alerts_classification_check CHECK ((classification = ANY (ARRAY['fake_result'::text, 'false_claim'::text, 'manipulated_media'::text, 'impersonation'::text, 'incitement'::text, 'other'::text]))),
    CONSTRAINT misinformation_alerts_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))),
    CONSTRAINT misinformation_alerts_status_check CHECK ((status = ANY (ARRAY['detected'::text, 'verified'::text, 'debunked'::text, 'monitoring'::text, 'escalated'::text])))
);

CREATE INDEX IF NOT EXISTS idx_misinfo ON misinformation_alerts USING btree (status, detected_at);

CREATE TABLE IF NOT EXISTS model_metrics (
    id SERIAL PRIMARY KEY,
    model_name text NOT NULL,
    accuracy real NOT NULL,
    latency_ms real,
    sample_count integer,
    evaluated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_model_metrics_name ON model_metrics USING btree (model_name, evaluated_at);

CREATE TABLE IF NOT EXISTS predictive_analytics (
    id SERIAL PRIMARY KEY,
    election_id integer,
    state_code text,
    predicted_turnout real,
    confidence real DEFAULT 0.8,
    model_version text DEFAULT 'v1'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sentiment_analysis (
    id SERIAL PRIMARY KEY,
    source text NOT NULL,
    content_snippet text,
    sentiment text NOT NULL,
    score real NOT NULL,
    topics text,
    location text,
    language text DEFAULT 'en'::text,
    election_id integer,
    analyzed_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT sentiment_analysis_sentiment_check CHECK ((sentiment = ANY (ARRAY['positive'::text, 'negative'::text, 'neutral'::text, 'mixed'::text]))),
    CONSTRAINT sentiment_analysis_source_check CHECK ((source = ANY (ARRAY['twitter'::text, 'facebook'::text, 'news'::text, 'radio'::text, 'whatsapp'::text, 'other'::text])))
);

CREATE INDEX IF NOT EXISTS idx_sentiment ON sentiment_analysis USING btree (sentiment, analyzed_at);

CREATE TABLE IF NOT EXISTS video_analyses (
    id SERIAL PRIMARY KEY,
    report_id integer,
    observer_id integer,
    filename text,
    duration_sec real,
    frame_count integer,
    anomaly_count integer,
    ballot_event_count integer,
    integrity_score real,
    summary text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


