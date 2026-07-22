FROM python:3.12-slim AS builder

WORKDIR /build

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc g++ libgl1 libglib2.0-0 libsm6 libxrender1 libxext6 \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN echo "torch==2.13.0+cpu" > /tmp/constraints.txt \
    && pip install --no-cache-dir --prefix=/install \
       --extra-index-url https://download.pytorch.org/whl/cpu \
       -c /tmp/constraints.txt -r requirements.txt

FROM python:3.12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1 libglib2.0-0 libsm6 libxrender1 libxext6 \
    && rm -rf /var/lib/apt/lists/* \
    && addgroup --system app && adduser --system --ingroup app app

COPY --from=builder /install /usr/local

WORKDIR /app
COPY main.py .
RUN mkdir -p /tmp/document-ai-uploads && chown -R app:app /app /tmp/document-ai-uploads

USER app
EXPOSE 8089

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8089/health')" || exit 1

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8089", "--workers", "2"]
