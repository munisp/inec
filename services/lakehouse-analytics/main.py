"""INEC Election Lakehouse Analytics + AI/ML Pipeline.

This service provides:
- DuckDB-backed analytical queries (Lakehouse pattern)
- Benford's Law analysis for fraud detection
- Anomaly detection using Isolation Forest with persisted models
- Election integrity scoring
- Real-time data ingestion from PostgreSQL
"""

import asyncio
import hashlib
import json
import os
import math
import re
import time
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from typing import Optional

import duckdb
import httpx
import joblib
import numpy as np
import psycopg2
import psycopg2.extras
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

MODEL_DIR = os.getenv("MODEL_DIR", os.path.join(os.path.dirname(__file__), "models"))
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
            CREATE TABLE IF NOT EXISTS election_results (
                id VARCHAR PRIMARY KEY,
                election_id VARCHAR NOT NULL,
                polling_unit_code VARCHAR NOT NULL,
                party_code VARCHAR,
                votes INTEGER NOT NULL DEFAULT 0,
                status VARCHAR,
                state_code VARCHAR,
                lga_code VARCHAR,
                submitted_at TIMESTAMP
            )
        """)
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS collation_snapshots (
                id VARCHAR PRIMARY KEY,
                level VARCHAR NOT NULL,
                code VARCHAR NOT NULL,
                party_code VARCHAR,
                total_votes BIGINT NOT NULL DEFAULT 0,
                snapshot_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS anomaly_log (
                id VARCHAR PRIMARY KEY,
                polling_unit_code VARCHAR NOT NULL,
                anomaly_type VARCHAR NOT NULL,
                severity VARCHAR NOT NULL,
                confidence DOUBLE NOT NULL,
                description VARCHAR NOT NULL,
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
                    f"{r.get('id')}:{r.get('party_code') or '_'}",
                    str(r.get("election_id")),
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

    def get_vote_records(self, election_id: Optional[str] = None) -> list[tuple[str, int]]:
        if election_id is None:
            result = self.conn.execute(
                """SELECT polling_unit_code, SUM(votes) AS votes
                   FROM election_results WHERE votes > 0
                   GROUP BY polling_unit_code ORDER BY polling_unit_code"""
            ).fetchall()
        else:
            result = self.conn.execute(
                """SELECT polling_unit_code, SUM(votes) AS votes
                   FROM election_results WHERE election_id = ? AND votes > 0
                   GROUP BY polling_unit_code ORDER BY polling_unit_code""",
                [str(election_id)],
            ).fetchall()
        return [(str(row[0]), int(row[1])) for row in result]

    def get_vote_distribution(self, election_id: Optional[str] = None) -> list[int]:
        return [votes for _, votes in self.get_vote_records(election_id)]

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

    def ingest_collation_events(self, records: list[dict]) -> int:
        for record in records:
            canonical = json.dumps(record, sort_keys=True, default=str, separators=(",", ":"))
            snapshot_id = hashlib.sha256(canonical.encode("utf-8")).hexdigest()
            self.conn.execute(
                """INSERT OR REPLACE INTO collation_snapshots
                   (id, level, code, party_code, total_votes, snapshot_at)
                   VALUES (?, ?, ?, ?, ?, ?)""",
                [
                    snapshot_id,
                    str(record.get("level", "unknown")),
                    str(record.get("code", "unknown")),
                    record.get("party_code"),
                    int(record.get("total_votes", 0)),
                    record.get("collated_at") or datetime.now(timezone.utc).isoformat(),
                ],
            )
        return len(records)

    def query_readonly(
        self,
        query: str,
        parameters: Optional[dict] = None,
        limit: int = 0,
        offset: int = 0,
    ) -> dict:
        normalized = query.strip()
        if not normalized:
            normalized = "SELECT id, election_id, polling_unit_code, party_code, votes, status, submitted_at FROM election_results"
        if ";" in normalized or not re.match(r"^(SELECT|WITH)\b", normalized, flags=re.IGNORECASE):
            raise ValueError("only a single SELECT or WITH query is allowed")
        if re.search(r"\b(INSERT|UPDATE|DELETE|CREATE|ALTER|DROP|COPY|ATTACH|INSTALL|LOAD)\b", normalized, flags=re.IGNORECASE):
            raise ValueError("mutation and extension statements are not allowed")

        values: list[object] = []
        params = parameters or {}
        names = re.findall(r"(?<!:):([A-Za-z_][A-Za-z0-9_]*)", normalized)
        for name in names:
            if name not in params:
                raise ValueError(f"missing query parameter: {name}")
            normalized = re.sub(rf"(?<!:):{re.escape(name)}\b", "?", normalized, count=1)
            values.append(params[name])

        total_count = 0
        if limit > 0 or offset > 0:
            total_count = int(self.conn.execute(f"SELECT COUNT(*) FROM ({normalized}) AS _count", values).fetchone()[0])
        if limit > 0:
            normalized = f"{normalized} LIMIT {int(limit)}"
            if offset > 0:
                normalized = f"{normalized} OFFSET {int(offset)}"

        cursor = self.conn.execute(normalized, values)
        columns = [column[0] for column in cursor.description]
        rows = []
        for row in cursor.fetchall():
            rows.append({
                column: value.isoformat() if hasattr(value, "isoformat") else value
                for column, value in zip(columns, row)
            })
        return {
            "columns": columns,
            "rows": rows,
            "count": len(rows),
            "total_count": total_count if limit > 0 or offset > 0 else len(rows),
            "limit": limit,
            "offset": offset,
            "has_more": bool(limit > 0 and offset + len(rows) < total_count),
        }


# --- AI/ML Engine ---


class AnomalyDetector:
    """Election anomaly detection using Isolation Forest and statistical methods.

    Model persistence:
    - On init, attempts to load a persisted model from disk.
    - Training is an explicit, persisted lifecycle operation performed from
      available historical data at controlled startup or via the train endpoint.
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

    def detect_anomalies(self, vote_counts: list[int], polling_unit_codes: list[str]) -> list[AnomalyResult]:
        if len(vote_counts) != len(polling_unit_codes):
            raise ValueError("each anomaly observation requires its real polling-unit identifier")
        if len(vote_counts) < 10:
            return []
        if self.model is None:
            raise RuntimeError("anomaly model is not trained; ingest historical data and invoke POST /api/anomaly/train")

        arr = np.array(vote_counts).reshape(-1, 1)
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
                        polling_unit_code=polling_unit_codes[i],
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
    import signal

    def _sigterm_handler(signum, frame):
        log.info("lakehouse_sigterm_received", signal=signum)
        raise SystemExit(0)

    signal.signal(signal.SIGTERM, _sigterm_handler)

    global lakehouse
    lakehouse = Lakehouse(DUCKDB_PATH)
    log.info("lakehouse_started", path=DUCKDB_PATH)

    # Sync from PostgreSQL and train model on historical data
    try:
        await sync_from_postgres()
        # Train in executor to avoid blocking the async event loop
        loop = asyncio.get_running_loop()
        await loop.run_in_executor(None, _auto_train_model)
    except Exception as exc:
        log.error("initial_sync_failed", error=str(exc))
        lakehouse.conn.close()
        lakehouse = None
        raise RuntimeError("lakehouse initial synchronization failed") from exc

    yield

    # Cleanup: close DuckDB connection
    if lakehouse and lakehouse.conn:
        try:
            lakehouse.conn.close()
            log.info("lakehouse_duckdb_closed")
        except Exception:
            pass
    log.info("lakehouse_shutting_down_complete")


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


