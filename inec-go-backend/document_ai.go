package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ── Document AI Integration ──
// Calls the Python Document AI service for PaddleOCR, VLM, DocLing analysis.

var documentAIURL = getEnvDefault("DOCUMENT_AI_URL", "http://localhost:8089")

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ── OCR Analysis Types ──

type OCRRegion struct {
	Text       string     `json:"text"`
	Confidence float64    `json:"confidence"`
	BBox       [][]int    `json:"bbox"`
}

type EC8AExtraction struct {
	SerialNumber       *string  `json:"serial_number"`
	PollingUnitCode    *string  `json:"polling_unit_code"`
	PollingUnitName    *string  `json:"polling_unit_name"`
	Ward               *string  `json:"ward"`
	LGA                *string  `json:"lga"`
	State              *string  `json:"state"`
	ElectionType       *string  `json:"election_type"`
	PartyResults       []M      `json:"party_results"`
	TotalValidVotes    *int     `json:"total_valid_votes"`
	TotalRejectedVotes *int     `json:"total_rejected_votes"`
	TotalVotesCast     *int     `json:"total_votes_cast"`
	AccreditedVoters   *int     `json:"accredited_voters"`
	RegisteredVoters   *int     `json:"registered_voters"`
	PresidingOfficer   *string  `json:"presiding_officer_name"`
	RawOCRText         string   `json:"raw_ocr_text"`
	ConfidenceScore    float64  `json:"confidence_score"`
	Warnings           []string `json:"extraction_warnings"`
}

type VLMAnalysis struct {
	IsValidEC8A       bool     `json:"is_valid_ec8a"`
	TamperingDetected bool     `json:"tampering_detected"`
	TamperingConf     float64  `json:"tampering_confidence"`
	Indicators        []string `json:"tampering_indicators"`
	DocumentQuality   string   `json:"document_quality"`
	OrientationOK     bool     `json:"orientation_correct"`
	Completeness      float64  `json:"completeness_score"`
	Summary           string   `json:"analysis_summary"`
}

type DocLingTable struct {
	Headers    []string `json:"headers"`
	Rows       []M      `json:"rows"`
	Confidence float64  `json:"confidence"`
}

type FullPhotoAnalysis struct {
	ReportID         *int           `json:"report_id"`
	OCR              EC8AExtraction `json:"ocr"`
	VLM              VLMAnalysis    `json:"vlm"`
	DocLing          M              `json:"docling"`
	CombinedConf     float64        `json:"combined_confidence"`
	RequiresReview   bool           `json:"requires_manual_review"`
	Timestamp        string         `json:"timestamp"`
}

// ── Video Analysis Types ──

type VideoAnalysis struct {
	DurationSec     float64 `json:"duration_seconds"`
	FrameCount      int     `json:"frame_count"`
	FPS             float64 `json:"fps"`
	Resolution      M       `json:"resolution"`
	KeyFrames       int     `json:"key_frames_extracted"`
	Anomalies       []M     `json:"anomalies_detected"`
	BallotEvents    []M     `json:"ballot_counting_events"`
	IntegrityScore  float64 `json:"integrity_score"`
	Summary         string  `json:"analysis_summary"`
}

// ── KYC Types ──

type KYCResult struct {
	UserID         int      `json:"user_id"`
	Status         string   `json:"status"`
	IdentityScore  float64  `json:"identity_match_score"`
	DocVerified    bool     `json:"document_verified"`
	FaceMatchScore float64  `json:"face_match_score"`
	LivenessPassed bool     `json:"liveness_passed"`
	RiskScore      float64  `json:"risk_score"`
	Checks         []string `json:"checks_performed"`
	Flags          []string `json:"flags"`
	Timestamp      string   `json:"verification_timestamp"`
}

type LivenessResult struct {
	UserID       int     `json:"user_id"`
	Passed       bool    `json:"passed"`
	Confidence   float64 `json:"confidence"`
	Method       string  `json:"method"`
	AntiSpoof    float64 `json:"anti_spoofing_score"`
	Checks       []M     `json:"checks"`
	Timestamp    string  `json:"timestamp"`
}

// ── Database Schema ──

