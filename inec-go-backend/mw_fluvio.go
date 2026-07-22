package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type FluvioRecord struct {
	Topic     string                 `json:"topic"`
	Key       string                 `json:"key"`
	Value     map[string]interface{} `json:"value"`
	Timestamp time.Time              `json:"timestamp"`
	Offset    int64                  `json:"offset"`
}

// FluvioConsumerGroup describes a consumer-group subscription handled by the
// real Rust SDK bridge and its persisted checkpoint/offset configuration.
type FluvioConsumerGroup struct {
	GroupID    string   `json:"group_id"`
	Topic      string   `json:"topic"`
	Members    []string `json:"members"`
	Partitions int      `json:"partitions"`
}

// FluvioClient is implemented by the Rust fluvio-stream bridge in production.
// An explicit in-memory transport is available only for isolated test and local
// development runtimes; APP_ENV=production always requires the official SDK bridge.
type FluvioClient interface {
	Produce(ctx context.Context, topic string, record FluvioRecord) error
	Consume(ctx context.Context, topic string, offset int64, limit int) ([]FluvioRecord, error)
	ConsumeGroup(ctx context.Context, topic, groupID, memberID string, handler func(FluvioRecord) error) error
	CreateTopic(ctx context.Context, topic string, partitions int) error
	Status() MWStatus
	Close() error
}

// inMemoryFluvioClient preserves deterministic topic ordering for non-production
// tests. It is ephemeral and intentionally does not claim broker durability,
// partitioning, or cross-process consumer-group guarantees.
type inMemoryFluvioClient struct {
	mu           sync.RWMutex
	topics       map[string][]FluvioRecord
	groupOffsets map[string]int64
}

func newInMemoryFluvioClient() *inMemoryFluvioClient {
	return &inMemoryFluvioClient{topics: make(map[string][]FluvioRecord), groupOffsets: make(map[string]int64)}
}

func (f *inMemoryFluvioClient) Produce(ctx context.Context, topic string, record FluvioRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return fmt.Errorf("Fluvio topic is required")
	}
	f.mu.Lock()
	record.Topic = topic
	record.Offset = int64(len(f.topics[topic]))
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}
	f.topics[topic] = append(f.topics[topic], record)
	f.mu.Unlock()
	return nil
}

func (f *inMemoryFluvioClient) Consume(ctx context.Context, topic string, offset int64, limit int) ([]FluvioRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil, fmt.Errorf("Fluvio topic is required")
	}
	if offset < 0 {
		offset = 0
	}
	f.mu.RLock()
	records := f.topics[topic]
	if offset >= int64(len(records)) {
		f.mu.RUnlock()
		return nil, nil
	}
	end := len(records)
	if limit > 0 && int(offset)+limit < end {
		end = int(offset) + limit
	}
	out := append([]FluvioRecord(nil), records[offset:end]...)
	f.mu.RUnlock()
	return out, nil
}

func (f *inMemoryFluvioClient) ConsumeGroup(ctx context.Context, topic, groupID, memberID string, handler func(FluvioRecord) error) error {
	if strings.TrimSpace(groupID) == "" || handler == nil {
		return fmt.Errorf("group ID and handler are required for an in-memory Fluvio consumer")
	}
	groupKey := strings.TrimSpace(groupID) + "\x00" + strings.TrimSpace(topic)
	f.mu.RLock()
	offset := f.groupOffsets[groupKey]
	f.mu.RUnlock()
	records, err := f.Consume(ctx, topic, offset, 0)
	if err != nil {
		return err
	}
	for _, record := range records {
		if err := handler(record); err != nil {
			return err
		}
		f.mu.Lock()
		f.groupOffsets[groupKey] = record.Offset + 1
		f.mu.Unlock()
	}
	return nil
}

func (f *inMemoryFluvioClient) CreateTopic(ctx context.Context, topic string, partitions int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return fmt.Errorf("Fluvio topic is required")
	}
	f.mu.Lock()
	if _, exists := f.topics[topic]; !exists {
		f.topics[topic] = nil
	}
	f.mu.Unlock()
	return nil
}

func (f *inMemoryFluvioClient) Status() MWStatus {
	return MWStatus{Name: "Fluvio", Connected: true, Mode: "in-memory (non-production)", Details: "ephemeral ordered event transport"}
}

func (f *inMemoryFluvioClient) Close() error { return nil }

type fluvioBridgeClient struct {
	baseURL string
	client  *ResilientHTTPClient
}

func (f *fluvioBridgeClient) do(ctx context.Context, method, path string, payload interface{}, target interface{}) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal Fluvio bridge request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, f.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create Fluvio bridge request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read Fluvio bridge response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Fluvio bridge %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if target != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, target); err != nil {
			return fmt.Errorf("decode Fluvio bridge response: %w", err)
		}
	}
	return nil
}

func (f *fluvioBridgeClient) Produce(ctx context.Context, topic string, record FluvioRecord) error {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return fmt.Errorf("Fluvio topic is required")
	}
	return f.do(ctx, http.MethodPost, "/produce", struct {
		Topic string                 `json:"topic"`
		Key   string                 `json:"key,omitempty"`
		Event map[string]interface{} `json:"event"`
	}{Topic: topic, Key: record.Key, Event: record.Value}, nil)
}

