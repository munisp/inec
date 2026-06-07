"""INEC Unified Training Orchestrator.

Trains all models end-to-end:
1. XGBoost anomaly detection (50K synthetic samples)
2. GNN election graph anomaly detection (10K node graph)
3. CDCN liveness/PAD model (synthetic face data)

Generates synthetic data, trains with proper loops, saves weights + metadata.
Can be run standalone or via Ray for distributed training.

Usage:
    python ml/training/train_all.py                    # Train all models sequentially
    python ml/training/train_all.py --model anomaly    # Train only anomaly model
    python ml/training/train_all.py --model gnn        # Train only GNN model
    python ml/training/train_all.py --model liveness   # Train only liveness model
    python ml/training/train_all.py --ray               # Train all models with Ray
"""

import argparse
import json
import os
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

MODELS_DIR = Path(__file__).parent.parent / "models"
DATA_DIR = Path(__file__).parent.parent / "data"


def train_anomaly(output_dir: str | None = None, n_samples: int = 50000):
    """Train XGBoost anomaly detection model."""
    print("\n" + "=" * 70)
    print("  TRAINING: XGBoost Anomaly Detection")
    print("=" * 70)
    start = time.time()

    from ml.training.anomaly_detection.train import train_model
    model, scaler, metadata = train_model(output_dir=output_dir)

    elapsed = time.time() - start
    print(f"\nXGBoost training completed in {elapsed:.1f}s")
    print(f"  ROC-AUC: {metadata['metrics']['roc_auc']:.4f}")
    print(f"  F1 (anomaly): {metadata['metrics']['f1_anomaly']:.4f}")
    return metadata


def train_gnn(output_dir: str | None = None, epochs: int = 200):
    """Train GNN election anomaly model."""
    print("\n" + "=" * 70)
    print("  TRAINING: Graph Neural Network (GAT)")
    print("=" * 70)
    start = time.time()

    from ml.training.gnn_network.train import train_gnn as _train_gnn
    _train_gnn(output_dir=output_dir, epochs=epochs)

    elapsed = time.time() - start
    print(f"\nGNN training completed in {elapsed:.1f}s")

    meta_path = (Path(output_dir) if output_dir else MODELS_DIR) / "gnn_model_metadata.json"
    if meta_path.exists():
        with open(meta_path) as f:
            return json.load(f)
    return {}


def train_liveness(output_dir: str | None = None, epochs: int = 20):
    """Train CDCN liveness/PAD model."""
    print("\n" + "=" * 70)
    print("  TRAINING: CDCN Liveness/PAD")
    print("=" * 70)
    start = time.time()

    from ml.training.liveness_pad.train import train_pad_model
    train_pad_model(output_dir=output_dir, epochs=epochs)

    elapsed = time.time() - start
    print(f"\nCDCN training completed in {elapsed:.1f}s")

    meta_path = (Path(output_dir) if output_dir else MODELS_DIR) / "liveness_model_metadata.json"
    if meta_path.exists():
        with open(meta_path) as f:
            return json.load(f)
    return {}


def train_all_sequential(output_dir: str | None = None):
    """Train all models sequentially."""
    print("\n" + "#" * 70)
    print("  INEC ML TRAINING PIPELINE — Sequential Mode")
    print("#" * 70)

    start = time.time()
    results = {}

    results["anomaly"] = train_anomaly(output_dir)
    results["gnn"] = train_gnn(output_dir, epochs=200)
    results["liveness"] = train_liveness(output_dir, epochs=20)

    total = time.time() - start
    print("\n" + "#" * 70)
    print(f"  ALL MODELS TRAINED — Total time: {total:.1f}s")
    print("#" * 70)

    # Save unified training report
    report = {
        "trained_at": datetime.now(timezone.utc).isoformat(),
        "total_time_seconds": round(total, 1),
        "models": results,
        "output_dir": str(output_dir or MODELS_DIR),
    }
    report_path = (Path(output_dir) if output_dir else MODELS_DIR) / "training_report.json"
    with open(report_path, "w") as f:
        json.dump(report, f, indent=2, default=str)
    print(f"Training report: {report_path}")

    return report


