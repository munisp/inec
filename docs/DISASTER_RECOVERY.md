# INEC Election Platform — Disaster Recovery Plan

**Version:** 1.0  
**Last Updated:** 2026-06-08  
**Classification:** CONFIDENTIAL — INEC Operations  

---

## 1. Recovery Objectives

| Metric | Target | Rationale |
|--------|--------|-----------|
| **RTO (Recovery Time Objective)** | **15 minutes** | Election-day downtime beyond 15min causes national visibility |
| **RPO (Recovery Point Objective)** | **0 seconds** (synchronous replication) | Every vote is constitutionally significant; zero data loss |
| **MTTR (Mean Time To Repair)** | **< 10 minutes** | Automated failover for most scenarios |

### Tiered Recovery

| Tier | Scope | RTO | RPO |
|------|-------|-----|-----|
| Tier 1: Critical | Result submission, biometric verification, collation | 5 min | 0 |
| Tier 2: Important | Dashboard, tracking, observer reports | 15 min | 30 sec |
| Tier 3: Support | Training portal, analytics, historical reports | 4 hours | 1 hour |

---

## 2. Architecture for DR

```
                    ┌─────────────────────────────────────┐
                    │        CloudFlare / AWS Route53      │
                    │      (Global Load Balancer + DNS)    │
                    └──────────┬──────────────┬───────────┘
                               │              │
                    ┌──────────▼──────┐ ┌─────▼──────────┐
                    │  Primary Region │ │ Standby Region │
                    │  (Lagos/Abuja)  │ │   (Kano/PH)    │
                    ├─────────────────┤ ├────────────────┤
                    │ K8s Cluster     │ │ K8s Cluster    │
                    │ - 3 backend pods│ │ - 1 warm pod   │
                    │ - 2 frontend    │ │ - 1 frontend   │
                    ├─────────────────┤ ├────────────────┤
                    │ PostgreSQL      │ │ PostgreSQL     │
                    │ (Primary)       │→│ (Streaming     │
                    │                 │ │  Replica)      │
                    ├─────────────────┤ ├────────────────┤
                    │ Redis Sentinel  │ │ Redis Sentinel │
                    │ (3-node)        │ │ (3-node)       │
                    └─────────────────┘ └────────────────┘
```

### Replication Strategy

| Component | Method | Lag Target |
|-----------|--------|------------|
| PostgreSQL | Streaming replication (synchronous) | 0 ms |
| Redis | Redis Sentinel with cross-AZ replicas | < 100 ms |
| Object Storage | S3 cross-region replication | < 15 min |
| Blockchain Ledger | Multi-node Hyperledger Fabric | Consensus-based |
| TigerBeetle | Built-in quorum replication | 0 ms |

---

## 3. Failure Scenarios & Response Procedures

### Scenario 1: Single Pod Crash

**Detection:** Kubernetes liveness probe fails (3 consecutive failures, 10s interval)  
**Automatic Response:** K8s restarts pod, HPA scales replacement  
**RTO:** < 30 seconds  
**Manual Action:** None required  
**Escalation:** If pod crash-loops > 3 times → Alert on-call engineer  

### Scenario 2: Database Primary Failure

**Detection:** `INECDatabaseDown` alert fires (pg_isready fails for 30s)  
**Automatic Response:**  
1. Patroni/pg_auto_failover promotes standby to primary (< 10s)  
2. Application connection pool reconnects (< 5s)  
3. DNS/service endpoint updated automatically  

**Manual Verification:**  
```bash
# Check replication status
kubectl exec -it pg-primary -- psql -c "SELECT * FROM pg_stat_replication;"
# Verify no data loss
kubectl exec -it pg-new-primary -- psql -c "SELECT MAX(submitted_at) FROM results;"
```
**RTO:** < 30 seconds (automatic), < 5 minutes (manual verification)  
**Escalation:** DBA on-call if replication lag > 0 before failover  

### Scenario 3: Full Region Outage

**Detection:** All health checks from primary region fail for 60s  
**Response Procedure:**  
1. **DNS Failover** (automatic): Route53/CloudFlare health checks switch traffic to standby region
2. **Promote standby DB** (manual confirmation):
   ```bash
   # On standby region
   patronictl failover --candidate standby-node --force
   ```
3. **Scale standby pods** (automatic): HPA scales from warm-standby (1 pod) to production (3+ pods)
4. **Notify stakeholders**: Automated PagerDuty → Slack → SMS chain

**RTO:** 5-15 minutes  
**RPO:** 0 (synchronous replication)  
**Post-Recovery:**  
- Investigate root cause in failed region  
- Rebuild failed region as new standby  
- Verify data consistency between regions  

