package main

import (
	tb_types "github.com/tigerbeetle/tigerbeetle-go"

	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	tb "github.com/tigerbeetle/tigerbeetle-go"
)

// tbSDKClient is a real TigerBeetle integration over the official binary-protocol
// Go SDK (github.com/tigerbeetle/tigerbeetle-go), replacing the previous HTTP
// client which targeted a non-existent HTTP gateway. Activated when
// TIGERBEETLE_ADDRESSES is set; otherwise the durable DB-backed ledger is used.
//
// The platform addresses accounts/transfers by human-readable string IDs
// ("inec-operational", "result-42", ...). TigerBeetle uses 128-bit integer IDs,
// so string keys are mapped deterministically to Uint128 via SHA-256, letting
// lookups re-derive the same ID without a separate mapping table.
type tbSDKClient struct {
	client    tb_types.Client
	addresses string
	clusterID uint64
}

// keyToUint128 maps a stable string key to a deterministic non-zero Uint128.
func keyToUint128(key string) tb.Uint128 {
	sum := sha256.Sum256([]byte(key))
	var b [16]byte
	copy(b[:], sum[:16])
	if b == ([16]byte{}) {
		b[0] = 1
	}
	return tb.BytesToUint128(b)
}

func u128ToInt64(v tb.Uint128) int64 {
	lo, _ := v.Uint64()
	return int64(lo)
}

func newTBSDKClient(clusterID uint64, addresses []string) (*tbSDKClient, error) {
	c, err := tb.NewClient(tb.ToUint128(clusterID), addresses)
	if err != nil {
		return nil, fmt.Errorf("tigerbeetle connect: %w", err)
	}
	return &tbSDKClient{client: c, addresses: strings.Join(addresses, ","), clusterID: clusterID}, nil
}

// ensureAccount creates an account for the given key if it does not yet exist.
// The platform assumes accounts are auto-provisioned; TigerBeetle requires them
// to exist before a transfer references them.
func (t *tbSDKClient) ensureAccount(key string, ledger uint32, code uint16) error {
	if ledger == 0 {
		ledger = 1
	}
	if code == 0 {
		code = 1
	}
	res, err := t.client.CreateAccounts([]tb.Account{{
		ID:     keyToUint128(key),
		Ledger: ledger,
		Code:   code,
	}})
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.Status != tb.AccountCreated && r.Status != tb.AccountExists {
			return fmt.Errorf("create account %q: %s", key, r.Status)
		}
	}
	return nil
}

func (t *tbSDKClient) CreateTransfer(_ context.Context, transfer TBTransfer) (*TBTransfer, error) {
	// Reject non-positive amounts: casting a negative int64 to uint64 would wrap
	// to a near-MaxUint64 value and post an astronomical transfer to the ledger.
	if transfer.Amount <= 0 {
		return nil, fmt.Errorf("invalid transfer amount %d: must be positive", transfer.Amount)
	}
	if transfer.ID == "" {
		buf := make([]byte, 16)
		rand.Read(buf)
		transfer.ID = "TB-" + hex.EncodeToString(buf[:8])
	}
	ledger := uint32(transfer.Ledger)
	if ledger == 0 {
		ledger = 1
	}
	code := uint16(transfer.Code)
	if code == 0 {
		code = 1
	}
	if err := t.ensureAccount(transfer.DebitAccountID, ledger, code); err != nil {
		return nil, err
	}
	if err := t.ensureAccount(transfer.CreditAccountID, ledger, code); err != nil {
		return nil, err
	}
	// Create as a pending (two-phase) transfer so it can be posted/voided later,
	// matching the platform's PENDING -> POSTED/VOIDED lifecycle.
	res, err := t.client.CreateTransfers([]tb.Transfer{{
		ID:              keyToUint128(transfer.ID),
		DebitAccountID:  keyToUint128(transfer.DebitAccountID),
		CreditAccountID: keyToUint128(transfer.CreditAccountID),
		Amount:          tb.ToUint128(uint64(transfer.Amount)),
		Ledger:          ledger,
		Code:            code,
		Flags:           tb.TransferFlags{Pending: true}.ToUint16(),
	}})
	if err != nil {
		return nil, err
	}
	for _, r := range res {
		if r.Status != tb.TransferCreated && r.Status != tb.TransferExists {
			return nil, fmt.Errorf("create transfer %q: %s", transfer.ID, r.Status)
		}
	}
	transfer.Status = "PENDING"
	transfer.Timestamp = time.Now().UTC().Format(time.RFC3339)
	return &transfer, nil
}

