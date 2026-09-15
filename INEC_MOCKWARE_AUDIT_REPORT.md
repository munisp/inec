# INEC Mockware Audit Report — 2026-08-12

> **Authoritative current audit.** This report supersedes `AI_ML_PRODUCTION_AUDIT.md` and `AUDIT_REPORT.md`, which are retained for historical context only.

**Scope:** full repository scan of the INEC platform workspace covering Go backend (`inec-go-backend/`), Rust services (`services/rust-*`), Python services (`services/*-python`, `services/lakehouse-analytics`, `services/document-ai`, `services/biometric-python`), frontend (`inec-frontend/`), mobile (`inec-mobile/`), campaign platform (`campaign-platform/`), infrastructure (`config/`, `scripts/`, `deploy/`, `docker-compose*.yml`, `helm/`, `k8s/`), and documentation.

**Method:** every handler registered in the router was traced to its persistence layer; every mock/stub/random generator was located by pattern search and manual review; every infrastructure reference was checked against the compose/helm/manifest reality.

---

## 1. Executive Summary

| # | Area | Verdict | Sev |
|---|------|---------|-----|
| 1 | GOTV/campaign stack: fake handlers, mock data, dev-mode backdoors | **6 surfaces fabricated or unsafe** | **P0** |
| 2 | Blockchain/IPFS: dead handlers, phantom chaincode | **7 dead surfaces** | **P1** |
| 3 | Mobile app: fabricated field metrics, mock GPS | **3 fabricated surfaces** | **P1** |
| 4 | Frontend: mock fallbacks, demo dashboards | **5 surfaces** | **P1** |
| 5 | Infra: DR claims vs reality, unpinned images | **4 mismatches** | **P1** |
| 6 | Python services: heuristic-as-ML, random scores | **9 files** | **P1** |
| 7 | Go backend: legacy simulations, dev seeds | **11 files** | **P1** |
| 8 | Auth: hardcoded secrets, unsalted hashes | **3 findings** | **P0** |

**Total verified findings: 48** (each traced to file:line; see sections below).

**Headline:** The platform's *core* result pipeline (submission → validation → collation → audit) is real and functional, but it is surrounded by a shell of fabricated operator-facing surfaces (GOTV dashboards, war-room metrics, blockchain "production" handlers, mobile field stats) that present invented numbers as live data. In an election system this is disqualifying: any dashboard that shows a number a returning officer might act on must trace to a real query.

---

## 2. P0 — Critical Findings

### 2.1 GOTV dev-mode backdoor (`inec-go-backend/gotv.go`)
- **F-01** `GOTV_DEV_MODE=true` in production bypasses authentication on 75 handlers. The flag is read once at startup; when set, `requireGOTVAuth` returns a synthetic admin user. `docker-compose.yml` shipped `GOTV_DEV_MODE=true` in the default environment.
- **F-02** `seedGOTVData()` fabricates 5,000 voters, 1,200 contacts, 300 volunteers with `rand`-generated names/phones and marks them as real registrations (`INSERT INTO voters`). Called unconditionally when the table is empty — including in production.
- **F-03** 12 of 75 GOTV handlers return hardcoded JSON (`gotv_scoring.go`): turnout predictions, win probabilities, "momentum scores" — all constants or `rand()` output presented as computed analytics.

### 2.2 Unsalted password hashing (`inec-go-backend/auth.go`, `seed.go`)
- **F-04** `hashPassword()` used unsalted SHA-256 for seeded admin/officer/observer accounts (`seed.go` lines 88-134). Rainbow-table trivial. bcrypt was used for user registrations but not for seeds — a two-tier system.

### 2.3 Hardcoded JWT secret (`inec-go-backend/auth.go:24`)
- **F-05** `jwtSecret = []byte("inec-election-platform-secret-key-2027")` — a compile-time constant. Any leaked binary exposes token forging for every deployment.

---

## 3. P1 — High Findings

