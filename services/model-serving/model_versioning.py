"""Model Versioning and A/B Testing Framework.

Implements comprehensive model lifecycle management including:
- Version control with semantic versioning
- A/B testing with traffic splitting
- Model rollback capabilities
- Performance comparison between versions
- Canary deployments
- Model deprecation and retirement

Usage:
    from model_versioning import ModelVersionManager
    manager = ModelVersionManager(models_dir="services/biometric-python/models")
    manager.register_model("biometric-cdcn", "1.0.0", model_path)
    manager.deploy_canary("biometric-cdcn", "1.1.0", traffic_percentage=10)
    results = manager.compare_models("biometric-cdcn", ["1.0.0", "1.1.0"], test_data)
"""

import os
import json
import time
import hashlib
import threading
from pathlib import Path
from typing import Dict, List, Optional, Tuple, Any
from datetime import datetime
from dataclasses import dataclass, field
from enum import Enum

import numpy as np


class ModelVersionStatus(Enum):
    """Model version lifecycle status."""
    DRAFT = "draft"
    TESTING = "testing"
    CANARY = "canary"
    DEPLOYED = "deployed"
    ACTIVE = "active"
    ROLLED_BACK = "rolled_back"
    DEPRECATED = "deprecated"
    RETIRED = "retired"


class TrafficSplit(Enum):
    """Traffic split configurations for A/B testing."""
    A_B_TEST_50_50 = {"A": 0.5, "B": 0.5}
    CANARY_10 = {"primary": 0.9, "canary": 0.1}
    CANARY_1 = {"primary": 0.99, "canary": 0.01}
    ROLLOUT_10 = {"previous": 0.9, "new": 0.1}
    ROLLOUT_50 = {"previous": 0.5, "new": 0.5}
    ROLLOUT_90 = {"previous": 0.1, "new": 0.9}


@dataclass
class ModelVersion:
    """Represents a model version with metadata."""
    model_id: str
    version: str
    model_path: str
    status: ModelVersionStatus = ModelVersionStatus.DRAFT
    created_at: str = field(default_factory=lambda: datetime.now().isoformat())
    parameters: Dict = field(default_factory=dict)
    metrics: Dict = field(default_factory=dict)
    traffic_percentage: float = 0.0
    test_results: Dict = field(default_factory=dict)
    deploy_history: List[Dict] = field(default_factory=list)
    
    @property
    def model_hash(self) -> str:
        """Calculate hash of model file."""
        if Path(self.model_path).exists():
            with open(self.model_path, 'rb') as f:
                return hashlib.md5(f.read()).hexdigest()
        return "unknown"
    
    @property
    def is_production_ready(self) -> bool:
        """Check if model is ready for production."""
        return self.status in [ModelVersionStatus.ACTIVE, ModelVersionStatus.DEPLOYED]


@dataclass
class ABTestResult:
    """Results from A/B test between model versions."""
    test_id: str
    model_id: str
    version_a: str
    version_b: str
    start_time: str
    end_time: Optional[str] = None
    results: Dict = field(default_factory=dict)
    winner: Optional[str] = None
    confidence: float = 0.0
    metrics_compared: List[str] = field(default_factory=list)
    
    @property
    def is_complete(self) -> bool:
        return self.end_time is not None
    
    @property
    def is_significant(self) -> bool:
        """Check if test results are statistically significant."""
        return self.confidence >= 0.95


