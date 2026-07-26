package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
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
// unavailableMojaloopClient never queues, simulates, or fabricates a payment lifecycle.
// It is the safe default until the Electoral Commission has authorized an external
// FSPIOP scheme and explicitly enabled the operational-settlement integration.
type unavailableMojaloopClient struct {
	reason string
}

func (m *unavailableMojaloopClient) unavailable() error {
	return fmt.Errorf("Mojaloop is unavailable: %s", m.reason)
}
func (m *unavailableMojaloopClient) PartyLookup(context.Context, string, string) (*MojaParty, error) {
	return nil, m.unavailable()
}
func (m *unavailableMojaloopClient) CreateQuote(context.Context, MojaQuoteRequest) (*MojaQuote, error) {
	return nil, m.unavailable()
}
func (m *unavailableMojaloopClient) CreateTransfer(context.Context, MojaTransferRequest) (*MojaTransfer, error) {
	return nil, m.unavailable()
}
func (m *unavailableMojaloopClient) SettleBatch(context.Context, string) (*MojaSettlement, error) {
	return nil, m.unavailable()
}
func (m *unavailableMojaloopClient) GetTransaction(context.Context, string) (*MojaTransaction, error) {
	return nil, m.unavailable()
}
func (m *unavailableMojaloopClient) ListTransactions(context.Context, string, int) ([]MojaTransaction, error) {
	return nil, m.unavailable()
}
func (m *unavailableMojaloopClient) HandleCallback(context.Context, string, string, []byte) error {
	return m.unavailable()
}
func (m *unavailableMojaloopClient) RegisterCallbackURL(string) {}
func (m *unavailableMojaloopClient) Status() MWStatus {
	return MWStatus{Name: "Mojaloop", Connected: false, Mode: "disabled/unavailable", Details: m.reason}
}
func (m *unavailableMojaloopClient) Close() error { return nil }

type MojaCallback struct {
	Type       string          `json:"type"` // "quote", "transfer", "party"
	ResourceID string          `json:"resource_id"`
	Status     string          `json:"status"` // "success", "error"
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

func newAuthorizedMojaloopClient() (*ResilientHTTPClient, error) {
	certFile := strings.TrimSpace(os.Getenv("MOJALOOP_TLS_CERT_FILE"))
	keyFile := strings.TrimSpace(os.Getenv("MOJALOOP_TLS_KEY_FILE"))
	caFile := strings.TrimSpace(os.Getenv("MOJALOOP_CA_FILE"))
	if certFile == "" || keyFile == "" || caFile == "" {
		return nil, fmt.Errorf("MOJALOOP_TLS_CERT_FILE, MOJALOOP_TLS_KEY_FILE, and MOJALOOP_CA_FILE are required")
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load Mojaloop client certificate: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read Mojaloop CA bundle: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("MOJALOOP_CA_FILE does not contain a trusted PEM certificate")
	}
	client := NewResilientHTTPClient("mojaloop")
	client.Client.Transport = &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			RootCAs:      roots,
			Certificates: []tls.Certificate{certificate},
		},
	}
	return client, nil
}

func initMojaloopClient() MojaloopClient {
	if !envBool("MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED", false) {
		return &unavailableMojaloopClient{reason: "MOJALOOP_OPERATIONAL_SETTLEMENT_ENABLED is false pending INEC scheme authorization"}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("MOJALOOP_URL")), "/")
	if baseURL == "" {
		return &unavailableMojaloopClient{reason: "MOJALOOP_URL is required for authorized operational settlement"}
	}
	if !strings.HasPrefix(strings.ToLower(baseURL), "https://") {
		return &unavailableMojaloopClient{reason: "MOJALOOP_URL must use HTTPS"}
	}
	transport, err := newAuthorizedMojaloopClient()
	if err != nil {
		return &unavailableMojaloopClient{reason: err.Error()}
	}
	log.Info().Str("url", baseURL).Msg("Mojaloop: connecting to authorized external FSPIOP switch")
	client := &mojaHTTPClient{client: transport, baseURL: baseURL}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil) // #nosec G704 -- baseURL is an authorized administrator-controlled endpoint
	if err != nil {
		return &unavailableMojaloopClient{reason: "construct health request: " + err.Error()}
	}
	resp, err := client.client.Client.Do(req) // #nosec G704 -- baseURL is an authorized administrator-controlled endpoint
	if err != nil {
		return &unavailableMojaloopClient{reason: "external FSPIOP health check failed: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &unavailableMojaloopClient{reason: fmt.Sprintf("external FSPIOP health check returned HTTP %d", resp.StatusCode)}
	}
	log.Info().Str("url", baseURL).Msg("Mojaloop authorized external FSPIOP connection initialized")
	return client
}

func (m *mojaHTTPClient) Close() error { return nil }

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
