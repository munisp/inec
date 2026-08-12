use actix_web::{
    body::BoxBody,
    dev::{ServiceRequest, ServiceResponse},
    middleware::{self, Next},
    web, App, Error, HttpResponse, HttpServer,
};
use fluvio::{Fluvio, FluvioConfig, TopicProducerPool, ConsumerConfig, Offset};
use fluvio::metadata::topic::TopicSpec;
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tokio::sync::RwLock;
use tracing::{info, error, warn};
use chrono::Utc;
use uuid::Uuid;

// Election event types that flow through the streaming pipeline.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "event_type")]
pub enum ElectionEvent {
    ResultSubmitted {
        id: String,
        election_id: i64,
        polling_unit_code: String,
        party_code: String,
        votes: i64,
        submitted_by: String,
        timestamp: String,
    },
    ResultValidated {
        id: String,
        result_id: String,
        validator: String,
        status: String,
        timestamp: String,
    },
    IncidentReported {
        id: String,
        severity: String,
        polling_unit_code: String,
        description: String,
        timestamp: String,
    },
    CollationUpdate {
        id: String,
        level: String,
        code: String,
        total_votes: i64,
        timestamp: String,
    },
    AuditEntry {
        id: String,
        action: String,
        entity_type: String,
        entity_id: String,
        user_id: String,
        timestamp: String,
    },
}

// Topics for INEC election data streams.
const TOPIC_RESULTS: &str = "inec.results.submitted";
const TOPIC_VALIDATED: &str = "inec.results.validated";
const TOPIC_INCIDENTS: &str = "inec.incidents.reported";
const TOPIC_COLLATION: &str = "inec.collation.updates";
const TOPIC_AUDIT: &str = "inec.audit.log";

const ALL_TOPICS: &[&str] = &[
    TOPIC_RESULTS,
    TOPIC_VALIDATED,
    TOPIC_INCIDENTS,
    TOPIC_COLLATION,
    TOPIC_AUDIT,
];

/// Hard cap on records returned per /consume request.
const MAX_CONSUME_LIMIT: usize = 1000;
/// Maximum time a /consume request may spend reading from the broker.
const CONSUME_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(5);

// Shared application state.
struct AppState {
    fluvio: Fluvio,
    // TopicProducer<SpuSocketPool> is not Clone in fluvio 0.24 — shared via Arc.
    producers: RwLock<std::collections::HashMap<String, Arc<TopicProducerPool>>>,
    stats: RwLock<StreamStats>,
}

#[derive(Debug, Clone, Serialize, Default)]
struct StreamStats {
    total_produced: u64,
    total_consumed: u64,
    topics_created: Vec<String>,
    last_event_at: Option<String>,
    uptime_seconds: u64,
}

#[derive(Debug, Deserialize)]
struct ProduceRequest {
    topic: String,
    key: Option<String>,
    event: serde_json::Value,
}

#[derive(Debug, Deserialize)]
struct ConsumeQuery {
    topic: String,
    offset: Option<i64>,
    limit: Option<usize>,
}

// POST /produce — publish an event to a Fluvio topic
async fn produce_event(
    state: web::Data<Arc<AppState>>,
    body: web::Json<ProduceRequest>,
) -> HttpResponse {
    let topic = &body.topic;
    let key = body.key.clone().unwrap_or_else(|| Uuid::new_v4().to_string());
    let payload = serde_json::to_string(&body.event).unwrap_or_default();

    let producer = {
        let producers = state.producers.read().await;
        producers.get(topic).cloned()
    };

    let producer = match producer {
        Some(p) => p,
        None => {
            match state.fluvio.topic_producer(topic).await {
                Ok(p) => {
                    let p = Arc::new(p);
                    let mut producers = state.producers.write().await;
                    producers.insert(topic.clone(), p.clone());
                    p
                }
                Err(e) => {
                    error!("Failed to create producer for topic {}: {}", topic, e);
                    return HttpResponse::InternalServerError().json(serde_json::json!({
                        "error": format!("Failed to create producer: {}", e)
                    }));
                }
            }
        }
    };

    match producer.send(key.as_bytes().to_vec(), payload.as_bytes().to_vec()).await {
        Ok(_) => {
            let mut stats = state.stats.write().await;
            stats.total_produced += 1;
            stats.last_event_at = Some(Utc::now().to_rfc3339());
            HttpResponse::Ok().json(serde_json::json!({
                "produced": true,
                "topic": topic,
                "key": key,
            }))
        }
        Err(e) => {
            error!("Produce failed: {}", e);
            HttpResponse::InternalServerError().json(serde_json::json!({
                "error": format!("Produce failed: {}", e)
            }))
        }
    }
}

