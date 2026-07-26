package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

const (
	operationalSettlementSourceType    = "settlement"
	operationalSettlementAggregateType = "operational_settlement"
)

type operationalSettlementRequest struct {
	ElectionID         int    `json:"election_id"`
	DeviceID           string `json:"device_id"`
	CommitmentKind     string `json:"commitment_kind"`
	AmountMinor        int64  `json:"amount_minor"`
	Currency           string `json:"currency"`
	RecipientReference string `json:"recipient_reference"`
	EvidenceSHA256     string `json:"evidence_sha256"`
	PurposeReference   string `json:"purpose_reference"`
	IdempotencyKey     string `json:"idempotency_key"`
}

type operationalSettlementDispatchRequest struct {
	// RecipientReference is intentionally used only for the outbound FSPIOP call.
	// PostgreSQL, TigerBeetle, external outboxes, logs, and lakehouse records retain
	// only the SHA-256 binding recorded at request creation.
	RecipientReference string `json:"recipient_reference"`
}

type operationalSettlementRecord struct {
	ID                       string `json:"id"`
	ElectionID               int    `json:"election_id"`
	DeviceID                 string `json:"device_id"`
	CommitmentKind           string `json:"commitment_kind"`
	AmountMinor              int64  `json:"amount_minor"`
	Currency                 string `json:"currency"`
	RecipientReferenceSHA256 string `json:"recipient_reference_sha256"`
	EvidenceSHA256           string `json:"evidence_sha256"`
	PurposeReferenceSHA256   string `json:"purpose_reference_sha256"`
	IdempotencyKey           string `json:"idempotency_key"`
	RequestedBy              string `json:"requested_by"`
	ApprovedBy               string `json:"approved_by,omitempty"`
	ApprovedAt               string `json:"approved_at,omitempty"`
	Status                   string `json:"status"`
	TigerBeetleDebitAccount  string `json:"tigerbeetle_debit_account_id"`
	TigerBeetleCreditAccount string `json:"tigerbeetle_credit_account_id"`
	TigerBeetleTransferID    string `json:"tigerbeetle_transfer_id,omitempty"`
	MojaloopQuoteID          string `json:"mojaloop_quote_id,omitempty"`
	MojaloopTransferID       string `json:"mojaloop_transfer_id,omitempty"`
	ExternalReceiptSHA256    string `json:"external_receipt_sha256,omitempty"`
	FailureCode              string `json:"failure_code,omitempty"`
	FailureDetailRedacted    string `json:"failure_detail_redacted,omitempty"`
	CreatedAt                string `json:"created_at"`
	UpdatedAt                string `json:"updated_at"`
}

type operationalSettlementAudit struct {
	Action         string                 `json:"action"`
	Actor          string                 `json:"actor"`
	EvidenceSHA256 string                 `json:"evidence_sha256"`
	Details        map[string]interface{} `json:"details"`
}

type operationalSettlementHTTPError struct {
	Status int
	Err    error
}

func (e *operationalSettlementHTTPError) Error() string { return e.Err.Error() }

func settlementHTTPError(status int, format string, args ...interface{}) error {
	return &operationalSettlementHTTPError{Status: status, Err: fmt.Errorf(format, args...)}
}

func operationalSettlementStatus(err error) int {
	if typed, ok := err.(*operationalSettlementHTTPError); ok {
		return typed.Status
	}
	return http.StatusInternalServerError
}

func initOperationalSettlementSchema(database *sql.DB) {
	if usePostgres || strings.EqualFold(os.Getenv("APP_ENV"), "production") {
		// Migration 000025 is authoritative in PostgreSQL production.
		return
	}
	schema := `
	CREATE TABLE IF NOT EXISTS operational_settlement_commitments (
		id TEXT PRIMARY KEY,
		election_id INTEGER NOT NULL,
		device_id TEXT NOT NULL,
		commitment_kind TEXT NOT NULL,
		amount_minor INTEGER NOT NULL,
		currency TEXT NOT NULL,
		recipient_reference_sha256 TEXT NOT NULL,
		evidence_sha256 TEXT NOT NULL,
		purpose_reference_sha256 TEXT NOT NULL,
		idempotency_key TEXT NOT NULL UNIQUE,
		requested_by TEXT NOT NULL,
		approved_by TEXT,
		approved_at TIMESTAMP,
		status TEXT NOT NULL DEFAULT 'requested',
		tigerbeetle_debit_account_id TEXT NOT NULL,
		tigerbeetle_credit_account_id TEXT NOT NULL,
		tigerbeetle_transfer_id TEXT UNIQUE,
		mojaloop_quote_id TEXT,
		mojaloop_transfer_id TEXT,
		external_receipt_sha256 TEXT,
		failure_code TEXT,
		failure_detail_redacted TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		CHECK (commitment_kind IN ('device_logistics','device_reimbursement','device_repair')),
		CHECK (amount_minor > 0),
		CHECK (length(currency) = 3),
		CHECK (approved_by IS NULL OR approved_by <> requested_by)
	);
	CREATE INDEX IF NOT EXISTS idx_operational_settlement_commitments_device ON operational_settlement_commitments(device_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_operational_settlement_commitments_status ON operational_settlement_commitments(status, updated_at);
	CREATE TABLE IF NOT EXISTS operational_settlement_audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		commitment_id TEXT NOT NULL,
		action TEXT NOT NULL,
		actor TEXT NOT NULL,
		evidence_sha256 TEXT NOT NULL,
		details_redacted TEXT NOT NULL DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_operational_settlement_audit_commitment ON operational_settlement_audit(commitment_id, id);
	`
	execMulti(database, schema)
	for _, statement := range []string{
		`CREATE TRIGGER IF NOT EXISTS trg_operational_settlement_no_delete BEFORE DELETE ON operational_settlement_commitments BEGIN SELECT RAISE(ABORT, 'operational settlement commitments may not be deleted'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_operational_settlement_audit_immutable BEFORE UPDATE ON operational_settlement_audit BEGIN SELECT RAISE(ABORT, 'operational settlement audit entries are append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_operational_settlement_audit_no_delete BEFORE DELETE ON operational_settlement_audit BEGIN SELECT RAISE(ABORT, 'operational settlement audit entries are append-only'); END`,
	} {
		if _, err := database.Exec(statement); err != nil {
			log.Warn().Err(err).Msg("operational settlement SQLite trigger initialization failed")
		}
	}
}

