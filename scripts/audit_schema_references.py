#!/usr/bin/env python3
"""Static SQL schema-reference audit for the INEC Go platform.

The audit inspects versioned PostgreSQL migrations and SQL-bearing Go string
literals. It removes comments, excludes Go test files, and distinguishes table
names from CTE aliases and SQL function arguments. It is conservative: any
remaining candidates should be manually reconciled against the migration chain.
"""
from __future__ import annotations

import json
import re
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BACKEND = ROOT / "inec-go-backend"
OUTPUT = ROOT / ".audit"
OUTPUT.mkdir(exist_ok=True)

RAW_STRING_RE = re.compile(r"`([^`]*)`", re.DOTALL)
QUOTED_STRING_RE = re.compile(r'"(?:\\.|[^"\\])*"', re.DOTALL)
SQL_HINT_RE = re.compile(
    r"^\s*(?:WITH\b|SELECT\b|INSERT\s+INTO\b|UPDATE\s+(?:[A-Za-z_][\w$]*\.)?[\"`']?[A-Za-z_][\w$]*[\"`']?\s+SET\b|DELETE\s+FROM\b|CREATE\s+(?:OR\s+REPLACE\s+)?(?:TABLE|VIEW)\b|ALTER\s+TABLE\b|DROP\s+(?:TABLE|VIEW)\b|TRUNCATE\s+TABLE\b)",
    re.IGNORECASE,
)
CREATE_TABLE_RE = re.compile(
    r"\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:[A-Za-z_][\w$]*\.)?[\"`']?([A-Za-z_][\w$]*)[\"`']?",
    re.IGNORECASE,
)
CREATE_VIEW_RE = re.compile(
    r"\bCREATE\s+(?:OR\s+REPLACE\s+)?VIEW\s+(?:[A-Za-z_][\w$]*\.)?[\"`']?([A-Za-z_][\w$]*)[\"`']?",
    re.IGNORECASE,
)
# Capture the referenced identifier and the immediate token after it. A closing
# parenthesis or opening parenthesis means the match is a function argument,
# rather than a relation in a FROM/JOIN clause.
TABLE_REF_RE = re.compile(
    r"\b(?:FROM|JOIN|INTO|UPDATE|DELETE\s+FROM|ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?|TRUNCATE\s+TABLE)\s+(?!OF\b)(?:ONLY\s+)?(?:[A-Za-z_][\w$]*\.)?[\"`']?([A-Za-z_][\w$]*)[\"`']?(?=\s|,|;|$|\)|\()",
    re.IGNORECASE,
)
CTE_RE = re.compile(
    r"(?:\bWITH|,)\s*([A-Za-z_][\w$]*)\s*(?:\([^)]*\))?\s+AS\s*\(",
    re.IGNORECASE,
)
IGNORED = {
    "select", "set", "values", "returning", "excluded", "information_schema",
    "pg_tables", "pg_indexes", "pg_class", "pg_stat_activity", "pg_stat_replication",
    "pg_settings", "pg_database", "pg_replication_slots", "table_constraints",
    "constraint_column_usage", "generate_series", "unnest", "columns", "tables",
}


def clean(name: str) -> str:
    return name.strip().strip('"`\'').lower()


def strings_from_go(text: str) -> list[str]:
    literals = RAW_STRING_RE.findall(text)
    for match in QUOTED_STRING_RE.findall(text):
        try:
            literals.append(bytes(match[1:-1], "utf-8").decode("unicode_escape"))
        except UnicodeDecodeError:
            literals.append(match[1:-1])
    return [literal for literal in literals if SQL_HINT_RE.search(strip_sql_comments(literal))]


def strip_sql_comments(statement: str) -> str:
    """Remove line and block comments before relation extraction."""
    without_blocks = re.sub(r"/[*].*?[*]/", "", statement, flags=re.DOTALL)
    return "\n".join(line.split("--", 1)[0] for line in without_blocks.splitlines())


def strip_sql_string_literals(statement: str) -> str:
    """Remove SQL single-quoted literals while preserving identifiers and syntax."""
    return re.sub(r"'(?:''|[^'])*'", "''", statement, flags=re.DOTALL)


