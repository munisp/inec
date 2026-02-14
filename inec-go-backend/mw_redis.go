package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Publish(ctx context.Context, channel string, message interface{}) error
	Subscribe(ctx context.Context, channel string, handler func(string)) error
	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Ping() MWStatus
	Close() error
}

type redisHTTPClient struct {
	baseURL string
	client  *http.Client
}

func (r *redisHTTPClient) doCmd(ctx context.Context, cmd string, args ...string) (string, error) {
	payload := map[string]interface{}{"command": cmd, "args": args}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", r.baseURL+"/exec", jsonReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Value string `json:"value"`
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error != "" {
		return "", fmt.Errorf("%s", result.Error)
	}
	return result.Value, nil
}

func (r *redisHTTPClient) Get(ctx context.Context, key string) (string, error) {
	return r.doCmd(ctx, "GET", key)
}

func (r *redisHTTPClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	v := fmt.Sprintf("%v", value)
	if ttl > 0 {
		_, err := r.doCmd(ctx, "SETEX", key, fmt.Sprintf("%d", int(ttl.Seconds())), v)
		return err
	}
	_, err := r.doCmd(ctx, "SET", key, v)
	return err
}

func (r *redisHTTPClient) Del(ctx context.Context, keys ...string) error {
	_, err := r.doCmd(ctx, "DEL", keys...)
	return err
}

func (r *redisHTTPClient) Publish(ctx context.Context, channel string, message interface{}) error {
	v, _ := json.Marshal(message)
	_, err := r.doCmd(ctx, "PUBLISH", channel, string(v))
	return err
}

func (r *redisHTTPClient) Subscribe(ctx context.Context, channel string, handler func(string)) error {
	return nil
}

func (r *redisHTTPClient) Incr(ctx context.Context, key string) (int64, error) {
	v, err := r.doCmd(ctx, "INCR", key)
	if err != nil {
		return 0, err
	}
	var n int64
	fmt.Sscanf(v, "%d", &n)
	return n, nil
}

func (r *redisHTTPClient) Expire(ctx context.Context, key string, ttl time.Duration) error {
	_, err := r.doCmd(ctx, "EXPIRE", key, fmt.Sprintf("%d", int(ttl.Seconds())))
	return err
}

func (r *redisHTTPClient) Ping() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lat, err := measureLatency(func() error {
		_, e := r.doCmd(ctx, "PING")
		return e
	})
	if err != nil {
		return MWStatus{Name: "Redis", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Redis", Connected: true, Mode: "external", Latency: fmtLatency(lat)}
}

func (r *redisHTTPClient) Close() error { return nil }

type embeddedRedis struct {
	mu      sync.RWMutex
	store   map[string]redisEntry
	subs    map[string][]func(string)
	subsMu  sync.RWMutex
}

type redisEntry struct {
	value  string
	expiry time.Time
}

func newEmbeddedRedis() *embeddedRedis {
	r := &embeddedRedis{
		store: make(map[string]redisEntry),
		subs:  make(map[string][]func(string)),
	}
	go r.cleanup()
	return r
}

func (r *embeddedRedis) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		for k, v := range r.store {
			if !v.expiry.IsZero() && now.After(v.expiry) {
				delete(r.store, k)
			}
		}
		r.mu.Unlock()
	}
}

func (r *embeddedRedis) Get(_ context.Context, key string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.store[key]
	if !ok {
		return "", fmt.Errorf("key not found")
	}
	if !e.expiry.IsZero() && time.Now().After(e.expiry) {
		return "", fmt.Errorf("key expired")
	}
	return e.value, nil
}

func (r *embeddedRedis) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	r.store[key] = redisEntry{value: fmt.Sprintf("%v", value), expiry: exp}
	return nil
}

func (r *embeddedRedis) Del(_ context.Context, keys ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range keys {
		delete(r.store, k)
	}
	return nil
}

func (r *embeddedRedis) Publish(_ context.Context, channel string, message interface{}) error {
	r.subsMu.RLock()
	handlers := r.subs[channel]
	r.subsMu.RUnlock()
	v, _ := json.Marshal(message)
	for _, h := range handlers {
		go h(string(v))
	}
	return nil
}

func (r *embeddedRedis) Subscribe(_ context.Context, channel string, handler func(string)) error {
	r.subsMu.Lock()
	defer r.subsMu.Unlock()
	r.subs[channel] = append(r.subs[channel], handler)
	return nil
}

func (r *embeddedRedis) Incr(_ context.Context, key string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.store[key]
	var n int64
	fmt.Sscanf(e.value, "%d", &n)
	n++
	r.store[key] = redisEntry{value: fmt.Sprintf("%d", n), expiry: e.expiry}
	return n, nil
}

func (r *embeddedRedis) Expire(_ context.Context, key string, ttl time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.store[key]; ok {
		e.expiry = time.Now().Add(ttl)
		r.store[key] = e
	}
	return nil
}

func (r *embeddedRedis) Ping() MWStatus {
	return MWStatus{Name: "Redis", Connected: true, Mode: "embedded", Latency: "0.0ms", Details: "in-memory store with TTL"}
}

func (r *embeddedRedis) Close() error { return nil }

func initRedisClient() RedisClient {
	redisURL := envOrDefault("REDIS_URL", "")
	if redisURL != "" {
		client := &redisHTTPClient{
			baseURL: redisURL,
			client:  &http.Client{Timeout: 5 * time.Second},
		}
		s := client.Ping()
		if s.Connected {
			log.Println("[Redis] Connected to external Redis at", redisURL)
			return client
		}
		log.Println("[Redis] External Redis unreachable, falling back to embedded")
	}
	log.Println("[Redis] Using embedded in-memory store")
	return newEmbeddedRedis()
}
