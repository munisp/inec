# inec-biometric-vault (biometric-rust)

Rust biometric vault and matching service for the INEC election platform:
encrypted biometric template storage, cancelable biometrics, and
fingerprint/face/iris matching with score fusion. Persists to PostgreSQL
(sqlx); the schema is embedded from `migrations/001_biometric_tables.sql`.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/health` | public | Readiness probe (see below) |
| GET | `/vault/stats` | API key | Vault statistics |
| POST | `/vault/encrypt` | API key | Encrypt and store a biometric template |
| POST | `/vault/decrypt` | API key | Decrypt a stored template |
| POST | `/vault/rotate-key` | API key | Transactional vault key rotation |
| GET | `/vault/audit` | API key | Audit log for vault operations |
| POST | `/cancelable/create` | API key | Create a cancelable template |
| POST | `/cancelable/apply` | API key | Apply a cancelable transform |
| POST | `/cancelable/revoke` | API key | Revoke a cancelable template |
| POST | `/cancelable/compare` | API key | Compare cancelable templates |
| POST | `/match/fingerprint` | API key | Fingerprint matching |
| POST | `/match/face` | API key | Face matching |
| POST | `/match/iris` | API key | Iris matching |
| POST | `/match/fuse` | API key | Multi-modal score fusion |

## Authentication

All endpoints except `/health` require the `x-api-key` header.

- `BIOMETRIC_VAULT_API_KEY` — **required**. Accepts a comma-separated list
  (`KEY1,KEY2`) so key rotation is possible without downtime; any listed key
  authenticates. Comparison is constant-time (length check, then
  XOR-accumulate). When unset/empty the service fails closed: every
  non-`/health` request returns `503`.
- The audit actor identity is derived from the key that actually
  authenticated: `BIOMETRIC_VAULT_KEY_LABEL` when set, otherwise a SHA-256
  hash prefix of the matched key. The raw key is never logged.
- Wrong/missing key → `401`.

## Environment

| Variable | Required | Default | Notes |
|---|---|---|---|
| `BIOMETRIC_VAULT_API_KEY` | yes | — | Comma-separated list allowed (rotation) |
| `DATABASE_URL` | yes | — | No fallback credentials; missing = fatal at startup |
| `PORT` | no | `8091` | Listen port |
| `BIOMETRIC_VAULT_KEY_LABEL` | no | — | Stable audit actor label |
| `CORS_ORIGINS` | no | deny all | Comma-separated allowed origins; unset = no cross-origin requests |
| `TEST_DATABASE_URL` | tests | — | PostgreSQL DSN for `#[ignore]`d integration tests |

## Health / readiness semantics

`GET /health` is public and readiness-style: it performs a real PostgreSQL
health check. `200` with `"database": "up"` when the vault can serve;
`503` with `"status": "unhealthy"` when PostgreSQL is unreachable.

## Container

Multi-stage Dockerfile (rust:1-bookworm builder → debian:bookworm-slim,
non-root). Build context is the **repo root** (the build embeds
`migrations/`):

```
docker build -f services/biometric-rust/Dockerfile .
```
