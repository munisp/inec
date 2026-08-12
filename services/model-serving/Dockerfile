FROM python:3.12-slim

WORKDIR /app

# Runtime deps for ONNX serving.
RUN pip install --no-cache-dir \
    onnxruntime==1.19.2 \
    numpy==2.2.5 \
    fastapi==0.115.6 \
    uvicorn[standard]==0.32.1 \
    pydantic==2.10.4

COPY model_serving.py model_versioning.py model_monitoring.py serve_http.py ./

ENV MODEL_DIR=/models \
    MODEL_CACHE_SIZE=10000

EXPOSE 8501

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://localhost:8501/healthz').status==200 else 1)"

CMD ["uvicorn", "serve_http:app", "--host", "0.0.0.0", "--port", "8501"]