func sha256LowerHex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func operationalSettlementEnabled() bool {
	return envBool("OPERATIONAL_SETTLEMENTS_ENABLED", false)
}

func operationalMojaloopEnabled() bool {
	return envBool("MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED", false)
}

func requiredPositiveIntEnv(name string) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0, fmt.Errorf("%s must be configured", name)
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func operationalSettlementConfiguration() (treasuryAccount string, ledger, accountCode, transferCode int, err error) {
	if !operationalSettlementEnabled() {
		return "", 0, 0, 0, settlementHTTPError(http.StatusServiceUnavailable, "operational settlement is disabled pending INEC financial authorization")
	}
	treasuryAccount = strings.TrimSpace(os.Getenv("OPERATIONAL_SETTLEMENT_TREASURY_ACCOUNT"))
	if treasuryAccount == "" {
		return "", 0, 0, 0, settlementHTTPError(http.StatusServiceUnavailable, "OPERATIONAL_SETTLEMENT_TREASURY_ACCOUNT is not configured")
	}
	if ledger, err = requiredPositiveIntEnv("OPERATIONAL_SETTLEMENT_TB_LEDGER"); err != nil {
		return "", 0, 0, 0, settlementHTTPError(http.StatusServiceUnavailable, "%v", err)
	}
	if accountCode, err = requiredPositiveIntEnv("OPERATIONAL_SETTLEMENT_TB_ACCOUNT_CODE"); err != nil {
		return "", 0, 0, 0, settlementHTTPError(http.StatusServiceUnavailable, "%v", err)
	}
	if transferCode, err = requiredPositiveIntEnv("OPERATIONAL_SETTLEMENT_TB_TRANSFER_CODE"); err != nil {
		return "", 0, 0, 0, settlementHTTPError(http.StatusServiceUnavailable, "%v", err)
	}
	return treasuryAccount, ledger, accountCode, transferCode, nil
}

func operationalSettlementTigerBeetleReady() error {
	if mwHub == nil || mwHub.TigerBeetle == nil {
		return settlementHTTPError(http.StatusServiceUnavailable, "TigerBeetle is unavailable")
	}
	status := mwHub.TigerBeetle.Status()
	if !status.Connected || !strings.Contains(strings.ToLower(status.Mode), "native tigerbeetle-go") {
		return settlementHTTPError(http.StatusServiceUnavailable, "native TigerBeetle is required for operational commitments")
	}
	return nil
}

func operationalSettlementMojaloopReady() error {
	if !operationalMojaloopEnabled() {
		return settlementHTTPError(http.StatusServiceUnavailable, "Mojaloop operational settlement is disabled pending authorized scheme onboarding")
	}
	if mwHub == nil || mwHub.Mojaloop == nil {
		return settlementHTTPError(http.StatusServiceUnavailable, "Mojaloop is unavailable")
	}
	status := mwHub.Mojaloop.Status()
	if !status.Connected || !strings.Contains(strings.ToLower(status.Mode), "external (fspiop)") {
		return settlementHTTPError(http.StatusServiceUnavailable, "authorized external Mojaloop FSPIOP connection is required")
	}
	if strings.TrimSpace(os.Getenv("MOJALOOP_OPERATIONAL_PAYER_FSP")) == "" {
		return settlementHTTPError(http.StatusServiceUnavailable, "MOJALOOP_OPERATIONAL_PAYER_FSP is not configured")
	}
	return nil
}

func requireOperationalSettlementAuthorization(r *http.Request, permission, resourceType, resource string) (string, error) {
	if _, err := requireRole(r, "admin", "finance_officer"); err != nil {
		return "", settlementHTTPError(http.StatusForbidden, "%v", err)
	}
	actor, err := currentGatewayActor(r)
	if err != nil {
		return "", settlementHTTPError(http.StatusUnauthorized, "%v", err)
	}
	if mwHub == nil || mwHub.Permify == nil || !mwHub.Permify.Status().Connected {
		return "", settlementHTTPError(http.StatusServiceUnavailable, "Permify authorization is required for operational settlement")
	}
	allowed, err := mwHub.Permify.Check(r.Context(), PermifyCheck{
		Subject: actor, SubjectType: "user", Permission: permission,
		Resource: resource, ResourceType: resourceType,
	})
	if err != nil {
		return "", settlementHTTPError(http.StatusServiceUnavailable, "check operational settlement authorization: %v", err)
	}
	if !allowed {
		return "", settlementHTTPError(http.StatusForbidden, "permission denied for operational settlement")
	}
	return actor, nil
}

