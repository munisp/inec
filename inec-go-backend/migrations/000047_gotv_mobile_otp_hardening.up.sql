-- R5-042: GOTV mobile OTP hardening — state for real brute-force controls.
-- otp_last_sent_at:      send cooldown (no instant re-issue)
-- otp_locked_until:      lockout after max verify attempts (survives re-request)
-- otp_request_count /
-- otp_request_window_start: real per-phone request rate limit (the old
--      "rate limit" counted user rows per phone — always <=1, dead code)
ALTER TABLE gotv_mobile_users ADD COLUMN IF NOT EXISTS otp_last_sent_at TIMESTAMP;
ALTER TABLE gotv_mobile_users ADD COLUMN IF NOT EXISTS otp_locked_until TIMESTAMP;
ALTER TABLE gotv_mobile_users ADD COLUMN IF NOT EXISTS otp_request_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE gotv_mobile_users ADD COLUMN IF NOT EXISTS otp_request_window_start TIMESTAMP;
