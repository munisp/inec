#!/bin/bash
# Pgpool-II failover script for INEC Election Platform
# Arguments:
# %d = failed node id
# %h = failed node hostname
# %p = failed node port
# %D = failed node database directory
# %m = new primary node id
# %H = new primary node hostname
# %M = old primary node id
# %P = old primary node port
# %r = new primary port
# %R = new primary database directory
# %N = old primary hostname
# %S = old primary port

FAILED_NODE_ID="$1"
FAILED_NODE_HOST="$2"
FAILED_NODE_PORT="$3"
FAILED_NODE_PGDATA="$4"
NEW_PRIMARY_NODE_ID="$5"
NEW_PRIMARY_NODE_HOST="$6"
OLD_PRIMARY_NODE_ID="$7"
OLD_PRIMARY_NODE_PORT="$8"
NEW_PRIMARY_PORT="$9"
NEW_PRIMARY_PGDATA="${10}"
OLD_PRIMARY_HOST="${11}"
OLD_PRIMARY_PORT="${12}"

TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')

echo "$TIMESTAMP: Failover triggered"
echo "  Failed node: id=$FAILED_NODE_ID host=$FAILED_NODE_HOST port=$FAILED_NODE_PORT"
echo "  New primary: id=$NEW_PRIMARY_NODE_ID host=$NEW_PRIMARY_NODE_HOST"
echo "  Old primary: id=$OLD_PRIMARY_NODE_ID host=$OLD_PRIMARY_HOST"

if [ "$FAILED_NODE_ID" = "$OLD_PRIMARY_NODE_ID" ]; then
    echo "$TIMESTAMP: Primary node $FAILED_NODE_HOST failed. Promoting $NEW_PRIMARY_NODE_HOST to primary."
    ssh -T -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        postgres@$NEW_PRIMARY_NODE_HOST \
        "pg_ctl promote -D $NEW_PRIMARY_PGDATA" 2>/dev/null || \
    psql -h "$NEW_PRIMARY_NODE_HOST" -p "$NEW_PRIMARY_PORT" -U inec_admin -d inec_db \
        -c "SELECT pg_promote(true, 30);" 2>/dev/null || \
    echo "$TIMESTAMP: WARNING - Could not promote $NEW_PRIMARY_NODE_HOST automatically. Manual intervention may be needed."

    echo "$TIMESTAMP: Failover to $NEW_PRIMARY_NODE_HOST complete."
else
    echo "$TIMESTAMP: Standby node $FAILED_NODE_HOST failed. No promotion needed."
fi

exit 0
