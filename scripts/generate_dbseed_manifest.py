#!/usr/bin/env python3
"""Generate the dbseed table coverage manifest from versioned PostgreSQL migrations.

The generated manifest does not invent fixture rows.  It declares who owns each
schema table so dbseed can fail when a new table is introduced without an
explicit data-lifecycle decision.
"""
from __future__ import annotations

import argparse
import json
import re
from pathlib import Path

CREATE_TABLE = re.compile(
    r"\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:public\.)?\"?([a-z_][a-z0-9_]*)\"?",
    re.IGNORECASE,
)
LINE_COMMENT = re.compile(r"--[^\n]*")
BLOCK_COMMENT = re.compile(r"/\*.*?\*/", re.DOTALL)
DROP_TABLE = re.compile(
    r"\bDROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:public\.)?\"?([a-z_][a-z0-9_]*)\"?",
    re.IGNORECASE,
)

# Only immutable, non-personal reference data belongs in the repository's
# default baseline fixture. Geographic records beyond states must be loaded
# from an approved official register, not fabricated by a seed utility.
FIXTURE_TABLES = {"states"}
SECURITY_TERMS = (
    "api_key", "auth", "credential", "device_key", "encryption", "hsm",
    "key", "mfa", "password", "secret", "session", "token", "vault",
)
EXTERNAL_TERMS = (
    "apisix", "dapr", "fabric", "fluvio", "ipfs", "keycloak", "kafka",
    "lakehouse", "mojaloop", "opensearch", "openappsec", "permify",
    "redis", "temporal", "tigerbeetle",
)


def classify(table: str) -> tuple[str, str]:
    if table in FIXTURE_TABLES:
        return "fixture", "Repository-approved immutable baseline reference data."
    if any(term in table for term in SECURITY_TERMS):
        return "security", "Created by the security or identity subsystem; never seeded from repository fixtures."
    if any(term in table for term in EXTERNAL_TERMS):
        return "external", "Owned or synchronized by an external integration; do not fabricate local rows."
    return "live", "Populated only through approved operational APIs, imports, or event flows."


def tables_from_migrations(migrations_dir: Path) -> list[str]:
    tables: set[str] = set()
    for path in sorted(migrations_dir.glob("*.up.sql")):
        sql = path.read_text()
        sql = BLOCK_COMMENT.sub("", sql)
        sql = LINE_COMMENT.sub("", sql)
        tables.update(match.group(1).lower() for match in CREATE_TABLE.finditer(sql))
        tables.difference_update(match.group(1).lower() for match in DROP_TABLE.finditer(sql))
    return sorted(tables)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--migrations", default="inec-go-backend/migrations")
    parser.add_argument("--output", default="fixtures/db/table_manifest.json")
    args = parser.parse_args()

    tables = tables_from_migrations(Path(args.migrations))
    manifest = {
        "version": 1,
        "generated_from": str(args.migrations),
        "tables": [
            {
                "table": table,
                "mode": classify(table)[0],
                "description": classify(table)[1],
                "required": table in FIXTURE_TABLES,
            }
            for table in tables
        ],
    }
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(manifest, indent=2) + "\n")
    print(f"wrote {len(tables)} table policies to {output}")


if __name__ == "__main__":
    main()
