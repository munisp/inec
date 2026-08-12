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
#                              returns 503 for all non-/health requests when unset.
#                              Accepts a comma-separated list ("KEY1,KEY2") so key
#                              rotation is possible without downtime; any listed
#                              key authenticates (constant-time comparison).
# Tenancy (one is REQUIRED in production; startup exits non-zero otherwise):
#   GOTV_ENGINE_PARTY_KEYS   — JSON {"<api-key>": <party_id>} enables multi-tenant
#                              mode (party derived from key, 403 on mismatch).
#                              dapr-token-only calls carry no party binding and are
#                              rejected 403 unless the Dapr token value itself is
#                              registered here.
#   GOTV_SINGLE_TENANT_MODE  — set to "true" to explicitly opt into the
#                              single-tenant fallback (party_id trusted from the
#                              request body). Refused implicitly.
# Optional environment:
#   PORT                     — listen port (default 8101)
#   ENVIRONMENT / APP_ENV    — anything but dev/test/local/staging counts as
#                              production for the fail-closed tenancy startup check
#   DAPR_API_TOKEN           — Dapr service-invocation token (falls back to the
#                              service key when unset); comma-separated list
#                              accepted for rotation
#   DATABASE_URL / REDIS_URL — enable persistence (PostgreSQL via GOTV_BACKEND_URL)
#                              and position caching; /health returns 503 with
#                              persistence:"degraded" when writes fail
#   GOTV_BACKEND_URL         — backend API used for persistence writes
#   CORS_ORIGINS             — comma-separated allowed origins; unset = deny all
#   KAFKA_REST_URL / FLUVIO_URL / OPENSEARCH_URL / TEMPORAL_FRONTEND_URL /
#   LAKEHOUSE_URL / REDIS_HTTP_URL / DAPR_HTTP_PORT
#                            — optional middleware integrations
CMD ["inec-gotv-engine"]
