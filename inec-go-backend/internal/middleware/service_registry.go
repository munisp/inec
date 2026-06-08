// Package middleware provides the service registry that holds references to all
// infrastructure services.  Each service is behind an interface so it can be
// swapped between real (production) and embedded (dev) implementations without
// changing business logic.
//
// The registry enables:
// 1. Dependency injection — handlers receive services via context, not globals
// 2. Horizontal scaling — services use Redis/Kafka when available, not in-memory
// 3. Testability — mock implementations can be injected in tests
// 4. Service decomposition — when a service is extracted to a separate binary,
//    the interface stays the same and only the implementation changes
package middleware

import (
	"context"
	"sync"

	"inec-go-backend/internal/circuit"
	"inec-go-backend/internal/eventbus"
	"inec-go-backend/internal/observability"
)

// ServiceRegistry holds all infrastructure service references.
// Thread-safe after initialization.
type ServiceRegistry struct {
	// Circuit breakers for all external dependencies
	Breakers *circuit.Registry

	// Event bus for inter-service communication
	EventBus eventbus.Bus

	// Distributed tracing
	Tracer *observability.Tracer

	// Service metadata
	mu       sync.RWMutex
	services map[string]ServiceInfo
}

// ServiceInfo describes a registered service.
type ServiceInfo struct {
	Name       string `json:"name"`
	Mode       string `json:"mode"`       // "native", "embedded", "remote"
	Connected  bool   `json:"connected"`
	Latency    string `json:"latency,omitempty"`
	Version    string `json:"version,omitempty"`
	BreakerState string `json:"breaker_state,omitempty"`
}

// NewServiceRegistry creates a registry with all supporting infrastructure.
func NewServiceRegistry() *ServiceRegistry {
	breakers := circuit.NewRegistry()
	tracer := observability.NewTracer("inec-backend", &observability.LogExporter{})

	return &ServiceRegistry{
		Breakers: breakers,
		EventBus: eventbus.NewLocal(),
		Tracer:   tracer,
		services: make(map[string]ServiceInfo),
	}
}

// Register adds or updates a service in the registry.
func (r *ServiceRegistry) Register(info ServiceInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[info.Name] = info
}

// Get returns info for a named service.
func (r *ServiceRegistry) Get(name string) (ServiceInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.services[name]
	return info, ok
}

// All returns all registered services with their breaker states.
func (r *ServiceRegistry) All() []ServiceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ServiceInfo, 0, len(r.services))
	for _, info := range r.services {
		// Enrich with breaker state
		breaker := r.Breakers.Get(info.Name)
		info.BreakerState = breaker.State().String()
		out = append(out, info)
	}
	return out
}

// Shutdown gracefully shuts down all services.
func (r *ServiceRegistry) Shutdown() {
	if r.EventBus != nil {
		r.EventBus.Close()
	}
	if r.Tracer != nil && r.Tracer.Exporter != nil {
		r.Tracer.Exporter.Shutdown(context.Background())
	}
}

// --- Context-based dependency injection ---

type registryKey struct{}

// WithRegistry attaches the service registry to a context.
func WithRegistry(ctx context.Context, reg *ServiceRegistry) context.Context {
	return context.WithValue(ctx, registryKey{}, reg)
}

// FromContext extracts the service registry from context.
func FromContext(ctx context.Context) *ServiceRegistry {
	if reg, ok := ctx.Value(registryKey{}).(*ServiceRegistry); ok {
		return reg
	}
	return nil
}
