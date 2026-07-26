#!/usr/bin/env bash
# Launch the optional device-operational settlement control plane only after
# documented INEC finance/FSPIOP authorization. This script never generates
# accounts, certificates, or payment credentials.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

require_value() {
  local name="$1"
  local value="${!name:-}"
  if [[ -z "${value//[[:space:]]/}" ]]; then
    printf '%s\n' "operational settlement launch rejected: ${name} must be set" >&2
    exit 64
  fi
}

require_file() {
  local name="$1"
  local path="${!name:-}"
  require_value "$name"
  if [[ ! -f "$path" ]]; then
    printf '%s\n' "operational settlement launch rejected: ${name} is not a readable file" >&2
    exit 64
  fi
}

if [[ "${OPERATIONAL_SETTLEMENTS_ENABLED:-false}" != "true" ]]; then
  printf '%s\n' 'operational settlement launch rejected: OPERATIONAL_SETTLEMENTS_ENABLED must equal true' >&2
  exit 64
fi
if [[ "${MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED:-false}" != "true" ]]; then
  printf '%s\n' 'operational settlement launch rejected: MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED must equal true' >&2
  exit 64
fi

for name in \
  OPERATIONAL_SETTLEMENT_TREASURY_ACCOUNT \
  OPERATIONAL_SETTLEMENT_DEVICE_ACCOUNT_PREFIX \
  OPERATIONAL_SETTLEMENT_TB_LEDGER \
  OPERATIONAL_SETTLEMENT_TB_ACCOUNT_CODE \
  OPERATIONAL_SETTLEMENT_TB_TRANSFER_CODE \
  OPERATIONAL_SETTLEMENT_MAX_AMOUNT_MINOR \
  MOJALOOP_URL \
  MOJALOOP_OPERATIONAL_PAYER_FSP \
  MOJALOOP_SECRETS_DIR; do
  require_value "$name"
done

if [[ ! "${MOJALOOP_URL}" =~ ^https:// ]]; then
  printf '%s\n' 'operational settlement launch rejected: MOJALOOP_URL must use HTTPS' >&2
  exit 64
fi
if [[ ! -d "${MOJALOOP_SECRETS_DIR}" ]]; then
  printf '%s\n' 'operational settlement launch rejected: MOJALOOP_SECRETS_DIR is not a directory' >&2
  exit 64
fi
if [[ ! "${OPERATIONAL_SETTLEMENT_TB_LEDGER}" =~ ^[1-9][0-9]*$ ]] || \
   [[ ! "${OPERATIONAL_SETTLEMENT_TB_ACCOUNT_CODE}" =~ ^[1-9][0-9]*$ ]] || \
   [[ ! "${OPERATIONAL_SETTLEMENT_TB_TRANSFER_CODE}" =~ ^[1-9][0-9]*$ ]] || \
   [[ ! "${OPERATIONAL_SETTLEMENT_MAX_AMOUNT_MINOR}" =~ ^[1-9][0-9]*$ ]]; then
  printf '%s\n' 'operational settlement launch rejected: ledger, account code, transfer code, and maximum amount must be positive integers' >&2
  exit 64
fi

require_file MOJALOOP_TLS_CERT_FILE
require_file MOJALOOP_TLS_KEY_FILE
require_file MOJALOOP_CA_FILE
openssl x509 -in "$MOJALOOP_TLS_CERT_FILE" -noout >/dev/null
openssl pkey -in "$MOJALOOP_TLS_KEY_FILE" -noout >/dev/null
openssl x509 -in "$MOJALOOP_CA_FILE" -noout >/dev/null

docker compose -f docker-compose.yml -f docker-compose.operational-settlement.yml config --quiet
docker compose -f docker-compose.yml -f docker-compose.operational-settlement.yml up -d go-backend
