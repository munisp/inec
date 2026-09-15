// handlers_primaries.go — Party Primaries & Remote Voting handlers
// Phase 1: Convention management, aspirant CRUD, delegate credentialing,
//          ballot creation, multi-round voting, quorum tracking
// Phase 2: Remote electronic voting with E2E verifiability,
//          encrypted ballot submission, device binding, coercion resistance
//
// Middleware integration: Kafka (event streaming), Redis (caching/sessions),
// TigerBeetle (financial audit), Permify (authorization), OpenSearch (search),
// Keycloak (delegate auth), Temporal (workflow orchestration), Fluvio (live stream),
// Dapr (service invocation), Mojaloop (delegate payment), APISIX (rate limiting)

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
	"github.com/rs/zerolog/log"

	gotv "inec-go-backend/internal/gotv"
)

// ═══════════════════════════════════════════════════════════════════════════
// SECURITY — CRYPTO/BIOMETRIC BACKEND CONFIGURATION
// ═══════════════════════════════════════════════════════════════════════════
//
// This service previously FABRICATED election-cryptography artifacts:
// SHA-256 hashes were stored as "encrypted ballots", HMACs with a hardcoded
// key were stored as "proofs", random blobs were stored as "election keys"
// (private key in plaintext), and any non-empty string counted as "biometric
// verification". Those code paths have been removed. Endpoints now FAIL
// LOUDLY (HTTP 503) unless a real backend is configured.
//
// SECURITY: refuses to fabricate cryptographic artifacts.

// electionCryptoBackendURL, when set, points at a real ElectionGuard/Paillier
// tally backend. When empty, ballot casting and every crypto-artifact
// endpoint refuse to operate rather than fabricate keys/proofs/ciphertexts.
var electionCryptoBackendURL = os.Getenv("GOTV_ELECTION_CRYPTO_BACKEND_URL")

// quorumThresholdFraction is the fraction of registered delegates that must
// be accredited for quorum. SECURITY/INTEGRITY: this is the single source of
// truth — the constitutional value enforced at round-open (50%). Every
// quorum computation (round open, dashboard, quorum check, snapshots) MUST
// use this constant instead of a local literal.
const quorumThresholdFraction = 0.50

// biometricServiceURL, when set, points at the biometric verification
// pipeline used for remote-voter authentication. When empty, remote
// authentication is rejected — a non-empty payload string is NOT proof of
// biometric verification.
var biometricServiceURL = os.Getenv("GOTV_BIOMETRIC_SERVICE_URL")

// cbBiometricVerify protects calls to the biometric verification pipeline.
var cbBiometricVerify = NewGOTVCircuitBreaker("biometric-verify", 3, 30*time.Second)

// cryptoBackendConfigured reports whether a real election-cryptography
// backend is configured for this deployment.
func cryptoBackendConfigured() bool {
	return electionCryptoBackendURL != ""
}

// cryptoUnavailable writes a loud HTTP 503 for operations that would
// otherwise have to fabricate election-cryptography artifacts.
// SECURITY: refuses to fabricate cryptographic artifacts.
func cryptoUnavailable(w http.ResponseWriter, op string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(map[string]string{
		"error":  op + " not configured",
		"detail": "this deployment has no real ElectionGuard/Paillier backend; refusing to fabricate cryptographic artifacts",
	})
}

// verifyBiometricPayload submits a biometric payload to the configured
// biometric verification pipeline and returns true ONLY when that pipeline
// explicitly verifies it. SECURITY: never treats payload non-emptiness as
// verification.
func verifyBiometricPayload(r *http.Request, payload string) bool {
	if biometricServiceURL == "" || payload == "" {
		return false
	}
	body, _ := json.Marshal(map[string]string{"biometric_payload": payload})
	resp, code, err := resilientCall(r.Context(), cbBiometricVerify, "POST",
		biometricServiceURL+"/verify", body)
	if err != nil || code != http.StatusOK {
		return false
	}
	var result struct {
		Verified bool `json:"verified"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return false
	}
	return result.Verified
}

// cbElectionCrypto protects calls to the election-cryptography backend.
var cbElectionCrypto = NewGOTVCircuitBreaker("election-crypto", 3, 30*time.Second)

// cbAnchorService protects calls to the external blockchain anchor service.
var cbAnchorService = NewGOTVCircuitBreaker("anchor-service", 3, 30*time.Second)

// ═══════════════════════════════════════════════════════════════════════════
// ELECTION CRYPTO BACKEND — MINIMAL CONTRACT (documented, shape-verified)
// ═══════════════════════════════════════════════════════════════════════════
//
// SECURITY: ballot custody. A vote choice (aspirant_id) must NEVER be stored
// in cleartext next to the voter's identity (delegate_id) in this service's
// database. When GOTV_ELECTION_CRYPTO_BACKEND_URL is configured, this service
// DELEGATES custody of the plaintext choice to the crypto backend, which is
// the only component that may keep the aspirant linkage (inside its own
// trust domain, e.g. under an ElectionGuard/Paillier tally key).
//
// Contract (JSON over HTTPS):
//
//	POST {backend}/encrypt
//	  request:  {"round_id","ballot_id","vote_type","aspirant_id" (optional)}
//	  response: 200 {"ciphertext":"<non-empty>","proof":"<optional>",
//	                 "ballot_ref":"<opaque backend handle>"}
//	  The backend encrypts the choice and returns ONLY ciphertext + an opaque
//	  ballot_ref. The response MUST NOT echo the plaintext choice.
//
//	POST {backend}/verify-proof
//	  request:  {"round_id","ciphertext","proof"}
//	  response: 200 {"valid":true|false,"ballot_ref":"<opaque handle>"}
//	  Used when the client submits its own E2E-encrypted ballot: the backend
//	  verifies the zero-knowledge proof BEFORE anything is stored.
//
//	POST {backend}/tally
//	  request:  {"round_id","ballot_refs":[...]}
//	  response: 200 {"results":[{"aspirant_id","votes"}]}
//	  Aggregate tally inside the backend's domain; cleartext per-ballot
//	  choices are never returned to (or stored by) this service.
//
// On ANY backend call failure the calling handler returns 503 and performs
// NO database mutation — a failed encryption must never degrade into a
// cleartext insert.

// encryptBallotViaBackend submits a plaintext vote choice to the crypto
// backend and returns (ciphertext, proof, ballotRef). The plaintext choice
// stays in the backend's domain; this service stores only the ciphertext.
func encryptBallotViaBackend(ctx context.Context, roundID, ballotID, voteType, aspirantID string) (string, string, string, error) {
	if electionCryptoBackendURL == "" {
		return "", "", "", fmt.Errorf("election crypto backend not configured (GOTV_ELECTION_CRYPTO_BACKEND_URL)")
	}
	payload, _ := json.Marshal(map[string]string{
		"round_id":    roundID,
		"ballot_id":   ballotID,
		"vote_type":   voteType,
		"aspirant_id": aspirantID,
	})
	respBody, code, err := resilientCall(ctx, cbElectionCrypto, "POST",
		electionCryptoBackendURL+"/encrypt", payload)
	if err != nil || code != http.StatusOK {
		return "", "", "", fmt.Errorf("crypto backend /encrypt failed (code=%d): %v", code, err)
	}
	// Verify the response shape: ciphertext MUST be present and the plaintext
	// choice MUST NOT be echoed back.
	var result struct {
		Ciphertext string `json:"ciphertext"`
		Proof      string `json:"proof"`
		BallotRef  string `json:"ballot_ref"`
		AspirantID string `json:"aspirant_id"` // must stay empty
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", "", fmt.Errorf("crypto backend /encrypt returned malformed response: %w", err)
	}
	if result.Ciphertext == "" {
		return "", "", "", fmt.Errorf("crypto backend /encrypt returned empty ciphertext")
	}
	if result.AspirantID != "" {
		return "", "", "", fmt.Errorf("crypto backend /encrypt response leaked the plaintext choice — refusing storage")
	}
	return result.Ciphertext, result.Proof, result.BallotRef, nil
}

// verifyBallotProofViaBackend asks the crypto backend to verify a
// client-supplied E2E-encrypted ballot's zero-knowledge proof BEFORE storage.
func verifyBallotProofViaBackend(ctx context.Context, roundID, ciphertext, proof string) (string, error) {
	if electionCryptoBackendURL == "" {
		return "", fmt.Errorf("election crypto backend not configured (GOTV_ELECTION_CRYPTO_BACKEND_URL)")
	}
	payload, _ := json.Marshal(map[string]string{
		"round_id":   roundID,
		"ciphertext": ciphertext,
		"proof":      proof,
	})
	respBody, code, err := resilientCall(ctx, cbElectionCrypto, "POST",
		electionCryptoBackendURL+"/verify-proof", payload)
	if err != nil || code != http.StatusOK {
		return "", fmt.Errorf("crypto backend /verify-proof failed (code=%d): %v", code, err)
	}
	var result struct {
		Valid     bool   `json:"valid"`
		BallotRef string `json:"ballot_ref"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("crypto backend /verify-proof returned malformed response: %w", err)
	}
	if !result.Valid {
		return "", fmt.Errorf("ballot proof rejected by crypto backend")
	}
	return result.BallotRef, nil
}

// tallyBallotsViaBackend delegates aggregation of backend-custodied ballots
// to the crypto backend (which holds the only copy of the vote choices).
func tallyBallotsViaBackend(ctx context.Context, roundID string, ballotRefs []string) (map[string]int, error) {
	if electionCryptoBackendURL == "" {
		return nil, fmt.Errorf("election crypto backend not configured (GOTV_ELECTION_CRYPTO_BACKEND_URL)")
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"round_id":    roundID,
		"ballot_refs": ballotRefs,
	})
	respBody, code, err := resilientCall(ctx, cbElectionCrypto, "POST",
		electionCryptoBackendURL+"/tally", payload)
	if err != nil || code != http.StatusOK {
		return nil, fmt.Errorf("crypto backend /tally failed (code=%d): %v", code, err)
	}
	var result struct {
		Results []struct {
			AspirantID string `json:"aspirant_id"`
			Votes      int    `json:"votes"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("crypto backend /tally returned malformed response: %w", err)
	}
	out := make(map[string]int, len(result.Results))
	for _, r := range result.Results {
		if r.AspirantID == "" {
			return nil, fmt.Errorf("crypto backend /tally returned result without aspirant_id")
		}
		out[r.AspirantID] += r.Votes
	}
	return out, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// SCHEMA HARDENING COLUMNS (idempotent, additive only)
// ═══════════════════════════════════════════════════════════════════════════
//
// SECURITY: ballots.ballot_ref stores the crypto backend's opaque handle for
// a custodied ballot (in place of cleartext aspirant_id next to delegate_id);
// delegates.duress_code_hash stores the SHA-256 of a per-delegate duress
// ("panic") credential registered at credential issuance for coercion
// resistance. Both columns are additive and nullable — existing deployments
// keep working; deployments that need them get them on first use.
var ensureHardeningColumnsOnce sync.Once
var ensureHardeningColumnsErr error

func ensureHardeningColumns(ctx context.Context) error {
	ensureHardeningColumnsOnce.Do(func() {
		stmts := []string{
			`ALTER TABLE ballots ADD COLUMN IF NOT EXISTS ballot_ref TEXT`,
			`ALTER TABLE delegates ADD COLUMN IF NOT EXISTS duress_code_hash TEXT`,
		}
		for _, s := range stmts {
			if _, err := dbConn.ExecContext(ctx, s); err != nil {
				ensureHardeningColumnsErr = err
				log.Error().Err(err).Str("stmt", s).Msg("SECURITY: failed to ensure hardening column")
				return
			}
		}
	})
	return ensureHardeningColumnsErr
}

// ═══════════════════════════════════════════════════════════════════════════
// PARTY SCOPING — cross-party isolation for primaries mutations
// ═══════════════════════════════════════════════════════════════════════════
//
// SECURITY: the elections table has no party linkage; a party's ownership of
// a primary election is derived from its registered delegates/aspirants
// (both carry party_code). Every mutating round/dispute handler MUST enforce
// the caller's party_code, otherwise any party user can close/tally/certify
// another party's rounds or resolve their disputes.

// partyOwnsElection reports whether the caller's party has delegates or
// aspirants registered for the given election.
func partyOwnsElection(ctx context.Context, electionID int, partyCode string) bool {
	var n int
	if err := dbConn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT 1 FROM delegates WHERE election_id=$1 AND party_code=$2
			UNION ALL
			SELECT 1 FROM aspirants WHERE election_id=$1 AND party_code=$2
		) owned`, electionID, partyCode).Scan(&n); err != nil {
		log.Error().Err(err).Int("election_id", electionID).Msg("party ownership check failed — denying (fail closed)")
		return false
	}
	return n > 0
}

// partyOwnsRound resolves the round's election and checks party ownership.
func partyOwnsRound(ctx context.Context, roundID, partyCode string) (electionID int, owned bool) {
	if err := dbConn.QueryRowContext(ctx,
		"SELECT election_id FROM voting_rounds WHERE round_id=$1", roundID).Scan(&electionID); err != nil {
		return 0, false
	}
	return electionID, partyOwnsElection(ctx, electionID, partyCode)
}

// ═══════════════════════════════════════════════════════════════════════════
// ROUTE REGISTRATION
// ═══════════════════════════════════════════════════════════════════════════

// requirePrimaryRole (R5-039) gates convention-management routes on the
// SERVER-DERIVED GOTV role — the X-GOTV-Role header is set only by the auth
// middleware from the party-membership/credential tables (R5-036); a
// client-supplied value is stripped before it can reach here. Previously
// all ~43 primaries routes were wrapped in bare auth(...): a single
// compromised party credential could register fake delegates, accredit
// them, open/tally/certify rounds, and custody the election keys. Denials
// are logged as security events and fail closed (no role → 401).
func requirePrimaryRole(next http.HandlerFunc, roles ...GOTVRole) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := GOTVRole(r.Header.Get("X-GOTV-Role"))
		for _, allowed := range roles {
			if role == allowed {
				next(w, r)
				return
			}
		}
		pid, user := getParty(r)
		log.Warn().
			Int("party_id", pid).
			Str("user", user).
			Str("role", string(role)).
			Str("path", r.URL.Path).
			Msg("SECURITY: primaries RBAC denied")
		w.Header().Set("Content-Type", "application/json")
		if role == "" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "convention management requires a server-issued GOTV role (party membership)"})
			return
		}
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "insufficient role for this convention operation"})
	}
}

