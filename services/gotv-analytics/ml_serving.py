"""ML Model Serving — Production inference endpoints for trained PyTorch models.

Loads trained weights from ml/models/ and serves predictions via FastAPI.
Supports: fraud detection, voter engagement scoring, GNN anomaly detection.
Includes: model registry queries, monitoring, drift detection, A/B testing.
"""

import json
import os
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import numpy as np
import structlog
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, Field

logger = structlog.get_logger()

router = APIRouter(prefix="/ml", tags=["ml-serving"])

# Resolve models directory
ML_DIR = Path(os.getenv("ML_MODELS_DIR", str(
    Path(__file__).parent.parent.parent / "ml"
)))
MODELS_DIR = ML_DIR / "models"

# Add ML to path for imports
if str(ML_DIR.parent) not in sys.path:
    sys.path.insert(0, str(ML_DIR.parent))

# ── Lazy model loading ──

_fraud_model = None
_fraud_scaler = None
_voter_model = None
_voter_scaler = None
_voter_target_norm = None
_gnn_model = None
_gnn_scaler = None


def _load_fraud_model():
    global _fraud_model, _fraud_scaler
    if _fraud_model is not None:
        return _fraud_model, _fraud_scaler

    import torch
    import joblib

    weights_path = MODELS_DIR / "fraud_dnn.pt"
    scaler_path = MODELS_DIR / "fraud_dnn_scaler.pkl"

    if not weights_path.exists():
        logger.warning("fraud_model_not_found", path=str(weights_path))
        return None, None

    from ml.training.gotv_ml_stack import FraudDetectionDNN
    model = FraudDetectionDNN(in_features=17)
    model.load_state_dict(torch.load(weights_path, map_location="cpu", weights_only=True))
    model.eval()

    scaler = joblib.load(scaler_path) if scaler_path.exists() else None
    _fraud_model = model
    _fraud_scaler = scaler
    logger.info("fraud_model_loaded", weights=str(weights_path))
    return model, scaler


def _load_voter_model():
    global _voter_model, _voter_scaler, _voter_target_norm
    if _voter_model is not None:
        return _voter_model, _voter_scaler, _voter_target_norm

    import torch
    import joblib

    weights_path = MODELS_DIR / "voter_scoring.pt"
    scaler_path = MODELS_DIR / "voter_scoring_scaler.pkl"
    norm_path = MODELS_DIR / "voter_scoring_target_norm.pkl"

    if not weights_path.exists():
        return None, None, None

    from ml.training.gotv_ml_stack import VoterScoringNet
    model = VoterScoringNet(in_features=11)
    model.load_state_dict(torch.load(weights_path, map_location="cpu", weights_only=True))
    model.eval()

    scaler = joblib.load(scaler_path) if scaler_path.exists() else None
    target_norm = joblib.load(norm_path) if norm_path.exists() else {"mean": 50.0, "std": 15.0}
    _voter_model = model
    _voter_scaler = scaler
    _voter_target_norm = target_norm
    logger.info("voter_model_loaded")
    return model, scaler, target_norm


def _load_gnn_model():
    global _gnn_model, _gnn_scaler
    if _gnn_model is not None:
        return _gnn_model, _gnn_scaler

    import torch
    import joblib

    weights_path = MODELS_DIR / "gnn_election.pt"
    scaler_path = MODELS_DIR / "gnn_scaler.pkl"

    if not weights_path.exists():
        return None, None

    from ml.training.gotv_ml_stack import ElectionGATFallback
    model = ElectionGATFallback(in_channels=17)
    model.load_state_dict(torch.load(weights_path, map_location="cpu", weights_only=True))
    model.eval()

    scaler = joblib.load(scaler_path) if scaler_path.exists() else None
    _gnn_model = model
    _gnn_scaler = scaler
    logger.info("gnn_model_loaded")
    return model, scaler


# ── Request/Response schemas ──

class FraudPredictionRequest(BaseModel):
    registered_voters: int = Field(..., ge=1)
    accredited_voters: int = Field(..., ge=0)
    total_valid_votes: int = Field(..., ge=0)
    rejected_votes: int = Field(0, ge=0)
    party_a_votes: int = Field(0, ge=0)
    party_b_votes: int = Field(0, ge=0)
    benford_deviation: float = Field(0.0, ge=0)
    submission_delay_hours: float = Field(3.0, ge=0)
    regional_mean_turnout: float = Field(0.4, ge=0, le=1)

