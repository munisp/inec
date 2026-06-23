FROM rust:1.78 AS builder

WORKDIR /app
COPY Cargo.toml .
COPY src/ src/

RUN cargo build --release

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/target/release/inec-biometric-vault /usr/local/bin/

EXPOSE 8091

CMD ["inec-biometric-vault"]
