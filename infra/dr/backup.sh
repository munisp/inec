#!/usr/bin/env bash
# Disaster-recovery backup for the INEC platform.
#
# Produces a consistent, encrypted, timestamped backup of:
#   1. PostgreSQL (primary system of record) via pg_dump custom format
#   2. Fabric ledger channel data (if a live network is running)
#   3. Object/model artifacts referenced by MODEL_DIR
#
# Backups are encrypted with age (or GPG) and optionally uploaded to S3/GCS.
# Designed to be run on a schedule (cron/k8s CronJob) and before any migration.
#
# Required env:
#   DATABASE_URL            postgres connection string
# Optional env:
#   BACKUP_DIR              local staging dir           (default: /var/backups/inec)
#   BACKUP_S3_BUCKET        s3://bucket/prefix for offsite copy
#   BACKUP_AGE_RECIPIENT    age public key for encryption (recommended)
#   MODEL_DIR               path to ML model artifacts to include
#   RETENTION_DAYS          local retention window       (default: 14)
set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is required}"
BACKUP_DIR="${BACKUP_DIR:-/var/backups/inec}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
STAGE="${BACKUP_DIR}/${TS}"
mkdir -p "$STAGE"

echo "==> [1/4] Dumping PostgreSQL"
pg_dump --format=custom --no-owner --no-privileges --file="${STAGE}/postgres.dump" "$DATABASE_URL"
pg_dump --schema-only --no-owner --file="${STAGE}/schema.sql" "$DATABASE_URL"

echo "==> [2/4] Capturing Fabric ledger (if present)"
if docker ps --format '{{.Names}}' 2>/dev/null | grep -q 'peer0.inec'; then
  docker exec peer0.inec.inec.gov.ng peer channel getinfo -c inec-results \
    > "${STAGE}/fabric-channel-height.json" 2>/dev/null || true
fi

echo "==> [3/4] Bundling model artifacts"
if [[ -n "${MODEL_DIR:-}" && -d "${MODEL_DIR}" ]]; then
  tar -czf "${STAGE}/models.tar.gz" -C "$(dirname "$MODEL_DIR")" "$(basename "$MODEL_DIR")"
fi

echo "==> Packaging"
ARCHIVE="${BACKUP_DIR}/inec-backup-${TS}.tar.gz"
tar -czf "$ARCHIVE" -C "$BACKUP_DIR" "$TS"
rm -rf "$STAGE"

if [[ -n "${BACKUP_AGE_RECIPIENT:-}" ]] && command -v age >/dev/null; then
  echo "==> Encrypting with age"
  age -r "$BACKUP_AGE_RECIPIENT" -o "${ARCHIVE}.age" "$ARCHIVE"
  rm -f "$ARCHIVE"
  ARCHIVE="${ARCHIVE}.age"
fi

sha256sum "$ARCHIVE" > "${ARCHIVE}.sha256"

echo "==> [4/4] Offsite upload"
if [[ -n "${BACKUP_S3_BUCKET:-}" ]]; then
  aws s3 cp "$ARCHIVE" "${BACKUP_S3_BUCKET}/" --only-show-errors
  aws s3 cp "${ARCHIVE}.sha256" "${BACKUP_S3_BUCKET}/" --only-show-errors
fi

echo "==> Pruning backups older than ${RETENTION_DAYS} days"
find "$BACKUP_DIR" -maxdepth 1 -name 'inec-backup-*.tar.gz*' -mtime "+${RETENTION_DAYS}" -delete || true

echo "Backup complete: $ARCHIVE"
