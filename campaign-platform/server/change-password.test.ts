/**
 * POST /api/change-password (audit SEC-follow-up): exercises the REAL express
 * route registered by registerLocalAuthRoutes against a real express app on
 * an ephemeral port. Only the trust boundaries are stubbed (session auth,
 * user store, rate-limit backend) — the handler, policy checks, and bcrypt
 * hashing/verification under test are the production code paths.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import express from "express";
import bcrypt from "bcryptjs";
import type { AddressInfo } from "net";

// ── Stubbed trust boundaries ────────────────────────────────────────────────
const authState = { authenticated: true, username: "changeme-user" };
const dbState = {
  passwordHash: bcrypt.hashSync("OldPassw0rd", 10),
  updatedTo: null as string | null,
};
const rlState = { allowed: true };

vi.mock("./_core/sdk", () => ({
  sdk: {
    authenticateRequest: vi.fn(async () => {
      if (!authState.authenticated) throw new Error("no session");
      return { username: authState.username };
    }),
  },
  SESSION_TTL_MS: 86400000,
}));
// NOTE: vi.mock paths resolve relative to THIS test file — "./db" here is
// server/db.ts, which localAuth.ts imports as "../db".
vi.mock("./db", () => ({
  getUserByUsername: vi.fn(async (username: string) =>
    username === dbStateUsername()
      ? { id: 7, username, passwordHash: dbState.passwordHash }
      : null),
  updateUserPassword: vi.fn(async (_id: number, hash: string) => {
    dbState.updatedTo = hash;
  }),
  getUserMfaEnabled: vi.fn(async () => false),
}));
vi.mock("./_core/rateLimit", () => ({
  hitRateLimit: vi.fn(async () => ({ allowed: rlState.allowed, retryAfterSeconds: 300 })),
  resetRateLimit: vi.fn(async () => {}),
}));
vi.mock("./_core/env", () => ({ ENV: { isProduction: false } }));

function dbStateUsername() {
  return authState.username;
}

// vi.mock calls above are hoisted by vitest, so this static import receives
// the stubbed sdk/db/rateLimit modules.
import { registerLocalAuthRoutes } from "./_core/localAuth";

async function startApp() {
  const app = express();
  app.use(express.json());
  registerLocalAuthRoutes(app);
  const server = await new Promise<ReturnType<express.Express["listen"]>>((resolve) => {
    const s = app.listen(0, "127.0.0.1", () => resolve(s));
  });
  const port = (server.address() as AddressInfo).port;
  return { server, base: `http://127.0.0.1:${port}` };
}

async function post(base: string, path: string, body: unknown) {
  const res = await fetch(`${base}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return { status: res.status, json: await res.json().catch(() => ({})) };
}

describe("POST /api/change-password", () => {
  beforeEach(() => {
    authState.authenticated = true;
    dbState.updatedTo = null;
    rlState.allowed = true;
  });

  it("rejects unauthenticated requests (401)", async () => {
    authState.authenticated = false;
    const { server, base } = await startApp();
    try {
      const r = await post(base, "/api/change-password", { currentPassword: "OldPassw0rd", newPassword: "NewPassw0rd" });
      expect(r.status).toBe(401);
      expect(dbState.updatedTo).toBeNull();
    } finally { server.close(); }
  });

  it("rejects missing fields (400)", async () => {
    const { server, base } = await startApp();
    try {
      const r = await post(base, "/api/change-password", { currentPassword: "OldPassw0rd" });
      expect(r.status).toBe(400);
    } finally { server.close(); }
  });

  it("rejects weak new passwords (400) — policy: 8-128 chars, upper+lower+digit", async () => {
    const { server, base } = await startApp();
    try {
      for (const weak of ["short1A", "alllowercase1", "ALLUPPERCASE1", "NoDigitsHere"]) {
        const r = await post(base, "/api/change-password", { currentPassword: "OldPassw0rd", newPassword: weak });
        expect(r.status).toBe(400);
        expect(dbState.updatedTo).toBeNull();
      }
    } finally { server.close(); }
  });

  it("rejects an incorrect current password (401)", async () => {
    const { server, base } = await startApp();
    try {
      const r = await post(base, "/api/change-password", { currentPassword: "WrongPass1", newPassword: "NewPassw0rd" });
      expect(r.status).toBe(401);
      expect(dbState.updatedTo).toBeNull();
    } finally { server.close(); }
  });

  it("rejects new password identical to current (400)", async () => {
    const { server, base } = await startApp();
    try {
      const r = await post(base, "/api/change-password", { currentPassword: "OldPassw0rd", newPassword: "OldPassw0rd" });
      expect(r.status).toBe(400);
      expect(dbState.updatedTo).toBeNull();
    } finally { server.close(); }
  });

  it("honours the rate limiter (429)", async () => {
    rlState.allowed = false;
    const { server, base } = await startApp();
    try {
      const r = await post(base, "/api/change-password", { currentPassword: "OldPassw0rd", newPassword: "NewPassw0rd" });
      expect(r.status).toBe(429);
      expect(dbState.updatedTo).toBeNull();
    } finally { server.close(); }
  });

  it("changes the password on success — stores a bcrypt hash of the NEW password", async () => {
    const { server, base } = await startApp();
    try {
      const r = await post(base, "/api/change-password", { currentPassword: "OldPassw0rd", newPassword: "NewPassw0rd" });
      expect(r.status).toBe(200);
      expect(r.json.ok).toBe(true);
      expect(dbState.updatedTo).not.toBeNull();
      // The stored value must be a real bcrypt hash of the new password —
      // and must NOT verify against the old one.
      expect(await bcrypt.compare("NewPassw0rd", dbState.updatedTo!)).toBe(true);
      expect(await bcrypt.compare("OldPassw0rd", dbState.updatedTo!)).toBe(false);
    } finally { server.close(); }
  });
});
