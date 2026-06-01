"""Embedded AI analytics endpoints - statistical anomaly detection."""
import os
import time
import math
import sqlite3
from collections import Counter, defaultdict
from typing import Optional
from fastapi import APIRouter, Query

router = APIRouter()
DB_PATH = os.getenv("DB_PATH", "/data/app.db")

def get_db():
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    return conn

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

def _load_results(election_id):
    db = get_db()
    rows = db.execute("""
        SELECT r.polling_unit_code as code, pu.name, pu.registered_voters,
            COALESCE(SUM(rps.votes), 0) as total_votes, r.rejected_votes, r.accredited_voters
        FROM results r
        JOIN polling_units pu ON r.polling_unit_code = pu.code
        LEFT JOIN result_party_scores rps ON rps.result_id = r.id
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
            p.code as party, rps.votes
        FROM result_party_scores rps
        JOIN results r ON rps.result_id = r.id
        JOIN parties p ON rps.party_code = p.code
        JOIN polling_units pu ON r.polling_unit_code = pu.code
        WHERE r.election_id = ?
        ORDER BY r.polling_unit_code, rps.votes DESC
    """, (election_id,)).fetchall()
    db.close()
    pu_party_votes = defaultdict(list)
    party_vote_totals = []
    for pr in party_rows:
        d = dict(pr)
        pu_party_votes[d["code"]].append(d)
        if d["votes"] > 0:
            party_vote_totals.append(d["votes"])
    return results, vote_totals, pu_party_votes, party_vote_totals


@router.get("/ai/anomalies")
def anomaly_detection(election_id: int = 1, severity: Optional[str] = None):
    start = time.time()
    results, vote_totals, pu_party_votes, party_vote_totals = _load_results(election_id)
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
    benford_first = benfords_first_digit(vote_totals)
    benford_party = benfords_first_digit(party_vote_totals)
    counts = Counter(a["severity"] for a in all_anomalies)
    type_counts = Counter(a["anomaly_type"] for a in all_anomalies)
    elapsed = (time.time() - start) * 1000
    return {
        "election_id": election_id, "total_analyzed": len(results),
        "total_anomalies": len(all_anomalies),
        "summary": {"critical": counts.get("critical", 0), "warning": counts.get("warning", 0),
                     "minor": counts.get("minor", 0), "info": counts.get("info", 0)},
        "statistics": {"mean_turnout": round(m_turnout, 2), "std_turnout": round(s_turnout, 2)},
        "benford_analysis": {"vote_totals": benford_first, "party_votes": benford_party},
        "anomalies": all_anomalies[:200],
        "query_ms": round(elapsed, 2)
    }


@router.get("/ai/benford")
def benford_analysis(election_id: int = 1):
    start = time.time()
    results, vote_totals, _, party_vote_totals = _load_results(election_id)
    first_digit = benfords_first_digit(vote_totals)
    last_digit = benfords_last_digit(vote_totals)
    party_first = benfords_first_digit(party_vote_totals)
    elapsed = (time.time() - start) * 1000
    return {
        "election_id": election_id,
        "first_digit_analysis": first_digit,
        "last_digit_analysis": last_digit,
        "party_vote_analysis": party_first,
        "sample_size": len(vote_totals),
        "query_ms": round(elapsed, 2),
        **first_digit
    }


@router.get("/ai/integrity")
def integrity_score(election_id: int = 1):
    start = time.time()
    results, vote_totals, pu_party_votes, party_vote_totals = _load_results(election_id)
    overvotes = detect_overvoting(results)
    outliers = detect_turnout_outliers(results)
    dominance = detect_party_dominance(pu_party_votes)
    round_nums = detect_round_number_bias(results)
    sequential = detect_sequential_patterns(results)
    benford = benfords_first_digit(vote_totals)
    score = 100.0
    n = max(len(results), 1)
    score -= min(30, len(overvotes) / n * 100 * 3)
    score -= min(20, len(outliers) / n * 100 * 2)
    score -= min(15, len(dominance) / n * 100 * 1.5)
    score -= min(10, len(round_nums) / n * 100)
    score -= min(10, len(sequential) / n * 100)
    if benford.get("status") == "fail":
        score -= 15
    elif benford.get("status") == "suspicious":
        score -= 7
    score = max(0, min(100, round(score, 1)))
    if score >= 90:
        grade = "A"
    elif score >= 80:
        grade = "B"
    elif score >= 70:
        grade = "C"
    elif score >= 60:
        grade = "D"
    else:
        grade = "F"
    elapsed = (time.time() - start) * 1000
    return {
        "election_id": election_id, "integrity_score": score, "grade": grade,
        "breakdown": {
            "overvoting_penalty": len(overvotes), "outlier_penalty": len(outliers),
            "dominance_penalty": len(dominance), "round_number_penalty": len(round_nums),
            "sequential_penalty": len(sequential),
            "benford_status": benford.get("status", "unknown")
        },
        "methods_used": ["benfords_law", "z_score", "iqr", "party_dominance", "round_number", "sequential_pattern"],
        "total_results_analyzed": len(results),
        "query_ms": round(elapsed, 2)
    }


@router.get("/ai/methods")
def ai_methods():
    return {
        "methods": [
            {"name": "benfords_law", "description": "Chi-square test of first/last digit distribution"},
            {"name": "z_score_outlier", "description": "Z-score based turnout outlier detection"},
            {"name": "iqr_outlier", "description": "IQR-based vote count outlier detection"},
            {"name": "party_dominance", "description": "Single-party dominance detection (>90% share)"},
            {"name": "round_number_bias", "description": "Round number vote total detection"},
            {"name": "sequential_pattern", "description": "Identical/sequential pattern detection across adjacent PUs"},
        ],
        "engine": "python-statistical",
        "version": "2.0.0"
    }
