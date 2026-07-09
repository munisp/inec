#!/usr/bin/env bash
# Bootstrap the INEC Hyperledger Fabric network end-to-end:
#   1. generate crypto material via the Fabric CAs (or cryptogen)
#   2. bring up orderer + peers + CouchDB
#   3. create the `inec-results` channel and join both peers
#   4. package, install, approve and commit the `election` chaincode
#
# Requirements (install once, see README.md):
#   - docker + docker compose
#   - Fabric binaries on PATH: peer, orderer, configtxgen, cryptogen, fabric-ca-client
#     (fabric-samples install-fabric.sh installs them + docker images)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"

CHANNEL="inec-results"
CC_NAME="election"
CC_VERSION="1.0"
CC_SEQUENCE="1"
CC_SRC="chaincode/election"

command -v cryptogen  >/dev/null || { echo "ERROR: Fabric binaries not on PATH (peer, cryptogen, configtxgen). See README.md"; exit 1; }
command -v configtxgen >/dev/null || { echo "ERROR: configtxgen not on PATH."; exit 1; }

echo "==> [1/4] Generating crypto material"
cryptogen generate --config=./config/crypto-config.yaml --output=./organizations

echo "==> [2/4] Generating channel genesis block"
export FABRIC_CFG_PATH="$HERE/config"
configtxgen -profile INECResultsChannel -outputBlock ./channel-artifacts/${CHANNEL}.block -channelID ${CHANNEL}

echo "==> [3/4] Starting network"
docker compose up -d
sleep 8

echo "==> [3b] Creating channel ${CHANNEL} and joining peers"
export CORE_PEER_TLS_ENABLED=true
osnadmin channel join --channelID ${CHANNEL} \
  --config-block ./channel-artifacts/${CHANNEL}.block \
  -o localhost:7053 \
  --ca-file "$(pwd)/organizations/ordererOrganizations/inec.gov.ng/orderers/orderer.inec.gov.ng/tls/ca.crt" \
  --client-cert "$(pwd)/organizations/ordererOrganizations/inec.gov.ng/orderers/orderer.inec.gov.ng/tls/server.crt" \
  --client-key "$(pwd)/organizations/ordererOrganizations/inec.gov.ng/orderers/orderer.inec.gov.ng/tls/server.key"

for ORG in inec observers; do
  source ./scripts/set-peer-env.sh "$ORG"
  peer channel join -b ./channel-artifacts/${CHANNEL}.block
done

echo "==> [4/4] Deploying chaincode ${CC_NAME}@${CC_VERSION}"
pushd "${CC_SRC}" >/dev/null && GO111MODULE=on go mod vendor && popd >/dev/null
peer lifecycle chaincode package ${CC_NAME}.tar.gz --path ${CC_SRC} --lang golang --label ${CC_NAME}_${CC_VERSION}

for ORG in inec observers; do
  source ./scripts/set-peer-env.sh "$ORG"
  peer lifecycle chaincode install ${CC_NAME}.tar.gz
done

PKG_ID=$(peer lifecycle chaincode queryinstalled | sed -n "s/^Package ID: \(${CC_NAME}_${CC_VERSION}:[a-f0-9]*\).*/\1/p" | head -1)
for ORG in inec observers; do
  source ./scripts/set-peer-env.sh "$ORG"
  peer lifecycle chaincode approveformyorg -o localhost:7050 --channelID ${CHANNEL} \
    --name ${CC_NAME} --version ${CC_VERSION} --package-id "${PKG_ID}" --sequence ${CC_SEQUENCE} --tls \
    --cafile "$(pwd)/organizations/ordererOrganizations/inec.gov.ng/orderers/orderer.inec.gov.ng/tls/ca.crt"
done

peer lifecycle chaincode commit -o localhost:7050 --channelID ${CHANNEL} \
  --name ${CC_NAME} --version ${CC_VERSION} --sequence ${CC_SEQUENCE} --tls \
  --cafile "$(pwd)/organizations/ordererOrganizations/inec.gov.ng/orderers/orderer.inec.gov.ng/tls/ca.crt" \
  --peerAddresses localhost:7051 --peerAddresses localhost:9051

echo "==> Fabric network up. Chaincode '${CC_NAME}' committed to '${CHANNEL}'."
echo "    Point the Go backend at it with:"
echo "      export FABRIC_GATEWAY_ENDPOINT=localhost:7051"
echo "      export FABRIC_CHANNEL=${CHANNEL} FABRIC_CHAINCODE=${CC_NAME} FABRIC_MSP_ID=INECMSP"
