# Election Evidence Operations Runbook

## Operating objective

The election-evidence workflow is designed to **fail closed**. A result cannot be finalised in production unless its required policy, immutable lifecycle evidence, blocking reconciliation state, and Ed25519 signer are available. Document-analysis output is evidence for accountable review; it never changes a result to finalised status by itself.

## Required production controls

| Control | Deployment requirement | Health signal | Failure behaviour |
|---|---|---|---|
| Ed25519 evidence signer | `INTEGRITY_SIGNING_REQUIRED=true`, a unique base64 32-byte `INTEGRITY_SIGNING_KEY`, `INTEGRITY_SIGNER_KEY_ID`, and `INTEGRITY_SERVICE_TOKEN` | `GET /integrity/health` reports `signer.status=healthy` and `signing_ready=true` | Validation/finalisation fails; health is degraded |
| Policy provenance | An approved policy version is created per election before governed transitions | Result journey displays `policy_version_id` | Governed transitions are rejected |
| Evidence storage | EC8A/EC8B/EC8C, analysis manifests, and bundles use SHA-256 content addresses and private object-storage paths | Result journey lists artifact hashes, not private bytes | Artifact must be re-uploaded as a new object; mutation is rejected |
| Document analysis | PaddleOCR, Docling, self-hosted VLM, and configured secondary OCR settings are supplied | Document AI health exposes engine state and analysis manifest | Analysis returns unavailable/review-required; no automatic approval |
| Reconciliation | Critical discrepancy cases require a reason, evidence, and authorised resolution | Integrity health reports open blocking cases | Finalisation is blocked |
| Official voter services | `INEC_CVR_URL` is a validated HTTPS INEC endpoint | `GET /integrity/voter-services` | Clients show unavailable navigation; no local registration is attempted |

## Key management and rotation

The Rust signer accepts a 32-byte Ed25519 private key encoded with standard base64. The value must be held only in the deployment secret manager and injected into the `inference-engine` container. It must never be included in a model manifest, browser bundle, database row, audit payload, or application log.

To rotate a key, provision a new signer key and key ID in the secret manager, schedule a controlled deployment, and verify `POST /integrity/health` from the Go backend’s private network path. Existing event signatures remain associated with their stored signer key ID. Do not rewrite old events; publish a new event or bundle version when a new signed assertion is required.

## Incident response

A failed signer, unknown policy, broken event chain, or critical reconciliation case is an integrity incident. The response owner should preserve the affected evidence hashes, inspect the public-safe result journey, and open or update a reconciliation case with an evidence artifact. The responsible collation officer or administrator must resolve or dismiss the case with a reason and resolution evidence. Direct database edits are prohibited by migration `000020_evidence_immutability_controls.sql`.

## Deployment verification

Before releasing an election workflow, operators must apply migrations through the approved PostgreSQL migration runner, configure every value in `.env.production.example`, and validate Compose with the protected production environment. Then confirm the following endpoints with authorised accounts:

| Endpoint | Expected condition |
|---|---|
| `GET /integrity/health` | `200`, signer healthy, no signer-unavailable events |
| `GET /integrity/voter-services` | HTTPS official-service metadata and no voter-record payload |
| `GET /integrity/results/{id}/verify` | Contiguous event chain; valid signature if signing is required |
| `GET /integrity/results/{id}/journey` | Public-safe event/artifact/case data with policy version |
| Document AI `/health` | Required engine configuration visible and healthy |

## Retention and privacy

Store private observer documents and unredacted analysis only in restricted storage controlled by retention policy and applicable Nigerian electoral and data-protection requirements. Public APIs expose hashes, state, redacted payloads, and approved material-manifest metadata. They must not expose private keys, personal voter records, image bytes, biometric templates, device fingerprints, or private observer narrative.
