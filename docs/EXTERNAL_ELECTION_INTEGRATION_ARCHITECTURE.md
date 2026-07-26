# External Election-System Integration Architecture

**Scope:** This design hardens the platform’s BVAS-style device ingestion and IReV-style result-portal integration without claiming an official INEC connection until an authorized interface, device certificates, client credentials, and acceptance evidence are configured.

## Design principle

> **The platform must distinguish an internally accepted device event from an officially accepted external-system receipt.** No worker, queue, dashboard, or fallback may manufacture a BVAS, IReV, Mojaloop, or other external confirmation.

The PostgreSQL result/evidence ledger remains the transactional system of record. It stores protected raw-envelope metadata, validation decisions, immutable event links, durable outbox rows, portal submission attempts, and externally returned receipts. The Fabric anchor remains a separate consortium proof path. Raw biometric payloads, EC8A images, device private keys, and voter identifiers do not enter Kafka, Fluvio, OpenSearch, lakehouse exports, or Fabric anchors.

## Device trust contract

A device event is an Ed25519-signed canonical JSON envelope:

```json
{
  "version": "bvas-envelope-v1",
  "device_id": "BVAS-...",
  "election_id": 123,
  "polling_unit_code": "...",
  "event_type": "accreditation|result_capture|heartbeat|incident",
  "sequence": 42,
  "nonce": "base64url-32-byte-value",
  "observed_at": "2026-07-26T12:00:00Z",
  "payload_sha256": "lowercase-hex-sha256",
  "payload": {"...": "non-biometric operational fields only"},
  "signature": "standard-base64-ed25519-signature"
}
```

The Go gateway validates transport and enrollment state; the Rust integrity service verifies canonical bytes and Ed25519 signatures against the enrolled device public key. PostgreSQL enforces `(device_id, sequence)` and `(device_id, nonce)` uniqueness. Redis provides a short-lived `SET NX` replay gate. The gateway rejects any event that cannot pass all required trust controls.

## P0 integration flow

| Stage | Responsible component | Required behavior |
|---|---|---|
| Device enrollment | Go + Keycloak + Permify + PostgreSQL | Only an authorized officer may enroll a device public key, election, polling unit, firmware allowlist, and certificate fingerprint. |
| Edge access | APISIX + OpenAppSec + mTLS | APISIX terminates mTLS and routes only `/device-gateway/v1/*`; OpenAppSec inspects traffic. Direct backend access is denied in production. |
| Integrity verification | Rust | Canonicalizes envelope payload, validates SHA-256 and timestamp skew, verifies Ed25519 signature, and returns a signed verifier attestation. |
| Ingestion decision | Go + PostgreSQL + Redis | Enforces enrollment/policy/sequence/nonce/idempotency/geofence constraints, persists the immutable inbox record, and links accepted results to the evidence ledger. |
| Event fan-out | PostgreSQL outbox + Kafka + Dapr + Fluvio | Delivers only redacted event metadata after transaction commit. Kafka is the durable operational stream; Dapr supplies pub/sub and service invocation; Fluvio provides low-latency analytical ingestion. |
| Recovery | Temporal | Runs retry, reconciliation, and receipt-polling workflows from durable outbox/attempt state. Failure remains visible and unresolved; no success is inferred. |

## P1 official portal contract

The `IReVAdapter` is disabled until all of the following are configured and validated: HTTPS endpoint, client certificate/key/CA bundle, official service identity, interface version, signed-request policy, response schema, callback verification policy, and INEC-authorized onboarding status.

Once authorized, a portal submission consists of a signed external request derived from an approved evidence artifact. The system records a `pending` attempt before network I/O; it records `accepted`, `rejected`, or `unavailable` only from a verified external response or verified callback. It never maps a locally queued item to `completed`.

## P1 analytics and spatial controls

Python calculates non-decisive operational quality signals and exports redacted analytical rows. Apache Sedona is invoked only by a configured, approved spatial endpoint for device/polling-unit geofence and route controls. GeoLibre displays verified device trajectory and receipt status, but cannot modify evidence. Lakehouse ingests only redacted device-event and portal-receipt analytics rows.

## P2 operational governance

