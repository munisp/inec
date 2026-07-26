# External Election Integration Operations

**Owner:** Electoral Operations, ICT Security, and Integration Governance

This runbook covers the deployed BVAS-style device gateway and the authorized IReV-style portal adapter. It is deliberately designed to fail closed: an internally stored event is not an official device or portal acceptance, and no middleware status may be interpreted as one.

## P0 device-trust operations

The device gateway accepts only a canonical signed envelope through the APISIX route `/device-gateway/v1/events`. The backend port is internal to the Compose network. In production, the request must arrive over the dedicated APISIX client-certificate SNI and carry APISIX-overwritten certificate metadata. The Go service rejects a missing APISIX marker, a failed TLS verification indicator, a certificate fingerprint that is not enrolled, a stale timestamp, a replayed nonce, a sequence regression, an invalid Ed25519 envelope signature, or any prohibited raw biometric or voter field.

| Control | Operational owner | Failure action |
|---|---|---|
| Device certificate issuance, revocation, and client CA | Device PKI authority | Do not enable the mTLS overlay until the certificate chain and revocation process are approved. |
| Device enrollment and Permify assignment | Electoral ICT officer | Bind one public key, certificate fingerprint, election, and polling unit per active device. Revoke before reassignment. |
| Rust integrity verifier | Platform cryptography owner | Treat a `503` or invalid attestation as a rejected device event; never bypass it with human-session ingress. |
| Redis replay gate and PostgreSQL inbox | Platform operations | Investigate all nonce or sequence conflicts. PostgreSQL remains the durable duplicate record. |
| OpenAppSec and APISIX | Security operations | Block suspicious traffic, preserve redacted events, and investigate the correlated request identifier. |

## APISIX client-certificate deployment

Place the APISIX server certificate, server key, and client-CA bundle in a protected host directory that is outside the repository. Set `APISIX_DEVICE_CONFIG_DIR` to a separate protected render directory, then populate the `DEVICE_GATEWAY_*` variables in the protected production environment file. Run:

```bash
scripts/launch-device-gateway.sh
```

The wrapper renders the standalone APISIX configuration with the SSL resource embedded from the mounted files, validates that the resource exists, renders the base Compose configuration plus `docker-compose.device-gateway.yml`, and starts APISIX. It refuses source-tree render paths and missing certificates. It never creates a development CA or fallback credential.

> The APISIX SSL resource is the actual client-certificate enforcement point. The Go headers are an additional binding check, not a substitute for TLS authentication.

## Event delivery and observability

Accepted device events become redacted PostgreSQL outbox records only after the immutable inbox transaction commits. The delivery worker records append-only outcomes for Kafka, Dapr, Fluvio, OpenSearch, and Temporal. An outbox record is `delivered` only when every required sink returns actual acceptance. Failed and unavailable sinks retain the record with exponential retry scheduling and an immutable failure history.

OpenSearch stores only the pre-approved redacted operational projection: correlation ID, device and envelope hashes, event type, election/polling-unit references, timestamp, attestation metadata, and permitted quality telemetry. Raw PVC values, biometric templates, images, device private keys, and portal credentials remain excluded.

## IReV-style portal authorization

The IReV adapter is intentionally unavailable until a sanctioned interface is supplied. Before activation, integration governance must record the official endpoint, mTLS client credential, CA bundle, OAuth client identity, interface version, response schema, callback signing rule, sandbox evidence, and election-day acceptance result. Portal submission is evidence-bound and idempotent. A response becomes `accepted` only after a verified external receipt or verified signed callback.

## Recovery and escalation

The protected device health endpoint reports APISIX mTLS configuration, Redis, Permify, OpenAppSec, OpenSearch, Kafka, Dapr, Fluvio, Temporal, and Rust integrity-verifier state. With `BVAS_DEVICE_GATEWAY_REQUIRED=true`, any unavailable component returns `503` instead of a partial-ready state.

