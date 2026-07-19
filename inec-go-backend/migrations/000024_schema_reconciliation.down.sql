-- Rollback 000021 (drops added columns; NOT NULL relaxations are not re-applied)
DROP TABLE IF EXISTS search_documents;

-- api_key_metadata
ALTER TABLE "api_key_metadata" DROP COLUMN IF EXISTS "environment";
ALTER TABLE "api_key_metadata" DROP COLUMN IF EXISTS "last_used";
ALTER TABLE "api_key_metadata" DROP COLUMN IF EXISTS "owner";
ALTER TABLE "api_key_metadata" DROP COLUMN IF EXISTS "permissions";
ALTER TABLE "api_key_metadata" DROP COLUMN IF EXISTS "rate_limit";

-- api_usage
ALTER TABLE "api_usage" DROP COLUMN IF EXISTS "key_id";
ALTER TABLE "api_usage" DROP COLUMN IF EXISTS "response_time_ms";

-- audit_log
ALTER TABLE "audit_log" DROP COLUMN IF EXISTS "performed_at";
ALTER TABLE "audit_log" DROP COLUMN IF EXISTS "performed_by";

-- biometric_match_log
ALTER TABLE "biometric_match_log" DROP COLUMN IF EXISTS "is_match";
ALTER TABLE "biometric_match_log" DROP COLUMN IF EXISTS "template_id_a";
ALTER TABLE "biometric_match_log" DROP COLUMN IF EXISTS "template_id_b";
ALTER TABLE "biometric_match_log" DROP COLUMN IF EXISTS "threshold";

-- biometric_profiles
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "device_id";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "enrollment_center";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "enrollment_status";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "facial_quality";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "fingerprint_quality";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "iris_quality";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "last_verified";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "operator_id";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "photo_hash";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "verification_count";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "vin";
ALTER TABLE "biometric_profiles" DROP COLUMN IF EXISTS "voter_id";

-- biometric_quality_scores
ALTER TABLE "biometric_quality_scores" DROP COLUMN IF EXISTS "contrast";
ALTER TABLE "biometric_quality_scores" DROP COLUMN IF EXISTS "iso_quality";
ALTER TABLE "biometric_quality_scores" DROP COLUMN IF EXISTS "minutiae_count";
ALTER TABLE "biometric_quality_scores" DROP COLUMN IF EXISTS "nfiq_score";
ALTER TABLE "biometric_quality_scores" DROP COLUMN IF EXISTS "sharpness";
ALTER TABLE "biometric_quality_scores" DROP COLUMN IF EXISTS "template_id";
ALTER TABLE "biometric_quality_scores" DROP COLUMN IF EXISTS "uniformity";

-- biometric_templates
ALTER TABLE "biometric_templates" DROP COLUMN IF EXISTS "captured_at";
ALTER TABLE "biometric_templates" DROP COLUMN IF EXISTS "device_id";
ALTER TABLE "biometric_templates" DROP COLUMN IF EXISTS "encrypted_template";
ALTER TABLE "biometric_templates" DROP COLUMN IF EXISTS "is_active";
ALTER TABLE "biometric_templates" DROP COLUMN IF EXISTS "profile_id";
ALTER TABLE "biometric_templates" DROP COLUMN IF EXISTS "template_hash";
ALTER TABLE "biometric_templates" DROP COLUMN IF EXISTS "template_id";
ALTER TABLE "biometric_templates" DROP COLUMN IF EXISTS "version";

-- biometric_verifications
ALTER TABLE "biometric_verifications" DROP COLUMN IF EXISTS "latitude";
ALTER TABLE "biometric_verifications" DROP COLUMN IF EXISTS "liveness_score";
ALTER TABLE "biometric_verifications" DROP COLUMN IF EXISTS "longitude";
ALTER TABLE "biometric_verifications" DROP COLUMN IF EXISTS "match_result";
ALTER TABLE "biometric_verifications" DROP COLUMN IF EXISTS "operator_id";
ALTER TABLE "biometric_verifications" DROP COLUMN IF EXISTS "processing_time_ms";
ALTER TABLE "biometric_verifications" DROP COLUMN IF EXISTS "profile_id";
ALTER TABLE "biometric_verifications" DROP COLUMN IF EXISTS "quality_score";
ALTER TABLE "biometric_verifications" DROP COLUMN IF EXISTS "score";