func validateOperationalSettlementRequest(req *operationalSettlementRequest) error {
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.CommitmentKind = strings.TrimSpace(req.CommitmentKind)
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	req.RecipientReference = strings.TrimSpace(req.RecipientReference)
	req.PurposeReference = strings.TrimSpace(req.PurposeReference)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if req.ElectionID <= 0 || req.DeviceID == "" || req.AmountMinor <= 0 {
		return settlementHTTPError(http.StatusBadRequest, "election_id, device_id, and positive amount_minor are required")
	}
	if req.CommitmentKind != "device_logistics" && req.CommitmentKind != "device_reimbursement" && req.CommitmentKind != "device_repair" {
		return settlementHTTPError(http.StatusBadRequest, "commitment_kind is not allowed")
	}
	if len(req.Currency) != 3 || req.Currency[0] < 'A' || req.Currency[0] > 'Z' || req.Currency[1] < 'A' || req.Currency[1] > 'Z' || req.Currency[2] < 'A' || req.Currency[2] > 'Z' {
		return settlementHTTPError(http.StatusBadRequest, "currency must be a three-letter uppercase code")
	}
	if req.RecipientReference == "" || req.PurposeReference == "" {
		return settlementHTTPError(http.StatusBadRequest, "recipient_reference and purpose_reference are required and retained only as hashes")
	}
	if len(req.RecipientReference) > 1024 || len(req.PurposeReference) > 4096 {
		return settlementHTTPError(http.StatusBadRequest, "recipient_reference or purpose_reference exceeds allowed length")
	}
	if _, err := uuid.Parse(req.IdempotencyKey); err != nil {
		return settlementHTTPError(http.StatusBadRequest, "idempotency_key must be a UUID")
	}
	normalizedEvidence, err := normalizeLowerHex64(req.EvidenceSHA256)
	if err != nil {
		return settlementHTTPError(http.StatusBadRequest, "evidence_sha256: %v", err)
	}
	req.EvidenceSHA256 = normalizedEvidence
	limit := int64(envIntOr("OPERATIONAL_SETTLEMENT_MAX_AMOUNT_MINOR", 0))
	if limit > 0 && req.AmountMinor > limit {
		return settlementHTTPError(http.StatusBadRequest, "amount_minor exceeds the configured operational settlement cap")
	}
	return nil
}

func loadActiveSettlementDevice(ctx context.Context, electionID int, deviceID string) error {
	if db == nil {
		return settlementHTTPError(http.StatusServiceUnavailable, "PostgreSQL is unavailable")
	}
	var enrolledElection int
	var status string
	err := db.QueryRowContext(ctx, `SELECT election_id,status FROM bvas_device_enrollments WHERE device_id=?`, deviceID).Scan(&enrolledElection, &status)
	if err == sql.ErrNoRows {
		return settlementHTTPError(http.StatusNotFound, "device is not enrolled for operational settlement")
	}
	if err != nil {
		return settlementHTTPError(http.StatusServiceUnavailable, "load device enrollment: %v", err)
	}
	if enrolledElection != electionID || !strings.EqualFold(status, "active") {
		return settlementHTTPError(http.StatusConflict, "device enrollment is not active for the specified election")
	}
	return nil
}

func settlementDeviceAccount(deviceID string) (string, error) {
	prefix := strings.TrimSpace(os.Getenv("OPERATIONAL_SETTLEMENT_DEVICE_ACCOUNT_PREFIX"))
	if prefix == "" {
		return "", settlementHTTPError(http.StatusServiceUnavailable, "OPERATIONAL_SETTLEMENT_DEVICE_ACCOUNT_PREFIX is not configured")
	}
	return prefix + sha256LowerHex(deviceID), nil
}

func ensureTigerBeetleAccount(ctx context.Context, accountID string, ledger, accountCode int) error {
	account, err := mwHub.TigerBeetle.GetAccount(ctx, accountID)
	if err == nil {
		if account.Ledger != ledger || account.Code != accountCode {
			return settlementHTTPError(http.StatusConflict, "TigerBeetle account configuration does not match the operational settlement ledger")
		}
		return nil
	}
	if err := mwHub.TigerBeetle.CreateAccount(ctx, TBAccount{ID: accountID, Ledger: ledger, Code: accountCode}); err != nil {
		// A concurrent safe creation can report duplicate; always resolve with a lookup.
		account, lookupErr := mwHub.TigerBeetle.GetAccount(ctx, accountID)
		if lookupErr != nil || account.Ledger != ledger || account.Code != accountCode {
			return settlementHTTPError(http.StatusServiceUnavailable, "create TigerBeetle account: %v", err)
		}
	}
	return nil
}

