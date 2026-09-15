# Build context is the repo root (see .github/workflows/ci.yml docker-build
# matrix). db.rs embeds the schema via include_str!("../migrations/001_biometric_tables.sql"),
# which resolves to services/biometric-rust/migrations/ — the SERVICE-local
# migrations directory, not the repo-root migrations/. Both that directory and
# the committed Cargo.lock must be copied or the release build fails.
FROM rust:1-bookworm AS builder

WORKDIR /build
COPY services/biometric-rust/migrations/ services/biometric-rust/migrations/
COPY services/biometric-rust/Cargo.toml services/biometric-rust/Cargo.lock services/biometric-rust/
COPY services/biometric-rust/src/ services/biometric-rust/src/

WORKDIR /build/services/biometric-rust
RUN cargo build --release --locked

FROM debian:bookworm-slim

# R4-39d: distro-managed pins — package versions are bounded by the pinned
# base-image tag; security-sensitive packages (ca-certificates/openssl/libssl) are
# listed explicitly so they receive distro security updates on rebuild. See docs/SUPPLY_CHAIN_PINS.md.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl libssl3 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /build/services/biometric-rust/target/release/inec-biometric-vault /usr/local/bin/

EXPOSE 8091

# Run as an unprivileged user (hardening): the vault listens on 8091 (>1024)
# and needs no root capabilities.
RUN groupadd --system app && useradd --system --gid app --no-create-home app
USER app

CMD ["inec-biometric-vault"]
