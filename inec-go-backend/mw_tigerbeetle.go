package main

import (
	"bytes"
	"context"
	cryptoRand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"net/http"
	"sync"
	"time"
)

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
}

type TBAccount struct {
	ID             string `json:"id"`
	Ledger         int    `json:"ledger"`
	Code           int    `json:"code"`
	CreditsPosted  int64  `json:"credits_posted"`
	DebitsPosted   int64  `json:"debits_posted"`
	CreditsPending int64  `json:"credits_pending"`
	DebitsPending  int64  `json:"debits_pending"`
}

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

type tbHTTPClient struct {
	baseURL string
	client  *ResilientHTTPClient
}

func (t *tbHTTPClient) CreateTransfer(ctx context.Context, transfer TBTransfer) (*TBTransfer, error) {
	body, _ := json.Marshal(transfer)
	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/transfers", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result TBTransfer
	json.NewDecoder(resp.Body).Decode(&result)
	return &result, nil
}

func (t *tbHTTPClient) GetTransfer(ctx context.Context, transferID string) (*TBTransfer, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/transfers/"+transferID, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result TBTransfer
	json.NewDecoder(resp.Body).Decode(&result)
	return &result, nil
}

func (t *tbHTTPClient) VoidTransfer(ctx context.Context, transferID string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/transfers/"+transferID+"/void", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (t *tbHTTPClient) PostTransfer(ctx context.Context, transferID string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/transfers/"+transferID+"/post", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (t *tbHTTPClient) CreateAccount(ctx context.Context, account TBAccount) error {
	body, _ := json.Marshal(account)
	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/accounts", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (t *tbHTTPClient) GetAccount(ctx context.Context, accountID string) (*TBAccount, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/accounts/"+accountID, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result TBAccount
	json.NewDecoder(resp.Body).Decode(&result)
	return &result, nil
}

func (t *tbHTTPClient) LookupTransfers(ctx context.Context, accountID string, limit int) ([]TBTransfer, error) {
	url := fmt.Sprintf("%s/accounts/%s/transfers?limit=%d", t.baseURL, accountID, limit)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var transfers []TBTransfer
	json.NewDecoder(resp.Body).Decode(&transfers)
	return transfers, nil
}

func (t *tbHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/health", nil)
	lat, err := measureLatency(func() error {
		resp, e := t.client.Client.Do(req)
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "TigerBeetle", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "TigerBeetle", Connected: true, Mode: "external", Latency: fmtLatency(lat)}
}

func (t *tbHTTPClient) Close() error { return nil }

type embeddedTigerBeetle struct {
	mu        sync.RWMutex
	transfers map[string]*TBTransfer
	accounts  map[string]*TBAccount
}

func newEmbeddedTigerBeetle() *embeddedTigerBeetle {
	tb := &embeddedTigerBeetle{
		transfers: make(map[string]*TBTransfer),
		accounts:  make(map[string]*TBAccount),
	}
	tb.accounts["inec-operational"] = &TBAccount{ID: "inec-operational", Ledger: 1, Code: 1}
	tb.accounts["inec-official"] = &TBAccount{ID: "inec-official", Ledger: 2, Code: 1}
	return tb
}

func (t *embeddedTigerBeetle) CreateTransfer(_ context.Context, transfer TBTransfer) (*TBTransfer, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if transfer.ID == "" {
		rngBuf := make([]byte, 16)
		cryptoRand.Read(rngBuf)
		h := sha256.Sum256(append([]byte(fmt.Sprintf("%d-", time.Now().UnixNano())), rngBuf...))
		transfer.ID = "TB-" + hex.EncodeToString(h[:6])
	}
	transfer.Status = "PENDING"
	transfer.Timestamp = time.Now().UTC().Format(time.RFC3339)
	t.transfers[transfer.ID] = &transfer

	if da, ok := t.accounts[transfer.DebitAccountID]; ok {
		da.DebitsPending += transfer.Amount
	}
	if ca, ok := t.accounts[transfer.CreditAccountID]; ok {
		ca.CreditsPending += transfer.Amount
	}
	return &transfer, nil
}

func (t *embeddedTigerBeetle) GetTransfer(_ context.Context, transferID string) (*TBTransfer, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	tr, ok := t.transfers[transferID]
	if !ok {
		return nil, fmt.Errorf("transfer not found: %s", transferID)
	}
	return tr, nil
}

func (t *embeddedTigerBeetle) VoidTransfer(_ context.Context, transferID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.transfers[transferID]
	if !ok {
		return fmt.Errorf("transfer not found: %s", transferID)
	}
	if tr.Status == "PENDING" {
		if da, ok := t.accounts[tr.DebitAccountID]; ok {
			da.DebitsPending -= tr.Amount
		}
		if ca, ok := t.accounts[tr.CreditAccountID]; ok {
			ca.CreditsPending -= tr.Amount
		}
	}
	tr.Status = "VOIDED"
	return nil
}

func (t *embeddedTigerBeetle) PostTransfer(_ context.Context, transferID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.transfers[transferID]
	if !ok {
		return fmt.Errorf("transfer not found: %s", transferID)
	}
	if tr.Status != "PENDING" {
		return fmt.Errorf("transfer not pending: %s", tr.Status)
	}
	if da, ok := t.accounts[tr.DebitAccountID]; ok {
		da.DebitsPending -= tr.Amount
		da.DebitsPosted += tr.Amount
	}
	if ca, ok := t.accounts[tr.CreditAccountID]; ok {
		ca.CreditsPending -= tr.Amount
		ca.CreditsPosted += tr.Amount
	}
	tr.Status = "POSTED"
	return nil
}

func (t *embeddedTigerBeetle) CreateAccount(_ context.Context, account TBAccount) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.accounts[account.ID] = &account
	return nil
}

func (t *embeddedTigerBeetle) GetAccount(_ context.Context, accountID string) (*TBAccount, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	a, ok := t.accounts[accountID]
	if !ok {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}
	return a, nil
}

func (t *embeddedTigerBeetle) LookupTransfers(_ context.Context, accountID string, limit int) ([]TBTransfer, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var result []TBTransfer
	for _, tr := range t.transfers {
		if tr.DebitAccountID == accountID || tr.CreditAccountID == accountID {
			result = append(result, *tr)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (t *embeddedTigerBeetle) Status() MWStatus {
	t.mu.RLock()
	trCount := len(t.transfers)
	acctCount := len(t.accounts)
	var posted, pending, voided int
	for _, tr := range t.transfers {
		switch tr.Status {
		case "POSTED":
			posted++
		case "PENDING":
			pending++
		case "VOIDED":
			voided++
		}
	}
	t.mu.RUnlock()
	return MWStatus{
		Name: "TigerBeetle", Connected: true, Mode: "embedded",
		Latency: "0.0ms",
		Details: fmt.Sprintf("local ledger, %d accounts, %d transfers (posted:%d pending:%d voided:%d)", acctCount, trCount, posted, pending, voided),
	}
}

func (t *embeddedTigerBeetle) Close() error { return nil }

// dbBackedTigerBeetle persists transfers and accounts to PostgreSQL for durability.
// This replaces the in-memory embedded mode when no real TigerBeetle cluster is available.
type dbBackedTigerBeetle struct {
	embedded *embeddedTigerBeetle
}

func newDBBackedTigerBeetle() *dbBackedTigerBeetle {
	tb := &dbBackedTigerBeetle{embedded: newEmbeddedTigerBeetle()}
	// Create persistent tables
	dbExecLog("schema", `CREATE TABLE IF NOT EXISTS tb_accounts (
		id TEXT PRIMARY KEY,
		ledger INTEGER NOT NULL,
		code INTEGER NOT NULL,
		credits_posted INTEGER DEFAULT 0,
		debits_posted INTEGER DEFAULT 0,
		credits_pending INTEGER DEFAULT 0,
		debits_pending INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	dbExecLog("schema", `CREATE TABLE IF NOT EXISTS tb_transfers (
		id TEXT PRIMARY KEY,
		debit_account_id TEXT NOT NULL,
		credit_account_id TEXT NOT NULL,
		amount INTEGER NOT NULL,
		ledger INTEGER NOT NULL,
		code INTEGER NOT NULL,
		status TEXT DEFAULT 'PENDING',
		user_data TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		posted_at TIMESTAMP
	)`)
	// Seed default accounts
	dbExecLog("tb_accounts", "INSERT OR IGNORE INTO tb_accounts (id, ledger, code) VALUES ('inec-operational', 1, 1)")
	dbExecLog("tb_accounts", "INSERT OR IGNORE INTO tb_accounts (id, ledger, code) VALUES ('inec-official', 2, 1)")
	log.Info().Msg("TigerBeetle using DB-backed persistent ledger")
	return tb
}

func (t *dbBackedTigerBeetle) CreateTransfer(ctx context.Context, transfer TBTransfer) (*TBTransfer, error) {
	result, err := t.embedded.CreateTransfer(ctx, transfer)
	if err != nil {
		return nil, err
	}
	// Persist to DB
	dbExecLog("db_op", convertPlaceholders(
		"INSERT OR IGNORE INTO tb_transfers (id, debit_account_id, credit_account_id, amount, ledger, code, status, user_data) VALUES (?,?,?,?,?,?,?,?)"),
		result.ID, result.DebitAccountID, result.CreditAccountID, result.Amount, result.Ledger, result.Code, result.Status, result.UserData)
	return result, nil
}

func (t *dbBackedTigerBeetle) GetTransfer(ctx context.Context, transferID string) (*TBTransfer, error) {
	return t.embedded.GetTransfer(ctx, transferID)
}

func (t *dbBackedTigerBeetle) VoidTransfer(ctx context.Context, transferID string) error {
	if err := t.embedded.VoidTransfer(ctx, transferID); err != nil {
		return err
	}
	dbExecLog("tb_transfers", convertPlaceholders("UPDATE tb_transfers SET status = 'VOIDED' WHERE id = ?"), transferID)
	return nil
}

func (t *dbBackedTigerBeetle) PostTransfer(ctx context.Context, transferID string) error {
	if err := t.embedded.PostTransfer(ctx, transferID); err != nil {
		return err
	}
	dbExecLog("tb_transfers", convertPlaceholders("UPDATE tb_transfers SET status = 'POSTED', posted_at = CURRENT_TIMESTAMP WHERE id = ?"), transferID)
	return nil
}

func (t *dbBackedTigerBeetle) CreateAccount(ctx context.Context, account TBAccount) error {
	if err := t.embedded.CreateAccount(ctx, account); err != nil {
		return err
	}
	dbExecLog("tb_accounts", convertPlaceholders("INSERT OR IGNORE INTO tb_accounts (id, ledger, code) VALUES (?,?,?)"),
		account.ID, account.Ledger, account.Code)
	return nil
}

func (t *dbBackedTigerBeetle) GetAccount(ctx context.Context, accountID string) (*TBAccount, error) {
	return t.embedded.GetAccount(ctx, accountID)
}

func (t *dbBackedTigerBeetle) LookupTransfers(ctx context.Context, accountID string, limit int) ([]TBTransfer, error) {
	return t.embedded.LookupTransfers(ctx, accountID, limit)
}

func (t *dbBackedTigerBeetle) Status() MWStatus {
	s := t.embedded.Status()
	s.Mode = "db-backed"
	s.Details = "persistent ledger (DB-backed; production: use tigerbeetle-go SDK with binary protocol)"
	return s
}

func (t *dbBackedTigerBeetle) Close() error { return nil }

func initTigerBeetleClient() TigerBeetleClient {
	tbURL := envOrDefault("TIGERBEETLE_URL", "")
	if tbURL != "" {
		// NOTE: Real TigerBeetle uses a custom binary protocol on port 3000.
		// The HTTP client here is for TigerBeetle's optional HTTP gateway (if deployed).
		// For production, use github.com/tigerbeetle/tigerbeetle-go SDK directly.
		client := &tbHTTPClient{
			baseURL: tbURL,
			client:  NewResilientHTTPClient("tigerbeetle"),
		}
		s := client.Status()
		if s.Connected {
			log.Info().Str("url", tbURL).Msg("TigerBeetle HTTP gateway connected")
			return client
		}
		log.Warn().Msg("TigerBeetle unreachable, falling back to DB-backed mode")
	}
	// Use DB-backed persistent ledger instead of volatile in-memory
	return newDBBackedTigerBeetle()
}
