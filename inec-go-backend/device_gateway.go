package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

const (
	deviceGatewayEnvelopeVersion = "bvas-envelope-v1"
	deviceGatewayNonceTTL        = 30 * time.Minute
	deviceGatewayMaxClockSkew    = 10 * time.Minute
)

// DeviceGatewayEnvelope is a device-originated, signed, canonical event. It never
// accepts raw PVC values, biometric templates, result images, or private keys.
type DeviceGatewayEnvelope struct {
	Version         string          `json:"version"`
	DeviceID        string          `json:"device_id"`
	ElectionID      int             `json:"election_id"`
	PollingUnitCode string          `json:"polling_unit_code"`
	EventType       string          `json:"event_type"`
	Sequence        int64           `json:"sequence"`
	Nonce           string          `json:"nonce"`
	ObservedAt      string          `json:"observed_at"`
	PayloadSHA256   string          `json:"payload_sha256"`
	Payload         json.RawMessage `json:"payload"`
	Signature       string          `json:"signature"`
}

type deviceEnrollmentRequest struct {
	DeviceID                     string   `json:"device_id"`
	ElectionID                   int      `json:"election_id"`
	PollingUnitCode              string   `json:"polling_unit_code"`
	PublicKeyBase64              string   `json:"public_key_base64"`
	CertificateFingerprintSHA256 string   `json:"certificate_fingerprint_sha256"`
	FirmwareAllowlist            []string `json:"firmware_allowlist"`
	AttestationPolicyVersion     string   `json:"attestation_policy_version"`
	Activate                     bool     `json:"activate"`
}

type deviceVerificationResponse struct {
	Valid          bool            `json:"valid"`
	EnvelopeSHA256 string          `json:"envelope_sha256"`
	PayloadSHA256  string          `json:"payload_sha256"`
	Attestation    json.RawMessage `json:"attestation"`
	Reason         string          `json:"reason,omitempty"`
}

type enrolledDeviceTrust struct {
	DeviceID                     string
	ElectionID                   int
	PollingUnitCode              string
	PublicKeyBase64              string
	CertificateFingerprintSHA256 string
	FirmwareAllowlist            []string
	PolicyVersion                string
	Status                       string
}

func initDeviceGatewaySchema(database *sql.DB) {
	// Development/test parity for migration 000022. Production PostgreSQL applies
	// the stronger migration with JSONB, UUID, indexes, and immutability triggers.
	schema := `
	CREATE TABLE IF NOT EXISTS bvas_device_enrollments (
		device_id TEXT PRIMARY KEY,
		election_id INTEGER NOT NULL,
		polling_unit_code TEXT NOT NULL,
		public_key_base64 TEXT NOT NULL,
		certificate_fingerprint_sha256 TEXT NOT NULL,
		firmware_allowlist TEXT NOT NULL DEFAULT '[]',
		attestation_policy_version TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		enrolled_by INTEGER,
		activated_at TIMESTAMP,
		revoked_at TIMESTAMP,
		revocation_reason TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS bvas_device_gateway_inbox (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id TEXT NOT NULL,
		election_id INTEGER NOT NULL,
		polling_unit_code TEXT NOT NULL,
		event_type TEXT NOT NULL,
		sequence_no INTEGER NOT NULL,
		nonce_sha256 TEXT NOT NULL,
		payload_sha256 TEXT NOT NULL,
		envelope_sha256 TEXT NOT NULL,
		signature_base64 TEXT NOT NULL,
		verifier_attestation TEXT NOT NULL,
		edge_certificate_fingerprint_sha256 TEXT NOT NULL,
		observed_at TIMESTAMP NOT NULL,
		received_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		correlation_id TEXT NOT NULL,
		processing_status TEXT NOT NULL DEFAULT 'accepted',
		rejection_reason TEXT,
		UNIQUE(device_id, sequence_no),
		UNIQUE(device_id, nonce_sha256),
		UNIQUE(envelope_sha256)
	);
	CREATE TABLE IF NOT EXISTS external_integration_outbox (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		correlation_id TEXT NOT NULL,
		source_type TEXT NOT NULL,
		aggregate_type TEXT NOT NULL,
		aggregate_id TEXT NOT NULL,
		event_type TEXT NOT NULL,
		event_version TEXT NOT NULL DEFAULT 'v1',
		partition_key TEXT NOT NULL,
		payload_redacted TEXT NOT NULL,
		payload_sha256 TEXT NOT NULL,
		required_sinks TEXT NOT NULL DEFAULT '[]',
		delivery_status TEXT NOT NULL DEFAULT 'pending',
		attempt_count INTEGER NOT NULL DEFAULT 0,
		next_attempt_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_error TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		delivered_at TIMESTAMP,
		UNIQUE(event_type, aggregate_type, aggregate_id, payload_sha256)
	);
	CREATE TABLE IF NOT EXISTS external_portal_integrations (
		portal_code TEXT PRIMARY KEY,
		integration_status TEXT NOT NULL DEFAULT 'unconfigured',
		endpoint_origin TEXT,
		api_version TEXT,
		authentication_mode TEXT,
		callback_verification_policy TEXT,
		configuration_hash TEXT,
		authorization_reference TEXT,
		approved_by INTEGER,
		approved_at TIMESTAMP,
		last_verified_at TIMESTAMP,
		failure_reason TEXT,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS external_portal_submission_attempts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		portal_code TEXT NOT NULL,
		result_id INTEGER,
		artifact_id INTEGER,
		correlation_id TEXT NOT NULL,
		request_hash TEXT NOT NULL,
		request_signature TEXT NOT NULL,
		external_reference TEXT,
		receipt_hash TEXT,
		receipt_payload_redacted TEXT,
		state TEXT NOT NULL DEFAULT 'pending',
		response_code INTEGER,
		failure_reason TEXT,
		submitted_at TIMESTAMP,
		received_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(portal_code, request_hash)
	);
	`
	execMulti(database, schema)
	database.Exec("INSERT OR IGNORE INTO external_portal_integrations (portal_code) VALUES ('irev')")
}

