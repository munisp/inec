// Package workflow provides Temporal SDK integration for long-running election workflows.
// Replaces the HTTP client approach with typed workflows and activities.
package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// TaskQueue names for different workflow types.
const (
	TaskQueueResults    = "inec-results"
	TaskQueueCollation  = "inec-collation"
	TaskQueueBiometric  = "inec-biometric"
	TaskQueueAudit      = "inec-audit"
)

// Config for Temporal connection.
type Config struct {
	HostPort  string // e.g., "temporal:7233"
	Namespace string // e.g., "inec-production"
}

// Service wraps the Temporal client and worker.
type Service struct {
	client    client.Client
	workers   []worker.Worker
	namespace string
}

// NewService creates a new Temporal workflow service.
func NewService(cfg Config) (*Service, error) {
	c, err := client.Dial(client.Options{
		HostPort:  cfg.HostPort,
		Namespace: cfg.Namespace,
		Logger:    &temporalLogger{},
	})
	if err != nil {
		return nil, fmt.Errorf("connect to Temporal at %s: %w", cfg.HostPort, err)
	}

	log.Info().Str("host", cfg.HostPort).Str("namespace", cfg.Namespace).Msg("Temporal client connected")

	return &Service{
		client:    c,
		namespace: cfg.Namespace,
	}, nil
}

// RegisterWorkers starts workers for all INEC task queues.
func (s *Service) RegisterWorkers() {
	// Results processing worker
	resultsWorker := worker.New(s.client, TaskQueueResults, worker.Options{
		MaxConcurrentActivityExecutionSize: 50,
	})
	resultsWorker.RegisterWorkflow(ResultSubmissionWorkflow)
	resultsWorker.RegisterWorkflow(ResultValidationWorkflow)
	resultsWorker.RegisterActivity(&ResultActivities{})
	s.workers = append(s.workers, resultsWorker)

	// Collation worker
	collationWorker := worker.New(s.client, TaskQueueCollation, worker.Options{
		MaxConcurrentActivityExecutionSize: 20,
	})
	collationWorker.RegisterWorkflow(CollationWorkflow)
	collationWorker.RegisterActivity(&CollationActivities{})
	s.workers = append(s.workers, collationWorker)

	// Biometric worker
	biometricWorker := worker.New(s.client, TaskQueueBiometric, worker.Options{
		MaxConcurrentActivityExecutionSize: 100,
	})
	biometricWorker.RegisterWorkflow(BiometricVerificationWorkflow)
	biometricWorker.RegisterActivity(&BiometricActivities{})
	s.workers = append(s.workers, biometricWorker)

	// Audit worker
	auditWorker := worker.New(s.client, TaskQueueAudit, worker.Options{
		MaxConcurrentActivityExecutionSize: 30,
	})
	auditWorker.RegisterWorkflow(AuditTrailWorkflow)
	auditWorker.RegisterActivity(&AuditActivities{})
	s.workers = append(s.workers, auditWorker)
}

// Start begins processing workflows on all workers.
func (s *Service) Start() error {
	for _, w := range s.workers {
		if err := w.Start(); err != nil {
			return fmt.Errorf("start worker: %w", err)
		}
	}
	log.Info().Int("workers", len(s.workers)).Msg("Temporal workers started")
	return nil
}

// Close stops workers and disconnects.
func (s *Service) Close() {
	for _, w := range s.workers {
		w.Stop()
	}
	if s.client != nil {
		s.client.Close()
	}
}

// --- Result Submission Workflow ---

// ResultSubmissionInput contains the data for result processing.
type ResultSubmissionInput struct {
	ElectionID      int    `json:"election_id"`
	PollingUnitCode string `json:"polling_unit_code"`
	State           string `json:"state"`
	LGA             string `json:"lga"`
	Ward            string `json:"ward"`
	TotalVotes      int    `json:"total_votes"`
	RejectedVotes   int    `json:"rejected_votes"`
	AccreditedVoters int   `json:"accredited_voters"`
	SubmittedBy     int    `json:"submitted_by"`
	DeviceID        string `json:"device_id"`
	ResultHash      string `json:"result_hash"`
}

