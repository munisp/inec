-- Rollback: API keys, webhooks, portal, push notifications

DROP TABLE IF EXISTS webhook_subscriptions CASCADE;
DROP TABLE IF EXISTS push_notifications CASCADE;
DROP TABLE IF EXISTS push_devices CASCADE;
DROP TABLE IF EXISTS portal_webhooks CASCADE;
DROP TABLE IF EXISTS portal_sync_log CASCADE;
DROP TABLE IF EXISTS portal_connections CASCADE;
DROP TABLE IF EXISTS offline_sync_queue CASCADE;
DROP TABLE IF EXISTS ingestion_jobs CASCADE;
DROP TABLE IF EXISTS dead_letter_queue CASCADE;
DROP TABLE IF EXISTS api_key_metadata CASCADE;
DROP TABLE IF EXISTS api_usage CASCADE;
DROP TABLE IF EXISTS api_keys CASCADE;

