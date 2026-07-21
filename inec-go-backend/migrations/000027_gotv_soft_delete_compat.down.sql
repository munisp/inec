DROP INDEX IF EXISTS idx_gotv_pledges_active_party;
DROP INDEX IF EXISTS idx_gotv_volunteers_active_party;
DROP INDEX IF EXISTS idx_gotv_contacts_active_party;

ALTER TABLE gotv_pledges
    DROP COLUMN IF EXISTS deleted_at;

ALTER TABLE gotv_volunteers
    DROP COLUMN IF EXISTS deleted_at;

ALTER TABLE gotv_contacts
    DROP COLUMN IF EXISTS deleted_at;