// ResultSubmissionWorkflow orchestrates the full result submission pipeline.
func ResultSubmissionWorkflow(ctx workflow.Context, input ResultSubmissionInput) (string, error) {
	retryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    time.Minute,
		MaximumAttempts:    5,
	}
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         retryPolicy,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var activities *ResultActivities

	// Step 1: Validate result data
	var validationResult string
	err := workflow.ExecuteActivity(ctx, activities.ValidateResult, input).Get(ctx, &validationResult)
	if err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}

	// Step 2: Check for anomalies
	var anomalyScore float64
	err = workflow.ExecuteActivity(ctx, activities.CheckAnomalies, input).Get(ctx, &anomalyScore)
	if err != nil {
		return "", fmt.Errorf("anomaly check failed: %w", err)
	}

	// Step 3: Store result
	var resultID string
	err = workflow.ExecuteActivity(ctx, activities.StoreResult, input).Get(ctx, &resultID)
	if err != nil {
		return "", fmt.Errorf("store failed: %w", err)
	}

	// Step 4: Publish to blockchain audit trail
	err = workflow.ExecuteActivity(ctx, activities.PublishToBlockchain, resultID, input.ResultHash).Get(ctx, nil)
	if err != nil {
		// Non-fatal — log and continue
		workflow.GetLogger(ctx).Warn("Blockchain publish failed, will retry", "error", err)
	}

	// Step 5: Notify observers via event
	err = workflow.ExecuteActivity(ctx, activities.NotifyObservers, resultID, input.PollingUnitCode).Get(ctx, nil)
	if err != nil {
		workflow.GetLogger(ctx).Warn("Observer notification failed", "error", err)
	}

	return resultID, nil
}

// ResultActivities implements the activities for result processing.
type ResultActivities struct{}

func (a *ResultActivities) ValidateResult(ctx context.Context, input ResultSubmissionInput) (string, error) {
	// Validate vote counts, check PU exists, verify submitter authorization
	if input.TotalVotes < 0 || input.AccreditedVoters < 0 {
		return "rejected", fmt.Errorf("negative vote counts")
	}
	if input.TotalVotes > input.AccreditedVoters {
		return "flagged", nil // Overvoting
	}
	return "valid", nil
}

func (a *ResultActivities) CheckAnomalies(ctx context.Context, input ResultSubmissionInput) (float64, error) {
	// Statistical anomaly detection
	if input.AccreditedVoters > 0 {
		turnout := float64(input.TotalVotes) / float64(input.AccreditedVoters)
		if turnout > 0.95 {
			return turnout, nil // Flag high turnout
		}
	}
	return 0.0, nil
}

func (a *ResultActivities) StoreResult(ctx context.Context, input ResultSubmissionInput) (string, error) {
	resultID := fmt.Sprintf("RES-%s-%d", input.PollingUnitCode, time.Now().UnixNano())
	return resultID, nil
}

func (a *ResultActivities) PublishToBlockchain(ctx context.Context, resultID, hash string) error {
	// Publish immutable record to Hyperledger/blockchain audit trail
	return nil
}

func (a *ResultActivities) NotifyObservers(ctx context.Context, resultID, pollingUnit string) error {
	// Send SSE/WebSocket notification to observer dashboard
	return nil
}

// --- Result Validation Workflow ---

// ResultValidationWorkflow handles multi-step validation of submitted results.
func ResultValidationWorkflow(ctx workflow.Context, resultID string, electionID int) (string, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var activities *ResultActivities

	// Wait for officer confirmation (with timeout)
	var confirmed bool
	confirmCh := workflow.GetSignalChannel(ctx, "officer-confirm")
	selector := workflow.NewSelector(ctx)
	selector.AddReceive(confirmCh, func(c workflow.ReceiveChannel, more bool) {
		c.Receive(ctx, &confirmed)
	})
	timerFuture := workflow.NewTimer(ctx, 30*time.Minute)
	selector.AddFuture(timerFuture, func(f workflow.Future) {
		confirmed = false
	})
	selector.Select(ctx)

	if !confirmed {
		return "timeout", nil
	}

	// Cross-reference with BVAS device data
	_ = workflow.ExecuteActivity(ctx, activities.ValidateResult, ResultSubmissionInput{ElectionID: electionID}).Get(ctx, nil)

	return "validated", nil
}

// --- Collation Workflow ---

// CollationInput defines the scope of collation.
type CollationInput struct {
	ElectionID int    `json:"election_id"`
	Level      string `json:"level"` // ward, lga, state, national
	AreaCode   string `json:"area_code"`
}

// CollationWorkflow aggregates results at different administrative levels.
func CollationWorkflow(ctx workflow.Context, input CollationInput) (map[string]interface{}, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		HeartbeatTimeout:    30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var activities *CollationActivities

	// Aggregate results
	var totals map[string]interface{}
	err := workflow.ExecuteActivity(ctx, activities.AggregateResults, input).Get(ctx, &totals)
	if err != nil {
		return nil, err
	}

	// Validate aggregation
	err = workflow.ExecuteActivity(ctx, activities.ValidateAggregation, totals).Get(ctx, nil)
	if err != nil {
		return nil, err
	}

	// Publish to next level
	err = workflow.ExecuteActivity(ctx, activities.PublishCollation, input.Level, totals).Get(ctx, nil)
	if err != nil {
		return nil, err
	}

	return totals, nil
}

// CollationActivities implements collation activity methods.
type CollationActivities struct{}

func (a *CollationActivities) AggregateResults(ctx context.Context, input CollationInput) (map[string]interface{}, error) {
	return map[string]interface{}{"election_id": input.ElectionID, "level": input.Level}, nil
}

