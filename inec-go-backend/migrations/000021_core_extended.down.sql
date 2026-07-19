-- Rollback: Core extended tables: voters, audit, collation, validation

DROP TABLE IF EXISTS polling_unit_locations CASCADE;
DROP TABLE IF EXISTS token_blacklist CASCADE;
DROP TABLE IF EXISTS tb_transfers CASCADE;
DROP TABLE IF EXISTS tb_journal CASCADE;
DROP TABLE IF EXISTS tb_accounts CASCADE;
DROP TABLE IF EXISTS opensearch_documents CASCADE;
DROP TABLE IF EXISTS mojaloop_transactions CASCADE;
DROP TABLE IF EXISTS metrics_client CASCADE;
DROP TABLE IF EXISTS grievances CASCADE;
DROP TABLE IF EXISTS gps_spoof_events CASCADE;
DROP TABLE IF EXISTS escalation_rules CASCADE;
DROP TABLE IF EXISTS escalation_log CASCADE;
DROP TABLE IF EXISTS command_center_config CASCADE;
DROP TABLE IF EXISTS command_alerts CASCADE;
DROP TABLE IF EXISTS citizen_verifications CASCADE;
DROP TABLE IF EXISTS anomaly_escalations CASCADE;
DROP TABLE IF EXISTS validation_results CASCADE;
DROP TABLE IF EXISTS validation_rules CASCADE;
DROP TABLE IF EXISTS collation_party_scores CASCADE;
DROP TABLE IF EXISTS collation_results CASCADE;
DROP TABLE IF EXISTS audit_log CASCADE;
DROP TABLE IF EXISTS voters CASCADE;
