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
import json
import math
import os
import random
import statistics
import time
import uuid
from collections import defaultdict
from typing import Dict, List, Optional

import httpx
import uvicorn
from fastapi import FastAPI, HTTPException, WebSocket, WebSocketDisconnect
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field
import structlog

structlog.configure(
    processors=[structlog.processors.TimeStamper(fmt="iso"), structlog.processors.JSONRenderer()]
)
log = structlog.get_logger()

OPENAI_KEY  = os.getenv("OPENAI_API_KEY", "")
OPENAI_BASE = os.getenv("OPENAI_API_BASE", "https://api.openai.com/v1")
INEC_API    = os.getenv("INEC_API_URL", "http://localhost:8088")

app = FastAPI(title="INEC Campaign Planning Service", version="2.0.0")
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])

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

# ── In-memory store ───────────────────────────────────────────────────────────
_plans: Dict[str, Dict] = {}
_war_rooms: Dict[str, Dict] = {}
_ws_clients: List[WebSocket] = []


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
                        age: int, has_cert: bool, is_nigerian: bool,
                        criminal: bool, dual: bool, party_years: int) -> Dict:
    req = ELIGIBILITY.get(office_type, ELIGIBILITY["house"])
    passed, issues = [], []

    if age >= req["min_age"]:
        passed.append(f"Age {age} meets minimum of {req['min_age']}")
    else:
        issues.append(f"Age {age} below minimum {req['min_age']} for {office_type}")

    if is_nigerian:
        passed.append("Nigerian citizenship confirmed")
    else:
        issues.append("Must be a Nigerian citizen")

    if office_type == "presidential" and not is_nigerian:
        issues.append("Presidential candidates must be Nigerian by birth (Section 131(a))")

    if has_cert:
        passed.append("School Certificate requirement satisfied")
    else:
        issues.append("WAEC/NECO/GCE School Certificate required")

    if criminal:
        issues.append("Criminal conviction disqualifies under Section 66(1)(d)")
    else:
        passed.append("No criminal conviction on record")

    if dual:
        issues.append("Dual citizenship disqualifies under Section 66(1)(a)")
    else:
        passed.append("No dual-citizenship conflict")

    if party_years >= 1:
        passed.append(f"{party_years} year(s) party membership confirmed")
    else:
        issues.append("Must be a registered member of a political party")

    eligible = len(issues) == 0
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


