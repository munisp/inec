package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	fabricgateway "github.com/hyperledger/fabric-gateway/pkg/client"
	fabricgatewayhash "github.com/hyperledger/fabric-gateway/pkg/hash"
	fabricgatewayidentity "github.com/hyperledger/fabric-gateway/pkg/identity"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	fabricAnchorSchemaVersion = 1
	fabricAnchorTypeEvent     = "result_event"
	fabricAnchorTypeBundle    = "collation_bundle"
)

var (
	errFabricGatewayDisabled      = errors.New("Hyperledger Fabric anchoring is disabled")
	errFabricGatewayNotConfigured = errors.New("Hyperledger Fabric gateway is not configured")
	fabricGatewayClientFactory    = newFabricGatewayClient
	fabricAnchorWorkerStartOnce   sync.Once
	fabricAnchorWorkerStopOnce    sync.Once
	fabricAnchorWorkerStopChannel chan struct{}
)

type fabricEvidenceAnchor struct {
	SchemaVersion   int    `json:"schema_version"`
	AnchorID        string `json:"anchor_id"`
	AnchorType      string `json:"anchor_type"`
	ElectionID      int64  `json:"election_id"`
	ResultID        int64  `json:"result_id,omitempty"`
	EventHash       string `json:"event_hash"`
	PayloadSHA256   string `json:"payload_sha256"`
	PriorEventHash  string `json:"prior_event_hash,omitempty"`
	Signature       string `json:"signature"`
	SignerKeyID     string `json:"signer_key_id"`
	PolicyVersionID int64  `json:"policy_version_id,omitempty"`
	CreatedAt       string `json:"created_at"`
	CreatorMSP      string `json:"creator_msp,omitempty"`
	FabricTxID      string `json:"fabric_tx_id,omitempty"`
}

type fabricAnchorIDInput struct {
	SchemaVersion   int    `json:"schema_version"`
	AnchorType      string `json:"anchor_type"`
	ElectionID      int64  `json:"election_id"`
	ResultID        int64  `json:"result_id,omitempty"`
	EventHash       string `json:"event_hash"`
	PayloadSHA256   string `json:"payload_sha256"`
	PriorEventHash  string `json:"prior_event_hash,omitempty"`
	Signature       string `json:"signature"`
	SignerKeyID     string `json:"signer_key_id"`
	PolicyVersionID int64  `json:"policy_version_id,omitempty"`
	CreatedAt       string `json:"created_at"`
}

type fabricAnchorReceipt struct {
	AnchorID      string `json:"anchor_id"`
	FabricTxID    string `json:"fabric_tx_id"`
	ChannelID     string `json:"channel_id"`
	ChaincodeName string `json:"chaincode_name"`
	CreatorMSP    string `json:"creator_msp"`
	CreatedAt     string `json:"created_at"`
	Idempotent    bool   `json:"idempotent"`
}

type fabricAnchorRequest struct {
	ID                    int64
	ResultEvidenceEventID int64
	CollationBundleID     int64
	AnchorType            string
	AnchorID              string
	AnchorPayload         fabricEvidenceAnchor
	Status                string
	FabricChannel         string
	ChaincodeID           string
	ContractName          string
	TransactionID         string
	CommitStatus          string
	AttemptCount          int
	LastError             string
	CreatedAt             time.Time
	CommittedAt           *time.Time
}

type fabricGatewayConfig struct {
	Endpoint      string
	TLSServerName string
	TLSRootCert   string
	ClientCert    string
	ClientKey     string
	MSPID         string
	Channel       string
	Chaincode     string
	Contract      string
	EndorsingMSPs []string
}

type fabricGatewayClient interface {
	SubmitAnchor(context.Context, fabricEvidenceAnchor) (fabricAnchorReceipt, error)
	ReadAnchor(context.Context, string) (fabricEvidenceAnchor, error)
	ReadGovernance(context.Context) ([]string, error)
	Close() error
}

