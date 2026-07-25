package main

import (
	"context"
	"fmt"
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

// inMemoryTigerBeetleClient is a deterministic local ledger used only for
// explicit test/development environments. Native TigerBeetle remains required
// for production startup and operational writes.
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
	account.ID = strings.TrimSpace(account.ID)
	if account.ID == "" {
		return fmt.Errorf("TigerBeetle account ID is required")
	}
	if account.Ledger <= 0 || account.Code <= 0 {
		return fmt.Errorf("TigerBeetle account ledger and code must be positive")
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
	defer t.mu.RUnlock()
	account, exists := t.accounts[strings.TrimSpace(accountID)]
	if !exists {
		return nil, fmt.Errorf("TigerBeetle account %q not found", accountID)
	}
	return &account, nil
}

func (t *inMemoryTigerBeetleClient) CreateTransfer(ctx context.Context, transfer TBTransfer) (*TBTransfer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	transfer.ID = strings.TrimSpace(transfer.ID)
	transfer.DebitAccountID = strings.TrimSpace(transfer.DebitAccountID)
	transfer.CreditAccountID = strings.TrimSpace(transfer.CreditAccountID)
	if transfer.ID == "" || transfer.DebitAccountID == "" || transfer.CreditAccountID == "" {
		return nil, fmt.Errorf("TigerBeetle transfer ID, debit account, and credit account are required")
	}
	if transfer.Amount <= 0 || transfer.Ledger <= 0 || transfer.Code <= 0 {
		return nil, fmt.Errorf("TigerBeetle transfer amount, ledger, and code must be positive")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if existing, exists := t.transfers[transfer.ID]; exists {
		return &existing, nil
	}
	debit, debitExists := t.accounts[transfer.DebitAccountID]
	credit, creditExists := t.accounts[transfer.CreditAccountID]
	if !debitExists || !creditExists {
		return nil, fmt.Errorf("TigerBeetle transfer accounts must exist")
	}
	if debit.Ledger != transfer.Ledger || credit.Ledger != transfer.Ledger {
		return nil, fmt.Errorf("TigerBeetle transfer ledger does not match account ledger")
	}
	transfer.Status = "posted"
	transfer.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	debit.DebitsPosted += transfer.Amount
	credit.CreditsPosted += transfer.Amount
	t.accounts[debit.ID] = debit
	t.accounts[credit.ID] = credit
	t.transfers[transfer.ID] = transfer
	return &transfer, nil
}

func (t *inMemoryTigerBeetleClient) GetTransfer(ctx context.Context, transferID string) (*TBTransfer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	transfer, exists := t.transfers[strings.TrimSpace(transferID)]
	if !exists {
		return nil, fmt.Errorf("TigerBeetle transfer %q not found", transferID)
	}
	return &transfer, nil
}

func (t *inMemoryTigerBeetleClient) VoidTransfer(ctx context.Context, transferID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	transfer, exists := t.transfers[strings.TrimSpace(transferID)]
	if !exists {
		return fmt.Errorf("TigerBeetle transfer %q not found", transferID)
	}
	if transfer.Status == "voided" {
		return nil
	}
	if transfer.Status == "posted" {
		debit := t.accounts[transfer.DebitAccountID]
		credit := t.accounts[transfer.CreditAccountID]
		debit.DebitsPosted -= transfer.Amount
		credit.CreditsPosted -= transfer.Amount
		t.accounts[debit.ID] = debit
		t.accounts[credit.ID] = credit
	}
	transfer.Status = "voided"
	t.transfers[transfer.ID] = transfer
	return nil
}

func (t *inMemoryTigerBeetleClient) PostTransfer(ctx context.Context, transferID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	transfer, exists := t.transfers[strings.TrimSpace(transferID)]
	if !exists {
		return fmt.Errorf("TigerBeetle transfer %q not found", transferID)
	}
	if transfer.Status == "voided" {
		return fmt.Errorf("TigerBeetle transfer %q is voided", transferID)
	}
	if transfer.Status != "posted" {
		debit := t.accounts[transfer.DebitAccountID]
		credit := t.accounts[transfer.CreditAccountID]
		debit.DebitsPosted += transfer.Amount
		credit.CreditsPosted += transfer.Amount
		t.accounts[debit.ID] = debit
		t.accounts[credit.ID] = credit
		transfer.Status = "posted"
		t.transfers[transfer.ID] = transfer
	}
	return nil
}

func (t *inMemoryTigerBeetleClient) LookupTransfers(ctx context.Context, accountID string, limit int) ([]TBTransfer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, fmt.Errorf("TigerBeetle account ID is required")
	}
	if limit <= 0 || limit > 1_000 {
		limit = 100
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if _, exists := t.accounts[accountID]; !exists {
		return nil, fmt.Errorf("TigerBeetle account %q not found", accountID)
	}
	results := make([]TBTransfer, 0, limit)
	for _, transfer := range t.transfers {
		if transfer.DebitAccountID == accountID || transfer.CreditAccountID == accountID {
			results = append(results, transfer)
			if len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

func (t *inMemoryTigerBeetleClient) Status() MWStatus {
	return MWStatus{Name: "TigerBeetle", Connected: true, Mode: "in-memory (non-production)", Details: "local test/development transport"}
}

func (t *inMemoryTigerBeetleClient) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	clear(t.accounts)
	clear(t.transfers)
	return nil
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
		if isExplicitNonProductionDaprEnvironment() {
			log.Info().Msg("TigerBeetle addresses not configured; using explicit in-memory non-production transport")
			return newInMemoryTigerBeetleClient()
		}
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
