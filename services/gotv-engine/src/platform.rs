//! Platform Improvements — Rust-side implementations
//!
//! P4-1: TSP route optimization with 2-opt improvement
//! P4-3: Blockchain Merkle tree verification
//! P4-4: Crowd density estimation (model inference stub)
//! P3-3: Isochrone computation with haversine
//! P0-3: Rate limiting with sliding window

use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use std::time::{Duration, Instant};

// ═══════════════════════════════════════════════════════════════════════════
// P4-1: Advanced Route Optimization (Nearest-Neighbor + 2-opt)
// ═══════════════════════════════════════════════════════════════════════════

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RouteStop {
    pub id: String,
    pub lat: f64,
    pub lng: f64,
    pub name: String,
    pub priority: i32,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct OptimizedRoute {
    pub stops: Vec<RouteStop>,
    pub total_distance_m: f64,
    pub estimated_time_min: f64,
    pub improvement_pct: f64,
    pub algorithm: String,
}

fn haversine_m(lat1: f64, lng1: f64, lat2: f64, lng2: f64) -> f64 {
    const R: f64 = 6_371_000.0;
    let dlat = (lat2 - lat1).to_radians();
    let dlng = (lng2 - lng1).to_radians();
    let a = (dlat / 2.0).sin().powi(2)
        + lat1.to_radians().cos() * lat2.to_radians().cos() * (dlng / 2.0).sin().powi(2);
    R * 2.0 * a.sqrt().atan2((1.0 - a).sqrt())
}

fn route_distance(stops: &[RouteStop], start_lat: f64, start_lng: f64) -> f64 {
    if stops.is_empty() {
        return 0.0;
    }
    let mut total = haversine_m(start_lat, start_lng, stops[0].lat, stops[0].lng);
    for i in 1..stops.len() {
        total += haversine_m(stops[i - 1].lat, stops[i - 1].lng, stops[i].lat, stops[i].lng);
    }
    total
}

/// Nearest-neighbor heuristic for initial route
fn nearest_neighbor(start_lat: f64, start_lng: f64, stops: &[RouteStop]) -> Vec<RouteStop> {
    let mut remaining: Vec<_> = stops.to_vec();
    let mut route = Vec::with_capacity(stops.len());
    let mut cur_lat = start_lat;
    let mut cur_lng = start_lng;

    while !remaining.is_empty() {
        let mut best_idx = 0;
        let mut best_dist = f64::MAX;
        for (i, s) in remaining.iter().enumerate() {
            let d = haversine_m(cur_lat, cur_lng, s.lat, s.lng);
            if d < best_dist {
                best_dist = d;
                best_idx = i;
            }
        }
        let chosen = remaining.remove(best_idx);
        cur_lat = chosen.lat;
        cur_lng = chosen.lng;
        route.push(chosen);
    }
    route
}

/// 2-opt local search improvement
fn two_opt_improve(route: &mut Vec<RouteStop>, start_lat: f64, start_lng: f64, max_iterations: usize) {
    let n = route.len();
    if n < 4 {
        return;
    }

    for _ in 0..max_iterations {
        let mut improved = false;
        for i in 0..n - 1 {
            for j in (i + 2)..n {
                let before = segment_cost(route, i, j, start_lat, start_lng);
                route[i + 1..=j].reverse();
                let after = segment_cost(route, i, j, start_lat, start_lng);
                if after < before {
                    improved = true;
                } else {
                    route[i + 1..=j].reverse(); // revert
                }
            }
        }
        if !improved {
            break;
        }
    }
}

fn segment_cost(route: &[RouteStop], i: usize, j: usize, start_lat: f64, start_lng: f64) -> f64 {
    let prev = if i == 0 {
        (start_lat, start_lng)
    } else {
        (route[i].lat, route[i].lng)
    };
    let mut cost = haversine_m(prev.0, prev.1, route[i + 1].lat, route[i + 1].lng);
    for k in (i + 1)..j {
        cost += haversine_m(route[k].lat, route[k].lng, route[k + 1].lat, route[k + 1].lng);
    }
    if j + 1 < route.len() {
        cost += haversine_m(route[j].lat, route[j].lng, route[j + 1].lat, route[j + 1].lng);
    }
    cost
}

pub fn optimize_route(start_lat: f64, start_lng: f64, stops: Vec<RouteStop>) -> OptimizedRoute {
    if stops.is_empty() {
        return OptimizedRoute {
            stops: vec![],
            total_distance_m: 0.0,
            estimated_time_min: 0.0,
            improvement_pct: 0.0,
            algorithm: "empty".to_string(),
        };
    }

    // Phase 1: Nearest neighbor
    let mut route = nearest_neighbor(start_lat, start_lng, &stops);
    let nn_distance = route_distance(&route, start_lat, start_lng);

    // Phase 2: 2-opt improvement
    two_opt_improve(&mut route, start_lat, start_lng, 100);
    let opt_distance = route_distance(&route, start_lat, start_lng);

    let improvement = if nn_distance > 0.0 {
        ((nn_distance - opt_distance) / nn_distance * 100.0).max(0.0)
    } else {
        0.0
    };

    OptimizedRoute {
        stops: route,
        total_distance_m: (opt_distance * 10.0).round() / 10.0,
        estimated_time_min: (opt_distance / 80.0 * 10.0).round() / 10.0, // ~80m/min walking
        improvement_pct: (improvement * 10.0).round() / 10.0,
        algorithm: "nearest-neighbor-2opt".to_string(),
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// P4-3: Merkle Tree for Pledge Verification
// ═══════════════════════════════════════════════════════════════════════════

use sha2::{Digest, Sha256};

pub fn compute_merkle_root(items: &[String]) -> String {
    if items.is_empty() {
        return String::new();
    }

    let mut hashes: Vec<String> = items
        .iter()
        .map(|item| {
            let mut hasher = Sha256::new();
            hasher.update(item.as_bytes());
            hex::encode(hasher.finalize())
        })
        .collect();

    while hashes.len() > 1 {
        let mut next = Vec::new();
        for chunk in hashes.chunks(2) {
            let combined = if chunk.len() == 2 {
                format!("{}{}", chunk[0], chunk[1])
            } else {
                format!("{}{}", chunk[0], chunk[0])
            };
            let mut hasher = Sha256::new();
            hasher.update(combined.as_bytes());
            next.push(hex::encode(hasher.finalize()));
        }
        hashes = next;
    }

    hashes.into_iter().next().unwrap_or_default()
}

// ═══════════════════════════════════════════════════════════════════════════
// P0-3: Sliding Window Rate Limiter
// ═══════════════════════════════════════════════════════════════════════════

pub struct SlidingWindowLimiter {
    windows: RwLock<HashMap<String, Vec<Instant>>>,
    max_requests: usize,
    window_duration: Duration,
}

impl SlidingWindowLimiter {
    pub fn new(max_requests: usize, window_secs: u64) -> Self {
        Self {
            windows: RwLock::new(HashMap::new()),
            max_requests,
            window_duration: Duration::from_secs(window_secs),
        }
    }

    pub fn allow(&self, key: &str) -> bool {
        let now = Instant::now();
        let cutoff = now - self.window_duration;

        let mut windows = self.windows.write().unwrap();
        let entry = windows.entry(key.to_string()).or_insert_with(Vec::new);

        // Remove expired entries
        entry.retain(|&t| t > cutoff);

        if entry.len() >= self.max_requests {
            return false;
        }

        entry.push(now);
        true
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// P3-3: Isochrone Computation
// ═══════════════════════════════════════════════════════════════════════════

#[derive(Debug, Serialize, Deserialize)]
pub struct Isochrone {
    pub center: (f64, f64),
    pub mode: String,
    pub minutes: u32,
    pub radius_m: f64,
    pub polygon: Vec<(f64, f64)>,
}

pub fn compute_isochrone(lat: f64, lng: f64, mode: &str, minutes: u32) -> Isochrone {
    let speed_mpm = match mode {
        "driving" => 500.0,
        "cycling" => 250.0,
        _ => 80.0, // walking
    };
    let radius = speed_mpm * minutes as f64;

    let mut polygon = Vec::with_capacity(17);
    for i in 0..16 {
        let angle = i as f64 * (2.0 * std::f64::consts::PI / 16.0);
        let dlat = (radius * angle.cos()) / 111_320.0;
        let dlng = (radius * angle.sin()) / (111_320.0 * lat.to_radians().cos());
        polygon.push((
            ((lat + dlat) * 1_000_000.0).round() / 1_000_000.0,
            ((lng + dlng) * 1_000_000.0).round() / 1_000_000.0,
        ));
    }
    polygon.push(polygon[0]); // close ring

    Isochrone {
        center: (lat, lng),
        mode: mode.to_string(),
        minutes,
        radius_m: radius,
        polygon,
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// P4-4: Crowd Density Estimation
// ═══════════════════════════════════════════════════════════════════════════

#[derive(Debug, Serialize, Deserialize)]
pub struct CrowdEstimate {
    pub estimated_count: u64,
    pub confidence_low: u64,
    pub confidence_high: u64,
    pub density_per_sqm: f64,
    pub model: String,
}

pub fn estimate_crowd(area_sqm: f64, density_factor: f64) -> CrowdEstimate {
    // In production: pass image through CSRNet ONNX model via inference engine
    let density = if density_factor > 0.0 { density_factor } else { 2.0 };
    let count = (area_sqm * density) as u64;
    let margin = (count as f64 * 0.15) as u64;

    CrowdEstimate {
        estimated_count: count,
        confidence_low: count.saturating_sub(margin),
        confidence_high: count + margin,
        density_per_sqm: (density * 10.0).round() / 10.0,
        model: "density-area-v1".to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_haversine() {
        // Lagos to Ikeja ~16km
        let d = haversine_m(6.4541, 3.3947, 6.6018, 3.3515);
        assert!(d > 15_000.0 && d < 20_000.0, "distance={}", d);
    }

    #[test]
    fn test_haversine_same_point() {
        let d = haversine_m(6.45, 3.39, 6.45, 3.39);
        assert!(d < 0.01);
    }

    #[test]
    fn test_optimize_route_empty() {
        let result = optimize_route(6.45, 3.39, vec![]);
        assert_eq!(result.stops.len(), 0);
        assert_eq!(result.total_distance_m, 0.0);
    }

    #[test]
    fn test_optimize_route_single() {
        let stops = vec![RouteStop {
            id: "a".into(), lat: 6.46, lng: 3.40, name: "A".into(), priority: 1,
        }];
        let result = optimize_route(6.45, 3.39, stops);
        assert_eq!(result.stops.len(), 1);
        assert!(result.total_distance_m > 0.0);
    }

    #[test]
    fn test_optimize_route_multi() {
        let stops = vec![
            RouteStop { id: "a".into(), lat: 6.45, lng: 3.40, name: "A".into(), priority: 1 },
            RouteStop { id: "b".into(), lat: 6.50, lng: 3.45, name: "B".into(), priority: 2 },
            RouteStop { id: "c".into(), lat: 6.46, lng: 3.41, name: "C".into(), priority: 1 },
            RouteStop { id: "d".into(), lat: 6.55, lng: 3.50, name: "D".into(), priority: 3 },
        ];
        let result = optimize_route(6.44, 3.39, stops);
        assert_eq!(result.stops.len(), 4);
        assert!(result.total_distance_m > 0.0);
        // First stop should be nearest to start
        assert_eq!(result.stops[0].id, "a");
    }

    #[test]
    fn test_merkle_root_empty() {
        assert_eq!(compute_merkle_root(&[]), "");
    }

    #[test]
    fn test_merkle_root_single() {
        let root = compute_merkle_root(&["pledge-1".to_string()]);
        assert!(!root.is_empty());
        assert_eq!(root.len(), 64); // SHA-256 hex
    }

    #[test]
    fn test_merkle_root_deterministic() {
        let items = vec!["a".to_string(), "b".to_string(), "c".to_string()];
        let root1 = compute_merkle_root(&items);
        let root2 = compute_merkle_root(&items);
        assert_eq!(root1, root2);
    }

    #[test]
    fn test_rate_limiter() {
        let limiter = SlidingWindowLimiter::new(3, 60);
        assert!(limiter.allow("ip1"));
        assert!(limiter.allow("ip1"));
        assert!(limiter.allow("ip1"));
        assert!(!limiter.allow("ip1")); // 4th request blocked
        assert!(limiter.allow("ip2")); // different key ok
    }

    #[test]
    fn test_isochrone() {
        let iso = compute_isochrone(6.45, 3.39, "walking", 15);
        assert_eq!(iso.polygon.len(), 17); // 16 + 1 to close
        assert_eq!(iso.radius_m, 1200.0); // 80m/min * 15min
    }

    #[test]
    fn test_crowd_estimate() {
        let est = estimate_crowd(5000.0, 2.0);
        assert_eq!(est.estimated_count, 10_000);
        assert_eq!(est.density_per_sqm, 2.0);
        assert!(est.confidence_low < est.estimated_count);
        assert!(est.confidence_high > est.estimated_count);
    }
}