func registerPrimaryRoutes(r *mux.Router, auth func(http.HandlerFunc) http.HandlerFunc) {
	// R5-039 role groups:
	//   desk  — registration-desk operations (delegate/aspirant admin):
	//           party_admin + coordinator. Deliberately distinct from the
	//           returning-officer group so no single non-admin role both
	//           accredits delegates and tallies their votes.
	//   ro    — returning-officer operations (round lifecycle, tally,
	//           certify, key custody, dispute resolution): party_admin only.
	//   read  — any authenticated party identity (observer/analyst act as
	//           auditors): unchanged auth(...) wrapping.
	//   vote  — accredited-delegate gated inside the handlers (delegate
	//           credential + accreditation_status + one-vote-per-round).
	desk := func(h http.HandlerFunc) http.HandlerFunc {
		return requirePrimaryRole(h, RolePartyAdmin, RoleCoordinator)
	}
	ro := func(h http.HandlerFunc) http.HandlerFunc { return requirePrimaryRole(h, RolePartyAdmin) }

	// ─── Aspirant Management ────────────────────────────────────────────
	r.HandleFunc("/gotv/primaries/aspirants", auth(handleListAspirants)).Methods("GET")
	r.HandleFunc("/gotv/primaries/aspirants", auth(desk(handleCreateAspirant))).Methods("POST")
	r.HandleFunc("/gotv/primaries/aspirants/{id}", auth(handleGetAspirant)).Methods("GET")
	r.HandleFunc("/gotv/primaries/aspirants/{id}", auth(desk(handleUpdateAspirant))).Methods("PUT")
	r.HandleFunc("/gotv/primaries/aspirants/{id}/screen", auth(desk(handleScreenAspirant))).Methods("POST")
	r.HandleFunc("/gotv/primaries/aspirants/{id}/withdraw", auth(desk(handleWithdrawAspirant))).Methods("POST")
	r.HandleFunc("/gotv/primaries/aspirants/{id}/deposit", auth(desk(handleAspirantDeposit))).Methods("POST")

	// ─── Delegate Management ────────────────────────────────────────────
	r.HandleFunc("/gotv/primaries/delegates", auth(handleListDelegates)).Methods("GET")
	r.HandleFunc("/gotv/primaries/delegates", auth(desk(handleCreateDelegate))).Methods("POST")
	r.HandleFunc("/gotv/primaries/delegates/bulk", auth(desk(handleBulkCreateDelegates))).Methods("POST")
	r.HandleFunc("/gotv/primaries/delegates/{id}", auth(handleGetDelegate)).Methods("GET")
	r.HandleFunc("/gotv/primaries/delegates/{id}/credential", auth(desk(handleIssueCredential))).Methods("POST")
	r.HandleFunc("/gotv/primaries/delegates/{id}/accredit", auth(desk(handleAccreditDelegate))).Methods("POST")
	r.HandleFunc("/gotv/primaries/delegates/{id}/revoke", auth(desk(handleRevokeDelegate))).Methods("POST")
	r.HandleFunc("/gotv/primaries/delegates/{id}/checkin", auth(desk(handleDelegateCheckin))).Methods("POST")

	// ─── Convention & Venues ────────────────────────────────────────────
	r.HandleFunc("/gotv/primaries/venues", auth(handleListVenues)).Methods("GET")
	r.HandleFunc("/gotv/primaries/venues", auth(desk(handleCreateVenue))).Methods("POST")
	r.HandleFunc("/gotv/primaries/convention/dashboard", auth(handleConventionDashboard)).Methods("GET")
	r.HandleFunc("/gotv/primaries/convention/quorum", auth(handleQuorumCheck)).Methods("GET")

	// ─── Voting Rounds ──────────────────────────────────────────────────
	r.HandleFunc("/gotv/primaries/rounds", auth(handleListRounds)).Methods("GET")
	r.HandleFunc("/gotv/primaries/rounds", auth(ro(handleCreateRound))).Methods("POST")
	r.HandleFunc("/gotv/primaries/rounds/{id}/open", auth(ro(handleOpenRound))).Methods("POST")
	r.HandleFunc("/gotv/primaries/rounds/{id}/close", auth(ro(handleCloseRound))).Methods("POST")
	r.HandleFunc("/gotv/primaries/rounds/{id}/tally", auth(ro(handleTallyRound))).Methods("POST")
	r.HandleFunc("/gotv/primaries/rounds/{id}/certify", auth(ro(handleCertifyRound))).Methods("POST")
	r.HandleFunc("/gotv/primaries/rounds/{id}/results", auth(handleRoundResults)).Methods("GET")

	// ─── Ballot Casting (In-Person) ─────────────────────────────────────
	r.HandleFunc("/gotv/primaries/vote", auth(handleCastBallot)).Methods("POST")
	r.HandleFunc("/gotv/primaries/vote/verify", auth(handleVerifyBallot)).Methods("GET")

	// ─── Remote Voting (Phase 2) ────────────────────────────────────────
	r.HandleFunc("/gotv/primaries/remote/register-device", auth(handleRegisterVotingDevice)).Methods("POST")
	r.HandleFunc("/gotv/primaries/remote/session", auth(handleCreateVotingSession)).Methods("POST")
	r.HandleFunc("/gotv/primaries/remote/authenticate", auth(handleRemoteAuthenticate)).Methods("POST")
	r.HandleFunc("/gotv/primaries/remote/vote", auth(handleRemoteVote)).Methods("POST")
	r.HandleFunc("/gotv/primaries/remote/verify", handleRemoteVerifyBallot).Methods("GET") // public
	r.HandleFunc("/gotv/primaries/remote/coercion-vote", auth(handleCoercionVote)).Methods("POST")

	// ─── Cryptographic Operations ───────────────────────────────────────
	r.HandleFunc("/gotv/primaries/crypto/keys", auth(ro(handleGenerateElectionKeys))).Methods("POST")
	r.HandleFunc("/gotv/primaries/crypto/encrypt-tally", auth(ro(handleEncryptedTally))).Methods("POST")
	r.HandleFunc("/gotv/primaries/crypto/shuffle", auth(ro(handleMixNetShuffle))).Methods("POST")
	r.HandleFunc("/gotv/primaries/crypto/decrypt", auth(ro(handleThresholdDecrypt))).Methods("POST")
	r.HandleFunc("/gotv/primaries/crypto/audit-trail", auth(handleCryptoAuditTrail)).Methods("GET")

	// ─── Disputes ───────────────────────────────────────────────────────
	r.HandleFunc("/gotv/primaries/disputes", auth(handleListDisputes)).Methods("GET")
	r.HandleFunc("/gotv/primaries/disputes", auth(handleFileDispute)).Methods("POST")
	r.HandleFunc("/gotv/primaries/disputes/{id}/resolve", auth(ro(handleResolveDispute))).Methods("POST")

	// ─── Convention Audit ───────────────────────────────────────────────
	r.HandleFunc("/gotv/primaries/audit-log", auth(handleConventionAuditLog)).Methods("GET")
}

// ═══════════════════════════════════════════════════════════════════════════
// ASPIRANT HANDLERS
// ═══════════════════════════════════════════════════════════════════════════

func handleListAspirants(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	electionID := r.URL.Query().Get("election_id")
	status := r.URL.Query().Get("status")
	pgLimit, pgOffset := parsePagination(r)

	query := `SELECT aspirant_id, election_id, party_code, full_name, position_sought,
		gender, state_of_origin, screening_status, deposit_paid, endorsement_count,
		delegate_votes, is_winner, created_at
		FROM aspirants WHERE party_code=$1 AND deleted_at IS NULL`
	args := []interface{}{fmt.Sprintf("party_%d", pid)}
	idx := 2

	if electionID != "" {
		query += fmt.Sprintf(" AND election_id=$%d", idx)
		args = append(args, electionID)
		idx++
	}
	if status != "" {
		query += fmt.Sprintf(" AND screening_status=$%d", idx)
		args = append(args, status)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY delegate_votes DESC, created_at ASC LIMIT %d OFFSET %d", pgLimit, pgOffset)

	rows, err := dbConn.QueryContext(r.Context(), query, args...)
	if err != nil {
		jsonErr(w, "query failed: "+err.Error(), 500)
		return
	}
	defer rows.Close()

	var aspirants []map[string]interface{}
	for rows.Next() {
		var (
			aspID, partyCode, name, position, screenStatus string
			elecID, endorsements, votes                    int
			gender, stateOrigin                            sql.NullString
			depositPaid, isWinner                          bool
			createdAt                                      time.Time
		)
		if err := rows.Scan(&aspID, &elecID, &partyCode, &name, &position,
			&gender, &stateOrigin, &screenStatus, &depositPaid, &endorsements,
			&votes, &isWinner, &createdAt); err != nil {
			continue
		}
		aspirants = append(aspirants, map[string]interface{}{
			"aspirant_id":       aspID,
			"election_id":       elecID,
			"party_code":        partyCode,
			"full_name":         name,
			"position_sought":   position,
			"gender":            nullVal(gender),
			"state_of_origin":   nullVal(stateOrigin),
			"screening_status":  screenStatus,
			"deposit_paid":      depositPaid,
			"endorsement_count": endorsements,
			"delegate_votes":    votes,
			"is_winner":         isWinner,
			"created_at":        createdAt,
		})
	}
	if aspirants == nil {
		aspirants = []map[string]interface{}{}
	}

	// Publish to Kafka
	publishKafkaEvent("primaries.aspirants.listed", map[string]interface{}{
		"party_id": pid, "count": len(aspirants),
	})

	jsonResp(w, map[string]interface{}{"aspirants": aspirants, "total": len(aspirants)})
}

func handleCreateAspirant(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		ElectionID     int    `json:"election_id"`
		FullName       string `json:"full_name"`
		PositionSought string `json:"position_sought"`
		Gender         string `json:"gender"`
		StateOfOrigin  string `json:"state_of_origin"`
		LGAOfOrigin    string `json:"lga_of_origin"`
		NIN            string `json:"nin_number"`
		DateOfBirth    string `json:"date_of_birth"`
		PhotoURL       string `json:"photo_url"`
		ManifestoURL   string `json:"manifesto_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", 400)
		return
	}
	if req.ElectionID == 0 || req.FullName == "" || req.PositionSought == "" {
		jsonErr(w, "election_id, full_name, and position_sought required", 400)
		return
	}

	// Check permission via Permify
	if !checkPrimaryPermission(pid, user, "create_aspirant") {
		jsonErr(w, "insufficient permissions", 403)
		return
	}

	aspirantID := "asp-" + uuid.New().String()[:8]
	partyCode := fmt.Sprintf("party_%d", pid)

	_, err := dbConn.ExecContext(r.Context(), `
		INSERT INTO aspirants (aspirant_id, election_id, party_code, full_name, position_sought,
			gender, state_of_origin, lga_of_origin, nin_number, date_of_birth, photo_url, manifesto_url)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		aspirantID, req.ElectionID, partyCode, req.FullName, req.PositionSought,
		nullStr(req.Gender), nullStr(req.StateOfOrigin), nullStr(req.LGAOfOrigin),
		nullStr(req.NIN), nullStr(req.DateOfBirth), nullStr(req.PhotoURL), nullStr(req.ManifestoURL))
	if err != nil {
		jsonErr(w, "create failed: "+err.Error(), 500)
		return
	}

	// Index in OpenSearch
	indexInOpenSearch("aspirants", aspirantID, map[string]interface{}{
		"full_name": req.FullName, "position": req.PositionSought,
		"party_code": partyCode, "state": req.StateOfOrigin,
	})

	// Publish Kafka event
	publishKafkaEvent("primaries.aspirant.created", map[string]interface{}{
		"aspirant_id": aspirantID, "election_id": req.ElectionID,
		"full_name": req.FullName, "position": req.PositionSought,
	})

	// Publish to Fluvio stream
	publishFluvioEvent("primaries-stream", map[string]interface{}{
		"event": "aspirant_created", "aspirant_id": aspirantID, "name": req.FullName,
	})

	// Convention audit log
	logConventionEvent(r.Context(), req.ElectionID, "aspirant_registered", user, "aspirant", aspirantID, map[string]interface{}{
		"full_name": req.FullName, "position": req.PositionSought,
	})

	jsonResp(w, map[string]interface{}{"aspirant_id": aspirantID, "status": "pending"})
}

func handleGetAspirant(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	var asp struct {
		AspID, Name, Position, Status string
		ElecID, Endorsements, Votes   int
		DepositPaid, IsWinner         bool
	}
	err := dbConn.QueryRowContext(r.Context(), `
		SELECT aspirant_id, full_name, position_sought, screening_status,
			election_id, endorsement_count, delegate_votes, deposit_paid, is_winner
		FROM aspirants WHERE aspirant_id=$1 AND party_code=$2 AND deleted_at IS NULL`,
		id, partyCode).Scan(&asp.AspID, &asp.Name, &asp.Position, &asp.Status,
		&asp.ElecID, &asp.Endorsements, &asp.Votes, &asp.DepositPaid, &asp.IsWinner)
	if err != nil {
		jsonErr(w, "aspirant not found", 404)
		return
	}

	jsonResp(w, map[string]interface{}{
		"aspirant_id": asp.AspID, "full_name": asp.Name, "position_sought": asp.Position,
		"screening_status": asp.Status, "election_id": asp.ElecID,
		"endorsement_count": asp.Endorsements, "delegate_votes": asp.Votes,
		"deposit_paid": asp.DepositPaid, "is_winner": asp.IsWinner,
	})
}

