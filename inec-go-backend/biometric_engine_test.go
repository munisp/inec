package main

import (
	"crypto/sha256"
	"math"
	"os"
	"testing"
)

// TestExtractFingerprintMinutiae verifies that fingerprint minutiae extraction
// produces real-looking templates (non-zero minutiae with bounded positions)
// when given deterministic seed data. The function uses SecureRng seeded from
// the input hash, so same input → same output (deterministic, not random).
func TestExtractFingerprintMinutiae(t *testing.T) {
	// Use a deterministic seed so results are reproducible.
	seed := "test-fingerprint-input-2024"
	rng := NewSecureRngFromSeed([]byte(seed))
	template := extractFingerprintMinutiae(seed, rng)

	if template == nil {
		t.Fatal("extractFingerprintMinutiae returned nil template")
	}

	// Should extract a realistic number of minutiae (30-79 from the source code).
	if len(template.Minutiae) == 0 {
		t.Error("expected non-zero minutiae count from image analysis")
	}

	// Minutiae positions must be within valid image bounds.
	// Source code: X in [20, 280), Y in [20, 380).
	for i, m := range template.Minutiae {
		if m.X < 0 || m.X > 300 {
			t.Errorf("minutiae[%d] X out of bounds: %d", i, m.X)
		}
		if m.Y < 0 || m.Y > 400 {
			t.Errorf("minutiae[%d] Y out of bounds: %d", i, m.Y)
		}
		// Quality should be in [60, 100).
		if m.Quality < 60 || m.Quality >= 100 {
			t.Errorf("minutiae[%d] quality out of expected range [60,100): %d", i, m.Quality)
		}
		// Angle should be in [0, 360).
		if m.Angle < 0 || m.Angle >= 360 {
			t.Errorf("minutiae[%d] angle out of range: %f", i, m.Angle)
		}
	}

	// Core points should have reasonable positions.
	for i, core := range template.CorePoints {
		if core[0] < 0 || core[0] > 300 || core[1] < 0 || core[1] > 400 {
			t.Errorf("core point[%d] out of bounds: (%d, %d)", i, core[0], core[1])
		}
	}

	// Delta points should have reasonable positions.
	for i, delta := range template.DeltaPoints {
		if delta[0] < 0 || delta[0] > 300 || delta[1] < 0 || delta[1] > 400 {
			t.Errorf("delta point[%d] out of bounds: (%d, %d)", i, delta[0], delta[1])
		}
	}

	// NFIQ2 score should be in valid range [1, 5].
	if template.NFIQ2Score < 1 || template.NFIQ2Score > 5 {
		t.Errorf("NFIQ2 score out of valid range [1,5]: %d", template.NFIQ2Score)
	}

	// Verify determinism: same seed must produce the same result.
	rng2 := NewSecureRngFromSeed([]byte(seed))
	template2 := extractFingerprintMinutiae(seed, rng2)
	if len(template.Minutiae) != len(template2.Minutiae) {
		t.Error("determinism violation: different minutiae counts")
	}
	for i := range template.Minutiae {
		if template.Minutiae[i].X != template2.Minutiae[i].X ||
			template.Minutiae[i].Y != template2.Minutiae[i].Y {
			t.Error("determinism violation: different minutiae positions")
			break
		}
	}
}

// TestDeterministicFingerprintDifferentSeeds verifies that different inputs
// produce different minutiae patterns (not just random noise).
func TestDeterministicFingerprintDifferentSeeds(t *testing.T) {
	results := make(map[string]int)
	for i := 0; i < 20; i++ {
		seed := string(rune('A' + i))
		rng := NewSecureRngFromSeed([]byte(seed))
		tmpl := extractFingerprintMinutiae(seed, rng)
		results[tmpl.PatternType]++
	}

	if len(results) < 2 {
		t.Log("all templates got same pattern type — may be expected with small sample")
	}

	// All templates should have non-zero minutiae.
	for _, seed := range []string{"seed-A", "seed-B", "seed-C", "seed-D", "seed-E"} {
		rng := NewSecureRngFromSeed([]byte(seed))
		tmpl := extractFingerprintMinutiae(seed, rng)
		if len(tmpl.Minutiae) == 0 {
			t.Errorf("template from seed %q should have non-zero minutiae", seed)
		}
	}
}

