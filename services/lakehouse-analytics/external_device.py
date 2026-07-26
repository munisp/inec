"""Authoritative external-device analytics for the INEC lakehouse.

This module accepts only pre-verified, redacted device events from the Go gateway.
It stores hashes and operational metrics, never raw biometric templates, voter identifiers,
or result images. Spatial validation is delegated to a configured Apache Sedona service;
there is deliberately no local geometric approximation when that authority is unavailable.
"""

from __future__ import annotations

import hashlib
import os
from datetime import UTC, datetime
from typing import Any

import duckdb
import httpx
from pydantic import BaseModel, ConfigDict, Field, field_validator


class ExternalDeviceEvent(BaseModel):
    model_config = ConfigDict(extra="forbid")

    correlation_id: str = Field(min_length=16, max_length=128)
    event_hash: str = Field(min_length=64, max_length=64)
    device_id: str = Field(min_length=1, max_length=128)
    election_id: int = Field(gt=0)
    polling_unit_code: str = Field(min_length=1, max_length=128)
    event_type: str
    observed_at: datetime
    payload_sha256: str = Field(min_length=64, max_length=64)
    envelope_sha256: str = Field(min_length=64, max_length=64)
    attestation_key_id: str = Field(min_length=1, max_length=256)
    attestation_policy_version: str = Field(min_length=1, max_length=128)
    battery_level: int | None = Field(default=None, ge=0, le=100)
    signal_strength: int | None = Field(default=None, ge=-150, le=0)
    latitude: float | None = Field(default=None, ge=-90, le=90)
    longitude: float | None = Field(default=None, ge=-180, le=180)
    firmware_version: str | None = Field(default=None, max_length=128)
    sync_queue_size: int | None = Field(default=None, ge=0)

    @field_validator("event_hash", "payload_sha256", "envelope_sha256")
    @classmethod
    def lowercase_sha256(cls, value: str) -> str:
        value = value.lower().strip()
        if len(value) != 64 or any(char not in "0123456789abcdef" for char in value):
            raise ValueError("must be a lowercase SHA-256 hex digest")
        return value

    @field_validator("event_type")
    @classmethod
    def approved_event_type(cls, value: str) -> str:
        if value not in {"accreditation", "result_capture", "heartbeat", "incident"}:
            raise ValueError("unapproved device event type")
        return value

    @field_validator("observed_at")
    @classmethod
    def require_timezone(cls, value: datetime) -> datetime:
        if value.tzinfo is None:
            raise ValueError("observed_at must include a timezone")
        return value.astimezone(UTC)


class DeviceQualityResult(BaseModel):
    event_hash: str
    device_hash: str
    status: str
    requires_manual_review: bool
    findings: list[dict[str, Any]]
    risk_score: float
    assessed_at: datetime


class SedonaSpatialGateway:
    """Call a governed Apache Sedona HTTP service; never infer a result locally."""

    def __init__(self) -> None:
        self.base_url = os.getenv("SEDONA_SERVICE_URL", "").strip().rstrip("/")
        self.token = os.getenv("SEDONA_SERVICE_TOKEN", "").strip()

    async def validate(self, event: ExternalDeviceEvent) -> dict[str, Any]:
        if not self.base_url or not self.token:
            return {
                "status": "unavailable",
                "reason": "Apache Sedona spatial authority is not configured",
            }
        if event.latitude is None or event.longitude is None:
            return {
                "status": "unavailable",
                "reason": "device event has no authoritative coordinate pair",
            }
        payload = {
            "version": "inec-device-spatial-v1",
            "event_hash": event.event_hash,
            "device_hash": device_hash(event.device_id),
            "election_id": event.election_id,
            "polling_unit_code": event.polling_unit_code,
            "latitude": event.latitude,
            "longitude": event.longitude,
            "observed_at": event.observed_at.isoformat(),
        }
        try:
            async with httpx.AsyncClient(timeout=10.0) as client:
                response = await client.post(
                    f"{self.base_url}/v1/device-geofence/validate",
                    json=payload,
                    headers={"Authorization": f"Bearer {self.token}"},
                )
        except httpx.HTTPError:
            return {
                "status": "unavailable",
                "reason": "Apache Sedona spatial authority did not respond",
            }
        if response.status_code != 200:
            return {
                "status": "unavailable",
                "reason": f"Apache Sedona spatial authority returned {response.status_code}",
            }
        try:
            data = response.json()
        except ValueError:
            return {
                "status": "unavailable",
                "reason": "Apache Sedona spatial authority returned invalid JSON",
            }
        required = {"status", "within_geofence", "distance_m", "validation_id", "source_sha256"}
        if not required.issubset(data) or data["status"] not in {"validated", "outside_geofence"}:
            return {
                "status": "unavailable",
                "reason": "Apache Sedona spatial authority returned an invalid validation contract",
            }
        return {
            "status": data["status"],
            "within_geofence": bool(data["within_geofence"]),
            "distance_m": float(data["distance_m"]),
            "validation_id": str(data["validation_id"]),
            "source_sha256": str(data["source_sha256"]),
        }


