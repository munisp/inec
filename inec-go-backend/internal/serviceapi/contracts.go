// Package serviceapi defines typed inter-service communication contracts.
// Each service exposes a Go client that speaks HTTP to the target service,
// enabling both in-process (monolith) and distributed (microservice) modes.
package serviceapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ServiceClient is the base HTTP client for inter-service communication.
type ServiceClient struct {
	BaseURL     string
	HTTPClient  *http.Client
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

// Post performs a POST request with a JSON body.
func (c *ServiceClient) Post(ctx context.Context, path string, body interface{}, result interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%s: marshal failed: %w", c.ServiceName, err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: request creation failed: %w", c.ServiceName, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Name", c.ServiceName)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: request failed: %w", c.ServiceName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s: returned %d", c.ServiceName, resp.StatusCode)
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// Put performs a PUT request with a JSON body.
func (c *ServiceClient) Put(ctx context.Context, path string, body interface{}, result interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%s: marshal failed: %w", c.ServiceName, err)
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: request creation failed: %w", c.ServiceName, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Name", c.ServiceName)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: request failed: %w", c.ServiceName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s: returned %d", c.ServiceName, resp.StatusCode)
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// Delete performs a DELETE request.
func (c *ServiceClient) Delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.BaseURL+path, nil)
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
	return nil
}

// Health checks the service health endpoint.
func (c *ServiceClient) Health(ctx context.Context) error {
	var result map[string]interface{}
	return c.Get(ctx, "/health", &result)
}

// --- Auth Service Client ---

type AuthClient struct{ *ServiceClient }

func NewAuthClient(baseURL string) *AuthClient {
	return &AuthClient{NewServiceClient("auth-svc", baseURL)}
}

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

func (c *AuthClient) Login(ctx context.Context, username, password string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/auth/login", map[string]string{"username": username, "password": password}, &result)
	return result, err
}

func (c *AuthClient) SetupMFA(ctx context.Context, userID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/auth/mfa/setup", map[string]string{"user_id": userID}, &result)
	return result, err
}

func (c *AuthClient) VerifyMFA(ctx context.Context, userID, code string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/auth/mfa/verify", map[string]string{"user_id": userID, "code": code}, &result)
	return result, err
}

// --- Election Service Client ---

type ElectionClient struct{ *ServiceClient }

func NewElectionClient(baseURL string) *ElectionClient {
	return &ElectionClient{NewServiceClient("election-svc", baseURL)}
}

func (c *ElectionClient) ListElections(ctx context.Context) ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	err := c.Get(ctx, "/elections", &result)
	return result, err
}

func (c *ElectionClient) GetElection(ctx context.Context, id int) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Get(ctx, fmt.Sprintf("/elections/%d", id), &result)
	return result, err
}

func (c *ElectionClient) CreateElection(ctx context.Context, data map[string]string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/elections", data, &result)
	return result, err
}

func (c *ElectionClient) SubmitResult(ctx context.Context, data map[string]interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/results/submit", data, &result)
	return result, err
}

func (c *ElectionClient) TransitionState(ctx context.Context, electionID int, newState string) error {
	return c.Post(ctx, fmt.Sprintf("/elections/%d/transition", electionID), map[string]string{"state": newState}, nil)
}

// --- Biometric Service Client ---

type BiometricClient struct{ *ServiceClient }

func NewBiometricClient(baseURL string) *BiometricClient {
	return &BiometricClient{NewServiceClient("biometric-svc", baseURL)}
}

func (c *BiometricClient) Verify(ctx context.Context, voterID string, template []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/biometric/verify", map[string]interface{}{
		"voter_id": voterID,
		"template": template,
	}, &result)
	return result, err
}

func (c *BiometricClient) Enroll(ctx context.Context, voterID string, template []byte) error {
	return c.Post(ctx, "/biometric/enroll", map[string]interface{}{
		"voter_id": voterID,
		"template": template,
	}, nil)
}

// --- Geo Service Client ---

type GeoClient struct{ *ServiceClient }

func NewGeoClient(baseURL string) *GeoClient {
	return &GeoClient{NewServiceClient("geo-svc", baseURL)}
}

func (c *GeoClient) ValidateGeofence(ctx context.Context, lat, lng float64, pollingUnitID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/geo/validate-geofence", map[string]interface{}{
		"latitude":        lat,
		"longitude":       lng,
		"polling_unit_id": pollingUnitID,
	}, &result)
	return result, err
}

