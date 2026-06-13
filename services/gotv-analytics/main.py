"""GOTV Campaign Analytics — ML Targeting & Turnout Intelligence.

Provides:
- Campaign performance analytics (A/B testing, conversion funnels, geo breakdown)
- ML-powered contact targeting (likelihood-to-vote scoring)
- Real-time turnout intelligence (aggregate PU-level, never individual)
- Volunteer efficiency analytics
- Pledge fulfillment prediction
"""

import os
import time
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from typing import Optional

import numpy as np
import polars as pl
import structlog
from fastapi import FastAPI, HTTPException, Query, Request
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field
from scipy.spatial.distance import cdist
from sklearn.ensemble import GradientBoostingClassifier, IsolationForest
from sklearn.preprocessing import StandardScaler

logger = structlog.get_logger()

DB_URL = os.getenv("DATABASE_URL", "postgresql://ngapp:ngapp123@localhost:5432/ngapp")


# ─── DB helpers (connection pooling) ────────────────────────────────────────

_db_pool = None


def get_db_pool():
    """Get or create a connection pool (min=2, max=10)."""
    global _db_pool
    if _db_pool is None:
        import psycopg2.pool
        _db_pool = psycopg2.pool.ThreadedConnectionPool(
            minconn=2, maxconn=10, dsn=DB_URL
        )
    return _db_pool


def get_db_connection():
    """Get a connection from the pool."""
    return get_db_pool().getconn()


def release_db_connection(conn):
    """Return a connection to the pool."""
    try:
        get_db_pool().putconn(conn)
    except Exception:
        pass


def query_df(sql: str, params: tuple = ()) -> pl.DataFrame:
    """Run SQL and return a Polars DataFrame."""
    conn = get_db_connection()
    try:
        import psycopg2.extras
        cur = conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
        cur.execute(sql, params)
        rows = cur.fetchall()
        cur.close()
        if not rows:
            return pl.DataFrame()
        return pl.from_dicts(rows)
    finally:
        release_db_connection(conn)


# ─── ML Models ──────────────────────────────────────────────────────────────

