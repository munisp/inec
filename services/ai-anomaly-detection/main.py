"""INEC real-time anomaly API backed by the trained CPU ONNX inference service.

This service validates election-result features, delegates all scoring to the
versioned XGBoost ONNX model in ``inference-engine``, and broadcasts only real
model outputs. It does not train from synthetic data or fabricate baseline
statistics at runtime.
"""

from __future__ import annotations

import asyncio
import os
from datetime import datetime, timezone
from typing import Any

import httpx
import uvicorn
from fastapi import FastAPI, HTTPException, WebSocket, WebSocketDisconnect
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field

INFERENCE_ENGINE_URL = os.getenv("INFERENCE_ENGINE_URL", "").strip().rstrip("/")
CORS_ORIGINS = [origin.strip() for origin in os.getenv("CORS_ORIGINS", "").split(",") if origin.strip()]

app = FastAPI(
    title="INEC AI Anomaly Detection Service",
    description="Real-time election irregularity detection using the trained CPU ONNX model",
    version="2.0.0",
)
app.add_middleware(
    CORSMiddleware,
    allow_origins=CORS_ORIGINS,
    allow_methods=["GET", "POST"],
    allow_headers=["Content-Type", "Authorization", "X-Request-ID"],
    allow_credentials=True,
)

connected_clients: list[WebSocket] = []


class VotingRecord(BaseModel):
    polling_unit_id: str = Field(min_length=1)
    state: str = Field(min_length=1)
    lga: str = Field(min_length=1)
    ward: str = Field(min_length=1)
    registered_voters: int = Field(gt=0)
    accredited_voters: int = Field(ge=0)
    votes_cast: int = Field(ge=0)
    rejected_votes: int = Field(ge=0)
    submission_delay_min: float = Field(ge=0)
    benford_deviation: float = Field(ge=0)
    regional_mean_turnout: float = Field(ge=0, le=1)
    party_results: dict[str, int]
    timestamp: datetime = Field(default_factory=lambda: datetime.now(timezone.utc))


class AnomalyAlert(BaseModel):
    polling_unit_id: str
    state: str
    anomaly_score: float
    is_anomaly: bool
    confidence: float
    features: dict[str, float]
    explanation: str
    severity: str
    timestamp: datetime
    model: str
    inference_time_us: int


def require_inference_url() -> str:
    if not INFERENCE_ENGINE_URL:
        raise HTTPException(status_code=503, detail="INFERENCE_ENGINE_URL is required for trained anomaly inference")
    return INFERENCE_ENGINE_URL


def inference_payload(record: VotingRecord) -> tuple[dict[str, Any], dict[str, float]]:
    if len(record.party_results) < 2:
        raise HTTPException(status_code=422, detail="party_results must contain at least two actual party totals")
    if any(v < 0 for v in record.party_results.values()):
        raise HTTPException(status_code=422, detail="party_results must not contain negative values")

    ordered_party_totals = sorted(record.party_results.values(), reverse=True)
    total_valid_votes = sum(record.party_results.values())
    if total_valid_votes <= 0:
        raise HTTPException(status_code=422, detail="party_results must contain at least one valid vote")

    turnout = record.accredited_voters / record.registered_voters
    payload = {
        "polling_unit_code": record.polling_unit_id,
        "registered_voters": record.registered_voters,
        "accredited_voters": record.accredited_voters,
        "total_valid_votes": total_valid_votes,
        "rejected_votes": record.rejected_votes,
        "party_a_votes": ordered_party_totals[0],
        "party_b_votes": ordered_party_totals[1],
        "benford_deviation": record.benford_deviation,
        "submission_delay_hours": record.submission_delay_min / 60.0,
        "regional_mean_turnout": record.regional_mean_turnout,
    }
    features = {
        "votes_cast": float(record.votes_cast),
        "registered_voters": float(record.registered_voters),
        "accredited_voters": float(record.accredited_voters),
        "turnout_pct": turnout * 100.0,
        "submission_delay_min": record.submission_delay_min,
        "rejected_votes": float(record.rejected_votes),
        "benford_deviation": record.benford_deviation,
        "regional_mean_turnout": record.regional_mean_turnout,
    }
    return payload, features


def severity_for_score(score: float) -> str:
    if score >= 0.85:
        return "critical"
    if score >= 0.65:
        return "high"
    if score >= 0.40:
        return "medium"
    return "low"


