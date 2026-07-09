# Build context is the repo root (see .github/workflows/ci.yml docker-build
# matrix): the vault embeds the shared migrations/ SQL via include_str!, which
# lives outside the service directory. The repo layout is preserved inside the
# image so the relative include path resolves during compilation.
FROM rust:1-bookworm AS builder

WORKDIR /build
COPY migrations/ migrations/
COPY services/biometric-rust/Cargo.toml services/biometric-rust/
COPY services/biometric-rust/src/ services/biometric-rust/src/

WORKDIR /build/services/biometric-rust
RUN cargo build --release

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /build/services/biometric-rust/target/release/inec-biometric-vault /usr/local/bin/

EXPOSE 8091

CMD ["inec-biometric-vault"]
