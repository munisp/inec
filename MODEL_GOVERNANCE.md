# Model Governance and Production Approval

## Purpose

The INEC platform treats every production machine-learning decision as an **approved evidence-bearing deployment artifact**, not as a default model or a development convenience. The anomaly inference engine loads `anomaly_xgboost.onnx` only when the sibling manifest `anomaly_xgboost.manifest.json` identifies the artifact, records its SHA-256 digest, identifies a validation report, and explicitly sets `approved_for_production` to `true`.

> A missing, malformed, mismatched, or unapproved manifest is a deployment-blocking condition. The inference engine reports a degraded health state with HTTP `503`, and the anomaly gateway and Go backend propagate that unavailable state instead of returning a synthetic anomaly score.

## Required Artifact Set

The production `MODELS_DIR` must contain the following files as an immutable release set. Do not copy a model into a running container without its reviewed manifest.

| File | Required purpose | Production rule |
|---|---|---|
| `anomaly_xgboost.onnx` | Executable ONNX model artifact | Must match the manifest SHA-256 exactly. |
| `anomaly_xgboost.manifest.json` | Approval and provenance record | Must pass the schema and contain a real validation-report URI. |
| Validation report referenced by `validation_report_uri` | Evidence of evaluation, scope, limitations, and approver decision | Must be retained in the approved document repository and remain accessible to the release approver. |
| Release change record | Deployment authorization | Must identify the artifact digest, manifest version, approver, target environment, and rollback version. |

The repository includes `ml/models/anomaly_xgboost.manifest.json.example` as a structural template only. It is **not** an approval record and must never be renamed or used as a production manifest unchanged.

## Manifest Contract

The current runtime contract requires the fields below. All values must refer to the actual deployed artifact and review evidence.

```json
{
  "model_id": "anomaly_xgboost",
  "version": "YYYY.MM.DD.release",
  "sha256": "<64-character lowercase SHA-256 of anomaly_xgboost.onnx>",
  "approved_for_production": true,
  "validation_report_uri": "https://approved-document-repository.example/model-validation/<release>"
}
```

| Field | Acceptance rule |
|---|---|
| `model_id` | Must be exactly `anomaly_xgboost`. |
| `version` | Must be non-empty and correspond to the approved release/change record. |
| `sha256` | Must equal the SHA-256 digest of the deployed ONNX file, using lowercase hexadecimal notation. |
| `approved_for_production` | Must be literal JSON boolean `true`; any other value blocks inference. |
| `validation_report_uri` | Must be non-empty and resolve through the organization’s authorized evidence-retention process. |

## Approval and Deployment Procedure

1. The model owner prepares the model artifact, validation report, training-data provenance, intended-use statement, measured performance, known limitations, and rollback candidate.
2. The authorized model-governance owner reviews the evidence and records an approval decision in the release change record.
3. The release engineer calculates the model digest on the deployment host:

   ```bash
   sha256sum /secure/models/anomaly_xgboost.onnx
   ```

4. The release engineer creates the manifest using that exact digest, the approved version, and the validation-report URI. The reviewer confirms the values before deployment.
5. The artifact and manifest are copied together into the protected directory mounted at `MODELS_DIR`. The directory must be readable by the inference service and not writable by the application user after deployment.
6. The release engineer starts or updates the Compose stack with `docker compose --env-file .env -f docker-compose.yml up -d` and confirms that `inference-engine` reaches `healthy` status.
7. The release engineer verifies the actual health payload through the internal network or authenticated gateway. It must report `status: "healthy"`, `models.anomaly_xgboost: true`, and `models.anomaly_governance.approved: true`.
8. The change record is closed only after gateway-level anomaly health, backend readiness, and the relevant browser-facing unavailable/available state have been checked.

## Rejection, Revocation, and Rollback

If validation evidence is incomplete, the digest differs, approval is withdrawn, or the model produces an operational incident, set `approved_for_production` to `false` in a reviewed replacement manifest or remove the artifact/manifest release pair. The platform will fail closed and no longer make anomaly decisions. Do not replace the model with a neutral score or a local heuristic.

To roll back, deploy the previously approved ONNX and matching manifest as a pair, verify its digest, then repeat the health verification procedure. The release record must identify both the revoked release and the restored release.

## Operational Alerts

The deployment owner must alert on each of the following conditions:

| Signal | Required response |
|---|---|
| `inference-engine` unhealthy | Treat anomaly scoring as unavailable; investigate model/manifest integrity before restart. |
| Hash mismatch or missing manifest in health reason | Stop promotion; restore the approved artifact pair or correct the controlled deployment package. |
| Anomaly gateway `503` | Verify inference-engine health, approved manifest, and network reachability; do not retry with fabricated scores. |
| Validation report removed or approval revoked | Remove the production approval and follow the rollback or suspension procedure. |

## Ownership

The platform source code enforces the runtime gate. The following inputs remain external operational responsibilities and cannot be certified from source control alone: real model artifact custody, validation evidence, authorized approval decisions, protected deployment storage, monitoring recipients, and Kasicloud host access controls.
