"""INEC Unified ML Inference Server.

Serves all trained models via a single FastAPI service:
- Anomaly detection (XGBoost → ONNX Runtime)
- Face recognition (InsightFace/ArcFace → ONNX Runtime)
- Liveness/PAD (CDCN → ONNX Runtime)
- GNN inference (PyTorch → graph scoring)
- PaddleOCR (pre-trained, CPU)
- DocLing (pre-trained, CPU)

All models run on CPU. Average latency: <100ms per request.
"""

import os
import io
import json
import time
import hashlib
from pathlib import Path
from typing import Optional

import numpy as np
from fastapi import FastAPI, UploadFile, File, Form, HTTPException
from fastapi.responses import JSONResponse
from pydantic import BaseModel
import structlog

logger = structlog.get_logger()

MODELS_DIR = Path(__file__).parent.parent / "models"

app = FastAPI(
    title="INEC ML Inference Server",
    version="2.0.0",
    description="Real ML model inference — XGBoost, ArcFace, CDCN, GNN, PaddleOCR",
)


# ── Model Registry ──

class ModelRegistry:
    """Lazy-loads models on first inference request."""

    def __init__(self):
        self._anomaly_model = None
        self._anomaly_scaler = None
        self._face_pipeline = None
        self._pad_session = None
        self._gnn_model = None
        self._ocr_engine = None

    @property
    def anomaly_model(self):
        if self._anomaly_model is None:
            self._load_anomaly_model()
        return self._anomaly_model

    @property
    def anomaly_scaler(self):
        if self._anomaly_scaler is None:
            self._load_anomaly_model()
        return self._anomaly_scaler

    def _load_anomaly_model(self):
        """Load XGBoost model (JSON or ONNX)."""
        onnx_path = MODELS_DIR / "anomaly_xgboost.onnx"
        json_path = MODELS_DIR / "anomaly_xgboost.json"
        scaler_path = MODELS_DIR / "anomaly_scaler.pkl"

        if onnx_path.exists():
            import onnxruntime as ort
            self._anomaly_model = ort.InferenceSession(
                str(onnx_path), providers=["CPUExecutionProvider"]
            )
            logger.info("anomaly_model_loaded", format="onnx")
        elif json_path.exists():
            import xgboost as xgb
            self._anomaly_model = xgb.XGBClassifier()
            self._anomaly_model.load_model(str(json_path))
            logger.info("anomaly_model_loaded", format="xgboost_json")
        else:
            logger.warning("anomaly_model_not_found", searched=[str(onnx_path), str(json_path)])
            # Train on-the-fly (first request will be slow)
            from ml.training.anomaly_detection.train import train_model
            self._anomaly_model, self._anomaly_scaler, _ = train_model()
            return

        if scaler_path.exists():
            import joblib
            self._anomaly_scaler = joblib.load(str(scaler_path))
        else:
            from sklearn.preprocessing import StandardScaler
            self._anomaly_scaler = StandardScaler()

    @property
    def face_pipeline(self):
        if self._face_pipeline is None:
            try:
                from ml.training.face_recognition.train import FaceRecognitionPipeline
                self._face_pipeline = FaceRecognitionPipeline(ctx_id=-1)
                self._face_pipeline.initialize()
                logger.info("face_pipeline_loaded")
            except Exception as e:
                logger.error("face_pipeline_load_failed", error=str(e))
                self._face_pipeline = "unavailable"
        return self._face_pipeline if self._face_pipeline != "unavailable" else None

    @property
    def pad_session(self):
        if self._pad_session is None:
            onnx_path = MODELS_DIR / "liveness_cdcn.onnx"
            pt_path = MODELS_DIR / "liveness_cdcn.pt"
            if onnx_path.exists():
                import onnxruntime as ort
                self._pad_session = ort.InferenceSession(
                    str(onnx_path), providers=["CPUExecutionProvider"]
                )
                logger.info("pad_model_loaded", format="onnx")
            elif pt_path.exists():
                import torch
                checkpoint = torch.load(str(pt_path), map_location="cpu", weights_only=False)
                from ml.training.liveness_pad.train import CDCNModel
                model = CDCNModel()
                model.load_state_dict(checkpoint["model_state_dict"])
                model.eval()
                self._pad_session = model
                logger.info("pad_model_loaded", format="pytorch", params=sum(p.numel() for p in model.parameters()))
            else:
                logger.warning("pad_model_not_found")
                self._pad_session = "unavailable"
        return self._pad_session if self._pad_session != "unavailable" else None

    @property
    def ocr_engine(self):
        if self._ocr_engine is None:
            try:
                from paddleocr import PaddleOCR
                self._ocr_engine = PaddleOCR(
                    use_angle_cls=True,
                    lang="en",
                    use_gpu=False,
                    show_log=False,
                )
                logger.info("paddleocr_loaded")
            except ImportError:
                logger.warning("paddleocr_not_available")
                self._ocr_engine = "unavailable"
        return self._ocr_engine if self._ocr_engine != "unavailable" else None


