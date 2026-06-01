# INEC ML Training & Inference Makefile

PYTHON := python3
MODELS_DIR := models
DATA_DIR := data

.PHONY: all train-anomaly train-face train-liveness train-gnn serve clean

all: train-anomaly train-liveness train-gnn metadata

# ── Training ──

train-anomaly:
	@echo "Training XGBoost anomaly detection model..."
	$(PYTHON) training/anomaly_detection/train.py --output $(MODELS_DIR)

train-liveness:
	@echo "Training CDCN liveness/PAD model..."
	$(PYTHON) training/liveness_pad/train.py --output $(MODELS_DIR) --epochs 10

train-gnn:
	@echo "Training GNN election network model..."
	$(PYTHON) training/gnn_network/train.py --output $(MODELS_DIR) --epochs 50

metadata:
	@echo "Generating face recognition metadata..."
	$(PYTHON) training/face_recognition/train.py --action metadata

# ── Serving ──

serve-python:
	@echo "Starting Python ML inference server on :8090..."
	cd serving && $(PYTHON) -m uvicorn inference_server:app --host 0.0.0.0 --port 8090

serve-rust:
	@echo "Starting Rust inference engine on :8091..."
	cd ../services/inference-engine && cargo run --release

# ── Utilities ──

install:
	pip install -r requirements.txt

clean:
	rm -rf $(MODELS_DIR)/*.onnx $(MODELS_DIR)/*.pt $(MODELS_DIR)/*.json $(MODELS_DIR)/*.pkl
	rm -rf $(DATA_DIR)/processed/*

test:
	$(PYTHON) -c "from training.anomaly_detection.train import generate_synthetic_training_data; d=generate_synthetic_training_data(100); print(f'Generated {len(d)} samples, {d.label.sum()} anomalies')"
	@echo "ML pipeline smoke test passed"
