CREATE TYPE "public"."volunteer_task_status" AS ENUM('pending', 'in_progress', 'completed', 'cancelled');--> statement-breakpoint
CREATE TYPE "public"."volunteer_task_type" AS ENUM('canvassing', 'polling_unit', 'data_entry', 'logistics', 'security', 'media', 'other');--> statement-breakpoint
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
ALTER TABLE "volunteer_tasks" ADD CONSTRAINT "volunteer_tasks_profile_id_candidate_profiles_id_fk" FOREIGN KEY ("profile_id") REFERENCES "public"."candidate_profiles"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "volunteer_tasks" ADD CONSTRAINT "volunteer_tasks_volunteer_id_volunteers_id_fk" FOREIGN KEY ("volunteer_id") REFERENCES "public"."volunteers"("id") ON DELETE no action ON UPDATE no action;