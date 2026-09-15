# INEC Platform — Silent Mockware Audit & Remediation Report

**Repository:** github.com/munisp/inec @ `1d80c54` (main)
**Audit date:** 2026-08-12
**Scope:** Round 1 — full monorepo mockware sweep (Go backend, Python/ML services, Rust services, Fabric chaincode, React/RN frontends). Round 2 — dedicated deep audit of the campaign manager: `campaign-platform/` (all pages, tRPC routers, authz/roles, party scoping, LLM usage, finance flows), `inec-frontend` GOTV/party features, `inec-mobile`, `cmd/gotv-svc`, and campaign-adjacent services (`gotv-analytics`, `campaign-planning`, `gotv-engine`, `geolibre-collab`).
**Deliverable:** `inec-mockware-fixes.patch` — cumulative, both rounds: **144 files changed, +7,562 / −9,251**. Verified to apply cleanly onto `1d80c54` with `git apply --check`.

---

## 1. Executive Summary

The platform's most dangerous production-readiness gap was not missing features — it was **silent mockware**: code that emits plausible-looking, fabricated results indistinguishable from real ones. Four parallel domain sweeps (Go, Python, Rust/chaincode, TypeScript) confirmed **70+ instances**, clustered in eleven critical groups. **All confirmed instances have been fixed** using one rule: *real logic where cheap, loud failure (400/403/501/503 + no persistence) everywhere else — silence is the bug.*

The three most dangerous findings:

1. **The "E2E-verifiable" election cryptography was theatrical — in two independent codebases.** Both the Go primaries service (`handlers_primaries.go`) and the Rust gotv-engine (`voting_crypto.rs`) implemented "ElGamal encryption" as SHA-256 hashing, "Chaum-Pedersen proofs" that verify `true` for anything, "homomorphic tallies" that ignore the votes, "mix-net re-encryption" that destroys ballots, and "threshold decryption" that echoes a caller-supplied number back with a hash "proof". The Go audit endpoint returned a hardcoded `"integrity": "verified"`. These endpoints *manufactured cryptographic evidence for elections*.
2. **Biometric verification matched voters against themselves.** `handleABISVerify` fetched the "probe" from the same vault record it compared against — every enrolled voter scored a perfect match. PAD/liveness fell back to hash-derived scores biased into 0.7–1.0 ("live", ISO-stamped), KYC "verified" people by file format, and BVAS accreditation stored client-asserted `biometric_match` flags.
3. **The ML model supply chain is synthetic.** Every production ONNX/PT artifact (anomaly XGBoost, CDCN liveness, ArcFace, GNN fraud) traces to noise/synthetic training data, with metadata fabricating ISO-30107 Level-2 compliance — while dashboards present verdicts as "trained model" output.

The repo's own `AI_ML_PRODUCTION_AUDIT.md` claims "100/100 — all gaps closed". **That claim is itself not accurate** as of `1d80c54`; this report's file:line findings are the ground truth.

**Round 2 (campaign manager):** a dedicated audit of the campaign platform found its most dangerous gap was not mockware but **missing tenancy**: any authenticated user could read, edit, or delete every candidate's campaign data, and several router→schema field mismatches were silently swallowed by drizzle's unknown-key dropping. All findings fixed — see §6.

---

## 2. What Was Fixed (by cluster)

### 2.1 Fake election cryptography — Go (`fix/primaries-crypto`, commit 3612bf4)
`inec-go-backend/cmd/gotv-svc/handlers_primaries.go` (+189/−261)
- Deleted: rand-blob key generation (+ unencrypted private-key storage), fake Shamir shares, SHA-256 `encryptBallot`, HMAC proofs with hardcoded `"election-proof-key"`, hash "homomorphic" tally, SHA-256 mix-net, count-only threshold decrypt.
- All `/gotv/primaries/crypto/*` endpoints now return **503** with explicit "service not configured — refusing to fabricate cryptographic artifacts" until a real backend is set via `GOTV_ELECTION_CRYPTO_BACKEND_URL`. Ballot casting hard-fails 503 rather than storing hash-"encrypted" ballots.
- Audit trail now returns real DB counts with `integrity:"unknown"` + a warning that pre-existing artifacts were fabricated and **must not be trusted**.
- `handleRemoteAuthenticate`: `biometric_verified` now requires an explicit `{"verified":true}` from the biometric pipeline (`GOTV_BIOMETRIC_SERVICE_URL`); otherwise 503. Payload-non-emptiness no longer counts as verification.

### 2.2 Fake election cryptography — Rust (`fix/rust-services`, commit 9b03ca0)
`services/gotv-engine/src/voting_crypto.rs` — every operation returns `Err("…refusing to fabricate cryptographic artifacts")`; HTTP layer maps to 503. The `panic!("Not enough shares")` API-driven DoS removed. **24/24 unit tests pass** (rewritten to assert loud failure). Service auth now fails closed: unset `GOTV_ENGINE_API_KEY` ⇒ 503 on all non-health routes; `dapr-api-token` must match a configured value.

