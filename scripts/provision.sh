#!/usr/bin/env bash
# INEC Platform — Service Provisioning Script
# Usage: ./scripts/provision.sh [environment]
# Environments: dev, staging, production
set -euo pipefail

ENV="${1:-dev}"
echo "=== INEC Platform Provisioning (${ENV}) ==="

# Check required tools
for cmd in docker docker-compose psql; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "ERROR: $cmd is required but not installed"
    exit 1
  fi
done

# Validate environment
case "$ENV" in
  dev|staging|production) ;;
  *) echo "ERROR: Invalid environment '$ENV'. Use: dev, staging, production"; exit 1 ;;
esac

# Check required env vars for production
if [ "$ENV" = "production" ] || [ "$ENV" = "staging" ]; then
  REQUIRED_VARS=(
    "DATABASE_URL"
    "JWT_SECRET"
    "BIOMETRIC_VAULT_MASTER_KEY"
    "KEYCLOAK_URL"
    "KEYCLOAK_REALM"
    "KEYCLOAK_CLIENT_ID"
    "KEYCLOAK_CLIENT_SECRET"
    "KAFKA_BROKERS"
    "REDIS_URL"
  )
  MISSING=()
  for var in "${REQUIRED_VARS[@]}"; do
    if [ -z "${!var:-}" ]; then
      MISSING+=("$var")
    fi
  done
  if [ ${#MISSING[@]} -gt 0 ]; then
    echo "ERROR: Missing required environment variables for $ENV:"
    printf '  - %s\n' "${MISSING[@]}"
    echo ""
    echo "Set these in your .env file or environment before deploying."
    exit 1
  fi
  echo "All required environment variables present."
fi

# PostgreSQL + PostGIS setup
echo ""
echo "--- PostgreSQL/PostGIS Setup ---"
if [ "$ENV" = "dev" ]; then
  echo "Starting PostgreSQL via Docker..."
  docker run -d --name inec-postgres \
    -e POSTGRES_USER=ngapp \
    -e POSTGRES_PASSWORD=ngapp \
    -e POSTGRES_DB=ngapp \
    -p 5432:5432 \
    postgis/postgis:16-3.4 2>/dev/null || echo "PostgreSQL container already running"
  sleep 3
  echo "Running migrations..."
  export DATABASE_URL="postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable"
else
  echo "Using DATABASE_URL from environment."
  echo "Running migrations..."
fi

# Run schema migrations — FAIL LOUD (R4-39c): a failed migration aborts
# provisioning. Errors are NOT swallowed; stderr is shown. Escape hatch:
# pass --allow-partial to keep going after a failure (loud warning).
ALLOW_PARTIAL=0
for arg in "$@"; do
  [ "$arg" = "--allow-partial" ] && ALLOW_PARTIAL=1
done
if [ -d "migrations" ]; then
  MIGRATION_FAILURES=()
  for f in migrations/*.sql; do
    echo "  Applying: $(basename "$f")"
    if ! psql "${DATABASE_URL:-postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable}" -v ON_ERROR_STOP=1 -f "$f"; then
      echo "ERROR: migration $(basename "$f") FAILED" >&2
      MIGRATION_FAILURES+=("$(basename "$f")")
      if [ "$ALLOW_PARTIAL" -ne 1 ]; then
        echo "ERROR: aborting provisioning due to failed migration (R4-39c)." >&2
        echo "       Re-run with --allow-partial to continue past failures (NOT for production)." >&2
        exit 1
      fi
    fi
  done
  if [ ${#MIGRATION_FAILURES[@]} -gt 0 ]; then
    echo "WARNING: --allow-partial in effect; ${#MIGRATION_FAILURES[@]} migration(s) FAILED:" >&2
    printf '  - %s\n' "${MIGRATION_FAILURES[@]}" >&2
    echo "WARNING: the database schema is INCOMPLETE. Do not run production workloads." >&2
  fi
fi

# Enable PostGIS (optional: warn loudly instead of silently swallowing when
# the extension is unavailable in this PostgreSQL build)
if ! psql "${DATABASE_URL:-postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable}" -c "CREATE EXTENSION IF NOT EXISTS postgis;"; then
  echo "WARNING: postgis extension could not be enabled (unavailable in this build?)" >&2
fi

# Redis setup
echo ""
echo "--- Redis Setup ---"
if [ "$ENV" = "dev" ]; then
  docker run -d --name inec-redis -p 6379:6379 redis:7-alpine 2>/dev/null || echo "Redis container already running"
else
  echo "Using REDIS_URL from environment."
fi

# Kafka setup (dev only — production uses managed Kafka)
echo ""
echo "--- Kafka Setup ---"
if [ "$ENV" = "dev" ]; then
  echo "Skipping Kafka in dev (using in-memory event bus fallback)"
else
  echo "Using KAFKA_BROKERS from environment."
fi

# Build Go backend
echo ""
echo "--- Building Go Backend ---"
cd inec-go-backend
go build -o inec-server .
echo "Build successful: inec-server"
cd ..

# Build frontend
echo ""
echo "--- Building Frontend ---"
cd inec-frontend
npm install --production=false
npm run build
echo "Frontend build complete: dist/"
cd ..

# Build ML inference server
echo ""
echo "--- ML Inference Server ---"
cd ml
pip install -r requirements.txt 2>/dev/null || pip3 install -r requirements.txt 2>/dev/null || echo "WARN: pip install failed — ML features may not work"
cd ..

# Generate platform config summary
echo ""
echo "=== Provisioning Complete (${ENV}) ==="
echo "  Backend:   inec-go-backend/inec-server (port 8088)"
echo "  Frontend:  inec-frontend/dist/ (serve via nginx/caddy)"
echo "  ML Server: ml/inference_server.py (port 8000)"
echo ""
if [ "$ENV" = "dev" ]; then
  echo "Start with:"
  echo "  cd inec-go-backend && APP_ENV=development ./inec-server"
  echo "  cd inec-frontend && npm run dev"
elif [ "$ENV" = "production" ]; then
  echo "Start with:"
  echo "  APP_ENV=production ./inec-go-backend/inec-server"
  echo ""
  echo "IMPORTANT: Ensure all middleware services are reachable:"
  echo "  - PostgreSQL (DATABASE_URL)"
  echo "  - Keycloak (KEYCLOAK_URL)"
  echo "  - Kafka (KAFKA_BROKERS)"
  echo "  - Redis (REDIS_URL)"
fi
