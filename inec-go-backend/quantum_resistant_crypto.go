package main

// quantum_resistant_crypto.go implements NIST FIPS 204 ML-DSA-65 signatures
// for election-result and document signing. It intentionally exposes no
// synthetic or hash-chain-only signature path.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/sha3"
)

// PQSignatureScheme identifies a supported post-quantum signature scheme.
type PQSignatureScheme string

const (
	// SchemeMLDSA65 is the FIPS 204 ML-DSA-65 parameter set.
	SchemeMLDSA65 PQSignatureScheme = "ML-DSA-65"
	// SchemeDilithium3 is retained as a compatibility alias for callers that
	// used the pre-standardized Dilithium3 name.
	SchemeDilithium3 PQSignatureScheme = SchemeMLDSA65
	// SchemeEd25519PQ is intentionally unsupported until a real hybrid scheme
	// is provisioned; callers receive a validation error rather than a fallback.
	SchemeEd25519PQ PQSignatureScheme = "Ed25519-PQ"
)

const (
	pqElectionContext = "inec-election-result-v1"
	pqDocumentContext = "inec-document-v1"
)

// PQKeyPair contains encoded ML-DSA-65 keys. PrivateKey must be stored in an
// HSM or equivalent secret store in production and is never serialized.
type PQKeyPair struct {
	Scheme     PQSignatureScheme `json:"scheme"`
	PublicKey  string            `json:"public_key"`
	PrivateKey string            `json:"-"`
	CreatedAt  time.Time         `json:"created_at"`
	ExpiresAt  time.Time         `json:"expires_at"`
}

// PQSignedResult is an election result signed with ML-DSA-65.
type PQSignedResult struct {
	ElectionID  string            `json:"election_id"`
	PollingUnit string            `json:"polling_unit"`
	ResultHash  string            `json:"result_hash"`
	Signature   string            `json:"signature"`
	Scheme      PQSignatureScheme `json:"scheme"`
	SignedAt    time.Time         `json:"signed_at"`
	PublicKeyID string            `json:"public_key_id"`
}

// SHAKE256Hash computes a SHAKE-256 extendable-output hash.
func SHAKE256Hash(data []byte, outputLen int) []byte {
	h := sha3.NewShake256()
	_, _ = h.Write(data)
	out := make([]byte, outputLen)
	_, _ = h.Read(out)
	return out
}

func normalizePQScheme(scheme PQSignatureScheme) (PQSignatureScheme, error) {
	switch strings.ToLower(strings.TrimSpace(string(scheme))) {
	case "", "ml-dsa-65", "mldsa65", "dilithium3":
		return SchemeMLDSA65, nil
	default:
		return "", fmt.Errorf("unsupported post-quantum signature scheme %q; only ML-DSA-65 is enabled", scheme)
	}
}

func pqElectionMessage(resultHash []byte, electionID, pollingUnit string, signedAt time.Time) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s", pqElectionContext, hex.EncodeToString(resultHash), electionID, pollingUnit+"\x00"+signedAt.UTC().Format(time.RFC3339Nano)))
}

func pqKeyID(publicKey string) string {
	return hex.EncodeToString(SHAKE256Hash([]byte(publicKey), 16))
}

func decodeMLDSAPrivateKey(encoded string) (*mldsa65.PrivateKey, error) {
	keyBytes, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode ML-DSA private key: %w", err)
	}
	key := new(mldsa65.PrivateKey)
	if err := key.UnmarshalBinary(keyBytes); err != nil {
		return nil, fmt.Errorf("unmarshal ML-DSA private key: %w", err)
	}
	return key, nil
}

func decodeMLDSAPublicKey(encoded string) (*mldsa65.PublicKey, error) {
	keyBytes, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode ML-DSA public key: %w", err)
	}
	key := new(mldsa65.PublicKey)
	if err := key.UnmarshalBinary(keyBytes); err != nil {
		return nil, fmt.Errorf("unmarshal ML-DSA public key: %w", err)
	}
	return key, nil
}

// GeneratePQKeyPair generates a genuine ML-DSA-65 key pair using crypto/rand.
func GeneratePQKeyPair(scheme PQSignatureScheme) (*PQKeyPair, error) {
	normalized, err := normalizePQScheme(scheme)
	if err != nil {
		return nil, err
	}
	publicKey, privateKey, err := mldsa65.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ML-DSA-65 keypair: %w", err)
	}
	kp := &PQKeyPair{
		Scheme:     normalized,
		PublicKey:  hex.EncodeToString(publicKey.Bytes()),
		PrivateKey: hex.EncodeToString(privateKey.Bytes()),
		CreatedAt:  time.Now().UTC(),
		ExpiresAt:  time.Now().UTC().Add(365 * 24 * time.Hour),
	}
	log.Info().Str("scheme", string(normalized)).Msg("ML-DSA-65 keypair generated")
	return kp, nil
}

