package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// unavailableRedisClient never stores, synthesizes, or substitutes Redis data.
// It preserves the dependency error for callers until the configured native service recovers.
type unavailableRedisClient struct {
	reason string
}

func (r *unavailableRedisClient) unavailable() error {
	return fmt.Errorf("native Redis is unavailable: %s", r.reason)
}
func (r *unavailableRedisClient) Get(context.Context, string) (string, error) {
	return "", r.unavailable()
}
func (r *unavailableRedisClient) Set(context.Context, string, interface{}, time.Duration) error {
	return r.unavailable()
}
func (r *unavailableRedisClient) Del(context.Context, ...string) error { return r.unavailable() }
func (r *unavailableRedisClient) Publish(context.Context, string, interface{}) error {
	return r.unavailable()
}
func (r *unavailableRedisClient) Subscribe(context.Context, string, func(string)) error {
	return r.unavailable()
}
func (r *unavailableRedisClient) Incr(context.Context, string) (int64, error) {
	return 0, r.unavailable()
}
func (r *unavailableRedisClient) Expire(context.Context, string, time.Duration) error {
	return r.unavailable()
}
func (r *unavailableRedisClient) Ping() MWStatus {
	return MWStatus{Name: "Redis", Connected: false, Mode: "native Redis required", Details: r.reason}
}
func (r *unavailableRedisClient) Close() error { return nil }

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

func newRealRedisClientFromURL(rawURL, password string) (*realRedisClient, error) {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	if password != "" {
		opts.Password = password
	}
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second
	opts.PoolSize = 20
	opts.MinIdleConns = 5
	return &realRedisClient{client: redis.NewClient(opts), addr: opts.Addr}, nil
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

// --- Redis Cluster client using go-redis ClusterClient ---

type realRedisClusterClient struct {
	client *redis.ClusterClient
	addrs  []string
}

func newRealRedisClusterClient(addrs []string, password string) *realRedisClusterClient {
	rdb := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:          addrs,
		Password:       password,
		DialTimeout:    5 * time.Second,
		ReadTimeout:    3 * time.Second,
		WriteTimeout:   3 * time.Second,
		PoolSize:       20,
		MinIdleConns:   5,
		ReadOnly:       true,
		RouteByLatency: true,
	})
	return &realRedisClusterClient{client: rdb, addrs: addrs}
}

func (r *realRedisClusterClient) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

func (r *realRedisClusterClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *realRedisClusterClient) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

func (r *realRedisClusterClient) Publish(ctx context.Context, channel string, message interface{}) error {
	v, _ := json.Marshal(message)
	return r.client.Publish(ctx, channel, string(v)).Err()
}

func (r *realRedisClusterClient) Subscribe(ctx context.Context, channel string, handler func(string)) error {
	sub := r.client.Subscribe(ctx, channel)
	go func() {
		ch := sub.Channel()
		for msg := range ch {
			handler(msg.Payload)
		}
	}()
	return nil
}

func (r *realRedisClusterClient) Incr(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

func (r *realRedisClusterClient) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.client.Expire(ctx, key, ttl).Err()
}

func (r *realRedisClusterClient) Ping() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lat, err := measureLatency(func() error {
		return r.client.ForEachShard(ctx, func(ctx context.Context, shard *redis.Client) error {
			return shard.Ping(ctx).Err()
		})
	})
	if err != nil {
		return MWStatus{Name: "Redis", Connected: false, Mode: "cluster (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Redis", Connected: true, Mode: "cluster", Latency: fmtLatency(lat),
		Details: fmt.Sprintf("cluster with %d nodes, read replicas, route-by-latency", len(r.addrs))}
}

func (r *realRedisClusterClient) Close() error {
	return r.client.Close()
}

// --- Init ---

func initRedisClient() RedisClient {
	password := envOrDefault("REDIS_PASSWORD", "")
	clusterAddrs := envOrDefault("REDIS_CLUSTER_ADDRS", "")
	if clusterAddrs != "" {
		addrs := splitAndTrim(clusterAddrs, ",")
		if len(addrs) == 0 {
			return &unavailableRedisClient{reason: "REDIS_CLUSTER_ADDRS contained no valid addresses"}
		}
		client := newRealRedisClusterClient(addrs, password)
		if status := client.Ping(); status.Connected {
			log.Info().Strs("addrs", addrs).Msg("Redis connected via native go-redis cluster client")
			return client
		} else {
			_ = client.Close()
			return &unavailableRedisClient{reason: status.Details}
		}
	}

	if redisURL := envOrDefault("REDIS_URL", ""); redisURL != "" {
		client, err := newRealRedisClientFromURL(redisURL, password)
		if err != nil {
			return &unavailableRedisClient{reason: err.Error()}
		}
		if status := client.Ping(); status.Connected {
			log.Info().Str("addr", client.addr).Msg("Redis connected via native go-redis URL client")
			return client
		} else {
			_ = client.Close()
			return &unavailableRedisClient{reason: status.Details}
		}
	}

	if redisAddr := envOrDefault("REDIS_ADDR", ""); redisAddr != "" {
		client := newRealRedisClient(redisAddr, password, 0)
		if status := client.Ping(); status.Connected {
			log.Info().Str("addr", redisAddr).Msg("Redis connected via native go-redis client")
			return client
		} else {
			_ = client.Close()
			return &unavailableRedisClient{reason: status.Details}
		}
	}
	return &unavailableRedisClient{reason: "REDIS_URL, REDIS_ADDR, or REDIS_CLUSTER_ADDRS must be configured"}
}

func splitAndTrim(s, sep string) []string {
	parts := make([]string, 0)
	for _, p := range strings.Split(s, sep) {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}
