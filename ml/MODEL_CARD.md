# INEC AI/ML Model Card

## Overview
This document describes the AI/ML models deployed in the INEC election platform, their training data, known limitations, and retraining requirements.

---

## 1. XGBoost Anomaly Detection

| Field | Value |
|-------|-------|
| **Architecture** | XGBoost gradient boosted trees |
| **File** | `ml/models/xgboost_anomaly_model.json` (479KB) |
| **Training Data** | 50,000 synthetic election result records |
| **Features** | 10 numeric features (voter turnout ratio, rejected ballot ratio, party vote concentration, deviation from historical, etc.) |
| **Metrics** | ROC-AUC: 1.0, F1: 0.9959, Precision: 0.9917 |
| **Threshold** | 0.5 (anomaly if score > threshold) |

### Limitations
- **SYNTHETIC DATA ONLY**: Trained on algorithmically generated data, not real Nigerian election data
- **Perfect metrics are a red flag**: ROC-AUC of 1.0 suggests the synthetic data may be too separable; real-world performance will be lower
- **No geographic bias testing**: Not validated across different Nigerian states/regions
- **Feature drift**: If INEC changes result reporting formats, features must be recalculated
- **Adversarial robustness**: Not tested against deliberate evasion (e.g., sophisticated ballot stuffing that mimics normal patterns)

### Retraining Requirements
- Must be retrained on real election data from at least 2 prior elections before production use
- Minimum 100,000 real records recommended for training
- A/B test against rule-based anomaly detection for at least 1 election cycle
- Monitor feature distribution drift weekly using PSI (Population Stability Index)

---

## 2. GAT Graph Neural Network

| Field | Value |
|-------|-------|
| **Architecture** | 2-layer Graph Attention Network (PyTorch) |
| **File** | `ml/models/gat_gnn_model.pt` (374KB) |
| **Training Data** | 500-node synthetic graph with 3,000 edges |
| **Features** | 5 per node (turnout, rejection rate, timing, vote concentration, geo distance) |
| **Metrics** | F1: 0.9988, Precision: 0.9975 |

### Limitations
- **Tiny training graph**: 500 nodes vs 48,842 real polling units — graph structure doesn't reflect real geography
- **Synthetic edges**: Edges were randomly generated, not based on real administrative/geographic adjacency
- **Scalability unknown**: Not tested at 48K+ node scale; memory and inference time may be prohibitive
- **No temporal modeling**: Treats each election as a static snapshot, missing inter-election patterns

### Retraining Requirements
- Build real adjacency graph from INEC polling unit geographic data (PostGIS)
- Train on minimum 10,000 nodes with real adjacency
- Benchmark inference time at full scale (48K nodes, ~200K edges)
- Consider GraphSAGE for better scalability if GAT doesn't scale

---

## 3. CDCN Liveness Detection

| Field | Value |
|-------|-------|
| **Architecture** | Central Difference Convolutional Network (6.1M parameters) |
| **File** | `ml/models/cdcn_liveness_model.pt` (24.5MB) |
| **Training Data** | 1,000 synthetic face images (500 real, 500 spoof) |
| **Input** | 256x256 RGB images |
| **Training** | 20 epochs, loss: 0.0155 |

### Limitations
- **Critically insufficient training data**: 1K images is orders of magnitude below production requirements (minimum 100K+ diverse face images)
- **No real spoof attacks**: Synthetic spoofs don't represent real presentation attacks (printed photos, screen replay, 3D masks)
- **No demographic diversity**: Not tested across Nigerian population demographics (skin tone, age, lighting conditions)
- **BVAS camera variation**: Not validated on actual BVAS device camera hardware
- **Lighting conditions**: Not tested in outdoor/tent polling unit conditions typical in Nigeria
- **CPU-only inference**: Trained on CPU; inference latency on mobile BVAS devices unknown

### Retraining Requirements
- **CRITICAL**: Must not be deployed for real biometric verification without retraining on:
  - Minimum 100,000 real face images with diverse Nigerian demographics
  - Real presentation attack instruments (PAI) including printed photos, screen replays
  - Images captured on actual BVAS hardware in field conditions
- Comply with ISO/IEC 30107-3 for presentation attack detection testing
- Validate APCER (Attack Presentation Classification Error Rate) < 1% and BPCER < 5%

---

## Continuous Training Pipeline

The platform includes a continuous training pipeline (`ml/continuous_training.py`) that:
1. Monitors feature distribution drift using PSI and KS tests
2. Triggers retraining when drift exceeds thresholds
3. Manages model versioning via a local registry
4. Supports A/B deployment for safe model updates

### Before Production
1. Replace all synthetic training data with real election data
2. Establish baseline metrics on real data
3. Set up monitoring dashboards for model drift (Grafana + Prometheus)
4. Define rollback procedures if model performance degrades
5. Get independent audit of model fairness across regions and demographics
6. Document all model decisions for electoral transparency requirements
