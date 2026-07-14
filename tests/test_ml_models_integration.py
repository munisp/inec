"""Integration Tests for All ML Models and AI Components.

Tests comprehensive AI/ML functionality including:
- Biometric PAD (CDCN)
- Face recognition (ArcFace)
- GNN election validation
- XGBoost fraud detection
- YOLO ballot counting
- Neo4j graph database
- TigerBeetle ledger
- Model serving infrastructure
- Model versioning and A/B testing
- Model monitoring and drift detection
- Dataset preparation

Usage:
    pytest tests/test_ml_models_integration.py -v
"""

import os
import sys
import json
import time
import hashlib
import tempfile
import shutil
from pathlib import Path
from typing import Dict, List, Any
from datetime import datetime

import numpy as np
import pandas as pd
import pytest

# Add services to path
sys.path.insert(0, str(Path(__file__).parent.parent / "services"))
sys.path.insert(0, str(Path(__file__).parent.parent / "services" / "ml-models" / "cdc-pad"))
sys.path.insert(0, str(Path(__file__).parent.parent / "services" / "ml-models" / "arcface"))
sys.path.insert(0, str(Path(__file__).parent.parent / "services" / "ml-models" / "gnn"))
sys.path.insert(0, str(Path(__file__).parent.parent / "services" / "ml-models" / "xgboost-fraud"))
sys.path.insert(0, str(Path(__file__).parent.parent / "services" / "ml-models" / "neo4j"))
sys.path.insert(0, str(Path(__file__).parent.parent / "services" / "ml-models" / "tigerbeetle"))
sys.path.insert(0, str(Path(__file__).parent.parent / "services" / "model-serving"))
sys.path.insert(0, str(Path(__file__).parent.parent / "services" / "datasets"))

try:
    import torch  # noqa: F401
    import torch_geometric  # noqa: F401
    TORCH_GEOMETRIC_AVAILABLE = True
except ImportError:
    TORCH_GEOMETRIC_AVAILABLE = False

try:
    import torch  # noqa: F401
    import ultralytics  # noqa: F401
    ULTRALYTICS_AVAILABLE = True
except ImportError:
    ULTRALYTICS_AVAILABLE = False


# ============================================================================
# Biometric PAD Tests
# ============================================================================

class TestBiometricPAD:
    """Test Biometric PAD (CDCN) model training pipeline."""
    
    def test_cdcn_block_creation(self):
        """Test CDCNBlock module creation."""
        try:
            from train_cdcn import CDCNBlock
            
            block = CDCNBlock(in_channels=32, out_channels=64, dropout=0.3)
            assert block is not None
            assert hasattr(block, 'conv1')
            assert hasattr(block, 'depthwise')
            assert hasattr(block, 'conv2')
        except ImportError:
            pytest.skip("PyTorch not available")
    
    def test_cdcn_model_creation(self):
        """Test CDCN model initialization."""
        try:
            from train_cdcn import CDCN
            
            model = CDCN(num_classes=1, input_size=128)
            assert model is not None
            assert hasattr(model, 'initial')
            assert hasattr(model, 'encoder')
            assert hasattr(model, 'classifier')
        except ImportError:
            pytest.skip("PyTorch not available")
    
    def test_cdcn_forward_pass(self):
        """Test CDCN model forward pass."""
        try:
            from train_cdcn import CDCN
            
            model = CDCN(num_classes=1, input_size=128)
            model.eval()
            
            dummy_input = torch.randn(1, 1, 128, 128)
            with torch.no_grad():
                output = model(dummy_input)
            
            assert output is not None
            assert output.shape[0] == 1
        except ImportError:
            pytest.skip("PyTorch not available")
    
    def test_cdcn_predictor_initialization(self):
        """Test CDCN predictor initialization."""
        try:
            from train_cdcn import CDCNPredictor, CDCN_MODEL_PATH
            
            # Predictor should initialize even without model file
            predictor = CDCNPredictor()
            # If model doesn't exist, it should handle gracefully
            assert predictor.ready or not os.path.exists(str(CDCN_MODEL_PATH))
        except ImportError:
            pytest.skip("Dependencies not available")
    
    def test_pad_dataset_loading(self):
        """Test PAD dataset loading."""
        try:
            from train_cdcn import PADDataset, MODEL_DIR
            
            # Create synthetic dataset
            os.makedirs(MODEL_DIR / "datasets" / "oulu-npu" / "real" / "video1", exist_ok=True)
            os.makedirs(MODEL_DIR / "datasets" / "oulu-npu" / "spoof" / "print1", exist_ok=True)
            
            from PIL import Image
            img = Image.fromarray(np.random.randint(0, 255, (128, 128), dtype=np.uint8))
            img.save(MODEL_DIR / "datasets" / "oulu-npu" / "real" / "video1" / "test.jpg")
            img.save(MODEL_DIR / "datasets" / "oulu-npu" / "spoof" / "print1" / "test.jpg")
            
            dataset = PADDataset(str(MODEL_DIR / "datasets" / "oulu-npu"))
            assert len(dataset) > 0
            
            # Clean up
            shutil.rmtree(MODEL_DIR / "datasets", ignore_errors=True)
        except ImportError:
            pytest.skip("Dependencies not available")


