package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

var errIReVUnavailable = errors.New("authorized IReV integration is unavailable")

type irevConfig struct {
	BaseURL           string
	SubmitPath        string
	TokenURL          string
	ClientID          string
	ClientSecret      string
	Scope             string
	Audience          string
	ClientCertFile    string
	ClientKeyFile     string
	CACertFile        string
	WebhookHMACSecret string
	Required          bool
}

type irevExternalReceipt struct {
	ReceiptID     string `json:"receipt_id"`
	TransactionID string `json:"transaction_id"`
	Status        string `json:"status"`
	CorrelationID string `json:"correlation_id"`
}

func currentIReVConfig() irevConfig {
	return irevConfig{
		BaseURL:           os.Getenv("IREV_API_BASE_URL"),
		SubmitPath:        os.Getenv("IREV_SUBMIT_PATH"),
		TokenURL:          os.Getenv("IREV_OAUTH_TOKEN_URL"),
		ClientID:          os.Getenv("IREV_OAUTH_CLIENT_ID"),
		ClientSecret:      os.Getenv("IREV_OAUTH_CLIENT_SECRET"),
		Scope:             os.Getenv("IREV_OAUTH_SCOPE"),
		Audience:          os.Getenv("IREV_OAUTH_AUDIENCE"),
		ClientCertFile:    os.Getenv("IREV_MTLS_CLIENT_CERT_FILE"),
		ClientKeyFile:     os.Getenv("IREV_MTLS_CLIENT_KEY_FILE"),
		CACertFile:        os.Getenv("IREV_CA_CERT_FILE"),
		WebhookHMACSecret: os.Getenv("IREV_WEBHOOK_HMAC_SECRET"),
		Required:          strings.EqualFold(strings.TrimSpace(os.Getenv("IREV_REQUIRED")), "true"),
	}
}

func (config irevConfig) configured() error {
	fields := []string{config.BaseURL, config.SubmitPath, config.TokenURL, config.ClientID, config.ClientSecret, config.ClientCertFile, config.ClientKeyFile, config.CACertFile}
	for _, field := range fields {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("%w: missing sanctioned IReV configuration", errIReVUnavailable)
		}
	}
	base, err := url.Parse(config.BaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return fmt.Errorf("%w: IReV base URL must be HTTPS", errIReVUnavailable)
	}
	tokenURL, err := url.Parse(config.TokenURL)
	if err != nil || tokenURL.Scheme != "https" || tokenURL.Host == "" {
		return fmt.Errorf("%w: IReV token URL must be HTTPS", errIReVUnavailable)
	}
	if !strings.HasPrefix(config.SubmitPath, "/") || strings.Contains(config.SubmitPath, "://") {
		return fmt.Errorf("%w: IReV submit path must be a relative HTTPS path", errIReVUnavailable)
	}
	return nil
}

func (config irevConfig) httpClient() (*http.Client, error) {
	if err := config.configured(); err != nil {
		return nil, err
	}
	certificate, err := tls.LoadX509KeyPair(config.ClientCertFile, config.ClientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("%w: load IReV client certificate", errIReVUnavailable)
	}
	caPEM, err := os.ReadFile(config.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("%w: read IReV CA certificate", errIReVUnavailable)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("%w: invalid IReV CA certificate", errIReVUnavailable)
	}
	return &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		RootCAs:      roots,
	}}}, nil
}

func (config irevConfig) accessToken(ctx context.Context, client *http.Client) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	if config.Scope != "" {
		form.Set("scope", config.Scope)
	}
	if config.Audience != "" {
		form.Set("audience", config.Audience)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(config.ClientID, config.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: request IReV token", errIReVUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: IReV token endpoint returned %d", errIReVUnavailable, response.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token); err != nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", fmt.Errorf("%w: IReV token response is invalid", errIReVUnavailable)
	}
	return token.AccessToken, nil
}

