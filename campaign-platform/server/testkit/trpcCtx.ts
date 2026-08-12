// ─── Shared tRPC test context builders ───────────────────────────────────────
import type { TrpcContext } from "../_core/context";

export type TestUser = NonNullable<TrpcContext["user"]>;

// Real `users` table shape (drizzle/schema.ts) minus passwordHash, which the
// request context strips (SessionUser in _core/context.ts) — same fixture
// pattern as server/auth.logout.test.ts.
export function createTestUser(overrides: Partial<TestUser> = {}): TestUser {
  return {
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
    ...overrides,
  };
}

export function createCtx(user: TestUser | null, ip = "10.0.0.1"): TrpcContext {
  return {
    user,
    req: {
      protocol: "https",
      headers: {},
      ip,
      socket: { remoteAddress: ip },
    } as unknown as TrpcContext["req"],
    res: {
      clearCookie: () => {},
    } as unknown as TrpcContext["res"],
  };
}
