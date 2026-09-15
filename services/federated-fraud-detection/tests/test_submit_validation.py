"""Regression tests: num_samples validation + per-round state dedup (R4-26).

Previously num_samples was client-asserted with no lower bound (0 across all
clients -> ZeroDivisionError -> 500; negatives -> FedAvg weight manipulation),
and a second submission from the same state in the same round was counted.
"""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("federated_fraud_detection_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service
_spec.loader.exec_module(service)

KEY = "validation-test-key"


def _body(state_code, num_samples=100):
    return {
        "state_code": state_code,
        "round_number": 0,
        "weights": [0.01] * 8,
        "bias": 0.0,
        "num_samples": num_samples,
        "loss": 0.5,
        "accuracy": 0.9,
    }


@pytest.fixture()
def client(monkeypatch):
    monkeypatch.setattr(service, "FEDERATED_SUBMIT_KEYS", [KEY])
    return TestClient(service.app)


def _submit(client, state, num_samples=100):
    return client.post(
        "/api/v1/federated/submit-update",
        json=_body(state, num_samples),
        headers={"Authorization": f"Bearer {KEY}"},
    )


def test_zero_num_samples_rejected_422_not_500(client):
    resp = _submit(client, "ZZ-zero", num_samples=0)
    assert resp.status_code == 422, resp.text


def test_negative_num_samples_rejected_422(client):
    resp = _submit(client, "ZZ-neg", num_samples=-50)
    assert resp.status_code == 422, resp.text


def test_duplicate_state_same_round_rejected_409(client):
    r1 = _submit(client, "ZZ-dup", num_samples=10)
    assert r1.status_code == 200
    r2 = _submit(client, "ZZ-dup", num_samples=10)
    assert r2.status_code == 409, r2.text


def test_valid_update_accepted(client):
    resp = _submit(client, "ZZ-ok", num_samples=250)
    assert resp.status_code == 200
    assert resp.json()["accepted"] is True
