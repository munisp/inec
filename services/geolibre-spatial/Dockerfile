# Multi-stage build for the INEC GeoLibre spatial analysis engine (Rust).
# Build context: services/geolibre-spatial (self-contained — sources,
# Cargo.toml and the committed Cargo.lock only).
#
#   docker build -t inec-geolibre-spatial ./services/geolibre-spatial
#
FROM rust:1-bookworm AS builder

WORKDIR /build
COPY Cargo.toml Cargo.lock ./
COPY src/ src/

# Reproducible release build pinned by the committed Cargo.lock.
RUN cargo build --release --locked

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl libssl3 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home --shell /usr/sbin/nologin geolibre

COPY --from=builder /build/target/release/inec-geolibre-spatial /usr/local/bin/

USER geolibre

# Listen port is fixed at 8770 (see src/main.rs).
EXPOSE 8770

# Required environment:
#   GEOLIBRE_API_KEY  — internal API key; auth is fail-closed (503 for all
#                       non-/health requests when unset). Comma-separated
#                       values ("KEY1,KEY2") are accepted so key rotation is
#                       possible without downtime; any listed key
#                       authenticates (constant-time comparison).
# Optional environment:
#   CORS_ORIGINS      — comma-separated allowed origins; unset = deny all
#                       cross-origin requests
#
# Health semantics: /health is public and returns 200 with a static payload
# while the process is up (liveness).
CMD ["inec-geolibre-spatial"]
