# Runbook: INEC Backend Down

**Alert:** `INECBackendDown`  
**Severity:** CRITICAL  
**Team:** Platform  

## Symptoms
- `/healthz` returns non-200 or times out
- All API requests fail
- Election operations halted

## Immediate Actions (< 5 minutes)

### 1. Verify the alert
```bash
curl -s -o /dev/null -w "%{http_code}" https://api.inec.ng/healthz
# Expected: 200
# If non-200 or timeout → proceed
```

### 2. Check pod status (Kubernetes)
```bash
kubectl -n inec get pods -l app=inec-backend
kubectl -n inec describe pod <pod-name>
kubectl -n inec logs <pod-name> --tail=100
```

### 3. Check if it's a rollout issue
```bash
kubectl -n inec rollout status deployment/inec-backend
# If a bad deploy is in progress:
kubectl -n inec rollout undo deployment/inec-backend
```

### 4. Check resource exhaustion
```bash
kubectl -n inec top pods -l app=inec-backend
# OOMKilled? → increase memory limits in Helm values
# CPU throttling? → increase CPU limits
```

### 5. Check database connectivity
```bash
kubectl -n inec exec -it <pod-name> -- /bin/sh -c 'curl -s localhost:8088/healthz | jq .middleware'
# Look for database: connected: false
```

### 6. Check external dependencies
```bash
# From within the pod:
kubectl -n inec exec -it <pod-name> -- /bin/sh -c '
  curl -s localhost:8088/middleware/health | jq .
'
```

## Escalation

| Time | Action |
|------|--------|
| 0-5 min | On-call engineer investigates |
| 5-15 min | If not resolved → page secondary on-call |
| 15-30 min | If not resolved → page Engineering Manager |
| 30+ min (election day) | If not resolved → page CTO + INEC liaison |

## Recovery Verification
```bash
# Verify backend is healthy
curl -s https://api.inec.ng/healthz | jq .status
# Expected: "healthy"

# Verify all middleware connected
curl -s https://api.inec.ng/middleware/health | jq '.[] | {name, connected}'

# Verify recent results can be submitted (dry run)
curl -s https://api.inec.ng/readiness
# Expected: 200
```

## Post-Incident
1. Create incident report within 24 hours
2. Update this runbook if new failure mode discovered
3. Add regression test for the failure case