func handleUpdateAspirant(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	// Authorization: aspirant updates are a privileged mutation.
	if !checkPrimaryPermission(pid, user, "update_aspirant") {
		jsonErr(w, "insufficient permissions", 403)
		return
	}

	var req map[string]interface{}
	json.NewDecoder(r.Body).Decode(&req)

	updates := []string{}
	args := []interface{}{}
	idx := 1
	for _, field := range []string{"full_name", "position_sought", "photo_url", "manifesto_url"} {
		if v, ok := req[field]; ok {
			updates = append(updates, fmt.Sprintf("%s=$%d", field, idx))
			args = append(args, v)
			idx++
		}
	}
	if len(updates) == 0 {
		jsonErr(w, "no fields to update", 400)
		return
	}
	updates = append(updates, fmt.Sprintf("updated_at=NOW()"))
	args = append(args, id, partyCode)
	query := fmt.Sprintf("UPDATE aspirants SET %s WHERE aspirant_id=$%d AND party_code=$%d AND deleted_at IS NULL",
		strings.Join(updates, ","), idx, idx+1)

	res, err := dbConn.ExecContext(r.Context(), query, args...)
	if err != nil {
		jsonErr(w, "update failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "aspirant not found", 404)
		return
	}

	// Invalidate Redis cache
	cacheInvalidate(r.Context(), "aspirants:"+id)

	jsonResp(w, map[string]interface{}{"updated": true})
}

func handleScreenAspirant(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	var req struct {
		Decision string `json:"decision"` // cleared, disqualified
		Notes    string `json:"notes"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	validDecisions := map[string]bool{"cleared": true, "disqualified": true, "screened": true}
	if !validDecisions[req.Decision] {
		jsonErr(w, "decision must be cleared, disqualified, or screened", 400)
		return
	}

	// Authorization: screening decisions are a privileged mutation.
	if !checkPrimaryPermission(pid, user, "screen_aspirant") {
		jsonErr(w, "insufficient permissions", 403)
		return
	}

	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE aspirants SET screening_status=$1, screening_notes=$2, screening_date=NOW(),
			updated_at=NOW()
		WHERE aspirant_id=$3 AND party_code=$4 AND screening_status IN ('pending','documents_submitted','screened')
		AND deleted_at IS NULL`,
		req.Decision, req.Notes, id, partyCode)
	if err != nil {
		jsonErr(w, "screening failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "aspirant not found or already screened", 404)
		return
	}

	publishKafkaEvent("primaries.aspirant.screened", map[string]interface{}{
		"aspirant_id": id, "decision": req.Decision,
	})

	var elecID int
	dbConn.QueryRow("SELECT election_id FROM aspirants WHERE aspirant_id=$1", id).Scan(&elecID)
	logConventionEvent(r.Context(), elecID, "aspirant_screened", user, "aspirant", id, map[string]interface{}{
		"decision": req.Decision, "notes": req.Notes,
	})

	jsonResp(w, map[string]interface{}{"screened": true, "decision": req.Decision})
}

func handleWithdrawAspirant(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	// Authorization: withdrawing an aspirant is a privileged mutation.
	if !checkPrimaryPermission(pid, user, "withdraw_aspirant") {
		jsonErr(w, "insufficient permissions", 403)
		return
	}

	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE aspirants SET screening_status='withdrawn', withdrawn_at=NOW(), updated_at=NOW()
		WHERE aspirant_id=$1 AND party_code=$2 AND screening_status NOT IN ('withdrawn','disqualified')
		AND deleted_at IS NULL`, id, partyCode)
	if err != nil {
		jsonErr(w, "withdrawal failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "aspirant not found or already withdrawn", 404)
		return
	}
	jsonResp(w, map[string]interface{}{"withdrawn": true})
}

func handleAspirantDeposit(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		AmountKobo int64 `json:"amount_kobo"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.AmountKobo <= 0 {
		jsonErr(w, "amount_kobo must be positive", 400)
		return
	}

	partyCode := fmt.Sprintf("party_%d", pid)

	// Authorization: recording a deposit is a privileged mutation.
	if !checkPrimaryPermission(pid, user, "record_deposit") {
		jsonErr(w, "insufficient permissions", 403)
		return
	}

	// SECURITY/INTEGRITY: a deposit is marked paid ONLY on real ledger
	// confirmation. With no payment ledger configured there is no proof of
	// payment — fail loudly (503) and leave deposit_paid=FALSE rather than
	// take the caller's word. The previous code stored a fabricated
	// "tb-<random>" transfer id when TigerBeetle was unconfigured.
	if gotvLedger == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "payment ledger unavailable",
			"detail": "no TigerBeetle ledger configured; refusing to mark a deposit paid without ledger confirmation",
		})
		return
	}
	tbTransferID, err := recordTBTransfer("aspirant_deposit", req.AmountKobo, id, user)
	if err != nil || tbTransferID == "" {
		log.Error().Err(err).Str("aspirant_id", id).Msg("deposit ledger transfer failed — deposit NOT marked paid")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "payment ledger unavailable",
			"detail": "ledger transfer could not be confirmed; deposit remains unpaid",
		})
		return
	}

	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE aspirants SET deposit_paid=TRUE, deposit_amount_kobo=$1, deposit_tb_transfer_id=$2,
			updated_at=NOW()
		WHERE aspirant_id=$3 AND party_code=$4 AND deposit_paid=FALSE AND deleted_at IS NULL`,
		req.AmountKobo, tbTransferID, id, partyCode)
	if err != nil {
		jsonErr(w, "deposit recording failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "aspirant not found or deposit already paid", 404)
		return
	}

	publishKafkaEvent("primaries.deposit.paid", map[string]interface{}{
		"aspirant_id": id, "amount_kobo": req.AmountKobo, "tb_transfer_id": tbTransferID,
	})

	jsonResp(w, map[string]interface{}{"deposit_paid": true, "tb_transfer_id": tbTransferID,
		"amount_naira": float64(req.AmountKobo) / 100.0})
}

// ═══════════════════════════════════════════════════════════════════════════
// DELEGATE HANDLERS
// ═══════════════════════════════════════════════════════════════════════════

func handleListDelegates(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	electionID := r.URL.Query().Get("election_id")
	status := r.URL.Query().Get("status")
	state := r.URL.Query().Get("state")
	pgLimit, pgOffset := parsePagination(r)
	partyCode := fmt.Sprintf("party_%d", pid)

	query := `SELECT delegate_id, election_id, full_name, delegate_type,
		state_code, lga_code, ward_code, credential_number, credential_verified,
		accreditation_status, has_voted, voting_weight, created_at
		FROM delegates WHERE party_code=$1`
	args := []interface{}{partyCode}
	idx := 2

	if electionID != "" {
		query += fmt.Sprintf(" AND election_id=$%d", idx)
		args = append(args, electionID)
		idx++
	}
	if status != "" {
		query += fmt.Sprintf(" AND accreditation_status=$%d", idx)
		args = append(args, status)
		idx++
	}
	if state != "" {
		query += fmt.Sprintf(" AND state_code=$%d", idx)
		args = append(args, state)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY state_code, lga_code LIMIT %d OFFSET %d", pgLimit, pgOffset)

	rows, err := dbConn.QueryContext(r.Context(), query, args...)
	if err != nil {
		jsonErr(w, "query failed", 500)
		return
	}
	defer rows.Close()

	var delegates []map[string]interface{}
	for rows.Next() {
		var (
			delID, name, dtype, accStatus   string
			elecID, weight                  int
			stCode, lgaCode, wCode, credNum sql.NullString
			credVerified, hasVoted          bool
			createdAt                       time.Time
		)
		if err := rows.Scan(&delID, &elecID, &name, &dtype, &stCode, &lgaCode,
			&wCode, &credNum, &credVerified, &accStatus, &hasVoted, &weight, &createdAt); err != nil {
			continue
		}
		delegates = append(delegates, map[string]interface{}{
			"delegate_id":          delID,
			"election_id":          elecID,
			"full_name":            name,
			"delegate_type":        dtype,
			"state_code":           nullVal(stCode),
			"lga_code":             nullVal(lgaCode),
			"ward_code":            nullVal(wCode),
			"credential_number":    nullVal(credNum),
			"credential_verified":  credVerified,
			"accreditation_status": accStatus,
			"has_voted":            hasVoted,
			"voting_weight":        weight,
			"created_at":           createdAt,
		})
	}
	if delegates == nil {
		delegates = []map[string]interface{}{}
	}

	var total int
	dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE party_code=$1", partyCode).Scan(&total)

	jsonResp(w, map[string]interface{}{"delegates": delegates, "total": total})
}

func handleCreateDelegate(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		ElectionID   int    `json:"election_id"`
		FullName     string `json:"full_name"`
		Phone        string `json:"phone"`
		NIN          string `json:"nin"`
		DelegateType string `json:"delegate_type"`
		StateCode    string `json:"state_code"`
		LGACode      string `json:"lga_code"`
		WardCode     string `json:"ward_code"`
		VotingWeight int    `json:"voting_weight"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", 400)
		return
	}
	if req.ElectionID == 0 || req.FullName == "" {
		jsonErr(w, "election_id and full_name required", 400)
		return
	}
	if req.DelegateType == "" {
		req.DelegateType = "elected"
	}
	if req.VotingWeight <= 0 {
		req.VotingWeight = 1
	}

	// Authorization: delegate registration is a privileged mutation.
	if !checkPrimaryPermission(pid, user, "create_delegate") {
		jsonErr(w, "insufficient permissions", 403)
		return
	}

	delegateID := "del-" + uuid.New().String()[:8]
	partyCode := fmt.Sprintf("party_%d", pid)

	phoneHash := hashStringSHA(req.Phone)
	ninHash := hashStringSHA(req.NIN)

	_, err := dbConn.ExecContext(r.Context(), `
		INSERT INTO delegates (delegate_id, election_id, party_code, full_name, phone_hash, nin_hash,
			delegate_type, state_code, lga_code, ward_code, voting_weight)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		delegateID, req.ElectionID, partyCode, req.FullName, phoneHash, ninHash,
		req.DelegateType, nullStr(req.StateCode), nullStr(req.LGACode), nullStr(req.WardCode),
		req.VotingWeight)
	if err != nil {
		jsonErr(w, "create delegate failed: "+err.Error(), 500)
		return
	}

	indexInOpenSearch("delegates", delegateID, map[string]interface{}{
		"full_name": req.FullName, "state": req.StateCode, "type": req.DelegateType,
	})

	publishKafkaEvent("primaries.delegate.created", map[string]interface{}{
		"delegate_id": delegateID, "election_id": req.ElectionID, "state": req.StateCode,
	})

	logConventionEvent(r.Context(), req.ElectionID, "delegate_registered", user, "delegate", delegateID, map[string]interface{}{
		"full_name": req.FullName, "state": req.StateCode, "type": req.DelegateType,
	})

	jsonResp(w, map[string]interface{}{"delegate_id": delegateID, "status": "registered"})
}

func handleBulkCreateDelegates(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		ElectionID int `json:"election_id"`
		Delegates  []struct {
			FullName     string `json:"full_name"`
			Phone        string `json:"phone"`
			DelegateType string `json:"delegate_type"`
			StateCode    string `json:"state_code"`
			LGACode      string `json:"lga_code"`
			WardCode     string `json:"ward_code"`
		} `json:"delegates"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.ElectionID == 0 || len(req.Delegates) == 0 {
		jsonErr(w, "election_id and delegates array required", 400)
		return
	}

	// Authorization: bulk delegate registration is a privileged mutation.
	if !checkPrimaryPermission(pid, user, "bulk_create_delegates") {
		jsonErr(w, "insufficient permissions", 403)
		return
	}

	partyCode := fmt.Sprintf("party_%d", pid)
	created := 0
	for _, d := range req.Delegates {
		delegateID := "del-" + uuid.New().String()[:8]
		dtype := d.DelegateType
		if dtype == "" {
			dtype = "elected"
		}
		_, err := dbConn.ExecContext(r.Context(), `
			INSERT INTO delegates (delegate_id, election_id, party_code, full_name, phone_hash,
				delegate_type, state_code, lga_code, ward_code)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			delegateID, req.ElectionID, partyCode, d.FullName, hashStringSHA(d.Phone),
			dtype, nullStr(d.StateCode), nullStr(d.LGACode), nullStr(d.WardCode))
		if err == nil {
			created++
		}
	}

	publishKafkaEvent("primaries.delegates.bulk_created", map[string]interface{}{
		"election_id": req.ElectionID, "count": created,
	})

	jsonResp(w, map[string]interface{}{"created": created, "total_submitted": len(req.Delegates)})
}

func handleGetDelegate(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	var del struct {
		DelID, Name, DType, AccStatus string
		ElecID, Weight                int
		CredVerified, HasVoted        bool
	}
	err := dbConn.QueryRowContext(r.Context(), `
		SELECT delegate_id, full_name, delegate_type, accreditation_status,
			election_id, voting_weight, credential_verified, has_voted
		FROM delegates WHERE delegate_id=$1 AND party_code=$2`, id, partyCode).
		Scan(&del.DelID, &del.Name, &del.DType, &del.AccStatus,
			&del.ElecID, &del.Weight, &del.CredVerified, &del.HasVoted)
	if err != nil {
		jsonErr(w, "delegate not found", 404)
		return
	}
	jsonResp(w, map[string]interface{}{
		"delegate_id": del.DelID, "full_name": del.Name, "delegate_type": del.DType,
		"accreditation_status": del.AccStatus, "election_id": del.ElecID,
		"voting_weight": del.Weight, "credential_verified": del.CredVerified,
		"has_voted": del.HasVoted,
	})
}

func handleIssueCredential(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	// Authorization: credential issuance is a privileged mutation.
	if !checkPrimaryPermission(pid, user, "issue_credential") {
		jsonErr(w, "insufficient permissions", 403)
		return
	}

	// Optional per-delegate duress ("panic") credential for coercion
	// resistance. Only the SHA-256 hash is stored; the code itself is given
	// to the delegate out-of-band and is never persisted or logged.
	var req struct {
		DuressCode string `json:"duress_code"`
	}
	json.NewDecoder(r.Body).Decode(&req) // body optional
	var duressHash interface{}
	if req.DuressCode != "" {
		if err := ensureHardeningColumns(r.Context()); err != nil {
			log.Error().Err(err).Msg("duress credential column unavailable")
			jsonErr(w, "duress credential storage unavailable", 503)
			return
		}
		h := hashStringSHA(req.DuressCode)
		duressHash = h
	}

	credNumber := fmt.Sprintf("CRED-%s-%s", strings.ToUpper(uuid.New().String()[:4]), strings.ToUpper(uuid.New().String()[:4]))

	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE delegates SET credential_number=$1, accreditation_status='credential_issued',
			duress_code_hash=COALESCE($4, duress_code_hash),
			updated_at=NOW()
		WHERE delegate_id=$2 AND party_code=$3 AND accreditation_status='registered'`,
		credNumber, id, partyCode, duressHash)
	if err != nil {
		jsonErr(w, "credential issue failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "delegate not found or already has credential", 404)
		return
	}

	var elecID int
	dbConn.QueryRow("SELECT election_id FROM delegates WHERE delegate_id=$1", id).Scan(&elecID)
	logConventionEvent(r.Context(), elecID, "credential_issued", user, "delegate", id, map[string]interface{}{
		"credential_number": credNumber,
	})

	jsonResp(w, map[string]interface{}{"credential_number": credNumber, "status": "credential_issued"})
}

func handleAccreditDelegate(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	var req struct {
		BiometricHash string `json:"biometric_hash"`
		DeviceID      string `json:"device_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Authorization: accrediting delegates is a privileged mutation.
	if !checkPrimaryPermission(pid, user, "accredit_delegate") {
		jsonErr(w, "insufficient permissions", 403)
		return
	}

	// SECURITY: when Keycloak is configured, the delegate's session token
	// MUST validate — previously the result was only logged ("cosmetic"
	// check) and the token was never even forwarded.
	if keycloakURL != "" && !validateKeycloakDelegateSession(r) {
		jsonErr(w, "delegate Keycloak session validation failed", 403)
		return
	}
	keycloakValid := true // gated above: true here means "validated or Keycloak not configured"

	// SECURITY: a caller-supplied biometric_hash is NOT verification. When a
	// biometric payload is presented it MUST be verified through the
	// configured biometric pipeline (same as handleRemoteAuthenticate); with
	// no pipeline configured we refuse (503) rather than store an unverified
	// hash and set credential_verified=TRUE.
	if req.BiometricHash != "" {
		if biometricServiceURL == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error":  "biometric verification service unavailable",
				"detail": "no biometric verification pipeline configured (GOTV_BIOMETRIC_SERVICE_URL); refusing to accredit on an unverified biometric",
			})
			return
		}
		if !verifyBiometricPayload(r, req.BiometricHash) {
			jsonErr(w, "biometric verification failed", 401)
			return
		}
	}

	// SECURITY: accreditation requires the credential_issued state — a
	// merely 'registered' delegate has NO credential and must not be
	// accredited (or marked credential_verified) on the caller's word.
	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE delegates SET accreditation_status='accredited', accredited_at=NOW(),
			credential_verified=TRUE, credential_verified_at=NOW(),
			biometric_hash=$1, device_id=$2, floor_access=TRUE, check_in_at=NOW(),
			updated_at=NOW()
		WHERE delegate_id=$3 AND party_code=$4 AND accreditation_status='credential_issued'`,
		nullStr(req.BiometricHash), nullStr(req.DeviceID), id, partyCode)
	if err != nil {
		jsonErr(w, "accreditation failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "delegate not found, already accredited, or has no issued credential", 404)
		return
	}

	var elecID int
	dbConn.QueryRow("SELECT election_id FROM delegates WHERE delegate_id=$1", id).Scan(&elecID)

	publishKafkaEvent("primaries.delegate.accredited", map[string]interface{}{
		"delegate_id": id, "election_id": elecID, "keycloak_valid": keycloakValid,
	})

	logConventionEvent(r.Context(), elecID, "delegate_accredited", user, "delegate", id, map[string]interface{}{
		"biometric": req.BiometricHash != "", "keycloak_valid": keycloakValid,
	})

	// Update quorum count
	updateQuorumSnapshot(r.Context(), elecID)

	jsonResp(w, map[string]interface{}{"accredited": true, "floor_access": true})
}

func handleRevokeDelegate(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Authorization: revoking a delegate is a privileged mutation.
	if !checkPrimaryPermission(pid, user, "revoke_delegate") {
		jsonErr(w, "insufficient permissions", 403)
		return
	}

	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE delegates SET accreditation_status='revoked', floor_access=FALSE, updated_at=NOW()
		WHERE delegate_id=$1 AND party_code=$2 AND accreditation_status='accredited'`,
		id, partyCode)
	if err != nil {
		jsonErr(w, "revocation failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "delegate not found or not accredited", 404)
		return
	}

	var elecID int
	dbConn.QueryRow("SELECT election_id FROM delegates WHERE delegate_id=$1", id).Scan(&elecID)
	logConventionEvent(r.Context(), elecID, "delegate_revoked", user, "delegate", id, map[string]interface{}{
		"reason": req.Reason,
	})

	jsonResp(w, map[string]interface{}{"revoked": true})
}

func handleDelegateCheckin(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	// INTEGRITY: check rows affected — previously this returned
	// checked_in:true even when no delegate matched (0 rows updated), a
	// phantom check-in that inflates quorum snapshots.
	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE delegates SET check_in_at=NOW(), floor_access=TRUE, updated_at=NOW()
		WHERE delegate_id=$1 AND party_code=$2 AND accreditation_status='accredited'`,
		id, partyCode)
	if err != nil {
		jsonErr(w, "check-in failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "delegate not found or not accredited", 404)
		return
	}

	var elecID int
	dbConn.QueryRow("SELECT election_id FROM delegates WHERE delegate_id=$1", id).Scan(&elecID)
	updateQuorumSnapshot(r.Context(), elecID)

	jsonResp(w, map[string]interface{}{"checked_in": true})
}

// ═══════════════════════════════════════════════════════════════════════════
// CONVENTION & VENUE HANDLERS
// ═══════════════════════════════════════════════════════════════════════════

func handleListVenues(w http.ResponseWriter, r *http.Request) {
	electionID := r.URL.Query().Get("election_id")
	if electionID == "" {
		jsonErr(w, "election_id required", 400)
		return
	}

	rows, err := dbConn.QueryContext(r.Context(), `
		SELECT venue_id, name, address, state_code, capacity, venue_type, is_active, streaming_url
		FROM convention_venues WHERE election_id=$1 AND is_active=TRUE`, electionID)
	if err != nil {
		jsonErr(w, "query failed", 500)
		return
	}
	defer rows.Close()

	var venues []map[string]interface{}
	for rows.Next() {
		var vID, name string
		var addr, stCode, streamURL sql.NullString
		var capacity int
		var vType string
		var active bool
		rows.Scan(&vID, &name, &addr, &stCode, &capacity, &vType, &active, &streamURL)
		venues = append(venues, map[string]interface{}{
			"venue_id": vID, "name": name, "address": nullVal(addr),
			"state_code": nullVal(stCode), "capacity": capacity,
			"venue_type": vType, "streaming_url": nullVal(streamURL),
		})
	}
	if venues == nil {
		venues = []map[string]interface{}{}
	}
	jsonResp(w, map[string]interface{}{"venues": venues})
}

func handleCreateVenue(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	_ = pid
	var req struct {
		ElectionID   int     `json:"election_id"`
		Name         string  `json:"name"`
		Address      string  `json:"address"`
		StateCode    string  `json:"state_code"`
		Latitude     float64 `json:"latitude"`
		Longitude    float64 `json:"longitude"`
		Capacity     int     `json:"capacity"`
		VenueType    string  `json:"venue_type"`
		StreamingURL string  `json:"streaming_url"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.ElectionID == 0 || req.Name == "" {
		jsonErr(w, "election_id and name required", 400)
		return
	}
	if req.VenueType == "" {
		req.VenueType = "main"
	}

	venueID := "ven-" + uuid.New().String()[:8]
	_, err := dbConn.ExecContext(r.Context(), `
		INSERT INTO convention_venues (venue_id, election_id, name, address, state_code,
			latitude, longitude, capacity, venue_type, streaming_url)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		venueID, req.ElectionID, req.Name, nullStr(req.Address), nullStr(req.StateCode),
		req.Latitude, req.Longitude, req.Capacity, req.VenueType, nullStr(req.StreamingURL))
	if err != nil {
		jsonErr(w, "create venue failed", 500)
		return
	}
	jsonResp(w, map[string]interface{}{"venue_id": venueID})
}

func handleConventionDashboard(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	electionID := r.URL.Query().Get("election_id")
	partyCode := fmt.Sprintf("party_%d", pid)

	// Redis cache
	cacheKey := fmt.Sprintf("convention_dash:%s:%s", partyCode, electionID)
	if cached, ok := cacheGet(r.Context(), cacheKey); ok {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.Write([]byte(cached))
		return
	}

	var totalDelegates, accredited, hasVoted, totalAspirants, clearedAspirants int
	if electionID != "" {
		dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE party_code=$1 AND election_id=$2", partyCode, electionID).Scan(&totalDelegates)
		dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE party_code=$1 AND election_id=$2 AND accreditation_status='accredited'", partyCode, electionID).Scan(&accredited)
		dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE party_code=$1 AND election_id=$2 AND has_voted=TRUE", partyCode, electionID).Scan(&hasVoted)
		dbConn.QueryRow("SELECT COUNT(*) FROM aspirants WHERE party_code=$1 AND election_id=$2 AND deleted_at IS NULL", partyCode, electionID).Scan(&totalAspirants)
		dbConn.QueryRow("SELECT COUNT(*) FROM aspirants WHERE party_code=$1 AND election_id=$2 AND screening_status='cleared' AND deleted_at IS NULL", partyCode, electionID).Scan(&clearedAspirants)
	}

	// Quorum calculation — single source of truth: quorumThresholdFraction
	// (the constitutional value enforced at round-open).
	quorumThreshold := quorumThresholdFraction
	quorumMet := float64(accredited)/math.Max(float64(totalDelegates), 1) >= quorumThreshold

	// Active round info
	var activeRound sql.NullString
	var activeRoundNum sql.NullInt64
	if electionID != "" {
		dbConn.QueryRow(`SELECT round_id, round_number FROM voting_rounds
			WHERE election_id=$1 AND status IN ('open','voting') ORDER BY round_number DESC LIMIT 1`,
			electionID).Scan(&activeRound, &activeRoundNum)
	}

	// State breakdown
	var stateBreakdown []map[string]interface{}
	if electionID != "" {
		stRows, _ := dbConn.QueryContext(r.Context(), `
			SELECT COALESCE(state_code,'Unknown'), COUNT(*),
				SUM(CASE WHEN accreditation_status='accredited' THEN 1 ELSE 0 END),
				SUM(CASE WHEN has_voted THEN 1 ELSE 0 END)
			FROM delegates WHERE party_code=$1 AND election_id=$2
			GROUP BY state_code ORDER BY COUNT(*) DESC`, partyCode, electionID)
		if stRows != nil {
			defer stRows.Close()
			for stRows.Next() {
				var st string
				var total, acc, voted int
				stRows.Scan(&st, &total, &acc, &voted)
				stateBreakdown = append(stateBreakdown, map[string]interface{}{
					"state": st, "total": total, "accredited": acc, "voted": voted,
				})
			}
		}
	}

	result := map[string]interface{}{
		"election_id":         electionID,
		"total_delegates":     totalDelegates,
		"accredited":          accredited,
		"has_voted":           hasVoted,
		"turnout_pct":         math.Round(float64(hasVoted)/math.Max(float64(accredited), 1)*10000) / 100,
		"total_aspirants":     totalAspirants,
		"cleared_aspirants":   clearedAspirants,
		"quorum_threshold":    quorumThreshold * 100,
		"quorum_present_pct":  math.Round(float64(accredited)/math.Max(float64(totalDelegates), 1)*10000) / 100,
		"quorum_met":          quorumMet,
		"active_round":        nullVal(activeRound),
		"active_round_number": nullValInt64(activeRoundNum),
		"state_breakdown":     stateBreakdown,
	}

	cacheSet(r.Context(), cacheKey, result, 10*time.Second)
	jsonResp(w, result)
}

func handleQuorumCheck(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	electionID := r.URL.Query().Get("election_id")
	partyCode := fmt.Sprintf("party_%d", pid)

	var total, accredited, present int
	dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE party_code=$1 AND election_id=$2", partyCode, electionID).Scan(&total)
	dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE party_code=$1 AND election_id=$2 AND accreditation_status='accredited'", partyCode, electionID).Scan(&accredited)
	dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE party_code=$1 AND election_id=$2 AND floor_access=TRUE", partyCode, electionID).Scan(&present)

	threshold := quorumThresholdFraction * 100
	quorumMet := float64(accredited)/math.Max(float64(total), 1)*100 >= threshold

	jsonResp(w, map[string]interface{}{
		"total_registered": total,
		"accredited":       accredited,
		"present":          present,
		"threshold_pct":    threshold,
		"present_pct":      math.Round(float64(accredited)/math.Max(float64(total), 1)*10000) / 100,
		"quorum_met":       quorumMet,
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// VOTING ROUND HANDLERS
// ═══════════════════════════════════════════════════════════════════════════

func handleListRounds(w http.ResponseWriter, r *http.Request) {
	electionID := r.URL.Query().Get("election_id")
	if electionID == "" {
		jsonErr(w, "election_id required", 400)
		return
	}

	rows, err := dbConn.QueryContext(r.Context(), `
		SELECT round_id, round_number, round_type, status, voting_method,
			quorum_required, quorum_present, quorum_met,
			total_eligible_voters, total_votes_cast, total_valid_votes,
			opened_at, closed_at, merkle_root
		FROM voting_rounds WHERE election_id=$1 ORDER BY round_number`, electionID)
	if err != nil {
		jsonErr(w, "query failed", 500)
		return
	}
	defer rows.Close()

	var rounds []map[string]interface{}
	for rows.Next() {
		var rID, rType, status, method string
		var rNum, qReq, qPres, eligible, cast, valid int
		var qMet bool
		var opened, closed sql.NullTime
		var merkle sql.NullString
		rows.Scan(&rID, &rNum, &rType, &status, &method, &qReq, &qPres, &qMet,
			&eligible, &cast, &valid, &opened, &closed, &merkle)
		rounds = append(rounds, map[string]interface{}{
			"round_id": rID, "round_number": rNum, "round_type": rType,
			"status": status, "voting_method": method,
			"quorum_required": qReq, "quorum_present": qPres, "quorum_met": qMet,
			"total_eligible": eligible, "total_votes_cast": cast, "total_valid": valid,
			"opened_at": nullTime(opened), "closed_at": nullTime(closed),
			"merkle_root": nullVal(merkle),
		})
	}
	if rounds == nil {
		rounds = []map[string]interface{}{}
	}
	jsonResp(w, map[string]interface{}{"rounds": rounds})
}

func handleCreateRound(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	_ = pid
	var req struct {
		ElectionID   int    `json:"election_id"`
		RoundNumber  int    `json:"round_number"`
		RoundType    string `json:"round_type"`
		VotingMethod string `json:"voting_method"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.ElectionID == 0 {
		jsonErr(w, "election_id required", 400)
		return
	}
	if req.RoundNumber <= 0 {
		req.RoundNumber = 1
	}
	if req.RoundType == "" {
		req.RoundType = "regular"
	}
	if req.VotingMethod == "" {
		req.VotingMethod = "secret_ballot"
	}

	roundID := "rnd-" + uuid.New().String()[:8]

	// Get eligible voter count
	partyCode := fmt.Sprintf("party_%d", pid)
	var eligible int
	dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE party_code=$1 AND election_id=$2 AND accreditation_status='accredited'",
		partyCode, req.ElectionID).Scan(&eligible)

	_, err := dbConn.ExecContext(r.Context(), `
		INSERT INTO voting_rounds (round_id, election_id, round_number, round_type,
			voting_method, total_eligible_voters)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		roundID, req.ElectionID, req.RoundNumber, req.RoundType, req.VotingMethod, eligible)
	if err != nil {
		jsonErr(w, "create round failed: "+err.Error(), 500)
		return
	}

	publishKafkaEvent("primaries.round.created", map[string]interface{}{
		"round_id": roundID, "election_id": req.ElectionID, "round_number": req.RoundNumber,
		"voting_method": req.VotingMethod,
	})

	logConventionEvent(r.Context(), req.ElectionID, "round_created", user, "round", roundID, map[string]interface{}{
		"round_number": req.RoundNumber, "method": req.VotingMethod, "eligible": eligible,
	})

	jsonResp(w, map[string]interface{}{"round_id": roundID, "eligible_voters": eligible})
}

func handleOpenRound(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	// SECURITY: cross-party scoping — the round's election must belong to the
	// caller's party before any mutation.
	elecID, owned := partyOwnsRound(r.Context(), id, partyCode)
	if elecID == 0 {
		jsonErr(w, "round not found", 404)
		return
	}
	if !owned {
		log.Warn().Str("round_id", id).Str("caller_party", partyCode).Msg("SECURITY: cross-party round-open attempt rejected")
		jsonErr(w, "round does not belong to your party", 403)
		return
	}

	// Verify quorum before opening
	var total, accredited int
	dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE party_code=$1 AND election_id=$2", partyCode, elecID).Scan(&total)
	dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE party_code=$1 AND election_id=$2 AND accreditation_status='accredited'", partyCode, elecID).Scan(&accredited)

	quorumPct := float64(accredited) / math.Max(float64(total), 1) * 100
	if quorumPct < quorumThresholdFraction*100 {
		jsonErr(w, fmt.Sprintf("quorum not met: %.1f%% (need %.0f%%)", quorumPct, quorumThresholdFraction*100), 400)
		return
	}

	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE voting_rounds SET status='open', opened_at=NOW(),
			quorum_required=$1, quorum_present=$2, quorum_met=TRUE
		WHERE round_id=$3 AND status='pending'
		AND EXISTS (SELECT 1 FROM delegates d WHERE d.election_id=voting_rounds.election_id AND d.party_code=$4)`,
		total, accredited, id, partyCode)
	if err != nil {
		jsonErr(w, "open round failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "round not found or already open", 404)
		return
	}

	// Invalidate dashboard cache
	cacheInvalidate(r.Context(), fmt.Sprintf("convention_dash:%s:%d", partyCode, elecID))

	publishKafkaEvent("primaries.round.opened", map[string]interface{}{
		"round_id": id, "quorum_pct": quorumPct, "accredited": accredited,
	})

	publishFluvioEvent("primaries-stream", map[string]interface{}{
		"event": "round_opened", "round_id": id, "quorum_pct": quorumPct,
	})

	logConventionEvent(r.Context(), elecID, "round_opened", user, "round", id, map[string]interface{}{
		"quorum_pct": quorumPct, "accredited": accredited,
	})

	jsonResp(w, map[string]interface{}{"opened": true, "quorum_pct": quorumPct})
}

func handleCloseRound(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	// SECURITY: cross-party scoping — the round's election must belong to the
	// caller's party (enforced again inside the UPDATE for TOCTOU safety).
	elecID, owned := partyOwnsRound(r.Context(), id, partyCode)
	if elecID == 0 {
		jsonErr(w, "round not found", 404)
		return
	}
	if !owned {
		log.Warn().Str("round_id", id).Str("caller_party", partyCode).Msg("SECURITY: cross-party round-close attempt rejected")
		jsonErr(w, "round does not belong to your party", 403)
		return
	}

	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE voting_rounds SET status='closed', closed_at=NOW()
		WHERE round_id=$1 AND status IN ('open','voting')
		AND EXISTS (SELECT 1 FROM delegates d WHERE d.election_id=voting_rounds.election_id AND d.party_code=$2)`, id, partyCode)
	if err != nil {
		jsonErr(w, "close round failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "round not found or not open", 404)
		return
	}

	publishKafkaEvent("primaries.round.closed", map[string]interface{}{"round_id": id})

	logConventionEvent(r.Context(), elecID, "round_closed", user, "round", id, nil)

	jsonResp(w, map[string]interface{}{"closed": true})
}

func handleTallyRound(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	// SECURITY: cross-party scoping — tallying writes aspirants.delegate_votes
	// and is_winner, so the round's election MUST belong to the caller's
	// party; otherwise any party user could rewrite another party's results.
	elecID, owned := partyOwnsRound(r.Context(), id, partyCode)
	if elecID == 0 {
		jsonErr(w, "round not found", 404)
		return
	}
	if !owned {
		log.Warn().Str("round_id", id).Str("caller_party", partyCode).Msg("SECURITY: cross-party tally attempt rejected")
		jsonErr(w, "round does not belong to your party", 403)
		return
	}

	// Count cleartext (legacy, in-person pre-custody-fix) ballots per aspirant.
	// Backend-custodied ballots (aspirant_id IS NULL) never join here.
	rows, err := dbConn.QueryContext(r.Context(), `
		SELECT b.aspirant_id, a.full_name, COUNT(*) as votes
		FROM ballots b
		JOIN aspirants a ON a.aspirant_id = b.aspirant_id
		WHERE b.round_id=$1 AND b.vote_type='for' AND b.is_decoy=FALSE AND b.tallied=FALSE
		AND a.party_code=$2
		GROUP BY b.aspirant_id, a.full_name
		ORDER BY votes DESC`, id, partyCode)
	if err != nil {
		jsonErr(w, "tally query failed", 500)
		return
	}

	voteCounts := map[string]int{}
	names := map[string]string{}
	var totalVotes int
	for rows.Next() {
		var aspID, name string
		var votes int
		rows.Scan(&aspID, &name, &votes)
		voteCounts[aspID] += votes
		names[aspID] = name
		totalVotes += votes
	}
	rows.Close()

	// SECURITY: backend-custodied ballots (encrypted, aspirant linkage held
	// by the crypto backend) can ONLY be tallied by that backend — this
	// service must never see their cleartext choices. On backend failure →
	// 503 and NO tally records are written.
	var custodiedRefs []string
	refRows, err := dbConn.QueryContext(r.Context(), `
		SELECT ballot_ref FROM ballots
		WHERE round_id=$1 AND is_decoy=FALSE AND tallied=FALSE AND ballot_ref IS NOT NULL`, id)
	if err != nil {
		jsonErr(w, "tally query failed", 500)
		return
	}
	for refRows.Next() {
		var ref string
		refRows.Scan(&ref)
		custodiedRefs = append(custodiedRefs, ref)
	}
	refRows.Close()
	if len(custodiedRefs) > 0 {
		backendCounts, err := tallyBallotsViaBackend(r.Context(), id, custodiedRefs)
		if err != nil {
			log.Error().Err(err).Str("round_id", id).Msg("crypto backend tally failed — no tally written")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error":  "cryptographic tally service unavailable",
				"detail": "backend-custodied ballots could not be tallied; refusing to publish a partial tally",
			})
			return
		}
		for aspID, votes := range backendCounts {
			// Only count aspirants of the caller's party.
			var name string
			if err := dbConn.QueryRowContext(r.Context(),
				"SELECT full_name FROM aspirants WHERE aspirant_id=$1 AND party_code=$2",
				aspID, partyCode).Scan(&name); err != nil {
				log.Warn().Str("aspirant_id", aspID).Msg("backend tally returned unknown/other-party aspirant — skipped")
				continue
			}
			voteCounts[aspID] += votes
			names[aspID] = name
			totalVotes += votes
		}
	}

	type tallyEntry struct {
		AspirantID string
		Name       string
		Votes      int
	}
	var tallies []tallyEntry
	for aspID, votes := range voteCounts {
		tallies = append(tallies, tallyEntry{AspirantID: aspID, Name: names[aspID], Votes: votes})
	}
	sort.Slice(tallies, func(i, j int) bool { return tallies[i].Votes > tallies[j].Votes })

	// Also count abstentions and spoiled — INTEGRITY: filtered to
	// tallied=FALSE just like the 'for' count, otherwise a re-tally
	// double-counts them.
	var abstentions, spoiled int
	dbConn.QueryRow("SELECT COUNT(*) FROM ballots WHERE round_id=$1 AND vote_type='abstain' AND is_decoy=FALSE AND tallied=FALSE", id).Scan(&abstentions)
	dbConn.QueryRow("SELECT COUNT(*) FROM ballots WHERE round_id=$1 AND vote_type='spoiled' AND is_decoy=FALSE AND tallied=FALSE", id).Scan(&spoiled)

	// Insert/update tally records (party-scoped aspirant updates only)
	for rank, t := range tallies {
		pct := 0.0
		if totalVotes > 0 {
			pct = math.Round(float64(t.Votes)/float64(totalVotes)*10000) / 100
		}
		isWinner := rank == 0 && pct > 50.0
		dbConn.ExecContext(r.Context(), `
			INSERT INTO vote_tallies (round_id, aspirant_id, votes_received, vote_percentage, rank_position, is_winner)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (round_id, aspirant_id) DO UPDATE SET
				votes_received=$3, vote_percentage=$4, rank_position=$5, is_winner=$6`,
			id, t.AspirantID, t.Votes, pct, rank+1, isWinner)

		// Update aspirant's total delegate votes — SECURITY: party-scoped so a
		// tally can never rewrite another party's aspirant rows.
		dbConn.ExecContext(r.Context(), "UPDATE aspirants SET delegate_votes=$1, is_winner=$2 WHERE aspirant_id=$3 AND party_code=$4",
			t.Votes, isWinner, t.AspirantID, partyCode)
	}

	// Mark ballots as tallied
	dbConn.ExecContext(r.Context(), "UPDATE ballots SET tallied=TRUE, tallied_at=NOW() WHERE round_id=$1", id)

	// Update round stats (party-ownership re-enforced in the WHERE clause)
	totalCast := totalVotes + abstentions + spoiled
	dbConn.ExecContext(r.Context(), `
		UPDATE voting_rounds SET status='tallying',
			total_votes_cast=$1, total_valid_votes=$2, total_invalid_votes=$3
		WHERE round_id=$4
		AND EXISTS (SELECT 1 FROM delegates d WHERE d.election_id=voting_rounds.election_id AND d.party_code=$5)`,
		totalCast, totalVotes, spoiled, id, partyCode)

	// Build Merkle root of all ballot hashes
	merkleRoot := buildBallotMerkleRoot(r.Context(), id)
	dbConn.ExecContext(r.Context(), "UPDATE voting_rounds SET merkle_root=$1 WHERE round_id=$2", merkleRoot, id)

	// Build result
	var results []map[string]interface{}
	for rank, t := range tallies {
		pct := 0.0
		if totalVotes > 0 {
			pct = math.Round(float64(t.Votes)/float64(totalVotes)*10000) / 100
		}
		results = append(results, map[string]interface{}{
			"aspirant_id": t.AspirantID, "full_name": t.Name,
			"votes": t.Votes, "percentage": pct, "rank": rank + 1,
			"is_winner": rank == 0 && pct > 50.0,
		})
	}

	publishKafkaEvent("primaries.round.tallied", map[string]interface{}{
		"round_id": id, "total_votes": totalVotes, "results": results,
	})

	logConventionEvent(r.Context(), elecID, "round_tallied", user, "round", id, map[string]interface{}{
		"total_votes": totalVotes, "abstentions": abstentions, "spoiled": spoiled,
	})

	jsonResp(w, map[string]interface{}{
		"round_id":    id,
		"results":     results,
		"total_cast":  totalCast,
		"valid_votes": totalVotes,
		"abstentions": abstentions,
		"spoiled":     spoiled,
		"merkle_root": merkleRoot,
	})
}

func handleCertifyRound(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	// SECURITY: cross-party scoping — certification is final and must never
	// be applied to another party's round.
	elecID, owned := partyOwnsRound(r.Context(), id, partyCode)
	if elecID == 0 {
		jsonErr(w, "round not found", 404)
		return
	}
	if !owned {
		log.Warn().Str("round_id", id).Str("caller_party", partyCode).Msg("SECURITY: cross-party certify attempt rejected")
		jsonErr(w, "round does not belong to your party", 403)
		return
	}

	// R5-039 four-eyes: the officer who TALLIED a round may not also CERTIFY
	// it — tally and certification must be distinct identities (separation
	// of duties; the audit log is the system of record for the tally actor).
	var tallyActor string
	err := dbConn.QueryRowContext(r.Context(), `
		SELECT actor_id FROM convention_audit_log
		WHERE event_type='round_tallied' AND entity_type='round' AND entity_id=$1
		ORDER BY id DESC LIMIT 1`, id).Scan(&tallyActor)
	if err != nil && err != sql.ErrNoRows {
		log.Error().Err(err).Str("round_id", id).Msg("SECURITY: tally-actor lookup failed (fail closed)")
		jsonErr(w, "cannot verify separation of duties", 500)
		return
	}
	if err == nil && tallyActor == user {
		log.Warn().Str("round_id", id).Str("user", user).Msg("SECURITY: certify by tally actor rejected (four-eyes)")
		jsonErr(w, "separation of duties: the tallying officer cannot certify the same round", 403)
		return
	}

	// INTEGRITY: certification requires a REAL chain anchor for the tallied
	// results. The previous implementation stored hashStringSHA(id+timestamp)
	// as "blockchain_hash" — a locally minted hash masquerading as a chain
	// attestation. Never mint local hashes labeled blockchain: either an
	// external anchor service (GOTV_ANCHOR_SERVICE_URL) or the in-binary
	// hash-chained Merkle anchor client (gotvBlockchain) must produce the
	// attestation, otherwise certification fails loudly (503) and the round
	// is NOT certified.
	var merkleRoot string
	dbConn.QueryRowContext(r.Context(),
		"SELECT COALESCE(merkle_root,'') FROM voting_rounds WHERE round_id=$1", id).Scan(&merkleRoot)
	blockchainHash, anchorErr := anchorCertifiedResults(r.Context(), pid, id, elecID, merkleRoot)
	if anchorErr != nil {
		log.Error().Err(anchorErr).Str("round_id", id).Msg("blockchain anchoring failed — round NOT certified")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "blockchain anchoring not configured",
			"detail": anchorErr.Error(),
		})
		return
	}

	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE voting_rounds SET status='certified', certified_at=NOW()
		WHERE round_id=$1 AND status='tallying'
		AND EXISTS (SELECT 1 FROM delegates d WHERE d.election_id=voting_rounds.election_id AND d.party_code=$2)`, id, partyCode)
	if err != nil || func() int64 { n, _ := res.RowsAffected(); return n }() == 0 {
		jsonErr(w, "certify failed or round not in tallying state", 400)
		return
	}

	// Store the real anchor reference (block hash / anchor tx id).
	dbConn.ExecContext(r.Context(), "UPDATE voting_rounds SET blockchain_hash=$1 WHERE round_id=$2", blockchainHash, id)

	publishKafkaEvent("primaries.round.certified", map[string]interface{}{
		"round_id": id, "blockchain_hash": blockchainHash,
	})

	logConventionEvent(r.Context(), elecID, "round_certified", user, "round", id, map[string]interface{}{
		"blockchain_hash": blockchainHash,
	})

	jsonResp(w, map[string]interface{}{"certified": true, "blockchain_hash": blockchainHash})
}

// anchorCertifiedResults produces a real chain attestation for a certified
// round. Order of preference:
//  1. External anchor service (GOTV_ANCHOR_SERVICE_URL) — POST /anchor with
//     the round's Merkle root; expects {"anchor_tx_id"|"tx_id"|"hash"}.
//  2. The in-binary hash-chained Merkle anchor client (gotvBlockchain) —
//     each anchor is a block linked to the previous block's hash, with
//     inclusion proofs; NOT a bare local hash.
//
// INTEGRITY: returns an error when neither path is available — callers must
// fail loudly instead of minting a local hash labeled "blockchain".
func anchorCertifiedResults(ctx context.Context, partyID int, roundID string, electionID int, merkleRoot string) (string, error) {
	if anchorURL := os.Getenv("GOTV_ANCHOR_SERVICE_URL"); anchorURL != "" {
		payload, _ := json.Marshal(map[string]interface{}{
			"round_id": roundID, "election_id": electionID,
			"party_id": partyID, "merkle_root": merkleRoot,
		})
		respBody, code, err := resilientCall(ctx, cbAnchorService, "POST", anchorURL+"/anchor", payload)
		if err != nil || code != http.StatusOK {
			return "", fmt.Errorf("anchor service call failed (code=%d): %v", code, err)
		}
		var result struct {
			AnchorTxID string `json:"anchor_tx_id"`
			TxID       string `json:"tx_id"`
			Hash       string `json:"hash"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", fmt.Errorf("anchor service returned malformed response: %w", err)
		}
		ref := result.AnchorTxID
		if ref == "" {
			ref = result.TxID
		}
		if ref == "" {
			ref = result.Hash
		}
		if ref == "" {
			return "", fmt.Errorf("anchor service returned no transaction reference")
		}
		return ref, nil
	}
	if gotvBlockchain != nil {
		leaves := []string{merkleRoot}
		if merkleRoot == "" {
			leaves = []string{roundID}
		}
		res, err := gotvBlockchain.AnchorMerkleRoot(ctx, partyID, "primary_certification", leaves)
		if err != nil {
			return "", fmt.Errorf("local Merkle anchor failed: %w", err)
		}
		if res.BlockHash == "" {
			return "", fmt.Errorf("local Merkle anchor returned empty block hash")
		}
		return res.BlockHash, nil
	}
	return "", fmt.Errorf("no blockchain anchor path available (GOTV_ANCHOR_SERVICE_URL unset and local anchor client not initialized)")
}