func externalDeviceGatewayRequired() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") || envBool("BVAS_DEVICE_GATEWAY_REQUIRED", false)
}

func deviceGatewayRequiredService(name string, connected bool) error {
	if externalDeviceGatewayRequired() && !connected {
		return fmt.Errorf("required device gateway dependency %s is unavailable", name)
	}
	return nil
}

func normalizeLowerHex64(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) != 64 {
		return "", fmt.Errorf("expected 64-character lowercase SHA-256 hex")
	}
	if _, err := hex.DecodeString(v); err != nil {
		return "", fmt.Errorf("invalid SHA-256 hex")
	}
	return v, nil
}

func currentGatewayActor(r *http.Request) (string, error) {
	claims, err := getCurrentUser(r)
	if err != nil {
		return "", err
	}
	for _, k := range []string{"sub", "user_id", "id", "email"} {
		if v, ok := claims[k]; ok {
			value := strings.TrimSpace(fmt.Sprint(v))
			if value != "" && value != "<nil>" {
				return value, nil
			}
		}
	}
	return "", fmt.Errorf("authenticated principal has no stable subject")
}

func requireDeviceEnrollmentAuthorization(r *http.Request, deviceID string) (string, error) {
	if _, err := requireRole(r, "admin", "ict_officer"); err != nil {
		return "", err
	}
	actor, err := currentGatewayActor(r)
	if err != nil {
		return "", err
	}
	if mwHub == nil || mwHub.Permify == nil {
		if externalDeviceGatewayRequired() {
			return "", fmt.Errorf("Permify authorization is required for device enrollment")
		}
		return actor, nil
	}
	allowed, err := mwHub.Permify.Check(r.Context(), PermifyCheck{
		Subject: actor, SubjectType: "user", Permission: "manage_bvas", Resource: deviceID, ResourceType: "bvas_device",
	})
	if err != nil {
		if externalDeviceGatewayRequired() {
			return "", fmt.Errorf("check device enrollment permission: %w", err)
		}
		return actor, nil
	}
	if !allowed {
		return "", fmt.Errorf("permission denied to enroll device")
	}
	return actor, nil
}

