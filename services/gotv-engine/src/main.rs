// GOTV Engine — Rust-based volunteer matching and ride-to-polls geo engine.
// Uses R-tree spatial index for O(log n) nearest-volunteer lookup.
// Handles: ride matching, canvasser route optimization, polling unit proximity.

mod middleware;
mod persistence;
pub mod platform;
pub mod voting_crypto;

use axum::{
    body::Body,
    extract::{Json, Path, Query, State},
    http::{Request, StatusCode},
    middleware as axum_mw,
    response::IntoResponse,
    routing::{get, post},
    Extension, Router,
};
use geo::HaversineDistance;
use geo::Point;
use ordered_float::OrderedFloat;
use rstar::{RTree, RTreeObject, AABB};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use tower_http::cors::CorsLayer;
use tracing::{info, warn};

// ─── Domain Types ──────────────────────────────────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Volunteer {
    pub id: String,
    pub party_id: i64,
    pub name: String,
    pub role: String, // canvasser, driver, coordinator, phone_banker, team_lead
    pub latitude: f64,
    pub longitude: f64,
    pub has_vehicle: bool,
    pub vehicle_capacity: i32,
    pub is_available: bool,
    pub assigned_rides: i32,
    // Real state code supplied at registration. INTEGRITY: coverage analysis
    // must NOT derive "state" from ID/ward-code string prefixes — only this
    // explicit field is used for per-state attribution.
    #[serde(default)]
    pub state_code: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PollingUnit {
    pub code: String,
    pub name: String,
    pub latitude: f64,
    pub longitude: f64,
    pub ward_code: String,
    pub registered_voters: i32,
    // Real state code (see Volunteer.state_code note).
    #[serde(default)]
    pub state_code: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RideRequest {
    pub id: String,
    pub party_id: i64,
    pub contact_id: String,
    pub pickup_lat: f64,
    pub pickup_lng: f64,
    pub polling_unit_code: String,
    pub status: String,
}

// ─── R-Tree Spatial Index ──────────────────────────────────────────────────

#[derive(Debug, Clone, PartialEq)]
struct VolunteerPoint {
    id: String,
    party_id: i64,
    lat: f64,
    lng: f64,
    has_vehicle: bool,
    capacity: i32,
    available: bool,
}

impl rstar::Point for VolunteerPoint {
    type Scalar = f64;
    const DIMENSIONS: usize = 2;

    fn generate(mut generator: impl FnMut(usize) -> Self::Scalar) -> Self {
        VolunteerPoint {
            id: String::new(),
            party_id: 0,
            lat: generator(0),
            lng: generator(1),
            has_vehicle: false,
            capacity: 0,
            available: false,
        }
    }

    fn nth(&self, index: usize) -> Self::Scalar {
        match index {
            0 => self.lat,
            1 => self.lng,
            _ => unreachable!(),
        }
    }

    fn nth_mut(&mut self, index: usize) -> &mut Self::Scalar {
        match index {
            0 => &mut self.lat,
            1 => &mut self.lng,
            _ => unreachable!(),
        }
    }
}

// ─── App State ─────────────────────────────────────────────────────────────

struct AppState {
    volunteers: RwLock<HashMap<i64, Vec<Volunteer>>>, // party_id -> volunteers
    rtree: RwLock<RTree<VolunteerPoint>>,             // spatial index
    polling_units: RwLock<HashMap<String, PollingUnit>>, // code -> PU
    ride_requests: RwLock<Vec<RideRequest>>,
    mw: middleware::Middleware,
    persistence: persistence::PersistenceLayer,
}

// ─── Lock-poisoning safety ──────────────────────────────────────────────────
//
// A panic while a write guard is held poisons the std::sync::RwLock. Every
// `.read().unwrap()` / `.write().unwrap()` on a request path would then panic
// too, turning one bad request into a permanent service-wide outage. Request
// handlers use these macros instead: a poisoned lock yields HTTP 500
// (`internal_state_unavailable`), which axum returns without aborting the
// worker.

fn lock_poisoned() -> axum::response::Response {
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(serde_json::json!({"error": "internal_state_unavailable", "message": "shared state lock poisoned by a prior panic"})),
    )
        .into_response()
}

/// Acquire a read guard on shared state; on poison return 500 from the handler.
macro_rules! state_read {
    ($lock:expr) => {
        match $lock.read() {
            Ok(guard) => guard,
            Err(_) => return lock_poisoned(),
        }
    };
}

/// Acquire a write guard on shared state; on poison return 500 from the handler.
macro_rules! state_write {
    ($lock:expr) => {
        match $lock.write() {
            Ok(guard) => guard,
            Err(_) => return lock_poisoned(),
        }
    };
}

impl AppState {
    fn new() -> Self {
        Self {
            volunteers: RwLock::new(HashMap::new()),
            rtree: RwLock::new(RTree::new()),
            polling_units: RwLock::new(HashMap::new()),
            ride_requests: RwLock::new(Vec::new()),
            mw: middleware::Middleware::new(),
            persistence: persistence::PersistenceLayer::new(),
        }
    }

    /// Rebuild the spatial index. Returns false when a lock is poisoned so
    /// the caller can surface HTTP 500 instead of panicking.
    fn rebuild_rtree(&self) -> bool {
        let vols = match self.volunteers.read() {
            Ok(g) => g,
            Err(_) => return false,
        };
        let points: Vec<VolunteerPoint> = vols
            .values()
            .flat_map(|v| v.iter())
            .filter(|v| v.latitude != 0.0 && v.longitude != 0.0)
            .map(|v| VolunteerPoint {
                id: v.id.clone(),
                party_id: v.party_id,
                lat: v.latitude,
                lng: v.longitude,
                has_vehicle: v.has_vehicle,
                capacity: v.vehicle_capacity,
                available: v.is_available,
            })
            .collect();
        let tree = RTree::bulk_load(points);
        drop(vols);
        match self.rtree.write() {
            Ok(mut guard) => {
                *guard = tree;
                true
            }
            Err(_) => false,
        }
    }
}

// ─── Request/Response Types ────────────────────────────────────────────────

#[derive(Deserialize)]
struct MatchRideRequest {
    party_id: i64,
    ride_id: Option<String>,
    pickup_lat: f64,
    pickup_lng: f64,
    polling_unit_code: String,
    max_distance_km: Option<f64>,
    require_vehicle: Option<bool>,
}

#[derive(Serialize)]
struct MatchResult {
    volunteer_id: String,
    volunteer_name: String,
    distance_km: f64,
    has_vehicle: bool,
    vehicle_capacity: i32,
    estimated_pickup_minutes: f64,
}

#[derive(Deserialize)]
struct BulkMatchRequest {
    party_id: i64,
    requests: Vec<SingleRideRequest>,
}

#[derive(Deserialize)]
struct SingleRideRequest {
    contact_id: String,
    pickup_lat: f64,
    pickup_lng: f64,
    polling_unit_code: String,
}

