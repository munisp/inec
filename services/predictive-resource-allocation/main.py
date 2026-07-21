"""INEC resource allocation service using persisted election history.

The turnout estimator is trained only from finalized or validated historical
results joined with polling-unit registration data. It never synthesizes
training samples. A persisted model is required for prediction; operators may
refresh it after new real election data has been ingested.
"""

from __future__ import annotations

import asyncio
import math
import os
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import asyncpg
import joblib
import numpy as np
import uvicorn
from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field
from sklearn.ensemble import HistGradientBoostingRegressor
from sklearn.preprocessing import StandardScaler

DATABASE_URL = os.getenv("DATABASE_URL", "").strip()
MODEL_DIR = Path(os.getenv("MODEL_DIR", "").strip()) if os.getenv("MODEL_DIR", "").strip() else None
MIN_TRAINING_SAMPLES = int(os.getenv("MIN_TRAINING_SAMPLES", "0"))
CORS_ORIGINS = [value.strip() for value in os.getenv("CORS_ORIGINS", "").split(",") if value.strip()]
BALLOT_BUFFER = float(os.getenv("RESOURCE_BALLOT_BUFFER", "nan"))
VOTERS_PER_CLERK = int(os.getenv("RESOURCE_VOTERS_PER_CLERK", "0"))
MIN_POLL_CLERKS = int(os.getenv("RESOURCE_MIN_POLL_CLERKS", "0"))
MIN_SECURITY_PERSONNEL = int(os.getenv("RESOURCE_MIN_SECURITY_PERSONNEL", "0"))

FEATURE_NAMES = [
    "registered_voters",
    "prior_turnout",
    "prior_vote_to_accredited_ratio",
    "prior_rejection_ratio",
    "submission_hour_fraction",
]
MODEL_FILENAME = "turnout_history_model.joblib"

app = FastAPI(
    title="INEC Predictive Resource Allocation AI",
    description="Resource planning using a persisted model trained from real historical election results",
    version="2.0.0",
)
app.add_middleware(
    CORSMiddleware,
    allow_origins=CORS_ORIGINS,
    allow_methods=["GET", "POST"],
    allow_headers=["Content-Type", "Authorization", "X-Request-ID"],
    allow_credentials=True,
)


@dataclass
class ModelState:
    model: HistGradientBoostingRegressor | None = None
    scaler: StandardScaler | None = None
    trained_at: str | None = None
    training_samples: int = 0

    @property
    def path(self) -> Path:
        if MODEL_DIR is None:
            raise RuntimeError("MODEL_DIR must be configured")
        return MODEL_DIR / MODEL_FILENAME

    def load(self) -> bool:
        if not self.path.exists():
            return False
        payload = joblib.load(self.path)
        if payload.get("feature_names") != FEATURE_NAMES:
            raise RuntimeError("persisted resource allocation model uses an incompatible feature schema")
        self.model = payload["model"]
        self.scaler = payload["scaler"]
        self.trained_at = payload["trained_at"]
        self.training_samples = int(payload["training_samples"])
        return True

    def persist(self) -> None:
        if self.model is None or self.scaler is None or not self.trained_at:
            raise RuntimeError("cannot persist an untrained turnout model")
        self.path.parent.mkdir(parents=True, exist_ok=True)
        joblib.dump(
            {
                "model": self.model,
                "scaler": self.scaler,
                "trained_at": self.trained_at,
                "training_samples": self.training_samples,
                "feature_names": FEATURE_NAMES,
            },
            self.path,
        )

    def ready(self) -> bool:
        return self.model is not None and self.scaler is not None


state = ModelState()


class PollingUnitFeatures(BaseModel):
    polling_unit_id: str = Field(min_length=1)
    state: str = Field(min_length=1)
    lga: str = Field(min_length=1)
    registered_voters: int = Field(gt=0)
    prev_turnout: float = Field(ge=0, le=1, description="Prior official accredited-voter turnout fraction")
    prior_vote_to_accredited_ratio: float = Field(ge=0, le=1, description="Prior official valid-vote to accredited-voter ratio")
    prior_rejection_ratio: float = Field(ge=0, le=1, description="Prior official rejected-vote to accredited-voter ratio")
    prior_submission_hour_fraction: float = Field(ge=0, le=1, description="Prior official submission hour divided by 24")
    lat: float
    lon: float


class AllocationRequest(BaseModel):
    election_id: str = Field(min_length=1)
    polling_units: list[PollingUnitFeatures] = Field(min_length=1, max_length=50_000)
    depot_lat: float
    depot_lon: float
    num_vehicles: int = Field(gt=0, le=10_000)


class TrainingResponse(BaseModel):
    status: str
    training_samples: int
    trained_at: str
    model_path: str


