-- Migration 000016: Platform Improvements
-- Adds: soft delete, FK constraints, scoring persistence, social inbox,
-- user preferences, WhatsApp inbound, federated learning tables

BEGIN;

-- ═══════════════════════════════════════════════════════════════════════════
-- P0: Soft Delete — add deleted_at to all GOTV tables
-- ═══════════════════════════════════════════════════════════════════════════

ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_campaigns ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_pledges ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_tasks ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_ride_requests ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_door_knocks ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_field_reports ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_endorsements ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_surveys ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL;

-- Partial indexes for soft-delete queries
CREATE INDEX IF NOT EXISTS idx_contacts_active ON gotv_contacts(party_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_volunteers_active ON gotv_volunteers(party_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_active ON gotv_tasks(party_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_campaigns_active ON gotv_campaigns(party_id) WHERE deleted_at IS NULL;

-- ═══════════════════════════════════════════════════════════════════════════
-- P1: Scoring Persistence
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS gotv_contact_scores (
    id              SERIAL PRIMARY KEY,
    contact_id      TEXT NOT NULL,
    party_id        INT NOT NULL,
    score           NUMERIC(5,1) NOT NULL,
    engagement      NUMERIC(5,1) DEFAULT 0,
    recency         NUMERIC(5,1) DEFAULT 0,
    responsiveness  NUMERIC(5,1) DEFAULT 0,
    loyalty         NUMERIC(5,1) DEFAULT 0,
    mobilization    NUMERIC(5,1) DEFAULT 0,
    model_version   TEXT NOT NULL DEFAULT 'v1.0',
    computed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(contact_id, party_id)
);

CREATE TABLE IF NOT EXISTS gotv_scoring_runs (
    id              SERIAL PRIMARY KEY,
    party_id        INT NOT NULL,
    model_version   TEXT NOT NULL,
    contacts_scored INT NOT NULL DEFAULT 0,
    parameters      JSONB DEFAULT '{}',
    started_at      TIMESTAMPTZ NOT NULL,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_scores_party ON gotv_contact_scores(party_id, score DESC);
CREATE INDEX IF NOT EXISTS idx_scoring_runs ON gotv_scoring_runs(party_id, created_at DESC);

-- ═══════════════════════════════════════════════════════════════════════════
-- P3: Social Media Command Center
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS gotv_social_inbox (
    id          SERIAL PRIMARY KEY,
    party_id    INT NOT NULL,
    platform    TEXT NOT NULL, -- twitter, facebook, whatsapp, instagram
    author      TEXT NOT NULL,
    message     TEXT NOT NULL,
    sentiment   TEXT DEFAULT 'neutral', -- positive, negative, neutral
    status      TEXT DEFAULT 'unread', -- unread, read, responded, escalated
    response    TEXT,
    responded_at TIMESTAMPTZ,
    media_urls  TEXT[],
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_social_inbox_party ON gotv_social_inbox(party_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_social_inbox_platform ON gotv_social_inbox(platform, created_at DESC);

-- ═══════════════════════════════════════════════════════════════════════════
-- P3: Dashboard Widget Preferences
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS gotv_user_preferences (
    id              SERIAL PRIMARY KEY,
    party_id        INT NOT NULL,
    user_id         TEXT NOT NULL,
    preference_key  TEXT NOT NULL,
    preference_value TEXT NOT NULL,
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(party_id, user_id, preference_key)
);

-- ═══════════════════════════════════════════════════════════════════════════
-- P3: WhatsApp Two-Way Conversations
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS gotv_whatsapp_inbound (
    id          SERIAL PRIMARY KEY,
    phone       TEXT NOT NULL,
    message     TEXT NOT NULL,
    action      TEXT NOT NULL, -- confirm_pledge, request_ride, opt_out, unknown
    party_id    INT,
    contact_id  TEXT,
    processed_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_wa_inbound_phone ON gotv_whatsapp_inbound(phone, processed_at DESC);

-- ═══════════════════════════════════════════════════════════════════════════
-- P4: Team Gamification
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS gotv_team_competitions (
    id          SERIAL PRIMARY KEY,
    party_id    INT NOT NULL,
    name        TEXT NOT NULL,
    group_by    TEXT NOT NULL DEFAULT 'ward', -- ward, lga, state
    start_date  DATE NOT NULL,
    end_date    DATE NOT NULL,
    prize_desc  TEXT,
    is_active   BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS gotv_volunteer_badges (
    id           SERIAL PRIMARY KEY,
    volunteer_id TEXT NOT NULL,
    party_id     INT NOT NULL,
    badge_name   TEXT NOT NULL,
    badge_icon   TEXT NOT NULL,
    earned_at    TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(volunteer_id, badge_name)
);

-- ═══════════════════════════════════════════════════════════════════════════
-- P4: Simulation History
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS gotv_simulations (
    id          SERIAL PRIMARY KEY,
    party_id    INT NOT NULL,
    scenario    TEXT NOT NULL,
    parameters  JSONB NOT NULL,
    results     JSONB NOT NULL,
    created_by  TEXT,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- ═══════════════════════════════════════════════════════════════════════════
-- P4: NL Query History (for improving query matching)
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS gotv_nl_queries (
    id          SERIAL PRIMARY KEY,
    party_id    INT NOT NULL,
    query_text  TEXT NOT NULL,
    matched_label TEXT,
    result_value TEXT,
    feedback    TEXT, -- correct, incorrect, null
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- ═══════════════════════════════════════════════════════════════════════════
-- P4: Federated Learning Rounds
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS gotv_fl_rounds (
    id              SERIAL PRIMARY KEY,
    round_number    INT NOT NULL,
    model_type      TEXT NOT NULL,
    participants    INT NOT NULL DEFAULT 0,
    global_accuracy NUMERIC(5,2),
    epsilon         NUMERIC(5,2) DEFAULT 1.0,
    status          TEXT DEFAULT 'pending',
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

-- ═══════════════════════════════════════════════════════════════════════════
-- Add media_url column to field reports
-- ═══════════════════════════════════════════════════════════════════════════

ALTER TABLE gotv_field_reports ADD COLUMN IF NOT EXISTS media_url TEXT;
ALTER TABLE gotv_field_reports ADD COLUMN IF NOT EXISTS latitude NUMERIC(9,6);
ALTER TABLE gotv_field_reports ADD COLUMN IF NOT EXISTS longitude NUMERIC(9,6);
ALTER TABLE gotv_field_reports ADD COLUMN IF NOT EXISTS reviewed_by TEXT;
ALTER TABLE gotv_field_reports ADD COLUMN IF NOT EXISTS review_status TEXT DEFAULT 'pending';

-- ═══════════════════════════════════════════════════════════════════════════
-- Data retention policy table
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS gotv_data_retention_policy (
    id              SERIAL PRIMARY KEY,
    table_name      TEXT NOT NULL UNIQUE,
    retention_days  INT NOT NULL DEFAULT 365,
    purge_type      TEXT DEFAULT 'soft_delete', -- soft_delete, hard_delete, archive
    last_purge_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

INSERT INTO gotv_data_retention_policy (table_name, retention_days, purge_type) VALUES
    ('gotv_outreach_log', 180, 'archive'),
    ('gotv_door_knocks', 365, 'soft_delete'),
    ('gotv_sentiment_log', 90, 'hard_delete'),
    ('gotv_dead_letter_queue', 30, 'hard_delete'),
    ('gotv_whatsapp_inbound', 180, 'archive'),
    ('gotv_nl_queries', 90, 'hard_delete'),
    ('gotv_audit_log', 730, 'archive')
ON CONFLICT (table_name) DO NOTHING;

COMMIT;