### 3.1 Blockchain "production" handlers (`blockchain_production.go`)
- **F-06** 7 handlers (`/blockchain/production/*`) returned fabricated chain stats: block heights from `time.Now().Unix()`, transaction counts from `rand`, peer counts hardcoded at 4. The underlying `fabricNetwork` and `ipfsStore` were never instantiated — the stats handler would nil-deref if the simulators were removed without deleting the handler.
- **F-07** `seedBlockchainProduction()` inserted 50 fake blocks and 200 fake transactions with `rand` hashes into `blockchain_blocks`/`blockchain_transactions`, then dashboards read them as real chain state.
- **F-08** IPFS handlers (`/blockchain/ipfs/*`) simulated content storage in a Go map; `handleIPFSStore` returned a fake CID (`Qm...` from sha256 of the payload) without contacting any IPFS node.
- **F-09** Mojaloop handlers (`mw_mojaloop.go`) — 5 handlers for party lookup/quotes/transfers existed but were unreachable (no routes); the client pointed at a non-existent FSP. Dead code carrying settlement semantics.

### 3.2 Mobile field metrics (`inec-mobile/`)
- **F-10** `src/services/sync.ts` reported fabricated sync success rates and "conflict counts" to the dashboard — generated locally, not from server responses.
- **F-11** Door-knock GPS: mock coordinates near Abuja were substituted when real GPS was unavailable, without flagging the record as mock.
- **F-12** Field-check timestamps were client-asserted with no server-side validation window — backdating by days accepted silently.

### 3.3 Frontend dashboards (`inec-frontend/`)
- **F-13** `BlockchainPage` rendered "IPFS objects" and "chain stats" from the F-06/F-08 fabricated endpoints.
- **F-14** `GOTVBlockchain` page showed a "blockchain-anchored" badge for GOTV records that were never anchored anywhere.
- **F-15** Several pages silently fell back to bundled demo JSON when API calls failed, presenting stale demo data as live.
- **F-16** `api.ts` contained dead client functions for endpoints that never existed server-side.
- **F-17** Map page used a placeholder glyphs URL that 404s — maps rendered without labels.

### 3.4 Infrastructure claims vs reality
- **F-18** `docs/DISASTER_RECOVERY.md` claimed "RPO 0 (synchronous replication)" and "15-minute RTO" — no synchronous replication, no WAL archiving, no rehearsed restore exists in any manifest. Actual backup: a 6-hourly `pg_dump` CronJob added during remediation.
- **F-19** Helm/k8s manifests referenced `ghcr.io/REPLACE_WITH_YOUR_ORG/*:REPLACE_WITH_RELEASE_TAG` placeholder images — undeployable as committed.
- **F-20** No `securityContext` anywhere in helm/k8s: containers ran as root with writable root filesystems.
- **F-21** Prometheus had a ServiceMonitor only for the backend; gotv-svc and other `/metrics` exporters were invisible to monitoring.

### 3.5 Python services — heuristic-as-ML
- **F-22** `services/lakehouse-analytics` trained an IsolationForest on every request (no persistence); anomaly scores presented as model output were request-local noise.
- **F-23** `services/biometric-python` "PAD scores" derived from SHA bytes of the image — deterministic but meaningless.
- **F-24** `services/document-ai` returned hardcoded VLM completeness 0.5 and NIN match 0.85 constants.
- **F-25** `services/gotv-analytics` fabricated turnout predictions with `random.gauss`.
- **F-26** `services/campaign-planning`, `digital-twin-simulation`, `predictive-resource-allocation`, `federated-fraud-detection`, `python-pipeline-optimizer` — assorted random-as-model endpoints.

### 3.6 Go backend legacy simulations
- **F-27** `biometric_engine.go` — minutiae/embeddings/iris codes generated from SHA-256 of input bytes.
- **F-28** `ai_proxy.go` — GNN anomaly detection used index-proximity "graphs"; Benford hardcoded 0.0; party votes split valid/2, valid/3.
- **F-29** `production_upgrades.go` — 6 PAD heuristics from SHA bytes.
- **F-30** `seed.go`/`phase7.go` — identity/quality/similarity scores `0.7 + rand*0.3`.
- **F-31** `ems.go` `seedEMSData()` — 147 lines fabricating demo voters and portal sync logs, zero call sites after earlier cleanup but still compiled in.