func initIReVSchema(database *sql.DB) {
	schema := `
	CREATE TABLE IF NOT EXISTS authorized_portal_connections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		portal_code TEXT NOT NULL UNIQUE,
		base_url TEXT NOT NULL,
		submit_path TEXT NOT NULL,
		token_url TEXT,
		client_id_reference TEXT,
		client_secret_reference TEXT,
		client_cert_reference TEXT,
		client_key_reference TEXT,
		ca_cert_reference TEXT,
		audience TEXT,
		scope TEXT,
		status TEXT NOT NULL DEFAULT 'unconfigured',
		approved_by INTEGER,
		approved_at TIMESTAMP,
		last_health_at TIMESTAMP,
		last_health_status TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS irev_submission_receipts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		result_id INTEGER NOT NULL,
		portal_connection_id INTEGER,
		idempotency_key TEXT NOT NULL UNIQUE,
		evidence_event_hash TEXT NOT NULL,
		payload_sha256 TEXT NOT NULL,
		submission_status TEXT NOT NULL,
		external_receipt_id TEXT,
		external_transaction_id TEXT,
		external_status TEXT,
		external_response_redacted TEXT NOT NULL DEFAULT '{}',
		submitted_at TIMESTAMP,
		acknowledged_at TIMESTAMP,
		last_error_code TEXT,
		last_error_detail TEXT,
		created_by INTEGER,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(result_id,evidence_event_hash)
	);
	CREATE TABLE IF NOT EXISTS irev_webhook_receipts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		portal_connection_id INTEGER,
		external_receipt_id TEXT NOT NULL,
		delivery_id TEXT,
		payload_sha256 TEXT NOT NULL,
		signature_valid INTEGER NOT NULL,
		payload_redacted TEXT NOT NULL DEFAULT '{}',
		received_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(portal_connection_id,external_receipt_id,payload_sha256)
	);`
	execMulti(database, schema)
}

