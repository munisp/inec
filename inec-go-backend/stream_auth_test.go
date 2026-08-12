package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signStreamToken mints an HS256 JWT with the ephemeral test key (see
// auth.go init — test binaries always get a key).
func signStreamToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(time.Hour).Unix()
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tok
}

func accessClaims() jwt.MapClaims {
	return jwt.MapClaims{"sub": "42", "role": "admin", "type": "access", "jti": "jti-stream-ok"}
}

func TestStreamAuthRejectsNoCredentials(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/results/ws/updates", nil)
	rec := httptest.NewRecorder()
	if _, ok := authenticateStreamRequest(rec, req); ok {
		t.Fatal("expected unauthenticated request to be rejected")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestStreamAuthAcceptsCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/results/ws/updates", nil)
	req.AddCookie(&http.Cookie{Name: "inec_token", Value: signStreamToken(t, accessClaims())})
	rec := httptest.NewRecorder()
	claims, ok := authenticateStreamRequest(rec, req)
	if !ok {
		t.Fatalf("expected cookie-authenticated request to pass, got status %d", rec.Code)
	}
	if sub, _ := claims["sub"].(string); sub != "42" {
		t.Fatalf("expected sub=42, got %v", claims["sub"])
	}
}

func TestStreamAuthAcceptsBearerHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/observer/stream", nil)
	req.Header.Set("Authorization", "Bearer "+signStreamToken(t, accessClaims()))
	rec := httptest.NewRecorder()
	if _, ok := authenticateStreamRequest(rec, req); !ok {
		t.Fatalf("expected bearer-authenticated request to pass, got status %d", rec.Code)
	}
}

func TestStreamAuthRejectsRefreshToken(t *testing.T) {
	claims := accessClaims()
	claims["type"] = "refresh"
	req := httptest.NewRequest(http.MethodGet, "/observer/stream", nil)
	req.AddCookie(&http.Cookie{Name: "inec_token", Value: signStreamToken(t, claims)})
	rec := httptest.NewRecorder()
	if _, ok := authenticateStreamRequest(rec, req); ok {
		t.Fatal("refresh token must not authenticate a stream")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestStreamAuthRejectsBlacklistedJTI(t *testing.T) {
	claims := accessClaims()
	claims["jti"] = "jti-revoked-stream"
	blacklist.mu.Lock()
	blacklist.tokens["jti-revoked-stream"] = time.Now().Add(time.Hour)
	blacklist.mu.Unlock()
	defer func() {
		blacklist.mu.Lock()
		delete(blacklist.tokens, "jti-revoked-stream")
		blacklist.mu.Unlock()
	}()

	req := httptest.NewRequest(http.MethodGet, "/observer/stream", nil)
	req.AddCookie(&http.Cookie{Name: "inec_token", Value: signStreamToken(t, claims)})
	rec := httptest.NewRecorder()
	if _, ok := authenticateStreamRequest(rec, req); ok {
		t.Fatal("revoked token must not authenticate a stream")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestStreamAuthQueryTokenRejectedOutsideDevMode(t *testing.T) {
	t.Setenv("GOTV_DEV_MODE", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("INEC_ENV", "")
	req := httptest.NewRequest(http.MethodGet, "/observer/stream?token="+signStreamToken(t, accessClaims()), nil)
	rec := httptest.NewRecorder()
	if _, ok := authenticateStreamRequest(rec, req); ok {
		t.Fatal("query-token auth must be disabled outside dev mode")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestStreamAuthQueryTokenAllowedInDevMode(t *testing.T) {
	t.Setenv("GOTV_DEV_MODE", "true")
	t.Setenv("APP_ENV", "")
	t.Setenv("INEC_ENV", "")
	req := httptest.NewRequest(http.MethodGet, "/observer/stream?token="+signStreamToken(t, accessClaims()), nil)
	rec := httptest.NewRecorder()
	if _, ok := authenticateStreamRequest(rec, req); !ok {
		t.Fatalf("expected dev-mode query token to pass, got status %d", rec.Code)
	}
}

func TestStreamAuthQueryTokenRejectedInProductionEvenWithDevMode(t *testing.T) {
	t.Setenv("GOTV_DEV_MODE", "true")
	t.Setenv("APP_ENV", "production")
	req := httptest.NewRequest(http.MethodGet, "/observer/stream?token="+signStreamToken(t, accessClaims()), nil)
	rec := httptest.NewRecorder()
	if _, ok := authenticateStreamRequest(rec, req); ok {
		t.Fatal("query-token auth must never be honored in production")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestStreamAuthRejectsGarbageToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/results/ws/updates", nil)
	req.AddCookie(&http.Cookie{Name: "inec_token", Value: "not-a-jwt"})
	rec := httptest.NewRecorder()
	if _, ok := authenticateStreamRequest(rec, req); ok {
		t.Fatal("garbage token must be rejected")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// The WS upgrade handlers must fail closed before touching the upgrader.
func TestHandleWSUpdatesRejectsUnauthenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/results/ws/updates", nil)
	rec := httptest.NewRecorder()
	handleWSUpdates(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandleWSUpdatesShardedRejectsUnauthenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/results/ws/updates/sharded", nil)
	rec := httptest.NewRecorder()
	handleWSUpdatesSharded(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandleDashboardSSERejectsUnauthenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/dashboard/stream", nil)
	rec := httptest.NewRecorder()
	handleDashboardSSE(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
