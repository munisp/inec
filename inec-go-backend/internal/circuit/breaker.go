// Package circuit implements the Circuit Breaker pattern for graceful degradation.
// Services wrap external calls in a breaker; after N consecutive failures the
// breaker opens and calls fail-fast for a configurable cooldown, then enters
// half-open to probe recovery.
package circuit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// State represents the current breaker state.
type State int

const (
	Closed   State = iota // Healthy — requests flow through
	Open                  // Tripped — requests fail-fast
	HalfOpen              // Probing — single request allowed to test recovery
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Config tunes breaker behaviour per service.
type Config struct {
	Name             string        // human-readable service name
	MaxFailures      int           // failures before tripping (default 5)
	CooldownDuration time.Duration // how long to stay open (default 30s)
	HalfOpenMax      int           // probes allowed in half-open (default 1)
	OnStateChange    func(name string, from, to State)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig(name string) Config {
	return Config{
		Name:             name,
		MaxFailures:      5,
		CooldownDuration: 30 * time.Second,
		HalfOpenMax:      1,
	}
}

// Breaker tracks state for a single downstream dependency.
type Breaker struct {
	mu           sync.RWMutex
	cfg          Config
	state        State
	failures     int
	successes    int
	lastFailure  time.Time
	halfOpenUsed int
}

// New creates a breaker in Closed state.
func New(cfg Config) *Breaker {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 5
	}
	if cfg.CooldownDuration <= 0 {
		cfg.CooldownDuration = 30 * time.Second
	}
	if cfg.HalfOpenMax <= 0 {
		cfg.HalfOpenMax = 1
	}
	return &Breaker{cfg: cfg, state: Closed}
}

// ErrOpen is returned when the breaker is open and not accepting calls.
var ErrOpen = errors.New("circuit breaker is open")

// Do executes fn if the breaker allows it.  On success the breaker moves
// toward Closed; on error it moves toward Open.
func (b *Breaker) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := b.allow(); err != nil {
		return err
	}
	err := fn(ctx)
	b.record(err)
	return err
}

// State returns current breaker state (thread-safe).
func (b *Breaker) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.currentState()
}

// Stats returns a snapshot for monitoring.
type Stats struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Failures    int    `json:"consecutive_failures"`
	LastFailure string `json:"last_failure,omitempty"`
}

func (b *Breaker) Stats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s := Stats{
		Name:     b.cfg.Name,
		State:    b.currentState().String(),
		Failures: b.failures,
	}
	if !b.lastFailure.IsZero() {
		s.LastFailure = b.lastFailure.Format(time.RFC3339)
	}
	return s
}

// --- internal ---

func (b *Breaker) currentState() State {
	if b.state == Open && time.Since(b.lastFailure) >= b.cfg.CooldownDuration {
		return HalfOpen
	}
	return b.state
}

func (b *Breaker) allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.currentState() {
	case Closed:
		return nil
	case HalfOpen:
		if b.halfOpenUsed < b.cfg.HalfOpenMax {
			b.halfOpenUsed++
			return nil
		}
		return fmt.Errorf("%w: %s (half-open probe limit reached)", ErrOpen, b.cfg.Name)
	default: // Open
		return fmt.Errorf("%w: %s (cooldown %s remaining)", ErrOpen, b.cfg.Name,
			(b.cfg.CooldownDuration - time.Since(b.lastFailure)).Truncate(time.Second))
	}
}

func (b *Breaker) record(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	prev := b.currentState()

	if err != nil {
		b.failures++
		b.successes = 0
		b.lastFailure = time.Now()
		if b.failures >= b.cfg.MaxFailures && prev != Open {
			b.transition(Open)
		}
		return
	}

	// Success
	b.successes++
	b.failures = 0
	if prev == HalfOpen {
		b.transition(Closed)
	}
}

func (b *Breaker) transition(to State) {
	from := b.state
	b.state = to
	b.halfOpenUsed = 0
	log.Info().Str("breaker", b.cfg.Name).Str("from", from.String()).Str("to", to.String()).Msg("circuit breaker state change")
	if b.cfg.OnStateChange != nil {
		go b.cfg.OnStateChange(b.cfg.Name, from, to)
	}
}

// Registry holds named breakers for the whole application.
type Registry struct {
	mu       sync.RWMutex
	breakers map[string]*Breaker
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{breakers: make(map[string]*Breaker)}
}

// Get returns (or creates) a breaker by name.
func (r *Registry) Get(name string) *Breaker {
	r.mu.RLock()
	if b, ok := r.breakers[name]; ok {
		r.mu.RUnlock()
		return b
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.breakers[name]; ok {
		return b
	}
	b := New(DefaultConfig(name))
	r.breakers[name] = b
	return b
}

// Register adds a breaker with custom config.
func (r *Registry) Register(cfg Config) *Breaker {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := New(cfg)
	r.breakers[cfg.Name] = b
	return b
}

// All returns stats for every registered breaker.
func (r *Registry) All() []Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Stats, 0, len(r.breakers))
	for _, b := range r.breakers {
		out = append(out, b.Stats())
	}
	return out
}
