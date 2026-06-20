"""INEC Pipeline Optimizer — High-throughput analytics engine.

Optimizes Lakehouse, Permify, Dapr, Kafka, Redis, Postgres, and OpenSearch
for millions of transactions per second using:
- Apache Arrow columnar format (zero-copy, vectorized operations)
- DuckDB for in-process analytical queries (no network overhead)
- Polars for parallel DataFrame operations (Rust-backed)
- Async batch processing with backpressure
- Connection pooling and pipeline multiplexing
"""

import asyncio
import os
import time
from contextlib import asynccontextmanager
from typing import Optional

import duckdb
import orjson
import structlog
from fastapi import FastAPI
from prometheus_client import Counter, Gauge, Histogram, generate_latest

from lakehouse_pipeline import LakehousePipeline
from permify_optimizer import PermifyBatchOptimizer
from dapr_bulk_processor import DaprBulkProcessor
from kafka_arrow_consumer import KafkaArrowConsumer
from redis_batch_processor import RedisBatchProcessor
from pg_copy_writer import PGCopyWriter
from opensearch_parallel import OpenSearchParallelIndexer

log = structlog.get_logger()

# Metrics
TRANSACTIONS_PROCESSED = Counter("inec_py_transactions_processed_total", "Total transactions processed")
PROCESSING_LATENCY = Histogram("inec_py_processing_latency_seconds", "Processing latency",
                                buckets=[0.001, 0.005, 0.01, 0.05, 0.1, 0.5])
CURRENT_TPS = Gauge("inec_py_current_tps", "Current transactions per second")
PIPELINE_QUEUE_DEPTH = Gauge("inec_py_queue_depth", "Pipeline queue depth")


class Config:
    """Configuration from environment variables."""
    PORT = int(os.getenv("PORT", "9092"))
    
    # Kafka
    KAFKA_BROKERS = os.getenv("KAFKA_BROKERS", "localhost:9092")
    KAFKA_GROUP_ID = os.getenv("KAFKA_GROUP_ID", "inec-py-pipeline")
    KAFKA_BATCH_SIZE = int(os.getenv("KAFKA_BATCH_SIZE", "50000"))
    KAFKA_BATCH_TIMEOUT_MS = int(os.getenv("KAFKA_BATCH_TIMEOUT_MS", "100"))
    
    # Redis
    REDIS_URL = os.getenv("REDIS_URL", "redis://localhost:6379")
    REDIS_PIPELINE_SIZE = int(os.getenv("REDIS_PIPELINE_SIZE", "5000"))
    REDIS_POOL_SIZE = int(os.getenv("REDIS_POOL_SIZE", "100"))
    
    # Postgres
    PG_DSN = os.getenv("DATABASE_URL", "postgresql://ngapp:ngapp123@localhost:5432/ngapp")
    PG_POOL_SIZE = int(os.getenv("PG_POOL_SIZE", "50"))
    PG_BATCH_SIZE = int(os.getenv("PG_BATCH_SIZE", "10000"))
    
    # DuckDB Lakehouse
    DUCKDB_PATH = os.getenv("DUCKDB_PATH", "/tmp/inec_lakehouse.duckdb")
    PARQUET_OUTPUT = os.getenv("PARQUET_OUTPUT", "/tmp/inec_lakehouse/")
    
    # OpenSearch
    OPENSEARCH_URL = os.getenv("OPENSEARCH_URL", "http://localhost:9200")
    OS_BATCH_SIZE = int(os.getenv("OS_BATCH_SIZE", "10000"))
    OS_WORKERS = int(os.getenv("OS_WORKERS", "8"))
    
    # Permify
    PERMIFY_URL = os.getenv("PERMIFY_URL", "http://localhost:3476")
    PERMIFY_CACHE_SIZE = int(os.getenv("PERMIFY_CACHE_SIZE", "100000"))
    PERMIFY_CACHE_TTL = int(os.getenv("PERMIFY_CACHE_TTL", "30"))
    
    # Dapr
    DAPR_URL = os.getenv("DAPR_URL", "http://localhost:3500")
    DAPR_BATCH_SIZE = int(os.getenv("DAPR_BATCH_SIZE", "1000"))
    
    # Pipeline
    WORKERS = int(os.getenv("PIPELINE_WORKERS", "16"))
    QUEUE_SIZE = int(os.getenv("PIPELINE_QUEUE_SIZE", "1000000"))