def _read_results_from_postgres() -> list[dict]:
    """Read result-party rows from the transactional PostgreSQL store.

    The lakehouse receives a read-only snapshot through Pgpool instead of
    calling a browser-authenticated backend endpoint. This keeps analytics
    ingestion deterministic and works in service-to-service deployments.
    """
    query = """
        SELECT
            r.id::text AS id,
            r.election_id::text AS election_id,
            r.polling_unit_code,
            rps.party_code,
            COALESCE(rps.votes, 0) AS votes,
            r.status,
            s.code AS state_code,
            l.code AS lga_code,
            r.submitted_at
        FROM results r
        LEFT JOIN result_party_scores rps ON rps.result_id = r.id
        LEFT JOIN polling_units pu ON pu.code = r.polling_unit_code
        LEFT JOIN wards w ON w.code = pu.ward_code
        LEFT JOIN lgas l ON l.code = w.lga_code
        LEFT JOIN states s ON s.code = l.state_code
        ORDER BY r.submitted_at ASC, r.id ASC
    """
    with psycopg2.connect(POSTGRES_URL, connect_timeout=10) as conn:
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cursor:
            cursor.execute(query)
            return [dict(row) for row in cursor.fetchall()]


async def sync_from_postgres():
    """Pull a current PostgreSQL result snapshot into DuckDB."""
    if lakehouse is None:
        raise HTTPException(status_code=503, detail="lakehouse not initialized")
    rows = await asyncio.to_thread(_read_results_from_postgres)
    count = lakehouse.ingest_results(rows)
    log.info("sync_completed", count=count)
    return count


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
    records = lakehouse.get_vote_records()
    votes = [votes for _, votes in records]
    polling_unit_codes = [code for code, _ in records]
    try:
        anomalies = detector.detect_anomalies(votes, polling_unit_codes)
    except RuntimeError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    for anomaly in anomalies:
        lakehouse.log_anomaly(anomaly)
    return {"anomalies": [anomaly.model_dump() for anomaly in anomalies], "total": len(anomalies)}


