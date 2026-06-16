-- KOH 2027 Indicators Framework — Database Migration
-- Adds 8 tables for: CPI tracking, surveys, social listening, endorsements,
-- LGA tiers, scheduled reports, platform analytics ingestion.

BEGIN;

-- Demographic enrichment on existing contacts
ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS age_group VARCHAR(20);
ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS gender VARCHAR(10);
ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS lga_code VARCHAR(20);
ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS lcda_code VARCHAR(20);
ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS lga_tier INTEGER;
ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS socioeconomic_class VARCHAR(5);
ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS occupation_group VARCHAR(30);
ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS education_level VARCHAR(20);
ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS religion VARCHAR(20);
ALTER TABLE gotv_contacts ADD COLUMN IF NOT EXISTS ethnicity VARCHAR(20);

-- Table 1: CPI History (Composite Popularity Index tracking)
CREATE TABLE IF NOT EXISTS gotv_cpi_history (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL,
    computed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    cpi_score NUMERIC(5,2) NOT NULL,
    voting_intention_pct NUMERIC(5,2),
    favourability_pct NUMERIC(5,2),
    digital_sentiment NUMERIC(5,2),
    ground_mobilisation NUMERIC(5,2),
    endorsement_index NUMERIC(5,2),
    share_of_voice NUMERIC(5,2),
    lga_code VARCHAR(20),
    demographic_filter JSONB,
    notes TEXT
);
CREATE INDEX IF NOT EXISTS idx_gotv_cpi_party ON gotv_cpi_history(party_id, computed_at);

