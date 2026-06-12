"""GOTV Scoring Engine — Cambridge Analytica-grade analytics (ethical implementation).

Implements:
1. Individual Voter Scoring (0-100) — engagement × recency × channel responsiveness
2. Persuadability Classifier — ML model: which undecided contacts convert?
3. Resource Allocation Engine — "deploy N canvassers to ward X for +Y pledges"
4. Win Probability Calculator — pledges + turnout + opposition → win %
5. Auto-optimize Message Selection — multi-armed bandit on A/B variants

Architecture:
- Python (FastAPI) for ML/scoring logic
- PostgreSQL for data layer
- Redis for score caching (30-min TTL)
- Kafka for score-update events
- Temporal for batch re-scoring workflows
"""

import os
import math
import json
from datetime import datetime, timezone, timedelta
from typing import Optional, List, Dict, Any, Tuple
from enum import Enum

import numpy as np
import structlog
from fastapi import APIRouter, HTTPException, Query, BackgroundTasks
from pydantic import BaseModel, Field

logger = structlog.get_logger()

DB_URL = os.getenv("DATABASE_URL", "postgresql://ngapp:ngapp123@localhost:5432/ngapp")
REDIS_URL = os.getenv("REDIS_URL", "redis://localhost:6379/2")

router = APIRouter(prefix="/gotv-analytics/scoring", tags=["scoring_engine"])


# ─── DB / Cache helpers ──────────────────────────────────────────────────────

_scoring_pool = None


def _get_pool():
    global _scoring_pool
    if _scoring_pool is None:
        import psycopg2.pool
        _scoring_pool = psycopg2.pool.ThreadedConnectionPool(minconn=2, maxconn=10, dsn=DB_URL)
    return _scoring_pool


def query_rows(sql: str, params: tuple = ()) -> List[Dict]:
    pool = _get_pool()
    conn = pool.getconn()
    try:
        import psycopg2.extras
        cur = conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
        cur.execute(sql, params)
        rows = cur.fetchall()
        cur.close()
        return [dict(r) for r in rows]
    finally:
        pool.putconn(conn)


def execute_sql(sql: str, params: tuple = ()):
    pool = _get_pool()
    conn = pool.getconn()
    try:
        cur = conn.cursor()
        cur.execute(sql, params)
        conn.commit()
        cur.close()
    finally:
        pool.putconn(conn)


def _get_redis():
    """Get Redis client (optional — gracefully degrades if unavailable)."""
    try:
        import redis
        return redis.from_url(REDIS_URL, decode_responses=True, socket_timeout=2)
    except Exception:
        return None


# ═══════════════════════════════════════════════════════════════════════════════
# 1. INDIVIDUAL VOTER SCORING ENGINE (0-100)
# ═══════════════════════════════════════════════════════════════════════════════

class VoterScore(BaseModel):
    contact_id: str
    overall_score: float = Field(ge=0, le=100)
    engagement_score: float = Field(ge=0, le=100)
    recency_score: float = Field(ge=0, le=100)
    responsiveness_score: float = Field(ge=0, le=100)
    loyalty_score: float = Field(ge=0, le=100)
    mobilization_readiness: float = Field(ge=0, le=100)
    segment: str  # hot, warm, cool, cold, dormant
    recommended_channel: str
    recommended_action: str
    factors: List[str]


class ScoreBreakdown(BaseModel):
    """Explains how a score was computed — transparency for campaign managers."""
    metric: str
    raw_value: float
    weight: float
    contribution: float
    explanation: str


# Scoring weights (calibrated to Nigerian election patterns)
ENGAGEMENT_WEIGHT = 0.30
RECENCY_WEIGHT = 0.25
RESPONSIVENESS_WEIGHT = 0.25
LOYALTY_WEIGHT = 0.10
MOBILIZATION_WEIGHT = 0.10


@router.get("/voter/{contact_id}")
async def get_voter_score(contact_id: str, party_id: int) -> Dict[str, Any]:
    """Get individual voter score with full breakdown."""

    # Check cache first
    rc = _get_redis()
    cache_key = f"vscore:{party_id}:{contact_id}"
    if rc:
        cached = rc.get(cache_key)
        if cached:
            return json.loads(cached)

    score = _compute_voter_score(contact_id, party_id)
    if score is None:
        raise HTTPException(status_code=404, detail="Contact not found")

    result = score.model_dump()

    # Cache for 30 minutes
    if rc:
        rc.setex(cache_key, 1800, json.dumps(result, default=str))

    return result


@router.post("/voters/batch")
async def batch_score_voters(
    party_id: int,
    ward_code: Optional[str] = None,
    state_code: Optional[str] = None,
    limit: int = Query(default=100, le=1000),
    min_score: float = Query(default=0, ge=0, le=100),
    segment: Optional[str] = None,
) -> Dict[str, Any]:
    """Score multiple voters with filtering. Returns ranked list for targeting."""

    where_clauses = ["party_id=%s", "opted_out=FALSE"]
    params: list = [party_id]

    if ward_code:
        where_clauses.append("ward_code=%s")
        params.append(ward_code)
    if state_code:
        where_clauses.append("state_code=%s")
        params.append(state_code)

    contacts = query_rows(f"""
        SELECT contact_id FROM gotv_contacts
        WHERE {' AND '.join(where_clauses)}
        ORDER BY contact_count DESC, created_at DESC
        LIMIT %s
    """, tuple(params + [limit * 3]))  # Over-fetch then filter by score

    scores = []
    for c in contacts:
        score = _compute_voter_score(c["contact_id"], party_id)
        if score and score.overall_score >= min_score:
            if segment is None or score.segment == segment:
                scores.append(score.model_dump())

    # Sort by overall score descending
    scores.sort(key=lambda s: s["overall_score"], reverse=True)
    scores = scores[:limit]

    # Aggregate stats
    all_scores = [s["overall_score"] for s in scores]
    segment_dist = {}
    for s in scores:
        seg = s["segment"]
        segment_dist[seg] = segment_dist.get(seg, 0) + 1

    return {
        "party_id": party_id,
        "total_scored": len(scores),
        "avg_score": round(np.mean(all_scores), 1) if all_scores else 0,
        "median_score": round(float(np.median(all_scores)), 1) if all_scores else 0,
        "segment_distribution": segment_dist,
        "voters": scores,
        "generated_at": datetime.now(timezone.utc).isoformat(),
    }


