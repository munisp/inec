package main

// Real Hyperledger Fabric Gateway client.
//
// When FABRIC_GATEWAY_ENDPOINT is configured, election transactions are
// submitted to the live Fabric network defined under blockchain/fabric/ via the
// official fabric-gateway SDK over gRPC/TLS. The private key never leaves the
// signing identity's MSP, and the returned transaction ID is the on-ledger ID.
//
// If the gateway is not configured (or a connection cannot be established), the
// caller falls back to the PostgreSQL-backed ledger so the platform stays
// operational in single-node / CI environments.
//
// Configuration (environment):
//   FABRIC_GATEWAY_ENDPOINT  peer gateway address, e.g. localhost:7051
//   FABRIC_MSP_ID            signing identity MSP, e.g. INECMSP
//   FABRIC_CERT_PATH         PEM path to the identity's signing certificate
//   FABRIC_KEY_PATH          PEM path to the identity's private key
//   FABRIC_TLS_CERT_PATH     PEM path to the peer's TLS CA certificate
//   FABRIC_GATEWAY_SNI       TLS server-name override (peer host), optional

import (
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/hyperledger/fabric-gateway/pkg/client"
	"github.com/hyperledger/fabric-gateway/pkg/identity"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type FabricGatewayClient struct {
	gw   *client.Gateway
	conn *grpc.ClientConn
}

// NewFabricGatewayClient dials the peer gateway and constructs an authenticated
// SDK gateway. Returns (nil, nil) when the gateway is not configured.
func NewFabricGatewayClient() (*FabricGatewayClient, error) {
	endpoint := os.Getenv("FABRIC_GATEWAY_ENDPOINT")
	if endpoint == "" {
		return nil, nil
	}
	mspID := envOrDefault("FABRIC_MSP_ID", "INECMSP")
	certPath := os.Getenv("FABRIC_CERT_PATH")
	keyPath := os.Getenv("FABRIC_KEY_PATH")
	tlsPath := os.Getenv("FABRIC_TLS_CERT_PATH")
	sni := os.Getenv("FABRIC_GATEWAY_SNI")

	tlsPEM, err := os.ReadFile(tlsPath)
	if err != nil {
		return nil, fmt.Errorf("read TLS cert %q: %w", tlsPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(tlsPEM) {
		return nil, fmt.Errorf("invalid TLS CA cert %q", tlsPath)
	}
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(pool, sni)))
	if err != nil {
		return nil, fmt.Errorf("grpc dial %q: %w", endpoint, err)
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read signing cert %q: %w", certPath, err)
	}
	cert, err := identity.CertificateFromPEM(certPEM)
	if err != nil {
		conn.Close()
		return nil, err
	}
	id, err := identity.NewX509Identity(mspID, cert)
	if err != nil {
		conn.Close()
		return nil, err
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read signing key %q: %w", keyPath, err)
	}
	privKey, err := identity.PrivateKeyFromPEM(keyPEM)
	if err != nil {
		conn.Close()
		return nil, err
	}
	sign, err := identity.NewPrivateKeySign(privKey)
	if err != nil {
		conn.Close()
		return nil, err
	}

	gw, err := client.Connect(id,
		client.WithSign(sign),
		client.WithClientConnection(conn),
		client.WithEvaluateTimeout(10*time.Second),
		client.WithEndorseTimeout(30*time.Second),
		client.WithSubmitTimeout(30*time.Second),
		client.WithCommitStatusTimeout(1*time.Minute),
	)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("gateway connect: %w", err)
	}

	log.Info().Str("endpoint", endpoint).Str("msp", mspID).Msg("Fabric Gateway connected")
	return &FabricGatewayClient{gw: gw, conn: conn}, nil
}

// Submit endorses and commits a transaction on the given channel/chaincode and
// returns the committed on-ledger transaction ID.
func (c *FabricGatewayClient) Submit(channelID, chaincodeID, function string, args []string) (string, error) {
	network := c.gw.GetNetwork(channelID)
	contract := network.GetContract(chaincodeID)

	proposal, err := contract.NewProposal(function, client.WithArguments(args...))
	if err != nil {
		return "", err
	}
	txn, err := proposal.Endorse()
	if err != nil {
		return "", fmt.Errorf("endorse: %w", err)
	}
	commit, err := txn.Submit()
	if err != nil {
		return "", fmt.Errorf("submit: %w", err)
	}
	status, err := commit.Status()
	if err != nil {
		return "", fmt.Errorf("commit status: %w", err)
	}
	if !status.Successful {
		return "", fmt.Errorf("transaction %s failed validation: code %d", commit.TransactionID(), int32(status.Code))
	}
	return commit.TransactionID(), nil
}

// Evaluate runs a read-only query against the chaincode.
func (c *FabricGatewayClient) Evaluate(channelID, chaincodeID, function string, args []string) ([]byte, error) {
	network := c.gw.GetNetwork(channelID)
	contract := network.GetContract(chaincodeID)
	return contract.EvaluateTransaction(function, args...)
}

func (c *FabricGatewayClient) Close() {
	if c.gw != nil {
		c.gw.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}
