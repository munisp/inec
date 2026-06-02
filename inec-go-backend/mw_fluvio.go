package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"net/http"
	"sync"
	"time"
)

type FluvioRecord struct {
	Topic     string                 `json:"topic"`
	Key       string                 `json:"key"`
	Value     map[string]interface{} `json:"value"`
	Timestamp time.Time              `json:"timestamp"`
	Offset    int64                  `json:"offset"`
}

type FluvioClient interface {
	Produce(ctx context.Context, topic string, record FluvioRecord) error
	Consume(ctx context.Context, topic string, offset int64, limit int) ([]FluvioRecord, error)
	CreateTopic(ctx context.Context, topic string, partitions int) error
	Status() MWStatus
	Close() error
}

type fluvioHTTPClient struct {
	baseURL string
	client  *ResilientHTTPClient
}

func (f *fluvioHTTPClient) Produce(ctx context.Context, topic string, record FluvioRecord) error {
	record.Timestamp = time.Now()
	body, _ := json.Marshal(record)
	url := fmt.Sprintf("%s/api/v1/topics/%s/produce", f.baseURL, topic)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (f *fluvioHTTPClient) Consume(ctx context.Context, topic string, offset int64, limit int) ([]FluvioRecord, error) {
	url := fmt.Sprintf("%s/api/v1/topics/%s/consume?offset=%d&limit=%d", f.baseURL, topic, offset, limit)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var records []FluvioRecord
	json.NewDecoder(resp.Body).Decode(&records)
	return records, nil
}

func (f *fluvioHTTPClient) CreateTopic(ctx context.Context, topic string, partitions int) error {
	body, _ := json.Marshal(map[string]interface{}{"name": topic, "partitions": partitions})
	url := fmt.Sprintf("%s/api/v1/topics", f.baseURL)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (f *fluvioHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.baseURL+"/api/v1/topics", nil)
	lat, err := measureLatency(func() error {
		resp, e := f.client.Client.Do(req)
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "Fluvio", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Fluvio", Connected: true, Mode: "external", Latency: fmtLatency(lat)}
}

func (f *fluvioHTTPClient) Close() error { return nil }

type embeddedFluvio struct {
	mu sync.RWMutex
}

func newEmbeddedFluvio() *embeddedFluvio {
	// Create persistent event bus table
	db.Exec(`CREATE TABLE IF NOT EXISTS event_bus (
		id SERIAL PRIMARY KEY,
		topic TEXT NOT NULL,
		event_key TEXT,
		payload TEXT NOT NULL,
		offset_id INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_event_bus_topic ON event_bus(topic, offset_id)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS event_bus_topics (
		topic TEXT PRIMARY KEY,
		partitions INTEGER DEFAULT 1,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	return &embeddedFluvio{}
}

func (f *embeddedFluvio) Produce(_ context.Context, topic string, record FluvioRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	record.Timestamp = time.Now()

	// Get next offset for this topic
	var maxOffset int64
	db.QueryRow("SELECT COALESCE(MAX(offset_id), -1) FROM event_bus WHERE topic=?", topic).Scan(&maxOffset)
	record.Offset = maxOffset + 1

	payload, _ := json.Marshal(record.Value)
	dbExecLog("event_bus", "INSERT INTO event_bus (topic, event_key, payload, offset_id, created_at) VALUES (?,?,?,?,?)",
		topic, record.Key, string(payload), record.Offset, record.Timestamp)
	return nil
}

func (f *embeddedFluvio) Consume(_ context.Context, topic string, offset int64, limit int) ([]FluvioRecord, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	rows, err := db.Query("SELECT event_key, payload, offset_id, created_at FROM event_bus WHERE topic=? AND offset_id >= ? ORDER BY offset_id LIMIT ?",
		topic, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []FluvioRecord{}
	for rows.Next() {
		var r FluvioRecord
		var payload string
		var ts time.Time
		if err := rows.Scan(&r.Key, &payload, &r.Offset, &ts); err != nil {
			continue
		}
		json.Unmarshal([]byte(payload), &r.Value)
		r.Timestamp = ts
		records = append(records, r)
	}
	return records, nil
}

func (f *embeddedFluvio) CreateTopic(_ context.Context, topic string, partitions int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if partitions < 1 {
		partitions = 1
	}
	dbExecLog("event_bus", "INSERT INTO event_bus_topics (topic, partitions) VALUES (?,?) ON CONFLICT(topic) DO NOTHING", topic, partitions)
	return nil
}

func (f *embeddedFluvio) Status() MWStatus {
	var topicCount, recordCount int
	db.QueryRow("SELECT COUNT(*) FROM event_bus_topics").Scan(&topicCount)
	db.QueryRow("SELECT COUNT(*) FROM event_bus").Scan(&recordCount)
	return MWStatus{
		Name: "Fluvio", Connected: true, Mode: "embedded_persistent",
		Latency: "0.1ms",
		Details: fmt.Sprintf("PostgreSQL-backed event bus, %d topics, %d records", topicCount, recordCount),
	}
}

func (f *embeddedFluvio) Close() error { return nil }

func initFluvioClient() FluvioClient {
	fluvioURL := envOrDefault("FLUVIO_URL", "")
	if fluvioURL != "" {
		client := &fluvioHTTPClient{
			baseURL: fluvioURL,
			client:  NewResilientHTTPClient("fluvio"),
		}
		s := client.Status()
		if s.Connected {
			log.Info().Str("url", fluvioURL).Msg("Fluvio connected")
			return client
		}
		log.Warn().Msg("Fluvio unreachable, falling back to embedded")
	}
	log.Info().Msg("Fluvio using embedded in-memory streaming")
	return newEmbeddedFluvio()
}
