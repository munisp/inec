import { drizzle } from "drizzle-orm/node-postgres";
import { Pool } from "pg";
import { eq, desc, and, sql, gte, lte, isNull } from "drizzle-orm";
import { TRPCError } from "@trpc/server";
import * as schema from "../drizzle/schema";
import { ENV } from "./_core/env";

let _pool: Pool | null = null;
let _db: ReturnType<typeof drizzle> | null = null;

/** Drain the connection pool (graceful shutdown). */
export async function closeDb(): Promise<void> {
  const pool = _pool;
  _pool = null;
  _db = null;
  if (pool) await pool.end();
}

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

// ─── Cross-tenant upsert guard ───────────────────────────────────────────────
// SECURITY: every `if (data.id)` update path below must match BOTH the row id
// AND the caller's profileId — a bare `WHERE id = ?` lets any tenant rewrite
// another campaign's rows by guessing ids (IDOR). profileId itself is never
// overwritten by an update. Zero matched rows → NOT_FOUND (never a silent
// no-op), so callers cannot distinguish "no such row" from "not your row".
function assertUpdated<T>(rows: T[], label: string): T {
  if (rows.length === 0) {
    throw new TRPCError({ code: "NOT_FOUND", message: `${label} not found` });
  }
  return rows[0];
}

// The drizzle insert types mark profileId optional; a tenant-guarded UPDATE is
// meaningless without it, so require it explicitly rather than letting a
// null/undefined guard value silently match nothing (or worse, everything).
function requireTenantId(profileId: number | null | undefined): number {
  if (typeof profileId !== "number" || !Number.isInteger(profileId)) {
    throw new TRPCError({ code: "BAD_REQUEST", message: "profileId is required" });
  }
  return profileId;
}

// ─── Users ──────────────────────────────────────────────────────────────────
// `users` is owned by the Go backend (username/password_hash local login).
// OAuth's `openId` concept doesn't exist there, so it's mapped onto `username`
// for any OAuth-created accounts. In practice this app authenticates via the
// local-login route (server/_core/localAuth.ts), not OAuth, so this path is
// mostly dormant — kept working so it doesn't silently break if OAuth is ever
// configured for a real Manus deployment.
export async function upsertUser(user: {
  openId: string;
  name?: string | null;
  email?: string | null;
  loginMethod?: string | null;
}): Promise<void> {
  const db = getDb();
  if (!db) return;
  const isOwner = user.openId === ENV.ownerOpenId;
  await db
    .insert(schema.users)
    .values({
      username: user.openId,
      passwordHash: "oauth-account:no-local-password",
      fullName: user.name || user.openId,
      role: isOwner ? "admin" : "public",
    })
    .onConflictDoUpdate({
      target: schema.users.username,
      set: {
        fullName: user.name || user.openId,
      },
    });
}

export async function getUserByOpenId(openId: string) {
  return getUserByUsername(openId);
}

export async function getUserByUsername(username: string) {
  const db = getDb();
  if (!db) return undefined;
  const rows = await db
    .select()
    .from(schema.users)
    .where(eq(schema.users.username, username))
    .limit(1);
  return rows[0];
}

