# Portable Docker-Host Deployment

## Purpose

This release path deploys the repaired INEC dashboard and campaign dashboard to an **authorized Docker host** without requiring a Kasicloud self-hosted runner. It retains the existing Docker Compose service topology, Caddy edge, Coraza/OpenAppSec-capable gateway image, PostgreSQL stack, and fail-closed production configuration. The release is activated through the GitHub-hosted **Portable Docker Host Deploy** workflow, so the GitHub runner only needs SSH access to the selected host.

> This is a production deployment mechanism, not a public-development shortcut. The selected host must be controlled by INEC or its approved operator, have Docker Compose v2 installed, expose TCP ports 80 and 443, and have DNS records for both public hostnames.

## Deployment Architecture

| Component | Portable mechanism |
|---|---|
| Caddy edge | Immutable `INEC_CADDY_IMAGE` pulled from GHCR, retaining the custom WAF-capable gateway build. |
| INEC PWA | Immutable `INEC_FRONTEND_IMAGE` pulled from GHCR and served behind Caddy. |
| Go backend | Immutable `INEC_BACKEND_IMAGE` pulled from GHCR inside the existing Compose topology. |
| Campaign dashboard | Immutable `CAMPAIGN_DASHBOARD_IMAGE` proxied as a separate public Caddy virtual host. |
| Edge/TLS | Caddy provisions certificates for publicly resolvable DNS names; it uses internal TLS for local or `.local` names. |
| Deployment controller | GitHub-hosted manual workflow SSHs to the approved Docker host and runs the guarded release script. |
| Secrets | The host keeps its protected environment file locally. GitHub receives only SSH connection and host-path secrets. |

## One-Time Host Preparation

Clone the repository to a protected host path, authenticate the host Docker daemon to read approved GHCR images, and create a protected environment file outside the repository.

```bash
git clone https://github.com/munisp/inec.git /srv/inec
sudo install -d -m 700 -o "$USER" -g "$USER" /etc/inec
sudo cp /srv/inec/deploy/portable/.env.portable.example /etc/inec/portable.env
sudo chmod 600 /etc/inec/portable.env
```

Set the public hostnames, exact commit-tagged image references, and all mandatory values inherited from `.env.production.example`. Do **not** use `:latest`; `scripts/deploy-portable-docker-host.sh` rejects it. The selected image tags must correspond to the reviewed commit, for example `sha-4e89f7f` when that tag exists in GHCR.

The host must complete GHCR authentication out of band using an approved read-only package credential. Do not put the registry credential in the repository, the environment file, or the workflow inputs.

```bash
docker login ghcr.io
cd /srv/inec
scripts/deploy-portable-docker-host.sh validate --env-file /etc/inec/portable.env
```

## GitHub Environment Secrets

Create these secrets in the protected GitHub environment selected at workflow dispatch. They are consumed only by `.github/workflows/portable-docker-host-deploy.yml`.

| Secret | Required value |
|---|---|
| `PORTABLE_DEPLOY_HOST` | DNS name or IP address of the approved Docker host. |
| `PORTABLE_DEPLOY_USER` | Least-privileged host account permitted to run the release script and Docker Compose. |
| `PORTABLE_DEPLOY_SSH_KEY` | Private SSH key for that deployment account. |
| `PORTABLE_DEPLOY_KNOWN_HOSTS` | Pinned `known_hosts` entry for the approved host. |
| `PORTABLE_DEPLOY_PATH` | Absolute Git checkout path, such as `/srv/inec`. |
| `PORTABLE_DEPLOY_ENV_FILE` | Absolute protected environment-file path, such as `/etc/inec/portable.env`. |

## Release Procedure

Dispatch **Portable Docker Host Deploy** from GitHub Actions and select the reviewed revision plus the protected environment. The workflow checks that every connection secret is present, uses strict host-key checking, checks out the requested immutable revision on the target host, runs `deploy`, and then runs `smoke`.

The host-side script verifies the Compose topology before any container start. It pulls only the three release images from GHCR, starts the existing service graph, and confirms both public sites plus the two internal health endpoints. It neither creates secrets nor changes DNS, firewall rules, or operational feature flags.

## Manual Emergency Execution

An approved operator with direct access to the host can run the same guarded process without GitHub Actions.

```bash
cd /srv/inec
git fetch --tags origin
git checkout --detach 4e89f7fa989a25d9217fcc5663398b9b65fe9ba7
scripts/deploy-portable-docker-host.sh deploy --env-file /etc/inec/portable.env
scripts/deploy-portable-docker-host.sh smoke --env-file /etc/inec/portable.env
```

## Rollback

Choose an earlier reviewed SHA in the workflow, or execute the manual process with that SHA. The script uses commit-specific images and does not mutate database contents or production secrets, so rollback is a controlled image/configuration reversal. Database migrations must follow the established migration and change-control process; this workflow does not run destructive schema reversals.

## Current Limitation

The production DNS names `inec.servers.upi.dev` and `campaign.inec.servers.upi.dev` cannot be moved by repository code alone. Their DNS or upstream load-balancer targets must be updated by the domain or infrastructure owner to point to the authorized portable Docker host before this workaround can become the public production endpoint.

## Local Validation Record

| Validation | Result |
|---|---|
| Portable environment and Compose overlay rendering | Passed with Docker Compose v2 against a non-secret validation environment. |
| Guarded host-script syntax and `validate` command | Passed under Docker-administrator privileges. |
| Environment-file permission guard | Passed: deployment rejects a mode other than `0600`. |
| Immutable-image guard | Passed: deployment rejects `:latest` references. |
| Workflow and Compose YAML linting | Passed. |
| Caddy toolchain compatibility | Corrected: the legacy Caddy 2.7.6 Go 1.21 builder could not resolve Coraza Caddy v2. The custom edge now uses Caddy 2.11.2 with Coraza Caddy v2.5.0, which resolves the confirmed module-version constraint. |

The GitHub-hosted **Build Portable Release** workflow remains the authoritative full container-build validation and publishes the immutable image tags consumed by the host deployment workflow.