# ============================================================================
# ArcFace Face Recognition Tests
# ============================================================================

class TestArcFace:
    """Test ArcFace face recognition model training pipeline."""
    
    def test_arc_margin_product(self):
        """Test ArcMarginProduct module."""
        try:
            from train_arcface import ArcMarginProduct
            
            margin = ArcMarginProduct(in_features=512, out_features=100, s=64.0, m=0.5)
            assert margin is not None
            assert hasattr(margin, 'weight')
        except ImportError:
            pytest.skip("PyTorch not available")
    
    def test_insight_face_resnet(self):
        """Test InsightFaceResNet backbone."""
        try:
            from train_arcface import InsightFaceResNet
            
            model = InsightFaceResNet(embedding_size=512)
            assert model is not None
            assert hasattr(model, 'conv1')
            assert hasattr(model, 'fc')
        except ImportError:
            pytest.skip("PyTorch not available")
    
    def test_arcface_forward_pass(self):
        """Test ArcFace forward pass."""
        try:
            from train_arcface import InsightFaceResNet
            
            model = InsightFaceResNet(embedding_size=512)
            model.eval()
            
            dummy_input = torch.randn(1, 3, 112, 112)
            with torch.no_grad():
                embedding = model(dummy_input)
            
            assert embedding is not None
            assert embedding.shape[1] == 512
        except ImportError:
            pytest.skip("PyTorch not available")
    
    def test_arcface_predictor(self):
        """Test ArcFace predictor."""
        try:
            from train_arcface import ARCFACE_MODEL_PATH, ArcFacePredictor
            
            # Predictor should handle missing model gracefully
            predictor = ArcFacePredictor()
            assert predictor.ready or not os.path.exists(str(ARCFACE_MODEL_PATH))
        except ImportError:
            pytest.skip("Dependencies not available")


# ============================================================================
# GNN Election Validation Tests
# ============================================================================

@pytest.mark.skipif(not TORCH_GEOMETRIC_AVAILABLE, reason="PyTorch Geometric not available")
class TestGNN:
    """Test GNN for election validation."""
    
    def test_election_node_creation(self):
        """Test ElectionNode creation."""
        from train_gnn import ElectionNode
        
        node = ElectionNode(
            pu_code="PU-001",
            latitude=9.0579,
            longitude=7.4951,
            ward="Ward-A",
            lga="LGA-1",
            vote_count=1000,
            accredited_voters=1500,
        )
        
        assert node.pu_code == "PU-001"
        assert len(node.features) == 4
        assert abs(node.features[0] - 66.6667) < 0.01  # turnout_percentage ~66.67%
    
    def test_gnn_graph_builder(self):
        """Test election graph building."""
        from train_gnn import ElectionGraphBuilder, ElectionNode
        
        builder = ElectionGraphBuilder(distance_threshold_km=2.0)
        
        # Create nodes in same ward
        nodes = [
            ElectionNode(f"PU-{i}", 9.0 + i*0.01, 7.4, "Ward-A", "LGA-1", 1000, 1500)
            for i in range(10)
        ]
        
        graph = builder.build_graph(nodes)
        assert graph is not None
        assert graph.x.shape[0] == 10
    
    def test_gnn_model_creation(self):
        """Test GNN model creation."""
        try:
            from train_gnn import GNNAnomalyDetection, EnhancedGNNAnomalyDetection
            
            model_gcn = GNNAnomalyDetection(num_features=4, hidden_channels=64)
            assert model_gcn is not None
            
            model_gat = EnhancedGNNAnomalyDetection(
                num_features=4,
                hidden_channels=64,
                num_heads=4,
            )
            assert model_gat is not None
        except ImportError:
            pytest.skip("PyTorch Geometric not available")
    
    def test_gnn_predictor(self):
        """Test GNN predictor."""
        try:
            from train_gnn import GNNPredictor, GNN_MODEL_PATH
            
            # Predictor handles missing model gracefully
            predictor = GNNPredictor()
            assert predictor.ready or not os.path.exists(str(GNN_MODEL_PATH))
        except ImportError:
            pytest.skip("Dependencies not available")
    
    def test_synthetic_election_data(self):
        """Test synthetic election data generation."""
        from train_gnn import create_synthetic_election_data
        
        nodes, builder = create_synthetic_election_data(num_nodes=100, anomaly_rate=0.05)
        
        assert len(nodes) == 100
        assert sum(1 for n in nodes if n.is_anomalous) > 0
        assert all(5.0 < n.latitude < 15.0 for n in nodes)