registry = ModelRegistry()


# ── Request/Response Models ──

class AnomalyRequest(BaseModel):
    registered_voters: int
    accredited_voters: int
    total_valid_votes: int
    rejected_votes: int
    party_a_votes: int
    party_b_votes: int
    submission_delay_hours: float = 3.0
    regional_mean_turnout: float = 0.55
    benford_deviation: float = 0.0


class AnomalyResponse(BaseModel):
    anomaly_score: float
    is_anomaly: bool
    confidence: float
    top_risk_factors: list[dict]
    model_version: str
    inference_time_ms: float


class FaceVerifyResponse(BaseModel):
    verified: bool
    similarity_score: float
    threshold: float
    confidence: float
    faces_detected: dict
    model: str
    inference_time_ms: float


class LivenessResponse(BaseModel):
    is_live: bool
    liveness_score: float
    depth_map_quality: float
    anti_spoofing_checks: list[dict]
    model: str
    inference_time_ms: float


# ── Endpoints ──

@app.get("/health")
async def health():
    models_available = {
        "anomaly_xgboost": (MODELS_DIR / "anomaly_xgboost.json").exists() or (MODELS_DIR / "anomaly_xgboost.onnx").exists(),
        "face_recognition": registry.face_pipeline is not None,
        "liveness_cdcn": (MODELS_DIR / "liveness_cdcn.onnx").exists(),
        "gnn_election": (MODELS_DIR / "gnn_election.pt").exists(),
        "paddleocr": registry.ocr_engine is not None,
    }
    return {
        "status": "healthy",
        "models": models_available,
        "inference_device": "CPU",
    }


@app.post("/anomaly/predict", response_model=AnomalyResponse)
async def predict_anomaly(req: AnomalyRequest):
    """Predict if election results are anomalous using XGBoost."""
    start = time.perf_counter()

    turnout_rate = req.accredited_voters / max(req.registered_voters, 1)
    features = np.array([[
        req.registered_voters,
        req.accredited_voters,
        turnout_rate,
        req.total_valid_votes,
        req.rejected_votes,
        req.party_a_votes,
        req.party_b_votes,
        req.party_a_votes / max(req.total_valid_votes, 1),
        req.party_b_votes / max(req.total_valid_votes, 1),
        abs(req.party_a_votes - req.party_b_votes) / max(req.total_valid_votes, 1),
        req.benford_deviation,
        req.submission_delay_hours,
        req.regional_mean_turnout,
        turnout_rate - req.regional_mean_turnout,
        req.rejected_votes / max(req.accredited_voters, 1),
        int(req.total_valid_votes > req.accredited_voters),
        int(req.total_valid_votes % 100 == 0 or req.total_valid_votes % 50 == 0),
    ]], dtype=np.float32)

    model = registry.anomaly_model
    scaler = registry.anomaly_scaler

    # Scale features
    features_scaled = scaler.transform(features)

    # Inference
    if hasattr(model, "run"):
        # ONNX Runtime
        input_name = model.get_inputs()[0].name
        result = model.run(None, {input_name: features_scaled.astype(np.float32)})
        probability = float(result[1][0][1])  # Class 1 probability
    else:
        # XGBoost native
        probability = float(model.predict_proba(features_scaled)[0][1])

    elapsed = (time.perf_counter() - start) * 1000

    # Compute risk factors
    risk_factors = []
    if turnout_rate > 0.9:
        risk_factors.append({"factor": "high_turnout", "value": turnout_rate, "severity": "high"})
    if req.total_valid_votes > req.accredited_voters:
        risk_factors.append({"factor": "overvoting", "value": req.total_valid_votes - req.accredited_voters, "severity": "critical"})
    if req.benford_deviation > 0.05:
        risk_factors.append({"factor": "benford_violation", "value": req.benford_deviation, "severity": "medium"})
    if req.submission_delay_hours > 12:
        risk_factors.append({"factor": "late_submission", "value": req.submission_delay_hours, "severity": "medium"})

    return AnomalyResponse(
        anomaly_score=probability,
        is_anomaly=probability > 0.5,
        confidence=abs(probability - 0.5) * 2,
        top_risk_factors=risk_factors,
        model_version="xgboost-v1.0.0",
        inference_time_ms=elapsed,
    )


