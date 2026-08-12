# inec-inference-engine (inference-engine, v1)

Rust ML inference service (v1): ONNX Runtime (CPU) anomaly XGBoost
inference with model governance (signed manifest approval), face-embedding
comparison, Neo4j graph queries, GPS spoof detection, and an Ed25519
integrity signer for attestation envelopes (including BVAS device
envelopes). Axum service, default port 8091.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/health` | public | Readiness-style health (see below) |
| POST | `/anomaly/predict` | network-internal | Anomaly prediction (governance-gated) |
| POST | `/anomaly/batch` | network-internal | Batch anomaly prediction |
| POST | `/face/compare` | network-internal | Face-embedding comparison |
| POST | `/graph/neighborhood` | network-internal | Neo4j neighborhood query |
| POST | `/gps/spoof-detect` | network-internal | GPS spoofing detection |
| POST | `/integrity/sign` | Bearer token | Sign an integrity attestation |
| POST | `/integrity/verify` | Bearer token | Verify an integrity signature |
| POST | `/integrity/device/verify` | Bearer token | Verify a signed BVAS device envelope |
| GET/POST | `/integrity/health` | public | Integrity-signer readiness |

## Authentication

- `/integrity/*` operations require `Authorization: Bearer <token>` matched
  against `INTEGRITY_SERVICE_TOKEN`. The comparison is constant-time
  (`subtle::ConstantTimeEq`, length-checked first) and the variable accepts
  a **comma-separated list** (`TOKEN1,TOKEN2`) so token rotation is
  possible without downtime; any listed token authorizes. When unset,
  integrity operations are never authorized (fail-closed).
- The inference endpoints (`/anomaly/*`, `/face/*`, `/graph/*`,
  `/gps/*`) carry no per-request credential in this v1 service; they are
  protected by network isolation only (the compose/k8s deployments keep
  the port on the internal network and route all external traffic through
  the gateway). Do not expose this port publicly. The hardened
  `inference-engine-v2` service enforces `x-api-key` auth on the same
  inference surface and is the recommended deployment.
- CORS is currently permissive in this binary, which is safe only under
  the network-isolation assumption above.

## Environment

| Variable | Required | Default | Notes |
|---|---|---|---|
| `INTEGRITY_SERVICE_TOKEN` | for `/integrity/*` | — | Comma-separated list allowed (rotation) |
| `INTEGRITY_SIGNER_KEY_ID` | for signing | `unconfigured` | Key id embedded in attestations |
| `INTEGRITY_SIGNING_KEY` | for signing | — | Base64-encoded 32-byte Ed25519 secret key |
| `INTEGRITY_VERIFYING_KEY` | no | derived | Base64 32-byte Ed25519 public key (verify-only mode) |
| `MODELS_DIR` | no | `./models` | ONNX artifacts + `anomaly_xgboost.manifest.json` governance manifest (mounted read-only in the container) |
| `PORT` | no | `8091` | Listen port |
| `RUST_LOG` | no | — | tracing/env filter |

## Health / readiness semantics

`GET /health` is readiness-style: `200` only when the anomaly model is
loaded **and** its governance manifest is approved; otherwise `503` with
`"status": "degraded"` and per-component detail (`models.*`, including
`integrity_signer` readiness). `/integrity/health` separately reports
whether the signer is fully configured (signing key + token + key id).

## Container

Multi-stage Dockerfile (rust:1.88-bookworm → debian:bookworm-slim,
non-root user, `cargo build --release --locked`, curl-based healthcheck on
`/health`). Build context is the service directory:

```
docker build -t inec-inference-engine ./services/inference-engine
```
