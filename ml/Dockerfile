FROM python:3.12-slim

WORKDIR /app

# System dependencies for OpenCV, PaddleOCR
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1-mesa-glx libglib2.0-0 libsm6 libxrender1 libxext6 \
    curl gcc g++ \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy training scripts and serving code
COPY training/ training/
COPY serving/ serving/
COPY configs/ configs/

# Create model directory
RUN mkdir -p models data/processed

EXPOSE 8090

# Default: run inference server
CMD ["python", "-m", "uvicorn", "serving.inference_server:app", "--host", "0.0.0.0", "--port", "8090"]
