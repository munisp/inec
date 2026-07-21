-- Rollback 000026: GOTV operational schema reconciliation
--
-- This migration removes the schema objects introduced by 000026 in reverse
-- dependency order. It is intentionally destructive and should be used only
-- for controlled rollback environments.

DROP INDEX IF EXISTS idx_incidents_election_status_reported;
DROP INDEX IF EXISTS idx_results_election_validated;
DROP INDEX IF EXISTS idx_results_election_submitted;

-- GOTV reconciliation tables.
DROP TABLE IF EXISTS gotv_federated_rounds;
DROP TABLE IF EXISTS gotv_federated_participants;
DROP TABLE IF EXISTS gotv_social_inbox;
DROP TABLE IF EXISTS gotv_crowd_estimates;
DROP TABLE IF EXISTS gotv_user_preferences;
DROP TABLE IF EXISTS gotv_whatsapp_inbound;
DROP TABLE IF EXISTS gotv_scoring_runs;
DROP TABLE IF EXISTS gotv_contact_scores;

-- GOTV v2 operational tables.
DROP TABLE IF EXISTS gotv_tasks;
DROP TABLE IF EXISTS gotv_voice_calls;
DROP TABLE IF EXISTS gotv_alliances;
DROP TABLE IF EXISTS gotv_field_reports;
DROP TABLE IF EXISTS gotv_pledge_hashes;
DROP TABLE IF EXISTS gotv_ai_variants;
DROP TABLE IF EXISTS gotv_challenges;
DROP TABLE IF EXISTS gotv_territories;
DROP TABLE IF EXISTS gotv_segments;
DROP TABLE IF EXISTS gotv_campaign_sequences;

-- Canonical GOTV base schema, ordered by foreign-key dependencies.
DROP TABLE IF EXISTS gotv_mobile_users;
DROP TABLE IF EXISTS gotv_shifts;
DROP TABLE IF EXISTS gotv_door_knocks;
DROP TABLE IF EXISTS gotv_import_log;
DROP TABLE IF EXISTS gotv_audit_log;
DROP TABLE IF EXISTS gotv_dead_letter_queue;
DROP TABLE IF EXISTS gotv_outreach_log;
DROP TABLE IF EXISTS gotv_ride_requests;
DROP TABLE IF EXISTS gotv_pledges;
DROP TABLE IF EXISTS gotv_volunteers;
DROP TABLE IF EXISTS gotv_contacts;
DROP TABLE IF EXISTS gotv_campaigns;
DROP TABLE IF EXISTS gotv_party_access;

-- Stablecoin compatibility additions.
DROP TABLE IF EXISTS stablecoin_ledger;
DROP INDEX IF EXISTS idx_stablecoin_ledger_wallet_created;
DROP INDEX IF EXISTS idx_stablecoin_transactions_tx_id;
DROP INDEX IF EXISTS idx_stablecoin_tx_wallets;
DROP INDEX IF EXISTS idx_stablecoin_tx_status;
DROP TRIGGER IF EXISTS trg_sync_stablecoin_transaction_identifiers ON stablecoin_transactions;
DROP FUNCTION IF EXISTS sync_stablecoin_transaction_identifiers();
ALTER TABLE IF EXISTS stablecoin_transactions DROP COLUMN IF EXISTS confirmed_at;
ALTER TABLE IF EXISTS stablecoin_transactions DROP COLUMN IF EXISTS metadata;
ALTER TABLE IF EXISTS stablecoin_transactions DROP COLUMN IF EXISTS block_hash;
ALTER TABLE IF EXISTS stablecoin_transactions DROP COLUMN IF EXISTS signature;
ALTER TABLE IF EXISTS stablecoin_transactions DROP COLUMN IF EXISTS tx_type;
ALTER TABLE IF EXISTS stablecoin_transactions DROP COLUMN IF EXISTS to_wallet;
ALTER TABLE IF EXISTS stablecoin_transactions DROP COLUMN IF EXISTS from_wallet;
ALTER TABLE IF EXISTS stablecoin_transactions DROP COLUMN IF EXISTS tx_id;
DROP TABLE IF EXISTS stablecoin_wallets;
