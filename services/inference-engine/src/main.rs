//! INEC High-Performance ML Inference Engine (Rust)
//!
//! Handles performance-critical ML inference paths:
//! - ONNX Runtime model execution (anomaly detection, liveness/PAD)
//! - Face embedding cosine similarity computation
//! - Neo4j graph queries for GNN feature extraction
//! - Batch inference for election-day load (176K+ polling units)
//!
//! Designed for CPU inference with <10ms latency per request.

use axum::{
    extract::{Json, State},
    http::{HeaderMap, StatusCode},
    routing::{get, post},
    Router,
};
use base64::{engine::general_purpose::STANDARD as BASE64_STANDARD, Engine as _};
use chrono::{DateTime, Utc};
use ed25519_dalek::{Signature, Signer, SigningKey, Verifier, VerifyingKey};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use subtle::ConstantTimeEq;
use std::{fs, path::Path, sync::Arc};
use tokio::sync::RwLock;
use tower_http::cors::CorsLayer;
use tracing::{info, warn};

mod models;
mod neo4j_client;

use models::{AnomalyModel, FaceModel, LivenessModel};
use neo4j_client::{GraphQueryResponse, Neo4jClient};

// ── Application State ──

#[derive(Clone, Serialize)]
struct ModelGovernance {
    model_id: String,
    version: String,
    sha256: Option<String>,
    approved: bool,
    reason: Option<String>,
}

struct IntegritySigner {
    signing_key: Option<SigningKey>,
    verifying_key: Option<VerifyingKey>,
    key_id: String,
    service_token: Option<String>,
}

impl IntegritySigner {
    fn load() -> Self {
        let key_id = std::env::var("INTEGRITY_SIGNER_KEY_ID")
            .unwrap_or_else(|_| "unconfigured".to_string());
        let service_token = std::env::var("INTEGRITY_SERVICE_TOKEN")
            .ok()
            .filter(|token| !token.trim().is_empty());
        let signing_key = std::env::var("INTEGRITY_SIGNING_KEY")
            .ok()
            .and_then(|encoded| BASE64_STANDARD.decode(encoded.trim()).ok())
            .and_then(|bytes| <[u8; 32]>::try_from(bytes.as_slice()).ok())
            .map(|bytes| SigningKey::from_bytes(&bytes));
        let verifying_key = if let Some(key) = signing_key.as_ref() {
            Some(key.verifying_key())
        } else {
            std::env::var("INTEGRITY_VERIFYING_KEY")
                .ok()
                .and_then(|encoded| BASE64_STANDARD.decode(encoded.trim()).ok())
                .and_then(|bytes| <[u8; 32]>::try_from(bytes.as_slice()).ok())
                .and_then(|bytes| VerifyingKey::from_bytes(&bytes).ok())
        };
        Self { signing_key, verifying_key, key_id, service_token }
    }

    fn signing_ready(&self) -> bool {
        self.signing_key.is_some() && self.service_token.is_some() && self.key_id != "unconfigured"
    }

    fn authorizes(&self, headers: &HeaderMap) -> bool {
        let Some(expected) = self.service_token.as_ref() else {
            return false;
        };
        let Some(header) = headers.get("authorization").and_then(|value| value.to_str().ok()) else {
            return false;
        };
        let Some(provided) = header.strip_prefix("Bearer ") else {
            return false;
        };
        let provided_bytes = provided.as_bytes();
        let expected_bytes = expected.as_bytes();
        provided_bytes.len() == expected_bytes.len()
            && bool::from(provided_bytes.ct_eq(expected_bytes))
    }
}

impl ModelGovernance {
    fn load(models_dir: &str) -> Self {
        let model_path = Path::new(models_dir).join("anomaly_xgboost.onnx");
        let manifest_path = Path::new(models_dir).join("anomaly_xgboost.manifest.json");
        let mut governance = Self {
            model_id: "anomaly_xgboost".into(),
            version: "unapproved".into(),
            sha256: None,
            approved: false,
            reason: Some("model manifest is missing".into()),
        };
        let model_bytes = match fs::read(&model_path) {
            Ok(bytes) => bytes,
            Err(error) => {
                governance.reason = Some(format!("model artifact unavailable: {error}"));
                return governance;
            }
        };
        let actual_hash = format!("{:x}", Sha256::digest(&model_bytes));
        governance.sha256 = Some(actual_hash.clone());
        let manifest_text = match fs::read_to_string(&manifest_path) {
            Ok(text) => text,
            Err(error) => {
                governance.reason = Some(format!("model manifest unavailable: {error}"));
                return governance;
            }
        };
        let manifest: serde_json::Value = match serde_json::from_str(&manifest_text) {
            Ok(value) => value,
            Err(error) => {
                governance.reason = Some(format!("model manifest is invalid JSON: {error}"));
                return governance;
            }
        };
        let model_id = manifest.get("model_id").and_then(|value| value.as_str()).unwrap_or("");
        let version = manifest.get("version").and_then(|value| value.as_str()).unwrap_or("");
        let declared_hash = manifest.get("sha256").and_then(|value| value.as_str()).unwrap_or("");
        let approved = manifest.get("approved_for_production").and_then(|value| value.as_bool()).unwrap_or(false);
        let validation_report = manifest.get("validation_report_uri").and_then(|value| value.as_str()).unwrap_or("");
        if model_id != "anomaly_xgboost" || version.is_empty() || validation_report.is_empty() {
            governance.reason = Some("model manifest is missing required identity or validation evidence".into());
            return governance;
        }
        governance.version = version.to_string();
        if declared_hash != actual_hash {
            governance.reason = Some("model artifact SHA-256 does not match approved manifest".into());
            return governance;
        }
        if !approved {
            governance.reason = Some("model is not explicitly approved for production".into());
            return governance;
        }
        governance.approved = true;
        governance.reason = None;
        governance
    }

