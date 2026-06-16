// Package gotv — Webhook/callback system for GOTV events.
// HMAC-signed POST delivery with retry queue and exponential backoff.
package gotv

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// WebhookEvent types
const (
	EventCampaignCompleted = "campaign.completed"
	EventRideMatched       = "ride.matched"
	EventPledgeFulfilled   = "pledge.fulfilled"
	EventContactOptedOut   = "contact.opted_out"
	EventVolunteerCheckin  = "volunteer.checkin"
	EventTurnoutUpdate     = "turnout.update"
)

// WebhookPayload is the envelope sent to webhook endpoints.
type WebhookPayload struct {
	ID        string      `json:"id"`
	Event     string      `json:"event"`
	PartyID   int         `json:"party_id"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// WebhookManager handles webhook registration and event delivery.
type WebhookManager struct {
	db      *sql.DB
	client  *http.Client
	retryQ  chan retryItem
	wg      sync.WaitGroup
}

type retryItem struct {
	url     string
	secret  string
	payload []byte
	attempt int
}

// NewWebhookManager creates a webhook manager.
func NewWebhookManager(db *sql.DB) *WebhookManager {
	wm := &WebhookManager{
		db:     db,
		client: &http.Client{Timeout: 10 * time.Second},
		retryQ: make(chan retryItem, 1000),
	}
	return wm
}

// InitTables creates webhook schema.
func (wm *WebhookManager) InitTables(ctx context.Context) error {
	_, err := wm.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS gotv_webhooks (
			id SERIAL PRIMARY KEY,
			party_id INTEGER NOT NULL REFERENCES parties(id),
			url TEXT NOT NULL,
			secret TEXT NOT NULL,
			event_types TEXT[] NOT NULL,
			is_active BOOLEAN DEFAULT TRUE,
			failure_count INTEGER DEFAULT 0,
			last_success_at TIMESTAMP,
			last_failure_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(party_id, url)
		);
		CREATE INDEX IF NOT EXISTS idx_gotv_webhooks_party ON gotv_webhooks(party_id);

		CREATE TABLE IF NOT EXISTS gotv_webhook_deliveries (
			id SERIAL PRIMARY KEY,
			webhook_id INTEGER REFERENCES gotv_webhooks(id),
			event TEXT NOT NULL,
			payload JSONB NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			response_code INTEGER,
			response_body TEXT,
			attempts INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			delivered_at TIMESTAMP
		);
	`)
	return err
}

// StartRetryWorker processes failed webhook deliveries.
func (wm *WebhookManager) StartRetryWorker(ctx context.Context) {
	wm.wg.Add(1)
	go func() {
		defer wm.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case item := <-wm.retryQ:
				backoff := time.Duration(1<<uint(item.attempt)) * time.Second
				if backoff > 5*time.Minute {
					backoff = 5 * time.Minute
				}
				time.Sleep(backoff)
				wm.deliver(item.url, item.secret, item.payload, item.attempt)
			}
		}
	}()
}

// Emit sends an event to all matching webhooks for a party.
func (wm *WebhookManager) Emit(partyID int, event string, data interface{}) {
	payload := WebhookPayload{
		ID:        fmt.Sprintf("whk-%d-%d", partyID, time.Now().UnixNano()),
		Event:     event,
		PartyID:   partyID,
		Timestamp: time.Now(),
		Data:      data,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("webhook: marshal failed")
		return
	}

	rows, err := wm.db.Query(
		`SELECT url, secret FROM gotv_webhooks
		 WHERE party_id=$1 AND is_active=TRUE AND $2 = ANY(event_types)
		 AND failure_count < 10`,
		partyID, event,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var url, secret string
		if err := rows.Scan(&url, &secret); err != nil {
			continue
		}
		go wm.deliver(url, secret, body, 0)
	}
}

func (wm *WebhookManager) deliver(url, secret string, body []byte, attempt int) {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		log.Error().Err(err).Str("url", url).Msg("webhook: request creation failed")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GOTV-Signature", signature)
	req.Header.Set("X-GOTV-Event", string(body))

	resp, err := wm.client.Do(req)
	if err != nil {
		if attempt < 5 {
			select {
			case wm.retryQ <- retryItem{url: url, secret: secret, payload: body, attempt: attempt + 1}:
			default:
				log.Warn().Str("url", url).Msg("webhook: retry queue full")
			}
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		wm.db.Exec("UPDATE gotv_webhooks SET last_success_at=NOW(), failure_count=0 WHERE url=$1", url)
	} else if attempt < 5 {
		wm.db.Exec("UPDATE gotv_webhooks SET last_failure_at=NOW(), failure_count=failure_count+1 WHERE url=$1", url)
		select {
		case wm.retryQ <- retryItem{url: url, secret: secret, payload: body, attempt: attempt + 1}:
		default:
		}
	} else {
		wm.db.Exec("UPDATE gotv_webhooks SET is_active=FALSE WHERE url=$1 AND failure_count >= 10", url)
	}
}

// Stop waits for pending webhook deliveries to drain.
func (wm *WebhookManager) Stop() {
	close(wm.retryQ)
	wm.wg.Wait()
}