### Scenario 4: Data Corruption / Accidental Deletion

**Detection:** Anomaly detection alerts, user reports, audit log review  
**Response:**  
1. **Isolate:** Remove affected pod from load balancer
2. **Assess scope:** Query audit_log for recent changes
   ```sql
   SELECT * FROM audit_log 
   WHERE action IN ('DELETE','UPDATE') 
   AND created_at > NOW() - INTERVAL '1 hour'
   ORDER BY created_at DESC;
   ```
3. **Point-in-Time Recovery:**
   ```bash
   # Restore to specific timestamp
   pg_basebackup -D /restore/data --checkpoint=fast
   # Apply WAL logs up to corruption point
   recovery_target_time = '2027-02-25 14:30:00 UTC'
   ```
4. **Validate:** Compare restored data against blockchain attestations
5. **Swap:** Promote restored DB, redirect traffic

**RTO:** 30-60 minutes  
**RPO:** Depends on detection speed (WAL archiving is continuous)  

### Scenario 5: DDoS Attack

**Detection:** `INECHighRateLimitRejections` alert, traffic anomaly  
**Response:**  
1. CloudFlare/AWS Shield auto-mitigates known attack patterns
2. WAF rules block malicious IPs (OpenAppSec integration)
3. Rate limiter (X-Forwarded-For aware) throttles per-IP
4. If overwhelmed: Enable geo-fencing to allow only Nigerian IPs
   ```bash
   # Emergency geo-fence
   kubectl set env deployment/inec-backend GEO_FENCE_ENABLED=true ALLOWED_COUNTRIES=NG
   ```

**RTO:** < 5 minutes (auto-mitigation), < 15 minutes (manual intervention)  

### Scenario 6: Biometric System Failure

**Detection:** `INECBiometricFailureRateHigh` alert (>10% failure rate)  
**Response:**  
1. Circuit breaker opens automatically (after 5 consecutive failures)
2. Fallback: Manual identity verification with VIN + photo ID
3. Record offline verifications for post-election reconciliation
4. Escalate to biometric vendor for hardware/API fix

**RTO:** Immediate (circuit breaker), election continues with manual fallback  

---

## 4. Backup Strategy

### Backup Schedule

| Data | Method | Frequency | Retention | Storage |
|------|--------|-----------|-----------|---------|
| PostgreSQL (full) | pg_basebackup | Daily 2:00 AM WAT | 90 days | S3 (encrypted, cross-region) |
| PostgreSQL (WAL) | Continuous archiving | Real-time | 30 days | S3 |
| PostgreSQL (logical) | pg_dump | Weekly | 1 year | S3 + offline tape |
| Redis | RDB snapshot | Every 15 min | 7 days | S3 |
| Blockchain ledger | Peer snapshot | Daily | Permanent | S3 + offline |
| Upload files (EC8A forms) | S3 versioning | On upload | 7 years (NDPR) | S3 (lifecycle to Glacier after 1yr) |
| Configuration | Git + Vault snapshot | On change | Permanent | Git history |

### Backup Verification

**Monthly drill (automated):**
```bash
#!/bin/bash
# backup_verification.sh — runs monthly via CronJob
set -e

# 1. Restore latest backup to test environment
pg_restore -d inec_test /backups/latest/inec_full.dump

# 2. Verify row counts match production (±0.1%)
PROD_COUNT=$(psql -h prod -c "SELECT COUNT(*) FROM results" -t)
TEST_COUNT=$(psql -h test -c "SELECT COUNT(*) FROM results" -t)
DIFF=$(echo "scale=4; ($PROD_COUNT - $TEST_COUNT) / $PROD_COUNT * 100" | bc)
if (( $(echo "$DIFF > 0.1" | bc -l) )); then
  echo "ALERT: Backup row count drift > 0.1%"
  exit 1
fi

# 3. Verify data integrity (spot-check 100 random results)
psql -h test -c "
  SELECT COUNT(*) FROM results r
  JOIN result_party_scores rps ON r.id = rps.result_id
  WHERE r.id IN (SELECT id FROM results ORDER BY RANDOM() LIMIT 100)
  AND rps.votes >= 0
" -t | grep -q "100"

# 4. Report
echo "Backup verification PASSED: $TEST_COUNT rows, integrity confirmed"
```

**Election-day backup (enhanced):**
- pg_basebackup every 2 hours (instead of daily)
- WAL archiving verified every 5 minutes
- Dedicated backup monitoring dashboard in Grafana

---

## 5. Communication Plan

### Escalation Matrix