func selectOperationalSettlement(ctx context.Context, query string, args ...interface{}) (*operationalSettlementRecord, error) {
	if db == nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "PostgreSQL is unavailable")
	}
	row := db.QueryRowContext(ctx, query, args...)
	var record operationalSettlementRecord
	err := row.Scan(
		&record.ID, &record.ElectionID, &record.DeviceID, &record.CommitmentKind, &record.AmountMinor, &record.Currency,
		&record.RecipientReferenceSHA256, &record.EvidenceSHA256, &record.PurposeReferenceSHA256, &record.IdempotencyKey,
		&record.RequestedBy, &record.ApprovedBy, &record.ApprovedAt, &record.Status, &record.TigerBeetleDebitAccount,
		&record.TigerBeetleCreditAccount, &record.TigerBeetleTransferID, &record.MojaloopQuoteID, &record.MojaloopTransferID,
		&record.ExternalReceiptSHA256, &record.FailureCode, &record.FailureDetailRedacted, &record.CreatedAt, &record.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, settlementHTTPError(http.StatusNotFound, "operational settlement commitment not found")
	}
	if err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "load operational settlement commitment: %v", err)
	}
	return &record, nil
}

const operationalSettlementSelectColumns = `id,election_id,device_id,commitment_kind,amount_minor,currency,recipient_reference_sha256,evidence_sha256,purpose_reference_sha256,idempotency_key,requested_by,COALESCE(approved_by,''),COALESCE(CAST(approved_at AS TEXT),''),status,tigerbeetle_debit_account_id,tigerbeetle_credit_account_id,COALESCE(tigerbeetle_transfer_id,''),COALESCE(mojaloop_quote_id,''),COALESCE(mojaloop_transfer_id,''),COALESCE(external_receipt_sha256,''),COALESCE(failure_code,''),COALESCE(failure_detail_redacted,''),CAST(created_at AS TEXT),CAST(updated_at AS TEXT)`

func getOperationalSettlement(ctx context.Context, id string) (*operationalSettlementRecord, error) {
	return selectOperationalSettlement(ctx, `SELECT `+operationalSettlementSelectColumns+` FROM operational_settlement_commitments WHERE id=?`, id)
}

func getOperationalSettlementByIdempotency(ctx context.Context, idempotencyKey string) (*operationalSettlementRecord, error) {
	return selectOperationalSettlement(ctx, `SELECT `+operationalSettlementSelectColumns+` FROM operational_settlement_commitments WHERE idempotency_key=?`, idempotencyKey)
}

func appendOperationalSettlementAudit(ctx context.Context, tx *sql.Tx, commitmentID string, event operationalSettlementAudit) error {
	encoded, err := json.Marshal(event.Details)
	if err != nil {
		return fmt.Errorf("marshal operational settlement audit details: %w", err)
	}
	query := `INSERT INTO operational_settlement_audit (commitment_id,action,actor,evidence_sha256,details_redacted) VALUES (?,?,?,?,?)`
	if tx != nil {
		_, err = tx.ExecContext(ctx, query, commitmentID, event.Action, event.Actor, event.EvidenceSHA256, string(encoded))
	} else {
		_, err = db.ExecContext(ctx, query, commitmentID, event.Action, event.Actor, event.EvidenceSHA256, string(encoded))
	}
	return err
}

