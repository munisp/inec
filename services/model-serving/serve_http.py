"""Production HTTP serving layer for INEC ML models.

Wraps the ModelServer/ModelRouter with a FastAPI app exposing:
  GET  /healthz               liveness/readiness (model status + cache stats)
  GET  /v1/models             registered models and versions
  POST /v1/predict/{model_id} run inference on a JSON-encoded input tensor

Models are loaded at startup from MODEL_DIR (default: the biometric-python
models directory). The set of models to load is driven by MODEL_MANIFEST (JSON)
or falls back to auto-discovering *.onnx files.

Run:
  uvicorn serve_http:app --host 0.0.0.0 --port 8501
"""
from __future__ import annotations

import glob
import hmac
import json
import os
from contextlib import asynccontextmanager
from pathlib import Path
from typing import List

import numpy as np
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel

from model_serving import ModelServer, ModelRouter

MODEL_DIR = os.getenv(
    "MODEL_DIR",
    str(Path(__file__).resolve().parent.parent / "biometric-python" / "models"),
)
CACHE_SIZE = int(os.getenv("MODEL_CACHE_SIZE", "10000"))
# SECURITY: bound inference payloads (413 beyond) — previously unbounded.
MAX_PREDICT_VALUES = int(os.getenv("MAX_PREDICT_VALUES", "1000000"))

server = ModelServer(models_dir=MODEL_DIR, cache_size=CACHE_SIZE)
router = ModelRouter(server)


@asynccontextmanager
async def lifespan(app: FastAPI):
    _load_models()
    server.start()
    yield
    server.stop()


app = FastAPI(title="INEC Model Serving", version="1.0.0", lifespan=lifespan)

# SECURITY: /v1/* was unauthenticated. The service FAILS CLOSED when
# MODEL_SERVING_API_KEY is unset (503 on all non-health routes).
MODEL_SERVING_API_KEY = os.getenv("MODEL_SERVING_API_KEY", "").strip()


@app.middleware("http")
async def api_key_auth_middleware(request: Request, call_next):
    """Require the service API key on all non-health endpoints (fail closed)."""
    if request.url.path == "/healthz":
        return await call_next(request)
    if not MODEL_SERVING_API_KEY:
        return JSONResponse(
            status_code=503,
            content={"error": "MODEL_SERVING_API_KEY not configured; refusing to serve unauthenticated requests"},
        )
    auth = request.headers.get("Authorization", "")
    bearer = auth[7:] if auth.lower().startswith("bearer ") else auth
    provided = bearer or request.headers.get("x-api-key", "")
    if not provided or not hmac.compare_digest(provided.encode(), MODEL_SERVING_API_KEY.encode()):
        return JSONResponse(status_code=401, content={"error": "authentication required"})
    return await call_next(request)


class PredictRequest(BaseModel):
    # Flat input tensor plus its shape, so any modality (image/graph/features)
    # can be posted without a bespoke schema per model.
    data: List[float]
    shape: List[int]
    use_cache: bool = True


def _load_models() -> None:
    manifest = os.getenv("MODEL_MANIFEST")
    if manifest:
        for m in json.loads(manifest):
            server.load_model(
                model_id=m["model_id"],
                model_path=m["model_path"],
                version=m.get("version", "1.0.0"),
                model_type=m.get("model_type", "onnx"),
            )
        return
    # Auto-discover ONNX models by filename.
    for path in sorted(glob.glob(os.path.join(MODEL_DIR, "*.onnx"))):
        model_id = Path(path).stem.replace("_", "-")
        try:
            server.load_model(model_id=model_id, model_path=path, version="1.0.0", model_type="onnx")
        except Exception as exc:  # a bad/oversized file shouldn't kill startup
            print(f"WARN: could not load {path}: {exc}")


@app.get("/healthz")
def healthz() -> dict:
    return server.get_health()


@app.get("/v1/models")
def list_models() -> dict:
    models = server.model_registry.list_models()
    return {
        "models": [
            {"model_id": m.model_id, "version": m.version, "status": m.status.value}
            for m in models
        ]
    }


@app.post("/v1/predict/{model_id}")
def predict(model_id: str, req: PredictRequest) -> dict:
    if len(req.data) > MAX_PREDICT_VALUES:
        raise HTTPException(
            status_code=413,
            detail=f"data has {len(req.data)} values; limit is {MAX_PREDICT_VALUES}",
        )
    try:
        arr = np.asarray(req.data, dtype=np.float32).reshape(req.shape)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=f"data does not match shape: {exc}")
    try:
        result = server.predict(model_id, arr, use_cache=req.use_cache)
    except KeyError:
        raise HTTPException(status_code=404, detail=f"model not found: {model_id}")
    return {
        "model_id": result.model_id,
        "prediction": result.prediction,
        "confidence": result.confidence,
        "inference_time_ms": result.inference_time_ms,
        "cache_hit": result.cache_hit,
    }