### 2.3 Simulated hot-path pipeline — Rust (same commit)
`services/rust-hot-path/` previously **did not compile** (`conn` undefined) and simulated everything: no Kafka consumption, TigerBeetle no-op `Ok(())` while "transfers_submitted" incremented, OpenSearch indexing commented out, **dedup always answered "not a duplicate"**, tally echoed inputs, unique counts hardcoded 0. Now: real `ClusterClient` Redis (BF.ADD/ZADD/PFADD/Lua tally/pipelining), real OpenSearch `/_bulk` POST, real rdkafka consumer behind `feature="kafka"` — and without it the service **refuses to start** rather than simulate. TigerBeetle/Fluvio return `Err` until real clients are added. `cargo check` passes.

### 2.4 Biometric / KYC / BVAS verification (`fix/biometric-kyc`, commit d0b1a65 + cdb2d7e)
- `handleABISVerify` / `handleMultiModalVerify`: require real `probe_data`/`probes` (400 otherwise); self-match removed; PAD unavailable ⇒ 503 before persistence.
- PAD fallback deriving "live" scores from SHA-256 bytes: **deleted** — fails closed, never stamps ISO compliance.
- `phase7.go` hash-similarity fallback and probe auto-enrollment (quality 0.8): **deleted**; ML service mandatory (503).
- KYC: Document-AI failure ⇒ 503 + `pending_review`, never `verified`; hardcoded `faceMatchScore 0.5` removed. Liveness: any-video->10KB pass **deleted** — fails closed.
- BVAS accreditation: requires a `verification_id` referencing a server-side `match` row (403/503); client-asserted flags ignored.
- Fabricated 10-finger enrollment (SHA-256(vin+finger) templates), kiosk 8-step auto-complete, no-op `SecureMatch` with Paillier/ZK/ISO-24745 claims, fake NIST benchmarks (`nist_compliant:true`), fake mapreduce dedup jobs, startup-seeded PAD accuracies (0.987/0.973/0.991), benign-default integrity scores for data-less PUs: **all removed or fail loudly** (`insufficient_data`/`not_implemented`/503).
- Invented FAR/FRR heuristics removed from Go ABIS (`MatchResult.far/frr` now omitted) and both Python engines (`far`/`frr` now `null`).
- Dead fabricators deleted: `seedPhase7Data`, `seedBiometricEngine`, `seedBiometricAdvanced`, `ABISEngine.Enroll`.
**`go build ./...` + `go vet` pass.**

### 2.5 GOTV analytics, evidence, events (`fix/go-misc`, commit 813736a)
- Predictive turnout: `rand`-generated show-rates/CIs replaced by deterministic constants + normal-approximation CI from real sample size; model relabeled `pledge-based-v2`.
- Field-report media: bytes now actually persisted (`GOTV_MEDIA_DIR`, 10MB cap); storage failure ⇒ 503, no fake `media_url`.
- `publishEvent`: audit-topic failures now return errors + ERROR logs (was silent debug drop).
- AI anomalies: DB error ⇒ 500 (was 200 "no anomalies"); hardcoded features removed, `features:"partial"` declared.
- DND check fails **closed**; BVAS firmware validated against `BVAS_FIRMWARE_ALLOWLIST` (empty ⇒ reject).
- USSD/IVR hardcoded "PU-001-Lagos-Island" lookups replaced with real voter-registry queries + incident persistence; honest "No registration found".
- NDPR data-subject handlers rewritten: real access export + transactional erasure with audit rows — no more bare `{"status":"success"}`.
- Deleted dead fabricators: `startRealtimeTicker` (random "live" broadcasts), seed endpoints in `geospatial_enhanced.go`.
**`go build ./...` passes (backend + biometric-go).**

### 2.6 ML supply chain (`fix/python-ml`, commit c7d7aed)
- All trainers (anomaly XGBoost, CDCN, liveness, ArcFace, GNN, GOTV stack) **exit non-zero** without a real `--data` path; synthetic mode requires explicit `--allow-synthetic` and stamps `SYNTHETIC_NOT_FOR_PRODUCTION`.
- Fabricated metadata stripped: ISO-30107 Level 2, `attack_types_detected`, ROC-AUC claims. Checked-in `ml/models/*.json` marked synthetic; registry's synthetic "production" entries **demoted to staged**.
- Continuous training: all 17 features computed from real rows (no more `valid//2`, `0.55` constants); auto-promotion requires `trained_on=="real"` + ≥1000 samples + AUC threshold; empty ingestion skips the cycle.
- Homomorphic tally: real Miller-Rabin (40 rounds, `secrets`) + 2048-bit default; `TALLY_DECRYPT_TOKEN` has **no default** (503 unset); timing-safe compare.
- Runtime stubs fixed: federated predict 503 at round 0 (was constant 0.5), real PU codes in anomaly log (was `PU-{index}`), ML-vs-heuristic labeling in targeting, hardcoded DocLing confidence 0.85 → derived/None, `train_pad_model.py` refuses unimplemented datasets, `simulate_fl_round` deleted.
- **Deleted `services/document-ai copy/`** — stale duplicate whose sanctions screening passed everyone and face-compare fabricated scores.
**18 files py_compile clean + runtime smoke tests passed (Paillier round-trip, 503 flows, feature computation).**

