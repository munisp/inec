DROP TABLE IF EXISTS data_erasure_requests;
DROP TABLE IF EXISTS retention_archive_log;
DELETE FROM legal_holds WHERE placed_by = 'system'
    AND table_name IN ('audit_log', 'stakeholder_incidents');
DROP TABLE IF EXISTS legal_holds;
DROP INDEX IF EXISTS uq_result_signatures_result_id;
DROP TRIGGER IF EXISTS trg_result_signatures_no_delete ON result_signatures;
DROP TRIGGER IF EXISTS trg_result_signatures_no_update ON result_signatures;
DROP FUNCTION IF EXISTS reject_result_signature_mutation();
DROP TRIGGER IF EXISTS trg_audit_log_no_delete ON audit_log;
DROP TRIGGER IF EXISTS trg_audit_log_no_update ON audit_log;
DROP FUNCTION IF EXISTS reject_audit_log_mutation();