    fn model_label(&self) -> String {
        format!("{}@{}", self.model_id, self.version)
    }
}

struct AppState {
    anomaly_model: Option<AnomalyModel>,
    anomaly_governance: ModelGovernance,
    face_model: Option<FaceModel>,
    liveness_model: Option<LivenessModel>,
    neo4j: Option<Neo4jClient>,
    integrity_signer: IntegritySigner,
}

impl AppState {
    async fn new() -> Self {
        let models_dir = std::env::var("MODELS_DIR")
            .unwrap_or_else(|_| "/app/models".to_string());

        let anomaly_governance = ModelGovernance::load(&models_dir);
        if !anomaly_governance.approved {
            warn!(reason = ?anomaly_governance.reason, "Anomaly model governance approval is unavailable");
        }
        let anomaly_model_path = format!("{}/anomaly_xgboost.onnx", models_dir);
        let anomaly_model = if Path::new(&anomaly_model_path).is_file() {
            AnomalyModel::load(&anomaly_model_path)
                .map_err(|e| warn!("Anomaly model not loaded: {}", e))
                .ok()
        } else {
            warn!(path = %anomaly_model_path, "Anomaly model file is absent; inference remains unavailable");
            None
        };

        let liveness_model_path = format!("{}/liveness_cdcn.onnx", models_dir);
        let liveness_model = if Path::new(&liveness_model_path).is_file() {
            LivenessModel::load(&liveness_model_path)
                .map_err(|e| warn!("Liveness model not loaded: {}", e))
                .ok()
        } else {
            warn!(path = %liveness_model_path, "Liveness model file is absent; liveness inference remains unavailable");
            None
        };

        let face_model = FaceModel::new()
            .map_err(|e| warn!("Face model not loaded: {}", e))
            .ok();

        let neo4j = Neo4jClient::connect().await
            .map_err(|e| warn!("Neo4j not connected: {}", e))
            .ok();
        let integrity_signer = IntegritySigner::load();
        if !integrity_signer.signing_ready() {
            warn!("Evidence integrity signing is not configured; production callers must fail closed");
        }

        info!(
            anomaly = anomaly_model.is_some(),
            face = face_model.is_some(),
            liveness = liveness_model.is_some(),
            neo4j = neo4j.is_some(),
            integrity_signer = integrity_signer.signing_ready(),
            "Inference engine initialized"
        );

        Self { anomaly_model, anomaly_governance, face_model, liveness_model, neo4j, integrity_signer }
    }
}

type SharedState = Arc<RwLock<AppState>>;

// ── Request/Response Types ──

#[derive(Deserialize)]
struct AnomalyRequest {
    registered_voters: u32,
    accredited_voters: u32,
    total_valid_votes: u32,
    rejected_votes: u32,
    party_a_votes: u32,
    party_b_votes: u32,
    #[serde(default = "default_delay")]
    submission_delay_hours: f64,
    #[serde(default = "default_turnout")]
    regional_mean_turnout: f64,
    #[serde(default)]
    benford_deviation: f64,
}

fn default_delay() -> f64 { 3.0 }
fn default_turnout() -> f64 { 0.55 }

#[derive(Serialize)]
struct AnomalyResponse {
    anomaly_score: f64,
    is_anomaly: bool,
    confidence: f64,
    risk_factors: Vec<RiskFactor>,
    model: String,
    inference_time_us: u64,
}

#[derive(Serialize)]
struct RiskFactor {
    factor: String,
    value: f64,
    severity: String,
}

#[derive(Deserialize)]
struct FaceCompareRequest {
    embedding_a: Vec<f32>,
    embedding_b: Vec<f32>,
    threshold: Option<f32>,
}

#[derive(Serialize)]
struct FaceCompareResponse {
    similarity: f32,
    verified: bool,
    threshold: f32,
    inference_time_us: u64,
}

#[derive(Deserialize)]
struct BatchAnomalyRequest {
    polling_units: Vec<AnomalyRequest>,
}

#[derive(Serialize)]
struct BatchAnomalyResponse {
    results: Vec<AnomalyResponse>,
    total_anomalies: usize,
    batch_inference_time_ms: f64,
}

#[derive(Deserialize)]
struct GraphQueryRequest {
    pu_code: String,
    hops: Option<u32>,
}

#[derive(Deserialize)]
struct IntegritySignRequest {
    payload_sha256: String,
}

#[derive(Serialize)]
struct IntegritySignResponse {
    payload_sha256: String,
    signature: String,
    key_id: String,
}

#[derive(Deserialize)]
struct IntegrityVerifyRequest {
    payload_sha256: String,
    signature: String,
    key_id: String,
}

#[derive(Serialize)]
struct IntegrityVerifyResponse {
    valid: bool,
    key_id: String,
}

#[derive(Clone, Deserialize, Serialize)]
struct DeviceEnvelope {
    version: String,
    device_id: String,
    election_id: i64,
    polling_unit_code: String,
    event_type: String,
    sequence: i64,
    nonce: String,
    observed_at: String,
    payload_sha256: String,
    payload: serde_json::Value,
    signature: String,
}

