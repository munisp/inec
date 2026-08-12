package gotv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Gap 1: the GOTV auth stack must accept the HttpOnly inec_token cookie
// (browser WS/SSE clients cannot set headers) and must reject ?token=
// query-param credentials unless dev mode is active.

func TestIsProductionEnv(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("INEC_ENV", "")
	if IsProductionEnv() {
		t.Fatal("empty env must not be production")
	}
	t.Setenv("APP_ENV", "production")
	if !IsProductionEnv() {
		t.Fatal("APP_ENV=production must be production")
	}
	t.Setenv("APP_ENV", "")
	t.Setenv("INEC_ENV", "production")
	if !IsProductionEnv() {
		t.Fatal("INEC_ENV=production must be production")
	}
}

func TestQueryTokenRejectedWithoutDevMode(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("INEC_ENV", "")
	am := NewAuthMiddleware(nil, AuthConfig{DevMode: false})
	req := httptest.NewRequest(http.MethodGet, "/gotv/ws?token=abc", nil)
	if _, _, err := am.Authenticate(req); err == nil ||
		!strings.Contains(err.Error(), "query-token") {
		t.Fatalf("expected query-token rejection, got %v", err)
	}
}

func TestQueryTokenAcceptedInDevMode(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("INEC_ENV", "")
	// DevMode with no auth service: any Bearer authenticates as party 1.
	am := NewAuthMiddleware(nil, AuthConfig{DevMode: true})
	req := httptest.NewRequest(http.MethodGet, "/gotv/ws?token=abc", nil)
	partyID, user, err := am.Authenticate(req)
	if err != nil {
		t.Fatalf("expected dev-mode query token to authenticate, got %v", err)
	}
	if partyID != 1 || user != "dev-user" {
		t.Fatalf("expected party 1 dev-user, got %d %q", partyID, user)
	}
}

func TestCookieAuthenticatesAsBearer(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("INEC_ENV", "")
	am := NewAuthMiddleware(nil, AuthConfig{DevMode: true})
	req := httptest.NewRequest(http.MethodGet, "/gotv/ws", nil)
	req.AddCookie(&http.Cookie{Name: "inec_token", Value: "cookie-jwt"})
	partyID, _, err := am.Authenticate(req)
	if err != nil {
		t.Fatalf("expected cookie credential to authenticate in dev mode, got %v", err)
	}
	if partyID != 1 {
		t.Fatalf("expected party 1, got %d", partyID)
	}
}

func TestNoCredentialsRejected(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("INEC_ENV", "")
	am := NewAuthMiddleware(nil, AuthConfig{DevMode: true})
	req := httptest.NewRequest(http.MethodGet, "/gotv/ws", nil)
	if _, _, err := am.Authenticate(req); err == nil {
		t.Fatal("request with no credentials must be rejected")
	}
}
