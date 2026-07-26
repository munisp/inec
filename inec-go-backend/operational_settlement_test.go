package main

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestValidateOperationalSettlementRequestAcceptsBoundDeviceCommitment(t *testing.T) {
	req := operationalSettlementRequest{
		ElectionID:         1,
		DeviceID:           "BVAS-00001",
		CommitmentKind:     "device_logistics",
		AmountMinor:        12500,
		Currency:           "ngn",
		RecipientReference: "approved-operational-recipient-reference",
		EvidenceSHA256:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PurposeReference:   "dispatch-manifest-sha256:placeholder-reference",
		IdempotencyKey:     uuid.NewString(),
	}
	if err := validateOperationalSettlementRequest(&req); err != nil {
		t.Fatalf("expected valid operational settlement request, got %v", err)
	}
	if req.Currency != "NGN" {
		t.Fatalf("expected currency normalization, got %q", req.Currency)
	}
}

func TestValidateOperationalSettlementRequestRejectsInvalidEvidence(t *testing.T) {
	req := operationalSettlementRequest{
		ElectionID:         1,
		DeviceID:           "BVAS-00001",
		CommitmentKind:     "device_reimbursement",
		AmountMinor:        1,
		Currency:           "NGN",
		RecipientReference: "approved-operational-recipient-reference",
		EvidenceSHA256:     "not-a-sha256",
		PurposeReference:   "purpose-reference",
		IdempotencyKey:     uuid.NewString(),
	}
	if err := validateOperationalSettlementRequest(&req); err == nil {
		t.Fatal("expected invalid evidence hash to be rejected")
	}
}

func TestValidateOperationalSettlementRequestEnforcesConfiguredCap(t *testing.T) {
	t.Setenv("OPERATIONAL_SETTLEMENT_MAX_AMOUNT_MINOR", "100")
	req := operationalSettlementRequest{
		ElectionID:         1,
		DeviceID:           "BVAS-00001",
		CommitmentKind:     "device_repair",
		AmountMinor:        101,
		Currency:           "NGN",
		RecipientReference: "approved-operational-recipient-reference",
		EvidenceSHA256:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		PurposeReference:   "purpose-reference",
		IdempotencyKey:     uuid.NewString(),
	}
	if err := validateOperationalSettlementRequest(&req); err == nil {
		t.Fatal("expected amount beyond configured cap to be rejected")
	}
}

func TestMojaloopOperationalSettlementIsDisabledByDefault(t *testing.T) {
	old, present := os.LookupEnv("MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED")
	if err := os.Unsetenv("MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv("MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED", old)
		} else {
			_ = os.Unsetenv("MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED")
		}
	})
	client := initMojaloopClient()
	if client.Status().Connected {
		t.Fatal("Mojaloop must not become connected without explicit authorization")
	}
	if _, err := client.CreateTransfer(context.Background(), MojaTransferRequest{}); err == nil {
		t.Fatal("disabled Mojaloop client must not fabricate a transfer")
	}
}

func TestOperationalMojaloopReadinessReportsUnavailableWhenDisabled(t *testing.T) {
	t.Setenv("MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED", "false")
	if err := operationalSettlementMojaloopReady(); err == nil {
		t.Fatal("expected disabled Mojaloop operational settlement to be unavailable")
	} else if operationalSettlementStatus(err) != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%v)", operationalSettlementStatus(err), err)
	}
}
