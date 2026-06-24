package main

import (
	"context"
	"encoding/json"
	"fmt"
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

// KafkaClient defines the interface for Kafka operations.
type KafkaClient interface {
	Produce(ctx context.Context, msg KafkaMessage) error
	Subscribe(topic string, handler func(KafkaMessage)) error
	Status() MWStatus
	Close() error
}

// Kafka topic constants
const (
	TopicResultSubmitted = "inec.results.submitted"
	TopicResultValidated = "inec.results.validated"
	TopicResultFinalized = "inec.results.finalized"
	TopicResultDisputed  = "inec.results.disputed"
	TopicAuditLog        = "inec.audit.log"
	TopicIncidentReport  = "inec.incidents.reported"
	TopicFluvioIngest    = "inec.fluvio.ingest"
)

// --- Real Kafka client using segmentio/kafka-go ---

type realKafkaClient struct {
	brokers []string
	writers map[string]*kafka.Writer
	mu      sync.RWMutex
}

func newRealKafkaClient(brokers []string) *realKafkaClient {
	return &realKafkaClient{
		brokers: brokers,
		writers: make(map[string]*kafka.Writer),
	}
}

func (k *realKafkaClient) getWriter(topic string) *kafka.Writer {
	k.mu.RLock()
	w, ok := k.writers[topic]
	k.mu.RUnlock()
	if ok {
		return w
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if w, ok = k.writers[topic]; ok {
		return w
	}
	w = &kafka.Writer{
		Addr:         kafka.TCP(k.brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireOne,
		Async:        false,
	}
	k.writers[topic] = w
	return w
}

func (k *realKafkaClient) Produce(ctx context.Context, msg KafkaMessage) error {
	msg.Timestamp = time.Now()
	value, _ := json.Marshal(msg.Value)
	w := k.getWriter(msg.Topic)
	return w.WriteMessages(ctx, kafka.Message{
		Key:   []byte(msg.Key),
		Value: value,
		Time:  msg.Timestamp,
	})
}

func (k *realKafkaClient) Subscribe(topic string, handler func(KafkaMessage)) error {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        k.brokers,
		Topic:          topic,
		GroupID:        "inec-backend-" + topic,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
	})
	go func() {
		for {
			m, err := reader.ReadMessage(context.Background())
			if err != nil {
				log.Error().Err(err).Str("topic", topic).Msg("Kafka consume error")
				time.Sleep(time.Second)
				continue
			}
			var val map[string]interface{}
			json.Unmarshal(m.Value, &val)
			handler(KafkaMessage{
				Topic:     topic,
				Key:       string(m.Key),
				Value:     val,
				Timestamp: m.Time,
			})
		}
	}()
	return nil
}

func (k *realKafkaClient) Status() MWStatus {
	conn, err := kafka.Dial("tcp", k.brokers[0])
	if err != nil {
		return MWStatus{Name: "Kafka", Connected: false, Mode: "native kafka-go (unreachable)", Details: err.Error()}
	}
	defer conn.Close()

	brokers, err := conn.Brokers()
	if err != nil {
		return MWStatus{Name: "Kafka", Connected: false, Mode: "native kafka-go", Details: err.Error()}
	}
	return MWStatus{
		Name: "Kafka", Connected: true, Mode: "native kafka-go",
		Latency: "< 1ms",
		Details: fmt.Sprintf("%d broker(s), consumer groups, async produce", len(brokers)),
	}
}

func (k *realKafkaClient) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, w := range k.writers {
		w.Close()
	}
	return nil
}

// --- PostgreSQL-backed fallback (persistent) ---

type pgKafka struct {
	mu          sync.RWMutex
	subscribers map[string][]func(KafkaMessage)
}

func newPGKafka() *pgKafka {
	db.Exec(`CREATE TABLE IF NOT EXISTS kafka_messages (
		id BIGSERIAL PRIMARY KEY,
		topic TEXT NOT NULL,
		key TEXT NOT NULL DEFAULT '',
		value JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_kafka_messages_topic ON kafka_messages(topic, created_at DESC)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS kafka_subscriptions (
		id SERIAL PRIMARY KEY,
		topic TEXT NOT NULL,
		last_processed_id BIGINT NOT NULL DEFAULT 0,
		consumer_group TEXT NOT NULL DEFAULT 'inec-backend',
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(topic, consumer_group)
	)`)
	log.Info().Msg("Kafka fallback: PostgreSQL-backed event bus initialized")
	return &pgKafka{subscribers: make(map[string][]func(KafkaMessage))}
}

