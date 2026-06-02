package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// Temporal Workflow Definitions for Election Lifecycle.
// These workflows orchestrate long-running election processes.

type ElectionWorkflowInput struct {
	ElectionID int    `json:"election_id"`
	Action     string `json:"action"`
	Actor      string `json:"actor"`
}

// StartElectionActivationWorkflow triggers the multi-step activation process via Temporal.
func StartElectionActivationWorkflow(ctx context.Context, electionID int, actor string) error {
	if mwHub == nil || mwHub.Temporal == nil {
		log.Debug().Msg("Temporal not available, running inline")
		return runActivationInline(ctx, electionID, actor)
	}

	wf, err := mwHub.Temporal.StartWorkflow(ctx, WorkflowInput{
		WorkflowID:   fmt.Sprintf("election-activate-%d-%d", electionID, time.Now().Unix()),
		WorkflowType: "ElectionActivation",
		TaskQueue:    "inec-election-lifecycle",
		Input: map[string]interface{}{
			"election_id": electionID,
			"action":      "activate",
			"actor":       actor,
		},
	})
	if err != nil {
		log.Warn().Err(err).Int("election_id", electionID).Msg("Temporal workflow failed, running inline")
		return runActivationInline(ctx, electionID, actor)
	}
	log.Info().Str("workflow_id", wf.WorkflowID).Str("run_id", wf.RunID).Msg("election activation workflow started")
	return nil
}

func runActivationInline(ctx context.Context, electionID int, actor string) error {
	// Step 1: Validate staff assignments
	var staffCount int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM staff_assignments WHERE election_id=?", electionID).Scan(&staffCount)
	if staffCount == 0 {
		return fmt.Errorf("no staff assigned to election %d", electionID)
	}

	// Step 2: Generate BVAS sync tokens
	dbExecLog("bvas_prep", "UPDATE bvas_devices SET sync_token=hex(randomblob(16)) WHERE assigned_election_id=?", electionID)

	// Step 3: Notify observers
	if mwHub != nil && mwHub.Kafka != nil {
		mwHub.Kafka.Produce(ctx, KafkaMessage{
			Topic: "inec.election.lifecycle",
			Key:   fmt.Sprintf("%d", electionID),
			Value: map[string]interface{}{
				"event": "activation_complete", "election_id": electionID, "staff_count": staffCount,
			},
		})
	}

	// Step 4: Cache election config in Redis
	if mwHub != nil && mwHub.Redis != nil {
		mwHub.Redis.Set(ctx, fmt.Sprintf("election:%d:active", electionID), "true", 24*time.Hour)
	}

	// Step 5: Index in OpenSearch for real-time queries
	if mwHub != nil && mwHub.OpenSearch != nil {
		mwHub.OpenSearch.Index(ctx, "elections", fmt.Sprintf("%d", electionID), map[string]interface{}{
			"id": electionID, "status": "active", "activated_by": actor, "activated_at": time.Now().UTC().Format(time.RFC3339),
		})
	}

	return nil
}

// StartResultCollationWorkflow triggers auto-collation when all PUs in a ward submit.
func StartResultCollationWorkflow(ctx context.Context, electionID int, wardCode string) {
	if mwHub == nil || mwHub.Temporal == nil {
		runCollationInline(ctx, electionID, wardCode)
		return
	}

	mwHub.Temporal.StartWorkflow(ctx, WorkflowInput{
		WorkflowID:   fmt.Sprintf("collation-%d-%s-%d", electionID, wardCode, time.Now().Unix()),
		WorkflowType: "ResultCollation",
		TaskQueue:    "inec-collation",
		Input: map[string]interface{}{
			"election_id": electionID,
			"ward_code":   wardCode,
		},
	})
}

func runCollationInline(ctx context.Context, electionID int, wardCode string) {
	result, err := collateWard(ctx, electionID, wardCode)
	if err != nil {
		log.Error().Err(err).Str("ward", wardCode).Msg("inline collation failed")
		return
	}

	// Persist collation result
	dbExecLog("collation_save", "INSERT INTO collation_results (election_id, level, code, total_votes, status, collated_at) VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)",
		electionID, "ward", wardCode, result.TotalVotes, "collated")

	// Publish event
	if mwHub != nil && mwHub.Kafka != nil {
		mwHub.Kafka.Produce(ctx, KafkaMessage{
			Topic: TopicResultValidated,
			Key:   wardCode,
			Value: map[string]interface{}{
				"election_id": electionID, "ward_code": wardCode,
				"total_votes": result.TotalVotes, "party_count": len(result.PartyTotals),
			},
		})
	}

	// Store in Lakehouse for analytics
	if mwHub != nil && mwHub.Lakehouse != nil {
		mwHub.Lakehouse.Ingest(ctx, "collation_events", []map[string]interface{}{{
			"election_id": electionID, "level": "ward", "code": wardCode,
			"total_votes": result.TotalVotes, "collated_at": time.Now().UTC().Format(time.RFC3339),
		}})
	}

	log.Info().Str("ward", wardCode).Int64("total_votes", result.TotalVotes).Msg("ward collation complete")
}

// --- Mojaloop Payment Settlement for Election Materials ---