class EngagementPredictionRequest(BaseModel):
    age: int = Field(35, ge=18, le=100)
    contacts_received: int = Field(0, ge=0)
    sms_received: int = Field(0, ge=0)
    calls_received: int = Field(0, ge=0)
    door_knocks: int = Field(0, ge=0)
    has_pledge: int = Field(0, ge=0, le=1)
    needs_ride: int = Field(0, ge=0, le=1)
    prev_elections: int = Field(0, ge=0, le=10)
    is_urban: int = Field(0, ge=0, le=1)
    days_since_last_contact: int = Field(7, ge=0)
    whatsapp_interactions: int = Field(0, ge=0)


# ── Prediction endpoints ──

@router.post("/predict/fraud")
async def predict_fraud(req: FraudPredictionRequest):
    """Run fraud detection on a single polling unit result."""
    import torch

    model, scaler = _load_fraud_model()
    if model is None:
        raise HTTPException(503, "Fraud model not loaded — run training pipeline")

    turnout_rate = req.accredited_voters / max(req.registered_voters, 1)
    apc_share = req.party_a_votes / max(req.total_valid_votes, 1)
    pdp_share = req.party_b_votes / max(req.total_valid_votes, 1)
    vote_margin = abs(req.party_a_votes - req.party_b_votes) / max(req.total_valid_votes, 1)
    rejected_rate = req.rejected_votes / max(req.accredited_voters, 1)
    overvoting = 1 if req.total_valid_votes > req.accredited_voters else 0
    round_flag = 1 if req.total_valid_votes % 100 == 0 or req.total_valid_votes % 50 == 0 else 0

    features = np.array([[
        req.registered_voters, req.accredited_voters, turnout_rate,
        req.total_valid_votes, req.rejected_votes,
        req.party_a_votes, req.party_b_votes,
        apc_share, pdp_share, vote_margin,
        req.benford_deviation, req.submission_delay_hours,
        req.regional_mean_turnout,
        turnout_rate - req.regional_mean_turnout,
        rejected_rate, overvoting, round_flag,
    ]], dtype=np.float32)

    if scaler is not None:
        features = scaler.transform(features)

    with torch.no_grad():
        tensor = torch.tensor(features, dtype=torch.float32)
        score = model(tensor).item()

    return {
        "model": "FraudDetectionDNN-v1",
        "fraud_probability": round(score, 4),
        "is_fraud": score > 0.5,
        "risk_level": "high" if score > 0.8 else "medium" if score > 0.5 else "low",
        "features_used": 17,
        "inference_device": "cpu",
    }


@router.post("/predict/engagement")
async def predict_engagement(req: EngagementPredictionRequest):
    """Score voter engagement likelihood (0-100)."""
    import torch

    model, scaler, target_norm = _load_voter_model()
    if model is None:
        raise HTTPException(503, "Voter scoring model not loaded")

    features = np.array([[
        req.age, req.contacts_received, req.sms_received, req.calls_received,
        req.door_knocks, req.has_pledge, req.needs_ride, req.prev_elections,
        req.is_urban, req.days_since_last_contact, req.whatsapp_interactions,
    ]], dtype=np.float32)

    if scaler is not None:
        features = scaler.transform(features)

    with torch.no_grad():
        tensor = torch.tensor(features, dtype=torch.float32)
        raw = model(tensor).item()

    # Denormalize
    score = raw * target_norm["std"] + target_norm["mean"]
    score = max(0.0, min(100.0, score))

    return {
        "model": "VoterScoringNet-v1",
        "engagement_score": round(score, 1),
        "tier": "high" if score >= 70 else "medium" if score >= 40 else "low",
        "features_used": 11,
    }


