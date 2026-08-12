# inec-hot-path (rust-hot-path)

Ultra-low-latency transaction processor for INEC election traffic:
lock-free MPMC queues (crossbeam), zero-copy Kafka consumers (optional
`kafka` feature, rdkafka), Redis cluster pipelining, TigerBeetle batch
writes, OpenSearch bulk indexing and Fluvio smart streaming, glued by a
bounded-channel pipeline engine. Axum is used only for the operator
surface (health/readiness/metrics/stats); the data plane is the pipeline.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/health` | Liveness only — `"OK"` while the process is up |
| GET | `/readyz` | Readiness — `200` only when every configured sink is reachable, `503` otherwise |
| GET | `/metrics` | Prometheus text format |
| GET | `/stats` | JSON pipeline counters |

These endpoints expose operational counters only; the service holds no
per-tenant API surface and therefore has no API-key auth — bind it to the
internal network (as docker-compose and k8s do) and scrape via the
orchestrator.

## Environment

Every value has a localhost default; nothing is required.

| Variable | Default | Notes |
|---|---|---|
| `PORT` | `9091` | Operator HTTP listen port |
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated brokers |
| `KAFKA_GROUP_ID` | `inec-hot-path` | Consumer group |
| `KAFKA_TOPICS` | `inec.results.submitted,inec.ballots.cast,inec.incidents.reported` | Comma-separated |
| `KAFKA_CONSUMERS` / `KAFKA_BATCH_SIZE` | `16` / `10000` | Consumer parallelism / batch depth |
| `REDIS_NODES` | `redis://localhost:6379` | Comma-separated cluster nodes |
| `REDIS_PIPELINE_SIZE` / `REDIS_POOL_SIZE` | `1000` / `500` | |
| `TB_ADDRESSES` / `TB_CLUSTER_ID` / `TB_BATCH_SIZE` | `localhost:3000` / `0` / `8190` | TigerBeetle |
| `OPENSEARCH_URLS` | `http://localhost:9200` | Comma-separated |
| `OS_BATCH_SIZE` / `OS_FLUSH_MS` / `OS_WORKERS` | `5000` / `1000` / `8` | Bulk writer tuning |
| `FLUVIO_ENDPOINT` / `FLUVIO_TOPICS` / `FLUVIO_WORKERS` | `localhost:9003` / `inec.stream.results,inec.stream.ballots` / `8` | |
| `CHANNEL_CAPACITY` | `5000` | Bounded internal channel (backpressure) |
| `RUST_LOG` | — | tracing/env filter |

The pipeline fails loudly: if a worker dies the process exits non-zero
rather than running a half-simulated pipeline.

## Health / readiness semantics

- `/health` — liveness; deliberately says nothing about sink connectivity.
- `/readyz` — readiness; `200` only when every sink has succeeded at least
  once and none is always-erroring, otherwise `503` with per-sink detail.
  Wire orchestrator readiness gates to `/readyz`, not `/health`.

## Container

Multi-stage Dockerfile (rust:1-bookworm → debian:bookworm-slim, non-root
`hotpath` user, `cargo build --release --locked`). The optional `kafka`
feature (librdkafka/cmake) is OFF in the default build. Build context is
the service directory:

```
docker build -t inec-rust-hot-path ./services/rust-hot-path
```
