//! INEC Biometric Vault & Matching Service (Rust).
//!
//! All state persisted to PostgreSQL — zero in-memory storage.
//! - AES-256-GCM template encryption/decryption
//! - Cancelable biometrics (BioHashing)
//! - High-speed 1:N parallel matching
//! - Score fusion (weighted sum, max, product)
//! - Key management (generate, rotate, revoke)
//! - Full audit trail

mod cancelable;
pub mod db;
mod matching;
mod vault;

use axum::{
    extract::{Json, Request},
    http::{header, Method, StatusCode},
    middleware::{self, Next},
    response::{IntoResponse, Response},
    routing::{get, post},
    Extension, Router,
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

    // Connect to PostgreSQL — required, no fallback to in-memory and no
    // fallback credentials: a missing DATABASE_URL is a fatal misconfiguration.
    let database_url = std::env::var("DATABASE_URL")
        .expect("DATABASE_URL is required — refusing to start with fallback credentials");

    let pool = db::init_pool(&database_url)
        .await
        .expect("failed to connect to PostgreSQL — vault requires persistent storage");

    let vault = BiometricVault::new(pool.clone())
        .await
        .expect("failed to initialize vault — set BIOMETRIC_MASTER_KEY (hex-encoded 32 bytes) in non-dev environments");

    let cancelable = CancelableBiometrics::new(pool);

    let state = Arc::new(AppState { vault, cancelable });

    let app = Router::new()
        .route("/health", get({
            let s = state.clone();
            move || health(s)
        }))
        // Vault endpoints
        .route("/vault/stats", get({
            let s = state.clone();
            move || vault_stats(s)
        }))
        .route("/vault/encrypt", post({
            let s = state.clone();
            move |actor, body| vault_encrypt(s, actor, body)
        }))
        .route("/vault/decrypt", post({
            let s = state.clone();
            move |actor, body| vault_decrypt(s, actor, body)
        }))
        .route("/vault/rotate-key", post({
            let s = state.clone();
            move |actor, body| vault_rotate_key(s, actor, body)
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
        .layer(middleware::from_fn(vault_api_key_auth))
        .layer(cors_layer());

    let port = std::env::var("PORT").unwrap_or_else(|_| "8091".to_string());
    let addr = format!("0.0.0.0:{}", port);
    tracing::info!("biometric vault service listening on {} (PostgreSQL persistence)", addr);

    let listener = tokio::net::TcpListener::bind(&addr).await.unwrap();

    // Graceful shutdown on SIGTERM/SIGINT — Kubernetes sends SIGTERM first and
    // expects in-flight vault operations to drain before SIGKILL.
    axum::serve(listener, app)
        .with_graceful_shutdown(async {
            let ctrl_c = tokio::signal::ctrl_c();
            let mut sigterm = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
                .expect("failed to register SIGTERM handler");
            tokio::select! {
                _ = ctrl_c => tracing::info!("received SIGINT, starting graceful shutdown"),
                _ = sigterm.recv() => tracing::info!("received SIGTERM, starting graceful shutdown"),
            }
        })
        .await
        .unwrap();
}

// ─── Internal API Key Auth ───────────────────────────────────────

/// Authenticated caller identity attached as a request extension. The audit
/// actor is derived from the API key identity — never from the request body.
#[derive(Clone)]
struct VaultActor(String);

/// Derive a stable, non-secret identity for the configured API key:
/// BIOMETRIC_VAULT_KEY_LABEL when set, otherwise a SHA-256 hash prefix so the
/// raw key never appears in the audit log.
fn vault_actor_identity(key: &str) -> String {
    if let Ok(label) = std::env::var("BIOMETRIC_VAULT_KEY_LABEL") {
        if !label.trim().is_empty() {
            return format!("vault-key:{}", label.trim());
        }
    }
    use sha2::Digest;
    let hash = sha2::Sha256::digest(key.as_bytes());
    format!("vault-key:{}", hex::encode(&hash[..4]))
}

/// API-key authentication for all vault/matching endpoints. /health stays
/// public for orchestrator probes. Fail-closed: when BIOMETRIC_VAULT_API_KEY
/// is unset the service returns 503 rather than serving unauthenticated
/// biometric plaintext.
async fn vault_api_key_auth(req: Request, next: Next) -> Response {
    if req.uri().path() == "/health" {
        return next.run(req).await;
    }

    let expected_key = std::env::var("BIOMETRIC_VAULT_API_KEY").unwrap_or_default();
    if expected_key.is_empty() {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "BIOMETRIC_VAULT_API_KEY not configured; refusing to serve unauthenticated requests"
            })),
        )
            .into_response();
    }

    let provided = req
        .headers()
        .get("x-api-key")
        .and_then(|v| v.to_str().ok())
        .map(|v| v == expected_key)
        .unwrap_or(false);

    if !provided {
        return (
            StatusCode::UNAUTHORIZED,
            Json(serde_json::json!({ "error": "missing or invalid x-api-key" })),
        )
            .into_response();
    }

    let actor = vault_actor_identity(&expected_key);
    let mut req = req;
    req.extensions_mut().insert(VaultActor(actor));
    next.run(req).await
}

