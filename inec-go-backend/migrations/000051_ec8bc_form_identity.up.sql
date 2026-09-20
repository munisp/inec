-- GAP-2 (2026-09 deep audit): model the INEC EC8B (ward) / EC8C (LGA)
-- collation forms on collation_results — previously the forms existed only
-- as names in the material-type enum while the numeric rollups had no form
-- identity (serial, scanned-image hash, signatory, submission time).
ALTER TABLE collation_results ADD COLUMN IF NOT EXISTS form_type TEXT;
ALTER TABLE collation_results ADD COLUMN IF NOT EXISTS form_serial TEXT;
ALTER TABLE collation_results ADD COLUMN IF NOT EXISTS form_image_hash TEXT;
ALTER TABLE collation_results ADD COLUMN IF NOT EXISTS signed_by TEXT;
ALTER TABLE collation_results ADD COLUMN IF NOT EXISTS form_submitted_at TIMESTAMP;

-- EC8B is the ward-level form, EC8C the LGA-level form; state/national
-- rollups carry no EC8B/C form (NULL). Backfill the identity from level.
UPDATE collation_results SET form_type = 'EC8B' WHERE level = 'ward' AND form_type IS NULL;
UPDATE collation_results SET form_type = 'EC8C' WHERE level = 'lga' AND form_type IS NULL;

ALTER TABLE collation_results DROP CONSTRAINT IF EXISTS collation_results_form_check;
ALTER TABLE collation_results ADD CONSTRAINT collation_results_form_check
    CHECK (
        (level = 'ward' AND (form_type = 'EC8B' OR form_type IS NULL)) OR
        (level = 'lga' AND (form_type = 'EC8C' OR form_type IS NULL)) OR
        (level IN ('state', 'national') AND form_type IS NULL)
    );
