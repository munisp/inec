//! Spatial analysis algorithms for the INEC GeoLibre integration.
//!
//! All functions accept GeoJSON input and return GeoJSON output,
//! making them directly consumable by the GeoLibre viewer.

use actix_web::HttpResponse;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

// ─── Common types ───────────────────────────────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PointInput {
    pub longitude: f64,
    pub latitude: f64,
    pub properties: Option<HashMap<String, serde_json::Value>>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct FeatureCollection {
    #[serde(rename = "type")]
    pub fc_type: String,
    pub features: Vec<Feature>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub metadata: Option<HashMap<String, serde_json::Value>>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct Feature {
    #[serde(rename = "type")]
    pub feat_type: String,
    pub geometry: Geometry,
    pub properties: HashMap<String, serde_json::Value>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct Geometry {
    #[serde(rename = "type")]
    pub geom_type: String,
    pub coordinates: serde_json::Value,
}

impl FeatureCollection {
    fn new() -> Self {
        FeatureCollection {
            fc_type: "FeatureCollection".into(),
            features: Vec::new(),
            metadata: None,
        }
    }

    fn with_metadata(mut self, key: &str, value: serde_json::Value) -> Self {
        self.metadata
            .get_or_insert_with(HashMap::new)
            .insert(key.into(), value);
        self
    }
}

// ─── Buffer Analysis ────────────────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct BufferRequest {
    pub points: Vec<PointInput>,
    pub radius_km: f64,
    #[serde(default = "default_segments")]
    pub segments: usize,
}

fn default_segments() -> usize { 32 }

pub async fn buffer_analysis(body: actix_web::web::Json<BufferRequest>) -> HttpResponse {
    let req = body.into_inner();
    let radius_deg = req.radius_km / 111.32; // approximate km to degrees
    let mut fc = FeatureCollection::new();

    for (i, pt) in req.points.iter().enumerate() {
        // Generate circular polygon approximation
        let mut ring: Vec<[f64; 2]> = Vec::with_capacity(req.segments + 1);
        for j in 0..=req.segments {
            let angle = 2.0 * std::f64::consts::PI * (j as f64) / (req.segments as f64);
            let dx = radius_deg * angle.cos();
            let dy = radius_deg * angle.sin() / (pt.latitude.to_radians().cos()).max(0.01);
            ring.push([pt.longitude + dy, pt.latitude + dx]);
        }

        let mut props: HashMap<String, serde_json::Value> = pt.properties.clone().unwrap_or_default();
        props.insert("buffer_index".into(), serde_json::json!(i));
        props.insert("radius_km".into(), serde_json::json!(req.radius_km));
        props.insert("center".into(), serde_json::json!([pt.longitude, pt.latitude]));

        fc.features.push(Feature {
            feat_type: "Feature".into(),
            geometry: Geometry {
                geom_type: "Polygon".into(),
                coordinates: serde_json::json!([ring]),
            },
            properties: props,
        });
    }

    let fc = fc
        .with_metadata("analysis", serde_json::json!("buffer"))
        .with_metadata("radius_km", serde_json::json!(req.radius_km))
        .with_metadata("feature_count", serde_json::json!(fc.features.len()));

    HttpResponse::Ok()
        .content_type("application/geo+json")
        .json(fc)
}

// ─── Voronoi Tessellation ───────────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct VoronoiRequest {
    pub points: Vec<PointInput>,
    #[serde(default = "default_bbox")]
    pub bbox: [f64; 4], // [min_lng, min_lat, max_lng, max_lat]
}

fn default_bbox() -> [f64; 4] { [2.5, 4.0, 14.7, 14.0] } // Nigeria bounds

