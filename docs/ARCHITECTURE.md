# INEC Platform — Service Architecture

## Overview

The INEC platform is designed as a **modular monolith** — a single Go binary organized into independent service domains with clear boundaries. This architecture allows:

1. **Single binary deployment** for simplicity
2. **Independent scaling** via Kubernetes pod autoscaling with service-specific resource limits
3. **Future microservice extraction** — each domain can be split into a separate service when needed

## Service Domains

```
┌────────────────────────────────────────────────────────────────┐
│                    INEC Platform Backend                       │
│                                                                │
│  ┌─────────────┐  ┌─────────────┐  ┌──────────────────────┐  │
│  │    Auth      │  │  Elections  │  │   Results/Collation  │  │
│  │  Service     │  │  Service    │  │      Service         │  │
│  │             │  │             │  │                      │  │
│  │ - Login     │  │ - CRUD      │  │ - Result submission  │  │
│  │ - JWT       │  │ - Lifecycle │  │ - Hierarchical       │  │
│  │ - MFA       │  │ - FSM       │  │   collation          │  │
│  │ - Sessions  │  │ - Materials │  │ - Blockchain audit   │  │
│  │ - RBAC      │  │             │  │ - Dispute resolution │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬───────────┘  │
│         │                │                     │              │
│  ┌──────┴──────┐  ┌──────┴──────┐  ┌──────────┴───────────┐  │
│  │  Biometric  │  │   Geospatial │  │    Observer/Monitor  │  │
│  │  Service    │  │   Service    │  │      Service         │  │
│  │             │  │              │  │                      │  │
│  │ - BVAS      │  │ - Maps/GIS   │  │ - Real-time feed     │  │
│  │ - PAD/FAD   │  │ - Tracking   │  │ - Incident reporting │  │
│  │ - Vault     │  │ - Geofencing │  │ - Analytics          │  │
│  │ - Templates │  │ - MVT tiles  │  │ - Stakeholder        │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬───────────┘  │
│         │                │                     │              │
│  ┌──────┴──────────────────────────────────────┴───────────┐  │
│  │              Shared Infrastructure Layer                 │  │
│  │                                                          │  │
│  │  Middleware Hub (13 services) │ Event Bus │ Circuit      │  │
│  │  PostgreSQL/PostGIS           │ Redis     │ Breakers     │  │
│  │  Kafka                        │ Temporal  │ OTEL Tracing │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────┘
```

## Route Ownership

Each service domain owns specific route prefixes:

| Domain | Route Prefix | Handler File(s) | Hot Path? |
|--------|-------------|-----------------|-----------|
| Auth | `/auth/*` | `auth.go`, `middleware_auth.go` | Yes |
| Elections | `/elections/*` | `ems.go`, `election_fsm.go` | Medium |
| Results | `/results/*`, `/collation/*` | `handlers.go`, `domain_inec.go` | **Critical** |
| Biometric | `/biometric/*`, `/bvas/*` | `biometric_engine.go`, `biometric_advanced.go` | **Critical** |
| Geospatial | `/geo/*`, `/map/*` | `geo_advanced.go`, `geospatial_enhanced.go` | Medium |
| Observer | `/observer/*`, `/incidents/*` | `observer_monitoring.go` | Low |
| Admin | `/admin/*`, `/middleware/*` | `platform_enhancements.go` | Low |
| Infrastructure | `/healthz`, `/metrics`, `/architecture/*` | `main.go`, `architecture.go` | Always |

## Scaling Strategy

### Kubernetes HPA (Current)

The Helm chart supports horizontal pod autoscaling:

```yaml
backend:
  autoscaling:
    enabled: true
    minReplicas: 5
    maxReplicas: 40
    targetCPUUtilization: 60
    targetMemoryUtilization: 70
```

### Election Day Scaling Profile

| Component | Normal | Election Day |
|-----------|--------|-------------|
| Backend pods | 5 | 20-40 |
| PostgreSQL | r6g.2xlarge | r6g.4xlarge |
| Read replicas | 2 | 4 |
| Redis nodes | 3 | 6 |
| Kafka brokers | 3 | 6 |

### Future: Service Mesh Decomposition

When traffic patterns warrant independent scaling:

1. Extract `biometric_engine.go` + `biometric_advanced.go` → separate gRPC service
2. Extract `geo_advanced.go` + `geospatial_enhanced.go` → separate service with its own PostGIS instance
3. Extract `handlers.go` (results) → separate service behind Kafka for write buffering
4. Keep auth + admin + infrastructure in the core service

Each extraction follows the Strangler Fig pattern:
1. Deploy new service alongside monolith
2. Route traffic via APISIX to new service
3. Remove code from monolith
4. Scale independently

## Communication Patterns

### Synchronous (HTTP/gRPC)
- Auth → PostgreSQL (login, session validation)
- Results → PostgreSQL (CRUD)
- Biometric → PostgreSQL + Vault (verify, store)

### Asynchronous (Event Bus / Kafka)
- Result submitted → `results.submitted` event → Blockchain audit, Collation update, Observer notification
- Biometric verified → `biometric.verified` event → BVAS reconciliation
- Geofence violation → `geofence.violation` event → Alert, Incident creation

### Circuit Breakers
14 circuit breakers protect all external service calls:
- Configurable failure threshold (default: 5 failures)
- Half-open retry after 30 seconds
- Prometheus metrics on state transitions
