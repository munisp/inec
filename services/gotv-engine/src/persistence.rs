// GOTV Engine Persistence Layer — PostgreSQL + Redis backing for in-memory state.
// Solves CRITICAL #6: Rust engine volatile (in-memory only) data loss on restart.
// Also implements: territory partitioning, predictive turnout, and isochrone support.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::env;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

/// PostgreSQL persistence client for volunteer/ride state.
#[derive(Clone)]
pub struct PersistenceLayer {
    pg_url: Option<String>,
    redis_url: Option<String>,
    client: Option<reqwest::Client>,
    /// Count of failed persistence writes. Responses claim "persistent" when
    /// the layer is enabled, so failures must be loud and observable: every
    /// failed write is logged and counted, and /health reports "degraded"
    /// while this counter is non-zero.
    write_failures: Arc<AtomicU64>,
}

impl PersistenceLayer {
    pub fn new() -> Self {
        Self {
            pg_url: env::var("DATABASE_URL").ok(),
            redis_url: env::var("REDIS_URL").ok(),
            client: Some(
                reqwest::Client::builder()
                    .timeout(std::time::Duration::from_secs(5))
                    .build()
                    .unwrap_or_default(),
            ),
            write_failures: Arc::new(AtomicU64::new(0)),
        }
    }

    pub fn is_enabled(&self) -> bool {
        self.pg_url.is_some()
    }

    /// Number of persistence writes that have failed since startup.
    pub fn write_failures(&self) -> u64 {
        self.write_failures.load(Ordering::Relaxed)
    }

    /// Persistence remains fire-and-forget by design (the in-memory engine
    /// must not block on backend latency), but failures are OBSERVABLE: each
    /// one is logged at error level and counted in `write_failures`, which is
    /// surfaced on /health and /gotv-engine/stats.
    fn record_failure(&self, url: &str, err: &str) {
        let n = self.write_failures.fetch_add(1, Ordering::Relaxed) + 1;
        tracing::error!(url = %url, error = %err, total_failures = n, "persistence write failed");
    }

    /// POST JSON, logging and counting any failure instead of discarding it.
    async fn post_json(&self, url: String, body: serde_json::Value) {
        if let Some(ref client) = self.client {
            match client.post(&url).json(&body).send().await {
                Ok(resp) if resp.status().is_success() => {}
                Ok(resp) => self.record_failure(&url, &format!("HTTP {}", resp.status())),
                Err(e) => self.record_failure(&url, &e.to_string()),
            }
        }
    }

    /// Persist volunteer position to PostgreSQL via GOTV backend API.
    pub async fn save_volunteer_position(&self, vol_id: &str, party_id: i64, lat: f64, lng: f64) {
        if self.pg_url.is_some() {
            let api_url = env::var("GOTV_BACKEND_URL")
                .unwrap_or_else(|_| "http://localhost:8103".to_string());
            self.post_json(
                format!("{}/gotv/volunteers/{}/location", api_url, vol_id),
                serde_json::json!({
                    "latitude": lat,
                    "longitude": lng,
                    "party_id": party_id,
                }),
            )
            .await;
        }
    }

    /// Save ride match to persistent store.
    pub async fn save_ride_match(&self, ride_id: &str, volunteer_id: &str, distance_km: f64) {
        if self.pg_url.is_some() {
            let api_url = env::var("GOTV_BACKEND_URL")
                .unwrap_or_else(|_| "http://localhost:8103".to_string());
            self.post_json(
                format!("{}/gotv/rides/{}/match", api_url, ride_id),
                serde_json::json!({
                    "volunteer_id": volunteer_id,
                    "distance_km": distance_km,
                }),
            )
            .await;
        }
    }

    /// Cache volunteer positions in Redis for fast retrieval.
    pub async fn cache_volunteer_position(&self, vol_id: &str, lat: f64, lng: f64) {
        if let Some(ref redis_url) = self.redis_url {
            if let Some(ref client) = self.client {
                let url = format!(
                    "{}/GEOADD/gotv:volunteer_positions/{}/{}/{}",
                    redis_url, lng, lat, vol_id
                );
                match client.post(&url).send().await {
                    Ok(resp) if resp.status().is_success() => {}
                    Ok(resp) => self.record_failure(&url, &format!("HTTP {}", resp.status())),
                    Err(e) => self.record_failure(&url, &e.to_string()),
                }
            }
        }
    }

    /// Load all volunteers from PostgreSQL on startup (hydrate in-memory state).
    pub async fn load_volunteers(&self) -> Vec<PersistedVolunteer> {
        if let Some(ref _url) = self.pg_url {
            if let Some(ref client) = self.client {
                let api_url = env::var("GOTV_BACKEND_URL")
                    .unwrap_or_else(|_| "http://localhost:8103".to_string());
                let resp = client
                    .get(format!("{}/gotv/geo/volunteers?limit=10000", api_url))
                    .send()
                    .await;
                if let Ok(resp) = resp {
                    if let Ok(vols) = resp.json::<Vec<PersistedVolunteer>>().await {
                        return vols;
                    }
                }
            }
        }
        Vec::new()
    }