// GET /consume — consume events from a Fluvio topic
async fn consume_events(
    state: web::Data<Arc<AppState>>,
    query: web::Query<ConsumeQuery>,
) -> HttpResponse {
    let topic = &query.topic;
    let offset = query.offset.unwrap_or(0);
    let limit = query.limit.unwrap_or(100).min(MAX_CONSUME_LIMIT);

    // All managed topics are created with a single partition (see
    // ensure_topics), so partition 0 holds the full log.
    let consumer = match state
        .fluvio
        .partition_consumer(topic.clone(), 0)
        .await
    {
        Ok(c) => c,
        Err(e) => {
            error!("Failed to create consumer for topic {}: {}", topic, e);
            return HttpResponse::InternalServerError().json(serde_json::json!({
                "error": format!("Consumer creation failed: {}", e)
            }));
        }
    };

    let consumer_config = match ConsumerConfig::builder().build() {
        Ok(c) => c,
        Err(e) => {
            error!("Invalid consumer config for topic {}: {}", topic, e);
            return HttpResponse::InternalServerError().json(serde_json::json!({
                "error": format!("Invalid consumer config: {}", e)
            }));
        }
    };

    let mut records = Vec::new();
    use futures_util::StreamExt;
    let offset_start = Offset::absolute(offset).unwrap_or_else(|_| Offset::beginning());
    let mut stream = match consumer
        .stream_with_config(offset_start, consumer_config)
        .await
    {
        Ok(s) => s,
        Err(e) => {
            error!("Failed to open consumer stream for topic {}: {}", topic, e);
            return HttpResponse::InternalServerError().json(serde_json::json!({
                "error": format!("Consumer stream failed: {}", e)
            }));
        }
    };

    // Bounded read: a topic with fewer records than `limit` (or a stalled
    // broker) must not hang the request forever. On timeout we return the
    // records collected so far and flag the response as partial.
    let timed_out = tokio::time::timeout(CONSUME_TIMEOUT, async {
        while let Some(result) = stream.next().await {
            match result {
                Ok(record) => {
                    let value: serde_json::Value = serde_json::from_slice(record.value())
                        .unwrap_or(serde_json::Value::String(
                            String::from_utf8_lossy(record.value()).to_string(),
                        ));
                    records.push(serde_json::json!({
                        "offset": record.offset(),
                        "key": String::from_utf8_lossy(record.key().unwrap_or(&[])),
                        "value": value,
                        "timestamp": record.timestamp(),
                    }));
                    if records.len() >= limit {
                        break;
                    }
                }
                Err(e) => {
                    warn!("Consumer stream error on topic {}: {}", topic, e);
                    break;
                }
            }
        }
    })
    .await
    .is_err();

    if timed_out {
        warn!(
            "Consume on topic {} timed out after {:?}; returning {} partial records",
            topic, CONSUME_TIMEOUT, records.len()
        );
    }

    let mut stats = state.stats.write().await;
    stats.total_consumed += records.len() as u64;

    HttpResponse::Ok().json(serde_json::json!({
        "topic": topic,
        "records": records,
        "count": records.len(),
        "partial": timed_out,
    }))
}

