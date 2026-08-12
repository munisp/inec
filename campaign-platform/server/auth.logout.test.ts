import { describe, expect, it } from "vitest";
import { appRouter } from "./routers";
import { COOKIE_NAME } from "../shared/const";
import type { TrpcContext } from "./_core/context";

type CookieCall = {
  name: string;
  options: Record<string, unknown>;
};

type AuthenticatedUser = NonNullable<TrpcContext["user"]>;

function createAuthContext(): { ctx: TrpcContext; clearedCookies: CookieCall[] } {
  const clearedCookies: CookieCall[] = [];

  // Real `users` table shape (drizzle/schema.ts) minus passwordHash, which
  // the request context strips (SessionUser in _core/context.ts).
  const user: AuthenticatedUser = {
    id: 1,
    username: "sample-user",
    fullName: "Sample User",
    role: "user",
    staffId: null,
    stateCode: null,
    lgaCode: null,
    pollingUnitCode: null,
    createdAt: new Date(),
    isActive: 1,
    partyId: null,
    kycStatus: null,
  };

  const ctx: TrpcContext = {
    user,
    req: {
      protocol: "https",
      headers: {},
    } as TrpcContext["req"],
    res: {
      clearCookie: (name: string, options: Record<string, unknown>) => {
        clearedCookies.push({ name, options });
      },
    } as TrpcContext["res"],
  };

  return { ctx, clearedCookies };
}

describe("auth.logout", () => {
  it("clears the session cookie and reports success", async () => {
    const { ctx, clearedCookies } = createAuthContext();
    const caller = appRouter.createCaller(ctx);

    const result = await caller.auth.logout();

    expect(result).toEqual({ success: true });
    expect(clearedCookies).toHaveLength(1);
    expect(clearedCookies[0]?.name).toBe(COOKIE_NAME);
    expect(clearedCookies[0]?.options).toMatchObject({
      maxAge: -1,
      secure: true,
      // Session cookies default to SameSite=Lax (CSRF hardening); "none" is
      // only used when explicitly enabled via COOKIE_SAMESITE for embeds.
      sameSite: "lax",
      httpOnly: true,
      path: "/",
    });
  });
});
