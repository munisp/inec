//! Core pipeline engine — orchestrates all hot-path components.

use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Instant;

use crossbeam::channel;
use serde::{Deserialize, Serialize};

use crate::kafka_consumer::KafkaHotConsumer;
use crate::redis_cluster::RedisClusterPipeline;
use crate::tigerbeetle::TigerBeetleDirectClient;
use crate::opensearch::OpenSearchBulkWriter;
use crate::fluvio_smart::FluvioSmartProcessor;

/// Configuration loaded from environment variables.
#[derive(Clone)]
pub struct Config {
    pub port: u16,

    // Kafka
    pub kafka_brokers: String,
    pub kafka_group_id: String,
    pub kafka_topics: Vec<String>,
    pub kafka_consumers: usize,      // parallel consumer threads
    pub kafka_batch_size: usize,     // messages per batch before processing

    // Redis
    pub redis_nodes: Vec<String>,
    pub redis_pipeline_size: usize,  // commands per pipeline flush
    pub redis_pool_size: usize,

    // TigerBeetle
    pub tb_addresses: Vec<String>,
    pub tb_cluster_id: u128,
    pub tb_batch_size: usize,        // max 8190

    // OpenSearch
    pub os_urls: Vec<String>,
    pub os_batch_size: usize,
    pub os_flush_ms: u64,
    pub os_workers: usize,

    // Fluvio
    pub fluvio_endpoint: String,
    pub fluvio_topics: Vec<String>,
    pub fluvio_workers: usize,

    // Pipeline
    pub channel_capacity: usize,     // internal channel buffer size
}

impl Config {
    pub fn from_env() -> Self {
        Self {
            port: env_u16("PORT", 9091),
            kafka_brokers: env_str("KAFKA_BROKERS", "localhost:9092"),
            kafka_group_id: env_str("KAFKA_GROUP_ID", "inec-hot-path"),
            kafka_topics: env_str("KAFKA_TOPICS", "inec.results.submitted,inec.ballots.cast,inec.incidents.reported")
                .split(',').map(|s| s.to_string()).collect(),
            kafka_consumers: env_usize("KAFKA_CONSUMERS", 16),
            kafka_batch_size: env_usize("KAFKA_BATCH_SIZE", 10000),
            redis_nodes: env_str("REDIS_NODES", "redis://localhost:6379")
                .split(',').map(|s| s.to_string()).collect(),
            redis_pipeline_size: env_usize("REDIS_PIPELINE_SIZE", 1000),
            redis_pool_size: env_usize("REDIS_POOL_SIZE", 500),
            tb_addresses: env_str("TB_ADDRESSES", "localhost:3000")
                .split(',').map(|s| s.to_string()).collect(),
            tb_cluster_id: env_str("TB_CLUSTER_ID", "0").parse().unwrap_or(0),
            tb_batch_size: env_usize("TB_BATCH_SIZE", 8190),
            os_urls: env_str("OPENSEARCH_URLS", "http://localhost:9200")
                .split(',').map(|s| s.to_string()).collect(),
            os_batch_size: env_usize("OS_BATCH_SIZE", 5000),
            os_flush_ms: env_str("OS_FLUSH_MS", "1000").parse().unwrap_or(1000),
            os_workers: env_usize("OS_WORKERS", 8),
            fluvio_endpoint: env_str("FLUVIO_ENDPOINT", "localhost:9003"),
            fluvio_topics: env_str("FLUVIO_TOPICS", "inec.stream.results,inec.stream.ballots")
                .split(',').map(|s| s.to_string()).collect(),
            fluvio_workers: env_usize("FLUVIO_WORKERS", 8),
            channel_capacity: env_usize("CHANNEL_CAPACITY", 1_000_000),
        }
    }
}

/// Core transaction type flowing through the hot path.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Transaction {
    pub id: String,
    #[serde(rename = "type")]
    pub tx_type: String,
    pub source: String,
    pub timestamp: i64,
    pub election_id: String,
    pub state_code: String,
    pub lga_id: String,
    pub ward_id: String,
    pub pu_id: String,
    pub amount: i64,
    pub hash: String,
    #[serde(default)]
    pub data: serde_json::Value,
}

/// The main engine coordinating all pipeline stages.
pub struct Engine {
    config: Arc<Config>,
    start_time: Instant,

    // Metrics
    consumed: AtomicU64,
    processed: AtomicU64,
    errors: AtomicU64,

    // Internal channels (lock-free MPMC)
    tx_sender: channel::Sender<Vec<Transaction>>,
    tx_receiver: channel::Receiver<Vec<Transaction>>,
}

impl Engine {
    pub async fn new(config: Arc<Config>) -> anyhow::Result<Self> {
        let (tx_sender, tx_receiver) = channel::bounded(config.channel_capacity);

        Ok(Self {
            config,
            start_time: Instant::now(),
            consumed: AtomicU64::new(0),
            processed: AtomicU64::new(0),
            errors: AtomicU64::new(0),
            tx_sender,
            tx_receiver,
        })
    }