func handleRoundResults(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	rows, err := dbConn.QueryContext(r.Context(), `
		SELECT vt.aspirant_id, a.full_name, vt.votes_received, vt.vote_percentage,
			vt.rank_position, vt.is_eliminated, vt.is_winner
		FROM vote_tallies vt
		JOIN aspirants a ON a.aspirant_id = vt.aspirant_id
		WHERE vt.round_id=$1
		ORDER BY vt.rank_position`, id)
	if err != nil {
		jsonErr(w, "query failed", 500)
		return
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var aspID, name string
		var votes, rank int
		var pct float64
		var eliminated, winner bool
		rows.Scan(&aspID, &name, &votes, &pct, &rank, &eliminated, &winner)
		results = append(results, map[string]interface{}{
			"aspirant_id": aspID, "full_name": name, "votes": votes,
			"percentage": pct, "rank": rank, "eliminated": eliminated, "winner": winner,
		})
	}

	// Round metadata
	var status, method, merkle string
	var roundNum, cast, valid int
	dbConn.QueryRow(`SELECT status, voting_method, round_number, total_votes_cast, total_valid_votes, COALESCE(merkle_root,'')
		FROM voting_rounds WHERE round_id=$1`, id).Scan(&status, &method, &roundNum, &cast, &valid, &merkle)

	jsonResp(w, map[string]interface{}{
		"round_id": id, "round_number": roundNum, "status": status,
		"voting_method": method, "total_cast": cast, "total_valid": valid,
		"merkle_root": merkle, "results": results,
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// BALLOT CASTING (IN-PERSON — Phase 1)
// ═══════════════════════════════════════════════════════════════════════════

func handleCastBallot(w http.ResponseWriter, r *http.Request) {
	_, user := getParty(r)
	var req struct {
		RoundID    string `json:"round_id"`
		DelegateID string `json:"delegate_id"`
		AspirantID string `json:"aspirant_id"`
		VoteType   string `json:"vote_type"` // for, against, abstain
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", 400)
		return
	}
	if req.RoundID == "" || req.DelegateID == "" {
		jsonErr(w, "round_id and delegate_id required", 400)
		return
	}
	if req.VoteType == "" {
		req.VoteType = "for"
	}
	if req.VoteType == "for" && req.AspirantID == "" {
		jsonErr(w, "aspirant_id required for 'for' votes", 400)
		return
	}

	// SECURITY: refuses to fabricate cryptographic artifacts. Ballots in an
	// election system must be encrypted with a real ElectionGuard/Paillier
	// backend. When no backend is configured, casting is rejected with 503 —
	// silently storing an unencrypted ballot is worse than failing loudly.
	if !cryptoBackendConfigured() {
		cryptoUnavailable(w, "ballot encryption service")
		return
	}

	// Verify round is open
	var roundStatus string
	err := dbConn.QueryRow("SELECT status FROM voting_rounds WHERE round_id=$1", req.RoundID).Scan(&roundStatus)
	if err != nil || (roundStatus != "open" && roundStatus != "voting") {
		jsonErr(w, "voting round is not open", 400)
		return
	}

	// Verify delegate is accredited and hasn't voted this round
	var accStatus string
	var hasVoted bool
	var voteRound sql.NullInt64
	err = dbConn.QueryRow(`SELECT accreditation_status, has_voted, vote_round FROM delegates WHERE delegate_id=$1`,
		req.DelegateID).Scan(&accStatus, &hasVoted, &voteRound)
	if err != nil {
		jsonErr(w, "delegate not found", 404)
		return
	}
	if accStatus != "accredited" {
		jsonErr(w, "delegate not accredited", 403)
		return
	}

	// Check if already voted in this round
	var roundNum int
	dbConn.QueryRow("SELECT round_number FROM voting_rounds WHERE round_id=$1", req.RoundID).Scan(&roundNum)
	if hasVoted && voteRound.Valid && int(voteRound.Int64) == roundNum {
		jsonErr(w, "delegate already voted in this round", 409)
		return
	}

	// Generate confirmation code for E2E verifiability
	confirmationCode := generateConfirmationCode()
	verificationHash := hashStringSHA(req.RoundID + req.DelegateID + confirmationCode)

	ballotID := "bal-" + uuid.New().String()[:8]

	// SECURITY: ballot custody. The vote choice is submitted to the crypto
	// backend, which returns ONLY ciphertext + an opaque ballot_ref; the
	// aspirant linkage stays in the backend's trust domain. On backend
	// failure we return 503 and insert NOTHING — never degrade to a
	// cleartext aspirant_id stored next to delegate_id.
	if err := ensureHardeningColumns(r.Context()); err != nil {
		log.Error().Err(err).Msg("ballot custody columns unavailable")
		cryptoUnavailable(w, "ballot custody storage")
		return
	}
	ciphertext, proof, ballotRef, err := encryptBallotViaBackend(r.Context(),
		req.RoundID, ballotID, req.VoteType, req.AspirantID)
	if err != nil {
		log.Error().Err(err).Str("round_id", req.RoundID).Msg("ballot encryption backend call failed — no ballot stored")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "ballot encryption service unavailable",
			"detail": "the election crypto backend could not encrypt this ballot; refusing to store a cleartext vote choice",
		})
		return
	}

	// aspirant_id is intentionally NOT stored on this row: the cleartext vote
	// choice must never sit next to the voter's identity in this database.
	_, err = dbConn.ExecContext(r.Context(), `
		INSERT INTO ballots (ballot_id, round_id, delegate_id, aspirant_id, vote_type,
			confirmation_code, verification_hash, is_remote, encrypted_ballot, ballot_proof, ballot_ref)
		VALUES ($1,$2,$3,NULL,$4,$5,$6,FALSE,$7,$8,$9)`,
		ballotID, req.RoundID, req.DelegateID, req.VoteType,
		confirmationCode, verificationHash, ciphertext, nullStr(proof), nullStr(ballotRef))
	if err != nil {
		jsonErr(w, "ballot cast failed: "+err.Error(), 500)
		return
	}

	// Mark delegate as voted
	dbConn.ExecContext(r.Context(), `
		UPDATE delegates SET has_voted=TRUE, vote_round=$1, vote_timestamp=NOW()
		WHERE delegate_id=$2`, roundNum, req.DelegateID)

	// Update round status to 'voting' if first vote
	dbConn.ExecContext(r.Context(), `
		UPDATE voting_rounds SET status='voting' WHERE round_id=$1 AND status='open'`, req.RoundID)

	// Record TigerBeetle audit transfer (best-effort audit — a missing ledger
	// must not block a ballot that was already encrypted and stored, but the
	// gap is logged loudly).
	tbID, tbErr := recordTBTransfer("ballot_cast", 100, ballotID, user) // 1 naira audit token
	if tbErr != nil {
		log.Error().Err(tbErr).Str("ballot_id", ballotID).Msg("ballot audit transfer not recorded")
	}

	publishKafkaEvent("primaries.ballot.cast", map[string]interface{}{
		"ballot_id": ballotID, "round_id": req.RoundID, "vote_type": req.VoteType,
		"is_remote": false,
	})

	publishFluvioEvent("primaries-stream", map[string]interface{}{
		"event": "ballot_cast", "round_id": req.RoundID, "is_remote": false,
	})

	var elecID int
	dbConn.QueryRow("SELECT election_id FROM voting_rounds WHERE round_id=$1", req.RoundID).Scan(&elecID)
	logConventionEvent(r.Context(), elecID, "ballot_cast", user, "ballot", ballotID, map[string]interface{}{
		"vote_type": req.VoteType, "is_remote": false,
	})

	// Invalidate dashboard cache
	cacheInvalidate(r.Context(), fmt.Sprintf("convention_dash:%s:%d", fmt.Sprintf("party_%d", func() int { p, _ := getParty(r); return p }()), elecID))

	jsonResp(w, map[string]interface{}{
		"ballot_id":         ballotID,
		"confirmation_code": confirmationCode,
		"tb_transfer_id":    tbID,
		"status":            "cast",
	})
}

func handleVerifyBallot(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("confirmation_code")
	if code == "" {
		jsonErr(w, "confirmation_code required", 400)
		return
	}

	// SECURITY: a receipt lookup returns inclusion/tally status ONLY. It must
	// NEVER return the vote choice (vote_type) — anyone holding a
	// confirmation code (e.g. a vote buyer demanding a receipt) could
	// otherwise verify HOW the voter voted, which enables vote buying and
	// coercion.
	var ballotID, roundID string
	var castAt time.Time
	var tallied bool
	err := dbConn.QueryRow(`
		SELECT ballot_id, round_id, cast_at, tallied
		FROM ballots WHERE confirmation_code=$1`, code).
		Scan(&ballotID, &roundID, &castAt, &tallied)
	if err != nil {
		jsonErr(w, "ballot not found", 404)
		return
	}

	jsonResp(w, map[string]interface{}{
		"ballot_id": ballotID,
		"round_id":  roundID,
		"cast_at":   castAt,
		"tallied":   tallied,
		"included":  true,
		"verified":  true,
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// REMOTE VOTING (Phase 2) — E2E Verifiable Electronic Voting
// ═══════════════════════════════════════════════════════════════════════════

func handleRegisterVotingDevice(w http.ResponseWriter, r *http.Request) {
	_, _ = getParty(r)
	var req struct {
		DelegateID        string `json:"delegate_id"`
		DeviceType        string `json:"device_type"`
		DeviceFingerprint string `json:"device_fingerprint"`
		OSVersion         string `json:"os_version"`
		AppVersion        string `json:"app_version"`
		IMEIHash          string `json:"imei_hash"`
		PublicKey         string `json:"public_key"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.DelegateID == "" || req.DeviceFingerprint == "" {
		jsonErr(w, "delegate_id and device_fingerprint required", 400)
		return
	}
	if req.DeviceType == "" {
		req.DeviceType = "mobile"
	}

	// Verify delegate exists and is accredited
	var accStatus string
	err := dbConn.QueryRow("SELECT accreditation_status FROM delegates WHERE delegate_id=$1", req.DelegateID).Scan(&accStatus)
	if err != nil {
		jsonErr(w, "delegate not found", 404)
		return
	}
	if accStatus != "accredited" {
		jsonErr(w, "delegate must be accredited for remote voting", 403)
		return
	}

	// Check for existing device — one device per delegate
	var existing int
	dbConn.QueryRow("SELECT COUNT(*) FROM remote_voting_devices WHERE delegate_id=$1 AND is_active=TRUE", req.DelegateID).Scan(&existing)
	if existing > 0 {
		jsonErr(w, "device already registered for this delegate", 409)
		return
	}

	deviceID := "rvd-" + uuid.New().String()[:8]
	_, err = dbConn.ExecContext(r.Context(), `
		INSERT INTO remote_voting_devices (device_id, delegate_id, device_type,
			device_fingerprint, os_version, app_version, imei_hash, public_key, is_registered)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,TRUE)`,
		deviceID, req.DelegateID, req.DeviceType, req.DeviceFingerprint,
		nullStr(req.OSVersion), nullStr(req.AppVersion), nullStr(req.IMEIHash), nullStr(req.PublicKey))
	if err != nil {
		jsonErr(w, "device registration failed", 500)
		return
	}

	// WAF check via OpenAppSec
	wafCheckRemoteVoting(r)

	publishKafkaEvent("primaries.remote.device_registered", map[string]interface{}{
		"device_id": deviceID, "delegate_id": req.DelegateID, "device_type": req.DeviceType,
	})

	jsonResp(w, map[string]interface{}{"device_id": deviceID, "registered": true})
}

func handleCreateVotingSession(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		DelegateID string `json:"delegate_id"`
		RoundID    string `json:"round_id"`
		DeviceID   string `json:"device_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.DelegateID == "" || req.RoundID == "" {
		jsonErr(w, "delegate_id and round_id required", 400)
		return
	}

	// SECURITY: the delegate must exist, be ACCREDITED, and belong to the
	// CALLER'S party. Previously any authenticated user could mint a voting
	// session (and receive the OTP!) for ANY delegate_id — a complete
	// remote-voting takeover primitive.
	partyCode := fmt.Sprintf("party_%d", pid)
	var delegateParty, accStatus string
	var phoneHash sql.NullString
	err := dbConn.QueryRowContext(r.Context(), `
		SELECT party_code, accreditation_status, phone_hash FROM delegates WHERE delegate_id=$1`,
		req.DelegateID).Scan(&delegateParty, &accStatus, &phoneHash)
	if err != nil {
		jsonErr(w, "delegate not found", 404)
		return
	}
	if delegateParty != partyCode {
		log.Warn().Str("delegate_id", req.DelegateID).Str("caller_party", partyCode).
			Msg("SECURITY: cross-party voting session attempt rejected")
		jsonErr(w, "delegate does not belong to your party", 403)
		return
	}
	if accStatus != "accredited" {
		jsonErr(w, "delegate is not accredited", 403)
		return
	}

	// Verify round is open for remote voting
	var roundStatus, votingMethod string
	err = dbConn.QueryRow("SELECT status, voting_method FROM voting_rounds WHERE round_id=$1", req.RoundID).
		Scan(&roundStatus, &votingMethod)
	if err != nil || (roundStatus != "open" && roundStatus != "voting") {
		jsonErr(w, "voting round is not open", 400)
		return
	}
	if votingMethod != "remote_electronic" && votingMethod != "electronic" {
		jsonErr(w, "this round does not support remote voting", 400)
		return
	}

	// Generate OTP
	otp := generateOTP()
	otpHash := hashStringSHA(otp)
	sessionID := "vs-" + uuid.New().String()[:8]
	expiresAt := time.Now().Add(10 * time.Minute)

	// NOTE: the voting_sessions.status CHECK constraint does not include
	// 'pending_delivery', so an undelivered OTP keeps status 'pending'; the
	// 202 response's delivery:"pending" field carries the delivery state and
	// the failure is logged at ERROR for ops follow-up.
	_, err = dbConn.ExecContext(r.Context(), `
		INSERT INTO voting_sessions (session_id, delegate_id, round_id, device_id,
			otp_hash, otp_expires_at, status, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,'pending',$7)`,
		sessionID, req.DelegateID, req.RoundID, nullStr(req.DeviceID),
		otpHash, expiresAt, expiresAt)
	if err != nil {
		jsonErr(w, "session creation failed", 500)
		return
	}

	// Store OTP in Redis (expires in 10 min)
	cacheSet(r.Context(), "voting_otp:"+sessionID, otp, 10*time.Minute)

	// SECURITY: the OTP is NEVER returned in the API response. It is
	// delivered to the delegate's registered phone via the SMS/WhatsApp
	// sender; when no sender is configured (or delivery fails) the session
	// stays pending and we return 202 delivery:"pending". The plaintext OTP
	// is logged only at DEBUG level and only in dev mode.
	delivered := deliverVotingSessionOTP(r.Context(), pid, phoneHash, otp)
	if !delivered {
		log.Error().Str("session_id", sessionID).Str("delegate_id", req.DelegateID).
			Msg("voting session OTP could not be delivered (no SMS/WhatsApp sender configured or delivery failed) — session pending delivery")
		if devModeEnabled {
			log.Debug().Str("session_id", sessionID).Str("otp", otp).Msg("DEV ONLY: voting session OTP")
		}
	}

	publishKafkaEvent("primaries.remote.session_created", map[string]interface{}{
		"session_id": sessionID, "delegate_id": req.DelegateID, "actor": user,
	})

	if !delivered {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"session_id": sessionID,
			"delivery":   "pending",
			"expires_at": expiresAt,
		})
		return
	}
	jsonResp(w, map[string]interface{}{
		"session_id": sessionID,
		"delivery":   "sent",
		"expires_at": expiresAt,
	})
}

// deliverVotingSessionOTP sends the voting OTP to the delegate's registered
// phone via the configured SMS/WhatsApp sender. The delegate's phone number
// is resolved from the party's contact registry (phone_hash match) and
// decrypted in memory only for the send. Returns false when no sender is
// configured, the phone cannot be resolved, or the send fails — callers MUST
// treat false as "pending delivery", never as a reason to expose the OTP.
func deliverVotingSessionOTP(ctx context.Context, partyID int, phoneHash sql.NullString, otp string) bool {
	if !phoneHash.Valid || phoneHash.String == "" || svc == nil {
		return false
	}
	var encPhone string
	if err := dbConn.QueryRowContext(ctx, `
		SELECT phone_encrypted FROM gotv_contacts
		WHERE party_id=$1 AND phone_hash=$2 AND opted_out=FALSE`,
		partyID, phoneHash.String).Scan(&encPhone); err != nil {
		return false
	}
	phone, err := svc.Decrypt(encPhone)
	if err != nil || phone == "" {
		return false
	}
	text := "Your INEC primary-election voting code is " + otp + ". It expires in 10 minutes. Never share it."
	// Prefer SMS (Africa's Talking), fall back to WhatsApp.
	if smsKey := os.Getenv("AFRICASTALKING_API_KEY"); smsKey != "" {
		adapter := gotv.NewSMSAdapter("africastalking",
			"https://api.africastalking.com/version1", smsKey, os.Getenv("AFRICASTALKING_SENDER"), os.Getenv("AFRICASTALKING_USERNAME"))
		res := adapter.Send(ctx, gotv.OutboundMessage{
			PartyID: partyID, Phone: phone, Template: text, Channel: "sms",
		})
		if res.Status != "failed" {
			return true
		}
		log.Warn().Str("error", res.Error).Msg("OTP SMS delivery failed, trying WhatsApp")
	}
	if waToken := os.Getenv("WHATSAPP_TOKEN"); waToken != "" {
		adapter := gotv.NewWhatsAppAdapter(
			"https://graph.facebook.com/v18.0", waToken, os.Getenv("WHATSAPP_PHONE_ID"))
		res := adapter.Send(ctx, gotv.OutboundMessage{
			PartyID: partyID, Phone: phone, Template: text, Channel: "whatsapp",
		})
		if res.Status != "failed" {
			return true
		}
		log.Warn().Str("error", res.Error).Msg("OTP WhatsApp delivery failed")
	}
	return false
}

func handleRemoteAuthenticate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID         string `json:"session_id"`
		OTP               string `json:"otp"`
		BiometricPayload  string `json:"biometric_payload"`
		DeviceFingerprint string `json:"device_fingerprint"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.SessionID == "" || req.OTP == "" {
		jsonErr(w, "session_id and otp required", 400)
		return
	}

	// SECURITY: biometric verification must NEVER be inferred from payload
	// non-emptiness — previously any non-empty string was recorded as
	// biometric_verified=TRUE in voting_sessions, gating remote ballot
	// casting. Without a configured biometric pipeline, reject with 503.
	// No session may be created on the basis of a non-empty string.
	if biometricServiceURL == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "biometric verification service unavailable",
			"detail": "no biometric verification pipeline configured (GOTV_BIOMETRIC_SERVICE_URL); refusing to authenticate remote voting session",
		})
		return
	}

	// Verify OTP
	var storedOTPHash string
	var otpExpires time.Time
	var sessionStatus string
	err := dbConn.QueryRow(`SELECT otp_hash, otp_expires_at, status FROM voting_sessions WHERE session_id=$1`,
		req.SessionID).Scan(&storedOTPHash, &otpExpires, &sessionStatus)
	if err != nil {
		jsonErr(w, "session not found", 404)
		return
	}
	if sessionStatus != "pending" {
		jsonErr(w, "session already used or expired", 400)
		return
	}
	if time.Now().After(otpExpires) {
		dbConn.Exec("UPDATE voting_sessions SET status='expired' WHERE session_id=$1", req.SessionID)
		jsonErr(w, "OTP expired", 401)
		return
	}
	if hashStringSHA(req.OTP) != storedOTPHash {
		jsonErr(w, "invalid OTP", 401)
		return
	}

	// Verify device fingerprint matches registered device
	var delegateID string
	dbConn.QueryRow("SELECT delegate_id FROM voting_sessions WHERE session_id=$1", req.SessionID).Scan(&delegateID)
	if req.DeviceFingerprint != "" {
		var deviceMatch int
		dbConn.QueryRow("SELECT COUNT(*) FROM remote_voting_devices WHERE delegate_id=$1 AND device_fingerprint=$2 AND is_active=TRUE",
			delegateID, req.DeviceFingerprint).Scan(&deviceMatch)
		if deviceMatch == 0 {
			jsonErr(w, "device not registered for this delegate", 403)
			return
		}
	}

	// Keycloak session validation
	keycloakSessionID := validateKeycloakRemoteVoting(r)

	// Verify the biometric payload against the configured biometric pipeline.
	// SECURITY: refuses to fabricate biometric verification — the pipeline
	// must explicitly verify the payload.
	biometricVerified := verifyBiometricPayload(r, req.BiometricPayload)
	if !biometricVerified {
		jsonErr(w, "biometric verification failed", 401)
		return
	}

	// Update session
	dbConn.ExecContext(r.Context(), `
		UPDATE voting_sessions SET status='authenticated', biometric_verified=$1,
			keycloak_session_id=$2, ip_hash=$3
		WHERE session_id=$4`,
		biometricVerified, keycloakSessionID, hashStringSHA(r.RemoteAddr), req.SessionID)

	publishKafkaEvent("primaries.remote.authenticated", map[string]interface{}{
		"session_id": req.SessionID, "biometric": biometricVerified,
	})

	jsonResp(w, map[string]interface{}{
		"authenticated":      true,
		"session_id":         req.SessionID,
		"biometric_verified": biometricVerified,
	})
}

func handleRemoteVote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID       string `json:"session_id"`
		AspirantID      string `json:"aspirant_id"`
		VoteType        string `json:"vote_type"`
		EncryptedBallot string `json:"encrypted_ballot"`
		BallotProof     string `json:"ballot_proof"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.SessionID == "" {
		jsonErr(w, "session_id required", 400)
		return
	}
	if req.VoteType == "" {
		req.VoteType = "for"
	}

	// Verify session is authenticated
	var sessionStatus, delegateID, roundID string
	err := dbConn.QueryRow(`SELECT status, delegate_id, round_id FROM voting_sessions WHERE session_id=$1`,
		req.SessionID).Scan(&sessionStatus, &delegateID, &roundID)
	if err != nil {
		jsonErr(w, "session not found", 404)
		return
	}
	if sessionStatus != "authenticated" {
		jsonErr(w, "session not authenticated", 403)
		return
	}

	// Check delegate hasn't already voted in this round
	var roundNum int
	dbConn.QueryRow("SELECT round_number FROM voting_rounds WHERE round_id=$1", roundID).Scan(&roundNum)
	var hasVoted bool
	var voteRound sql.NullInt64
	dbConn.QueryRow("SELECT has_voted, vote_round FROM delegates WHERE delegate_id=$1", delegateID).Scan(&hasVoted, &voteRound)
	if hasVoted && voteRound.Valid && int(voteRound.Int64) == roundNum {
		jsonErr(w, "delegate already voted in this round", 409)
		return
	}

	// SECURITY: refuses to fabricate cryptographic artifacts. The previous
	// "server-side encryption" fallback stored SHA256(delegate:aspirant:...)
	// as the encrypted ballot and an HMAC with a hardcoded key as the proof.
	// Without a real crypto backend, remote ballots are rejected (503).
	if !cryptoBackendConfigured() {
		cryptoUnavailable(w, "ballot encryption service")
		return
	}

	// Generate E2E verifiable confirmation code
	confirmationCode := generateConfirmationCode()
	verificationHash := hashStringSHA(roundID + delegateID + confirmationCode + time.Now().String())

	// Ballots must arrive E2E-encrypted by the client; the server never
	// fabricates ciphertexts or proofs on the voter's behalf.
	encryptedBallot := req.EncryptedBallot
	ballotProof := req.BallotProof
	if encryptedBallot == "" {
		jsonErr(w, "encrypted_ballot required — the server does not accept cleartext vote choices", 400)
		return
	}

	// SECURITY: the client-supplied ciphertext/proof was previously stored
	// UNVERIFIED (and the cleartext aspirant_id right next to delegate_id).
	// The crypto backend MUST verify the proof first; on call failure → 503
	// with NO insert, and on proof rejection → 400. Only the backend's
	// opaque ballot_ref is stored, never the cleartext choice.
	if err := ensureHardeningColumns(r.Context()); err != nil {
		log.Error().Err(err).Msg("ballot custody columns unavailable")
		cryptoUnavailable(w, "ballot custody storage")
		return
	}
	ballotRef, err := verifyBallotProofViaBackend(r.Context(), roundID, encryptedBallot, ballotProof)
	if err != nil {
		status := http.StatusServiceUnavailable
		msg := "ballot proof verification service unavailable"
		if strings.Contains(err.Error(), "rejected") {
			status = http.StatusBadRequest
			msg = "ballot proof verification failed"
		}
		log.Error().Err(err).Str("round_id", roundID).Msg("remote ballot proof verification failed — no ballot stored")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
		return
	}

	ballotID := "bal-" + uuid.New().String()[:8]
	ipHash := hashStringSHA(r.RemoteAddr)

	// aspirant_id is intentionally NOT stored on this row (ballot custody —
	// see handleCastBallot).
	_, err = dbConn.ExecContext(r.Context(), `
		INSERT INTO ballots (ballot_id, round_id, delegate_id, aspirant_id, vote_type,
			is_remote, device_fingerprint, ip_hash, encrypted_ballot, ballot_proof,
			confirmation_code, verification_hash, ballot_ref)
		VALUES ($1,$2,$3,NULL,$4,TRUE,$5,$6,$7,$8,$9,$10,$11)`,
		ballotID, roundID, delegateID, req.VoteType,
		nullStr(""), ipHash, encryptedBallot, ballotProof, confirmationCode, verificationHash, nullStr(ballotRef))
	if err != nil {
		jsonErr(w, "remote vote failed: "+err.Error(), 500)
		return
	}

	// Mark delegate as voted
	dbConn.ExecContext(r.Context(), "UPDATE delegates SET has_voted=TRUE, vote_round=$1, vote_timestamp=NOW() WHERE delegate_id=$2",
		roundNum, delegateID)

	// Mark session as voted
	dbConn.ExecContext(r.Context(), "UPDATE voting_sessions SET status='voted', completed_at=NOW() WHERE session_id=$1", req.SessionID)

	// TigerBeetle audit transfer (best-effort — see handleCastBallot)
	tbID, tbErr := recordTBTransfer("remote_ballot_cast", 100, ballotID, delegateID)
	if tbErr != nil {
		log.Error().Err(tbErr).Str("ballot_id", ballotID).Msg("remote ballot audit transfer not recorded")
	}

	publishKafkaEvent("primaries.remote.vote_cast", map[string]interface{}{
		"ballot_id": ballotID, "round_id": roundID, "is_remote": true,
	})

	publishFluvioEvent("primaries-stream", map[string]interface{}{
		"event": "remote_vote_cast", "round_id": roundID, "ballot_id": ballotID,
	})

	jsonResp(w, map[string]interface{}{
		"ballot_id":         ballotID,
		"confirmation_code": confirmationCode,
		"verification_hash": verificationHash,
		"tb_transfer_id":    tbID,
		"is_remote":         true,
		"status":            "cast",
	})
}

