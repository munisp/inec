# INEC Platform — Disaster Recovery Runbook

## 1. Severity Classification

| Level | Description | Response Time | Example |
|-------|------------|---------------|---------|
| P1 — Critical | Platform completely unavailable on election day | < 15 min | Database cluster failure, DNS outage |
| P2 — Major | Core feature degraded (results, BVAS) | < 30 min | Kafka broker failure, Redis OOM |
| P3 — Minor | Non-core feature unavailable | < 2 hours | ML inference down, observer portal slow |
| P4 — Low | Cosmetic/logging issue | Next business day | Metrics missing, dashboard lag |

---

## 2. Database Recovery

### PostgreSQL Primary Failure

```bash
# 1. Check primary status
pg_isready -h primary-host -p 5432

# 2. Promote standby to primary
pg_ctl promote -D /var/lib/postgresql/data

# 3. Update connection strings
# Update DATABASE_URL in Kubernetes secrets or .env
kubectl set env deployment/inec-backend DATABASE_URL="postgresql://..."

# 4. Verify
psql $DATABASE_URL -c "SELECT pg_is_in_recovery();"
# Should return 'f' (false = primary)

# 5. Rebuild old primary as standby
pg_basebackup -h new-primary -D /var/lib/postgresql/data -R -P
```

### Point-in-Time Recovery (PITR)

```bash
# 1. Stop the server
pg_ctl stop -D /var/lib/postgresql/data

# 2. Restore base backup
pg_restore -d ngapp /backups/latest_base.tar

# 3. Configure recovery target
echo "recovery_target_time = '2026-06-01 12:00:00+00'" > /var/lib/postgresql/data/recovery.signal

# 4. Restart
pg_ctl start -D /var/lib/postgresql/data
```

### Data Verification After Recovery

```sql
-- Check core tables exist and have data
SELECT 'users' as tbl, COUNT(*) FROM users
UNION ALL SELECT 'elections', COUNT(*) FROM elections
UNION ALL SELECT 'results', COUNT(*) FROM results
UNION ALL SELECT 'polling_units', COUNT(*) FROM polling_units
UNION ALL SELECT 'parties', COUNT(*) FROM parties;

-- Check schema version
SELECT * FROM schema_migrations ORDER BY version DESC LIMIT 5;
```

---

## 3. Application Recovery

### Full Restart Procedure

```bash
# 1. Check Kubernetes cluster health
kubectl get nodes
kubectl get pods -n inec

# 2. Restart backend pods (rolling)
kubectl rollout restart deployment/inec-backend -n inec

# 3. Verify readiness
kubectl rollout status deployment/inec-backend -n inec --timeout=300s

# 4. Verify health endpoint
curl -s https://api.inec.gov.ng/healthz | jq .

# 5. Run quick smoke tests
curl -s https://api.inec.gov.ng/readiness | jq .status
```

### Emergency Rollback

```bash
# Rollback to previous version
kubectl rollout undo deployment/inec-backend -n inec

# Or rollback to specific revision
kubectl rollout undo deployment/inec-backend -n inec --to-revision=N

# Verify
kubectl rollout status deployment/inec-backend -n inec
```

---

## 4. Middleware Recovery

### Keycloak (Authentication)

If Keycloak is down and `APP_ENV=production`:
- The platform will NOT start (hard fail by design)
- Fix Keycloak first, then restart platform pods

```bash
kubectl rollout restart statefulset/keycloak -n inec
kubectl wait --for=condition=ready pod -l app=keycloak -n inec --timeout=300s
```

### Kafka (Event Streaming)

```bash
# Check broker status
kafka-broker-api-versions.sh --bootstrap-server $KAFKA_BROKERS

# If topic is corrupted, recreate
kafka-topics.sh --delete --topic inec.results --bootstrap-server $KAFKA_BROKERS
kafka-topics.sh --create --topic inec.results --partitions 37 --replication-factor 3 --bootstrap-server $KAFKA_BROKERS
```

### Redis (Cache/Rate Limiting)

```bash
# Check Redis
redis-cli -u $REDIS_URL PING

# Clear rate limit keys (emergency: allows all requests through)
redis-cli -u $REDIS_URL KEYS "ratelimit:*" | xargs redis-cli -u $REDIS_URL DEL

# If Redis is completely down, the platform falls back to in-memory rate limiting
```

---

## 5. Election Day Emergency Procedures

### Scenario: Results Not Submitting

1. Check backend health: `curl https://api.inec.gov.ng/healthz`
2. Check database connectivity: `kubectl exec -it deploy/inec-backend -- wget -qO- localhost:8080/readiness`
3. Check Kafka: `kubectl logs deploy/inec-backend | grep -i kafka`
4. Check rate limiting: Are legitimate requests being blocked?
5. Check BVAS sync: Are devices able to reach the API?

### Scenario: Authentication Down

1. Check Keycloak pods: `kubectl get pods -l app=keycloak`
2. Check Keycloak logs: `kubectl logs statefulset/keycloak`
3. If Keycloak unrecoverable, enable emergency auth bypass (requires manual code deployment)
4. Document all manual overrides for post-election audit

### Scenario: Map/Tracking Not Updating

1. Check SSE connections: `curl -N https://api.inec.gov.ng/geo/live-stream`
2. Check PostGIS: `psql $DATABASE_URL -c "SELECT PostGIS_version();"`
3. Restart geo-specific services if needed

---

## 6. Backup Schedule

| Data | Frequency | Retention | Method |
|------|-----------|-----------|--------|
| Full database | Daily (2 AM WAT) | 30 days | pg_dump + S3 |
| WAL archives | Continuous | 7 days | pg_receivewal + S3 |
| Election results | Real-time | Permanent | Kafka → S3 (immutable) |
| Audit trail | Real-time | 7 years | Blockchain hash + S3 |
| ML models | Per training | 1 year | S3 versioned |
| Config/secrets | On change | 90 days | Vault + Git |

### Verify Backups

```bash
# Test restore from latest backup (weekly)
pg_restore -d ngapp_test /backups/latest.dump
psql ngapp_test -c "SELECT COUNT(*) FROM results;"
```

---

## 7. Communication Plan

| Audience | Channel | Frequency |
|----------|---------|-----------|
| INEC IT Operations | Slack #inec-ops | Real-time during incident |
| State IT Coordinators | SMS + Email | Every 30 min during P1/P2 |
| Public | inec.gov.ng status page | Every 1 hour during P1 |
| Media | Official press briefing | After resolution |

---

## 8. Post-Incident

After every P1/P2 incident:
1. Write incident report within 24 hours
2. Conduct blameless post-mortem within 48 hours
3. Create action items for prevention
4. Update this runbook with lessons learned
5. Test recovery procedures quarterly
