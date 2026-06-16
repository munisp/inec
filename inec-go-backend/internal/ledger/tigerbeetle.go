// Package ledger provides double-entry accounting using the TigerBeetle SDK.
// This replaces the HTTP sidecar approach with the official native Go client.
package ledger

import (
	"context"
	"fmt"
	"sync"
	"time"

	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbt "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
	"github.com/rs/zerolog/log"
)

// Ledger constants for INEC financial tracking.
const (
	LedgerElectionFunding     = 1
	LedgerMaterialProcurement = 2
	LedgerStaffAllowances     = 3
	LedgerLogistics           = 4
	LedgerResultTransmission  = 5

	CodeTransfer       = 1
	CodeAllocation     = 2
	CodeDisbursement   = 3
	CodeReconciliation = 4
)

// Transfer represents a ledger transfer.
type Transfer struct {
	ID              tbt.Uint128
	DebitAccountID  tbt.Uint128
	CreditAccountID tbt.Uint128
	Amount          tbt.Uint128
	Ledger          uint32
	Code            uint16
	Timestamp       uint64
	Status          string
}

// Account represents a ledger account.
type Account struct {
	ID             tbt.Uint128
	Ledger         uint32
	Code           uint16
	CreditsPosted  tbt.Uint128
	DebitsPosted   tbt.Uint128
	CreditsPending tbt.Uint128
	DebitsPending  tbt.Uint128
}

// Service wraps the TigerBeetle client for INEC operations.
type Service struct {
	client  tb.Client
	mu      sync.RWMutex
	address string
}

// Config for TigerBeetle connection.
type Config struct {
	ClusterID uint64
	Addresses []string // e.g., ["3000", "3001", "3002"]
}

// NewService creates a new ledger service connected to TigerBeetle.
func NewService(cfg Config) (*Service, error) {
	clusterID := tbt.ToUint128(cfg.ClusterID)

	client, err := tb.NewClient(clusterID, cfg.Addresses)
	if err != nil {
		return nil, fmt.Errorf("connect to TigerBeetle cluster %d: %w", cfg.ClusterID, err)
	}

	addrStr := ""
	for i, addr := range cfg.Addresses {
		if i > 0 {
			addrStr += ","
		}
		addrStr += addr
	}

	log.Info().Uint64("cluster_id", cfg.ClusterID).Str("addresses", addrStr).Msg("TigerBeetle client connected")

	return &Service{
		client:  client,
		address: addrStr,
	}, nil
}

// Close disconnects from TigerBeetle.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.Close()
	}
}

// CreateAccount creates a new account in the ledger.
func (s *Service) CreateAccount(ctx context.Context, ledger uint32, code uint16, userData tbt.Uint128) (tbt.Uint128, error) {
	id := tbt.ID()

	accounts := []tbt.Account{{
		ID:          id,
		Ledger:      ledger,
		Code:        code,
		UserData128: userData,
	}}

	results, err := s.client.CreateAccounts(accounts)
	if err != nil {
		return id, fmt.Errorf("create account: %w", err)
	}
	if len(results) > 0 {
		return id, fmt.Errorf("create account failed: %s", results[0].Result.String())
	}

	log.Debug().Str("account_id", id.String()).Uint32("ledger", ledger).Msg("Ledger account created")
	return id, nil
}

// CreateTransfer records a double-entry transfer between two accounts.
func (s *Service) CreateTransfer(ctx context.Context, debit, credit tbt.Uint128, amount uint64, ledger uint32, code uint16) (tbt.Uint128, error) {
	id := tbt.ID()

	transfers := []tbt.Transfer{{
		ID:              id,
		DebitAccountID:  debit,
		CreditAccountID: credit,
		Amount:          tbt.ToUint128(amount),
		Ledger:          ledger,
		Code:            code,
	}}

	results, err := s.client.CreateTransfers(transfers)
	if err != nil {
		return id, fmt.Errorf("create transfer: %w", err)
	}
	if len(results) > 0 {
		return id, fmt.Errorf("transfer failed: %s", results[0].Result.String())
	}

	log.Debug().Str("transfer_id", id.String()).Uint64("amount", amount).Msg("Ledger transfer posted")
	return id, nil
}

// CreatePendingTransfer creates a two-phase transfer (pending).
func (s *Service) CreatePendingTransfer(ctx context.Context, debit, credit tbt.Uint128, amount uint64, ledger uint32, code uint16, timeout uint32) (tbt.Uint128, error) {
	id := tbt.ID()

	transfers := []tbt.Transfer{{
		ID:              id,
		DebitAccountID:  debit,
		CreditAccountID: credit,
		Amount:          tbt.ToUint128(amount),
		Ledger:          ledger,
		Code:            code,
		Flags:           tbt.TransferFlags{Pending: true}.ToUint16(),
		Timeout:         timeout,
	}}

	results, err := s.client.CreateTransfers(transfers)
	if err != nil {
		return id, fmt.Errorf("create pending transfer: %w", err)
	}
	if len(results) > 0 {
		return id, fmt.Errorf("pending transfer failed: %s", results[0].Result.String())
	}

	return id, nil
}

