import {
  pgTable, serial, text, varchar, integer, boolean,
  timestamp, pgEnum, jsonb, real, date, unique, numeric, uniqueIndex, index,
  primaryKey, doublePrecision
} from "drizzle-orm/pg-core";
import { sql } from "drizzle-orm";

// ─── Enums ────────────────────────────────────────────────────────────────────
export const userRoleEnum = pgEnum("user_role", ["user", "admin"]);
export const officeEnum = pgEnum("office_type", ["President", "Governor", "Senator", "House", "LGA"]);
export const priorityEnum = pgEnum("priority_level", ["low", "medium", "high", "critical"]);
export const statusEnum = pgEnum("item_status", ["active", "inactive", "pending", "completed", "cancelled"]);
export const incidentStatusEnum = pgEnum("incident_status", ["open", "escalated", "resolved"]);
export const incidentSeverityEnum = pgEnum("incident_severity", ["low", "medium", "high", "critical"]);
export const agentStatusEnum = pgEnum("agent_status", ["active", "silent", "sos", "offline"]);
export const complianceStatusEnum = pgEnum("compliance_status", ["compliant", "warning", "non_compliant", "pending"]);
export const petitionStatusEnum = pgEnum("petition_status", ["draft", "active", "closed"]);
export const memberRoleEnum = pgEnum("member_role", ["owner", "manager", "viewer"]);

// ─── Users ──────────────────────────────────────────────────────────────────
// This table is owned by the Go backend (inec-go-backend/db.go) and shared —
// campaign-platform must match its exact columns, not define its own shape.
// Valid `role` values (Postgres CHECK, not a native enum):
// 'admin' | 'presiding_officer' | 'collation_officer' | 'observer' | 'public'
export const users = pgTable("users", {
  id: serial("id").primaryKey(),
  username: text("username").notNull().unique(),
  passwordHash: text("password_hash").notNull(),
  fullName: text("full_name").notNull(),
  role: text("role").notNull(),
  staffId: text("staff_id").unique(),
  stateCode: text("state_code"),
  lgaCode: text("lga_code"),
  pollingUnitCode: text("polling_unit_code"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
  isActive: integer("is_active").default(1),
  partyId: integer("party_id"),
  kycStatus: text("kyc_status").default("not_started"),
});
export type User = typeof users.$inferSelect;
export type InsertUser = typeof users.$inferInsert;

// ─── Candidate Profiles ───────────────────────────────────────────────────────
export const candidateProfiles = pgTable("candidate_profiles", {
  id: serial("id").primaryKey(),
  userId: integer("user_id").references(() => users.id),
  candidateName: varchar("candidate_name", { length: 200 }).notNull(),
  partyName: varchar("party_name", { length: 100 }),
  partyColor: varchar("party_color", { length: 20 }).default("#006400"),
  stateCode: varchar("state_code", { length: 10 }),
  stateName: varchar("state_name", { length: 100 }),
  office: officeEnum("office").default("Governor"),
  religion: varchar("religion", { length: 50 }),
  gender: varchar("gender", { length: 20 }),
  geopoliticalZone: varchar("geopolitical_zone", { length: 50 }),
  isActive: boolean("is_active").default(true),
  isSeeded: boolean("is_seeded").default(false),
  createdAt: timestamp("created_at").defaultNow().notNull(),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
}, (table) => [
  // One profile per user; partial so Go-managed rows with NULL user_id are unaffected.
  // Closes the getOrCreateUserProfile check-then-insert race.
  uniqueIndex("candidate_profiles_user_id_unique")
    .on(table.userId)
    .where(sql`${table.userId} IS NOT NULL`),
]);
export type CandidateProfile = typeof candidateProfiles.$inferSelect;
export type InsertCandidateProfile = typeof candidateProfiles.$inferInsert;

// ─── Campaign Timeline Events ─────────────────────────────────────────────────
export const timelineEvents = pgTable("timeline_events", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  title: varchar("title", { length: 300 }).notNull(),
  description: text("description"),
  eventDate: date("event_date").notNull(),
  category: varchar("category", { length: 100 }),
  status: statusEnum("status").default("pending"),
  location: varchar("location", { length: 200 }),
  priority: priorityEnum("priority").default("medium"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
  lastAlertedAt: timestamp("last_alerted_at"),
});
export type TimelineEvent = typeof timelineEvents.$inferSelect;
export type InsertTimelineEvent = typeof timelineEvents.$inferInsert;

// ─── Voter Registration Records ───────────────────────────────────────────────
export const voterRegistrations = pgTable("voter_registrations", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  fullName: varchar("full_name", { length: 200 }).notNull(),
  vin: varchar("vin", { length: 50 }),
  stateCode: varchar("state_code", { length: 10 }),
  lga: varchar("lga", { length: 100 }),
  ward: varchar("ward", { length: 100 }),
  pollingUnit: varchar("polling_unit", { length: 200 }),
  phone: varchar("phone", { length: 20 }),
  // Optional PWD accessibility needs recorded WITH the voter's consent
  // (W14, audit GAP-7) — drives accessible-PU assignment planning.
  accessibilityNeeds: varchar("accessibility_needs", { length: 120 }),
  registeredAt: timestamp("registered_at").defaultNow().notNull(),
  isVerified: boolean("is_verified").default(false),
});
export type VoterRegistration = typeof voterRegistrations.$inferSelect;

