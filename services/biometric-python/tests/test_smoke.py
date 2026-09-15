"""Smoke tests: fail-closed auth, key rotation, and a real health probe."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("biometric_service", SERVICE_DIR / "service.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)


@pytest.fixture()
def client():
    # No lifespan: startup requires a live inference engine + PostgreSQL, which
    # are deployment dependencies, not unit-test ones.
    return TestClient(service.app)


def test_protected_endpoint_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "BIOMETRIC_API_KEYS", [])
    resp = client.get("/metrics")
    assert resp.status_code == 503


def test_protected_endpoint_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setattr(service, "BIOMETRIC_API_KEYS", ["primary-key", "rotated-key"])
    assert client.get("/metrics").status_code == 401
    assert (
        client.get("/metrics", headers={"Authorization": "Bearer wrong-key"}).status_code
        == 401
    )


def test_rotated_second_key_is_accepted(client, monkeypatch):
    """Key rotation: any comma-separated key authenticates (constant-time)."""
    monkeypatch.setattr(service, "BIOMETRIC_API_KEYS", ["primary-key", "rotated-key"])
    resp = client.get("/metrics", headers={"Authorization": "Bearer rotated-key"})
    assert resp.status_code == 200


def test_health_returns_real_probe_structure(client):
    resp = client.get("/health")
    assert resp.status_code in (200, 503)
    body = resp.json()
    assert body["status"] in {"healthy", "degraded"}
    assert isinstance(body["checks"], dict)
    assert body["persistence"] == "postgresql"
