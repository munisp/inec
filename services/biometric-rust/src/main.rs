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
    fuse_scores, match_face_embeddings, match_fingerprint_minutiae, match_iris_codes, FusionMethod,
    IdentifyThresholds, MatchDecision,
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
        .route(
            "/health",
            get({
                let s = state.clone();
                move || health(s)
            }),
        )
        // Vault endpoints
        .route(
            "/vault/stats",
            get({
                let s = state.clone();
                move || vault_stats(s)
            }),
        )
        .route(
            "/vault/encrypt",
            post({
                let s = state.clone();
                move |actor, body| vault_encrypt(s, actor, body)
            }),
        )
        .route(
            "/vault/decrypt",
            post({
                let s = state.clone();
                move |actor, body| vault_decrypt(s, actor, body)
            }),
        )
        .route(
            "/vault/rotate-key",
            post({
                let s = state.clone();
                move |actor, body| vault_rotate_key(s, actor, body)
            }),
        )
        .route(
            "/vault/audit",
            get({
                let s = state.clone();
                move || vault_audit(s)
            }),
        )
        // Cancelable biometrics
        .route(
            "/cancelable/create",
            post({
                let s = state.clone();
                move |body| cancelable_create(s, body)
            }),
        )
        .route(
            "/cancelable/apply",
            post({
                let s = state.clone();
                move |body| cancelable_apply(s, body)
            }),
        )
        .route(
            "/cancelable/revoke",
            post({
                let s = state.clone();
                move |body| cancelable_revoke(s, body)
            }),
        )
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
    tracing::info!(
        "biometric vault service listening on {} (PostgreSQL persistence)",
        addr
    );

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

/// Constant-time byte comparison. Length is checked first (length is not
/// secret); the XOR-accumulate loop never short-circuits on content.
fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut acc = 0u8;
    for (x, y) in a.iter().zip(b.iter()) {
        acc |= x ^ y;
    }
    acc == 0
}

/// Parse a comma-separated API-key list (e.g. "KEY1,KEY2") so key rotation
/// is possible without downtime: during a rotation window both the old and
/// the new key authenticate. Blank entries are ignored.
fn configured_api_keys(env_var: &str) -> Vec<String> {
    std::env::var(env_var)
        .unwrap_or_default()
        .split(',')
        .map(|k| k.trim().to_string())
        .filter(|k| !k.is_empty())
        .collect()
}

/// Configured vault API tokens. Primary source: BIOMETRIC_VAULT_API_KEY
/// (comma-separated rotation list). VAULT_API_TOKEN is accepted as a
/// single-token alias for operators that follow the generic vault naming.
fn vault_api_tokens() -> Vec<String> {
    let mut keys = configured_api_keys("BIOMETRIC_VAULT_API_KEY");
    keys.extend(configured_api_keys("VAULT_API_TOKEN"));
    keys
}

/// Return the configured key that matches `provided`, comparing each
/// candidate in constant time.
fn matching_api_key<'a>(provided: &str, keys: &'a [String]) -> Option<&'a str> {
    keys.iter()
        .find(|k| constant_time_eq(provided.as_bytes(), k.as_bytes()))
        .map(String::as_str)
}

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
/// public for orchestrator probes. Fail-closed: when neither
/// BIOMETRIC_VAULT_API_KEY nor VAULT_API_TOKEN is configured the service
/// returns 503 rather than serving unauthenticated biometric plaintext.
/// BIOMETRIC_VAULT_API_KEY accepts a comma-separated key list so rotation is
/// possible without downtime; the presented credential is compared against
/// each configured key in constant time. Callers may authenticate with an
/// `x-api-key: <key>` header or `Authorization: Bearer <token>`.
async fn vault_api_key_auth(req: Request, next: Next) -> Response {
    if req.uri().path() == "/health" {
        return next.run(req).await;
    }

    let expected_keys = vault_api_tokens();
    if expected_keys.is_empty() {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "BIOMETRIC_VAULT_API_KEY/VAULT_API_TOKEN not configured; refusing to serve unauthenticated requests"
            })),
        )
            .into_response();
    }

    // Accept either x-api-key or an Authorization: Bearer token.
    let presented = req
        .headers()
        .get("x-api-key")
        .and_then(|v| v.to_str().ok())
        .map(str::to_string)
        .or_else(|| {
            req.headers()
                .get(header::AUTHORIZATION)
                .and_then(|v| v.to_str().ok())
                .and_then(|v| v.strip_prefix("Bearer "))
                .map(|t| t.trim().to_string())
        });

    let provided = presented
        .as_deref()
        .and_then(|v| matching_api_key(v, &expected_keys));

    let Some(matched_key) = provided else {
        return (
            StatusCode::UNAUTHORIZED,
            Json(serde_json::json!({ "error": "missing or invalid credentials (x-api-key or Authorization: Bearer)" })),
        )
            .into_response();
    };

    // The audit actor is derived from the key that actually authenticated,
    // so rotation windows never blur caller identity.
    let actor = vault_actor_identity(matched_key);
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
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

