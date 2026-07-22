package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// FluvioClient is implemented exclusively by the Rust fluvio-stream bridge.
// That service embeds the official Fluvio Rust SDK; no in-memory, PostgreSQL,
// controller-TCP surrogate, or unsupported REST adapter is permitted.
type FluvioClient interface {
	Produce(ctx context.Context, topic string, record FluvioRecord) error
	Consume(ctx context.Context, topic string, offset int64, limit int) ([]FluvioRecord, error)
	ConsumeGroup(ctx context.Context, topic, groupID, memberID string, handler func(FluvioRecord) error) error
	CreateTopic(ctx context.Context, topic string, partitions int) error
	Status() MWStatus
	Close() error
}

type fluvioBridgeClient struct {
	baseURL string
	client  *ResilientHTTPClient
}

// unavailableFluvioClient never stores, synthesizes, or substitutes Fluvio delivery.
// It preserves the dependency error for callers until the configured bridge recovers.
type unavailableFluvioClient struct {
	reason string
}

func (f *unavailableFluvioClient) unavailable() error {
	return fmt.Errorf("Fluvio bridge is unavailable: %s", f.reason)
}
func (f *unavailableFluvioClient) Produce(context.Context, string, FluvioRecord) error {
	return f.unavailable()
}
func (f *unavailableFluvioClient) Consume(context.Context, string, int64, int) ([]FluvioRecord, error) {
	return nil, f.unavailable()
}
func (f *unavailableFluvioClient) ConsumeGroup(context.Context, string, string, string, func(FluvioRecord) error) error {
	return f.unavailable()
}
func (f *unavailableFluvioClient) CreateTopic(context.Context, string, int) error {
	return f.unavailable()
}
func (f *unavailableFluvioClient) Status() MWStatus {
	return MWStatus{Name: "Fluvio", Connected: false, Mode: "Rust SDK bridge required", Details: f.reason}
}
func (f *unavailableFluvioClient) Close() error { return nil }

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
	if bridgeURL == "" {
		log.Warn().Msg("Fluvio unavailable: FLUVIO_STREAM_URL not set")
		return &unavailableFluvioClient{reason: "FLUVIO_STREAM_URL must be configured"}
	}
	client := &fluvioBridgeClient{baseURL: bridgeURL, client: NewResilientHTTPClient("fluvio-stream")}
	status := client.Status()
	if !status.Connected {
		log.Warn().Str("url", bridgeURL).Str("details", status.Details).Msg("Fluvio bridge unavailable")
		return &unavailableFluvioClient{reason: status.Details}
	}
	log.Info().Str("url", bridgeURL).Msg("Fluvio official Rust SDK bridge connected")
	return client
}
