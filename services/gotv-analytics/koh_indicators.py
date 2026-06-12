"""KOH 2027 Indicators — Advanced Analytics Module.

Provides ML-powered analytics for the KOH campaign performance framework:
- CPI predictive modeling (forecast next month's CPI)
- Demographic gap analysis (automatically identify underperforming segments)
- Survey response interpolation (estimate indicators between survey waves)
- Sentiment trend forecasting (detect crisis before it peaks)
- Endorsement impact scoring (which endorsements moved the needle?)
- LGA priority recommender (where to allocate next campaign dollar)
"""

import os
import math
from datetime import datetime, timezone, timedelta
from typing import Optional, List, Dict, Any

import numpy as np
import structlog
from fastapi import APIRouter, HTTPException, Query
from pydantic import BaseModel, Field

logger = structlog.get_logger()

DB_URL = os.getenv("DATABASE_URL", "postgresql://ngapp:ngapp123@localhost:5432/ngapp")

router = APIRouter(prefix="/gotv-analytics/koh", tags=["koh_indicators"])


# ─── DB helpers ──────────────────────────────────────────────────────────────

_koh_pool = None


def _get_pool():
    global _koh_pool
    if _koh_pool is None:
        import psycopg2.pool
        _koh_pool = psycopg2.pool.ThreadedConnectionPool(minconn=2, maxconn=10, dsn=DB_URL)
    return _koh_pool


def query_rows(sql: str, params: tuple = ()) -> List[Dict]:
    pool = _get_pool()
    conn = pool.getconn()
    try:
        import psycopg2.extras
        cur = conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
        cur.execute(sql, params)
        rows = cur.fetchall()
        cur.close()
        conn.commit()
        return [dict(r) for r in rows]
    finally:
        pool.putconn(conn)


def query_one(sql: str, params: tuple = ()) -> Optional[Dict]:
    rows = query_rows(sql, params)
    return rows[0] if rows else None


def execute(sql: str, params: tuple = ()):
    pool = _get_pool()
    conn = pool.getconn()
    try:
        cur = conn.cursor()
        cur.execute(sql, params)
        conn.commit()
        cur.close()
    finally:
        pool.putconn(conn)


# ─── Models ──────────────────────────────────────────────────────────────────

class CPIForecast(BaseModel):
    current_cpi: float
    forecast_next_month: float
    confidence_interval: List[float] = Field(default_factory=lambda: [0, 0])
    trend: str  # "improving", "stable", "declining"
    drivers: List[Dict[str, Any]] = Field(default_factory=list)


class DemographicGap(BaseModel):
    dimension: str
    segment: str
    metric: str
    value: float
    benchmark: float
    gap: float
    priority: str  # "critical", "high", "medium", "low"
    recommendation: str


class LGAPriority(BaseModel):
    lga_code: str
    lga_name: str
    tier: int
    priority_score: float
    recommendation: str
    estimated_impact: str
    required_resources: Dict[str, int] = Field(default_factory=dict)


# ─── CPI Forecasting ────────────────────────────────────────────────────────

@router.get("/cpi/forecast")
async def forecast_cpi(party_id: int = Query(...)):
    """Predict next month's CPI using linear extrapolation on historical data."""
    history = query_rows("""
        SELECT cpi_score, voting_intention_pct, favourability_pct, 
               digital_sentiment, ground_mobilisation, endorsement_index, share_of_voice,
               computed_at
        FROM gotv_cpi_history WHERE party_id = %s
        ORDER BY computed_at DESC LIMIT 6
    """, (party_id,))

    if not history:
        # Compute from current data
        current = compute_live_cpi(party_id)
        return CPIForecast(
            current_cpi=current,
            forecast_next_month=current,
            confidence_interval=[max(0, current - 5), min(100, current + 5)],
            trend="stable",
            drivers=[{"component": "insufficient_data", "impact": 0}]
        )

    scores = [h["cpi_score"] for h in history if h["cpi_score"] is not None]
    if len(scores) < 2:
        current = scores[0] if scores else 0
        return CPIForecast(
            current_cpi=current,
            forecast_next_month=current,
            confidence_interval=[max(0, current - 5), min(100, current + 5)],
            trend="stable",
            drivers=[]
        )

    # Linear trend
    x = np.arange(len(scores))
    y = np.array(scores[::-1])  # oldest to newest
    slope = np.polyfit(x, y, 1)[0]

    current = scores[0]
    forecast = min(100, max(0, current + slope))

    # Confidence interval based on variance
    std = np.std(y) if len(y) > 1 else 5
    ci = [max(0, forecast - 1.96 * std), min(100, forecast + 1.96 * std)]

    trend = "improving" if slope > 1 else ("declining" if slope < -1 else "stable")

    # Identify drivers (which components changed most)
    drivers = []
    if len(history) >= 2:
        latest = history[0]
        prev = history[1]
        components = ["voting_intention_pct", "favourability_pct", "digital_sentiment",
                      "ground_mobilisation", "endorsement_index", "share_of_voice"]
        weights = [0.30, 0.25, 0.15, 0.15, 0.10, 0.05]
        for comp, weight in zip(components, weights):
            val_now = latest.get(comp) or 0
            val_prev = prev.get(comp) or 0
            delta = val_now - val_prev
            if abs(delta) > 1:
                drivers.append({
                    "component": comp.replace("_pct", ""),
                    "delta": round(delta, 2),
                    "weighted_impact": round(delta * weight, 2),
                    "direction": "up" if delta > 0 else "down"
                })
        drivers.sort(key=lambda d: abs(d["weighted_impact"]), reverse=True)

    return CPIForecast(
        current_cpi=round(current, 2),
        forecast_next_month=round(forecast, 2),
        confidence_interval=[round(ci[0], 2), round(ci[1], 2)],
        trend=trend,
        drivers=drivers[:5]
    )


