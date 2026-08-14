"""Regression tests: per-(election, polling-unit) submission idempotency (R4-09).

A re-POST of the same polling unit result must be rejected with 409 instead of
double-counting via unconditional homomorphic addition.
"""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("homomorphic_tally_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service
_spec.loader.exec_module(service)

TOKEN = "idem-test-token"


@pytest.fixture()
def client(monkeypatch):
    monkeypatch.setattr(service, "TALLY_SUBMIT_TOKENS", [TOKEN])
    public_key, private_key = service.generate_paillier_keypair(bits=512)
    monkeypatch.setattr(service, "public_key", public_key)
    monkeypatch.setattr(service, "private_key", private_key)
    return TestClient(service.app)


def _submit(client, election_id, pu_id, votes):
    return client.post(
        "/api/v1/tally/submit",
        json={
            "election_id": election_id,
            "polling_unit_id": pu_id,
            "party_votes": votes,
        },
        headers={"Authorization": f"Bearer {TOKEN}"},
    )


def test_duplicate_polling_unit_submission_rejected_409(client):
    election = "idem-election-a"
    r1 = _submit(client, election, "PU-IDEM-001", {"APC": 10, "LP": 5})
    assert r1.status_code == 200
    r2 = _submit(client, election, "PU-IDEM-001", {"APC": 10, "LP": 5})
    assert r2.status_code == 409


def test_same_pu_different_election_allowed(client):
    assert _submit(client, "idem-election-b1", "PU-IDEM-002", {"APC": 3}).status_code == 200
    assert _submit(client, "idem-election-b2", "PU-IDEM-002", {"APC": 3}).status_code == 200


def test_tally_math_correct_with_distinct_polling_units(client, monkeypatch):
    election = "idem-election-c"
    assert _submit(client, election, "PU-IDEM-010", {"APC": 100, "LP": 40}).status_code == 200
    assert _submit(client, election, "PU-IDEM-011", {"APC": 23, "LP": 17}).status_code == 200
    # one duplicate attempt must not change the tally
    assert _submit(client, election, "PU-IDEM-011", {"APC": 999, "LP": 999}).status_code == 409

    monkeypatch.setenv("TALLY_DECRYPT_TOKEN", "decrypt-tok")
    resp = client.post(
        "/api/v1/tally/decrypt",
        json={"election_id": election, "authorization_token": "decrypt-tok"},
    )
    assert resp.status_code == 200
    assert resp.json()["results"] == {"APC": 123, "LP": 57}
    assert resp.json()["total_votes"] == 180


def test_persistence_required_but_absent_fails_closed_503(client, monkeypatch):
    monkeypatch.setattr(service, "_pg_pool", None)
    monkeypatch.setattr(service, "TALLY_REQUIRE_PERSISTENCE", True)
    resp = _submit(client, "idem-election-d", "PU-IDEM-020", {"APC": 1})
    assert resp.status_code == 503