def required_configuration() -> None:
    if not DATABASE_URL:
        raise RuntimeError("DATABASE_URL must be configured")
    if MODEL_DIR is None:
        raise RuntimeError("MODEL_DIR must be configured")
    if MIN_TRAINING_SAMPLES < 10:
        raise RuntimeError("MIN_TRAINING_SAMPLES must be at least 10")
    if not 0 <= BALLOT_BUFFER <= 1:
        raise RuntimeError("RESOURCE_BALLOT_BUFFER must be between 0 and 1")
    if VOTERS_PER_CLERK <= 0 or MIN_POLL_CLERKS <= 0 or MIN_SECURITY_PERSONNEL <= 0:
        raise RuntimeError("resource staffing policy variables must be positive")


async def historical_training_matrix() -> np.ndarray:
    """Return real historical feature rows, excluding PUs without a prior result."""
    query = """
        WITH history AS (
            SELECT
                r.polling_unit_code,
                p.registered_voters::double precision AS registered_voters,
                r.accredited_voters::double precision AS accredited_voters,
                r.total_votes_cast::double precision AS total_votes_cast,
                r.rejected_votes::double precision AS rejected_votes,
                r.submitted_at,
                LAG(r.accredited_voters::double precision / NULLIF(p.registered_voters, 0))
                    OVER (PARTITION BY r.polling_unit_code ORDER BY r.election_id, r.submitted_at) AS prior_turnout,
                LAG(r.total_votes_cast::double precision / NULLIF(r.accredited_voters, 0))
                    OVER (PARTITION BY r.polling_unit_code ORDER BY r.election_id, r.submitted_at) AS prior_vote_to_accredited_ratio,
                LAG(r.rejected_votes::double precision / NULLIF(r.accredited_voters, 0))
                    OVER (PARTITION BY r.polling_unit_code ORDER BY r.election_id, r.submitted_at) AS prior_rejection_ratio
            FROM results r
            JOIN polling_units p ON p.code = r.polling_unit_code
            WHERE r.status IN ('validated', 'finalized')
              AND p.registered_voters > 0
              AND r.accredited_voters >= 0
              AND r.submitted_at IS NOT NULL
        )
        SELECT registered_voters, prior_turnout, prior_vote_to_accredited_ratio,
               prior_rejection_ratio,
               EXTRACT(HOUR FROM submitted_at)::double precision / 24.0 AS submission_hour_fraction,
               accredited_voters / NULLIF(registered_voters, 0) AS observed_turnout
        FROM history
        WHERE prior_turnout IS NOT NULL
          AND prior_vote_to_accredited_ratio IS NOT NULL
          AND prior_rejection_ratio IS NOT NULL
          AND accredited_voters <= registered_voters
    """
    connection = await asyncpg.connect(DATABASE_URL)
    try:
        rows = await connection.fetch(query)
    finally:
        await connection.close()
    if not rows:
        return np.empty((0, len(FEATURE_NAMES) + 1), dtype=np.float64)
    return np.asarray([tuple(float(value) for value in row.values()) for row in rows], dtype=np.float64)


async def refresh_model() -> TrainingResponse:
    data = await historical_training_matrix()
    if len(data) < MIN_TRAINING_SAMPLES:
        raise HTTPException(
            status_code=503,
            detail=(
                f"insufficient real historical results for turnout model: "
                f"need {MIN_TRAINING_SAMPLES}, found {len(data)}"
            ),
        )
    features = data[:, :-1]
    labels = data[:, -1]
    scaler = StandardScaler()
    model = HistGradientBoostingRegressor(
        learning_rate=0.05,
        max_iter=300,
        max_leaf_nodes=31,
        l2_regularization=0.1,
    )
    model.fit(scaler.fit_transform(features), labels)
    state.model = model
    state.scaler = scaler
    state.training_samples = len(data)
    state.trained_at = datetime.now(timezone.utc).isoformat()
    state.persist()
    return TrainingResponse(
        status="trained",
        training_samples=state.training_samples,
        trained_at=state.trained_at,
        model_path=str(state.path),
    )


def model_features(pu: PollingUnitFeatures) -> np.ndarray:
    """Use only prior official result attributes supplied by the authorised planner."""
    return np.array(
        [[
            float(pu.registered_voters),
            pu.prev_turnout,
            pu.prior_vote_to_accredited_ratio,
            pu.prior_rejection_ratio,
            pu.prior_submission_hour_fraction,
        ]],
        dtype=np.float64,
    )


def predict_turnout(pu: PollingUnitFeatures) -> float:
    if not state.ready():
        raise HTTPException(status_code=503, detail="real historical turnout model is not trained")
    assert state.model is not None and state.scaler is not None
    predicted = float(state.model.predict(state.scaler.transform(model_features(pu)))[0])
    if not math.isfinite(predicted):
        raise HTTPException(status_code=503, detail="turnout model returned a non-finite value")
    return max(0.0, min(1.0, predicted))


