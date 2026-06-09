// Auth Service — independently deployable authentication and session management service.
// Handles: Login, registration, JWT lifecycle, MFA (TOTP/WebAuthn), session management.
//
// Usage:
//   go run ./cmd/auth-svc --port=8090 --db=postgres://...
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

	"inec-go-backend/internal/auth"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	port := flag.Int("port", 8090, "HTTP port")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
	jwtSecret := flag.String("jwt-secret", os.Getenv("JWT_SECRET"), "JWT signing secret")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	if *dbURL == "" {
		*dbURL = "postgres://ngapp:ngapp123@localhost:5432/ngapp?sslmode=disable"
	}
	if *jwtSecret == "" {
		log.Fatal().Msg("JWT_SECRET is required")
	}

	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)

	cfg := auth.DefaultConfig([]byte(*jwtSecret))
	svc := auth.NewService(db, cfg)
	mfaSvc := auth.NewMFAService(db, "INEC Platform")
	mfaSvc.InitTables(context.Background())

	r := mux.NewRouter()
	r.Use(corsMiddleware)

	// Health
	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"service": "auth-svc", "status": "healthy", "version": "1.0.0",
		})
	}).Methods("GET")

	// Auth endpoints
	r.HandleFunc("/auth/login", login(svc, mfaSvc)).Methods("POST")
	r.HandleFunc("/auth/register", register(svc)).Methods("POST")
	r.HandleFunc("/auth/refresh", refresh(svc)).Methods("POST")
	r.HandleFunc("/auth/me", me(svc)).Methods("GET")
	r.HandleFunc("/auth/logout", logout(svc)).Methods("POST")

	// MFA endpoints
	r.HandleFunc("/auth/mfa/setup", mfaSetup(svc, mfaSvc)).Methods("POST")
	r.HandleFunc("/auth/mfa/verify", mfaVerify(mfaSvc)).Methods("POST")
	r.HandleFunc("/auth/mfa/status", mfaStatus(svc, mfaSvc)).Methods("GET")

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Info().Int("port", *port).Msg("Auth service starting")
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
	log.Info().Msg("Auth service stopped")
}

func login(svc *auth.Service, mfaSvc *auth.MFAService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			TOTPCode string `json:"totp_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, 400)
			return
		}

		user, err := svc.Authenticate(r.Context(), req.Username, req.Password)
		if err != nil {
			http.Error(w, `{"error":"invalid credentials"}`, 401)
			return
		}

		// Check MFA
		mfaRequired, _ := mfaSvc.IsMFARequired(r.Context(), user.ID)
		if mfaRequired && req.TOTPCode == "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"mfa_required": true, "mfa_type": "totp", "user_id": user.ID,
			})
			return
		}
		if mfaRequired {
			valid, err := mfaSvc.ValidateLogin(r.Context(), user.ID, req.TOTPCode)
			if err != nil || !valid {
				http.Error(w, `{"error":"invalid TOTP code"}`, 401)
				return
			}
		}

		token, refresh, err := svc.IssueTokens(r.Context(), user)
		if err != nil {
			http.Error(w, `{"error":"token generation failed"}`, 500)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name: "inec_token", Value: token, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 3600,
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": token, "refresh_token": refresh,
			"user": user, "expires_in": 3600,
		})
	}
}

func register(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			FullName string `json:"full_name"`
			Role     string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, 400)
			return
		}
		user, err := svc.Register(r.Context(), req.Username, req.Password, req.FullName, req.Role)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(user)
	}
}

func refresh(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		pair, err := svc.RefreshToken(r.Context(), req.RefreshToken)
		if err != nil {
			http.Error(w, `{"error":"invalid refresh token"}`, 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"access_token": pair.AccessToken, "refresh_token": pair.RefreshToken,
		})
	}
}

func me(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if len(token) > 7 {
			token = token[7:]
		}
		claims, err := svc.ValidateToken(token)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(claims)
	}
}

func logout(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if len(token) > 7 {
			token = token[7:]
		}
		svc.Revoke(context.Background(), token)
		http.SetCookie(w, &http.Cookie{
			Name: "inec_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "logged out"})
	}
}

func mfaSetup(svc *auth.Service, mfaSvc *auth.MFAService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if len(token) > 7 {
			token = token[7:]
		}
		claims, err := svc.ValidateToken(token)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, 401)
			return
		}
		userID, _ := strconv.Atoi(claims.Subject)
		setup, err := mfaSvc.SetupTOTP(r.Context(), userID, claims.Username)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(setup)
	}
}

func mfaVerify(mfaSvc *auth.MFAService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserID int    `json:"user_id"`
			Code   string `json:"code"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		valid, err := mfaSvc.VerifyTOTP(r.Context(), req.UserID, req.Code)
		if err != nil || !valid {
			http.Error(w, `{"error":"invalid code"}`, 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"verified": true})
	}
}

func mfaStatus(svc *auth.Service, mfaSvc *auth.MFAService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if len(token) > 7 {
			token = token[7:]
		}
		claims, err := svc.ValidateToken(token)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, 401)
			return
		}
		userID, _ := strconv.Atoi(claims.Subject)
		status, _ := mfaSvc.GetStatus(r.Context(), userID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Placeholder for unused import
var _ = strconv.Itoa
