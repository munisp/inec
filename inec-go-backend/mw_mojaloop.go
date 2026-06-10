package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog/log"
)

// MojaloopClient implements the 4-Phase Transaction Pattern:
// Discovery → Quote → Transfer → Settlement
//
// FSPIOP is async: requests return 202 Accepted, responses arrive via PUT callbacks.
// The client supports both sync mode (polling) and async mode (callback registration).
type MojaloopClient interface {
	// Discovery phase: lookup payee
	PartyLookup(ctx context.Context, partyType, partyID string) (*MojaParty, error)
	// Quote phase: get transfer terms
	CreateQuote(ctx context.Context, req MojaQuoteRequest) (*MojaQuote, error)
	// Transfer phase: execute transfer
	CreateTransfer(ctx context.Context, req MojaTransferRequest) (*MojaTransfer, error)
	// Settlement phase: settle batch
	SettleBatch(ctx context.Context, settlementModel string) (*MojaSettlement, error)
	// Get transaction by ID
	GetTransaction(ctx context.Context, txID string) (*MojaTransaction, error)
	// List transactions
	ListTransactions(ctx context.Context, phase string, limit int) ([]MojaTransaction, error)
	// HandleCallback processes async FSPIOP PUT callbacks from the switch
	HandleCallback(ctx context.Context, callbackType string, resourceID string, payload []byte) error
	// RegisterCallbackURL sets the URL where the switch should send async responses
	RegisterCallbackURL(callbackURL string)
	Status() MWStatus
	Close() error
}

// MojaCallback represents an async FSPIOP callback from the switch.
type MojaCallback struct {
	Type       string          `json:"type"`       // "quote", "transfer", "party"
	ResourceID string          `json:"resource_id"`
	Status     string          `json:"status"`     // "success", "error"
	Payload    json.RawMessage `json:"payload"`
	ReceivedAt string          `json:"received_at"`
}

type MojaParty struct {
	PartyType   string `json:"party_type"`
	PartyID     string `json:"party_id"`
	FSPName     string `json:"fsp_name"`
	FSPID       string `json:"fsp_id"`
	Name        string `json:"name"`
	DateOfBirth string `json:"date_of_birth,omitempty"`
}

type MojaQuoteRequest struct {
	QuoteID  string  `json:"quote_id"`
	PayerFSP string  `json:"payer_fsp"`
	PayeeFSP string  `json:"payee_fsp"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type MojaQuote struct {
	QuoteID         string  `json:"quote_id"`
	TransferAmount  float64 `json:"transfer_amount"`
	PayeeFee        float64 `json:"payee_fee"`
	PayeeCommission float64 `json:"payee_commission"`
	ILPPacket       string  `json:"ilp_packet"`
	Condition       string  `json:"condition"`
	Expiration      string  `json:"expiration"`
}

type MojaTransferRequest struct {
	TransferID string  `json:"transfer_id"`
	QuoteID    string  `json:"quote_id"`
	PayerFSP   string  `json:"payer_fsp"`
	PayeeFSP   string  `json:"payee_fsp"`
	Amount     float64 `json:"amount"`
	Currency   string  `json:"currency"`
	ILPPacket  string  `json:"ilp_packet"`
	Condition  string  `json:"condition"`
}

type MojaTransfer struct {
	TransferID  string `json:"transfer_id"`
	Fulfilment  string `json:"fulfilment"`
	State       string `json:"state"`
	CompletedAt string `json:"completed_at"`
}

type MojaSettlement struct {
	SettlementID string           `json:"settlement_id"`
	State        string           `json:"state"`
	Accounts     []MojaSettleAcct `json:"accounts"`
	CreatedAt    string           `json:"created_at"`
}

type MojaSettleAcct struct {
	FSPID  string  `json:"fsp_id"`
	Credit float64 `json:"credit"`
	Debit  float64 `json:"debit"`
	NetPos float64 `json:"net_position"`
}

type MojaTransaction struct {
	ID           string  `json:"id"`
	PayerFSP     string  `json:"payer_fsp"`
	PayeeFSP     string  `json:"payee_fsp"`
	Amount       float64 `json:"amount"`
	Currency     string  `json:"currency"`
	Phase        string  `json:"phase"`
	QuoteID      string  `json:"quote_id,omitempty"`
	TransferID   string  `json:"transfer_id,omitempty"`
	SettlementID string  `json:"settlement_id,omitempty"`
	ILPPacket    string  `json:"ilp_packet,omitempty"`
	Condition    string  `json:"condition,omitempty"`
	Fulfilment   string  `json:"fulfilment,omitempty"`
	ErrorInfo    string  `json:"error_info,omitempty"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// HTTP client for real Mojaloop Switch
type mojaHTTPClient struct {
	client      *ResilientHTTPClient
	baseURL     string
	callbackURL string // URL where switch sends async FSPIOP PUT responses
}

func (m *mojaHTTPClient) PartyLookup(ctx context.Context, partyType, partyID string) (*MojaParty, error) {
	url := fmt.Sprintf("%s/parties/%s/%s", m.baseURL, partyType, partyID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.interoperability.parties+json;version=1.1")
	req.Header.Set("Content-Type", "application/vnd.interoperability.parties+json;version=1.1")
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("party lookup: %w", err)
	}
	defer resp.Body.Close()
	var party MojaParty
	if err := json.NewDecoder(resp.Body).Decode(&party); err != nil {
		return nil, fmt.Errorf("decode party: %w", err)
	}
	return &party, nil
}

func (m *mojaHTTPClient) CreateQuote(ctx context.Context, qr MojaQuoteRequest) (*MojaQuote, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"quoteId":    qr.QuoteID,
		"payer":      map[string]string{"partyIdType": "MSISDN", "partyIdentifier": qr.PayerFSP, "fspId": qr.PayerFSP},
		"payee":      map[string]string{"partyIdType": "MSISDN", "partyIdentifier": qr.PayeeFSP, "fspId": qr.PayeeFSP},
		"amountType": "SEND",
		"amount":     map[string]interface{}{"currency": qr.Currency, "amount": fmt.Sprintf("%.2f", qr.Amount)},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", m.baseURL+"/quotes", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.interoperability.quotes+json;version=1.1")
	req.Header.Set("Content-Type", "application/vnd.interoperability.quotes+json;version=1.1")
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create quote: %w", err)
	}
	defer resp.Body.Close()
	var quote MojaQuote
	json.NewDecoder(resp.Body).Decode(&quote)
	return &quote, nil
}

