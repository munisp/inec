# W6 HANDOFF — Go-owned changes required to complete R5 W6 items

Config/manifest halves landed on branch r5-w6. The following require edits
to Go files owned by other waves; ownership per GAP_REGISTER.md wave table.

## R5-086 (HIGH) — Redis fail-closed limiter → fail-open with local fallback [W2: main.go]
- `inec-go-backend/main.go:1082-1087, 1116-1118`: in production, Redis
  unreachable => reject EVERY governed path. Change to fail-open using the
  existing non-prod local-limiter fallback + emit a metric/log alarm.
- Sentinel-aware Redis client (or managed-failover endpoint) needed to use
  the `redis-replica` added in docker-compose.yml; client currently uses
  plain `redis.ParseURL` (mw_redis.go).

## R5-091 (MEDIUM) — backend limiter keying & per-endpoint budgets [W2: main.go]
- `inec-go-backend/main.go:1174, 1179-1184`: `/results` 60 req/min per
  client IP (X-Forwarded-For) breaks behind carrier CGNAT; `/ingestion/*`
  unlimited. Key on authenticated officer/device identity where available,
  keep XFF only behind TRUSTED_PROXY_CIDRS validation, and align budgets
  with the APISIX edge budgets (config/apisix/apisix.yaml header: ~200 rps
  national submit peak, <10 rps per egress IP).

## R5-087 (HIGH) — real migrate-only entrypoint + advisory lock [W2: main.go, migrations.go]
- Implement `--migrate-only` (or `RUN_MIGRATIONS_ONLY=true`) to run
  migrations and exit 0; then simplify
  helm/inec-platform/templates/migration-job.yaml (currently boots the full
  server and polls /readiness — works today, but a dedicated flag is cleaner).
- Take `pg_advisory_lock` around the migration loop in
  `inec-go-backend/migrations.go:78-117` so racing pods cannot wedge
  mid-deploy; treat already-applied errors idempotently.

## R5-085 (HIGH) — producer/topic durability in Go [W4/W7: mw_kafka.go, cmd/gotv-svc/middleware.go]
- `inec-go-backend/mw_kafka.go:162,191`: topic auto-create RF=1 → RF=3 with
  min.insync.replicas=2; writer RequiredAcks RequireOne → RequireAll.
- `inec-go-backend/cmd/gotv-svc/middleware.go:73`: same RF=3 change.
- Reference values + retention already provisioned by the `kafka-init`
  service in docker-compose.yml (audit topics 3y retention).

## R5-088/R5-094 (HIGH/MEDIUM) — metric emission gaps [W2]
- `inec_results_submitted_total` is registered (metrics.go) but never
  incremented — emit it in `handleSubmitResult` (handlers.go) with
  state_code/status labels; until then INECResultSubmissionStalled may
  false-fire.
- `inec_arch_circuit_breaker_state`, `inec_arch_biometric_verifications_total`,
  `inec_arch_events_published_total`, `inec_arch_active_polling_units`,
  `inec_arch_service_request_duration_seconds`, `inec_arch_events_consumed_total`:
  referenced by observability/grafana dashboards (and previously by alerts)
  but emitted nowhere — emit from the architecture/biometric layers or
  delete the panels. Alerts depending on these were removed/proxied in
  k8s/alerting.yaml + observability/prometheus/alerts.yml.
- Split Go services (cmd/*-svc) never call initTracing; Python/Rust
  services mostly emit no metrics (R5-094) — add OTEL/metrics per service
  (owning waves), ServiceMonitor coverage for them added on r5-w6 only
  where a /metrics endpoint exists (gotv-svc).
