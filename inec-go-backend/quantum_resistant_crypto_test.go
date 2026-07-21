package main

import "testing"

func TestMLDSA65ElectionResultSignatureRoundTrip(t *testing.T) {
	keyPair, err := GeneratePQKeyPair(SchemeMLDSA65)
	if err != nil {
		t.Fatalf("GeneratePQKeyPair() error = %v", err)
	}
	payload := []byte(`{"result_id":42,"votes":1234}`)
	signed, err := SignElectionResult(keyPair, "2027-general", "PU-LA-001", payload)
	if err != nil {
		t.Fatalf("SignElectionResult() error = %v", err)
	}
	if !VerifyPQSignature(signed, keyPair.PublicKey, payload) {
		t.Fatal("VerifyPQSignature() returned false for a valid ML-DSA-65 signature")
	}
	if VerifyPQSignature(signed, keyPair.PublicKey, []byte(`{"result_id":42,"votes":1235}`)) {
		t.Fatal("VerifyPQSignature() accepted a modified election-result payload")
	}
}

func TestMLDSA65DocumentSignatureRoundTrip(t *testing.T) {
	keyPair, err := GeneratePQKeyPair(SchemeDilithium3)
	if err != nil {
		t.Fatalf("GeneratePQKeyPair() error = %v", err)
	}
	payload := []byte("INEC post-quantum document signature test")
	signature, err := signDocument(keyPair, payload)
	if err != nil {
		t.Fatalf("signDocument() error = %v", err)
	}
	valid, err := verifyDocumentSignature(payload, signature, keyPair.PublicKey)
	if err != nil {
		t.Fatalf("verifyDocumentSignature() error = %v", err)
	}
	if !valid {
		t.Fatal("verifyDocumentSignature() returned false for a valid ML-DSA-65 signature")
	}
}
