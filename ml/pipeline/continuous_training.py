"""INEC Continuous Training Pipeline.

Event-driven model retraining from platform data:
1. Monitors PostgreSQL for new election results
2. Ingests new data through Lakehouse pipeline
3. Evaluates current model performance on new data
4. Triggers retraining when performance degrades
5. Validates new model before promoting to production

Supports:
- Scheduled retraining (daily/weekly)
- Drift detection (performance monitoring)
- A/B model comparison
- Automatic rollback on regression

Integration:
- Kafka: Consumes election result events for real-time ingestion
- PostgreSQL: Source of truth for election data
- Lakehouse: Bronze→Silver→Gold data pipeline
- Ray: Distributed retraining when triggered
"""

import json
import os
import time
import hashlib
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import numpy as np
import pandas as pd
import structlog

log = structlog.get_logger()

MODELS_DIR = Path(__file__).parent.parent / "models"
DATA_DIR = Path(__file__).parent.parent / "data"


class ModelRegistry:
    """Track model versions, metrics, and promotion status."""

    def __init__(self, registry_dir: Path | None = None):
        self.registry_dir = registry_dir or MODELS_DIR / "registry"
        self.registry_dir.mkdir(parents=True, exist_ok=True)
        self.registry_file = self.registry_dir / "model_registry.json"
        self._registry = self._load()

    def _load(self) -> dict:
        if self.registry_file.exists():
            with open(self.registry_file) as f:
                return json.load(f)
        return {"models": {}, "production": {}}

    def _save(self):
        with open(self.registry_file, "w") as f:
            json.dump(self._registry, f, indent=2, default=str)

    def register(self, model_name: str, version: str, metrics: dict,
                 weights_path: str, metadata: dict | None = None) -> str:
        """Register a new model version."""
        model_id = f"{model_name}-{version}"
        self._registry["models"][model_id] = {
            "name": model_name,
            "version": version,
            "metrics": metrics,
            "weights_path": weights_path,
            "metadata": metadata or {},
            "registered_at": datetime.now(timezone.utc).isoformat(),
            "status": "staged",
        }
        self._save()
        log.info("model_registered", model_id=model_id, metrics=metrics)
        return model_id

    def promote_to_production(self, model_id: str) -> bool:
        """Promote a staged model to production."""
        if model_id not in self._registry["models"]:
            return False

        model = self._registry["models"][model_id]
        model_name = model["name"]

        # Archive current production model
        if model_name in self._registry["production"]:
            old_id = self._registry["production"][model_name]
            if old_id in self._registry["models"]:
                self._registry["models"][old_id]["status"] = "archived"

        model["status"] = "production"
        self._registry["production"][model_name] = model_id
        self._save()
        log.info("model_promoted", model_id=model_id, model_name=model_name)
        return True

    def get_production_model(self, model_name: str) -> Optional[dict]:
        """Get the current production model info."""
        model_id = self._registry["production"].get(model_name)
        if model_id:
            return self._registry["models"].get(model_id)
        return None

    def list_versions(self, model_name: str) -> list[dict]:
        """List all versions of a model."""
        return [
            {**v, "id": k}
            for k, v in self._registry["models"].items()
            if v["name"] == model_name
        ]


class DriftDetector:
    """Detect model performance drift using statistical tests."""

    def __init__(self, window_size: int = 100, threshold: float = 0.05):
        self.window_size = window_size
        self.threshold = threshold
        self.prediction_history: list[dict] = []

    def record_prediction(self, prediction: float, actual: Optional[float] = None):
        """Record a prediction for drift monitoring."""
        self.prediction_history.append({
            "prediction": prediction,
            "actual": actual,
            "timestamp": datetime.now(timezone.utc).isoformat(),
        })
        # Keep only recent window
        if len(self.prediction_history) > self.window_size * 2:
            self.prediction_history = self.prediction_history[-self.window_size * 2:]

    def check_drift(self) -> dict:
        """Check for prediction distribution drift."""
        if len(self.prediction_history) < self.window_size:
            return {"drift_detected": False, "reason": "insufficient_data"}

        recent = [p["prediction"] for p in self.prediction_history[-self.window_size:]]
        older = [p["prediction"] for p in self.prediction_history[-self.window_size * 2:-self.window_size]]

        if len(older) < self.window_size // 2:
            return {"drift_detected": False, "reason": "insufficient_baseline"}

        # PSI (Population Stability Index)
        psi = self._compute_psi(older, recent)

        # KS test
        from scipy import stats
        ks_stat, ks_p = stats.ks_2samp(older, recent)

        drift_detected = psi > 0.2 or ks_p < self.threshold

        result = {
            "drift_detected": drift_detected,
            "psi": round(psi, 4),
            "ks_statistic": round(ks_stat, 4),
            "ks_p_value": round(ks_p, 4),
            "recommendation": "retrain" if drift_detected else "no_action",
            "window_size": self.window_size,
            "n_predictions": len(self.prediction_history),
        }

        if drift_detected:
            log.warning("drift_detected", **result)

        return result

    @staticmethod
    def _compute_psi(expected: list[float], actual: list[float], bins: int = 10) -> float:
        """Population Stability Index."""
        eps = 1e-10
        e_hist, bin_edges = np.histogram(expected, bins=bins)
        a_hist, _ = np.histogram(actual, bins=bin_edges)

        e_pct = e_hist / max(sum(e_hist), 1) + eps
        a_pct = a_hist / max(sum(a_hist), 1) + eps

        psi = np.sum((a_pct - e_pct) * np.log(a_pct / e_pct))
        return float(psi)


