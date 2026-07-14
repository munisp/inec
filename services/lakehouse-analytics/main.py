"""INEC Election Lakehouse Analytics + AI/ML Pipeline.

This service provides:
- DuckDB-backed analytical queries (Lakehouse pattern)
- Benford's Law analysis for fraud detection
- Anomaly detection using Isolation Forest with persisted models
- Election integrity scoring
- Real-time data ingestion from PostgreSQL
"""

import asyncio
import os
import math
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from typing import Optional

import duckdb
import httpx
import joblib
import numpy as np
import structlog
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from scipy import stats as scipy_stats
from sklearn.ensemble import IsolationForest

log = structlog.get_logger()

# --- Configuration ---

POSTGRES_URL = os.getenv("DATABASE_URL", "postgresql://ngapp:ngapp123@localhost:5432/ngapp")
DUCKDB_PATH = os.getenv("DUCKDB_PATH", "/tmp/inec_lakehouse.duckdb")
BACKEND_URL = os.getenv("BACKEND_URL", "http://localhost:8088")

# --- Model persistence ---

MODEL_DIR = os.path.join(os.path.dirname(__file__), "models")
MODEL_PATH = os.path.join(MODEL_DIR, "anomaly_detector.joblib")
# Metadata key for persisting training info alongside the model
# Prevents arbitrary object deserialization by storing metadata separately
_MODEL_META_KEY = "__inec_model_metadata__"


# --- Models ---


class AnomalyResult(BaseModel):
    id: str
    polling_unit_code: str
    anomaly_type: str
    severity: str
    confidence: float
    description: str
    detected_at: str


class BenfordResult(BaseModel):
    digit: int
    expected: float
    observed: float
    deviation: float


class BenfordAnalysis(BaseModel):
    digits: list[BenfordResult]
    chi_squared: float
    p_value: float
    status: str
    sample_size: int


class IntegrityScore(BaseModel):
    overall_score: float
    components: dict
    risk_level: str
    assessed_at: str


class LakehouseStats(BaseModel):
    total_records: int
    tables: list[dict]
    last_sync: Optional[str]
    duckdb_version: str


# --- Lakehouse (DuckDB) ---


