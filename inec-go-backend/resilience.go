package main

import (
	"context"
	"fmt"
	"github.com/rs/zerolog/log"
	"math"
	"net/http"
	"sync"
	"time"
)

// CircuitBreaker implements the circuit breaker pattern for inter-service calls.
type CircuitBreaker struct {
	mu           sync.Mutex
	name         string
	state        cbState
	failures     int
	successes    int
	threshold    int
	resetTimeout time.Duration
	halfOpenMax  int
	lastFailure  time.Time
}

type cbState int

const (
	cbClosed cbState = iota
	cbOpen
	cbHalfOpen
)

func NewCircuitBreaker(name string, threshold int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:         name,
		state:        cbClosed,
		threshold:    threshold,
		resetTimeout: resetTimeout,
		halfOpenMax:  2,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case cbClosed:
		return true
	case cbOpen:
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = cbHalfOpen
			cb.successes = 0
			log.Info().Str("breaker", cb.name).Msg("circuit breaker: open → half-open")
			return true
		}
		return false
	case cbHalfOpen:
		return cb.successes < cb.halfOpenMax
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	if cb.state == cbHalfOpen {
		cb.successes++
		if cb.successes >= cb.halfOpenMax {
			cb.state = cbClosed
			log.Info().Str("breaker", cb.name).Msg("circuit breaker: half-open → closed")
		}
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailure = time.Now()
	if cb.failures >= cb.threshold {
		cb.state = cbOpen
		log.Warn().Str("breaker", cb.name).Int("failures", cb.failures).Msg("circuit breaker: closed → open")
	}
}

func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbClosed:
		return "closed"
	case cbOpen:
		return "open"
	case cbHalfOpen:
		return "half-open"
	}
	return "unknown"
}

// RetryConfig controls retry behavior.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

var DefaultRetryConfig = RetryConfig{
	MaxAttempts: 3,
	BaseDelay:   200 * time.Millisecond,
	MaxDelay:    5 * time.Second,
}

// RetryWithBackoff retries fn with exponential backoff.
func RetryWithBackoff(ctx context.Context, cfg RetryConfig, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if attempt < cfg.MaxAttempts-1 {
			delay := time.Duration(float64(cfg.BaseDelay) * math.Pow(2, float64(attempt)))
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return fmt.Errorf("after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

// ResilientHTTPClient wraps http.Client with circuit breaker and retries.
type ResilientHTTPClient struct {
	Client *http.Client
	CB     *CircuitBreaker
	Retry  RetryConfig
}

func NewResilientHTTPClient(name string) *ResilientHTTPClient {
	return &ResilientHTTPClient{
		Client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		CB:    NewCircuitBreaker(name, 5, 30*time.Second),
		Retry: DefaultRetryConfig,
	}
}

func (rc *ResilientHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if !rc.CB.Allow() {
		return nil, fmt.Errorf("circuit breaker open for %s", rc.CB.name)
	}

	var resp *http.Response
	err := RetryWithBackoff(req.Context(), rc.Retry, func() error {
		var e error
		resp, e = rc.Client.Do(req)
		return e
	})

	if err != nil {
		rc.CB.RecordFailure()
		return nil, err
	}
	rc.CB.RecordSuccess()
	return resp, nil
}
