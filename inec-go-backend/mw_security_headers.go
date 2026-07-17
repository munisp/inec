package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// SecurityHeadersMiddleware injects a comprehensive set of HTTP security headers.
// This satisfies OWASP Secure Headers Project recommendations for a government platform.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Content Security Policy — strict for a React SPA
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'wasm-unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob: https://*.tile.openstreetmap.org https://*.mapbox.com; "+
				"connect-src 'self' wss: https:; "+
				"font-src 'self' data:; "+
				"frame-src 'none'; "+
				"object-src 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'; "+
				"upgrade-insecure-requests;",
		)
		// Strict Transport Security — 2 years, include subdomains, preload
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		// Prevent MIME sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Clickjacking protection
		w.Header().Set("X-Frame-Options", "DENY")
		// XSS filter (legacy browsers)
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		// Referrer policy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Permissions policy — restrict sensitive browser APIs
		w.Header().Set("Permissions-Policy",
			"geolocation=(self), camera=(self), microphone=(self), "+
				"payment=(), usb=(), bluetooth=(), serial=()",
		)
		// Remove server fingerprint
		w.Header().Del("Server")
		w.Header().Del("X-Powered-By")
		// Cross-Origin policies for SharedArrayBuffer / performance.measureUserAgentSpecificMemory
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")

		next.ServeHTTP(w, r)
	})
}

// ---- Secret Rotation Manager -----------------------------------------------

// SecretManager provides in-memory secret rotation with configurable TTL.
// In production this would integrate with HashiCorp Vault or AWS Secrets Manager.
type SecretManager struct {
	mu      sync.RWMutex
	secrets map[string]*secret
}

type secret struct {
	value     string
	expiresAt time.Time
}

var globalSecretManager = &SecretManager{
	secrets: make(map[string]*secret),
}

// Get retrieves a secret by name, refreshing it if expired.
func (sm *SecretManager) Get(name string) (string, error) {
	sm.mu.RLock()
	s, ok := sm.secrets[name]
	sm.mu.RUnlock()

	if ok && time.Now().Before(s.expiresAt) {
		return s.value, nil
	}

	// Attempt to load from environment (Vault agent would inject these)
	val := os.Getenv(name)
	if val == "" {
		return "", fmt.Errorf("secret %q not found in environment", name)
	}

	sm.mu.Lock()
	sm.secrets[name] = &secret{
		value:     val,
		expiresAt: time.Now().Add(15 * time.Minute),
	}
	sm.mu.Unlock()

	return val, nil
}

// Rotate generates a new cryptographically random secret and stores it.
// This is used for ephemeral session tokens and CSRF nonces.
func (sm *SecretManager) Rotate(name string, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate secret: %w", err)
	}
	val := base64.URLEncoding.EncodeToString(b)
	sm.mu.Lock()
	sm.secrets[name] = &secret{value: val, expiresAt: time.Now().Add(ttl)}
	sm.mu.Unlock()
	log.Info().Str("secret", name).Dur("ttl", ttl).Msg("secret rotated")
	return val, nil
}

// StartSecretRotation launches a background goroutine that rotates the JWT
// signing key every 24 hours, ensuring forward secrecy.
func StartSecretRotation() {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := globalSecretManager.Rotate("JWT_SIGNING_KEY", 25*time.Hour); err != nil {
				log.Error().Err(err).Msg("JWT signing key rotation failed")
			} else {
				log.Info().Msg("JWT signing key rotated successfully")
			}
		}
	}()
}

// CSRFNonce generates a per-request CSRF nonce for use in CSP script-src.
func CSRFNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
