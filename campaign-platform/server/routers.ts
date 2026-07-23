import { z } from "zod";
import { COOKIE_NAME } from "@shared/const";
import { getSessionCookieOptions } from "./_core/cookies";
import { systemRouter } from "./_core/systemRouter";
import { publicProcedure, protectedProcedure, router } from "./_core/trpc";
import { notifyOwner } from "./_core/notification";
import { broadcastWarRoomUpdate } from "./_core/index";
import { createHeartbeatJob, deleteHeartbeatJob, listHeartbeatJobs } from "./_core/heartbeat";
import { parse as parseCookie } from "cookie";
import * as db from "./db";
import { invokeLLM } from "./_core/llm";

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
        // Ensure user owns this profile
        const profile = await db.getProfileById(id);
        if (!profile || profile.userId !== ctx.user.id) throw new Error("Forbidden");
        return db.updateProfile(id, data);
      }),
  }),
  // ─── Timeline ──────────────────────────────────────────────────────────────
  timeline: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getTimelineEvents(input.profileId)),
    upsert: protectedProcedure
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
      .mutation(({ input }) => db.deleteTimelineEvent(input.id)),
  }),
  // ─── Voter Registration ────────────────────────────────────────────────────
  voters: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getVoterRegistrations(input.profileId)),
    add: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        fullName: z.string(),
        phone: z.string().optional(),
        ward: z.string().optional(),
        lga: z.string().optional(),
        pollingUnit: z.string().optional(),
        vinNumber: z.string().optional(),
        status: z.string().optional(),
      }))
      .mutation(({ input }) => db.addVoterRegistration(input as any)),
    bulkImport: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        rows: z.array(z.object({
          fullName: z.string(),
          vin: z.string().optional(),
          lga: z.string().optional(),
          ward: z.string().optional(),
          pollingUnit: z.string().optional(),
          phone: z.string().optional(),
        })),
      }))
      .mutation(async ({ input }) => {
        let inserted = 0;
        for (const row of input.rows) {
          if (!row.fullName?.trim()) continue;
          await db.addVoterRegistration({ profileId: input.profileId, fullName: row.fullName, vinNumber: row.vin, lga: row.lga, ward: row.ward, pollingUnit: row.pollingUnit, phone: row.phone } as any);
          inserted++;
        }
        return { inserted };
      }),
  }),
  // ─── Polling Units ─────────────────────────────────────────────────────────
  pollingUnits: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getPollingUnits(input.profileId)),
    upsert: protectedProcedure
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
    bulkImport: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        rows: z.array(z.object({
          puCode: z.string().optional(),
          name: z.string(),
          lga: z.string().optional(),
          ward: z.string().optional(),
          latitude: z.number().optional(),
          longitude: z.number().optional(),
          registeredVoters: z.number().optional(),
        })),
      }))
      .mutation(async ({ input }) => {
        let inserted = 0;
        for (const row of input.rows) {
          if (!row.name?.trim()) continue;
          await db.upsertPollingUnit({ profileId: input.profileId, ...row } as any);
          inserted++;
        }
        return { inserted };
      }),
  }),
  // ─── Volunteers ────────────────────────────────────────────────────────────
  volunteers: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getVolunteers(input.profileId)),
    add: protectedProcedure
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
      .mutation(({ input }) => db.addVolunteer(input as any)),
    updateStatus: protectedProcedure
      .input(z.object({ id: z.number(), status: z.string() }))
      .mutation(({ input }) => db.updateVolunteerStatus(input.id, input.status as any)),
  }),
  // ─── Press Releases ────────────────────────────────────────────────────────
  pressRelease: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getPressReleases(input.profileId)),
    save: protectedProcedure
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        title: z.string(),
        content: z.string(),
        template: z.string().optional(),
        status: z.string().optional(),
      }))
      .mutation(({ input }) => db.savePressRelease(input as any)),
    aiDraft: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        template: z.string(),
        headline: z.string(),
        keyPoints: z.string(),
        tone: z.string().optional(),
      }))
      .mutation(async ({ input, ctx }) => {
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
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getSocialPosts(input.profileId)),
    save: protectedProcedure
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        platform: z.string(),
        content: z.string(),
        scheduledAt: z.string().optional(),
        status: z.string().optional(),
        hashtags: z.array(z.string()).optional(),
      }))
      .mutation(({ input }) => db.saveSocialPost(input as any)),
    aiGenerate: protectedProcedure
      .input(z.object({
        platform: z.string(),
        topic: z.string(),
        tone: z.string().optional(),
      }))
      .mutation(async ({ input, ctx }) => {
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
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getComplianceItems(input.profileId)),
    upsert: protectedProcedure
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
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getOppositionResearch(input.profileId)),
    upsert: protectedProcedure
      .input(z.object({
        id: z.number().optional(),
        profileId: z.number(),
        opponentName: z.string(),
        party: z.string().optional(),
        threatLevel: z.enum(["low", "medium", "high", "critical"]).optional(),
        strengths: z.array(z.string()).optional(),
        weaknesses: z.array(z.string()).optional(),
        keyIssues: z.array(z.string()).optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => db.upsertOppositionEntry(input as any)),
    aiAnalyze: protectedProcedure
      .input(z.object({
        opponentName: z.string(),
        party: z.string().optional(),
        strength: z.string().optional(),
        weakness: z.string().optional(),
        notes: z.string().optional(),
        threatLevel: z.string().optional(),
      }))
      .mutation(async ({ input, ctx }) => {
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
    incidents: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getWarRoomIncidents(input.profileId)),
    addIncident: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        severity: z.enum(["low", "medium", "high", "critical"]),
        description: z.string(),
        lga: z.string().optional(),
        pollingUnit: z.string().optional(),
      }))
      .mutation(async ({ input }) => {
        const incident = await db.addWarRoomIncident(input as any);
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
      .mutation(async ({ input }) => {
        const result = await db.updateIncidentStatus(input.id, input.status as any);
        if (input.profileId) broadcastWarRoomUpdate(input.profileId);
        return result;
      }),
    agents: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getFieldAgents(input.profileId)),
    upsertAgent: protectedProcedure
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
      .mutation(({ input }) => db.upsertFieldAgent(input as any)),
  }),
  // ─── Election Results ──────────────────────────────────────────────────────
  results: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getElectionResults(input.profileId)),
    add: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        candidateName: z.string(),
        party: z.string(),
        lga: z.string().optional(),
        ward: z.string().optional(),
        votes: z.number(),
        reportedAt: z.string().optional(),
      }))
      .mutation(({ input }) => db.upsertElectionResult(input as any)),
  }),
  // ─── Manifesto ─────────────────────────────────────────────────────────────
  manifesto: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getManifestoSections(input.profileId)),
    upsert: protectedProcedure
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
      .mutation(({ input }) => db.deleteManifestoSection(input.id)),
  }),
  // ─── Petitions ─────────────────────────────────────────────────────────────
  petitions: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getPetitions(input.profileId)),
    create: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        title: z.string(),
        description: z.string().optional(),
        targetSignatures: z.number().optional(),
      }))
      .mutation(({ input }) => db.createPetition(input as any)),
    signatures: protectedProcedure
      .input(z.object({ petitionId: z.number() }))
      .query(({ input }) => db.getPetitionSignatures(input.petitionId)),
    signatureCount: protectedProcedure
      .input(z.object({ petitionId: z.number() }))
      .query(({ input }) => db.getPetitionSignatureCount(input.petitionId)),
    sign: protectedProcedure
      .input(z.object({
        petitionId: z.number(),
        signerName: z.string(),
        signerPhone: z.string().optional(),
        signerLga: z.string().optional(),
      }))
      .mutation(({ input }) => db.addPetitionSignature(input as any)),
    getPublic: publicProcedure
      .input(z.object({ petitionId: z.number() }))
      .query(async ({ input }) => {
        const petition = await db.getPetitionById(input.petitionId);
        if (!petition) return null;
        const count = await db.getPetitionSignatureCount(input.petitionId);
        return { ...petition, signatureCount: count };
      }),
    publicSign: publicProcedure
      .input(z.object({
        petitionId: z.number(),
        signerName: z.string().min(2),
        signerPhone: z.string().optional(),
        signerLga: z.string().optional(),
        signerEmail: z.string().email().optional(),
      }))
      .mutation(({ input }) => db.addPetitionSignature(input as any)),
  }),
  // ─── Diaspora ──────────────────────────────────────────────────────────────
  diaspora: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getDiasporaContacts(input.profileId)),
    add: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        fullName: z.string(),
        country: z.string().optional(),
        city: z.string().optional(),
        organization: z.string().optional(),
        phone: z.string().optional(),
        email: z.string().optional(),
        status: z.string().optional(),
        notes: z.string().optional(),
      }))
      .mutation(({ input }) => db.addDiasporaContact(input as any)),
    aiDraft: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        contactName: z.string(),
        country: z.string(),
        city: z.string().optional(),
        messageType: z.enum(["whatsapp", "email"]),
        candidateName: z.string().optional(),
        partyName: z.string().optional(),
        keyMessage: z.string().optional(),
      }))
      .mutation(async ({ input }) => {
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
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getEndorsements(input.profileId)),
    add: protectedProcedure
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
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getFundraisingTransactions(input.profileId)),
    add: protectedProcedure
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
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getBudgetItems(input.profileId)),
    upsert: protectedProcedure
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
      .mutation(({ input }) => db.deleteBudgetItem(input.id)),
  }),
  // ─── Media Monitoring ──────────────────────────────────────────────────────
  media: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getMediaItems(input.profileId)),
    add: protectedProcedure
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
      .mutation(async ({ input }) => {
        const dbConn = await db.getDb();
        if (!dbConn) return null;
        const { mediaItems } = await import("../drizzle/schema");
        const { eq } = await import("drizzle-orm");
        await dbConn.delete(mediaItems).where(eq(mediaItems.id, input.id));
        return { success: true };
      }),
  }),
  // ─── Debate Coach ──────────────────────────────────────────────────────────
  debate: router({
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getDebatePrepNotes(input.profileId)),
    upsert: protectedProcedure
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
    aiPrep: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        topic: z.string(),
        opponentName: z.string().optional(),
        opponentWeaknesses: z.array(z.string()).optional(),
        opponentStrengths: z.array(z.string()).optional(),
        candidateName: z.string().optional(),
        partyName: z.string().optional(),
      }))
      .mutation(async ({ input }) => {
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
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getDebatePracticeScores(input.profileId)),
    add: protectedProcedure
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
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getStakeholderContacts(input.profileId)),
    upsert: protectedProcedure
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
      .mutation(({ input }) => db.deleteStakeholderContact(input.id)),
  }),
  // ─── Simulation ────────────────────────────────────────────────────────────
  simulation: router({
    history: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getSimulationRuns(input.profileId)),
    save: protectedProcedure
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
        scenario: z.string(),
        stateCode: z.string().optional(),
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
        disruptions: z.array(z.string()),
      }))
      .mutation(async ({ input }) => {
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
    kpis: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getDashboardKPIs(input.profileId)),
    electionDate: protectedProcedure
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
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getCampaignMembers(input.profileId)),
    myRole: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ ctx, input }) => db.getMyRoleForProfile(input.profileId, ctx.user.id)),
    invite: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        email: z.string().email(),
        name: z.string(),
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
      .query(({ input }) => db.getMemberByInviteToken(input.token)),
    confirmAccept: protectedProcedure
      .input(z.object({ token: z.string() }))
      .mutation(({ ctx, input }) => db.acceptCampaignInvite(input.token, ctx.user.id)),
    updateRole: protectedProcedure
      .input(z.object({
        memberId: z.number(),
        role: z.enum(["manager", "viewer"]),
      }))
      .mutation(({ input }) => db.updateMemberRole(input.memberId, input.role)),
    remove: protectedProcedure
      .input(z.object({ memberId: z.number() }))
      .mutation(({ input }) => db.removeCampaignMember(input.memberId)),
  }),
  notifications: router({
    // Get current heartbeat job status for deadline alerts
    status: protectedProcedure.query(async ({ ctx }) => {
      try {
        const sessionToken = parseCookie(ctx.req.headers.cookie ?? "")[COOKIE_NAME] ?? "";
        const jobs = await listHeartbeatJobs(sessionToken);
        const alertJob = jobs.jobs.find(j => j.name.startsWith("deadline-alerts-"));
        return { enabled: !!alertJob?.isEnable, job: alertJob ?? null };
      } catch {
        return { enabled: false, job: null };
      }
    }),
    // Enable deadline alert notifications
    enable: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .mutation(async ({ ctx, input }) => {
        const sessionToken = parseCookie(ctx.req.headers.cookie ?? "")[COOKIE_NAME] ?? "";
        const job = await createHeartbeatJob({
          name: `deadline-alerts-${input.profileId}-${ctx.user.id}`,
          cron: "0 0 8 * * *", // Daily 08:00 UTC
          path: "/api/scheduled/deadline-alerts",
          payload: { profileId: input.profileId },
          description: `Daily deadline alerts for profile ${input.profileId}`,
        }, sessionToken);
        return { taskUid: job.taskUid, nextExecutionAt: job.nextExecutionAt };
      }),
    // Disable deadline alert notifications
    disable: protectedProcedure
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
    testAlert: protectedProcedure
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
    draft: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        policyArea: z.string(),
        brief: z.string(),
        tone: z.string().optional(),
      }))
      .mutation(async ({ input, ctx }) => {
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
    list: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .query(({ input }) => db.getVolunteerTasks(input.profileId)),
    create: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        title: z.string(),
        description: z.string().optional(),
        taskType: z.enum(["canvassing", "polling_unit", "data_entry", "logistics", "social_media", "other"]).optional(),
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
      .mutation(({ input }) => db.updateVolunteerTaskStatus(input.id, input.status)),
    delete: protectedProcedure
      .input(z.object({ id: z.number() }))
      .mutation(({ input }) => db.deleteVolunteerTask(input.id)),
  }),
  // ─── Candidate Website Publish ────────────────────────────────────────────
  // ─── Global Seed ──────────────────────────────────────────────────────────
  seed: router({
    all: protectedProcedure
      .input(z.object({ profileId: z.number() }))
      .mutation(async ({ input }) => {
        await db.seedProfileData(input.profileId);
        return { success: true, message: 'All modules seeded with realistic Nigerian election data.' };
      }),
  }),
  candidateWebsite: router({
    publish: protectedProcedure
      .input(z.object({
        profileId: z.number(),
        htmlContent: z.string().max(500_000),
        candidateName: z.string(),
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
