"""Redis Batch Processor — Pipeline multiplexing for 2M+ ops/sec.

Key optimizations:
- Pipeline batching (5000 commands per flush)
- hiredis C parser (10x faster than pure Python)
- Connection pool (100 connections)
- Lua scripting for atomic multi-key operations
- orjson serialization (3x faster than json)
"""

import asyncio
import time
from typing import Optional

import orjson
import redis.asyncio as aioredis
import structlog

log = structlog.get_logger()

# Lua script for atomic vote tally update
TALLY_SCRIPT = """
local key = KEYS[1]
local party = ARGV[1]
local votes = tonumber(ARGV[2])
redis.call('HINCRBY', key, party, votes)
redis.call('HINCRBY', key, 'total', votes)
redis.call('EXPIRE', key, 3600)
return redis.call('HGET', key, 'total')
"""

# Lua script for rate limiting (sliding window)
RATE_LIMIT_SCRIPT = """
local key = KEYS[1]
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local count = redis.call('ZCARD', key)
if count < limit then
    redis.call('ZADD', key, now, now .. '-' .. math.random(1000000))
    redis.call('EXPIRE', key, window / 1000)
    return 1
end
return 0
"""


class RedisBatchProcessor:
    """High-throughput Redis client using pipeline multiplexing."""
    
    def __init__(self, cfg):
        self.cfg = cfg
        self.redis_url = cfg.REDIS_URL
        self.pipeline_size = cfg.REDIS_PIPELINE_SIZE
        self.pool_size = cfg.REDIS_POOL_SIZE
        self.client: Optional[aioredis.Redis] = None
        self.tally_sha: Optional[str] = None
        self.rate_limit_sha: Optional[str] = None
        
        self.total_commands = 0
        self.total_pipelines = 0
    
    async def connect(self):
        """Connect with optimized pool settings."""
        self.client = aioredis.from_url(
            self.redis_url,
            max_connections=self.pool_size,
            decode_responses=False,  # raw bytes (faster)
            socket_timeout=2.0,
            socket_connect_timeout=5.0,
            retry_on_timeout=True,
        )
        
        # Pre-load Lua scripts (execute by SHA, not full script text)
        self.tally_sha = await self.client.script_load(TALLY_SCRIPT)
        self.rate_limit_sha = await self.client.script_load(RATE_LIMIT_SCRIPT)
        
        log.info("redis connected", url=self.redis_url, pool_size=self.pool_size)
    
    async def process_batch(self, transactions: list[dict]):
        """Process batch through Redis pipeline.
        
        Each transaction generates 4 commands:
        1. SET tx:{id} → serialized tx (10s TTL for dashboard)
        2. INCR counter:state:{state}:{type}
        3. INCR counter:total:{type}
        4. PUBLISH events:{type} → real-time notification
        
        Pipeline batching: 5000 commands in ONE network RTT.
        """
        if not self.client or not transactions:
            return
        
        # Process in pipeline chunks
        for i in range(0, len(transactions), self.pipeline_size):
            chunk = transactions[i:i + self.pipeline_size]
            pipe = self.client.pipeline(transaction=False)
            
            for tx in chunk:
                tx_id = tx.get("id", "")
                tx_type = tx.get("type", "")
                state_code = tx.get("state_code", "")
                
                # Cache full transaction (10s TTL)
                pipe.setex(f"tx:{tx_id}", 10, orjson.dumps(tx))
                
                # Increment counters
                pipe.incr(f"counter:state:{state_code}:{tx_type}")
                pipe.incr(f"counter:total:{tx_type}")
                
                # Publish for real-time subscribers
                pipe.publish(f"events:{tx_type}", tx_id)
            
            await pipe.execute()
            self.total_commands += len(chunk) * 4
            self.total_pipelines += 1
    
    async def atomic_tally(self, state_code: str, party: str, votes: int) -> int:
        """Atomic vote tally update using pre-loaded Lua script."""
        if not self.client or not self.tally_sha:
            return 0
        
        result = await self.client.evalsha(
            self.tally_sha,
            1,  # number of keys
            f"tally:{state_code}",  # KEYS[1]
            party,  # ARGV[1]
            str(votes),  # ARGV[2]
        )
        return int(result) if result else 0
    
    async def rate_limit_check(self, key: str, window_ms: int, limit: int) -> bool:
        """Sliding window rate limit check using Lua script."""
        if not self.client or not self.rate_limit_sha:
            return True
        
        now = int(time.time() * 1000)
        result = await self.client.evalsha(
            self.rate_limit_sha,
            1,
            f"ratelimit:{key}",
            str(window_ms),
            str(limit),
            str(now),
        )
        return result == 1
    
    async def close(self):
        if self.client:
            await self.client.close()
    
    def stats(self) -> dict:
        return {
            "total_commands": self.total_commands,
            "total_pipelines": self.total_pipelines,
            "pipeline_size": self.pipeline_size,
            "pool_size": self.pool_size,
        }
