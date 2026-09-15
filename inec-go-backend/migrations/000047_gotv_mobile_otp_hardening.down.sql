ALTER TABLE gotv_mobile_users DROP COLUMN IF EXISTS otp_last_sent_at;
ALTER TABLE gotv_mobile_users DROP COLUMN IF EXISTS otp_locked_until;
ALTER TABLE gotv_mobile_users DROP COLUMN IF EXISTS otp_request_count;
ALTER TABLE gotv_mobile_users DROP COLUMN IF EXISTS otp_request_window_start;
