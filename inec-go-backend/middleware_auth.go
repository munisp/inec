package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const userContextKey contextKey = "user"

// publicPaths are endpoints that do not require authentication.
var publicPaths = map[string]bool{
	"/healthz":         true,
	"/readiness":       true,
	"/auth/login":      true,
	"/auth/register":   true,
	"/ws":              true,
	"/metrics":         true,
	"/auth/refresh":    true,
	"/observer/stream": true, // SSE uses query param auth (EventSource can't set headers)
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

		ctx := context.WithValue(r.Context(), userContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// getUserFromContext extracts JWT claims from request context.
func getUserFromContext(r *http.Request) (jwt.MapClaims, bool) {
	claims, ok := r.Context().Value(userContextKey).(jwt.MapClaims)
	return claims, ok
}

// corsProductionMiddleware replaces the wildcard CORS with configurable origins.
func corsProductionMiddleware(next http.Handler) http.Handler {
	allowedOrigins := strings.Split(envOrDefault("CORS_ORIGINS", "*"), ",")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := false

		for _, ao := range allowedOrigins {
			ao = strings.TrimSpace(ao)
			if ao == "*" || ao == origin {
				allowed = true
				break
			}
		}

		if allowed && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
