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
import hmac
import os
import time
from contextlib import asynccontextmanager
from typing import Optional

import structlog
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from prometheus_client import Counter, Gauge, Histogram, generate_latest

from dapr_bulk_processor import DaprBulkProcessor
from kafka_arrow_consumer import KafkaArrowConsumer
from lakehouse_pipeline import LakehousePipeline
from opensearch_parallel import OpenSearchParallelIndexer
from permify_optimizer import PermifyBatchOptimizer
from pg_copy_writer import PGCopyWriter
from redis_batch_processor import RedisBatchProcessor

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
    # SECURITY: no hardcoded DSN fallback — DATABASE_URL is mandatory and the
    # service fails fast at startup when it is unset.
    PG_DSN = os.getenv("DATABASE_URL", "").strip()
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

    # Kafka consumer is OPT-IN: KAFKA_ENABLED=true starts a real
    # confluent-kafka consumer on KAFKA_TOPICS. When disabled (default) the
    # service only serves the ingest API — no simulated consume loop runs.
    KAFKA_ENABLED = os.getenv("KAFKA_ENABLED", "").strip().lower() in ("1", "true", "yes")
    KAFKA_TOPICS = [
        t.strip()
        for t in os.getenv("KAFKA_TOPICS", "inec.results.submitted,inec.ballots.cast").split(",")
        if t.strip()
    ]


# Global state
pipeline_engine: Optional["PipelineEngine"] = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global pipeline_engine
    cfg = Config()
    if not cfg.PG_DSN:
        raise RuntimeError(
            "DATABASE_URL is required for python-pipeline-optimizer; refusing to start"
        )
    pipeline_engine = PipelineEngine(cfg)
    await pipeline_engine.start()
    log.info("pipeline optimizer started", port=cfg.PORT, workers=cfg.WORKERS)
    yield
    await pipeline_engine.stop()


# SECURITY: in production the interactive docs/OpenAPI schema are disabled —
# they leak the full API surface to unauthenticated callers.
_PRODUCTION = os.getenv("APP_ENV", "development").strip().lower() == "production"

app = FastAPI(
    title="INEC Pipeline Optimizer",
    lifespan=lifespan,
    docs_url=None if _PRODUCTION else "/docs",
    redoc_url=None if _PRODUCTION else "/redoc",
    openapi_url=None if _PRODUCTION else "/openapi.json",
)


# ─── API-token authentication (fail closed) ──────────────────────────────────
# SECURITY: /api/v1/ingest (and every other non-health route) was previously
# unauthenticated — anyone could inject arbitrary batches into the election
# data pipeline. A shared bearer token is now mandatory; the service FAILS
# CLOSED (503) when PIPELINE_OPTIMIZER_API_TOKEN is unset.
# KEY ROTATION: comma-separated tokens; any constant-time match authenticates.
PIPELINE_API_KEYS: list[str] = [
    k.strip()
    for k in os.getenv("PIPELINE_OPTIMIZER_API_TOKEN", "").split(",")
    if k.strip()
]

_PUBLIC_PATHS = ("/health",)


@app.middleware("http")
async def auth_middleware(request: Request, call_next):
    if request.url.path in _PUBLIC_PATHS:
        return await call_next(request)
    if not PIPELINE_API_KEYS:
        log.error("api_token_not_configured",
                  detail="PIPELINE_OPTIMIZER_API_TOKEN unset")
        return JSONResponse(
            status_code=503,
            content={"error": "PIPELINE_OPTIMIZER_API_TOKEN not configured; "
                              "refusing to serve unauthenticated requests"},
        )
    auth = request.headers.get("Authorization", "")
    bearer = auth[7:] if auth.lower().startswith("bearer ") else auth
    if not bearer or not any(
        hmac.compare_digest(bearer.encode(), key.encode()) for key in PIPELINE_API_KEYS
    ):
        return JSONResponse(status_code=401, content={"error": "authentication required"})
    return await call_next(request)


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
        # Keep strong references to every spawned task so they are not
        # garbage-collected mid-flight, and so stop() can cancel them.
        self._tasks: set[asyncio.Task] = set()

    def _spawn(self, coro) -> asyncio.Task:
        task = asyncio.create_task(coro)
        self._tasks.add(task)
        task.add_done_callback(self._tasks.discard)
        return task

    async def start(self):
        self._running = True
        # Start all pipeline components
        await self.lakehouse.initialize()
        await self.redis.connect()
        await self.pg.connect()

        # Start worker tasks
        for i in range(self.cfg.WORKERS):
            self._spawn(self._worker(i))

        # Start Kafka consumer (opt-in). Previously this spawned a SIMULATED
        # consume loop that never connected to Kafka — pure theater. Now a
        # real confluent-kafka consumer runs only when explicitly enabled.
        if self.cfg.KAFKA_ENABLED:
            self._spawn(self.kafka.consume(self._queue))
            log.info("kafka consumer ENABLED", brokers=self.cfg.KAFKA_BROKERS,
                     topics=self.cfg.KAFKA_TOPICS)
        else:
            log.info("kafka consumer DISABLED — set KAFKA_ENABLED=true to "
                     "consume from Kafka; the /api/v1/ingest API remains available")

    async def stop(self):
        self._running = False
        for task in list(self._tasks):
            task.cancel()
        if self._tasks:
            await asyncio.gather(*self._tasks, return_exceptions=True)
        self._tasks.clear()
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
    # Fail closed: Flask-style tuple returns are not honored by FastAPI (they
    # serialize as a 200 JSON array), so use an explicit JSONResponse.
    from fastapi.responses import JSONResponse
    return JSONResponse(status_code=503, content={"error": "engine not ready"})


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=Config.PORT, workers=1, loop="uvloop")