pub async fn voronoi_analysis(body: actix_web::web::Json<VoronoiRequest>) -> HttpResponse {
    let req = body.into_inner();
    let mut fc = FeatureCollection::new();

    // For each point, compute its Voronoi cell using half-plane intersection
    // Simplified: assign each point a bounding box clipped region
    for (i, pt) in req.points.iter().enumerate() {
        let mut cell_bounds = req.bbox;

        // Clip against all other points
        for (j, other) in req.points.iter().enumerate() {
            if i == j { continue; }

            let mid_lng = (pt.longitude + other.longitude) / 2.0;
            let mid_lat = (pt.latitude + other.latitude) / 2.0;
            let dx = other.longitude - pt.longitude;
            let dy = other.latitude - pt.latitude;

            // Half-plane: keep the side closer to pt
            if dx.abs() > dy.abs() {
                if dx > 0.0 {
                    cell_bounds[2] = cell_bounds[2].min(mid_lng);
                } else {
                    cell_bounds[0] = cell_bounds[0].max(mid_lng);
                }
            } else {
                if dy > 0.0 {
                    cell_bounds[3] = cell_bounds[3].min(mid_lat);
                } else {
                    cell_bounds[1] = cell_bounds[1].max(mid_lat);
                }
            }
        }

        if cell_bounds[0] >= cell_bounds[2] || cell_bounds[1] >= cell_bounds[3] {
            continue;
        }

        let ring = vec![
            [cell_bounds[0], cell_bounds[1]],
            [cell_bounds[2], cell_bounds[1]],
            [cell_bounds[2], cell_bounds[3]],
            [cell_bounds[0], cell_bounds[3]],
            [cell_bounds[0], cell_bounds[1]],
        ];

        let mut props: HashMap<String, serde_json::Value> = pt.properties.clone().unwrap_or_default();
        props.insert("voronoi_index".into(), serde_json::json!(i));
        props.insert("center".into(), serde_json::json!([pt.longitude, pt.latitude]));
        let area = (cell_bounds[2] - cell_bounds[0]) * (cell_bounds[3] - cell_bounds[1]) * 111.32 * 111.32;
        props.insert("area_km2".into(), serde_json::json!((area * 100.0).round() / 100.0));

        fc.features.push(Feature {
            feat_type: "Feature".into(),
            geometry: Geometry {
                geom_type: "Polygon".into(),
                coordinates: serde_json::json!([ring]),
            },
            properties: props,
        });
    }

    let count = fc.features.len();
    let fc = fc
        .with_metadata("analysis", serde_json::json!("voronoi"))
        .with_metadata("feature_count", serde_json::json!(count));

    HttpResponse::Ok()
        .content_type("application/geo+json")
        .json(fc)
}

// ─── H3 Hexagonal Aggregation ───────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct H3Request {
    pub points: Vec<PointInput>,
    #[serde(default = "default_resolution")]
    pub resolution: u8,
    pub aggregate_field: Option<String>,
}

fn default_resolution() -> u8 { 5 }