func (m *mojaHTTPClient) CreateTransfer(ctx context.Context, tr MojaTransferRequest) (*MojaTransfer, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"transferId": tr.TransferID,
		"payerFsp":   tr.PayerFSP,
		"payeeFsp":   tr.PayeeFSP,
		"amount":     map[string]interface{}{"currency": tr.Currency, "amount": fmt.Sprintf("%.2f", tr.Amount)},
		"ilpPacket":  tr.ILPPacket,
		"condition":  tr.Condition,
		"expiration": time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
	})
	req, err := http.NewRequestWithContext(ctx, "POST", m.baseURL+"/transfers", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.interoperability.transfers+json;version=1.1")
	req.Header.Set("Content-Type", "application/vnd.interoperability.transfers+json;version=1.1")
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}
	defer resp.Body.Close()
	var transfer MojaTransfer
	json.NewDecoder(resp.Body).Decode(&transfer)
	return &transfer, nil
}

func (m *mojaHTTPClient) SettleBatch(ctx context.Context, settlementModel string) (*MojaSettlement, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"reason":          "election settlement batch",
		"settlementModel": settlementModel,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", m.baseURL+"/settlements", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("settle batch: %w", err)
	}
	defer resp.Body.Close()
	var settlement MojaSettlement
	json.NewDecoder(resp.Body).Decode(&settlement)
	return &settlement, nil
}

func (m *mojaHTTPClient) GetTransaction(ctx context.Context, txID string) (*MojaTransaction, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", m.baseURL+"/transactions/"+txID, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get transaction: %w", err)
	}
	defer resp.Body.Close()
	var tx MojaTransaction
	json.NewDecoder(resp.Body).Decode(&tx)
	return &tx, nil
}

func (m *mojaHTTPClient) ListTransactions(ctx context.Context, phase string, limit int) ([]MojaTransaction, error) {
	url := fmt.Sprintf("%s/transactions?phase=%s&limit=%d", m.baseURL, phase, limit)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	defer resp.Body.Close()
	var txs []MojaTransaction
	json.NewDecoder(resp.Body).Decode(&txs)
	return txs, nil
}

