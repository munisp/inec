from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import pytest

MODULE_PATH = Path(__file__).with_name("main.py")
SPEC = importlib.util.spec_from_file_location("inec_sedona_spatial", MODULE_PATH)
assert SPEC and SPEC.loader
spatial = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = spatial
SPEC.loader.exec_module(spatial)


class FakeResult:
    def __init__(self, distance_m: float) -> None:
        self.distance_m = distance_m

    def first(self) -> dict[str, float]:
        return {"distance_m": self.distance_m}


class FakeFrame:
    def createOrReplaceTempView(self, name: str) -> None:
        assert name == "device_geofence_input"


class FakeSpark:
    def __init__(self, distance_m: float) -> None:
        self.distance_m = distance_m
        self.rows = None

    def createDataFrame(self, rows, columns):
        self.rows = rows
        assert columns == ["device_lng", "device_lat", "pu_lng", "pu_lat"]
        return FakeFrame()

    def sql(self, query: str) -> FakeResult:
        assert "ST_DistanceSphere" in query
        return FakeResult(self.distance_m)


def payload() -> spatial.DeviceGeofenceRequest:
    return spatial.DeviceGeofenceRequest(
        version="inec-device-spatial-v1",
        event_hash="a" * 64,
        device_hash="b" * 64,
        election_id=1,
        polling_unit_code="PU-001",
        latitude=9.05,
        longitude=7.49,
        observed_at="2026-07-26T12:00:00Z",
    )


def authority(distance_m: float) -> spatial.SedonaAuthority:
    instance = spatial.SedonaAuthority(
        spatial.Settings(
            database_url="postgres://not-used", token="test-token", geofence_meters=50.0
        )
    )
    instance.spark = FakeSpark(distance_m)
    instance._polling_unit_coordinate = lambda _: (9.05, 7.49)  # type: ignore[method-assign]
    return instance


def test_validate_returns_provenance_bound_geofence_result() -> None:
    result = authority(12.345).validate(payload())
    assert result["status"] == "validated"
    assert result["within_geofence"] is True
    assert result["distance_m"] == 12.345
    assert len(result["validation_id"]) == 64
    assert len(result["source_sha256"]) == 64


def test_validate_routes_outside_geofence_to_reviewable_status() -> None:
    result = authority(81.2).validate(payload())
    assert result["status"] == "outside_geofence"
    assert result["within_geofence"] is False


def test_validate_fails_closed_without_sedona_engine() -> None:
    instance = authority(1.0)
    instance.spark = None
    with pytest.raises(Exception) as error:
        instance.validate(payload())
    assert getattr(error.value, "status_code", None) == 503
