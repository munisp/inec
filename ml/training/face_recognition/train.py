"""INEC Face Recognition — ArcFace Embedding Model.

Uses InsightFace's pre-trained ArcFace model for face embedding extraction,
with optional fine-tuning on African face datasets for improved accuracy
on Nigerian demographics.

Pipeline:
1. Face detection (RetinaFace/SCRFD)
2. Alignment (5-point landmark)
3. Embedding extraction (ArcFace R100)
4. Cosine similarity matching

Outputs: ONNX model + embedding database schema + evaluation metrics.
"""

import os
import json
import argparse
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import numpy as np

MODELS_DIR = Path(__file__).parent.parent.parent / "models"


class FaceRecognitionPipeline:
    """Production face recognition with InsightFace ArcFace."""

    def __init__(self, model_name: str = "buffalo_l", ctx_id: int = -1):
        """
        Args:
            model_name: InsightFace model pack (buffalo_l = ArcFace R100 + SCRFD)
            ctx_id: -1 for CPU, 0+ for GPU
        """
        self.model_name = model_name
        self.ctx_id = ctx_id
        self.app = None
        self._initialized = False

    def initialize(self):
        """Load InsightFace models (downloads on first use ~250MB)."""
        if self._initialized:
            return

        try:
            import insightface
            from insightface.app import FaceAnalysis

            self.app = FaceAnalysis(
                name=self.model_name,
                root=str(MODELS_DIR / "insightface"),
                providers=["CPUExecutionProvider"] if self.ctx_id < 0 else ["CUDAExecutionProvider"],
            )
            self.app.prepare(ctx_id=self.ctx_id, det_size=(640, 640))
            self._initialized = True
            print(f"InsightFace initialized: {self.model_name} (CPU={'yes' if self.ctx_id < 0 else 'no'})")
        except ImportError:
            raise RuntimeError("Install insightface: pip install insightface onnxruntime")

    def extract_embedding(self, image: np.ndarray) -> Optional[np.ndarray]:
        """Extract 512-d face embedding from image.

        Args:
            image: BGR numpy array (OpenCV format)

        Returns:
            512-dimensional L2-normalized embedding vector, or None if no face found.
        """
        self.initialize()
        faces = self.app.get(image)
        if len(faces) == 0:
            return None
        # Use largest face
        face = max(faces, key=lambda f: (f.bbox[2] - f.bbox[0]) * (f.bbox[3] - f.bbox[1]))
        return face.embedding  # 512-d, already L2-normalized

    def compare_faces(self, embedding1: np.ndarray, embedding2: np.ndarray) -> float:
        """Compute cosine similarity between two face embeddings.

        Returns:
            Similarity score in [-1, 1]. >0.4 is same person (threshold tunable).
        """
        return float(np.dot(embedding1, embedding2))

    def detect_faces(self, image: np.ndarray) -> list[dict]:
        """Detect all faces in image with landmarks and quality score."""
        self.initialize()
        faces = self.app.get(image)
        results = []
        for face in faces:
            bbox = face.bbox.astype(int).tolist()
            results.append({
                "bbox": bbox,
                "confidence": float(face.det_score),
                "landmarks": face.kps.tolist() if face.kps is not None else None,
                "age": int(face.age) if hasattr(face, "age") and face.age else None,
                "gender": "M" if hasattr(face, "gender") and face.gender == 1 else "F" if hasattr(face, "gender") else None,
                "embedding_dim": 512,
            })
        return results

    def verify_identity(self, image1: np.ndarray, image2: np.ndarray, threshold: float = 0.4) -> dict:
        """Verify if two images contain the same person.

        Args:
            image1: First image (e.g., selfie)
            image2: Second image (e.g., ID document photo)
            threshold: Cosine similarity threshold (0.4 = balanced, 0.5 = strict)

        Returns:
            Dict with match result, score, and confidence.
        """
        emb1 = self.extract_embedding(image1)
        emb2 = self.extract_embedding(image2)

        if emb1 is None:
            return {"verified": False, "score": 0.0, "error": "No face detected in image 1"}
        if emb2 is None:
            return {"verified": False, "score": 0.0, "error": "No face detected in image 2"}

        score = self.compare_faces(emb1, emb2)
        verified = score >= threshold

        # Confidence: distance from threshold normalized
        if verified:
            confidence = min(1.0, (score - threshold) / (1.0 - threshold))
        else:
            confidence = min(1.0, (threshold - score) / threshold)

        return {
            "verified": verified,
            "score": float(score),
            "threshold": threshold,
            "confidence": float(confidence),
        }


