-- P0 durable middleware-delivery provenance for external election device events.

CREATE TABLE IF NOT EXISTS external_integration_delivery_attempts (
    id BIGSERIAL PRIMARY KEY,
    outbox_id BIGINT NOT NULL REFERENCES external_integration_outbox(id) ON DELETE RESTRICT,
    correlation_id UUID NOT NULL,
    sink_name TEXT NOT NULL CHECK (sink_name IN ('kafka','dapr','fluvio','temporal')),
    attempt_no INTEGER NOT NULL CHECK (attempt_no > 0),
    outcome TEXT NOT NULL CHECK (outcome IN ('accepted','failed','unavailable')),
    external_reference TEXT,
    error_code TEXT,
    error_detail TEXT,
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (outbox_id, sink_name, attempt_no)
);

CREATE INDEX IF NOT EXISTS idx_external_delivery_attempt_outbox ON external_integration_delivery_attempts(outbox_id, attempted_at DESC);
CREATE INDEX IF NOT EXISTS idx_external_delivery_attempt_correlation ON external_integration_delivery_attempts(correlation_id, attempted_at DESC);

CREATE OR REPLACE FUNCTION reject_external_delivery_attempt_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'external integration delivery attempts are append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_external_delivery_attempts_immutable ON external_integration_delivery_attempts;
CREATE TRIGGER trg_external_delivery_attempts_immutable
BEFORE UPDATE OR DELETE ON external_integration_delivery_attempts
FOR EACH ROW EXECUTE FUNCTION reject_external_delivery_attempt_mutation();
