#!/usr/bin/env bash
# End-to-end validation for cmd/dbseed against a disposable PostgreSQL instance.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="$ROOT_DIR/inec-go-backend"
CONTAINER="inec-dbseed-test-$$"
DB_NAME="inec_dbseed_test"
DB_USER="dbseed_test"
DB_PASSWORD="${DBSEED_TEST_PASSWORD:-$(openssl rand -hex 24)}"
PORT="${DBSEED_TEST_PORT:-55432}"
DSN="postgres://${DB_USER}:${DB_PASSWORD}@127.0.0.1:${PORT}/${DB_NAME}?sslmode=disable"
REPORT="$(mktemp /tmp/inec-dbseed-report.XXXXXX.json)"

cleanup() {
  sudo docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  rm -f "$REPORT"
}
trap cleanup EXIT

sudo docker run -d --rm --name "$CONTAINER" --network host \
  -e POSTGRES_USER="$DB_USER" \
  -e POSTGRES_PASSWORD="$DB_PASSWORD" \
  -e POSTGRES_DB="$DB_NAME" \
  postgres:16-alpine -c "port=${PORT}" >/dev/null

for _ in $(seq 1 60); do
  if sudo docker exec "$CONTAINER" pg_isready -p "$PORT" -U "$DB_USER" -d "$DB_NAME" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
sudo docker exec "$CONTAINER" pg_isready -p "$PORT" -U "$DB_USER" -d "$DB_NAME" >/dev/null

for migration in "$BACKEND_DIR"/migrations/*.up.sql; do
  sudo docker exec -i "$CONTAINER" psql -v ON_ERROR_STOP=1 -p "$PORT" -U "$DB_USER" -d "$DB_NAME" < "$migration" >/dev/null
done
sudo docker exec "$CONTAINER" psql -v ON_ERROR_STOP=1 -p "$PORT" -U "$DB_USER" -d "$DB_NAME" \
  -c "CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, description TEXT, applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP); INSERT INTO schema_migrations(version, description) VALUES (999999, 'integration test marker') ON CONFLICT DO NOTHING;" >/dev/null

cd "$ROOT_DIR"
DATABASE_URL="$DSN" DBSEED_ENV=test ./scripts/seed_database --report "$REPORT" >/dev/null
first_count="$(sudo docker exec "$CONTAINER" psql -At -p "$PORT" -U "$DB_USER" -d "$DB_NAME" -c 'SELECT COUNT(*) FROM states;')"
test "$first_count" = "37"

DATABASE_URL="$DSN" DBSEED_ENV=test ./scripts/seed_database --report "$REPORT" >/dev/null
second_count="$(sudo docker exec "$CONTAINER" psql -At -p "$PORT" -U "$DB_USER" -d "$DB_NAME" -c 'SELECT COUNT(*) FROM states;')"
test "$second_count" = "37"

python3 - "$REPORT" <<'PY'
import json
import sys
report = json.load(open(sys.argv[1]))
assert report["schema_table_count"] == report["manifest_table_count"], report
assert report["uncovered_schema_tables"] == [], report
assert report["stale_manifest_tables"] == [], report
assert any(item["table"] == "states" and item["rows"] == 37 for item in report["seeded"]), report
PY

echo "dbseed PostgreSQL integration test passed: seeded ${second_count} baseline states idempotently"
