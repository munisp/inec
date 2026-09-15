import { eq, sql } from "drizzle-orm";
import * as schema from "../../drizzle/schema";
import "dotenv/config";
import express from "express";
import { createServer } from "http";
import net from "net";
import { createExpressMiddleware } from "@trpc/server/adapters/express";
import { registerLocalAuthRoutes } from "./localAuth";
import { registerStorageProxy } from "./storageProxy";
import { appRouter } from "../routers";
import { createContext } from "./context";
import { serveStatic, setupVite } from "./vite";
import { closeDb, getAllProfiles } from "../db";
import { sdk } from "./sdk";
import { notifyOwner } from "./notification";
import { assertProfileRole } from "./trpc";
import { logger } from "./logger";
import {
  metricsAuthGuard,
  metricsMiddleware,
  recordTrpcError,
  renderMetrics,
} from "./metrics";
import {
  SSE_MAX_CLIENTS,
  SSE_MAX_STREAMS_PER_USER,
  broadcastWarRoomUpdate,
  destroyAllSseClients,
  registerSseClient,
  sseClientCount,
  sseStreamCountForUser,
} from "./sse";
import * as db from "../db";

// Re-export so existing importers of "./_core/index" keep working while the
// registry itself lives in ./sse (see sse.ts header for why it was extracted).
export { broadcastWarRoomUpdate };

function isPortAvailable(port: number): Promise<boolean> {
  return new Promise(resolve => {
    const server = net.createServer();
    server.listen(port, () => {
      server.close(() => resolve(true));
    });
    server.on("error", () => resolve(false));
  });
}

async function findAvailablePort(startPort: number = 3000): Promise<number> {
  for (let port = startPort; port < startPort + 20; port++) {
    if (await isPortAvailable(port)) {
      return port;
    }
  }
  throw new Error(`No available port found starting from ${startPort}`);
}

