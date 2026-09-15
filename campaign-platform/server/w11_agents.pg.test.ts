// R5-099 (real PG): agent self check-in stamps last_checkin and returns the
// agent to 'active'; the silent-agent scan flags agents with no check-in
// inside the threshold and is idempotent.
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Pool } from "pg";

import { createScratchDb, seedProfile } from "./testkit/pgScratch";

let dsn: string | null = null;
let profileId = 0;
let userId = 0;
let pool: Pool | null = null;

async function seedAgent(name: string, status: string, lastCheckin: Date | null) {
  const r = await pool!.query(
    `INSERT INTO field_agents (profile_id, name, agent_status, last_checkin) VALUES ($1,$2,$3,$4) RETURNING id`,
    [profileId, name, status, lastCheckin],
  );
  return r.rows[0].id as number;
}

beforeAll(async () => {
  dsn = await createScratchDb("agents");
  if (!dsn) return;
  process.env.POSTGRES_URL = dsn;
  const seeded = await seedProfile(dsn, { username: "w11-ag", memberRole: "owner" });
  userId = seeded.userId;
  profileId = seeded.profileId;
  pool = new Pool({ connectionString: dsn });
});

afterAll(async () => {
  const { closeDb } = await import("./db");
  await closeDb();
  await pool?.end();
});

describe("R5-099 agent check-in + silent scan (PG)", () => {
  it("check-in stamps last_checkin/active and silent scan flags stale agents", async () => {
    if (!dsn) return;
    const { appRouter } = await import("./routers");
    const { createCtx, createTestUser } = await import("./testkit/trpcCtx");
    const caller = appRouter.createCaller(createCtx(createTestUser({ id: userId, username: "w11-ag" })));

    const staleId = await seedAgent("Stale Agent", "active", new Date(Date.now() - 3 * 3600_000));
    const neverId = await seedAgent("Never Checked In", "active", null);
    const freshId = await seedAgent("Fresh Agent", "active", new Date());
    const offlineId = await seedAgent("Offline Agent", "offline", new Date(Date.now() - 5 * 3600_000));

    // 1. Scan flags stale + never-checked-in deployed agents, not fresh/offline.
    const silent = (await caller.warRoom.silentAgents({ profileId, thresholdMinutes: 60 })) as Array<{ id: number; agentStatus: string }>;
    const silentIds = silent.map((a) => a.id);
    expect(silentIds).toContain(staleId);
    expect(silentIds).toContain(neverId);
    expect(silentIds).not.toContain(freshId);
    expect(silentIds).not.toContain(offlineId);
    expect(silent.every((a) => a.agentStatus === "silent")).toBe(true);

    // Scan is idempotent — second run keeps the same set.
    const again = (await caller.warRoom.silentAgents({ profileId, thresholdMinutes: 60 })) as Array<{ id: number }>;
    expect(again.map((a) => a.id).sort()).toEqual(silentIds.sort());

    // 2. Self check-in returns the agent to active with a fresh stamp and
    // records the agent-reported voters-counted figure.
    const checked = (await caller.warRoom.checkIn({ profileId, agentId: staleId, votersCounted: 42 })) as unknown as {
      id: number; agentStatus: string; lastCheckin: string; votersCounted: number;
    };
    expect(checked.agentStatus).toBe("active");
    expect(checked.votersCounted).toBe(42);
    expect(new Date(checked.lastCheckin).getTime()).toBeGreaterThan(Date.now() - 60_000);

    // 3. After check-in the agent drops out of the silent set.
    const after = (await caller.warRoom.silentAgents({ profileId, thresholdMinutes: 60 })) as Array<{ id: number }>;
    expect(after.map((a) => a.id)).not.toContain(staleId);

    // 4. Cross-tenant check-in is rejected (agent row belongs to profileId).
    const seeded2 = await seedProfile(dsn!, { username: "w11-ag2", memberRole: "owner" });
    const caller2 = appRouter.createCaller(createCtx(createTestUser({ id: seeded2.userId, username: "w11-ag2" })));
    const err = await caller2.warRoom
      .checkIn({ profileId: seeded2.profileId, agentId: neverId })
      .catch((e: unknown) => e);
    expect((err as { code?: string }).code).toBe("NOT_FOUND");
  });
});
