-- W2 MEDIUMs.
-- (a) returning_officer / ict_officer are referenced throughout the authz
--     model but could never exist as user roles — declare/returning flows
--     were unusable. Extend the role vocabulary.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role = ANY (ARRAY[
    'admin','presiding_officer','collation_officer','returning_officer',
    'ict_officer','observer','public'
]));

-- (b) R5-028: election-day voted marker on the voter register.
ALTER TABLE voters ADD COLUMN IF NOT EXISTS has_voted integer NOT NULL DEFAULT 0;
ALTER TABLE voters ADD COLUMN IF NOT EXISTS voted_at timestamptz;

-- (c) R5-032: party-agent countersigning of EC8A results (signed or
--     formally refused — both are captured).
CREATE TABLE IF NOT EXISTS result_agent_signatures (
    id SERIAL PRIMARY KEY,
    result_id integer NOT NULL REFERENCES results(id) ON DELETE CASCADE,
    party_code text NOT NULL,
    agent_name text NOT NULL,
    decision text NOT NULL CHECK (decision = ANY (ARRAY['signed','refused'])),
    reason text,
    signed_by text,
    signed_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (result_id, party_code)
);
