# Runbook: High Error Rate

**Alert:** `INECHighErrorRate`  
**Severity:** CRITICAL  
**Team:** Platform  

## Symptoms
- 5xx error rate exceeds 5% for >2 minutes
- Users see "Something went wrong" errors
- Result submissions fail intermittently

## Diagnosis Steps

### 1. Identify the failing endpoints
```bash
# Check which paths are returning 5xx
kubectl -n inec logs -l app=inec-backend --tail=500 | \
  jq 'select(.status >= 500) | {path, status, error}' | \
  sort | uniq -c | sort -rn | head -20
```

### 2. Check if it's concentrated or widespread
```bash
# Grafana query:
# sum(rate(inec_http_requests_total{status=~"5.."}[5m])) by (handler)
```

### 3. Common causes

| Pattern | Likely Cause | Fix |
|---------|-------------|-----|
| All 500s on `/auth/*` | JWT secret mismatch across pods | Check `JWT_SECRET` env var is identical on all pods |
| All 500s on `/results/*` | Database connection exhaustion | Check `inec_db_connections_active` metric, increase pool |
| 500s on POST endpoints only | Write replica routing error | Check PgPool/pgscale read/write routing |
| 500s spike after deploy | Bad code release | `kubectl rollout undo deployment/inec-backend` |
| Gradual 500 increase | Memory leak / connection leak | Restart pods: `kubectl rollout restart deployment/inec-backend` |

### 4. Check circuit breaker states
```bash
curl -s https://api.inec.ng/admin/circuit-breakers | jq .
# Any breaker in "open" state? → that service is causing cascading failures
```

### 5. Check database health
```bash
# Active connections
kubectl -n inec exec -it <pg-pod> -- psql -U inec_admin -c "SELECT count(*) FROM pg_stat_activity WHERE state = 'active';"

# Long-running queries
kubectl -n inec exec -it <pg-pod> -- psql -U inec_admin -c "SELECT pid, now() - pg_stat_activity.query_start AS duration, query FROM pg_stat_activity WHERE state != 'idle' ORDER BY duration DESC LIMIT 10;"
```

## Recovery Actions

### Quick fix: Restart pods
```bash
kubectl -n inec rollout restart deployment/inec-backend
```

### If database-related:
```bash
# Kill stuck connections
kubectl -n inec exec -it <pg-pod> -- psql -U inec_admin -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE state = 'idle in transaction' AND query_start < now() - interval '5 minutes';"
```

### If deploy-related:
```bash
kubectl -n inec rollout undo deployment/inec-backend
```

## Recovery Verification
- Error rate drops below 1% within 5 minutes
- `curl https://api.inec.ng/healthz` returns 200
- Grafana overview dashboard shows green
