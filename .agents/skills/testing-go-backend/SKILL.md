---
name: testing-go-backend
description: Test the INEC Go backend end-to-end — JWT auth, stablecoin engine, compliance/NIN, Kafka/Redis PG fallback, and HTTP endpoints. Use when verifying changes to inec-go-backend/.
---

# Testing INEC Go Backend

## Overview
The Go backend (`inec-go-backend/`) is the main API server with 570+ endpoints. Testing covers JWT auth, stablecoin ECDSA engine, compliance/sanctions screening, Kafka/Redis PostgreSQL fallbacks, and the full HTTP API surface. All testing is shell-based (no browser/GUI needed, no recording).

## Prerequisites

### Build Tools
- **Go 1.22+**: Located at `/usr/local/go/bin/go` (add to PATH: `export PATH="/usr/local/go/bin:$PATH"`)
- **PostgreSQL**: Must be running with `ngapp` user/database

### Database Setup
```bash
# Verify PostgreSQL is running and accessible
PGPASSWORD=ngapp psql -U ngapp -h 127.0.0.1 -d ngapp -c "SELECT 1;"
```

If pg_hba.conf uses `scram-sha-256` and connections fail, change the `127.0.0.1/32` line to `md5`:
```bash
sudo sed -i 's/host.*all.*all.*127.0.0.1\/32.*scram-sha-256/host all all 127.0.0.1\/32 md5/' /etc/postgresql/*/main/pg_hba.conf
sudo systemctl reload postgresql
```

### Pre-existing Schema Bug
The `bvas.go` seed code may crash with `nil pointer dereference` due to MySQL-style `?` placeholders and wrong column names. **Workaround:** Insert a dummy row before starting the backend:
```sql
INSERT INTO bvas_devices (device_id, firmware_version) VALUES ('BVAS-DUMMY-001', '3.2.1') ON CONFLICT DO NOTHING;
```
This allows the seed code's `SELECT COUNT(*)` to return > 0 and skip the buggy INSERT.

## Test Execution

### 1. Build
```bash
export PATH="/usr/local/go/bin:$PATH"
cd inec-go-backend
go build -o /tmp/inec-backend .
# Expected: exit code 0, binary ~30-40MB
```

### 2. JWT_SECRET Enforcement (Production Mode)
```bash
# Must crash without JWT_SECRET in production
INEC_ENV=production DATABASE_URL="postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable" /tmp/inec-backend 2>&1
# Expected: Exits immediately with "JWT_SECRET environment variable is required in production"

# Must reject short secrets
JWT_SECRET=short DATABASE_URL="postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable" /tmp/inec-backend 2>&1
# Expected: Exits with "JWT_SECRET must be at least 32 characters"
```

### 3. Start Backend (Dev Mode)
```bash
DATABASE_URL="postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable" /tmp/inec-backend > /tmp/backend-log.txt 2>&1 &
echo "Backend PID: $!"
sleep 5

# Verify startup logs
grep -E 'Kafka.*PostgreSQL|Redis.*PostgreSQL|Dapr.*PostgreSQL|Stablecoin|listening' /tmp/backend-log.txt
# Expected:
#   "Redis using PostgreSQL-backed cache (persistent)"
#   "Kafka using PostgreSQL-backed event bus (persistent)"
#   "Dapr fallback: PostgreSQL-backed state/pubsub initialized"
#   "Stablecoin wallets seeded" (5 wallets)
#   "INEC Go Backend listening" on :8088
```

### 4. Obtain Admin JWT Token
```bash
TOKEN=$(curl -s -X POST http://localhost:8088/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['access_token'])")
```
Default admin credentials: `admin` / `admin123` (bcrypt hash in DB).