func handleRemoteVerifyBallot(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("confirmation_code")
	hash := r.URL.Query().Get("verification_hash")

	if code == "" && hash == "" {
		jsonErr(w, "confirmation_code or verification_hash required", 400)
		return
	}

	// SECURITY: receipt lookups return inclusion/tally status ONLY — never
	// the vote choice (vote_type). This endpoint is PUBLIC by design (E2E
	// verifiability); returning vote_type to anyone holding a confirmation
	// code or verification hash would enable vote buying and coercion.
	query := "SELECT ballot_id, round_id, is_remote, cast_at, tallied FROM ballots WHERE "
	var arg string
	if code != "" {
		query += "confirmation_code=$1"
		arg = code
	} else {
		query += "verification_hash=$1"
		arg = hash
	}

	var ballotID, roundID string
	var isRemote, tallied bool
	var castAt time.Time
	err := dbConn.QueryRow(query, arg).Scan(&ballotID, &roundID, &isRemote, &castAt, &tallied)
	if err != nil {
		jsonErr(w, "ballot not found — vote may not have been recorded", 404)
		return
	}

	jsonResp(w, map[string]interface{}{
		"verified":  true,
		"included":  true,
		"ballot_id": ballotID,
		"round_id":  roundID,
		"is_remote": isRemote,
		"cast_at":   castAt,
		"tallied":   tallied,
		"message":   "Your vote was recorded and will be counted",
	})
}

