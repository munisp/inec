package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// DaprClient is implemented exclusively by the Dapr sidecar HTTP client. State,
// pub/sub, and service invocation must retain Dapr's component semantics and
// cannot silently be emulated through PostgreSQL or in-process handlers.
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

type daprHTTPClient struct {
	baseURL string
	client  *ResilientHTTPClient
}

// unavailableDaprClient never stores, synthesizes, or substitutes Dapr operations.
// It preserves the dependency error for callers until the sidecar is configured.
type unavailableDaprClient struct {
	reason string
}

func (d *unavailableDaprClient) unavailable() error {
	return fmt.Errorf("Dapr sidecar is unavailable: %s", d.reason)
}
func (d *unavailableDaprClient) PublishEvent(context.Context, string, string, interface{}) error {
	return d.unavailable()
}
func (d *unavailableDaprClient) InvokeService(context.Context, string, string, interface{}) ([]byte, error) {
	return nil, d.unavailable()
}
func (d *unavailableDaprClient) InvokeServiceValidated(context.Context, string, string, interface{}, DaprResponseSchema) ([]byte, *DaprValidationResult, error) {
	return nil, nil, d.unavailable()
}
func (d *unavailableDaprClient) GetState(context.Context, string, string) ([]byte, error) {
	return nil, d.unavailable()
}
func (d *unavailableDaprClient) SaveState(context.Context, string, string, interface{}) error {
	return d.unavailable()
}
func (d *unavailableDaprClient) DeleteState(context.Context, string, string) error {
	return d.unavailable()
}
func (d *unavailableDaprClient) Status() MWStatus {
	return MWStatus{Name: "Dapr", Connected: false, Mode: "Dapr sidecar required", Details: d.reason}
}
func (d *unavailableDaprClient) Close() error { return nil }

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
		log.Warn().Msg("Dapr unavailable: set DAPR_HTTP_URL or DAPR_HTTP_PORT and run the Dapr sidecar")
		return &unavailableDaprClient{reason: "DAPR_HTTP_URL or DAPR_HTTP_PORT must be configured"}
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
