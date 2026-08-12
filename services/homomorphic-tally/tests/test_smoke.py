"""Smoke tests: fail-closed auth, key rotation, and a real health probe."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("homomorphic_tally_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)

VOTE_BODY = {
    "election_id": "test-election-2027",
    "polling_unit_id": "PU-TEST-001",
    "party_votes": {"APC": 120, "LP": 95},
}


@pytest.fixture()
def client():
    return TestClient(service.app)


@pytest.fixture()
def test_keypair(monkeypatch):
    """Small keypair for tests only — production startup enforces >= 2048 bits."""
    public_key, private_key = service.generate_paillier_keypair(bits=512)
    monkeypatch.setattr(service, "public_key", public_key)
    monkeypatch.setattr(service, "private_key", private_key)
    return public_key, private_key


def test_protected_endpoint_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "TALLY_SUBMIT_TOKENS", [])
    resp = client.post("/api/v1/tally/submit", json=VOTE_BODY)
    assert resp.status_code == 503


def test_protected_endpoint_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setattr(service, "TALLY_SUBMIT_TOKENS", ["primary-token", "rotated-token"])
    assert client.post("/api/v1/tally/submit", json=VOTE_BODY).status_code == 401
    assert (
        client.post(
            "/api/v1/tally/submit", json=VOTE_BODY,
            headers={"Authorization": "Bearer wrong-token"},
        ).status_code
        == 401
    )


def test_rotated_second_key_is_accepted(client, monkeypatch, test_keypair):
    """Key rotation: any comma-separated token authenticates (constant-time)."""
    monkeypatch.setattr(service, "TALLY_SUBMIT_TOKENS", ["primary-token", "rotated-token"])
    resp = client.post(
        "/api/v1/tally/submit", json=VOTE_BODY,
        headers={"Authorization": "Bearer rotated-token"},
    )
    assert resp.status_code == 200
    assert resp.json()["status"] == "accepted"


def test_health_returns_real_probe_structure(client):
    resp = client.get("/api/v1/tally/health")
    assert resp.status_code in (200, 503)
    body = resp.json()
    assert body["status"] in {"healthy", "degraded"}
    assert isinstance(body["checks"], dict)
    assert body["persistence"] in {"postgresql", "in_memory"}
