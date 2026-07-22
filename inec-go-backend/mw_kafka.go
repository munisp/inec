package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/segmentio/kafka-go"
)

// KafkaMessage is the message format for event streaming.
type KafkaMessage struct {
	Topic     string                 `json:"topic"`
	Key       string                 `json:"key"`
	Value     map[string]interface{} `json:"value"`
	Timestamp time.Time              `json:"timestamp"`
}

// KafkaClient is backed by configured Kafka brokers in production. Non-production
// runtimes may use the explicit in-memory implementation below for isolated tests
// and local development; it is never selected when APP_ENV=production.
type KafkaClient interface {
	Produce(ctx context.Context, msg KafkaMessage) error
	Subscribe(topic string, handler func(KafkaMessage)) error
	SubscribeGroup(topic, groupID string, handler func(KafkaMessage)) error
	Status() MWStatus
	Close() error
}

// inMemoryKafkaClient is an explicit non-production event transport. It keeps
// test and local-development flows runnable without implying Kafka durability,
// partitioning, or consumer-group semantics.
type inMemoryKafkaClient struct {
	mu          sync.RWMutex
	subscribers map[string][]func(KafkaMessage)
}

func newInMemoryKafkaClient() *inMemoryKafkaClient {
	return &inMemoryKafkaClient{subscribers: make(map[string][]func(KafkaMessage))}
}

func (k *inMemoryKafkaClient) Produce(ctx context.Context, msg KafkaMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}
	k.mu.RLock()
	handlers := append([]func(KafkaMessage){}, k.subscribers[msg.Topic]...)
	k.mu.RUnlock()
	for _, handler := range handlers {
		handler(msg)
	}
	return nil
}

func (k *inMemoryKafkaClient) Subscribe(topic string, handler func(KafkaMessage)) error {
	if strings.TrimSpace(topic) == "" || handler == nil {
		return fmt.Errorf("topic and handler are required for an in-memory Kafka subscription")
	}
	k.mu.Lock()
	k.subscribers[topic] = append(k.subscribers[topic], handler)
	k.mu.Unlock()
	return nil
}

func (k *inMemoryKafkaClient) SubscribeGroup(topic, groupID string, handler func(KafkaMessage)) error {
	if strings.TrimSpace(groupID) == "" {
		return fmt.Errorf("group ID is required for an in-memory Kafka group subscription")
	}
	return k.Subscribe(topic, handler)
}

func (k *inMemoryKafkaClient) Status() MWStatus {
	return MWStatus{Name: "Kafka", Connected: true, Mode: "in-memory (non-production)", Details: "ephemeral local event transport"}
}

func (k *inMemoryKafkaClient) Close() error { return nil }

const (
	TopicResultSubmitted = "inec.results.submitted"
	TopicResultValidated = "inec.results.validated"
	TopicResultFinalized = "inec.results.finalized"
	TopicResultDisputed  = "inec.results.disputed"
	TopicAuditLog        = "inec.audit.log"
	TopicIncidentReport  = "inec.incidents.reported"
	TopicFluvioIngest    = "inec.fluvio.ingest"
)

type realKafkaClient struct {
	brokers []string
	writers map[string]*kafka.Writer
	mu      sync.RWMutex
}

func newRealKafkaClient(brokers []string) *realKafkaClient {
	return &realKafkaClient{brokers: brokers, writers: make(map[string]*kafka.Writer)}
}

func (k *realKafkaClient) ensureTopics() error {
	topics := []string{
		TopicResultSubmitted, TopicResultValidated, TopicResultFinalized,
		TopicResultDisputed, TopicAuditLog, TopicIncidentReport, TopicFluvioIngest,
	}
	conn, err := kafka.Dial("tcp", k.brokers[0])
	if err != nil {
		return fmt.Errorf("dial Kafka for topic provisioning: %w", err)
	}
	defer conn.Close()
	configs := make([]kafka.TopicConfig, 0, len(topics))
	for _, topic := range topics {
		configs = append(configs, kafka.TopicConfig{Topic: topic, NumPartitions: 3, ReplicationFactor: 1})
	}
	if err := conn.CreateTopics(configs...); err != nil {
		// Kafka returns an error when a requested topic already exists on some
		// broker versions. Existing application topics are an idempotent success.
		if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return fmt.Errorf("provision Kafka topics: %w", err)
		}
	}
	return nil
}