@router.post("/predict/anomaly")
async def predict_anomaly(req: FraudPredictionRequest):
    """Run GNN-based anomaly detection on polling unit data."""
    import torch

    model, scaler = _load_gnn_model()
    if model is None:
        raise HTTPException(503, "GNN model not loaded")

    turnout_rate = req.accredited_voters / max(req.registered_voters, 1)
    apc_share = req.party_a_votes / max(req.total_valid_votes, 1)
    pdp_share = req.party_b_votes / max(req.total_valid_votes, 1)
    vote_margin = abs(req.party_a_votes - req.party_b_votes) / max(req.total_valid_votes, 1)
    rejected_rate = req.rejected_votes / max(req.accredited_voters, 1)
    overvoting = 1 if req.total_valid_votes > req.accredited_voters else 0
    round_flag = 1 if req.total_valid_votes % 100 == 0 or req.total_valid_votes % 50 == 0 else 0

    features = np.array([[
        req.registered_voters, req.accredited_voters, turnout_rate,
        req.total_valid_votes, req.rejected_votes,
        req.party_a_votes, req.party_b_votes,
        apc_share, pdp_share, vote_margin,
        req.benford_deviation, req.submission_delay_hours,
        req.regional_mean_turnout,
        turnout_rate - req.regional_mean_turnout,
        rejected_rate, overvoting, round_flag,
    ]], dtype=np.float32)

    if scaler is not None:
        features = scaler.transform(features)

    with torch.no_grad():
        tensor = torch.tensor(features, dtype=torch.float32)
        score = model(tensor).squeeze().item()

    return {
        "model": "ElectionGAT-v1",
        "anomaly_probability": round(score, 4),
        "is_anomaly": score > 0.5,
        "graph_context": "node-level (no graph edges in API mode)",
    }


# ── Registry & Monitoring ──

@router.get("/models")
async def list_models():
    """List all registered models."""
    registry_path = MODELS_DIR / "registry" / "model_registry.json"
    if not registry_path.exists():
        return {"models": {}, "production": {}}
    with open(registry_path) as f:
        return json.load(f)


@router.get("/monitoring")
async def get_monitoring():
    """Get monitoring status and alerts."""
    monitor_path = MODELS_DIR / "registry" / "monitoring.json"
    if not monitor_path.exists():
        return {"predictions": 0, "alerts": [], "drift_checks": []}
    with open(monitor_path) as f:
        data = json.load(f)
    return {
        "total_predictions": len(data.get("predictions", [])),
        "active_alerts": len(data.get("alerts", [])),
        "drift_checks": data.get("drift_checks", [])[-10:],
        "alerts": data.get("alerts", [])[-10:],
    }


@router.get("/weights")
async def list_weights():
    """List shipped model weight files."""
    weights = []
    for ext in ["*.pt", "*.json", "*.pkl"]:
        for f in MODELS_DIR.glob(ext):
            stat = f.stat()
            weights.append({
                "name": f.name,
                "size_bytes": stat.st_size,
                "modified": datetime.fromtimestamp(stat.st_mtime, tz=timezone.utc).isoformat(),
            })
    return {"weights": weights, "total": len(weights)}


@router.post("/train")
async def trigger_training(model: str = "all"):
    """Trigger model training (runs synchronously for now)."""
    try:
        from ml.training.gotv_ml_stack import (
            generate_nigerian_election_data, generate_voter_engagement_data,
            train_fraud_dnn, train_voter_scoring, train_gnn_fallback,
            ProductionModelRegistry,
        )
        import time as _time

        results = {}
        registry = ProductionModelRegistry()

        if model in ("all", "fraud"):
            data = generate_nigerian_election_data(50000)
            meta = train_fraud_dnn(data, epochs=50)
            mid = registry.register("fraud_dnn", f"1.{int(_time.time())}",
                                    meta["test_metrics"], str(MODELS_DIR / "fraud_dnn.pt"))
            if meta["test_metrics"].get("roc_auc", 0) > 0.85:
                registry.promote(mid)
            results["fraud_dnn"] = meta["test_metrics"]
            # Invalidate cached model
            global _fraud_model
            _fraud_model = None

        if model in ("all", "voter"):
            data = generate_voter_engagement_data(100000)
            meta = train_voter_scoring(data, epochs=50)
            mid = registry.register("voter_scoring", f"1.{int(_time.time())}",
                                    meta["test_metrics"], str(MODELS_DIR / "voter_scoring.pt"))
            if meta["test_metrics"].get("r2", 0) > 0.5:
                registry.promote(mid)
            results["voter_scoring"] = meta["test_metrics"]
            global _voter_model
            _voter_model = None

        if model in ("all", "gnn"):
            data = generate_nigerian_election_data(50000)
            meta = train_gnn_fallback(data, epochs=50)
            mid = registry.register("gnn_election", f"1.{int(_time.time())}",
                                    meta["test_metrics"], str(MODELS_DIR / "gnn_election.pt"))
            if meta["test_metrics"].get("roc_auc", 0) > 0.85:
                registry.promote(mid)
            results["gnn_election"] = meta["test_metrics"]
            global _gnn_model
            _gnn_model = None

        return {"status": "completed", "models_trained": list(results.keys()), "metrics": results}

    except Exception as e:
        logger.error("training_failed", error=str(e))
        raise HTTPException(500, f"Training failed: {e}")
