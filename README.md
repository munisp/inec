# INEC Election Platform

A comprehensive, production-grade election management platform for Nigeria's Independent National Electoral Commission.

## Architecture

| Component | Stack | Directory |
|-----------|-------|-----------|
| **Backend API** | Go (net/http), PostgreSQL/PostGIS | `inec-go-backend/` |
| **Frontend PWA** | React 18, TypeScript, Vite, MapLibre GL | `inec-frontend/` |
| **Mobile App** | React Native (Expo SDK 56) | `inec-mobile/` |
| **ML/AI Pipeline** | Python, PyTorch, XGBoost, DuckDB | `ml/` |
| **Inference Engine** | Rust | `services/inference-engine/` |
| **Stream Processing** | Rust (Fluvio) | `services/fluvio-stream/` |
| **Lakehouse Analytics** | Python (FastAPI), DuckDB, Parquet | `services/lakehouse-analytics/` |
| **Document AI** | Python (FastAPI) | `services/document-ai/` |
| **E2E Tests** | Playwright | `e2e/` |
| **Load Tests** | Go (hey), K6 | `tests/` |
| **Deployment** | Helm, Docker Compose, K8s | `helm/`, `k8s/` |

## Key Features

- **397+ API routes** — elections, collation, BVAS devices, biometrics, disputes, geofencing, real-time tracking
- **44 PWA pages** — dashboard, elections, collation, map, command center, TV dashboard, compliance, ML dashboard, and more
- **26 mobile screens** — full feature parity with PWA, native maps (iOS MapKit/Android Google Maps)
- **PostGIS geospatial** — 48,842 polling units, landmarks, heatmaps, geofence zones, spatial stats
- **Real-time tracking** — SSE live streams, official tracking, crowd density monitoring
- **AI/ML stack** — XGBoost anomaly detection, GNN fraud scoring, CDCN liveness detection, Lakehouse pipeline (Bronze→Silver→Gold)
- **Security** — AES-256-GCM vault, TLS 1.2+, pen test suite, API key rotation
- **320+ Go tests** with `-race` detection, all passing on PostgreSQL

## Prerequisites

- Go 1.21+
- Node.js 20+
- Python 3.12+
- PostgreSQL 16 with PostGIS extension
- Rust (for inference-engine and fluvio-stream services)

## Quick Start

```bash
# 1. Start PostgreSQL with PostGIS
docker run -d --name inec-postgres \
  -e POSTGRES_USER=ngapp -e POSTGRES_PASSWORD=ngapp -e POSTGRES_DB=ngapp \
  -p 5432:5432 postgis/postgis:16-3.4

# 2. Start backend
export DATABASE_URL="postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable"
cd inec-go-backend && go run .

# 3. Start frontend (new terminal)
cd inec-frontend && npm install && npm run dev

# 4. Open http://localhost:5173
```

## Make Targets

```bash
make build          # Build Go backend
make run            # Build + run backend
make test           # Run Go tests
make lint           # Run go vet
make frontend-dev   # Start frontend dev server
make dev            # Start full stack (backend + frontend)
```

## CI Pipeline

8-job GitHub Actions pipeline (`.github/workflows/ci.yml`):

1. **Go Backend** — vet, build, test (320+ tests with `-race`) on PostGIS 16
2. **Frontend** — lint, typecheck, build
3. **Mobile** — typecheck
4. **Python Analytics** — lint (ruff), import test
5. **Docker Build** — backend, frontend, analytics images
6. **E2E Tests** — Playwright on Chromium
7. **Integration Tests** — Go integration tests with PostGIS + Redis
8. **Load Tests** — hey benchmarks (dashboard, results, elections, health)

## Database

PostgreSQL-only (no SQLite). All SQL uses PostgreSQL syntax with a runtime `pgcompat` converter for DDL/DML compatibility. PostGIS extension required for geospatial features.

```
DSN: postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable
```

## ML Models

| Model | Type | Metrics | Size |
|-------|------|---------|------|
| XGBoost Anomaly | Gradient Boosting | ROC-AUC: 1.0, F1: 0.9959 | 479 KB |
| GAT GNN | Graph Attention Network | F1: 0.9988, Precision: 0.9975 | 374 KB |
| CDCN Liveness | Central Difference CNN | 6.1M params, Loss: 0.015 | 24.5 MB |

## License

Proprietary — INEC Nigeria