func (k *pgKafka) Produce(_ context.Context, msg KafkaMessage) error {
	msg.Timestamp = time.Now()
	valueJSON, _ := json.Marshal(msg.Value)
	_, err := db.Exec(`INSERT INTO kafka_messages (topic, key, value, created_at) VALUES ($1, $2, $3, $4)`,
		msg.Topic, msg.Key, string(valueJSON), msg.Timestamp)
	if err != nil {
		return fmt.Errorf("pg kafka produce: %w", err)
	}

	// Notify in-process subscribers
	k.mu.RLock()
	handlers := k.subscribers[msg.Topic]
	k.mu.RUnlock()
	for _, h := range handlers {
		go h(msg)
	}

	// Trim old messages (keep last 50K per topic)
	db.Exec(`DELETE FROM kafka_messages WHERE topic=$1 AND id NOT IN (
		SELECT id FROM kafka_messages WHERE topic=$1 ORDER BY id DESC LIMIT 50000)`, msg.Topic)
	return nil
}

func (k *pgKafka) Subscribe(topic string, handler func(KafkaMessage)) error {
	k.mu.Lock()
	k.subscribers[topic] = append(k.subscribers[topic], handler)
	k.mu.Unlock()

	// Replay unprocessed messages from PG
	go func() {
		var lastID int64
		db.QueryRow(`SELECT COALESCE(last_processed_id, 0) FROM kafka_subscriptions 
			WHERE topic=$1 AND consumer_group='inec-backend'`, topic).Scan(&lastID)

		rows, err := db.Query(`SELECT id, topic, key, value, created_at FROM kafka_messages 
			WHERE topic=$1 AND id > $2 ORDER BY id ASC LIMIT 1000`, topic, lastID)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var t, k, v string
			var ts time.Time
			rows.Scan(&id, &t, &k, &v, &ts)
			var val map[string]interface{}
			json.Unmarshal([]byte(v), &val)
			handler(KafkaMessage{Topic: t, Key: k, Value: val, Timestamp: ts})
			lastID = id
		}
		db.Exec(`INSERT INTO kafka_subscriptions (topic, consumer_group, last_processed_id, updated_at) 
			VALUES ($1, 'inec-backend', $2, NOW()) 
			ON CONFLICT (topic, consumer_group) DO UPDATE SET last_processed_id=$2, updated_at=NOW()`,
			topic, lastID)
	}()
	return nil
}

func (k *pgKafka) Status() MWStatus {
	var topicCount, msgCount int
	db.QueryRow(`SELECT COUNT(DISTINCT topic), COUNT(*) FROM kafka_messages`).Scan(&topicCount, &msgCount)
	return MWStatus{
		Name: "Kafka", Connected: true, Mode: "pg-backed",
		Latency: "< 1ms",
		Details: fmt.Sprintf("PostgreSQL-persisted event bus, %d topics, %d messages", topicCount, msgCount),
	}
}

func (k *pgKafka) Close() error { return nil }

// --- Init ---

func initKafkaClient() KafkaClient {
	brokersStr := envOrDefault("KAFKA_BROKERS", "")
	if brokersStr == "" {
		// Try legacy URL
		kafkaURL := envOrDefault("KAFKA_REST_URL", "")
		if kafkaURL != "" {
			brokersStr = kafkaURL
		}
	}

	if brokersStr != "" {
		brokers := []string{brokersStr}
		client := newRealKafkaClient(brokers)
		s := client.Status()
		if s.Connected {
			log.Info().Strs("brokers", brokers).Msg("Kafka connected via kafka-go")
			return client
		}
		log.Warn().Str("brokers", brokersStr).Msg("Kafka unreachable, falling back to embedded")
		client.Close()
	}
	log.Info().Msg("Kafka using PostgreSQL-backed event bus (persistent)")
	return newPGKafka()
}
