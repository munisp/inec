import { drizzle } from "drizzle-orm/node-postgres";
import { Pool } from "pg";
import { eq, desc, and, sql, gte, lte } from "drizzle-orm";
import * as schema from "../drizzle/schema";
import type { InsertUser } from "../drizzle/schema";
import { ENV } from "./_core/env";

let _pool: Pool | null = null;
let _db: ReturnType<typeof drizzle> | null = null;

export function getDb() {
  if (!_db) {
    // Prefer POSTGRES_URL (local Postgres) over DATABASE_URL (platform MySQL/TiDB)
    const url = process.env.POSTGRES_URL || process.env.DATABASE_URL || ENV.databaseUrl;
    if (!url) {
      console.warn("[Database] DATABASE_URL not set");
      return null;
    }
    _pool = new Pool({ connectionString: url });
    _db = drizzle(_pool, { schema });
  }
  return _db;
}

// ─── Users ────────────────────────────────────────────────────────────────────
export async function upsertUser(user: InsertUser): Promise<void> {
  const db = getDb();
  if (!db) return;
  await db
    .insert(schema.users)
    .values({
      ...user,
      role: user.openId === ENV.ownerOpenId ? "admin" : (user.role ?? "user"),
    })
    .onConflictDoUpdate({
      target: schema.users.openId,
      set: {
        name: user.name,
        email: user.email,
        loginMethod: user.loginMethod,
        lastSignedIn: new Date(),
        updatedAt: new Date(),
      },
    });
}

export async function getUserByOpenId(openId: string) {
  const db = getDb();
  if (!db) return undefined;
  const rows = await db
    .select()
    .from(schema.users)
    .where(eq(schema.users.openId, openId))
    .limit(1);
  return rows[0];
}

// ─── Candidate Profiles ───────────────────────────────────────────────────────
export async function getOrCreateDefaultProfile(userId?: number) {
  const db = getDb();
  if (!db) return null;
  const rows = await db
    .select()
    .from(schema.candidateProfiles)
    .where(eq(schema.candidateProfiles.isActive, true))
    .orderBy(schema.candidateProfiles.id)
    .limit(1);
  if (rows.length > 0) return rows[0];
  // Create a default profile
  const inserted = await db
    .insert(schema.candidateProfiles)
    .values({
      candidateName: "Our Candidate",
      partyName: "PDP",
      partyColor: "#006400",
      stateCode: "OYO",
      stateName: "Oyo",
      office: "Governor",
      religion: "Mixed / Prefer not to say",
      gender: "Male",
      geopoliticalZone: "South-West",
      isActive: true,
      userId: userId ?? null,
    })
    .returning();
  return inserted[0];
}

export async function updateProfile(id: number, data: Partial<schema.InsertCandidateProfile>) {
  const db = getDb();
  if (!db) return null;
  const rows = await db
    .update(schema.candidateProfiles)
    .set({ ...data, updatedAt: new Date() })
    .where(eq(schema.candidateProfiles.id, id))
    .returning();
  return rows[0];
}

// ─── Timeline Events ──────────────────────────────────────────────────────────
export async function getTimelineEvents(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.timelineEvents)
    .where(eq(schema.timelineEvents.profileId, profileId))
    .orderBy(schema.timelineEvents.eventDate);
}

export async function upsertTimelineEvent(data: schema.InsertTimelineEvent) {
  const db = getDb();
  if (!db) return null;
  if (data.id) {
    const rows = await db
      .update(schema.timelineEvents)
      .set(data)
      .where(eq(schema.timelineEvents.id, data.id))
      .returning();
    return rows[0];
  }
  const rows = await db.insert(schema.timelineEvents).values(data).returning();
  return rows[0];
}

export async function deleteTimelineEvent(id: number) {
  const db = getDb();
  if (!db) return;
  await db.delete(schema.timelineEvents).where(eq(schema.timelineEvents.id, id));
}

