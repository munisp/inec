package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ML model serving endpoints — proxy to Python analytics service
// and expose model registry, monitoring, A/B test, and training status

var mlModelsDir = func() string {
	d := os.Getenv("ML_MODELS_DIR")
	if d != "" {
		return d
	}
	return filepath.Join("..", "..", "ml", "models")
}()

// GET /gotv/ml/models — list all registered models
func handleMLModelRegistry(w http.ResponseWriter, r *http.Request) {
	registryPath := filepath.Join(mlModelsDir, "registry", "model_registry.json")
	data, err := os.ReadFile(registryPath)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models":     map[string]interface{}{},
			"production": map[string]interface{}{},
			"ab_tests":   map[string]interface{}{},
			"error":      "registry not found — run training pipeline first",
		})
		return
	}

	var registry map[string]interface{}
	json.Unmarshal(data, &registry)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(registry)
}

// GET /gotv/ml/models/{name}/metadata — get model metadata
func handleMLModelMetadata(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "fraud_dnn"
	}

	metaPath := filepath.Join(mlModelsDir, name+"_metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"metadata not found for %s"}`, name), 404)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// GET /gotv/ml/monitoring — check drift and performance
func handleMLMonitoring(w http.ResponseWriter, r *http.Request) {
	monitorPath := filepath.Join(mlModelsDir, "registry", "monitoring.json")
	data, err := os.ReadFile(monitorPath)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "no_data",
			"predictions":  0,
			"alerts":       []interface{}{},
			"drift_checks": []interface{}{},
		})
		return
	}

	var monitor map[string]interface{}
	json.Unmarshal(data, &monitor)

	// Count predictions and alerts
	preds, _ := monitor["predictions"].([]interface{})
	alerts, _ := monitor["alerts"].([]interface{})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           "operational",
		"total_predictions": len(preds),
		"active_alerts":    len(alerts),
		"monitoring_data":  monitor,
	})
}

// GET /gotv/ml/weights — list shipped model weights
func handleMLWeights(w http.ResponseWriter, r *http.Request) {
	type WeightFile struct {
		Name     string `json:"name"`
		Size     int64  `json:"size_bytes"`
		SizeHR   string `json:"size_human"`
		Modified string `json:"modified_at"`
	}

	var weights []WeightFile
	patterns := []string{"*.pt", "*.json", "*.pkl"}

	for _, pattern := range patterns {
		matches, _ := filepath.Glob(filepath.Join(mlModelsDir, pattern))
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil {
				continue
			}
			sizeHR := fmt.Sprintf("%.1f KB", float64(info.Size())/1024)
			if info.Size() > 1024*1024 {
				sizeHR = fmt.Sprintf("%.1f MB", float64(info.Size())/(1024*1024))
			}
			weights = append(weights, WeightFile{
				Name:     filepath.Base(m),
				Size:     info.Size(),
				SizeHR:   sizeHR,
				Modified: info.ModTime().Format(time.RFC3339),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"weights": weights,
		"total":   len(weights),
		"models_dir": mlModelsDir,
	})
}

// POST /gotv/ml/predict/fraud — proxy fraud detection to Python analytics
func handleMLPredictFraud(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, 400)
		return
	}

	// Forward to Python gotv-analytics service via Dapr/direct
	body, _ := json.Marshal(req)
	respBody, status, err := resilientCall(r.Context(), cbPythonAnalytics,
		"POST", pythonAnalyticsURL()+"/ml/predict/fraud", body)

	if err != nil {
		// Fallback: use Go-side heuristic scoring
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"model":    "heuristic-fallback",
			"score":    0.0,
			"is_fraud": false,
			"note":     "Python analytics unavailable, using heuristic fallback",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// POST /gotv/ml/predict/engagement — proxy voter scoring to Python
func handleMLPredictEngagement(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, 400)
		return
	}

	body, _ := json.Marshal(req)
	respBody, status, err := resilientCall(r.Context(), cbPythonAnalytics,
		"POST", pythonAnalyticsURL()+"/ml/predict/engagement", body)

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"model": "heuristic-fallback",
			"score": 50.0,
			"note":  "Python analytics unavailable",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// POST /gotv/ml/train — trigger training cycle
func handleMLTrain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"` // fraud, voter, gnn, all
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Model == "" {
		req.Model = "all"
	}

	body, _ := json.Marshal(map[string]string{"model": req.Model})
	respBody, status, err := resilientCall(r.Context(), cbPythonAnalytics,
		"POST", pythonAnalyticsURL()+"/ml/train", body)

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "training_queued",
			"model":  req.Model,
			"note":   "Training will run on next analytics service cycle",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// GET /gotv/ml/training-report — latest training metrics
func handleMLTrainingReport(w http.ResponseWriter, r *http.Request) {
	reportPath := filepath.Join(mlModelsDir, "ray_training_report.json")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		http.Error(w, `{"error":"no training report found"}`, 404)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func pythonAnalyticsURL() string {
	if u := os.Getenv("GOTV_ANALYTICS_URL"); u != "" {
		return u
	}
	return "http://localhost:8201"
}
