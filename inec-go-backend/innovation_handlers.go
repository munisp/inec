package main

// innovation_handlers.go
// Production-complete HTTP handlers for all 10 next-generation innovation modules.
// Each handler proxies to the appropriate microservice or executes inline logic.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// ── Service URL helpers ────────────────────────────────────────────────────────

func anomalyServiceURL() string {
	return envString("ANOMALY_SERVICE_URL", "http://ai-anomaly-detection:8000")
}
func homomorphicURL() string {
	return envString("HOMOMORPHIC_SERVICE_URL", "http://homomorphic-tally:8000")
}
func federatedURL() string {
	return envString("FEDERATED_SERVICE_URL", "http://federated-fraud-detection:8000")
}
func digitalTwinURL() string {
	return envString("DIGITAL_TWIN_SERVICE_URL", "http://digital-twin-simulation:8000")
}
func satelliteURL() string {
	return strings.TrimRight(envString("SATELLITE_SERVICE_URL", "http://satellite-change-detection:8204"), "/")
}
func predictiveAllocURL() string {
	return envString("PREDICTIVE_ALLOC_URL", "http://predictive-resource-allocation:8000")
}

// proxyToService forwards a request to a downstream microservice and streams back the response.
func proxyToService(w http.ResponseWriter, r *http.Request, targetURL string) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		writeError(w, 502, "failed to build proxy request")
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	req.Header.Set("X-Request-ID", r.Header.Get("X-Request-ID"))
	req.Header.Set("X-User-ID", r.Header.Get("X-User-ID"))

	resp, err := client.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("target", targetURL).Msg("proxy_service_unavailable")
		writeError(w, 503, "innovation service temporarily unavailable")
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// ── Innovation 1: AI Anomaly Detection ────────────────────────────────────────

// handleAnomalyDetect proxies a real voting record to the governance-aware
// anomaly gateway. The gateway returns 503 when its approved model or inference
// dependency is unavailable; this handler does not substitute a local score.
func handleAnomalyDetect(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, anomalyServiceURL()+"/api/v1/anomaly/score")
}

// handleAnomalyModelStatus returns governance and runtime health from the
// anomaly gateway. It is intentionally a health endpoint, not a fabricated
// model-accuracy report.
func handleAnomalyModelStatus(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, anomalyServiceURL()+"/api/v1/anomaly/health")
}

// ── Innovation 2: Zero-Knowledge Proof Voter Verification ─────────────────────

// ZKP eligibility proof creation and verification require an approved proof
// service, key custody, verifier registry, and persisted audit evidence. The
// previous inline routines produced random values and structural checks only.
func handleZKPGenerateProof(w http.ResponseWriter, r *http.Request) {
	writeError(
		w,
		http.StatusServiceUnavailable,
		"ZKP eligibility proofs are unavailable until an approved proof service is configured",
	)
}

func handleZKPVerifyProof(w http.ResponseWriter, r *http.Request) {
	writeError(
		w,
		http.StatusServiceUnavailable,
		"ZKP proof verification is unavailable until an approved verifier is configured",
	)
}

func handleZKPStats(w http.ResponseWriter, r *http.Request) {
	writeError(
		w,
		http.StatusServiceUnavailable,
		"ZKP verification statistics are unavailable until an authoritative verifier is configured",
	)
}

// ── Innovation 3: Homomorphic Encryption Vote Tallying ────────────────────────

// handleHomomorphicEncryptVote encrypts a vote using homomorphic encryption.
func handleHomomorphicEncryptVote(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, homomorphicURL()+"/encrypt")
}

// handleHomomorphicTally performs a tally on encrypted votes without decryption.
func handleHomomorphicTally(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, homomorphicURL()+"/tally")
}

// handleHomomorphicDecryptResult decrypts the final aggregated tally (admin only).
func handleHomomorphicDecryptResult(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, homomorphicURL()+"/decrypt")
}

// handleHomomorphicStats returns the homomorphic tally service status.
func handleHomomorphicStats(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, homomorphicURL()+"/stats")
}

// ── Innovation 4: Federated Learning Fraud Detection ─────────────────────────

// handleFederatedModelUpdate receives a model weight update from a regional node.
func handleFederatedModelUpdate(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, federatedURL()+"/model/update")
}

// handleFederatedAggregate triggers federated aggregation of all regional models.
func handleFederatedAggregate(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, federatedURL()+"/model/aggregate")
}

// handleFederatedFraudScore returns a fraud risk score for a given transaction.
func handleFederatedFraudScore(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, federatedURL()+"/score")
}

// handleFederatedStats returns federated learning model performance metrics.
func handleFederatedStats(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, federatedURL()+"/stats")
}

