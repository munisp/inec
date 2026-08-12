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
use std::collections::HashMap;

mod spatial;

/// API-key authentication for all spatial endpoints. /health stays public
/// for orchestrator probes. Fail-closed: when GEOLIBRE_API_KEY is unset the
/// service returns 503 rather than serving spatial analysis unauthenticated.
async fn api_key_auth(
    req: ServiceRequest,
    next: Next<BoxBody>,
) -> Result<ServiceResponse<BoxBody>, Error> {
    if req.path() == "/health" {
        return next.call(req).await.map(ServiceResponse::map_into_boxed_body);
    }

    let expected_key = std::env::var("GEOLIBRE_API_KEY").unwrap_or_default();
    if expected_key.is_empty() {
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
        .map(|v| v == expected_key)
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
        let cors = Cors::default()
            .allow_any_origin()
            .allow_any_method()
            .allow_any_header();

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
