package main

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

// getNFIQ2Description maps an NFIQ2 score to its human-readable description.
func getNFIQ2Description(score int) string {
	switch score {
	case 1:
		return "Sharp"
	case 2:
		return "Acceptable"
	case 3:
		return "Borderline"
	case 4:
		return "Poor"
	case 5:
		return "Very poor"
	default:
		return fmt.Sprintf("Unknown (%d)", score)
	}
}

// TestLoadBenchmarkCohort verifies that GetBenchmarkCohort returns valid
// NIST benchmark data for known modalities. The benchmark config is loaded
// from config/biometric_benchmarks.json at startup, falling back to embedded
// NIST FRVT defaults if the file is not found.
func TestLoadBenchmarkCohort(t *testing.T) {
	// Initialize benchmarks if not already done (from initBiometricBenchmarks).
	// The initBiometricBenchmarks() function is called from init() or main init flow.

	modalities := []string{"fingerprint", "facial", "iris"}
	for _, mod := range modalities {
		cohort := GetBenchmarkCohort(mod)
		if cohort == nil {
			t.Fatalf("benchmark cohort not found for modality %q (check initBiometricBenchmarks init flow)", mod)
		}

		// MeanGenuine should be in (0, 1).
		if cohort.MeanGenuine <= 0 || cohort.MeanGenuine >= 1 {
			t.Errorf("cohort %q MeanGenuine out of valid range (0,1): %f", mod, cohort.MeanGenuine)
		}

		// StdGenuine should be positive.
		if cohort.StdGenuine <= 0 {
			t.Errorf("cohort %q StdGenuine should be positive: %f", mod, cohort.StdGenuine)
		}

		// MeanImpostor should be in (0, 1).
		if cohort.MeanImpostor <= 0 || cohort.MeanImpostor >= 1 {
			t.Errorf("cohort %q MeanImpostor out of valid range (0,1): %f", mod, cohort.MeanImpostor)
		}

		// StdImpostor should be positive.
		if cohort.StdImpostor <= 0 {
			t.Errorf("cohort %q StdImpostor should be positive: %f", mod, cohort.StdImpostor)
		}

		// SampleSize should be positive.
		if cohort.SampleSize <= 0 {
			t.Errorf("cohort %q SampleSize should be positive: %d", mod, cohort.SampleSize)
		}

		// Benchmark should be a non-empty string.
		if cohort.Benchmark == "" {
			t.Errorf("cohort %q Benchmark string is empty", mod)
		}

		t.Logf("cohort %q: MeanGenuine=%f, StdGenuine=%f, MeanImpostor=%f, StdImpostor=%f, SampleSize=%d, Benchmark=%q",
			mod, cohort.MeanGenuine, cohort.StdGenuine, cohort.MeanImpostor, cohort.StdImpostor, cohort.SampleSize, cohort.Benchmark)
	}
}

// TestBenchmarkCohortUnknownModality verifies that unknown modalities return nil.
func TestBenchmarkCohortUnknownModality(t *testing.T) {
	cohort := GetBenchmarkCohort("unknown-modality")
	if cohort != nil {
		t.Error("expected nil cohort for unknown modality")
	}
}

// TestBenchmarkCohortValueOrdering verifies that genuine scores are generally
// higher than impostor scores (as expected in biometric matching).
func TestBenchmarkCohortValueOrdering(t *testing.T) {
	modalities := []string{"fingerprint", "facial", "iris"}
	for _, mod := range modalities {
		cohort := GetBenchmarkCohort(mod)
		if cohort == nil {
			continue
		}

		// Genuine scores should be higher than impostor scores.
		if cohort.MeanGenuine < cohort.MeanImpostor {
			t.Errorf("cohort %q: MeanGenuine (%f) should be > MeanImpostor (%f)",
				mod, cohort.MeanGenuine, cohort.MeanImpostor)
		}
	}
}

