use actix_web::{web, App, HttpServer, HttpResponse, middleware};
use fluvio::{Fluvio, FluvioConfig, Offset};
use fluvio::metadata::topic::TopicSpec;
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tokio::sync::RwLock;
use tracing::{info, error, warn};
use chrono::Utc;
use uuid::Uuid;

pub mod service_client;

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

// Shared application state.
struct AppState {
    fluvio: Fluvio,
    producers: RwLock<std::collections::HashMap<String, Arc<fluvio::TopicProducer<fluvio::spu::SpuSocketPool>>>>,
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

    match producer.send(fluvio::RecordKey::from(key.as_bytes().to_vec()), payload.as_bytes().to_vec()).await {
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

// GET /consume — consume events from a Fluvio topic (with timeout to prevent indefinite blocking)
async fn consume_events(
    state: web::Data<Arc<AppState>>,
    query: web::Query<ConsumeQuery>,
) -> HttpResponse {
    let topic = &query.topic;
    let offset = query.offset.unwrap_or(0);
    let limit = query.limit.unwrap_or(100);

    let consumer = match state
        .fluvio
        .partition_consumer(topic, 0)
        .await
    {
        Ok(c) => c,
        Err(e) => {
            return HttpResponse::InternalServerError().json(serde_json::json!({
                "error": format!("Consumer creation failed: {}", e)
            }));
        }
    };

    let mut records = Vec::new();
    use futures_util::StreamExt;
    let mut stream = consumer.stream(Offset::absolute(offset).unwrap_or(Offset::beginning())).await.unwrap();

    // Timeout prevents indefinite blocking on empty/slow topics
    let timeout_duration = tokio::time::Duration::from_secs(5);
    let deadline = tokio::time::Instant::now() + timeout_duration;

    loop {
        let next = tokio::time::timeout_at(deadline, stream.next()).await;
        match next {
            Ok(Some(Ok(record))) => {
                let value: serde_json::Value = serde_json::from_slice(record.value())
                    .unwrap_or(serde_json::Value::String(String::from_utf8_lossy(record.value()).to_string()));
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
            Ok(Some(Err(e))) => {
                error!("Consumer stream error: {}", e);
                break;
            }
            Ok(None) => break,       // Stream ended
            Err(_) => break,         // Timeout reached — return what we have
        }
    }

    let mut stats = state.stats.write().await;
    stats.total_consumed += records.len() as u64;

    HttpResponse::Ok().json(serde_json::json!({
        "topic": topic,
        "records": records,
        "count": records.len(),
        "timed_out": records.len() < limit,
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
