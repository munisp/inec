-- R5-049(a)/R5-040: accreditation dedupe and bvas-svc persistence fixes.
--
-- 1. Cross-PU dedupe: the W1 constraint (voter_pvc_hash, election_id,
--    polling_unit_code) only stopped the same voter re-accrediting at the
--    SAME polling unit. One voter = one accreditation per election,
--    nationwide (Electoral Act s.47 — accreditation count caps votes).
--    Historical cross-PU duplicates are removed keeping the earliest row.
DELETE FROM bvas_accreditations a
USING bvas_accreditations b
WHERE a.voter_pvc_hash = b.voter_pvc_hash
  AND a.election_id = b.election_id
  AND a.id > b.id;

CREATE UNIQUE INDEX IF NOT EXISTS bvas_accreditations_voter_election_unique
    ON bvas_accreditations (voter_pvc_hash, election_id);

-- 2. The bvas-svc accreditation path INSERTs a voter_vin column that never
--    existed in the canonical table — every write failed and the service
--    discarded the error, returning "accredited" for nothing (R5-040).
ALTER TABLE bvas_accreditations ADD COLUMN IF NOT EXISTS voter_vin TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS bvas_accreditations_vin_election_unique
    ON bvas_accreditations (election_id, voter_vin) WHERE voter_vin IS NOT NULL;

-- 3. Single-use biometric verifications (R5-049(b)): the legacy staff path
--    accepted any verification_id with result='match', replayable against
--    arbitrary PVCs. consumed_at marks a verification as spent.
ALTER TABLE biometric_verifications ADD COLUMN IF NOT EXISTS consumed_at TIMESTAMP;
