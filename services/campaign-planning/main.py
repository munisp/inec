"""
INEC Candidate Campaign Planning Service — Production-Complete v2.0
===================================================================
10 Next-Generation Innovations:
  1.  AI Speech Writer — rally speeches, manifestos, press releases (LLM-backed)
  2.  Micro-targeting heat maps — ward-level voter density & swing analysis
  3.  Opponent vulnerability scanner — public record & strength analysis
  4.  Fundraising optimizer — donor segmentation & ask-amount prediction
  5.  Canvassing route optimizer — TSP nearest-neighbour optimal routing
  6.  Real-time debate performance tracker — per-statement sentiment scoring
  7.  Volunteer network graph — social-network reach analysis
  8.  Policy resonance analyzer — maps policies to zone demographic priorities
  9.  Media buy optimizer — GRP/reach/frequency optimisation across channels
  10. Election day war room dashboard — live command-centre aggregation
"""
from __future__ import annotations

import asyncio
import hmac
import json
import math
import os
import time
import uuid
from collections import defaultdict
from typing import Dict, List, Optional

import asyncpg
import httpx
import uvicorn
from fastapi import FastAPI, HTTPException, Request, WebSocket, WebSocketDisconnect
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field
from slowapi import Limiter
from slowapi.errors import RateLimitExceeded
from slowapi.util import get_remote_address
import structlog

structlog.configure(
    processors=[structlog.processors.TimeStamper(fmt="iso"), structlog.processors.JSONRenderer()]
)
log = structlog.get_logger()

OPENAI_KEY = os.getenv("OPENAI_API_KEY", "").strip()
OPENAI_BASE = os.getenv("OPENAI_API_BASE", "").strip().rstrip("/")
OPENAI_MODEL = os.getenv("OPENAI_MODEL", "").strip()
INEC_API = os.getenv("INEC_API_URL", "").strip().rstrip("/")
CORS_ORIGINS = [origin.strip() for origin in os.getenv("CORS_ORIGINS", "").split(",") if origin.strip()]
# SECURITY: service API key. When unset, the service FAILS CLOSED (503 on all
# non-health routes) — previously every route was unauthenticated, including
# the paid-LLM /speech endpoint (cost-abuse vector).
# KEY ROTATION: the variable accepts a comma-separated list of keys; any
# constant-time match authenticates, so operators can rotate without downtime.
CAMPAIGN_API_KEYS: List[str] = [
    k.strip() for k in os.getenv("CAMPAIGN_PLANNING_API_KEY", "").split(",") if k.strip()
]

APP_ENV = os.getenv("APP_ENV", "development").strip().lower()
_PRODUCTION = APP_ENV == "production"

# PostgreSQL persistence for campaign plans / war-room state. Optional in
# development (in-memory with a loud warning), MANDATORY in production.
DATABASE_URL = os.getenv("DATABASE_URL", "").strip()
_pg_pool: Optional[asyncpg.Pool] = None

# SECURITY: in production the interactive docs/OpenAPI schema are disabled —
# they leak the full API surface to unauthenticated callers.
app = FastAPI(
    title="INEC Campaign Planning Service",
    version="2.1.0",
    docs_url=None if _PRODUCTION else "/docs",
    redoc_url=None if _PRODUCTION else "/redoc",
    openapi_url=None if _PRODUCTION else "/openapi.json",
)

# Rate limiting (429 + Retry-After on breach). /speech hits a paid LLM, so it
# is capped hardest.
limiter = Limiter(key_func=get_remote_address)
app.state.limiter = limiter


@app.exception_handler(RateLimitExceeded)
async def rate_limit_handler(request: Request, exc: RateLimitExceeded):
    return JSONResponse(
        status_code=429,
        content={"error": "rate limit exceeded", "detail": str(exc.detail)},
        headers={"Retry-After": "60"},
    )


def _key_valid(provided: str) -> bool:
    """Constant-time match against ANY configured key (comma-separated rotation)."""
    return bool(provided) and any(
        hmac.compare_digest(provided.encode(), key.encode()) for key in CAMPAIGN_API_KEYS
    )


@app.middleware("http")
async def api_key_auth_middleware(request, call_next):
    """Require the service API key on all non-health endpoints (fail closed)."""
    from fastapi.responses import JSONResponse
    public = ("/api/v1/campaign/health", "/docs", "/openapi.json", "/redoc")
    if request.url.path in public:
        return await call_next(request)
    if not CAMPAIGN_API_KEYS:
        log.error("api_key_not_configured", detail="CAMPAIGN_PLANNING_API_KEY unset")
        return JSONResponse(
            status_code=503,
            content={"error": "CAMPAIGN_PLANNING_API_KEY not configured; refusing to serve unauthenticated requests"},
        )
    auth = request.headers.get("Authorization", "")
    bearer = auth[7:] if auth.lower().startswith("bearer ") else auth
    provided = bearer or request.headers.get("x-api-key", "")
    if not _key_valid(provided):
        return JSONResponse(status_code=401, content={"error": "authentication required"})
    return await call_next(request)
