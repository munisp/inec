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

type KafkaMessage struct {
	Topic     string                 `json:"topic"`
	Key       string                 `json:"key"`
	Value     map[string]interface{} `json:"value"`
	Timestamp time.Time              `json:"timestamp"`
}

type KafkaClient interface {
	Produce(ctx context.Context, msg KafkaMessage) error
	Subscribe(topic string, handler func(KafkaMessage)) error
	Status() MWStatus
	Close() error
}

type kafkaHTTPClient struct {
	baseURL string
	client  *http.Client
}

func (k *kafkaHTTPClient) Produce(ctx context.Context, msg KafkaMessage) error {
	msg.Timestamp = time.Now()
	body, _ := json.Marshal(msg)
	req, _ := http.NewRequestWithContext(ctx, "POST", k.baseURL+"/topics/"+msg.Topic+"/messages", jsonReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("kafka produce failed: %d", resp.StatusCode)
	}
	return nil
}

func (k *kafkaHTTPClient) Subscribe(topic string, handler func(KafkaMessage)) error {
	return nil
}

func (k *kafkaHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", k.baseURL+"/brokers", nil)
	lat, err := measureLatency(func() error {
		resp, e := k.client.Do(req)
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "Kafka", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Kafka", Connected: true, Mode: "external", Latency: fmtLatency(lat)}
}

func (k *kafkaHTTPClient) Close() error { return nil }

type embeddedKafka struct {
	mu          sync.RWMutex
	topics      map[string][]KafkaMessage
	subscribers map[string][]func(KafkaMessage)
}

func newEmbeddedKafka() *embeddedKafka {
	return &embeddedKafka{
		topics:      make(map[string][]KafkaMessage),
		subscribers: make(map[string][]func(KafkaMessage)),
	}
}

func (k *embeddedKafka) Produce(_ context.Context, msg KafkaMessage) error {
	msg.Timestamp = time.Now()
	k.mu.Lock()
	k.topics[msg.Topic] = append(k.topics[msg.Topic], msg)
	if len(k.topics[msg.Topic]) > 10000 {
		k.topics[msg.Topic] = k.topics[msg.Topic][len(k.topics[msg.Topic])-5000:]
	}
	handlers := k.subscribers[msg.Topic]
	k.mu.Unlock()
	for _, h := range handlers {
		go h(msg)
	}
	return nil
}

func (k *embeddedKafka) Subscribe(topic string, handler func(KafkaMessage)) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.subscribers[topic] = append(k.subscribers[topic], handler)
	return nil
}

func (k *embeddedKafka) Status() MWStatus {
	k.mu.RLock()
	topicCount := len(k.topics)
	var msgCount int
	for _, msgs := range k.topics {
		msgCount += len(msgs)
	}
	k.mu.RUnlock()
	return MWStatus{
		Name: "Kafka", Connected: true, Mode: "embedded",
		Latency: "0.0ms",
		Details: fmt.Sprintf("in-memory event bus, %d topics, %d messages", topicCount, msgCount),
	}
}

func (k *embeddedKafka) Close() error { return nil }

const (
	TopicResultSubmitted = "inec.results.submitted"
	TopicResultValidated = "inec.results.validated"
	TopicResultFinalized = "inec.results.finalized"
	TopicResultDisputed  = "inec.results.disputed"
	TopicAuditLog        = "inec.audit.log"
	TopicIncidentReport  = "inec.incidents.reported"
	TopicFluvioIngest    = "inec.fluvio.ingest"
)

func initKafkaClient() KafkaClient {
	kafkaURL := envOrDefault("KAFKA_REST_URL", "")
	if kafkaURL != "" {
		client := &kafkaHTTPClient{
			baseURL: kafkaURL,
			client:  &http.Client{Timeout: 5 * time.Second},
		}
		s := client.Status()
		if s.Connected {
			log.Println("[Kafka] Connected to external Kafka REST at", kafkaURL)
			return client
		}
		log.Println("[Kafka] External Kafka unreachable, falling back to embedded")
	}
	log.Println("[Kafka] Using embedded in-memory event bus")
	return newEmbeddedKafka()
}
