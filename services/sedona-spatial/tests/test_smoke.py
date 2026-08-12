"""Smoke tests: fail-closed auth, key rotation, and a real health probe."""

import importlib.util
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

SERVICE_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SERVICE_DIR))

_spec = importlib.util.spec_from_file_location("sedona_spatial_main", SERVICE_DIR / "main.py")
service = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = service  # register so pydantic resolves forward refs
_spec.loader.exec_module(service)

GEOFENCE_BODY = {
    "version": "inec-device-spatial-v1",
    "event_hash": "a" * 64,
    "device_hash": "b" * 64,
    "election_id": 1,
    "polling_unit_code": "PU-001",
    "latitude": 9.05,
    "longitude": 7.49,
    "observed_at": "2026-07-26T12:00:00Z",
}


class _FakeResult:
    def first(self):
        return {"distance_m": 12.345}


class _FakeFrame:
    def createOrReplaceTempView(self, name):
        assert name == "device_geofence_input"


class _FakeSpark:
    def createDataFrame(self, rows, columns):
        return _FakeFrame()

    def sql(self, query):
        assert "ST_DistanceSphere" in query
        return _FakeResult()


def _test_settings(token="primary-token,rotated-token"):
    return service.Settings(
        database_url="postgres://not-used", token=token, geofence_meters=50.0
    )


def _fake_authority():
    instance = service.SedonaAuthority(_test_settings())
    instance.spark = _FakeSpark()
    instance._polling_unit_coordinate = lambda _: (9.05, 7.49)
    return instance


@pytest.fixture()
def client():
    # No lifespan: startup requires DATABASE_URL + a Sedona/Spark engine,
    # which are deployment dependencies, not unit-test ones.
    return TestClient(service.app)


def test_protected_endpoint_fails_closed_when_key_unconfigured(client, monkeypatch):
    monkeypatch.setattr(service, "settings", None)
    resp = client.post("/v1/device-geofence/validate", json=GEOFENCE_BODY)
    assert resp.status_code == 503


def test_protected_endpoint_rejects_missing_and_wrong_key(client, monkeypatch):
    monkeypatch.setattr(service, "settings", _test_settings())
    assert client.post("/v1/device-geofence/validate", json=GEOFENCE_BODY).status_code == 401
    assert (
        client.post(
            "/v1/device-geofence/validate", json=GEOFENCE_BODY,
            headers={"Authorization": "Bearer wrong-token"},
        ).status_code
        == 401
    )


def test_rotated_second_key_is_accepted(client, monkeypatch):
    """Key rotation: any comma-separated token authenticates (constant-time)."""
    monkeypatch.setattr(service, "settings", _test_settings())
    monkeypatch.setattr(service, "authority", _fake_authority())
    resp = client.post(
        "/v1/device-geofence/validate", json=GEOFENCE_BODY,
        headers={"Authorization": "Bearer rotated-token"},
    )
    assert resp.status_code == 200
    assert resp.json()["status"] == "validated"


def test_health_returns_real_probe_structure(client, monkeypatch):
    monkeypatch.setattr(service, "settings", None)
    monkeypatch.setattr(service, "authority", None)
    resp = client.get("/health")
    assert resp.status_code == 200
    body = resp.json()
    # Unconfigured spatial authority must report unavailable, never "healthy".
    assert body["status"] == "unavailable"
    assert body["reason"]
