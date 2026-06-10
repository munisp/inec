package main

import (
	"bytes"
	"context"
	cryptoRand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	tbClient "github.com/tigerbeetle/tigerbeetle-go"
	tbTypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
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
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
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
	mu              sync.RWMutex
	transfers       map[string]*TBTransfer
	accounts        map[string]*TBAccount
	idempotencyKeys map[string]string // idempotency_key → transfer_id
}

func newEmbeddedTigerBeetle() *embeddedTigerBeetle {
	tb := &embeddedTigerBeetle{
		transfers:       make(map[string]*TBTransfer),
		accounts:        make(map[string]*TBAccount),
		idempotencyKeys: make(map[string]string),
	}
	tb.accounts["inec-operational"] = &TBAccount{ID: "inec-operational", Ledger: 1, Code: 1}
	tb.accounts["inec-official"] = &TBAccount{ID: "inec-official", Ledger: 2, Code: 1}
	return tb
}

func (t *embeddedTigerBeetle) CreateTransfer(_ context.Context, transfer TBTransfer) (*TBTransfer, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Idempotency check: if same key was used before, return existing transfer
	if transfer.IdempotencyKey != "" {
		if existingID, exists := t.idempotencyKeys[transfer.IdempotencyKey]; exists {
			if existing, ok := t.transfers[existingID]; ok {
				return existing, nil
			}
		}
	}

	if transfer.ID == "" {
		rngBuf := make([]byte, 16)
		cryptoRand.Read(rngBuf)
		h := sha256.Sum256(append([]byte(fmt.Sprintf("%d-", time.Now().UnixNano())), rngBuf...))
		transfer.ID = "TB-" + hex.EncodeToString(h[:6])
	}

	// Check for duplicate transfer ID
	if _, exists := t.transfers[transfer.ID]; exists {
		return t.transfers[transfer.ID], nil
	}

	transfer.Status = "PENDING"
	transfer.Timestamp = time.Now().UTC().Format(time.RFC3339)
	t.transfers[transfer.ID] = &transfer

	// Store idempotency mapping
	if transfer.IdempotencyKey != "" {
		t.idempotencyKeys[transfer.IdempotencyKey] = transfer.ID
	}

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

// tbSDKClient wraps the official TigerBeetle Go SDK using the native binary protocol.
type tbSDKClient struct {
	client    tbClient.Client
	addresses []string
	mu        sync.RWMutex
	transfers map[string]*TBTransfer // local cache for GetTransfer by string ID
	accounts  map[string]*TBAccount
}

func newTBSDKClient(addresses []string) (*tbSDKClient, error) {
	client, err := tbClient.NewClient(tbTypes.ToUint128(0), addresses)
	if err != nil {
		return nil, fmt.Errorf("tigerbeetle-go connect: %w", err)
	}
	// Verify connectivity with a Nop request
	if err := client.Nop(); err != nil {
		client.Close()
		return nil, fmt.Errorf("tigerbeetle nop: %w", err)
	}
	c := &tbSDKClient{
		client:    client,
		addresses: addresses,
		transfers: make(map[string]*TBTransfer),
		accounts:  make(map[string]*TBAccount),
	}
	// Create default ledger accounts
	defaultAccounts := []tbTypes.Account{
		{ID: tbTypes.ToUint128(1), Ledger: 1, Code: 1},
		{ID: tbTypes.ToUint128(2), Ledger: 2, Code: 1},
	}
	_, _ = client.CreateAccounts(defaultAccounts)
	c.accounts["inec-operational"] = &TBAccount{ID: "inec-operational", Ledger: 1, Code: 1}
	c.accounts["inec-official"] = &TBAccount{ID: "inec-official", Ledger: 2, Code: 1}
	return c, nil
}

func stringToUint128(s string) tbTypes.Uint128 {
	h := sha256.Sum256([]byte(s))
	var bytes [16]byte
	copy(bytes[:], h[:16])
	return tbTypes.BytesToUint128(bytes)
}

func (t *tbSDKClient) CreateTransfer(_ context.Context, transfer TBTransfer) (*TBTransfer, error) {
	if transfer.Amount < 0 {
		return nil, fmt.Errorf("transfer amount must be non-negative, got %d", transfer.Amount)
	}
	if transfer.Ledger < 0 || transfer.Ledger > 0xFFFFFFFF {
		return nil, fmt.Errorf("ledger out of uint32 range: %d", transfer.Ledger)
	}
	if transfer.Code < 0 || transfer.Code > 0xFFFF {
		return nil, fmt.Errorf("code out of uint16 range: %d", transfer.Code)
	}
	if transfer.ID == "" {
		rngBuf := make([]byte, 16)
		cryptoRand.Read(rngBuf)
		h := sha256.Sum256(append([]byte(fmt.Sprintf("%d-", time.Now().UnixNano())), rngBuf...))
		transfer.ID = "TB-" + hex.EncodeToString(h[:6])
	}

	tbTransfer := tbTypes.Transfer{
		ID:              stringToUint128(transfer.ID),
		DebitAccountID:  stringToUint128(transfer.DebitAccountID),
		CreditAccountID: stringToUint128(transfer.CreditAccountID),
		Amount:          tbTypes.ToUint128(uint64(transfer.Amount)), // #nosec G115 -- validated non-negative above
		Ledger:          uint32(transfer.Ledger),                    // #nosec G115 -- validated in range above
		Code:            uint16(transfer.Code),                      // #nosec G115 -- validated in range above
		Flags:           tbTypes.TransferFlags{Pending: true}.ToUint16(),
	}
	results, err := t.client.CreateTransfers([]tbTypes.Transfer{tbTransfer})
	if err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}
	for _, r := range results {
		if r.Result != tbTypes.TransferOK {
			return nil, fmt.Errorf("transfer rejected: %v", r.Result)
		}
	}
	transfer.Status = "PENDING"
	transfer.Timestamp = time.Now().UTC().Format(time.RFC3339)
	t.mu.Lock()
	t.transfers[transfer.ID] = &transfer
	t.mu.Unlock()
	return &transfer, nil
}

