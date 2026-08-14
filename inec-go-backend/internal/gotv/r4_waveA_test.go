// Wave-A Round-4 regression tests (R4-01, R4-02, R4-03, R4-06, R4-43).
// Each test FAILS on the unfixed 00f20ef code and PASSES after the fix.
package gotv

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// r4ScratchDBName is this suite's dedicated scratch database. The R4 suites
// in the monolith root, internal/auth and internal/gotv must NOT share one
// scratch database: their hand-rolled schemas are mutually incompatible
// (this suite creates parties.is_active BOOLEAN while the canonical monolith
// schema uses INTEGER), which made `go test ./...` order-dependent.
const r4ScratchDBName = "r4_wavea_gotv"

var (
	r4ScratchOnce sync.Once
	r4ScratchDSN  string
	r4ScratchErr  error
)

// r4WithDBName retargets a lib/pq DSN (URL or keyword/value form) at a
// different database name.
func r4WithDBName(dsn, name string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("parse DSN URL: %w", err)
		}
		u.Path = "/" + name
		return u.String(), nil
	}
	fields := strings.Fields(dsn)
	replaced := false
	for i, f := range fields {
		if strings.HasPrefix(f, "dbname=") {
			fields[i] = "dbname=" + name
			replaced = true
		}
	}
	if !replaced {
		fields = append(fields, "dbname="+name)
	}
	return strings.Join(fields, " "), nil
}