-- Table 2: Surveys
CREATE TABLE IF NOT EXISTS gotv_surveys (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL,
    survey_name VARCHAR(255) NOT NULL,
    wave_number INTEGER DEFAULT 1,
    sample_size INTEGER,
    methodology VARCHAR(100),
    start_date DATE,
    end_date DATE,
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Table 3: Survey Responses
CREATE TABLE IF NOT EXISTS gotv_survey_responses (
    id SERIAL PRIMARY KEY,
    survey_id INTEGER NOT NULL REFERENCES gotv_surveys(id),
    party_id INTEGER NOT NULL,
    lga_code VARCHAR(20),
    lcda_code VARCHAR(20),
    age_group VARCHAR(20),
    gender VARCHAR(10),
    socioeconomic_class VARCHAR(5),
    occupation_group VARCHAR(30),
    education_level VARCHAR(20),
    religion VARCHAR(20),
    ethnicity VARCHAR(20),
    awareness_score NUMERIC(5,2),
    favourability_score NUMERIC(5,2),
    voting_intention BOOLEAN,
    issue_alignment NUMERIC(5,2),
    message_recall BOOLEAN,
    nps_score INTEGER,
    net_promoter VARCHAR(20),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_gotv_survey_resp_survey ON gotv_survey_responses(survey_id);
CREATE INDEX IF NOT EXISTS idx_gotv_survey_resp_party ON gotv_survey_responses(party_id);

-- Table 4: Social Metrics (aggregated platform data)
CREATE TABLE IF NOT EXISTS gotv_social_metrics (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL,
    date DATE NOT NULL,
    platform VARCHAR(30) NOT NULL,
    metric_type VARCHAR(50) NOT NULL,
    value NUMERIC(12,2),
    demographic_filter JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_gotv_social_metrics_date ON gotv_social_metrics(party_id, date);

-- Table 5: Sentiment Log (individual mentions)
CREATE TABLE IF NOT EXISTS gotv_sentiment_log (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    platform VARCHAR(30),
    mention_text TEXT,
    sentiment VARCHAR(20),
    sentiment_score NUMERIC(5,2),
    candidate_mentioned VARCHAR(100),
    topic VARCHAR(100),
    url TEXT
);
CREATE INDEX IF NOT EXISTS idx_gotv_sentiment_ts ON gotv_sentiment_log(party_id, timestamp);

-- Table 6: Endorsements
CREATE TABLE IF NOT EXISTS gotv_endorsements (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL,
    endorser_name VARCHAR(255) NOT NULL,
    endorser_type VARCHAR(50) NOT NULL,
    endorser_category VARCHAR(50),
    lga_code VARCHAR(20),
    demographic_reach INTEGER,
    date_endorsed DATE,
    public_statement TEXT,
    verified BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_gotv_endorsements_party ON gotv_endorsements(party_id);

-- Table 7: Defections
CREATE TABLE IF NOT EXISTS gotv_defections (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL,
    defector_name VARCHAR(255) NOT NULL,
    from_party VARCHAR(100),
    to_party VARCHAR(100),
    defection_date DATE,
    lga_code VARCHAR(20),
    is_public BOOLEAN DEFAULT TRUE,
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Table 8: LGA Strategic Tiers (reference data)
CREATE TABLE IF NOT EXISTS gotv_lga_tiers (
    id SERIAL PRIMARY KEY,
    lga_code VARCHAR(20) UNIQUE NOT NULL,
    lga_name VARCHAR(100) NOT NULL,
    tier INTEGER NOT NULL,
    tier_name VARCHAR(50) NOT NULL,
    strategic_focus TEXT,
    party_id INTEGER
);

-- Table 9: Generated Reports
CREATE TABLE IF NOT EXISTS gotv_reports_generated (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL,
    report_type VARCHAR(50) NOT NULL,
    frequency VARCHAR(20) NOT NULL,
    generated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    data_snapshot JSONB,
    period_start DATE,
    period_end DATE,
    status VARCHAR(20) DEFAULT 'generated'
);
CREATE INDEX IF NOT EXISTS idx_gotv_reports_party ON gotv_reports_generated(party_id, generated_at);

-- Table 10: Platform Analytics (ingested from Meta/TikTok/X/YouTube)
CREATE TABLE IF NOT EXISTS gotv_platform_analytics (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL,
    date DATE NOT NULL,
    platform VARCHAR(30) NOT NULL,
    followers INTEGER,
    follower_growth_pct NUMERIC(5,2),
    total_reach INTEGER,
    organic_reach INTEGER,
    paid_reach INTEGER,
    engagement_rate NUMERIC(5,4),
    video_completion_rate NUMERIC(5,4),
    impressions INTEGER,
    clicks INTEGER,
    shares INTEGER,
    comments INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_gotv_platform_analytics ON gotv_platform_analytics(party_id, date, platform);

-- Seed Lagos LGA Tiers
INSERT INTO gotv_lga_tiers (lga_code, lga_name, tier, tier_name, strategic_focus, party_id) VALUES
    ('alimosho', 'Alimosho', 1, 'Stronghold', 'Maximise turnout; defend existing support base', 1),
    ('mushin', 'Mushin', 1, 'Stronghold', 'Maximise turnout; defend existing support base', 1),
    ('agege', 'Agege', 1, 'Stronghold', 'Maximise turnout; defend existing support base', 1),
    ('oshodi-isolo', 'Oshodi-Isolo', 1, 'Stronghold', 'Maximise turnout; defend existing support base', 1),
    ('ajeromi-ifelodun', 'Ajeromi-Ifelodun', 1, 'Stronghold', 'Maximise turnout; defend existing support base', 1),
    ('kosofe', 'Kosofe', 2, 'Swing', 'Convert undecided voters; build margins', 1),
    ('somolu', 'Somolu', 2, 'Swing', 'Convert undecided voters; build margins', 1),
    ('lagos-mainland', 'Lagos Mainland', 2, 'Swing', 'Convert undecided voters; build margins', 1),
    ('ifako-ijaiye', 'Ifako-Ijaiye', 2, 'Swing', 'Convert undecided voters; build margins', 1),
    ('surulere', 'Surulere', 2, 'Swing', 'Convert undecided voters; build margins', 1),
    ('ojo', 'Ojo', 2, 'Swing', 'Convert undecided voters; build margins', 1),
    ('ikorodu', 'Ikorodu', 3, 'Growth', 'Expand footprint; community-level penetration', 1),
    ('badagry', 'Badagry', 3, 'Growth', 'Expand footprint; community-level penetration', 1),
    ('epe', 'Epe', 3, 'Growth', 'Expand footprint; community-level penetration', 1),
    ('ibeju-lekki', 'Ibeju-Lekki', 3, 'Growth', 'Expand footprint; community-level penetration', 1),
    ('ikeja', 'Ikeja', 4, 'Urban Centre', 'Professional and upper-class voter engagement', 1),
    ('lagos-island', 'Lagos Island', 4, 'Urban Centre', 'Professional and upper-class voter engagement', 1),
    ('eti-osa', 'Eti-Osa', 4, 'Urban Centre', 'Professional and upper-class voter engagement', 1),
    ('apapa', 'Apapa', 4, 'Urban Centre', 'Professional and upper-class voter engagement', 1)
ON CONFLICT (lga_code) DO NOTHING;

COMMIT;