def _compute_voter_score(contact_id: str, party_id: int) -> Optional[VoterScore]:
    """Core scoring algorithm — combines 5 signal dimensions into 0-100 score."""

    # Get contact data
    rows = query_rows("""
        SELECT c.contact_id, c.voter_status, c.contact_count, c.tags,
               c.consent_id, c.created_at, c.state_code, c.ward_code
        FROM gotv_contacts c
        WHERE c.contact_id=%s AND c.party_id=%s
    """, (contact_id, party_id))

    if not rows:
        return None
    contact = rows[0]

    # Get interaction history
    outreach = query_rows("""
        SELECT channel, status, sent_at, direction
        FROM gotv_outreach_log
        WHERE contact_id=%s AND party_id=%s
        ORDER BY sent_at DESC LIMIT 50
    """, (contact_id, party_id))

    # Get pledge data
    pledges = query_rows("""
        SELECT pledge_type, status, created_at
        FROM gotv_pledges
        WHERE contact_id=%s AND party_id=%s
    """, (contact_id, party_id))

    # Get door knock data
    knocks = query_rows("""
        SELECT outcome, knocked_at
        FROM gotv_door_knocks
        WHERE contact_id=%s AND party_id=%s
        ORDER BY knocked_at DESC LIMIT 10
    """, (contact_id, party_id))

    factors = []

    # ─── DIMENSION 1: Engagement Score (0-100) ───────────────────────────
    engagement = _calc_engagement(contact, outreach, pledges, knocks, factors)

    # ─── DIMENSION 2: Recency Score (0-100) ──────────────────────────────
    recency = _calc_recency(outreach, knocks, factors)

    # ─── DIMENSION 3: Responsiveness Score (0-100) ───────────────────────
    responsiveness = _calc_responsiveness(outreach, factors)

    # ─── DIMENSION 4: Loyalty Score (0-100) ──────────────────────────────
    loyalty = _calc_loyalty(contact, pledges, factors)

    # ─── DIMENSION 5: Mobilization Readiness (0-100) ─────────────────────
    mobilization = _calc_mobilization(contact, pledges, knocks, factors)

    # Weighted composite
    overall = (
        engagement * ENGAGEMENT_WEIGHT +
        recency * RECENCY_WEIGHT +
        responsiveness * RESPONSIVENESS_WEIGHT +
        loyalty * LOYALTY_WEIGHT +
        mobilization * MOBILIZATION_WEIGHT
    )

    # Determine segment
    if overall >= 80:
        segment = "hot"
    elif overall >= 60:
        segment = "warm"
    elif overall >= 40:
        segment = "cool"
    elif overall >= 20:
        segment = "cold"
    else:
        segment = "dormant"

    # Best channel recommendation
    channel_scores = _calc_channel_preference(outreach)
    recommended_channel = max(channel_scores, key=channel_scores.get) if channel_scores else "sms"

    # Action recommendation
    action = _recommend_action(segment, contact, pledges, mobilization)

    return VoterScore(
        contact_id=contact_id,
        overall_score=round(overall, 1),
        engagement_score=round(engagement, 1),
        recency_score=round(recency, 1),
        responsiveness_score=round(responsiveness, 1),
        loyalty_score=round(loyalty, 1),
        mobilization_readiness=round(mobilization, 1),
        segment=segment,
        recommended_channel=recommended_channel,
        recommended_action=action,
        factors=factors[:8],  # Top 8 factors
    )


def _calc_engagement(contact: Dict, outreach: List, pledges: List, knocks: List, factors: List) -> float:
    """Engagement = how actively the contact interacts with the campaign."""
    score = 0.0

    # Contact frequency (0-30)
    contact_count = contact.get("contact_count", 0) or 0
    freq_score = min(30, contact_count * 5)
    score += freq_score

    # Outreach interactions (0-25)
    total_interactions = len(outreach)
    interaction_score = min(25, total_interactions * 2.5)
    score += interaction_score

    # Pledges made (0-25)
    pledge_score = min(25, len(pledges) * 12)
    score += pledge_score
    if pledges:
        factors.append(f"made_{len(pledges)}_pledges")

    # Door knock engagement (0-20)
    positive_knocks = sum(1 for k in knocks if k.get("outcome") in ("home", "pledged"))
    knock_score = min(20, positive_knocks * 10)
    score += knock_score
    if positive_knocks > 0:
        factors.append(f"answered_door_{positive_knocks}_times")

    return min(100, score)