    /// Run the full pipeline: consume → process → write
    pub async fn run(&self) {
        let config = self.config.clone();

        // Stage 1: Kafka consumers → internal channel
        let sender = self.tx_sender.clone();
        let consumed = &self.consumed;
        let kafka = KafkaHotConsumer::new(&config);
        let kafka_handle = tokio::spawn({
            let config = config.clone();
            let sender = sender.clone();
            async move {
                kafka.consume_batched(&config, sender).await;
            }
        });

        // Stage 2: Process batches from channel → fan-out to sinks
        let receiver = self.tx_receiver.clone();
        let redis = Arc::new(RedisClusterPipeline::new(&config).await);
        let tb = Arc::new(TigerBeetleDirectClient::new(&config));
        let os = Arc::new(OpenSearchBulkWriter::new(&config));
        let fluvio = Arc::new(FluvioSmartProcessor::new(&config));

        let processed = &self.processed;
        let errors = &self.errors;

        // Spawn N processor workers
        let mut handles = Vec::new();
        for worker_id in 0..num_cpus::get().min(32) {
            let rx = receiver.clone();
            let redis = redis.clone();
            let tb = tb.clone();
            let os = os.clone();
            let fluvio = fluvio.clone();

            handles.push(tokio::spawn(async move {
                loop {
                    match rx.recv() {
                        Ok(batch) => {
                            let batch_arc = Arc::new(batch);

                            // Fan-out to all sinks in parallel
                            let (r1, r2, r3, r4) = tokio::join!(
                                redis.pipeline_batch(batch_arc.clone()),
                                tb.batch_transfer(batch_arc.clone()),
                                os.bulk_index(batch_arc.clone()),
                                fluvio.produce_batch(batch_arc.clone()),
                            );

                            // Log any errors but don't stop processing
                            if let Err(e) = r1 { tracing::warn!(worker_id, "redis error: {}", e); }
                            if let Err(e) = r2 { tracing::warn!(worker_id, "tb error: {}", e); }
                            if let Err(e) = r3 { tracing::warn!(worker_id, "os error: {}", e); }
                            if let Err(e) = r4 { tracing::warn!(worker_id, "fluvio error: {}", e); }
                        }
                        Err(_) => break, // channel closed
                    }
                }
            }));
        }

        // Wait for shutdown
        let _ = kafka_handle.await;
        for h in handles {
            let _ = h.await;
        }
    }

    pub fn prometheus_metrics(&self) -> String {
        let consumed = self.consumed.load(Ordering::Relaxed);
        let processed = self.processed.load(Ordering::Relaxed);
        let errors = self.errors.load(Ordering::Relaxed);
        let uptime = self.start_time.elapsed().as_secs_f64();

        format!(
            "# HELP inec_hot_path_consumed_total Messages consumed from Kafka\n\
             # TYPE inec_hot_path_consumed_total counter\n\
             inec_hot_path_consumed_total {consumed}\n\
             # HELP inec_hot_path_processed_total Transactions processed through pipeline\n\
             # TYPE inec_hot_path_processed_total counter\n\
             inec_hot_path_processed_total {processed}\n\
             # HELP inec_hot_path_errors_total Processing errors\n\
             # TYPE inec_hot_path_errors_total counter\n\
             inec_hot_path_errors_total {errors}\n\
             # HELP inec_hot_path_uptime_seconds Engine uptime\n\
             # TYPE inec_hot_path_uptime_seconds gauge\n\
             inec_hot_path_uptime_seconds {uptime:.2}\n\
             # HELP inec_hot_path_tps Current transactions per second\n\
             # TYPE inec_hot_path_tps gauge\n\
             inec_hot_path_tps {:.0}\n",
            if uptime > 0.0 { processed as f64 / uptime } else { 0.0 }
        )
    }

    pub fn stats(&self) -> serde_json::Value {
        let uptime = self.start_time.elapsed().as_secs_f64();
        let processed = self.processed.load(Ordering::Relaxed);
        serde_json::json!({
            "uptime_sec": uptime,
            "consumed": self.consumed.load(Ordering::Relaxed),
            "processed": processed,
            "errors": self.errors.load(Ordering::Relaxed),
            "tps": if uptime > 0.0 { processed as f64 / uptime } else { 0.0 },
            "channel_depth": self.tx_sender.len(),
            "channel_capacity": self.config.channel_capacity,
        })
    }
}

fn env_str(key: &str, default: &str) -> String {
    std::env::var(key).unwrap_or_else(|_| default.to_string())
}

fn env_u16(key: &str, default: u16) -> u16 {
    std::env::var(key).ok().and_then(|v| v.parse().ok()).unwrap_or(default)
}

fn env_usize(key: &str, default: usize) -> usize {
    std::env::var(key).ok().and_then(|v| v.parse().ok()).unwrap_or(default)
}