func initDocumentAISchema() {
	// Add kyc_status column to users if not present
	db.Exec("ALTER TABLE users ADD COLUMN kyc_status TEXT DEFAULT 'not_started'")

	schema := `
	CREATE TABLE IF NOT EXISTS document_analyses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		report_id INTEGER REFERENCES observer_reports(id),
		analysis_type TEXT NOT NULL DEFAULT 'full',
		ocr_confidence REAL,
		vlm_tampering_detected INTEGER DEFAULT 0,
		vlm_quality TEXT,
		combined_confidence REAL,
		requires_review INTEGER DEFAULT 0,
		party_results_json TEXT,
		raw_ocr_text TEXT,
		warnings_json TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS video_analyses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		report_id INTEGER,
		observer_id INTEGER,
		filename TEXT,
		duration_sec REAL,
		frame_count INTEGER,
		anomaly_count INTEGER,
		ballot_event_count INTEGER,
		integrity_score REAL,
		summary TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS kyc_verifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		id_type TEXT,
		id_number_hash TEXT,
		identity_match_score REAL,
		document_verified INTEGER DEFAULT 0,
		face_match_score REAL,
		liveness_passed INTEGER DEFAULT 0,
		risk_score REAL,
		checks_json TEXT,
		flags_json TEXT,
		verified_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS liveness_checks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		passed INTEGER DEFAULT 0,
		confidence REAL,
		method TEXT,
		anti_spoofing_score REAL,
		checks_json TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	db.Exec(schema)
}

// ── Handlers ──

// handleAnalyzePhoto triggers AI analysis on an uploaded observer report photo.
func handleAnalyzePhoto(w http.ResponseWriter, r *http.Request) {
	reportIDStr := r.URL.Query().Get("report_id")
	if reportIDStr == "" {
		writeError(w, 400, "report_id query param required")
		return
	}
	reportID, _ := strconv.Atoi(reportIDStr)

	// Get the photo URL from the report
	var photoURL string
	row := db.QueryRow("SELECT photo_url FROM observer_reports WHERE id=?", reportID)
	if err := row.Scan(&photoURL); err != nil {
		writeError(w, 404, "report not found")
		return
	}

	// Read the file from disk
	filePath := strings.TrimPrefix(photoURL, "/")
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		writeError(w, 500, "cannot read photo file: "+err.Error())
		return
	}

	// Call Document AI service
	analysis, err := callDocumentAIAnalyze(fileBytes, filepath.Base(filePath), reportID)
	if err != nil {
		// Fallback: store pending analysis record
		db.Exec("INSERT INTO document_analyses (report_id, analysis_type, requires_review) VALUES (?, 'pending', 1)", reportID)
		writeJSON(w, 202, M{
			"report_id": reportID,
			"status":    "queued",
			"message":   "Document AI service unavailable, queued for later analysis",
			"error":     err.Error(),
		})
		return
	}

	// Persist analysis results
	partyJSON, _ := json.Marshal(analysis.OCR.PartyResults)
	warningsJSON, _ := json.Marshal(analysis.OCR.Warnings)
	db.Exec(`INSERT INTO document_analyses 
		(report_id, analysis_type, ocr_confidence, vlm_tampering_detected, vlm_quality, combined_confidence, requires_review, party_results_json, raw_ocr_text, warnings_json) 
		VALUES (?, 'full', ?, ?, ?, ?, ?, ?, ?, ?)`,
		reportID, analysis.OCR.ConfidenceScore,
		docAIBoolToInt(analysis.VLM.TamperingDetected), analysis.VLM.DocumentQuality,
		analysis.CombinedConf, docAIBoolToInt(analysis.RequiresReview),
		string(partyJSON), analysis.OCR.RawOCRText, string(warningsJSON))

	// Update report status based on analysis
	if analysis.VLM.TamperingDetected {
		db.Exec("UPDATE observer_reports SET status='flagged' WHERE id=?", reportID)
	} else if analysis.CombinedConf > 0.7 {
		db.Exec("UPDATE observer_reports SET status='verified' WHERE id=?", reportID)
	}

	writeJSON(w, 200, analysis)
}

func docAIBoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// handleUploadVideo handles video upload from observers for ballot counting verification.
func handleUploadVideo(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardAuth(w, r)
	if !ok {
		return
	}
	userIDStr, _ := claims["sub"].(string)
	userID, _ := strconv.Atoi(userIDStr)

	// Parse multipart form (max 500MB for video)
	if err := r.ParseMultipartForm(500 << 20); err != nil {
		writeError(w, 400, "file too large (max 500MB)")
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		writeError(w, 400, "video field required")
		return
	}
	defer file.Close()

	// Validate video type
	ext := strings.ToLower(filepath.Ext(header.Filename))
	validExts := map[string]bool{".mp4": true, ".mov": true, ".avi": true, ".webm": true, ".mkv": true}
	if !validExts[ext] {
		writeError(w, 400, "invalid video type (allowed: mp4, mov, avi, webm, mkv)")
		return
	}

	// Save video to disk
	uploadDir := filepath.Join("uploads", "observer-videos")
	os.MkdirAll(uploadDir, 0755)
	filename := fmt.Sprintf("%d_%d%s", userID, time.Now().UnixNano(), ext)
	filePath := filepath.Join(uploadDir, filename)

	dst, err := os.Create(filePath)
	if err != nil {
		writeError(w, 500, "failed to save video")
		return
	}

	videoBytes, err := io.ReadAll(file)
	if err != nil {
		dst.Close()
		writeError(w, 500, "failed to read video")
		return
	}
	dst.Write(videoBytes)
	dst.Close()

	// Call Document AI video analysis
	analysis, err := callVideoAnalyze(videoBytes, filename)
	if err != nil {
		// Save without analysis
		db.Exec("INSERT INTO video_analyses (observer_id, filename, duration_sec, integrity_score, summary) VALUES (?, ?, 0, 0, ?)",
			userID, filename, "Analysis pending: "+err.Error())
		writeJSON(w, 201, M{
			"video_url": "/uploads/observer-videos/" + filename,
			"status":    "uploaded_pending_analysis",
			"message":   "Video saved, AI analysis queued",
		})
		return
	}

	// Persist analysis
	db.Exec(`INSERT INTO video_analyses 
		(observer_id, filename, duration_sec, frame_count, anomaly_count, ballot_event_count, integrity_score, summary) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, filename, analysis.DurationSec, analysis.FrameCount,
		len(analysis.Anomalies), len(analysis.BallotEvents),
		analysis.IntegrityScore, analysis.Summary)

	writeJSON(w, 201, M{
		"video_url":       "/uploads/observer-videos/" + filename,
		"analysis":        analysis,
		"status":          "analyzed",
	})
}

