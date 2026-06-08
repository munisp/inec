# INEC Platform — Production Deployment Checklist

## Pre-Deployment (T-30 days)

### Infrastructure
- [ ] PostgreSQL 16 with PostGIS 3.4 provisioned (primary + 2 replicas)
- [ ] PgPool-II configured for read/write splitting
- [ ] Redis cluster provisioned (3 nodes minimum)
- [ ] Kafka cluster provisioned (3 brokers, replication factor 3)
- [ ] TigerBeetle cluster provisioned (3 replicas)
- [ ] Kubernetes cluster sized (minimum 6 nodes, 32GB RAM each)
- [ ] TLS certificates provisioned (Let's Encrypt or organizational CA)
- [ ] DNS records configured for all services
- [ ] CDN configured for frontend static assets

### Security
- [ ] `JWT_SECRET` generated (64+ random bytes, stored in Vault/KMS)
- [ ] `BIOMETRIC_VAULT_KEY` stored in HSM/AWS KMS (NOT in source code)
- [ ] `DB_SSL_MODE=verify-full` in all database connections
- [ ] Keycloak realm configured with SSO for admin users
- [ ] Network policies applied (pod-to-pod communication restricted)
- [ ] WAF rules configured (OWASP Top 10 protection)
- [ ] DDoS mitigation enabled
- [ ] Penetration test completed and findings remediated

### Data
- [ ] Schema migrations run against production database (`/admin/data-retention` returns 9 policies)
- [ ] Seed data loaded for all 774 LGAs, 8,809 wards, 176,846 PUs
- [ ] BVAS device registration imported from INEC inventory
- [ ] Observer/agent credentials provisioned
- [ ] ML models retrained on real historical data (minimum 2 election cycles)
- [ ] ML model validation script passed (`python ml/validate_models.py -v`)

### Monitoring
- [ ] Prometheus scraping all `/metrics` endpoints
- [ ] Grafana dashboards imported (election-day, biometric, geo, infrastructure)
- [ ] AlertManager configured (PagerDuty/Opsgenie integration)
- [ ] Log aggregation running (ELK/Loki)
- [ ] Distributed tracing enabled (Jaeger/Tempo)

## Pre-Election (T-7 days)

### Load Testing
- [ ] K6 election-day simulation run at 2x expected load
- [ ] All endpoint thresholds met (healthz p99 < 100ms, results p95 < 1s)
- [ ] Database connection pool tested under peak (1000+ concurrent)
- [ ] SSE/WebSocket connections tested at scale (10K+ simultaneous)

### Failover Testing
- [ ] Database failover tested (primary → replica promotion < 30s)
- [ ] Redis failover tested (sentinel auto-promotion)
- [ ] Application pod rolling restart tested (zero downtime)
- [ ] Network partition simulation (Chaos Mesh/LitmusChaos)

### Data Validation
- [ ] All polling unit coordinates validated (none in Atlantic Ocean)
- [ ] Geofence zones created for all polling units (500m radius)
- [ ] Landmark seed data verified per state
- [ ] Election configured with all parties, candidates, constituencies

## Election Day (T-0)

### Pre-Start (6:00 AM)
- [ ] Health check: `GET /healthz` returns `healthy` on all pods
- [ ] Readiness check: `GET /readiness` returns 200 on all pods
- [ ] Database replication lag < 100ms
- [ ] Redis cluster healthy
- [ ] All BVAS devices reporting heartbeat
- [ ] Command center dashboard accessible
- [ ] TV dashboard accessible on large screens

### During Voting (8:30 AM – 2:30 PM)
- [ ] Monitor SSE live feed for real-time updates
- [ ] Watch anomaly detection alerts (auto-flagged results)
- [ ] Track observer check-in rates per state
- [ ] Monitor biometric verification queue times
- [ ] Watch crowd density alerts near polling units

### Post-Voting (2:30 PM – 11:59 PM)
- [ ] Collation pipeline running (ward → LGA → state → national)
- [ ] Blockchain audit trail entries being created
- [ ] Result photo uploads verified
- [ ] Dispute resolution portal active
- [ ] Public API serving real-time results

## Post-Election (T+1 to T+30)

- [ ] Full audit trail export for election tribunal
- [ ] Blockchain verification of all result records
- [ ] ML model performance report (actual vs predicted anomalies)
- [ ] Data retention policies active (audit logs: 7 years, PII: per NDPA)
- [ ] Incident post-mortem report generated
- [ ] K6 load test results archived for comparison