// ─── Voter Registrations ──────────────────────────────────────────────────────
export async function getVoterRegistrations(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.voterRegistrations)
    .where(eq(schema.voterRegistrations.profileId, profileId))
    .orderBy(desc(schema.voterRegistrations.registeredAt));
}

export async function addVoterRegistration(data: typeof schema.voterRegistrations.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.voterRegistrations).values(data).returning();
  return rows[0];
}

// ─── Polling Units ────────────────────────────────────────────────────────────
export async function getPollingUnits(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.pollingUnits)
    .where(eq(schema.pollingUnits.profileId, profileId))
    .orderBy(schema.pollingUnits.lga, schema.pollingUnits.name);
}

export async function upsertPollingUnit(data: typeof schema.pollingUnits.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  if (data.id) {
    const rows = await db.update(schema.pollingUnits).set(data).where(eq(schema.pollingUnits.id, data.id)).returning();
    return rows[0];
  }
  const rows = await db.insert(schema.pollingUnits).values(data).returning();
  return rows[0];
}

// ─── Volunteers ───────────────────────────────────────────────────────────────
export async function getVolunteers(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.volunteers)
    .where(eq(schema.volunteers.profileId, profileId))
    .orderBy(desc(schema.volunteers.joinedAt));
}

export async function addVolunteer(data: typeof schema.volunteers.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.volunteers).values(data).returning();
  return rows[0];
}

export async function updateVolunteerStatus(id: number, status: "active" | "inactive" | "pending" | "completed" | "cancelled") {
  const db = getDb();
  if (!db) return null;
  const rows = await db.update(schema.volunteers).set({ status }).where(eq(schema.volunteers.id, id)).returning();
  return rows[0];
}

// ─── Press Releases ───────────────────────────────────────────────────────────
export async function getPressReleases(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.pressReleases)
    .where(eq(schema.pressReleases.profileId, profileId))
    .orderBy(desc(schema.pressReleases.createdAt));
}

export async function savePressRelease(data: typeof schema.pressReleases.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.pressReleases).values(data).returning();
  return rows[0];
}

// ─── Social Media Posts ───────────────────────────────────────────────────────
export async function getSocialPosts(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.socialMediaPosts)
    .where(eq(schema.socialMediaPosts.profileId, profileId))
    .orderBy(desc(schema.socialMediaPosts.createdAt));
}

export async function saveSocialPost(data: typeof schema.socialMediaPosts.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.socialMediaPosts).values(data).returning();
  return rows[0];
}

// ─── Compliance Items ─────────────────────────────────────────────────────────
export async function getComplianceItems(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.complianceItems)
    .where(eq(schema.complianceItems.profileId, profileId))
    .orderBy(schema.complianceItems.category);
}

export async function upsertComplianceItem(data: typeof schema.complianceItems.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  if (data.id) {
    const rows = await db.update(schema.complianceItems).set({ ...data, updatedAt: new Date() }).where(eq(schema.complianceItems.id, data.id)).returning();
    return rows[0];
  }
  const rows = await db.insert(schema.complianceItems).values(data).returning();
  return rows[0];
}

// ─── Opposition Research ──────────────────────────────────────────────────────
export async function getOppositionResearch(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.oppositionResearch)
    .where(eq(schema.oppositionResearch.profileId, profileId))
    .orderBy(schema.oppositionResearch.opponentName);
}

export async function upsertOppositionEntry(data: typeof schema.oppositionResearch.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  if (data.id) {
    const rows = await db.update(schema.oppositionResearch).set({ ...data, updatedAt: new Date() }).where(eq(schema.oppositionResearch.id, data.id)).returning();
    return rows[0];
  }
  const rows = await db.insert(schema.oppositionResearch).values(data).returning();
  return rows[0];
}

// ─── War Room ─────────────────────────────────────────────────────────────────
export async function getWarRoomIncidents(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.warRoomIncidents)
    .where(eq(schema.warRoomIncidents.profileId, profileId))
    .orderBy(desc(schema.warRoomIncidents.reportedAt));
}