#[derive(Serialize)]
struct BulkMatchResult {
    contact_id: String,
    matched: bool,
    volunteer_id: Option<String>,
    distance_km: Option<f64>,
    reason: Option<String>,
}

#[derive(Deserialize)]
struct RouteOptRequest {
    party_id: i64,
    volunteer_id: String,
    pickup_points: Vec<LatLng>,
    destination: LatLng,
}

#[derive(Deserialize, Serialize, Clone)]
struct LatLng {
    lat: f64,
    lng: f64,
}

#[derive(Serialize)]
struct OptimizedRoute {
    volunteer_id: String,
    ordered_stops: Vec<RouteStop>,
    total_distance_km: f64,
    estimated_time_minutes: f64,
}

#[derive(Serialize)]
struct RouteStop {
    index: usize,
    lat: f64,
    lng: f64,
    cumulative_distance_km: f64,
}

#[derive(Deserialize)]
struct ProximityQuery {
    lat: f64,
    lng: f64,
    radius_km: Option<f64>,
    limit: Option<usize>,
}

#[derive(Serialize)]
struct ProximityResult {
    code: String,
    name: String,
    distance_km: f64,
    registered_voters: i32,
}

#[derive(Deserialize)]
struct RegisterVolunteersRequest {
    party_id: i64,
    volunteers: Vec<Volunteer>,
}

#[derive(Deserialize)]
struct RegisterPUsRequest {
    polling_units: Vec<PollingUnit>,
}

#[derive(Serialize)]
struct CoverageAnalysis {
    party_id: i64,
    total_volunteers: usize,
    total_drivers: usize,
    total_vehicle_capacity: i32,
    // "ok" | "partial_state_codes" | "unavailable_no_state_codes"
    coverage_status: String,
    coverage_by_state: HashMap<String, StateCoverage>,
    uncovered_pus: Vec<String>,
}

#[derive(Serialize, Default)]
struct StateCoverage {
    volunteers: i32,
    drivers: i32,
    polling_units: i32,
    capacity: i32,
}

// ─── Internal API Key Auth ─────────────────────────────────────────────────

/// Party identity derived from the presented API key in multi-tenant mode.
/// Inserted as a request extension by internal_api_key_auth.
#[derive(Clone)]
struct PartyKey(i64);

/// Multi-tenancy: GOTV_ENGINE_PARTY_KEYS is a JSON object mapping
/// {"<api-key>": <party_id>}. When set, the map is authoritative — every
/// request must present a key from the map and the derived party is enforced
/// against any party_id in the request (403 on mismatch). When unset, the
/// single GOTV_ENGINE_API_KEY mode is a SINGLE-TENANT fallback that trusts
/// party_id from the request body; it must be explicitly enabled with
/// GOTV_SINGLE_TENANT_MODE=true (see tenancy_mode).
fn party_keys() -> Option<HashMap<String, i64>> {
    std::env::var("GOTV_ENGINE_PARTY_KEYS")
        .ok()
        .and_then(|s| serde_json::from_str(&s).ok())
}

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

/// True when `provided` matches any configured key, comparing each
/// candidate in constant time.
fn api_key_matches(provided: &str, keys: &[String]) -> bool {
    keys.iter()
        .any(|k| constant_time_eq(provided.as_bytes(), k.as_bytes()))
}

/// Find the party bound to `key` in the party-keys map, comparing each
/// registered key in constant time.
fn party_for_key(keys: &HashMap<String, i64>, key: &str) -> Option<i64> {
    keys.iter()
        .find(|(k, _)| constant_time_eq(k.as_bytes(), key.as_bytes()))
        .map(|(_, p)| *p)
}

/// Tenancy modes. Multi-tenant (GOTV_ENGINE_PARTY_KEYS) is the only mode
/// that binds callers to parties. The single-tenant fallback trusts the
/// request body's party_id and therefore must be an explicit, deliberate
/// choice: GOTV_SINGLE_TENANT_MODE=true.
fn single_tenant_mode_enabled() -> bool {
    std::env::var("GOTV_SINGLE_TENANT_MODE")
        .map(|v| v.trim().eq_ignore_ascii_case("true"))
        .unwrap_or(false)
}

/// Production detection for fail-closed startup checks. Conservatively,
/// any ENVIRONMENT/APP_ENV other than an explicit non-prod value counts as
/// production — including unset.
fn is_production() -> bool {
    let env = std::env::var("ENVIRONMENT")
        .or_else(|_| std::env::var("APP_ENV"))
        .unwrap_or_default()
        .to_ascii_lowercase();
    !matches!(
        env.as_str(),
        "development" | "dev" | "test" | "testing" | "local" | "staging"
    )
}

/// Startup tenancy guard: refuse to boot in production with neither
/// GOTV_ENGINE_PARTY_KEYS (multi-tenant) nor GOTV_SINGLE_TENANT_MODE=true
/// (explicit single-tenant fallback). Without this guard an operator who
/// simply forgot GOTV_ENGINE_PARTY_KEYS would silently run a deployment
/// that trusts any caller-supplied party_id.
fn enforce_tenancy_config_at_startup() {
    if party_keys().is_some() {
        info!("tenancy: multi-tenant mode (GOTV_ENGINE_PARTY_KEYS)");
        return;
    }
    if single_tenant_mode_enabled() {
        warn!("tenancy: GOTV_SINGLE_TENANT_MODE=true — party_id from request bodies is trusted; do not use for multi-party deployments");
        return;
    }
    if is_production() {
        eprintln!(
            "FATAL: no tenancy configuration. Set GOTV_ENGINE_PARTY_KEYS (multi-tenant) \
             or explicitly opt into the single-tenant fallback with \
             GOTV_SINGLE_TENANT_MODE=true. Refusing to start in production \
             with an implicit trust-body-party_id fallback."
        );
        std::process::exit(1);
    }
    // Non-prod: boot so developers can iterate, but the auth middleware
    // still fails closed (503) until one of the two modes is configured.
    warn!("tenancy: neither GOTV_ENGINE_PARTY_KEYS nor GOTV_SINGLE_TENANT_MODE=true is set — non-prod boot allowed, but authenticated requests will be rejected (503) until tenancy is configured");
}

/// Reject a request whose party_id disagrees with the key-derived party.
fn enforce_party(
    party: &Option<PartyKey>,
    requested_party: i64,
) -> Result<(), axum::response::Response> {
    if let Some(PartyKey(p)) = party {
        if *p != requested_party {
            return Err((
                StatusCode::FORBIDDEN,
                Json(serde_json::json!({
                    "error": "party_mismatch",
                    "message": "API key is not authorized for the requested party_id"
                })),
            )
                .into_response());
        }
    }
    Ok(())
}

