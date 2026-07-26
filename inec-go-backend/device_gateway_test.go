package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func validDeviceGatewayEnvelope(t *testing.T, payload map[string]interface{}) DeviceGatewayEnvelope {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(payloadBytes)
	return DeviceGatewayEnvelope{
		Version:         deviceGatewayEnvelopeVersion,
		DeviceID:        "BVAS-00001",
		ElectionID:      1,
		PollingUnitCode: "PU-001",
		EventType:       "accreditation",
		Sequence:        1,
		Nonce:           base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
		ObservedAt:      time.Now().UTC().Format(time.RFC3339),
		PayloadSHA256:   hex.EncodeToString(h[:]),
		Payload:         payloadBytes,
		Signature:       base64.StdEncoding.EncodeToString(make([]byte, 64)),
	}
}

func TestValidateDeviceGatewayEnvelopeAcceptsRedactedAccreditation(t *testing.T) {
	envelope := validDeviceGatewayEnvelope(t, map[string]interface{}{
		"voter_pvc_hash":  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"biometric_match": true,
		"pvc_verified":    true,
		"method":          "biometric",
	})
	_, nonceHash, payloadHash, err := validateDeviceGatewayEnvelope(&envelope)
	if err != nil {
		t.Fatalf("expected valid envelope, got %v", err)
	}
	if nonceHash == "" || payloadHash != envelope.PayloadSHA256 {
		t.Fatalf("unexpected validation result nonce=%q payload=%q", nonceHash, payloadHash)
	}
}

func TestValidateDeviceGatewayEnvelopeRejectsRawPVC(t *testing.T) {
	envelope := validDeviceGatewayEnvelope(t, map[string]interface{}{
		"voter_pvc_number": "PVC-SENSITIVE-VALUE",
	})
	if _, _, _, err := validateDeviceGatewayEnvelope(&envelope); err == nil {
		t.Fatal("expected raw PVC payload to be rejected")
	}
}

func TestLegacyDeviceIngressBlockedWhenRequired(t *testing.T) {
	oldEnv, hadEnv := os.LookupEnv("BVAS_DEVICE_GATEWAY_REQUIRED")
	t.Cleanup(func() {
		if hadEnv {
			_ = os.Setenv("BVAS_DEVICE_GATEWAY_REQUIRED", oldEnv)
		} else {
			_ = os.Unsetenv("BVAS_DEVICE_GATEWAY_REQUIRED")
		}
	})
	if err := os.Setenv("BVAS_DEVICE_GATEWAY_REQUIRED", "true"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if !rejectLegacyDeviceIngress(w) {
		t.Fatal("expected required gateway mode to reject legacy ingress")
	}
	if w.Code != 410 {
		t.Fatalf("expected 410, got %d", w.Code)
	}
}

func TestDeviceGatewayOperationalStatusRequiresAuthorizedActor(t *testing.T) {
	req := httptest.NewRequest("GET", "/device-gateway/v1/status", nil)
	w := httptest.NewRecorder()
	handleDeviceGatewayOperationalStatus(w, req)
	if w.Code != 403 {
		t.Fatalf("expected unauthenticated device gateway status request to be denied with 403, got %d", w.Code)
	}
}