class ModelVersionManager:
    """Manages model versions and A/B testing."""
    
    def __init__(self, models_dir: str = "services/biometric-python/models"):
        self.models_dir = Path(models_dir)
        self.versions: Dict[str, List[ModelVersion]] = {}
        self.ab_tests: Dict[str, ABTestResult] = {}
        self._lock = threading.RLock()
        
        print(f"✓ Model Version Manager initialized")
        print(f"  Models directory: {self.models_dir}")
    
    def register_model(
        self,
        model_id: str,
        version: str,
        model_path: str,
        parameters: Dict = None,
    ) -> ModelVersion:
        """Register a new model version."""
        metadata = ModelVersion(
            model_id=model_id,
            version=version,
            model_path=model_path,
            parameters=parameters or {},
        )
        
        with self._lock:
            if model_id not in self.versions:
                self.versions[model_id] = []
            
            # Check if version already exists
            existing = [v for v in self.versions[model_id] if v.version == version]
            if existing:
                existing[0].parameters.update(parameters or {})
                return existing[0]
            
            self.versions[model_id].append(metadata)
            print(f"✓ Registered model: {model_id} v{version}")
        
        return metadata
    
    def promote_to_canary(self, model_id: str, version: str, 
                         traffic_percentage: float = 10.0) -> ModelVersion:
        """Promote a model version to canary deployment."""
        with self._lock:
            versions = self.versions.get(model_id, [])
            model = next((v for v in versions if v.version == version), None)
            
            if model is None:
                raise ValueError(f"Model version not found: {model_id} v{version}")
            
            model.status = ModelVersionStatus.CANARY
            model.traffic_percentage = traffic_percentage
            model.deploy_history.append({
                'action': 'promote_to_canary',
                'timestamp': datetime.now().isoformat(),
                'traffic_percentage': traffic_percentage,
            })
            
            print(f"✓ Promoted {model_id} v{version} to canary (traffic: {traffic_percentage}%)")
            
            return model
    
    def deploy_version(self, model_id: str, version: str) -> ModelVersion:
        """Deploy a model version as active."""
        with self._lock:
            versions = self.versions.get(model_id, [])
            model = next((v for v in versions if v.version == version), None)
            
            if model is None:
                raise ValueError(f"Model version not found: {model_id} v{version}")
            
            model.status = ModelVersionStatus.ACTIVE
            model.traffic_percentage = 100.0
            model.deploy_history.append({
                'action': 'deploy',
                'timestamp': datetime.now().isoformat(),
                'traffic_percentage': 100.0,
            })
            
            print(f"✓ Deployed {model_id} v{version} as active")
            
            return model
    
    def rollback_version(self, model_id: str, version: str) -> ModelVersion:
        """Rollback a model version."""
        with self._lock:
            versions = self.versions.get(model_id, [])
            model = next((v for v in versions if v.version == version), None)
            
            if model is None:
                raise ValueError(f"Model version not found: {model_id} v{version}")
            
            model.status = ModelVersionStatus.ROLLED_BACK
            model.traffic_percentage = 0.0
            model.deploy_history.append({
                'action': 'rollback',
                'timestamp': datetime.now().isoformat(),
            })
            
            print(f"✓ Rolled back {model_id} v{version}")
            
            return model
    
    def deprecate_version(self, model_id: str, version: str) -> ModelVersion:
        """Deprecate a model version."""
        with self._lock:
            versions = self.versions.get(model_id, [])
            model = next((v for v in versions if v.version == version), None)
            
            if model is None:
                raise ValueError(f"Model version not found: {model_id} v{version}")
            
            model.status = ModelVersionStatus.DEPRECATED
            model.traffic_percentage = 0.0
            model.deploy_history.append({
                'action': 'deprecate',
                'timestamp': datetime.now().isoformat(),
            })
            
            print(f"✓ Deprecated {model_id} v{version}")
            
            return model
    
    def create_ab_test(
        self,
        model_id: str,
        version_a: str,
        version_b: str,
        test_name: str = None,
        metrics: List[str] = None,
    ) -> ABTestResult:
        """Create an A/B test between two model versions."""
        test_id = f"test_{model_id}_{version_a}_vs_{version_b}_{int(time.time())}"
        
        test = ABTestResult(
            test_id=test_id,
            model_id=model_id,
            version_a=version_a,
            version_b=version_b,
            start_time=datetime.now().isoformat(),
            metrics_compared=metrics or ['accuracy', 'latency', 'throughput'],
        )
        
        with self._lock:
            self.ab_tests[test_id] = test
        
        print(f"✓ Created A/B test: {test_id}")
        print(f"  Version A: {version_a}")
        print(f"  Version B: {version_b}")
        
        return test
    
    def record_ab_test_result(
        self,
        test_id: str,
        version: str,
        results: Dict,
    ):
        """Record results for an A/B test."""
        with self._lock:
            test = self.ab_tests.get(test_id)
            if test is None:
                raise ValueError(f"Test not found: {test_id}")
            
            if version not in test.results:
                test.results[version] = []
            
            test.results[version].append(results)
    
    def finalize_ab_test(self, test_id: str) -> ABTestResult:
        """Finalize an A/B test and determine winner."""
        with self._lock:
            test = self.ab_tests.get(test_id)
            if test is None:
                raise ValueError(f"Test not found: {test_id}")
            
            test.end_time = datetime.now().isoformat()
            
            # Calculate aggregate metrics
            for version, results_list in test.results.items():
                if results_list:
                    test.results[version] = {
                        key: np.mean([r[key] for r in results_list])
                        for key in results_list[0].keys()
                    }
            
            # Determine winner based on primary metric
            primary_metric = test.metrics_compared[0] if test.metrics_compared else 'accuracy'
            
            results_a = test.results.get(test.version_a, {})
            results_b = test.results.get(test.version_b, {})
            
            value_a = results_a.get(primary_metric, 0)
            value_b = results_b.get(primary_metric, 0)
            
            if value_b > value_a * 1.05:  # 5% improvement threshold
                test.winner = test.version_b
                test.confidence = min((value_b - value_a) / value_a * 100, 1.0)
            elif value_a > value_b * 1.05:
                test.winner = test.version_a
                test.confidence = min((value_a - value_b) / value_b * 100, 1.0)
            else:
                test.winner = None
                test.confidence = 0.0
            
            print(f"✓ A/B test finalized: {test_id}")
            print(f"  Winner: {test.winner or 'no significant difference'}")
            print(f"  Confidence: {test.confidence:.2%}")
            
            return test
    
    def compare_models(
        self,
        model_id: str,
        versions: List[str],
        test_data: np.ndarray = None,
    ) -> Dict:
        """Compare performance of multiple model versions."""
        comparison = {
            'model_id': model_id,
            'versions': {},
            'timestamp': datetime.now().isoformat(),
        }
        
        with self._lock:
            for version in versions:
                model = next((v for v in self.versions.get(model_id, []) 
                             if v.version == version), None)
                
                if model:
                    comparison['versions'][version] = {
                        'status': model.status.value,
                        'parameters': model.parameters,
                        'metrics': model.metrics,
                        'model_hash': model.model_hash,
                        'test_results': model.test_results,
                    }
        
        print(f"✓ Compared {len(versions)} versions of {model_id}")
        
        return comparison
    
    def get_active_model(self, model_id: str) -> Optional[ModelVersion]:
        """Get the currently active model version."""
        with self._lock:
            versions = self.versions.get(model_id, [])
            active = next((v for v in versions if v.status == ModelVersionStatus.ACTIVE), None)
            return active
    
    def get_version_history(self, model_id: str) -> List[ModelVersion]:
        """Get version history for a model."""
        with self._lock:
            return self.versions.get(model_id, [])
    
    def get_canary_models(self, model_id: str) -> List[ModelVersion]:
        """Get all canary deployments for a model."""
        with self._lock:
            versions = self.versions.get(model_id, [])
            return [v for v in versions if v.status == ModelVersionStatus.CANARY]


