"""GOTV Analytics Middleware Integration — connects the Python analytics service
to Kafka, Redis, OpenSearch, Lakehouse/Trino, Dapr, Fluvio, and Temporal.

Each middleware is optional: if the env var is not set, calls are no-ops.
"""

import json
import os
import time
from typing import Any, Optional

import httpx
import structlog

logger = structlog.get_logger()

# ─── Kafka Integration ──────────────────────────────────────────────────────
# Publishes analytics events: model retraining, scoring completions, anomalies.

KAFKA_REST_URL = os.getenv("KAFKA_REST_URL")


def publish_kafka(topic: str, key: str, payload: dict, retries: int = 3) -> None:
    """Publish an event to Kafka via REST proxy with retry."""
    if not KAFKA_REST_URL:
        logger.debug("gotv_analytics_event", topic=topic, key=key, payload=payload)
        return
    last_err = None
    for attempt in range(retries):
        try:
            httpx.post(
                f"{KAFKA_REST_URL}/produce",
                json={"topic": topic, "key": key, "value": payload},
                timeout=5.0,
            )
            return
        except Exception as e:
            last_err = e
            time.sleep(0.1 * (2 ** attempt))
    logger.warning("kafka_publish_failed", topic=topic, attempts=retries, error=str(last_err))


# ─── Redis Integration ──────────────────────────────────────────────────────
# Caches ML model predictions, scoring results, and aggregated analytics.

REDIS_URL = os.getenv("REDIS_URL")
_redis_client = None


def _get_redis():
    global _redis_client
    if _redis_client is None and REDIS_URL:
        try:
            import redis
            _redis_client = redis.Redis.from_url(
                f"redis://{REDIS_URL}/3",  # DB 3 for analytics
                decode_responses=True,
                socket_timeout=3,
            )
            _redis_client.ping()
            logger.info("gotv_redis_connected", url=REDIS_URL)
        except Exception as e:
            logger.warning("gotv_redis_failed", error=str(e))
            _redis_client = None
    return _redis_client


def cache_get(key: str) -> Optional[str]:
    r = _get_redis()
    if r is None:
        return None
    try:
        return r.get(f"gotv-analytics:{key}")
    except Exception:
        return None


def cache_set(key: str, value: Any, ttl: int = 300) -> None:
    r = _get_redis()
    if r is None:
        return
    try:
        r.setex(f"gotv-analytics:{key}", ttl, json.dumps(value))
    except Exception:
        pass


def cache_invalidate(pattern: str) -> None:
    r = _get_redis()
    if r is None:
        return
    try:
        keys = r.keys(f"gotv-analytics:{pattern}*")
        if keys:
            r.delete(*keys)
    except Exception:
        pass


# ─── OpenSearch Integration ─────────────────────────────────────────────────
# Full-text search and analytics aggregations on GOTV data.

OPENSEARCH_URL = os.getenv("OPENSEARCH_URL")


def search_opensearch(index: str, query: str, party_id: int, size: int = 50) -> list[dict]:
    """Search documents in OpenSearch."""
    if not OPENSEARCH_URL:
        return []
    try:
        body = {
            "query": {
                "bool": {
                    "must": [
                        {"multi_match": {"query": query, "fields": ["name", "state", "lga", "role", "tags"]}},
                        {"term": {"party_id": party_id}},
                    ]
                }
            },
            "size": size,
        }
        resp = httpx.post(f"{OPENSEARCH_URL}/{index}/_search", json=body, timeout=10.0)
        result = resp.json()
        return [hit["_source"] for hit in result.get("hits", {}).get("hits", [])]
    except Exception as e:
        logger.warning("opensearch_search_failed", index=index, error=str(e))
        return []


def aggregate_opensearch(index: str, party_id: int, field: str) -> dict:
    """Run an aggregation query on OpenSearch."""
    if not OPENSEARCH_URL:
        return {}
    try:
        body = {
            "query": {"term": {"party_id": party_id}},
            "size": 0,
            "aggs": {
                "by_field": {"terms": {"field": field, "size": 100}},
            },
        }
        resp = httpx.post(f"{OPENSEARCH_URL}/{index}/_search", json=body, timeout=10.0)
        result = resp.json()
        buckets = result.get("aggregations", {}).get("by_field", {}).get("buckets", [])
        return {b["key"]: b["doc_count"] for b in buckets}
    except Exception:
        return {}