pub async fn h3_aggregation(body: actix_web::web::Json<H3Request>) -> HttpResponse {
    let req = body.into_inner();
    let mut hex_map: HashMap<String, Vec<&PointInput>> = HashMap::new();

    // Group points by H3 cell
    for pt in &req.points {
        // Use resolution to create cell key (simplified H3-like grid)
        let cell_size = 1.0 / (2.0_f64.powi(req.resolution as i32));
        let col = (pt.longitude / cell_size).floor() as i64;
        let row = (pt.latitude / cell_size).floor() as i64;
        let key = format!("{}_{}_r{}", col, row, req.resolution);

        hex_map.entry(key).or_default().push(pt);
    }

    let mut fc = FeatureCollection::new();

    for (hex_key, points) in &hex_map {
        let avg_lng: f64 = points.iter().map(|p| p.longitude).sum::<f64>() / points.len() as f64;
        let avg_lat: f64 = points.iter().map(|p| p.latitude).sum::<f64>() / points.len() as f64;

        // Generate hexagon around centroid
        let hex_radius = 0.5 / (2.0_f64.powi(req.resolution as i32));
        let mut ring: Vec<[f64; 2]> = Vec::with_capacity(7);
        for k in 0..=6 {
            let angle = std::f64::consts::PI / 3.0 * (k as f64) + std::f64::consts::PI / 6.0;
            ring.push([
                avg_lng + hex_radius * angle.cos(),
                avg_lat + hex_radius * angle.sin(),
            ]);
        }

        let mut props = HashMap::new();
        props.insert("h3_index".into(), serde_json::json!(hex_key));
        props.insert("point_count".into(), serde_json::json!(points.len()));
        props.insert("resolution".into(), serde_json::json!(req.resolution));
        props.insert("centroid".into(), serde_json::json!([avg_lng, avg_lat]));

        // Aggregate numeric field if specified
        if let Some(ref field) = req.aggregate_field {
            let sum: f64 = points.iter()
                .filter_map(|p| p.properties.as_ref()?.get(field)?.as_f64())
                .sum();
            let avg = if !points.is_empty() { sum / points.len() as f64 } else { 0.0 };
            props.insert(format!("{}_sum", field), serde_json::json!(sum));
            props.insert(format!("{}_avg", field), serde_json::json!((avg * 100.0).round() / 100.0));
        }

        fc.features.push(Feature {
            feat_type: "Feature".into(),
            geometry: Geometry {
                geom_type: "Polygon".into(),
                coordinates: serde_json::json!([ring]),
            },
            properties: props,
        });
    }

    let count = fc.features.len();
    let fc = fc
        .with_metadata("analysis", serde_json::json!("h3_aggregation"))
        .with_metadata("resolution", serde_json::json!(req.resolution))
        .with_metadata("hex_count", serde_json::json!(count))
        .with_metadata("point_count", serde_json::json!(req.points.len()));

    HttpResponse::Ok()
        .content_type("application/geo+json")
        .json(fc)
}

// ─── DBSCAN Spatial Clustering ──────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct ClusterRequest {
    pub points: Vec<PointInput>,
    #[serde(default = "default_eps")]
    pub eps_km: f64,
    #[serde(default = "default_min_pts")]
    pub min_points: usize,
}

fn default_eps() -> f64 { 5.0 }
fn default_min_pts() -> usize { 3 }

fn haversine_km(lat1: f64, lon1: f64, lat2: f64, lon2: f64) -> f64 {
    let r = 6371.0;
    let dlat = (lat2 - lat1).to_radians();
    let dlon = (lon2 - lon1).to_radians();
    let a = (dlat / 2.0).sin().powi(2)
        + lat1.to_radians().cos() * lat2.to_radians().cos() * (dlon / 2.0).sin().powi(2);
    r * 2.0 * a.sqrt().asin()
}

pub async fn dbscan_cluster(body: actix_web::web::Json<ClusterRequest>) -> HttpResponse {
    let req = body.into_inner();
    let n = req.points.len();
    let mut labels: Vec<i32> = vec![-1; n]; // -1 = noise
    let mut cluster_id: i32 = 0;

    for i in 0..n {
        if labels[i] != -1 { continue; }

        // Find neighbors
        let neighbors: Vec<usize> = (0..n)
            .filter(|&j| {
                haversine_km(
                    req.points[i].latitude, req.points[i].longitude,
                    req.points[j].latitude, req.points[j].longitude,
                ) <= req.eps_km
            })
            .collect();

        if neighbors.len() < req.min_points { continue; }

        labels[i] = cluster_id;
        let mut queue = neighbors.clone();
        let mut qi = 0;

        while qi < queue.len() {
            let j = queue[qi];
            qi += 1;

            if labels[j] == -1 {
                labels[j] = cluster_id;
            }
            if labels[j] != -1 && labels[j] != cluster_id {
                continue;
            }
            labels[j] = cluster_id;

            let j_neighbors: Vec<usize> = (0..n)
                .filter(|&k| {
                    haversine_km(
                        req.points[j].latitude, req.points[j].longitude,
                        req.points[k].latitude, req.points[k].longitude,
                    ) <= req.eps_km
                })
                .collect();

            if j_neighbors.len() >= req.min_points {
                for nj in j_neighbors {
                    if labels[nj] == -1 {
                        queue.push(nj);
                    }
                }
            }
        }

        cluster_id += 1;
    }

    let mut fc = FeatureCollection::new();
    for (i, pt) in req.points.iter().enumerate() {
        let mut props: HashMap<String, serde_json::Value> = pt.properties.clone().unwrap_or_default();
        props.insert("cluster_id".into(), serde_json::json!(labels[i]));
        props.insert("is_noise".into(), serde_json::json!(labels[i] == -1));

        fc.features.push(Feature {
            feat_type: "Feature".into(),
            geometry: Geometry {
                geom_type: "Point".into(),
                coordinates: serde_json::json!([pt.longitude, pt.latitude]),
            },
            properties: props,
        });
    }

    let noise_count = labels.iter().filter(|&&l| l == -1).count();
    let count = fc.features.len();
    let fc = fc
        .with_metadata("analysis", serde_json::json!("dbscan_clustering"))
        .with_metadata("cluster_count", serde_json::json!(cluster_id))
        .with_metadata("noise_count", serde_json::json!(noise_count))
        .with_metadata("feature_count", serde_json::json!(count));

    HttpResponse::Ok()
        .content_type("application/geo+json")
        .json(fc)
}

