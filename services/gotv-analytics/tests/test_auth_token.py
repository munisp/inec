"""Regression tests: presence-only auth must stay dead (R4-11).

The old middleware accepted ANY non-empty Authorization / dapr-api-token
header. These tests prove: garbage credentials -> 401, unset token env -> 503
(fail closed), correct GOTV_ANALYTICS_TOKEN -> pass.
"""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("gotv_analytics_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service
_spec.loader.exec_module(service)

PROBE = "/gotv-analytics/middleware/status"


@pytest.fixture()
def client():
    return TestClient(service.app)


def test_unset_token_env_fails_closed_503(client, monkeypatch):
    monkeypatch.delenv("GOTV_ANALYTICS_TOKEN", raising=False)
    monkeypatch.delenv("GOTV_ANALYTICS_API_KEY", raising=False)
    assert client.get(PROBE).status_code == 503


def test_garbage_authorization_rejected_401(client, monkeypatch):
    monkeypatch.setenv("GOTV_ANALYTICS_TOKEN", "real-token-value")
    assert client.get(PROBE, headers={"Authorization": "garbage"}).status_code == 401
    assert (
        client.get(PROBE, headers={"Authorization": "Bearer not-the-token"}).status_code
        == 401
    )
    # non-empty dapr-api-token alone must NOT authenticate (old presence-only bug)
    assert client.get(PROBE, headers={"dapr-api-token": "anything"}).status_code == 401


def test_correct_token_passes(client, monkeypatch):
    monkeypatch.delenv("GOTV_ANALYTICS_API_KEY", raising=False)
    monkeypatch.setenv("GOTV_ANALYTICS_TOKEN", "real-token-value")
    resp = client.get(PROBE, headers={"Authorization": "Bearer real-token-value"})
    assert resp.status_code == 200
    assert resp.json()["service"] == "gotv-analytics"


def test_dapr_token_must_match_real_value(client, monkeypatch):
    monkeypatch.setenv("GOTV_ANALYTICS_TOKEN", "svc-token")
    monkeypatch.setenv("DAPR_API_TOKEN", "dapr-secret")
    assert client.get(PROBE, headers={"dapr-api-token": "wrong"}).status_code == 401
    assert client.get(PROBE, headers={"dapr-api-token": "dapr-secret"}).status_code == 200
