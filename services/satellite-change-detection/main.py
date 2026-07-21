"""Real STAC-backed satellite imagery analysis for polling-unit validation.

The service queries a configured STAC API for actual imagery scenes, downloads a
configured preview asset, and computes spectral change from observed pixels. It
never invents scene locations, image patches, flood scores, or crowd counts.
"""

from __future__ import annotations

import io
import math
import os
from datetime import date, datetime, timedelta, timezone
from typing import Any

import httpx
import numpy as np
import uvicorn
from fastapi import FastAPI, HTTPException
from PIL import Image
from pydantic import BaseModel, Field

APP_VERSION = "2.0.0"
REQUEST_TIMEOUT_SECONDS = 30.0
SCENE_WINDOW_DAYS = 14

app = FastAPI(
    title="INEC Satellite Change Detection Service",
    description="Real STAC-backed polling-unit imagery analysis",
    version=APP_VERSION,
)


class PollingUnitValidationRequest(BaseModel):
    polling_unit_id: str
    registered_lat: float = Field(ge=-90, le=90)
    registered_lon: float = Field(ge=-180, le=180)
    election_date: str
    include_flood_risk: bool = False
    include_crowd_density: bool = False


class ChangeDetectionRequest(BaseModel):
    polling_unit_id: str
    lat: float = Field(ge=-90, le=90)
    lon: float = Field(ge=-180, le=180)
    before_date: str
    after_date: str


class STACConfig(BaseModel):
    api_url: str
    collection: str
    preview_asset: str
    max_cloud_cover: float


def required_env(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        raise HTTPException(
            status_code=503,
            detail=f"satellite service requires {name}; synthetic imagery is disabled",
        )
    return value


def stac_config() -> STACConfig:
    try:
        max_cloud_cover = float(required_env("STAC_MAX_CLOUD_COVER"))
    except ValueError as exc:
        raise HTTPException(status_code=503, detail="STAC_MAX_CLOUD_COVER must be numeric") from exc
    if not 0.0 <= max_cloud_cover <= 100.0:
        raise HTTPException(status_code=503, detail="STAC_MAX_CLOUD_COVER must be between 0 and 100")
    return STACConfig(
        api_url=required_env("STAC_API_URL").rstrip("/"),
        collection=required_env("STAC_COLLECTION"),
        preview_asset=required_env("STAC_PREVIEW_ASSET"),
        max_cloud_cover=max_cloud_cover,
    )


def parse_date(value: str, field: str) -> date:
    try:
        return date.fromisoformat(value)
    except ValueError as exc:
        raise HTTPException(status_code=422, detail=f"{field} must use ISO date format YYYY-MM-DD") from exc


def date_interval(target: date) -> str:
    start = datetime.combine(target - timedelta(days=SCENE_WINDOW_DAYS), datetime.min.time(), tzinfo=timezone.utc)
    end = datetime.combine(target + timedelta(days=SCENE_WINDOW_DAYS), datetime.max.time(), tzinfo=timezone.utc)
    return f"{start.isoformat().replace('+00:00', 'Z')}/{end.isoformat().replace('+00:00', 'Z')}"


def point_bbox(lat: float, lon: float, radius_degrees: float = 0.005) -> list[float]:
    return [lon - radius_degrees, lat - radius_degrees, lon + radius_degrees, lat + radius_degrees]


def scene_cloud_cover(scene: dict[str, Any]) -> float | None:
    cloud_cover = scene.get("properties", {}).get("eo:cloud_cover")
    if cloud_cover is None:
        return None
    try:
        return float(cloud_cover)
    except (TypeError, ValueError):
        return None


async def search_scene(
    client: httpx.AsyncClient,
    config: STACConfig,
    lat: float,
    lon: float,
    target_date: date,
) -> dict[str, Any]:
    payload = {
        "collections": [config.collection],
        "bbox": point_bbox(lat, lon),
        "datetime": date_interval(target_date),
        "limit": 20,
        "query": {"eo:cloud_cover": {"lte": config.max_cloud_cover}},
        "sortby": [{"field": "properties.datetime", "direction": "asc"}],
    }
    try:
        response = await client.post(f"{config.api_url}/search", json=payload)
        response.raise_for_status()
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=503, detail=f"STAC search unavailable: {exc}") from exc
    features = response.json().get("features", [])
    if not features:
        raise HTTPException(
            status_code=404,
            detail=f"no {config.collection} STAC scene found for requested location and date window",
        )
    scenes_with_assets = [scene for scene in features if scene.get("assets", {}).get(config.preview_asset, {}).get("href")]
    if not scenes_with_assets:
        raise HTTPException(
            status_code=502,
            detail=f"STAC scenes did not expose configured preview asset {config.preview_asset!r}",
        )
    return min(
        scenes_with_assets,
        key=lambda scene: abs(
            (datetime.fromisoformat(scene["properties"]["datetime"].replace("Z", "+00:00")).date() - target_date).days
        ),
    )


async def download_preview(client: httpx.AsyncClient, scene: dict[str, Any], asset_name: str) -> np.ndarray:
    href = scene["assets"][asset_name]["href"]
    try:
        response = await client.get(href)
        response.raise_for_status()
        image = Image.open(io.BytesIO(response.content)).convert("RGB")
    except (httpx.HTTPError, OSError) as exc:
        raise HTTPException(status_code=502, detail=f"unable to retrieve STAC preview asset: {exc}") from exc
    image.thumbnail((512, 512), Image.Resampling.LANCZOS)
    return np.asarray(image, dtype=np.float32) / 255.0


