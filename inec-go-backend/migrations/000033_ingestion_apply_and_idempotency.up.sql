-- R5-001/R5-002/R5-003: canonical ingestion apply path + durable idempotency.
--
-- 1. ingestion_jobs: enforce uniqueness on id and idempotency_key. Migration
--    000012 created the table without constraints, so the ON CONFLICT
--    idempotency arbiter could not work on PostgreSQL and restarts/replicas
--    re-processed jobs.
-- 2. offline_sync_queue: idempotency key + apply outcome columns so an
--    offline-sync item is only 'synced' after the canonical apply succeeds.
-- 3. bvas_accreditations: uniqueness on (voter_pvc_hash, election_id,
--    polling_unit_code) so offline/gateway accreditation dedupe is enforced
--    by the database instead of a best-effort pre-check.

-- Dedupe guards: keep the earliest row per key before constraining.
DELETE FROM ingestion_jobs a
USING ingestion_jobs b
WHERE a.id = b.id
  AND a.ctid > b.ctid;

DELETE FROM ingestion_jobs a
USING ingestion_jobs b
WHERE a.idempotency_key IS NOT NULL
  AND a.idempotency_key = b.idempotency_key
  AND a.ctid > b.ctid;

CREATE UNIQUE INDEX IF NOT EXISTS ingestion_jobs_id_unique
    ON ingestion_jobs (id);

CREATE UNIQUE INDEX IF NOT EXISTS ingestion_jobs_idempotency_key_unique
    ON ingestion_jobs (idempotency_key);

ALTER TABLE offline_sync_queue
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT,
    ADD COLUMN IF NOT EXISTS error_message TEXT,
    ADD COLUMN IF NOT EXISTS result_id INTEGER;

CREATE UNIQUE INDEX IF NOT EXISTS offline_sync_queue_idempotency_key_unique
    ON offline_sync_queue (idempotency_key);

DELETE FROM bvas_accreditations a
USING bvas_accreditations b
WHERE a.voter_pvc_hash = b.voter_pvc_hash
  AND a.election_id = b.election_id
  AND a.polling_unit_code = b.polling_unit_code
  AND a.id > b.id;

CREATE UNIQUE INDEX IF NOT EXISTS bvas_accreditations_voter_election_pu_unique
    ON bvas_accreditations (voter_pvc_hash, election_id, polling_unit_code);