type bridgeRecord struct {
	Offset    int64           `json:"offset"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Timestamp json.RawMessage `json:"timestamp"`
}

type bridgeConsumeResponse struct {
	Records []bridgeRecord `json:"records"`
}

func parseBridgeTimestamp(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, text); parseErr == nil {
			return parsed, nil
		}
		return time.Time{}, fmt.Errorf("unsupported Fluvio timestamp %q", text)
	}
	var numeric int64
	if err := json.Unmarshal(raw, &numeric); err != nil {
		return time.Time{}, fmt.Errorf("decode Fluvio timestamp: %w", err)
	}
	if numeric >= 1_000_000_000_000 {
		return time.UnixMilli(numeric).UTC(), nil
	}
	return time.Unix(numeric, 0).UTC(), nil
}

func (f *fluvioBridgeClient) Consume(ctx context.Context, topic string, offset int64, limit int) ([]FluvioRecord, error) {
	if strings.TrimSpace(topic) == "" {
		return nil, fmt.Errorf("Fluvio topic is required")
	}
	if limit <= 0 || limit > 1_000 {
		limit = 100
	}
	query := url.Values{}
	query.Set("topic", topic)
	query.Set("offset", strconv.FormatInt(offset, 10))
	query.Set("limit", strconv.Itoa(limit))
	var response bridgeConsumeResponse
	if err := f.do(ctx, http.MethodGet, "/consume?"+query.Encode(), nil, &response); err != nil {
		return nil, err
	}
	records := make([]FluvioRecord, 0, len(response.Records))
	for _, item := range response.Records {
		var value map[string]interface{}
		if err := json.Unmarshal(item.Value, &value); err != nil {
			return nil, fmt.Errorf("decode Fluvio record at offset %d: %w", item.Offset, err)
		}
		timestamp, err := parseBridgeTimestamp(item.Timestamp)
		if err != nil {
			return nil, err
		}
		records = append(records, FluvioRecord{Topic: topic, Key: item.Key, Value: value, Offset: item.Offset, Timestamp: timestamp})
	}
	return records, nil
}

func (f *fluvioBridgeClient) ConsumeGroup(ctx context.Context, topic, groupID, memberID string, handler func(FluvioRecord) error) error {
	if strings.TrimSpace(topic) == "" || strings.TrimSpace(groupID) == "" || strings.TrimSpace(memberID) == "" {
		return fmt.Errorf("Fluvio topic, group ID, and member ID are required")
	}
	query := url.Values{}
	query.Set("topic", topic)
	query.Set("group", groupID)
	query.Set("member_id", memberID)
	query.Set("limit", "50")
	query.Set("commit", "true")
	var response bridgeConsumeResponse
	if err := f.do(ctx, http.MethodGet, "/consume?"+query.Encode(), nil, &response); err != nil {
		return err
	}
	for _, item := range response.Records {
		var value map[string]interface{}
		if err := json.Unmarshal(item.Value, &value); err != nil {
			return fmt.Errorf("decode Fluvio consumer-group record at offset %d: %w", item.Offset, err)
		}
		timestamp, err := parseBridgeTimestamp(item.Timestamp)
		if err != nil {
			return err
		}
		if err := handler(FluvioRecord{Topic: topic, Key: item.Key, Value: value, Offset: item.Offset, Timestamp: timestamp}); err != nil {
			return err
		}
	}
	return nil
}

func (f *fluvioBridgeClient) CreateTopic(ctx context.Context, topic string, partitions int) error {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return fmt.Errorf("Fluvio topic is required")
	}
	if partitions < 1 {
		partitions = 1
	}
	return f.do(ctx, http.MethodPost, "/topics", map[string]interface{}{"topic": topic, "partitions": partitions}, nil)
}

func (f *fluvioBridgeClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var response struct {
		Status string `json:"status"`
	}
	if err := f.do(ctx, http.MethodGet, "/health", nil, &response); err != nil {
		return MWStatus{Name: "Fluvio", Connected: false, Mode: "official Rust SDK bridge", Details: err.Error()}
	}
	if response.Status != "healthy" {
		return MWStatus{Name: "Fluvio", Connected: false, Mode: "official Rust SDK bridge", Details: "bridge reported " + response.Status}
	}
	return MWStatus{Name: "Fluvio", Connected: true, Mode: "official Rust SDK bridge"}
}

func (f *fluvioBridgeClient) Close() error { return nil }

func initFluvioClient() FluvioClient {
	bridgeURL := strings.TrimRight(strings.TrimSpace(envOrDefault("FLUVIO_STREAM_URL", "")), "/")
	isProduction := strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
	if bridgeURL == "" {
		if !isProduction {
			log.Warn().Msg("Fluvio is not configured — using the explicit in-memory non-production transport")
			return newInMemoryFluvioClient()
		}
		log.Fatal().Msg("Fluvio is required: set FLUVIO_STREAM_URL to the Rust SDK bridge endpoint")
	}
	client := &fluvioBridgeClient{baseURL: bridgeURL, client: NewResilientHTTPClient("fluvio-stream")}
	status := client.Status()
	if !status.Connected {
		if !isProduction {
			log.Warn().Str("url", bridgeURL).Str("details", status.Details).Msg("Fluvio bridge is unavailable — using the explicit in-memory non-production transport")
			return newInMemoryFluvioClient()
		}
		log.Fatal().Str("url", bridgeURL).Str("details", status.Details).Msg("Fluvio Rust SDK bridge is required but unavailable")
	}
	log.Info().Str("url", bridgeURL).Msg("Fluvio official Rust SDK bridge connected")
	return client
}