def align_images(before: np.ndarray, after: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
    height = min(before.shape[0], after.shape[0])
    width = min(before.shape[1], after.shape[1])
    if height < 16 or width < 16:
        raise HTTPException(status_code=502, detail="retrieved STAC preview image is too small for change analysis")
    return before[:height, :width, :], after[:height, :width, :]


def observed_change(before: np.ndarray, after: np.ndarray) -> dict[str, Any]:
    before, after = align_images(before, after)
    per_pixel_distance = np.sqrt(np.mean((before - after) ** 2, axis=2))
    magnitude = float(np.mean(per_pixel_distance))
    high_change_fraction = float(np.mean(per_pixel_distance >= 0.20))
    significant = high_change_fraction >= 0.15
    return {
        "change_magnitude": round(magnitude, 6),
        "high_change_pixel_fraction": round(high_change_fraction, 6),
        "significant_change": significant,
        "change_type": "observed_spectral_change" if significant else "no_material_spectral_change",
    }


def scene_metadata(scene: dict[str, Any], asset_name: str) -> dict[str, Any]:
    properties = scene.get("properties", {})
    asset = scene.get("assets", {}).get(asset_name, {})
    return {
        "scene_id": scene.get("id"),
        "collection": scene.get("collection"),
        "acquired_at": properties.get("datetime"),
        "cloud_cover": scene_cloud_cover(scene),
        "preview_asset": asset_name,
        "preview_href": asset.get("href"),
        "bbox": scene.get("bbox"),
    }


def scene_covers_coordinate(scene: dict[str, Any], lat: float, lon: float) -> bool | None:
    bbox = scene.get("bbox")
    if not isinstance(bbox, list) or len(bbox) < 4:
        return None
    return bool(bbox[0] <= lon <= bbox[2] and bbox[1] <= lat <= bbox[3])


async def analyze_polling_unit(req: PollingUnitValidationRequest) -> dict[str, Any]:
    if req.include_flood_risk or req.include_crowd_density:
        raise HTTPException(
            status_code=422,
            detail=(
                "flood-risk and crowd-density analysis require separately configured real DEM and crowd-model services; "
                "synthetic analytics are disabled"
            ),
        )
    config = stac_config()
    election_date = parse_date(req.election_date, "election_date")
    async with httpx.AsyncClient(timeout=REQUEST_TIMEOUT_SECONDS, follow_redirects=True) as client:
        scene = await search_scene(client, config, req.registered_lat, req.registered_lon, election_date)
    coverage = scene_covers_coordinate(scene, req.registered_lat, req.registered_lon)
    return {
        "polling_unit_id": req.polling_unit_id,
        "source": "STAC",
        "scene": scene_metadata(scene, config.preview_asset),
        "coordinate_validation": {
            "scene_covers_registered_coordinate": coverage,
            "registered_lat": req.registered_lat,
            "registered_lon": req.registered_lon,
        },
        "validation_status": "scene_available" if coverage else "scene_geometry_unavailable",
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }


async def analyze_changes(req: ChangeDetectionRequest) -> dict[str, Any]:
    before_date = parse_date(req.before_date, "before_date")
    after_date = parse_date(req.after_date, "after_date")
    if before_date >= after_date:
        raise HTTPException(status_code=422, detail="before_date must precede after_date")
    config = stac_config()
    async with httpx.AsyncClient(timeout=REQUEST_TIMEOUT_SECONDS, follow_redirects=True) as client:
        before_scene = await search_scene(client, config, req.lat, req.lon, before_date)
        after_scene = await search_scene(client, config, req.lat, req.lon, after_date)
        before_image = await download_preview(client, before_scene, config.preview_asset)
        after_image = await download_preview(client, after_scene, config.preview_asset)
    result = observed_change(before_image, after_image)
    average_cloud = [value for value in (scene_cloud_cover(before_scene), scene_cloud_cover(after_scene)) if value is not None]
    confidence = None if not average_cloud else round(max(0.0, min(1.0, 1.0 - sum(average_cloud) / (100.0 * len(average_cloud)))), 6)
    return {
        "polling_unit_id": req.polling_unit_id,
        "before_date": req.before_date,
        "after_date": req.after_date,
        "source": "STAC preview assets",
        "before_scene": scene_metadata(before_scene, config.preview_asset),
        "after_scene": scene_metadata(after_scene, config.preview_asset),
        **result,
        "confidence_from_scene_cloud_cover": confidence,
        "recommendation": "field inspection recommended" if result["significant_change"] else "no material observed spectral change",
    }


@app.post("/analyze")
@app.post("/api/v1/satellite/validate-polling-unit")
async def validate_polling_unit(req: PollingUnitValidationRequest):
    return await analyze_polling_unit(req)


@app.post("/detect-changes")
@app.post("/api/v1/satellite/detect-changes")
async def detect_changes(req: ChangeDetectionRequest):
    return await analyze_changes(req)


@app.get("/status")
@app.get("/api/v1/satellite/health")
async def health():
    config = stac_config()
    try:
        async with httpx.AsyncClient(timeout=REQUEST_TIMEOUT_SECONDS, follow_redirects=True) as client:
            response = await client.get(config.api_url)
            response.raise_for_status()
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=503, detail=f"STAC service unavailable: {exc}") from exc
    return {
        "status": "healthy",
        "version": APP_VERSION,
        "imagery_source": "configured STAC API",
        "collection": config.collection,
        "preview_asset": config.preview_asset,
    }


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=int(os.getenv("PORT", "8204")), log_level="info")