def train_all_ray(output_dir: str | None = None):
    """Train all models in parallel using Ray."""
    print("\n" + "#" * 70)
    print("  INEC ML TRAINING PIPELINE — Ray Distributed Mode")
    print("#" * 70)

    import ray

    if not ray.is_initialized():
        ray.init(num_cpus=os.cpu_count(), ignore_reinit_error=True)
        print(f"Ray initialized: {ray.cluster_resources()}")

    @ray.remote
    def ray_train_anomaly(out_dir):
        import sys
        sys.path.insert(0, str(Path(__file__).parent.parent.parent))
        from ml.training.anomaly_detection.train import train_model
        _, _, metadata = train_model(output_dir=out_dir)
        return {"model": "anomaly_xgboost", "metrics": metadata["metrics"]}

    @ray.remote
    def ray_train_gnn(out_dir, epochs):
        import sys
        sys.path.insert(0, str(Path(__file__).parent.parent.parent))
        from ml.training.gnn_network.train import train_gnn
        train_gnn(output_dir=out_dir, epochs=epochs)
        meta_path = Path(out_dir or MODELS_DIR) / "gnn_model_metadata.json"
        if meta_path.exists():
            with open(meta_path) as f:
                return json.load(f)
        return {"model": "gnn_election"}

    @ray.remote
    def ray_train_liveness(out_dir, epochs):
        import sys
        sys.path.insert(0, str(Path(__file__).parent.parent.parent))
        from ml.training.liveness_pad.train import train_pad_model
        train_pad_model(output_dir=out_dir, epochs=epochs)
        meta_path = Path(out_dir or MODELS_DIR) / "liveness_model_metadata.json"
        if meta_path.exists():
            with open(meta_path) as f:
                return json.load(f)
        return {"model": "liveness_cdcn"}

    out = str(output_dir or MODELS_DIR)
    start = time.time()

    # Launch all training tasks in parallel
    futures = [
        ray_train_anomaly.remote(out),
        ray_train_gnn.remote(out, 200),
        ray_train_liveness.remote(out, 20),
    ]

    print(f"Launched {len(futures)} training tasks on Ray...")
    results = ray.get(futures)

    total = time.time() - start
    print(f"\nAll models trained via Ray in {total:.1f}s")

    report = {
        "trained_at": datetime.now(timezone.utc).isoformat(),
        "total_time_seconds": round(total, 1),
        "mode": "ray_distributed",
        "models": {r.get("model", f"model_{i}"): r for i, r in enumerate(results)},
    }
    report_path = Path(out) / "training_report.json"
    with open(report_path, "w") as f:
        json.dump(report, f, indent=2, default=str)

    ray.shutdown()
    return report


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="INEC ML Training Orchestrator")
    parser.add_argument("--model", choices=["anomaly", "gnn", "liveness", "all"], default="all")
    parser.add_argument("--output", type=str, help="Output directory")
    parser.add_argument("--ray", action="store_true", help="Use Ray for distributed training")
    parser.add_argument("--epochs-gnn", type=int, default=200)
    parser.add_argument("--epochs-liveness", type=int, default=20)
    parser.add_argument("--samples", type=int, default=50000)
    args = parser.parse_args()

    sys.path.insert(0, str(Path(__file__).parent.parent.parent))

    if args.model == "all":
        if args.ray:
            train_all_ray(args.output)
        else:
            train_all_sequential(args.output)
    elif args.model == "anomaly":
        train_anomaly(args.output, args.samples)
    elif args.model == "gnn":
        train_gnn(args.output, args.epochs_gnn)
    elif args.model == "liveness":
        train_liveness(args.output, args.epochs_liveness)
