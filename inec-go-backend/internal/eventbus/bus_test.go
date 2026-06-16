package eventbus

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLocalBusPublishSubscribe(t *testing.T) {
	bus := NewLocal()
	defer bus.Close()

	var mu sync.Mutex
	received := make([]Event, 0)

	bus.Subscribe(Subscriber{
		ID:      "test-sub",
		Pattern: "election.created",
		Handler: func(ctx context.Context, e Event) error {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
			return nil
		},
	})

	bus.Publish(context.Background(), Event{
		ID:        "evt-1",
		Type:      "election.created",
		Source:    "test",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"name": "Presidential 2027"},
	})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].ID != "evt-1" {
		t.Fatalf("expected event ID 'evt-1', got %s", received[0].ID)
	}
	mu.Unlock()
}

func TestLocalBusWildcardPattern(t *testing.T) {
	bus := NewLocal()
	defer bus.Close()

	var mu sync.Mutex
	count := 0

	bus.Subscribe(Subscriber{
		ID:      "wildcard-sub",
		Pattern: "election.*",
		Handler: func(ctx context.Context, e Event) error {
			mu.Lock()
			count++
			mu.Unlock()
			return nil
		},
	})

	bus.Publish(context.Background(), Event{Type: "election.created", Timestamp: time.Now()})
	bus.Publish(context.Background(), Event{Type: "election.state_changed", Timestamp: time.Now()})
	bus.Publish(context.Background(), Event{Type: "biometric.verified", Timestamp: time.Now()})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if count != 2 {
		t.Fatalf("expected 2 matched events, got %d", count)
	}
	mu.Unlock()
}

func TestLocalBusUnsubscribe(t *testing.T) {
	bus := NewLocal()
	defer bus.Close()

	var mu sync.Mutex
	count := 0

	cancel := bus.Subscribe(Subscriber{
		ID:      "temp-sub",
		Pattern: "test.event",
		Handler: func(ctx context.Context, e Event) error {
			mu.Lock()
			count++
			mu.Unlock()
			return nil
		},
	})

	bus.Publish(context.Background(), Event{Type: "test.event", Timestamp: time.Now()})
	time.Sleep(50 * time.Millisecond)

	cancel() // Unsubscribe

	bus.Publish(context.Background(), Event{Type: "test.event", Timestamp: time.Now()})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if count != 1 {
		t.Fatalf("expected 1 event (unsubscribed before second), got %d", count)
	}
	mu.Unlock()
}

func TestLocalBusNoSubscribers(t *testing.T) {
	bus := NewLocal()
	defer bus.Close()

	err := bus.Publish(context.Background(), Event{Type: "orphan.event", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("expected no error for unmatched publish, got %v", err)
	}
}
