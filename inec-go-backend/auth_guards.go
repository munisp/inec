package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
)

// telcoWebhookSecretEnv is the shared-secret env var used to authenticate
// telecom/aggregator callbacks (Africa's Talking, Twilio, SMS aggregators).
const telcoWebhookSecretEnv = "TELCO_WEBHOOK_SECRET"

// verifyProviderSignature checks an HMAC-SHA256 hex signature over a raw
// request body using a constant-time comparison.
func verifyProviderSignature(secret, body, signature string) bool {
	if secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(strings.TrimSpace(signature))), []byte(expected))
}

// readAndRestoreBody reads the full request body (bounded) and restores it so
// the wrapped handler can decode it again.
func readAndRestoreBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unable to read request body")
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, true
}

// telcoProviderAuth wraps telco/aggregator callback handlers (USSD gateways,
// inbound-SMS webhooks, IVR callbacks). These channels are initiated by
// telecom providers that cannot carry voter JWTs, so instead of user auth
// they authenticate the *provider* via a shared-secret HMAC-SHA256 signature
// over the raw request body.
//
// FAIL CLOSED: if TELCO_WEBHOOK_SECRET is not configured the endpoint is
// disabled (503) rather than left open to anonymous internet traffic — the
// same pattern as the IReV receipt webhook. Providers sign with the
// X-Telco-Signature header (lowercase hex HMAC-SHA256 of the raw body);
// Meta-style X-Hub-Signature-256 ("sha256=<hex>") is also accepted.
func telcoProviderAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := strings.TrimSpace(os.Getenv(telcoWebhookSecretEnv))
		if secret == "" {
			writeError(w, http.StatusServiceUnavailable, "telco webhook verification is not configured")
			return
		}
		body, ok := readAndRestoreBody(w, r)
		if !ok {
			return
		}
		signature := strings.TrimSpace(r.Header.Get("X-Telco-Signature"))
		if signature == "" {
			signature = strings.TrimPrefix(strings.TrimSpace(r.Header.Get("X-Hub-Signature-256")), "sha256=")
		}
		if !verifyProviderSignature(secret, string(body), signature) {
			writeError(w, http.StatusUnauthorized, "invalid telco webhook signature")
			return
		}
		handler(w, r)
	}
}

// authRequired wraps a handler to require any authenticated user.
func authRequired(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := getUserFromContext(r); !ok {
			writeError(w, 401, "authentication required")
			return
		}
		handler(w, r)
	}
}

// roleRequired wraps a handler to require specific roles.
func roleRequired(handler http.HandlerFunc, roles ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := guardRole(w, r, roles...); !ok {
			return
		}
		handler(w, r)
	}
}

// adminOnly is a shorthand for requiring admin role.
func adminOnly(handler http.HandlerFunc) http.HandlerFunc {
	return roleRequired(handler, "admin")
}

// staffOnly requires admin, presiding_officer, or collation_officer roles.
func staffOnly(handler http.HandlerFunc) http.HandlerFunc {
	return roleRequired(handler, "admin", "presiding_officer", "collation_officer")
}

// readAuth requires any authenticated user for read operations on sensitive data.
func readAuth(handler http.HandlerFunc) http.HandlerFunc {
	return authRequired(handler)
}

// writeAuth requires admin or specific officer roles for write operations.
func writeAuth(handler http.HandlerFunc) http.HandlerFunc {
	return roleRequired(handler, "admin", "presiding_officer", "collation_officer")
}

// adminOrOfficer requires admin or any officer role for management operations.
func adminOrOfficer(handler http.HandlerFunc) http.HandlerFunc {
	return roleRequired(handler, "admin", "presiding_officer", "collation_officer", "observer")
}

// handlePromoteUser allows admins to assign elevated roles to existing users.
func handlePromoteUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := guardRole(w, r, "admin"); !ok {
		return
	}
	var req struct {
		UserID int `json:"user_id" validate:"required,gt=0"`
		// SEC-14: oneof list must match the users.role CHECK (migrations/000050)
		// — admins can now assign the officer roles the guards reference.
		Role string `json:"role" validate:"required,oneof=admin presiding_officer collation_officer returning_officer ict_officer security dpo officer observer public"`
	}
	if !decodeAndValidateBody(w, r, &req) {
		return
	}
	if _, err := dbExecCtx(r.Context(), "UPDATE users SET role=? WHERE id=?", req.Role, req.UserID); err != nil {
		writeError(w, 500, "failed to update user role")
		return
	}
	auditWrite("USER_PROMOTED", "user", "", r, map[string]interface{}{"user_id": req.UserID, "new_role": req.Role})
	writeJSON(w, 200, M{"message": "User role updated", "role": req.Role})
}
