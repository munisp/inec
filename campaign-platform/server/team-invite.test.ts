import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("pg", async () => await import("./testkit/fakePg"));

process.env.POSTGRES_URL ||= "postgres://test:test@localhost:5432/test";

import { TRPCError } from "@trpc/server";
import { fakePgState } from "./testkit/fakePg";
import { createCtx, createTestUser } from "./testkit/trpcCtx";
import { appRouter } from "./routers";
import { INVITE_TTL_MS } from "./db";

// pg wire format for `timestamp` (no tz) columns — a Date object would be
// re-parsed through the column's mapFromDriverValue and shifted by the local
// timezone, so fixtures must use the same string shape real pg returns.
const pgTs = (d: Date) => d.toISOString().replace("T", " ").replace("Z", "");

function memberRow(overrides: Record<string, unknown> = {}) {
  return {
    id: 7,
    profile_id: 1,
    user_id: null,
    email: "invitee@example.com",
    name: "Invitee",
    role: "viewer",
    invite_token: "tok-abc",
    invited_at: pgTs(new Date()),
    accepted_at: null,
    ...overrides,
  };
}

function routeMembers(rows: Array<Record<string, unknown>>) {
  fakePgState.handler = (text) => {
    if (text.toLowerCase().includes('from "campaign_members"')) return { rows };
    return { rows: [] };
  };
}

beforeEach(() => fakePgState.reset());

describe("team invites", () => {
  it("rejects acceptance of an expired invite (BAD_REQUEST)", async () => {
    routeMembers([
      memberRow({ invited_at: pgTs(new Date(Date.now() - INVITE_TTL_MS - 60_000)) }),
    ]);
    const caller = appRouter.createCaller(createCtx(createTestUser()));

    const err = await caller.team
      .confirmAccept({ token: "tok-abc", email: "invitee@example.com" })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("BAD_REQUEST");
    expect((err as TRPCError).message).toMatch(/expired/i);
  });

  it("resolves an expired invite token to null on lookup (same as unknown)", async () => {
    routeMembers([
      memberRow({ invited_at: pgTs(new Date(Date.now() - INVITE_TTL_MS - 60_000)) }),
    ]);
    const caller = appRouter.createCaller(createCtx(null)); // public query

    await expect(caller.team.acceptInvite({ token: "tok-abc" })).resolves.toBeNull();
  });

  it("accepts a fresh invite with the matching email", async () => {
    const fresh = memberRow();
    fakePgState.handler = (text) => {
      const sql = text.toLowerCase();
      if (sql.includes('from "campaign_members"')) return { rows: [fresh] };
      if (sql.startsWith('update "campaign_members"')) {
        return { rows: [{ ...fresh, user_id: 1, accepted_at: pgTs(new Date()), invite_token: null }] };
      }
      return { rows: [] };
    };
    const caller = appRouter.createCaller(createCtx(createTestUser()));

    const result = await caller.team.confirmAccept({
      token: "tok-abc",
      email: "invitee@example.com",
    });

    expect(result).toMatchObject({ userId: 1 });
  });
});