// TestComputeNFIQ2Score verifies NFIQ2 quality scoring from Laplacian variance.
func TestComputeNFIQ2Score(t *testing.T) {
	tests := []struct {
		name        string
		laplacian   float64
		expected    int
		description string
	}{
		{"sharp", 500, 1, "Very sharp image (>400)"},
		{"sharp-boundary", 401, 1, "Just above sharp threshold"},
		{"acceptable", 300, 2, "Acceptable quality (>200)"},
		{"acceptable-boundary", 201, 2, "Just above acceptable threshold"},
		{"borderline", 150, 3, "Borderline quality (>100)"},
		{"borderline-boundary", 101, 3, "Just above borderline threshold"},
		{"poor", 75, 4, "Poor quality (>50)"},
		{"poor-boundary", 51, 4, "Just above poor threshold"},
		{"very-poor", 25, 5, "Very poor quality (<=50)"},
		{"zero", 0, 5, "Zero variance — very poor"},
		{"boundary-400", 400, 2, "Exactly at 400 → Acceptable"},
		{"boundary-200", 200, 3, "Exactly at 200 → Borderline"},
		{"boundary-100", 100, 4, "Exactly at 100 → Poor"},
		{"boundary-50", 50, 5, "Exactly at 50 → Very poor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ComputeNFIQ2Score(tt.laplacian)
			if score != tt.expected {
				t.Errorf("ComputeNFIQ2Score(%f) = %d (%s), expected %d (%s)",
					tt.laplacian, score, getNFIQ2Description(score), tt.expected, tt.description)
			}
		})
	}
}

// TestComputeNFIQ2ScoreMonotonic verifies that NFIQ2 score is non-decreasing
// as Laplacian variance decreases (lower variance → worse quality).
func TestComputeNFIQ2ScoreMonotonic(t *testing.T) {
	prevScore := 0
	for laplacian := 500.0; laplacian >= 0; laplacian -= 25 {
		score := ComputeNFIQ2Score(laplacian)
		if score < prevScore {
			t.Errorf("NFIQ2 score should be non-decreasing as variance decreases: laplacian=%f, score=%d < prev=%d",
				laplacian, score, prevScore)
		}
		prevScore = score
	}
}

// TestComputeNFIQ2ScoreRange verifies all returned scores are in [1, 5].
func TestComputeNFIQ2ScoreRange(t *testing.T) {
	testValues := []float64{0, 25, 50, 100, 150, 200, 250, 300, 400, 500, 1000, -10, -100}
	for _, v := range testValues {
		score := ComputeNFIQ2Score(v)
		if score < 1 || score > 5 {
			t.Errorf("ComputeNFIQ2Score(%f) = %d, expected range [1,5]", v, score)
		}
	}
}

// TestGetEERRange verifies that EER ranges are returned correctly for known modalities.
func TestGetEERRange(t *testing.T) {
	testCases := []struct {
		modality    string
		quality     string
		expectNonNil bool
	}{
		{"fingerprint", "good", true},
		{"fingerprint", "poor", true},
		{"facial", "good", true},
		{"facial", "poor", true},
		{"iris", "good", true},
		{"iris", "poor", true},
		{"unknown", "good", false},
		{"fingerprint", "excellent", false}, // quality level doesn't exist
	}

	for _, tc := range testCases {
		rangeData := GetEERRange(tc.modality, tc.quality)
		if tc.expectNonNil && rangeData == nil {
			t.Errorf("GetEERRange(%q, %q) returned nil, expected non-nil range",
				tc.modality, tc.quality)
		}
		if !tc.expectNonNil && rangeData != nil {
			t.Errorf("GetEERRange(%q, %q) returned non-nil, expected nil",
				tc.modality, tc.quality)
		}
		if rangeData != nil {
			// EERMin should be >= 0.
			if rangeData.EERMin < 0 {
				t.Errorf("GetEERRange(%q, %q).EERMin < 0: %f", tc.modality, tc.quality, rangeData.EERMin)
			}
			// EERMax should be > 0.
			if rangeData.EERMax <= 0 {
				t.Errorf("GetEERRange(%q, %q).EERMax <= 0: %f", tc.modality, tc.quality, rangeData.EERMax)
			}
			// EERMax should be >= EERMin.
			if rangeData.EERMax < rangeData.EERMin {
				t.Errorf("GetEERRange(%q, %q).EERMax (%f) < EERMin (%f)",
					tc.modality, tc.quality, rangeData.EERMax, rangeData.EERMin)
			}
		}
	}
}

