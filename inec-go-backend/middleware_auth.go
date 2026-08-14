package main

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
)

type contextKey string

const userContextKey contextKey = "user"

// publicPaths are endpoints that do not require authentication.
var publicPaths = map[string]bool{
	"/healthz":                          true,
	"/readiness":                        true,
	"/auth/login":                       true,
	"/auth/register":                    true,
	"/metrics":                          true,
	"/auth/refresh":                     true,
	"/observer/stream":                  true, // SSE authenticates in-handler via authenticateStreamRequest (HttpOnly cookie; EventSource can't set headers)
	"/.well-known/openid-configuration": true,
}

// publicPrefixes are path prefixes accessible without auth.
var publicPrefixes = []string{
	"/public/",
	"/api/v1/docs",
}

func isPublicPath(path string) bool {
	if publicPaths[path] {
		return true
	}
	for _, prefix := range publicPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// jwtAuthMiddleware enforces JWT authentication on all routes except public ones.
func jwtAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Check Authorization header first, then fall back to httpOnly cookie
		auth := r.Header.Get("Authorization")
		var tokenStr string
		if auth != "" && strings.HasPrefix(auth, "Bearer ") {
			tokenStr = strings.TrimPrefix(auth, "Bearer ")
		} else if cookie, err := r.Cookie("inec_token"); err == nil && cookie.Value != "" {
			tokenStr = cookie.Value
		} else {
			writeJSON(w, 401, M{"error": "authentication required"})
			return
		}
		claims, err := decodeToken(tokenStr)
		if err != nil {
			writeJSON(w, 401, M{"error": "invalid or expired token"})
			return
		}

		// Refresh tokens must never authenticate API requests.
		if tokenType, _ := claims["type"].(string); tokenType != "access" {
			writeJSON(w, 401, M{"error": "access token required"})
			return
		}

		// Reject revoked tokens (logout / session revocation by jti).
		if jti, _ := claims["jti"].(string); jti != "" && blacklist.isBlacklisted(jti) {
			writeJSON(w, 401, M{"error": "token has been revoked"})
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// getUserFromContext extracts JWT claims from request context.
func getUserFromContext(r *http.Request) (jwt.MapClaims, bool) {
	claims, ok := r.Context().Value(userContextKey).(jwt.MapClaims)
	return claims, ok
}

// streamDevMode reports whether the dev-only ?token= query fallback for
// WebSocket/SSE endpoints is enabled. Query-string tokens leak into access
// logs, proxies and browser history, so they are honored ONLY under an
// explicit GOTV_DEV_MODE=true (never acceptable in production — gotv-svc
// refuses to boot with that combination, and authenticateStreamRequest
// additionally refuses query tokens whenever APP_ENV=production).
func streamDevMode() bool {
	if isProduction() {
		return false
	}
	return os.Getenv("GOTV_DEV_MODE") == "true"
}

// authenticateStreamRequest authenticates WebSocket upgrade and SSE stream
// requests with the same auth stack as the API middleware: HS256 JWT via
// decodeToken, type=="access" claim required, jti blacklist enforced.
//
// Browsers cannot set headers on WebSocket/EventSource connections, so the
// HttpOnly inec_token cookie is accepted as a first-class credential (this
// replaces the old "edge proxy must translate cookie→Bearer" requirement).
// The ?token= query fallback exists ONLY in dev mode (see streamDevMode).
//
// Fail closed: any failure writes a 401 and returns ok=false.
func authenticateStreamRequest(w http.ResponseWriter, r *http.Request) (jwt.MapClaims, bool) {
	// 1. Claims already validated by jwtAuthMiddleware (header or cookie).
	if claims, ok := getUserFromContext(r); ok {
		return claims, true
	}

	// 2. Authorization: Bearer header (non-browser clients).
	var tokenStr string
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		tokenStr = strings.TrimPrefix(auth, "Bearer ")
	} else if cookie, err := r.Cookie("inec_token"); err == nil && cookie.Value != "" {
		// 3. HttpOnly cookie (browser WebSocket/EventSource clients).
		tokenStr = cookie.Value
	} else if q := r.URL.Query().Get("token"); q != "" {
		// 4. Dev-only query fallback — log-leakable, never in production.
		if !streamDevMode() {
			writeJSON(w, 401, M{"error": "query-token authentication is disabled (set GOTV_DEV_MODE=true in non-production to enable)"})
			return nil, false
		}
		tokenStr = q
	}

	if tokenStr == "" {
		writeJSON(w, 401, M{"error": "authentication required"})
		return nil, false
	}
	claims, err := decodeToken(tokenStr)
	if err != nil {
		writeJSON(w, 401, M{"error": "invalid or expired token"})
		return nil, false
	}
	// Refresh tokens must never authenticate streams.
	if tokenType, _ := claims["type"].(string); tokenType != "access" {
		writeJSON(w, 401, M{"error": "access token required"})
		return nil, false
	}
	// Reject revoked tokens (logout / session revocation by jti).
	if jti, _ := claims["jti"].(string); jti != "" && blacklist.isBlacklisted(jti) {
		writeJSON(w, 401, M{"error": "token has been revoked"})
		return nil, false
	}
	return claims, true
}

// corsProductionMiddleware implements a strict origin allow-list driven by
// CORS_ORIGINS (comma-separated). SECURITY:
//   - unset CORS_ORIGINS => empty allow-list (all cross-origin requests denied);
//   - APP_ENV=production with an empty allow-list is a fatal startup error;
//   - Access-Control-Allow-Credentials:true is NEVER sent with a wildcard origin.
func corsProductionMiddleware(next http.Handler) http.Handler {
	raw := strings.TrimSpace(os.Getenv("CORS_ORIGINS"))
	var allowedOrigins []string
	wildcard := false
	if raw == "" {
		if isProduction() {
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
				allowedOrigins = append(allowedOrigins, o)
			}
		}
	}
	if wildcard {
		log.Warn().Msg("CORS allow-list contains wildcard — credentials will not be allowed")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			allowed := false
			for _, ao := range allowedOrigins {
				if ao == origin {
					allowed = true
					break
				}
			}
			switch {
			case allowed:
				// Explicitly listed origin: credentials permitted.
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
			case wildcard:
				// Wildcard (dev only): reflect via "*" and NEVER allow credentials.
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
