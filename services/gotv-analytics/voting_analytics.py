"""
voting_analytics.py — Vote Analytics, Anomaly Detection, Turnout Prediction,
Delegate Behavior Analysis, Remote Voting Fraud Detection for Party Primaries.

Middleware integration:
- PostgreSQL (Lakehouse queries)
- Redis (caching predictions)
- Kafka (consuming vote events)
- OpenSearch (anomaly indexing)
- Fluvio (live streaming analytics)
- DuckDB/Lakehouse (historical analysis)
"""

import hashlib
import json
import logging
import math
import os
import time
from collections import Counter, defaultdict
from dataclasses import dataclass, field, asdict
from datetime import datetime, timedelta
from typing import Any, Dict, List, Optional, Tuple

import numpy as np

logger = logging.getLogger("voting_analytics")

# ═══════════════════════════════════════════════════════════════════════════
# DATA MODELS
# ═══════════════════════════════════════════════════════════════════════════


@dataclass
class VoteEvent:
    ballot_id: str
    round_id: str
    delegate_id: str
    aspirant_id: str
    vote_type: str  # for, against, abstain, spoiled
    is_remote: bool
    is_decoy: bool = False
    timestamp: float = field(default_factory=time.time)
    ip_hash: str = ""
    device_fingerprint: str = ""


@dataclass
class DelegateProfile:
    delegate_id: str
    state_code: str
    delegate_type: str
    accreditation_time: float = 0
    vote_time: float = 0
    is_remote: bool = False
    device_count: int = 0
    session_count: int = 0


@dataclass
class AnomalyAlert:
    alert_id: str
    alert_type: str
    severity: str  # low, medium, high, critical
    description: str
    evidence: Dict[str, Any] = field(default_factory=dict)
    timestamp: float = field(default_factory=time.time)


@dataclass
class TurnoutPrediction:
    round_id: str
    predicted_turnout_pct: float
    confidence_interval: Tuple[float, float]
    model_type: str
    features_used: List[str] = field(default_factory=list)
    timestamp: float = field(default_factory=time.time)


# ═══════════════════════════════════════════════════════════════════════════
# VOTE ANOMALY DETECTION ENGINE
# ═══════════════════════════════════════════════════════════════════════════


class VoteAnomalyDetector:
    """Detects voting irregularities and potential fraud in real-time."""

    def __init__(self):
        self.vote_events: List[VoteEvent] = []
        self.alerts: List[AnomalyAlert] = []
        self.ip_vote_counts: Dict[str, int] = defaultdict(int)
        self.device_vote_counts: Dict[str, int] = defaultdict(int)
        self.delegate_vote_times: Dict[str, List[float]] = defaultdict(list)
        self.state_vote_rates: Dict[str, List[float]] = defaultdict(list)

    def ingest_vote(self, event: VoteEvent) -> List[AnomalyAlert]:
        """Process a vote event and return any anomalies detected."""
        self.vote_events.append(event)
        alerts = []

        # Check 1: Duplicate vote detection
        dup_alert = self._check_duplicate_vote(event)
        if dup_alert:
            alerts.append(dup_alert)

        # Check 2: IP concentration (multiple votes from same IP)
        if event.is_remote and event.ip_hash:
            self.ip_vote_counts[event.ip_hash] += 1
            if self.ip_vote_counts[event.ip_hash] > 3:
                alerts.append(AnomalyAlert(
                    alert_id=f"ip-conc-{event.ip_hash[:8]}",
                    alert_type="ip_concentration",
                    severity="high",
                    description=f"Multiple votes ({self.ip_vote_counts[event.ip_hash]}) from same IP",
                    evidence={"ip_hash": event.ip_hash, "count": self.ip_vote_counts[event.ip_hash]},
                ))

        # Check 3: Device reuse across delegates
        if event.device_fingerprint:
            self.device_vote_counts[event.device_fingerprint] += 1
            if self.device_vote_counts[event.device_fingerprint] > 1:
                alerts.append(AnomalyAlert(
                    alert_id=f"dev-reuse-{event.device_fingerprint[:8]}",
                    alert_type="device_reuse",
                    severity="critical",
                    description="Same device used by multiple delegates",
                    evidence={"device": event.device_fingerprint[:16]},
                ))

        # Check 4: Voting speed anomaly (too fast)
        self.delegate_vote_times[event.delegate_id].append(event.timestamp)
        speed_alert = self._check_voting_speed(event)
        if speed_alert:
            alerts.append(speed_alert)

        # Check 5: Geographic impossibility (remote voting from unexpected location)
        geo_alert = self._check_geographic_anomaly(event)
        if geo_alert:
            alerts.append(geo_alert)

        self.alerts.extend(alerts)
        return alerts

    def _check_duplicate_vote(self, event: VoteEvent) -> Optional[AnomalyAlert]:
        delegate_votes = [v for v in self.vote_events if v.delegate_id == event.delegate_id
                          and v.round_id == event.round_id and v.ballot_id != event.ballot_id]
        if delegate_votes:
            return AnomalyAlert(
                alert_id=f"dup-{event.delegate_id}-{event.round_id}",
                alert_type="duplicate_vote",
                severity="critical",
                description=f"Delegate {event.delegate_id} voted multiple times in round {event.round_id}",
                evidence={"delegate_id": event.delegate_id, "vote_count": len(delegate_votes) + 1},
            )
        return None

    def _check_voting_speed(self, event: VoteEvent) -> Optional[AnomalyAlert]:
        times = self.delegate_vote_times[event.delegate_id]
        if len(times) >= 2:
            time_diff = times[-1] - times[-2]
            if time_diff < 2.0:  # Less than 2 seconds between votes
                return AnomalyAlert(
                    alert_id=f"speed-{event.delegate_id}",
                    alert_type="voting_speed",
                    severity="medium",
                    description=f"Suspiciously fast voting: {time_diff:.1f}s between votes",
                    evidence={"delegate_id": event.delegate_id, "interval_seconds": round(time_diff, 2)},
                )
        return None

    def _check_geographic_anomaly(self, event: VoteEvent) -> Optional[AnomalyAlert]:
        if not event.is_remote:
            return None
        # In production, compare IP geolocation with delegate's registered state
        return None

    def get_anomaly_summary(self) -> Dict[str, Any]:
        """Return summary of all detected anomalies."""
        severity_counts = Counter(a.severity for a in self.alerts)
        type_counts = Counter(a.alert_type for a in self.alerts)
        return {
            "total_alerts": len(self.alerts),
            "by_severity": dict(severity_counts),
            "by_type": dict(type_counts),
            "critical_alerts": [asdict(a) for a in self.alerts if a.severity == "critical"],
        }