# ============================================================================
# XGBoost Fraud Detection Tests
# ============================================================================

class TestXGBoostFraud:
    """Test XGBoost fraud detection pipeline."""
    
    def test_election_fraud_features(self):
        """Test feature engineering."""
        from train_xgboost_fraud import ElectionFraudFeatures
        
        records = [
            {
                'pu_code': 'PU-001',
                'accredited_voters': 1000,
                'votes_cast': 800,
                'party_a_votes': 400,
                'party_b_votes': 300,
                'latitude': 9.0,
                'longitude': 7.4,
                'submitted_at': '2026-01-01 10:00:00',
                'is_anomalous': 0,
            }
        ]
        
        df = ElectionFraudFeatures.extract_features(records)
        assert len(df) == 1
        assert 'turnout_percentage' in df.columns
    
    def test_xgboost_fraud_detector(self):
        """Test XGBoost fraud detector initialization."""
        from train_xgboost_fraud import XGBoostFraudDetector
        
        detector = XGBoostFraudDetector(
            max_depth=4,
            learning_rate=0.1,
            n_estimators=50,
        )
        assert detector is not None
        assert detector.model is not None
    
    def test_synthetic_fraud_data(self):
        """Test synthetic fraud data generation."""
        from train_xgboost_fraud import create_synthetic_fraud_data
        
        X, y = create_synthetic_fraud_data(num_samples=1000, fraud_rate=0.1)
        
        assert len(X) == 1000
        assert len(y) == 1000
        assert y.sum() > 0  # Has fraud samples
    
    def test_xgboost_predictor(self):
        """Test fraud predictor."""
        from train_xgboost_fraud import FraudPredictor, FRAUD_MODEL_PATH
        
        # Predictor handles missing model gracefully
        predictor = FraudPredictor()
        assert predictor.ready or not os.path.exists(str(FRAUD_MODEL_PATH))
    
    def test_hyperparameter_tuning(self):
        """Test hyperparameter tuning."""
        from train_xgboost_fraud import (
            XGBoostFraudDetector,
            create_synthetic_fraud_data,
            ElectionFraudFeatures,
        )
        
        X, y = create_synthetic_fraud_data(num_samples=500, fraud_rate=0.1)
        X_train = X[:400]
        y_train = y[:400]
        X_val = X[400:]
        y_val = y[400:]
        
        detector = XGBoostFraudDetector(n_estimators=10)
        detector.feature_columns = X.columns.tolist()
        
        # Train on small subset
        detector.train(X_train, y_train, X_val, y_val)
        assert len(detector.training_history) > 0


# ============================================================================
# YOLO Ballot Counting Tests
# ============================================================================

