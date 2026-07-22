FROM rust:1.89-slim-trixie AS builder

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
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libstdc++6 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/target/release/fluvio-stream /usr/local/bin/fluvio-stream

EXPOSE 8100
ENTRYPOINT ["/usr/local/bin/fluvio-stream"]
