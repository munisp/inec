"""Smoke tests: fail-closed auth, key rotation, and a real health probe."""

import importlib.util
import sys
from datetime import datetime, timezone
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("ai_anomaly_detection_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)

SCORE_BODY = {
    "polling_unit_id": "PU-TEST-001",
    "state": "LA",
    "lga": "IKEJA",
    "ward": "WARD-01",
    "registered_voters": 1000,
    "accredited_voters": 500,
    "votes_cast": 490,
    "rejected_votes": 10,
    "submission_delay_min": 30.0,
    "benford_deviation": 0.05,
    "regional_mean_turnout": 0.5,
    "party_results": {"APC": 250, "LP": 240},
}


@pytest.fixture()
def client():
    return TestClient(service.app)


def test_protected_endpoint_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "AI_ANOMALY_API_KEYS", [])
    resp = client.post("/api/v1/anomaly/score", json=SCORE_BODY)
    assert resp.status_code == 503


def test_protected_endpoint_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setattr(service, "AI_ANOMALY_API_KEYS", ["primary-key", "rotated-key"])
    assert client.post("/api/v1/anomaly/score", json=SCORE_BODY).status_code == 401
    assert (
        client.post(
            "/api/v1/anomaly/score", json=SCORE_BODY,
            headers={"Authorization": "Bearer wrong-key"},
        ).status_code
        == 401
    )


def test_rotated_second_key_is_accepted(client, monkeypatch):
    """Key rotation: any comma-separated key authenticates (constant-time).

    The trained-inference call is stubbed so this test asserts the auth path,
    not the (separately deployed) ONNX inference engine.
    """
    monkeypatch.setattr(service, "AI_ANOMALY_API_KEYS", ["primary-key", "rotated-key"])

    async def fake_score_record(record):
        return service.AnomalyAlert(
            polling_unit_id=record.polling_unit_id,
            state=record.state,
            anomaly_score=0.1,
            is_anomaly=False,
            confidence=0.9,
            features={},
            explanation="stubbed trained inference",
            severity="low",
            timestamp=datetime.now(timezone.utc),
            model="anomaly_xgboost",
            inference_time_us=1,
        )

    monkeypatch.setattr(service, "score_record", fake_score_record)
    resp = client.post(
        "/api/v1/anomaly/score", json=SCORE_BODY,
        headers={"Authorization": "Bearer rotated-key"},
    )
    assert resp.status_code == 200
    assert resp.json()["is_anomaly"] is False


def test_health_fails_closed_without_inference_backend(client, monkeypatch):
    """Health is a real probe: without INFERENCE_ENGINE_URL it reports 503."""
    monkeypatch.setattr(service, "INFERENCE_ENGINE_URL", "")
    resp = client.get("/api/v1/anomaly/health")
    assert resp.status_code == 503
    assert "inference" in resp.json()["detail"].lower()