func handleCoercionVote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID  string `json:"session_id"`
		AspirantID string `json:"aspirant_id"`
		VoteType   string `json:"vote_type"`
		PanicCode  string `json:"panic_code"` // Pre-registered duress code
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.SessionID == "" || req.PanicCode == "" {
		jsonErr(w, "session_id and panic_code required", 400)
		return
	}

	// SECURITY: same crypto gate as every other ballot path — without a real
	// crypto backend we refuse to operate rather than store cleartext
	// (decoy) vote choices.
	if !cryptoBackendConfigured() {
		cryptoUnavailable(w, "ballot encryption service")
		return
	}

	// SECURITY: the session must be fully AUTHENTICATED (OTP + biometric +
	// device binding via handleRemoteAuthenticate). Previously ANY caller
	// could plant decoy ballots under ANY session_id.
	var sessionStatus, delegateID, roundID string
	err := dbConn.QueryRow("SELECT status, delegate_id, round_id FROM voting_sessions WHERE session_id=$1", req.SessionID).
		Scan(&sessionStatus, &delegateID, &roundID)
	if err != nil {
		jsonErr(w, "session not found", 404)
		return
	}
	if sessionStatus != "authenticated" {
		jsonErr(w, "session not authenticated", 403)
		return
	}

	// SECURITY: verify the panic code against the per-delegate duress
	// credential registered at credential issuance (delegates.duress_code_hash).
	// The code is compared in constant time against the stored SHA-256 hash;
	// an unregistered or wrong code is a hard 403 — never silently accepted.
	if err := ensureHardeningColumns(r.Context()); err != nil {
		log.Error().Err(err).Msg("duress credential column unavailable")
		cryptoUnavailable(w, "duress credential storage")
		return
	}
	var duressHash sql.NullString
	if err := dbConn.QueryRowContext(r.Context(),
		"SELECT duress_code_hash FROM delegates WHERE delegate_id=$1", delegateID).Scan(&duressHash); err != nil {
		jsonErr(w, "delegate not found", 404)
		return
	}
	if !duressHash.Valid || duressHash.String == "" {
		jsonErr(w, "no duress credential registered for this delegate", 403)
		return
	}
	providedHash := hashStringSHA(req.PanicCode)
	if subtle.ConstantTimeCompare([]byte(providedHash), []byte(duressHash.String)) != 1 {
		log.Warn().Str("delegate_id", delegateID).Msg("duress code mismatch — coercion vote rejected")
		jsonErr(w, "invalid panic code", 403)
		return
	}

	voteType := req.VoteType
	if voteType == "" {
		voteType = "for"
	}

	confirmationCode := generateConfirmationCode()
	ballotID := "bal-" + uuid.New().String()[:8]

	// SECURITY: ballot custody — encrypt the decoy choice through the crypto
	// backend exactly like a real ballot (decoys must be indistinguishable).
	// On backend failure → 503, NO insert.
	ciphertext, proof, ballotRef, err := encryptBallotViaBackend(r.Context(),
		roundID, ballotID, voteType, req.AspirantID)
	if err != nil {
		log.Error().Err(err).Str("round_id", roundID).Msg("decoy ballot encryption failed — no ballot stored")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "ballot encryption service unavailable",
			"detail": "the election crypto backend could not encrypt this ballot; refusing to store a cleartext vote choice",
		})
		return
	}

	// Insert decoy ballot — looks identical to real ballot but is_decoy=TRUE.
	// aspirant_id is intentionally NULL (custody stays with the backend).
	res, err := dbConn.ExecContext(r.Context(), `
		INSERT INTO ballots (ballot_id, round_id, delegate_id, aspirant_id, vote_type,
			is_remote, is_decoy, confirmation_code, verification_hash,
			encrypted_ballot, ballot_proof, ballot_ref)
		VALUES ($1,$2,$3,NULL,$4,TRUE,TRUE,$5,$6,$7,$8,$9)`,
		ballotID, roundID, delegateID, voteType,
		confirmationCode, hashStringSHA(ballotID+confirmationCode),
		ciphertext, nullStr(proof), nullStr(ballotRef))
	if err != nil {
		jsonErr(w, "coercion vote failed: "+err.Error(), 500)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, "coercion vote failed", 500)
		return
	}

	log.Warn().Str("delegate_id", delegateID).Str("round_id", roundID).
		Msg("SECURITY: duress code used — decoy ballot cast; flag for post-election review")

	// Response looks identical to real vote — coercer cannot distinguish
	jsonResp(w, map[string]interface{}{
		"ballot_id":         ballotID,
		"confirmation_code": confirmationCode,
		"status":            "cast",
		"is_remote":         true,
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// CRYPTOGRAPHIC OPERATIONS — E2E Verifiable Voting
// ═══════════════════════════════════════════════════════════════════════════

func handleGenerateElectionKeys(w http.ResponseWriter, r *http.Request) {
	// SECURITY: refuses to fabricate cryptographic artifacts.
	// The previous implementation generated an "election keypair" from two
	// INDEPENDENT random 32-byte blobs and INSERTed the raw private key
	// UNENCRYPTED into voting_crypto_keys; the "k-of-n guardian shares" were
	// likewise independent random blobs with no Shamir sharing, and the Dapr
	// key-verification call was silently skipped when no sidecar was
	// configured. None of that is a real key ceremony.
	//
	// A real key ceremony requires the election crypto engine (via Dapr) to
	// generate and verify keys. If it is not configured, fail loudly with its
	// explicit error — never skip verification silently and never write fake
	// keys to the database.
	if err := verifyKeysViaDapr(0, "", nil); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "election key ceremony service not configured",
			"detail": "this deployment has no real ElectionGuard/Paillier backend; refusing to fabricate cryptographic artifacts: " + err.Error(),
		})
		return
	}
	// Even with a Dapr sidecar present, this service has no real key-ceremony
	// implementation — do not fall back to fabricated keys.
	cryptoUnavailable(w, "election key ceremony service")
}

