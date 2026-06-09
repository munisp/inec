// API Gateway — routes requests to appropriate microservices.
// In monolith mode (default), all services run in-process.
// In distributed mode (--distributed), routes to external service URLs.
//
// Usage:
//   go run ./cmd/gateway --port=8088
//   go run ./cmd/gateway --port=8088 --distributed --auth-url=http://auth:8090
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
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ServiceEndpoint defines a backend service for routing.
type ServiceEndpoint struct {
	Name    string
	URL     string
	Prefix  string
	Healthy bool
}

func main() {
	port := flag.Int("port", 8088, "Gateway port")
	distributed := flag.Bool("distributed", false, "Route to external services")
	authURL := flag.String("auth-url", "http://localhost:8090", "Auth service URL")
	electionURL := flag.String("election-url", "http://localhost:8091", "Election service URL")
	biometricURL := flag.String("biometric-url", "http://localhost:8092", "Biometric service URL")
	geoURL := flag.String("geo-url", "http://localhost:8093", "Geo service URL")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	services := []ServiceEndpoint{
		{Name: "auth", URL: *authURL, Prefix: "/auth"},
		{Name: "election", URL: *electionURL, Prefix: "/elections"},
		{Name: "biometric", URL: *biometricURL, Prefix: "/biometric"},
		{Name: "geo", URL: *geoURL, Prefix: "/geo"},
	}

	r := mux.NewRouter()

	// Gateway health — aggregates all service health
	r.HandleFunc("/health", gatewayHealth(services)).Methods("GET")
	r.HandleFunc("/services", listServices(services)).Methods("GET")

	if *distributed {
		// Distributed mode — proxy to external services
		for _, svc := range services {
			target, err := url.Parse(svc.URL)
			if err != nil {
				log.Fatal().Err(err).Str("service", svc.Name).Msg("Invalid service URL")
			}
			proxy := httputil.NewSingleHostReverseProxy(target)
			r.PathPrefix(svc.Prefix).Handler(http.StripPrefix("", proxy))
			log.Info().Str("service", svc.Name).Str("url", svc.URL).Msg("Routing to external service")
		}
	} else {
		log.Info().Msg("Running in monolith mode — use --distributed for microservice routing")
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
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
		allHealthy := true

		client := &http.Client{Timeout: 2 * time.Second}
		for _, svc := range services {
			resp, err := client.Get(svc.URL + "/health")
			if err != nil {
				results[svc.Name] = map[string]interface{}{"status": "unreachable", "error": err.Error()}
				allHealthy = false
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var health interface{}
			json.Unmarshal(body, &health)
			results[svc.Name] = health
		}

		status := "healthy"
		if !allHealthy {
			status = "degraded"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"gateway": status,
			"services": results,
		})
	}
}

func listServices(services []ServiceEndpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(services)
	}
}
