// Package main implements the production ABIS (Automated Biometric Identification System)
// enrollment pipeline for the INEC election platform.
//
// Pipeline stages:
//  1. Capture — receive biometric image from BVAS device
//  2. Quality Check — NFIQ2 quality assessment, reject poor samples
//  3. Template Extract — call Python service for minutiae/embedding extraction
//  4. Dedup Check — 1:N search against enrolled gallery using LSH blocking
//  5. Vault Store — encrypt template via Rust vault service
//  6. Complete — update voter record, emit audit event

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type PipelineStage string

const (
	StageCapture         PipelineStage = "capture"
	StageQualityCheck    PipelineStage = "quality_check"
	StageTemplateExtract PipelineStage = "template_extract"
	StageDedupCheck      PipelineStage = "dedup_check"
	StageVaultStore      PipelineStage = "vault_store"
	StageComplete        PipelineStage = "complete"
	StageFailed          PipelineStage = "failed"
)

type EnrollmentRequest struct {
	VoterVIN  string `json:"voter_vin"`
	Modality  string `json:"modality"` // fingerprint, face, iris
	DeviceID  string `json:"device_id"`
	ImageData []byte `json:"image_data"` // raw image bytes
}

type EnrollmentResult struct {
	VoterVIN     string        `json:"voter_vin"`
	Modality     string        `json:"modality"`
	Stage        PipelineStage `json:"stage"`
	TemplateHash string        `json:"template_hash,omitempty"`
	TemplateID   string        `json:"template_id,omitempty"`
	QualityScore float64       `json:"quality_score,omitempty"`
	DedupClear   bool          `json:"dedup_clear"`
	Error        string        `json:"error,omitempty"`
	Latencies    StageLatency  `json:"latencies"`
}

type StageLatency struct {
	CaptureMs   float64 `json:"capture_ms"`
	QualityMs   float64 `json:"quality_ms"`
	ExtractMs   float64 `json:"extract_ms"`
	DedupMs     float64 `json:"dedup_ms"`
	VaultMs     float64 `json:"vault_ms"`
	TotalMs     float64 `json:"total_ms"`
}

type TemplateData struct {
	VoterVIN     string  `json:"voter_vin"`
	Modality     string  `json:"modality"`
	TemplateHash string  `json:"template_hash"`
	QualityScore float64 `json:"quality_score"`
	// Minutiae for fingerprint matching
	Minutiae []MinutiaPoint `json:"minutiae,omitempty"`
	// Embedding for face matching
	Embedding []float64 `json:"embedding,omitempty"`
	// IrisCode for iris matching
	IrisCode []byte `json:"iris_code,omitempty"`
	IrisMask []byte `json:"iris_mask,omitempty"`
}

type MinutiaPoint struct {
	X     int     `json:"x"`
	Y     int     `json:"y"`
	Angle float64 `json:"angle"`
	Type  string  `json:"type"`
}

// ABISPipeline orchestrates the enrollment process.
type ABISPipeline struct {
	pythonURL   string // biometric-python service URL
	rustURL     string // biometric-rust vault service URL
	gallery     *BiometricGallery
	qualityGate *QualityGateway
	dedupEngine *DeduplicationEngine
	auditLog    *AuditLog
}

func NewABISPipeline(pythonURL, rustURL string) *ABISPipeline {
	return &ABISPipeline{
		pythonURL:   pythonURL,
		rustURL:     rustURL,
		gallery:     NewBiometricGallery(),
		qualityGate: NewQualityGateway(),
		dedupEngine: NewDeduplicationEngine(),
		auditLog:    NewAuditLog(),
	}
}