# Global state
pipeline_engine: Optional["PipelineEngine"] = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global pipeline_engine
    cfg = Config()
    pipeline_engine = PipelineEngine(cfg)
    await pipeline_engine.start()
    log.info("pipeline optimizer started", port=cfg.PORT, workers=cfg.WORKERS)
    yield
    await pipeline_engine.stop()


app = FastAPI(title="INEC Pipeline Optimizer", lifespan=lifespan)


class PipelineEngine:
    """Orchestrates all optimization pipelines."""
    
    def __init__(self, cfg: Config):
        self.cfg = cfg
        self.start_time = time.time()
        self.total_processed = 0
        self.total_errors = 0
        
        self.lakehouse = LakehousePipeline(cfg)
        self.permify = PermifyBatchOptimizer(cfg)
        self.dapr = DaprBulkProcessor(cfg)
        self.kafka = KafkaArrowConsumer(cfg)
        self.redis = RedisBatchProcessor(cfg)
        self.pg = PGCopyWriter(cfg)
        self.opensearch = OpenSearchParallelIndexer(cfg)
        
        self._queue: asyncio.Queue = asyncio.Queue(maxsize=cfg.QUEUE_SIZE)
        self._running = False
    
    async def start(self):
        self._running = True
        # Start all pipeline components
        await self.lakehouse.initialize()
        await self.redis.connect()
        await self.pg.connect()
        
        # Start worker tasks
        for i in range(self.cfg.WORKERS):
            asyncio.create_task(self._worker(i))
        
        # Start Kafka consumer
        asyncio.create_task(self.kafka.consume(self._queue))
    
    async def stop(self):
        self._running = False
        await self.redis.close()
        await self.pg.close()
    
    async def _worker(self, worker_id: int):
        """Process batches from queue through all pipelines."""
        while self._running:
            try:
                batch = await asyncio.wait_for(self._queue.get(), timeout=1.0)
            except asyncio.TimeoutError:
                continue
            
            start = time.perf_counter()
            
            try:
                # Fan-out to all sinks in parallel
                await asyncio.gather(
                    self.lakehouse.ingest_batch(batch),
                    self.redis.process_batch(batch),
                    self.pg.copy_batch(batch),
                    self.opensearch.bulk_index(batch),
                    self.dapr.publish_batch(batch),
                    return_exceptions=True,
                )
                
                elapsed = time.perf_counter() - start
                count = len(batch)
                self.total_processed += count
                TRANSACTIONS_PROCESSED.inc(count)
                PROCESSING_LATENCY.observe(elapsed)
                
            except Exception as e:
                self.total_errors += 1
                log.error("worker error", worker_id=worker_id, error=str(e))
    
    def stats(self) -> dict:
        uptime = time.time() - self.start_time
        tps = self.total_processed / uptime if uptime > 0 else 0
        CURRENT_TPS.set(tps)
        PIPELINE_QUEUE_DEPTH.set(self._queue.qsize())
        
        return {
            "uptime_sec": round(uptime, 2),
            "total_processed": self.total_processed,
            "total_errors": self.total_errors,
            "current_tps": round(tps),
            "queue_depth": self._queue.qsize(),
            "queue_capacity": self.cfg.QUEUE_SIZE,
            "components": {
                "lakehouse": self.lakehouse.stats(),
                "permify": self.permify.stats(),
                "redis": self.redis.stats(),
                "pg": self.pg.stats(),
                "opensearch": self.opensearch.stats(),
            },
        }


@app.get("/health")
async def health():
    return {"status": "healthy"}


@app.get("/metrics")
async def metrics():
    return generate_latest().decode()


@app.get("/stats")
async def stats():
    if pipeline_engine:
        return pipeline_engine.stats()
    return {"status": "not_started"}


@app.post("/api/v1/ingest")
async def ingest(batch: list[dict]):
    if pipeline_engine:
        await pipeline_engine._queue.put(batch)
        return {"status": "accepted", "count": len(batch)}
    return {"error": "engine not ready"}, 503


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=Config.PORT, workers=1, loop="uvloop")
