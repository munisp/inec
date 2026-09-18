"""Engine wiring tests: real computation, honest fail-closed behaviour, no orphan code.

These tests run without Postgres (_pg_pool is None), which exercises two
honesty paths:
  * engines that compute from caller inputs + in-file reference data must
    return real results with the appropriate data-quality labels;
  * engines that require DB-backed tables must fail closed with 503 and a
    precise required_data descriptor — never fabricated rows.
"""

import importlib.util
import re
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("campaign_planning_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service
_spec.loader.exec_module(service)

KEY = "engine-test-key"
AUTH = {"Authorization": f"Bearer {KEY}"}


@pytest.fixture(autouse=True)
def _auth(monkeypatch):
    monkeypatch.setattr(service, "CAMPAIGN_API_KEYS", [KEY])
    monkeypatch.setattr(service, "_pg_pool", None)
    service._table_presence.clear()
    yield


@pytest.fixture()
def client():
    return TestClient(service.app)


# ── Budget (Electoral Act 2022 §88 caps + exact allocation) ─────────────────

def test_budget_allocation_sums_exactly(client):
    resp = client.post("/api/v1/campaign/budget", headers=AUTH, json={
        "candidate_id": "1", "election_id": "1", "total_budget": 10_000_003,
        "state_code": "LA", "office_type": "house",
    })
    assert resp.status_code == 200
    body = resp.json()
    assert sum(body["allocation_ngn"].values()) == 10_000_003
    assert body["statutory_cap"]["cap_ngn"] == 70_000_000  # EA 2022 s.88(5)
    assert body["statutory_cap"]["compliant"] is True


def test_budget_rejects_over_statutory_cap(client):
    # Presidential cap is ₦5,000,000,000 (EA 2022 s.88(2)).
    resp = client.post("/api/v1/campaign/budget", headers=AUTH, json={
        "candidate_id": "1", "election_id": "1", "total_budget": 6_000_000_000,
        "state_code": "LA", "office_type": "presidential",
    })
    assert resp.status_code == 422
    assert resp.json()["detail"]["cap_ngn"] == 5_000_000_000


def test_budget_unknown_office_and_nonpositive_total(client):
    assert client.post("/api/v1/campaign/budget", headers=AUTH, json={
        "candidate_id": "1", "election_id": "1", "total_budget": 1000,
        "state_code": "LA", "office_type": "dogcatcher",
    }).status_code == 400
    assert client.post("/api/v1/campaign/budget", headers=AUTH, json={
        "candidate_id": "1", "election_id": "1", "total_budget": 0,
        "state_code": "LA", "office_type": "house",
    }).status_code == 400


# ── Schedule (caller-anchored, never fabricated election date) ───────────────

def test_schedule_requires_real_election_date(client):
    assert client.post("/api/v1/campaign/schedule", headers=AUTH, json={
        "candidate_id": "1", "election_id": "1", "state_code": "LA", "office_type": "house",
    }).status_code == 422  # pydantic: required field
    assert client.post("/api/v1/campaign/schedule", headers=AUTH, json={
        "candidate_id": "1", "election_id": "1", "state_code": "LA", "office_type": "house",
        "election_date": "next-year-sometime",
    }).status_code == 400


def test_schedule_milestones_anchored_to_supplied_date(client):
    resp = client.post("/api/v1/campaign/schedule", headers=AUTH, json={
        "candidate_id": "1", "election_id": "1", "state_code": "LA", "office_type": "house",
        "election_date": "2027-02-20",
    })
    assert resp.status_code == 200
    ms = resp.json()["milestones"]
    assert ms[-1]["date"] == "2027-02-20" and ms[-1]["days_before_election"] == 0
    offsets = [m["days_before_election"] for m in ms]
    assert offsets == sorted(offsets, reverse=True)


# ── Targeting (editorial fallback is labeled; sums exactly) ──────────────────

def test_targeting_editorial_fallback_is_labeled_and_exact(client):
    resp = client.post("/api/v1/campaign/targeting", headers=AUTH, json={
        "candidate_id": "1", "state_code": "LA", "party_code": "APC",
        "office_type": "house", "target_votes": 100_001,
    })
    assert resp.status_code == 200
    body = resp.json()
    assert body["data_quality"] == "reference_estimates_unverified"
    assert sum(t["target_votes"] for t in body["lga_targets"]) == 100_001
    assert body["lga_count"] == 20  # Lagos LGA count from reference data


def test_targeting_rejects_unknown_state_and_zero_target(client):
    assert client.post("/api/v1/campaign/targeting", headers=AUTH, json={
        "candidate_id": "1", "state_code": "XX", "party_code": "APC",
        "office_type": "house", "target_votes": 1000,
    }).status_code == 400
    assert client.post("/api/v1/campaign/targeting", headers=AUTH, json={
        "candidate_id": "1", "state_code": "LA", "party_code": "APC",
        "office_type": "house", "target_votes": 0,
    }).status_code == 400


# ── Media buy (money arithmetic only; no invented reach figures) ─────────────

def test_media_buy_split_sums_exactly_with_cap_context(client):
    resp = client.post("/api/v1/campaign/media-buy", headers=AUTH, json={
        "candidate_id": "1", "state_code": "KN", "budget": 5_000_001, "office_type": "senatorial",
    })
    assert resp.status_code == 200
    body = resp.json()
    assert sum(body["channel_split_ngn"].values()) == 5_000_001
    assert body["planning_model"] is True
    assert body["statutory_cap_context"]["cap_ngn"] == 100_000_000  # EA 2022 s.88(4)
    assert "GRP" in body["note"]  # explicitly disclaimed, not asserted


# ── Policy resonance (editorial weights; unmapped → null, never invented) ────

def test_policy_resonance_scores_mapped_and_nulls_unmapped(client):
    resp = client.post("/api/v1/campaign/policy-resonance", headers=AUTH, json={
        "candidate_id": "1", "state_code": "KD",  # NW zone
        "policies": ["Security reform", "Free university education", "Untranslatable xyz"],
    })
    assert resp.status_code == 200
    body = resp.json()
    assert body["data_quality"] == "reference_estimates_unverified"
    by_policy = {p["policy"]: p for p in body["policies"]}
    assert by_policy["Security reform"]["resonance_score"] == 0.90  # NW security weight
    assert by_policy["Free university education"]["resonance_score"] == 0.70
    assert by_policy["Untranslatable xyz"]["resonance_score"] is None
    assert by_policy["Untranslatable xyz"]["unmapped"] is True


# ── DB-backed engines fail closed without the schema (no fabricated rows) ────

@pytest.mark.parametrize("path,payload", [
    ("/api/v1/campaign/canvassing-routes",
     {"candidate_id": "1", "state_code": "KN", "lga_codes": ["01"]}),
    ("/api/v1/campaign/fundraising",
     {"candidate_id": "1", "office_type": "house", "target_amount": 5_000_000}),
    ("/api/v1/campaign/opponents",
     {"candidate_id": "1", "state_code": "LA", "office_type": "house"}),
    ("/api/v1/campaign/volunteer-network",
     {"candidate_id": "1", "state_code": "LA", "num_volunteers": 50}),
])
def test_db_backed_engines_fail_closed_without_schema(client, path, payload):
    resp = client.post(path, headers=AUTH, json=payload)
    assert resp.status_code == 503
    detail = resp.json()["detail"]
    assert detail["status"] == "unavailable"
    assert "required_data" in detail


def test_stakeholders_fail_closed_without_schema(client):
    resp = client.post("/api/v1/campaign/stakeholders", headers=AUTH, json={
        "candidate_id": 1, "candidate_name": "Test", "state_code": "LA",
        "office_type": "house", "party_code": "APC",
    })
    assert resp.status_code == 503
    assert client.get("/api/v1/campaign/stakeholders/categories",
                      headers=AUTH, params={"candidate_id": 1}).status_code == 503


def test_war_room_honest_empty_without_schema(client):
    resp = client.post("/api/v1/campaign/war-room", headers=AUTH, json={
        "candidate_id": "99", "election_id": "1",
    })
    assert resp.status_code == 200
    body = resp.json()
    assert body["data_source"] == "none"
    assert body["open_count"] == 0
    assert "never" not in body  # sanity: dict shape, not prose


# ── Composed plan: real sections, persisted, retrievable ─────────────────────

def test_plan_compose_persist_and_retrieve(client):
    resp = client.post("/api/v1/campaign/plan", headers=AUTH, json={
        "candidate_id": 7, "election_id": 3, "office_type": "house",
        "state_code": "LA", "party_code": "APC", "target_votes": 40_000,
        "budget_ngn": 20_000_000, "election_date": "2027-02-20",
    })
    assert resp.status_code == 200
    plan = resp.json()
    assert plan["sections"]["budget"]["statutory_cap"]["compliant"] is True
    assert plan["sections"]["schedule"]["election_date"] == "2027-02-20"
    assert plan["sections"]["targeting"]["target_votes"] == 40_000
    got = client.get(f"/api/v1/campaign/plan/{plan['plan_id']}", headers=AUTH)
    assert got.status_code == 200
    assert got.json()["plan_id"] == plan["plan_id"]


def test_plan_requires_election_date_and_known_office(client):
    assert client.post("/api/v1/campaign/plan", headers=AUTH, json={
        "candidate_id": 7, "election_id": 3, "office_type": "house",
        "state_code": "LA", "party_code": "APC",
    }).status_code == 400  # schedule anchor missing
    assert client.post("/api/v1/campaign/plan", headers=AUTH, json={
        "candidate_id": 7, "election_id": 3, "office_type": "king",
        "state_code": "LA", "party_code": "APC", "election_date": "2027-02-20",
    }).status_code == 400


# ── TSP primitives (pure functions, synthetic coordinates) ───────────────────

def test_haversine_known_distance():
    # Lagos (6.5244, 3.3792) → Abuja (9.0765, 7.3986) ≈ 536 km great-circle.
    d = service._haversine_km(6.5244, 3.3792, 9.0765, 7.3986)
    assert 500 < d < 570


def test_nearest_neighbour_visits_all_and_starts_at_first():
    pts = [{"code": f"P{i}", "latitude": 12.0 + i * 0.01, "longitude": 8.5 + i * 0.01}
           for i in range(6)]
    route = service._nearest_neighbour_route(pts)
    assert [p["code"] for p in route][0] == "P0"
    assert sorted(p["code"] for p in route) == [f"P{i}" for i in range(6)]


# ── Genuinely external features stay honestly disabled ───────────────────────

@pytest.mark.parametrize("path,payload", [
    ("/api/v1/campaign/sentiment", {"candidate_id": "1", "period": "30d"}),
    ("/api/v1/campaign/debate-tracker", {"candidate_id": "1", "statements": ["x"]}),
])
def test_external_data_features_remain_disabled(client, path, payload):
    resp = client.post(path, headers=AUTH, json=payload)
    assert resp.status_code == 503
    assert resp.json()["detail"]["status"] == "disabled"


# ── No-orphan guarantees ─────────────────────────────────────────────────────

def test_disabled_registry_matches_exactly_the_two_external_features():
    assert sorted(service.DISABLED_DATA_FEATURES) == ["debate_tracker", "sentiment_analysis"]


def test_every_engine_is_wired_to_an_endpoint():
    """No orphan code: every engine_* defined must be invoked elsewhere."""
    src = (SERVICE_DIR / "main.py").read_text()
    defined = set(re.findall(r"(?:async )?def (engine_\w+)\(", src))
    assert defined, "expected engine functions to exist"
    for name in defined:
        calls = len(re.findall(rf"\b{name}\(", src))
        assert calls >= 2, f"{name} is defined but never invoked (orphan)"


def test_health_reports_wired_features(client):
    resp = client.get("/api/v1/campaign/health")
    body = resp.json()
    assert body["disabled_features"] == ["debate_tracker", "sentiment_analysis"]
    for f in ("campaign_plan", "budget_allocation", "voter_targeting", "canvassing_routes",
              "fundraising", "media_buy", "policy_resonance", "war_room",
              "opponent_analysis", "volunteer_network", "stakeholder_recommendations"):
        assert f in body["enabled_features"]
