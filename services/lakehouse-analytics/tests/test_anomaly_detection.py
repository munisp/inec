"""Regression tests for the lakehouse anomaly and integrity analytics contract.

The service evaluates the supplied election batch at request time. It does not
persist a generic model trained on one election for reuse with another election.
Candidates from Isolation Forest must also exceed the robust modified-z threshold
before they are emitted as findings.
"""
import sys
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).parent.parent))
from main import AnomalyDetector, detector


class TestAnomalyDetection:
    """Exercise conservative, deterministic anomaly candidate selection."""

    def test_detects_clear_material_outlier(self):
        values = [500, 510, 490, 505, 495, 500, 500, 500, 500, 500, 5000]
        findings = AnomalyDetector().detect_anomalies(values)

        assert len(findings) == 1
        finding = findings[0]
        assert finding.polling_unit_code == "PU-00010"
        assert finding.anomaly_type == "statistical_outlier"
        assert finding.confidence >= 0.8
        assert "modified-z" in finding.description

    def test_normal_variation_does_not_create_forced_contamination_findings(self):
        values = [490, 500, 510, 495, 505, 500, 498, 502, 501, 499]
        assert AnomalyDetector().detect_anomalies(values) == []

    def test_findings_are_deterministic_for_identical_input(self):
        values = [500, 510, 490, 505, 495, 500, 500, 500, 500, 500, 5000]
        first = AnomalyDetector().detect_anomalies(values)
        second = AnomalyDetector().detect_anomalies(values)

        assert [(item.polling_unit_code, item.confidence, item.severity) for item in first] == [
            (item.polling_unit_code, item.confidence, item.severity) for item in second
        ]

    def test_short_batch_returns_no_finding(self):
        assert AnomalyDetector().detect_anomalies([100, 200, 300]) == []

    def test_confidence_is_bounded(self):
        findings = AnomalyDetector().detect_anomalies(
            [500, 510, 490, 505, 495, 500, 500, 500, 500, 500, 5000]
        )
        assert findings
        assert all(0.0 <= item.confidence <= 1.0 for item in findings)


class TestBenfordsLaw:
    """Exercise the public BenfordAnalysis model rather than historical fields."""

    def test_benford_analysis_returns_all_digit_observations(self):
        votes = [
            100, 120, 150, 180, 200, 210, 230, 250, 280, 300,
            350, 400, 450, 500, 550, 600, 650, 700, 750, 800,
            850, 900, 950, 1000, 1050, 1100, 1200, 1300, 1400, 1500,
        ]
        result = detector.benford_analysis(votes)

        assert result.sample_size == len(votes)
        assert len(result.digits) == 9
        assert result.digits[0].digit == 1
        assert all(0.0 <= item.expected <= 1.0 for item in result.digits)
        assert all(0.0 <= item.observed <= 1.0 for item in result.digits)

    def test_benford_analysis_accepts_a_distribution_with_all_leading_digits(self):
        rng = np.random.RandomState(42)
        first_digits = rng.choice(
            [1, 2, 3, 4, 5, 6, 7, 8, 9],
            p=[0.301, 0.176, 0.125, 0.097, 0.079, 0.067, 0.058, 0.051, 0.046],
            size=1000,
        )
        votes = [int(d) * 100 + int(rng.randint(0, 99)) for d in first_digits]
        result = detector.benford_analysis(votes)

        assert result.sample_size == 1000
        assert result.status in {"pass", "fail"}


class TestIntegrityScore:
    """Exercise the public IntegrityScore schema and numerical bounds."""

    def test_integrity_score_computation(self):
        votes = list(range(100, 10100, 100))
        local_detector = AnomalyDetector()
        benford = local_detector.benford_analysis(votes)
        anomalies = local_detector.detect_anomalies(votes)
        result = local_detector.integrity_score(votes, benford, len(anomalies))

        assert 0 <= result.overall_score <= 100
        assert result.risk_level in {"low", "medium", "high", "critical"}
        assert "anomaly_score" in result.components