func handleDeviceGatewayEnroll(w http.ResponseWriter, r *http.Request) {
	var req deviceEnrollmentRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid enrollment request")
		return
	}
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.PollingUnitCode = strings.TrimSpace(req.PollingUnitCode)
	req.AttestationPolicyVersion = strings.TrimSpace(req.AttestationPolicyVersion)
	if req.DeviceID == "" || req.ElectionID <= 0 || req.PollingUnitCode == "" || req.AttestationPolicyVersion == "" {
		writeError(w, http.StatusBadRequest, "device_id, election_id, polling_unit_code, and attestation_policy_version are required")
		return
	}
	if _, err := base64.StdEncoding.DecodeString(req.PublicKeyBase64); err != nil {
		writeError(w, http.StatusBadRequest, "public_key_base64 must be standard base64")
		return
	}
	if len(req.PublicKeyBase64) < 40 {
		writeError(w, http.StatusBadRequest, "public_key_base64 is too short")
		return
	}
	fingerprint, err := normalizeLowerHex64(req.CertificateFingerprintSHA256)
	if err != nil {
		writeError(w, http.StatusBadRequest, "certificate_fingerprint_sha256 "+err.Error())
		return
	}
	actor, err := requireDeviceEnrollmentAuthorization(r, req.DeviceID)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	var actualElection int
	var actualPU string
	var status string
	if err := db.QueryRow("SELECT election_id, polling_unit_code, status FROM bvas_devices WHERE id=?", req.DeviceID).Scan(&actualElection, &actualPU, &status); err != nil {
		writeError(w, http.StatusNotFound, "registered BVAS device not found")
		return
	}
	if actualElection != req.ElectionID || actualPU != req.PollingUnitCode {
		writeError(w, http.StatusConflict, "enrollment assignment does not match registered device")
		return
	}
	if status == "decommissioned" || status == "lost" {
		writeError(w, http.StatusConflict, "device cannot be enrolled in its current status")
		return
	}
	allowlist, _ := json.Marshal(req.FirmwareAllowlist)
	enrollmentStatus := "pending"
	if req.Activate {
		enrollmentStatus = "active"
	}
	_, err = db.Exec(`INSERT INTO bvas_device_enrollments
		(device_id,election_id,polling_unit_code,public_key_base64,certificate_fingerprint_sha256,firmware_allowlist,attestation_policy_version,status,enrolled_by,activated_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,CASE WHEN ? THEN CURRENT_TIMESTAMP ELSE NULL END,CURRENT_TIMESTAMP)
		ON CONFLICT(device_id) DO UPDATE SET election_id=excluded.election_id,polling_unit_code=excluded.polling_unit_code,public_key_base64=excluded.public_key_base64,certificate_fingerprint_sha256=excluded.certificate_fingerprint_sha256,firmware_allowlist=excluded.firmware_allowlist,attestation_policy_version=excluded.attestation_policy_version,status=excluded.status,enrolled_by=excluded.enrolled_by,activated_at=excluded.activated_at,updated_at=CURRENT_TIMESTAMP`,
		req.DeviceID, req.ElectionID, req.PollingUnitCode, req.PublicKeyBase64, fingerprint, string(allowlist), req.AttestationPolicyVersion, enrollmentStatus, actor, req.Activate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "persist device enrollment")
		return
	}
	if mwHub != nil && mwHub.Permify != nil {
		if err := mwHub.Permify.WriteRelationship(r.Context(), req.DeviceID, "bvas_device", "assigned", req.PollingUnitCode, "polling_unit"); err != nil && externalDeviceGatewayRequired() {
			writeError(w, http.StatusServiceUnavailable, "persist device authorization relationship")
			return
		}
	}
	auditWrite("BVAS_DEVICE_ENROLLED", "bvas_device", req.DeviceID, r, M{"election_id": req.ElectionID, "polling_unit_code": req.PollingUnitCode, "status": enrollmentStatus, "certificate_fingerprint": fingerprint})
	writeJSON(w, http.StatusCreated, M{"device_id": req.DeviceID, "status": enrollmentStatus, "attestation_policy_version": req.AttestationPolicyVersion})
}

func loadEnrolledDeviceTrust(ctx context.Context, deviceID string) (*enrolledDeviceTrust, error) {
	var trust enrolledDeviceTrust
	var allowlistRaw string
	err := db.QueryRowContext(ctx, `SELECT device_id,election_id,polling_unit_code,public_key_base64,certificate_fingerprint_sha256,firmware_allowlist,attestation_policy_version,status
		FROM bvas_device_enrollments WHERE device_id=?`, deviceID).Scan(&trust.DeviceID, &trust.ElectionID, &trust.PollingUnitCode, &trust.PublicKeyBase64, &trust.CertificateFingerprintSHA256, &allowlistRaw, &trust.PolicyVersion, &trust.Status)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(allowlistRaw), &trust.FirmwareAllowlist)
	return &trust, nil
}