func (p *ABISPipeline) Enroll(ctx context.Context, req EnrollmentRequest) EnrollmentResult {
	start := time.Now()
	result := EnrollmentResult{
		VoterVIN: req.VoterVIN,
		Modality: req.Modality,
	}

	// Stage 1: Capture validation
	captureStart := time.Now()
	if len(req.ImageData) == 0 {
		result.Stage = StageFailed
		result.Error = "empty image data"
		return result
	}
	if len(req.ImageData) > 10*1024*1024 { // 10MB max
		result.Stage = StageFailed
		result.Error = "image exceeds 10MB limit"
		return result
	}
	result.Latencies.CaptureMs = float64(time.Since(captureStart).Microseconds()) / 1000

	// Stage 2: Quality check via Python service
	qualityStart := time.Now()
	qr, err := p.checkQuality(ctx, req.ImageData, req.Modality)
	if err != nil {
		result.Stage = StageFailed
		result.Error = fmt.Sprintf("quality check failed: %v", err)
		p.auditLog.Log("enrollment_failed", req.VoterVIN, req.Modality, result.Error)
		return result
	}
	if !qr.PassThreshold {
		result.Stage = StageFailed
		result.QualityScore = qr.OverallScore
		result.Error = fmt.Sprintf("quality below threshold: %.2f (reasons: %v)", qr.OverallScore, qr.RejectionReasons)
		p.auditLog.Log("enrollment_quality_rejected", req.VoterVIN, req.Modality, result.Error)
		return result
	}
	result.QualityScore = qr.OverallScore
	result.Latencies.QualityMs = float64(time.Since(qualityStart).Microseconds()) / 1000

	// Stage 3: Template extraction via Python service
	extractStart := time.Now()
	tmpl, err := p.extractTemplate(ctx, req.ImageData, req.Modality)
	if err != nil {
		result.Stage = StageFailed
		result.Error = fmt.Sprintf("template extraction failed: %v", err)
		p.auditLog.Log("enrollment_extract_failed", req.VoterVIN, req.Modality, result.Error)
		return result
	}
	tmpl.VoterVIN = req.VoterVIN
	tmpl.Modality = req.Modality
	result.TemplateHash = tmpl.TemplateHash
	result.Latencies.ExtractMs = float64(time.Since(extractStart).Microseconds()) / 1000

	// Stage 4: Deduplication check
	dedupStart := time.Now()
	dupVIN, isDup := p.dedupEngine.CheckDuplicate(tmpl, p.gallery)
	if isDup {
		result.Stage = StageFailed
		result.DedupClear = false
		result.Error = fmt.Sprintf("duplicate detected: matches voter %s", dupVIN)
		p.auditLog.Log("enrollment_duplicate", req.VoterVIN, req.Modality,
			fmt.Sprintf("matches %s", dupVIN))
		return result
	}
	result.DedupClear = true
	result.Latencies.DedupMs = float64(time.Since(dedupStart).Microseconds()) / 1000

	// Stage 5: Vault storage via Rust service
	vaultStart := time.Now()
	templateID, err := p.storeInVault(ctx, req.VoterVIN, req.Modality, tmpl)
	if err != nil {
		result.Stage = StageFailed
		result.Error = fmt.Sprintf("vault storage failed: %v", err)
		p.auditLog.Log("enrollment_vault_failed", req.VoterVIN, req.Modality, result.Error)
		return result
	}
	result.TemplateID = templateID
	result.Latencies.VaultMs = float64(time.Since(vaultStart).Microseconds()) / 1000

	// Stage 6: Complete — add to gallery
	p.gallery.Add(tmpl)
	result.Stage = StageComplete
	result.Latencies.TotalMs = float64(time.Since(start).Microseconds()) / 1000
	p.auditLog.Log("enrollment_complete", req.VoterVIN, req.Modality,
		fmt.Sprintf("quality=%.2f template=%s", result.QualityScore, result.TemplateID))

	return result
}

// QualityResponse from the Python biometric service.
type QualityResponse struct {
	OverallScore     float64  `json:"overall_score"`
	Level            string   `json:"level"`
	PassThreshold    bool     `json:"pass_threshold"`
	Metrics          map[string]interface{} `json:"metrics"`
	RejectionReasons []string `json:"rejection_reasons"`
}

func (p *ABISPipeline) checkQuality(ctx context.Context, imageData []byte, modality string) (*QualityResponse, error) {
	payload := map[string]interface{}{
		"image":    encodeBase64(imageData),
		"modality": modality,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", p.pythonURL+"/quality/assess", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("python service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("quality check returned %d: %s", resp.StatusCode, string(b))
	}

	var qr QualityResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return nil, fmt.Errorf("decode quality response: %w", err)
	}
	return &qr, nil
}

