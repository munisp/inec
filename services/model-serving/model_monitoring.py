"""Model Monitoring and Drift Detection.

Implements comprehensive model monitoring including:
- Performance tracking and alerting
- Data drift detection (PSI, KS test, population stability)
- Concept drift detection
- Prediction distribution monitoring
- Latency and throughput monitoring
- Automated model retraining triggers

Usage:
    from model_monitoring import ModelMonitor, DriftDetector
    monitor = ModelMonitor(models_dir="services/biometric-python/models")
    monitor.record_prediction("biometric-cdcn", input_data, prediction, actual)
    drift_report = monitor.detect_drift("biometric-cdcn")
"""

import os
import json
import time
import math
import threading
from pathlib import Path
from typing import Dict, List, Optional, Tuple, Any
from datetime import datetime, timedelta
from dataclasses import dataclass, field
from collections import deque, defaultdict

import numpy as np
from scipy import stats


@dataclass
class PredictionRecord:
    """Record of a single prediction."""
    model_id: str
    input_hash: str
    prediction: Any
    actual: Any = None
    confidence: float = 0.0
    latency_ms: float = 0.0
    timestamp: str = field(default_factory=lambda: datetime.now().isoformat())
    metadata: Dict = field(default_factory=dict)


@dataclass
class DriftMetrics:
    """Metrics for drift detection."""
    model_id: str
    detection_time: str = field(default_factory=lambda: datetime.now().isoformat())
    psi: float = 0.0  # Population Stability Index
    ks_statistic: float = 0.0  # Kolmogorov-Smirnov statistic
    ks_pvalue: float = 0.0
    mean_shift: float = 0.0
    variance_ratio: float = 0.0
    severity: str = "stable"  # stable, mild, moderate, severe
    recommendations: List[str] = field(default_factory=list)


@dataclass
class PerformanceMetrics:
    """Model performance metrics."""
    model_id: str
    accuracy: float = 0.0
    precision: float = 0.0
    recall: float = 0.0
    f1_score: float = 0.0
    auc_roc: float = 0.0
    avg_latency_ms: float = 0.0
    p95_latency_ms: float = 0.0
    p99_latency_ms: float = 0.0
    throughput_per_second: float = 0.0
    prediction_count: int = 0
    calculated_at: str = field(default_factory=lambda: datetime.now().isoformat())


class DriftDetector:
    """Detects data and concept drift in model predictions."""
    
    @staticmethod
    def calculate_psi(
        reference_distribution: np.ndarray,
        current_distribution: np.ndarray,
        buckets: int = 10,
    ) -> float:
        """Calculate Population Stability Index (PSI).
        
        PSI measures the stability of a population over time.
        Values:
        - < 0.1: Stable
        - 0.1 - 0.25: Moderate shift
        - > 0.25: Significant shift
        
        Args:
            reference_distribution: Baseline distribution
            current_distribution: Current distribution
            buckets: Number of quantile buckets
            
        Returns:
            PSI value
        """
        # Create quantile buckets from reference distribution
        percentiles = np.linspace(0, 100, buckets + 1)
        breakpoints = np.percentile(reference_distribution, percentiles)
        breakpoints = np.unique(breakpoints)
        
        # Calculate proportions
        reference_counts, _ = np.histogram(reference_distribution, bins=breakpoints)
        current_counts, _ = np.histogram(current_distribution, bins=breakpoints)
        
        reference_props = reference_counts / len(reference_distribution)
        current_props = current_counts / len(current_distribution)
        
        # Add smoothing to avoid division by zero
        epsilon = 1e-4
        reference_props = np.where(reference_props < epsilon, epsilon, reference_props)
        current_props = np.where(current_props < epsilon, epsilon, current_props)
        
        # Calculate PSI
        psi = np.sum((current_props - reference_props) * np.log(current_props / reference_props))
        
        return float(psi)
    
    @staticmethod
    def calculate_ks_test(
        reference_data: np.ndarray,
        current_data: np.ndarray,
    ) -> Tuple[float, float]:
        """Calculate Kolmogorov-Smirnov test for distribution comparison.
        
        Args:
            reference_data: Reference distribution
            current_data: Current distribution
            
        Returns:
            Tuple of (KS statistic, p-value)
        """
        ks_stat, p_value = stats.ks_2samp(reference_data, current_data)
        return float(ks_stat), float(p_value)
    
    @staticmethod
    def detect_drift(
        model_id: str,
        reference_data: np.ndarray,
        current_data: np.ndarray,
        threshold_psi: float = 0.1,
        threshold_ks: float = 0.05,
    ) -> DriftMetrics:
        """Detect drift between reference and current data.
        
        Args:
            model_id: Model identifier
            reference_data: Baseline data distribution
            current_data: Current data distribution
            threshold_psi: PSI threshold for drift
            threshold_ks: KS test p-value threshold
            
        Returns:
            DriftMetrics with detection results
        """
        # Calculate metrics
        psi = DriftDetector.calculate_psi(reference_data, current_data)
        ks_stat, ks_pvalue = DriftDetector.calculate_ks_test(reference_data, current_data)
        
        # Calculate additional statistics
        mean_shift = abs(np.mean(current_data) - np.mean(reference_data))
        variance_ratio = np.var(current_data) / max(np.var(reference_data), 1e-10)
        
        # Determine severity
        if psi > 0.25 or ks_pvalue < 0.01:
            severity = "severe"
        elif psi > 0.1 or ks_pvalue < threshold_ks:
            severity = "moderate"
        elif psi > 0.05:
            severity = "mild"
        else:
            severity = "stable"
        
        # Generate recommendations
        recommendations = []
        if severity == "severe":
            recommendations.extend([
                "Immediate model review required",
                "Consider retraining with recent data",
                "Investigate data pipeline for anomalies",
            ])
        elif severity == "moderate":
            recommendations.extend([
                "Schedule model retraining",
                "Monitor prediction distributions closely",
            ])
        elif severity == "mild":
            recommendations.append("Continue monitoring")
        
        return DriftMetrics(
            model_id=model_id,
            psi=psi,
            ks_statistic=ks_stat,
            ks_pvalue=ks_pvalue,
            mean_shift=mean_shift,
            variance_ratio=variance_ratio,
            severity=severity,
            recommendations=recommendations,
        )


