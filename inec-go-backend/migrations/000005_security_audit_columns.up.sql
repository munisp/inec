
-- Add missing audit columns
ALTER TABLE parties ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE states ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE lgas ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE wards ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE polling_units ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

-- Add updated_at columns
ALTER TABLE results ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE bvas_devices ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

-- Enable Row Level Security (only on tables that exist — several of these
-- are created by later migrations or runtime schema initialisation)
DO $$
BEGIN
    IF to_regclass('audit_trail') IS NOT NULL THEN
        ALTER TABLE audit_trail ENABLE ROW LEVEL SECURITY;
    END IF;
    IF to_regclass('staff_assignments') IS NOT NULL THEN
        ALTER TABLE staff_assignments ENABLE ROW LEVEL SECURITY;
    END IF;
    IF to_regclass('keycloak_sessions') IS NOT NULL THEN
        ALTER TABLE keycloak_sessions ENABLE ROW LEVEL SECURITY;
    END IF;
END $$;

-- Encrypt sensitive columns (pseudocode for Pgcrypto integration)
-- Guarded: pgcrypto contrib may be unavailable on minimal Postgres builds;
-- a missing extension must not abort the whole migration.
DO $$ BEGIN CREATE EXTENSION IF NOT EXISTS pgcrypto;
EXCEPTION WHEN OTHERS THEN RAISE WARNING 'pgcrypto extension unavailable (%) — continuing without it', SQLERRM; END $$;
-- Assuming columns are migrated to bytea for encrypted storage in production