class ContinuousTrainingPipeline:
    """Orchestrates continuous model retraining from platform data."""

    def __init__(self, db_url: str | None = None):
        self.db_url = db_url or os.getenv(
            "DATABASE_URL", "postgresql://ngapp:ngapp123@localhost:5432/ngapp"
        )
        self.registry = ModelRegistry()
        self.drift_detector = DriftDetector()
        self._last_retrain: Optional[datetime] = None

    def ingest_from_postgres(self) -> pd.DataFrame:
        """Pull latest election results from PostgreSQL into training data."""
        try:
            import psycopg2
            conn = psycopg2.connect(self.db_url)
            query = """
                SELECT r.polling_unit_code, r.election_id,
                       pu.registered_voters, r.accredited_voters,
                       r.total_valid_votes, r.rejected_votes,
                       r.status, pu.state_code, pu.lga_code
                FROM results r
                JOIN polling_units pu ON r.polling_unit_code = pu.code
                ORDER BY r.created_at DESC
            """
            df = pd.read_sql(query, conn)
            conn.close()
            log.info("postgres_ingest", rows=len(df))
            return df
        except Exception as e:
            log.warning("postgres_ingest_failed", error=str(e))
            return pd.DataFrame()

    def evaluate_current_model(self, test_data: pd.DataFrame) -> dict:
        """Evaluate current production model on new data."""
        import joblib
        import xgboost as xgb

        model_path = MODELS_DIR / "anomaly_xgboost.json"
        scaler_path = MODELS_DIR / "anomaly_scaler.pkl"

        if not model_path.exists():
            return {"error": "No production model found"}

        model = xgb.XGBClassifier()
        model.load_model(str(model_path))
        scaler = joblib.load(str(scaler_path)) if scaler_path.exists() else None

        # Compute features from test data
        features = self._compute_features(test_data)
        if features is None or len(features) == 0:
            return {"error": "No features computed"}

        if scaler:
            features_scaled = scaler.transform(features)
        else:
            features_scaled = features

        predictions = model.predict_proba(features_scaled)[:, 1]

        # Record for drift detection
        for pred in predictions:
            self.drift_detector.record_prediction(float(pred))

        return {
            "n_samples": len(predictions),
            "mean_score": float(np.mean(predictions)),
            "anomaly_rate": float((predictions > 0.5).mean()),
            "score_distribution": {
                "p10": float(np.percentile(predictions, 10)),
                "p50": float(np.percentile(predictions, 50)),
                "p90": float(np.percentile(predictions, 90)),
            },
        }

    def should_retrain(self) -> dict:
        """Determine if retraining is needed."""
        reasons = []

        # Check drift
        drift = self.drift_detector.check_drift()
        if drift.get("drift_detected"):
            reasons.append(f"Distribution drift detected (PSI={drift['psi']:.4f})")

        # Check time since last training
        meta_path = MODELS_DIR / "anomaly_model_metadata.json"
        if meta_path.exists():
            with open(meta_path) as f:
                meta = json.load(f)
            trained_at = meta.get("trained_at", "")
            if trained_at:
                from dateutil.parser import parse
                age_hours = (datetime.now(timezone.utc) - parse(trained_at)).total_seconds() / 3600
                if age_hours > 168:  # > 1 week
                    reasons.append(f"Model age: {age_hours:.0f}h (>168h threshold)")

        # Check new data volume
        try:
            new_data = self.ingest_from_postgres()
            if len(new_data) > 1000:
                reasons.append(f"New data available: {len(new_data)} records")
        except Exception:
            pass

        return {
            "should_retrain": len(reasons) > 0,
            "reasons": reasons,
            "drift_status": drift,
        }

    # Minimum real training samples required before a retrained model may be
    # auto-promoted to production.
    MIN_REAL_SAMPLES_FOR_PROMOTION = 1000

    def retrain(self, use_ray: bool = False) -> dict:
        """Trigger model retraining.

        INTEGRITY: retraining uses ONLY real data ingested from PostgreSQL.
        If insufficient real data is available, the retrain is skipped with a
        warning — no model is trained, registered, or promoted. Auto-promotion
        additionally requires the artifact metadata to prove trained_on == "real"
        and a minimum real-sample count.
        """
        log.info("retraining_started", use_ray=use_ray)

        if use_ray:
            from ml.pipeline.ray_engine import RayEngine
            engine = RayEngine()
            report = engine.train_distributed(["anomaly", "gnn"])
            engine.shutdown()
        else:
            from ml.training.anomaly_detection.train import (
                train_model, FEATURE_COLUMNS,
            )

            # Real data only — never fall back to the synthetic generator.
            data = self.ingest_from_postgres()
            required_cols = set(FEATURE_COLUMNS) | {"label"}
            if len(data) < self.MIN_REAL_SAMPLES_FOR_PROMOTION:
                log.warning(
                    "retrain_skipped_insufficient_real_data",
                    rows=len(data),
                    min_required=self.MIN_REAL_SAMPLES_FOR_PROMOTION,
                )
                return {"status": "skipped", "reason": "insufficient_real_data",
                        "rows": len(data), "auto_promoted": False}
            if not required_cols.issubset(data.columns):
                missing = sorted(required_cols - set(data.columns))
                log.warning("retrain_skipped_missing_columns", missing=missing)
                return {"status": "skipped",
                        "reason": f"ingested data missing required columns: {missing}",
                        "auto_promoted": False}

            data_path = DATA_DIR / "processed" / "retrain_real_data.parquet"
            data_path.parent.mkdir(parents=True, exist_ok=True)
            data.to_parquet(data_path)
            _, _, metadata = train_model(data_path=str(data_path))
            report = {"anomaly": metadata}

        # Register new model version
        version = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")
        model_id = self.registry.register(
            "anomaly_xgboost",
            version,
            report.get("anomaly", {}).get("metrics", {}),
            str(MODELS_DIR / "anomaly_xgboost.json"),
        )

        # Auto-promote ONLY if metrics are good AND the model was trained on
        # enough REAL data (never synthetic). roc_auc > 0.95 on synthetic data
        # is trivially achievable and is not evidence of production quality.
        anomaly_report = report.get("anomaly", {})
        metrics = anomaly_report.get("metrics", {})
        trained_on_real = anomaly_report.get("trained_on") == "real"
        enough_real_samples = (
            anomaly_report.get("n_samples", 0) >= self.MIN_REAL_SAMPLES_FOR_PROMOTION
        )
        auto_promoted = bool(
            metrics.get("roc_auc", 0) > 0.95 and trained_on_real and enough_real_samples
        )
        if auto_promoted:
            self.registry.promote_to_production(model_id)
            log.info("model_auto_promoted", model_id=model_id, roc_auc=metrics.get("roc_auc"))
        else:
            log.warning(
                "model_auto_promotion_blocked",
                model_id=model_id,
                roc_auc=metrics.get("roc_auc"),
                trained_on=anomaly_report.get("trained_on"),
                n_samples=anomaly_report.get("n_samples"),
            )

        self._last_retrain = datetime.now(timezone.utc)

        return {
            "model_id": model_id,
            "report": report,
            "auto_promoted": auto_promoted,
        }

    def run_continuous_cycle(self) -> dict:
        """Run one cycle of the continuous training pipeline."""
        cycle_start = time.time()

        # 1. Check if retraining is needed
        check = self.should_retrain()

        if not check["should_retrain"]:
            return {
                "action": "no_retrain",
                "reasons": check["reasons"],
                "drift_status": check["drift_status"],
            }

        # 2. Retrain
        retrain_result = self.retrain(use_ray=True)

        elapsed = time.time() - cycle_start
        return {
            "action": "retrained",
            "reasons": check["reasons"],
            "retrain_result": retrain_result,
            "elapsed_seconds": round(elapsed, 1),
        }

    def _compute_features(self, df: pd.DataFrame) -> Optional[np.ndarray]:
        """Compute feature matrix from raw election data.

        INTEGRITY: every feature must be derived from the actual result rows.
        Rows missing data needed for a feature are excluded — feature values
        are never fabricated as constants. Feature order matches FEATURE_COLUMNS
        in ml/training/anomaly_detection/train.py:
            [registered_voters, accredited_voters, turnout_rate,
             total_valid_votes, rejected_votes, party_a_votes, party_b_votes,
             party_a_share, party_b_share, vote_margin,
             benford_deviation, submission_delay_hours,
             regional_mean_turnout, turnout_vs_region,
             rejected_rate, overvoting_flag, round_number_flag]
        """
        if df.empty:
            return None

        # Regional mean turnout per state (from the rows themselves).
        work = df.copy()
        work["_turnout"] = work["accredited_voters"] / work["registered_voters"].clip(lower=1)
        if "state_code" in work.columns:
            work["_regional_mean"] = work.groupby("state_code")["_turnout"].transform("mean")
        else:
            work["_regional_mean"] = work["_turnout"].mean()

        # Benford deviation per election (chi-squared over first digits of the
        # per-PU vote totals within each election_id group).
        benford_by_election: dict = {}
        if "election_id" in work.columns:
            for election_id, group in work.groupby("election_id"):
                benford_by_election[election_id] = self._benford_deviation(
                    group["total_valid_votes"].dropna().tolist()
                )

        # Per-party vote columns: accept either long form (party_code/votes
        # rows) or wide form (party_a_votes / party_b_votes columns).
        has_wide_parties = {"party_a_votes", "party_b_votes"}.issubset(work.columns)

        # Submission delay from timestamps, if present.
        has_timestamps = {"submitted_at", "poll_closed_at"}.issubset(work.columns)

        features = []
        for _, row in work.iterrows():
            try:
                reg = float(row["registered_voters"])
                acc = float(row["accredited_voters"])
                valid = float(row["total_valid_votes"])
                rej = float(row["rejected_votes"])
            except (KeyError, TypeError, ValueError):
                continue  # exclude rows lacking the core counts
            if reg <= 0 or acc <= 0:
                continue
            turnout = acc / reg

            # Party votes / shares / margin — only from real per-party data.
            if has_wide_parties:
                pa = float(row["party_a_votes"])
                pb = float(row["party_b_votes"])
            else:
                continue  # no real per-party votes available: exclude the row
            pa_share = pa / max(valid, 1)
            pb_share = pb / max(valid, 1)
            margin = abs(pa - pb) / max(valid, 1)

            # Benford deviation from the row's election group; skip if unknown.
            if "election_id" in row.index:
                benford = benford_by_election.get(row["election_id"])
            else:
                benford = None
            if benford is None:
                continue

            # Submission delay from real timestamps; skip if unavailable.
            if has_timestamps:
                try:
                    delay = (
                        pd.to_datetime(row["submitted_at"])
                        - pd.to_datetime(row["poll_closed_at"])
                    ).total_seconds() / 3600.0
                except (TypeError, ValueError):
                    continue
                if not np.isfinite(delay) or delay < 0:
                    continue
            else:
                continue

            regional_mean = float(row["_regional_mean"])

            features.append([
                reg, acc, turnout, valid, rej,
                pa, pb,
                pa_share, pb_share,
                margin,
                benford,
                delay,
                regional_mean,
                turnout - regional_mean,
                rej / acc,
                int(valid > acc),
                int(valid > 0 and (int(valid) % 100 == 0 or int(valid) % 50 == 0)),
            ])

        if not features:
            log.warning(
                "feature_computation_yielded_no_rows",
                detail="no result rows had all required real fields; "
                       "refusing to fabricate feature constants",
            )
            return None

        return np.array(features, dtype=np.float32)

    @staticmethod
    def _benford_deviation(votes: list) -> Optional[float]:
        """Chi-squared deviation of first-digit distribution from Benford's Law."""
        expected = np.array([30.1, 17.6, 12.5, 9.7, 7.9, 6.7, 5.8, 5.1, 4.6])
        first_digits = [int(str(abs(int(v)))[0]) for v in votes if v and v > 0]
        if len(first_digits) < 30:
            return None
        observed = np.zeros(9)
        for d in first_digits:
            if 1 <= d <= 9:
                observed[d - 1] += 1
        observed = observed / observed.sum() * 100
        return float(np.sum((observed - expected) ** 2 / expected))

    def get_status(self) -> dict:
        """Get continuous training pipeline status."""
        prod_model = self.registry.get_production_model("anomaly_xgboost")
        drift = self.drift_detector.check_drift()

        return {
            "production_model": prod_model,
            "drift_status": drift,
            "last_retrain": self._last_retrain.isoformat() if self._last_retrain else None,
            "prediction_count": len(self.drift_detector.prediction_history),
            "registry": {
                "total_models": len(self.registry._registry["models"]),
                "production_models": len(self.registry._registry["production"]),
            },
        }