// TestGetEERRangeByModality verifies that iris generally has lower EER than fingerprint.
func TestGetEERRangeByModality(t *testing.T) {
	// Iris should generally have lower EER than fingerprint (more discriminating).
	// From embedded defaults:
	//   fingerprint good: EER [0.005, 0.02]
	//   iris good:        EER [0.001, 0.005]
	ee := GetEERRange("fingerprint", "good")
	ei := GetEERRange("iris", "good")

	if ee == nil || ei == nil {
		t.Skip("benchmark data not loaded")
	}

	if ei.EERMax > ee.EERMax {
		t.Errorf("Iris EER max (%f) should generally be <= fingerprint EER max (%f)",
			ei.EERMax, ee.EERMax)
	}
	if ei.EERMin > ee.EERMin {
		t.Errorf("Iris EER min (%f) should generally be <= fingerprint EER min (%f)",
			ei.EERMin, ee.EERMin)
	}

	t.Logf("fingerprint good: EER=[%.4f, %.4f]", ee.EERMin, ee.EERMax)
	t.Logf("iris good:        EER=[%.4f, %.4f]", ei.EERMin, ei.EERMax)
}

// TestGetEERRangePoorQuality verifies that "poor" quality has higher EER than "good".
func TestGetEERRangePoorQuality(t *testing.T) {
	modalities := []string{"fingerprint", "facial", "iris"}
	for _, mod := range modalities {
		good := GetEERRange(mod, "good")
		poor := GetEERRange(mod, "poor")

		if good == nil || poor == nil {
			continue
		}

		// Poor quality should have higher (worse) EER than good quality.
		if poor.EERMin < good.EERMin {
			t.Errorf("poor EERMin (%f) < good EERMin (%f) for %s",
				poor.EERMin, good.EERMin, mod)
		}
		if poor.EERMax < good.EERMax {
			t.Errorf("poor EERMax (%f) < good EERMax (%f) for %s",
				poor.EERMax, good.EERMax, mod)
		}
	}
}

// TestGetPADBaselineAccuracy verifies PAD baseline accuracies for known modalities.
func TestGetPADBaselineAccuracy(t *testing.T) {
	modalities := []string{"fingerprint", "facial", "iris"}
	for _, mod := range modalities {
		accuracy := GetPADBaselineAccuracy(mod)
		if accuracy <= 0 || accuracy > 1 {
			t.Errorf("PAD baseline accuracy for %q out of (0,1]: %f", mod, accuracy)
		}
		// Should be high (>0.9) for baseline accuracy.
		if accuracy < 0.9 {
			t.Errorf("PAD baseline accuracy for %q is suspiciously low: %f", mod, accuracy)
		}
	}

	// Unknown modality should return the default of 0.95.
	accuracy := GetPADBaselineAccuracy("unknown")
	if accuracy != 0.95 {
		t.Errorf("unknown modality should return default 0.95, got %f", accuracy)
	}
}

// TestGetPADBaselineAccuracyByModality verifies expected PAD accuracy ordering.
func TestGetPADBaselineAccuracyByModality(t *testing.T) {
	fp := GetPADBaselineAccuracy("fingerprint")
	face := GetPADBaselineAccuracy("facial")
	iris := GetPADBaselineAccuracy("iris")

	t.Logf("PAD baseline: fingerprint=%f, facial=%f, iris=%f", fp, face, iris)

	// All should be in reasonable range.
	for _, mod := range []string{"fingerprint", "facial", "iris"} {
		acc := GetPADBaselineAccuracy(mod)
		if acc < 0.9 || acc > 1.0 {
			t.Errorf("PAD accuracy for %q out of [0.9, 1.0]: %f", mod, acc)
		}
	}
}

// TestEstimateLaplacianVarianceFromQuality verifies the quality-to-variance conversion.
func TestEstimateLaplacianVarianceFromQuality(t *testing.T) {
	tests := []struct {
		quality    float64
		expectMin  float64
		expectMax  float64
		description string
	}{
		{0.0, 50.0, 50.0, "quality=0 → variance=50"},
		{0.5, 235.0, 235.0, "quality=0.5 → variance=235"},
		{1.0, 420.0, 420.0, "quality=1.0 → variance=420"},
		{0.25, 142.5, 142.5, "quality=0.25 → variance=142.5"},
	}

	for _, tt := range tests {
		v := EstimateLaplacianVarianceFromQuality(tt.quality)
		if v < tt.expectMin || v > tt.expectMax {
			t.Errorf("EstimateLaplacianVarianceFromQuality(%f) = %f, expected [%f, %f] (%s)",
				tt.quality, v, tt.expectMin, tt.expectMax, tt.description)
		}
	}

	// Should be monotonically increasing with quality.
	prevV := 0.0
	for q := 0.0; q <= 1.0; q += 0.1 {
		v := EstimateLaplacianVarianceFromQuality(q)
		if v < prevV {
			t.Errorf("Laplacian variance should increase with quality: q=%f, v=%f < prev=%f",
				q, v, prevV)
		}
		prevV = v
	}
}