async fn internal_api_key_auth(req: Request<Body>, next: axum_mw::Next) -> impl IntoResponse {
    // Health endpoint is always public
    if req.uri().path() == "/health" {
        return next.run(req).await;
    }
    // SECURITY: fail closed. Previously, when GOTV_ENGINE_API_KEY was unset
    // ALL requests were allowed (auth silently disabled), and ANY request
    // bearing ANY dapr-api-token header value passed unauthenticated.
    // GOTV_ENGINE_API_KEY accepts a comma-separated key list so rotation is
    // possible without downtime; the presented key is compared against each
    // configured key in constant time.
    let expected_keys = configured_api_keys("GOTV_ENGINE_API_KEY");
    if expected_keys.is_empty() {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "GOTV_ENGINE_API_KEY not configured; refusing to serve unauthenticated requests"
            })),
        )
            .into_response();
    }

    let presented_key = req
        .headers()
        .get("x-api-key")
        .and_then(|v| v.to_str().ok())
        .map(|v| v.to_string());

    let has_key = presented_key
        .as_deref()
        .map(|v| api_key_matches(v, &expected_keys))
        .unwrap_or(false);

    // The dapr-api-token header must match the configured Dapr API token
    // (DAPR_API_TOKEN, falling back to the service key list; both accept
    // comma-separated values for rotation) — mere presence of the header is
    // NOT authentication.
    let dapr_tokens = {
        let tokens = configured_api_keys("DAPR_API_TOKEN");
        if tokens.is_empty() {
            expected_keys.clone()
        } else {
            tokens
        }
    };
    let presented_dapr = req
        .headers()
        .get("dapr-api-token")
        .and_then(|v| v.to_str().ok())
        .map(|v| v.to_string());
    let has_dapr = presented_dapr
        .as_deref()
        .map(|v| api_key_matches(v, &dapr_tokens))
        .unwrap_or(false);

    if has_dapr || has_key {
        let mut req = req;
        if let Some(keys) = party_keys() {
            // Multi-tenant mode: the presented credential must be bound to a
            // party; the party travels as an extension and handlers reject
            // mismatched party_ids with 403. The x-api-key is consulted
            // first, then the Dapr token — registering the Dapr token value
            // in GOTV_ENGINE_PARTY_KEYS is how a service account is bound to
            // a party.
            let binding = presented_key
                .as_deref()
                .and_then(|k| party_for_key(&keys, k))
                .or_else(|| {
                    presented_dapr
                        .as_deref()
                        .and_then(|t| party_for_key(&keys, t))
                });
            match binding {
                Some(party_id) => {
                    req.extensions_mut().insert(PartyKey(party_id));
                }
                None => {
                    // Fail closed. A dapr-token-authenticated call with no
                    // party binding used to sail through with NO per-party
                    // restriction (cross-tenant service account); it is now
                    // rejected with 403. An x-api-key unknown to the party
                    // map remains a 401.
                    if has_dapr && !has_key {
                        return (
                            StatusCode::FORBIDDEN,
                            Json(serde_json::json!({
                                "error": "party_binding_required",
                                "message": "dapr-token-authenticated calls carry no party binding; register the Dapr token in GOTV_ENGINE_PARTY_KEYS or call with a party-scoped x-api-key"
                            })),
                        )
                            .into_response();
                    }
                    return (
                        StatusCode::UNAUTHORIZED,
                        Json(
                            serde_json::json!({"error": "x-api-key not authorized for any party"}),
                        ),
                    )
                        .into_response();
                }
            }
        } else if !single_tenant_mode_enabled() {
            // Single-tenant fallback (party_id trusted from the request body)
            // only when explicitly enabled; otherwise fail closed.
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                Json(serde_json::json!({
                    "error": "tenancy_not_configured",
                    "message": "set GOTV_ENGINE_PARTY_KEYS (multi-tenant) or GOTV_SINGLE_TENANT_MODE=true (explicit single-tenant fallback)"
                })),
            )
                .into_response();
        }
        return next.run(req).await;
    }

    (
        StatusCode::UNAUTHORIZED,
        Json(serde_json::json!({"error": "unauthorized"})),
    )
        .into_response()
}

// ─── Rate Limiting ─────────────────────────────────────────────────────────

/// Sliding-window rate limiter (~120 requests/minute per API key).
/// Runs AFTER authentication so the limiter key is the caller identity.
async fn rate_limit(
    axum::extract::State(limiter): axum::extract::State<Arc<platform::SlidingWindowLimiter>>,
    req: Request<Body>,
    next: axum_mw::Next,
) -> axum::response::Response {
    let key = req
        .headers()
        .get("x-api-key")
        .and_then(|v| v.to_str().ok())
        .map(|s| s.to_string())
        .unwrap_or_else(|| "anonymous".to_string());

    if !limiter.allow(&key) {
        warn!(key_prefix = &key[..key.len().min(8)], "rate limit exceeded");
        return (
            StatusCode::TOO_MANY_REQUESTS,
            Json(
                serde_json::json!({"error": "rate_limit_exceeded", "limit": "120 requests/minute"}),
            ),
        )
            .into_response();
    }
    next.run(req).await
}

/// Maximum items accepted in bulk registration/match payloads.
const MAX_BULK_ITEMS: usize = 10_000;

