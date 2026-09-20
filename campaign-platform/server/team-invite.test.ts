import { createHash } from "crypto";
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

// Tokens are stored as SHA-256 hashes in invite_token (audit SEC-6); the
// plaintext "tok-abc" only ever exists in the invite URL presented to the
// inviter. fakePg routes by table, but fixtures mirror real storage.
const PLAINTEXT_TOKEN = "tok-abc";
const TOKEN_HASH = createHash("sha256").update(PLAINTEXT_TOKEN).digest("hex");

function memberRow(overrides: Record<string, unknown> = {}) {
  return {
    id: 7,
    profile_id: 1,
    user_id: null,
    email: "invitee@example.com",
    name: "Invitee",
    role: "viewer",
    invite_token: TOKEN_HASH,
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
      .confirmAccept({ token: PLAINTEXT_TOKEN, email: "invitee@example.com" })
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

    await expect(caller.team.acceptInvite({ token: PLAINTEXT_TOKEN })).resolves.toBeNull();
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
      token: PLAINTEXT_TOKEN,
      email: "invitee@example.com",
    });

    expect(result).toMatchObject({ userId: 1 });
  });
});