/// CORS policy driven by CORS_ORIGINS (comma-separated). Default deny: when
/// unset, no cross-origin requests are permitted.
fn cors_layer() -> CorsLayer {
    let origins_str = std::env::var("CORS_ORIGINS").unwrap_or_default();
    let origins: Vec<header::HeaderValue> = origins_str
        .split(',')
        .filter_map(|s| s.trim().parse().ok())
        .collect();
    if origins.is_empty() {
        CorsLayer::new()
    } else {
        CorsLayer::new()
            .allow_origin(origins)
            .allow_methods([Method::GET, Method::POST])
            .allow_headers([
                header::CONTENT_TYPE,
                header::AUTHORIZATION,
                header::HeaderName::from_static("x-api-key"),
            ])
    }
}

async fn health(state: Arc<AppState>) -> impl IntoResponse {
    // Readiness-style health: verify PostgreSQL is actually reachable instead
    // of reporting a static OK while the vault cannot serve requests.
    match state.vault.health_check().await {
        Ok(()) => (
            StatusCode::OK,
            Json(serde_json::json!({
                "status": "healthy",
                "service": "inec-biometric-vault",
                "persistence": "postgresql",
                "database": "up",
                "capabilities": ["vault", "cancelable", "matching", "fusion"],
            })),
        )
            .into_response(),
        Err(e) => (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "status": "unhealthy",
                "service": "inec-biometric-vault",
                "database": "down",
                "error": e.to_string(),
            })),
        )
            .into_response(),
    }
}

// ─── Vault ──────────────────────────────────────────────────────

// NOTE: no `actor` fields — the audit actor is derived from the authenticated
// API key identity (see VaultActor), never from caller-supplied request data.
#[derive(Deserialize)]
struct EncryptRequest {
    voter_vin: String,
    modality: String,
    template_data: String, // base64
}

#[derive(Deserialize)]
struct DecryptRequest {
    template_id: String,
}

#[derive(Deserialize)]
struct RotateKeyRequest {
    key_id: String,
}

async fn vault_stats(state: Arc<AppState>) -> impl IntoResponse {
    match state.vault.get_stats().await {
        Ok(stats) => Json(stats).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    }
}

async fn vault_encrypt(
    state: Arc<AppState>,
    Extension(actor): Extension<VaultActor>,
    Json(req): Json<EncryptRequest>,
) -> impl IntoResponse {
    let data = match base64::engine::general_purpose::STANDARD.decode(&req.template_data) {
        Ok(d) => d,
        Err(e) => return (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    };

    match state.vault.encrypt_template(&req.voter_vin, &req.modality, &data, &actor.0).await {
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
    Extension(actor): Extension<VaultActor>,
    Json(req): Json<DecryptRequest>,
) -> impl IntoResponse {
    match state.vault.decrypt_template(&req.template_id, &actor.0).await {
        Ok(plaintext) => Json(serde_json::json!({
            "template_data": base64::engine::general_purpose::STANDARD.encode(&plaintext),
            "size_bytes": plaintext.len(),
        })).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    }
}

async fn vault_rotate_key(
    state: Arc<AppState>,
    Extension(actor): Extension<VaultActor>,
    Json(req): Json<RotateKeyRequest>,
) -> impl IntoResponse {
    match state.vault.rotate_key(&req.key_id, &actor.0).await {
        Ok(new_id) => Json(serde_json::json!({
            "old_key_id": req.key_id,
            "new_key_id": new_id,
        })).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    }
}

async fn vault_audit(state: Arc<AppState>) -> impl IntoResponse {
    match state.vault.get_audit_log(100).await {
        Ok(entries) => Json(serde_json::json!({ "entries": entries })).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    }
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

    match state.cancelable.create_transform(&req.voter_vin, &req.modality, tt).await {
        Ok(id) => Json(serde_json::json!({ "transform_id": id })).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": e.to_string()}))).into_response(),
    }
}

async fn cancelable_apply(
    state: Arc<AppState>,
    Json(req): Json<ApplyTransformRequest>,
) -> impl IntoResponse {
    match state.cancelable.apply_biohash(&req.transform_id, &req.features).await {
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
    match state.cancelable.revoke_transform(&req.transform_id).await {
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
