"""INEC Ray Distributed Compute Engine.

Provides:
1. Distributed model training (parallelize across CPUs)
2. Ray Serve for model inference serving
3. Batch inference for large-scale election data
4. Distributed feature engineering

Usage:
    from ml.pipeline.ray_engine import RayEngine

    engine = RayEngine()
    engine.train_distributed()        # Train all models in parallel
    engine.batch_predict(data)        # Distributed batch inference
    engine.start_inference_service()  # Start Ray Serve deployment
"""

import os
import json
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import numpy as np
import structlog

log = structlog.get_logger()

MODELS_DIR = Path(__file__).parent.parent / "models"


class RayEngine:
    """Ray-powered distributed training and inference for INEC ML models."""

    def __init__(self, num_cpus: int | None = None):
        self.num_cpus = num_cpus or os.cpu_count()
        self._ray_initialized = False

    def _ensure_ray(self):
        """Lazy-init Ray cluster."""
        if self._ray_initialized:
            return
        import ray
        if not ray.is_initialized():
            ray.init(
                num_cpus=self.num_cpus,
                ignore_reinit_error=True,
                logging_level="warning",
            )
        self._ray_initialized = True
        log.info("ray_initialized", cpus=self.num_cpus, resources=ray.cluster_resources())

    def shutdown(self):
        """Shutdown Ray cluster."""
        if self._ray_initialized:
            import ray
            ray.shutdown()
            self._ray_initialized = False

    # ── Distributed Training ──

    def train_distributed(self, models: list[str] | None = None) -> dict:
        """Train models in parallel using Ray remote tasks.

        Args:
            models: List of model names to train. Default: all models.
                    Options: "anomaly", "gnn", "liveness"

        Returns:
            Training report with metrics for each model.
        """
        import ray
        self._ensure_ray()

        if models is None:
            models = ["anomaly", "gnn", "liveness"]

        start = time.time()
        futures = {}

        for model_name in models:
            if model_name == "anomaly":
                futures["anomaly"] = _ray_train_anomaly.remote(str(MODELS_DIR))
            elif model_name == "gnn":
                futures["gnn"] = _ray_train_gnn.remote(str(MODELS_DIR), 200)
            elif model_name == "liveness":
                futures["liveness"] = _ray_train_liveness.remote(str(MODELS_DIR), 20)

        log.info("ray_training_started", models=list(futures.keys()))

        # Collect results
        results = {}
        for name, future in futures.items():
            try:
                results[name] = ray.get(future)
                log.info("ray_model_completed", model=name)
            except Exception as e:
                results[name] = {"error": str(e)}
                log.error("ray_model_failed", model=name, error=str(e))

        elapsed = time.time() - start

        report = {
            "mode": "ray_distributed",
            "total_time_seconds": round(elapsed, 1),
            "models_trained": len([r for r in results.values() if "error" not in r]),
            "models_failed": len([r for r in results.values() if "error" in r]),
            "results": results,
            "ray_resources": ray.cluster_resources(),
            "trained_at": datetime.now(timezone.utc).isoformat(),
        }

        report_path = MODELS_DIR / "ray_training_report.json"
        with open(report_path, "w") as f:
            json.dump(report, f, indent=2, default=str)

        log.info("ray_training_complete", elapsed=elapsed, report=str(report_path))
        return report

    # ── Distributed Batch Inference ──

    def batch_predict_anomalies(self, polling_unit_data: list[dict], batch_size: int = 1000) -> list[dict]:
        """Distributed batch anomaly scoring using Ray.

        Splits data into batches and processes in parallel.

        Args:
            polling_unit_data: List of PU feature dicts
            batch_size: Size of each parallel batch

        Returns:
            List of dicts with anomaly_score and is_anomaly for each PU.
        """
        import ray
        self._ensure_ray()

        # Split into batches
        batches = [
            polling_unit_data[i:i + batch_size]
            for i in range(0, len(polling_unit_data), batch_size)
        ]

        log.info("batch_predict_started", total=len(polling_unit_data), batches=len(batches))

        # Process batches in parallel
        futures = [_ray_predict_batch.remote(batch, str(MODELS_DIR)) for batch in batches]
        results = ray.get(futures)

        # Flatten results
        all_predictions = []
        for batch_result in results:
            all_predictions.extend(batch_result)

        log.info("batch_predict_complete", total_predictions=len(all_predictions))
        return all_predictions

    def batch_score_graph(self, node_features: np.ndarray, edge_index: np.ndarray) -> np.ndarray:
        """Score nodes using GNN model via Ray."""
        import ray
        self._ensure_ray()

        future = _ray_gnn_score.remote(node_features, edge_index, str(MODELS_DIR))
        return ray.get(future)

    # ── Feature Engineering ──

    def distributed_feature_engineering(self, raw_results: list[dict]) -> list[dict]:
        """Compute ML features in parallel using Ray."""
        import ray
        self._ensure_ray()

        batch_size = max(100, len(raw_results) // self.num_cpus)
        batches = [
            raw_results[i:i + batch_size]
            for i in range(0, len(raw_results), batch_size)
        ]

        futures = [_ray_compute_features.remote(batch) for batch in batches]
        results = ray.get(futures)

        return [feat for batch in results for feat in batch]


# ── Ray Remote Functions ──

try:
    import ray

    @ray.remote
    def _ray_train_anomaly(output_dir: str) -> dict:
        import sys
        sys.path.insert(0, str(Path(output_dir).parent.parent))
        from ml.training.anomaly_detection.train import train_model
        _, _, metadata = train_model(output_dir=output_dir)
        return {"model": "anomaly_xgboost", "metrics": metadata.get("metrics", {})}

    @ray.remote
    def _ray_train_gnn(output_dir: str, epochs: int) -> dict:
        import sys
        sys.path.insert(0, str(Path(output_dir).parent.parent))
        from ml.training.gnn_network.train import train_gnn
        train_gnn(output_dir=output_dir, epochs=epochs)
        meta_path = Path(output_dir) / "gnn_model_metadata.json"
        if meta_path.exists():
            with open(meta_path) as f:
                return json.load(f)
        return {"model": "gnn_election"}

    @ray.remote
    def _ray_train_liveness(output_dir: str, epochs: int) -> dict:
        import sys
        sys.path.insert(0, str(Path(output_dir).parent.parent))
        from ml.training.liveness_pad.train import train_pad_model
        train_pad_model(output_dir=output_dir, epochs=epochs)
        meta_path = Path(output_dir) / "liveness_model_metadata.json"
        if meta_path.exists():
            with open(meta_path) as f:
                return json.load(f)
        return {"model": "liveness_cdcn"}

    @ray.remote
    def _ray_predict_batch(batch: list[dict], models_dir: str) -> list[dict]:
        """Score a batch of polling units for anomalies."""
        import joblib
        import xgboost as xgb

        model_path = Path(models_dir) / "anomaly_xgboost.json"
        scaler_path = Path(models_dir) / "anomaly_scaler.pkl"

        if not model_path.exists():
            return [{"error": "Model not found"} for _ in batch]

        model = xgb.XGBClassifier()
        model.load_model(str(model_path))
        scaler = joblib.load(str(scaler_path)) if scaler_path.exists() else None

        predictions = []
        for pu in batch:
            reg = pu.get("registered_voters", 1000)
            acc = pu.get("accredited_voters", 500)
            valid = pu.get("total_valid_votes", 450)
            rej = pu.get("rejected_votes", 50)
            pa = pu.get("party_a_votes", 200)
            pb = pu.get("party_b_votes", 150)
            turnout = acc / max(reg, 1)

            features = np.array([[
                reg, acc, turnout, valid, rej, pa, pb,
                pa / max(valid, 1), pb / max(valid, 1),
                abs(pa - pb) / max(valid, 1),
                pu.get("benford_deviation", 0.02),
                pu.get("submission_delay_hours", 3.0),
                pu.get("regional_mean_turnout", 0.55),
                turnout - pu.get("regional_mean_turnout", 0.55),
                rej / max(acc, 1),
                int(valid > acc),
                int(valid % 100 == 0 or valid % 50 == 0),
            ]], dtype=np.float32)

            if scaler:
                features = scaler.transform(features)

            prob = float(model.predict_proba(features)[0][1])
            predictions.append({
                "polling_unit_code": pu.get("polling_unit_code", "unknown"),
                "anomaly_score": prob,
                "is_anomaly": prob > 0.5,
                "confidence": abs(prob - 0.5) * 2,
            })

        return predictions

    @ray.remote
    def _ray_gnn_score(node_features: np.ndarray, edge_index: np.ndarray, models_dir: str) -> np.ndarray:
        """Score nodes using GNN model."""
        import torch
        model_path = Path(models_dir) / "gnn_election.pt"
        if not model_path.exists():
            return np.zeros(len(node_features))

        from ml.training.gnn_network.train import ElectionGAT

        model = ElectionGAT(in_channels=17, hidden_channels=64, heads=4)
        checkpoint = torch.load(str(model_path), weights_only=True, map_location="cpu")
        if isinstance(checkpoint, dict) and "model_state_dict" in checkpoint:
            model.load_state_dict(checkpoint["model_state_dict"])
            x_mean = checkpoint.get("feature_mean")
            x_std = checkpoint.get("feature_std")
        else:
            model.load_state_dict(checkpoint)
            x_mean, x_std = None, None

        model.eval()
        x = torch.tensor(node_features, dtype=torch.float32)
        ei = torch.tensor(edge_index, dtype=torch.long)

        if x_mean is not None and x_std is not None:
            x = (x - x_mean) / x_std.clamp(min=1e-8)

        with torch.no_grad():
            scores = model(x, ei)

        return scores.numpy().flatten()

    @ray.remote
    def _ray_compute_features(batch: list[dict]) -> list[dict]:
        """Compute ML features for a batch of raw results."""
        features = []
        for r in batch:
            reg = r.get("registered_voters", 0)
            acc = r.get("accredited_voters", 0)
            valid = r.get("total_valid_votes", 0)
            rej = r.get("rejected_votes", 0)
            turnout = acc / max(reg, 1)

            features.append({
                **r,
                "turnout_rate": turnout,
                "rejection_rate": rej / max(acc, 1),
                "overvoting_flag": int(valid > acc),
                "round_number_flag": int(valid > 0 and (valid % 100 == 0 or valid % 50 == 0)),
            })
        return features

except ImportError:
    log.warning("ray_not_available")