/// CORS policy driven by CORS_ORIGINS (comma-separated). Default deny: when
/// unset, no cross-origin requests are permitted (replaces permissive CORS).
fn cors_layer() -> CorsLayer {
    use axum::http::{header, Method};
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

// ─── Handlers ──────────────────────────────────────────────────────────────

async fn health(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    // Report persistence honestly: a persistence layer that is enabled but
    // accumulating write failures is DEGRADED — responses claimed "persistent"
    // while writes were being discarded.
    let enabled = state.persistence.is_enabled();
    let failures = state.persistence.write_failures();
    let persistence = if !enabled {
        "disabled"
    } else if failures > 0 {
        "degraded"
    } else {
        "ok"
    };
    // A degraded persistence layer means writes are being discarded while
    // reads keep serving stale in-memory state: report 503 so orchestrator
    // probes (readiness/liveness) take the pod out of rotation instead of
    // trusting a healthy-looking 200 payload.
    let status = if persistence == "degraded" {
        StatusCode::SERVICE_UNAVAILABLE
    } else {
        StatusCode::OK
    };
    (
        status,
        Json(serde_json::json!({
            "service": "gotv-engine",
            "status": if persistence == "degraded" { "degraded" } else { "healthy" },
            "version": "1.0.0",
            "language": "rust",
            "persistence": persistence,
            "persistence_write_failures": failures,
            "capabilities": ["ride_matching", "route_optimization", "proximity_search", "coverage_analysis"]
        })),
    )
}

async fn register_volunteers(
    State(state): State<Arc<AppState>>,
    party: Option<Extension<PartyKey>>,
    Json(req): Json<RegisterVolunteersRequest>,
) -> axum::response::Response {
    if let Err(resp) = enforce_party(&party.map(|Extension(p)| p), req.party_id) {
        return resp;
    }
    if req.volunteers.len() > MAX_BULK_ITEMS {
        return (
            StatusCode::PAYLOAD_TOO_LARGE,
            Json(serde_json::json!({"error": "too_many_volunteers", "max": MAX_BULK_ITEMS})),
        )
            .into_response();
    }
    let count = req.volunteers.len();
    // Collect positions to persist (id, lat, lng) before consuming req.
    let positions: Vec<(String, f64, f64)> = req
        .volunteers
        .iter()
        .filter(|v| v.latitude != 0.0 && v.longitude != 0.0)
        .map(|v| (v.id.clone(), v.latitude, v.longitude))
        .collect();
    {
        let mut vols = state_write!(state.volunteers);
        let entry = vols.entry(req.party_id).or_insert_with(Vec::new);
        for v in req.volunteers {
            // Upsert by volunteer_id
            if let Some(existing) = entry.iter_mut().find(|e| e.id == v.id) {
                *existing = v;
            } else {
                entry.push(v);
            }
        }
    }
    if !state.rebuild_rtree() {
        return lock_poisoned();
    }

    // Persist positions asynchronously (registration/upsert = position update).
    let persistence = state.persistence.clone();
    let party_id = req.party_id;
    tokio::spawn(async move {
        for (vol_id, lat, lng) in positions {
            persistence
                .save_volunteer_position(&vol_id, party_id, lat, lng)
                .await;
            persistence
                .cache_volunteer_position(&vol_id, lat, lng)
                .await;
        }
    });
    info!(
        party_id = req.party_id,
        count = count,
        "Registered volunteers"
    );

    (
        StatusCode::OK,
        Json(serde_json::json!({
            "registered": count,
            "party_id": req.party_id,
            // INTEGRITY: report honestly whether state survives a restart.
            "persistence": if state.persistence.is_enabled() { "persistent" } else { "ephemeral" },
        })),
    )
        .into_response()
}

async fn register_polling_units(
    State(state): State<Arc<AppState>>,
    Json(req): Json<RegisterPUsRequest>,
) -> axum::response::Response {
    if req.polling_units.len() > MAX_BULK_ITEMS {
        return (
            StatusCode::PAYLOAD_TOO_LARGE,
            Json(serde_json::json!({"error": "too_many_polling_units", "max": MAX_BULK_ITEMS})),
        )
            .into_response();
    }
    let count = req.polling_units.len();
    let mut pus = state_write!(state.polling_units);
    for pu in req.polling_units {
        pus.insert(pu.code.clone(), pu);
    }
    info!(count = count, "Registered polling units");

    (
        StatusCode::OK,
        Json(serde_json::json!({"registered": count})),
    )
        .into_response()
}

async fn match_ride(
    State(state): State<Arc<AppState>>,
    party: Option<Extension<PartyKey>>,
    Json(req): Json<MatchRideRequest>,
) -> axum::response::Response {
    if let Err(resp) = enforce_party(&party.map(|Extension(p)| p), req.party_id) {
        return resp;
    }
    let max_dist = req.max_distance_km.unwrap_or(10.0);
    let require_vehicle = req.require_vehicle.unwrap_or(true);

    // Scope RwLock guards so they're dropped before any .await
    let (results, no_match) = {
        let tree = state_read!(state.rtree);
        let pickup = VolunteerPoint {
            id: String::new(),
            party_id: req.party_id,
            lat: req.pickup_lat,
            lng: req.pickup_lng,
            has_vehicle: false,
            capacity: 0,
            available: false,
        };

        let candidates: Vec<_> = tree
            .nearest_neighbor_iter(&pickup)
            .filter(|vp| vp.party_id == req.party_id && vp.available)
            .filter(|vp| !require_vehicle || vp.has_vehicle)
            .take(10)
            .collect();

        if candidates.is_empty() {
            (Vec::new(), true)
        } else {
            let pickup_point = Point::new(req.pickup_lng, req.pickup_lat);
            let mut results: Vec<MatchResult> = candidates
                .iter()
                .map(|vp| {
                    let vol_point = Point::new(vp.lng, vp.lat);
                    let dist = pickup_point.haversine_distance(&vol_point) / 1000.0;
                    MatchResult {
                        volunteer_id: vp.id.clone(),
                        volunteer_name: String::new(),
                        distance_km: (dist * 100.0).round() / 100.0,
                        has_vehicle: vp.has_vehicle,
                        vehicle_capacity: vp.capacity,
                        estimated_pickup_minutes: (dist / 30.0 * 60.0).round(),
                    }
                })
                .filter(|m| m.distance_km <= max_dist)
                .collect();
            results.sort_by_key(|r| OrderedFloat(r.distance_km));

            // Populate volunteer names
            let vols = state_read!(state.volunteers);
            if let Some(party_vols) = vols.get(&req.party_id) {
                for result in &mut results {
                    if let Some(vol) = party_vols.iter().find(|v| v.id == result.volunteer_id) {
                        result.volunteer_name = vol.name.clone();
                    }
                }
            }
            (results, false)
        }
    }; // All RwLock guards dropped here

    if no_match {
        return (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({"error": "no_available_volunteers", "message": "No volunteers found within range"})),
        ).into_response();
    }

    // Middleware: publish match event to Kafka + Fluvio (no locks held)
    let best_match = results.first().map(|m| m.volunteer_id.clone());
    let event = serde_json::json!({
        "event": "ride_matched",
        "ride_id": req.ride_id,
        "party_id": req.party_id,
        "pickup_lat": req.pickup_lat,
        "pickup_lng": req.pickup_lng,
        "candidates": results.len(),
        "matched_volunteer": best_match,
    });
    state
        .mw
        .publish_kafka(
            "gotv.rides",
            req.ride_id.as_deref().unwrap_or("unknown"),
            &event,
        )
        .await;
    state.mw.stream_fluvio("gotv-ride-matches", &event).await;

    // Persist the match (previously nothing was saved — match results were
    // ephemeral despite reporting success).
    if let (Some(ride_id), Some(best)) = (req.ride_id.clone(), results.first()) {
        state
            .persistence
            .save_ride_match(&ride_id, &best.volunteer_id, best.distance_km)
            .await;
    }

    (
        StatusCode::OK,
        Json(serde_json::json!({
            "matches": results,
            "total_candidates": results.len(),
            "polling_unit": req.polling_unit_code,
            // INTEGRITY: report honestly whether the match was persisted.
            "persistence": if state.persistence.is_enabled() { "persistent" } else { "ephemeral" },
        })),
    )
        .into_response()
}