// ── Innovation 5: Digital Twin Election Simulation ────────────────────────────

// handleDigitalTwinSimulate runs an election scenario simulation.
func handleDigitalTwinSimulate(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, digitalTwinURL()+"/simulate")
}

// handleDigitalTwinScenarios returns available simulation scenarios.
func handleDigitalTwinScenarios(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, digitalTwinURL()+"/scenarios")
}

// handleDigitalTwinResults returns results of a completed simulation run.
func handleDigitalTwinResults(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, digitalTwinURL()+"/results")
}

// ── Innovation 6: Quantum-Resistant Cryptography ──────────────────────────────

// handleQuantumSign signs a document using post-quantum cryptography.
func handleQuantumSign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DocumentID string `json:"document_id"`
		Content    string `json:"content"`
		Algorithm  string `json:"algorithm"` // "dilithium3" | "falcon512" | "sphincs"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.DocumentID == "" || req.Content == "" {
		writeError(w, 400, "document_id and content are required")
		return
	}
	if req.Algorithm == "" {
		req.Algorithm = "dilithium3"
	}

	kp, err := GeneratePQKeyPair(PQSignatureScheme(req.Algorithm))
	if err != nil {
		log.Error().Err(err).Str("doc_id", req.DocumentID).Msg("quantum_keygen_failed")
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sig, err := signDocument(kp, []byte(req.Content))
	if err != nil {
		log.Error().Err(err).Str("doc_id", req.DocumentID).Msg("quantum_sign_failed")
		writeError(w, http.StatusInternalServerError, "ML-DSA-65 signing failed")
		return
	}
	auditWrite("quantum_document_signed", "document_id", req.DocumentID, r, nil)
	writeJSON(w, http.StatusOK, M{
		"document_id":   req.DocumentID,
		"algorithm":     kp.Scheme,
		"signature":     sig,
		"public_key":    kp.PublicKey,
		"public_key_id": pqKeyID(kp.PublicKey),
		"signed_at":     time.Now().UTC(),
	})
}

// handleQuantumVerify verifies a post-quantum signature.
func handleQuantumVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DocumentID string `json:"document_id"`
		Content    string `json:"content"`
		Signature  string `json:"signature"`
		PublicKey  string `json:"public_key"`
		Algorithm  string `json:"algorithm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}

	if _, err := normalizePQScheme(PQSignatureScheme(req.Algorithm)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	valid, err := verifyDocumentSignature([]byte(req.Content), req.Signature, req.PublicKey)
	if err != nil {
		log.Error().Err(err).Str("doc_id", req.DocumentID).Msg("quantum_verify_failed")
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, 200, M{"valid": valid, "document_id": req.DocumentID, "verified_at": time.Now().UTC()})
}

// handleQuantumKeyPair generates a new post-quantum key pair.
func handleQuantumKeyPair(w http.ResponseWriter, r *http.Request) {
	algorithm := r.URL.Query().Get("algorithm")
	if algorithm == "" {
		algorithm = "dilithium3"
	}
	pub, priv, err := GenerateQuantumKeyPair(algorithm)
	if err != nil {
		log.Error().Err(err).Msg("quantum_keygen_failed")
		writeError(w, 500, "key generation failed")
		return
	}
	auditWrite("quantum_keypair_generated", "algorithm", algorithm, r, nil)
	writeJSON(w, 200, M{
		"algorithm":    algorithm,
		"public_key":   pub,
		"private_key":  priv,
		"generated_at": time.Now().UTC(),
		"warning":      "Store the private key securely — it will not be shown again",
	})
}

// ── Innovation 7: Satellite Imagery Change Detection ──────────────────────────

// handleSatelliteAnalyze submits a polling unit to the configured real STAC analysis service.
func handleSatelliteAnalyze(w http.ResponseWriter, r *http.Request) {
	url := satelliteURL()
	if url == "" {
		writeError(w, http.StatusServiceUnavailable, "SATELLITE_SERVICE_URL is not configured")
		return
	}
	proxyToService(w, r, url+"/analyze")
}

// handleSatelliteStatus returns real STAC-service readiness.
func handleSatelliteStatus(w http.ResponseWriter, r *http.Request) {
	url := satelliteURL()
	if url == "" {
		writeError(w, http.StatusServiceUnavailable, "SATELLITE_SERVICE_URL is not configured")
		return
	}
	proxyToService(w, r, url+"/status")
}

// ── Innovation 8: Voice IVR Voter Assistance (extended) ───────────────────────

// IVR status and metrics require an approved telephony provider and persistent
// call records. No active-session or zero-count response is returned locally.
func handleIVRSessionStatus(w http.ResponseWriter, r *http.Request) {
	writeError(
		w,
		http.StatusServiceUnavailable,
		"IVR session status is unavailable until an approved telephony provider is configured",
	)
}

func handleIVRStats(w http.ResponseWriter, r *http.Request) {
	writeError(
		w,
		http.StatusServiceUnavailable,
		"IVR statistics are unavailable until an approved telephony provider is configured",
	)
}

// ── Innovation 9: Blockchain IPFS Audit Trail (extended) ─────────────────────

// handleIPFSAnchorEvent anchors a critical election event to IPFS + blockchain.
func handleIPFSAnchorEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EventType  string      `json:"event_type"`
		EventData  interface{} `json:"event_data"`
		ElectionID int         `json:"election_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.EventType == "" {
		writeError(w, 400, "event_type is required")
		return
	}

	record, err := AnchorToIPFS(req.EventType, req.EventData, req.ElectionID)
	if err != nil {
		log.Error().Err(err).Str("event_type", req.EventType).Msg("ipfs_anchor_unavailable")
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	auditWrite("ipfs_event_anchored", "event_type", req.EventType, r, nil)
	writeJSON(w, 201, record)
}

