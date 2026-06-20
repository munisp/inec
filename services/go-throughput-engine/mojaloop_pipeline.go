package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

// MojaloopPipeline handles high-concurrency Mojaloop transfers with:
// - Semaphore-limited concurrency (5000 parallel transfers)
// - Connection pooling (keep-alive, 1000 idle connections)
// - Pipeline stages: Discovery → Quote → Transfer → Settlement
// - Batch settlement (settle N transfers at once)
// - Circuit breaker for unhealthy FSPs
type MojaloopPipeline struct {
	cfg    Config
	logger *zap.Logger
	client *http.Client
	sem    *semaphore.Weighted

	// Circuit breaker state per FSP
	fspHealth sync.Map // fspID -> *fspState

	transferred atomic.Int64
	settled     atomic.Int64
}

type fspState struct {
	failures  atomic.Int32
	lastCheck time.Time
	healthy   bool
}

func NewMojaloopPipeline(cfg Config, logger *zap.Logger) *MojaloopPipeline {
	return &MojaloopPipeline{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{
			Timeout: time.Duration(cfg.MojaTimeout) * time.Millisecond,
			Transport: &http.Transport{
				MaxIdleConns:        1000,
				MaxIdleConnsPerHost: 200,
				MaxConnsPerHost:     500,
				IdleConnTimeout:     90 * time.Second,
				DisableCompression:  true,
			},
		},
		sem: semaphore.NewWeighted(int64(cfg.MojaConcurrency)),
	}
}

// ExecuteTransfer runs the full 4-phase Mojaloop transfer with concurrency limiting.
func (m *MojaloopPipeline) ExecuteTransfer(ctx context.Context, tx Transaction) error {
	if err := m.sem.Acquire(ctx, 1); err != nil {
		return err
	}
	defer m.sem.Release(1)

	payerFSP := "inec-fsp"
	payeeFSP := fmt.Sprintf("fsp-%s", tx.StateCode)

	// Check circuit breaker
	if !m.isFSPHealthy(payeeFSP) {
		return fmt.Errorf("fsp %s circuit open", payeeFSP)
	}

	// Phase 1: Party Lookup
	party, err := m.partyLookup(ctx, "ACCOUNT_ID", tx.PUID)
	if err != nil {
		m.recordFailure(payeeFSP)
		return fmt.Errorf("party lookup failed: %w", err)
	}

	// Phase 2: Quote
	quote, err := m.createQuote(ctx, payerFSP, party.FSPID, float64(tx.Amount)/100)
	if err != nil {
		m.recordFailure(payeeFSP)
		return fmt.Errorf("quote failed: %w", err)
	}

	// Phase 3: Transfer
	err = m.createTransfer(ctx, quote)
	if err != nil {
		m.recordFailure(payeeFSP)
		return fmt.Errorf("transfer failed: %w", err)
	}

	m.transferred.Add(1)
	return nil
}

// BatchSettle settles multiple transfers in a single batch request.
func (m *MojaloopPipeline) BatchSettle(ctx context.Context, transferIDs []string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"settlementModel": "DEFERRED_NET",
		"transferIds":     transferIDs,
	})

	req, _ := http.NewRequestWithContext(ctx, "POST",
		m.cfg.MojaBaseURL+"/v1/settlements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("FSPIOP-Source", "inec-hub")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	m.settled.Add(int64(len(transferIDs)))
	return nil
}

func (m *MojaloopPipeline) partyLookup(ctx context.Context, partyType, partyID string) (*mojaPartyResp, error) {
	url := fmt.Sprintf("%s/parties/%s/%s", m.cfg.MojaBaseURL, partyType, partyID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Accept", "application/vnd.interoperability.parties+json;version=1")
	req.Header.Set("FSPIOP-Source", "inec-fsp")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var party mojaPartyResp
	json.NewDecoder(resp.Body).Decode(&party)
	return &party, nil
}

func (m *MojaloopPipeline) createQuote(ctx context.Context, payerFSP, payeeFSP string, amount float64) (*mojaQuoteResp, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"quoteId": fmt.Sprintf("q-%d", time.Now().UnixNano()),
		"payer":   map[string]string{"fspId": payerFSP},
		"payee":   map[string]string{"fspId": payeeFSP},
		"amount":  map[string]interface{}{"amount": fmt.Sprintf("%.2f", amount), "currency": "NGN"},
	})

	req, _ := http.NewRequestWithContext(ctx, "POST",
		m.cfg.MojaBaseURL+"/quotes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/vnd.interoperability.quotes+json;version=1")
	req.Header.Set("FSPIOP-Source", payerFSP)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var quote mojaQuoteResp
	json.NewDecoder(resp.Body).Decode(&quote)
	return &quote, nil
}

func (m *MojaloopPipeline) createTransfer(ctx context.Context, quote *mojaQuoteResp) error {
	body, _ := json.Marshal(map[string]interface{}{
		"transferId":    fmt.Sprintf("t-%d", time.Now().UnixNano()),
		"payerFsp":     quote.PayerFSP,
		"payeeFsp":     quote.PayeeFSP,
		"amount":       quote.Amount,
		"ilpPacket":    quote.ILPPacket,
		"condition":    quote.Condition,
		"expiration":   time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339),
	})

	req, _ := http.NewRequestWithContext(ctx, "POST",
		m.cfg.MojaBaseURL+"/transfers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/vnd.interoperability.transfers+json;version=1")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (m *MojaloopPipeline) isFSPHealthy(fspID string) bool {
	val, ok := m.fspHealth.Load(fspID)
	if !ok {
		return true // assume healthy if unknown
	}
	state := val.(*fspState)
	if state.failures.Load() >= 10 {
		// Circuit open for 30 seconds
		if time.Since(state.lastCheck) > 30*time.Second {
			state.failures.Store(0)
			state.lastCheck = time.Now()
			return true // half-open
		}
		return false
	}
	return true
}

func (m *MojaloopPipeline) recordFailure(fspID string) {
	val, _ := m.fspHealth.LoadOrStore(fspID, &fspState{lastCheck: time.Now(), healthy: true})
	state := val.(*fspState)
	state.failures.Add(1)
}

type mojaPartyResp struct {
	FSPID string `json:"fspId"`
	Name  string `json:"name"`
}

type mojaQuoteResp struct {
	QuoteID    string `json:"quoteId"`
	PayerFSP   string `json:"payerFsp"`
	PayeeFSP   string `json:"payeeFsp"`
	Amount     map[string]string `json:"amount"`
	ILPPacket  string `json:"ilpPacket"`
	Condition  string `json:"condition"`
}
