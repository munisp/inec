-- Biometric: profiles, templates, vault, ABIS, quality

CREATE TABLE IF NOT EXISTS biometric_profiles (
    id integer NOT NULL,
    voter_vin text NOT NULL,
    fingerprint_hash text,
    facial_hash text,
    iris_hash text,
    modalities_enrolled text DEFAULT 'fingerprint'::text NOT NULL,
    quality_score real DEFAULT 0,
    enrollment_device text,
    enrollment_date timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    last_verified_at timestamp without time zone,
    match_count integer DEFAULT 0,
    duplicate_flag integer DEFAULT 0,
    duplicate_matched_vin text,
    status text DEFAULT 'active'::text NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT biometric_profiles_status_check CHECK ((status = ANY (ARRAY['active'::text, 'suspended'::text, 'flagged'::text, 'revoked'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bio_voter ON biometric_profiles USING btree (voter_vin);

CREATE TABLE IF NOT EXISTS biometric_templates (
    id integer NOT NULL,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    template_data bytea NOT NULL,
    template_format text DEFAULT 'ISO_19794'::text NOT NULL,
    encryption_key_id text NOT NULL,
    iv bytea NOT NULL,
    quality_score real DEFAULT 0 NOT NULL,
    nfiq_score integer DEFAULT 0,
    minutiae_count integer DEFAULT 0,
    embedding_dim integer DEFAULT 0,
    iris_code_bits integer DEFAULT 0,
    capture_device text,
    capture_timestamp timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    iso_compliance text DEFAULT 'ISO_19794_2'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT biometric_templates_modality_check CHECK ((modality = ANY (ARRAY['fingerprint'::text, 'facial'::text, 'iris'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bio_tmpl_mod ON biometric_templates USING btree (modality);
CREATE INDEX IF NOT EXISTS idx_bio_tmpl_vin ON biometric_templates USING btree (voter_vin);

CREATE TABLE IF NOT EXISTS biometric_verifications (
    id integer NOT NULL,
    voter_vin text NOT NULL,
    device_id text,
    modality text NOT NULL,
    match_score real NOT NULL,
    threshold real DEFAULT 0.85,
    result text NOT NULL,
    latency_ms integer,
    polling_unit_code text,
    election_id integer,
    verified_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT biometric_verifications_modality_check CHECK ((modality = ANY (ARRAY['fingerprint'::text, 'facial'::text, 'iris'::text, 'multi_modal'::text]))),
    CONSTRAINT biometric_verifications_result_check CHECK ((result = ANY (ARRAY['match'::text, 'no_match'::text, 'uncertain'::text, 'spoof_detected'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bio_verif ON biometric_verifications USING btree (voter_vin, verified_at);

CREATE TABLE IF NOT EXISTS biometric_vault_keys (
    id integer NOT NULL,
    key_id text NOT NULL,
    encrypted_key bytea NOT NULL,
    key_type text DEFAULT 'AES-256-GCM'::text NOT NULL,
    purpose text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    rotation_count integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    rotated_at timestamp without time zone,
    expires_at timestamp without time zone,
    CONSTRAINT biometric_vault_keys_purpose_check CHECK ((purpose = ANY (ARRAY['template_encryption'::text, 'signing'::text, 'key_wrapping'::text]))),
    CONSTRAINT biometric_vault_keys_status_check CHECK ((status = ANY (ARRAY['active'::text, 'rotated'::text, 'revoked'::text])))
);

CREATE INDEX IF NOT EXISTS idx_vault_keys ON biometric_vault_keys USING btree (key_id, status);

CREATE TABLE IF NOT EXISTS biometric_vault_audit (
    id integer NOT NULL,
    operation text NOT NULL,
    key_id text,
    voter_vin text,
    modality text,
    actor text,
    ip_address text,
    success integer DEFAULT 1 NOT NULL,
    error_detail text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_vault_audit ON biometric_vault_audit USING btree (voter_vin, "timestamp");

CREATE TABLE IF NOT EXISTS biometric_match_log (
    id integer NOT NULL,
    voter_vin text,
    modality text NOT NULL,
    match_score real DEFAULT 0 NOT NULL,
    is_genuine integer DEFAULT 0 NOT NULL,
    device_id text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_match_log_modality ON biometric_match_log USING btree (modality);

CREATE TABLE IF NOT EXISTS biometric_quality_scores (
    id integer NOT NULL,
    capture_id text,
    modality text,
    blur_score real,
    exposure_score real,
    angle_score real,
    overall_quality real,
    guidance text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS biometric_sdk_providers (
    id integer NOT NULL,
    provider_name text NOT NULL,
    sdk_version text NOT NULL,
    modalities text NOT NULL,
    license_type text DEFAULT 'commercial'::text,
    api_endpoint text,
    status text DEFAULT 'active'::text,
    accuracy_fingerprint real DEFAULT 0,
    accuracy_facial real DEFAULT 0,
    accuracy_iris real DEFAULT 0,
    last_health_check timestamp without time zone,
    registered_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bio_audit_timeline (
    id integer NOT NULL,
    event_type text NOT NULL,
    category text NOT NULL,
    severity text DEFAULT 'info'::text,
    actor text,
    voter_vin text,
    device_id text,
    details text,
    ip_address text,
    geo_location text,
    session_id text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_time ON bio_audit_timeline USING btree ("timestamp", event_type);

CREATE TABLE IF NOT EXISTS bvas_capture_sessions (
    id integer NOT NULL,
    session_id text NOT NULL,
    device_id text NOT NULL,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    capture_quality real DEFAULT 0 NOT NULL,
    nfiq2_score integer DEFAULT 0,
    capture_attempts integer DEFAULT 1,
    max_attempts integer DEFAULT 3,
    image_width integer DEFAULT 0,
    image_height integer DEFAULT 0,
    image_dpi integer DEFAULT 500,
    status text DEFAULT 'captured'::text,
    error_code text,
    processing_time_ms integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT bvas_capture_sessions_status_check CHECK ((status = ANY (ARRAY['initiated'::text, 'capturing'::text, 'captured'::text, 'quality_failed'::text, 'processed'::text, 'error'::text])))
);

CREATE INDEX IF NOT EXISTS idx_bvas_cap ON bvas_capture_sessions USING btree (device_id, voter_vin);

CREATE TABLE IF NOT EXISTS abis_duplicate_checks (
    id integer NOT NULL,
    source_vin text NOT NULL,
    candidate_vin text,
    similarity_score real NOT NULL,
    modality text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    reviewed_by integer,
    reviewed_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT abis_duplicate_checks_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'confirmed_duplicate'::text, 'false_positive'::text, 'resolved'::text])))
);

CREATE TABLE IF NOT EXISTS abis_enrollment_pipeline (
    id integer NOT NULL,
    voter_vin text NOT NULL,
    stage text NOT NULL,
    modality text NOT NULL,
    device_id text,
    quality_passed integer DEFAULT 0,
    template_extracted integer DEFAULT 0,
    dedup_cleared integer DEFAULT 0,
    vault_stored integer DEFAULT 0,
    far_threshold real DEFAULT 0.0001,
    frr_threshold real DEFAULT 0.01,
    error_detail text,
    started_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    completed_at timestamp without time zone,
    CONSTRAINT abis_enrollment_pipeline_stage_check CHECK ((stage = ANY (ARRAY['capture'::text, 'quality_check'::text, 'template_extract'::text, 'dedup_check'::text, 'vault_store'::text, 'complete'::text, 'failed'::text])))
);

CREATE INDEX IF NOT EXISTS idx_abis_pipe ON abis_enrollment_pipeline USING btree (voter_vin, stage);

CREATE TABLE IF NOT EXISTS cancelable_transforms (
    id integer NOT NULL,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    transform_id text NOT NULL,
    transform_type text DEFAULT 'biohashing'::text NOT NULL,
    transform_seed bytea NOT NULL,
    version integer DEFAULT 1,
    revoked integer DEFAULT 0,
    revoked_at timestamp without time zone,
    revocation_reason text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cancel_vin ON cancelable_transforms USING btree (voter_vin, modality);

CREATE TABLE IF NOT EXISTS distributed_dedup_partitions (
    id integer NOT NULL,
    job_id integer NOT NULL,
    partition_key text NOT NULL,
    worker_id text NOT NULL,
    status text DEFAULT 'pending'::text,
    records_count integer DEFAULT 0,
    comparisons integer DEFAULT 0,
    duplicates integer DEFAULT 0,
    started_at timestamp without time zone,
    completed_at timestamp without time zone
);

CREATE TABLE IF NOT EXISTS liveness_checks (
    id integer NOT NULL,
    user_id integer NOT NULL,
    passed integer DEFAULT 0,
    confidence real,
    method text,
    anti_spoofing_score real,
    checks_json text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS multi_finger_enrollments (
    id integer NOT NULL,
    voter_vin text NOT NULL,
    finger_position text NOT NULL,
    finger_index integer NOT NULL,
    template_hash text NOT NULL,
    quality_score real DEFAULT 0,
    nfiq2_score integer DEFAULT 0,
    is_primary integer DEFAULT 0,
    is_fallback integer DEFAULT 0,
    enrolled_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_multi_finger ON multi_finger_enrollments USING btree (voter_vin, finger_position);

CREATE TABLE IF NOT EXISTS nist_benchmark_results (
    id integer NOT NULL,
    benchmark_type text NOT NULL,
    modality text NOT NULL,
    dataset text NOT NULL,
    total_subjects integer DEFAULT 0,
    total_comparisons integer DEFAULT 0,
    fnmr_at_fmr_001 real DEFAULT 0,
    fnmr_at_fmr_01 real DEFAULT 0,
    fnmr_at_fmr_1 real DEFAULT 0,
    eer real DEFAULT 0,
    throughput_per_sec real DEFAULT 0,
    template_size_bytes integer DEFAULT 0,
    run_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    status text DEFAULT 'completed'::text
);

CREATE TABLE IF NOT EXISTS offline_enrollment_queue (
    id integer NOT NULL,
    device_id text NOT NULL,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    template_data_hash text NOT NULL,
    queued_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    connectivity_status text DEFAULT 'offline'::text,
    sync_status text DEFAULT 'pending'::text,
    sync_attempts integer DEFAULT 0,
    synced_at timestamp without time zone,
    conflict_detected integer DEFAULT 0,
    resolution text
);

CREATE INDEX IF NOT EXISTS idx_offline_sync ON offline_enrollment_queue USING btree (sync_status, device_id);

CREATE TABLE IF NOT EXISTS pad_attack_log (
    id integer NOT NULL,
    voter_vin text,
    modality text NOT NULL,
    attack_type text NOT NULL,
    attack_instrument text,
    detection_score real NOT NULL,
    pad_model_id integer,
    texture_lbp_score real DEFAULT 0,
    frequency_score real DEFAULT 0,
    gradient_score real DEFAULT 0,
    color_hist_score real DEFAULT 0,
    motion_flow_score real DEFAULT 0,
    depth_consistency real DEFAULT 0,
    blocked integer DEFAULT 1 NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pad_attack ON pad_attack_log USING btree (voter_vin, created_at);

CREATE TABLE IF NOT EXISTS pad_models (
    id integer NOT NULL,
    model_name text NOT NULL,
    model_version text NOT NULL,
    modality text NOT NULL,
    attack_types text NOT NULL,
    accuracy real NOT NULL,
    far real NOT NULL,
    frr real NOT NULL,
    training_samples integer DEFAULT 0,
    iso_30107_level text DEFAULT 'level2'::text,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pad_results (
    id integer NOT NULL,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    device_id text,
    liveness_score real NOT NULL,
    texture_score real DEFAULT 0,
    motion_score real DEFAULT 0,
    depth_score real DEFAULT 0,
    spectral_score real DEFAULT 0,
    pad_decision text NOT NULL,
    pad_level text DEFAULT 'level2'::text NOT NULL,
    attack_type text,
    confidence real DEFAULT 0 NOT NULL,
    iso_30107_compliance integer DEFAULT 1,
    checked_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT pad_results_pad_decision_check CHECK ((pad_decision = ANY (ARRAY['live'::text, 'spoof'::text, 'uncertain'::text]))),
    CONSTRAINT pad_results_pad_level_check CHECK ((pad_level = ANY (ARRAY['level1'::text, 'level2'::text, 'level3'::text])))
);

CREATE INDEX IF NOT EXISTS idx_pad_vin ON pad_results USING btree (voter_vin, checked_at);

CREATE TABLE IF NOT EXISTS quality_gateway_rejections (
    id integer NOT NULL,
    device_id text NOT NULL,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    nfiq2_score integer DEFAULT 0,
    quality_score real DEFAULT 0,
    rejection_reason text NOT NULL,
    threshold_applied real DEFAULT 0,
    retry_count integer DEFAULT 0,
    bandwidth_saved_kb real DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS score_normalization_cohorts (
    id integer NOT NULL,
    cohort_id text NOT NULL,
    modality text NOT NULL,
    norm_type text DEFAULT 'z_norm'::text NOT NULL,
    mean_genuine real DEFAULT 0,
    std_genuine real DEFAULT 0,
    mean_impostor real DEFAULT 0,
    std_impostor real DEFAULT 0,
    sample_size integer DEFAULT 0,
    device_id text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS template_aging_records (
    id integer NOT NULL,
    voter_vin text NOT NULL,
    modality text NOT NULL,
    enrolled_at timestamp without time zone NOT NULL,
    age_days integer DEFAULT 0,
    max_age_days integer DEFAULT 1825,
    quality_decay real DEFAULT 0,
    re_enrollment_required integer DEFAULT 0,
    re_enrollment_scheduled timestamp without time zone,
    re_enrollment_completed timestamp without time zone,
    notification_sent integer DEFAULT 0,
    status text DEFAULT 'valid'::text
);

CREATE INDEX IF NOT EXISTS idx_aging_status ON template_aging_records USING btree (status, re_enrollment_required);
CREATE INDEX IF NOT EXISTS idx_aging_vin ON template_aging_records USING btree (voter_vin);

CREATE TABLE IF NOT EXISTS threshold_tuning_runs (
    id integer NOT NULL,
    modality text NOT NULL,
    genuine_pairs integer DEFAULT 0,
    impostor_pairs integer DEFAULT 0,
    optimal_threshold real DEFAULT 0,
    eer real DEFAULT 0,
    far_at_threshold real DEFAULT 0,
    frr_at_threshold real DEFAULT 0,
    auc real DEFAULT 0,
    det_points text,
    roc_points text,
    status text DEFAULT 'completed'::text,
    run_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


