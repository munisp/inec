package main

import (
	"testing"
	"time"
)

func TestCircuitBreakerInitialState(t *testing.T) {
	cb := NewGOTVCircuitBreaker("test", 3, 5*time.Second)
	if cb.State() != "closed" {
		t.Errorf("initial state = %s, want closed", cb.State())
	}
	if !cb.Allow() {
		t.Error("closed breaker should allow requests")
	}
}

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	cb := NewGOTVCircuitBreaker("test", 3, 5*time.Second)

	// Record 3 failures — should open
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != "open" {
		t.Errorf("state = %s after 3 failures, want open", cb.State())
	}
	if cb.Allow() {
		t.Error("open breaker should reject requests")
	}
}

func TestCircuitBreakerResetsOnSuccess(t *testing.T) {
	cb := NewGOTVCircuitBreaker("test", 3, 5*time.Second)

	// 2 failures (below threshold)
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != "closed" {
		t.Errorf("state = %s after 2 failures, want closed", cb.State())
	}

	// Success resets counter
	cb.RecordSuccess()
	cb.RecordFailure()
	cb.RecordFailure()
	// Still closed — counter was reset
	if cb.State() != "closed" {
		t.Errorf("state = %s after reset + 2 failures, want closed", cb.State())
	}
}

func TestCircuitBreakerHalfOpen(t *testing.T) {
	cb := NewGOTVCircuitBreaker("test", 2, 10*time.Millisecond)

	// Open the breaker
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Fatalf("state = %s, want open", cb.State())
	}

	// Wait for reset timeout
	time.Sleep(15 * time.Millisecond)

	// Should transition to half-open on Allow()
	if !cb.Allow() {
		t.Error("should allow after reset timeout (half-open)")
	}
	if cb.State() != "half-open" {
		t.Errorf("state = %s, want half-open", cb.State())
	}
}

func TestCircuitBreakerHalfOpenToClose(t *testing.T) {
	cb := NewGOTVCircuitBreaker("test", 2, 10*time.Millisecond)
	cb.halfOpenMax = 2

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)
	cb.Allow() // transitions to half-open

	// 2 successes should close it
	cb.RecordSuccess()
	cb.RecordSuccess()
	if cb.State() != "closed" {
		t.Errorf("state = %s after 2 successes in half-open, want closed", cb.State())
	}
}

func TestCircuitBreakerStats(t *testing.T) {
	cb := NewGOTVCircuitBreaker("my-service", 5, 30*time.Second)
	stats := cb.Stats()
	if stats["name"] != "my-service" {
		t.Errorf("name = %v, want my-service", stats["name"])
	}
	if stats["state"] != "closed" {
		t.Errorf("state = %v, want closed", stats["state"])
	}
	if stats["threshold"] != int32(5) {
		t.Errorf("threshold = %v, want 5", stats["threshold"])
	}
}

func TestByteReadCloser(t *testing.T) {
	data := []byte("hello world")
	br := jsonReader(data)
	buf := make([]byte, 20)
	n, _ := br.Read(buf)
	if string(buf[:n]) != "hello world" {
		t.Errorf("read = %q, want %q", string(buf[:n]), "hello world")
	}
	if err := br.Close(); err != nil {
		t.Errorf("close error: %v", err)
	}
}

func TestReadLimited(t *testing.T) {
	br := jsonReader([]byte("short"))
	result, err := readLimited(br, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != "short" {
		t.Errorf("result = %q, want %q", string(result), "short")
	}
}
