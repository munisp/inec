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
	client  *http.Client
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
		resp, e := f.client.Do(req)
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
	mu     sync.RWMutex
	topics map[string][]FluvioRecord
}

func newEmbeddedFluvio() *embeddedFluvio {
	return &embeddedFluvio{
		topics: make(map[string][]FluvioRecord),
	}
}

func (f *embeddedFluvio) Produce(_ context.Context, topic string, record FluvioRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	record.Timestamp = time.Now()
	record.Offset = int64(len(f.topics[topic]))
	f.topics[topic] = append(f.topics[topic], record)
	if len(f.topics[topic]) > 50000 {
		f.topics[topic] = f.topics[topic][len(f.topics[topic])-25000:]
	}
	return nil
}

func (f *embeddedFluvio) Consume(_ context.Context, topic string, offset int64, limit int) ([]FluvioRecord, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	records := f.topics[topic]
	if int(offset) >= len(records) {
		return nil, nil
	}
	end := int(offset) + limit
	if end > len(records) {
		end = len(records)
	}
	return records[offset:end], nil
}

func (f *embeddedFluvio) CreateTopic(_ context.Context, topic string, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.topics[topic]; !ok {
		f.topics[topic] = make([]FluvioRecord, 0)
	}
	return nil
}

func (f *embeddedFluvio) Status() MWStatus {
	f.mu.RLock()
	topicCount := len(f.topics)
	var recordCount int
	for _, records := range f.topics {
		recordCount += len(records)
	}
	f.mu.RUnlock()
	return MWStatus{
		Name: "Fluvio", Connected: true, Mode: "embedded",
		Latency: "0.0ms",
		Details: fmt.Sprintf("in-memory streaming, %d topics, %d records", topicCount, recordCount),
	}
}

func (f *embeddedFluvio) Close() error { return nil }

func initFluvioClient() FluvioClient {
	fluvioURL := envOrDefault("FLUVIO_URL", "")
	if fluvioURL != "" {
		client := &fluvioHTTPClient{
			baseURL: fluvioURL,
			client:  &http.Client{Timeout: 5 * time.Second},
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
