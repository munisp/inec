package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCaddyClient_Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newCaddyClient(server.URL)
	status := client.Status()

	if !status.Connected {
		t.Errorf("expected Caddy to be connected, got false")
	}
	if status.Mode != "edge" {
		t.Errorf("expected Mode edge, got %s", status.Mode)
	}
}

func TestCaddyClient_UpdateRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" && r.URL.Path == "/config/apps/http/servers/srv0/routes/test-route" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := newCaddyClient(server.URL)
	err := client.UpdateRoute(context.Background(), "test-route", map[string]interface{}{
		"match": []map[string]interface{}{
			{"path": []string{"/test"}},
		},
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}
