# INEC Campaign Intelligence Platform

Full-stack campaign operations dashboard (React + Express/tRPC + PostgreSQL via
Drizzle ORM) for INEC-registered candidates: voter registration, polling-unit
assignments, volunteers, war-room incident tracking (with SSE live updates),
compliance, budget/fundraising, petitions, manifesto/press AI drafting, and
Monte-Carlo turnout simulation.

## Setup

```bash
pnpm install            # or: npm install --legacy-peer-deps
cp .env.example .env    # fill in JWT_SECRET and POSTGRES_URL/DATABASE_URL
pnpm db:migrate         # apply journaled SQL migrations (see policy below)
pnpm dev                # tsx watch server/_core/index.ts (Vite dev server)
```

Production:

```bash
pnpm build              # vite build + esbuild server bundle -> dist/
pnpm start              # NODE_ENV=production node dist/index.js
# or build the Docker image (see Dockerfile header for required env)
```

Checks:

```bash
pnpm check              # tsc --noEmit (must stay at 0 errors)
pnpm test               # vitest run
```

## Environment variables

| Variable | Required | Description |
| --- | --- | --- |
| `JWT_SECRET` | **yes (prod)** | Session JWT signing secret, >= 32 random chars. Boot fails without it outside development/test. |
| `POSTGRES_URL` | **yes (prod)** | PostgreSQL connection string (preferred when both are set). Boot fails in production with neither set. |
| `DATABASE_URL` | **yes (prod)** | Fallback database connection string. |
| `PORT` | no | Listen port (default 3000; Dockerfile pins 8206). |
| `NODE_ENV` | no | `development` / `test` / `production`. |
| `VITE_APP_ID` | no | Platform app id. |
| `OWNER_OPEN_ID` | no | Platform owner identity (receives `notifyOwner` alerts). |
| `BUILT_IN_FORGE_API_URL` / `BUILT_IN_FORGE_API_KEY` | no | Forge API used for LLM calls, owner notifications, storage. |
| `OAUTH_SERVER_URL` | no | Legacy external OAuth server (unused in local-login deployments). |
| `COOKIE_SAMESITE` | no | `lax` (default) / `none` / `strict` for the session cookie. |
| `COOKIE_SECURE` | no | Override the session cookie `Secure` attribute. |
| `AUTH_RETURN_SESSION_COOKIE` | no | `true` returns the session token in the `/api/login` JSON body (non-browser clients only). |
| `CAMPAIGN_ALLOW_FIXTURE_SEED` | no | `true` enables fixture seeding (`seed.all`) — ignored in production. |
| `METRICS_BEARER_TOKEN` | **yes (prod)** for `/metrics` | Bearer token required to scrape `GET /metrics`. In production the endpoint is disabled (503, fail closed) when unset; outside production it stays open for local scraping. |
| `VITE_CAMPAIGN_API_URL` | no | Client: API base URL override at build time. |
| `VITE_ANALYTICS_ENDPOINT` / `VITE_ANALYTICS_WEBSITE_ID` | no | Client: analytics integration. |
| `VITE_FRONTEND_FORGE_API_URL` / `VITE_FRONTEND_FORGE_API_KEY` | no | Client: Forge API for browser-side calls. |

See `.env.example` for a copyable template.

## Migration policy

- **Only journaled migration files are applied.** `drizzle/meta/_journal.json`
  is the source of truth; every entry must have a matching
  `drizzle/<tag>.sql` and `drizzle/meta/<idx>_snapshot.json`. Orphaned,
  unjournaled files are deleted (superseded historical copies live in git
  history — see commits prior to this hardening round).
- **Never generate SQL at deploy time.** `pnpm db:generate` is a local,
  development-only step whose output is reviewed and committed. Deployments
  run `pnpm db:migrate` (= `drizzle-kit migrate`, apply-only). `pnpm db:push`
  is kept as an alias of `db:migrate` — despite the name it does **not** run
  `drizzle-kit push` or `generate`.
- Schema edits go through `drizzle/schema.ts` → `pnpm db:generate` → review →
  commit both the `.sql` and the meta snapshot/journal.

## Runbook

- **Health:** `GET /api/v1/campaign/health` runs `SELECT 1` with a 2s timeout
  and returns 503 when the database is missing/unreachable. The OK payload
  includes `sseClients` — the number of active War Room SSE connections held
  by this process. The Docker HEALTHCHECK hits this endpoint — expect deploy
  failures (not silent ok) when the DB env is wrong.
- **Metrics:** `GET /metrics` serves Prometheus text format: request
  count/duration by route (`campaign_http_requests_total`,
  `campaign_http_request_duration_ms_{count,sum}`), tRPC errors
  (`campaign_trpc_errors_total`), and active SSE clients
  (`campaign_sse_active_clients`). Guarded by `METRICS_BEARER_TOKEN`; fails
  closed (503) in production when the token is unset.
- **Logging:** in production all server logs are structured JSON lines
  (`{level, msg, ts, route, durationMs, ...}`) on stdout/stderr
  (server/_core/logger.ts); outside production they stay human-readable.
- **Boot failures** exit the process with code 1 (previously logged and
  exited 0); orchestrators will restart/alert on crash loops.
- **Graceful shutdown:** SIGTERM/SIGINT stop the HTTP listener, end open SSE
  streams, drain the Postgres pool, then exit 0.
- **Login throttling:** 5 attempts/5 min per IP+username and 30 attempts/5 min
  per IP (spraying guard). `trust proxy` is set to 1 hop, so real client IPs
  are used — if you add more proxy hops, adjust `app.set("trust proxy", …)`.
- **Rate limits:** public petition signing is deduped per phone (24h) or per
  IP+petition when no phone is given, and capped at 5 signatures/IP/hour. AI
  endpoints are capped at 20 calls/user/hour, and the login throttle above is
  the third consumer. All limiters are backed by the shared Postgres
  `rate_limits` table (fixed windows, see server/_core/rateLimit.ts and
  `drizzle/0002_rate_limits.sql`), so they hold across processes, replicas,
  and restarts; the in-memory fallback exists only when
  `NODE_ENV !== "production"` (development/tests) and is logged. In
  production a limiter failure fails closed (the hit is denied).
- **SSE is single-process:** War Room live updates
  (`GET /api/war-room/stream`) keep their client registry in-process
  (server/_core/sse.ts), so `broadcastWarRoomUpdate` only reaches clients
  connected to the instance that handled the triggering mutation. A
  multi-instance deployment requires either **sticky sessions** at the load
  balancer (still only correct if broadcasts happen on the same instance —
  so sticky sessions alone are a mitigation, not a fix) or, properly, a
  **shared pub/sub fan-out (e.g. Redis pub/sub) between replicas** — that is
  planned future work. Until then, run a single replica. The active client
  count is visible on `/api/v1/campaign/health` (`sseClients`) and
  `/metrics` (`campaign_sse_active_clients`).
- **Invites:** campaign-team invite tokens expire 7 days after issue and are
  single-use; expired/used tokens behave like unknown ones.
- **Logs to watch:** `[tRPC]` errors (per-procedure failures), `[health]`,
  `[deadline-check]` (hourly cron handler), `[SECURITY]` JWT secret warnings.
