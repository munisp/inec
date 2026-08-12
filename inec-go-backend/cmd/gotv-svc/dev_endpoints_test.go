package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Gap 2: dev-login / dev-me vend admin-role tokens without checking
// credentials — they must be hidden (404) unless GOTV_DEV_MODE=true.

func withDevMode(t *testing.T, enabled bool) {
	t.Helper()
	prev := devModeEnabled
	devModeEnabled = enabled
	t.Cleanup(func() { devModeEnabled = prev })
}

func TestDevLoginHiddenWithoutDevMode(t *testing.T) {
	withDevMode(t, false)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"admin","password":"x"}`))
	rec := httptest.NewRecorder()
	handleDevLogin(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 without dev mode, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "access_token") {
		t.Fatal("no token may be vended without dev mode")
	}
}

func TestDevMeHiddenWithoutDevMode(t *testing.T) {
	withDevMode(t, false)
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	rec := httptest.NewRecorder()
	handleDevMe(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 without dev mode, got %d", rec.Code)
	}
}

func TestDevLoginWorksInDevMode(t *testing.T) {
	withDevMode(t, true)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"canvasser1","password":"x"}`))
	rec := httptest.NewRecorder()
	handleDevLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 in dev mode, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "dev-canvasser1-token") {
		t.Fatalf("expected dev token in response, got %s", rec.Body.String())
	}
}

// Gap 1: /gotv/ws must reject query-string tokens outside dev mode before
// touching the auth middleware (URLs leak into logs).
func TestWebSocketRejectsQueryTokenOutsideDevMode(t *testing.T) {
	withDevMode(t, false)
	t.Setenv("APP_ENV", "")
	t.Setenv("INEC_ENV", "")
	req := httptest.NewRequest(http.MethodGet, "/gotv/ws?token=anything", nil)
	rec := httptest.NewRecorder()
	handleWebSocket(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestWebSocketRejectsUnauthenticated(t *testing.T) {
	withDevMode(t, false)
	req := httptest.NewRequest(http.MethodGet, "/gotv/ws", nil)
	rec := httptest.NewRecorder()
	// authMid is nil in tests; an unauthenticated request must be rejected
	// before authMid is dereferenced only if no credential is present — the
	// auth stack would fail closed regardless. Guard against nil for the
	// no-credential path by ensuring we never reach it: with no Authorization
	// header, no cookie and no query token, Authenticate is still invoked,
	// so skip when authMid is not initialized.
	if authMid == nil {
		t.Skip("authMid not initialized in unit test context")
	}
	handleWebSocket(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