func requireTrustedDeviceEdge(r *http.Request, trust *enrolledDeviceTrust) (string, error) {
	if externalDeviceGatewayRequired() && !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Device-Gateway")), "apisix-mtls") {
		return "", fmt.Errorf("device traffic must arrive through the configured APISIX mTLS gateway")
	}
	fingerprint := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Device-Certificate-Fingerprint")))
	if fingerprint == "" {
		fingerprint = strings.ToLower(strings.TrimSpace(r.Header.Get("X-SSL-Client-Fingerprint")))
	}
	mtlsVerified := strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Device-MTLS-Verified")), "true") || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-SSL-Client-Verify")), "SUCCESS")
	if !mtlsVerified {
		return "", fmt.Errorf("verified device mTLS edge header is required")
	}
	fingerprint, err := normalizeLowerHex64(fingerprint)
	if err != nil {
		return "", fmt.Errorf("invalid device certificate fingerprint")
	}
	if fingerprint != trust.CertificateFingerprintSHA256 {
		return "", fmt.Errorf("device certificate fingerprint does not match enrollment")
	}
	return fingerprint, nil
}

func validateDeviceGatewayEnvelope(envelope *DeviceGatewayEnvelope) (time.Time, string, string, error) {
	if envelope.Version != deviceGatewayEnvelopeVersion {
		return time.Time{}, "", "", fmt.Errorf("unsupported envelope version")
	}
	if strings.TrimSpace(envelope.DeviceID) == "" || envelope.ElectionID <= 0 || strings.TrimSpace(envelope.PollingUnitCode) == "" {
		return time.Time{}, "", "", fmt.Errorf("device identity, election, and polling unit are required")
	}
	if envelope.Sequence <= 0 || strings.TrimSpace(envelope.Nonce) == "" || strings.TrimSpace(envelope.Signature) == "" {
		return time.Time{}, "", "", fmt.Errorf("sequence, nonce, and signature are required")
	}
	if _, err := base64.StdEncoding.DecodeString(envelope.Signature); err != nil {
		return time.Time{}, "", "", fmt.Errorf("signature must be standard base64")
	}
	if _, err := base64.RawURLEncoding.DecodeString(envelope.Nonce); err != nil {
		return time.Time{}, "", "", fmt.Errorf("nonce must be base64url without padding")
	}
	observedAt, err := time.Parse(time.RFC3339, envelope.ObservedAt)
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("observed_at must be RFC3339")
	}
	if time.Since(observedAt) > deviceGatewayMaxClockSkew || observedAt.After(time.Now().UTC().Add(deviceGatewayMaxClockSkew)) {
		return time.Time{}, "", "", fmt.Errorf("device timestamp outside permitted clock skew")
	}
	payloadHash, err := normalizeLowerHex64(envelope.PayloadSHA256)
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("payload_sha256 %w", err)
	}
	if !map[string]bool{"accreditation": true, "result_capture": true, "heartbeat": true, "incident": true}[envelope.EventType] {
		return time.Time{}, "", "", fmt.Errorf("unsupported device event type")
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return time.Time{}, "", "", fmt.Errorf("payload must be JSON object")
	}
	canonicalPayload, err := json.Marshal(payload)
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("canonicalize device payload")
	}
	calculated := sha256.Sum256(canonicalPayload)
	if payloadHash != hex.EncodeToString(calculated[:]) {
		return time.Time{}, "", "", fmt.Errorf("payload SHA-256 mismatch")
	}
	for _, prohibited := range []string{"voter_pvc_number", "biometric_template", "fingerprint_image", "face_embedding", "raw_image", "result_image"} {
		if _, exists := payload[prohibited]; exists {
			return time.Time{}, "", "", fmt.Errorf("payload contains prohibited raw sensitive field %s", prohibited)
		}
	}
	return observedAt.UTC(), sha256Hex([]byte(envelope.Nonce)), payloadHash, nil
}