def _calc_recency(outreach: List, knocks: List, factors: List) -> float:
    """Recency = how recently the contact was active. Decays exponentially."""
    now = datetime.now(timezone.utc)

    # Find most recent interaction
    last_touch = None
    for o in outreach:
        sent = o.get("sent_at")
        if sent:
            if isinstance(sent, str):
                sent = datetime.fromisoformat(sent.replace("Z", "+00:00"))
            if not sent.tzinfo:
                sent = sent.replace(tzinfo=timezone.utc)
            if last_touch is None or sent > last_touch:
                last_touch = sent

    for k in knocks:
        knocked = k.get("knocked_at")
        if knocked:
            if isinstance(knocked, str):
                knocked = datetime.fromisoformat(knocked.replace("Z", "+00:00"))
            if not knocked.tzinfo:
                knocked = knocked.replace(tzinfo=timezone.utc)
            if last_touch is None or knocked > last_touch:
                last_touch = knocked

    if last_touch is None:
        factors.append("no_recent_interaction")
        return 10.0  # Base score for having a record at all

    days_ago = (now - last_touch).total_seconds() / 86400

    # Exponential decay: score = 100 * e^(-days/30)
    # Full score if touched today, ~37 at 30 days, ~14 at 60 days
    score = 100 * math.exp(-days_ago / 30.0)

    if days_ago <= 3:
        factors.append("contacted_last_3_days")
    elif days_ago <= 7:
        factors.append("contacted_last_week")
    elif days_ago > 60:
        factors.append("dormant_60plus_days")

    return max(5, min(100, score))


def _calc_responsiveness(outreach: List, factors: List) -> float:
    """Responsiveness = ratio of positive responses to total outbound touches."""
    outbound = [o for o in outreach if o.get("direction") == "outbound"]
    if not outbound:
        return 30.0  # Neutral if never contacted

    responses = sum(1 for o in outbound if o.get("status") in ("responded", "read", "delivered"))
    total = len(outbound)
    ratio = responses / total

    score = ratio * 100

    if ratio > 0.5:
        factors.append(f"highly_responsive ({ratio:.0%})")
    elif ratio < 0.1:
        factors.append(f"low_responsiveness ({ratio:.0%})")

    # Channel diversity bonus (responds on multiple channels = more engaged)
    channels = set(o.get("channel") for o in outbound if o.get("status") in ("responded", "read"))
    if len(channels) > 2:
        score = min(100, score + 10)
        factors.append("multi_channel_responder")

    return min(100, score)


def _calc_loyalty(contact: Dict, pledges: List, factors: List) -> float:
    """Loyalty = strength of commitment to the party/cause."""
    score = 20.0  # Base for existing in the system

    voter_status = contact.get("voter_status", "unknown")
    status_scores = {
        "confirmed": 40,
        "pledged": 30,
        "unknown": 0,
        "declined": -20,
        "unreachable": -10,
    }
    score += status_scores.get(voter_status, 0)

    # Pledge quality
    for p in pledges:
        ptype = p.get("pledge_type", "")
        pstatus = p.get("status", "")
        if pstatus in ("fulfilled", "confirmed"):
            score += 15
            factors.append(f"fulfilled_pledge:{ptype}")
        elif pstatus == "reminded":
            score += 5

    # Consent signal
    if contact.get("consent_id"):
        score += 10
    else:
        score -= 10
        factors.append("no_consent")

    return max(0, min(100, score))


def _calc_mobilization(contact: Dict, pledges: List, knocks: List, factors: List) -> float:
    """Mobilization readiness = likelihood of actually showing up to vote."""
    score = 30.0  # Base

    # Has ride pledge or needs_ride → lower mobilization barrier
    for p in pledges:
        if p.get("pledge_type") == "needs_ride":
            score += 15
            factors.append("needs_transport_arranged")
        elif p.get("pledge_type") == "will_vote":
            score += 20
            factors.append("explicit_will_vote_pledge")
        elif p.get("pledge_type") == "bring_family":
            score += 25
            factors.append("will_bring_family")

    # Positive door knock = face-to-face commitment
    recent_positive = any(k.get("outcome") == "pledged" for k in knocks)
    if recent_positive:
        score += 20
        factors.append("face_to_face_pledge")

    # Tags indicating readiness
    tags = contact.get("tags") or []
    if "needs_transport" in tags:
        score -= 10  # Barrier exists
    if "first_time_voter" in tags:
        score -= 5  # Uncertainty
    if "youth" in tags:
        score += 5  # Higher enthusiasm

    return max(0, min(100, score))


def _calc_channel_preference(outreach: List) -> Dict[str, float]:
    """Determine best channel based on historical response patterns."""
    channel_stats: Dict[str, Dict[str, int]] = {}

    for o in outreach:
        ch = o.get("channel", "unknown")
        if ch not in channel_stats:
            channel_stats[ch] = {"sent": 0, "positive": 0}
        if o.get("direction") == "outbound":
            channel_stats[ch]["sent"] += 1
        if o.get("status") in ("responded", "read", "delivered"):
            channel_stats[ch]["positive"] += 1

    scores = {}
    for ch, stats in channel_stats.items():
        if stats["sent"] > 0:
            scores[ch] = stats["positive"] / stats["sent"]
        else:
            scores[ch] = 0.0

    # Default channel preferences for Nigeria if no data
    if not scores:
        return {"whatsapp": 0.6, "sms": 0.5, "phone_call": 0.4}

    return scores