#[derive(Deserialize)]
struct DeviceEnvelopeVerifyRequest {
    envelope: DeviceEnvelope,
    public_key_base64: String,
    attestation_policy_version: String,
}

#[derive(Debug, Serialize)]
struct DeviceEnvelopeVerifyResponse {
    valid: bool,
    envelope_sha256: String,
    payload_sha256: String,
    attestation: serde_json::Value,
    reason: Option<String>,
}

#[derive(Deserialize)]
struct GpsSpoofRequest {
    device_id: String,
    current_lat: f64,
    current_lng: f64,
    previous_lat: Option<f64>,
    previous_lng: Option<f64>,
    time_delta_seconds: Option<f64>,
    accuracy: Option<f64>,
    altitude: Option<f64>,
    mock_provider: Option<bool>,
    jitter_samples: Option<Vec<f64>>,
    expected_lat: Option<f64>,
    expected_lng: Option<f64>,
    geofence_radius_m: Option<f64>,
}

#[derive(Serialize)]
struct GpsSpoofResponse {
    device_id: String,
    is_spoofed: bool,
    confidence: f64,
    indicators: Vec<SpoofIndicator>,
    distance_from_expected_m: Option<f64>,
    velocity_kmh: Option<f64>,
    inference_time_us: u64,
}

#[derive(Serialize)]
struct SpoofIndicator {
    check: String,
    result: String,
    severity: String,
    detail: String,
}

#[derive(Serialize)]
struct HealthResponse {
    status: String,
    models: ModelsStatus,
    inference_device: String,
}

#[derive(Serialize)]
struct ModelsStatus {
    anomaly_xgboost: bool,
    anomaly_governance: ModelGovernance,
    face_embeddings: bool,
    liveness_cdcn: bool,
    neo4j_connected: bool,
    integrity_signer: bool,
}

#[derive(Serialize)]
struct IntegritySignerHealthResponse {
    status: String,
    signing_ready: bool,
    verifying_ready: bool,
    key_id: Option<String>,
}

// ── Handlers ──

async fn health(State(state): State<SharedState>) -> (StatusCode, Json<HealthResponse>) {
    let s = state.read().await;
    let anomaly_ready = s.anomaly_model.is_some() && s.anomaly_governance.approved;
    let status_code = if anomaly_ready {
        StatusCode::OK
    } else {
        StatusCode::SERVICE_UNAVAILABLE
    };
    (
        status_code,
        Json(HealthResponse {
            status: if anomaly_ready { "healthy" } else { "degraded" }.into(),
            models: ModelsStatus {
                anomaly_xgboost: anomaly_ready,
                anomaly_governance: s.anomaly_governance.clone(),
                face_embeddings: s.face_model.is_some(),
                liveness_cdcn: s.liveness_model.is_some(),
                neo4j_connected: s.neo4j.is_some(),
                integrity_signer: s.integrity_signer.signing_ready(),
            },
            inference_device: "CPU".into(),
        }),
    )
}

async fn integrity_signer_health(
    State(state): State<SharedState>,
    headers: HeaderMap,
) -> (StatusCode, Json<IntegritySignerHealthResponse>) {
    let s = state.read().await;
    if !s.integrity_signer.authorizes(&headers) {
        return (
            StatusCode::UNAUTHORIZED,
            Json(IntegritySignerHealthResponse {
                status: "unauthorized".into(),
                signing_ready: false,
                verifying_ready: false,
                key_id: None,
            }),
        );
    }
    let signing_ready = s.integrity_signer.signing_ready();
    let verifying_ready = s.integrity_signer.verifying_key.is_some();
    let status = if signing_ready { "healthy" } else { "unavailable" };
    (
        if signing_ready { StatusCode::OK } else { StatusCode::SERVICE_UNAVAILABLE },
        Json(IntegritySignerHealthResponse {
            status: status.into(),
            signing_ready,
            verifying_ready,
            key_id: signing_ready.then(|| s.integrity_signer.key_id.clone()),
        }),
    )
}

async fn predict_anomaly(
    State(state): State<SharedState>,
    Json(req): Json<AnomalyRequest>,
) -> Result<Json<AnomalyResponse>, StatusCode> {
    let start = std::time::Instant::now();
    let s = state.read().await;

    let model = s.anomaly_model.as_ref()
        .filter(|_| s.anomaly_governance.approved)
        .ok_or(StatusCode::SERVICE_UNAVAILABLE)?;

    let turnout = req.accredited_voters as f64 / req.registered_voters.max(1) as f64;
    let features = vec![
        req.registered_voters as f64,
        req.accredited_voters as f64,
        turnout,
        req.total_valid_votes as f64,
        req.rejected_votes as f64,
        req.party_a_votes as f64,
        req.party_b_votes as f64,
        req.party_a_votes as f64 / req.total_valid_votes.max(1) as f64,
        req.party_b_votes as f64 / req.total_valid_votes.max(1) as f64,
        (req.party_a_votes as f64 - req.party_b_votes as f64).abs() / req.total_valid_votes.max(1) as f64,
        req.benford_deviation,
        req.submission_delay_hours,
        req.regional_mean_turnout,
        turnout - req.regional_mean_turnout,
        req.rejected_votes as f64 / req.accredited_voters.max(1) as f64,
        if req.total_valid_votes > req.accredited_voters { 1.0 } else { 0.0 },
        if req.total_valid_votes % 100 == 0 || req.total_valid_votes % 50 == 0 { 1.0 } else { 0.0 },
    ];

    let score = model
        .predict(&features)
        .map_err(|error| {
            warn!(error = %error, "approved anomaly model inference failed");
            StatusCode::SERVICE_UNAVAILABLE
        })?;
    let elapsed = start.elapsed().as_micros() as u64;

    let mut risk_factors = Vec::new();
    if turnout > 0.9 {
        risk_factors.push(RiskFactor {
            factor: "high_turnout".into(), value: turnout, severity: "high".into()
        });
    }
    if req.total_valid_votes > req.accredited_voters {
        risk_factors.push(RiskFactor {
            factor: "overvoting".into(),
            value: (req.total_valid_votes - req.accredited_voters) as f64,
            severity: "critical".into(),
        });
    }
    if req.benford_deviation > 0.05 {
        risk_factors.push(RiskFactor {
            factor: "benford_violation".into(), value: req.benford_deviation, severity: "medium".into()
        });
    }

    Ok(Json(AnomalyResponse {
        anomaly_score: score,
        is_anomaly: score > 0.5,
        confidence: (score - 0.5).abs() * 2.0,
        risk_factors,
        model: s.anomaly_governance.model_label(),
        inference_time_us: elapsed,
    }))
}