export async function addWarRoomIncident(data: typeof schema.warRoomIncidents.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.warRoomIncidents).values(data).returning();
  return rows[0];
}

export async function updateIncidentStatus(id: number, status: "open" | "escalated" | "resolved") {
  const db = getDb();
  if (!db) return null;
  const rows = await db
    .update(schema.warRoomIncidents)
    .set({ status, resolvedAt: status === "resolved" ? new Date() : null })
    .where(eq(schema.warRoomIncidents.id, id))
    .returning();
  return rows[0];
}

export async function getFieldAgents(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.fieldAgents)
    .where(eq(schema.fieldAgents.profileId, profileId))
    .orderBy(schema.fieldAgents.name);
}

export async function upsertFieldAgent(data: typeof schema.fieldAgents.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  if (data.id) {
    const rows = await db.update(schema.fieldAgents).set({ ...data, lastCheckin: new Date() }).where(eq(schema.fieldAgents.id, data.id)).returning();
    return rows[0];
  }
  const rows = await db.insert(schema.fieldAgents).values(data).returning();
  return rows[0];
}

// ─── Election Results ─────────────────────────────────────────────────────────
export async function getElectionResults(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.electionResults)
    .where(eq(schema.electionResults.profileId, profileId))
    .orderBy(schema.electionResults.lga, schema.electionResults.candidateName);
}

export async function upsertElectionResult(data: typeof schema.electionResults.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.electionResults).values(data).returning();
  return rows[0];
}

// ─── Manifesto ────────────────────────────────────────────────────────────────
export async function getManifestoSections(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.manifestoSections)
    .where(eq(schema.manifestoSections.profileId, profileId))
    .orderBy(schema.manifestoSections.sortOrder);
}

export async function upsertManifestoSection(data: typeof schema.manifestoSections.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  if (data.id) {
    const rows = await db.update(schema.manifestoSections).set({ ...data, updatedAt: new Date() }).where(eq(schema.manifestoSections.id, data.id)).returning();
    return rows[0];
  }
  const rows = await db.insert(schema.manifestoSections).values(data).returning();
  return rows[0];
}

export async function deleteManifestoSection(id: number) {
  const db = getDb();
  if (!db) return;
  await db.delete(schema.manifestoSections).where(eq(schema.manifestoSections.id, id));
}

// ─── Petitions ────────────────────────────────────────────────────────────────
export async function getPetitions(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.petitions)
    .where(eq(schema.petitions.profileId, profileId))
    .orderBy(desc(schema.petitions.createdAt));
}

export async function createPetition(data: typeof schema.petitions.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.petitions).values(data).returning();
  return rows[0];
}

export async function getPetitionSignatures(petitionId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.petitionSignatures)
    .where(eq(schema.petitionSignatures.petitionId, petitionId))
    .orderBy(desc(schema.petitionSignatures.signedAt));
}

export async function addPetitionSignature(data: typeof schema.petitionSignatures.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.petitionSignatures).values(data).returning();
  return rows[0];
}

export async function getPetitionSignatureCount(petitionId: number) {
  const db = getDb();
  if (!db) return 0;
  const result = await db
    .select({ count: sql<number>`count(*)::int` })
    .from(schema.petitionSignatures)
    .where(eq(schema.petitionSignatures.petitionId, petitionId));
  return result[0]?.count ?? 0;
}

// ─── Diaspora ─────────────────────────────────────────────────────────────────
export async function getDiasporaContacts(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.diasporaContacts)
    .where(eq(schema.diasporaContacts.profileId, profileId))
    .orderBy(schema.diasporaContacts.country, schema.diasporaContacts.name);
}

export async function addDiasporaContact(data: typeof schema.diasporaContacts.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.diasporaContacts).values(data).returning();
  return rows[0];
}

