-- Middleware persistence tables
-- Auto-generated from actual PostgreSQL schema

CREATE TABLE IF NOT EXISTS waf_blocklist (
    id SERIAL PRIMARY KEY,
    ip_address text NOT NULL,
    reason text,
    blocked_at text
);

CREATE INDEX IF NOT EXISTS idx_waf_ip ON waf_blocklist USING btree (ip_address);