func (t *tbSDKClient) GetTransfer(_ context.Context, transferID string) (*TBTransfer, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	tr, ok := t.transfers[transferID]
	if !ok {
		return nil, fmt.Errorf("transfer not found: %s", transferID)
	}
	return tr, nil
}

func (t *tbSDKClient) VoidTransfer(_ context.Context, transferID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.transfers[transferID]
	if !ok {
		return fmt.Errorf("transfer not found: %s", transferID)
	}
	voidTransfer := tbTypes.Transfer{
		ID:        stringToUint128(transferID + "-void"),
		PendingID: stringToUint128(transferID),
		Flags:     tbTypes.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
	}
	_, err := t.client.CreateTransfers([]tbTypes.Transfer{voidTransfer})
	if err != nil {
		return fmt.Errorf("void transfer: %w", err)
	}
	tr.Status = "VOIDED"
	return nil
}

func (t *tbSDKClient) PostTransfer(_ context.Context, transferID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.transfers[transferID]
	if !ok {
		return fmt.Errorf("transfer not found: %s", transferID)
	}
	postTransfer := tbTypes.Transfer{
		ID:        stringToUint128(transferID + "-post"),
		PendingID: stringToUint128(transferID),
		Flags:     tbTypes.TransferFlags{PostPendingTransfer: true}.ToUint16(),
	}
	_, err := t.client.CreateTransfers([]tbTypes.Transfer{postTransfer})
	if err != nil {
		return fmt.Errorf("post transfer: %w", err)
	}
	tr.Status = "POSTED"
	return nil
}

