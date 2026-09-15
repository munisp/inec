ALTER TABLE biometric_verifications DROP COLUMN IF EXISTS consumed_at;
DROP INDEX IF EXISTS bvas_accreditations_vin_election_unique;
ALTER TABLE bvas_accreditations DROP COLUMN IF EXISTS voter_vin;
DROP INDEX IF EXISTS bvas_accreditations_voter_election_unique;
