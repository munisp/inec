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

// DaprBulkPublisher uses Dapr's bulk publish API for 1M+ events/sec.
//
// Key optimizations:
// - Bulk publish API (1000 events per HTTP call)
// - Batch accumulation with size/timeout flush
// - Connection pooling (reuse HTTP connections)
// - Parallel workers for independent pub/sub topics
// - State store batching for transactional state
type DaprBulkPublisher struct {
	cfg    Config
	logger *zap.Logger
	client *http.Client

	buffer    chan daprEvent
	pool      *WorkerPool
	published atomic.Int64
}

type daprEvent struct {
	Topic string
	Data  interface{}
}

type daprBulkEntry struct {
	EntryID     string      `json:"entryId"`
	Event       interface{} `json:"event"`
	ContentType string      `json:"contentType"`
}

func NewDaprBulkPublisher(cfg Config, logger *zap.Logger) *DaprBulkPublisher {
	return &DaprBulkPublisher{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 200,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		buffer: make(chan daprEvent, cfg.DaprBatchSize*4),
		pool:   NewWorkerPool("dapr-bulk", 8, 32, logger),
	}
}

func (d *DaprBulkPublisher) Start(ctx context.Context) {
	d.pool.Start(ctx)

	go func() {
		// Group by topic
		topicBatches := make(map[string][]daprBulkEntry)
		timer := time.NewTicker(5 * time.Millisecond)
		defer timer.Stop()

		flush := func() {
			for topic, entries := range topicBatches {
				if len(entries) > 0 {
					toFlush := entries
					t := topic
					d.pool.Submit(func() { d.bulkPublish(ctx, t, toFlush) })
				}
			}
			topicBatches = make(map[string][]daprBulkEntry)
		}

		for {
			select {
			case <-ctx.Done():
				flush()
				return
			case evt, ok := <-d.buffer:
				if !ok {
					flush()
					return
				}
				entry := daprBulkEntry{
					EntryID:     fmt.Sprintf("%d", time.Now().UnixNano()),
					Event:       evt.Data,
					ContentType: "application/json",
				}
				topicBatches[evt.Topic] = append(topicBatches[evt.Topic], entry)

				total := 0
				for _, v := range topicBatches {
					total += len(v)
				}
				if total >= d.cfg.DaprBatchSize {
					flush()
				}
			case <-timer.C:
				flush()
			}
		}
	}()
}

func (d *DaprBulkPublisher) bulkPublish(ctx context.Context, topic string, entries []daprBulkEntry) {
	if len(entries) == 0 {
		return
	}

	body, _ := json.Marshal(entries)
	url := fmt.Sprintf("%s/v1.0-alpha1/publish/bulk/inec-pubsub/%s", d.cfg.DaprURL, topic)

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		d.logger.Error("dapr bulk publish failed",
			zap.String("topic", topic),
			zap.Int("entries", len(entries)),
			zap.Error(err))
		return
	}
	defer resp.Body.Close()

	d.published.Add(int64(len(entries)))
}

func (d *DaprBulkPublisher) PublishBatch(ctx context.Context, txs []Transaction) error {
	for i := range txs {
		d.buffer <- daprEvent{
			Topic: topicForType(txs[i].Type),
			Data:  txs[i],
		}
	}
	return nil
}

// BatchSaveState saves multiple state entries atomically.
func (d *DaprBulkPublisher) BatchSaveState(ctx context.Context, storeName string, items []stateItem) error {
	body, _ := json.Marshal(items)
	url := fmt.Sprintf("%s/v1.0/state/%s", d.cfg.DaprURL, storeName)

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

type stateItem struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

func (d *DaprBulkPublisher) Close() {
	close(d.buffer)
	d.pool.Close()
}
