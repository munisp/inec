package main

import (
	"strings"
	"testing"
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
