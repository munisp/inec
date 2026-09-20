// W14 audit-fix tests (deep audit 2026-09): SEC/GAP closures.
// Uses the programmable fake `pg` module (same pattern as compliance.test.ts).
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("pg", async () => await import("./testkit/fakePg"));

process.env.POSTGRES_URL ||= "postgres://test:test@localhost:5432/test";

import { TRPCError } from "@trpc/server";
import { fakePgState } from "./testkit/fakePg";
import { createCtx, createTestUser } from "./testkit/trpcCtx";
import { appRouter } from "./routers";

function routeAs(role: "manager" | "viewer" | "owner", extra?: (sql: string) => { rows: Record<string, unknown>[] } | undefined) {
  fakePgState.handler = (text) => {
    const sql = text.toLowerCase();
    const custom = extra?.(sql);
    if (custom) return custom;
    if (sql.includes('from "candidate_profiles"')) return { rows: [] };
    if (sql.includes('from "campaign_members"')) return { rows: [{ role }] };
    return { rows: [] };
  };
}

beforeEach(() => fakePgState.reset());

describe("SEC-1: silentAgents is a manager-scoped mutation", () => {
  it("rejects viewer role (read roles must not mutate campaign state)", async () => {
    routeAs("viewer");
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.warRoom
      .silentAgents({ profileId: 1, thresholdMinutes: 60 })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("FORBIDDEN");
  });

  it("runs for manager", async () => {
    routeAs("manager", (sql) =>
      sql.includes('from "field_agents"') ? { rows: [] } : undefined);
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    await expect(
      caller.warRoom.silentAgents({ profileId: 1, thresholdMinutes: 60 }),
    ).resolves.toEqual([]);
  });
});

describe("SEC-5: petitions.getPublic returns a sanitized projection", () => {
  it("never leaks profileId or internal timestamps to anonymous callers", async () => {
    fakePgState.handler = (text) => {
      const sql = text.toLowerCase();
      if (sql.includes('from "petitions"') && !sql.includes("count")) {
        return {
          rows: [{
            id: 3, profile_id: 99, title: "Public Petition",
            description: "desc", target_signatures: 5000, status: "active",
            created_at: "2026-01-01 00:00:00",
          }],
        };
      }
      if (sql.includes("count")) return { rows: [{ "count(*)::int": 12 }] }; // expression column name (fakePg positional mapping)
      return { rows: [] };
    };
    const caller = appRouter.createCaller(createCtx(null)); // anonymous
    const out = await caller.petitions.getPublic({ petitionId: 3 });
    expect(out).toMatchObject({ id: 3, title: "Public Petition", signatureCount: 12 });
    expect(out).not.toHaveProperty("profileId");
    expect(out).not.toHaveProperty("profile_id");
    expect(out).not.toHaveProperty("createdAt");
    expect(out).not.toHaveProperty("created_at");
  });
});

describe("SEC-6/7: invite hardening", () => {
  it("stores only the sha256 hash of the invite token", async () => {
    const { inviteCampaignMember } = await import("./db");
    let capturedParams: unknown[] = [];
    fakePgState.handler = (text, params) => {
      if (text.toLowerCase().includes('insert into "campaign_members"')) {
        capturedParams = params;
        return { rows: [{ id: 5, profile_id: 1, email: "a@b.cd", name: "A", role: "viewer", invited_at: null, accepted_at: null }] };
      }
      return { rows: [] };
    };
    const result = await inviteCampaignMember({
      profileId: 1, email: "a@b.cd", name: "A", role: "viewer",
    });
    // The returned plaintext token never appears in the stored params.
    const stored = JSON.stringify(capturedParams);
    expect(stored).not.toContain(result.inviteToken);
    // Stored value is a 64-hex sha256 of the plaintext token.
    const { createHash } = await import("crypto");
    expect(stored).toContain(createHash("sha256").update(result.inviteToken).digest("hex"));
  });

  it("treats a NULL invited_at as expired (fail closed)", async () => {
    const { getMemberByInviteToken } = await import("./db");
    fakePgState.handler = (text) => {
      if (text.toLowerCase().includes('from "campaign_members"')) {
        return { rows: [{ id: 5, profile_id: 1, invite_token: "x", invited_at: null, accepted_at: null }] };
      }
      return { rows: [] };
    };
    await expect(getMemberByInviteToken("tok")).resolves.toBeNull();
  });
});

describe("GAP-3: tribunal tracking", () => {
  it("creates and lists election petitions tenant-scoped", async () => {
    routeAs("manager", (sql) => {
      if (sql.includes('insert into "election_petitions"')) {
        return { rows: [{ id: 9, profile_id: 1, election_name: "2027 Gubernatorial", status: "filed", created_at: "2026-09-20 00:00:00", updated_at: "2026-09-20 00:00:00" }] };
      }
      if (sql.includes('from "election_petitions"')) {
        return { rows: [{ id: 9, profile_id: 1, election_name: "2027 Gubernatorial", status: "filed" }] };
      }
      return undefined;
    });
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const created = await caller.tribunal.upsert({
      profileId: 1, electionName: "2027 Gubernatorial", court: "Election Petition Tribunal",
    });
    expect(created).toMatchObject({ id: 9, status: "filed" });
    const list = await caller.tribunal.list({ profileId: 1 });
    expect(list).toHaveLength(1);
  });

  it("delete requires owner role", async () => {
    routeAs("manager");
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.tribunal
      .delete({ profileId: 1, id: 9 })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("FORBIDDEN");
  });
});

