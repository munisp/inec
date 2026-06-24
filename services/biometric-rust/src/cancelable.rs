//! Cancelable biometrics — BioHashing and revocable template transforms.
//!
//! All transforms persisted to PostgreSQL — no in-memory storage.
//! If a biometric template is compromised, the transform can be revoked
//! and a new one issued with a different seed, without re-enrolling the
//! voter's actual biometric data.

use rand::RngCore;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use sqlx::PgPool;
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
    #[error("database error: {0}")]
    DatabaseError(String),
}

impl From<sqlx::Error> for CancelableError {
    fn from(e: sqlx::Error) -> Self {
        CancelableError::DatabaseError(e.to_string())
    }
}

#[derive(Clone, Serialize, Deserialize)]
pub struct CancelableTransform {
    pub transform_id: String,
    pub voter_vin: String,
    pub modality: String,
    pub transform_type: TransformType,
    pub version: i32,
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

impl TransformType {
    fn as_str(&self) -> &'static str {
        match self {
            TransformType::BioHashing => "BioHashing",
            TransformType::RandomProjection => "RandomProjection",
            TransformType::BloomFilter => "BloomFilter",
        }
    }

    fn from_str(s: &str) -> Self {
        match s {
            "RandomProjection" => TransformType::RandomProjection,
            "BloomFilter" => TransformType::BloomFilter,
            _ => TransformType::BioHashing,
        }
    }
}

/// Cancelable biometrics — all state in PostgreSQL.
pub struct CancelableBiometrics {
    pool: PgPool,
}

impl CancelableBiometrics {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }

    /// Create a new cancelable transform and persist to PostgreSQL.
    pub async fn create_transform(
        &self,
        voter_vin: &str,
        modality: &str,
        transform_type: TransformType,
    ) -> Result<String, CancelableError> {
        let transform_id = format!("ct-{}", Uuid::new_v4());
        let mut seed = vec![0u8; 64];
        rand::rngs::OsRng.fill_bytes(&mut seed);

        sqlx::query(
            "INSERT INTO cancelable_transforms (transform_id, voter_vin, modality, transform_type, version, is_revoked, seed)
             VALUES ($1, $2, $3, $4, 1, FALSE, $5)"
        )
            .bind(&transform_id)
            .bind(voter_vin)
            .bind(modality)
            .bind(transform_type.as_str())
            .bind(&seed)
            .execute(&self.pool)
            .await?;

        seed.zeroize();
        Ok(transform_id)
    }

    /// Apply BioHash transform to a feature vector.
    pub async fn apply_biohash(
        &self,
        transform_id: &str,
        features: &[f64],
    ) -> Result<Vec<u8>, CancelableError> {
        let transform = self.load_transform(transform_id).await?;

        if transform.revoked {
            return Err(CancelableError::TransformRevoked(transform_id.to_string()));
        }

        let dim = features.len();
        let projection = Self::generate_projection_matrix(&transform.seed, dim);

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
    pub async fn apply_random_projection(
        &self,
        transform_id: &str,
        features: &[f64],
        output_dim: usize,
    ) -> Result<Vec<f64>, CancelableError> {
        let transform = self.load_transform(transform_id).await?;

        if transform.revoked {
            return Err(CancelableError::TransformRevoked(transform_id.to_string()));
        }

        let projection = Self::generate_projection_matrix(&transform.seed, features.len());

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

    /// Revoke a transform in PostgreSQL.
    pub async fn revoke_transform(
        &self,
        transform_id: &str,
    ) -> Result<(), CancelableError> {
        let result = sqlx::query(
            "UPDATE cancelable_transforms SET is_revoked = TRUE, seed = '\\x00', revoked_at = NOW()
             WHERE transform_id = $1 AND is_revoked = FALSE"
        )
            .bind(transform_id)
            .execute(&self.pool)
            .await?;

        if result.rows_affected() == 0 {
            return Err(CancelableError::TransformNotFound(transform_id.to_string()));
        }

        Ok(())
    }

    /// Re-issue a transform with new seed (after revocation).
    pub async fn reissue_transform(
        &self,
        voter_vin: &str,
        modality: &str,
        transform_type: TransformType,
    ) -> Result<String, CancelableError> {
        self.create_transform(voter_vin, modality, transform_type).await
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

    // ─── Private ────────────────────────────────────────────────

    async fn load_transform(&self, transform_id: &str) -> Result<CancelableTransform, CancelableError> {
        let row = sqlx::query_as::<_, (String, String, String, i32, bool, Vec<u8>)>(
            "SELECT voter_vin, modality, transform_type, version, is_revoked, seed
             FROM cancelable_transforms WHERE transform_id = $1"
        )
            .bind(transform_id)
            .fetch_optional(&self.pool)
            .await?
            .ok_or_else(|| CancelableError::TransformNotFound(transform_id.to_string()))?;

        Ok(CancelableTransform {
            transform_id: transform_id.to_string(),
            voter_vin: row.0,
            modality: row.1,
            transform_type: TransformType::from_str(&row.2),
            version: row.3,
            revoked: row.4,
            seed: row.5,
        })
    }

    fn generate_projection_matrix(seed: &[u8], dim: usize) -> Vec<Vec<f64>> {
        let mut matrix = Vec::with_capacity(dim);

        for i in 0..dim {
            let mut row = Vec::with_capacity(dim);
            for j in 0..dim {
                let mut hasher = Sha256::new();
                hasher.update(seed);
                hasher.update(&(i as u64).to_le_bytes());
                hasher.update(&(j as u64).to_le_bytes());
                let hash = hasher.finalize();

                let val = u64::from_le_bytes(hash[..8].try_into().unwrap());
                let normalized = (val as f64 / u64::MAX as f64) * 2.0 - 1.0;
                row.push(normalized);
            }

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

    #[tokio::test]
    #[ignore]
    async fn test_biohash_same_features_same_result() {
        let pool = crate::db::init_pool("postgresql://ngapp:ngapp123@localhost:5432/ngapp")
            .await.unwrap();
        let cb = CancelableBiometrics::new(pool);
        let tid = cb.create_transform("VIN001", "fingerprint", TransformType::BioHashing).await.unwrap();

        let features = vec![0.1, 0.5, -0.3, 0.8, -0.2, 0.4, 0.7, -0.1];
        let hash1 = cb.apply_biohash(&tid, &features).await.unwrap();
        let hash2 = cb.apply_biohash(&tid, &features).await.unwrap();

        assert_eq!(hash1, hash2);
        assert_eq!(CancelableBiometrics::compare_biohash(&hash1, &hash2), 0.0);
    }

    #[tokio::test]
    #[ignore]
    async fn test_revoke_blocks_usage() {
        let pool = crate::db::init_pool("postgresql://ngapp:ngapp123@localhost:5432/ngapp")
            .await.unwrap();
        let cb = CancelableBiometrics::new(pool);
        let tid = cb.create_transform("VIN001", "facial", TransformType::BioHashing).await.unwrap();

        cb.revoke_transform(&tid).await.unwrap();

        let result = cb.apply_biohash(&tid, &[0.1, 0.2]).await;
        assert!(result.is_err());
    }
}