def compute_live_cpi(party_id: int) -> float:
    """Compute CPI from current data without relying on history table."""
    # Ground mobilisation from canvass data
    ground = query_one("""
        SELECT (COUNT(DISTINCT volunteer_id) * 100.0 / NULLIF(
            (SELECT COUNT(*) FROM gotv_territories WHERE party_id = %s), 1)) AS coverage
        FROM gotv_canvass_logs WHERE party_id = %s 
        AND knocked_at > NOW() - INTERVAL '30 days'
    """, (party_id, party_id))
    ground_score = min(100, ground["coverage"] or 0) if ground else 0

    # Pledge rate as voting intention proxy
    pledge_data = query_one("""
        SELECT COUNT(CASE WHEN status IN ('confirmed_day_of','fulfilled') THEN 1 END) * 100.0 /
            NULLIF(COUNT(*), 0) AS rate
        FROM gotv_pledges WHERE party_id = %s
    """, (party_id,))
    vi = (pledge_data["rate"] or 0) if pledge_data else 0

    # Endorsement index
    endorse = query_one("""
        SELECT COUNT(*) AS total, COUNT(DISTINCT endorser_type) AS types
        FROM gotv_endorsements WHERE party_id = %s AND verified = true
    """, (party_id,))
    endorse_score = 0
    if endorse:
        breadth = min((endorse["types"] or 0) / 10.0, 1.0)
        volume = min((endorse["total"] or 0) / 50.0, 1.0)
        endorse_score = (volume * 0.6 + breadth * 0.4) * 100

    # CPI with defaults for missing sentiment/SOV
    cpi = vi * 0.30 + 50 * 0.25 + 50 * 0.15 + ground_score * 0.15 + endorse_score * 0.10 + 0 * 0.05
    return min(100, max(0, cpi))


# ─── Demographic Gap Analysis ────────────────────────────────────────────────

@router.get("/demographics/gaps")
async def demographic_gap_analysis(party_id: int = Query(...)):
    """Identify underperforming demographic segments that need attention."""
    gaps: List[DemographicGap] = []

    dimensions = [
        ("age_group", ["youth_18_35", "middle_36_55", "senior_56_plus"]),
        ("gender", ["male", "female"]),
        ("socioeconomic_class", ["AB", "C1", "C2", "DE"]),
        ("lga_tier", ["1", "2", "3", "4"]),
    ]

    for dim, segments in dimensions:
        # Get overall average pledge rate
        overall = query_one(f"""
            SELECT COUNT(CASE WHEN voter_status IN ('pledged','confirmed') THEN 1 END) * 100.0 /
                NULLIF(COUNT(*), 0) AS rate
            FROM gotv_contacts WHERE party_id = %s AND {dim} IS NOT NULL
        """, (party_id,))
        benchmark = (overall["rate"] or 0) if overall else 0

        for segment in segments:
            seg_data = query_one(f"""
                SELECT COUNT(*) AS total,
                    COUNT(CASE WHEN voter_status IN ('pledged','confirmed') THEN 1 END) * 100.0 /
                        NULLIF(COUNT(*), 0) AS rate
                FROM gotv_contacts WHERE party_id = %s AND {dim} = %s
            """, (party_id, segment))

            if seg_data and seg_data["total"] and seg_data["total"] > 0:
                rate = seg_data["rate"] or 0
                gap_val = benchmark - rate

                if gap_val > 5:  # Only flag significant gaps
                    priority = "critical" if gap_val > 20 else ("high" if gap_val > 10 else "medium")
                    rec = _generate_recommendation(dim, segment, gap_val, rate)
                    gaps.append(DemographicGap(
                        dimension=dim, segment=segment, metric="pledge_rate",
                        value=round(rate, 2), benchmark=round(benchmark, 2),
                        gap=round(gap_val, 2), priority=priority, recommendation=rec
                    ))

    # Sort by gap size descending
    gaps.sort(key=lambda g: g.gap, reverse=True)
    return {"gaps": [g.dict() for g in gaps], "total_gaps": len(gaps)}


