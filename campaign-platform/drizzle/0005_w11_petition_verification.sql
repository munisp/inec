-- ============================================================================
-- HAND-WRITTEN migration (R5-102, drizzle-kit generate needs a TTY here).
-- Petition signature verification tiers + signer-identity dedupe. Idempotent.
-- ============================================================================
-- NB: uses the built-in sha256() SQL function (PG 11+ core) — no pgcrypto
-- extension dependency.

ALTER TABLE "petition_signatures" ADD COLUMN IF NOT EXISTS "signer_hash" varchar(64);
ALTER TABLE "petition_signatures" ADD COLUMN IF NOT EXISTS "verification_status" varchar(20) DEFAULT 'unverified' NOT NULL;
ALTER TABLE "petition_signatures" ADD COLUMN IF NOT EXISTS "verified_at" timestamp;
ALTER TABLE "petition_signatures" ADD COLUMN IF NOT EXISTS "verified_by" varchar(200);

-- Backfill identity hashes for pre-existing signatures (same canonical form
-- as the application: lower(trim(phone)) | lower(trim(name)) | lower(trim(lga))).
UPDATE "petition_signatures"
SET "signer_hash" = encode(sha256(convert_to(
	regexp_replace(COALESCE("phone", ''), '[^0-9]', '', 'g')
	|| '|' || lower(regexp_replace(trim("signer_name"), '\s+', ' ', 'g'))
	|| '|' || lower(trim(COALESCE("lga", ''))),
	'UTF8')), 'hex')
WHERE "signer_hash" IS NULL;

-- Honestly flag pre-existing duplicate identities (keep the first signing,
-- mark the rest rejected_duplicate with a disambiguated hash so the unique
-- index below can be created).
WITH ranked AS (
	SELECT id, ROW_NUMBER() OVER (PARTITION BY petition_id, signer_hash ORDER BY signed_at, id) AS rn
	FROM petition_signatures
	WHERE signer_hash IS NOT NULL
)
UPDATE petition_signatures ps
SET verification_status = 'rejected_duplicate',
	signer_hash = ps.signer_hash || '#dup:' || ps.id
FROM ranked
WHERE ps.id = ranked.id AND ranked.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS "petition_signatures_signer_hash_uniq"
	ON "petition_signatures" ("petition_id", "signer_hash")
	WHERE "signer_hash" IS NOT NULL;
CREATE INDEX IF NOT EXISTS "petition_signatures_status_idx"
	ON "petition_signatures" ("petition_id", "verification_status");
