"""Dapr Bulk Processor — Batch pub/sub and state operations.

Key optimizations:
- Bulk publish API (1000 events per HTTP call vs 1 event per call)
- Batch state save (N state items in one transactional call)
- Topic-grouped publishing (one bulk call per topic)
- Connection pooling via httpx
"""

import asyncio
import time
from typing import Any

import httpx
import orjson
import structlog

log = structlog.get_logger()


class DaprBulkProcessor:
    """High-throughput Dapr client using bulk APIs."""
    
    def __init__(self, cfg):
        self.cfg = cfg
        self.base_url = cfg.DAPR_URL
        self.batch_size = cfg.DAPR_BATCH_SIZE
        self.client = httpx.AsyncClient(
            base_url=self.base_url,
            timeout=10.0,
            limits=httpx.Limits(max_connections=200, max_keepalive_connections=100),
        )
        self.total_published = 0
        self.total_state_ops = 0
    
    async def publish_batch(self, transactions: list[dict]):
        """Publish batch of transactions using Dapr bulk publish API.
        
        Groups by topic and sends one bulk request per topic.
        1000 events per call = 1000x fewer HTTP roundtrips.
        """
        if not transactions:
            return
        
        # Group by type → topic
        topic_groups: dict[str, list[dict]] = {}
        for tx in transactions:
            topic = self._topic_for_type(tx.get("type", ""))
            if topic not in topic_groups:
                topic_groups[topic] = []
            topic_groups[topic].append(tx)
        
        # Bulk publish each topic group
        tasks = []
        for topic, events in topic_groups.items():
            # Chunk into batch_size
            for i in range(0, len(events), self.batch_size):
                chunk = events[i:i + self.batch_size]
                tasks.append(self._bulk_publish(topic, chunk))
        
        await asyncio.gather(*tasks, return_exceptions=True)
    
    async def _bulk_publish(self, topic: str, events: list[dict]):
        """Single bulk publish API call."""
        entries = [
            {
                "entryId": f"{int(time.time_ns())}-{i}",
                "event": event,
                "contentType": "application/json",
            }
            for i, event in enumerate(events)
        ]
        
        try:
            resp = await self.client.post(
                f"/v1.0-alpha1/publish/bulk/inec-pubsub/{topic}",
                content=orjson.dumps(entries),
                headers={"Content-Type": "application/json"},
            )
            if resp.status_code in (200, 204):
                self.total_published += len(entries)
        except Exception as e:
            log.warn("dapr bulk publish failed", topic=topic, count=len(entries), error=str(e))
    
    async def batch_save_state(self, store_name: str, items: list[tuple[str, Any]]):
        """Save multiple state items in one transactional call.
        
        Uses Dapr state management API with batch operations.
        """
        state_items = [
            {"key": key, "value": orjson.dumps(value).decode()}
            for key, value in items
        ]
        
        try:
            resp = await self.client.post(
                f"/v1.0/state/{store_name}",
                content=orjson.dumps(state_items),
                headers={"Content-Type": "application/json"},
            )
            if resp.status_code in (200, 204):
                self.total_state_ops += len(state_items)
        except Exception as e:
            log.warn("dapr batch state save failed", store=store_name, error=str(e))
    
    async def invoke_service(self, app_id: str, method: str, data: Any) -> Any:
        """Service invocation via Dapr sidecar (avoids service discovery overhead)."""
        try:
            resp = await self.client.post(
                f"/v1.0/invoke/{app_id}/method/{method}",
                content=orjson.dumps(data),
                headers={"Content-Type": "application/json"},
            )
            if resp.status_code == 200:
                return orjson.loads(resp.content)
        except Exception as e:
            log.warn("dapr invoke failed", app_id=app_id, method=method, error=str(e))
        return None
    
    def _topic_for_type(self, tx_type: str) -> str:
        mapping = {
            "result_submission": "inec.results.submitted",
            "ballot_cast": "inec.ballots.cast",
            "incident": "inec.incidents.reported",
            "accreditation": "inec.accreditation.events",
            "collation": "inec.collation.updates",
        }
        return mapping.get(tx_type, "inec.events.general")
    
    def stats(self) -> dict:
        return {
            "total_published": self.total_published,
            "total_state_ops": self.total_state_ops,
        }