async fn batch_predict(
    State(state): State<SharedState>,
    Json(req): Json<BatchAnomalyRequest>,
) -> Result<Json<BatchAnomalyResponse>, StatusCode> {
    let start = std::time::Instant::now();
    let s = state.read().await;

    let model = s.anomaly_model.as_ref()
        .filter(|_| s.anomaly_governance.approved)
        .ok_or(StatusCode::SERVICE_UNAVAILABLE)?;

    let mut results = Vec::with_capacity(req.polling_units.len());
    let mut total_anomalies = 0;

    for pu in &req.polling_units {
        let turnout = pu.accredited_voters as f64 / pu.registered_voters.max(1) as f64;
        let features = vec![
            pu.registered_voters as f64,
            pu.accredited_voters as f64,
            turnout,
            pu.total_valid_votes as f64,
            pu.rejected_votes as f64,
            pu.party_a_votes as f64,
            pu.party_b_votes as f64,
            pu.party_a_votes as f64 / pu.total_valid_votes.max(1) as f64,
            pu.party_b_votes as f64 / pu.total_valid_votes.max(1) as f64,
            (pu.party_a_votes as f64 - pu.party_b_votes as f64).abs() / pu.total_valid_votes.max(1) as f64,
            pu.benford_deviation,
            pu.submission_delay_hours,
            pu.regional_mean_turnout,
            turnout - pu.regional_mean_turnout,
            pu.rejected_votes as f64 / pu.accredited_voters.max(1) as f64,
            if pu.total_valid_votes > pu.accredited_voters { 1.0 } else { 0.0 },
            if pu.total_valid_votes % 100 == 0 || pu.total_valid_votes % 50 == 0 { 1.0 } else { 0.0 },
        ];

        let score = model
        .predict(&features)
        .map_err(|error| {
            warn!(error = %error, "approved anomaly model inference failed");
            StatusCode::SERVICE_UNAVAILABLE
        })?;
        let is_anomaly = score > 0.5;
        if is_anomaly { total_anomalies += 1; }

        results.push(AnomalyResponse {
            anomaly_score: score,
            is_anomaly,
            confidence: (score - 0.5).abs() * 2.0,
            risk_factors: vec![],
            model: s.anomaly_governance.model_label(),
            inference_time_us: 0,
        });
    }

    let elapsed = start.elapsed().as_secs_f64() * 1000.0;

    Ok(Json(BatchAnomalyResponse {
        results,
        total_anomalies,
        batch_inference_time_ms: elapsed,
    }))
}

async fn compare_faces(
    State(state): State<SharedState>,
    Json(req): Json<FaceCompareRequest>,
) -> Result<Json<FaceCompareResponse>, StatusCode> {
    let start = std::time::Instant::now();
    let s = state.read().await;

    let _face_model = s.face_model.as_ref()
        .ok_or(StatusCode::SERVICE_UNAVAILABLE)?;

    if req.embedding_a.len() != 512 || req.embedding_b.len() != 512 {
        return Err(StatusCode::BAD_REQUEST);
    }

    // Cosine similarity via nalgebra
    let dot: f32 = req
        .embedding_a
        .iter()
        .zip(&req.embedding_b)
        .map(|(left, right)| left * right)
        .sum();
    let norm_a = req.embedding_a.iter().map(|value| value * value).sum::<f32>().sqrt();
    let norm_b = req.embedding_b.iter().map(|value| value * value).sum::<f32>().sqrt();
    let similarity = dot / (norm_a * norm_b).max(1e-10);

    let threshold = req.threshold.unwrap_or(0.4);
    let elapsed = start.elapsed().as_micros() as u64;

    Ok(Json(FaceCompareResponse {
        similarity,
        verified: similarity >= threshold,
        threshold,
        inference_time_us: elapsed,
    }))
}

// Haversine distance in meters
fn haversine_m(lat1: f64, lng1: f64, lat2: f64, lng2: f64) -> f64 {
    let r = 6_371_000.0; // Earth radius in meters
    let d_lat = (lat2 - lat1).to_radians();
    let d_lng = (lng2 - lng1).to_radians();
    let a = (d_lat / 2.0).sin().powi(2)
        + lat1.to_radians().cos() * lat2.to_radians().cos() * (d_lng / 2.0).sin().powi(2);
    let c = 2.0 * a.sqrt().asin();
    r * c
}