// ─── Kernel Density Estimation ──────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct DensityRequest {
    pub points: Vec<PointInput>,
    #[serde(default = "default_bandwidth")]
    pub bandwidth_km: f64,
    #[serde(default = "default_grid_size")]
    pub grid_size: usize,
    pub bbox: Option<[f64; 4]>,
}

fn default_bandwidth() -> f64 { 10.0 }
fn default_grid_size() -> usize { 20 }

pub async fn kernel_density(body: actix_web::web::Json<DensityRequest>) -> HttpResponse {
    let req = body.into_inner();

    let bbox = req.bbox.unwrap_or_else(|| {
        if req.points.is_empty() { return [2.5, 4.0, 14.7, 14.0]; }
        let mut min_lng = f64::MAX; let mut max_lng = f64::MIN;
        let mut min_lat = f64::MAX; let mut max_lat = f64::MIN;
        for p in &req.points {
            min_lng = min_lng.min(p.longitude); max_lng = max_lng.max(p.longitude);
            min_lat = min_lat.min(p.latitude);  max_lat = max_lat.max(p.latitude);
        }
        let pad = req.bandwidth_km / 111.32;
        [min_lng - pad, min_lat - pad, max_lng + pad, max_lat + pad]
    });

    let step_lng = (bbox[2] - bbox[0]) / req.grid_size as f64;
    let step_lat = (bbox[3] - bbox[1]) / req.grid_size as f64;
    let bandwidth_deg = req.bandwidth_km / 111.32;

    let mut fc = FeatureCollection::new();

    for row in 0..req.grid_size {
        for col in 0..req.grid_size {
            let cell_lng = bbox[0] + (col as f64 + 0.5) * step_lng;
            let cell_lat = bbox[1] + (row as f64 + 0.5) * step_lat;

            // Gaussian kernel density
            let density: f64 = req.points.iter()
                .map(|p| {
                    let dx = (p.longitude - cell_lng) / bandwidth_deg;
                    let dy = (p.latitude - cell_lat) / bandwidth_deg;
                    let u2 = dx * dx + dy * dy;
                    (-0.5 * u2).exp() / (2.0 * std::f64::consts::PI)
                })
                .sum();

            if density < 0.001 { continue; }

            let ring = vec![
                [cell_lng - step_lng / 2.0, cell_lat - step_lat / 2.0],
                [cell_lng + step_lng / 2.0, cell_lat - step_lat / 2.0],
                [cell_lng + step_lng / 2.0, cell_lat + step_lat / 2.0],
                [cell_lng - step_lng / 2.0, cell_lat + step_lat / 2.0],
                [cell_lng - step_lng / 2.0, cell_lat - step_lat / 2.0],
            ];

            let mut props = HashMap::new();
            props.insert("density".into(), serde_json::json!((density * 10000.0).round() / 10000.0));
            props.insert("grid_row".into(), serde_json::json!(row));
            props.insert("grid_col".into(), serde_json::json!(col));

            fc.features.push(Feature {
                feat_type: "Feature".into(),
                geometry: Geometry {
                    geom_type: "Polygon".into(),
                    coordinates: serde_json::json!([ring]),
                },
                properties: props,
            });
        }
    }

    let count = fc.features.len();
    let fc = fc
        .with_metadata("analysis", serde_json::json!("kernel_density"))
        .with_metadata("bandwidth_km", serde_json::json!(req.bandwidth_km))
        .with_metadata("grid_size", serde_json::json!(req.grid_size))
        .with_metadata("cell_count", serde_json::json!(count));

    HttpResponse::Ok()
        .content_type("application/geo+json")
        .json(fc)
}

