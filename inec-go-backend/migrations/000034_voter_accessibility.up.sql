-- R5-074 / H4-06: disability accommodation data model.
-- The platform previously could not even RECORD a PWD voter or an
-- inaccessible polling unit. These columns are the minimal schema support;
-- write-path wiring in ems.go lands via the W2 handoff.

ALTER TABLE voters ADD COLUMN IF NOT EXISTS disability_type TEXT;
ALTER TABLE voters ADD COLUMN IF NOT EXISTS assistance_needed BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN voters.disability_type IS 'Optional self-declared disability category (visual, hearing, mobility, cognitive, other) for PWD priority-voting support.';
COMMENT ON COLUMN voters.assistance_needed IS 'Voter requests assisted/priority voting on election day.';

ALTER TABLE polling_units ADD COLUMN IF NOT EXISTS step_free_access BOOLEAN;
ALTER TABLE polling_units ADD COLUMN IF NOT EXISTS accessibility_notes TEXT;

COMMENT ON COLUMN polling_units.step_free_access IS 'TRUE/FALSE when surveyed; NULL = accessibility not yet assessed.';
COMMENT ON COLUMN polling_units.accessibility_notes IS 'Free-text accessibility notes (ramps, stairs, terrain) for PWD voter guidance.';

CREATE INDEX IF NOT EXISTS idx_voters_assistance ON voters (assistance_needed) WHERE assistance_needed;
