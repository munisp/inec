// Package biometric provides fingerprint/face verification, enrollment,
// liveness detection (PAD), and ABIS integration as a bounded service context.
package biometric

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/rs/zerolog/log"
)

// Modality represents a biometric capture type.
type Modality string

const (
	ModalityFingerprint Modality = "fingerprint"
	ModalityFace        Modality = "face"
	ModalityIris        Modality = "iris"
)

// VerificationResult represents the outcome of a biometric match.
type VerificationResult struct {
	Match           bool    `json:"match"`
	Score           float64 `json:"score"`
	Threshold       float64 `json:"threshold"`
	Modality        string  `json:"modality"`
	QualityScore    int     `json:"quality_score"`
	LivenessScore   float64 `json:"liveness_score"`
	LivenessPass    bool    `json:"liveness_pass"`
	ProcessingTime  int64   `json:"processing_time_ms"`
	TemplateID      string  `json:"template_id"`
}

// EnrollmentData contains biometric templates for enrollment.
type EnrollmentData struct {
	VoterVIN    string            `json:"voter_vin"`
	Templates   map[Modality][]byte `json:"templates"`
	Quality     map[Modality]int  `json:"quality_scores"`
	CapturedAt  time.Time         `json:"captured_at"`
	DeviceID    string            `json:"device_id"`
}

// Config holds biometric service configuration.
type Config struct {
	MatchThreshold    float64
	LivenessThreshold float64
	QualityMinimum    int
	VaultKey          []byte // 32 bytes for AES-256
	MaxRetries        int
}

// DefaultConfig returns production-safe biometric configuration.
func DefaultConfig(vaultKey []byte) Config {
	return Config{
		MatchThreshold:    0.75,
		LivenessThreshold: 0.85,
		QualityMinimum:    40, // NFIQ2 scale
		VaultKey:          vaultKey,
		MaxRetries:        3,
	}
}

// Service provides biometric verification operations.
type Service struct {
	db     *sql.DB
	config Config
}

// NewService creates a new biometric service.
func NewService(db *sql.DB, cfg Config) *Service {
	return &Service{db: db, config: cfg}
}

// Verify performs biometric verification of a template against stored data.
func (s *Service) Verify(ctx context.Context, voterVIN string, capturedTemplate []byte, modality Modality) (*VerificationResult, error) {
	start := time.Now()
	result := &VerificationResult{
		Modality:  string(modality),
		Threshold: s.config.MatchThreshold,
	}

	// Quality check
	quality := s.assessQuality(capturedTemplate, modality)
	result.QualityScore = quality
	if quality < s.config.QualityMinimum {
		result.ProcessingTime = time.Since(start).Milliseconds()
		return result, fmt.Errorf("quality score %d below minimum %d", quality, s.config.QualityMinimum)
	}

	// Liveness detection (PAD)
	liveness := s.detectLiveness(capturedTemplate, modality)
	result.LivenessScore = liveness
	result.LivenessPass = liveness >= s.config.LivenessThreshold
	if !result.LivenessPass {
		result.ProcessingTime = time.Since(start).Milliseconds()
		return result, fmt.Errorf("liveness check failed: %.2f < %.2f", liveness, s.config.LivenessThreshold)
	}

	// Retrieve stored template from vault
	storedTemplate, templateID, err := s.getTemplate(ctx, voterVIN, modality)
	if err != nil {
		result.ProcessingTime = time.Since(start).Milliseconds()
		return result, fmt.Errorf("template retrieval: %w", err)
	}
	result.TemplateID = templateID

	// Perform matching
	score := s.matchTemplates(capturedTemplate, storedTemplate, modality)
	result.Score = score
	result.Match = score >= s.config.MatchThreshold
	result.ProcessingTime = time.Since(start).Milliseconds()

	// Audit log
	s.logVerification(ctx, voterVIN, modality, result)

	return result, nil
}

// Enroll stores biometric templates for a voter.
func (s *Service) Enroll(ctx context.Context, data *EnrollmentData) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for modality, template := range data.Templates {
		quality, ok := data.Quality[modality]
		if !ok || quality < s.config.QualityMinimum {
			return fmt.Errorf("modality %s quality %d below minimum %d", modality, quality, s.config.QualityMinimum)
		}

		// Encrypt template with vault key
		encrypted, err := s.encryptTemplate(template)
		if err != nil {
			return fmt.Errorf("encrypt template: %w", err)
		}

		templateID := fmt.Sprintf("BIO-%s-%s-%d", data.VoterVIN, modality, time.Now().UnixNano())
		templateHash := sha256.Sum256(template)

		_, err = tx.ExecContext(ctx,
			`INSERT INTO biometric_templates (voter_vin, modality, template_id, encrypted_template,
			 template_hash, quality_score, device_id, captured_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 ON CONFLICT (voter_vin, modality) DO UPDATE
			 SET encrypted_template = $4, template_hash = $5, quality_score = $6, captured_at = $8`,
			data.VoterVIN, modality, templateID, encrypted,
			hex.EncodeToString(templateHash[:]), quality, data.DeviceID, data.CapturedAt)
		if err != nil {
			return fmt.Errorf("store template: %w", err)
		}
	}

	return tx.Commit()
}