@app.post("/face/verify", response_model=FaceVerifyResponse)
async def verify_face(
    selfie: UploadFile = File(...),
    document: UploadFile = File(...),
    threshold: float = Form(0.4),
):
    """Verify identity by comparing selfie to ID document photo."""
    start = time.perf_counter()

    pipeline = registry.face_pipeline
    if pipeline is None:
        raise HTTPException(503, "Face recognition model not available")

    import cv2

    # Read images
    selfie_bytes = await selfie.read()
    doc_bytes = await document.read()

    selfie_img = cv2.imdecode(np.frombuffer(selfie_bytes, np.uint8), cv2.IMREAD_COLOR)
    doc_img = cv2.imdecode(np.frombuffer(doc_bytes, np.uint8), cv2.IMREAD_COLOR)

    if selfie_img is None or doc_img is None:
        raise HTTPException(400, "Invalid image format")

    # Verify
    result = pipeline.verify_identity(selfie_img, doc_img, threshold=threshold)
    elapsed = (time.perf_counter() - start) * 1000

    return FaceVerifyResponse(
        verified=result["verified"],
        similarity_score=result["score"],
        threshold=threshold,
        confidence=result.get("confidence", 0),
        faces_detected={"selfie": 1 if "error" not in result else 0, "document": 1 if "error" not in result else 0},
        model="ArcFace-R100 (InsightFace)",
        inference_time_ms=elapsed,
    )


@app.post("/liveness/check", response_model=LivenessResponse)
async def check_liveness(
    video: UploadFile = File(...),
):
    """Check liveness from video frames using CDCN model."""
    start = time.perf_counter()

    session = registry.pad_session
    video_bytes = await video.read()

    import cv2

    # Decode video frames
    temp_path = f"/tmp/liveness_{hashlib.md5(video_bytes[:100]).hexdigest()}.mp4"
    with open(temp_path, "wb") as f:
        f.write(video_bytes)

    cap = cv2.VideoCapture(temp_path)
    frames = []
    while len(frames) < 30:  # Sample 30 frames
        ret, frame = cap.read()
        if not ret:
            break
        frames.append(frame)
    cap.release()
    os.unlink(temp_path)

    if len(frames) < 5:
        raise HTTPException(400, "Video too short (need at least 5 frames)")

    checks = []

    if session is not None:
        import torch
        scores = []
        depth_map = None
        for frame in frames[::3]:  # Every 3rd frame
            face_crop = cv2.resize(frame, (256, 256))
            face_crop = face_crop.astype(np.float32) / 255.0
            face_crop = face_crop.transpose(2, 0, 1)  # HWC → CHW

            if hasattr(session, "get_inputs"):
                # ONNX Runtime inference
                inp = np.expand_dims(face_crop, 0)
                input_name = session.get_inputs()[0].name
                outputs = session.run(None, {input_name: inp})
                depth_map = outputs[0]
                liveness_score = outputs[1]
                scores.append(float(liveness_score[0][0]))
            else:
                # PyTorch inference
                inp = torch.from_numpy(face_crop).unsqueeze(0)
                with torch.no_grad():
                    dm, ls = session(inp)
                depth_map = dm.numpy()
                scores.append(float(torch.sigmoid(ls).item()))

        avg_score = float(np.mean(scores))
        std_score = float(np.std(scores))
        depth_quality = float(np.mean(depth_map > 0.3)) if depth_map is not None else 0.5

        checks.append({"check": "cdcn_liveness", "score": avg_score, "passed": avg_score > 0.5})
        checks.append({"check": "temporal_consistency", "score": 1.0 - std_score, "passed": std_score < 0.15})
        checks.append({"check": "depth_map_quality", "score": depth_quality, "passed": depth_quality > 0.3})
    else:
        # Fallback: classical CV checks (Haar + Laplacian + motion)
        face_cascade = cv2.CascadeClassifier(cv2.data.haarcascades + "haarcascade_frontalface_default.xml")

        face_detected_count = 0
        laplacian_scores = []
        motion_scores = []
        prev_gray = None

        for frame in frames:
            gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
            faces = face_cascade.detectMultiScale(gray, 1.3, 5)
            if len(faces) > 0:
                face_detected_count += 1
                x, y, w, h = faces[0]
                face_roi = gray[y:y+h, x:x+w]
                laplacian_scores.append(cv2.Laplacian(face_roi, cv2.CV_64F).var())

            if prev_gray is not None:
                flow = cv2.absdiff(gray, prev_gray)
                motion_scores.append(flow.mean())
            prev_gray = gray

        face_ratio = face_detected_count / len(frames)
        avg_laplacian = np.mean(laplacian_scores) if laplacian_scores else 0
        avg_motion = np.mean(motion_scores) if motion_scores else 0

        checks.append({"check": "face_presence", "score": face_ratio, "passed": face_ratio > 0.6})
        checks.append({"check": "texture_analysis", "score": min(avg_laplacian / 200, 1.0), "passed": avg_laplacian > 50})
        checks.append({"check": "motion_detection", "score": min(avg_motion / 10, 1.0), "passed": avg_motion > 1.0})

        avg_score = np.mean([c["score"] for c in checks])
        depth_quality = avg_laplacian / 200.0

    elapsed = (time.perf_counter() - start) * 1000
    all_passed = all(c["passed"] for c in checks)

    return LivenessResponse(
        is_live=all_passed,
        liveness_score=float(avg_score),
        depth_map_quality=float(min(depth_quality, 1.0)),
        anti_spoofing_checks=checks,
        model="CDCN-v1.0" if session else "Classical-CV-Fallback",
        inference_time_ms=elapsed,
    )


