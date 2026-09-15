-- R5-048: device fleet revocation metadata. A lost/stolen/misused BVAS
-- device must be revocable with an auditable reason; accreditation already
-- fails closed on any status other than 'active' (bvas.go:381+,
-- internal/bvas/service.go), so setting status='decommissioned'/'lost'
-- here immediately blocks the device everywhere.
ALTER TABLE bvas_devices ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMP;
ALTER TABLE bvas_devices ADD COLUMN IF NOT EXISTS revoked_by INTEGER;
ALTER TABLE bvas_devices ADD COLUMN IF NOT EXISTS revoke_reason TEXT;
