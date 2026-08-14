//! Core pipeline engine — orchestrates all hot-path components.

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Instant;

use crossbeam::channel;
use serde::{Deserialize, Serialize};

use crate::fluvio_smart::FluvioSmartProcessor;
use crate::kafka_consumer::KafkaHotConsumer;
use crate::opensearch::OpenSearchBulkWriter;
use crate::redis_cluster::RedisClusterPipeline;
use crate::tigerbeetle::TigerBeetleDirectClient;

/// Configuration loaded from environment variables.
#[derive(Clone)]
pub struct Config {
    pub port: u16,

    // Kafka
    pub kafka_brokers: String,
    pub kafka_group_id: String,
    pub kafka_topics: Vec<String>,
    pub kafka_consumers: usize,  // parallel consumer threads
    pub kafka_batch_size: usize, // messages per batch before processing

    // Redis
    pub redis_nodes: Vec<String>,
    pub redis_pipeline_size: usize, // commands per pipeline flush
    pub redis_pool_size: usize,

    // TigerBeetle
    pub tb_addresses: Vec<String>,
    pub tb_cluster_id: u128,
    pub tb_batch_size: usize, // max 8190

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
    pub channel_capacity: usize, // internal channel buffer size

    // Durable retry (R4-25): failed sink batches are appended to a local
    // write-ahead log and replayed on startup. See wal.rs for the honest
    // at-least-once caveat.
    pub wal_dir: std::path::PathBuf,
}

impl Config {
    pub fn from_env() -> Self {
        Self {
            port: env_u16("PORT", 9091),
            kafka_brokers: env_str("KAFKA_BROKERS", "localhost:9092"),
            kafka_group_id: env_str("KAFKA_GROUP_ID", "inec-hot-path"),
            kafka_topics: env_str(
                "KAFKA_TOPICS",
                "inec.results.submitted,inec.ballots.cast,inec.incidents.reported",
            )
            .split(',')
            .map(|s| s.to_string())
            .collect(),
            kafka_consumers: env_usize("KAFKA_CONSUMERS", 16),
            kafka_batch_size: env_usize("KAFKA_BATCH_SIZE", 10000),
            redis_nodes: env_str("REDIS_NODES", "redis://localhost:6379")
                .split(',')
                .map(|s| s.to_string())
                .collect(),
            redis_pipeline_size: env_usize("REDIS_PIPELINE_SIZE", 1000),
            redis_pool_size: env_usize("REDIS_POOL_SIZE", 500),
            tb_addresses: env_str("TB_ADDRESSES", "localhost:3000")
                .split(',')
                .map(|s| s.to_string())
                .collect(),
            tb_cluster_id: env_str("TB_CLUSTER_ID", "0").parse().unwrap_or(0),
            tb_batch_size: env_usize("TB_BATCH_SIZE", 8190),
            os_urls: env_str("OPENSEARCH_URLS", "http://localhost:9200")
                .split(',')
                .map(|s| s.to_string())
                .collect(),
            os_batch_size: env_usize("OS_BATCH_SIZE", 5000),
            os_flush_ms: env_str("OS_FLUSH_MS", "1000").parse().unwrap_or(1000),
            os_workers: env_usize("OS_WORKERS", 8),
            fluvio_endpoint: env_str("FLUVIO_ENDPOINT", "localhost:9003"),
            fluvio_topics: env_str("FLUVIO_TOPICS", "inec.stream.results,inec.stream.ballots")
                .split(',')
                .map(|s| s.to_string())
                .collect(),
            fluvio_workers: env_usize("FLUVIO_WORKERS", 8),
            // Default 5k batches (not 1M): a million-deep buffer of 10k-message
            // batches is an unbounded-memory footgun, not backpressure.
            channel_capacity: env_usize("CHANNEL_CAPACITY", 5_000),
            // --wal-dir <path> CLI flag wins over WAL_DIR; default /tmp.
            wal_dir: std::path::PathBuf::from(
                wal_dir_from_args().unwrap_or_else(|| env_str("WAL_DIR", "/tmp/inec-hot-path-wal")),
            ),
        }
    }
}