-- blockchain_audit_trail
ALTER TABLE "blockchain_audit_trail" DROP COLUMN IF EXISTS "block_number";
ALTER TABLE "blockchain_audit_trail" DROP COLUMN IF EXISTS "channel";
ALTER TABLE "blockchain_audit_trail" DROP COLUMN IF EXISTS "data_hash";
ALTER TABLE "blockchain_audit_trail" DROP COLUMN IF EXISTS "verified";

-- bvas_accreditations
ALTER TABLE "bvas_accreditations" DROP COLUMN IF EXISTS "match_score";
ALTER TABLE "bvas_accreditations" DROP COLUMN IF EXISTS "status";
ALTER TABLE "bvas_accreditations" DROP COLUMN IF EXISTS "timestamp";
ALTER TABLE "bvas_accreditations" DROP COLUMN IF EXISTS "voter_vin";

-- bvas_capture_sessions
ALTER TABLE "bvas_capture_sessions" DROP COLUMN IF EXISTS "attempt_number";
ALTER TABLE "bvas_capture_sessions" DROP COLUMN IF EXISTS "capture_type";
ALTER TABLE "bvas_capture_sessions" DROP COLUMN IF EXISTS "duration_ms";
ALTER TABLE "bvas_capture_sessions" DROP COLUMN IF EXISTS "latitude";
ALTER TABLE "bvas_capture_sessions" DROP COLUMN IF EXISTS "longitude";
ALTER TABLE "bvas_capture_sessions" DROP COLUMN IF EXISTS "success";

-- bvas_device_capabilities
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "battery_capacity_mah";
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "connectivity";
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "facial_camera";
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "gps_module";
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "iris_scanner";
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "nfc_reader";
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "os_version";
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "ram_mb";
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "screen_size_inch";
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "sim_slots";
ALTER TABLE "bvas_device_capabilities" DROP COLUMN IF EXISTS "storage_gb";

-- bvas_heartbeats
ALTER TABLE "bvas_heartbeats" DROP COLUMN IF EXISTS "gps_lat";
ALTER TABLE "bvas_heartbeats" DROP COLUMN IF EXISTS "gps_lng";
ALTER TABLE "bvas_heartbeats" DROP COLUMN IF EXISTS "status";

-- bvas_location_logs
ALTER TABLE "bvas_location_logs" DROP COLUMN IF EXISTS "accuracy";
ALTER TABLE "bvas_location_logs" DROP COLUMN IF EXISTS "altitude";
ALTER TABLE "bvas_location_logs" DROP COLUMN IF EXISTS "device_id";

-- bvas_sync_queue
ALTER TABLE "bvas_sync_queue" DROP COLUMN IF EXISTS "data_type";
ALTER TABLE "bvas_sync_queue" DROP COLUMN IF EXISTS "payload_size";

-- collation_party_scores
ALTER TABLE "collation_party_scores" DROP COLUMN IF EXISTS "collation_id";

-- collation_results
ALTER TABLE "collation_results" DROP COLUMN IF EXISTS "code";
ALTER TABLE "collation_results" DROP COLUMN IF EXISTS "collated_at";
ALTER TABLE "collation_results" DROP COLUMN IF EXISTS "collation_officer_id";
ALTER TABLE "collation_results" DROP COLUMN IF EXISTS "total_accredited";
ALTER TABLE "collation_results" DROP COLUMN IF EXISTS "total_registered";
ALTER TABLE "collation_results" DROP COLUMN IF EXISTS "total_rejected";
ALTER TABLE "collation_results" DROP COLUMN IF EXISTS "total_valid";
ALTER TABLE "collation_results" DROP COLUMN IF EXISTS "total_votes";

-- crowd_density
ALTER TABLE "crowd_density" DROP COLUMN IF EXISTS "geom";

-- data_classification
ALTER TABLE "data_classification" DROP COLUMN IF EXISTS "field_name";
ALTER TABLE "data_classification" DROP COLUMN IF EXISTS "is_encrypted";

-- data_encryption_keys
ALTER TABLE "data_encryption_keys" DROP COLUMN IF EXISTS "is_active";
ALTER TABLE "data_encryption_keys" DROP COLUMN IF EXISTS "rotation_interval_days";

-- dedup_candidates
ALTER TABLE "dedup_candidates" DROP COLUMN IF EXISTS "match_score";
ALTER TABLE "dedup_candidates" DROP COLUMN IF EXISTS "modality";
ALTER TABLE "dedup_candidates" DROP COLUMN IF EXISTS "profile_id_a";
ALTER TABLE "dedup_candidates" DROP COLUMN IF EXISTS "profile_id_b";
ALTER TABLE "dedup_candidates" DROP COLUMN IF EXISTS "review_notes";
ALTER TABLE "dedup_candidates" DROP COLUMN IF EXISTS "status";

