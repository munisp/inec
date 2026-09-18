-- ============================================================================
-- HAND-WRITTEN migration (W13 / CA-parity analytics, drizzle-kit generate
-- needs a TTY). Consent-first psychometrics and message testing:
--   * survey_panelists   — consented survey panel (consent_id links to the
--                          W12 consent_records substrate; fail-closed)
--   * survey_responses   — psychometric instrument item responses (Likert 1-5)
--   * message_tests      — A/B message experiments
--   * message_variants   — variants per test
--   * message_events     — impressions/responses/conversions per variant
-- Idempotent. Capability analogue of CA's psychographic + micro-targeting
-- stack, lawfully built on consented first-party data only.
-- ============================================================================

-- Consented survey panel. Every panelist MUST reference an active consent
-- record (W12) — no consent, no panelist. Psychographic data is sensitive
-- (political opinion adjacency, NDPA 2023) and collected only by opt-in.
CREATE TABLE IF NOT EXISTS "survey_panelists" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer NOT NULL,
	"consent_id" integer NOT NULL,
	"full_name" varchar(200) NOT NULL,
	"state_code" varchar(10),
	"lga" varchar(100),
	"ward" varchar(100),
	"age_band" varchar(10),
	"gender" varchar(20),
	"status" varchar(20) NOT NULL DEFAULT 'active',
	"created_at" timestamp DEFAULT now() NOT NULL
);
CREATE INDEX IF NOT EXISTS "survey_panelists_profile_idx"
	ON "survey_panelists" ("profile_id", "status");

-- Psychometric item responses. instrument + item_key identify the question
-- (e.g. instrument 'OCEAN20', item_key 'E1'); score is Likert 1-5.
-- Responses are the ONLY lawful basis for trait scoring — the platform never
-- infers personality for non-respondents.
CREATE TABLE IF NOT EXISTS "survey_responses" (
	"id" serial PRIMARY KEY NOT NULL,
	"panelist_id" integer NOT NULL,
	"instrument" varchar(40) NOT NULL,
	"item_key" varchar(20) NOT NULL,
	"score" integer NOT NULL,
	"responded_at" timestamp DEFAULT now() NOT NULL
);
CREATE INDEX IF NOT EXISTS "survey_responses_panelist_idx"
	ON "survey_responses" ("panelist_id", "instrument");

-- A/B message testing (the lawful analogue of CA's per-personality ad
-- variants): tests run against the campaign's own consented audiences.
CREATE TABLE IF NOT EXISTS "message_tests" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer NOT NULL,
	"name" varchar(200) NOT NULL,
	"channel" varchar(40),
	"status" varchar(20) NOT NULL DEFAULT 'draft',
	"created_at" timestamp DEFAULT now() NOT NULL
);

CREATE TABLE IF NOT EXISTS "message_variants" (
	"id" serial PRIMARY KEY NOT NULL,
	"test_id" integer NOT NULL,
	"label" varchar(40) NOT NULL,
	"body" text NOT NULL,
	"created_at" timestamp DEFAULT now() NOT NULL
);

-- Real event counts per variant. Analysis is computed from these rows only.
CREATE TABLE IF NOT EXISTS "message_events" (
	"id" serial PRIMARY KEY NOT NULL,
	"variant_id" integer NOT NULL,
	"event_type" varchar(20) NOT NULL, -- impression | response | conversion
	"occurred_at" timestamp DEFAULT now() NOT NULL
);
CREATE INDEX IF NOT EXISTS "message_events_variant_idx"
	ON "message_events" ("variant_id", "event_type");