func (t *tbSDKClient) resolvePending(pendingKey string, flags tb.TransferFlags) error {
	res, err := t.client.CreateTransfers([]tb.Transfer{{
		ID:        tb.ID(),
		PendingID: keyToUint128(pendingKey),
		Flags:     flags.ToUint16(),
	}})
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.Status != tb.TransferCreated && r.Status != tb.TransferExists {
			return fmt.Errorf("resolve pending %q: %s", pendingKey, r.Status)
		}
	}
	return nil
}

func (t *tbSDKClient) PostTransfer(_ context.Context, transferID string) error {
	return t.resolvePending(transferID, tb.TransferFlags{PostPendingTransfer: true})
}

func (t *tbSDKClient) VoidTransfer(_ context.Context, transferID string) error {
	return t.resolvePending(transferID, tb.TransferFlags{VoidPendingTransfer: true})
}

func (t *tbSDKClient) GetTransfer(_ context.Context, transferID string) (*TBTransfer, error) {
	found, err := t.client.LookupTransfers([]tb.Uint128{keyToUint128(transferID)})
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("transfer not found: %s", transferID)
	}
	return tbTransferFromSDK(transferID, found[0]), nil
}

func tbTransferFromSDK(id string, tr tb.Transfer) *TBTransfer {
	status := "POSTED"
	if tr.TransferFlags().Pending {
		status = "PENDING"
	}
	return &TBTransfer{
		ID:              id,
		DebitAccountID:  tr.DebitAccountID.String(),
		CreditAccountID: tr.CreditAccountID.String(),
		Amount:          u128ToInt64(tr.Amount),
		Ledger:          int(tr.Ledger),
		Code:            int(tr.Code),
		Status:          status,
	}
}

func (t *tbSDKClient) CreateAccount(_ context.Context, account TBAccount) error {
	return t.ensureAccount(account.ID, uint32(account.Ledger), uint16(account.Code))
}

func (t *tbSDKClient) GetAccount(_ context.Context, accountID string) (*TBAccount, error) {
	found, err := t.client.LookupAccounts([]tb.Uint128{keyToUint128(accountID)})
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}
	a := found[0]
	return &TBAccount{
		ID:             accountID,
		Ledger:         int(a.Ledger),
		Code:           int(a.Code),
		CreditsPosted:  u128ToInt64(a.CreditsPosted),
		DebitsPosted:   u128ToInt64(a.DebitsPosted),
		CreditsPending: u128ToInt64(a.CreditsPending),
		DebitsPending:  u128ToInt64(a.DebitsPending),
	}, nil
}

func (t *tbSDKClient) LookupTransfers(_ context.Context, accountID string, limit int) ([]TBTransfer, error) {
	if limit <= 0 {
		limit = 100
	}
	transfers, err := t.client.GetAccountTransfers(tb.AccountFilter{
		AccountID: keyToUint128(accountID),
		Limit:     uint32(limit),
		Flags:     tb.AccountFilterFlags{Debits: true, Credits: true}.ToUint32(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]TBTransfer, 0, len(transfers))
	for _, tr := range transfers {
		out = append(out, *tbTransferFromSDK(tr.ID.String(), tr))
	}
	return out, nil
}

func (t *tbSDKClient) Status() MWStatus {
	lat, err := measureLatency(func() error { return t.client.Nop() })
	if err != nil {
		return MWStatus{Name: "TigerBeetle", Connected: false, Mode: "binary-sdk (unreachable)", Details: err.Error()}
	}
	return MWStatus{
		Name: "TigerBeetle", Connected: true, Mode: "binary-sdk",
		Latency: fmtLatency(lat),
		Details: fmt.Sprintf("cluster=%d addresses=%s", t.clusterID, t.addresses),
	}
}

func (t *tbSDKClient) Close() error {
	if t.client != nil {
		t.client.Close()
	}
	return nil
}

func initTBSDKClient() TigerBeetleClient {
	addrs := envOrDefault("TIGERBEETLE_ADDRESSES", "")
	if addrs == "" {
		return nil
	}
	clusterID := uint64(0)
	if cid := envOrDefault("TIGERBEETLE_CLUSTER_ID", ""); cid != "" {
		if parsed, err := strconv.ParseUint(cid, 10, 64); err == nil {
			clusterID = parsed
		}
	}
	c, err := newTBSDKClient(clusterID, strings.Split(addrs, ","))
	if err != nil {
		log.Warn().Err(err).Str("addresses", addrs).Msg("TigerBeetle binary SDK connect failed, falling back")
		return nil
	}
	s := c.Status()
	if !s.Connected {
		log.Warn().Str("addresses", addrs).Msg("TigerBeetle binary SDK unreachable, falling back")
		c.Close()
		return nil
	}
	log.Info().Str("addresses", addrs).Uint64("cluster", clusterID).Msg("TigerBeetle connected via binary SDK")
	return c
}