func (c *GeoClient) NearestPollingUnit(ctx context.Context, lat, lng float64) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Get(ctx, fmt.Sprintf("/geo/nearest?lat=%f&lng=%f", lat, lng), &result)
	return result, err
}

// --- Compliance Service Client ---

type ComplianceClient struct{ *ServiceClient }

func NewComplianceClient(baseURL string) *ComplianceClient {
	return &ComplianceClient{NewServiceClient("compliance-svc", baseURL)}
}

func (c *ComplianceClient) RecordConsent(ctx context.Context, voterID, purpose string) error {
	return c.Post(ctx, "/compliance/consent", map[string]string{
		"voter_id": voterID,
		"purpose":  purpose,
	}, nil)
}

func (c *ComplianceClient) ProcessDSR(ctx context.Context, requestType, voterID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/compliance/dsr", map[string]string{
		"request_type": requestType,
		"voter_id":     voterID,
	}, &result)
	return result, err
}

func (c *ComplianceClient) GetProcessingRegister(ctx context.Context) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Get(ctx, "/compliance/ndpr/processing-register", &result)
	return result, err
}

// --- Ingestion Service Client ---

type IngestionClient struct{ *ServiceClient }

func NewIngestionClient(baseURL string) *IngestionClient {
	return &IngestionClient{NewServiceClient("ingestion-svc", baseURL)}
}

func (c *IngestionClient) Ingest(ctx context.Context, data map[string]interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/ingestion/submit", data, &result)
	return result, err
}

func (c *IngestionClient) GetQueueStatus(ctx context.Context) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Get(ctx, "/ingestion/queue/status", &result)
	return result, err
}

// --- BVAS Service Client ---

type BVASClient struct{ *ServiceClient }

func NewBVASClient(baseURL string) *BVASClient {
	return &BVASClient{NewServiceClient("bvas-svc", baseURL)}
}

func (c *BVASClient) AccreditVoter(ctx context.Context, deviceID, voterID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/bvas/accredit", map[string]string{
		"device_id": deviceID,
		"voter_id":  voterID,
	}, &result)
	return result, err
}

func (c *BVASClient) GetDeviceStatus(ctx context.Context, deviceID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Get(ctx, fmt.Sprintf("/bvas/devices/%s", deviceID), &result)
	return result, err
}

// --- Rust Inference Engine Client ---

type InferenceClient struct{ *ServiceClient }

func NewInferenceClient(baseURL string) *InferenceClient {
	return &InferenceClient{NewServiceClient("inference-engine", baseURL)}
}

func (c *InferenceClient) Predict(ctx context.Context, modelName string, input interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, fmt.Sprintf("/models/%s/predict", modelName), input, &result)
	return result, err
}

func (c *InferenceClient) ListModels(ctx context.Context) ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	err := c.Get(ctx, "/models", &result)
	return result, err
}

// --- Python Analytics Client ---

type AnalyticsClient struct{ *ServiceClient }

func NewAnalyticsClient(baseURL string) *AnalyticsClient {
	return &AnalyticsClient{NewServiceClient("lakehouse-analytics", baseURL)}
}

func (c *AnalyticsClient) Query(ctx context.Context, queryName string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Get(ctx, fmt.Sprintf("/analytics/%s", queryName), &result)
	return result, err
}

func (c *AnalyticsClient) DetectAnomalies(ctx context.Context, electionID int) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/analytics/anomalies", map[string]int{"election_id": electionID}, &result)
	return result, err
}

func (c *AnalyticsClient) SpatialAnalysis(ctx context.Context, analysisType string, params map[string]interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	params["analysis_type"] = analysisType
	err := c.Post(ctx, "/spatial/analyze", params, &result)
	return result, err
}

// --- Python Document AI Client ---

type DocumentAIClient struct{ *ServiceClient }

func NewDocumentAIClient(baseURL string) *DocumentAIClient {
	return &DocumentAIClient{NewServiceClient("document-ai", baseURL)}
}

func (c *DocumentAIClient) ExtractForm(ctx context.Context, imageData []byte, formType string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/extract", map[string]interface{}{
		"image": imageData,
		"type":  formType,
	}, &result)
	return result, err
}

func (c *DocumentAIClient) VerifyDocument(ctx context.Context, docID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Get(ctx, fmt.Sprintf("/verify/%s", docID), &result)
	return result, err
}