/// Canonical biometric modality vocabulary — must match the CHECK constraint in
/// migrations/001_biometric_tables.sql (vault_templates, cancelable_transforms).
/// Validated at the HTTP boundary so bad vocabulary is a 400, not a DB 500.
const VALID_MODALITIES: [&str; 3] = ["fingerprint", "face", "iris"];

fn modality_allowed(m: &str) -> bool {
    VALID_MODALITIES.contains(&m)
}

fn invalid_modality_response(m: &str) -> axum::response::Response {
    (
        StatusCode::BAD_REQUEST,
        Json(serde_json::json!({
            "error": format!("invalid modality {:?}: must be one of {:?}", m, VALID_MODALITIES)
        })),
    )
        .into_response()
}

async fn vault_encrypt(
    state: Arc<AppState>,
    Extension(actor): Extension<VaultActor>,
    Json(req): Json<EncryptRequest>,
) -> impl IntoResponse {
    let data = match base64::engine::general_purpose::STANDARD.decode(&req.template_data) {
        Ok(d) => d,
        Err(e) => {
            return (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({"error": e.to_string()})),
            )
                .into_response()
        }
    };
    if !modality_allowed(&req.modality) {
        return invalid_modality_response(&req.modality);
    }

    match state
        .vault
        .encrypt_template(&req.voter_vin, &req.modality, &data, &actor.0)
        .await
    {
        Ok(encrypted) => Json(serde_json::json!({
            "template_id": encrypted.template_id,
            "voter_vin": encrypted.voter_vin,
            "modality": encrypted.modality,
            "key_id": encrypted.key_id,
            "ciphertext_len": encrypted.ciphertext.len(),
            "created_at": encrypted.created_at.to_rfc3339(),
        }))
        .into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

async fn vault_decrypt(
    state: Arc<AppState>,
    Extension(actor): Extension<VaultActor>,
    Json(req): Json<DecryptRequest>,
) -> impl IntoResponse {
    match state
        .vault
        .decrypt_template(&req.template_id, &actor.0)
        .await
    {
        Ok(plaintext) => Json(serde_json::json!({
            "template_data": base64::engine::general_purpose::STANDARD.encode(&plaintext),
            "size_bytes": plaintext.len(),
        }))
        .into_response(),
        Err(e) => (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": e.to_string()})),
        )
            .into_response(),
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
        }))
        .into_response(),
        Err(e) => (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

async fn vault_audit(state: Arc<AppState>) -> impl IntoResponse {
    match state.vault.get_audit_log(100).await {
        Ok(entries) => Json(serde_json::json!({ "entries": entries })).into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": e.to_string()})),
        )
            .into_response(),
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
    if !modality_allowed(&req.modality) {
        return invalid_modality_response(&req.modality);
    }
    let tt = match req.transform_type.as_str() {
        "biohashing" => TransformType::BioHashing,
        "random_projection" => TransformType::RandomProjection,
        "bloom_filter" => TransformType::BloomFilter,
        other => {
            return (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({
                    "error": format!("invalid transform_type {:?}: must be biohashing|random_projection|bloom_filter", other)
                })),
            )
                .into_response()
        }
    };

    match state
        .cancelable
        .create_transform(&req.voter_vin, &req.modality, tt)
        .await
    {
        Ok(id) => Json(serde_json::json!({ "transform_id": id })).into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

async fn cancelable_apply(
    state: Arc<AppState>,
    Json(req): Json<ApplyTransformRequest>,
) -> impl IntoResponse {
    match state
        .cancelable
        .apply_biohash(&req.transform_id, &req.features)
        .await
    {
        Ok(hash) => Json(serde_json::json!({
            "biohash": base64::engine::general_purpose::STANDARD.encode(&hash),
            "bits": hash.len() * 8,
        }))
        .into_response(),
        Err(e) => (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

async fn cancelable_revoke(
    state: Arc<AppState>,
    Json(req): Json<RevokeTransformRequest>,
) -> impl IntoResponse {
    match state.cancelable.revoke_transform(&req.transform_id).await {
        Ok(()) => Json(serde_json::json!({"status": "revoked"})).into_response(),
        Err(e) => (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

async fn cancelable_compare(Json(req): Json<CompareHashRequest>) -> impl IntoResponse {
    let h1 = base64::engine::general_purpose::STANDARD
        .decode(&req.hash1)
        .unwrap_or_default();
    let h2 = base64::engine::general_purpose::STANDARD
        .decode(&req.hash2)
        .unwrap_or_default();
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
    probe: Vec<[f64; 4]>, // [x, y, angle, type]
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
    probe_code: String,   // base64
    gallery_code: String, // base64
    probe_mask: String,   // base64
    gallery_mask: String, // base64
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
    let probe: Vec<(i32, i32, f64, u8)> = req
        .probe
        .iter()
        .map(|m| (m[0] as i32, m[1] as i32, m[2], m[3] as u8))
        .collect();
    let gallery: Vec<(i32, i32, f64, u8)> = req
        .gallery
        .iter()
        .map(|m| (m[0] as i32, m[1] as i32, m[2], m[3] as u8))
        .collect();
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

    let scores: Vec<matching::MatchScore> = req
        .scores
        .iter()
        .map(|s| matching::MatchScore {
            probe_id: String::new(),
            gallery_id: String::new(),
            modality: s.modality.clone(),
            score: s.score,
            normalized_score: s.score,
            decision: if s.score >= 0.45 {
                MatchDecision::Match
            } else {
                MatchDecision::NoMatch
            },
            algorithm: "external".into(),
            latency_us: 0,
        })
        .collect();

    let fused = fuse_scores(&scores, &weights, method);
    Json(serde_json::json!({
        "fused_score": fused.fused_score,
        "decision": format!("{:?}", fused.decision),
        "fusion_method": format!("{:?}", fused.fusion_method),
    }))
}

// ─── Tests ──────────────────────────────────────────────────────────────────

#[cfg(test)]
mod auth_tests {
    use super::*;
    use axum::body::Body;
    use tower::ServiceExt; // for `oneshot`

    /// Minimal router exercising ONLY the auth middleware with a dummy
    /// protected route and the public /health route — no database needed.
    fn test_app() -> Router {
        Router::new()
            .route("/health", get(|| async { StatusCode::OK }))
            .route("/vault/stats", get(|| async { StatusCode::OK }))
            .layer(middleware::from_fn(vault_api_key_auth))
    }

    async fn status_for(app: Router, headers: &[(&str, &str)], path: &str) -> StatusCode {
        let mut builder = Request::builder().uri(path);
        for (k, v) in headers {
            builder = builder.header(*k, *v);
        }
        let resp = app
            .oneshot(builder.body(Body::empty()).unwrap())
            .await
            .unwrap();
        resp.status()
    }

    /// Single test function because the middleware reads process-global env
    /// vars; parallel tests mutating them would race.
    #[tokio::test]
    async fn vault_auth_is_fail_closed_and_authenticates() {
        std::env::remove_var("BIOMETRIC_VAULT_API_KEY");
        std::env::remove_var("VAULT_API_TOKEN");

        // 1. No token configured -> protected routes fail closed with 503.
        assert_eq!(
            status_for(test_app(), &[], "/vault/stats").await,
            StatusCode::SERVICE_UNAVAILABLE
        );
        // /health stays public even when unconfigured.
        assert_eq!(status_for(test_app(), &[], "/health").await, StatusCode::OK);

        // 2. Token configured (VAULT_API_TOKEN alias) but none presented -> 401.
        std::env::set_var("VAULT_API_TOKEN", "s3cret-vault-token");
        assert_eq!(
            status_for(test_app(), &[], "/vault/stats").await,
            StatusCode::UNAUTHORIZED
        );

        // 3. Wrong token -> 401.
        assert_eq!(
            status_for(test_app(), &[("x-api-key", "wrong")], "/vault/stats").await,
            StatusCode::UNAUTHORIZED
        );
        assert_eq!(
            status_for(
                test_app(),
                &[("authorization", "Bearer wrong")],
                "/vault/stats"
            )
            .await,
            StatusCode::UNAUTHORIZED
        );

        // 4. Correct token -> pass, via both header forms.
        assert_eq!(
            status_for(
                test_app(),
                &[("x-api-key", "s3cret-vault-token")],
                "/vault/stats"
            )
            .await,
            StatusCode::OK
        );
        assert_eq!(
            status_for(
                test_app(),
                &[("authorization", "Bearer s3cret-vault-token")],
                "/vault/stats"
            )
            .await,
            StatusCode::OK
        );

        // 5. Primary env var (comma-separated rotation list) also authenticates.
        std::env::remove_var("VAULT_API_TOKEN");
        std::env::set_var("BIOMETRIC_VAULT_API_KEY", "old-key,new-key");
        assert_eq!(
            status_for(test_app(), &[("x-api-key", "old-key")], "/vault/stats").await,
            StatusCode::OK
        );
        assert_eq!(
            status_for(test_app(), &[("x-api-key", "new-key")], "/vault/stats").await,
            StatusCode::OK
        );

        // 6. Authenticated identity derives from the key, not caller input:
        //    different keys map to distinct audit actors, and no actor field
        //    is read from request bodies (EncryptRequest has no such field).
        let a1 = vault_actor_identity("old-key");
        let a2 = vault_actor_identity("new-key");
        assert_ne!(a1, a2);
        assert!(a1.starts_with("vault-key:"));
        assert!(!a1.contains("old-key")); // raw key never in the audit trail

        std::env::remove_var("BIOMETRIC_VAULT_API_KEY");
    }
}