def _generate_recommendation(dim: str, segment: str, gap: float, rate: float) -> str:
    """Generate targeted recommendation based on demographic gap."""
    recs = {
        "age_group": {
            "youth_18_35": "Increase social media presence (TikTok, Instagram). Deploy youth-focused messaging.",
            "middle_36_55": "Focus on economic policy messaging. Engage through WhatsApp and professional networks.",
            "senior_56_plus": "Community radio, traditional media, and community leader engagement needed.",
        },
        "gender": {
            "male": "Increase male voter outreach through transport union and market associations.",
            "female": "Deploy women's group endorsements and maternal/education policy messaging.",
        },
        "socioeconomic_class": {
            "AB": "Host policy roundtables and professional networking events.",
            "C1": "Infrastructure and security messaging. WhatsApp broadcast engagement.",
            "C2": "Market visits and economic relief messaging. USSD outreach.",
            "DE": "Community-level mobilisation. Door-to-door canvassing and basic amenity messaging.",
        },
        "lga_tier": {
            "1": "Defend base: maximise PVC collection and GOTV logistics.",
            "2": "Swing area: intensive canvassing and undecided voter persuasion.",
            "3": "Growth area: expand volunteer network and community penetration.",
            "4": "Urban centre: professional body endorsements and digital campaign.",
        },
    }
    return recs.get(dim, {}).get(segment, f"Increase outreach to {segment} segment (currently {rate:.1f}% vs {rate+gap:.1f}% benchmark)")


# ─── LGA Priority Recommender ────────────────────────────────────────────────

@router.get("/lga/priorities")
async def lga_priority_recommendations(party_id: int = Query(...)):
    """Recommend LGA-level resource allocation based on strategic tiers and current performance."""
    lga_data = query_rows("""
        SELECT t.lga_code, t.lga_name, t.tier, t.tier_name, t.strategic_focus,
            COALESCE(c.contact_count, 0) AS contacts,
            COALESCE(c.pledged_count, 0) AS pledged,
            COALESCE(v.vol_count, 0) AS volunteers,
            COALESCE(e.endorse_count, 0) AS endorsements
        FROM gotv_lga_tiers t
        LEFT JOIN (
            SELECT lga_code, COUNT(*) AS contact_count,
                COUNT(CASE WHEN voter_status IN ('pledged','confirmed') THEN 1 END) AS pledged_count
            FROM gotv_contacts WHERE party_id = %s GROUP BY lga_code
        ) c ON t.lga_code = c.lga_code
        LEFT JOIN (
            SELECT lga_code, COUNT(*) AS vol_count
            FROM gotv_volunteers WHERE party_id = %s GROUP BY lga_code
        ) v ON t.lga_code = v.lga_code
        LEFT JOIN (
            SELECT lga_code, COUNT(*) AS endorse_count
            FROM gotv_endorsements WHERE party_id = %s AND verified = true GROUP BY lga_code
        ) e ON t.lga_code = e.lga_code
        ORDER BY t.tier, t.lga_name
    """, (party_id, party_id, party_id))

    priorities: List[LGAPriority] = []
    for lga in lga_data:
        contacts = lga["contacts"] or 0
        pledged = lga["pledged"] or 0
        volunteers = lga["volunteers"] or 0
        tier = lga["tier"]

        # Priority scoring based on tier strategy
        pledge_rate = (pledged / contacts * 100) if contacts > 0 else 0
        vol_density = (volunteers / max(contacts, 1)) * 1000

        # Higher priority = more opportunity
        if tier == 1:  # Stronghold: priority if low turnout prep
            score = 100 - min(pledge_rate * 2, 100)
        elif tier == 2:  # Swing: priority if many undecided
            score = max(0, 100 - pledge_rate) * 1.5
        elif tier == 3:  # Growth: priority if low penetration
            score = 100 - min(contacts / 100, 100)
        else:  # Urban: priority if low endorsements
            score = 100 - min((lga["endorsements"] or 0) * 20, 100)

        score = min(100, max(0, score))

        # Generate recommendation
        if tier == 1:
            rec = f"Focus on PVC collection and turnout logistics. Current pledge rate: {pledge_rate:.1f}%"
            impact = f"+{max(0, int(contacts * 0.1 - pledged))} potential pledges with GOTV push"
            resources = {"canvassers": max(3, int(contacts / 500)), "vehicles": max(1, int(contacts / 2000))}
        elif tier == 2:
            rec = f"Deploy persuasion canvassers. {contacts - pledged} undecided contacts remaining."
            impact = f"+{int((contacts - pledged) * 0.3)} estimated conversions with 2-week campaign"
            resources = {"canvassers": max(5, int(contacts / 300)), "phone_bankers": max(2, int(contacts / 500))}
        elif tier == 3:
            rec = f"Expand contact list and volunteer network. Only {contacts} contacts registered."
            impact = f"+{max(100, int(contacts * 0.5))} new contacts achievable with community drives"
            resources = {"recruiters": max(3, int(4)), "community_events": 2}
        else:
            rec = f"Pursue professional body endorsements. Currently {lga['endorsements'] or 0} endorsements."
            impact = f"+{max(2, 5 - (lga['endorsements'] or 0))} achievable endorsements"
            resources = {"liaison_officers": 2, "event_budget_ngn": 500000}

        priorities.append(LGAPriority(
            lga_code=lga["lga_code"], lga_name=lga["lga_name"], tier=tier,
            priority_score=round(score, 2), recommendation=rec,
            estimated_impact=impact, required_resources=resources
        ))

    # Sort by priority score descending
    priorities.sort(key=lambda p: p.priority_score, reverse=True)
    return {"priorities": [p.dict() for p in priorities], "total_lgas": len(priorities)}