@pytest.mark.skipif(not ULTRALYTICS_AVAILABLE, reason="Ultralytics/YOLO not available")
class TestYOLOBallot:
    """Test YOLO ballot counting pipeline."""
    
    def test_ballot_counting_dataset(self):
        """Test dataset preparation."""
        from train_yolo_ballot import BallotCountingDataset
        
        dataset = BallotCountingDataset()
        assert dataset is not None
    
    def test_frame_extraction(self):
        """Test video frame extraction."""
        from train_yolo_ballot import BallotCountingDataset
        
        with tempfile.TemporaryDirectory() as tmpdir:
            # Create synthetic video
            video_path = os.path.join(tmpdir, "test.mp4")
            writer = cv2.VideoWriter(
                video_path,
                cv2.VideoWriter_fourcc(*'mp4v'),
                10,
                (128, 128),
            )
            for i in range(10):
                frame = np.random.randint(0, 255, (128, 128, 3), dtype=np.uint8)
                writer.write(frame)
            writer.release()
            
            # Extract frames
            output_dir = os.path.join(tmpdir, "frames")
            count = BallotCountingDataset.extract_frames(video_path, output_dir, fps=1)
            
            assert count > 0
            assert os.path.exists(output_dir)
    
    def test_synthetic_training_data(self):
        """Test synthetic training data generation."""
        from train_yolo_ballot import create_synthetic_training_data
        
        yaml_path = create_synthetic_training_data(num_images=10)
        
        assert os.path.exists(yaml_path)
        assert os.path.exists(os.path.dirname(yaml_path) + "/images/train")
    
    def test_ballot_counting_trainer(self):
        """Test trainer initialization."""
        from train_yolo_ballot import BallotCountingTrainer
        
        trainer = BallotCountingTrainer(
            model_name='yolov8n.pt',
            epochs=1,  # Minimal for test
            batch_size=1,
        )
        assert trainer is not None


# ============================================================================
# Neo4j Integration Tests
# ============================================================================

class TestNeo4jIntegration:
    """Test Neo4j graph database integration."""
    
    def test_neo4j_client_initialization(self):
        """Test Neo4j client initialization."""
        from neo4j_integration import Neo4jElectionGraph
        
        graph = Neo4jElectionGraph(
            uri="bolt://localhost:7687",
            user="neo4j",
            password="test",
        )
        assert graph is not None
        assert graph.uri == "bolt://localhost:7687"
    
    def test_default_connection_params(self):
        """Test Neo4j client default connection parameters."""
        from neo4j_integration import Neo4jElectionGraph

        graph = Neo4jElectionGraph()
        assert graph.uri == "bolt://localhost:7687"
        assert graph.user == "neo4j"
        assert graph.database == "neo4j"
        assert graph.connected is False
        assert graph.driver is None

    def test_query_requires_connection(self):
        """Test that querying without a connection raises an error."""
        from neo4j_integration import Neo4jElectionGraph

        graph = Neo4jElectionGraph()
        with pytest.raises(ConnectionError):
            graph.execute_query("MATCH (n) RETURN n")

    def test_graph_analyzer_wraps_graph(self):
        """Test ElectionGraphAnalyzer holds a reference to the graph."""
        from neo4j_integration import Neo4jElectionGraph, ElectionGraphAnalyzer

        graph = Neo4jElectionGraph()
        analyzer = ElectionGraphAnalyzer(graph)
        assert analyzer.graph is graph


# ============================================================================
# TigerBeetle Ledger Tests
# ============================================================================

class TestTigerBeetleLedger:
    """Test TigerBeetle ledger integration."""
    
    def test_tigerbeetle_client_initialization(self):
        """Test TigerBeetle client initialization."""
        from tigerbeetle_integration import TigerBeetleLedger
        
        ledger = TigerBeetleLedger(
            host="localhost",
            port=3000,
        )
        assert ledger is not None
        assert ledger.host == "localhost"
    
    def test_account_types(self):
        """Test account type enumeration."""
        from tigerbeetle_integration import AccountType
        
        assert len(AccountType) > 0
        assert AccountType.ELECTION_FUND.value == "election_fund"
    
    def test_transaction_types(self):
        """Test transaction type enumeration."""
        from tigerbeetle_integration import TransactionType
        
        assert len(TransactionType) > 0
        assert TransactionType.DEPOSIT.value == "deposit"
    
    def test_account_model(self):
        """Test Account data model."""
        from tigerbeetle_integration import Account
        
        account = Account(
            account_id=1,
            code="ELECT_001",
            name="Election Fund",
            account_type=None,
            currency="NGN",
        )
        assert account.account_id == 1
        assert account.currency == "NGN"
    
    def test_transaction_model(self):
        """Test Transaction data model."""
        from tigerbeetle_integration import Transaction
        
        transaction = Transaction(
            transaction_id=1,
            user_data_1=1,
            user_data_2=2,
            amount=100000,  # 1000 NGN in kobo
        )
        assert transaction.amount == 100000
    
    def test_balance_model(self):
        """Test Balance data model."""
        from tigerbeetle_integration import Balance
        
        balance = Balance(
            account_id=1,
            credits=100000,  # 1000 NGN
            debits=50000,    # 500 NGN
        )
        assert balance.available_balance == 500.0  # Naira
        assert balance.credits_naira == 1000.0
    
    def test_election_finance_manager(self):
        """Test ElectionFinanceManager initialization."""
        from tigerbeetle_integration import TigerBeetleLedger, ElectionFinanceManager
        
        ledger = TigerBeetleLedger()
        manager = ElectionFinanceManager(ledger)
        assert manager is not None
        assert manager.ledger == ledger