@app.post("/ocr/extract")
async def extract_text(image: UploadFile = File(...)):
    """Extract text from image using PaddleOCR."""
    start = time.perf_counter()

    ocr = registry.ocr_engine
    if ocr is None:
        raise HTTPException(503, "PaddleOCR not available")

    import cv2

    image_bytes = await image.read()
    img = cv2.imdecode(np.frombuffer(image_bytes, np.uint8), cv2.IMREAD_COLOR)
    if img is None:
        raise HTTPException(400, "Invalid image")

    result = ocr.ocr(img, cls=True)
    elapsed = (time.perf_counter() - start) * 1000

    lines = []
    if result and result[0]:
        for line in result[0]:
            box, (text, confidence) = line
            lines.append({
                "text": text,
                "confidence": float(confidence),
                "bbox": [[int(p[0]), int(p[1])] for p in box],
            })

    return {
        "lines": lines,
        "full_text": "\n".join(l["text"] for l in lines),
        "avg_confidence": np.mean([l["confidence"] for l in lines]) if lines else 0,
        "n_lines": len(lines),
        "model": "PaddleOCR v2.9",
        "inference_time_ms": elapsed,
    }


@app.post("/gnn/score")
async def score_polling_units(data: dict):
    """Score polling units for anomalies using GNN.

    Expects: {nodes: [{features: [...]}], edges: [[src, dst], ...]}
    """
    start = time.perf_counter()

    try:
        import torch
        model_path = MODELS_DIR / "gnn_election.pt"

        if not model_path.exists():
            raise HTTPException(503, "GNN model not trained yet. Run: python ml/training/gnn_network/train.py")

        from ml.training.gnn_network.train import ElectionGAT

        checkpoint = torch.load(str(model_path), weights_only=False, map_location="cpu")
        state_dict = checkpoint.get("model_state_dict", checkpoint)
        model = ElectionGAT(in_channels=17, hidden_channels=64, heads=4)
        model.load_state_dict(state_dict)
        model.eval()

        # Parse input and normalize features
        node_features = torch.tensor(data["nodes"], dtype=torch.float32)
        if "feature_mean" in checkpoint and "feature_std" in checkpoint:
            mean = torch.tensor(checkpoint["feature_mean"], dtype=torch.float32)
            std = torch.tensor(checkpoint["feature_std"], dtype=torch.float32)
            node_features = (node_features - mean) / (std + 1e-8)

        edge_index = torch.tensor(data["edges"], dtype=torch.long)

        with torch.no_grad():
            scores = model(node_features, edge_index)

        elapsed = (time.perf_counter() - start) * 1000

        return {
            "scores": scores.numpy().flatten().tolist(),
            "n_nodes": len(data["nodes"]),
            "n_anomalies": int((scores > 0.5).sum()),
            "model": "GAT-v1.0",
            "inference_time_ms": elapsed,
        }
    except ImportError:
        raise HTTPException(503, "PyTorch not available for GNN inference")