describe("GAP-5: donor screening", () => {
  const BASE = {
    profileId: 1, donorName: "Test Donor", amount: 1000,
  };

  it("rejects anonymous donations above the configured limit (default 0)", async () => {
    routeAs("manager");
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.fundraising
      .add({ ...BASE, donorName: undefined, donorType: "anonymous", amount: 500 })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).message).toContain("Anonymous");
    expect(fakePgState.queries.filter(q => q.text.toLowerCase().includes('insert into "fundraising_transactions"'))).toHaveLength(0);
  });

  it("rejects diaspora donations without source attestation", async () => {
    routeAs("manager");
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.fundraising
      .add({ ...BASE, donorType: "diaspora", sourceAttested: false })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).message).toContain("attestation");
  });

  it("accepts attested diaspora donations within the per-donor cap", async () => {
    routeAs("manager", (sql) => {
      if (sql.includes('from fundraising_transactions')) return { rows: [{ total: 0 }] };
      if (sql.includes('insert into "fundraising_transactions"')) {
        return { rows: [{ id: 21, profile_id: 1, amount: 1000, donor_type: "diaspora", source_attested: true, transacted_at: "2026-09-20 00:00:00" }] };
      }
      return undefined;
    });
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const out = await caller.fundraising.add({
      ...BASE, donorType: "diaspora", sourceAttested: true,
    });
    expect(out).toMatchObject({ id: 21 });
  });
});

describe("GAP-6: media compliance gate", () => {
  it("updates compliance status tenant-scoped; invalid status rejected by zod", async () => {
    routeAs("manager", (sql) =>
      sql.includes('update "media_items"')
        ? { rows: [{ id: 4, profile_id: 1, headline: "H", compliance_status: "compliant" }] }
        : undefined);
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const ok = await caller.media.updateCompliance({ profileId: 1, id: 4, status: "compliant" });
    expect(ok).toMatchObject({ complianceStatus: "compliant" });
    const err = await caller.media
      // @ts-expect-error — intentionally invalid status
      .updateCompliance({ profileId: 1, id: 4, status: "whatever" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("BAD_REQUEST");
  });
});

describe("GAP-13: onboarding audit logging", () => {
  it("team.invite writes a campaign_members audit entry", async () => {
    routeAs("manager", (sql) => {
      if (sql.includes('insert into "campaign_members"')) {
        return { rows: [{ id: 5, profile_id: 1, email: "a@b.cd", name: "A", role: "viewer", invited_at: null, accepted_at: null }] };
      }
      if (sql.includes('from "candidate_profiles"') && sql.includes("user_id")) {
        return { rows: [{ id: 1, candidate_name: "C" }] };
      }
      return undefined;
    });
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    await caller.team.invite({
      profileId: 1, email: "a@b.cd", name: "A", role: "viewer",
    });
    const audit = fakePgState.queries.find(q =>
      q.text.toLowerCase().includes('insert into "data_access_audit"'));
    expect(audit).toBeTruthy();
    expect(JSON.stringify(audit!.params)).toContain("campaign_members");
    expect(JSON.stringify(audit!.params)).toContain("invite");
  });
});

describe("GAP-4: finance disclosure report", () => {
  it("computes real aggregates and reports cap headroom", async () => {
    routeAs("viewer", (sql) => {
      if (sql.includes("from budget_items") && sql.includes("sum(spent_amount)")) {
        return { rows: [{ total_spent: 40000000, total_budgeted: 60000000 }] };
      }
      if (sql.includes("from budget_items") && sql.includes("group by category")) {
        return { rows: [{ category: "media", budgeted: 30000000, spent: 25000000 }] };
      }
      if (sql.includes("from fundraising_transactions")) {
        return { rows: [{ total_raised: 55000000, transaction_count: 12, verified_raised: 50000000, diaspora_raised: 0, anonymous_count: 0, unattested_count: 0 }] };
      }
      if (sql.includes('from "budget_statutory_caps"')) {
        return { rows: [{ office: "gubernatorial", cap_amount: 1000000000, notes: "EA 2022 s.88(3)", updated_at: "2026-01-01 00:00:00" }] };
      }
      return undefined;
    });
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const report = await caller.budget.disclosureReport({ profileId: 1, office: "gubernatorial" });
    // The fixture guarantees a cap row + aggregates, so a null report is a
    // genuine failure — assert explicitly (also narrows the type).
    if (!report) throw new Error("disclosureReport returned null despite complete fixtures");
    expect(report.totalSpent).toBe(40000000);
    expect(report.capHeadroom).toBe(960000000);
    expect(report.capBreached).toBe(false);
    expect(report.fundraising.totalRaised).toBe(55000000);
    expect(report.note).toContain("INEC");
  });
});
