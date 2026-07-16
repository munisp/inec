-- Rollback: Middleware persistence: event bus, state, cache, ledger

DROP TABLE IF EXISTS event_bus_topics CASCADE;
DROP TABLE IF EXISTS event_bus CASCADE;
DROP TABLE IF EXISTS mw_mojaloop_transactions CASCADE;
DROP TABLE IF EXISTS mw_ledger_transfers CASCADE;
DROP TABLE IF EXISTS mw_ledger_accounts CASCADE;
DROP TABLE IF EXISTS mw_circuit_breaker_log CASCADE;
DROP TABLE IF EXISTS mw_waf_events CASCADE;
DROP TABLE IF EXISTS mw_workflows CASCADE;
DROP TABLE IF EXISTS mw_search_index CASCADE;
DROP TABLE IF EXISTS mw_consumer_offsets CASCADE;
DROP TABLE IF EXISTS mw_streams CASCADE;
DROP TABLE IF EXISTS mw_pubsub CASCADE;
DROP TABLE IF EXISTS mw_events CASCADE;
DROP TABLE IF EXISTS mw_cache CASCADE;
DROP TABLE IF EXISTS mw_state CASCADE;

