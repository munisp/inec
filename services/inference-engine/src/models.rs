//! ML Model wrappers for ONNX Runtime inference on CPU.

use anyhow::{Context, Result};
use ndarray::Array2;
use tracing::info;

/// XGBoost anomaly detection model (ONNX format).
pub struct AnomalyModel {
    session: ort::Session,
    n_features: usize,
}

impl AnomalyModel {
    /// Load ONNX model from disk.
    pub fn load(path: &str) -> Result<Self> {
        let session = ort::Session::builder()?
            .with_optimization_level(ort::GraphOptimizationLevel::Level3)?
            .with_intra_threads(4)?
            .commit_from_file(path)
            .context("Failed to load anomaly ONNX model")?;

        info!(path, "Anomaly model loaded (ONNX)");
        Ok(Self { session, n_features: 17 })
    }

    /// Run inference on a single feature vector. Returns anomaly probability [0,1].
    pub fn predict(&self, features: &[f64]) -> f64 {
        assert_eq!(features.len(), self.n_features, "Expected 17 features");

        let input: Vec<f32> = features.iter().map(|&x| x as f32).collect();
        let array = Array2::from_shape_vec((1, self.n_features), input)
            .expect("Feature shape mismatch");

        let outputs = self.session
            .run(ort::inputs![array].unwrap())
            .unwrap_or_default();

        // XGBoost ONNX outputs: [labels, probabilities]
        if outputs.len() >= 2 {
            // probabilities shape: (1, 2) — [p_normal, p_anomaly]
            if let Ok(probs) = outputs[1].try_extract_tensor::<f32>() {
                let view = probs.view();
                if view.len() >= 2 {
                    return view[[0, 1]] as f64;
                }
            }
        }

        // Fallback: return label
        if let Ok(labels) = outputs[0].try_extract_tensor::<i64>() {
            return labels.view()[[0]] as f64;
        }

        0.0
    }

    /// Batch predict on multiple polling units.
    pub fn predict_batch(&self, batch_features: &[Vec<f64>]) -> Vec<f64> {
        let n = batch_features.len();
        let flat: Vec<f32> = batch_features.iter()
            .flat_map(|f| f.iter().map(|&x| x as f32))
            .collect();

        let array = Array2::from_shape_vec((n, self.n_features), flat)
            .expect("Batch shape mismatch");

        let outputs = self.session
            .run(ort::inputs![array].unwrap())
            .unwrap_or_default();

        if outputs.len() >= 2 {
            if let Ok(probs) = outputs[1].try_extract_tensor::<f32>() {
                let view = probs.view();
                return (0..n).map(|i| view[[i, 1]] as f64).collect();
            }
        }

        vec![0.0; n]
    }
}

/// Face embedding model wrapper.
/// Actual embedding extraction happens in Python (InsightFace).
/// This module handles fast cosine similarity and batch matching.
pub struct FaceModel {
    embedding_dim: usize,
}

impl FaceModel {
    pub fn new() -> Result<Self> {
        Ok(Self { embedding_dim: 512 })
    }

    /// Compute cosine similarity between two L2-normalized embeddings.
    pub fn cosine_similarity(&self, a: &[f32], b: &[f32]) -> f32 {
        assert_eq!(a.len(), self.embedding_dim);
        assert_eq!(b.len(), self.embedding_dim);

        let dot: f32 = a.iter().zip(b.iter()).map(|(x, y)| x * y).sum();
        let norm_a: f32 = a.iter().map(|x| x * x).sum::<f32>().sqrt();
        let norm_b: f32 = b.iter().map(|x| x * x).sum::<f32>().sqrt();

        dot / (norm_a * norm_b).max(1e-10)
    }

    /// Search for the closest match in a database of embeddings.
    /// Returns (index, similarity) of the best match above threshold.
    pub fn search_nearest(
        &self,
        query: &[f32],
        database: &[Vec<f32>],
        threshold: f32,
    ) -> Option<(usize, f32)> {
        let mut best_idx = 0;
        let mut best_score = f32::MIN;

        for (i, stored) in database.iter().enumerate() {
            let score = self.cosine_similarity(query, stored);
            if score > best_score {
                best_score = score;
                best_idx = i;
            }
        }

        if best_score >= threshold {
            Some((best_idx, best_score))
        } else {
            None
        }
    }

    /// Batch cosine similarity: compare one query against N stored embeddings.
    /// Uses SIMD-friendly layout for performance.
    pub fn batch_similarity(&self, query: &[f32], database: &[Vec<f32>]) -> Vec<f32> {
        database.iter()
            .map(|stored| self.cosine_similarity(query, stored))
            .collect()
    }
}

/// CDCN Liveness/PAD model (ONNX format).
pub struct LivenessModel {
    session: ort::Session,
}

impl LivenessModel {
    pub fn load(path: &str) -> Result<Self> {
        let session = ort::Session::builder()?
            .with_optimization_level(ort::GraphOptimizationLevel::Level3)?
            .with_intra_threads(2)?
            .commit_from_file(path)
            .context("Failed to load liveness ONNX model")?;

        info!(path, "Liveness CDCN model loaded (ONNX)");
        Ok(Self { session })
    }

    /// Run liveness check on a face crop (256x256 RGB, normalized to [0,1]).
    /// Returns (depth_map_quality, liveness_score).
    pub fn predict(&self, face_crop: &[f32]) -> (f32, f32) {
        // Input shape: (1, 3, 256, 256) = 196608 floats
        let expected_size = 1 * 3 * 256 * 256;
        if face_crop.len() != expected_size {
            return (0.0, 0.0);
        }

        let array = ndarray::Array4::from_shape_vec(
            (1, 3, 256, 256),
            face_crop.to_vec(),
        ).expect("Shape mismatch");

        let outputs = self.session
            .run(ort::inputs![array].unwrap())
            .unwrap_or_default();

        // Output 0: depth_map (1, 1, 32, 32)
        // Output 1: liveness_score (1, 1)
        let depth_quality = if let Ok(depth) = outputs[0].try_extract_tensor::<f32>() {
            let view = depth.view();
            let total: f32 = view.iter().sum();
            total / (32.0 * 32.0) // Average depth value as quality indicator
        } else {
            0.0
        };

        let liveness = if outputs.len() > 1 {
            if let Ok(score) = outputs[1].try_extract_tensor::<f32>() {
                score.view()[[0, 0]]
            } else {
                0.0
            }
        } else {
            0.0
        };

        (depth_quality, liveness)
    }
}