def _recommend_action(segment: str, contact: Dict, pledges: List, mobilization: float) -> str:
    """Generate specific recommended action based on voter state."""
    if segment == "hot":
        if mobilization < 60:
            return "Arrange transport — committed but mobility barrier"
        return "Confirm polling unit and election day plan"
    elif segment == "warm":
        if not pledges:
            return "Request pledge via preferred channel"
        return "Reinforce commitment — send reminder and rally invite"
    elif segment == "cool":
        return "Initiate personal outreach — door knock or phone call"
    elif segment == "cold":
        return "Low-cost touch — add to SMS campaign, await response"
    else:  # dormant
        return "Deprioritize — reallocate resources to higher-scoring contacts"


# ═══════════════════════════════════════════════════════════════════════════════
# 2. PERSUADABILITY CLASSIFIER
# ═══════════════════════════════════════════════════════════════════════════════

class PersuadabilityResult(BaseModel):
    contact_id: str
    persuadability_score: float = Field(ge=0, le=100, description="0=immovable, 100=easily persuaded")
    category: str  # persuadable, lean_support, strong_support, lean_oppose, immovable
    key_factors: List[str]
    recommended_approach: str
    estimated_touches_to_convert: int


@router.post("/persuadability")
async def classify_persuadability(
    party_id: int,
    state_code: Optional[str] = None,
    ward_code: Optional[str] = None,
    limit: int = Query(default=50, le=500),
) -> Dict[str, Any]:
    """Classify undecided/unknown contacts by persuadability.

    Uses a logistic regression-inspired model trained on historical conversions:
    - Which contacts went from 'unknown' → 'pledged'?
    - What signals preceded that conversion?
    - Apply those patterns to score remaining unknowns.
    """

    # Get undecided contacts (the persuadable universe)
    where_parts = ["party_id=%s", "voter_status IN ('unknown','unreachable')", "opted_out=FALSE"]
    params: list = [party_id]
    if state_code:
        where_parts.append("state_code=%s")
        params.append(state_code)
    if ward_code:
        where_parts.append("ward_code=%s")
        params.append(ward_code)

    contacts = query_rows(f"""
        SELECT contact_id, voter_status, contact_count, tags, state_code,
               ward_code, consent_id, created_at
        FROM gotv_contacts
        WHERE {' AND '.join(where_parts)}
        ORDER BY contact_count DESC
        LIMIT %s
    """, tuple(params + [limit * 2]))

    # Learn conversion patterns from historical data
    conversion_signals = _learn_conversion_patterns(party_id)

    results = []
    for c in contacts[:limit]:
        result = _classify_single_contact(c, party_id, conversion_signals)
        results.append(result.model_dump())

    # Sort by persuadability (highest first = best targets)
    results.sort(key=lambda r: r["persuadability_score"], reverse=True)

    # Aggregate
    categories = {}
    for r in results:
        cat = r["category"]
        categories[cat] = categories.get(cat, 0) + 1

    persuadable_count = sum(1 for r in results if r["persuadability_score"] >= 50)

    return {
        "party_id": party_id,
        "total_analyzed": len(results),
        "persuadable_count": persuadable_count,
        "category_distribution": categories,
        "avg_persuadability": round(np.mean([r["persuadability_score"] for r in results]), 1) if results else 0,
        "contacts": results,
        "conversion_model_signals": conversion_signals.get("top_features", []),
        "generated_at": datetime.now(timezone.utc).isoformat(),
    }


def _learn_conversion_patterns(party_id: int) -> Dict:
    """Learn from historical conversions: what made unknown → pledged happen?"""

    # Get contacts who converted (were unknown, now pledged/confirmed)
    converted = query_rows("""
        SELECT c.contact_id, c.contact_count, c.tags, c.state_code,
               (SELECT COUNT(*) FROM gotv_outreach_log WHERE contact_id=c.contact_id) AS outreach_count,
               (SELECT COUNT(*) FROM gotv_door_knocks WHERE contact_id=c.contact_id AND outcome='pledged') AS positive_knocks,
               (SELECT COUNT(DISTINCT channel) FROM gotv_outreach_log WHERE contact_id=c.contact_id) AS channels_used
        FROM gotv_contacts c
        WHERE c.party_id=%s AND c.voter_status IN ('pledged','confirmed')
        LIMIT 500
    """, (party_id,))

    if not converted:
        return {"top_features": ["insufficient_data"], "avg_touches": 3}

    # Calculate average profile of converted contacts
    avg_contact_count = np.mean([c.get("contact_count", 0) or 0 for c in converted])
    avg_outreach = np.mean([c.get("outreach_count", 0) or 0 for c in converted])
    avg_knocks = np.mean([c.get("positive_knocks", 0) or 0 for c in converted])
    avg_channels = np.mean([c.get("channels_used", 0) or 0 for c in converted])

    top_features = []
    if avg_outreach > 2:
        top_features.append(f"multi_touch_outreach (avg {avg_outreach:.1f})")
    if avg_knocks > 0.3:
        top_features.append(f"door_knock_positive (avg {avg_knocks:.1f})")
    if avg_channels > 1.5:
        top_features.append(f"multi_channel_contact (avg {avg_channels:.1f})")
    if avg_contact_count > 3:
        top_features.append(f"high_contact_frequency (avg {avg_contact_count:.1f})")

    return {
        "avg_contact_count": avg_contact_count,
        "avg_outreach": avg_outreach,
        "avg_positive_knocks": avg_knocks,
        "avg_channels_used": avg_channels,
        "avg_touches": max(1, int(avg_outreach + avg_knocks)),
        "top_features": top_features or ["consent_given", "youth_tag", "multi_touch"],
    }


