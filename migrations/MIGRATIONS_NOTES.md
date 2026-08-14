# MIGRATIONS_NOTES.md

Round-4 (R4-39c) remediation notes for the INEC migration series.

## The three migration series (do not merge them)

| Series | Location | Runner | Notes |
|---|---|---|---|
| go-backend | `inec-go-backend/migrations/` (000001–000027, up/down pairs) | embedded Go runner (`migrations.go`, `--migrate-only`), helm migration job | Authoritative for the main DB. All ups apply cleanly on PG 16.2 (264 tables). |
| root plain-SQL | `migrations/` (000016–000025 + `001_biometric_tables.sql`) | `scripts/provision.sh` loop (now fail-loud) | A **continuation** of the go series: it requires the go-backend schema (elections, users, gotv_*, bvas_devices, …) to be present. Version numbers 000019–000025 collide with the go series but contain different DDL — this is historical and retained for compatibility; do not renumber (deployments may have recorded applied filenames). |
| biometric-rust | `services/biometric-rust/migrations/001_biometric_tables.sql` | embedded in `src/db.rs` at startup | Self-contained for the biometric-vault database. Distinct schema from the same-named root file (the collision is historical; the embedded file is the one the Rust service uses). |
| campaign-platform drizzle | `campaign-platform/drizzle/` | drizzle-kit | Own database; journal-tracked. |

## R4-39c fixes applied

1. `migrations/000017_fk_constraints.sql` — referenced the nonexistent column
   `gotv_tasks.assigned_volunteer_id`; corrected to the real column `volunteer_id`
   (defined in `inec-go-backend/migrations/000026_gotv_operational_schema.up.sql:403`
   and runtime DDL `gotv.go:263`). The migration could never apply before this fix.
2. `migrations/000022_external_election_device_gateway.sql` — failed because
   go-backend's `bvas_devices` (migration 000001) declares `id text NOT NULL` with no
   PRIMARY KEY/UNIQUE. The migration now creates a guarded
   `CREATE UNIQUE INDEX IF NOT EXISTS bvas_devices_id_unique ON bvas_devices(id)`
   before the FK that references it. `000023` and `000025` only failed as
   downstream consequences of 000022 (they reference `external_integration_outbox`
   and `bvas_device_enrollments`, both created by 000022); they needed no content
   changes once 000022 applies.
3. `migrations/001_biometric_tables.sql` — all plain `CREATE INDEX` made
   `IF NOT EXISTS`; the two indexes on root-only columns
   (`cancelable_transforms.is_revoked`, `bvas_devices.assigned_pu`) are guarded by
   column-existence DO blocks because the go-backend schema's same-named tables
   (created by `inec-go-backend/migrations/000023_biometric.up.sql`) win the
   `CREATE TABLE IF NOT EXISTS` race.
4. go-backend downs: added the missing `000015_koh_indicators.down.sql` (10 tables)
   and `000019_party_primaries_remote_voting.down.sql` (14 tables + restores the
   original `elections_election_type_check`); completed
   `000022_election_management.down.sql` (8 missing drops); fixed
   `000024_schema_reconciliation.down.sql` which hard-failed on
   `gotv_field_reports` / `gotv_ride_requests` / `gotv_volunteers` /
   `stablecoin_ledger` / `stablecoin_wallets` — those tables are created by the
   *later* 000026 whose down has already dropped them, so the ALTERs are now
   `to_regclass`-guarded.

## Executed proof (PostgreSQL 16.2, pgserver socket)

```
# go series: fresh DB -> 23 ups -> all downs in reverse
baseline tables: 0  ->  after ups: 264  ->  after downs: 0   (all exit 0)

# root series on top of the go schema
for f in $(ls migrations/*.sql | sort); do psql -v ON_ERROR_STOP=1 -f $f; done
# 11/11 exit 0; tables 264 -> 298
```

Before these fixes the same rehearsals produced: 000017 exit 3
(`assigned_volunteer_id does not exist`), 000022 exit 3 (`no unique constraint
matching given keys for bvas_devices`), 000023/000025 exit 3 (missing relations),
001 exit 3 (`is_revoked does not exist`), and a full down-run that failed at
000024 and left 32 tables.

## Runtime DDL ownership (initDB / service-init)

Some tables are (also) created at service startup, outside any migration runner:

- `inec-go-backend/device_gateway.go` (`initDeviceGatewaySchema`) — SQLite-dialect
  **development parity** for migration 000022 (`bvas_device_enrollments`,
  `bvas_device_gateway_inbox`, `external_integration_outbox`,
  `external_portal_integrations`, …). On PostgreSQL the migration is authoritative.
- `inec-go-backend/bvas.go`, `gotv.go` — `CREATE TABLE IF NOT EXISTS` parity DDL
  (SQLite dev mode). On PostgreSQL these are no-ops over the migrated schema.

Because this parity DDL uses `IF NOT EXISTS`, it neither conflicts with nor
replaces the migrations; a full `migrate down` removes only migration-owned
objects, and a fresh database has zero tables before migration 000001 — so the
go series unwinds 100% (264 → 0). Root `migrations/` has no down files by design
(one-way hardening/evidence migrations); rollback of that series is manual.