// handleKYCVerify performs identity verification for a platform user.
func handleKYCVerify(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeError(w, 400, "form data too large (max 20MB)")
		return
	}

	userIDStr := r.FormValue("user_id")
	fullName := r.FormValue("full_name")
	idType := r.FormValue("id_type")
	idNumber := r.FormValue("id_number")
	dob := r.FormValue("date_of_birth")
	phone := r.FormValue("phone_number")

	if userIDStr == "" || fullName == "" || idType == "" || idNumber == "" {
		writeError(w, 400, "user_id, full_name, id_type, id_number required")
		return
	}

	userID, _ := strconv.Atoi(userIDStr)

	// Validate ID type
	validTypes := map[string]bool{"nin": true, "voters_card": true, "passport": true, "drivers_license": true}
	if !validTypes[idType] {
		writeError(w, 400, "invalid id_type (allowed: nin, voters_card, passport, drivers_license)")
		return
	}

	// Read uploaded files
	var idDocBytes, selfieBytes []byte
	if idDoc, _, err := r.FormFile("id_document"); err == nil {
		idDocBytes, _ = io.ReadAll(idDoc)
		idDoc.Close()
	}
	if selfie, _, err := r.FormFile("selfie"); err == nil {
		selfieBytes, _ = io.ReadAll(selfie)
		selfie.Close()
	}

	// Call Document AI KYC endpoint
	result, err := callKYCVerify(userID, fullName, idType, idNumber, dob, phone, idDocBytes, selfieBytes)
	if err != nil {
		// Fallback: local validation only
		result = &KYCResult{
			UserID:    userID,
			Status:    "pending_review",
			Checks:    []string{"id_format_validation"},
			Flags:     []string{"AI service unavailable"},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
	}

	// Persist to DB
	checksJSON, _ := json.Marshal(result.Checks)
	flagsJSON, _ := json.Marshal(result.Flags)
	idHash := fmt.Sprintf("%x", []byte(idNumber))
	if len(idHash) > 32 {
		idHash = idHash[:32]
	}

	db.Exec(`INSERT INTO kyc_verifications 
		(user_id, status, id_type, id_number_hash, identity_match_score, document_verified, face_match_score, liveness_passed, risk_score, checks_json, flags_json, verified_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, result.Status, idType, idHash,
		result.IdentityScore, docAIBoolToInt(result.DocVerified),
		result.FaceMatchScore, docAIBoolToInt(result.LivenessPassed),
		result.RiskScore, string(checksJSON), string(flagsJSON),
		time.Now().UTC())

	// Update user KYC status
	db.Exec("UPDATE users SET kyc_status=? WHERE id=?", result.Status, userID)

	writeJSON(w, 200, result)
}

// handleLivenessCheck performs liveness detection from a video.
func handleLivenessCheck(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeError(w, 400, "form too large (max 50MB)")
		return
	}

	userIDStr := r.FormValue("user_id")
	method := r.FormValue("method")
	if userIDStr == "" {
		writeError(w, 400, "user_id required")
		return
	}
	userID, _ := strconv.Atoi(userIDStr)
	if method == "" {
		method = "passive"
	}

	videoFile, _, err := r.FormFile("video")
	if err != nil {
		writeError(w, 400, "video field required")
		return
	}
	defer videoFile.Close()
	videoBytes, _ := io.ReadAll(videoFile)

	// Call Document AI liveness endpoint
	result, err := callLivenessCheck(userID, method, videoBytes)
	if err != nil {
		result = &LivenessResult{
			UserID:     userID,
			Passed:     false,
			Confidence: 0,
			Method:     method,
			AntiSpoof:  0,
			Checks:     []M{{"name": "service_unavailable", "passed": false, "note": err.Error()}},
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
		}
	}

	// Persist
	lchecksJSON, _ := json.Marshal(result.Checks)
	db.Exec(`INSERT INTO liveness_checks (user_id, passed, confidence, method, anti_spoofing_score, checks_json) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, docAIBoolToInt(result.Passed), result.Confidence, method, result.AntiSpoof, string(lchecksJSON))

	// Update KYC if liveness passes
	if result.Passed {
		db.Exec("UPDATE kyc_verifications SET liveness_passed=1 WHERE user_id=? ORDER BY id DESC LIMIT 1", userID)
	}

	writeJSON(w, 200, result)
}

