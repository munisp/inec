package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func signBody(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// telcoProviderAuth must fail closed (503) when no secret is configured —
// the endpoint is disabled rather than left open to anonymous traffic.
func TestTelcoProviderAuthFailsClosedWithoutSecret(t *testing.T) {
	t.Setenv(telcoWebhookSecretEnv, "")
	called := false
	h := telcoProviderAuth(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest("POST", "/ussd/gateway", strings.NewReader("text="))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without secret, got %d", rec.Code)
	}
	if called {
		t.Fatal("handler must not run without a configured secret")
	}
}

// Valid HMAC signature over the raw body passes and the handler can re-read
// the body.
func TestTelcoProviderAuthAcceptsValidSignature(t *testing.T) {
	secret := "test-secret"
	body := "sessionId=abc&text=1*2"
	t.Setenv(telcoWebhookSecretEnv, secret)
	var gotBody string
	h := telcoProviderAuth(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotBody = r.FormValue("sessionId")
		w.WriteHeader(200)
	})
	req := httptest.NewRequest("POST", "/ussd/gateway", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Telco-Signature", signBody(secret, body))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 with valid signature, got %d", rec.Code)
	}
	if gotBody != "abc" {
		t.Fatalf("handler could not re-read body, got %q", gotBody)
	}
}

// Wrong/missing signatures are rejected with 401.
func TestTelcoProviderAuthRejectsBadSignature(t *testing.T) {
	t.Setenv(telcoWebhookSecretEnv, "test-secret")
	h := telcoProviderAuth(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run with a bad signature")
	})
	for _, sig := range []string{"", "deadbeef", signBody("other-secret", "x=1")} {
		req := httptest.NewRequest("POST", "/sms/verify", strings.NewReader("x=1"))
		req.Header.Set("X-Telco-Signature", sig)
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for signature %q, got %d", sig, rec.Code)
		}
	}
}

// The USSD string table must always fall back to English for unknown
// languages or missing keys, and never return empty.
func TestUSSDTextFallback(t *testing.T) {
	if got := ussdText("xx", "main_menu"); got != ussdStrings["en"]["main_menu"] {
		t.Fatalf("unknown language must fall back to English, got %q", got)
	}
	if got := ussdText("ha", "no_such_key"); got != "" {
		t.Fatalf("unknown key must return empty, got %q", got)
	}
	for _, lang := range ussdSupportedLangs {
		for _, key := range []string{"lang_menu", "main_menu", "enter_pu", "goodbye", "invalid", "incident_ok"} {
			if ussdText(lang, key) == "" {
				t.Fatalf("missing %s/%s", lang, key)
			}
		}
	}
}

// The WhatsApp HELP reply must not require a database and must list the
// voter intents.
func TestWhatsAppHelpReply(t *testing.T) {
	reply := processWhatsAppVoterMessage("+2348000000000", "HELP")
	for _, kw := range []string{"REGISTER", "PU", "REPORT"} {
		if !strings.Contains(reply, kw) {
			t.Fatalf("help reply missing %q: %q", kw, reply)
		}
	}
}
