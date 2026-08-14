// Wave-A Round-4 regression tests for gotv-svc (R4-03, R4-04, R4-43, R4-58).
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"inec-go-backend/internal/gotv"
)

// ─── R4-04: dev auth routes must not exist when dev mode is off ────────────

func TestR404_DevAuthRoutesGatedAtRegistration(t *testing.T) {
	orig := devModeEnabled
	t.Cleanup(func() { devModeEnabled = orig })

	// Dev mode OFF: /auth/login must not exist (404 on the router).
	devModeEnabled = false
	r := mux.NewRouter()
	maybeRegisterDevAuthRoutes(r)
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"admin","password":"anything"}`))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("R4-04: /auth/login with dev mode off returned %d, want 404 (route must not exist)", rr.Code)
	}

	// Dev mode ON: the route exists and vends the dev token.
	devModeEnabled = true
	r2 := mux.NewRouter()
	maybeRegisterDevAuthRoutes(r2)
	req2 := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"dev","password":"x"}`))
	rr2 := httptest.NewRecorder()
	r2.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("R4-04: /auth/login with dev mode on returned %d, want 200", rr2.Code)
	}
}

// ─── R4-03: public delivery/inbound webhooks fail closed ────────────────────

func TestR403_WebhookEndpoints503WhenSecretUnset(t *testing.T) {
	gotv.InitWebhookSecrets("", "", "")
	t.Cleanup(func() { gotv.InitWebhookSecrets("", "", "") })

	// AT delivery receipt
	rr := httptest.NewRecorder()
	handleDeliveryReceiptAT(rr, httptest.NewRequest("POST", "/gotv/webhooks/delivery/africastalking", strings.NewReader(`{}`)))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("R4-03: AT delivery webhook with unset secret returned %d, want 503", rr.Code)
	}

	// Twilio delivery receipt
	rr = httptest.NewRecorder()
	handleDeliveryReceiptTwilio(rr, httptest.NewRequest("POST", "/gotv/webhooks/delivery/twilio", strings.NewReader("MessageSid=x")))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("R4-03: Twilio delivery webhook with unset secret returned %d, want 503", rr.Code)
	}

	// WhatsApp delivery receipt (POST)
	rr = httptest.NewRecorder()
	handleDeliveryReceiptWhatsApp(rr, httptest.NewRequest("POST", "/gotv/webhooks/delivery/whatsapp", strings.NewReader(`{}`)))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("R4-03: WhatsApp delivery webhook with unset secret returned %d, want 503", rr.Code)
	}

	// Inbound SMS (opt-out processing) — previously NO verification at all.
	rr = httptest.NewRecorder()
	handleInboundSMS(rr, httptest.NewRequest("POST", "/gotv/webhooks/inbound/sms", strings.NewReader("from=%2B234&text=STOP")))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("R4-03: inbound SMS with unset secret returned %d, want 503", rr.Code)
	}
}

func TestR403_InboundSMSRequiresValidSignature(t *testing.T) {
	secret := "at-shared-secret"
	gotv.InitWebhookSecrets(secret, "", "")
	t.Cleanup(func() { gotv.InitWebhookSecrets("", "", "") })

	form := url.Values{"from": {"+2348012345678"}, "text": {"HELLO"}} // non-opt-out text: no dispatcher needed
	body := form.Encode()

	// Unsigned → 403.
	req := httptest.NewRequest("POST", "/gotv/webhooks/inbound/sms", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handleInboundSMS(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("R4-03: unsigned inbound SMS returned %d, want 403", rr.Code)
	}

	// Forged signature → 403.
	req = httptest.NewRequest("POST", "/gotv/webhooks/inbound/sms", strings.NewReader(body))
	req.Header.Set("X-AT-Signature", "deadbeef")
	rr = httptest.NewRecorder()
	handleInboundSMS(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("R4-03: forged inbound SMS signature returned %d, want 403", rr.Code)
	}

	// Valid HMAC → processed (200).
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	req = httptest.NewRequest("POST", "/gotv/webhooks/inbound/sms", strings.NewReader(body))
	req.Header.Set("X-AT-Signature", hex.EncodeToString(mac.Sum(nil)))
	rr = httptest.NewRecorder()
	handleInboundSMS(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("R4-03: validly-signed inbound SMS returned %d, want 200", rr.Code)
	}
}

// ─── R4-43: webhook registration SSRF validation ───────────────────────────

func TestR443_CreateWebhookRejectsUnsafeURLs(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data",
		"https://127.0.0.1/hook",
		"https://10.1.2.3/hook",
		"not-a-url",
	} {
		body := fmt.Sprintf(`{"url":%q,"secret":"s","event_types":["campaign.completed"]}`, u)
		req := httptest.NewRequest("POST", "/gotv/webhooks", strings.NewReader(body))
		req.Header.Set("X-GOTV-Party-ID", "1")
		rr := httptest.NewRecorder()
		handleCreateWebhook(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("R4-43: webhook registration with %s returned %d, want 400", u, rr.Code)
		}
	}
}

// ─── R4-58: canvass walklist proximity ordering ────────────────────────────

func TestR458_WalklistProximityOrdering(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}
	// Polling units at known coordinates.
	if _, err := dbConn.Exec(`INSERT INTO polling_units (code, latitude, longitude) VALUES
		('R458-NEAR', 6.4500, 3.3900),
		('R458-FAR', 9.0500, 7.4900)
		ON CONFLICT (code) DO UPDATE SET latitude=EXCLUDED.latitude, longitude=EXCLUDED.longitude`); err != nil {
		t.Fatalf("seed PUs: %v", err)
	}
	// Two contacts for party 999058, one near the canvasser, one far.
	for i, c := range []struct{ id, pu string }{{"r458-near", "R458-NEAR"}, {"r458-far", "R458-FAR"}} {
		enc, err := svc.Encrypt("+234800000000" + fmt.Sprint(i))
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		if _, err := dbConn.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, polling_unit_code)
			VALUES ($1, 999058, $2, $3, $4)
			ON CONFLICT (contact_id) DO UPDATE SET polling_unit_code=EXCLUDED.polling_unit_code`,
			c.id, enc, svc.PhoneHash("+234800000000"+fmt.Sprint(i)), c.pu); err != nil {
			t.Fatalf("seed contact: %v", err)
		}
	}

	// Canvasser at Lagos (6.45, 3.39): the NEAR contact must sort first.
	req := httptest.NewRequest("GET", "/gotv/canvass/walklist?lat=6.45&lng=3.39", nil)
	req.Header.Set("X-GOTV-Party-ID", "999058")
	rr := httptest.NewRecorder()
	handleCanvassWalklist(rr, req)
	if rr.Code != 200 {
		t.Fatalf("walklist returned %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	nearIdx := strings.Index(body, "r458-near")
	farIdx := strings.Index(body, "r458-far")
	if nearIdx < 0 || farIdx < 0 {
		t.Fatalf("walklist missing seeded contacts: %s", body)
	}
	if nearIdx > farIdx {
		t.Fatalf("R4-58: proximity ordering not applied (near contact after far contact): %s", body)
	}
}
