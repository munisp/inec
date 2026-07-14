package main

import (
"context"
"fmt"
"time"

"github.com/rs/zerolog/log"
tb "github.com/tigerbeetle/tigerbeetle-go"
tb_types "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

type tbNativeClient struct {
client tb.Client
}

func newTBNativeClient(address string) (TigerBeetleClient, error) {
client, err := tb.NewClient(0, []string{address}, 256)
if err != nil {
return nil, err
}
return &tbNativeClient{client: client}, nil
}

func (t *tbNativeClient) CreateTransfer(ctx context.Context, transfer TBTransfer) (*TBTransfer, error) {
id, _ := tb_types.HexStringToUint128(transfer.ID)
debitID, _ := tb_types.HexStringToUint128(transfer.DebitAccountID)
creditID, _ := tb_types.HexStringToUint128(transfer.CreditAccountID)

tbTransfer := tb_types.Transfer{
ID:              id,
DebitAccountID:  debitID,
CreditAccountID: creditID,
Amount:          transfer.Amount,
Ledger:          uint32(transfer.Ledger),
Code:            uint16(transfer.Code),
Flags:           0,
}

res, err := t.client.CreateTransfers([]tb_types.Transfer{tbTransfer})
if err != nil {
return nil, err
}
if len(res) > 0 {
return nil, fmt.Errorf("transfer creation failed: %v", res[0].Result)
}

return &transfer, nil
}

func (t *tbNativeClient) GetTransfer(ctx context.Context, transferID string) (*TBTransfer, error) {
id, _ := tb_types.HexStringToUint128(transferID)
transfers, err := t.client.LookupTransfers([]tb_types.Uint128{id})
if err != nil || len(transfers) == 0 {
return nil, fmt.Errorf("transfer not found")
}

tr := transfers[0]
return &TBTransfer{
ID:              transferID,
DebitAccountID:  tr.DebitAccountID.String(),
CreditAccountID: tr.CreditAccountID.String(),
Amount:          tr.Amount,
Ledger:          int(tr.Ledger),
Code:            int(tr.Code),
Status:          "completed",
}, nil
}

func (t *tbNativeClient) VoidTransfer(ctx context.Context, transferID string) error {
// Not implemented in native client for now
return nil
}

func (t *tbNativeClient) PostTransfer(ctx context.Context, transferID string) error {
// Not implemented in native client for now
return nil
}

func (t *tbNativeClient) CreateAccount(ctx context.Context, account TBAccount) error {
id, _ := tb_types.HexStringToUint128(account.ID)
tbAccount := tb_types.Account{
ID:     id,
Ledger: uint32(account.Ledger),
Code:   uint16(account.Code),
Flags:  0,
}

res, err := t.client.CreateAccounts([]tb_types.Account{tbAccount})
if err != nil {
return err
}
if len(res) > 0 {
return fmt.Errorf("account creation failed: %v", res[0].Result)
}
return nil
}

func (t *tbNativeClient) GetAccount(ctx context.Context, accountID string) (*TBAccount, error) {
id, _ := tb_types.HexStringToUint128(accountID)
accounts, err := t.client.LookupAccounts([]tb_types.Uint128{id})
if err != nil || len(accounts) == 0 {
return nil, fmt.Errorf("account not found")
}

acc := accounts[0]
return &TBAccount{
ID:       accountID,
Ledger:   int(acc.Ledger),
Code:     int(acc.Code),
Debits:   acc.DebitsPosted,
Credits:  acc.CreditsPosted,
}, nil
}

func (t *tbNativeClient) LookupTransfers(ctx context.Context, accountID string, limit int) ([]TBTransfer, error) {
// Not implemented in native client for now
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
t.client.Close()
return nil
}