def explanation_for_result(result: dict[str, Any]) -> str:
    factors = result.get("risk_factors", [])
    if factors:
        return "; ".join(str(factor.get("factor", "model risk factor")) for factor in factors)
    return "trained CPU ONNX model found no named risk factor"


def alert_from_result(record: VotingRecord, features: dict[str, float], result: dict[str, Any]) -> AnomalyAlert:
    score = float(result["anomaly_score"])
    return AnomalyAlert(
        polling_unit_id=record.polling_unit_id,
        state=record.state,
        anomaly_score=score,
        is_anomaly=bool(result["is_anomaly"]),
        confidence=float(result["confidence"]),
        features=features,
        explanation=explanation_for_result(result),
        severity=severity_for_score(score),
        timestamp=datetime.now(timezone.utc),
        model=str(result["model"]),
        inference_time_us=int(result.get("inference_time_us", 0)),
    )


async def call_inference(path: str, payload: dict[str, Any]) -> dict[str, Any]:
    base_url = require_inference_url()
    try:
        async with httpx.AsyncClient(timeout=20.0) as client:
            response = await client.post(f"{base_url}{path}", json=payload)
            response.raise_for_status()
            return response.json()
    except httpx.HTTPStatusError as exc:
        raise HTTPException(status_code=503, detail=f"trained anomaly inference returned HTTP {exc.response.status_code}") from exc
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=503, detail="trained anomaly inference service is unavailable") from exc


async def score_record(record: VotingRecord) -> AnomalyAlert:
    payload, features = inference_payload(record)
    result = await call_inference("/anomaly/predict", payload)
    return alert_from_result(record, features, result)


@app.post("/api/v1/anomaly/score", response_model=AnomalyAlert)
async def score_voting_record(record: VotingRecord):
    alert = await score_record(record)
    if alert.is_anomaly:
        await broadcast_alert(alert.model_dump(mode="json"))
    return alert


@app.post("/api/v1/anomaly/batch")
async def score_batch(records: list[VotingRecord]):
    if not records:
        raise HTTPException(status_code=422, detail="at least one voting record is required")
    if len(records) > 50_000:
        raise HTTPException(status_code=413, detail="batch exceeds trained inference service limit")

    mapped = [inference_payload(record) for record in records]
    result = await call_inference("/anomaly/batch", {"polling_units": [payload for payload, _ in mapped]})
    model_results = result.get("results", [])
    if len(model_results) != len(records):
        raise HTTPException(status_code=503, detail="trained anomaly inference returned an incomplete batch")
    alerts = [alert_from_result(record, features, model_result)
              for record, (_, features), model_result in zip(records, mapped, model_results)]
    anomalies = [alert for alert in alerts if alert.is_anomaly]
    for alert in anomalies:
        await broadcast_alert(alert.model_dump(mode="json"))
    return {"total": len(records), "anomalies": len(anomalies), "alerts": anomalies}


@app.get("/api/v1/anomaly/health")
async def health():
    base_url = require_inference_url()
    try:
        async with httpx.AsyncClient(timeout=5.0) as client:
            response = await client.get(f"{base_url}/health")
            response.raise_for_status()
            inference_health = response.json()
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=503, detail="trained anomaly inference service is unavailable") from exc
    if not inference_health.get("models", {}).get("anomaly_xgboost", False):
        raise HTTPException(status_code=503, detail="trained anomaly ONNX model is unavailable")
    return {"status": "healthy", "model_trained": True, "inference": inference_health}


@app.websocket("/ws/anomalies")
async def websocket_anomalies(websocket: WebSocket):
    await websocket.accept()
    connected_clients.append(websocket)
    try:
        while True:
            await asyncio.sleep(30)
            await websocket.send_json({"type": "ping"})
    except WebSocketDisconnect:
        if websocket in connected_clients:
            connected_clients.remove(websocket)


async def broadcast_alert(alert: dict[str, Any]):
    disconnected: list[WebSocket] = []
    for client in connected_clients:
        try:
            await client.send_json({"type": "anomaly_alert", "data": alert})
        except Exception:
            disconnected.append(client)
    for client in disconnected:
        if client in connected_clients:
            connected_clients.remove(client)


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=int(os.getenv("PORT", "8200")), log_level="info")
