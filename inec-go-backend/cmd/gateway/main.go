// API Gateway — routes requests to appropriate microservices.
// In monolith mode (default), all services run in-process.
// In distributed mode (--distributed), routes to external service URLs.
//
// Service Architecture:
//   auth-svc:8090        — Authentication, JWT, MFA, sessions
//   election-svc:8091    — Election lifecycle, FSM, results, collation
//   biometric-svc:8092   — Biometric verification, template matching
//   geo-svc:8093         — Geospatial: geofencing, tracking, PostGIS
//   compliance-svc:8094  — NDPR, DSR, consent, breach register
//   ingestion-svc:8095   — Data ingestion with backpressure
//   bvas-svc:8096        — BVAS device management, accreditation
//   inference-engine:8097 — Rust ML inference
//   lakehouse:8098       — Python analytics + Apache Sedona
//   document-ai:8099     — Python OCR + document verification
//   fluvio-stream:8100   — Rust event streaming
//
// Usage:
//   go run ./cmd/gateway --port=8088
//   go run ./cmd/gateway --port=8088 --distributed
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ServiceEndpoint defines a backend service for routing.
type ServiceEndpoint struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Prefix  string `json:"prefix"`
	Lang    string `json:"language"` // go, rust, python
	Healthy bool   `json:"healthy"`
}

func main() {
	port := flag.Int("port", 8088, "Gateway port")
	distributed := flag.Bool("distributed", false, "Route to external services")

	// Go services
	authURL := flag.String("auth-url", envOr("AUTH_URL", "http://localhost:8090"), "Auth service URL")
	electionURL := flag.String("election-url", envOr("ELECTION_URL", "http://localhost:8091"), "Election service URL")
	biometricURL := flag.String("biometric-url", envOr("BIOMETRIC_URL", "http://localhost:8092"), "Biometric service URL")
	geoURL := flag.String("geo-url", envOr("GEO_URL", "http://localhost:8093"), "Geo service URL")
	complianceURL := flag.String("compliance-url", envOr("COMPLIANCE_URL", "http://localhost:8094"), "Compliance service URL")
	ingestionURL := flag.String("ingestion-url", envOr("INGESTION_URL", "http://localhost:8095"), "Ingestion service URL")
	bvasURL := flag.String("bvas-url", envOr("BVAS_URL", "http://localhost:8096"), "BVAS service URL")

	// Rust services
	inferenceURL := flag.String("inference-url", envOr("INFERENCE_URL", "http://localhost:8097"), "Rust inference engine URL")
	fluvioURL := flag.String("fluvio-url", envOr("FLUVIO_URL", "http://localhost:8100"), "Rust Fluvio stream URL")

	// GOTV service (Go)
	gotvURL := flag.String("gotv-url", envOr("GOTV_URL", "http://localhost:8103"), "GOTV voter mobilization service URL")

	// Python services
	lakehouseURL := flag.String("lakehouse-url", envOr("LAKEHOUSE_URL", "http://localhost:8098"), "Python lakehouse analytics URL")
	documentAIURL := flag.String("docai-url", envOr("DOCUMENT_AI_URL", "http://localhost:8099"), "Python document AI URL")

	// GOTV analytics (Python)
	gotvAnalyticsURL := flag.String("gotv-analytics-url", envOr("GOTV_ANALYTICS_URL", "http://localhost:8102"), "GOTV Python analytics URL")

	// GOTV engine (Rust)
	gotvEngineURL := flag.String("gotv-engine-url", envOr("GOTV_ENGINE_URL", "http://localhost:8101"), "GOTV Rust geo-matching engine URL")

	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	services := []ServiceEndpoint{
		// Go microservices
		{Name: "auth-svc", URL: *authURL, Prefix: "/auth", Lang: "go"},
		{Name: "election-svc", URL: *electionURL, Prefix: "/elections", Lang: "go"},
		{Name: "biometric-svc", URL: *biometricURL, Prefix: "/biometric", Lang: "go"},
		{Name: "geo-svc", URL: *geoURL, Prefix: "/geo", Lang: "go"},
		{Name: "compliance-svc", URL: *complianceURL, Prefix: "/compliance", Lang: "go"},
		{Name: "ingestion-svc", URL: *ingestionURL, Prefix: "/ingestion", Lang: "go"},
		{Name: "bvas-svc", URL: *bvasURL, Prefix: "/bvas", Lang: "go"},

		// Rust microservices
		{Name: "inference-engine", URL: *inferenceURL, Prefix: "/inference", Lang: "rust"},
		{Name: "fluvio-stream", URL: *fluvioURL, Prefix: "/stream", Lang: "rust"},

		// GOTV microservices
		{Name: "gotv-svc", URL: *gotvURL, Prefix: "/gotv", Lang: "go"},
		{Name: "gotv-engine", URL: *gotvEngineURL, Prefix: "/gotv-engine", Lang: "rust"},
		{Name: "gotv-analytics", URL: *gotvAnalyticsURL, Prefix: "/gotv-analytics", Lang: "python"},

		// Python microservices
		{Name: "lakehouse-analytics", URL: *lakehouseURL, Prefix: "/analytics", Lang: "python"},
		{Name: "document-ai", URL: *documentAIURL, Prefix: "/documents", Lang: "python"},
	}

	r := mux.NewRouter()
	r.Use(corsMiddleware)
	r.Use(requestIDMiddleware)

	// Gateway health — aggregates all service health
	r.HandleFunc("/health", gatewayHealth(services)).Methods("GET")
	r.HandleFunc("/services", listServices(services)).Methods("GET")
	r.HandleFunc("/architecture", architectureInfo(services)).Methods("GET")

	if *distributed {
		for _, svc := range services {
			target, err := url.Parse(svc.URL)
			if err != nil {
				log.Fatal().Err(err).Str("service", svc.Name).Msg("Invalid service URL")
			}
			proxy := httputil.NewSingleHostReverseProxy(target)
			proxy.ErrorHandler = proxyErrorHandler(svc.Name)
			r.PathPrefix(svc.Prefix).Handler(http.StripPrefix("", proxy))
			log.Info().Str("service", svc.Name).Str("url", svc.URL).Str("lang", svc.Lang).Msg("Routing to service")
		}
	} else {
		log.Info().Msg("Running in monolith mode — use --distributed for microservice routing")
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info().Int("port", *port).Bool("distributed", *distributed).
			Int("services", len(services)).Msg("API Gateway starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Gateway failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Info().Msg("API Gateway stopped")
}

func gatewayHealth(services []ServiceEndpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		results := make(map[string]interface{})
		var mu sync.Mutex
		var wg sync.WaitGroup
		allHealthy := true

		client := &http.Client{Timeout: 2 * time.Second}
		for _, svc := range services {
			wg.Add(1)
			go func(s ServiceEndpoint) {
				defer wg.Done()
				resp, err := client.Get(s.URL + "/health")
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					results[s.Name] = map[string]interface{}{"status": "unreachable", "error": err.Error()}
					allHealthy = false
					return
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				var health interface{}
				json.Unmarshal(body, &health)
				results[s.Name] = health
			}(svc)
		}
		wg.Wait()

		status := "healthy"
		if !allHealthy {
			status = "degraded"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"gateway":  status,
			"services": results,
			"total":    len(services),
		})
	}
}