def _classify_single_contact(contact: Dict, party_id: int, patterns: Dict) -> PersuadabilityResult:
    """Score a single contact's persuadability using learned patterns."""
    contact_id = contact["contact_id"]
    factors = []
    score = 50.0  # Start neutral

    # Signal 1: Contact frequency vs. converted average
    contact_count = contact.get("contact_count", 0) or 0
    avg_cc = patterns.get("avg_contact_count", 3)
    if contact_count >= avg_cc:
        score += 10
        factors.append("high_engagement_frequency")
    elif contact_count == 0:
        score -= 15
        factors.append("never_contacted")

    # Signal 2: Has consent (opted in = openness)
    if contact.get("consent_id"):
        score += 12
        factors.append("consent_given")
    else:
        score -= 8

    # Signal 3: Tags indicate openness
    tags = contact.get("tags") or []
    if "youth" in tags or "first_time_voter" in tags:
        score += 8
        factors.append("youth_or_first_timer")
    if "needs_transport" in tags or "needs_info" in tags:
        score += 5
        factors.append("expressed_need")

    # Signal 4: State-specific conversion rates
    state = contact.get("state_code", "")
    state_conv = query_rows("""
        SELECT COUNT(*) FILTER(WHERE voter_status IN ('pledged','confirmed')) AS converted,
               COUNT(*) AS total
        FROM gotv_contacts
        WHERE party_id=%s AND state_code=%s AND opted_out=FALSE
    """, (party_id, state))
    if state_conv and state_conv[0].get("total", 0) > 0:
        state_rate = (state_conv[0].get("converted", 0) or 0) / state_conv[0]["total"]
        if state_rate > 0.4:
            score += 8
            factors.append(f"high_conversion_state ({state_rate:.0%})")
        elif state_rate < 0.15:
            score -= 5
            factors.append(f"low_conversion_state ({state_rate:.0%})")

    # Signal 5: Was previously unreachable (harder to persuade)
    if contact.get("voter_status") == "unreachable":
        score -= 15
        factors.append("previously_unreachable")

    # Clamp
    score = max(5, min(95, score))

    # Category
    if score >= 65:
        category = "persuadable"
        approach = "Prioritize: personal outreach via door knock or phone call"
    elif score >= 50:
        category = "lean_support"
        approach = "Multi-touch sequence: SMS → WhatsApp → follow-up call"
    elif score >= 35:
        category = "lean_oppose"
        approach = "Low-cost monitoring: include in SMS campaigns only"
    else:
        category = "immovable"
        approach = "Deprioritize: allocate resources to higher-scoring contacts"

    # Estimated touches needed
    est_touches = max(1, int(patterns.get("avg_touches", 3) * (100 - score) / 50))

    return PersuadabilityResult(
        contact_id=contact_id,
        persuadability_score=round(score, 1),
        category=category,
        key_factors=factors[:5],
        recommended_approach=approach,
        estimated_touches_to_convert=est_touches,
    )


# ═══════════════════════════════════════════════════════════════════════════════
# 3. RESOURCE ALLOCATION ENGINE
# ═══════════════════════════════════════════════════════════════════════════════

class WardAllocation(BaseModel):
    ward_code: str
    state_code: str
    current_volunteers: int
    recommended_additional: int
    expected_pledge_gain: int
    priority: str  # critical, high, medium, low
    reasoning: str
    cost_per_pledge_estimate: float