app.add_middleware(
    CORSMiddleware,
    allow_origins=CORS_ORIGINS,
    allow_methods=["GET", "POST"],
    allow_headers=["Content-Type", "Authorization", "X-Request-ID"],
    allow_credentials=True,
)

# ── INEC Eligibility Requirements (1999 Constitution as amended) ──────────────
ELIGIBILITY: Dict[str, Dict] = {
    "presidential": {
        "min_age": 40, "citizenship": "Nigerian by birth", "education": "School Certificate",
        "party_membership": True, "residency_years": 10,
        "forms": ["CF001", "CF002", "CF003", "CF004"],
        "fees_ngn": 150_000_000, "nomination_fee_ngn": 100_000_000,
        "disqualifiers": ["conviction_criminal", "dual_citizenship", "mental_incapacity", "impeachment_within_7yrs"],
        "constitutional_sections": ["Section 131", "Section 137"],
    },
    "gubernatorial": {
        "min_age": 35, "citizenship": "Nigerian", "education": "School Certificate",
        "party_membership": True, "residency_years": 5,
        "forms": ["CF001", "CF002", "CF003"],
        "fees_ngn": 50_000_000, "nomination_fee_ngn": 25_000_000,
        "disqualifiers": ["conviction_criminal", "dual_citizenship", "mental_incapacity"],
        "constitutional_sections": ["Section 177", "Section 182"],
    },
    "senatorial": {
        "min_age": 35, "citizenship": "Nigerian", "education": "School Certificate",
        "party_membership": True, "residency_years": 3,
        "forms": ["CF001", "CF002"],
        "fees_ngn": 3_500_000, "nomination_fee_ngn": 2_000_000,
        "disqualifiers": ["conviction_criminal", "dual_citizenship", "undischarged_bankrupt"],
        "constitutional_sections": ["Section 65", "Section 66"],
    },
    "house": {
        "min_age": 25, "citizenship": "Nigerian", "education": "School Certificate",
        "party_membership": True, "residency_years": 2,
        "forms": ["CF001", "CF002"],
        "fees_ngn": 1_000_000, "nomination_fee_ngn": 500_000,
        "disqualifiers": ["conviction_criminal", "dual_citizenship", "undischarged_bankrupt"],
        "constitutional_sections": ["Section 65", "Section 66"],
    },
    "local": {
        "min_age": 25, "citizenship": "Nigerian", "education": "School Certificate",
        "party_membership": True, "residency_years": 1,
        "forms": ["CF001"],
        "fees_ngn": 200_000, "nomination_fee_ngn": 100_000,
        "disqualifiers": ["conviction_criminal"],
        "constitutional_sections": ["State Electoral Laws"],
    },
}

