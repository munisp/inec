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
import calendar
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


# Features that remain honestly disabled: they require external data sources
# that no schema in this platform provides. Every other former entry has been
# wired to a real computation path (see engines below); do not re-add an
# entry here while a wired engine exists for it.
DISABLED_DATA_FEATURES = {
    "debate_tracker": "approved NLP model and verified debate transcript data",
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


# ── Shared DB-table access for wired engines ─────────────────────────────────
# The engines below read the campaign-platform Postgres schema (polling_units,
# war_room_incidents, fundraising_transactions, volunteers, opposition_research,
# budget_items, budget_statutory_caps, stakeholder_contacts, endorsements) when
# the configured DATABASE_URL points at a database containing those tables.
# Access is fail-closed: a missing table yields an honest 503, never fabricated
# rows.

_table_presence: Dict[str, bool] = {}


async def _table_exists(name: str) -> bool:
    if _pg_pool is None:
        return False
    if name in _table_presence:
        return _table_presence[name]
    async with _pg_pool.acquire() as conn:
        present = await conn.fetchval("SELECT to_regclass($1) IS NOT NULL", f"public.{name}")
    _table_presence[name] = bool(present)
    return bool(present)


async def _fetch_table(table: str, feature: str, sql: str, *args):
    """Run `sql` against `table` if present; otherwise fail closed with 503."""
    if not await _table_exists(table):
        raise HTTPException(
            status_code=503,
            detail={
                "status": "unavailable",
                "feature": feature,
                "reason": f"required table '{table}' is not present in the configured database",
                "required_data": f"campaign-platform schema table {table}",
            },
        )
    async with _pg_pool.acquire() as conn:
        return await conn.fetch(sql, *args)


def _profile_id(candidate_id) -> Optional[int]:
    """Map an endpoint candidate_id to the integer campaign profile id."""
    try:
        return int(candidate_id)
    except (TypeError, ValueError):
        return None


# ── Statutory campaign-spend caps (Electoral Act 2022 §88) ───────────────────
# Mirrors the authoritative, admin-editable seed in campaign-platform migration
# 0004 (budget_statutory_caps). The DB table, when reachable, overrides these
# constants so gazetted amendments propagate without a redeploy.
STATUTORY_CAPS_NGN = {
    "presidential": 5_000_000_000,   # Electoral Act 2022 s.88(2)
    "gubernatorial": 1_000_000_000,  # s.88(3)
    "senatorial": 100_000_000,       # s.88(4)
    "house": 70_000_000,             # s.88(5)
    "local": 30_000_000,             # s.88(6) — state assembly / area council
}
_CAP_OFFICE_LABELS = {
    "presidential": "President", "gubernatorial": "Governor",
    "senatorial": "Senator", "house": "House", "local": "LGA",
}


async def _statutory_cap(office_type: str) -> Optional[int]:
    """§88 cap for an office; DB value wins, statute constant is the fallback."""
    label = _CAP_OFFICE_LABELS.get(office_type)
    if label is None:
        return None
    if await _table_exists("budget_statutory_caps"):
        async with _pg_pool.acquire() as conn:
            row = await conn.fetchrow(
                "SELECT cap_amount FROM budget_statutory_caps WHERE office = $1", label)
        if row is not None:
            return int(row["cap_amount"])
    return STATUTORY_CAPS_NGN[office_type]


# Documented planning-model channel weights (heuristic, caller-overridable via
# the allocation output, NOT empirically fitted). The split itself is exact
# arithmetic over the caller's budget — no invented cost or reach figures.
BUDGET_CHANNELS = {
    "field_operations": 0.30,
    "media_and_communications": 0.25,
    "rallies_and_events": 0.15,
    "canvassing_materials": 0.10,
    "logistics_and_transport": 0.10,
    "agent_remuneration": 0.05,
    "contingency": 0.05,
}


async def engine_budget(candidate_id: str, election_id: str, total: int,
                        state_code: str, office_type: str) -> Dict:
    """§88-compliant budget allocation.

    Real components: (1) statutory cap check against Electoral Act 2022 §88
    (DB-seeded table wins when reachable); (2) deterministic largest-remainder
    allocation of the caller's total across documented channels; (3) actual
    spend aggregation from budget_items when the schema is reachable.
    """
    if office_type not in STATUTORY_CAPS_NGN:
        raise HTTPException(
            status_code=400,
            detail=f"unknown office_type '{office_type}'; valid: {sorted(STATUTORY_CAPS_NGN)}",
        )
    if total <= 0:
        raise HTTPException(status_code=400, detail="total_budget must be a positive amount in NGN")

    cap = await _statutory_cap(office_type)
    over_cap = total > cap

    # Largest-remainder allocation so channel amounts sum exactly to total.
    raw = [(ch, total * w) for ch, w in BUDGET_CHANNELS.items()]
    floors = {ch: int(math.floor(v)) for ch, v in raw}
    remainder = total - sum(floors.values())
    for ch, _ in sorted(raw, key=lambda kv: kv[1] - math.floor(kv[1]), reverse=True)[:remainder]:
        floors[ch] += 1

    result: Dict = {
        "candidate_id": candidate_id, "election_id": election_id,
        "office_type": office_type, "state_code": state_code,
        "total_budget_ngn": total,
        "allocation_ngn": floors,
        "allocation_model": "documented_channel_weights_v1",
        "planning_model": True,
        "statutory_cap": {
            "office": _CAP_OFFICE_LABELS[office_type],
            "cap_ngn": cap,
            "source": "Electoral Act 2022 §88 (budget_statutory_caps when reachable)",
            "compliant": not over_cap,
            "exceeds_cap_by_ngn": max(0, total - cap),
        },
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    if over_cap:
        # INTEGRITY: never silently allocate an unlawful budget — refuse.
        raise HTTPException(
            status_code=422,
            detail={
                "status": "rejected",
                "reason": "total_budget exceeds the Electoral Act 2022 §88 statutory cap",
                "office": _CAP_OFFICE_LABELS[office_type],
                "cap_ngn": cap,
                "requested_ngn": total,
                "exceeds_by_ngn": total - cap,
            },
        )

    pid = _profile_id(candidate_id)
    if pid is not None and await _table_exists("budget_items"):
        rows = await _fetch_table(
            "budget_items", "budget_allocation",
            """SELECT category,
                      COALESCE(SUM(budgeted_amount), 0) AS budgeted,
                      COALESCE(SUM(spent_amount), 0) AS spent
               FROM budget_items WHERE profile_id = $1
               GROUP BY category ORDER BY budgeted DESC""", pid)
        result["actuals"] = {
            "source": "budget_items",
            "by_category": [
                {"category": r["category"], "budgeted_ngn": float(r["budgeted"]),
                 "spent_ngn": float(r["spent"])} for r in rows
            ],
            "total_budgeted_ngn": float(sum(r["budgeted"] for r in rows)),
            "total_spent_ngn": float(sum(r["spent"] for r in rows)),
        }
    else:
        result["actuals"] = None
    return result


async def engine_targeting(candidate_id: str, state_code: str, party_code: str,
                           office_type: str, target_votes: int) -> Dict:
    """Ward-level vote-target decomposition.

    Prefers the real polling_units registry (registered-voter weights per ward);
    falls back to a uniform LGA split over the editorial state reference data,
    with the data-quality label propagated either way.
    """
    st = next((s for s in STATES if s["code"] == state_code), None)
    if st is None:
        raise HTTPException(
            status_code=400,
            detail=f"unknown state_code '{state_code}'; valid: {[s['code'] for s in STATES]}",
        )
    if target_votes <= 0:
        raise HTTPException(status_code=400, detail="target_votes must be positive")

    if await _table_exists("polling_units"):
        rows = await _fetch_table(
            "polling_units", "voter_targeting",
            """SELECT ward_code, COUNT(*) AS polling_units,
                      COALESCE(SUM(registered_voters), 0) AS registered_voters
               FROM polling_units WHERE code LIKE $1
               GROUP BY ward_code ORDER BY registered_voters DESC""",
            f"{state_code}/%")
        if rows:
            total_reg = sum(int(r["registered_voters"]) for r in rows)
            targets = []
            allocated = 0
            for i, r in enumerate(rows):
                voters = int(r["registered_voters"])
                if i == len(rows) - 1:
                    share = target_votes - allocated  # exact remainder on last ward
                else:
                    share = round(target_votes * voters / total_reg) if total_reg else 0
                allocated += share
                targets.append({
                    "ward_code": r["ward_code"], "polling_units": int(r["polling_units"]),
                    "registered_voters": voters, "target_votes": share,
                })
            return {
                "candidate_id": candidate_id, "state_code": state_code,
                "party_code": party_code, "office_type": office_type,
                "target_votes": target_votes,
                "data_source": "polling_units_registry",
                "data_quality": "registry_operational_data",
                "wards": targets,
                "ward_count": len(targets),
                "note": "Targets are pro-rata to registered voters per ward; "
                        "persuasion priorities require survey data this platform does not hold.",
                "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            }

    # Fallback: uniform split across the state's LGAs over editorial estimates.
    per_lga, rem = divmod(target_votes, st["lgas"])
    lga_targets = [
        {"lga_index": i + 1, "target_votes": per_lga + (1 if i < rem else 0)}
        for i in range(st["lgas"])
    ]
    return {
        "candidate_id": candidate_id, "state_code": state_code,
        "party_code": party_code, "office_type": office_type,
        "target_votes": target_votes,
        "data_source": "editorial_state_reference",
        "data_quality": "reference_estimates_unverified",
        "lga_targets": lga_targets,
        "lga_count": st["lgas"],
        "note": "Uniform per-LGA split of the caller's target. State voter figures are "
                "unverified editorial estimates, not INEC register data. Configure the "
                "polling_units registry for ward-level operational targets.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


_SCHEDULE_MILESTONES = [
    # (days_before_election, phase, activity, statutory)
    (365, "foundation", "Formal declaration and campaign structure setup", False),
    (270, "primaries", "Party primary campaign window (confirm dates with party/INEC)", False),
    (180, "compliance", "INEC nomination filing preparation (confirm deadline with INEC)", False),
    (120, "outreach", "Voter-contact drive: ward-level canvassing phase 1", False),
    (90, "policy", "Manifesto launch and stakeholder engagement tour", False),
    (60, "mobilisation", "Rally tour across LGAs; canvassing phase 2", False),
    (30, "persuasion", "Targeted outreach, debate preparation, media buy execution", False),
    (14, "gotv", "GOTV operation stand-up; agent recruitment finalised", False),
    (7, "readiness", "Election-day logistics: agent deployment, war-room drill", False),
    (0, "election_day", "Election day operations and results monitoring", False),
]


def engine_schedule(candidate_id: str, election_id: str, state_code: str,
                    office_type: str, election_date: Optional[str]) -> Dict:
    """Backward-planned milestone schedule anchored to a caller-supplied,
    INEC-confirmed election date. Never invents the election date itself."""
    if not election_date:
        raise HTTPException(
            status_code=400,
            detail="election_date is required (YYYY-MM-DD, INEC-confirmed); "
                   "the service never fabricates election dates",
        )
    try:
        y, m, d = (int(p) for p in election_date.split("-"))
        # Validate as a real calendar date.
        assert 1 <= m <= 12 and 1 <= d <= calendar.monthrange(y, m)[1]
        election_epoch = calendar.timegm((y, m, d, 0, 0, 0, 0, 0, 0))
    except (ValueError, AssertionError, AttributeError):
        raise HTTPException(status_code=400, detail="election_date must be YYYY-MM-DD")

    milestones = []
    for days_before, phase, activity, statutory in _SCHEDULE_MILESTONES:
        ts = election_epoch - days_before * 86400
        milestones.append({
            "date": time.strftime("%Y-%m-%d", time.gmtime(ts)),
            "days_before_election": days_before,
            "phase": phase,
            "activity": activity,
            "statutory_deadline": statutory,
        })
    return {
        "candidate_id": candidate_id, "election_id": election_id,
        "state_code": state_code, "office_type": office_type,
        "election_date": election_date,
        "milestones": milestones,
        "note": "Milestones are a campaign planning template anchored to the supplied "
                "election date. Statutory/INEC deadlines must be confirmed against the "
                "official INEC timetable; none are asserted here.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


async def engine_opponents(candidate_id: str, state_code: str, office_type: str) -> Dict:
    """Opposition research from the campaign's own recorded entries."""
    pid = _profile_id(candidate_id)
    if pid is None:
        raise HTTPException(status_code=400, detail="candidate_id must be the numeric campaign profile id")
    rows = await _fetch_table(
        "opposition_research", "opponent_analysis",
        """SELECT opponent_name, party, threat_level, key_issues, strength, weakness,
                  notes, updated_at
           FROM opposition_research WHERE profile_id = $1
           ORDER BY updated_at DESC""", pid)
    return {
        "candidate_id": candidate_id, "state_code": state_code, "office_type": office_type,
        "data_source": "opposition_research",
        "opponents": [
            {"opponent_name": r["opponent_name"], "party": r["party"],
             "threat_level": r["threat_level"], "key_issues": r["key_issues"],
             "strength": r["strength"], "weakness": r["weakness"], "notes": r["notes"],
             "updated_at": r["updated_at"].isoformat() if r["updated_at"] else None}
            for r in rows
        ],
        "opponent_count": len(rows),
        "note": "Entries are the campaign's own recorded research; empty means none recorded.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def _haversine_km(lat1: float, lon1: float, lat2: float, lon2: float) -> float:
    r = 6371.0088
    p1, p2 = math.radians(lat1), math.radians(lat2)
    dp, dl = math.radians(lat2 - lat1), math.radians(lon2 - lon1)
    a = math.sin(dp / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dl / 2) ** 2
    return 2 * r * math.asin(math.sqrt(a))


def _nearest_neighbour_route(points: List[Dict]) -> List[Dict]:
    """Greedy nearest-neighbour tour over geocoded polling units (real TSP heuristic)."""
    if len(points) <= 2:
        return list(points)
    remaining = points[1:]
    route = [points[0]]
    while remaining:
        last = route[-1]
        nxt = min(remaining, key=lambda p: _haversine_km(
            last["latitude"], last["longitude"], p["latitude"], p["longitude"]))
        route.append(nxt)
        remaining.remove(nxt)
    return route


async def engine_canvassing(candidate_id: str, state_code: str, lga_codes: List[str]) -> Dict:
    """Canvassing route optimiser over the real PU registry geography.

    Nearest-neighbour TSP across geocoded polling units, one route per
    requested LGA (PU codes are namespaced STATE/LGA/...).
    """
    if not lga_codes:
        raise HTTPException(status_code=400, detail="lga_codes must list at least one LGA code")
    routes = []
    for lga in lga_codes:
        rows = await _fetch_table(
            "polling_units", "canvassing_routes",
            """SELECT code, name, ward_code, registered_voters, latitude, longitude
               FROM polling_units
               WHERE code LIKE $1 AND latitude IS NOT NULL AND longitude IS NOT NULL
               ORDER BY code""", f"{state_code}/{lga}/%")
        points = [
            {"code": r["code"], "name": r["name"], "ward_code": r["ward_code"],
             "registered_voters": r["registered_voters"] or 0,
             "latitude": float(r["latitude"]), "longitude": float(r["longitude"])}
            for r in rows
        ]
        if not points:
            routes.append({"lga_code": lga, "status": "no_geocoded_polling_units",
                           "stops": [], "distance_km": 0.0})
            continue
        ordered = _nearest_neighbour_route(points)
        dist = sum(
            _haversine_km(a["latitude"], a["longitude"], b["latitude"], b["longitude"])
            for a, b in zip(ordered, ordered[1:])
        )
        routes.append({
            "lga_code": lga,
            "status": "optimised",
            "algorithm": "nearest_neighbour_tsp",
            "stops": ordered,
            "stop_count": len(ordered),
            "total_registered_voters": sum(p["registered_voters"] for p in ordered),
            "distance_km": round(dist, 2),
        })
    return {
        "candidate_id": candidate_id, "state_code": state_code,
        "data_source": "polling_units_registry",
        "routes": routes,
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


async def engine_fundraising(candidate_id: str, office_type: str, target: int) -> Dict:
    """Fundraising position from the campaign's real transaction ledger."""
    pid = _profile_id(candidate_id)
    if pid is None:
        raise HTTPException(status_code=400, detail="candidate_id must be the numeric campaign profile id")
    if target <= 0:
        raise HTTPException(status_code=400, detail="target_amount must be positive")

    totals = await _fetch_table(
        "fundraising_transactions", "fundraising",
        """SELECT COALESCE(SUM(amount), 0) AS total,
                  COUNT(*) AS transaction_count,
                  COUNT(*) FILTER (WHERE is_verified) AS verified_count,
                  COALESCE(SUM(amount) FILTER (WHERE is_verified), 0) AS verified_total,
                  COUNT(DISTINCT donor_name) FILTER (WHERE donor_name IS NOT NULL) AS donor_count
           FROM fundraising_transactions WHERE profile_id = $1""", pid)
    by_source = await _fetch_table(
        "fundraising_transactions", "fundraising",
        """SELECT COALESCE(source, 'unspecified') AS source,
                  SUM(amount) AS total, COUNT(*) AS count
           FROM fundraising_transactions WHERE profile_id = $1
           GROUP BY 1 ORDER BY total DESC""", pid)
    by_category = await _fetch_table(
        "fundraising_transactions", "fundraising",
        """SELECT COALESCE(category, 'uncategorised') AS category,
                  SUM(amount) AS total, COUNT(*) AS count
           FROM fundraising_transactions WHERE profile_id = $1
           GROUP BY 1 ORDER BY total DESC""", pid)

    t = totals[0]
    verified_total = float(t["verified_total"])
    return {
        "candidate_id": candidate_id, "office_type": office_type,
        "target_amount_ngn": target,
        "data_source": "fundraising_transactions",
        "raised_total_ngn": float(t["total"]),
        "verified_raised_ngn": verified_total,
        "transaction_count": int(t["transaction_count"]),
        "verified_transaction_count": int(t["verified_count"]),
        "distinct_donor_count": int(t["donor_count"]),
        "gap_to_target_ngn": max(0.0, target - verified_total),
        "pct_of_target_verified": round(verified_total / target * 100, 1),
        "by_source": [{"source": r["source"], "total_ngn": float(r["total"]),
                       "count": int(r["count"])} for r in by_source],
        "by_category": [{"category": r["category"], "total_ngn": float(r["total"]),
                         "count": int(r["count"])} for r in by_category],
        "note": "Aggregates computed from the campaign's recorded transactions; "
                "gap and percentages use verified amounts only.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


# Media-mix planning weights (documented heuristic, NOT fitted to rate cards).
MEDIA_MIX_WEIGHTS = {
    "radio": 0.35, "television": 0.30, "digital": 0.20,
    "outdoor": 0.10, "print": 0.05,
}


async def engine_media_buy(candidate_id: str, state_code: str, budget: int, office_type: str) -> Dict:
    """Media budget split. Money arithmetic is real; no reach/GRP figures are
    asserted because the platform holds no rate-card or audience data."""
    st = next((s for s in STATES if s["code"] == state_code), None)
    if st is None:
        raise HTTPException(
            status_code=400,
            detail=f"unknown state_code '{state_code}'; valid: {[s['code'] for s in STATES]}",
        )
    if budget <= 0:
        raise HTTPException(status_code=400, detail="budget must be a positive amount in NGN")
    cap = await _statutory_cap(office_type) if office_type in STATUTORY_CAPS_NGN else None

    raw = [(ch, budget * w) for ch, w in MEDIA_MIX_WEIGHTS.items()]
    floors = {ch: int(math.floor(v)) for ch, v in raw}
    remainder = budget - sum(floors.values())
    for ch, _ in sorted(raw, key=lambda kv: kv[1] - math.floor(kv[1]), reverse=True)[:remainder]:
        floors[ch] += 1

    result: Dict = {
        "candidate_id": candidate_id, "state_code": state_code, "office_type": office_type,
        "budget_ngn": budget,
        "channel_split_ngn": floors,
        "allocation_model": "documented_media_mix_v1",
        "planning_model": True,
        "note": "Budget split only. Reach, GRP and frequency estimates require rate-card "
                "and audience data the platform does not hold; none are asserted.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    if cap is not None:
        result["statutory_cap_context"] = {
            "cap_ngn": cap,
            "pct_of_cap": round(budget / cap * 100, 2),
            "note": "Media spend counts toward the Electoral Act 2022 §88 total cap.",
        }
    return result


# Keyword map from caller-supplied policy text to ZONE_PRIORITIES topics.
_POLICY_KEYWORDS = {
    "security": ["security", "insecurity", "police", "kidnap", "insurg", "bandit"],
    "agriculture": ["agric", "farm", "food", "irrigation", "livestock"],
    "education": ["educat", "school", "teacher", "literacy", "university"],
    "infrastructure": ["infrastructure", "road", "power", "electric", "water", "bridge", "housing"],
    "health": ["health", "hospital", "clinic", "medical", "malaria", "maternal"],
    "economy": ["econom", "job", "unemploy", "trade", "business", "inflation", "tax"],
    "tech": ["tech", "digital", "internet", "startup", "innovation"],
    "oil_gas": ["oil", "gas", "petrol", "refiner", "pipeline"],
    "environment": ["environment", "erosion", "flood", "pollution", "climate", "desertif"],
}


def engine_policy_resonance(candidate_id: str, state_code: str, policies: List[str]) -> Dict:
    """Map caller policies onto the zone priority matrix. Scores are the
    editorial ZONE_PRIORITIES weights of matched topics — labeled as such."""
    st = next((s for s in STATES if s["code"] == state_code), None)
    if st is None:
        raise HTTPException(
            status_code=400,
            detail=f"unknown state_code '{state_code}'; valid: {[s['code'] for s in STATES]}",
        )
    weights = ZONE_PRIORITIES.get(st["zone"], {})
    scored = []
    for policy in policies:
        text = policy.lower()
        matched = [t for t, kws in _POLICY_KEYWORDS.items() if any(k in text for k in kws)]
        if matched and any(t in weights for t in matched):
            topic_scores = {t: weights[t] for t in matched if t in weights}
            score = max(topic_scores.values())
            scored.append({"policy": policy, "matched_topics": matched,
                           "resonance_score": score, "unmapped": False})
        else:
            # INTEGRITY: unmapped policies get null, not an invented score.
            scored.append({"policy": policy, "matched_topics": matched,
                           "resonance_score": None, "unmapped": True,
                           "note": "no zone-priority weight for the matched topic(s)"})
    ranked = sorted((s for s in scored if s["resonance_score"] is not None),
                    key=lambda s: s["resonance_score"], reverse=True)
    return {
        "candidate_id": candidate_id, "state_code": state_code, "zone": st["zone"],
        "data_quality": "reference_estimates_unverified",
        "policies": scored,
        "priority_ranking": [s["policy"] for s in ranked],
        "unmapped_count": sum(1 for s in scored if s["unmapped"]),
        "note": "Scores are the editorial zone-priority weights (unverified), not survey "
                "measurements. Unmapped policies return null rather than a fabricated score.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


async def engine_war_room(candidate_id: str, election_id: str) -> Dict:
    """War-room situation board aggregated from real incident records.

    Uses war_room_incidents when reachable; otherwise the service's own
    persisted store (which only ever holds previously computed boards) with an
    explicit empty-data marker — never fabricated incidents.
    """
    pid = _profile_id(candidate_id)
    if pid is not None and await _table_exists("war_room_incidents"):
        by_sev = await _fetch_table(
            "war_room_incidents", "war_room",
            """SELECT severity, status, COUNT(*) AS count FROM war_room_incidents
               WHERE profile_id = $1 GROUP BY severity, status""", pid)
        by_type = await _fetch_table(
            "war_room_incidents", "war_room",
            """SELECT COALESCE(incident_type, 'unspecified') AS incident_type, COUNT(*) AS count
               FROM war_room_incidents WHERE profile_id = $1
               GROUP BY 1 ORDER BY count DESC""", pid)
        open_rows = await _fetch_table(
            "war_room_incidents", "war_room",
            """SELECT id, incident_type, lga, ward, severity, occurred_at, assigned_to
               FROM war_room_incidents
               WHERE profile_id = $1 AND status IN ('open', 'in_progress')
               ORDER BY occurred_at DESC NULLS LAST LIMIT 50""", pid)
        sev_map: Dict[str, Dict[str, int]] = defaultdict(dict)
        for r in by_sev:
            sev_map[r["severity"] or "unknown"][r["status"] or "unknown"] = int(r["count"])
        return {
            "candidate_id": candidate_id, "election_id": election_id,
            "data_source": "war_room_incidents",
            "incidents_by_severity_status": {k: dict(v) for k, v in sev_map.items()},
            "incidents_by_type": [{"incident_type": r["incident_type"],
                                   "count": int(r["count"])} for r in by_type],
            "open_incidents": [
                {"id": r["id"], "incident_type": r["incident_type"], "lga": r["lga"],
                 "ward": r["ward"], "severity": r["severity"],
                 "occurred_at": r["occurred_at"].isoformat() if r["occurred_at"] else None,
                 "assigned_to": r["assigned_to"]}
                for r in open_rows
            ],
            "open_count": len(open_rows),
            "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        }

    existing = _war_rooms.get(candidate_id)
    return {
        "candidate_id": candidate_id, "election_id": election_id,
        "data_source": "service_store" if existing else "none",
        "board": existing,
        "open_count": 0 if not existing else existing.get("open_count", 0),
        "note": "war_room_incidents table not reachable; showing the last persisted "
                "board only. No incident data is fabricated.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


async def engine_volunteer_network(candidate_id: str, state_code: str) -> Dict:
    """Volunteer coverage aggregated from the real volunteer registry."""
    pid = _profile_id(candidate_id)
    if pid is None:
        raise HTTPException(status_code=400, detail="candidate_id must be the numeric campaign profile id")
    by_status = await _fetch_table(
        "volunteers", "volunteer_network",
        """SELECT status, COUNT(*) AS count FROM volunteers
           WHERE profile_id = $1 GROUP BY status""", pid)
    by_ward = await _fetch_table(
        "volunteers", "volunteer_network",
        """SELECT COALESCE(lga, 'unspecified') AS lga, COALESCE(ward, 'unspecified') AS ward,
                  COUNT(*) AS count
           FROM volunteers WHERE profile_id = $1
           GROUP BY 1, 2 ORDER BY count DESC""", pid)
    by_role = await _fetch_table(
        "volunteers", "volunteer_network",
        """SELECT COALESCE(role, 'unspecified') AS role, COUNT(*) AS count
           FROM volunteers WHERE profile_id = $1 GROUP BY 1 ORDER BY count DESC""", pid)
    total = sum(int(r["count"]) for r in by_status)
    wards_covered = sum(1 for r in by_ward if r["ward"] != "unspecified")
    return {
        "candidate_id": candidate_id, "state_code": state_code,
        "data_source": "volunteers",
        "total_volunteers": total,
        "by_status": {r["status"] or "unknown": int(r["count"]) for r in by_status},
        "by_role": [{"role": r["role"], "count": int(r["count"])} for r in by_role],
        "wards_covered": wards_covered,
        "by_ward": [{"lga": r["lga"], "ward": r["ward"], "count": int(r["count"])}
                    for r in by_ward],
        "note": "Counts from the recorded volunteer registry; empty means none registered. "
                "No ward-coverage denominator is asserted without an authoritative ward list.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


# ── W13: CA-Parity Analytics Engines ─────────────────────────────────────────
# The lawful analogue of Cambridge Analytica's psychographic/micro-targeting
# stack. Every score is computed from the campaign's own CONSENTED panel and
# real event data (campaign-platform migration 0007 tables). No inference over
# non-consenting individuals, ever; absent data fails closed.

# OCEAN trait → item-key prefixes. Item keys ending in 'r' are reverse-scored
# (6 - score), standard psychometric practice (IPIP convention).
_OCEAN_TRAITS = ("O", "C", "E", "A", "N")


def _score_panel_rows(rows) -> Dict[int, Dict[str, float]]:
    """Trait means per panelist from raw response rows. Real psychometric
    aggregation: mean of item scores per trait, reverse-keyed items flipped."""
    per: Dict[int, Dict[str, List[int]]] = defaultdict(lambda: defaultdict(list))
    for r in rows:
        key = r["item_key"]
        trait = key[0].upper()
        if trait not in _OCEAN_TRAITS:
            continue
        score = int(r["score"])
        if key.endswith("r"):
            score = 6 - score
        per[int(r["panelist_id"])][trait].append(score)
    return {
        pid: {t: round(sum(v) / len(v), 3) for t, v in traits.items() if v}
        for pid, traits in per.items()
    }


async def engine_panel_scores(candidate_id: str) -> Dict:
    """Psychometric trait scores for the consented survey panel."""
    pid = _profile_id(candidate_id)
    if pid is None:
        raise HTTPException(status_code=400, detail="candidate_id must be the numeric campaign profile id")
    rows = await _fetch_table(
        "survey_responses", "panel_scores",
        """SELECT sr.panelist_id, sr.instrument, sr.item_key, sr.score
           FROM survey_responses sr
           JOIN survey_panelists sp ON sp.id = sr.panelist_id
           WHERE sp.profile_id = $1 AND sp.status = 'active'""", pid)
    scores = _score_panel_rows(rows)
    return {
        "candidate_id": candidate_id,
        "data_source": "survey_responses (consented panel)",
        "panelists_scored": len(scores),
        "scores": [
            {"panelist_id": p, "traits": t} for p, t in scores.items()
        ],
        "method": "OCEAN trait means over Likert 1-5 items, reverse-keyed items flipped (IPIP convention)",
        "note": "Scores exist only for consented panelists who actually responded. "
                "The platform never infers personality for non-respondents.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def _kmeans(vectors: List[List[float]], k: int, iterations: int = 50) -> List[int]:
    """Deterministic k-means (farthest-point init, no RNG) over trait vectors."""
    n = len(vectors)
    if n == 0 or k <= 0:
        return []
    k = min(k, n)
    centroids = [vectors[0]]
    while len(centroids) < k:  # farthest-point initialisation — deterministic
        nxt = max(vectors, key=lambda v: min(
            sum((a - b) ** 2 for a, b in zip(v, c)) for c in centroids))
        centroids.append(nxt)
    assign = [0] * n
    for _ in range(iterations):
        changed = False
        for i, v in enumerate(vectors):
            best = min(range(k), key=lambda c: sum((a - b) ** 2 for a, b in zip(v, centroids[c])))
            if best != assign[i]:
                assign[i] = best
                changed = True
        for c in range(k):
            members = [vectors[i] for i in range(n) if assign[i] == c]
            if members:
                centroids[c] = [sum(m[j] for m in members) / len(members)
                                for j in range(len(vectors[0]))]
        if not changed:
            break
    return assign


async def engine_segments(candidate_id: str, k: int = 4) -> Dict:
    """Audience segmentation: deterministic k-means over consented-panel trait
    vectors joined to panelist geography. Segments carry real member counts."""
    pid = _profile_id(candidate_id)
    if pid is None:
        raise HTTPException(status_code=400, detail="candidate_id must be the numeric campaign profile id")
    if not 2 <= k <= 10:
        raise HTTPException(status_code=400, detail="k must be between 2 and 10")
    rows = await _fetch_table(
        "survey_responses", "audience_segmentation",
        """SELECT sr.panelist_id, sr.item_key, sr.score,
                  sp.state_code, sp.lga, sp.age_band, sp.gender
           FROM survey_responses sr
           JOIN survey_panelists sp ON sp.id = sr.panelist_id
           WHERE sp.profile_id = $1 AND sp.status = 'active'""", pid)
    meta: Dict[int, Dict] = {}
    for r in rows:
        meta[int(r["panelist_id"])] = {
            "state_code": r["state_code"], "lga": r["lga"],
            "age_band": r["age_band"], "gender": r["gender"],
        }
    scores = _score_panel_rows(rows)
    full = [p for p, t in scores.items() if len(t) == len(_OCEAN_TRAITS)]
    if len(full) < 2:
        return {
            "candidate_id": candidate_id, "data_source": "survey_responses (consented panel)",
            "segments": [], "panelists_with_full_vectors": len(full),
            "note": "Fewer than 2 panelists have complete OCEAN vectors — no segmentation "
                    "computed rather than fabricated. Collect more consented responses.",
            "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        }
    vectors = [[scores[p][t] for t in _OCEAN_TRAITS] for p in full]
    assign = _kmeans(vectors, k)
    segments: Dict[int, List[int]] = defaultdict(list)
    for p, a in zip(full, assign):
        segments[a].append(p)
    out = []
    for idx, members in sorted(segments.items()):
        centroid = {t: round(sum(scores[p][t] for p in members) / len(members), 3)
                    for t in _OCEAN_TRAITS}
        lgas: Dict[str, int] = defaultdict(int)
        for p in members:
            lga = meta.get(p, {}).get("lga") or "unspecified"
            lgas[lga] += 1
        out.append({
            "segment": idx, "member_count": len(members),
            "trait_centroid": centroid,
            "top_lgas": sorted(lgas.items(), key=lambda kv: kv[1], reverse=True)[:5],
        })
    return {
        "candidate_id": candidate_id,
        "data_source": "survey_responses (consented panel)",
        "algorithm": "deterministic k-means (farthest-point init) over OCEAN vectors",
        "k_requested": k,
        "segments": out,
        "note": "Segments are computed from consented panel responses only; membership "
                "counts are real. Projection onto non-panel voters requires their consent.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def _chi2_sf(x: float, df: int) -> float:
    """Survival function P(χ²_df ≥ x) via the regularised upper incomplete gamma
    (series/continued-fraction, Numerical Recipes). Real statistics, no scipy."""
    if x <= 0:
        return 1.0
    a = df / 2.0
    x = x / 2.0  # χ²_df CDF = P(df/2, x/2), the regularised lower gamma

    def gammainc_help(a: float, x: float):
        # returns (P, Q) regularised lower/upper incomplete gamma
        gln = math.lgamma(a)
        if x < a + 1.0:  # series for P
            ap, s, delta = a, 1.0 / a, 1.0 / a
            for _ in range(1000):
                ap += 1
                delta *= x / ap
                s += delta
                if abs(delta) < abs(s) * 1e-12:
                    break
            p = s * math.exp(-x + a * math.log(x) - gln)
            return p, 1.0 - p
        # continued fraction for Q
        b, c, d = x + 1.0 - a, 1e300, 1.0 / (x + 1.0 - a)
        h = d
        for i in range(1, 1000):
            an = -i * (i - a)
            b += 2.0
            d = an * d + b
            if abs(d) < 1e-300:
                d = 1e-300
            c = b + an / c
            if abs(c) < 1e-300:
                c = 1e-300
            d = 1.0 / d
            delta = d * c
            h *= delta
            if abs(delta - 1.0) < 1e-12:
                break
        q = math.exp(-x + a * math.log(x) - gln) * h
        return 1.0 - q, q

    _, q = gammainc_help(a, x)
    return min(max(q, 0.0), 1.0)


async def engine_message_test(test_id: int) -> Dict:
    """A/B message-test analysis: real chi-square independence test on response
    rates across variants, with effect size and honest power assessment."""
    variants = await _fetch_table(
        "message_variants", "message_testing",
        "SELECT id, label, body FROM message_variants WHERE test_id = $1 ORDER BY id",
        test_id)
    if not variants:
        raise HTTPException(status_code=404, detail="message test not found")
    counts = await _fetch_table(
        "message_events", "message_testing",
        """SELECT variant_id, event_type, COUNT(*) AS n
           FROM message_events
           WHERE variant_id = ANY($1::int[])
           GROUP BY variant_id, event_type""", [v["id"] for v in variants])
    per_variant: Dict[int, Dict[str, int]] = {v["id"]: defaultdict(int) for v in variants}
    for r in counts:
        per_variant[int(r["variant_id"])][r["event_type"]] = int(r["n"])

    table = []
    for v in variants:
        c = per_variant[v["id"]]
        imp, resp, conv = c.get("impression", 0), c.get("response", 0), c.get("conversion", 0)
        table.append({
            "variant_id": v["id"], "label": v["label"],
            "impressions": imp, "responses": resp, "conversions": conv,
            "response_rate": round(resp / imp, 4) if imp else None,
        })

    total_imp = sum(t["impressions"] for t in table)
    total_resp = sum(t["responses"] for t in table)
    analysis: Dict
    if len(table) < 2 or total_imp == 0:
        analysis = {"status": "insufficient_data",
                    "note": "No impressions recorded yet — no result asserted."}
    else:
        # χ² test of independence on responded/not-responded × variant.
        chi2 = 0.0
        for t in table:
            for observed, expected in (
                (t["responses"], total_resp * t["impressions"] / total_imp),
                (t["impressions"] - t["responses"],
                 (total_imp - total_resp) * t["impressions"] / total_imp),
            ):
                if expected > 0:
                    chi2 += (observed - expected) ** 2 / expected
        df = len(table) - 1
        p_value = round(_chi2_sf(chi2, df), 4)
        best = max((t for t in table if t["response_rate"] is not None),
                   key=lambda t: t["response_rate"], default=None)
        analysis = {
            "status": "analysed",
            "chi2_statistic": round(chi2, 4),
            "degrees_of_freedom": df,
            "p_value": p_value,
            "significant_at_95": p_value < 0.05,
            "leading_variant": best["label"] if best else None,
            "note": "Chi-square independence test over real recorded events. "
                    "p ≥ 0.05 means no statistically significant difference — "
                    "reported honestly, not smoothed over.",
        }
    return {
        "test_id": test_id,
        "data_source": "message_events",
        "variants": table,
        "analysis": analysis,
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


async def engine_sentiment(candidate_id: str, period: str) -> Dict:
    """Press-sentiment aggregation over the campaign's recorded media items
    (media_items table — real monitored coverage with human/system-assigned
    sentiment labels). Social listening remains unavailable without a feed."""
    pid = _profile_id(candidate_id)
    if pid is None:
        raise HTTPException(status_code=400, detail="candidate_id must be the numeric campaign profile id")
    days = {"7d": 7, "30d": 30, "90d": 90}.get(period)
    if days is None:
        raise HTTPException(status_code=400, detail="period must be one of 7d, 30d, 90d")
    rows = await _fetch_table(
        "media_items", "sentiment_analysis",
        """SELECT sentiment, source_type, zone, published_at, reach
           FROM media_items
           WHERE profile_id = $1
             AND published_at >= NOW() - ($2 || ' days')::interval""",
        pid, str(days))
    by_sentiment: Dict[str, int] = defaultdict(int)
    by_zone: Dict[str, Dict[str, int]] = defaultdict(lambda: defaultdict(int))
    by_source: Dict[str, Dict[str, int]] = defaultdict(lambda: defaultdict(int))
    reach_by_sentiment: Dict[str, int] = defaultdict(int)
    for r in rows:
        s = (r["sentiment"] or "unlabeled").lower()
        by_sentiment[s] += 1
        by_zone[r["zone"] or "unspecified"][s] += 1
        by_source[r["source_type"] or "unspecified"][s] += 1
        reach_by_sentiment[s] += int(r["reach"] or 0)
    total = sum(by_sentiment.values())
    return {
        "candidate_id": candidate_id, "period": period,
        "data_source": "media_items (campaign-recorded press monitoring)",
        "items_analysed": total,
        "by_sentiment": dict(by_sentiment),
        "reach_by_sentiment": dict(reach_by_sentiment),
        "by_zone": {z: dict(v) for z, v in by_zone.items()},
        "by_source_type": {s: dict(v) for s, v in by_source.items()},
        "note": "Aggregates the campaign's own recorded media items. Social-listening "
                "feeds are NOT integrated; unlabeled items are reported as unlabeled, "
                "never auto-classified by an unapproved model.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def _cosine(a: List[float], b: List[float]) -> float:
    dot = sum(x * y for x, y in zip(a, b))
    na = math.sqrt(sum(x * x for x in a))
    nb = math.sqrt(sum(x * x for x in b))
    return dot / (na * nb) if na and nb else 0.0


async def engine_lookalike(candidate_id: str, panelist_id: int, top_n: int = 10) -> Dict:
    """Lookalike expansion WITHIN the consented panel: cosine similarity over
    OCEAN trait vectors. Never expands into non-consenting populations."""
    pid = _profile_id(candidate_id)
    if pid is None:
        raise HTTPException(status_code=400, detail="candidate_id must be the numeric campaign profile id")
    rows = await _fetch_table(
        "survey_responses", "lookalike_expansion",
        """SELECT sr.panelist_id, sr.item_key, sr.score
           FROM survey_responses sr
           JOIN survey_panelists sp ON sp.id = sr.panelist_id
           WHERE sp.profile_id = $1 AND sp.status = 'active'""", pid)
    scores = _score_panel_rows(rows)
    full = {p: [t[t2] for t2 in _OCEAN_TRAITS] for p, t in scores.items()
            if len(t) == len(_OCEAN_TRAITS)}
    if panelist_id not in full:
        raise HTTPException(
            status_code=404,
            detail="panelist not found or lacks a complete OCEAN vector "
                   "(score exists only for consented respondents)")
    target = full[panelist_id]
    sims = [
        {"panelist_id": p, "similarity": round(_cosine(target, vec), 4)}
        for p, vec in full.items() if p != panelist_id
    ]
    sims.sort(key=lambda s: s["similarity"], reverse=True)
    return {
        "candidate_id": candidate_id, "seed_panelist_id": panelist_id,
        "data_source": "survey_responses (consented panel)",
        "method": "cosine similarity over OCEAN trait vectors",
        "lookalikes": sims[: max(1, min(top_n, 100))],
        "note": "Expansion is strictly within the consented panel. Projecting onto "
                "non-panel voters requires their consent (NDPA 2023).",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


async def engine_persuasion(candidate_id: str, state_code: str, office_type: str) -> Dict:
    """Persuasion-priority ranking at WARD level (aggregate only). A labeled
    statistical planning model over real polling-unit registration counts:
    priority = registered_voters × zone_priority_weight. The platform
    deliberately does NOT score individual non-consenting voters — persuasion
    insight at individual level comes only from the consented panel."""
    pid = _profile_id(candidate_id)
    if pid is None:
        raise HTTPException(status_code=400, detail="candidate_id must be the numeric campaign profile id")
    st = state_code.upper()
    if st not in {s["code"] for s in STATES}:
        raise HTTPException(status_code=400, detail=f"Unknown state code: {st}")
    rows = await _fetch_table(
        "polling_units", "persuasion_model",
        """SELECT ward_code,
                  COUNT(*) AS polling_units,
                  COALESCE(SUM(registered_voters), 0)::bigint AS registered_voters
           FROM polling_units
           WHERE code LIKE $1
           GROUP BY ward_code""", f"{st}/%")
    zone = next((z for z, info in ZONES.items() if st in info["states"]), None)
    priorities = ZONE_PRIORITIES.get(zone, {})
    zone_weight = round(sum(priorities.values()) / len(priorities), 3) if priorities else 1.0
    wards = [
        {
            "ward_code": r["ward_code"],
            "polling_units": int(r["polling_units"]),
            "registered_voters": int(r["registered_voters"] or 0),
            "persuasion_priority": round(int(r["registered_voters"] or 0) * zone_weight, 1),
        }
        for r in rows
    ]
    wards.sort(key=lambda w: w["persuasion_priority"], reverse=True)
    return {
        "candidate_id": candidate_id, "state_code": st, "office_type": office_type,
        "data_source": "polling_units (registration counts) + zone priorities",
        "model": "persuasion_priority = registered_voters × zone salience — a labeled "
                 "aggregate planning heuristic, not a voter-level score",
        "zone": zone,
        "zone_salience": {"value": zone_weight,
                          "definition": "mean of ZONE_PRIORITIES policy weights for the zone"},
        "wards_ranked": len(wards),
        "top_wards": wards[:20],
        "note": "Ward-level aggregation only. Individual-level persuasion scoring of "
                "non-consenting voters is intentionally not implemented (NDPA 2023); "
                "individual-level insight exists solely inside the consented panel.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


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
    # INTEGRITY: the election date must come from the caller (INEC-confirmed);
    # the service refuses to fabricate one.
    election_date: str

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
    """Compose and durably persist a campaign plan from the wired engines.

    The plan is a real composition: eligibility facts are not asserted here
    (use /eligibility with explicit facts), and the schedule is anchored to the
    caller's INEC-confirmed election date. Persisted to campaign_plans when
    Postgres is configured (write-through cache otherwise — non-production).
    """
    if req.office_type not in ELIGIBILITY:
        raise HTTPException(
            status_code=400,
            detail=f"unknown office_type '{req.office_type}'; valid: {sorted(ELIGIBILITY)}",
        )
    budget = await engine_budget(str(req.candidate_id), str(req.election_id),
                                 req.budget_ngn, req.state_code, req.office_type)
    schedule = engine_schedule(str(req.candidate_id), str(req.election_id),
                               req.state_code, req.office_type, req.election_date or None)
    targeting = await engine_targeting(str(req.candidate_id), req.state_code,
                                       req.party_code, req.office_type, req.target_votes)
    plan_id = uuid.uuid4().hex
    plan = {
        "plan_id": plan_id,
        "candidate_id": req.candidate_id, "election_id": req.election_id,
        "office_type": req.office_type, "state_code": req.state_code,
        "lga_code": req.lga_code, "party_code": req.party_code,
        "target_votes": req.target_votes, "budget_ngn": req.budget_ngn,
        "election_date": req.election_date,
        "sections": {"budget": budget, "schedule": schedule, "targeting": targeting},
        "eligibility": "not_assessed — run /api/v1/campaign/eligibility with explicit facts",
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    _plans[plan_id] = plan
    await _persist_plan(plan_id, plan)
    return plan


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
    return await engine_targeting(req.candidate_id, req.state_code, req.party_code,
                                   req.office_type, req.target_votes)


@app.post("/api/v1/campaign/budget", tags=["Budget Optimiser"])
async def budget_allocation(req: BudgetReq):
    """Innovation 4 & 9: AI-optimised budget allocation with channel ROI modelling."""
    return await engine_budget(req.candidate_id, req.election_id, req.total_budget,
                                req.state_code, req.office_type)


@app.post("/api/v1/campaign/schedule", tags=["Campaign Schedule"])
async def campaign_schedule(req: ScheduleReq):
    """Generate a full 52-week campaign event schedule."""
    return engine_schedule(req.candidate_id, req.election_id, req.state_code,
                           req.office_type, req.election_date)


@app.post("/api/v1/campaign/sentiment", tags=["Sentiment Analysis"])
async def sentiment_analysis(req: SentimentReq):
    """Press-sentiment aggregation over the campaign's recorded media items."""
    return await engine_sentiment(req.candidate_id, req.period)


@app.post("/api/v1/campaign/opponents", tags=["Opponent Intelligence"])
async def opponent_analysis(req: OpponentReq):
    """Innovation 3: Opponent vulnerability scanner."""
    return await engine_opponents(req.candidate_id, req.state_code, req.office_type)


# ── W13 analytics endpoints (CA-parity, consent-first) ────────────────────────

class PanelScoresReq(BaseModel):
    candidate_id: str

class SegmentsReq(BaseModel):
    candidate_id: str; k: int = 4

class LookalikeReq(BaseModel):
    candidate_id: str; panelist_id: int; top_n: int = 10

class PersuasionReq(BaseModel):
    candidate_id: str; state_code: str; office_type: str

@app.post("/api/v1/campaign/analytics/panel-scores", tags=["Analytics (CA-Parity)"])
async def analytics_panel_scores(req: PanelScoresReq):
    """Psychometric OCEAN trait scores for the consented survey panel."""
    return await engine_panel_scores(req.candidate_id)


@app.post("/api/v1/campaign/analytics/segments", tags=["Analytics (CA-Parity)"])
async def analytics_segments(req: SegmentsReq):
    """Audience segmentation (deterministic k-means) over consented-panel trait vectors."""
    return await engine_segments(req.candidate_id, req.k)


@app.get("/api/v1/campaign/analytics/message-test/{test_id}", tags=["Analytics (CA-Parity)"])
async def analytics_message_test(test_id: int):
    """A/B message-test analysis with a real chi-square independence test."""
    return await engine_message_test(test_id)


@app.post("/api/v1/campaign/analytics/lookalike", tags=["Analytics (CA-Parity)"])
async def analytics_lookalike(req: LookalikeReq):
    """Lookalike expansion within the consented panel (cosine similarity over OCEAN vectors)."""
    return await engine_lookalike(req.candidate_id, req.panelist_id, req.top_n)


@app.post("/api/v1/campaign/analytics/persuasion", tags=["Analytics (CA-Parity)"])
async def analytics_persuasion(req: PersuasionReq):
    """Ward-level persuasion-priority ranking (labeled aggregate model only)."""
    return await engine_persuasion(req.candidate_id, req.state_code, req.office_type)


@app.post("/api/v1/campaign/canvassing-routes", tags=["Canvassing Optimiser"])
async def canvassing_routes(req: CanvassingReq):
    """Innovation 5: TSP nearest-neighbour optimal canvassing route optimiser."""
    return await engine_canvassing(req.candidate_id, req.state_code, req.lga_codes)


@app.post("/api/v1/campaign/fundraising", tags=["Fundraising"])
async def fundraising_optimizer(req: FundraisingReq):
    """Innovation 4: Fundraising optimiser with donor segmentation."""
    return await engine_fundraising(req.candidate_id, req.office_type, req.target_amount)


@app.post("/api/v1/campaign/media-buy", tags=["Media Buy"])
async def media_buy_optimizer(req: MediaBuyReq):
    """Innovation 9: Media buy optimiser — GRP/reach/frequency optimisation."""
    return await engine_media_buy(req.candidate_id, req.state_code, req.budget, req.office_type)


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
    data = await engine_war_room(req.candidate_id, req.election_id)
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
    """Volunteer coverage from the recorded volunteer registry (real aggregates)."""
    return await engine_volunteer_network(req.candidate_id, req.state_code)


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
        "enabled_features": [
            "eligibility", "speech", "states", "offices",
            "campaign_plan", "budget_allocation", "voter_targeting",
            "campaign_schedule", "canvassing_routes", "fundraising",
            "media_buy", "policy_resonance", "war_room", "opponent_analysis",
            "volunteer_network", "stakeholder_recommendations",
            "sentiment_analysis", "panel_scores", "audience_segmentation",
            "message_testing", "lookalike_expansion", "persuasion_model",
        ],
    }
    return JSONResponse(status_code=503 if degraded else 200, content=body)


# ─── Stakeholder Recommendation Engine ───────────────────────────────────────

class StakeholderReq(BaseModel):
    candidate_id: int  # numeric campaign profile id — recommendations read the recorded registry
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
    """Prioritised engagement list from the campaign's recorded stakeholder registry.

    INTEGRITY: only real recorded contacts are returned — the service never
    invents named individuals. Ranking uses the registry's own influence_level
    and relationship fields; public endorsements are listed separately when
    the endorsements table is reachable.
    """
    rank = {"high": 0, "medium": 1, "low": 2}
    rel_rank = {"hostile": 0, "neutral": 1, "supportive": 2}
    rows = await _fetch_table(
        "stakeholder_contacts", "stakeholder_recommendations",
        """SELECT name, title, organization, category, influence_level, relationship,
                  state, lga, next_action, last_contact
           FROM stakeholder_contacts WHERE profile_id = $1""", req.candidate_id)
    contacts = [
        {"name": r["name"], "title": r["title"], "organization": r["organization"],
         "category": r["category"], "influence_level": r["influence_level"],
         "relationship": r["relationship"], "state": r["state"], "lga": r["lga"],
         "next_action": r["next_action"],
         "last_contact": r["last_contact"].isoformat() if r["last_contact"] else None}
        for r in rows
    ]
    # Priority: high influence first, then least-warm relationship (most upside).
    contacts.sort(key=lambda c: (rank.get(c["influence_level"] or "medium", 1),
                                 rel_rank.get(c["relationship"] or "neutral", 1)))
    endorsements_out: List[Dict] = []
    if await _table_exists("endorsements"):
        erows = await _fetch_table(
            "endorsements", "stakeholder_recommendations",
            """SELECT endorser_name, title, organization, category, endorsed_at
               FROM endorsements WHERE profile_id = $1 AND is_public
               ORDER BY endorsed_at DESC""", req.candidate_id)
        endorsements_out = [
            {"endorser_name": r["endorser_name"], "title": r["title"],
             "organization": r["organization"], "category": r["category"],
             "endorsed_at": r["endorsed_at"].isoformat() if r["endorsed_at"] else None}
            for r in erows
        ]
    return {
        "candidate_id": req.candidate_id, "candidate_name": req.candidate_name,
        "state_code": req.state_code, "office_type": req.office_type,
        "data_source": "stakeholder_contacts",
        "recommendations": contacts[: req.top_n],
        "total_contacts": len(contacts),
        "endorsements": endorsements_out,
        "note": "Only stakeholders recorded in the campaign registry are returned; "
                "the registry is populated by the campaign platform.",
        "computed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }

@app.get("/api/v1/campaign/stakeholders/categories", tags=["Stakeholder Engagement"])
async def stakeholder_categories(candidate_id: int):
    """Distinct categories actually present in the campaign's stakeholder registry."""
    rows = await _fetch_table(
        "stakeholder_contacts", "stakeholder_recommendations",
        """SELECT DISTINCT category FROM stakeholder_contacts
           WHERE profile_id = $1 AND category IS NOT NULL ORDER BY category""",
        candidate_id)
    return {"candidate_id": candidate_id, "data_source": "stakeholder_contacts",
            "categories": [r["category"] for r in rows]}


@app.get("/api/v1/campaign/stakeholders/states", tags=["Stakeholder Engagement"])
async def stakeholder_states(candidate_id: int):
    """Distinct states actually present in the campaign's stakeholder registry."""
    rows = await _fetch_table(
        "stakeholder_contacts", "stakeholder_recommendations",
        """SELECT DISTINCT state FROM stakeholder_contacts
           WHERE profile_id = $1 AND state IS NOT NULL ORDER BY state""",
        candidate_id)
    return {"candidate_id": candidate_id, "data_source": "stakeholder_contacts",
            "states": [r["state"] for r in rows]}


if __name__ == "__main__":
    port = int(os.environ["PORT"])
    uvicorn.run(app, host="0.0.0.0", port=port, log_level="info")
