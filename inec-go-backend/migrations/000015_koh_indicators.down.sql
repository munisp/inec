-- Migration 000015 DOWN: KOH Indicators / GOTV analytics rollup tables
-- Added in R4-39c remediation: this down was missing, so `migrate down`
-- left 10 gotv_* tables behind. Drops every table created by the up
-- (seed rows in gotv_lga_tiers are removed with the table).

DROP TABLE IF EXISTS gotv_platform_analytics CASCADE;
DROP TABLE IF EXISTS gotv_reports_generated CASCADE;
DROP TABLE IF EXISTS gotv_lga_tiers CASCADE;
DROP TABLE IF EXISTS gotv_defections CASCADE;
DROP TABLE IF EXISTS gotv_endorsements CASCADE;
DROP TABLE IF EXISTS gotv_sentiment_log CASCADE;
DROP TABLE IF EXISTS gotv_social_metrics CASCADE;
DROP TABLE IF EXISTS gotv_survey_responses CASCADE;
DROP TABLE IF EXISTS gotv_surveys CASCADE;
DROP TABLE IF EXISTS gotv_cpi_history CASCADE;
