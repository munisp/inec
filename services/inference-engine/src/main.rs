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
use ndarray::{Array1, Array2};
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tokio::sync::RwLock;
use tower_http::cors::CorsLayer;
use tracing::{info, warn};

mod models;
mod neo4j_client;

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

        let neo4j = Neo4jClient::connect().await
            .map_err(|e| warn!("Neo4j not connected: {}", e))
            .ok();

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

#[derive(Serialize)]
struct GraphQueryResponse {
    pu_code: String,
    neighbors: Vec<NeighborInfo>,
    avg_neighbor_turnout: f64,
    deviation_from_neighbors: f64,
    flagged_neighbors: u32,
}

#[derive(Serialize)]
struct NeighborInfo {
    code: String,
    turnout: f64,
    distance_km: f64,
    flagged: bool,
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
    let s = state.read().await;

    let model = s.anomaly_model.as_ref()
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

        let score = model.predict(&features);
        let is_anomaly = score > 0.5;
        if is_anomaly { total_anomalies += 1; }

        results.push(AnomalyResponse {
            anomaly_score: score,
            is_anomaly,
            confidence: (score - 0.5).abs() * 2.0,
            risk_factors: vec![],
            model: "xgboost-onnx-v1.0-batch".into(),
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
        .layer(CorsLayer::permissive())
        .with_state(state);

    let port = std::env::var("PORT").unwrap_or_else(|_| "8091".to_string());
    let addr = format!("0.0.0.0:{}", port);
    info!("INEC Inference Engine starting on {}", addr);

    let listener = tokio::net::TcpListener::bind(&addr).await.unwrap();
    axum::serve(listener, app).await.unwrap();
}