async fn detect_gps_spoof(
    Json(req): Json<GpsSpoofRequest>,
) -> Json<GpsSpoofResponse> {
    let start = std::time::Instant::now();
    let mut indicators = Vec::new();
    let mut spoof_score: f64 = 0.0;

    // 1. Mock provider detection
    if req.mock_provider.unwrap_or(false) {
        indicators.push(SpoofIndicator {
            check: "mock_provider".into(),
            result: "FAIL".into(),
            severity: "critical".into(),
            detail: "Device reports mock location provider active".into(),
        });
        spoof_score += 0.9;
    }

    // 2. Accuracy check (GPS accuracy > 100m is suspicious)
    if let Some(acc) = req.accuracy {
        if acc > 100.0 || acc <= 0.0 {
            indicators.push(SpoofIndicator {
                check: "accuracy".into(),
                result: "FAIL".into(),
                severity: "high".into(),
                detail: format!("GPS accuracy {}m is outside normal range (1-100m)", acc),
            });
            spoof_score += 0.3;
        }
    }

    // 3. Altitude check (Nigeria max altitude ~2,419m at Chappal Waddi)
    if let Some(alt) = req.altitude {
        if alt < -50.0 || alt > 3000.0 {
            indicators.push(SpoofIndicator {
                check: "altitude".into(),
                result: "FAIL".into(),
                severity: "medium".into(),
                detail: format!("Altitude {}m is outside Nigeria range (-50 to 3000)", alt),
            });
            spoof_score += 0.3;
        }
    }

    // 4. Velocity check (teleportation detection)
    let mut velocity_kmh = None;
    if let (Some(prev_lat), Some(prev_lng), Some(dt)) =
        (req.previous_lat, req.previous_lng, req.time_delta_seconds)
    {
        if dt > 0.0 {
            let dist_m = haversine_m(prev_lat, prev_lng, req.current_lat, req.current_lng);
            let v = (dist_m / dt) * 3.6; // m/s to km/h
            velocity_kmh = Some(v);

            if v > 500.0 {
                indicators.push(SpoofIndicator {
                    check: "teleportation".into(),
                    result: "FAIL".into(),
                    severity: "critical".into(),
                    detail: format!("Velocity {:.0} km/h exceeds 500 km/h threshold (distance {:.0}m in {:.0}s)", v, dist_m, dt),
                });
                spoof_score += 0.8;
            } else if v > 200.0 {
                indicators.push(SpoofIndicator {
                    check: "high_velocity".into(),
                    result: "WARN".into(),
                    severity: "medium".into(),
                    detail: format!("Velocity {:.0} km/h is unusually high", v),
                });
                spoof_score += 0.2;
            }
        }
    }

    // 5. Geofence check (distance from expected polling unit location)
    let mut distance_from_expected = None;
    if let (Some(exp_lat), Some(exp_lng)) = (req.expected_lat, req.expected_lng) {
        let dist = haversine_m(req.current_lat, req.current_lng, exp_lat, exp_lng);
        distance_from_expected = Some(dist);
        let radius = req.geofence_radius_m.unwrap_or(500.0);

        if dist > radius {
            indicators.push(SpoofIndicator {
                check: "geofence".into(),
                result: "FAIL".into(),
                severity: "high".into(),
                detail: format!("Device is {:.0}m from expected location (radius: {:.0}m)", dist, radius),
            });
            spoof_score += 0.5;
        }
    }

    // 6. Jitter analysis (zero jitter = emulated GPS)
    if let Some(ref samples) = req.jitter_samples {
        if samples.len() >= 3 {
            let mean = samples.iter().sum::<f64>() / samples.len() as f64;
            let variance = samples.iter().map(|s| (s - mean).powi(2)).sum::<f64>() / samples.len() as f64;
            let std_dev = variance.sqrt();

            if std_dev < 0.0001 {
                indicators.push(SpoofIndicator {
                    check: "jitter".into(),
                    result: "FAIL".into(),
                    severity: "high".into(),
                    detail: format!("GPS jitter std_dev={:.6} suggests emulated/static GPS", std_dev),
                });
                spoof_score += 0.4;
            }
        }
    }

    let confidence = spoof_score.min(1.0);
    let is_spoofed = confidence > 0.5;
    let elapsed = start.elapsed().as_micros() as u64;

    Json(GpsSpoofResponse {
        device_id: req.device_id,
        is_spoofed,
        confidence,
        indicators,
        distance_from_expected_m: distance_from_expected,
        velocity_kmh,
        inference_time_us: elapsed,
    })
}

fn valid_sha256(value: &str) -> bool {
    value.len() == 64 && value.bytes().all(|byte| byte.is_ascii_hexdigit())
}

async fn sign_integrity(
    State(state): State<SharedState>,
    headers: HeaderMap,
    Json(req): Json<IntegritySignRequest>,
) -> Result<Json<IntegritySignResponse>, StatusCode> {
    if !valid_sha256(&req.payload_sha256) {
        return Err(StatusCode::BAD_REQUEST);
    }
    let state = state.read().await;
    if !state.integrity_signer.authorizes(&headers) {
        return Err(StatusCode::UNAUTHORIZED);
    }
    let signing_key = state.integrity_signer.signing_key.as_ref().ok_or(StatusCode::SERVICE_UNAVAILABLE)?;
    if state.integrity_signer.key_id == "unconfigured" {
        return Err(StatusCode::SERVICE_UNAVAILABLE);
    }
    let signature = signing_key.sign(req.payload_sha256.as_bytes());
    Ok(Json(IntegritySignResponse {
        payload_sha256: req.payload_sha256,
        signature: BASE64_STANDARD.encode(signature.to_bytes()),
        key_id: state.integrity_signer.key_id.clone(),
    }))
}