-- dedup_jobs
ALTER TABLE "dedup_jobs" DROP COLUMN IF EXISTS "algorithm";
ALTER TABLE "dedup_jobs" DROP COLUMN IF EXISTS "batch_size";
ALTER TABLE "dedup_jobs" DROP COLUMN IF EXISTS "job_id";
ALTER TABLE "dedup_jobs" DROP COLUMN IF EXISTS "modality";

-- document_analyses
ALTER TABLE "document_analyses" DROP COLUMN IF EXISTS "anomaly_flags";
ALTER TABLE "document_analyses" DROP COLUMN IF EXISTS "confidence_score";
ALTER TABLE "document_analyses" DROP COLUMN IF EXISTS "document_type";
ALTER TABLE "document_analyses" DROP COLUMN IF EXISTS "extracted_data";
ALTER TABLE "document_analyses" DROP COLUMN IF EXISTS "file_url";
ALTER TABLE "document_analyses" DROP COLUMN IF EXISTS "processing_time_ms";
ALTER TABLE "document_analyses" DROP COLUMN IF EXISTS "status";
ALTER TABLE "document_analyses" DROP COLUMN IF EXISTS "validation_status";

-- election_lifecycle
ALTER TABLE "election_lifecycle" DROP COLUMN IF EXISTS "from_state";
ALTER TABLE "election_lifecycle" DROP COLUMN IF EXISTS "reason";
ALTER TABLE "election_lifecycle" DROP COLUMN IF EXISTS "started_at";
ALTER TABLE "election_lifecycle" DROP COLUMN IF EXISTS "status";
ALTER TABLE "election_lifecycle" DROP COLUMN IF EXISTS "to_state";

-- election_materials
ALTER TABLE "election_materials" DROP COLUMN IF EXISTS "category";
ALTER TABLE "election_materials" DROP COLUMN IF EXISTS "name";
ALTER TABLE "election_materials" DROP COLUMN IF EXISTS "quantity_allocated";
ALTER TABLE "election_materials" DROP COLUMN IF EXISTS "quantity_deployed";
ALTER TABLE "election_materials" DROP COLUMN IF EXISTS "quantity_returned";
ALTER TABLE "election_materials" DROP COLUMN IF EXISTS "tracking_code";

-- election_staff_assignments
ALTER TABLE "election_staff_assignments" DROP COLUMN IF EXISTS "polling_unit_code";
ALTER TABLE "election_staff_assignments" DROP COLUMN IF EXISTS "state_code";

-- election_state_log
ALTER TABLE "election_state_log" DROP COLUMN IF EXISTS "changed_by";
ALTER TABLE "election_state_log" DROP COLUMN IF EXISTS "reason";

-- elections
ALTER TABLE "elections" DROP COLUMN IF EXISTS "state_code";

-- ems_workflow_phases
ALTER TABLE "ems_workflow_phases" DROP COLUMN IF EXISTS "assigned_to";
ALTER TABLE "ems_workflow_phases" DROP COLUMN IF EXISTS "phase_name";
ALTER TABLE "ems_workflow_phases" DROP COLUMN IF EXISTS "sequence_order";

-- ems_workflows
ALTER TABLE "ems_workflows" DROP COLUMN IF EXISTS "created_by";
ALTER TABLE "ems_workflows" DROP COLUMN IF EXISTS "name";

-- escalation_log
ALTER TABLE "escalation_log" DROP COLUMN IF EXISTS "resolved";
ALTER TABLE "escalation_log" DROP COLUMN IF EXISTS "rule_id";
ALTER TABLE "escalation_log" DROP COLUMN IF EXISTS "triggered_by";

-- escalation_rules
ALTER TABLE "escalation_rules" DROP COLUMN IF EXISTS "action_type";
ALTER TABLE "escalation_rules" DROP COLUMN IF EXISTS "condition_expr";
ALTER TABLE "escalation_rules" DROP COLUMN IF EXISTS "cooldown_minutes";
ALTER TABLE "escalation_rules" DROP COLUMN IF EXISTS "is_active";
ALTER TABLE "escalation_rules" DROP COLUMN IF EXISTS "severity";

