"""INEC Lakehouse Analytics Service - DuckDB-powered analytics for election data."""
import os
import time
import sqlite3
import json
from fastapi import FastAPI, Query
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel
from typing import Optional

app = FastAPI(title="INEC Lakehouse Analytics", version="1.0.0")
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])

DB_PATH = os.getenv("DB_PATH", "/data/app.db")

def get_db():
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    return conn

class LakehouseQuery(BaseModel):
    query: str
    parameters: Optional[dict] = None
    format: Optional[str] = "json"

@app.get("/health")
def health():
    return {"status": "healthy", "service": "lakehouse-analytics", "engine": "sqlite-analytics"}

@app.get("/tables")
def list_tables():
    db = get_db()
    rows = db.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").fetchall()
    db.close()
    return [r["name"] for r in rows]

@app.get("/analytics/{election_id}/turnout")
def turnout_analytics(election_id: int):
    start = time.time()
    db = get_db()
    rows = db.execute("""
        SELECT s.code, s.name, s.geo_zone,
            COUNT(DISTINCT pu.code) as total_pus,
            COUNT(DISTINCT r.id) as results_received,
            COALESCE(SUM(r.accredited_voters), 0) as accredited,
            COALESCE(SUM(r.total_votes_cast), 0) as votes_cast,
            CASE WHEN SUM(r.accredited_voters) > 0
                THEN ROUND(CAST(SUM(r.total_votes_cast) AS FLOAT) / SUM(r.accredited_voters) * 100, 2)
                ELSE 0 END as turnout_pct
        FROM states s
        LEFT JOIN lgas l ON l.state_code = s.code
        LEFT JOIN wards w ON w.lga_code = l.code
        LEFT JOIN polling_units pu ON pu.ward_code = w.code
        LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = ?
        GROUP BY s.code ORDER BY turnout_pct DESC
    """, (election_id,)).fetchall()
    db.close()
    elapsed = (time.time() - start) * 1000
    return {
        "election_id": election_id,
        "analysis_type": "turnout",
        "query_ms": round(elapsed, 2),
        "data": [dict(r) for r in rows],
    }

@app.get("/analytics/{election_id}/party_performance")
def party_performance(election_id: int):
    start = time.time()
    db = get_db()
    rows = db.execute("""
        SELECT p.code, p.name, p.color, p.abbreviation,
            SUM(rps.votes) as total_votes,
            COUNT(DISTINCT r.id) as results_count,
            ROUND(CAST(SUM(rps.votes) AS FLOAT) /
                NULLIF((SELECT SUM(total_valid_votes) FROM results WHERE election_id = ? AND status IN ('finalized','validated')), 0) * 100, 2) as vote_share_pct
        FROM parties p
        LEFT JOIN result_party_scores rps ON rps.party_code = p.code
        LEFT JOIN results r ON r.id = rps.result_id AND r.election_id = ? AND r.status IN ('finalized','validated')
        WHERE p.is_active = 1
        GROUP BY p.code ORDER BY total_votes DESC
    """, (election_id, election_id)).fetchall()
    db.close()
    elapsed = (time.time() - start) * 1000
    return {
        "election_id": election_id,
        "analysis_type": "party_performance",
        "query_ms": round(elapsed, 2),
        "data": [dict(r) for r in rows],
    }

@app.get("/analytics/{election_id}/timeline")
def timeline_analytics(election_id: int, interval: str = Query("hour", regex="^(hour|day|minute)$")):
    start = time.time()
    db = get_db()
    fmt_map = {"minute": "%Y-%m-%d %H:%M", "hour": "%Y-%m-%d %H:00", "day": "%Y-%m-%d"}
    fmt = fmt_map.get(interval, "%Y-%m-%d %H:00")
    rows = db.execute(f"""
        SELECT strftime('{fmt}', submitted_at) as time_bucket,
            COUNT(*) as results_count,
            SUM(total_votes_cast) as total_votes,
            SUM(total_valid_votes) as valid_votes
        FROM results WHERE election_id = ?
        GROUP BY time_bucket ORDER BY time_bucket
    """, (election_id,)).fetchall()
    db.close()
    elapsed = (time.time() - start) * 1000
    cumulative = 0
    data = []
    for r in rows:
        cumulative += r["results_count"]
        d = dict(r)
        d["cumulative_results"] = cumulative
        data.append(d)
    return {
        "election_id": election_id,
        "analysis_type": "timeline",
        "interval": interval,
        "query_ms": round(elapsed, 2),
        "data": data,
    }

