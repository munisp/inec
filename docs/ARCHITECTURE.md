# INEC Platform Architecture & Service Decomposition Guide

## Current State

The Go backend is a monolithic binary: **42K lines across 76 files** in a single package (`package main`).

### Domain Boundaries (Logical)

| Domain | Files | Lines | Endpoints | Description |
|--------|-------|-------|-----------|-------------|
| **Auth & Security** | `middleware_auth.go`, `security.go`, `session_blacklist.go` | ~1,200 | 8 | JWT, RBAC, rate limiting, session management |
| **Elections & Results** | `handlers.go` (partial) | ~1,600 | 25+ | Election CRUD, result submission, collation |
| **Geospatial** | `geospatial_enhanced.go`, `geo_advanced.go` | ~3,500 | 30+ | PostGIS queries, tracking, landmarks, heatmaps |
| **Biometric Engine** | `biometric_engine.go` | ~2,000 | 15+ | BVAS, fingerprint, facial, liveness (PAD) |
| **Blockchain** | `blockchain_production.go` | ~900 | 8 | Fabric integration, audit trail, verification |
| **AI/ML Bridge** | `ml_bridge.go`, `lakehouse_bridge.go` | ~800 | 12 | Python model inference, lakehouse pipeline |
| **Middleware Hub** | `mw_*.go` (13 files) | ~3,000 | — | Kafka, Redis, Keycloak, TigerBeetle, etc. |
| **Platform** | `platform_*.go`, `production_*.go` | ~5,000 | 40+ | KYC, disputes, incidents, observer, SMS |
| **Scale & Ops** | `scale_fixes.go`, `pgpool.go`, `tracing.go` | ~1,500 | 10 | PgPool, metrics, tracing, health checks |

### Why Monolith Is Acceptable for V1

1. **Single deployment unit** — simpler ops for election day (no inter-service networking)
2. **Shared database** — all domains read/write the same PostgreSQL instance
3. **In-memory middleware hub** — graceful fallbacks without network hops
4. **No distributed transaction coordination** needed

### When to Decompose

Decompose when:
- Team grows beyond 4-5 engineers (ownership boundaries needed)
- Individual domains need independent scaling (e.g., biometric peak vs. collation peak)
- Deployment frequency differs across domains (e.g., geo changes weekly, auth changes monthly)
- Database becomes a bottleneck (read replicas not sufficient)

## Decomposition Plan (V2)

### Phase 1: Internal Packages (Low Risk)

Extract types and business logic into `internal/` packages while keeping a single binary:

```
inec-go-backend/
├── cmd/server/main.go          # Entry point, wiring
├── internal/
│   ├── auth/                   # JWT, RBAC, session, rate limiting
│   ├── election/               # Election lifecycle, results, collation
│   ├── geo/                    # PostGIS, tracking, landmarks, heatmaps
│   ├── biometric/              # BVAS, fingerprint, facial, liveness
│   ├── blockchain/             # Fabric, audit trail
│   ├── ml/                     # Model inference bridge, lakehouse
│   ├── platform/               # KYC, disputes, incidents, observer
│   ├── middleware/              # Kafka, Redis, Keycloak, TigerBeetle hub
│   └── infra/                  # PgPool, metrics, tracing, health
├── pkg/
│   ├── models/                 # Shared types (M, User, Election, Result)
│   └── pgcompat/               # PostgreSQL compatibility layer
```

**Effort:** 2-3 weeks for 1 engineer. No behavior change — same binary, just organized.

### Phase 2: API Gateway + Services (Medium Risk)

Split into independently deployable services behind an API gateway:

```
┌─────────────┐
│  APISIX GW  │  ← TLS termination, rate limiting, auth
└──────┬──────┘
       │
  ┌────┴────┬──────────┬───────────┬──────────┐
  │ Auth    │ Election │ Geo       │ Biometric│
  │ Service │ Service  │ Service   │ Service  │
  └─────────┴──────────┴───────────┴──────────┘
       │         │          │           │
  ┌────┴─────────┴──────────┴───────────┴────┐
  │           PostgreSQL + PostGIS            │
  └───────────────────────────────────────────┘
```

**Effort:** 4-6 weeks for 2-3 engineers. Requires service discovery, distributed tracing, API contracts.

### Phase 3: Event-Driven Architecture (High Risk)

Add Kafka-driven event sourcing for cross-service communication:

- Election service publishes `result.submitted` events
- Collation service consumes and aggregates
- Blockchain service consumes and creates audit entries
- ML service consumes and runs anomaly detection

**Effort:** 8-12 weeks. Requires event schema registry, saga patterns, eventual consistency handling.

## Recommended Approach

**For nationwide election deployment:** Stay with the monolith (V1). The operational simplicity of a single binary is a feature, not a bug, when you need 99.99% uptime on election day.

**Post-election:** Execute Phase 1 (internal packages) to improve code organization and enable independent testing. Evaluate Phase 2 based on team size and scaling needs.
