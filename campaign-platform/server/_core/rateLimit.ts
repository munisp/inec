// ─── Postgres-backed fixed-window rate limiter ───────────────────────────────
// Shared store for every throttling control in this service (login brute-force
// throttle, per-user LLM cost cap, public petition-sign dedup/rate limit).
//
// Backing table: rate_limits(key text, window_start timestamptz, count int,
// PK(key, window_start)) — see drizzle/0002_rate_limits.sql. Each hit is a
// single atomic INSERT ... ON CONFLICT DO UPDATE ... RETURNING count, so the
// limit holds across processes, replicas, and restarts (the previous
// per-process in-memory maps did neither).
//
// Fallback policy (fail closed):
//  - production: the Postgres store is mandatory. If the DB is unavailable or
//    the rate_limits query fails, the hit is DENIED (allowed:false) and the
//    failure is logged — a security control must never silently fail open.
//  - non-production (development/test): an in-memory map is used instead so
//    unit tests and local dev need no database; a warning is logged once per
//    process so the fallback is never silent.
import { sql } from "drizzle-orm";
import { logger } from "./logger";

export type RateLimitResult = {
  allowed: boolean;
  /** Hits recorded in the current window, including this one. */
  count: number;
  limit: number;
  /** Seconds until the current fixed window closes (for Retry-After). */
  retryAfterSeconds: number;
};

function windowStartMs(windowSeconds: number, nowMs: number): number {
  const w = windowSeconds * 1000;
  return Math.floor(nowMs / w) * w;
}

function retryAfter(windowSeconds: number, nowMs: number): number {
  return Math.ceil((windowStartMs(windowSeconds, nowMs) + windowSeconds * 1000 - nowMs) / 1000);
}

// ─── Non-production in-memory fallback ───────────────────────────────────────
const memoryWindows = new Map<string, { windowStartMs: number; count: number }>();
let fallbackWarned = false;

function hitInMemory(key: string, windowSeconds: number, limit: number): RateLimitResult {
  if (!fallbackWarned) {
    fallbackWarned = true;
    logger.warn(
      "rateLimit: using the in-memory rate-limit fallback — this is only " +
        "valid outside production (NODE_ENV !== 'production'); limits are " +
        "per-process and reset on restart"
    );
  }
  const now = Date.now();
  const start = windowStartMs(windowSeconds, now);
  const entry = memoryWindows.get(key);
  const count = !entry || entry.windowStartMs !== start ? 1 : entry.count + 1;
  memoryWindows.set(key, { windowStartMs: start, count });
  // Bound memory: drop fully-expired windows once the map grows large.
  if (memoryWindows.size > 10_000) {
    memoryWindows.forEach((v, k) => {
      if (v.windowStartMs !== start && now - v.windowStartMs > windowSeconds * 1000) {
        memoryWindows.delete(k);
      }
    });
  }
  return { allowed: count <= limit, count, limit, retryAfterSeconds: retryAfter(windowSeconds, now) };
}

// Test-only hook: reset the in-memory fallback between cases.
export function __resetInMemoryRateLimitsForTests() {
  memoryWindows.clear();
}

/**
 * Record one hit against `key` in the current fixed window and report whether
 * it is within `limit`. In production this is one atomic Postgres round-trip;
 * outside production (or when no database is configured there) it falls back
 * per the policy in this file's header.
 */
export async function hitRateLimit(
  key: string,
  windowSeconds: number,
  limit: number,
): Promise<RateLimitResult> {
  const isProduction = process.env.NODE_ENV === "production";
  const { getDb } = await import("../db");
  const conn = getDb();

  if (!isProduction && !conn) return hitInMemory(key, windowSeconds, limit);
  if (!conn) {
    // Production without a database: fail closed.
    logger.error("rateLimit: no database connection in production — denying hit (fail closed)", { key });
    return { allowed: false, count: limit + 1, limit, retryAfterSeconds: windowSeconds };
  }
  if (!isProduction) {
    // Development/test with a database still uses the in-memory fallback so
    // tests never depend on a migrated schema. (NODE_ENV !== "production"
    // is the explicit opt-in to the non-shared path.)
    return hitInMemory(key, windowSeconds, limit);
  }

  try {
    const now = Date.now();
    const start = new Date(windowStartMs(windowSeconds, now));
    const result = await conn.execute(sql`
      INSERT INTO rate_limits ("key", "window_start", "count")
      VALUES (${key}, ${start.toISOString()}, 1)
      ON CONFLICT ("key", "window_start")
      DO UPDATE SET "count" = rate_limits."count" + 1
      RETURNING "count"
    `);
    const count = Number((result.rows[0] as { count?: number } | undefined)?.count ?? 1);
    // Opportunistic pruning (~1% of hits) so expired windows cannot accumulate.
    if (Math.random() < 0.01) {
      void conn
        .execute(sql`DELETE FROM rate_limits WHERE "window_start" < now() - interval '2 days'`)
        .catch(err => logger.warn("rateLimit: failed to prune expired windows", { err: String(err) }));
    }
    return { allowed: count <= limit, count, limit, retryAfterSeconds: retryAfter(windowSeconds, now) };
  } catch (err) {
    // Fail closed: a broken limiter must not become an unlimited service.
    logger.error("rateLimit: Postgres rate-limit hit failed — denying (fail closed)", { key, err });
    return { allowed: false, count: limit + 1, limit, retryAfterSeconds: windowSeconds };
  }
}

/** Reset a key (e.g. clear the login throttle for a user after a successful login). */
export async function resetRateLimit(key: string): Promise<void> {
  const isProduction = process.env.NODE_ENV === "production";
  const { getDb } = await import("../db");
  const conn = getDb();
  if (!isProduction || !conn) {
    memoryWindows.delete(key);
    return;
  }
  try {
    await conn.execute(sql`DELETE FROM rate_limits WHERE "key" = ${key}`);
  } catch (err) {
    // Non-fatal: the window expires on its own.
    logger.warn("rateLimit: failed to reset key", { key, err: String(err) });
  }
}
