FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1

WORKDIR /app

# Install dependencies first for layer caching
COPY pyproject.toml ./
RUN pip install --no-cache-dir \
    "fastapi>=0.115.0" \
    "uvicorn[standard]>=0.32.0" \
    "duckdb>=1.1.0" \
    "pyarrow>=18.0.0" \
    "polars>=1.14.0" \
    "numpy>=2.0.0" \
    "httpx>=0.27.0" \
    "redis[hiredis]>=5.2.0" \
    "confluent-kafka>=2.6.0" \
    "structlog>=24.4.0" \
    "prometheus-client>=0.21.0" \
    "asyncpg>=0.30.0" \
    "orjson>=3.10.0" \
    "cachetools>=5.5.0" \
    "aiokafka>=0.12.0"

COPY main.py ./
COPY dapr_bulk_processor.py kafka_arrow_consumer.py lakehouse_pipeline.py \
     opensearch_parallel.py permify_optimizer.py pg_copy_writer.py \
     redis_batch_processor.py ./

RUN groupadd -r app && useradd -r -g app app && chown -R app:app /app
USER app

EXPOSE 9092
CMD ["python", "main.py"]