// ─── Endorsements ────────────────────────────────────────────────────────────
export async function getEndorsements(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.endorsements)
    .where(eq(schema.endorsements.profileId, profileId))
    .orderBy(desc(schema.endorsements.endorsedAt));
}

export async function addEndorsement(data: typeof schema.endorsements.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.endorsements).values(data).returning();
  return rows[0];
}

// ─── Fundraising ─────────────────────────────────────────────────────────────
export async function getFundraisingTransactions(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.fundraisingTransactions)
    .where(eq(schema.fundraisingTransactions.profileId, profileId))
    .orderBy(desc(schema.fundraisingTransactions.transactedAt));
}

export async function addFundraisingTransaction(data: typeof schema.fundraisingTransactions.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.fundraisingTransactions).values(data).returning();
  return rows[0];
}

// ─── Budget ───────────────────────────────────────────────────────────────────
export async function getBudgetItems(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.budgetItems)
    .where(eq(schema.budgetItems.profileId, profileId))
    .orderBy(schema.budgetItems.category);
}

export async function upsertBudgetItem(data: typeof schema.budgetItems.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  if (data.id) {
    const rows = await db.update(schema.budgetItems).set(data).where(eq(schema.budgetItems.id, data.id)).returning();
    return rows[0];
  }
  const rows = await db.insert(schema.budgetItems).values(data).returning();
  return rows[0];
}

export async function deleteBudgetItem(id: number) {
  const db = getDb();
  if (!db) return;
  await db.delete(schema.budgetItems).where(eq(schema.budgetItems.id, id));
}

// ─── Media Monitoring ─────────────────────────────────────────────────────────
export async function getMediaItems(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.mediaItems)
    .where(eq(schema.mediaItems.profileId, profileId))
    .orderBy(desc(schema.mediaItems.createdAt));
}

export async function addMediaItem(data: typeof schema.mediaItems.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.mediaItems).values(data).returning();
  return rows[0];
}

// ─── Debate Prep ──────────────────────────────────────────────────────────────
export async function getDebatePrepNotes(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.debatePrepNotes)
    .where(eq(schema.debatePrepNotes.profileId, profileId))
    .orderBy(schema.debatePrepNotes.topic);
}

export async function upsertDebatePrepNote(data: typeof schema.debatePrepNotes.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  if (data.id) {
    const rows = await db.update(schema.debatePrepNotes).set({ ...data, updatedAt: new Date() }).where(eq(schema.debatePrepNotes.id, data.id)).returning();
    return rows[0];
  }
  const rows = await db.insert(schema.debatePrepNotes).values(data).returning();
  return rows[0];
}

// ─── Simulation Runs ──────────────────────────────────────────────────────────
export async function getSimulationRuns(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select()
    .from(schema.simulationRuns)
    .where(eq(schema.simulationRuns.profileId, profileId))
    .orderBy(desc(schema.simulationRuns.runAt))
    .limit(20);
}

export async function saveSimulationRun(data: typeof schema.simulationRuns.$inferInsert) {
  const db = getDb();
  if (!db) return null;
  const rows = await db.insert(schema.simulationRuns).values(data).returning();
  return rows[0];
}

// ─── User-scoped Profile ───────────────────────────────────────────────────────
export async function getOrCreateUserProfile(userId: number) {
  const db = getDb();
  if (!db) return null;
  // Find existing profile for this user
  const rows = await db
    .select()
    .from(schema.candidateProfiles)
    .where(and(eq(schema.candidateProfiles.userId, userId), eq(schema.candidateProfiles.isActive, true)))
    .limit(1);
  if (rows.length > 0) return rows[0];
  // Create a new profile for this user
  const inserted = await db
    .insert(schema.candidateProfiles)
    .values({
      candidateName: "Our Candidate",
      partyName: "PDP",
      partyColor: "#006400",
      stateCode: "OY",
      stateName: "Oyo",
      office: "Governor",
      geopoliticalZone: "South-West",
      isActive: true,
      userId,
    })
    .returning();
  return inserted[0];
}

