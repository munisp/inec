package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

const (
	deviceEventKafkaTopic        = "inec.bvas.device-events.v1"
	deviceEventFluvioTopic       = "inec.bvas.device-events.v1"
	externalIntegrationTaskQueue = "external-integration"
)

type externalOutboxEvent struct {
	ID            int64
	CorrelationID string
	SourceType    string
	AggregateType string
	AggregateID   string
	EventType     string
	PartitionKey  string
	Payload       map[string]interface{}
	RequiredSinks []string
	AttemptCount  int
}

var (
	externalDeliveryCancel context.CancelFunc
	externalDeliveryOnce   sync.Once
)

func initExternalIntegrationDeliverySchema(database *sql.DB) {
	schema := `
	CREATE TABLE IF NOT EXISTS external_integration_delivery_attempts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		outbox_id INTEGER NOT NULL,
		correlation_id TEXT NOT NULL,
		sink_name TEXT NOT NULL,
		attempt_no INTEGER NOT NULL,
		outcome TEXT NOT NULL,
		external_reference TEXT,
		error_code TEXT,
		error_detail TEXT,
		attempted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(outbox_id,sink_name,attempt_no)
	);
	CREATE INDEX IF NOT EXISTS idx_external_delivery_attempt_outbox ON external_integration_delivery_attempts(outbox_id, attempted_at);
	`
	execMulti(database, schema)
}

func startExternalIntegrationDeliveryWorker() {
	externalDeliveryOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		externalDeliveryCancel = cancel
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				if err := deliverExternalIntegrationOutbox(ctx, 32); err != nil {
					log.Warn().Err(err).Msg("external integration outbox worker pass failed")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func stopExternalIntegrationDeliveryWorker() {
	if externalDeliveryCancel != nil {
		externalDeliveryCancel()
	}
}

func parseRequiredSinks(raw string) []string {
	var sinks []string
	if err := json.Unmarshal([]byte(raw), &sinks); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(sinks))
	out := make([]string, 0, len(sinks))
	for _, sink := range sinks {
		sink = strings.ToLower(strings.TrimSpace(sink))
		if sink == "" {
			continue
		}
		if _, exists := seen[sink]; exists {
			continue
		}
		seen[sink] = struct{}{}
		out = append(out, sink)
	}
	return out
}

func claimExternalOutboxEvent(ctx context.Context) (*externalOutboxEvent, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,correlation_id,source_type,aggregate_type,aggregate_id,event_type,partition_key,payload_redacted,required_sinks,attempt_count
		FROM external_integration_outbox
		WHERE delivery_status IN ('pending','failed') AND next_attempt_at <= CURRENT_TIMESTAMP
		ORDER BY id LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var event externalOutboxEvent
	var payloadRaw, sinksRaw string
	if err := rows.Scan(&event.ID, &event.CorrelationID, &event.SourceType, &event.AggregateType, &event.AggregateID, &event.EventType, &event.PartitionKey, &payloadRaw, &sinksRaw, &event.AttemptCount); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(payloadRaw), &event.Payload); err != nil {
		return nil, fmt.Errorf("outbox %d has invalid redacted payload: %w", event.ID, err)
	}
	event.RequiredSinks = parseRequiredSinks(sinksRaw)
	if len(event.RequiredSinks) == 0 {
		return nil, fmt.Errorf("outbox %d declares no required sinks", event.ID)
	}
	result, err := db.ExecContext(ctx, `UPDATE external_integration_outbox SET delivery_status='delivering',attempt_count=attempt_count+1
		WHERE id=? AND delivery_status IN ('pending','failed')`, event.ID)
	if err != nil {
		return nil, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return nil, nil
	}
	event.AttemptCount++
	return &event, nil
}

func recordExternalDeliveryAttempt(ctx context.Context, event *externalOutboxEvent, sink, outcome, externalReference string, err error) {
	var code, detail interface{}
	if err != nil {
		code = "delivery_error"
		detail = err.Error()
	}
	if _, writeErr := db.ExecContext(ctx, `INSERT INTO external_integration_delivery_attempts
		(outbox_id,correlation_id,sink_name,attempt_no,outcome,external_reference,error_code,error_detail)
		VALUES (?,?,?,?,?,?,?,?)`, event.ID, event.CorrelationID, sink, event.AttemptCount, outcome, nullIfEmpty(externalReference), code, detail); writeErr != nil {
		log.Error().Err(writeErr).Int64("outbox_id", event.ID).Str("sink", sink).Msg("failed to persist external delivery attempt")
	}
}

