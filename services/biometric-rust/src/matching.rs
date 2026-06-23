//! High-speed biometric matching engine.
//!
//! - Parallel 1:N identification using rayon
//! - Score fusion (weighted sum, max rule, sum rule)
//! - Score normalization (Z-norm, min-max)
//! - FAR/FRR computation

use rayon::prelude::*;
use serde::{Deserialize, Serialize};
use std::time::Instant;

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct MatchScore {
    pub probe_id: String,
    pub gallery_id: String,
    pub modality: String,
    pub score: f64,
    pub normalized_score: f64,
    pub decision: MatchDecision,
    pub algorithm: String,
    pub latency_us: u64,
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
pub enum MatchDecision {
    Match,
    NoMatch,
    Inconclusive,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FusedScore {
    pub fused_score: f64,
    pub decision: MatchDecision,
    pub fusion_method: FusionMethod,
    pub modality_scores: Vec<MatchScore>,
    pub latency_us: u64,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub enum FusionMethod {
    WeightedSum,
    MaxRule,
    SumRule,
    ProductRule,
}

#[derive(Clone, Debug)]
pub struct NormalizationParams {
    pub mean: f64,
    pub std_dev: f64,
    pub min_val: f64,
    pub max_val: f64,
}

impl Default for NormalizationParams {
    fn default() -> Self {
        Self {
            mean: 0.5,
            std_dev: 0.2,
            min_val: 0.0,
            max_val: 1.0,
        }
    }
}

/// Fingerprint minutiae matching (Bozorth3-like algorithm).
pub fn match_fingerprint_minutiae(
    probe: &[(i32, i32, f64, u8)],  // (x, y, angle, type)
    gallery: &[(i32, i32, f64, u8)],
    threshold: f64,
) -> MatchScore {
    let start = Instant::now();

    if probe.is_empty() || gallery.is_empty() {
        return MatchScore {
            probe_id: String::new(),
            gallery_id: String::new(),
            modality: "fingerprint".into(),
            score: 0.0,
            normalized_score: 0.0,
            decision: MatchDecision::NoMatch,
            algorithm: "bozorth3".into(),
            latency_us: start.elapsed().as_micros() as u64,
        };
    }

    let spatial_tol = 15.0f64;
    let angle_tol = 30.0f64;

    // Build compatibility pairs
    let mut pairs: Vec<(usize, usize, f64)> = Vec::new();

    for (pi, pm) in probe.iter().enumerate() {
        for (gi, gm) in gallery.iter().enumerate() {
            let dx = (pm.0 - gm.0) as f64;
            let dy = (pm.1 - gm.1) as f64;
            let dist = (dx * dx + dy * dy).sqrt();

            let mut angle_diff = (pm.2 - gm.2).abs();
            if angle_diff > 180.0 {
                angle_diff = 360.0 - angle_diff;
            }

            if dist < spatial_tol * 3.0 && angle_diff < angle_tol * 2.0 {
                let mut compat = (1.0 - dist / (spatial_tol * 3.0))
                    * (1.0 - angle_diff / (angle_tol * 2.0));
                if pm.3 == gm.3 {
                    compat *= 1.1;
                }
                pairs.push((pi, gi, compat.min(1.0)));
            }
        }
    }

    pairs.sort_by(|a, b| b.2.partial_cmp(&a.2).unwrap_or(std::cmp::Ordering::Equal));

    let mut used_probe = vec![false; probe.len()];
    let mut used_gallery = vec![false; gallery.len()];
    let mut matched = 0;
    let mut total_compat = 0.0;

    for (pi, gi, compat) in &pairs {
        if !used_probe[*pi] && !used_gallery[*gi] {
            used_probe[*pi] = true;
            used_gallery[*gi] = true;
            matched += 1;
            total_compat += compat;
        }
    }

    let min_count = probe.len().min(gallery.len());
    let score = if min_count > 0 {
        (matched as f64 / min_count as f64) * (total_compat / matched.max(1) as f64)
    } else {
        0.0
    };

    let decision = if score >= threshold {
        MatchDecision::Match
    } else {
        MatchDecision::NoMatch
    };

    MatchScore {
        probe_id: String::new(),
        gallery_id: String::new(),
        modality: "fingerprint".into(),
        score,
        normalized_score: score,
        decision,
        algorithm: "bozorth3".into(),
        latency_us: start.elapsed().as_micros() as u64,
    }
}

/// Face embedding matching using cosine similarity.
pub fn match_face_embeddings(
    probe: &[f64],
    gallery: &[f64],
    threshold: f64,
) -> MatchScore {
    let start = Instant::now();

    let dim = probe.len().min(gallery.len());
    if dim == 0 {
        return MatchScore {
            probe_id: String::new(),
            gallery_id: String::new(),
            modality: "face".into(),
            score: 0.0,
            normalized_score: 0.0,
            decision: MatchDecision::NoMatch,
            algorithm: "cosine".into(),
            latency_us: start.elapsed().as_micros() as u64,
        };
    }

    let mut dot = 0.0f64;
    let mut n1 = 0.0f64;
    let mut n2 = 0.0f64;

    for i in 0..dim {
        dot += probe[i] * gallery[i];
        n1 += probe[i] * probe[i];
        n2 += gallery[i] * gallery[i];
    }

    let score = if n1 > 1e-10 && n2 > 1e-10 {
        (dot / (n1.sqrt() * n2.sqrt()) + 1.0) / 2.0
    } else {
        0.0
    };

    let decision = if score >= threshold {
        MatchDecision::Match
    } else {
        MatchDecision::NoMatch
    };

    MatchScore {
        probe_id: String::new(),
        gallery_id: String::new(),
        modality: "face".into(),
        score,
        normalized_score: score,
        decision,
        algorithm: "cosine".into(),
        latency_us: start.elapsed().as_micros() as u64,
    }
}

/// Iris code matching using fractional Hamming distance.
pub fn match_iris_codes(
    probe: &[u8],
    gallery: &[u8],
    probe_mask: &[u8],
    gallery_mask: &[u8],
    threshold: f64,
    n_rotations: i32,
) -> MatchScore {
    let start = Instant::now();

    let min_len = probe.len().min(gallery.len()).min(probe_mask.len()).min(gallery_mask.len());
    if min_len == 0 {
        return MatchScore {
            probe_id: String::new(),
            gallery_id: String::new(),
            modality: "iris".into(),
            score: 0.0,
            normalized_score: 0.0,
            decision: MatchDecision::NoMatch,
            algorithm: "hamming".into(),
            latency_us: start.elapsed().as_micros() as u64,
        };
    }

    let mut best_hd = 1.0f64;
    let shift_unit = (min_len / 64).max(1);

    for rot in -n_rotations..=n_rotations {
        let shift = (rot.unsigned_abs() as usize) * shift_unit;
        if shift >= min_len {
            continue;
        }

        let mut diff_bits = 0u32;
        let mut total_bits = 0u32;

        for i in 0..min_len {
            let gi = if rot > 0 {
                (i + shift) % min_len
            } else if rot < 0 {
                (i + min_len - shift) % min_len
            } else {
                i
            };

            let mask = probe_mask[i] & gallery_mask[gi];
            if mask == 0 {
                continue;
            }

            let xor = (probe[i] ^ gallery[gi]) & mask;
            diff_bits += xor.count_ones();
            total_bits += mask.count_ones();
        }

        if total_bits > 0 {
            let hd = diff_bits as f64 / total_bits as f64;
            if hd < best_hd {
                best_hd = hd;
            }
        }
    }

    let score = 1.0 - best_hd;
    let decision = if best_hd < threshold {
        MatchDecision::Match
    } else {
        MatchDecision::NoMatch
    };

    MatchScore {
        probe_id: String::new(),
        gallery_id: String::new(),
        modality: "iris".into(),
        score,
        normalized_score: score,
        decision,
        algorithm: "hamming".into(),
        latency_us: start.elapsed().as_micros() as u64,
    }
}

/// 1:N identification — search gallery in parallel.
pub fn identify_1n(
    probe_fingerprint: Option<&[(i32, i32, f64, u8)]>,
    probe_face: Option<&[f64]>,
    probe_iris: Option<(&[u8], &[u8])>,
    gallery: &[(String, Option<Vec<(i32, i32, f64, u8)>>, Option<Vec<f64>>, Option<(Vec<u8>, Vec<u8>)>)],
    thresholds: &IdentifyThresholds,
) -> Vec<FusedScore> {
    let _start = Instant::now();

    let results: Vec<FusedScore> = gallery
        .par_iter()
        .filter_map(|(id, gfp, gface, giris)| {
            let mut scores = Vec::new();

            if let (Some(pfp), Some(gfp)) = (probe_fingerprint, gfp.as_ref()) {
                let mut s = match_fingerprint_minutiae(pfp, gfp, thresholds.fingerprint);
                s.gallery_id = id.clone();
                scores.push(s);
            }

            if let (Some(pface), Some(gface)) = (probe_face, gface.as_ref()) {
                let mut s = match_face_embeddings(pface, gface, thresholds.face);
                s.gallery_id = id.clone();
                scores.push(s);
            }

            if let (Some((pcode, pmask)), Some((gcode, gmask))) = (probe_iris, giris.as_ref()) {
                let mut s = match_iris_codes(pcode, gcode, pmask, gmask, thresholds.iris, 7);
                s.gallery_id = id.clone();
                scores.push(s);
            }

            if scores.is_empty() {
                return None;
            }

            let fused = fuse_scores(&scores, &thresholds.weights, FusionMethod::WeightedSum);
            Some(fused)
        })
        .collect();

    let mut sorted = results;
    sorted.sort_by(|a, b| b.fused_score.partial_cmp(&a.fused_score).unwrap_or(std::cmp::Ordering::Equal));
    sorted
}

#[derive(Clone, Debug)]
pub struct IdentifyThresholds {
    pub fingerprint: f64,
    pub face: f64,
    pub iris: f64,
    pub fused: f64,
    pub weights: std::collections::HashMap<String, f64>,
}

impl Default for IdentifyThresholds {
    fn default() -> Self {
        let mut weights = std::collections::HashMap::new();
        weights.insert("fingerprint".into(), 0.40);
        weights.insert("face".into(), 0.35);
        weights.insert("iris".into(), 0.25);
        Self {
            fingerprint: 0.40,
            face: 0.45,
            iris: 0.32,
            fused: 0.45,
            weights,
        }
    }
}

/// Score-level fusion.
pub fn fuse_scores(
    scores: &[MatchScore],
    weights: &std::collections::HashMap<String, f64>,
    method: FusionMethod,
) -> FusedScore {
    let start = Instant::now();

    let fused_score = match method {
        FusionMethod::WeightedSum => {
            let mut total = 0.0f64;
            let mut total_weight = 0.0f64;
            for s in scores {
                let w = weights.get(&s.modality).copied().unwrap_or(1.0 / scores.len() as f64);
                total += s.normalized_score * w;
                total_weight += w;
            }
            if total_weight > 0.0 { total / total_weight } else { 0.0 }
        }
        FusionMethod::MaxRule => {
            scores.iter().map(|s| s.normalized_score).fold(0.0f64, f64::max)
        }
        FusionMethod::SumRule => {
            let sum: f64 = scores.iter().map(|s| s.normalized_score).sum();
            sum / scores.len().max(1) as f64
        }
        FusionMethod::ProductRule => {
            let product: f64 = scores.iter().map(|s| s.normalized_score.max(1e-10)).product();
            product.powf(1.0 / scores.len().max(1) as f64)
        }
    };

    let decision = if fused_score >= 0.45 {
        MatchDecision::Match
    } else {
        MatchDecision::NoMatch
    };

    FusedScore {
        fused_score,
        decision,
        fusion_method: method,
        modality_scores: scores.to_vec(),
        latency_us: start.elapsed().as_micros() as u64,
    }
}

/// Z-score normalization.
pub fn normalize_zscore(score: f64, params: &NormalizationParams) -> f64 {
    if params.std_dev < 1e-10 {
        return 0.5;
    }
    let z = (score - params.mean) / params.std_dev;
    // Sigmoid mapping to [0, 1]
    1.0 / (1.0 + (-z).exp())
}

/// Min-max normalization.
pub fn normalize_minmax(score: f64, params: &NormalizationParams) -> f64 {
    let range = params.max_val - params.min_val;
    if range < 1e-10 {
        return 0.5;
    }
    ((score - params.min_val) / range).clamp(0.0, 1.0)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_fingerprint_self_match() {
        let minutiae = vec![
            (100, 150, 45.0, 0u8),
            (120, 180, 90.0, 1),
            (80, 200, 135.0, 0),
            (150, 160, 270.0, 1),
        ];
        let result = match_fingerprint_minutiae(&minutiae, &minutiae, 0.4);
        assert_eq!(result.decision, MatchDecision::Match);
        assert!(result.score > 0.9);
    }

    #[test]
    fn test_face_self_match() {
        let embedding: Vec<f64> = (0..128).map(|i| (i as f64 * 0.01).sin()).collect();
        let result = match_face_embeddings(&embedding, &embedding, 0.45);
        assert_eq!(result.decision, MatchDecision::Match);
        assert!((result.score - 1.0).abs() < 1e-6);
    }

    #[test]
    fn test_iris_self_match() {
        let code: Vec<u8> = (0..256).map(|i| (i & 0xFF) as u8).collect();
        let mask = vec![0xFF; 256];
        let result = match_iris_codes(&code, &code, &mask, &mask, 0.32, 7);
        assert_eq!(result.decision, MatchDecision::Match);
        assert!((result.score - 1.0).abs() < 1e-6);
    }

    #[test]
    fn test_1n_identification() {
        let probe_face: Vec<f64> = (0..128).map(|i| (i as f64 * 0.01).sin()).collect();

        let gallery: Vec<(String, Option<Vec<(i32, i32, f64, u8)>>, Option<Vec<f64>>, Option<(Vec<u8>, Vec<u8>)>)> = (0..100)
            .map(|i| {
                let face: Vec<f64> = (0..128).map(|j| ((j + i) as f64 * 0.01).sin()).collect();
                (format!("voter-{}", i), None, Some(face), None)
            })
            .collect();

        let thresholds = IdentifyThresholds::default();
        let results = identify_1n(None, Some(&probe_face), None, &gallery, &thresholds);
        assert!(!results.is_empty());
        assert!(results[0].fused_score >= results.last().unwrap().fused_score);
    }

    #[test]
    fn test_score_fusion() {
        let scores = vec![
            MatchScore {
                probe_id: "p1".into(),
                gallery_id: "g1".into(),
                modality: "fingerprint".into(),
                score: 0.8,
                normalized_score: 0.8,
                decision: MatchDecision::Match,
                algorithm: "bozorth3".into(),
                latency_us: 100,
            },
            MatchScore {
                probe_id: "p1".into(),
                gallery_id: "g1".into(),
                modality: "face".into(),
                score: 0.7,
                normalized_score: 0.7,
                decision: MatchDecision::Match,
                algorithm: "cosine".into(),
                latency_us: 50,
            },
        ];

        let mut weights = std::collections::HashMap::new();
        weights.insert("fingerprint".into(), 0.5);
        weights.insert("face".into(), 0.5);

        let fused = fuse_scores(&scores, &weights, FusionMethod::WeightedSum);
        assert!((fused.fused_score - 0.75).abs() < 0.01);
    }

    #[test]
    fn test_zscore_normalization() {
        let params = NormalizationParams {
            mean: 0.5,
            std_dev: 0.2,
            min_val: 0.0,
            max_val: 1.0,
        };
        let norm = normalize_zscore(0.5, &params);
        assert!((norm - 0.5).abs() < 0.01);

        let high = normalize_zscore(0.9, &params);
        assert!(high > 0.85);
    }
}