class ContactTargetingModel:
    """Predicts likelihood a contact will vote (pledge fulfillment)."""

    def __init__(self):
        self.model = GradientBoostingClassifier(
            n_estimators=100, max_depth=5, learning_rate=0.1, random_state=42
        )
        self.scaler = StandardScaler()
        self.is_fitted = False
        self.last_fit_time = 0.0
        self.last_data_hash = ""

    def _build_features(self, df: pl.DataFrame) -> np.ndarray:
        """Extract features from contact data."""
        features = []
        for row in df.iter_rows(named=True):
            f = [
                row.get("contact_count", 0) or 0,
                1 if row.get("voter_status") == "pledged" else 0,
                1 if row.get("voter_status") == "confirmed" else 0,
                row.get("tags_count", 0) or 0,
                row.get("days_since_contact", 365) or 365,
                1 if row.get("has_consent") else 0,
                row.get("outreach_count", 0) or 0,
                row.get("response_count", 0) or 0,
            ]
            features.append(f)
        return np.array(features, dtype=np.float64) if features else np.empty((0, 8))

    def fit(self, party_id: int):
        """Train on party's historical pledge data."""
        df = query_df("""
            SELECT c.contact_count, c.voter_status,
                   COALESCE(array_length(c.tags, 1), 0) AS tags_count,
                   EXTRACT(DAY FROM NOW() - COALESCE(c.last_contacted_at, c.created_at)) AS days_since_contact,
                   CASE WHEN c.consent_id IS NOT NULL THEN 1 ELSE 0 END AS has_consent,
                   COALESCE(o.outreach_count, 0) AS outreach_count,
                   COALESCE(o.response_count, 0) AS response_count,
                   CASE WHEN p.status = 'fulfilled' THEN 1 ELSE 0 END AS label
            FROM gotv_contacts c
            LEFT JOIN (
                SELECT contact_id, COUNT(*) AS outreach_count,
                       COUNT(*) FILTER(WHERE status = 'responded') AS response_count
                FROM gotv_outreach_log GROUP BY contact_id
            ) o ON o.contact_id = c.contact_id
            LEFT JOIN gotv_pledges p ON p.contact_id = c.contact_id
            WHERE c.party_id = %s AND c.opted_out = FALSE
            LIMIT 50000
        """, (party_id,))

        if len(df) < 20:
            logger.warning("Insufficient data for ML model", party_id=party_id, rows=len(df))
            return False

        X = self._build_features(df)
        y = df["label"].to_numpy()

        if len(np.unique(y)) < 2:
            logger.warning("Single-class data, skipping model fit", party_id=party_id)
            return False

        X_scaled = self.scaler.fit_transform(X)
        self.model.fit(X_scaled, y)
        self.is_fitted = True
        self.last_fit_time = time.time()
        logger.info("Model fitted", party_id=party_id, samples=len(df))
        return True

    def predict(self, party_id: int, limit: int = 100) -> list[dict]:
        """Score contacts by likelihood to fulfill pledge."""
        df = query_df("""
            SELECT c.contact_id, c.voter_status, c.contact_count,
                   COALESCE(array_length(c.tags, 1), 0) AS tags_count,
                   EXTRACT(DAY FROM NOW() - COALESCE(c.last_contacted_at, c.created_at)) AS days_since_contact,
                   CASE WHEN c.consent_id IS NOT NULL THEN 1 ELSE 0 END AS has_consent,
                   COALESCE(o.outreach_count, 0) AS outreach_count,
                   COALESCE(o.response_count, 0) AS response_count
            FROM gotv_contacts c
            LEFT JOIN (
                SELECT contact_id, COUNT(*) AS outreach_count,
                       COUNT(*) FILTER(WHERE status = 'responded') AS response_count
                FROM gotv_outreach_log GROUP BY contact_id
            ) o ON o.contact_id = c.contact_id
            WHERE c.party_id = %s AND c.opted_out = FALSE
              AND c.voter_status IN ('unknown', 'pledged')
            LIMIT 10000
        """, (party_id,))

        if len(df) == 0:
            return []

        X = self._build_features(df)
        if not self.is_fitted:
            # Heuristic scoring if model not fitted
            scores = X[:, 0] * 0.2 + X[:, 5] * 0.3 + X[:, 7] * 0.3 + (1 - X[:, 4] / 365) * 0.2
            scores = np.clip(scores / max(scores.max(), 1), 0, 1)
        else:
            X_scaled = self.scaler.transform(X)
            scores = self.model.predict_proba(X_scaled)[:, 1]

        contact_ids = df["contact_id"].to_list()
        results = sorted(
            [{"contact_id": cid, "score": round(float(s), 4)} for cid, s in zip(contact_ids, scores)],
            key=lambda x: x["score"],
            reverse=True,
        )
        return results[:limit]


class AnomalyDetector:
    """Detects anomalous outreach patterns (spam, bot-like behavior)."""

    def __init__(self):
        self.model = IsolationForest(n_estimators=100, contamination=0.05, random_state=42)
        self.is_fitted = False

    def detect(self, party_id: int) -> list[dict]:
        df = query_df("""
            SELECT campaign_id,
                   COUNT(*) AS total_outreach,
                   COUNT(*) FILTER(WHERE status = 'failed') AS failed,
                   COUNT(*) FILTER(WHERE status = 'opted_out') AS opted_out,
                   COUNT(DISTINCT contact_id) AS unique_contacts,
                   MAX(created_at) - MIN(created_at) AS duration
            FROM gotv_outreach_log
            WHERE party_id = %s
            GROUP BY campaign_id
        """, (party_id,))

        if len(df) < 5:
            return []

        features = np.column_stack([
            df["total_outreach"].to_numpy(dtype=np.float64),
            df["failed"].to_numpy(dtype=np.float64),
            df["opted_out"].to_numpy(dtype=np.float64),
            df["unique_contacts"].to_numpy(dtype=np.float64),
        ])

        self.model.fit(features)
        predictions = self.model.predict(features)
        scores = self.model.score_samples(features)

        anomalies = []
        campaign_ids = df["campaign_id"].to_list()
        for i, (pred, score) in enumerate(zip(predictions, scores)):
            if pred == -1:
                anomalies.append({
                    "campaign_id": campaign_ids[i],
                    "anomaly_score": round(float(score), 4),
                    "reason": "Unusual outreach pattern detected",
                })
        return anomalies


