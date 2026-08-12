package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"
)

func validTestAnchor() EvidenceAnchorV1 {
	anchor := EvidenceAnchorV1{
		SchemaVersion:   anchorSchemaVersion,
		AnchorType:      "result_event",
		ElectionID:      1,
		ResultID:        10,
		EventHash:       strings.Repeat("a", 64),
		PayloadSHA256:   strings.Repeat("b", 64),
		PriorEventHash:  strings.Repeat("c", 64),
		Signature:       "c2lnbmVkLWV2aWRlbmNl",
		SignerKeyID:     "inec-key-v1",
		PolicyVersionID: 2,
		CreatedAt:       "2026-07-26T12:00:00Z",
	}
	anchorID, _ := deterministicAnchorID(anchor)
	anchor.AnchorID = anchorID
	return anchor
}

func TestEvidenceAnchorValidationAndDeterministicID(t *testing.T) {
	first := validTestAnchor()
	if err := validateAnchor(&first); err != nil {
		t.Fatalf("expected valid signed anchor: %v", err)
	}
	second := validTestAnchor()
	if first.AnchorID != second.AnchorID || len(first.AnchorID) != 64 {
		t.Fatalf("expected deterministic anchor ID, got %q and %q", first.AnchorID, second.AnchorID)
	}
	second.Signature = "ZGlmZmVyZW50LXNpZ25hdHVyZQ=="
	changedID, err := deterministicAnchorID(second)
	if err != nil {
		t.Fatalf("calculate changed anchor ID: %v", err)
	}
	if changedID == first.AnchorID {
		t.Fatal("anchor ID must bind the signer output")
	}
}

func TestEvidenceAnchorValidationRejectsUnsafePayload(t *testing.T) {
	anchor := validTestAnchor()
	anchor.EventHash = "not-a-hash"
	if err := validateAnchor(&anchor); err == nil {
		t.Fatal("invalid event hash must be rejected")
	}
	anchor = validTestAnchor()
	anchor.Signature = "not base64!"
	if err := validateAnchor(&anchor); err == nil {
		t.Fatal("invalid signature encoding must be rejected")
	}
	anchor = validTestAnchor()
	anchor.CreatedAt = "tomorrow"
	if err := validateAnchor(&anchor); err == nil {
		t.Fatal("non-RFC3339 timestamp must be rejected")
	}
}

func selfSignedCert(t *testing.T, publicKey any, signer any, commonName string) *x509.Certificate {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, signer)
	if err != nil {
		t.Fatalf("create self-signed certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse self-signed certificate: %v", err)
	}
	return cert
}

func signTestAnchor(t *testing.T, anchor *EvidenceAnchorV1, sign func(digest []byte) []byte) {
	t.Helper()
	digest, err := canonicalSigningDigest(anchor)
	if err != nil {
		t.Fatalf("canonical signing digest: %v", err)
	}
	anchor.Signature = base64.StdEncoding.EncodeToString(sign(digest))
}

func TestVerifySignatureOverAnchorAcceptsValidECDSASignature(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}
	cert := selfSignedCert(t, &key.PublicKey, key, "inec-signer")
	anchor := validTestAnchor()
	anchor.SignerKeyID = cert.Subject.CommonName
	signTestAnchor(t, &anchor, func(digest []byte) []byte {
		sig, err := ecdsa.SignASN1(rand.Reader, key, digest)
		if err != nil {
			t.Fatalf("sign anchor: %v", err)
		}
		return sig
	})
	if err := verifySignatureOverAnchor(cert, &anchor); err != nil {
		t.Fatalf("valid ECDSA signature must verify: %v", err)
	}
}

func TestVerifySignatureOverAnchorAcceptsValidRSASignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	cert := selfSignedCert(t, &key.PublicKey, key, "inec-signer")
	anchor := validTestAnchor()
	anchor.SignerKeyID = cert.Subject.CommonName
	signTestAnchor(t, &anchor, func(digest []byte) []byte {
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest)
		if err != nil {
			t.Fatalf("sign anchor: %v", err)
		}
		return sig
	})
	if err := verifySignatureOverAnchor(cert, &anchor); err != nil {
		t.Fatalf("valid RSA signature must verify: %v", err)
	}
}

func TestVerifySignatureOverAnchorRejectsForgery(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}
	cert := selfSignedCert(t, &key.PublicKey, key, "inec-signer")

	// Arbitrary base64 blob (the previous silent-mockware acceptance case).
	anchor := validTestAnchor()
	anchor.SignerKeyID = cert.Subject.CommonName
	if err := verifySignatureOverAnchor(cert, &anchor); err == nil {
		t.Fatal("arbitrary base64 blob must not anchor as a signed commitment")
	}

	// Valid signature over a tampered payload must be rejected.
	anchor = validTestAnchor()
	anchor.SignerKeyID = cert.Subject.CommonName
	signTestAnchor(t, &anchor, func(digest []byte) []byte {
		sig, err := ecdsa.SignASN1(rand.Reader, key, digest)
		if err != nil {
			t.Fatalf("sign anchor: %v", err)
		}
		return sig
	})
	anchor.EventHash = strings.Repeat("d", 64)
	if err := verifySignatureOverAnchor(cert, &anchor); err == nil {
		t.Fatal("signature over tampered commitment must be rejected")
	}

	// Valid signature from a different key must be rejected.
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate second ECDSA key: %v", err)
	}
	anchor = validTestAnchor()
	anchor.SignerKeyID = cert.Subject.CommonName
	signTestAnchor(t, &anchor, func(digest []byte) []byte {
		sig, err := ecdsa.SignASN1(rand.Reader, other, digest)
		if err != nil {
			t.Fatalf("sign anchor: %v", err)
		}
		return sig
	})
	if err := verifySignatureOverAnchor(cert, &anchor); err == nil {
		t.Fatal("signature from an unrelated key must be rejected")
	}
}

func TestSignerKeyIDMatchesInvokerIdentity(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}
	cert := selfSignedCert(t, &key.PublicKey, key, "inec-signer")
	fingerprint := sha256.Sum256(cert.Raw)
	invokerID := "x509::CN=inec-signer::CN=ca"
	for _, accepted := range []string{"inec-signer", invokerID, hex.EncodeToString(fingerprint[:])} {
		if !signerKeyIDMatches(cert, invokerID, accepted) {
			t.Fatalf("signer_key_id %q must match the invoker identity", accepted)
		}
	}
	for _, rejected := range []string{"", "attacker-key", strings.Repeat("0", 64)} {
		if signerKeyIDMatches(cert, invokerID, rejected) {
			t.Fatalf("signer_key_id %q must not match the invoker identity", rejected)
		}
	}
}

func TestNormalizeMSPsRequiresStableNonEmptySet(t *testing.T) {
	MSPs, err := normalizeMSPs([]string{"ObserverMSP", "INECMSP", "ObserverMSP"})
	if err != nil {
		t.Fatalf("normalize MSPs: %v", err)
	}
	if strings.Join(MSPs, ",") != "INECMSP,ObserverMSP" {
		t.Fatalf("unexpected normalized MSPs: %v", MSPs)
	}
	if _, err := normalizeMSPs([]string{"INECMSP", ""}); err == nil {
		t.Fatal("empty MSP identifier must be rejected")
	}
}