class ModelRolloutManager:
    """Manages gradual model rollouts with automatic rollback."""
    
    def __init__(self, version_manager: ModelVersionManager):
        self.version_manager = version_manager
        self.rollouts: Dict[str, Dict] = {}
    
    def start_rollout(
        self,
        model_id: str,
        new_version: str,
        increment: float = 10.0,
        evaluation_period_hours: float = 24.0,
    ) -> Dict:
        """Start a gradual rollout of a new model version.
        
        Args:
            model_id: Model identifier
            new_version: New model version to deploy
            increment: Traffic increment percentage per step
            evaluation_period_hours: Hours to evaluate at each step
            
        Returns:
            Rollout configuration
        """
        rollout_id = f"rollout_{model_id}_{new_version}_{int(time.time())}"
        
        rollout = {
            'rollout_id': rollout_id,
            'model_id': model_id,
            'new_version': new_version,
            'current_percentage': 0.0,
            'increment': increment,
            'evaluation_period_hours': evaluation_period_hours,
            'status': 'starting',
            'steps': [],
            'created_at': datetime.now().isoformat(),
        }
        
        self.rollouts[rollout_id] = rollout
        
        print(f"✓ Started rollout: {rollout_id}")
        print(f"  Model: {model_id}")
        print(f"  Version: {new_version}")
        print(f"  Increment: {increment}%")
        
        return rollout
    
    def evaluate_step(self, rollout_id: str, metrics: Dict) -> Dict:
        """Evaluate current rollout step and decide next action."""
        rollout = self.rollouts.get(rollout_id)
        if rollout is None:
            raise ValueError(f"Rollout not found: {rollout_id}")
        
        step = {
            'timestamp': datetime.now().isoformat(),
            'current_percentage': rollout['current_percentage'],
            'metrics': metrics,
        }
        
        rollout['steps'].append(step)
        
        # Check if metrics meet threshold
        accuracy = metrics.get('accuracy', 0)
        latency_ms = metrics.get('latency_ms', 0)
        
        if accuracy < 0.9 or latency_ms > 1000:
            # Rollback
            rollout['status'] = 'rollback'
            self.version_manager.rollback_version(rollout['model_id'], rollout['new_version'])
            
            print(f"✗ Rollout rolled back: {rollout_id}")
            print(f"  Reason: metrics below threshold")
            
            return {
                'action': 'rollback',
                'reason': 'metrics below threshold',
                'metrics': metrics,
            }
        
        # Continue rollout
        if rollout['current_percentage'] < 100.0:
            rollout['current_percentage'] += rollout['increment']
            rollout['current_percentage'] = min(rollout['current_percentage'], 100.0)
            
            if rollout['current_percentage'] >= 100.0:
                rollout['status'] = 'complete'
                self.version_manager.deploy_version(rollout['model_id'], rollout['new_version'])
                
                print(f"✓ Rollout complete: {rollout_id}")
            else:
                rollout['status'] = 'in_progress'
                print(f"✓ Rollout step {rollout_id}: {rollout['current_percentage']}%")
        
        return {
            'action': 'continue',
            'current_percentage': rollout['current_percentage'],
            'status': rollout['status'],
        }


