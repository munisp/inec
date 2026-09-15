"""Smoke tests: fail-closed auth, key rotation, and a real health probe."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("gotv_analytics_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)


@pytest.fixture()
def client():
    return TestClient(service.app)


def test_protected_endpoint_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.delenv("GOTV_ANALYTICS_API_KEY", raising=False)
    resp = client.get("/gotv-analytics/middleware/status")
    assert resp.status_code == 503


def test_protected_endpoint_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setenv("GOTV_ANALYTICS_API_KEY", "primary-key,rotated-key")
    assert client.get("/gotv-analytics/middleware/status").status_code == 401
    assert (
        client.get(
            "/gotv-analytics/middleware/status",
            headers={"Authorization": "Bearer wrong-key"},
        ).status_code
        == 401
    )


def test_rotated_second_key_is_accepted(client, monkeypatch):
    """Key rotation: any comma-separated key authenticates (constant-time)."""
    monkeypatch.setenv("GOTV_ANALYTICS_API_KEY", "primary-key,rotated-key")
    resp = client.get(
        "/gotv-analytics/middleware/status",
        headers={"Authorization": "Bearer rotated-key"},
    )
    assert resp.status_code == 200
    assert resp.json()["service"] == "gotv-analytics"


def test_health_returns_real_probe_structure(client, monkeypatch):
    monkeypatch.delenv("DATABASE_URL", raising=False)
    resp = client.get("/health")
    assert resp.status_code in (200, 503)
    body = resp.json()
    assert body["status"] in {"healthy", "degraded"}
    assert isinstance(body["checks"], dict)
    assert "postgresql" in body["checks"]