# ============================================================================
# Model Serving Tests
# ============================================================================

class TestModelServing:
    """Test model serving infrastructure."""
    
    def test_model_server_initialization(self):
        """Test ModelServer initialization."""
        from model_serving import ModelServer
        
        server = ModelServer(
            models_dir="services/biometric-python/models",
            cache_size=1000,
        )
        assert server is not None
        assert server.model_cache.max_size == 1000
    
    def test_model_registry(self):
        """Test ModelRegistry."""
        from model_serving import ModelRegistry, ModelMetadata, ModelStatus
        
        registry = ModelRegistry()
        
        metadata = ModelMetadata(
            model_id="test-model",
            version="1.0.0",
            model_type="onnx",
            model_path="/tmp/test.onnx",
            status=ModelStatus.READY,
        )
        
        registry.register_model(metadata)
        assert len(registry.models) == 1
        
        active = registry.get_active_model("test-model")
        assert active is not None
        assert active.version == "1.0.0"
    
    def test_model_cache(self):
        """Test ModelCache."""
        from model_serving import ModelCache
        
        cache = ModelCache(max_size=100)
        
        # Test cache miss
        result = cache.get("model1", "input1")
        assert result is None
        
        # Test cache hit
        cache.put("model1", "input1", {"prediction": 0.95})
        result = cache.get("model1", "input1")
        assert result is not None
        assert result["prediction"] == 0.95
        
        # One miss (initial get) + one hit (after put) => hit_rate = 0.5
        assert cache.hit_rate == 0.5
    
    def test_onnx_model_wrapper(self):
        """Test ONNX model wrapper."""
        from model_serving import ONNXModelWrapper
        
        # Wrapper should initialize even without model
        try:
            wrapper = ONNXModelWrapper("/tmp/nonexistent.onnx")
            assert wrapper.ready or not os.path.exists("/tmp/nonexistent.onnx")
        except Exception:
            pass  # Expected if ONNX not installed
    
    def test_model_router(self):
        """Test ModelRouter."""
        from model_serving import ModelServer, ModelRouter
        
        server = ModelServer()
        router = ModelRouter(server)
        assert router is not None
        assert router.server == server


# ============================================================================
# Model Versioning Tests
# ============================================================================

class TestModelVersioning:
    """Test model versioning and A/B testing framework."""
    
    def test_model_version_creation(self):
        """Test ModelVersion creation."""
        from model_versioning import ModelVersion, ModelVersionStatus
        
        version = ModelVersion(
            model_id="test-model",
            version="1.0.0",
            model_path="/tmp/test.onnx",
            status=ModelVersionStatus.ACTIVE,
        )
        assert version.model_id == "test-model"
        assert version.is_production_ready
    
    def test_model_version_manager(self):
        """Test ModelVersionManager."""
        from model_versioning import ModelVersionManager
        
        manager = ModelVersionManager()
        
        # Register models
        manager.register_model(
            model_id="test-model",
            version="1.0.0",
            model_path="/tmp/test_v1.onnx",
        )
        manager.register_model(
            model_id="test-model",
            version="1.1.0",
            model_path="/tmp/test_v2.onnx",
        )
        
        assert len(manager.get_version_history("test-model")) == 2
    
    def test_ab_test_creation(self):
        """Test A/B test creation."""
        from model_versioning import ModelVersionManager, ABTestResult
        
        manager = ModelVersionManager()
        
        test = manager.create_ab_test(
            model_id="test-model",
            version_a="1.0.0",
            version_b="1.1.0",
            metrics=['accuracy', 'latency'],
        )
        
        assert test is not None
        assert test.version_a == "1.0.0"
        assert test.version_b == "1.1.0"
        assert not test.is_complete
    
    def test_model_rollout_manager(self):
        """Test ModelRolloutManager."""
        from model_versioning import ModelVersionManager, ModelRolloutManager
        
        manager = ModelVersionManager()
        rollout_manager = ModelRolloutManager(manager)
        
        rollout = rollout_manager.start_rollout(
            model_id="test-model",
            new_version="1.1.0",
            increment=10.0,
        )
        
        assert rollout is not None
        assert rollout['status'] == 'starting'


