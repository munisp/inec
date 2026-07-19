import {
  pgTable, serial, text, varchar, integer, boolean,
  timestamp, pgEnum, jsonb, real, date
} from "drizzle-orm/pg-core";

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

// ─── Users ────────────────────────────────────────────────────────────────────
export const users = pgTable("users", {
  id: serial("id").primaryKey(),
  openId: varchar("open_id", { length: 64 }).notNull().unique(),
  name: text("name"),
  email: varchar("email", { length: 320 }),
  loginMethod: varchar("login_method", { length: 64 }),
  role: userRoleEnum("role").default("user").notNull(),
  createdAt: timestamp("created_at").defaultNow().notNull(),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
  lastSignedIn: timestamp("last_signed_in").defaultNow().notNull(),
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
  createdAt: timestamp("created_at").defaultNow().notNull(),
  updatedAt: timestamp("updated_at").defaultNow().notNull(),
});
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
  registeredAt: timestamp("registered_at").defaultNow().notNull(),
  isVerified: boolean("is_verified").default(false),
});
export type VoterRegistration = typeof voterRegistrations.$inferSelect;

// ─── Polling Units ────────────────────────────────────────────────────────────
export const pollingUnits = pgTable("polling_units", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  puCode: varchar("pu_code", { length: 50 }),
  name: varchar("name", { length: 300 }).notNull(),
  ward: varchar("ward", { length: 100 }),
  lga: varchar("lga", { length: 100 }),
  stateCode: varchar("state_code", { length: 10 }),
  lat: real("lat"),
  lng: real("lng"),
  registeredVoters: integer("registered_voters").default(0),
  agentAssigned: varchar("agent_assigned", { length: 200 }),
  agentPhone: varchar("agent_phone", { length: 20 }),
  notes: text("notes"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type PollingUnit = typeof pollingUnits.$inferSelect;

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
  reportedAt: timestamp("reported_at").defaultNow().notNull(),
  resolvedAt: timestamp("resolved_at"),
});
export type WarRoomIncident = typeof warRoomIncidents.$inferSelect;

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
  pledgedAmount: real("pledged_amount").default(0),
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
  amount: real("amount").notNull(),
  currency: varchar("currency", { length: 10 }).default("NGN"),
  source: varchar("source", { length: 100 }),
  category: varchar("category", { length: 100 }),
  notes: text("notes"),
  transactedAt: timestamp("transacted_at").defaultNow().notNull(),
  isVerified: boolean("is_verified").default(false),
});
export type FundraisingTransaction = typeof fundraisingTransactions.$inferSelect;

// ─── Budget Items ─────────────────────────────────────────────────────────────
export const budgetItems = pgTable("budget_items", {
  id: serial("id").primaryKey(),
  profileId: integer("profile_id").references(() => candidateProfiles.id),
  category: varchar("category", { length: 100 }).notNull(),
  description: varchar("description", { length: 300 }).notNull(),
  budgetedAmount: real("budgeted_amount").notNull(),
  spentAmount: real("spent_amount").default(0),
  priority: priorityEnum("priority").default("medium"),
  notes: text("notes"),
  createdAt: timestamp("created_at").defaultNow().notNull(),
});
export type BudgetItem = typeof budgetItems.$inferSelect;

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
});
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