/// Parse `--wal-dir <path>` from process arguments (minimal, flag-only).
fn wal_dir_from_args() -> Option<String> {
    let mut args = std::env::args().skip(1);
    while let Some(arg) = args.next() {
        if arg == "--wal-dir" {
            return args.next();
        }
        if let Some(v) = arg.strip_prefix("--wal-dir=") {
            return Some(v.to_string());
        }
    }
    None
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

/// Per-sink outcome counters used by /readyz to distinguish a sink that has
/// never been exercised from one that is always failing.
#[derive(Default)]
pub struct SinkStats {
    pub successes: AtomicU64,
    pub errors: AtomicU64,
}

impl SinkStats {
    fn record(&self, ok: bool, n: u64) {
        if ok {
            self.successes.fetch_add(n, Ordering::Relaxed);
        } else {
            self.errors.fetch_add(n, Ordering::Relaxed);
        }
    }

    fn snapshot(&self) -> (u64, u64) {
        (
            self.successes.load(Ordering::Relaxed),
            self.errors.load(Ordering::Relaxed),
        )
    }
}

/// The main engine coordinating all pipeline stages.
pub struct Engine {
    config: Arc<Config>,
    start_time: Instant,

    // Metrics
    consumed: Arc<AtomicU64>,
    processed: Arc<AtomicU64>,
    errors: Arc<AtomicU64>,

    // Per-sink outcomes for readiness reporting (/readyz).
    sink_redis: Arc<SinkStats>,
    sink_tigerbeetle: Arc<SinkStats>,
    sink_opensearch: Arc<SinkStats>,
    sink_fluvio: Arc<SinkStats>,

    // Internal channels (lock-free MPMC)
    tx_sender: channel::Sender<Vec<Transaction>>,
    tx_receiver: channel::Receiver<Vec<Transaction>>,

    // Durable retry log for failed sink batches (R4-25).
    wal: Option<Arc<crate::wal::WriteAheadLog>>,
}

impl Engine {
    pub async fn new(config: Arc<Config>) -> anyhow::Result<Self> {
        // SECURITY: without the `kafka` feature there is no real consumer —
        // the previous build "simulated" consumption while presenting as the
        // election hot path. Refuse to start instead.
        if !cfg!(feature = "kafka") {
            anyhow::bail!(
                "hot-path built WITHOUT the `kafka` feature (rdkafka): no real Kafka consumer available; refusing to start a simulated election pipeline. Rebuild with --features kafka"
            );
        }

        let (tx_sender, tx_receiver) = channel::bounded(config.channel_capacity);

        // WAL failure is not fatal at boot only in the sense that the
        // pipeline still runs — but it is logged at error level because
        // without the WAL failed batches are dropped again.
        let wal = match crate::wal::WriteAheadLog::open(&config.wal_dir) {
            Ok(w) => Some(Arc::new(w)),
            Err(e) => {
                tracing::error!(
                    "failed to open WAL dir {}: {e:#} — failed sink batches will NOT be durably retried",
                    config.wal_dir.display()
                );
                None
            }
        };

        Ok(Self {
            config,
            start_time: Instant::now(),
            consumed: Arc::new(AtomicU64::new(0)),
            processed: Arc::new(AtomicU64::new(0)),
            errors: Arc::new(AtomicU64::new(0)),
            sink_redis: Arc::new(SinkStats::default()),
            sink_tigerbeetle: Arc::new(SinkStats::default()),
            sink_opensearch: Arc::new(SinkStats::default()),
            sink_fluvio: Arc::new(SinkStats::default()),
            tx_sender,
            tx_receiver,
            wal,
        })
    }

    /// Run the full pipeline: consume → process → write
    ///
    /// Returns Err at startup if any real backend (Kafka, Redis cluster)
    /// cannot be reached — silence is the bug, so we fail loudly.
    pub async fn run(&self) -> anyhow::Result<()> {
        let config = self.config.clone();

        // Stage 1: Kafka consumers → internal channel
        let sender = self.tx_sender.clone();
        let kafka = KafkaHotConsumer::new(&config);
        let kafka_handle = tokio::spawn({
            let config = config.clone();
            let sender = sender.clone();
            let errors = self.errors.clone();
            async move { kafka.consume_batched(&config, sender, errors).await }
        });

        // Stage 2: Process batches from channel → fan-out to sinks
        let receiver = self.tx_receiver.clone();
        let redis = Arc::new(RedisClusterPipeline::new(&config).await?);
        let tb = Arc::new(TigerBeetleDirectClient::new(&config));
        let os = Arc::new(OpenSearchBulkWriter::new(&config));
        let fluvio = Arc::new(FluvioSmartProcessor::new(&config));

        // R4-25: replay batches that failed during a previous run BEFORE new
        // traffic is processed. Batches that fail again are kept in the WAL.
        self.replay_wal(&redis, &tb, &os, &fluvio).await;

        // Spawn N processor workers
        let mut handles = Vec::new();
        for worker_id in 0..num_cpus::get().min(32) {
            let rx = receiver.clone();
            let redis = redis.clone();
            let tb = tb.clone();
            let os = os.clone();
            let fluvio = fluvio.clone();
            let consumed = self.consumed.clone();
            let processed = self.processed.clone();
            let errors = self.errors.clone();
            let sink_redis = self.sink_redis.clone();
            let sink_tigerbeetle = self.sink_tigerbeetle.clone();
            let sink_opensearch = self.sink_opensearch.clone();
            let sink_fluvio = self.sink_fluvio.clone();
            let wal = self.wal.clone();

            handles.push(tokio::spawn(async move {
                loop {
                    match rx.recv() {
                        Ok(batch) => {
                            let batch_len = batch.len() as u64;
                            consumed.fetch_add(batch_len, Ordering::Relaxed);
                            let batch_arc = Arc::new(batch);

                            // Fan-out to all sinks in parallel
                            let (r1, r2, r3, r4) = tokio::join!(
                                redis.pipeline_batch(batch_arc.clone()),
                                tb.batch_transfer(batch_arc.clone()),
                                os.bulk_index(batch_arc.clone()),
                                fluvio.produce_batch(batch_arc.clone()),
                            );

                            // Log any errors but don't stop processing;
                            // every sink failure is counted loudly.
                            let mut failed = false;
                            if let Err(e) = &r1 { failed = true; tracing::warn!(worker_id, "redis error: {}", e); }
                            if let Err(e) = &r2 { failed = true; tracing::warn!(worker_id, "tb error: {}", e); }
                            if let Err(e) = &r3 { failed = true; tracing::warn!(worker_id, "os error: {}", e); }
                            if let Err(e) = &r4 { failed = true; tracing::warn!(worker_id, "fluvio error: {}", e); }
                            sink_redis.record(r1.is_ok(), batch_len);
                            sink_tigerbeetle.record(r2.is_ok(), batch_len);
                            sink_opensearch.record(r3.is_ok(), batch_len);
                            sink_fluvio.record(r4.is_ok(), batch_len);

                            if failed {
                                errors.fetch_add(batch_len, Ordering::Relaxed);
                                // R4-25: never silently drop a failed batch —
                                // persist it for replay on next startup.
                                if let Some(wal) = &wal {
                                    let mut failed_sinks = Vec::new();
                                    if r1.is_err() { failed_sinks.push("redis"); }
                                    if r2.is_err() { failed_sinks.push("tigerbeetle"); }
                                    if r3.is_err() { failed_sinks.push("opensearch"); }
                                    if r4.is_err() { failed_sinks.push("fluvio"); }
                                    if let Err(e) = wal.append(&failed_sinks, &batch_arc) {
                                        tracing::error!(
                                            "WAL append failed: {e:#} — batch of {} is LOST (no durable fallback left)",
                                            batch_len
                                        );
                                    }
                                }
                            } else {
                                processed.fetch_add(batch_len, Ordering::Relaxed);
                            }
                        }
                        Err(_) => break, // channel closed
                    }
                }
            }));
        }

        // Wait for shutdown; propagate consumer failure loudly.
        kafka_handle
            .await
            .map_err(|e| anyhow::anyhow!("kafka consumer task join error: {}", e))??;
        for h in handles {
            let _ = h.await;
        }
        Ok(())
    }

    /// Replay WAL-persisted failed batches through the same fan-out path.
    /// Batches that fail again are rewritten into the WAL for the next
    /// restart; succeeded batches are counted as processed. At-least-once:
    /// see wal.rs module docs for the delivery-semantics caveat.
    async fn replay_wal(
        &self,
        redis: &Arc<RedisClusterPipeline>,
        tb: &Arc<TigerBeetleDirectClient>,
        os: &Arc<OpenSearchBulkWriter>,
        fluvio: &Arc<FluvioSmartProcessor>,
    ) {
        let Some(wal) = &self.wal else { return };
        let records = match wal.read_all() {
            Ok(r) => r,
            Err(e) => {
                tracing::error!("WAL replay: failed to read {}: {e:#}", wal.path().display());
                return;
            }
        };
        if records.is_empty() {
            return;
        }
        tracing::info!(
            "WAL replay: retrying {} failed batch(es) from previous run",
            records.len()
        );

        let mut still_failed: Vec<crate::wal::WalRecord> = Vec::new();
        for mut record in records {
            let batch_len = record.transactions.len() as u64;
            let batch = Arc::new(record.transactions.clone());
            let (r1, r2, r3, r4) = tokio::join!(
                redis.pipeline_batch(batch.clone()),
                tb.batch_transfer(batch.clone()),
                os.bulk_index(batch.clone()),
                fluvio.produce_batch(batch.clone()),
            );
            self.sink_redis.record(r1.is_ok(), batch_len);
            self.sink_tigerbeetle.record(r2.is_ok(), batch_len);
            self.sink_opensearch.record(r3.is_ok(), batch_len);
            self.sink_fluvio.record(r4.is_ok(), batch_len);

            record.failed_sinks.clear();
            if let Err(e) = &r1 {
                record.failed_sinks.push(format!("redis: {e}"));
            }
            if let Err(e) = &r2 {
                record.failed_sinks.push(format!("tigerbeetle: {e}"));
            }
            if let Err(e) = &r3 {
                record.failed_sinks.push(format!("opensearch: {e}"));
            }
            if let Err(e) = &r4 {
                record.failed_sinks.push(format!("fluvio: {e}"));
            }

            if record.failed_sinks.is_empty() {
                self.processed.fetch_add(batch_len, Ordering::Relaxed);
            } else {
                self.errors.fetch_add(batch_len, Ordering::Relaxed);
                still_failed.push(record);
            }
        }
        if let Err(e) = wal.rewrite(&still_failed) {
            tracing::error!(
                "WAL replay: failed to rewrite {}: {e:#} — {} record(s) may be replayed twice on next start (at-least-once)",
                wal.path().display(),
                still_failed.len()
            );
        }
        tracing::info!(
            "WAL replay complete: {} record(s) still failing and retained",
            still_failed.len()
        );
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
            if uptime > 0.0 {
                processed as f64 / uptime
            } else {
                0.0
            }
        )
    }

    /// Readiness snapshot: per-sink outcomes plus the global error counter.
    /// A sink that has recorded errors but never a success is "always-Err" —
    /// the service is alive but NOT ready, and /readyz must report non-200.
    pub fn readiness(&self) -> (bool, serde_json::Value) {
        let sink_json = |name: &str, s: &SinkStats| {
            let (ok, err) = s.snapshot();
            serde_json::json!({
                "sink": name,
                "successes": ok,
                "errors": err,
                // Not exercised yet counts as available; always-Err does not.
                "available": err == 0 || ok > 0,
            })
        };

        let sinks = serde_json::json!([
            sink_json("redis", &self.sink_redis),
            sink_json("tigerbeetle", &self.sink_tigerbeetle),
            sink_json("opensearch", &self.sink_opensearch),
            sink_json("fluvio", &self.sink_fluvio),
        ]);

        let ready = sinks
            .as_array()
            .map(|arr| {
                arr.iter()
                    .all(|s| s["available"].as_bool().unwrap_or(false))
            })
            .unwrap_or(false);

        let body = serde_json::json!({
            "ready": ready,
            "errors_total": self.errors.load(Ordering::Relaxed),
            "sinks": sinks,
        });
        (ready, body)
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
    std::env::var(key)
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(default)
}

fn env_usize(key: &str, default: usize) -> usize {
    std::env::var(key)
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(default)
}