@app.get("/analytics/{election_id}/anomalies")
def anomaly_detection(election_id: int):
    start = time.time()
    db = get_db()
    anomalies = []

    high_turnout = db.execute("""
        SELECT r.id, r.polling_unit_code, r.total_votes_cast, r.accredited_voters,
            CASE WHEN r.accredited_voters > 0
                THEN ROUND(CAST(r.total_votes_cast AS FLOAT) / r.accredited_voters * 100, 2)
                ELSE 0 END as turnout_pct
        FROM results r WHERE r.election_id = ? AND r.accredited_voters > 0
            AND CAST(r.total_votes_cast AS FLOAT) / r.accredited_voters > 0.95
        ORDER BY turnout_pct DESC LIMIT 20
    """, (election_id,)).fetchall()
    for r in high_turnout:
        anomalies.append({"type": "high_turnout", "severity": "medium", "result_id": r["id"],
                          "pu_code": r["polling_unit_code"], "detail": f"Turnout {r['turnout_pct']}%"})

    over_votes = db.execute("""
        SELECT r.id, r.polling_unit_code, r.total_votes_cast, r.accredited_voters
        FROM results r WHERE r.election_id = ?
            AND r.total_votes_cast > r.accredited_voters AND r.accredited_voters > 0
        LIMIT 20
    """, (election_id,)).fetchall()
    for r in over_votes:
        anomalies.append({"type": "over_voting", "severity": "high", "result_id": r["id"],
                          "pu_code": r["polling_unit_code"],
                          "detail": f"Votes {r['total_votes_cast']} > Accredited {r['accredited_voters']}"})

    lopsided = db.execute("""
        SELECT r.id, r.polling_unit_code, rps.party_code,
            rps.votes, r.total_valid_votes,
            ROUND(CAST(rps.votes AS FLOAT) / NULLIF(r.total_valid_votes, 0) * 100, 2) as pct
        FROM results r JOIN result_party_scores rps ON rps.result_id = r.id
        WHERE r.election_id = ? AND r.total_valid_votes > 50
            AND CAST(rps.votes AS FLOAT) / r.total_valid_votes > 0.95
        ORDER BY pct DESC LIMIT 20
    """, (election_id,)).fetchall()
    for r in lopsided:
        anomalies.append({"type": "lopsided_result", "severity": "medium", "result_id": r["id"],
                          "pu_code": r["polling_unit_code"],
                          "detail": f"{r['party_code']} got {r['pct']}% of votes"})

    db.close()
    elapsed = (time.time() - start) * 1000
    return {
        "election_id": election_id,
        "analysis_type": "anomalies",
        "query_ms": round(elapsed, 2),
        "total_anomalies": len(anomalies),
        "data": anomalies,
    }

@app.post("/query")
def execute_query(q: LakehouseQuery):
    start = time.time()
    db = get_db()
    safe_query = q.query.strip().upper()
    if not safe_query.startswith("SELECT"):
        return {"error": "Only SELECT queries allowed"}
    try:
        rows = db.execute(q.query).fetchall()
        cols = [desc[0] for desc in db.execute(q.query).description] if rows else []
        db.close()
        elapsed = (time.time() - start) * 1000
        return {
            "columns": cols,
            "rows": [dict(r) for r in rows],
            "count": len(rows),
            "query_ms": round(elapsed, 2),
        }
    except Exception as e:
        db.close()
        return {"error": str(e)}

if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("PORT", "8090"))
    uvicorn.run(app, host="0.0.0.0", port=port)
