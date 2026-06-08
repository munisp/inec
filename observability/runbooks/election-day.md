# Runbook: Election Day Operations

## Pre-Election Checklist (T-24 hours)

### Infrastructure
- [ ] All pods healthy: `kubectl -n inec get pods` (all Running, 0 restarts)
- [ ] Database replication lag < 100ms: check `pg_stat_replication`
- [ ] Redis cluster healthy: `redis-cli cluster info`
- [ ] All circuit breakers closed: `curl api.inec.ng/admin/circuit-breakers`
- [ ] SSL certificates valid for >30 days
- [ ] Disk usage < 60% on all volumes
- [ ] Backup CronJob ran successfully in last 6 hours

### Application
- [ ] Health check passes: `curl api.inec.ng/healthz` → `{"status":"healthy"}`
- [ ] Readiness check passes: `curl api.inec.ng/readiness`
- [ ] All 13 middleware services connected (not embedded/fallback)
- [ ] JWT_SECRET, BIOMETRIC_VAULT_MASTER_KEY, DATABASE_URL all set
- [ ] Rate limiting functional (test with 6 rapid login attempts → 429)
- [ ] CORS configured for production domain only

### Data
- [ ] All 48,842 polling units loaded and geocoded
- [ ] Election created with correct date/type
- [ ] All registered parties and candidates loaded
- [ ] BVAS devices registered and firmware up to date
- [ ] Official assignments complete (presiding officers → polling units)

### Monitoring
- [ ] Grafana dashboards accessible: `grafana.inec.ng`
- [ ] Alert channels verified (Slack, PagerDuty, SMS)
- [ ] On-call rotation confirmed for election day
- [ ] Test alert sent and received on all channels

### Communications
- [ ] War room established (physical + virtual)
- [ ] Escalation contacts confirmed (see below)
- [ ] Backup communication channel ready (WhatsApp group)

---

## Election Day Timeline

### 05:00 — System Warm-Up
```bash
# Verify all systems
curl -s api.inec.ng/healthz | jq .
curl -s api.inec.ng/middleware/health | jq .

# Check pod count matches HPA minimum
kubectl -n inec get hpa

# Pre-warm database connection pool
curl -s api.inec.ng/readiness
```

### 06:00 — Polling Units Open
- Monitor: Active PU count should ramp to 48,842 over 30 minutes
- Dashboard: Grafana → INEC Election Day → "Active Polling Units Reporting"
- Alert threshold: < 40,000 PUs reporting by 07:00 → escalate

### 06:00-14:00 — Voting Period
**Key metrics to watch:**
- Voter throughput: 50-200 voters/min/PU (varies by location)
- Biometric success rate: should be > 80%
- BVAS heartbeat rate: should show ~48K heartbeats/min
- Error rate: must stay < 1%
- Latency p99: must stay < 500ms

**Common issues during voting:**
| Issue | Symptom | Action |
|-------|---------|--------|
| BVAS offline | Heartbeat drops for a PU | Contact field officer, check connectivity |
| Biometric failures | Success rate drops in a state | Check BVAS firmware, environmental conditions |
| High latency | p99 > 1s | Check DB queries, scale up pods |
| Rate limiting | 429s from legitimate PUs | Whitelist PU IP ranges in rate limiter |

### 14:00 — Voting Ends
- Voting stops but result submission begins
- Monitor results submission rate — should see 48K+ submissions over 2-4 hours

### 14:00-20:00 — Collation Period
**Key metrics:**
- Results submitted: track progress toward 48,842
- Collation progress: ward → LGA → state → national
- Blockchain attestations: should match results count
- Anomaly detections: investigate any spikes

**Collation verification:**
```bash
# Check collation progress
curl -s api.inec.ng/collation/progress | jq .

# Verify dual-ledger reconciliation
curl -s api.inec.ng/results/reconciliation | jq .total_discrepancies
# Expected: 0
```

### 20:00+ — Results Announcement
- All results should be submitted and collated
- Final reconciliation check
- Blockchain attestation complete for all results
- Export results for public portal

---

## Escalation Matrix

| Severity | Response Time | Who |
|----------|--------------|-----|
| P1 (system down) | 5 min | On-call → Engineering Lead → CTO |
| P2 (degraded) | 15 min | On-call → Engineering Lead |
| P3 (minor issue) | 30 min | On-call |
| P4 (cosmetic) | Next day | Backlog |

**Election Day Contacts:**
- On-call Engineer 1: [NAME] — [PHONE]
- On-call Engineer 2: [NAME] — [PHONE]
- Engineering Lead: [NAME] — [PHONE]
- INEC IT Director: [NAME] — [PHONE]
- CTO: [NAME] — [PHONE]

---

## Post-Election Checklist (T+1 day)

- [ ] All results submitted and collated
- [ ] Zero unresolved anomalies
- [ ] Dual-ledger reconciliation shows 0 discrepancies
- [ ] Full database backup completed
- [ ] Audit logs exported and archived
- [ ] Blockchain attestation chain verified
- [ ] Incident reports filed for any P1/P2 events
- [ ] Post-mortem scheduled for any incidents