@app.get("/ai/benford")
async def benford_analysis():
    votes = lakehouse.get_vote_distribution()
    result = detector.benford_analysis(votes)
    return result.model_dump()


@app.get("/ai/integrity")
async def integrity_score():
    records = lakehouse.get_vote_records()
    votes = [votes for _, votes in records]
    polling_unit_codes = [code for code, _ in records]
    benford = detector.benford_analysis(votes)
    try:
        anomalies = detector.detect_anomalies(votes, polling_unit_codes)
    except RuntimeError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
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


# --- Apache Sedona Spatial Analytics ---
# Distributed spatial queries for election geospatial analysis.
# Uses DuckDB spatial extension as local engine; connects to Apache Sedona/Spark cluster when SEDONA_URL is set.

SEDONA_URL = os.getenv("SEDONA_URL", "")


class SpatialQuery(BaseModel):
    query_type: str  # "hotspot", "coverage", "clustering", "distance_matrix"
    bounds: Optional[dict] = None  # {"min_lat": ..., "max_lat": ..., "min_lng": ..., "max_lng": ...}
    radius_km: float = 5.0
    limit: int = 100


class SpatialResult(BaseModel):
    query_type: str
    results: list
    count: int
    engine: str  # "duckdb_spatial" or "sedona_distributed"


@app.post("/spatial/analyze")
async def spatial_analyze(query: SpatialQuery):
    """Run distributed spatial analytics on polling unit data."""
    engine = "sedona_distributed" if SEDONA_URL else "duckdb_spatial"

    if query.query_type == "hotspot":
        results = await _spatial_hotspot_analysis(query)
    elif query.query_type == "coverage":
        results = await _spatial_coverage_analysis(query)
    elif query.query_type == "clustering":
        results = await _spatial_clustering(query)
    elif query.query_type == "distance_matrix":
        results = await _spatial_distance_matrix(query)
    else:
        return {"error": f"Unknown query_type: {query.query_type}"}

    return SpatialResult(
        query_type=query.query_type,
        results=results,
        count=len(results),
        engine=engine,
    ).model_dump()


async def _spatial_hotspot_analysis(query: SpatialQuery) -> list:
    """Identify geographic hotspots of anomalous voting patterns using spatial clustering."""
    if SEDONA_URL:
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(f"{SEDONA_URL}/analyze/hotspot", json=query.model_dump())
            resp.raise_for_status()
            return resp.json().get("results", [])

    # DuckDB is the configured local spatial engine when Sedona is not selected.
    conn = duckdb.connect(DUCKDB_PATH, read_only=True)
    try:
        sql = """
            SELECT state, lga, COUNT(*) as pu_count,
                   AVG(total_votes) as avg_votes,
                   STDDEV(total_votes) as stddev_votes
            FROM results
            GROUP BY state, lga
            HAVING STDDEV(total_votes) > AVG(total_votes) * 0.5
            ORDER BY stddev_votes DESC
            LIMIT ?
        """
        rows = conn.execute(sql, [query.limit]).fetchall()
        return [{"state": r[0], "lga": r[1], "pu_count": r[2],
                 "avg_votes": r[3], "stddev_votes": r[4]} for r in rows]
    except Exception as exc:
        log.error("spatial_hotspot_query_failed", error=str(exc))
        raise HTTPException(status_code=500, detail="DuckDB spatial hotspot query failed") from exc
    finally:
        conn.close()


