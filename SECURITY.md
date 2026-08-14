# Security Policy

## Reporting a vulnerability

Report suspected vulnerabilities to the platform security team through the
private channel designated by the INEC programme office. Do not open public
issues for unremediated vulnerabilities.

## Known history exposure (R4-39) — action required by repo owners

The git **history** of this repository still contains a third-party
`tourismpay` database dump (with PII: NIN, phone numbers, names, emails),
three ~69 MB Go SDK tarballs, a committed APISIX default admin key, and a
seed file that created an admin account with an unsalted SHA-256 password.
These files are absent from the HEAD tree but remain fetchable from any
full clone.

The exact remediation procedure (git filter-repo commands, force-push
implications, clone invalidation) is maintained in the assurance runbook
`history-purge-runbook.md` accompanying the round-4 audit. Until the purge
is executed:

- treat any credential that ever appeared in history as compromised and keep
  it rotated/disabled;
- do not redistribute clones of this repository outside the programme.

## Hardening notes

- `scripts/bootstrap_integrations.py` refuses to run without an explicit
  `APISIX_ADMIN_KEY`; no default gateway keys are shipped (R4-39b).
- `scripts/provision.sh` aborts on any failed migration; `--allow-partial`
  exists only for development triage (R4-39c).
- See `migrations/MIGRATIONS_NOTES.md` for migration-series ownership and
  `docs/EVENT_CATALOG.md` for the event-topic contract.