async fn bulk_match_rides(
    State(state): State<Arc<AppState>>,
    party: Option<Extension<PartyKey>>,
    Json(req): Json<BulkMatchRequest>,
) -> axum::response::Response {
    if let Err(resp) = enforce_party(&party.map(|Extension(p)| p), req.party_id) {
        return resp;
    }
    if req.requests.len() > MAX_BULK_ITEMS {
        return (
            StatusCode::PAYLOAD_TOO_LARGE,
            Json(serde_json::json!({"error": "too_many_requests", "max": MAX_BULK_ITEMS})),
        )
            .into_response();
    }
    let tree = state_read!(state.rtree);
    let mut assigned: HashMap<String, bool> = HashMap::new();
    let mut results = Vec::new();

    for ride_req in &req.requests {
        let pickup_point = Point::new(ride_req.pickup_lng, ride_req.pickup_lat);
        let pickup_vp = VolunteerPoint {
            id: String::new(),
            party_id: req.party_id,
            lat: ride_req.pickup_lat,
            lng: ride_req.pickup_lng,
            has_vehicle: false,
            capacity: 0,
            available: false,
        };

        let matched = tree
            .nearest_neighbor_iter(&pickup_vp)
            .filter(|vp| {
                vp.party_id == req.party_id
                    && vp.available
                    && vp.has_vehicle
                    && !assigned.contains_key(&vp.id)
            })
            .find_map(|vp| {
                let vol_point = Point::new(vp.lng, vp.lat);
                let dist = pickup_point.haversine_distance(&vol_point) / 1000.0;
                if dist <= 15.0 {
                    Some((vp.id.clone(), dist))
                } else {
                    None
                }
            });

        match matched {
            Some((vol_id, dist)) => {
                assigned.insert(vol_id.clone(), true);
                results.push(BulkMatchResult {
                    contact_id: ride_req.contact_id.clone(),
                    matched: true,
                    volunteer_id: Some(vol_id),
                    distance_km: Some((dist * 100.0).round() / 100.0),
                    reason: None,
                });
            }
            None => {
                results.push(BulkMatchResult {
                    contact_id: ride_req.contact_id.clone(),
                    matched: false,
                    volunteer_id: None,
                    distance_km: None,
                    reason: Some("No available driver within 15km".into()),
                });
            }
        }
    }

    let matched_count = results.iter().filter(|r| r.matched).count();

    Json(serde_json::json!({
        "results": results,
        "total_requests": req.requests.len(),
        "matched": matched_count,
        "unmatched": req.requests.len() - matched_count,
        // INTEGRITY: bulk matches are computed in memory; report persistence honestly.
        "persistence": if state.persistence.is_enabled() { "persistent" } else { "ephemeral" },
    }))
    .into_response()
}

async fn optimize_route(
    State(_state): State<Arc<AppState>>,
    party: Option<Extension<PartyKey>>,
    Json(req): Json<RouteOptRequest>,
) -> axum::response::Response {
    if let Err(resp) = enforce_party(&party.map(|Extension(p)| p), req.party_id) {
        return resp;
    }
    // Nearest-neighbor TSP heuristic for canvasser route optimization
    let mut remaining: Vec<(usize, LatLng)> = req
        .pickup_points
        .iter()
        .enumerate()
        .map(|(i, p)| (i, p.clone()))
        .collect();

    let mut ordered_stops = Vec::new();
    let mut current = req.destination.clone(); // Start from destination (polling unit)
    let mut total_distance = 0.0;

    while !remaining.is_empty() {
        let (best_idx, best_dist) = remaining
            .iter()
            .enumerate()
            .map(|(ri, (_, p))| {
                let d = haversine_km(current.lat, current.lng, p.lat, p.lng);
                (ri, d)
            })
            .min_by_key(|&(_, d)| OrderedFloat(d))
            .unwrap();

        let (original_idx, point) = remaining.remove(best_idx);
        total_distance += best_dist;

        ordered_stops.push(RouteStop {
            index: original_idx,
            lat: point.lat,
            lng: point.lng,
            cumulative_distance_km: (total_distance * 100.0).round() / 100.0,
        });

        current = point;
    }

    // Add return to destination
    if let Some(last) = ordered_stops.last() {
        total_distance +=
            haversine_km(last.lat, last.lng, req.destination.lat, req.destination.lng);
    }

    let route = OptimizedRoute {
        volunteer_id: req.volunteer_id,
        ordered_stops,
        total_distance_km: (total_distance * 100.0).round() / 100.0,
        estimated_time_minutes: (total_distance / 25.0 * 60.0).round(), // ~25 km/h with stops
    };

    Json(serde_json::json!(route)).into_response()
}

async fn proximity_polling_units(
    State(state): State<Arc<AppState>>,
    Query(query): Query<ProximityQuery>,
) -> axum::response::Response {
    let radius = query.radius_km.unwrap_or(5.0);
    let limit = query.limit.unwrap_or(10);
    let pus = state_read!(state.polling_units);

    let query_point = Point::new(query.lng, query.lat);
    let mut results: Vec<ProximityResult> = pus
        .values()
        .filter_map(|pu| {
            let pu_point = Point::new(pu.longitude, pu.latitude);
            let dist = query_point.haversine_distance(&pu_point) / 1000.0;
            if dist <= radius {
                Some(ProximityResult {
                    code: pu.code.clone(),
                    name: pu.name.clone(),
                    distance_km: (dist * 100.0).round() / 100.0,
                    registered_voters: pu.registered_voters,
                })
            } else {
                None
            }
        })
        .collect();

    results.sort_by_key(|r| OrderedFloat(r.distance_km));
    results.truncate(limit);

    Json(serde_json::json!({
        "polling_units": results,
        "total": results.len(),
        "radius_km": radius
    }))
    .into_response()
}