func enqueueOperationalSettlementEvent(ctx context.Context, record *operationalSettlementRecord, action string) error {
	payload := map[string]interface{}{
		"event_version":              "v1",
		"commitment_id":              record.ID,
		"election_id":                record.ElectionID,
		"device_id":                  record.DeviceID,
		"commitment_kind":            record.CommitmentKind,
		"status":                     record.Status,
		"evidence_sha256":            record.EvidenceSHA256,
		"recipient_reference_sha256": record.RecipientReferenceSHA256,
		"purpose_reference_sha256":   record.PurposeReferenceSHA256,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	payloadSHA := sha256LowerHex(string(encoded))
	eventType := "operational_settlement." + action + ".v1"
	_, err = db.ExecContext(ctx, `INSERT INTO external_integration_outbox
		(correlation_id,source_type,aggregate_type,aggregate_id,event_type,event_version,partition_key,payload_redacted,payload_sha256,required_sinks,delivery_status,next_attempt_at)
		VALUES (?,?,?,?,?,'v1',?,?,?,'["kafka","dapr","fluvio","opensearch"]','pending',CURRENT_TIMESTAMP)
		ON CONFLICT(event_type,aggregate_type,aggregate_id,payload_sha256) DO NOTHING`,
		uuid.NewString(), operationalSettlementSourceType, operationalSettlementAggregateType, record.ID, eventType,
		record.DeviceID, string(encoded), payloadSHA)
	return err
}

func createOperationalSettlement(ctx context.Context, actor string, req operationalSettlementRequest) (*operationalSettlementRecord, bool, error) {
	if err := validateOperationalSettlementRequest(&req); err != nil {
		return nil, false, err
	}
	if _, _, _, _, err := operationalSettlementConfiguration(); err != nil {
		return nil, false, err
	}
	if err := operationalSettlementTigerBeetleReady(); err != nil {
		return nil, false, err
	}
	if err := loadActiveSettlementDevice(ctx, req.ElectionID, req.DeviceID); err != nil {
		return nil, false, err
	}
	debit, _, _, _, _ := operationalSettlementConfiguration()
	credit, err := settlementDeviceAccount(req.DeviceID)
	if err != nil {
		return nil, false, err
	}
	record := &operationalSettlementRecord{
		ID:                       uuid.NewString(),
		ElectionID:               req.ElectionID,
		DeviceID:                 req.DeviceID,
		CommitmentKind:           req.CommitmentKind,
		AmountMinor:              req.AmountMinor,
		Currency:                 req.Currency,
		RecipientReferenceSHA256: sha256LowerHex(req.RecipientReference),
		EvidenceSHA256:           req.EvidenceSHA256,
		PurposeReferenceSHA256:   sha256LowerHex(req.PurposeReference),
		IdempotencyKey:           req.IdempotencyKey,
		RequestedBy:              actor,
		Status:                   "requested",
		TigerBeetleDebitAccount:  debit,
		TigerBeetleCreditAccount: credit,
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, settlementHTTPError(http.StatusServiceUnavailable, "begin operational settlement request: %v", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO operational_settlement_commitments
		(id,election_id,device_id,commitment_kind,amount_minor,currency,recipient_reference_sha256,evidence_sha256,purpose_reference_sha256,idempotency_key,requested_by,status,tigerbeetle_debit_account_id,tigerbeetle_credit_account_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(idempotency_key) DO NOTHING`,
		record.ID, record.ElectionID, record.DeviceID, record.CommitmentKind, record.AmountMinor, record.Currency,
		record.RecipientReferenceSHA256, record.EvidenceSHA256, record.PurposeReferenceSHA256, record.IdempotencyKey,
		record.RequestedBy, record.Status, record.TigerBeetleDebitAccount, record.TigerBeetleCreditAccount)
	if err != nil {
		return nil, false, settlementHTTPError(http.StatusServiceUnavailable, "store operational settlement request: %v", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		existing, lookupErr := getOperationalSettlementByIdempotency(ctx, record.IdempotencyKey)
		if lookupErr != nil {
			return nil, false, lookupErr
		}
		if existing.ElectionID != record.ElectionID || existing.DeviceID != record.DeviceID || existing.AmountMinor != record.AmountMinor || existing.EvidenceSHA256 != record.EvidenceSHA256 || existing.RecipientReferenceSHA256 != record.RecipientReferenceSHA256 || existing.PurposeReferenceSHA256 != record.PurposeReferenceSHA256 {
			return nil, false, settlementHTTPError(http.StatusConflict, "idempotency_key is already bound to a different operational commitment")
		}
		return existing, false, tx.Commit()
	}
	if err := appendOperationalSettlementAudit(ctx, tx, record.ID, operationalSettlementAudit{
		Action: "requested", Actor: actor, EvidenceSHA256: record.EvidenceSHA256,
		Details: map[string]interface{}{"commitment_kind": record.CommitmentKind, "device_id": record.DeviceID},
	}); err != nil {
		return nil, false, settlementHTTPError(http.StatusServiceUnavailable, "write operational settlement audit: %v", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, false, settlementHTTPError(http.StatusServiceUnavailable, "commit operational settlement request: %v", err)
	}
	record, err = getOperationalSettlement(ctx, record.ID)
	if err != nil {
		return nil, false, err
	}
	if err := enqueueOperationalSettlementEvent(ctx, record, "requested"); err != nil {
		return nil, false, settlementHTTPError(http.StatusServiceUnavailable, "enqueue operational settlement event: %v", err)
	}
	return record, true, nil
}

func approveOperationalSettlement(ctx context.Context, actor, id string) (*operationalSettlementRecord, error) {
	record, err := getOperationalSettlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.RequestedBy == actor {
		return nil, settlementHTTPError(http.StatusForbidden, "requester may not approve the same operational settlement")
	}
	if record.Status != "requested" {
		if record.Status == "approved" || record.Status == "ledger_pending" || record.Status == "mojaloop_unavailable" || record.Status == "mojaloop_pending" || record.Status == "settled" {
			return record, nil
		}
		return nil, settlementHTTPError(http.StatusConflict, "operational settlement cannot be approved from status %s", record.Status)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "begin operational settlement approval: %v", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE operational_settlement_commitments SET approved_by=?,approved_at=CURRENT_TIMESTAMP,status='approved',updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='requested' AND approved_by IS NULL`, actor, id)
	if err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "approve operational settlement: %v", err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return nil, settlementHTTPError(http.StatusConflict, "operational settlement approval changed concurrently")
	}
	if err := appendOperationalSettlementAudit(ctx, tx, id, operationalSettlementAudit{
		Action: "approved", Actor: actor, EvidenceSHA256: record.EvidenceSHA256,
		Details: map[string]interface{}{"independent_approval": true},
	}); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "write approval audit: %v", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "commit operational settlement approval: %v", err)
	}
	record, err = getOperationalSettlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := enqueueOperationalSettlementEvent(ctx, record, "approved"); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "enqueue approval event: %v", err)
	}
	return record, nil
}

