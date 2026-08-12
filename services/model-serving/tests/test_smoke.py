"""Smoke tests: fail-closed auth, key rotation, and a real health probe."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location(
    "model_serving_serve_http", SERVICE_DIR / "serve_http.py"
)
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)


@pytest.fixture()
def client():
    # No context manager → lifespan (model loading) is not run; we exercise
    # middleware + the health probe only.
    return TestClient(service.app)


def test_models_endpoint_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "MODEL_SERVING_API_KEYS", [])
    assert client.get("/v1/models").status_code == 503


def test_models_endpoint_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setattr(service, "MODEL_SERVING_API_KEYS", ["ms-key-1", "ms-key-2"])
    assert client.get("/v1/models").status_code == 401
    assert client.get("/v1/models", headers={"x-api-key": "bad"}).status_code == 401


def test_rotated_key_accepted(client, monkeypatch):
    monkeypatch.setattr(service, "MODEL_SERVING_API_KEYS", ["ms-key-1", "ms-key-2"])
    resp = client.get("/v1/models", headers={"Authorization": "Bearer ms-key-2"})
    assert resp.status_code == 200
    assert "models" in resp.json()


def test_healthz_returns_real_probe_structure(client):
    resp = client.get("/healthz")
    assert resp.status_code in (200, 503)
    body = resp.json()
    assert body["status"] in {"healthy", "degraded"}
    assert isinstance(body["models"], list)
    assert "cache" in body and "hit_rate" in body["cache"]
