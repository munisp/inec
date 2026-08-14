-- R4-53: persistent store for web/mobile client error telemetry
-- (POST /api/v1/errors/frontend). Fields are pre-sanitized and length-capped
-- by the ingest handler; keep generous column sizes anyway.
CREATE TABLE IF NOT EXISTS frontend_errors (
    id          BIGSERIAL PRIMARY KEY,
    app         VARCHAR(32)  NOT NULL DEFAULT '',
    severity    VARCHAR(16)  NOT NULL DEFAULT 'error',
    message     VARCHAR(512) NOT NULL,
    stack       TEXT,
    url         VARCHAR(512),
    component   VARCHAR(512),
    context     TEXT,
    user_agent  VARCHAR(512),
    client_ip   VARCHAR(64),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_frontend_errors_created_at
    ON frontend_errors (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_frontend_errors_severity
    ON frontend_errors (severity, created_at DESC);
