"""Apache Sedona spatial authority for redacted BVAS device-event geofence validation.

The service consumes only hash-addressed device telemetry and authoritative
polling-unit coordinates.
"""

from __future__ import annotations

import hashlib
import json
import os
from contextlib import asynccontextmanager
from dataclasses import dataclass
from typing import Any

import psycopg
from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel, Field, field_validator


@dataclass(frozen=True)
class Settings:
    database_url: str
    token: str
    geofence_meters: float

    @classmethod
    def load(cls) -> "Settings":
        database_url = os.getenv("DATABASE_URL", "").strip()
        token = os.getenv("SEDONA_SERVICE_TOKEN", "").strip()
        raw_radius = os.getenv("DEVICE_GEOFENCE_METERS", "").strip()
        if not database_url or not token or not raw_radius:
            raise RuntimeError(
                "DATABASE_URL, SEDONA_SERVICE_TOKEN, and "
                "DEVICE_GEOFENCE_METERS are required"
            )
        try:
            radius = float(raw_radius)
        except ValueError as exc:
            raise RuntimeError("DEVICE_GEOFENCE_METERS must be numeric") from exc
        if radius <= 0 or radius > 5000:
            raise RuntimeError("DEVICE_GEOFENCE_METERS must be between 0 and 5000")
        return cls(database_url=database_url, token=token, geofence_meters=radius)


class DeviceGeofenceRequest(BaseModel):
    version: str
    event_hash: str = Field(min_length=64, max_length=64)
    device_hash: str = Field(min_length=64, max_length=64)
    election_id: int = Field(gt=0)
    polling_unit_code: str = Field(min_length=1, max_length=128)
    latitude: float = Field(ge=-90, le=90)
    longitude: float = Field(ge=-180, le=180)
    observed_at: str = Field(min_length=20, max_length=64)

    @field_validator("version")
    @classmethod
    def version_is_supported(cls, value: str) -> str:
        if value != "inec-device-spatial-v1":
            raise ValueError("unsupported device spatial contract version")
        return value

    @field_validator("event_hash", "device_hash")
    @classmethod
    def sha256_is_canonical(cls, value: str) -> str:
        value = value.lower()
        if any(char not in "0123456789abcdef" for char in value):
            raise ValueError("hash must be a SHA-256 hex digest")
        return value


class SedonaAuthority:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self.spark: Any | None = None
        self.startup_error: str | None = None

    def start(self) -> None:
        try:
            from sedona.spark import SedonaContext

            self.spark = SedonaContext.create(
                SedonaContext.builder()
                .appName("inec-sedona-device-geofence")
                .config("spark.sql.session.timeZone", "UTC")
                .getOrCreate()
            )
        except Exception as exc:  # no local fallback is permitted
            self.startup_error = str(exc)
            self.spark = None

    def _polling_unit_coordinate(self, polling_unit_code: str) -> tuple[float, float]:
        try:
            with psycopg.connect(
                self.settings.database_url, connect_timeout=5
            ) as connection:
                with connection.cursor() as cursor:
                    cursor.execute(
                        """
                        SELECT latitude, longitude
                        FROM polling_units
                        WHERE code = %s
                          AND latitude IS NOT NULL
                          AND longitude IS NOT NULL
                        """,
                        (polling_unit_code,),
                    )
                    row = cursor.fetchone()
        except Exception as exc:
            raise HTTPException(
                status_code=503,
                detail="authoritative polling-unit spatial source is unavailable",
            ) from exc
        if not row:
            raise HTTPException(
                status_code=503,
                detail="authoritative polling-unit coordinate is unavailable",
            )
        return float(row[0]), float(row[1])

    def validate(self, request: DeviceGeofenceRequest) -> dict[str, Any]:
        if self.spark is None:
            raise HTTPException(
                status_code=503, detail="Apache Sedona spatial engine is unavailable"
            )
        center_latitude, center_longitude = self._polling_unit_coordinate(
            request.polling_unit_code
        )
        try:
            frame = self.spark.createDataFrame(
                [
                    (
                        request.longitude,
                        request.latitude,
                        center_longitude,
                        center_latitude,
                    )
                ],
                ["device_lng", "device_lat", "pu_lng", "pu_lat"],
            )
            frame.createOrReplaceTempView("device_geofence_input")
            distance = self.spark.sql(
                """
                SELECT ST_DistanceSphere(
                    ST_Point(device_lng, device_lat),
                    ST_Point(pu_lng, pu_lat)
                ) AS distance_m
                FROM device_geofence_input
                """
            ).first()["distance_m"]
        except Exception as exc:
            raise HTTPException(
                status_code=503, detail="Apache Sedona spatial computation failed"
            ) from exc
        source = {
            "polling_unit_code": request.polling_unit_code,
            "latitude": center_latitude,
            "longitude": center_longitude,
            "geofence_meters": self.settings.geofence_meters,
        }
        source_sha256 = hashlib.sha256(
            json.dumps(source, sort_keys=True, separators=(",", ":")).encode("utf-8")
        ).hexdigest()
        validation_id = hashlib.sha256(
            f"{request.event_hash}:{source_sha256}".encode("utf-8")
        ).hexdigest()
        distance_m = float(distance)
        within = distance_m <= self.settings.geofence_meters
        return {
            "status": "validated" if within else "outside_geofence",
            "within_geofence": within,
            "distance_m": round(distance_m, 3),
            "validation_id": validation_id,
            "source_sha256": source_sha256,
        }


settings: Settings | None = None
authority: SedonaAuthority | None = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global settings, authority
    try:
        settings = Settings.load()
        authority = SedonaAuthority(settings)
        authority.start()
    except RuntimeError:
        settings = None
        authority = None
    yield
    if authority and authority.spark:
        authority.spark.stop()


app = FastAPI(
    title="INEC Apache Sedona Spatial Authority", version="1.0.0", lifespan=lifespan
)


def require_token(request: Request) -> None:
    if settings is None or not settings.token:
        raise HTTPException(status_code=503, detail="spatial authority is unconfigured")
    if request.headers.get("authorization", "") != f"Bearer {settings.token}":
        raise HTTPException(status_code=401, detail="invalid spatial authority token")


@app.get("/health")
async def health() -> dict[str, Any]:
    if settings is None or authority is None:
        return {
            "status": "unavailable",
            "reason": "required spatial authority configuration is missing",
        }
    if authority.spark is None:
        return {"status": "unavailable", "reason": "Apache Sedona startup failed"}
    return {
        "status": "healthy",
        "engine": "apache-sedona",
        "geofence_meters": settings.geofence_meters,
    }


@app.post("/v1/device-geofence/validate")
async def validate_device_geofence(
    payload: DeviceGeofenceRequest, request: Request
) -> dict[str, Any]:
    require_token(request)
    if authority is None:
        raise HTTPException(status_code=503, detail="spatial authority is unconfigured")
    return authority.validate(payload)
