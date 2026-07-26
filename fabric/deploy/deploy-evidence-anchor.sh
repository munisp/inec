#!/usr/bin/env bash
# Deploy the evidence-anchor chaincode from an authorized Fabric CLI host.
# This script never generates MSP identities, peers, orderers, or a consortium.
# Each participating organization runs `approve`; an authorized channel operator
# then runs `commit` and `initialize-governance` after the policy approvals exist.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEFINITION_FILE="${FABRIC_DEFINITION_FILE:-${ROOT_DIR}/fabric/deploy/evidence-anchor-definition.env}"
CHAINCODE_DIR="${FABRIC_CHAINCODE_DIR:-${ROOT_DIR}/fabric/chaincode/evidence-anchor}"
PACKAGE_DIR="${FABRIC_PACKAGE_DIR:-${ROOT_DIR}/fabric/packages}"
ACTION="${1:-}"

if [[ -z "${ACTION}" ]]; then
  echo "usage: $0 {package|approve|commit|initialize-governance}" >&2
  exit 64
fi
if [[ ! -r "${DEFINITION_FILE}" ]]; then
  echo "Fabric chaincode definition is not readable: ${DEFINITION_FILE}" >&2
  exit 65
fi
if [[ ! -d "${CHAINCODE_DIR}" ]]; then
  echo "Fabric chaincode source is not available: ${CHAINCODE_DIR}" >&2
  exit 66
fi
if ! command -v peer >/dev/null 2>&1; then
  echo "The Fabric peer CLI is required on this authorized deployment host" >&2
  exit 69
fi

# shellcheck disable=SC1090
source "${DEFINITION_FILE}"
mkdir -p "${PACKAGE_DIR}"
PACKAGE_FILE="${PACKAGE_DIR}/${FABRIC_CHAINCODE_LABEL}.tar.gz"

required_common=(FABRIC_CHANNEL FABRIC_CHAINCODE FABRIC_CHAINCODE_VERSION FABRIC_CHAINCODE_SEQUENCE FABRIC_CHAINCODE_LABEL FABRIC_SIGNATURE_POLICY)
for name in "${required_common[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required definition variable: ${name}" >&2
    exit 64
  fi
done

require_peer_identity() {
  local name
  for name in CORE_PEER_LOCALMSPID CORE_PEER_MSPCONFIGPATH CORE_PEER_ADDRESS CORE_PEER_TLS_ROOTCERT_FILE; do
    if [[ -z "${!name:-}" ]]; then
      echo "Missing Fabric peer identity variable: ${name}" >&2
      exit 64
    fi
  done
  [[ -r "${CORE_PEER_TLS_ROOTCERT_FILE}" ]] || { echo "Peer TLS root certificate is unreadable" >&2; exit 66; }
  [[ -d "${CORE_PEER_MSPCONFIGPATH}" ]] || { echo "Peer MSP configuration directory is unreadable" >&2; exit 66; }
}

require_orderer() {
  local name
  for name in FABRIC_ORDERER_ADDRESS FABRIC_ORDERER_TLS_CA; do
    if [[ -z "${!name:-}" ]]; then
      echo "Missing Fabric ordering-service variable: ${name}" >&2
      exit 64
    fi
  done
  [[ -r "${FABRIC_ORDERER_TLS_CA}" ]] || { echo "Orderer TLS CA is unreadable" >&2; exit 66; }
}

package_chaincode() {
  if [[ ! -f "${PACKAGE_FILE}" ]]; then
    (cd "${CHAINCODE_DIR}" && go mod vendor)
    peer lifecycle chaincode package "${PACKAGE_FILE}" --path "${CHAINCODE_DIR}" --lang golang --label "${FABRIC_CHAINCODE_LABEL}"
  fi
  peer lifecycle chaincode calculatepackageid "${PACKAGE_FILE}"
}

package_id() {
  package_chaincode | tail -n 1
}

