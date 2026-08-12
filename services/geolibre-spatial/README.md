# inec-geolibre-spatial (geolibre-spatial)

Rust spatial computation engine backing the GeoLibre integration:
high-performance geospatial analysis over polling-unit/point data.
Actix-web service, fixed listen port **8770**.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/health` | public | Liveness (static 200 payload) |
| POST | `/spatial/buffer` | API key | Buffer analysis around points |
| POST | `/spatial/voronoi` | API key | Voronoi tessellation of polling units |
| POST | `/spatial/h3` | API key | H3 hexagonal aggregation |
| POST | `/spatial/cluster` | API key | DBSCAN spatial clustering |
| POST | `/spatial/density` | API key | Kernel density estimation |
| POST | `/spatial/nearest` | API key | K-nearest neighbors |
| POST | `/spatial/convex-hull` | API key | Convex hull |
| POST | `/spatial/centroid` | API key | Centroid analysis |

## Authentication

All endpoints except `/health` require the `x-api-key` header.

- `GEOLIBRE_API_KEY` — **required**. Accepts a comma-separated list
  (`KEY1,KEY2`) so key rotation is possible without downtime; any listed
  key authenticates. Comparison is constant-time (length check, then
  XOR-accumulate). When unset/empty the service fails closed: every
  non-`/health` request returns `503`.
- Wrong/missing key → `401`.

CORS is default-deny: `CORS_ORIGINS` (comma-separated) lists the allowed
origins; when unset, no cross-origin browser requests are permitted.

## Environment

| Variable | Required | Default | Notes |
|---|---|---|---|
| `GEOLIBRE_API_KEY` | yes | — | Comma-separated list allowed (rotation) |
| `CORS_ORIGINS` | no | deny all | Comma-separated allowed origins |
| `RUST_LOG` | no | — | tracing/env filter |

## Health / readiness semantics

`GET /health` is public and returns `200` with a static payload
(service/version) while the process is up — liveness only; the service is
stateless, so there is no separate readiness signal.

## Container

Multi-stage Dockerfile (rust:1-bookworm → debian:bookworm-slim, non-root
`geolibre` user, `cargo build --release --locked`). Build context is the
service directory:

```
docker build -t inec-geolibre-spatial ./services/geolibre-spatial
```