def calculate_ballot_papers(registered: int, predicted_turnout: float) -> int:
    return math.ceil(registered * predicted_turnout * (1.0 + BALLOT_BUFFER))


def calculate_staff_allocation(registered: int, predicted_turnout: float) -> dict[str, int]:
    expected_voters = math.ceil(registered * predicted_turnout)
    poll_clerks = max(MIN_POLL_CLERKS, math.ceil(expected_voters / VOTERS_PER_CLERK))
    security = max(MIN_SECURITY_PERSONNEL, math.ceil(expected_voters / VOTERS_PER_CLERK))
    return {
        "presiding_officers": 1,
        "poll_clerks": poll_clerks,
        "security_personnel": security,
        "total_staff": 1 + poll_clerks + security,
    }


def greedy_vehicle_routing(polling_units: list[dict[str, Any]], depot: dict[str, float], num_vehicles: int) -> list[dict[str, Any]]:
    """Deterministic balanced nearest-depot allocation using submitted coordinates."""
    routes: list[dict[str, Any]] = [
        {"vehicle_id": index + 1, "stops": [], "total_distance_km": 0.0}
        for index in range(num_vehicles)
    ]
    for pu in sorted(polling_units, key=lambda item: (item["polling_unit_id"], item["lat"], item["lon"])):
        distance = math.hypot(pu["lat"] - depot["lat"], pu["lon"] - depot["lon"]) * 111.0
        route = min(routes, key=lambda item: (item["total_distance_km"], item["vehicle_id"]))
        route["stops"].append({
            "polling_unit_id": pu["polling_unit_id"],
            "lat": pu["lat"],
            "lon": pu["lon"],
            "ballot_papers": pu["ballot_papers"],
        })
        route["total_distance_km"] += distance
    return routes


@app.on_event("startup")
async def startup() -> None:
    required_configuration()
    try:
        state.load()
    except FileNotFoundError:
        pass


@app.post("/api/v1/allocation/train", response_model=TrainingResponse)
async def train_from_historical_results() -> TrainingResponse:
    return await refresh_model()


@app.post("/api/v1/allocation/predict")
async def predict_allocation(req: AllocationRequest):
    if not state.ready():
        raise HTTPException(status_code=503, detail="real historical turnout model is not trained; invoke /api/v1/allocation/train after data ingestion")
    allocations: list[dict[str, Any]] = []
    total_ballot_papers = 0
    total_staff = 0
    for pu in req.polling_units:
        turnout = predict_turnout(pu)
        ballot_papers = calculate_ballot_papers(pu.registered_voters, turnout)
        staff = calculate_staff_allocation(pu.registered_voters, turnout)
        total_ballot_papers += ballot_papers
        total_staff += staff["total_staff"]
        allocations.append({
            "polling_unit_id": pu.polling_unit_id,
            "state": pu.state,
            "lga": pu.lga,
            "registered_voters": pu.registered_voters,
            "predicted_turnout_pct": round(turnout * 100.0, 2),
            "expected_voters": math.ceil(pu.registered_voters * turnout),
            "ballot_papers": ballot_papers,
            "staff": staff,
            "lat": pu.lat,
            "lon": pu.lon,
        })
    routes = greedy_vehicle_routing(allocations, {"lat": req.depot_lat, "lon": req.depot_lon}, req.num_vehicles)
    return {
        "election_id": req.election_id,
        "total_polling_units": len(allocations),
        "total_ballot_papers": total_ballot_papers,
        "total_staff_required": total_staff,
        "vehicle_routes": routes,
        "allocations": allocations,
        "model": {"trained_at": state.trained_at, "training_samples": state.training_samples},
        "generated_at": datetime.now(timezone.utc).isoformat(),
    }


@app.post("/api/v1/allocation/single")
async def predict_single(pu: PollingUnitFeatures):
    turnout = predict_turnout(pu)
    return {
        "polling_unit_id": pu.polling_unit_id,
        "predicted_turnout_pct": round(turnout * 100.0, 2),
        "ballot_papers": calculate_ballot_papers(pu.registered_voters, turnout),
        "staff": calculate_staff_allocation(pu.registered_voters, turnout),
        "model": {"trained_at": state.trained_at, "training_samples": state.training_samples},
    }


@app.get("/api/v1/allocation/health")
async def health():
    return {
        "status": "healthy" if state.ready() else "model_untrained",
        "model_trained": state.ready(),
        "training_samples": state.training_samples,
        "trained_at": state.trained_at,
        "requires_real_historical_data": True,
    }


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=int(os.getenv("PORT", "8205")), log_level="info")
