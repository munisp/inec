package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// DaprResponseSchema defines expected fields for validating service invocation responses.
type DaprResponseSchema struct {
	RequiredFields []string `json:"required_fields"`
	AllowExtra     bool     `json:"allow_extra"`
}

// DaprValidationResult captures the outcome of response schema validation.
type DaprValidationResult struct {
	Valid         bool     `json:"valid"`
	MissingFields []string `json:"missing_fields,omitempty"`
	ExtraFields   []string `json:"extra_fields,omitempty"`
}

// DaprClient is implemented by the configured Dapr sidecar in production. Test
// and development runtimes may use the explicit in-memory implementation below
// for state and event paths; service invocation remains unavailable without a
// real sidecar and APP_ENV=production never selects the local implementation.
type DaprClient interface {
	PublishEvent(ctx context.Context, pubsub, topic string, data interface{}) error
	InvokeService(ctx context.Context, appID, method string, data interface{}) ([]byte, error)
	InvokeServiceValidated(ctx context.Context, appID, method string, data interface{}, schema DaprResponseSchema) ([]byte, *DaprValidationResult, error)
	GetState(ctx context.Context, store, key string) ([]byte, error)
	SaveState(ctx context.Context, store, key string, value interface{}) error
	DeleteState(ctx context.Context, store, key string) error
	Status() MWStatus
	Close() error
}

// localDaprClient is an explicit non-production transport for isolated tests
// and local development. It intentionally does not emulate cross-service
// invocation: callers receive an error until a real Dapr sidecar is configured.
type localDaprClient struct {
	mu    sync.RWMutex
	state map[string][]byte
}

func newLocalDaprClient() *localDaprClient {
	return &localDaprClient{state: make(map[string][]byte)}
}

func localDaprStateKey(store, key string) string {
	return store + "\x00" + key
}

func (d *localDaprClient) PublishEvent(ctx context.Context, pubsub, topic string, data interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(pubsub) == "" || strings.TrimSpace(topic) == "" {
		return fmt.Errorf("pubsub and topic are required for a local Dapr event")
	}
	if _, err := json.Marshal(data); err != nil {
		return fmt.Errorf("marshal local Dapr event: %w", err)
	}
	return nil
}

func (d *localDaprClient) InvokeService(ctx context.Context, appID, method string, data interface{}) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("Dapr service invocation is unavailable without a configured sidecar")
}

func (d *localDaprClient) InvokeServiceValidated(ctx context.Context, appID, method string, data interface{}, schema DaprResponseSchema) ([]byte, *DaprValidationResult, error) {
	raw, err := d.InvokeService(ctx, appID, method, data)
	if err != nil {
		return nil, nil, err
	}
	return raw, validateDaprResponse(raw, schema), nil
}

func (d *localDaprClient) GetState(ctx context.Context, store, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.RLock()
	value := append([]byte(nil), d.state[localDaprStateKey(store, key)]...)
	d.mu.RUnlock()
	return value, nil
}

func (d *localDaprClient) SaveState(ctx context.Context, store, key string, value interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(store) == "" || strings.TrimSpace(key) == "" {
		return fmt.Errorf("store and key are required for local Dapr state")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal local Dapr state: %w", err)
	}
	d.mu.Lock()
	d.state[localDaprStateKey(store, key)] = append([]byte(nil), raw...)
	d.mu.Unlock()
	return nil
}

func (d *localDaprClient) DeleteState(ctx context.Context, store, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	delete(d.state, localDaprStateKey(store, key))
	d.mu.Unlock()
	return nil
}

func (d *localDaprClient) Status() MWStatus {
	return MWStatus{Name: "Dapr", Connected: true, Mode: "in-memory (non-production)", Details: "ephemeral local state and event transport"}
}

func (d *localDaprClient) Close() error { return nil }

type daprHTTPClient struct {
	baseURL string
	client  *ResilientHTTPClient
}

func (d *daprHTTPClient) do(ctx context.Context, method, url string, payload []byte) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create Dapr request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read Dapr response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Dapr %s %s returned %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func (d *daprHTTPClient) PublishEvent(ctx context.Context, pubsub, topic string, data interface{}) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal Dapr event: %w", err)
	}
	_, err = d.do(ctx, http.MethodPost, fmt.Sprintf("%s/v1.0/publish/%s/%s", d.baseURL, pubsub, topic), body)
	return err
}

