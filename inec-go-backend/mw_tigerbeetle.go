package main

import (
	"context"
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
		log.Fatal().Msg("TigerBeetle is required: set TIGERBEETLE_ADDRESSES or TIGERBEETLE_URL to one or more native binary-protocol endpoints")
	}

	client, err := newTBNativeClient(addresses)
	if err != nil {
		log.Fatal().Err(err).Strs("addresses", addresses).Msg("TigerBeetle native SDK initialization failed")
	}
	log.Info().Strs("addresses", addresses).Msg("TigerBeetle native binary-protocol client initialized")
	return client
}