def audit_file(path: Path) -> list[str]:
    if path.name.endswith("_test.go"):
        return []
    text = path.read_text(encoding="utf-8", errors="ignore")
    if path.suffix == ".sql":
        return [strip_sql_comments(text)]
    if path.suffix == ".go":
        return [strip_sql_comments(statement) for statement in strings_from_go(text)]
    return []


def cte_aliases(statement: str) -> set[str]:
    return {clean(match.group(1)) for match in CTE_RE.finditer(statement)}


def is_function_argument(statement: str, match: re.Match[str]) -> bool:
    """Reject `FROM x)` / `FROM x(` fragments used inside SQL expressions."""
    tail = statement[match.end():].lstrip()
    return tail.startswith(")") or tail.startswith("(")


def main() -> None:
    tables: dict[str, set[str]] = defaultdict(set)
    views: dict[str, set[str]] = defaultdict(set)
    refs: dict[str, set[str]] = defaultdict(set)
    statements = 0
    files = sorted(
        path for path in (list(BACKEND.rglob("*.go")) + list((BACKEND / "migrations").glob("*.sql")))
        if path.is_file() and not path.name.endswith("_test.go")
    )

    for path in files:
        relative = str(path.relative_to(ROOT))
        for statement in audit_file(path):
            statements += 1
            statement = strip_sql_string_literals(statement)
            aliases = cte_aliases(statement)
            for match in CREATE_TABLE_RE.finditer(statement):
                tables[clean(match.group(1))].add(relative)
            for match in CREATE_VIEW_RE.finditer(statement):
                views[clean(match.group(1))].add(relative)
            for match in TABLE_REF_RE.finditer(statement):
                name = clean(match.group(1))
                if name not in IGNORED and name not in aliases and not is_function_argument(statement, match):
                    refs[name].add(relative)

    defined = set(tables) | set(views)
    candidates = {
        name: sorted(paths)
        for name, paths in refs.items()
        if name not in defined and name not in IGNORED
    }
    result = {
        "scope": "inec-go-backend production Go SQL literals and versioned migrations",
        "files_scanned": len(files),
        "sql_statements_scanned": statements,
        "defined_tables": {name: sorted(paths) for name, paths in sorted(tables.items())},
        "defined_views": {name: sorted(paths) for name, paths in sorted(views.items())},
        "referenced_tables": {name: sorted(paths) for name, paths in sorted(refs.items())},
        "candidate_missing_tables": dict(sorted(candidates.items())),
        "summary": {
            "defined_table_count": len(tables),
            "defined_view_count": len(views),
            "referenced_table_count": len(refs),
            "candidate_missing_table_count": len(candidates),
        },
    }
    (OUTPUT / "schema_static_audit.json").write_text(
        json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )

    lines = [
        "# Static Schema Reference Audit",
        "",
        "Scope: production Go SQL literals and PostgreSQL migrations in `inec-go-backend`. SQL comments, test-only SQL, CTE aliases, function arguments, and system-catalog metadata are excluded; remaining candidates require review.",
        "",
        "| Metric | Count |",
        "| --- | ---: |",
        f"| Files scanned | {len(files)} |",
        f"| SQL statements scanned | {statements} |",
        f"| Declared tables | {len(tables)} |",
        f"| Declared views | {len(views)} |",
        f"| Referenced tables | {len(refs)} |",
        f"| Candidate unbacked references | {len(candidates)} |",
        "",
        "## Candidate Unbacked References",
        "",
    ]
    if candidates:
        lines.extend(["| Table | Referencing files |", "| --- | --- |"])
        for name, paths in sorted(candidates.items()):
            shown = ", ".join(f"`{path}`" for path in paths[:8])
            suffix = " …" if len(paths) > 8 else ""
            lines.append(f"| `{name}` | {shown}{suffix} |")
    else:
        lines.append("No candidate unbacked references were found in the static scope.")
    (OUTPUT / "schema_static_audit.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(json.dumps(result["summary"], sort_keys=True))


if __name__ == "__main__":
    main()