func verifyDeviceEnvelopeWithIntegrityService(ctx context.Context, envelope DeviceGatewayEnvelope, trust *enrolledDeviceTrust) (*deviceVerificationResponse, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("INFERENCE_ENGINE_URL")), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("RUST_INFERENCE_URL")), "/")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("Rust integrity service URL is not configured")
	}
	token := strings.TrimSpace(os.Getenv("INTEGRITY_SERVICE_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("integrity service token is not configured")
	}
	requestBody, _ := json.Marshal(M{"envelope": envelope, "public_key_base64": trust.PublicKeyBase64, "attestation_policy_version": trust.PolicyVersion})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/integrity/device/verify", bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device verifier unavailable: %w", err)
	}
	defer resp.Body.Close()
	var verification deviceVerificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&verification); err != nil {
		return nil, fmt.Errorf("decode device verifier response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || !verification.Valid {
		if verification.Reason == "" {
			verification.Reason = "signature verification rejected"
		}
		return nil, fmt.Errorf("%s", verification.Reason)
	}
	return &verification, nil
}

func acquireDeviceGatewayNonce(ctx context.Context, deviceID, nonceHash string) error {
	if mwHub == nil || mwHub.Redis == nil {
		return deviceGatewayRequiredService("redis", false)
	}
	key := "device-gateway:nonce:" + deviceID + ":" + nonceHash
	value, err := mwHub.Redis.Get(ctx, key)
	if err == nil && value != "" {
		return fmt.Errorf("replayed device nonce")
	}
	if err != nil && externalDeviceGatewayRequired() {
		return fmt.Errorf("check device nonce: %w", err)
	}
	if err := mwHub.Redis.Set(ctx, key, "1", deviceGatewayNonceTTL); err != nil {
		if externalDeviceGatewayRequired() {
			return fmt.Errorf("record device nonce: %w", err)
		}
	}
	return nil
}

