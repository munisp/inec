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

### ✅ Production-Ready Components

These components have real business logic and could be deployed:

- EC8A form validation rules
- Hierarchical SQL collation
- JWT authentication + role guards
- Geofencing (correct Haversine math)
- SSE real-time streaming
- Rate limiting
- Photo upload + storage
- Alert CRUD
- PaddleOCR + DocLing (real libraries)
- Isolation Forest anomaly detection (persisted models)
- Benford's Law statistical test

### 🔄 In Progress / Needs Hardening

These components work but need production hardening:

- WAF (regex-based only, no ML detection)
- Rate limiter (no Redis persistence in production)
- JWT (no refresh tokens or token rotation)
- Session management (no distributed invalidation)
- Biometric PAD (real but needs model fine-tuning)
- KYC pipeline (format validation, needs NIN API)

### 🧪 Under Development

These components are being built or integrated:

- Deep PAD model (CDCN) for liveness detection
- GNN for cross-PU validation
- ArcFace face embeddings for KYC
- Real-time fraud detection with XGBoost
- Video ballot counting with YOLO

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

All services use environment variables. A `.env.example` file is provided:

```bash
cp .env.example .env
# Edit .env with your configuration
```

Key environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `PG_USER` | PostgreSQL username | `inec_admin` |
| `PG_PASSWORD` | PostgreSQL password | *(required)* |
| `BIOMETRIC_MASTER_KEY` | Encryption key for biometric vault | *(required)* |
| `NIN_API_KEY` | NIN/NIMC verification API key | *(optional)* |
| `DATABASE_URL` | PostgreSQL connection string | See docker-compose.yml |

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