// SignElectionResult signs canonical result metadata with ML-DSA-65.
func SignElectionResult(kp *PQKeyPair, electionID, pollingUnit string, resultData []byte) (*PQSignedResult, error) {
	if kp == nil {
		return nil, fmt.Errorf("post-quantum keypair is required")
	}
	if _, err := normalizePQScheme(kp.Scheme); err != nil {
		return nil, err
	}
	privateKey, err := decodeMLDSAPrivateKey(kp.PrivateKey)
	if err != nil {
		return nil, err
	}
	resultHash := SHAKE256Hash(resultData, 64)
	signedAt := time.Now().UTC()
	signature := make([]byte, mldsa65.SignatureSize)
	if err := mldsa65.SignTo(privateKey, pqElectionMessage(resultHash, electionID, pollingUnit, signedAt), []byte(pqElectionContext), true, signature); err != nil {
		return nil, fmt.Errorf("sign ML-DSA-65 election result: %w", err)
	}
	return &PQSignedResult{
		ElectionID:  electionID,
		PollingUnit: pollingUnit,
		ResultHash:  hex.EncodeToString(resultHash),
		Signature:   hex.EncodeToString(signature),
		Scheme:      SchemeMLDSA65,
		SignedAt:    signedAt,
		PublicKeyID: pqKeyID(kp.PublicKey),
	}, nil
}

// VerifyPQSignature cryptographically verifies an ML-DSA-65 election signature.
func VerifyPQSignature(signed *PQSignedResult, publicKey string, resultData []byte) bool {
	if signed == nil {
		return false
	}
	if _, err := normalizePQScheme(signed.Scheme); err != nil {
		log.Warn().Err(err).Msg("unsupported PQ signature scheme")
		return false
	}
	if signed.SignedAt.IsZero() {
		log.Warn().Msg("PQ signature has no signing timestamp")
		return false
	}
	resultHash := SHAKE256Hash(resultData, 64)
	if hex.EncodeToString(resultHash) != signed.ResultHash {
		log.Warn().Str("election", signed.ElectionID).Msg("PQ signature result hash mismatch")
		return false
	}
	if signed.PublicKeyID != "" && signed.PublicKeyID != pqKeyID(publicKey) {
		log.Warn().Str("election", signed.ElectionID).Msg("PQ signature public-key identifier mismatch")
		return false
	}
	key, err := decodeMLDSAPublicKey(publicKey)
	if err != nil {
		log.Warn().Err(err).Msg("PQ signature public-key decode failed")
		return false
	}
	signature, err := hex.DecodeString(signed.Signature)
	if err != nil || len(signature) != mldsa65.SignatureSize {
		log.Warn().Msg("PQ signature encoding or length invalid")
		return false
	}
	return mldsa65.Verify(key, pqElectionMessage(resultHash, signed.ElectionID, signed.PollingUnit, signed.SignedAt), []byte(pqElectionContext), signature)
}

func signDocument(kp *PQKeyPair, content []byte) (string, error) {
	if kp == nil {
		return "", fmt.Errorf("post-quantum keypair is required")
	}
	privateKey, err := decodeMLDSAPrivateKey(kp.PrivateKey)
	if err != nil {
		return "", err
	}
	signature := make([]byte, mldsa65.SignatureSize)
	if err := mldsa65.SignTo(privateKey, content, []byte(pqDocumentContext), true, signature); err != nil {
		return "", fmt.Errorf("sign ML-DSA-65 document: %w", err)
	}
	return hex.EncodeToString(signature), nil
}

func verifyDocumentSignature(content []byte, signature, publicKey string) (bool, error) {
	key, err := decodeMLDSAPublicKey(publicKey)
	if err != nil {
		return false, err
	}
	signatureBytes, err := hex.DecodeString(signature)
	if err != nil {
		return false, fmt.Errorf("decode ML-DSA signature: %w", err)
	}
	if len(signatureBytes) != mldsa65.SignatureSize {
		return false, fmt.Errorf("invalid ML-DSA-65 signature length")
	}
	return mldsa65.Verify(key, content, []byte(pqDocumentContext), signatureBytes), nil
}

var pqKeyStore = struct {
	sync.RWMutex
	keys map[string]*PQKeyPair
}{keys: make(map[string]*PQKeyPair)}

func storePQKey(kp *PQKeyPair) string {
	keyID := pqKeyID(kp.PublicKey)
	pqKeyStore.Lock()
	pqKeyStore.keys[keyID] = kp
	pqKeyStore.Unlock()
	return keyID
}

func loadPQKey(keyID string) (*PQKeyPair, bool) {
	pqKeyStore.RLock()
	kp, ok := pqKeyStore.keys[keyID]
	pqKeyStore.RUnlock()
	return kp, ok
}

// PQGenerateKeyHandler creates an in-memory keypair for the legacy result-signing API.
func PQGenerateKeyHandler(w http.ResponseWriter, r *http.Request) {
	kp, err := GeneratePQKeyPair(SchemeMLDSA65)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	keyID := storePQKey(kp)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"key_id":     keyID,
		"scheme":     kp.Scheme,
		"public_key": kp.PublicKey,
		"expires_at": kp.ExpiresAt,
	})
}

// PQSignResultHandler signs an election result using an in-memory ML-DSA-65 key.
func PQSignResultHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		KeyID       string `json:"key_id"`
		ElectionID  string `json:"election_id"`
		PollingUnit string `json:"polling_unit"`
		ResultData  string `json:"result_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.KeyID == "" || req.ElectionID == "" || req.PollingUnit == "" || req.ResultData == "" {
		http.Error(w, "key_id, election_id, polling_unit, and result_data are required", http.StatusBadRequest)
		return
	}
	kp, ok := loadPQKey(req.KeyID)
	if !ok {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}
	signed, err := SignElectionResult(kp, req.ElectionID, req.PollingUnit, []byte(req.ResultData))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(signed)
}