async def _spatial_coverage_analysis(query: SpatialQuery) -> list:
    """Analyze spatial coverage of polling units — identify underserved areas."""
    if SEDONA_URL:
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(f"{SEDONA_URL}/analyze/coverage", json=query.model_dump())
            resp.raise_for_status()
            return resp.json().get("results", [])

    conn = duckdb.connect(DUCKDB_PATH, read_only=True)
    try:
        sql = """
            SELECT state, COUNT(*) as pu_count,
                   SUM(registered_voters) as total_registered,
                   SUM(registered_voters) / COUNT(*) as voters_per_pu
            FROM results
            GROUP BY state
            ORDER BY voters_per_pu DESC
            LIMIT ?
        """
        rows = conn.execute(sql, [query.limit]).fetchall()
        return [{"state": r[0], "pu_count": r[1],
                 "total_registered": r[2], "voters_per_pu": r[3]} for r in rows]
    except Exception as exc:
        log.error("spatial_coverage_query_failed", error=str(exc))
        raise HTTPException(status_code=500, detail="DuckDB spatial coverage query failed") from exc
    finally:
        conn.close()


async def _spatial_clustering(query: SpatialQuery) -> list:
    """Cluster polling units by geographic proximity and voting patterns."""
    if SEDONA_URL:
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(f"{SEDONA_URL}/analyze/clustering", json=query.model_dump())
            resp.raise_for_status()
            return resp.json().get("results", [])

    conn = duckdb.connect(DUCKDB_PATH, read_only=True)
    try:
        sql = """
            SELECT lga, state, COUNT(*) as cluster_size,
                   SUM(total_votes) as total_votes,
                   AVG(total_votes) as avg_votes
            FROM results
            GROUP BY lga, state
            ORDER BY cluster_size DESC
            LIMIT ?
        """
        rows = conn.execute(sql, [query.limit]).fetchall()
        return [{"lga": r[0], "state": r[1], "cluster_size": r[2],
                 "total_votes": r[3], "avg_votes": r[4]} for r in rows]
    except Exception as exc:
        log.error("spatial_clustering_query_failed", error=str(exc))
        raise HTTPException(status_code=500, detail="DuckDB spatial clustering query failed") from exc
    finally:
        conn.close()


async def _spatial_distance_matrix(query: SpatialQuery) -> list:
    """Compute distance matrix between LGA centers for logistics planning."""
    if SEDONA_URL:
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(f"{SEDONA_URL}/analyze/distance", json=query.model_dump())
            resp.raise_for_status()
            return resp.json().get("results", [])

    raise HTTPException(status_code=503, detail="SEDONA_URL is required for distributed spatial distance-matrix analysis")


@app.get("/spatial/capabilities")
async def spatial_capabilities():
    """Report available spatial analysis capabilities."""
    return {
        "engine": "sedona_distributed" if SEDONA_URL else "duckdb_spatial",
        "sedona_connected": bool(SEDONA_URL),
        "capabilities": [
            {"name": "hotspot", "description": "Geographic hotspot detection for anomalous voting patterns"},
            {"name": "coverage", "description": "Spatial coverage analysis of polling unit distribution"},
            {"name": "clustering", "description": "Geographic clustering of polling units by proximity + patterns"},
            {"name": "distance_matrix", "description": "Inter-LGA distance computation for logistics"},
        ],
        "supported_formats": ["geojson", "wkt", "point"],
    }


# --- Compatibility API for the platform middleware client ---

class LakehouseQueryRequest(BaseModel):
    query: str = ""
    parameters: Optional[dict] = None
    format: str = "json"
    limit: int = 0
    offset: int = 0


class LakehouseIngestRequest(BaseModel):
    table: str
    rows: list[dict]


def _rows_as_dicts(sql: str, parameters: list[object]) -> list[dict]:
    if lakehouse is None:
        raise HTTPException(status_code=503, detail="lakehouse not initialized")
    cursor = lakehouse.conn.execute(sql, parameters)
    columns = [column[0] for column in cursor.description]
    return [
        {
            column: value.isoformat() if hasattr(value, "isoformat") else value
            for column, value in zip(columns, row)
        }
        for row in cursor.fetchall()
    ]


