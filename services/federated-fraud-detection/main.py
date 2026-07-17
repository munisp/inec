"""
INEC Innovation 4: Federated Learning Fraud Detection
======================================================
Implements a Federated Learning (FL) framework for election fraud detection.
Each state INEC office trains a local model on its own data without sharing
raw data. Only model weight updates (gradients) are shared with the central
aggregator, which uses FedAvg to produce a global model.

Privacy guarantees:
  - Raw voting data never leaves the state office
  - Differential privacy noise added to gradients before sharing
  - The global model improves from all states' experience

Architecture:
  - This service acts as the FL aggregator (central server)
  - State clients POST their model updates
  - Aggregator applies FedAvg and returns the updated global model
"""

import json
import time
from dataclasses import dataclass, field
from typing import Optional

import numpy as np
import uvicorn
from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

app = FastAPI(
    title="INEC Federated Fraud Detection Aggregator",
    description="Privacy-preserving federated learning for election fraud detection",
    version="1.0.0",
)

app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])

# ── Global Model State ────────────────────────────────────────────────────────

# Simple logistic regression weights (feature_dim=8)
FEATURE_DIM = 8
global_weights = np.zeros(FEATURE_DIM)
global_bias = 0.0
round_number = 0
client_updates: list[dict] = []
MIN_CLIENTS_PER_ROUND = 3  # Minimum state offices needed before aggregation

# Differential privacy parameters
DP_NOISE_SCALE = 0.01  # Gaussian noise std dev
DP_CLIP_NORM = 1.0     # Gradient clipping norm


# ── Federated Learning Utilities ──────────────────────────────────────────────

def add_differential_privacy_noise(weights: np.ndarray, noise_scale: float) -> np.ndarray:
    """Add Gaussian noise for differential privacy."""
    noise = np.random.normal(0, noise_scale, weights.shape)
    return weights + noise


def clip_gradients(weights: np.ndarray, clip_norm: float) -> np.ndarray:
    """Clip gradient norm to bound sensitivity."""
    norm = np.linalg.norm(weights)
    if norm > clip_norm:
        weights = weights * (clip_norm / norm)
    return weights


def federated_averaging(updates: list[dict]) -> tuple[np.ndarray, float]:
    """
    FedAvg algorithm: weighted average of client model updates.
    Weights are proportional to the number of local training samples.
    """
    total_samples = sum(u["num_samples"] for u in updates)
    agg_weights = np.zeros(FEATURE_DIM)
    agg_bias = 0.0

    for update in updates:
        weight = update["num_samples"] / total_samples
        client_w = np.array(update["weights"])
        # Apply DP noise and gradient clipping
        client_w = clip_gradients(client_w, DP_CLIP_NORM)
        client_w = add_differential_privacy_noise(client_w, DP_NOISE_SCALE)
        agg_weights += weight * client_w
        agg_bias += weight * update["bias"]

    return agg_weights, agg_bias


def sigmoid(x: np.ndarray) -> np.ndarray:
    return 1 / (1 + np.exp(-np.clip(x, -500, 500)))


def predict_fraud_probability(features: list[float]) -> float:
    """Predict fraud probability using the global model."""
    x = np.array(features)
    logit = np.dot(global_weights, x) + global_bias
    return float(sigmoid(logit))


# ── API Models ────────────────────────────────────────────────────────────────

class ModelUpdate(BaseModel):
    state_code: str
    round_number: int
    weights: list[float]  # Local model weights (length = FEATURE_DIM)
    bias: float
    num_samples: int
    loss: float
    accuracy: float


class FraudPredictionRequest(BaseModel):
    polling_unit_id: str
    features: list[float]  # [turnout_anomaly, time_anomaly, result_pattern, ...]


class GlobalModelResponse(BaseModel):
    round_number: int
    weights: list[float]
    bias: float
    num_clients: int
    message: str


# ── Endpoints ─────────────────────────────────────────────────────────────────

@app.post("/api/v1/federated/submit-update")
async def submit_model_update(update: ModelUpdate):
    """State INEC office submits its local model update."""
    global round_number, global_weights, global_bias

    if len(update.weights) != FEATURE_DIM:
        raise HTTPException(
            status_code=400,
            detail=f"Expected {FEATURE_DIM} weights, got {len(update.weights)}"
        )

    client_updates.append(update.dict())

    response = {
        "accepted": True,
        "state": update.state_code,
        "clients_in_round": len(client_updates),
        "min_required": MIN_CLIENTS_PER_ROUND,
        "aggregation_ready": len(client_updates) >= MIN_CLIENTS_PER_ROUND,
    }

    # Trigger aggregation when enough clients have submitted
    if len(client_updates) >= MIN_CLIENTS_PER_ROUND:
        new_weights, new_bias = federated_averaging(client_updates)
        global_weights = new_weights
        global_bias = new_bias
        round_number += 1
        client_updates.clear()
        response["aggregated"] = True
        response["new_round"] = round_number
        print(f"[FederatedFL] Round {round_number} aggregated from {MIN_CLIENTS_PER_ROUND}+ clients")

    return response


@app.get("/api/v1/federated/global-model", response_model=GlobalModelResponse)
async def get_global_model():
    """State offices download the current global model for local training."""
    return GlobalModelResponse(
        round_number=round_number,
        weights=global_weights.tolist(),
        bias=float(global_bias),
        num_clients=len(client_updates),
        message=f"Global model at round {round_number}",
    )


@app.post("/api/v1/federated/predict")
async def predict_fraud(req: FraudPredictionRequest):
    """Predict fraud probability for a polling unit using the global model."""
    if len(req.features) != FEATURE_DIM:
        raise HTTPException(
            status_code=400,
            detail=f"Expected {FEATURE_DIM} features, got {len(req.features)}"
        )

    prob = predict_fraud_probability(req.features)
    risk_level = "critical" if prob > 0.8 else "high" if prob > 0.6 else "medium" if prob > 0.4 else "low"

    return {
        "polling_unit_id": req.polling_unit_id,
        "fraud_probability": prob,
        "risk_level": risk_level,
        "model_round": round_number,
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


@app.get("/api/v1/federated/status")
async def status():
    return {
        "status": "healthy",
        "current_round": round_number,
        "pending_updates": len(client_updates),
        "min_clients_per_round": MIN_CLIENTS_PER_ROUND,
        "dp_noise_scale": DP_NOISE_SCALE,
        "dp_clip_norm": DP_CLIP_NORM,
    }


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8202, log_level="info")