// handleIPFSAuditVerify verifies the integrity of an IPFS-anchored audit record.
func handleIPFSAuditVerify(w http.ResponseWriter, r *http.Request) {
	cid := r.URL.Query().Get("cid")
	if cid == "" {
		writeError(w, 400, "cid is required")
		return
	}
	result, err := VerifyIPFSRecord(cid)
	if err != nil {
		log.Error().Err(err).Str("cid", cid).Msg("ipfs_verify_unavailable")
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, 200, result)
}

// ── Innovation 10: Predictive Resource Allocation ─────────────────────────────

// handleResourceAllocationPredict returns AI-driven resource allocation recommendations.
func handleResourceAllocationPredict(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, predictiveAllocURL()+"/predict")
}

// handleResourceAllocationOptimize triggers a full resource optimization run.
func handleResourceAllocationOptimize(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, predictiveAllocURL()+"/optimize")
}

// handleResourceAllocationStatus returns the current allocation status.
func handleResourceAllocationStatus(w http.ResponseWriter, r *http.Request) {
	proxyToService(w, r, predictiveAllocURL()+"/status")
}

// ── Candidate Campaign Planning Module ────────────────────────────────────────
// Campaign plans, voter-targeting recommendations, polling analysis, competitor
// analysis, budget allocation, schedules, sentiment, and eligibility decisions
// all require authoritative data and an approved planning provider. The former
// local handlers emitted invented records and metrics, so every route now fails
// closed until that integration is deployed.
func campaignPlanningUnavailable(w http.ResponseWriter, r *http.Request) {
	writeError(
		w,
		http.StatusServiceUnavailable,
		"campaign planning is unavailable until authoritative data and an approved provider are configured",
	)
}

// ── Quantum ML-DSA-65 compatibility helpers ───────────────────────────────────

func QuantumSign(content []byte, algorithm string) (string, error) {
	kp, err := GeneratePQKeyPair(PQSignatureScheme(algorithm))
	if err != nil {
		return "", fmt.Errorf("ML-DSA-65 key generation failed: %w", err)
	}
	return signDocument(kp, content)
}

func QuantumVerify(content []byte, signature, publicKey, algorithm string) (bool, error) {
	if _, err := normalizePQScheme(PQSignatureScheme(algorithm)); err != nil {
		return false, err
	}
	return verifyDocumentSignature(content, signature, publicKey)
}

func GenerateQuantumKeyPair(algorithm string) (string, string, error) {
	kp, err := GeneratePQKeyPair(PQSignatureScheme(algorithm))
	if err != nil {
		return "", "", err
	}
	return kp.PublicKey, kp.PrivateKey, nil
}

// IPFS and EVM anchoring are intentionally unavailable until real external
// endpoints and credentials are configured. The platform must never fabricate
// a CID, transaction hash, block number, or verification result.
func AnchorToIPFS(eventType string, eventData interface{}, electionID int) (interface{}, error) {
	return nil, fmt.Errorf("IPFS and blockchain anchoring is not configured; set a real IPFS API and EVM anchor integration before invoking this endpoint")
}

func VerifyIPFSRecord(cid string) (interface{}, error) {
	if len(cid) < 10 {
		return nil, fmt.Errorf("invalid CID format")
	}
	return nil, fmt.Errorf("IPFS verification is not configured; no fabricated verification result is available")
}

// ── Environment helper (local to avoid conflict) ──────────────────────────────

var _ = os.Getenv // ensure os is used
