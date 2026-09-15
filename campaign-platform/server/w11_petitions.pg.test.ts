// R5-102 (real PG): petition signatures carry an identity hash and a
// verification tier — new signatures are 'unverified', identity duplicates
// are rejected, manager verification stamps who/when, and the migration
// backfills hashes + flags pre-existing duplicates honestly.
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Pool } from "pg";

import { createScratchDb, seedProfile } from "./testkit/pgScratch";

let dsn: string | null = null;
let profileId = 0;
let userId = 0;
let pool: Pool | null = null;
let petitionId = 0;

beforeAll(async () => {
  dsn = await createScratchDb("petitions");
  if (!dsn) throw new Error("pgserver unavailable — PG-backed test must not be vacuous");
  process.env.POSTGRES_URL = dsn;
  const seeded = await seedProfile(dsn, { username: "w11-pet", memberRole: "owner" });
  userId = seeded.userId;
  profileId = seeded.profileId;
  pool = new Pool({ connectionString: dsn });
});

afterAll(async () => {
  const { closeDb } = await import("./db");
  await closeDb();
  await pool?.end();
});

describe("R5-102 petition signature verification tiers (PG)", () => {
  it("dedupes by identity hash and never auto-verifies", async () => {
    if (!dsn) throw new Error("pgserver unavailable — PG-backed test must not be vacuous");
    const { appRouter } = await import("./routers");
    const { createCtx, createTestUser } = await import("./testkit/trpcCtx");
    const caller = appRouter.createCaller(createCtx(createTestUser({ id: userId, username: "w11-pet" })));

    const petition = (await caller.petitions.create({
      profileId, title: "Fix the ward road", targetSignatures: 100,
    })) as { id: number };
    petitionId = petition.id;
    await pool!.query(`UPDATE petitions SET status='active' WHERE id=$1`, [petitionId]);

    // 1. New signature → unverified + identity hash stored.
    const sig1 = (await caller.petitions.sign({
      petitionId, signerName: "Amina Bello", signerPhone: "0803 111-2222", signerLga: "Ikeja",
    })) as { id: number; verificationStatus: string; signerHash: string };
    expect(sig1.verificationStatus).toBe("unverified");
    expect(sig1.signerHash).toMatch(/^[0-9a-f]{64}$/);

    // 2. Same identity (phone formatted differently, name case differs) → CONFLICT.
    const dup = await caller.petitions.sign({
      petitionId, signerName: "amina bello", signerPhone: "08031112222", signerLga: "IKEJA",
    }).catch((e: unknown) => e);
    expect((dup as { code?: string }).code).toBe("CONFLICT");
    expect(String((dup as Error).message)).toContain("already signed");

    // 3. Different identity is fine and also starts unverified.
    const sig2 = (await caller.petitions.sign({
      petitionId, signerName: "Chidi Okafor", signerPhone: "0805-333-4444", signerLga: "Eti-Osa",
    })) as { id: number; verificationStatus: string };
    expect(sig2.verificationStatus).toBe("unverified");

    // 4. Stats reflect the tiers (nothing verified yet).
    let stats = (await caller.petitions.signatureStats({ petitionId })) as {
      total: number; verified: number; unverified: number; rejectedDuplicate: number;
    };
    expect(stats).toEqual({ total: 2, verified: 0, unverified: 2, rejectedDuplicate: 0 });

    // 5. Manager verifies one → stamped who/when; stats move.
    const verified = (await caller.petitions.verifySignature({ signatureId: sig1.id, petitionId })) as {
      verificationStatus: string; verifiedBy: string; verifiedAt: string | null;
    };
    expect(verified.verificationStatus).toBe("verified");
    expect(verified.verifiedBy).toBe("w11-pet");
    expect(verified.verifiedAt).toBeTruthy();
    stats = (await caller.petitions.signatureStats({ petitionId })) as typeof stats;
    expect(stats.verified).toBe(1);
    expect(stats.unverified).toBe(1);

    // 6. Double verification is rejected (state machine, not blind update).
    const reverify = await caller.petitions.verifySignature({ signatureId: sig1.id, petitionId }).catch((e: unknown) => e);
    expect((reverify as { code?: string }).code).toBe("CONFLICT");
  });

  it("migration created the identity-hash unique index (DB-level dedupe)", async () => {
    if (!dsn) throw new Error("pgserver unavailable — PG-backed test must not be vacuous");
    // A second row with the same (petition_id, signer_hash) must be rejected
    // by the partial unique index from migration 0005 — even bypassing the app.
    const hash = "ab".repeat(32);
    await pool!.query(
      `INSERT INTO petition_signatures (petition_id, signer_name, phone, signer_hash) VALUES ($1, 'X', '1', $2)`,
      [petitionId, hash],
    );
    await expect(
      pool!.query(
        `INSERT INTO petition_signatures (petition_id, signer_name, phone, signer_hash) VALUES ($1, 'Y', '2', $2)`,
        [petitionId, hash],
      ),
    ).rejects.toThrow(/petition_signatures_signer_hash_uniq/);
    // And rows without a hash (legacy) are still permitted by the partial index.
    await pool!.query(
      `INSERT INTO petition_signatures (petition_id, signer_name) VALUES ($1, 'No Hash Legacy')`,
      [petitionId],
    );
  });
});
