"""
INEC GeoLibre Spatial Analytics — Python Pipeline

Provides advanced geospatial analytics that complement the Rust spatial engine,
focusing on statistical analysis, anomaly detection, and report generation.

Modules:
- Spatial autocorrelation (Moran's I, Getis-Ord Gi*)
- Geographic weighted regression for turnout prediction
- Benford's law analysis with spatial context
- Election result interpolation (IDW, Kriging)
- Admin boundary enrichment from GeoJSON
"""

from __future__ import annotations
import json
import math
import logging
from dataclasses import dataclass, field, asdict
from typing import Optional

logger = logging.getLogger("inec.geo_spatial_analytics")

# ─── Data Types ──────────────────────────────────────────────────────────


@dataclass
class GeoPoint:
    longitude: float
    latitude: float
    properties: dict = field(default_factory=dict)


@dataclass
class SpatialAnalysisResult:
    analysis_type: str
    features: list[dict] = field(default_factory=list)
    statistics: dict = field(default_factory=dict)
    metadata: dict = field(default_factory=dict)

    def to_geojson(self) -> dict:
        return {
            "type": "FeatureCollection",
            "features": self.features,
            "metadata": {
                "analysis": self.analysis_type,
                "statistics": self.statistics,
                **self.metadata,
            },
        }


# ─── Haversine Distance ─────────────────────────────────────────────────


def haversine_km(lat1: float, lon1: float, lat2: float, lon2: float) -> float:
    R = 6371.0
    dlat = math.radians(lat2 - lat1)
    dlon = math.radians(lon2 - lon1)
    a = (
        math.sin(dlat / 2) ** 2
        + math.cos(math.radians(lat1))
        * math.cos(math.radians(lat2))
        * math.sin(dlon / 2) ** 2
    )
    return R * 2 * math.asin(math.sqrt(a))


# ─── Moran's I — Spatial Autocorrelation ────────────────────────────────


def morans_i(points: list[GeoPoint], value_field: str, bandwidth_km: float = 50.0) -> SpatialAnalysisResult:
    """
    Calculate Moran's I statistic for spatial autocorrelation.

    A positive Moran's I indicates clustering (similar values near each other).
    A negative value indicates dispersion.
    Near zero indicates randomness.
    """
    n = len(points)
    if n < 3:
        return SpatialAnalysisResult(
            analysis_type="morans_i",
            statistics={"error": "Need at least 3 points"},
        )

    values = [p.properties.get(value_field, 0.0) for p in points]
    mean_val = sum(values) / n

    # Weight matrix (binary: 1 if within bandwidth, 0 otherwise)
    W = 0.0
    numerator = 0.0
    denominator = sum((v - mean_val) ** 2 for v in values)

    if denominator == 0:
        return SpatialAnalysisResult(
            analysis_type="morans_i",
            statistics={"morans_i": 0.0, "interpretation": "no variance"},
        )

    for i in range(n):
        for j in range(n):
            if i == j:
                continue
            dist = haversine_km(
                points[i].latitude, points[i].longitude,
                points[j].latitude, points[j].longitude,
            )
            if dist <= bandwidth_km:
                w_ij = 1.0
                W += w_ij
                numerator += w_ij * (values[i] - mean_val) * (values[j] - mean_val)

    if W == 0:
        return SpatialAnalysisResult(
            analysis_type="morans_i",
            statistics={"morans_i": 0.0, "interpretation": "no neighbors within bandwidth"},
        )

    I = (n / W) * (numerator / denominator)

    interpretation = "random"
    if I > 0.3:
        interpretation = "strong positive autocorrelation (clustered)"
    elif I > 0.1:
        interpretation = "moderate positive autocorrelation"
    elif I < -0.3:
        interpretation = "strong negative autocorrelation (dispersed)"
    elif I < -0.1:
        interpretation = "moderate negative autocorrelation"

    return SpatialAnalysisResult(
        analysis_type="morans_i",
        statistics={
            "morans_i": round(I, 4),
            "interpretation": interpretation,
            "sample_size": n,
            "bandwidth_km": bandwidth_km,
            "total_weights": W,
        },
    )


# ─── Getis-Ord Gi* — Hotspot Analysis ───────────────────────────────────