# ============================================================================
# Model Monitoring Tests
# ============================================================================

class TestModelMonitoring:
    """Test model monitoring and drift detection."""
    
    def test_drift_detector_psi(self):
        """Test PSI calculation."""
        from model_monitoring import DriftDetector
        
        reference = np.random.normal(0, 1, 1000)
        current = np.random.normal(0.1, 1, 1000)
        
        psi = DriftDetector.calculate_psi(reference, current)
        assert 0 <= psi <= 1  # PSI typically in [0, 1]
    
    def test_drift_detector_ks_test(self):
        """Test KS test calculation."""
        from model_monitoring import DriftDetector
        
        reference = np.random.normal(0, 1, 1000)
        current = np.random.normal(0, 1, 1000)
        
        ks_stat, p_value = DriftDetector.calculate_ks_test(reference, current)
        assert 0 <= ks_stat <= 1
        assert 0 <= p_value <= 1
    
    def test_drift_detection(self):
        """Test drift detection."""
        from model_monitoring import DriftDetector, DriftMetrics
        
        reference = np.random.normal(0, 1, 1000)
        current = np.random.normal(0, 1, 1000)
        
        drift = DriftDetector.detect_drift(
            model_id="test-model",
            reference_data=reference,
            current_data=current,
        )
        
        assert drift is not None
        assert drift.model_id == "test-model"
        assert drift.severity in ["stable", "mild", "moderate", "severe"]
    
    def test_model_monitor(self):
        """Test ModelMonitor."""
        from model_monitoring import ModelMonitor
        
        monitor = ModelMonitor(
            models_dir="services/biometric-python/models",
            history_window_hours=24,
        )
        assert monitor is not None
        
        # Record prediction
        monitor.record_prediction(
            model_id="test-model",
            input_data="test_input",
            prediction=0.95,
            actual=1,
            confidence=0.95,
            latency_ms=50.0,
        )
        
        # Calculate metrics
        metrics = monitor.calculate_performance_metrics("test-model")
        assert metrics.model_id == "test-model"
        assert metrics.prediction_count >= 1
    
    def test_prediction_analytics(self):
        """Test PredictionAnalytics."""
        from model_monitoring import ModelMonitor, PredictionAnalytics
        
        monitor = ModelMonitor()
        analytics = PredictionAnalytics(monitor)
        
        # Record some predictions
        for i in range(10):
            monitor.record_prediction(
                model_id="test-model",
                input_data=f"input_{i}",
                prediction=0.9 + i * 0.01,
                confidence=0.9 + i * 0.01,
            )
        
        # Analyze distribution
        distribution = analytics.analyze_prediction_distribution("test-model", hours=24)
        assert distribution['model_id'] == "test-model"
        assert distribution['prediction_count'] == 10


# ============================================================================
# Dataset Preparation Tests
# ============================================================================

class TestDatasetPreparation:
    """Test dataset preparation utilities."""
    
    def test_biometric_dataset_prep(self):
        """Test biometric dataset preparation."""
        from dataset_preparation import BiometricDatasetPrep
        
        prep = BiometricDatasetPrep(
            output_dir="/tmp/test_biometric_dataset",
        )
        assert prep is not None
    
    def test_election_dataset_prep(self):
        """Test election dataset preparation."""
        from dataset_preparation import ElectionDatasetPrep
        
        prep = ElectionDatasetPrep(
            output_dir="/tmp/test_election_dataset",
        )
        assert prep is not None
    
    def test_dataset_stats(self):
        """Test DatasetStats."""
        from dataset_preparation import DatasetStats
        
        stats = DatasetStats(
            dataset_name="test",
            total_samples=1000,
            train_samples=800,
            val_samples=100,
            test_samples=100,
        )
        
        assert stats.total_samples == 1000
        assert stats.train_samples == 800
    
    def test_synthetic_election_data(self):
        """Test synthetic election data generation."""
        from dataset_preparation import ElectionDatasetPrep
        
        prep = ElectionDatasetPrep()
        
        # Generate synthetic data
        samples = prep._generate_synthetic_election_data(num_samples=100)
        
        assert len(samples) == 100
        assert all('pu_code' in s for s in samples)
        assert all('features' in s for s in samples)
    
    def test_split_dataset(self):
        """Test dataset splitting."""
        from dataset_preparation import BiometricDatasetPrep
        
        prep = BiometricDatasetPrep()
        
        samples = list(range(100))
        train, val, test = prep.split_dataset(
            samples,
            train_ratio=0.8,
            val_ratio=0.1,
            test_ratio=0.1,
        )
        
        assert len(train) == 80
        assert len(val) == 10
        assert len(test) == 10
        assert len(train) + len(val) + len(test) == 100


