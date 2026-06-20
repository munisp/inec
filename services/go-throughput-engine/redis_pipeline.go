package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RedisPipeline batches Redis commands into pipelines for 2M+ ops/sec.
//
// Key optimizations:
// - Pipeline batching (1000 commands per flush = 1000x fewer RTTs)
// - Cluster-aware sharding (hash slots spread load across nodes)
// - Connection pooling (500 connections per node)
// - Automatic pipeline flush on size or timeout threshold
// - Lua scripting for atomic multi-key operations
type RedisPipeline struct {
	cfg    Config
	logger *zap.Logger
	client redis.UniversalClient

	// Pipeline accumulator
	cmdBuffer chan redisCmd
	pool      *WorkerPool
	executed  atomic.Int64
}

type redisCmd struct {
	op    string
	key   string
	value interface{}
	ttl   time.Duration
}

func NewRedisPipeline(cfg Config, logger *zap.Logger) *RedisPipeline {
	var client redis.UniversalClient

	if cfg.RedisCluster {
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        cfg.RedisAddrs,
			PoolSize:     cfg.RedisPoolSize,
			MinIdleConns: cfg.RedisPoolSize / 4,
			ReadTimeout:  2 * time.Millisecond,
			WriteTimeout: 2 * time.Millisecond,
			DialTimeout:  5 * time.Second,
			MaxRetries:   3,
		})
	} else {
		client = redis.NewClient(&redis.Options{
			Addr:         cfg.RedisAddrs[0],
			PoolSize:     cfg.RedisPoolSize,
			MinIdleConns: cfg.RedisPoolSize / 4,
			ReadTimeout:  2 * time.Millisecond,
			WriteTimeout: 2 * time.Millisecond,
			DialTimeout:  5 * time.Second,
			MaxRetries:   3,
		})
	}

	return &RedisPipeline{
		cfg:       cfg,
		logger:    logger,
		client:    client,
		cmdBuffer: make(chan redisCmd, cfg.RedisPipeSize*10),
		pool:      NewWorkerPool("redis-pipe", 8, 32, logger),
	}
}

func (r *RedisPipeline) Start(ctx context.Context) {
	r.pool.Start(ctx)

	// Background pipeline flusher
	go func() {
		batch := make([]redisCmd, 0, r.cfg.RedisPipeSize)
		timer := time.NewTicker(time.Duration(r.cfg.RedisPipeTimeout) * time.Millisecond)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				if len(batch) > 0 {
					r.flushPipeline(context.Background(), batch)
				}
				return
			case cmd, ok := <-r.cmdBuffer:
				if !ok {
					return
				}
				batch = append(batch, cmd)
				if len(batch) >= r.cfg.RedisPipeSize {
					toFlush := batch
					batch = make([]redisCmd, 0, r.cfg.RedisPipeSize)
					r.pool.Submit(func() { r.flushPipeline(ctx, toFlush) })
				}
			case <-timer.C:
				if len(batch) > 0 {
					toFlush := batch
					batch = make([]redisCmd, 0, r.cfg.RedisPipeSize)
					r.pool.Submit(func() { r.flushPipeline(ctx, toFlush) })
				}
			}
		}
	}()
}

func (r *RedisPipeline) flushPipeline(ctx context.Context, cmds []redisCmd) {
	pipe := r.client.Pipeline()

	for _, cmd := range cmds {
		switch cmd.op {
		case "SET":
			data, _ := json.Marshal(cmd.value)
			pipe.Set(ctx, cmd.key, data, cmd.ttl)
		case "INCR":
			pipe.Incr(ctx, cmd.key)
		case "EXPIRE":
			pipe.Expire(ctx, cmd.key, cmd.ttl)
		case "HSET":
			pipe.HSet(ctx, cmd.key, cmd.value)
		case "ZADD":
			if m, ok := cmd.value.(*redis.Z); ok {
				pipe.ZAdd(ctx, cmd.key, *m)
			}
		case "PUBLISH":
			pipe.Publish(ctx, cmd.key, cmd.value)
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		r.logger.Error("redis pipeline flush failed",
			zap.Int("commands", len(cmds)),
			zap.Error(err))
	}
	r.executed.Add(int64(len(cmds)))
}

func (r *RedisPipeline) CacheResult(ctx context.Context, tx Transaction) error {
	data, _ := json.Marshal(tx)

	// Cache the result with 10s TTL for real-time dashboard
	r.cmdBuffer <- redisCmd{
		op:    "SET",
		key:   fmt.Sprintf("tx:%s", tx.ID),
		value: json.RawMessage(data),
		ttl:   10 * time.Second,
	}

	// Increment counters atomically
	r.cmdBuffer <- redisCmd{
		op:  "INCR",
		key: fmt.Sprintf("counter:state:%s:%s", tx.StateCode, tx.Type),
	}
	r.cmdBuffer <- redisCmd{
		op:  "INCR",
		key: fmt.Sprintf("counter:total:%s", tx.Type),
	}

	// Publish for real-time subscribers
	r.cmdBuffer <- redisCmd{
		op:    "PUBLISH",
		key:   fmt.Sprintf("events:%s", tx.Type),
		value: data,
	}

	return nil
}

func (r *RedisPipeline) CacheBatch(ctx context.Context, txs []Transaction) error {
	for i := range txs {
		r.CacheResult(ctx, txs[i])
	}
	return nil
}

// AtomicTallyUpdate uses Lua script for atomic multi-key vote tally updates.
// This ensures consistency even under millions of concurrent updates.
var tallyScript = redis.NewScript(`
local key = KEYS[1]
local party = ARGV[1]
local votes = tonumber(ARGV[2])
redis.call('HINCRBY', key, party, votes)
redis.call('HINCRBY', key, 'total', votes)
redis.call('EXPIRE', key, 3600)
return redis.call('HGET', key, 'total')
`)

func (r *RedisPipeline) AtomicTallyUpdate(ctx context.Context, stateCode, party string, votes int64) (int64, error) {
	key := fmt.Sprintf("tally:%s", stateCode)
	result, err := tallyScript.Run(ctx, r.client, []string{key}, party, votes).Int64()
	if err != nil {
		return 0, err
	}
	return result, nil
}

// Sorted set for real-time leaderboards
func (r *RedisPipeline) UpdateLeaderboard(ctx context.Context, tx Transaction) {
	r.cmdBuffer <- redisCmd{
		op:  "ZADD",
		key: fmt.Sprintf("leaderboard:%s", tx.ElectionID),
		value: &redis.Z{
			Score:  float64(tx.Amount),
			Member: tx.StateCode,
		},
	}
}

func (r *RedisPipeline) PipelineDepth() int {
	return len(r.cmdBuffer)
}

func (r *RedisPipeline) Close() {
	close(r.cmdBuffer)
	r.pool.Close()
	r.client.Close()
}