# ── Nigeria State Reference Data ──────────────────────────────────────────────
STATES = [
    {"code": "AB", "name": "Abia",       "zone": "SE", "lgas": 17, "voters": 1_200_000, "swing": 0.30},
    {"code": "AD", "name": "Adamawa",    "zone": "NE", "lgas": 21, "voters": 1_800_000, "swing": 0.40},
    {"code": "AK", "name": "Akwa Ibom",  "zone": "SS", "lgas": 31, "voters": 2_100_000, "swing": 0.25},
    {"code": "AN", "name": "Anambra",    "zone": "SE", "lgas": 21, "voters": 2_000_000, "swing": 0.20},
    {"code": "BA", "name": "Bauchi",     "zone": "NE", "lgas": 20, "voters": 2_300_000, "swing": 0.45},
    {"code": "BY", "name": "Bayelsa",    "zone": "SS", "lgas":  8, "voters":   900_000, "swing": 0.30},
    {"code": "BE", "name": "Benue",      "zone": "NC", "lgas": 23, "voters": 2_200_000, "swing": 0.40},
    {"code": "BO", "name": "Borno",      "zone": "NE", "lgas": 27, "voters": 2_500_000, "swing": 0.35},
    {"code": "CR", "name": "Cross River","zone": "SS", "lgas": 18, "voters": 1_500_000, "swing": 0.35},
    {"code": "DE", "name": "Delta",      "zone": "SS", "lgas": 25, "voters": 2_800_000, "swing": 0.30},
    {"code": "EB", "name": "Ebonyi",     "zone": "SE", "lgas": 13, "voters": 1_100_000, "swing": 0.25},
    {"code": "ED", "name": "Edo",        "zone": "SS", "lgas": 18, "voters": 2_200_000, "swing": 0.40},
    {"code": "EK", "name": "Ekiti",      "zone": "SW", "lgas": 16, "voters":   900_000, "swing": 0.45},
    {"code": "EN", "name": "Enugu",      "zone": "SE", "lgas": 17, "voters": 1_600_000, "swing": 0.20},
    {"code": "GO", "name": "Gombe",      "zone": "NE", "lgas": 11, "voters": 1_100_000, "swing": 0.40},
    {"code": "IM", "name": "Imo",        "zone": "SE", "lgas": 27, "voters": 1_800_000, "swing": 0.35},
    {"code": "JI", "name": "Jigawa",     "zone": "NW", "lgas": 27, "voters": 2_200_000, "swing": 0.30},
    {"code": "KD", "name": "Kaduna",     "zone": "NW", "lgas": 23, "voters": 3_800_000, "swing": 0.50},
    {"code": "KN", "name": "Kano",       "zone": "NW", "lgas": 44, "voters": 5_500_000, "swing": 0.40},
    {"code": "KT", "name": "Katsina",    "zone": "NW", "lgas": 34, "voters": 3_200_000, "swing": 0.35},
    {"code": "KE", "name": "Kebbi",      "zone": "NW", "lgas": 21, "voters": 1_600_000, "swing": 0.30},
    {"code": "KO", "name": "Kogi",       "zone": "NC", "lgas": 21, "voters": 1_700_000, "swing": 0.45},
    {"code": "KW", "name": "Kwara",      "zone": "NC", "lgas": 16, "voters": 1_200_000, "swing": 0.40},
    {"code": "LA", "name": "Lagos",      "zone": "SW", "lgas": 20, "voters": 7_200_000, "swing": 0.35},
    {"code": "NA", "name": "Nasarawa",   "zone": "NC", "lgas": 13, "voters": 1_100_000, "swing": 0.40},
    {"code": "NI", "name": "Niger",      "zone": "NC", "lgas": 25, "voters": 2_400_000, "swing": 0.35},
    {"code": "OG", "name": "Ogun",       "zone": "SW", "lgas": 20, "voters": 2_200_000, "swing": 0.40},
    {"code": "ON", "name": "Ondo",       "zone": "SW", "lgas": 18, "voters": 1_700_000, "swing": 0.40},
    {"code": "OS", "name": "Osun",       "zone": "SW", "lgas": 30, "voters": 1_600_000, "swing": 0.45},
    {"code": "OY", "name": "Oyo",        "zone": "SW", "lgas": 33, "voters": 3_200_000, "swing": 0.35},
    {"code": "PL", "name": "Plateau",    "zone": "NC", "lgas": 17, "voters": 2_000_000, "swing": 0.45},
    {"code": "RI", "name": "Rivers",     "zone": "SS", "lgas": 23, "voters": 3_500_000, "swing": 0.35},
    {"code": "SO", "name": "Sokoto",     "zone": "NW", "lgas": 23, "voters": 1_900_000, "swing": 0.30},
    {"code": "TA", "name": "Taraba",     "zone": "NE", "lgas": 16, "voters": 1_400_000, "swing": 0.40},
    {"code": "YO", "name": "Yobe",       "zone": "NE", "lgas": 17, "voters": 1_300_000, "swing": 0.35},
    {"code": "ZA", "name": "Zamfara",    "zone": "NW", "lgas": 14, "voters": 1_500_000, "swing": 0.30},
    {"code": "FC", "name": "FCT Abuja",  "zone": "NC", "lgas":  6, "voters": 1_200_000, "swing": 0.50},
]

ZONES = {
    "NW": {"states": ["KN","KT","KD","SO","KE","ZA","JI"], "total_voters": 19_700_000},
    "NE": {"states": ["BO","AD","GO","BA","TA","YO"],       "total_voters": 10_400_000},
    "NC": {"states": ["KO","BE","NI","PL","NA","KW","FC"],  "total_voters": 10_800_000},
    "SW": {"states": ["LA","OY","OG","OS","EK","ON"],       "total_voters": 17_800_000},
    "SE": {"states": ["AN","IM","EN","AB","EB"],            "total_voters":  7_700_000},
    "SS": {"states": ["RI","DE","AK","ED","CR","BY"],       "total_voters": 13_000_000},
}

ZONE_PRIORITIES = {
    "NW": {"security": 0.90, "agriculture": 0.80, "education": 0.70, "infrastructure": 0.60, "health": 0.50},
    "NE": {"security": 0.95, "agriculture": 0.75, "infrastructure": 0.70, "education": 0.60, "health": 0.60},
    "NC": {"agriculture": 0.85, "security": 0.80, "infrastructure": 0.75, "education": 0.65, "health": 0.60},
    "SW": {"economy": 0.90, "education": 0.85, "infrastructure": 0.80, "tech": 0.75, "health": 0.70},
    "SE": {"economy": 0.90, "education": 0.85, "security": 0.75, "infrastructure": 0.70, "health": 0.65},
    "SS": {"oil_gas": 0.90, "security": 0.85, "infrastructure": 0.80, "environment": 0.75, "health": 0.70},
}

# ── In-memory store (write-through cache over Postgres when configured) ──────
_plans: Dict[str, Dict] = {}
_war_rooms: Dict[str, Dict] = {}
_ws_clients: List[WebSocket] = []


async def _get_with_retry(url: str, *, timeout: float = 5.0, attempts: int = 3) -> httpx.Response:
    """Idempotent GET with explicit connect/read budgets and bounded backoff retry."""
    last_exc: Optional[Exception] = None
    for attempt in range(attempts):
        try:
            async with httpx.AsyncClient(timeout=httpx.Timeout(timeout, connect=3.0)) as client:
                return await client.get(url)
        except httpx.TransportError as exc:
            last_exc = exc
            if attempt < attempts - 1:
                await asyncio.sleep(0.2 * (2 ** attempt))
    raise last_exc  # type: ignore[misc]


