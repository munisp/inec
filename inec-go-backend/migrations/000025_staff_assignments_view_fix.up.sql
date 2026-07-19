-- Migration 000025: Resolve staff_assignments table/view name collision
-- 000003 created a legacy staff_assignments TABLE; schema_compat.go then failed
-- at startup with 'CREATE OR REPLACE VIEW staff_assignments' (relation is not a
-- view), so staffing-readiness checks silently read the empty legacy table.
-- Drop the duplicate and let the canonical view over
-- election_staff_assignments take over. Defaults allow simple-view INSERTs
-- (seed_comprehensive.go) that omit area_type/area_code.

ALTER TABLE election_staff_assignments ALTER COLUMN area_type SET DEFAULT 'state';
ALTER TABLE election_staff_assignments ALTER COLUMN area_code SET DEFAULT '';

DROP TABLE IF EXISTS staff_assignments;

CREATE OR REPLACE VIEW staff_assignments AS
    SELECT * FROM election_staff_assignments;
