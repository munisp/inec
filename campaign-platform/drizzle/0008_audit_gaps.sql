-- W14: audit-driven gap closure (deep audit 2026-09).
-- Idempotent: DO blocks / IF NOT EXISTS / ADD COLUMN IF NOT EXISTS throughout.
--
-- 1. Onboarding audit coverage: data_access_audit.subject_table must be able
--    to reference campaign_members (invite/accept/role-change/removal logs).
-- 2. Election tribunal tracking (post-election legal petitions) — GAP-3.
-- 3. NBC media/advert compliance gate fields — GAP-6.
-- 4. PWD accessibility flags (campaign-side) — GAP-7.
-- 5. Donor screening fields for EA 2022 funding-legality checks — GAP-5.

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_enum e
    JOIN pg_type t ON t.oid = e.enumtypid
    WHERE t.typname = 'subject_table' AND e.enumlabel = 'campaign_members'
  ) THEN
    ALTER TYPE "subject_table" ADD VALUE 'campaign_members';
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS "election_petitions" (
  "id" serial PRIMARY KEY NOT NULL,
  "profile_id" integer REFERENCES "candidate_profiles"("id"),
  "election_name" varchar(200) NOT NULL,
  "petition_type" varchar(20) NOT NULL DEFAULT 'post_election', -- pre_election|post_election
  "court" varchar(200),              -- tribunal / court seised
  "case_number" varchar(100),
  "petitioner" varchar(200),
  "respondent" varchar(200),
  "counsel" varchar(200),
  "filed_at" timestamp,
  "hearing_date" timestamp,
  "status" varchar(30) NOT NULL DEFAULT 'filed',  -- filed|hearing|judgment|appealed|closed
  "outcome" text,
  "notes" text,
  "created_at" timestamp DEFAULT now() NOT NULL,
  "updated_at" timestamp DEFAULT now() NOT NULL
);
CREATE INDEX IF NOT EXISTS "election_petitions_profile_idx" ON "election_petitions" ("profile_id");

ALTER TABLE "media_items"
  ADD COLUMN IF NOT EXISTS "compliance_status" varchar(20) DEFAULT 'unreviewed', -- unreviewed|compliant|breach|cleared
  ADD COLUMN IF NOT EXISTS "compliance_notes" text;

ALTER TABLE "campaign_pu_assignments"
  ADD COLUMN IF NOT EXISTS "pwd_accessible" boolean DEFAULT false;

ALTER TABLE "voter_registrations"
  ADD COLUMN IF NOT EXISTS "accessibility_needs" varchar(120);

ALTER TABLE "fundraising_transactions"
  ADD COLUMN IF NOT EXISTS "donor_type" varchar(20) DEFAULT 'individual_local', -- individual_local|corporate_local|diaspora|anonymous
  ADD COLUMN IF NOT EXISTS "source_attested" boolean DEFAULT false;
