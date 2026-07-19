import { eq } from "drizzle-orm";
import * as schema from "../../drizzle/schema";
import "dotenv/config";
import express from "express";
import { createServer } from "http";
import net from "net";
import { createExpressMiddleware } from "@trpc/server/adapters/express";
import { registerOAuthRoutes } from "./oauth";
import { registerStorageProxy } from "./storageProxy";
import { appRouter } from "../routers";
import { createContext } from "./context";
import { serveStatic, setupVite } from "./vite";
import { getAllProfiles } from "../db";
import { sdk } from "./sdk";
import { notifyOwner } from "./notification";
import * as db from "../db";

// In-memory SSE client registry keyed by profileId
const sseClients = new Map<number, Set<(data: string) => void>>();

export function broadcastWarRoomUpdate(profileId: number) {
  const clients = sseClients.get(profileId);
  if (clients) {
    clients.forEach(send => send(JSON.stringify({ type: "update", profileId })));
  }
}

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
  // Configure body parser with larger size limit for file uploads
  app.use(express.json({ limit: "50mb" }));
  app.use(express.urlencoded({ limit: "50mb", extended: true }));
  registerStorageProxy(app);
  registerOAuthRoutes(app);

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
      console.error("[deadline-check]", err);
      return res.status(500).json({
        error: String(err),
        timestamp: new Date().toISOString(),
      });
    }
  });

  // SSE endpoint for War Room real-time updates
  app.get("/api/war-room/stream", (req, res) => {
    const profileId = parseInt(req.query.profileId as string);
    if (!profileId) { res.status(400).end(); return; }
    res.setHeader("Content-Type", "text/event-stream");
    res.setHeader("Cache-Control", "no-cache");
    res.setHeader("Connection", "keep-alive");
    res.flushHeaders();
    const send = (data: string) => res.write(`data: ${data}\n\n`);
    if (!sseClients.has(profileId)) sseClients.set(profileId, new Set());
    sseClients.get(profileId)!.add(send);
    req.on("close", () => {
      sseClients.get(profileId)?.delete(send);
    });
  });
  // tRPC API
  app.use(
    "/api/trpc",
    createExpressMiddleware({
      router: appRouter,
      createContext,
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
    console.log(`Port ${preferredPort} is busy, using port ${port} instead`);
  }

  server.listen(port, () => {
    console.log(`Server running on http://localhost:${port}/`);
  });
}

startServer().catch(console.error);
