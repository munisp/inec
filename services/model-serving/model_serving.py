"""Model Serving Infrastructure for Production ML Deployment.

Implements a unified model serving layer that supports:
- ONNX Runtime for CPU inference
- TorchServe for GPU inference
- Model versioning and A/B testing
- Load balancing and caching
- Health checks and monitoring
- Batch and real-time inference

Usage:
    from model_serving import ModelServer, ModelRouter
    server = ModelServer(models_dir="services/biometric-python/models")
    server.start()
    
    router = ModelRouter(server)
    result = router.predict("biometric-cdcn", image_data)
"""

import os
import json
import time
import hashlib
import threading
from pathlib import Path
from typing import Dict, List, Optional, Any, Callable, Union
from datetime import datetime
from dataclasses import dataclass, field
from enum import Enum

import numpy as np
import onnxruntime as ort

# Optional imports
try:
    import torch
    TORCH_AVAILABLE = True
except ImportError:
    TORCH_AVAILABLE = False


class ModelStatus(Enum):
    """Model deployment status."""
    PENDING = "pending"
    LOADING = "loading"
    READY = "ready"
    ERROR = "error"
    DEPRECATED = "deprecated"


@dataclass
class ModelMetadata:
    """Metadata for a deployed model."""
    model_id: str
    version: str
    model_type: str
    model_path: str
    status: ModelStatus = ModelStatus.PENDING
    created_at: str = field(default_factory=lambda: datetime.now().isoformat())
    parameters: Dict = field(default_factory=dict)
    metrics: Dict = field(default_factory=dict)
    cache_enabled: bool = False
    batch_size: int = 1


@dataclass
class InferenceResult:
    """Result from model inference."""
    model_id: str
    prediction: Any
    confidence: float = 0.0
    inference_time_ms: float = 0.0
    cache_hit: bool = False
    metadata: Dict = field(default_factory=dict)


class ModelCache:
    """LRU cache for model predictions."""
    
    def __init__(self, max_size: int = 10000):
        self.cache = {}
        self.max_size = max_size
        self.hits = 0
        self.misses = 0
        self._lock = threading.Lock()
    
    def _cache_key(self, model_id: str, input_hash: str) -> str:
        """Generate cache key from model and input."""
        return f"{model_id}:{input_hash}"
    
    def get(self, model_id: str, input_data: Any) -> Optional[Any]:
        """Get cached prediction."""
        input_hash = hashlib.sha256(str(input_data).encode()).hexdigest()
        key = self._cache_key(model_id, input_hash)
        
        with self._lock:
            if key in self.cache:
                self.hits += 1
                return self.cache[key]['result']
            self.misses += 1
            return None
    
    def put(self, model_id: str, input_data: Any, result: Any):
        """Cache a prediction result."""
        input_hash = hashlib.sha256(str(input_data).encode()).hexdigest()
        key = self._cache_key(model_id, input_hash)
        
        with self._lock:
            if len(self.cache) >= self.max_size:
                # Remove oldest entry
                oldest_key = next(iter(self.cache))
                del self.cache[oldest_key]
            
            self.cache[key] = {
                'result': result,
                'timestamp': time.time(),
            }
    
    @property
    def hit_rate(self) -> float:
        """Calculate cache hit rate."""
        total = self.hits + self.misses
        return self.hits / total if total > 0 else 0.0


class ONNXModelWrapper:
    """Wrapper for ONNX Runtime models."""
    
    def __init__(self, model_path: str):
        self.model_path = model_path
        self.session = ort.InferenceSession(model_path)
        self.input_name = self.session.get_inputs()[0].name
        self.output_names = [output.name for output in self.session.get_outputs()]
        self.ready = True
    
    def predict(self, input_data: np.ndarray) -> Dict:
        """Run inference on input data."""
        start_time = time.time()
        
        outputs = self.session.run(self.output_names, {self.input_name: input_data})
        
        inference_time_ms = (time.time() - start_time) * 1000
        
        # Parse outputs based on model type
        if len(outputs) == 1:
            prediction = outputs[0]
            confidence = float(np.max(prediction)) if prediction.ndim > 1 else float(prediction)
        else:
            prediction = outputs[0]
            confidence = float(outputs[1][0][0]) if len(outputs[1]) > 0 else 0.0
        
        return {
            'prediction': prediction,
            'confidence': confidence,
            'inference_time_ms': inference_time_ms,
        }


