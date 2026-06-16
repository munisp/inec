// Package eventbus provides an in-process event bus with optional Redis Pub/Sub
// backend for multi-pod consistency.  Services publish domain events (e.g.
// "election.results.submitted") and other services subscribe without direct
// coupling — enabling a microservice-ready architecture even inside a monolith.
package eventbus

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Event is the envelope for all domain events.
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`      // e.g. "election.results.submitted"
	Source    string                 `json:"source"`    // originating service
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	TraceID   string                 `json:"trace_id,omitempty"`
}

// Handler processes a single event.  Return an error to NACK (retry logic is
// transport-dependent).
type Handler func(ctx context.Context, event Event) error

// Subscriber is a named handler for a specific event type pattern.
type Subscriber struct {
	ID      string
	Pattern string // exact match or prefix with "*" suffix
	Handler Handler
}

// Bus defines the event bus interface — implementations can be local (channel)
// or distributed (Redis Pub/Sub, Kafka, NATS).
type Bus interface {
	Publish(ctx context.Context, event Event) error
	Subscribe(sub Subscriber) (cancel func())
	Close()
}

// --- Local (in-process) implementation ---

// LocalBus is a goroutine-safe in-process event bus.
type LocalBus struct {
	mu          sync.RWMutex
	subscribers map[string][]Subscriber
	closed      bool
}

// NewLocal creates a local event bus.
func NewLocal() *LocalBus {
	return &LocalBus{subscribers: make(map[string][]Subscriber)}
}

func (b *LocalBus) Publish(_ context.Context, event Event) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return nil
	}
	for pattern, subs := range b.subscribers {
		if matchPattern(pattern, event.Type) {
			for _, s := range subs {
				go func(s Subscriber, e Event) {
					if err := s.Handler(context.Background(), e); err != nil {
						log.Error().Err(err).Str("subscriber", s.ID).Str("event", e.Type).Msg("event handler error")
					}
				}(s, event)
			}
		}
	}
	return nil
}

func (b *LocalBus) Subscribe(sub Subscriber) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[sub.Pattern] = append(b.subscribers[sub.Pattern], sub)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subscribers[sub.Pattern]
		for i, s := range subs {
			if s.ID == sub.ID {
				b.subscribers[sub.Pattern] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}
}

func (b *LocalBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.subscribers = make(map[string][]Subscriber)
}

// --- Redis-backed implementation ---

// RedisBus uses Redis Pub/Sub for multi-pod event distribution.
type RedisBus struct {
	local    *LocalBus
	redisPub func(channel string, data []byte) error // injected Redis publish fn
	redisSub func(channel string, handler func([]byte)) func() // injected Redis subscribe fn
	cancels  []func()
	mu       sync.Mutex
}

// NewRedis creates a Redis-backed event bus.  Pass nil for redisPub/redisSub to
// fall back to local-only mode (dev).
func NewRedis(
	redisPub func(channel string, data []byte) error,
	redisSub func(channel string, handler func([]byte)) func(),
) *RedisBus {
	return &RedisBus{
		local:    NewLocal(),
		redisPub: redisPub,
		redisSub: redisSub,
	}
}

func (b *RedisBus) Publish(ctx context.Context, event Event) error {
	// Always publish locally
	if err := b.local.Publish(ctx, event); err != nil {
		return err
	}
	// Also publish to Redis for other pods
	if b.redisPub != nil {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		return b.redisPub("inec:events:"+event.Type, data)
	}
	return nil
}

func (b *RedisBus) Subscribe(sub Subscriber) func() {
	localCancel := b.local.Subscribe(sub)

	// Also subscribe via Redis
	if b.redisSub != nil {
		redisCancel := b.redisSub("inec:events:"+sub.Pattern, func(data []byte) {
			var event Event
			if err := json.Unmarshal(data, &event); err != nil {
				log.Error().Err(err).Msg("failed to unmarshal Redis event")
				return
			}
			if err := sub.Handler(context.Background(), event); err != nil {
				log.Error().Err(err).Str("subscriber", sub.ID).Msg("Redis event handler error")
			}
		})
		b.mu.Lock()
		b.cancels = append(b.cancels, redisCancel)
		b.mu.Unlock()

		return func() {
			localCancel()
			redisCancel()
		}
	}
	return localCancel
}

func (b *RedisBus) Close() {
	b.local.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, cancel := range b.cancels {
		cancel()
	}
	b.cancels = nil
}

// matchPattern does exact match or prefix-wildcard ("election.*" matches "election.created").
func matchPattern(pattern, eventType string) bool {
	if pattern == eventType {
		return true
	}
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(eventType) >= len(prefix) && eventType[:len(prefix)] == prefix
	}
	return false
}

// Well-known event types for the INEC platform.
const (
	EventElectionCreated          = "election.created"
	EventElectionStateChanged     = "election.state_changed"
	EventResultSubmitted          = "election.result.submitted"
	EventResultCollated           = "election.result.collated"
	EventBiometricVerified        = "biometric.verified"
	EventBiometricFailed          = "biometric.failed"
	EventVoterRegistered          = "voter.registered"
	EventIncidentReported         = "incident.reported"
	EventOfficialLocationUpdated  = "tracking.official.location"
	EventCrowdDensityUpdated      = "tracking.crowd.density"
	EventAnomalyDetected          = "anomaly.detected"
	EventAuditLogCreated          = "audit.log.created"
	EventGeofenceViolation        = "geofence.violation"
	EventBVASHeartbeat            = "bvas.heartbeat"
	EventBlockchainAttested       = "blockchain.attested"
)
