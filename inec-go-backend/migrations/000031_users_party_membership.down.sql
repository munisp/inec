-- Rollback for 000031 (R4-05).
DROP INDEX IF EXISTS idx_users_party;
ALTER TABLE users DROP COLUMN IF EXISTS party_id;
