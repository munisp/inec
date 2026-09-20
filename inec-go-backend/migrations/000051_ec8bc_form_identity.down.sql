-- Reverse 000051.
ALTER TABLE collation_results DROP CONSTRAINT IF EXISTS collation_results_form_check;
ALTER TABLE collation_results DROP COLUMN IF EXISTS form_type;
ALTER TABLE collation_results DROP COLUMN IF EXISTS form_serial;
ALTER TABLE collation_results DROP COLUMN IF EXISTS form_image_hash;
ALTER TABLE collation_results DROP COLUMN IF EXISTS signed_by;
ALTER TABLE collation_results DROP COLUMN IF EXISTS form_submitted_at;
