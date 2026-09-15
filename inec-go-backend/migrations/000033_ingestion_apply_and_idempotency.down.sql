DROP INDEX IF EXISTS bvas_accreditations_voter_election_pu_unique;
DROP INDEX IF EXISTS offline_sync_queue_idempotency_key_unique;

ALTER TABLE offline_sync_queue
    DROP COLUMN IF EXISTS result_id,
    DROP COLUMN IF EXISTS error_message,
    DROP COLUMN IF EXISTS idempotency_key;

DROP INDEX IF EXISTS ingestion_jobs_idempotency_key_unique;
DROP INDEX IF EXISTS ingestion_jobs_id_unique;
