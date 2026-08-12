import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("pg", async () => await import("./testkit/fakePg"));

process.env.POSTGRES_URL ||= "postgres://test:test@localhost:5432/test";

import { TRPCError } from "@trpc/server";
import { fakePgState } from "./testkit/fakePg";
import { createCtx, createTestUser } from "./testkit/trpcCtx";
import { appRouter } from "./routers";

// The caller is a MANAGER of profile 1 but owns nothing: ownership lookup
// returns no rows, membership lookup returns the manager row.
function routeAsManagerOfProfile1() {
  fakePgState.handler = (text) => {
    const sql = text.toLowerCase();
    if (sql.includes('from "candidate_profiles"')) return { rows: [] };
    if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
    // The tenant-guarded UPDATE matches zero rows for a cross-tenant row id —
    // exactly what Postgres would do for `WHERE id = ? AND profile_id = ?`
    // when the row belongs to another campaign.
    if (sql.startsWith('update "timeline_events"')) return { rows: [] };
    return { rows: [] };
  };
}

beforeEach(() => fakePgState.reset());

describe("tenancy-guarded updates", () => {
  it("returns NOT_FOUND when updating a row id that belongs to another tenant", async () => {
    routeAsManagerOfProfile1();
    const caller = appRouter.createCaller(createCtx(createTestUser()));

    // timeline.upsert with id=42 + profileId=1: the membership check passes
    // (manager of profile 1), but the row id belongs to another campaign, so
    // the guarded UPDATE matches nothing and must surface NOT_FOUND — never a
    // silent no-op, never the other tenant's row.
    const err = await caller.timeline
      .upsert({ id: 42, profileId: 1, title: "Rally", eventDate: "2027-02-01" })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("NOT_FOUND");
  });

  it("returns FORBIDDEN before any write when the user has no membership at all", async () => {
    fakePgState.handler = () => ({ rows: [] }); // no ownership, no membership
    const caller = appRouter.createCaller(createCtx(createTestUser()));

    const err = await caller.timeline
      .upsert({ id: 42, profileId: 1, title: "Rally", eventDate: "2027-02-01" })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("FORBIDDEN");
    // Fail closed: no UPDATE may have been issued.
    expect(
      fakePgState.queries.filter(q => q.text.toLowerCase().startsWith("update ")),
    ).toHaveLength(0);
  });
});
