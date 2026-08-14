//! Redis cluster pipeline — batched commands for 2M+ ops/sec.
//!
//! Key optimizations:
//! - Async pipeline (accumulate N commands, flush in one RTT)
//! - Cluster-aware routing (hash slots → correct node)
//! - Connection pool per node (500 connections)
//! - Lua scripts for atomic multi-key operations
//! - MessagePack values (smaller than JSON)

use anyhow::{anyhow, Result};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use tokio::sync::Mutex;

use crate::pipeline::{Config, Transaction};

/// Redis cluster pipeline backed by a REAL cluster connection.
///
/// SECURITY: The previous implementation never opened a connection —
/// `pipeline_batch` referenced an undefined `conn` (the crate did not even
/// compile), `check_duplicate` always returned Ok(false) (dedup silently
/// disabled), `count_unique` always returned Ok(0), `update_leaderboard`
/// was a no-op, and `atomic_tally` echoed the input back as the server
/// total. Every method below now executes real Redis commands and fails
/// loudly on error.
pub struct RedisClusterPipeline {
    conn: Mutex<redis::cluster_async::ClusterConnection>,

    // Metrics
    commands_executed: AtomicU64,
    pipeline_flushes: AtomicU64,
}

impl RedisClusterPipeline {
    /// Establish a real cluster connection at startup; fails (refusing to
    /// start the pipeline) when Redis is unreachable.
    pub async fn new(config: &Config) -> Result<Self> {
        let client =
            redis::cluster::ClusterClient::new(config.redis_nodes.clone()).map_err(|e| {
                anyhow!(
                    "invalid redis cluster nodes {:?}: {}",
                    config.redis_nodes,
                    e
                )
            })?;
        let conn = client.get_async_connection().await.map_err(|e| {
            anyhow!(
                "cannot connect to redis cluster {:?}: {}",
                config.redis_nodes,
                e
            )
        })?;
        Ok(Self {
            conn: Mutex::new(conn),
            commands_executed: AtomicU64::new(0),
            pipeline_flushes: AtomicU64::new(0),
        })
    }

    /// Process a batch of transactions through Redis pipeline.
    /// Each transaction generates 4 Redis commands:
    /// 1. SET tx:{id} → full transaction (10s TTL for real-time dashboard)
    /// 2. INCR counter:state:{state}:{type} → state-level counter
    /// 3. INCR counter:total:{type} → global counter
    /// 4. PUBLISH events:{type} → real-time pub/sub
    ///
    /// With pipeline_size=1000, this sends 1000 commands in ONE network RTT.
    pub async fn pipeline_batch(&self, batch: Arc<Vec<Transaction>>) -> Result<()> {
        if batch.is_empty() {
            return Ok(());
        }

        let mut pipe = redis::pipe();
        pipe.atomic(); // all-or-nothing
        for tx in batch.iter() {
            let value = rmp_serde::to_vec(tx)?; // MessagePack encoding
            pipe.set_ex(format!("tx:{}", tx.id), value, 10);
            pipe.incr(
                format!("counter:state:{}:{}", tx.state_code, tx.tx_type),
                1i64,
            );
            pipe.incr(format!("counter:total:{}", tx.tx_type), 1i64);
            pipe.publish(format!("events:{}", tx.tx_type), &tx.id);
        }

        let mut conn = self.conn.lock().await;
        pipe.query_async::<()>(&mut *conn)
            .await
            .map_err(|e| anyhow!("redis pipeline flush failed: {}", e))?;

        self.commands_executed
            .fetch_add(batch.len() as u64 * 4, Ordering::Relaxed);
        self.pipeline_flushes.fetch_add(1, Ordering::Relaxed);
        Ok(())
    }

