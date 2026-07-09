#!/usr/bin/env bash
# Disaster-recovery restore for the INEC platform. Inverse of backup.sh.
#
# Verifies the archive checksum, decrypts (age) if needed, and restores the
# PostgreSQL database with pg_restore. Model artifacts are extracted to MODEL_DIR.
#
# Usage:
#   DATABASE_URL=... ./restore.sh /var/backups/inec/inec-backup-<TS>.tar.gz[.age]
#
# Required env:
#   DATABASE_URL            target postgres connection string
# Optional env:
#   BACKUP_AGE_IDENTITY     age private key file (required if archive is .age)
#   MODEL_DIR               where to extract bundled models
#   DROP_EXISTING=1         drop & recreate objects before restore (destructive)
set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is required}"
ARCHIVE="${1:?usage: restore.sh <backup-archive>}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if [[ -f "${ARCHIVE}.sha256" ]]; then
  echo "==> Verifying checksum"
  (cd "$(dirname "$ARCHIVE")" && sha256sum -c "$(basename "$ARCHIVE").sha256")
fi

if [[ "$ARCHIVE" == *.age ]]; then
  : "${BACKUP_AGE_IDENTITY:?BACKUP_AGE_IDENTITY required to decrypt .age archive}"
  echo "==> Decrypting"
  age -d -i "$BACKUP_AGE_IDENTITY" -o "${WORK}/backup.tar.gz" "$ARCHIVE"
  ARCHIVE="${WORK}/backup.tar.gz"
fi

echo "==> Extracting"
tar -xzf "$ARCHIVE" -C "$WORK"
STAGE="$(find "$WORK" -maxdepth 1 -type d -name '20*' | head -1)"
[[ -n "$STAGE" ]] || { echo "ERROR: no dump directory in archive"; exit 1; }

echo "==> Restoring PostgreSQL"
RESTORE_FLAGS=(--no-owner --no-privileges --exit-on-error)
if [[ "${DROP_EXISTING:-0}" == "1" ]]; then
  RESTORE_FLAGS+=(--clean --if-exists)
fi
pg_restore "${RESTORE_FLAGS[@]}" --dbname="$DATABASE_URL" "${STAGE}/postgres.dump"

if [[ -f "${STAGE}/models.tar.gz" && -n "${MODEL_DIR:-}" ]]; then
  echo "==> Restoring model artifacts to ${MODEL_DIR}"
  mkdir -p "$(dirname "$MODEL_DIR")"
  tar -xzf "${STAGE}/models.tar.gz" -C "$(dirname "$MODEL_DIR")"
fi

echo "Restore complete from: $1"
echo "NOTE: Fabric ledger state is rebuilt by re-joining peers to the channel;"
echo "      see blockchain/fabric/deploy.sh. The PG ledger tables are restored above."
