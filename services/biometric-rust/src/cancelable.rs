//! Cancelable biometrics — BioHashing and revocable template transforms.
//!
//! If a biometric template is compromised, the transform can be revoked
//! and a new one issued with a different seed, without re-enrolling the
//! voter's actual biometric data.

use rand::RngCore;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::sync::Arc;
use parking_lot::RwLock;
use uuid::Uuid;
use zeroize::Zeroize;

#[derive(Debug, thiserror::Error)]
pub enum CancelableError {
    #[error("transform not found: {0}")]
    TransformNotFound(String),
    #[error("transform revoked: {0}")]
    TransformRevoked(String),
    #[error("dimension mismatch: expected {expected}, got {got}")]
    DimensionMismatch { expected: usize, got: usize },
}

#[derive(Clone, Serialize, Deserialize)]
pub struct CancelableTransform {
    pub transform_id: String,
    pub voter_vin: String,
    pub modality: String,
    pub transform_type: TransformType,
    pub version: u32,
    pub revoked: bool,
    seed: Vec<u8>,
}

impl Drop for CancelableTransform {
    fn drop(&mut self) {
        self.seed.zeroize();
    }
}

#[derive(Clone, Serialize, Deserialize)]
pub enum TransformType {
    BioHashing,
    RandomProjection,
    BloomFilter,
}

pub struct CancelableBiometrics {
    transforms: Arc<RwLock<HashMap<String, CancelableTransform>>>,
}

