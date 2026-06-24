"""Production biometric processing service.

FastAPI service exposing fingerprint, facial, iris, PAD, and quality APIs.
Designed for deployment behind APISIX in the INEC election platform.
"""

from __future__ import annotations

import base64
import io
import time
from contextlib import asynccontextmanager

import cv2
import numpy as np
import structlog
from fastapi import FastAPI, File, HTTPException, UploadFile
from fastapi.middleware.cors import CORSMiddleware
from PIL import Image
from prometheus_client import Counter, Histogram, generate_latest
from pydantic import BaseModel, Field
from starlette.responses import Response

from facial_engine import FacialEngine, FacialMatcher
from fingerprint_engine import FingerprintEngine, FingerprintMatcher
from iris_engine import IrisEngine, IrisMatcher
from pad_engine import FacePADEngine, FingerprintPADEngine, PADLevel
from quality_engine import (
    FaceQualityAssessor,
    FingerprintQualityAssessor,
    IrisQualityAssessor,
)

log = structlog.get_logger()

REQUESTS = Counter("biometric_requests_total", "Total requests", ["endpoint", "status"])
LATENCY = Histogram("biometric_latency_seconds", "Request latency", ["endpoint"])

fingerprint_engine = FingerprintEngine()
fingerprint_matcher = FingerprintMatcher()
facial_engine: FacialEngine | None = None
facial_matcher = FacialMatcher()
iris_engine = IrisEngine()
iris_matcher = IrisMatcher()
face_pad = FacePADEngine()
fingerprint_pad = FingerprintPADEngine()
fp_quality = FingerprintQualityAssessor()
face_quality = FaceQualityAssessor()
iris_quality = IrisQualityAssessor()


@asynccontextmanager
async def lifespan(app: FastAPI):
    global facial_engine
    facial_engine = FacialEngine()
    # Initialize PostgreSQL audit logging
    try:
        from pg_audit import init_pool, close_pool
        await init_pool()
        log.info("pg_audit_connected")
    except Exception as e:
        log.warning("pg_audit_unavailable", error=str(e))
    log.info("biometric_service_started", engines=["fingerprint", "facial", "iris", "pad", "quality"])
    yield
    try:
        from pg_audit import close_pool
        await close_pool()
    except Exception:
        pass
    log.info("biometric_service_stopped")


