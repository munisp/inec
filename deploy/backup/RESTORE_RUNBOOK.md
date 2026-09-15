# Database Restore Runbook (R5-084)

Scope: restoring the INEC platform PostgreSQL database from the backups the
repo actually produces (helm `db-backup` CronJob: plain `pg_dump | gzip`
every 6h into the `<release>-db-backups` PVC) and, once WAL archiving is
provisioned (see `postgresql-pitr.conf`), point-in-time recovery.

**Honesty note:** this runbook ships with the repo and the script below has
been statically reviewed and shellcheck-clean, but it has NOT been rehearsed
against a production-like cluster in this audit. The quarterly drill claimed
in `docs/DISASTER_RECOVERY.md` is an aspiration, not a recorded event — the
first action below schedules the real one.

## 0. Before you need this (standing tasks)

1. Schedule a **real** restore drill (recommend monthly, election-week
   daily): run section 2 end-to-end on a scratch namespace and record
   timings — that measured number replaces the guessed RTO.
2. Provision off-cluster backup storage (EXTERNAL dependency): object
   storage bucket + lifecycle policy. Until then all backups live on one
   RWO PVC in the same cluster as the database — cluster loss = backup loss.
3. Enable WAL archiving per `deploy/backup/postgresql-pitr.conf` so PITR
   (section 3) becomes available; without it the best achievable RPO is the
   last pg_dump, i.e. **up to 6 hours of data loss**.

## 1. Identify the backup

Backups are named `inec_YYYYMMDD_HHMMSS.sql.gz` on the backup PVC.
Attach a tools pod to the PVC or `kubectl cp` from the most recent
`db-backup` CronJob pod:

```sh
# label matches helm/inec-platform/templates/db-backup-cronjob.yaml:
kubectl get jobs -n inec -l app.kubernetes.io/name=<release>-db-backup
# list available backups (from any pod mounting the PVC, or locally):
deploy/backup/restore.sh --list --backup-dir /backups
```

Pick the newest backup **older than the incident** (corruption propagates).

## 2. Restore (safe, no overwrite of live DB)

```sh
deploy/backup/restore.sh \
  --backup /backups/inec_YYYYMMDD_HHMMSS.sql.gz \
  --admin-url "postgres://postgres:${PG_ADMIN_PASSWORD}@<pg-host>:5432/postgres" \
  --target-db inec
```

The script:
1. gzip-tests the backup;
2. takes a **pre-restore safety dump** of the current live database;
3. restores into a NEW database `inec_restore_<timestamp>` — never over the
   live one;
4. runs sanity checks (table count, row counts for results/polling_units/
   elections/voters);
5. prints cutover options. With `CONFIRM_RESTORE=yes` it performs the
   rename cutover (terminates connections, renames live → `_retired_`,
   restored → live).

Post-cutover checklist:
- [ ] backend pods reconnect (roll restart: `kubectl rollout restart deploy/<release>-backend`)
- [ ] gotv-svc pods reconnect too — `helm/inec-platform/templates/deployment-gotv.yaml`
      uses the SAME `<release>-db-credentials` DATABASE_URL, and the gateway
      routes `/gotv/*` to it (`kubectl rollout restart deploy/<release>-gotv`)
- [ ] `/healthz` and `/readiness` green
- [ ] spot-check: latest result submissions present, collation totals match
      the blockchain-attested tally where available
- [ ] keep the `_retired_` database and the safety dump until the incident
      review closes

## 3. Point-in-time recovery (requires WAL archiving — EXTERNAL infra)

Only available once `postgresql-pitr.conf` is applied and WAL is flowing to
object storage. PITR uses the wal-g/pgBackRest tooling named in that file;
standard flow: restore the latest base backup to a new instance, then reply
WAL to `recovery_target_time` just before the incident. Do NOT improvise
this during an incident — rehearse in the drill from section 0.

## 4. What this runbook does NOT cover

- Cross-region failover: no standby region exists in any committed manifest.
- Redis: the helm chart deploys the Bitnami subchart with
  `architecture: replication` and `replica.replicaCount: 3`
  (`helm/inec-platform/values.yaml`) behind `<release>-redis-master`, so a
  single pod loss self-heals via sentinel. Its contents (rate-limit counters,
  idempotency keys, cache) are NOT backed up — after a full cluster loss,
  in-flight idempotency dedup windows reset; clients must tolerate retries.
- Kafka: topics are created RF=3 in the compose reference topology
  (docker-compose.yml broker env + topic-init; R5-085), but message restore
  is not covered by database backups — events between the last pg_dump and
  the incident are lost.