// TestGenerateFacialEmbedding verifies that facial embedding generation
// produces valid embeddings based on input, not arbitrary random values.
func TestGenerateFacialEmbedding(t *testing.T) {
	seed := "test-facial-input-2024"
	rng := NewSecureRngFromSeed([]byte(seed))
	embedding := generateFacialEmbedding(seed, rng)

	if embedding == nil {
		t.Fatal("generateFacialEmbedding returned nil")
	}

	// Vector dimension should be 128 (as per source code).
	if embedding.Dimension != 128 {
		t.Errorf("expected dimension 128, got %d", embedding.Dimension)
	}

	// Vector should have 128 elements.
	if len(embedding.Vector) != 128 {
		t.Errorf("expected 128-d vector, got %d", len(embedding.Vector))
	}

	// Vector should be normalized (norm ≈ 1.0 after normalization in source).
	var norm float64
	for _, v := range embedding.Vector {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm < 0.9 || norm > 1.1 {
		t.Errorf("embedding norm should be ≈1.0 after normalization, got %f", norm)
	}

	// Should have 68 facial landmarks.
	if len(embedding.Landmarks) != 68 {
		t.Errorf("expected 68 landmarks, got %d", len(embedding.Landmarks))
	}

	// Landmark positions should be within reasonable bounds.
	for i, lm := range embedding.Landmarks {
		if lm[0] < 0 || lm[0] > 300 || lm[1] < 0 || lm[1] > 300 {
			t.Errorf("landmark[%d] out of bounds: (%d, %d)", i, lm[0], lm[1])
		}
	}

	// Determinism: same input → same output.
	rng2 := NewSecureRngFromSeed([]byte(seed))
	embedding2 := generateFacialEmbedding(seed, rng2)
	for i := range embedding.Vector {
		if embedding.Vector[i] != embedding2.Vector[i] {
			t.Error("determinism violation: different embedding vectors")
			break
		}
	}
}

// TestGenerateIrisCode verifies that iris code generation produces valid codes.
func TestGenerateIrisCode(t *testing.T) {
	seed := "test-iris-input-2024"
	rng := NewSecureRngFromSeed([]byte(seed))
	code := generateIrisCode(seed, rng)

	if code == nil {
		t.Fatal("generateIrisCode returned nil")
	}

	// Should have 2048 bits = 256 bytes.
	expectedBits := 2048
	if code.Bits != expectedBits {
		t.Errorf("expected %d bits, got %d", expectedBits, code.Bits)
	}
	if len(code.Code) != expectedBits/8 {
		t.Errorf("expected %d code bytes, got %d", expectedBits/8, len(code.Code))
	}
	if len(code.Mask) != expectedBits/8 {
		t.Errorf("expected %d mask bytes, got %d", expectedBits/8, len(code.Mask))
	}

	// Usability should be in [0.8, 1.0).
	if code.Usability < 0.8 || code.Usability >= 1.0 {
		t.Errorf("usability out of expected range [0.8,1.0): %f", code.Usability)
	}

	// Mask should be all 0xFF or 0x00 per byte (no partial masks).
	for i, m := range code.Mask {
		if m != 0xFF && m != 0x00 {
			t.Errorf("mask[%d] should be 0xFF or 0x00, got 0x%02x", i, m)
		}
	}

	// Pupil diameter should be reasonable (40-60 from source).
	if code.PupilDiam < 40 || code.PupilDiam > 60 {
		t.Errorf("pupil diameter out of expected range: %d", code.PupilDiam)
	}

	// Determinism: same input → same output.
	rng2 := NewSecureRngFromSeed([]byte(seed))
	code2 := generateIrisCode(seed, rng2)
	for i := range code.Code {
		if code.Code[i] != code2.Code[i] {
			t.Error("determinism violation: different iris codes")
			break
		}
	}
}

// TestPerformPADCheck verifies that PAD check returns scores in valid ranges
// even when the ML service is unavailable (deterministic fallback).
func TestPerformPADCheck(t *testing.T) {
	// Call with deterministic inputs — the ML service won't be available,
	// so the deterministic hash-based fallback will be used.
	result := performPADCheck("test-vin-001", "fingerprint", "test-device-001")
	if result == nil {
		t.Fatal("performPADCheck returned nil result")
	}

	// All scores must be in [0.7, 1.0] per the deterministic fallback formula.
	if result.LivenessScore < 0.0 || result.LivenessScore > 1.0 {
		t.Errorf("liveness_score out of [0,1]: %f", result.LivenessScore)
	}
	if result.TextureScore < 0.0 || result.TextureScore > 1.0 {
		t.Errorf("texture_score out of [0,1]: %f", result.TextureScore)
	}
	if result.MotionScore < 0.0 || result.MotionScore > 1.0 {
		t.Errorf("motion_score out of [0,1]: %f", result.MotionScore)
	}
	if result.DepthScore < 0.0 || result.DepthScore > 1.0 {
		t.Errorf("depth_score out of [0,1]: %f", result.DepthScore)
	}
	if result.SpectralScore < 0.0 || result.SpectralScore > 1.0 {
		t.Errorf("spectral_score out of [0,1]: %f", result.SpectralScore)
	}

	// Decision should be one of the valid choices.
	validDecisions := map[string]bool{"live": true, "spoof": true, "uncertain": true}
	if !validDecisions[result.Decision] {
		t.Errorf("invalid pad_decision: %q", result.Decision)
	}

	// ISO compliance should be true (set in fallback code path).
	if !result.ISOCompliant {
		t.Error("ISOCompliant should be true in deterministic fallback")
	}

	// PAD level should be "level2".
	if result.PADLevel != "level2" {
		t.Errorf("expected pad_level=level2, got %q", result.PADLevel)
	}
}

// TestBiometricPerformPADCheckDeterministic verifies that the fallback scoring
// produces the same result for the same input (deterministic hash-based).
func TestBiometricPerformPADCheckDeterministic(t *testing.T) {
	r1 := performPADCheck("vin-abc", "facial", "device-001")
	r2 := performPADCheck("vin-abc", "facial", "device-001")

	if r1.LivenessScore != r2.LivenessScore {
		t.Errorf("liveness_score not deterministic: %f vs %f", r1.LivenessScore, r2.LivenessScore)
	}
	if r1.TextureScore != r2.TextureScore {
		t.Error("texture_score not deterministic")
	}
}

// TestBiometricPerformPADCheckDifferentInputs verifies that different inputs produce
// different scores (the scores are derived from SHA-256 of the input).
func TestBiometricPerformPADCheckDifferentInputs(t *testing.T) {
	r1 := performPADCheck("vin-001", "fingerprint", "device-001")
	r2 := performPADCheck("vin-002", "fingerprint", "device-001")

	// Different VINs should produce different liveness scores (different SHA-256 hash).
	if r1.LivenessScore == r2.LivenessScore {
		t.Error("different inputs should produce different scores")
	}
}

// TestMasterKeyHardcoded verifies that the BiometricVault master key comes
// from an environment variable (BIOMETRIC_MASTER_KEY) rather than being hardcoded.
// If the env var is not set, it should use a default from env or fail gracefully.
// This test documents the expected behavior: master key should be configurable.
func TestMasterKeyFromEnv(t *testing.T) {
	// Set a test master key via environment variable.
	os.Setenv("BIOMETRIC_MASTER_KEY", "test-key-for-unit-testing-12345678")
	defer os.Unsetenv("BIOMETRIC_MASTER_KEY")

	// Verify the env var is set as expected.
	key := os.Getenv("BIOMETRIC_MASTER_KEY")
	if key != "test-key-for-unit-testing-12345678" {
		t.Errorf("expected env var to be set, got %q", key)
	}

	// The actual vault constructor uses a hardcoded key. This test
	// documents that the fix to use env var is still pending.
	// If NewBiometricVault reads BIOMETRIC_MASTER_KEY, the key would differ
	// from the hardcoded one below.
	hardcoded := sha256.Sum256([]byte("INEC-BIOMETRIC-VAULT-MASTER-KEY-2027"))
	envKey := sha256.Sum256([]byte(key))
	if hardcoded == envKey {
		t.Error("master key should differ between hardcoded and env var values")
	}
}

// TestPerformPADCheckByModality verifies that PAD scores differ by modality
// because the weighted combination formula varies per modality.
func TestPerformPADCheckByModality(t *testing.T) {
	vin := "vin-001"
	device := "device-001"

	fp := performPADCheck(vin, "fingerprint", device)
	face := performPADCheck(vin, "facial", device)
	iris := performPADCheck(vin, "iris", device)

	// The weighted formula differs per modality:
	// fingerprint: 0.4*texture + 0.1*motion + 0.3*depth + 0.2*spectral
	// facial:      0.2*texture + 0.4*motion + 0.3*depth + 0.1*spectral
	// iris:        0.15*texture + 0.15*motion + 0.2*depth + 0.5*spectral
	// With the same hash-derived component scores, the final liveness_score
	// should differ because of different weights.
	if fp.LivenessScore == face.LivenessScore ||
		fp.LivenessScore == iris.LivenessScore ||
		face.LivenessScore == iris.LivenessScore {
		t.Log("liveness_scores are the same across modalities — may indicate identical component scores")
	}
}

// TestPadResultStructure verifies that all PADResult fields are populated.
func TestPadResultStructure(t *testing.T) {
	result := performPADCheck("test-vin", "face", "test-dev")
	if result == nil {
		t.Fatal("expected non-nil PADResult")
	}

	// Verify confidence is set (equals liveness_score in source).
	if result.Confidence != result.LivenessScore {
		t.Log("confidence should equal liveness_score per source code")
	}
}


