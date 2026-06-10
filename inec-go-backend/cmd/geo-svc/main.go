// Geo Service — independently deployable geospatial and mapping service.
// Handles: Geofencing, PostGIS queries, polling unit mapping, landmarks, heatmaps.
//
// Usage:
//   go run ./cmd/geo-svc --port=8093 --db=postgres://...
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"inec-go-backend/internal/geo"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	port := flag.Int("port", 8093, "HTTP port")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	if *dbURL == "" {
		*dbURL = "postgres://ngapp:ngapp123@localhost:5432/ngapp?sslmode=disable"
	}

	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	svc := geo.NewService(db)

	r := mux.NewRouter()

	// Health
	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"service": "geo-svc", "status": "healthy", "version": "1.0.0",
		})
	}).Methods("GET")

	// Geofence
	r.HandleFunc("/geofence/check", geofenceCheck(svc)).Methods("POST")
	r.HandleFunc("/geofence/validate", geofenceValidate(svc)).Methods("POST")

	// Polling units
	r.HandleFunc("/geo/polling-units", listPollingUnits(svc)).Methods("GET")
	r.HandleFunc("/geo/polling-units/{code}", getPollingUnit(svc)).Methods("GET")
	r.HandleFunc("/geo/nearby", nearbyPUs(svc)).Methods("GET")

	// Officials tracking
	r.HandleFunc("/geo/officials", listOfficials(svc)).Methods("GET")
	r.HandleFunc("/geo/officials/track", trackOfficial(svc)).Methods("POST")

	// Analytics
	r.HandleFunc("/geo/heatmap", heatmap(svc)).Methods("GET")
	r.HandleFunc("/geo/clusters", clusters(svc)).Methods("GET")

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Info().Int("port", *port).Msg("Geo service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Info().Msg("Geo service stopped")
}

func geofenceCheck(svc *geo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Lat             float64 `json:"lat"`
			Lng             float64 `json:"lng"`
			PollingUnitCode string  `json:"polling_unit_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, 400)
			return
		}
		result, err := svc.CheckGeofence(r.Context(), req.Lat, req.Lng, req.PollingUnitCode)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func geofenceValidate(svc *geo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Lat             float64 `json:"lat"`
			Lng             float64 `json:"lng"`
			PollingUnitCode string  `json:"polling_unit_code"`
			DeviceID        string  `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, 400)
			return
		}
		result, err := svc.ValidateGeofence(r.Context(), req.Lat, req.Lng, req.PollingUnitCode)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func listPollingUnits(svc *geo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pus, err := svc.ListPollingUnits(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("lga"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pus)
	}
}

func getPollingUnit(svc *geo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := mux.Vars(r)["code"]
		pu, err := svc.GetPollingUnit(r.Context(), code)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pu)
	}
}

func nearbyPUs(svc *geo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
		lng, _ := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
		radius, _ := strconv.ParseFloat(r.URL.Query().Get("radius"), 64)
		if radius == 0 {
			radius = 5000
		}
		pus, err := svc.NearbyPollingUnits(r.Context(), lat, lng, radius)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pus)
	}
}

func listOfficials(svc *geo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		officials, err := svc.ListOfficials(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(officials)
	}
}

func trackOfficial(svc *geo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			StaffID string  `json:"staff_id"`
			Lat     float64 `json:"lat"`
			Lng     float64 `json:"lng"`
			Battery int     `json:"battery"`
			Speed   float64 `json:"speed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, 400)
			return
		}
		err := svc.UpdateOfficialLocationSimple(r.Context(), req.StaffID, req.Lat, req.Lng, req.Battery, req.Speed)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "tracked"})
	}
}

func heatmap(svc *geo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := svc.Heatmap(r.Context(), r.URL.Query().Get("metric"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}
}

func clusters(svc *geo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := svc.Clusters(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}
}
