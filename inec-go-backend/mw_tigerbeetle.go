package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
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
	ID              string `json:"id"`
	Ledger          int    `json:"ledger"`
	Code            int    `json:"code"`
	CreditsPosted   int64  `json:"credits_posted"`
	DebitsPosted    int64  `json:"debits_posted"`
	CreditsPending  int64  `json:"credits_pending"`
	DebitsPending   int64  `json:"debits_pending"`
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
	client  *http.Client
}

func (t *tbHTTPClient) CreateTransfer(ctx context.Context, transfer TBTransfer) (*TBTransfer, error) {
	body, _ := json.Marshal(transfer)
	req, _ := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/transfers", bytes.NewReader(body))
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
	req, _ := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/transfers/"+transferID, nil)
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
	req, _ := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/transfers/"+transferID+"/void", nil)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (t *tbHTTPClient) PostTransfer(ctx context.Context, transferID string) error {
	req, _ := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/transfers/"+transferID+"/post", nil)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (t *tbHTTPClient) CreateAccount(ctx context.Context, account TBAccount) error {
	body, _ := json.Marshal(account)
	req, _ := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (t *tbHTTPClient) GetAccount(ctx context.Context, accountID string) (*TBAccount, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/accounts/"+accountID, nil)
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
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
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
		resp, e := t.client.Do(req)
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
		h := sha256.Sum256([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Int63())))
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

func initTigerBeetleClient() TigerBeetleClient {
	tbURL := envOrDefault("TIGERBEETLE_URL", "")
	if tbURL != "" {
		client := &tbHTTPClient{
			baseURL: tbURL,
			client:  &http.Client{Timeout: 5 * time.Second},
		}
		s := client.Status()
		if s.Connected {
			log.Println("[TigerBeetle] Connected to external TigerBeetle at", tbURL)
			return client
		}
		log.Println("[TigerBeetle] External TigerBeetle unreachable, falling back to embedded")
	}
	log.Println("[TigerBeetle] Using embedded local ledger")
	return newEmbeddedTigerBeetle()
}