func (m *mojaHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Try /health first, then root path — TTK may not have /health
	var lat time.Duration
	var lastErr error
	for _, path := range []string{"/health", "/"} {
		req, _ := http.NewRequestWithContext(ctx, "GET", m.baseURL+path, nil)
		req.Header.Set("Accept", "application/json")
		l, err := measureLatency(func() error {
			resp, e := m.client.Client.Do(req)
			if e != nil {
				return e
			}
			resp.Body.Close()
			return nil
		})
		if err == nil {
			lat = l
			lastErr = nil
			break
		}
		lastErr = err
	}
	if lastErr != nil {
		return MWStatus{Name: "Mojaloop", Connected: false, Mode: "external (unreachable)", Details: lastErr.Error()}
	}
	return MWStatus{Name: "Mojaloop", Connected: true, Mode: "external (FSPIOP)", Latency: fmtLatency(lat)}
}

// HandleCallback processes async FSPIOP PUT callbacks from the Mojaloop switch.
// In production, the switch sends PUT /quotes/{id}, PUT /transfers/{id} with results.
func (m *mojaHTTPClient) HandleCallback(ctx context.Context, callbackType string, resourceID string, payload []byte) error {
	log.Info().Str("type", callbackType).Str("resource_id", resourceID).Msg("mojaloop: received async callback")
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO mw_mojaloop_callbacks (type, resource_id, payload, status, received_at)
		 VALUES ($1, $2, $3, 'received', NOW())
		 ON CONFLICT (resource_id, type) DO UPDATE SET payload=$3, status='received', received_at=NOW()`,
		callbackType, resourceID, string(payload))
	return err
}

func (m *mojaHTTPClient) RegisterCallbackURL(callbackURL string) {
	m.callbackURL = callbackURL
}

// Embedded Mojaloop implementation backed by PostgreSQL
type embeddedMojaloop struct {
	callbackURL string
}

func (m *mojaHTTPClient) Close() error { return nil }

func (m *embeddedMojaloop) PartyLookup(ctx context.Context, partyType, partyID string) (*MojaParty, error) {
	return &MojaParty{
		PartyType: partyType,
		PartyID:   partyID,
		FSPName:   "INEC Financial Unit",
		FSPID:     "inec-fsp",
		Name:      "INEC " + partyType + " Account",
	}, nil
}

func (m *embeddedMojaloop) CreateQuote(ctx context.Context, req MojaQuoteRequest) (*MojaQuote, error) {
	// Generate ILP packet and condition
	ilpData := fmt.Sprintf("%s:%s:%.2f:%s", req.PayerFSP, req.PayeeFSP, req.Amount, req.Currency)
	hash := sha256.Sum256([]byte(ilpData))
	condition := base64.StdEncoding.EncodeToString(hash[:])

	quote := &MojaQuote{
		QuoteID:         req.QuoteID,
		TransferAmount:  req.Amount,
		PayeeFee:        req.Amount * 0.001,
		PayeeCommission: 0,
		ILPPacket:       base64.StdEncoding.EncodeToString([]byte(ilpData)),
		Condition:       condition,
		Expiration:      time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
	}

	// Persist to DB — advance phase to 'quote'
	txID := req.QuoteID
	_, err := db.ExecContext(ctx,
		`INSERT INTO mw_mojaloop_transactions (id, payer_fsp, payee_fsp, amount, currency, phase, quote_id, ilp_packet, condition)
		 VALUES (?, ?, ?, ?, ?, 'quote', ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET phase='quote', quote_id=excluded.quote_id, ilp_packet=excluded.ilp_packet, condition=excluded.condition, updated_at=CURRENT_TIMESTAMP`,
		txID, req.PayerFSP, req.PayeeFSP, req.Amount, req.Currency, req.QuoteID, quote.ILPPacket, quote.Condition)
	if err != nil {
		log.Warn().Err(err).Msg("mojaloop: persist quote error")
	}
	return quote, nil
}

func (m *embeddedMojaloop) CreateTransfer(ctx context.Context, req MojaTransferRequest) (*MojaTransfer, error) {
	// Generate fulfilment from condition
	fulfilData := fmt.Sprintf("fulfil:%s:%s", req.TransferID, req.Condition)
	hash := sha256.Sum256([]byte(fulfilData))
	fulfilment := base64.StdEncoding.EncodeToString(hash[:])

	transfer := &MojaTransfer{
		TransferID:  req.TransferID,
		Fulfilment:  fulfilment,
		State:       "COMMITTED",
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Persist — advance phase to 'transfer'
	_, err := db.ExecContext(ctx,
		`UPDATE mw_mojaloop_transactions SET phase='transfer', transfer_id=?, fulfilment=?, updated_at=CURRENT_TIMESTAMP WHERE quote_id=?`,
		req.TransferID, fulfilment, req.QuoteID)
	if err != nil {
		log.Warn().Err(err).Msg("mojaloop: persist transfer error")
	}
	return transfer, nil
}

func (m *embeddedMojaloop) SettleBatch(ctx context.Context, settlementModel string) (*MojaSettlement, error) {
	settlementID := fmt.Sprintf("settle-%d", time.Now().UnixNano())

	// Aggregate all committed transfers
	rows, err := db.QueryContext(ctx,
		`SELECT payer_fsp, payee_fsp, SUM(amount) FROM mw_mojaloop_transactions WHERE phase='transfer' GROUP BY payer_fsp, payee_fsp`)
	if err != nil {
		return nil, fmt.Errorf("settlement query: %w", err)
	}
	defer rows.Close()

	accountMap := make(map[string]*MojaSettleAcct)
	for rows.Next() {
		var payer, payee string
		var amount float64
		if err := rows.Scan(&payer, &payee, &amount); err != nil {
			continue
		}
		if _, ok := accountMap[payer]; !ok {
			accountMap[payer] = &MojaSettleAcct{FSPID: payer}
		}
		if _, ok := accountMap[payee]; !ok {
			accountMap[payee] = &MojaSettleAcct{FSPID: payee}
		}
		accountMap[payer].Debit += amount
		accountMap[payee].Credit += amount
	}

	var accounts []MojaSettleAcct
	for _, acct := range accountMap {
		acct.NetPos = acct.Credit - acct.Debit
		accounts = append(accounts, *acct)
	}

	// Mark transfers as settled
	db.ExecContext(ctx,
		`UPDATE mw_mojaloop_transactions SET phase='settlement', settlement_id=?, updated_at=CURRENT_TIMESTAMP WHERE phase='transfer'`,
		settlementID)

	return &MojaSettlement{
		SettlementID: settlementID,
		State:        "SETTLED",
		Accounts:     accounts,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (m *embeddedMojaloop) GetTransaction(ctx context.Context, txID string) (*MojaTransaction, error) {
	var tx MojaTransaction
	var quoteID, transferID, settlementID, ilp, condition, fulfilment, errInfo sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT id, payer_fsp, payee_fsp, amount, currency, phase, quote_id, transfer_id, settlement_id, ilp_packet, condition, fulfilment, error_info, created_at, updated_at
		 FROM mw_mojaloop_transactions WHERE id=?`, txID).Scan(
		&tx.ID, &tx.PayerFSP, &tx.PayeeFSP, &tx.Amount, &tx.Currency, &tx.Phase,
		&quoteID, &transferID, &settlementID, &ilp, &condition, &fulfilment, &errInfo,
		&tx.CreatedAt, &tx.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("transaction not found: %w", err)
	}
	tx.QuoteID = quoteID.String
	tx.TransferID = transferID.String
	tx.SettlementID = settlementID.String
	tx.ILPPacket = ilp.String
	tx.Condition = condition.String
	tx.Fulfilment = fulfilment.String
	tx.ErrorInfo = errInfo.String
	return &tx, nil
}