func (d *daprHTTPClient) InvokeService(ctx context.Context, appID, method string, data interface{}) ([]byte, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal Dapr invocation: %w", err)
	}
	return d.do(ctx, http.MethodPost, fmt.Sprintf("%s/v1.0/invoke/%s/method/%s", d.baseURL, appID, method), body)
}

func (d *daprHTTPClient) GetState(ctx context.Context, store, key string) ([]byte, error) {
	return d.do(ctx, http.MethodGet, fmt.Sprintf("%s/v1.0/state/%s/%s", d.baseURL, store, key), nil)
}

func (d *daprHTTPClient) SaveState(ctx context.Context, store, key string, value interface{}) error {
	body, err := json.Marshal([]map[string]interface{}{{"key": key, "value": value}})
	if err != nil {
		return fmt.Errorf("marshal Dapr state: %w", err)
	}
	_, err = d.do(ctx, http.MethodPost, fmt.Sprintf("%s/v1.0/state/%s", d.baseURL, store), body)
	return err
}

func (d *daprHTTPClient) DeleteState(ctx context.Context, store, key string) error {
	_, err := d.do(ctx, http.MethodDelete, fmt.Sprintf("%s/v1.0/state/%s/%s", d.baseURL, store, key), nil)
	return err
}

func (d *daprHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+"/v1.0/healthz", nil)
	if err != nil {
		return MWStatus{Name: "Dapr", Connected: false, Mode: "sidecar", Details: err.Error()}
	}
	lat, err := measureLatency(func() error {
		resp, e := d.client.Client.Do(req)
		if e != nil {
			return e
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent && (resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices) {
			return fmt.Errorf("health endpoint returned %d", resp.StatusCode)
		}
		return nil
	})
	if err != nil {
		return MWStatus{Name: "Dapr", Connected: false, Mode: "sidecar", Details: err.Error()}
	}
	return MWStatus{Name: "Dapr", Connected: true, Mode: "sidecar", Latency: fmtLatency(lat)}
}

func (d *daprHTTPClient) InvokeServiceValidated(ctx context.Context, appID, method string, data interface{}, schema DaprResponseSchema) ([]byte, *DaprValidationResult, error) {
	raw, err := d.InvokeService(ctx, appID, method, data)
	if err != nil {
		return nil, nil, err
	}
	result := validateDaprResponse(raw, schema)
	return raw, result, nil
}

func (d *daprHTTPClient) Close() error { return nil }

// validateDaprResponse checks a JSON response against the given schema.
func validateDaprResponse(raw []byte, schema DaprResponseSchema) *DaprValidationResult {
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return &DaprValidationResult{Valid: false, MissingFields: schema.RequiredFields}
	}
	result := &DaprValidationResult{Valid: true}
	for _, field := range schema.RequiredFields {
		if _, ok := parsed[field]; !ok {
			result.MissingFields = append(result.MissingFields, field)
			result.Valid = false
		}
	}
	if !schema.AllowExtra {
		requiredSet := make(map[string]bool, len(schema.RequiredFields))
		for _, field := range schema.RequiredFields {
			requiredSet[field] = true
		}
		for key := range parsed {
			if !requiredSet[key] {
				result.ExtraFields = append(result.ExtraFields, key)
			}
		}
	}
	return result
}

func initDaprClient() DaprClient {
	daprURL := strings.TrimRight(strings.TrimSpace(envOrDefault("DAPR_HTTP_URL", "")), "/")
	if daprURL == "" {
		daprPort := strings.TrimSpace(envOrDefault("DAPR_HTTP_PORT", ""))
		if daprPort != "" {
			daprURL = "http://localhost:" + daprPort
		}
	}
	if daprURL == "" {
		isProduction := strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
		if !isProduction {
			log.Warn().Msg("Dapr is not configured — using the explicit in-memory non-production transport")
			return newLocalDaprClient()
		}
		log.Fatal().Msg("Dapr is required: set DAPR_HTTP_URL or DAPR_HTTP_PORT and run the Dapr sidecar")
	}

	client := &daprHTTPClient{baseURL: daprURL, client: NewResilientHTTPClient("dapr")}
	status := client.Status()
	if !status.Connected {
		// Compose launches a same-network Dapr sidecar after the application
		// process exists. Keep the native client only; readiness and all Dapr
		// operations remain unavailable until the real sidecar becomes healthy.
		log.Warn().Str("url", daprURL).Str("details", status.Details).Msg("Dapr sidecar is not ready yet; no fallback is available")
		return client
	}
	log.Info().Str("url", daprURL).Msg("Dapr sidecar connected")
	return client
}