async fn coverage_analysis(
    State(state): State<Arc<AppState>>,
    party: Option<Extension<PartyKey>>,
    Path(party_id): Path<i64>,
) -> axum::response::Response {
    if let Err(resp) = enforce_party(&party.map(|Extension(p)| p), party_id) {
        return resp;
    }
    // PERFORMANCE/DoS: cap the number of polling units processed per call.
    // Coverage check uses the R-tree spatial index: O(PU log V) instead of
    // the previous O(PU x V) brute-force scan.
    const MAX_COVERAGE_PUS: usize = 20_000;

    let vols = state_read!(state.volunteers);
    let pus = state_read!(state.polling_units);
    let tree = state_read!(state.rtree);

    let party_vols = vols.get(&party_id).cloned().unwrap_or_default();
    let total_volunteers = party_vols.len();
    let total_drivers = party_vols.iter().filter(|v| v.has_vehicle).count();
    let total_capacity: i32 = party_vols.iter().map(|v| v.vehicle_capacity).sum();

    // INTEGRITY: per-state attribution uses ONLY explicit state_code fields on
    // registered records. Previously the "state" was the first 5 chars of the
    // PU ward_code / volunteer ID — garbage attribution presented as per-state
    // coverage. When no records carry state codes, we say so honestly.
    let pu_codes_present = pus.values().filter(|p| p.state_code.is_some()).count();
    let vol_codes_present = party_vols.iter().filter(|v| v.state_code.is_some()).count();
    let coverage_status = if pu_codes_present == 0 && vol_codes_present == 0 {
        "unavailable_no_state_codes"
    } else if pu_codes_present < pus.len() || vol_codes_present < party_vols.len() {
        "partial_state_codes"
    } else {
        "ok"
    };

    let mut coverage_by_state: HashMap<String, StateCoverage> = HashMap::new();
    let mut covered_pus: HashMap<String, bool> = HashMap::new();

    if coverage_status != "unavailable_no_state_codes" {
        for pu in pus.values().take(MAX_COVERAGE_PUS) {
            let state_key = pu
                .state_code
                .clone()
                .unwrap_or_else(|| "unknown".to_string());
            let entry = coverage_by_state.entry(state_key).or_default();
            entry.polling_units += 1;

            // R-tree nearest-volunteer lookup (O(log V)), then exact haversine.
            let pickup = VolunteerPoint {
                id: String::new(),
                party_id,
                lat: pu.latitude,
                lng: pu.longitude,
                has_vehicle: false,
                capacity: 0,
                available: false,
            };
            let nearest = tree
                .nearest_neighbor_iter(&pickup)
                .find(|vp| vp.party_id == party_id && vp.available);
            if let Some(vp) = nearest {
                let pu_point = Point::new(pu.longitude, pu.latitude);
                let vol_point = Point::new(vp.lng, vp.lat);
                let dist = pu_point.haversine_distance(&vol_point) / 1000.0;
                if dist <= 10.0 {
                    covered_pus.insert(pu.code.clone(), true);
                }
            }
        }

        // Count volunteers per real state code
        for vol in &party_vols {
            let state_key = vol
                .state_code
                .clone()
                .unwrap_or_else(|| "unknown".to_string());
            let entry = coverage_by_state.entry(state_key).or_default();
            entry.volunteers += 1;
            if vol.has_vehicle {
                entry.drivers += 1;
                entry.capacity += vol.vehicle_capacity;
            }
        }
    } else {
        // No state codes anywhere: still compute covered/uncovered PUs
        // (geo-only, no state attribution).
        for pu in pus.values().take(MAX_COVERAGE_PUS) {
            let pickup = VolunteerPoint {
                id: String::new(),
                party_id,
                lat: pu.latitude,
                lng: pu.longitude,
                has_vehicle: false,
                capacity: 0,
                available: false,
            };
            let nearest = tree
                .nearest_neighbor_iter(&pickup)
                .find(|vp| vp.party_id == party_id && vp.available);
            if let Some(vp) = nearest {
                let pu_point = Point::new(pu.longitude, pu.latitude);
                let vol_point = Point::new(vp.lng, vp.lat);
                let dist = pu_point.haversine_distance(&vol_point) / 1000.0;
                if dist <= 10.0 {
                    covered_pus.insert(pu.code.clone(), true);
                }
            }
        }
    }

    let uncovered: Vec<String> = pus
        .keys()
        .take(MAX_COVERAGE_PUS)
        .filter(|code| !covered_pus.contains_key(*code))
        .take(100)
        .cloned()
        .collect();

    Json(CoverageAnalysis {
        party_id,
        total_volunteers,
        total_drivers,
        total_vehicle_capacity: total_capacity,
        coverage_status: coverage_status.to_string(),
        coverage_by_state,
        uncovered_pus: uncovered,
    })
    .into_response()
}

// ─── Helpers ───────────────────────────────────────────────────────────────

fn haversine_km(lat1: f64, lon1: f64, lat2: f64, lon2: f64) -> f64 {
    let r = 6371.0; // Earth radius km
    let d_lat = (lat2 - lat1).to_radians();
    let d_lon = (lon2 - lon1).to_radians();
    let a = (d_lat / 2.0).sin().powi(2)
        + lat1.to_radians().cos() * lat2.to_radians().cos() * (d_lon / 2.0).sin().powi(2);
    let c = 2.0 * a.sqrt().atan2((1.0 - a).sqrt());
    r * c
}

// ─── Middleware Status ─────────────────────────────────────────────────────

async fn middleware_status(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    Json(serde_json::json!({
        "service": "gotv-engine",
        "language": "rust",
        "middleware": state.mw.status(),
    }))
}

// ─── V2: Territory Partitioning ────────────────────────────────────────────

#[derive(Deserialize)]
struct TerritoryPartitionRequest {
    party_id: i64,
    ward_code: String,
    contact_locations: Vec<LatLng>,
}

async fn partition_territories(
    State(state): State<Arc<AppState>>,
    party: Option<Extension<PartyKey>>,
    Json(req): Json<TerritoryPartitionRequest>,
) -> axum::response::Response {
    if let Err(resp) = enforce_party(&party.map(|Extension(p)| p), req.party_id) {
        return resp;
    }
    let vols = state_read!(state.volunteers);
    let party_vols = vols.get(&req.party_id);

    let vol_positions: Vec<(String, f64, f64)> = party_vols
        .map(|vs| {
            vs.iter()
                .filter(|v| v.is_available && v.latitude != 0.0)
                .map(|v| (v.id.clone(), v.latitude, v.longitude))
                .collect()
        })
        .unwrap_or_default();

    let contact_locs: Vec<(f64, f64)> = req
        .contact_locations
        .iter()
        .map(|c| (c.lat, c.lng))
        .collect();

    let territories =
        persistence::partition_ward_territories(&vol_positions, &contact_locs, &req.ward_code);

    (
        StatusCode::OK,
        Json(serde_json::json!({
            "ward_code": req.ward_code,
            "territories": territories,
            "total_volunteers": vol_positions.len(),
            "total_contacts": contact_locs.len(),
        })),
    )
        .into_response()
}

// ─── V2: Predictive Turnout ────────────────────────────────────────────────

#[derive(Deserialize)]
struct TurnoutPredictionRequest {
    ward_code: String,
    historical_turnout_pct: f64,
    pledge_count: i32,
    registered_voters: i32,
    active_volunteers: i32,
    weather_clear: Option<bool>,
}

async fn predict_turnout(Json(req): Json<TurnoutPredictionRequest>) -> impl IntoResponse {
    let prediction = persistence::predict_ward_turnout(
        req.historical_turnout_pct,
        req.pledge_count,
        req.registered_voters,
        req.active_volunteers,
        req.weather_clear.unwrap_or(true),
        &req.ward_code,
    );

    Json(prediction)
}

// ─── V2: Isochrone Calculation ─────────────────────────────────────────────

#[derive(Deserialize)]
struct IsochroneRequest {
    party_id: i64,
    volunteer_id: String,
    is_urban: Option<bool>,
}

async fn calculate_isochrone(
    State(state): State<Arc<AppState>>,
    party: Option<Extension<PartyKey>>,
    Json(req): Json<IsochroneRequest>,
) -> axum::response::Response {
    if let Err(resp) = enforce_party(&party.map(|Extension(p)| p), req.party_id) {
        return resp;
    }
    let vols = state_read!(state.volunteers);
    let party_vols = vols.get(&req.party_id);

    let vol = party_vols.and_then(|vs| vs.iter().find(|v| v.id == req.volunteer_id));

    match vol {
        Some(v) => {
            let pus = state_read!(state.polling_units);
            let pu_list: Vec<(String, f64, f64)> = pus
                .values()
                .map(|pu| (pu.code.clone(), pu.latitude, pu.longitude))
                .collect();

            let iso = persistence::calculate_isochrone(
                v.latitude,
                v.longitude,
                &v.id,
                &pu_list,
                req.is_urban.unwrap_or(true),
            );
            (StatusCode::OK, Json(serde_json::json!(iso)))
        }
        None => (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({"error": "volunteer not found"})),
        ),
    }
    .into_response()
}