# ═══════════════════════════════════════════════════════════════════════════
# TURNOUT PREDICTION MODEL
# ═══════════════════════════════════════════════════════════════════════════


class TurnoutPredictor:
    """Predicts final turnout based on early voting patterns."""

    def __init__(self):
        self.vote_timestamps: List[float] = []
        self.eligible_voters: int = 0
        self.historical_turnouts: List[float] = []

    def set_eligible_voters(self, count: int):
        self.eligible_voters = max(count, 1)

    def add_historical_turnout(self, pct: float):
        self.historical_turnouts.append(pct)

    def record_vote(self, timestamp: float):
        self.vote_timestamps.append(timestamp)

    def predict_turnout(self, round_id: str, voting_duration_hours: float = 6.0) -> TurnoutPrediction:
        """Predict final turnout using logistic growth model."""
        if not self.vote_timestamps or self.eligible_voters == 0:
            return TurnoutPrediction(
                round_id=round_id,
                predicted_turnout_pct=0.0,
                confidence_interval=(0.0, 0.0),
                model_type="no_data",
            )

        # Sort timestamps
        sorted_times = sorted(self.vote_timestamps)
        start_time = sorted_times[0]
        elapsed_hours = (sorted_times[-1] - start_time) / 3600.0
        elapsed_hours = max(elapsed_hours, 0.01)

        current_turnout = len(sorted_times) / self.eligible_voters * 100
        time_fraction = min(elapsed_hours / voting_duration_hours, 1.0)

        # Logistic growth model: T(t) = L / (1 + e^(-k*(t-t0)))
        # where L is the carrying capacity (max turnout)
        if self.historical_turnouts:
            avg_hist = np.mean(self.historical_turnouts)
        else:
            avg_hist = 70.0  # Default Nigerian primary turnout

        # Estimate final turnout
        if time_fraction > 0.1:
            growth_rate = current_turnout / time_fraction
            predicted = min(growth_rate * 0.85, avg_hist * 1.2)  # Decay factor
        else:
            predicted = avg_hist

        predicted = max(min(predicted, 100.0), current_turnout)

        # Confidence interval (wider early, narrower late)
        ci_width = max(5.0, 30.0 * (1.0 - time_fraction))
        ci_low = max(current_turnout, predicted - ci_width)
        ci_high = min(100.0, predicted + ci_width)

        return TurnoutPrediction(
            round_id=round_id,
            predicted_turnout_pct=round(predicted, 2),
            confidence_interval=(round(ci_low, 2), round(ci_high, 2)),
            model_type="logistic_growth",
            features_used=["elapsed_time", "current_turnout", "historical_avg", "growth_rate"],
        )


