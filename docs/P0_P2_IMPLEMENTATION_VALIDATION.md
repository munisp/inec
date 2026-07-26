# P0–P2 External-Election Integration Validation

**Validation date:** 2026-07-26
**Scope:** BVAS device trust, signed envelopes, durable delivery, authorised IReV receipts, governed analytics, APISIX/OpenAppSec controls, TigerBeetle/Mojaloop settlement boundaries, and PWA/mobile operational status surfaces.

The validation was run against the implementation in this repository. It does not represent authorization to connect to an external IReV portal, Mojaloop FSPIOP, production Keycloak realm, Permify tenant, or financial ledger; those integrations remain fail closed until their documented credentials, policies, and approvals are supplied.

| Layer | Validation command or method | Result |
|---|---|---|
| Go backend | `go test ./...` in `inec-go-backend` | Passed. This includes device-gateway envelope validation, legacy-ingress rejection, operational settlement controls, and backend regression packages. |
| Rust verifier | `cargo test` in `services/inference-engine` using Rust 1.97.1 | Passed: 5 integrity tests, including canonical SHA-256 handling, prohibited payload rejection, signed attestation, and Ed25519 verification. |
| Python lakehouse | `pytest -q services/lakehouse-analytics/test_external_device.py services/lakehouse-analytics/tests/test_anomaly_detection.py` | Passed: 11 tests. The anomaly path now requires both Isolation Forest candidacy and a robust MAD confirmation before creating a finding. |
| Python Sedona service | `pytest -q services/sedona-spatial/test_main.py` | Passed: 3 tests. |
| PWA | `pnpm build` in `inec-frontend` | Passed. The PWA includes aggregated device enrollment/delivery state, device-gateway readiness, and per-result IReV receipt state. |
| Expo mobile | `pnpm typecheck` in `inec-mobile` | Passed. The evidence and health journeys use the authenticated external-election transport. |
| Compose definitions | `docker compose ... config --quiet` for base, device-gateway overlay, and operational-settlement overlay | Passed using non-secret validation placeholders and temporary mount directories. |
| PostgreSQL migrations | Postgres 16 isolated container, migrations `000022`–`000025` plus saved SQL assertions | Passed. The test proved that a TigerBeetle transfer reference is rejected before independent approval and that settlement audit rows are append-only. |
| Settlement launcher | `scripts/launch-operational-settlement.sh` with disabled flags | Passed. It exits with status 64 before any Compose startup when authorization flags are not enabled. |
| Route/mutation boundary | Repository scan | Passed. Generic Mojaloop quote, transfer, and settlement routes and generic production-ledger transfer routes are absent. Native TigerBeetle mutations occur only in `operational_settlement.go`. |
| Repository hygiene | `git diff --check` | Passed. |

## Operational Interpretation

The platform does not label unavailable infrastructure as ready. Device events require the mTLS gateway and all mandatory control-plane dependencies. An IReV receipt is displayed only when the backend has recorded it for the selected result. Mojaloop remains disabled by default and status-only outside the independently approved, evidence-bound operational settlement workflow.

## Required External Preconditions

Before enabling production external delivery, operators must complete the procedures in `docs/EXTERNAL_ELECTION_INTEGRATION_OPERATIONS.md`, load the corresponding Permify relations and policies, configure Keycloak service identities, supply production APISIX and Mojaloop mTLS material from the approved secret store, provision the native TigerBeetle accounts/codes, and obtain the applicable INEC and FSPIOP approvals.
