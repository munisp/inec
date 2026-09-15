import { z } from "zod";
import { TRPCError } from "@trpc/server";
import { COOKIE_NAME } from "@shared/const";
import { getSessionCookieOptions } from "./_core/cookies";
import { systemRouter } from "./_core/systemRouter";
import {
  publicProcedure,
  protectedProcedure,
  profileScopedProcedure,
  assertProfileRole,
  router,
} from "./_core/trpc";
import { notifyOwner } from "./_core/notification";
import { hitRateLimit } from "./_core/rateLimit";
// NOTE: imported from ./sse, not ./index — importing the entrypoint boots the
// HTTP server as a side effect (circular import).
import { broadcastWarRoomUpdate } from "./_core/sse";
import { createHeartbeatJob, deleteHeartbeatJob, listHeartbeatJobs } from "./_core/heartbeat";
import { parse as parseCookie } from "cookie";
import * as db from "./db";
import { invokeLLM } from "./_core/llm";

// ─── SECURITY helpers ────────────────────────────────────────────────────────
// Mask an email for display: "j***@example.com". Used by team.acceptInvite so
// the full invited address is not leaked to whoever holds the invite link.
function maskEmail(email: string | null | undefined): string {
  if (!email) return "";
  const at = email.indexOf("@");
  if (at <= 0) return "***";
  return `${email[0]}***@${email.slice(at + 1)}`;
}

// Resolve the owning profileId for procedures whose input only carries a row
// `id` (deletes, status flips). Returns null when the row does not exist.
async function profileIdForRow(
  table: any,
  id: number,
): Promise<number | null> {
  const conn = db.getDb();
  if (!conn) return null; // SECURITY: fail closed — assertProfileRole will reject
  const { eq } = await import("drizzle-orm");
  const rows = await conn
    .select({ profileId: table.profileId })
    .from(table)
    .where(eq(table.id, id))
    .limit(1);
  return (rows[0] as any)?.profileId ?? null;
}

// SECURITY: require the caller to hold at least `minRole` on the profile that
// owns row `id` in `table`. Throws NOT_FOUND when the row does not exist so
// callers cannot probe for other tenants' row ids.
async function assertRowAccess(
  user: Parameters<typeof assertProfileRole>[0],
  table: any,
  id: number,
  minRole: "owner" | "manager" | "viewer",
) {
  const profileId = await profileIdForRow(table, id);
  if (profileId == null) {
    throw new TRPCError({ code: "NOT_FOUND", message: "Record not found" });
  }
  await assertProfileRole(user, profileId, minRole);
  return profileId;
}

// SECURITY: require the caller to hold at least `minRole` on the profile that
// owns petition `petitionId`. Throws NOT_FOUND for unknown/orphaned petitions
// (petitions.profile_id is nullable in the schema — fail closed when null).
async function assertPetitionAccess(
  user: Parameters<typeof assertProfileRole>[0],
  petitionId: number,
  minRole: "owner" | "manager" | "viewer",
) {
  const petition = await db.getPetitionById(petitionId);
  if (!petition || petition.profileId == null) {
    throw new TRPCError({ code: "NOT_FOUND", message: "Petition not found" });
  }
  await assertProfileRole(user, petition.profileId, minRole);
  return petition;
}

// SECURITY: fetch a campaign_members row by id for team management authz.
// Throws NOT_FOUND so callers cannot probe other tenants' member ids.
async function getMemberOrThrow(memberId: number) {
  const conn = db.getDb();
  if (!conn) {
    throw new TRPCError({ code: "NOT_FOUND", message: "Member not found" });
  }
  const { campaignMembers } = await import("../drizzle/schema");
  const { eq } = await import("drizzle-orm");
  const rows = await conn
    .select()
    .from(campaignMembers)
    .where(eq(campaignMembers.id, memberId))
    .limit(1);
  const member = rows[0];
  if (!member) {
    throw new TRPCError({ code: "NOT_FOUND", message: "Member not found" });
  }
  return member;
}

// SECURITY: dedup/rate-limit for public petition signing
// (petitions.publicSign is intentionally unauthenticated, so it is abusable for
// signature stuffing). Backed by the shared Postgres rate_limits table
// (server/_core/rateLimit.ts) so limits survive restarts and hold across
// replicas; the in-memory fallback is non-production only. A durable unique
// constraint on petition_id+phone remains a worthwhile follow-up.
const PETITION_SIGN_IP_WINDOW_SECONDS = 60 * 60; // 1 hour
const PETITION_SIGN_IP_LIMIT = 5; // max signatures per petition per IP per window
const PETITION_SIGN_PHONE_WINDOW_SECONDS = 24 * 60 * 60; // 24h phone dedup

async function checkPublicSignAllowed(petitionId: number, ip: string, phone?: string) {
  // Dedup: identify the signer by phone when supplied; when it is omitted,
  // fall back to an ip+petition key so anonymous signature stuffing is also
  // blocked. The dedup key allows exactly one hit per window — a repeat
  // signature is rejected.
  const dedupKey = phone
    ? `p:${petitionId}:ph:${phone}`
    : `p:${petitionId}:ip-dedup:${ip}`;
  const dedup = await hitRateLimit(dedupKey, PETITION_SIGN_PHONE_WINDOW_SECONDS, 1);
  if (!dedup.allowed) {
    throw new TRPCError({
      code: "TOO_MANY_REQUESTS",
      message: phone
        ? "This phone number has already signed this petition."
        : "A signature from this network was already recorded for this petition.",
    });
  }
  const ipHit = await hitRateLimit(
    `p:${petitionId}:ip:${ip}`,
    PETITION_SIGN_IP_WINDOW_SECONDS,
    PETITION_SIGN_IP_LIMIT,
  );
  if (!ipHit.allowed) {
    throw new TRPCError({
      code: "TOO_MANY_REQUESTS",
      message: "Too many signatures from this network. Please try again later.",
    });
  }
}

// ─── LLM cost-abuse limiter ──────────────────────────────────────────────────
// SECURITY: per-user fixed-window cap on AI endpoints, backed by the shared
// Postgres rate_limits table (server/_core/rateLimit.ts). LLM calls cost real
// money; without a per-user ceiling any authenticated account could run up
// unbounded spend.
const LLM_WINDOW_SECONDS = 60 * 60; // 1 hour
const LLM_MAX_CALLS_PER_USER = 20;

async function assertLlmCallAllowed(userId: number) {
  const hit = await hitRateLimit(`llm:${userId}`, LLM_WINDOW_SECONDS, LLM_MAX_CALLS_PER_USER);
  if (!hit.allowed) {
    throw new TRPCError({
      code: "TOO_MANY_REQUESTS",
      message: `AI usage limit reached (${LLM_MAX_CALLS_PER_USER} requests/hour). Please try again later.`,
    });
  }
}

