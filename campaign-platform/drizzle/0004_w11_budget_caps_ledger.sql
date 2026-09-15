-- ============================================================================
-- HAND-WRITTEN migration (R5-101, drizzle-kit generate needs a TTY here).
-- Statutory per-office campaign-spend caps (Electoral Act 2022 §88) and an
-- append-only ledger for every budget mutation. Idempotent.
-- ============================================================================
CREATE TABLE IF NOT EXISTS "budget_statutory_caps" (
	"office" "office_type" PRIMARY KEY NOT NULL,
	"cap_amount" numeric(15, 2) NOT NULL,
	"notes" text,
	"updated_at" timestamp DEFAULT now() NOT NULL
);

-- Electoral Act 2022 §88(2)-(7) campaign expense limits.
INSERT INTO "budget_statutory_caps" ("office", "cap_amount", "notes") VALUES
	('President', 5000000000.00, 'Electoral Act 2022 s.88(2) — presidential'),
	('Governor', 1000000000.00, 'Electoral Act 2022 s.88(3) — governorship'),
	('Senator', 100000000.00, 'Electoral Act 2022 s.88(4) — senatorial'),
	('House', 70000000.00, 'Electoral Act 2022 s.88(5) — House of Representatives'),
	('LGA', 30000000.00, 'Electoral Act 2022 s.88(6) — state assembly / area council')
ON CONFLICT ("office") DO NOTHING;

CREATE TABLE IF NOT EXISTS "budget_spend_ledger" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer NOT NULL,
	"budget_item_id" integer,
	"change_type" varchar(20) NOT NULL,
	"previous_budgeted" numeric(15, 2),
	"new_budgeted" numeric(15, 2),
	"previous_spent" numeric(15, 2),
	"new_spent" numeric(15, 2),
	"changed_by" varchar(200),
	"note" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
DO $$ BEGIN
	ALTER TABLE "budget_spend_ledger" ADD CONSTRAINT "budget_spend_ledger_profile_id_fk"
		FOREIGN KEY ("profile_id") REFERENCES "candidate_profiles"("id");
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
CREATE INDEX IF NOT EXISTS "budget_spend_ledger_profile_idx" ON "budget_spend_ledger" ("profile_id");
CREATE INDEX IF NOT EXISTS "budget_spend_ledger_item_idx" ON "budget_spend_ledger" ("budget_item_id");

-- Append-only enforcement: UPDATE/DELETE on the ledger raises.
CREATE OR REPLACE FUNCTION budget_ledger_immutable() RETURNS trigger AS $$
BEGIN
	RAISE EXCEPTION 'budget_spend_ledger is append-only';
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS budget_ledger_immutable_trg ON "budget_spend_ledger";
CREATE TRIGGER budget_ledger_immutable_trg
	BEFORE UPDATE OR DELETE ON "budget_spend_ledger"
	FOR EACH ROW EXECUTE FUNCTION budget_ledger_immutable();