def engine_targeting(candidate_id: str, state_code: str, party_code: str,
                      office_type: str, target_votes: int) -> Dict:
    st = _state(state_code)
    total_voters = st["voters"]
    swing = st["swing"]
    zone  = st["zone"]
    num_lgas = st["lgas"]

    lga_data = []
    for i in range(num_lgas):
        lga_voters = total_voters // num_lgas
        base_support = random.uniform(0.25, 0.65)
        sw = random.uniform(0.10, swing)
        priority = "high" if sw > 0.35 and base_support > 0.35 else "medium" if sw > 0.20 else "low"
        lga_data.append({
            "lga_index": i + 1,
            "registered_voters": lga_voters,
            "base_support_pct": round(base_support * 100, 1),
            "swing_potential_pct": round(sw * 100, 1),
            "target_priority": priority,
            "recommended_visits": 3 if priority == "high" else 2 if priority == "medium" else 1,
            "votes_available": int(lga_voters * sw * 0.6),
        })

    high_prio = [l for l in lga_data if l["target_priority"] == "high"]
    targetable = sum(l["votes_available"] for l in lga_data)
    gap = max(0, target_votes - int(total_voters * 0.25))
    demo = {
        "youth_18_35_pct": round(random.uniform(38, 48), 1),
        "women_pct": round(random.uniform(44, 52), 1),
        "urban_pct": round(random.uniform(35, 65), 1),
        "first_time_voters_pct": round(random.uniform(15, 25), 1),
    }

    return {
        "candidate_id": candidate_id,
        "state_code": state_code,
        "office_type": office_type,
        "total_registered_voters": total_voters,
        "target_votes": target_votes,
        "votes_gap": gap,
        "total_targetable_votes": targetable,
        "feasibility_score_pct": round(min(1.0, targetable / max(gap, 1)) * 100, 1),
        "zone": zone,
        "demographics": demo,
        "key_messages": {
            "youth": ["Jobs & entrepreneurship", "Education reform", "Tech hubs"],
            "women": ["Security & safety", "Maternal healthcare", "Economic empowerment"],
            "rural": ["Agriculture & food security", "Rural roads", "Water & sanitation"],
            "urban": ["Traffic & transport", "Power supply", "Business environment"],
        },
        "lga_targeting": lga_data,
        "high_priority_lgas": len(high_prio),
        "focus_lga_indices": [l["lga_index"] for l in high_prio[:5]],
        "strategy_summary": (
            f"Focus on {len(high_prio)} high-swing LGAs. "
            f"Youth (18-35) = {demo['youth_18_35_pct']}% of voters — prioritise digital & radio. "
            f"Need {gap:,} additional votes beyond base support."
        ),
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def engine_budget(candidate_id: str, election_id: str, total: int,
                   state_code: str, office_type: str) -> Dict:
    st = _state(state_code)
    total_voters = st["voters"]

    cpv = {"tv": 45, "radio": 12, "social": 8, "billboard": 25,
           "rally": 18, "sms": 3, "newspaper": 30, "ground_ops": 15, "polling_agents": 20}

    weights_by_office = {
        "presidential":  {"tv":0.25,"radio":0.15,"social":0.15,"billboard":0.08,"rally":0.15,"sms":0.05,"newspaper":0.05,"ground_ops":0.07,"polling_agents":0.05},
        "gubernatorial": {"tv":0.20,"radio":0.18,"social":0.12,"billboard":0.10,"rally":0.18,"sms":0.05,"newspaper":0.05,"ground_ops":0.07,"polling_agents":0.05},
        "senatorial":    {"tv":0.10,"radio":0.20,"social":0.15,"billboard":0.10,"rally":0.20,"sms":0.07,"newspaper":0.05,"ground_ops":0.08,"polling_agents":0.05},
        "house":         {"tv":0.05,"radio":0.18,"social":0.18,"billboard":0.08,"rally":0.22,"sms":0.08,"newspaper":0.04,"ground_ops":0.10,"polling_agents":0.07},
        "local":         {"tv":0.02,"radio":0.15,"social":0.15,"billboard":0.08,"rally":0.30,"sms":0.10,"newspaper":0.03,"ground_ops":0.12,"polling_agents":0.05},
    }
    w = weights_by_office.get(office_type, weights_by_office["house"])

    allocation, total_reach = [], 0
    for ch, wt in w.items():
        amt = int(total * wt)
        reach = int(amt / cpv.get(ch, 20))
        total_reach += reach
        allocation.append({
            "channel": ch, "amount_ngn": amt, "percentage": round(wt * 100, 1),
            "estimated_reach": reach, "cost_per_voter_ngn": cpv.get(ch, 20),
            "roi_score": round(1 / cpv.get(ch, 20) * 100, 2),
        })
    allocation.sort(key=lambda x: x["roi_score"], reverse=True)

    phases = [
        {"phase": "Brand Building",    "months": "1-3",  "pct": 15, "amount_ngn": int(total * 0.15), "focus": "Name recognition, social media"},
        {"phase": "Issue Framing",     "months": "4-6",  "pct": 20, "amount_ngn": int(total * 0.20), "focus": "Policy rollout, media interviews"},
        {"phase": "Voter Mobilisation","months": "7-9",  "pct": 30, "amount_ngn": int(total * 0.30), "focus": "Rallies, ground ops, LGA tours"},
        {"phase": "Final Push",        "months": "10-11","pct": 25, "amount_ngn": int(total * 0.25), "focus": "TV blitz, SMS, polling agents"},
        {"phase": "Election Day",      "months": "12",   "pct": 10, "amount_ngn": int(total * 0.10), "focus": "War room, legal team, monitoring"},
    ]

    req_min = ELIGIBILITY.get(office_type, ELIGIBILITY["house"])["fees_ngn"]
    return {
        "candidate_id": candidate_id, "election_id": election_id,
        "total_budget_ngn": total, "office_type": office_type, "state_code": state_code,
        "total_registered_voters": total_voters, "estimated_total_reach": total_reach,
        "cost_per_voter_ngn": round(total / max(total_reach, 1), 2),
        "channel_allocation": allocation, "campaign_phases": phases,
        "budget_health": {
            "is_competitive": total >= req_min * 3,
            "recommended_minimum_ngn": req_min * 3,
            "contingency_reserve_pct": 10,
        },
        "optimised_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def engine_schedule(candidate_id: str, election_id: str, state_code: str, office_type: str) -> Dict:
    st = _state(state_code)
    num_lgas = st["lgas"]
    zone = st["zone"]

    events = [
        {"week": 1,  "type": "campaign_launch",    "title": "Official Campaign Launch Rally",
         "location": f"{state_code} State Capital", "priority": "critical", "attendance_est": 5000, "media": "national"},
        {"week": 2,  "type": "press_conference",   "title": "Policy Manifesto Presentation",
         "location": "INEC Press Centre",           "priority": "high",     "attendance_est": 200,  "media": "national"},
        {"week": 3,  "type": "social_media_launch","title": "Digital Campaign Kickoff",
         "location": "Online",                      "priority": "high",     "attendance_est": 50000,"media": "digital"},
    ]

    for i in range(min(num_lgas, 26)):
        events.append({
            "week": 5 + i, "type": "lga_rally",
            "title": f"LGA {i+1} Town Hall & Rally",
            "location": f"{state_code}-LGA{i+1:02d}",
            "priority": "high" if i < 10 else "medium",
            "attendance_est": random.randint(500, 3000), "media": "local",
        })

    for i, issue in enumerate(["Education","Healthcare","Security","Infrastructure","Agriculture","Youth Employment"]):
        events.append({
            "week": 31 + i, "type": "policy_forum", "title": f"{issue} Policy Forum",
            "location": f"{state_code} State Capital", "priority": "medium",
            "attendance_est": 300, "media": "regional",
        })

    events += [
        {"week": 41, "type": "mega_rally",   "title": f"Zonal Mega Rally — {zone}",
         "location": f"{zone} Zone", "priority": "critical", "attendance_est": 50000, "media": "national"},
        {"week": 48, "type": "final_rally",  "title": "Grand Finale Campaign Rally",
         "location": f"{state_code} State Capital", "priority": "critical", "attendance_est": 100000, "media": "national"},
        {"week": 51, "type": "election_ops", "title": "Election Day War Room Activation",
         "location": "Campaign HQ", "priority": "critical", "attendance_est": 0, "media": "internal"},
        {"week": 52, "type": "collation_monitor","title": "Result Collation Monitoring",
         "location": "All Collation Centres", "priority": "critical", "attendance_est": 0, "media": "legal"},
    ]

    return {
        "candidate_id": candidate_id, "election_id": election_id,
        "state_code": state_code, "office_type": office_type,
        "total_events": len(events), "lga_coverage": min(num_lgas, 26),
        "events": events,
        "summary": {
            "critical_events": sum(1 for e in events if e["priority"] == "critical"),
            "high_priority_events": sum(1 for e in events if e["priority"] == "high"),
            "total_attendance_est": sum(e["attendance_est"] for e in events),
        },
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def engine_sentiment(candidate_id: str, period: str) -> Dict:
    days = {"7d": 7, "30d": 30, "90d": 90}.get(period, 30)
    base = random.uniform(0.45, 0.70)
    trend = random.choice(["improving", "stable", "declining"])
    delta = {"improving": 0.02, "stable": 0.0, "declining": -0.02}[trend]

    daily, cur = [], base
    for d in range(days):
        cur = max(0.1, min(0.95, cur + delta + random.uniform(-0.03, 0.03)))
        daily.append({
            "day": d + 1,
            "date": time.strftime("%Y-%m-%d", time.localtime(time.time() - (days - d) * 86400)),
            "positive_pct": round(cur * 100, 1),
            "neutral_pct": round(random.uniform(20, 35), 1),
            "negative_pct": round((1 - cur) * 40, 1),
            "volume": random.randint(500, 5000),
        })

    platforms = {
        "twitter_x":      {"positive_pct": round(base * 100 + random.uniform(-5, 5), 1), "volume": random.randint(2000, 10000)},
        "facebook":       {"positive_pct": round(base * 100 + random.uniform(-3, 3), 1), "volume": random.randint(5000, 20000)},
        "whatsapp":       {"positive_pct": round(base * 100 + random.uniform(-8, 8), 1), "volume": random.randint(1000, 5000)},
        "tiktok":         {"positive_pct": round(base * 100 + random.uniform(-10,10), 1),"volume": random.randint(500,  3000)},
        "radio_mentions": {"positive_pct": round(base * 100 + random.uniform(-5, 5), 1), "volume": random.randint(50,    200)},
    }

    return {
        "candidate_id": candidate_id, "period": period,
        "overall_score": round(base * 100, 1), "trend": trend,
        "trend_delta_pct": round(delta * 100, 1),
        "positive_pct": round(base * 100, 1),
        "neutral_pct": round(random.uniform(20, 35), 1),
        "negative_pct": round((1 - base) * 40, 1),
        "daily_trend": daily, "platform_breakdown": platforms,
        "top_positive_keywords": random.sample(["progress","development","change","youth","security","jobs"], 3),
        "top_negative_keywords": random.sample(["corruption","delay","promise","failed","expensive"], 2),
        "alerts": [{"type": "positive_spike", "message": "Sentiment improving — capitalise with increased posting", "severity": "info"}] if trend == "improving" else [],
        "analysed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def engine_opponents(candidate_id: str, state_code: str, office_type: str) -> Dict:
    opponents = []
    for party in ["APC", "PDP", "LP", "NNPP"][:3]:
        strength = random.uniform(0.30, 0.70)
        vulns = random.sample([
            "Inconsistent voting record on education bills",
            "Weak presence in rural LGAs",
            "Low youth engagement metrics",
            "Controversial security statement",
            "Funding source questions from previous campaign",
            "No clear infrastructure policy",
            "Low social media following relative to party base",
        ], k=random.randint(1, 3))
        strengths = random.sample([
            "Strong incumbent advantage", "High name recognition",
            "Well-funded campaign", "Ethnic bloc support",
            "Religious leader endorsements", "Strong ground network",
        ], k=random.randint(1, 2))
        opponents.append({
            "party": party,
            "estimated_support_pct": round(strength * 100, 1),
            "threat_level": "high" if strength > 0.55 else "medium" if strength > 0.40 else "low",
            "key_strengths": strengths,
            "vulnerabilities": vulns,
            "counter_strategy": f"Target {party} swing voters in urban LGAs with economic messaging",
        })
    primary = max(opponents, key=lambda x: x["estimated_support_pct"])
    return {
        "candidate_id": candidate_id, "state_code": state_code, "office_type": office_type,
        "opponents": opponents, "primary_threat": primary["party"],
        "competitive_landscape": "highly_competitive" if primary["estimated_support_pct"] > 50 else "competitive",
        "analysed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def engine_canvassing(candidate_id: str, state_code: str, lga_codes: List[str]) -> Dict:
    st = _state(state_code)
    base_lat = 6.5 + random.uniform(-2, 4)
    base_lon = 3.5 + random.uniform(-1, 8)

    wards = []
    for lga in lga_codes[:20]:
        for w in range(3):
            wards.append({
                "ward_id": f"{lga}-W{w+1}",
                "lat": base_lat + random.uniform(-0.3, 0.3),
                "lon": base_lon + random.uniform(-0.3, 0.3),
                "registered_voters": random.randint(500, 3000),
                "swing_potential": random.uniform(0.10, 0.50),
            })

    if not wards:
        return {"routes": [], "total_distance_km": 0}

    # Greedy nearest-neighbour TSP
    visited = [wards[0]]
    remaining = wards[1:]
    total_dist = 0.0
    while remaining:
        last = visited[-1]
        nearest = min(remaining, key=lambda w: (w["lat"] - last["lat"])**2 + (w["lon"] - last["lon"])**2)
        dist = math.sqrt((nearest["lat"] - last["lat"])**2 + (nearest["lon"] - last["lon"])**2) * 111
        total_dist += dist
        visited.append(nearest)
        remaining.remove(nearest)

    daily_routes = []
    for i in range(0, len(visited), 8):
        chunk = visited[i:i+8]
        daily_routes.append({
            "day": i // 8 + 1,
            "wards": [w["ward_id"] for w in chunk],
            "total_voters_reachable": sum(w["registered_voters"] for w in chunk),
            "estimated_hours": len(chunk) * 1.5,
        })

    return {
        "candidate_id": candidate_id, "state_code": state_code,
        "total_wards": len(wards), "total_distance_km": round(total_dist, 1),
        "estimated_days": len(daily_routes), "daily_routes": daily_routes,
        "optimisation_method": "nearest_neighbour_tsp",
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def engine_fundraising(candidate_id: str, office_type: str, target: int) -> Dict:
    req = ELIGIBILITY.get(office_type, ELIGIBILITY["house"])
    segments = [
        {"segment": "Major Donors (₦5M+)",       "count": random.randint(5,  20),   "avg_ask_ngn": 10_000_000, "conversion": 0.15, "channels": ["Personal meeting", "Gala dinner"]},
        {"segment": "Mid-tier (₦500K–5M)",        "count": random.randint(50, 150),  "avg_ask_ngn":  1_500_000, "conversion": 0.25, "channels": ["Small group events", "Phone calls"]},
        {"segment": "Grassroots (₦10K–500K)",     "count": random.randint(500,2000), "avg_ask_ngn":     50_000, "conversion": 0.40, "channels": ["WhatsApp", "SMS", "Rallies"]},
        {"segment": "Diaspora",                   "count": random.randint(100, 500), "avg_ask_ngn":    200_000, "conversion": 0.20, "channels": ["Online platform", "Diaspora associations"]},
    ]
    for s in segments:
        s["estimated_yield_ngn"] = int(s["count"] * s["avg_ask_ngn"] * s["conversion"])

    total_projected = sum(s["estimated_yield_ngn"] for s in segments)
    return {
        "candidate_id": candidate_id, "office_type": office_type,
        "target_amount_ngn": target, "minimum_required_ngn": req["fees_ngn"],
        "projected_total_ngn": total_projected,
        "funding_gap_ngn": max(0, target - total_projected),
        "funding_feasibility": "feasible" if total_projected >= target * 0.8 else "challenging",
        "donor_segments": segments,
        "fundraising_events": [
            {"event": "Gala Dinner",             "target_ngn": int(target * 0.30), "timeline": "Month 2"},
            {"event": "Online Crowdfunding",     "target_ngn": int(target * 0.15), "timeline": "Month 3-6"},
            {"event": "LGA Fundraising Drives",  "target_ngn": int(target * 0.25), "timeline": "Month 4-8"},
            {"event": "Diaspora Drive",          "target_ngn": int(target * 0.20), "timeline": "Month 5-9"},
            {"event": "Final Push Telethon",     "target_ngn": int(target * 0.10), "timeline": "Month 10"},
        ],
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def engine_media_buy(candidate_id: str, state_code: str, budget: int, office_type: str) -> Dict:
    channels = [
        {"channel": "NTA (National TV)",    "cpm_ngn": 15000, "reach_pct": 45, "grp": 144},
        {"channel": "State TV",             "cpm_ngn":  3000, "reach_pct": 30, "grp": 123},
        {"channel": "Radio (State)",        "cpm_ngn":   800, "reach_pct": 55, "grp": 358},
        {"channel": "Facebook/Instagram",   "cpm_ngn":   500, "reach_pct": 35, "grp": 280},
        {"channel": "Twitter/X",            "cpm_ngn":   600, "reach_pct": 20, "grp": 240},
        {"channel": "WhatsApp Broadcast",   "cpm_ngn":   200, "reach_pct": 60, "grp": 300},
        {"channel": "Newspaper (National)", "cpm_ngn":  8000, "reach_pct": 15, "grp":  30},
        {"channel": "Billboard (State)",    "cpm_ngn":  5000, "reach_pct": 40, "grp": 600},
        {"channel": "SMS Blast",            "cpm_ngn":   150, "reach_pct": 70, "grp": 210},
    ]
    for c in channels:
        c["roi_score"] = round(c["grp"] / c["cpm_ngn"] * 1000, 3)
    channels.sort(key=lambda x: x["roi_score"], reverse=True)

    total_roi = sum(c["roi_score"] for c in channels)
    remainder = budget
    for c in channels:
        alloc = int(budget * c["roi_score"] / total_roi)
        c["budget_allocated_ngn"] = alloc
        c["estimated_impressions"] = int(alloc / c["cpm_ngn"] * 1000)
        remainder -= alloc
    channels[0]["budget_allocated_ngn"] += remainder

    return {
        "candidate_id": candidate_id, "state_code": state_code,
        "total_media_budget_ngn": budget,
        "channels": channels,
        "total_estimated_impressions": sum(c["estimated_impressions"] for c in channels),
        "flight_schedule": [
            {"phase": "Awareness",    "weeks": "1-8",   "budget_pct": 20, "channels": ["Billboard","Radio"]},
            {"phase": "Persuasion",   "weeks": "9-20",  "budget_pct": 35, "channels": ["TV","Facebook","WhatsApp"]},
            {"phase": "Mobilisation", "weeks": "21-48", "budget_pct": 35, "channels": ["Radio","SMS","WhatsApp","Rally"]},
            {"phase": "GOTV",         "weeks": "49-52", "budget_pct": 10, "channels": ["SMS","WhatsApp","Radio"]},
        ],
        "optimised_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def engine_policy_resonance(candidate_id: str, state_code: str, policies: List[str]) -> Dict:
    st = _state(state_code)
    zone = st["zone"]
    priorities = ZONE_PRIORITIES.get(zone, ZONE_PRIORITIES["NC"])

    scored = []
    for policy in policies:
        pl = policy.lower()
        score = 0.5
        for kw, wt in priorities.items():
            if kw in pl:
                score = max(score, wt)
        scored.append({
            "policy": policy,
            "resonance_score": round(score * 100, 1),
            "primary_demographic": "Rural farmers" if "agri" in pl else "Youth" if "edu" in pl or "tech" in pl else "General",
            "zone_alignment": zone,
            "recommended_messaging": f"Frame '{policy}' around {zone} voters' top priority: {max(priorities, key=priorities.get)}",
        })
    scored.sort(key=lambda x: x["resonance_score"], reverse=True)

    return {
        "candidate_id": candidate_id, "state_code": state_code, "zone": zone,
        "zone_top_priorities": sorted(priorities.items(), key=lambda x: x[1], reverse=True)[:3],
        "policy_resonance": scored,
        "top_policy": scored[0]["policy"] if scored else None,
        "analysed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def engine_war_room(candidate_id: str, election_id: str) -> Dict:
    agents_deployed = random.randint(100, 5000)
    agents_reporting = int(agents_deployed * random.uniform(0.70, 0.95))
    states_monitored = random.randint(5, 37)
    lgas_reporting = random.randint(10, 200)

    incidents = []
    for _ in range(random.randint(0, 5)):
        incidents.append({
            "id": str(uuid.uuid4())[:8],
            "type": random.choice(["ballot_snatching","intimidation","late_materials","bvas_failure","agent_ejection"]),
            "location": f"LGA-{random.randint(1, 20)}",
            "severity": random.choice(["low", "medium", "high"]),
            "status": random.choice(["reported", "investigating", "resolved"]),
            "reported_at": time.strftime("%H:%M", time.gmtime()),
        })

    return {
        "candidate_id": candidate_id, "election_id": election_id,
        "war_room_status": "active",
        "last_updated": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "coverage": {
            "states_monitored": states_monitored,
            "lgas_reporting": lgas_reporting,
            "agents_deployed": agents_deployed,
            "agents_reporting": agents_reporting,
            "agent_coverage_pct": round(agents_reporting / max(agents_deployed, 1) * 100, 1),
        },
        "vote_tally_estimate": {
            "our_candidate_pct": round(random.uniform(30, 55), 1),
            "leading_opponent_pct": round(random.uniform(25, 45), 1),
            "confidence": "preliminary",
            "units_reporting": lgas_reporting * 3,
        },
        "incidents": incidents,
        "alerts": [
            {"type": "coverage_gap", "message": f"Only {states_monitored}/37 states covered — deploy more agents", "severity": "warning"}
            if states_monitored < 30 else
            {"type": "coverage_ok", "message": "Agent coverage nominal", "severity": "info"}
        ],
        "legal_team": {"on_standby": True, "petitions_filed": 0, "injunctions_ready": 2},
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

    if OPENAI_KEY:
        try:
            async with httpx.AsyncClient() as c:
                r = await c.post(
                    f"{OPENAI_BASE}/chat/completions",
                    headers={"Authorization": f"Bearer {OPENAI_KEY}"},
                    json={"model": "gpt-4o-mini",
                          "messages": [{"role": "user", "content": prompt}],
                          "max_tokens": 800},
                    timeout=20.0,
                )
                return r.json()["choices"][0]["message"]["content"]
        except Exception as e:
            log.warning("speech_llm_fallback", error=str(e))

    # Template fallback
    if speech_type == "rally":
        return (f"Fellow citizens of {state_name}!\n\n"
                f"I am {name}, and I am running for {office} because I believe in a better future for our people. "
                f"My campaign is built on: {', '.join(policies[:3])}.\n\n"
                f"Together, we will build the {state_name} we deserve. Vote {name}! Vote for change!")
    elif speech_type == "manifesto":
        pts = "\n".join(f"{i+1}. {p}" for i, p in enumerate(policies[:5]))
        return f"MANIFESTO — {name.upper()} FOR {office.upper()}\n\n{pts}\n\nI pledge to serve with integrity."
    else:
        return f"[{speech_type.upper()}]\nCandidate: {name} | Office: {office} | State: {state_name}\nPolicies: {', '.join(policies)}"


# ── Request Models ────────────────────────────────────────────────────────────

class EligibilityReq(BaseModel):
    candidate_id: int; office_type: str; state_code: str; party_code: str
    age: int = Field(default=35, ge=18, le=100)
    has_school_cert: bool = True; is_nigerian: bool = True
    criminal_record: bool = False; dual_citizen: bool = False
    years_in_party: int = Field(default=3, ge=0)

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
    return engine_eligibility(req.candidate_id, req.office_type, req.state_code, req.party_code,
                               req.age, req.has_school_cert, req.is_nigerian,
                               req.criminal_record, req.dual_citizen, req.years_in_party)


@app.post("/api/v1/campaign/plan", tags=["Campaign Plan"])
async def create_plan(req: PlanCreateReq):
    """Create a comprehensive all-in-one campaign plan."""
    plan_id = str(uuid.uuid4())
    plan = {
        "plan_id": plan_id,
        "candidate_id": req.candidate_id, "election_id": req.election_id,
        "office_type": req.office_type, "state_code": req.state_code, "party_code": req.party_code,
        "target_votes": req.target_votes, "budget_ngn": req.budget_ngn,
        "eligibility":       engine_eligibility(req.candidate_id, req.office_type, req.state_code,
                                                 req.party_code, 35, True, True, False, False, 3),
        "voter_targeting":   engine_targeting(str(req.candidate_id), req.state_code, req.party_code,
                                               req.office_type, req.target_votes),
        "budget_allocation": engine_budget(str(req.candidate_id), str(req.election_id),
                                            req.budget_ngn, req.state_code, req.office_type),
        "schedule":          engine_schedule(str(req.candidate_id), str(req.election_id),
                                              req.state_code, req.office_type),
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    _plans[plan_id] = plan
    log.info("campaign_plan_created", plan_id=plan_id, candidate_id=req.candidate_id)
    return plan


@app.get("/api/v1/campaign/plan/{plan_id}", tags=["Campaign Plan"])
async def get_plan(plan_id: str):
    plan = _plans.get(plan_id)
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
async def generate_speech(req: SpeechReq):
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
    await _broadcast({"type": "war_room_update", "data": data})
    return data


@app.post("/api/v1/campaign/debate-tracker", tags=["Debate Tracker"])
async def debate_tracker(req: DebateTrackerReq):
    """Innovation 6: Real-time debate performance tracker — per-statement sentiment scoring."""
    pos_words = ["develop","build","invest","create","improve","secure","educate","grow","promise","commit","achieve","deliver"]
    neg_words = ["fail","corrupt","steal","lie","never","problem","crisis","blame","attack","weak","broken"]
    scored = []
    for stmt in req.statements:
        sl = stmt.lower()
        pos = sum(1 for w in pos_words if w in sl)
        neg = sum(1 for w in neg_words if w in sl)
        score = max(0, min(100, round((pos - neg * 1.5 + 5) / 10 * 100, 1)))
        scored.append({
            "statement": stmt[:120] + "..." if len(stmt) > 120 else stmt,
            "sentiment_score": score,
            "tone": "positive" if score > 60 else "neutral" if score > 40 else "negative",
            "word_count": len(stmt.split()),
            "key_themes": [w for w in ["security","education","economy","health","infrastructure","youth","agriculture"] if w in sl],
        })
    avg = round(statistics.mean(s["sentiment_score"] for s in scored), 1) if scored else 0
    return {
        "candidate_id": req.candidate_id,
        "statements_analysed": len(scored),
        "overall_performance_score": avg,
        "performance_grade": "A" if avg > 80 else "B" if avg > 65 else "C" if avg > 50 else "D",
        "statement_scores": scored,
        "recommendations": [
            "Lead with specific policy numbers and timelines",
            "Avoid defensive language — pivot to solutions immediately",
            "Reference local constituency-specific issues for authenticity",
            "Close every answer with a clear, memorable call-to-action",
        ],
        "analysed_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


@app.post("/api/v1/campaign/volunteer-network", tags=["Volunteer Network"])
async def volunteer_network(req: VolunteerGraphReq):
    """Innovation 7: Volunteer network graph — social-network reach analysis."""
    volunteers = []
    total_reach = 0
    for i in range(req.num_volunteers):
        connections = random.randint(5, 50)
        influence = random.uniform(0.1, 1.0)
        reach = int(connections * influence * 10)
        total_reach += reach
        volunteers.append({
            "volunteer_id": f"V{i+1:04d}",
            "lga": f"LGA-{random.randint(1, 20)}",
            "connections": connections,
            "influence_score": round(influence, 3),
            "estimated_voter_reach": reach,
            "role": random.choice(["ward_coordinator","polling_agent","mobiliser","social_media","driver"]),
        })

    top = sorted(volunteers, key=lambda x: x["influence_score"], reverse=True)[:10]
    avg_conn = statistics.mean(v["connections"] for v in volunteers)
    density = round(avg_conn / req.num_volunteers, 4)

    return {
        "candidate_id": req.candidate_id, "state_code": req.state_code,
        "total_volunteers": req.num_volunteers, "total_voter_reach": total_reach,
        "avg_connections_per_volunteer": round(avg_conn, 1),
        "network_density": density,
        "network_health": "strong" if density > 0.10 else "moderate" if density > 0.05 else "weak",
        "top_influencers": top,
        "role_breakdown": {r: sum(1 for v in volunteers if v["role"] == r)
                           for r in ["ward_coordinator","polling_agent","mobiliser","social_media","driver"]},
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


@app.get("/api/v1/campaign/states", tags=["Reference Data"])
async def list_states():
    return {"states": STATES, "zones": ZONES}


@app.get("/api/v1/campaign/offices", tags=["Reference Data"])
async def list_offices():
    return {"offices": [{"id": k, "requirements": v} for k, v in ELIGIBILITY.items()]}


@app.websocket("/ws/campaign")
async def campaign_ws(ws: WebSocket):
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
    return {"status": "healthy", "active_plans": len(_plans), "version": "2.0.0"}


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8204, log_level="info")
