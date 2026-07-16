package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type WorkflowInput struct {
	WorkflowID   string                 `json:"workflow_id"`
	WorkflowType string                 `json:"workflow_type"`
	TaskQueue    string                 `json:"task_queue"`
	Input        map[string]interface{} `json:"input"`
	RetryPolicy  *RetryPolicy           `json:"retry_policy,omitempty"`
}

// RetryPolicy defines how workflow activities should be retried on failure.
type RetryPolicy struct {
	MaxAttempts        int           `json:"max_attempts"`
	InitialInterval    time.Duration `json:"initial_interval"`
	BackoffCoefficient float64       `json:"backoff_coefficient"`
	MaxInterval        time.Duration `json:"max_interval"`
}

// DefaultRetryPolicy for election workflows — limited retries with exponential backoff.
var DefaultRetryPolicy = &RetryPolicy{
	MaxAttempts:        3,
	InitialInterval:   time.Second,
	BackoffCoefficient: 2.0,
	MaxInterval:        30 * time.Second,
}

type WorkflowStatus struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	Result     string `json:"result,omitempty"`
	StartedAt  string `json:"started_at"`
	ClosedAt   string `json:"closed_at,omitempty"`
}

type TemporalClient interface {
	StartWorkflow(ctx context.Context, input WorkflowInput) (*WorkflowStatus, error)
	GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowStatus, error)
	SignalWorkflow(ctx context.Context, workflowID, signalName string, data interface{}) error
	CancelWorkflow(ctx context.Context, workflowID string) error
	Status() MWStatus
	Close() error
}

type temporalHTTPClient struct {
	baseURL string
	client  *ResilientHTTPClient
}

func (t *temporalHTTPClient) StartWorkflow(ctx context.Context, input WorkflowInput) (*WorkflowStatus, error) {
	body, _ := json.Marshal(input)
	req, _ := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/api/v1/namespaces/default/workflows", jsonReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var ws WorkflowStatus
	json.NewDecoder(resp.Body).Decode(&ws)
	return &ws, nil
}

func (t *temporalHTTPClient) GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowStatus, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/api/v1/namespaces/default/workflows/"+workflowID, nil)
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var ws WorkflowStatus
	json.NewDecoder(resp.Body).Decode(&ws)
	return &ws, nil
}

