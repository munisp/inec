package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// ══════════════════════════════════════════════════════════════════════════════
// eNaira / CBDC / Stablecoin Engine — Election Fund Management
// ══════════════════════════════════════════════════════════════════════════════

type StablecoinEngine struct {
	db *sql.DB
}

type StablecoinWallet struct {
	WalletID    string  `json:"wallet_id"`
	OwnerID     string  `json:"owner_id"`
	OwnerType   string  `json:"owner_type"`
	Currency    string  `json:"currency"`
	Balance     float64 `json:"balance"`
	Status      string  `json:"status"`
	PublicKey   string  `json:"public_key"`
	CreatedAt   string  `json:"created_at"`
}

type StablecoinTx struct {
	TxID        string  `json:"tx_id"`
	FromWallet  string  `json:"from_wallet"`
	ToWallet    string  `json:"to_wallet"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	TxType      string  `json:"tx_type"`
	Purpose     string  `json:"purpose"`
	Status      string  `json:"status"`
	Signature   string  `json:"signature"`
	BlockHash   string  `json:"block_hash"`
	CreatedAt   string  `json:"created_at"`
	ConfirmedAt string  `json:"confirmed_at,omitempty"`
}

func NewStablecoinEngine(database *sql.DB) *StablecoinEngine {
	database.Exec(`CREATE TABLE IF NOT EXISTS stablecoin_wallets (
		wallet_id TEXT PRIMARY KEY,
		owner_id TEXT NOT NULL,
		owner_type TEXT NOT NULL DEFAULT 'institution',
		currency TEXT NOT NULL DEFAULT 'eNGN',
		balance DECIMAL(20,4) NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'active',
		public_key TEXT NOT NULL DEFAULT '',
		private_key_enc TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	database.Exec(`CREATE INDEX IF NOT EXISTS idx_wallets_owner ON stablecoin_wallets(owner_id, owner_type)`)

	database.Exec(`CREATE TABLE IF NOT EXISTS stablecoin_transactions (
		tx_id TEXT PRIMARY KEY,
		from_wallet TEXT NOT NULL DEFAULT '',
		to_wallet TEXT NOT NULL DEFAULT '',
		amount DECIMAL(20,4) NOT NULL,
		currency TEXT NOT NULL DEFAULT 'eNGN',
		tx_type TEXT NOT NULL,
		purpose TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		signature TEXT NOT NULL DEFAULT '',
		block_hash TEXT NOT NULL DEFAULT '',
		metadata JSONB DEFAULT '{}',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		confirmed_at TIMESTAMP
	)`)
	database.Exec(`CREATE INDEX IF NOT EXISTS idx_stablecoin_tx_wallets ON stablecoin_transactions(from_wallet, to_wallet)`)
	database.Exec(`CREATE INDEX IF NOT EXISTS idx_stablecoin_tx_status ON stablecoin_transactions(status, created_at DESC)`)

	database.Exec(`CREATE TABLE IF NOT EXISTS stablecoin_ledger (
		id BIGSERIAL PRIMARY KEY,
		tx_id TEXT NOT NULL REFERENCES stablecoin_transactions(tx_id),
		wallet_id TEXT NOT NULL,
		entry_type TEXT NOT NULL,
		amount DECIMAL(20,4) NOT NULL,
		balance_after DECIMAL(20,4) NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)

	se := &StablecoinEngine{db: database}
	se.seedWallets()
	log.Info().Msg("Stablecoin/CBDC/eNaira engine initialized")
	return se
}

func (s *StablecoinEngine) seedWallets() {
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM stablecoin_wallets`).Scan(&count)
	if count > 0 {
		return
	}

	wallets := []struct {
		walletID, ownerID, ownerType, currency string
		balance                                float64
	}{
		{"INEC-TREASURY-001", "INEC_HQ", "institution", "eNGN", 50000000000},
		{"INEC-ELECTION-FUND-001", "ELECTION_2027", "election", "eNGN", 10000000000},
		{"CBN-ENAIRA-RESERVE", "CBN", "central_bank", "eNGN", 100000000000},
		{"INEC-LOGISTICS-001", "INEC_LOGISTICS", "department", "eNGN", 5000000000},
		{"INEC-STAFF-PAYROLL", "INEC_HR", "department", "eNGN", 2000000000},
	}
	for _, w := range wallets {
		privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		pubKeyBytes := elliptic.MarshalCompressed(privKey.PublicKey.Curve, privKey.PublicKey.X, privKey.PublicKey.Y)
		pubKeyHex := hex.EncodeToString(pubKeyBytes)
		privKeyHex := hex.EncodeToString(privKey.D.Bytes())

		s.db.Exec(`INSERT INTO stablecoin_wallets (wallet_id, owner_id, owner_type, currency, balance, public_key, private_key_enc)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			w.walletID, w.ownerID, w.ownerType, w.currency, w.balance, pubKeyHex, privKeyHex)
	}
	log.Info().Int("wallets", len(wallets)).Msg("Stablecoin wallets seeded")
}

func (s *StablecoinEngine) CreateWallet(ctx context.Context, ownerID, ownerType, currency string) (*StablecoinWallet, error) {
	_ = ctx
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	pubKeyBytes := elliptic.MarshalCompressed(privKey.PublicKey.Curve, privKey.PublicKey.X, privKey.PublicKey.Y)
	pubKeyHex := hex.EncodeToString(pubKeyBytes)
	privKeyHex := hex.EncodeToString(privKey.D.Bytes())

	walletID := fmt.Sprintf("W-%s", hex.EncodeToString(sha256.New().Sum([]byte(ownerID+time.Now().String()))[:8]))

	_, err = s.db.Exec(`INSERT INTO stablecoin_wallets (wallet_id, owner_id, owner_type, currency, public_key, private_key_enc)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		walletID, ownerID, ownerType, currency, pubKeyHex, privKeyHex)
	if err != nil {
		return nil, fmt.Errorf("create wallet: %w", err)
	}

	return &StablecoinWallet{
		WalletID:  walletID,
		OwnerID:   ownerID,
		OwnerType: ownerType,
		Currency:  currency,
		Balance:   0,
		Status:    "active",
		PublicKey: pubKeyHex,
	}, nil
}

func (s *StablecoinEngine) Transfer(ctx context.Context, fromWallet, toWallet string, amount float64, purpose, txType string) (*StablecoinTx, error) {
	_ = ctx
	if amount <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Lock sender wallet and check balance
	var senderBalance float64
	var senderPrivKeyHex string
	err = tx.QueryRow(`SELECT balance, private_key_enc FROM stablecoin_wallets 
		WHERE wallet_id=$1 AND status='active' FOR UPDATE`, fromWallet).Scan(&senderBalance, &senderPrivKeyHex)
	if err != nil {
		return nil, fmt.Errorf("sender wallet not found or inactive")
	}
	if senderBalance < amount {
		return nil, fmt.Errorf("insufficient balance: have %.4f, need %.4f", senderBalance, amount)
	}

	// Lock receiver wallet
	var receiverBalance float64
	err = tx.QueryRow(`SELECT balance FROM stablecoin_wallets 
		WHERE wallet_id=$1 AND status='active' FOR UPDATE`, toWallet).Scan(&receiverBalance)
	if err != nil {
		return nil, fmt.Errorf("receiver wallet not found or inactive")
	}

	// Generate transaction ID and signature
	txID := fmt.Sprintf("TX-%s", hex.EncodeToString(sha256.New().Sum([]byte(fromWallet+toWallet+fmt.Sprintf("%.4f", amount)+time.Now().String()))[:12]))
	signature := signTransaction(senderPrivKeyHex, txID, amount)
	blockHash := computeBlockHash(txID, fromWallet, toWallet, amount)

	// Debit sender
	newSenderBalance := senderBalance - amount
	tx.Exec(`UPDATE stablecoin_wallets SET balance=$1, updated_at=NOW() WHERE wallet_id=$2`, newSenderBalance, fromWallet)

	// Credit receiver
	newReceiverBalance := receiverBalance + amount
	tx.Exec(`UPDATE stablecoin_wallets SET balance=$1, updated_at=NOW() WHERE wallet_id=$2`, newReceiverBalance, toWallet)

	// Record transaction
	tx.Exec(`INSERT INTO stablecoin_transactions (tx_id, from_wallet, to_wallet, amount, tx_type, purpose, status, signature, block_hash, confirmed_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'confirmed', $7, $8, NOW())`,
		txID, fromWallet, toWallet, amount, txType, purpose, signature, blockHash)

	// Double-entry ledger
	tx.Exec(`INSERT INTO stablecoin_ledger (tx_id, wallet_id, entry_type, amount, balance_after)
		VALUES ($1, $2, 'debit', $3, $4)`, txID, fromWallet, amount, newSenderBalance)
	tx.Exec(`INSERT INTO stablecoin_ledger (tx_id, wallet_id, entry_type, amount, balance_after)
		VALUES ($1, $2, 'credit', $3, $4)`, txID, toWallet, amount, newReceiverBalance)

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &StablecoinTx{
		TxID:      txID,
		FromWallet: fromWallet,
		ToWallet:   toWallet,
		Amount:    amount,
		Currency:  "eNGN",
		TxType:    txType,
		Purpose:   purpose,
		Status:    "confirmed",
		Signature: signature,
		BlockHash: blockHash,
	}, nil
}

func (s *StablecoinEngine) GetWallet(walletID string) (*StablecoinWallet, error) {
	var w StablecoinWallet
	var createdAt time.Time
	err := s.db.QueryRow(`SELECT wallet_id, owner_id, owner_type, currency, balance, status, public_key, created_at 
		FROM stablecoin_wallets WHERE wallet_id=$1`, walletID).Scan(
		&w.WalletID, &w.OwnerID, &w.OwnerType, &w.Currency, &w.Balance, &w.Status, &w.PublicKey, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("wallet not found")
	}
	w.CreatedAt = createdAt.Format(time.RFC3339)
	return &w, nil
}

func (s *StablecoinEngine) ListTransactions(walletID string, limit int) ([]StablecoinTx, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT tx_id, from_wallet, to_wallet, amount, currency, tx_type, purpose, status, signature, block_hash, created_at
		FROM stablecoin_transactions WHERE from_wallet=$1 OR to_wallet=$1 ORDER BY created_at DESC LIMIT $2`,
		walletID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var txs []StablecoinTx
	for rows.Next() {
		var t StablecoinTx
		var createdAt time.Time
		rows.Scan(&t.TxID, &t.FromWallet, &t.ToWallet, &t.Amount, &t.Currency, &t.TxType, &t.Purpose, &t.Status, &t.Signature, &t.BlockHash, &createdAt)
		t.CreatedAt = createdAt.Format(time.RFC3339)
		txs = append(txs, t)
	}
	return txs, nil
}

func signTransaction(privKeyHex, txID string, amount float64) string {
	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil || len(privKeyBytes) == 0 {
		return "unsigned"
	}
	privKey := new(ecdsa.PrivateKey)
	privKey.PublicKey.Curve = elliptic.P256()
	privKey.D = new(big.Int).SetBytes(privKeyBytes)
	privKey.PublicKey.X, privKey.PublicKey.Y = privKey.PublicKey.Curve.ScalarBaseMult(privKeyBytes)

	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%.4f", txID, amount)))
	r, s, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
	if err != nil {
		return "sign-error"
	}
	return hex.EncodeToString(r.Bytes()) + hex.EncodeToString(s.Bytes())
}

func computeBlockHash(txID, from, to string, amount float64) string {
	data := fmt.Sprintf("%s:%s:%s:%.4f:%d", txID, from, to, amount, time.Now().UnixNano())
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// ══════════════════════════════════════════════════════════════════════════════
// HTTP Handlers
// ══════════════════════════════════════════════════════════════════════════════

var stablecoinEngine *StablecoinEngine

func initStablecoinEngine() {
	stablecoinEngine = NewStablecoinEngine(db)
}

func handleCreateWallet(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}
	var req struct {
		OwnerID   string `json:"owner_id"`
		OwnerType string `json:"owner_type"`
		Currency  string `json:"currency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid body")
		return
	}
	if req.Currency == "" {
		req.Currency = "eNGN"
	}
	wallet, err := stablecoinEngine.CreateWallet(r.Context(), req.OwnerID, req.OwnerType, req.Currency)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	logAudit("WALLET_CREATED", "stablecoin", wallet.WalletID, 0, map[string]interface{}{"owner": req.OwnerID, "type": req.OwnerType})
	writeJSON(w, 201, wallet)
}

