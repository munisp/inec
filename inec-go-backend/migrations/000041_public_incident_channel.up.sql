-- R5-073 / R5-078: public voter complaint & incident channel.
-- Extends the officer incident pipeline with a source marker and optional
-- reporter contact so unauthenticated web/USSD/IVR/WhatsApp reports route
-- into the same triage queue, and adds a WhatsApp message audit table.

ALTER TABLE incidents ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'officer';
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS reporter_name TEXT;
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS reporter_phone TEXT;
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS reporter_email TEXT;

CREATE INDEX IF NOT EXISTS idx_incidents_source ON incidents (source, reported_at DESC);

CREATE TABLE IF NOT EXISTS whatsapp_messages (
    id BIGSERIAL PRIMARY KEY,
    message_id TEXT NOT NULL UNIQUE,
    sender TEXT NOT NULL,
    direction TEXT NOT NULL DEFAULT 'inbound',
    body TEXT,
    reply TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_whatsapp_messages_sender ON whatsapp_messages (sender, created_at DESC);