export async function getProfileById(id: number) {
  const db = getDb();
  if (!db) return null;
  const rows = await db
    .select()
    .from(schema.candidateProfiles)
    .where(eq(schema.candidateProfiles.id, id))
    .limit(1);
  return rows[0] ?? null;
}

// ─── Upcoming Deadlines ────────────────────────────────────────────────────────
export async function getAllProfiles() {
  const db = getDb();
  if (!db) return [];
  return db.select().from(schema.candidateProfiles).orderBy(schema.candidateProfiles.id);
}

export async function getUpcomingDeadlines(profileId: number, withinHours: number) {
  const db = getDb();
  if (!db) return [];
  const now = new Date();
  const cutoff = new Date(now.getTime() + withinHours * 60 * 60 * 1000);
  return db
    .select()
    .from(schema.timelineEvents)
    .where(
      and(
        eq(schema.timelineEvents.profileId, profileId),
        eq(schema.timelineEvents.priority, "critical"),
        gte(schema.timelineEvents.eventDate, now.toISOString().split("T")[0]),
        lte(schema.timelineEvents.eventDate, cutoff.toISOString().split("T")[0])
      )
    )
    .orderBy(schema.timelineEvents.eventDate);
}

// ─── Campaign Team Members ────────────────────────────────────────────────────
export async function getCampaignMembers(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db.select().from(schema.campaignMembers)
    .where(eq(schema.campaignMembers.profileId, profileId))
    .orderBy(schema.campaignMembers.invitedAt);
}

export async function inviteCampaignMember(input: {
  profileId: number; email: string; name: string; role: "manager" | "viewer";
}) {
  const db = getDb();
  if (!db) throw new Error("DB not available");
  const [row] = await db.insert(schema.campaignMembers).values({
    profileId: input.profileId,
    email: input.email,
    name: input.name,
    role: input.role,
  }).returning();
  return row;
}

export async function updateMemberRole(memberId: number, role: "manager" | "viewer") {
  const db = getDb();
  if (!db) throw new Error("DB not available");
  const [row] = await db.update(schema.campaignMembers)
    .set({ role })
    .where(eq(schema.campaignMembers.id, memberId))
    .returning();
  return row;
}

export async function removeCampaignMember(memberId: number) {
  const db = getDb();
  if (!db) throw new Error("DB not available");
  await db.delete(schema.campaignMembers)
    .where(eq(schema.campaignMembers.id, memberId));
  return { success: true };
}

// ─── Get current user's role for a profile ────────────────────────────────────
export async function getMyRoleForProfile(profileId: number, userId: number): Promise<"owner" | "manager" | "viewer"> {
  const db = getDb();
  if (!db) return "viewer";
  // Check if user owns the profile
  const profile = await db.select({ userId: schema.candidateProfiles.userId })
    .from(schema.candidateProfiles)
    .where(and(eq(schema.candidateProfiles.id, profileId), eq(schema.candidateProfiles.userId, userId)))
    .limit(1);
  if (profile.length > 0) return "owner";
  // Check campaign_members table
  const member = await db.select({ role: schema.campaignMembers.role })
    .from(schema.campaignMembers)
    .where(and(eq(schema.campaignMembers.profileId, profileId), eq(schema.campaignMembers.userId, userId)))
    .limit(1);
  if (member.length > 0) return member[0].role as "manager" | "viewer";
  return "viewer";
}

// ─── Get single petition by ID (public) ──────────────────────────────────────
export async function getPetitionById(petitionId: number) {
  const db = getDb();
  if (!db) return null;
  const rows = await db
    .select()
    .from(schema.petitions)
    .where(eq(schema.petitions.id, petitionId))
    .limit(1);
  return rows[0] ?? null;
}