-- export_audit_log
ALTER TABLE "export_audit_log" DROP COLUMN IF EXISTS "record_count";

-- fabric_blocks
ALTER TABLE "fabric_blocks" DROP COLUMN IF EXISTS "channel";

-- fabric_transactions
ALTER TABLE "fabric_transactions" DROP COLUMN IF EXISTS "chaincode";
ALTER TABLE "fabric_transactions" DROP COLUMN IF EXISTS "channel";
ALTER TABLE "fabric_transactions" DROP COLUMN IF EXISTS "data_hash";
ALTER TABLE "fabric_transactions" DROP COLUMN IF EXISTS "status";

-- gotv_cpi_history
ALTER TABLE "gotv_cpi_history" DROP COLUMN IF EXISTS "endorsements";
ALTER TABLE "gotv_cpi_history" DROP COLUMN IF EXISTS "favourability";
ALTER TABLE "gotv_cpi_history" DROP COLUMN IF EXISTS "sentiment";
ALTER TABLE "gotv_cpi_history" DROP COLUMN IF EXISTS "voting_intention";

-- gotv_field_reports
ALTER TABLE "gotv_field_reports" DROP COLUMN IF EXISTS "lga_code";
ALTER TABLE "gotv_field_reports" DROP COLUMN IF EXISTS "media_url";
ALTER TABLE "gotv_field_reports" DROP COLUMN IF EXISTS "report_type";
ALTER TABLE "gotv_field_reports" DROP COLUMN IF EXISTS "state_code";

-- gotv_ride_requests
ALTER TABLE "gotv_ride_requests" DROP COLUMN IF EXISTS "created_at";
ALTER TABLE "gotv_ride_requests" DROP COLUMN IF EXISTS "notes";

-- gotv_volunteers
ALTER TABLE "gotv_volunteers" DROP COLUMN IF EXISTS "vetting_status";

-- grievances
ALTER TABLE "grievances" DROP COLUMN IF EXISTS "category";
ALTER TABLE "grievances" DROP COLUMN IF EXISTS "election_id";
ALTER TABLE "grievances" DROP COLUMN IF EXISTS "filed_by";
ALTER TABLE "grievances" DROP COLUMN IF EXISTS "polling_unit_code";

-- incidents
ALTER TABLE "incidents" DROP COLUMN IF EXISTS "latitude";
ALTER TABLE "incidents" DROP COLUMN IF EXISTS "longitude";
ALTER TABLE "incidents" DROP COLUMN IF EXISTS "title";

-- ingestion_jobs
ALTER TABLE "ingestion_jobs" DROP COLUMN IF EXISTS "job_id";
ALTER TABLE "ingestion_jobs" DROP COLUMN IF EXISTS "output_path";
ALTER TABLE "ingestion_jobs" DROP COLUMN IF EXISTS "records_failed";
ALTER TABLE "ingestion_jobs" DROP COLUMN IF EXISTS "records_processed";
ALTER TABLE "ingestion_jobs" DROP COLUMN IF EXISTS "records_total";
ALTER TABLE "ingestion_jobs" DROP COLUMN IF EXISTS "source";
ALTER TABLE "ingestion_jobs" DROP COLUMN IF EXISTS "source_type";
ALTER TABLE "ingestion_jobs" DROP COLUMN IF EXISTS "tier";

-- kyb_verifications
ALTER TABLE "kyb_verifications" DROP COLUMN IF EXISTS "rc_number";
ALTER TABLE "kyb_verifications" DROP COLUMN IF EXISTS "tin";
ALTER TABLE "kyb_verifications" DROP COLUMN IF EXISTS "verified_by";

-- kyc_verifications
ALTER TABLE "kyc_verifications" DROP COLUMN IF EXISTS "document_number";
ALTER TABLE "kyc_verifications" DROP COLUMN IF EXISTS "document_type";
ALTER TABLE "kyc_verifications" DROP COLUMN IF EXISTS "expiry_date";
ALTER TABLE "kyc_verifications" DROP COLUMN IF EXISTS "verification_type";
ALTER TABLE "kyc_verifications" DROP COLUMN IF EXISTS "verified_by";

-- liveness_checks
ALTER TABLE "liveness_checks" DROP COLUMN IF EXISTS "attempt_number";
ALTER TABLE "liveness_checks" DROP COLUMN IF EXISTS "challenge_type";
ALTER TABLE "liveness_checks" DROP COLUMN IF EXISTS "device_id";
ALTER TABLE "liveness_checks" DROP COLUMN IF EXISTS "profile_id";
ALTER TABLE "liveness_checks" DROP COLUMN IF EXISTS "result";

