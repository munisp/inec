package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// RedisClient defines the interface for Redis operations.
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

// --- Real Redis client using go-redis ---

type realRedisClient struct {
	client *redis.Client
	addr   string
}

func newRealRedisClient(addr, password string, db int) *realRedisClient {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
		MinIdleConns: 5,
	})
	return &realRedisClient{client: rdb, addr: addr}
}

func (r *realRedisClient) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

func (r *realRedisClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *realRedisClient) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

func (r *realRedisClient) Publish(ctx context.Context, channel string, message interface{}) error {
	v, _ := json.Marshal(message)
	return r.client.Publish(ctx, channel, string(v)).Err()
}

func (r *realRedisClient) Subscribe(ctx context.Context, channel string, handler func(string)) error {
	sub := r.client.Subscribe(ctx, channel)
	go func() {
		ch := sub.Channel()
		for msg := range ch {
			handler(msg.Payload)
		}
	}()
	return nil
}

func (r *realRedisClient) Incr(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

func (r *realRedisClient) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.client.Expire(ctx, key, ttl).Err()
}

func (r *realRedisClient) Ping() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lat, err := measureLatency(func() error {
		return r.client.Ping(ctx).Err()
	})
	if err != nil {
		return MWStatus{Name: "Redis", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Redis", Connected: true, Mode: "native go-redis", Latency: fmtLatency(lat), Details: "connection pool, pub/sub, TTL"}
}

func (r *realRedisClient) Close() error {
	return r.client.Close()
}

// --- Embedded fallback (in-memory) ---

type embeddedRedis struct {
	mu     sync.RWMutex
	store  map[string]redisEntry
	subs   map[string][]func(string)
	subsMu sync.RWMutex
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

// --- Init ---

func initRedisClient() RedisClient {
	redisAddr := envOrDefault("REDIS_ADDR", "")
	if redisAddr == "" {
		// Try legacy REDIS_URL
		redisURL := envOrDefault("REDIS_URL", "")
		if redisURL != "" {
			// Parse redis://host:port or just host:port
			redisAddr = redisURL
			if len(redisAddr) > 7 && redisAddr[:7] == "redis://" {
				redisAddr = redisAddr[8:]
			}
			if len(redisAddr) > 8 && redisAddr[:8] == "http://" {
				redisAddr = redisAddr[7:]
			}
		}
	}

	if redisAddr != "" {
		password := envOrDefault("REDIS_PASSWORD", "")
		client := newRealRedisClient(redisAddr, password, 0)
		s := client.Ping()
		if s.Connected {
			log.Info().Str("addr", redisAddr).Msg("Redis connected via go-redis")
			return client
		}
		log.Warn().Str("addr", redisAddr).Msg("Redis unreachable, falling back to embedded")
		client.Close()
	}
	log.Info().Msg("Redis using embedded in-memory store")
	return newEmbeddedRedis()
}
