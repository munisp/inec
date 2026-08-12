package authmw

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testSecret(t *testing.T) []byte {
	t.Helper()
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	return Secret()
}

func sign(t *testing.T, secret []byte, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }

func TestMiddlewareRejectsMissingToken(t *testing.T) {
	secret := testSecret(t)
	_ = secret
	h := Middleware("/health")(http.HandlerFunc(okHandler))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/elections", nil))
	if rec.Code != 401 {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestMiddlewarePublicPath(t *testing.T) {
	testSecret(t)
	h := Middleware("/health")(http.HandlerFunc(okHandler))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestMiddlewareRejectsRefreshToken(t *testing.T) {
	secret := testSecret(t)
	tok := sign(t, secret, jwt.MapClaims{
		"sub": "7", "role": "admin", "type": "refresh",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	h := Middleware()(http.HandlerFunc(okHandler))
	req := httptest.NewRequest("GET", "/elections", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("refresh token must be rejected: want 401, got %d", rec.Code)
	}
}

func TestMiddlewareAcceptsAccessTokenAndExposesClaims(t *testing.T) {
	secret := testSecret(t)
	tok := sign(t, secret, jwt.MapClaims{
		"sub": "42", "role": "collation_officer", "type": "access",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	var gotRole string
	var gotUID int
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := Claims(r)
		if !ok {
			t.Error("claims missing from context")
		}
		gotRole, _ = claims["role"].(string)
		gotUID = UserID(claims)
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("POST", "/elections/1/transition", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || gotRole != "collation_officer" || gotUID != 42 {
		t.Fatalf("code=%d role=%q uid=%d", rec.Code, gotRole, gotUID)
	}
}

func TestRequireRoleForbidden(t *testing.T) {
	secret := testSecret(t)
	tok := sign(t, secret, jwt.MapClaims{
		"sub": "9", "role": "observer", "type": "access",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := RequireRole(w, r, "admin", "collation_officer"); !ok {
			return
		}
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("POST", "/elections/1/transition", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}

func TestCORSDeniesByDefault(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "")
	t.Setenv("APP_ENV", "")
	h := CORS()(http.HandlerFunc(okHandler))
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unset CORS_ORIGINS must deny, got %q", got)
	}
}

func TestCORSWildcardNeverAllowsCredentials(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "*")
	t.Setenv("APP_ENV", "")
	h := CORS()(http.HandlerFunc(okHandler))
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Origin", "https://anything.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("want wildcard *, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got == "true" {
		t.Fatal("credentials must never be allowed with wildcard origin")
	}
}

func TestCORSListedOriginAllowsCredentials(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "https://inec.example, https://portal.example")
	h := CORS()(http.HandlerFunc(okHandler))
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Origin", "https://portal.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.example" {
		t.Fatalf("want reflected origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatal("listed origin should allow credentials")
	}
}

func TestHealthHandlerUnavailableDB(t *testing.T) {
	// nil db must report 503, not a static "healthy".
	h := HealthHandler(nil, "test-svc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 503 {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}
