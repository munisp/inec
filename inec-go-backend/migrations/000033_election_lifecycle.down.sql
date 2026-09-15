-- Rollback for 000033_election_lifecycle.
ALTER TABLE disputes DROP CONSTRAINT IF EXISTS disputes_outcome_check;
ALTER TABLE disputes DROP COLUMN IF EXISTS outcome;
ALTER TABLE disputes DROP COLUMN IF EXISTS result_id;

DROP TABLE IF EXISTS rerun_scopes;
ALTER TABLE elections DROP CONSTRAINT IF EXISTS elections_kind_check;
ALTER TABLE elections DROP COLUMN IF EXISTS election_kind;
ALTER TABLE elections DROP COLUMN IF EXISTS parent_election_id;

DROP TABLE IF EXISTS result_corrections;
DROP INDEX IF EXISTS results_idempotency_key_unique;
ALTER TABLE results DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE results DROP COLUMN IF EXISTS correction_reason;
ALTER TABLE results DROP COLUMN IF EXISTS supersedes_result_id;
DROP INDEX IF EXISTS results_canonical_pu_unique;
ALTER TABLE results ADD CONSTRAINT results_election_pu_unique UNIQUE (election_id, polling_unit_code);
ALTER TABLE results DROP CONSTRAINT IF EXISTS results_status_check;
ALTER TABLE results ADD CONSTRAINT results_status_check CHECK (status = ANY (ARRAY[
    'pending','validated','finalized','disputed','voided'
]));

ALTER TABLE elections DROP COLUMN IF EXISTS declaration_notes;
ALTER TABLE elections DROP COLUMN IF EXISTS winner_payload;
ALTER TABLE elections DROP COLUMN IF EXISTS declared_by;
ALTER TABLE elections DROP COLUMN IF EXISTS declared_at;

ALTER TABLE elections DROP CONSTRAINT IF EXISTS elections_status_check;
ALTER TABLE elections ADD CONSTRAINT elections_status_check CHECK (status = ANY (ARRAY[
    'draft','scheduled','upcoming','active','voting','collating','closed',
    'completed','cancelled','disputed'
]));