// GET /topics — list all managed topics
async fn list_topics(state: web::Data<Arc<AppState>>) -> HttpResponse {
    let stats = state.stats.read().await;
    HttpResponse::Ok().json(serde_json::json!({
        "topics": stats.topics_created,
    }))
}

// GET /health — health check
async fn health_check(state: web::Data<Arc<AppState>>) -> HttpResponse {
    let stats = state.stats.read().await;
    HttpResponse::Ok().json(serde_json::json!({
        "status": "healthy",
        "service": "fluvio-stream",
        "stats": *stats,
    }))
}

// GET /stats — detailed statistics
async fn get_stats(state: web::Data<Arc<AppState>>) -> HttpResponse {
    let stats = state.stats.read().await;
    HttpResponse::Ok().json(serde_json::json!(*stats))
}

/// API-key authentication for all endpoints. /health stays public for
/// orchestrator probes. Fail-closed: when FLUVIO_STREAM_API_KEY is unset the
/// service returns 503 rather than allowing forged audit/election events to
/// be produced by unauthenticated callers.
async fn api_key_auth(
    req: ServiceRequest,
    next: Next<BoxBody>,
) -> Result<ServiceResponse<BoxBody>, Error> {
    if req.path() == "/health" {
        return next.call(req).await.map(ServiceResponse::map_into_boxed_body);
    }

    let expected_key = std::env::var("FLUVIO_STREAM_API_KEY").unwrap_or_default();
    if expected_key.is_empty() {
        return Ok(req.into_response(
            HttpResponse::ServiceUnavailable()
                .json(serde_json::json!({
                    "error": "FLUVIO_STREAM_API_KEY not configured; refusing to serve unauthenticated requests"
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

// Ensure all INEC topics exist.
async fn ensure_topics(fluvio: &Fluvio) -> Vec<String> {
    let admin = fluvio.admin().await;
    let mut created = Vec::new();

    for topic_name in ALL_TOPICS {
        let spec = TopicSpec::new_computed(1, 1, None);
        match admin.create(topic_name.to_string(), false, spec).await {
            Ok(_) => {
                info!("Created topic: {}", topic_name);
                created.push(topic_name.to_string());
            }
            Err(e) => {
                warn!("Topic {} may already exist: {}", topic_name, e);
                created.push(topic_name.to_string());
            }
        }
    }
    created
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt::init();
    info!("Starting INEC Fluvio Stream Processor");

    let fluvio_endpoint = std::env::var("FLUVIO_ENDPOINT")
        .unwrap_or_else(|_| "localhost:9003".to_string());

    info!("Connecting to Fluvio at {}", fluvio_endpoint);
    let config = FluvioConfig::new(&fluvio_endpoint);
    let fluvio = Fluvio::connect_with_config(&config).await?;
    info!("Connected to Fluvio cluster");

    let topics = ensure_topics(&fluvio).await;

    let state = Arc::new(AppState {
        fluvio,
        producers: RwLock::new(std::collections::HashMap::new()),
        stats: RwLock::new(StreamStats {
            topics_created: topics,
            ..Default::default()
        }),
    });

    let port: u16 = std::env::var("PORT")
        .unwrap_or_else(|_| "9003".to_string())
        .parse()
        .unwrap_or(9003);

    info!("Fluvio Stream Processor listening on port {}", port);

    HttpServer::new(move || {
        App::new()
            .app_data(web::Data::new(state.clone()))
            .wrap(middleware::from_fn(api_key_auth))
            .route("/health", web::get().to(health_check))
            .route("/stats", web::get().to(get_stats))
            .route("/topics", web::get().to(list_topics))
            .route("/produce", web::post().to(produce_event))
            .route("/consume", web::get().to(consume_events))
    })
    .bind(("0.0.0.0", port))?
    .run()
    .await?;

    Ok(())
}