build_commit_peer_args() {
  if [[ -z "${FABRIC_COMMIT_PEER_ADDRESSES:-}" || -z "${FABRIC_COMMIT_PEER_TLS_ROOT_CERTS:-}" ]]; then
    echo "FABRIC_COMMIT_PEER_ADDRESSES and FABRIC_COMMIT_PEER_TLS_ROOT_CERTS are required for commit or governance initialization" >&2
    exit 64
  fi
  IFS=',' read -r -a addresses <<< "${FABRIC_COMMIT_PEER_ADDRESSES}"
  IFS=',' read -r -a roots <<< "${FABRIC_COMMIT_PEER_TLS_ROOT_CERTS}"
  if [[ "${#addresses[@]}" -lt 2 || "${#addresses[@]}" -ne "${#roots[@]}" ]]; then
    echo "At least two matching commit peer addresses and TLS root certificates are required" >&2
    exit 64
  fi
  COMMIT_PEER_ARGS=()
  local index
  for index in "${!addresses[@]}"; do
    addresses[${index}]="${addresses[${index}]//[[:space:]]/}"
    roots[${index}]="${roots[${index}]//[[:space:]]/}"
    [[ -r "${roots[${index}]}" ]] || { echo "Commit peer TLS root is unreadable: ${roots[${index}]}" >&2; exit 66; }
    COMMIT_PEER_ARGS+=(--peerAddresses "${addresses[${index}]}" --tlsRootCertFiles "${roots[${index}]}")
  done
}

case "${ACTION}" in
  package)
    package_chaincode
    ;;
  approve)
    require_peer_identity
    require_orderer
    PACKAGE_ID="$(package_id)"
    peer lifecycle chaincode install "${PACKAGE_FILE}"
    peer lifecycle chaincode approveformyorg \
      --channelID "${FABRIC_CHANNEL}" \
      --name "${FABRIC_CHAINCODE}" \
      --version "${FABRIC_CHAINCODE_VERSION}" \
      --sequence "${FABRIC_CHAINCODE_SEQUENCE}" \
      --package-id "${PACKAGE_ID}" \
      --signature-policy "${FABRIC_SIGNATURE_POLICY}" \
      --tls --cafile "${FABRIC_ORDERER_TLS_CA}" -o "${FABRIC_ORDERER_ADDRESS}" --waitForEvent
    ;;
  commit)
    require_peer_identity
    require_orderer
    build_commit_peer_args
    peer lifecycle chaincode checkcommitreadiness \
      --channelID "${FABRIC_CHANNEL}" \
      --name "${FABRIC_CHAINCODE}" \
      --version "${FABRIC_CHAINCODE_VERSION}" \
      --sequence "${FABRIC_CHAINCODE_SEQUENCE}" \
      --signature-policy "${FABRIC_SIGNATURE_POLICY}" \
      --output json --tls --cafile "${FABRIC_ORDERER_TLS_CA}" -o "${FABRIC_ORDERER_ADDRESS}" \
      "${COMMIT_PEER_ARGS[@]}"
    peer lifecycle chaincode commit \
      --channelID "${FABRIC_CHANNEL}" \
      --name "${FABRIC_CHAINCODE}" \
      --version "${FABRIC_CHAINCODE_VERSION}" \
      --sequence "${FABRIC_CHAINCODE_SEQUENCE}" \
      --signature-policy "${FABRIC_SIGNATURE_POLICY}" \
      --tls --cafile "${FABRIC_ORDERER_TLS_CA}" -o "${FABRIC_ORDERER_ADDRESS}" \
      "${COMMIT_PEER_ARGS[@]}"
    ;;
  initialize-governance)
    require_peer_identity
    require_orderer
    build_commit_peer_args
    : "${FABRIC_CONSORTIUM_MSPS_JSON:?FABRIC_CONSORTIUM_MSPS_JSON must be a JSON array, for example [\"INECMSP\",\"ObserverMSP\"]}"
    governance_json="${FABRIC_CONSORTIUM_MSPS_JSON//\\/\\\\}"
    governance_json="${governance_json//\"/\\\"}"
    peer chaincode invoke \
      --channelID "${FABRIC_CHANNEL}" \
      --name "${FABRIC_CHAINCODE}" \
      --ctor "{\"function\":\"InitializeGovernance\",\"Args\":[\"${governance_json}\"]}" \
      --tls --cafile "${FABRIC_ORDERER_TLS_CA}" -o "${FABRIC_ORDERER_ADDRESS}" \
      "${COMMIT_PEER_ARGS[@]}"
    ;;
  *)
    echo "unknown action: ${ACTION}" >&2
    exit 64
    ;;
esac
