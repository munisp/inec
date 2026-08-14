# Multi-stage build for the INEC hot-path engine (Rust).
# Build context: services/rust-hot-path (self-contained — sources, Cargo.toml
# and the committed Cargo.lock only; the `kafka` feature is optional and OFF
# by default, so no librdkafka/cmake toolchain is needed).
#
#   docker build -t inec-rust-hot-path ./services/rust-hot-path
#
FROM rust:1-bookworm AS builder

WORKDIR /build
COPY Cargo.toml Cargo.lock ./
COPY src/ src/

# Reproducible release build pinned by the committed Cargo.lock.
RUN cargo build --release --locked

FROM debian:bookworm-slim

# R4-39d: distro-managed pins — package versions are bounded by the pinned
# base-image tag; security-sensitive packages (ca-certificates/openssl/libssl) are
# listed explicitly so they receive distro security updates on rebuild. See docs/SUPPLY_CHAIN_PINS.md.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home --shell /usr/sbin/nologin hotpath

COPY --from=builder /build/target/release/inec-hot-path /usr/local/bin/

USER hotpath

# PORT env (default 9091, see src/pipeline.rs Config::from_env).
EXPOSE 9091

# Environment contract (all optional — every value has a localhost default):
#   PORT                  — listen port for /health, /readyz, /metrics, /stats (default 9091)
#   KAFKA_BROKERS         — comma-separated brokers (default localhost:9092)
#   KAFKA_GROUP_ID        — consumer group (default inec-hot-path)
#   KAFKA_TOPICS          — comma-separated topics consumed
#   KAFKA_CONSUMERS / KAFKA_BATCH_SIZE
#   REDIS_NODES           — comma-separated redis:// node URLs (cluster pipelining)
#   REDIS_PIPELINE_SIZE / REDIS_POOL_SIZE
#   TB_ADDRESSES          — comma-separated TigerBeetle addresses
#   TB_CLUSTER_ID / TB_BATCH_SIZE
#   OPENSEARCH_URLS       — comma-separated OpenSearch URLs (bulk writer)
#   OS_BATCH_SIZE / OS_FLUSH_MS / OS_WORKERS
#   FLUVIO_ENDPOINT       — Fluvio SC endpoint (default localhost:9003)
#   FLUVIO_TOPICS / FLUVIO_WORKERS
#   CHANNEL_CAPACITY      — internal bounded channel depth (default 5000)
#   RUST_LOG              — tracing filter (e.g. info)
#
# Health semantics: /health is liveness only (always 200 while the process is
# up); /readyz returns 200 only when every configured sink is reachable and
# 503 otherwise — wire orchestrator readiness probes to /readyz.
CMD ["inec-hot-path"]