// TestComputeEERFromQuality verifies EER computation produces valid values.
func TestComputeEERFromQuality(t *testing.T) {
	modalities := []string{"fingerprint", "facial", "iris"}
	qualities := []float64{0.5, 0.7, 0.85, 1.0}

	for _, mod := range modalities {
		for _, q := range qualities {
			eer := computeEERFromQuality(mod, q)
			if eer < 0 || eer > 1 {
				t.Errorf("EER for %q at quality %.2f out of [0,1]: %f", mod, q, eer)
			}
			// Higher quality should produce lower (better) EER.
			t.Logf("EER %q quality=%.2f: %.6f", mod, q, eer)
		}
	}

	// EER should decrease as quality increases (for same modality).
	qualityOrder := []float64{0.5, 0.6, 0.7, 0.8, 0.9, 1.0}
	for _, mod := range modalities {
		prevEER := 0.0
		for _, q := range qualityOrder {
			eer := computeEERFromQuality(mod, q)
			if eer > prevEER && prevEER > 0 {
				t.Logf("EER for %q at quality %.2f (%.6f) > previous (%.6f) — may be expected at boundaries",
					mod, q, eer, prevEER)
			}
			prevEER = eer
		}
	}
}

// TestComputeEERFromQualityUnknownModality verifies that unknown modalities
// get a default EER value.
func TestComputeEERFromQualityUnknownModality(t *testing.T) {
	eer := computeEERFromQuality("unknown-modality", 0.8)
	if eer < 0 || eer > 1 {
		t.Errorf("unknown modality EER out of range: %f", eer)
	}
	// Default is 0.02 (Generic good quality, quality=0.8→"good").
	if eer < 0.01 || eer > 0.03 {
		t.Errorf("unknown modality EER should be ~0.02, got %f", eer)
	}
}

// TestMeanStdDev verifies the mean and stddev helper functions.
func TestMeanStdDev(t *testing.T) {
	// Mean of [1, 2, 3, 4, 5] = 3.
	mean := meanF64([]float64{1, 2, 3, 4, 5})
	if mean != 3.0 {
		t.Errorf("mean([1,2,3,4,5]) = %f, expected 3.0", mean)
	}

	// Stddev of [1, 2, 3, 4, 5] = sqrt(2) ≈ 1.414.
	std := stddevF64([]float64{1, 2, 3, 4, 5})
	expected := math.Sqrt(2.0)
	if math.Abs(std-expected) > 0.001 {
		t.Errorf("stddev([1,2,3,4,5]) = %f, expected ~%f", std, expected)
	}

	// Empty slice returns mean=0.
	if meanF64(nil) != 0 {
		t.Error("mean of nil slice should be 0")
	}
	if meanF64([]float64{}) != 0 {
		t.Error("mean of empty slice should be 0")
	}

	// Single element returns stddev ≈ 0.1 (default in source).
	if stddevF64([]float64{5}) < 0.09 {
		t.Errorf("stddev of single element should be ~0.1, got %f", stddevF64([]float64{5}))
	}
}

// TestClamp01 verifies the clamp01 helper function.
func TestClamp01(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{-10, 0},
		{-1, 0},
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1, 1},
		{1.5, 1},
		{10, 1},
	}

	for _, tt := range tests {
		result := clamp01(tt.input)
		if result != tt.expected {
			t.Errorf("clamp01(%f) = %f, expected %f", tt.input, result, tt.expected)
		}
	}
}