-- mfa_settings
ALTER TABLE "mfa_settings" DROP COLUMN IF EXISTS "backup_codes";
ALTER TABLE "mfa_settings" DROP COLUMN IF EXISTS "is_enabled";
ALTER TABLE "mfa_settings" DROP COLUMN IF EXISTS "method";

-- model_metrics
ALTER TABLE "model_metrics" DROP COLUMN IF EXISTS "f1_score";
ALTER TABLE "model_metrics" DROP COLUMN IF EXISTS "precision_score";
ALTER TABLE "model_metrics" DROP COLUMN IF EXISTS "recall";

-- mw_events
ALTER TABLE "mw_events" DROP COLUMN IF EXISTS "details";
ALTER TABLE "mw_events" DROP COLUMN IF EXISTS "event_type";
ALTER TABLE "mw_events" DROP COLUMN IF EXISTS "message";
ALTER TABLE "mw_events" DROP COLUMN IF EXISTS "middleware";
ALTER TABLE "mw_events" DROP COLUMN IF EXISTS "severity";

-- mw_state
ALTER TABLE "mw_state" DROP COLUMN IF EXISTS "last_check";
ALTER TABLE "mw_state" DROP COLUMN IF EXISTS "name";
ALTER TABLE "mw_state" DROP COLUMN IF EXISTS "status";

-- observer_alert_rules
ALTER TABLE "observer_alert_rules" DROP COLUMN IF EXISTS "action";
ALTER TABLE "observer_alert_rules" DROP COLUMN IF EXISTS "condition_expr";
ALTER TABLE "observer_alert_rules" DROP COLUMN IF EXISTS "cooldown_minutes";
ALTER TABLE "observer_alert_rules" DROP COLUMN IF EXISTS "name";
ALTER TABLE "observer_alert_rules" DROP COLUMN IF EXISTS "severity";

-- official_tracking
ALTER TABLE "official_tracking" DROP COLUMN IF EXISTS "geom";

-- offline_sync_queue
ALTER TABLE "offline_sync_queue" DROP COLUMN IF EXISTS "data_type";
ALTER TABLE "offline_sync_queue" DROP COLUMN IF EXISTS "priority";
ALTER TABLE "offline_sync_queue" DROP COLUMN IF EXISTS "retry_count";

-- polling_unit_locations
ALTER TABLE "polling_unit_locations" DROP COLUMN IF EXISTS "geom";

-- portal_connections
ALTER TABLE "portal_connections" DROP COLUMN IF EXISTS "endpoint_url";
ALTER TABLE "portal_connections" DROP COLUMN IF EXISTS "last_sync";
ALTER TABLE "portal_connections" DROP COLUMN IF EXISTS "name";

-- portal_webhooks
ALTER TABLE "portal_webhooks" DROP COLUMN IF EXISTS "endpoint_url";
ALTER TABLE "portal_webhooks" DROP COLUMN IF EXISTS "is_active";
ALTER TABLE "portal_webhooks" DROP COLUMN IF EXISTS "secret_hash";

-- push_notifications
ALTER TABLE "push_notifications" DROP COLUMN IF EXISTS "channel";
ALTER TABLE "push_notifications" DROP COLUMN IF EXISTS "election_id";
ALTER TABLE "push_notifications" DROP COLUMN IF EXISTS "priority";
ALTER TABLE "push_notifications" DROP COLUMN IF EXISTS "recipients";

-- registration_centers
ALTER TABLE "registration_centers" DROP COLUMN IF EXISTS "equipment_status";
ALTER TABLE "registration_centers" DROP COLUMN IF EXISTS "operating_hours";
ALTER TABLE "registration_centers" DROP COLUMN IF EXISTS "supervisor_id";

-- result_signatures
ALTER TABLE "result_signatures" DROP COLUMN IF EXISTS "algorithm";
ALTER TABLE "result_signatures" DROP COLUMN IF EXISTS "signature_hash";
ALTER TABLE "result_signatures" DROP COLUMN IF EXISTS "signer_id";
ALTER TABLE "result_signatures" DROP COLUMN IF EXISTS "signer_role";
ALTER TABLE "result_signatures" DROP COLUMN IF EXISTS "verification_status";

