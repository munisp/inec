package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// Evidence integrity is deliberately separate from the legacy audit table. The
// audit table records general actions; this module records deterministic,
// hash-chained evidence events that explain the lifecycle of a result.

const (
	integrityVisibilityPublic     = "public"
	integrityVisibilityObserver   = "observer"
	integrityVisibilityRestricted = "restricted"
)

type integrityEventInput struct {
	ResultID        int64
	EventType       string
	PolicyVersionID int64
	ArtifactID      int64
	Visibility      string
	PublicPayload   M
	PrivatePayload  M
	CreatedBy       int
}

type integrityEventRecord struct {
	SequenceNo     int64
	EventHash      string
	PriorEventHash string
	PayloadSHA256  string
	Signature      string
	SignerKeyID    string
	SignerStatus   string
}

type integritySignatureResponse struct {
	PayloadSHA256 string `json:"payload_sha256"`
	Signature     string `json:"signature"`
	KeyID         string `json:"key_id"`
}

type integrityVerificationResponse struct {
	Valid bool   `json:"valid"`
	KeyID string `json:"key_id"`
}

func initEvidenceIntegritySchema() {
	// Production PostgreSQL is managed by migrations. The local/test schema keeps
	// the same logical shape while remaining compatible with SQLite test fixtures.
	if usePostgres || os.Getenv("APP_ENV") == "production" {
		return
	}

	schema := `
	CREATE TABLE IF NOT EXISTS election_policy_versions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		election_id INTEGER NOT NULL,
		version TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'draft',
		legal_basis TEXT NOT NULL,
		rules_json TEXT NOT NULL DEFAULT '{}',
		rules_sha256 TEXT NOT NULL,
		approved_by INTEGER,
		approved_at TIMESTAMP,
		effective_from TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		effective_until TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(election_id, version)
	);
	CREATE TABLE IF NOT EXISTS evidence_artifacts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		election_id INTEGER NOT NULL,
		result_id INTEGER,
		artifact_kind TEXT NOT NULL,
		content_sha256 TEXT NOT NULL UNIQUE,
		media_type TEXT NOT NULL DEFAULT 'application/octet-stream',
		storage_uri TEXT,
		original_filename TEXT,
		byte_size INTEGER NOT NULL DEFAULT 0,
		metadata_json TEXT NOT NULL DEFAULT '{}',
		policy_version_id INTEGER,
		uploaded_by INTEGER,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS result_evidence_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		result_id INTEGER NOT NULL,
		sequence_no INTEGER NOT NULL,
		event_type TEXT NOT NULL,
		prior_event_hash TEXT,
		event_hash TEXT NOT NULL UNIQUE,
		payload_sha256 TEXT NOT NULL,
		signature TEXT,
		signer_key_id TEXT,
		signer_status TEXT NOT NULL DEFAULT 'not_required',
		artifact_id INTEGER,
		policy_version_id INTEGER,
		visibility TEXT NOT NULL DEFAULT 'restricted',
		public_payload TEXT NOT NULL DEFAULT '{}',
		private_payload TEXT NOT NULL DEFAULT '{}',
		created_by INTEGER,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(result_id, sequence_no)
	);
	CREATE TABLE IF NOT EXISTS reconciliation_cases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		result_id INTEGER NOT NULL,
		election_id INTEGER NOT NULL,
		case_type TEXT NOT NULL,
		severity TEXT NOT NULL DEFAULT 'medium',
		status TEXT NOT NULL DEFAULT 'open',
		blocking INTEGER NOT NULL DEFAULT 1,
		expected_value TEXT NOT NULL DEFAULT '{}',
		observed_value TEXT NOT NULL DEFAULT '{}',
		reason_code TEXT NOT NULL,
		description TEXT NOT NULL,
		evidence_artifact_id INTEGER,
		policy_version_id INTEGER,
		opened_by INTEGER,
		opened_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		resolution_reason TEXT,
		resolution_evidence_artifact_id INTEGER,
		resolved_by INTEGER,
		resolved_at TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS collation_evidence_bundles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		election_id INTEGER NOT NULL,
		level TEXT NOT NULL,
		area_code TEXT NOT NULL,
		bundle_version INTEGER NOT NULL DEFAULT 1,
		child_results_sha256 TEXT NOT NULL,
		aggregate_sha256 TEXT NOT NULL,
		event_root_sha256 TEXT,
		policy_version_id INTEGER,
		artifact_id INTEGER,
		status TEXT NOT NULL DEFAULT 'draft',
		created_by INTEGER,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		published_at TIMESTAMP,
		UNIQUE(election_id, level, area_code, bundle_version)
	);
	CREATE TABLE IF NOT EXISTS document_integrity_assessments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		result_id INTEGER,
		report_id INTEGER,
		artifact_id INTEGER NOT NULL,
		assessment_status TEXT NOT NULL,
		combined_confidence REAL,
		requires_manual_review INTEGER NOT NULL DEFAULT 1,
		manifest_sha256 TEXT NOT NULL,
		engine_versions TEXT NOT NULL DEFAULT '{}',
		assessment_json TEXT NOT NULL DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(artifact_id, manifest_sha256)
	);
	CREATE TABLE IF NOT EXISTS election_material_manifests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		election_id INTEGER NOT NULL,
		policy_version_id INTEGER NOT NULL,
		material_type TEXT NOT NULL,
		version TEXT NOT NULL,
		manifest_sha256 TEXT NOT NULL,
		artifact_id INTEGER,
		status TEXT NOT NULL DEFAULT 'draft',
		effective_from TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		effective_until TIMESTAMP,
		approved_by INTEGER,
		approved_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(election_id, material_type, version)
	);
	CREATE INDEX IF NOT EXISTS idx_evidence_events_result ON result_evidence_events(result_id, sequence_no);
	CREATE INDEX IF NOT EXISTS idx_reconciliation_cases_result ON reconciliation_cases(result_id, status, blocking);
	CREATE INDEX IF NOT EXISTS idx_evidence_artifacts_result ON evidence_artifacts(result_id, created_at);
	CREATE INDEX IF NOT EXISTS idx_collation_evidence_lookup ON collation_evidence_bundles(election_id, level, area_code, bundle_version);
	`
	execMulti(db, schema)
	// PostgreSQL enforces equivalent rules through migration 000020. SQLite is used
	// by local development and tests, so install trigger parity without passing its
	// trigger syntax through execMulti's semicolon splitter.
	if !usePostgres {
		for _, statement := range []string{
			`CREATE TRIGGER IF NOT EXISTS trg_result_evidence_events_no_update BEFORE UPDATE ON result_evidence_events BEGIN SELECT RAISE(ABORT, 'result evidence events are immutable'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_result_evidence_events_no_delete BEFORE DELETE ON result_evidence_events BEGIN SELECT RAISE(ABORT, 'result evidence events are immutable'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_evidence_artifacts_no_update BEFORE UPDATE ON evidence_artifacts BEGIN SELECT RAISE(ABORT, 'evidence artifacts are immutable'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_evidence_artifacts_no_delete BEFORE DELETE ON evidence_artifacts BEGIN SELECT RAISE(ABORT, 'evidence artifacts are immutable'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_document_assessments_no_update BEFORE UPDATE ON document_integrity_assessments BEGIN SELECT RAISE(ABORT, 'document integrity assessments are immutable'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_document_assessments_no_delete BEFORE DELETE ON document_integrity_assessments BEGIN SELECT RAISE(ABORT, 'document integrity assessments are immutable'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_policy_versions_no_delete BEFORE DELETE ON election_policy_versions BEGIN SELECT RAISE(ABORT, 'policy versions cannot be deleted'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_material_manifests_no_delete BEFORE DELETE ON election_material_manifests BEGIN SELECT RAISE(ABORT, 'material manifests cannot be deleted'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_collation_bundles_no_delete BEFORE DELETE ON collation_evidence_bundles BEGIN SELECT RAISE(ABORT, 'collation bundles cannot be deleted'); END`,
		} {
			if _, err := db.Exec(statement); err != nil {
				log.Warn().Err(err).Msg("failed to install SQLite evidence immutability trigger")
			}
		}
	}
}

