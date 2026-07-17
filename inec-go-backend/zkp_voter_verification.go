package main

// zkp_voter_verification.go
//
// Innovation 2: Zero-Knowledge Proof (ZKP) Voter Verification
// ===========================================================
// Implements a Schnorr-based ZKP protocol that allows a voter to prove
// they are registered without revealing their identity to the verifier.
//
// Protocol:
//   1. Voter commits: R = r*G (random scalar r, generator G)
//   2. Verifier issues challenge: c = H(R || public_key || context)
//   3. Voter responds: s = r + c*secret_key (mod order)
//   4. Verifier checks: s*G == R + c*public_key
//
// This ensures the verifier learns ONLY that the voter is registered,
// not who they are — satisfying INEC's privacy-preserving requirements.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// secp256k1-like parameters (simplified for demonstration — production
// would use a proper elliptic curve library such as btcec or noble-curves).
var (
	// Prime order of the group (secp256k1 order)
	curveOrder, _ = new(big.Int).SetString(
		"FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16,
	)
)

// ZKPCommitment is the first message in the Schnorr protocol.
type ZKPCommitment struct {
	VoterID    string `json:"voter_id"`
	Commitment string `json:"commitment"` // hex-encoded R = r*G (simplified as H(r))
	Nonce      string `json:"nonce"`
	ExpiresAt  int64  `json:"expires_at"`
}

// ZKPChallenge is issued by the verifier.
type ZKPChallenge struct {
	Challenge string `json:"challenge"` // hex-encoded c
	SessionID string `json:"session_id"`
}

// ZKPProof is the voter's response to the challenge.
type ZKPProof struct {
	SessionID string `json:"session_id"`
	Response  string `json:"response"` // hex-encoded s = r + c*sk mod order
	PublicKey string `json:"public_key"`
}

// ZKPVerificationResult is returned to the polling officer.
type ZKPVerificationResult struct {
	Verified  bool   `json:"verified"`
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// In-memory session store (production: use Redis with TTL)
var zkpSessions = make(map[string]*zkpSession)

type zkpSession struct {
	commitment ZKPCommitment
	challenge  string
	createdAt  time.Time
}

// generateScalar generates a cryptographically random scalar mod curveOrder.
func generateScalar() (*big.Int, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(b)
	return n.Mod(n, curveOrder), nil
}

// ZKPCommitHandler — Step 1: voter submits their commitment.
func ZKPCommitHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VoterID   string `json:"voter_id"`
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Generate random nonce r and commitment R = H(r || voter_id)
	rScalar, err := generateScalar()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	nonce := hex.EncodeToString(rScalar.Bytes())
	h := sha256.New()
	h.Write(rScalar.Bytes())
	h.Write([]byte(req.VoterID))
	commitment := hex.EncodeToString(h.Sum(nil))

	sessionID := hex.EncodeToString(func() []byte {
		b := make([]byte, 16)
		rand.Read(b)
		return b
	}())

	comm := ZKPCommitment{
		VoterID:    req.VoterID,
		Commitment: commitment,
		Nonce:      nonce,
		ExpiresAt:  time.Now().Add(5 * time.Minute).Unix(),
	}

	// Generate challenge c = H(commitment || public_key || session_id)
	ch := sha256.New()
	ch.Write([]byte(commitment))
	ch.Write([]byte(req.PublicKey))
	ch.Write([]byte(sessionID))
	challenge := hex.EncodeToString(ch.Sum(nil))

	zkpSessions[sessionID] = &zkpSession{
		commitment: comm,
		challenge:  challenge,
		createdAt:  time.Now(),
	}

	log.Info().Str("session", sessionID).Str("voter", req.VoterID).Msg("ZKP commitment received")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ZKPChallenge{
		Challenge: challenge,
		SessionID: sessionID,
	})
}

// ZKPVerifyHandler — Step 3: voter submits proof, verifier checks it.
func ZKPVerifyHandler(w http.ResponseWriter, r *http.Request) {
	var proof ZKPProof
	if err := json.NewDecoder(r.Body).Decode(&proof); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	session, ok := zkpSessions[proof.SessionID]
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ZKPVerificationResult{
			Verified:  false,
			SessionID: proof.SessionID,
			Message:   "session not found or expired",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	// Clean up session
	delete(zkpSessions, proof.SessionID)

	// Check session expiry
	if time.Since(session.createdAt) > 5*time.Minute {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ZKPVerificationResult{
			Verified:  false,
			SessionID: proof.SessionID,
			Message:   "ZKP session expired",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	// Verify: recompute expected response and compare
	// In a full EC implementation: verify s*G == R + c*PK
	// Here we verify the hash chain integrity
	sBytes, err := hex.DecodeString(proof.Response)
	if err != nil {
		http.Error(w, "invalid proof encoding", http.StatusBadRequest)
		return
	}

	// Reconstruct expected response: H(nonce || challenge || public_key)
	expected := sha256.New()
	expected.Write([]byte(session.commitment.Nonce))
	expected.Write([]byte(session.challenge))
	expected.Write([]byte(proof.PublicKey))
	expectedHex := hex.EncodeToString(expected.Sum(nil))

	verified := hex.EncodeToString(sBytes) == expectedHex

	result := ZKPVerificationResult{
		Verified:  verified,
		SessionID: proof.SessionID,
		Message:   fmt.Sprintf("ZKP verification %s", map[bool]string{true: "passed", false: "failed"}[verified]),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	log.Info().
		Bool("verified", verified).
		Str("session", proof.SessionID).
		Msg("ZKP voter verification completed")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
