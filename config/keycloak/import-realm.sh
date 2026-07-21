#!/usr/bin/env bash
set -euo pipefail

# Keycloak imports JSON literally; it does not expand ${VARIABLE} placeholders
# contained in a mounted realm file. Render the template inside the container so
# secrets stay in runtime environment configuration rather than source control.
required=(
  KEYCLOAK_CLIENT_SECRET
  INEC_ADMIN_PASSWORD
  OFFICER_PASSWORD
  OBSERVER_PASSWORD
)

for name in "${required[@]}"; do
  value="${!name:-}"
  if [[ -z "${value}" ]]; then
    echo "${name} must be configured before importing the INEC realm" >&2
    exit 64
  fi
  # The template interpolation is intentionally constrained to base64url-like
  # credentials and URL-safe password characters. This prevents sed delimiter
  # injection while still allowing strong generated secrets.
  if [[ ! "${value}" =~ ^[A-Za-z0-9._~@%+=:-]+$ ]]; then
    echo "${name} contains unsupported characters for the realm import template" >&2
    exit 64
  fi
done

template=/opt/keycloak/data/import-template/inec-realm.json
target_dir=/opt/keycloak/data/import
target="${target_dir}/inec-realm.json"
mkdir -p "${target_dir}"

sed \
  -e "s|\${KEYCLOAK_CLIENT_SECRET}|${KEYCLOAK_CLIENT_SECRET}|g" \
  -e "s|\${ADMIN_PASSWORD}|${INEC_ADMIN_PASSWORD}|g" \
  -e "s|\${OFFICER_PASSWORD}|${OFFICER_PASSWORD}|g" \
  -e "s|\${OBSERVER_PASSWORD}|${OBSERVER_PASSWORD}|g" \
  "${template}" > "${target}"

chmod 600 "${target}"
exec /opt/keycloak/bin/kc.sh start-dev --import-realm
