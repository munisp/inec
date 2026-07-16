//! GOTV Engine Middleware Integration — connects the Rust spatial engine
//! to Kafka, Redis, OpenSearch, Dapr, Fluvio, and Lakehouse via HTTP APIs.
//!
//! Each middleware is optional: if the env var is not set, calls are no-ops.

use reqwest::Client;
use serde::Serialize;
use std::sync::Arc;
use tracing::{info, warn};

/// Holds connections to all middleware services.
pub struct Middleware {
    http: Client,
    pub kafka_url: Option<String>,
    pub redis_url: Option<String>,
    pub opensearch_url: Option<String>,
    pub dapr_port: Option<String>,
    pub fluvio_url: Option<String>,
    pub temporal_url: Option<String>,
    pub lakehouse_url: Option<String>,
}

impl Middleware {
    pub fn new() -> Self {
        let kafka_url = std::env::var("KAFKA_REST_URL").ok();
        let redis_url = std::env::var("REDIS_HTTP_URL").ok();
        let opensearch_url = std::env::var("OPENSEARCH_URL").ok();
        let dapr_port = std::env::var("DAPR_HTTP_PORT").ok();
        let fluvio_url = std::env::var("FLUVIO_URL").ok();
        let temporal_url = std::env::var("TEMPORAL_FRONTEND_URL").ok();
        let lakehouse_url = std::env::var("LAKEHOUSE_URL").ok();

        let connected: Vec<&str> = [
            kafka_url.as_ref().map(|_| "kafka"),
            redis_url.as_ref().map(|_| "redis"),
            opensearch_url.as_ref().map(|_| "opensearch"),
            dapr_port.as_ref().map(|_| "dapr"),
            fluvio_url.as_ref().map(|_| "fluvio"),
            temporal_url.as_ref().map(|_| "temporal"),
            lakehouse_url.as_ref().map(|_| "lakehouse"),
        ]
        .iter()
        .filter_map(|x| *x)
        .collect();

        info!(
            connected = ?connected,
            total = connected.len(),
            "GOTV Engine middleware initialized"
        );

        Self {
            http: Client::builder()
                .timeout(std::time::Duration::from_secs(10))
                .connect_timeout(std::time::Duration::from_secs(5))
                .build()
                .unwrap_or_default(),
            kafka_url,
            redis_url,
            opensearch_url,
            dapr_port,
            fluvio_url,
            temporal_url,
            lakehouse_url,
        }
    }

    /// Publish a domain event to Kafka via the Go backend's Kafka REST proxy.
    /// Publish with retry (up to 3 attempts with exponential backoff).
    pub async fn publish_kafka(&self, topic: &str, key: &str, payload: &impl Serialize) {
        let url = match &self.kafka_url {
            Some(u) => u,
            None => return,
        };
        let body = serde_json::json!({
            "topic": topic,
            "key": key,
            "value": payload,
        });
        for attempt in 0..3u32 {
            match self.http.post(format!("{}/produce", url))
                .json(&body)
                .send()
                .await
            {
                Ok(_) => return,
                Err(e) => {
                    warn!(error = %e, topic, attempt, "GOTV Engine: Kafka publish attempt failed");
                    tokio::time::sleep(std::time::Duration::from_millis(100 * 2u64.pow(attempt))).await;
                }
            }
        }
    }

    /// Cache a value in Redis via HTTP proxy.
    pub async fn cache_set(&self, key: &str, value: &str, ttl_secs: u64) {
        let url = match &self.redis_url {
            Some(u) => u,
            None => return,
        };
        let body = serde_json::json!({
            "command": "SET",
            "args": [format!("gotv-engine:{}", key), value, "EX", ttl_secs.to_string()],
        });
        let _ = self.http.post(format!("{}/command", url))
            .json(&body)
            .send()
            .await;
    }

    /// Get a cached value from Redis.
    pub async fn cache_get(&self, key: &str) -> Option<String> {
        let url = self.redis_url.as_ref()?;
        let body = serde_json::json!({
            "command": "GET",
            "args": [format!("gotv-engine:{}", key)],
        });
        let resp = self.http.post(format!("{}/command", url))
            .json(&body)
            .send()
            .await
            .ok()?;
        let result: serde_json::Value = resp.json().await.ok()?;
        result.get("result")?.as_str().map(String::from)
    }

    /// Index a document in OpenSearch.
    pub async fn index_opensearch(&self, index: &str, id: &str, doc: &impl Serialize) {
        let url = match &self.opensearch_url {
            Some(u) => u,
            None => return,
        };
        let _ = self.http.put(format!("{}/{}/_doc/{}", url, index, id))
            .json(doc)
            .send()
            .await;
    }

    /// Invoke a service via Dapr sidecar or direct HTTP.
    pub async fn dapr_invoke(
        &self,
        app_id: &str,
        method: &str,
        payload: &impl Serialize,
    ) -> Option<serde_json::Value> {
        let url = if let Some(port) = &self.dapr_port {
            format!(
                "http://localhost:{}/v1.0/invoke/{}/method/{}",
                port, app_id, method
            )
        } else {
            // Direct fallback
            let port = match app_id {
                "gotv-svc" => "8103",
                "gotv-analytics" => "8102",
                _ => return None,
            };
            format!("http://localhost:{}/{}", port, method)
        };
        let resp = self.http.post(&url).json(payload).send().await.ok()?;
        resp.json().await.ok()
    }

    /// Stream an event to Fluvio for real-time processing.
    pub async fn stream_fluvio(&self, topic: &str, payload: &impl Serialize) {
        let url = match &self.fluvio_url {
            Some(u) => u,
            None => return,
        };
        let body = serde_json::json!({
            "topic": topic,
            "key": "gotv-engine",
            "payload": serde_json::to_string(payload).unwrap_or_default(),
        });
        let _ = self.http.post(format!("{}/produce", url))
            .json(&body)
            .send()
            .await;
    }

    /// Query the Lakehouse for analytical data.
    /// Only allows SELECT queries to prevent injection.
    pub async fn query_lakehouse(&self, sql: &str) -> Option<Vec<serde_json::Value>> {
        let url = self.lakehouse_url.as_ref()?;
        let trimmed = sql.trim().to_uppercase();
        if !trimmed.starts_with("SELECT") {
            warn!("Lakehouse query rejected: not a SELECT");
            return None;
        }
        for banned in &["DROP", "DELETE", "INSERT", "UPDATE", "ALTER", "TRUNCATE", "--", ";"] {
            if trimmed.contains(banned) {
                warn!(keyword = banned, "Lakehouse query rejected: forbidden keyword");
                return None;
            }
        }
        let body = serde_json::json!({"query": sql});
        let resp = self.http.post(format!("{}/query", url))
            .json(&body)
            .send()
            .await
            .ok()?;
        let result: serde_json::Value = resp.json().await.ok()?;
        result.get("data")?.as_array().cloned()
    }

    /// Get middleware connectivity status.
    pub fn status(&self) -> serde_json::Value {
        serde_json::json!({
            "kafka": self.kafka_url.is_some(),
            "redis": self.redis_url.is_some(),
            "opensearch": self.opensearch_url.is_some(),
            "dapr": self.dapr_port.is_some(),
            "fluvio": self.fluvio_url.is_some(),
            "temporal": self.temporal_url.is_some(),
            "lakehouse": self.lakehouse_url.is_some(),
        })
    }
}

/// Shared middleware state for use in Axum handlers.
pub type SharedMiddleware = Arc<Middleware>;
