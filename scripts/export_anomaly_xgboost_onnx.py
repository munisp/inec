#!/usr/bin/env python3
"""Export the trained INEC XGBoost anomaly classifier to ONNX for CPU serving."""
from __future__ import annotations

from pathlib import Path

import onnx
import onnxmltools
import xgboost as xgb
from onnxmltools.convert.common.data_types import FloatTensorType

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "ml" / "models" / "anomaly_xgboost.json"
TARGET = ROOT / "ml" / "models" / "anomaly_xgboost.onnx"
FEATURE_COUNT = 17


def main() -> None:
    if not SOURCE.is_file():
        raise FileNotFoundError(f"trained XGBoost artifact is missing: {SOURCE}")

    classifier = xgb.XGBClassifier()
    classifier.load_model(str(SOURCE))
    onnx_model = onnxmltools.convert_xgboost(
        classifier,
        initial_types=[("float_input", FloatTensorType([None, FEATURE_COUNT]))],
        target_opset=15,
    )
    TARGET.parent.mkdir(parents=True, exist_ok=True)
    onnx.save_model(onnx_model, str(TARGET))
    loaded = onnx.load(str(TARGET))
    onnx.checker.check_model(loaded)
    print(f"exported and validated {TARGET} ({TARGET.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
