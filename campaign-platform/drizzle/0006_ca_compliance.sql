-- ============================================================================
-- HAND-WRITTEN migration (W12 / CA-compliance, drizzle-kit generate needs a TTY).
-- Cambridge Analytica lessons → NDPA 2023 compliance substrate:
--   * consent_records        — per-subject, per-purpose consent registry
--   * data_provenance_ledger — append-only origin record for personal data
--   * data_access_audit      — append-only log of PII list access
--   * data_subject_requests  — DSAR workflow (access/rectification/erasure/objection)
-- Idempotent. Research basis: /mnt/agents/output/research/ca_*.md
-- (FTC v. Cambridge Analytica deletion/destruction order; FTC 20-year Facebook
-- order accountability machinery; NDPA 2023 ss.25/36/37).
-- ============================================================================

DO $$ BEGIN
	CREATE TYPE "lawful_basis" AS ENUM ('consent', 'contract', 'legal_obligation',
		'vital_interest', 'public_interest', 'legitimate_interest');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
	CREATE TYPE "subject_table" AS ENUM ('voter_registrations', 'diaspora_contacts',
		'stakeholder_contacts', 'volunteers', 'petition_signatures');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS "consent_records" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer NOT NULL,
	-- Which personal-data table + row this consent covers.
	"subject_table" "subject_table" NOT NULL,
	"subject_id" integer NOT NULL,
	-- NDPA 2023 s.25 lawful bases relevant to campaign processing.
	"lawful_basis" "lawful_basis" NOT NULL,
	-- What the data may be used for (purpose binding — no repurposing).
	"purpose" varchar(120) NOT NULL,
	-- How consent was captured when lawful_basis = 'consent'.
	"consent_method" varchar(20),
	"consent_granted" boolean NOT NULL DEFAULT false,
	"consented_at" timestamp,
	-- Withdrawal is a state transition; history is never deleted.
	"withdrawn_at" timestamp,
	-- Retention limit (NDPA: store no longer than necessary).
	"retention_until" date,
	"notes" text,
	"created_at" timestamp DEFAULT now() NOT NULL,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
CREATE INDEX IF NOT EXISTS "consent_records_subject_idx"
	ON "consent_records" ("subject_table", "subject_id");
CREATE INDEX IF NOT EXISTS "consent_records_profile_idx"
	ON "consent_records" ("profile_id");

-- Append-only: where every personal-data record came from. No UPDATE/DELETE
-- paths exist in application code — provenance opacity is what made the CA
-- breach unauditable and deletion unverifiable.
CREATE TABLE IF NOT EXISTS "data_provenance_ledger" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer NOT NULL,
	"subject_table" "subject_table" NOT NULL,
	"subject_id" integer NOT NULL,
	-- Declared collection channel (door_to_door, event_signup, ...).
	"source" varchar(40) NOT NULL,
	"collected_by" varchar(200),
	"collected_at" timestamp DEFAULT now() NOT NULL,
	"lawful_basis" "lawful_basis" NOT NULL,
	"notes" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
CREATE INDEX IF NOT EXISTS "provenance_subject_idx"
	ON "data_provenance_ledger" ("subject_table", "subject_id");
CREATE INDEX IF NOT EXISTS "provenance_profile_idx"
	ON "data_provenance_ledger" ("profile_id");

-- Append-only: who read PII lists, when, how many rows, declared purpose.
-- The FTC order's accountability machinery (certifications, incident
-- documentation) presumes exactly this evidence trail.
CREATE TABLE IF NOT EXISTS "data_access_audit" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer NOT NULL,
	"actor_id" integer,
	"actor_name" varchar(200),
	"subject_table" "subject_table" NOT NULL,
	"action" varchar(30) NOT NULL,
	"row_count" integer,
	"purpose" varchar(200),
	"created_at" timestamp DEFAULT now() NOT NULL
);
CREATE INDEX IF NOT EXISTS "access_audit_profile_idx"
	ON "data_access_audit" ("profile_id");

-- Data Subject Access Request workflow (NDPA 2023 rights: access,
-- rectification, erasure, restriction, portability, objection).
CREATE TABLE IF NOT EXISTS "data_subject_requests" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer NOT NULL,
	"request_type" varchar(20) NOT NULL,
	"subject_name" varchar(200) NOT NULL,
	"subject_contact" varchar(320),
	"subject_table" "subject_table",
	"subject_id" integer,
	"status" varchar(20) NOT NULL DEFAULT 'open',
	"received_at" timestamp DEFAULT now() NOT NULL,
	-- NDPA: respond without undue delay; 30-day default tracked here.
	"due_at" date NOT NULL,
	"fulfilled_at" timestamp,
	"rejection_reason" text,
	"notes" text,
	"created_at" timestamp DEFAULT now() NOT NULL,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
CREATE INDEX IF NOT EXISTS "dsar_profile_idx"
	ON "data_subject_requests" ("profile_id", "status");
