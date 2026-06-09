// Compliance Service — independently deployable NDPR compliance service.
// Handles: Consent management, DSR requests, breach notifications, processing register.
//
// Usage:
//   go run ./cmd/compliance-svc --port=8094 --db=postgres://...
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
	"syscall"
	"time"

	"inec-go-backend/internal/compliance"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	port := flag.Int("port", 8094, "HTTP port")
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

	svc := compliance.NewService(db)

	r := mux.NewRouter()

	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"service": "compliance-svc", "status": "healthy", "version": "1.0.0",
		})
	}).Methods("GET")

	// Processing register
	r.HandleFunc("/compliance/register", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svc.GetProcessingRegister())
	}).Methods("GET")

	// Dashboard
	r.HandleFunc("/compliance/dashboard", dashboard(svc)).Methods("GET")

	// Consent
	r.HandleFunc("/compliance/consent", grantConsent(svc)).Methods("POST")
	r.HandleFunc("/compliance/consent/{nin}", listConsents(svc)).Methods("GET")
	r.HandleFunc("/compliance/consent/{nin}/withdraw", withdrawConsent(svc)).Methods("POST")

	// DSR
	r.HandleFunc("/compliance/dsr", submitDSR(svc)).Methods("POST")
	r.HandleFunc("/compliance/dsr/{nin}", listDSR(svc)).Methods("GET")

	// Breach
	r.HandleFunc("/compliance/breach", reportBreach(svc)).Methods("POST")

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Info().Int("port", *port).Msg("Compliance service starting")
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
}

func dashboard(svc *compliance.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d, err := svc.Dashboard(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(d)
	}
}

func grantConsent(svc *compliance.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SubjectNIN string `json:"subject_nin"`
			Purpose    string `json:"purpose"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		record, err := svc.GrantConsent(r.Context(), req.SubjectNIN, req.Purpose, nil)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(record)
	}
}

func listConsents(svc *compliance.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nin := mux.Vars(r)["nin"]
		records, err := svc.ListConsents(r.Context(), nin)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(records)
	}
}

func withdrawConsent(svc *compliance.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nin := mux.Vars(r)["nin"]
		var req struct {
			Purpose string `json:"purpose"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		err := svc.WithdrawConsent(r.Context(), nin, req.Purpose)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "withdrawn"})
	}
}

func submitDSR(svc *compliance.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SubjectNIN string `json:"subject_nin"`
			RightType  string `json:"right_type"`
			Details    string `json:"details"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		dsr, err := svc.SubmitDSR(r.Context(), req.SubjectNIN, req.RightType, req.Details)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(dsr)
	}
}

func listDSR(svc *compliance.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nin := mux.Vars(r)["nin"]
		requests, err := svc.ListDSRRequests(r.Context(), nin)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(requests)
	}
}

func reportBreach(svc *compliance.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Severity      string `json:"severity"`
			Description   string `json:"description"`
			AffectedCount int    `json:"affected_count"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		breach, err := svc.ReportBreach(r.Context(), req.Severity, req.Description, req.AffectedCount)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(breach)
	}
}
