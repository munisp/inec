CREATE TYPE "public"."agent_status" AS ENUM('active', 'silent', 'sos', 'offline');--> statement-breakpoint
CREATE TYPE "public"."compliance_status" AS ENUM('compliant', 'warning', 'non_compliant', 'pending');--> statement-breakpoint
CREATE TYPE "public"."incident_severity" AS ENUM('low', 'medium', 'high', 'critical');--> statement-breakpoint
CREATE TYPE "public"."incident_status" AS ENUM('open', 'escalated', 'resolved');--> statement-breakpoint
CREATE TYPE "public"."member_role" AS ENUM('owner', 'manager', 'viewer');--> statement-breakpoint
CREATE TYPE "public"."office_type" AS ENUM('President', 'Governor', 'Senator', 'House', 'LGA');--> statement-breakpoint
CREATE TYPE "public"."petition_status" AS ENUM('draft', 'active', 'closed');--> statement-breakpoint
CREATE TYPE "public"."priority_level" AS ENUM('low', 'medium', 'high', 'critical');--> statement-breakpoint
CREATE TYPE "public"."item_status" AS ENUM('active', 'inactive', 'pending', 'completed', 'cancelled');--> statement-breakpoint
CREATE TYPE "public"."user_role" AS ENUM('user', 'admin');--> statement-breakpoint
CREATE TYPE "public"."volunteer_task_status" AS ENUM('pending', 'in_progress', 'completed', 'cancelled');--> statement-breakpoint
CREATE TYPE "public"."volunteer_task_type" AS ENUM('canvassing', 'polling_unit', 'data_entry', 'logistics', 'security', 'media', 'other');--> statement-breakpoint
CREATE TABLE "budget_items" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"category" varchar(100) NOT NULL,
	"description" varchar(300) NOT NULL,
	"budgeted_amount" real NOT NULL,
	"spent_amount" real DEFAULT 0,
	"priority" "priority_level" DEFAULT 'medium',
	"notes" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "campaign_members" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer NOT NULL,
	"user_id" integer,
	"name" varchar(200) NOT NULL,
	"email" varchar(320) NOT NULL,
	"role" "member_role" DEFAULT 'viewer' NOT NULL,
	"invited_at" timestamp DEFAULT now() NOT NULL,
	"accepted_at" timestamp,
	"invite_token" varchar(64)
);
--> statement-breakpoint
CREATE TABLE "campaign_pu_assignments" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer NOT NULL,
	"pu_code" text NOT NULL,
	"agent_name" varchar(200),
	"agent_phone" varchar(20),
	"status" varchar(50),
	"notes" text,
	"created_at" timestamp DEFAULT now() NOT NULL,
	CONSTRAINT "campaign_pu_assignments_profile_pu_unique" UNIQUE("profile_id","pu_code")
);
--> statement-breakpoint
CREATE TABLE "candidate_profiles" (
	"id" serial PRIMARY KEY NOT NULL,
	"user_id" integer,
	"candidate_name" varchar(200) NOT NULL,
	"party_name" varchar(100),
	"party_color" varchar(20) DEFAULT '#006400',
	"state_code" varchar(10),
	"state_name" varchar(100),
	"office" "office_type" DEFAULT 'Governor',
	"religion" varchar(50),
	"gender" varchar(20),
	"geopolitical_zone" varchar(50),
	"is_active" boolean DEFAULT true,
	"is_seeded" boolean DEFAULT false,
	"created_at" timestamp DEFAULT now() NOT NULL,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "compliance_items" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"title" varchar(300) NOT NULL,
	"category" varchar(100),
	"description" text,
	"status" "compliance_status" DEFAULT 'pending',
	"deadline" date,
	"notes" text,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "debate_practice_scores" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"topic" varchar(200) NOT NULL,
	"score" integer NOT NULL,
	"max_score" integer DEFAULT 10,
	"notes" text,
	"scored_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "debate_prep_notes" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"topic" varchar(200) NOT NULL,
	"key_message" text,
	"counter_arguments" jsonb,
	"statistics" jsonb,
	"practice_score" integer,
	"notes" text,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "diaspora_contacts" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"name" varchar(200) NOT NULL,
	"country" varchar(100),
	"city" varchar(100),
	"email" varchar(320),
	"phone" varchar(30),
	"organization" varchar(200),
	"status" "item_status" DEFAULT 'active',
	"pledged_amount" real DEFAULT 0,
	"notes" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "election_results" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"lga" varchar(100) NOT NULL,
	"candidate_name" varchar(200) NOT NULL,
	"party" varchar(100),
	"votes" integer DEFAULT 0,
	"reported_at" timestamp DEFAULT now() NOT NULL,
	"is_projected" boolean DEFAULT false
);
--> statement-breakpoint
CREATE TABLE "endorsements" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"endorser_name" varchar(200) NOT NULL,
	"title" varchar(200),
	"organization" varchar(200),
	"category" varchar(100),
	"statement" text,
	"is_public" boolean DEFAULT true,
	"endorsed_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "field_agents" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"name" varchar(200) NOT NULL,
	"phone" varchar(20),
	"assigned_pu" varchar(300),
	"lga" varchar(100),
	"agent_status" "agent_status" DEFAULT 'offline',
	"voters_counted" integer DEFAULT 0,
	"last_checkin" timestamp,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "fundraising_transactions" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"donor_name" varchar(200),
	"amount" real NOT NULL,
	"currency" varchar(10) DEFAULT 'NGN',
	"source" varchar(100),
	"category" varchar(100),
	"notes" text,
	"transacted_at" timestamp DEFAULT now() NOT NULL,
	"is_verified" boolean DEFAULT false
);
--> statement-breakpoint
CREATE TABLE "manifesto_sections" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"section_title" varchar(200) NOT NULL,
	"summary" text,
	"commitments" jsonb,
	"timeline" varchar(100),
	"budget" varchar(100),
	"priority" "priority_level" DEFAULT 'high',
	"sort_order" integer DEFAULT 0,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "media_items" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"source" varchar(200) NOT NULL,
	"headline" text NOT NULL,
	"sentiment" varchar(20),
	"source_type" varchar(20) DEFAULT 'online',
	"reach" integer DEFAULT 0,
	"zone" varchar(100),
	"url" text,
	"published_at" timestamp,
	"notes" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "opposition_research" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"opponent_name" varchar(200) NOT NULL,
	"party" varchar(100),
	"strength" text,
	"weakness" text,
	"key_issues" jsonb,
	"threat_level" "priority_level" DEFAULT 'medium',
	"notes" text,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "petition_signatures" (
	"id" serial PRIMARY KEY NOT NULL,
	"petition_id" integer,
	"signer_name" varchar(200) NOT NULL,
	"phone" varchar(20),
	"lga" varchar(100),
	"signed_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "petitions" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"title" varchar(400) NOT NULL,
	"description" text,
	"target_signatures" integer DEFAULT 10000,
	"status" "petition_status" DEFAULT 'draft',
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
-- `polling_units` is NOT created here — it's the Go backend's shared,
-- pre-existing national PU registry (inec-go-backend/db.go). This app only
-- reads/references it (see campaign_pu_assignments FK below).
CREATE TABLE "press_releases" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"title" varchar(400) NOT NULL,
	"body" text NOT NULL,
	"template" varchar(100),
	"published_at" timestamp,
	"status" "item_status" DEFAULT 'pending',
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "simulation_runs" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"scenario" varchar(50) DEFAULT 'baseline',
	"state_code" varchar(10),
	"iterations" integer DEFAULT 1000,
	"weather_severity" integer DEFAULT 20,
	"security_threat" integer DEFAULT 15,
	"bvas_reliability" integer DEFAULT 85,
	"staff_training" integer DEFAULT 75,
	"projected_turnout" real,
	"valid_votes_cast" integer,
	"bvas_failure_rate" real,
	"certification_eta" integer,
	"logistics_score" integer,
	"security_index" integer,
	"rejected_ballots" integer,
	"monte_carlo_p50" real,
	"monte_carlo_p5" real,
	"monte_carlo_p95" real,
	"model_confidence" real,
	"disruptions" jsonb,
	"run_at" timestamp DEFAULT now() NOT NULL,
	"ai_narrative" text,
	"label" varchar(120)
);
--> statement-breakpoint
CREATE TABLE "social_media_posts" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"platform" varchar(50) NOT NULL,
	"content" text NOT NULL,
	"scheduled_at" timestamp,
	"published_at" timestamp,
	"status" "item_status" DEFAULT 'pending',
	"impressions" integer DEFAULT 0,
	"engagements" integer DEFAULT 0,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "stakeholder_contacts" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"name" varchar(200) NOT NULL,
	"title" varchar(200),
	"organization" varchar(200),
	"category" varchar(100),
	"phone" varchar(30),
	"email" varchar(320),
	"state" varchar(100),
	"lga" varchar(100),
	"influence_level" "priority_level" DEFAULT 'medium',
	"relationship" varchar(50) DEFAULT 'neutral',
	"last_contact" date,
	"next_action" text,
	"notes" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "timeline_events" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"title" varchar(300) NOT NULL,
	"description" text,
	"event_date" date NOT NULL,
	"category" varchar(100),
	"status" "item_status" DEFAULT 'pending',
	"location" varchar(200),
	"priority" "priority_level" DEFAULT 'medium',
	"created_at" timestamp DEFAULT now() NOT NULL,
	"last_alerted_at" timestamp
);
--> statement-breakpoint
-- `users` is NOT created here — it's the Go backend's shared, pre-existing
-- local-auth table (inec-go-backend/db.go). This app authenticates against
-- it directly (see server/_core/localAuth.ts) rather than owning its own copy.
--> statement-breakpoint
CREATE TABLE "volunteer_tasks" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"volunteer_id" integer,
	"title" varchar(300) NOT NULL,
	"description" text,
	"task_type" "volunteer_task_type" DEFAULT 'other',
	"status" "volunteer_task_status" DEFAULT 'pending',
	"due_date" timestamp,
	"completed_at" timestamp,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "volunteers" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"full_name" varchar(200) NOT NULL,
	"phone" varchar(20),
	"email" varchar(320),
	"lga" varchar(100),
	"ward" varchar(100),
	"role" varchar(100),
	"skills" text,
	"status" "item_status" DEFAULT 'active',
	"joined_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "voter_registrations" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"full_name" varchar(200) NOT NULL,
	"vin" varchar(50),
	"state_code" varchar(10),
	"lga" varchar(100),
	"ward" varchar(100),
	"polling_unit" varchar(200),
	"phone" varchar(20),
	"registered_at" timestamp DEFAULT now() NOT NULL,
	"is_verified" boolean DEFAULT false
);
--> statement-breakpoint
CREATE TABLE "war_room_incidents" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"reported_by" varchar(200),
	"lga" varchar(100),
	"ward" varchar(100),
	"pu_name" varchar(300),
	"incident_type" varchar(100),
	"description" text NOT NULL,
	"severity" "incident_severity" DEFAULT 'medium',
	"status" "incident_status" DEFAULT 'open',
	"reported_at" timestamp DEFAULT now() NOT NULL,
	"resolved_at" timestamp
);
--> statement-breakpoint
ALTER TABLE "budget_items" ADD CONSTRAINT "budget_items_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "campaign_members" ADD CONSTRAINT "campaign_members_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "campaign_members" ADD CONSTRAINT "campaign_members_user_id_users_id_fk" FOREIGN KEY ("user_id") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "campaign_pu_assignments" ADD CONSTRAINT "campaign_pu_assignments_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "campaign_pu_assignments" ADD CONSTRAINT "campaign_pu_assignments_pu_code_polling_units_code_fk" FOREIGN KEY ("pu_code") REFERENCES "public"."polling_units"("code") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "candidate_profiles" ADD CONSTRAINT "candidate_profiles_user_id_users_id_fk" FOREIGN KEY ("user_id") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "compliance_items" ADD CONSTRAINT "compliance_items_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "debate_practice_scores" ADD CONSTRAINT "debate_practice_scores_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "debate_prep_notes" ADD CONSTRAINT "debate_prep_notes_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "diaspora_contacts" ADD CONSTRAINT "diaspora_contacts_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "election_results" ADD CONSTRAINT "election_results_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "endorsements" ADD CONSTRAINT "endorsements_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "field_agents" ADD CONSTRAINT "field_agents_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "fundraising_transactions" ADD CONSTRAINT "fundraising_transactions_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "manifesto_sections" ADD CONSTRAINT "manifesto_sections_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "media_items" ADD CONSTRAINT "media_items_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "opposition_research" ADD CONSTRAINT "opposition_research_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "petition_signatures" ADD CONSTRAINT "petition_signatures_petition_id_petitions_id_fk" FOREIGN KEY ("petition_id") REFERENCES "public"."petitions"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "petitions" ADD CONSTRAINT "petitions_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "press_releases" ADD CONSTRAINT "press_releases_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "simulation_runs" ADD CONSTRAINT "simulation_runs_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "social_media_posts" ADD CONSTRAINT "social_media_posts_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "stakeholder_contacts" ADD CONSTRAINT "stakeholder_contacts_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "timeline_events" ADD CONSTRAINT "timeline_events_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "volunteer_tasks" ADD CONSTRAINT "volunteer_tasks_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "volunteer_tasks" ADD CONSTRAINT "volunteer_tasks_volunteer_id_volunteers_id_fk" FOREIGN KEY ("volunteer_id") REFERENCES "public"."volunteers"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "volunteers" ADD CONSTRAINT "volunteers_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "voter_registrations" ADD CONSTRAINT "voter_registrations_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "war_room_incidents" ADD CONSTRAINT "war_room_incidents_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;