func upsertIReVPortalConfiguration(config irevConfig) (int64, error) {
	status := "unconfigured"
	if config.configured() == nil {
		status = "configured"
	}
	_, err := db.Exec(`INSERT INTO authorized_portal_connections
		(portal_code,base_url,submit_path,token_url,client_id_reference,client_secret_reference,client_cert_reference,client_key_reference,ca_cert_reference,audience,scope,status,updated_at)
		VALUES ('irev',?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(portal_code) DO UPDATE SET base_url=excluded.base_url,submit_path=excluded.submit_path,token_url=excluded.token_url,client_id_reference=excluded.client_id_reference,client_secret_reference=excluded.client_secret_reference,client_cert_reference=excluded.client_cert_reference,client_key_reference=excluded.client_key_reference,ca_cert_reference=excluded.ca_cert_reference,audience=excluded.audience,scope=excluded.scope,status=excluded.status,updated_at=CURRENT_TIMESTAMP`,
		config.BaseURL, config.SubmitPath, config.TokenURL, envReference("IREV_OAUTH_CLIENT_ID"), envReference("IREV_OAUTH_CLIENT_SECRET"), envReference("IREV_MTLS_CLIENT_CERT_FILE"), envReference("IREV_MTLS_CLIENT_KEY_FILE"), envReference("IREV_CA_CERT_FILE"), config.Audience, config.Scope, status)
	if err != nil {
		return 0, err
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM authorized_portal_connections WHERE portal_code='irev'").Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func envReference(name string) string {
	if strings.TrimSpace(os.Getenv(name)) == "" {
		return ""
	}
	return "env:" + name
}

func authoritativeIReVPayload(resultID int) (map[string]interface{}, string, error) {
	var electionID, totalValid, rejected, totalCast, accredited int
	var pollingUnit, status string
	if err := db.QueryRow(`SELECT election_id,polling_unit_code,status,total_valid_votes,rejected_votes,total_votes_cast,accredited_voters FROM results WHERE id=?`, resultID).Scan(&electionID, &pollingUnit, &status, &totalValid, &rejected, &totalCast, &accredited); err != nil {
		return nil, "", err
	}
	if status != "finalized" {
		return nil, "", fmt.Errorf("result must be finalized before authorized IReV submission")
	}
	var eventHash string
	if err := db.QueryRow(`SELECT event_hash FROM result_evidence_events WHERE result_id=? AND event_type='RESULT_FINALIZED' AND signer_status='signed' ORDER BY sequence_no DESC LIMIT 1`, resultID).Scan(&eventHash); err != nil {
		return nil, "", fmt.Errorf("signed final evidence event is required: %w", err)
	}
	var blocking int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reconciliation_cases WHERE result_id=? AND blocking=1 AND status IN ('open','under_review')`, resultID).Scan(&blocking); err != nil {
		return nil, "", err
	}
	if blocking > 0 {
		return nil, "", fmt.Errorf("result has unresolved blocking reconciliation cases")
	}
	rows, err := db.Query(`SELECT party_code,votes FROM result_party_scores WHERE result_id=? ORDER BY party_code`, resultID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	partyVotes := make([]map[string]interface{}, 0)
	for rows.Next() {
		var party string
		var votes int
		if err := rows.Scan(&party, &votes); err != nil {
			return nil, "", err
		}
		partyVotes = append(partyVotes, map[string]interface{}{"party_code": party, "votes": votes})
	}
	payload := map[string]interface{}{
		"version":             "inec-irev-submission-v1",
		"result_id":           resultID,
		"election_id":         electionID,
		"polling_unit_code":   pollingUnit,
		"total_valid_votes":   totalValid,
		"rejected_votes":      rejected,
		"total_votes_cast":    totalCast,
		"accredited_voters":   accredited,
		"party_votes":         partyVotes,
		"evidence_event_hash": eventHash,
	}
	return payload, eventHash, nil
}

func redactIReVResponse(receipt irevExternalReceipt) map[string]interface{} {
	return map[string]interface{}{
		"receipt_id":     receipt.ReceiptID,
		"transaction_id": receipt.TransactionID,
		"status":         receipt.Status,
		"correlation_id": receipt.CorrelationID,
	}
}

func submitAuthorizedIReVResult(ctx context.Context, resultID int, createdBy interface{}) (map[string]interface{}, int, error) {
	config := currentIReVConfig()
	portalID, portalErr := upsertIReVPortalConfiguration(config)
	if portalErr != nil {
		return nil, http.StatusServiceUnavailable, fmt.Errorf("record IReV portal configuration: %w", portalErr)
	}
	payload, eventHash, err := authoritativeIReVPayload(resultID)
	if err != nil {
		return nil, http.StatusConflict, err
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	payloadHash := fmt.Sprintf("%x", sha256.Sum256(canonical))
	idempotencyKey := uuid.NewString()
	result, err := db.Exec(`INSERT INTO irev_submission_receipts (result_id,portal_connection_id,idempotency_key,evidence_event_hash,payload_sha256,submission_status,created_by)
		VALUES (?,?,?,?,?,'pending',?) ON CONFLICT(result_id,evidence_event_hash) DO NOTHING`, resultID, portalID, idempotencyKey, eventHash, payloadHash, createdBy)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		row, rowErr := querySingleRow(`SELECT * FROM irev_submission_receipts WHERE result_id=? AND evidence_event_hash=?`, resultID, eventHash)
		if rowErr != nil {
			return nil, http.StatusConflict, rowErr
		}
		return row, http.StatusConflict, fmt.Errorf("an IReV submission already exists for this evidence event")
	}
	client, err := config.httpClient()
	if err != nil {
		db.Exec(`UPDATE irev_submission_receipts SET submission_status='unavailable',last_error_code='configuration_unavailable',last_error_detail=?,updated_at=CURRENT_TIMESTAMP WHERE idempotency_key=?`, err.Error(), idempotencyKey)
		return map[string]interface{}{"idempotency_key": idempotencyKey, "status": "unavailable", "reason": err.Error()}, http.StatusServiceUnavailable, err
	}
	token, err := config.accessToken(ctx, client)
	if err != nil {
		db.Exec(`UPDATE irev_submission_receipts SET submission_status='unavailable',last_error_code='token_unavailable',last_error_detail=?,updated_at=CURRENT_TIMESTAMP WHERE idempotency_key=?`, err.Error(), idempotencyKey)
		return map[string]interface{}{"idempotency_key": idempotencyKey, "status": "unavailable", "reason": err.Error()}, http.StatusServiceUnavailable, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(config.BaseURL, "/")+config.SubmitPath, bytes.NewReader(canonical))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	req.Header.Set("X-Election-Evidence-Hash", eventHash)
	req.Header.Set("X-Submission-Payload-SHA256", payloadHash)
	response, err := client.Do(req)
	if err != nil {
		db.Exec(`UPDATE irev_submission_receipts SET submission_status='failed',last_error_code='transport_failure',last_error_detail=?,updated_at=CURRENT_TIMESTAMP WHERE idempotency_key=?`, err.Error(), idempotencyKey)
		return map[string]interface{}{"idempotency_key": idempotencyKey, "status": "failed"}, http.StatusBadGateway, fmt.Errorf("IReV submit request: %w", err)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if readErr != nil {
		return nil, http.StatusBadGateway, readErr
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusAccepted {
		db.Exec(`UPDATE irev_submission_receipts SET submission_status='failed',last_error_code=?,last_error_detail=?,updated_at=CURRENT_TIMESTAMP WHERE idempotency_key=?`, fmt.Sprintf("http_%d", response.StatusCode), "IReV rejected submission", idempotencyKey)
		return map[string]interface{}{"idempotency_key": idempotencyKey, "status": "failed", "http_status": response.StatusCode}, http.StatusBadGateway, fmt.Errorf("IReV submit returned %d", response.StatusCode)
	}
	var externalReceipt irevExternalReceipt
	if err := json.Unmarshal(body, &externalReceipt); err != nil || strings.TrimSpace(externalReceipt.ReceiptID) == "" || strings.TrimSpace(externalReceipt.Status) == "" {
		db.Exec(`UPDATE irev_submission_receipts SET submission_status='reconciliation_required',last_error_code='invalid_receipt',last_error_detail='IReV response lacked receipt_id or status',updated_at=CURRENT_TIMESTAMP WHERE idempotency_key=?`, idempotencyKey)
		return map[string]interface{}{"idempotency_key": idempotencyKey, "status": "reconciliation_required"}, http.StatusBadGateway, fmt.Errorf("IReV response did not supply a verifiable receipt")
	}
	status := "submitted"
	acknowledgedAt := interface{}(nil)
	if strings.EqualFold(externalReceipt.Status, "acknowledged") || strings.EqualFold(externalReceipt.Status, "accepted") {
		status = "acknowledged"
		acknowledgedAt = time.Now().UTC()
	}
	redacted, _ := json.Marshal(redactIReVResponse(externalReceipt))
	_, err = db.Exec(`UPDATE irev_submission_receipts SET submission_status=?,external_receipt_id=?,external_transaction_id=?,external_status=?,external_response_redacted=?,submitted_at=CURRENT_TIMESTAMP,acknowledged_at=?,updated_at=CURRENT_TIMESTAMP WHERE idempotency_key=?`, status, externalReceipt.ReceiptID, nullIfEmpty(externalReceipt.TransactionID), externalReceipt.Status, string(redacted), acknowledgedAt, idempotencyKey)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return map[string]interface{}{"idempotency_key": idempotencyKey, "status": status, "receipt": redactIReVResponse(externalReceipt), "payload_sha256": payloadHash, "evidence_event_hash": eventHash}, http.StatusAccepted, nil
}

func handleIReVSubmit(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin", "ict_officer", "collation_officer")
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var request struct {
		ResultID int `json:"result_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request); err != nil || request.ResultID <= 0 {
		writeError(w, http.StatusBadRequest, "positive result_id is required")
		return
	}
	userID := claims["user_id"]
	response, status, submitErr := submitAuthorizedIReVResult(r.Context(), request.ResultID, userID)
	if submitErr != nil {
		writeJSON(w, status, M{"error": submitErr.Error(), "receipt": response})
		return
	}
	writeJSON(w, status, response)
}

