-- Migration 000019 DOWN: Party Primaries & Remote Voting Infrastructure
-- Added in R4-39c remediation: this down was missing, so `migrate down`
-- left 14 tables behind and never restored the elections.election_type
-- CHECK constraint to its 000001 definition.
--
-- NOTE: restoring the original constraint will fail if rows using the
-- primaries-only election types still exist in `elections`; remove or
-- reclassify those rows before rolling back.

-- 1. Restore the original election_type constraint (see 000001_initial_schema.up.sql:34)
ALTER TABLE elections DROP CONSTRAINT IF EXISTS elections_election_type_check;
ALTER TABLE elections ADD CONSTRAINT elections_election_type_check CHECK (
    election_type = ANY (ARRAY[
        'presidential','gubernatorial','senatorial','house_of_reps',
        'state_assembly','local_government'
    ])
);

-- 2. Drop the tables created by the up (children before parents; CASCADE
--    covers any stray FKs added elsewhere)
DROP TABLE IF EXISTS primary_disputes CASCADE;
DROP TABLE IF EXISTS shuffle_records CASCADE;
DROP TABLE IF EXISTS encrypted_tallies CASCADE;
DROP TABLE IF EXISTS voting_crypto_keys CASCADE;
DROP TABLE IF EXISTS convention_audit_log CASCADE;
DROP TABLE IF EXISTS quorum_snapshots CASCADE;
DROP TABLE IF EXISTS voting_sessions CASCADE;
DROP TABLE IF EXISTS remote_voting_devices CASCADE;
DROP TABLE IF EXISTS vote_tallies CASCADE;
DROP TABLE IF EXISTS ballots CASCADE;
DROP TABLE IF EXISTS voting_rounds CASCADE;
DROP TABLE IF EXISTS convention_venues CASCADE;
DROP TABLE IF EXISTS delegates CASCADE;
DROP TABLE IF EXISTS aspirants CASCADE;
