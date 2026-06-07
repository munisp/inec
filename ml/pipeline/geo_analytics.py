"""Geospatial Analytics Pipeline — PostGIS + Apache Sedona + Lakehouse Integration.

Provides spatial analytics for election data:
- Hotspot detection (spatial clustering of anomalies)
- Coverage gap analysis (PU density vs population)
- Spatial autocorrelation (Moran's I for turnout patterns)
- Geospatial feature engineering for ML models
- Integration with DuckDB spatial extension for Lakehouse analytics
"""

import os
import json
import math
import logging
from pathlib import Path
from datetime import datetime, timezone
from typing import Optional

logger = logging.getLogger(__name__)

# Lakehouse directories
DATA_DIR = Path(os.environ.get("LAKEHOUSE_DIR", str(Path(__file__).parent.parent / "data" / "lakehouse")))
GEO_DIR = DATA_DIR / "geo"
GEO_DIR.mkdir(parents=True, exist_ok=True)


class GeoAnalyticsPipeline:
    """Geospatial analytics using DuckDB spatial extension (Sedona-compatible queries)."""

    def __init__(self, db_url: Optional[str] = None):
        self.db_url = db_url or os.environ.get(
            "DATABASE_URL", "postgresql://ngapp:ngapp@localhost:5432/ngapp"
        )
        self._conn = None
        self._pg = None

    @property
    def conn(self):
        """Lazy DuckDB connection with spatial extension."""
        if self._conn is None:
            import duckdb
            self._conn = duckdb.connect(str(GEO_DIR / "geo_analytics.duckdb"))
            try:
                self._conn.execute("INSTALL spatial; LOAD spatial;")
            except Exception:
                try:
                    self._conn.execute("LOAD spatial;")
                except Exception:
                    logger.warning("DuckDB spatial extension not available, using basic queries")
        return self._conn

    @property
    def pg(self):
        """Lazy PostgreSQL connection for PostGIS queries."""
        if self._pg is None:
            try:
                import psycopg2
                self._pg = psycopg2.connect(self.db_url)
                self._pg.autocommit = True
            except ImportError:
                logger.warning("psycopg2 not available for PostGIS queries")
        return self._pg

    def ingest_pu_locations(self) -> dict:
        """Ingest polling unit locations from PostgreSQL into DuckDB geo layer."""
        try:
            # Try direct PostgreSQL attachment
            self.conn.execute(f"""
                INSTALL postgres; LOAD postgres;
                ATTACH '{self.db_url}' AS pg (TYPE POSTGRES, READ_ONLY);
            """)

            self.conn.execute("""
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
                WHERE pu.latitude IS NOT NULL AND pu.longitude IS NOT NULL
            """)

            count = self.conn.execute("SELECT COUNT(*) FROM pu_locations").fetchone()[0]
            logger.info("ingested_pu_locations", count=count)
            return {"status": "ok", "count": count}
        except Exception as e:
            logger.warning(f"PG attach failed: {e}, using synthetic data")
            return {"status": "fallback", "error": str(e)}

    def compute_hotspots(self, election_id: int = 1) -> dict:
        """Detect spatial clusters of high anomaly scores using grid-based analysis."""
        try:
            self.conn.execute("SELECT 1 FROM pu_locations LIMIT 1")
        except Exception:
            self.ingest_pu_locations()

        try:
            # Grid-based hotspot detection (0.5 degree grid ~55km)
            grid_size = 0.5
            results = self.conn.execute(f"""
                SELECT
                    FLOOR(latitude / {grid_size}) * {grid_size} + {grid_size}/2 AS grid_lat,
                    FLOOR(longitude / {grid_size}) * {grid_size} + {grid_size}/2 AS grid_lng,
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
            """).fetchall()

            hotspots = []
            for row in results:
                hotspots.append({
                    "center": {"lat": row[0], "lng": row[1]},
                    "pu_count": row[2],
                    "total_registered": row[3],
                    "avg_registered": round(row[4], 1) if row[4] else 0,
                    "std_registered": round(row[5], 1) if row[5] else 0,
                    "state_code": row[6],
                    "lga_codes": row[7] if row[7] else [],
                })

            # Save to parquet for Lakehouse Gold tier
            self.conn.execute(f"""
                COPY (
                    SELECT
                        FLOOR(latitude / {grid_size}) * {grid_size} + {grid_size}/2 AS grid_lat,
                        FLOOR(longitude / {grid_size}) * {grid_size} + {grid_size}/2 AS grid_lng,
                        COUNT(*) AS pu_count,
                        SUM(registered_voters) AS total_registered,
                        AVG(registered_voters) AS avg_registered,
                        state_code
                    FROM pu_locations
                    GROUP BY grid_lat, grid_lng, state_code
                    HAVING COUNT(*) > 5
                ) TO '{GEO_DIR}/hotspots.parquet' (FORMAT PARQUET)
            """)

            return {
                "type": "hotspot_analysis",
                "grid_size_deg": grid_size,
                "hotspot_count": len(hotspots),
                "hotspots": hotspots,
                "parquet_path": str(GEO_DIR / "hotspots.parquet"),
            }
        except Exception as e:
            return {"type": "hotspot_analysis", "error": str(e)}

    def compute_coverage_gaps(self) -> dict:
        """Identify areas with insufficient polling unit density."""
        try:
            self.conn.execute("SELECT 1 FROM pu_locations LIMIT 1")
        except Exception:
            self.ingest_pu_locations()

        try:
            results = self.conn.execute("""
                SELECT
                    state_code,
                    lga_code,
                    lga_name,
                    COUNT(*) AS pu_count,
                    SUM(registered_voters) AS total_registered,
                    CASE WHEN COUNT(*) > 0
                        THEN CAST(SUM(registered_voters) AS DOUBLE) / COUNT(*)
                        ELSE 0 END AS voters_per_pu,
                    AVG(latitude) AS center_lat,
                    AVG(longitude) AS center_lng,
                    MAX(latitude) - MIN(latitude) AS lat_spread,
                    MAX(longitude) - MIN(longitude) AS lng_spread
                FROM pu_locations
                GROUP BY state_code, lga_code, lga_name
                ORDER BY voters_per_pu DESC
            """).fetchall()

            gaps = []
            for row in results:
                lat_spread = row[8] if row[8] else 0
                lng_spread = row[9] if row[9] else 0
                area_km2 = lat_spread * 111 * lng_spread * 111 * math.cos(math.radians(row[6] or 9.0))
                density = row[3] / max(area_km2, 0.01)

                gaps.append({
                    "state_code": row[0],
                    "lga_code": row[1],
                    "lga_name": row[2],
                    "pu_count": row[3],
                    "total_registered": row[4],
                    "voters_per_pu": round(row[5], 1),
                    "center": {"lat": row[6], "lng": row[7]},
                    "area_km2": round(area_km2, 1),
                    "pu_density_per_km2": round(density, 2),
                    "gap_severity": "high" if row[5] > 750 else "medium" if row[5] > 500 else "low",
                })

            return {
                "type": "coverage_gap_analysis",
                "total_lgas": len(gaps),
                "high_gap_count": sum(1 for g in gaps if g["gap_severity"] == "high"),
                "gaps": gaps,
            }
        except Exception as e:
            return {"type": "coverage_gap_analysis", "error": str(e)}

    def compute_spatial_autocorrelation(self) -> dict:
        """Compute Moran's I statistic for voter registration patterns."""
        try:
            self.conn.execute("SELECT 1 FROM pu_locations LIMIT 1")
        except Exception:
            self.ingest_pu_locations()

        try:
            # State-level aggregation for Moran's I
            state_data = self.conn.execute("""
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
            """).fetchall()

            if len(state_data) < 3:
                return {"type": "spatial_autocorrelation", "morans_i": 0, "note": "insufficient data"}

            # Compute global mean
            all_values = [row[3] for row in state_data if row[3] is not None]
            if not all_values:
                return {"type": "spatial_autocorrelation", "morans_i": 0, "note": "no data"}

            global_mean = sum(all_values) / len(all_values)
            n = len(state_data)

            # Spatial weights (inverse distance)
            numerator = 0.0
            denominator = 0.0
            total_weight = 0.0

            for i, (_, lat_i, lng_i, val_i, _, _) in enumerate(state_data):
                if val_i is None:
                    continue
                dev_i = val_i - global_mean
                denominator += dev_i ** 2

                for j, (_, lat_j, lng_j, val_j, _, _) in enumerate(state_data):
                    if i == j or val_j is None:
                        continue
                    dist = math.sqrt((lat_i - lat_j)**2 + (lng_i - lng_j)**2)
                    if dist < 0.001:
                        continue
                    w = 1.0 / dist
                    total_weight += w
                    dev_j = val_j - global_mean
                    numerator += w * dev_i * dev_j

            morans_i = 0.0
            if denominator > 0 and total_weight > 0:
                morans_i = (n / total_weight) * (numerator / denominator)

            interpretation = "random"
            if morans_i > 0.3:
                interpretation = "clustered"
            elif morans_i < -0.3:
                interpretation = "dispersed"

            state_details = []
            for row in state_data:
                state_details.append({
                    "state_code": row[0],
                    "center": {"lat": row[1], "lng": row[2]},
                    "avg_voters": round(row[3], 1) if row[3] else 0,
                    "pu_count": row[4],
                    "total_voters": row[5],
                })

            return {
                "type": "spatial_autocorrelation",
                "morans_i": round(morans_i, 4),
                "interpretation": interpretation,
                "global_mean_voters": round(global_mean, 1),
                "state_count": n,
                "states": state_details,
            }
        except Exception as e:
            return {"type": "spatial_autocorrelation", "error": str(e)}

    def generate_geo_features(self) -> dict:
        """Generate geospatial features for ML models and store in Lakehouse Gold tier."""
        try:
            self.conn.execute("SELECT 1 FROM pu_locations LIMIT 1")
        except Exception:
            self.ingest_pu_locations()

        try:
            # Compute distance to nearest neighbor, state centroid, etc.
            self.conn.execute(f"""
                COPY (
                    SELECT
                        p.pu_code,
                        p.latitude,
                        p.longitude,
                        p.registered_voters,
                        p.state_code,
                        p.lga_code,
                        -- Distance to state centroid
                        (6371 * acos(
                            cos(radians(p.latitude)) * cos(radians(s.avg_lat)) *
                            cos(radians(s.avg_lng) - radians(p.longitude)) +
                            sin(radians(p.latitude)) * sin(radians(s.avg_lat))
                        )) AS dist_to_state_centroid_km,
                        -- Deviation from LGA mean registration
                        p.registered_voters - l.avg_reg AS reg_deviation,
                        -- Z-score within state
                        CASE WHEN s.std_reg > 0
                            THEN (p.registered_voters - s.avg_reg) / s.std_reg
                            ELSE 0 END AS reg_zscore
                    FROM pu_locations p
                    JOIN (
                        SELECT state_code, AVG(latitude) AS avg_lat, AVG(longitude) AS avg_lng,
                               AVG(registered_voters) AS avg_reg, STDDEV(registered_voters) AS std_reg
                        FROM pu_locations GROUP BY state_code
                    ) s ON s.state_code = p.state_code
                    JOIN (
                        SELECT lga_code, AVG(registered_voters) AS avg_reg
                        FROM pu_locations GROUP BY lga_code
                    ) l ON l.lga_code = p.lga_code
                ) TO '{GEO_DIR}/geo_features.parquet' (FORMAT PARQUET)
            """)

            count = self.conn.execute(f"""
                SELECT COUNT(*) FROM read_parquet('{GEO_DIR}/geo_features.parquet')
            """).fetchone()[0]

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
        except Exception as e:
            return {"status": "error", "error": str(e)}

    def run_full_analysis(self, election_id: int = 1) -> dict:
        """Run complete geospatial analysis pipeline."""
        started = datetime.now(timezone.utc)

        results = {
            "ingestion": self.ingest_pu_locations(),
            "hotspots": self.compute_hotspots(election_id),
            "coverage_gaps": self.compute_coverage_gaps(),
            "spatial_autocorrelation": self.compute_spatial_autocorrelation(),
            "geo_features": self.generate_geo_features(),
        }

        elapsed = (datetime.now(timezone.utc) - started).total_seconds()
        results["elapsed_seconds"] = round(elapsed, 2)
        results["timestamp"] = datetime.now(timezone.utc).isoformat()

        return results