func handleIReVStatus(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "ict_officer", "security", "collation_officer"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	config := currentIReVConfig()
	portalID, _ := upsertIReVPortalConfiguration(config)
	status := "unavailable"
	if err := config.configured(); err == nil {
		status = "configured"
	}
	counts := M{"pending": 0, "submitted": 0, "acknowledged": 0, "rejected": 0, "unavailable": 0, "failed": 0, "reconciliation_required": 0}
	rows, err := db.Query(`SELECT submission_status,COUNT(*) FROM irev_submission_receipts GROUP BY submission_status`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var key string
			var count int
			if rows.Scan(&key, &count) == nil {
				counts[key] = count
			}
		}
	}
	writeJSON(w, http.StatusOK, M{"status": status, "required": config.Required, "portal_connection_id": portalID, "submissions": counts})
}

func handleIReVReceipt(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "ict_officer", "security", "collation_officer", "observer"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	resultID := mux.Vars(r)["resultID"]
	row, err := querySingleRow(`SELECT * FROM irev_submission_receipts WHERE result_id=? ORDER BY created_at DESC LIMIT 1`, resultID)
	if err != nil {
		writeError(w, http.StatusNotFound, "IReV receipt not found")
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func handleIReVWebhook(w http.ResponseWriter, r *http.Request) {
	config := currentIReVConfig()
	if strings.TrimSpace(config.WebhookHMACSecret) == "" {
		writeError(w, http.StatusServiceUnavailable, "IReV webhook verification is not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read IReV webhook")
		return
	}
	signature := strings.TrimSpace(r.Header.Get("X-IReV-Signature"))
	mac := hmac.New(sha256.New, []byte(config.WebhookHMACSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	valid := hmac.Equal([]byte(strings.ToLower(signature)), []byte(expected))
	payloadHash := fmt.Sprintf("%x", sha256.Sum256(body))
	var receipt irevExternalReceipt
	if err := json.Unmarshal(body, &receipt); err != nil || strings.TrimSpace(receipt.ReceiptID) == "" {
		writeError(w, http.StatusBadRequest, "IReV webhook must contain receipt_id")
		return
	}
	portalID, _ := upsertIReVPortalConfiguration(config)
	redacted, _ := json.Marshal(redactIReVResponse(receipt))
	db.Exec(`INSERT INTO irev_webhook_receipts (portal_connection_id,external_receipt_id,delivery_id,payload_sha256,signature_valid,payload_redacted) VALUES (?,?,?,?,?,?) ON CONFLICT DO NOTHING`, portalID, receipt.ReceiptID, r.Header.Get("X-Delivery-ID"), payloadHash, boolToInt(valid), string(redacted))
	if !valid {
		writeError(w, http.StatusUnauthorized, "invalid IReV webhook signature")
		return
	}
	if receipt.Status != "" {
		db.Exec(`UPDATE irev_submission_receipts SET submission_status=?,external_status=?,acknowledged_at=CASE WHEN ? IN ('acknowledged','accepted') THEN CURRENT_TIMESTAMP ELSE acknowledged_at END,updated_at=CURRENT_TIMESTAMP WHERE external_receipt_id=?`, normalizeIReVStatus(receipt.Status), receipt.Status, strings.ToLower(receipt.Status), receipt.ReceiptID)
	}
	writeJSON(w, http.StatusAccepted, M{"status": "verified", "receipt_id": receipt.ReceiptID})
}

func normalizeIReVStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "acknowledged", "accepted":
		return "acknowledged"
	case "rejected":
		return "rejected"
	default:
		return "reconciliation_required"
	}
}

func handleLegacyPortalSyncDisabled(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeError(w, http.StatusServiceUnavailable, "generic portal synchronization is disabled; use the authorized IReV evidence submission workflow")
}