# ─── Singleton Models ───────────────────────────────────────────────────────

_targeting_models: dict[int, ContactTargetingModel] = {}
_anomaly_detector = AnomalyDetector()


def get_targeting_model(party_id: int) -> ContactTargetingModel:
    if party_id not in _targeting_models:
        _targeting_models[party_id] = ContactTargetingModel()
    return _targeting_models[party_id]


# ─── Pydantic Models ───────────────────────────────────────────────────────

class CampaignAnalyticsResponse(BaseModel):
    campaign_id: str
    total_contacts: int = 0
    reached: int = 0
    responded: int = 0
    delivery_rate: float = 0.0
    response_rate: float = 0.0
    opt_out_rate: float = 0.0
    variant_a: dict = Field(default_factory=dict)
    variant_b: dict = Field(default_factory=dict)
    geo_breakdown: list[dict] = Field(default_factory=list)
    hourly_trend: list[dict] = Field(default_factory=list)


class TurnoutResponse(BaseModel):
    election_id: int
    total_registered: int = 0
    total_accredited: int = 0
    turnout_pct: float = 0.0
    by_state: list[dict] = Field(default_factory=list)


class VolunteerMetricsResponse(BaseModel):
    party_id: int
    total_volunteers: int = 0
    active_volunteers: int = 0
    total_doors_knocked: int = 0
    total_calls_made: int = 0
    total_rides_given: int = 0
    top_performers: list[dict] = Field(default_factory=list)
    by_role: list[dict] = Field(default_factory=list)


# ─── App ────────────────────────────────────────────────────────────────────

@asynccontextmanager
async def lifespan(app: FastAPI):
    from middleware import middleware_status
    logger.info("GOTV Analytics starting", middleware=middleware_status())
    yield
    logger.info("GOTV Analytics shutting down")


app = FastAPI(
    title="INEC GOTV Analytics",
    description="Campaign analytics, ML targeting & turnout intelligence",
    version="1.0.0",
    lifespan=lifespan,
)

