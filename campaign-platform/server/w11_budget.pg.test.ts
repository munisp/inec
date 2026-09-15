// R5-101 (real PG): statutory per-office campaign-spend caps are enforced
// server-side and every budget mutation lands in an append-only ledger.
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Pool } from "pg";

import { createScratchDb, seedProfile } from "./testkit/pgScratch";

let dsn: string | null = null;
let profileId = 0;
let userId = 0;
let pool: Pool | null = null;

beforeAll(async () => {
  dsn = await createScratchDb("budget");
  if (!dsn) throw new Error("pgserver unavailable — PG-backed test must not be vacuous");
  process.env.POSTGRES_URL = dsn;
  const seeded = await seedProfile(dsn, { username: "w11-bud", memberRole: "owner", office: "Governor" });
  userId = seeded.userId;
  profileId = seeded.profileId;
  pool = new Pool({ connectionString: dsn });
});

afterAll(async () => {
  const { closeDb } = await import("./db");
  await closeDb();
  await pool?.end();
});

describe("R5-101 budget statutory caps + ledger (PG)", () => {
  it("enforces the Governor ₦1bn cap and audits every mutation", async () => {
    if (!dsn) throw new Error("pgserver unavailable — PG-backed test must not be vacuous");
    const { appRouter } = await import("./routers");
    const { createCtx, createTestUser } = await import("./testkit/trpcCtx");
    const caller = appRouter.createCaller(createCtx(createTestUser({ id: userId, username: "w11-bud" })));

    // Caps are seeded from the Electoral Act values.
    const caps = (await caller.budget.caps({ profileId })) as Array<{ office: string; capAmount: number }>;
    expect(caps.length).toBeGreaterThanOrEqual(5);
    expect(caps.find((c) => c.office === "Governor")?.capAmount).toBe(1_000_000_000);

    // 1. ₦600m spend — under the ₦1bn cap.
    const item1 = (await caller.budget.upsert({
      profileId, category: "logistics", description: "rally buses",
      budgetedAmount: 700_000_000, spentAmount: 600_000_000,
    })) as { id: number };
    expect(item1.id).toBeTruthy();

    // 2. ₦500m more — projected ₦1.1bn > cap → CONFLICT.
    const overCap = await caller.budget.upsert({
      profileId, category: "media", description: "tv slots",
      budgetedAmount: 600_000_000, spentAmount: 500_000_000,
    }).catch((e: unknown) => e);
    expect((overCap as { code?: string }).code).toBe("CONFLICT");
    expect(String((overCap as Error).message)).toContain("Electoral Act 2022");

    // 3. Updating spend to ₦950m is fine; ₦1.2bn is not.
    const ok = (await caller.budget.upsert({
      id: item1.id, profileId, category: "logistics", description: "rally buses",
      budgetedAmount: 700_000_000, spentAmount: 950_000_000,
    })) as { spentAmount: number };
    expect(Number(ok.spentAmount)).toBe(950_000_000);
    const overUpdate = await caller.budget.upsert({
      id: item1.id, profileId, category: "logistics", description: "rally buses",
      budgetedAmount: 700_000_000, spentAmount: 1_200_000_000,
    }).catch((e: unknown) => e);
    expect((overUpdate as { code?: string }).code).toBe("CONFLICT");

    // 4. Owner can adjust the cap (configurable) — then the spend passes.
    await caller.budget.setCap({ profileId, office: "Governor", capAmount: 1_500_000_000, notes: "test amendment" });
    const afterCapRaise = (await caller.budget.upsert({
      id: item1.id, profileId, category: "logistics", description: "rally buses",
      budgetedAmount: 700_000_000, spentAmount: 1_200_000_000,
    })) as { spentAmount: number };
    expect(Number(afterCapRaise.spentAmount)).toBe(1_200_000_000);

    // 5. Delete is ledgered too.
    await caller.budget.delete({ id: item1.id });

    // 6. Ledger has created/spend_changed entries and the deletion.
    const ledger = (await caller.budget.ledger({ profileId })) as Array<{ changeType: string; changedBy: string }>;
    const types = ledger.map((l) => l.changeType).sort();
    expect(types).toContain("created");
    expect(types).toContain("spend_changed");
    expect(types).toContain("deleted");
    expect(ledger.every((l) => l.changedBy === "w11-bud")).toBe(true);

    // 7. The ledger is append-only at the database level.
    await expect(
      pool!.query(`UPDATE budget_spend_ledger SET note='tamper' WHERE profile_id=$1`, [profileId]),
    ).rejects.toThrow(/append-only/);
    await expect(
      pool!.query(`DELETE FROM budget_spend_ledger WHERE profile_id=$1`, [profileId]),
    ).rejects.toThrow(/append-only/);
  });
});
