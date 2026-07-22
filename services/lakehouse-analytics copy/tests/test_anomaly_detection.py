"""Integration tests for the lakehouse analytics anomaly detection system.

These tests verify that the Isolation Forest model:
1. Persists to disk and loads correctly
2. Produces deterministic results
3. Detects actual anomalies (not random scores)
4. Handles edge cases properly
"""

import os
import sys
import tempfile
from pathlib import Path

import numpy as np

# Add parent directory to path
sys.path.insert(0, str(Path(__file__).parent.parent))

from main import AnomalyDetector, MODEL_PATH


class TestModelPersistence:
    """Test that models are properly persisted and loaded."""

    def setup_method(self):
        """Create a temporary directory for test models."""
        self.test_dir = tempfile.mkdtemp()
        # Patch the MODEL_PATH to use our temp directory
        self.original_path = MODEL_PATH
        self.test_model_path = os.path.join(self.test_dir, "anomaly_detector.joblib")
        # We need to patch at the module level
        import main as main_module
        self.original_model_path = main_module.MODEL_PATH
        main_module.MODEL_PATH = self.test_model_path

    def teardown_method(self):
        """Restore original MODEL_PATH and clean up."""
        import main as main_module
        main_module.MODEL_PATH = self.original_model_path
        # Clean up temp directory
        import shutil
        shutil.rmtree(self.test_dir, ignore_errors=True)

    def test_train_and_persist_model(self):
        """Verify model trains and persists to disk."""
        detector = AnomalyDetector()
        # Train with synthetic data
        data = np.random.randn(100, 1) * 100 + 500
        result = detector.train_model(data)
        assert result is True, "Training should succeed"
        assert os.path.exists(self.test_model_path), "Model file should exist"

    def test_load_persisted_model(self):
        """Verify persisted model can be loaded."""
        detector = AnomalyDetector()
        # First train
        data = np.random.randn(100, 1) * 100 + 500
        detector.train_model(data)
        
        # Create a new detector instance - should load from disk
        detector2 = AnomalyDetector()
        assert detector2.model is not None, "Model should be loaded from disk"
        assert detector2._training_samples == 100, "Training samples should be preserved"
        assert detector2._trained_at is not None, "Training timestamp should be preserved"

    def test_model_metadata_stored_with_model(self):
        """Verify training metadata is stored alongside the model."""
        import joblib
        detector = AnomalyDetector()
        data = np.random.randn(50, 1) * 50 + 250
        detector.train_model(data)
        
        # Load the raw file and check structure
        stored_data = joblib.load(self.test_model_path)
        assert isinstance(stored_data, dict), "Should store dict with model + metadata"
        assert "model" in stored_data, "Should contain 'model' key"
        assert "metadata" in stored_data, "Should contain 'metadata' key"
        meta = stored_data["metadata"]
        assert "trained_at" in meta, "Should store trained_at timestamp"
        assert "training_samples" in meta, "Should store training_samples"
        assert meta["training_samples"] == 50

    def test_legacy_model_load(self):
        """Verify backward compatibility with legacy model format."""
        import joblib
        # Write a legacy format file (model directly, not wrapped in dict)
        detector = AnomalyDetector()
        data = np.random.randn(50, 1)
        detector.train_model(data)
        
        # Simulate legacy format
        joblib.dump(detector.model, self.test_model_path)
        
        # New detector should still load it
        detector2 = AnomalyDetector()
        assert detector2.model is not None, "Should load legacy format"