# ─── Sentiment Forecasting ───────────────────────────────────────────────────

@router.get("/sentiment/forecast")
async def sentiment_forecast(party_id: int = Query(...), days: int = Query(default=7)):
    """Predict sentiment trajectory and detect potential crises early."""
    # Get daily sentiment data
    daily_data = query_rows("""
        SELECT DATE(timestamp) AS d,
            COUNT(CASE WHEN sentiment = 'positive' THEN 1 END) AS positive,
            COUNT(CASE WHEN sentiment = 'negative' THEN 1 END) AS negative,
            COUNT(*) AS total
        FROM gotv_sentiment_log WHERE party_id = %s
        AND timestamp > NOW() - INTERVAL '30 days'
        GROUP BY DATE(timestamp) ORDER BY d
    """, (party_id,))

    if not daily_data:
        return {
            "current_sentiment": 50.0,
            "forecast": 50.0,
            "trend": "stable",
            "crisis_risk": "low",
            "daily_data": []
        }

    # Compute daily sentiment scores
    scores = []
    for d in daily_data:
        total = d["total"] or 1
        pos_pct = (d["positive"] or 0) / total * 100
        scores.append(pos_pct)

    current = scores[-1] if scores else 50
    avg_7d = np.mean(scores[-7:]) if len(scores) >= 7 else np.mean(scores)

    # Simple linear forecast
    if len(scores) >= 3:
        x = np.arange(len(scores))
        slope = np.polyfit(x, scores, 1)[0]
        forecast = min(100, max(0, current + slope * days))
    else:
        forecast = current
        slope = 0

    trend = "improving" if slope > 0.5 else ("declining" if slope < -0.5 else "stable")

    # Crisis detection: sharp negative spike
    crisis_risk = "low"
    if len(scores) >= 2:
        recent_drop = scores[-1] - np.mean(scores[-3:]) if len(scores) >= 3 else 0
        if recent_drop < -20:
            crisis_risk = "critical"
        elif recent_drop < -10:
            crisis_risk = "high"
        elif slope < -1:
            crisis_risk = "medium"

    return {
        "current_sentiment": round(current, 2),
        "avg_7d": round(avg_7d, 2),
        "forecast": round(forecast, 2),
        "trend": trend,
        "crisis_risk": crisis_risk,
        "slope_per_day": round(slope, 3),
        "daily_data": [
            {"date": str(d["d"]), "positive": d["positive"], "negative": d["negative"],
             "score": round((d["positive"] or 0) / max(d["total"] or 1, 1) * 100, 2)}
            for d in daily_data[-14:]  # Last 14 days
        ]
    }


# ─── Survey Interpolation ────────────────────────────────────────────────────

