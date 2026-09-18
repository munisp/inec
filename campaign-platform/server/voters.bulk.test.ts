import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("pg", async () => await import("./testkit/fakePg"));

process.env.POSTGRES_URL ||= "postgres://test:test@localhost:5432/test";

import { TRPCError } from "@trpc/server";
import { fakePgState } from "./testkit/fakePg";
import { createCtx, createTestUser } from "./testkit/trpcCtx";
import { appRouter } from "./routers";
import { MAX_BULK_IMPORT_ROWS } from "./db";

// Caller is a manager of profile 1 (membership row present, not the owner).
function routeAsManager() {
  fakePgState.handler = (text) => {
    const sql = text.toLowerCase();
    if (sql.includes('from "candidate_profiles"')) return { rows: [] };
    if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
    return { rows: [] };
  };
}

beforeEach(() => fakePgState.reset());

describe("voters.bulkImport bounds", () => {
  it(`rejects more than ${MAX_BULK_IMPORT_ROWS} rows (BAD_REQUEST) before touching the DB`, async () => {
    routeAsManager();
    const caller = appRouter.createCaller(createCtx(createTestUser()));

    const rows = Array.from({ length: MAX_BULK_IMPORT_ROWS + 1 }, (_, i) => ({
      fullName: `Voter ${i}`,
    }));

    const err = await caller.voters
      .bulkImport({ profileId: 1, rows, dataSource: "event_signup", consentBasis: "consent", consentMethod: "written", purpose: "voter contact for campaign events" })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("BAD_REQUEST");
    // Nothing may have been inserted.
    expect(
      fakePgState.queries.filter(q => q.text.toLowerCase().startsWith("insert ")),
    ).toHaveLength(0);
  });

  it("accepts exactly the maximum row count", async () => {
    fakePgState.handler = (text) => {
      const sql = text.toLowerCase();
      if (sql.includes('from "candidate_profiles"')) return { rows: [] };
      if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
      if (sql.startsWith('insert into "voter_registrations"')) {
        return { rows: [{ id: 1 }] };
      }
      return { rows: [] };
    };
    const caller = appRouter.createCaller(createCtx(createTestUser()));

    const rows = Array.from({ length: MAX_BULK_IMPORT_ROWS }, (_, i) => ({
      fullName: `Voter ${i}`,
    }));

    await expect(
      caller.voters.bulkImport({ profileId: 1, rows, dataSource: "event_signup", consentBasis: "consent", consentMethod: "written", purpose: "voter contact for campaign events" }),
    ).resolves.toBeDefined();
  });
});
