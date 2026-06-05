package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ── Role-Based Access Guard ──

// allowedSelfRegRoles are the only roles a user can assign themselves during registration.
var allowedSelfRegRoles = map[string]bool{
	"public":   true,
	"observer": true,
}

// guardAuth is a convenience wrapper: extracts user claims from JWT context,
// or returns 401. Use this for any endpoint that requires authentication
// but does NOT require a specific role.
func guardAuth(w http.ResponseWriter, r *http.Request) (map[string]interface{}, bool) {
	claims, ok := getUserFromContext(r)
	if !ok {
		writeError(w, 401, "authentication required")
		return nil, false
	}
	return claims, true
}

// guardRole extracts user from JWT context and checks role membership.
// Returns the claims and true on success, writes 401/403 and returns false on failure.
func guardRole(w http.ResponseWriter, r *http.Request, roles ...string) (map[string]interface{}, bool) {
	claims, ok := getUserFromContext(r)
	if !ok {
		writeError(w, 401, "authentication required")
		return nil, false
	}
	role, _ := claims["role"].(string)
	for _, allowed := range roles {
		if role == allowed {
			return claims, true
		}
	}
	writeError(w, 403, fmt.Sprintf("role '%s' not authorized; required one of: %v", role, roles))
	return nil, false
}

// guardWrite checks auth + Permify permission for write operations.
func guardWrite(w http.ResponseWriter, r *http.Request, permission string, roles ...string) (map[string]interface{}, bool) {
	claims, ok := guardRole(w, r, roles...)
	if !ok {
		return nil, false
	}
	role, _ := claims["role"].(string)
	if !checkPermission(role, permission) {
		writeError(w, 403, "permission denied by authorization service")
		return nil, false
	}
	return claims, true
}

// ── Result Status Machine ──

// validTransitions defines the allowed status transitions for election results.
var validTransitions = map[string][]string{
	"pending":   {"validated", "disputed"},
	"validated": {"finalized", "disputed"},
	"disputed":  {"pending", "validated"}, // Can be re-opened
	"finalized": {},                       // Terminal state — no further transitions
}

// canTransition checks if a result can move from currentStatus to newStatus.
func canTransition(currentStatus, newStatus string) bool {
	allowed, exists := validTransitions[currentStatus]
	if !exists {
		return false
	}
	for _, s := range allowed {
		if s == newStatus {
			return true
		}
	}
	return false
}

// ── CSRF Protection ──

var (
	csrfTokenStore = struct {
		sync.RWMutex
		tokens map[string]time.Time
	}{tokens: make(map[string]time.Time)}
)

func generateCSRFToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func csrfMiddleware(next http.Handler) http.Handler {
	safeMethods := map[string]bool{"GET": true, "HEAD": true, "OPTIONS": true}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF for API key authenticated routes
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		// Skip for public paths, health checks, metrics
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		// Skip CSRF for safe methods
		if safeMethods[r.Method] {
			next.ServeHTTP(w, r)
			return
		}
		// For state-changing methods, check CSRF token header
		csrfToken := r.Header.Get("X-CSRF-Token")
		if csrfToken == "" {
			// Also accept for programmatic clients with valid JWT
			auth := r.Header.Get("Authorization")
			if auth != "" && strings.HasPrefix(auth, "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, 403, "CSRF token missing")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Audit Trail Helper ──

// auditWrite logs an audit entry for any write operation.
func auditWrite(action, entityType, entityID string, r *http.Request, details map[string]interface{}) {
	userID := 0
	claims, ok := getUserFromContext(r)
	if ok {
		if sub, ok := claims["sub"].(string); ok {
			fmt.Sscanf(sub, "%d", &userID)
		}
	}
	logAudit(action, entityType, entityID, userID, details)
}

// ── Input Validation Helpers ──

// decodeAndValidateBody decodes JSON body and validates using struct tags.
// Returns the error message to send to client, or empty string on success.
func decodeAndValidateBody(w http.ResponseWriter, r *http.Request, dest interface{}) bool {
	if err := decodeAndValidate(r, dest); err != nil {
		writeError(w, 400, err.Error())
		return false
	}
	return true
}

// ── Security Headers Enhancement ──

func enhancedSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self' ws: wss: https:; font-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// ── Rate Limiter with Redis fallback ──

type redisRateLimiter struct {
	mu       sync.Mutex
	fallback *rateLimiterStore
}

func newRedisRateLimiter() *redisRateLimiter {
	return &redisRateLimiter{
		fallback: newRateLimiter(),
	}
}

func (rl *redisRateLimiter) allow(key string, limit int, window time.Duration) bool {
	// Try Redis-based rate limiting if middleware hub has Redis
	if mwHub != nil && mwHub.Redis != nil {
		ctx := context.Background()
		count, err := mwHub.Redis.Incr(ctx, key)
		if err == nil {
			if count == 1 {
				mwHub.Redis.Set(ctx, key, "1", window)
			}
			return count <= int64(limit)
		}
		// Fall through to in-memory on Redis error
		log.Debug().Err(err).Msg("Redis rate limiter fallback to in-memory")
	}
	return rl.fallback.allow(key, limit, window)
}

// stripPort removes :port from an IP address for rate limiting consistency.
func stripPort(addr string) string {
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		// Handle IPv6 [::1]:port format
		if strings.Contains(addr, "[") {
			if bracketEnd := strings.Index(addr, "]"); bracketEnd > 0 {
				return addr[1:bracketEnd]
			}
		}
		return addr[:idx]
	}
	return addr
}

// ── API Key Hashing ──

func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