-- results
ALTER TABLE "results" DROP COLUMN IF EXISTS "lga";
ALTER TABLE "results" DROP COLUMN IF EXISTS "state";
ALTER TABLE "results" DROP COLUMN IF EXISTS "submitted_by";
ALTER TABLE "results" DROP COLUMN IF EXISTS "total_votes";
ALTER TABLE "results" DROP COLUMN IF EXISTS "ward";

-- security_audit_events
ALTER TABLE "security_audit_events" DROP COLUMN IF EXISTS "user_agent";

-- sms_verifications
ALTER TABLE "sms_verifications" DROP COLUMN IF EXISTS "attempts";
ALTER TABLE "sms_verifications" DROP COLUMN IF EXISTS "code";
ALTER TABLE "sms_verifications" DROP COLUMN IF EXISTS "purpose";
ALTER TABLE "sms_verifications" DROP COLUMN IF EXISTS "verified";

-- stablecoin_ledger
ALTER TABLE "stablecoin_ledger" DROP COLUMN IF EXISTS "amount";
ALTER TABLE "stablecoin_ledger" DROP COLUMN IF EXISTS "balance_after";

-- stablecoin_wallets
ALTER TABLE "stablecoin_wallets" DROP COLUMN IF EXISTS "balance";

-- stakeholders
ALTER TABLE "stakeholders" DROP COLUMN IF EXISTS "contact_person";
ALTER TABLE "stakeholders" DROP COLUMN IF EXISTS "org_name";
ALTER TABLE "stakeholders" DROP COLUMN IF EXISTS "state_code";
ALTER TABLE "stakeholders" DROP COLUMN IF EXISTS "status";
ALTER TABLE "stakeholders" DROP COLUMN IF EXISTS "type";

-- tb_accounts
ALTER TABLE "tb_accounts" DROP COLUMN IF EXISTS "account_id";
ALTER TABLE "tb_accounts" DROP COLUMN IF EXISTS "credit_balance";
ALTER TABLE "tb_accounts" DROP COLUMN IF EXISTS "currency";
ALTER TABLE "tb_accounts" DROP COLUMN IF EXISTS "debit_balance";
ALTER TABLE "tb_accounts" DROP COLUMN IF EXISTS "description";

-- tb_transfers
ALTER TABLE "tb_transfers" DROP COLUMN IF EXISTS "credit_account";
ALTER TABLE "tb_transfers" DROP COLUMN IF EXISTS "debit_account";
ALTER TABLE "tb_transfers" DROP COLUMN IF EXISTS "description";
ALTER TABLE "tb_transfers" DROP COLUMN IF EXISTS "transfer_id";

-- training_courses
ALTER TABLE "training_courses" DROP COLUMN IF EXISTS "duration_hours";
ALTER TABLE "training_courses" DROP COLUMN IF EXISTS "status";

-- users
ALTER TABLE "users" DROP COLUMN IF EXISTS "email";
ALTER TABLE "users" DROP COLUMN IF EXISTS "login_count";

-- ussd_sessions
ALTER TABLE "ussd_sessions" DROP COLUMN IF EXISTS "last_input";
ALTER TABLE "ussd_sessions" DROP COLUMN IF EXISTS "session_id";
ALTER TABLE "ussd_sessions" DROP COLUMN IF EXISTS "state";

-- validation_results
ALTER TABLE "validation_results" DROP COLUMN IF EXISTS "election_id";
ALTER TABLE "validation_results" DROP COLUMN IF EXISTS "polling_unit_code";
ALTER TABLE "validation_results" DROP COLUMN IF EXISTS "result_id";
ALTER TABLE "validation_results" DROP COLUMN IF EXISTS "status";

-- validation_rules
ALTER TABLE "validation_rules" DROP COLUMN IF EXISTS "auto_flag";
ALTER TABLE "validation_rules" DROP COLUMN IF EXISTS "category";
ALTER TABLE "validation_rules" DROP COLUMN IF EXISTS "name";
ALTER TABLE "validation_rules" DROP COLUMN IF EXISTS "query_template";

-- voters
ALTER TABLE "voters" DROP COLUMN IF EXISTS "biometric_captured";
ALTER TABLE "voters" DROP COLUMN IF EXISTS "created_at";
ALTER TABLE "voters" DROP COLUMN IF EXISTS "photo_url";
ALTER TABLE "voters" DROP COLUMN IF EXISTS "registration_status";

-- waf_blocklist
ALTER TABLE "waf_blocklist" DROP COLUMN IF EXISTS "expires_at";