def device_hash(device_id: str) -> str:
    return hashlib.sha256(device_id.encode("utf-8")).hexdigest()


class ExternalDeviceLakehouse:
    def __init__(self, conn: duckdb.DuckDBPyConnection, data_dir: str) -> None:
        self.conn = conn
        self.data_dir = data_dir
        self.sedona = SedonaSpatialGateway()
        self._init_tables()

    def _init_tables(self) -> None:
        self.conn.execute(
            """
            CREATE TABLE IF NOT EXISTS external_device_events (
                event_hash VARCHAR PRIMARY KEY,
                correlation_id VARCHAR NOT NULL,
                device_hash VARCHAR NOT NULL,
                election_id BIGINT NOT NULL,
                polling_unit_code VARCHAR NOT NULL,
                event_type VARCHAR NOT NULL,
                observed_at TIMESTAMPTZ NOT NULL,
                payload_sha256 VARCHAR NOT NULL,
                envelope_sha256 VARCHAR NOT NULL,
                attestation_key_id VARCHAR NOT NULL,
                attestation_policy_version VARCHAR NOT NULL,
                battery_level INTEGER,
                signal_strength INTEGER,
                latitude DOUBLE,
                longitude DOUBLE,
                firmware_version VARCHAR,
                sync_queue_size INTEGER,
                ingested_at TIMESTAMPTZ NOT NULL
            )
            """
        )
        self.conn.execute(
            """
            CREATE TABLE IF NOT EXISTS external_device_quality (
                event_hash VARCHAR PRIMARY KEY,
                device_hash VARCHAR NOT NULL,
                status VARCHAR NOT NULL,
                requires_manual_review BOOLEAN NOT NULL,
                risk_score DOUBLE NOT NULL,
                findings_json VARCHAR NOT NULL,
                spatial_status VARCHAR NOT NULL,
                spatial_validation_id VARCHAR,
                spatial_source_sha256 VARCHAR,
                assessed_at TIMESTAMPTZ NOT NULL
            )
            """
        )

    def assess_quality(self, event: ExternalDeviceEvent) -> DeviceQualityResult:
        findings: list[dict[str, Any]] = []
        risk_score = 0.0
        if event.battery_level is not None and event.battery_level < 15:
            findings.append({"code": "low_battery", "severity": "medium", "value": event.battery_level})
            risk_score += 0.2
        if event.signal_strength is not None and event.signal_strength < -110:
            findings.append({"code": "weak_signal", "severity": "medium", "value": event.signal_strength})
            risk_score += 0.2
        if event.sync_queue_size is not None and event.sync_queue_size > 100:
            findings.append({"code": "backlogged_sync_queue", "severity": "high", "value": event.sync_queue_size})
            risk_score += 0.4
        if not event.firmware_version:
            findings.append({"code": "firmware_version_missing", "severity": "high"})
            risk_score += 0.4
        requires_manual_review = any(item["severity"] == "high" for item in findings)
        status = "manual_review_required" if requires_manual_review else "analysis_complete"
        return DeviceQualityResult(
            event_hash=event.event_hash,
            device_hash=device_hash(event.device_id),
            status=status,
            requires_manual_review=requires_manual_review,
            findings=findings,
            risk_score=min(1.0, risk_score),
            assessed_at=datetime.now(UTC),
        )

    async def ingest(self, event: ExternalDeviceEvent) -> dict[str, Any]:
        existing = self.conn.execute(
            "SELECT event_hash FROM external_device_events WHERE event_hash = ?", [event.event_hash]
        ).fetchone()
        if existing:
            return {"status": "idempotent", "event_hash": event.event_hash}
        quality = self.assess_quality(event)
        spatial = await self.sedona.validate(event)
        if spatial["status"] == "outside_geofence":
            quality.findings.append(
                {
                    "code": "outside_authoritative_geofence",
                    "severity": "high",
                    "distance_m": spatial["distance_m"],
                    "validation_id": spatial["validation_id"],
                }
            )
            quality.requires_manual_review = True
            quality.status = "manual_review_required"
            quality.risk_score = min(1.0, quality.risk_score + 0.5)
        elif spatial["status"] == "unavailable":
            quality.findings.append({"code": "spatial_validation_unavailable", "severity": "medium"})
            quality.requires_manual_review = True
            quality.status = "manual_review_required"
            quality.risk_score = min(1.0, quality.risk_score + 0.2)
        now = datetime.now(UTC)
        self.conn.execute(
            """
            INSERT INTO external_device_events VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            [
                event.event_hash,
                event.correlation_id,
                device_hash(event.device_id),
                event.election_id,
                event.polling_unit_code,
                event.event_type,
                event.observed_at,
                event.payload_sha256,
                event.envelope_sha256,
                event.attestation_key_id,
                event.attestation_policy_version,
                event.battery_level,
                event.signal_strength,
                event.latitude,
                event.longitude,
                event.firmware_version,
                event.sync_queue_size,
                now,
            ],
        )
        self.conn.execute(
            """
            INSERT INTO external_device_quality VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            [
                quality.event_hash,
                quality.device_hash,
                quality.status,
                quality.requires_manual_review,
                quality.risk_score,
                json_dumps(quality.findings),
                spatial["status"],
                spatial.get("validation_id"),
                spatial.get("source_sha256"),
                quality.assessed_at,
            ],
        )
        self.export_parquet()
        return {
            "status": "ingested",
            "event_hash": event.event_hash,
            "quality": quality.model_dump(mode="json"),
            "spatial": spatial,
        }

    def export_parquet(self) -> None:
        os.makedirs(self.data_dir, exist_ok=True)
        target = os.path.join(self.data_dir, "external_device_events.parquet")
        self.conn.execute(
            f"COPY (SELECT * FROM external_device_events ORDER BY ingested_at, event_hash) TO '{target}' (FORMAT PARQUET)"
        )

    def quality_summary(self, limit: int = 100) -> dict[str, Any]:
        rows = self.conn.execute(
            """
            SELECT status, spatial_status, COUNT(*) AS events,
                   SUM(CASE WHEN requires_manual_review THEN 1 ELSE 0 END) AS manual_review_count,
                   AVG(risk_score) AS avg_risk
            FROM external_device_quality
            GROUP BY status, spatial_status
            ORDER BY events DESC
            LIMIT ?
            """,
            [limit],
        ).fetchall()
        return {
            "status": "ok",
            "groups": [
                {
                    "assessment_status": row[0],
                    "spatial_status": row[1],
                    "events": row[2],
                    "manual_review_count": row[3],
                    "average_risk_score": round(float(row[4] or 0), 4),
                }
                for row in rows
            ],
        }


def json_dumps(value: Any) -> str:
    import json

    return json.dumps(value, sort_keys=True, separators=(",", ":"))