// ─── K-Nearest Neighbors ────────────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct NearestRequest {
    pub query_point: PointInput,
    pub points: Vec<PointInput>,
    #[serde(default = "default_k")]
    pub k: usize,
}

fn default_k() -> usize { 10 }

pub async fn nearest_neighbors(body: actix_web::web::Json<NearestRequest>) -> HttpResponse {
    let req = body.into_inner();
    let q = &req.query_point;

    let mut distances: Vec<(usize, f64)> = req.points.iter().enumerate()
        .map(|(i, p)| {
            let dist = haversine_km(q.latitude, q.longitude, p.latitude, p.longitude);
            (i, dist)
        })
        .collect();

    distances.sort_by(|a, b| a.1.partial_cmp(&b.1).unwrap());
    distances.truncate(req.k);

    let mut fc = FeatureCollection::new();

    for (rank, (i, dist)) in distances.iter().enumerate() {
        let pt = &req.points[*i];
        let mut props: HashMap<String, serde_json::Value> = pt.properties.clone().unwrap_or_default();
        props.insert("rank".into(), serde_json::json!(rank + 1));
        props.insert("distance_km".into(), serde_json::json!((*dist * 1000.0).round() / 1000.0));

        fc.features.push(Feature {
            feat_type: "Feature".into(),
            geometry: Geometry {
                geom_type: "Point".into(),
                coordinates: serde_json::json!([pt.longitude, pt.latitude]),
            },
            properties: props,
        });
    }

    let count = fc.features.len();
    let fc = fc
        .with_metadata("analysis", serde_json::json!("nearest_neighbors"))
        .with_metadata("k", serde_json::json!(req.k))
        .with_metadata("query_point", serde_json::json!([q.longitude, q.latitude]))
        .with_metadata("feature_count", serde_json::json!(count));

    HttpResponse::Ok()
        .content_type("application/geo+json")
        .json(fc)
}

// ─── Convex Hull ────────────────────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct HullRequest {
    pub points: Vec<PointInput>,
}