class TestAnomalyDetection:
    """Test that anomaly detection produces real, deterministic results."""

    def setup_method(self):
        """Set up detector with test model."""
        self.test_dir = tempfile.mkdtemp()
        import main as main_module
        self.original_path = main_module.MODEL_PATH
        main_module.MODEL_PATH = os.path.join(self.test_dir, "test_model.joblib")
        self.detector = AnomalyDetector()

    def teardown_method(self):
        """Clean up."""
        import main as main_module
        main_module.MODEL_PATH = self.original_path
        import shutil
        shutil.rmtree(self.test_dir, ignore_errors=True)

    def test_detects_clear_anomaly(self):
        """Verify a clear outlier is detected as anomalous."""
        # Train on normal data (vote counts around 500)
        train_data = np.random.randn(100, 1) * 50 + 500
        self.detector.train_model(train_data)
        
        # Test with a clear outlier (5000 votes when normal is ~500)
        results = self.detector.detect_anomalies([500, 510, 490, 505, 495, 500, 500, 500, 500, 500, 5000])
        
        assert len(results) > 0, "Should detect the outlier"
        # The outlier should have high confidence
        outlier = [r for r in results if r.confidence > 0.5]
        assert len(outlier) > 0, "Outlier should have high confidence"

    def test_no_false_anomalies_normal_data(self):
        """Verify normal data produces no anomalies."""
        train_data = np.random.randn(100, 1) * 50 + 500
        self.detector.train_model(train_data)
        
        # All values within normal range
        results = self.detector.detect_anomalies([490, 500, 510, 495, 505, 500, 498, 502, 501, 499])
        
        assert len(results) == 0, "Normal data should not produce anomalies"

    def test_deterministic_results(self):
        """Verify same input produces same output."""
        data = np.random.RandomState(42).randn(100, 1) * 50 + 500
        self.detector.train_model(data)
        
        test_data = [500, 510, 490, 505, 495, 500, 500, 500, 500, 500, 5000]
        results1 = self.detector.detect_anomalies(test_data)
        results2 = self.detector.detect_anomalies(test_data)
        
        assert len(results1) == len(results2), "Should produce same number of results"
        assert results1[0].confidence == results2[0].confidence, "Confidence should match"

    def test_small_data_returns_no_results(self):
        """Verify less than 10 data points returns no anomalies."""
        self.detector.model = None  # Reset model
        results = self.detector.detect_anomalies([100, 200, 300])
        assert len(results) == 0, "Small data should return no anomalies"

    def test_score_range_is_valid(self):
        """Verify all confidence scores are in [0, 1] range."""
        train_data = np.random.randn(100, 1) * 50 + 500
        self.detector.train_model(train_data)
        
        test_data = [500, 510, 490, 505, 495, 500, 500, 500, 500, 500, 5000]
        results = self.detector.detect_anomalies(test_data)
        
        for result in results:
            assert 0.0 <= result.confidence <= 1.0, f"Score {result.confidence} out of range"


class TestBenfordsLaw:
    """Test Benford's Law implementation."""

    def test_benfords_analysis(self):
        """Verify Benford's law analysis produces valid results."""
        # Import the function directly
        from main import detector
        
        # Data that should roughly follow Benford's law
        votes = [
            100, 120, 150, 180, 200, 210, 230, 250, 280, 300,
            350, 400, 450, 500, 550, 600, 650, 700, 750, 800,
            850, 900, 950, 1000, 1050, 1100, 1200, 1300, 1400, 1500
        ]
        
        result = detector.benford_analysis(votes)
        
        assert result is not None
        assert hasattr(result, 'digit'), "Result should have digit field"
        assert hasattr(result, 'expected'), "Result should have expected field"
        assert hasattr(result, 'observed'), "Result should have observed field"

    def test_benfords_hardcoded_data(self):
        """Test with data that strictly follows Benford's distribution."""
        from main import detector
        
        # Generate data following Benford's law
        np.random.seed(42)
        first_digits = np.random.choice(
            [1, 2, 3, 4, 5, 6, 7, 8, 9],
            p=[0.301, 0.176, 0.125, 0.097, 0.079, 0.067, 0.058, 0.051, 0.046],
            size=1000
        )
        votes = [d * 100 + np.random.randint(0, 99) for d in first_digits]
        
        result = detector.benford_analysis(votes)
        assert result is not None


class TestIntegrityScore:
    """Test election integrity scoring."""

    def test_integrity_score_computation(self):
        """Verify integrity score is computed from real data."""
        from main import detector
        
        votes = list(range(100, 10100, 100))  # 100 values
        
        benford = detector.benford_analysis(votes)
        anomalies = detector.detect_anomalies(votes)
        
        result = detector.integrity_score(votes, benford, len(anomalies))
        
        assert result is not None
        assert hasattr(result, 'score'), "Result should have score"
        assert 0 <= result.score <= 100, "Score should be in [0, 100] range"
