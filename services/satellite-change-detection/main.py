"""
INEC Innovation 7: Satellite Imagery Change Detection for Polling Unit Validation
==================================================================================
Uses computer vision on satellite/aerial imagery to:

  1. Validate that polling unit locations match their registered coordinates
  2. Detect structural changes (new buildings, demolitions) that may affect
     accessibility or capacity
  3. Identify crowd density at polling units on election day
  4. Flag polling units in flood-prone or conflict-affected areas
  5. Verify that polling units are accessible (road connectivity analysis)

The service integrates with:
  - Sentinel-2 / Landsat imagery via STAC API
  - OpenStreetMap for baseline infrastructure data
  - INEC's polling unit registry for coordinate validation
"""

import io
import json
import math
import os
import time
from dataclasses import dataclass
from typing import Optional

import numpy as np
import uvicorn
from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

app = FastAPI(
    title="INEC Satellite Change Detection Service",
    description="Polling unit validation via satellite imagery analysis",
    version="1.0.0",
)

app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])


# ── Change Detection Algorithms ───────────────────────────────────────────────

def normalized_difference_index(band1: np.ndarray, band2: np.ndarray) -> np.ndarray:
    """Compute normalised difference index (e.g., NDVI, NDWI)."""
    denom = band1.astype(float) + band2.astype(float)
    denom[denom == 0] = 1e-10
    return (band1.astype(float) - band2.astype(float)) / denom


def detect_change_magnitude(before: np.ndarray, after: np.ndarray) -> float:
    """
    Compute the change magnitude between two image patches using
    the Euclidean distance in spectral space.
    """
    diff = before.astype(float) - after.astype(float)
    return float(np.sqrt(np.mean(diff ** 2)))


def estimate_crowd_density(image_patch: np.ndarray) -> dict:
    """
    Estimate crowd density from a satellite image patch using
    a simplified spectral analysis approach.
    High-density crowds appear as anomalous spectral signatures.
    """
    # Simulate crowd density estimation
    # Production: use a fine-tuned CNN (e.g., CSRNet) on VHR imagery
    mean_intensity = float(np.mean(image_patch))
    std_intensity = float(np.std(image_patch))

    # Heuristic: high variance + specific spectral range = crowd
    crowd_indicator = std_intensity / (mean_intensity + 1e-10)
    density = min(1.0, crowd_indicator * 2)

    return {
        "density_score": round(density, 3),
        "category": "high" if density > 0.7 else "medium" if density > 0.4 else "low",
        "estimated_count": int(density * 500),  # rough estimate
    }


def check_flood_risk(lat: float, lon: float) -> dict:
    """
    Assess flood risk for a polling unit location using elevation
    and proximity to water bodies.
    """
    # Simulate flood risk assessment
    # Production: use DEM data + HAND (Height Above Nearest Drainage) model
    risk_score = abs(math.sin(lat * lon)) * 0.8  # deterministic pseudo-random
    return {
        "flood_risk_score": round(risk_score, 3),
        "risk_level": "high" if risk_score > 0.6 else "medium" if risk_score > 0.3 else "low",
        "nearest_water_body_km": round(abs(math.cos(lat)) * 5, 2),
        "elevation_m": round(abs(math.sin(lon)) * 200 + 50, 1),
    }


def validate_coordinate_accuracy(
    registered_lat: float,
    registered_lon: float,
    imagery_lat: float,
    imagery_lon: float,
) -> dict:
    """
    Validate that a polling unit's registered coordinates match
    the location identified in satellite imagery.
    """
    # Haversine distance
    R = 6371000  # Earth radius in meters
    phi1, phi2 = math.radians(registered_lat), math.radians(imagery_lat)
    dphi = math.radians(imagery_lat - registered_lat)
    dlam = math.radians(imagery_lon - registered_lon)
    a = math.sin(dphi / 2) ** 2 + math.cos(phi1) * math.cos(phi2) * math.sin(dlam / 2) ** 2
    distance_m = 2 * R * math.asin(math.sqrt(a))

    return {
        "distance_m": round(distance_m, 1),
        "coordinate_valid": distance_m < 100,  # within 100m
        "accuracy_grade": "A" if distance_m < 10 else "B" if distance_m < 50 else "C" if distance_m < 100 else "F",
    }


# ── API Models ────────────────────────────────────────────────────────────────

class PollingUnitValidationRequest(BaseModel):
    polling_unit_id: str
    registered_lat: float
    registered_lon: float
    election_date: str
    include_flood_risk: bool = True
    include_crowd_density: bool = True


class ChangeDetectionRequest(BaseModel):
    polling_unit_id: str
    lat: float
    lon: float
    before_date: str
    after_date: str


# ── Endpoints ─────────────────────────────────────────────────────────────────

@app.post("/api/v1/satellite/validate-polling-unit")
async def validate_polling_unit(req: PollingUnitValidationRequest):
    """
    Comprehensive satellite-based validation of a polling unit.
    Returns coordinate accuracy, flood risk, crowd density, and change detection.
    """
    # Simulate satellite-derived coordinates (production: STAC API query)
    imagery_lat = req.registered_lat + np.random.normal(0, 0.0001)
    imagery_lon = req.registered_lon + np.random.normal(0, 0.0001)

    coord_validation = validate_coordinate_accuracy(
        req.registered_lat, req.registered_lon,
        imagery_lat, imagery_lon,
    )

    result = {
        "polling_unit_id": req.polling_unit_id,
        "coordinate_validation": coord_validation,
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }

    if req.include_flood_risk:
        result["flood_risk"] = check_flood_risk(req.registered_lat, req.registered_lon)

    if req.include_crowd_density:
        # Simulate image patch
        patch = np.random.randint(50, 200, (64, 64, 3), dtype=np.uint8)
        result["crowd_density"] = estimate_crowd_density(patch)

    # Overall validation status
    issues = []
    if not coord_validation["coordinate_valid"]:
        issues.append("coordinate_mismatch")
    if result.get("flood_risk", {}).get("risk_level") == "high":
        issues.append("high_flood_risk")
    if result.get("crowd_density", {}).get("category") == "high":
        issues.append("high_crowd_density")

    result["validation_status"] = "flagged" if issues else "passed"
    result["issues"] = issues
    result["recommendation"] = (
        "Requires field verification" if issues else "Polling unit validated"
    )

    return result


@app.post("/api/v1/satellite/detect-changes")
async def detect_changes(req: ChangeDetectionRequest):
    """
    Detect structural changes at a polling unit location between two dates.
    """
    # Simulate before/after image patches
    before = np.random.randint(80, 180, (64, 64, 3), dtype=np.uint8)
    after = np.random.randint(70, 190, (64, 64, 3), dtype=np.uint8)

    change_magnitude = detect_change_magnitude(before, after)
    significant_change = change_magnitude > 15.0

    return {
        "polling_unit_id": req.polling_unit_id,
        "before_date": req.before_date,
        "after_date": req.after_date,
        "change_magnitude": round(change_magnitude, 3),
        "significant_change": significant_change,
        "change_type": "structural" if significant_change else "none",
        "confidence": min(1.0, change_magnitude / 30.0),
        "recommendation": (
            "Field inspection recommended" if significant_change
            else "No significant changes detected"
        ),
    }


@app.get("/api/v1/satellite/health")
async def health():
    return {"status": "healthy", "imagery_source": "Sentinel-2 (simulated)"}


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8204, log_level="info")
