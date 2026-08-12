import { COOKIE_NAME } from "@shared/const";
import bcrypt from "bcryptjs";
import type { Express, Request, Response } from "express";
import * as db from "../db";
import { getSessionCookieOptions } from "./cookies";
import { sdk, SESSION_TTL_MS } from "./sdk";
import { ENV } from "./env";

// ─── Login rate limiting ─────────────────────────────────────────────────────
// SECURITY: per-IP+username sliding-window throttle against credential
// brute-forcing / password spraying. In-memory (per process); a multi-instance
// deployment should move this to a shared store (e.g. Redis).
const LOGIN_WINDOW_MS = 5 * 60 * 1000; // 5 minutes
const LOGIN_MAX_ATTEMPTS = 5;
// SECURITY: per-IP-only window against password spraying — an attacker
// rotating through many usernames from one address never trips the
// per-IP+username key above, so the whole source IP gets a wider cap.
const LOGIN_IP_WINDOW_MS = 5 * 60 * 1000; // 5 minutes
const LOGIN_IP_MAX_ATTEMPTS = 30;
const loginAttempts = new Map<string, number[]>();

function loginAttemptKey(req: Request, username: string): string {
  const ip = req.ip || req.socket.remoteAddress || "unknown";
  return `${ip}:${username.toLowerCase()}`;
}

// req.ip respects `app.set("trust proxy", 1)` (set in _core/index.ts), so this
// is the real client IP, not the load balancer's.
function loginIpOnlyKey(req: Request): string {
  return `ip-only:${req.ip || req.socket.remoteAddress || "unknown"}`;
}

function isLoginThrottled(
  key: string,
  windowMs: number = LOGIN_WINDOW_MS,
  maxAttempts: number = LOGIN_MAX_ATTEMPTS,
): boolean {
  const now = Date.now();
  const attempts = (loginAttempts.get(key) ?? []).filter(t => now - t < windowMs);
  if (attempts.length === 0) {
    loginAttempts.delete(key);
  } else {
    loginAttempts.set(key, attempts);
  }
  return attempts.length >= maxAttempts;
}

function recordLoginAttempt(key: string, windowMs: number = LOGIN_WINDOW_MS) {
  const now = Date.now();
  const attempts = (loginAttempts.get(key) ?? []).filter(t => now - t < windowMs);
  attempts.push(now);
  loginAttempts.set(key, attempts);
  // Bound memory: occasionally drop fully-expired keys.
  if (loginAttempts.size > 10_000) {
    loginAttempts.forEach((v, k) => {
      if (v.every(t => now - t >= LOGIN_WINDOW_MS)) loginAttempts.delete(k);
    });
  }
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
    if (
      isLoginThrottled(throttleKey) ||
      isLoginThrottled(ipKey, LOGIN_IP_WINDOW_MS, LOGIN_IP_MAX_ATTEMPTS)
    ) {
      res.status(429).json({ error: "too many login attempts; try again in a few minutes" });
      return;
    }
    recordLoginAttempt(throttleKey);
    recordLoginAttempt(ipKey, LOGIN_IP_WINDOW_MS);

    const user = await db.getUserByUsername(username);
    if (!user || !(await bcrypt.compare(password, user.passwordHash))) {
      res.status(401).json({ error: "invalid username or password" });
      return;
    }
    if (user.isActive === 0) {
      res.status(403).json({ error: "account is inactive" });
      return;
    }

    // Successful login: reset the throttle window for this key.
    loginAttempts.delete(throttleKey);

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
}