// handleKYCStatus returns the current KYC status for a user.
func handleKYCStatus(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.URL.Query().Get("user_id")
	if userIDStr == "" {
		writeError(w, 400, "user_id query param required")
		return
	}
	userID, _ := strconv.Atoi(userIDStr)

	var status, idType, checksJSON, flagsJSON string
	var identityScore, faceScore, riskScore float64
	var docVerified, livenessPassed int
	var verifiedAt *string

	err := db.QueryRow(`SELECT status, id_type, identity_match_score, document_verified, face_match_score, liveness_passed, risk_score, checks_json, flags_json, verified_at 
		FROM kyc_verifications WHERE user_id=? ORDER BY id DESC LIMIT 1`, userID).Scan(
		&status, &idType, &identityScore, &docVerified, &faceScore, &livenessPassed, &riskScore, &checksJSON, &flagsJSON, &verifiedAt)

	if err != nil {
		writeJSON(w, 200, M{
			"user_id":    userID,
			"status":     "not_started",
			"message":    "No KYC verification found for this user",
		})
		return
	}

	var checks []string
	var flags []string
	json.Unmarshal([]byte(checksJSON), &checks)
	json.Unmarshal([]byte(flagsJSON), &flags)

	writeJSON(w, 200, M{
		"user_id":              userID,
		"status":              status,
		"id_type":             idType,
		"identity_match_score": identityScore,
		"document_verified":    docVerified == 1,
		"face_match_score":     faceScore,
		"liveness_passed":      livenessPassed == 1,
		"risk_score":          riskScore,
		"checks_performed":     checks,
		"flags":               flags,
		"verified_at":         verifiedAt,
	})
}

