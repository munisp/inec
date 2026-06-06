//! ML Model wrappers for ONNX Runtime inference on CPU.

use anyhow::Result;
use ort::session::{Session, builder::GraphOptimizationLevel};
use std::sync::Mutex;
use tracing::info;

fn ort_err(e: impl std::fmt::Display) -> anyhow::Error {
    anyhow::anyhow!("ort error: {}", e)
}

/// XGBoost anomaly detection model (ONNX format).
pub struct AnomalyModel {
    session: Mutex<Session>,
    n_features: usize,
}

impl AnomalyModel {
    /// Load ONNX model from disk.
    pub fn load(path: &str) -> Result<Self> {
        let session = Session::builder()
            .map_err(ort_err)?
            .with_optimization_level(GraphOptimizationLevel::Level3)
            .map_err(ort_err)?
            .with_intra_threads(4)
            .map_err(ort_err)?
            .commit_from_file(path)
            .map_err(ort_err)?;

        info!(path, "Anomaly model loaded (ONNX)");
        Ok(Self { session: Mutex::new(session), n_features: 17 })
    }

    /// Run inference on a single feature vector. Returns anomaly probability [0,1].
    pub fn predict(&self, features: &[f64]) -> f64 {
        assert_eq!(features.len(), self.n_features, "Expected 17 features");

        let input: Vec<f32> = features.iter().map(|&x| x as f32).collect();
        let input_tensor = ort::value::Tensor::from_array(
            ([1_usize, self.n_features], input)
        );
        let input_tensor = match input_tensor {
            Ok(t) => t,
            Err(_) => return 0.0,
        };

        let mut session = self.session.lock().unwrap();
        let outputs = match session.run(ort::inputs!["float_input" => input_tensor]) {
            Ok(o) => o,
            Err(_) => return 0.0,
        };

        // XGBoost ONNX outputs: [labels, probabilities]
        if outputs.len() >= 2 {
            if let Ok((_shape, data)) = outputs[1].try_extract_tensor::<f32>() {
                if data.len() >= 2 {
                    return data[1] as f64; // p_anomaly
                }
            }
        }

        // Fallback: return label
        if let Ok((_shape, data)) = outputs[0].try_extract_tensor::<i64>() {
            if !data.is_empty() {
                return data[0] as f64;
            }
        }

        0.0
    }

    /// Batch predict on multiple polling units.
    pub fn predict_batch(&self, batch_features: &[Vec<f64>]) -> Vec<f64> {
        batch_features.iter().map(|f| self.predict(f)).collect()
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
    pub fn batch_similarity(&self, query: &[f32], database: &[Vec<f32>]) -> Vec<f32> {
        database.iter()
            .map(|stored| self.cosine_similarity(query, stored))
            .collect()
    }
}

/// CDCN Liveness/PAD model (ONNX format).
pub struct LivenessModel {
    session: Mutex<Session>,
}

impl LivenessModel {
    pub fn load(path: &str) -> Result<Self> {
        let session = Session::builder()
            .map_err(ort_err)?
            .with_optimization_level(GraphOptimizationLevel::Level3)
            .map_err(ort_err)?
            .with_intra_threads(2)
            .map_err(ort_err)?
            .commit_from_file(path)
            .map_err(ort_err)?;

        info!(path, "Liveness CDCN model loaded (ONNX)");
        Ok(Self { session: Mutex::new(session) })
    }

    /// Run liveness check on a face crop (256x256 RGB, normalized to [0,1]).
    /// Returns (depth_map_quality, liveness_score).
    pub fn predict(&self, face_crop: &[f32]) -> (f32, f32) {
        let expected_size = 3 * 256 * 256;
        if face_crop.len() != expected_size {
            return (0.0, 0.0);
        }

        let input_tensor = match ort::value::Tensor::from_array(
            ([1_usize, 3, 256, 256], face_crop.to_vec())
        ) {
            Ok(t) => t,
            Err(_) => return (0.0, 0.0),
        };

        let mut session = self.session.lock().unwrap();
        let outputs = match session.run(ort::inputs!["input" => input_tensor]) {
            Ok(o) => o,
            Err(_) => return (0.0, 0.0),
        };

        // Output 0: depth_map (1, 1, 32, 32)
        let depth_quality = if let Ok((_shape, data)) = outputs[0].try_extract_tensor::<f32>() {
            let total: f32 = data.iter().sum();
            total / (32.0 * 32.0) // Average depth value as quality indicator
        } else {
            0.0
        };

        let liveness = if outputs.len() > 1 {
            if let Ok((_shape, data)) = outputs[1].try_extract_tensor::<f32>() {
                if !data.is_empty() { data[0] } else { 0.0 }
            } else {
                0.0
            }
        } else {
            0.0
        };

        (depth_quality, liveness)
    }
}
