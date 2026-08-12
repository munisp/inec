"""Smoke tests: fail-closed auth, key rotation, and a real health probe."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("predictive_alloc_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)


@pytest.fixture()
def client():
    # NOTE: no context manager → startup (which requires DATABASE_URL etc.)
    # is intentionally not run; we exercise middleware + health only.
    return TestClient(service.app)


def _body():
    return {
        "election_id": "E1",
        "polling_units": [{
            "polling_unit_id": "PU-1", "state": "LA", "lga": "IKEJA",
            "registered_voters": 1000, "prev_turnout": 0.5,
            "prior_vote_to_accredited_ratio": 0.9, "prior_rejection_ratio": 0.02,
            "prior_submission_hour_fraction": 0.7, "lat": 6.6, "lon": 3.3,
        }],
        "depot_lat": 6.5, "depot_lon": 3.4, "num_vehicles": 2,
    }


def test_predict_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "PREDICTIVE_ALLOC_API_KEYS", [])
    assert client.post("/api/v1/allocation/predict", json=_body()).status_code == 503


def test_predict_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setattr(service, "PREDICTIVE_ALLOC_API_KEYS", ["alloc-1", "alloc-2"])
    url = "/api/v1/allocation/predict"
    assert client.post(url, json=_body()).status_code == 401
    assert client.post(url, json=_body(), headers={"x-api-key": "bad"}).status_code == 401


def test_rotated_key_passes_auth(client, monkeypatch):
    """Second (rotated) key authenticates; model layer then fails closed (untrained)."""
    monkeypatch.setattr(service, "PREDICTIVE_ALLOC_API_KEYS", ["alloc-1", "alloc-2"])
    resp = client.post(
        "/api/v1/allocation/predict", json=_body(),
        headers={"Authorization": "Bearer alloc-2"},
    )
    # Model not trained in test env → fail-closed 503, not 401.
    assert resp.status_code == 503
    assert "not trained" in resp.json()["detail"]


def test_health_returns_real_probe_structure(client):
    resp = client.get("/api/v1/allocation/health")
    assert resp.status_code in (200, 503)
    body = resp.json()
    assert body["status"] in {"healthy", "model_untrained", "degraded"}
    assert "model_trained" in body
    assert body["requires_real_historical_data"] is True
