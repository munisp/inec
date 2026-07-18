-- Rollback: Biometric: profiles, templates, vault, ABIS, quality

DROP TABLE IF EXISTS threshold_tuning_runs CASCADE;
DROP TABLE IF EXISTS template_aging_records CASCADE;
DROP TABLE IF EXISTS score_normalization_cohorts CASCADE;
DROP TABLE IF EXISTS quality_gateway_rejections CASCADE;
DROP TABLE IF EXISTS pad_results CASCADE;
DROP TABLE IF EXISTS pad_models CASCADE;
DROP TABLE IF EXISTS pad_attack_log CASCADE;
DROP TABLE IF EXISTS offline_enrollment_queue CASCADE;
DROP TABLE IF EXISTS nist_benchmark_results CASCADE;
DROP TABLE IF EXISTS multi_finger_enrollments CASCADE;
DROP TABLE IF EXISTS liveness_checks CASCADE;
DROP TABLE IF EXISTS distributed_dedup_partitions CASCADE;
DROP TABLE IF EXISTS cancelable_transforms CASCADE;
DROP TABLE IF EXISTS abis_enrollment_pipeline CASCADE;
DROP TABLE IF EXISTS abis_duplicate_checks CASCADE;
DROP TABLE IF EXISTS bvas_capture_sessions CASCADE;
DROP TABLE IF EXISTS bio_audit_timeline CASCADE;
DROP TABLE IF EXISTS biometric_sdk_providers CASCADE;
DROP TABLE IF EXISTS biometric_quality_scores CASCADE;
DROP TABLE IF EXISTS biometric_match_log CASCADE;
DROP TABLE IF EXISTS biometric_vault_audit CASCADE;
DROP TABLE IF EXISTS biometric_vault_keys CASCADE;
DROP TABLE IF EXISTS biometric_verifications CASCADE;
DROP TABLE IF EXISTS biometric_templates CASCADE;
DROP TABLE IF EXISTS biometric_profiles CASCADE;