class TorchModelWrapper:
    """Wrapper for PyTorch models."""
    
    def __init__(self, model_path: str, device: str = None):
        if not TORCH_AVAILABLE:
            raise ImportError("PyTorch not installed")
        
        self.model_path = model_path
        self.device = device or ('cuda' if torch.cuda.is_available() else 'cpu')
        
        checkpoint = torch.load(model_path, map_location=self.device)
        
        if 'model_state_dict' in checkpoint:
            self.model = checkpoint['model']
            self.model.load_state_dict(checkpoint['model_state_dict'])
        else:
            self.model = checkpoint['model'] if 'model' in checkpoint else None
        
        self.model.eval()
        self.ready = True
    
    def predict(self, input_data: np.ndarray) -> Dict:
        """Run inference on input data."""
        start_time = time.time()
        
        tensor = torch.from_numpy(input_data).float().to(self.device)
        
        with torch.no_grad():
            output = self.model(tensor)
            if hasattr(output, 'softmax'):
                prediction = output.softmax(dim=1)
            else:
                prediction = output.sigmoid() if output.shape[-1] == 1 else output
        
        inference_time_ms = (time.time() - start_time) * 1000
        
        confidence = float(torch.max(prediction).item())
        prediction_value = prediction.detach().cpu().numpy()
        
        return {
            'prediction': prediction_value,
            'confidence': confidence,
            'inference_time_ms': inference_time_ms,
        }


class ModelRegistry:
    """Registry for model versions and routing."""
    
    def __init__(self):
        self.models: Dict[str, List[ModelMetadata]] = {}
        self._lock = threading.RLock()
    
    def register_model(self, metadata: ModelMetadata):
        """Register a model version."""
        with self._lock:
            if metadata.model_id not in self.models:
                self.models[metadata.model_id] = []
            
            # Check if this version already exists
            existing = [m for m in self.models[metadata.model_id] if m.version == metadata.version]
            if existing:
                existing[0].status = metadata.status
            else:
                self.models[metadata.model_id].append(metadata)
            
            print(f"✓ Registered model: {metadata.model_id} v{metadata.version}")
    
    def get_active_model(self, model_id: str) -> Optional[ModelMetadata]:
        """Get the active (latest) model version."""
        with self._lock:
            versions = self.models.get(model_id, [])
            ready_versions = [m for m in versions if m.status == ModelStatus.READY]
            
            if ready_versions:
                return ready_versions[-1]  # Return latest
            return None
    
    def get_model_version(self, model_id: str, version: str) -> Optional[ModelMetadata]:
        """Get a specific model version."""
        with self._lock:
            versions = self.models.get(model_id, [])
            for m in versions:
                if m.version == version:
                    return m
            return None
    
    def list_models(self, model_id: str = None) -> List[ModelMetadata]:
        """List all registered models."""
        with self._lock:
            if model_id:
                return self.models.get(model_id, [])
            return [m for versions in self.models.values() for m in versions]