// --- Rust Fluvio Stream Client ---

type FluvioStreamClient struct{ *ServiceClient }

func NewFluvioStreamClient(baseURL string) *FluvioStreamClient {
	return &FluvioStreamClient{NewServiceClient("fluvio-stream", baseURL)}
}

func (c *FluvioStreamClient) Produce(ctx context.Context, topic string, key string, value interface{}) error {
	return c.Post(ctx, "/produce", map[string]interface{}{
		"topic": topic,
		"key":   key,
		"value": value,
	}, nil)
}

func (c *FluvioStreamClient) GetTopicStats(ctx context.Context, topic string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Get(ctx, fmt.Sprintf("/topics/%s/stats", topic), &result)
	return result, err
}

// --- Middleware Service Client ---

type MiddlewareClient struct{ *ServiceClient }

func NewMiddlewareClient(baseURL string) *MiddlewareClient {
	return &MiddlewareClient{NewServiceClient("middleware-svc", baseURL)}
}

func (c *MiddlewareClient) PublishEvent(ctx context.Context, topic, key string, value interface{}) error {
	return c.Post(ctx, "/kafka/publish", map[string]interface{}{
		"topic": topic,
		"key":   key,
		"value": value,
	}, nil)
}

func (c *MiddlewareClient) CacheGet(ctx context.Context, key string) (interface{}, error) {
	var result map[string]interface{}
	err := c.Get(ctx, fmt.Sprintf("/cache/%s", key), &result)
	if err != nil {
		return nil, err
	}
	return result["value"], nil
}

func (c *MiddlewareClient) CacheSet(ctx context.Context, key string, value interface{}) error {
	return c.Put(ctx, fmt.Sprintf("/cache/%s", key), map[string]interface{}{"value": value}, nil)
}

func (c *MiddlewareClient) StartWorkflow(ctx context.Context, workflowID, workflowType string, input interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.Post(ctx, "/workflows/start", map[string]interface{}{
		"workflow_id":   workflowID,
		"workflow_type": workflowType,
		"input":         input,
	}, &result)
	return result, err
}

// --- Service Registry ---

// Registry holds clients for all services, used by gateway and monolith.
type Registry struct {
	Auth          *AuthClient
	Election      *ElectionClient
	Biometric     *BiometricClient
	Geo           *GeoClient
	Compliance    *ComplianceClient
	Ingestion     *IngestionClient
	BVAS          *BVASClient
	Inference     *InferenceClient
	Analytics     *AnalyticsClient
	DocumentAI    *DocumentAIClient
	FluvioStream  *FluvioStreamClient
	Middleware    *MiddlewareClient
}

// NewRegistry creates a complete service registry from service URLs.
func NewRegistry(urls map[string]string) *Registry {
	return &Registry{
		Auth:         NewAuthClient(urls["auth-svc"]),
		Election:     NewElectionClient(urls["election-svc"]),
		Biometric:    NewBiometricClient(urls["biometric-svc"]),
		Geo:          NewGeoClient(urls["geo-svc"]),
		Compliance:   NewComplianceClient(urls["compliance-svc"]),
		Ingestion:    NewIngestionClient(urls["ingestion-svc"]),
		BVAS:         NewBVASClient(urls["bvas-svc"]),
		Inference:    NewInferenceClient(urls["inference-engine"]),
		Analytics:    NewAnalyticsClient(urls["lakehouse-analytics"]),
		DocumentAI:   NewDocumentAIClient(urls["document-ai"]),
		FluvioStream: NewFluvioStreamClient(urls["fluvio-stream"]),
		Middleware:   NewMiddlewareClient(urls["middleware-svc"]),
	}
}

// DefaultURLs returns the default localhost URLs for all services.
func DefaultURLs() map[string]string {
	return map[string]string{
		"auth-svc":            "http://localhost:8090",
		"election-svc":        "http://localhost:8091",
		"biometric-svc":       "http://localhost:8092",
		"geo-svc":             "http://localhost:8093",
		"compliance-svc":      "http://localhost:8094",
		"ingestion-svc":       "http://localhost:8095",
		"bvas-svc":            "http://localhost:8096",
		"inference-engine":    "http://localhost:8097",
		"lakehouse-analytics": "http://localhost:8098",
		"document-ai":         "http://localhost:8099",
		"fluvio-stream":       "http://localhost:8100",
		"middleware-svc":      "http://localhost:8085",
	}
}
