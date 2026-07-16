package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// temporalSDKTaskQueue is the single task queue the in-process worker polls.
// All workflows are started here so the worker always picks them up regardless
// of the caller-requested queue (recorded in the workflow input for context).
const temporalSDKTaskQueue = "inec-platform"

// temporalSDKClient is a real Temporal integration over the gRPC SDK. It dials
// the Temporal frontend, runs an in-process worker that registers the
// platform's workflow types, and drives workflows via ExecuteWorkflow /
// DescribeWorkflowExecution / SignalWorkflow. Activated when TEMPORAL_HOSTPORT
// is set; otherwise the embedded engine is used.
type temporalSDKClient struct {
	c         client.Client
	w         worker.Worker
	hostPort  string
	namespace string
}

func newTemporalSDKClient(hostPort, namespace string) (*temporalSDKClient, error) {
	if namespace == "" {
		namespace = "default"
	}
	c, err := client.Dial(client.Options{
		HostPort:  hostPort,
		Namespace: namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("temporal dial: %w", err)
	}
	w := worker.New(c, temporalSDKTaskQueue, worker.Options{})
	w.RegisterWorkflowWithOptions(temporalResultSubmissionWF, workflow.RegisterOptions{Name: "ResultSubmissionWorkflow"})
	w.RegisterWorkflowWithOptions(temporalResultValidationWF, workflow.RegisterOptions{Name: "ResultValidationWorkflow"})
	w.RegisterWorkflowWithOptions(temporalResultFinalizationWF, workflow.RegisterOptions{Name: "ResultFinalizationWorkflow"})
	w.RegisterWorkflowWithOptions(temporalGenericWF, workflow.RegisterOptions{Name: "GenericWorkflow"})
	if err := w.Start(); err != nil {
		c.Close()
		return nil, fmt.Errorf("temporal worker start: %w", err)
	}
	return &temporalSDKClient{c: c, w: w, hostPort: hostPort, namespace: namespace}, nil
}

func (t *temporalSDKClient) workflowName(reqType string) string {
	switch reqType {
	case "ResultSubmissionWorkflow", "ResultValidationWorkflow", "ResultFinalizationWorkflow":
		return reqType
	default:
		return "GenericWorkflow"
	}
}

func (t *temporalSDKClient) StartWorkflow(ctx context.Context, input WorkflowInput) (*WorkflowStatus, error) {
	if input.Input == nil {
		input.Input = map[string]interface{}{}
	}
	// Preserve the caller-requested type/queue as workflow context.
	input.Input["_requested_type"] = input.WorkflowType
	input.Input["_requested_queue"] = input.TaskQueue
	opts := client.StartWorkflowOptions{
		ID:        input.WorkflowID,
		TaskQueue: temporalSDKTaskQueue,
	}
	run, err := t.c.ExecuteWorkflow(ctx, opts, t.workflowName(input.WorkflowType), input.Input)
	if err != nil {
		return nil, fmt.Errorf("temporal execute: %w", err)
	}
	return &WorkflowStatus{
		WorkflowID: run.GetID(),
		RunID:      run.GetRunID(),
		Status:     "RUNNING",
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (t *temporalSDKClient) GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowStatus, error) {
	desc, err := t.c.DescribeWorkflowExecution(ctx, workflowID, "")
	if err != nil {
		return nil, err
	}
	info := desc.GetWorkflowExecutionInfo()
	ws := &WorkflowStatus{
		WorkflowID: workflowID,
		Status:     temporalStatusString(info.GetStatus()),
	}
	if exec := info.GetExecution(); exec != nil {
		ws.RunID = exec.GetRunId()
	}
	if t := info.GetStartTime(); t != nil {
		ws.StartedAt = t.AsTime().UTC().Format(time.RFC3339)
	}
	if t := info.GetCloseTime(); t != nil {
		ws.ClosedAt = t.AsTime().UTC().Format(time.RFC3339)
	}
	return ws, nil
}

func (t *temporalSDKClient) SignalWorkflow(ctx context.Context, workflowID, signalName string, data interface{}) error {
	return t.c.SignalWorkflow(ctx, workflowID, "", signalName, data)
}


func (t *temporalSDKClient) CancelWorkflow(ctx context.Context, workflowID string) error {
	return t.c.CancelWorkflow(ctx, workflowID, "")
}
func (t *temporalSDKClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lat, err := measureLatency(func() error {
		_, e := t.c.CheckHealth(ctx, &client.CheckHealthRequest{})
		return e
	})
	if err != nil {
		return MWStatus{Name: "Temporal", Connected: false, Mode: "grpc (unhealthy)", Details: err.Error()}
	}
	return MWStatus{
		Name: "Temporal", Connected: true, Mode: "grpc-sdk",
		Latency: fmtLatency(lat),
		Details: fmt.Sprintf("namespace=%s task_queue=%s", t.namespace, temporalSDKTaskQueue),
	}
}

func (t *temporalSDKClient) Close() error {
	if t.w != nil {
		t.w.Stop()
	}
	if t.c != nil {
		t.c.Close()
	}
	return nil
}

func temporalStatusString(s enums.WorkflowExecutionStatus) string {
	switch s {
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING:
		return "RUNNING"
	case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "COMPLETED"
	case enums.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "FAILED"
	case enums.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "CANCELED"
	case enums.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "TERMINATED"
	case enums.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "TIMED_OUT"
	default:
		return "UNKNOWN"
	}
}

// ── Registered workflow definitions (deterministic; mirror the embedded phases) ──

func temporalPhasedWF(ctx workflow.Context, phases []string, result string) (string, error) {
	for _, phase := range phases {
		_ = workflow.Sleep(ctx, 50*time.Millisecond)
		workflow.GetLogger(ctx).Info("phase", "phase", phase)
	}
	return result, nil
}

func temporalResultSubmissionWF(ctx workflow.Context, _ map[string]interface{}) (string, error) {
	return temporalPhasedWF(ctx, []string{"pre_validation", "edge_validation", "submission", "acknowledgement"}, "result_submitted")
}

func temporalResultValidationWF(ctx workflow.Context, _ map[string]interface{}) (string, error) {
	return temporalPhasedWF(ctx, []string{"vote_count_check", "cross_reference", "officer_verification", "validation_complete"}, "result_validated")
}

func temporalResultFinalizationWF(ctx workflow.Context, _ map[string]interface{}) (string, error) {
	return temporalPhasedWF(ctx, []string{"ledger_posting", "blockchain_commit", "ipfs_archive", "finalization_complete"}, "result_finalized")
}

func temporalGenericWF(ctx workflow.Context, input map[string]interface{}) (string, error) {
	reqType, _ := input["_requested_type"].(string)
	_ = workflow.Sleep(ctx, 50*time.Millisecond)
	workflow.GetLogger(ctx).Info("generic workflow", "requested_type", reqType)
	return "completed:" + reqType, nil
}

func initTemporalSDKClient() TemporalClient {
	hostPort := envOrDefault("TEMPORAL_HOSTPORT", "")
	if hostPort == "" {
		return nil
	}
	namespace := envOrDefault("TEMPORAL_NAMESPACE", "default")
	c, err := newTemporalSDKClient(hostPort, namespace)
	if err != nil {
		log.Warn().Err(err).Str("hostport", hostPort).Msg("Temporal gRPC SDK connect failed, falling back")
		return nil
	}
	log.Info().Str("hostport", hostPort).Str("namespace", namespace).Msg("Temporal connected via gRPC SDK")
	return c
}