// ─── Polling Units ────────────────────────────────────────────────────────────
// Canonical, nationwide PU registry — owned by the Go backend (inec-go-backend/db.go)
// and shared. Must match its exact columns; do not add campaign-specific fields here.
export const pollingUnits = pgTable("polling_units", {
  id: serial("id").primaryKey(),
  code: text("code").notNull().unique(),
  name: text("name").notNull(),
  wardCode: text("ward_code").notNull(),
  registeredVoters: integer("registered_voters").default(0),
  latitude: real("latitude"),
  longitude: real("longitude"),
});
export type PollingUnit = typeof pollingUnits.$inferSelect;

// ─── Campaign PU Assignments ──────────────────────────────────────────────────
// Per-candidate operational tracking (agent, status, notes) for a canonical PU.
// Kept separate from `pollingUnits` so campaign-platform never writes campaign-
// specific fields onto the shared, Go-owned national PU registry.
export const campaignPuAssignments = pgTable("campaign_pu_assignments", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").notNull().references(() => candidateProfiles.id),
  puCode: text("pu_code").notNull().references(() => pollingUnits.code),
  agentName: varchar("agent_name", { length: 200 }),
  agentPhone: varchar("agent_phone", { length: 20 }),
  status: varchar("status", { length: 50 }),
  notes: text("notes"),
  // PWD-accessible designation — campaign-side planning flag (W14, audit
  // GAP-7); the shared Go-owned PU registry is intentionally not modified.
  pwdAccessible: boolean("pwd_accessible").default(false),
  createdAt: timestamp("created_at").defaultNow().notNull(),
}, (table) => [
  unique("campaign_pu_assignments_profile_pu_unique").on(table.profileId, table.puCode),
]);
export type CampaignPuAssignment = typeof campaignPuAssignments.$inferSelect;

// ─── Volunteers ───────────────────────────────────────────────────────────────
export const volunteers = pgTable("volunteers", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  fullName: varchar("full_name", { length: 200 }).notNull(),
  phone: varchar("phone", { length: 20 }),
  email: varchar("email", { length: 320 }),
  lga: varchar("lga", { length: 100 }),
  ward: varchar("ward", { length: 100 }),
  role: varchar("role", { length: 100 }),
  skills: text("skills"),
  status: statusEnum("status").default("active"),
  joinedAt: timestamp("joined_at").defaultNow().notNull(),
});
export type Volunteer = typeof volunteers.$inferSelect;