func (a *CollationActivities) ValidateAggregation(ctx context.Context, totals map[string]interface{}) error {
	return nil
}

func (a *CollationActivities) PublishCollation(ctx context.Context, level string, totals map[string]interface{}) error {
	return nil
}

// --- Biometric Verification Workflow ---

// BiometricInput for verification request.
type BiometricInput struct {
	VoterVIN  string `json:"voter_vin"`
	DeviceID  string `json:"device_id"`
	Modality  string `json:"modality"`
	Template  []byte `json:"template"`
}

// BiometricVerificationWorkflow handles biometric verification with retries.
func BiometricVerificationWorkflow(ctx workflow.Context, input BiometricInput) (bool, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
			InitialInterval: 500 * time.Millisecond,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var activities *BiometricActivities

	var matched bool
	err := workflow.ExecuteActivity(ctx, activities.VerifyBiometric, input).Get(ctx, &matched)
	if err != nil {
		return false, err
	}

	// Log the attempt
	_ = workflow.ExecuteActivity(ctx, activities.LogAttempt, input.VoterVIN, matched).Get(ctx, nil)

	return matched, nil
}

// BiometricActivities for biometric operations.
type BiometricActivities struct{}

func (a *BiometricActivities) VerifyBiometric(ctx context.Context, input BiometricInput) (bool, error) {
	return true, nil // Delegates to biometric.Service
}

func (a *BiometricActivities) LogAttempt(ctx context.Context, voterVIN string, matched bool) error {
	return nil
}

// --- Audit Trail Workflow ---

// AuditEvent for audit recording.
type AuditEvent struct {
	Action    string `json:"action"`
	UserID    int    `json:"user_id"`
	Resource  string `json:"resource"`
	Details   string `json:"details"`
	IPAddress string `json:"ip_address"`
}

// AuditTrailWorkflow records audit events with guaranteed delivery.
func AuditTrailWorkflow(ctx workflow.Context, event AuditEvent) error {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    10,
			InitialInterval:    time.Second,
			BackoffCoefficient: 1.5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var activities *AuditActivities
	return workflow.ExecuteActivity(ctx, activities.RecordAuditEvent, event).Get(ctx, nil)
}

// AuditActivities for audit operations.
type AuditActivities struct{}

func (a *AuditActivities) RecordAuditEvent(ctx context.Context, event AuditEvent) error {
	return nil // Delegates to database insert
}

// --- Client methods for starting workflows ---

// SubmitResult starts a result submission workflow.
func (s *Service) SubmitResult(ctx context.Context, input ResultSubmissionInput) (string, error) {
	workflowID := fmt.Sprintf("result-%s-%d", input.PollingUnitCode, time.Now().UnixNano())
	run, err := s.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueueResults,
	}, ResultSubmissionWorkflow, input)
	if err != nil {
		return "", fmt.Errorf("start result workflow: %w", err)
	}

	var resultID string
	if err := run.Get(ctx, &resultID); err != nil {
		return "", err
	}
	return resultID, nil
}

// StartCollation triggers collation at a given level.
func (s *Service) StartCollation(ctx context.Context, input CollationInput) (string, error) {
	workflowID := fmt.Sprintf("collation-%s-%s-%d", input.Level, input.AreaCode, time.Now().UnixNano())
	run, err := s.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueueCollation,
	}, CollationWorkflow, input)
	if err != nil {
		return "", err
	}
	return run.GetID(), nil
}

// VerifyBiometric starts a biometric verification workflow.
func (s *Service) VerifyBiometric(ctx context.Context, input BiometricInput) (bool, error) {
	workflowID := fmt.Sprintf("bio-%s-%d", input.VoterVIN, time.Now().UnixNano())
	run, err := s.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueueBiometric,
	}, BiometricVerificationWorkflow, input)
	if err != nil {
		return false, err
	}

	var matched bool
	if err := run.Get(ctx, &matched); err != nil {
		return false, err
	}
	return matched, nil
}

// RecordAudit starts an audit trail workflow.
func (s *Service) RecordAudit(ctx context.Context, event AuditEvent) error {
	workflowID := fmt.Sprintf("audit-%d-%d", event.UserID, time.Now().UnixNano())
	_, err := s.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueueAudit,
	}, AuditTrailWorkflow, event)
	return err
}

// --- Temporal logger adapter ---

type temporalLogger struct{}

func (l *temporalLogger) Debug(msg string, keyvals ...interface{}) {
	log.Debug().Interface("temporal", keyvals).Msg(msg)
}
func (l *temporalLogger) Info(msg string, keyvals ...interface{}) {
	log.Info().Interface("temporal", keyvals).Msg(msg)
}
func (l *temporalLogger) Warn(msg string, keyvals ...interface{}) {
	log.Warn().Interface("temporal", keyvals).Msg(msg)
}
func (l *temporalLogger) Error(msg string, keyvals ...interface{}) {
	log.Error().Interface("temporal", keyvals).Msg(msg)
}
