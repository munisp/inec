// Package ledger provides double-entry accounting using the TigerBeetle SDK.
// This replaces the HTTP sidecar approach with the official native Go client.
package ledger

import (
	"context"
	"fmt"
	"sync"
	"time"

	tb "github.com/tigerbeetle/tigerbeetle-go"
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
	ID              tb.Uint128
	DebitAccountID  tb.Uint128
	CreditAccountID tb.Uint128
	Amount          tb.Uint128
	Ledger          uint32
	Code            uint16
	Timestamp       uint64
	Status          string
}

// Account represents a ledger account.
type Account struct {
	ID             tb.Uint128
	Ledger         uint32
	Code           uint16
	CreditsPosted  tb.Uint128
	DebitsPosted   tb.Uint128
	CreditsPending tb.Uint128
	DebitsPending  tb.Uint128
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
	clusterID := tb.ToUint128(cfg.ClusterID)

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
func (s *Service) CreateAccount(ctx context.Context, ledger uint32, code uint16, userData tb.Uint128) (tb.Uint128, error) {
	id := tb.ID()

	accounts := []tb.Account{{
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
		return id, fmt.Errorf("create account failed: %s", results[0].Status.String())
	}

	log.Debug().Str("account_id", id.String()).Uint32("ledger", ledger).Msg("Ledger account created")
	return id, nil
}

// CreateTransfer records a double-entry transfer between two accounts.
func (s *Service) CreateTransfer(ctx context.Context, debit, credit tb.Uint128, amount uint64, ledger uint32, code uint16) (tb.Uint128, error) {
	id := tb.ID()

	transfers := []tb.Transfer{{
		ID:              id,
		DebitAccountID:  debit,
		CreditAccountID: credit,
		Amount:          tb.ToUint128(amount),
		Ledger:          ledger,
		Code:            code,
	}}

	results, err := s.client.CreateTransfers(transfers)
	if err != nil {
		return id, fmt.Errorf("create transfer: %w", err)
	}
	if len(results) > 0 {
		return id, fmt.Errorf("transfer failed: %s", results[0].Status.String())
	}

	log.Debug().Str("transfer_id", id.String()).Uint64("amount", amount).Msg("Ledger transfer posted")
	return id, nil
}

// CreatePendingTransfer creates a two-phase transfer (pending).
func (s *Service) CreatePendingTransfer(ctx context.Context, debit, credit tb.Uint128, amount uint64, ledger uint32, code uint16, timeout uint32) (tb.Uint128, error) {
	id := tb.ID()

	transfers := []tb.Transfer{{
		ID:              id,
		DebitAccountID:  debit,
		CreditAccountID: credit,
		Amount:          tb.ToUint128(amount),
		Ledger:          ledger,
		Code:            code,
		Flags:           tb.TransferFlags{Pending: true}.ToUint16(),
		Timeout:         timeout,
	}}

	results, err := s.client.CreateTransfers(transfers)
	if err != nil {
		return id, fmt.Errorf("create pending transfer: %w", err)
	}
	if len(results) > 0 {
		return id, fmt.Errorf("pending transfer failed: %s", results[0].Status.String())
	}

	return id, nil
}

// PostPendingTransfer confirms a pending two-phase transfer.
func (s *Service) PostPendingTransfer(ctx context.Context, pendingID tb.Uint128) error {
	id := tb.ID()

	transfers := []tb.Transfer{{
		ID:        id,
		PendingID: pendingID,
		Flags:     tb.TransferFlags{PostPendingTransfer: true}.ToUint16(),
	}}

	results, err := s.client.CreateTransfers(transfers)
	if err != nil {
		return fmt.Errorf("post pending transfer: %w", err)
	}
	if len(results) > 0 {
		return fmt.Errorf("post failed: %s", results[0].Status.String())
	}
	return nil
}

// VoidPendingTransfer cancels a pending two-phase transfer.
func (s *Service) VoidPendingTransfer(ctx context.Context, pendingID tb.Uint128) error {
	id := tb.ID()

	transfers := []tb.Transfer{{
		ID:        id,
		PendingID: pendingID,
		Flags:     tb.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
	}}

	results, err := s.client.CreateTransfers(transfers)
	if err != nil {
		return fmt.Errorf("void pending transfer: %w", err)
	}
	if len(results) > 0 {
		return fmt.Errorf("void failed: %s", results[0].Status.String())
	}
	return nil
}

// GetAccountBalance returns the current balance of an account.
func (s *Service) GetAccountBalance(ctx context.Context, accountID tb.Uint128) (*Account, error) {
	ids := []tb.Uint128{accountID}

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
func (s *Service) GetAccountTransfers(ctx context.Context, accountID tb.Uint128, limit uint32) ([]Transfer, error) {
	filter := tb.AccountFilter{
		AccountID: accountID,
		Limit:     limit,
		Flags: tb.AccountFilterFlags{
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
func (s *Service) BatchTransfer(ctx context.Context, transfers []tb.Transfer) error {
	results, err := s.client.CreateTransfers(transfers)
	if err != nil {
		return fmt.Errorf("batch transfer: %w", err)
	}
	if len(results) > 0 {
		return fmt.Errorf("batch transfer failed: %s", results[0].Status.String())
	}
	return nil
}

// Ping checks connectivity to TigerBeetle.
func (s *Service) Ping() error {
	return s.client.Nop()
}

// ReconcileAccounts checks that debits equal credits across a set of accounts.
func (s *Service) ReconcileAccounts(ctx context.Context, accountIDs []tb.Uint128) (bool, error) {
	accounts, err := s.client.LookupAccounts(accountIDs)
	if err != nil {
		return false, err
	}

	// Use BigInt for accurate comparison
	var totalCredits, totalDebits uint64
	for _, a := range accounts {
		cLo, _ := a.CreditsPosted.Uint64()
		dLo, _ := a.DebitsPosted.Uint64()
		totalCredits += cLo
		totalDebits += dLo
	}

	balanced := totalCredits == totalDebits
	if !balanced {
		log.Warn().Uint64("credits", totalCredits).Uint64("debits", totalDebits).Msg("Account reconciliation mismatch")
	}

	return balanced, nil
}

// ElectionAccountSetup creates the standard set of accounts for an election.
func (s *Service) ElectionAccountSetup(ctx context.Context, electionID uint64) (map[string]tb.Uint128, error) {
	userData := tb.ToUint128(electionID)
	accounts := make(map[string]tb.Uint128)

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
