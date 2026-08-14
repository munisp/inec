FROM python:3.12-slim AS builder

WORKDIR /build

# R4-39d: distro-managed pins — package versions are bounded by the pinned
# base-image tag; security-sensitive packages (ca-certificates/openssl/libssl) are
# listed explicitly so they receive distro security updates on rebuild. See docs/SUPPLY_CHAIN_PINS.md.
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc g++ libgl1 libglib2.0-0 libsm6 libxrender1 libxext6 curl \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir --prefix=/install -r requirements.txt

FROM python:3.12-slim

# R4-39d: distro-managed pins — package versions are bounded by the pinned
# base-image tag; security-sensitive packages (ca-certificates/openssl/libssl) are
# listed explicitly so they receive distro security updates on rebuild. See docs/SUPPLY_CHAIN_PINS.md.
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1 libglib2.0-0 libsm6 libxrender1 libxext6 \
    && rm -rf /var/lib/apt/lists/* \
    && addgroup --system app && adduser --system --ingroup app app

COPY --from=builder /install /usr/local

WORKDIR /app
COPY training/ training/
COPY serving/ serving/
COPY configs/ configs/
RUN mkdir -p models data/processed && chown -R app:app /app

USER app
EXPOSE 8090

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8090/health')" || exit 1

CMD ["python", "-m", "uvicorn", "serving.inference_server:app", "--host", "0.0.0.0", "--port", "8090"]