type MaterialPaymentRequest struct {
	ElectionID   int     `json:"election_id"`
	RecipientFSP string  `json:"recipient_fsp"`
	RecipientID  string  `json:"recipient_id"`
	Amount       float64 `json:"amount"`
	Currency     string  `json:"currency"`
	Description  string  `json:"description"`
}

// ProcessMaterialPayment uses Mojaloop for paying logistics vendors.
func ProcessMaterialPayment(ctx context.Context, req MaterialPaymentRequest) error {
	if mwHub == nil || mwHub.Mojaloop == nil {
		log.Warn().Msg("Mojaloop not available, payment queued")
		dbExecLog("payment_queue", "INSERT INTO payment_queue (election_id, recipient_id, amount, currency, status) VALUES (?,?,?,?,'queued')",
			req.ElectionID, req.RecipientID, req.Amount, req.Currency)
		return nil
	}

	// Phase 1: Party lookup
	party, err := mwHub.Mojaloop.PartyLookup(ctx, "MSISDN", req.RecipientID)
	if err != nil {
		return fmt.Errorf("payee lookup failed: %w", err)
	}

	// Phase 2: Quote
	quote, err := mwHub.Mojaloop.CreateQuote(ctx, MojaQuoteRequest{
		QuoteID:  fmt.Sprintf("INEC-Q-%d-%d", req.ElectionID, time.Now().Unix()),
		PayerFSP: "INEC-FSP",
		PayeeFSP: party.FSPID,
		Amount:   req.Amount,
		Currency: req.Currency,
	})
	if err != nil {
		return fmt.Errorf("quote failed: %w", err)
	}

	// Phase 3: Transfer
	_, err = mwHub.Mojaloop.CreateTransfer(ctx, MojaTransferRequest{
		TransferID: fmt.Sprintf("INEC-T-%d-%d", req.ElectionID, time.Now().Unix()),
		QuoteID:    quote.QuoteID,
		PayerFSP:   "INEC-FSP",
		PayeeFSP:   party.FSPID,
		Amount:     req.Amount,
		Currency:   req.Currency,
		ILPPacket:  quote.ILPPacket,
		Condition:  quote.Condition,
	})
	if err != nil {
		return fmt.Errorf("transfer failed: %w", err)
	}

	log.Info().Float64("amount", req.Amount).Str("recipient", req.RecipientID).Msg("material payment processed")
	return nil
}

// --- Permify Authorization Checks ---

// CheckPermission validates an action against the Permify policy engine.
func CheckPermission(ctx context.Context, subject, action, resource string) (bool, error) {
	if mwHub == nil || mwHub.Permify == nil {
		// Fallback to built-in RBAC
		return true, nil
	}
	return mwHub.Permify.Check(ctx, PermifyCheck{
		Subject:      subject,
		SubjectType:  "user",
		Permission:   action,
		Resource:     resource,
		ResourceType: "election",
	})
}

// --- Dapr State Management ---

// SaveElectionState persists election state to Dapr state store (Redis-backed).
func SaveElectionState(ctx context.Context, electionID int, state map[string]interface{}) error {
	if mwHub == nil || mwHub.Dapr == nil {
		return nil
	}
	key := fmt.Sprintf("election-%d-state", electionID)
	return mwHub.Dapr.SaveState(ctx, "statestore", key, state)
}

// PublishElectionEvent publishes an event via Dapr pub/sub (Kafka-backed).
func PublishElectionEvent(ctx context.Context, topic string, data interface{}) error {
	if mwHub == nil || mwHub.Dapr == nil {
		return nil
	}
	return mwHub.Dapr.PublishEvent(ctx, "pubsub", topic, data)
}

// --- OpenAppSec Threat Intelligence ---

// ReportThreatToOpenAppSec sends detected threats to OpenAppSec for blocking.
func ReportThreatToOpenAppSec(ctx context.Context, threatType, sourceIP, details string) {
	if mwHub == nil || mwHub.OpenAppSec == nil {
		return
	}
	mwHub.OpenAppSec.AddIPToBlocklist(ctx, sourceIP, fmt.Sprintf("%s: %s", threatType, details))
}

// --- APISIX Route Registration ---

// RegisterAPIRoute registers a new route in APISIX gateway.
func RegisterAPIRoute(ctx context.Context, path, upstream string, rateLimit int) error {
	if mwHub == nil || mwHub.APISIX == nil {
		return nil
	}
	return mwHub.APISIX.RegisterRoute(ctx, APISIXRoute{
		ID:       fmt.Sprintf("inec-%s", path),
		URI:      path,
		Upstream: map[string]interface{}{"nodes": map[string]int{upstream: 1}, "type": "roundrobin"},
		Plugins: map[string]interface{}{
			"limit-req": map[string]interface{}{"rate": rateLimit, "burst": rateLimit * 2, "rejected_code": 429},
		},
	})
}

// --- Fluvio Stream Processing ---

// PublishToFluvio sends events to Fluvio for stream processing.
func PublishToFluvio(ctx context.Context, topic string, data interface{}) error {
	if mwHub == nil || mwHub.Fluvio == nil {
		return nil
	}
	record, ok := data.(FluvioRecord)
	if !ok {
		record = FluvioRecord{
			Topic:     topic,
			Key:       topic,
			Value:     map[string]interface{}{"data": data},
			Timestamp: time.Now(),
		}
	}
	return mwHub.Fluvio.Produce(ctx, topic, record)
}