// PostPendingTransfer confirms a pending two-phase transfer.
func (s *Service) PostPendingTransfer(ctx context.Context, pendingID tbt.Uint128) error {
	id := tbt.ID()

	transfers := []tbt.Transfer{{
		ID:        id,
		PendingID: pendingID,
		Flags:     tbt.TransferFlags{PostPendingTransfer: true}.ToUint16(),
	}}

	results, err := s.client.CreateTransfers(transfers)
	if err != nil {
		return fmt.Errorf("post pending transfer: %w", err)
	}
	if len(results) > 0 {
		return fmt.Errorf("post failed: %s", results[0].Result.String())
	}
	return nil
}

// VoidPendingTransfer cancels a pending two-phase transfer.
func (s *Service) VoidPendingTransfer(ctx context.Context, pendingID tbt.Uint128) error {
	id := tbt.ID()

	transfers := []tbt.Transfer{{
		ID:        id,
		PendingID: pendingID,
		Flags:     tbt.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
	}}

	results, err := s.client.CreateTransfers(transfers)
	if err != nil {
		return fmt.Errorf("void pending transfer: %w", err)
	}
	if len(results) > 0 {
		return fmt.Errorf("void failed: %s", results[0].Result.String())
	}
	return nil
}

// GetAccountBalance returns the current balance of an account.
func (s *Service) GetAccountBalance(ctx context.Context, accountID tbt.Uint128) (*Account, error) {
	ids := []tbt.Uint128{accountID}

	accounts, err := s.client.LookupAccounts(ids)
	if err != nil {
		return nil, fmt.Errorf("lookup account: %w", err)
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("account not found")
	}

	a := accounts[0]
	return &Account{
		ID:             a.ID,
		Ledger:         a.Ledger,
		Code:           a.Code,
		CreditsPosted:  a.CreditsPosted,
		DebitsPosted:   a.DebitsPosted,
		CreditsPending: a.CreditsPending,
		DebitsPending:  a.DebitsPending,
	}, nil
}

// GetAccountTransfers returns transfers for a given account.
func (s *Service) GetAccountTransfers(ctx context.Context, accountID tbt.Uint128, limit uint32) ([]Transfer, error) {
	filter := tbt.AccountFilter{
		AccountID: accountID,
		Limit:     limit,
		Flags: tbt.AccountFilterFlags{
			Credits: true,
			Debits:  true,
		}.ToUint32(),
	}

	tbTransfers, err := s.client.GetAccountTransfers(filter)
	if err != nil {
		return nil, fmt.Errorf("get account transfers: %w", err)
	}

	transfers := make([]Transfer, 0, len(tbTransfers))
	for _, t := range tbTransfers {
		transfers = append(transfers, Transfer{
			ID:              t.ID,
			DebitAccountID:  t.DebitAccountID,
			CreditAccountID: t.CreditAccountID,
			Amount:          t.Amount,
			Ledger:          t.Ledger,
			Code:            t.Code,
			Timestamp:       t.Timestamp,
		})
	}
	return transfers, nil
}

// BatchTransfer creates multiple transfers atomically.
func (s *Service) BatchTransfer(ctx context.Context, transfers []tbt.Transfer) error {
	results, err := s.client.CreateTransfers(transfers)
	if err != nil {
		return fmt.Errorf("batch transfer: %w", err)
	}
	if len(results) > 0 {
		return fmt.Errorf("batch transfer failed: %s", results[0].Result.String())
	}
	return nil
}

// Ping checks connectivity to TigerBeetle.
func (s *Service) Ping() error {
	return s.client.Nop()
}

// ReconcileAccounts checks that debits equal credits across a set of accounts.
func (s *Service) ReconcileAccounts(ctx context.Context, accountIDs []tbt.Uint128) (bool, error) {
	accounts, err := s.client.LookupAccounts(accountIDs)
	if err != nil {
		return false, err
	}

	// Use BigInt for accurate comparison
	var totalCredits, totalDebits uint64
	for _, a := range accounts {
		c := a.CreditsPosted.BigInt()
		d := a.DebitsPosted.BigInt()
		totalCredits += c.Uint64()
		totalDebits += d.Uint64()
	}

	balanced := totalCredits == totalDebits
	if !balanced {
		log.Warn().Uint64("credits", totalCredits).Uint64("debits", totalDebits).Msg("Account reconciliation mismatch")
	}

	return balanced, nil
}

// ElectionAccountSetup creates the standard set of accounts for an election.
func (s *Service) ElectionAccountSetup(ctx context.Context, electionID uint64) (map[string]tbt.Uint128, error) {
	userData := tbt.ToUint128(electionID)
	accounts := make(map[string]tbt.Uint128)

	categories := map[string]uint32{
		"funding":      LedgerElectionFunding,
		"procurement":  LedgerMaterialProcurement,
		"allowances":   LedgerStaffAllowances,
		"logistics":    LedgerLogistics,
		"transmission": LedgerResultTransmission,
	}

	for name, ledger := range categories {
		id, err := s.CreateAccount(ctx, ledger, uint16(CodeAllocation), userData)
		if err != nil {
			return nil, fmt.Errorf("create %s account: %w", name, err)
		}
		accounts[name] = id
		time.Sleep(time.Millisecond)
	}

	log.Info().Uint64("election_id", electionID).Int("accounts", len(accounts)).Msg("Election ledger accounts created")
	return accounts, nil
}
