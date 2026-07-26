import asyncio
from datetime import UTC, datetime

import duckdb
import pytest
from pydantic import ValidationError

from external_device import ExternalDeviceEvent, ExternalDeviceLakehouse


def event_payload(**overrides):
    payload = {
        "correlation_id": "c" * 32,
        "event_hash": "a" * 64,
        "device_id": "BVAS-001",
        "election_id": 1,
        "polling_unit_code": "PU-001",
        "event_type": "heartbeat",
        "observed_at": datetime.now(UTC).isoformat(),
        "payload_sha256": "b" * 64,
        "envelope_sha256": "a" * 64,
        "attestation_key_id": "inec-integrity-key-v1",
        "attestation_policy_version": "device-policy-v1",
        "battery_level": 11,
        "signal_strength": -115,
        "firmware_version": "1.2.3",
        "sync_queue_size": 120,
    }
    payload.update(overrides)
    return payload


def test_external_device_event_rejects_unknown_sensitive_field():
    with pytest.raises(ValidationError):
        ExternalDeviceEvent.model_validate(event_payload(voter_pvc_number="not-permitted"))


def test_external_device_lakehouse_ingests_redacted_event_and_requires_review(tmp_path, monkeypatch):
    monkeypatch.delenv("SEDONA_SERVICE_URL", raising=False)
    monkeypatch.delenv("SEDONA_SERVICE_TOKEN", raising=False)
    conn = duckdb.connect(":memory:")
    store = ExternalDeviceLakehouse(conn, str(tmp_path))
    event = ExternalDeviceEvent.model_validate(event_payload())

    result = asyncio.run(store.ingest(event))

    assert result["status"] == "ingested"
    assert result["quality"]["requires_manual_review"] is True
    assert result["spatial"]["status"] == "unavailable"
    assert (tmp_path / "external_device_events.parquet").exists()
    row = conn.execute("SELECT device_hash, event_hash FROM external_device_events").fetchone()
    assert row[0] != event.device_id
    assert row[1] == event.event_hash


def test_external_device_lakehouse_is_idempotent(tmp_path, monkeypatch):
    monkeypatch.delenv("SEDONA_SERVICE_URL", raising=False)
    monkeypatch.delenv("SEDONA_SERVICE_TOKEN", raising=False)
    conn = duckdb.connect(":memory:")
    store = ExternalDeviceLakehouse(conn, str(tmp_path))
    event = ExternalDeviceEvent.model_validate(event_payload())

    asyncio.run(store.ingest(event))
    result = asyncio.run(store.ingest(event))

    assert result == {"status": "idempotent", "event_hash": event.event_hash}
    assert conn.execute("SELECT COUNT(*) FROM external_device_events").fetchone()[0] == 1
