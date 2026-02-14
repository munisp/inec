package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

type MiddlewareHub struct {
	Redis       RedisClient
	Kafka       KafkaClient
	Temporal    TemporalClient
	Dapr        DaprClient
	Keycloak    KeycloakClient
	Permify     PermifyClient
	Fluvio      FluvioClient
	TigerBeetle TigerBeetleClient
	APISIX      APISIXClient
	Lakehouse   LakehouseClient
	mu          sync.RWMutex
	status      map[string]MWStatus
}

type MWStatus struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Mode      string `json:"mode"`
	Latency   string `json:"latency,omitempty"`
	Details   string `json:"details,omitempty"`
}

var mwHub *MiddlewareHub

func initMiddlewareHub() *MiddlewareHub {
	hub := &MiddlewareHub{
		status: make(map[string]MWStatus),
	}

	hub.Redis = initRedisClient()
	hub.setStatus("redis", hub.Redis.Ping())

	hub.Kafka = initKafkaClient()
	hub.setStatus("kafka", hub.Kafka.Status())

	hub.Temporal = initTemporalClient()
	hub.setStatus("temporal", hub.Temporal.Status())

	hub.Dapr = initDaprClient()
	hub.setStatus("dapr", hub.Dapr.Status())

	hub.Keycloak = initKeycloakClient()
	hub.setStatus("keycloak", hub.Keycloak.Status())

	hub.Permify = initPermifyClient()
	hub.setStatus("permify", hub.Permify.Status())

	hub.Fluvio = initFluvioClient()
	hub.setStatus("fluvio", hub.Fluvio.Status())

	hub.TigerBeetle = initTigerBeetleClient()
	hub.setStatus("tigerbeetle", hub.TigerBeetle.Status())

	hub.APISIX = initAPISIXClient()
	hub.setStatus("apisix", hub.APISIX.Status())

	hub.Lakehouse = initLakehouseClient()
	hub.setStatus("lakehouse", hub.Lakehouse.Status())

	hub.logStatus()
	return hub
}

func (h *MiddlewareHub) setStatus(name string, s MWStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status[name] = s
}

func (h *MiddlewareHub) GetAllStatus() []MWStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]MWStatus, 0, len(h.status))
	for _, s := range h.status {
		out = append(out, s)
	}
	return out
}

func (h *MiddlewareHub) logStatus() {
	h.mu.RLock()
	defer h.mu.RUnlock()
	log.Println("=== Middleware Status ===")
	for name, s := range h.status {
		mode := s.Mode
		if s.Connected {
			mode += " [connected]"
		} else {
			mode += " [fallback]"
		}
		log.Printf("  %-14s %s", name, mode)
	}
	log.Println("========================")
}

func (h *MiddlewareHub) HealthCheck() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make(map[string]interface{})
	allHealthy := true
	for name, s := range h.status {
		result[name] = map[string]interface{}{
			"connected": s.Connected,
			"mode":      s.Mode,
			"latency":   s.Latency,
			"details":   s.Details,
		}
		if !s.Connected {
			allHealthy = false
		}
	}
	result["all_connected"] = allHealthy
	return result
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func measureLatency(fn func() error) (time.Duration, error) {
	t0 := time.Now()
	err := fn()
	return time.Since(t0), err
}

func fmtLatency(d time.Duration) string {
	return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000.0)
}
