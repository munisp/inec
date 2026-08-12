import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("pg", async () => await import("./testkit/fakePg"));

process.env.POSTGRES_URL ||= "postgres://test:test@localhost:5432/test";
// Petition dedup/rate-limit uses the in-memory fallback outside production —
// exactly the mode under test here (Postgres-backed in prod, see rateLimit.ts).
process.env.NODE_ENV = "test";

import { TRPCError } from "@trpc/server";
import { fakePgState } from "./testkit/fakePg";
import { createCtx } from "./testkit/trpcCtx";
import { __resetInMemoryRateLimitsForTests } from "./_core/rateLimit";
import { appRouter } from "./routers";

function petitionRow(overrides: Record<string, unknown> = {}) {
  return {
    id: 5,
    profile_id: 1,
    title: "Fix the ward road",
    status: "active",
    target_signatures: 1000,
    ...overrides,
  };
}

// publicSign is a publicProcedure — unauthenticated ctx is the real caller.
const publicCtx = () => createCtx(null, "203.0.113.9");

function routePetition(rows: Array<Record<string, unknown>>) {
  fakePgState.handler = (text) => {
    const sql = text.toLowerCase();
    if (sql.includes('from "petitions"')) return { rows };
    if (sql.startsWith('insert into "petition_signatures"')) {
      return { rows: [{ id: 1, petition_id: 5, signer_name: "Amina Bello" }] };
    }
    return { rows: [] };
  };
}

beforeEach(() => {
  fakePgState.reset();
  __resetInMemoryRateLimitsForTests();
});

describe("petitions.publicSign semantics", () => {
  it("returns NOT_FOUND for an unknown petition id", async () => {
    routePetition([]);
    const caller = appRouter.createCaller(publicCtx());

    const err = await caller.petitions
      .publicSign({ petitionId: 999, signerName: "Amina Bello" })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("NOT_FOUND");
  });

  it("returns CONFLICT when the petition is not open for signatures", async () => {
    routePetition([petitionRow({ status: "closed" })]);
    const caller = appRouter.createCaller(publicCtx());

    const err = await caller.petitions
      .publicSign({ petitionId: 5, signerName: "Amina Bello" })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("CONFLICT");
  });

  it("rejects a duplicate signature from the same phone (TOO_MANY_REQUESTS)", async () => {
    routePetition([petitionRow()]);
    const caller = appRouter.createCaller(publicCtx());

    const first = await caller.petitions.publicSign({
      petitionId: 5,
      signerName: "Amina Bello",
      signerPhone: "08031234567",
    });
    expect(first).toMatchObject({ petitionId: 5 });

    const err = await caller.petitions
      .publicSign({ petitionId: 5, signerName: "Amina Bello", signerPhone: "08031234567" })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("TOO_MANY_REQUESTS");
    // The duplicate must not have reached the INSERT path a second time.
    const inserts = fakePgState.queries.filter(q =>
      q.text.toLowerCase().startsWith('insert into "petition_signatures"'),
    );
    expect(inserts).toHaveLength(1);
  });
});
