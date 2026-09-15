FROM rust:1.89-slim-trixie AS builder

# R4-39d: distro-managed pins — package versions bounded by the pinned base-image tag;
# security-sensitive packages (ca-certificates/openssl/libssl) listed explicitly to receive
# distro security updates on rebuild. See docs/SUPPLY_CHAIN_PINS.md.
RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates g++ perl make \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
ENV CARGO_HTTP_TIMEOUT=600 \
    CARGO_NET_RETRY=10 \
    CARGO_HTTP_MULTIPLEXING=false \
    OPENSSL_NO_VENDOR=1
COPY Cargo.toml Cargo.lock* ./
RUN mkdir src && printf 'fn main() {}\n' > src/main.rs && cargo build --release && rm -rf src

COPY src ./src
RUN cargo build --release

FROM debian:trixie-slim
# R4-39d: distro-managed pins — package versions bounded by the pinned base-image tag;
# security-sensitive packages (ca-certificates/openssl/libssl) listed explicitly to receive
# distro security updates on rebuild. See docs/SUPPLY_CHAIN_PINS.md.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libstdc++6 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/target/release/fluvio-stream /usr/local/bin/fluvio-stream

# Run as a non-root user; /data holds the offset checkpoint file (named volume).
RUN useradd -u 10001 app && mkdir -p /data && chown app:app /data
USER app
EXPOSE 8100
ENTRYPOINT ["/usr/local/bin/fluvio-stream"]
