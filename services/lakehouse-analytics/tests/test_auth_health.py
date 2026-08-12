"""Smoke tests: fail-closed auth, key rotation, and a real health probe."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("lakehouse_analytics_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)


@pytest.fixture()
def client():
    # No lifespan: startup opens DuckDB and syncs from the Go backend, which
    # are deployment dependencies, not unit-test ones.
    return TestClient(service.app)


def test_protected_endpoint_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "LAKEHOUSE_API_KEYS", [])
    resp = client.get("/ai/methods")
    assert resp.status_code == 503


def test_protected_endpoint_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setattr(service, "LAKEHOUSE_API_KEYS", ["primary-key", "rotated-key"])
    assert client.get("/ai/methods").status_code == 401
    assert (
        client.get("/ai/methods", headers={"Authorization": "Bearer wrong-key"}).status_code
        == 401
    )


def test_rotated_second_key_is_accepted(client, monkeypatch):
    """Key rotation: any comma-separated key authenticates (constant-time)."""
    monkeypatch.setattr(service, "LAKEHOUSE_API_KEYS", ["primary-key", "rotated-key"])
    resp = client.get("/ai/methods", headers={"Authorization": "Bearer rotated-key"})
    assert resp.status_code == 200
    assert isinstance(resp.json()["methods"], list)


def test_health_returns_real_probe_structure(client):
    resp = client.get("/health")
    assert resp.status_code == 200
    body = resp.json()
    assert body["status"] == "healthy"
    assert body["service"] == "lakehouse-analytics"
    assert isinstance(body["stats"], dict)
