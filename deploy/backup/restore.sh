#!/usr/bin/env bash
# INEC platform — PostgreSQL restore script (R5-084).
#
# Restores a backup produced by the helm db-backup CronJob
# (helm/inec-platform/templates/db-backup-cronjob.yaml: plain `pg_dump | gzip`
# SQL files named inec_YYYYMMDD_HHMMSS.sql.gz).
#
# Safety model: restore goes into a NEW database (never over the live one).
# After validation you cut over by pointing DATABASE_URL at the restored
# database (or renaming). A pre-restore safety dump of the CURRENT target
# is taken first.
#
# Prerequisites: postgresql16-client (psql, pg_dump) and gzip.
#
# Usage:
#   ./restore.sh --backup /backups/inec_20260812_120000.sql.gz \
#                --admin-url postgres://postgres:PASS@pg-primary:5432/postgres \
#                --target-db inec
#   ./restore.sh --list --backup-dir /backups
#
# Required env: PGPASSWORD embedded in --admin-url, plus CONFIRM_RESTORE=yes
# for the final cutover rename (step 4 prints the exact commands instead of
# executing them unless CONFIRM_RESTORE=yes).
set -euo pipefail

BACKUP_FILE=""
BACKUP_DIR="/backups"
ADMIN_URL=""
TARGET_DB="inec"
LIST_ONLY=0
CONFIRM="${CONFIRM_RESTORE:-no}"

die() { echo "restore.sh: ERROR: $*" >&2; exit 1; }
log() { echo "restore.sh: $*"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --backup) BACKUP_FILE="$2"; shift 2 ;;
    --backup-dir) BACKUP_DIR="$2"; shift 2 ;;
    --admin-url) ADMIN_URL="$2"; shift 2 ;;
    --target-db) TARGET_DB="$2"; shift 2 ;;
    --list) LIST_ONLY=1; shift ;;
    *) die "unknown argument: $1" ;;
  esac
done

if [ "$LIST_ONLY" = "1" ]; then
  ls -lt "${BACKUP_DIR}"/inec_*.sql.gz 2>/dev/null || die "no backups found in ${BACKUP_DIR}"
  exit 0
fi

[ -n "$BACKUP_FILE" ] || die "--backup is required (or --list to enumerate)"
[ -f "$BACKUP_FILE" ] || die "backup file not found: $BACKUP_FILE"
[ -n "$ADMIN_URL" ] || die "--admin-url is required (superuser connection to the postgres server)"
command -v psql >/dev/null || die "psql not found"
command -v pg_dump >/dev/null || die "pg_dump not found"

TS="$(date +%Y%m%d_%H%M%S)"
RESTORE_DB="${TARGET_DB}_restore_${TS}"
SAFETY_DUMP="/tmp/pre_restore_safety_${TARGET_DB}_${TS}.sql.gz"

log "step 0: verifying backup integrity (gzip test)"
gzip -t "$BACKUP_FILE" || die "backup file fails gzip integrity check: $BACKUP_FILE"

log "step 1: pre-restore safety dump of current '${TARGET_DB}' (if it exists)"
if psql "$ADMIN_URL" -tAc "SELECT 1 FROM pg_database WHERE datname='${TARGET_DB}'" | grep -q 1; then
  TARGET_URL="$(echo "$ADMIN_URL" | sed "s|/postgres\$|/${TARGET_DB}|")"
  pg_dump "$TARGET_URL" | gzip > "$SAFETY_DUMP"
  log "  safety dump written: $SAFETY_DUMP ($(du -h "$SAFETY_DUMP" | cut -f1))"
else
  log "  target database '${TARGET_DB}' does not exist yet — skipping safety dump"
fi

log "step 2: creating restore database '${RESTORE_DB}'"
psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${RESTORE_DB}\"" >/dev/null

log "step 3: loading backup into '${RESTORE_DB}'"
RESTORE_URL="$(echo "$ADMIN_URL" | sed "s|/postgres\$|/${RESTORE_DB}|")"
gunzip -c "$BACKUP_FILE" | psql "$RESTORE_URL" -v ON_ERROR_STOP=1 -q

log "step 3b: post-load sanity checks"
TABLES=$(psql "$RESTORE_URL" -tAc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")
log "  public tables present: ${TABLES}"
[ "${TABLES:-0}" -gt 0 ] || die "restore produced 0 tables — aborting before cutover"
for t in results polling_units elections voters; do
  EXISTS=$(psql "$RESTORE_URL" -tAc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='${t}'")
  if [ "$EXISTS" = "1" ]; then
    N=$(psql "$RESTORE_URL" -tAc "SELECT count(*) FROM ${t}" 2>/dev/null || echo "n/a")
    log "  ${t}: ${N} rows"
  fi
done

cat <<EOF

restore.sh: step 4 — CUTOVER
  Restore verified in database '${RESTORE_DB}'.
  To cut over, either:
    a) point the platform DATABASE_URL at '${RESTORE_DB}', or
    b) rename databases (requires all clients disconnected):
         psql "$ADMIN_URL" -c "ALTER DATABASE \"${TARGET_DB}\" RENAME TO \"${TARGET_DB}_retired_${TS}\""
         psql "$ADMIN_URL" -c "ALTER DATABASE \"${RESTORE_DB}\" RENAME TO \"${TARGET_DB}\""
  Re-run with CONFIRM_RESTORE=yes to have this script execute option (b).
EOF

if [ "$CONFIRM" = "yes" ]; then
  log "CONFIRM_RESTORE=yes — performing rename cutover"
  psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${TARGET_DB}' AND pid <> pg_backend_pid()" >/dev/null
  psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -c "ALTER DATABASE \"${TARGET_DB}\" RENAME TO \"${TARGET_DB}_retired_${TS}\"" >/dev/null || die "rename of live db failed — investigate before continuing"
  psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -c "ALTER DATABASE \"${RESTORE_DB}\" RENAME TO \"${TARGET_DB}\"" >/dev/null
  log "cutover complete. Previous live database preserved as '${TARGET_DB}_retired_${TS}'."
fi

log "done."