func (t *temporalHTTPClient) SignalWorkflow(ctx context.Context, workflowID, signalName string, data interface{}) error {
	body, _ := json.Marshal(map[string]interface{}{"signal_name": signalName, "input": data})
	req, _ := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/api/v1/namespaces/default/workflows/"+workflowID+"/signal", jsonReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (t *temporalHTTPClient) CancelWorkflow(ctx context.Context, workflowID string) error {
	req, _ := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/api/v1/namespaces/default/workflows/"+workflowID+"/cancel", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (t *temporalHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/api/v1/namespaces", nil)
	lat, err := measureLatency(func() error {
		resp, e := t.client.Client.Do(req)
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "Temporal", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Temporal", Connected: true, Mode: "external", Latency: fmtLatency(lat)}
}

func (t *temporalHTTPClient) Close() error { return nil }

type embeddedTemporal struct {
	mu        sync.RWMutex
	workflows map[string]*WorkflowStatus
	signals   map[string][]interface{}
}

func newEmbeddedTemporal() *embeddedTemporal {
	return &embeddedTemporal{
		workflows: make(map[string]*WorkflowStatus),
		signals:   make(map[string][]interface{}),
	}
}

func (t *embeddedTemporal) StartWorkflow(_ context.Context, input WorkflowInput) (*WorkflowStatus, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	runID := fmt.Sprintf("run-%d", time.Now().UnixNano())
	ws := &WorkflowStatus{
		WorkflowID: input.WorkflowID,
		RunID:      runID,
		Status:     "RUNNING",
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	t.workflows[input.WorkflowID] = ws

	go t.executeWorkflow(input)
	return ws, nil
}

func (t *embeddedTemporal) executeWorkflow(input WorkflowInput) {
	switch input.WorkflowType {
	case "ResultSubmissionWorkflow":
		t.runResultSubmissionWorkflow(input)
	case "ResultValidationWorkflow":
		t.runResultValidationWorkflow(input)
	case "ResultFinalizationWorkflow":
		t.runResultFinalizationWorkflow(input)
	default:
		t.completeWorkflow(input.WorkflowID, "COMPLETED", "unknown workflow type")
	}
}

func (t *embeddedTemporal) runResultSubmissionWorkflow(input WorkflowInput) {
	phases := []string{"pre_validation", "edge_validation", "submission", "acknowledgement"}
	for _, phase := range phases {
		time.Sleep(50 * time.Millisecond)
		t.mu.Lock()
		if ws, ok := t.workflows[input.WorkflowID]; ok {
			ws.Result = fmt.Sprintf("phase:%s", phase)
		}
		t.mu.Unlock()
	}
	t.completeWorkflow(input.WorkflowID, "COMPLETED", "result_submitted")
}

func (t *embeddedTemporal) runResultValidationWorkflow(input WorkflowInput) {
	phases := []string{"vote_count_check", "cross_reference", "officer_verification", "validation_complete"}
	for _, phase := range phases {
		time.Sleep(50 * time.Millisecond)
		t.mu.Lock()
		if ws, ok := t.workflows[input.WorkflowID]; ok {
			ws.Result = fmt.Sprintf("phase:%s", phase)
		}
		t.mu.Unlock()
	}
	t.completeWorkflow(input.WorkflowID, "COMPLETED", "result_validated")
}

func (t *embeddedTemporal) runResultFinalizationWorkflow(input WorkflowInput) {
	phases := []string{"ledger_posting", "blockchain_commit", "ipfs_archive", "finalization_complete"}
	for _, phase := range phases {
		time.Sleep(50 * time.Millisecond)
		t.mu.Lock()
		if ws, ok := t.workflows[input.WorkflowID]; ok {
			ws.Result = fmt.Sprintf("phase:%s", phase)
		}
		t.mu.Unlock()
	}
	t.completeWorkflow(input.WorkflowID, "COMPLETED", "result_finalized")
}

func (t *embeddedTemporal) completeWorkflow(workflowID, status, result string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ws, ok := t.workflows[workflowID]; ok {
		ws.Status = status
		ws.Result = result
		ws.ClosedAt = time.Now().UTC().Format(time.RFC3339)
	}
}

func (t *embeddedTemporal) GetWorkflowStatus(_ context.Context, workflowID string) (*WorkflowStatus, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ws, ok := t.workflows[workflowID]
	if !ok {
		return nil, fmt.Errorf("workflow not found: %s", workflowID)
	}
	return ws, nil
}

func (t *embeddedTemporal) SignalWorkflow(_ context.Context, workflowID, signalName string, data interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := workflowID + ":" + signalName
	t.signals[key] = append(t.signals[key], data)
	return nil
}

func (t *embeddedTemporal) CancelWorkflow(_ context.Context, workflowID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ws, ok := t.workflows[workflowID]; ok {
		ws.Status = "CANCELED"
		ws.ClosedAt = time.Now().UTC().Format(time.RFC3339)
		ws.Result = "canceled_with_compensation"
		log.Warn().Str("workflow_id", workflowID).Msg("temporal: workflow canceled, running compensation")
		return nil
	}
	return fmt.Errorf("workflow not found: %s", workflowID)
}

func (t *embeddedTemporal) Status() MWStatus {
	t.mu.RLock()
	wfCount := len(t.workflows)
	var running int
	for _, ws := range t.workflows {
		if ws.Status == "RUNNING" {
			running++
		}
	}
	t.mu.RUnlock()
	return MWStatus{
		Name: "Temporal", Connected: true, Mode: "embedded",
		Latency: "0.0ms",
		Details: fmt.Sprintf("local workflow engine, %d workflows (%d running)", wfCount, running),
	}
}

func (t *embeddedTemporal) Close() error { return nil }

func initTemporalClient() TemporalClient {
	// Prefer the real gRPC SDK client (+ in-process worker) when configured.
	if sdk := initTemporalSDKClient(); sdk != nil {
		return sdk
	}
	temporalURL := envOrDefault("TEMPORAL_URL", "")
	if temporalURL != "" {
		client := &temporalHTTPClient{
			baseURL: temporalURL,
			client:  NewResilientHTTPClient("temporal"),
		}
		s := client.Status()
		if s.Connected {
			log.Info().Str("url", temporalURL).Msg("Temporal connected")
			return client
		}
		log.Warn().Msg("Temporal unreachable, falling back to embedded")
	}
	env := os.Getenv("APP_ENV")
	if env == "production" || env == "staging" {
		log.Fatal().Msg("Temporal is REQUIRED in production/staging for durable workflow orchestration. Set TEMPORAL_URL")
	}
	log.Warn().Msg("Temporal using embedded local workflow engine (DEV ONLY)")
	return newEmbeddedTemporal()
}
