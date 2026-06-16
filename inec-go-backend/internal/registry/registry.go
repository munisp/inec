// Package registry provides a service registry for dependency injection
// and inter-service communication in the decomposed architecture.
package registry

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"sync"

	"inec-go-backend/internal/auth"
	"inec-go-backend/internal/biometric"
	"inec-go-backend/internal/circuit"
	"inec-go-backend/internal/election"
	"inec-go-backend/internal/eventbus"
	"inec-go-backend/internal/geo"

	"github.com/rs/zerolog/log"
)

// ServiceRegistry holds all service instances and manages their lifecycle.
type ServiceRegistry struct {
	mu sync.RWMutex

	// Core services
	Auth       *auth.Service
	MFA        *auth.MFAService
	Election   *election.Service
	Biometric  *biometric.Service
	Geo        *geo.Service
	EventBus   eventbus.Bus

	// Infrastructure
	DB             *sql.DB
	CircuitBreakers map[string]*circuit.Breaker

	// Service metadata
	services map[string]ServiceInfo
}

// ServiceInfo describes a registered service for health checks and routing.
type ServiceInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	Port        int    `json:"port,omitempty"`
	HealthPath  string `json:"health_path"`
	Description string `json:"description"`
}

// New creates a new service registry with all services initialized.
func New(db *sql.DB, jwtSecret []byte) (*ServiceRegistry, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection required")
	}

	reg := &ServiceRegistry{
		DB:              db,
		CircuitBreakers: make(map[string]*circuit.Breaker),
		services:        make(map[string]ServiceInfo),
	}

	// Initialize event bus (inter-service communication)
	reg.EventBus = eventbus.NewLocal()
	reg.register("eventbus", "1.0.0", "Event-driven inter-service communication")

	// Initialize auth service
	authCfg := auth.DefaultConfig(jwtSecret)
	reg.Auth = auth.NewService(db, authCfg)
	reg.register("auth", "1.0.0", "Authentication, sessions, token lifecycle")

	// Initialize MFA service
	reg.MFA = auth.NewMFAService(db, "INEC Platform")
	reg.register("mfa", "1.0.0", "Multi-factor authentication (TOTP, WebAuthn, backup codes)")

	// Initialize election service
	reg.Election = election.NewService(db)
	reg.register("election", "1.0.0", "Election lifecycle, FSM, results, collation")

	// Initialize biometric service
	vaultKey := make([]byte, 32)
	rand.Read(vaultKey)
	reg.Biometric = biometric.NewService(db, biometric.DefaultConfig(vaultKey))
	reg.register("biometric", "1.0.0", "Fingerprint/face verification, vault, ABIS")

	// Initialize geo service
	reg.Geo = geo.NewService(db)
	reg.register("geo", "1.0.0", "Geofencing, PostGIS, polling unit mapping")

	// Add circuit breakers for each service
	for name := range reg.services {
		reg.CircuitBreakers[name] = circuit.New(circuit.DefaultConfig(name))
	}

	log.Info().Int("services", len(reg.services)).Msg("Service registry initialized")
	return reg, nil
}

func (r *ServiceRegistry) register(name, version, desc string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[name] = ServiceInfo{
		Name:        name,
		Version:     version,
		Status:      "running",
		HealthPath:  fmt.Sprintf("/services/%s/health", name),
		Description: desc,
	}
}

// ListServices returns all registered services.
func (r *ServiceRegistry) ListServices() []ServiceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ServiceInfo, 0, len(r.services))
	for _, svc := range r.services {
		result = append(result, svc)
	}
	return result
}

// Health returns the health status of all services.
func (r *ServiceRegistry) Health() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	health := map[string]interface{}{
		"status":   "healthy",
		"services": len(r.services),
	}

	details := make(map[string]interface{})
	for name, info := range r.services {
		cb, ok := r.CircuitBreakers[name]
		cbState := "closed"
		if ok {
			cbState = cb.State().String()
		}
		details[name] = map[string]interface{}{
			"status":          info.Status,
			"version":         info.Version,
			"circuit_breaker": cbState,
		}
		if cbState == "open" {
			health["status"] = "degraded"
		}
	}
	health["details"] = details
	return health
}

// Shutdown gracefully shuts down all services.
func (r *ServiceRegistry) Shutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name := range r.services {
		r.services[name] = ServiceInfo{
			Name:   name,
			Status: "stopped",
		}
	}
	log.Info().Msg("All services stopped")
}
