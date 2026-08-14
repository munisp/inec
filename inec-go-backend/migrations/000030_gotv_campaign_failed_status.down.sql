-- Rollback for 000030 (R4-01): restore the original status set.
-- Note: rows already marked 'failed' would violate the restored constraint;
-- reclassify them to 'cancelled' first.
UPDATE gotv_campaigns SET status='cancelled' WHERE status='failed';

ALTER TABLE gotv_campaigns DROP CONSTRAINT IF EXISTS gotv_campaigns_status_check;

ALTER TABLE gotv_campaigns
    ADD CONSTRAINT gotv_campaigns_status_check
    CHECK (status IN ('draft','scheduled','active','paused','completed','cancelled'));
