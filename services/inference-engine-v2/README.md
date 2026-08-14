# inec-inference-engine (inference-engine-v2)

> **Canonicality note (R4-34 / R4-54 reconciliation, audit Round 4).**
> Two Rust inference services coexist: `services/inference-engine` (**v1**)
> and `services/inference-engine-v2` (**this crate**).
>
> - **v1 is the deployed/canonical service today**: all compose references
>   (`docker-compose.yml`, `docker-compose.ml.yml`) build and route to
>   `inference-engine`, not v2.
> - **v2 is a functional superset** (it adds `/liveness/predict`,
>   `/face/compare`, `/graph/neighborhood`, `/gps/spoof-detect`), but it is
>   NOT referenced by any compose/k8s manifest.
> - **Route parity gap:** `docker-compose.yml` sets
>   `BIOMETRIC_LIVENESS_URL=http://inference-engine:8091/liveness/predict`,
>   but **v1 has no `/liveness/predict` route** — that URL 404s against v1.
>   Either v1 must grow `/liveness/predict` parity, or compose must point at
>   v2. This is a product/deployment decision; v2 is deliberately NOT deleted
>   until that decision is recorded.
> - v2 unit tests are ENV_BLOCKED in the audit sandbox: the prebuilt ONNX
>   Runtime binary requires glibc >= 2.38 (sandbox: Debian 12 / glibc 2.36).
>   `cargo check` passes; `cargo test` must run on a glibc >= 2.38 runner.

Rust ML inference service (v2): ONNX Runtime (CPU) serving for the anomaly
XGBoost model, face-embedding comparison (ArcFace), CDCN liveness
detection, Neo4j graph neighborhood queries and GPS spoof detection. Axum
service, default port 8091.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/health` | public | Health with per-model loaded status |
| POST | `/anomaly/predict` | API key | Single anomaly prediction (`xgboost-onnx-v1.0`) |
| POST | `/anomaly/batch` | API key | Batch anomaly prediction |
| POST | `/face/compare` | API key | Face-embedding comparison |
| POST | `/liveness/predict` | API key | Liveness (anti-spoof) prediction |
| POST | `/graph/neighborhood` | API key | Neo4j neighborhood query |
| POST | `/gps/spoof-detect` | API key | GPS spoofing detection |

## Authentication

All endpoints except `/health` require the `x-api-key` header.

- `INFERENCE_API_KEY` — **required**. Accepts a comma-separated list
  (`KEY1,KEY2`) so key rotation is possible without downtime; any listed
  key authenticates. Comparison is constant-time (length check, then
  XOR-accumulate). When unset/empty the service fails closed: every
  non-`/health` request returns `503` — biometric inference is never
  served unauthenticated.
- Wrong/missing key → `401`.

## Environment

| Variable | Required | Default | Notes |
|---|---|---|---|
| `INFERENCE_API_KEY` | yes | — | Comma-separated list allowed (rotation) |
| `MODELS_DIR` | no | `./models` | Directory with the ONNX artifacts (`anomaly_xgboost.onnx`, `arcface_embedding.onnx`, liveness CDN model) |
| `PORT` | no | `8091` | Listen port |
| `ORT_DYLIB_PATH` | no | static CPU ORT | Override to link a dynamic ONNX Runtime |
| `CORS_ORIGINS` | no | `http://localhost:3000,http://localhost:5173` | Comma-separated allowed origins; set explicitly in production |
| `RUST_LOG` | no | — | tracing/env filter |

## Health / readiness semantics

`GET /health` is public and returns `200` with a per-component payload
(`models.anomaly_xgboost`, `face_embeddings`, `liveness_cdcn`,
`neo4j_connected`) so operators can see which models actually loaded. It
is a liveness/status signal; gate rollout on the model flags, not just the
HTTP status.

## Container

Multi-stage Dockerfile built from the **repo root** (it copies the ONNX
artifacts from `services/ml-models/biometric-python/models/`); non-root
runtime user:

```
docker build -f services/inference-engine-v2/Dockerfile .
```