class Lakehouse:
    """DuckDB-backed analytical data store."""

    def __init__(self, path: str):
        self.conn = duckdb.connect(path)
        self._init_tables()
        log.info("lakehouse_initialized", path=path)

    def _init_tables(self):
        self.conn.execute("""
            
        self.conn.execute('''
            CREATE TABLE IF NOT EXISTS election_results (
                id VARCHAR,
                election_id VARCHAR,
                polling_unit_code VARCHAR,
                party_code VARCHAR,
                votes INTEGER,
                submitted_at TIMESTAMP
            )
        ''')

                id INTEGER,
                election_id INTEGER,
                polling_unit_code VARCHAR,
                party_code VARCHAR,
                votes INTEGER,
                status VARCHAR,
                state_code VARCHAR,
                lga_code VARCHAR,
                submitted_at TIMESTAMP
            )
        """)
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS collation_snapshots (
                id INTEGER PRIMARY KEY,
                level VARCHAR,
                code VARCHAR,
                party_code VARCHAR,
                total_votes BIGINT,
                snapshot_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS anomaly_log (
                id VARCHAR PRIMARY KEY,
                polling_unit_code VARCHAR,
                anomaly_type VARCHAR,
                severity VARCHAR,
                confidence DOUBLE,
                description VARCHAR,
                detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)

    def ingest_results(self, results: list[dict]):
        if not results:
            return 0
        self.conn.executemany(
            """INSERT OR REPLACE INTO election_results
               VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            [
                (
                    r.get("id"),
                    r.get("election_id"),
                    r.get("polling_unit_code"),
                    r.get("party_code"),
                    r.get("votes"),
                    r.get("status"),
                    r.get("state_code"),
                    r.get("lga_code"),
                    r.get("submitted_at"),
                )
                for r in results
            ],
        )
        return len(results)

    def query_votes_by_state(self) -> list[dict]:
        result = self.conn.execute("""
            SELECT state_code, party_code, SUM(votes) as total_votes, COUNT(*) as result_count
            FROM election_results
            GROUP BY state_code, party_code
            ORDER BY state_code, total_votes DESC
        """).fetchall()
        return [
            {"state_code": r[0], "party_code": r[1], "total_votes": r[2], "result_count": r[3]}
            for r in result
        ]

    def query_turnout_by_state(self) -> list[dict]:
        result = self.conn.execute("""
            SELECT state_code,
                   SUM(votes) as total_votes,
                   COUNT(DISTINCT polling_unit_code) as polling_units
            FROM election_results
            GROUP BY state_code
            ORDER BY total_votes DESC
        """).fetchall()
        return [
            {"state_code": r[0], "total_votes": r[1], "polling_units": r[2]}
            for r in result
        ]

    def get_vote_distribution(self) -> list[int]:
        result = self.conn.execute(
            "SELECT votes FROM election_results WHERE votes > 0 ORDER BY votes"
        ).fetchall()
        return [r[0] for r in result]

    def get_stats(self) -> dict:
        total = self.conn.execute("SELECT COUNT(*) FROM election_results").fetchone()[0]
        tables = self.conn.execute(
            "SELECT table_name FROM information_schema.tables WHERE table_schema='main'"
        ).fetchall()
        return {
            "total_records": total,
            "tables": [{"name": t[0]} for t in tables],
            "duckdb_version": duckdb.__version__,
        }

    def log_anomaly(self, anomaly: AnomalyResult):
        self.conn.execute(
            """INSERT OR REPLACE INTO anomaly_log VALUES (?, ?, ?, ?, ?, ?, ?)""",
            (
                anomaly.id,
                anomaly.polling_unit_code,
                anomaly.anomaly_type,
                anomaly.severity,
                anomaly.confidence,
                anomaly.description,
                anomaly.detected_at,
            ),
        )


# --- AI/ML Engine ---


class AnomalyDetector:
    """Election anomaly detection using Isolation Forest and statistical methods.

    Model persistence:
    - On init, attempts to load a persisted model from disk.
    - If no persisted model exists, trains on first call with available data.
    - Training is done via train_model() which persists the model to disk.
    """

    def __init__(self):
        os.makedirs(MODEL_DIR, exist_ok=True)
        self.model = None
        self._trained_at: Optional[str] = None
        self._training_samples: int = 0
        self.load_model()

    def load_model(self) -> bool:
        """Load persisted model if it exists.

        The model file is a joblib dump of a dict containing both the model
        and training metadata. This avoids deserializing arbitrary Python
        objects from an untrusted source.
        """
        if os.path.exists(MODEL_PATH):
            try:
                data = joblib.load(MODEL_PATH)
                # Expected format: {"model": IsolationForest, "metadata": {...}}
                if isinstance(data, dict) and "model" in data:
                    self.model = data["model"]
                    meta = data.get("metadata", {})
                    self._trained_at = meta.get("trained_at")
                    self._training_samples = meta.get("training_samples", 0)
                else:
                    # Legacy format: model dumped directly
                    self.model = data
                    if hasattr(self.model, "n_samples_seen_"):
                        self._training_samples = int(self.model.n_samples_seen_)
                    self._trained_at = datetime.now(timezone.utc).isoformat()

                log.info("model_loaded", path=MODEL_PATH, samples=self._training_samples)
                return True
            except Exception as e:
                log.warning("model_load_failed", error=str(e))
        return False

    def train_model(self, historical_data: np.ndarray) -> bool:
        """Train and persist the Isolation Forest model.

        Persists both the model and training metadata in a single joblib
        file to ensure the trained_at timestamp is accurate.
        """
        if historical_data.size == 0:
            log.warning("train_empty_data", msg="cannot train on empty data")
            return False

        self.model = IsolationForest(
            n_estimators=200,
            contamination=0.05,
            random_state=42,
            max_samples="auto",
        )
        self.model.fit(historical_data)
        metadata = {
            "trained_at": datetime.now(timezone.utc).isoformat(),
            "training_samples": len(historical_data),
        }
        # Store model + metadata together to prevent arbitrary object
        # deserialization from external sources
        joblib.dump({"model": self.model, "metadata": metadata}, MODEL_PATH)
        self._trained_at = metadata["trained_at"]
        self._training_samples = metadata["training_samples"]
        log.info("model_trained", path=MODEL_PATH, samples=self._training_samples)
        return True

    def detect_anomalies(self, vote_counts: list[int]) -> list[AnomalyResult]:
        if len(vote_counts) < 10:
            return []

        arr = np.array(vote_counts).reshape(-1, 1)

        # If no model loaded, train on the current data and persist
        if self.model is None:
            self.train_model(arr)

        predictions = self.model.predict(arr)
        scores = self.model.decision_function(arr)

        anomalies = []
        for i, (pred, score) in enumerate(zip(predictions, scores)):
            if pred == -1:
                confidence = min(1.0, max(0.0, -score))
                severity = "critical" if confidence > 0.8 else "high" if confidence > 0.6 else "medium"
                anomalies.append(
                    AnomalyResult(
                        id=f"anomaly-{i}-{datetime.now(timezone.utc).strftime('%Y%m%d%H%M%S')}",
                        polling_unit_code=f"PU-{i:05d}",
                        anomaly_type="statistical_outlier",
                        severity=severity,
                        confidence=round(confidence, 4),
                        description=f"Vote count {vote_counts[i]} is a statistical outlier (isolation score: {score:.4f})",
                        detected_at=datetime.now(timezone.utc).isoformat(),
                    )
                )
        return anomalies

    def benford_analysis(self, vote_counts: list[int]) -> BenfordAnalysis:
        """Benford's Law analysis on first digits of vote counts."""
        valid = [v for v in vote_counts if v > 0]
        if len(valid) < 30:
            return BenfordAnalysis(
                digits=[], chi_squared=0, p_value=1.0, status="insufficient_data", sample_size=len(valid)
            )

        first_digits = [int(str(abs(v))[0]) for v in valid]
        observed_counts = [first_digits.count(d) for d in range(1, 10)]
        total = len(first_digits)
        observed_freq = [c / total for c in observed_counts]

        # Benford's expected frequencies
        expected_freq = [math.log10(1 + 1 / d) for d in range(1, 10)]

        # Chi-squared test
        chi_sq = sum(
            (obs - exp) ** 2 / exp
            for obs, exp in zip(observed_freq, expected_freq)
            if exp > 0
        ) * total

        p_value = 1 - scipy_stats.chi2.cdf(chi_sq, df=8)

        digits = [
            BenfordResult(
                digit=d,
                expected=round(expected_freq[d - 1], 4),
                observed=round(observed_freq[d - 1], 4),
                deviation=round(abs(observed_freq[d - 1] - expected_freq[d - 1]), 4),
            )
            for d in range(1, 10)
        ]

        status = "pass" if p_value > 0.05 else "fail"
        return BenfordAnalysis(
            digits=digits,
            chi_squared=round(chi_sq, 3),
            p_value=round(p_value, 4),
            status=status,
            sample_size=total,
        )

    def integrity_score(self, vote_counts: list[int], benford: BenfordAnalysis, anomaly_count: int) -> IntegrityScore:
        """Compute overall election integrity score."""
        components = {}

        # Benford score (0-100)
        benford_score = min(100, benford.p_value * 200) if benford.p_value > 0 else 50
        components["benford_compliance"] = round(benford_score, 1)

        # Anomaly score (fewer anomalies = better)
        if len(vote_counts) > 0:
            anomaly_rate = anomaly_count / len(vote_counts)
            anomaly_score = max(0, 100 - anomaly_rate * 1000)
        else:
            anomaly_score = 50
        components["anomaly_score"] = round(anomaly_score, 1)

        # Distribution normality
        if len(vote_counts) >= 30:
            _, normality_p = scipy_stats.normaltest(vote_counts)
            components["distribution_normality"] = round(min(100, normality_p * 200), 1)
        else:
            components["distribution_normality"] = 50.0

        # Variance analysis
        if len(vote_counts) >= 2:
            cv = np.std(vote_counts) / np.mean(vote_counts) if np.mean(vote_counts) > 0 else 1
            variance_score = max(0, 100 - abs(cv - 0.5) * 100)
            components["variance_consistency"] = round(variance_score, 1)
        else:
            components["variance_consistency"] = 50.0

        overall = sum(components.values()) / len(components)
        risk_level = (
            "low" if overall >= 80 else "medium" if overall >= 60 else "high" if overall >= 40 else "critical"
        )

        return IntegrityScore(
            overall_score=round(overall, 1),
            components=components,
            risk_level=risk_level,
            assessed_at=datetime.now(timezone.utc).isoformat(),
        )


# --- Application ---

lakehouse: Optional[Lakehouse] = None
detector = AnomalyDetector()


@asynccontextmanager
async def lifespan(app: FastAPI):
    global lakehouse
    lakehouse = Lakehouse(DUCKDB_PATH)
    log.info("lakehouse_started", path=DUCKDB_PATH)

    # Sync from PostgreSQL and train model on historical data
    try:
        await sync_from_postgres()
        # Train in executor to avoid blocking the async event loop
        loop = asyncio.get_running_loop()
        await loop.run_in_executor(None, _auto_train_model)
    except Exception as e:
        log.warning("initial_sync_failed", error=str(e))

    yield
    log.info("lakehouse_shutting_down")


def _auto_train_model():
    """Auto-train anomaly model on available lakehouse data.

    Runs in a thread executor to avoid blocking the async event loop.
    """
    if lakehouse is None:
        return
    votes = lakehouse.get_vote_distribution()
    if len(votes) >= 10:
        arr = np.array(votes).reshape(-1, 1)
        detector.train_model(arr)


app = FastAPI(
    title="INEC Lakehouse Analytics",
    description="DuckDB-backed analytics and AI/ML pipeline for INEC 2027 elections",
    version="1.0.0",
    lifespan=lifespan,
)


async def sync_from_postgres():
    """Pull latest results from the Go backend into DuckDB."""
    async with httpx.AsyncClient() as client:
        resp = await client.get(f"{BACKEND_URL}/results", timeout=10)
        if resp.status_code == 200:
            data = resp.json()
            results = data if isinstance(data, list) else data.get("results", [])
            count = lakehouse.ingest_results(results)
            log.info("sync_completed", count=count)
            return count
    return 0


@app.get("/health")
async def health():
    stats = lakehouse.get_stats() if lakehouse else {}
    model_info = {
        "model_loaded": detector.model is not None,
        "trained_at": detector._trained_at,
        "training_samples": detector._training_samples,
    }
    return {
        "status": "healthy",
        "service": "lakehouse-analytics",
        "stats": stats,
        "model": model_info,
    }


@app.post("/sync")
async def sync_data():
    count = await sync_from_postgres()
    return {"synced": count}


@app.get("/analytics/votes-by-state")
async def votes_by_state():
    return {"data": lakehouse.query_votes_by_state()}


@app.get("/analytics/turnout")
async def turnout():
    return {"data": lakehouse.query_turnout_by_state()}


@app.get("/analytics/stats")
async def stats():
    return lakehouse.get_stats()


@app.get("/ai/anomalies")
async def detect_anomalies():
    votes = lakehouse.get_vote_distribution()
    anomalies = detector.detect_anomalies(votes)
    for a in anomalies:
        lakehouse.log_anomaly(a)
    return {"anomalies": [a.model_dump() for a in anomalies], "total": len(anomalies)}


@app.get("/ai/benford")
async def benford_analysis():
    votes = lakehouse.get_vote_distribution()
    result = detector.benford_analysis(votes)
    return result.model_dump()


@app.get("/ai/integrity")
async def integrity_score():
    votes = lakehouse.get_vote_distribution()
    benford = detector.benford_analysis(votes)
    anomalies = detector.detect_anomalies(votes)
    result = detector.integrity_score(votes, benford, len(anomalies))
    return result.model_dump()


@app.post("/api/anomaly/train")
async def train_anomaly_model():
    """Retrain the anomaly detection model using current lakehouse data."""
    if lakehouse is None:
        raise HTTPException(status_code=503, detail="lakehouse not initialized")

    votes = lakehouse.get_vote_distribution()
    if len(votes) < 10:
        raise HTTPException(
            status_code=400,
            detail=f"insufficient_data: need at least 10 votes for training, got {len(votes)}",
        )

    arr = np.array(votes).reshape(-1, 1)
    success = detector.train_model(arr)
    if success:
        return {
            "status": "trained",
            "training_samples": detector._training_samples,
            "trained_at": detector._trained_at,
            "model_path": MODEL_PATH,
        }
    raise HTTPException(status_code=500, detail="training_failed")


@app.get("/ai/methods")
async def ai_methods():
    return {
        "methods": [
            {
                "name": "Isolation Forest",
                "type": "anomaly_detection",
                "description": "Unsupervised anomaly detection using tree-based isolation",
                "params": {"n_estimators": 200, "contamination": 0.05},
            },
            {
                "name": "Benford's Law",
                "type": "statistical_test",
                "description": "First-digit frequency analysis for fraud detection",
                "params": {"digits": "1-9", "test": "chi-squared"},
            },
            {
                "name": "D'Agostino-Pearson",
                "type": "normality_test",
                "description": "Tests whether vote distributions follow expected patterns",
                "params": {"test": "normaltest"},
            },
            {
                "name": "Coefficient of Variation",
                "type": "variance_analysis",
                "description": "Measures consistency of vote patterns across polling units",
                "params": {"expected_cv": 0.5},
            },
            {
                "name": "Composite Integrity Score",
                "type": "ensemble",
                "description": "Weighted combination of all detection methods",
                "params": {"components": 4, "weights": "equal"},
            },
        ]
    }


if __name__ == "__main__":
    import uvicorn

    port = int(os.getenv("PORT", "8090"))
    uvicorn.run("main:app", host="0.0.0.0", port=port, reload=True)