async function startServer() {
  const app = express();
  const server = createServer(app);
  // SECURITY: trust the first reverse-proxy hop so req.ip reflects the real
  // client IP (used by the login throttle and petition-sign rate limits).
  app.set("trust proxy", 1);
  // Body parser limit — 2mb is ample for all JSON API payloads; larger uploads
  // go through the storage proxy, not the JSON body.
  app.use(express.json({ limit: "2mb" }));
  app.use(express.urlencoded({ limit: "2mb", extended: true }));
  // Request metrics + structured access log (registered before the routes so
  // every response is timed).
  app.use(metricsMiddleware());
  registerStorageProxy(app);
  registerLocalAuthRoutes(app);

  // Prometheus-style metrics. Guarded by METRICS_BEARER_TOKEN; fails closed
  // in production when the token is unset (see _core/metrics.ts).
  app.get("/metrics", metricsAuthGuard, (_req, res) => {
    res.setHeader("Content-Type", "text/plain; version=0.0.4; charset=utf-8");
    res.send(renderMetrics());
  });

  // Liveness/readiness probe: actually verify the database instead of always
  // returning ok. 503 when the DB is missing, unreachable, or slow (>2s).
  app.get("/api/v1/campaign/health", async (_req, res) => {
    const dbConn = db.getDb();
    if (!dbConn) {
      res.status(503).json({ status: "error", reason: "database not configured" });
      return;
    }
    try {
      await Promise.race([
        dbConn.execute(sql`SELECT 1`),
        new Promise((_, reject) =>
          setTimeout(() => reject(new Error("health check timed out")), 2000)
        ),
      ]);
      // Report the in-process SSE client count so operators can see the war
      // room stream load (and the single-process SSE limitation) on the same
      // probe they already scrape.
      res.json({ status: "ok", sseClients: sseClientCount() });
    } catch (err) {
      logger.error("health: database check failed", { err });
      res.status(503).json({ status: "error", reason: "database unreachable" });
    }
  });

  // ── Deadline notification heartbeat handler ─────────────────────────────────
  // Triggered every hour by a project-level Heartbeat cron.
  // Scans all candidate profiles for timeline_events due within 48 hours and
  // sends an owner notification for each critical one.
  app.post("/api/scheduled/deadline-check", async (req, res) => {
    try {
      const user = await sdk.authenticateRequest(req);
      if (!user.isCron) {
        return res.status(403).json({ error: "cron-only endpoint" });
      }

      const dbConn = db.getDb();
      if (!dbConn) return res.json({ ok: true, skipped: "no-db" });

      const profiles = await getAllProfiles();
      let notified = 0;

      for (const profile of profiles) {
        const deadlines = await db.getUpcomingDeadlines(profile.id, 48);
        for (const event of deadlines) {
          // Deduplicate: skip if already alerted within the last 24 hours
          if (event.lastAlertedAt) {
            const hoursSinceAlert = (Date.now() - new Date(event.lastAlertedAt).getTime()) / 3_600_000;
            if (hoursSinceAlert < 24) continue;
          }

          const dueDate = new Date(event.eventDate);
          const hoursUntil = Math.round((dueDate.getTime() - Date.now()) / 3_600_000);
          const sent = await notifyOwner({
            title: `⏰ Deadline Alert: ${event.title}`,
            content: `Campaign deadline approaching in ${hoursUntil} hour${hoursUntil !== 1 ? "s" : ""}.\n\nCandidate: ${profile.candidateName} (${profile.partyName ?? "—"})\nEvent: ${event.title}\nDue: ${dueDate.toLocaleString("en-NG")}\nPriority: ${event.priority ?? "normal"}\n\nLog in to the INEC Campaign Intelligence Platform to review.`,
          });
          if (sent) {
            notified++;
            // Stamp the event so it won't fire again for 24 hours
            await dbConn.update(schema.timelineEvents)
              .set({ lastAlertedAt: new Date() })
              .where(eq(schema.timelineEvents.id, event.id));
          }
        }
      }

      return res.json({ ok: true, notified });
    } catch (err) {
      logger.error("deadline-check failed", { err });
      return res.status(500).json({
        error: String(err),
        timestamp: new Date().toISOString(),
      });
    }
  });

  // ── R5-099: silent-agent scan heartbeat ─────────────────────────────────────
  // Triggered periodically by a project-level Heartbeat cron (see
  // scheduled-tasks.json). Flags deployed agents with no check-in inside the
  // threshold as 'silent' across every profile and notifies the owner once
  // per scan when at least one agent is silent.
  app.post("/api/scheduled/silent-agent-scan", async (req, res) => {
    try {
      const user = await sdk.authenticateRequest(req);
      if (!user.isCron) {
        return res.status(403).json({ error: "cron-only endpoint" });
      }
      const dbConn = db.getDb();
      if (!dbConn) return res.json({ ok: true, skipped: "no-db" });

      const threshold = Number(process.env.SILENT_AGENT_THRESHOLD_MINUTES ?? 60);
      const silent = await db.scanSilentAgents(null, threshold);
      if (silent.length > 0) {
        try {
          await notifyOwner({
            title: `🔇 ${silent.length} field agent${silent.length !== 1 ? "s" : ""} silent`,
            content: `No check-in within ${threshold} minutes: ${silent
              .slice(0, 10)
              .map((a) => `${a.name}${a.assignedPu ? ` (${a.assignedPu})` : ""}`)
              .join(", ")}${silent.length > 10 ? `, +${silent.length - 10} more` : ""}`,
          });
        } catch { /* notification failure must not fail the scan */ }
      }
      return res.json({ ok: true, silentCount: silent.length });
    } catch (err) {
      logger.error("silent-agent-scan failed", { err });
      return res.status(500).json({
        error: String(err),
        timestamp: new Date().toISOString(),
      });
    }
  });

  // SSE endpoint for War Room real-time updates.
  // SECURITY: requires a verified session (same SDK verification as the rest of
  // the API — cookie, or Bearer for non-browser clients), enforces per-profile
  // tenancy (viewer role on the requested profileId), and is capped both
  // globally (SSE_MAX_CLIENTS) and per user (SSE_MAX_STREAMS_PER_USER).
  app.get("/api/war-room/stream", async (req, res) => {
    let user;
    try {
      user = await sdk.authenticateRequest(req);
    } catch {
      res.status(401).json({ error: "authentication required" });
      return;
    }
    const profileId = Number(req.query.profileId);
    if (!Number.isInteger(profileId) || profileId <= 0) { res.status(400).end(); return; }
    // SECURITY: per-profile authorization — a valid session alone must not
    // grant a stream of another campaign's war-room updates.
    try {
      await assertProfileRole(user, profileId, "viewer");
    } catch {
      res.status(403).json({ error: "no access to this campaign profile" });
      return;
    }
    if (sseClientCount() >= SSE_MAX_CLIENTS) {
      res.status(503).json({ error: "too many open streams; try again later" });
      return;
    }
    if (sseStreamCountForUser(user.id) >= SSE_MAX_STREAMS_PER_USER) {
      res.status(429).json({ error: "too many open streams for this account" });
      return;
    }
    res.setHeader("Content-Type", "text/event-stream");
    res.setHeader("Cache-Control", "no-cache");
    res.setHeader("Connection", "keep-alive");
    res.flushHeaders();
    const unregister = registerSseClient(profileId, user.id, res);
    req.on("close", unregister);
  });
  // tRPC API
  app.use(
    "/api/trpc",
    createExpressMiddleware({
      router: appRouter,
      createContext,
      onError({ path, error }) {
        recordTrpcError(path ?? "<unknown>", error.code);
        logger.error("tRPC procedure failed", {
          route: path ?? "<unknown>",
          code: error.code,
          err: error,
          cause: error.cause ? String(error.cause) : undefined,
        });
      },
    })
  );
  // development mode uses Vite, production mode uses static files
  if (process.env.NODE_ENV === "development") {
    await setupVite(app, server);
  } else {
    serveStatic(app);
  }

  const preferredPort = parseInt(process.env.PORT || "3000");
  const port = await findAvailablePort(preferredPort);

  if (port !== preferredPort) {
    logger.info("preferred port busy, using fallback", { preferredPort, port });
  }

  server.listen(port, () => {
    logger.info("server running", { port, env: process.env.NODE_ENV ?? "development" });
  });

  // ── Graceful shutdown ──────────────────────────────────────────────────────
  // Stop accepting connections, end open SSE streams, drain the DB pool, then
  // exit 0 so orchestrators see a clean stop instead of a SIGKILL timeout.
  let shuttingDown = false;
  const shutdown = (signal: string) => {
    if (shuttingDown) return;
    shuttingDown = true;
    logger.info("shutdown signal received — shutting down gracefully", { signal });
    server.close(() => logger.info("HTTP listener closed"));
    destroyAllSseClients();
    void closeDb()
      .catch(err => logger.error("failed to close DB pool", { err }))
      .finally(() => process.exit(0));
  };
  process.on("SIGTERM", () => shutdown("SIGTERM"));
  process.on("SIGINT", () => shutdown("SIGINT"));
}

// A boot failure must exit non-zero — previously the rejection was only
// logged, leaving the process alive with exit code 0 and no listener.
startServer().catch(err => {
  logger.error("server failed to start", { err });
  process.exit(1);
});