// ─── V2: Geofence Check ───────────────────────────────────────────────────

#[derive(Deserialize)]
struct GeofenceCheckRequest {
    volunteer_id: String,
    latitude: f64,
    longitude: f64,
    assigned_ward: String,
}

#[derive(Serialize)]
struct GeofenceResult {
    in_zone: bool,
    distance_to_center_km: f64,
    nearest_pu: String,
    alert: Option<String>,
}

/// GET /gotv-engine/stats — operational counters, including persistence
/// health (R4-28): fire-and-forget writes are fine, but their failures must
/// be observable.
async fn engine_stats(State(state): State<Arc<AppState>>) -> axum::response::Response {
    let vols = state_read!(state.volunteers);
    let pus = state_read!(state.polling_units);
    let rides = state_read!(state.ride_requests);
    let volunteer_count: usize = vols.values().map(|v| v.len()).sum();
    Json(serde_json::json!({
        "volunteers": volunteer_count,
        "parties_with_volunteers": vols.len(),
        "polling_units": pus.len(),
        "ride_requests_tracked": rides.len(),
        "persistence_enabled": state.persistence.is_enabled(),
        "persistence_write_failures": state.persistence.write_failures(),
    }))
    .into_response()
}

async fn check_geofence(
    State(state): State<Arc<AppState>>,
    Json(req): Json<GeofenceCheckRequest>,
) -> axum::response::Response {
    let pus = state_read!(state.polling_units);
    let ward_pus: Vec<&PollingUnit> = pus
        .values()
        .filter(|pu| pu.ward_code == req.assigned_ward)
        .collect();

    if ward_pus.is_empty() {
        // SECURITY: fail closed. A ward with no registered polling units is
        // UNKNOWN territory — previously this returned in_zone: true, letting
        // any volunteer claim presence in any unregistered ward.
        return Json(GeofenceResult {
            in_zone: false,
            distance_to_center_km: 0.0,
            nearest_pu: String::new(),
            alert: Some(format!(
                "unknown_ward: no polling units registered for ward {}; cannot verify geofence",
                req.assigned_ward
            )),
        })
        .into_response();
    }

    // Calculate centroid of ward PUs
    let center_lat = ward_pus.iter().map(|pu| pu.latitude).sum::<f64>() / ward_pus.len() as f64;
    let center_lng = ward_pus.iter().map(|pu| pu.longitude).sum::<f64>() / ward_pus.len() as f64;

    let dist_to_center = haversine_km(req.latitude, req.longitude, center_lat, center_lng);

    // Find nearest PU
    let mut nearest_pu = String::new();
    let mut min_dist = f64::MAX;
    for pu in &ward_pus {
        let d = haversine_km(req.latitude, req.longitude, pu.latitude, pu.longitude);
        if d < min_dist {
            min_dist = d;
            nearest_pu = pu.code.clone();
        }
    }

    let in_zone = dist_to_center < 5.0; // 5km ward radius threshold
    let alert = if !in_zone {
        Some(format!(
            "Volunteer {} is {:.1}km outside assigned ward {}",
            req.volunteer_id, dist_to_center, req.assigned_ward
        ))
    } else {
        None
    };

    Json(GeofenceResult {
        in_zone,
        distance_to_center_km: dist_to_center,
        nearest_pu,
        alert,
    })
    .into_response()
}

// ─── Main ──────────────────────────────────────────────────────────────────

#[tokio::main]
async fn main() {
    // Install panic hook — logs structured panic info instead of default stderr dump.
    // K8s restartPolicy: Always will restart the pod after a panic-induced exit.
    let default_panic = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        let payload = info.payload();
        let msg = if let Some(s) = payload.downcast_ref::<&str>() {
            s.to_string()
        } else if let Some(s) = payload.downcast_ref::<String>() {
            s.clone()
        } else {
            "unknown panic".to_string()
        };
        let location = info
            .location()
            .map(|l| format!("{}:{}:{}", l.file(), l.line(), l.column()))
            .unwrap_or_else(|| "unknown".to_string());
        eprintln!("PANIC at {}: {}", location, msg);
        default_panic(info);
    }));

    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()),
        )
        .init();

    // Fail closed on tenancy misconfiguration: production refuses to boot
    // without GOTV_ENGINE_PARTY_KEYS or an explicit GOTV_SINGLE_TENANT_MODE=true.
    enforce_tenancy_config_at_startup();

    let port = std::env::var("PORT").unwrap_or_else(|_| "8101".to_string());
    let state = Arc::new(AppState::new());

    // Hydrate in-memory state from PostgreSQL on startup
    if state.persistence.is_enabled() {
        info!("Loading persisted state from PostgreSQL...");
        let vols = state.persistence.load_volunteers().await;
        if !vols.is_empty() {
            // Startup-only, no concurrent holders: recover a poisoned lock.
            let mut vol_map = state.volunteers.write().unwrap_or_else(|e| e.into_inner());
            for pv in &vols {
                let entry = vol_map.entry(pv.party_id).or_insert_with(Vec::new);
                entry.push(Volunteer {
                    id: pv.volunteer_id.clone(),
                    party_id: pv.party_id,
                    name: pv.full_name.clone(),
                    role: pv.role.clone(),
                    latitude: pv.latitude,
                    longitude: pv.longitude,
                    has_vehicle: pv.has_vehicle,
                    vehicle_capacity: pv.vehicle_capacity,
                    is_available: pv.is_active,
                    assigned_rides: 0,
                    state_code: None, // hydration source has no state code
                });
            }
            info!(count = vols.len(), "Hydrated volunteers from PostgreSQL");
        }

        let rides = state.persistence.load_pending_rides().await;
        if !rides.is_empty() {
            // Startup-only, no concurrent holders: recover a poisoned lock.
            let mut rr = state
                .ride_requests
                .write()
                .unwrap_or_else(|e| e.into_inner());
            for pr in &rides {
                rr.push(RideRequest {
                    id: pr.request_id.clone(),
                    party_id: pr.party_id,
                    contact_id: pr.contact_id.clone(),
                    pickup_lat: pr.pickup_latitude,
                    pickup_lng: pr.pickup_longitude,
                    polling_unit_code: pr.polling_unit_code.clone(),
                    status: pr.status.clone(),
                });
            }
            info!(
                count = rides.len(),
                "Hydrated pending rides from PostgreSQL"
            );
        }

        if !state.rebuild_rtree() {
            warn!("rtree rebuild failed after hydration (poisoned lock) — spatial queries will be stale until next registration");
        }
    }

    let app = build_router(state);

    let addr = format!("0.0.0.0:{}", port);
    info!("GOTV Engine starting on {}", addr);

    let listener = tokio::net::TcpListener::bind(&addr).await.unwrap();

    // Graceful shutdown: listen for SIGTERM/SIGINT, drain connections, then exit.
    // K8s sends SIGTERM first, then waits terminationGracePeriodSeconds before SIGKILL.
    // The preStop hook (sleep 3) ensures the Service endpoint is de-registered
    // before we start draining.
    axum::serve(listener, app)
        .with_graceful_shutdown(async {
            let ctrl_c = tokio::signal::ctrl_c();
            let mut sigterm =
                tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
                    .expect("failed to register SIGTERM handler");
            tokio::select! {
                _ = ctrl_c => info!("received SIGINT, starting graceful shutdown"),
                _ = sigterm.recv() => info!("received SIGTERM, starting graceful shutdown"),
            }
        })
        .await
        .unwrap();

    info!("GOTV Engine shut down gracefully");
}

