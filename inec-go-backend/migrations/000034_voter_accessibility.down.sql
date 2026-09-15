DROP INDEX IF EXISTS idx_voters_assistance;

ALTER TABLE polling_units DROP COLUMN IF EXISTS accessibility_notes;
ALTER TABLE polling_units DROP COLUMN IF EXISTS step_free_access;

ALTER TABLE voters DROP COLUMN IF EXISTS assistance_needed;
ALTER TABLE voters DROP COLUMN IF EXISTS disability_type;