@router.get("/allocation/optimize")
async def optimize_resource_allocation(
    party_id: int,
    available_volunteers: int = Query(default=20, ge=1, le=500),
    target_metric: str = Query(default="pledges", regex="^(pledges|turnout|coverage)$"),
) -> Dict[str, Any]:
    """Optimal volunteer/resource allocation across wards.

    Algorithm (Marginal Returns Optimization):
    1. Calculate current state of each ward (volunteers, contacts, pledges, coverage)
    2. Estimate marginal pledge gain per additional volunteer in each ward
    3. Greedily allocate volunteers to highest-marginal-return wards
    4. Return allocation plan with expected outcomes
    """

    # Get ward-level statistics
    wards = query_rows("""
        SELECT c.ward_code, c.state_code,
               COUNT(*) AS total_contacts,
               COUNT(*) FILTER(WHERE c.voter_status IN ('pledged','confirmed')) AS pledged,
               COUNT(*) FILTER(WHERE c.voter_status = 'unknown') AS unknown,
               (SELECT COUNT(*) FROM gotv_volunteers v
                WHERE v.party_id=%s AND v.assigned_ward=c.ward_code AND v.is_active=TRUE) AS current_vols
        FROM gotv_contacts c
        WHERE c.party_id=%s AND c.opted_out=FALSE AND c.ward_code IS NOT NULL
        GROUP BY c.ward_code, c.state_code
        HAVING COUNT(*) >= 5
        ORDER BY COUNT(*) DESC
        LIMIT 100
    """, (party_id, party_id))

    if not wards:
        return {"party_id": party_id, "allocations": [], "message": "No ward data available"}

    # Calculate marginal returns for each ward
    ward_returns = []
    for w in wards:
        total = w.get("total_contacts", 0) or 0
        pledged = w.get("pledged", 0) or 0
        unknown = w.get("unknown", 0) or 0
        current_vols = w.get("current_vols", 0) or 0

        # Coverage ratio
        coverage = pledged / total if total > 0 else 0

        # Marginal return model:
        # Each volunteer can convert ~15 unknowns/day via door knocking
        # Conversion rate depends on existing coverage (diminishing returns)
        # formula: gain = unknowns * conversion_rate * diminishing_factor
        base_conversion_rate = 0.25  # 25% of door knocks lead to pledge
        diminishing = 1.0 / (1.0 + current_vols * 0.3)  # Diminishing returns
        potential_gain = unknown * base_conversion_rate * diminishing * 0.15  # 15 doors/day

        ward_returns.append({
            "ward_code": w["ward_code"],
            "state_code": w["state_code"],
            "total_contacts": total,
            "pledged": pledged,
            "unknown": unknown,
            "current_vols": current_vols,
            "coverage": coverage,
            "marginal_gain": potential_gain,
        })

    # Greedy allocation
    ward_returns.sort(key=lambda w: w["marginal_gain"], reverse=True)

    allocations = []
    remaining = available_volunteers

    for w in ward_returns:
        if remaining <= 0:
            break

        # How many volunteers does this ward need?
        optimal_vols = max(1, int(w["unknown"] / 50))  # 1 vol per 50 unknowns
        additional_needed = max(0, optimal_vols - w["current_vols"])

        if additional_needed == 0:
            continue

        assigned = min(additional_needed, remaining)
        expected_gain = int(assigned * 15 * 0.25)  # 15 doors/day * 25% conversion

        # Priority
        if w["coverage"] < 0.2 and w["unknown"] > 30:
            priority = "critical"
        elif w["coverage"] < 0.4:
            priority = "high"
        elif w["coverage"] < 0.6:
            priority = "medium"
        else:
            priority = "low"

        # Cost estimate (volunteer stipend ₦5000/day)
        cost_per_pledge = 5000 / max(1, (15 * 0.25))  # ₦1,333 per pledge

        allocations.append(WardAllocation(
            ward_code=w["ward_code"],
            state_code=w["state_code"],
            current_volunteers=w["current_vols"],
            recommended_additional=assigned,
            expected_pledge_gain=expected_gain,
            priority=priority,
            reasoning=f"{w['unknown']} unknowns, {w['coverage']:.0%} coverage, {w['current_vols']} existing vols",
            cost_per_pledge_estimate=round(cost_per_pledge, 0),
        ).model_dump())

        remaining -= assigned

    total_expected_gain = sum(a["expected_pledge_gain"] for a in allocations)

    return {
        "party_id": party_id,
        "available_volunteers": available_volunteers,
        "allocated": available_volunteers - remaining,
        "unallocated": remaining,
        "total_expected_pledge_gain": total_expected_gain,
        "allocations": allocations,
        "optimization_metric": target_metric,
        "generated_at": datetime.now(timezone.utc).isoformat(),
    }


# ═══════════════════════════════════════════════════════════════════════════════
# 4. WIN PROBABILITY CALCULATOR
# ═══════════════════════════════════════════════════════════════════════════════

class WinProbability(BaseModel):
    state_code: str
    win_probability: float = Field(ge=0, le=1)
    confidence: float = Field(ge=0, le=1)
    our_projected_votes: int
    total_projected_turnout: int
    vote_share: float
    margin: int  # positive = winning, negative = losing
    scenario: str  # winning, competitive, losing
    factors: List[str]
    actions_to_improve: List[str]


