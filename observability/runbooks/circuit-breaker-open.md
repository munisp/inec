# Runbook: Circuit Breaker Open

**Alert:** `INECCircuitBreakerOpen`  
**Severity:** CRITICAL  
**Team:** Platform  

## What This Means
A circuit breaker has tripped, meaning a downstream service has failed repeatedly (5+ consecutive failures in 30s). The breaker is now rejecting all requests to that service to prevent cascading failures.

## Diagnosis

### 1. Identify which breaker is open
```bash
curl -s https://api.inec.ng/admin/circuit-breakers | jq '.[] | select(.state == "open")'
```

### 2. Service-specific diagnosis

| Service | Check | Common Fix |
|---------|-------|-----------|
| **Redis** | `redis-cli ping` | Restart Redis pod, check memory limits |
| **Kafka** | Check broker logs | Restart Kafka, check topic config |
| **Keycloak** | `curl <KEYCLOAK_URL>/health` | Restart Keycloak, check realm config |
| **TigerBeetle** | Check TigerBeetle logs | Restart, check data directory |
| **PostgreSQL** | `pg_isready` | See database-down.md runbook |
| **Temporal** | Check Temporal server | Restart Temporal, check namespace |

### 3. Check if the upstream service is actually down
```bash
# From within the backend pod:
kubectl -n inec exec -it <backend-pod> -- /bin/sh -c '
  # Redis
  redis-cli -h redis -p 6379 ping
  # Kafka
  kafkacat -b kafka:9092 -L
  # Keycloak
  curl -s ${KEYCLOAK_URL}/health
'
```

## Recovery

### Automatic recovery
Circuit breakers automatically transition to **half-open** after the cooldown period (30s by default). A single successful request will close the breaker. No manual intervention needed if the upstream service recovers.

### Manual reset (if needed)
```bash
# Force-close a breaker via admin API
curl -X POST https://api.inec.ng/admin/circuit-breakers/reset -d '{"service": "<service-name>"}'
```

### If the upstream service won't recover
The platform is designed to degrade gracefully. When a circuit breaker is open:
- **Redis open** → rate limiting falls back to in-memory (per-pod, less accurate but functional)
- **Kafka open** → events queued in memory, flushed when Kafka recovers
- **Keycloak open** → JWT validation uses local verification (production only — requires JWT_SECRET)
- **TigerBeetle open** → financial ledger operations queued for retry

## Monitoring
- Watch `inec_circuit_breaker_state` metric in Grafana
- State transitions are logged: `circuit breaker state change` with `from` and `to` fields