| Symptom | Immediate action | Escalation record |
|---|---|---|
| Device gateway health is unavailable | Halt new device ingestion for the affected unit, preserve pending envelopes at the authorized device edge, and restore the named dependency. | Security and ICT incident with component, correlation ID, and timeline. |
| OpenSearch/Kafka/Dapr/Fluvio/Temporal delivery fails | Do not mark the outbox record delivered. Restore the sink and let the durable worker retry, or use the protected retry endpoint after review. | Outbox ID, sink, retry attempts, and external reference. |
| APISIX mTLS failure | Verify SNI, client chain, certificate fingerprint enrollment, server key, and CA bundle. Do not use general staff APIs as a bypass. | Device ID, certificate fingerprint, enrollment state, and rejected handshake evidence. |
| Portal receipt mismatch or webhook failure | Mark the portal attempt as unresolved and prevent any official-publication assertion. | Submission payload hash, receipt ID, callback signature status, and reconciliation decision. |

## Data retention and lakehouse boundary

The Python lakehouse and Apache Sedona authority consume only redacted device-event metadata. Sedona decisions use authoritative PostgreSQL polling-unit coordinates and return unavailable when its engine, token, or source data is absent. GeoLibre is an operator visualization surface; it cannot create a result, a device acceptance, a portal receipt, or a field package.

## P2 operational settlement controls

The operational-settlement API is limited to **device logistics, device reimbursement, and device repair commitments**. Its schema has no voter, PVC, biometric, ballot, polling result, or portal-result fields. A requester submits a UUID-idempotent commitment bound to an active BVAS enrollment, an immutable evidence SHA-256, a purpose-reference hash, and a recipient-reference hash. The full recipient reference is accepted only at explicitly authorized Mojaloop dispatch time and is not written to PostgreSQL, TigerBeetle, Kafka, Dapr, Fluvio, OpenSearch, or the lakehouse.

| Workflow transition | Required condition | Durable outcome |
|---|---|---|
| `requested` | Authorized requester, active device enrollment, evidence binding, configured operational cap | PostgreSQL commitment and append-only audit entry; redacted outbox event. |
| `approved` | Different authorized approver and Permify authorization | Approval is immutable; requester cannot self-approve. |
| `ledger_pending` | Native TigerBeetle is connected and configured account/transfer codes match | A pending double-entry operational commitment is created between the configured treasury and device-operational account. |
| `mojaloop_pending` | Explicit Mojaloop enablement, HTTPS TLS 1.3 mutual authentication, CA pinning, authorized FSPIOP switch, party lookup, and quote/transfer acceptance | External transfer is pending; TigerBeetle remains pending. |
| `settled` | Verified external settlement state on reconciliation | TigerBeetle pending transfer is posted and only a receipt hash is retained. |
| `voided` | No external transfer is pending or settled | TigerBeetle pending transfer is voided and the immutable audit trail is extended. |

The API returns `503 Service Unavailable` while the feature is disabled, Permify is unavailable, native TigerBeetle is unavailable, the operational account configuration is absent, or Mojaloop has not been explicitly authorized. It neither queues a payment nor fabricates a quote, transfer, settlement, receipt, or balance in those states. `MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED=false` is the production-template default. An enabled Mojaloop endpoint must be HTTPS, use a client certificate, private key, approved CA bundle, TLS 1.3, and an authorized payer FSP before it is considered connected.

> A `ledger_pending` TigerBeetle entry is an approved operational commitment, not an external payment confirmation. A `settled` status requires verified external reconciliation and a successful TigerBeetle post operation.

Use the protected `/operational-settlements/health` endpoint to inspect readiness. Only authorize the endpoint permissions `request_operational_settlement`, `approve_operational_settlement`, `commit_operational_settlement`, `dispatch_operational_settlement`, `reconcile_operational_settlement`, `void_operational_settlement`, and `view_operational_settlement` through the production Permify policy after finance-governance approval.