func createPendingOperationalCommitment(ctx context.Context, actor, id string) (*operationalSettlementRecord, error) {
	record, err := getOperationalSettlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.Status == "ledger_pending" || record.Status == "mojaloop_unavailable" || record.Status == "mojaloop_pending" || record.Status == "settled" {
		return record, nil
	}
	if record.Status != "approved" && record.Status != "failed" {
		return nil, settlementHTTPError(http.StatusConflict, "operational settlement cannot create a ledger commitment from status %s", record.Status)
	}
	if record.ApprovedBy == "" || record.ApprovedBy == record.RequestedBy {
		return nil, settlementHTTPError(http.StatusConflict, "operational settlement lacks independent approval")
	}
	if err := operationalSettlementTigerBeetleReady(); err != nil {
		return nil, err
	}
	treasury, ledger, accountCode, transferCode, err := operationalSettlementConfiguration()
	if err != nil {
		return nil, err
	}
	if treasury != record.TigerBeetleDebitAccount {
		return nil, settlementHTTPError(http.StatusConflict, "configured treasury account does not match commitment binding")
	}
	if err := ensureTigerBeetleAccount(ctx, record.TigerBeetleDebitAccount, ledger, accountCode); err != nil {
		return nil, err
	}
	if err := ensureTigerBeetleAccount(ctx, record.TigerBeetleCreditAccount, ledger, accountCode); err != nil {
		return nil, err
	}
	transferID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("inec-operational-settlement-tigerbeetle:"+record.ID)).String()
	if _, err := mwHub.TigerBeetle.CreateTransfer(ctx, TBTransfer{
		ID: transferID, DebitAccountID: record.TigerBeetleDebitAccount, CreditAccountID: record.TigerBeetleCreditAccount,
		Amount: record.AmountMinor, Ledger: ledger, Code: transferCode, Status: "PENDING", IdempotencyKey: record.ID,
	}); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "create pending TigerBeetle operational commitment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE operational_settlement_commitments SET status='ledger_pending',tigerbeetle_transfer_id=?,failure_code=NULL,failure_detail_redacted=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status IN ('approved','failed')`, transferID, record.ID); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "record TigerBeetle operational commitment: %v", err)
	}
	if err := appendOperationalSettlementAudit(ctx, nil, record.ID, operationalSettlementAudit{
		Action: "ledger_pending", Actor: actor, EvidenceSHA256: record.EvidenceSHA256,
		Details: map[string]interface{}{"tigerbeetle_transfer_id": transferID},
	}); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "write ledger commitment audit: %v", err)
	}
	record, err = getOperationalSettlement(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	if err := enqueueOperationalSettlementEvent(ctx, record, "ledger_pending"); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "enqueue ledger commitment event: %v", err)
	}
	return record, nil
}

func markOperationalSettlementUnavailable(ctx context.Context, record *operationalSettlementRecord, actor, code string) error {
	_, err := db.ExecContext(ctx, `UPDATE operational_settlement_commitments SET status='mojaloop_unavailable',failure_code=?,failure_detail_redacted=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status IN ('ledger_pending','mojaloop_unavailable')`, code, "external settlement was not dispatched", record.ID)
	if err != nil {
		return err
	}
	if err := appendOperationalSettlementAudit(ctx, nil, record.ID, operationalSettlementAudit{
		Action: "mojaloop_unavailable", Actor: actor, EvidenceSHA256: record.EvidenceSHA256,
		Details: map[string]interface{}{"reason_code": code},
	}); err != nil {
		return err
	}
	updated, err := getOperationalSettlement(ctx, record.ID)
	if err == nil {
		_ = enqueueOperationalSettlementEvent(ctx, updated, "mojaloop_unavailable")
	}
	return nil
}

func dispatchOperationalSettlement(ctx context.Context, actor, id, recipientReference string) (*operationalSettlementRecord, error) {
	record, err := getOperationalSettlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.Status == "mojaloop_pending" || record.Status == "settled" {
		return record, nil
	}
	if record.Status != "ledger_pending" && record.Status != "mojaloop_unavailable" {
		return nil, settlementHTTPError(http.StatusConflict, "operational settlement cannot dispatch from status %s", record.Status)
	}
	recipientReference = strings.TrimSpace(recipientReference)
	if recipientReference == "" || sha256LowerHex(recipientReference) != record.RecipientReferenceSHA256 {
		return nil, settlementHTTPError(http.StatusBadRequest, "recipient_reference does not match the approved operational commitment")
	}
	if err := operationalSettlementMojaloopReady(); err != nil {
		_ = markOperationalSettlementUnavailable(ctx, record, actor, "mojaloop_unavailable")
		return nil, err
	}
	payerFSP := strings.TrimSpace(os.Getenv("MOJALOOP_OPERATIONAL_PAYER_FSP"))
	party, err := mwHub.Mojaloop.PartyLookup(ctx, "MSISDN", recipientReference)
	if err != nil || party == nil || strings.TrimSpace(party.FSPID) == "" {
		_ = markOperationalSettlementUnavailable(ctx, record, actor, "party_lookup_unavailable")
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "Mojaloop party lookup failed")
	}
	quoteID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("inec-operational-settlement-quote:"+record.ID)).String()
	quote, err := mwHub.Mojaloop.CreateQuote(ctx, MojaQuoteRequest{
		QuoteID: quoteID, PayerFSP: payerFSP, PayeeFSP: party.FSPID,
		Amount: float64(record.AmountMinor) / 100, Currency: record.Currency,
	})
	if err != nil || quote == nil || strings.TrimSpace(quote.QuoteID) == "" || strings.TrimSpace(quote.ILPPacket) == "" || strings.TrimSpace(quote.Condition) == "" {
		_ = markOperationalSettlementUnavailable(ctx, record, actor, "quote_unavailable")
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "Mojaloop quote was not verified")
	}
	transferID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("inec-operational-settlement-mojaloop:"+record.ID)).String()
	transfer, err := mwHub.Mojaloop.CreateTransfer(ctx, MojaTransferRequest{
		TransferID: transferID, QuoteID: quote.QuoteID, PayerFSP: payerFSP, PayeeFSP: party.FSPID,
		Amount: float64(record.AmountMinor) / 100, Currency: record.Currency, ILPPacket: quote.ILPPacket, Condition: quote.Condition,
	})
	if err != nil || transfer == nil || strings.TrimSpace(transfer.TransferID) == "" {
		_ = markOperationalSettlementUnavailable(ctx, record, actor, "transfer_unavailable")
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "Mojaloop transfer was not accepted")
	}
	if _, err := db.ExecContext(ctx, `UPDATE operational_settlement_commitments SET status='mojaloop_pending',mojaloop_quote_id=?,mojaloop_transfer_id=?,failure_code=NULL,failure_detail_redacted=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status IN ('ledger_pending','mojaloop_unavailable')`, quote.QuoteID, transfer.TransferID, record.ID); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "record Mojaloop transfer: %v", err)
	}
	if err := appendOperationalSettlementAudit(ctx, nil, record.ID, operationalSettlementAudit{
		Action: "mojaloop_pending", Actor: actor, EvidenceSHA256: record.EvidenceSHA256,
		Details: map[string]interface{}{"mojaloop_transfer_id": transfer.TransferID, "mojaloop_state": transfer.State},
	}); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "write Mojaloop dispatch audit: %v", err)
	}
	record, err = getOperationalSettlement(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	if err := enqueueOperationalSettlementEvent(ctx, record, "mojaloop_pending"); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "enqueue Mojaloop dispatch event: %v", err)
	}
	return record, nil
}