async fn verify_integrity(
    State(state): State<SharedState>,
    headers: HeaderMap,
    Json(req): Json<IntegrityVerifyRequest>,
) -> Result<Json<IntegrityVerifyResponse>, StatusCode> {
    if !valid_sha256(&req.payload_sha256) || req.signature.trim().is_empty() {
        return Err(StatusCode::BAD_REQUEST);
    }
    let state = state.read().await;
    if !state.integrity_signer.authorizes(&headers) {
        return Err(StatusCode::UNAUTHORIZED);
    }
    let verifying_key = state.integrity_signer.verifying_key.as_ref().ok_or(StatusCode::SERVICE_UNAVAILABLE)?;
    let signature_bytes = BASE64_STANDARD.decode(req.signature.trim()).map_err(|_| StatusCode::BAD_REQUEST)?;
    let signature_array = <[u8; 64]>::try_from(signature_bytes.as_slice()).map_err(|_| StatusCode::BAD_REQUEST)?;
    let signature = Signature::from_bytes(&signature_array);
    let key_matches = req.key_id == state.integrity_signer.key_id;
    let valid = key_matches && verifying_key.verify(req.payload_sha256.as_bytes(), &signature).is_ok();
    Ok(Json(IntegrityVerifyResponse { valid, key_id: state.integrity_signer.key_id.clone() }))
}

fn canonical_json(value: &serde_json::Value) -> Result<String, String> {
    match value {
        serde_json::Value::Null | serde_json::Value::Bool(_) | serde_json::Value::Number(_) | serde_json::Value::String(_) => {
            serde_json::to_string(value).map_err(|error| error.to_string())
        }
        serde_json::Value::Array(values) => {
            let members = values
                .iter()
                .map(canonical_json)
                .collect::<Result<Vec<_>, _>>()?;
            Ok(format!("[{}]", members.join(",")))
        }
        serde_json::Value::Object(values) => {
            let mut entries = values.iter().collect::<Vec<_>>();
            entries.sort_by(|left, right| left.0.cmp(right.0));
            let members = entries
                .into_iter()
                .map(|(key, value)| {
                    let encoded_key = serde_json::to_string(key).map_err(|error| error.to_string())?;
                    Ok(format!("{}:{}", encoded_key, canonical_json(value)?))
                })
                .collect::<Result<Vec<_>, String>>()?;
            Ok(format!("{{{}}}", members.join(",")))
        }
    }
}

fn canonical_device_envelope(envelope: &DeviceEnvelope) -> Result<Vec<u8>, String> {
    let canonical_payload = canonical_json(&envelope.payload)?;
    let fields = vec![
        envelope.version.clone(),
        envelope.device_id.clone(),
        envelope.election_id.to_string(),
        envelope.polling_unit_code.clone(),
        envelope.event_type.clone(),
        envelope.sequence.to_string(),
        envelope.nonce.clone(),
        envelope.observed_at.clone(),
        envelope.payload_sha256.to_ascii_lowercase(),
        canonical_payload,
    ];
    Ok(fields.join("\\n").into_bytes())
}

fn valid_device_event_type(event_type: &str) -> bool {
    matches!(event_type, "accreditation" | "result_capture" | "heartbeat" | "incident")
}

fn contains_prohibited_device_field(value: &serde_json::Value) -> bool {
    const PROHIBITED: [&str; 6] = [
        "voter_pvc_number",
        "biometric_template",
        "fingerprint_image",
        "face_embedding",
        "raw_image",
        "result_image",
    ];
    match value {
        serde_json::Value::Object(entries) => entries.iter().any(|(key, nested)| {
            PROHIBITED.contains(&key.as_str()) || contains_prohibited_device_field(nested)
        }),
        serde_json::Value::Array(entries) => entries.iter().any(contains_prohibited_device_field),
        _ => false,
    }
}

