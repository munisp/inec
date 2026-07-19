CREATE TYPE "public"."agent_status" AS ENUM('active', 'silent', 'sos', 'offline');--> statement-breakpoint
CREATE TYPE "public"."compliance_status" AS ENUM('compliant', 'warning', 'non_compliant', 'pending');--> statement-breakpoint
CREATE TYPE "public"."incident_severity" AS ENUM('low', 'medium', 'high', 'critical');--> statement-breakpoint
CREATE TYPE "public"."incident_status" AS ENUM('open', 'escalated', 'resolved');--> statement-breakpoint
CREATE TYPE "public"."office_type" AS ENUM('President', 'Governor', 'Senator', 'House', 'LGA');--> statement-breakpoint
CREATE TYPE "public"."petition_status" AS ENUM('draft', 'active', 'closed');--> statement-breakpoint
CREATE TYPE "public"."priority_level" AS ENUM('low', 'medium', 'high', 'critical');--> statement-breakpoint
CREATE TYPE "public"."item_status" AS ENUM('active', 'inactive', 'pending', 'completed', 'cancelled');--> statement-breakpoint
CREATE TYPE "public"."user_role" AS ENUM('user', 'admin');--> statement-breakpoint
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
CREATE TABLE "polling_units" (
	"id" serial PRIMARY KEY NOT NULL,
	"profile_id" integer,
	"pu_code" varchar(50),
	"name" varchar(300) NOT NULL,
	"ward" varchar(100),
	"lga" varchar(100),
	"state_code" varchar(10),
	"lat" real,
	"lng" real,
	"registered_voters" integer DEFAULT 0,
	"agent_assigned" varchar(200),
	"agent_phone" varchar(20),
	"notes" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
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
	"run_at" timestamp DEFAULT now() NOT NULL
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
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE "users" (
	"id" serial PRIMARY KEY NOT NULL,
	"open_id" varchar(64) NOT NULL,
	"name" text,
	"email" varchar(320),
	"login_method" varchar(64),
	"role" "user_role" DEFAULT 'user' NOT NULL,
	"created_at" timestamp DEFAULT now() NOT NULL,
	"updated_at" timestamp DEFAULT now() NOT NULL,
	"last_signed_in" timestamp DEFAULT now() NOT NULL,
	CONSTRAINT "users_open_id_unique" UNIQUE("open_id")
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
ALTER TABLE "candidate_profiles" ADD CONSTRAINT "candidate_profiles_user_id_users_id_fk" FOREIGN KEY ("user_id") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "compliance_items" ADD CONSTRAINT "compliance_items_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
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
ALTER TABLE "polling_units" ADD CONSTRAINT "polling_units_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "press_releases" ADD CONSTRAINT "press_releases_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "simulation_runs" ADD CONSTRAINT "simulation_runs_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "social_media_posts" ADD CONSTRAINT "social_media_posts_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "timeline_events" ADD CONSTRAINT "timeline_events_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "volunteers" ADD CONSTRAINT "volunteers_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "voter_registrations" ADD CONSTRAINT "voter_registrations_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "war_room_incidents" ADD CONSTRAINT "war_room_incidents_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;