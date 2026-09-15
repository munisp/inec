-- ============================================================================
-- HAND-WRITTEN migration (R5-098, drizzle-kit generate needs a TTY here).
-- War-room incidents gain geo/evidence/occurrence/escalation/attribution
-- fields, plus an append-only audit table for the escalation workflow.
-- Idempotent — safe to re-apply.
-- ============================================================================
ALTER TABLE "war_room_incidents" ADD COLUMN IF NOT EXISTS "latitude" double precision;
ALTER TABLE "war_room_incidents" ADD COLUMN IF NOT EXISTS "longitude" double precision;
ALTER TABLE "war_room_incidents" ADD COLUMN IF NOT EXISTS "evidence_url" text;
ALTER TABLE "war_room_incidents" ADD COLUMN IF NOT EXISTS "occurred_at" timestamp;
ALTER TABLE "war_room_incidents" ADD COLUMN IF NOT EXISTS "assigned_to" varchar(200);
ALTER TABLE "war_room_incidents" ADD COLUMN IF NOT EXISTS "escalated_to" varchar(100);
ALTER TABLE "war_room_incidents" ADD COLUMN IF NOT EXISTS "escalated_at" timestamp;
ALTER TABLE "war_room_incidents" ADD COLUMN IF NOT EXISTS "escalation_note" text;
ALTER TABLE "war_room_incidents" ADD COLUMN IF NOT EXISTS "opposition_entry_id" integer REFERENCES "opposition_research"("id");

CREATE TABLE IF NOT EXISTS "war_room_incident_audit" (
	"id" serial PRIMARY KEY NOT NULL,
	"incident_id" integer NOT NULL,
	"action" varchar(30) NOT NULL,
	"actor" varchar(200),
	"from_status" varchar(20),
	"to_status" varchar(20),
	"detail" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
DO $$ BEGIN
	ALTER TABLE "war_room_incident_audit" ADD CONSTRAINT "war_room_incident_audit_incident_id_fk"
		FOREIGN KEY ("incident_id") REFERENCES "war_room_incidents"("id");
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
CREATE INDEX IF NOT EXISTS "war_room_incident_audit_incident_idx" ON "war_room_incident_audit" ("incident_id");
