//! Inter-service HTTP clients for the Fluvio Stream processor.
//! Allows the stream processor to validate auth tokens, publish processed
//! events to analytics, and notify other services of stream state changes.

use anyhow::{Context, Result};
use reqwest::Client;
use serde::{de::DeserializeOwned, Deserialize, Serialize};
use std::time::Duration;
use tracing::warn;

#[derive(Clone)]
pub struct ServiceClient {
    client: Client,
    base_url: String,
    service_name: String,
}

impl ServiceClient {
    pub fn new(service_name: &str, base_url: &str) -> Self {
        let client = Client::builder()
            .timeout(Duration::from_secs(10))
            .build()
            .expect("failed to build HTTP client");
        Self {
            client,
            base_url: base_url.trim_end_matches('/').to_string(),
            service_name: service_name.to_string(),
        }
    }

    pub async fn get<T: DeserializeOwned>(&self, path: &str) -> Result<T> {
        let url = format!("{}{}", self.base_url, path);
        let resp = self
            .client
            .get(&url)
            .header("X-Service-Name", &self.service_name)
            .send()
            .await
            .with_context(|| format!("{}: GET {} failed", self.service_name, path))?;
        if !resp.status().is_success() {
            anyhow::bail!("{}: GET {} returned {}", self.service_name, path, resp.status());
        }
        resp.json().await.with_context(|| format!("{}: decode failed", self.service_name))
    }

    pub async fn post<B: Serialize, T: DeserializeOwned>(&self, path: &str, body: &B) -> Result<T> {
        let url = format!("{}{}", self.base_url, path);
        let resp = self
            .client
            .post(&url)
            .header("X-Service-Name", &self.service_name)
            .json(body)
            .send()
            .await
            .with_context(|| format!("{}: POST {} failed", self.service_name, path))?;
        if !resp.status().is_success() {
            anyhow::bail!("{}: POST {} returned {}", self.service_name, path, resp.status());
        }
        resp.json().await.with_context(|| format!("{}: decode failed", self.service_name))
    }

    pub async fn health(&self) -> bool {
        match self.get::<serde_json::Value>("/health").await {
            Ok(_) => true,
            Err(e) => {
                warn!("{} health check failed: {}", self.service_name, e);
                false
            }
        }
    }
}

// --- Typed Clients for Fluvio Stream Processor ---

#[derive(Clone)]
pub struct InferenceEngineClient(ServiceClient);

impl InferenceEngineClient {
    pub fn new(base_url: &str) -> Self {
        Self(ServiceClient::new("inference-engine", base_url))
    }

    pub async fn detect_anomaly(&self, features: &[f64]) -> Result<AnomalyResult> {
        self.0
            .post("/predict/anomaly", &serde_json::json!({ "features": features }))
            .await
    }

    pub async fn health(&self) -> bool {
        self.0.health().await
    }
}

#[derive(Clone)]
pub struct ElectionServiceClient(ServiceClient);

impl ElectionServiceClient {
    pub fn new(base_url: &str) -> Self {
        Self(ServiceClient::new("election-svc", base_url))
    }

    pub async fn get_election(&self, id: i64) -> Result<serde_json::Value> {
        self.0.get(&format!("/elections/{}", id)).await
    }

    pub async fn health(&self) -> bool {
        self.0.health().await
    }
}

#[derive(Clone)]
pub struct ComplianceClient(ServiceClient);

impl ComplianceClient {
    pub fn new(base_url: &str) -> Self {
        Self(ServiceClient::new("compliance-svc", base_url))
    }

    pub async fn log_audit_event(&self, action: &str, entity_type: &str, entity_id: &str) -> Result<()> {
        let _: serde_json::Value = self
            .0
            .post(
                "/compliance/audit",
                &serde_json::json!({
                    "action": action,
                    "entity_type": entity_type,
                    "entity_id": entity_id,
                }),
            )
            .await?;
        Ok(())
    }

    pub async fn health(&self) -> bool {
        self.0.health().await
    }
}

// --- Response Types ---

#[derive(Debug, Clone, Deserialize)]
pub struct AnomalyResult {
    pub is_anomaly: bool,
    pub score: f64,
    pub model: String,
}

// --- Service Registry ---

#[derive(Clone)]
pub struct ServiceRegistry {
    pub inference: InferenceEngineClient,
    pub election: ElectionServiceClient,
    pub compliance: ComplianceClient,
}

impl ServiceRegistry {
    pub fn from_env() -> Self {
        let inference_url =
            std::env::var("INFERENCE_URL").unwrap_or_else(|_| "http://localhost:8097".into());
        let election_url =
            std::env::var("ELECTION_URL").unwrap_or_else(|_| "http://localhost:8091".into());
        let compliance_url =
            std::env::var("COMPLIANCE_URL").unwrap_or_else(|_| "http://localhost:8094".into());

        Self {
            inference: InferenceEngineClient::new(&inference_url),
            election: ElectionServiceClient::new(&election_url),
            compliance: ComplianceClient::new(&compliance_url),
        }
    }
}
