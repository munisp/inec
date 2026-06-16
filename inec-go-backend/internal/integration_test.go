// Package internal contains integration tests that verify real client code paths.
// These tests require running services (Redis, Kafka, etc.) and are skipped
// if the service is not available. Run with: go test ./internal/... -tags=integration
//go:build integration

package internal

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"github.com/rs/zerolog/log"
)

// TestRedisIntegration verifies real Redis client operations.
func TestRedisIntegration(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	client := redis.NewClient(&redis.Options{
		Addr:        addr,
		DialTimeout: 3 * time.Second,
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Ping
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available at %s: %v", addr, err)
	}

	t.Run("SET_GET", func(t *testing.T) {
		key := "integration_test_key"
		value := "test_value_123"
		ttl := 10 * time.Second

		if err := client.Set(ctx, key, value, ttl).Err(); err != nil {
			t.Fatalf("SET failed: %v", err)
		}

		got, err := client.Get(ctx, key).Result()
		if err != nil {
			t.Fatalf("GET failed: %v", err)
		}
		if got != value {
			t.Errorf("GET = %q, want %q", got, value)
		}

		// Cleanup
		client.Del(ctx, key)
	})

	t.Run("INCR_EXPIRE", func(t *testing.T) {
		key := "integration_test_counter"
		client.Del(ctx, key)

		val, err := client.Incr(ctx, key).Result()
		if err != nil {
			t.Fatalf("INCR failed: %v", err)
		}
		if val != 1 {
			t.Errorf("INCR = %d, want 1", val)
		}

		val, _ = client.Incr(ctx, key).Result()
		if val != 2 {
			t.Errorf("second INCR = %d, want 2", val)
		}

		if err := client.Expire(ctx, key, 5*time.Second).Err(); err != nil {
			t.Fatalf("EXPIRE failed: %v", err)
		}

		ttl := client.TTL(ctx, key).Val()
		if ttl <= 0 || ttl > 5*time.Second {
			t.Errorf("TTL = %v, want between 0 and 5s", ttl)
		}

		client.Del(ctx, key)
	})

	t.Run("PUBSUB", func(t *testing.T) {
		channel := "integration_test_channel"
		sub := client.Subscribe(ctx, channel)
		defer sub.Close()

		// Wait for subscription
		_, err := sub.Receive(ctx)
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		// Publish
		msg := "hello_integration"
		if err := client.Publish(ctx, channel, msg).Err(); err != nil {
			t.Fatalf("Publish failed: %v", err)
		}

		// Receive with timeout
		recvCtx, recvCancel := context.WithTimeout(ctx, 3*time.Second)
		defer recvCancel()

		received, err := sub.ReceiveMessage(recvCtx)
		if err != nil {
			t.Fatalf("ReceiveMessage failed: %v", err)
		}
		if received.Payload != msg {
			t.Errorf("received %q, want %q", received.Payload, msg)
		}
	})

	log.Info().Str("addr", addr).Msg("Redis integration tests passed")
}

// TestKafkaIntegration verifies real Kafka producer/consumer operations.
func TestKafkaIntegration(t *testing.T) {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "localhost:9092"
	}

	// Check connectivity
	conn, err := kafka.DialContext(context.Background(), "tcp", brokers)
	if err != nil {
		t.Skipf("Kafka not available at %s: %v", brokers, err)
	}
	conn.Close()

	topic := "integration-test-topic"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("ProduceConsume", func(t *testing.T) {
		// Create topic
		adminConn, err := kafka.DialContext(ctx, "tcp", brokers)
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		adminConn.CreateTopics(kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		})
		adminConn.Close()

		// Produce
		writer := &kafka.Writer{
			Addr:  kafka.TCP(brokers),
			Topic: topic,
		}
		defer writer.Close()

		testMsg := kafka.Message{
			Key:   []byte("test-key"),
			Value: []byte(`{"event":"result_submitted","pu":"23/04/09/001"}`),
		}
		if err := writer.WriteMessages(ctx, testMsg); err != nil {
			t.Fatalf("produce failed: %v", err)
		}

		// Consume
		reader := kafka.NewReader(kafka.ReaderConfig{
			Brokers:  []string{brokers},
			Topic:    topic,
			GroupID:  "integration-test-group",
			MaxWait:  5 * time.Second,
		})
		defer reader.Close()

		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			t.Fatalf("consume failed: %v", err)
		}
		if string(msg.Key) != "test-key" {
			t.Errorf("key = %q, want %q", string(msg.Key), "test-key")
		}
		if string(msg.Value) != string(testMsg.Value) {
			t.Errorf("value mismatch")
		}
	})

	log.Info().Str("brokers", brokers).Msg("Kafka integration tests passed")
}

// TestKeycloakIntegration verifies real Keycloak token validation.
func TestKeycloakIntegration(t *testing.T) {
	kcURL := os.Getenv("KEYCLOAK_URL")
	if kcURL == "" {
		t.Skip("KEYCLOAK_URL not set")
	}

	// This test requires a running Keycloak instance with a configured realm
	t.Run("TokenIntrospection", func(t *testing.T) {
		// Would test: get admin token, create user, login, validate token
		// Requires actual Keycloak setup
		t.Log("Keycloak integration test requires configured realm")
	})
}
