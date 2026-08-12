# INEC Go Backend

Go services for the INEC election platform: the monolith API (`main.go`,
default port **8088**) and the per-service commands under `cmd/`
(`gotv-svc`, `gateway`, `auth-svc`, `geo-svc`, `ingestion-svc`, `dbseed`, …).

## Build, vet, test

```bash
go build ./...
go vet ./...
go test ./...
```

The module targets the Go toolchain declared in `go.mod`. Test binaries
automatically use an ephemeral JWT key, so the suite runs without exporting
secrets.

## Authentication model

### JWT (HS256)

- All API authentication uses HS256 JSON Web Tokens signed with `JWT_SECRET`
  (≥32 chars). Outside an explicit `INEC_ENV=development`, a missing or short
  secret is a **fatal startup error** — services never boot with an
  empty/ephemeral HMAC key in production.
- Shared middleware lives in `internal/authmw` (monolith equivalent:
  `middleware_auth.go`). Every service enforces the same validation:
  signature + expiry, `type == "access"` (refresh tokens never authenticate
  API or stream requests), and **jti blacklist** enforcement.
- Claims issued at login: `sub` (user id), `role`, `jti` (unique token id,
  enables server-side revocation), `type` (`access` = 1h, `refresh` = 7d),
  plus GOTV tenancy claims (`party_id` / party scoping) where applicable.
  GOTV party tenancy is enforced by `internal/gotv.AuthMiddleware`, which
  accepts a party API key (`X-API-Key`), a Bearer JWT validated against
  auth-svc, or gateway trust headers (`X-Party-ID` + `X-Internal-Service`)
  **only** when the caller also proves the `INTERNAL_SERVICE_SECRET` shared
  secret — otherwise trust headers are rejected (fail closed).

### Session revocation (jti blacklist)

Logout / session revocation records the token `jti` in the `token_blacklist`
table (in-memory set synced with Postgres; see `session_blacklist.go`).
The auth middleware and the stream authenticator both consult the blacklist,
so a revoked token dies everywhere, including open streams on reconnect.

### Cookies

Login sets two HttpOnly cookies (`handlers.go`):

| Cookie         | Path            | Contents       | Notes                                        |
| -------------- | --------------- | -------------- | -------------------------------------------- |
| `inec_token`   | `/`             | access token   | Secure + SameSite=Strict in staging/prod     |
| `inec_refresh` | `/auth/refresh` | refresh token  | only sent to the refresh endpoint            |

`Authorization: Bearer <token>` remains supported for non-browser clients;
the cookie is the browser path.

### WebSocket / SSE auth contract (cookie-based, in-process)

Browsers cannot set headers on `WebSocket` / `EventSource` connections.
Stream endpoints therefore authenticate **in-process** with the same stack
(no edge-proxy cookie→Bearer translation required):

1. claims already validated by the auth middleware (header or cookie), else
2. `Authorization: Bearer <jwt>` header, else
3. the HttpOnly **`inec_token` cookie**, else
4. `?token=<jwt>` query parameter — **dev mode only** (`GOTV_DEV_MODE=true`
   and never when `APP_ENV`/`INEC_ENV` is `production`). Query-string tokens
   leak into access logs, proxies and browser history, so they are rejected
   outright otherwise.

Every accepted credential must decode as a valid HS256 JWT with
`type == "access"` and a non-blacklisted `jti`. Failure is a `401` before
any upgrade/streaming starts (fail closed).

Applies to: `/results/ws/updates`, `/results/ws/updates/sharded`,
`/observer/stream`, `/dashboard/stream` (monolith) and `/gotv/ws` (gotv-svc,
via `internal/gotv.AuthMiddleware`).

### Dev mode (`GOTV_DEV_MODE`)

`GOTV_DEV_MODE=true` (gotv-svc `--dev`) relaxes auth for local development:
dev-login endpoints (`/auth/login`, `/auth/me`) vend dev tokens, the party
header escape hatch opens, and query-string stream tokens are accepted.
**Combining dev mode with `APP_ENV=production` or `INEC_ENV=production` is a
fatal startup error.** Outside dev mode the dev-login endpoints return 404.

## Configuration

All environment variables are documented in **`.env.example`** (the single
source of truth). Variables labeled `REQUIRED-IN-PROD` cause a fatal startup
error — or a fail-closed code path — when missing with `APP_ENV=production`
(`validateConfig` in `main.go`; gotv-svc performs its own production checks
for the gotv-deployment variables). Highlights:

- Core: `APP_ENV`, `INEC_ENV`, `DATABASE_URL`, `JWT_SECRET`, `CORS_ORIGINS`
- Inter-service trust: `INTERNAL_SERVICE_SECRET`, `TRUSTED_PROXY_CIDRS`
- TLS: `TLS_CERT_FILE`, `TLS_KEY_FILE`
- Observability: `METRICS_BEARER_TOKEN`
- GOTV: `GOTV_ENCRYPTION_KEY`, `GOTV_MOBILE_JWT_SECRET`,
  `GOTV_ANALYTICS_URL`, `GOTV_ENGINE_URL`, `GOTV_DEV_MODE` (never in prod)

### CORS

CORS is owned exclusively by middleware (`corsProductionMiddleware` /
`authmw`): an explicit origin allow-list driven by `CORS_ORIGINS`, deny by
default, fatal in production when unset, and credentials are never combined
with a wildcard. Handlers must not set `Access-Control-Allow-Origin`
themselves.

## Health & metrics endpoints

Monolith (port 8088):

| Endpoint      | Auth            | Purpose                                   |
| ------------- | --------------- | ----------------------------------------- |
| `/healthz`    | public          | deep health check (liveness)              |
| `/readiness`  | public          | readiness probe                           |
| `/metrics`    | Bearer token    | Prometheus metrics                        |

`/metrics` requires `Authorization: Bearer $METRICS_BEARER_TOKEN`. In
production a missing `METRICS_BEARER_TOKEN` is a **fatal startup error**
(fail closed); outside production the endpoint stays open for local
scraping with a warning. gotv-svc exposes `/health` and `/ready` on its own
port (default 8103).

## Layout

- `main.go` — monolith entrypoint, router, `validateConfig`, WS hub
- `middleware_auth.go` — JWT middleware, CORS, stream authenticator
- `session_blacklist.go` — jti revocation
- `internal/authmw` — shared JWT/CORS/health middleware for services
- `internal/gotv` — GOTV domain: party auth, mobile auth (phone+OTP),
  dispatch, websocket hub
- `cmd/gotv-svc` — GOTV service (campaigns, primaries, platform handlers)
- `cmd/gateway` — API gateway (routes, `INTERNAL_SERVICE_SECRET` trust headers)
