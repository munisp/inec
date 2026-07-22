package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterUsesLocalWindowOutsideProduction(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	originalHub := mwHub
	mwHub = nil
	defer func() { mwHub = originalHub }()

	limiter := newRateLimiter()
	if !limiter.allow("client", 2, time.Hour) || !limiter.allow("client", 2, time.Hour) {
		t.Fatal("local rate limiter rejected requests within its limit")
	}
	if limiter.allow("client", 2, time.Hour) {
		t.Fatal("local rate limiter allowed a request above its limit")
	}
	if !limiter.allow("other-client", 2, time.Hour) {
		t.Fatal("local rate limiter did not isolate keys")
	}
}

func TestRateLimitMiddlewareUsesHigherLoginCeilingInTestRuntime(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("GITHUB_ACTIONS", "")
	originalHub := mwHub
	originalLimiter := rateLimiter
	mwHub = nil
	rateLimiter = newRateLimiter()
	defer func() {
		mwHub = originalHub
		rateLimiter = originalLimiter
	}()

	handler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("test-runtime login request %d returned %d, want %d", i+1, rec.Code, http.StatusNoContent)
		}
	}
}

func TestRateLimiterFailsClosedWithoutRedisInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	originalHub := mwHub
	mwHub = nil
	defer func() { mwHub = originalHub }()

	if newRateLimiter().allow("client", 1, time.Minute) {
		t.Fatal("production rate limiter allowed a request without connected Redis")
	}
}
