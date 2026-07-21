// Biometric Service — independently deployable service for real biometric operations.
// It requires PostgreSQL persistence, a 256-bit vault key, and the CPU inference
// service for presentation-attack detection. It does not generate sample data or
// return fabricated biometric results.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"inec-go-backend/internal/biometric"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const defaultLivenessThreshold = 0.85

func main() {
	port := flag.Int("port", 8092, "HTTP port")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	if strings.TrimSpace(*dbURL) == "" {
		log.Fatal().Msg("DATABASE_URL or --db is required")
	}
	inferenceURL := strings.TrimRight(strings.TrimSpace(os.Getenv("INFERENCE_ENGINE_URL")), "/")
	if inferenceURL == "" {
		log.Fatal().Msg("INFERENCE_ENGINE_URL is required")
	}
	vaultKey, err := decodeVaultKey(os.Getenv("VAULT_KEY"))
	if err != nil {
		log.Fatal().Err(err).Msg("invalid VAULT_KEY")
	}

	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("open PostgreSQL connection")
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	pingCtx, cancelPing := context.WithTimeout(context.Background(), 10*time.Second)
	if err := db.PingContext(pingCtx); err != nil {
		cancelPing()
		log.Fatal().Err(err).Msg("connect to PostgreSQL")
	}
	cancelPing()

	cfg := biometric.DefaultConfig(vaultKey)
	svc := biometric.NewService(db, cfg)

	r := mux.NewRouter()
	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"service": "biometric-svc", "status": "healthy", "version": "1.0.0",
		})
	}).Methods(http.MethodGet)
	r.HandleFunc("/biometric/verify", verify(svc)).Methods(http.MethodPost)
	r.HandleFunc("/biometric/enroll", enroll(svc)).Methods(http.MethodPost)
	r.HandleFunc("/biometric/liveness", liveness(inferenceURL)).Methods(http.MethodPost)
	r.HandleFunc("/biometric/stats", stats(db)).Methods(http.MethodGet)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Info().Int("port", *port).Msg("biometric service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("biometric service failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("biometric service shutdown")
	}
	log.Info().Msg("biometric service stopped")
}

func decodeVaultKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("VAULT_KEY is required and must be a 64-character hex-encoded AES-256 key")
	}
	key, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("VAULT_KEY must be hex encoded: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("VAULT_KEY must decode to exactly 32 bytes")
	}
	return key, nil
}

func parseModality(value string) (biometric.Modality, error) {
	modality := biometric.Modality(strings.ToLower(strings.TrimSpace(value)))
	switch modality {
	case biometric.ModalityFingerprint, biometric.ModalityFace, biometric.ModalityIris:
		return modality, nil
	default:
		return "", fmt.Errorf("unsupported biometric modality %q", value)
	}
}

func decodeCapture(value, field string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("%s is required", field)
	}
	if _, data, ok := strings.Cut(value, ","); ok {
		value = data
	}
	capture, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(capture) == 0 {
		return nil, fmt.Errorf("%s must be non-empty base64 data", field)
	}
	return capture, nil
}

func verify(svc *biometric.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			VIN      string `json:"vin"`
			Modality string `json:"modality"`
			Template string `json:"template"`
			DeviceID string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON request")
			return
		}
		if strings.TrimSpace(req.VIN) == "" || strings.TrimSpace(req.DeviceID) == "" {
			writeError(w, http.StatusBadRequest, "vin and device_id are required")
			return
		}
		modality, err := parseModality(req.Modality)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		template, err := decodeCapture(req.Template, "template")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := svc.Verify(r.Context(), req.VIN, template, modality)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func enroll(svc *biometric.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			VIN      string `json:"vin"`
			Modality string `json:"modality"`
			Template string `json:"template"`
			Quality  int    `json:"quality"`
			DeviceID string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON request")
			return
		}
		if strings.TrimSpace(req.VIN) == "" || strings.TrimSpace(req.DeviceID) == "" {
			writeError(w, http.StatusBadRequest, "vin and device_id are required")
			return
		}
		modality, err := parseModality(req.Modality)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		template, err := decodeCapture(req.Template, "template")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Quality < 40 || req.Quality > 100 {
			writeError(w, http.StatusBadRequest, "quality must be an externally measured score from 40 through 100")
			return
		}
		data := &biometric.EnrollmentData{
			VoterVIN:   req.VIN,
			Templates:  map[biometric.Modality][]byte{modality: template},
			Quality:    map[biometric.Modality]int{modality: req.Quality},
			DeviceID:   req.DeviceID,
			CapturedAt: time.Now().UTC(),
		}
		if err := svc.Enroll(r.Context(), data); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "enrolled", "vin": req.VIN})
	}
}

func liveness(inferenceURL string) http.HandlerFunc {
	client := &http.Client{Timeout: 15 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Image    string `json:"image"`
			Modality string `json:"modality"`
			DeviceID string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON request")
			return
		}
		if strings.TrimSpace(req.DeviceID) == "" {
			writeError(w, http.StatusBadRequest, "device_id is required")
			return
		}
		if _, err := parseModality(req.Modality); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := decodeCapture(req.Image, "image"); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		payload, err := json.Marshal(map[string]interface{}{
			"image_base64": req.Image,
			"threshold":    defaultLivenessThreshold,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "encode liveness inference request")
			return
		}
		upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, inferenceURL+"/liveness/predict", bytes.NewReader(payload))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "create liveness inference request")
			return
		}
		upstreamReq.Header.Set("Content-Type", "application/json")
		upstreamResp, err := client.Do(upstreamReq)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "liveness inference service unavailable")
			return
		}
		defer upstreamResp.Body.Close()
		if upstreamResp.StatusCode != http.StatusOK {
			writeError(w, http.StatusServiceUnavailable, "liveness inference rejected the capture")
			return
		}
		var result struct {
			LivenessScore   float64 `json:"liveness_score"`
			LivenessPass    bool    `json:"liveness_pass"`
			Threshold       float64 `json:"threshold"`
			Model           string  `json:"model"`
			InferenceTimeUS uint64  `json:"inference_time_us"`
		}
		if err := json.NewDecoder(upstreamResp.Body).Decode(&result); err != nil {
			writeError(w, http.StatusBadGateway, "invalid liveness inference response")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"liveness_pass":     result.LivenessPass,
			"score":             result.LivenessScore,
			"threshold":         result.Threshold,
			"model":             result.Model,
			"inference_time_us": result.InferenceTimeUS,
			"modality":          req.Modality,
			"device_id":         req.DeviceID,
		})
	}
}

func stats(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var enrollments, verifications int64
		var matchRate, averageLatency float64
		err := db.QueryRowContext(ctx, `
			SELECT
				(SELECT COUNT(*) FROM biometric_templates),
				(SELECT COUNT(*) FROM biometric_verifications),
				COALESCE((SELECT AVG(CASE WHEN match_result THEN 100.0 ELSE 0.0 END) FROM biometric_verifications), 0),
				COALESCE((SELECT AVG(processing_time_ms) FROM biometric_verifications), 0)
		`).Scan(&enrollments, &verifications, &matchRate, &averageLatency)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query biometric statistics: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enrollments":    enrollments,
			"verifications":  verifications,
			"match_rate_pct": matchRate,
			"avg_latency_ms": averageLatency,
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
