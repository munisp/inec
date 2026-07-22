package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// TBTransfer is the platform representation of a TigerBeetle transfer.
type TBTransfer struct {
	ID              string `json:"id"`
	DebitAccountID  string `json:"debit_account_id"`
	CreditAccountID string `json:"credit_account_id"`
	Amount          int64  `json:"amount"`
	Ledger          int    `json:"ledger"`
	Code            int    `json:"code"`
	Status          string `json:"status"`
	Timestamp       string `json:"timestamp"`
	UserData        string `json:"user_data"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

// TBAccount is the platform representation of a TigerBeetle account.
type TBAccount struct {
	ID             string `json:"id"`
	Ledger         int    `json:"ledger"`
	Code           int    `json:"code"`
	CreditsPosted  int64  `json:"credits_posted"`
	DebitsPosted   int64  `json:"debits_posted"`
	CreditsPending int64  `json:"credits_pending"`
	DebitsPending  int64  `json:"debits_pending"`
}

// TigerBeetleClient is backed exclusively by the official binary-protocol SDK.
// Financial and operational ledger mutations must never degrade to HTTP, local
// memory, or PostgreSQL emulation, because those modes do not provide the
// TigerBeetle consistency model required by this platform.
type TigerBeetleClient interface {
	CreateTransfer(ctx context.Context, transfer TBTransfer) (*TBTransfer, error)
	GetTransfer(ctx context.Context, transferID string) (*TBTransfer, error)
	VoidTransfer(ctx context.Context, transferID string) error
	PostTransfer(ctx context.Context, transferID string) error
	CreateAccount(ctx context.Context, account TBAccount) error
	GetAccount(ctx context.Context, accountID string) (*TBAccount, error)
	LookupTransfers(ctx context.Context, accountID string, limit int) ([]TBTransfer, error)
	Status() MWStatus
	Close() error
}

// unavailableTigerBeetleClient never stores, synthesizes, or substitutes ledger
// operations. It preserves the dependency error for callers until the native
// binary-protocol endpoints recover.
type unavailableTigerBeetleClient struct {
	reason string
}

func (t *unavailableTigerBeetleClient) unavailable() error {
	return fmt.Errorf("native TigerBeetle is unavailable: %s", t.reason)
}
func (t *unavailableTigerBeetleClient) CreateTransfer(context.Context, TBTransfer) (*TBTransfer, error) {
	return nil, t.unavailable()
}
func (t *unavailableTigerBeetleClient) GetTransfer(context.Context, string) (*TBTransfer, error) {
	return nil, t.unavailable()
}
func (t *unavailableTigerBeetleClient) VoidTransfer(context.Context, string) error {
	return t.unavailable()
}
func (t *unavailableTigerBeetleClient) PostTransfer(context.Context, string) error {
	return t.unavailable()
}
func (t *unavailableTigerBeetleClient) CreateAccount(context.Context, TBAccount) error {
	return t.unavailable()
}
func (t *unavailableTigerBeetleClient) GetAccount(context.Context, string) (*TBAccount, error) {
	return nil, t.unavailable()
}
func (t *unavailableTigerBeetleClient) LookupTransfers(context.Context, string, int) ([]TBTransfer, error) {
	return nil, t.unavailable()
}
func (t *unavailableTigerBeetleClient) Status() MWStatus {
	return MWStatus{Name: "TigerBeetle", Connected: false, Mode: "native binary-protocol required", Details: t.reason}
}
func (t *unavailableTigerBeetleClient) Close() error { return nil }

func tigerBeetleAddresses() []string {
	raw := strings.TrimSpace(envOrDefault("TIGERBEETLE_ADDRESSES", ""))
	if raw == "" {
		raw = strings.TrimSpace(envOrDefault("TIGERBEETLE_URL", ""))
		raw = strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://")
	}

	var addresses []string
	for _, address := range strings.Split(raw, ",") {
		if address = strings.TrimSpace(address); address != "" {
			addresses = append(addresses, address)
		}
	}
	return addresses
}

func initTigerBeetleClient() TigerBeetleClient {
	addresses := tigerBeetleAddresses()
	if len(addresses) == 0 {
		log.Warn().Msg("TigerBeetle unavailable: TIGERBEETLE_ADDRESSES/TIGERBEETLE_URL not set")
		return &unavailableTigerBeetleClient{reason: "TIGERBEETLE_ADDRESSES or TIGERBEETLE_URL must be configured"}
	}

	client, err := newTBNativeClient(addresses)
	if err != nil {
		log.Warn().Err(err).Strs("addresses", addresses).Msg("TigerBeetle native SDK initialization failed")
		return &unavailableTigerBeetleClient{reason: err.Error()}
	}
	log.Info().Strs("addresses", addresses).Msg("TigerBeetle native binary-protocol client initialized")
	return client
}
