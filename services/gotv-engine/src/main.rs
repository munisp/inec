// GOTV Engine — Rust-based volunteer matching and ride-to-polls geo engine.
// Uses R-tree spatial index for O(log n) nearest-volunteer lookup.
// Handles: ride matching, canvasser route optimization, polling unit proximity.

mod middleware;
mod persistence;
pub mod platform;
pub mod voting_crypto;

use axum::{
    extract::{Json, Path, Query, State},
    http::{StatusCode, Request},
    middleware as axum_mw,
    response::IntoResponse,
    routing::{get, post},
    Router,
    body::Body,
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
    pub role: String,          // canvasser, driver, coordinator, phone_banker, team_lead
    pub latitude: f64,
    pub longitude: f64,
    pub has_vehicle: bool,
    pub vehicle_capacity: i32,
    pub is_available: bool,
    pub assigned_rides: i32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PollingUnit {
    pub code: String,
    pub name: String,
    pub latitude: f64,
    pub longitude: f64,
    pub ward_code: String,
    pub registered_voters: i32,
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
    volunteers: RwLock<HashMap<i64, Vec<Volunteer>>>,       // party_id -> volunteers
    rtree: RwLock<RTree<VolunteerPoint>>,                   // spatial index
    polling_units: RwLock<HashMap<String, PollingUnit>>,     // code -> PU
    ride_requests: RwLock<Vec<RideRequest>>,
    mw: middleware::Middleware,
    persistence: persistence::PersistenceLayer,
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

    fn rebuild_rtree(&self) {
        let vols = self.volunteers.read().unwrap();
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
        *self.rtree.write().unwrap() = tree;
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

async fn internal_api_key_auth(
    req: Request<Body>,
    next: axum_mw::Next,
) -> impl IntoResponse {
    // Health endpoint is always public
    if req.uri().path() == "/health" {
        return next.run(req).await;
    }
    // Accept dapr-api-token or GOTV_ENGINE_API_KEY
    let expected_key = std::env::var("GOTV_ENGINE_API_KEY").unwrap_or_default();
    let has_dapr = req.headers().get("dapr-api-token").is_some();
    let has_key = req.headers()
        .get("x-api-key")
        .and_then(|v| v.to_str().ok())
        .map(|v| !expected_key.is_empty() && v == expected_key)
        .unwrap_or(false);

    // In dev mode (no key configured), allow all requests
    if expected_key.is_empty() || has_dapr || has_key {
        return next.run(req).await;
    }

    (StatusCode::UNAUTHORIZED, Json(serde_json::json!({"error": "unauthorized"}))).into_response()
}

// ─── Handlers ──────────────────────────────────────────────────────────────

async fn health() -> impl IntoResponse {
    Json(serde_json::json!({
        "service": "gotv-engine",
        "status": "healthy",
        "version": "1.0.0",
        "language": "rust",
        "capabilities": ["ride_matching", "route_optimization", "proximity_search", "coverage_analysis"]
    }))
}

async fn register_volunteers(
    State(state): State<Arc<AppState>>,
    Json(req): Json<RegisterVolunteersRequest>,
) -> impl IntoResponse {
    let count = req.volunteers.len();
    {
        let mut vols = state.volunteers.write().unwrap();
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
    state.rebuild_rtree();
    info!(party_id = req.party_id, count = count, "Registered volunteers");

    (StatusCode::OK, Json(serde_json::json!({
        "registered": count,
        "party_id": req.party_id
    })))
}

async fn register_polling_units(
    State(state): State<Arc<AppState>>,
    Json(req): Json<RegisterPUsRequest>,
) -> impl IntoResponse {
    let count = req.polling_units.len();
    let mut pus = state.polling_units.write().unwrap();
    for pu in req.polling_units {
        pus.insert(pu.code.clone(), pu);
    }
    info!(count = count, "Registered polling units");

    (StatusCode::OK, Json(serde_json::json!({"registered": count})))
}

async fn match_ride(
    State(state): State<Arc<AppState>>,
    Json(req): Json<MatchRideRequest>,
) -> impl IntoResponse {
    let max_dist = req.max_distance_km.unwrap_or(10.0);
    let require_vehicle = req.require_vehicle.unwrap_or(true);

    // Scope RwLock guards so they're dropped before any .await
    let (results, no_match) = {
        let tree = state.rtree.read().unwrap();
        let pickup = VolunteerPoint {
            id: String::new(), party_id: req.party_id, lat: req.pickup_lat, lng: req.pickup_lng,
            has_vehicle: false, capacity: 0, available: false,
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
            let vols = state.volunteers.read().unwrap();
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
        );
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
    state.mw.publish_kafka("gotv.rides", req.ride_id.as_deref().unwrap_or("unknown"), &event).await;
    state.mw.stream_fluvio("gotv-ride-matches", &event).await;

    (StatusCode::OK, Json(serde_json::json!({
        "matches": results,
        "total_candidates": results.len(),
        "polling_unit": req.polling_unit_code
    })))
}

async fn bulk_match_rides(
    State(state): State<Arc<AppState>>,
    Json(req): Json<BulkMatchRequest>,
) -> impl IntoResponse {
    let tree = state.rtree.read().unwrap();
    let mut assigned: HashMap<String, bool> = HashMap::new();
    let mut results = Vec::new();

    for ride_req in &req.requests {
        let pickup_point = Point::new(ride_req.pickup_lng, ride_req.pickup_lat);
        let pickup_vp = VolunteerPoint {
            id: String::new(), party_id: req.party_id, lat: ride_req.pickup_lat, lng: ride_req.pickup_lng,
            has_vehicle: false, capacity: 0, available: false,
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
    }))
}

async fn optimize_route(
    State(_state): State<Arc<AppState>>,
    Json(req): Json<RouteOptRequest>,
) -> impl IntoResponse {
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
        total_distance += haversine_km(last.lat, last.lng, req.destination.lat, req.destination.lng);
    }

    let route = OptimizedRoute {
        volunteer_id: req.volunteer_id,
        ordered_stops,
        total_distance_km: (total_distance * 100.0).round() / 100.0,
        estimated_time_minutes: (total_distance / 25.0 * 60.0).round(), // ~25 km/h with stops
    };

    Json(serde_json::json!(route))
}

async fn proximity_polling_units(
    State(state): State<Arc<AppState>>,
    Query(query): Query<ProximityQuery>,
) -> impl IntoResponse {
    let radius = query.radius_km.unwrap_or(5.0);
    let limit = query.limit.unwrap_or(10);
    let pus = state.polling_units.read().unwrap();

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
}

async fn coverage_analysis(
    State(state): State<Arc<AppState>>,
    Path(party_id): Path<i64>,
) -> impl IntoResponse {
    let vols = state.volunteers.read().unwrap();
    let pus = state.polling_units.read().unwrap();

    let party_vols = vols.get(&party_id).cloned().unwrap_or_default();
    let total_volunteers = party_vols.len();
    let total_drivers = party_vols.iter().filter(|v| v.has_vehicle).count();
    let total_capacity: i32 = party_vols.iter().map(|v| v.vehicle_capacity).sum();

    // Coverage by state (check which PUs have a driver within 10km)
    let mut coverage_by_state: HashMap<String, StateCoverage> = HashMap::new();
    let mut covered_pus: HashMap<String, bool> = HashMap::new();

    for pu in pus.values() {
        let pu_point = Point::new(pu.longitude, pu.latitude);
        let state_key = pu.ward_code.get(..5).unwrap_or("UNK").to_string(); // approximate state from ward code prefix

        let entry = coverage_by_state.entry(state_key).or_default();
        entry.polling_units += 1;

        for vol in &party_vols {
            if vol.latitude == 0.0 || vol.longitude == 0.0 {
                continue;
            }
            let vol_point = Point::new(vol.longitude, vol.latitude);
            let dist = pu_point.haversine_distance(&vol_point) / 1000.0;
            if dist <= 10.0 {
                covered_pus.insert(pu.code.clone(), true);
                break;
            }
        }
    }

    let uncovered: Vec<String> = pus
        .keys()
        .filter(|code| !covered_pus.contains_key(*code))
        .take(100)
        .cloned()
        .collect();

    // Count volunteers per approximate state
    for vol in &party_vols {
        // Use assigned state if available, otherwise approximate
        let state_key = vol
            .id
            .get(..5)
            .unwrap_or("UNK")
            .to_string();
        let entry = coverage_by_state.entry(state_key).or_default();
        entry.volunteers += 1;
        if vol.has_vehicle {
            entry.drivers += 1;
            entry.capacity += vol.vehicle_capacity;
        }
    }

    Json(CoverageAnalysis {
        party_id,
        total_volunteers,
        total_drivers,
        total_vehicle_capacity: total_capacity,
        coverage_by_state,
        uncovered_pus: uncovered,
    })
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
    Json(req): Json<TerritoryPartitionRequest>,
) -> impl IntoResponse {
    let vols = state.volunteers.read().unwrap();
    let party_vols = vols.get(&req.party_id);

    let vol_positions: Vec<(String, f64, f64)> = party_vols
        .map(|vs| {
            vs.iter()
                .filter(|v| v.is_available && v.latitude != 0.0)
                .map(|v| (v.id.clone(), v.latitude, v.longitude))
                .collect()
        })
        .unwrap_or_default();

    let contact_locs: Vec<(f64, f64)> = req.contact_locations.iter().map(|c| (c.lat, c.lng)).collect();

    let territories = persistence::partition_ward_territories(
        &vol_positions,
        &contact_locs,
        &req.ward_code,
    );

    (StatusCode::OK, Json(serde_json::json!({
        "ward_code": req.ward_code,
        "territories": territories,
        "total_volunteers": vol_positions.len(),
        "total_contacts": contact_locs.len(),
    })))
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

async fn predict_turnout(
    Json(req): Json<TurnoutPredictionRequest>,
) -> impl IntoResponse {
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
    Json(req): Json<IsochroneRequest>,
) -> impl IntoResponse {
    let vols = state.volunteers.read().unwrap();
    let party_vols = vols.get(&req.party_id);

    let vol = party_vols.and_then(|vs| vs.iter().find(|v| v.id == req.volunteer_id));

    match vol {
        Some(v) => {
            let pus = state.polling_units.read().unwrap();
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
        None => (StatusCode::NOT_FOUND, Json(serde_json::json!({"error": "volunteer not found"}))),
    }
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

async fn check_geofence(
    State(state): State<Arc<AppState>>,
    Json(req): Json<GeofenceCheckRequest>,
) -> impl IntoResponse {
    let pus = state.polling_units.read().unwrap();
    let ward_pus: Vec<&PollingUnit> = pus.values()
        .filter(|pu| pu.ward_code == req.assigned_ward)
        .collect();

    if ward_pus.is_empty() {
        return Json(GeofenceResult {
            in_zone: true,
            distance_to_center_km: 0.0,
            nearest_pu: String::new(),
            alert: None,
        });
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
        Some(format!("Volunteer {} is {:.1}km outside assigned ward {}", req.volunteer_id, dist_to_center, req.assigned_ward))
    } else {
        None
    };

    Json(GeofenceResult {
        in_zone,
        distance_to_center_km: dist_to_center,
        nearest_pu,
        alert,
    })
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
        let location = info.location().map(|l| format!("{}:{}:{}", l.file(), l.line(), l.column()))
            .unwrap_or_else(|| "unknown".to_string());
        eprintln!("PANIC at {}: {}", location, msg);
        default_panic(info);
    }));

    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "info".into()),
        )
        .init();

    let port = std::env::var("PORT").unwrap_or_else(|_| "8101".to_string());
    let state = Arc::new(AppState::new());

    // Hydrate in-memory state from PostgreSQL on startup
    if state.persistence.is_enabled() {
        info!("Loading persisted state from PostgreSQL...");
        let vols = state.persistence.load_volunteers().await;
        if !vols.is_empty() {
            let mut vol_map = state.volunteers.write().unwrap();
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
                });
            }
            info!(count = vols.len(), "Hydrated volunteers from PostgreSQL");
        }

        let rides = state.persistence.load_pending_rides().await;
        if !rides.is_empty() {
            let mut rr = state.ride_requests.write().unwrap();
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
            info!(count = rides.len(), "Hydrated pending rides from PostgreSQL");
        }

        state.rebuild_rtree();
    }

    // ── Voting Crypto Handlers ────────────────────────────────────────────
    async fn encrypt_ballot_handler(
        Json(req): Json<voting_crypto::EncryptBallotRequest>,
    ) -> impl IntoResponse {
        let resp = voting_crypto::handle_encrypt_ballot(req);
        Json(serde_json::json!(resp))
    }

    async fn shuffle_handler(
        Json(req): Json<voting_crypto::ShuffleRequest>,
    ) -> impl IntoResponse {
        let resp = voting_crypto::handle_shuffle(req);
        Json(serde_json::json!(resp))
    }

    async fn merkle_tree_handler(
        Json(req): Json<voting_crypto::MerkleTreeRequest>,
    ) -> impl IntoResponse {
        let resp = voting_crypto::handle_merkle_tree(req);
        Json(serde_json::json!(resp))
    }

    async fn verify_keys_handler(
        Json(req): Json<voting_crypto::VerifyKeyRequest>,
    ) -> impl IntoResponse {
        let resp = voting_crypto::handle_verify_keys(req);
        Json(serde_json::json!(resp))
    }

    let app = Router::new()
        .route("/health", get(health))
        .route("/gotv-engine/volunteers", post(register_volunteers))
        .route("/gotv-engine/polling-units", post(register_polling_units))
        .route("/gotv-engine/match", post(match_ride))
        .route("/gotv-engine/bulk-match", post(bulk_match_rides))
        .route("/gotv-engine/optimize-route", post(optimize_route))
        .route("/gotv-engine/proximity", get(proximity_polling_units))
        .route("/gotv-engine/coverage/:party_id", get(coverage_analysis))
        .route("/gotv-engine/middleware/status", get(middleware_status))
        // V2 endpoints
        .route("/gotv-engine/territories/partition", post(partition_territories))
        .route("/gotv-engine/turnout/predict", post(predict_turnout))
        .route("/gotv-engine/isochrone", post(calculate_isochrone))
        .route("/gotv-engine/geofence/check", post(check_geofence))
        // Voting crypto endpoints (Dapr service invocation targets)
        .route("/gotv-engine/crypto/encrypt-ballot", post(encrypt_ballot_handler))
        .route("/gotv-engine/crypto/shuffle", post(shuffle_handler))
        .route("/gotv-engine/crypto/merkle-tree", post(merkle_tree_handler))
        .route("/gotv-engine/verify-keys", post(verify_keys_handler))
        .layer(axum_mw::from_fn(internal_api_key_auth))
        .layer(CorsLayer::permissive())
        .with_state(state);

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
            let mut sigterm = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
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