### 2.7 Frontend silent fallbacks (`fix/frontend`, commit c5b66b9)
- `GOTVBlockchain.tsx`: deleted catch-seed that rendered a fabricated **"Chain Valid"** badge (24 fake blocks, `chain_integrity:true`) on any API failure → now `AuthoritativeDataUnavailable`.
- `GOTVLedger.tsx`: deleted seeded fake TigerBeetle accounts/transfers + `{balanced:true, variance:0}` → unavailable state; no "Balanced" badge without a real reconcile response.
- Deleted `lib/demo-data.ts` (302 lines of fabricated election fabric) — confirmed unreferenced.
- `nigeria-geo.ts`: fake random-hexagon state boundaries replaced with honest center-point markers; MapPage choropleth removed + visible note "boundary geometry not available in this build".
- Campaign platform: "Live Sentiment Tracker" (pseudo-random, endpoint doesn't exist, fabricated n=800-1200) → explicit "no live source connected" state; rival-candidate comparison labeled "Hypothetical scenario"; stakeholder reach numbers marked editorial heuristics; LGA "leaders identified" → "roles to identify" checklist.
- Mobile `biometrics.tsx`: "Run Verification" no longer posts hardcoded `VIN-DEMO-001`; requires a VIN; demo gated behind `__DEV__` with visible DEMO badge.

### 2.8 Fabric chaincode (`fix/fabric-anchor`, commit 7f73223)
`validateAnchor` accepted any base64 blob as a "signed" evidence commitment. Now: invoker X.509 cert via CID, `signer_key_id` bound to cert CN/ID/fingerprint, ECDSA/RSA signature verified over the canonical commitment (which binds `payload_sha256`). Unverifiable ⇒ rejected. **`go test` passes**, including the original exploit case.

### 2.9 Inference safety
`inference-engine-v2`: ONNX/tensor failure returned `0.0` → silently cleared anomalies. Now `Result<f64>` + HTTP 503. `biometric-rust` vault: master key from `BIOMETRIC_MASTER_KEY`; random keys only in dev envs (loud warning), else startup refused.

---

## 3. Verification

| Surface | Result |
|---|---|
| `inec-go-backend` `go build ./...` + `go vet` (go 1.26) | **PASS** |
| `services/biometric-go` `go build` | **PASS** |
| `fabric/chaincode/evidence-anchor` `go build/vet/test` | **PASS** (exploit-case tests) |
| `gotv-engine` `cargo check` + tests | **PASS** (24/24) |
| `rust-hot-path` `cargo check` | **PASS** (previously didn't compile) |
| `inference-engine-v2`, `biometric-vault` `cargo check` | **PASS** |
| 18 changed Python files `py_compile` + smoke tests | **PASS** |
| 11 changed TS/TSX files parser checks | **PASS** |
| Final residual grep (rand fabrication, hardcoded "verified", proof keys, payload-nonempty checks, deleted files) | **CLEAN** (remaining `rand` = E2E-fixture-gated seed code only) |

*Caveat:* `rust-hot-path --features kafka` not compile-verified (cmake unavailable); full Go test suites and TS cross-module type-checks should run in CI.

## 4. Intended breaking changes (ops must act)

| Change | Required action |
|---|---|
| Primaries ballot casting + all `/crypto/*` ⇒ 503 | Set `GOTV_ELECTION_CRYPTO_BACKEND_URL` to a real ElectionGuard/Paillier backend before any primary |
| Remote voting auth ⇒ 503 | Set `GOTV_BIOMETRIC_SERVICE_URL` |
| KYC/liveness/biometric verify ⇒ 503 on service outage | Ensure Document-AI + biometric pipeline are deployed; treat 503 as "down", not "failed verification" |
| BVAS accreditation needs `verification_id` | Update BVAS client flow to verify first, accredit second |
| Tally decryption ⇒ 503 | Set `TALLY_DECRYPT_TOKEN`; regenerate Paillier keys (old keys likely composite) |
| Model training refuses synthetic-by-default | Provide real `--data`; **retrain and replace all checked-in ONNX/PT artifacts** — current ones are noise-trained |
| ML cycles skip without real rows | Extend `ingest_from_db` to select party-vote columns |
| Media uploads need storage | Set `GOTV_MEDIA_DIR` (default `/data/media`) |
| DND/firmware fail closed | Set `BVAS_FIRMWARE_ALLOWLIST`, DND registry endpoint |
| **All previously stored crypto proofs, PAD results, KYC "verified" rows, benchmark records** | **Treat as untrustworthy; audit and purge** — they were fabricated |

## 5. Remaining production-readiness gaps (beyond mockware)

Not fixed here (out of scope, but blocking for production):
1. **Middleware is simulated by default**: TigerBeetle/Redis/Kafka/APISIX/Keycloak/Permify/Fluvio/Dapr fall back to in-memory "embedded" modes; Mojaloop/OpenAppSec/OpenSearch absent (corroborated by the repo's own `AUDIT_REPORT.md`).
2. **Hardcoded secrets**: JWT secret `"inec-election-platform-secret-key-2027"` in `auth.go`, demo passwords (`admin123`…), APISIX key in docker-compose.
3. **Health endpoints lie**: `/healthz`/`/readiness` always OK, no DB checks.
4. **No graceful shutdown, CORS `*`, no body-size limits, no rate limit on login.**
5. **Duplicates**: `fluvio-stream copy/` and `lakehouse-analytics copy/` remain (the lakehouse *copy* is actually the better variant — consolidate deliberately).
6. **Stale docs**: `AI_ML_PRODUCTION_AUDIT.md` ("100/100 all gaps closed") contradicts the code as shipped — the fixes in this patch make reality closer to the claims, but docs should be rewritten after retraining real models.

## 6. Round 2 — Campaign Manager Deep Audit

Triggered by the follow-up question *"did you review campaign manager features of the platform?"* — Round 1 had only partially covered them. A dedicated multi-agent audit then examined every campaign-platform page and tRPC router, the Go GOTV service's authorization model, and the four campaign-adjacent microservices. The same fix rule applied: **real logic where cheap, loud failure everywhere else — silence is the bug.**

### 6.1 The most dangerous campaign-manager findings (all fixed)

1. **No tenancy enforcement on the campaign platform.** tRPC procedures took a bare `profileId` from the client and read/wrote any candidate's data — any logged-in user could enumerate, edit, or delete *every* campaign's war room, finances, voter data, and team. Fixed with a `profileScopedProcedure(minRole)` middleware (`server/_core/trpc.ts`) that resolves the caller's membership role per request (fail-closed), applied to **all** profile-scoped procedures, plus row-level `assertProfileRole` on id-based deletes/updates so row ids can't be probed across tenants.
2. **Team management was open to any authenticated user** — including changing any member's role (silent privilege escalation) and accepting invites under the wrong identity. Fixed: owner-only role changes, invite acceptance bound to the invited email (`acceptCampaignInvite(token, userId, userEmail)` + race-safe conditional UPDATE), and because the local users table has no email column, `confirmAccept` now requires the acceptor to type the invited address while `acceptInvite` returns it **masked** (`j***@example.com`) so the check can't be self-defeated.
3. **Schema drift masked by drizzle's silent key-dropping.** Routers passed fields the schema didn't have — drizzle **silently discards unknown keys** in `.values()`, so `election_results.ward`, `social_media_posts.hashtags`, and several client↔server field mismatches (`vin`, `agentStatus`, `puName`, `content` vs `body`, strength/weakness shapes) vanished without error. Fixed: columns added to `drizzle/schema.ts` (+ idempotent `ADD COLUMN IF NOT EXISTS` in migration `0001`), zod contracts aligned to schema on both sides.
4. **gotv-svc fabricated cryptography and leaked OTPs.** The Go service accepted client-supplied "encrypted" ballots without ever calling a crypto backend, returned OTP codes in API responses, and trusted `X-Party-Code` headers from anywhere. Fixed: real crypto-backend integration (`encryptBallotViaBackend` / `verifyBallotProofViaBackend` / `tallyBallotsViaBackend` with circuit breaker, contract: `POST {backend}/encrypt → {ciphertext, proof, ballot_ref}`, `/verify-proof → {valid, ballot_ref}`, `/tally → {results}`), OTP delivered only via SMS/WhatsApp, party-code header fallback gated behind `GOTV_DEV_MODE`, gateway trust requires `GOTV_GATEWAY_SECRET`, Permify fail-closed, WhatsApp webhook HMAC-verified, cross-party access scoped via `partyOwnsElection`/`partyOwnsRound`, coercion votes recorded via `duress_code_hash` instead of a visible `vote_type`.
5. **Frontend tenancy defaults silently pinned users to one party.** GOTV pages defaulted to a hardcoded party code (APC) and a hardcoded election id — a volunteer of any party would see and submit data into the wrong party's workspace without any warning. Fixed: `gotv-session.tsx` adds `useGOTVParty()` (no default; explicit `GOTVPartySelector` when unset), `useResolvedElection()` (never a hardcoded id), auth headers on every GOTV request; mobile KYC no longer fabricates verification results.
6. **Campaign microservices ran unauthenticated with open data access.** `gotv-analytics` exposed a lakehouse query endpoint with no auth and raw SQL (any table, any party's rows); `campaign-planning` trained ML models on synthetic data and let the results influence "predictions" with neutral defaults presented as data; `geolibre-collab` had an unauthenticated websocket with unbounded reads. Fixed: API-key auth fail-closed (`GOTV_ANALYTICS_API_KEY`, `CAMPAIGN_PLANNING_API_KEY`, `GEOLIBRE_COLLAB_TOKEN`), lakehouse 7-view allowlist with server-injected `WHERE party_id = $1`, `/ml/train` admin-gated with synthetic models never promoted to serving, neutral/insufficient-data responses labelled `insufficient_data`, geofence fail-closed, collab server rewritten (per-client buffered channels, deadlines, read limits).
7. **Money stored as floating point.** Campaign finance amounts (`fundraising_transactions.amount`, `budget_items.*`, `diaspora_contacts.pledged_amount`) were `real` (float4). Fixed: `numeric(15,2)` with a hand-written migration; write paths wrapped in transactions.
8. **Silent-accumulation vectors closed:** petition signature dedup (429 on duplicates), login throttle (5 attempts / 5 min → 429), 30-day session TTL, SSE endpoint authenticated and capped at 500 subscribers, LLM prompts input-capped (500/2000 chars) to bound cost injection, bulk imports capped (500 rows, chunks of 100), cron path fixed to `/api/scheduled/deadline-check`, `JWT_SECRET` fail-fast at boot.
9. **Dead mockware deleted:** `server/index.ts` (duplicate server entry), `_core/dataApi.ts`, `imageGeneration.ts`, `voiceTranscription.ts`, `map.ts` (unused generated stubs), `stakeholder_engine.py`, `platform_analytics.py` (pure-fabrication services), `DashboardLayout`, `ComponentShowcase`.

### 6.2 Round-2 fix branches (all merged)

| Branch | Commit | Surface |
|---|---|---|
| `fix/campaign-server-security` | `7e14e17` | tRPC tenancy middleware, team authz, petition dedup, zod↔schema alignment, LLM caps, cron path |
| `fix/campaign-server-db` | `79b54bf` | JWT fail-fast, money numeric + migration, transactions, invite email binding, SSE auth, login throttle, dead-file deletion |
| `fix/campaign-client` | `ff06d70` | 22 client fixes: fail-closed viewer default, real Box–Muller Monte Carlo, XSS guards (`esc`/`safeUrl`/`safeColor`), honest labels |
| `fix/frontend-tenancy` | `59163b7` | `gotv-session.tsx` party/election resolution, KYC user_id from auth, no APC default |
| `fix/gotv-svc-authz` | `3de0a39` | Real crypto-backend calls, OTP delivery, party scoping, webhook HMAC, Permify fail-closed |
| `fix/campaign-services` | `053bb86` | API-key auth, lakehouse allowlist + party scoping, ML train gating, geolibre-collab rewrite |
| (handoff closure, on main) | `5f0e591` | `security` task enum, `ward`/`hashtags` columns + migration, invite email end-to-end |

### 6.3 Round-2 verification (on the fully merged tree)

| Check | Result |
|---|---|
| `campaign-platform` `tsc --noEmit` (full type-check, deps installed) | **PASS** |
| `inec-go-backend` `go build ./...` (go 1.26) | **PASS** |
| `go vet ./cmd/gotv-svc/...` | **PASS** |
| `go test ./cmd/gotv-svc/...` | **PASS** |
| Cumulative patch `git apply --check` onto pristine `1d80c54` | **PASS** (144 files, +7,562/−9,251) |

### 6.4 Round-2 intended breaking changes (ops must act)

| Change | Required action |
|---|---|
| All profile-scoped tRPC procedures require membership | Seed `campaign_members` rows for every legitimate user per candidate profile — previously "working" accounts with no membership row will now get 403 |
| Money columns float4 ⇒ numeric(15,2) | Run migration `campaign-platform/drizzle/0001_audit_money_numeric_and_indexes.sql` before deploying the new server |
| `election_results.ward`, `social_media_posts.hashtags` added | Same migration (idempotent `ADD COLUMN IF NOT EXISTS`) |
| Invite acceptance requires typed email | Tell invitees the acceptance page now asks for the invited email address |
| gotv-svc crypto endpoints call a real backend | Set `GOTV_ELECTION_CRYPTO_BACKEND_URL` implementing `/encrypt`, `/verify-proof`, `/tally` (contract in §6.1.4) |
| OTP no longer in API responses | Configure Africa's Talking (SMS) / WhatsApp credentials; client UX must say "check your phone" |
| `X-Party-Code` header trust gated | Set `GOTV_DEV_MODE=true` only in dev; set `GOTV_GATEWAY_SECRET` in the API gateway for production header trust |
| WhatsApp webhook verified | Set `WHATSAPP_WEBHOOK_TOKEN` + app secret for `X-Hub-Signature-256` |
| Microservice endpoints require API keys | Set `GOTV_ANALYTICS_API_KEY`, `CAMPAIGN_PLANNING_API_KEY`, `GEOLIBRE_COLLAB_TOKEN`; update callers to send them |
| GOTV pages require explicit party selection | Users pick their party once (`GOTVPartySelector`); no silent default exists anymore |
| `JWT_SECRET` fail-fast | Set a strong `JWT_SECRET` env var — the server refuses to boot without it |

### 6.5 Campaign-manager gaps remaining (beyond this patch)

1. **In-memory rate limits / dedup** (login throttle, petition dedup) are per-process — move to Redis for multi-instance deployments.
2. **Crypto backend is an external contract** — gotv-svc now calls it honestly, but a real ElectionGuard/Paillier service must still be deployed (same dependency as Round 1 §4).
3. **Party-ownership model**: a party "owns" an election iff it has delegates/aspirants registered — independent observers with no candidates get read access denied by design; create an explicit observer role if INEC needs one.
4. **In-binary blockchain anchor fallback** exists when `GOTV_ANCHOR_SERVICE_URL` is unset — fine for dev, anchor to real Fabric in production.

## 7. Applying the fixes

```bash
git clone https://github.com/munisp/inec && cd inec
git checkout 1d80c54
git apply /path/to/inec-mockware-fixes.patch   # cumulative: rounds 1 + 2
```

The patch is cumulative (144 files, +7,562/−9,251) and verified to apply cleanly onto `1d80c54`. Round-1 commits are the eight branches listed in §2; Round-2 adds the seven branches in §6.2 — per-branch history is preserved in the merge clone if `git am`-style application is preferred.
## 8. Round 3 — Production-readiness hardening (this patch, final round)

Round 3 answers one question: *is the post-fix tree production-ready, measured against a transparent rubric, at ≥95/100?* Six parallel assessors scored every domain, six fix waves closed the gaps, and three independent verifiers re-scored the merged tree under a strict anti-theater rule: **a fix that fails open, or is defined-but-unwired, is THEATER and earns zero credit.**

### 8.1 The rubric (weights sum to 100)

| # | Dimension | Weight |
|---|---|---|
| 1 | Security, authentication, authorization, tenancy | 20 |
| 2 | Data integrity (money, persistence, races, transactions) | 15 |
| 3 | Secrets & configuration hygiene | 10 |
| 4 | Reliability (timeouts, retries, graceful shutdown, resource caps) | 10 |
| 5 | Observability & health (real healthchecks, metrics, log hygiene) | 10 |
| 6 | API safety (rate limits, payload caps, CORS, error semantics) | 10 |
| 7 | Build, test & CI (compiles, tests run, CI meaningful) | 10 |
| 8 | Deployment & ops (Dockerfiles non-root, compose/helm sane, migrations) | 10 |
| 9 | Documentation accuracy | 5 |

External-service dependencies (Permify, TigerBeetle, the election-crypto backend, SMS/WhatsApp providers) are scored as **documented ops actions, not code defects**, provided the code fails closed when the dependency is absent and the requirement is documented.

### 8.2 Baseline → final

Independent verifier-awarded weighted totals (three verification rounds; anti-theater rule enforced — unwired/fail-open fixes score zero):

| Domain | Baseline | Round 3 | R95 wave | **Final** |
|---|---|---|---|---|
| Go backend | 59 | 78 | 83 | **85** |
| Rust services | 44 | 70 | 80 | **81** |
| Python services | 48 | 73 | 84 | **85** |
| Campaign platform | 53 | 74 | 83 | **83** |
| Infra & deployment | 50 | 72 | 80 | **82** |
| Frontends | 45 | 60 | 71.5 | **73** |
| **Aggregate** | **≈50** | **71** | **80** | **81.5** |

Full per-category scorecard, method, and the honest analysis of the ≥95 target are in `PRODUCTION_READINESS_REPORT.md`. Final tree: **zero THEATER, zero NOT-FOUND** across all verifier rounds; every residual point is a documented architectural tradeoff or ops action, not a code defect.

### 8.3 What Round 3 fixed, by domain (132 of 137 catalogued gaps closed; 5 reclassified as documented ops actions)

**Go backend — `pr/go-hardening` (21 gaps).** New shared JWT middleware (`internal/authmw`) mounted on every `cmd/*-svc` and the gateway: `jti` claim in both token creators with blacklist enforcement, `type!="access"` rejected, `state_code`/`staff_id` claims with `enforceStateTenancy` on result-submit and incident-create handlers, `auth.go` refuses insecure defaults unless `INEC_ENV=development`. Election-transition endpoint takes `user_id` from claims + role check (was client-supplied). MFA Scan error now 500 (was silent nil). ~33 silent write sites error-checked. 7 services `log.Fatal` on missing DSN. CORS default-deny, no credentials-with-wildcard, prod Fatal. GOTV trust path requires `X-Internal-Token`/`INTERNAL_SERVICE_SECRET` (legacy `GOTV_GATEWAY_SECRET` alias kept). `TRUSTED_PROXY_CIDRS` support. Seed passwords env-required; `seed_all_tables.go` deleted. Microservice `/health` does a real DB ping (503 on failure). Three committed Go SDK tarballs removed. `.env.example` (211 vars) + `validateConfig()`. `METRICS_BEARER_TOKEN` guard. WAF warn+metric instead of silent pass. AutoMigrate gated out of production. New `authmw_test.go` (9 tests).

**Python services — `pr/python-hardening` (22 gaps).** Fail-closed API-key middleware (`DOCUMENT_AI_API_KEY`, `LAKEHOUSE_API_KEY`, `BIOMETRIC_API_KEY`, `TALLY_SUBMIT_TOKEN`, `FEDERATED_SUBMIT_KEY`, `GOTV_ANALYTICS_PARTY_KEYS` per-party tenancy map, `PREDICTIVE_ALLOC_ADMIN_KEY`, `MODEL_SERVING_API_KEY`) — 503 when unset, `/health` public. `ngapp123` hardcoded DSN defaults removed (3 files). Tally persistence is real asyncpg (`tally` table), fail-closed in prod. Federated norm capped at 100.0, one update per state per round. CORS default-deny in 6 services. slowapi 10/min on `/speech` + `/what-if`. Upload caps (`_read_upload_capped`, 413). OpenSearch index allowlist `{gotv-contacts, gotv-outreach}`. Real health `SELECT 1`. pipeline-optimizer task lifecycle + Dockerfile. Auth on predictive/digital-twin/ai-anomaly/satellite. Paillier ≥2048-bit assert. neo4j no default password. `asyncio.to_thread` offloads. model-serving lifespan + pins. `reload=True` removed. Docs gated in prod. campaign-planning Dockerfile COPY fix; gotv-analytics py-modules + non-root Dockerfile.

**Rust services — `pr/rust-hardening` (21 gaps).** biometric-rust: `vault_api_key_auth` fail-closed, VaultActor derived from key label/hash (body-supplied actor removed), `DATABASE_URL` `expect()`, tests skip-if-unset, migrations COPYed in Dockerfile, PORT env, SIGTERM, health `SELECT 1`. fluvio: panics→500, `/consume` 5s timeout + 1000 cap + `partial` flag, `FLUVIO_STREAM_API_KEY`. gotv-engine: new multi-stage non-root Dockerfile (`--locked`, EXPOSE 8101), `GOTV_ENGINE_PARTY_KEYS` tenancy. hot-path: 7 unused deps pruned, SIGTERM, kafka deserialize log+error counter, `/readyz` per-sink. rotate_key per-template tx + paged (500) + resumable. CORS_ORIGINS env (both services). SlidingWindowLimiter wired 120/min + 10k Vec caps 413. persistence `write_failures` counter + degraded health. `INFERENCE_API_KEY`, unwrap→`ok_or` 503. geolibre lat/lng validation + `f64::total_cmp` + `GEOLIBRE_API_KEY`. `panic="abort"` removed. CHANNEL_CAPACITY 1M→5000. Cargo.lock committed for all 7 crates. Two pre-existing compile breakages repaired (fluvio-stream vs fluvio 0.24.4; geolibre borrow-after-move).

**Campaign platform — `pr/campaign-hardening` (24 gaps).** 8 upsert IDORs guarded (`WHERE id AND profileId`, NOT_FOUND on 0 rows). `getCampaignMembers` excludes inviteToken, masks emails for viewers. 6 orphan migration files + snapshots 0002–0005 deleted. `SessionUser = Omit<User,"passwordHash">`. task enum aligned, social_media→media transform. Bulk imports `.max(500)` + chunked. SSE `assertProfileRole` + per-user cap 5. publicSign petition: 404/409-active, trust-proxy 1, `req.ip`, ip+petition dedup. 7-day invite expiry. SIGTERM graceful shutdown + `closeDb()`, boot exit 1. health `SELECT 1` (2s) → 503. pressRelease/socialMedia update branches. `.env.example` + prod fail-fast. Per-user LLM limiter 20/hour on 7 invokeLLM procedures. `notifications.status` profileScoped. Dockerfile `ENV PORT=8206`, prod install, `USER node`, db:generate/db:migrate split. TRPCError 4xx in acceptCampaignInvite. `server/_core/sse.ts` breaks circular import. SQL-aggregated KPIs. `oauth.ts` deleted. Real User test fixture. 2mb body limit. Per-IP spray window 30/5min. README + todo.md banner.

**Frontends — `pr/frontend-hardening` (22 gaps).** Demo creds DEV-gated (both apps). 6 GOTV pages on `useGOTVParty`/`gotvAuthHeaders` with `GOTVPartySelector` gate (canonical `X-GOTV-Party-Code`). ResultsPage/IncidentsPage/GOTVAnalytics use `useResolvedElection` + submit gating. Mobile KYC: real 11-digit NIN. WS/SSE `?token=` dropped (edge proxy must translate cookie→Bearer — documented follow-up). expo-crypto CSPRNG; `encrypt*`→`obfuscate*` honesty rename + name masking. Mobile route guards. `page-roles.ts` role gating + nav filter + 403. FormData content-type fix. GeoLibre XSS sinks → textContent/DOM. eas.json `EXPO_PUBLIC_GOTV_API_URL` + startup throw. Observer check-in PU input. New `inec-mobile/src/lib/election.tsx` resolver on 8 screens; `api.ts` electionId required. sw.js strips Authorization; dead zustand deleted. Prod telemetry batched+sanitized to `/api/v1/errors/frontend`. Sync XHR removed; 401→`#/login?returnTo`. `.env` files untracked; pnpm-lock deleted; playwright devDep. Real READMEs both apps.

**Infra & ops — `pr/infra-hardening` (23 gaps).** tourismpay PII dump removed from tree (history purge = owner action, documented). Port conflict resolved (lakehouse → 127.0.0.1:8093). apisix.yaml jwt-auth removed (auth lives in go-backend). pg healthchecks env-based. keycloak secrets → import-realm.sh interpolation. etcd service deleted. `restart: unless-stopped` on stateful core. Superseded banners on stale audit docs. `apisix-conf/` deleted. Resource limits. `.env.example` completed. `* copy/` dirs removed. `.gitignore` hardened (`*.tar.gz`, `.env.*`, `__pycache__`). Prometheus target fixed; grafana `:?` guard. Non-root USER in 14 Dockerfiles. neo4j `:?` + localhost binding. helm `global.imageRegistry`/tag + placeholder refs. CI: E2E strict, rust-services matrix, pytest step, docker matrix. caddy admin → 127.0.0.1. Root junk deleted. Compose injects all new API keys + gotv-analytics service. README hardening section.

### 8.4 Round-3 verification matrix (merged tree @ `07502ba`)

| Check | Result |
|---|---|
| Go: `go build ./...`, `go vet ./...`, `go test ./...` (7 pkgs incl. new authmw tests) | **PASS** (0 FAIL) |
| campaign-platform: `tsc --noEmit` + `vitest` | **PASS** (0 type errors; 5 passed / 1 skipped) |
| inec-frontend: `vite build` + `vitest` | **PASS** (60/60) |
| inec-mobile: `tsc --noEmit` | **PASS** |
| Python: `py_compile` on all changed files; lakehouse `pytest` | **PASS** (8/8 — verifier-recounted; earlier "11/11" claim was inflated) |
| Rust: `cargo check --locked` on all 7 crates | **PASS** |
| YAML validity (compose, CI, k8s) | **PASS** (2 justified exclusions: Permify schema DSL, helm Go-template syntax) |
| Cumulative patch `git apply --check` onto pristine `1d80c54` | **PASS** (see §9 for binary-file note) |

### 8.5 Round-3 intended breaking changes (ops must act — supersedes/extends §6.4)

| Change | Required action |
|---|---|
| All Go services fail-fast on missing DSN/JWT secrets | Populate every var in the new `.env.example` (211 vars) before boot |
| Internal service-to-service calls need keys | Set `INTERNAL_SERVICE_SECRET` (gateway), `*_API_KEY` for every Python/Rust service listed in §8.3; compose already injects them |
| `X-Internal-Token` replaces blind header trust | Gateway must send it; legacy `GOTV_GATEWAY_SECRET` still accepted as alias |
| CORS is default-deny everywhere | Set explicit `CORS_ORIGINS`/`ALLOWED_ORIGINS` per service |
| Frontend SSE/WS no longer accepts `?token=` | Edge proxy must translate the `inec_token` cookie into an `Authorization: Bearer` header for gotv-svc streams |
| Seed users have no default passwords | Set `SEED_*_PASSWORD` env vars; `seed_all_tables.go` is gone |
| 5 binary blobs removed from the tree | `git rm` the 3 Go SDK tarballs + tourismpay dump (they remain in history — owner must purge history to fully remove the PII dump) |
| AutoMigrate disabled in production | Run migrations explicitly via the drizzle SQL files / `db:migrate` |
| LLM procedures rate-limited 20/hour per user | Tune `LLM_RATE_LIMIT` if campaign staff need more |

### 8.6 Remaining known limitations (accepted, documented)

1. **In-memory rate limits/dedup** are per-process (carried from §6.5) — Redis for multi-instance.
2. **Election-crypto backend** remains an external contract (§6.4); code fails closed without it.
3. **Observer role** for parties with no registered candidates (§6.5 item 3) — product decision, not a defect.
4. **Helm templates** not rendered in CI (no helm binary in sandbox) — syntax validated by inspection only.
5. **PII dump in git history** — removed from tree; history rewrite is the repo owner's call (force-push implications documented).

### 8.7 R95 closure wave + mop-up (verifier-driven)

After the first independent re-score (Go 78, Rust 70, Python 73, Infra 72, Campaign 74, Fronte