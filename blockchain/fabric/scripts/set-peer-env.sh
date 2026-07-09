#!/usr/bin/env bash
# Source-able helper that exports the peer CLI environment for a given org.
# Must be sourced so the exports reach the caller: `source ./scripts/set-peer-env.sh <inec|observers>`
set -euo pipefail
ORG="${1:?usage: set-peer-env.sh <inec|observers>}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

export CORE_PEER_TLS_ENABLED=true
export FABRIC_CFG_PATH="${ROOT}/config"

case "$ORG" in
  inec)
    export CORE_PEER_LOCALMSPID=INECMSP
    export CORE_PEER_ADDRESS=localhost:7051
    export CORE_PEER_TLS_ROOTCERT_FILE="${ROOT}/organizations/peerOrganizations/inec.inec.gov.ng/peers/peer0.inec.inec.gov.ng/tls/ca.crt"
    export CORE_PEER_MSPCONFIGPATH="${ROOT}/organizations/peerOrganizations/inec.inec.gov.ng/users/Admin@inec.inec.gov.ng/msp"
    ;;
  observers)
    export CORE_PEER_LOCALMSPID=ObserversMSP
    export CORE_PEER_ADDRESS=localhost:9051
    export CORE_PEER_TLS_ROOTCERT_FILE="${ROOT}/organizations/peerOrganizations/observers.inec.gov.ng/peers/peer0.observers.inec.gov.ng/tls/ca.crt"
    export CORE_PEER_MSPCONFIGPATH="${ROOT}/organizations/peerOrganizations/observers.inec.gov.ng/users/Admin@observers.inec.gov.ng/msp"
    ;;
  *) echo "unknown org: $ORG"; exit 1 ;;
esac