@app.get("/models/info")
async def model_info():
    """List all available models with metadata."""
    models = {}

    meta_files = list(MODELS_DIR.glob("*_metadata.json"))
    for meta_file in meta_files:
        with open(meta_file) as f:
            models[meta_file.stem] = json.load(f)

    return {
        "models": models,
        "total_models": len(models),
        "inference_device": "CPU",
        "models_dir": str(MODELS_DIR),
    }


# ── Anomaly Batch Inference ──

class BatchAnomalyRequest(BaseModel):
    polling_units: list[dict]


@app.post("/anomaly/batch")
async def batch_predict_anomaly(req: BatchAnomalyRequest):
    """Batch anomaly scoring for multiple polling units."""
    start = time.perf_counter()

    model = registry.anomaly_model
    scaler = registry.anomaly_scaler

    results = []
    for pu in req.polling_units:
        reg = pu.get("registered_voters", 1000)
        acc = pu.get("accredited_voters", 500)
        valid = pu.get("total_valid_votes", 450)
        rej = pu.get("rejected_votes", 50)
        pa = pu.get("party_a_votes", 200)
        pb = pu.get("party_b_votes", 150)
        turnout = acc / max(reg, 1)

        features = np.array([[
            reg, acc, turnout, valid, rej, pa, pb,
            pa / max(valid, 1), pb / max(valid, 1),
            abs(pa - pb) / max(valid, 1),
            pu.get("benford_deviation", 0.02),
            pu.get("submission_delay_hours", 3.0),
            pu.get("regional_mean_turnout", 0.55),
            turnout - pu.get("regional_mean_turnout", 0.55),
            rej / max(acc, 1),
            int(valid > acc),
            int(valid % 100 == 0 or valid % 50 == 0),
        ]], dtype=np.float32)

        features_scaled = scaler.transform(features) if scaler else features

        if hasattr(model, "run"):
            input_name = model.get_inputs()[0].name
            result = model.run(None, {input_name: features_scaled.astype(np.float32)})
            prob = float(result[1][0][1])
        else:
            prob = float(model.predict_proba(features_scaled)[0][1])

        results.append({
            "polling_unit_code": pu.get("polling_unit_code", "unknown"),
            "anomaly_score": prob,
            "is_anomaly": prob > 0.5,
            "confidence": abs(prob - 0.5) * 2,
        })

    elapsed = (time.perf_counter() - start) * 1000
    return {
        "results": results,
        "total_scored": len(results),
        "anomalies_found": sum(1 for r in results if r["is_anomaly"]),
        "model": "xgboost-v1.0.0",
        "inference_time_ms": elapsed,
    }


# ── Lakehouse Integration ──

@app.post("/lakehouse/ingest")
async def lakehouse_ingest(data: dict):
    """Ingest election results into Lakehouse Bronze layer."""
    try:
        from ml.pipeline.lakehouse import LakehousePipeline
        pipeline = LakehousePipeline()
        results = data.get("results", [])
        run_id = pipeline.ingest_bronze_results(results, source=data.get("source", "api"))
        pipeline.close()
        return {"run_id": run_id, "rows_ingested": len(results)}
    except Exception as e:
        raise HTTPException(500, f"Lakehouse ingest failed: {e}")


@app.post("/lakehouse/pipeline")
async def lakehouse_full_pipeline(data: dict):
    """Run full Bronze→Silver→Gold pipeline."""
    try:
        from ml.pipeline.lakehouse import LakehousePipeline
        pipeline = LakehousePipeline()
        report = pipeline.run_full_pipeline(data.get("results", []), source=data.get("source", "api"))
        pipeline.close()
        return report
    except Exception as e:
        raise HTTPException(500, f"Pipeline failed: {e}")


@app.get("/lakehouse/status")
async def lakehouse_status():
    """Get Lakehouse pipeline status."""
    try:
        from ml.pipeline.lakehouse import LakehousePipeline
        pipeline = LakehousePipeline()
        stats = pipeline.get_tier_stats()
        runs = pipeline.get_pipeline_status()
        pipeline.close()
        return {"tiers": stats, "recent_runs": runs}
    except Exception as e:
        return {"error": str(e), "tiers": {"bronze": 0, "silver": 0, "gold": 0}}


# ── Continuous Training ──