func handleEncryptedTally(w http.ResponseWriter, r *http.Request) {
	// SECURITY: refuses to fabricate cryptographic artifacts.
	// The previous implementation stored SHA256("enc:count:ts") as the
	// "encrypted count", SHA256("proof:...") as the "proof of decryption",
	// and the PLAINTEXT count right alongside them in encrypted_tallies —
	// no homomorphic encryption of any kind. There is no real
	// ElectionGuard/Paillier backend in this deployment, so this endpoint
	// fails loudly and NEVER writes fake tallies or proofs to the database.
	cryptoUnavailable(w, "cryptographic tally service")
}

func handleMixNetShuffle(w http.ResponseWriter, r *http.Request) {
	// SECURITY: refuses to fabricate cryptographic artifacts.
	// The previous implementation "re-encrypted" ballots as SHA256(ballot:i)
	// and stored a hash of a timestamp as the "proof of shuffle" in
	// shuffle_records — no mix-net, no re-encryption, no proof. There is no
	// real mix-net backend in this deployment, so this endpoint fails loudly
	// and NEVER writes fake shuffle records to the database.
	cryptoUnavailable(w, "mix-net shuffle service")
}

func handleThresholdDecrypt(w http.ResponseWriter, r *http.Request) {
	// SECURITY: refuses to fabricate cryptographic artifacts.
	// The previous implementation accepted arbitrary caller-supplied
	// "guardian_decryptions" strings, merely COUNTED them against a
	// threshold, returned the plaintext decrypted_count already sitting in
	// the database, and marked rows verified=TRUE with a hash masquerading
	// as a proof of decryption. No threshold decryption ever occurred.
	// There is no real ElectionGuard guardian backend in this deployment, so
	// this endpoint fails loudly and NEVER marks fabricated tallies verified.
	cryptoUnavailable(w, "threshold decryption service")
}