### 5. Stablecoin Wallet Lifecycle
```bash
# List seeded wallets (expect 5)
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8088/stablecoin/wallets

# Create new wallet
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  http://localhost:8088/stablecoin/wallets/create \
  -d '{"owner_id":"TEST-AGENT","owner_type":"agent","currency":"eNGN"}'
# Expected: wallet_id starting with "W-", public_key 66 chars (ECDSA P-256 compressed hex)

# Verify in PostgreSQL
PGPASSWORD=ngapp psql -U ngapp -h 127.0.0.1 -d ngapp -c \
  "SELECT wallet_id, balance, length(public_key) as pk_len FROM stablecoin_wallets WHERE owner_id='TEST-AGENT';"
```

### 6. Stablecoin Transfer (ECDSA-Signed)
```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  http://localhost:8088/stablecoin/transfer \
  -d '{"from_wallet":"INEC-TREASURY-001","to_wallet":"INEC-LOGISTICS-001","amount":1000.0,"purpose":"election_logistics","tx_type":"disbursement"}'
# Expected: status="confirmed", non-empty signature (128-char hex) and block_hash (64-char hex)

# Verify balances changed in PostgreSQL
PGPASSWORD=ngapp psql -U ngapp -h 127.0.0.1 -d ngapp -c \
  "SELECT wallet_id, balance FROM stablecoin_wallets WHERE wallet_id IN ('INEC-TREASURY-001', 'INEC-LOGISTICS-001');"
# Treasury should decrease by 1000, Logistics should increase by 1000
```

### 7. Sanctions Screening
```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  http://localhost:8088/compliance/sanctions/screen \
  -d '{"name":"John Doe","entity_type":"individual"}'
# Expected: screen_id, risk_level in (clear/low/medium/high), lists_checked array with 6 entries
```

### 8. Kafka/Redis PG Fallback Verification
```bash
# Kafka messages persisted to PostgreSQL
PGPASSWORD=ngapp psql -U ngapp -h 127.0.0.1 -d ngapp -c \
  "SELECT id, topic, key, created_at FROM kafka_messages ORDER BY id DESC LIMIT 5;"
# Expected: Messages from stablecoin transfers and compliance screening

# Redis cache persisted to PostgreSQL
PGPASSWORD=ngapp psql -U ngapp -h 127.0.0.1 -d ngapp -c \
  "SELECT key, length(value) as val_len, expiry FROM redis_cache LIMIT 5;"
# Expected: Rate limiter entries with TTL expiry timestamps
```

## Known Issues

### NIN Column Missing from voters Table
`compliance_engine.go:105` queries `WHERE nin = $1` but the `voters` table (created in `ems.go`) has no `nin` column. NIN verification via DB fallback silently fails — always returns `verified=false, match_score=0`. The fuzzy matching logic (Jaro-Winkler) is correct but never executes. Fix requires adding `nin TEXT` column to the voters schema.

### Health Endpoint Requires Auth
`GET /health` returns `{"error":"authentication required"}` without a Bearer token. Include the JWT token in health check requests.

### bvas.go Seed Data Bug (Pre-existing)
The seed function uses MySQL `?` placeholders and wrong column names (`id` instead of `device_id`, `polling_unit_code` instead of `assigned_pu`). This is a pre-existing bug not introduced by recent PRs. Workaround: insert a dummy bvas_devices row so the COUNT(*) check skips the seed INSERT.

## Devin Secrets Needed
- `DATABASE_URL` — PostgreSQL connection string (default: `postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable`)
- For production mode testing: `JWT_SECRET` — minimum 32 characters

## Tips
- Always kill the backend process before restarting: `kill $(lsof -ti:8088) 2>/dev/null`
- Log file at `/tmp/backend-log.txt` — check for panics: `grep -i 'panic\|fatal' /tmp/backend-log.txt`
- The backend seeds 5 stablecoin wallets on startup (INEC-TREASURY-001, INEC-ELECTION-FUND-001, CBN-ENAIRA-RESERVE, INEC-LOGISTICS-001, INEC-STAFF-PAYROLL)
- Compliance engine logs `nimc_configured:false` when NIMC API keys aren't set — this is expected and triggers DB fallback mode
- Rate limiter uses the PG-backed Redis cache, so rate limit entries appear in `redis_cache` table