app = FastAPI(
    title="INEC Biometric Processing Service",
    version="1.0.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)


def decode_image(data: bytes) -> np.ndarray:
    nparr = np.frombuffer(data, np.uint8)
    img = cv2.imdecode(nparr, cv2.IMREAD_COLOR)
    if img is None:
        pil_img = Image.open(io.BytesIO(data))
        img = cv2.cvtColor(np.array(pil_img), cv2.COLOR_RGB2BGR)
    if img is None:
        raise ValueError("Could not decode image")
    return img


class MatchRequest(BaseModel):
    probe_image: str = Field(..., description="Base64-encoded probe image")
    gallery_image: str = Field(..., description="Base64-encoded gallery image")


class MultiModalMatchRequest(BaseModel):
    probe_fingerprint: str | None = Field(None, description="Base64 fingerprint image")
    probe_face: str | None = Field(None, description="Base64 face image")
    probe_iris: str | None = Field(None, description="Base64 iris image")
    gallery_fingerprint: str | None = Field(None, description="Base64 fingerprint image")
    gallery_face: str | None = Field(None, description="Base64 face image")
    gallery_iris: str | None = Field(None, description="Base64 iris image")
    fusion_weights: dict[str, float] | None = Field(
        None, description="Weights per modality, e.g. {'fingerprint': 0.4, 'face': 0.35, 'iris': 0.25}"
    )


class PADRequest(BaseModel):
    image: str = Field(..., description="Base64-encoded image")
    modality: str = Field("face", description="face or fingerprint")
    pad_level: str = Field("level2", description="level1, level2, or level3")
    face_bbox: list[int] | None = Field(None, description="[x1, y1, x2, y2]")


class QualityRequest(BaseModel):
    image: str = Field(..., description="Base64-encoded image")
    modality: str = Field("fingerprint", description="fingerprint, face, or iris")
    face_bbox: list[int] | None = Field(None, description="[x1, y1, x2, y2] for face")


# ─── Health ──────────────────────────────────────────────────────
@app.get("/health")
async def health():
    return {
        "status": "healthy",
        "persistence": "postgresql",
        "engines": {
            "fingerprint": True,
            "facial": facial_engine is not None,
            "iris": True,
            "pad": True,
            "quality": True,
        },
    }


@app.get("/processing/stats")
async def processing_stats():
    from pg_audit import get_processing_stats
    return await get_processing_stats()


@app.get("/metrics")
async def metrics():
    return Response(generate_latest(), media_type="text/plain")


# ─── Fingerprint ─────────────────────────────────────────────────
@app.post("/fingerprint/extract")
async def fingerprint_extract(file: UploadFile = File(...)):
    start = time.monotonic()
    try:
        data = await file.read()
        img = decode_image(data)
        template = fingerprint_engine.extract_template(img)
        REQUESTS.labels(endpoint="fingerprint_extract", status="success").inc()
        LATENCY.labels(endpoint="fingerprint_extract").observe(time.monotonic() - start)
        return {
            "template_hash": template.template_hash,
            "minutiae_count": len(template.minutiae),
            "pattern_type": template.pattern_type.value,
            "nfiq2_score": template.nfiq2_score,
            "core_points": len(template.core_points),
            "delta_points": len(template.delta_points),
            "ridge_count": template.ridge_count,
            "width": template.width,
            "height": template.height,
            "dpi": template.dpi,
            "minutiae": [
                {
                    "x": m.x, "y": m.y, "angle": round(m.angle, 1),
                    "type": m.minutia_type.value, "quality": round(m.quality, 3),
                }
                for m in template.minutiae[:50]
            ],
        }
    except Exception as e:
        REQUESTS.labels(endpoint="fingerprint_extract", status="error").inc()
        raise HTTPException(status_code=400, detail=str(e))


@app.post("/fingerprint/match")
async def fingerprint_match(req: MatchRequest):
    start = time.monotonic()
    try:
        probe_img = decode_image(base64.b64decode(req.probe_image))
        gallery_img = decode_image(base64.b64decode(req.gallery_image))

        probe = fingerprint_engine.extract_template(probe_img)
        gallery = fingerprint_engine.extract_template(gallery_img)
        result = fingerprint_matcher.match(probe, gallery)

        REQUESTS.labels(endpoint="fingerprint_match", status="success").inc()
        LATENCY.labels(endpoint="fingerprint_match").observe(time.monotonic() - start)
        return result
    except Exception as e:
        REQUESTS.labels(endpoint="fingerprint_match", status="error").inc()
        raise HTTPException(status_code=400, detail=str(e))


# ─── Face ────────────────────────────────────────────────────────
@app.post("/face/extract")
async def face_extract(file: UploadFile = File(...)):
    start = time.monotonic()
    try:
        data = await file.read()
        img = decode_image(data)
        template = facial_engine.extract_template(img)
        REQUESTS.labels(endpoint="face_extract", status="success").inc()
        LATENCY.labels(endpoint="face_extract").observe(time.monotonic() - start)
        return {
            "template_hash": template.template_hash,
            "embedding_dim": template.dimension,
            "bbox": {
                "x1": template.bbox.x1, "y1": template.bbox.y1,
                "x2": template.bbox.x2, "y2": template.bbox.y2,
                "confidence": round(template.bbox.confidence, 4),
            },
            "landmarks": {
                "left_eye": template.landmarks.left_eye,
                "right_eye": template.landmarks.right_eye,
                "nose": template.landmarks.nose,
            },
            "head_pose": {
                "yaw": round(template.head_pose.yaw, 2),
                "pitch": round(template.head_pose.pitch, 2),
                "roll": round(template.head_pose.roll, 2),
                "is_frontal": template.head_pose.is_frontal(),
            },
            "quality": {
                "overall": template.quality.overall,
                "iso_compliant": template.quality.iso_compliant,
                "brightness": template.quality.brightness,
                "contrast": template.quality.contrast,
                "sharpness": template.quality.sharpness,
                "rejection_reasons": template.quality.rejection_reasons,
            },
        }
    except Exception as e:
        REQUESTS.labels(endpoint="face_extract", status="error").inc()
        raise HTTPException(status_code=400, detail=str(e))


@app.post("/face/match")
async def face_match(req: MatchRequest):
    start = time.monotonic()
    try:
        probe_img = decode_image(base64.b64decode(req.probe_image))
        gallery_img = decode_image(base64.b64decode(req.gallery_image))

        probe = facial_engine.extract_template(probe_img)
        gallery = facial_engine.extract_template(gallery_img)
        result = facial_matcher.match(probe, gallery)

        REQUESTS.labels(endpoint="face_match", status="success").inc()
        LATENCY.labels(endpoint="face_match").observe(time.monotonic() - start)
        return result
    except Exception as e:
        REQUESTS.labels(endpoint="face_match", status="error").inc()
        raise HTTPException(status_code=400, detail=str(e))


# ─── Iris ────────────────────────────────────────────────────────
@app.post("/iris/extract")
async def iris_extract(file: UploadFile = File(...)):
    start = time.monotonic()
    try:
        data = await file.read()
        img = decode_image(data)
        template = iris_engine.extract_template(img)
        REQUESTS.labels(endpoint="iris_extract", status="success").inc()
        LATENCY.labels(endpoint="iris_extract").observe(time.monotonic() - start)
        return {
            "template_hash": template.template_hash,
            "code_bits": template.bits,
            "quality_score": round(template.quality_score, 4),
            "usable_bits_ratio": round(template.usable_bits_ratio, 4),
            "boundaries": {
                "pupil_center": template.boundaries.pupil_center,
                "pupil_radius": template.boundaries.pupil_radius,
                "iris_center": template.boundaries.iris_center,
                "iris_radius": template.boundaries.iris_radius,
            },
        }
    except Exception as e:
        REQUESTS.labels(endpoint="iris_extract", status="error").inc()
        raise HTTPException(status_code=400, detail=str(e))


@app.post("/iris/match")
async def iris_match(req: MatchRequest):
    start = time.monotonic()
    try:
        probe_img = decode_image(base64.b64decode(req.probe_image))
        gallery_img = decode_image(base64.b64decode(req.gallery_image))

        probe = iris_engine.extract_template(probe_img)
        gallery = iris_engine.extract_template(gallery_img)
        result = iris_matcher.match(probe, gallery)

        REQUESTS.labels(endpoint="iris_match", status="success").inc()
        LATENCY.labels(endpoint="iris_match").observe(time.monotonic() - start)
        return result
    except Exception as e:
        REQUESTS.labels(endpoint="iris_match", status="error").inc()
        raise HTTPException(status_code=400, detail=str(e))


# ─── Multi-modal ─────────────────────────────────────────────────
@app.post("/multimodal/match")
async def multimodal_match(req: MultiModalMatchRequest):
    start = time.monotonic()
    results = {}
    default_weights = {"fingerprint": 0.40, "face": 0.35, "iris": 0.25}
    weights = req.fusion_weights or default_weights

    try:
        if req.probe_fingerprint and req.gallery_fingerprint:
            probe = fingerprint_engine.extract_template(
                decode_image(base64.b64decode(req.probe_fingerprint))
            )
            gallery = fingerprint_engine.extract_template(
                decode_image(base64.b64decode(req.gallery_fingerprint))
            )
            results["fingerprint"] = fingerprint_matcher.match(probe, gallery)

        if req.probe_face and req.gallery_face:
            probe = facial_engine.extract_template(
                decode_image(base64.b64decode(req.probe_face))
            )
            gallery = facial_engine.extract_template(
                decode_image(base64.b64decode(req.gallery_face))
            )
            results["face"] = facial_matcher.match(probe, gallery)

        if req.probe_iris and req.gallery_iris:
            probe = iris_engine.extract_template(
                decode_image(base64.b64decode(req.probe_iris))
            )
            gallery = iris_engine.extract_template(
                decode_image(base64.b64decode(req.gallery_iris))
            )
            results["iris"] = iris_matcher.match(probe, gallery)

        if not results:
            raise ValueError("At least one modality pair is required")

        fused_score = 0.0
        total_weight = 0.0
        for modality, result in results.items():
            w = weights.get(modality, 1.0 / len(results))
            fused_score += result["score"] * w
            total_weight += w

        if total_weight > 0:
            fused_score /= total_weight

        decision = "match" if fused_score >= 0.45 else "no_match"

        REQUESTS.labels(endpoint="multimodal_match", status="success").inc()
        LATENCY.labels(endpoint="multimodal_match").observe(time.monotonic() - start)

        return {
            "fused_score": round(fused_score, 6),
            "decision": decision,
            "fusion_method": "weighted_sum",
            "weights": weights,
            "modality_results": results,
            "modalities_used": list(results.keys()),
            "latency_ms": round((time.monotonic() - start) * 1000, 2),
        }
    except Exception as e:
        REQUESTS.labels(endpoint="multimodal_match", status="error").inc()
        raise HTTPException(status_code=400, detail=str(e))


# ─── PAD ─────────────────────────────────────────────────────────
@app.post("/pad/check")
async def pad_check(req: PADRequest):
    start = time.monotonic()
    try:
        img = decode_image(base64.b64decode(req.image))
        level = PADLevel(req.pad_level)

        if req.modality == "fingerprint":
            result = fingerprint_pad.check(img)
        else:
            bbox = tuple(req.face_bbox) if req.face_bbox else None
            result = face_pad.check(img, face_bbox=bbox, pad_level=level)

        REQUESTS.labels(endpoint="pad_check", status="success").inc()
        LATENCY.labels(endpoint="pad_check").observe(time.monotonic() - start)

        return {
            "liveness_score": result.liveness_score,
            "decision": result.decision.value,
            "pad_level": result.pad_level.value,
            "attack_type": result.attack_type.value,
            "confidence": result.confidence,
            "iso_30107_compliant": result.iso_30107_compliant,
            "scores": {
                "texture": result.texture_score,
                "color": result.color_score,
                "moire": result.moire_score,
                "specular": result.specular_score,
                "frequency": result.frequency_score,
            },
            "processing_time_ms": result.processing_time_ms,
        }
    except Exception as e:
        REQUESTS.labels(endpoint="pad_check", status="error").inc()
        raise HTTPException(status_code=400, detail=str(e))


# ─── Quality ─────────────────────────────────────────────────────
@app.post("/quality/assess")
async def quality_assess(req: QualityRequest):
    start = time.monotonic()
    try:
        img = decode_image(base64.b64decode(req.image))

        if req.modality == "fingerprint":
            result = fp_quality.assess(img)
        elif req.modality == "face":
            bbox = tuple(req.face_bbox) if req.face_bbox else None
            result = face_quality.assess(img, face_bbox=bbox)
        elif req.modality == "iris":
            result = iris_quality.assess(img)
        else:
            raise ValueError(f"Unknown modality: {req.modality}")

        REQUESTS.labels(endpoint="quality_assess", status="success").inc()
        LATENCY.labels(endpoint="quality_assess").observe(time.monotonic() - start)

        return {
            "overall_score": result.overall_score,
            "level": result.level.value,
            "pass_threshold": result.pass_threshold,
            "metrics": result.metrics,
            "rejection_reasons": result.rejection_reasons,
            "processing_time_ms": result.processing_time_ms,
        }
    except Exception as e:
        REQUESTS.labels(endpoint="quality_assess", status="error").inc()
        raise HTTPException(status_code=400, detail=str(e))


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8090)
