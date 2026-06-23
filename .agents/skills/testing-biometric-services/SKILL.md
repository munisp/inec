---
name: testing-biometric-services
description: Test the INEC biometric services (Python image processing, Rust crypto vault, Go ABIS pipeline) for compilation, runtime correctness, and algorithm validation. Use when verifying changes to services/biometric-python, services/biometric-rust, or services/biometric-go.
---

# Testing Biometric Services

## Overview
Three backend services implementing production biometric processing. All testing is shell-based (no browser/GUI needed, no recording).

## Prerequisites

### Build Tools Required
- **Go 1.22+**: Located at `/usr/local/go/bin/go` (add to PATH: `export PATH="/usr/local/go/bin:$PATH"`)
- **Rust/Cargo**: Available via `~/.cargo/bin/cargo`
- **Python 3.12+**: Available via system python3

### Python Dependencies
Install before testing Python service:
```bash
pip install opencv-python-headless scikit-image scipy numpy fastapi uvicorn pyyaml prometheus-client
```

## Test Execution

### 1. Rust Crypto Vault + Matching (`cargo test`)
```bash
cd services/biometric-rust
cargo test
# Expected: 15 tests pass (5 vault + 4 cancelable + 6 matching)
# Key assertions:
# - encrypt_decrypt_roundtrip: ciphertext != plaintext, decrypted == original
# - key_rotation: new_key_id != old, data still decryptable
# - revoked_key_blocks_decrypt: Err on decrypt
# - tampered_ciphertext_detected: XOR'd byte -> auth failure
# - fingerprint_self_match: score > 0.9, decision=Match
# - face_self_match: score ~ 1.0 (within 1e-6)
# - iris_self_match: score ~ 1.0 (within 1e-6)
# - score_fusion: weighted_sum(0.8*0.5 + 0.7*0.5) ~ 0.75
```

### 2. Python Fingerprint Engine
```python
import numpy as np, sys
sys.path.insert(0, 'services/biometric-python')
from fingerprint_engine import FingerprintEngine, FingerprintMatcher

engine = FingerprintEngine()

# Generate synthetic ridge image
img = np.zeros((300, 300), dtype=np.uint8)
for y in range(300):
    for x in range(300):
        img[y, x] = int(128 + 64 * np.sin(x * 0.3 + y * 0.1))
img += np.random.randint(0, 20, (300, 300), dtype=np.uint8)

template = engine.extract_template(img, dpi=500)
# Expected: len(template.minutiae) > 0, nfiq2_score in [1,5]
# Self-match score should be 1.0, cross-match < self-match
```

### 3. Python Facial Engine
```python
from facial_engine import FacialEngine
import cv2

engine = FacialEngine()
# Create synthetic face (oval + eye circles on black bg)
face_img = np.zeros((480, 640, 3), dtype=np.uint8)
cv2.ellipse(face_img, (320, 200), (80, 100), 0, 0, 360, (200, 180, 160), -1)
cv2.circle(face_img, (290, 180), 10, (50, 50, 50), -1)
cv2.circle(face_img, (350, 180), 10, (50, 50, 50), -1)

template = engine.extract_template(face_img)
# Expected: len(template.embedding) in (128, 512)
# Note: field is `bbox` not `bounding_box`, quality is a FaceQuality dataclass not dict
```

### 4. Python Iris Engine
```python
from iris_engine import IrisEngine

engine = IrisEngine()
iris_img = np.zeros((480, 640), dtype=np.uint8)
cv2.circle(iris_img, (320, 240), 100, 180, -1)
cv2.circle(iris_img, (320, 240), 40, 30, -1)

template = engine.extract_template(iris_img)
# Expected: len(bytes(template.code)) == 256, template.bits == 2048
# Note: fields are `code`, `mask`, `bits` (not iris_code, iris_mask, code_bits)
```

### 5. Go ABIS Pipeline
```bash
export PATH="/usr/local/go/bin:$PATH"
cd services/biometric-go
go build -o /tmp/biometric-go-abis .
/tmp/biometric-go-abis &
sleep 2

# Health check
curl -s http://localhost:8092/health
# Expected: {"status":"healthy", ...} with capabilities list

# BVAS lifecycle
curl -s -X POST http://localhost:8092/bvas/devices/register -d '{"device_id":"BVAS-001","firmware_version":"2.1.0","supported_modalities":["fingerprint","facial"]}'
curl -s -X POST http://localhost:8092/bvas/devices/BVAS-001/heartbeat
curl -s -X POST http://localhost:8092/bvas/devices/BVAS-001/capture/start -d '{"voter_vin":"VIN001","modality":"fingerprint"}'
curl -s http://localhost:8092/bvas/stats
# Expected: total_devices >= 1

kill %1
```

### 6. Config YAML
```python
import yaml
with open('config/biometric-services.yaml') as f:
    config = yaml.safe_load(f)
# Verify: 3 sections (biometric_python, biometric_rust, biometric_go)
# Spot-check: min_minutiae=12, embedding_dim=512, AES-256-GCM, face_threshold=0.45, lsh_tables=20
```

## Known Issues
- Python `FacialTemplate.quality` is a `FaceQuality` dataclass, not a dict -- don't call `.keys()` on it
- Python `IrisCode` fields are `code`, `mask`, `bits` -- not `iris_code`, `iris_mask`, `code_bits`
- Rust has ~11 unused-code warnings (items used only in HTTP handlers, not in unit tests)
- Go `fmt` import might be unused if only used in other files -- remove if build fails

## Not Testable Without Infrastructure
- Inter-service pipeline (Python<->Rust<->Go) -- needs all 3 running + Docker networking
- TypeScript WebAuthn/WebUSB -- needs browser + biometric hardware
- Docker compose orchestration -- needs Docker daemon
- gRPC protobuf compilation -- needs protoc + language-specific plugins
- FastAPI service startup -- needs full virtualenv with all deps

## Devin Secrets Needed
None required for compilation/runtime testing. All testing uses synthetic data generated with numpy.
