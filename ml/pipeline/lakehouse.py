"""INEC Lakehouse Pipeline — Bronze → Silver → Gold Data Tiers.

Implements a medallion architecture for election data:

Bronze (Raw):
  - Raw election results ingested from PostgreSQL/Kafka
  - No transformations, append-only, partitioned by date
  - Stored as Parquet files in data/lakehouse/bronze/

Silver (Cleaned):
  - Deduplicated, validated, enriched with computed features
  - Benford deviation, turnout ratios, regional comparisons
  - Stored as Parquet files in data/lakehouse/silver/

Gold (Aggregated):
  - ML-ready feature matrices
  - Precomputed aggregations (state/LGA/ward level)
  - Anomaly scores, integrity metrics
  - Stored as Parquet files in data/lakehouse/gold/

DuckDB serves as the query engine across all tiers.
"""

import os
import json
import hashlib
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import duckdb
import numpy as np
import pandas as pd
import structlog

log = structlog.get_logger()

LAKEHOUSE_DIR = Path(os.getenv("LAKEHOUSE_DIR", str(Path(__file__).parent.parent / "data" / "lakehouse")))
DUCKDB_PATH = os.getenv("DUCKDB_PATH", str(LAKEHOUSE_DIR / "inec_lakehouse.duckdb"))