func integritySigningRequired() bool {
	return strings.EqualFold(os.Getenv("INTEGRITY_SIGNING_REQUIRED"), "true") ||
		strings.EqualFold(os.Getenv("APP_ENV"), "production")
}

func integrityPolicyRequired() bool {
	return integritySigningRequired() || strings.EqualFold(os.Getenv("APP_ENV"), "staging")
}

type integritySignerHealthStatus struct {
	Status         string `json:"status"`
	SigningReady   bool   `json:"signing_ready"`
	VerifyingReady bool   `json:"verifying_ready"`
	KeyID          string `json:"key_id"`
}

func checkIntegritySignerHealth(ctx context.Context) M {
	required := integritySigningRequired()
	result := M{
		"required":   required,
		"configured": integrityServiceToken() != "",
		"status":     "not_required",
	}
	if !required && integrityServiceToken() == "" {
		return result
	}
	if integrityServiceToken() == "" {
		result["status"] = "unavailable"
		result["reason"] = "INTEGRITY_SERVICE_TOKEN is not configured"
		return result
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var response integritySignerHealthStatus
	if err := callIntegrityService(requestCtx, "/integrity/health", nil, &response); err != nil {
		result["status"] = "unavailable"
		result["reason"] = err.Error()
		return result
	}
	result["status"] = response.Status
	result["signing_ready"] = response.SigningReady
	result["verifying_ready"] = response.VerifyingReady
	if response.KeyID != "" {
		result["key_id"] = response.KeyID
	}
	return result
}

func officialVoterServiceURL() (string, error) {
	raw := strings.TrimSpace(os.Getenv("INEC_CVR_URL"))
	if raw == "" {
		raw = "https://cvr.inecnigeria.org/"
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("INEC_CVR_URL must be an absolute HTTPS URL")
	}
	return raw, nil
}

func handleIntegrityVoterServices(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "presiding_officer", "collation_officer", "observer", "public"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	cvrURL, err := officialVoterServiceURL()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "official voter-service navigation is unavailable: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, M{
		"services": []M{{
			"id":            "inec_continuous_voter_registration",
			"label":         "Official INEC Continuous Voter Registration",
			"url":           cvrURL,
			"purpose":       "registration, transfer, correction, and collection guidance",
			"authoritative": true,
		}},
		"notice": "This platform does not submit voter applications, hold voter-register copies, or determine eligibility.",
	})
}

func handleIntegrityHealth(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "presiding_officer", "collation_officer"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var openBlocking, unsignedEvents int
	if err := dbQueryRowCtx(r.Context(), convertPlaceholders(`SELECT COUNT(*) FROM reconciliation_cases WHERE status IN ('open','under_review') AND blocking=?`), true).Scan(&openBlocking); err != nil {
		writeError(w, http.StatusServiceUnavailable, "integrity reconciliation health is unavailable")
		return
	}
	if err := dbQueryRowCtx(r.Context(), `SELECT COUNT(*) FROM result_evidence_events WHERE signer_status='unavailable'`).Scan(&unsignedEvents); err != nil {
		writeError(w, http.StatusServiceUnavailable, "integrity event health is unavailable")
		return
	}
	signer := checkIntegritySignerHealth(r.Context())
	healthy := true
	if integritySigningRequired() {
		signingReady, _ := signer["signing_ready"].(bool)
		healthy = signer["status"] == "healthy" && signingReady && unsignedEvents == 0
	}
	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, M{
		"status": func() string {
			if healthy {
				return "healthy"
			}
			return "degraded"
		}(),
		"signer":                             signer,
		"open_blocking_reconciliation_cases": openBlocking,
		"events_with_unavailable_signer":     unsignedEvents,
		"signing_required":                   integritySigningRequired(),
	})
}

func canonicalJSON(value interface{}) ([]byte, error) {
	// encoding/json deterministically orders map keys. No timestamps or random
	// fields are added by this helper, making content hashes reproducible.
	return json.Marshal(value)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func payloadHash(payload M) (string, []byte, error) {
	if payload == nil {
		payload = M{}
	}
	data, err := canonicalJSON(payload)
	if err != nil {
		return "", nil, err
	}
	return sha256Hex(data), data, nil
}

func integrityEventHash(resultID, sequence int64, eventType, priorHash, payloadSHA string, policyVersionID, artifactID int64) string {
	canonical := fmt.Sprintf("%d|%d|%s|%s|%s|%d|%d", resultID, sequence, eventType, priorHash, payloadSHA, policyVersionID, artifactID)
	return sha256Hex([]byte(canonical))
}

func integrityServiceBaseURL() string {
	baseURL := strings.TrimRight(strings.TrimSpace(rustInferenceURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("RUST_INFERENCE_URL")), "/")
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8091"
	}
	return baseURL
}

func integrityServiceToken() string {
	return strings.TrimSpace(os.Getenv("INTEGRITY_SERVICE_TOKEN"))
}