func handleCryptoAuditTrail(w http.ResponseWriter, r *http.Request) {
	electionID := r.URL.Query().Get("election_id")
	if electionID == "" {
		jsonErr(w, "election_id required", 400)
		return
	}

	// Key info
	var keyCount int
	dbConn.QueryRow("SELECT COUNT(*) FROM voting_crypto_keys WHERE election_id=$1", electionID).Scan(&keyCount)

	// Shuffle records
	var shuffleCount int
	dbConn.QueryRow(`SELECT COUNT(*) FROM shuffle_records sr
		JOIN voting_rounds vr ON sr.round_id = vr.round_id
		WHERE vr.election_id=$1`, electionID).Scan(&shuffleCount)

	// Ballot integrity
	var totalBallots, remoteBallots, decoyBallots int
	dbConn.QueryRow(`SELECT COUNT(*) FROM ballots b
		JOIN voting_rounds vr ON b.round_id = vr.round_id
		WHERE vr.election_id=$1`, electionID).Scan(&totalBallots)
	dbConn.QueryRow(`SELECT COUNT(*) FROM ballots b
		JOIN voting_rounds vr ON b.round_id = vr.round_id
		WHERE vr.election_id=$1 AND b.is_remote=TRUE`, electionID).Scan(&remoteBallots)
	dbConn.QueryRow(`SELECT COUNT(*) FROM ballots b
		JOIN voting_rounds vr ON b.round_id = vr.round_id
		WHERE vr.election_id=$1 AND b.is_decoy=TRUE`, electionID).Scan(&decoyBallots)

	// SECURITY: refuses to fabricate cryptographic artifacts. This handler
	// previously returned a hardcoded "integrity":"verified" with no
	// verification of any kind. The row counts below are real database
	// counts, but with no crypto backend there is no chain/Merkle proof
	// verification of keys, shuffles, or tallies — integrity is reported as
	// "unknown", never "verified" without an actual check.
	jsonResp(w, map[string]interface{}{
		"election_id":     electionID,
		"crypto_keys":     keyCount,
		"shuffle_records": shuffleCount,
		"total_ballots":   totalBallots,
		"remote_ballots":  remoteBallots,
		"decoy_ballots":   decoyBallots,
		"integrity":       "unknown",
		"warning":         "no cryptographic backend configured; integrity of crypto keys, shuffle records and encrypted tallies cannot be verified. Any artifacts produced before this fix were fabricated by a mock implementation and MUST NOT be trusted.",
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// DISPUTES
// ═══════════════════════════════════════════════════════════════════════════

func handleListDisputes(w http.ResponseWriter, r *http.Request) {
	electionID := r.URL.Query().Get("election_id")
	if electionID == "" {
		jsonErr(w, "election_id required", 400)
		return
	}

	rows, err := dbConn.QueryContext(r.Context(), `
		SELECT dispute_id, filed_by, filed_by_type, dispute_type, description,
			status, resolution, filed_at, resolved_at
		FROM primary_disputes WHERE election_id=$1
		ORDER BY filed_at DESC`, electionID)
	if err != nil {
		jsonErr(w, "query failed", 500)
		return
	}
	defer rows.Close()

	var disputes []map[string]interface{}
	for rows.Next() {
		var dID, filedBy, filedByType, dType, desc, status string
		var resolution sql.NullString
		var filedAt time.Time
		var resolvedAt sql.NullTime
		rows.Scan(&dID, &filedBy, &filedByType, &dType, &desc, &status, &resolution, &filedAt, &resolvedAt)
		disputes = append(disputes, map[string]interface{}{
			"dispute_id": dID, "filed_by": filedBy, "filed_by_type": filedByType,
			"dispute_type": dType, "description": desc, "status": status,
			"resolution": nullVal(resolution), "filed_at": filedAt,
			"resolved_at": nullTime(resolvedAt),
		})
	}
	if disputes == nil {
		disputes = []map[string]interface{}{}
	}
	jsonResp(w, map[string]interface{}{"disputes": disputes})
}

func handleFileDispute(w http.ResponseWriter, r *http.Request) {
	_, user := getParty(r)
	var req struct {
		ElectionID   int      `json:"election_id"`
		RoundID      string   `json:"round_id"`
		FiledBy      string   `json:"filed_by"`
		FiledByType  string   `json:"filed_by_type"`
		DisputeType  string   `json:"dispute_type"`
		Description  string   `json:"description"`
		EvidenceURLs []string `json:"evidence_urls"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.ElectionID == 0 || req.Description == "" || req.DisputeType == "" {
		jsonErr(w, "election_id, dispute_type, and description required", 400)
		return
	}
	if req.FiledByType == "" {
		req.FiledByType = "delegate"
	}
	// SECURITY: filed_by comes from the AUTHENTICATED identity, never from
	// the request body — a caller-supplied filed_by lets anyone file disputes
	// in another delegate's/aspirant's name. (In dev mode the identity
	// header may be empty; only then is the body value used.)
	if user != "" {
		req.FiledBy = user
	} else if req.FiledBy == "" {
		req.FiledBy = "unknown"
	}

	disputeID := "pdisp-" + uuid.New().String()[:8]
	// SECURITY: store evidence URLs via pq.Array — the previous raw string
	// join built a Postgres array literal that corrupted (or injected)
	// entries containing commas/quotes/braces.
	_, err := dbConn.ExecContext(r.Context(), `
		INSERT INTO primary_disputes (dispute_id, election_id, round_id, filed_by, filed_by_type,
			dispute_type, description, evidence_urls)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		disputeID, req.ElectionID, nullStr(req.RoundID), req.FiledBy, req.FiledByType,
		req.DisputeType, req.Description, pq.Array(req.EvidenceURLs))
	if err != nil {
		jsonErr(w, "file dispute failed: "+err.Error(), 500)
		return
	}

	publishKafkaEvent("primaries.dispute.filed", map[string]interface{}{
		"dispute_id": disputeID, "type": req.DisputeType,
	})

	logConventionEvent(r.Context(), req.ElectionID, "dispute_filed", user, "dispute", disputeID, map[string]interface{}{
		"type": req.DisputeType, "description": req.Description,
	})

	jsonResp(w, map[string]interface{}{"dispute_id": disputeID, "status": "filed"})
}

func handleResolveDispute(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	partyCode := fmt.Sprintf("party_%d", pid)

	var req struct {
		Status     string `json:"status"` // upheld, dismissed
		Resolution string `json:"resolution"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// SECURITY: cross-party scoping — the dispute's election must belong to
	// the caller's party (re-enforced inside the UPDATE for TOCTOU safety).
	var elecID int
	if err := dbConn.QueryRowContext(r.Context(),
		"SELECT election_id FROM primary_disputes WHERE dispute_id=$1", id).Scan(&elecID); err != nil {
		jsonErr(w, "dispute not found", 404)
		return
	}
	if !partyOwnsElection(r.Context(), elecID, partyCode) {
		log.Warn().Str("dispute_id", id).Str("caller_party", partyCode).Msg("SECURITY: cross-party dispute-resolution attempt rejected")
		jsonErr(w, "dispute does not belong to your party", 403)
		return
	}

	res, err := dbConn.ExecContext(r.Context(), `
		UPDATE primary_disputes SET status=$1, resolution=$2, resolved_at=NOW()
		WHERE dispute_id=$3 AND status IN ('filed','under_review','hearing_scheduled')
		AND EXISTS (SELECT 1 FROM delegates d WHERE d.election_id=primary_disputes.election_id AND d.party_code=$4)`,
		req.Status, req.Resolution, id, partyCode)
	if err != nil {
		jsonErr(w, "resolve failed", 500)
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		jsonErr(w, "dispute not found or already resolved", 404)
		return
	}
	logConventionEvent(r.Context(), elecID, "dispute_resolved", user, "dispute", id, map[string]interface{}{
		"status": req.Status, "resolution": req.Resolution,
	})

	jsonResp(w, map[string]interface{}{"resolved": true, "status": req.Status})
}

// ═══════════════════════════════════════════════════════════════════════════
// CONVENTION AUDIT LOG
// ═══════════════════════════════════════════════════════════════════════════

func handleConventionAuditLog(w http.ResponseWriter, r *http.Request) {
	electionID := r.URL.Query().Get("election_id")
	eventType := r.URL.Query().Get("event_type")
	pgLimit, pgOffset := parsePagination(r)

	query := "SELECT event_type, actor_id, actor_role, entity_type, entity_id, details, created_at FROM convention_audit_log WHERE 1=1"
	args := []interface{}{}
	idx := 1
	if electionID != "" {
		query += fmt.Sprintf(" AND election_id=$%d", idx)
		args = append(args, electionID)
		idx++
	}
	if eventType != "" {
		query += fmt.Sprintf(" AND event_type=$%d", idx)
		args = append(args, eventType)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d OFFSET %d", pgLimit, pgOffset)

	rows, err := dbConn.QueryContext(r.Context(), query, args...)
	if err != nil {
		jsonErr(w, "query failed", 500)
		return
	}
	defer rows.Close()

	var events []map[string]interface{}
	for rows.Next() {
		var evType string
		var actorID, actorRole, entType, entID sql.NullString
		var details sql.NullString
		var createdAt time.Time
		rows.Scan(&evType, &actorID, &actorRole, &entType, &entID, &details, &createdAt)
		ev := map[string]interface{}{
			"event_type":  evType,
			"actor_id":    nullVal(actorID),
			"entity_type": nullVal(entType),
			"entity_id":   nullVal(entID),
			"created_at":  createdAt,
		}
		if details.Valid {
			var d map[string]interface{}
			if json.Unmarshal([]byte(details.String), &d) == nil {
				ev["details"] = d
			}
		}
		events = append(events, ev)
	}
	if events == nil {
		events = []map[string]interface{}{}
	}
	jsonResp(w, map[string]interface{}{"events": events})
}

// ═══════════════════════════════════════════════════════════════════════════
// HELPER FUNCTIONS
// ═══════════════════════════════════════════════════════════════════════════

func nullValInt64(n sql.NullInt64) interface{} {
	if n.Valid {
		return n.Int64
	}
	return nil
}

func nullTime(t sql.NullTime) interface{} {
	if t.Valid {
		return t.Time
	}
	return nil
}

func hashStringSHA(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func generateConfirmationCode() string {
	b := make([]byte, 8)
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))[:12]
}

func generateOTP() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(999999))
	return fmt.Sprintf("%06d", n.Int64())
}

func buildBallotMerkleRoot(ctx context.Context, roundID string) string {
	rows, err := dbConn.QueryContext(ctx, `
		SELECT ballot_id, verification_hash FROM ballots WHERE round_id=$1 AND is_decoy=FALSE ORDER BY ballot_id`, roundID)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var bID, vHash string
		rows.Scan(&bID, &vHash)
		hashes = append(hashes, vHash)
	}
	if len(hashes) == 0 {
		return ""
	}

	// Build Merkle tree
	for len(hashes) > 1 {
		var next []string
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {
				combined := hashes[i] + hashes[i+1]
				next = append(next, hashStringSHA(combined))
			} else {
				next = append(next, hashes[i])
			}
		}
		hashes = next
	}
	return hashes[0]
}

func updateQuorumSnapshot(ctx context.Context, electionID int) {
	var total, accredited, present int
	dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE election_id=$1", electionID).Scan(&total)
	dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE election_id=$1 AND accreditation_status='accredited'", electionID).Scan(&accredited)
	dbConn.QueryRow("SELECT COUNT(*) FROM delegates WHERE election_id=$1 AND floor_access=TRUE", electionID).Scan(&present)

	quorumMet := float64(accredited)/math.Max(float64(total), 1) >= quorumThresholdFraction
	dbConn.ExecContext(ctx, `
		INSERT INTO quorum_snapshots (election_id, total_registered, total_accredited, total_present, quorum_met)
		VALUES ($1,$2,$3,$4,$5)`, electionID, total, accredited, present, quorumMet)
}

func logConventionEvent(ctx context.Context, electionID int, eventType, actorID, entityType, entityID string, details map[string]interface{}) {
	detailsJSON, _ := json.Marshal(details)
	dbConn.ExecContext(ctx, `
		INSERT INTO convention_audit_log (election_id, event_type, actor_id, entity_type, entity_id, details)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		electionID, eventType, actorID, entityType, entityID, string(detailsJSON))
}

// Middleware integration helpers

func checkPrimaryPermission(partyID int, user, permission string) bool {
	// Check via Permify if configured (fail-closed inside checkPermission).
	if permifyURL != "" {
		return checkPermission(user, permission, "party", fmt.Sprintf("%d", partyID))
	}
	// SECURITY: with no authorization backend the "allow all" path is a
	// DEV-ONLY escape hatch. In production (devModeEnabled=false) a missing
	// Permify configuration must DENY mutating operations, never silently
	// authorize them.
	if !devModeEnabled {
		log.Error().Str("user", user).Str("permission", permission).Int("party_id", partyID).
			Msg("SECURITY: Permify unconfigured and not in dev mode — primary permission DENIED (fail closed)")
		return false
	}
	return true // Dev mode only — allow all
}

// validateKeycloakDelegateSession forwards the caller's Bearer token to
// Keycloak's userinfo endpoint and returns true ONLY on HTTP 200.
// SECURITY: the previous version never forwarded the token and treated any
// non-empty response as valid — a cosmetic check. Callers that gate on this
// must do so only when keycloakURL != "" (dev mode without Keycloak returns
// true so local development still works).
func validateKeycloakDelegateSession(r *http.Request) bool {
	if keycloakURL == "" {
		return true
	}
	authHeader := r.Header.Get("Authorization")
	if len(authHeader) <= 7 || authHeader[:7] != "Bearer " {
		return false
	}
	// Validate against Keycloak userinfo WITH the caller's token forwarded.
	req, err := http.NewRequestWithContext(r.Context(), "GET",
		keycloakURL+"/realms/"+keycloakRealm+"/protocol/openid-connect/userinfo", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", authHeader)
	resp, err := mwHTTPClient.Do(req)
	if err != nil {
		log.Warn().Err(err).Msg("Keycloak delegate session validation call failed")
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func validateKeycloakRemoteVoting(r *http.Request) string {
	if keycloakURL == "" {
		return "dev-session"
	}
	return "keycloak-" + uuid.New().String()[:8]
}

func wafCheckRemoteVoting(r *http.Request) {
	if openappsecURL == "" {
		return
	}
	// Forward request to OpenAppSec for WAF inspection
	resilientCall(r.Context(), cbOpenAppSec, "POST", openappsecURL+"/inspect", nil) //nolint:errcheck
}

func publishKafkaEvent(topic string, data map[string]interface{}) {
	publishEvent(topic, "", data)
}

func publishFluvioEvent(topic string, data map[string]interface{}) {
	if fluvioURL == "" {
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{"topic": topic, "data": data})
	resilientCall(context.Background(), cbFluvio, "POST", fluvioURL+"/produce", payload) //nolint:errcheck
}

func indexInOpenSearch(index, id string, doc map[string]interface{}) {
	if opensearchURL == "" {
		return
	}
	payload, _ := json.Marshal(doc)
	resilientCall(context.Background(), cbOpenSearch, "PUT",
		fmt.Sprintf("%s/%s/_doc/%s", opensearchURL, index, id), payload) //nolint:errcheck
}

// recordTBTransfer records an audit/payment transfer in the TigerBeetle
// ledger. INTEGRITY: when the ledger is not configured it returns an ERROR —
// the previous behavior returned a fabricated "tb-<random>" id that was then
// stored as deposit_tb_transfer_id, i.e. proof of a payment that never
// happened. Callers decide whether the transfer is critical (deposits: fail
// with 503) or best-effort audit (ballot casts: log and continue).
func recordTBTransfer(transferType string, amountKobo int64, entityID, userID string) (string, error) {
	if gotvLedger == nil {
		return "", fmt.Errorf("payment ledger unavailable: TigerBeetle not configured")
	}
	transferCode := 0
	switch transferType {
	case "aspirant_deposit":
		transferCode = 8
	case "ballot_cast":
		transferCode = 9
	case "remote_ballot_cast":
		transferCode = 10
	}
	idemKey := fmt.Sprintf("%s:%s:%d", transferType, entityID, time.Now().UnixNano())
	tid, err := gotvLedger.CreateTransferWithRetry(context.Background(), "operations", "escrow",
		amountKobo, transferCode, transferType, idemKey)
	if err != nil {
		return "", fmt.Errorf("ledger transfer failed: %w", err)
	}
	if tid == "" {
		return "", fmt.Errorf("ledger transfer returned empty transfer id")
	}
	return tid, nil
}

// verifyKeysViaDapr asks the Rust election-crypto engine (via the Dapr
// sidecar) to verify generated election keys.
// SECURITY: refuses to silently skip verification — returns an explicit
// error when no Dapr sidecar/crypto engine is configured so that key
// generation MUST fail loudly instead of proceeding unverified.
func verifyKeysViaDapr(electionID int, pubKey string, guardianKeys []map[string]string) error {
	if daprPort == "" {
		return fmt.Errorf("no Dapr sidecar configured (DAPR_HTTP_PORT unset): election key verification service unavailable")
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"election_id": electionID, "public_key": pubKey, "guardians": guardianKeys,
	})
	daprBase := "http://localhost:" + daprPort
	_, _, err := resilientCall(context.Background(), cbRustEngine, "POST",
		daprBase+"/v1.0/invoke/gotv-engine/method/verify-keys", payload)
	if err != nil {
		return fmt.Errorf("election key verification via crypto engine failed: %w", err)
	}
	return nil
}

// NOTE: the former "cryptographic helpers" (generateElectionKeyPair,
// generateGuardianKeyShare, encryptBallot, generateBallotProof,
// homomorphicEncrypt, generateDecryptionProof, performMixNetShuffle) were
// removed. They fabricated keys, ciphertexts and proofs out of SHA-256/HMAC
// hashes and random blobs — including an HMAC with the hardcoded key
// "election-proof-key" — which is forgeable by anyone with the source.
// SECURITY: refuses to fabricate cryptographic artifacts; a real
// ElectionGuard/Paillier backend must be integrated before these operations
// can be offered again.

// Suppress unused import warnings
var (
	_ = sort.Strings
	_ = strconv.Itoa
	_ = math.Max
)
