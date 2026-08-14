//! INEC Hot Path — Ultra-low-latency transaction processor in Rust.
//!
//! Handles millions of TPS through:
//! - Lock-free MPMC queues (crossbeam)
//! - Zero-copy Kafka consumers (rdkafka + zero-copy deserialization)
//! - Redis cluster pipelining (async batched commands)
//! - Arrow columnar batching for analytical writes
//! - DashMap for concurrent state (no mutex contention)

mod fluvio_smart;
mod kafka_consumer;
mod metrics;
mod opensearch;
mod pipeline;
mod redis_cluster;
mod tigerbeetle;
mod wal;

use axum::{routing::get, Json, Router};
use std::sync::Arc;
use tokio::signal;
use tracing_subscriber::EnvFilter;

#[tokio::main(flavor = "multi_thread", worker_threads = 16)]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .init();

    let config = Arc::new(pipeline::Config::from_env());
    let engine = Arc::new(pipeline::Engine::new(config.clone()).await?);

    // Start all pipeline workers
    let engine_clone = engine.clone();
    tokio::spawn(async move {
        if let Err(e) = engine_clone.run().await {
            // SECURITY: fail loudly — never run a half-simulated pipeline.
            tracing::error!("hot-path pipeline failed: {e:#}; exiting");
            std::process::exit(1);
        }
    });

    // HTTP server for health/metrics
    let app = Router::new()
        // /health is liveness only: the process is up. It deliberately says
        // nothing about sink connectivity — that is /readyz's job.
        .route("/health", get(health))
        .route(
            "/readyz",
            get({
                let e = engine.clone();
                move || readyz_handler(e.clone())
            }),
        )
        .route(
            "/metrics",
            get({
                let e = engine.clone();
                move || metrics_handler(e.clone())
            }),
        )
        .route(
            "/stats",
            get({
                let e = engine.clone();
                move || stats_handler(e.clone())
            }),
        );

    let addr = format!("0.0.0.0:{}", config.port);
    tracing::info!("hot-path engine listening on {}", addr);

    let listener = tokio::net::TcpListener::bind(&addr).await?;
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await?;

    Ok(())
}

async fn health() -> &'static str {
    "OK"
}

/// Readiness: 200 only when every sink is reachable (i.e. none is always-Err).
async fn readyz_handler(engine: Arc<pipeline::Engine>) -> axum::response::Response {
    use axum::response::IntoResponse;
    let (ready, body) = engine.readiness();
    let status = if ready {
        axum::http::StatusCode::OK
    } else {
        axum::http::StatusCode::SERVICE_UNAVAILABLE
    };
    (status, Json(body)).into_response()
}

async fn metrics_handler(engine: Arc<pipeline::Engine>) -> String {
    engine.prometheus_metrics()
}

async fn stats_handler(engine: Arc<pipeline::Engine>) -> Json<serde_json::Value> {
    Json(engine.stats())
}

async fn shutdown_signal() {
    // Kubernetes sends SIGTERM first; handle both SIGTERM and SIGINT.
    let ctrl_c = signal::ctrl_c();
    let mut sigterm = signal::unix::signal(signal::unix::SignalKind::terminate())
        .expect("failed to register SIGTERM handler");
    tokio::select! {
        _ = ctrl_c => tracing::info!("received SIGINT, shutting down hot-path engine"),
        _ = sigterm.recv() => tracing::info!("received SIGTERM, shutting down hot-path engine"),
    }
}