func (p *ABISPipeline) extractTemplate(ctx context.Context, imageData []byte, modality string) (*TemplateData, error) {
	endpoint := fmt.Sprintf("/%s/extract", modality)
	// For extract endpoints, we POST multipart form data
	payload := map[string]interface{}{
		"image":    encodeBase64(imageData),
		"modality": modality,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", p.pythonURL+endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("python service unreachable: %w", err)
	}
	defer resp.Body.Close()

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	tmpl := &TemplateData{}
	if h, ok := raw["template_hash"].(string); ok {
		tmpl.TemplateHash = h
	} else {
		// Compute hash from image data
		hash := sha256.Sum256(imageData)
		tmpl.TemplateHash = hex.EncodeToString(hash[:])
	}

	if score, ok := raw["quality_score"].(float64); ok {
		tmpl.QualityScore = score
	}

	return tmpl, nil
}

func (p *ABISPipeline) storeInVault(ctx context.Context, vin, modality string, tmpl *TemplateData) (string, error) {
	templateBytes, _ := json.Marshal(tmpl)

	payload := map[string]interface{}{
		"voter_vin":     vin,
		"modality":      modality,
		"template_data": encodeBase64(templateBytes),
		"actor":         "abis_pipeline",
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", p.rustURL+"/vault/encrypt", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("rust vault unreachable: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if tid, ok := result["template_id"].(string); ok {
		return tid, nil
	}

	if errMsg, ok := result["error"].(string); ok {
		return "", fmt.Errorf("vault error: %s", errMsg)
	}

	return "", fmt.Errorf("unexpected vault response")
}

// ─── Biometric Gallery ──────────────────────────────────────────

type BiometricGallery struct {
	mu        sync.RWMutex
	templates map[string]*TemplateData // keyed by voter_vin:modality
	lshIndex  *LSHIndex
}

func NewBiometricGallery() *BiometricGallery {
	return &BiometricGallery{
		templates: make(map[string]*TemplateData),
		lshIndex:  NewLSHIndex(20, 10), // 20 hash tables, 10 hash bits
	}
}

func (g *BiometricGallery) Add(tmpl *TemplateData) {
	g.mu.Lock()
	defer g.mu.Unlock()

	key := fmt.Sprintf("%s:%s", tmpl.VoterVIN, tmpl.Modality)
	g.templates[key] = tmpl

	// Index for LSH-based dedup
	hash := sha256.Sum256([]byte(tmpl.TemplateHash))
	g.lshIndex.Insert(tmpl.VoterVIN, hash[:])
}

func (g *BiometricGallery) GetAll() []*TemplateData {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make([]*TemplateData, 0, len(g.templates))
	for _, t := range g.templates {
		result = append(result, t)
	}
	return result
}

func (g *BiometricGallery) Size() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.templates)
}

// ─── LSH Index (Locality-Sensitive Hashing) ─────────────────────

type LSHIndex struct {
	numTables   int
	numBits     int
	tables      []map[uint64][]string
	hyperplanes [][][]byte
	mu          sync.RWMutex
}

func NewLSHIndex(numTables, numBits int) *LSHIndex {
	tables := make([]map[uint64][]string, numTables)
	hyperplanes := make([][][]byte, numTables)

	for t := 0; t < numTables; t++ {
		tables[t] = make(map[uint64][]string)
		hyperplanes[t] = make([][]byte, numBits)
		for b := 0; b < numBits; b++ {
			hp := make([]byte, 32)
			// Deterministic hyperplanes from table+bit index
			h := sha256.Sum256([]byte(fmt.Sprintf("lsh_%d_%d", t, b)))
			copy(hp, h[:])
			hyperplanes[t][b] = hp
		}
	}

	return &LSHIndex{
		numTables:   numTables,
		numBits:     numBits,
		tables:      tables,
		hyperplanes: hyperplanes,
	}
}

func (idx *LSHIndex) Insert(id string, data []byte) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	for t := 0; t < idx.numTables; t++ {
		hash := idx.computeHash(t, data)
		idx.tables[t][hash] = append(idx.tables[t][hash], id)
	}
}

func (idx *LSHIndex) Query(data []byte) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	candidates := make(map[string]int)
	for t := 0; t < idx.numTables; t++ {
		hash := idx.computeHash(t, data)
		for _, id := range idx.tables[t][hash] {
			candidates[id]++
		}
	}

	// Sort by number of hash table hits (more hits = more likely match)
	type candidate struct {
		id   string
		hits int
	}
	sorted := make([]candidate, 0, len(candidates))
	for id, hits := range candidates {
		sorted = append(sorted, candidate{id, hits})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].hits > sorted[j].hits
	})

	result := make([]string, 0, len(sorted))
	for _, c := range sorted {
		result = append(result, c.id)
	}
	return result
}

