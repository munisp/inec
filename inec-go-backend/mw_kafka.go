package main

import (
	"context"
	"encoding/json"
	"fmt"
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

// KafkaClient is backed exclusively by the configured Kafka brokers. Platform
// events must not be silently written to PostgreSQL or an in-process queue,
// because those substitutes do not provide Kafka partitioning or consumer-group
// delivery semantics.
type KafkaClient interface {
	Produce(ctx context.Context, msg KafkaMessage) error
	Subscribe(topic string, handler func(KafkaMessage)) error
	SubscribeGroup(topic, groupID string, handler func(KafkaMessage)) error
	Status() MWStatus
	Close() error
}

const (
	TopicResultSubmitted = "inec.results.submitted"
	TopicResultValidated = "inec.results.validated"
	TopicResultFinalized = "inec.results.finalized"
	TopicResultDisputed  = "inec.results.disputed"
	TopicAuditLog        = "inec.audit.log"
	TopicIncidentReport  = "inec.incidents.reported"
	TopicFluvioIngest    = "inec.fluvio.ingest"
)

// unavailableKafkaClient never stores, synthesizes, or substitutes Kafka delivery.
// It preserves the dependency error for callers until the configured native brokers recover.
type unavailableKafkaClient struct {
	reason string
}

func (k *unavailableKafkaClient) unavailable() error {
	return fmt.Errorf("native Kafka is unavailable: %s", k.reason)
}
func (k *unavailableKafkaClient) Produce(context.Context, KafkaMessage) error { return k.unavailable() }
func (k *unavailableKafkaClient) Subscribe(string, func(KafkaMessage)) error  { return k.unavailable() }
func (k *unavailableKafkaClient) SubscribeGroup(string, string, func(KafkaMessage)) error {
	return k.unavailable()
}
func (k *unavailableKafkaClient) Status() MWStatus {
	return MWStatus{Name: "Kafka", Connected: false, Mode: "native kafka-go required", Details: k.reason}
}
func (k *unavailableKafkaClient) Close() error { return nil }

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
	if len(brokers) == 0 {
		log.Warn().Msg("Kafka unavailable: KAFKA_BROKERS not set")
		return &unavailableKafkaClient{reason: "KAFKA_BROKERS must be configured"}
	}
	client := newRealKafkaClient(brokers)
	status := client.Status()
	if !status.Connected {
		log.Warn().Strs("brokers", brokers).Str("details", status.Details).Msg("Kafka unavailable")
		return &unavailableKafkaClient{reason: status.Details}
	}
	if err := client.ensureTopics(); err != nil {
		log.Warn().Strs("brokers", brokers).Err(err).Msg("Kafka topic provisioning failed")
		return &unavailableKafkaClient{reason: err.Error()}
	}
	log.Info().Strs("brokers", brokers).Msg("Kafka native client connected")
	return client
}