// ─── Candidate Profiles ───────────────────────────────────────────────────────
export async function getOrCreateDefaultProfile(_userId?: number) {
  const db = getDb();
  if (!db) return null;
  const rows = await db
    .select()
    .from(schema.candidateProfiles)
    .where(eq(schema.candidateProfiles.isActive, true))
    .orderBy(schema.candidateProfiles.id)
    .limit(1);
  // A missing profile is an operational setup state, not a reason to fabricate a candidate.
  return rows[0] ?? null;
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
    // SECURITY: tenant-guarded update — never set profileId on update.
    const { id, profileId, ...rest } = data;
    const rows = await db
      .update(schema.timelineEvents)
      .set(rest)
      .where(and(eq(schema.timelineEvents.id, id), eq(schema.timelineEvents.profileId, requireTenantId(data.profileId))))
      .returning();
    return assertUpdated(rows, "Timeline event");
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

// ─── Bulk Import Guards ───────────────────────────────────────────────────────
// SECURITY: bulk imports are capped at the db layer (in addition to any router-
// level zod caps) so a single request can never force unbounded row writes,
// and large imports are written in bounded chunks instead of one huge INSERT.
export const MAX_BULK_IMPORT_ROWS = 500;
export const BULK_INSERT_CHUNK_SIZE = 100;

function assertBulkImportSize(rows: unknown, label: string): asserts rows is unknown[] {
  if (!Array.isArray(rows)) {
    throw new Error(`${label}: expected an array of rows`);
  }
  if (rows.length > MAX_BULK_IMPORT_ROWS) {
    throw new Error(
      `${label}: too many rows (${rows.length}); at most ${MAX_BULK_IMPORT_ROWS} rows are allowed per import`
    );
  }
}

/** drizzle transaction handles expose the same query-building API as the db object. */
type QueryExecutor = Pick<NonNullable<ReturnType<typeof getDb>>, "insert" | "update" | "delete" | "select">;

// Normalize a voter row: the column is `vin`, but legacy callers passed
// `vinNumber`; accept either and always write to `vin`.
function normalizeVoterRow(row: Record<string, unknown>) {
  const { vinNumber, ...rest } = row;
  return { ...rest, vin: (rest.vin as string | undefined) ?? (vinNumber as string | undefined) };
}

export async function addVoterRegistration(
  data: typeof schema.voterRegistrations.$inferInsert & { vinNumber?: string }
) {
  const db = getDb();
  if (!db) return null;
  const rows = await db
    .insert(schema.voterRegistrations)
    .values(normalizeVoterRow(data as Record<string, unknown>) as typeof schema.voterRegistrations.$inferInsert)
    .returning();
  return rows[0];
}

export async function bulkAddVoterRegistrations(
  profileId: number,
  rows: Array<Record<string, unknown>>
) {
  const db = getDb();
  if (!db) return { inserted: 0 };
  assertBulkImportSize(rows, "voter bulk import");
  let inserted = 0;
  for (let i = 0; i < rows.length; i += BULK_INSERT_CHUNK_SIZE) {
    const chunk = rows
      .slice(i, i + BULK_INSERT_CHUNK_SIZE)
      .filter(r => typeof r?.fullName === "string" && (r.fullName as string).trim().length > 0)
      .map(r => ({ ...normalizeVoterRow(r), profileId }));
    if (chunk.length === 0) continue;
    const result = await db
      .insert(schema.voterRegistrations)
      .values(chunk as Array<typeof schema.voterRegistrations.$inferInsert>)
      .returning({ id: schema.voterRegistrations.id });
    inserted += result.length;
  }
  return { inserted };
}

// ─── Polling Units ──────────────────────────────────────────────────────────
// `polling_units` is the canonical, Go-owned national PU registry (code/name/
// ward_code/registered_voters/latitude/longitude). Per-candidate operational
// fields (agent, status, notes) live in `campaign_pu_assignments`, keyed by
// (profileId, puCode), so this app never writes campaign-specific data onto
// the shared registry.
export async function getPollingUnits(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db
    .select({
      id: schema.campaignPuAssignments.id,
      profileId: schema.campaignPuAssignments.profileId,
      puCode: schema.pollingUnits.code,
      name: schema.pollingUnits.name,
      ward: schema.pollingUnits.wardCode,
      latitude: schema.pollingUnits.latitude,
      longitude: schema.pollingUnits.longitude,
      registeredVoters: schema.pollingUnits.registeredVoters,
      agentName: schema.campaignPuAssignments.agentName,
      agentPhone: schema.campaignPuAssignments.agentPhone,
      status: schema.campaignPuAssignments.status,
      notes: schema.campaignPuAssignments.notes,
    })
    .from(schema.campaignPuAssignments)
    .innerJoin(schema.pollingUnits, eq(schema.campaignPuAssignments.puCode, schema.pollingUnits.code))
    .where(eq(schema.campaignPuAssignments.profileId, profileId))
    .orderBy(schema.pollingUnits.name);
}

type PollingUnitWriteInput = {
  id?: number;
  profileId: number;
  puCode?: string;
  name: string;
  ward?: string;
  latitude?: number;
  longitude?: number;
  registeredVoters?: number;
  agentName?: string;
  agentPhone?: string;
  status?: string;
};

// Shared by single-row upsert and bulk import. Runs inside whatever
// transaction/connection the caller supplies.
async function writePollingUnitAssignment(executor: QueryExecutor, data: PollingUnitWriteInput) {
  const code = data.puCode || data.name.toLowerCase().replace(/[^a-z0-9]+/g, "-").slice(0, 50);

  // SECURITY: `polling_units` is the SHARED, Go-owned national registry. A
  // campaign write path must NEVER mutate existing registry rows (that would
  // let any user overwrite the canonical name/voter-count/coordinates of any
  // PU nationwide). We only insert when the PU code is genuinely absent.
  await executor
    .insert(schema.pollingUnits)
    .values({
      code,
      name: data.name,
      wardCode: data.ward || "unassigned",
      registeredVoters: data.registeredVoters ?? 0,
      latitude: data.latitude,
      longitude: data.longitude,
    })
    .onConflictDoNothing({ target: schema.pollingUnits.code });

  const rows = await executor
    .insert(schema.campaignPuAssignments)
    .values({
      profileId: data.profileId,
      puCode: code,
      agentName: data.agentName,
      agentPhone: data.agentPhone,
      status: data.status,
    })
    .onConflictDoUpdate({
      target: [schema.campaignPuAssignments.profileId, schema.campaignPuAssignments.puCode],
      set: {
        agentName: data.agentName,
        agentPhone: data.agentPhone,
        status: data.status,
      },
    })
    .returning();

  return rows[0];
}

export async function upsertPollingUnit(data: PollingUnitWriteInput) {
  const db = getDb();
  if (!db) return null;

  // The registry insert and the per-campaign assignment are one logical write:
  // keep them atomic so a partial failure cannot leave an assignment pointing
  // at a PU row that was never created.
  return db.transaction(async (tx) => writePollingUnitAssignment(tx, data));
}

export async function bulkUpsertPollingUnits(
  profileId: number,
  rows: Array<Omit<PollingUnitWriteInput, "id" | "profileId">>,
) {
  const db = getDb();
  if (!db) return { upserted: 0 };
  assertBulkImportSize(rows, "polling-unit bulk import");
  let upserted = 0;
  for (let i = 0; i < rows.length; i += BULK_INSERT_CHUNK_SIZE) {
    const chunk = rows
      .slice(i, i + BULK_INSERT_CHUNK_SIZE)
      .filter(r => typeof r?.name === "string" && r.name.trim().length > 0)
      .map(r => ({ ...r, profileId }));
    if (chunk.length === 0) continue;
    // Each chunk commits atomically; a failure rolls back only that chunk.
    await db.transaction(async (tx) => {
      for (const row of chunk) {
        await writePollingUnitAssignment(tx, row);
        upserted++;
      }
    });
  }
  return { upserted };
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
  if (data.id) {
    // SECURITY: tenant-guarded update — the router accepts `id` for edits, so
    // an id must UPDATE (previously it was silently ignored and a duplicate
    // row was inserted). Never set profileId on update.
    const { id, profileId, ...rest } = data;
    const rows = await db.update(schema.pressReleases)
      .set(rest)
      .where(and(eq(schema.pressReleases.id, id), eq(schema.pressReleases.profileId, requireTenantId(data.profileId))))
      .returning();
    return assertUpdated(rows, "Press release");
  }
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
  if (data.id) {
    // SECURITY: tenant-guarded update — see savePressRelease. Never set
    // profileId on update.
    const { id, profileId, ...rest } = data;
    const rows = await db.update(schema.socialMediaPosts)
      .set(rest)
      .where(and(eq(schema.socialMediaPosts.id, id), eq(schema.socialMediaPosts.profileId, requireTenantId(data.profileId))))
      .returning();
    return assertUpdated(rows, "Social media post");
  }
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
    // SECURITY: tenant-guarded update — never set profileId on update.
    const { id, profileId, ...rest } = data;
    const rows = await db.update(schema.complianceItems)
      .set({ ...rest, updatedAt: new Date() })
      .where(and(eq(schema.complianceItems.id, id), eq(schema.complianceItems.profileId, requireTenantId(data.profileId))))
      .returning();
    return assertUpdated(rows, "Compliance item");
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
    // SECURITY: tenant-guarded update — never set profileId on update.
    const { id, profileId, ...rest } = data;
    const rows = await db.update(schema.oppositionResearch)
      .set({ ...rest, updatedAt: new Date() })
      .where(and(eq(schema.oppositionResearch.id, id), eq(schema.oppositionResearch.profileId, requireTenantId(data.profileId))))
      .returning();
    return assertUpdated(rows, "Opposition entry");
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
    // SECURITY: tenant-guarded update — never set profileId on update.
    const { id, profileId, ...rest } = data;
    const rows = await db.update(schema.fieldAgents)
      .set({ ...rest, lastCheckin: new Date() })
      .where(and(eq(schema.fieldAgents.id, id), eq(schema.fieldAgents.profileId, requireTenantId(data.profileId))))
      .returning();
    return assertUpdated(rows, "Field agent");
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
    // SECURITY: tenant-guarded update — never set profileId on update.
    const { id, profileId, ...rest } = data;
    const rows = await db.update(schema.manifestoSections)
      .set({ ...rest, updatedAt: new Date() })
      .where(and(eq(schema.manifestoSections.id, id), eq(schema.manifestoSections.profileId, requireTenantId(data.profileId))))
      .returning();
    return assertUpdated(rows, "Manifesto section");
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
    // SECURITY: tenant-guarded update — never set profileId on update.
    const { id, profileId, ...rest } = data;
    const rows = await db.update(schema.budgetItems)
      .set(rest)
      .where(and(eq(schema.budgetItems.id, id), eq(schema.budgetItems.profileId, requireTenantId(data.profileId))))
      .returning();
    return assertUpdated(rows, "Budget item");
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
    // SECURITY: tenant-guarded update — never set profileId on update.
    const { id, profileId, ...rest } = data;
    const rows = await db.update(schema.debatePrepNotes)
      .set({ ...rest, updatedAt: new Date() })
      .where(and(eq(schema.debatePrepNotes.id, id), eq(schema.debatePrepNotes.profileId, requireTenantId(data.profileId))))
      .returning();
    return assertUpdated(rows, "Debate prep note");
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
  const db = await getDb();
  if (!db) return null;
  // Find existing profile for this user
  const rows = await db
    .select()
    .from(schema.candidateProfiles)
    .where(and(eq(schema.candidateProfiles.userId, userId), eq(schema.candidateProfiles.isActive, true)))
    .limit(1);
  if (rows.length > 0) return rows[0];
  // Create an empty, user-owned profile. Real campaign, party, jurisdiction, and office
  // details must be supplied explicitly through the profile workflow.
  const owners = await db
    .select({ fullName: schema.users.fullName })
    .from(schema.users)
    .where(eq(schema.users.id, userId))
    .limit(1);
  const inserted = await db
    .insert(schema.candidateProfiles)
    .values({
      candidateName: owners[0]?.fullName?.trim() || "Unconfigured campaign profile",
      partyName: null,
      partyColor: "#006400",
      stateCode: null,
      stateName: null,
      office: null,
      geopoliticalZone: null,
      isActive: true,
      isSeeded: false,
      userId,
    })
    // Closes the get-then-create race: candidate_profiles.user_id has a unique
    // index, so a concurrent creator for the same user conflicts here and we
    // just touch updated_at and return the existing row instead of crashing.
    .onConflictDoUpdate({
      target: schema.candidateProfiles.userId,
      targetWhere: sql`${schema.candidateProfiles.userId} IS NOT NULL`,
      set: { updatedAt: new Date() },
    })
    .returning();
  return inserted[0];
}

export function isFixtureSeedingAllowed(): boolean {
  const runtime = (process.env.NODE_ENV || "").toLowerCase();
  const explicitFixtureFlag = process.env.CAMPAIGN_ALLOW_FIXTURE_SEED === "true";
  const nonProductionRuntime = runtime === "test" || runtime === "development" || process.env.GITHUB_ACTIONS === "true";
  return explicitFixtureFlag && nonProductionRuntime;
}

// Fixture data is reserved for explicitly enabled non-production test environments.
export async function seedProfileData(profileId: number): Promise<void> {
  if (!isFixtureSeedingAllowed()) {
    throw new Error("Campaign fixture seeding is disabled outside an explicitly enabled non-production environment");
  }
  const db = await getDb();
  if (!db) return;
  const { eq } = await import("drizzle-orm");
  const {
    timelineEvents, voterRegistrations, volunteers, volunteerTasks,
    pressReleases, socialMediaPosts, complianceItems, oppositionResearch,
    warRoomIncidents, electionResults, manifestoSections, petitions,
    diasporaContacts, endorsements, fundraisingTransactions, budgetItems,
    mediaItems, debatePrepNotes, debatePracticeScores, stakeholderContacts,
    fieldAgents, pollingUnits, campaignPuAssignments, candidateProfiles,
  } = await import("../drizzle/schema");

  // Mark as seeded first to prevent duplicate seeding
  await db.update(candidateProfiles).set({ isSeeded: true }).where(eq(candidateProfiles.id, profileId));

  const daysFromNow = (n: number) => { const d = new Date(); d.setDate(d.getDate() + n); return d; };

  // Clear existing data. All deletes run in ONE transaction so a mid-seed
  // failure cannot leave the profile with half its data wiped.
  await db.transaction(async (tx) => {
    await tx.delete(timelineEvents).where(eq(timelineEvents.profileId, profileId));
    await tx.delete(voterRegistrations).where(eq(voterRegistrations.profileId, profileId));
    await tx.delete(volunteers).where(eq(volunteers.profileId, profileId));
    await tx.delete(volunteerTasks).where(eq(volunteerTasks.profileId, profileId));
    await tx.delete(pressReleases).where(eq(pressReleases.profileId, profileId));
    await tx.delete(socialMediaPosts).where(eq(socialMediaPosts.profileId, profileId));
    await tx.delete(complianceItems).where(eq(complianceItems.profileId, profileId));
    await tx.delete(oppositionResearch).where(eq(oppositionResearch.profileId, profileId));
    await tx.delete(warRoomIncidents).where(eq(warRoomIncidents.profileId, profileId));
    await tx.delete(electionResults).where(eq(electionResults.profileId, profileId));
    await tx.delete(manifestoSections).where(eq(manifestoSections.profileId, profileId));
    await tx.delete(diasporaContacts).where(eq(diasporaContacts.profileId, profileId));
    await tx.delete(endorsements).where(eq(endorsements.profileId, profileId));
    await tx.delete(fundraisingTransactions).where(eq(fundraisingTransactions.profileId, profileId));
    await tx.delete(budgetItems).where(eq(budgetItems.profileId, profileId));
    await tx.delete(mediaItems).where(eq(mediaItems.profileId, profileId));
    await tx.delete(debatePrepNotes).where(eq(debatePrepNotes.profileId, profileId));
    await tx.delete(debatePracticeScores).where(eq(debatePracticeScores.profileId, profileId));
    await tx.delete(stakeholderContacts).where(eq(stakeholderContacts.profileId, profileId));
    await tx.delete(fieldAgents).where(eq(fieldAgents.profileId, profileId));
    // Don't touch `pollingUnits` — it's the shared, Go-owned national registry.
    await tx.delete(campaignPuAssignments).where(eq(campaignPuAssignments.profileId, profileId));
  });

  // ── Timeline Events ───────────────────────────────────────────────────────
  await db.insert(timelineEvents).values([
    { profileId, title: "Campaign Launch — Kano City", eventDate: daysFromNow(-90).toISOString().split("T")[0], category: "Rally", status: "completed" as const, location: "Sani Abacha Stadium, Kano", priority: "critical" as const },
    { profileId, title: "Ward-Level Mobilisation — Dala LGA", eventDate: daysFromNow(-75).toISOString().split("T")[0], category: "Canvassing", status: "completed" as const, location: "Dala LGA", priority: "high" as const },
    { profileId, title: "Women's Forum — Gwale", eventDate: daysFromNow(-60).toISOString().split("T")[0], category: "Outreach", status: "completed" as const, location: "Gwale Town Hall", priority: "high" as const },
    { profileId, title: "INEC Candidate Screening", eventDate: daysFromNow(-45).toISOString().split("T")[0], category: "INEC", status: "completed" as const, location: "INEC Kano HQ", priority: "critical" as const },
    { profileId, title: "Manifesto Launch — Bayero University", eventDate: daysFromNow(-30).toISOString().split("T")[0], category: "Media", status: "completed" as const, location: "BUK Auditorium", priority: "critical" as const },
    { profileId, title: "Northern Governors Endorsement Meeting", eventDate: daysFromNow(-20).toISOString().split("T")[0], category: "Stakeholder", status: "completed" as const, location: "Abuja", priority: "high" as const },
    { profileId, title: "TV Debate — Channels Television", eventDate: daysFromNow(-14).toISOString().split("T")[0], category: "Media", status: "completed" as const, location: "Channels TV Studio, Lagos", priority: "critical" as const },
    { profileId, title: "Youth Rally — Nassarawa LGA", eventDate: daysFromNow(-7).toISOString().split("T")[0], category: "Rally", status: "completed" as const, location: "Nassarawa Stadium", priority: "high" as const },
    { profileId, title: "Final Mega Rally — Sani Abacha Stadium", eventDate: daysFromNow(-2).toISOString().split("T")[0], category: "Rally", status: "active" as const, location: "Sani Abacha Stadium, Kano", priority: "critical" as const },
    { profileId, title: "INEC Accreditation Deadline", eventDate: daysFromNow(3).toISOString().split("T")[0], category: "INEC", status: "pending" as const, location: "INEC State Office", priority: "critical" as const },
    { profileId, title: "Election Day — Governorship Election", eventDate: daysFromNow(14).toISOString().split("T")[0], category: "Election", status: "pending" as const, location: "All 44 LGAs", priority: "critical" as const },
    { profileId, title: "INEC Result Collation — State HQ", eventDate: daysFromNow(15).toISOString().split("T")[0], category: "INEC", status: "pending" as const, location: "INEC Kano HQ", priority: "critical" as const },
    { profileId, title: "Victory Press Conference", eventDate: daysFromNow(16).toISOString().split("T")[0], category: "Media", status: "pending" as const, location: "Campaign HQ", priority: "high" as const },
  ]);

  // ── Voter Registrations ───────────────────────────────────────────────────
  const voters = [
    { fullName: "Aminu Suleiman Kano", vin: "19KN0001234567", lga: "Dala", ward: "Dala Central", pollingUnit: "DALA PRIMARY SCHOOL", phone: "08031234567", isVerified: true },
    { fullName: "Fatima Yusuf Ibrahim", vin: "19KN0002345678", lga: "Gwale", ward: "Gwale North", pollingUnit: "GWALE MODEL PRIMARY", phone: "08052345678", isVerified: true },
    { fullName: "Musa Abdullahi Wada", vin: "19KN0003456789", lga: "Nassarawa", ward: "Nassarawa East", pollingUnit: "NASSARAWA SEC SCHOOL", phone: "08073456789", isVerified: false },
    { fullName: "Hauwa Garba Sani", vin: "19KN0004567890", lga: "Kumbotso", ward: "Kumbotso Central", pollingUnit: "KUMBOTSO PRIMARY", phone: "08094567890", isVerified: true },
    { fullName: "Ibrahim Musa Tukur", vin: "19KN0005678901", lga: "Tarauni", ward: "Tarauni South", pollingUnit: "TARAUNI TOWN HALL", phone: "08015678901", isVerified: false },
    { fullName: "Zainab Umar Shehu", vin: "19KN0006789012", lga: "Fagge", ward: "Fagge D2", pollingUnit: "FAGGE PRIMARY SCHOOL", phone: "08036789012", isVerified: true },
    { fullName: "Kabiru Aliyu Danmusa", vin: "19KN0007890123", lga: "Municipal", ward: "Kano Municipal A", pollingUnit: "MUNICIPAL GOVT SCHOOL", phone: "08057890123", isVerified: true },
    { fullName: "Rabi Usman Bello", vin: "19KN0008901234", lga: "Ungogo", ward: "Ungogo North", pollingUnit: "UNGOGO CENTRAL SCHOOL", phone: "08078901234", isVerified: false },
  ];
  await db.insert(voterRegistrations).values(voters.map(v => ({ ...v, profileId, stateCode: "KN" })));

  // ── Volunteers ────────────────────────────────────────────────────────────
  const volData = [
    { fullName: "Bashir Abdulkadir", phone: "08031111111", lga: "Dala", role: "Ward Coordinator", skills: "Community mobilisation, data entry", status: "active" as const },
    { fullName: "Khadija Musa Sani", phone: "08052222222", lga: "Gwale", role: "Women Leader", skills: "Voter registration, outreach", status: "active" as const },
    { fullName: "Umar Faruk Dankabo", phone: "08073333333", lga: "Nassarawa", role: "Youth Coordinator", skills: "Social media, logistics", status: "active" as const },
    { fullName: "Aisha Bello Kano", phone: "08094444444", lga: "Kumbotso", role: "Polling Agent", skills: "Election monitoring, BVAS operation", status: "active" as const },
    { fullName: "Sani Ibrahim Wada", phone: "08015555555", lga: "Tarauni", role: "Transport Coordinator", skills: "Logistics, vehicle management", status: "active" as const },
    { fullName: "Maryam Garba Tukur", phone: "08036666666", lga: "Fagge", role: "Media Liaison", skills: "Photography, social media", status: "active" as const },
  ];
  const insertedVols = await db.insert(volunteers).values(volData.map(v => ({ ...v, profileId }))).returning();
  if (insertedVols.length >= 2) {
    await db.insert(volunteerTasks).values([
      { profileId, volunteerId: insertedVols[0].id, title: "Register 200 voters in Dala ward", taskType: "canvassing" as const, status: "completed" as const, dueDate: daysFromNow(-7) },
      { profileId, volunteerId: insertedVols[1].id, title: "Organise women's rally — Gwale", taskType: "canvassing" as const, status: "in_progress" as const, dueDate: daysFromNow(2) },
      { profileId, volunteerId: insertedVols[2].id, title: "Social media content for final week", taskType: "media" as const, status: "in_progress" as const, dueDate: daysFromNow(1) },
      { profileId, volunteerId: insertedVols[3].id, title: "BVAS training — Kumbotso PUs", taskType: "polling_unit" as const, status: "pending" as const, dueDate: daysFromNow(5) },
      { profileId, volunteerId: insertedVols[4].id, title: "Arrange 10 buses for Election Day", taskType: "logistics" as const, status: "pending" as const, dueDate: daysFromNow(12) },
      { profileId, volunteerId: insertedVols[5].id, title: "Prepare press kits for final rally", taskType: "media" as const, status: "completed" as const, dueDate: daysFromNow(-2) },
    ]);
  }

  // ── Press Releases ────────────────────────────────────────────────────────
  await db.insert(pressReleases).values([
    { profileId, title: "Candidate Unveils 10-Point Economic Agenda for Kano", body: "The governorship candidate today unveiled a comprehensive 10-point economic agenda aimed at creating 500,000 jobs in Kano State within four years. The plan focuses on agro-processing, technology, and small business development.", status: "completed" as const, publishedAt: daysFromNow(-30) },
    { profileId, title: "Campaign Condemns Electoral Violence in Kumbotso", body: "The campaign strongly condemns the reported incidents of electoral violence in Kumbotso LGA and calls on security agencies to ensure a peaceful election environment for all citizens.", status: "completed" as const, publishedAt: daysFromNow(-7) },
    { profileId, title: "Final Rally Set for Sani Abacha Stadium", body: "The campaign announces the final mega rally scheduled for Sani Abacha Stadium. Thousands of supporters from all 44 LGAs are expected to attend.", status: "pending" as const },
  ]);

  // ── Social Media Posts ────────────────────────────────────────────────────
  await db.insert(socialMediaPosts).values([
    { profileId, platform: "Twitter", content: "Our candidate is committed to creating 500,000 jobs in Kano State. Vote for a better Kano! #KanoDecides #OurCandidate", scheduledAt: daysFromNow(-14), status: "completed" as const },
    { profileId, platform: "Facebook", content: "Join us at the final mega rally at Sani Abacha Stadium! Bring your family and friends. Together we will win! #FinalRally", scheduledAt: daysFromNow(-2), status: "completed" as const },
    { profileId, platform: "WhatsApp", content: "Reminder: Election Day is in 14 days. Make sure your PVC is ready. Share this with your contacts!", scheduledAt: daysFromNow(1), status: "pending" as const },
    { profileId, platform: "Instagram", content: "Behind the scenes at campaign HQ — our team working tirelessly for Kano's future. #TeamWork #KanoFirst", scheduledAt: daysFromNow(3), status: "pending" as const },
    { profileId, platform: "Twitter", content: "Thank you Nassarawa LGA for the incredible turnout at yesterday's rally! Your energy fuels our campaign. #Nassarawa", scheduledAt: daysFromNow(-7), status: "completed" as const },
  ]);

  // ── Compliance Items ──────────────────────────────────────────────────────
  const toDateStr = (d: Date) => d.toISOString().split("T")[0];
  await db.insert(complianceItems).values([
    { profileId, title: "Campaign Finance Report — Q3", description: "Submit quarterly campaign finance report to INEC as required by Electoral Act 2022 Section 87", category: "Finance", status: "compliant" as const, deadline: toDateStr(daysFromNow(-10)) },
    { profileId, title: "Candidate Affidavit Submission", description: "Submit sworn affidavit of personal particulars to INEC", category: "Legal", status: "compliant" as const, deadline: toDateStr(daysFromNow(-45)) },
    { profileId, title: "Campaign Finance Report — Q4 (Final)", description: "Submit final campaign finance report within 6 months of election", category: "Finance", status: "pending" as const, deadline: toDateStr(daysFromNow(180)) },
    { profileId, title: "INEC Polling Agent Accreditation", description: "Submit list of polling agents to INEC at least 7 days before election", category: "Electoral", status: "warning" as const, deadline: toDateStr(daysFromNow(7)) },
    { profileId, title: "Campaign Spending Cap Compliance", description: "Ensure total campaign spending does not exceed ₦1B as per INEC guidelines", category: "Finance", status: "warning" as const, deadline: toDateStr(daysFromNow(14)) },
    { profileId, title: "Party Nomination Form CF001", description: "File nomination form with party secretariat", category: "Party", status: "compliant" as const, deadline: toDateStr(daysFromNow(-60)) },
  ]);

  // ── Opposition Research ───────────────────────────────────────────────────
  await db.insert(oppositionResearch).values([
    { profileId, opponentName: "Alhaji Kabiru Rufa'i", party: "APC", strength: "Incumbent advantage, APC federal backing, strong North-West network", weakness: "Failed to address banditry, poor education record, corruption allegations", keyIssues: ["Security", "Education", "Corruption"], threatLevel: "high" as const, notes: "Primary opponent. Focus on security failures and UBEC fund mismanagement." },
    { profileId, opponentName: "Dr. Amina Bello", party: "LP", strength: "Youth appeal, social media presence, anti-establishment narrative", weakness: "No governance experience, LP never governed a state, limited funding", keyIssues: ["Youth unemployment", "Economic reform"], threatLevel: "medium" as const, notes: "Labour Party candidate. Targets same youth demographic. Monitor closely." },
    { profileId, opponentName: "Alhaji Musa Kwankwaso", party: "NNPP", strength: "Strong Kwankwasiyya movement, grassroots network, former governor", weakness: "Defection history raises loyalty concerns, limited diaspora support", keyIssues: ["Infrastructure", "Education"], threatLevel: "high" as const, notes: "NNPP candidate with deep Kano roots. Most dangerous opponent in rural LGAs." },
  ]);

  // ── War Room Incidents ────────────────────────────────────────────────────
  await db.insert(warRoomIncidents).values([
    { profileId, reportedBy: "Agent Musa Dala", lga: "Dala", ward: "Dala Central", incidentType: "Ballot Stuffing Attempt", description: "Suspected ballot stuffing at Dala Primary School PU. 3 individuals apprehended by security.", severity: "high" as const, status: "resolved" as const, reportedAt: daysFromNow(-7) },
    { profileId, reportedBy: "Agent Fatima Gwale", lga: "Gwale", ward: "Gwale North", incidentType: "Voter Intimidation", description: "Armed individuals seen near polling unit intimidating voters. Police notified.", severity: "critical" as const, status: "escalated" as const, reportedAt: daysFromNow(-3) },
    { profileId, reportedBy: "Agent Umar Nassarawa", lga: "Nassarawa", ward: "Nassarawa East", incidentType: "BVAS Malfunction", description: "BVAS device at Nassarawa Secondary School PU not functioning. INEC technician requested.", severity: "medium" as const, status: "open" as const, reportedAt: daysFromNow(-1) },
    { profileId, reportedBy: "HQ Observer", lga: "Kumbotso", ward: "Kumbotso Central", incidentType: "Late Materials Arrival", description: "Ballot papers arrived 2 hours late at Kumbotso Primary School. Voting delayed.", severity: "medium" as const, status: "resolved" as const, reportedAt: daysFromNow(-14) },
  ]);

  // ── Election Results ──────────────────────────────────────────────────────
  const lgas = ["Dala", "Gwale", "Nassarawa", "Kumbotso", "Tarauni", "Fagge", "Municipal", "Ungogo", "Kura", "Bebeji"];
  const resultData: any[] = [];
  lgas.forEach(lga => {
    const base = Math.floor(Math.random() * 5000) + 8000;
    resultData.push({ profileId, lga, candidateName: "Our Candidate", party: "PDP", votes: base + Math.floor(Math.random() * 3000), isProjected: true });
    resultData.push({ profileId, lga, candidateName: "Alhaji Kabiru Rufa'i", party: "APC", votes: base - Math.floor(Math.random() * 2000), isProjected: true });
    resultData.push({ profileId, lga, candidateName: "Dr. Amina Bello", party: "LP", votes: Math.floor(Math.random() * 2000) + 500, isProjected: true });
  });
  await db.insert(electionResults).values(resultData);

  // ── Manifesto Sections ────────────────────────────────────────────────────
  await db.insert(manifestoSections).values([
    { profileId, sectionTitle: "Economic Development & Job Creation", summary: "Create 500,000 jobs through agro-processing, technology hubs, and SME support", commitments: ["Establish 5 industrial parks", "₦50B SME fund", "Tech hub in Kano City"], timeline: "Year 1-2", budget: "₦120B", priority: "critical" as const, sortOrder: 1 },
    { profileId, sectionTitle: "Education Reform", summary: "Rebuild 2,000 schools and provide free education from primary to JSS3", commitments: ["Free education JSS1-3", "2,000 school renovations", "10,000 teacher recruitment"], timeline: "Year 1-4", budget: "₦80B", priority: "critical" as const, sortOrder: 2 },
    { profileId, sectionTitle: "Healthcare Transformation", summary: "Build 100 primary health centres and upgrade 5 general hospitals", commitments: ["100 new PHCs", "Free maternal care", "Medical equipment upgrade"], timeline: "Year 1-3", budget: "₦60B", priority: "high" as const, sortOrder: 3 },
    { profileId, sectionTitle: "Security & Rule of Law", summary: "Strengthen security architecture and community policing", commitments: ["1,000 community police recruits", "CCTV in major cities", "Security trust fund"], timeline: "Year 1", budget: "₦40B", priority: "critical" as const, sortOrder: 4 },
    { profileId, sectionTitle: "Agriculture & Food Security", summary: "Modernise Kano's agricultural sector and reduce food prices", commitments: ["Irrigation expansion", "Fertiliser subsidy", "Commodity exchange"], timeline: "Year 1-2", budget: "₦50B", priority: "high" as const, sortOrder: 5 },
  ]);

  // ── Petitions ─────────────────────────────────────────────────────────────
  const { petitionSignatures } = await import("../drizzle/schema");
  const [petition] = await db.insert(petitions).values([
    { profileId, title: "Support Free Education in Kano State", description: "We call on the next governor of Kano State to implement free education from primary to JSS3 level for all Kano children.", targetSignatures: 50000, status: "active" as const },
  ]).returning();
  if (petition) {
    await db.insert(petitionSignatures).values([
      { petitionId: petition.id, signerName: "Aminu Suleiman", lga: "Dala" },
      { petitionId: petition.id, signerName: "Fatima Ibrahim", lga: "Gwale" },
      { petitionId: petition.id, signerName: "Musa Wada", lga: "Nassarawa" },
    ]);
  }

  // ── Diaspora Contacts ─────────────────────────────────────────────────────
  await db.insert(diasporaContacts).values([
    { profileId, name: "Dr. Usman Kano", country: "United Kingdom", city: "London", phone: "+447911123456", email: "usman.kano@gmail.com", organization: "Kano UK Association", status: "active" as const },
    { profileId, name: "Hajiya Maryam Sule", country: "United States", city: "Houston", phone: "+17135551234", email: "maryam.sule@yahoo.com", organization: "Kano-Texas Community", status: "active" as const },
    { profileId, name: "Alhaji Bello Dantata", country: "Saudi Arabia", city: "Jeddah", phone: "+966501234567", organization: "Nigerian Muslim Community Jeddah", status: "active" as const },
    { profileId, name: "Prof. Amina Garba", country: "Canada", city: "Toronto", phone: "+14165551234", email: "amina.garba@utoronto.ca", organization: "Kano Professionals Canada", status: "active" as const },
  ]);

  // ── Endorsements ──────────────────────────────────────────────────────────
  await db.insert(endorsements).values([
    { profileId, endorserName: "Alhaji Aminu Dantata", title: "Business Mogul", organization: "Dantata Group", category: "Business", statement: "I endorse this candidate because of his commitment to economic development and job creation for Kano youth.", endorsedAt: daysFromNow(-30) },
    { profileId, endorserName: "Dr. Fatima Aliyu", title: "NMA Kano Chairman", organization: "Nigerian Medical Association", category: "Professional", statement: "As a healthcare professional, I support this candidate's plan to build 100 PHCs and provide free maternal care.", endorsedAt: daysFromNow(-20) },
    { profileId, endorserName: "Comrade Usman Bello", title: "NLC Kano Chairman", organization: "NLC Kano", category: "Labour", statement: "The NLC Kano endorses this candidate for his pro-worker policies and commitment to minimum wage enforcement.", endorsedAt: daysFromNow(-15) },
    { profileId, endorserName: "Hajiya Zainab Umar", title: "KMWA President", organization: "Kano Market Women Association", category: "Civil Society", statement: "Market women across Kano support this candidate because he understands our economic challenges.", endorsedAt: daysFromNow(-10) },
  ]);

  // ── Fundraising ───────────────────────────────────────────────────────────
  await db.insert(fundraisingTransactions).values([
    { profileId, donorName: "Alhaji Aminu Dantata", amount: 50000000, currency: "NGN", category: "Individual", source: "Bank Transfer", transactedAt: daysFromNow(-60), isVerified: true },
    { profileId, donorName: "Kano Business Forum", amount: 25000000, currency: "NGN", category: "Corporate", source: "Bank Transfer", transactedAt: daysFromNow(-45), isVerified: true },
    { profileId, donorName: "UK Kano Association", amount: 15000000, currency: "NGN", category: "Diaspora", source: "Wire Transfer", transactedAt: daysFromNow(-30), isVerified: true },
    { profileId, donorName: "Hajiya Zainab Umar", amount: 5000000, currency: "NGN", category: "Individual", source: "Cash", transactedAt: daysFromNow(-20), isVerified: true },
    { profileId, donorName: "Kano Traders Union", amount: 10000000, currency: "NGN", category: "Corporate", source: "Bank Transfer", transactedAt: daysFromNow(-15), isVerified: true },
    { profileId, donorName: "Anonymous Supporter", amount: 2000000, currency: "NGN", category: "Individual", source: "Cash", transactedAt: daysFromNow(-7), isVerified: false },
  ]);

  // ── Budget Items ──────────────────────────────────────────────────────────
  await db.insert(budgetItems).values([
    { profileId, category: "Rallies & Events", description: "Venue hire, sound systems, logistics for 20 major rallies", budgetedAmount: 80000000, spentAmount: 72000000 },
    { profileId, category: "Media & Advertising", description: "TV, radio, newspaper, and digital advertising", budgetedAmount: 50000000, spentAmount: 45000000 },
    { profileId, category: "Volunteer & Staff", description: "Salaries, allowances, and training for 500 volunteers", budgetedAmount: 30000000, spentAmount: 28000000 },
    { profileId, category: "Printing & Materials", description: "Posters, flyers, branded materials, T-shirts", budgetedAmount: 20000000, spentAmount: 18500000 },
    { profileId, category: "Transportation", description: "Vehicles, fuel, and logistics for campaign team", budgetedAmount: 25000000, spentAmount: 22000000 },
    { profileId, category: "Legal & Compliance", description: "Legal fees, INEC filings, compliance costs", budgetedAmount: 10000000, spentAmount: 8000000 },
    { profileId, category: "Technology", description: "Campaign management software, website, social media tools", budgetedAmount: 5000000, spentAmount: 4200000 },
  ]);

  // ── Media Items ───────────────────────────────────────────────────────────
  await db.insert(mediaItems).values([
    { profileId, headline: "Candidate Promises 500,000 Jobs in Kano", source: "Daily Trust", sourceType: "print" as const, sentiment: "positive" as const, reach: 250000, zone: "North-West", publishedAt: daysFromNow(-30) },
    { profileId, headline: "Kano Governorship Race Heats Up", source: "Channels TV", sourceType: "broadcast" as const, sentiment: "neutral" as const, reach: 2000000, zone: "National", publishedAt: daysFromNow(-20) },
    { profileId, headline: "Opposition Questions Campaign Finance", source: "Punch", sourceType: "print" as const, sentiment: "negative" as const, reach: 500000, zone: "National", publishedAt: daysFromNow(-15) },
    { profileId, headline: "Candidate Receives NMA Endorsement", source: "Vanguard", sourceType: "online" as const, sentiment: "positive" as const, reach: 800000, zone: "National", publishedAt: daysFromNow(-10) },
    { profileId, headline: "Final Rally Draws Record Crowd", source: "Arewa FM", sourceType: "broadcast" as const, sentiment: "positive" as const, reach: 1500000, zone: "North-West", publishedAt: daysFromNow(-2) },
  ]);

  // ── Debate Practice Scores ────────────────────────────────────────────────
  await db.insert(debatePracticeScores).values([
    { profileId, topic: "Security & Banditry", score: 7, maxScore: 10, notes: "Good on community policing, needs stronger data", scoredAt: daysFromNow(-45) },
    { profileId, topic: "Education", score: 9, maxScore: 10, notes: "Excellent delivery, compelling statistics", scoredAt: daysFromNow(-40) },
    { profileId, topic: "Economy & Jobs", score: 6, maxScore: 10, notes: "Needs more specific job creation metrics", scoredAt: daysFromNow(-35) },
    { profileId, topic: "Healthcare", score: 8, maxScore: 10, notes: "Strong on PHC numbers, improve on specialist care", scoredAt: daysFromNow(-30) },
    { profileId, topic: "Security & Banditry", score: 8, maxScore: 10, notes: "Improved significantly after coaching", scoredAt: daysFromNow(-20) },
    { profileId, topic: "Economy & Jobs", score: 8, maxScore: 10, notes: "Much better with tech hub specifics", scoredAt: daysFromNow(-15) },
    { profileId, topic: "Education", score: 9, maxScore: 10, notes: "Consistent high performance", scoredAt: daysFromNow(-10) },
    { profileId, topic: "Healthcare", score: 9, maxScore: 10, notes: "Best performance yet", scoredAt: daysFromNow(-5) },
  ]);

  // ── Stakeholder Contacts ──────────────────────────────────────────────────
  await db.insert(stakeholderContacts).values([
    { profileId, name: "Alhaji Aminu Dantata", title: "Business Mogul", organization: "Dantata Group", category: "Business", phone: "08031234567", state: "Kano", lga: "Municipal", influenceLevel: "critical" as const, relationship: "supporter", nextAction: "Confirm attendance at final rally" },
    { profileId, name: "Emir of Kano", title: "His Royal Highness", organization: "Kano Emirate", category: "Traditional", state: "Kano", influenceLevel: "critical" as const, relationship: "neutral", nextAction: "Request audience before election day" },
    { profileId, name: "Dr. Fatima Aliyu", title: "Chairman, NMA Kano", organization: "Nigerian Medical Association", category: "Professional", phone: "08052345678", email: "fatima.a@nma.org", state: "Kano", influenceLevel: "high" as const, relationship: "supporter" },
    { profileId, name: "Comrade Usman Bello", title: "NLC Kano Chairman", organization: "NLC Kano", category: "Labour", phone: "08073456789", state: "Kano", influenceLevel: "high" as const, relationship: "supporter" },
    { profileId, name: "Hajiya Zainab Umar", title: "President, KMWA", organization: "Kano Market Women Association", category: "Civil Society", phone: "08094567890", state: "Kano", influenceLevel: "high" as const, relationship: "supporter" },
    { profileId, name: "Bishop Emmanuel Okafor", title: "Bishop", organization: "Catholic Diocese of Kano", category: "Religious", phone: "08015678901", state: "Kano", influenceLevel: "medium" as const, relationship: "neutral", nextAction: "Invite to interfaith dialogue" },
    { profileId, name: "Prof. Abdullahi Usman", title: "Vice Chancellor", organization: "Bayero University Kano", category: "Academia", phone: "08036789012", email: "vc@buk.edu.ng", state: "Kano", influenceLevel: "medium" as const, relationship: "neutral" },
  ]);

  // ── Field Agents ──────────────────────────────────────────────────────────
  await db.insert(fieldAgents).values([
    { profileId, name: "Musa Dala", phone: "08031111111", assignedPu: "DALA PRIMARY SCHOOL", lga: "Dala", agentStatus: "active" as const, votersCounted: 342 },
    { profileId, name: "Fatima Gwale", phone: "08052222222", assignedPu: "GWALE MODEL PRIMARY", lga: "Gwale", agentStatus: "active" as const, votersCounted: 289 },
    { profileId, name: "Umar Nassarawa", phone: "08073333333", assignedPu: "NASSARAWA SEC SCHOOL", lga: "Nassarawa", agentStatus: "sos" as const, votersCounted: 156 },
    { profileId, name: "Aisha Kumbotso", phone: "08094444444", assignedPu: "KUMBOTSO PRIMARY", lga: "Kumbotso", agentStatus: "active" as const, votersCounted: 412 },
  ]);

  // ── Polling Units ─────────────────────────────────────────────────────────
  // Canonical PU rows are upserted (shared, Go-owned registry); the per-
  // campaign operational fields (agent, status) go in campaignPuAssignments.
  const demoPUs = [
    { puCode: "KN/01/01/001", name: "DALA PRIMARY SCHOOL", ward: "Dala Central", lat: 12.0022, lng: 8.5919, registeredVoters: 842, agentName: "Musa Dala", agentPhone: "08031111111" },
    { puCode: "KN/02/01/001", name: "GWALE MODEL PRIMARY SCHOOL", ward: "Gwale North", lat: 11.9980, lng: 8.5150, registeredVoters: 654, agentName: "Fatima Gwale", agentPhone: "08052222222" },
    { puCode: "KN/03/01/001", name: "NASSARAWA SECONDARY SCHOOL", ward: "Nassarawa East", lat: 12.0100, lng: 8.5300, registeredVoters: 1120, agentName: "Umar Nassarawa", agentPhone: "08073333333" },
    { puCode: "KN/04/01/001", name: "KUMBOTSO PRIMARY SCHOOL", ward: "Kumbotso Central", lat: 12.0500, lng: 8.4800, registeredVoters: 780, agentName: "Aisha Kumbotso", agentPhone: "08094444444" },
    { puCode: "KN/05/01/001", name: "TARAUNI TOWN HALL", ward: "Tarauni South", lat: 12.0200, lng: 8.5600, registeredVoters: 920 },
    { puCode: "KN/06/01/001", name: "FAGGE PRIMARY SCHOOL", ward: "Fagge D2", lat: 11.9900, lng: 8.5200, registeredVoters: 560 },
  ];
  for (const pu of demoPUs) {
    await db
      .insert(pollingUnits)
      .values({ code: pu.puCode, name: pu.name, wardCode: pu.ward, registeredVoters: pu.registeredVoters, latitude: pu.lat, longitude: pu.lng })
      .onConflictDoNothing({ target: pollingUnits.code });
    await db.insert(campaignPuAssignments).values({
      profileId, puCode: pu.puCode, agentName: pu.agentName, agentPhone: pu.agentPhone,
    });
  }

  // ── Debate Prep Notes ─────────────────────────────────────────────────────
  await db.insert(debatePrepNotes).values([
    { profileId, topic: "Economy & Jobs", keyMessage: "500,000 jobs through agro-processing and tech hubs", counterArguments: ["Job creation is federal responsibility", "Private sector drives employment"], statistics: ["Kano unemployment rate is 34%", "Agro-processing can create 200,000 jobs"], practiceScore: 7 },
    { profileId, topic: "Education", keyMessage: "Free education from primary to JSS3 for all Kano children", counterArguments: ["Education funding is federal", "Quality over quantity"], statistics: ["1.2M out-of-school children in Kano", "2,000 schools need renovation"], practiceScore: 9 },
    { profileId, topic: "Security & Banditry", keyMessage: "Community policing and intelligence sharing to defeat banditry", counterArguments: ["Security is federal responsibility", "Army handles banditry"], statistics: ["Banditry incidents up 40% under APC", "200 kidnappings in 2023"], practiceScore: 8 },
  ]);
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
// Explicit column list: invite_token is a bearer credential for joining the
// campaign and must never leave the db layer via a list endpoint (viewers can
// call team.list). Email masking for non-privileged viewers happens in the
// router, which knows the caller's role.
export async function getCampaignMembers(profileId: number) {
  const db = getDb();
  if (!db) return [];
  return db.select({
    id: schema.campaignMembers.id,
    profileId: schema.campaignMembers.profileId,
    userId: schema.campaignMembers.userId,
    name: schema.campaignMembers.name,
    email: schema.campaignMembers.email,
    role: schema.campaignMembers.role,
    invitedAt: schema.campaignMembers.invitedAt,
    acceptedAt: schema.campaignMembers.acceptedAt,
  })
    .from(schema.campaignMembers)
    .where(eq(schema.campaignMembers.profileId, profileId))
    .orderBy(schema.campaignMembers.invitedAt);
}

export async function inviteCampaignMember(input: {
  profileId: number; email: string; name: string; role: "manager" | "viewer"; origin?: string;
}) {
  const db = getDb();
  if (!db) throw new Error("DB not available");
  // Generate a cryptographically random invite token
  const { randomBytes } = await import("crypto");
  const inviteToken = randomBytes(32).toString("hex");
  const [row] = await db.insert(schema.campaignMembers).values({
    profileId: input.profileId,
    email: input.email,
    name: input.name,
    role: input.role,
    inviteToken,
  }).returning();
  return { ...row, inviteToken, inviteUrl: input.origin ? `${input.origin}/join?token=${inviteToken}` : null };
}

// SECURITY: invites are bearer tokens — they must expire. 7 days is long
// enough for a human to respond, short enough that a leaked old link is dead.
export const INVITE_TTL_MS = 7 * 24 * 60 * 60 * 1000;

function isInviteExpired(invitedAt: Date | string | null | undefined): boolean {
  if (!invitedAt) return false; // legacy rows without a timestamp stay usable
  return Date.now() - new Date(invitedAt).getTime() > INVITE_TTL_MS;
}

export async function acceptCampaignInvite(token: string, userId: number, userEmail?: string | null) {
  const db = getDb();
  if (!db) throw new Error("DB not available");
  const [member] = await db.select().from(schema.campaignMembers)
    .where(eq(schema.campaignMembers.inviteToken, token)).limit(1);
  // SECURITY: typed TRPCErrors — bare Errors surface as HTTP 500s.
  if (!member) {
    throw new TRPCError({ code: "BAD_REQUEST", message: "Invalid invite token" });
  }
  if (isInviteExpired(member.invitedAt)) {
    throw new TRPCError({ code: "BAD_REQUEST", message: "This invite has expired" });
  }
  if (member.acceptedAt) {
    throw new TRPCError({ code: "BAD_REQUEST", message: "Invite already accepted" });
  }

  // SECURITY: bind acceptance to the invitee identity. If the invite carries an
  // email and we know the accepting user's email, they must match — otherwise
  // anyone holding the token could claim membership under the wrong account.
  // If the member row has no email recorded, claim it for the accepting user.
  const normalize = (e: string | null | undefined) => (e ?? "").trim().toLowerCase();
  let emailToSet: string | undefined;
  if (normalize(member.email) && normalize(userEmail)) {
    if (normalize(member.email) !== normalize(userEmail)) {
      throw new TRPCError({
        code: "FORBIDDEN",
        message: "This invite was issued to a different email address",
      });
    }
  } else if (!normalize(member.email) && normalize(userEmail)) {
    emailToSet = userEmail!.trim();
  }

  // Single conditional UPDATE closes the select-then-update race: only the
  // first concurrent acceptance flips accepted_at from NULL and gets the row.
  const [updated] = await db.update(schema.campaignMembers)
    .set({
      userId,
      acceptedAt: new Date(),
      inviteToken: null,
      ...(emailToSet ? { email: emailToSet } : {}),
    })
    .where(and(
      eq(schema.campaignMembers.inviteToken, token),
      isNull(schema.campaignMembers.acceptedAt)
    ))
    .returning();
  if (!updated) {
    throw new TRPCError({ code: "BAD_REQUEST", message: "Invite already accepted" });
  }
  return updated;
}

export async function getMemberByInviteToken(token: string) {
  const db = getDb();
  if (!db) return null;
  const [member] = await db.select().from(schema.campaignMembers)
    .where(eq(schema.campaignMembers.inviteToken, token)).limit(1);
  // SECURITY: expired invites resolve to null — same as an unknown token, so
  // callers cannot probe token validity windows.
  if (!member || isInviteExpired(member.invitedAt)) return null;
  return member;
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
// Read-only hot path: called by per-request middleware. One indexed round-trip
// (candidate_profiles.id/user_id PK + campaign_members(profile_id, user_id)
// index), returns null — never throws — when the user has no membership.
export async function getMyRoleForProfile(profileId: number, userId: number): Promise<"owner" | "manager" | "viewer" | null> {
  const db = getDb();
  if (!db) return null;
  try {
    const result = await db.execute(sql<{ role: string | null }>`
      SELECT COALESCE(
        (SELECT 'owner' FROM candidate_profiles WHERE id = ${profileId} AND user_id = ${userId} LIMIT 1),
        (SELECT role::text FROM campaign_members WHERE profile_id = ${profileId} AND user_id = ${userId} LIMIT 1)
      ) AS role
    `);
    const role = result.rows[0]?.role;
    if (role === "owner" || role === "manager" || role === "viewer") return role;
    return null;
  } catch (err) {
    console.error("[getMyRoleForProfile] lookup failed:", err);
    return null;
  }
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

  // Aggregate in SQL — previously whole tables (donations, budget, timeline,
  // incidents) were loaded into JS just to be counted/summed.
  const today = new Date().toISOString().split("T")[0];
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
    db.select({
      total: sql<number>`count(*)::int`,
      compliant: sql<number>`count(*) filter (where ${schema.complianceItems.status} = 'compliant')::int`,
    }).from(schema.complianceItems).where(eq(schema.complianceItems.profileId, profileId)),
    db.select({
      total: sql<number>`coalesce(sum(${schema.fundraisingTransactions.amount}), 0)::float`,
    }).from(schema.fundraisingTransactions).where(eq(schema.fundraisingTransactions.profileId, profileId)),
    db.select({
      total: sql<number>`coalesce(sum(${schema.budgetItems.budgetedAmount}), 0)::float`,
    }).from(schema.budgetItems).where(eq(schema.budgetItems.profileId, profileId)),
    db.select({
      total: sql<number>`count(*)::int`,
      completed: sql<number>`count(*) filter (where ${schema.timelineEvents.status} = 'completed')::int`,
      nextDeadline: sql<string | null>`min(${schema.timelineEvents.eventDate}) filter (where ${schema.timelineEvents.eventDate} > ${today} and ${schema.timelineEvents.status} <> 'completed')`,
    }).from(schema.timelineEvents).where(eq(schema.timelineEvents.profileId, profileId)),
    db.select({ count: sql<number>`count(*)::int` }).from(schema.petitions).where(eq(schema.petitions.profileId, profileId)),
    db.select({ count: sql<number>`count(*)::int` }).from(schema.campaignMembers).where(eq(schema.campaignMembers.profileId, profileId)),
    db.select({
      active: sql<number>`count(*) filter (where ${schema.warRoomIncidents.status} not in ('resolved', 'escalated'))::int`,
      critical: sql<number>`count(*) filter (where ${schema.warRoomIncidents.severity} = 'critical' and ${schema.warRoomIncidents.status} not in ('resolved', 'escalated'))::int`,
    }).from(schema.warRoomIncidents).where(eq(schema.warRoomIncidents.profileId, profileId)),
  ]);

  const totalVolunteers = volunteerRows[0]?.count ?? 0;
  const complianceTotal = complianceRows[0]?.total ?? 0;
  const complianceCompliant = complianceRows[0]?.compliant ?? 0;
  const complianceScore = complianceTotal > 0 ? Math.round((complianceCompliant / complianceTotal) * 100) : 0;
  const totalFundraising = donationRows[0]?.total ?? 0;
  const totalBudget = budgetRows[0]?.total ?? 0;
  const totalPetitions = petitionRows[0]?.count ?? 0;
  const totalTeamMembers = memberRows[0]?.count ?? 0;

  const now = new Date();
  const nextDeadlineDate = timelineRows[0]?.nextDeadline ?? null;
  const daysToNextDeadline = nextDeadlineDate
    ? Math.ceil((new Date(nextDeadlineDate).getTime() - now.getTime()) / 86400000)
    : null;

  const completedMilestones = timelineRows[0]?.completed ?? 0;
  const totalMilestones = timelineRows[0]?.total ?? 0;

  const activeIncidents = incidentRows[0]?.active ?? 0;
  const criticalIncidents = incidentRows[0]?.critical ?? 0;

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
    nextDeadlineDate,
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
  const { eq, and } = await import("drizzle-orm");
  if (data.id) {
    // SECURITY: tenant-guarded update — never set profileId on update.
    const { id, profileId, ...rest } = data;
    const rows = await db.update(stakeholderContacts)
      .set(rest)
      .where(and(eq(stakeholderContacts.id, id), eq(stakeholderContacts.profileId, requireTenantId(data.profileId))))
      .returning({ id: stakeholderContacts.id });
    return assertUpdated(rows, "Stakeholder contact");
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