func handleTransfer(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}
	var req struct {
		FromWallet string  `json:"from_wallet"`
		ToWallet   string  `json:"to_wallet"`
		Amount     float64 `json:"amount"`
		Purpose    string  `json:"purpose"`
		TxType     string  `json:"tx_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid body")
		return
	}
	if req.TxType == "" {
		req.TxType = "transfer"
	}
	txn, err := stablecoinEngine.Transfer(r.Context(), req.FromWallet, req.ToWallet, req.Amount, req.Purpose, req.TxType)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	mwHub.Kafka.Produce(r.Context(), KafkaMessage{
		Topic: "inec.stablecoin.transfer",
		Key:   txn.TxID,
		Value: M{"event": "transfer", "amount": req.Amount, "from": req.FromWallet, "to": req.ToWallet},
	})

	logAudit("STABLECOIN_TRANSFER", "stablecoin", txn.TxID, 0, map[string]interface{}{"amount": req.Amount, "from": req.FromWallet, "to": req.ToWallet})
	writeJSON(w, 200, txn)
}

func handleGetWallet(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}
	walletID := r.URL.Query().Get("wallet_id")
	if walletID == "" {
		writeError(w, 400, "wallet_id required")
		return
	}
	wallet, err := stablecoinEngine.GetWallet(walletID)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, wallet)
}

func handleListWallets(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}
	rows, qErr := db.Query(`SELECT wallet_id, owner_id, owner_type, currency, balance, status, public_key, created_at 
		FROM stablecoin_wallets ORDER BY created_at ASC`)
	if qErr != nil {
		writeError(w, 500, qErr.Error())
		return
	}
	defer rows.Close()
	var wallets []StablecoinWallet
	for rows.Next() {
		var wl StablecoinWallet
		var createdAt time.Time
		rows.Scan(&wl.WalletID, &wl.OwnerID, &wl.OwnerType, &wl.Currency, &wl.Balance, &wl.Status, &wl.PublicKey, &createdAt)
		wl.CreatedAt = createdAt.Format(time.RFC3339)
		wallets = append(wallets, wl)
	}
	writeJSON(w, 200, M{"wallets": wallets, "total": len(wallets)})
}

func handleWalletTransactions(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}
	walletID := r.URL.Query().Get("wallet_id")
	if walletID == "" {
		writeError(w, 400, "wallet_id required")
		return
	}
	txs, err := stablecoinEngine.ListTransactions(walletID, 50)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, M{"transactions": txs, "total": len(txs)})
}

func handleStablecoinDashboard(w http.ResponseWriter, r *http.Request) {
	_, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}

	var walletCount int
	var totalBalance float64
	var txCount int
	var totalVolume float64
	db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(balance), 0) FROM stablecoin_wallets`).Scan(&walletCount, &totalBalance)
	db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM stablecoin_transactions WHERE status='confirmed'`).Scan(&txCount, &totalVolume)

	writeJSON(w, 200, M{
		"wallets":      M{"total": walletCount, "total_balance": totalBalance},
		"transactions": M{"total": txCount, "total_volume": totalVolume},
		"currencies":   []string{"eNGN"},
		"engine":       "ecdsa-p256-signed-double-entry",
	})
}
