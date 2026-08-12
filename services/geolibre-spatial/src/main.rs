//! INEC GeoLibre Spatial Analysis Engine
//!
//! Rust-based spatial computation service that provides high-performance
//! geospatial analysis for the INEC election platform's GeoLibre integration.
//!
//! Endpoints:
//! - POST /spatial/buffer      — Buffer analysis around points
//! - POST /spatial/voronoi     — Voronoi tessellation of polling units
//! - POST /spatial/h3          — H3 hexagonal aggregation
//! - POST /spatial/cluster     — DBSCAN spatial clustering
//! - POST /spatial/density     — Kernel density estimation
//! - POST /spatial/nearest     — K-nearest neighbors
//! - GET  /health              — Health check

use actix_cors::Cors;
use actix_web::{
    body::BoxBody,
    dev::{ServiceRequest, ServiceResponse},
    middleware::{self, Next},
    web, App, Error, HttpResponse, HttpServer,
};
use serde::{Deserialize, Serialize};

mod spatial;

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

/// API-key authentication for all spatial endpoints. /health stays public
/// for orchestrator probes. Fail-closed: when GEOLIBRE_API_KEY is unset the
/// service returns 503 rather than serving spatial analysis unauthenticated.
/// GEOLIBRE_API_KEY accepts a comma-separated key list so rotation is
/// possible without downtime; the presented key is compared against each
/// configured key in constant time.
async fn api_key_auth(
    req: ServiceRequest,
    next: Next<BoxBody>,
) -> Result<ServiceResponse<BoxBody>, Error> {
    if req.path() == "/health" {
        return next.call(req).await.map(ServiceResponse::map_into_boxed_body);
    }

    let expected_keys = configured_api_keys("GEOLIBRE_API_KEY");
    if expected_keys.is_empty() {
        return Ok(req.into_response(
            HttpResponse::ServiceUnavailable()
                .json(serde_json::json!({
                    "error": "GEOLIBRE_API_KEY not configured; refusing to serve unauthenticated requests"
                }))
                .map_into_boxed_body(),
        ));
    }

    let authorized = req
        .headers()
        .get("x-api-key")
        .and_then(|v| v.to_str().ok())
        .map(|v| api_key_matches(v, &expected_keys))
        .unwrap_or(false);

    if !authorized {
        return Ok(req.into_response(
            HttpResponse::Unauthorized()
                .json(serde_json::json!({ "error": "missing or invalid x-api-key" }))
                .map_into_boxed_body(),
        ));
    }

    next.call(req).await.map(ServiceResponse::map_into_boxed_body)
}

#[derive(Debug, Serialize, Deserialize)]
struct HealthResponse {
    status: String,
    service: String,
    version: String,
}

async fn health() -> HttpResponse {
    HttpResponse::Ok().json(HealthResponse {
        status: "healthy".into(),
        service: "inec-geolibre-spatial".into(),
        version: "1.0.0".into(),
    })
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    eprintln!("[geolibre-spatial] Starting on :8770");

    HttpServer::new(|| {
        // CORS policy driven by CORS_ORIGINS (comma-separated). Default deny:
        // when unset, no cross-origin requests are permitted (replaces the
        // previous allow-any policy, which let any website call the spatial
        // API from a browser).
        let origins_str = std::env::var("CORS_ORIGINS").unwrap_or_default();
        let mut cors = Cors::default()
            .allowed_methods(vec!["GET", "POST"])
            .allowed_headers(vec![
                actix_web::http::header::CONTENT_TYPE,
                actix_web::http::header::AUTHORIZATION,
                actix_web::http::header::HeaderName::from_static("x-api-key"),
            ])
            .max_age(3600);
        for origin in origins_str
            .split(',')
            .map(str::trim)
            .filter(|s| !s.is_empty())
        {
            cors = cors.allowed_origin(origin);
        }

        App::new()
            // Registered first = innermost: auth wraps the router directly so
            // CORS preflight (OPTIONS) is handled before authentication.
            .wrap(middleware::from_fn(api_key_auth))
            .wrap(cors)
            .wrap(middleware::Logger::default())
            .route("/health", web::get().to(health))
            .service(
                web::scope("/spatial")
                    .route("/buffer", web::post().to(spatial::buffer_analysis))
                    .route("/voronoi", web::post().to(spatial::voronoi_analysis))
                    .route("/h3", web::post().to(spatial::h3_aggregation))
                    .route("/cluster", web::post().to(spatial::dbscan_cluster))
                    .route("/density", web::post().to(spatial::kernel_density))
                    .route("/nearest", web::post().to(spatial::nearest_neighbors))
                    .route("/convex-hull", web::post().to(spatial::convex_hull))
                    .route("/centroid", web::post().to(spatial::centroid_analysis))
            )
    })
    .bind("0.0.0.0:8770")?
    .run()
    .await
}