    /// Load pending ride requests from PostgreSQL on startup.
    pub async fn load_pending_rides(&self) -> Vec<PersistedRide> {
        if let Some(ref _url) = self.pg_url {
            if let Some(ref client) = self.client {
                let api_url = env::var("GOTV_BACKEND_URL")
                    .unwrap_or_else(|_| "http://localhost:8103".to_string());
                let resp = client
                    .get(format!(
                        "{}/gotv/geo/rides?status=pending&limit=10000",
                        api_url
                    ))
                    .send()
                    .await;
                if let Ok(resp) = resp {
                    if let Ok(rides) = resp.json::<Vec<PersistedRide>>().await {
                        return rides;
                    }
                }
            }
        }
        Vec::new()
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PersistedVolunteer {
    pub volunteer_id: String,
    pub party_id: i64,
    pub full_name: String,
    pub role: String,
    pub latitude: f64,
    pub longitude: f64,
    pub has_vehicle: bool,
    pub vehicle_capacity: i32,
    pub is_active: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PersistedRide {
    pub request_id: String,
    pub party_id: i64,
    pub contact_id: String,
    pub pickup_latitude: f64,
    pub pickup_longitude: f64,
    pub polling_unit_code: String,
    pub status: String,
}

// ─── ENHANCE #15: Territory Partitioning ───────────────────────────────────

/// Voronoi-based territory partition for canvasser assignment.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TerritoryPartition {
    pub territory_id: String,
    pub volunteer_id: String,
    pub ward_code: String,
    pub center_lat: f64,
    pub center_lng: f64,
    pub radius_km: f64,
    pub contact_count: i32,
    pub boundary_points: Vec<(f64, f64)>,
}

/// Partition a ward into canvasser territories using simple grid-based approach.
pub fn partition_ward_territories(
    volunteers: &[(String, f64, f64)], // (id, lat, lng)
    contacts: &[(f64, f64)],           // (lat, lng) of contacts in ward
    ward_code: &str,
) -> Vec<TerritoryPartition> {
    if volunteers.is_empty() {
        return Vec::new();
    }

    // Assign each contact to nearest volunteer (Voronoi-like partition)
    let mut assignments: HashMap<usize, Vec<(f64, f64)>> = HashMap::new();
    for &(clat, clng) in contacts {
        let mut nearest_idx = 0;
        let mut nearest_dist = f64::MAX;
        for (i, (_, vlat, vlng)) in volunteers.iter().enumerate() {
            let dist = haversine_distance(clat, clng, *vlat, *vlng);
            if dist < nearest_dist {
                nearest_dist = dist;
                nearest_idx = i;
            }
        }
        assignments
            .entry(nearest_idx)
            .or_default()
            .push((clat, clng));
    }

    let mut territories = Vec::new();
    for (i, (vol_id, vlat, vlng)) in volunteers.iter().enumerate() {
        let assigned = assignments.get(&i).map(|v| v.len()).unwrap_or(0);
        // Calculate bounding radius
        let max_dist = assignments
            .get(&i)
            .map(|pts| {
                pts.iter()
                    .map(|(clat, clng)| haversine_distance(*vlat, *vlng, *clat, *clng))
                    .fold(0.0f64, f64::max)
            })
            .unwrap_or(0.5);

        territories.push(TerritoryPartition {
            territory_id: format!("terr-{}-{}", ward_code, i + 1),
            volunteer_id: vol_id.clone(),
            ward_code: ward_code.to_string(),
            center_lat: *vlat,
            center_lng: *vlng,
            radius_km: max_dist,
            contact_count: assigned as i32,
            boundary_points: Vec::new(), // Simplified: no full polygon
        });
    }
    territories
}

// ─── INNOVATE #18: Predictive Turnout ──────────────────────────────────────

/// Simple turnout prediction based on historical data + real-time signals.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TurnoutPrediction {
    pub ward_code: String,
    pub predicted_turnout_pct: f64,
    /// Heuristic quality score (0-1) reflecting data availability only —
    /// SECURITY/LABELING: this is NOT a trained-model confidence. It was
    /// previously a hardcoded 0.72/0.45 presented as model confidence.
    pub heuristic_score: f64,
    pub risk_level: String, // high, medium, low
    pub factors: Vec<String>,
}

/// Predict turnout for a ward based on available signals.
pub fn predict_ward_turnout(
    historical_turnout_pct: f64,
    pledge_count: i32,
    registered_voters: i32,
    active_volunteers: i32,
    weather_clear: bool,
    ward_code: &str,
) -> TurnoutPrediction {
    let mut predicted = historical_turnout_pct;
    let mut factors = Vec::new();

    // Pledge-to-registered ratio boost
    let pledge_ratio = if registered_voters > 0 {
        pledge_count as f64 / registered_voters as f64
    } else {
        0.0
    };
    if pledge_ratio > 0.3 {
        predicted += 5.0;
        factors.push("high_pledge_ratio".to_string());
    }

    // Volunteer presence boost
    if active_volunteers > 3 {
        predicted += 3.0;
        factors.push("strong_volunteer_presence".to_string());
    }

    // Weather impact
    if !weather_clear {
        predicted -= 8.0;
        factors.push("poor_weather".to_string());
    }

    predicted = predicted.clamp(5.0, 95.0);

    let risk_level = if predicted < 40.0 {
        "high".to_string()
    } else if predicted < 60.0 {
        "medium".to_string()
    } else {
        "low".to_string()
    };

    // Heuristic data-availability score — NOT a trained-model confidence.
    let heuristic_score = if historical_turnout_pct > 0.0 {
        0.72
    } else {
        0.45
    };

    TurnoutPrediction {
        ward_code: ward_code.to_string(),
        predicted_turnout_pct: predicted,
        heuristic_score,
        risk_level,
        factors,
    }
}

// ─── Isochrone Service Areas ───────────────────────────────────────────────

/// Calculate approximate isochrone (drive-time radius) for a volunteer.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Isochrone {
    pub volunteer_id: String,
    pub center_lat: f64,
    pub center_lng: f64,
    pub radius_km_15min: f64,
    pub radius_km_30min: f64,
    pub radius_km_45min: f64,
    pub reachable_pus: Vec<String>,
}