func (m *embeddedMojaloop) ListTransactions(ctx context.Context, phase string, limit int) ([]MojaTransaction, error) {
	query := `SELECT id, payer_fsp, payee_fsp, amount, currency, phase, created_at, updated_at FROM mw_mojaloop_transactions`
	args := []interface{}{}
	if phase != "" {
		query += ` WHERE phase=?`
		args = append(args, phase)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []MojaTransaction
	for rows.Next() {
		var tx MojaTransaction
		if err := rows.Scan(&tx.ID, &tx.PayerFSP, &tx.PayeeFSP, &tx.Amount, &tx.Currency, &tx.Phase, &tx.CreatedAt, &tx.UpdatedAt); err != nil {
			continue
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func (m *embeddedMojaloop) Status() MWStatus {
	return MWStatus{Name: "Mojaloop", Connected: true, Mode: "embedded (DB-backed)", Details: "4-phase ILP pattern"}
}

func (m *embeddedMojaloop) HandleCallback(ctx context.Context, callbackType string, resourceID string, payload []byte) error {
	log.Info().Str("type", callbackType).Str("resource_id", resourceID).Msg("mojaloop embedded: callback received")
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO mw_mojaloop_callbacks (type, resource_id, payload, status, received_at)
		 VALUES ($1, $2, $3, 'received', NOW())
		 ON CONFLICT (resource_id, type) DO UPDATE SET payload=$3, status='received', received_at=NOW()`,
		callbackType, resourceID, string(payload))
	return err
}

func (m *embeddedMojaloop) RegisterCallbackURL(callbackURL string) {
	m.callbackURL = callbackURL
}

func (m *embeddedMojaloop) Close() error { return nil }

func initMojaloopClient() MojaloopClient {
	baseURL := os.Getenv("MOJALOOP_URL")
	if baseURL != "" {
		log.Info().Str("url", baseURL).Msg("Mojaloop: connecting")
		client := &mojaHTTPClient{
			client:  NewResilientHTTPClient("mojaloop"),
			baseURL: baseURL,
		}
		// Check connectivity via /health endpoint (TTK), PartyLookup, or simple GET
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		connected := false
		// Try /health first (Mojaloop TTK)
		req, _ := http.NewRequestWithContext(ctx, "GET", baseURL+"/health", nil) // #nosec G704 -- baseURL is admin-configured env var
		if resp, err := client.client.Client.Do(req); err == nil {               // #nosec G704
			resp.Body.Close()
			if resp.StatusCode < 500 {
				connected = true
			}
		}
		if !connected {
			// Try PartyLookup as fallback
			_, err := client.PartyLookup(ctx, "MSISDN", "test")
			if err == nil {
				connected = true
			}
		}
		if connected {
			log.Info().Str("url", baseURL).Msg("Mojaloop connected to external service")
			return client
		}
		log.Warn().Msg("Mojaloop: external connection failed, using embedded")
	}
	env := os.Getenv("APP_ENV")
	if env == "production" || env == "staging" {
		log.Fatal().Msg("Mojaloop is REQUIRED in production/staging for financial settlement. Set MOJALOOP_URL")
	}
	log.Warn().Msg("Mojaloop using embedded DB-backed implementation (DEV ONLY)")
	return &embeddedMojaloop{}
}

// HTTP handlers for Mojaloop endpoints
func handleMojaPartyLookup(w http.ResponseWriter, r *http.Request) {
	partyType := queryParam(r, "type", "MSISDN")
	partyID := queryParam(r, "id", "")
	if partyID == "" {
		writeError(w, 400, "party id required")
		return
	}
	party, err := mwHub.Mojaloop.PartyLookup(r.Context(), partyType, partyID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, party)
}

func handleMojaCreateQuote(w http.ResponseWriter, r *http.Request) {
	var req MojaQuoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.QuoteID == "" {
		req.QuoteID = fmt.Sprintf("quote-%d", time.Now().UnixNano())
	}
	if req.Currency == "" {
		req.Currency = "NGN"
	}
	quote, err := mwHub.Mojaloop.CreateQuote(r.Context(), req)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, quote)
}

func handleMojaCreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req MojaTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.TransferID == "" {
		req.TransferID = fmt.Sprintf("transfer-%d", time.Now().UnixNano())
	}
	transfer, err := mwHub.Mojaloop.CreateTransfer(r.Context(), req)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, transfer)
}

func handleMojaSettle(w http.ResponseWriter, r *http.Request) {
	model := queryParam(r, "model", "DEFERRED_NET")
	settlement, err := mwHub.Mojaloop.SettleBatch(r.Context(), model)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, settlement)
}

func handleMojaTransactions(w http.ResponseWriter, r *http.Request) {
	phase := queryParam(r, "phase", "")
	limit := queryParamInt(r, "limit", 50)
	txs, err := mwHub.Mojaloop.ListTransactions(r.Context(), phase, limit)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if txs == nil {
		txs = []MojaTransaction{}
	}
	writeJSON(w, 200, M{"transactions": txs, "count": len(txs)})
}

func handleMojaStatus(w http.ResponseWriter, r *http.Request) {
	status := mwHub.Mojaloop.Status()
	writeJSON(w, 200, M{
		"name":      status.Name,
		"connected": status.Connected,
		"mode":      status.Mode,
		"details":   status.Details,
		"phases":    []string{"discovery", "quote", "transfer", "settlement"},
		"protocol":  "ILP (Interledger Protocol)",
	})
}
