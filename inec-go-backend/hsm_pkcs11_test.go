package main

import (
	"crypto/sha256"
	"os"
	"testing"
)

// TestPKCS11SignerAgainstSoftHSM exercises the real PKCS#11 signing path.
// It runs only when HSM_PKCS11_LIB points at an initialised token (e.g. SoftHSM
// in CI). Otherwise it is skipped so the default `go test` stays green.
func TestPKCS11SignerAgainstSoftHSM(t *testing.T) {
	if os.Getenv("HSM_PKCS11_LIB") == "" {
		t.Skip("HSM_PKCS11_LIB not set; skipping real PKCS#11 test")
	}

	signer, err := NewPKCS11Signer()
	if err != nil {
		t.Fatalf("NewPKCS11Signer: %v", err)
	}
	defer signer.Close()

	if signer.PublicKey() == nil {
		t.Fatal("expected a public key from the token")
	}

	digest := sha256.Sum256([]byte("INEC result submission PU-24-05-07-011"))
	sig, err := signer.Sign(digest[:])
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) < 64 {
		t.Fatalf("expected >=64 byte P-256 signature, got %d", len(sig))
	}
	if !signer.Verify(digest[:], sig) {
		t.Fatal("valid signature failed verification")
	}

	// Tamper detection: flip a byte in the digest, signature must not verify.
	bad := digest
	bad[0] ^= 0xFF
	if signer.Verify(bad[:], sig) {
		t.Fatal("signature verified against a tampered digest")
	}
}