// ─── Press Releases ───────────────────────────────────────────────────────────
export const pressReleases = pgTable("press_releases", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  title: varchar("title", { length: 400 }).notNull(),
  body: text("body").notNull(),
  template: varchar("template", { length: 100 }),
  publishedAt: timestamp("published_at"),
  status: statusEnum("status").default("pending"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type PressRelease = typeof pressReleases.$inferSelect;

// ─── Social Media Posts ───────────────────────────────────────────────────────
export const socialMediaPosts = pgTable("social_media_posts", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  platform: varchar("platform", { length: 50 }).notNull(),
  content: text("content").notNull(),
  // Space-joined hashtag string submitted with the post (router input is a
  // string array, stored joined). Nullable — hashtags are optional.
  hashtags: text("hashtags"),
  scheduledAt: timestamp("scheduled_at"),
  publishedAt: timestamp("published_at"),
  status: statusEnum("status").default("pending"),
  impressions: integer("impressions").default(0),
  engagements: integer("engagements").default(0),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type SocialMediaPost = typeof socialMediaPosts.$inferSelect;

// ─── Legal Compliance Items ───────────────────────────────────────────────────
export const complianceItems = pgTable("compliance_items", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  title: varchar("title", { length: 300 }).notNull(),
  category: varchar("category", { length: 100 }),
  description: text("description"),
  status: complianceStatusEnum("status").default("pending"),
  deadline: date("deadline"),
  notes: text("notes"),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
});
export type ComplianceItem = typeof complianceItems.$inferSelect;

// ─── Opposition Research ──────────────────────────────────────────────────────
export const oppositionResearch = pgTable("opposition_research", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  opponentName: varchar("opponent_name", { length: 200 }).notNull(),
  party: varchar("party", { length: 100 }),
  strength: text("strength"),
  weakness: text("weakness"),
  keyIssues: jsonb("key_issues"),
  threatLevel: priorityEnum("threat_level").default("medium"),
  notes: text("notes"),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
});
export type OppositionResearchEntry = typeof oppositionResearch.$inferSelect;

// ─── War Room Incidents ───────────────────────────────────────────────────────
export const warRoomIncidents = pgTable("war_room_incidents", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  reportedBy: varchar("reported_by", { length: 200 }),
  lga: varchar("lga", { length: 100 }),
  ward: varchar("ward", { length: 100 }),
  puName: varchar("pu_name", { length: 300 }),
  incidentType: varchar("incident_type", { length: 100 }),
  description: text("description").notNull(),
  severity: incidentSeverityEnum("severity").default("medium"),
  status: incidentStatusEnum("status").default("open"),
  // R5-098: geo, evidence, time-of-occurrence, escalation and rival-party
  // attribution fields — previously the UI only captured free text + severity.
  latitude: doublePrecision("latitude"),
  longitude: doublePrecision("longitude"),
  evidenceUrl: text("evidence_url"),
  occurredAt: timestamp("occurred_at"),
  assignedTo: varchar("assigned_to", { length: 200 }),
  escalatedTo: varchar("escalated_to", { length: 100 }),
  escalatedAt: timestamp("escalated_at"),
  escalationNote: text("escalation_note"),
  oppositionEntryId: integer("opposition_entry_id").references(() => oppositionResearch.id),
  reportedAt: timestamp("reported_at").defaultNow().notNull(),
  resolvedAt: timestamp("resolved_at"),
});
export type WarRoomIncident = typeof warRoomIncidents.$inferSelect;

