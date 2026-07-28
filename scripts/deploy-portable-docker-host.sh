#!/usr/bin/env bash
# Deploy the INEC platform and repaired campaign dashboard to an authorized Docker host.
# This script never creates secrets, falls back to :latest, or changes DNS/firewall rules.
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/deploy-portable-docker-host.sh validate --env-file /secure/inec/.env
  scripts/deploy-portable-docker-host.sh deploy   --env-file /secure/inec/.env
  scripts/deploy-portable-docker-host.sh status   --env-file /secure/inec/.env
  scripts/deploy-portable-docker-host.sh smoke    --env-file /secure/inec/.env

The environment file must be protected (chmod 600) and include the reviewed
production values plus INEC_PUBLIC_HOST, CAMPAIGN_PUBLIC_HOST, INEC_FRONTEND_IMAGE,
INEC_BACKEND_IMAGE, and CAMPAIGN_DASHBOARD_IMAGE. Image values must be immutable
GHCR digest or SHA-tag references; :latest is rejected.
USAGE
}

command_name="${1:-}"
shift || true
env_file=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-file)
      env_file="${2:-}"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

if [[ -z "$command_name" || -z "$env_file" ]]; then
  usage >&2
  exit 64
fi
if [[ ! -f "$env_file" ]]; then
  printf 'Environment file does not exist: %s\n' "$env_file" >&2
  exit 66
fi
if [[ "$(stat -c '%a' "$env_file")" != "600" ]]; then
  printf 'Refusing to use %s: set permission mode 600 first.\n' "$env_file" >&2
  exit 77
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose=(docker compose --env-file "$env_file" -f "$repo_root/docker-compose.yml" -f "$repo_root/deploy/portable/docker-compose.images.yml")

# Docker Compose interpolation validates the full service graph. Source only the
# operator-provided file after its permissions have been checked.
set -a
# shellcheck disable=SC1090
source "$env_file"
set +a

required=(INEC_PUBLIC_HOST CAMPAIGN_PUBLIC_HOST INEC_CADDY_IMAGE INEC_FRONTEND_IMAGE INEC_BACKEND_IMAGE CAMPAIGN_DASHBOARD_IMAGE JWT_SECRET)
for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    printf 'Required portable deployment value is empty: %s\n' "$name" >&2
    exit 78
  fi
done
for name in INEC_CADDY_IMAGE INEC_FRONTEND_IMAGE INEC_BACKEND_IMAGE CAMPAIGN_DASHBOARD_IMAGE; do
  reference="${!name}"
  if [[ "$reference" == *':latest' || "$reference" != ghcr.io/* || ( "$reference" != *@sha256:* && "$reference" != *':sha-'* ) ]]; then
    printf 'Refusing non-immutable image reference for %s: %s\n' "$name" "$reference" >&2
    exit 78
  fi
done

validate() {
  command -v docker >/dev/null || { echo 'Docker is required.' >&2; return 69; }
  docker info >/dev/null
  "${compose[@]}" config --quiet
}

case "$command_name" in
  validate)
    validate
    echo 'Portable deployment configuration is valid.'
    ;;
  deploy)
    validate
    "${compose[@]}" pull frontend go-backend campaign-dashboard caddy
    "${compose[@]}" up -d --remove-orphans
    "${compose[@]}" ps
    ;;
  status)
    validate
    "${compose[@]}" ps
    ;;
  smoke)
    validate
    "${compose[@]}" ps --status running
    curl --fail --silent --show-error --resolve "${INEC_PUBLIC_HOST}:443:127.0.0.1" "https://${INEC_PUBLIC_HOST}/" >/dev/null
    curl --fail --silent --show-error --resolve "${CAMPAIGN_PUBLIC_HOST}:443:127.0.0.1" "https://${CAMPAIGN_PUBLIC_HOST}/" >/dev/null
    "${compose[@]}" exec -T go-backend sh -ec 'wget -qO- http://127.0.0.1:8088/healthz >/dev/null || curl -fsS http://127.0.0.1:8088/healthz >/dev/null'
    "${compose[@]}" exec -T campaign-dashboard sh -ec 'wget -qO- http://127.0.0.1:8206/api/v1/campaign/health >/dev/null'
    echo 'Portable deployment smoke checks passed.'
    ;;
  *)
    printf 'Unknown command: %s\n' "$command_name" >&2
    usage >&2
    exit 64
    ;;
esac