# ═══════════════════════════════════════════════════════════════════════════
# DELEGATE BEHAVIOR ANALYSIS
# ═══════════════════════════════════════════════════════════════════════════


class DelegateBehaviorAnalyzer:
    """Analyzes delegate voting patterns for irregularities."""

    def __init__(self):
        self.profiles: Dict[str, DelegateProfile] = {}
        self.state_voting_patterns: Dict[str, Dict[str, int]] = defaultdict(lambda: defaultdict(int))

    def add_profile(self, profile: DelegateProfile):
        self.profiles[profile.delegate_id] = profile

    def record_vote(self, delegate_id: str, aspirant_id: str, state_code: str):
        if delegate_id in self.profiles:
            self.profiles[delegate_id].vote_time = time.time()
        self.state_voting_patterns[state_code][aspirant_id] += 1

    def detect_bloc_voting(self, threshold: float = 0.85) -> List[Dict[str, Any]]:
        """Detect states where delegates vote overwhelmingly for one aspirant."""
        bloc_alerts = []
        for state, aspirant_votes in self.state_voting_patterns.items():
            total = sum(aspirant_votes.values())
            if total < 5:
                continue
            for aspirant, votes in aspirant_votes.items():
                pct = votes / total
                if pct >= threshold:
                    bloc_alerts.append({
                        "state": state,
                        "aspirant": aspirant,
                        "percentage": round(pct * 100, 1),
                        "total_votes": total,
                        "alert_type": "bloc_voting",
                        "severity": "medium" if pct < 0.95 else "high",
                    })
        return bloc_alerts

    def detect_accreditation_anomalies(self) -> List[Dict[str, Any]]:
        """Detect suspicious accreditation patterns."""
        anomalies = []
        accreditation_times = [
            p.accreditation_time for p in self.profiles.values()
            if p.accreditation_time > 0
        ]
        if not accreditation_times:
            return anomalies

        mean_time = np.mean(accreditation_times)
        std_time = np.std(accreditation_times)

        for delegate_id, profile in self.profiles.items():
            if profile.accreditation_time > 0 and std_time > 0:
                z_score = (profile.accreditation_time - mean_time) / std_time
                if abs(z_score) > 3:
                    anomalies.append({
                        "delegate_id": delegate_id,
                        "accreditation_time": profile.accreditation_time,
                        "z_score": round(z_score, 2),
                        "alert_type": "accreditation_time_outlier",
                    })

        return anomalies

    def get_state_breakdown(self) -> Dict[str, Any]:
        """Get voting pattern breakdown by state."""
        breakdown = {}
        for state, aspirant_votes in self.state_voting_patterns.items():
            total = sum(aspirant_votes.values())
            breakdown[state] = {
                "total_votes": total,
                "aspirant_breakdown": {
                    asp: {"votes": v, "pct": round(v / max(total, 1) * 100, 1)}
                    for asp, v in sorted(aspirant_votes.items(), key=lambda x: -x[1])
                },
            }
        return breakdown


# ═══════════════════════════════════════════════════════════════════════════
# REMOTE VOTING FRAUD DETECTION
# ═══════════════════════════════════════════════════════════════════════════