// DuplicateCheck performs 1:N matching against the database (ABIS).
func (s *Service) DuplicateCheck(ctx context.Context, template []byte, modality Modality, excludeVIN string) ([]string, error) {
	// In a real ABIS, this would be a specialized matching server
	// Here we implement basic 1:N with configurable batch size
	rows, err := s.db.QueryContext(ctx,
		`SELECT voter_vin, encrypted_template FROM biometric_templates
		 WHERE modality = $1 AND voter_vin != $2 LIMIT 10000`,
		modality, excludeVIN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var duplicates []string
	for rows.Next() {
		var vin string
		var encrypted []byte
		if err := rows.Scan(&vin, &encrypted); err != nil {
			continue
		}
		stored, err := s.decryptTemplate(encrypted)
		if err != nil {
			continue
		}
		score := s.matchTemplates(template, stored, modality)
		if score >= s.config.MatchThreshold {
			duplicates = append(duplicates, vin)
		}
	}
	return duplicates, nil
}

// getTemplate retrieves and decrypts a stored template.
func (s *Service) getTemplate(ctx context.Context, voterVIN string, modality Modality) ([]byte, string, error) {
	var encrypted []byte
	var templateID string
	err := s.db.QueryRowContext(ctx,
		`SELECT encrypted_template, template_id FROM biometric_templates
		 WHERE voter_vin = $1 AND modality = $2`,
		voterVIN, modality).Scan(&encrypted, &templateID)
	if err == sql.ErrNoRows {
		return nil, "", fmt.Errorf("no template found for %s/%s", voterVIN, modality)
	}
	if err != nil {
		return nil, "", err
	}

	template, err := s.decryptTemplate(encrypted)
	if err != nil {
		return nil, "", fmt.Errorf("decrypt: %w", err)
	}
	return template, templateID, nil
}

// encryptTemplate uses AES-256-GCM to encrypt a biometric template.
func (s *Service) encryptTemplate(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.config.VaultKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptTemplate uses AES-256-GCM to decrypt a biometric template.
func (s *Service) decryptTemplate(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.config.VaultKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// matchTemplates computes similarity between two templates.
// In production, this delegates to a specialized matching algorithm (e.g., SourceAFIS, Neurotechnology).
func (s *Service) matchTemplates(captured, stored []byte, modality Modality) float64 {
	if len(captured) == 0 || len(stored) == 0 {
		return 0
	}

	// Normalized cross-correlation (simplified — real impl uses minutiae extraction)
	minLen := len(captured)
	if len(stored) < minLen {
		minLen = len(stored)
	}

	var dotProduct, normA, normB float64
	for i := 0; i < minLen; i++ {
		a := float64(captured[i])
		b := float64(stored[i])
		dotProduct += a * b
		normA += a * a
		normB += b * b
	}

	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// assessQuality computes a quality score for a captured template (NFIQ2-like).
func (s *Service) assessQuality(template []byte, modality Modality) int {
	if len(template) < 64 {
		return 0
	}
	// Simplified quality assessment based on entropy and structure
	var sum, sumSq float64
	n := float64(len(template))
	for _, b := range template {
		v := float64(b)
		sum += v
		sumSq += v * v
	}
	mean := sum / n
	variance := (sumSq / n) - (mean * mean)
	stddev := math.Sqrt(variance)

	// Map stddev to quality score (higher variance = better quality for fingerprints)
	quality := int(math.Min(100, stddev*1.5))
	if quality < 0 {
		quality = 0
	}
	return quality
}

// detectLiveness performs Presentation Attack Detection (PAD).
func (s *Service) detectLiveness(template []byte, modality Modality) float64 {
	if len(template) < 32 {
		return 0
	}
	// Simplified PAD score — real implementation uses ML models (CDCN, FAS)
	// Check for statistical patterns indicative of printed/screen attacks
	var entropy float64
	histogram := make(map[byte]int)
	for _, b := range template {
		histogram[b]++
	}
	n := float64(len(template))
	for _, count := range histogram {
		p := float64(count) / n
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	// Normalize entropy (max for byte is 8 bits)
	normalizedEntropy := entropy / 8.0

	// Live captures typically have higher entropy than printed/screen attacks
	return math.Min(1.0, normalizedEntropy*1.2)
}

// logVerification records a verification attempt for audit.
func (s *Service) logVerification(ctx context.Context, voterVIN string, modality Modality, result *VerificationResult) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO biometric_verifications (voter_vin, modality, match_result, score,
		 quality_score, liveness_score, processing_time_ms, verified_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		voterVIN, modality, result.Match, result.Score,
		result.QualityScore, result.LivenessScore, result.ProcessingTime)
	if err != nil {
		log.Error().Err(err).Str("voter_vin", voterVIN).Msg("Failed to log verification")
	}
}
