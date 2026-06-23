//! INEC Biometric Vault & Matching Service (Rust).
//!
//! Production service providing:
//! - AES-256-GCM template encryption/decryption
//! - Cancelable biometrics (BioHashing)
//! - High-speed 1:N parallel matching
//! - Score fusion (weighted sum, max, product)
//! - Key management (generate, rotate, revoke)
//! - Full audit trail

mod cancelable;
mod matching;
mod vault;

use axum::{
    extract::Json,
    http::StatusCode,
    response::IntoResponse,
    routing::{get, post},
    Router,
};
use base64::Engine as _;
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tower_http::cors::CorsLayer;
use tracing_subscriber::EnvFilter;

use cancelable::{CancelableBiometrics, TransformType};
use matching::{
    fuse_scores, match_face_embeddings, match_fingerprint_minutiae, match_iris_codes,
    FusionMethod, IdentifyThresholds, MatchDecision,
};
use vault::BiometricVault;

struct AppState {
    vault: BiometricVault,
    cancelable: CancelableBiometrics,
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env().add_directive("info".parse().unwrap()))
        .json()
        .init();

    let state = Arc::new(AppState {
        vault: BiometricVault::new(),
        cancelable: CancelableBiometrics::new(),
    });

    let app = Router::new()
        .route("/health", get(health))
        // Vault endpoints
        .route("/vault/stats", get({
            let s = state.clone();
            move || vault_stats(s)
        }))
        .route("/vault/encrypt", post({
            let s = state.clone();
            move |body| vault_encrypt(s, body)
        }))
        .route("/vault/decrypt", post({
            let s = state.clone();
            move |body| vault_decrypt(s, body)
        }))
        .route("/vault/rotate-key", post({
            let s = state.clone();
            move |body| vault_rotate_key(s, body)
        }))
        .route("/vault/audit", get({
            let s = state.clone();
            move || vault_audit(s)
        }))
        // Cancelable biometrics
        .route("/cancelable/create", post({
            let s = state.clone();
            move |body| cancelable_create(s, body)
        }))
        .route("/cancelable/apply", post({
            let s = state.clone();
            move |body| cancelable_apply(s, body)
        }))
        .route("/cancelable/revoke", post({
            let s = state.clone();
            move |body| cancelable_revoke(s, body)
        }))
        .route("/cancelable/compare", post(cancelable_compare))
        // Matching endpoints
        .route("/match/fingerprint", post(match_fingerprint))
        .route("/match/face", post(match_face))
        .route("/match/iris", post(match_iris))
        .route("/match/fuse", post(match_fuse))
        .layer(CorsLayer::permissive());

    let addr = "0.0.0.0:8091";
    tracing::info!("biometric vault service listening on {}", addr);

    let listener = tokio::net::TcpListener::bind(addr).await.unwrap();
    axum::serve(listener, app).await.unwrap();
}

async fn health() -> impl IntoResponse {
    Json(serde_json::json!({
        "status": "healthy",
        "service": "inec-biometric-vault",
        "capabilities": ["vault", "cancelable", "matching", "fusion"],
    }))
}

// ─── Vault ──────────────────────────────────────────────────────

#[derive(Deserialize)]
struct EncryptRequest {
    voter_vin: String,
    modality: String,
    template_data: String, // base64
    actor: String,
}

#[derive(Deserialize)]
struct DecryptRequest {
    template_id: String,
    actor: String,
}

#[derive(Deserialize)]
struct RotateKeyRequest {
    key_id: String,
    actor: String,
}

async fn vault_stats(state: Arc<AppState>) -> impl IntoResponse {
    Json(state.vault.get_stats())
}

