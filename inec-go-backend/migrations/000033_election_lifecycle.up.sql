-- R5-011/013/015/016/018: election lifecycle core.
--
-- 1. Unify election status vocabulary across the monolith FSM and
--    election-svc (canonical set adds 'suspended', 'postponed', 'declared').
-- 2. Declaration persistence (declared_at / declared_by / winner payload).
-- 3. Correction/supersession: results may be superseded (never mutated);
--    the strict (election_id, polling_unit_code) UNIQUE becomes a partial
--    unique index over canonical (non-superseded) results only.
-- 4. Rerun / supplementary / by-election support: parent linkage + scope.
-- 5. Idempotency keys for result submission (R5-025).

-- 1. Election status vocabulary -------------------------------------------------
ALTER TABLE elections DROP CONSTRAINT IF EXISTS elections_status_check;
ALTER TABLE elections ADD CONSTRAINT elections_status_check CHECK (status = ANY (ARRAY[
    'draft','scheduled','upcoming','active','voting','collating','closed',
    'completed','cancelled','disputed','suspended','postponed','declared'
]));

-- 2. Declaration persistence ----------------------------------------------------
ALTER TABLE elections ADD COLUMN IF NOT EXISTS declared_at timestamptz;
ALTER TABLE elections ADD COLUMN IF NOT EXISTS declared_by text;
ALTER TABLE elections ADD COLUMN IF NOT EXISTS winner_payload jsonb;
ALTER TABLE elections ADD COLUMN IF NOT EXISTS declaration_notes text;

-- 3a. Result supersession status -------------------------------------------------
ALTER TABLE results DROP CONSTRAINT IF EXISTS results_status_check;
ALTER TABLE results ADD CONSTRAINT results_status_check CHECK (status = ANY (ARRAY[
    'pending','validated','finalized','disputed','voided','superseded'
]));

-- 3b. At most one canonical result per (election, polling unit): superseded and
--     voided rows are history and do not block a correction/resubmission.
ALTER TABLE results DROP CONSTRAINT IF EXISTS results_election_pu_unique;
DROP INDEX IF EXISTS results_election_pu_unique;
CREATE UNIQUE INDEX IF NOT EXISTS results_canonical_pu_unique
    ON results (election_id, polling_unit_code)
    WHERE status NOT IN ('superseded','voided');

ALTER TABLE results ADD COLUMN IF NOT EXISTS supersedes_result_id integer REFERENCES results(id);
ALTER TABLE results ADD COLUMN IF NOT EXISTS correction_reason text;
ALTER TABLE results ADD COLUMN IF NOT EXISTS idempotency_key text;
CREATE UNIQUE INDEX IF NOT EXISTS results_idempotency_key_unique
    ON results (idempotency_key) WHERE idempotency_key IS NOT NULL;

-- 3c. Correction audit trail ------------------------------------------------------
CREATE TABLE IF NOT EXISTS result_corrections (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL REFERENCES elections(id),
    polling_unit_code text NOT NULL,
    superseded_result_id integer NOT NULL REFERENCES results(id),
    new_result_id integer NOT NULL REFERENCES results(id),
    reason text NOT NULL,
    dispute_id integer,
    corrected_by text NOT NULL,
    corrected_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_result_corrections_election ON result_corrections(election_id);
CREATE INDEX IF NOT EXISTS idx_result_corrections_pu ON result_corrections(polling_unit_code);

-- 4. Rerun / supplementary / by-elections ----------------------------------------
ALTER TABLE elections ADD COLUMN IF NOT EXISTS parent_election_id integer REFERENCES elections(id);
ALTER TABLE elections ADD COLUMN IF NOT EXISTS election_kind text NOT NULL DEFAULT 'general';
ALTER TABLE elections DROP CONSTRAINT IF EXISTS elections_kind_check;
ALTER TABLE elections ADD CONSTRAINT elections_kind_check CHECK (election_kind = ANY (ARRAY[
    'general','rerun','supplementary','by_election'
]));

CREATE TABLE IF NOT EXISTS rerun_scopes (
    id SERIAL PRIMARY KEY,
    election_id integer NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    scope_type text NOT NULL CHECK (scope_type = ANY (ARRAY['polling_unit','lga','ward'])),
    area_code text NOT NULL,
    reason text,
    created_by text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (election_id, scope_type, area_code)
);
CREATE INDEX IF NOT EXISTS idx_rerun_scopes_election ON rerun_scopes(election_id);

-- 5. Dispute remediation linkage (R5-020) ----------------------------------------
ALTER TABLE disputes ADD COLUMN IF NOT EXISTS result_id integer;
ALTER TABLE disputes ADD COLUMN IF NOT EXISTS outcome text;
ALTER TABLE disputes DROP CONSTRAINT IF EXISTS disputes_outcome_check;
ALTER TABLE disputes ADD CONSTRAINT disputes_outcome_check CHECK (outcome IS NULL OR outcome = ANY (ARRAY[
    'upheld','dismissed','correction_ordered','result_annulled','rerun_ordered'
]));
