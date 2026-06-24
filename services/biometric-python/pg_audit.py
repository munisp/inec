"""PostgreSQL audit persistence for the biometric processing service.

All processing operations are logged to PostgreSQL for durability.
No in-memory state is retained between requests — the service is stateless.
"""

from __future__ import annotations

import os
import time
import uuid
from contextlib import asynccontextmanager
from typing import Optional

import asyncpg
import structlog

log = structlog.get_logger()

_pool: Optional[asyncpg.Pool] = None


async def init_pool() -> asyncpg.Pool:
    """Initialize the PostgreSQL connection pool."""
    global _pool
    database_url = os.getenv(
        "DATABASE_URL", "postgresql://ngapp:ngapp123@localhost:5432/ngapp"
    )
    _pool = await asyncpg.create_pool(
        database_url,
        min_size=5,
        max_size=50,
        command_timeout=10,
    )

    # Ensure table exists
    async with _pool.acquire() as conn:
        await conn.execute("""
            CREATE TABLE IF NOT EXISTS biometric_processing_log (
                id TEXT PRIMARY KEY,
                operation TEXT NOT NULL,
                modality TEXT NOT NULL,
                voter_vin TEXT,
                quality_score DOUBLE PRECISION,
                minutiae_count INTEGER,
                embedding_dim INTEGER,
                iris_code_bits INTEGER,
                processing_ms DOUBLE PRECISION NOT NULL,
                success BOOLEAN NOT NULL DEFAULT TRUE,
                error_detail TEXT,
                created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
            );
            CREATE INDEX IF NOT EXISTS idx_processing_log_time
                ON biometric_processing_log (created_at DESC);
            CREATE INDEX IF NOT EXISTS idx_processing_log_voter
                ON biometric_processing_log (voter_vin);
        """)

    log.info("pg_audit_initialized", pool_size=50)
    return _pool


async def close_pool():
    """Close the PostgreSQL connection pool."""
    global _pool
    if _pool:
        await _pool.close()
        _pool = None


async def log_processing(
    operation: str,
    modality: str,
    processing_ms: float,
    *,
    voter_vin: str | None = None,
    quality_score: float | None = None,
    minutiae_count: int | None = None,
    embedding_dim: int | None = None,
    iris_code_bits: int | None = None,
    success: bool = True,
    error_detail: str | None = None,
):
    """Log a biometric processing operation to PostgreSQL."""
    if _pool is None:
        return

    entry_id = f"proc-{uuid.uuid4()}"
    try:
        async with _pool.acquire() as conn:
            await conn.execute(
                """
                INSERT INTO biometric_processing_log
                    (id, operation, modality, voter_vin, quality_score,
                     minutiae_count, embedding_dim, iris_code_bits,
                     processing_ms, success, error_detail)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
                """,
                entry_id,
                operation,
                modality,
                voter_vin,
                quality_score,
                minutiae_count,
                embedding_dim,
                iris_code_bits,
                processing_ms,
                success,
                error_detail,
            )
    except Exception as e:
        log.error("pg_audit_write_failed", error=str(e))


async def get_processing_stats() -> dict:
    """Get processing statistics from PostgreSQL."""
    if _pool is None:
        return {"error": "not connected"}

    async with _pool.acquire() as conn:
        total = await conn.fetchval("SELECT COUNT(*) FROM biometric_processing_log")
        success = await conn.fetchval(
            "SELECT COUNT(*) FROM biometric_processing_log WHERE success = TRUE"
        )
        avg_latency = await conn.fetchval(
            "SELECT AVG(processing_ms) FROM biometric_processing_log WHERE success = TRUE"
        )

        by_modality = await conn.fetch("""
            SELECT modality, COUNT(*) as count, AVG(processing_ms) as avg_ms
            FROM biometric_processing_log
            WHERE success = TRUE
            GROUP BY modality
        """)

    return {
        "total_operations": total,
        "successful": success,
        "failed": total - success,
        "avg_latency_ms": round(avg_latency, 2) if avg_latency else 0,
        "by_modality": {
            row["modality"]: {
                "count": row["count"],
                "avg_ms": round(row["avg_ms"], 2),
            }
            for row in by_modality
        },
        "persistence": "postgresql",
    }