| Severity | Detection | First Response | Escalation | Executive |
|----------|-----------|----------------|------------|-----------|
| P1 (Critical) | Automated alert | On-call SRE (5 min) | Engineering Lead (15 min) | CTO + INEC Ops (30 min) |
| P2 (High) | Automated alert | On-call SRE (15 min) | Engineering Lead (1 hr) | — |
| P3 (Medium) | Monitoring | Next business day | — | — |

### Communication Channels

| Channel | Use Case | SLA |
|---------|----------|-----|
| PagerDuty | P1/P2 alerts → on-call | < 5 min acknowledgment |
| Slack #inec-ops | Real-time coordination | During incidents |
| Slack #inec-election-day | Election-day war room | Election day only |
| SMS broadcast | Critical status updates | < 15 min |
| Status page | Public-facing status | Updated every 15 min during incidents |

---

## 6. Election Day War Room Procedures

### T-24 Hours (Pre-Election Checklist)
- [ ] Verify all backups completed and tested
- [ ] Confirm standby region is synchronized (replication lag = 0)
- [ ] Scale primary region to election-day capacity (HPA max)
- [ ] Run K6 load test against staging (48K VU target)
- [ ] Verify all 15 alert rules are active in AlertManager
- [ ] Pre-stage database connection pool (warm connections)
- [ ] Confirm CDN cache is primed for static assets
- [ ] Test PagerDuty → Slack → SMS escalation chain
- [ ] Brief war room team on runbooks

### T-0 to T+12 (Election Day)
- [ ] War room staffed: SRE (2), DBA (1), Security (1), Comms (1)
- [ ] Grafana dashboard on large screen: inec-election-day.json
- [ ] Continuous monitoring: error rate < 0.1%, p99 < 2s, circuit breakers closed
- [ ] Hourly backup verification (automated CronJob)
- [ ] 2-hour check-ins with INEC operations center

### Post-Election (T+12 to T+48)
- [ ] Full backup of election-day data
- [ ] Blockchain attestation verification for all results
- [ ] Audit log export for INEC compliance
- [ ] Scale down to normal capacity
- [ ] Post-mortem of any incidents

---

## 7. Testing & Drills

### Quarterly DR Drills

| Drill | Scenario | Success Criteria |
|-------|----------|-----------------|
| Q1 | Database failover | RTO < 30s, RPO = 0, no user-visible errors |
| Q2 | Full region failover | RTO < 15min, all services operational in standby |
| Q3 | Backup restore | Full restore in < 2 hours, data integrity verified |
| Q4 | Election day simulation | 48K VU sustained for 12 hours, < 0.1% error rate |

### Chaos Engineering (Monthly)

Using Chaos Monkey / Litmus Chaos:
- Random pod kills during business hours
- Network partition between app and DB (5 min)
- Redis memory pressure (80% utilization)
- Disk I/O saturation on DB nodes
- Clock skew simulation (NTP drift)

---

## 8. Compliance & Audit

- All DR procedures documented and versioned in Git
- Quarterly drill reports retained for 7 years (NDPR)
- Audit trail of all recovery actions (who, when, what)
- Annual review by third-party security firm
- INEC compliance team sign-off on DR plan changes

---

## Appendix A: Key Contacts

| Role | Primary | Backup |
|------|---------|--------|
| SRE On-Call | [TBD - Rotates weekly] | [TBD] |
| DBA | [TBD] | [TBD] |
| Security Lead | [TBD] | [TBD] |
| INEC Operations | [TBD] | [TBD] |
| Cloud Provider Support | [TBD - AWS/GCP TAM] | [TBD] |

## Appendix B: Recovery Commands Quick Reference

```bash
# Check PostgreSQL replication status
psql -c "SELECT pid, state, sent_lsn, write_lsn, flush_lsn, replay_lsn FROM pg_stat_replication;"

# Force failover (Patroni)
patronictl failover --candidate standby --force

# Scale backend pods
kubectl scale deployment inec-backend --replicas=5

# Check circuit breaker status
curl -s http://localhost:8088/architecture/circuit-breakers | jq '.[] | select(.state != "closed")'

# Emergency: block non-Nigerian IPs
kubectl set env deployment/inec-backend GEO_FENCE_ENABLED=true ALLOWED_COUNTRIES=NG

# Restore from backup
pg_restore -h localhost -U ngapp -d ngapp_restore /backups/latest/inec_full.dump

# Verify data integrity
psql -c "SELECT COUNT(*) as total_results, COUNT(DISTINCT polling_unit_code) as unique_pus FROM results WHERE election_id=1;"
```
