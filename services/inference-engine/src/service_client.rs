//! Inter-service HTTP clients for the INEC microservice mesh.
//! Allows the inference engine to call other services (auth validation,
//! analytics data, event publishing) via typed Rust clients.

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
        let status = resp.status();
        if !status.is_success() {
            anyhow::bail!("{}: GET {} returned {}", self.service_name, path, status);
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
        let status = resp.status();
        if !status.is_success() {
            anyhow::bail!("{}: POST {} returned {}", self.service_name, path, status);
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

// --- Typed Service Clients ---

#[derive(Clone)]
pub struct AuthServiceClient(ServiceClient);

impl AuthServiceClient {
    pub fn new(base_url: &str) -> Self {
        Self(ServiceClient::new("auth-svc", base_url))
    }

    pub async fn validate_token(&self, token: &str) -> Result<TokenClaims> {
        let url = format!("{}/auth/me", self.0.base_url);
        let resp = self
            .0
            .client
            .get(&url)
            .header("Authorization", format!("Bearer {}", token))
            .send()
            .await?;
        if !resp.status().is_success() {
            anyhow::bail!("token invalid: {}", resp.status());
        }
        Ok(resp.json().await?)
    }

    pub async fn health(&self) -> bool {
        self.0.health().await
    }
}

#[derive(Clone)]
pub struct AnalyticsClient(ServiceClient);

impl AnalyticsClient {
    pub fn new(base_url: &str) -> Self {
        Self(ServiceClient::new("lakehouse-analytics", base_url))
    }

    pub async fn get_election_data(&self, election_id: i64) -> Result<serde_json::Value> {
        self.0
            .get(&format!("/analytics/election/{}", election_id))
            .await
    }

    pub async fn health(&self) -> bool {
        self.0.health().await
    }
}

#[derive(Clone)]
pub struct EventBusClient(ServiceClient);

impl EventBusClient {
    pub fn new(base_url: &str) -> Self {
        Self(ServiceClient::new("middleware-svc", base_url))
    }

    pub async fn publish(&self, topic: &str, key: &str, value: &serde_json::Value) -> Result<()> {
        #[derive(Serialize)]
        struct PublishReq<'a> {
            topic: &'a str,
            key: &'a str,
            value: &'a serde_json::Value,
        }
        let _: serde_json::Value = self
            .0
            .post(
                "/kafka/publish",
                &PublishReq { topic, key, value },
            )
            .await?;
        Ok(())
    }

    pub async fn health(&self) -> bool {
        self.0.health().await
    }
}

#[derive(Clone)]
pub struct GeoServiceClient(ServiceClient);

impl GeoServiceClient {
    pub fn new(base_url: &str) -> Self {
        Self(ServiceClient::new("geo-svc", base_url))
    }

    pub async fn validate_geofence(&self, lat: f64, lng: f64, pu_id: &str) -> Result<GeofenceResult> {
        self.0
            .post(
                "/geo/validate-geofence",
                &serde_json::json!({
                    "latitude": lat,
                    "longitude": lng,
                    "polling_unit_id": pu_id,
                }),
            )
            .await
    }

    pub async fn health(&self) -> bool {
        self.0.health().await
    }
}

// --- Response Types ---

#[derive(Debug, Clone, Deserialize)]
pub struct TokenClaims {
    pub user_id: String,
    pub role: String,
    #[serde(default)]
    pub permissions: Vec<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct GeofenceResult {
    pub valid: bool,
    pub distance_meters: f64,
    pub polling_unit_id: String,
}

// --- Service Registry ---

#[derive(Clone)]
pub struct ServiceRegistry {
    pub auth: AuthServiceClient,
    pub analytics: AnalyticsClient,
    pub event_bus: EventBusClient,
    pub geo: GeoServiceClient,
}

impl ServiceRegistry {
    pub fn from_env() -> Self {
        let auth_url = std::env::var("AUTH_URL").unwrap_or_else(|_| "http://localhost:8090".into());
        let analytics_url =
            std::env::var("LAKEHOUSE_URL").unwrap_or_else(|_| "http://localhost:8098".into());
        let middleware_url =
            std::env::var("MIDDLEWARE_URL").unwrap_or_else(|_| "http://localhost:8085".into());
        let geo_url = std::env::var("GEO_URL").unwrap_or_else(|_| "http://localhost:8093".into());

        Self {
            auth: AuthServiceClient::new(&auth_url),
            analytics: AnalyticsClient::new(&analytics_url),
            event_bus: EventBusClient::new(&middleware_url),
            geo: GeoServiceClient::new(&geo_url),
        }
    }
}