class RemoteVotingFraudDetector:
    """ML-based fraud detection for remote electronic voting."""

    def __init__(self):
        self.sessions: List[Dict[str, Any]] = []
        self.risk_scores: Dict[str, float] = {}

    def score_session(self, session: Dict[str, Any]) -> float:
        """Calculate fraud risk score for a remote voting session (0-100)."""
        risk = 0.0
        factors = []

        # Factor 1: Device registration age
        device_age_hours = session.get("device_age_hours", 0)
        if device_age_hours < 1:
            risk += 25
            factors.append("device_registered_recently")
        elif device_age_hours < 24:
            risk += 10
            factors.append("device_registered_within_24h")

        # Factor 2: Session velocity (time from auth to vote)
        auth_to_vote_seconds = session.get("auth_to_vote_seconds", 0)
        if auth_to_vote_seconds < 5:
            risk += 20
            factors.append("suspiciously_fast_vote")
        elif auth_to_vote_seconds < 15:
            risk += 5

        # Factor 3: IP reputation
        ip_votes = session.get("ip_total_votes", 0)
        if ip_votes > 5:
            risk += 30
            factors.append("high_ip_concentration")
        elif ip_votes > 2:
            risk += 10

        # Factor 4: Biometric verification
        if not session.get("biometric_verified", False):
            risk += 15
            factors.append("no_biometric")

        # Factor 5: Multiple failed OTP attempts
        otp_failures = session.get("otp_failures", 0)
        if otp_failures > 3:
            risk += 20
            factors.append("multiple_otp_failures")
        elif otp_failures > 0:
            risk += 5

        # Factor 6: VPN/proxy detection
        if session.get("is_vpn", False):
            risk += 15
            factors.append("vpn_detected")

        risk = min(risk, 100.0)
        self.risk_scores[session.get("session_id", "")] = risk
        session["risk_score"] = risk
        session["risk_factors"] = factors
        self.sessions.append(session)

        return risk

    def get_high_risk_sessions(self, threshold: float = 50.0) -> List[Dict[str, Any]]:
        """Return sessions above the risk threshold."""
        return [s for s in self.sessions if s.get("risk_score", 0) >= threshold]

    def get_risk_distribution(self) -> Dict[str, int]:
        """Distribution of risk scores."""
        buckets = {"low (0-25)": 0, "medium (25-50)": 0, "high (50-75)": 0, "critical (75-100)": 0}
        for score in self.risk_scores.values():
            if score < 25:
                buckets["low (0-25)"] += 1
            elif score < 50:
                buckets["medium (25-50)"] += 1
            elif score < 75:
                buckets["high (50-75)"] += 1
            else:
                buckets["critical (75-100)"] += 1
        return buckets


# ═══════════════════════════════════════════════════════════════════════════
# ROUND RESULT ANALYSIS
# ═══════════════════════════════════════════════════════════════════════════


def analyze_round_results(results: List[Dict[str, Any]], total_eligible: int) -> Dict[str, Any]:
    """Comprehensive analysis of a voting round's results."""
    if not results:
        return {"error": "no results"}

    total_votes = sum(r.get("votes", 0) for r in results)
    turnout_pct = (total_votes / max(total_eligible, 1)) * 100

    # Winner analysis
    sorted_results = sorted(results, key=lambda x: -x.get("votes", 0))
    winner = sorted_results[0] if sorted_results else None
    runner_up = sorted_results[1] if len(sorted_results) > 1 else None

    margin = 0
    if winner and runner_up:
        margin = winner["votes"] - runner_up["votes"]

    # Check if runoff needed (no majority)
    winner_pct = (winner["votes"] / max(total_votes, 1) * 100) if winner else 0
    needs_runoff = winner_pct < 50.0

    # Effective number of candidates (Laakso-Taagepera index)
    vote_shares = [r["votes"] / max(total_votes, 1) for r in results if r.get("votes", 0) > 0]
    hhi = sum(s ** 2 for s in vote_shares) if vote_shares else 1
    effective_candidates = round(1 / max(hhi, 0.001), 2)

    # Competitiveness index (0 = one-sided, 100 = perfectly competitive)
    if len(vote_shares) >= 2:
        sorted_shares = sorted(vote_shares, reverse=True)
        competitiveness = round((1 - (sorted_shares[0] - sorted_shares[1])) * 100, 1)
    else:
        competitiveness = 0

    return {
        "total_votes": total_votes,
        "total_eligible": total_eligible,
        "turnout_pct": round(turnout_pct, 2),
        "winner": {
            "aspirant_id": winner.get("aspirant_id", ""),
            "name": winner.get("full_name", ""),
            "votes": winner.get("votes", 0),
            "percentage": round(winner_pct, 2),
        } if winner else None,
        "margin_of_victory": margin,
        "needs_runoff": needs_runoff,
        "effective_candidates": effective_candidates,
        "competitiveness_index": competitiveness,
        "results": [{
            "aspirant_id": r.get("aspirant_id", ""),
            "votes": r.get("votes", 0),
            "percentage": round(r.get("votes", 0) / max(total_votes, 1) * 100, 2),
        } for r in sorted_results],
    }


# ═══════════════════════════════════════════════════════════════════════════
# BENFORD'S LAW ANALYSIS (Digit distribution fraud detection)
# ═══════════════════════════════════════════════════════════════════════════