pub async fn convex_hull(body: actix_web::web::Json<HullRequest>) -> HttpResponse {
    let req = body.into_inner();

    if req.points.len() < 3 {
        return HttpResponse::BadRequest().json(serde_json::json!({
            "error": "Need at least 3 points for convex hull"
        }));
    }

    // Graham scan
    let mut pts: Vec<(f64, f64)> = req.points.iter()
        .map(|p| (p.longitude, p.latitude))
        .collect();

    // Find bottom-most point
    let start = pts.iter().enumerate()
        .min_by(|a, b| a.1.1.partial_cmp(&b.1.1).unwrap().then(a.1.0.partial_cmp(&b.1.0).unwrap()))
        .map(|(i, _)| i)
        .unwrap_or(0);
    pts.swap(0, start);

    let pivot = pts[0];
    pts[1..].sort_by(|a, b| {
        let angle_a = (a.1 - pivot.1).atan2(a.0 - pivot.0);
        let angle_b = (b.1 - pivot.1).atan2(b.0 - pivot.0);
        angle_a.partial_cmp(&angle_b).unwrap()
    });

    let mut hull: Vec<(f64, f64)> = Vec::new();
    for pt in &pts {
        while hull.len() >= 2 {
            let a = hull[hull.len() - 2];
            let b = hull[hull.len() - 1];
            let cross = (b.0 - a.0) * (pt.1 - a.1) - (b.1 - a.1) * (pt.0 - a.0);
            if cross <= 0.0 { hull.pop(); } else { break; }
        }
        hull.push(*pt);
    }

    // Close the ring
    if !hull.is_empty() {
        hull.push(hull[0]);
    }

    let ring: Vec<[f64; 2]> = hull.iter().map(|p| [p.0, p.1]).collect();

    let mut fc = FeatureCollection::new();
    let mut props = HashMap::new();
    props.insert("point_count".into(), serde_json::json!(req.points.len()));
    props.insert("hull_vertices".into(), serde_json::json!(ring.len() - 1));

    fc.features.push(Feature {
        feat_type: "Feature".into(),
        geometry: Geometry {
            geom_type: "Polygon".into(),
            coordinates: serde_json::json!([ring]),
        },
        properties: props,
    });

    let fc = fc
        .with_metadata("analysis", serde_json::json!("convex_hull"))
        .with_metadata("point_count", serde_json::json!(req.points.len()));

    HttpResponse::Ok()
        .content_type("application/geo+json")
        .json(fc)
}

// ─── Centroid Analysis ──────────────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct CentroidRequest {
    pub groups: Vec<PointGroup>,
}

#[derive(Debug, Deserialize)]
pub struct PointGroup {
    pub name: String,
    pub points: Vec<PointInput>,
}

pub async fn centroid_analysis(body: actix_web::web::Json<CentroidRequest>) -> HttpResponse {
    let req = body.into_inner();
    let mut fc = FeatureCollection::new();

    for group in &req.groups {
        if group.points.is_empty() { continue; }

        let n = group.points.len() as f64;
        let avg_lng: f64 = group.points.iter().map(|p| p.longitude).sum::<f64>() / n;
        let avg_lat: f64 = group.points.iter().map(|p| p.latitude).sum::<f64>() / n;

        // Standard distance
        let std_dist: f64 = (group.points.iter()
            .map(|p| {
                let dx = (p.longitude - avg_lng) * 111.32;
                let dy = (p.latitude - avg_lat) * 111.32;
                dx * dx + dy * dy
            })
            .sum::<f64>() / n)
            .sqrt();

        let mut props = HashMap::new();
        props.insert("group_name".into(), serde_json::json!(group.name));
        props.insert("point_count".into(), serde_json::json!(group.points.len()));
        props.insert("centroid_lng".into(), serde_json::json!((avg_lng * 10000.0).round() / 10000.0));
        props.insert("centroid_lat".into(), serde_json::json!((avg_lat * 10000.0).round() / 10000.0));
        props.insert("standard_distance_km".into(), serde_json::json!((std_dist * 100.0).round() / 100.0));

        fc.features.push(Feature {
            feat_type: "Feature".into(),
            geometry: Geometry {
                geom_type: "Point".into(),
                coordinates: serde_json::json!([avg_lng, avg_lat]),
            },
            properties: props,
        });
    }

    let count = fc.features.len();
    let fc = fc
        .with_metadata("analysis", serde_json::json!("centroid"))
        .with_metadata("group_count", serde_json::json!(count));

    HttpResponse::Ok()
        .content_type("application/geo+json")
        .json(fc)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_haversine_km() {
        // Lagos to Abuja: approximately 450 km
        let dist = haversine_km(6.45, 3.40, 9.06, 7.49);
        assert!(dist > 400.0 && dist < 500.0, "Lagos-Abuja should be ~450km, got {}", dist);
    }

    #[test]
    fn test_haversine_same_point() {
        let dist = haversine_km(9.06, 7.49, 9.06, 7.49);
        assert!(dist < 0.001, "Same point should be ~0km, got {}", dist);
    }
}