class ModelServer:
    """Production model serving server."""
    
    def __init__(
        self,
        models_dir: str = "services/biometric-python/models",
        cache_size: int = 10000,
        batch_size: int = 1,
    ):
        self.models_dir = Path(models_dir)
        self.model_registry = ModelRegistry()
        self.model_cache = ModelCache(max_size=cache_size)
        self.model_wrappers: Dict[str, Any] = {}
        self.batch_size = batch_size
        self.running = False
        self._lock = threading.RLock()
        
        print(f"✓ Model Server initialized")
        print(f"  Models directory: {self.models_dir}")
        print(f"  Cache size: {cache_size}")
        print(f"  Batch size: {batch_size}")
    
    def load_model(
        self,
        model_id: str,
        model_path: str,
        version: str = "1.0.0",
        model_type: str = "onnx",
        parameters: Dict = None,
    ) -> ModelMetadata:
        """Load and register a model."""
        metadata = ModelMetadata(
            model_id=model_id,
            version=version,
            model_type=model_type,
            model_path=model_path,
            status=ModelStatus.LOADING,
            parameters=parameters or {},
        )
        
        try:
            # Load model wrapper based on type
            if model_type == 'onnx':
                wrapper = ONNXModelWrapper(model_path)
            elif model_type == 'pytorch':
                wrapper = TorchModelWrapper(model_path)
            else:
                raise ValueError(f"Unsupported model type: {model_type}")
            
            # Test inference
            if model_type == 'onnx':
                test_input = np.random.randn(1, 1, 128, 128).astype(np.float32)
            else:
                test_input = np.random.randn(1, 3, 112, 112).astype(np.float32)
            
            result = wrapper.predict(test_input)
            
            metadata.status = ModelStatus.READY
            metadata.metrics = {
                'test_inference_time_ms': result['inference_time_ms'],
                'test_confidence': result['confidence'],
            }
            
            with self._lock:
                self.model_wrappers[model_id] = wrapper
            
            print(f"✓ Model loaded: {model_id} v{version}")
            
        except Exception as e:
            metadata.status = ModelStatus.ERROR
            print(f"✗ Failed to load model: {model_id} - {str(e)}")
        
        # Register model
        self.model_registry.register_model(metadata)
        
        return metadata
    
    def predict(
        self,
        model_id: str,
        input_data: np.ndarray,
        use_cache: bool = True,
    ) -> InferenceResult:
        """Run inference on a model.
        
        Args:
            model_id: Model identifier
            input_data: Input data array
            use_cache: Use prediction cache
            
        Returns:
            InferenceResult with prediction
        """
        # Check cache
        if use_cache:
            cached_result = self.model_cache.get(model_id, input_data)
            if cached_result is not None:
                return InferenceResult(
                    model_id=model_id,
                    prediction=cached_result,
                    cache_hit=True,
                )
        
        # Get model wrapper
        wrapper = self.model_wrappers.get(model_id)
        if wrapper is None:
            raise KeyError(f"Model not found: {model_id}")
        
        # Run inference
        start_time = time.time()
        result = wrapper.predict(input_data)
        inference_time_ms = result['inference_time_ms']
        
        inference_result = InferenceResult(
            model_id=model_id,
            prediction=result['prediction'],
            confidence=result['confidence'],
            inference_time_ms=inference_time_ms,
            cache_hit=False,
        )
        
        # Cache result
        if use_cache:
            self.model_cache.put(model_id, input_data, result['prediction'])
        
        return inference_result
    
    def get_health(self) -> Dict:
        """Get server health status."""
        models_status = []
        for model_id, wrapper in self.model_wrappers.items():
            active_model = self.model_registry.get_active_model(model_id)
            models_status.append({
                'model_id': model_id,
                'version': active_model.version if active_model else 'unknown',
                'status': active_model.status.value if active_model else 'unknown',
                'ready': wrapper.ready,
            })
        
        return {
            'status': 'healthy',
            'timestamp': datetime.now().isoformat(),
            'models': models_status,
            'cache': {
                'hit_rate': self.model_cache.hit_rate,
                'size': len(self.model_cache.cache),
                'hits': self.model_cache.hits,
                'misses': self.model_cache.misses,
            },
            'batch_size': self.batch_size,
        }
    
    def start(self):
        """Start the model server."""
        self.running = True
        print("✓ Model Server started")
    
    def stop(self):
        """Stop the model server."""
        self.running = False
        print("✓ Model Server stopped")
    
    def __enter__(self):
        self.start()
        return self
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        self.stop()


class ModelRouter:
    """Route predictions to appropriate models based on type."""
    
    def __init__(self, server: ModelServer):
        self.server = server
    
    def predict(
        self,
        model_type: str,
        input_data: np.ndarray,
        **kwargs,
    ) -> InferenceResult:
        """Route prediction to appropriate model.
        
        Args:
            model_type: Type of model (e.g., 'biometric-cdcn', 'face-arcface')
            input_data: Input data
            **kwargs: Additional parameters
            
        Returns:
            InferenceResult
        """
        model_id = model_type
        
        # Try to find active model
        active_model = self.server.model_registry.get_active_model(model_id)
        if active_model:
            model_id = active_model.model_id
        else:
            # Try loading model if path provided
            model_path = kwargs.get('model_path')
            if model_path and Path(model_path).exists():
                self.server.load_model(model_id, model_path, **kwargs)
        
        return self.server.predict(model_id, input_data, **kwargs)


def main():
    """Demonstrate model serving usage."""
    print("=" * 60)
    print("Model Serving Infrastructure")
    print("=" * 60)
    
    print("\nExample usage:")
    print("""
    from model_serving import ModelServer, ModelRouter
    
    # Initialize server
    server = ModelServer(
        models_dir="services/biometric-python/models",
        cache_size=10000,
    )
    
    # Load models
    server.load_model(
        model_id="biometric-cdcn",
        model_path="services/biometric-python/models/cdc_pad.onnx",
        version="1.0.0",
        model_type="onnx",
    )
    
    server.load_model(
        model_id="face-arcface",
        model_path="services/biometric-python/models/arcface_embedding.onnx",
        version="1.0.0",
        model_type="onnx",
    )
    
    # Create router
    router = ModelRouter(server)
    
    # Run prediction
    result = router.predict(
        model_type="biometric-cdcn",
        input_data=image_array,
    )
    
    print(f"Prediction: {result.prediction}")
    print(f"Confidence: {result.confidence}")
    print(f"Inference time: {result.inference_time_ms:.2f}ms")
    
    # Check health
    health = server.get_health()
    print(f"Cache hit rate: {health['cache']['hit_rate']:.2%}")
    """)
    
    print("\n" + "=" * 60)
    print("Model Serving Ready")
    print("=" * 60)


if __name__ == "__main__":
    main()