// handleDocumentAnalysisStatus returns analysis results for a report.
func handleDocumentAnalysisStatus(w http.ResponseWriter, r *http.Request) {
	reportIDStr := r.URL.Query().Get("report_id")
	if reportIDStr == "" {
		writeError(w, 400, "report_id query param required")
		return
	}
	reportID, _ := strconv.Atoi(reportIDStr)

	var analysisType, vlmQuality, partyJSON, warningsJSON string
	var ocrConf, combinedConf float64
	var tamperingDetected, requiresReview int
	var rawText string

	err := db.QueryRow(`SELECT analysis_type, ocr_confidence, vlm_tampering_detected, vlm_quality, combined_confidence, requires_review, party_results_json, raw_ocr_text, warnings_json 
		FROM document_analyses WHERE report_id=? ORDER BY id DESC LIMIT 1`, reportID).Scan(
		&analysisType, &ocrConf, &tamperingDetected, &vlmQuality, &combinedConf, &requiresReview, &partyJSON, &rawText, &warningsJSON)

	if err != nil {
		writeJSON(w, 200, M{
			"report_id": reportID,
			"status":    "not_analyzed",
		})
		return
	}

	var partyResults []M
	var warnings []string
	json.Unmarshal([]byte(partyJSON), &partyResults)
	json.Unmarshal([]byte(warningsJSON), &warnings)

	writeJSON(w, 200, M{
		"report_id":            reportID,
		"analysis_type":        analysisType,
		"ocr_confidence":       ocrConf,
		"tampering_detected":   tamperingDetected == 1,
		"document_quality":     vlmQuality,
		"combined_confidence":  combinedConf,
		"requires_review":      requiresReview == 1,
		"party_results":        partyResults,
		"warnings":             warnings,
	})
}

// ── HTTP Helpers for Document AI Service ──

var docAIClient = NewResilientHTTPClient("document-ai")

func callDocumentAIAnalyze(fileBytes []byte, filename string, reportID int) (*FullPhotoAnalysis, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	part.Write(fileBytes)
	writer.WriteField("report_id", strconv.Itoa(reportID))
	writer.Close()

	req, _ := http.NewRequest("POST", documentAIURL+"/analyze/photo-report", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := docAIClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("document AI service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("document AI returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result FullPhotoAnalysis
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func callVideoAnalyze(videoBytes []byte, filename string) (*VideoAnalysis, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	part.Write(videoBytes)
	writer.Close()

	req, _ := http.NewRequest("POST", documentAIURL+"/video/analyze", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := docAIClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("video analysis service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("video analysis returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result VideoAnalysis
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func callKYCVerify(userID int, fullName, idType, idNumber, dob, phone string, idDocBytes, selfieBytes []byte) (*KYCResult, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("user_id", strconv.Itoa(userID))
	writer.WriteField("full_name", fullName)
	writer.WriteField("id_type", idType)
	writer.WriteField("id_number", idNumber)
	if dob != "" {
		writer.WriteField("date_of_birth", dob)
	}
	if phone != "" {
		writer.WriteField("phone_number", phone)
	}
	if idDocBytes != nil {
		part, _ := writer.CreateFormFile("id_document", "id_document.jpg")
		part.Write(idDocBytes)
	}
	if selfieBytes != nil {
		part, _ := writer.CreateFormFile("selfie", "selfie.jpg")
		part.Write(selfieBytes)
	}
	writer.Close()

	req, _ := http.NewRequest("POST", documentAIURL+"/kyc/verify", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := docAIClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("KYC service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("KYC service returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result KYCResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func callLivenessCheck(userID int, method string, videoBytes []byte) (*LivenessResult, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("user_id", strconv.Itoa(userID))
	writer.WriteField("method", method)
	part, err := writer.CreateFormFile("video", "liveness.mp4")
	if err != nil {
		return nil, err
	}
	part.Write(videoBytes)
	writer.Close()

	req, _ := http.NewRequest("POST", documentAIURL+"/kyc/liveness", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := docAIClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("liveness service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("liveness service returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result LivenessResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}


