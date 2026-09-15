ALTER TABLE bvas_devices DROP COLUMN IF EXISTS revoked_at;
ALTER TABLE bvas_devices DROP COLUMN IF EXISTS revoked_by;
ALTER TABLE bvas_devices DROP COLUMN IF EXISTS revoke_reason;