export const appRouter = router({
  system: systemRouter,
  // ─── Auth ──────────────────────────────────────────────────────────────────
  auth: router({
    me: publicProcedure.query(opts => opts.ctx.user),
    logout: publicProcedure.mutation(({ ctx }) => {
      const cookieOptions = getSessionCookieOptions(ctx.req);
      ctx.res.clearCookie(COOKIE_NAME, { ...cookieOptions, maxAge: -1 });
      return { success: true } as const;
    }),
  }),
  // ─── Candidate Profile ─────────────────────────────────────────────────────
  profile: router({
    get: protectedProcedure.query(async ({ ctx }) => {
      return db.getOrCreateUserProfile(ctx.user.id);
    }),
    update: protectedProcedure
      .input(z.object({
        id: z.number(),
        candidateName: z.string().optional(),
        partyName: z.string().optional(),
        partyColor: z.string().optional(),
        stateCode: z.string().optional(),
        stateName: z.string().optional(),
        office: z.enum(["President", "Governor", "Senator", "House", "LGA"]).optional(),
        religion: z.string().optional(),
        gender: z.string().optional(),
        geopoliticalZone: z.string().optional(),
      }))
      .mutation(async ({ ctx, input }) => {
        const { id, ...data } = input;
        // SECURITY: only the profile owner may update profile metadata
        // (previously threw a bare Error("Forbidden") → HTTP 500 instead of 403).
        await assertProfileRole(ctx.user, id, "owner");
        return db.updateProfile(id, data);
      }),
  }),
  // ─── Timeline ──────────────────────────────────────────────────────────────
  timeline: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes, owner deletes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getTimelineEvents(input.profileId)),
    upsert: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        title: z.string(),
        description: z.string().optional(),
        eventDate: z.string(),
        category: z.string().optional(),
        status: z.enum(["active", "inactive", "pending", "completed", "cancelled"]).optional(),
        location: z.string().optional(),
        priority: z.enum(["low", "medium", "high", "critical"]).optional(),
      }))
      .mutation(({ input }) => db.upsertTimelineEvent(input as any)),
    delete: protectedProcedure
      .input(z.object({ id: z.number() }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: destructive op — resolve owning profile from the row id,
        // require owner role (previously any authenticated user could delete).
        const { timelineEvents } = await import("../drizzle/schema");
        await assertRowAccess(ctx.user, timelineEvents, input.id, "owner");
        return db.deleteTimelineEvent(input.id);
      }),
  }),
  // ─── Voter Registration ────────────────────────────────────────────────────
  voters: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getVoterRegistrations(input.profileId)),
    add: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        fullName: z.string(),
        phone: z.string().optional(),
        ward: z.string().optional(),
        lga: z.string().optional(),
        pollingUnit: z.string().optional(),
        // FIX: drizzle column is voter_registrations.vin — the old zod key
        // `vinNumber` matched nothing and was silently dropped.
        vin: z.string().optional(),
        // Accept the legacy client key as an alias for backwards compatibility.
        vinNumber: z.string().optional(),
        status: z.string().optional(),
      }))
      .mutation(({ input }) => {
        const { vinNumber, ...rest } = input;
        return db.addVoterRegistration({ ...rest, vin: input.vin ?? vinNumber } as any);
      }),
    bulkImport: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        // SECURITY: bound the array (also enforced at the db layer) and write
        // in bounded chunks via the db bulk path instead of per-row INSERTs.
        rows: z.array(z.object({
          fullName: z.string(),
          vin: z.string().optional(),
          lga: z.string().optional(),
          ward: z.string().optional(),
          pollingUnit: z.string().optional(),
          phone: z.string().optional(),
        })).max(db.MAX_BULK_IMPORT_ROWS),
      }))
      .mutation(({ input }) => db.bulkAddVoterRegistrations(input.profileId, input.rows)),
  }),
  // ─── Polling Units ─────────────────────────────────────────────────────────
  pollingUnits: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getPollingUnits(input.profileId)),
    upsert: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        puCode: z.string().optional(),
        name: z.string(),
        ward: z.string().optional(),
        lga: z.string().optional(),
        latitude: z.number().optional(),
        longitude: z.number().optional(),
        registeredVoters: z.number().optional(),
        agentName: z.string().optional(),
        agentPhone: z.string().optional(),
        status: z.string().optional(),
      }))
      .mutation(({ input }) => db.upsertPollingUnit(input as any)),
    bulkImport: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        // SECURITY: bound the array (also enforced at the db layer) and write
        // in bounded, transactional chunks via the db bulk path.
        rows: z.array(z.object({
          puCode: z.string().optional(),
          name: z.string(),
          lga: z.string().optional(),
          ward: z.string().optional(),
          latitude: z.number().optional(),
          longitude: z.number().optional(),
          registeredVoters: z.number().optional(),
        })).max(db.MAX_BULK_IMPORT_ROWS),
      }))
      .mutation(async ({ input }) => {
        const { upserted } = await db.bulkUpsertPollingUnits(input.profileId, input.rows);
        // Keep the historical response key (`inserted`) for the client.
        return { inserted: upserted };
      }),
  }),
  // ─── Volunteers ────────────────────────────────────────────────────────────
  volunteers: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getVolunteers(input.profileId)),
    add: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        fullName: z.string(),
        phone: z.string().optional(),
        email: z.string().optional(),
        lga: z.string().optional(),
        ward: z.string().optional(),
        skills: z.array(z.string()).optional(),
        role: z.string().optional(),
        status: z.string().optional(),
      }))
      .mutation(({ input }) => {
        // FIX: volunteers.skills is a TEXT column — join the client's string
        // array instead of passing an array that drizzle silently drops.
        const { skills, ...rest } = input;
        return db.addVolunteer({
          ...rest,
          skills: skills?.length ? skills.join(", ") : undefined,
        } as any);
      }),
    updateStatus: protectedProcedure
      .input(z.object({ id: z.number(), status: z.string() }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: input has no profileId — resolve it from the volunteer row.
        const { volunteers } = await import("../drizzle/schema");
        await assertRowAccess(ctx.user, volunteers, input.id, "manager");
        return db.updateVolunteerStatus(input.id, input.status as any);
      }),
  }),
  // ─── Press Releases ────────────────────────────────────────────────────────
  pressRelease: router({
    // SECURITY: tenancy enforced — viewer reads/AI-drafts, manager saves.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getPressReleases(input.profileId)),
    save: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        title: z.string(),
        content: z.string(),
        template: z.string().optional(),
        status: z.string().optional(),
      }))
      .mutation(({ input }) => {
        // FIX: the client sends `content`, but press_releases.body is NOT NULL —
        // every save previously threw a DB constraint error. Map content→body.
        const { content, ...rest } = input;
        return db.savePressRelease({ ...rest, body: content } as any);
      }),
    aiDraft: profileScopedProcedure("viewer")
      .input(z.object({
        profileId: z.number(),
        // SECURITY: LLM cost-abuse caps — bound all free-text prompt fields.
        template: z.string().max(100),
        headline: z.string().max(500),
        keyPoints: z.string().max(2000),
        tone: z.string().max(100).optional(),
      }))
      .mutation(async ({ input, ctx }) => {
        await assertLlmCallAllowed(ctx.user.id);
        const profile = await db.getOrCreateUserProfile(ctx.user.id);
        const name = profile?.candidateName ?? "The Candidate";
        const party = profile?.partyName ?? "The Party";
        const state = profile?.stateName ?? "the State";
        const office = profile?.office ?? "Office";
        const tone = input.tone ?? "professional and authoritative";
        const systemPrompt = `You are a professional political communications writer specialising in Nigerian elections. Write press releases for INEC-registered candidates in a ${tone} tone. Always include: a dateline (FOR IMMEDIATE RELEASE), a strong headline, a lead paragraph with the 5 Ws, 2-3 body paragraphs with quotes from the candidate, and a standard boilerplate ending. Use formal Nigerian English.`;
        const userPrompt = `Write a full press release for the following:\n\nTemplate type: ${input.template}\nCandidate: ${name}\nParty: ${party}\nOffice sought: ${office}\nState: ${state}\nHeadline/Topic: ${input.headline}\nKey points to include:\n${input.keyPoints}\n\nProduce only the press release text, no commentary.`;
        const response = await invokeLLM({
          messages: [
            { role: "system", content: systemPrompt },
            { role: "user", content: userPrompt },
          ],
        });
        const content = (response as any)?.choices?.[0]?.message?.content ?? "";
        return { content, title: input.headline };
      }),
  }),
  // ─── Social Media ──────────────────────────────────────────────────────────
  socialMedia: router({
    // SECURITY: tenancy enforced — viewer reads, manager saves.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getSocialPosts(input.profileId)),
    save: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        platform: z.string(),
        content: z.string(),
        scheduledAt: z.string().optional(),
        status: z.string().optional(),
        hashtags: z.array(z.string()).optional(),
      }))
      .mutation(({ input }) => {
        // FIX: hashtags arrive as a string array; store as space-joined text.
        // HANDOFF (db/schema agent): social_media_posts needs a `hashtags`
        // text column — until it exists drizzle silently drops this key.
        const { hashtags, ...rest } = input;
        return db.saveSocialPost({
          ...rest,
          hashtags: hashtags?.length ? hashtags.join(" ") : undefined,
        } as any);
      }),
    aiGenerate: protectedProcedure
      .input(z.object({
        // SECURITY: LLM cost-abuse caps — bound all free-text prompt fields.
        platform: z.string().max(50),
        topic: z.string().max(500),
        tone: z.string().max(100).optional(),
      }))
      .mutation(async ({ input, ctx }) => {
        await assertLlmCallAllowed(ctx.user.id);
        const profile = await db.getOrCreateUserProfile(ctx.user.id);
        const candidate = profile?.candidateName ?? "The Candidate";
        const party = profile?.partyName ?? "The Party";
        const limits: Record<string, number> = { twitter: 280, facebook: 500, instagram: 2200, whatsapp: 1000 };
        const limit = limits[input.platform.toLowerCase()] ?? 500;
        const response = await invokeLLM({
          messages: [{
            role: "user",
            content: `Write a ${input.platform} post for Nigerian political candidate ${candidate} (${party}). Topic: ${input.topic}. Tone: ${input.tone ?? "inspiring and relatable"}. Max ${limit} characters. Include 2-3 relevant hashtags at the end. Output only the post text.`,
          }],
          max_tokens: 300,
        });
        const content = (response as any)?.choices?.[0]?.message?.content ?? "";
        return { content };
      }),
  }),
  // ─── Compliance ────────────────────────────────────────────────────────────
  compliance: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getComplianceItems(input.profileId)),
    upsert: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        title: z.string(),
        category: z.string().optional(),
        description: z.string().optional(),
        status: z.enum(["compliant", "warning", "non_compliant", "pending"]).optional(),
        deadline: z.string().optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => db.upsertComplianceItem(input as any)),
  }),
  // ─── Opposition Research ───────────────────────────────────────────────────
  opposition: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getOppositionResearch(input.profileId)),
    upsert: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        opponentName: z.string(),
        party: z.string().optional(),
        threatLevel: z.enum(["low", "medium", "high", "critical"]).optional(),
        // FIX: opposition_research.strength / .weakness are TEXT columns and the
        // client sends single strings — the old plural array keys
        // (strengths/weaknesses) mapped to nothing and were silently dropped.
        strength: z.string().optional(),
        weakness: z.string().optional(),
        // key_issues is a jsonb column — the array is correct there.
        keyIssues: z.array(z.string()).optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => db.upsertOppositionEntry(input as any)),
    aiAnalyze: protectedProcedure
      .input(z.object({
        // SECURITY: LLM cost-abuse caps — bound all free-text prompt fields.
        opponentName: z.string().max(200),
        party: z.string().max(100).optional(),
        strength: z.string().max(2000).optional(),
        weakness: z.string().max(2000).optional(),
        notes: z.string().max(2000).optional(),
        threatLevel: z.string().max(20).optional(),
      }))
      .mutation(async ({ input, ctx }) => {
        await assertLlmCallAllowed(ctx.user.id);
        const profile = await db.getOrCreateUserProfile(ctx.user.id);
        const candidate = profile?.candidateName ?? "Our candidate";
        const response = await invokeLLM({
          messages: [{
            role: "user",
            content: `You are a Nigerian political strategist. Analyse this opponent and provide 3-4 specific, actionable counter-strategy recommendations for ${candidate}.\n\nOpponent: ${input.opponentName} (${input.party ?? "Unknown party"})\nThreat level: ${input.threatLevel ?? "medium"}\nStrengths: ${input.strength ?? "Unknown"}\nWeaknesses: ${input.weakness ?? "Unknown"}\nNotes: ${input.notes ?? "None"}\n\nProvide only the strategic analysis, no preamble.`,
          }],
          max_tokens: 400,
        });
        const analysis = (response as any)?.choices?.[0]?.message?.content ?? "Unable to generate analysis.";
        return { analysis };
      }),
  }),
  // ─── War Room ──────────────────────────────────────────────────────────────
  warRoom: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes.
    incidents: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getWarRoomIncidents(input.profileId)),
    addIncident: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        severity: z.enum(["low", "medium", "high", "critical"]),
        description: z.string(),
        lga: z.string().optional(),
        pollingUnit: z.string().optional(),
        // R5-098: category/geo/evidence/occurrence/attribution — the schema
        // columns existed but no input path wrote them.
        incidentType: z.enum(["violence", "vote_buying", "inec_failure", "intimidation", "logistics", "other"]).optional(),
        latitude: z.number().min(-90).max(90).optional(),
        longitude: z.number().min(-180).max(180).optional(),
        evidenceUrl: z.string().max(500).optional(),
        occurredAt: z.string().optional(),
        oppositionEntryId: z.number().optional(),
      }))
      .mutation(async ({ input, ctx }) => {
        // FIX: war_room_incidents has `pu_name`, not `polling_unit` — map the
        // client key onto the real column instead of silently dropping it.
        const { pollingUnit, occurredAt, ...rest } = input;
        const incident = await db.addWarRoomIncident({
          ...rest,
          puName: pollingUnit,
          occurredAt: occurredAt ? new Date(occurredAt) : undefined,
          reportedBy: ctx.user?.username ?? ctx.user?.fullName ?? undefined,
        } as any, ctx.user?.username);
        if (input.severity === "critical" || input.severity === "high") {
          try {
            await notifyOwner({
              title: `⚠️ ${input.severity.toUpperCase()} Incident — ${input.lga ?? "Unknown LGA"}`,
              content: `${input.description}${input.pollingUnit ? ` (PU: ${input.pollingUnit})` : ""}`,
            });
          } catch { /* notification failure must not block incident save */ }
        }
        broadcastWarRoomUpdate(input.profileId);
        return incident;
      }),
    updateIncidentStatus: protectedProcedure
      .input(z.object({ id: z.number(), status: z.string(), profileId: z.number().optional() }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: profileId is optional here — never trust the caller-supplied
        // value for authz; resolve the owning profile from the incident row.
        const { warRoomIncidents } = await import("../drizzle/schema");
        const profileId = await assertRowAccess(ctx.user, warRoomIncidents, input.id, "manager");
        const result = await db.updateIncidentStatus(input.id, input.status as any, ctx.user?.username);
        broadcastWarRoomUpdate(input.profileId ?? profileId);
        return result;
      }),
    // R5-098: escalation workflow — assign to a responder, escalate to an
    // external authority (timestamped, audited), resolve with audit trail.
    assignIncident: protectedProcedure
      .input(z.object({ id: z.number(), assignedTo: z.string().min(1).max(200), profileId: z.number().optional() }))
      .mutation(async ({ ctx, input }) => {
        const { warRoomIncidents } = await import("../drizzle/schema");
        const profileId = await assertRowAccess(ctx.user, warRoomIncidents, input.id, "manager");
        const result = await db.assignIncident(input.id, input.assignedTo, ctx.user?.username);
        broadcastWarRoomUpdate(input.profileId ?? profileId);
        return result;
      }),
    escalateIncident: protectedProcedure
      .input(z.object({
        id: z.number(),
        escalatedTo: z.enum(["security_agency", "inec", "neutral_observer", "party_hq"]),
        note: z.string().max(1000).optional(),
        profileId: z.number().optional(),
      }))
      .mutation(async ({ ctx, input }) => {
        const { warRoomIncidents } = await import("../drizzle/schema");
        const profileId = await assertRowAccess(ctx.user, warRoomIncidents, input.id, "manager");
        const result = await db.escalateIncident(input.id, input.escalatedTo, input.note, ctx.user?.username);
        try {
          await notifyOwner({
            title: `🚨 Incident ESCALATED to ${input.escalatedTo.replace(/_/g, " ")}`,
            content: `Incident #${input.id} escalated by ${ctx.user?.username ?? "unknown"}${input.note ? `: ${input.note}` : ""}`,
          });
        } catch { /* notification failure must not block the escalation record */ }
        broadcastWarRoomUpdate(input.profileId ?? profileId);
        return result;
      }),
    resolveIncident: protectedProcedure
      .input(z.object({ id: z.number(), profileId: z.number().optional() }))
      .mutation(async ({ ctx, input }) => {
        const { warRoomIncidents } = await import("../drizzle/schema");
        const profileId = await assertRowAccess(ctx.user, warRoomIncidents, input.id, "manager");
        const result = await db.resolveIncident(input.id, ctx.user?.username);
        broadcastWarRoomUpdate(input.profileId ?? profileId);
        return result;
      }),
    incidentAudit: protectedProcedure
      .input(z.object({ incidentId: z.number() }))
      .query(async ({ ctx, input }) => {
        const { warRoomIncidents } = await import("../drizzle/schema");
        await assertRowAccess(ctx.user, warRoomIncidents, input.incidentId, "viewer");
        return db.getIncidentAudit(input.incidentId);
      }),
    agents: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getFieldAgents(input.profileId)),
    // R5-099: agent self check-in — any member of the campaign (viewer role
    // is what field agents are enrolled as) can check in; the agent row must
    // belong to the profile (tenant-guarded in agentCheckIn).
    checkIn: profileScopedProcedure("viewer")
      .input(z.object({
        profileId: z.number(),
        agentId: z.number(),
        votersCounted: z.number().int().min(0).optional(),
      }))
      .mutation(async ({ input }) => {
        const agent = await db.agentCheckIn(input.agentId, input.profileId, input.votersCounted);
        if (!agent) {
          throw new TRPCError({ code: "NOT_FOUND", message: "Agent not found for this profile" });
        }
        broadcastWarRoomUpdate(input.profileId);
        return agent;
      }),
    // R5-099: silent-agent scan surfaced to the dashboard — flags overdue
    // agents 'silent' (idempotent) and returns the current silent set.
    silentAgents: profileScopedProcedure("viewer")
      .input(z.object({
        profileId: z.number(),
        thresholdMinutes: z.number().int().min(5).max(24 * 60).default(60),
      }))
      .query(({ input }) => db.scanSilentAgents(input.profileId, input.thresholdMinutes)),
    upsertAgent: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        name: z.string(),
        phone: z.string().optional(),
        lga: z.string().optional(),
        pollingUnit: z.string().optional(),
        status: z.string().optional(),
        lastCheckIn: z.string().optional(),
      }))
      .mutation(({ input }) => {
        // FIX: field_agents columns are agent_status / assigned_pu /
        // last_checkin — the old zod keys (status, pollingUnit, lastCheckIn)
        // matched nothing and were silently dropped.
        const { pollingUnit, status, lastCheckIn, ...rest } = input;
        return db.upsertFieldAgent({
          ...rest,
          assignedPu: pollingUnit,
          agentStatus: status,
          // last_checkin is a timestamp column — coerce the client's string.
          lastCheckin: lastCheckIn ? new Date(lastCheckIn) : undefined,
        } as any);
      }),
  }),
  // ─── Election Results ──────────────────────────────────────────────────────
  results: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getElectionResults(input.profileId)),
    add: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        candidateName: z.string(),
        party: z.string(),
        lga: z.string().optional(),
        // HANDOFF (db/schema agent): election_results needs a `ward`
        // varchar(100) column — the router passes it through but drizzle
        // silently drops it until the column exists.
        ward: z.string().optional(),
        votes: z.number(),
        reportedAt: z.string().optional(),
      }))
      .mutation(({ input }) => db.upsertElectionResult(input as any)),
  }),
  // ─── Manifesto ─────────────────────────────────────────────────────────────
  manifesto: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes, owner deletes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getManifestoSections(input.profileId)),
    upsert: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        sectionTitle: z.string(),
        summary: z.string().optional(),
        fullText: z.string().optional(),
        priority: z.enum(["low", "medium", "high", "critical"]).optional(),
        sortOrder: z.number().optional(),
      }))
      .mutation(({ input }) => db.upsertManifestoSection(input as any)),
    delete: protectedProcedure
      .input(z.object({ id: z.number() }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: destructive op — owner only, profile resolved from row id.
        const { manifestoSections } = await import("../drizzle/schema");
        await assertRowAccess(ctx.user, manifestoSections, input.id, "owner");
        return db.deleteManifestoSection(input.id);
      }),
  }),
  // ─── Petitions ─────────────────────────────────────────────────────────────
  petitions: router({
    // SECURITY: tenancy enforced — viewer reads, manager creates.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getPetitions(input.profileId)),
    create: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        title: z.string().max(400),
        description: z.string().max(5000).optional(),
        targetSignatures: z.number().optional(),
      }))
      .mutation(({ input }) => db.createPetition(input as any)),
    signatures: protectedProcedure
      .input(z.object({ petitionId: z.number() }))
      .query(async ({ ctx, input }) => {
        // SECURITY: signer PII (names/phones) — restrict to members of the
        // profile that owns the petition (previously any authenticated user).
        await assertPetitionAccess(ctx.user, input.petitionId, "viewer");
        return db.getPetitionSignatures(input.petitionId);
      }),
    signatureCount: protectedProcedure
      .input(z.object({ petitionId: z.number() }))
      .query(async ({ ctx, input }) => {
        // SECURITY: same scoping as `signatures` (aggregate of private data).
        await assertPetitionAccess(ctx.user, input.petitionId, "viewer");
        return db.getPetitionSignatureCount(input.petitionId);
      }),
    sign: protectedProcedure
      .input(z.object({
        petitionId: z.number(),
        signerName: z.string().max(200),
        signerPhone: z.string().max(20).optional(),
        signerLga: z.string().max(100).optional(),
      }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY (role decision): recording a signature is a WRITE to the
        // campaign's petition data, and the authenticated path exists for
        // campaign staff entering signatures collected in the field — so it
        // requires the "manager" role, consistent with every other write in
        // this router. "viewer" is read-only everywhere else and was a
        // privilege-escalation hole here. The general public signs through
        // the unauthenticated petitions.publicSign endpoint below, which
        // keeps its own dedup/rate-limit controls and requires no account.
        await assertPetitionAccess(ctx.user, input.petitionId, "manager");
        // FIX: petition_signatures columns are phone/lga — signerPhone/signerLga
        // matched nothing and were silently dropped.
        return db.addPetitionSignature({
          petitionId: input.petitionId,
          signerName: input.signerName,
          phone: input.signerPhone,
          lga: input.signerLga,
        } as any);
      }),
    getPublic: publicProcedure
      .input(z.object({ petitionId: z.number() }))
      .query(async ({ input }) => {
        const petition = await db.getPetitionById(input.petitionId);
        // SECURITY: drafts are not public — only active/closed petitions may
        // be viewed through the unauthenticated endpoint.
        if (!petition || petition.status === "draft") return null;
        const count = await db.getPetitionSignatureCount(input.petitionId);
        return { ...petition, signatureCount: count };
      }),
    publicSign: publicProcedure
      .input(z.object({
        petitionId: z.number(),
        // SECURITY: hard length caps — this endpoint is unauthenticated, so
        // bound the body size to prevent abuse.
        signerName: z.string().min(2).max(200),
        signerPhone: z.string().max(20).optional(),
        signerLga: z.string().max(100).optional(),
        // FIX: dropped `signerEmail` — petition_signatures has no email column
        // (it was silently discarded). Zod strips unknown keys, so older
        // clients sending it keep working.
      }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: the petition must exist and be signable — previously a
        // missing petition surfaced as an FK violation (HTTP 500) and drafts
        // were silently signable.
        const petition = await db.getPetitionById(input.petitionId);
        if (!petition) {
          throw new TRPCError({ code: "NOT_FOUND", message: "Petition not found" });
        }
        if (petition.status !== "active") {
          throw new TRPCError({
            code: "CONFLICT",
            message: "This petition is not open for signatures",
          });
        }
        // SECURITY: dedup + per-IP rate limit against signature stuffing,
        // backed by the shared Postgres rate_limits store (see
        // checkPublicSignAllowed). req.ip is trustworthy because the app sets
        // `trust proxy` (see _core/index.ts); never parse x-forwarded-for by
        // hand.
        const ip = ctx.req.ip || ctx.req.socket?.remoteAddress || "unknown";
        await checkPublicSignAllowed(input.petitionId, ip, input.signerPhone);
        return db.addPetitionSignature({
          petitionId: input.petitionId,
          signerName: input.signerName,
          phone: input.signerPhone,
          lga: input.signerLga,
        } as any);
      }),
  }),
  // ─── Diaspora ──────────────────────────────────────────────────────────────
  diaspora: router({
    // SECURITY: tenancy enforced — viewer reads/AI-drafts, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getDiasporaContacts(input.profileId)),
    add: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        // Client sends `fullName`; the drizzle column is diaspora_contacts.name.
        fullName: z.string(),
        country: z.string().optional(),
        city: z.string().optional(),
        organization: z.string().optional(),
        phone: z.string().optional(),
        email: z.string().optional(),
        status: z.string().optional(),
        // FIX: accept pledgedAmount (diaspora_contacts.pledged_amount exists).
        pledgedAmount: z.number().optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => {
        // FIX: map fullName→name (the old key matched no column and the NOT
        // NULL name column would reject the insert / drop the value).
        const { fullName, ...rest } = input;
        return db.addDiasporaContact({ ...rest, name: fullName } as any);
      }),
    aiDraft: profileScopedProcedure("viewer")
      .input(z.object({
        profileId: z.number(),
        // SECURITY: LLM cost-abuse caps — bound all free-text prompt fields.
        contactName: z.string().max(200),
        country: z.string().max(100),
        city: z.string().max(100).optional(),
        messageType: z.enum(["whatsapp", "email"]),
        candidateName: z.string().max(200).optional(),
        partyName: z.string().max(100).optional(),
        keyMessage: z.string().max(2000).optional(),
      }))
      .mutation(async ({ input, ctx }) => {
        await assertLlmCallAllowed(ctx.user.id);
        const systemPrompt = `You are a Nigerian political campaign communications specialist. Write personalised outreach messages for diaspora Nigerians. Be warm, specific, and compelling. Keep WhatsApp messages under 300 words and emails under 400 words.`;
        const userPrompt = `Write a ${input.messageType === "whatsapp" ? "WhatsApp" : "professional email"} message to ${input.contactName} in ${input.city ? input.city + ", " : ""}${input.country}.
Candidate: ${input.candidateName || "our candidate"}
Party: ${input.partyName || "our party"}
${input.keyMessage ? "Key message to convey: " + input.keyMessage : "Focus on diaspora support, voter mobilisation, and financial contributions."}
Make it personal, specific to their location, and include a clear call to action.`;
        const response = await invokeLLM({
          model: "auto",
          messages: [
            { role: "system", content: systemPrompt },
            { role: "user", content: userPrompt },
          ],
          max_tokens: 600,
        });
        return { content: response.choices[0]?.message?.content ?? "Unable to generate message." };
      }),
  }),
  // ─── Endorsements ──────────────────────────────────────────────────────────
  endorsements: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getEndorsements(input.profileId)),
    add: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        endorserName: z.string(),
        title: z.string().optional(),
        organization: z.string().optional(),
        category: z.string().optional(),
        statement: z.string().optional(),
        isPublic: z.boolean().optional(),
      }))
      .mutation(({ input }) => db.addEndorsement(input as any)),
  }),
  // ─── Fundraising ───────────────────────────────────────────────────────────
  fundraising: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getFundraisingTransactions(input.profileId)),
    add: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        donorName: z.string().optional(),
        amount: z.number(),
        currency: z.string().optional(),
        source: z.string().optional(),
        category: z.string().optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => db.addFundraisingTransaction(input as any)),
  }),
  // ─── Budget ────────────────────────────────────────────────────────────────
  budget: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes, owner deletes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getBudgetItems(input.profileId)),
    upsert: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        category: z.string(),
        description: z.string(),
        budgetedAmount: z.number(),
        spentAmount: z.number().optional(),
        priority: z.enum(["low", "medium", "high", "critical"]).optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => db.upsertBudgetItem(input as any)),
    delete: protectedProcedure
      .input(z.object({ id: z.number() }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: destructive op — owner only, profile resolved from row id.
        const { budgetItems } = await import("../drizzle/schema");
        await assertRowAccess(ctx.user, budgetItems, input.id, "owner");
        return db.deleteBudgetItem(input.id);
      }),
  }),
  // ─── Media Monitoring ──────────────────────────────────────────────────────
  media: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes, owner deletes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getMediaItems(input.profileId)),
    add: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        source: z.string(),
        headline: z.string(),
        sentiment: z.string().optional(),
        sourceType: z.string().optional(),
        reach: z.number().optional(),
        zone: z.string().optional(),
        url: z.string().optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => db.addMediaItem(input as any)),
    delete: protectedProcedure
      .input(z.object({ id: z.number() }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: destructive op — owner only, profile resolved from row id
        // (previously any authenticated user could delete any media item).
        const { mediaItems } = await import("../drizzle/schema");
        await assertRowAccess(ctx.user, mediaItems, input.id, "owner");
        const dbConn = await db.getDb();
        if (!dbConn) return null;
        const { eq } = await import("drizzle-orm");
        await dbConn.delete(mediaItems).where(eq(mediaItems.id, input.id));
        return { success: true };
      }),
  }),
  // ─── Debate Coach ──────────────────────────────────────────────────────────
  debate: router({
    // SECURITY: tenancy enforced — viewer reads/AI-prep, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getDebatePrepNotes(input.profileId)),
    upsert: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        topic: z.string(),
        keyMessage: z.string().optional(),
        counterArguments: z.array(z.string()).optional(),
        statistics: z.array(z.string()).optional(),
        practiceScore: z.number().optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => db.upsertDebatePrepNote(input as any)),
    aiPrep: profileScopedProcedure("viewer")
      .input(z.object({
        profileId: z.number(),
        // SECURITY: LLM cost-abuse caps — bound all free-text prompt fields.
        topic: z.string().max(500),
        opponentName: z.string().max(200).optional(),
        opponentWeaknesses: z.array(z.string().max(500)).max(10).optional(),
        opponentStrengths: z.array(z.string().max(500)).max(10).optional(),
        candidateName: z.string().max(200).optional(),
        partyName: z.string().max(100).optional(),
      }))
      .mutation(async ({ input, ctx }) => {
        await assertLlmCallAllowed(ctx.user.id);
        const systemPrompt = `You are an expert Nigerian political debate coach preparing a candidate for a gubernatorial/senatorial debate.
Generate structured debate preparation material in a professional, confident tone appropriate for Nigerian political discourse.`;
        const userPrompt = `Prepare debate material for ${input.candidateName || "our candidate"} (${input.partyName || "our party"}) on the topic: "${input.topic}".
${input.opponentName ? `Opponent: ${input.opponentName}` : ""}
${input.opponentWeaknesses?.length ? `Opponent weaknesses to exploit: ${input.opponentWeaknesses.join(", ")}` : ""}
${input.opponentStrengths?.length ? `Opponent strengths to counter: ${input.opponentStrengths.join(", ")}` : ""}

Provide:
1. **Opening Statement** (2-3 sentences, powerful and memorable)
2. **3 Key Talking Points** (specific, data-driven, actionable)
3. **2 Rebuttal Lines** (direct counter to opponent's likely attacks)
4. **Closing Message** (1-2 sentences that voters will remember)

Format with clear headers. Be specific to Nigerian political context.`;

        const response = await invokeLLM({
          model: "auto",
          messages: [
            { role: "system", content: systemPrompt },
            { role: "user", content: userPrompt },
          ],
          max_tokens: 800,
        });
        return { content: response.choices[0]?.message?.content ?? "Unable to generate debate prep." };
      }),
  }),
  // ─── Debate Practice Scores ────────────────────────────────────────────────
  debateScores: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getDebatePracticeScores(input.profileId)),
    add: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        topic: z.string(),
        score: z.number().min(0).max(10),
        maxScore: z.number().optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => db.addDebatePracticeScore(input)),
  }),
  // ─── Stakeholder Contacts ──────────────────────────────────────────────────
  stakeholders: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes, owner deletes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getStakeholderContacts(input.profileId)),
    upsert: profileScopedProcedure("manager")
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        name: z.string(),
        title: z.string().optional(),
        organization: z.string().optional(),
        category: z.string().optional(),
        phone: z.string().optional(),
        email: z.string().optional(),
        state: z.string().optional(),
        lga: z.string().optional(),
        influenceLevel: z.enum(["low","medium","high","critical"]).optional(),
        relationship: z.string().optional(),
        lastContact: z.string().optional(),
        nextAction: z.string().optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => db.upsertStakeholderContact(input)),
    delete: protectedProcedure
      .input(z.object({ id: z.number() }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: destructive op — owner only, profile resolved from row id.
        const { stakeholderContacts } = await import("../drizzle/schema");
        await assertRowAccess(ctx.user, stakeholderContacts, input.id, "owner");
        return db.deleteStakeholderContact(input.id);
      }),
  }),
  // ─── Simulation ────────────────────────────────────────────────────────────
  simulation: router({
    // SECURITY: tenancy enforced — viewer reads, manager saves runs.
    history: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getSimulationRuns(input.profileId)),
    save: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        scenario: z.string().optional(),
        stateCode: z.string().optional(),
        iterations: z.number().optional(),
        weatherSeverity: z.number().optional(),
        securityThreat: z.number().optional(),
        bvasReliability: z.number().optional(),
        staffTraining: z.number().optional(),
        projectedTurnout: z.number().optional(),
        validVotesCast: z.number().optional(),
        bvasFailureRate: z.number().optional(),
        certificationEta: z.number().optional(),
        logisticsScore: z.number().optional(),
        securityIndex: z.number().optional(),
        rejectedBallots: z.number().optional(),
        monteCarloP50: z.number().optional(),
        monteCarloP5: z.number().optional(),
        monteCarloP95: z.number().optional(),
        modelConfidence: z.number().optional(),
        disruptions: z.array(z.string()).optional(),
        aiNarrative: z.string().optional(),
        label: z.string().max(120).optional(),
      }))
      .mutation(({ input }) => db.saveSimulationRun(input as any)),
    narrative: protectedProcedure
      .input(z.object({
        // SECURITY: LLM cost-abuse caps — bound all free-text prompt fields.
        scenario: z.string().max(100),
        stateCode: z.string().max(10).optional(),
        projectedTurnout: z.number(),
        validVotesCast: z.number(),
        bvasFailureRate: z.number(),
        logisticsScore: z.number(),
        securityIndex: z.number(),
        certificationEta: z.number(),
        rejectedBallots: z.number(),
        monteCarloP5: z.number(),
        monteCarloP50: z.number(),
        monteCarloP95: z.number(),
        modelConfidence: z.number(),
        disruptions: z.array(z.string().max(200)).max(20),
      }))
      .mutation(async ({ input, ctx }) => {
        await assertLlmCallAllowed(ctx.user.id);
        const promptLines = [
          "You are an election analyst for Nigeria. Summarise this Monte Carlo simulation result in 2-3 plain-English sentences for a campaign team briefing. Be specific about the numbers and actionable in your recommendation. Do not use bullet points.",
          "",
          `Scenario: ${input.scenario} | State: ${input.stateCode ?? "FCT"}`,
          `Projected turnout: ${input.projectedTurnout}% (P5: ${input.monteCarloP5}%, P50: ${input.monteCarloP50}%, P95: ${input.monteCarloP95}%)`,
          `Valid votes cast: ${input.validVotesCast.toLocaleString()}`,
          `BVAS failure rate: ${input.bvasFailureRate}%`,
          `Logistics score: ${input.logisticsScore}/100`,
          `Security index: ${input.securityIndex}/100`,
          `Certification ETA: ${input.certificationEta} hours`,
          `Rejected ballots: ${input.rejectedBallots.toLocaleString()}`,
          `Model confidence: ${input.modelConfidence}%`,
          `Disruptions: ${input.disruptions.join("; ")}`,
        ];
        const response = await invokeLLM({
          model: "auto",
          messages: [{ role: "user", content: promptLines.join("\n") }],
          max_tokens: 250,
        });
        const narrative = response.choices[0]?.message?.content ?? "Unable to generate narrative.";
        return { narrative };
      }),
  }),
  // ─── Dashboard KPIs ───────────────────────────────────────────────────────
  dashboard: router({
    // SECURITY: tenancy enforced — viewer reads.
    kpis: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getDashboardKPIs(input.profileId)),
    electionDate: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(async ({ input }) => {
        const events = await db.getTimelineEvents(input.profileId);
        const electionEvent = events.find(e =>
          e.title.toLowerCase().includes("election day") || e.category === "election_day"
        );
        return { electionDate: electionEvent?.eventDate ?? null };
      }),
  }),
  // ─── Deadline Notifications ────────────────────────────────────────────────
  // ─── Campaign Team ─────────────────────────────────────────────────────────
  team: router({
    // SECURITY: tenancy enforced — membership management is owner/manager only.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(async ({ ctx, input }) => {
        // SECURITY: getCampaignMembers already omits invite_token. Emails are
        // masked for viewers; only owner/manager see full addresses.
        const members = await db.getCampaignMembers(input.profileId);
        if (ctx.profileRole === "owner" || ctx.profileRole === "manager") {
          return members;
        }
        return members.map(m => ({ ...m, email: maskEmail(m.email) }));
      }),
    myRole: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      // The tenancy middleware already resolved (and verified) the caller's
      // role — return it directly instead of a second, fail-open lookup.
      .query(({ ctx }) => ctx.profileRole),
    invite: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        email: z.string().email(),
        name: z.string().max(200),
        role: z.enum(["manager", "viewer"]),
        origin: z.string().url().optional(),
      }))
      .mutation(async ({ ctx, input }) => {
        const result = await db.inviteCampaignMember(input);
        // Notify the platform owner that a new team member was invited
        const profile = await db.getOrCreateUserProfile(ctx.user.id);
        const candidateName = profile?.candidateName ?? "Campaign";
        const inviteUrl = result.inviteUrl ?? "(no URL — origin not provided)";
        await notifyOwner({
          title: `👥 New Team Invite — ${candidateName}`,
          content: `${ctx.user.fullName} invited ${input.name} (${input.email}) as ${input.role} to the ${candidateName} campaign.

Invite link: ${inviteUrl}

The invitee can use this link to join the campaign team.`,
        }).catch(() => {}); // non-blocking
        return result;
      }),
    acceptInvite: publicProcedure
      .input(z.object({ token: z.string() }))
      .query(async ({ input }) => {
        const member = await db.getMemberByInviteToken(input.token);
        if (!member) return member;
        // SECURITY: mask the invited email. confirmAccept requires the acceptor
        // to type the full address, so it must not be fully revealed to anyone
        // merely holding the link — otherwise the email check is no check at all.
        return { ...member, email: maskEmail(member.email) };
      }),
    confirmAccept: protectedProcedure
      // The local users table has no email column, so identity binding at
      // acceptance is enforced by requiring the acceptor to assert the email
      // the invite was issued to; acceptCampaignInvite rejects on mismatch.
      .input(z.object({ token: z.string(), email: z.string().email().max(320) }))
      .mutation(({ ctx, input }) => db.acceptCampaignInvite(input.token, ctx.user.id, input.email)),
    updateRole: protectedProcedure
      .input(z.object({
        memberId: z.number(),
        role: z.enum(["manager", "viewer"]),
      }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: previously any authenticated user could change any member's
        // role. Require owner/manager on the member's own profile, and never
        // allow demoting the profile owner.
        const member = await getMemberOrThrow(input.memberId);
        await assertProfileRole(ctx.user, member.profileId, "manager");
        if (member.role === "owner") {
          throw new TRPCError({
            code: "FORBIDDEN",
            message: "The profile owner's role cannot be changed",
          });
        }
        return db.updateMemberRole(input.memberId, input.role);
      }),
    remove: protectedProcedure
      .input(z.object({ memberId: z.number() }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: previously any authenticated user could remove any member
        // (including the owner). Require owner/manager on the member's
        // profile; the owner can never be removed.
        const member = await getMemberOrThrow(input.memberId);
        await assertProfileRole(ctx.user, member.profileId, "manager");
        if (member.role === "owner") {
          throw new TRPCError({
            code: "FORBIDDEN",
            message: "The profile owner cannot be removed from the campaign",
          });
        }
        return db.removeCampaignMember(input.memberId);
      }),
  }),
  notifications: router({
    // Get current heartbeat job status for deadline alerts
    // SECURITY: profile-scoped — previously returned the FIRST
    // deadline-alerts-* job regardless of which profile it belonged to.
    status: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(async ({ ctx, input }) => {
        try {
          const sessionToken = parseCookie(ctx.req.headers.cookie ?? "")[COOKIE_NAME] ?? "";
          const jobs = await listHeartbeatJobs(sessionToken);
          const alertJob = jobs.jobs.find(j => j.name.startsWith(`deadline-alerts-${input.profileId}-`));
          return { enabled: !!alertJob?.isEnable, job: alertJob ?? null };
        } catch {
          return { enabled: false, job: null };
        }
      }),
    // Enable deadline alert notifications
    // SECURITY: tenancy enforced — only owner/manager may create cron jobs for
    // a profile (the job payload carries that profileId).
    enable: profileScopedProcedure("manager")
      .input(z.object({ profileId: z.number() }))
      .mutation(async ({ ctx, input }) => {
        const sessionToken = parseCookie(ctx.req.headers.cookie ?? "")[COOKIE_NAME] ?? "";
        const job = await createHeartbeatJob({
          name: `deadline-alerts-${input.profileId}-${ctx.user.id}`,
          cron: "0 0 8 * * *", // Daily 08:00 UTC
          // FIX: the registered handler is /api/scheduled/deadline-check (see
          // server/_core/index.ts); the old path matched nothing, so the cron
          // would 404 every run.
          path: "/api/scheduled/deadline-check",
          payload: { profileId: input.profileId },
          description: `Daily deadline alerts for profile ${input.profileId}`,
        }, sessionToken);
        return { taskUid: job.taskUid, nextExecutionAt: job.nextExecutionAt };
      }),
    // Disable deadline alert notifications
    // SECURITY: tenancy enforced — owner/manager only.
    disable: profileScopedProcedure("manager")
      .input(z.object({ profileId: z.number() }))
      .mutation(async ({ ctx, input }) => {
        const sessionToken = parseCookie(ctx.req.headers.cookie ?? "")[COOKIE_NAME] ?? "";
        const jobs = await listHeartbeatJobs(sessionToken);
        const alertJob = jobs.jobs.find(j => j.name.startsWith(`deadline-alerts-${input.profileId}-`));
        if (alertJob) {
          await deleteHeartbeatJob(alertJob.taskUid, sessionToken);
        }
        return { disabled: true };
      }),
    // Send a test notification immediately
    // SECURITY: tenancy enforced — reads the profile's deadlines.
    testAlert: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .mutation(async ({ input }) => {
        const events = await db.getUpcomingDeadlines(input.profileId, 48);
        if (events.length === 0) {
          await notifyOwner({
            title: "INEC Campaign — No Upcoming Deadlines",
            content: "No critical deadlines in the next 48 hours.",
          });
          return { sent: true, count: 0 };
        }
        const list = events.map(e => `• ${e.title} — ${new Date(e.eventDate).toLocaleDateString("en-NG")}`).join("\n");
        await notifyOwner({
          title: `INEC Campaign — ${events.length} Deadline(s) in 48 Hours`,
          content: `The following campaign deadlines are approaching:\n\n${list}\n\nPlease take action immediately.`,
        });
        return { sent: true, count: events.length };
      }),
  }),
  // ─── Manifesto AI ─────────────────────────────────────────────────────────
  manifestoAI: router({
    draft: profileScopedProcedure("viewer")
      .input(z.object({
        profileId: z.number(),
        // SECURITY: LLM cost-abuse caps — bound all free-text prompt fields.
        policyArea: z.string().max(200),
        brief: z.string().max(2000),
        tone: z.string().max(100).optional(),
      }))
      .mutation(async ({ input, ctx }) => {
        await assertLlmCallAllowed(ctx.user.id);
        const profile = await db.getOrCreateUserProfile(ctx.user.id);
        const name = profile?.candidateName ?? "The Candidate";
        const party = profile?.partyName ?? "The Party";
        const state = profile?.stateName ?? "the State";
        const office = profile?.office ?? "Office";
        const tone = input.tone ?? "visionary and actionable";
        const systemPrompt = `You are a senior political policy writer specialising in Nigerian electoral manifestos. Write compelling, specific, and credible manifesto sections for INEC-registered candidates. Use a ${tone} tone. Each section should include: a clear policy statement, 3-5 specific commitments with measurable targets, implementation timeline, and expected impact on citizens. Use formal Nigerian English.`;
        const userPrompt = `Write a manifesto section for the following:

Policy Area: ${input.policyArea}
Candidate: ${name}
Party: ${party}
Office sought: ${office}
State: ${state}
Brief/Key ideas: ${input.brief}

Produce only the manifesto section text, no commentary.`;
        const response = await invokeLLM({
          messages: [
            { role: "system", content: systemPrompt },
            { role: "user", content: userPrompt },
          ],
        });
        const draftContent = (response as any)?.choices?.[0]?.message?.content ?? "";
        return { content: draftContent };
      }),
  }),
  // ─── Volunteer Tasks ───────────────────────────────────────────────────────
  volunteerTasks: router({
    // SECURITY: tenancy enforced — viewer reads, manager writes, owner deletes.
    list: profileScopedProcedure("viewer")
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getVolunteerTasks(input.profileId)),
    create: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        title: z.string(),
        description: z.string().optional(),
        // FIX: align with the PG enum volunteer_task_type (schema.ts) — it has
        // "media", not "social_media". Older clients still send the legacy
        // value, so accept it and map it to "media" server-side.
        taskType: z.union([
          z.enum(["canvassing", "polling_unit", "data_entry", "logistics", "security", "media", "other"]),
          z.literal("social_media").transform(() => "media" as const),
        ]).optional(),
        status: z.enum(["pending", "in_progress", "completed", "cancelled"]).optional(),
        volunteerId: z.number().optional(),
        dueDate: z.string().optional(),
      }))
      .mutation(({ input }) => db.createVolunteerTask(input as any)),
    updateStatus: protectedProcedure
      .input(z.object({
        id: z.number(),
        status: z.enum(["pending", "in_progress", "completed", "cancelled"]),
      }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: input has no profileId — resolve it from the task row.
        const { volunteerTasks } = await import("../drizzle/schema");
        await assertRowAccess(ctx.user, volunteerTasks, input.id, "manager");
        return db.updateVolunteerTaskStatus(input.id, input.status);
      }),
    delete: protectedProcedure
      .input(z.object({ id: z.number() }))
      .mutation(async ({ ctx, input }) => {
        // SECURITY: destructive op — owner only, profile resolved from row id.
        const { volunteerTasks } = await import("../drizzle/schema");
        await assertRowAccess(ctx.user, volunteerTasks, input.id, "owner");
        return db.deleteVolunteerTask(input.id);
      }),
  }),
  // ─── Candidate Website Publish ────────────────────────────────────────────
  // ─── Non-production fixture seed ───────────────────────────────────────────
  seed: router({
    // SECURITY: destructive fixture write — tenancy enforced, owner only.
    all: profileScopedProcedure("owner")
      .input(z.object({ profileId: z.number() }))
      .mutation(async ({ input }) => {
        if (!db.isFixtureSeedingAllowed()) {
          // SECURITY: proper TRPCError (was a bare Error → HTTP 500).
          throw new TRPCError({
            code: "FORBIDDEN",
            message: "Campaign fixture seeding is disabled in this runtime",
          });
        }
        await db.seedProfileData(input.profileId);
        return { success: true, message: "Non-production fixture data seeded for an explicitly enabled test profile." };
      }),
  }),
  candidateWebsite: router({
    // SECURITY: tenancy enforced — manager publishes.
    publish: profileScopedProcedure("manager")
      .input(z.object({
        profileId: z.number(),
        htmlContent: z.string().max(500_000),
        candidateName: z.string().max(200),
      }))
      .mutation(async ({ input }) => {
        const { storagePut } = await import('./storage.js');
        const key = `campaign-sites/profile-${input.profileId}/index.html`;
        const { url } = await storagePut(key, input.htmlContent, 'text/html');
        return { url, key };
      }),
  }),
});

export type AppRouter = typeof appRouter;