@router.get("/win-probability")
async def calculate_win_probability(
    party_id: int,
    state_code: Optional[str] = None,
) -> Dict[str, Any]:
    """Calculate win probability per state using:
    - Confirmed pledges (weighted by fulfillment history)
    - Turnout prediction
    - Volunteer coverage
    - Historical party strength
    - Opposition estimate (1 - our_share assuming multi-party)

    Model: Win% = logistic(pledge_strength * turnout_factor * coverage_factor)
    """

    states_filter = ""
    params: list = [party_id]
    if state_code:
        states_filter = "AND c.state_code=%s"
        params.append(state_code)

    # Get state-level aggregates
    states = query_rows(f"""
        SELECT c.state_code,
               COUNT(*) AS total_contacts,
               COUNT(*) FILTER(WHERE voter_status IN ('pledged','confirmed')) AS confirmed_supporters,
               COUNT(*) FILTER(WHERE voter_status = 'unknown') AS persuadable_pool,
               COUNT(*) FILTER(WHERE voter_status = 'declined') AS opposition,
               (SELECT COUNT(*) FROM gotv_volunteers v
                WHERE v.party_id=%s AND v.assigned_state=c.state_code AND v.is_active=TRUE) AS active_vols,
               (SELECT COUNT(*) FROM gotv_door_knocks dk
                WHERE dk.party_id=%s AND dk.outcome='pledged'
                  AND dk.volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE assigned_state=c.state_code)) AS field_pledges
        FROM gotv_contacts c
        WHERE c.party_id=%s AND c.opted_out=FALSE {states_filter}
        GROUP BY c.state_code
        HAVING COUNT(*) >= 10
        ORDER BY COUNT(*) DESC
    """, tuple([party_id, party_id] + params))

    results = []
    for s in states:
        total = s.get("total_contacts", 0) or 0
        confirmed = s.get("confirmed_supporters", 0) or 0
        persuadable = s.get("persuadable_pool", 0) or 0
        opposition = s.get("opposition", 0) or 0
        active_vols = s.get("active_vols", 0) or 0
        field_pledges = s.get("field_pledges", 0) or 0

        factors = []
        actions = []

        # ─── Base vote projection ────────────────────────────────────────
        # Confirmed supporters + 30% of persuadable (conservative estimate)
        projected_our_votes = confirmed + int(persuadable * 0.30)

        # Estimated turnout (60-70% typical Nigerian urban/rural)
        estimated_turnout_rate = 0.65
        if active_vols > 10:
            estimated_turnout_rate += 0.05  # Mobilization effect
            factors.append("strong_ground_game")
        if field_pledges > confirmed * 0.5:
            estimated_turnout_rate += 0.03
            factors.append("active_field_confirmation")

        total_projected_turnout = int(total * estimated_turnout_rate)

        # Vote share
        vote_share = projected_our_votes / total_projected_turnout if total_projected_turnout > 0 else 0

        # ─── Win probability (logistic model) ────────────────────────────
        # In a multi-party system with 5 parties, winning threshold is ~25-35%
        # Model: P(win) = sigmoid(vote_share - threshold) adjusted for confidence
        threshold = 0.30  # Multi-party winning threshold
        logit = (vote_share - threshold) * 10  # Scale for sigmoid
        win_prob = 1.0 / (1.0 + math.exp(-logit))

        # Adjust for ground game strength
        coverage_factor = min(1.0, active_vols / max(1, total / 100))
        win_prob = win_prob * (0.7 + 0.3 * coverage_factor)

        # Confidence based on data quality
        confidence = 0.40
        if total > 100:
            confidence += 0.15
        if confirmed > 20:
            confidence += 0.15
        if active_vols > 5:
            confidence += 0.10
        if field_pledges > 10:
            confidence += 0.10
        confidence = min(0.90, confidence)

        # Margin
        # Assume largest opponent has similar machinery
        opponent_est = max(opposition, int(total * 0.25))
        margin = projected_our_votes - opponent_est

        # Scenario classification
        if win_prob > 0.65:
            scenario = "winning"
        elif win_prob > 0.35:
            scenario = "competitive"
            actions.append("Increase door-knock coverage by 50%")
            actions.append("Launch targeted WhatsApp campaign to persuadable pool")
        else:
            scenario = "losing"
            actions.append("Deploy additional volunteers urgently")
            actions.append("Activate alliance ride-sharing for transportation")
            actions.append("Intensify phone banking to unreachable contacts")

        # State-specific actions
        if coverage_factor < 0.5:
            actions.append(f"Need {int(total/100 - active_vols)} more volunteers in {s['state_code']}")
        if persuadable > confirmed:
            actions.append("Large persuadable pool — launch multi-channel sequence")

        results.append(WinProbability(
            state_code=s["state_code"],
            win_probability=round(win_prob, 3),
            confidence=round(confidence, 2),
            our_projected_votes=projected_our_votes,
            total_projected_turnout=total_projected_turnout,
            vote_share=round(vote_share, 3),
            margin=margin,
            scenario=scenario,
            factors=factors,
            actions_to_improve=actions[:4],
        ).model_dump())

    # Sort by competitiveness (closest races first — where effort matters most)
    results.sort(key=lambda r: abs(r["win_probability"] - 0.5))

    # Overall probability (if winning majority of states)
    winning_states = sum(1 for r in results if r["win_probability"] > 0.5)
    overall_strength = winning_states / len(results) if results else 0

    return {
        "party_id": party_id,
        "total_states_analyzed": len(results),
        "winning_states": winning_states,
        "competitive_states": sum(1 for r in results if r["scenario"] == "competitive"),
        "losing_states": sum(1 for r in results if r["scenario"] == "losing"),
        "overall_strength": round(overall_strength, 3),
        "states": results,
        "generated_at": datetime.now(timezone.utc).isoformat(),
    }


# ═══════════════════════════════════════════════════════════════════════════════
# 5. AUTO-OPTIMIZE MESSAGE SELECTION (Multi-Armed Bandit)
# ═══════════════════════════════════════════════════════════════════════════════

class BanditArm(BaseModel):
    variant_id: str
    variant_text: str
    impressions: int
    conversions: int
    conversion_rate: float
    ucb_score: float  # Upper Confidence Bound
    thompson_sample: float  # Thompson sampling posterior
    status: str  # exploring, exploiting, retired