# CORS for cross-origin requests from frontend
app.add_middleware(
    CORSMiddleware,
    allow_origins=os.getenv("GOTV_CORS_ORIGINS", "*").split(","),
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.middleware("http")
async def auth_middleware(request: Request, call_next):
    """Validate auth header on non-health endpoints (service-to-service via Dapr or Bearer token)."""
    if request.url.path in ("/health", "/docs", "/openapi.json"):
        return await call_next(request)
    auth = request.headers.get("Authorization", "")
    dapr_token = request.headers.get("dapr-api-token", "")
    if not auth and not dapr_token:
        from fastapi.responses import JSONResponse
        return JSONResponse(status_code=401, content={"error": "authentication required"})
    return await call_next(request)


@app.get("/health")
async def health():
    return {
        "service": "gotv-analytics",
        "status": "healthy",
        "version": "1.0.0",
        "language": "python",
        "capabilities": [
            "campaign_analytics",
            "ml_targeting",
            "turnout_intelligence",
            "anomaly_detection",
            "volunteer_metrics",
        ],
    }


@app.get("/gotv-analytics/middleware/status")
async def mw_status():
    from middleware import middleware_status
    return middleware_status()


@app.get("/gotv-analytics/search")
async def opensearch_search(q: str, party_id: int = Query(...), index: str = "gotv-contacts"):
    """Full-text search across GOTV data via OpenSearch."""
    from middleware import search_opensearch
    results = search_opensearch(index, q, party_id)
    return {"results": results, "count": len(results), "source": "opensearch" if results else "unavailable"}


@app.get("/gotv-analytics/lakehouse")
async def lakehouse_query(sql: str = Query(...)):
    """Run analytical query against Lakehouse/Trino."""
    from middleware import query_lakehouse
    results = query_lakehouse(sql)
    return {"data": results, "count": len(results), "source": "lakehouse"}


@app.get("/gotv-analytics/campaign/{campaign_id}")
async def campaign_analytics(campaign_id: str, party_id: int = Query(...)):
    """Full analytics for a single campaign — delivery funnel, A/B test results, geo breakdown."""

    # Outreach funnel
    funnel = query_df("""
        SELECT status, message_variant, COUNT(*) AS cnt
        FROM gotv_outreach_log
        WHERE campaign_id = %s AND party_id = %s
        GROUP BY status, message_variant
    """, (campaign_id, party_id))

    if len(funnel) == 0:
        raise HTTPException(404, "Campaign has no outreach data")

    total = int(funnel["cnt"].sum())
    reached = int(funnel.filter(pl.col("status").is_in(["delivered", "read", "responded"]))["cnt"].sum())
    responded = int(funnel.filter(pl.col("status") == "responded")["cnt"].sum())
    opted_out = int(funnel.filter(pl.col("status") == "opted_out")["cnt"].sum())

    # A/B split
    variant_a_data = funnel.filter(pl.col("message_variant") == "a")
    variant_b_data = funnel.filter(pl.col("message_variant") == "b")

    def variant_stats(vdf: pl.DataFrame) -> dict:
        t = int(vdf["cnt"].sum()) if len(vdf) > 0 else 0
        r = int(vdf.filter(pl.col("status").is_in(["delivered", "read", "responded"]))["cnt"].sum()) if len(vdf) > 0 else 0
        rsp = int(vdf.filter(pl.col("status") == "responded")["cnt"].sum()) if len(vdf) > 0 else 0
        return {
            "total": t,
            "reached": r,
            "responded": rsp,
            "delivery_rate": round(r / t, 4) if t > 0 else 0,
            "response_rate": round(rsp / r, 4) if r > 0 else 0,
        }

    # Geo breakdown
    geo = query_df("""
        SELECT c.state_code, COUNT(*) AS total,
               COUNT(*) FILTER(WHERE o.status IN ('delivered','read','responded')) AS reached,
               COUNT(*) FILTER(WHERE o.status = 'responded') AS responded
        FROM gotv_outreach_log o
        JOIN gotv_contacts c ON c.contact_id = o.contact_id AND c.party_id = o.party_id
        WHERE o.campaign_id = %s AND o.party_id = %s
        GROUP BY c.state_code
        ORDER BY total DESC
    """, (campaign_id, party_id))

    # Hourly trend
    hourly = query_df("""
        SELECT DATE_TRUNC('hour', created_at) AS hour, COUNT(*) AS sent,
               COUNT(*) FILTER(WHERE status IN ('delivered','read','responded')) AS delivered
        FROM gotv_outreach_log
        WHERE campaign_id = %s AND party_id = %s
        GROUP BY DATE_TRUNC('hour', created_at)
        ORDER BY hour
    """, (campaign_id, party_id))

    return CampaignAnalyticsResponse(
        campaign_id=campaign_id,
        total_contacts=total,
        reached=reached,
        responded=responded,
        delivery_rate=round(reached / total, 4) if total > 0 else 0,
        response_rate=round(responded / reached, 4) if reached > 0 else 0,
        opt_out_rate=round(opted_out / total, 4) if total > 0 else 0,
        variant_a=variant_stats(variant_a_data),
        variant_b=variant_stats(variant_b_data),
        geo_breakdown=geo.to_dicts() if len(geo) > 0 else [],
        hourly_trend=hourly.to_dicts() if len(hourly) > 0 else [],
    )


@app.get("/gotv-analytics/targeting/{party_id}")
async def ml_targeting(party_id: int, limit: int = Query(100, le=500)):
    """ML-powered contact targeting — returns contacts ranked by vote likelihood."""
    model = get_targeting_model(party_id)

    # Refit if stale (>1h) or never fitted
    if not model.is_fitted or (time.time() - model.last_fit_time > 3600):
        model.fit(party_id)

    scores = model.predict(party_id, limit=limit)

    # Middleware: publish scoring event to Kafka, cache results in Redis
    from middleware import publish_kafka, cache_set
    publish_kafka("gotv.analytics", f"targeting-{party_id}", {
        "event": "ml_targeting_run", "party_id": party_id,
        "total_scored": len(scores), "model_fitted": model.is_fitted,
    })
    cache_set(f"targeting:{party_id}", {"total_scored": len(scores)}, ttl=600)

    return {
        "party_id": party_id,
        "model_fitted": model.is_fitted,
        "contacts": scores,
        "total_scored": len(scores),
    }


@app.get("/gotv-analytics/anomalies/{party_id}")
async def anomaly_detection(party_id: int):
    """Detect anomalous campaign patterns (spam, bot-like behavior)."""
    anomalies = _anomaly_detector.detect(party_id)

    # Middleware: publish anomaly events to Kafka + Fluvio for real-time alerting
    if anomalies:
        from middleware import publish_kafka, stream_fluvio
        publish_kafka("gotv.analytics", f"anomalies-{party_id}", {
            "event": "anomalies_detected", "party_id": party_id,
            "count": len(anomalies),
        })
        stream_fluvio("gotv-anomalies", {
            "party_id": party_id, "anomalies": anomalies[:5],
        })

    return {
        "party_id": party_id,
        "anomalies": anomalies,
        "total_flagged": len(anomalies),
    }


@app.get("/gotv-analytics/turnout/{election_id}")
async def turnout_intelligence(election_id: int):
    """Aggregate turnout intelligence — PU-level only, never individual."""
    states = query_df("""
        SELECT pu.state_code,
               COUNT(DISTINCT pu.polling_unit_id) AS pus,
               COALESCE(SUM(pu.registered_voters), 0) AS registered,
               COALESCE(SUM(r.accredited_voters), 0) AS accredited
        FROM polling_units pu
        LEFT JOIN results r ON r.polling_unit_id = pu.polling_unit_id AND r.election_id = %s
        GROUP BY pu.state_code
        ORDER BY pu.state_code
    """, (election_id,))

    if len(states) == 0:
        return TurnoutResponse(election_id=election_id)

    total_reg = int(states["registered"].sum())
    total_acc = int(states["accredited"].sum())

    by_state = []
    for row in states.iter_rows(named=True):
        reg = row["registered"] or 0
        acc = row["accredited"] or 0
        by_state.append({
            "state_code": row["state_code"],
            "polling_units": row["pus"],
            "registered": reg,
            "accredited": acc,
            "turnout_pct": round(acc / reg * 100, 2) if reg > 0 else 0,
        })

    return TurnoutResponse(
        election_id=election_id,
        total_registered=total_reg,
        total_accredited=total_acc,
        turnout_pct=round(total_acc / total_reg * 100, 2) if total_reg > 0 else 0,
        by_state=by_state,
    )


@app.get("/gotv-analytics/volunteers/{party_id}")
async def volunteer_metrics(party_id: int):
    """Volunteer performance metrics — top performers, role distribution."""
    vols = query_df("""
        SELECT volunteer_id, full_name, role, is_active,
               doors_knocked, calls_made, rides_given
        FROM gotv_volunteers
        WHERE party_id = %s
    """, (party_id,))

    if len(vols) == 0:
        return VolunteerMetricsResponse(party_id=party_id)

    total = len(vols)
    active = int(vols.filter(pl.col("is_active") == True)["is_active"].count())
    total_doors = int(vols["doors_knocked"].sum())
    total_calls = int(vols["calls_made"].sum())
    total_rides = int(vols["rides_given"].sum())

    # Top performers by total activity
    vols_with_score = vols.with_columns(
        (pl.col("doors_knocked") + pl.col("calls_made") + pl.col("rides_given") * 3).alias("score")
    ).sort("score", descending=True)

    top = vols_with_score.head(10).to_dicts()

    # By role
    by_role = (
        vols.group_by("role")
        .agg([
            pl.count().alias("count"),
            pl.col("doors_knocked").sum().alias("doors"),
            pl.col("calls_made").sum().alias("calls"),
            pl.col("rides_given").sum().alias("rides"),
        ])
        .sort("count", descending=True)
        .to_dicts()
    )

    return VolunteerMetricsResponse(
        party_id=party_id,
        total_volunteers=total,
        active_volunteers=active,
        total_doors_knocked=total_doors,
        total_calls_made=total_calls,
        total_rides_given=total_rides,
        top_performers=top,
        by_role=by_role,
    )


@app.get("/gotv-analytics/pledge-funnel/{party_id}")
async def pledge_funnel(party_id: int, election_id: Optional[int] = None):
    """Pledge fulfillment funnel — pledged → reminded → confirmed → fulfilled."""
    where_clause = "WHERE p.party_id = %s"
    params: list = [party_id]
    if election_id:
        where_clause += " AND p.election_id = %s"
        params.append(election_id)

    funnel = query_df(f"""
        SELECT p.status, COUNT(*) AS cnt
        FROM gotv_pledges p
        {where_clause}
        GROUP BY p.status
    """, tuple(params))

    if len(funnel) == 0:
        return {"party_id": party_id, "funnel": {}, "total": 0}

    status_counts = {row["status"]: int(row["cnt"]) for row in funnel.iter_rows(named=True)}
    total = sum(status_counts.values())

    return {
        "party_id": party_id,
        "election_id": election_id,
        "total_pledges": total,
        "funnel": status_counts,
        "conversion_rates": {
            "pledge_to_remind": round(status_counts.get("reminded", 0) / total, 4) if total > 0 else 0,
            "remind_to_confirm": round(
                status_counts.get("confirmed_day_of", 0) / max(status_counts.get("reminded", 0), 1), 4
            ),
            "confirm_to_fulfill": round(
                status_counts.get("fulfilled", 0) / max(status_counts.get("confirmed_day_of", 0), 1), 4
            ),
            "overall_fulfillment": round(status_counts.get("fulfilled", 0) / total, 4) if total > 0 else 0,
        },
    }


@app.get("/gotv-analytics/heatmap/{party_id}")
async def contact_heatmap(party_id: int):
    """Geographic heatmap of contacts by voter status."""
    data = query_df("""
        SELECT state_code, lga_code, voter_status, COUNT(*) AS cnt
        FROM gotv_contacts
        WHERE party_id = %s AND opted_out = FALSE AND state_code IS NOT NULL
        GROUP BY state_code, lga_code, voter_status
        ORDER BY state_code, lga_code
    """, (party_id,))

    if len(data) == 0:
        return {"party_id": party_id, "heatmap": []}

    return {
        "party_id": party_id,
        "heatmap": data.to_dicts(),
        "total_regions": len(data.unique("state_code")),
    }


# ─── V2 Router Integration ──────────────────────────────────────────────────
try:
    from analytics_v2 import router as v2_router
    app.include_router(v2_router)
    logger.info("V2 analytics router loaded (turnout prediction, ROI, war room)")
except ImportError:
    logger.warning("analytics_v2 module not found — V2 endpoints disabled")

# ─── Scoring Engine (Cambridge Analytica-grade analytics) ────────────────────
try:
    from scoring_engine import router as scoring_router
    app.include_router(scoring_router)
    logger.info("Scoring engine loaded (voter scoring, persuadability, win probability, bandit)")
except ImportError:
    logger.warning("scoring_engine module not found — scoring endpoints disabled")


# ─── KOH 2027 Indicators (CPI forecasting, demographic gaps, LGA priorities) ─
try:
    from koh_indicators import router as koh_router
    app.include_router(koh_router)
    logger.info("KOH indicators module loaded (CPI forecast, demographic gaps, sentiment forecast)")
except ImportError:
    logger.warning("koh_indicators module not found — KOH endpoints disabled")


# ─── ML Serving (PyTorch model inference, registry, monitoring) ──────────────
try:
    from ml_serving import router as ml_router
    app.include_router(ml_router)
    logger.info("ML serving module loaded (fraud DNN, voter scoring, GNN inference)")
except ImportError:
    logger.warning("ml_serving module not found — ML inference endpoints disabled")


if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("PORT", "8102"))
    uvicorn.run(app, host="0.0.0.0", port=port)