def benfords_law_test(vote_counts: List[int]) -> Dict[str, Any]:
    """Test if vote counts follow Benford's Law (used to detect fabrication)."""
    if not vote_counts or all(v == 0 for v in vote_counts):
        return {"applicable": False, "reason": "insufficient data"}

    # Expected Benford distribution for first digit
    expected = {d: math.log10(1 + 1 / d) for d in range(1, 10)}

    # Observed first digit distribution
    first_digits = []
    for count in vote_counts:
        if count > 0:
            first_digit = int(str(abs(count))[0])
            first_digits.append(first_digit)

    if len(first_digits) < 20:
        return {"applicable": False, "reason": "need at least 20 data points"}

    observed = Counter(first_digits)
    total = len(first_digits)

    # Chi-squared test
    chi_squared = 0
    digit_analysis = {}
    for d in range(1, 10):
        obs_freq = observed.get(d, 0) / total
        exp_freq = expected[d]
        chi_squared += ((obs_freq - exp_freq) ** 2) / exp_freq
        digit_analysis[str(d)] = {
            "observed_pct": round(obs_freq * 100, 2),
            "expected_pct": round(exp_freq * 100, 2),
            "deviation": round(abs(obs_freq - exp_freq) * 100, 2),
        }

    # Critical value at 95% confidence, 8 degrees of freedom = 15.507
    conforms = chi_squared < 15.507

    return {
        "applicable": True,
        "chi_squared": round(chi_squared, 4),
        "conforms_to_benfords": conforms,
        "verdict": "natural" if conforms else "potentially_fabricated",
        "confidence": "95%",
        "digit_analysis": digit_analysis,
        "data_points": len(first_digits),
    }


# ═══════════════════════════════════════════════════════════════════════════
# FASTAPI ENDPOINTS
# ═══════════════════════════════════════════════════════════════════════════


# Global instances
anomaly_detector = VoteAnomalyDetector()
turnout_predictor = TurnoutPredictor()
behavior_analyzer = DelegateBehaviorAnalyzer()
fraud_detector = RemoteVotingFraudDetector()


def create_voting_analytics_routes(app):
    """Register FastAPI routes for voting analytics."""
    from fastapi import Request
    from fastapi.responses import JSONResponse

    @app.post("/analytics/vote-event")
    async def ingest_vote_event(request: Request):
        data = await request.json()
        event = VoteEvent(
            ballot_id=data.get("ballot_id", ""),
            round_id=data.get("round_id", ""),
            delegate_id=data.get("delegate_id", ""),
            aspirant_id=data.get("aspirant_id", ""),
            vote_type=data.get("vote_type", "for"),
            is_remote=data.get("is_remote", False),
            ip_hash=data.get("ip_hash", ""),
            device_fingerprint=data.get("device_fingerprint", ""),
        )
        alerts = anomaly_detector.ingest_vote(event)
        turnout_predictor.record_vote(event.timestamp)
        return JSONResponse({
            "processed": True,
            "alerts": [asdict(a) for a in alerts],
        })

    @app.get("/analytics/anomalies")
    async def get_anomalies():
        return JSONResponse(anomaly_detector.get_anomaly_summary())

    @app.post("/analytics/predict-turnout")
    async def predict_turnout(request: Request):
        data = await request.json()
        turnout_predictor.set_eligible_voters(data.get("eligible_voters", 1000))
        prediction = turnout_predictor.predict_turnout(
            round_id=data.get("round_id", ""),
            voting_duration_hours=data.get("duration_hours", 6.0),
        )
        return JSONResponse(asdict(prediction))

    @app.post("/analytics/score-session")
    async def score_remote_session(request: Request):
        data = await request.json()
        risk = fraud_detector.score_session(data)
        return JSONResponse({
            "risk_score": risk,
            "risk_level": "low" if risk < 25 else "medium" if risk < 50 else "high" if risk < 75 else "critical",
        })

    @app.get("/analytics/risk-distribution")
    async def risk_distribution():
        return JSONResponse(fraud_detector.get_risk_distribution())

    @app.post("/analytics/round-results")
    async def analyze_results(request: Request):
        data = await request.json()
        analysis = analyze_round_results(
            results=data.get("results", []),
            total_eligible=data.get("total_eligible", 0),
        )
        return JSONResponse(analysis)

    @app.post("/analytics/benfords-test")
    async def benfords_test(request: Request):
        data = await request.json()
        result = benfords_law_test(data.get("vote_counts", []))
        return JSONResponse(result)

    @app.get("/analytics/delegate-behavior")
    async def delegate_behavior():
        bloc_alerts = behavior_analyzer.detect_bloc_voting()
        accred_anomalies = behavior_analyzer.detect_accreditation_anomalies()
        return JSONResponse({
            "bloc_voting_alerts": bloc_alerts,
            "accreditation_anomalies": accred_anomalies,
            "state_breakdown": behavior_analyzer.get_state_breakdown(),
        })