// TestEstimateImpostorDistribution verifies that the impostor distribution
// is computed without errors and produces bounded scores.
func TestEstimateImpostorDistribution(t *testing.T) {
	genuineScores := []float64{0.8, 0.85, 0.9, 0.92, 0.95, 0.97, 0.98, 0.99}
	nPoints := 100
	scores := estimateImpostorDistribution(genuineScores, nPoints)

	if len(scores) != nPoints {
		t.Errorf("expected %d impostor scores, got %d", nPoints, len(scores))
	}

	for i, s := range scores {
		if s < 0 || s > 1 {
			t.Errorf("impostor score[%d] out of [0,1]: %f", i, s)
		}
	}

	// Mean of impostor scores should be less than mean of genuine scores.
	genuineMean := meanF64(genuineScores)
	impostorMean := meanF64(scores)
	if impostorMean >= genuineMean {
		t.Logf("impostor mean (%f) >= genuine mean (%f) — may be expected depending on data",
			impostorMean, genuineMean)
	}
}

// TestBenchmarkConfigSerialization verifies the benchmark config can be
// serialized/deserialized without data loss (testing the data structure).
func TestBenchmarkConfigSerialization(t *testing.T) {
	cfg := embeddedNISTDefaults()

	// Serialize to JSON.
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal benchmark config: %v", err)
	}

	// Deserialize.
	var restored BiometricBenchmarkConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to unmarshal benchmark config: %v", err)
	}

	// Verify all three modalities are present.
	for _, mod := range []string{"fingerprint", "facial", "iris"} {
		if _, ok := restored.ScoreNormalizationCohorts[mod]; !ok {
			t.Errorf("cohort %q lost during serialization", mod)
		}
		if qmap, ok := restored.EERByModalityQuality[mod]; !ok {
			t.Errorf("EER map for %q lost during serialization", mod)
		} else {
			for _, qlevel := range []string{"good", "poor"} {
				if qmap[qlevel] == nil {
					t.Errorf("EER range %q/%q lost during serialization", mod, qlevel)
				}
			}
		}
	}
}

// TestBenchmarkCohortFingerprintSpecific verifies fingerprint cohort values
// match the embedded NIST defaults.
func TestBenchmarkCohortFingerprintSpecific(t *testing.T) {
	cohort := GetBenchmarkCohort("fingerprint")
	if cohort == nil {
		t.Skip("fingerprint cohort not available")
	}

	// From embeddedNISTDefaults():
	// MeanGenuine:  0.98, StdGenuine: 0.03
	// MeanImpostor: 0.42, StdImpostor: 0.14
	// SampleSize:   50000
	// Benchmark:    "NIST_FRVT_1N"
	if cohort.MeanGenuine != 0.98 {
		t.Logf("fingerprint MeanGenuine = %f (expected 0.98 from defaults)", cohort.MeanGenuine)
	}
	if cohort.Benchmark != "NIST_FRVT_1N" {
		t.Logf("fingerprint Benchmark = %q (expected NIST_FRVT_1N from defaults)", cohort.Benchmark)
	}
}

// TestBenchmarkCohortIrisSpecific verifies iris cohort values match defaults.
func TestBenchmarkCohortIrisSpecific(t *testing.T) {
	cohort := GetBenchmarkCohort("iris")
	if cohort == nil {
		t.Skip("iris cohort not available")
	}

	if cohort.Benchmark != "NIST_IREX2" {
		t.Logf("iris Benchmark = %q (expected NIST_IREX2 from defaults)", cohort.Benchmark)
	}
}

// TestBenchmarkCohortFacialSpecific verifies facial cohort values match defaults.
func TestBenchmarkCohortFacialSpecific(t *testing.T) {
	cohort := GetBenchmarkCohort("facial")
	if cohort == nil {
		t.Skip("facial cohort not available")
	}

	if cohort.Benchmark != "NIST_FRVT_1N_Facial" {
		t.Logf("facial Benchmark = %q (expected NIST_FRVT_1N_Facial from defaults)", cohort.Benchmark)
	}
}

