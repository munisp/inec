// INEC ABIS Pipeline Service (Go).
//
// ALL STATE PERSISTED TO POSTGRESQL — zero in-memory storage.
//
// Production service providing:
// - 6-stage enrollment pipeline (capture → quality → extract → dedup → vault → complete)
// - BVAS device management and protocol
// - LSH-based deduplication (PostgreSQL-backed)
// - Score normalization (Z-score, min-max)
// - Full audit trail (PostgreSQL-backed)

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	pipeline       *ABISPipeline
	deviceRegistry *BVASDeviceRegistry
	pgStore        *PGStore
)

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Connect to PostgreSQL — required, no fallback to in-memory
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgresql://ngapp:ngapp123@localhost:5432/ngapp"
	}

	ctx := context.Background()
	var err error
	pgStore, err = NewPGStore(ctx, dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to PostgreSQL — ABIS requires persistent storage")
	}
	defer pgStore.Close()

	log.Info().Msg("PostgreSQL connected — all state persisted to database")

	pythonURL := os.Getenv("BIOMETRIC_PYTHON_URL")
	if pythonURL == "" {
		pythonURL = "http://localhost:8090"
	}
	rustURL := os.Getenv("BIOMETRIC_RUST_URL")
	if rustURL == "" {
		rustURL = "http://localhost:8091"
	}

	pipeline = NewABISPipeline(pythonURL, rustURL, pgStore)
	deviceRegistry = NewBVASDeviceRegistry(pgStore)

	r := mux.NewRouter()

	// Health
	r.HandleFunc("/health", handleHealth).Methods("GET")

	// ABIS Pipeline
	r.HandleFunc("/abis/enroll", handleEnroll).Methods("POST")
	r.HandleFunc("/abis/gallery/stats", handleGalleryStats).Methods("GET")
	r.HandleFunc("/abis/audit", handleAudit).Methods("GET")

	// BVAS Device Management
	r.HandleFunc("/bvas/devices", handleListDevices).Methods("GET")
	r.HandleFunc("/bvas/devices/register", handleRegisterDevice).Methods("POST")
	r.HandleFunc("/bvas/devices/{id}", handleGetDevice).Methods("GET")
	r.HandleFunc("/bvas/devices/{id}/heartbeat", handleDeviceHeartbeat).Methods("POST")
	r.HandleFunc("/bvas/devices/{id}/capture/start", handleStartCapture).Methods("POST")
	r.HandleFunc("/bvas/captures/{session_id}/complete", handleCompleteCapture).Methods("POST")
	r.HandleFunc("/bvas/stats", handleDeviceStats).Methods("GET")

	// Score Normalization
	r.HandleFunc("/normalize/zscore", handleZScoreNorm).Methods("POST")
	r.HandleFunc("/normalize/minmax", handleMinMaxNorm).Methods("POST")

	addr := ":8092"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	log.Info().Str("addr", addr).Str("python", pythonURL).Str("rust", rustURL).Msg("ABIS pipeline service starting (PostgreSQL persistence)")
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	writeJSON(w, 200, map[string]interface{}{
		"status":      "healthy",
		"service":     "inec-abis-pipeline",
		"persistence": "postgresql",
		"capabilities": []string{
			"enrollment_pipeline", "bvas_protocol", "deduplication",
			"quality_gateway", "score_normalization", "audit",
		},
		"gallery_size": pgStore.GallerySize(ctx),
	})
}

func handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req EnrollmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.VoterVIN == "" || req.Modality == "" {
		writeJSON(w, 400, map[string]interface{}{"error": "voter_vin and modality are required"})
		return
	}

	if len(req.ImageData) == 0 {
		writeJSON(w, 400, map[string]interface{}{"error": "image_data is required"})
		return
	}

	result := pipeline.Enroll(r.Context(), req)

	status := 200
	if result.Stage == StageFailed {
		status = 400
	}

	writeJSON(w, status, result)
}

func handleGalleryStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	writeJSON(w, 200, map[string]interface{}{
		"gallery_size":    pgStore.GallerySize(ctx),
		"dedup_threshold": pipeline.dedupEngine.threshold,
		"quality_thresholds": map[string]float64{
			"fingerprint": pipeline.qualityGate.GetThreshold("fingerprint"),
			"face":        pipeline.qualityGate.GetThreshold("face"),
			"iris":        pipeline.qualityGate.GetThreshold("iris"),
		},
	})
}

func handleAudit(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	entries := pgStore.GetRecentAudit(r.Context(), limit)
	writeJSON(w, 200, map[string]interface{}{
		"entries": entries,
	})
}

func handleListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := pgStore.ListDevices(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"devices": devices,
		"count":   len(devices),
	})
}

func handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	var device BVASDevice
	if err := json.NewDecoder(r.Body).Decode(&device); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": "invalid JSON"})
		return
	}

	if err := deviceRegistry.RegisterDevice(device); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, 201, map[string]interface{}{
		"device_id": device.DeviceID,
		"status":    "registered",
	})
}

func handleGetDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := mux.Vars(r)["id"]
	d, err := deviceRegistry.GetDevice(deviceID)
	if err != nil {
		writeJSON(w, 404, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, 200, d)
}

func handleDeviceHeartbeat(w http.ResponseWriter, r *http.Request) {
	deviceID := mux.Vars(r)["id"]

	var loc *DeviceLocation
	var body struct {
		Location *DeviceLocation `json:"location"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
		loc = body.Location
	}

	if err := deviceRegistry.Heartbeat(deviceID, loc); err != nil {
		writeJSON(w, 404, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"status": "ok", "timestamp": time.Now()})
}

func handleStartCapture(w http.ResponseWriter, r *http.Request) {
	deviceID := mux.Vars(r)["id"]
	var req struct {
		VoterVIN string `json:"voter_vin"`
		Modality string `json:"modality"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": "invalid JSON"})
		return
	}

	session, err := deviceRegistry.StartCapture(deviceID, req.VoterVIN, req.Modality)
	if err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, 201, session)
}

func handleCompleteCapture(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["session_id"]
	var req struct {
		Quality float64 `json:"quality"`
		NFIQ2   int     `json:"nfiq2_score"`
		Width   int     `json:"width"`
		Height  int     `json:"height"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": "invalid JSON"})
		return
	}

	if err := deviceRegistry.CompleteCapture(sessionID, req.Quality, req.NFIQ2, req.Width, req.Height); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"status": "ok"})
}

func handleDeviceStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, deviceRegistry.GetStats())
}

func handleZScoreNorm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Score  float64 `json:"score"`
		Mean   float64 `json:"mean"`
		StdDev float64 `json:"std_dev"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": "invalid JSON"})
		return
	}

	normalized := ZScoreNormalize(req.Score, ScoreNormParams{
		Mean: req.Mean, StdDev: req.StdDev,
	})
	writeJSON(w, 200, map[string]interface{}{
		"original":   req.Score,
		"normalized": normalized,
		"method":     "z_score",
	})
}

func handleMinMaxNorm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Score float64 `json:"score"`
		Min   float64 `json:"min"`
		Max   float64 `json:"max"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": "invalid JSON"})
		return
	}

	normalized := MinMaxNormalize(req.Score, ScoreNormParams{
		Min: req.Min, Max: req.Max,
	})
	writeJSON(w, 200, map[string]interface{}{
		"original":   req.Score,
		"normalized": normalized,
		"method":     "min_max",
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
