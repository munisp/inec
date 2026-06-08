-- Rollback: Security: encryption keys, HSM, audit events, access policies

DROP TABLE IF EXISTS security_threats CASCADE;
DROP TABLE IF EXISTS security_audit_events CASCADE;
DROP TABLE IF EXISTS row_access_policies CASCADE;
DROP TABLE IF EXISTS privacy_preserving_ops CASCADE;
DROP TABLE IF EXISTS hsm_slot_keys CASCADE;
DROP TABLE IF EXISTS hsm_operations CASCADE;
DROP TABLE IF EXISTS hsm_keys CASCADE;
DROP TABLE IF EXISTS hsm_audit CASCADE;
DROP TABLE IF EXISTS export_audit_log CASCADE;
DROP TABLE IF EXISTS device_locations CASCADE;
DROP TABLE IF EXISTS device_keys CASCADE;
DROP TABLE IF EXISTS data_encryption_keys CASCADE;
DROP TABLE IF EXISTS data_classification CASCADE;
DROP TABLE IF EXISTS active_sessions CASCADE;

