package main

import (
	"context"
	"testing"
)

func TestInMemoryFluvioClientOrdersRecordsAndTracksGroups(t *testing.T) {
	client := newInMemoryFluvioClient()
	ctx := context.Background()
	if err := client.CreateTopic(ctx, "audit", 1); err != nil {
		t.Fatalf("create topic: %v", err)
	}
	for _, key := range []string{"first", "second"} {
		if err := client.Produce(ctx, "audit", FluvioRecord{Key: key, Value: map[string]interface{}{"key": key}}); err != nil {
			t.Fatalf("produce %q: %v", key, err)
		}
	}

	records, err := client.Consume(ctx, "audit", 0, 0)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if len(records) != 2 || records[0].Offset != 0 || records[1].Offset != 1 {
		t.Fatalf("unexpected ordered records: %#v", records)
	}

	var consumed []string
	if err := client.ConsumeGroup(ctx, "audit", "workers", "member-1", func(record FluvioRecord) error {
		consumed = append(consumed, record.Key)
		return nil
	}); err != nil {
		t.Fatalf("consume group: %v", err)
	}
	if len(consumed) != 2 || consumed[0] != "first" || consumed[1] != "second" {
		t.Fatalf("group consumption = %#v", consumed)
	}
	if err := client.ConsumeGroup(ctx, "audit", "workers", "member-1", func(record FluvioRecord) error {
		t.Fatalf("already consumed record was delivered again: %#v", record)
		return nil
	}); err != nil {
		t.Fatalf("second group consume: %v", err)
	}
}

func TestInitFluvioClientUsesInMemoryTransportOutsideProduction(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("FLUVIO_STREAM_URL", "")
	client := initFluvioClient()
	if _, ok := client.(*inMemoryFluvioClient); !ok {
		t.Fatalf("client type = %T, want *inMemoryFluvioClient", client)
	}
	if status := client.Status(); !status.Connected || status.Mode != "in-memory (non-production)" {
		t.Fatalf("unexpected in-memory Fluvio status: %#v", status)
	}
}