/// Estimate isochrone radii based on average Nigerian urban/rural speeds.
pub fn calculate_isochrone(
    vol_lat: f64,
    vol_lng: f64,
    vol_id: &str,
    polling_units: &[(String, f64, f64)], // (code, lat, lng)
    is_urban: bool,
) -> Isochrone {
    // Average speeds: urban 25 km/h, rural 40 km/h (Nigerian roads)
    let avg_speed_kmh = if is_urban { 25.0 } else { 40.0 };
    let r15 = avg_speed_kmh * (15.0 / 60.0); // 6.25 or 10 km
    let r30 = avg_speed_kmh * (30.0 / 60.0); // 12.5 or 20 km
    let r45 = avg_speed_kmh * (45.0 / 60.0); // 18.75 or 30 km

    let reachable: Vec<String> = polling_units
        .iter()
        .filter(|(_, plat, plng)| haversine_distance(vol_lat, vol_lng, *plat, *plng) <= r30)
        .map(|(code, _, _)| code.clone())
        .collect();

    Isochrone {
        volunteer_id: vol_id.to_string(),
        center_lat: vol_lat,
        center_lng: vol_lng,
        radius_km_15min: r15,
        radius_km_30min: r30,
        radius_km_45min: r45,
        reachable_pus: reachable,
    }
}

/// Haversine distance in km.
pub fn haversine_distance(lat1: f64, lng1: f64, lat2: f64, lng2: f64) -> f64 {
    let r = 6371.0; // Earth radius km
    let dlat = (lat2 - lat1).to_radians();
    let dlng = (lng2 - lng1).to_radians();
    let a = (dlat / 2.0).sin().powi(2)
        + lat1.to_radians().cos() * lat2.to_radians().cos() * (dlng / 2.0).sin().powi(2);
    r * 2.0 * a.sqrt().asin()
}

#[cfg(test)]
mod tests {
    use super::*;

    /// R4-28 regression: a failed persistence write must be counted and
    /// observable — never silently swallowed. Uses a scratch DATABASE_URL so
    /// the layer is "enabled" and an unroutable backend address so the POST
    /// deterministically fails (connection refused).
    #[tokio::test]
    async fn failed_write_increments_failure_counter() {
        // Point the backend at a port nothing listens on.
        std::env::set_var("DATABASE_URL", "postgres://localhost:1/x");
        std::env::set_var("GOTV_BACKEND_URL", "http://127.0.0.1:1");
        let layer = PersistenceLayer::new();
        assert!(layer.is_enabled());
        let before = layer.write_failures();

        layer.save_ride_match("ride-1", "vol-1", 1.5).await;
        layer.save_volunteer_position("vol-1", 7, 6.5, 3.4).await;

        assert_eq!(
            layer.write_failures(),
            before + 2,
            "both failed writes must be counted"
        );
        std::env::remove_var("GOTV_BACKEND_URL");
        std::env::remove_var("DATABASE_URL");

        // Disabled layer (no DATABASE_URL): writes are no-ops and the counter
        // stays at zero — silence is correct only when nothing was claimed.
        let layer = PersistenceLayer::new();
        assert!(!layer.is_enabled());
        layer.save_ride_match("ride-2", "vol-2", 0.5).await;
        assert_eq!(layer.write_failures(), 0);
    }
}