class ModelMonitor:
    """Monitors model performance and detects drift."""
    
    def __init__(
        self,
        models_dir: str = "services/biometric-python/models",
        history_window_hours: int = 168,  # 1 week
        drift_check_interval_minutes: int = 60,
    ):
        self.models_dir = Path(models_dir)
        self.history_window_hours = history_window_hours
        self.drift_check_interval = drift_check_interval_minutes
        
        # Prediction history per model
        self.predictions: Dict[str, deque] = defaultdict(lambda: deque(maxlen=10000))
        
        # Reference distributions for drift detection
        self.reference_distributions: Dict[str, np.ndarray] = {}
        
        # Performance metrics history
        self.performance_history: Dict[str, List[PerformanceMetrics]] = defaultdict(list)
        
        # Alert system
        self.alerts: List[Dict] = []
        self._lock = threading.RLock()
        
        print(f"✓ Model Monitor initialized")
        print(f"  History window: {history_window_hours} hours")
        print(f"  Drift check interval: {drift_check_interval_minutes} minutes")
    
    def record_prediction(
        self,
        model_id: str,
        input_data: Any,
        prediction: Any,
        actual: Any = None,
        confidence: float = 0.0,
        latency_ms: float = 0.0,
    ):
        """Record a prediction for monitoring."""
        import hashlib
        
        input_hash = hashlib.sha256(str(input_data).encode()).hexdigest()
        
        record = PredictionRecord(
            model_id=model_id,
            input_hash=input_hash,
            prediction=prediction,
            actual=actual,
            confidence=confidence,
            latency_ms=latency_ms,
        )
        
        with self._lock:
            self.predictions[model_id].append(record)
    
    def set_reference_distribution(self, model_id: str, data: np.ndarray):
        """Set the reference distribution for drift detection."""
        with self._lock:
            self.reference_distributions[model_id] = data.copy()
        print(f"✓ Set reference distribution for {model_id}")
    
    def calculate_performance_metrics(self, model_id: str, hours: int = 24) -> PerformanceMetrics:
        """Calculate performance metrics for a model.
        
        Args:
            model_id: Model identifier
            hours: Time window for metrics
            
        Returns:
            PerformanceMetrics with calculated statistics
        """
        cutoff_time = datetime.now() - timedelta(hours=hours)
        
        with self._lock:
            records = [
                r for r in self.predictions.get(model_id, [])
                if datetime.fromisoformat(r.timestamp) > cutoff_time
            ]
        
        if not records:
            return PerformanceMetrics(model_id=model_id)
        
        # Calculate metrics
        latencies = [r.latency_ms for r in records if r.latency_ms > 0]
        confidences = [r.confidence for r in records]
        
        # Accuracy metrics (if actuals available)
        correct_predictions = sum(1 for r in records if r.actual is not None and r.prediction == r.actual)
        accuracy = correct_predictions / len(records) if records else 0.0
        
        metrics = PerformanceMetrics(
            model_id=model_id,
            accuracy=accuracy,
            avg_latency_ms=np.mean(latencies) if latencies else 0.0,
            p95_latency_ms=np.percentile(latencies, 95) if latencies else 0.0,
            p99_latency_ms=np.percentile(latencies, 99) if latencies else 0.0,
            throughput_per_second=len(records) / max(hours * 3600, 1),
            prediction_count=len(records),
        )
        
        with self._lock:
            self.performance_history[model_id].append(metrics)
        
        return metrics
    
    def detect_drift(self, model_id: str, hours: int = 24) -> Optional[DriftMetrics]:
        """Detect drift for a model.
        
        Args:
            model_id: Model identifier
            hours: Time window for drift detection
            
        Returns:
            DriftMetrics or None if insufficient data
        """
        cutoff_time = datetime.now() - timedelta(hours=hours)
        
        with self._lock:
            recent_records = [
                r for r in self.predictions.get(model_id, [])
                if datetime.fromisoformat(r.timestamp) > cutoff_time
            ]
        
        if not recent_records:
            return None
        
        reference_data = self.reference_distributions.get(model_id)
        if reference_data is None:
            # Use older predictions as reference
            older_records = [
                r for r in self.predictions.get(model_id, [])
                if datetime.fromisoformat(r.timestamp) <= cutoff_time
            ]
            if not older_records:
                return None
            reference_data = np.array([r.confidence for r in older_records])
            current_data = np.array([r.confidence for r in recent_records])
        else:
            current_data = np.array([r.confidence for r in recent_records])
        
        return DriftDetector.detect_drift(
            model_id=model_id,
            reference_data=reference_data,
            current_data=current_data,
        )
    
    def check_model_health(self, model_id: str) -> Dict:
        """Check overall health of a model.
        
        Returns:
            Dict with health status and recommendations
        """
        perf_metrics = self.calculate_performance_metrics(model_id)
        drift_metrics = self.detect_drift(model_id)
        
        health_status = "healthy"
        issues = []
        recommendations = []
        
        # Check performance
        if perf_metrics.accuracy < 0.9:
            health_status = "degraded"
            issues.append(f"Low accuracy: {perf_metrics.accuracy:.4f}")
            recommendations.append("Consider retraining model")
        
        if perf_metrics.p99_latency_ms > 1000:
            health_status = "degraded"
            issues.append(f"High P99 latency: {perf_metrics.p99_latency_ms:.0f}ms")
            recommendations.append("Optimize model inference")
        
        # Check drift
        if drift_metrics:
            if drift_metrics.severity == "severe":
                health_status = "critical"
                issues.append(f"Severe drift detected (PSI: {drift_metrics.psi:.4f})")
                recommendations.extend(drift_metrics.recommendations)
            elif drift_metrics.severity == "moderate":
                if health_status == "healthy":
                    health_status = "warning"
                issues.append(f"Moderate drift detected (PSI: {drift_metrics.psi:.4f})")
                recommendations.extend(drift_metrics.recommendations)
        
        # Generate alert if needed
        if health_status in ["degraded", "critical"]:
            alert = {
                'model_id': model_id,
                'timestamp': datetime.now().isoformat(),
                'status': health_status,
                'issues': issues,
                'recommendations': recommendations,
            }
            self.alerts.append(alert)
        
        return {
            'model_id': model_id,
            'health_status': health_status,
            'performance_metrics': {
                'accuracy': perf_metrics.accuracy,
                'avg_latency_ms': perf_metrics.avg_latency_ms,
                'p95_latency_ms': perf_metrics.p95_latency_ms,
                'p99_latency_ms': perf_metrics.p99_latency_ms,
                'throughput_per_second': perf_metrics.throughput_per_second,
                'prediction_count': perf_metrics.prediction_count,
            },
            'drift_metrics': {
                'psi': drift_metrics.psi if drift_metrics else None,
                'severity': drift_metrics.severity if drift_metrics else None,
                'recommendations': drift_metrics.recommendations if drift_metrics else [],
            } if drift_metrics else None,
            'issues': issues,
            'recommendations': recommendations,
        }
    
    def get_monitoring_report(self) -> Dict:
        """Generate comprehensive monitoring report."""
        models = set(r.model_id for records in self.predictions.values() for r in records)
        
        model_reports = {}
        for model_id in models:
            model_reports[model_id] = self.check_model_health(model_id)
        
        return {
            'generated_at': datetime.now().isoformat(),
            'models': model_reports,
            'total_alerts': len(self.alerts),
            'recent_alerts': self.alerts[-10:],  # Last 10 alerts
        }
    
    def trigger_retraining(self, model_id: str, reason: str) -> Dict:
        """Trigger model retraining based on monitoring.
        
        Args:
            model_id: Model identifier
            reason: Reason for retraining
            
        Returns:
            Retraining job configuration
        """
        job = {
            'job_id': f"retrain_{model_id}_{int(time.time())}",
            'model_id': model_id,
            'reason': reason,
            'triggered_at': datetime.now().isoformat(),
            'status': 'queued',
        }
        
        print(f"✓ Triggered retraining for {model_id}")
        print(f"  Reason: {reason}")
        print(f"  Job ID: {job['job_id']}")
        
        return job