fn verify_device_envelope_payload(
    request: &DeviceEnvelopeVerifyRequest,
    signer: &IntegritySigner,
) -> Result<DeviceEnvelopeVerifyResponse, (StatusCode, String)> {
    let envelope = &request.envelope;
    if envelope.version != "bvas-envelope-v1"
        || envelope.device_id.trim().is_empty()
        || envelope.election_id <= 0
        || envelope.polling_unit_code.trim().is_empty()
        || envelope.sequence <= 0
        || !valid_device_event_type(&envelope.event_type)
        || !valid_sha256(&envelope.payload_sha256)
        || request.attestation_policy_version.trim().is_empty()
    {
        return Err((StatusCode::BAD_REQUEST, "invalid device envelope shape".into()));
    }
    let observed = DateTime::parse_from_rfc3339(&envelope.observed_at)
        .map_err(|_| (StatusCode::BAD_REQUEST, "observed_at must be RFC3339".into()))?
        .with_timezone(&Utc);
    let skew_seconds = (Utc::now() - observed).num_seconds().abs();
    if skew_seconds > 600 {
        return Err((StatusCode::UNPROCESSABLE_ENTITY, "device timestamp outside permitted clock skew".into()));
    }
    if contains_prohibited_device_field(&envelope.payload) {
        return Err((StatusCode::UNPROCESSABLE_ENTITY, "payload contains prohibited sensitive device field".into()));
    }
    let canonical_payload = canonical_json(&envelope.payload)
        .map_err(|_| (StatusCode::BAD_REQUEST, "payload cannot be canonicalized".into()))?;
    let calculated_payload_hash = format!("{:x}", Sha256::digest(canonical_payload.as_bytes()));
    if calculated_payload_hash != envelope.payload_sha256.to_ascii_lowercase() {
        return Err((StatusCode::UNPROCESSABLE_ENTITY, "payload SHA-256 mismatch".into()));
    }
    let public_key_bytes = BASE64_STANDARD
        .decode(request.public_key_base64.trim())
        .map_err(|_| (StatusCode::BAD_REQUEST, "invalid enrolled device public key".into()))?;
    let public_key_array = <[u8; 32]>::try_from(public_key_bytes.as_slice())
        .map_err(|_| (StatusCode::BAD_REQUEST, "invalid enrolled device public key length".into()))?;
    let verifying_key = VerifyingKey::from_bytes(&public_key_array)
        .map_err(|_| (StatusCode::BAD_REQUEST, "invalid enrolled device public key".into()))?;
    let signature_bytes = BASE64_STANDARD
        .decode(envelope.signature.trim())
        .map_err(|_| (StatusCode::BAD_REQUEST, "invalid device signature encoding".into()))?;
    let signature_array = <[u8; 64]>::try_from(signature_bytes.as_slice())
        .map_err(|_| (StatusCode::BAD_REQUEST, "invalid device signature length".into()))?;
    let signature = Signature::from_bytes(&signature_array);
    let canonical_envelope = canonical_device_envelope(envelope)
        .map_err(|_| (StatusCode::BAD_REQUEST, "envelope cannot be canonicalized".into()))?;
    verifying_key
        .verify(&canonical_envelope, &signature)
        .map_err(|_| (StatusCode::UNPROCESSABLE_ENTITY, "device signature verification failed".into()))?;
    let envelope_sha256 = format!("{:x}", Sha256::digest(&canonical_envelope));
    let signing_key = signer.signing_key.as_ref().ok_or((StatusCode::SERVICE_UNAVAILABLE, "integrity attestation signer is unavailable".into()))?;
    if signer.key_id == "unconfigured" {
        return Err((StatusCode::SERVICE_UNAVAILABLE, "integrity attestation key is unavailable".into()));
    }
    let verified_at = Utc::now().to_rfc3339();
    let attestation_payload = format!(
        "device-attestation-v1\\n{}\\n{}\\n{}\\n{}\\n{}",
        envelope_sha256,
        envelope.payload_sha256.to_ascii_lowercase(),
        request.attestation_policy_version,
        signer.key_id,
        verified_at
    );
    let attestation_signature = signing_key.sign(attestation_payload.as_bytes());
    let attestation = serde_json::json!({
        "version": "device-attestation-v1",
        "envelope_sha256": envelope_sha256,
        "payload_sha256": envelope.payload_sha256.to_ascii_lowercase(),
        "attestation_policy_version": request.attestation_policy_version,
        "key_id": signer.key_id,
        "verified_at": verified_at,
        "signature": BASE64_STANDARD.encode(attestation_signature.to_bytes())
    });
    Ok(DeviceEnvelopeVerifyResponse {
        valid: true,
        envelope_sha256,
        payload_sha256: envelope.payload_sha256.to_ascii_lowercase(),
        attestation,
        reason: None,
    })
}

async fn verify_device_envelope(
    State(state): State<SharedState>,
    headers: HeaderMap,
    Json(request): Json<DeviceEnvelopeVerifyRequest>,
) -> (StatusCode, Json<DeviceEnvelopeVerifyResponse>) {
    let signer_state = state.read().await;
    if !signer_state.integrity_signer.authorizes(&headers) {
        return (
            StatusCode::UNAUTHORIZED,
            Json(DeviceEnvelopeVerifyResponse {
                valid: false,
                envelope_sha256: String::new(),
                payload_sha256: String::new(),
                attestation: serde_json::json!({}),
                reason: Some("unauthorized".into()),
            }),
        );
    }
    match verify_device_envelope_payload(&request, &signer_state.integrity_signer) {
        Ok(response) => (StatusCode::OK, Json(response)),
        Err((status, reason)) => (
            status,
            Json(DeviceEnvelopeVerifyResponse {
                valid: false,
                envelope_sha256: String::new(),
                payload_sha256: request.envelope.payload_sha256,
                attestation: serde_json::json!({}),
                reason: Some(reason),
            }),
        ),
    }
}

async fn query_graph(
    State(state): State<SharedState>,
    Json(req): Json<GraphQueryRequest>,
) -> Result<Json<GraphQueryResponse>, StatusCode> {
    let s = state.read().await;
    let neo4j = s.neo4j.as_ref()
        .ok_or(StatusCode::SERVICE_UNAVAILABLE)?;

    let hops = req.hops.unwrap_or(2);
    let result = neo4j.get_neighborhood(&req.pu_code, hops).await
        .map_err(|_| StatusCode::INTERNAL_SERVER_ERROR)?;

    Ok(Json(result))
}

