package main

import (
	"go.uber.org/zap"
)

// APISIXOptimizer manages dynamic APISIX route configuration for traffic shaping.
//
// Key optimizations:
// - Dynamic upstream weight adjustment based on backend health
// - Rate limiting per election phase (normal: 10K/s, election day: 1M/s)
// - Route caching (avoid etcd lookups on hot paths)
// - Batch route updates via Admin API
// - Traffic mirroring for shadow testing
// - Connection pooling configuration pushed to APISIX
type APISIXOptimizer struct {
	cfg    Config
	logger *zap.Logger
}

func NewAPISIXOptimizer(cfg Config, logger *zap.Logger) *APISIXOptimizer {
	return &APISIXOptimizer{
		cfg:    cfg,
		logger: logger,
	}
}

// OptimalRouteConfig returns APISIX route config optimized for millions TPS.
type OptimalRouteConfig struct {
	// Connection pool settings for upstream
	UpstreamKeepalive        int `json:"upstream_keepalive"`         // idle connections per upstream (default 320)
	UpstreamKeepaliveTimeout int `json:"upstream_keepalive_timeout"` // seconds (default 60)
	UpstreamKeepaliveRequests int `json:"upstream_keepalive_requests"` // max reqs per conn (default 1000)

	// Rate limiting
	RateLimitCount  int `json:"rate_limit_count"`  // requests per second
	RateLimitWindow int `json:"rate_limit_window"` // window in seconds

	// Circuit breaker
	UnhealthyThreshold   int `json:"unhealthy_threshold"`   // consecutive failures to trip
	HealthyThreshold     int `json:"healthy_threshold"`     // consecutive successes to close
	HealthCheckInterval  int `json:"health_check_interval"` // seconds between checks

	// Retries
	Retries     int `json:"retries"`
	RetryTimeout int `json:"retry_timeout"` // seconds
}

// ElectionDayConfig returns config optimized for election day peak load.
func ElectionDayConfig() OptimalRouteConfig {
	return OptimalRouteConfig{
		UpstreamKeepalive:         640,
		UpstreamKeepaliveTimeout:  120,
		UpstreamKeepaliveRequests: 10000,
		RateLimitCount:            1000000, // 1M req/s
		RateLimitWindow:           1,
		UnhealthyThreshold:        5,
		HealthyThreshold:          2,
		HealthCheckInterval:       1,
		Retries:                   3,
		RetryTimeout:              2,
	}
}

// NormalConfig returns config for non-peak periods.
func NormalConfig() OptimalRouteConfig {
	return OptimalRouteConfig{
		UpstreamKeepalive:         320,
		UpstreamKeepaliveTimeout:  60,
		UpstreamKeepaliveRequests: 1000,
		RateLimitCount:            10000, // 10K req/s
		RateLimitWindow:           1,
		UnhealthyThreshold:        3,
		HealthyThreshold:          3,
		HealthCheckInterval:       5,
		Retries:                   2,
		RetryTimeout:              5,
	}
}