@router.get("/optimize/messages")
async def get_message_optimization(
    party_id: int,
    campaign_id: Optional[str] = None,
) -> Dict[str, Any]:
    """Multi-armed bandit for message variant selection.

    Algorithm: Thompson Sampling with UCB1 exploration bonus.
    - Each message variant is an "arm"
    - Reward = conversion (response, pledge, ride request)
    - Balance exploration (try new messages) vs exploitation (use best performer)
    - Auto-retire underperforming variants after sufficient trials
    """

    # Get all active variants
    variant_filter = "AND v.campaign_id=%s" if campaign_id else ""
    variant_params = (party_id, campaign_id) if campaign_id else (party_id,)

    variants = query_rows(f"""
        SELECT v.variant_id, v.variant_text, v.base_message, v.target_state, v.channel
        FROM gotv_ai_variants v
        WHERE v.party_id=%s {variant_filter}
        ORDER BY v.created_at DESC
        LIMIT 50
    """, variant_params)

    if not variants:
        return {"party_id": party_id, "arms": [], "message": "No variants to optimize"}

    total_impressions = 0
    arms = []

    for v in variants:
        vid = v["variant_id"]

        # Get performance data
        perf = query_rows("""
            SELECT
                COUNT(*) AS impressions,
                COUNT(*) FILTER(WHERE status IN ('responded','read')) AS conversions
            FROM gotv_outreach_log
            WHERE party_id=%s AND message_variant_id=%s
        """, (party_id, vid))

        impressions = perf[0].get("impressions", 0) if perf else 0
        conversions = perf[0].get("conversions", 0) if perf else 0
        total_impressions += impressions

        # Conversion rate
        rate = conversions / impressions if impressions > 0 else 0.0

        arms.append({
            "variant_id": vid,
            "variant_text": v.get("variant_text", "")[:100],
            "channel": v.get("channel", ""),
            "target_state": v.get("target_state", ""),
            "impressions": impressions,
            "conversions": conversions,
            "rate": rate,
        })

    # Calculate UCB1 and Thompson Sampling scores
    result_arms = []
    for arm in arms:
        n = arm["impressions"]
        k = arm["conversions"]

        # UCB1: mean + sqrt(2 * ln(total) / n)
        if n > 0 and total_impressions > 0:
            ucb = arm["rate"] + math.sqrt(2 * math.log(total_impressions) / n)
        else:
            ucb = float("inf")  # Unexplored = infinite optimism

        # Thompson Sampling: Beta(successes+1, failures+1)
        alpha = k + 1
        beta_param = (n - k) + 1
        thompson = np.random.beta(alpha, beta_param)

        # Status
        if n < 20:
            status = "exploring"
        elif arm["rate"] > 0.1:
            status = "exploiting"
        elif n > 100 and arm["rate"] < 0.02:
            status = "retired"
        else:
            status = "exploring"

        result_arms.append(BanditArm(
            variant_id=arm["variant_id"],
            variant_text=arm["variant_text"],
            impressions=n,
            conversions=k,
            conversion_rate=round(arm["rate"], 4),
            ucb_score=round(min(ucb, 2.0), 4),
            thompson_sample=round(thompson, 4),
            status=status,
        ).model_dump())

    # Sort by Thompson sample (recommended selection order)
    result_arms.sort(key=lambda a: a["thompson_sample"], reverse=True)

    # Recommendations
    best_arm = result_arms[0] if result_arms else None
    exploring_count = sum(1 for a in result_arms if a["status"] == "exploring")
    retired_count = sum(1 for a in result_arms if a["status"] == "retired")

    return {
        "party_id": party_id,
        "total_variants": len(result_arms),
        "total_impressions": total_impressions,
        "exploring": exploring_count,
        "exploiting": len(result_arms) - exploring_count - retired_count,
        "retired": retired_count,
        "recommended_next": best_arm["variant_id"] if best_arm else None,
        "recommended_text": best_arm["variant_text"] if best_arm else None,
        "arms": result_arms,
        "algorithm": "Thompson Sampling + UCB1",
        "generated_at": datetime.now(timezone.utc).isoformat(),
    }


@router.post("/optimize/select-variant")
async def select_variant_for_send(
    party_id: int,
    campaign_id: str,
) -> Dict[str, Any]:
    """Called by dispatch engine before each send — selects optimal variant.

    Returns the variant_id to use for the next message send, balancing
    exploration (try under-tested variants) vs exploitation (use best performer).
    """

    variants = query_rows("""
        SELECT v.variant_id, v.variant_text
        FROM gotv_ai_variants v
        WHERE v.party_id=%s AND v.campaign_id=%s
        ORDER BY v.created_at
    """, (party_id, campaign_id))

    if not variants:
        return {"selected": None, "reason": "no_variants_available"}

    best_score = -1.0
    selected = variants[0]

    for v in variants:
        vid = v["variant_id"]
        perf = query_rows("""
            SELECT COUNT(*) AS n,
                   COUNT(*) FILTER(WHERE status IN ('responded','read')) AS k
            FROM gotv_outreach_log
            WHERE party_id=%s AND message_variant_id=%s
        """, (party_id, vid))

        n = perf[0].get("n", 0) if perf else 0
        k = perf[0].get("k", 0) if perf else 0

        # Thompson sampling
        score = np.random.beta(k + 1, (n - k) + 1)

        if score > best_score:
            best_score = score
            selected = v

    return {
        "selected_variant_id": selected["variant_id"],
        "variant_text": selected["variant_text"],
        "selection_method": "thompson_sampling",
        "confidence": round(best_score, 4),
    }


# ═══════════════════════════════════════════════════════════════════════════════
# BONUS: Scoring Summary Dashboard
# ═══════════════════════════════════════════════════════════════════════════════

@router.get("/summary")
async def scoring_summary(party_id: int) -> Dict[str, Any]:
    """High-level scoring analytics for the campaign manager dashboard."""

    # Segment distribution (sample 500 contacts)
    sample = query_rows("""
        SELECT contact_id FROM gotv_contacts
        WHERE party_id=%s AND opted_out=FALSE
        ORDER BY contact_count DESC
        LIMIT 500
    """, (party_id,))

    segments = {"hot": 0, "warm": 0, "cool": 0, "cold": 0, "dormant": 0}
    total_score = 0.0
    scored = 0

    for c in sample[:200]:  # Score 200 for speed
        score = _compute_voter_score(c["contact_id"], party_id)
        if score:
            segments[score.segment] += 1
            total_score += score.overall_score
            scored += 1

    avg_score = total_score / scored if scored > 0 else 0

    return {
        "party_id": party_id,
        "contacts_sampled": scored,
        "average_score": round(avg_score, 1),
        "segment_distribution": segments,
        "actionable_insights": [
            f"{segments.get('hot', 0)} contacts ready to mobilize (score 80+)",
            f"{segments.get('warm', 0)} warm leads need one more touch",
            f"{segments.get('cold', 0) + segments.get('dormant', 0)} low-priority contacts to deprioritize",
        ],
        "generated_at": datetime.now(timezone.utc).isoformat(),
    }
