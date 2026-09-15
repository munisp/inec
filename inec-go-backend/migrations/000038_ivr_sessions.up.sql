-- R5-072 / R5-120 (sandbox sub-action): durable IVR session store.
-- Replaces the process-local in-memory IVR session map so telephony
-- callbacks work across replicas and restarts.

CREATE TABLE IF NOT EXISTS ivr_sessions (
    session_id TEXT PRIMARY KEY,
    caller_phone TEXT,
    language TEXT NOT NULL DEFAULT 'en',
    state TEXT NOT NULL DEFAULT 'menu',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_action TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ivr_sessions_last_action ON ivr_sessions (last_action);