// ── Main ──

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "info".into()),
        )
        .init();

    let state: SharedState = Arc::new(RwLock::new(AppState::new().await));

    let app = Router::new()
        .route("/health", get(health))
        .route("/anomaly/predict", post(predict_anomaly))
        .route("/anomaly/batch", post(batch_predict))
        .route("/face/compare", post(compare_faces))
        .route("/graph/neighborhood", post(query_graph))
        .route("/gps/spoof-detect", post(detect_gps_spoof))
        .route("/integrity/sign", post(sign_integrity))
        .route("/integrity/verify", post(verify_integrity))
        .route("/integrity/device/verify", post(verify_device_envelope))
        .route("/integrity/health", get(integrity_signer_health).post(integrity_signer_health))
        .layer(CorsLayer::permissive())
        .with_state(state);

    let port = std::env::var("PORT").unwrap_or_else(|_| "8091".to_string());
    let addr = format!("0.0.0.0:{}", port);
    info!("INEC Inference Engine starting on {}", addr);

    let listener = tokio::net::TcpListener::bind(&addr).await.unwrap();
    axum::serve(listener, app).await.unwrap();
}

#[cfg(test)]
mod integrity_tests {
    use super::*;
    use axum::http::{HeaderMap, HeaderValue};

    #[test]
    fn accepts_only_canonical_sha256_hex() {
        assert!(valid_sha256(&"a".repeat(64)));
        assert!(valid_sha256(&"F".repeat(64)));
        assert!(!valid_sha256("short"));
        assert!(!valid_sha256(&"z".repeat(64)));
    }

    #[test]
    fn requires_exact_bearer_token_for_integrity_operations() {
        let signing_key = SigningKey::from_bytes(&[17_u8; 32]);
        let signer = IntegritySigner {
            verifying_key: Some(signing_key.verifying_key()),
            signing_key: Some(signing_key),
            key_id: "inec-test-key".to_string(),
            service_token: Some("service-secret".to_string()),
        };
        let mut accepted = HeaderMap::new();
        accepted.insert("authorization", HeaderValue::from_static("Bearer service-secret"));
        assert!(signer.authorizes(&accepted));

        let mut rejected = HeaderMap::new();
        rejected.insert("authorization", HeaderValue::from_static("Bearer different-secret"));
        assert!(!signer.authorizes(&rejected));
        assert!(!signer.authorizes(&HeaderMap::new()));
    }

    #[test]
    fn ed25519_integrity_signature_round_trip_verifies() {
        let signing_key = SigningKey::from_bytes(&[23_u8; 32]);
        let verifying_key = signing_key.verifying_key();
        let digest = "a2d7798c9f4f094c0f7dbbd6f1d9b58975523ffcc5e6980b57eae3dac0dbcb81";
        let signature = signing_key.sign(digest.as_bytes());
        assert!(verifying_key.verify(digest.as_bytes(), &signature).is_ok());
        assert!(verifying_key.verify(b"different digest", &signature).is_err());
    }

    fn signed_device_request(payload: serde_json::Value) -> DeviceEnvelopeVerifyRequest {
        let device_key = SigningKey::from_bytes(&[31_u8; 32]);
        let canonical_payload = canonical_json(&payload).expect("canonical payload");
        let payload_sha256 = format!("{:x}", Sha256::digest(canonical_payload.as_bytes()));
        let mut envelope = DeviceEnvelope {
            version: "bvas-envelope-v1".into(),
            device_id: "BVAS-00001".into(),
            election_id: 1,
            polling_unit_code: "PU-001".into(),
            event_type: "accreditation".into(),
            sequence: 1,
            nonce: BASE64_STANDARD.encode([9_u8; 32]),
            observed_at: Utc::now().to_rfc3339(),
            payload_sha256,
            payload,
            signature: String::new(),
        };
        let bytes = canonical_device_envelope(&envelope).expect("canonical envelope");
        envelope.signature = BASE64_STANDARD.encode(device_key.sign(&bytes).to_bytes());
        DeviceEnvelopeVerifyRequest {
            envelope,
            public_key_base64: BASE64_STANDARD.encode(device_key.verifying_key().to_bytes()),
            attestation_policy_version: "device-policy-v1".into(),
        }
    }

    #[test]
    fn device_envelope_verifies_and_returns_signed_attestation() {
        let signer_key = SigningKey::from_bytes(&[41_u8; 32]);
        let signer = IntegritySigner {
            signing_key: Some(signer_key),
            verifying_key: None,
            key_id: "integrity-device-test-key".into(),
            service_token: Some("service-secret".into()),
        };
        let request = signed_device_request(serde_json::json!({
            "voter_pvc_hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            "biometric_match": true,
            "pvc_verified": true,
            "method": "biometric"
        }));
        let response = verify_device_envelope_payload(&request, &signer).expect("valid device envelope");
        assert!(response.valid);
        assert!(valid_sha256(&response.envelope_sha256));
        assert!(response.attestation.get("signature").is_some());
    }

    #[test]
    fn device_envelope_rejects_prohibited_sensitive_payload() {
        let signer_key = SigningKey::from_bytes(&[41_u8; 32]);
        let signer = IntegritySigner {
            signing_key: Some(signer_key),
            verifying_key: None,
            key_id: "integrity-device-test-key".into(),
            service_token: Some("service-secret".into()),
        };
        let request = signed_device_request(serde_json::json!({"voter_pvc_number": "not-permitted"}));
        let error = verify_device_envelope_payload(&request, &signer).expect_err("sensitive field must fail");
        assert_eq!(error.0, StatusCode::UNPROCESSABLE_ENTITY);
    }
}
