# Runbook: Database Down

**Alert:** `INECDatabaseDown`  
**Severity:** CRITICAL  
**Team:** DBA  

## Symptoms
- `inec_db_connections_active` drops to 0
- All API requests return 500
- Health endpoint shows database: disconnected

## Immediate Actions

### 1. Check PostgreSQL pod status
```bash
kubectl -n inec get pods -l app=pg-primary
kubectl -n inec get pods -l app=pg-replica
kubectl -n inec logs -l app=pg-primary --tail=50
```

### 2. Check if primary is running
```bash
kubectl -n inec exec -it <pg-primary-pod> -- pg_isready -U inec_admin -d inec_db
# Expected: accepting connections
```

### 3. Check disk space
```bash
kubectl -n inec exec -it <pg-primary-pod> -- df -h /var/lib/postgresql/data
# If >90% → emergency cleanup needed
```

### 4. Check connection limits
```bash
kubectl -n inec exec -it <pg-primary-pod> -- psql -U inec_admin -c "
  SELECT max_conn, used, max_conn - used AS free
  FROM (SELECT count(*) AS used FROM pg_stat_activity) t1,
       (SELECT setting::int AS max_conn FROM pg_settings WHERE name='max_connections') t2;
"
```

## Failover Procedure

### If primary is unrecoverable:
```bash
# 1. Promote replica to primary
kubectl -n inec exec -it <pg-replica-pod> -- pg_ctl promote -D /var/lib/postgresql/data

# 2. Update backend config to point to new primary
kubectl -n inec set env deployment/inec-backend DATABASE_URL="postgresql://inec_admin:${PG_PASSWORD}@pg-replica:5432/inec_db?sslmode=require"

# 3. Restart backend to pick up new config
kubectl -n inec rollout restart deployment/inec-backend
```

### Post-failover:
1. Set up new replica from promoted primary
2. Verify replication is working
3. Update DNS/service records

## Election Day Specific
- During election hours (06:00-22:00), database downtime > 5 minutes requires:
  1. Immediate notification to INEC IT director
  2. Activate backup data collection (paper forms + BVAS local storage)
  3. Plan for batch data reconciliation after DB recovery
