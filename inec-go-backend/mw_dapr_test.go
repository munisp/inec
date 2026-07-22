package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestLocalDaprClientStateLifecycle(t *testing.T) {
	client := newLocalDaprClient()
	ctx := context.Background()
	value := map[string]string{"status": "ready"}
	if err := client.SaveState(ctx, "statestore", "health", value); err != nil {
		t.Fatalf("save state: %v", err)
	}

	raw, err := client.GetState(ctx, "statestore", "health")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if got["status"] != "ready" {
		t.Fatalf("state = %#v, want ready", got)
	}

	if err := client.DeleteState(ctx, "statestore", "health"); err != nil {
		t.Fatalf("delete state: %v", err)
	}
	raw, err = client.GetState(ctx, "statestore", "health")
	if err != nil {
		t.Fatalf("get deleted state: %v", err)
	}
	if raw != nil {
		t.Fatalf("deleted state = %q, want nil", raw)
	}
}

func TestLocalDaprClientValidatesInputs(t *testing.T) {
	client := newLocalDaprClient()
	ctx := context.Background()
	if err := client.PublishEvent(ctx, "", "topic", map[string]string{}); err == nil {
		t.Fatal("empty pubsub event succeeded")
	}
	if err := client.SaveState(ctx, "", "key", "value"); err == nil {
		t.Fatal("empty state store succeeded")
	}
	if _, err := client.InvokeService(ctx, "service", "method", nil); err == nil {
		t.Fatal("local service invocation unexpectedly succeeded")
	}
}

func TestInitDaprClientUsesLocalTransportOutsideProduction(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("DAPR_HTTP_URL", "")
	t.Setenv("DAPR_HTTP_PORT", "")
	client := initDaprClient()
	if _, ok := client.(*localDaprClient); !ok {
		t.Fatalf("client type = %T, want *localDaprClient", client)
	}
	if status := client.Status(); !status.Connected || status.Mode != "in-memory (non-production)" {
		t.Fatalf("unexpected local Dapr status: %#v", status)
	}
}