impl CancelableBiometrics {
    pub fn new() -> Self {
        Self {
            transforms: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    /// Create a new cancelable transform for a voter's modality.
    pub fn create_transform(
        &self,
        voter_vin: &str,
        modality: &str,
        transform_type: TransformType,
    ) -> String {
        let transform_id = format!("ct-{}", Uuid::new_v4());
        let mut seed = vec![0u8; 64];
        rand::rngs::OsRng.fill_bytes(&mut seed);

        let transform = CancelableTransform {
            transform_id: transform_id.clone(),
            voter_vin: voter_vin.to_string(),
            modality: modality.to_string(),
            transform_type,
            version: 1,
            revoked: false,
            seed,
        };

        self.transforms.write().insert(transform_id.clone(), transform);
        transform_id
    }

    /// Apply BioHash transform to a feature vector.
    /// Returns a binary hash that can be compared via Hamming distance.
    pub fn apply_biohash(
        &self,
        transform_id: &str,
        features: &[f64],
    ) -> Result<Vec<u8>, CancelableError> {
        let transforms = self.transforms.read();
        let transform = transforms
            .get(transform_id)
            .ok_or_else(|| CancelableError::TransformNotFound(transform_id.to_string()))?;

        if transform.revoked {
            return Err(CancelableError::TransformRevoked(transform_id.to_string()));
        }

        let dim = features.len();

        // Generate pseudo-random orthogonal projection matrix from seed
        let projection = self.generate_projection_matrix(&transform.seed, dim);

        // Project features and binarize
        let mut hash = Vec::with_capacity(dim / 8 + 1);
        let mut byte = 0u8;
        let mut bit_idx = 0;

        for row in &projection {
            let dot: f64 = row.iter().zip(features).map(|(a, b)| a * b).sum();
            if dot > 0.0 {
                byte |= 1 << (7 - bit_idx);
            }
            bit_idx += 1;
            if bit_idx == 8 {
                hash.push(byte);
                byte = 0;
                bit_idx = 0;
            }
        }
        if bit_idx > 0 {
            hash.push(byte);
        }

        Ok(hash)
    }

    /// Apply random projection transform (dimensionality-preserving).
    pub fn apply_random_projection(
        &self,
        transform_id: &str,
        features: &[f64],
        output_dim: usize,
    ) -> Result<Vec<f64>, CancelableError> {
        let transforms = self.transforms.read();
        let transform = transforms
            .get(transform_id)
            .ok_or_else(|| CancelableError::TransformNotFound(transform_id.to_string()))?;

        if transform.revoked {
            return Err(CancelableError::TransformRevoked(transform_id.to_string()));
        }

        let projection = self.generate_projection_matrix(&transform.seed, features.len());

        let mut result = Vec::with_capacity(output_dim);
        for (_i, row) in projection.iter().enumerate().take(output_dim) {
            let dot: f64 = row.iter().zip(features).map(|(a, b)| a * b).sum();
            result.push(dot);
        }

        // L2 normalize
        let norm: f64 = result.iter().map(|x| x * x).sum::<f64>().sqrt();
        if norm > 1e-10 {
            for v in &mut result {
                *v /= norm;
            }
        }

        Ok(result)
    }

    /// Revoke a transform (template is no longer valid).
    pub fn revoke_transform(
        &self,
        transform_id: &str,
    ) -> Result<(), CancelableError> {
        let mut transforms = self.transforms.write();
        let transform = transforms
            .get_mut(transform_id)
            .ok_or_else(|| CancelableError::TransformNotFound(transform_id.to_string()))?;

        transform.revoked = true;
        transform.seed.zeroize();
        Ok(())
    }

    /// Re-issue a transform with new seed (after revocation).
    pub fn reissue_transform(
        &self,
        voter_vin: &str,
        modality: &str,
        transform_type: TransformType,
    ) -> String {
        self.create_transform(voter_vin, modality, transform_type)
    }

    /// Compare two BioHash codes via normalized Hamming distance.
    pub fn compare_biohash(hash1: &[u8], hash2: &[u8]) -> f64 {
        let min_len = hash1.len().min(hash2.len());
        if min_len == 0 {
            return 1.0;
        }

        let mut diff_bits = 0u32;
        let mut total_bits = 0u32;

        for i in 0..min_len {
            let xor = hash1[i] ^ hash2[i];
            diff_bits += xor.count_ones();
            total_bits += 8;
        }

        diff_bits as f64 / total_bits as f64
    }

    fn generate_projection_matrix(&self, seed: &[u8], dim: usize) -> Vec<Vec<f64>> {
        let mut matrix = Vec::with_capacity(dim);

        for i in 0..dim {
            let mut row = Vec::with_capacity(dim);
            for j in 0..dim {
                let mut hasher = Sha256::new();
                hasher.update(seed);
                hasher.update(&(i as u64).to_le_bytes());
                hasher.update(&(j as u64).to_le_bytes());
                let hash = hasher.finalize();

                // Convert first 8 bytes to f64 in [-1, 1]
                let val = u64::from_le_bytes(hash[..8].try_into().unwrap());
                let normalized = (val as f64 / u64::MAX as f64) * 2.0 - 1.0;
                row.push(normalized);
            }

            // Normalize row
            let norm: f64 = row.iter().map(|x| x * x).sum::<f64>().sqrt();
            if norm > 1e-10 {
                for v in &mut row {
                    *v /= norm;
                }
            }

            matrix.push(row);
        }

        matrix
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_biohash_same_features_same_result() {
        let cb = CancelableBiometrics::new();
        let tid = cb.create_transform("VIN001", "fingerprint", TransformType::BioHashing);

        let features = vec![0.1, 0.5, -0.3, 0.8, -0.2, 0.4, 0.7, -0.1];
        let hash1 = cb.apply_biohash(&tid, &features).unwrap();
        let hash2 = cb.apply_biohash(&tid, &features).unwrap();

        assert_eq!(hash1, hash2);
        assert_eq!(CancelableBiometrics::compare_biohash(&hash1, &hash2), 0.0);
    }

    #[test]
    fn test_biohash_different_features_different_result() {
        let cb = CancelableBiometrics::new();
        let tid = cb.create_transform("VIN001", "fingerprint", TransformType::BioHashing);

        let f1 = vec![0.1, 0.5, -0.3, 0.8];
        let f2 = vec![-0.9, -0.5, 0.3, -0.8];

        let h1 = cb.apply_biohash(&tid, &f1).unwrap();
        let h2 = cb.apply_biohash(&tid, &f2).unwrap();

        let dist = CancelableBiometrics::compare_biohash(&h1, &h2);
        assert!(dist > 0.0, "Different features should produce different hashes");
    }

    #[test]
    fn test_revoke_blocks_usage() {
        let cb = CancelableBiometrics::new();
        let tid = cb.create_transform("VIN001", "facial", TransformType::BioHashing);

        cb.revoke_transform(&tid).unwrap();

        let result = cb.apply_biohash(&tid, &[0.1, 0.2]);
        assert!(result.is_err());
    }

    #[test]
    fn test_different_transforms_different_hashes() {
        let cb = CancelableBiometrics::new();
        let tid1 = cb.create_transform("VIN001", "fingerprint", TransformType::BioHashing);
        let tid2 = cb.create_transform("VIN001", "fingerprint", TransformType::BioHashing);

        let features = vec![0.1, 0.5, -0.3, 0.8];
        let h1 = cb.apply_biohash(&tid1, &features).unwrap();
        let h2 = cb.apply_biohash(&tid2, &features).unwrap();

        // Different seeds → different hashes (with high probability)
        assert_ne!(h1, h2);
    }
}
