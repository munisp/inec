package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ---- Adaptive Rate Limiter ------------------------------------------------

// TokenBucket implements a per-IP token bucket rate limiter.
type TokenBucket struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     float64 // tokens per second
	capacity float64
}

type bucket struct {
	tokens    float64
	lastRefil time.Time
}

func NewTokenBucket(ratePerSec, capacity float64) *TokenBucket {
	tb := &TokenBucket{
		buckets:  make(map[string]*bucket),
		rate:     ratePerSec,
		capacity: capacity,
	}
	// Background cleanup of stale buckets every 5 minutes
	go func() {
		for range time.Tick(5 * time.Minute) {
			tb.mu.Lock()
			for ip, b := range tb.buckets {
				if time.Since(b.lastRefil) > 10*time.Minute {
					delete(tb.buckets, ip)
				}
			}
			tb.mu.Unlock()
		}
	}()
	return tb
}

func (tb *TokenBucket) Allow(ip string) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	b, ok := tb.buckets[ip]
	if !ok {
		b = &bucket{tokens: tb.capacity, lastRefil: time.Now()}
		tb.buckets[ip] = b
	}
	now := time.Now()
	elapsed := now.Sub(b.lastRefil).Seconds()
	b.tokens = min(tb.capacity, b.tokens+elapsed*tb.rate)
	b.lastRefil = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}


// AdaptiveRateLimitMiddleware applies per-IP rate limiting with configurable burst.
func AdaptiveRateLimitMiddleware(ratePerSec, burst float64) func(http.Handler) http.Handler {
	limiter := NewTokenBucket(ratePerSec, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.Header.Get("X-Forwarded-For")
			if ip == "" {
				ip = r.RemoteAddr
			}
			if !limiter.Allow(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "rate limit exceeded",
					"code":  "RATE_LIMIT_EXCEEDED",
				})
				log.Warn().Str("ip", ip).Str("path", r.URL.Path).Msg("rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---- Circuit Breaker -------------------------------------------------------

type CircuitState int

const (
	StateClosed   CircuitState = iota // normal operation
	StateOpen                         // failing — reject fast
	StateHalfOpen                     // probe — allow one request
)

// CircuitBreaker is a simple three-state circuit breaker for downstream calls.
type AdvancedCircuitBreaker struct {
	mu           sync.Mutex
	state        CircuitState
	failures     int
	threshold    int
	resetTimeout time.Duration
	openedAt     time.Time
	name         string
}

func NewAdvancedCircuitBreaker(name string, threshold int, resetTimeout time.Duration) *AdvancedCircuitBreaker {
	return &AdvancedCircuitBreaker{
		name:         name,
		threshold:    threshold,
		resetTimeout: resetTimeout,
		state:        StateClosed,
	}
}

func (cb *AdvancedCircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.openedAt) > cb.resetTimeout {
			cb.state = StateHalfOpen
			log.Info().Str("circuit", cb.name).Msg("circuit breaker half-open — probing")
			return true
		}
		return false
	case StateHalfOpen:
		return true
	}
	return false
}

func (cb *AdvancedCircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	if cb.state == StateHalfOpen {
		cb.state = StateClosed
		log.Info().Str("circuit", cb.name).Msg("circuit breaker closed — service recovered")
	}
}

func (cb *AdvancedCircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.failures >= cb.threshold {
		cb.state = StateOpen
		cb.openedAt = time.Now()
		log.Error().Str("circuit", cb.name).Int("failures", cb.failures).Msg("circuit breaker OPEN")
	}
}

// CircuitBreakerMiddleware wraps an HTTP handler with circuit breaker protection.
func AdvancedCircuitBreakerMiddleware(cb *AdvancedCircuitBreaker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cb.Allow() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "service temporarily unavailable",
					"code":  "CIRCUIT_OPEN",
				})
				return
			}
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			if rw.status >= 500 {
				cb.RecordFailure()
			} else {
				cb.RecordSuccess()
			}
		})
	}
}