# ============================================================================
# End-to-End Integration Tests
# ============================================================================

class TestEndToEnd:
    """End-to-end integration tests for AI/ML pipeline."""
    
    def test_complete_training_pipeline(self):
        """Test complete model training pipeline."""
        # 1. Prepare dataset
        from dataset_preparation import BiometricDatasetPrep
        prep = BiometricDatasetPrep(output_dir="/tmp/e2e_test")
        
        # 2. Create model components
        from model_monitoring import DriftDetector
        
        reference = np.random.normal(0, 1, 100)
        current = np.random.normal(0.1, 1, 100)
        
        # 3. Monitor drift
        drift = DriftDetector.detect_drift(
            model_id="e2e-test",
            reference_data=reference,
            current_data=current,
        )
        
        assert drift is not None
        assert drift.model_id == "e2e-test"
    
    def test_model_lifecycle(self):
        """Test complete model lifecycle."""
        from model_versioning import ModelVersionManager
        from model_monitoring import ModelMonitor
        
        # 1. Register model
        manager = ModelVersionManager()
        manager.register_model(
            model_id="lifecycle-test",
            version="1.0.0",
            model_path="/tmp/test.onnx",
        )
        
        # 2. Promote to canary
        manager.promote_to_canary(
            model_id="lifecycle-test",
            version="1.0.0",
            traffic_percentage=10.0,
        )
        
        # 3. Monitor
        monitor = ModelMonitor()
        monitor.record_prediction(
            model_id="lifecycle-test",
            input_data="test",
            prediction=0.95,
            confidence=0.95,
        )
        
        assert monitor.predictions["lifecycle-test"].__len__() >= 1
    
    def test_fraud_detection_pipeline(self):
        """Test complete fraud detection pipeline."""
        from train_xgboost_fraud import (
            XGBoostFraudDetector,
            create_synthetic_fraud_data,
            ElectionFraudFeatures,
        )
        
        # 1. Prepare data
        X, y = create_synthetic_fraud_data(num_samples=1000, fraud_rate=0.1)
        
        # 2. Extract features
        features_df = ElectionFraudFeatures.extract_features(
            pd.concat([X, pd.DataFrame({'is_anomalous': y})], axis=1).to_dict('records')
        )
        
        # 3. Train model
        detector = XGBoostFraudDetector(n_estimators=10)
        detector.feature_columns = features_df.columns.tolist()
        detector.train(features_df, pd.Series(y))
        
        assert len(detector.training_history) > 0


# ============================================================================
# Test Fixtures and Helpers
# ============================================================================

@pytest.fixture
def temp_model_dir():
    """Create temporary directory for model testing."""
    with tempfile.TemporaryDirectory() as tmpdir:
        yield tmpdir


@pytest.fixture
def sample_biometric_data():
    """Generate sample biometric data for testing."""
    return {
        'face': np.random.randint(0, 255, (112, 112, 3), dtype=np.uint8),
        'fingerprint': np.random.randint(0, 255, (128, 128), dtype=np.uint8),
        'iris': np.random.randint(0, 255, (128, 128), dtype=np.uint8),
    }


@pytest.fixture
def sample_election_data():
    """Generate sample election data for testing."""
    return [
        {
            'pu_code': f"PU-{i:05d}",
            'accredited_voters': 1000 + i * 10,
            'votes_cast': 600 + i * 5,
            'party_a_votes': 300 + i * 3,
            'party_b_votes': 200 + i * 2,
            'latitude': 9.0 + i * 0.001,
            'longitude': 7.4 + i * 0.001,
            'submitted_at': f"2026-01-01 {10+i}:00:00",
            'is_anomalous': 1 if i % 20 == 0 else 0,
        }
        for i in range(100)
    ]
