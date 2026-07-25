"""Geospatial analytics over authoritative polling-unit source data.

The pipeline creates lakehouse artifacts only from configured PostgreSQL polling-unit
records. When the authoritative source, mapped coordinates, or statistical support is
unavailable, each analysis returns a transparent ``unavailable`` status instead of
synthetic locations, zero-valued metrics, or assumed baseline observations.
"""

import logging
import math
import os
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Optional

logger = logging.getLogger(__name__)

_DEFAULT_LAKEHOUSE_DIR = Path(__file__).parent.parent / "data" / "lakehouse"
DATA_DIR = Path(os.environ.get("LAKEHOUSE_DIR", str(_DEFAULT_LAKEHOUSE_DIR)))
GEO_DIR = DATA_DIR / "geo"
GEO_DIR.mkdir(parents=True, exist_ok=True)


class GeoSourceUnavailable(RuntimeError):
    """Raised when authoritative geospatial source data cannot support analysis."""


class GeoAnalyticsPipeline:
    """Perform geospatial analytics only from authoritative mapped polling units."""

    def __init__(self, db_url: Optional[str] = None):
        self.db_url = (db_url or os.environ.get("DATABASE_URL", "")).strip()
        self._conn: Any = None
        self._pg: Any = None

    @staticmethod
    def _unavailable(analysis_type: str, reason: str) -> dict[str, str]:
        """Return the common explicit unavailable response contract."""
        return {
            "type": analysis_type,
            "status": "unavailable",
            "reason": reason,
        }

    @staticmethod
    def _require_number(value: Any, field_name: str) -> float:
        """Require a real numeric observation rather than supplying a fallback."""
        if value is None:
            raise GeoSourceUnavailable(
                f"authoritative geospatial data is missing {field_name}"
            )
        return float(value)

    @property
    def conn(self) -> Any:
        """Return a lazy DuckDB connection for authoritative lakehouse analysis."""
        if self._conn is None:
            try:
                import duckdb
            except ImportError as error:
                raise GeoSourceUnavailable(
                    "DuckDB is required for authoritative geospatial analysis"
                ) from error

            database_path = GEO_DIR / "geo_analytics.duckdb"
            self._conn = duckdb.connect(str(database_path))
            try:
                self._conn.execute("INSTALL spatial; LOAD spatial;")
            except Exception:
                try:
                    self._conn.execute("LOAD spatial;")
                except Exception:
                    logger.warning(
                        "duckdb_spatial_extension_unavailable",
                        exc_info=True,
                    )
        return self._conn

    @property
    def pg(self) -> Any:
        """Return an optional PostgreSQL connection for callers that require it."""
        if not self.db_url:
            return None
        if self._pg is None:
            try:
                import psycopg2
            except ImportError:
                logger.warning("psycopg2_not_available_for_postgis_queries")
                return None
            self._pg = psycopg2.connect(self.db_url)
            self._pg.autocommit = True
        return self._pg

    def ingest_pu_locations(self) -> dict[str, Any]:
        """Ingest real mapped polling units from PostgreSQL into the geo layer."""
        if not self.db_url:
            raise GeoSourceUnavailable(
                "DATABASE_URL is required for authoritative geospatial ingestion"
            )

        try:
            try:
                self.conn.execute("DETACH pg;")
            except Exception:
                # The alias is absent on the first ingestion of this connection.
                pass

            self.conn.execute("INSTALL postgres; LOAD postgres;")
            attachment = f"ATTACH '{self.db_url}' AS pg (TYPE POSTGRES, READ_ONLY);"
            self.conn.execute(attachment)
            self.conn.execute(
                """
                CREATE OR REPLACE TABLE pu_locations AS
                SELECT
                    pu.code AS pu_code,
                    pu.name AS pu_name,
                    pu.latitude,
                    pu.longitude,
                    pu.registered_voters,
                    w.name AS ward_name,
                    w.code AS ward_code,
                    l.name AS lga_name,
                    l.code AS lga_code,
                    l.state_code
                FROM pg.polling_units pu
                JOIN pg.wards w ON w.code = pu.ward_code
                JOIN pg.lgas l ON l.code = w.lga_code
                WHERE pu.latitude IS NOT NULL
                  AND pu.longitude IS NOT NULL
                  AND pu.registered_voters IS NOT NULL
                """
            )
            count = self.conn.execute("SELECT COUNT(*) FROM pu_locations").fetchone()[0]
            if count <= 0:
                raise GeoSourceUnavailable(
                    "authoritative polling-unit geospatial data has no mapped locations"
                )
            logger.info("authoritative_polling_units_ingested", extra={"count": count})
            return {"status": "ok", "count": count}
        except GeoSourceUnavailable:
            raise
        except Exception as error:
            logger.exception("authoritative_polling_unit_ingestion_failed")
            raise GeoSourceUnavailable(
                "authoritative polling-unit geospatial data is unavailable"
            ) from error

    def _ensure_pu_locations(self) -> None:
        """Ensure the local geo layer has real mapped polling-unit observations."""
        try:
            count = self.conn.execute("SELECT COUNT(*) FROM pu_locations").fetchone()[0]
        except Exception:
            ingestion = self.ingest_pu_locations()
            count = ingestion.get("count", 0)

        if count <= 0:
            raise GeoSourceUnavailable(
                "authoritative polling-unit geospatial data is unavailable"
            )

    def compute_hotspots(self, election_id: int = 1) -> dict[str, Any]:
        """Summarize real mapped polling-unit concentration by geographic grid."""
        del election_id  # The source is election-scoped by the configured database.
        analysis_type = "hotspot_analysis"
        try:
            self._ensure_pu_locations()
            grid_size = 0.5
            results = self.conn.execute(
                f"""
                SELECT
                    FLOOR(latitude / {grid_size}) * {grid_size} +
                        {grid_size} / 2 AS grid_lat,
                    FLOOR(longitude / {grid_size}) * {grid_size} +
                        {grid_size} / 2 AS grid_lng,
                    COUNT(*) AS pu_count,
                    SUM(registered_voters) AS total_registered,
                    AVG(registered_voters) AS avg_registered,
                    STDDEV(registered_voters) AS std_registered,
                    state_code,
                    LIST(DISTINCT lga_code) AS lga_codes
                FROM pu_locations
                GROUP BY grid_lat, grid_lng, state_code
                HAVING COUNT(*) > 5
                ORDER BY pu_count DESC
                LIMIT 50
                """
            ).fetchall()

            if not results:
                return self._unavailable(
                    analysis_type,
                    "insufficient authoritative mapped polling-unit data for hotspots",
                )

            hotspots: list[dict[str, Any]] = []
            for row in results:
                total_registered = self._require_number(
                    row[3], "hotspot registered-voter total"
                )
                average_registered = self._require_number(
                    row[4], "hotspot average registration"
                )
                standard_deviation = self._require_number(
                    row[5], "hotspot registration deviation"
                )
                hotspots.append(
                    {
                        "center": {"lat": row[0], "lng": row[1]},
                        "pu_count": row[2],
                        "total_registered": total_registered,
                        "avg_registered": round(average_registered, 1),
                        "std_registered": round(standard_deviation, 1),
                        "state_code": row[6],
                        "lga_codes": row[7] or [],
                    }
                )

            self.conn.execute(
                f"""
                COPY (
                    SELECT
                        FLOOR(latitude / {grid_size}) * {grid_size} +
                            {grid_size} / 2 AS grid_lat,
                        FLOOR(longitude / {grid_size}) * {grid_size} +
                            {grid_size} / 2 AS grid_lng,
                        COUNT(*) AS pu_count,
                        SUM(registered_voters) AS total_registered,
                        AVG(registered_voters) AS avg_registered,
                        state_code
                    FROM pu_locations
                    GROUP BY grid_lat, grid_lng, state_code
                    HAVING COUNT(*) > 5
                ) TO '{GEO_DIR}/hotspots.parquet' (FORMAT PARQUET)
                """
            )

            return {
                "type": analysis_type,
                "grid_size_deg": grid_size,
                "hotspot_count": len(hotspots),
                "hotspots": hotspots,
                "parquet_path": str(GEO_DIR / "hotspots.parquet"),
            }
        except GeoSourceUnavailable as error:
            return self._unavailable(analysis_type, str(error))
        except Exception:
            logger.exception("hotspot_analysis_failed")
            return self._unavailable(
                analysis_type,
                "geospatial hotspot analysis could not be completed",
            )

    def compute_coverage_gaps(self) -> dict[str, Any]:
        """Identify coverage gaps only from real mapped polling-unit observations."""
        analysis_type = "coverage_gap_analysis"
        try:
            self._ensure_pu_locations()
            results = self.conn.execute(
                """
                SELECT
                    state_code,
                    lga_code,
                    lga_name,
                    COUNT(*) AS pu_count,
                    SUM(registered_voters) AS total_registered,
                    CAST(SUM(registered_voters) AS DOUBLE) / COUNT(*)
                        AS voters_per_pu,
                    AVG(latitude) AS center_lat,
                    AVG(longitude) AS center_lng,
                    MAX(latitude) - MIN(latitude) AS lat_spread,
                    MAX(longitude) - MIN(longitude) AS lng_spread
                FROM pu_locations
                GROUP BY state_code, lga_code, lga_name
                ORDER BY voters_per_pu DESC
                """
            ).fetchall()

            if not results:
                return self._unavailable(
                    analysis_type,
                    "no authoritative mapped polling-unit observations are available",
                )

            gaps: list[dict[str, Any]] = []
            for row in results:
                registered = self._require_number(
                    row[4], "coverage-gap registered-voter total"
                )
                voters_per_pu = self._require_number(
                    row[5], "coverage-gap voters per polling unit"
                )
                center_latitude = self._require_number(
                    row[6], "coverage-gap center latitude"
                )
                center_longitude = self._require_number(
                    row[7], "coverage-gap center longitude"
                )
                latitude_spread = self._require_number(
                    row[8], "coverage-gap latitude spread"
                )
                longitude_spread = self._require_number(
                    row[9], "coverage-gap longitude spread"
                )
                area_km2 = (
                    latitude_spread
                    * 111
                    * longitude_spread
                    * 111
                    * math.cos(math.radians(center_latitude))
                )
                if area_km2 <= 0:
                    raise GeoSourceUnavailable(
                        "authoritative mapped polling-unit geometry cannot support "
                        "coverage-density analysis"
                    )
                density = row[3] / area_km2
                if voters_per_pu > 750:
                    severity = "high"
                elif voters_per_pu > 500:
                    severity = "medium"
                else:
                    severity = "low"

                gaps.append(
                    {
                        "state_code": row[0],
                        "lga_code": row[1],
                        "lga_name": row[2],
                        "pu_count": row[3],
                        "total_registered": registered,
                        "voters_per_pu": round(voters_per_pu, 1),
                        "center": {
                            "lat": center_latitude,
                            "lng": center_longitude,
                        },
                        "area_km2": round(area_km2, 1),
                        "pu_density_per_km2": round(density, 2),
                        "gap_severity": severity,
                    }
                )

            return {
                "type": analysis_type,
                "total_lgas": len(gaps),
                "high_gap_count": sum(
                    1 for gap in gaps if gap["gap_severity"] == "high"
                ),
                "gaps": gaps,
            }
        except GeoSourceUnavailable as error:
            return self._unavailable(analysis_type, str(error))
        except Exception:
            logger.exception("coverage_gap_analysis_failed")
            return self._unavailable(
                analysis_type,
                "geospatial coverage-gap analysis could not be completed",
            )

    def compute_spatial_autocorrelation(self) -> dict[str, Any]:
        """Compute Moran's I only where authoritative state observations support it."""
        analysis_type = "spatial_autocorrelation"
        try:
            self._ensure_pu_locations()
            state_data = self.conn.execute(
                """
                SELECT
                    state_code,
                    AVG(latitude) AS center_lat,
                    AVG(longitude) AS center_lng,
                    AVG(registered_voters) AS avg_voters,
                    COUNT(*) AS pu_count,
                    SUM(registered_voters) AS total_voters
                FROM pu_locations
                GROUP BY state_code
                ORDER BY state_code
                """
            ).fetchall()

            if len(state_data) < 3:
                return self._unavailable(
                    analysis_type,
                    "insufficient mapped state data for spatial autocorrelation",
                )

            observations: list[tuple[str, float, float, float, Any, Any]] = []
            for row in state_data:
                observations.append(
                    (
                        row[0],
                        self._require_number(row[1], "state center latitude"),
                        self._require_number(row[2], "state center longitude"),
                        self._require_number(
                            row[3], "state average registered-voter count"
                        ),
                        row[4],
                        row[5],
                    )
                )

            global_mean = sum(row[3] for row in observations) / len(observations)
            numerator = 0.0
            denominator = 0.0
            total_weight = 0.0

            for index, (_, lat_i, lng_i, value_i, _, _) in enumerate(observations):
                deviation_i = value_i - global_mean
                denominator += deviation_i**2
                for other_index, (_, lat_j, lng_j, value_j, _, _) in enumerate(
                    observations
                ):
                    if index == other_index:
                        continue
                    distance = math.sqrt((lat_i - lat_j) ** 2 + (lng_i - lng_j) ** 2)
                    if distance < 0.001:
                        continue
                    weight = 1.0 / distance
                    total_weight += weight
                    numerator += weight * deviation_i * (value_j - global_mean)

            if denominator <= 0 or total_weight <= 0:
                return self._unavailable(
                    analysis_type,
                    "authoritative state observations lack spatial variation",
                )

            morans_i = (len(observations) / total_weight) * (numerator / denominator)
            if morans_i > 0.3:
                interpretation = "clustered"
            elif morans_i < -0.3:
                interpretation = "dispersed"
            else:
                interpretation = "random"

            state_details = [
                {
                    "state_code": state_code,
                    "center": {"lat": latitude, "lng": longitude},
                    "avg_voters": round(average_voters, 1),
                    "pu_count": polling_unit_count,
                    "total_voters": total_voters,
                }
                for (
                    state_code,
                    latitude,
                    longitude,
                    average_voters,
                    polling_unit_count,
                    total_voters,
                ) in observations
            ]
            return {
                "type": analysis_type,
                "morans_i": round(morans_i, 4),
                "interpretation": interpretation,
                "global_mean_voters": round(global_mean, 1),
                "state_count": len(observations),
                "states": state_details,
            }
        except GeoSourceUnavailable as error:
            return self._unavailable(analysis_type, str(error))
        except Exception:
            logger.exception("spatial_autocorrelation_failed")
            return self._unavailable(
                analysis_type,
                "spatial autocorrelation could not be completed",
            )

    def generate_geo_features(self) -> dict[str, Any]:
        """Generate lakehouse feature artifacts solely from mapped source records."""
        try:
            self._ensure_pu_locations()
            self.conn.execute(
                f"""
                COPY (
                    SELECT
                        p.pu_code,
                        p.latitude,
                        p.longitude,
                        p.registered_voters,
                        p.state_code,
                        p.lga_code,
                        (6371 * acos(
                            cos(radians(p.latitude)) * cos(radians(s.avg_lat)) *
                            cos(radians(s.avg_lng) - radians(p.longitude)) +
                            sin(radians(p.latitude)) * sin(radians(s.avg_lat))
                        )) AS dist_to_state_centroid_km,
                        p.registered_voters - l.avg_reg AS reg_deviation,
                        CASE
                            WHEN s.std_reg > 0 THEN
                                (p.registered_voters - s.avg_reg) / s.std_reg
                            ELSE NULL
                        END AS reg_zscore
                    FROM pu_locations p
                    JOIN (
                        SELECT
                            state_code,
                            AVG(latitude) AS avg_lat,
                            AVG(longitude) AS avg_lng,
                            AVG(registered_voters) AS avg_reg,
                            STDDEV(registered_voters) AS std_reg
                        FROM pu_locations
                        GROUP BY state_code
                    ) s ON s.state_code = p.state_code
                    JOIN (
                        SELECT
                            lga_code,
                            AVG(registered_voters) AS avg_reg
                        FROM pu_locations
                        GROUP BY lga_code
                    ) l ON l.lga_code = p.lga_code
                ) TO '{GEO_DIR}/geo_features.parquet' (FORMAT PARQUET)
                """
            )
            count = self.conn.execute(
                f"SELECT COUNT(*) FROM read_parquet('{GEO_DIR}/geo_features.parquet')"
            ).fetchone()[0]
            if count <= 0:
                return {
                    "status": "unavailable",
                    "reason": "no authoritative geo features were generated",
                }
            return {
                "status": "ok",
                "feature_count": count,
                "features": [
                    "dist_to_state_centroid_km",
                    "reg_deviation",
                    "reg_zscore",
                ],
                "parquet_path": str(GEO_DIR / "geo_features.parquet"),
            }
        except GeoSourceUnavailable as error:
            return {"status": "unavailable", "reason": str(error)}
        except Exception:
            logger.exception("geo_feature_generation_failed")
            return {
                "status": "unavailable",
                "reason": "geospatial feature generation could not be completed",
            }

    def run_full_analysis(self, election_id: int = 1) -> dict[str, Any]:
        """Run all geospatial analysis stages with transparent source availability."""
        started = datetime.now(timezone.utc)
        try:
            ingestion = self.ingest_pu_locations()
        except GeoSourceUnavailable as error:
            ingestion = {"status": "unavailable", "reason": str(error)}

        results = {
            "ingestion": ingestion,
            "hotspots": self.compute_hotspots(election_id),
            "coverage_gaps": self.compute_coverage_gaps(),
            "spatial_autocorrelation": self.compute_spatial_autocorrelation(),
            "geo_features": self.generate_geo_features(),
        }
        elapsed = (datetime.now(timezone.utc) - started).total_seconds()
        results["elapsed_seconds"] = round(elapsed, 2)
        results["timestamp"] = datetime.now(timezone.utc).isoformat()
        return results
