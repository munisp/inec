-- R4-01: allow gotv_campaigns.status = 'failed'.
-- The dispatch engine now fails a campaign LOUDLY (status='failed' + per-message
-- error) when no channel provider is configured, instead of silently logging
-- messages as "delivered" via the LogAdapter fallback. The old CHECK constraint
-- did not include 'failed' and would have rejected the honest status write.
ALTER TABLE gotv_campaigns DROP CONSTRAINT IF EXISTS gotv_campaigns_status_check;

ALTER TABLE gotv_campaigns
    ADD CONSTRAINT gotv_campaigns_status_check
    CHECK (status IN ('draft','scheduled','active','paused','completed','cancelled','failed'));