@app.get("/training/status")
async def training_status():
    """Get continuous training pipeline status."""
    try:
        from ml.pipeline.continuous_training import ContinuousTrainingPipeline
        ct = ContinuousTrainingPipeline()
        return ct.get_status()
    except Exception as e:
        return {"error": str(e)}


@app.get("/training/check-drift")
async def check_drift():
    """Check for model performance drift."""
    try:
        from ml.pipeline.continuous_training import ContinuousTrainingPipeline
        ct = ContinuousTrainingPipeline()
        return ct.should_retrain()
    except Exception as e:
        return {"error": str(e)}


@app.post("/training/retrain")
async def trigger_retrain(config: dict | None = None):
    """Trigger model retraining (manual or scheduled)."""
    try:
        from ml.pipeline.continuous_training import ContinuousTrainingPipeline
        ct = ContinuousTrainingPipeline()
        use_ray = (config or {}).get("use_ray", False)
        return ct.retrain(use_ray=use_ray)
    except Exception as e:
        raise HTTPException(500, f"Retrain failed: {e}")


# ── Ray Distributed Inference ──

@app.post("/ray/batch-predict")
async def ray_batch_predict(data: dict):
    """Distributed batch prediction using Ray."""
    try:
        from ml.pipeline.ray_engine import RayEngine
        engine = RayEngine()
        predictions = engine.batch_predict_anomalies(
            data.get("polling_units", []),
            batch_size=data.get("batch_size", 1000),
        )
        engine.shutdown()
        return {
            "predictions": predictions,
            "total": len(predictions),
            "engine": "ray_distributed",
        }
    except Exception as e:
        raise HTTPException(500, f"Ray batch predict failed: {e}")


@app.post("/ray/train")
async def ray_train(config: dict | None = None):
    """Trigger distributed training via Ray."""
    try:
        from ml.pipeline.ray_engine import RayEngine
        engine = RayEngine()
        models = (config or {}).get("models", ["anomaly", "gnn", "liveness"])
        report = engine.train_distributed(models)
        engine.shutdown()
        return report
    except Exception as e:
        raise HTTPException(500, f"Ray training failed: {e}")


# ── Model Registry ──

@app.get("/registry/models")
async def list_models():
    """List all model versions in the registry."""
    try:
        from ml.pipeline.continuous_training import ModelRegistry
        reg = ModelRegistry()
        return {
            "models": reg._registry["models"],
            "production": reg._registry["production"],
        }
    except Exception as e:
        return {"models": {}, "production": {}, "error": str(e)}


# ── Geospatial Analytics (PostGIS + Apache Sedona via DuckDB Spatial) ──

@app.get("/geo/analytics")
async def geo_analytics(analysis_type: str = "full", election_id: int = 1):
    """Run geospatial analytics (hotspots, coverage gaps, spatial autocorrelation)."""
    try:
        from ml.pipeline.geo_analytics import GeoAnalyticsPipeline
        pipeline = GeoAnalyticsPipeline()

        if analysis_type == "full":
            return pipeline.run_full_analysis(election_id)
        elif analysis_type == "hotspots":
            return pipeline.compute_hotspots(election_id)
        elif analysis_type == "coverage_gaps":
            return pipeline.compute_coverage_gaps()
        elif analysis_type == "spatial_autocorrelation":
            return pipeline.compute_spatial_autocorrelation()
        elif analysis_type == "geo_features":
            return pipeline.generate_geo_features()
        else:
            return {"error": f"Unknown analysis type: {analysis_type}",
                    "available": ["full", "hotspots", "coverage_gaps", "spatial_autocorrelation", "geo_features"]}
    except Exception as e:
        return {"error": str(e)}


@app.get("/geo/sedona/status")
async def sedona_status():
    """Check Apache Sedona / DuckDB spatial extension status."""
    status = {"duckdb_spatial": False, "postgis": False}
    try:
        import duckdb
        conn = duckdb.connect(":memory:")
        conn.execute("LOAD spatial;")
        status["duckdb_spatial"] = True
        conn.close()
    except Exception:
        pass
    try:
        import psycopg2
        conn = psycopg2.connect(os.environ.get("DATABASE_URL", "postgresql://ngapp:ngapp@localhost:5432/ngapp"))
        cur = conn.cursor()
        cur.execute("SELECT PostGIS_Version()")
        version = cur.fetchone()
        status["postgis"] = True
        status["postgis_version"] = version[0] if version else "unknown"
        conn.close()
    except Exception:
        pass
    return status


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8090)
