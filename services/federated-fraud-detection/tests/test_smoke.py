"""Smoke tests: fail-closed auth, key rotation, and a real health probe."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("federated_fraud_detection_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)

SUBMIT_BODY = {
    "state_code": "LA",
    "round_number": 0,
    "weights": [0.01] * 8,
    "bias": 0.0,
    "num_samples": 100,
    "loss": 0.5,
    "accuracy": 0.9,
}


@pytest.fixture()
def client():
    return TestClient(service.app)


def test_protected_endpoint_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "FEDERATED_SUBMIT_KEYS", [])
    resp = client.post("/api/v1/federated/submit-update", json=SUBMIT_BODY)
    assert resp.status_code == 503


def test_protected_endpoint_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setattr(service, "FEDERATED_SUBMIT_KEYS", ["primary-key", "rotated-key"])
    assert client.post("/api/v1/federated/submit-update", json=SUBMIT_BODY).status_code == 401
    assert (
        client.post(
            "/api/v1/federated/submit-update", json=SUBMIT_BODY,
            headers={"Authorization": "Bearer wrong-key"},
        ).status_code
        == 401
    )


def test_rotated_second_key_is_accepted(client, monkeypatch):
    """Key rotation: any comma-separated key authenticates (constant-time)."""
    monkeypatch.setattr(service, "FEDERATED_SUBMIT_KEYS", ["primary-key", "rotated-key"])
    resp = client.post(
        "/api/v1/federated/submit-update", json=SUBMIT_BODY,
        headers={"Authorization": "Bearer rotated-key"},
    )
    assert resp.status_code == 200
    assert resp.json()["accepted"] is True


def test_health_returns_real_probe_structure(client):
    resp = client.get("/api/v1/federated/status")
    assert resp.status_code == 200
    body = resp.json()
    assert body["status"] == "healthy"
    assert isinstance(body["current_round"], int)
    assert body["persistence"] in {"postgresql", "in_memory"}
