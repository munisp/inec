package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type DaprClient interface {
	PublishEvent(ctx context.Context, pubsub, topic string, data interface{}) error
	InvokeService(ctx context.Context, appID, method string, data interface{}) ([]byte, error)
	GetState(ctx context.Context, store, key string) ([]byte, error)
	SaveState(ctx context.Context, store, key string, value interface{}) error
	DeleteState(ctx context.Context, store, key string) error
	Status() MWStatus
	Close() error
}

type daprHTTPClient struct {
	baseURL string
	client  *ResilientHTTPClient
}

func (d *daprHTTPClient) PublishEvent(ctx context.Context, pubsub, topic string, data interface{}) error {
	body, _ := json.Marshal(data)
	url := fmt.Sprintf("%s/v1.0/publish/%s/%s", d.baseURL, pubsub, topic)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (d *daprHTTPClient) InvokeService(ctx context.Context, appID, method string, data interface{}) ([]byte, error) {
	body, _ := json.Marshal(data)
	url := fmt.Sprintf("%s/v1.0/invoke/%s/method/%s", d.baseURL, appID, method)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return buf.Bytes(), nil
}

func (d *daprHTTPClient) GetState(ctx context.Context, store, key string) ([]byte, error) {
	url := fmt.Sprintf("%s/v1.0/state/%s/%s", d.baseURL, store, key)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return buf.Bytes(), nil
}

func (d *daprHTTPClient) SaveState(ctx context.Context, store, key string, value interface{}) error {
	body, _ := json.Marshal([]map[string]interface{}{{"key": key, "value": value}})
	url := fmt.Sprintf("%s/v1.0/state/%s", d.baseURL, store)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (d *daprHTTPClient) DeleteState(ctx context.Context, store, key string) error {
	url := fmt.Sprintf("%s/v1.0/state/%s/%s", d.baseURL, store, key)
	req, _ := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (d *daprHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", d.baseURL+"/v1.0/healthz", nil)
	lat, err := measureLatency(func() error {
		resp, e := d.client.Client.Do(req)
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "Dapr", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Dapr", Connected: true, Mode: "sidecar", Latency: fmtLatency(lat)}
}

func (d *daprHTTPClient) Close() error { return nil }

type embeddedDapr struct {
	mu    sync.RWMutex
	state map[string]map[string]interface{}
	subs  map[string][]func(interface{})
}

func newEmbeddedDapr() *embeddedDapr {
	return &embeddedDapr{
		state: make(map[string]map[string]interface{}),
		subs:  make(map[string][]func(interface{})),
	}
}

func (d *embeddedDapr) PublishEvent(_ context.Context, pubsub, topic string, data interface{}) error {
	d.mu.RLock()
	key := pubsub + "/" + topic
	handlers := d.subs[key]
	d.mu.RUnlock()
	for _, h := range handlers {
		go h(data)
	}
	return nil
}

func (d *embeddedDapr) InvokeService(_ context.Context, appID, method string, data interface{}) ([]byte, error) {
	result, _ := json.Marshal(map[string]interface{}{
		"status": "ok", "app_id": appID, "method": method,
		"message": "handled by embedded Dapr",
	})
	return result, nil
}

func (d *embeddedDapr) GetState(_ context.Context, store, key string) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if s, ok := d.state[store]; ok {
		if v, ok := s[key]; ok {
			data, _ := json.Marshal(v)
			return data, nil
		}
	}
	return nil, fmt.Errorf("key not found")
}

func (d *embeddedDapr) SaveState(_ context.Context, store, key string, value interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state[store] == nil {
		d.state[store] = make(map[string]interface{})
	}
	d.state[store][key] = value
	return nil
}

func (d *embeddedDapr) DeleteState(_ context.Context, store, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s, ok := d.state[store]; ok {
		delete(s, key)
	}
	return nil
}

func (d *embeddedDapr) Status() MWStatus {
	d.mu.RLock()
	storeCount := len(d.state)
	d.mu.RUnlock()
	return MWStatus{
		Name: "Dapr", Connected: true, Mode: "embedded",
		Latency: "0.0ms",
		Details: fmt.Sprintf("local state/pubsub, %d stores", storeCount),
	}
}

func (d *embeddedDapr) Close() error { return nil }

func initDaprClient() DaprClient {
	daprURL := envOrDefault("DAPR_HTTP_URL", "")
	if daprURL == "" {
		daprPort := envOrDefault("DAPR_HTTP_PORT", "")
		if daprPort != "" {
			daprURL = "http://localhost:" + daprPort
		}
	}
	if daprURL != "" {
		client := &daprHTTPClient{
			baseURL: daprURL,
			client:  NewResilientHTTPClient("dapr"),
		}
		s := client.Status()
		if s.Connected {
			log.Info().Str("url", daprURL).Msg("Dapr connected")
			return client
		}
		log.Warn().Msg("Dapr sidecar unreachable, falling back to embedded")
	}
	env := os.Getenv("APP_ENV")
	if env == "production" || env == "staging" {
		log.Fatal().Msg("Dapr sidecar is REQUIRED in production/staging for service mesh. Set DAPR_HTTP_PORT")
	}
	log.Warn().Msg("Dapr using embedded local state/pubsub (DEV ONLY)")
	return newEmbeddedDapr()
}
