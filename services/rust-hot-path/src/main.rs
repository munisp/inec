//! INEC Hot Path — Ultra-low-latency transaction processor in Rust.
//!
//! Handles millions of TPS through:
//! - Lock-free MPMC queues (crossbeam)
//! - Zero-copy Kafka consumers (rdkafka + zero-copy deserialization)
//! - Redis cluster pipelining (async batched commands)
//! - Arrow columnar batching for analytical writes
//! - DashMap for concurrent state (no mutex contention)

mod kafka_consumer;
mod redis_cluster;
mod tigerbeetle;
mod opensearch;
mod fluvio_smart;
mod pipeline;
mod metrics;

use std::sync::Arc;
use axum::{Router, routing::get, Json};
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
        engine_clone.run().await;
    });

    // HTTP server for health/metrics
    let app = Router::new()
        .route("/health", get(health))
        .route("/metrics", get({
            let e = engine.clone();
            move || metrics_handler(e.clone())
        }))
        .route("/stats", get({
            let e = engine.clone();
            move || stats_handler(e.clone())
        }));

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

async fn metrics_handler(engine: Arc<pipeline::Engine>) -> String {
    engine.prometheus_metrics()
}

async fn stats_handler(engine: Arc<pipeline::Engine>) -> Json<serde_json::Value> {
    Json(engine.stats())
}

async fn shutdown_signal() {
    signal::ctrl_c().await.expect("failed to listen for ctrl_c");
    tracing::info!("shutting down hot-path engine");
}
