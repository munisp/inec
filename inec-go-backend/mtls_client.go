package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog/log"
)

// MTLSConfig holds paths to TLS certificates for mutual TLS.
type MTLSConfig struct {
	CACertPath     string
	ClientCertPath string
	ClientKeyPath  string
	ServerName     string
}

// NewMTLSClient creates an HTTP client configured for mutual TLS authentication.
// This is used for all service-to-service calls within the INEC platform.
func NewMTLSClient(cfg MTLSConfig) (*http.Client, error) {
	// Load CA certificate pool
	caCert, err := os.ReadFile(cfg.CACertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to append CA cert to pool")
	}

	// Load client certificate and key
	cert, err := tls.LoadX509KeyPair(cfg.ClientCertPath, cfg.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client cert/key: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		ServerName:   cfg.ServerName,
		MinVersion:   tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}

	transport := &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	log.Info().Str("server_name", cfg.ServerName).Msg("mTLS client initialised")

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

// DefaultMTLSConfig returns mTLS configuration from standard environment variables.
// Certificate files are expected to be mounted via Kubernetes secrets or Docker volumes.
func DefaultMTLSConfig(serviceName string) MTLSConfig {
	return MTLSConfig{
		CACertPath:     getEnvOrDefault("MTLS_CA_CERT", "/certs/ca.crt"),
		ClientCertPath: getEnvOrDefault("MTLS_CLIENT_CERT", "/certs/client.crt"),
		ClientKeyPath:  getEnvOrDefault("MTLS_CLIENT_KEY", "/certs/client.key"),
		ServerName:     serviceName,
	}
}

// MTLSServerConfig returns a *tls.Config suitable for an mTLS server.
// It requires clients to present a valid certificate signed by the platform CA.
func MTLSServerConfig() (*tls.Config, error) {
	caCert, err := os.ReadFile(getEnvOrDefault("MTLS_CA_CERT", "/certs/ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert for server: %w", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to append CA cert to pool")
	}

	return &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  caCertPool,
		MinVersion: tls.VersionTLS13,
	}, nil
}