func (t *tbSDKClient) CreateAccount(_ context.Context, account TBAccount) error {
	if account.Ledger < 0 || account.Ledger > 0xFFFFFFFF {
		return fmt.Errorf("ledger out of uint32 range: %d", account.Ledger)
	}
	if account.Code < 0 || account.Code > 0xFFFF {
		return fmt.Errorf("code out of uint16 range: %d", account.Code)
	}
	tbAcct := tbTypes.Account{
		ID:     stringToUint128(account.ID),
		Ledger: uint32(account.Ledger), // #nosec G115 -- validated in range above
		Code:   uint16(account.Code),   // #nosec G115 -- validated in range above
	}
	_, err := t.client.CreateAccounts([]tbTypes.Account{tbAcct})
	if err != nil {
		return fmt.Errorf("create account: %w", err)
	}
	t.mu.Lock()
	t.accounts[account.ID] = &account
	t.mu.Unlock()
	return nil
}

func (t *tbSDKClient) GetAccount(_ context.Context, accountID string) (*TBAccount, error) {
	results, err := t.client.LookupAccounts([]tbTypes.Uint128{stringToUint128(accountID)})
	if err != nil {
		return nil, fmt.Errorf("lookup account: %w", err)
	}
	if len(results) == 0 {
		t.mu.RLock()
		a, ok := t.accounts[accountID]
		t.mu.RUnlock()
		if ok {
			return a, nil
		}
		return nil, fmt.Errorf("account not found: %s", accountID)
	}
	a := results[0]
	cp := a.CreditsPosted.BigInt()
	dp := a.DebitsPosted.BigInt()
	cpn := a.CreditsPending.BigInt()
	dpn := a.DebitsPending.BigInt()
	return &TBAccount{
		ID:             accountID,
		Ledger:         int(a.Ledger),
		Code:           int(a.Code),
		CreditsPosted:  cp.Int64(),
		DebitsPosted:   dp.Int64(),
		CreditsPending: cpn.Int64(),
		DebitsPending:  dpn.Int64(),
	}, nil
}

func (t *tbSDKClient) LookupTransfers(_ context.Context, accountID string, limit int) ([]TBTransfer, error) {
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

func (t *tbSDKClient) Status() MWStatus {
	lat, err := measureLatency(func() error {
		return t.client.Nop()
	})
	if err != nil {
		return MWStatus{Name: "TigerBeetle", Connected: false, Mode: "native tigerbeetle-go (unreachable)", Details: err.Error()}
	}
	t.mu.RLock()
	trCount := len(t.transfers)
	acctCount := len(t.accounts)
	t.mu.RUnlock()
	return MWStatus{
		Name: "TigerBeetle", Connected: true, Mode: "native tigerbeetle-go",
		Latency: fmtLatency(lat),
		Details: fmt.Sprintf("binary protocol, cluster=0, %d accounts, %d transfers", acctCount, trCount),
	}
}

func (t *tbSDKClient) Close() error {
	t.client.Close()
	return nil
}

func initTigerBeetleClient() TigerBeetleClient {
	// Try native binary protocol first (TIGERBEETLE_ADDRESSES="host:port")
	tbAddrs := envOrDefault("TIGERBEETLE_ADDRESSES", "")
	if tbAddrs == "" {
		// Fallback: parse TIGERBEETLE_URL for host:port
		tbURL := envOrDefault("TIGERBEETLE_URL", "")
		if tbURL != "" {
			// Strip http:// prefix and use as address
			addr := strings.TrimPrefix(strings.TrimPrefix(tbURL, "http://"), "https://")
			tbAddrs = addr
		}
	}
	if tbAddrs != "" {
		addresses := strings.Split(tbAddrs, ",")
		client, err := newTBSDKClient(addresses)
		if err == nil {
			log.Info().Strs("addresses", addresses).Msg("TigerBeetle connected via native SDK (binary protocol)")
			return client
		}
		log.Warn().Err(err).Msg("TigerBeetle native SDK connection failed, trying fallback")
	}
	env := os.Getenv("APP_ENV")
	if env == "production" || env == "staging" {
		log.Fatal().Msg("TigerBeetle is REQUIRED in production/staging for double-entry ledger integrity. Set TIGERBEETLE_ADDRESSES")
	}
	log.Warn().Msg("TigerBeetle using DB-backed persistent ledger (DEV ONLY — reduced throughput)")
	return newDBBackedTigerBeetle()
}
