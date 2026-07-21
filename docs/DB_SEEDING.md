# PostgreSQL Database Seeding

`cmd/dbseed` is the single supported repository seed path. It seeds **approved, deterministic, non-personal fixtures** into a database that has already received every versioned PostgreSQL migration. It is not an election simulator, and it will not create voters, users, credentials, results, biometric templates, ledger entries, workflow history, or external-integration records.

> **Safety model:** Reference data may be seeded from reviewed fixtures. Operational data must arrive through the owning API, approved import, or live integration. Security and integration-owned records are never manufactured by repository seed data.

## Quick start

The wrapper accepts no implicit database configuration. Use it only against a development or test database.

```bash
export DATABASE_URL='postgres://inec:password@localhost:5432/inec_dev?sslmode=disable'
export DBSEED_ENV=development
./scripts/seed_database --report .audit/dbseed-baseline-report.json
```

For a transaction-only validation with no persisted writes, add `--dry-run`.

```bash
DBSEED_ENV=test ./scripts/seed_database --dry-run --report /tmp/dbseed-report.json
```

The command rejects all of the following:

| Condition | Result |
|---|---|
| Missing `--confirm-non-production` | Refuses to run. |
| Environment other than `development` or `test` | Refuses to run. |
| `APP_ENV=production` in the wrapper | Refuses to run. |
| DSN whose host or path appears to target production | Refuses to run. |
| Database without recorded migrations | Refuses to run. |
| Fixture table or column not found in PostgreSQL | Refuses to run. |
| Unclassified schema table | Refuses to run when coverage is required. |

## Fixture contract

Fixtures are JSON files under `fixtures/db`. A fixture contains a profile, an auditable source statement, and table rows. Every seeded table declares its conflict columns, which makes the command idempotent through `ON CONFLICT (...) DO NOTHING`.

```json
{
  "version": 1,
  "profile": "baseline",
  "source": "Authoritative reference description",
  "tables": [
    {
      "table": "states",
      "conflict_columns": ["code"],
      "rows": [{"code": "FC", "name": "Federal Capital Territory"}]
    }
  ]
}
```

The bundled `baseline.json` contains only the 36 Nigerian states and Federal Capital Territory. It contains no personal, credential, election, biometric, or result data.

## Complete schema coverage without fabricated rows

`fixtures/db/table_manifest.json` is generated from every `*.up.sql` migration by:

```bash
python3 scripts/generate_dbseed_manifest.py
```

Every discovered table is classified as one of the following modes:

| Mode | Meaning |
|---|---|
| `fixture` | Safe, approved static reference data may be seeded from a reviewed fixture. |
| `live` | Populated only by approved operational APIs, imports, or event flows. |
| `external` | Owned or synchronized by a real external integration such as Keycloak, Temporal, Kafka, Fluvio, APISIX, Permify, or TigerBeetle. |
| `security` | Created only by security or identity flows and never by repository fixtures. |

This is the definition of **complete coverage**: every migration table has an explicit data-lifecycle owner. Empty operational, security, and integration tables are intentional and are reported rather than hidden by fake data.

## Adding an approved fixture

1. Add reviewed, non-personal data to a named profile in `fixtures/db`.
2. Declare its source, table name, conflict columns, and rows.
3. Ensure the table has `fixture` mode in `table_manifest.json`; do not reclassify security, external, or live tables merely to populate a demo.
4. Run the dry-run validation, then the normal command against a disposable test database.
5. Commit the fixture, manifest update, and source-review evidence together.

## Production policy

No repository command seeds production. Production reference data must be imported from an approved, versioned INEC source under a controlled change process. The seed command intentionally has no production override.
