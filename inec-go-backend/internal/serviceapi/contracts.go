// Package serviceapi defines typed inter-service communication contracts.
// Each service exposes a Go client that speaks HTTP to the target service,
// enabling both in-process (monolith) and distributed (microservice) modes.
package serviceapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ServiceClient is the base HTTP client for inter-service communication.
type ServiceClient struct {
	BaseURL    string
	HTTPClient *http.Client
	ServiceName string
}

// NewServiceClient creates a client for a downstream service.
func NewServiceClient(name, baseURL string) *ServiceClient {
	return &ServiceClient{
		ServiceName: name,
		BaseURL:     baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Get performs a GET request to the service.
func (c *ServiceClient) Get(ctx context.Context, path string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("%s: request creation failed: %w", c.ServiceName, err)
	}
	req.Header.Set("X-Service-Name", c.ServiceName)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: request failed: %w", c.ServiceName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s: returned %d", c.ServiceName, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

// Health checks the service health endpoint.
func (c *ServiceClient) Health(ctx context.Context) error {
	var result map[string]interface{}
	return c.Get(ctx, "/health", &result)
}

// --- Auth Service Client ---

// AuthClient communicates with the auth service.
type AuthClient struct {
	*ServiceClient
}

// NewAuthClient creates an auth service client.
func NewAuthClient(baseURL string) *AuthClient {
	return &AuthClient{NewServiceClient("auth-svc", baseURL)}
}

// ValidateToken asks the auth service to validate a token.
func (c *AuthClient) ValidateToken(ctx context.Context, token string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/auth/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token invalid: %d", resp.StatusCode)
	}
	var claims map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&claims)
	return claims, nil
}

// --- Election Service Client ---

// ElectionClient communicates with the election service.
type ElectionClient struct {
	*ServiceClient
}

// NewElectionClient creates an election service client.
func NewElectionClient(baseURL string) *ElectionClient {
	return &ElectionClient{NewServiceClient("election-svc", baseURL)}
}

// ListElections fetches all elections.
func (c *ElectionClient) ListElections(ctx context.Context) ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	err := c.Get(ctx, "/elections", &result)
	return result, err
}

// --- Biometric Service Client ---

// BiometricClient communicates with the biometric service.
type BiometricClient struct {
	*ServiceClient
}

// NewBiometricClient creates a biometric service client.
func NewBiometricClient(baseURL string) *BiometricClient {
	return &BiometricClient{NewServiceClient("biometric-svc", baseURL)}
}

// --- Geo Service Client ---

// GeoClient communicates with the geo service.
type GeoClient struct {
	*ServiceClient
}

// NewGeoClient creates a geo service client.
func NewGeoClient(baseURL string) *GeoClient {
	return &GeoClient{NewServiceClient("geo-svc", baseURL)}
}

// --- Compliance Service Client ---

// ComplianceClient communicates with the compliance service.
type ComplianceClient struct {
	*ServiceClient
}

// NewComplianceClient creates a compliance service client.
func NewComplianceClient(baseURL string) *ComplianceClient {
	return &ComplianceClient{NewServiceClient("compliance-svc", baseURL)}
}

// --- Ingestion Service Client ---

// IngestionClient communicates with the ingestion service.
type IngestionClient struct {
	*ServiceClient
}

// NewIngestionClient creates an ingestion service client.
func NewIngestionClient(baseURL string) *IngestionClient {
	return &IngestionClient{NewServiceClient("ingestion-svc", baseURL)}
}

// --- BVAS Service Client ---

// BVASClient communicates with the BVAS service.
type BVASClient struct {
	*ServiceClient
}

// NewBVASClient creates a BVAS service client.
func NewBVASClient(baseURL string) *BVASClient {
	return &BVASClient{NewServiceClient("bvas-svc", baseURL)}
}

// --- Rust Inference Engine Client ---

// InferenceClient communicates with the Rust inference engine.
type InferenceClient struct {
	*ServiceClient
}

// NewInferenceClient creates an inference engine client.
func NewInferenceClient(baseURL string) *InferenceClient {
	return &InferenceClient{NewServiceClient("inference-engine", baseURL)}
}

// Predict sends data to the inference engine for ML prediction.
func (c *InferenceClient) Predict(ctx context.Context, modelName string, input interface{}) (map[string]interface{}, error) {
	// The Rust inference engine exposes /predict endpoint
	var result map[string]interface{}
	err := c.Get(ctx, fmt.Sprintf("/models/%s/predict", modelName), &result)
	return result, err
}

// --- Python Analytics Client ---

// AnalyticsClient communicates with the Python lakehouse-analytics service.
type AnalyticsClient struct {
	*ServiceClient
}

// NewAnalyticsClient creates an analytics service client.
func NewAnalyticsClient(baseURL string) *AnalyticsClient {
	return &AnalyticsClient{NewServiceClient("lakehouse-analytics", baseURL)}
}

// Query executes an analytics query.
func (c *AnalyticsClient) Query(ctx context.Context, queryName string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Get(ctx, fmt.Sprintf("/analytics/%s", queryName), &result)
	return result, err
}