func deviceGatewayAccreditationPayload(payload json.RawMessage) (string, bool, bool, string, error) {
	var value struct {
		VoterPVCHash   string `json:"voter_pvc_hash"`
		BiometricMatch bool   `json:"biometric_match"`
		PVCVerified    bool   `json:"pvc_verified"`
		Method         string `json:"method"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return "", false, false, "", err
	}
	normalizedHash, err := normalizeLowerHex64(value.VoterPVCHash)
	if err != nil {
		return "", false, false, "", fmt.Errorf("voter_pvc_hash %w", err)
	}
	value.VoterPVCHash = normalizedHash
	if value.Method == "" {
		value.Method = "biometric"
	}
	if !map[string]bool{"biometric": true, "manual": true, "override": true}[value.Method] {
		return "", false, false, "", fmt.Errorf("invalid accreditation method")
	}
	return value.VoterPVCHash, value.BiometricMatch, value.PVCVerified, value.Method, nil
}

func redactedDeviceAnalyticsPayload(envelope DeviceGatewayEnvelope, observedAt time.Time, payloadHash string, inboxID int64, verification *deviceVerificationResponse) M {
	payload := M{}
	_ = json.Unmarshal(envelope.Payload, &payload)
	attestation := M{}
	_ = json.Unmarshal(verification.Attestation, &attestation)
	result := M{
		"device_id":                  envelope.DeviceID,
		"event_hash":                 verification.EnvelopeSHA256,
		"election_id":                envelope.ElectionID,
		"polling_unit_code":          envelope.PollingUnitCode,
		"event_type":                 envelope.EventType,
		"sequence":                   envelope.Sequence,
		"observed_at":                observedAt.UTC().Format(time.RFC3339Nano),
		"payload_sha256":             payloadHash,
		"envelope_sha256":            verification.EnvelopeSHA256,
		"attestation_key_id":         attestation["key_id"],
		"attestation_policy_version": attestation["attestation_policy_version"],
		"inbox_id":                   inboxID,
	}
	for source, target := range map[string]string{
		"battery_level":    "battery_level",
		"signal_strength":  "signal_strength",
		"gps_latitude":     "latitude",
		"gps_longitude":    "longitude",
		"firmware_version": "firmware_version",
		"sync_queue_size":  "sync_queue_size",
	} {
		if value, exists := payload[source]; exists {
			result[target] = value
		}
	}
	return result
}

func persistDeviceGatewayEnvelope(ctx context.Context, envelope DeviceGatewayEnvelope, trust *enrolledDeviceTrust, observedAt time.Time, nonceHash, payloadHash, edgeFingerprint string, verification *deviceVerificationResponse) (int64, string, error) {
	correlationID := uuid.NewString()
	attestation := verification.Attestation
	if len(attestation) == 0 {
		attestation = json.RawMessage(`{}`)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO bvas_device_gateway_inbox
		(device_id,election_id,polling_unit_code,event_type,sequence_no,nonce_sha256,payload_sha256,envelope_sha256,signature_base64,verifier_attestation,edge_certificate_fingerprint_sha256,observed_at,correlation_id,processing_status)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,'accepted')`,
		envelope.DeviceID, envelope.ElectionID, envelope.PollingUnitCode, envelope.EventType, envelope.Sequence, nonceHash, payloadHash, verification.EnvelopeSHA256, envelope.Signature, string(attestation), edgeFingerprint, observedAt, correlationID)
	if err != nil {
		return 0, "", fmt.Errorf("persist immutable device inbox: %w", err)
	}
	inboxID, err := result.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	if envelope.EventType == "accreditation" {
		pvcHash, biometricMatch, pvcVerified, method, err := deviceGatewayAccreditationPayload(envelope.Payload)
		if err != nil {
			return 0, "", err
		}
		var deviceStatus, electionStatus string
		if err := tx.QueryRowContext(ctx, "SELECT status FROM bvas_devices WHERE id=?", envelope.DeviceID).Scan(&deviceStatus); err != nil || deviceStatus != "active" {
			return 0, "", fmt.Errorf("device is not active")
		}
		if err := tx.QueryRowContext(ctx, "SELECT status FROM elections WHERE id=?", envelope.ElectionID).Scan(&electionStatus); err != nil || (electionStatus != "voting" && electionStatus != "active") {
			return 0, "", fmt.Errorf("election is not accepting accreditation")
		}
		var existing int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM bvas_accreditations WHERE voter_pvc_hash=? AND election_id=? AND polling_unit_code=?", pvcHash, envelope.ElectionID, envelope.PollingUnitCode).Scan(&existing); err != nil {
			return 0, "", err
		}
		if existing > 0 {
			return 0, "", fmt.Errorf("voter already accredited at this polling unit")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bvas_accreditations (device_id,election_id,polling_unit_code,voter_pvc_hash,biometric_match,pvc_verified,method,accredited_at,synced_at)
			VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, envelope.DeviceID, envelope.ElectionID, envelope.PollingUnitCode, pvcHash, boolToInt(biometricMatch), boolToInt(pvcVerified), method); err != nil {
			return 0, "", fmt.Errorf("persist accredited device event: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE bvas_devices SET last_sync_at=CURRENT_TIMESTAMP WHERE id=?", envelope.DeviceID); err != nil {
		return 0, "", err
	}
	redactedPayload, _ := json.Marshal(redactedDeviceAnalyticsPayload(envelope, observedAt, payloadHash, inboxID, verification))
	if _, err := tx.ExecContext(ctx, `INSERT INTO external_integration_outbox (correlation_id,source_type,aggregate_type,aggregate_id,event_type,event_version,partition_key,payload_redacted,payload_sha256,required_sinks,delivery_status)
		VALUES (?,?,?,?,?,'v1',?,?,?,'["kafka","dapr","fluvio","opensearch"]','pending') ON CONFLICT DO NOTHING`,
		correlationID, "bvas_gateway", "device_gateway_inbox", fmt.Sprintf("%d", inboxID), "inec.bvas.device-event.v1", envelope.DeviceID, string(redactedPayload), sha256Hex(redactedPayload)); err != nil {
		return 0, "", fmt.Errorf("enqueue device event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return inboxID, correlationID, nil
}

func inspectDeviceGatewayRequest(r *http.Request, rawBody []byte) error {
	if mwHub == nil || mwHub.OpenAppSec == nil {
		return deviceGatewayRequiredService("openappsec", false)
	}
	headers := map[string]string{
		"Content-Type":                     r.Header.Get("Content-Type"),
		"User-Agent":                       r.UserAgent(),
		"X-Device-Gateway":                 r.Header.Get("X-Device-Gateway"),
		"X-Device-Certificate-Fingerprint": r.Header.Get("X-Device-Certificate-Fingerprint"),
		"X-SSL-Client-Fingerprint":         r.Header.Get("X-SSL-Client-Fingerprint"),
		"X-SSL-Client-Verify":              r.Header.Get("X-SSL-Client-Verify"),
	}
	decision, err := mwHub.OpenAppSec.InspectRequest(r.Context(), WAFRequest{
		SourceIP:    getClientIP(r),
		Method:      r.Method,
		Path:        r.URL.Path,
		QueryString: r.URL.RawQuery,
		Headers:     headers,
		Body:        string(rawBody),
		UserAgent:   r.UserAgent(),
	})
	if err != nil {
		if externalDeviceGatewayRequired() {
			return fmt.Errorf("OpenAppSec device inspection unavailable: %w", err)
		}
		return nil
	}
	if decision == nil {
		return fmt.Errorf("OpenAppSec returned no device-ingress decision")
	}
	if !strings.EqualFold(decision.Action, "allow") {
		return fmt.Errorf("device ingress blocked by OpenAppSec: %s", decision.ThreatLevel)
	}
	return nil
}

func handleDeviceGatewayEvent(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, 131072)
	defer body.Close()
	rawBody, err := io.ReadAll(body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "device envelope exceeds the maximum permitted size")
		return
	}
	if err := inspectDeviceGatewayRequest(r, rawBody); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var envelope DeviceGatewayEnvelope
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		writeError(w, http.StatusBadRequest, "invalid signed device envelope")
		return
	}
	observedAt, nonceHash, payloadHash, err := validateDeviceGatewayEnvelope(&envelope)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	trust, err := loadEnrolledDeviceTrust(r.Context(), envelope.DeviceID)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusForbidden, "device is not enrolled")
			return
		}
		writeError(w, http.StatusServiceUnavailable, "load device enrollment")
		return
	}
	if trust.Status != "active" {
		writeError(w, http.StatusForbidden, "device enrollment is not active")
		return
	}
	if trust.ElectionID != envelope.ElectionID || trust.PollingUnitCode != envelope.PollingUnitCode {
		writeError(w, http.StatusConflict, "device enrollment does not match event assignment")
		return
	}
	edgeFingerprint, err := requireTrustedDeviceEdge(r, trust)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err := acquireDeviceGatewayNonce(r.Context(), envelope.DeviceID, nonceHash); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	verification, err := verifyDeviceEnvelopeWithIntegrityService(r.Context(), envelope, trust)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if verification.PayloadSHA256 != payloadHash {
		writeError(w, http.StatusUnprocessableEntity, "verifier payload hash mismatch")
		return
	}
	if _, err := normalizeLowerHex64(verification.EnvelopeSHA256); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "verifier returned invalid envelope hash")
		return
	}
	inboxID, correlationID, err := persistDeviceGatewayEnvelope(r.Context(), envelope, trust, observedAt, nonceHash, payloadHash, edgeFingerprint, verification)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			writeError(w, http.StatusConflict, "replayed or conflicting device event")
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	auditWrite("BVAS_DEVICE_ENVELOPE_ACCEPTED", "bvas_device_gateway_inbox", fmt.Sprintf("%d", inboxID), r, M{"device_id": envelope.DeviceID, "event_type": envelope.EventType, "sequence": envelope.Sequence, "correlation_id": correlationID, "envelope_sha256": verification.EnvelopeSHA256})
	writeJSON(w, http.StatusAccepted, M{"inbox_id": inboxID, "correlation_id": correlationID, "status": "accepted", "envelope_sha256": verification.EnvelopeSHA256})
}

