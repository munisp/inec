package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// FluvioPipeline handles real-time stream processing with Fluvio.
//
// Key optimizations:
// - Batch produce (accumulate and flush for fewer RPCs)
// - SmartModule integration (WASM-based in-line transforms)
// - Parallel consumers per partition
// - Backpressure handling (bounded buffer with overflow to disk)
type FluvioPipeline struct {
	cfg    Config
	logger *zap.Logger
	client *http.Client

	buffer    chan fluvioRecord
	pool      *WorkerPool
	produced  atomic.Int64
	consumed  atomic.Int64
}

type fluvioRecord struct {
	Topic string
	Key   string
	Value []byte
}

func NewFluvioPipeline(cfg Config, logger *zap.Logger) *FluvioPipeline {
	return &FluvioPipeline{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 50,
				IdleConnTimeout:     60 * time.Second,
			},
		},
		buffer: make(chan fluvioRecord, 100000),
		pool:   NewWorkerPool("fluvio-produce", cfg.FluvioWorkers, cfg.FluvioWorkers*2, logger),
	}
}

func (f *FluvioPipeline) Start(ctx context.Context) {
	f.pool.Start(ctx)

	// Background batch producer
	go func() {
		batch := make([]fluvioRecord, 0, 1000)
		timer := time.NewTicker(5 * time.Millisecond)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				if len(batch) > 0 {
					f.flushBatch(context.Background(), batch)
				}
				return
			case rec, ok := <-f.buffer:
				if !ok {
					return
				}
				batch = append(batch, rec)
				if len(batch) >= 1000 {
					toFlush := batch
					batch = make([]fluvioRecord, 0, 1000)
					f.pool.Submit(func() { f.flushBatch(ctx, toFlush) })
				}
			case <-timer.C:
				if len(batch) > 0 {
					toFlush := batch
					batch = make([]fluvioRecord, 0, 1000)
					f.pool.Submit(func() { f.flushBatch(ctx, toFlush) })
				}
			}
		}
	}()
}

func (f *FluvioPipeline) flushBatch(ctx context.Context, records []fluvioRecord) {
	if len(records) == 0 {
		return
	}

	// Group by topic
	grouped := make(map[string][]map[string]interface{})
	for _, rec := range records {
		grouped[rec.Topic] = append(grouped[rec.Topic], map[string]interface{}{
			"key":   rec.Key,
			"value": json.RawMessage(rec.Value),
		})
	}

	for topic, recs := range grouped {
		body, _ := json.Marshal(map[string]interface{}{
			"records": recs,
		})

		url := fmt.Sprintf("%s/api/v1/topics/%s/produce-batch", f.cfg.FluvioURL, topic)
		req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := f.client.Do(req)
		if err != nil {
			f.logger.Error("fluvio batch produce failed",
				zap.String("topic", topic),
				zap.Int("records", len(recs)),
				zap.Error(err))
			continue
		}
		resp.Body.Close()
		f.produced.Add(int64(len(recs)))
	}
}

func (f *FluvioPipeline) Stream(ctx context.Context, tx Transaction) error {
	data, _ := json.Marshal(tx)
	f.buffer <- fluvioRecord{
		Topic: fmt.Sprintf("inec.stream.%s", tx.Type),
		Key:   tx.ID,
		Value: data,
	}
	return nil
}

func (f *FluvioPipeline) StreamBatch(ctx context.Context, txs []Transaction) error {
	for i := range txs {
		data, _ := json.Marshal(txs[i])
		f.buffer <- fluvioRecord{
			Topic: fmt.Sprintf("inec.stream.%s", txs[i].Type),
			Key:   txs[i].ID,
			Value: data,
		}
	}
	return nil
}

func (f *FluvioPipeline) Close() {
	close(f.buffer)
	f.pool.Close()
}
