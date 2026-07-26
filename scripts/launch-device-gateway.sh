#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${APISIX_DEVICE_CONFIG_DIR:?set APISIX_DEVICE_CONFIG_DIR to a protected writable host directory}"

if [[ "$APISIX_DEVICE_CONFIG_DIR" == "$repo_dir"/* || "$APISIX_DEVICE_CONFIG_DIR" == "$repo_dir" ]]; then
  printf 'APISIX_DEVICE_CONFIG_DIR must be outside the repository to protect certificate material\n' >&2
  exit 64
fi

mkdir -p "$APISIX_DEVICE_CONFIG_DIR"
chmod 0700 "$APISIX_DEVICE_CONFIG_DIR"

export APISIX_DEVICE_CONFIG_DIR
"$repo_dir/config/apisix/render-device-gateway-config.py"

if ! grep -q '^ssls:' "$APISIX_DEVICE_CONFIG_DIR/apisix.yaml"; then
  printf 'rendered APISIX configuration lacks a device mTLS SSL resource\n' >&2
  exit 65
fi

cd "$repo_dir"
docker compose \
  --env-file "${ENV_FILE:-.env.production}" \
  -f docker-compose.yml \
  -f docker-compose.device-gateway.yml \
  config >/tmp/inec-device-gateway-compose-rendered.yaml

docker compose \
  --env-file "${ENV_FILE:-.env.production}" \
  -f docker-compose.yml \
  -f docker-compose.device-gateway.yml \
  up -d apisix
