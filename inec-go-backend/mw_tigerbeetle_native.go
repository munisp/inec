package main

import (
	"context"
	"fmt"
	"math/big"

	tb "github.com/tigerbeetle/tigerbeetle-go"
	tb_types "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// tbNativeClient implements TigerBeetleClient using the official binary-protocol Go SDK.
type tbNativeClient struct {
	client tb.Client
}

func newTBNativeClient(address string) (TigerBeetleClient, error) {
	clusterID := tb_types.ToUint128(0)
	client, err := tb.NewClient(clusterID, []string{address})
	if err != nil {
		return nil, err
	}
	return &tbNativeClient{client: client}, nil
}

func int64ToUint128(v int64) tb_types.Uint128 {
	b := big.NewInt(v)
	return tb_types.BigIntToUint128(*b)
}

func uint128ToInt64(v tb_types.Uint128) int64 {
	b := v.BigInt()
	return b.Int64()
}

func stringToUint128(s string) tb_types.Uint128 {
	v, err := tb_types.HexStringToUint128(s)
	if err != nil {
		b := big.NewInt(0)
		for i, c := range s {
			b.Add(b, big.NewInt(int64(c)*int64(i+1)))
		}
		return tb_types.BigIntToUint128(*b)
	}
	return v
}

func (t *tbNativeClient) CreateTransfer(_ context.Context, transfer TBTransfer) (*TBTransfer, error) {
	tbTransfer := tb_types.Transfer{
		ID:              stringToUint128(transfer.ID),
		DebitAccountID:  stringToUint128(transfer.DebitAccountID),
		CreditAccountID: stringToUint128(transfer.CreditAccountID),
		Amount:          int64ToUint128(transfer.Amount),
		Ledger:          uint32(transfer.Ledger),
		Code:            uint16(transfer.Code),
	}
	results, err := t.client.CreateTransfers([]tb_types.Transfer{tbTransfer})
	if err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}
	if len(results) > 0 {
		return nil, fmt.Errorf("create transfer failed: %v", results[0].Result)
	}
	transfer.Status = "POSTED"
	return &transfer, nil
}

func (t *tbNativeClient) GetTransfer(_ context.Context, transferID string) (*TBTransfer, error) {
	transfers, err := t.client.LookupTransfers([]tb_types.Uint128{stringToUint128(transferID)})
	if err != nil {
		return nil, fmt.Errorf("lookup transfer: %w", err)
	}
	if len(transfers) == 0 {
		return nil, fmt.Errorf("transfer not found: %s", transferID)
	}
	tr := transfers[0]
	return &TBTransfer{
		ID:              transferID,
		DebitAccountID:  tr.DebitAccountID.String(),
		CreditAccountID: tr.CreditAccountID.String(),
		Amount:          uint128ToInt64(tr.Amount),
		Ledger:          int(tr.Ledger),
		Code:            int(tr.Code),
		Status:          "POSTED",
	}, nil
}

func (t *tbNativeClient) VoidTransfer(_ context.Context, transferID string) error {
	voidTransfer := tb_types.Transfer{
		ID:        stringToUint128(transferID + "-void"),
		PendingID: stringToUint128(transferID),
		Flags:     tb_types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
	}
	_, err := t.client.CreateTransfers([]tb_types.Transfer{voidTransfer})
	return err
}

func (t *tbNativeClient) PostTransfer(_ context.Context, transferID string) error {
	postTransfer := tb_types.Transfer{
		ID:        stringToUint128(transferID + "-post"),
		PendingID: stringToUint128(transferID),
		Flags:     tb_types.TransferFlags{PostPendingTransfer: true}.ToUint16(),
	}
	_, err := t.client.CreateTransfers([]tb_types.Transfer{postTransfer})
	return err
}

func (t *tbNativeClient) CreateAccount(_ context.Context, account TBAccount) error {
	tbAcct := tb_types.Account{
		ID:     stringToUint128(account.ID),
		Ledger: uint32(account.Ledger),
		Code:   uint16(account.Code),
	}
	results, err := t.client.CreateAccounts([]tb_types.Account{tbAcct})
	if err != nil {
		return fmt.Errorf("create account: %w", err)
	}
	if len(results) > 0 {
		return fmt.Errorf("create account failed: %v", results[0].Result)
	}
	return nil
}

func (t *tbNativeClient) GetAccount(_ context.Context, accountID string) (*TBAccount, error) {
	results, err := t.client.LookupAccounts([]tb_types.Uint128{stringToUint128(accountID)})
	if err != nil {
		return nil, fmt.Errorf("lookup account: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}
	a := results[0]
	return &TBAccount{
		ID:             accountID,
		Ledger:         int(a.Ledger),
		Code:           int(a.Code),
		CreditsPosted:  uint128ToInt64(a.CreditsPosted),
		DebitsPosted:   uint128ToInt64(a.DebitsPosted),
		CreditsPending: uint128ToInt64(a.CreditsPending),
		DebitsPending:  uint128ToInt64(a.DebitsPending),
	}, nil
}

func (t *tbNativeClient) LookupTransfers(_ context.Context, accountID string, limit int) ([]TBTransfer, error) {
	filter := tb_types.AccountFilter{
		AccountID: stringToUint128(accountID),
		Limit:     uint32(limit),
	}
	transfers, err := t.client.GetAccountTransfers(filter)
	if err != nil {
		return nil, fmt.Errorf("get account transfers: %w", err)
	}
	result := make([]TBTransfer, 0, len(transfers))
	for _, tr := range transfers {
		result = append(result, TBTransfer{
			Amount: uint128ToInt64(tr.Amount),
			Status: "POSTED",
		})
	}
	return result, nil
}

func (t *tbNativeClient) Status() MWStatus {
	lat, err := measureLatency(func() error {
		return t.client.Nop()
	})
	if err != nil {
		return MWStatus{Name: "TigerBeetle", Connected: false, Mode: "native tigerbeetle-go (unreachable)", Details: err.Error()}
	}
	return MWStatus{
		Name:      "TigerBeetle",
		Connected: true,
		Mode:      "native tigerbeetle-go binary protocol",
		Latency:   fmtLatency(lat),
	}
}

func (t *tbNativeClient) Close() error {
	t.client.Close()
	return nil
}