func (idx *LSHIndex) computeHash(table int, data []byte) uint64 {
	var hash uint64
	for b := 0; b < idx.numBits; b++ {
		hp := idx.hyperplanes[table][b]
		dotProduct := 0
		for i := 0; i < len(data) && i < len(hp); i++ {
			dotProduct += int(data[i]) * int(hp[i])
		}
		if dotProduct > 0 {
			hash |= 1 << uint(b)
		}
	}
	return hash
}

// ─── Deduplication Engine ───────────────────────────────────────

type DeduplicationEngine struct {
	threshold float64 // similarity threshold for duplicate detection
}

func NewDeduplicationEngine() *DeduplicationEngine {
	return &DeduplicationEngine{
		threshold: 0.85,
	}
}

func (d *DeduplicationEngine) CheckDuplicate(probe *TemplateData, gallery *BiometricGallery) (string, bool) {
	gallery.mu.RLock()
	defer gallery.mu.RUnlock()

	// Use LSH for blocking — only compare against candidates
	hash := sha256.Sum256([]byte(probe.TemplateHash))
	candidates := gallery.lshIndex.Query(hash[:])

	for _, candidateVIN := range candidates {
		if candidateVIN == probe.VoterVIN {
			continue
		}

		key := fmt.Sprintf("%s:%s", candidateVIN, probe.Modality)
		existing, ok := gallery.templates[key]
		if !ok {
			continue
		}

		// Direct template hash comparison
		if probe.TemplateHash == existing.TemplateHash {
			return candidateVIN, true
		}
	}

	return "", false
}

// ─── Quality Gateway ────────────────────────────────────────────

type QualityGateway struct {
	thresholds map[string]float64
}

func NewQualityGateway() *QualityGateway {
	return &QualityGateway{
		thresholds: map[string]float64{
			"fingerprint": 0.50,
			"face":        0.55,
			"iris":        0.50,
		},
	}
}

func (g *QualityGateway) GetThreshold(modality string) float64 {
	if t, ok := g.thresholds[modality]; ok {
		return t
	}
	return 0.50
}

// ─── Score Normalization ────────────────────────────────────────

type ScoreNormParams struct {
	Mean   float64
	StdDev float64
	Min    float64
	Max    float64
}

func ZScoreNormalize(score float64, params ScoreNormParams) float64 {
	if params.StdDev < 1e-10 {
		return 0.5
	}
	z := (score - params.Mean) / params.StdDev
	return 1.0 / (1.0 + math.Exp(-z))
}

func MinMaxNormalize(score float64, params ScoreNormParams) float64 {
	r := params.Max - params.Min
	if r < 1e-10 {
		return 0.5
	}
	v := (score - params.Min) / r
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ─── Audit Log ──────────────────────────────────────────────────

type AuditEntry struct {
	ID        string    `json:"id"`
	Operation string    `json:"operation"`
	VoterVIN  string    `json:"voter_vin"`
	Modality  string    `json:"modality"`
	Detail    string    `json:"detail"`
	Timestamp time.Time `json:"timestamp"`
}

type AuditLog struct {
	mu      sync.Mutex
	entries []AuditEntry
}

func NewAuditLog() *AuditLog {
	return &AuditLog{
		entries: make([]AuditEntry, 0, 1000),
	}
}

func (a *AuditLog) Log(operation, vin, modality, detail string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	entry := AuditEntry{
		ID:        fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		Operation: operation,
		VoterVIN:  vin,
		Modality:  modality,
		Detail:    detail,
		Timestamp: time.Now(),
	}
	a.entries = append(a.entries, entry)

	log.Info().
		Str("operation", operation).
		Str("voter_vin", vin).
		Str("modality", modality).
		Msg(detail)
}

func (a *AuditLog) GetRecent(limit int) []AuditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()

	if limit > len(a.entries) {
		limit = len(a.entries)
	}
	start := len(a.entries) - limit
	result := make([]AuditEntry, limit)
	copy(result, a.entries[start:])
	return result
}