func (k *realKafkaClient) getWriter(topic string) *kafka.Writer {
	k.mu.RLock()
	writer, ok := k.writers[topic]
	k.mu.RUnlock()
	if ok {
		return writer
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if writer, ok = k.writers[topic]; ok {
		return writer
	}
	writer = &kafka.Writer{
		Addr:         kafka.TCP(k.brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireOne,
		Async:        false,
	}
	k.writers[topic] = writer
	return writer
}

func (k *realKafkaClient) Produce(ctx context.Context, msg KafkaMessage) error {
	if strings.TrimSpace(msg.Topic) == "" {
		return fmt.Errorf("Kafka topic is required")
	}
	msg.Timestamp = time.Now().UTC()
	value, err := json.Marshal(msg.Value)
	if err != nil {
		return fmt.Errorf("marshal Kafka event: %w", err)
	}
	return k.getWriter(msg.Topic).WriteMessages(ctx, kafka.Message{Key: []byte(msg.Key), Value: value, Time: msg.Timestamp})
}

func (k *realKafkaClient) Subscribe(topic string, handler func(KafkaMessage)) error {
	return k.SubscribeGroup(topic, "inec-backend-"+topic, handler)
}

func (k *realKafkaClient) SubscribeGroup(topic, groupID string, handler func(KafkaMessage)) error {
	if strings.TrimSpace(topic) == "" || strings.TrimSpace(groupID) == "" {
		return fmt.Errorf("Kafka topic and consumer group are required")
	}
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        k.brokers,
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		StartOffset:    kafka.LastOffset,
	})
	go func() {
		defer reader.Close()
		consecutiveErrors := 0
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			message, err := reader.ReadMessage(ctx)
			cancel()
			if err != nil {
				consecutiveErrors++
				backoff := time.Duration(consecutiveErrors) * time.Second
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
				log.Error().Err(err).Str("topic", topic).Str("group", groupID).Int("consecutive_errors", consecutiveErrors).Msg("Kafka consume error")
				time.Sleep(backoff)
				continue
			}
			consecutiveErrors = 0
			var value map[string]interface{}
			if err := json.Unmarshal(message.Value, &value); err != nil {
				log.Error().Err(err).Str("topic", topic).Msg("Kafka message payload is not valid JSON")
				continue
			}
			handler(KafkaMessage{Topic: topic, Key: string(message.Key), Value: value, Timestamp: message.Time})
		}
	}()
	return nil
}

func (k *realKafkaClient) Status() MWStatus {
	conn, err := kafka.Dial("tcp", k.brokers[0])
	if err != nil {
		return MWStatus{Name: "Kafka", Connected: false, Mode: "native kafka-go", Details: err.Error()}
	}
	defer conn.Close()
	brokers, err := conn.Brokers()
	if err != nil {
		return MWStatus{Name: "Kafka", Connected: false, Mode: "native kafka-go", Details: err.Error()}
	}
	return MWStatus{Name: "Kafka", Connected: true, Mode: "native kafka-go", Latency: "< 1ms", Details: fmt.Sprintf("%d broker(s), consumer groups, acknowledged produce", len(brokers))}
}

func (k *realKafkaClient) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	var firstErr error
	for _, writer := range k.writers {
		if err := writer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func parseKafkaBrokers(raw string) []string {
	var brokers []string
	for _, broker := range strings.Split(raw, ",") {
		if broker = strings.TrimSpace(broker); broker != "" {
			brokers = append(brokers, broker)
		}
	}
	return brokers
}

func initKafkaClient() KafkaClient {
	brokers := parseKafkaBrokers(envOrDefault("KAFKA_BROKERS", ""))
	isProduction := strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
	if len(brokers) == 0 {
		if !isProduction {
			log.Warn().Msg("Kafka is not configured — using the explicit in-memory non-production event transport")
			return newInMemoryKafkaClient()
		}
		log.Fatal().Msg("Kafka is required: set KAFKA_BROKERS to one or more native broker addresses")
	}
	client := newRealKafkaClient(brokers)
	status := client.Status()
	if !status.Connected {
		log.Fatal().Strs("brokers", brokers).Str("details", status.Details).Msg("Kafka is required but unavailable")
	}
	if err := client.ensureTopics(); err != nil {
		log.Fatal().Strs("brokers", brokers).Err(err).Msg("Kafka topic provisioning failed")
	}
	log.Info().Strs("brokers", brokers).Msg("Kafka native client connected")
	return client
}
