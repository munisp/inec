#!/bin/bash
set -e

echo "Starting PostgreSQL replica setup..."

if [ ! -f "$PGDATA/PG_VERSION" ]; then
    echo "No existing data found. Running pg_basebackup from primary..."

    until pg_isready -h pg-primary -p 5432 -U replicator; do
        echo "Waiting for primary to become ready..."
        sleep 2
    done

    rm -rf "$PGDATA"/*

    PGPASSWORD="${REPLICATOR_PASSWORD:-changeme}" pg_basebackup \
        -h pg-primary \
        -p 5432 \
        -U replicator \
        -D "$PGDATA" \
        -Fp -Xs -P -R \
        -S replica_slot_1

    cat >> "$PGDATA/postgresql.conf" <<EOF

# --- Replica Settings ---
hot_standby = on
primary_conninfo = 'host=pg-primary port=5432 user=replicator password=${REPLICATOR_PASSWORD:-changeme} application_name=pg-replica'
primary_slot_name = 'replica_slot_1'
shared_buffers = 256MB
effective_cache_size = 768MB
work_mem = 8MB
max_connections = 200
EOF

    echo "Replica base backup complete. Starting in standby mode."
else
    echo "Existing data found. Starting in standby mode."
fi

exec docker-entrypoint.sh postgres
