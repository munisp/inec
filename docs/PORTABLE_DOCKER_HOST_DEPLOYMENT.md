# Portable Docker-Host Deployment

## Purpose

This **hybrid portable release** deploys the repaired INEC dashboard and campaign dashboard to an authorized Docker host without Kasicloud or a self-hosted GitHub runner. It retains the established Docker Compose topology, PostgreSQL stack, Caddy edge, Coraza-capable gateway, and fail-closed production configuration.

The release deliberately uses two verified immutable GitHub Container Registry artifacts for the repaired INEC PWA and Go backend. It then builds the campaign dashboard and the custom Caddy edge on the authorized host from an exact, clean Git revision. This removes the dependency on unpublished campaign/Caddy images while preventing source drift.

> This is a production deployment mechanism, not a public-development shortcut. The selected host must be owned or approved by INEC, provide Docker Engine and Docker Compose v2, expose TCP ports 80 and 443, and have public DNS records for both dashboard names.

## Deployment Architecture

| Component | Portable mechanism |
|---|---|
| INEC PWA | Pulls the immutable `INEC_FRONTEND_IMAGE` digest from GHCR. |
| Go backend | Pulls the immutable `INEC_BACKEND_IMAGE` digest from GHCR. |
| Caddy edge | Builds from `caddy/Dockerfile` at `PORTABLE_SOURCE_SHA`, retaining the custom WAF-capable build. |
| Campaign dashboard | Builds from `campaign-platform/Dockerfile` at `PORTABLE_SOURCE_SHA`. |
| Edge/TLS | Caddy provisions certificates for publicly resolvable DNS names and uses internal TLS for local or `.local` names. |
| Deployment controller | An approved host operator runs the guarded script; no self-hosted GitHub runner is required. |
| Secrets | The host holds its protected environment file locally; no credentials are committed or written by the script. |

## Verified Release Inputs

The successful GitHub-hosted CD run for commit `0349777` produced the following immutable artifacts:

| Service | Required reference |
|---|---|
| INEC PWA | `ghcr.io/munisp/inec-frontend:sha-0349777@sha256:a1867e32f27a43fcebca7f5e06d02dff67c638a95e1d9dd43d92f90cd99d9931` |
| Go backend | `ghcr.io/munisp/inec-backend:sha-0349777@sha256:52ab755d52591c43345f8cc7ebefec9291102bebec5cfc0aec9c0e26a1c9430d` |

The environment template contains these exact references. Do not replace them with `:latest`; the guarded script rejects mutable tags.

## One-Time Host Preparation

Clone the repository to a protected host path, then create a protected environment file outside the repository.

```bash
git clone https://github.com/munisp/inec.git /srv/inec
cd /srv/inec
git checkout --detach REPLACE_WITH_PORTABLE_SOURCE_SHA
sudo install -d -m 700 -o "$USER" -g "$USER" /etc/inec
sudo cp deploy/portable/.env.portable.example /etc/inec/portable.env
sudo chmod 600 /etc/inec/portable.env
```

Set `PORTABLE_SOURCE_SHA` in `/etc/inec/portable.env` to the exact detached revision. The script verifies that the checkout matches it and rejects a dirty working tree before it builds any host-side component.

Set both public hostnames and all mandatory values inherited from `.env.production.example`. The host needs a read-only approved GHCR credential to pull the two protected image digests; provide it interactively, not in repository files or environment inputs.

```bash
docker login ghcr.io
cd /srv/inec
scripts/deploy-portable-docker-host.sh validate --env-file /etc/inec/portable.env
```

## Release Procedure

On the approved Docker host, use the same guarded command for release activation.

```bash
cd /srv/inec
git fetch origin main
git checkout --detach "$PORTABLE_SOURCE_SHA"
scripts/deploy-portable-docker-host.sh deploy --env-file /etc/inec/portable.env
scripts/deploy-portable-docker-host.sh smoke --env-file /etc/inec/portable.env
```

The host-side script performs the following controls before starting containers:

1. Requires a `0600` environment file and the mandatory production values.
2. Verifies that frontend/backend values are immutable GHCR digests and not `:latest`.
3. Verifies that the repository is clean and exactly at `PORTABLE_SOURCE_SHA`.
4. Renders the complete Compose topology before any start action.
5. Pulls only the two approved GHCR artifacts and builds only Caddy and campaign dashboard locally from the verified source revision.
6. Starts the existing topology, then provides separate `smoke` checks for both public dashboards and both internal health endpoints.

It does **not** create secrets, change DNS, change firewall rules, enable financial integrations, or run destructive database rollbacks.

## Status, Smoke Checks, and Rollback

```bash
scripts/deploy-portable-docker-host.sh status --env-file /etc/inec/portable.env
scripts/deploy-portable-docker-host.sh smoke --env-file /etc/inec/portable.env
```

To roll back, choose an earlier reviewed revision and match it with the corresponding immutable frontend/backend image digests. The script does not mutate database contents or production secrets. Database migrations remain subject to the established migration and change-control process.

## Current Limitation

The current public DNS names, `inec.servers.upi.dev` and `campaign.inec.servers.upi.dev`, cannot be redirected by repository code. Their DNS records or upstream load-balancer targets must be changed by the domain or infrastructure owner to point at the approved portable Docker host before the workaround can serve the production URLs.

## Validation Record

| Validation | Result |
|---|---|
| Portable environment and hybrid Compose overlay rendering | Passed with Docker Compose v2 against a non-secret validation environment. |
| Guarded script syntax and host prerequisite validation | Passed under Docker-administrator privileges. |
| Environment-file permission guard | Passed: a mode other than `0600` is rejected. |
| Immutable image guard | Passed: mutable `:latest` values are rejected. |
| Source revision and dirty-tree guards | Implemented and verified through the script’s deterministic validation path. |
| Existing GHCR artifact verification | Confirmed from successful CD run `30376086654`: immutable frontend and backend digest references above were built and pushed. |
| Caddy toolchain compatibility | Corrected: Caddy now uses `2.11.2` with Coraza Caddy v2.5.0, resolving the prior Go-toolchain module constraint. |
