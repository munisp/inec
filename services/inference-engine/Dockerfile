# This Dockerfile is built from the repository root:
# docker build -f services/inference-engine/Dockerfile .
FROM rust:1.88-slim-trixie AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates g++ \
    && rm -rf /var/lib/apt/lists/*

COPY services/inference-engine/Cargo.toml /workspace/services/inference-engine/
COPY services/inference-engine/src /workspace/services/inference-engine/src

WORKDIR /workspace/services/inference-engine
ENV CARGO_HTTP_TIMEOUT=600 \
    CARGO_NET_RETRY=10 \
    CARGO_HTTP_MULTIPLEXING=false
RUN cargo build --release

# The ORT crate links the downloaded CPU ONNX Runtime archive statically. The
# resulting executable therefore needs only the standard C++ runtime at launch.

FROM debian:trixie-slim AS runtime
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libstdc++6 curl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /workspace/services/inference-engine/target/release/inec-inference-engine /usr/local/bin/inec-inference-engine
COPY ml/models/anomaly_xgboost.onnx /app/models/anomaly_xgboost.onnx
COPY services/ml-models/biometric-python/models/cdc_pad.onnx /app/models/liveness_cdcn.onnx
COPY services/ml-models/biometric-python/models/arcface_embedding.onnx /app/models/arcface_embedding.onnx

ENV MODELS_DIR=/app/models \
    RUST_LOG=info
EXPOSE 8091
CMD ["inec-inference-engine"]