func handleDeviceGatewayHealth(w http.ResponseWriter, r *http.Request) {
	statuses := M{
		"required":                     externalDeviceGatewayRequired(),
		"apisix_mtls_configured":       strings.TrimSpace(os.Getenv("DEVICE_GATEWAY_SNI")) != "",
		"redis":                        false,
		"permify":                      false,
		"openappsec":                   false,
		"opensearch":                   false,
		"kafka":                        false,
		"dapr":                         false,
		"fluvio":                       false,
		"temporal":                     false,
		"integrity_service_configured": false,
	}
	if mwHub != nil {
		if mwHub.Redis != nil {
			statuses["redis"] = mwHub.Redis.Ping().Connected
		}
		if mwHub.Permify != nil {
			statuses["permify"] = mwHub.Permify.Status().Connected
		}
		if mwHub.OpenAppSec != nil {
			statuses["openappsec"] = mwHub.OpenAppSec.Status().Connected
		}
		if mwHub.OpenSearch != nil {
			statuses["opensearch"] = mwHub.OpenSearch.Status().Connected
		}
		if mwHub.Kafka != nil {
			statuses["kafka"] = mwHub.Kafka.Status().Connected
		}
		if mwHub.Dapr != nil {
			statuses["dapr"] = mwHub.Dapr.Status().Connected
		}
		if mwHub.Fluvio != nil {
			statuses["fluvio"] = mwHub.Fluvio.Status().Connected
		}
		if mwHub.Temporal != nil {
			statuses["temporal"] = mwHub.Temporal.Status().Connected
		}
	}
	statuses["integrity_service_configured"] = strings.TrimSpace(os.Getenv("INFERENCE_ENGINE_URL")) != "" || strings.TrimSpace(os.Getenv("RUST_INFERENCE_URL")) != ""
	healthy := statuses["apisix_mtls_configured"].(bool) &&
		statuses["redis"].(bool) && statuses["permify"].(bool) &&
		statuses["openappsec"].(bool) && statuses["opensearch"].(bool) &&
		statuses["kafka"].(bool) && statuses["dapr"].(bool) &&
		statuses["fluvio"].(bool) && statuses["temporal"].(bool) &&
		statuses["integrity_service_configured"].(bool)
	if externalDeviceGatewayRequired() && !healthy {
		writeJSON(w, http.StatusServiceUnavailable, M{"status": "unavailable", "components": statuses})
		return
	}
	writeJSON(w, http.StatusOK, M{"status": "ready", "components": statuses})
}

