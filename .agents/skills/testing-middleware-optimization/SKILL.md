---
name: testing-middleware-optimization
description: Test the INEC middleware optimization services (Go, Rust, Python) for compilation, runtime correctness, and configuration validation. Use when verifying changes to services/go-throughput-engine, services/rust-hot-path, or services/python-pipeline-optimizer.
---

# Testing Middleware Optimization Services

## Overview
Three backend services optimizing 12 middleware components for millions TPS. All testing is shell-based (no browser/GUI needed, no recording).

## Prerequisites

### Build Tools Required
- **Go 1.22+**: Located at `/usr/local/go/bin/go` (add to PATH: `export PATH="/usr/local/go/bin:$PATH"`)
- **Rust/Cargo**: Available via `~/.cargo/bin/cargo`
- **Python 3.12+**: Available via pyenv at `~/.pyenv/shims/python3`

### Python Dependencies
Install before testing Python service:
```bash
pip install duckdb pyarrow polars orjson httpx fastapi structlog pyyaml
```

### Rust Note
The `rdkafka` crate is an **optional feature** (requires `librdkafka-dev` + `cmake` at build time). Default `cargo check` compiles without it. To test with Kafka support: `sudo apt-get install -y cmake librdkafka-dev && cargo check --features kafka`

## Test Execution

### 1. Go Throughput Engine
```bash
export PATH="/usr/local/go/bin:$PATH"
cd services/go-throughput-engine
go build -o /tmp/go-throughput-engine .
# Expected: exit code 0, binary ~18MB
```

### 2. Rust Hot Path
```bash
cd services/rust-hot-path
cargo check
# Expected: exit code 0, warnings only (unused items are expected without live services)
```

### 3. Python Pipeline - Lakehouse
```python
# Verify DuckDB ingest + query pipeline
from lakehouse_pipeline import LakehousePipeline
# Initialize → ingest batch → SELECT COUNT(*) should match batch size
# query_state_totals() should return grouped results
```

### 4. Python Pipeline - Permify Cache
```python
# Verify LRU eviction + TTL expiration
from permify_optimizer import LRUTTLCache
# max_size=10: inserting 11th should evict LRU entry
# ttl_seconds=1: entries should return None after 1.1s sleep
```

### 5. Config YAML Validation
```python
import yaml
# Parse config/middleware-tuning.yaml
# Verify all 12 sections: kafka, redis, tigerbeetle, postgres, opensearch,
#   temporal, mojaloop, fluvio, dapr, permify, apisix, lakehouse
# Spot-check: kafka batch.size=1048576, redis pipeline_size=1000,
#   tigerbeetle batch_size_max=8190, apisix election_day count=1000000
```

### 6. Load Test Binary
```bash
export PATH="/usr/local/go/bin:$PATH"
cd benchmarks
go build -o /tmp/inec-loadtest .
/tmp/inec-loadtest -h
# Expected: shows -target, -workers, -duration, -batch-size flags
```

## Known Issues
- Go `kafka-go` v0.4.47 uses `kafka.Compression` type (not `compress.Codec`)
- Go files ending in `_test.go` are treated as test-only files by the Go toolchain
- Rust `rdkafka-sys` requires cmake + librdkafka C library for full compilation
- 40 "unused" warnings in Rust are expected (items used only when connected to live services)

## Not Testable Without Infrastructure
- Actual Kafka produce/consume (needs Kafka cluster)
- Redis pipeline throughput (needs Redis server)
- TigerBeetle transfers (needs TigerBeetle cluster)
- OpenSearch bulk indexing (needs OpenSearch)
- Mojaloop payment pipeline (needs Mojaloop hub)
- End-to-end TPS measurement (needs all services running)

## Devin Secrets Needed
None required for compilation/unit testing. For live infrastructure testing:
- `KAFKA_BROKERS` - Kafka bootstrap servers
- `REDIS_URL` - Redis connection string
- `DATABASE_URL` - PostgreSQL connection string
- `OPENSEARCH_URL` - OpenSearch endpoint