func reconcileOperationalSettlement(ctx context.Context, actor, id string) (*operationalSettlementRecord, error) {
	record, err := getOperationalSettlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.Status == "settled" {
		return record, nil
	}
	if record.Status != "mojaloop_pending" || record.TigerBeetleTransferID == "" || record.MojaloopTransferID == "" {
		return nil, settlementHTTPError(http.StatusConflict, "operational settlement has no pending authorized Mojaloop transfer")
	}
	if err := operationalSettlementMojaloopReady(); err != nil {
		return nil, err
	}
	tx, err := mwHub.Mojaloop.GetTransaction(ctx, record.MojaloopTransferID)
	if err != nil || tx == nil || !strings.EqualFold(strings.TrimSpace(tx.Phase), "settlement") {
		return nil, settlementHTTPError(http.StatusConflict, "Mojaloop settlement receipt is not yet verified")
	}
	if err := operationalSettlementTigerBeetleReady(); err != nil {
		return nil, err
	}
	if err := mwHub.TigerBeetle.PostTransfer(ctx, record.TigerBeetleTransferID); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "post approved TigerBeetle commitment: %v", err)
	}
	receipt, err := json.Marshal(tx)
	if err != nil {
		return nil, settlementHTTPError(http.StatusInternalServerError, "marshal Mojaloop receipt: %v", err)
	}
	receiptSHA := sha256LowerHex(string(receipt))
	if _, err := db.ExecContext(ctx, `UPDATE operational_settlement_commitments SET status='settled',external_receipt_sha256=?,failure_code=NULL,failure_detail_redacted=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='mojaloop_pending'`, receiptSHA, record.ID); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "record settled operational commitment: %v", err)
	}
	if err := appendOperationalSettlementAudit(ctx, nil, record.ID, operationalSettlementAudit{
		Action: "settled", Actor: actor, EvidenceSHA256: record.EvidenceSHA256,
		Details: map[string]interface{}{"external_receipt_sha256": receiptSHA},
	}); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "write settlement audit: %v", err)
	}
	record, err = getOperationalSettlement(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	if err := enqueueOperationalSettlementEvent(ctx, record, "settled"); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "enqueue settled event: %v", err)
	}
	return record, nil
}

func voidOperationalSettlement(ctx context.Context, actor, id string) (*operationalSettlementRecord, error) {
	record, err := getOperationalSettlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.Status == "voided" {
		return record, nil
	}
	if record.Status == "settled" || record.Status == "mojaloop_pending" {
		return nil, settlementHTTPError(http.StatusConflict, "an external or settled operational commitment cannot be voided through this control")
	}
	if record.TigerBeetleTransferID != "" {
		if err := operationalSettlementTigerBeetleReady(); err != nil {
			return nil, err
		}
		if err := mwHub.TigerBeetle.VoidTransfer(ctx, record.TigerBeetleTransferID); err != nil {
			return nil, settlementHTTPError(http.StatusServiceUnavailable, "void TigerBeetle operational commitment: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE operational_settlement_commitments SET status='voided',updated_at=CURRENT_TIMESTAMP WHERE id=? AND status NOT IN ('settled','mojaloop_pending','voided')`, record.ID); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "record voided operational commitment: %v", err)
	}
	if err := appendOperationalSettlementAudit(ctx, nil, record.ID, operationalSettlementAudit{
		Action: "voided", Actor: actor, EvidenceSHA256: record.EvidenceSHA256,
		Details: map[string]interface{}{},
	}); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "write void audit: %v", err)
	}
	record, err = getOperationalSettlement(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	if err := enqueueOperationalSettlementEvent(ctx, record, "voided"); err != nil {
		return nil, settlementHTTPError(http.StatusServiceUnavailable, "enqueue void event: %v", err)
	}
	return record, nil
}

func writeOperationalSettlementError(w http.ResponseWriter, err error) {
	writeError(w, operationalSettlementStatus(err), err.Error())
}

func handleOperationalSettlementHealth(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "finance_officer", "ict_officer", "security"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	components := M{
		"operational_settlement_enabled": operationalSettlementEnabled(),
		"mojaloop_enabled":               operationalMojaloopEnabled(),
		"tigerbeetle":                    false,
		"mojaloop":                       false,
		"permify":                        false,
	}
	if mwHub != nil {
		if mwHub.TigerBeetle != nil {
			components["tigerbeetle"] = mwHub.TigerBeetle.Status().Connected && strings.Contains(strings.ToLower(mwHub.TigerBeetle.Status().Mode), "native tigerbeetle-go")
		}
		if mwHub.Mojaloop != nil {
			components["mojaloop"] = mwHub.Mojaloop.Status().Connected && strings.Contains(strings.ToLower(mwHub.Mojaloop.Status().Mode), "external (fspiop)")
		}
		if mwHub.Permify != nil {
			components["permify"] = mwHub.Permify.Status().Connected
		}
	}
	ready := components["operational_settlement_enabled"].(bool) && components["tigerbeetle"].(bool) && components["permify"].(bool)
	status := "unavailable"
	code := http.StatusServiceUnavailable
	if ready {
		status = "ready"
		code = http.StatusOK
	}
	writeJSON(w, code, M{"status": status, "components": components, "scope": "device logistics and reimbursement commitments only"})
}

func handleCreateOperationalSettlement(w http.ResponseWriter, r *http.Request) {
	var req operationalSettlementRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid operational settlement request")
		return
	}
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		writeError(w, http.StatusBadRequest, "read operational settlement request")
		return
	}
	actor, err := requireOperationalSettlementAuthorization(r, "request_operational_settlement", "bvas_device", strings.TrimSpace(req.DeviceID))
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	record, created, err := createOperationalSettlement(r.Context(), actor, req)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeJSON(w, code, record)
}

