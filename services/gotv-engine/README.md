# inec-gotv-engine (gotv-engine)

Rust GOTV (get-out-the-vote) engine: volunteer/driver registration, ride
matching, route optimization, polling-unit proximity search, coverage
analysis, territory partitioning, turnout prediction, isochrones and
geofencing. In-memory spatial indexes (R-tree) with optional PostgreSQL
persistence hydration/write-through. Axum service, default port 8101.

## Endpoints

All under `/gotv-engine/*` and require authentication; `/health` is public.

| Method | Path | Purpose |
|---|---|---|
| GET | `/health` | Public health/readiness (see below) |
| POST | `/gotv-engine/volunteers` | Bulk-register volunteers (max 10k/batch) |
| POST | `/gotv-engine/polling-units` | Register polling units |
| POST | `/gotv-engine/match` | Match a ride request to a driver |
| POST | `/gotv-engine/bulk-match` | Bulk ride matching |
| POST | `/gotv-engine/optimize-route` | Route optimization |
| GET | `/gotv-engine/proximity` | Nearest polling units |
| GET | `/gotv-engine/coverage/:party_id` | Coverage analysis (party-scoped) |
| GET | `/gotv-engine/middleware/status` | Optional middleware integration status |
| POST | `/gotv-engine/territories/partition` | Territory partitioning |
| POST | `/gotv-engine/turnout/predict` | Turnout prediction |
| POST | `/gotv-engine/isochrone` | Travel-time isochrone |
| POST | `/gotv-engine/geofence/check` | Geofence containment check |
| POST | `/gotv-engine/crypto/*`, `/gotv-engine/verify-keys` | Voting-crypto surface — returns `503` in this build (crypto backend not implemented; nothing is fabricated) |

A sliding-window rate limiter (~120 req/min per API key) runs after
authentication; excess requests get `429`.

## Authentication & tenancy

Two credentials are honored, both constant-time compared (length check, then
XOR-accumulate) and both accepting **comma-separated lists** (`KEY1,KEY2`)
so rotation is possible without downtime:

- `x-api-key` matched against `GOTV_ENGINE_API_KEY` (**required**; when
  unset the service fails closed with `503` for all non-`/health`
  requests).
- `dapr-api-token` matched against `DAPR_API_TOKEN` (falls back to
  `GOTV_ENGINE_API_KEY` when unset). Mere presence of the header is **not**
  authentication.

Tenancy is fail-closed — one of these modes must be configured:

- **Multi-tenant**: `GOTV_ENGINE_PARTY_KEYS='{"<api-key>": <party_id>}'`.
  The presented key is bound to a party, carried as a request extension,
  and handlers reject mismatched `party_id` with `403`. A
  dapr-token-authenticated call carries no party binding and is rejected
  with `403` (`party_binding_required`) unless the Dapr token value itself
  is registered in `GOTV_ENGINE_PARTY_KEYS` — that registration is how a
  service account is bound to a party. An `x-api-key` unknown to the map
  gets `401`.
- **Single-tenant (explicit fallback)**: `GOTV_SINGLE_TENANT_MODE=true`.
  One shared key; `party_id` from request bodies is trusted. This must be
  a deliberate opt-in.

At startup in production (`ENVIRONMENT`/`APP_ENV` unset or anything other
than dev/test/local/staging) the service **exits non-zero** when neither
mode is configured. In non-prod it boots but still answers authenticated
requests with `503 tenancy_not_configured` until one mode is set.

## Environment

| Variable | Required | Default | Notes |
|---|---|---|---|
| `GOTV_ENGINE_API_KEY` | yes | — | Comma-separated list allowed (rotation) |
| `GOTV_ENGINE_PARTY_KEYS` | one of the two | — | JSON key→party map; multi-tenant mode |
| `GOTV_SINGLE_TENANT_MODE` | one of the two | — | `true` opts into single-tenant fallback |
| `DAPR_API_TOKEN` | no | falls back to service key | Comma-separated list allowed |
| `PORT` | no | `8101` | Listen port |
| `ENVIRONMENT` / `APP_ENV` | no | production | Controls the fail-closed startup tenancy check |
| `DATABASE_URL` / `GOTV_BACKEND_URL` | no | — | Enable PostgreSQL persistence + hydration |
| `REDIS_URL` | no | — | Position caching |
| `CORS_ORIGINS` | no | deny all | Comma-separated allowed origins |
| `KAFKA_REST_URL` / `FLUVIO_URL` / `OPENSEARCH_URL` / `TEMPORAL_FRONTEND_URL` / `LAKEHOUSE_URL` / `REDIS_HTTP_URL` / `DAPR_HTTP_PORT` | no | — | Optional middleware integrations |

## Health / readiness semantics

`GET /health` reports persistence honestly:

- persistence `ok` or `disabled` → `200`, `"status": "healthy"`;
- persistence enabled but write failures have accumulated → **`503`** with
  `"status": "degraded"`, `"persistence": "degraded"` and the failure
  count, so orchestrator probes take the pod out of rotation instead of
  trusting a healthy-looking payload while writes are discarded.

## Container

Multi-stage Dockerfile (rust:1-bookworm → debian:bookworm-slim, non-root
`gotv` user). Build context is the service directory:

```
docker build -t inec-gotv-engine ./services/gotv-engine
```
