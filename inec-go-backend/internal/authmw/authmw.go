// Package authmw provides shared HTTP middleware for the INEC microservices
// and API gateway: HS256 JWT authentication, configurable CORS, and
// database-backed health checks. It centralizes the security-critical logic
// so every service enforces the same authentication, origin allow-list, and
// readiness semantics as the monolith.
package authmw

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
)

type contextKey string

const claimsContextKey contextKey = "authmw_claims"

// Secret returns the HS256 JWT signing secret from JWT_SECRET.
//
// SECURITY: in any environment other than an explicit INEC_ENV=development,
// a missing or short (<32 chars) secret is a fatal startup error — the
// service must never boot with an empty/ephemeral HMAC key in production.
// An ephemeral random key is generated ONLY for local development.
func Secret() []byte {
	s := os.Getenv("JWT_SECRET")
	dev := os.Getenv("INEC_ENV") == "development"
	if s == "" {
		if dev {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				log.Fatal().Err(err).Msg("failed to generate ephemeral JWT secret")
			}
			log.Warn().Msg("JWT_SECRET not set — using ephemeral key (INEC_ENV=development only)")
			return []byte(base64.RawURLEncoding.EncodeToString(b))
		}
		log.Fatal().Msg("JWT_SECRET environment variable is required (set INEC_ENV=development to allow ephemeral keys)")
	}
	if len(s) < 32 && !dev {
		log.Fatal().Msg("JWT_SECRET must be at least 32 characters")
	}
	return []byte(s)
}

// Validate parses and validates an HS256 JWT, returning its claims.
func Validate(secret []byte, tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, fmt.Errorf("invalid token")
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Middleware returns JWT authentication middleware. Every request must carry
// a valid Bearer access token signed with JWT_SECRET, except requests whose
// path exactly matches one of publicPaths (e.g. "/health"). Refresh tokens
// (type != "access") are rejected.
func Middleware(publicPaths ...string) func(http.Handler) http.Handler {
	secret := Secret()
	public := make(map[string]bool, len(publicPaths))
	for _, p := range publicPaths {
		public[p] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if public[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				writeUnauthorized(w, "authentication required")
				return
			}
			claims, err := Validate(secret, strings.TrimPrefix(auth, "Bearer "))
			if err != nil {
				writeUnauthorized(w, "invalid or expired token")
				return
			}
			// Refresh tokens must never be usable as access tokens.
			if t, _ := claims["type"].(string); t != "access" {
				writeUnauthorized(w, "access token required")
				return
			}
			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Claims extracts verified JWT claims from the request context.
func Claims(r *http.Request) (jwt.MapClaims, bool) {
	claims, ok := r.Context().Value(claimsContextKey).(jwt.MapClaims)
	return claims, ok
}

// UserID extracts the numeric user id from the verified JWT claims (sub).
// It never trusts caller-supplied body fields.
func UserID(claims jwt.MapClaims) int {
	switch sub := claims["sub"].(type) {
	case string:
		id, _ := strconv.Atoi(sub)
		return id
	case float64:
		return int(sub)
	}
	return 0
}

// RequireRole writes a 401/403 and returns ok=false unless the request
// carries verified claims with one of the allowed roles.
func RequireRole(w http.ResponseWriter, r *http.Request, roles ...string) (jwt.MapClaims, bool) {
	claims, ok := Claims(r)
	if !ok {
		writeUnauthorized(w, "authentication required")
		return nil, false
	}
	role, _ := claims["role"].(string)
	for _, allowed := range roles {
		if role == allowed {
			return claims, true
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]string{"error": "insufficient permissions"})
	return nil, false
}

// CORS returns middleware implementing a strict origin allow-list driven by
// the CORS_ORIGINS environment variable (comma-separated).
//
// SECURITY:
//   - When CORS_ORIGINS is unset the allow-list is EMPTY (deny all cross-origin).
//   - APP_ENV=production with an empty allow-list is a fatal startup error.
//   - Access-Control-Allow-Credentials:true is NEVER sent with a wildcard origin.
func CORS() func(http.Handler) http.Handler {
	raw := strings.TrimSpace(os.Getenv("CORS_ORIGINS"))
	var origins []string
	wildcard := false
	if raw == "" {
		if os.Getenv("APP_ENV") == "production" {
			log.Fatal().Msg("CORS_ORIGINS must be set in production (explicit origin allow-list required)")
		}
		log.Warn().Msg("CORS_ORIGINS not set — cross-origin requests will be denied")
	} else {
		for _, o := range strings.Split(raw, ",") {
			o = strings.TrimSpace(o)
			if o == "*" {
				wildcard = true
				continue
			}
			if o != "" {
				origins = append(origins, o)
			}
		}
	}
	if wildcard {
		log.Warn().Msg("CORS allow-list contains wildcard — credentials will not be allowed")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				matched := false
				for _, o := range origins {
					if o == origin {
						matched = true
						break
					}
				}
				switch {
				case matched:
					// Explicitly listed origin: credentials permitted.
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Vary", "Origin")
				case wildcard:
					// Wildcard dev mode: reflect via "*" and NEVER allow credentials.
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, X-CSRF-Token")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if r.Method == "OPTIONS" {
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HealthHandler returns a health/readiness endpoint that pings the database
// with a 2s timeout and reports 503 when the database is unreachable,
// mirroring the monolith's ReadinessHandler.
func HealthHandler(db *sql.DB, service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		status := "healthy"
		code := http.StatusOK
		if db == nil {
			status, code = "unhealthy", http.StatusServiceUnavailable
		} else if err := db.PingContext(ctx); err != nil {
			status, code = "unhealthy", http.StatusServiceUnavailable
			log.Error().Err(err).Str("service", service).Msg("health check: database ping failed")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"service": service,
			"status":  status,
			"version": "1.0.0",
		})
	}
}
