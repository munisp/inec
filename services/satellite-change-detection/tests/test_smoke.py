"""Smoke tests: fail-closed auth, key rotation, and a fail-closed health probe."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("satellite_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)


@pytest.fixture()
def client():
    return TestClient(service.app)


def _body():
    return {
        "polling_unit_id": "PU-1", "registered_lat": 6.5, "registered_lon": 3.3,
        "election_date": "2027-02-20",
    }


def test_analyze_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "SATELLITE_API_KEYS", [])
    resp = client.post("/api/v1/satellite/validate-polling-unit", json=_body())
    assert resp.status_code == 503


def test_analyze_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setattr(service, "SATELLITE_API_KEYS", ["sat-key-1", "sat-key-2"])
    url = "/api/v1/satellite/validate-polling-unit"
    assert client.post(url, json=_body()).status_code == 401
    assert (
        client.post(url, json=_body(), headers={"x-api-key": "nope"}).status_code == 401
    )


def test_rotated_key_passes_auth(client, monkeypatch):
    """Second (rotated) key authenticates; request then fails closed on STAC config."""
    monkeypatch.setattr(service, "SATELLITE_API_KEYS", ["sat-key-1", "sat-key-2"])
    resp = client.post(
        "/api/v1/satellite/validate-polling-unit", json=_body(),
        headers={"Authorization": "Bearer sat-key-2"},
    )
    # STAC_* env unset in test env → fail-closed 503, NOT 401/403.
    assert resp.status_code == 503


def test_health_fails_closed_without_stac_config(client, monkeypatch):
    for var in ("STAC_API_URL", "STAC_COLLECTION", "STAC_PREVIEW_ASSET", "STAC_MAX_CLOUD_COVER"):
        monkeypatch.delenv(var, raising=False)
    resp = client.get("/api/v1/satellite/health")
    assert resp.status_code == 503
    assert "detail" in resp.json()