class LakehousePipeline:
    """Medallion architecture data pipeline for INEC election data."""

    def __init__(self, base_dir: Path | None = None, db_path: str | None = None):
        self.base_dir = base_dir or LAKEHOUSE_DIR
        self.bronze_dir = self.base_dir / "bronze"
        self.silver_dir = self.base_dir / "silver"
        self.gold_dir = self.base_dir / "gold"

        for d in [self.bronze_dir, self.silver_dir, self.gold_dir]:
            d.mkdir(parents=True, exist_ok=True)

        self.conn = duckdb.connect(db_path or DUCKDB_PATH)
        self._init_catalog()
        log.info("lakehouse_initialized", base_dir=str(self.base_dir))

    def _init_catalog(self):
        """Create catalog tables for tracking data lineage."""
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS data_catalog (
                id VARCHAR PRIMARY KEY,
                tier VARCHAR NOT NULL,
                table_name VARCHAR NOT NULL,
                file_path VARCHAR,
                row_count BIGINT,
                schema_hash VARCHAR,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                metadata JSON
            )
        """)
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS pipeline_runs (
                id VARCHAR PRIMARY KEY,
                tier VARCHAR NOT NULL,
                status VARCHAR DEFAULT 'running',
                rows_in BIGINT DEFAULT 0,
                rows_out BIGINT DEFAULT 0,
                started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                completed_at TIMESTAMP,
                error TEXT
            )
        """)

    # ── Bronze Layer (Raw Ingestion) ──

    def ingest_bronze_results(self, results: list[dict], source: str = "postgres") -> str:
        """Ingest raw election results into Bronze layer."""
        run_id = f"bronze-{datetime.now(timezone.utc).strftime('%Y%m%d%H%M%S')}"
        self.conn.execute(
            "INSERT INTO pipeline_runs (id, tier) VALUES (?, ?)", [run_id, "bronze"]
        )

        if not results:
            self._complete_run(run_id, 0, 0)
            return run_id

        df = pd.DataFrame(results)
        df["_ingested_at"] = datetime.now(timezone.utc).isoformat()
        df["_source"] = source
        df["_batch_id"] = run_id

        file_path = self.bronze_dir / f"results_{run_id}.parquet"
        df.to_parquet(file_path, engine="pyarrow", index=False)

        # Register in DuckDB
        self.conn.execute(f"""
            CREATE OR REPLACE TABLE bronze_results AS
            SELECT * FROM read_parquet('{self.bronze_dir}/*.parquet')
        """)

        self._catalog_entry(run_id, "bronze", "bronze_results", str(file_path), len(df))
        self._complete_run(run_id, len(results), len(df))
        log.info("bronze_ingested", run_id=run_id, rows=len(df), source=source)
        return run_id

    def ingest_bronze_polling_units(self, polling_units: list[dict]) -> str:
        """Ingest polling unit metadata into Bronze layer."""
        run_id = f"bronze-pu-{datetime.now(timezone.utc).strftime('%Y%m%d%H%M%S')}"

        df = pd.DataFrame(polling_units)
        df["_ingested_at"] = datetime.now(timezone.utc).isoformat()

        file_path = self.bronze_dir / f"polling_units_{run_id}.parquet"
        df.to_parquet(file_path, engine="pyarrow", index=False)

        self.conn.execute(f"""
            CREATE OR REPLACE TABLE bronze_polling_units AS
            SELECT * FROM read_parquet('{self.bronze_dir}/polling_units_*.parquet')
        """)

        log.info("bronze_pu_ingested", rows=len(df))
        return run_id

    # ── Silver Layer (Cleaned + Enriched) ──

    def process_silver(self) -> str:
        """Transform Bronze → Silver: clean, deduplicate, enrich with features."""
        run_id = f"silver-{datetime.now(timezone.utc).strftime('%Y%m%d%H%M%S')}"
        self.conn.execute(
            "INSERT INTO pipeline_runs (id, tier) VALUES (?, ?)", [run_id, "silver"]
        )

        try:
            # Check if bronze data exists
            count = self.conn.execute(
                "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='bronze_results'"
            ).fetchone()[0]
            if count == 0:
                self._complete_run(run_id, 0, 0, "No bronze data available")
                return run_id

            bronze_count = self.conn.execute("SELECT COUNT(*) FROM bronze_results").fetchone()[0]

            # Deduplicate + compute features
            self.conn.execute("""
                CREATE OR REPLACE TABLE silver_results AS
                WITH deduped AS (
                    SELECT DISTINCT ON (polling_unit_code, party_code, election_id)
                        *
                    FROM bronze_results
                    ORDER BY polling_unit_code, party_code, election_id, _ingested_at DESC
                )
                SELECT
                    *,
                    -- Computed features
                    CASE WHEN registered_voters > 0
                         THEN CAST(accredited_voters AS DOUBLE) / registered_voters
                         ELSE 0 END AS turnout_rate,
                    CASE WHEN total_valid_votes > 0
                         THEN CAST(rejected_votes AS DOUBLE) / total_valid_votes
                         ELSE 0 END AS rejection_rate,
                    CASE WHEN accredited_voters > 0 AND total_valid_votes > accredited_voters
                         THEN 1 ELSE 0 END AS overvoting_flag,
                    CASE WHEN total_valid_votes > 0 AND (total_valid_votes % 100 = 0 OR total_valid_votes % 50 = 0)
                         THEN 1 ELSE 0 END AS round_number_flag,
                    CURRENT_TIMESTAMP AS _processed_at
                FROM deduped
            """)

            silver_count = self.conn.execute("SELECT COUNT(*) FROM silver_results").fetchone()[0]

            # Export to Parquet
            file_path = self.silver_dir / f"results_{run_id}.parquet"
            self.conn.execute(f"COPY silver_results TO '{file_path}' (FORMAT PARQUET)")

            self._catalog_entry(run_id, "silver", "silver_results", str(file_path), silver_count)
            self._complete_run(run_id, bronze_count, silver_count)
            log.info("silver_processed", run_id=run_id, bronze=bronze_count, silver=silver_count)

        except Exception as e:
            self._complete_run(run_id, 0, 0, str(e))
            log.error("silver_failed", error=str(e))
            raise

        return run_id

    # ── Gold Layer (ML-Ready Features) ──

    def process_gold(self) -> str:
        """Transform Silver → Gold: aggregate, compute ML features, score."""
        run_id = f"gold-{datetime.now(timezone.utc).strftime('%Y%m%d%H%M%S')}"
        self.conn.execute(
            "INSERT INTO pipeline_runs (id, tier) VALUES (?, ?)", [run_id, "gold"]
        )

        try:
            count = self.conn.execute(
                "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='silver_results'"
            ).fetchone()[0]
            if count == 0:
                self._complete_run(run_id, 0, 0, "No silver data available")
                return run_id

            silver_count = self.conn.execute("SELECT COUNT(*) FROM silver_results").fetchone()[0]

            # State-level aggregations
            self.conn.execute("""
                CREATE OR REPLACE TABLE gold_state_summary AS
                SELECT
                    state_code,
                    COUNT(DISTINCT polling_unit_code) AS total_pus,
                    SUM(votes) AS total_votes,
                    AVG(turnout_rate) AS avg_turnout,
                    STDDEV(turnout_rate) AS turnout_stddev,
                    SUM(overvoting_flag) AS overvoting_count,
                    SUM(round_number_flag) AS round_number_count,
                    AVG(rejection_rate) AS avg_rejection_rate,
                    CURRENT_TIMESTAMP AS _computed_at
                FROM silver_results
                GROUP BY state_code
            """)

            # ML feature matrix (per polling unit)
            self.conn.execute("""
                CREATE OR REPLACE TABLE gold_ml_features AS
                SELECT
                    polling_unit_code,
                    election_id,
                    MAX(registered_voters) AS registered_voters,
                    MAX(accredited_voters) AS accredited_voters,
                    MAX(turnout_rate) AS turnout_rate,
                    SUM(votes) AS total_valid_votes,
                    MAX(rejected_votes) AS rejected_votes,
                    MAX(CASE WHEN party_code = 'APC' THEN votes ELSE 0 END) AS party_a_votes,
                    MAX(CASE WHEN party_code = 'PDP' THEN votes ELSE 0 END) AS party_b_votes,
                    MAX(overvoting_flag) AS overvoting_flag,
                    MAX(round_number_flag) AS round_number_flag,
                    MAX(rejection_rate) AS rejection_rate,
                    state_code,
                    lga_code,
                    CURRENT_TIMESTAMP AS _computed_at
                FROM silver_results
                GROUP BY polling_unit_code, election_id, state_code, lga_code
            """)

            # Anomaly scores (using SQL-based Benford analysis)
            self.conn.execute("""
                CREATE OR REPLACE TABLE gold_benford_analysis AS
                WITH first_digits AS (
                    SELECT
                        state_code,
                        CAST(SUBSTRING(CAST(votes AS VARCHAR), 1, 1) AS INTEGER) AS first_digit,
                        COUNT(*) AS cnt
                    FROM silver_results
                    WHERE votes > 0
                    GROUP BY state_code, first_digit
                ),
                state_totals AS (
                    SELECT state_code, SUM(cnt) AS total
                    FROM first_digits
                    GROUP BY state_code
                )
                SELECT
                    fd.state_code,
                    fd.first_digit,
                    CAST(fd.cnt AS DOUBLE) / st.total * 100 AS observed_pct,
                    CASE fd.first_digit
                        WHEN 1 THEN 30.1 WHEN 2 THEN 17.6 WHEN 3 THEN 12.5
                        WHEN 4 THEN 9.7 WHEN 5 THEN 7.9 WHEN 6 THEN 6.7
                        WHEN 7 THEN 5.8 WHEN 8 THEN 5.1 WHEN 9 THEN 4.6
                    END AS expected_pct,
                    ABS(CAST(fd.cnt AS DOUBLE) / st.total * 100 -
                        CASE fd.first_digit
                            WHEN 1 THEN 30.1 WHEN 2 THEN 17.6 WHEN 3 THEN 12.5
                            WHEN 4 THEN 9.7 WHEN 5 THEN 7.9 WHEN 6 THEN 6.7
                            WHEN 7 THEN 5.8 WHEN 8 THEN 5.1 WHEN 9 THEN 4.6
                        END) AS deviation
                FROM first_digits fd
                JOIN state_totals st ON fd.state_code = st.state_code
                WHERE fd.first_digit BETWEEN 1 AND 9
                ORDER BY fd.state_code, fd.first_digit
            """)

            gold_count = self.conn.execute("SELECT COUNT(*) FROM gold_ml_features").fetchone()[0]

            # Export to Parquet
            for table in ["gold_state_summary", "gold_ml_features", "gold_benford_analysis"]:
                fp = self.gold_dir / f"{table}_{run_id}.parquet"
                self.conn.execute(f"COPY {table} TO '{fp}' (FORMAT PARQUET)")

            self._catalog_entry(run_id, "gold", "gold_ml_features", str(self.gold_dir), gold_count)
            self._complete_run(run_id, silver_count, gold_count)
            log.info("gold_processed", run_id=run_id, silver=silver_count, gold=gold_count)

        except Exception as e:
            self._complete_run(run_id, 0, 0, str(e))
            log.error("gold_failed", error=str(e))
            raise

        return run_id

    # ── Full Pipeline ──

    def run_full_pipeline(self, results: list[dict], source: str = "postgres") -> dict:
        """Run Bronze → Silver → Gold pipeline end-to-end."""
        start = datetime.now(timezone.utc)

        bronze_id = self.ingest_bronze_results(results, source)
        silver_id = self.process_silver()
        gold_id = self.process_gold()

        elapsed = (datetime.now(timezone.utc) - start).total_seconds()

        return {
            "bronze_run": bronze_id,
            "silver_run": silver_id,
            "gold_run": gold_id,
            "elapsed_seconds": round(elapsed, 2),
            "pipeline_runs": self.get_pipeline_status(),
        }

    def get_ml_features(self) -> Optional[pd.DataFrame]:
        """Get Gold-tier ML feature matrix as a DataFrame."""
        try:
            return self.conn.execute("SELECT * FROM gold_ml_features").df()
        except Exception:
            return None

    def get_pipeline_status(self) -> list[dict]:
        """Get status of recent pipeline runs."""
        rows = self.conn.execute("""
            SELECT id, tier, status, rows_in, rows_out, started_at, completed_at, error
            FROM pipeline_runs
            ORDER BY started_at DESC
            LIMIT 20
        """).fetchall()
        return [
            {
                "id": r[0], "tier": r[1], "status": r[2],
                "rows_in": r[3], "rows_out": r[4],
                "started_at": str(r[5]), "completed_at": str(r[6]),
                "error": r[7],
            }
            for r in rows
        ]

    def get_tier_stats(self) -> dict:
        """Get row counts across all tiers."""
        stats = {}
        for tier, table in [("bronze", "bronze_results"), ("silver", "silver_results"), ("gold", "gold_ml_features")]:
            try:
                count = self.conn.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
                stats[tier] = count
            except Exception:
                stats[tier] = 0
        return stats

    # ── Helpers ──

    def _catalog_entry(self, run_id, tier, table_name, file_path, row_count):
        self.conn.execute(
            "INSERT OR REPLACE INTO data_catalog (id, tier, table_name, file_path, row_count) VALUES (?,?,?,?,?)",
            [run_id, tier, table_name, file_path, row_count],
        )

    def _complete_run(self, run_id, rows_in, rows_out, error=None):
        status = "completed" if error is None else "failed"
        self.conn.execute(
            "UPDATE pipeline_runs SET status=?, rows_in=?, rows_out=?, completed_at=CURRENT_TIMESTAMP, error=? WHERE id=?",
            [status, rows_in, rows_out, error, run_id],
        )

    def close(self):
        self.conn.close()
