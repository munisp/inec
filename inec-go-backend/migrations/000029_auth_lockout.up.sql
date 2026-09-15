-- R4-08: persistent login-attempt lockout state for internal/auth.Service.
-- MaxLoginAttempts/LockoutDuration were configured but never enforced; these
-- columns back the enforcement (increment on failure, lock at threshold,
-- clear on success).
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS failed_login_attempts INTEGER NOT NULL DEFAULT 0;

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
