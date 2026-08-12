// ─── Prometheus-style metrics (hand-rolled, no new dependencies) ─────────────
// Exposes request counts/durations by route, tRPC error counts, and the
// active SSE client gauge at GET /metrics in Prometheus text format.
//
// Access control (mirrors the Go services' metricsBearerGuard, but fails
// closed in production):
//  - METRICS_BEARER_TOKEN set     → requests must present `Authorization:
//    Bearer <token>` (constant-time comparison).
//  - unset + NODE_ENV=production  → 503: the endpoint is disabled rather than
//    left open — fail closed.
//  - unset + non-production       → open, for local development/tests.
import { timingSafeEqual } from "crypto";
import type { NextFunction, Request, RequestHandler, Response } from "express";
import { logger } from "./logger";
import { sseClientCount } from "./sse";

// Label maps are bounded by normalizeRoute below (numeric path segments are
// collapsed), so cardinality stays proportional to the route table.
const httpRequestsTotal = new Map<string, number>(); // "method|route|status"
const httpDurationSumMs = new Map<string, number>(); // "method|route"
const httpDurationCount = new Map<string, number>(); // "method|route"
const trpcErrorsTotal = new Map<string, number>(); // "path|code"

function bump(map: Map<string, number>, key: string, by = 1) {
  map.set(key, (map.get(key) ?? 0) + by);
}

// Bound label cardinality: collapse numeric id segments and cap length.
export function normalizeRoute(path: string): string {
  const normalized = path.replace(/\/\d+(?=\/|$)/g, "/:id");
  return normalized.length > 120 ? normalized.slice(0, 120) : normalized;
}

/** Express middleware: per-request count/duration metrics + structured access log. */
export function metricsMiddleware(): RequestHandler {
  return (req: Request, res: Response, next: NextFunction) => {
    const start = process.hrtime.bigint();
    res.on("finish", () => {
      const durationMs = Number(process.hrtime.bigint() - start) / 1e6;
      const route = normalizeRoute(req.baseUrl + (req.route?.path ?? req.path));
      const key = `${req.method}|${route}`;
      bump(httpRequestsTotal, `${key}|${res.statusCode}`);
      httpDurationSumMs.set(key, (httpDurationSumMs.get(key) ?? 0) + durationMs);
      bump(httpDurationCount, key);
      // Structured access log (JSON lines in production, see logger.ts).
      logger.info("http request", {
        route,
        method: req.method,
        status: res.statusCode,
        durationMs: Math.round(durationMs * 100) / 100,
      });
    });
    next();
  };
}

/** Called from the tRPC onError hook. */
export function recordTrpcError(path: string, code: string) {
  bump(trpcErrorsTotal, `${path}|${code}`);
}

function renderCounter(
  name: string, help: string, labels: string[], map: Map<string, number>,
): string {
  const lines = [`# HELP ${name} ${help}`, `# TYPE ${name} counter`];
  map.forEach((value, key) => {
    const parts = key.split("|");
    const labelStr = labels.map((l, i) => `${l}="${parts[i] ?? ""}"`).join(",");
    lines.push(`${name}{${labelStr}} ${value}`);
  });
  return lines.join("\n");
}

/** Render the Prometheus text exposition format. */
export function renderMetrics(): string {
  const sections = [
    renderCounter(
      "campaign_http_requests_total",
      "HTTP requests by method, route and status code.",
      ["method", "route", "status"],
      httpRequestsTotal,
    ),
    renderCounter(
      "campaign_http_request_duration_ms_count",
      "HTTP request duration sample count by route.",
      ["method", "route"],
      httpDurationCount,
    ),
    renderCounter(
      "campaign_http_request_duration_ms_sum",
      "HTTP request duration sum in milliseconds by route.",
      ["method", "route"],
      httpDurationSumMs,
    ),
    renderCounter(
      "campaign_trpc_errors_total",
      "tRPC procedure errors by path and error code.",
      ["path", "code"],
      trpcErrorsTotal,
    ),
    [
      "# HELP campaign_sse_active_clients Active War Room SSE client connections.",
      "# TYPE campaign_sse_active_clients gauge",
      `campaign_sse_active_clients ${sseClientCount()}`,
    ].join("\n"),
    [
      "# HELP campaign_process_uptime_seconds Process uptime in seconds.",
      "# TYPE campaign_process_uptime_seconds gauge",
      `campaign_process_uptime_seconds ${Math.round(process.uptime())}`,
    ].join("\n"),
  ];
  return sections.join("\n") + "\n";
}

function safeEqual(a: string, b: string): boolean {
  const ab = Buffer.from(a);
  const bb = Buffer.from(b);
  return ab.length === bb.length && timingSafeEqual(ab, bb);
}

/**
 * Guard for GET /metrics — see this file's header for the policy.
 */
export function metricsAuthGuard(req: Request, res: Response, next: NextFunction) {
  const token = process.env.METRICS_BEARER_TOKEN ?? "";
  if (token) {
    if (!safeEqual(req.headers.authorization ?? "", `Bearer ${token}`)) {
      res.setHeader("WWW-Authenticate", 'Bearer realm="metrics"');
      res.status(401).json({ error: "unauthorized" });
      return;
    }
    next();
    return;
  }
  if (process.env.NODE_ENV === "production") {
    // SECURITY: fail closed — never expose operational metrics unauthenticated
    // in production because an operator forgot to set the token.
    logger.error("metrics: /metrics requested but METRICS_BEARER_TOKEN is not configured in production");
    res.status(503).json({ error: "metrics endpoint disabled: set METRICS_BEARER_TOKEN" });
    return;
  }
  next();
}
