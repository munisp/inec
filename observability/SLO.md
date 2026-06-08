# INEC Platform — Service Level Objectives (SLOs)

## Overview

These SLOs define the reliability targets for the INEC Election Platform.
They are measured continuously and enforced via Prometheus alerting rules.

---

## SLO 1: Availability

| Metric | Target | Measurement |
|--------|--------|-------------|
| **Success rate** | 99.95% | `1 - (5xx responses / total responses)` over 5-minute windows |
| **Error budget (monthly)** | 21.6 minutes downtime | 30 days × 24h × 60m × 0.0005 |
| **Error budget (election day)** | 43 seconds | 24h × 60m × 60s × 0.0005 |

**Prometheus rule:**
```
inec:slo:availability:ratio_5m = 1 - (sum(rate(inec_http_requests_total{status=~"5.."}[5m])) / sum(rate(inec_http_requests_total[5m])))
```

**Alert:** Fires when availability drops below 99.95% for 10 minutes.

---

## SLO 2: Latency

| Metric | Target | Measurement |
|--------|--------|-------------|
| **p50 latency** | < 100ms | 50th percentile of request duration |
| **p95 latency** | < 250ms | 95th percentile of request duration |
| **p99 latency** | < 500ms | 99th percentile of request duration |

**Prometheus rule:**
```
inec:slo:latency:p99_5m = histogram_quantile(0.99, sum(rate(inec_http_request_duration_seconds_bucket[5m])) by (le))
```

**Alert:** Fires when p99 exceeds 500ms for 10 minutes.

### Per-endpoint latency budgets:

| Endpoint Category | p99 Target | Rationale |
|-------------------|-----------|-----------|
| Auth (login/logout) | 200ms | User-facing, must be fast |
| Results submission | 500ms | Can tolerate slightly higher latency |
| Collation queries | 1000ms | Complex aggregation, acceptable |
| Biometric verification | 300ms | Field device timeout is typically 5s |
| Geo/map queries | 500ms | PostGIS spatial queries |
| Dashboard/metrics | 1000ms | Background refresh |

---

## SLO 3: Throughput

| Metric | Target | Measurement |
|--------|--------|-------------|
| **Results processing** | 1000 results/min | Election day peak capacity |
| **Biometric verifications** | 5000/min | 48K PUs × ~100 voters/PU over 8 hours |
| **Concurrent users** | 10,000 | Simultaneous WebSocket + API connections |

---

## SLO 4: Data Integrity

| Metric | Target | Measurement |
|--------|--------|-------------|
| **Dual-ledger match rate** | 100% | Primary vs. replica vote counts match |
| **Blockchain attestation rate** | 100% | Every result has a blockchain attestation |
| **Audit log completeness** | 100% | Every state-changing operation logged |

---

## SLO 5: Freshness

| Metric | Target | Measurement |
|--------|--------|-------------|
| **Real-time tracking delay** | < 30s | Official position update → map display |
| **Results display delay** | < 60s | Result submission → public dashboard |
| **Replication lag** | < 1s | Primary → replica PostgreSQL lag |

---

## Error Budget Policy

### When error budget is consumed > 50%:
1. Freeze non-critical deployments
2. Prioritize reliability work
3. Review recent changes for regressions

### When error budget is consumed > 80%:
1. All deployments require rollback plan
2. On-call staffing doubled
3. Performance review of recent changes

### When error budget is exhausted (100%):
1. Only critical security patches deployed
2. Incident review required before any new features
3. Executive notification

### Election Day Override:
During election day (06:00 - 22:00), the error budget is effectively zero.
Any SLO breach during this window is treated as P1 regardless of remaining budget.
