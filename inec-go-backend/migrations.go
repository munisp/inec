package main

import (
	"database/sql"
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type FileMigration struct {
	Version     int
	Description string
	UpSQL       string
	DownSQL     string
}

func loadMigrations() ([]FileMigration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("reading migrations dir: %w", err)
	}

	type pair struct {
		up, down string
		desc     string
	}
	byVersion := map[int]*pair{}
	re := regexp.MustCompile(`^(\d+)_(.+)\.(up|down)\.sql$`)

	for _, e := range entries {
		m := re.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		ver, _ := strconv.Atoi(m[1])
		desc := strings.ReplaceAll(m[2], "_", " ")
		direction := m[3]

		data, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", e.Name(), err)
		}

		p, ok := byVersion[ver]
		if !ok {
			p = &pair{desc: desc}
			byVersion[ver] = p
		}
		if direction == "up" {
			p.up = string(data)
		} else {
			p.down = string(data)
		}
	}

	var result []FileMigration
	for ver, p := range byVersion {
		result = append(result, FileMigration{
			Version:     ver,
			Description: p.desc,
			UpSQL:       p.up,
			DownSQL:     p.down,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result, nil
}

func runMigrations(database *sql.DB) error {
	// Ensure schema_migrations table exists
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			description TEXT,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("loading migrations: %w", err)
	}

	var currentVersion int
	database.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)

	applied := 0
	for _, m := range migrations {
		if m.Version <= currentVersion {
			continue
		}

		log.Info().
			Int("version", m.Version).
			Str("description", m.Description).
			Msg("Applying migration")

		tx, err := database.Begin()
		if err != nil {
			return fmt.Errorf("beginning tx for v%d: %w", m.Version, err)
		}

		if _, err := tx.Exec(m.UpSQL); err != nil {
			tx.Rollback()
			log.Error().Err(err).Int("version", m.Version).Msg("Migration failed")
			return fmt.Errorf("migration v%d failed: %w", m.Version, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version, description, applied_at) VALUES ($1, $2, $3)",
			m.Version, m.Description, time.Now(),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration v%d: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v%d: %w", m.Version, err)
		}
		applied++
	}

	if applied > 0 {
		log.Info().Int("applied", applied).Int("current_version", currentVersion+applied).Msg("Migrations complete")
	} else {
		log.Info().Int("current_version", currentVersion).Msg("Database schema up to date")
	}
	return nil
}

func rollbackMigration(database *sql.DB) error {
	var currentVersion int
	database.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)
	if currentVersion == 0 {
		log.Info().Msg("No migrations to rollback")
		return nil
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if m.Version == currentVersion {
			log.Info().Int("version", m.Version).Str("description", m.Description).Msg("Rolling back migration")
			if _, err := database.Exec(m.DownSQL); err != nil {
				return fmt.Errorf("rollback v%d failed: %w", m.Version, err)
			}
			database.Exec("DELETE FROM schema_migrations WHERE version = $1", currentVersion)
			log.Info().Int("version", m.Version).Msg("Rollback complete")
			return nil
		}
	}
	return fmt.Errorf("migration v%d not found", currentVersion)
}

func getMigrationStatus(database *sql.DB) ([]map[string]interface{}, error) {
	migrations, err := loadMigrations()
	if err != nil {
		return nil, err
	}

	applied := map[int]time.Time{}
	rows, err := database.Query("SELECT version, applied_at FROM schema_migrations ORDER BY version")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var v int
			var at time.Time
			rows.Scan(&v, &at)
			applied[v] = at
		}
	}

	var status []map[string]interface{}
	for _, m := range migrations {
		entry := map[string]interface{}{
			"version":     m.Version,
			"description": m.Description,
		}
		if at, ok := applied[m.Version]; ok {
			entry["status"] = "applied"
			entry["applied_at"] = at.Format(time.RFC3339)
		} else {
			entry["status"] = "pending"
		}
		status = append(status, entry)
	}
	return status, nil
}
