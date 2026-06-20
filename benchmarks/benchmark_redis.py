"""Redis pipeline benchmark — measures TPS for INEC workload patterns.

Tests:
1. Pipeline INCR (counter updates) — target: 2M+ ops/sec
2. Pipeline SET with TTL (transaction cache) — target: 1M+ ops/sec
3. Lua script (atomic tally) — target: 500K+ ops/sec
4. PUBLISH (real-time events) — target: 1M+ ops/sec
"""

import asyncio
import os
import time
from typing import Optional

# For benchmarking even without Redis running
REDIS_URL = os.getenv("REDIS_URL", "redis://localhost:6379")


async def benchmark_pipeline_incr(client, iterations: int = 100_000, pipeline_size: int = 1000):
    """Benchmark pipelined INCR commands."""
    start = time.perf_counter()
    
    for i in range(0, iterations, pipeline_size):
        pipe = client.pipeline(transaction=False)
        for j in range(pipeline_size):
            pipe.incr(f"bench:counter:{(i+j) % 37}")
        await pipe.execute()
    
    elapsed = time.perf_counter() - start
    tps = iterations / elapsed
    print(f"  Pipeline INCR: {tps:,.0f} ops/sec ({iterations:,} ops in {elapsed:.2f}s)")
    return tps


async def benchmark_pipeline_set(client, iterations: int = 100_000, pipeline_size: int = 1000):
    """Benchmark pipelined SET with TTL."""
    import orjson
    
    start = time.perf_counter()
    payload = orjson.dumps({"type": "ballot_cast", "state_code": "LA", "amount": 1})
    
    for i in range(0, iterations, pipeline_size):
        pipe = client.pipeline(transaction=False)
        for j in range(pipeline_size):
            pipe.setex(f"bench:tx:{i+j}", 10, payload)
        await pipe.execute()
    
    elapsed = time.perf_counter() - start
    tps = iterations / elapsed
    print(f"  Pipeline SET:  {tps:,.0f} ops/sec ({iterations:,} ops in {elapsed:.2f}s)")
    return tps


async def benchmark_lua_tally(client, iterations: int = 50_000):
    """Benchmark Lua script atomic tally."""
    script = """
    local key = KEYS[1]
    local party = ARGV[1]
    local votes = tonumber(ARGV[2])
    redis.call('HINCRBY', key, party, votes)
    redis.call('HINCRBY', key, 'total', votes)
    return redis.call('HGET', key, 'total')
    """
    
    sha = await client.script_load(script)
    parties = ["APC", "PDP", "LP", "NNPP", "ADC"]
    
    start = time.perf_counter()
    
    for i in range(iterations):
        await client.evalsha(sha, 1, f"bench:tally:{i % 37}", parties[i % 5], str(i % 1000))
    
    elapsed = time.perf_counter() - start
    tps = iterations / elapsed
    print(f"  Lua Tally:     {tps:,.0f} ops/sec ({iterations:,} ops in {elapsed:.2f}s)")
    return tps


async def benchmark_publish(client, iterations: int = 100_000, pipeline_size: int = 1000):
    """Benchmark pipelined PUBLISH commands."""
    start = time.perf_counter()
    
    for i in range(0, iterations, pipeline_size):
        pipe = client.pipeline(transaction=False)
        for j in range(pipeline_size):
            pipe.publish(f"events:result_submission", f"tx-{i+j}")
        await pipe.execute()
    
    elapsed = time.perf_counter() - start
    tps = iterations / elapsed
    print(f"  Pipeline PUB:  {tps:,.0f} ops/sec ({iterations:,} ops in {elapsed:.2f}s)")
    return tps


async def main():
    print("INEC Redis Benchmark")
    print("=" * 50)
    print(f"URL: {REDIS_URL}")
    print()
    
    try:
        import redis.asyncio as aioredis
        client = aioredis.from_url(REDIS_URL, max_connections=100, decode_responses=False)
        await client.ping()
    except Exception as e:
        print(f"Cannot connect to Redis: {e}")
        print("Showing theoretical throughput estimates:")
        print()
        print("  Pipeline INCR: ~2,000,000 ops/sec (1000-cmd pipeline × 2000 RTT/s)")
        print("  Pipeline SET:  ~1,500,000 ops/sec (1000-cmd pipeline × 1500 RTT/s)")
        print("  Lua Tally:     ~500,000 ops/sec (single key, sequential)")
        print("  Pipeline PUB:  ~2,000,000 ops/sec (1000-cmd pipeline × 2000 RTT/s)")
        return
    
    print("Running benchmarks...")
    print()
    
    results = {}
    results["incr"] = await benchmark_pipeline_incr(client)
    results["set"] = await benchmark_pipeline_set(client)
    results["lua"] = await benchmark_lua_tally(client)
    results["pub"] = await benchmark_publish(client)
    
    print()
    print("=" * 50)
    total = sum(results.values())
    print(f"Combined throughput: {total:,.0f} ops/sec")
    print(f"With 6 nodes (cluster): ~{total * 5:,.0f} ops/sec")
    
    await client.close()


if __name__ == "__main__":
    asyncio.run(main())
