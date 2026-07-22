package main

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryKafkaClientDeliversMessages(t *testing.T) {
	client := newInMemoryKafkaClient()
	received := make(chan KafkaMessage, 1)
	if err := client.Subscribe(TopicAuditLog, func(msg KafkaMessage) {
		received <- msg
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	message := KafkaMessage{Topic: TopicAuditLog, Key: "audit-1", Value: map[string]interface{}{"action": "created"}}
	if err := client.Produce(context.Background(), message); err != nil {
		t.Fatalf("produce: %v", err)
	}

	select {
	case delivered := <-received:
		if delivered.Key != message.Key || delivered.Value["action"] != "created" {
			t.Fatalf("delivered message = %#v, want %#v", delivered, message)
		}
		if delivered.Timestamp.IsZero() {
			t.Fatal("delivered message timestamp was not populated")
		}
	case <-time.After(time.Second):
		t.Fatal("in-memory Kafka subscription did not receive a produced message")
	}
}

func TestInMemoryKafkaClientValidatesSubscriptions(t *testing.T) {
	client := newInMemoryKafkaClient()
	if err := client.Subscribe("", func(KafkaMessage) {}); err == nil {
		t.Fatal("empty topic subscription succeeded")
	}
	if err := client.Subscribe(TopicAuditLog, nil); err == nil {
		t.Fatal("nil handler subscription succeeded")
	}
	if err := client.SubscribeGroup(TopicAuditLog, "", func(KafkaMessage) {}); err == nil {
		t.Fatal("empty group subscription succeeded")
	}
}

func TestInitKafkaClientUsesInMemoryTransportOutsideProduction(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("KAFKA_BROKERS", "")
	client := initKafkaClient()
	if _, ok := client.(*inMemoryKafkaClient); !ok {
		t.Fatalf("client type = %T, want *inMemoryKafkaClient", client)
	}
	if status := client.Status(); !status.Connected || status.Mode != "in-memory (non-production)" {
		t.Fatalf("unexpected in-memory client status: %#v", status)
	}
}