# ── PostgreSQL persistence (durable plans & war-room state) ──────────────────
# Tables are created idempotently at startup (CREATE TABLE IF NOT EXISTS):
#   campaign_plans(plan_id PK, data JSONB, updated_at)
#   campaign_war_rooms(candidate_id PK, data JSONB, updated_at)

async def _init_state_store() -> None:
    """Create tables and reload persisted plans/war-room state into memory."""
    global _pg_pool
    _pg_pool = await asyncpg.create_pool(DATABASE_URL, min_size=1, max_size=5)
    async with _pg_pool.acquire() as conn:
        await conn.execute("""
            CREATE TABLE IF NOT EXISTS campaign_plans (
                plan_id    TEXT PRIMARY KEY,
                data       JSONB NOT NULL,
                updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
            )
        """)
        await conn.execute("""
            CREATE TABLE IF NOT EXISTS campaign_war_rooms (
                candidate_id TEXT PRIMARY KEY,
                data         JSONB NOT NULL,
                updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
            )
        """)
        plan_rows = await conn.fetch("SELECT plan_id, data FROM campaign_plans")
        war_rows = await conn.fetch("SELECT candidate_id, data FROM campaign_war_rooms")
    for row in plan_rows:
        _plans[row["plan_id"]] = json.loads(row["data"])
    for row in war_rows:
        _war_rooms[row["candidate_id"]] = json.loads(row["data"])
    log.info("state_store_loaded", plans=len(plan_rows), war_rooms=len(war_rows))


async def _persist_plan(plan_id: str, data: Dict) -> None:
    if _pg_pool is None:
        return
    async with _pg_pool.acquire() as conn:
        await conn.execute(
            """INSERT INTO campaign_plans (plan_id, data, updated_at)
               VALUES ($1, $2::jsonb, NOW())
               ON CONFLICT (plan_id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()""",
            plan_id, json.dumps(data),
        )


async def _persist_war_room(candidate_id: str, data: Dict) -> None:
    if _pg_pool is None:
        return
    async with _pg_pool.acquire() as conn:
        await conn.execute(
            """INSERT INTO campaign_war_rooms (candidate_id, data, updated_at)
               VALUES ($1, $2::jsonb, NOW())
               ON CONFLICT (candidate_id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()""",
            candidate_id, json.dumps(data),
        )


async def _load_plan_from_store(plan_id: str) -> Optional[Dict]:
    if _pg_pool is None:
        return None
    async with _pg_pool.acquire() as conn:
        row = await conn.fetchrow("SELECT data FROM campaign_plans WHERE plan_id = $1", plan_id)
    return json.loads(row["data"]) if row else None


@app.on_event("startup")
async def startup() -> None:
    # SECURITY: fail fast in production when required config is missing —
    # running unauthenticated or with non-durable state is never acceptable.
    if _PRODUCTION:
        missing = []
        if not CAMPAIGN_API_KEYS:
            missing.append("CAMPAIGN_PLANNING_API_KEY")
        if not DATABASE_URL:
            missing.append("DATABASE_URL")
        if missing:
            raise RuntimeError(
                f"APP_ENV=production requires {', '.join(missing)}; refusing to start"
            )
    if DATABASE_URL:
        await _init_state_store()
        log.info("state_persistence", backend="postgresql")
    else:
        log.warn("state_persistence_in_memory",
                 detail="DATABASE_URL unset — plans/war-rooms are IN-MEMORY ONLY; "
                        "acceptable only outside production")


def _state(code: str) -> Dict:
    return next((s for s in STATES if s["code"] == code),
                {"code": code, "name": code, "zone": "NC", "lgas": 10, "voters": 1_000_000, "swing": 0.35})


async def _broadcast(data: Dict) -> None:
    dead = []
    for ws in _ws_clients:
        try:
            await ws.send_json(data)
        except Exception:
            dead.append(ws)
    for ws in dead:
        _ws_clients.remove(ws)


# ── Computation Engines ───────────────────────────────────────────────────────

