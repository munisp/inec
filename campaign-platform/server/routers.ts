import { z } from "zod";
import { COOKIE_NAME } from "@shared/const";
import { getSessionCookieOptions } from "./_core/cookies";
import { systemRouter } from "./_core/systemRouter";
import { publicProcedure, protectedProcedure, router } from "./_core/trpc";
import { notifyOwner } from "./_core/notification";
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
        return incident;
      }),
    updateIncidentStatus: protectedProcedure
      .input(z.object({ id: z.number(), status: z.string() }))
      .mutation(({ input }) => db.updateIncidentStatus(input.id, input.status as any)),
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
      }))
      .mutation(({ input }) => db.inviteCampaignMember(input)),
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
        const { profileId } = input;
        const dbConn = await db.getDb();
        if (!dbConn) throw new Error("DB unavailable");
        const { eq } = await import("drizzle-orm");
        const {
          timelineEvents, voterRegistrations, volunteers, volunteerTasks,
          pressReleases, socialMediaPosts, complianceItems, oppositionResearch,
          warRoomIncidents, electionResults, manifestoSections, petitions,
          diasporaContacts, endorsements, fundraisingTransactions, budgetItems,
          mediaItems, debatePrepNotes, debatePracticeScores, stakeholderContacts,
          fieldAgents, pollingUnits,
        } = await import("../drizzle/schema");

        // Clear existing seed data for this profile
        await dbConn.delete(timelineEvents).where(eq(timelineEvents.profileId, profileId));
        await dbConn.delete(voterRegistrations).where(eq(voterRegistrations.profileId, profileId));
        await dbConn.delete(volunteers).where(eq(volunteers.profileId, profileId));
        await dbConn.delete(volunteerTasks).where(eq(volunteerTasks.profileId, profileId));
        await dbConn.delete(pressReleases).where(eq(pressReleases.profileId, profileId));
        await dbConn.delete(socialMediaPosts).where(eq(socialMediaPosts.profileId, profileId));
        await dbConn.delete(complianceItems).where(eq(complianceItems.profileId, profileId));
        await dbConn.delete(oppositionResearch).where(eq(oppositionResearch.profileId, profileId));
        await dbConn.delete(warRoomIncidents).where(eq(warRoomIncidents.profileId, profileId));
        await dbConn.delete(electionResults).where(eq(electionResults.profileId, profileId));
        await dbConn.delete(manifestoSections).where(eq(manifestoSections.profileId, profileId));
        await dbConn.delete(diasporaContacts).where(eq(diasporaContacts.profileId, profileId));
        await dbConn.delete(endorsements).where(eq(endorsements.profileId, profileId));
        await dbConn.delete(fundraisingTransactions).where(eq(fundraisingTransactions.profileId, profileId));
        await dbConn.delete(budgetItems).where(eq(budgetItems.profileId, profileId));
        await dbConn.delete(mediaItems).where(eq(mediaItems.profileId, profileId));
        await dbConn.delete(debatePrepNotes).where(eq(debatePrepNotes.profileId, profileId));
        await dbConn.delete(debatePracticeScores).where(eq(debatePracticeScores.profileId, profileId));
        await dbConn.delete(stakeholderContacts).where(eq(stakeholderContacts.profileId, profileId));
        await dbConn.delete(fieldAgents).where(eq(fieldAgents.profileId, profileId));
        await dbConn.delete(pollingUnits).where(eq(pollingUnits.profileId, profileId));
        // Petitions have signatures FK — delete signatures first
        const pList = await dbConn.select().from(petitions).where(eq(petitions.profileId, profileId));
        const { petitionSignatures } = await import("../drizzle/schema");
        for (const p of pList) {
          await dbConn.delete(petitionSignatures).where(eq(petitionSignatures.petitionId, p.id));
        }
        await dbConn.delete(petitions).where(eq(petitions.profileId, profileId));

        const now = new Date();
        const daysFromNow = (d: number) => new Date(now.getTime() + d * 86400000);

        // ── Timeline Events ──────────────────────────────────────────────────
        await dbConn.insert(timelineEvents).values([
          { profileId, title: "Campaign Launch Rally — Kano Sports Complex", eventDate: daysFromNow(-120).toISOString().split("T")[0], category: "Rally", status: "completed", location: "Kano", priority: "critical" },
          { profileId, title: "Submit Nomination Forms to INEC", eventDate: daysFromNow(-90).toISOString().split("T")[0], category: "INEC Deadline", status: "completed", location: "INEC State Office", priority: "critical" },
          { profileId, title: "Governorship Debate — NTA Kano", eventDate: daysFromNow(-60).toISOString().split("T")[0], category: "Debate", status: "completed", location: "NTA Kano Studios", priority: "high" },
          { profileId, title: "Manifesto Public Presentation", eventDate: daysFromNow(-45).toISOString().split("T")[0], category: "Event", status: "completed", location: "Kano State Library", priority: "high" },
          { profileId, title: "LGA Stakeholder Tour — Dala, Gwale, Nassarawa", eventDate: daysFromNow(-30).toISOString().split("T")[0], category: "Outreach", status: "completed", location: "Kano LGAs", priority: "medium" },
          { profileId, title: "Campaign Finance Report Submission", eventDate: daysFromNow(-14).toISOString().split("T")[0], category: "INEC Deadline", status: "completed", location: "INEC Office", priority: "critical" },
          { profileId, title: "Final Mega Rally — Sani Abacha Stadium", eventDate: daysFromNow(3).toISOString().split("T")[0], category: "Rally", status: "active", location: "Kano", priority: "critical" },
          { profileId, title: "Election Day — Governorship Election", eventDate: daysFromNow(14).toISOString().split("T")[0], category: "Election", status: "pending", location: "All 44 LGAs", priority: "critical" },
          { profileId, title: "INEC Result Collation — State HQ", eventDate: daysFromNow(15).toISOString().split("T")[0], category: "INEC", status: "pending", location: "INEC Kano HQ", priority: "critical" },
          { profileId, title: "Victory Press Conference", eventDate: daysFromNow(16).toISOString().split("T")[0], category: "Media", status: "pending", location: "Campaign HQ", priority: "high" },
        ]);

        // ── Voter Registrations ───────────────────────────────────────────────
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
        await dbConn.insert(voterRegistrations).values(voters.map(v => ({ ...v, profileId, stateCode: "KN" })));

        // ── Volunteers ────────────────────────────────────────────────────────
        const volData = [
          { fullName: "Bashir Abdulkadir", phone: "08031111111", lga: "Dala", role: "Ward Coordinator", skills: "Community mobilisation, data entry", status: "active" as const },
          { fullName: "Khadija Musa Sani", phone: "08052222222", lga: "Gwale", role: "Women Leader", skills: "Voter registration, outreach", status: "active" as const },
          { fullName: "Umar Faruk Dankabo", phone: "08073333333", lga: "Nassarawa", role: "Youth Coordinator", skills: "Social media, logistics", status: "active" as const },
          { fullName: "Aisha Bello Kano", phone: "08094444444", lga: "Kumbotso", role: "Polling Agent", skills: "Election monitoring, BVAS operation", status: "active" as const },
          { fullName: "Sani Ibrahim Wada", phone: "08015555555", lga: "Tarauni", role: "Transport Coordinator", skills: "Logistics, vehicle management", status: "active" as const },
          { fullName: "Maryam Garba Tukur", phone: "08036666666", lga: "Fagge", role: "Media Liaison", skills: "Photography, social media", status: "active" as const },
        ];
        const insertedVols = await dbConn.insert(volunteers).values(volData.map(v => ({ ...v, profileId }))).returning();

        // ── Volunteer Tasks ───────────────────────────────────────────────────
        if (insertedVols.length >= 2) {
          await dbConn.insert(volunteerTasks).values([
            { profileId, volunteerId: insertedVols[0].id, title: "Register 200 voters in Dala ward", taskType: "canvassing" as const, status: "completed" as const, dueDate: daysFromNow(-7) },
            { profileId, volunteerId: insertedVols[1].id, title: "Organise women's rally — Gwale", taskType: "canvassing" as const, status: "in_progress" as const, dueDate: daysFromNow(2) },
            { profileId, volunteerId: insertedVols[2].id, title: "Social media content for final week", taskType: "media" as const, status: "in_progress" as const, dueDate: daysFromNow(1) },
            { profileId, volunteerId: insertedVols[3].id, title: "BVAS training — Kumbotso PUs", taskType: "polling_unit" as const, status: "pending" as const, dueDate: daysFromNow(5) },
            { profileId, volunteerId: insertedVols[4].id, title: "Arrange 10 buses for Election Day", taskType: "logistics" as const, status: "pending" as const, dueDate: daysFromNow(12) },
            { profileId, volunteerId: insertedVols[5].id, title: "Prepare press kits for final rally", taskType: "media" as const, status: "completed" as const, dueDate: daysFromNow(-2) },
          ]);
        }

        // ── Press Releases ────────────────────────────────────────────────────
        await dbConn.insert(pressReleases).values([
          { profileId, title: "Candidate Unveils 10-Point Economic Agenda for Kano", body: "The governorship candidate today unveiled a comprehensive 10-point economic agenda aimed at creating 500,000 jobs in Kano State within four years. The plan focuses on agro-processing, technology, and small business development.", status: "active" as const, publishedAt: daysFromNow(-30) },
          { profileId, title: "Campaign Condemns Electoral Violence in Kumbotso", body: "The campaign strongly condemns the reported incidents of electoral violence in Kumbotso LGA and calls on security agencies to ensure a peaceful election environment for all citizens.", status: "active" as const, publishedAt: daysFromNow(-7) },
          { profileId, title: "Final Rally Set for Sani Abacha Stadium", body: "The campaign announces the final mega rally scheduled for Sani Abacha Stadium. Thousands of supporters from all 44 LGAs are expected to attend.", status: "pending" as const },
        ]);

        // ── Social Media Posts ────────────────────────────────────────────────
        const platforms = ["Twitter", "Facebook", "Instagram", "WhatsApp"];
        const smPosts = [
          { platform: "Twitter", content: "Our candidate is committed to transforming Kano State. 500,000 jobs. Better schools. Modern healthcare. Vote wisely on election day! #KanoDecides #VoteRight", status: "active" as const, scheduledAt: daysFromNow(-5), impressions: 45200, engagements: 3100 },
          { platform: "Facebook", content: "Join us at the FINAL MEGA RALLY at Sani Abacha Stadium! Come out in your thousands and show your support. Free buses from all LGAs. #KanoForward", status: "pending" as const, scheduledAt: daysFromNow(2) },
          { platform: "Instagram", content: "Behind the scenes at our campaign headquarters — the team working tirelessly for Kano's future 💚🤍 #TeamKano", status: "active" as const, scheduledAt: daysFromNow(-10), impressions: 12800, engagements: 890 },
          { platform: "WhatsApp", content: "IMPORTANT: Election day is in 14 days. Make sure your PVC is ready. Know your polling unit. Vote early. Share this message!", status: "active" as const, scheduledAt: daysFromNow(-3), impressions: 8900, engagements: 2400 },
          { platform: "Twitter", content: "Debate performance recap: Our candidate dominated on education, healthcare, and security. Watch the full debate on our YouTube channel. #KanoDebate", status: "active" as const, scheduledAt: daysFromNow(-60), impressions: 67000, engagements: 5200 },
          { platform: "Facebook", content: "Manifesto highlight: We will build 100 new primary health centres across all 44 LGAs in Kano State. Healthcare is a right, not a privilege.", status: "active" as const, scheduledAt: daysFromNow(-45), impressions: 23400, engagements: 1800 },
          { platform: "Instagram", content: "Voter registration drive SUCCESS! Our volunteers registered 12,000 new voters in the past month. Thank you to our incredible team! 🙏", status: "active" as const, scheduledAt: daysFromNow(-20), impressions: 9800, engagements: 1200 },
          { platform: "Twitter", content: "Election day countdown: 14 days to go. Are you ready? Check your polling unit at INEC portal. Your vote is your power! #PVCPower", status: "pending" as const, scheduledAt: daysFromNow(1) },
        ];
        await dbConn.insert(socialMediaPosts).values(smPosts.map(p => ({ ...p, profileId })));

        // ── Compliance Items ──────────────────────────────────────────────────
        await dbConn.insert(complianceItems).values([
          { profileId, title: "Nomination Form Submission (Form CF001)", category: "INEC Filing", description: "Submit completed nomination forms to INEC state office", status: "compliant" as const, deadline: daysFromNow(-90).toISOString().split("T")[0], notes: "Submitted on time. Receipt No: INEC/KN/2025/0234" },
          { profileId, title: "Campaign Finance Account Opening", category: "Finance", description: "Open dedicated campaign finance account as required by Electoral Act 2022", status: "compliant" as const, deadline: daysFromNow(-80).toISOString().split("T")[0], notes: "Account opened at Zenith Bank. Account No: 1234567890" },
          { profileId, title: "Interim Campaign Finance Report", category: "Finance", description: "Submit interim report of campaign expenditure to INEC", status: "compliant" as const, deadline: daysFromNow(-14).toISOString().split("T")[0], notes: "Submitted. Total expenditure declared: ₦450M" },
          { profileId, title: "Final Campaign Finance Report", category: "Finance", description: "Submit final campaign finance report within 6 months of election", status: "pending" as const, deadline: daysFromNow(180).toISOString().split("T")[0] },
          { profileId, title: "Campaign Materials INEC Clearance", category: "Materials", description: "Ensure all campaign materials comply with INEC guidelines", status: "compliant" as const, deadline: daysFromNow(-60).toISOString().split("T")[0], notes: "All materials cleared" },
          { profileId, title: "Polling Agent Accreditation", category: "Election Day", description: "Submit list of polling agents to INEC for accreditation", status: "warning" as const, deadline: daysFromNow(7).toISOString().split("T")[0], notes: "Pending submission for 12 LGAs" },
          { profileId, title: "Campaign Spending Cap Compliance", category: "Finance", description: "Ensure total campaign spending does not exceed INEC cap of ₦1.2B for governorship", status: "warning" as const, deadline: daysFromNow(14).toISOString().split("T")[0], notes: "Currently at ₦890M — approaching limit" },
          { profileId, title: "Code of Conduct Signatory", category: "Ethics", description: "Sign INEC Code of Conduct for candidates", status: "compliant" as const, deadline: daysFromNow(-100).toISOString().split("T")[0], notes: "Signed at INEC office" },
        ]);

        // ── Opposition Research ───────────────────────────────────────────────
        await dbConn.insert(oppositionResearch).values([
          { profileId, opponentName: "Alhaji Kabiru Rufa'i", party: "APC", strength: "Incumbent advantage, federal connections, strong northern bloc support", weakness: "Poor infrastructure delivery record, corruption allegations, weak youth appeal", keyIssues: ["Security failures", "Education underfunding", "Unemployment"], threatLevel: "critical" as const, notes: "Main opponent. Has incumbency advantage and federal backing." },
          { profileId, opponentName: "Dr. Amina Bello", party: "Labour Party", strength: "Strong women and youth base, clean reputation, social media presence", weakness: "Limited LGA structure, low rural penetration, funding constraints", keyIssues: ["Women empowerment", "Youth unemployment", "Healthcare"], threatLevel: "medium" as const, notes: "Growing threat especially in urban areas." },
          { profileId, opponentName: "Engr. Sani Danladi", party: "NNPP", strength: "Local Kano roots, anti-establishment narrative, grassroots support", weakness: "Weak party structure outside Kano Municipal, no federal backing", keyIssues: ["Anti-corruption", "Local government autonomy"], threatLevel: "medium" as const, notes: "Could split the opposition vote." },
        ]);

        // ── War Room Incidents ────────────────────────────────────────────────
        await dbConn.insert(warRoomIncidents).values([
          { profileId, reportedBy: "Agent Musa Dala", lga: "Dala", ward: "Dala Central", incidentType: "Ballot Stuffing Attempt", description: "Suspected ballot stuffing at Dala Primary School PU. 3 individuals apprehended by security.", severity: "high" as const, status: "resolved" as const, reportedAt: daysFromNow(-7) },
          { profileId, reportedBy: "Agent Fatima Gwale", lga: "Gwale", ward: "Gwale North", incidentType: "Voter Intimidation", description: "Armed individuals seen near polling unit intimidating voters. Police notified.", severity: "critical" as const, status: "escalated" as const, reportedAt: daysFromNow(-3) },
          { profileId, reportedBy: "Agent Umar Nassarawa", lga: "Nassarawa", ward: "Nassarawa East", incidentType: "BVAS Malfunction", description: "BVAS device at Nassarawa Secondary School PU not functioning. INEC technician requested.", severity: "medium" as const, status: "open" as const, reportedAt: daysFromNow(-1) },
          { profileId, reportedBy: "HQ Observer", lga: "Kumbotso", ward: "Kumbotso Central", incidentType: "Late Materials Arrival", description: "Ballot papers arrived 2 hours late at Kumbotso Primary School. Voting delayed.", severity: "medium" as const, status: "resolved" as const, reportedAt: daysFromNow(-14) },
        ]);

        // ── Election Results ──────────────────────────────────────────────────
        const lgas = ["Dala", "Gwale", "Nassarawa", "Kumbotso", "Tarauni", "Fagge", "Municipal", "Ungogo", "Kano Municipal", "Kura"];
        const candidates = ["Our Candidate (PDP)", "Alhaji Kabiru Rufa'i (APC)", "Dr. Amina Bello (LP)"];
        const resultData: any[] = [];
        lgas.forEach(lga => {
          const base = Math.floor(Math.random() * 5000) + 8000;
          resultData.push({ profileId, lga, candidateName: candidates[0], party: "PDP", votes: base + Math.floor(Math.random() * 3000), isProjected: true });
          resultData.push({ profileId, lga, candidateName: candidates[1], party: "APC", votes: base - Math.floor(Math.random() * 2000), isProjected: true });
          resultData.push({ profileId, lga, candidateName: candidates[2], party: "LP", votes: Math.floor(Math.random() * 2000) + 500, isProjected: true });
        });
        await dbConn.insert(electionResults).values(resultData);

        // ── Manifesto Sections ────────────────────────────────────────────────
        await dbConn.insert(manifestoSections).values([
          { profileId, sectionTitle: "Economic Development & Job Creation", summary: "Create 500,000 jobs through agro-processing, technology hubs, and SME support", commitments: ["Establish 5 industrial parks", "₦50B SME fund", "Tech hub in Kano City"], timeline: "Year 1-2", budget: "₦120B", priority: "critical" as const, sortOrder: 1 },
          { profileId, sectionTitle: "Education Reform", summary: "Rebuild 2,000 schools and provide free education from primary to JSS3", commitments: ["Free education JSS1-3", "2,000 school renovations", "10,000 teacher recruitment"], timeline: "Year 1-4", budget: "₦80B", priority: "critical" as const, sortOrder: 2 },
          { profileId, sectionTitle: "Healthcare Transformation", summary: "Build 100 primary health centres and upgrade 5 general hospitals", commitments: ["100 new PHCs", "Free maternal care", "Medical equipment upgrade"], timeline: "Year 1-3", budget: "₦60B", priority: "high" as const, sortOrder: 3 },
          { profileId, sectionTitle: "Security & Rule of Law", summary: "Strengthen security architecture and community policing", commitments: ["1,000 community police recruits", "CCTV in major cities", "Security trust fund"], timeline: "Year 1", budget: "₦40B", priority: "critical" as const, sortOrder: 4 },
          { profileId, sectionTitle: "Agriculture & Food Security", summary: "Modernise Kano's agricultural sector and reduce food prices", commitments: ["Irrigation expansion", "Fertiliser subsidy", "Commodity exchange"], timeline: "Year 1-2", budget: "₦50B", priority: "high" as const, sortOrder: 5 },
        ]);

        // ── Petitions ─────────────────────────────────────────────────────────
        const [petition] = await dbConn.insert(petitions).values([
          { profileId, title: "Support Free Education in Kano State", description: "We call on the next governor of Kano State to implement free education from primary to JSS3 level for all Kano children.", targetSignatures: 50000, status: "active" as const },
        ]).returning();
        if (petition) {
          const { petitionSignatures } = await import("../drizzle/schema");
          await dbConn.insert(petitionSignatures).values([
            { petitionId: petition.id, signerName: "Aminu Kano", phone: "08031111111", lga: "Dala", signedAt: daysFromNow(-20) },
            { petitionId: petition.id, signerName: "Fatima Sani", phone: "08052222222", lga: "Gwale", signedAt: daysFromNow(-18) },
            { petitionId: petition.id, signerName: "Musa Wada", phone: "08073333333", lga: "Nassarawa", signedAt: daysFromNow(-15) },
            { petitionId: petition.id, signerName: "Hauwa Ibrahim", phone: "08094444444", lga: "Kumbotso", signedAt: daysFromNow(-12) },
            { petitionId: petition.id, signerName: "Kabiru Danladi", phone: "08015555555", lga: "Tarauni", signedAt: daysFromNow(-10) },
          ]);
        }

        // ── Diaspora Contacts ─────────────────────────────────────────────────
        await dbConn.insert(diasporaContacts).values([
          { profileId, name: "Dr. Usman Abdullahi", country: "United Kingdom", city: "London", email: "usman.a@gmail.com", phone: "+447911123456", organization: "Kano UK Association", status: "active" as const, pledgedAmount: 5000000 },
          { profileId, name: "Hajiya Maryam Sule", country: "United States", city: "Houston", email: "maryam.s@yahoo.com", phone: "+17135551234", organization: "Nigerians in Houston", status: "active" as const, pledgedAmount: 3000000 },
          { profileId, name: "Alhaji Bello Kano", country: "Saudi Arabia", city: "Jeddah", email: "bello.k@hotmail.com", phone: "+966551234567", organization: "Nigerian Muslim Community Jeddah", status: "active" as const, pledgedAmount: 8000000 },
          { profileId, name: "Engr. Sani Musa", country: "Canada", city: "Toronto", email: "sani.m@gmail.com", phone: "+14165551234", organization: "Kano Canada Network", status: "active" as const, pledgedAmount: 2000000 },
          { profileId, name: "Prof. Aisha Garba", country: "Germany", city: "Frankfurt", email: "aisha.g@uni-frankfurt.de", phone: "+4969551234", organization: "African Academics Germany", status: "active" as const, pledgedAmount: 1500000 },
          { profileId, name: "Alhaji Kabiru Shehu", country: "United Arab Emirates", city: "Dubai", email: "kabiru.s@gmail.com", phone: "+971501234567", organization: "Nigerian Business Council Dubai", status: "active" as const, pledgedAmount: 10000000 },
        ]);

        // ── Endorsements ──────────────────────────────────────────────────────
        await dbConn.insert(endorsements).values([
          { profileId, endorserName: "Alhaji Aminu Dantata", title: "Business Mogul", organization: "Dantata Group", category: "Business", statement: "I endorse this candidate because of his clear vision for economic development in Kano State.", isPublic: true },
          { profileId, endorserName: "Dr. Fatima Aliyu", title: "Chairman, Kano Medical Association", organization: "NMA Kano", category: "Professional", statement: "As a healthcare professional, I am confident this candidate will transform our health sector.", isPublic: true },
          { profileId, endorserName: "Mallam Ibrahim Sani", title: "Traditional Title Holder", organization: "Kano Emirate", category: "Traditional", statement: "The emirate council supports peaceful elections and commends this candidate's respect for tradition.", isPublic: true },
          { profileId, endorserName: "Hajiya Zainab Umar", title: "President, Kano Market Women Association", organization: "KMWA", category: "Civil Society", statement: "Our market women strongly support this candidate who has promised to reduce business taxes.", isPublic: true },
          { profileId, endorserName: "Comrade Usman Bello", title: "Chairman, NLC Kano", organization: "NLC Kano State", category: "Labour", statement: "Workers in Kano State deserve better wages and working conditions. We endorse this candidate.", isPublic: true },
        ]);

        // ── Fundraising ───────────────────────────────────────────────────────
        await dbConn.insert(fundraisingTransactions).values([
          { profileId, donorName: "Alhaji Kabiru Shehu", amount: 50000000, source: "Individual", category: "Major Donor", notes: "Diaspora donation via bank transfer", isVerified: true },
          { profileId, donorName: "Kano Business Forum", amount: 30000000, source: "Corporate", category: "Corporate", notes: "Corporate donation from business association", isVerified: true },
          { profileId, donorName: "Dantata Group", amount: 20000000, source: "Corporate", category: "Corporate", notes: "Corporate endorsement donation", isVerified: true },
          { profileId, donorName: "Diaspora Network UK", amount: 15000000, source: "Diaspora", category: "Diaspora", notes: "Collective donation from UK diaspora", isVerified: true },
          { profileId, donorName: "Anonymous", amount: 5000000, source: "Individual", category: "Individual", notes: "Cash donation at rally", isVerified: false },
          { profileId, donorName: "Youth Support Fund", amount: 2000000, source: "Grassroots", category: "Grassroots", notes: "Crowdfunding campaign proceeds", isVerified: true },
          { profileId, donorName: "Kano Traders Union", amount: 8000000, source: "Corporate", category: "Corporate", notes: "Traders association contribution", isVerified: true },
          { profileId, donorName: "Diaspora Network USA", amount: 12000000, source: "Diaspora", category: "Diaspora", notes: "Houston and New York diaspora", isVerified: true },
        ]);

        // ── Budget Items ──────────────────────────────────────────────────────
        await dbConn.insert(budgetItems).values([
          { profileId, category: "Rallies & Events", description: "Campaign rallies across 44 LGAs", budgetedAmount: 150000000, spentAmount: 120000000, priority: "critical" as const },
          { profileId, category: "Media & Advertising", description: "TV, radio, print and digital advertising", budgetedAmount: 100000000, spentAmount: 85000000, priority: "critical" as const },
          { profileId, category: "Logistics & Transport", description: "Vehicle hire, fuel, and logistics", budgetedAmount: 80000000, spentAmount: 65000000, priority: "high" as const },
          { profileId, category: "Campaign Materials", description: "Posters, T-shirts, caps, banners", budgetedAmount: 60000000, spentAmount: 55000000, priority: "high" as const },
          { profileId, category: "Staff & Volunteers", description: "Stipends, allowances, and training", budgetedAmount: 50000000, spentAmount: 40000000, priority: "high" as const },
          { profileId, category: "Security", description: "Private security and intelligence", budgetedAmount: 40000000, spentAmount: 30000000, priority: "critical" as const },
          { profileId, category: "Legal & Compliance", description: "Legal fees, INEC filings, compliance", budgetedAmount: 20000000, spentAmount: 15000000, priority: "medium" as const },
          { profileId, category: "Technology & Data", description: "Campaign software, data analytics, IT", budgetedAmount: 15000000, spentAmount: 12000000, priority: "medium" as const },
          { profileId, category: "Voter Education", description: "Voter registration drives, education", budgetedAmount: 25000000, spentAmount: 18000000, priority: "high" as const },
          { profileId, category: "Contingency", description: "Emergency and unforeseen expenses", budgetedAmount: 30000000, spentAmount: 5000000, priority: "low" as const },
        ]);

        // ── Media Items ───────────────────────────────────────────────────────
        await dbConn.insert(mediaItems).values([
          { profileId, source: "Daily Trust", headline: "Kano Governorship: PDP Candidate Unveils Bold Economic Plan", sentiment: "positive", sourceType: "print", reach: 450000, zone: "North-West", publishedAt: daysFromNow(-30) },
          { profileId, source: "Channels TV", headline: "Debate Analysis: PDP Candidate Scores High on Security", sentiment: "positive", sourceType: "broadcast", reach: 2500000, zone: "National", publishedAt: daysFromNow(-60) },
          { profileId, source: "The Punch", headline: "Kano Election: Tension Mounts as Candidates Trade Accusations", sentiment: "negative", sourceType: "print", reach: 800000, zone: "National", publishedAt: daysFromNow(-7) },
          { profileId, source: "NAN", headline: "INEC Confirms Kano Governorship Election Date", sentiment: "neutral", sourceType: "online", reach: 300000, zone: "National", publishedAt: daysFromNow(-45) },
          { profileId, source: "Arewa Voice", headline: "PDP Candidate Promises Free Education — Kano Voters React", sentiment: "positive", sourceType: "online", reach: 180000, zone: "North-West", publishedAt: daysFromNow(-25) },
          { profileId, source: "Vanguard", headline: "Kano Campaign Finance: Who Is Spending What?", sentiment: "neutral", sourceType: "print", reach: 600000, zone: "National", publishedAt: daysFromNow(-14) },
          { profileId, source: "BBC Hausa", headline: "Zaɓen Gwamna Kano: Yan Takara Sun Gabatar da Shirye-shiryen", sentiment: "neutral", sourceType: "broadcast", reach: 5000000, zone: "National", publishedAt: daysFromNow(-20) },
          { profileId, source: "Twitter/X", headline: "#KanoDecides trends as election day approaches", sentiment: "positive", sourceType: "social", reach: 120000, zone: "National", publishedAt: daysFromNow(-5) },
          { profileId, source: "Leadership Newspaper", headline: "PDP Candidate Leads in Latest Kano Poll — 48% Support", sentiment: "positive", sourceType: "print", reach: 350000, zone: "North-West", publishedAt: daysFromNow(-10) },
          { profileId, source: "Sahara Reporters", headline: "Allegations of Vote Buying in Kano Governorship Race", sentiment: "negative", sourceType: "online", reach: 900000, zone: "National", publishedAt: daysFromNow(-3) },
        ]);

        // ── Debate Prep Notes ─────────────────────────────────────────────────
        await dbConn.insert(debatePrepNotes).values([
          { profileId, topic: "Security & Banditry", keyMessage: "We will establish 1,000 community policing units and a ₦10B security trust fund", counterArguments: ["Banditry is a federal issue", "State police is unconstitutional"], statistics: ["Kano recorded 234 security incidents in 2024", "Community policing reduced crime by 40% in Lagos"], practiceScore: 8 },
          { profileId, topic: "Education", keyMessage: "Free education from primary to JSS3, rebuild 2,000 schools", counterArguments: ["State cannot afford free education", "Quality over quantity"], statistics: ["Kano has 1.2M out-of-school children", "Education budget is only 8% of state budget"], practiceScore: 9 },
          { profileId, topic: "Economy & Jobs", keyMessage: "500,000 jobs through agro-processing and tech hubs", counterArguments: ["Job creation is federal responsibility", "Private sector drives employment"], statistics: ["Kano unemployment rate is 34%", "Agro-processing can create 200,000 jobs"], practiceScore: 7 },
          { profileId, topic: "Healthcare", keyMessage: "100 new PHCs, free maternal care, hospital upgrades", counterArguments: ["Healthcare is underfunded nationally", "Doctors are leaving Nigeria"], statistics: ["Kano has 1 doctor per 10,000 people", "Maternal mortality rate is 800 per 100,000"], practiceScore: 8 },
        ]);

        // ── Debate Practice Scores ────────────────────────────────────────────
        await dbConn.insert(debatePracticeScores).values([
          { profileId, topic: "Security & Banditry", score: 7, maxScore: 10, notes: "Good on community policing, needs stronger data", scoredAt: daysFromNow(-45) },
          { profileId, topic: "Education", score: 9, maxScore: 10, notes: "Excellent delivery, compelling statistics", scoredAt: daysFromNow(-40) },
          { profileId, topic: "Economy & Jobs", score: 6, maxScore: 10, notes: "Needs more specific job creation metrics", scoredAt: daysFromNow(-35) },
          { profileId, topic: "Healthcare", score: 8, maxScore: 10, notes: "Strong on PHC numbers, improve on specialist care", scoredAt: daysFromNow(-30) },
          { profileId, topic: "Security & Banditry", score: 8, maxScore: 10, notes: "Improved significantly after coaching", scoredAt: daysFromNow(-20) },
          { profileId, topic: "Economy & Jobs", score: 8, maxScore: 10, notes: "Much better with tech hub specifics", scoredAt: daysFromNow(-15) },
          { profileId, topic: "Education", score: 9, maxScore: 10, notes: "Consistent high performance", scoredAt: daysFromNow(-10) },
          { profileId, topic: "Healthcare", score: 9, maxScore: 10, notes: "Best performance yet", scoredAt: daysFromNow(-5) },
        ]);

        // ── Stakeholder Contacts ──────────────────────────────────────────────
        await dbConn.insert(stakeholderContacts).values([
          { profileId, name: "Alhaji Aminu Dantata", title: "Business Mogul", organization: "Dantata Group", category: "Business", phone: "08031234567", state: "Kano", lga: "Municipal", influenceLevel: "critical" as const, relationship: "supporter", nextAction: "Confirm attendance at final rally" },
          { profileId, name: "Emir of Kano", title: "His Royal Highness", organization: "Kano Emirate", category: "Traditional", state: "Kano", influenceLevel: "critical" as const, relationship: "neutral", nextAction: "Request audience before election day" },
          { profileId, name: "Dr. Fatima Aliyu", title: "Chairman, NMA Kano", organization: "Nigerian Medical Association", category: "Professional", phone: "08052345678", email: "fatima.a@nma.org", state: "Kano", influenceLevel: "high" as const, relationship: "supporter" },
          { profileId, name: "Comrade Usman Bello", title: "NLC Kano Chairman", organization: "NLC Kano", category: "Labour", phone: "08073456789", state: "Kano", influenceLevel: "high" as const, relationship: "supporter" },
          { profileId, name: "Hajiya Zainab Umar", title: "President, KMWA", organization: "Kano Market Women Association", category: "Civil Society", phone: "08094567890", state: "Kano", influenceLevel: "high" as const, relationship: "supporter" },
          { profileId, name: "Bishop Emmanuel Okafor", title: "Bishop", organization: "Catholic Diocese of Kano", category: "Religious", phone: "08015678901", state: "Kano", influenceLevel: "medium" as const, relationship: "neutral", nextAction: "Invite to interfaith dialogue" },
          { profileId, name: "Alhaji Musa Kwankwaso", title: "Former Governor", organization: "NNPP", category: "Political", state: "Kano", influenceLevel: "critical" as const, relationship: "opponent", nextAction: "Monitor campaign activities" },
          { profileId, name: "Prof. Abdullahi Usman", title: "Vice Chancellor", organization: "Bayero University Kano", category: "Academia", phone: "08036789012", email: "vc@buk.edu.ng", state: "Kano", influenceLevel: "medium" as const, relationship: "neutral" },
        ]);

        // ── Field Agents ──────────────────────────────────────────────────────
        await dbConn.insert(fieldAgents).values([
          { profileId, name: "Musa Dala", phone: "08031111111", assignedPu: "DALA PRIMARY SCHOOL", lga: "Dala", agentStatus: "active" as const, votersCounted: 342 },
          { profileId, name: "Fatima Gwale", phone: "08052222222", assignedPu: "GWALE MODEL PRIMARY", lga: "Gwale", agentStatus: "active" as const, votersCounted: 289 },
          { profileId, name: "Umar Nassarawa", phone: "08073333333", assignedPu: "NASSARAWA SEC SCHOOL", lga: "Nassarawa", agentStatus: "sos" as const, votersCounted: 156 },
          { profileId, name: "Aisha Kumbotso", phone: "08094444444", assignedPu: "KUMBOTSO PRIMARY", lga: "Kumbotso", agentStatus: "active" as const, votersCounted: 412 },
        ]);

        // ── Polling Units ─────────────────────────────────────────────────────
        await dbConn.insert(pollingUnits).values([
          { profileId, puCode: "KN/01/01/001", name: "DALA PRIMARY SCHOOL", ward: "Dala Central", lga: "Dala", stateCode: "KN", lat: 12.0022, lng: 8.5919, registeredVoters: 842, agentAssigned: "Musa Dala", agentPhone: "08031111111" },
          { profileId, puCode: "KN/02/01/001", name: "GWALE MODEL PRIMARY SCHOOL", ward: "Gwale North", lga: "Gwale", stateCode: "KN", lat: 11.9980, lng: 8.5150, registeredVoters: 654, agentAssigned: "Fatima Gwale", agentPhone: "08052222222" },
          { profileId, puCode: "KN/03/01/001", name: "NASSARAWA SECONDARY SCHOOL", ward: "Nassarawa East", lga: "Nassarawa", stateCode: "KN", lat: 12.0100, lng: 8.5300, registeredVoters: 1120, agentAssigned: "Umar Nassarawa", agentPhone: "08073333333" },
          { profileId, puCode: "KN/04/01/001", name: "KUMBOTSO PRIMARY SCHOOL", ward: "Kumbotso Central", lga: "Kumbotso", stateCode: "KN", lat: 12.0500, lng: 8.4800, registeredVoters: 780, agentAssigned: "Aisha Kumbotso", agentPhone: "08094444444" },
          { profileId, puCode: "KN/05/01/001", name: "TARAUNI TOWN HALL", ward: "Tarauni South", lga: "Tarauni", stateCode: "KN", lat: 12.0200, lng: 8.5600, registeredVoters: 920 },
          { profileId, puCode: "KN/06/01/001", name: "FAGGE PRIMARY SCHOOL", ward: "Fagge D2", lga: "Fagge", stateCode: "KN", lat: 11.9900, lng: 8.5200, registeredVoters: 560 },
        ]);

        return { success: true, message: "All modules seeded with realistic Nigerian election data." };
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