def getis_ord_gi_star(
    points: list[GeoPoint], value_field: str, bandwidth_km: float = 50.0
) -> SpatialAnalysisResult:
    """
    Calculate Getis-Ord Gi* statistic for hotspot detection.

    High positive Gi* → hot spot (cluster of high values)
    High negative Gi* → cold spot (cluster of low values)
    """
    n = len(points)
    if n < 3:
        return SpatialAnalysisResult(
            analysis_type="getis_ord_gi_star",
            statistics={"error": "Need at least 3 points"},
        )

    values = [float(p.properties.get(value_field, 0.0)) for p in points]
    mean_val = sum(values) / n
    s = math.sqrt(sum(v ** 2 for v in values) / n - mean_val ** 2)

    if s == 0:
        return SpatialAnalysisResult(
            analysis_type="getis_ord_gi_star",
            statistics={"error": "zero variance"},
        )

    features = []
    hotspots = []
    coldspots = []

    for i in range(n):
        w_sum = 0.0
        wx_sum = 0.0
        w2_sum = 0.0

        for j in range(n):
            dist = haversine_km(
                points[i].latitude, points[i].longitude,
                points[j].latitude, points[j].longitude,
            )
            w = 1.0 if dist <= bandwidth_km else 0.0
            w_sum += w
            wx_sum += w * values[j]
            w2_sum += w * w

        if w_sum == 0:
            gi_star = 0.0
        else:
            numerator_gi = wx_sum - mean_val * w_sum
            denominator_gi = s * math.sqrt((n * w2_sum - w_sum ** 2) / (n - 1))
            gi_star = numerator_gi / denominator_gi if denominator_gi != 0 else 0.0

        label = "not significant"
        if gi_star > 2.58:
            label = "hot spot (99% confidence)"
            hotspots.append(i)
        elif gi_star > 1.96:
            label = "hot spot (95% confidence)"
            hotspots.append(i)
        elif gi_star > 1.65:
            label = "hot spot (90% confidence)"
            hotspots.append(i)
        elif gi_star < -2.58:
            label = "cold spot (99% confidence)"
            coldspots.append(i)
        elif gi_star < -1.96:
            label = "cold spot (95% confidence)"
            coldspots.append(i)
        elif gi_star < -1.65:
            label = "cold spot (90% confidence)"
            coldspots.append(i)

        feature = {
            "type": "Feature",
            "geometry": {
                "type": "Point",
                "coordinates": [points[i].longitude, points[i].latitude],
            },
            "properties": {
                **points[i].properties,
                "gi_star": round(gi_star, 4),
                "label": label,
                value_field: values[i],
            },
        }
        features.append(feature)

    return SpatialAnalysisResult(
        analysis_type="getis_ord_gi_star",
        features=features,
        statistics={
            "hot_spots": len(hotspots),
            "cold_spots": len(coldspots),
            "not_significant": n - len(hotspots) - len(coldspots),
            "bandwidth_km": bandwidth_km,
            "value_field": value_field,
        },
    )


# ─── Inverse Distance Weighting (IDW) Interpolation ─────────────────────


def idw_interpolation(
    known_points: list[GeoPoint],
    value_field: str,
    grid_points: list[tuple[float, float]],
    power: float = 2.0,
) -> SpatialAnalysisResult:
    """
    Interpolate unknown values at grid points using IDW.
    Useful for estimating turnout in areas without data.
    """
    features = []
    values = [float(p.properties.get(value_field, 0.0)) for p in known_points]

    for lng, lat in grid_points:
        weights = []
        for i, kp in enumerate(known_points):
            dist = haversine_km(lat, lng, kp.latitude, kp.longitude)
            if dist < 0.001:
                weights = [(1.0, values[i])]
                break
            weights.append((1.0 / (dist ** power), values[i]))

        total_weight = sum(w for w, _ in weights)
        if total_weight == 0:
            continue

        interpolated = sum(w * v for w, v in weights) / total_weight

        features.append({
            "type": "Feature",
            "geometry": {"type": "Point", "coordinates": [lng, lat]},
            "properties": {
                f"interpolated_{value_field}": round(interpolated, 2),
            },
        })

    return SpatialAnalysisResult(
        analysis_type="idw_interpolation",
        features=features,
        statistics={
            "known_points": len(known_points),
            "interpolated_points": len(features),
            "power": power,
            "value_field": value_field,
        },
    )


# ─── Benford's Law with Spatial Context ──────────────────────────────────