// R5-098: append-only audit trail for the incident escalation workflow —
// every create/assign/escalate/resolve/status change is recorded with actor
// and timestamp so election-day triage is tribunal-evidence grade.
export const warRoomIncidentAudit = pgTable("war_room_incident_audit", {
  id: serial("id").primaryKey(),
  incidentId: integer("incident_id").notNull().references(() => warRoomIncidents.id),
  action: varchar("action", { length: 30 }).notNull(),
  actor: varchar("actor", { length: 200 }),
  fromStatus: varchar("from_status", { length: 20 }),
  toStatus: varchar("to_status", { length: 20 }),
  detail: text("detail"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type WarRoomIncidentAudit = typeof warRoomIncidentAudit.$inferSelect;

// ─── War Room Field Agents ────────────────────────────────────────────────────
export const fieldAgents = pgTable("field_agents", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  name: varchar("name", { length: 200 }).notNull(),
  phone: varchar("phone", { length: 20 }),
  assignedPu: varchar("assigned_pu", { length: 300 }),
  lga: varchar("lga", { length: 100 }),
  agentStatus: agentStatusEnum("agent_status").default("offline"),
  votersCounted: integer("voters_counted").default(0),
  lastCheckin: timestamp("last_checkin"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type FieldAgent = typeof fieldAgents.$inferSelect;

// ─── Results Data ─────────────────────────────────────────────────────────────
export const electionResults = pgTable("election_results", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  lga: varchar("lga", { length: 100 }).notNull(),
  // Ward-level result reporting; nullable — state/LGA rollups have no ward.
  ward: varchar("ward", { length: 100 }),
  candidateName: varchar("candidate_name", { length: 200 }).notNull(),
  party: varchar("party", { length: 100 }),
  votes: integer("votes").default(0),
  reportedAt: timestamp("reported_at").defaultNow().notNull(),
  isProjected: boolean("is_projected").default(false),
});
export type ElectionResult = typeof electionResults.$inferSelect;

// ─── Manifesto Sections ───────────────────────────────────────────────────────
export const manifestoSections = pgTable("manifesto_sections", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  sectionTitle: varchar("section_title", { length: 200 }).notNull(),
  summary: text("summary"),
  commitments: jsonb("commitments"),
  timeline: varchar("timeline", { length: 100 }),
  budget: varchar("budget", { length: 100 }),
  priority: priorityEnum("priority").default("high"),
  sortOrder: integer("sort_order").default(0),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
});
export type ManifestoSection = typeof manifestoSections.$inferSelect;

// ─── Petitions ────────────────────────────────────────────────────────────────
export const petitions = pgTable("petitions", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  title: varchar("title", { length: 400 }).notNull(),
  description: text("description"),
  targetSignatures: integer("target_signatures").default(10000),
  status: petitionStatusEnum("status").default("draft"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type Petition = typeof petitions.$inferSelect;

export const petitionSignatures = pgTable("petition_signatures", {
  id: serial("id").primaryKey(),
  petitionId: integer("petition_id").references(() => petitions.id),
  signerName: varchar("signer_name", { length: 200 }).notNull(),
  phone: varchar("phone", { length: 20 }),
  lga: varchar("lga", { length: 100 }),
  // R5-102: identity hash for dedupe (sha256 of normalized phone|name|lga)
  // and a verification tier — signatures are 'unverified' until a campaign
  // manager verifies them; duplicates are flagged, never silently counted.
  signerHash: varchar("signer_hash", { length: 64 }),
  verificationStatus: varchar("verification_status", { length: 20 }).default("unverified").notNull(),
  verifiedAt: timestamp("verified_at"),
  verifiedBy: varchar("verified_by", { length: 200 }),
  signedAt: timestamp("signed_at").defaultNow().notNull(),
});
export type PetitionSignature = typeof petitionSignatures.$inferSelect;

// ─── Diaspora Contacts ────────────────────────────────────────────────────────
export const diasporaContacts = pgTable("diaspora_contacts", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  name: varchar("name", { length: 200 }).notNull(),
  country: varchar("country", { length: 100 }),
  city: varchar("city", { length: 100 }),
  email: varchar("email", { length: 320 }),
  phone: varchar("phone", { length: 30 }),
  organization: varchar("organization", { length: 200 }),
  status: statusEnum("status").default("active"),
  // Money: exact decimal, never float. mode "number" keeps TS types compatible
  // with existing Number() sums and numeric seed values.
  pledgedAmount: numeric("pledged_amount", { precision: 15, scale: 2, mode: "number" }).default(0),
  notes: text("notes"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type DiasporaContact = typeof diasporaContacts.$inferSelect;

// ─── Endorsements ────────────────────────────────────────────────────────────
export const endorsements = pgTable("endorsements", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  endorserName: varchar("endorser_name", { length: 200 }).notNull(),
  title: varchar("title", { length: 200 }),
  organization: varchar("organization", { length: 200 }),
  category: varchar("category", { length: 100 }),
  statement: text("statement"),
  isPublic: boolean("is_public").default(true),
  endorsedAt: timestamp("endorsed_at").defaultNow().notNull(),
});
export type Endorsement = typeof endorsements.$inferSelect;

// ─── Fundraising Transactions ─────────────────────────────────────────────────
export const fundraisingTransactions = pgTable("fundraising_transactions", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  donorName: varchar("donor_name", { length: 200 }),
  // Money: exact decimal, never float.
  amount: numeric("amount", { precision: 15, scale: 2, mode: "number" }).notNull(),
  currency: varchar("currency", { length: 10 }).default("NGN"),
  source: varchar("source", { length: 100 }),
  category: varchar("category", { length: 100 }),
  notes: text("notes"),
  transactedAt: timestamp("transacted_at").defaultNow().notNull(),
  isVerified: boolean("is_verified").default(false),
  // Donor screening (W14, audit GAP-5): funding-legality classification.
  donorType: varchar("donor_type", { length: 20 }).default("individual_local"),
  sourceAttested: boolean("source_attested").default(false),
});
export type FundraisingTransaction = typeof fundraisingTransactions.$inferSelect;

// ─── Budget Items ─────────────────────────────────────────────────────────────
export const budgetItems = pgTable("budget_items", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  category: varchar("category", { length: 100 }).notNull(),
  description: varchar("description", { length: 300 }).notNull(),
  // Money: exact decimal, never float.
  budgetedAmount: numeric("budgeted_amount", { precision: 15, scale: 2, mode: "number" }).notNull(),
  spentAmount: numeric("spent_amount", { precision: 15, scale: 2, mode: "number" }).default(0),
  priority: priorityEnum("priority").default("medium"),
  notes: text("notes"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type BudgetItem = typeof budgetItems.$inferSelect;

// ─── R5-101: Statutory campaign-spend caps + append-only budget ledger ───────
// Electoral Act 2022 §88 caps per office, seeded by migration 0004 and
// administrable (owner) so amendments/INEC regulations can adjust them.
export const budgetStatutoryCaps = pgTable("budget_statutory_caps", {
  office: officeEnum("office").primaryKey(),
  capAmount: numeric("cap_amount", { precision: 15, scale: 2, mode: "number" }).notNull(),
  notes: text("notes"),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
});
export type BudgetStatutoryCap = typeof budgetStatutoryCaps.$inferSelect;

// Every budget mutation (create/update/spend change/delete) is recorded here;
// a trigger makes the table append-only (UPDATE/DELETE raise).
export const budgetSpendLedger = pgTable("budget_spend_ledger", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").notNull().references(() => candidateProfiles.id),
  budgetItemId: integer("budget_item_id"),
  changeType: varchar("change_type", { length: 20 }).notNull(),
  previousBudgeted: numeric("previous_budgeted", { precision: 15, scale: 2, mode: "number" }),
  newBudgeted: numeric("new_budgeted", { precision: 15, scale: 2, mode: "number" }),
  previousSpent: numeric("previous_spent", { precision: 15, scale: 2, mode: "number" }),
  newSpent: numeric("new_spent", { precision: 15, scale: 2, mode: "number" }),
  changedBy: varchar("changed_by", { length: 200 }),
  note: text("note"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type BudgetSpendLedgerEntry = typeof budgetSpendLedger.$inferSelect;

// ─── Media Monitoring ─────────────────────────────────────────────────────────
export const mediaItems = pgTable("media_items", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  source: varchar("source", { length: 200 }).notNull(),
  headline: text("headline").notNull(),
  sentiment: varchar("sentiment", { length: 20 }),
  sourceType: varchar("source_type", { length: 20 }).default("online"),
  reach: integer("reach").default(0),
  zone: varchar("zone", { length: 100 }),
  url: text("url"),
  publishedAt: timestamp("published_at"),
  notes: text("notes"),
  // NBC media/advert compliance gate (W14, audit GAP-6)
  complianceStatus: varchar("compliance_status", { length: 20 }).default("unreviewed"),
  complianceNotes: text("compliance_notes"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type MediaItem = typeof mediaItems.$inferSelect;

// ─── Debate Prep Notes ────────────────────────────────────────────────────────
export const debatePrepNotes = pgTable("debate_prep_notes", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  topic: varchar("topic", { length: 200 }).notNull(),
  keyMessage: text("key_message"),
  counterArguments: jsonb("counter_arguments"),
  statistics: jsonb("statistics"),
  practiceScore: integer("practice_score"),
  notes: text("notes"),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
});
export type DebatePrepNote = typeof debatePrepNotes.$inferSelect;

// ─── Simulation Runs ──────────────────────────────────────────────────────────
export const simulationRuns = pgTable("simulation_runs", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  scenario: varchar("scenario", { length: 50 }).default("baseline"),
  stateCode: varchar("state_code", { length: 10 }),
  iterations: integer("iterations").default(1000),
  weatherSeverity: integer("weather_severity").default(20),
  securityThreat: integer("security_threat").default(15),
  bvasReliability: integer("bvas_reliability").default(85),
  staffTraining: integer("staff_training").default(75),
  projectedTurnout: real("projected_turnout"),
  validVotesCast: integer("valid_votes_cast"),
  bvasFailureRate: real("bvas_failure_rate"),
  certificationEta: integer("certification_eta"),
  logisticsScore: integer("logistics_score"),
  securityIndex: integer("security_index"),
  rejectedBallots: integer("rejected_ballots"),
  monteCarloP50: real("monte_carlo_p50"),
  monteCarloP5: real("monte_carlo_p5"),
  monteCarloP95: real("monte_carlo_p95"),
  modelConfidence: real("model_confidence"),
  disruptions: jsonb("disruptions"),
  runAt: timestamp("run_at").defaultNow().notNull(),
  aiNarrative: text("ai_narrative"),
  label: varchar("label", { length: 120 }),
});
export type SimulationRun = typeof simulationRuns.$inferSelect;

// ─── Campaign Team Members ────────────────────────────────────────────────────
export const campaignMembers = pgTable("campaign_members", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id).notNull(),
  userId: integer("user_id").references(() => users.id),
  name: varchar("name", { length: 200 }).notNull(),
  email: varchar("email", { length: 320 }).notNull(),
  role: memberRoleEnum("role").default("viewer").notNull(),
  invitedAt: timestamp("invited_at").defaultNow().notNull(),
  acceptedAt: timestamp("accepted_at"),
  inviteToken: varchar("invite_token", { length: 64 }),
}, (table) => [
  // Supports the per-request getMyRoleForProfile membership lookup.
  index("campaign_members_profile_user_idx").on(table.profileId, table.userId),
]);
export type CampaignMember = typeof campaignMembers.$inferSelect;
export type InsertCampaignMember = typeof campaignMembers.$inferInsert;

// ─── Volunteer Tasks ──────────────────────────────────────────────────────────
export const volunteerTaskStatus = pgEnum("volunteer_task_status", ["pending", "in_progress", "completed", "cancelled"]);
export const volunteerTaskType = pgEnum("volunteer_task_type", ["canvassing", "polling_unit", "data_entry", "logistics", "security", "media", "other"]);

export const volunteerTasks = pgTable("volunteer_tasks", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  volunteerId: integer("volunteer_id").references(() => volunteers.id),
  title: varchar("title", { length: 300 }).notNull(),
  description: text("description"),
  taskType: volunteerTaskType("task_type").default("other"),
  status: volunteerTaskStatus("status").default("pending"),
  dueDate: timestamp("due_date"),
  completedAt: timestamp("completed_at"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});

export type VolunteerTask = typeof volunteerTasks.$inferSelect;
export type InsertVolunteerTask = typeof volunteerTasks.$inferInsert;

// ─── Debate Practice Scores ───────────────────────────────────────────────────
export const debatePracticeScores = pgTable("debate_practice_scores", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  topic: varchar("topic", { length: 200 }).notNull(),
  score: integer("score").notNull(),
  maxScore: integer("max_score").default(10),
  notes: text("notes"),
  scoredAt: timestamp("scored_at").defaultNow().notNull(),
});
export type DebatePracticeScore = typeof debatePracticeScores.$inferSelect;

// ─── Stakeholder Contacts ─────────────────────────────────────────────────────
export const stakeholderContacts = pgTable("stakeholder_contacts", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  name: varchar("name", { length: 200 }).notNull(),
  title: varchar("title", { length: 200 }),
  organization: varchar("organization", { length: 200 }),
  category: varchar("category", { length: 100 }),
  phone: varchar("phone", { length: 30 }),
  email: varchar("email", { length: 320 }),
  state: varchar("state", { length: 100 }),
  lga: varchar("lga", { length: 100 }),
  influenceLevel: priorityEnum("influence_level").default("medium"),
  relationship: varchar("relationship", { length: 50 }).default("neutral"),
  lastContact: date("last_contact"),
  nextAction: text("next_action"),
  notes: text("notes"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type StakeholderContact = typeof stakeholderContacts.$inferSelect;

// ─── Rate Limits (shared, Postgres-backed) ───────────────────────────────────
// Fixed-window counters backing server/_core/rateLimit.ts (login throttle,
// LLM per-user cap, public petition-sign dedup). Postgres is the shared store
// so limits hold across processes, replicas, and restarts — the in-memory
// fallback is non-production only.
export const rateLimits = pgTable("rate_limits", {
  key: text("key").notNull(),
  windowStart: timestamp("window_start", { withTimezone: true }).notNull(),
  count: integer("count").notNull().default(0),
}, (table) => [
  primaryKey({ columns: [table.key, table.windowStart] }),
]);
export type RateLimit = typeof rateLimits.$inferSelect;

// ─── W12: CA-Lessons Compliance Substrate (migration 0006) ──────────────────
// The Cambridge Analytica scandal's core architectural failure: personal data
// held with no persisted consent, no provenance, no access audit, no data-
// subject rights path. These tables are that infrastructure, aligned to the
// Nigeria Data Protection Act 2023 (ss.25/36/37). See
// /mnt/agents/output/research/ca_insight.md.

/** NDPA s.25 lawful bases usable for campaign processing. */
export const lawfulBasisEnum = pgEnum("lawful_basis", [
  "consent", "contract", "legal_obligation", "vital_interest",
  "public_interest", "legitimate_interest",
]);

/** Personal-data tables covered by the compliance substrate. */
export const subjectTableEnum = pgEnum("subject_table", [
  "voter_registrations", "diaspora_contacts", "stakeholder_contacts",
  "volunteers", "petition_signatures", "campaign_members",
]);

// Per-subject, per-purpose consent registry. Withdrawal is a state transition
// (withdrawn_at), never a deletion — history must remain auditable.
export const consentRecords = pgTable("consent_records", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").notNull().references(() => candidateProfiles.id),
  subjectTable: subjectTableEnum("subject_table").notNull(),
  subjectId: integer("subject_id").notNull(),
  lawfulBasis: lawfulBasisEnum("lawful_basis").notNull(),
  purpose: varchar("purpose", { length: 120 }).notNull(),
  consentMethod: varchar("consent_method", { length: 20 }), // verbal/written/digital
  consentGranted: boolean("consent_granted").default(false).notNull(),
  consentedAt: timestamp("consented_at"),
  withdrawnAt: timestamp("withdrawn_at"),
  retentionUntil: date("retention_until"),
  notes: text("notes"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
}, (table) => [
  index("consent_records_subject_idx").on(table.subjectTable, table.subjectId),
  index("consent_records_profile_idx").on(table.profileId),
]);
export type ConsentRecord = typeof consentRecords.$inferSelect;

// Append-only origin record for every personal-data row. Application code
// exposes no update/delete path — provenance opacity is what made the CA
// breach unauditable and the 2015 deletion certifications unverifiable.
export const dataProvenanceLedger = pgTable("data_provenance_ledger", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").notNull().references(() => candidateProfiles.id),
  subjectTable: subjectTableEnum("subject_table").notNull(),
  subjectId: integer("subject_id").notNull(),
  source: varchar("source", { length: 40 }).notNull(), // door_to_door/event_signup/...
  collectedBy: varchar("collected_by", { length: 200 }),
  collectedAt: timestamp("collected_at").defaultNow().notNull(),
  lawfulBasis: lawfulBasisEnum("lawful_basis").notNull(),
  notes: text("notes"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
}, (table) => [
  index("provenance_subject_idx").on(table.subjectTable, table.subjectId),
  index("provenance_profile_idx").on(table.profileId),
]);
export type DataProvenanceEntry = typeof dataProvenanceLedger.$inferSelect;

// Append-only log of PII list reads/exports: actor, table, rows, purpose.
export const dataAccessAudit = pgTable("data_access_audit", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").notNull().references(() => candidateProfiles.id),
  actorId: integer("actor_id"),
  actorName: varchar("actor_name", { length: 200 }),
  subjectTable: subjectTableEnum("subject_table").notNull(),
  action: varchar("action", { length: 30 }).notNull(), // list/export/dsar_erasure/...
  rowCount: integer("row_count"),
  purpose: varchar("purpose", { length: 200 }),
  createdAt: timestamp("created_at").defaultNow().notNull(),
}, (table) => [
  index("access_audit_profile_idx").on(table.profileId),
]);
export type DataAccessAuditEntry = typeof dataAccessAudit.$inferSelect;

// DSAR workflow — NDPA rights: access/rectification/erasure/restriction/
// portability/objection. due_at tracks the 30-day response expectation.
export const dataSubjectRequests = pgTable("data_subject_requests", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").notNull().references(() => candidateProfiles.id),
  requestType: varchar("request_type", { length: 20 }).notNull(),
  subjectName: varchar("subject_name", { length: 200 }).notNull(),
  subjectContact: varchar("subject_contact", { length: 320 }),
  subjectTable: subjectTableEnum("subject_table"),
  subjectId: integer("subject_id"),
  status: varchar("status", { length: 20 }).default("open").notNull(),
  receivedAt: timestamp("received_at").defaultNow().notNull(),
  dueAt: date("due_at").notNull(),
  fulfilledAt: timestamp("fulfilled_at"),
  rejectionReason: text("rejection_reason"),
  notes: text("notes"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
}, (table) => [
  index("dsar_profile_idx").on(table.profileId, table.status),
]);
export type DataSubjectRequest = typeof dataSubjectRequests.$inferSelect;

// ─── W13: CA-Parity Analytics (migration 0007) ───────────────────────────────
// Lawful analogue of Cambridge Analytica's psychographic + micro-targeting
// stack: psychometrics computed ONLY from consented panel responses, message
// experiments run ONLY on the campaign's own audiences with real event data.

// Consented survey panel — consent_id links to the W12 consent substrate;
// application code refuses panelists without an active consent record.
export const surveyPanelists = pgTable("survey_panelists", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").notNull().references(() => candidateProfiles.id),
  consentId: integer("consent_id").notNull().references(() => consentRecords.id),
  fullName: varchar("full_name", { length: 200 }).notNull(),
  stateCode: varchar("state_code", { length: 10 }),
  lga: varchar("lga", { length: 100 }),
  ward: varchar("ward", { length: 100 }),
  ageBand: varchar("age_band", { length: 10 }),
  gender: varchar("gender", { length: 20 }),
  status: varchar("status", { length: 20 }).default("active").notNull(),
  createdAt: timestamp("created_at").defaultNow().notNull(),
}, (table) => [
  index("survey_panelists_profile_idx").on(table.profileId, table.status),
]);
export type SurveyPanelist = typeof surveyPanelists.$inferSelect;

// Psychometric item responses (Likert 1–5). The ONLY lawful input to trait
// scoring — the platform never infers personality for non-respondents.
export const surveyResponses = pgTable("survey_responses", {
  id: serial("id").primaryKey(),
  panelistId: integer("panelist_id").notNull().references(() => surveyPanelists.id),
  instrument: varchar("instrument", { length: 40 }).notNull(), // e.g. OCEAN20
  itemKey: varchar("item_key", { length: 20 }).notNull(),      // e.g. E1, N3r (r = reverse)
  score: integer("score").notNull(),
  respondedAt: timestamp("responded_at").defaultNow().notNull(),
}, (table) => [
  index("survey_responses_panelist_idx").on(table.panelistId, table.instrument),
]);
export type SurveyResponse = typeof surveyResponses.$inferSelect;

// A/B message experiments on the campaign's own consented audiences.
export const messageTests = pgTable("message_tests", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").notNull().references(() => candidateProfiles.id),
  name: varchar("name", { length: 200 }).notNull(),
  channel: varchar("channel", { length: 40 }),
  status: varchar("status", { length: 20 }).default("draft").notNull(),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type MessageTest = typeof messageTests.$inferSelect;

export const messageVariants = pgTable("message_variants", {
  id: serial("id").primaryKey(),
  testId: integer("test_id").notNull().references(() => messageTests.id),
  label: varchar("label", { length: 40 }).notNull(),
  body: text("body").notNull(),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type MessageVariant = typeof messageVariants.$inferSelect;

// Real event counts per variant; analysis is computed from these rows only.
export const messageEvents = pgTable("message_events", {
  id: serial("id").primaryKey(),
  variantId: integer("variant_id").notNull().references(() => messageVariants.id),
  eventType: varchar("event_type", { length: 20 }).notNull(), // impression|response|conversion
  occurredAt: timestamp("occurred_at").defaultNow().notNull(),
}, (table) => [
  index("message_events_variant_idx").on(table.variantId, table.eventType),
]);
export type MessageEvent = typeof messageEvents.$inferSelect;

// ─── W14: Election Tribunal Tracking (audit GAP-3) ───────────────────────────
// Post/pre-election LEGAL petitions (tribunal/court cases) — distinct from
// `petitions`, which are signature drives.
export const electionPetitions = pgTable("election_petitions", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  electionName: varchar("election_name", { length: 200 }).notNull(),
  petitionType: varchar("petition_type", { length: 20 }).default("post_election").notNull(), // pre_election|post_election
  court: varchar("court", { length: 200 }),
  caseNumber: varchar("case_number", { length: 100 }),
  petitioner: varchar("petitioner", { length: 200 }),
  respondent: varchar("respondent", { length: 200 }),
  counsel: varchar("counsel", { length: 200 }),
  filedAt: timestamp("filed_at"),
  hearingDate: timestamp("hearing_date"),
  status: varchar("status", { length: 30 }).default("filed").notNull(), // filed|hearing|judgment|appealed|closed
  outcome: text("outcome"),
  notes: text("notes"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
}, (table) => [
  index("election_petitions_profile_idx").on(table.profileId),
]);
export type ElectionPetition = typeof electionPetitions.$inferSelect;
