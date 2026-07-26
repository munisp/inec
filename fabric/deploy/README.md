# Evidence-Anchor Fabric Deployment

This directory deploys the **`evidence-anchor`** smart contract that records signed, deterministic commitments to the platform’s off-chain result-evidence ledger. It is deliberately not a generated single-operator blockchain network. A credible election consortium must provide real institutional membership, identities, orderers, peers, and endorsement governance before the platform enables required anchoring.

## Data Boundary

The public channel receives only an `EvidenceAnchorV1` record with signed SHA-256 commitments, policy references, and timestamps. The PostgreSQL evidence system retains raw EC8A documents, private payloads, voter information, document-analysis details, and reconciliation records. This boundary makes independent verification possible without publishing sensitive material to every channel member.

| Component | Holds | Does not hold |
|---|---|---|
| PostgreSQL evidence ledger | Raw artifacts, encrypted/private operational context, result workflow, reconciliation cases | Fabric peer ledger state |
| Rust integrity signer | Ed25519 signing and verification material | Fabric client MSP private key |
| Fabric `evidence-anchor` chaincode | Signed hash anchors, Fabric transaction identity, non-sensitive policy reference | EC8A image bytes, voter records, document OCR/VLM text, private case details |
| PWA and mobile clients | Public-safe status, hashes, transaction IDs, receipt hashes | Fabric client keys or private evidence payloads |

## Consortium Preconditions

The system must not enable `FABRIC_ANCHORING_REQUIRED=true` until all conditions below are satisfied.

1. INEC and at least one independently governed observer or audit organization have approved channel membership, peer operation, certificate authority ownership, incident response, and key-rotation procedures.
2. A Fabric v2.4-or-later Gateway-enabled peer and discovery service are reachable through mutual TLS.
3. Each participating organization has approved the same `evidence-anchor` chaincode definition and the channel operator has committed it.
4. The chaincode endorsement policy is formally approved. The repository baseline uses `AND('INECMSP.peer','ObserverMSP.peer')`; changing membership or policy requires a chaincode lifecycle update and recorded governance approval.
5. The `InitializeGovernance` transaction has stored the agreed MSP list. It cannot be silently changed by the application.
6. The INEC backend has a protected Fabric client certificate, private key, TLS root certificate, valid MSP ID, peer Gateway endpoint, channel, chaincode, contract, and endorsing-MSP configuration.

## Deploying the Chaincode

Run these commands only on a host that already has the official Fabric CLI, the invoking organization’s MSP configuration, and access to the pre-provisioned consortium network.

```bash
cd /path/to/inec-campaign-audit
source fabric/deploy/evidence-anchor-definition.env

# Each organization performs its own approval with its own identity.
./fabric/deploy/deploy-evidence-anchor.sh package
./fabric/deploy/deploy-evidence-anchor.sh approve

# An authorized channel operator commits after sufficient approvals.
./fabric/deploy/deploy-evidence-anchor.sh commit

# Perform once, with an approved JSON MSP list.
export FABRIC_CONSORTIUM_MSPS_JSON='["INECMSP","ObserverMSP"]'
./fabric/deploy/deploy-evidence-anchor.sh initialize-governance
```

The script validates required variables and refuses to create identities, use sample peer addresses, or substitute a local database for Fabric. It packages the Go chaincode from `fabric/chaincode/evidence-anchor` and invokes the actual `peer lifecycle` commands.

## Attaching the Application

Create a protected directory, for example `/srv/inec/fabric-client`, containing only the client identity files below. The directory must be owned by the deployment account and have restrictive permissions.

| Filename in mounted directory | Purpose |
|---|---|
| `tls-root-cert.pem` | Root/issuer certificate that validates the targeted Gateway peer TLS certificate |
| `client-cert.pem` | INEC backend Fabric X.509 enrollment certificate |
| `client-key.pem` | Corresponding Fabric X.509 private key; never commit or expose to clients |

Set the Fabric variables in the protected `.env` file, including `FABRIC_SECRETS_DIR` and `FABRIC_DOCKER_NETWORK`. Then start the platform with the external network overlay.

```bash
docker compose --env-file .env \
  -f docker-compose.yml \
  -f docker-compose.fabric.yml \
  up -d
```

The overlay does not start Fabric peers, orderers, or certificate authorities. It attaches the backend to an existing external consortium network, mounts the identity files read-only, and blocks backend startup if the supplied Gateway endpoint or identity files are unavailable.

## Operational Status and Recovery

The backend writes an immutable local Fabric-anchor request for every signed evidence event whenever anchoring is enabled. The worker submits pending anchors via the official Gateway API, records the returned transaction and receipt hash, and exposes their real status through these routes.

| Route | Audience | Purpose |
|---|---|---|
| `GET /integrity/fabric/health` | Authorized operations roles | Gateway configuration, governance availability, pending and failed anchor counts |
| `POST /integrity/fabric/anchors/{id}/retry` | Admin or collation officer | Explicit retry for a failed or unavailable submitted anchor |
| `GET /integrity/results/{id}/journey` | Authorized user | Public-safe local evidence verification and Fabric anchor receipt state |

An anchor status of `committed` is only recorded after the Gateway returns a matching Fabric receipt. `pending`, `failed`, and `unavailable` are shown explicitly in the PWA and mobile clients. In a production configuration with `FABRIC_ANCHORING_REQUIRED=true`, a missing gateway identity or unavailable Fabric configuration blocks new integrity-controlled transitions instead of claiming consortium confirmation.

## References

[1] [Hyperledger Fabric Gateway](https://hyperledger-fabric.readthedocs.io/en/latest/gateway.html)  
[2] [Hyperledger Fabric endorsement policies](https://hyperledger-fabric.readthedocs.io/en/latest/endorsement-policies.html)  
[3] [Fabric Gateway Go client API](https://pkg.go.dev/github.com/hyperledger/fabric-gateway/pkg/client)
