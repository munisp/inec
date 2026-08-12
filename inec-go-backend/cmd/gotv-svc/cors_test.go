package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// okHandler is a downstream handler that records whether it was reached.
func okHandler() (http.Handler, *bool) {
	reached := new(bool)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	}), reached
}

// TestResolveCORSConfigProductionRequiresOrigins proves the fatal path:
// GOTV_CORS_ORIGINS unset with APP_ENV/INEC_ENV=production must be a startup
// error (the caller log.Fatals) instead of failing open.
func TestResolveCORSConfigProductionRequiresOrigins(t *testing.T) {
	if _, err := resolveCORSConfig("", true); err == nil {
		t.Fatal("unset GOTV_CORS_ORIGINS in production must return an error (fatal startup path)")
	}
	// Whitespace-only counts as unset.
	if _, err := resolveCORSConfig("   ", true); err == nil {
		t.Fatal("whitespace-only GOTV_CORS_ORIGINS in production must return an error")
	}
	// Non-production must not error on unset (dev fallback).
	if _, err := resolveCORSConfig("", false); err != nil {
		t.Fatalf("unset GOTV_CORS_ORIGINS in dev must not error, got %v", err)
	}
	// Listed origins are valid in production.
	if _, err := resolveCORSConfig("https://portal.example", true); err != nil {
		t.Fatalf("listed origins in production must not error, got %v", err)
	}
}

// TestCORSMiddlewareDevWildcardHasNoCredentials proves that with
// GOTV_CORS_ORIGINS unset in a non-production environment the middleware
// falls back to a wildcard origin but NEVER sends
// Access-Control-Allow-Credentials (credentials+wildcard is forbidden).
func TestCORSMiddlewareDevWildcardHasNoCredentials(t *testing.T) {
	t.Setenv("GOTV_CORS_ORIGINS", "")
	t.Setenv("APP_ENV", "development")
	t.Setenv("INEC_ENV", "development")

	next, reached := okHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/gotv/pledges", nil)
	req.Header.Set("Origin", "https://evil.example")
	corsMiddleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("dev fallback must reflect wildcard origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got == "true" {
		t.Fatal("Access-Control-Allow-Credentials must NEVER be sent with a wildcard origin")
	}
	if !*reached {
		t.Fatal("request must reach the downstream handler")
	}
}

// TestCORSMiddlewareListedOriginReflectedWithCredentials proves that an
// origin present in GOTV_CORS_ORIGINS is reflected and credentials are
// allowed.
func TestCORSMiddlewareListedOriginReflectedWithCredentials(t *testing.T) {
	t.Setenv("GOTV_CORS_ORIGINS", "https://portal.example, https://admin.example")
	t.Setenv("APP_ENV", "production")
	t.Setenv("INEC_ENV", "")

	next, reached := okHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/gotv/pledges", nil)
	req.Header.Set("Origin", "https://portal.example")
	corsMiddleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.example" {
		t.Fatalf("listed origin must be reflected, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("listed origin must allow credentials, got %q", got)
	}
	if !*reached {
		t.Fatal("request must reach the downstream handler")
	}
}

// TestCORSMiddlewareUnlistedOriginNotReflected proves that an origin NOT in
// GOTV_CORS_ORIGINS gets no CORS headers at all (deny default).
func TestCORSMiddlewareUnlistedOriginNotReflected(t *testing.T) {
	t.Setenv("GOTV_CORS_ORIGINS", "https://portal.example")
	t.Setenv("APP_ENV", "production")
	t.Setenv("INEC_ENV", "")

	next, reached := okHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/gotv/pledges", nil)
	req.Header.Set("Origin", "https://evil.example")
	corsMiddleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unlisted origin must not be reflected, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("unlisted origin must not get credentials header, got %q", got)
	}
	if !*reached {
		t.Fatal("unlisted origin must still reach the downstream handler (CORS is advisory)")
	}
}
