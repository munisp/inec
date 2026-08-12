# Multi-stage build for the INEC GOTV engine (Rust).
# Build context: services/gotv-engine (self-contained; no repo-root files
# are referenced — sources, Cargo.toml and the committed Cargo.lock only).
#
#   docker build -t inec-gotv-engine ./services/gotv-engine
#
FROM rust:1-bookworm AS builder

WORKDIR /build
COPY Cargo.toml Cargo.lock ./
COPY src/ src/

# Reproducible release build pinned by the committed Cargo.lock.
RUN cargo build --release --locked

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home --shell /usr/sbin/nologin gotv

COPY --from=builder /build/target/release/inec-gotv-engine /usr/local/bin/

USER gotv

# PORT env (default 8101, see main.rs).
EXPOSE 8101

# Required environment:
#   GOTV_ENGINE_API_KEY      — internal API key auth is fail-closed; the service
#                              returns 503 for all non-/health requests when unset
# Optional environment:
#   PORT                     — listen port (default 8101)
#   GOTV_ENGINE_PARTY_KEYS   — JSON {"<api-key>": <party_id>} enables multi-tenant
#                              mode (party derived from key, 403 on mismatch);
#                              unset = documented single-tenant fallback
#   DAPR_API_TOKEN           — Dapr service-invocation token (falls back to the
#                              service key when unset)
#   DATABASE_URL / REDIS_URL — enable persistence (PostgreSQL via GOTV_BACKEND_URL)
#                              and position caching; /health reports "degraded"
#                              when writes fail
#   GOTV_BACKEND_URL         — backend API used for persistence writes
#   CORS_ORIGINS             — comma-separated allowed origins; unset = deny all
#   KAFKA_REST_URL / FLUVIO_URL / OPENSEARCH_URL / TEMPORAL_FRONTEND_URL /
#   LAKEHOUSE_URL / REDIS_HTTP_URL / DAPR_HTTP_PORT
#                            — optional middleware integrations
CMD ["inec-gotv-engine"]