| Component | Required role |
|---|---|
| PostgreSQL | Transactional ledger, enrollment, inbox, outbox, portal receipts, policy versions, and immutable audit state. |
| Kafka | Durable event stream: `inec.bvas.device-events.v1`, `inec.portal.receipts.v1`, and `inec.integration.alerts.v1`. |
| Dapr | Service invocation for Rust/Python adapters, state/retry coordination, and pub/sub parity; unavailable dependencies block the affected operation when required. |
| Fluvio | Redacted high-rate operational event stream for analytics; never a source of official acceptance. |
| Temporal | Durable retry and portal-receipt reconciliation workflows; workflow failures are materialized in PostgreSQL. |
| Keycloak | Human enrollment and support-session identity. Device messages never rely on a human role as their sole trust signal. |
| Permify | Device-to-election/polling-unit authorization and officer enrollment permission checks. |
| Redis | Nonce/replay gate, short-lived rate-limit counters, and volatile device-presence cache; PostgreSQL remains authoritative. |
| OpenSearch | Searchable redacted security/operational events with deterministic correlation IDs. |
| APISIX + OpenAppSec | mTLS edge, rate limiting, request-size limits, WAF inspection, and route policy enforcement. |
| TigerBeetle | Double-entry accounting of approved device logistics/operational reimbursement commitments, not voter or result values. |
| Mojaloop | Optional, disabled-by-default approved settlement connector for already-authorized operational reimbursement only; never involved in voter accreditation or results. |
| Apache Sedona + GeoLibre | Verifiable spatial checks and operator visualization of declared versus observed device movement. |
| Lakehouse | Redacted analytical export, quality trends, and model-training data under policy/version controls. |

## Fail-closed rules

The following conditions are `503`/`409`/`422`, not fallback data: missing device enrollment; missing public key; invalid signature; replayed nonce; sequence regression; device/polling-unit/election mismatch; stale timestamp; missing required Redis/Kafka/Dapr/Temporal dependency; portal integration not authorized; unverified callback; invalid portal receipt; unconfigured required spatial checker; or unapproved payment settlement.

## External prerequisites

A live BVAS or IReV connection requires INEC’s written partner authorization, approved device/portal specification, certificate issuance and revocation process, sandbox access, signed callback contract, retention/privacy terms, representative load/penetration test, and election-day acceptance evidence. This implementation provides the controlled integration boundary and intentionally leaves it unavailable until those prerequisites are present.

## Apache Sedona deployment basis

The spatial authority uses the official `apache/sedona:1.9.0` image and a server-side Sedona Spark context. Apache Sedona’s Docker guidance documents release-tagged images in the `apache/sedona:<sedona_version>` form, including `1.9.0`; its Python guidance states that Sedona extends PySpark and requires a matching Sedona Spark artifact for the active Spark and Scala version. The platform therefore exposes spatial validation through a dedicated fail-closed service rather than treating browser-side geometry as authoritative. [1] [2]

## References

[1] [Apache Sedona: Use Sedona in Docker](https://sedona.apache.org/latest/setup/docker/)

[2] [Apache Sedona: Install Sedona Python](https://sedona.apache.org/latest/setup/install-python/)

## APISIX device mTLS deployment basis

The device edge uses APISIX client-to-gateway mTLS. The official APISIX guide configures a client CA in the SSL resource (`client.ca`) and demonstrates forwarding the verified client certificate fingerprint, serial number, and subject DN to the upstream route through `proxy-rewrite`. APISIX documents `client-control.max_body_size` as the route-level mechanism for enforcing a request-body limit; the device event route is therefore capped at 131,072 bytes. The backend accepts the mTLS marker only when APISIX has overwritten client-supplied headers with the certificate-derived values. [3] [4] [5]

[3] [Apache APISIX: Configure mTLS for client to APISIX](https://apisix.apache.org/docs/apisix/tutorials/client-to-apisix-mtls/)

[4] [Apache APISIX: client-control plugin](https://apisix.apache.org/docs/apisix/plugins/client-control/)

[5] [Apache APISIX: proxy-rewrite plugin](https://github.com/apache/apisix/blob/master/docs/en/latest/plugins/proxy-rewrite.md)