// ─── Dashboard KPI aggregation ────────────────────────────────────────────────
export async function getDashboardKPIs(profileId: number) {
  const db = getDb();
  if (!db) return null;

  const [
    volunteerRows,
    complianceRows,
    donationRows,
    budgetRows,
    timelineRows,
    petitionRows,
    memberRows,
    incidentRows,
  ] = await Promise.all([
    db.select({ count: sql<number>`count(*)::int` }).from(schema.volunteers).where(eq(schema.volunteers.profileId, profileId)),
    db.select({ status: schema.complianceItems.status }).from(schema.complianceItems).where(eq(schema.complianceItems.profileId, profileId)),
    db.select({ amount: schema.fundraisingTransactions.amount }).from(schema.fundraisingTransactions).where(eq(schema.fundraisingTransactions.profileId, profileId)),
    db.select({ budgetedAmount: schema.budgetItems.budgetedAmount }).from(schema.budgetItems).where(eq(schema.budgetItems.profileId, profileId)),
    db.select({ eventDate: schema.timelineEvents.eventDate, status: schema.timelineEvents.status, priority: schema.timelineEvents.priority }).from(schema.timelineEvents).where(eq(schema.timelineEvents.profileId, profileId)).orderBy(schema.timelineEvents.eventDate),
    db.select({ count: sql<number>`count(*)::int` }).from(schema.petitions).where(eq(schema.petitions.profileId, profileId)),
    db.select({ count: sql<number>`count(*)::int` }).from(schema.campaignMembers).where(eq(schema.campaignMembers.profileId, profileId)),
    db.select({ severity: schema.warRoomIncidents.severity, status: schema.warRoomIncidents.status }).from(schema.warRoomIncidents).where(eq(schema.warRoomIncidents.profileId, profileId)),
  ]);

  const totalVolunteers = volunteerRows[0]?.count ?? 0;
  const complianceTotal = complianceRows.length;
  const complianceCompliant = complianceRows.filter(r => r.status === 'compliant').length;
  const complianceScore = complianceTotal > 0 ? Math.round((complianceCompliant / complianceTotal) * 100) : 0;
  const totalFundraising = donationRows.reduce((s, r) => s + Number(r.amount ?? 0), 0);
  const totalBudget = budgetRows.reduce((s, r) => s + Number(r.budgetedAmount ?? 0), 0);
  const totalPetitions = petitionRows[0]?.count ?? 0;
  const totalTeamMembers = memberRows[0]?.count ?? 0;

  const now = new Date();
  const upcomingDeadline = timelineRows.find(r => r.eventDate && new Date(r.eventDate) > now && r.status !== 'completed');
  const daysToNextDeadline = upcomingDeadline?.eventDate
    ? Math.ceil((new Date(upcomingDeadline.eventDate).getTime() - now.getTime()) / 86400000)
    : null;

  const completedMilestones = timelineRows.filter(r => r.status === 'completed').length;
  const totalMilestones = timelineRows.length;

  const activeIncidents = incidentRows.filter(r => r.status !== 'resolved' && r.status !== 'escalated').length;
  const criticalIncidents = incidentRows.filter(r => r.severity === 'critical' && (r.status !== 'resolved' && r.status !== 'escalated')).length;

  return {
    totalVolunteers,
    complianceScore,
    complianceCompliant,
    complianceTotal,
    totalFundraising,
    totalBudget,
    totalPetitions,
    totalTeamMembers,
    daysToNextDeadline,
    nextDeadlineDate: upcomingDeadline?.eventDate ?? null,
    completedMilestones,
    totalMilestones,
    activeIncidents,
    criticalIncidents,
  };
}

// ─── Volunteer Tasks ──────────────────────────────────────────────────────────
export async function getVolunteerTasks(profileId: number, volunteerId?: number) {
  const db = await getDb();
  if (!db) return [];
  const { volunteerTasks } = await import("../drizzle/schema");
  const { eq, and } = await import("drizzle-orm");
  const conditions = volunteerId
    ? and(eq(volunteerTasks.profileId, profileId), eq(volunteerTasks.volunteerId, volunteerId))
    : eq(volunteerTasks.profileId, profileId);
  return db.select().from(volunteerTasks).where(conditions).orderBy(volunteerTasks.createdAt);
}

