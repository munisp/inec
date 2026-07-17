package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog/log"
)

// DBPoolConfig holds tunable connection pool parameters.
type DBPoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultDBPoolConfig returns production-grade pool settings for the INEC platform.
// These values are tuned for a 16-core PostgreSQL host under election-day load.
func DefaultDBPoolConfig() DBPoolConfig {
	return DBPoolConfig{
		MaxOpenConns:    100,
		MaxIdleConns:    25,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 10 * time.Minute,
	}
}

// ApplyPoolConfig applies connection pool settings to an existing *sql.DB instance.
func ApplyPoolConfig(db *sql.DB, cfg DBPoolConfig) {
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	log.Info().
		Int("max_open", cfg.MaxOpenConns).
		Int("max_idle", cfg.MaxIdleConns).
		Dur("max_lifetime", cfg.ConnMaxLifetime).
		Msg("DB connection pool configured")
}

// DBHealthCheck performs a liveness probe against the database.
func DBHealthCheck(ctx context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}
	return nil
}

// DBStats returns a snapshot of the current pool utilisation for metrics export.
func DBStats(db *sql.DB) sql.DBStats {
	return db.Stats()
}

// GetDSN builds the PostgreSQL DSN from environment variables with sensible defaults.
func GetDSN() string {
	dsn := os.Getenv("DATABASE_URL")
	if dsn != "" {
		return dsn
	}
	host := getEnvOrDefault("DB_HOST", "postgres")
	port := getEnvOrDefault("DB_PORT", "5432")
	user := getEnvOrDefault("DB_USER", "ngapp")
	pass := getEnvOrDefault("DB_PASSWORD", "ngapp")
	name := getEnvOrDefault("DB_NAME", "ngapp")
	sslMode := getEnvOrDefault("DB_SSLMODE", "require")
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=Africa/Lagos",
		host, port, user, pass, name, sslMode,
	)
}
