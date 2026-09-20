-- Reverse 000050: demote any rows holding the extended roles back to
-- 'public' (the only safe generic role), then restore the original CHECK.
UPDATE users SET role = 'public'
    WHERE role IN ('returning_officer', 'ict_officer', 'security', 'dpo', 'officer');
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('admin', 'presiding_officer', 'collation_officer', 'observer', 'public'));
