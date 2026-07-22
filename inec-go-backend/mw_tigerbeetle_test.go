package main

import (
	"context"
	"testing"
)

func TestInMemoryTigerBeetleClientLedgerLifecycle(t *testing.T) {
	client := newInMemoryTigerBeetleClient()
	ctx := context.Background()
	for _, account := range []TBAccount{{ID: "debit", Ledger: 1, Code: 1}, {ID: "credit", Ledger: 1, Code: 1}} {
		if err := client.CreateAccount(ctx, account); err != nil {
			t.Fatalf("create account %q: %v", account.ID, err)
		}
	}

	transfer, err := client.CreateTransfer(ctx, TBTransfer{ID: "transfer-1", DebitAccountID: "debit", CreditAccountID: "credit", Amount: 125, Ledger: 1, Code: 1})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if transfer.Status != "posted" || transfer.Timestamp == "" {
		t.Fatalf("unexpected transfer: %#v", transfer)
	}
	debit, err := client.GetAccount(ctx, "debit")
	if err != nil {
		t.Fatalf("get debit account: %v", err)
	}
	credit, err := client.GetAccount(ctx, "credit")
	if err != nil {
		t.Fatalf("get credit account: %v", err)
	}
	if debit.DebitsPosted != 125 || credit.CreditsPosted != 125 {
		t.Fatalf("account balances = debit %#v credit %#v", debit, credit)
	}
	if err := client.VoidTransfer(ctx, "transfer-1"); err != nil {
		t.Fatalf("void transfer: %v", err)
	}
	stored, err := client.GetTransfer(ctx, "transfer-1")
	if err != nil {
		t.Fatalf("get transfer: %v", err)
	}
	if stored.Status != "voided" {
		t.Fatalf("transfer status = %q, want voided", stored.Status)
	}
}

func TestInitTigerBeetleClientUsesInMemoryTransportOutsideProduction(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("TIGERBEETLE_ADDRESSES", "")
	t.Setenv("TIGERBEETLE_URL", "")
	client := initTigerBeetleClient()
	if _, ok := client.(*inMemoryTigerBeetleClient); !ok {
		t.Fatalf("client type = %T, want *inMemoryTigerBeetleClient", client)
	}
	if status := client.Status(); !status.Connected || status.Mode != "in-memory (non-production)" {
		t.Fatalf("unexpected in-memory TigerBeetle status: %#v", status)
	}
}
