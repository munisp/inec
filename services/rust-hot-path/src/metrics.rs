//! Prometheus metrics for the hot-path engine.

use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Instant;

/// Global metrics registry for the hot-path engine.
pub struct Metrics {
    pub start_time: Instant,

    // Kafka
    pub kafka_messages_consumed: AtomicU64,
    pub kafka_batches_processed: AtomicU64,
    pub kafka_consumer_lag: AtomicU64,

    // Redis
    pub redis_commands_executed: AtomicU64,
    pub redis_pipeline_flushes: AtomicU64,
    pub redis_cache_hits: AtomicU64,
    pub redis_cache_misses: AtomicU64,

    // TigerBeetle
    pub tb_transfers_submitted: AtomicU64,
    pub tb_batches_sent: AtomicU64,
    pub tb_linked_transfers: AtomicU64,

    // OpenSearch
    pub os_documents_indexed: AtomicU64,
    pub os_bulk_requests: AtomicU64,
    pub os_bulk_errors: AtomicU64,

    // Fluvio
    pub fluvio_records_produced: AtomicU64,
    pub fluvio_smart_module_invocations: AtomicU64,

    // Pipeline
    pub total_transactions_processed: AtomicU64,
    pub total_processing_errors: AtomicU64,
    pub channel_depth: AtomicU64,

    // Latency histograms (bucket counts)
    pub latency_under_1ms: AtomicU64,
    pub latency_1_5ms: AtomicU64,
    pub latency_5_10ms: AtomicU64,
    pub latency_10_50ms: AtomicU64,
    pub latency_over_50ms: AtomicU64,
}

impl Metrics {
    pub fn new() -> Self {
        Self {
            start_time: Instant::now(),
            kafka_messages_consumed: AtomicU64::new(0),
            kafka_batches_processed: AtomicU64::new(0),
            kafka_consumer_lag: AtomicU64::new(0),
            redis_commands_executed: AtomicU64::new(0),
            redis_pipeline_flushes: AtomicU64::new(0),
            redis_cache_hits: AtomicU64::new(0),
            redis_cache_misses: AtomicU64::new(0),
            tb_transfers_submitted: AtomicU64::new(0),
            tb_batches_sent: AtomicU64::new(0),
            tb_linked_transfers: AtomicU64::new(0),
            os_documents_indexed: AtomicU64::new(0),
            os_bulk_requests: AtomicU64::new(0),
            os_bulk_errors: AtomicU64::new(0),
            fluvio_records_produced: AtomicU64::new(0),
            fluvio_smart_module_invocations: AtomicU64::new(0),
            total_transactions_processed: AtomicU64::new(0),
            total_processing_errors: AtomicU64::new(0),
            channel_depth: AtomicU64::new(0),
            latency_under_1ms: AtomicU64::new(0),
            latency_1_5ms: AtomicU64::new(0),
            latency_5_10ms: AtomicU64::new(0),
            latency_10_50ms: AtomicU64::new(0),
            latency_over_50ms: AtomicU64::new(0),
        }
    }

    pub fn record_latency(&self, duration_us: u64) {
        match duration_us {
            0..=999 => self.latency_under_1ms.fetch_add(1, Ordering::Relaxed),
            1000..=4999 => self.latency_1_5ms.fetch_add(1, Ordering::Relaxed),
            5000..=9999 => self.latency_5_10ms.fetch_add(1, Ordering::Relaxed),
            10000..=49999 => self.latency_10_50ms.fetch_add(1, Ordering::Relaxed),
            _ => self.latency_over_50ms.fetch_add(1, Ordering::Relaxed),
        };
    }

    pub fn tps(&self) -> f64 {
        let elapsed = self.start_time.elapsed().as_secs_f64();
        if elapsed > 0.0 {
            self.total_transactions_processed.load(Ordering::Relaxed) as f64 / elapsed
        } else {
            0.0
        }
    }

    /// Export as Prometheus text format.
    pub fn to_prometheus(&self) -> String {
        let uptime = self.start_time.elapsed().as_secs_f64();
        format!(
            "# HELP inec_hot_path_kafka_consumed Kafka messages consumed\n\
             # TYPE inec_hot_path_kafka_consumed counter\n\
             inec_hot_path_kafka_consumed {}\n\
             # HELP inec_hot_path_redis_commands Redis commands executed\n\
             # TYPE inec_hot_path_redis_commands counter\n\
             inec_hot_path_redis_commands {}\n\
             # HELP inec_hot_path_tb_transfers TigerBeetle transfers\n\
             # TYPE inec_hot_path_tb_transfers counter\n\
             inec_hot_path_tb_transfers {}\n\
             # HELP inec_hot_path_os_indexed OpenSearch docs indexed\n\
             # TYPE inec_hot_path_os_indexed counter\n\
             inec_hot_path_os_indexed {}\n\
             # HELP inec_hot_path_fluvio_produced Fluvio records produced\n\
             # TYPE inec_hot_path_fluvio_produced counter\n\
             inec_hot_path_fluvio_produced {}\n\
             # HELP inec_hot_path_tps Current TPS\n\
             # TYPE inec_hot_path_tps gauge\n\
             inec_hot_path_tps {:.0}\n\
             # HELP inec_hot_path_uptime_seconds Uptime\n\
             # TYPE inec_hot_path_uptime_seconds gauge\n\
             inec_hot_path_uptime_seconds {:.2}\n\
             # HELP inec_hot_path_latency_bucket Latency distribution\n\
             # TYPE inec_hot_path_latency_bucket histogram\n\
             inec_hot_path_latency_bucket{{le=\"0.001\"}} {}\n\
             inec_hot_path_latency_bucket{{le=\"0.005\"}} {}\n\
             inec_hot_path_latency_bucket{{le=\"0.01\"}} {}\n\
             inec_hot_path_latency_bucket{{le=\"0.05\"}} {}\n\
             inec_hot_path_latency_bucket{{le=\"+Inf\"}} {}\n",
            self.kafka_messages_consumed.load(Ordering::Relaxed),
            self.redis_commands_executed.load(Ordering::Relaxed),
            self.tb_transfers_submitted.load(Ordering::Relaxed),
            self.os_documents_indexed.load(Ordering::Relaxed),
            self.fluvio_records_produced.load(Ordering::Relaxed),
            self.tps(),
            uptime,
            self.latency_under_1ms.load(Ordering::Relaxed),
            self.latency_1_5ms.load(Ordering::Relaxed),
            self.latency_5_10ms.load(Ordering::Relaxed),
            self.latency_10_50ms.load(Ordering::Relaxed),
            self.latency_over_50ms.load(Ordering::Relaxed),
        )
    }
}
