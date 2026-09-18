// W13 analytics substrate tests — CA-parity capabilities, consent-gated.
// Uses the programmable fake `pg` module (same pattern as compliance.test.ts).
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("pg", async () => await import("./testkit/fakePg"));

process.env.POSTGRES_URL ||= "postgres://test:test@localhost:5432/test";

import { TRPCError } from "@trpc/server";
import { fakePgState } from "./testkit/fakePg";
import { createCtx, createTestUser } from "./testkit/trpcCtx";
import { appRouter } from "./routers";

function routeAsManager(extra?: (sql: string) => { rows: Record<string, unknown>[] } | undefined) {
  fakePgState.handler = (text) => {
    const sql = text.toLowerCase();
    const custom = extra?.(sql);
    if (custom) return custom;
    if (sql.includes('from "candidate_profiles"')) return { rows: [] };
    if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
    return { rows: [] };
  };
}

const GRANTED_CONSENT = {
  id: 7,
  profile_id: 1,
  subject_table: "survey_panelists",
  subject_id: 1,
  lawful_basis: "consent",
  purpose: "psychographic survey panel",
  consent_method: "written",
  consent_granted: true,
  consented_at: "2026-09-01T00:00:00Z",
  withdrawn_at: null,
  retention_until: null,
  notes: null,
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
};

beforeEach(() => fakePgState.reset());

describe("W13 consent-gated panel enrollment", () => {
  it("rejects enrollment when no consent record exists (fail closed)", async () => {
    routeAsManager(); // consent lookup → empty rows
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.analytics
      .enrollPanelist({ profileId: 1, consentId: 7, fullName: "Panelist A" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("BAD_REQUEST"); // consentId references nothing
    expect(
      fakePgState.queries.filter(q => q.text.toLowerCase().includes('insert into "survey_panelists"')),
    ).toHaveLength(0);
  });

  it("rejects enrollment when the consent has been withdrawn", async () => {
    routeAsManager((sql) =>
      sql.includes('from "consent_records"')
        ? { rows: [{ ...GRANTED_CONSENT, withdrawn_at: "2026-01-01T00:00:00Z" }] }
        : undefined,
    );
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.analytics
      .enrollPanelist({ profileId: 1, consentId: 7, fullName: "Panelist A" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("FORBIDDEN");
  });

  it("rejects enrollment when the consent belongs to another profile (cross-tenant)", async () => {
    // The consent lookup is profile-scoped (WHERE id AND profile_id), so a
    // cross-tenant consent id simply matches nothing — same fail-closed path
    // as a nonexistent consent.
    routeAsManager(); // consent lookup → empty rows
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.analytics
      .enrollPanelist({ profileId: 1, consentId: 7, fullName: "Panelist A" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("BAD_REQUEST");
  });

  it("enrolls a panelist when a granted, active consent exists", async () => {
    routeAsManager((sql) => {
      if (sql.includes('from "consent_records"')) return { rows: [GRANTED_CONSENT] };
      if (sql.includes('insert into "survey_panelists"')) {
        return { rows: [{ id: 11, profile_id: 1, consent_id: 7, full_name: "Panelist A", status: "active", created_at: "2026-09-19T00:00:00Z" }] };
      }
      return undefined;
    });
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const result = await caller.analytics.enrollPanelist({
      profileId: 1, consentId: 7, fullName: "Panelist A", stateCode: "LA",
    });
    expect(result).toMatchObject({ id: 11, consentId: 7 });
  });
});

describe("W13 survey response recording", () => {
  const ACTIVE_PANELIST = { id: 11, profile_id: 1, status: "active", created_at: "2026-09-01T00:00:00Z" };

  it("rejects responses for a withdrawn (inactive) panelist", async () => {
    routeAsManager((sql) =>
      sql.includes('from "survey_panelists"')
        ? { rows: [{ ...ACTIVE_PANELIST, status: "withdrawn" }] }
        : undefined,
    );
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.analytics
      .recordResponses({ profileId: 1, panelistId: 11, instrument: "OCEAN20", responses: [{ itemKey: "E1", score: 4 }] })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect(
      fakePgState.queries.filter(q => q.text.toLowerCase().includes('insert into "survey_responses"')),
    ).toHaveLength(0);
  });

  it("rejects out-of-range Likert scores at the schema level", async () => {
    routeAsManager((sql) =>
      sql.includes('from "survey_panelists"') ? { rows: [ACTIVE_PANELIST] } : undefined,
    );
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.analytics
      .recordResponses({ profileId: 1, panelistId: 11, instrument: "OCEAN20", responses: [{ itemKey: "E1", score: 6 }] })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("BAD_REQUEST");
  });

  it("records valid responses for an active panelist", async () => {
    routeAsManager((sql) => {
      if (sql.includes('from "survey_panelists"')) return { rows: [ACTIVE_PANELIST] };
      if (sql.includes('insert into "survey_responses"')) return { rows: [{ id: 101 }] };
      return undefined;
    });
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const result = await caller.analytics.recordResponses({
      profileId: 1, panelistId: 11, instrument: "OCEAN20", responses: [{ itemKey: "E1", score: 4 }],
    });
    expect(result).toEqual({ inserted: 1 });
  });
});

describe("W13 message testing", () => {
  it("rejects a message test with fewer than two variants", async () => {
    routeAsManager();
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.analytics
      .createMessageTest({
        profileId: 1, name: "Single-variant test",
        variants: [{ label: "A", body: "only one" }],
      })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("BAD_REQUEST");
  });

  it("rejects an invalid event type", async () => {
    routeAsManager();
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.analytics
      // @ts-expect-error — intentionally invalid event type
      .recordEvent({ profileId: 1, variantId: 5, eventType: "click_fraud" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("BAD_REQUEST");
  });

  it("creates a test with variants and records a real event", async () => {
    routeAsManager((sql) => {
      if (sql.includes('insert into "message_tests"')) {
        return { rows: [{ id: 21, profile_id: 1, name: "Rally framing", status: "draft", created_at: null }] };
      }
      if (sql.includes('insert into "message_variants"')) {
        return { rows: [{ id: 31, test_id: 21, label: "A", body: "body A" }] };
      }
      if (sql.includes('from "message_variants"')) {
        return { rows: [{ profile_id: 1 }] }; // tenant check join
      }
      if (sql.includes('insert into "message_events"')) {
        return { rows: [{ id: 41 }] };
      }
      return undefined;
    });
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const test = await caller.analytics.createMessageTest({
      profileId: 1, name: "Rally framing",
      variants: [{ label: "A", body: "body A" }, { label: "B", body: "body B" }],
    });
    expect(test).not.toBeNull();
    expect(test!.test).toMatchObject({ id: 21, name: "Rally framing" });
    expect(test!.variants).toHaveLength(1); // one INSERT … RETURNING round-trip in fake pg
    const ev = await caller.analytics.recordEvent({ profileId: 1, variantId: 31, eventType: "impression" });
    expect(ev).toMatchObject({ id: 41 });
  });
});