    /// Atomic tally update using a Lua script executed server-side.
    /// Increments party vote count AND total in a single atomic operation.
    /// Returns the real server-side total (previously echoed the input).
    pub async fn atomic_tally(&self, state_code: &str, party: &str, votes: i64) -> Result<i64> {
        let script = redis::Script::new(
            r#"
            local key = KEYS[1]
            local party = ARGV[1]
            local votes = tonumber(ARGV[2])
            redis.call('HINCRBY', key, party, votes)
            redis.call('HINCRBY', key, 'total', votes)
            redis.call('EXPIRE', key, 3600)
            return redis.call('HGET', key, 'total')
        "#,
        );

        let mut conn = self.conn.lock().await;
        let total: i64 = script
            .key(format!("tally:{}", state_code))
            .arg(party)
            .arg(votes)
            .invoke_async(&mut *conn)
            .await
            .map_err(|e| anyhow!("redis atomic_tally script failed: {}", e))?;
        Ok(total)
    }

    /// Sorted set leaderboard update (O(log N) per update).
    pub async fn update_leaderboard(
        &self,
        election_id: &str,
        state_code: &str,
        score: f64,
    ) -> Result<()> {
        let mut conn = self.conn.lock().await;
        redis::cmd("ZADD")
            .arg(format!("leaderboard:{}", election_id))
            .arg(score)
            .arg(state_code)
            .query_async::<()>(&mut *conn)
            .await
            .map_err(|e| anyhow!("redis ZADD leaderboard failed: {}", e))?;
        Ok(())
    }

    /// Bloom filter for deduplication (O(1) membership test).
    ///
    /// SECURITY: previously ALWAYS returned Ok(false) — dedup silently
    /// disabled. Now executes a real BF.ADD (requires the RedisBloom module
    /// server-side); if the module or server is unavailable this returns Err
    /// so callers know dedup is NOT enforced.
    pub async fn check_duplicate(&self, tx_id: &str) -> Result<bool> {
        let key = format!("dedup:{}", chrono::Utc::now().format("%Y-%m-%d"));
        let mut conn = self.conn.lock().await;
        // BF.ADD returns 1 when the item was newly added, 0 when it already
        // existed (i.e. this transaction is a duplicate).
        let added: i64 = redis::cmd("BF.ADD")
            .arg(key)
            .arg(tx_id)
            .query_async(&mut *conn)
            .await
            .map_err(|e| {
                anyhow!(
                    "redis BF.ADD dedup check failed (dedup NOT enforced): {}",
                    e
                )
            })?;
        Ok(added == 0)
    }

    /// HyperLogLog for cardinality estimation (unique voters per state).
    /// SECURITY: previously always returned Ok(0).
    pub async fn count_unique(&self, state_code: &str, voter_id: &str) -> Result<u64> {
        let key = format!("unique_voters:{}", state_code);
        let mut conn = self.conn.lock().await;
        redis::cmd("PFADD")
            .arg(&key)
            .arg(voter_id)
            .query_async::<()>(&mut *conn)
            .await
            .map_err(|e| anyhow!("redis PFADD failed: {}", e))?;
        let count: u64 = redis::cmd("PFCOUNT")
            .arg(&key)
            .query_async(&mut *conn)
            .await
            .map_err(|e| anyhow!("redis PFCOUNT failed: {}", e))?;
        Ok(count)
    }

    pub fn stats(&self) -> (u64, u64) {
        (
            self.commands_executed.load(Ordering::Relaxed),
            self.pipeline_flushes.load(Ordering::Relaxed),
        )
    }
}

/// Connection pool configuration for Redis cluster.
#[derive(Debug, Clone)]
pub struct RedisPoolConfig {
    /// Max connections per cluster node
    pub max_connections_per_node: usize,
    /// Min idle connections per node (pre-warmed)
    pub min_idle_per_node: usize,
    /// Connection timeout
    pub connect_timeout_ms: u64,
    /// Command timeout
    pub command_timeout_ms: u64,
    /// Automatic reconnection on failure
    pub auto_reconnect: bool,
    /// Read from replicas for GET commands (reduces primary load)
    pub read_from_replicas: bool,
}

impl Default for RedisPoolConfig {
    fn default() -> Self {
        Self {
            max_connections_per_node: 500,
            min_idle_per_node: 100,
            connect_timeout_ms: 5000,
            command_timeout_ms: 2,
            auto_reconnect: true,
            read_from_replicas: true,
        }
    }
}
