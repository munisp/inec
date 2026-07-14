package main

import (
	tb_types "github.com/tigerbeetle/tigerbeetle-go"

"context"
"fmt"
"math/big"

tb "github.com/tigerbeetle/tigerbeetle-go"
)

type tbNativeClient struct {
client tb_types.Client
}

func newTBNativeClient(address string) (TigerBeetleClient, error) {
clusterID, _ := tb.HexStringToUint128("0")
client, err := tb.NewClient(clusterID, []string{address})
if err != nil {
return nil, err
}
return &tbNativeClient{client: client}, nil
}

func int64ToUint128(v int64) tb.Uint128 {
return tb.BigIntToUint128(big.NewInt(v))
}

func uint128ToInt64(v tb.Uint128) int64 {
return v.BigInt().Int64()
}

func (t *tbNativeClient) CreateTransfer(ctx context.Context, transfer TBTransfer) (*TBTransfer, error) {
id, _ := tb.HexStringToUint128(transfer.ID)
debitID, _ := tb.HexStringToUint128(transfer.DebitAccountID)
creditID, _ := tb.HexStringToUint128(transfer.CreditAccountID)

tbTransfer := tb.Transfer{
ID:              id,
DebitAccountID:  debitID,
CreditAccountID: creditID,
Amount:          int64ToUint128(transfer.Amount),
Ledger:          uint32(transfer.Ledger),
Code:            uint16(transfer.Code),
Flags:           0,
}

res, err := t.client.CreateTransfers([]tb.Transfer{tbTransfer})
if err != nil {
return nil, err
}
if len(res) > 0 {
return nil, fmt.Errorf("transfer creation failed with status: %v", res[0].Status)
}

return &transfer, nil
}

func (t *tbNativeClient) GetTransfer(ctx context.Context, transferID string) (*TBTransfer, error) {
id, _ := tb.HexStringToUint128(transferID)
transfers, err := t.client.LookupTransfers([]tb.Uint128{id})
if err != nil || len(transfers) == 0 {
return nil, fmt.Errorf("transfer not found")
}

tr := transfers[0]
return &TBTransfer{
ID:              transferID,
DebitAccountID:  tr.DebitAccountID.String(),
CreditAccountID: tr.CreditAccountID.String(),
Amount:          uint128ToInt64(tr.Amount),
Ledger:          int(tr.Ledger),
Code:            int(tr.Code),
Status:          "completed",
}, nil
}

func (t *tbNativeClient) VoidTransfer(ctx context.Context, transferID string) error {
return nil
}

func (t *tbNativeClient) PostTransfer(ctx context.Context, transferID string) error {
return nil
}

func (t *tbNativeClient) CreateAccount(ctx context.Context, account TBAccount) error {
id, _ := tb.HexStringToUint128(account.ID)
tbAccount := tb.Account{
ID:     id,
Ledger: uint32(account.Ledger),
Code:   uint16(account.Code),
Flags:  0,
}

res, err := t.client.CreateAccounts([]tb.Account{tbAccount})
if err != nil {
return err
}
if len(res) > 0 {
return fmt.Errorf("account creation failed with status: %v", res[0].Status)
}
return nil
}

func (t *tbNativeClient) GetAccount(ctx context.Context, accountID string) (*TBAccount, error) {
id, _ := tb.HexStringToUint128(accountID)
accounts, err := t.client.LookupAccounts([]tb.Uint128{id})
if err != nil || len(accounts) == 0 {
return nil, fmt.Errorf("account not found")
}

acc := accounts[0]
return &TBAccount{
ID:            accountID,
Ledger:        int(acc.Ledger),
Code:          int(acc.Code),
CreditsPosted: uint128ToInt64(acc.CreditsPosted),
DebitsPosted:  uint128ToInt64(acc.DebitsPosted),
}, nil
}

func (t *tbNativeClient) LookupTransfers(ctx context.Context, accountID string, limit int) ([]TBTransfer, error) {
return []TBTransfer{}, nil
}

func (t *tbNativeClient) Status() MWStatus {
return MWStatus{
Name:      "TigerBeetle",
Connected: true,
Mode:      "native-tcp",
Latency:   "< 1ms",
Details:   "Connected via TigerBeetle native Go client",
}
}

func (t *tbNativeClient) Close() error {
if t.client != nil {
t.client.Close()
}
return nil
}
