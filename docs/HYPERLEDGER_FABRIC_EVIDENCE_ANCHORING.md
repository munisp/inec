# Hyperledger Fabric Evidence Anchoring Architecture

**Author:** Manus AI  
**Status:** Implemented integration design  
**Scope:** Consortium anchoring for the INEC election-evidence lifecycle

## Purpose and Trust Boundary

The platform keeps **raw EC8A images, voter data, private reconciliation detail, and document-analysis payloads off-chain** in the existing PostgreSQL evidence store. PostgreSQL remains the transactional system of record for result workflow, document analysis, and access-controlled evidence retrieval. Hyperledger Fabric adds an independent consortium-verifiable record of the minimal cryptographic evidence required to prove that a signed lifecycle event or collation bundle existed in a particular approved form.

> Fabric anchoring is not a replacement for the operational database. It is an independent, replicated commitment layer for hashes, signatures, policy references, and commit receipts.

Each anchor is submitted through the Fabric Gateway client API. Fabric Gateway gathers the endorsements needed by the deployed chaincode policy and returns a commit result, rather than the application fabricating peer signatures or local block numbers.[1]

## Canonical Anchor Contract

The chaincode accepts a JSON `EvidenceAnchorV1` document. It contains no raw election artifact or personally identifying data.

| Field | Source | Requirement |
|---|---|---|
| `schema_version` | Constant | Must equal `1` |
| `anchor_id` | Backend | SHA-256 identifier computed from the immutable anchor fields |
| `anchor_type` | Backend | `result_event` or `collation_bundle` |
| `election_id` | PostgreSQL | Authoritative election reference |
| `result_id` | PostgreSQL | Optional result reference; no voter identity |
| `event_hash` | Evidence ledger | 64-character SHA-256 lifecycle hash |
| `payload_sha256` | Evidence ledger | 64-character SHA-256 private payload hash |
| `prior_event_hash` | Evidence ledger | Optional predecessor hash for event chains |
| `signature` | Rust signer | Base64 Ed25519 signature over `event_hash` |
| `signer_key_id` | Rust signer | Approved signer key identifier |
| `policy_version_id` | PostgreSQL | Approved election-policy reference |
| `created_at` | Backend | UTC RFC 3339 timestamp |

The Fabric anchor ID is deterministic. Re-submitting the same immutable event is idempotent and returns the existing state instead of creating an alternative history. Any conflicting value for an existing anchor ID is rejected by chaincode.

## Fabric Chaincode Rules

The `evidence-anchor` chaincode exposes the following operations:

| Transaction | Rule |
|---|---|
| `InitializeGovernance` | Creates the one-time governance record, containing the permitted MSP IDs and protocol version. It may only run before governance exists and requires an administrator identity. |
| `CreateAnchor` | Validates the canonical fields, confirms the caller MSP is governed, rejects duplicate or conflicting anchors, writes the immutable record, and applies state-based endorsement for the configured consortium MSPs. |
| `ReadAnchor` | Returns an anchor by ID. |
| `AnchorExists` | Returns whether the canonical anchor is present. |
| `GetAnchorHistory` | Returns the Fabric key history for independent audit. |

Fabric’s chaincode endorsement policy is the authoritative multi-organization approval policy. The included deployment configuration specifies an `AND('INECMSP.peer','ObserverMSP.peer')` policy as a secure baseline; the final consortium must agree and commit its own membership and endorsement policy. Fabric Gateway is designed to obtain the required endorsements and wait for commit status.[1]

## Failure and Finality Model

The Go backend creates a local anchor request alongside each signed evidence event. In a production deployment, `FABRIC_ANCHORING_REQUIRED=true` makes Fabric configuration and confirmed submission mandatory for integrity-controlled lifecycle transitions. If the Gateway, TLS material, identity, endorsement, or commit status is unavailable, the transition fails instead of claiming external finality.

In non-production environments, a failed optional submission is recorded as `pending` or `unavailable`; the evidence UI labels it explicitly and never represents it as consortium-confirmed. Administrators can retry a pending anchor through an authenticated endpoint after the Fabric network recovers.

PostgreSQL retains the request, returned Fabric transaction ID, commit status, receipt hash, attempt count, and failure diagnostic. The PWA and mobile evidence journeys display the actual anchoring state separately from the local Ed25519 signature verification.

## Identity and Privacy Controls

The Go Gateway adapter uses a Fabric X.509 client certificate, private key, MSP ID, TLS root certificate, Gateway endpoint, channel, chaincode, and optional named contract. These are mounted as protected files and configured through environment variables; private keys and raw evidence are never written to the public channel ledger.

The Gateway client uses TLS and the current Go Fabric Gateway API. Client applications submit or evaluate contract transactions through the peer Gateway, while Fabric obtains endorsements based on the chaincode and state-based policies.[1] State-based endorsement is applied per anchor key so the governance MSP set is part of the signed ledger state.[2]

## Deployment Preconditions

A production consortium must provide all of the following before `FABRIC_ANCHORING_REQUIRED=true` is enabled:

1. A channel with at least INEC and an independently governed observer/audit organization.
2. A committed `evidence-anchor` chaincode definition with a formally approved endorsement policy.
3. A Gateway-enabled peer and discovery service available through mutual TLS.
4. Protected Fabric client certificate, private key, TLS root certificate, MSP ID, Gateway endpoint, channel, chaincode, contract, and endorsing-organization configuration.
5. A one-time `InitializeGovernance` transaction that records the approved MSP set.
6. Operational ownership for peers, orderers, certificate authorities, key rotation, backup, incident response, and member onboarding.

The supplied Compose profile is intentionally inactive by default. It can validate application configuration and run a local development network, but it cannot create an election consortium or substitute generated development certificates for real participating institutions.

## References

[1] [Hyperledger Fabric Gateway documentation](https://hyperledger-fabric.readthedocs.io/en/latest/gateway.html)  
[2] [Hyperledger Fabric endorsement policies](https://hyperledger-fabric.readthedocs.io/en/latest/endorsement-policies.html)  
[3] [Fabric Gateway Go client API](https://pkg.go.dev/github.com/hyperledger/fabric-gateway/pkg/client)
