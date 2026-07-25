//! ML model wrappers for ONNX inference.

use anyhow::{anyhow, Result};
use ort::{
    session::{builder::GraphOptimizationLevel, Session},
    value::Tensor,
};
use std::sync::Mutex;
use tracing::info;

/// XGBoost anomaly detection model (ONNX format).
pub struct AnomalyModel {
    session: Mutex<Session>,
    n_features: usize,
}

impl AnomalyModel {
    /// Load ONNX model from disk.
    pub fn load(path: &str) -> Result<Self> {
        let builder = Session::builder()
            .map_err(|error| anyhow!("unable to create anomaly model session builder: {error}"))?;
        let builder = builder
            .with_optimization_level(GraphOptimizationLevel::Level3)
            .map_err(|error| anyhow!("unable to configure anomaly model optimization: {error}"))?;
        let mut builder = builder
            .with_intra_threads(4)
            .map_err(|error| anyhow!("unable to configure anomaly model threads: {error}"))?;
        let session = builder
            .commit_from_file(path)
            .map_err(|error| anyhow!("failed to load anomaly ONNX model: {error}"))?;

        info!(path, "Anomaly model loaded (ONNX)");
        Ok(Self {
            session: Mutex::new(session),
            n_features: 17,
        })
    }

    /// Run inference on a single feature vector. Missing or malformed output is
    /// an explicit error; it is never converted into a neutral anomaly score.
    pub fn predict(&self, features: &[f64]) -> Result<f64> {
        if features.len() != self.n_features {
            return Err(anyhow!(
                "expected {} anomaly features, received {}",
                self.n_features,
                features.len()
            ));
        }
        let input: Vec<f32> = features.iter().map(|&value| value as f32).collect();
        let tensor = Tensor::from_array((vec![1usize, self.n_features], input))
            .map_err(|error| anyhow!("unable to construct anomaly input tensor: {error}"))?;
        let mut session = self
            .session
            .lock()
            .map_err(|_| anyhow!("anomaly model session lock poisoned"))?;
        let outputs = session
            .run(ort::inputs![tensor])
            .map_err(|error| anyhow!("approved anomaly model inference failed: {error}"))?;

        if outputs.len() >= 2 {
            if let Ok((_shape, probabilities)) = outputs[1].try_extract_tensor::<f32>() {
                if probabilities.len() >= 2 {
                    return Ok(probabilities[1] as f64);
                }
            }
        }
        if let Ok((_shape, labels)) = outputs[0].try_extract_tensor::<i64>() {
            if let Some(label) = labels.first() {
                return Ok(*label as f64);
            }
        }
        Err(anyhow!(
            "approved anomaly model returned no usable probability or label output"
        ))
    }

    /// Batch predict on multiple polling units without fabricating scores when
    /// the model response is missing or malformed.
    pub fn predict_batch(&self, batch_features: &[Vec<f64>]) -> Result<Vec<f64>> {
        if batch_features
            .iter()
            .any(|features| features.len() != self.n_features)
        {
            return Err(anyhow!(
                "batch contains an anomaly feature vector with an invalid length"
            ));
        }
        let count = batch_features.len();
        if count == 0 {
            return Err(anyhow!("anomaly batch must contain at least one feature vector"));
        }
        let flat: Vec<f32> = batch_features
            .iter()
            .flat_map(|features| features.iter().map(|&value| value as f32))
            .collect();
        let tensor = Tensor::from_array((vec![count, self.n_features], flat))
            .map_err(|error| anyhow!("unable to construct anomaly batch tensor: {error}"))?;
        let mut session = self
            .session
            .lock()
            .map_err(|_| anyhow!("anomaly model session lock poisoned"))?;
        let outputs = session
            .run(ort::inputs![tensor])
            .map_err(|error| anyhow!("approved anomaly batch inference failed: {error}"))?;
        if outputs.len() >= 2 {
            if let Ok((_shape, probabilities)) = outputs[1].try_extract_tensor::<f32>() {
                if probabilities.len() >= count.saturating_mul(2) {
                    return Ok((0..count)
                        .map(|index| probabilities[index * 2 + 1] as f64)
                        .collect());
                }
            }
        }
        Err(anyhow!(
            "approved anomaly model returned an incomplete batch probability output"
        ))
    }
}

/// Face embedding model wrapper.
/// Actual embedding extraction happens in a configured authoritative service.
/// This module performs only deterministic cosine similarity and batch matching.
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

        let dot: f32 = a.iter().zip(b.iter()).map(|(left, right)| left * right).sum();
        let norm_a: f32 = a.iter().map(|value| value * value).sum::<f32>().sqrt();
        let norm_b: f32 = b.iter().map(|value| value * value).sum::<f32>().sqrt();

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

        for (index, stored) in database.iter().enumerate() {
            let score = self.cosine_similarity(query, stored);
            if score > best_score {
                best_score = score;
                best_idx = index;
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
        database
            .iter()
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
        let builder = Session::builder()
            .map_err(|error| anyhow!("unable to create liveness model session builder: {error}"))?;
        let builder = builder
            .with_optimization_level(GraphOptimizationLevel::Level3)
            .map_err(|error| anyhow!("unable to configure liveness model optimization: {error}"))?;
        let mut builder = builder
            .with_intra_threads(2)
            .map_err(|error| anyhow!("unable to configure liveness model threads: {error}"))?;
        let session = builder
            .commit_from_file(path)
            .map_err(|error| anyhow!("failed to load liveness ONNX model: {error}"))?;

        info!(path, "Liveness CDCN model loaded (ONNX)");
        Ok(Self {
            session: Mutex::new(session),
        })
    }

    /// Run liveness check on a face crop (256x256 RGB, normalized to [0,1]).
    /// Missing or malformed model output is an explicit error, never a neutral
    /// liveness decision.
    pub fn predict(&self, face_crop: &[f32]) -> Result<(f32, f32)> {
        let expected_size = 3 * 256 * 256;
        if face_crop.len() != expected_size {
            return Err(anyhow!(
                "expected {} liveness values, received {}",
                expected_size,
                face_crop.len()
            ));
        }
        let tensor = Tensor::from_array((vec![1usize, 3, 256, 256], face_crop.to_vec()))
            .map_err(|error| anyhow!("unable to construct liveness input tensor: {error}"))?;
        let mut session = self
            .session
            .lock()
            .map_err(|_| anyhow!("liveness model session lock poisoned"))?;
        let outputs = session
            .run(ort::inputs![tensor])
            .map_err(|error| anyhow!("liveness model inference failed: {error}"))?;

        let depth_quality = if let Ok((_shape, depth_values)) = outputs[0].try_extract_tensor::<f32>() {
            if depth_values.is_empty() {
                return Err(anyhow!("liveness model returned an empty depth map"));
            }
            depth_values.iter().sum::<f32>() / depth_values.len() as f32
        } else {
            return Err(anyhow!("liveness model did not return a readable depth map"));
        };
        let liveness = if outputs.len() > 1 {
            if let Ok((_shape, scores)) = outputs[1].try_extract_tensor::<f32>() {
                *scores
                    .first()
                    .ok_or_else(|| anyhow!("liveness model returned an empty score"))?
            } else {
                return Err(anyhow!("liveness model did not return a readable score"));
            }
        } else {
            return Err(anyhow!("liveness model returned no score output"));
        };
        Ok((depth_quality, liveness))
    }
}
