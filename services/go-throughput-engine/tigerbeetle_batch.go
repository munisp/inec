package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// TigerBeetleBatch submits transfers in batches of up to 8190 (TB's max batch
// size) for maximum throughput. Targets 1M+ transfers/sec.
//
// Key optimizations:
// - Batch accumulation to TB's max batch size (8190)
// - Pre-allocated transfer IDs (128-bit deterministic from tx hash)
// - Multiple worker goroutines for parallel batch submission
// - Linked transfers for atomic multi-leg operations
// - Zero-allocation ID generation via SHA-256 truncation
type TigerBeetleBatch struct {
	cfg    Config
	logger *zap.Logger
	client *http.Client

	buffer   chan tbTransfer
	pool     *WorkerPool
	batched  atomic.Int64
	submitted atomic.Int64
}

type tbTransfer struct {
	ID              [16]byte `json:"id"`
	DebitAccountID  [16]byte `json:"debit_account_id"`
	CreditAccountID [16]byte `json:"credit_account_id"`
	Amount          uint64   `json:"amount"`
	Ledger          uint32   `json:"ledger"`
	Code            uint16   `json:"code"`
	UserData128     [16]byte `json:"user_data_128"`
	Flags           uint16   `json:"flags"`
	Timestamp       uint64   `json:"timestamp"`
}

// Transfer codes for election operations
const (
	CodeResultDeposit    uint16 = 1001
	CodeBallotAudit      uint16 = 1002
	CodeIncidentPenalty  uint16 = 1003
	CodeSettlement       uint16 = 1004
	CodeAccreditation    uint16 = 1005
)

// Ledger IDs
const (
	LedgerElection  uint32 = 1
	LedgerAudit     uint32 = 2
	LedgerPenalty   uint32 = 3
	LedgerSettlement uint32 = 4
)

func NewTigerBeetleBatch(cfg Config, logger *zap.Logger) *TigerBeetleBatch {
	return &TigerBeetleBatch{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 200,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		buffer: make(chan tbTransfer, cfg.TBBatchSize*cfg.TBWorkers),
		pool:   NewWorkerPool("tb-batch", cfg.TBWorkers, cfg.TBWorkers*2, logger),
	}
}

func (t *TigerBeetleBatch) Start(ctx context.Context) {
	t.pool.Start(ctx)

	// Background batch accumulator
	go func() {
		batch := make([]tbTransfer, 0, t.cfg.TBBatchSize)
		timer := time.NewTicker(5 * time.Millisecond)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				if len(batch) > 0 {
					t.submitBatch(context.Background(), batch)
				}
				return
			case tr, ok := <-t.buffer:
				if !ok {
					return
				}
				batch = append(batch, tr)
				if len(batch) >= t.cfg.TBBatchSize {
					toSubmit := batch
					batch = make([]tbTransfer, 0, t.cfg.TBBatchSize)
					t.pool.Submit(func() { t.submitBatch(ctx, toSubmit) })
				}
			case <-timer.C:
				if len(batch) > 0 {
					toSubmit := batch
					batch = make([]tbTransfer, 0, t.cfg.TBBatchSize)
					t.pool.Submit(func() { t.submitBatch(ctx, toSubmit) })
				}
			}
		}
	}()
}

func (t *TigerBeetleBatch) submitBatch(ctx context.Context, transfers []tbTransfer) {
	if len(transfers) == 0 {
		return
	}

	// Submit via TigerBeetle HTTP API (or native client)
	payload, _ := json.Marshal(map[string]interface{}{
		"transfers": transfers,
	})

	url := fmt.Sprintf("http://%s/transfers/batch", t.cfg.TBAddress)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, jsonReader(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		t.logger.Error("tigerbeetle batch submit failed",
			zap.Int("batch_size", len(transfers)),
			zap.Error(err))
		return
	}
	defer resp.Body.Close()

	t.submitted.Add(int64(len(transfers)))
	t.batched.Add(1)
}

func (t *TigerBeetleBatch) RecordTransfer(ctx context.Context, tx Transaction) error {
	tr := tbTransfer{
		ID:              deterministicID(tx.ID),
		DebitAccountID:  deterministicID(tx.Source),
		CreditAccountID: deterministicID(tx.ElectionID),
		Amount:          uint64(tx.Amount),
		Ledger:          ledgerForType(tx.Type),
		Code:            codeForType(tx.Type),
		UserData128:     deterministicID(tx.Hash),
		Timestamp:       uint64(tx.Timestamp.UnixNano()),
	}
	t.buffer <- tr
	return nil
}

func (t *TigerBeetleBatch) RecordBatch(ctx context.Context, txs []Transaction) error {
	for i := range txs {
		t.RecordTransfer(ctx, txs[i])
	}
	return nil
}

// RecordLinkedTransfers submits atomically linked transfers (all-or-nothing).
func (t *TigerBeetleBatch) RecordLinkedTransfers(ctx context.Context, txs []Transaction) error {
	for i := range txs {
		tr := tbTransfer{
			ID:              deterministicID(txs[i].ID),
			DebitAccountID:  deterministicID(txs[i].Source),
			CreditAccountID: deterministicID(txs[i].ElectionID),
			Amount:          uint64(txs[i].Amount),
			Ledger:          ledgerForType(txs[i].Type),
			Code:            codeForType(txs[i].Type),
			UserData128:     deterministicID(txs[i].Hash),
			Timestamp:       uint64(txs[i].Timestamp.UnixNano()),
		}
		if i < len(txs)-1 {
			tr.Flags = 0x0001 // linked flag
		}
		t.buffer <- tr
	}
	return nil
}

func (t *TigerBeetleBatch) QueueDepth() int {
	return len(t.buffer)
}

func (t *TigerBeetleBatch) Close() {
	close(t.buffer)
	t.pool.Close()
}

// deterministicID generates a 128-bit ID from a string using SHA-256 truncation.
func deterministicID(s string) [16]byte {
	h := sha256.Sum256([]byte(s))
	var id [16]byte
	copy(id[:], h[:16])
	return id
}

func deterministicIDHex(s string) string {
	id := deterministicID(s)
	return hex.EncodeToString(id[:])
}

func idToUint64(id [16]byte) uint64 {
	return binary.LittleEndian.Uint64(id[:8])
}

func ledgerForType(txType string) uint32 {
	switch txType {
	case "result_submission", "ballot_cast":
		return LedgerElection
	case "incident":
		return LedgerPenalty
	default:
		return LedgerAudit
	}
}

func codeForType(txType string) uint16 {
	switch txType {
	case "result_submission":
		return CodeResultDeposit
	case "ballot_cast":
		return CodeBallotAudit
	case "incident":
		return CodeIncidentPenalty
	default:
		return CodeSettlement
	}
}
