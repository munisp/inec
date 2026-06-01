"""INEC AI-Powered Election Analytics Service - Statistical anomaly detection and validation."""
import os
import time
import math
import sqlite3
from collections import Counter, defaultdict
from fastapi import FastAPI, Query
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel
from typing import Optional

app = FastAPI(title="INEC AI Analytics", version="2.0.0",
              description="AI-powered election result validation and anomaly detection")
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

def mean_std(values):
    if not values:
        return 0.0, 0.0
    n = len(values)
    m = sum(values) / n
    variance = sum((x - m) ** 2 for x in values) / n
    return m, math.sqrt(variance)

def median_val(values):
    s = sorted(values)
    n = len(s)
    if n == 0:
        return 0
    if n % 2 == 1:
        return s[n // 2]
    return (s[n // 2 - 1] + s[n // 2]) / 2

def iqr_bounds(values):
    s = sorted(values)
    n = len(s)
    if n < 4:
        return None, None
    q1 = s[n // 4]
    q3 = s[3 * n // 4]
    iqr = q3 - q1
    return q1 - 1.5 * iqr, q3 + 1.5 * iqr

def benfords_first_digit(values):
    if len(values) < 30:
        return {"status": "insufficient_data", "sample_size": len(values)}
    digits = []
    for v in values:
        if v > 0:
            first = int(str(abs(v))[0])
            if 1 <= first <= 9:
                digits.append(first)
    if len(digits) < 20:
        return {"status": "insufficient_data", "sample_size": len(digits)}
    expected_benford = {d: math.log10(1 + 1/d) for d in range(1, 10)}
    observed_counts = Counter(digits)
    n = len(digits)
    chi_sq = 0.0
    distribution = []
    for d in range(1, 10):
        obs = observed_counts.get(d, 0)
        exp = expected_benford[d] * n
        chi_sq += (obs - exp) ** 2 / exp
        distribution.append({
            "digit": d, "observed_count": obs,
            "observed_pct": round(obs / n * 100, 2),
            "expected_pct": round(expected_benford[d] * 100, 2),
            "deviation": round(abs(obs / n - expected_benford[d]) * 100, 2)
        })
    if chi_sq > 20.09:
        status = "fail"
    elif chi_sq > 15.51:
        status = "suspicious"
    else:
        status = "pass"
    return {"test": "benfords_first_digit", "chi_square": round(chi_sq, 3),
            "degrees_of_freedom": 8, "status": status, "sample_size": n, "distribution": distribution}

def benfords_last_digit(values):
    if len(values) < 30:
        return {"status": "insufficient_data", "sample_size": len(values)}
    digits = [abs(v) % 10 for v in values if v > 0]
    if len(digits) < 20:
        return {"status": "insufficient_data", "sample_size": len(digits)}
    n = len(digits)
    expected = n / 10.0
    observed_counts = Counter(digits)
    chi_sq = 0.0
    distribution = []
    for d in range(10):
        obs = observed_counts.get(d, 0)
        chi_sq += (obs - expected) ** 2 / expected
        distribution.append({
            "digit": d, "observed_count": obs,
            "observed_pct": round(obs / n * 100, 2),
            "expected_pct": 10.0,
            "deviation": round(abs(obs / n - 0.1) * 100, 2)
        })
    if chi_sq > 21.67:
        status = "fail"
    elif chi_sq > 16.92:
        status = "suspicious"
    else:
        status = "pass"
    return {"test": "benfords_last_digit", "chi_square": round(chi_sq, 3),
            "degrees_of_freedom": 9, "status": status, "sample_size": n, "distribution": distribution}

def detect_overvoting(results):
    anomalies = []
    for r in results:
        if r["registered_voters"] > 0 and r["total_votes"] > r["registered_voters"]:
            excess = r["total_votes"] - r["registered_voters"]
            pct = excess / r["registered_voters"] * 100
            anomalies.append({
                "polling_unit_code": r["code"], "pu_name": r["name"],
                "anomaly_type": "overvoting", "severity": "critical",
                "score": round(pct, 2),
                "detail": f"Votes ({r['total_votes']}) exceed registered ({r['registered_voters']}) by {excess} ({pct:.1f}%)",
                "total_votes": r["total_votes"], "registered_voters": r["registered_voters"],
                "turnout_pct": round(r["turnout"], 2)
            })
    return anomalies

def detect_turnout_outliers(results):
    turnouts = [r["turnout"] for r in results if r["turnout"] > 0]
    if len(turnouts) < 10:
        return []
    m, s = mean_std(turnouts)
    _, upper_iqr = iqr_bounds(turnouts)
    anomalies = []
    for r in results:
        if r["turnout"] <= 0 or s == 0:
            continue
        z = (r["turnout"] - m) / s
        is_iqr_outlier = upper_iqr is not None and r["turnout"] > upper_iqr
        if abs(z) > 2.5 or is_iqr_outlier:
            severity = "critical" if abs(z) > 3.5 else "warning"
            anomalies.append({
                "polling_unit_code": r["code"], "pu_name": r["name"],
                "anomaly_type": "turnout_outlier", "severity": severity,
                "score": round(abs(z), 2),
                "detail": f"Turnout {r['turnout']:.1f}% (z={z:.2f}, mean={m:.1f}%, std={s:.1f}%)",
                "total_votes": r["total_votes"], "registered_voters": r["registered_voters"],
                "turnout_pct": round(r["turnout"], 2)
            })
    return anomalies

def detect_party_dominance(pu_party_votes):
    anomalies = []
    for code, votes_list in pu_party_votes.items():
        total = sum(v["votes"] for v in votes_list)
        if total < 50 or len(votes_list) < 2:
            continue
        top = max(votes_list, key=lambda x: x["votes"])
        share = top["votes"] / total * 100
        if share > 90:
            severity = "critical" if share > 98 else "warning"
            anomalies.append({
                "polling_unit_code": code, "pu_name": votes_list[0].get("pu_name", ""),
                "anomaly_type": "party_dominance", "severity": severity,
                "score": round(share, 2),
                "detail": f"{top['party']} received {share:.1f}% of {total} votes",
                "total_votes": total, "registered_voters": votes_list[0].get("registered", 0),
                "turnout_pct": 0
            })
    return anomalies

def detect_round_number_bias(results):
    anomalies = []
    for r in results:
        v = r["total_votes"]
        if v >= 100 and v % 100 == 0:
            anomalies.append({
                "polling_unit_code": r["code"], "pu_name": r["name"],
                "anomaly_type": "round_number", "severity": "minor",
                "score": v, "detail": f"Suspiciously round vote count: {v}",
                "total_votes": v, "registered_voters": r["registered_voters"],
                "turnout_pct": round(r["turnout"], 2)
            })
    return anomalies

def detect_sequential_patterns(results):
    anomalies = []
    sorted_results = sorted(results, key=lambda r: r["code"])
    for i in range(1, len(sorted_results)):
        prev, curr = sorted_results[i-1], sorted_results[i]
        if prev["total_votes"] == curr["total_votes"] and prev["total_votes"] > 50:
            prefix_prev = "-".join(prev["code"].split("-")[:3])
            prefix_curr = "-".join(curr["code"].split("-")[:3])
            if prefix_prev == prefix_curr:
                anomalies.append({
                    "polling_unit_code": curr["code"], "pu_name": curr["name"],
                    "anomaly_type": "identical_adjacent", "severity": "warning",
                    "score": curr["total_votes"],
                    "detail": f"Identical count ({curr['total_votes']}) as adjacent {prev['code']}",
                    "total_votes": curr["total_votes"], "registered_voters": curr["registered_voters"],
                    "turnout_pct": round(curr["turnout"], 2)
                })
    return anomalies

@app.get("/health")
def health():
    return {"status": "healthy", "service": "ai-analytics", "engine": "python-statistical", "version": "2.0.0"}

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
    return {"election_id": election_id, "analysis_type": "turnout",
            "query_ms": round(elapsed, 2), "data": [dict(r) for r in rows]}

@app.get("/analytics/{election_id}/party_performance")
def party_performance(election_id: int):
    start = time.time()
    db = get_db()
    rows = db.execute("""
        SELECT p.abbreviation, p.name, p.color,
            COALESCE(SUM(rv.votes), 0) as total_votes,
            COUNT(DISTINCT r.id) as results_count
        FROM parties p
        LEFT JOIN result_votes rv ON rv.party_id = p.id
        LEFT JOIN results r ON r.id = rv.result_id AND r.election_id = ?
        GROUP BY p.id ORDER BY total_votes DESC
    """, (election_id,)).fetchall()
    db.close()
    total_all = sum(r["total_votes"] for r in rows)
    elapsed = (time.time() - start) * 1000
    data = []
    for r in rows:
        d = dict(r)
        d["vote_share_pct"] = round(d["total_votes"] / total_all * 100, 2) if total_all > 0 else 0
        data.append(d)
    return {"election_id": election_id, "analysis_type": "party_performance",
            "query_ms": round(elapsed, 2), "data": data}

@app.get("/analytics/{election_id}/timeline")
def timeline_analytics(election_id: int, interval: str = Query("hour", pattern="^(hour|day|minute)$")):
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
    return {"election_id": election_id, "analysis_type": "timeline",
            "interval": interval, "query_ms": round(elapsed, 2), "data": data}

@app.get("/analytics/{election_id}/anomalies")
def anomaly_detection(election_id: int, severity: Optional[str] = None):
    """AI-powered anomaly detection: overvote, z-score outliers, Benford's law, party dominance, patterns."""
    start = time.time()
    db = get_db()
    rows = db.execute("""
        SELECT r.polling_unit_code as code, pu.name, pu.registered_voters,
            COALESCE(SUM(rv.votes), 0) as total_votes, r.rejected_votes, r.accredited_voters
        FROM results r
        JOIN polling_units pu ON r.polling_unit_code = pu.code
        LEFT JOIN result_votes rv ON rv.result_id = r.id
        WHERE r.election_id = ?
        GROUP BY r.id
    """, (election_id,)).fetchall()

    results = []
    vote_totals = []
    for r in rows:
        d = dict(r)
        registered = d["registered_voters"] or 0
        total = d["total_votes"] or 0
        d["registered_voters"] = registered
        d["total_votes"] = total
        d["turnout"] = (total / registered * 100) if registered > 0 else 0
        results.append(d)
        if total > 0:
            vote_totals.append(total)

    party_rows = db.execute("""
        SELECT r.polling_unit_code as code, pu.name as pu_name, pu.registered_voters as registered,
            p.abbreviation as party, rv.votes
        FROM result_votes rv
        JOIN results r ON rv.result_id = r.id
        JOIN parties p ON rv.party_id = p.id
        JOIN polling_units pu ON r.polling_unit_code = pu.code
        WHERE r.election_id = ?
        ORDER BY r.polling_unit_code, rv.votes DESC
    """, (election_id,)).fetchall()
    db.close()

    pu_party_votes = defaultdict(list)
    party_vote_totals = []
    for pr in party_rows:
        d = dict(pr)
        pu_party_votes[d["code"]].append(d)
        if d["votes"] > 0:
            party_vote_totals.append(d["votes"])

    all_anomalies = []
    all_anomalies.extend(detect_overvoting(results))
    all_anomalies.extend(detect_turnout_outliers(results))
    all_anomalies.extend(detect_party_dominance(pu_party_votes))
    all_anomalies.extend(detect_round_number_bias(results))
    all_anomalies.extend(detect_sequential_patterns(results))

    if severity:
        all_anomalies = [a for a in all_anomalies if a["severity"] == severity]

    sev_order = {"critical": 0, "warning": 1, "minor": 2, "info": 3}
    all_anomalies.sort(key=lambda a: (sev_order.get(a["severity"], 9), -a["score"]))

    turnouts = [r["turnout"] for r in results if r["turnout"] > 0]
    m_turnout, s_turnout = mean_std(turnouts)
    med_turnout = median_val(turnouts)
    benford_first = benfords_first_digit(vote_totals)
    benford_last = benfords_last_digit(vote_totals)
    benford_party = benfords_first_digit(party_vote_totals)

    counts = Counter(a["severity"] for a in all_anomalies)
    type_counts = Counter(a["anomaly_type"] for a in all_anomalies)
    elapsed = (time.time() - start) * 1000
    return {
        "election_id": election_id, "analysis_type": "ai_anomaly_detection",
        "query_ms": round(elapsed, 2), "total_analyzed": len(results),
        "total_anomalies": len(all_anomalies),
        "summary": {"critical": counts.get("critical", 0), "warning": counts.get("warning", 0),
                     "minor": counts.get("minor", 0), "info": counts.get("info", 0),
                     "by_type": dict(type_counts)},
        "statistics": {"mean_turnout": round(m_turnout, 2), "median_turnout": round(med_turnout, 2),
                       "std_turnout": round(s_turnout, 2), "total_results": len(results)},
        "benford_analysis": {"first_digit_votes": benford_first, "last_digit_votes": benford_last,
                             "first_digit_party_votes": benford_party},
        "anomalies": all_anomalies,
    }

@app.get("/analytics/{election_id}/benford")
def benford_analysis(election_id: int):
    """Dedicated Benford's Law analysis."""
    start = time.time()
    db = get_db()
    vote_rows = db.execute("""
        SELECT COALESCE(SUM(rv.votes), 0) as total
        FROM results r LEFT JOIN result_votes rv ON rv.result_id = r.id
        WHERE r.election_id = ? GROUP BY r.id
    """, (election_id,)).fetchall()
    vote_totals = [r["total"] for r in vote_rows if r["total"] > 0]

    party_rows = db.execute("""
        SELECT rv.votes FROM result_votes rv
        JOIN results r ON rv.result_id = r.id
        WHERE r.election_id = ? AND rv.votes > 0
    """, (election_id,)).fetchall()
    party_votes = [r["votes"] for r in party_rows]

    acc_rows = db.execute("""
        SELECT accredited_voters FROM results
        WHERE election_id = ? AND accredited_voters > 0
    """, (election_id,)).fetchall()
    acc_values = [r["accredited_voters"] for r in acc_rows]
    db.close()
    elapsed = (time.time() - start) * 1000
    return {
        "election_id": election_id, "analysis_type": "benford", "query_ms": round(elapsed, 2),
        "total_vote_counts": {"first_digit": benfords_first_digit(vote_totals),
                              "last_digit": benfords_last_digit(vote_totals)},
        "party_vote_counts": {"first_digit": benfords_first_digit(party_votes),
                              "last_digit": benfords_last_digit(party_votes)},
        "accredited_voters": {"first_digit": benfords_first_digit(acc_values),
                              "last_digit": benfords_last_digit(acc_values)},
    }

@app.get("/analytics/{election_id}/integrity_score")
def integrity_score(election_id: int):
    """Composite election integrity score (0-100) from all AI checks."""
    start = time.time()
    anom = anomaly_detection(election_id)
    ben = benford_analysis(election_id)

    score = 100.0
    total = anom["total_analyzed"]
    if total == 0:
        return {"election_id": election_id, "integrity_score": 0, "detail": "No data"}

    crit = anom["summary"]["critical"]
    warn = anom["summary"]["warning"]
    minor = anom["summary"]["minor"]
    score -= min(40, crit / total * 400)
    score -= min(20, warn / total * 200)
    score -= min(10, minor / total * 100)

    bf1 = ben["total_vote_counts"]["first_digit"]
    bf_last = ben["total_vote_counts"]["last_digit"]
    bf1_pen = 10 if (isinstance(bf1, dict) and bf1.get("status") == "fail") else 5 if (isinstance(bf1, dict) and bf1.get("status") == "suspicious") else 0
    bfl_pen = 10 if (isinstance(bf_last, dict) and bf_last.get("status") == "fail") else 5 if (isinstance(bf_last, dict) and bf_last.get("status") == "suspicious") else 0
    score -= bf1_pen
    score -= bfl_pen
    score = max(0, min(100, score))
    grade = "A" if score >= 90 else "B" if score >= 75 else "C" if score >= 60 else "D" if score >= 40 else "F"

    elapsed = (time.time() - start) * 1000
    return {
        "election_id": election_id, "integrity_score": round(score, 1), "grade": grade,
        "query_ms": round(elapsed, 2),
        "breakdown": {
            "base_score": 100,
            "critical_penalty": round(min(40, crit / total * 400), 1),
            "warning_penalty": round(min(20, warn / total * 200), 1),
            "minor_penalty": round(min(10, minor / total * 100), 1),
            "benford_first_penalty": bf1_pen, "benford_last_penalty": bfl_pen,
        },
        "anomaly_summary": anom["summary"],
        "benford_summary": {
            "first_digit": bf1.get("status", "n/a") if isinstance(bf1, dict) else "n/a",
            "last_digit": bf_last.get("status", "n/a") if isinstance(bf_last, dict) else "n/a",
        },
        "methods_used": [
            "Overvote detection", "Z-score turnout outlier analysis",
            "IQR-based turnout outlier detection", "Single-party dominance (>90%)",
            "Round number bias detection", "Sequential pattern detection",
            "Benford's Law first-digit", "Benford's Law last-digit uniformity",
        ],
    }

@app.get("/ai/methods")
def ai_methods():
    return {"methods": [
        {"id": "overvote", "name": "Overvote Detection", "description": "Detects PUs where votes exceed registered voters", "type": "rule-based", "severity": "critical"},
        {"id": "turnout_outlier", "name": "Turnout Outlier", "description": "Z-score and IQR to find anomalous turnout", "type": "statistical", "severity": "warning-critical"},
        {"id": "party_dominance", "name": "Party Dominance", "description": "One party >90% of votes", "type": "statistical", "severity": "warning-critical"},
        {"id": "round_number", "name": "Round Number Bias", "description": "Suspicious multiples of 50/100", "type": "pattern", "severity": "minor"},
        {"id": "identical_adjacent", "name": "Sequential Patterns", "description": "Identical counts in adjacent PUs", "type": "pattern", "severity": "warning"},
        {"id": "benford_first", "name": "Benford First Digit", "description": "Chi-square test vs Benford distribution", "type": "statistical", "severity": "info"},
        {"id": "benford_last", "name": "Benford Last Digit", "description": "Last-digit uniformity test", "type": "statistical", "severity": "info"},
        {"id": "integrity_score", "name": "Integrity Score", "description": "Composite 0-100 score from all checks", "type": "composite", "severity": "n/a"},
    ]}

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
        return {"columns": cols, "rows": [dict(r) for r in rows],
                "count": len(rows), "query_ms": round(elapsed, 2)}
    except Exception as e:
        db.close()
        return {"error": str(e)}

if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("PORT", "8090"))
    uvicorn.run(app, host="0.0.0.0", port=port)