func commitmentIDFromRequest(r *http.Request) (string, error) {
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if _, err := uuid.Parse(id); err != nil {
		return "", settlementHTTPError(http.StatusBadRequest, "commitment id must be a UUID")
	}
	return id, nil
}

func handleGetOperationalSettlement(w http.ResponseWriter, r *http.Request) {
	id, err := commitmentIDFromRequest(r)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	if _, err := requireOperationalSettlementAuthorization(r, "view_operational_settlement", "operational_settlement", id); err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	record, err := getOperationalSettlement(r.Context(), id)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func handleApproveOperationalSettlement(w http.ResponseWriter, r *http.Request) {
	id, err := commitmentIDFromRequest(r)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	actor, err := requireOperationalSettlementAuthorization(r, "approve_operational_settlement", "operational_settlement", id)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	record, err := approveOperationalSettlement(r.Context(), actor, id)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func handleCommitOperationalSettlementLedger(w http.ResponseWriter, r *http.Request) {
	id, err := commitmentIDFromRequest(r)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	actor, err := requireOperationalSettlementAuthorization(r, "commit_operational_settlement", "operational_settlement", id)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	record, err := createPendingOperationalCommitment(r.Context(), actor, id)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func handleDispatchOperationalSettlement(w http.ResponseWriter, r *http.Request) {
	id, err := commitmentIDFromRequest(r)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	var req operationalSettlementDispatchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid operational settlement dispatch request")
		return
	}
	actor, err := requireOperationalSettlementAuthorization(r, "dispatch_operational_settlement", "operational_settlement", id)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	record, err := dispatchOperationalSettlement(r.Context(), actor, id, req.RecipientReference)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, record)
}

func handleReconcileOperationalSettlement(w http.ResponseWriter, r *http.Request) {
	id, err := commitmentIDFromRequest(r)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	actor, err := requireOperationalSettlementAuthorization(r, "reconcile_operational_settlement", "operational_settlement", id)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	record, err := reconcileOperationalSettlement(r.Context(), actor, id)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func handleVoidOperationalSettlement(w http.ResponseWriter, r *http.Request) {
	id, err := commitmentIDFromRequest(r)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	actor, err := requireOperationalSettlementAuthorization(r, "void_operational_settlement", "operational_settlement", id)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	record, err := voidOperationalSettlement(r.Context(), actor, id)
	if err != nil {
		writeOperationalSettlementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func registerOperationalSettlementRoutes(r *mux.Router) {
	r.HandleFunc("/operational-settlements/health", readAuth(handleOperationalSettlementHealth)).Methods(http.MethodGet)
	r.HandleFunc("/operational-settlements/commitments", authRequired(handleCreateOperationalSettlement)).Methods(http.MethodPost)
	r.HandleFunc("/operational-settlements/commitments/{id}", readAuth(handleGetOperationalSettlement)).Methods(http.MethodGet)
	r.HandleFunc("/operational-settlements/commitments/{id}/approve", authRequired(handleApproveOperationalSettlement)).Methods(http.MethodPost)
	r.HandleFunc("/operational-settlements/commitments/{id}/ledger", authRequired(handleCommitOperationalSettlementLedger)).Methods(http.MethodPost)
	r.HandleFunc("/operational-settlements/commitments/{id}/dispatch", authRequired(handleDispatchOperationalSettlement)).Methods(http.MethodPost)
	r.HandleFunc("/operational-settlements/commitments/{id}/reconcile", authRequired(handleReconcileOperationalSettlement)).Methods(http.MethodPost)
	r.HandleFunc("/operational-settlements/commitments/{id}/void", authRequired(handleVoidOperationalSettlement)).Methods(http.MethodPost)
}