func handleDeviceGatewayOperationalStatus(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "ict_officer", "security", "collation_officer", "observer"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, "device gateway status database is unavailable")
		return
	}
	enrollments := M{"pending": 0, "active": 0, "suspended": 0, "revoked": 0, "expired": 0}
	rows, err := db.QueryContext(r.Context(), `SELECT status,COUNT(*) FROM bvas_device_enrollments GROUP BY status`)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "load device enrollment status: "+err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err == nil {
			enrollments[status] = count
		}
	}
	delivery := M{"pending": 0, "delivering": 0, "delivered": 0, "failed": 0, "quarantined": 0}
	deliveryRows, err := db.QueryContext(r.Context(), `SELECT delivery_status,COUNT(*) FROM external_integration_outbox WHERE source_type='bvas_gateway' GROUP BY delivery_status`)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "load device delivery status: "+err.Error())
		return
	}
	defer deliveryRows.Close()
	for deliveryRows.Next() {
		var status string
		var count int
		if err := deliveryRows.Scan(&status, &count); err == nil {
			delivery[status] = count
		}
	}
	writeJSON(w, http.StatusOK, M{
		"status":      "available",
		"enrollments": enrollments,
		"delivery":    delivery,
		"notice":      "Counts cover only authoritative gateway enrollment and redacted external-device outbox records.",
	})
}

func rejectLegacyDeviceIngress(w http.ResponseWriter) bool {
	if !externalDeviceGatewayRequired() {
		return false
	}
	writeError(w, http.StatusGone, "legacy staff-authenticated BVAS ingestion is disabled; use the mTLS device gateway")
	return true
}

func registerDeviceGatewayRoutes(r *mux.Router) {
	r.HandleFunc("/device-gateway/v1/enrollments", authRequired(handleDeviceGatewayEnroll)).Methods(http.MethodPost)
	r.HandleFunc("/device-gateway/v1/events", handleDeviceGatewayEvent).Methods(http.MethodPost)
	r.HandleFunc("/device-gateway/v1/health", readAuth(handleDeviceGatewayHealth)).Methods(http.MethodGet)
	r.HandleFunc("/device-gateway/v1/status", readAuth(handleDeviceGatewayOperationalStatus)).Methods(http.MethodGet)
}
