-- Rollback for 000028 (R4-22): drop the duplicate-prevention constraint.
ALTER TABLE results
    DROP CONSTRAINT IF EXISTS results_election_pu_unique;
