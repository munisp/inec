// Audit GAP-1/GAP-6: statutory compliance checklist presets.
// Uses the programmable fake `pg` module (same pattern as compliance.test.ts).
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("pg", async () => await import("./testkit/fakePg"));

process.env.POSTGRES_URL ||= "postgres://test:test@localhost:5432/test";

import { fakePgState } from "./testkit/fakePg";
import { createCtx, createTestUser } from "./testkit/trpcCtx";
import { appRouter } from "./routers";
import { COMPLIANCE_PRESETS } from "./db";

function routeAsManager(existingTitles: string[] = []) {
  fakePgState.handler = (text) => {
    const sql = text.toLowerCase();
    if (sql.includes('from "candidate_profiles"')) return { rows: [] };
    if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
    if (sql.includes('from "compliance_items"') && sql.includes("select")) {
      return { rows: existingTitles.map((title) => ({ title })) };
    }
    return { rows: [] };
  };
}

beforeEach(() => fakePgState.reset());

describe("compliance statutory presets (GAP-1)", () => {
  it("presets query returns the statutory catalog (CF001, EA-2022 s.87/s.88, NBC)", async () => {
    routeAsManager();
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const presets = await caller.compliance.presets({ profileId: 1 });
    expect(presets.length).toBeGreaterThanOrEqual(7);
    const titles = presets.map((p) => p.title).join(" | ");
    expect(titles).toContain("CF001");
    expect(titles).toContain("s.87");
    expect(titles).toContain("s.88");
    expect(titles).toContain("NBC");
    // The catalog is static reference data — no DB writes.
    expect(fakePgState.queries.filter((q) => q.text.toLowerCase().includes("insert"))).toHaveLength(0);
  });

  it("loadPresets inserts exactly the missing items, all starting as pending", async () => {
    routeAsManager(["Party Nomination Form (INEC CF001)"]); // already tracked
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const result = await caller.compliance.loadPresets({ profileId: 1 });
    expect(result.skipped).toBe(1);
    expect(result.inserted).toBe(COMPLIANCE_PRESETS.length - 1);
    const inserts = fakePgState.queries.filter((q) => q.text.toLowerCase().includes("insert into"));
    expect(inserts).toHaveLength(1);
    // The insert must carry every missing preset except the existing CF001.
    const params = inserts[0].params ?? [];
    expect(params).not.toContain("Party Nomination Form (INEC CF001)");
    expect(params).toContain("Campaign Spending Cap Compliance (EA 2022 s.88)");
    expect(params).toContain("Broadcast Advert Clearance (NBC)");
  });

  it("loadPresets is idempotent when the checklist is fully loaded", async () => {
    routeAsManager(COMPLIANCE_PRESETS.map((p) => p.title));
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const result = await caller.compliance.loadPresets({ profileId: 1 });
    expect(result).toEqual({ inserted: 0, skipped: COMPLIANCE_PRESETS.length });
    expect(fakePgState.queries.filter((q) => q.text.toLowerCase().includes("insert"))).toHaveLength(0);
  });
});
