#!/bin/bash
set -euo pipefail

: "${POSTGRES_USER:?POSTGRES_USER must be set}"
: "${POSTGRES_DB:?POSTGRES_DB must be set}"
: "${REPLICATOR_PASSWORD:?REPLICATOR_PASSWORD must be set}"

echo "Configuring PostgreSQL primary for streaming replication and Keycloak..."

cat >> "$PGDATA/postgresql.conf" <<EOF

# --- Replication Settings (INEC Platform) ---
wal_level = replica
max_wal_senders = 5
wal_keep_size = 256MB
hot_standby = on
synchronous_commit = on
max_replication_slots = 5
archive_mode = off
shared_buffers = 256MB
effective_cache_size = 768MB
work_mem = 8MB
maintenance_work_mem = 128MB
max_connections = 200
listen_addresses = '*'
log_min_duration_statement = 100
log_checkpoints = on
log_lock_waits = on
EOF

cat >> "$PGDATA/pg_hba.conf" <<EOF

# Replication connections
host replication replicator 0.0.0.0/0 md5
host all all 0.0.0.0/0 md5
EOF

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<EOSQL
    CREATE USER replicator WITH REPLICATION ENCRYPTED PASSWORD '${REPLICATOR_PASSWORD}';
    SELECT pg_create_physical_replication_slot('replica_slot_1');
EOSQL

for database in keycloak temporal temporal_visibility; do
    if ! psql --username "$POSTGRES_USER" --dbname postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '${database}'" | grep -q 1; then
        psql --username "$POSTGRES_USER" --dbname postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${database}\" OWNER \"${POSTGRES_USER}\";"
    fi
done

echo "Primary configured for replication, Keycloak, and Temporal."