func listServices(services []ServiceEndpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"services": services,
			"total":    len(services),
			"languages": map[string]int{
				"go":     7,
				"rust":   2,
				"python": 2,
			},
		})
	}
}

func architectureInfo(services []ServiceEndpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"architecture": "microservices",
			"gateway":      "api-gateway:8088",
			"services":     services,
			"communication": map[string]interface{}{
				"sync":  "HTTP/REST via gateway reverse proxy",
				"async": "Kafka (event streaming) + Fluvio (real-time)",
				"cache": "Redis (session + rate limit)",
			},
			"databases": map[string]interface{}{
				"primary":    "PostgreSQL 16 + PostGIS",
				"ledger":     "TigerBeetle",
				"search":     "OpenSearch",
				"analytics":  "DuckDB (lakehouse) + Apache Sedona",
				"cache":      "Redis",
			},
			"security": map[string]interface{}{
				"auth":    "Keycloak (OIDC) + JWT + MFA",
				"authz":   "Permify (ReBAC)",
				"waf":     "OpenAppSec",
				"gateway": "APISIX (rate limiting, circuit breaking)",
			},
		})
	}
}

func proxyErrorHandler(serviceName string) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		log.Error().Err(err).Str("service", serviceName).Str("path", r.URL.Path).Msg("Proxy error")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "service_unavailable",
			"service": serviceName,
			"message": fmt.Sprintf("Service %s is not responding", serviceName),
		})
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "http://localhost:3000"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Request-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = fmt.Sprintf("gw-%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", reqID)
		next.ServeHTTP(w, r)
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
