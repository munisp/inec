DROP TABLE IF EXISTS result_agent_signatures;
ALTER TABLE voters DROP COLUMN IF EXISTS voted_at;
ALTER TABLE voters DROP COLUMN IF EXISTS has_voted;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role = ANY (ARRAY[
    'admin','presiding_officer','collation_officer','observer','public'
]));