class FaceEmbeddingDatabase:
    """Manage face embedding storage for KYC verification.

    In production, use PostgreSQL with pgvector extension for
    efficient nearest-neighbor search on embeddings.
    """

    def __init__(self, db_path: Optional[str] = None):
        self.embeddings: dict[int, np.ndarray] = {}
        self.db_path = db_path

    def register(self, user_id: int, embedding: np.ndarray) -> bool:
        """Register a face embedding for a user."""
        if embedding is None or len(embedding) != 512:
            return False
        # L2 normalize
        norm = np.linalg.norm(embedding)
        if norm > 0:
            embedding = embedding / norm
        self.embeddings[user_id] = embedding
        return True

    def search(self, query_embedding: np.ndarray, threshold: float = 0.4, top_k: int = 5) -> list[dict]:
        """Search for matching faces in the database.

        Args:
            query_embedding: 512-d embedding to search for
            threshold: Minimum cosine similarity
            top_k: Maximum results to return

        Returns:
            List of matches with user_id and similarity score.
        """
        if len(self.embeddings) == 0:
            return []

        query_norm = query_embedding / max(np.linalg.norm(query_embedding), 1e-10)
        results = []

        for user_id, stored_emb in self.embeddings.items():
            score = float(np.dot(query_norm, stored_emb))
            if score >= threshold:
                results.append({"user_id": user_id, "score": score})

        results.sort(key=lambda x: x["score"], reverse=True)
        return results[:top_k]

    def check_duplicate(self, embedding: np.ndarray, threshold: float = 0.6) -> Optional[int]:
        """Check if this face is already registered (prevent duplicate registrations).

        Higher threshold (0.6) to avoid false positives on deduplication.
        """
        matches = self.search(embedding, threshold=threshold, top_k=1)
        if matches:
            return matches[0]["user_id"]
        return None


def evaluate_model(pipeline: FaceRecognitionPipeline, test_pairs: list[tuple]) -> dict:
    """Evaluate face verification accuracy on test pairs.

    Args:
        pipeline: Initialized face recognition pipeline
        test_pairs: List of (image1, image2, is_same_person) tuples

    Returns:
        Dict with TAR@FAR thresholds, ROC-AUC, and optimal threshold.
    """
    scores = []
    labels = []

    for img1, img2, is_same in test_pairs:
        result = pipeline.verify_identity(img1, img2)
        if "error" not in result:
            scores.append(result["score"])
            labels.append(1 if is_same else 0)

    scores = np.array(scores)
    labels = np.array(labels)

    # Find optimal threshold
    best_acc = 0
    best_threshold = 0.4
    for t in np.arange(0.2, 0.8, 0.01):
        predictions = (scores >= t).astype(int)
        acc = (predictions == labels).mean()
        if acc > best_acc:
            best_acc = acc
            best_threshold = t

    # Compute TAR@FAR
    from sklearn.metrics import roc_curve, auc
    fpr, tpr, thresholds = roc_curve(labels, scores)
    roc_auc = auc(fpr, tpr)

    # TAR at specific FAR levels
    tar_at_far = {}
    for target_far in [0.001, 0.01, 0.1]:
        idx = np.argmin(np.abs(fpr - target_far))
        tar_at_far[f"TAR@FAR={target_far}"] = float(tpr[idx])

    return {
        "roc_auc": float(roc_auc),
        "best_threshold": float(best_threshold),
        "best_accuracy": float(best_acc),
        "tar_at_far": tar_at_far,
        "n_pairs": len(scores),
    }


def export_model_metadata():
    """Export model card and metadata for the face recognition system."""
    metadata = {
        "model_type": "face_recognition",
        "architecture": "ArcFace (ResNet-100)",
        "version": "1.0.0",
        "framework": "InsightFace + ONNX Runtime",
        "embedding_dim": 512,
        "input_size": [112, 112],
        "detection_model": "SCRFD-10GF",
        "trained_on": "MS1MV3 (cleaned MS-Celeb-1M, 5.8M images, 93K identities)",
        "cpu_inference": True,
        "gpu_inference": True,
        "inference_latency": {
            "cpu_detection_ms": "30-80ms",
            "cpu_embedding_ms": "20-50ms",
            "cpu_total_ms": "50-130ms per face",
            "gpu_total_ms": "10-25ms per face",
        },
        "accuracy": {
            "LFW": 0.9983,
            "CFP-FP": 0.9821,
            "AgeDB-30": 0.9815,
            "note": "May need fine-tuning for African face demographics",
        },
        "recommended_thresholds": {
            "strict_kyc": 0.5,
            "standard_verification": 0.4,
            "deduplication": 0.6,
        },
        "fine_tuning_needed": [
            "Collect 10K+ Nigerian face images (diverse age/lighting/expression)",
            "Use ArcFace loss with margin=0.5, scale=64",
            "Train last 2 residual blocks only (freeze backbone)",
            "Evaluate on held-out Nigerian test set",
        ],
        "created_at": datetime.now(timezone.utc).isoformat(),
    }

    output_path = MODELS_DIR / "face_recognition_metadata.json"
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with open(output_path, "w") as f:
        json.dump(metadata, f, indent=2)
    print(f"Model metadata saved: {output_path}")
    return metadata


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="INEC Face Recognition Pipeline")
    parser.add_argument("--action", choices=["init", "evaluate", "metadata"], default="metadata")
    parser.add_argument("--ctx-id", type=int, default=-1, help="GPU device ID (-1 for CPU)")
    args = parser.parse_args()

    if args.action == "init":
        pipeline = FaceRecognitionPipeline(ctx_id=args.ctx_id)
        pipeline.initialize()
        print("Face recognition pipeline ready")
    elif args.action == "metadata":
        export_model_metadata()
    elif args.action == "evaluate":
        print("Evaluation requires test image pairs. Use --data to provide.")
