package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port int

	// Kafka: batch producer config for 1M+ msgs/sec
	KafkaBrokers      []string
	KafkaBatchSize    int // messages per batch (default 10000)
	KafkaBatchTimeout int // ms to wait before flushing (default 5)
	KafkaPartitions   int // partitions per topic (default 128)
	KafkaCompression  string
	KafkaWorkers      int // parallel producer goroutines

	// Redis: pipeline config for 2M+ ops/sec
	RedisAddrs       []string
	RedisCluster     bool
	RedisPipeSize    int // commands per pipeline flush (default 1000)
	RedisPipeTimeout int // ms (default 1)
	RedisPoolSize    int // connections per node (default 500)

	// TigerBeetle: batch transfers for 1M+ TPS
	TBAddress   string
	TBClusterID uint64
	TBBatchSize int // transfers per batch (default 8190 = TB max)
	TBWorkers   int // parallel batch submitters

	// Postgres: pgx pool for 500K+ queries/sec
	PGConnString string
	PGPoolSize   int // max connections (default 200)
	PGBatchSize  int // rows per COPY batch (default 10000)

	// Mojaloop: concurrent transfer pipeline
	MojaBaseURL     string
	MojaConcurrency int // parallel transfers (default 5000)
	MojaTimeout     int // ms per transfer (default 2000)

	// Temporal: high-throughput workflow dispatch
	TemporalAddr      string
	TemporalNamespace string
	TemporalWorkers   int // workflow workers (default 100)
	TemporalTaskQueue string

	// APISIX: rate limiter sync + route cache
	APISIXAdminURL string
	APISIXAPIKey   string

	// OpenSearch: bulk indexer for 500K+ docs/sec
	OSAddrs         []string
	OSBatchSize     int // documents per bulk request (default 5000)
	OSFlushInterval int // ms between flushes (default 1000)
	OSWorkers       int // parallel bulk workers (default 8)

	// Dapr: bulk publish
	DaprURL       string
	DaprBatchSize int // events per bulk publish (default 1000)

	// Permify: cached permission checks
	PermifyURL       string
	PermifyCacheSize int // LRU cache entries (default 100000)
	PermifyCacheTTL  int // seconds (default 30)

	// Fluvio: stream processing
	FluvioURL     string
	FluvioWorkers int
}

func LoadConfig() Config {
	return Config{
		Port: envInt("PORT", 9090),

		KafkaBrokers:      strings.Split(envRequired("KAFKA_BROKERS"), ","),
		KafkaBatchSize:    envInt("KAFKA_BATCH_SIZE", 10000),
		KafkaBatchTimeout: envInt("KAFKA_BATCH_TIMEOUT_MS", 5),
		KafkaPartitions:   envInt("KAFKA_PARTITIONS", 128),
		KafkaCompression:  envStr("KAFKA_COMPRESSION", "lz4"),
		KafkaWorkers:      envInt("KAFKA_WORKERS", 32),

		RedisAddrs:       strings.Split(envRequired("REDIS_ADDRS"), ","),
		RedisCluster:     envBool("REDIS_CLUSTER", false),
		RedisPipeSize:    envInt("REDIS_PIPE_SIZE", 1000),
		RedisPipeTimeout: envInt("REDIS_PIPE_TIMEOUT_MS", 1),
		RedisPoolSize:    envInt("REDIS_POOL_SIZE", 500),

		TBAddress:   envRequired("TB_ADDRESS"),
		TBClusterID: uint64(envInt("TB_CLUSTER_ID", 0)),
		TBBatchSize: envInt("TB_BATCH_SIZE", 8190),
		TBWorkers:   envInt("TB_WORKERS", 16),

		PGConnString: envRequired("DATABASE_URL"),
		PGPoolSize:   envInt("PG_POOL_SIZE", 200),
		PGBatchSize:  envInt("PG_BATCH_SIZE", 10000),

		MojaBaseURL:     envRequired("MOJALOOP_URL"),
		MojaConcurrency: envInt("MOJA_CONCURRENCY", 5000),
		MojaTimeout:     envInt("MOJA_TIMEOUT_MS", 2000),

		TemporalAddr:      envRequired("TEMPORAL_ADDR"),
		TemporalNamespace: envStr("TEMPORAL_NAMESPACE", "inec-production"),
		TemporalWorkers:   envInt("TEMPORAL_WORKERS", 100),
		TemporalTaskQueue: envStr("TEMPORAL_TASK_QUEUE", "inec-high-throughput"),

		APISIXAdminURL: envRequired("APISIX_ADMIN_URL"),
		APISIXAPIKey:   envRequired("APISIX_API_KEY"),

		OSAddrs:         strings.Split(envRequired("OPENSEARCH_ADDRS"), ","),
		OSBatchSize:     envInt("OS_BATCH_SIZE", 5000),
		OSFlushInterval: envInt("OS_FLUSH_INTERVAL_MS", 1000),
		OSWorkers:       envInt("OS_WORKERS", 8),

		DaprURL:       envRequired("DAPR_URL"),
		DaprBatchSize: envInt("DAPR_BATCH_SIZE", 1000),

		PermifyURL:       envRequired("PERMIFY_URL"),
		PermifyCacheSize: envInt("PERMIFY_CACHE_SIZE", 100000),
		PermifyCacheTTL:  envInt("PERMIFY_CACHE_TTL_SEC", 30),

		FluvioURL:     envRequired("FLUVIO_STREAM_URL"),
		FluvioWorkers: envInt("FLUVIO_WORKERS", 16),
	}
}

func envRequired(key string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	panic(fmt.Sprintf("%s is required", key))
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1"
	}
	return def
}
