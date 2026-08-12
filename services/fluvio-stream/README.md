# fluvio-stream

Rust bridge between the INEC platform and Fluvio topics: produces
audit/election events into Fluvio, consumes them back over HTTP, and
checkpoints consumer offsets to a local file. Actix-web service.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/health` | public | Liveness + stats payload |
| GET | `/stats` | API key | Producer/consumer counters |
| GET | `/topics` | API key | List ensured INEC topics |
| POST | `/produce` | API key | Produce an event to a topic |
| GET | `/consume` | API key | Consume events (checkpointed offsets) |

## Authentication

All endpoints except `/health` require the `x-api-key` header.

- `FLUVIO_STREAM_API_KEY` — **required**. Accepts a comma-separated list
  (`KEY1,KEY2`) so key rotation is possible without downtime; any listed key
  authenticates. Comparison is constant-time (length check, then
  XOR-accumulate). When unset/empty the service fails closed: every
  non-`/health` request returns `503` (forged audit/election events must
  never be producible by unauthenticated callers).
- Wrong/missing key → `401`.

## Environment

| Variable | Required | Default | Notes |
|---|---|---|---|
| `FLUVIO_STREAM_API_KEY` | yes | — | Comma-separated list allowed (rotation) |
| `FLUVIO_ENDPOINT` | no | `localhost:9003` | Fluvio SC endpoint |
| `PORT` | no | `8100` | Listen port |
| `CHECKPOINT_FILE` | no | local file | Offset checkpoint path (mount a volume in containers) |
| `RUST_LOG` | no | — | tracing/env filter |

## Health / readiness semantics

`GET /health` is public and returns `200` with service stats while the
process is up (liveness). It does not verify Fluvio connectivity.

## Container

Multi-stage Dockerfile (rust slim-trixie builder → debian:trixie-slim,
non-root user, `/data` volume for the checkpoint file). Build context is
the service directory:

```
docker build -t inec-fluvio-stream ./services/fluvio-stream
```