func callIntegrityService(ctx context.Context, path string, payload interface{}, out interface{}) error {
	body, err := canonicalJSON(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, integrityServiceBaseURL()+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := integrityServiceToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("integrity service unavailable: %w", err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("integrity service returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("invalid integrity service response: %w", err)
	}
	return nil
}

func signIntegrityHash(ctx context.Context, hash string) (signature, keyID, signerStatus string, err error) {
	if !integritySigningRequired() {
		return "", "", "not_required", nil
	}
	if integrityServiceToken() == "" {
		return "", "", "unavailable", fmt.Errorf("INTEGRITY_SERVICE_TOKEN must be configured when integrity signing is required")
	}
	var response integritySignatureResponse
	if err := callIntegrityService(ctx, "/integrity/sign", M{"payload_sha256": hash}, &response); err != nil {
		return "", "", "unavailable", err
	}
	if response.PayloadSHA256 != hash || response.Signature == "" || response.KeyID == "" {
		return "", "", "unavailable", fmt.Errorf("integrity signer returned incomplete or mismatched evidence")
	}
	return response.Signature, response.KeyID, "signed", nil
}

func verifyIntegritySignature(ctx context.Context, hash, signature, keyID string) (bool, error) {
	if signature == "" {
		return !integritySigningRequired(), nil
	}
	if integrityServiceToken() == "" {
		return false, fmt.Errorf("integrity service token is unavailable")
	}
	var response integrityVerificationResponse
	if err := callIntegrityService(ctx, "/integrity/verify", M{
		"payload_sha256": hash,
		"signature":      signature,
		"key_id":         keyID,
	}, &response); err != nil {
		return false, err
	}
	return response.Valid && (response.KeyID == "" || response.KeyID == keyID), nil
}

func claimUserID(claims map[string]interface{}) int {
	userSub, _ := claims["sub"].(string)
	userID, _ := strconv.Atoi(userSub)
	return userID
}

func activePolicyVersionIDTx(ctx context.Context, tx *sql.Tx, electionID int) (int64, error) {
	var policyID int64
	err := tx.QueryRowContext(ctx, convertPlaceholders(`SELECT id FROM election_policy_versions
		WHERE election_id=? AND status='approved' AND (effective_until IS NULL OR effective_until > CURRENT_TIMESTAMP)
		ORDER BY effective_from DESC, id DESC LIMIT 1`), electionID).Scan(&policyID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return policyID, err
}

func activePolicyVersionID(ctx context.Context, electionID int) (int64, error) {
	var policyID int64
	err := dbQueryRowCtx(ctx, convertPlaceholders(`SELECT id FROM election_policy_versions
		WHERE election_id=? AND status='approved' AND (effective_until IS NULL OR effective_until > CURRENT_TIMESTAMP)
		ORDER BY effective_from DESC, id DESC LIMIT 1`), electionID).Scan(&policyID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return policyID, err
}

func requirePolicyVersion(ctx context.Context, tx *sql.Tx, electionID int) (int64, error) {
	var policyID int64
	var err error
	if tx != nil {
		policyID, err = activePolicyVersionIDTx(ctx, tx, electionID)
	} else {
		policyID, err = activePolicyVersionID(ctx, electionID)
	}
	if err != nil {
		return 0, err
	}
	if integrityPolicyRequired() && policyID == 0 {
		return 0, fmt.Errorf("an approved election policy version is required for integrity-controlled transitions")
	}
	return policyID, nil
}

func resultElectionIDTx(ctx context.Context, tx *sql.Tx, resultID int64) (int, error) {
	var electionID int
	if err := tx.QueryRowContext(ctx, convertPlaceholders("SELECT election_id FROM results WHERE id=?"), resultID).Scan(&electionID); err != nil {
		return 0, err
	}
	return electionID, nil
}

func lockResultForIntegrity(ctx context.Context, tx *sql.Tx, resultID int64) error {
	query := "SELECT id FROM results WHERE id=?"
	if usePostgres {
		query += " FOR UPDATE"
	}
	var id int64
	return tx.QueryRowContext(ctx, convertPlaceholders(query), resultID).Scan(&id)
}

func recordIntegrityEventTx(ctx context.Context, tx *sql.Tx, input integrityEventInput) (integrityEventRecord, error) {
	if input.ResultID <= 0 || strings.TrimSpace(input.EventType) == "" {
		return integrityEventRecord{}, fmt.Errorf("result_id and event_type are required for integrity events")
	}
	if err := lockResultForIntegrity(ctx, tx, input.ResultID); err != nil {
		return integrityEventRecord{}, err
	}
	if input.Visibility == "" {
		input.Visibility = integrityVisibilityRestricted
	}
	if input.Visibility != integrityVisibilityPublic && input.Visibility != integrityVisibilityObserver && input.Visibility != integrityVisibilityRestricted {
		return integrityEventRecord{}, fmt.Errorf("invalid evidence visibility")
	}
	if input.PublicPayload == nil {
		input.PublicPayload = M{}
	}
	if input.PrivatePayload == nil {
		input.PrivatePayload = M{}
	}

	var previousSequence int64
	var previousHash sql.NullString
	err := tx.QueryRowContext(ctx, convertPlaceholders(`SELECT sequence_no, event_hash FROM result_evidence_events
		WHERE result_id=? ORDER BY sequence_no DESC LIMIT 1`), input.ResultID).Scan(&previousSequence, &previousHash)
	if err != nil && err != sql.ErrNoRows {
		return integrityEventRecord{}, err
	}
	sequence := previousSequence + 1
	priorHash := ""
	if previousHash.Valid {
		priorHash = previousHash.String
	}
	payloadSHA, _, err := payloadHash(input.PrivatePayload)
	if err != nil {
		return integrityEventRecord{}, err
	}
	eventHash := integrityEventHash(input.ResultID, sequence, input.EventType, priorHash, payloadSHA, input.PolicyVersionID, input.ArtifactID)
	signature, keyID, signerStatus, err := signIntegrityHash(ctx, eventHash)
	if err != nil {
		return integrityEventRecord{}, err
	}
	publicJSON, err := canonicalJSON(input.PublicPayload)
	if err != nil {
		return integrityEventRecord{}, err
	}
	privateJSON, err := canonicalJSON(input.PrivatePayload)
	if err != nil {
		return integrityEventRecord{}, err
	}
	var artifactArg interface{}
	if input.ArtifactID > 0 {
		artifactArg = input.ArtifactID
	}
	var policyArg interface{}
	if input.PolicyVersionID > 0 {
		policyArg = input.PolicyVersionID
	}
	var signatureArg interface{}
	if signature != "" {
		signatureArg = signature
	}
	var keyArg interface{}
	if keyID != "" {
		keyArg = keyID
	}
	_, err = tx.ExecContext(ctx, convertPlaceholders(`INSERT INTO result_evidence_events
		(result_id, sequence_no, event_type, prior_event_hash, event_hash, payload_sha256, signature, signer_key_id, signer_status,
		 artifact_id, policy_version_id, visibility, public_payload, private_payload, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		input.ResultID, sequence, input.EventType, nullStringArg(priorHash), eventHash, payloadSHA,
		signatureArg, keyArg, signerStatus, artifactArg, policyArg, input.Visibility,
		string(publicJSON), string(privateJSON), nullIntArg(input.CreatedBy))
	if err != nil {
		return integrityEventRecord{}, err
	}
	return integrityEventRecord{
		SequenceNo:     sequence,
		EventHash:      eventHash,
		PriorEventHash: priorHash,
		PayloadSHA256:  payloadSHA,
		Signature:      signature,
		SignerKeyID:    keyID,
		SignerStatus:   signerStatus,
	}, nil
}

func nullStringArg(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func nullIntArg(value int) interface{} {
	if value <= 0 {
		return nil
	}
	return value
}

func resultHasBlockingCasesTx(ctx context.Context, tx *sql.Tx, resultID int64) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, convertPlaceholders(`SELECT COUNT(*) FROM reconciliation_cases
		WHERE result_id=? AND blocking=1 AND status IN ('open','under_review')`), resultID).Scan(&count)
	return count > 0, err
}

func recordResultIntegrityEvent(ctx context.Context, input integrityEventInput) (integrityEventRecord, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return integrityEventRecord{}, err
	}
	defer tx.Rollback()
	record, err := recordIntegrityEventTx(ctx, tx, input)
	if err != nil {
		return integrityEventRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return integrityEventRecord{}, err
	}
	return record, nil
}

func parseJSONMap(raw string) M {
	result := M{}
	if strings.TrimSpace(raw) == "" {
		return result
	}
	_ = json.Unmarshal([]byte(raw), &result)
	return result
}

func parseJSONValue(raw string) interface{} {
	var value interface{}
	if strings.TrimSpace(raw) == "" {
		return M{}
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return M{}
	}
	return value
}

func validIntegrityCaseType(value string) bool {
	_, ok := map[string]bool{
		"artifact_quality": true, "ocr_arithmetic": true, "structured_entry_mismatch": true,
		"accreditation_mismatch": true, "material_manifest_mismatch": true,
		"late_submission": true, "tampering_indicator": true, "other": true,
	}[value]
	return ok
}

func validIntegritySeverity(value string) bool {
	_, ok := map[string]bool{"low": true, "medium": true, "high": true, "critical": true}[value]
	return ok
}

func validMaterialType(value string) bool {
	_, ok := map[string]bool{
		"candidate_list": true, "party_list": true, "ballot_template": true,
		"ec8a_template": true, "ec8b_template": true, "ec8c_template": true,
	}[value]
	return ok
}

func handleIntegrityJourney(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "presiding_officer", "collation_officer", "observer", "public"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid result id")
		return
	}
	var electionID int
	var puCode, status, ec8aHash string
	var submittedAt time.Time
	if err := dbQueryRowCtx(r.Context(), convertPlaceholders(`SELECT election_id, polling_unit_code, status, COALESCE(ec8a_hash,''), submitted_at
		FROM results WHERE id=?`), id).Scan(&electionID, &puCode, &status, &ec8aHash, &submittedAt); err != nil {
		writeError(w, http.StatusNotFound, "result not found")
		return
	}

	events, err := listResultEvidenceEvents(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load evidence events")
		return
	}
	artifacts, err := listResultEvidenceArtifacts(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load evidence artifacts")
		return
	}
	cases, err := listResultReconciliationCases(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load reconciliation cases")
		return
	}
	verification := verifyEvidenceChain(r.Context(), id, events)
	policyID, _ := activePolicyVersionID(r.Context(), electionID)
	writeJSON(w, http.StatusOK, M{
		"result": M{
			"id": id, "election_id": electionID, "polling_unit_code": puCode, "status": status,
			"ec8a_hash": ec8aHash, "submitted_at": submittedAt.UTC().Format(time.RFC3339),
		},
		"policy_version_id":    policyID,
		"events":               events,
		"artifacts":            artifacts,
		"reconciliation_cases": cases,
		"verification":         verification,
		"public_safe":          true,
	})
}

func listResultEvidenceEvents(ctx context.Context, resultID int64) ([]M, error) {
	rows, err := dbQueryCtx(ctx, convertPlaceholders(`SELECT sequence_no, event_type, COALESCE(prior_event_hash,''), event_hash,
		payload_sha256, COALESCE(signature,''), COALESCE(signer_key_id,''), signer_status,
		COALESCE(artifact_id,0), COALESCE(policy_version_id,0), visibility, public_payload, created_at
		FROM result_evidence_events WHERE result_id=? ORDER BY sequence_no ASC`), resultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]M, 0)
	for rows.Next() {
		var sequence, artifactID, policyID int64
		var eventType, priorHash, eventHash, payloadSHA, signature, keyID, signerStatus, visibility, publicPayload string
		var createdAt time.Time
		if err := rows.Scan(&sequence, &eventType, &priorHash, &eventHash, &payloadSHA, &signature, &keyID, &signerStatus,
			&artifactID, &policyID, &visibility, &publicPayload, &createdAt); err != nil {
			return nil, err
		}
		events = append(events, M{
			"sequence_no": sequence, "event_type": eventType, "prior_event_hash": priorHash,
			"event_hash": eventHash, "payload_sha256": payloadSHA, "signature": signature,
			"signer_key_id": keyID, "signer_status": signerStatus, "artifact_id": artifactID,
			"policy_version_id": policyID, "visibility": visibility, "public_payload": parseJSONValue(publicPayload),
			"created_at": createdAt.UTC().Format(time.RFC3339),
		})
	}
	return events, rows.Err()
}

func listResultEvidenceArtifacts(ctx context.Context, resultID int64) ([]M, error) {
	rows, err := dbQueryCtx(ctx, convertPlaceholders(`SELECT id, artifact_kind, content_sha256, media_type,
		COALESCE(storage_uri,''), COALESCE(original_filename,''), byte_size, metadata_json,
		COALESCE(policy_version_id,0), created_at
		FROM evidence_artifacts WHERE result_id=? ORDER BY created_at ASC, id ASC`), resultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	artifacts := make([]M, 0)
	for rows.Next() {
		var id, byteSize, policyID int64
		var kind, hash, mediaType, storageURI, filename, metadata string
		var createdAt time.Time
		if err := rows.Scan(&id, &kind, &hash, &mediaType, &storageURI, &filename, &byteSize, &metadata, &policyID, &createdAt); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, M{
			"id": id, "artifact_kind": kind, "content_sha256": hash, "media_type": mediaType,
			"storage_uri": storageURI, "original_filename": filename, "byte_size": byteSize,
			"metadata": parseJSONValue(metadata), "policy_version_id": policyID,
			"created_at": createdAt.UTC().Format(time.RFC3339),
		})
	}
	return artifacts, rows.Err()
}

func listResultReconciliationCases(ctx context.Context, resultID int64) ([]M, error) {
	rows, err := dbQueryCtx(ctx, convertPlaceholders(`SELECT id, case_type, severity, status, blocking, expected_value, observed_value,
		reason_code, description, COALESCE(evidence_artifact_id,0), COALESCE(policy_version_id,0), opened_at,
		COALESCE(resolution_reason,''), COALESCE(resolved_at, CURRENT_TIMESTAMP)
		FROM reconciliation_cases WHERE result_id=? ORDER BY opened_at ASC, id ASC`), resultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cases := make([]M, 0)
	for rows.Next() {
		var id, artifactID, policyID int64
		var caseType, severity, status, expected, observed, reasonCode, description, resolution string
		var blocking bool
		var openedAt, resolvedAt time.Time
		if err := rows.Scan(&id, &caseType, &severity, &status, &blocking, &expected, &observed, &reasonCode, &description,
			&artifactID, &policyID, &openedAt, &resolution, &resolvedAt); err != nil {
			return nil, err
		}
		cases = append(cases, M{
			"id": id, "case_type": caseType, "severity": severity, "status": status, "blocking": blocking,
			"expected_value": parseJSONValue(expected), "observed_value": parseJSONValue(observed),
			"reason_code": reasonCode, "description": description, "evidence_artifact_id": artifactID,
			"policy_version_id": policyID, "opened_at": openedAt.UTC().Format(time.RFC3339),
			"resolution_reason": resolution, "resolved_at": resolvedAt.UTC().Format(time.RFC3339),
		})
	}
	return cases, rows.Err()
}

func verifyEvidenceChain(ctx context.Context, resultID int64, publicEvents []M) M {
	chainValid := true
	signatureValid := true
	signatureChecked := false
	failureReasons := make([]string, 0)
	priorHash := ""
	for index, event := range publicEvents {
		sequence, _ := event["sequence_no"].(int64)
		if sequence == 0 {
			switch value := event["sequence_no"].(type) {
			case int:
				sequence = int64(value)
			case float64:
				sequence = int64(value)
			}
		}
		if sequence != int64(index+1) {
			chainValid = false
			failureReasons = append(failureReasons, "event sequence is not contiguous")
		}
		storedPrior, _ := event["prior_event_hash"].(string)
		if storedPrior != priorHash {
			chainValid = false
			failureReasons = append(failureReasons, "event prior hash does not match the preceding event")
		}
		policyID := toInt64(event["policy_version_id"])
		artifactID := toInt64(event["artifact_id"])
		eventType, _ := event["event_type"].(string)
		payloadSHA, _ := event["payload_sha256"].(string)
		expectedHash := integrityEventHash(resultID, sequence, eventType, priorHash, payloadSHA, policyID, artifactID)
		storedHash, _ := event["event_hash"].(string)
		if expectedHash != storedHash {
			chainValid = false
			failureReasons = append(failureReasons, "event hash does not match its canonical payload")
		}
		signature, _ := event["signature"].(string)
		keyID, _ := event["signer_key_id"].(string)
		if signature != "" {
			signatureChecked = true
			valid, err := verifyIntegritySignature(ctx, storedHash, signature, keyID)
			if err != nil || !valid {
				signatureValid = false
				failureReasons = append(failureReasons, "signature verification failed or signer was unavailable")
			}
		}
		priorHash = storedHash
	}
	if integritySigningRequired() && !signatureChecked {
		signatureValid = false
		failureReasons = append(failureReasons, "integrity signing is required but no signature is present")
	}
	return M{
		"chain_valid": chainValid, "signature_checked": signatureChecked, "signature_valid": signatureValid,
		"event_count": len(publicEvents), "failure_reasons": failureReasons,
	}
}

func toInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

func handleVerifyIntegrityJourney(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "presiding_officer", "collation_officer", "observer", "public"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid result id")
		return
	}
	events, err := listResultEvidenceEvents(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load evidence events")
		return
	}
	if len(events) == 0 {
		writeJSON(w, http.StatusOK, M{"result_id": id, "chain_valid": false, "event_count": 0, "failure_reasons": []string{"no evidence events are recorded"}})
		return
	}
	verification := verifyEvidenceChain(r.Context(), id, events)
	verification["result_id"] = id
	writeJSON(w, http.StatusOK, verification)
}

func handleOpenReconciliationCase(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin", "collation_officer", "observer")
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	resultID, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || resultID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid result id")
		return
	}
	var request struct {
		CaseType           string `json:"case_type"`
		Severity           string `json:"severity"`
		Blocking           *bool  `json:"blocking"`
		ExpectedValue      M      `json:"expected_value"`
		ObservedValue      M      `json:"observed_value"`
		ReasonCode         string `json:"reason_code"`
		Description        string `json:"description"`
		EvidenceArtifactID int64  `json:"evidence_artifact_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	request.CaseType = strings.TrimSpace(request.CaseType)
	request.Severity = strings.TrimSpace(request.Severity)
	if request.Severity == "" {
		request.Severity = "medium"
	}
	if !validIntegrityCaseType(request.CaseType) || !validIntegritySeverity(request.Severity) || strings.TrimSpace(request.ReasonCode) == "" || strings.TrimSpace(request.Description) == "" {
		writeError(w, http.StatusBadRequest, "case_type, severity, reason_code, and description must be valid")
		return
	}
	blocking := true
	if request.Blocking != nil {
		blocking = *request.Blocking
	}
	expectedJSON, err := canonicalJSON(request.ExpectedValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid expected_value")
		return
	}
	observedJSON, err := canonicalJSON(request.ObservedValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid observed_value")
		return
	}
	userID := claimUserID(claims)
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database transaction failed")
		return
	}
	defer tx.Rollback()
	electionID, err := resultElectionIDTx(r.Context(), tx, resultID)
	if err != nil {
		writeError(w, http.StatusNotFound, "result not found")
		return
	}
	policyID, err := requirePolicyVersion(r.Context(), tx, electionID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	var artifactArg interface{}
	if request.EvidenceArtifactID > 0 {
		artifactArg = request.EvidenceArtifactID
	}
	caseID, err := integrityInsertReturningID(r.Context(), tx, `INSERT INTO reconciliation_cases
		(result_id, election_id, case_type, severity, status, blocking, expected_value, observed_value,
		reason_code, description, evidence_artifact_id, policy_version_id, opened_by)
		VALUES (?, ?, ?, ?, 'open', ?, ?, ?, ?, ?, ?, ?, ?)`,
		resultID, electionID, request.CaseType, request.Severity, blocking, string(expectedJSON), string(observedJSON),
		request.ReasonCode, request.Description, artifactArg, nullInt64Arg(policyID), nullIntArg(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to open reconciliation case")
		return
	}
	_, err = recordIntegrityEventTx(r.Context(), tx, integrityEventInput{
		ResultID: resultID, EventType: "RECONCILIATION_CASE_OPENED", PolicyVersionID: policyID,
		ArtifactID: request.EvidenceArtifactID, Visibility: integrityVisibilityObserver, CreatedBy: userID,
		PublicPayload:  M{"case_id": caseID, "case_type": request.CaseType, "severity": request.Severity, "blocking": blocking, "reason_code": request.ReasonCode},
		PrivatePayload: M{"case_id": caseID, "expected_value": request.ExpectedValue, "observed_value": request.ObservedValue, "description": request.Description},
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "integrity event could not be recorded: "+err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit reconciliation case")
		return
	}
	writeJSON(w, http.StatusCreated, M{"id": caseID, "result_id": resultID, "status": "open", "blocking": blocking})
}

func handleResolveReconciliationCase(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin", "collation_officer")
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	caseID, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || caseID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid reconciliation case id")
		return
	}
	var request struct {
		Status                     string `json:"status"`
		ResolutionReason           string `json:"resolution_reason"`
		ResolutionEvidenceArtifact int64  `json:"resolution_evidence_artifact_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.Status != "resolved" && request.Status != "dismissed" || strings.TrimSpace(request.ResolutionReason) == "" {
		writeError(w, http.StatusBadRequest, "status must be resolved or dismissed and resolution_reason is required")
		return
	}
	userID := claimUserID(claims)
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database transaction failed")
		return
	}
	defer tx.Rollback()
	var resultID int64
	var electionID int
	var currentStatus string
	if err := tx.QueryRowContext(r.Context(), convertPlaceholders(`SELECT result_id, election_id, status FROM reconciliation_cases WHERE id=?`), caseID).Scan(&resultID, &electionID, &currentStatus); err != nil {
		writeError(w, http.StatusNotFound, "reconciliation case not found")
		return
	}
	if currentStatus != "open" && currentStatus != "under_review" {
		writeError(w, http.StatusConflict, "reconciliation case is already closed")
		return
	}
	policyID, err := requirePolicyVersion(r.Context(), tx, electionID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	var artifactArg interface{}
	if request.ResolutionEvidenceArtifact > 0 {
		artifactArg = request.ResolutionEvidenceArtifact
	}
	if _, err := tx.ExecContext(r.Context(), convertPlaceholders(`UPDATE reconciliation_cases
		SET status=?, resolution_reason=?, resolution_evidence_artifact_id=?, resolved_by=?, resolved_at=CURRENT_TIMESTAMP WHERE id=?`),
		request.Status, request.ResolutionReason, artifactArg, nullIntArg(userID), caseID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve reconciliation case")
		return
	}
	_, err = recordIntegrityEventTx(r.Context(), tx, integrityEventInput{
		ResultID: resultID, EventType: "RECONCILIATION_CASE_RESOLVED", PolicyVersionID: policyID,
		ArtifactID: request.ResolutionEvidenceArtifact, Visibility: integrityVisibilityObserver, CreatedBy: userID,
		PublicPayload:  M{"case_id": caseID, "status": request.Status},
		PrivatePayload: M{"case_id": caseID, "resolution_reason": request.ResolutionReason},
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "integrity event could not be recorded: "+err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit reconciliation resolution")
		return
	}
	writeJSON(w, http.StatusOK, M{"id": caseID, "status": request.Status})
}

func integrityInsertReturningID(ctx context.Context, tx *sql.Tx, query string, args ...interface{}) (int64, error) {
	query = strings.TrimRight(strings.TrimSpace(query), ";") + " RETURNING id"
	var id int64
	err := tx.QueryRowContext(ctx, convertPlaceholders(query), args...).Scan(&id)
	return id, err
}

func nullInt64Arg(value int64) interface{} {
	if value <= 0 {
		return nil
	}
	return value
}

func handleCreateElectionPolicy(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var request struct {
		ElectionID  int    `json:"election_id"`
		Version     string `json:"version"`
		LegalBasis  string `json:"legal_basis"`
		Rules       M      `json:"rules"`
		EffectiveAt string `json:"effective_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.ElectionID <= 0 || strings.TrimSpace(request.Version) == "" || strings.TrimSpace(request.LegalBasis) == "" {
		writeError(w, http.StatusBadRequest, "election_id, version, and legal_basis are required")
		return
	}
	rulesJSON, err := canonicalJSON(request.Rules)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rules")
		return
	}
	effectiveAt := time.Now().UTC()
	if request.EffectiveAt != "" {
		parsed, parseErr := time.Parse(time.RFC3339, request.EffectiveAt)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "effective_at must be RFC3339")
			return
		}
		effectiveAt = parsed.UTC()
	}
	userID := claimUserID(claims)
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database transaction failed")
		return
	}
	defer tx.Rollback()
	var electionExists int
	if err := tx.QueryRowContext(r.Context(), convertPlaceholders("SELECT COUNT(*) FROM elections WHERE id=?"), request.ElectionID).Scan(&electionExists); err != nil || electionExists == 0 {
		writeError(w, http.StatusNotFound, "election not found")
		return
	}
	if _, err := tx.ExecContext(r.Context(), convertPlaceholders(`UPDATE election_policy_versions
		SET status='superseded', effective_until=? WHERE election_id=? AND status='approved' AND effective_until IS NULL`), effectiveAt, request.ElectionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to supersede active policy")
		return
	}
	policyID, err := integrityInsertReturningID(r.Context(), tx, `INSERT INTO election_policy_versions
		(election_id, version, status, legal_basis, rules_json, rules_sha256, approved_by, approved_at, effective_from)
		VALUES (?, ?, 'approved', ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		request.ElectionID, request.Version, request.LegalBasis, string(rulesJSON), sha256Hex(rulesJSON), nullIntArg(userID), effectiveAt)
	if err != nil {
		writeError(w, http.StatusConflict, "failed to create approved policy version")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit policy version")
		return
	}
	logAudit("ELECTION_POLICY_APPROVED", "election_policy", fmt.Sprintf("%d", policyID), userID, M{"election_id": request.ElectionID, "version": request.Version})
	writeJSON(w, http.StatusCreated, M{"id": policyID, "election_id": request.ElectionID, "version": request.Version, "status": "approved", "rules_sha256": sha256Hex(rulesJSON)})
}

func handleListElectionPolicies(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "presiding_officer", "collation_officer", "observer", "public"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	electionID := queryParamInt(r, "election_id", 0)
	if electionID <= 0 {
		writeError(w, http.StatusBadRequest, "election_id is required")
		return
	}
	rows, err := dbQueryCtx(r.Context(), convertPlaceholders(`SELECT id, version, status, legal_basis, rules_json, rules_sha256,
		effective_from, effective_until, created_at FROM election_policy_versions WHERE election_id=? ORDER BY effective_from DESC, id DESC`), electionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load election policies")
		return
	}
	defer rows.Close()
	policies := make([]M, 0)
	for rows.Next() {
		var id int64
		var version, status, legalBasis, rulesJSON, rulesHash string
		var effectiveFrom, createdAt time.Time
		var effectiveUntil sql.NullTime
		if err := rows.Scan(&id, &version, &status, &legalBasis, &rulesJSON, &rulesHash, &effectiveFrom, &effectiveUntil, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse election policy")
			return
		}
		policy := M{"id": id, "version": version, "status": status, "legal_basis": legalBasis, "rules": parseJSONValue(rulesJSON), "rules_sha256": rulesHash,
			"effective_from": effectiveFrom.UTC().Format(time.RFC3339), "created_at": createdAt.UTC().Format(time.RFC3339)}
		if effectiveUntil.Valid {
			policy["effective_until"] = effectiveUntil.Time.UTC().Format(time.RFC3339)
		}
		policies = append(policies, policy)
	}
	writeJSON(w, http.StatusOK, M{"election_id": electionID, "policies": policies})
}

func handleCreateMaterialManifest(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var request struct {
		ElectionID      int    `json:"election_id"`
		PolicyVersionID int64  `json:"policy_version_id"`
		MaterialType    string `json:"material_type"`
		Version         string `json:"version"`
		Manifest        M      `json:"manifest"`
		StorageURI      string `json:"storage_uri"`
		OriginalName    string `json:"original_filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.ElectionID <= 0 || request.PolicyVersionID <= 0 || !validMaterialType(request.MaterialType) || strings.TrimSpace(request.Version) == "" {
		writeError(w, http.StatusBadRequest, "election_id, policy_version_id, material_type, and version are required")
		return
	}
	manifestJSON, err := canonicalJSON(request.Manifest)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid manifest")
		return
	}
	manifestSHA := sha256Hex(manifestJSON)
	userID := claimUserID(claims)
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database transaction failed")
		return
	}
	defer tx.Rollback()
	var policyElectionID int
	var policyStatus string
	if err := tx.QueryRowContext(r.Context(), convertPlaceholders("SELECT election_id, status FROM election_policy_versions WHERE id=?"), request.PolicyVersionID).Scan(&policyElectionID, &policyStatus); err != nil || policyElectionID != request.ElectionID || policyStatus != "approved" {
		writeError(w, http.StatusConflict, "material manifest requires an approved policy version for the same election")
		return
	}
	artifactID, err := integrityInsertReturningID(r.Context(), tx, `INSERT INTO evidence_artifacts
		(election_id, artifact_kind, content_sha256, media_type, storage_uri, original_filename, byte_size, metadata_json, policy_version_id, uploaded_by)
		VALUES (?, 'material_manifest', ?, 'application/json', ?, ?, ?, ?, ?, ?)`, request.ElectionID, manifestSHA, request.StorageURI,
		request.OriginalName, len(manifestJSON), string(manifestJSON), request.PolicyVersionID, nullIntArg(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store material manifest evidence")
		return
	}
	manifestID, err := integrityInsertReturningID(r.Context(), tx, `INSERT INTO election_material_manifests
		(election_id, policy_version_id, material_type, version, manifest_sha256, artifact_id, status, approved_by, approved_at)
		VALUES (?, ?, ?, ?, ?, ?, 'approved', ?, CURRENT_TIMESTAMP)`, request.ElectionID, request.PolicyVersionID,
		request.MaterialType, request.Version, manifestSHA, artifactID, nullIntArg(userID))
	if err != nil {
		writeError(w, http.StatusConflict, "failed to register material manifest")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit material manifest")
		return
	}
	logAudit("MATERIAL_MANIFEST_APPROVED", "material_manifest", fmt.Sprintf("%d", manifestID), userID, M{"election_id": request.ElectionID, "material_type": request.MaterialType, "version": request.Version, "sha256": manifestSHA})
	writeJSON(w, http.StatusCreated, M{"id": manifestID, "artifact_id": artifactID, "manifest_sha256": manifestSHA, "status": "approved"})
}

func handleListMaterialManifests(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "presiding_officer", "collation_officer", "observer", "public"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	electionID := queryParamInt(r, "election_id", 0)
	if electionID <= 0 {
		writeError(w, http.StatusBadRequest, "election_id is required")
		return
	}
	rows, err := dbQueryCtx(r.Context(), convertPlaceholders(`SELECT m.id, m.policy_version_id, m.material_type, m.version, m.manifest_sha256,
		COALESCE(m.artifact_id,0), m.status, m.effective_from, m.effective_until, COALESCE(a.storage_uri,''), a.metadata_json
		FROM election_material_manifests m LEFT JOIN evidence_artifacts a ON a.id=m.artifact_id
		WHERE m.election_id=? ORDER BY m.material_type, m.effective_from DESC, m.id DESC`), electionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load material manifests")
		return
	}
	defer rows.Close()
	manifests := make([]M, 0)
	for rows.Next() {
		var id, policyID, artifactID int64
		var materialType, version, hash, status, storageURI string
		var effectiveFrom time.Time
		var effectiveUntil sql.NullTime
		var metadata sql.NullString
		if err := rows.Scan(&id, &policyID, &materialType, &version, &hash, &artifactID, &status, &effectiveFrom, &effectiveUntil, &storageURI, &metadata); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse material manifest")
			return
		}
		item := M{"id": id, "policy_version_id": policyID, "material_type": materialType, "version": version, "manifest_sha256": hash,
			"artifact_id": artifactID, "status": status, "storage_uri": storageURI, "effective_from": effectiveFrom.UTC().Format(time.RFC3339)}
		if metadata.Valid {
			item["manifest"] = parseJSONValue(metadata.String)
		}
		if effectiveUntil.Valid {
			item["effective_until"] = effectiveUntil.Time.UTC().Format(time.RFC3339)
		}
		manifests = append(manifests, item)
	}
	writeJSON(w, http.StatusOK, M{"election_id": electionID, "material_manifests": manifests})
}

func handleBuildCollationEvidenceBundle(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin", "collation_officer")
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var request struct {
		ElectionID int    `json:"election_id"`
		Level      string `json:"level"`
		AreaCode   string `json:"area_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.ElectionID <= 0 || (request.Level != "ward" && request.Level != "lga" && request.Level != "state" && request.Level != "national") {
		writeError(w, http.StatusBadRequest, "election_id and a valid level are required")
		return
	}
	if request.Level != "national" && strings.TrimSpace(request.AreaCode) == "" {
		writeError(w, http.StatusBadRequest, "area_code is required except for national collation")
		return
	}
	if request.Level == "national" {
		request.AreaCode = "NG"
	}
	userID := claimUserID(claims)
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database transaction failed")
		return
	}
	defer tx.Rollback()
	policyID, err := requirePolicyVersion(r.Context(), tx, request.ElectionID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	children, partyTotals, err := collectCollationEvidence(r.Context(), tx, request.ElectionID, request.Level, request.AreaCode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to collect collation evidence: "+err.Error())
		return
	}
	if len(children) == 0 {
		writeError(w, http.StatusConflict, "no validated or finalized child results are available for this collation bundle")
		return
	}
	childJSON, _ := canonicalJSON(children)
	aggregateJSON, _ := canonicalJSON(M{"party_totals": partyTotals, "child_count": len(children)})
	childSHA := sha256Hex(childJSON)
	aggregateSHA := sha256Hex(aggregateJSON)
	eventRoot := ""
	if len(children) > 0 {
		if hash, ok := children[len(children)-1]["last_event_hash"].(string); ok {
			eventRoot = hash
		}
	}
	bundlePayload := M{
		"election_id": request.ElectionID, "level": request.Level, "area_code": request.AreaCode,
		"children": children, "party_totals": partyTotals, "child_results_sha256": childSHA,
		"aggregate_sha256": aggregateSHA, "event_root_sha256": eventRoot, "policy_version_id": policyID,
	}
	bundleJSON, _ := canonicalJSON(bundlePayload)
	artifactID, err := integrityInsertReturningID(r.Context(), tx, `INSERT INTO evidence_artifacts
		(election_id, artifact_kind, content_sha256, media_type, byte_size, metadata_json, policy_version_id, uploaded_by)
		VALUES (?, 'collation_bundle', ?, 'application/json', ?, ?, ?, ?)`, request.ElectionID, sha256Hex(bundleJSON), len(bundleJSON), string(bundleJSON), nullInt64Arg(policyID), nullIntArg(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store collation evidence artifact")
		return
	}
	var previousVersion int
	if err := tx.QueryRowContext(r.Context(), convertPlaceholders(`SELECT COALESCE(MAX(bundle_version),0) FROM collation_evidence_bundles
		WHERE election_id=? AND level=? AND area_code=?`), request.ElectionID, request.Level, request.AreaCode).Scan(&previousVersion); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to allocate collation bundle version")
		return
	}
	bundleID, err := integrityInsertReturningID(r.Context(), tx, `INSERT INTO collation_evidence_bundles
		(election_id, level, area_code, bundle_version, child_results_sha256, aggregate_sha256, event_root_sha256,
		policy_version_id, artifact_id, status, created_by, published_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'published', ?, CURRENT_TIMESTAMP)`,
		request.ElectionID, request.Level, request.AreaCode, previousVersion+1, childSHA, aggregateSHA, nullStringArg(eventRoot),
		nullInt64Arg(policyID), artifactID, nullIntArg(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist collation evidence bundle")
		return
	}
	for _, child := range children {
		resultID := toInt64(child["result_id"])
		if resultID <= 0 {
			continue
		}
		_, err := recordIntegrityEventTx(r.Context(), tx, integrityEventInput{
			ResultID: resultID, EventType: "COLLATION_BUNDLED", PolicyVersionID: policyID, ArtifactID: artifactID,
			Visibility: integrityVisibilityPublic, CreatedBy: userID,
			PublicPayload:  M{"bundle_id": bundleID, "level": request.Level, "area_code": request.AreaCode, "aggregate_sha256": aggregateSHA},
			PrivatePayload: M{"bundle_id": bundleID, "result_id": resultID, "child_results_sha256": childSHA},
		})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "collation evidence signing failed: "+err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit collation evidence bundle")
		return
	}
	writeJSON(w, http.StatusCreated, M{"id": bundleID, "artifact_id": artifactID, "status": "published", "child_results_sha256": childSHA, "aggregate_sha256": aggregateSHA})
}

func collectCollationEvidence(ctx context.Context, tx *sql.Tx, electionID int, level, areaCode string) ([]M, M, error) {
	query := `SELECT r.id, COALESCE((SELECT event_hash FROM result_evidence_events ree WHERE ree.result_id=r.id ORDER BY sequence_no DESC LIMIT 1), '')
		FROM results r JOIN polling_units pu ON pu.code=r.polling_unit_code
		JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code
		WHERE r.election_id=? AND r.status IN ('validated','finalized')`
	args := []interface{}{electionID}
	switch level {
	case "ward":
		query += " AND pu.ward_code=?"
		args = append(args, areaCode)
	case "lga":
		query += " AND w.lga_code=?"
		args = append(args, areaCode)
	case "state":
		query += " AND l.state_code=?"
		args = append(args, areaCode)
	case "national":
		// no additional scope
	default:
		return nil, nil, fmt.Errorf("invalid collation level")
	}
	query += " ORDER BY r.id ASC"
	rows, err := tx.QueryContext(ctx, convertPlaceholders(query), args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	children := make([]M, 0)
	resultIDs := make([]int64, 0)
	for rows.Next() {
		var resultID int64
		var lastEventHash string
		if err := rows.Scan(&resultID, &lastEventHash); err != nil {
			return nil, nil, err
		}
		children = append(children, M{"result_id": resultID, "last_event_hash": lastEventHash})
		resultIDs = append(resultIDs, resultID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	partyTotals := M{}
	for _, resultID := range resultIDs {
		partyRows, err := tx.QueryContext(ctx, convertPlaceholders("SELECT party_code, votes FROM result_party_scores WHERE result_id=? ORDER BY party_code"), resultID)
		if err != nil {
			return nil, nil, err
		}
		for partyRows.Next() {
			var party string
			var votes int64
			if err := partyRows.Scan(&party, &votes); err != nil {
				partyRows.Close()
				return nil, nil, err
			}
			current := toInt64(partyTotals[party])
			partyTotals[party] = current + votes
		}
		partyRows.Close()
	}
	// Convert map-like child evidence to a stable order before hashing. The SQL
	// query is already ordered, but this makes intent explicit for future callers.
	sort.Slice(children, func(i, j int) bool { return toInt64(children[i]["result_id"]) < toInt64(children[j]["result_id"]) })
	return children, partyTotals, nil
}

func handleGetCollationEvidenceBundle(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "presiding_officer", "collation_officer", "observer", "public"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	electionID, err := strconv.Atoi(mux.Vars(r)["election_id"])
	if err != nil || electionID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid election id")
		return
	}
	level := mux.Vars(r)["level"]
	areaCode := mux.Vars(r)["area_code"]
	var bundleID, policyID, artifactID int64
	var version int
	var childSHA, aggregateSHA, eventRoot, status string
	var createdAt time.Time
	err = dbQueryRowCtx(r.Context(), convertPlaceholders(`SELECT id, bundle_version, child_results_sha256, aggregate_sha256,
		COALESCE(event_root_sha256,''), COALESCE(policy_version_id,0), COALESCE(artifact_id,0), status, created_at
		FROM collation_evidence_bundles WHERE election_id=? AND level=? AND area_code=?
		ORDER BY bundle_version DESC LIMIT 1`), electionID, level, areaCode).Scan(&bundleID, &version, &childSHA, &aggregateSHA, &eventRoot, &policyID, &artifactID, &status, &createdAt)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "published collation evidence bundle not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load collation evidence bundle")
		return
	}
	var metadata string
	if artifactID > 0 {
		_ = dbQueryRowCtx(r.Context(), convertPlaceholders("SELECT metadata_json FROM evidence_artifacts WHERE id=?"), artifactID).Scan(&metadata)
	}
	writeJSON(w, http.StatusOK, M{
		"id": bundleID, "election_id": electionID, "level": level, "area_code": areaCode, "bundle_version": version,
		"child_results_sha256": childSHA, "aggregate_sha256": aggregateSHA, "event_root_sha256": eventRoot,
		"policy_version_id": policyID, "artifact_id": artifactID, "status": status,
		"bundle": parseJSONValue(metadata), "created_at": createdAt.UTC().Format(time.RFC3339),
	})
}