type fabricGatewayConnection struct {
	connection *grpc.ClientConn
	gateway    *fabricgateway.Gateway
	contract   *fabricgateway.Contract
	config     fabricGatewayConfig
}

func fabricAnchoringRequired() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("FABRIC_ANCHORING_REQUIRED")), "true")
}

func fabricAnchoringEnabled() bool {
	return fabricAnchoringRequired() || strings.EqualFold(strings.TrimSpace(os.Getenv("FABRIC_ANCHORING_ENABLED")), "true")
}

func fabricAnchorRetryInterval() time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv("FABRIC_ANCHOR_RETRY_SECONDS")))
	if err != nil || seconds < 5 || seconds > 3600 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

func fabricGatewayConfigFromEnv() fabricGatewayConfig {
	parseMSPs := func(raw string) []string {
		values := strings.Split(raw, ",")
		result := make([]string, 0, len(values))
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				result = append(result, value)
			}
		}
		sort.Strings(result)
		return result
	}
	return fabricGatewayConfig{
		Endpoint:      strings.TrimSpace(os.Getenv("FABRIC_GATEWAY_ENDPOINT")),
		TLSServerName: strings.TrimSpace(os.Getenv("FABRIC_TLS_SERVER_NAME")),
		TLSRootCert:   strings.TrimSpace(os.Getenv("FABRIC_TLS_ROOT_CERT_FILE")),
		ClientCert:    strings.TrimSpace(os.Getenv("FABRIC_CLIENT_CERT_FILE")),
		ClientKey:     strings.TrimSpace(os.Getenv("FABRIC_CLIENT_KEY_FILE")),
		MSPID:         strings.TrimSpace(os.Getenv("FABRIC_MSP_ID")),
		Channel:       strings.TrimSpace(os.Getenv("FABRIC_CHANNEL")),
		Chaincode:     strings.TrimSpace(os.Getenv("FABRIC_CHAINCODE")),
		Contract:      strings.TrimSpace(os.Getenv("FABRIC_CONTRACT")),
		EndorsingMSPs: parseMSPs(os.Getenv("FABRIC_ENDORSING_MSPS")),
	}
}

func (config fabricGatewayConfig) validate() error {
	if !fabricAnchoringEnabled() {
		return errFabricGatewayDisabled
	}
	for name, value := range map[string]string{
		"FABRIC_GATEWAY_ENDPOINT":   config.Endpoint,
		"FABRIC_TLS_ROOT_CERT_FILE": config.TLSRootCert,
		"FABRIC_CLIENT_CERT_FILE":   config.ClientCert,
		"FABRIC_CLIENT_KEY_FILE":    config.ClientKey,
		"FABRIC_MSP_ID":             config.MSPID,
		"FABRIC_CHANNEL":            config.Channel,
		"FABRIC_CHAINCODE":          config.Chaincode,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required when Fabric anchoring is enabled", name)
		}
	}
	return nil
}

