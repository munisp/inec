-- Ensure every GOTV entity queried through soft-delete filters has a compatible
-- timestamp column. Existing rows remain active until an explicit deletion sets it.
ALTER TABLE gotv_contacts
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE gotv_volunteers
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE gotv_pledges
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_gotv_contacts_active_party
    ON gotv_contacts (party_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_gotv_volunteers_active_party
    ON gotv_volunteers (party_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_gotv_pledges_active_party
    ON gotv_pledges (party_id, created_at DESC)
    WHERE deleted_at IS NULL;
