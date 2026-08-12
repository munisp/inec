"""Smoke tests: fail-closed auth, key rotation, and a real health probe."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("digital_twin_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)


@pytest.fixture()
def client():
    return TestClient(service.app)


def _create_body():
    return {
        "election_id": "E2E-1", "scenario": "baseline",
        "num_states": 1, "pus_per_state": 1, "realtime": False,
    }


def test_create_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "DIGITAL_TWIN_API_KEYS", [])
    assert client.post("/api/v1/twin/create", json=_create_body()).status_code == 503


def test_create_rejects_missing_key(client, monkeypatch):
    monkeypatch.setattr(service, "DIGITAL_TWIN_API_KEYS", ["k1", "k2"])
    assert client.post("/api/v1/twin/create", json=_create_body()).status_code == 401


def test_rotated_key_accepted_and_twin_created(client, monkeypatch):
    monkeypatch.setattr(service, "DIGITAL_TWIN_API_KEYS", ["k1", "k2"])
    resp = client.post(
        "/api/v1/twin/create", json=_create_body(),
        headers={"x-api-key": "k2"},
    )
    assert resp.status_code == 200
    assert resp.json()["total_units"] == 1
    # cleanup in-memory state
    service._sims.pop("E2E-1", None)


def test_health_returns_real_probe_structure(client):
    resp = client.get("/api/v1/twin/health")
    assert resp.status_code in (200, 503)
    body = resp.json()
    assert body["status"] in {"healthy", "degraded"}
    assert isinstance(body["checks"], dict)
    assert body["persistence"] in {"postgresql", "in_memory"}
    assert "active_simulations" in body