class PredictionAnalytics:
    """Analyzes prediction patterns and trends."""
    
    def __init__(self, monitor: ModelMonitor):
        self.monitor = monitor
    
    def analyze_prediction_distribution(self, model_id: str, hours: int = 24) -> Dict:
        """Analyze distribution of predictions.
        
        Returns:
            Dict with distribution analysis
        """
        cutoff_time = datetime.now() - timedelta(hours=hours)
        
        with self.monitor._lock:
            records = [
                r for r in self.monitor.predictions.get(model_id, [])
                if datetime.fromisoformat(r.timestamp) > cutoff_time
            ]
        
        if not records:
            return {'model_id': model_id, 'error': 'No predictions found'}
        
        confidences = [r.confidence for r in records]
        
        # Calculate distribution statistics
        return {
            'model_id': model_id,
            'time_window_hours': hours,
            'prediction_count': len(records),
            'confidence_stats': {
                'mean': float(np.mean(confidences)),
                'median': float(np.median(confidences)),
                'std': float(np.std(confidences)),
                'min': float(np.min(confidences)),
                'max': float(np.max(confidences)),
                'p10': float(np.percentile(confidences, 10)),
                'p50': float(np.percentile(confidences, 50)),
                'p90': float(np.percentile(confidences, 90)),
            },
            'hourly_trend': self._calculate_hourly_trend(records),
        }
    
    def _calculate_hourly_trend(self, records: List[PredictionRecord]) -> List[Dict]:
        """Calculate hourly prediction trends."""
        hourly_counts = defaultdict(int)
        hourly_confidences = defaultdict(list)
        
        for record in records:
            hour = datetime.fromisoformat(record.timestamp).hour
            hourly_counts[hour] += 1
            hourly_confidences[hour].append(record.confidence)
        
        trend = []
        for hour in range(24):
            if hour in hourly_counts:
                trend.append({
                    'hour': hour,
                    'count': hourly_counts[hour],
                    'avg_confidence': float(np.mean(hourly_confidences[hour])),
                })
        
        return trend


