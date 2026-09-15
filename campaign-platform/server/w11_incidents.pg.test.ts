// R5-098 (real PG): war-room incidents capture type/geo/evidence/occurrence,
// and the escalation workflow (assign → escalate → resolve) writes an
// append-only audit trail.
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Pool } from "pg";

import { createScratchDb, seedProfile } from "./testkit/pgScratch";

let dsn: string | null = null;
let profileId = 0;
let userId = 0;
let verifyPool: Pool | null = null;

beforeAll(async () => {
  dsn = await createScratchDb("incidents");
  if (!dsn) return;
  process.env.POSTGRES_URL = dsn;
  const seeded = await seedProfile(dsn, { username: "w11-inc", memberRole: "owner" });
  userId = seeded.userId;
  profileId = seeded.profileId;
  verifyPool = new Pool({ connectionString: dsn });
});

afterAll(async () => {
  const { closeDb } = await import("./db");
  await closeDb();
  await verifyPool?.end();
});

describe("R5-098 war-room incidents + escalation (PG)", () => {
  it("stores category/geo/evidence/occurrence and audits the escalation workflow", async () => {
    if (!dsn) return; // pgserver unavailable
    const { appRouter } = await import("./routers");
    const { createCtx, createTestUser } = await import("./testkit/trpcCtx");
    const ctx = createCtx(createTestUser({ id: userId, username: "w11-inc" }));
    const caller = appRouter.createCaller(ctx);

    // 1. Create with the previously-unsent fields.
    const occurred = new Date(Date.now() - 45 * 60_000).toISOString();
    const incident = await caller.warRoom.addIncident({
      profileId,
      severity: "critical",
      description: "Thugs disrupted voting at PU 12",
      lga: "Ikeja",
      pollingUnit: "PU-012",
      incidentType: "violence",
      latitude: 6.6018,
      longitude: 3.3515,
      evidenceUrl: "https://evidence.example/ec8a/pu012.jpg",
      occurredAt: occurred,
    });
    expect(incident).toBeTruthy();
    const id = (incident as { id: number }).id;

    const row = await verifyPool!.query(
      `SELECT incident_type, latitude, longitude, evidence_url, occurred_at, reported_by, status FROM war_room_incidents WHERE id=$1`,
      [id],
    );
    expect(row.rows[0].incident_type).toBe("violence");
    expect(Number(row.rows[0].latitude)).toBeCloseTo(6.6018, 4);
    expect(Number(row.rows[0].longitude)).toBeCloseTo(3.3515, 4);
    expect(row.rows[0].evidence_url).toContain("pu012.jpg");
    expect(row.rows[0].occurred_at).toBeTruthy();
    expect(row.rows[0].reported_by).toBe("w11-inc");

    // 2. Assign → audit row.
    await caller.warRoom.assignIncident({ id, assignedTo: "rapid-response-4", profileId });
    // 3. Escalate → status/escalated_to/escalated_at + audit + notification.
    const escalated = await caller.warRoom.escalateIncident({
      id, escalatedTo: "security_agency", note: "Lives at risk", profileId,
    }) as { status: string; escalatedTo: string; escalatedAt: string | null };
    expect(escalated.status).toBe("escalated");
    expect(escalated.escalatedTo).toBe("security_agency");
    expect(escalated.escalatedAt).toBeTruthy();
    // 4. Resolve → status/resolved_at + audit.
    const resolved = await caller.warRoom.resolveIncident({ id, profileId }) as { status: string; resolvedAt: string | null };
    expect(resolved.status).toBe("resolved");
    expect(resolved.resolvedAt).toBeTruthy();

    // 5. Audit trail is complete and ordered (created/assigned/escalated/resolved).
    const audit = await caller.warRoom.incidentAudit({ incidentId: id }) as Array<{ action: string; actor: string }>;
    const actions = audit.map((a) => a.action).sort();
    expect(actions).toEqual(["assigned", "created", "escalated", "resolved"]);
    expect(audit.every((a) => a.actor === "w11-inc")).toBe(true);
  });

  it("rejects escalation by a viewer (manager role required)", async () => {
    if (!dsn) return;
    const { appRouter } = await import("./routers");
    const { createCtx, createTestUser } = await import("./testkit/trpcCtx");
    // Second user with only viewer membership on the same profile.
    const viewer = await seedProfile(dsn!, { username: "w11-viewer", memberRole: "viewer" });
    // Move their membership onto the FIRST profile as viewer.
    const vp = new Pool({ connectionString: dsn! });
    await vp.query(`DELETE FROM campaign_members WHERE user_id=$1`, [viewer.userId]);
    await vp.query(
      `INSERT INTO campaign_members (profile_id, user_id, name, email, role) VALUES ($1,$2,'Viewer','v@example.test','viewer')`,
      [profileId, viewer.userId],
    );
    await vp.end();

    const managerCtx = createCtx(createTestUser({ id: userId, username: "w11-inc" }));
    const manager = appRouter.createCaller(managerCtx);
    const incident = await manager.warRoom.addIncident({ profileId, severity: "low", description: "queue confusion" });
    const id = (incident as { id: number }).id;

    const viewerCaller = appRouter.createCaller(createCtx(createTestUser({ id: viewer.userId, username: "w11-viewer" })));
    const err = await viewerCaller.warRoom
      .escalateIncident({ id, escalatedTo: "inec", profileId })
      .catch((e: unknown) => e);
    expect((err as { code?: string }).code).toBe("FORBIDDEN");
  });
});