def engine_eligibility(candidate_id: int, office_type: str, state_code: str, party_code: str,
                        age: int, has_cert: Optional[bool], is_nigerian: Optional[bool],
                        criminal: Optional[bool], dual: Optional[bool],
                        party_years: Optional[int]) -> Dict:
    # INTEGRITY: unknown office types are rejected by the endpoint (400), never
    # silently remapped to "house" requirements.
    req = ELIGIBILITY[office_type]
    passed, issues, unassessed = [], [], []

    if age >= req["min_age"]:
        passed.append(f"Age {age} meets minimum of {req['min_age']}")
    else:
        issues.append(f"Age {age} below minimum {req['min_age']} for {office_type}")

    # INTEGRITY: caller-asserted facts are never defaulted to the favourable
    # answer. Unknown facts are reported as "not_assessed" and prevent a
    # positive eligibility verdict.
    if is_nigerian is None:
        unassessed.append("nigerian_citizenship")
    elif is_nigerian:
        passed.append("Nigerian citizenship confirmed")
    else:
        issues.append("Must be a Nigerian citizen")

    if office_type == "presidential" and is_nigerian is False:
        issues.append("Presidential candidates must be Nigerian by birth (Section 131(a))")

    if has_cert is None:
        unassessed.append("school_certificate")
    elif has_cert:
        passed.append("School Certificate requirement satisfied")
    else:
        issues.append("WAEC/NECO/GCE School Certificate required")

    if criminal is None:
        unassessed.append("criminal_record")
    elif criminal:
        issues.append("Criminal conviction disqualifies under Section 66(1)(d)")
    else:
        passed.append("No criminal conviction on record")

    if dual is None:
        unassessed.append("dual_citizenship")
    elif dual:
        issues.append("Dual citizenship disqualifies under Section 66(1)(a)")
    else:
        passed.append("No dual-citizenship conflict")

    if party_years is None:
        unassessed.append("party_membership")
    elif party_years >= 1:
        passed.append(f"{party_years} year(s) party membership confirmed")
    else:
        issues.append("Must be a registered member of a political party")

    eligible = len(issues) == 0 and len(unassessed) == 0
    score = round(len(passed) / max(len(passed) + len(issues), 1) * 100, 1)

    now = time.time()
    election_est = now + 365 * 86400
    filing_dl    = election_est - 180 * 86400
    primary_est  = election_est - 270 * 86400

    return {
        "candidate_id": candidate_id,
        "office_type": office_type,
        "state_code": state_code,
        "party_code": party_code,
        "eligible": eligible,
        "assessment": "complete" if not unassessed else "partial",
        "unassessed_facts": unassessed,
        "compliance_score_pct": score,
        "requirements_met": passed,
        "disqualifying_issues": issues,
        "constitutional_sections": req["constitutional_sections"],
        "inec_forms_required": req["forms"],
        "filing_fees_ngn": req["fees_ngn"],
        "nomination_fee_ngn": req["nomination_fee_ngn"],
        "campaign_timeline": {
            "party_primary_est": time.strftime("%Y-%m-%d", time.localtime(primary_est)),
            "filing_deadline_est": time.strftime("%Y-%m-%d", time.localtime(filing_dl)),
            "election_date_est": time.strftime("%Y-%m-%d", time.localtime(election_est)),
            "days_to_election": 365,
            # INTEGRITY: dates are placeholders (now+365d), not INEC-published
            # dates. Labeled so callers never treat them as authoritative.
            "illustrative_only": True,
            "note": "Illustrative timeline computed as now+365d; confirm actual dates with INEC",
        },
        "next_steps": (
            [
                "Obtain INEC form CF001 from your state INEC office",
                f"Pay filing fee of ₦{req['fees_ngn']:,} to INEC-designated bank",
                "Submit party nomination forms to your party secretariat",
                "Obtain police clearance certificate",
                "Obtain certified copies of educational certificates",
                "Prepare sworn affidavit of personal particulars",
            ]
            if eligible
            else ["Resolve all disqualifying issues before proceeding"]
        ),
        "checked_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


DISABLED_DATA_FEATURES = {
    "campaign_plan": "authoritative polling, survey, media, donor, volunteer, and canvassing data",
    "voter_targeting": "authoritative polling and voter-segmentation data",
    "budget_allocation": "approved campaign cost, reach, and conversion data",
    "campaign_schedule": "authoritative event, venue, and constituency data",
    "sentiment_analysis": "authoritative social-listening and media-ingestion data",
    "opponent_analysis": "authoritative public-record and polling data",
    "canvassing_routes": "authoritative polling-unit geography and canvassing data",
    "fundraising": "authoritative donor CRM and contribution data",
    "media_buy": "approved media inventory, pricing, and reach data",
    "policy_resonance": "authoritative constituency research and policy-response data",
    "war_room": "authoritative live incident, agent, and results data",
    "debate_tracker": "approved NLP model and verified debate transcript data",
    "volunteer_network": "authoritative volunteer CRM and relationship data",
    "stakeholder_recommendations": "authoritative stakeholder registry and engagement data",
}


def campaign_data_unavailable(feature: str):
    required = DISABLED_DATA_FEATURES[feature]
    raise HTTPException(
        status_code=503,
        detail={
            "status": "disabled",
            "feature": feature,
            "reason": "authoritative data integration is not configured",
            "required_data": required,
        },
    )


def engine_targeting(candidate_id: str, state_code: str, party_code: str, office_type: str, target_votes: int) -> Dict:
    return campaign_data_unavailable("voter_targeting")


def engine_budget(candidate_id: str, election_id: str, total: int, state_code: str, office_type: str) -> Dict:
    return campaign_data_unavailable("budget_allocation")


def engine_schedule(candidate_id: str, election_id: str, state_code: str, office_type: str) -> Dict:
    return campaign_data_unavailable("campaign_schedule")


def engine_sentiment(candidate_id: str, period: str) -> Dict:
    return campaign_data_unavailable("sentiment_analysis")


def engine_opponents(candidate_id: str, state_code: str, office_type: str) -> Dict:
    return campaign_data_unavailable("opponent_analysis")


def engine_canvassing(candidate_id: str, state_code: str, lga_codes: List[str]) -> Dict:
    return campaign_data_unavailable("canvassing_routes")


def engine_fundraising(candidate_id: str, office_type: str, target: int) -> Dict:
    return campaign_data_unavailable("fundraising")


def engine_media_buy(candidate_id: str, state_code: str, budget: int, office_type: str) -> Dict:
    return campaign_data_unavailable("media_buy")


def engine_policy_resonance(candidate_id: str, state_code: str, policies: List[str]) -> Dict:
    return campaign_data_unavailable("policy_resonance")


def engine_war_room(candidate_id: str, election_id: str) -> Dict:
    return campaign_data_unavailable("war_room")


async def engine_speech(speech_type: str, name: str, office: str,
                         state_code: str, policies: List[str], lang: str) -> str:
    st = _state(state_code)
    state_name = st["name"]
    lang_map = {"en": "English", "ha": "Hausa", "yo": "Yoruba", "ig": "Igbo"}
    lang_name = lang_map.get(lang, "English")

    prompts = {
        "rally":          f"Write a 3-paragraph energetic campaign rally speech for {name} running for {office} in {state_name}, Nigeria. Key policies: {', '.join(policies)}. Language: {lang_name}. Tone: hopeful, patriotic.",
        "manifesto":      f"Write a concise 5-point manifesto for {name} running for {office} in {state_name}. Policies: {', '.join(policies)}. Language: {lang_name}.",
        "press_release":  f"Write a professional press release announcing {name}'s candidacy for {office} in {state_name}. Key policies: {', '.join(policies)}.",
        "debate_opening": f"Write a 2-minute debate opening statement for {name} running for {office} in {state_name}. Policies: {', '.join(policies)}.",
        "victory":        f"Write a gracious victory speech for {name} who just won the {office} election in {state_name}.",
        "concession":     f"Write a dignified concession speech for {name} after losing the {office} election in {state_name}.",
        "policy_brief":   f"Write a 300-word policy brief on {', '.join(policies[:2])} for {name}'s {office} campaign in {state_name}.",
    }
    prompt = prompts.get(speech_type, prompts["rally"])

    if not OPENAI_KEY or not OPENAI_BASE or not OPENAI_MODEL:
        raise HTTPException(status_code=503, detail="configured campaign language model is unavailable")
    try:
        async with httpx.AsyncClient(timeout=20.0) as client:
            response = await client.post(
                f"{OPENAI_BASE}/chat/completions",
                headers={"Authorization": f"Bearer {OPENAI_KEY}"},
                json={
                    "model": OPENAI_MODEL,
                    "messages": [{"role": "user", "content": prompt}],
                    "max_tokens": 800,
                },
            )
            response.raise_for_status()
            content = response.json()["choices"][0]["message"]["content"].strip()
        if not content:
            raise ValueError("campaign model returned empty content")
        return content
    except (httpx.HTTPError, KeyError, IndexError, TypeError, ValueError) as exc:
        log.error("campaign_speech_model_unavailable", error=str(exc))
        raise HTTPException(status_code=503, detail="configured campaign language model is unavailable") from exc


# ── Request Models ────────────────────────────────────────────────────────────

class EligibilityReq(BaseModel):
    # INTEGRITY: facts must be explicitly provided. Previously has_school_cert
    # and is_nigerian defaulted to True, turning missing data into a favourable
    # compliance verdict. None means "not_assessed".
    candidate_id: int; office_type: str; state_code: str; party_code: str
    age: int = Field(..., ge=18, le=100)
    has_school_cert: Optional[bool] = None; is_nigerian: Optional[bool] = None
    criminal_record: Optional[bool] = None; dual_citizen: Optional[bool] = None
    years_in_party: Optional[int] = Field(default=None, ge=0)

class PlanCreateReq(BaseModel):
    candidate_id: int; election_id: int; office_type: str
    state_code: str; lga_code: str = ""; party_code: str
    target_votes: int = 100_000; budget_ngn: int = 10_000_000; election_date: str = ""

class TargetingReq(BaseModel):
    candidate_id: str; state_code: str; party_code: str; office_type: str; target_votes: int = 100_000

class BudgetReq(BaseModel):
    candidate_id: str; election_id: str; total_budget: int; state_code: str; office_type: str

class ScheduleReq(BaseModel):
    candidate_id: str; election_id: str; state_code: str; office_type: str

class SentimentReq(BaseModel):
    candidate_id: str; period: str = "30d"

class OpponentReq(BaseModel):
    candidate_id: str; state_code: str; office_type: str

class CanvassingReq(BaseModel):
    candidate_id: str; state_code: str; lga_codes: List[str]

class FundraisingReq(BaseModel):
    candidate_id: str; office_type: str; target_amount: int

class MediaBuyReq(BaseModel):
    candidate_id: str; state_code: str; budget: int; office_type: str

class SpeechReq(BaseModel):
    candidate_name: str; speech_type: str = "rally"; office_type: str; state_code: str
    key_policies: List[str] = Field(default=["Security", "Education", "Infrastructure"])
    language: str = "en"

class PolicyResonanceReq(BaseModel):
    candidate_id: str; state_code: str
    policies: List[str] = Field(default=["Security reform", "Education funding", "Agricultural development"])

class WarRoomReq(BaseModel):
    candidate_id: str; election_id: str

class DebateTrackerReq(BaseModel):
    candidate_id: str; statements: List[str]

class VolunteerGraphReq(BaseModel):
    candidate_id: str; state_code: str; num_volunteers: int = Field(default=100, ge=10, le=1000)


# ── Endpoints ─────────────────────────────────────────────────────────────────

@app.post("/api/v1/campaign/eligibility", tags=["Eligibility"])
async def check_eligibility(req: EligibilityReq):
    """Full INEC eligibility check against the 1999 Constitution (as amended)."""
    # INTEGRITY: unknown office types are rejected — never silently remapped
    # to "house" requirements.
    if req.office_type not in ELIGIBILITY:
        raise HTTPException(
            status_code=400,
            detail=f"unknown office_type '{req.office_type}'; valid: {sorted(ELIGIBILITY)}",
        )
    return engine_eligibility(req.candidate_id, req.office_type, req.state_code, req.party_code,
                               req.age, req.has_school_cert, req.is_nigerian,
                               req.criminal_record, req.dual_citizen, req.years_in_party)


@app.post("/api/v1/campaign/plan", tags=["Campaign Plan"])
async def create_plan(req: PlanCreateReq):
    """Disabled until the authorised campaign data integrations are configured."""
    return campaign_data_unavailable("campaign_plan")


@app.get("/api/v1/campaign/plan/{plan_id}", tags=["Campaign Plan"])
async def get_plan(plan_id: str):
    plan = _plans.get(plan_id)
    if not plan:
        # Durable fallback: the in-memory dict is a write-through cache; a
        # restarted/replica instance still serves persisted plans.
        plan = await _load_plan_from_store(plan_id)
        if plan:
            _plans[plan_id] = plan
    if not plan:
        raise HTTPException(404, "Plan not found")
    return plan


@app.post("/api/v1/campaign/targeting", tags=["Voter Targeting"])
async def voter_targeting(req: TargetingReq):
    """Innovation 2: Micro-targeting heat maps with ward-level swing analysis."""
    return engine_targeting(req.candidate_id, req.state_code, req.party_code,
                             req.office_type, req.target_votes)


@app.post("/api/v1/campaign/budget", tags=["Budget Optimiser"])
async def budget_allocation(req: BudgetReq):
    """Innovation 4 & 9: AI-optimised budget allocation with channel ROI modelling."""
    return engine_budget(req.candidate_id, req.election_id, req.total_budget,
                          req.state_code, req.office_type)


@app.post("/api/v1/campaign/schedule", tags=["Campaign Schedule"])
async def campaign_schedule(req: ScheduleReq):
    """Generate a full 52-week campaign event schedule."""
    return engine_schedule(req.candidate_id, req.election_id, req.state_code, req.office_type)


@app.post("/api/v1/campaign/sentiment", tags=["Sentiment Analysis"])
async def sentiment_analysis(req: SentimentReq):
    """Real-time sentiment analysis with platform breakdown and trend decomposition."""
    return engine_sentiment(req.candidate_id, req.period)


@app.post("/api/v1/campaign/opponents", tags=["Opponent Intelligence"])
async def opponent_analysis(req: OpponentReq):
    """Innovation 3: Opponent vulnerability scanner."""
    return engine_opponents(req.candidate_id, req.state_code, req.office_type)


@app.post("/api/v1/campaign/canvassing-routes", tags=["Canvassing Optimiser"])
async def canvassing_routes(req: CanvassingReq):
    """Innovation 5: TSP nearest-neighbour optimal canvassing route optimiser."""
    return engine_canvassing(req.candidate_id, req.state_code, req.lga_codes)


@app.post("/api/v1/campaign/fundraising", tags=["Fundraising"])
async def fundraising_optimizer(req: FundraisingReq):
    """Innovation 4: Fundraising optimiser with donor segmentation."""
    return engine_fundraising(req.candidate_id, req.office_type, req.target_amount)


@app.post("/api/v1/campaign/media-buy", tags=["Media Buy"])
async def media_buy_optimizer(req: MediaBuyReq):
    """Innovation 9: Media buy optimiser — GRP/reach/frequency optimisation."""
    return engine_media_buy(req.candidate_id, req.state_code, req.budget, req.office_type)


@app.post("/api/v1/campaign/speech", tags=["AI Speech Writer"])
@limiter.limit("10/minute")
async def generate_speech(request: Request, req: SpeechReq):
    """Innovation 1: AI speech writer — rally, manifesto, press release, debate, victory, concession."""
    text = await engine_speech(req.speech_type, req.candidate_name, req.office_type,
                                req.state_code, req.key_policies, req.language)
    return {
        "speech_type": req.speech_type, "candidate_name": req.candidate_name,
        "office_type": req.office_type, "state_code": req.state_code, "language": req.language,
        "speech_text": text,
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


@app.post("/api/v1/campaign/policy-resonance", tags=["Policy Analyser"])
async def policy_resonance(req: PolicyResonanceReq):
    """Innovation 8: Policy resonance analyser — maps policies to zone demographic priorities."""
    return engine_policy_resonance(req.candidate_id, req.state_code, req.policies)


@app.post("/api/v1/campaign/war-room", tags=["War Room"])
async def war_room_dashboard(req: WarRoomReq):
    """Innovation 10: Election day war room dashboard."""
    data = engine_war_room(req.candidate_id, req.election_id)
    _war_rooms[req.candidate_id] = data
    await _persist_war_room(req.candidate_id, data)
    await _broadcast({"type": "war_room_update", "data": data})
    return data


@app.post("/api/v1/campaign/debate-tracker", tags=["Debate Tracker"])
async def debate_tracker(req: DebateTrackerReq):
    """Disabled until verified transcripts and an approved NLP model are configured."""
    return campaign_data_unavailable("debate_tracker")


@app.post("/api/v1/campaign/volunteer-network", tags=["Volunteer Network"])
async def volunteer_network(req: VolunteerGraphReq):
    """Disabled until an authoritative volunteer CRM integration is configured."""
    return campaign_data_unavailable("volunteer_network")


@app.get("/api/v1/campaign/states", tags=["Reference Data"])
async def list_states():
    # INTEGRITY: the voter counts and swing values in STATES/ZONES are
    # editorial estimates, NOT official INEC register figures. Labeled so
    # callers never treat them as authoritative reference data.
    return {
        "states": STATES,
        "zones": ZONES,
        "data_quality": "reference_estimates_unverified",
        "source": "editorial",
        "note": "Voter counts and swing values are unverified editorial estimates, "
                "not official INEC figures. Do not use for planning decisions.",
    }


@app.get("/api/v1/campaign/offices", tags=["Reference Data"])
async def list_offices():
    return {"offices": [{"id": k, "requirements": v} for k, v in ELIGIBILITY.items()]}


@app.websocket("/ws/campaign")
async def campaign_ws(ws: WebSocket):
    # SECURITY: HTTP middleware does not cover WebSocket upgrades — enforce
    # the same API key here (query token), failing closed when unconfigured.
    token = ws.query_params.get("token", "")
    if not CAMPAIGN_API_KEYS or not _key_valid(token):
        await ws.close(code=4401)
        return
    await ws.accept()
    _ws_clients.append(ws)
    try:
        while True:
            data = await ws.receive_json()
            if data.get("action") == "war_room":
                wr = _war_rooms.get(data.get("candidate_id", ""), {})
                await ws.send_json({"type": "war_room_update", "data": wr})
            elif data.get("action") == "ping":
                await ws.send_json({"type": "pong", "ts": time.time()})
    except WebSocketDisconnect:
        if ws in _ws_clients:
            _ws_clients.remove(ws)


@app.get("/api/v1/campaign/health", tags=["Health"])
async def health():
    """Liveness + real dependency probe.

    When INEC_API_URL is configured we actually ping it; an unreachable
    upstream degrades the service to 503 instead of a static "healthy".
    """
    checks: Dict[str, bool] = {}
    degraded = False
    if INEC_API:
        try:
            resp = await _get_with_retry(f"{INEC_API}/health", timeout=5.0, attempts=2)
            checks["inec_api"] = resp.status_code < 500
        except httpx.HTTPError:
            checks["inec_api"] = False
        degraded = not checks["inec_api"]
    # Real persistence probe: SELECT 1 against the state store when configured.
    if _pg_pool is not None:
        try:
            async with _pg_pool.acquire() as conn:
                await conn.fetchval("SELECT 1")
            checks["postgres"] = True
        except (asyncpg.PostgresError, OSError):
            checks["postgres"] = False
            degraded = True
    elif DATABASE_URL:
        checks["postgres"] = False
        degraded = True
    body = {
        "status": "degraded" if degraded else "healthy",
        "checks": checks,
        "persistence": "postgresql" if _pg_pool is not None else "in_memory",
        "active_plans": len(_plans),
        "version": "2.1.0",
        "disabled_features": sorted(DISABLED_DATA_FEATURES),
        "enabled_features": ["eligibility", "speech", "states", "offices"],
    }
    return JSONResponse(status_code=503 if degraded else 200, content=body)


# ─── Stakeholder Recommendation Engine ───────────────────────────────────────

class StakeholderReq(BaseModel):
    candidate_name: str
    state_code: str
    office_type: str
    party_code: str
    religion: Optional[str] = None
    ethnicity: Optional[str] = None
    gender: Optional[str] = None
    top_n: int = 15

@app.post("/api/v1/campaign/stakeholders", tags=["Stakeholder Engagement"])
async def stakeholder_recommendations(req: StakeholderReq):
    """
    Returns a comprehensive, prioritised stakeholder engagement plan for a candidate.
    Covers local leaders, youth groups, women associations, market unions, religious bodies,
    traditional rulers, civil society, and professional associations for all 36 states + FCT.
    """
    return campaign_data_unavailable("stakeholder_recommendations")

@app.get("/api/v1/campaign/stakeholders/categories", tags=["Stakeholder Engagement"])
async def stakeholder_categories():
    return campaign_data_unavailable("stakeholder_recommendations")


@app.get("/api/v1/campaign/stakeholders/states", tags=["Stakeholder Engagement"])
async def stakeholder_states():
    return campaign_data_unavailable("stakeholder_recommendations")


if __name__ == "__main__":
    port = int(os.environ["PORT"])
    uvicorn.run(app, host="0.0.0.0", port=port, log_level="info")