def main():
    """Demonstrate model monitoring usage."""
    print("=" * 60)
    print("Model Monitoring and Drift Detection")
    print("=" * 60)
    
    print("\nExample usage:")
    print("""
    from model_monitoring import ModelMonitor, DriftDetector
    
    # Initialize monitor
    monitor = ModelMonitor(
        models_dir="services/biometric-python/models",
        history_window_hours=168,
    )
    
    # Set reference distribution (from training data)
    reference_data = np.array([0.85, 0.92, 0.78, 0.88, 0.91, 0.82])
    monitor.set_reference_distribution("biometric-cdcn", reference_data)
    
    # Record predictions
    monitor.record_prediction(
        model_id="biometric-cdcn",
        input_data=image_data,
        prediction=0.92,
        actual=1,
        confidence=0.92,
        latency_ms=45.5,
    )
    
    # Calculate performance metrics
    metrics = monitor.calculate_performance_metrics("biometric-cdcn")
    print(f"Accuracy: {metrics.accuracy:.4f}")
    print(f"Avg latency: {metrics.avg_latency_ms:.2f}ms")
    
    # Detect drift
    drift = monitor.detect_drift("biometric-cdcn")
    if drift:
        print(f"PSI: {drift.psi:.4f}")
        print(f"Severity: {drift.severity}")
        print(f"Recommendations: {drift.recommendations}")
    
    # Check health
    health = monitor.check_model_health("biometric-cdcn")
    print(f"Health status: {health['health_status']}")
    print(f"Issues: {health['issues']}")
    
    # Get monitoring report
    report = monitor.get_monitoring_report()
    for model_id, model_report in report['models'].items():
        print(f"{model_id}: {model_report['health_status']}")
    """)
    
    print("\n" + "=" * 60)
    print("Model Monitoring Ready")
    print("=" * 60)


if __name__ == "__main__":
    main()
