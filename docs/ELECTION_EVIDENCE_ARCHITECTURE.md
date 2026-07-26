# Election Evidence and Provenance Architecture

## Purpose

This document defines the integrated implementation for the election-integrity workflow. It turns each result from a mutable row into a verifiable lifecycle composed of evidence artifacts, policy versions, signed events, discrepancy cases, and collation evidence bundles.

The design is intentionally **fail closed in production**. An unavailable document-analysis engine, policy version, or required integrity signer cannot be represented as a successful validation or finalisation.

## Component responsibilities

| Component | Responsibility | Technology boundary |
|---|---|---|
| Go backend | Authoritative workflow, PostgreSQL persistence, role checks, event ordering, reconciliation cases, public-safe API responses | Go, PostgreSQL, existing TigerBeetle and audit integrations |
| Rust inference engine | Deterministic SHA-256 payload signing and signature verification using Ed25519; governed anomaly inference remains separate | Rust, `ed25519-dalek`, SHA-256 |
| Document AI service | PaddleOCR extraction, configured open-source VLM assessment, Docling table extraction, image-quality checks, cross-engine consensus, evidence manifest generation | Python, PaddleOCR, OpenCV, Docling, OpenAI-compatible self-hosted VLM endpoint |
| PWA | Results Evidence Journey, material-manifest visibility, manual-review and discrepancy status, voter-service navigation | TypeScript, React, existing PWA |
| Mobile | Compact evidence journey and result drill-down with accessible status, integrity checks, and official voter-service links | TypeScript, Expo React Native |

## PostgreSQL data model

| Table | Core purpose | Immutability / integrity rule |
|---|---|---|
| `election_policy_versions` | Versioned rules, legal/procedural references, and effective dates for an election | Approved policies cannot be edited; a new version supersedes the prior record |
| `election_material_manifests` | Signed, versioned party/candidate/form reference manifests | A manifest SHA-256 and policy version identify each material set |
| `evidence_artifacts` | EC8A/EC8B/EC8C files, structured analysis output, media metadata, and content hash | Artifact bytes are referenced by SHA-256; any replacement is a new artifact |
| `result_evidence_events` | Append-only lifecycle events for results | Sequence number, prior hash, event hash, optional Ed25519 signature, and public-safe payload |
| `reconciliation_cases` | Explicit mismatches and their accountable resolution path | Cases retain expected/observed values and never silently overwrite source evidence |
| `collation_evidence_bundles` | Evidence package for ward/LGA/state/national collation | Child-result manifest, aggregate hash, collation artifact, policy version, and publication state |
| `document_integrity_assessments` | Multi-engine PaddleOCR/VLM/Docling consensus outcome | Stores engine versions, manifest hash, review reasons, and decision state |

## Result lifecycle

1. A result submission validates EC8A arithmetic and creates `RESULT_SUBMITTED` evidence.
2. Document analysis produces an immutable evidence manifest and a conservative decision. Automation can request review; it cannot unilaterally declare an election result valid.
3. A `RECONCILIATION_CASE_OPENED` event blocks finalisation for critical discrepancies.
4. A collation officer may validate only if the policy, evidence, and open-case conditions are satisfied.
5. Finalisation requires an approved policy version, no unresolved blocking case, and—in production—an available Ed25519 integrity signer.
6. Every transition links to the preceding event hash, permitting public-safe verification without exposing protected data.

## Event canonicalisation and signing

The Go backend canonicalises an event into a deterministic UTF-8 JSON structure containing:

```text
result_id | sequence_no | event_type | prior_hash | policy_version_id | artifact_id | payload_sha256 | created_at
```

The Go backend hashes that canonical structure with SHA-256. The Rust service signs the hash with Ed25519 when `INTEGRITY_SIGNING_REQUIRED=true` or `APP_ENV=production`. The Go backend stores the signer key ID and signature. The public API exposes the event hash, prior hash, signature, and a redacted payload but never a private key, device fingerprint, or private observer data.

## Document-analysis consensus

The Document AI endpoint combines these open-source analysis layers:

| Layer | Contribution | Decision constraint |
|---|---|---|
| PaddleOCR | Structured EC8A text, totals, party rows, confidence, bounding regions | Low confidence or arithmetic mismatch requires review |
| Open-source VLM | Form type, orientation, completeness, tampering indicators, human-readable rationale | VLM output is advisory evidence and cannot finalise a result |
| Docling | Document structure and table extraction | Missing/contradictory table structure requires review |
| OpenCV + perceptual hash | Blur, dimensions, decode validity, pHash, content SHA-256 | Does not infer authenticity; produces technical evidence |
| Optional secondary open-source OCR endpoint | Ensemble comparison using a self-hosted GOT-OCR 2.0 or olmOCR-compatible service | An unavailable configured required engine fails analysis closed |

A result is returned as `manual_review_required`, `rejected_for_quality`, or `analysis_unavailable`; it is not automatically marked finalised by ML output.

## API surface

| Endpoint | Audience | Purpose |
|---|---|---|
| `GET /integrity/results/{id}/journey` | Authenticated public/observer/operator role, redacted by role | Result lifecycle, policy version, evidence artifacts, event chain, and reconciliation cases |
| `GET /integrity/results/{id}/verify` | Authenticated read role | Chain and signature verification outcome |
| `POST /integrity/results/{id}/cases` | Observer/admin/collation officer | Open a typed discrepancy case with evidence and reason codes |
| `POST /integrity/cases/{id}/resolve` | Admin/collation officer | Resolve or dismiss with a reason, evidence, and signed event |
| `GET /integrity/collation/{election_id}/{level}/{area_code}` | Read role | Signed/public-safe collation bundle and child-result manifest |
| `POST /integrity/material-manifests` | Admin only | Register an approved material manifest for an election policy |
| `GET /integrity/material-manifests` | Read role | View active and superseded reference manifests |
| `GET /integrity/voter-services` | Public read role | Official CVR service navigation metadata only; no voter-register duplication |
| `POST /analyze/photo-report` | Go backend | Returns document analysis plus immutable integrity manifest and conservative review state |
| `POST /integrity/sign`, `POST /integrity/verify` | Private Go-to-Rust service boundary | Sign and verify canonical evidence hashes; token protected |

## PWA and mobile experience

Both clients provide an **Evidence Journey** view that shows the current state first, then a chronological event trail, linked artifacts, policy version, open discrepancy cases, and collation bundle. The design uses status language such as “Under manual review,” “Evidence incomplete,” and “Published with no unresolved critical discrepancy,” rather than making unsupported claims of validity.

Mobile receives a compact, offline-friendly summary and a drill-down route. PWA exposes a fuller audit and material-manifest view. Both display official voter-service links from the backend rather than storing or duplicating voter personal data.

## Production controls

1. Production deployments require `INTEGRITY_SIGNING_REQUIRED=true`, `INTEGRITY_SIGNING_KEY`, `INTEGRITY_SIGNER_KEY_ID`, and `INTEGRITY_SERVICE_TOKEN`.
2. Document analysis requires configured PaddleOCR, VLM, and Docling engines. A configured secondary OCR engine is mandatory when `SECONDARY_OCR_REQUIRED=true`.
3. All new artifacts use content-addressed SHA-256 hashes, restrictive object storage access, encrypted transport, and privacy-safe public responses.
4. The system persists model/engine version strings with each assessment and records policy version with every lifecycle event.
5. Event-chain and signature verification must be monitored by the platform health and audit endpoints.
