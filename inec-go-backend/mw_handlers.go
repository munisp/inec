package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

func handleMiddlewareStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, M{
		"middleware": mwHub.GetAllStatus(),
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	})
}

func handleMiddlewareHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mwHub.HealthCheck())
}

func handleKafkaTopics(w http.ResponseWriter, r *http.Request) {
	topics := []string{
		TopicResultSubmitted, TopicResultValidated, TopicResultFinalized,
		TopicResultDisputed, TopicAuditLog, TopicIncidentReport, TopicFluvioIngest,
	}
	writeJSON(w, 200, M{"topics": topics, "status": mwHub.Kafka.Status()})
}

func handleTemporalWorkflows(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, M{"status": mwHub.Temporal.Status()})
}

func handleTemporalWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	ctx := r.Context()
	ws, err := mwHub.Temporal.GetWorkflowStatus(ctx, id)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, ws)
}

// handleTemporalStartWorkflow starts a workflow through the configured Temporal
// client. It accepts both the canonical WorkflowInput fields and the legacy web
// client shape (workflow + payload) while normalizing requests to a safe,
// registered workflow type.
func handleTemporalStartWorkflow(w http.ResponseWriter, r *http.Request) {
	if mwHub == nil || mwHub.Temporal == nil {
		writeError(w, http.StatusServiceUnavailable, "Temporal workflow engine is unavailable")
		return
	}

	var req struct {
		WorkflowID   string                 `json:"workflow_id"`
		WorkflowType string                 `json:"workflow_type"`
		Workflow     string                 `json:"workflow"`
		TaskQueue    string                 `json:"task_queue"`
		Input        map[string]interface{} `json:"input"`
		Payload      map[string]interface{} `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.WorkflowType == "" {
		req.WorkflowType = req.Workflow
	}
	if req.WorkflowType == "" {
		writeError(w, http.StatusBadRequest, "workflow_type is required")
		return
	}
	if req.Input == nil {
		req.Input = req.Payload
	}
	if req.Input == nil {
		req.Input = map[string]interface{}{}
	}
	if req.WorkflowID == "" {
		req.WorkflowID = fmt.Sprintf("api-%s-%d", req.WorkflowType, time.Now().UTC().UnixNano())
	}
	if req.TaskQueue == "" {
		req.TaskQueue = temporalSDKTaskQueue
	}

	// The in-process SDK worker only registers these workflow names. Preserve an
	// arbitrary client label as input to GenericWorkflow rather than attempting to
	// execute an unregistered Temporal workflow type.
	switch req.WorkflowType {
	case "ResultSubmissionWorkflow", "ResultValidationWorkflow", "ResultFinalizationWorkflow", "GenericWorkflow":
	default:
		req.Input["requested_workflow_type"] = req.WorkflowType
		req.WorkflowType = "GenericWorkflow"
	}

	workflow, err := mwHub.Temporal.StartWorkflow(r.Context(), WorkflowInput{
		WorkflowID:   req.WorkflowID,
		WorkflowType: req.WorkflowType,
		TaskQueue:    req.TaskQueue,
		Input:        req.Input,
		RetryPolicy:  DefaultRetryPolicy,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, workflow)
}

func handleTBAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accounts := []string{"inec-operational", "inec-official"}
	result := make([]interface{}, 0)
	for _, id := range accounts {
		acct, err := mwHub.TigerBeetle.GetAccount(ctx, id)
		if err == nil {
			result = append(result, acct)
		}
	}
	writeJSON(w, 200, M{"accounts": result, "status": mwHub.TigerBeetle.Status()})
}

func handleTBTransfers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accountID := queryParam(r, "account_id", "inec-operational")
	limit := queryParamInt(r, "limit", 50)
	transfers, err := mwHub.TigerBeetle.LookupTransfers(ctx, accountID, limit)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, M{"transfers": transfers, "status": mwHub.TigerBeetle.Status()})
}

func handleAPISIXRoutes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	routes, err := mwHub.APISIX.GetRoutes(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, M{"routes": routes, "status": mwHub.APISIX.Status()})
}

func handleAPISIXConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mwHub.APISIX.GetConfig())
}

func handlePermifyCheck(w http.ResponseWriter, r *http.Request) {
	var check PermifyCheck
	if err := json.NewDecoder(r.Body).Decode(&check); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if check.ResourceType == "" || check.Resource == "" || check.Permission == "" {
		writeError(w, http.StatusBadRequest, "resource_type, resource, and permission are required")
		return
	}

	// Do not accept a client-supplied authorization subject. The resource and
	// requested permission are client input, but identity must come from the
	// authenticated access token to prevent subject impersonation.
	claims, err := getCurrentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	subject, ok := claims["sub"].(string)
	if !ok || subject == "" {
		writeError(w, http.StatusUnauthorized, "authenticated subject is missing")
		return
	}
	check.Subject = subject
	check.SubjectType = "user"

	allowed, err := mwHub.Permify.Check(r.Context(), check)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, M{"allowed": allowed, "check": check})
}

func handleFluvioTopics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, M{"status": mwHub.Fluvio.Status()})
}

func handleFluvioConsume(w http.ResponseWriter, r *http.Request) {
	topic := mux.Vars(r)["topic"]
	offset := int64(queryParamInt(r, "offset", 0))
	limit := queryParamInt(r, "limit", 50)
	ctx := r.Context()
	records, err := mwHub.Fluvio.Consume(ctx, topic, offset, limit)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, M{"records": records, "topic": topic})
}

func handleLakehouseAnalytics(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	eidStr := vars["election_id"]
	analysisType := vars["type"]
	eid, _ := strconv.Atoi(eidStr)
	ctx := r.Context()
	result, err := mwHub.Lakehouse.GetAnalytics(ctx, eid, analysisType)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, result)
}

func handleLakehouseTables(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tables, err := mwHub.Lakehouse.GetTables(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, M{"tables": tables, "status": mwHub.Lakehouse.Status()})
}

func handleRedisStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, M{"status": mwHub.Redis.Ping()})
}

func publishResultEvent(topic string, resultID int64, puCode string, electionID int, userID int, extra map[string]interface{}) {
	ctx := context.Background()
	event := map[string]interface{}{
		"result_id":   resultID,
		"pu_code":     puCode,
		"election_id": electionID,
		"user_id":     userID,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	for k, v := range extra {
		event[k] = v
	}
	mwHub.Kafka.Produce(ctx, KafkaMessage{
		Topic: topic,
		Key:   fmt.Sprintf("result-%d", resultID),
		Value: event,
	})
	mwHub.Fluvio.Produce(ctx, TopicFluvioIngest, FluvioRecord{
		Key:   fmt.Sprintf("result-%d", resultID),
		Value: event,
	})
	mwHub.Dapr.PublishEvent(ctx, "inec-pubsub", topic, event)
}

func publishAuditEvent(action, entityType, entityID string, userID int, details map[string]interface{}) {
	ctx := context.Background()
	event := map[string]interface{}{
		"action":      action,
		"entity_type": entityType,
		"entity_id":   entityID,
		"user_id":     userID,
		"details":     details,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	mwHub.Kafka.Produce(ctx, KafkaMessage{
		Topic: TopicAuditLog,
		Key:   fmt.Sprintf("%s-%s", entityType, entityID),
		Value: event,
	})
}

func cacheGet(key string) (string, error) {
	ctx := context.Background()
	return mwHub.Redis.Get(ctx, key)
}

func cacheSet(key string, value interface{}, ttl time.Duration) {
	ctx := context.Background()
	data, _ := json.Marshal(value)
	mwHub.Redis.Set(ctx, key, string(data), ttl)
}

func cacheDel(keys ...string) {
	ctx := context.Background()
	mwHub.Redis.Del(ctx, keys...)
}

func checkPermission(role, permission string) bool {
	ctx := context.Background()
	allowed, _ := mwHub.Permify.Check(ctx, PermifyCheck{
		Subject:      role,
		SubjectType:  role,
		Permission:   permission,
		Resource:     "*",
		ResourceType: "election",
	})
	return allowed
}

func createTBTransfer(resultID int64, amount int64, userData string) (*TBTransfer, error) {
	if mwHub == nil || mwHub.TigerBeetle == nil {
		return nil, fmt.Errorf("TigerBeetle client is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return mwHub.TigerBeetle.CreateTransfer(ctx, TBTransfer{
		DebitAccountID:  "inec-operational",
		CreditAccountID: "inec-official",
		Amount:          amount,
		Ledger:          1,
		Code:            1,
		Status:          "PENDING",
		UserData:        userData,
		IdempotencyKey:  fmt.Sprintf("result:%d:%s", resultID, userData),
	})
}

func postTBTransfer(transferID string) error {
	if mwHub == nil || mwHub.TigerBeetle == nil {
		return fmt.Errorf("TigerBeetle client is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return mwHub.TigerBeetle.PostTransfer(ctx, transferID)
}

func voidTBTransfer(transferID string) error {
	if mwHub == nil || mwHub.TigerBeetle == nil {
		return fmt.Errorf("TigerBeetle client is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return mwHub.TigerBeetle.VoidTransfer(ctx, transferID)
}

func startResultWorkflow(workflowType string, resultID int64, data map[string]interface{}) *WorkflowStatus {
	ctx := context.Background()
	ws, _ := mwHub.Temporal.StartWorkflow(ctx, WorkflowInput{
		WorkflowID:   fmt.Sprintf("%s-%d-%d", workflowType, resultID, time.Now().UnixNano()),
		WorkflowType: workflowType,
		TaskQueue:    "inec-results",
		Input:        data,
	})
	return ws
}
