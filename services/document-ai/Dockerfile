FROM python:3.12-slim

WORKDIR /app

# System dependencies for OpenCV + PaddleOCR
# R4-39d: distro-managed pins — package versions are bounded by the pinned
# base-image tag; security-sensitive packages (ca-certificates/openssl/libssl) are
# listed explicitly so they receive distro security updates on rebuild. See docs/SUPPLY_CHAIN_PINS.md.
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1-mesa-glx libglib2.0-0 libsm6 libxrender1 libxext6 \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY main.py .

# Run as a non-root user. Uploads go to /tmp (world-writable).
RUN useradd -u 10001 app
USER app
EXPOSE 8089

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8089"]