def benfords_law_spatial(
    points: list[GeoPoint], value_field: str
) -> SpatialAnalysisResult:
    """
    Apply Benford's Law analysis to election data with spatial grouping.
    Compares observed first-digit distribution to expected Benford distribution.
    High deviation in a spatial cluster may indicate data fabrication.
    """
    expected = {d: math.log10(1 + 1.0 / d) for d in range(1, 10)}
    observed: dict[int, int] = {d: 0 for d in range(1, 10)}
    total = 0

    # Group by state_code for spatial context
    state_groups: dict[str, list[float]] = {}

    for p in points:
        val = p.properties.get(value_field, 0)
        if val is None or val == 0:
            continue
        first_digit = int(str(abs(int(val)))[0])
        if 1 <= first_digit <= 9:
            observed[first_digit] += 1
            total += 1

        state = p.properties.get("state_code", "unknown")
        state_groups.setdefault(state, []).append(float(val))

    if total == 0:
        return SpatialAnalysisResult(
            analysis_type="benfords_law",
            statistics={"error": "no valid values"},
        )

    # Chi-squared test
    chi_sq = 0.0
    digit_analysis = []
    for d in range(1, 10):
        obs_pct = observed[d] / total
        exp_pct = expected[d]
        chi_sq += ((obs_pct - exp_pct) ** 2) / exp_pct
        digit_analysis.append({
            "digit": d,
            "observed_pct": round(obs_pct * 100, 2),
            "expected_pct": round(exp_pct * 100, 2),
            "deviation": round((obs_pct - exp_pct) * 100, 2),
        })

    # Per-state analysis
    state_deviations = {}
    for state, vals in state_groups.items():
        state_obs: dict[int, int] = {d: 0 for d in range(1, 10)}
        state_total = 0
        for v in vals:
            if v == 0:
                continue
            fd = int(str(abs(int(v)))[0])
            if 1 <= fd <= 9:
                state_obs[fd] += 1
                state_total += 1
        if state_total < 10:
            continue
        state_chi = sum(
            ((state_obs[d] / state_total - expected[d]) ** 2) / expected[d]
            for d in range(1, 10)
        )
        state_deviations[state] = round(state_chi, 4)

    conformity = "conforms"
    if chi_sq > 0.05:
        conformity = "minor deviation"
    if chi_sq > 0.15:
        conformity = "significant deviation — investigate"
    if chi_sq > 0.30:
        conformity = "strong deviation — likely anomalous"

    flagged_states = [
        s for s, chi in state_deviations.items() if chi > 0.15
    ]

    return SpatialAnalysisResult(
        analysis_type="benfords_law",
        statistics={
            "chi_squared": round(chi_sq, 6),
            "conformity": conformity,
            "sample_size": total,
            "digit_analysis": digit_analysis,
            "flagged_states": flagged_states,
            "state_deviations": state_deviations,
        },
    )


# ─── Election Coverage Analysis ──────────────────────────────────────────


def coverage_analysis(
    pu_points: list[GeoPoint],
    population_points: list[GeoPoint],
    max_distance_km: float = 10.0,
) -> SpatialAnalysisResult:
    """
    Calculate what percentage of population centers are within
    max_distance_km of a polling unit.
    """
    covered = 0
    uncovered_features = []

    for pop in population_points:
        min_dist = float("inf")
        nearest_pu = None

        for pu in pu_points:
            dist = haversine_km(
                pop.latitude, pop.longitude,
                pu.latitude, pu.longitude,
            )
            if dist < min_dist:
                min_dist = dist
                nearest_pu = pu.properties.get("code", "unknown")

        if min_dist <= max_distance_km:
            covered += 1
        else:
            uncovered_features.append({
                "type": "Feature",
                "geometry": {
                    "type": "Point",
                    "coordinates": [pop.longitude, pop.latitude],
                },
                "properties": {
                    **pop.properties,
                    "nearest_pu": nearest_pu,
                    "distance_to_nearest_km": round(min_dist, 2),
                    "status": "uncovered",
                },
            })

    total = len(population_points)
    coverage_pct = (covered / total * 100) if total > 0 else 0

    return SpatialAnalysisResult(
        analysis_type="coverage_analysis",
        features=uncovered_features,
        statistics={
            "total_population_centers": total,
            "covered": covered,
            "uncovered": len(uncovered_features),
            "coverage_pct": round(coverage_pct, 2),
            "max_distance_km": max_distance_km,
        },
    )


# ─── FastAPI Endpoints ───────────────────────────────────────────────────

def create_geo_analytics_routes(app):
    """Register GeoLibre analytics routes on a FastAPI app."""
    from fastapi import FastAPI
    from pydantic import BaseModel

    class SpatialRequest(BaseModel):
        points: list[dict]
        value_field: str = "turnout_pct"
        bandwidth_km: float = 50.0

    @app.post("/geolibre/analytics/morans-i")
    async def api_morans_i(req: SpatialRequest):
        pts = [GeoPoint(p["longitude"], p["latitude"], p.get("properties", {})) for p in req.points]
        result = morans_i(pts, req.value_field, req.bandwidth_km)
        return result.to_geojson()

    @app.post("/geolibre/analytics/hotspots")
    async def api_hotspots(req: SpatialRequest):
        pts = [GeoPoint(p["longitude"], p["latitude"], p.get("properties", {})) for p in req.points]
        result = getis_ord_gi_star(pts, req.value_field, req.bandwidth_km)
        return result.to_geojson()

    @app.post("/geolibre/analytics/benfords-law")
    async def api_benfords(req: SpatialRequest):
        pts = [GeoPoint(p["longitude"], p["latitude"], p.get("properties", {})) for p in req.points]
        result = benfords_law_spatial(pts, req.value_field)
        return result.to_geojson()

    logger.info("GeoLibre analytics routes registered")