export async function createVolunteerTask(data: {
  profileId: number; volunteerId?: number; title: string; description?: string;
  taskType?: string; status?: string; dueDate?: string;
}) {
  const db = await getDb();
  if (!db) return null;
  const { volunteerTasks } = await import("../drizzle/schema");
  const insertData: Record<string, unknown> = {
    profileId: data.profileId,
    title: data.title,
  };
  if (data.volunteerId) insertData.volunteerId = data.volunteerId;
  if (data.description) insertData.description = data.description;
  if (data.taskType) insertData.taskType = data.taskType;
  if (data.status) insertData.status = data.status;
  if (data.dueDate) insertData.dueDate = new Date(data.dueDate);
  const result = await db.insert(volunteerTasks).values(insertData as any).returning();
  return result[0] ?? null;
}

export async function updateVolunteerTaskStatus(id: number, status: string) {
  const db = await getDb();
  if (!db) return null;
  const { volunteerTasks } = await import("../drizzle/schema");
  const { eq } = await import("drizzle-orm");
  const updateData: Record<string, unknown> = { status };
  if (status === "completed") updateData.completedAt = new Date();
  const result = await db.update(volunteerTasks).set(updateData as any).where(eq(volunteerTasks.id, id)).returning();
  return result[0] ?? null;
}

export async function deleteVolunteerTask(id: number) {
  const db = await getDb();
  if (!db) return null;
  const { volunteerTasks } = await import("../drizzle/schema");
  const { eq } = await import("drizzle-orm");
  await db.delete(volunteerTasks).where(eq(volunteerTasks.id, id));
  return { success: true };
}

// ─── Debate Practice Scores ───────────────────────────────────────────────────
export async function getDebatePracticeScores(profileId: number) {
  const db = await getDb();
  if (!db) return [];
  const { debatePracticeScores } = await import("../drizzle/schema");
  const { eq, desc } = await import("drizzle-orm");
  return db.select().from(debatePracticeScores).where(eq(debatePracticeScores.profileId, profileId)).orderBy(desc(debatePracticeScores.scoredAt));
}

export async function addDebatePracticeScore(data: { profileId: number; topic: string; score: number; maxScore?: number; notes?: string }) {
  const db = await getDb();
  if (!db) return null;
  const { debatePracticeScores } = await import("../drizzle/schema");
  const result = await db.insert(debatePracticeScores).values(data as any).returning();
  return result[0] ?? null;
}

// ─── Stakeholder Contacts ─────────────────────────────────────────────────────
export async function getStakeholderContacts(profileId: number) {
  const db = await getDb();
  if (!db) return [];
  const { stakeholderContacts } = await import("../drizzle/schema");
  const { eq, desc } = await import("drizzle-orm");
  return db.select().from(stakeholderContacts).where(eq(stakeholderContacts.profileId, profileId)).orderBy(desc(stakeholderContacts.createdAt));
}

export async function upsertStakeholderContact(data: any) {
  const db = await getDb();
  if (!db) return null;
  const { stakeholderContacts } = await import("../drizzle/schema");
  const { eq } = await import("drizzle-orm");
  if (data.id) {
    const { id, ...rest } = data;
    await db.update(stakeholderContacts).set(rest).where(eq(stakeholderContacts.id, id));
    return { id };
  }
  const result = await db.insert(stakeholderContacts).values(data).returning();
  return result[0] ?? null;
}

export async function deleteStakeholderContact(id: number) {
  const db = await getDb();
  if (!db) return null;
  const { stakeholderContacts } = await import("../drizzle/schema");
  const { eq } = await import("drizzle-orm");
  await db.delete(stakeholderContacts).where(eq(stakeholderContacts.id, id));
  return { success: true };
}
