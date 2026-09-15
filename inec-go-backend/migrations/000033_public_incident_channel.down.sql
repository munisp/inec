DROP TABLE IF EXISTS whatsapp_messages;

DROP INDEX IF EXISTS idx_incidents_source;

ALTER TABLE incidents DROP COLUMN IF EXISTS reporter_email;
ALTER TABLE incidents DROP COLUMN IF EXISTS reporter_phone;
ALTER TABLE incidents DROP COLUMN IF EXISTS reporter_name;
ALTER TABLE incidents DROP COLUMN IF EXISTS source;