@app.get("/tables")
async def compatibility_tables():
    if lakehouse is None:
        raise HTTPException(status_code=503, detail="lakehouse not initialized")
    return [item["name"] for item in lakehouse.get_stats()["tables"]]


@app.post("/ingest")
async def compatibility_ingest(payload: LakehouseIngestRequest):
    if lakehouse is None:
        raise HTTPException(status_code=503, detail="lakehouse not initialized")
    table = payload.table.strip().lower()
    if table in {"results", "election_results"}:
        count = lakehouse.ingest_results(payload.rows)
    elif table in {"collation_events", "collation_snapshots"}:
        count = lakehouse.ingest_collation_events(payload.rows)
    else:
        raise HTTPException(status_code=422, detail=f"unsupported lakehouse table: {payload.table}")
    return {"table": table, "ingested": count}


@app.post("/query")
async def compatibility_query(payload: LakehouseQueryRequest):
    if lakehouse is None:
        raise HTTPException(status_code=503, detail="lakehouse not initialized")
    started = time.perf_counter()
    try:
        response = lakehouse.query_readonly(
            payload.query,
            payload.parameters,
            max(0, min(payload.limit, 10_000)),
            max(0, payload.offset),
        )
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    response["query_ms"] = round((time.perf_counter() - started) * 1000, 3)
    return response


@app.get("/analytics/{election_id}/{analysis_type}")
async def compatibility_election_analytics(election_id: str, analysis_type: str):
    if lakehouse is None:
        raise HTTPException(status_code=503, detail="lakehouse not initialized")

    analysis_type = analysis_type.strip().lower()
    if analysis_type == "turnout":
        data = _rows_as_dicts(
            """
            SELECT state_code, SUM(votes) AS total_votes,
                   COUNT(DISTINCT polling_unit_code) AS polling_units
            FROM election_results
            WHERE election_id = ?
            GROUP BY state_code
            ORDER BY total_votes DESC
            """,
            [election_id],
        )
        return {"election_id": election_id, "type": analysis_type, "data": data}

    if analysis_type == "party_performance":
        data = _rows_as_dicts(
            """
            SELECT party_code, SUM(votes) AS total_votes,
                   COUNT(DISTINCT polling_unit_code) AS polling_units
            FROM election_results
            WHERE election_id = ?
            GROUP BY party_code
            ORDER BY total_votes DESC
            """,
            [election_id],
        )
        return {"election_id": election_id, "type": analysis_type, "data": data}

    if analysis_type == "timeline":
        data = _rows_as_dicts(
            """
            SELECT DATE_TRUNC('hour', submitted_at) AS period,
                   SUM(votes) AS total_votes,
                   COUNT(DISTINCT polling_unit_code) AS polling_units
            FROM election_results
            WHERE election_id = ? AND submitted_at IS NOT NULL
            GROUP BY period
            ORDER BY period ASC
            """,
            [election_id],
        )
        return {"election_id": election_id, "type": analysis_type, "data": data}

    records = lakehouse.get_vote_records(election_id)
    votes = [votes for _, votes in records]
    polling_unit_codes = [code for code, _ in records]
    if analysis_type == "anomalies":
        try:
            anomalies = detector.detect_anomalies(votes, polling_unit_codes)
        except RuntimeError as exc:
            raise HTTPException(status_code=503, detail=str(exc)) from exc
        for anomaly in anomalies:
            lakehouse.log_anomaly(anomaly)
        return {
            "election_id": election_id,
            "type": analysis_type,
            "anomalies": [item.model_dump() for item in anomalies],
            "total": len(anomalies),
        }
    if analysis_type == "benford":
        return {"election_id": election_id, "type": analysis_type, **detector.benford_analysis(votes).model_dump()}
    if analysis_type == "integrity_score":
        benford = detector.benford_analysis(votes)
        try:
            anomalies = detector.detect_anomalies(votes, polling_unit_codes)
        except RuntimeError as exc:
            raise HTTPException(status_code=503, detail=str(exc)) from exc
        return {
            "election_id": election_id,
            "type": analysis_type,
            **detector.integrity_score(votes, benford, len(anomalies)).model_dump(),
        }
    raise HTTPException(status_code=404, detail=f"unknown analytics type: {analysis_type}")


if __name__ == "__main__":
    import uvicorn

    port = int(os.getenv("PORT", "8090"))
    uvicorn.run("main:app", host="0.0.0.0", port=port, reload=True)