func nullIfEmpty(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func externalSinkStatus(name string) bool {
	if mwHub == nil {
		return false
	}
	switch name {
	case "kafka":
		return mwHub.Kafka != nil && mwHub.Kafka.Status().Connected
	case "dapr":
		return mwHub.Dapr != nil && mwHub.Dapr.Status().Connected
	case "fluvio":
		return mwHub.Fluvio != nil && mwHub.Fluvio.Status().Connected
	case "temporal":
		return mwHub.Temporal != nil && mwHub.Temporal.Status().Connected
	case "opensearch":
		return mwHub.OpenSearch != nil && mwHub.OpenSearch.Status().Connected
	default:
		return false
	}
}

func deliverExternalSink(ctx context.Context, event *externalOutboxEvent, sink string) (string, error) {
	if !externalSinkStatus(sink) {
		return "", fmt.Errorf("%s is unavailable", sink)
	}
	payload := make(map[string]interface{}, len(event.Payload)+3)
	for key, value := range event.Payload {
		payload[key] = value
	}
	payload["correlation_id"] = event.CorrelationID
	payload["outbox_id"] = event.ID
	payload["event_version"] = "v1"
	switch sink {
	case "kafka":
		topic := event.EventType
		if topic == "" {
			topic = deviceEventKafkaTopic
		}
		if err := mwHub.Kafka.Produce(ctx, KafkaMessage{Topic: topic, Key: event.PartitionKey, Value: payload, Timestamp: time.Now().UTC()}); err != nil {
			return "", err
		}
		return topic + ":" + event.PartitionKey, nil
	case "dapr":
		if err := mwHub.Dapr.PublishEvent(ctx, "pubsub", event.EventType, payload); err != nil {
			return "", err
		}
		return "pubsub:" + event.EventType, nil
	case "fluvio":
		topic := event.EventType
		if topic == "" {
			topic = deviceEventFluvioTopic
		}
		if err := mwHub.Fluvio.Produce(ctx, topic, FluvioRecord{Topic: topic, Key: event.PartitionKey, Value: payload, Timestamp: time.Now().UTC()}); err != nil {
			return "", err
		}
		return topic + ":" + event.PartitionKey, nil
	case "opensearch":
		indexed := M{
			"correlation_id": event.CorrelationID,
			"outbox_id":      event.ID,
			"source_type":    event.SourceType,
			"aggregate_type": event.AggregateType,
			"aggregate_id":   event.AggregateID,
			"event_type":     event.EventType,
			"event_version":  "v1",
			"received_at":    time.Now().UTC().Format(time.RFC3339Nano),
			"payload":        event.Payload,
		}
		if err := mwHub.OpenSearch.Index(ctx, "inec-external-device-events-v1", event.CorrelationID, indexed); err != nil {
			return "", err
		}
		return "inec-external-device-events-v1:" + event.CorrelationID, nil
	case "temporal":
		workflowID := "device-event-" + event.CorrelationID
		status, err := mwHub.Temporal.StartWorkflow(ctx, WorkflowInput{
			WorkflowID:   workflowID,
			WorkflowType: "ExternalIntegrationReconciliationWorkflow",
			TaskQueue:    externalIntegrationTaskQueue,
			Input:        payload,
			RetryPolicy:  &RetryPolicy{MaxAttempts: 8, InitialInterval: 5 * time.Second, BackoffCoefficient: 2, MaxInterval: 5 * time.Minute},
		})
		if err != nil {
			return "", err
		}
		if status == nil || strings.TrimSpace(status.WorkflowID) == "" {
			return "", fmt.Errorf("Temporal did not return a workflow identity")
		}
		return status.WorkflowID, nil
	default:
		return "", fmt.Errorf("unsupported external delivery sink %s", sink)
	}
}

func backoffForAttempt(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	return time.Duration(1<<shift) * time.Second
}

func finalizeExternalOutboxEvent(ctx context.Context, event *externalOutboxEvent, success bool, reason string) error {
	if success {
		_, err := db.ExecContext(ctx, `UPDATE external_integration_outbox SET delivery_status='delivered',delivered_at=CURRENT_TIMESTAMP,last_error=NULL WHERE id=?`, event.ID)
		return err
	}
	nextAttempt := time.Now().UTC().Add(backoffForAttempt(event.AttemptCount))
	_, err := db.ExecContext(ctx, `UPDATE external_integration_outbox SET delivery_status='failed',next_attempt_at=?,last_error=? WHERE id=?`, nextAttempt, reason, event.ID)
	return err
}

func deliverExternalIntegrationOutbox(ctx context.Context, limit int) error {
	if limit <= 0 {
		return nil
	}
	for count := 0; count < limit; count++ {
		event, err := claimExternalOutboxEvent(ctx)
		if err != nil {
			return err
		}
		if event == nil {
			return nil
		}
		allDelivered := true
		var failures []string
		for _, sink := range event.RequiredSinks {
			reference, sinkErr := deliverExternalSink(ctx, event, sink)
			if sinkErr != nil {
				outcome := "failed"
				if !externalSinkStatus(sink) {
					outcome = "unavailable"
				}
				recordExternalDeliveryAttempt(ctx, event, sink, outcome, "", sinkErr)
				allDelivered = false
				failures = append(failures, sink+": "+sinkErr.Error())
				continue
			}
			recordExternalDeliveryAttempt(ctx, event, sink, "accepted", reference, nil)
		}
		if err := finalizeExternalOutboxEvent(ctx, event, allDelivered, strings.Join(failures, "; ")); err != nil {
			return err
		}
	}
	return nil
}

func handleExternalIntegrationDeliveryStatus(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "ict_officer", "security"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	rows, err := db.Query(`SELECT delivery_status,COUNT(*) FROM external_integration_outbox GROUP BY delivery_status`)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "query delivery status")
		return
	}
	defer rows.Close()
	counts := M{"pending": 0, "delivering": 0, "delivered": 0, "failed": 0, "quarantined": 0}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err == nil {
			counts[status] = count
		}
	}
	sinks := M{"kafka": externalSinkStatus("kafka"), "dapr": externalSinkStatus("dapr"), "fluvio": externalSinkStatus("fluvio"), "temporal": externalSinkStatus("temporal"), "opensearch": externalSinkStatus("opensearch")}
	writeJSON(w, http.StatusOK, M{"delivery": counts, "sinks": sinks})
}

func handleRetryExternalIntegrationDelivery(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "ict_officer"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	result, err := db.Exec(`UPDATE external_integration_outbox SET delivery_status='pending',next_attempt_at=CURRENT_TIMESTAMP,last_error=NULL WHERE id=? AND delivery_status IN ('failed','quarantined')`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "retry external delivery")
		return
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		writeError(w, http.StatusConflict, "delivery is not eligible for retry")
		return
	}
	writeJSON(w, http.StatusAccepted, M{"outbox_id": id, "status": "pending"})
}
