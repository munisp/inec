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
    http::StatusCode,
    routing::{get, post},
    Router,
};
use ndarray::Array1;
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tokio::sync::RwLock;
use tower_http::cors::CorsLayer;
use tracing::{info, warn};

mod models;
mod neo4j_client;
pub mod service_client;

use models::{AnomalyModel, FaceModel, LivenessModel};
use neo4j_client::Neo4jClient;

// ── Application State ──

struct AppState {
    anomaly_model: Option<AnomalyModel>,
    face_model: Option<FaceModel>,
    liveness_model: Option<LivenessModel>,
    neo4j: Option<Neo4jClient>,
}

impl AppState {
    async fn new() -> Self {
        let models_dir = std::env::var("MODELS_DIR")
            .unwrap_or_else(|_| "/app/models".to_string());

        let anomaly_model = AnomalyModel::load(&format!("{}/anomaly_xgboost.onnx", models_dir))
            .map_err(|e| warn!("Anomaly model not loaded: {}", e))
            .ok();

        let liveness_model = LivenessModel::load(&format!("{}/liveness_cdcn.onnx", models_dir))
            .map_err(|e| warn!("Liveness model not loaded: {}", e))
            .ok();

        let face_model = FaceModel::new()
            .map_err(|e| warn!("Face model not loaded: {}", e))
            .ok();

        let neo4j = match tokio::time::timeout(
            std::time::Duration::from_secs(10),
            Neo4jClient::connect(),
        ).await {
            Ok(Ok(client)) => Some(client),
            Ok(Err(e)) => { warn!("Neo4j not connected: {}", e); None }
            Err(_) => { warn!("Neo4j connection timed out after 10s"); None }
        };

        info!(
            anomaly = anomaly_model.is_some(),
            face = face_model.is_some(),
            liveness = liveness_model.is_some(),
            neo4j = neo4j.is_some(),
            "Inference engine initialized"
        );

        Self { anomaly_model, face_model, liveness_model, neo4j }
    }
}

type SharedState = Arc<RwLock<AppState>>;

// ── Request/Response Types ──

#[derive(Deserialize, Clone)]
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

use neo4j_client::GraphQueryResponse;

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
    face_embeddings: bool,
    liveness_cdcn: bool,
    neo4j_connected: bool,
}

// ── Handlers ──

async fn health(State(state): State<SharedState>) -> Json<HealthResponse> {
    let s = state.read().await;
    Json(HealthResponse {
        status: "healthy".into(),
        models: ModelsStatus {
            anomaly_xgboost: s.anomaly_model.is_some(),
            face_embeddings: s.face_model.is_some(),
            liveness_cdcn: s.liveness_model.is_some(),
            neo4j_connected: s.neo4j.is_some(),
        },
        inference_device: "CPU".into(),
    })
}

async fn predict_anomaly(
    State(state): State<SharedState>,
    Json(req): Json<AnomalyRequest>,
) -> Result<Json<AnomalyResponse>, StatusCode> {
    let start = std::time::Instant::now();
    let s = state.read().await;

    let model = s.anomaly_model.as_ref()
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

    let score = model.predict(&features);
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
        model: "xgboost-onnx-v1.0".into(),
        inference_time_us: elapsed,
    }))
}

async fn batch_predict(
    State(state): State<SharedState>,
    Json(req): Json<BatchAnomalyRequest>,
) -> Result<Json<BatchAnomalyResponse>, StatusCode> {
    let start = std::time::Instant::now();

    // Verify model is available before spawning tasks
    {
        let s = state.read().await;
        if s.anomaly_model.is_none() {
            return Err(StatusCode::SERVICE_UNAVAILABLE);
        }
    }

    // Process polling units in parallel using tokio tasks, chunked to limit concurrency
    let chunk_size = 100;
    let chunks: Vec<Vec<AnomalyRequest>> = req.polling_units
        .chunks(chunk_size)
        .map(|c| c.to_vec())
        .collect();

    let mut handles = Vec::with_capacity(chunks.len());
    for chunk in chunks {
        let state_clone = state.clone();
        handles.push(tokio::spawn(async move {
            let s = state_clone.read().await;
            let model = s.anomaly_model.as_ref().unwrap();
            let mut chunk_results = Vec::with_capacity(chunk.len());

            for pu in &chunk {
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

                let score = model.predict(&features);
                let is_anomaly = score > 0.5;
                chunk_results.push(AnomalyResponse {
                    anomaly_score: score,
                    is_anomaly,
                    confidence: (score - 0.5).abs() * 2.0,
                    risk_factors: vec![],
                    model: "xgboost-onnx-v1.0-batch-parallel".into(),
                    inference_time_us: 0,
                });
            }
            chunk_results
        }));
    }

    // Collect results from all parallel tasks in order
    let mut results = Vec::with_capacity(req.polling_units.len());
    for handle in handles {
        match handle.await {
            Ok(chunk_results) => results.extend(chunk_results),
            Err(e) => {
                warn!("Batch task failed: {}", e);
                return Err(StatusCode::INTERNAL_SERVER_ERROR);
            }
        }
    }

    let total_anomalies = results.iter().filter(|r| r.is_anomaly).count();
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
    let a = Array1::from_vec(req.embedding_a.clone());
    let b = Array1::from_vec(req.embedding_b.clone());

    let dot = a.dot(&b);
    let norm_a = a.dot(&a).sqrt();
    let norm_b = b.dot(&b).sqrt();
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

    info!("Starting initialization...");

    if std::env::var("ORT_DYLIB_PATH").is_err() {
        warn!("ORT_DYLIB_PATH not set — ONNX models will be unavailable");
    }

    let state: SharedState = Arc::new(RwLock::new(AppState::new().await));
    info!("Initialization complete");

    let app = Router::new()
        .route("/health", get(health))
        .route("/anomaly/predict", post(predict_anomaly))
        .route("/anomaly/batch", post(batch_predict))
        .route("/face/compare", post(compare_faces))
        .route("/graph/neighborhood", post(query_graph))
        .route("/gps/spoof-detect", post(detect_gps_spoof))
        .layer(CorsLayer::permissive())
        .with_state(state);

    let port = std::env::var("PORT").unwrap_or_else(|_| "8091".to_string());
    let addr = format!("0.0.0.0:{}", port);
    info!("INEC Inference Engine starting on {}", addr);

    let listener = tokio::net::TcpListener::bind(&addr).await.unwrap();
    axum::serve(listener, app).await.unwrap();
}
