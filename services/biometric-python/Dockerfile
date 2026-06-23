FROM python:3.12-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1-mesa-glx libglib2.0-0 libsm6 libxext6 libxrender1 \
    && rm -rf /var/lib/apt/lists/*

COPY pyproject.toml .
RUN pip install --no-cache-dir .

COPY *.py .

EXPOSE 8090

CMD ["uvicorn", "service:app", "--host", "0.0.0.0", "--port", "8090", "--workers", "4"]
