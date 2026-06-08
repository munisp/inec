package main

import (
	"os"
	"strconv"
)

// PlatformConfig centralises all configurable thresholds and metrics.
// Every value reads from an environment variable first, falling back to its default.
var platformCfg = loadPlatformConfig()

// PlatformConfig holds all tunable platform thresholds.
type PlatformConfig struct {
	// Biometric thresholds
	BiometricMatchThreshold  float64 // BIOMETRIC_MATCH_THRESHOLD  (default 0.85)
	BiometricFusionThreshold float64 // BIOMETRIC_FUSION_THRESHOLD (default 0.85)
	HighConfidenceThreshold  float64 // HIGH_CONFIDENCE_THRESHOLD  (default 0.95)
	FaceWeightFactor         float64 // FACE_WEIGHT_FACTOR         (default 0.85)
	IrisWeightFactor         float64 // IRIS_WEIGHT_FACTOR         (default 0.80)
	MotionBaseScore          float64 // MOTION_BASE_SCORE          (default 0.75)
	CaptureQualityThreshold  float64 // CAPTURE_QUALITY_THRESHOLD  (default 0.70)
	FARThreshold             float64 // FAR_THRESHOLD              (default 0.0001)
	FRRThreshold             float64 // FRR_THRESHOLD              (default 0.01)

	// Anomaly detection
	AnomalyTurnoutCeiling float64 // ANOMALY_TURNOUT_CEILING (default 0.95)
	AnomalyHighScore      float64 // ANOMALY_HIGH_SCORE      (default 0.95)

	// NFIQ quality mapping
	NFIQ1Quality float64 // NFIQ1_QUALITY (default 0.95)
	NFIQ2Quality float64 // NFIQ2_QUALITY (default 0.85)
	NFIQ3Quality float64 // NFIQ3_QUALITY (default 0.72)
	NFIQ4Quality float64 // NFIQ4_QUALITY (default 0.55)
	NFIQ5Quality float64 // NFIQ5_QUALITY (default 0.35)

	// Connection pool
	DBMaxOpenConns        int // DB_MAX_OPEN_CONNS         (default 50)
	DBMaxIdleConns        int // DB_MAX_IDLE_CONNS         (default 25)
	DBReplicaMaxOpenConns int // DB_REPLICA_MAX_OPEN_CONNS (default 100)
	DBReplicaMaxIdleConns int // DB_REPLICA_MAX_IDLE_CONNS (default 50)

	// Rate limiting
	RateLimitRPS    int // RATE_LIMIT_RPS    (default 100)
	RateLimitWindow int // RATE_LIMIT_WINDOW (default 60) seconds
}

func loadPlatformConfig() PlatformConfig {
	return PlatformConfig{
		BiometricMatchThreshold:  cfgFloat("BIOMETRIC_MATCH_THRESHOLD", 0.85),
		BiometricFusionThreshold: cfgFloat("BIOMETRIC_FUSION_THRESHOLD", 0.85),
		HighConfidenceThreshold:  cfgFloat("HIGH_CONFIDENCE_THRESHOLD", 0.95),
		FaceWeightFactor:         cfgFloat("FACE_WEIGHT_FACTOR", 0.85),
		IrisWeightFactor:         cfgFloat("IRIS_WEIGHT_FACTOR", 0.80),
		MotionBaseScore:          cfgFloat("MOTION_BASE_SCORE", 0.75),
		CaptureQualityThreshold:  cfgFloat("CAPTURE_QUALITY_THRESHOLD", 0.70),
		FARThreshold:             cfgFloat("FAR_THRESHOLD", 0.0001),
		FRRThreshold:             cfgFloat("FRR_THRESHOLD", 0.01),
		AnomalyTurnoutCeiling:    cfgFloat("ANOMALY_TURNOUT_CEILING", 0.95),
		AnomalyHighScore:         cfgFloat("ANOMALY_HIGH_SCORE", 0.95),
		NFIQ1Quality:             cfgFloat("NFIQ1_QUALITY", 0.95),
		NFIQ2Quality:             cfgFloat("NFIQ2_QUALITY", 0.85),
		NFIQ3Quality:             cfgFloat("NFIQ3_QUALITY", 0.72),
		NFIQ4Quality:             cfgFloat("NFIQ4_QUALITY", 0.55),
		NFIQ5Quality:             cfgFloat("NFIQ5_QUALITY", 0.35),
		DBMaxOpenConns:           cfgInt("DB_MAX_OPEN_CONNS", 50),
		DBMaxIdleConns:           cfgInt("DB_MAX_IDLE_CONNS", 25),
		DBReplicaMaxOpenConns:    cfgInt("DB_REPLICA_MAX_OPEN_CONNS", 100),
		DBReplicaMaxIdleConns:    cfgInt("DB_REPLICA_MAX_IDLE_CONNS", 50),
		RateLimitRPS:             cfgInt("RATE_LIMIT_RPS", 100),
		RateLimitWindow:          cfgInt("RATE_LIMIT_WINDOW", 60),
	}
}

func cfgFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func cfgInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
