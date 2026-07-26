package main

import (
	"strings"
	"testing"
)

func testFabricAnchor() fabricEvidenceAnchor {
	return fabricEvidenceAnchor{
		SchemaVersion:   fabricAnchorSchemaVersion,
		AnchorType:      fabricAnchorTypeEvent,
		ElectionID:      17,
		ResultID:        42,
		EventHash:       strings.Repeat("a", 64),
		PayloadSHA256:   strings.Repeat("b", 64),
		PriorEventHash:  strings.Repeat("c", 64),
		Signature:       "c2lnbmVkLWV2aWRlbmNl", // base64("signed-evidence")
		SignerKeyID:     "inec-evidence-key-v1",
		PolicyVersionID: 3,
		CreatedAt:       "2026-07-26T12:00:00Z",
	}
}

func TestFabricAnchorIDIsDeterministicAndBindsSignedEvidence(t *testing.T) {
	first := testFabricAnchor()
	second := testFabricAnchor()
	firstID, err := calculateFabricAnchorID(first)
	if err != nil {
		t.Fatalf("calculate first Fabric anchor ID: %v", err)
	}
	secondID, err := calculateFabricAnchorID(second)
	if err != nil {
		t.Fatalf("calculate second Fabric anchor ID: %v", err)
	}
	if firstID != secondID || len(firstID) != 64 {
		t.Fatalf("expected deterministic SHA-256 Fabric anchor ID, got %q and %q", firstID, secondID)
	}
	second.Signature = "ZGlmZmVyZW50LXNpZ25hdHVyZQ=="
	changedID, err := calculateFabricAnchorID(second)
	if err != nil {
		t.Fatalf("calculate changed Fabric anchor ID: %v", err)
	}
	if changedID == firstID {
		t.Fatal("Fabric anchor ID must bind the signed evidence, not only the event hash")
	}
}

func TestFabricGatewayConfigFailsClosedWhenRequired(t *testing.T) {
	t.Setenv("FABRIC_ANCHORING_ENABLED", "true")
	t.Setenv("FABRIC_ANCHORING_REQUIRED", "true")
	for _, name := range []string{
		"FABRIC_GATEWAY_ENDPOINT", "FABRIC_TLS_ROOT_CERT_FILE", "FABRIC_CLIENT_CERT_FILE",
		"FABRIC_CLIENT_KEY_FILE", "FABRIC_MSP_ID", "FABRIC_CHANNEL", "FABRIC_CHAINCODE",
	} {
		t.Setenv(name, "")
	}
	if err := fabricGatewayConfigFromEnv().validate(); err == nil {
		t.Fatal("required Fabric anchoring must fail closed when gateway identity configuration is absent")
	}
}

func TestLegacyFabricEngineCannotSynthesizeConsortiumTransaction(t *testing.T) {
	engine := NewProductionFabricEngine(nil)
	if _, _, err := engine.SubmitWithEndorsement("inec-channel", "evidence-anchor", "CreateAnchor", nil, "INECMSP"); err == nil {
		t.Fatal("legacy Fabric facade must not fabricate a local endorsement or transaction")
	}
}
