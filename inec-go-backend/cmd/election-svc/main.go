// Election Service — independently deployable service for election lifecycle management.
// Handles: FSM transitions, result submission/validation, collation, real-time updates.
//
// Usage:
//   go run ./cmd/election-svc --port=8091 --db=postgres://...
//
// Or via Docker:
//   docker run inec/election-svc:latest
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

	"inec-go-backend/internal/authmw"
	"inec-go-backend/internal/election"
	"inec-go-backend/internal/eventbus"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	port := flag.Int("port", 8091, "HTTP port")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	if *dbURL == "" {
		log.Fatal().Msg("DATABASE_URL environment variable is required")
	}

	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	svc := election.NewService(db)
	bus := eventbus.NewLocal()

	r := mux.NewRouter()
	r.Use(authmw.CORS())
	r.Use(requestIDMiddleware)
	// JWT authentication on all routes except /health.
	r.Use(authmw.Middleware("/health"))

	// Health — pings the database, 503 when unreachable
	r.HandleFunc("/health", authmw.HealthHandler(db, "election-svc")).Methods("GET")

	// Elections CRUD
	r.HandleFunc("/elections", listElections(svc)).Methods("GET")
	r.HandleFunc("/elections/{id:[0-9]+}", getElection(svc)).Methods("GET")
	r.HandleFunc("/elections/{id:[0-9]+}/transition", transitionElection(svc, bus)).Methods("POST")
	r.HandleFunc("/elections/{id:[0-9]+}/stats", electionStats(svc)).Methods("GET")

	// Results
	r.HandleFunc("/elections/{id:[0-9]+}/results", listResults(svc)).Methods("GET")
	r.HandleFunc("/elections/{id:[0-9]+}/collation", collation(svc)).Methods("GET")

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info().Int("port", *port).Msg("Election service starting")
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
	log.Info().Msg("Election service stopped")
}

func listElections(svc *election.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		elections, err := svc.List(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(elections)
	}
}

func getElection(svc *election.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(mux.Vars(r)["id"])
		e, err := svc.Get(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(e)
	}
}

func transitionElection(svc *election.Service, bus *eventbus.LocalBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only admins and collation officers may drive election FSM transitions.
		claims, ok := authmw.RequireRole(w, r, "admin", "collation_officer")
		if !ok {
			return
		}
		// The actor is derived from verified JWT claims — NEVER from the body.
		userID := authmw.UserID(claims)
		if userID == 0 {
			http.Error(w, "invalid user in token", 401)
			return
		}
		id, _ := strconv.Atoi(mux.Vars(r)["id"])
		var req struct {
			TargetState string `json:"target_state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", 400)
			return
		}
		if err := svc.Transition(r.Context(), id, election.State(req.TargetState), userID); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		bus.Publish(r.Context(), eventbus.Event{
			Type: "election.transitioned",
			Source: "election-svc",
			Data: map[string]interface{}{"election_id": id, "new_state": req.TargetState},
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "new_state": req.TargetState})
	}
}

func electionStats(svc *election.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(mux.Vars(r)["id"])
		stats, err := svc.Stats(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}

func listResults(svc *election.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(mux.Vars(r)["id"])
		results, err := svc.ListResults(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func collation(svc *election.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(mux.Vars(r)["id"])
		summary, err := svc.Collate(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(summary)
	}
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Service", "election-svc")
		next.ServeHTTP(w, r)
	})
}