async fn vault_encrypt(
    state: Arc<AppState>,
    Json(req): Json<EncryptRequest>,
) -> impl IntoResponse {
    let data = match base64::engine::general_purpose::STANDARD.decode(&req.template_data) {
        Ok(d) => d,
        Err(e) => return (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    };

    match state.vault.encrypt_template(&req.voter_vin, &req.modality, &data, &req.actor) {
        Ok(encrypted) => Json(serde_json::json!({
            "template_id": encrypted.template_id,
            "voter_vin": encrypted.voter_vin,
            "modality": encrypted.modality,
            "key_id": encrypted.key_id,
            "ciphertext_len": encrypted.ciphertext.len(),
            "created_at": encrypted.created_at.to_rfc3339(),
        })).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    }
}

async fn vault_decrypt(
    state: Arc<AppState>,
    Json(req): Json<DecryptRequest>,
) -> impl IntoResponse {
    match state.vault.decrypt_template(&req.template_id, &req.actor) {
        Ok(plaintext) => Json(serde_json::json!({
            "template_data": base64::engine::general_purpose::STANDARD.encode(&plaintext),
            "size_bytes": plaintext.len(),
        })).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    }
}

async fn vault_rotate_key(
    state: Arc<AppState>,
    Json(req): Json<RotateKeyRequest>,
) -> impl IntoResponse {
    match state.vault.rotate_key(&req.key_id, &req.actor) {
        Ok(new_id) => Json(serde_json::json!({
            "old_key_id": req.key_id,
            "new_key_id": new_id,
        })).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    }
}

async fn vault_audit(state: Arc<AppState>) -> impl IntoResponse {
    Json(serde_json::json!({
        "entries": state.vault.get_audit_log(100),
    }))
}

// ─── Cancelable ─────────────────────────────────────────────────

#[derive(Deserialize)]
struct CreateTransformRequest {
    voter_vin: String,
    modality: String,
    transform_type: String,
}

#[derive(Deserialize)]
struct ApplyTransformRequest {
    transform_id: String,
    features: Vec<f64>,
}

#[derive(Deserialize)]
struct RevokeTransformRequest {
    transform_id: String,
}

#[derive(Deserialize)]
struct CompareHashRequest {
    hash1: String, // base64
    hash2: String, // base64
}

async fn cancelable_create(
    state: Arc<AppState>,
    Json(req): Json<CreateTransformRequest>,
) -> impl IntoResponse {
    let tt = match req.transform_type.as_str() {
        "biohashing" => TransformType::BioHashing,
        "random_projection" => TransformType::RandomProjection,
        "bloom_filter" => TransformType::BloomFilter,
        _ => TransformType::BioHashing,
    };

    let id = state.cancelable.create_transform(&req.voter_vin, &req.modality, tt);
    Json(serde_json::json!({ "transform_id": id }))
}

async fn cancelable_apply(
    state: Arc<AppState>,
    Json(req): Json<ApplyTransformRequest>,
) -> impl IntoResponse {
    match state.cancelable.apply_biohash(&req.transform_id, &req.features) {
        Ok(hash) => Json(serde_json::json!({
            "biohash": base64::engine::general_purpose::STANDARD.encode(&hash),
            "bits": hash.len() * 8,
        })).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    }
}

async fn cancelable_revoke(
    state: Arc<AppState>,
    Json(req): Json<RevokeTransformRequest>,
) -> impl IntoResponse {
    match state.cancelable.revoke_transform(&req.transform_id) {
        Ok(()) => Json(serde_json::json!({"status": "revoked"})).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    }
}

async fn cancelable_compare(Json(req): Json<CompareHashRequest>) -> impl IntoResponse {
    let h1 = base64::engine::general_purpose::STANDARD.decode(&req.hash1).unwrap_or_default();
    let h2 = base64::engine::general_purpose::STANDARD.decode(&req.hash2).unwrap_or_default();
    let distance = CancelableBiometrics::compare_biohash(&h1, &h2);
    Json(serde_json::json!({
        "hamming_distance": distance,
        "similarity": 1.0 - distance,
        "decision": if distance < 0.35 { "match" } else { "no_match" },
    }))
}

// ─── Matching ───────────────────────────────────────────────────

#[derive(Deserialize)]
struct FingerprintMatchRequest {
    probe: Vec<[f64; 4]>,   // [x, y, angle, type]
    gallery: Vec<[f64; 4]>,
    threshold: Option<f64>,
}

#[derive(Deserialize)]
struct FaceMatchRequest {
    probe: Vec<f64>,
    gallery: Vec<f64>,
    threshold: Option<f64>,
}

#[derive(Deserialize)]
struct IrisMatchRequest {
    probe_code: String,    // base64
    gallery_code: String,  // base64
    probe_mask: String,    // base64
    gallery_mask: String,  // base64
    threshold: Option<f64>,
}

#[derive(Deserialize)]
struct FuseRequest {
    scores: Vec<ScoreInput>,
    method: Option<String>,
    weights: Option<std::collections::HashMap<String, f64>>,
}

#[derive(Deserialize)]
struct ScoreInput {
    modality: String,
    score: f64,
}

async fn match_fingerprint(Json(req): Json<FingerprintMatchRequest>) -> impl IntoResponse {
    let probe: Vec<(i32, i32, f64, u8)> = req.probe.iter().map(|m| (m[0] as i32, m[1] as i32, m[2], m[3] as u8)).collect();
    let gallery: Vec<(i32, i32, f64, u8)> = req.gallery.iter().map(|m| (m[0] as i32, m[1] as i32, m[2], m[3] as u8)).collect();
    let threshold = req.threshold.unwrap_or(0.40);

    let result = match_fingerprint_minutiae(&probe, &gallery, threshold);
    Json(serde_json::json!({
        "score": result.score,
        "decision": format!("{:?}", result.decision),
        "algorithm": result.algorithm,
        "latency_us": result.latency_us,
    }))
}

async fn match_face(Json(req): Json<FaceMatchRequest>) -> impl IntoResponse {
    let threshold = req.threshold.unwrap_or(0.45);
    let result = match_face_embeddings(&req.probe, &req.gallery, threshold);
    Json(serde_json::json!({
        "score": result.score,
        "decision": format!("{:?}", result.decision),
        "algorithm": result.algorithm,
        "latency_us": result.latency_us,
    }))
}

async fn match_iris(Json(req): Json<IrisMatchRequest>) -> impl IntoResponse {
    let b64 = base64::engine::general_purpose::STANDARD;
    let probe = b64.decode(&req.probe_code).unwrap_or_default();
    let gallery = b64.decode(&req.gallery_code).unwrap_or_default();
    let pmask = b64.decode(&req.probe_mask).unwrap_or_default();
    let gmask = b64.decode(&req.gallery_mask).unwrap_or_default();
    let threshold = req.threshold.unwrap_or(0.32);

    let result = match_iris_codes(&probe, &gallery, &pmask, &gmask, threshold, 7);
    Json(serde_json::json!({
        "score": result.score,
        "decision": format!("{:?}", result.decision),
        "algorithm": result.algorithm,
        "latency_us": result.latency_us,
    }))
}

async fn match_fuse(Json(req): Json<FuseRequest>) -> impl IntoResponse {
    let method = match req.method.as_deref() {
        Some("max") => FusionMethod::MaxRule,
        Some("sum") => FusionMethod::SumRule,
        Some("product") => FusionMethod::ProductRule,
        _ => FusionMethod::WeightedSum,
    };

    let weights = req.weights.unwrap_or_else(|| {
        let mut w = std::collections::HashMap::new();
        w.insert("fingerprint".into(), 0.40);
        w.insert("face".into(), 0.35);
        w.insert("iris".into(), 0.25);
        w
    });

    let scores: Vec<matching::MatchScore> = req.scores.iter().map(|s| matching::MatchScore {
        probe_id: String::new(),
        gallery_id: String::new(),
        modality: s.modality.clone(),
        score: s.score,
        normalized_score: s.score,
        decision: if s.score >= 0.45 { MatchDecision::Match } else { MatchDecision::NoMatch },
        algorithm: "external".into(),
        latency_us: 0,
    }).collect();

    let fused = fuse_scores(&scores, &weights, method);
    Json(serde_json::json!({
        "fused_score": fused.fused_score,
        "decision": format!("{:?}", fused.decision),
        "fusion_method": format!("{:?}", fused.fusion_method),
    }))
}