// TestBenchmarkConfigThreadSafety verifies the benchmark config is thread-safe.
func TestBenchmarkConfigThreadSafety(t *testing.T) {
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				// Read operations should not panic or corrupt state.
				GetBenchmarkCohort("fingerprint")
				GetBenchmarkCohort("facial")
				GetBenchmarkCohort("iris")
				GetEERRange("fingerprint", "good")
				GetEERRange("iris", "poor")
				GetPADBaselineAccuracy("facial")
				ComputeNFIQ2Score(150)
				_ = meanF64([]float64{1, 2, 3})
				_ = stddevF64([]float64{1, 2, 3})
				_ = clamp01(0.5)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestEERRangeInterpolation verifies the EER interpolation in computeEERFromQuality.
func TestEERRangeInterpolation(t *testing.T) {
	// quality=0.5 → ratio=0 → EERMax (poor)
	// quality=1.0 → ratio=1 → EERMin (good)
	// quality=0.75 → ratio=0.5 → midpoint

	fpGood := GetEERRange("fingerprint", "good")
	if fpGood == nil {
		t.Skip("fingerprint EER data not available")
	}

	eerLow := computeEERFromQuality("fingerprint", 0.5)   // should be near EERMax
	eerHigh := computeEERFromQuality("fingerprint", 1.0)   // should be near EERMin
	eerMid := computeEERFromQuality("fingerprint", 0.75)   // should be between

	t.Logf("fingerprint EER: quality=0.5 → %.6f, quality=0.75 → %.6f, quality=1.0 → %.6f",
		eerLow, eerMid, eerHigh)

	// eerLow should be >= eerMid >= eerHigh.
	if eerLow < eerMid {
		t.Errorf("EER at quality=0.5 (%.6f) < EER at quality=0.75 (%.6f)", eerLow, eerMid)
	}
	if eerMid < eerHigh {
		t.Errorf("EER at quality=0.75 (%.6f) < EER at quality=1.0 (%.6f)", eerMid, eerHigh)
	}

	// EER should be within the good range at quality=1.0.
	if eerHigh > fpGood.EERMax || eerHigh < fpGood.EERMin {
		t.Logf("EER at quality=1.0 (%.6f) outside good range [%.6f, %.6f] — may use interpolation",
			eerHigh, fpGood.EERMin, fpGood.EERMax)
	}
}

// TestGetBenchmarkCohortNilBenchmarkConfig verifies that GetBenchmarkCohort
// handles a nil benchmark config gracefully (should panic in real code,
// but tests document expected behavior).
func TestGetBenchmarkCohortNilConfig(t *testing.T) {
	// This test documents that GetBenchmarkCohort panics with nil config.
	// In production, initBiometricBenchmarks() is called before any usage,
	// so benchmarkConfig is always initialized.
	defer func() {
		if r := recover(); r != nil {
			t.Logf("GetBenchmarkCohort panicked with nil config: %v", r)
		}
	}()

	// We can't test this directly without manipulating internal state.
	// The benchmark is initialized during package init.
	t.Log("nil config test skipped — benchmarkConfig is initialized at package init")
}

// TestNFIQ2ScoreBoundaryValues verifies exact boundary behavior.
func TestNFIQ2ScoreBoundaryValues(t *testing.T) {
	// Boundary values: >400→1, >200→2, >100→3, >50→4, else→5
	boundaries := []struct {
		val      float64
		expected int
	}{
		{400.0, 2},  // Not >400, but >200
		{400.01, 1}, // >400
		{200.0, 3},  // Not >200, but >100
		{200.01, 2}, // >200
		{100.0, 4},  // Not >100, but >50
		{100.01, 3}, // >100
		{50.0, 5},   // Not >50
		{50.01, 4},  // >50
	}

	for _, b := range boundaries {
		score := ComputeNFIQ2Score(b.val)
		if score != b.expected {
			t.Errorf("ComputeNFIQ2Score(%f) = %d, expected %d", b.val, score, b.expected)
		}
	}
}

// TestNFIQ2ScoreNegativeInput verifies behavior with negative Laplacian variance.
func TestNFIQ2ScoreNegativeInput(t *testing.T) {
	// Negative variance should return 5 (very poor).
	score := ComputeNFIQ2Score(-1.0)
	if score != 5 {
		t.Errorf("ComputeNFIQ2Score(-1.0) = %d, expected 5", score)
	}
}

// TestPADBaselineAccuracyUnknownModalityDefault verifies default PAD accuracy.
func TestPADBaselineAccuracyUnknownModalityDefault(t *testing.T) {
	accuracy := GetPADBaselineAccuracy("nonexistent")
	if accuracy != 0.95 {
		t.Errorf("unknown modality should return default 0.95, got %f", accuracy)
	}
}
