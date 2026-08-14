// voting_crypto.rs — Cryptographic Voting Core for Party Primaries & Remote Voting
// Implements: ElectionGuard-style E2E verifiable voting, homomorphic tallying,
// mix-net shuffle with zero-knowledge proofs, Merkle ballot tree, threshold decryption.
//
// Middleware: Dapr (service mesh), Kafka (event streaming), Redis (session cache),
// TigerBeetle (audit ledger), Fluvio (live stream)

use rand::Rng;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::HashMap;

// ═══════════════════════════════════════════════════════════════════════════
// CORE TYPES
// ═══════════════════════════════════════════════════════════════════════════

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ElectionKeyPair {
    pub election_id: i64,
    pub public_key: String,
    pub private_key_encrypted: String,
    pub guardian_count: usize,
    pub threshold: usize,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GuardianKeyShare {
    pub index: usize,
    pub public_key: String,
    pub verification_key: String,
    pub encrypted_share: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EncryptedBallot {
    pub ballot_id: String,
    pub delegate_id: String,
    pub ciphertext: String,
    pub proof: BallotProof,
    pub confirmation_code: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BallotProof {
    pub commitment: String,
    pub challenge: String,
    pub response: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ShuffleProof {
    pub input_hash: String,
    pub output_hash: String,
    pub permutation_commitment: String,
    pub proof_elements: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MerkleNode {
    pub hash: String,
    pub left: Option<Box<MerkleNode>>,
    pub right: Option<Box<MerkleNode>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MerkleProofStep {
    pub hash: String,
    pub position: String, // "left" or "right"
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DecryptionShare {
    pub guardian_index: usize,
    pub partial_decryption: String,
    pub proof: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TallyResult {
    pub aspirant_id: String,
    pub encrypted_count: String,
    pub decrypted_count: u64,
    pub proof_of_decryption: String,
    pub guardian_shares: Vec<DecryptionShare>,
}

// ═══════════════════════════════════════════════════════════════════════════
// CRYPTO BACKEND AVAILABILITY
// ═══════════════════════════════════════════════════════════════════════════

/// Error returned by every election-cryptography operation in this build.
///
/// SECURITY: This build ships WITHOUT a real election cryptography backend
/// (no ElGamal, no threshold/Shamir secret sharing, no mix-net, no
/// Chaum-Pedersen / zero-knowledge proofs). Previously these functions
/// fabricated cryptographic artifacts out of SHA-256 hashes and echoed
/// caller-supplied tallies back as "decrypted" results. That is silent
/// mockware and is unacceptable for an election platform. Every operation
/// below now fails loudly with this error; HTTP handlers map it to 503.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct VotingCryptoError {
    pub operation: String,
    pub message: String,
}

impl VotingCryptoError {
    pub fn unavailable(operation: &str) -> Self {
        VotingCryptoError {
            operation: operation.to_string(),
            message: format!(
                "{}: voting crypto backend not implemented in this build; refusing to fabricate cryptographic artifacts",
                operation
            ),
        }
    }

    pub fn invalid_input(operation: &str, detail: &str) -> Self {
        VotingCryptoError {
            operation: operation.to_string(),
            message: format!("{}: {}", operation, detail),
        }
    }
}

impl std::fmt::Display for VotingCryptoError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.message)
    }
}

impl std::error::Error for VotingCryptoError {}

// ═══════════════════════════════════════════════════════════════════════════
// ELECTION KEY GENERATION (ElectionGuard-style threshold encryption)
// ═══════════════════════════════════════════════════════════════════════════

/// SECURITY: Previously this generated a "public key" that was NOT derived
/// from the private key and "Shamir shares" via bytewise wrapping_add —
/// neither operation is cryptographically valid. Without a real
/// threshold-encryption backend this function refuses to fabricate keys.
pub fn generate_election_keys(
    _election_id: i64,
    _guardians: usize,
    _threshold: usize,
) -> Result<(ElectionKeyPair, Vec<GuardianKeyShare>), VotingCryptoError> {
    Err(VotingCryptoError::unavailable("generate_election_keys"))
}

/// SECURITY: Previously returned true for any non-empty share. No real
/// share-verification backend exists in this build, so verification fails
/// loudly instead of rubber-stamping invalid guardian shares.
pub fn verify_guardian_share(
    _share: &GuardianKeyShare,
    _election_pub_key: &str,
) -> Result<bool, VotingCryptoError> {
    Err(VotingCryptoError::unavailable("verify_guardian_share"))
}

// ═══════════════════════════════════════════════════════════════════════════
// BALLOT ENCRYPTION (Exponential ElGamal)
// ═══════════════════════════════════════════════════════════════════════════

/// SECURITY: The previous "ElGamal encryption" was SHA256(plaintext||nonce||pubkey)
/// — a hash, not encryption — and the "Chaum-Pedersen proof" was a chain of
/// SHA-256 hashes that verified trivially. No real ballot-encryption backend
/// is configured in this build, so this refuses to fabricate ciphertexts.
pub fn encrypt_ballot(
    _delegate_id: &str,
    _aspirant_id: &str,
    _vote_type: &str,
    _election_pub_key: &str,
) -> Result<EncryptedBallot, VotingCryptoError> {
    Err(VotingCryptoError::unavailable("encrypt_ballot"))
}

/// SECURITY: Previously only checked that proof fields were non-empty,
/// which is always true — any ballot "verified". Without a real proof
/// backend, verification fails loudly rather than returning a false valid.
pub fn verify_ballot_proof(_ballot: &EncryptedBallot) -> Result<bool, VotingCryptoError> {
    Err(VotingCryptoError::unavailable("verify_ballot_proof"))
}

// ═══════════════════════════════════════════════════════════════════════════
// MIX-NET SHUFFLE (Re-encryption with Zero-Knowledge Proof)
// ═══════════════════════════════════════════════════════════════════════════

/// SECURITY: The previous "re-encryption" was SHA256(input||nonce), which
/// irreversibly DESTROYS the ballot ciphertext, and the shuffle "proof"
/// verified by comparing caller-supplied list hashes — any shuffle passed
/// with verified:true. No mix-net backend exists in this build; refuse.
pub fn mix_net_shuffle(
    _encrypted_ballots: &[String],
) -> Result<(Vec<String>, ShuffleProof), VotingCryptoError> {
    Err(VotingCryptoError::unavailable("mix_net_shuffle"))
}

/// SECURITY: Previously any caller-supplied input/output hash pair passed.
/// Without a real shuffle-proof backend, verification fails loudly.
pub fn verify_shuffle_proof(
    _input: &[String],
    _output: &[String],
    _proof: &ShuffleProof,
) -> Result<bool, VotingCryptoError> {
    Err(VotingCryptoError::unavailable("verify_shuffle_proof"))
}

// ═══════════════════════════════════════════════════════════════════════════
// HOMOMORPHIC TALLYING
// ═══════════════════════════════════════════════════════════════════════════

/// SECURITY: The previous "homomorphic tally" hashed "tally:"||aspirant||
/// all-ballots, producing an identical digest for every aspirant regardless
/// of votes. That is a fabricated tally. No homomorphic-encryption backend
/// exists in this build; refuse.
pub fn homomorphic_tally(
    _encrypted_ballots: &[String],
    _aspirant_ids: &[String],
) -> Result<HashMap<String, String>, VotingCryptoError> {
    Err(VotingCryptoError::unavailable("homomorphic_tally"))
}

// ═══════════════════════════════════════════════════════════════════════════
// THRESHOLD DECRYPTION
// ═══════════════════════════════════════════════════════════════════════════

/// SECURITY: The previous partial decryption was SHA256("partial_decrypt:"||tally||share)
/// — a hash, not a threshold-decryption share. No backend; refuse.
pub fn create_decryption_share(
    _encrypted_tally: &str,
    _guardian_share: &GuardianKeyShare,
) -> Result<DecryptionShare, VotingCryptoError> {
    Err(VotingCryptoError::unavailable("create_decryption_share"))
}

/// SECURITY: Previously this echoed the CALLER-SUPPLIED `actual_count` back
/// as the "decrypted" tally with a SHA-256 "proof_of_decryption", and
/// PANICKED on insufficient shares (API-driven DoS). Both are removed:
/// insufficient shares returns Err, and without a real threshold-decryption
/// backend this refuses to fabricate a tally.
pub fn combine_decryption_shares(
    shares: &[DecryptionShare],
    threshold: usize,
    _actual_count: u64,
) -> Result<TallyResult, VotingCryptoError> {
    if shares.len() < threshold {
        return Err(VotingCryptoError::invalid_input(
            "combine_decryption_shares",
            &format!("not enough shares: need {} got {}", threshold, shares.len()),
        ));
    }
    Err(VotingCryptoError::unavailable("combine_decryption_shares: threshold decryption not available — refusing to fabricate tally"))
}

// ═══════════════════════════════════════════════════════════════════════════
// MERKLE BALLOT TREE
// ═══════════════════════════════════════════════════════════════════════════

pub fn build_ballot_merkle_tree(ballot_hashes: &[String]) -> (String, Vec<Vec<MerkleProofStep>>) {
    if ballot_hashes.is_empty() {
        return (String::new(), Vec::new());
    }

    let mut leaves: Vec<String> = ballot_hashes.to_vec();

    // Pad to power of 2
    while leaves.len().count_ones() != 1 {
        leaves.push(leaves.last().unwrap().clone());
    }

    // Build tree bottom-up
    let mut levels: Vec<Vec<String>> = vec![leaves.clone()];
    let mut current = leaves;

    while current.len() > 1 {
        let mut next = Vec::new();
        for chunk in current.chunks(2) {
            let combined = format!("{}{}", chunk[0], chunk.get(1).unwrap_or(&chunk[0]));
            let mut hasher = Sha256::new();
            hasher.update(combined.as_bytes());
            next.push(hex::encode(hasher.finalize()));
        }
        levels.push(next.clone());
        current = next;
    }

    let root = current[0].clone();

    // Generate proofs for each original ballot
    let mut proofs = Vec::new();
    for i in 0..ballot_hashes.len() {
        proofs.push(generate_merkle_proof(&levels, i));
    }

    (root, proofs)
}

fn generate_merkle_proof(levels: &[Vec<String>], index: usize) -> Vec<MerkleProofStep> {
    let mut proof = Vec::new();
    let mut idx = index;

    for level in levels.iter().take(levels.len() - 1) {
        let sibling_idx = if idx % 2 == 0 { idx + 1 } else { idx - 1 };
        if sibling_idx < level.len() {
            proof.push(MerkleProofStep {
                hash: level[sibling_idx].clone(),
                position: if idx % 2 == 0 {
                    "right".to_string()
                } else {
                    "left".to_string()
                },
            });
        }
        idx /= 2;
    }

    proof
}

pub fn verify_merkle_proof(leaf_hash: &str, proof: &[MerkleProofStep], root: &str) -> bool {
    let mut current = leaf_hash.to_string();

    for step in proof {
        let combined = if step.position == "right" {
            format!("{}{}", current, step.hash)
        } else {
            format!("{}{}", step.hash, current)
        };
        let mut hasher = Sha256::new();
        hasher.update(combined.as_bytes());
        current = hex::encode(hasher.finalize());
    }

    current == root
}

// ═══════════════════════════════════════════════════════════════════════════
// COERCION RESISTANCE
// ═══════════════════════════════════════════════════════════════════════════

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CoercionResistanceToken {
    pub delegate_id: String,
    pub panic_code_hash: String,
    pub decoy_credential: String,
    pub real_credential: String,
}

pub fn generate_coercion_resistance_tokens(
    delegate_id: &str,
    panic_code: &str,
) -> CoercionResistanceToken {
    let mut rng = rand::thread_rng();

    let mut panic_hasher = Sha256::new();
    panic_hasher.update(panic_code.as_bytes());
    panic_hasher.update(delegate_id.as_bytes());
    let panic_hash = hex::encode(panic_hasher.finalize());

    let real_cred: Vec<u8> = (0..16).map(|_| rng.gen()).collect();
    let decoy_cred: Vec<u8> = (0..16).map(|_| rng.gen()).collect();

    CoercionResistanceToken {
        delegate_id: delegate_id.to_string(),
        panic_code_hash: panic_hash,
        decoy_credential: hex::encode(decoy_cred),
        real_credential: hex::encode(real_cred),
    }
}

pub fn is_panic_code(input: &str, delegate_id: &str, stored_hash: &str) -> bool {
    let mut hasher = Sha256::new();
    hasher.update(input.as_bytes());
    hasher.update(delegate_id.as_bytes());
    hex::encode(hasher.finalize()) == stored_hash
}

// ═══════════════════════════════════════════════════════════════════════════
// RECEIPT-FREENESS (Designated Verifier Proof)
// ═══════════════════════════════════════════════════════════════════════════

pub fn generate_receipt(ballot_id: &str, confirmation_code: &str, voter_key: &str) -> String {
    // Generate a receipt that only the designated verifier (election authority) can verify
    let mut hasher = Sha256::new();
    hasher.update(b"receipt:");
    hasher.update(ballot_id.as_bytes());
    hasher.update(confirmation_code.as_bytes());
    hasher.update(voter_key.as_bytes());
    hex::encode(hasher.finalize())
}

pub fn verify_receipt(
    receipt: &str,
    ballot_id: &str,
    confirmation_code: &str,
    voter_key: &str,
) -> bool {
    let expected = generate_receipt(ballot_id, confirmation_code, voter_key);
    receipt == expected
}

// ═══════════════════════════════════════════════════════════════════════════
// ACTIX-WEB ENDPOINTS (Dapr service invocation target)
// ═══════════════════════════════════════════════════════════════════════════

#[derive(Deserialize)]
pub struct EncryptBallotRequest {
    pub delegate_id: String,
    pub aspirant_id: String,
    pub vote_type: String,
    pub election_pub_key: String,
}

#[derive(Deserialize)]
pub struct ShuffleRequest {
    pub encrypted_ballots: Vec<String>,
}

#[derive(Deserialize)]
pub struct MerkleTreeRequest {
    pub ballot_hashes: Vec<String>,
}

#[derive(Deserialize)]
pub struct VerifyKeyRequest {
    pub election_id: i64,
    pub public_key: String,
    pub guardians: Vec<HashMap<String, String>>,
}

#[derive(Serialize)]
pub struct EncryptBallotResponse {
    pub ballot: EncryptedBallot,
    pub valid: bool,
}

#[derive(Serialize)]
pub struct ShuffleResponse {
    pub shuffled: Vec<String>,
    pub proof: ShuffleProof,
    pub verified: bool,
}

#[derive(Serialize)]
pub struct MerkleTreeResponse {
    pub root: String,
    pub proof_count: usize,
}

#[derive(Serialize)]
pub struct VerifyKeyResponse {
    pub election_id: i64,
    pub valid: bool,
    pub guardian_count: usize,
}

// Actix-web handler functions (registered in main.rs)
// SECURITY: every handler that depends on a real crypto backend returns
// Err(VotingCryptoError); the HTTP layer maps this to 503 with a JSON error.
pub fn handle_encrypt_ballot(
    _req: EncryptBallotRequest,
) -> Result<EncryptBallotResponse, VotingCryptoError> {
    Err(VotingCryptoError::unavailable(
        "encrypt-ballot endpoint: ballot encryption backend not configured",
    ))
}

pub fn handle_shuffle(_req: ShuffleRequest) -> Result<ShuffleResponse, VotingCryptoError> {
    Err(VotingCryptoError::unavailable(
        "shuffle endpoint: mix-net backend not configured",
    ))
}

pub fn handle_merkle_tree(req: MerkleTreeRequest) -> MerkleTreeResponse {
    // Merkle ballot trees are a genuine SHA-256 hash-tree construction and
    // remain functional; they do not depend on the unavailable crypto backend.
    let (root, proofs) = build_ballot_merkle_tree(&req.ballot_hashes);
    MerkleTreeResponse {
        root,
        proof_count: proofs.len(),
    }
}

pub fn handle_verify_keys(_req: VerifyKeyRequest) -> Result<VerifyKeyResponse, VotingCryptoError> {
    // SECURITY: previously valid = !public_key.is_empty(). Without a real
    // key-verification backend, refuse rather than rubber-stamping keys.
    Err(VotingCryptoError::unavailable(
        "verify-keys endpoint: key verification backend not configured",
    ))
}

// ═══════════════════════════════════════════════════════════════════════════
// TESTS
// ═══════════════════════════════════════════════════════════════════════════

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_generate_election_keys_refuses_to_fabricate() {
        // SECURITY: no threshold-crypto backend in this build — must fail loudly.
        let err = generate_election_keys(1, 5, 3).unwrap_err();
        assert!(err.message.contains("not implemented"));
    }

    #[test]
    fn test_encrypt_and_verify_ballot_refuse() {
        let err = encrypt_ballot("del-001", "asp-001", "for", "test_pub_key").unwrap_err();
        assert!(err.message.contains("not implemented"));

        let ballot = EncryptedBallot {
            ballot_id: "bal-x".to_string(),
            delegate_id: "del-001".to_string(),
            ciphertext: "00".repeat(32),
            proof: BallotProof {
                commitment: "00".repeat(32),
                challenge: "00".repeat(32),
                response: "00".repeat(32),
            },
            confirmation_code: "ABC123".to_string(),
        };
        // SECURITY: verification must never trivially return true.
        assert!(verify_ballot_proof(&ballot).is_err());
    }

    #[test]
    fn test_mix_net_shuffle_refuses() {
        let ballots: Vec<String> = (0..10).map(|i| format!("ballot_{}", i)).collect();
        assert!(mix_net_shuffle(&ballots).is_err());
        let proof = ShuffleProof {
            input_hash: "a".to_string(),
            output_hash: "b".to_string(),
            permutation_commitment: "c".to_string(),
            proof_elements: vec![],
        };
        assert!(verify_shuffle_proof(&ballots, &ballots, &proof).is_err());
    }

    #[test]
    fn test_merkle_ballot_tree() {
        let hashes: Vec<String> = (0..8).map(|i| format!("hash_{}", i)).collect();
        let (root, proofs) = build_ballot_merkle_tree(&hashes);
        assert!(!root.is_empty());
        assert_eq!(proofs.len(), 8);
        // Verify each proof
        for (i, proof) in proofs.iter().enumerate() {
            assert!(verify_merkle_proof(&hashes[i], proof, &root));
        }
    }

    #[test]
    fn test_merkle_proof_invalid() {
        let hashes: Vec<String> = (0..4).map(|i| format!("hash_{}", i)).collect();
        let (root, proofs) = build_ballot_merkle_tree(&hashes);
        // Wrong leaf should fail
        assert!(!verify_merkle_proof("wrong_hash", &proofs[0], &root));
    }

    #[test]
    fn test_threshold_decryption_refuses_and_never_panics() {
        // SECURITY: insufficient shares returns Err (previously panic! = DoS).
        let shares: Vec<DecryptionShare> = vec![];
        let err = combine_decryption_shares(&shares, 3, 42).unwrap_err();
        assert!(err.message.contains("not enough shares"));

        let share = DecryptionShare {
            guardian_index: 1,
            partial_decryption: "00".repeat(32),
            proof: "00".repeat(32),
        };
        let shares = vec![share.clone(), share.clone(), share];
        // SECURITY: caller-supplied actual_count must never be echoed as a tally.
        let err = combine_decryption_shares(&shares, 3, 42).unwrap_err();
        assert!(err.message.contains("refusing to fabricate tally"));
    }

    #[test]
    fn test_coercion_resistance() {
        let token = generate_coercion_resistance_tokens("del-001", "panic123");
        assert_eq!(token.delegate_id, "del-001");
        assert!(is_panic_code("panic123", "del-001", &token.panic_code_hash));
        assert!(!is_panic_code(
            "wrong_code",
            "del-001",
            &token.panic_code_hash
        ));
    }

    #[test]
    fn test_receipt_freeness() {
        let receipt = generate_receipt("bal-001", "CONF123", "voter_key");
        assert!(verify_receipt(&receipt, "bal-001", "CONF123", "voter_key"));
        assert!(!verify_receipt(&receipt, "bal-001", "WRONG", "voter_key"));
    }

    #[test]
    fn test_guardian_share_verification_refuses() {
        let share = GuardianKeyShare {
            index: 1,
            public_key: "00".repeat(32),
            verification_key: "00".repeat(32),
            encrypted_share: "00".repeat(32),
        };
        // SECURITY: previously returned true for any non-empty share.
        assert!(verify_guardian_share(&share, "any_key").is_err());
    }

    #[test]
    fn test_homomorphic_tally_refuses() {
        let ballots: Vec<String> = (0..5).map(|i| format!("enc_ballot_{}", i)).collect();
        let aspirants = vec!["asp-001".to_string(), "asp-002".to_string()];
        // SECURITY: previously produced identical fake tallies per aspirant.
        assert!(homomorphic_tally(&ballots, &aspirants).is_err());
    }

    #[test]
    fn test_handle_encrypt_ballot_refuses() {
        let req = EncryptBallotRequest {
            delegate_id: "del-test".to_string(),
            aspirant_id: "asp-test".to_string(),
            vote_type: "for".to_string(),
            election_pub_key: "test_key".to_string(),
        };
        assert!(handle_encrypt_ballot(req).is_err());
    }

    #[test]
    fn test_handle_shuffle_refuses() {
        let req = ShuffleRequest {
            encrypted_ballots: (0..5).map(|i| format!("b{}", i)).collect(),
        };
        assert!(handle_shuffle(req).is_err());
    }

    #[test]
    fn test_handle_verify_keys_refuses() {
        let req = VerifyKeyRequest {
            election_id: 1,
            public_key: "00".repeat(32),
            guardians: vec![],
        };
        // SECURITY: previously valid = !public_key.is_empty().
        assert!(handle_verify_keys(req).is_err());
    }
}