// ─── Voting Crypto Handlers ───────────────────────────────────────────────
// SECURITY: crypto backend is not implemented in this build; handlers
// return 503 with a JSON error instead of fabricated artifacts.
fn crypto_unavailable(e: voting_crypto::VotingCryptoError) -> axum::response::Response {
    (
        StatusCode::SERVICE_UNAVAILABLE,
        Json(serde_json::json!({"error": e.to_string()})),
    )
        .into_response()
}

async fn encrypt_ballot_handler(
    Json(req): Json<voting_crypto::EncryptBallotRequest>,
) -> axum::response::Response {
    match voting_crypto::handle_encrypt_ballot(req) {
        Ok(resp) => Json(serde_json::json!(resp)).into_response(),
        Err(e) => crypto_unavailable(e),
    }
}

async fn shuffle_handler(
    Json(req): Json<voting_crypto::ShuffleRequest>,
) -> axum::response::Response {
    match voting_crypto::handle_shuffle(req) {
        Ok(resp) => Json(serde_json::json!(resp)).into_response(),
        Err(e) => crypto_unavailable(e),
    }
}

async fn merkle_tree_handler(
    Json(req): Json<voting_crypto::MerkleTreeRequest>,
) -> impl IntoResponse {
    let resp = voting_crypto::handle_merkle_tree(req);
    Json(serde_json::json!(resp))
}

async fn verify_keys_handler(
    Json(req): Json<voting_crypto::VerifyKeyRequest>,
) -> axum::response::Response {
    match voting_crypto::handle_verify_keys(req) {
        Ok(resp) => Json(serde_json::json!(resp)).into_response(),
        Err(e) => crypto_unavailable(e),
    }
}

// ─── Router ───────────────────────────────────────────────────────────────

/// Build the HTTP router. Extracted from main so tests can drive handlers
/// in-process (tower::ServiceExt::oneshot).
fn build_router(state: Arc<AppState>) -> Router {
    Router::new()
        .route("/health", get(health))
        .route("/gotv-engine/volunteers", post(register_volunteers))
        .route("/gotv-engine/polling-units", post(register_polling_units))
        .route("/gotv-engine/match", post(match_ride))
        .route("/gotv-engine/bulk-match", post(bulk_match_rides))
        .route("/gotv-engine/optimize-route", post(optimize_route))
        .route("/gotv-engine/proximity", get(proximity_polling_units))
        .route("/gotv-engine/coverage/:party_id", get(coverage_analysis))
        .route("/gotv-engine/middleware/status", get(middleware_status))
        .route("/gotv-engine/stats", get(engine_stats))
        // V2 endpoints
        .route(
            "/gotv-engine/territories/partition",
            post(partition_territories),
        )
        .route("/gotv-engine/turnout/predict", post(predict_turnout))
        .route("/gotv-engine/isochrone", post(calculate_isochrone))
        .route("/gotv-engine/geofence/check", post(check_geofence))
        // Voting crypto endpoints (Dapr service invocation targets)
        .route(
            "/gotv-engine/crypto/encrypt-ballot",
            post(encrypt_ballot_handler),
        )
        .route("/gotv-engine/crypto/shuffle", post(shuffle_handler))
        .route("/gotv-engine/crypto/merkle-tree", post(merkle_tree_handler))
        .route("/gotv-engine/verify-keys", post(verify_keys_handler))
        // Runs after auth (layers execute outermost-last): the limiter key is
        // the authenticated x-api-key identity. ~120 requests/minute per key.
        .layer(axum_mw::from_fn_with_state(
            Arc::new(platform::SlidingWindowLimiter::new(120, 60)),
            rate_limit,
        ))
        .layer(axum_mw::from_fn(internal_api_key_auth))
        .layer(cors_layer())
        .with_state(state)
}

// ─── Tests ──────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use tower::ServiceExt; // for `oneshot`

    /// R4-34c regression: a panic while a write guard is held poisons the
    /// RwLock; every subsequent handler touching that state used to panic on
    /// `.unwrap()` (permanent service-wide 500s-by-abort). Handlers must now
    /// return a clean HTTP 500 response instead.
    #[tokio::test]
    async fn poisoned_lock_yields_500_not_panic() {
        // Auth + tenancy config so the request reaches the handler.
        std::env::set_var("GOTV_ENGINE_API_KEY", "test-key");
        std::env::set_var("GOTV_SINGLE_TENANT_MODE", "true");
        std::env::remove_var("GOTV_ENGINE_PARTY_KEYS");

        let state = Arc::new(AppState::new());

        // Poison the volunteers lock: panic while holding a write guard.
        let s2 = state.clone();
        let handle = std::thread::spawn(move || {
            let _guard = s2.volunteers.write().unwrap();
            panic!("deliberate panic to poison the lock (test)");
        });
        assert!(
            handle.join().is_err(),
            "poisoning thread must have panicked"
        );
        assert!(state.volunteers.read().is_err(), "lock must be poisoned");

        // Hit a handler that writes state.volunteers — must 500, not abort.
        let app = build_router(state.clone());
        let req = Request::builder()
            .method("POST")
            .uri("/gotv-engine/volunteers")
            .header("x-api-key", "test-key")
            .header("content-type", "application/json")
            .body(Body::from(r#"{"party_id": 7, "volunteers": []}"#))
            .unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::INTERNAL_SERVER_ERROR);

        // A handler that only reads OTHER state (polling_units) still works —
        // poisoning one lock must not take down unrelated endpoints.
        let app = build_router(state.clone());
        let req = Request::builder()
            .method("GET")
            .uri("/gotv-engine/proximity?lat=6.5&lng=3.4")
            .header("x-api-key", "test-key")
            .body(Body::empty())
            .unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::OK);

        std::env::remove_var("GOTV_ENGINE_API_KEY");
        std::env::remove_var("GOTV_SINGLE_TENANT_MODE");
    }
}
