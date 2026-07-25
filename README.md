# INEC — Independent National Electoral Commission Platform

A production-grade, full-stack election management system built for Nigeria's electoral infrastructure.

> **Note:** This project is actively under development. Some components are production-ready, while others are still being integrated. See the [Production Readiness](#production-readiness) section for details.

## 📋 Overview

INEC is a microservices monorepo that provides end-to-end election management capabilities:

- **Voter registration & biometric verification** (fingerprint, facial, iris)
- **Real-time election result collation** (PU → Ward → LGA → State → National)
- **AI-powered anomaly detection** and fraud monitoring
- **Document AI** for EC8A form OCR and processing
- **Blockchain-style audit trails**
- **Multi-platform delivery**: Web, Mobile (iOS/Android), Desktop

## 🏗️ Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                       INEC Platform                              │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Frontend Layer                                                  │
│  ├── inec-frontend/     React/TypeScript SPA (46+ pages)        │
│  ├── inec-mobile/       Expo/React Native (35+ screens)         │
│  └── desktop/           Electron desktop app                    │
│                                                                  │
│  Application Layer                                               │
│  ├── inec-go-backend/   Go 1.22+ REST API (main services)       │
│  ├── inec-backend/      Python proxy service                    │
│  └── inec-analytics/    DuckDB lakehouse analytics              │
│                                                                  │
│  AI/ML Services                                                │
│  ├── services/biometric-python/  Face, fingerprint, iris, PAD   │
│  ├── services/biometric-rust/    Crypto vault + matching        │
│  ├── services/biometric-go/      ABIS pipeline                  │
│  ├── services/document-ai/       PaddleOCR + DocLing            │
│  └── services/lakehouse-analytics/ Isolation Forest + Benford    │
│                                                                  │
│  Infrastructure (Docker Compose)                                │
│  ├── PostgreSQL (Primary + Replica, streaming replication)      │
│  ├── Pgpool-II (Connection pooling, HA failover)               │
│  ├── Redis (Caching, sessions)                                  │
│  ├── Kafka (Event streaming, KRaft mode)                        │
│  ├── Temporal (Workflow orchestration)                          │
│  ├── Keycloak (Identity/SSO)                                    │
│  ├── Permify (Authorization)                                    │
│  ├── APISIX (API gateway, WAF)                                  │
│  ├── TigerBeetle (Double-entry ledger)                          │
│  ├── Fluvio (Streaming data)                                    │
│  └── Dapr (Sidecar primitives)                                  │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

## 🚀 Quick Start

### Prerequisites

- Docker & Docker Compose (v2.20+)
- Go 1.22+
- Python 3.11+
- Node.js 18+
- Rust 1.75+ (for biometric services)

### Start All Services

```bash
# Start infrastructure + application services
docker compose up -d

# Start ML-specific services (GPU recommended)
docker compose -f docker-compose.ml.yml up -d
```

### Verify Services

```bash
# Check all containers are running
docker compose ps

# View logs
docker compose logs -f go-backend

# Access services
curl http://localhost:8088/health          # Go Backend
curl http://localhost:3000                  # Frontend (nginx)
curl http://localhost:8090/health           # Lakehouse Analytics
curl http://localhost:8180/admin            # Keycloak Admin
```

### Build Individual Services

```bash
# Go backend
cd inec-go-backend && go build ./...

# Python services
cd services/biometric-python && pip install -r requirements.txt
cd services/document-ai && pip install -r requirements.txt
cd services/lakehouse-analytics && pip install -r requirements.txt

# Frontend
cd inec-frontend && npm install && npm run build

# Rust services
cd services/biometric-rust && cargo build
```

## 📁 Project Structure

```
inec/
├── inec-go-backend/              # Go backend API (main application)
├── inec-frontend/                # React/TypeScript web frontend
├── inec-mobile/                  # Expo/React Native mobile app
├── inec-analytics/               # Python DuckDB analytics lakehouse
├── desktop/                      # Electron desktop application
├── benchmarks/                   # Performance benchmarks
├── config/                       # Service configurations
│   ├── postgres/                 # Database initialization scripts
│   ├── pgpool/                   # Pgpool-II configuration
│   ├── keycloak/                 # Keycloak realm exports
│   ├── apisix/                   # APISIX gateway config
│   └── dapr/                     # Dapr components
├── helm/                         # Kubernetes Helm charts
├── k8s/                          # Kubernetes manifests
├── e2e/                          # End-to-end tests
├── services/                     # Specialized microservices
│   ├── biometric-python/         # Biometric processing + ML
│   ├── biometric-rust/           # Cryptographic operations
│   ├── biometric-go/             # ABIS pipeline
│   ├── document-ai/              # OCR + document analysis
│   └── lakehouse-analytics/      # Anomaly detection
├── docker-compose.yml            # Full orchestration (~20 services)
├── docker-compose.ml.yml         # ML-specific services
├── Makefile                      # Build automation
├── AI_ML_PRODUCTION_AUDIT.md     # AI/ML component audit
└── AUDIT_REPORT.md               # General audit report
```

## 🔑 Key Features

### Electoral Operations

| Feature | Description | Status |
|---------|-------------|--------|
| EC8A Form Validation | 7 INEC-specific validation rules | ✅ Production |
| Hierarchical Collation | SQL-based vote aggregation across 5 tiers | ✅ Production |
| Ballot Reconciliation | Cross-reference accredited vs. cast votes | ✅ Production |
| Geofencing | Haversine-based location validation | ✅ Production |
| Observer SSE Streaming | Real-time election result monitoring | ✅ Production |
| JWT Auth + RBAC | Role-based access control | ✅ Production |
| Registration Role Lock | Prevents admin self-assignment | ✅ Production |

### AI/ML Components

| Component | Description | Status |
|-----------|-------------|--------|
| Anomaly Detection | Isolation Forest with persisted models | ✅ Production |
| Benford's Law | First-digit frequency analysis | ✅ Production |
| Biometric Verification | Fingerprint, facial, iris matching | 🔄 In Progress |
| PAD (Liveness) | CDCN-based presentation attack detection | 🔄 In Progress |
| Document AI | PaddleOCR for EC8A form extraction | ✅ Production |
| GNN Cross-Validation | Geographic adjacency graph analysis | 🔄 In Progress |

### Infrastructure

| Component | Version | Purpose |
|-----------|---------|---------|
| PostgreSQL | 16 | Primary + replica with streaming replication |
| Pgpool-II | 4.5 | Connection pooling, load balancing, HA failover |
| Redis | 7 | Caching, sessions |
| Kafka | 7.5.0 | Event streaming (KRaft mode) |
| Temporal | 1.22 | Workflow orchestration |
| Keycloak | 23.0 | Identity/SSO |
| Permify | Latest | Fine-grained authorization |
| APISIX | 3.7.0 | API gateway, WAF |
| TigerBeetle | 0.15.3 | Double-entry ledger |

## 🔒 Security

- JWT authentication with role-based access control
- Pgpool-II with pool_hba.conf for connection-level authentication
- APISIX WAF with SQL injection pattern detection
- CSRF protection middleware
- Session revocation via database blacklist
- Biometric master key from environment variable (never hardcoded)
- Model files stored in non-world-writable directories

## 📊 Production Readiness

The platform source is designed to **fail closed** when authoritative election data, approved model artifacts, or external verification providers are unavailable. It does not treat demo records, neutral model scores, placeholder OCR output, or synthetic geospatial observations as production data. Source-controlled readiness is necessary but not sufficient for an official deployment: the Kasicloud host, real credentials, data provenance, model approval evidence, provider contracts, backups, and operational ownership must be verified separately.

### Kasicloud Deployment Go/No-Go Checklist

> The supported production target for this repository is **Kasicloud with Docker Compose**. This deployment guide does not require or prescribe AWS services.

| Gate | Required evidence before launch | Failure condition |
|---|---|---|
| Secrets and origins | Copy `.env.production.example` to the deployment host as `.env`; populate every required secret with approved values; set the actual browser origins; restrict the file to the deployment account. | Empty, development, shared, or committed credentials. |
| Database and recovery | Verify PostgreSQL primary/replica/Pgpool health, encrypted backups, restore rehearsal, migration completion, and least-privilege service accounts. | Any database role, backup, or restore procedure is unverified. |
| Identity and authorization | Verify Keycloak realm import, client secret, real administrator/officer/observer accounts, Permify policy, and session/role controls against the production URL. | Bootstrap, default, or unapproved accounts remain enabled. |
| Authoritative election data | Confirm the production database contains approved polling-unit, voter, result, and geographic records with documented provenance. | Any dashboard or workflow would rely on demo, seed, or synthetic records. |
| Model governance | Mount a protected `MODELS_DIR` containing the approved ONNX artifact and its matching manifest. Follow [MODEL_GOVERNANCE.md](MODEL_GOVERNANCE.md). | Manifest missing, hash mismatch, approval false, or validation evidence unavailable. |
| Document, identity, and satellite providers | Configure real VLM/OCR dependencies, authorized NIMC/identity/sanctions providers, and an approved STAC collection. Exercise both successful and provider-unavailable paths. | A provider is absent, unauthorized, or produces an unverified response. |
| Compose health | Run `docker compose --env-file .env -f docker-compose.yml config --quiet`, then start the stack and require all core dependencies to become healthy. Inference, anomaly, document AI, satellite, campaign-planning, database, and backend health must be green. | A required service is merely running, degraded, or retrying rather than healthy. |
| Browser verification | Verify authenticated and public flows on `https://inec.servers.upi.dev/` and `https://campaign.inec.servers.upi.dev/`, including mobile layouts, hash/deep links, unavailable states, and destructive-control accessibility. | Any frontend displays plausible fallback data after an API or provider failure. |
| Security and operations | Confirm TLS, CORS allow-lists, APISIX/OpenAppSec enforcement, monitoring, logs, alert recipients, incident runbooks, patching, and change-control approval. | Monitoring, escalation, or rollback ownership is absent. |
| Rollback | Record the prior Compose image set, database migration plan, model artifact/manifest pair, and tested rollback command before promotion. | A release cannot be reverted without improvisation. |

### Production Deployment Sequence

```bash
# On the protected Kasicloud host only.
cp .env.production.example .env
chmod 600 .env
# Populate .env from the approved secret-management process; never paste real values into Git.

docker compose --env-file .env -f docker-compose.yml config --quiet
docker compose --env-file .env -f docker-compose.yml pull
docker compose --env-file .env -f docker-compose.yml up -d

docker compose --env-file .env -f docker-compose.yml ps
docker compose --env-file .env -f docker-compose.yml logs --tail=200 go-backend inference-engine ai-anomaly-detection document-ai satellite-change-detection
```

The inference engine intentionally reports `503` when its approved model artifact or manifest is unavailable. The anomaly gateway and backend then report the dependency as unavailable; do **not** bypass this gate by substituting a score or editing the health check.

### Source-Controlled Assurance Scope

| Area | Source-control status | External prerequisite |
|---|---|---|
| Runtime demo/fixture paths | Production runtime paths are gated or fail closed. | Deployment environment must not enable explicit test/fixture flags. |
| Anomaly scoring | Governance manifest, SHA-256 match, approval gate, and unavailable propagation are enforced. | Approved model artifact, manifest, validation report, and approver decision. |
| Document/KYC analysis | Unavailable providers return explicit unavailable or pending-review states. | Real OCR/VLM/NIMC/identity/sanctions credentials and provider approvals. |
| Geo and satellite analytics | Missing source data or STAC configuration produces unavailable status. | Authoritative PostgreSQL geo records and approved STAC access. |
| Infrastructure | Compose requires core secrets and includes health-gated dependencies. | Kasicloud host security, TLS/DNS, backups, monitoring, and operator runbooks. |

## 🧪 Testing

```bash
# Run Go backend tests
cd inec-go-backend && go test ./...

# Run Python service tests
cd services/biometric-python && pytest

# Run e2e tests
cd e2e && npm test

# Full test suite
make test
```

## 📝 Configuration

All services use environment variables. Use the local template only for development; use the production template on the protected Kasicloud host:

```bash
# Development only
cp .env.example .env

# Production only: populate from approved secret management, then protect the file
cp .env.production.example .env
chmod 600 .env
```

Key production environment variables:

| Variable | Description | Production default |
|----------|-------------|--------------------|
| `PG_PASSWORD` | PostgreSQL service credential | **None; required.** |
| `HSM_MASTER_KEY` | Platform cryptographic root material | **None; required.** |
| `APISIX_ADMIN_KEY` | API gateway administration secret | **None; required.** |
| `KEYCLOAK_CLIENT_SECRET` | OIDC client credential | **None; required.** |
| `VLM_ENDPOINT` | Approved vision-analysis provider endpoint | **None; required for Document AI.** |
| `NIMC_VERIFICATION_URL` | Authorized identity verification provider | **None; required for authoritative NIN verification.** |
| `STAC_API_URL` | Approved real satellite/STAC provider | **None; required for satellite analysis.** |
| `MODELS_DIR` | Protected directory holding approved model and manifest | `/app/models` inside the inference container. |

## 📚 API Documentation

After starting the services:

- Go Backend: `http://localhost:8088/docs` (Swagger)
- Lakehouse Analytics: `http://localhost:8090/docs` (Swagger)
- Keycloak Admin: `http://localhost:8180/admin`
- Temporal UI: `http://localhost:8233`

## 🤝 Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Submit a pull request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🔍 Audits & Assessments

- **[AI/ML Production Audit](AI_ML_PRODUCTION_AUDIT.md)** — Detailed assessment of AI/ML component readiness
- **[General Audit Report](AUDIT_REPORT.md)** — Overall platform audit findings

## 🙏 Acknowledgments

Built for the Independent National Electoral Commission (INEC), Nigeria.