# ─── Lakehouse/Trino Integration ────────────────────────────────────────────
# Analytical queries for campaign performance, turnout trends.

LAKEHOUSE_URL = os.getenv("LAKEHOUSE_URL")


FORBIDDEN_SQL_KEYWORDS = {"DROP", "DELETE", "INSERT", "UPDATE", "ALTER", "TRUNCATE", "EXEC", "--", ";"}


def query_lakehouse(sql: str) -> list[dict]:
    """Run an analytical query against the Lakehouse/Trino cluster.
    Only SELECT queries are allowed to prevent injection."""
    if not LAKEHOUSE_URL:
        return []
    trimmed = sql.strip().upper()
    if not trimmed.startswith("SELECT"):
        logger.warning("lakehouse_query_rejected", reason="not a SELECT")
        return []
    for kw in FORBIDDEN_SQL_KEYWORDS:
        if kw in trimmed:
            logger.warning("lakehouse_query_rejected", keyword=kw)
            return []
    try:
        resp = httpx.post(f"{LAKEHOUSE_URL}/query", json={"query": sql}, timeout=30.0)
        result = resp.json()
        return result.get("data", [])
    except Exception as e:
        logger.warning("lakehouse_query_failed", error=str(e))
        return []


# ─── Dapr Integration ───────────────────────────────────────────────────────
# Service-to-service calls to gotv-svc (Go) and gotv-engine (Rust).

DAPR_HTTP_PORT = os.getenv("DAPR_HTTP_PORT")


def dapr_invoke(app_id: str, method: str, payload: dict = None) -> Optional[dict]:
    """Invoke a service via Dapr sidecar or direct HTTP."""
    if DAPR_HTTP_PORT:
        url = f"http://localhost:{DAPR_HTTP_PORT}/v1.0/invoke/{app_id}/method/{method}"
    else:
        ports = {"gotv-svc": "8103", "gotv-engine": "8101"}
        port = ports.get(app_id)
        if not port:
            return None
        url = f"http://localhost:{port}/{method}"
    try:
        resp = httpx.post(url, json=payload or {}, timeout=10.0)
        return resp.json()
    except Exception as e:
        logger.warning("dapr_invoke_failed", app_id=app_id, method=method, error=str(e))
        return None


# ─── Fluvio Integration ────────────────────────────────────────────────────
# Stream analytics events for real-time dashboard updates.

FLUVIO_URL = os.getenv("FLUVIO_URL")


def stream_fluvio(topic: str, payload: dict) -> None:
    """Stream an event to Fluvio."""
    if not FLUVIO_URL:
        return
    try:
        httpx.post(
            f"{FLUVIO_URL}/produce",
            json={"topic": topic, "key": "gotv-analytics", "payload": json.dumps(payload)},
            timeout=5.0,
        )
    except Exception:
        pass


# ─── Temporal Integration ───────────────────────────────────────────────────
# Trigger analytical workflow runs for model retraining.

TEMPORAL_URL = os.getenv("TEMPORAL_FRONTEND_URL")


def start_workflow(workflow_id: str, workflow_type: str, params: dict) -> bool:
    """Start a Temporal workflow for model retraining or batch analytics."""
    if not TEMPORAL_URL:
        return False
    try:
        payload = {
            "workflow_id": workflow_id,
            "workflow_type": workflow_type,
            "task_queue": "gotv-analytics",
            "namespace": "gotv",
            **params,
        }
        resp = httpx.post(
            f"{TEMPORAL_URL}/api/v1/namespaces/gotv/workflows",
            json=payload,
            timeout=10.0,
        )
        return resp.status_code < 400
    except Exception:
        return False


# ─── Middleware Status ──────────────────────────────────────────────────────

def middleware_status() -> dict:
    """Report connectivity status of all middleware services."""
    redis_ok = _get_redis() is not None
    status = {
        "kafka": bool(KAFKA_REST_URL),
        "redis": redis_ok,
        "opensearch": bool(OPENSEARCH_URL),
        "lakehouse": bool(LAKEHOUSE_URL),
        "dapr": bool(DAPR_HTTP_PORT),
        "fluvio": bool(FLUVIO_URL),
        "temporal": bool(TEMPORAL_URL),
        "postgresql": True,
    }
    connected = sum(1 for v in status.values() if v)
    return {
        "service": "gotv-analytics",
        "language": "python",
        "middleware": status,
        "connected": connected,
        "total": len(status),
    }
