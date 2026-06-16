-- Rollback: Observer monitoring and dispute resolution

DROP TABLE IF EXISTS dispute_comments CASCADE;
DROP TABLE IF EXISTS disputes CASCADE;
DROP TABLE IF EXISTS observer_reports CASCADE;
DROP TABLE IF EXISTS observer_photo_verifications CASCADE;
DROP TABLE IF EXISTS observer_check_ins CASCADE;
DROP TABLE IF EXISTS observer_alert_rules CASCADE;