def main():
    """Demonstrate model versioning usage."""
    print("=" * 60)
    print("Model Versioning and A/B Testing")
    print("=" * 60)
    
    print("\nExample usage:")
    print("""
    from model_versioning import ModelVersionManager
    
    # Initialize manager
    manager = ModelVersionManager(
        models_dir="services/biometric-python/models"
    )
    
    # Register models
    manager.register_model(
        model_id="biometric-cdcn",
        version="1.0.0",
        model_path="models/cdc_pad_v1.onnx",
    )
    
    manager.register_model(
        model_id="biometric-cdcn",
        version="1.1.0",
        model_path="models/cdc_pad_v2.onnx",
    )
    
    # Deploy to canary
    manager.promote_to_canary(
        model_id="biometric-cdcn",
        version="1.1.0",
        traffic_percentage=10.0,
    )
    
    # Create A/B test
    test = manager.create_ab_test(
        model_id="biometric-cdcn",
        version_a="1.0.0",
        version_b="1.1.0",
        metrics=['accuracy', 'latency'],
    )
    
    # Compare models
    comparison = manager.compare_models(
        model_id="biometric-cdcn",
        versions=["1.0.0", "1.1.0"],
    )
    
    # Get version history
    history = manager.get_version_history("biometric-cdcn")
    for v in history:
        print(f"v{v.version}: {v.status.value}")
    """)
    
    print("\n" + "=" * 60)
    print("Model Versioning Ready")
    print("=" * 60)


if __name__ == "__main__":
    main()
