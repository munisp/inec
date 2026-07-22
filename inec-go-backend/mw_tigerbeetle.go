package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

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

// TigerBeetleClient is backed by the official binary-protocol SDK in production.
// An explicit in-memory ledger is available only for isolated test and local
// development runtimes; APP_ENV=production never selects the local transport.
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

// inMemoryTigerBeetleClient provides an isolated, process-local ledger for
// non-production test and development runs. It intentionally does not claim
// TigerBeetle's replicated durability or concurrency guarantees.
type inMemoryTigerBeetleClient struct {
	mu        sync.RWMutex
	accounts  map[string]TBAccount
	transfers map[string]TBTransfer
}

func newInMemoryTigerBeetleClient() *inMemoryTigerBeetleClient {
	return &inMemoryTigerBeetleClient{accounts: make(map[string]TBAccount), transfers: make(map[string]TBTransfer)}
}

func (t *inMemoryTigerBeetleClient) CreateAccount(ctx context.Context, account TBAccount) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(account.ID) == "" {
		return fmt.Errorf("TigerBeetle account ID is required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.accounts[account.ID]; exists {
		return fmt.Errorf("TigerBeetle account %q already exists", account.ID)
	}
	t.accounts[account.ID] = account
	return nil
}

func (t *inMemoryTigerBeetleClient) GetAccount(ctx context.Context, accountID string) (*TBAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t.mu.RLock()
	account, ok := t.accounts[accountID]
	t.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("TigerBeetle account %q was not found", accountID)
	}
	return &account, nil
}

func (t *inMemoryTigerBeetleClient) CreateTransfer(ctx context.Context, transfer TBTransfer) (*TBTransfer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(transfer.ID) == "" || strings.TrimSpace(transfer.DebitAccountID) == "" || strings.TrimSpace(transfer.CreditAccountID) == "" || transfer.Amount <= 0 {
		return nil, fmt.Errorf("transfer ID, debit account, credit account, and positive amount are required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.transfers[transfer.ID]; exists {
		return nil, fmt.Errorf("TigerBeetle transfer %q already exists", transfer.ID)
	}
	debit, debitOK := t.accounts[transfer.DebitAccountID]
	credit, creditOK := t.accounts[transfer.CreditAccountID]
	if !debitOK || !creditOK {
		return nil, fmt.Errorf("debit and credit accounts must exist before a transfer")
	}
	debit.DebitsPosted += transfer.Amount
	credit.CreditsPosted += transfer.Amount
	t.accounts[debit.ID] = debit
	t.accounts[credit.ID] = credit
	if transfer.Timestamp == "" {
		transfer.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if transfer.Status == "" {
		transfer.Status = "posted"
	}
	t.transfers[transfer.ID] = transfer
	return &transfer, nil
}

func (t *inMemoryTigerBeetleClient) GetTransfer(ctx context.Context, transferID string) (*TBTransfer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t.mu.RLock()
	transfer, ok := t.transfers[transferID]
	t.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("TigerBeetle transfer %q was not found", transferID)
	}
	return &transfer, nil
}

func (t *inMemoryTigerBeetleClient) VoidTransfer(ctx context.Context, transferID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	transfer, ok := t.transfers[transferID]
	if !ok {
		return fmt.Errorf("TigerBeetle transfer %q was not found", transferID)
	}
	transfer.Status = "voided"
	t.transfers[transferID] = transfer
	return nil
}

func (t *inMemoryTigerBeetleClient) PostTransfer(ctx context.Context, transferID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	transfer, ok := t.transfers[transferID]
	if !ok {
		return fmt.Errorf("TigerBeetle transfer %q was not found", transferID)
	}
	transfer.Status = "posted"
	t.transfers[transferID] = transfer
	return nil
}

func (t *inMemoryTigerBeetleClient) LookupTransfers(ctx context.Context, accountID string, limit int) ([]TBTransfer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]TBTransfer, 0)
	for _, transfer := range t.transfers {
		if transfer.DebitAccountID == accountID || transfer.CreditAccountID == accountID {
			out = append(out, transfer)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (t *inMemoryTigerBeetleClient) Status() MWStatus {
	return MWStatus{Name: "TigerBeetle", Connected: true, Mode: "in-memory (non-production)", Details: "ephemeral local ledger"}
}

func (t *inMemoryTigerBeetleClient) Close() error { return nil }

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
	isProduction := strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
	if len(addresses) == 0 {
		if !isProduction {
			log.Warn().Msg("TigerBeetle is not configured — using the explicit in-memory non-production ledger")
			return newInMemoryTigerBeetleClient()
		}
		log.Fatal().Msg("TigerBeetle is required: set TIGERBEETLE_ADDRESSES or TIGERBEETLE_URL to one or more native binary-protocol endpoints")
	}

	client, err := newTBNativeClient(addresses)
	if err != nil {
		if !isProduction {
			log.Warn().Err(err).Strs("addresses", addresses).Msg("TigerBeetle native SDK is unavailable — using the explicit in-memory non-production ledger")
			return newInMemoryTigerBeetleClient()
		}
		log.Fatal().Err(err).Strs("addresses", addresses).Msg("TigerBeetle native SDK initialization failed")
	}
	log.Info().Strs("addresses", addresses).Msg("TigerBeetle native binary-protocol client initialized")
	return client
}
