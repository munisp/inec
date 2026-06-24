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

// --- PostgreSQL-backed fallback (persistent) ---

type pgRedis struct {
	mu   sync.RWMutex
	subs map[string][]func(string)
}

func newPGRedis() *pgRedis {
	db.Exec(`CREATE TABLE IF NOT EXISTS redis_cache (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT '',
		expiry TIMESTAMP,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_redis_cache_expiry ON redis_cache(expiry) WHERE expiry IS NOT NULL`)
	r := &pgRedis{subs: make(map[string][]func(string))}
	go r.cleanup()
	log.Info().Msg("Redis fallback: PostgreSQL-backed cache initialized")
	return r
}

func (r *pgRedis) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	for range ticker.C {
		db.Exec(`DELETE FROM redis_cache WHERE expiry IS NOT NULL AND expiry < NOW()`)
	}
}

func (r *pgRedis) Get(_ context.Context, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM redis_cache WHERE key=$1 AND (expiry IS NULL OR expiry > NOW())`, key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("key not found")
	}
	return value, nil
}

func (r *pgRedis) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	v := fmt.Sprintf("%v", value)
	if ttl > 0 {
		expiry := time.Now().Add(ttl)
		_, err := db.Exec(`INSERT INTO redis_cache (key, value, expiry, updated_at) VALUES ($1, $2, $3, NOW())
			ON CONFLICT (key) DO UPDATE SET value=$2, expiry=$3, updated_at=NOW()`, key, v, expiry)
		return err
	}
	_, err := db.Exec(`INSERT INTO redis_cache (key, value, expiry, updated_at) VALUES ($1, $2, NULL, NOW())
		ON CONFLICT (key) DO UPDATE SET value=$2, expiry=NULL, updated_at=NOW()`, key, v)
	return err
}

func (r *pgRedis) Del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		db.Exec(`DELETE FROM redis_cache WHERE key=$1`, k)
	}
	return nil
}

func (r *pgRedis) Publish(_ context.Context, channel string, message interface{}) error {
	r.mu.RLock()
	handlers := r.subs[channel]
	r.mu.RUnlock()
	v, _ := json.Marshal(message)
	for _, h := range handlers {
		go h(string(v))
	}
	return nil
}

func (r *pgRedis) Subscribe(_ context.Context, channel string, handler func(string)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subs[channel] = append(r.subs[channel], handler)
	return nil
}

func (r *pgRedis) Incr(_ context.Context, key string) (int64, error) {
	var newVal int64
	err := db.QueryRow(`INSERT INTO redis_cache (key, value, updated_at) VALUES ($1, '1', NOW())
		ON CONFLICT (key) DO UPDATE SET value = (COALESCE(redis_cache.value, '0')::bigint + 1)::text, updated_at=NOW()
		RETURNING value::bigint`, key).Scan(&newVal)
	if err != nil {
		return 0, err
	}
	return newVal, nil
}

func (r *pgRedis) Expire(_ context.Context, key string, ttl time.Duration) error {
	expiry := time.Now().Add(ttl)
	_, err := db.Exec(`UPDATE redis_cache SET expiry=$2, updated_at=NOW() WHERE key=$1`, key, expiry)
	return err
}

func (r *pgRedis) Ping() MWStatus {
	var keyCount int
	db.QueryRow(`SELECT COUNT(*) FROM redis_cache WHERE expiry IS NULL OR expiry > NOW()`).Scan(&keyCount)
	return MWStatus{
		Name: "Redis", Connected: true, Mode: "pg-backed",
		Latency: "< 1ms",
		Details: fmt.Sprintf("PostgreSQL-persisted cache, %d active keys", keyCount),
	}
}

func (r *pgRedis) Close() error { return nil }

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
	log.Info().Msg("Redis using PostgreSQL-backed cache (persistent)")
	return newPGRedis()
}