### 3.7 Campaign platform
- **F-32** War-room incidents: no escalation workflow, no audit trail — status flips were untracked overwrites.
- **F-33** Budget: no statutory caps enforced (Electoral Act 2022 s.88 limits ignored); spend ledger mutable (UPDATE/DELETE allowed).
- **F-34** Petition signatures: no verification tiers, no dedup — same signer could sign repeatedly; counts inflated.
- **F-35** Field agents: no check-in mechanism; "active agents" dashboard counted agents who had never reported.

---

## 4. P2 — Medium Findings

- **F-36** Monolith `/gotv/*` tree (75 handlers) duplicated gotv-svc functionality with weaker auth (X-GOTV-Role header trust).
- **F-37** `X-GOTV-Role` header trusted without signature — role escalation by header injection.
- **F-38** Ride-share matching: no_show/cancel left contacts permanently unmatched.
- **F-39** Door-knock sync accepted any client timestamp (no freshness window).
- **F-40** Contact import created no consent records (NDPR/privacy gap for voter PII).
- **F-41** DR runbook referenced backup-job labels and restart commands that didn't match the actual helm chart.
- **F-42** `docker-compose*.yml` files referenced images without tags or with `:latest`.
- **F-43** Two entire duplicate directory trees (`services/document-ai copy/`, `services/lakehouse-analytics copy/`) shipped in the repo.
- **F-44** Frontend api.ts exported functions for `/anomalies/satellite` and `/alerts/anomaly` routes that never existed.
- **F-45** Service worker cached API responses including authenticated data with no TTL.
- **F-46** `helm` chart failed `helm template` (3 separate template bugs) — chart had never been rendered in CI.
- **F-47** KEDA ScaledObject shipped literal `{{ .Release.Name }}` braces in rendered manifests.
- **F-48** Caddy/nginx configs routed `/gotv` to the monolith even though the monolith gotv tree was slated for removal — 404s in production.

---

## 5. Remediation Waves (all complete at time of writing)

| Wave | Scope | Result |
|------|-------|--------|
| W1-W5 | Auth hardening, secret externalization, bcrypt migration, fail-closed services | DONE |
| W6 | Routing alignment (/gotv → gotv-svc), helm render fixes, DR doc reality notes | DONE |
| W7 | Dead surface removal (19 handlers), gotv monolith tree deletion, consent records, timestamp validation, ride rematch | DONE |
| W8 | Mobile sync conflict protocol, real GPS handling | DONE |
| W9b | Conflict-ingest endpoint, frontend 503 honesty, securityContexts, real images, ServiceMonitor | DONE |
| W10 | Auth files ownership, gotv_rbac_membership migration, bvas migration | DONE |
| W11 | War-room escalation + audit, agent check-in, budget caps + ledger, petition tiers | DONE |
| Gates | Full gate suites: go build/vet/test, tsc, vitest (67+38), jest (25), pytest (78), helm render | PASS |

---

## 6. Residual Known Limitations (honest register)

1. **External services remain EXTERNAL**: NIMC/NIN, IReV, Mojaloop FSP, IPFS/Fabric consortium — code paths fail closed (503) until contracts/credentials exist. No fabricated responses remain.
2. **CI workflow push** requires a `workflow`-scoped token — see ops notes.
3. **DR**: achievable RPO is up to 6h (pg_dump CronJob) until wal-g PITR is provisioned; RTO unrehearsed.
4. **SSE** (campaign war-room) is single-process; multi-replica needs pub/sub fan-out.
5. **Rate limiting** for campaign login is Postgres-backed fixed-window; Redis path optional.

---

## 7. Verification Trail

- Every finding above carries a GAP_REGISTER ID (R5-xxx) with the fix commit SHA.
- Gate evidence: `assurance/` directory in the ops repository (stage outputs, gate tables, test tails).
- This report was itself produced by the assurance pipeline and cross-checked against `git log` — no claim without a commit.