func newFabricGatewayClient(ctx context.Context) (fabricGatewayClient, error) {
	config := fabricGatewayConfigFromEnv()
	if err := config.validate(); err != nil {
		return nil, err
	}

	rootPEM, err := os.ReadFile(config.TLSRootCert)
	if err != nil {
		return nil, fmt.Errorf("read Fabric TLS root certificate: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return nil, fmt.Errorf("parse Fabric TLS root certificate")
	}
	serverName := config.TLSServerName
	transport := credentials.NewClientTLSFromCert(roots, serverName)
	connection, err := grpc.NewClient(config.Endpoint, grpc.WithTransportCredentials(transport))
	if err != nil {
		return nil, fmt.Errorf("create Fabric gRPC connection: %w", err)
	}
	closeConnection := true
	defer func() {
		if closeConnection {
			_ = connection.Close()
		}
	}()

	certificatePEM, err := os.ReadFile(config.ClientCert)
	if err != nil {
		return nil, fmt.Errorf("read Fabric client certificate: %w", err)
	}
	certificate, err := fabricgatewayidentity.CertificateFromPEM(certificatePEM)
	if err != nil {
		return nil, fmt.Errorf("parse Fabric client certificate: %w", err)
	}
	identity, err := fabricgatewayidentity.NewX509Identity(config.MSPID, certificate)
	if err != nil {
		return nil, fmt.Errorf("create Fabric client identity: %w", err)
	}
	privateKeyPEM, err := os.ReadFile(config.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("read Fabric client key: %w", err)
	}
	privateKey, err := fabricgatewayidentity.PrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse Fabric client key: %w", err)
	}
	sign, err := fabricgatewayidentity.NewPrivateKeySign(privateKey)
	if err != nil {
		return nil, fmt.Errorf("create Fabric request signer: %w", err)
	}
	gateway, err := fabricgateway.Connect(identity,
		fabricgateway.WithSign(sign),
		fabricgateway.WithHash(fabricgatewayhash.SHA256),
		fabricgateway.WithClientConnection(connection),
		fabricgateway.WithEvaluateTimeout(10*time.Second),
		fabricgateway.WithEndorseTimeout(15*time.Second),
		fabricgateway.WithSubmitTimeout(10*time.Second),
		fabricgateway.WithCommitStatusTimeout(30*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("connect Fabric Gateway: %w", err)
	}
	contract := gateway.GetNetwork(config.Channel).GetContract(config.Chaincode)
	if config.Contract != "" {
		contract = gateway.GetNetwork(config.Channel).GetContractWithName(config.Chaincode, config.Contract)
	}
	closeConnection = false
	return &fabricGatewayConnection{connection: connection, gateway: gateway, contract: contract, config: config}, nil
}

func (client *fabricGatewayConnection) SubmitAnchor(ctx context.Context, anchor fabricEvidenceAnchor) (fabricAnchorReceipt, error) {
	payload, err := canonicalJSON(anchor)
	if err != nil {
		return fabricAnchorReceipt{}, fmt.Errorf("encode Fabric anchor: %w", err)
	}
	options := []fabricgateway.ProposalOption{fabricgateway.WithArguments(string(payload))}
	if len(client.config.EndorsingMSPs) > 0 {
		options = append(options, fabricgateway.WithEndorsingOrganizations(client.config.EndorsingMSPs...))
	}
	response, err := client.contract.SubmitWithContext(ctx, "CreateAnchor", options...)
	if err != nil {
		return fabricAnchorReceipt{}, fmt.Errorf("submit Fabric anchor: %w", err)
	}
	var receipt fabricAnchorReceipt
	if err := json.Unmarshal(response, &receipt); err != nil {
		return fabricAnchorReceipt{}, fmt.Errorf("decode Fabric anchor receipt: %w", err)
	}
	if receipt.AnchorID != anchor.AnchorID || receipt.FabricTxID == "" || receipt.ChannelID != client.config.Channel {
		return fabricAnchorReceipt{}, fmt.Errorf("Fabric anchor receipt is incomplete or mismatched")
	}
	return receipt, nil
}

func (client *fabricGatewayConnection) ReadAnchor(ctx context.Context, anchorID string) (fabricEvidenceAnchor, error) {
	response, err := client.contract.EvaluateWithContext(ctx, "ReadAnchor", fabricgateway.WithArguments(anchorID))
	if err != nil {
		return fabricEvidenceAnchor{}, fmt.Errorf("read Fabric anchor: %w", err)
	}
	var anchor fabricEvidenceAnchor
	if err := json.Unmarshal(response, &anchor); err != nil {
		return fabricEvidenceAnchor{}, fmt.Errorf("decode Fabric anchor: %w", err)
	}
	return anchor, nil
}

func (client *fabricGatewayConnection) ReadGovernance(ctx context.Context) ([]string, error) {
	response, err := client.contract.EvaluateWithContext(ctx, "ReadGovernance")
	if err != nil {
		return nil, fmt.Errorf("read Fabric governance: %w", err)
	}
	var governance struct {
		ConsortiumMSPs []string `json:"consortium_msps"`
	}
	if err := json.Unmarshal(response, &governance); err != nil {
		return nil, fmt.Errorf("decode Fabric governance: %w", err)
	}
	if len(governance.ConsortiumMSPs) < 2 {
		return nil, fmt.Errorf("Fabric governance has insufficient consortium members")
	}
	return governance.ConsortiumMSPs, nil
}

func (client *fabricGatewayConnection) Close() error {
	if client.gateway != nil {
		client.gateway.Close()
	}
	if client.connection != nil {
		return client.connection.Close()
	}
	return nil
}

func calculateFabricAnchorID(anchor fabricEvidenceAnchor) (string, error) {
	canonical, err := canonicalJSON(fabricAnchorIDInput{
		SchemaVersion:   anchor.SchemaVersion,
		AnchorType:      anchor.AnchorType,
		ElectionID:      anchor.ElectionID,
		ResultID:        anchor.ResultID,
		EventHash:       anchor.EventHash,
		PayloadSHA256:   anchor.PayloadSHA256,
		PriorEventHash:  anchor.PriorEventHash,
		Signature:       anchor.Signature,
		SignerKeyID:     anchor.SignerKeyID,
		PolicyVersionID: anchor.PolicyVersionID,
		CreatedAt:       anchor.CreatedAt,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func initFabricAnchorSchema() {
	if usePostgres || strings.EqualFold(os.Getenv("APP_ENV"), "production") {
		return
	}
	schema := `
		CREATE TABLE IF NOT EXISTS fabric_anchor_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			result_evidence_event_id INTEGER,
			collation_evidence_bundle_id INTEGER,
			anchor_type TEXT NOT NULL,
			anchor_id TEXT NOT NULL UNIQUE,
			anchor_payload TEXT NOT NULL,
			anchor_payload_sha256 TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			fabric_channel TEXT,
			chaincode_id TEXT,
			contract_name TEXT,
			transaction_id TEXT,
			commit_status TEXT,
			receipt_json TEXT,
			receipt_sha256 TEXT,
			last_error TEXT,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			last_attempt_at TIMESTAMP,
			committed_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			CHECK ((result_evidence_event_id IS NOT NULL) != (collation_evidence_bundle_id IS NOT NULL))
		);
		CREATE TABLE IF NOT EXISTS fabric_anchor_attempts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fabric_anchor_request_id INTEGER NOT NULL,
			attempt_no INTEGER NOT NULL,
			attempted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			gateway_endpoint TEXT,
			channel_id TEXT,
			chaincode_id TEXT,
			transaction_id TEXT,
			commit_status TEXT NOT NULL,
			receipt_json TEXT,
			diagnostic TEXT,
			UNIQUE(fabric_anchor_request_id, attempt_no)
		);
		CREATE UNIQUE INDEX IF NOT EXISTS uq_fabric_anchor_event_source ON fabric_anchor_requests(result_evidence_event_id) WHERE result_evidence_event_id IS NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS uq_fabric_anchor_bundle_source ON fabric_anchor_requests(collation_evidence_bundle_id) WHERE collation_evidence_bundle_id IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_fabric_anchor_requests_status ON fabric_anchor_requests(status, created_at);
	`
	execMulti(db, schema)
	for _, statement := range []string{
		`CREATE TRIGGER IF NOT EXISTS trg_fabric_anchor_no_identity_update BEFORE UPDATE ON fabric_anchor_requests WHEN OLD.anchor_id != NEW.anchor_id OR OLD.anchor_payload != NEW.anchor_payload OR OLD.anchor_payload_sha256 != NEW.anchor_payload_sha256 BEGIN SELECT RAISE(ABORT, 'Fabric anchor canonical payload is immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_fabric_anchor_committed_no_update BEFORE UPDATE ON fabric_anchor_requests WHEN OLD.status = 'committed' BEGIN SELECT RAISE(ABORT, 'committed Fabric anchor is immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_fabric_anchor_attempt_no_update BEFORE UPDATE ON fabric_anchor_attempts BEGIN SELECT RAISE(ABORT, 'Fabric anchor attempts are append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_fabric_anchor_attempt_no_delete BEFORE DELETE ON fabric_anchor_attempts BEGIN SELECT RAISE(ABORT, 'Fabric anchor attempts are append-only'); END`,
	} {
		if _, err := db.Exec(statement); err != nil {
			log.Warn().Err(err).Msg("failed to install SQLite Fabric anchor immutability trigger")
		}
	}
}

func queueFabricAnchorForEventTx(ctx context.Context, tx *sql.Tx, eventID int64, record integrityEventRecord, input integrityEventInput) (string, error) {
	if !fabricAnchoringEnabled() {
		return "", nil
	}
	if record.Signature == "" || record.SignerStatus != "signed" || record.SignerKeyID == "" {
		if fabricAnchoringRequired() {
			return "", fmt.Errorf("Fabric anchoring requires a signed integrity event")
		}
		return "", nil
	}
	var electionID int64
	var createdAt time.Time
	if err := tx.QueryRowContext(ctx, convertPlaceholders(`SELECT r.election_id, e.created_at
		FROM result_evidence_events e JOIN results r ON r.id=e.result_id WHERE e.id=?`), eventID).Scan(&electionID, &createdAt); err != nil {
		return "", fmt.Errorf("load event for Fabric anchor: %w", err)
	}
	anchor := fabricEvidenceAnchor{
		SchemaVersion:   fabricAnchorSchemaVersion,
		AnchorType:      fabricAnchorTypeEvent,
		ElectionID:      electionID,
		ResultID:        input.ResultID,
		EventHash:       record.EventHash,
		PayloadSHA256:   record.PayloadSHA256,
		PriorEventHash:  record.PriorEventHash,
		Signature:       record.Signature,
		SignerKeyID:     record.SignerKeyID,
		PolicyVersionID: input.PolicyVersionID,
		CreatedAt:       createdAt.UTC().Format(time.RFC3339Nano),
	}
	anchorID, err := calculateFabricAnchorID(anchor)
	if err != nil {
		return "", fmt.Errorf("calculate Fabric anchor ID: %w", err)
	}
	anchor.AnchorID = anchorID
	payload, err := canonicalJSON(anchor)
	if err != nil {
		return "", fmt.Errorf("encode Fabric anchor payload: %w", err)
	}
	config := fabricGatewayConfigFromEnv()
	status := "pending"
	if err := config.validate(); err != nil {
		status = "unavailable"
	}
	_, err = tx.ExecContext(ctx, convertPlaceholders(`INSERT INTO fabric_anchor_requests
		(result_evidence_event_id, anchor_type, anchor_id, anchor_payload, anchor_payload_sha256, status,
		 fabric_channel, chaincode_id, contract_name, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(anchor_id) DO NOTHING`),
		eventID, fabricAnchorTypeEvent, anchorID, string(payload), sha256Hex(payload), status,
		nullStringArg(config.Channel), nullStringArg(config.Chaincode), nullStringArg(config.Contract),
		nullStringArg(func() string {
			if status == "unavailable" {
				return "Fabric Gateway configuration is unavailable"
			}
			return ""
		}()),
	)
	if err != nil {
		return "", fmt.Errorf("enqueue Fabric anchor: %w", err)
	}
	return anchorID, nil
}

func scanFabricAnchorRequest(row interface{ Scan(...interface{}) error }) (fabricAnchorRequest, error) {
	var request fabricAnchorRequest
	var payload string
	var committedAt sql.NullTime
	err := row.Scan(
		&request.ID, &request.ResultEvidenceEventID, &request.CollationBundleID, &request.AnchorType, &request.AnchorID,
		&payload, &request.Status, &request.FabricChannel, &request.ChaincodeID, &request.ContractName,
		&request.TransactionID, &request.CommitStatus, &request.AttemptCount, &request.LastError, &request.CreatedAt, &committedAt,
	)
	if err != nil {
		return fabricAnchorRequest{}, err
	}
	if err := json.Unmarshal([]byte(payload), &request.AnchorPayload); err != nil {
		return fabricAnchorRequest{}, fmt.Errorf("decode Fabric anchor payload: %w", err)
	}
	if committedAt.Valid {
		value := committedAt.Time
		request.CommittedAt = &value
	}
	return request, nil
}

func getFabricAnchorRequest(ctx context.Context, requestID int64) (fabricAnchorRequest, error) {
	row := dbQueryRowCtx(ctx, convertPlaceholders(`SELECT id, COALESCE(result_evidence_event_id,0), COALESCE(collation_evidence_bundle_id,0),
		anchor_type, anchor_id, anchor_payload, status, COALESCE(fabric_channel,''), COALESCE(chaincode_id,''),
		COALESCE(contract_name,''), COALESCE(transaction_id,''), COALESCE(commit_status,''), attempt_count,
		COALESCE(last_error,''), created_at, committed_at
		FROM fabric_anchor_requests WHERE id=?`), requestID)
	request, err := scanFabricAnchorRequest(row)
	if err == sql.ErrNoRows {
		return fabricAnchorRequest{}, fmt.Errorf("Fabric anchor request not found")
	}
	return request, err
}

func submitFabricAnchorRequest(ctx context.Context, requestID int64) (fabricAnchorRequest, error) {
	request, err := getFabricAnchorRequest(ctx, requestID)
	if err != nil {
		return fabricAnchorRequest{}, err
	}
	if request.Status == "committed" {
		return request, nil
	}
	client, err := fabricGatewayClientFactory(ctx)
	if err != nil {
		return recordFabricAnchorOutcome(ctx, request, fabricAnchorReceipt{}, "unavailable", err)
	}
	defer client.Close()
	receipt, submitErr := client.SubmitAnchor(ctx, request.AnchorPayload)
	if submitErr != nil {
		return recordFabricAnchorOutcome(ctx, request, fabricAnchorReceipt{}, "failed", submitErr)
	}
	if receipt.AnchorID != request.AnchorID {
		return recordFabricAnchorOutcome(ctx, request, fabricAnchorReceipt{}, "failed", fmt.Errorf("Fabric receipt anchor ID does not match queued commitment"))
	}
	return recordFabricAnchorOutcome(ctx, request, receipt, "committed", nil)
}

func recordFabricAnchorOutcome(ctx context.Context, request fabricAnchorRequest, receipt fabricAnchorReceipt, status string, submissionErr error) (fabricAnchorRequest, error) {
	if request.Status == "committed" {
		return request, nil
	}
	config := fabricGatewayConfigFromEnv()
	attemptNo := request.AttemptCount + 1
	var diagnostic string
	if submissionErr != nil {
		diagnostic = submissionErr.Error()
	}
	var receiptJSON []byte
	var receiptHash string
	if status == "committed" {
		var err error
		receiptJSON, err = canonicalJSON(receipt)
		if err != nil {
			return fabricAnchorRequest{}, err
		}
		receiptHash = sha256Hex(receiptJSON)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fabricAnchorRequest{}, err
	}
	defer tx.Rollback()
	if status == "committed" {
		_, err = tx.ExecContext(ctx, convertPlaceholders(`UPDATE fabric_anchor_requests SET status=?, fabric_channel=?, chaincode_id=?, contract_name=?,
			transaction_id=?, commit_status=?, receipt_json=?, receipt_sha256=?, last_error=NULL, attempt_count=?,
			last_attempt_at=CURRENT_TIMESTAMP, committed_at=CURRENT_TIMESTAMP WHERE id=? AND status <> 'committed'`),
			status, receipt.ChannelID, receipt.ChaincodeName, config.Contract, receipt.FabricTxID, "VALID", string(receiptJSON), receiptHash, attemptNo, request.ID)
	} else {
		_, err = tx.ExecContext(ctx, convertPlaceholders(`UPDATE fabric_anchor_requests SET status=?, last_error=?, attempt_count=?,
			last_attempt_at=CURRENT_TIMESTAMP WHERE id=? AND status <> 'committed'`), status, diagnostic, attemptNo, request.ID)
	}
	if err != nil {
		return fabricAnchorRequest{}, err
	}
	_, err = tx.ExecContext(ctx, convertPlaceholders(`INSERT INTO fabric_anchor_attempts
		(fabric_anchor_request_id, attempt_no, gateway_endpoint, channel_id, chaincode_id, transaction_id, commit_status, receipt_json, diagnostic)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`), request.ID, attemptNo, nullStringArg(config.Endpoint),
		nullStringArg(config.Channel), nullStringArg(config.Chaincode), nullStringArg(receipt.FabricTxID), status,
		nullStringArg(string(receiptJSON)), nullStringArg(diagnostic))
	if err != nil {
		return fabricAnchorRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return fabricAnchorRequest{}, err
	}
	return getFabricAnchorRequest(ctx, request.ID)
}

func processPendingFabricAnchors(ctx context.Context, limit int) {
	if !fabricAnchoringEnabled() || limit <= 0 {
		return
	}
	rows, err := dbQueryCtx(ctx, convertPlaceholders(`SELECT id FROM fabric_anchor_requests
		WHERE status IN ('pending','failed','unavailable') ORDER BY created_at ASC LIMIT ?`), limit)
	if err != nil {
		log.Warn().Err(err).Msg("failed to load pending Fabric anchors")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var requestID int64
		if err := rows.Scan(&requestID); err != nil {
			continue
		}
		requestCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		_, err := submitFabricAnchorRequest(requestCtx, requestID)
		cancel()
		if err != nil {
			log.Warn().Err(err).Int64("fabric_anchor_request_id", requestID).Msg("Fabric anchor submission did not commit")
		}
	}
}

func startFabricAnchorWorker() {
	if !fabricAnchoringEnabled() {
		return
	}
	fabricAnchorWorkerStartOnce.Do(func() {
		fabricAnchorWorkerStopChannel = make(chan struct{})
		go func() {
			processPendingFabricAnchors(context.Background(), 20)
			ticker := time.NewTicker(fabricAnchorRetryInterval())
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					processPendingFabricAnchors(context.Background(), 20)
				case <-fabricAnchorWorkerStopChannel:
					return
				}
			}
		}()
	})
}

func stopFabricAnchorWorker() {
	fabricAnchorWorkerStopOnce.Do(func() {
		if fabricAnchorWorkerStopChannel != nil {
			close(fabricAnchorWorkerStopChannel)
		}
	})
}

func fabricAnchorHealth(ctx context.Context) M {
	config := fabricGatewayConfigFromEnv()
	result := M{
		"enabled":  fabricAnchoringEnabled(),
		"required": fabricAnchoringRequired(),
		"status":   "disabled",
	}
	if !fabricAnchoringEnabled() {
		return result
	}
	if err := config.validate(); err != nil {
		result["status"] = "unavailable"
		result["reason"] = err.Error()
		return result
	}
	var pending, failed, unavailable int
	if err := dbQueryRowCtx(ctx, `SELECT COUNT(*) FROM fabric_anchor_requests WHERE status='pending'`).Scan(&pending); err != nil {
		result["status"] = "unavailable"
		result["reason"] = "Fabric anchor status is unavailable"
		return result
	}
	_ = dbQueryRowCtx(ctx, `SELECT COUNT(*) FROM fabric_anchor_requests WHERE status='failed'`).Scan(&failed)
	_ = dbQueryRowCtx(ctx, `SELECT COUNT(*) FROM fabric_anchor_requests WHERE status='unavailable'`).Scan(&unavailable)
	result["pending"] = pending
	result["failed"] = failed
	result["unavailable"] = unavailable
	result["channel"] = config.Channel
	result["chaincode"] = config.Chaincode
	client, err := fabricGatewayClientFactory(ctx)
	if err != nil {
		result["status"] = "unavailable"
		result["reason"] = err.Error()
		return result
	}
	defer client.Close()
	MSPs, err := client.ReadGovernance(ctx)
	if err != nil {
		result["status"] = "unavailable"
		result["reason"] = err.Error()
		return result
	}
	result["consortium_msps"] = MSPs
	if fabricAnchoringRequired() && (pending > 0 || failed > 0 || unavailable > 0) {
		result["status"] = "degraded"
		return result
	}
	result["status"] = "healthy"
	return result
}

func listFabricAnchorsForResult(ctx context.Context, resultID int64) ([]M, error) {
	rows, err := dbQueryCtx(ctx, convertPlaceholders(`SELECT id, anchor_id, anchor_type, status, COALESCE(fabric_channel,''),
		COALESCE(chaincode_id,''), COALESCE(contract_name,''), COALESCE(transaction_id,''), COALESCE(commit_status,''),
		COALESCE(receipt_sha256,''), attempt_count, COALESCE(last_error,''), created_at, committed_at
		FROM fabric_anchor_requests WHERE result_evidence_event_id IN
		(SELECT id FROM result_evidence_events WHERE result_id=?) ORDER BY created_at ASC, id ASC`), resultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	anchors := make([]M, 0)
	for rows.Next() {
		var id int64
		var anchorID, anchorType, status, channel, chaincode, contract, txID, commitStatus, receiptSHA, lastError string
		var attempts int
		var createdAt time.Time
		var committedAt sql.NullTime
		if err := rows.Scan(&id, &anchorID, &anchorType, &status, &channel, &chaincode, &contract, &txID,
			&commitStatus, &receiptSHA, &attempts, &lastError, &createdAt, &committedAt); err != nil {
			return nil, err
		}
		item := M{
			"id": id, "anchor_id": anchorID, "anchor_type": anchorType, "status": status,
			"channel": channel, "chaincode": chaincode, "contract": contract, "transaction_id": txID,
			"commit_status": commitStatus, "receipt_sha256": receiptSHA, "attempt_count": attempts,
			"last_error": lastError, "created_at": createdAt.UTC().Format(time.RFC3339),
		}
		if committedAt.Valid {
			item["committed_at"] = committedAt.Time.UTC().Format(time.RFC3339)
		}
		anchors = append(anchors, item)
	}
	return anchors, rows.Err()
}

func handleFabricAnchorHealth(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "presiding_officer", "collation_officer", "observer"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	status := fabricAnchorHealth(r.Context())
	code := http.StatusOK
	if status["status"] == "unavailable" || (fabricAnchoringRequired() && status["status"] != "healthy") {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, status)
}

func handleFabricAnchorRetry(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "collation_officer"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	requestID, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || requestID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid Fabric anchor request id")
		return
	}
	requestCtx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	request, err := submitFabricAnchorRequest(requestCtx, requestID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Fabric anchor submission failed: "+err.Error())
		return
	}
	status := http.StatusAccepted
	if request.Status == "committed" {
		status = http.StatusOK
	}
	writeJSON(w, status, publicFabricAnchorRequest(request))
}

func publicFabricAnchorRequest(request fabricAnchorRequest) M {
	item := M{
		"id": request.ID, "anchor_id": request.AnchorID, "anchor_type": request.AnchorType,
		"status": request.Status, "channel": request.FabricChannel, "chaincode": request.ChaincodeID,
		"contract": request.ContractName, "transaction_id": request.TransactionID,
		"commit_status": request.CommitStatus, "attempt_count": request.AttemptCount,
		"created_at": request.CreatedAt.UTC().Format(time.RFC3339),
	}
	if request.CommittedAt != nil {
		item["committed_at"] = request.CommittedAt.UTC().Format(time.RFC3339)
	}
	if request.Status != "committed" && request.LastError != "" {
		item["diagnostic"] = request.LastError
	}
	return item
}