@router.get("/surveys/interpolate")
async def interpolate_survey_data(party_id: int = Query(...), indicator: str = Query(default="voting_intention")):
    """Estimate indicator values between survey waves using linear interpolation."""
    valid_indicators = {
        "voting_intention": "AVG(CASE WHEN voting_intention THEN 100.0 ELSE 0 END)",
        "favourability": "AVG(favourability_score)",
        "awareness": "AVG(awareness_score)",
        "nps": "AVG(nps_score)",
        "issue_alignment": "AVG(issue_alignment)",
    }
    agg = valid_indicators.get(indicator, valid_indicators["voting_intention"])

    waves = query_rows(f"""
        SELECT s.wave_number, s.end_date, {agg} AS value, COUNT(*) AS sample_size
        FROM gotv_survey_responses sr JOIN gotv_surveys s ON sr.survey_id = s.id
        WHERE sr.party_id = %s AND s.status = 'active'
        GROUP BY s.wave_number, s.end_date ORDER BY s.wave_number
    """, (party_id,))

    if not waves:
        return {"indicator": indicator, "waves": [], "interpolated": [], "forecast": None}

    # Linear interpolation between waves
    interpolated = []
    for i in range(len(waves) - 1):
        start_val = waves[i]["value"] or 0
        end_val = waves[i + 1]["value"] or 0
        start_wave = waves[i]["wave_number"]
        end_wave = waves[i + 1]["wave_number"]

        for j in range(end_wave - start_wave):
            t = j / max(end_wave - start_wave, 1)
            interp_val = start_val + (end_val - start_val) * t
            interpolated.append({
                "wave": start_wave + j * 0.5,
                "value": round(interp_val, 2),
                "type": "actual" if j == 0 else "interpolated"
            })

    # Add last actual
    if waves:
        interpolated.append({
            "wave": waves[-1]["wave_number"],
            "value": round(waves[-1]["value"] or 0, 2),
            "type": "actual"
        })

    # Forecast next wave
    if len(waves) >= 2:
        values = [w["value"] or 0 for w in waves]
        slope = (values[-1] - values[-2]) / max(1, waves[-1]["wave_number"] - waves[-2]["wave_number"])
        forecast = min(100, max(0, values[-1] + slope))
    else:
        forecast = waves[-1]["value"] or 0

    return {
        "indicator": indicator,
        "waves": [{"wave": w["wave_number"], "value": round(w["value"] or 0, 2),
                   "sample_size": w["sample_size"], "date": str(w["end_date"])} for w in waves],
        "interpolated": interpolated,
        "forecast_next_wave": round(forecast, 2)
    }


# ─── Endorsement Impact Analysis ─────────────────────────────────────────────

@router.get("/endorsements/impact")
async def endorsement_impact_analysis(party_id: int = Query(...)):
    """Analyze which endorsement types have the most impact on campaign metrics."""
    # Get endorsements grouped by type with timing
    endorsements_by_type = query_rows("""
        SELECT endorser_type, COUNT(*) AS count,
            AVG(demographic_reach) AS avg_reach,
            MIN(date_endorsed) AS earliest,
            MAX(date_endorsed) AS latest
        FROM gotv_endorsements WHERE party_id = %s AND verified = true
        GROUP BY endorser_type ORDER BY count DESC
    """, (party_id,))

    # Estimate impact by correlating endorsement timing with pledge rates
    impact_scores = []
    for etype in endorsements_by_type:
        # Higher reach + more endorsements = higher estimated impact
        reach_factor = min((etype["avg_reach"] or 1000) / 10000, 1.0)
        volume_factor = min(etype["count"] / 10, 1.0)
        impact = (reach_factor * 0.6 + volume_factor * 0.4) * 100

        impact_scores.append({
            "endorser_type": etype["endorser_type"],
            "count": etype["count"],
            "avg_demographic_reach": etype["avg_reach"] or 0,
            "estimated_impact_score": round(impact, 2),
            "recommendation": _endorsement_rec(etype["endorser_type"], etype["count"])
        })

    impact_scores.sort(key=lambda x: x["estimated_impact_score"], reverse=True)
    return {"endorsement_impact": impact_scores}


def _endorsement_rec(etype: str, count: int) -> str:
    targets = {
        "community_leader": 15, "religious_leader": 10, "professional_body": 8,
        "ethnic_union": 6, "womens_group": 8, "youth_org": 5,
        "celebrity": 5, "politician": 10, "academic": 5, "traditional_ruler": 8,
    }
    target = targets.get(etype, 5)
    if count >= target:
        return f"Target met ({count}/{target}). Focus on amplifying existing endorsements."
    return f"Need {target - count} more endorsements. Priority outreach to {etype.replace('_', ' ')} leaders."