// r4TestDB returns a handle to this suite's own scratch PostgreSQL database
// or skips. Set R4_TEST_DB to a lib/pq DSN used as the admin/server
// connection (e.g. "host=/path dbname=wavea user=postgres sslmode=disable");
// the scratch database (r4ScratchDBName) is provisioned underneath it.
func r4TestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("R4_TEST_DB")
	if dsn == "" {
		t.Skip("R4_TEST_DB not set — skipping PG-backed regression test")
	}
	// Provision a fresh per-suite scratch database once per test process.
	r4ScratchOnce.Do(func() {
		admin, err := sql.Open("postgres", dsn)
		if err != nil {
			r4ScratchErr = fmt.Errorf("open admin: %w", err)
			return
		}
		defer admin.Close()
		if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + r4ScratchDBName + ` WITH (FORCE)`); err != nil {
			r4ScratchErr = fmt.Errorf("drop scratch db: %w", err)
			return
		}
		if _, err := admin.Exec(`CREATE DATABASE ` + r4ScratchDBName); err != nil {
			r4ScratchErr = fmt.Errorf("create scratch db: %w", err)
			return
		}
		if r4ScratchDSN, r4ScratchErr = r4WithDBName(dsn, r4ScratchDBName); r4ScratchErr != nil {
			return
		}
	})
	if r4ScratchErr != nil {
		t.Fatalf("provision scratch db: %v", r4ScratchErr)
	}
	db, err := sql.Open("postgres", r4ScratchDSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ─── R4-01: LaunchCampaign must fail loudly when no channel adapter exists ──

func TestR401_LaunchCampaignNoProviderFailsLoud(t *testing.T) {
	db := r4TestDB(t)
	// The LogAdapter escape hatch is dev-only and OFF here.
	t.Setenv("GOTV_ALLOW_LOG_ADAPTER", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("INEC_ENV", "")

	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS parties (id SERIAL PRIMARY KEY, code TEXT UNIQUE, name TEXT, is_active BOOLEAN DEFAULT TRUE)`,
		`CREATE TABLE IF NOT EXISTS gotv_party_access (party_id INTEGER PRIMARY KEY, rate_limit_per_hour INTEGER, is_active BOOLEAN DEFAULT TRUE)`,
		`CREATE TABLE IF NOT EXISTS gotv_campaigns (
			id SERIAL PRIMARY KEY, campaign_id TEXT UNIQUE NOT NULL, party_id INTEGER NOT NULL,
			name TEXT, campaign_type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','scheduled','active','paused','completed','cancelled','failed')),
			message_template TEXT, message_variant_b TEXT, ab_split_pct INTEGER DEFAULT 0,
			contacts_reached INTEGER DEFAULT 0, contacts_responded INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, completed_at TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS gotv_outreach_log (
			id SERIAL PRIMARY KEY, party_id INTEGER NOT NULL, campaign_id TEXT, contact_id TEXT,
			channel TEXT NOT NULL, direction TEXT NOT NULL DEFAULT 'outbound',
			message_variant TEXT DEFAULT 'a', status TEXT NOT NULL DEFAULT 'queued',
			message_id TEXT, error_detail TEXT, latency_ms INTEGER DEFAULT 0, cost_kobo INTEGER DEFAULT 0,
			sent_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, delivered_at TIMESTAMP, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	cid := fmt.Sprintf("r401-%d", time.Now().UnixNano())
	if _, err := db.Exec(`INSERT INTO parties (code, name) VALUES ($1, 'R4 Party') ON CONFLICT (code) DO NOTHING`, "R401"); err != nil {
		t.Fatalf("seed party: %v", err)
	}
	var pid int
	db.QueryRow(`SELECT id FROM parties WHERE code='R401'`).Scan(&pid)
	if _, err := db.Exec(`INSERT INTO gotv_campaigns (campaign_id, party_id, name, campaign_type, status, message_template)
		VALUES ($1, $2, 'c', 'sms', 'active', 'hi')`, cid, pid); err != nil {
		t.Fatalf("seed campaign: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO gotv_outreach_log (party_id, campaign_id, contact_id, channel, status)
		VALUES ($1, $2, 'c1', 'sms', 'queued')`, pid, cid); err != nil {
		t.Fatalf("seed outreach: %v", err)
	}

	eng := NewDispatchEngine(db, nil, nil, 1) // NO adapters registered
	err := eng.LaunchCampaign(context.Background(), cid, pid)
	if err == nil {
		t.Fatal("R4-01: LaunchCampaign must return an error when no provider is configured (silent LogAdapter fallback)")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM gotv_campaigns WHERE campaign_id=$1`, cid).Scan(&status); err != nil {
		t.Fatalf("read campaign: %v", err)
	}
	if status != "failed" {
		t.Fatalf("R4-01: campaign status = %q, want 'failed'", status)
	}
	var errDetail sql.NullString
	var oStatus string
	if err := db.QueryRow(`SELECT status, error_detail FROM gotv_outreach_log WHERE campaign_id=$1`, cid).Scan(&oStatus, &errDetail); err != nil {
		t.Fatalf("read outreach: %v", err)
	}
	if oStatus != "failed" || !errDetail.Valid || errDetail.String == "" {
		t.Fatalf("R4-01: queued message must be marked failed with explicit error, got status=%q err=%v", oStatus, errDetail)
	}
}

// ─── R4-02: SMSAdapter must fail closed without AFRICASTALKING_USERNAME ─────

func TestR402_SMSAdapterRequiresUsername(t *testing.T) {
	hit := false
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		gotBody = string(buf)
		w.WriteHeader(201)
		fmt.Fprint(w, `{"SMSMessageData":{"Message":"Sent"}}`)
	}))
	defer srv.Close()

	// No username configured → must fail WITHOUT sending.
	a := NewSMSAdapter("africastalking", srv.URL, "key", "SENDER", "")
	res := a.Send(context.Background(), OutboundMessage{Phone: "+234", Template: "hi", Channel: "sms"})
	if res.Status != "failed" {
		t.Fatalf("R4-02: send without AT username must fail, got %q", res.Status)
	}
	if hit {
		t.Fatalf("R4-02: HTTP request was sent despite missing username (body=%s)", gotBody)
	}

	// With a username, the real value — never the hardcoded "sandbox" — is sent.
	a2 := NewSMSAdapter("africastalking", srv.URL, "key", "SENDER", "myprodapp")
	res2 := a2.Send(context.Background(), OutboundMessage{Phone: "+234", Template: "hi", Channel: "sms"})
	if res2.Status == "failed" {
		t.Fatalf("unexpected failure with username set: %s", res2.Error)
	}
	if !hit || gotBody == "" {
		t.Fatal("expected HTTP request with username set")
	}
	if contains(gotBody, "username=sandbox") {
		t.Fatal("R4-02: hardcoded sandbox username still used")
	}
	if !contains(gotBody, "username=myprodapp") {
		t.Fatalf("R4-02: configured username not used, body=%s", gotBody)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// ─── R4-03: webhook signature verification fails closed; Twilio = SHA1/b64 ──

func TestR403_SignaturesFailClosedWhenSecretUnset(t *testing.T) {
	InitWebhookSecrets("", "", "")
	t.Cleanup(func() { InitWebhookSecrets("", "", "") })

	if VerifyATSignature([]byte("x"), "sig") {
		t.Error("R4-03: VerifyATSignature must return FALSE when secret unset")
	}
	if VerifyTwilioSignature("https://h/x", map[string]string{"a": "b"}, "sig") {
		t.Error("R4-03: VerifyTwilioSignature must return FALSE when secret unset")
	}
	if VerifyWhatsAppSignature([]byte("x"), "sha256=sig") {
		t.Error("R4-03: VerifyWhatsAppSignature must return FALSE when secret unset")
	}
	if at, tw, wa := WebhookSecretsConfigured(); at || tw || wa {
		t.Error("R4-03: WebhookSecretsConfigured must report all-unset")
	}
}

func TestR403_TwilioRealAlgorithm(t *testing.T) {
	// Per https://www.twilio.com/docs/usage/security: HMAC-SHA1 over
	// URL + sorted params, base64-encoded, keyed by the account Auth Token.
	token := "test-auth-token"
	InitWebhookSecrets("", token, "")
	t.Cleanup(func() { InitWebhookSecrets("", "", "") })

	url := "https://example.com/gotv/webhooks/delivery/twilio"
	params := map[string]string{"MessageSid": "SM123", "MessageStatus": "delivered"}
	data := url + "MessageSidSM123" + "MessageStatusdelivered"
	mac := hmac.New(sha1.New, []byte(token))
	mac.Write([]byte(data))
	goodSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !VerifyTwilioSignature(url, params, goodSig) {
		t.Fatal("R4-03: real Twilio signature (HMAC-SHA1/base64) must verify")
	}
	// The old broken algorithm (HMAC-SHA256/hex) must NOT verify.
	mac256 := hmac.New(sha256.New, []byte(token))
	mac256.Write([]byte(data))
	oldSig := hex.EncodeToString(mac256.Sum(nil))
	if VerifyTwilioSignature(url, params, oldSig) {
		t.Fatal("R4-03: legacy SHA256/hex signature must be rejected")
	}
	if VerifyTwilioSignature(url, map[string]string{"MessageSid": "SM999", "MessageStatus": "delivered"}, goodSig) {
		t.Fatal("R4-03: tampered params must fail verification")
	}
}

// ─── R4-06: gateway trust headers require the shared internal token ────────

func TestR406_GatewayTrustRequiresInternalToken(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "")
	t.Setenv("GOTV_INTERNAL_TOKEN", "")
	t.Setenv("GOTV_GATEWAY_SECRET", "")

	am := NewAuthMiddleware(nil, AuthConfig{})
	spoof := func(headers map[string]string) error {
		r := httptest.NewRequest("GET", "/gotv/campaigns", nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		_, _, err := am.Authenticate(r)
		return err
	}

	// Spoofed gateway headers with NO token must be rejected (fail closed
	// when the shared secret is unset).
	if err := spoof(map[string]string{"X-Internal-Service": "gateway", "X-Party-ID": "7"}); err == nil {
		t.Fatal("R4-06: spoofed gateway headers accepted without internal token")
	}

	// With the secret configured, a wrong token is still rejected...
	t.Setenv("GOTV_INTERNAL_TOKEN", "s3cret-shared-token")
	am2 := NewAuthMiddleware(nil, AuthConfig{})
	r := httptest.NewRequest("GET", "/gotv/campaigns", nil)
	r.Header.Set("X-Internal-Service", "gateway")
	r.Header.Set("X-Party-ID", "7")
	r.Header.Set("X-Internal-Token", "wrong")
	if _, _, err := am2.Authenticate(r); err == nil {
		t.Fatal("R4-06: wrong internal token accepted")
	}
	// ...and the correct token authenticates.
	r2 := httptest.NewRequest("GET", "/gotv/campaigns", nil)
	r2.Header.Set("X-Internal-Service", "gateway")
	r2.Header.Set("X-Party-ID", "7")
	r2.Header.Set("X-Internal-Token", "s3cret-shared-token")
	pid, _, err := am2.Authenticate(r2)
	if err != nil || pid != 7 {
		t.Fatalf("R4-06: valid internal token rejected: %v", err)
	}
}

// ─── R4-43: webhook URL validation + raw-secret HMAC + event-only header ────

func TestR443_ValidateWebhookURL(t *testing.T) {
	t.Setenv("APP_ENV", "production") // https-only in production
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"http://169.254.169.254/latest/meta-data", true}, // link-local metadata
		{"https://127.0.0.1/hook", true},                  // loopback
		{"https://10.0.0.5/hook", true},                   // RFC1918
		{"https://192.168.1.1/hook", true},                // RFC1918
		{"https://localhost/hook", true},                  // localhost
		{"http://203.0.113.10/hook", true},                // plain http in prod
		{"ftp://203.0.113.10/hook", true},                 // bad scheme
		{"not-a-url", true},
		{"https://203.0.113.10/hook", false}, // public https IP, no DNS needed
	}
	for _, c := range cases {
		err := ValidateWebhookURL(c.url)
		if c.wantErr && err == nil {
			t.Errorf("R4-43: %s must be rejected", c.url)
		}
		if !c.wantErr && err != nil {
			t.Errorf("R4-43: %s must be accepted, got %v", c.url, err)
		}
	}
}

func TestR443_WebhookDeliveryRawSecretAndEventHeader(t *testing.T) {
	db := r4TestDB(t)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS gotv_webhooks (
		id SERIAL PRIMARY KEY, party_id INTEGER NOT NULL, url TEXT NOT NULL, secret TEXT NOT NULL,
		event_types TEXT[] NOT NULL, is_active BOOLEAN DEFAULT TRUE, failure_count INTEGER DEFAULT 0,
		last_success_at TIMESTAMP, last_failure_at TIMESTAMP, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("ddl: %v", err)
	}

	type captured struct {
		sig, event, body string
	}
	ch := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		ch <- captured{r.Header.Get("X-GOTV-Signature"), r.Header.Get("X-GOTV-Event"), string(buf)}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rawSecret := "subscriber-known-secret"
	if _, err := db.Exec(`INSERT INTO gotv_webhooks (party_id, url, secret, event_types)
		VALUES (999001, $1, $2, ARRAY['campaign.completed'])`, srv.URL, rawSecret); err != nil {
		t.Fatalf("insert webhook: %v", err)
	}

	wm := NewWebhookManager(db)
	wm.Emit(999001, "campaign.completed", map[string]interface{}{"campaign_id": "c1"})

	select {
	case got := <-ch:
		// (c) X-GOTV-Event must be the event TYPE ONLY, never the payload.
		if got.event != "campaign.completed" {
			t.Fatalf("R4-43: X-GOTV-Event = %q, want event type only", got.event)
		}
		// (b) HMAC must verify with the RAW registered secret.
		mac := hmac.New(sha256.New, []byte(rawSecret))
		mac.Write([]byte(got.body))
		want := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(got.sig), []byte(want)) {
			t.Fatalf("R4-43: signature %q does not verify with raw secret (want %q)", got.sig, want)
		}
		var payload WebhookPayload
		if err := json.Unmarshal([]byte(got.body), &payload); err != nil || payload.Event != "campaign.completed" {
			t.Fatalf("payload malformed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("webhook delivery not received")
	}
}
