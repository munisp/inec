import { COOKIE_NAME } from "@shared/const";
import bcrypt from "bcryptjs";
import type { Express, Request, Response } from "express";
import * as db from "../db";
import { getSessionCookieOptions } from "./cookies";
import { sdk, SESSION_TTL_MS } from "./sdk";
import { ENV } from "./env";
import { hitRateLimit, resetRateLimit } from "./rateLimit";

// ─── Login rate limiting ─────────────────────────────────────────────────────
// SECURITY: per-IP+username fixed-window throttle against credential
// brute-forcing / password spraying. Backed by the shared Postgres
// rate_limits table (server/_core/rateLimit.ts) so the throttle survives
// restarts and holds across replicas; the in-memory fallback is
// non-production only and fails closed in production.
const LOGIN_WINDOW_SECONDS = 5 * 60; // 5 minutes
const LOGIN_MAX_ATTEMPTS = 5;
// SECURITY: per-IP-only window against password spraying — an attacker
// rotating through many usernames from one address never trips the
// per-IP+username key above, so the whole source IP gets a wider cap.
const LOGIN_IP_WINDOW_SECONDS = 5 * 60; // 5 minutes
const LOGIN_IP_MAX_ATTEMPTS = 30;

function loginAttemptKey(req: Request, username: string): string {
  const ip = req.ip || req.socket.remoteAddress || "unknown";
  return `${ip}:${username.toLowerCase()}`;
}

// req.ip respects `app.set("trust proxy", 1)` (set in _core/index.ts), so this
// is the real client IP, not the load balancer's.
function loginIpOnlyKey(req: Request): string {
  return `ip-only:${req.ip || req.socket.remoteAddress || "unknown"}`;
}

// Local username/password login against the Go backend's shared `users`
// table (bcrypt password hashes) — used instead of OAuth, which requires an
// external OAuth server that isn't configured in this deployment.
export function registerLocalAuthRoutes(app: Express) {
  app.post("/api/login", async (req: Request, res: Response) => {
    const { username, password } = req.body ?? {};
    if (typeof username !== "string" || typeof password !== "string" || !username || !password) {
      res.status(400).json({ error: "username and password are required" });
      return;
    }

    const throttleKey = loginAttemptKey(req, username);
    const ipKey = loginIpOnlyKey(req);
    // Every attempt (successful or not) consumes from both windows; a 429
    // below means the fixed-window counter already exceeded the cap.
    const [userAttempt, ipAttempt] = await Promise.all([
      hitRateLimit(`login:user:${throttleKey}`, LOGIN_WINDOW_SECONDS, LOGIN_MAX_ATTEMPTS),
      hitRateLimit(`login:${ipKey}`, LOGIN_IP_WINDOW_SECONDS, LOGIN_IP_MAX_ATTEMPTS),
    ]);
    if (!userAttempt.allowed || !ipAttempt.allowed) {
      const retryAfter = Math.max(userAttempt.retryAfterSeconds, ipAttempt.retryAfterSeconds);
      res.setHeader("Retry-After", String(retryAfter));
      res.status(429).json({ error: "too many login attempts; try again in a few minutes" });
      return;
    }

    const user = await db.getUserByUsername(username);
    if (!user || !(await bcrypt.compare(password, user.passwordHash))) {
      res.status(401).json({ error: "invalid username or password" });
      return;
    }
    if (user.isActive === 0) {
      res.status(403).json({ error: "account is inactive" });
      return;
    }

    // SECURITY (audit SEC-8): accounts with MFA enabled on the shared INEC
    // identity store must not get a password-only session here — the Go EMS
    // login enforces the second factor, this one cannot. Fail closed only
    // when we can positively confirm MFA is on; "unknown" (mfa schema not
    // co-deployed) does not block legitimate campaign-only deployments.
    const mfa = await db.getUserMfaEnabled(user.id);
    if (mfa === true) {
      res.status(403).json({
        error: "this account has multi-factor authentication enabled; sign in through the INEC portal which enforces the second factor",
        code: "MFA_REQUIRED_ELSEWHERE",
      });
      return;
    }

    // Successful login: reset the per-user throttle window for this key.
    // The per-IP spraying window intentionally keeps counting.
    await resetRateLimit(`login:user:${throttleKey}`);

    const sessionToken = await sdk.signSession(
      { openId: user.username, appId: ENV.appId || "campaign-platform", name: user.fullName },
      { expiresInMs: SESSION_TTL_MS }
    );

    const cookieOptions = getSessionCookieOptions(req);
    res.cookie(COOKIE_NAME, sessionToken, { ...cookieOptions, maxAge: SESSION_TTL_MS });
    // The HttpOnly cookie is the primary session mechanism. A tab-scoped
    // sessionStorage mirror exists for non-browser/embedded clients that block
    // third-party cookies but can send a Bearer header; it is only included
    // when explicitly enabled to avoid exposing the token in the JSON body by
    // default (XSS-readable). Set AUTH_RETURN_SESSION_COOKIE=true to enable.
    const includeSessionCookie = process.env.AUTH_RETURN_SESSION_COOKIE === "true";
    res.json({
      ok: true,
      user: { username: user.username, fullName: user.fullName, role: user.role },
      ...(includeSessionCookie ? { sessionCookie: `${COOKIE_NAME}=${sessionToken}` } : {}),
    });
  });

  // ─── Authenticated password change (audit SEC-11) ─────────────────────────
  // There was previously NO password change/reset flow anywhere in the
  // platform. This is the self-service change path (authenticated, current
  // password verified, same policy as the Go backend: 8-128 chars with
  // upper+lower+digit). Self-service reset for forgotten passwords remains
  // an admin action out-of-band (no email/phone channel exists on accounts).
  app.post("/api/change-password", async (req: Request, res: Response) => {
    let session;
    try {
      session = await sdk.authenticateRequest(req);
    } catch {
      res.status(401).json({ error: "authentication required" });
      return;
    }
    const { currentPassword, newPassword } = req.body ?? {};
    if (typeof currentPassword !== "string" || typeof newPassword !== "string" || !currentPassword || !newPassword) {
      res.status(400).json({ error: "currentPassword and newPassword are required" });
      return;
    }
    if (
      newPassword.length < 8 || newPassword.length > 128 ||
      !/[a-z]/.test(newPassword) || !/[A-Z]/.test(newPassword) || !/[0-9]/.test(newPassword)
    ) {
      res.status(400).json({ error: "password must be 8-128 characters and include upper-case, lower-case, and a digit" });
      return;
    }

    // Same brute-force protection as login, keyed to the authenticated user.
    const key = `change-pw:${session.username}:${req.ip || req.socket.remoteAddress || "unknown"}`;
    const attempt = await hitRateLimit(key, LOGIN_WINDOW_SECONDS, LOGIN_MAX_ATTEMPTS);
    if (!attempt.allowed) {
      res.setHeader("Retry-After", String(attempt.retryAfterSeconds));
      res.status(429).json({ error: "too many attempts; try again in a few minutes" });
      return;
    }

    const user = await db.getUserByUsername(session.username);
    if (!user || !(await bcrypt.compare(currentPassword, user.passwordHash))) {
      res.status(401).json({ error: "current password is incorrect" });
      return;
    }
    if (await bcrypt.compare(newPassword, user.passwordHash)) {
      res.status(400).json({ error: "new password must differ from the current password" });
      return;
    }

    await db.updateUserPassword(user.id, await bcrypt.hash(newPassword, 10));
    await resetRateLimit(key);
    res.json({ ok: true });
  });
}
