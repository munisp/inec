// Command dbseed applies approved, deterministic non-production fixtures to a
// migrated INEC PostgreSQL database. It deliberately does not manufacture
// operational, personal, cryptographic, or integration-owned records.
package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

const confirmationFlag = "--confirm-non-production"

type manifest struct {
	Version int           `json:"version"`
	Tables  []tablePolicy `json:"tables"`
}

type tablePolicy struct {
	Table       string `json:"table"`
	Mode        string `json:"mode"` // fixture, live, external, or security
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type fixture struct {
	Version int            `json:"version"`
	Profile string         `json:"profile"`
	Source  string         `json:"source"`
	Tables  []fixtureTable `json:"tables"`
}

type fixtureTable struct {
	Table           string           `json:"table"`
	ConflictColumns []string         `json:"conflict_columns"`
	Rows            []map[string]any `json:"rows"`
}

type tableResult struct {
	Table string `json:"table"`
	Rows  int    `json:"rows"`
}

type coverageReport struct {
	GeneratedAt        time.Time      `json:"generated_at"`
	Environment        string         `json:"environment"`
	Profile            string         `json:"profile"`
	FixtureSHA256      string         `json:"fixture_sha256"`
	DryRun             bool           `json:"dry_run"`
	SchemaTableCount   int            `json:"schema_table_count"`
	ManifestTableCount int            `json:"manifest_table_count"`
	ModeCounts         map[string]int `json:"mode_counts"`
	Seeded             []tableResult  `json:"seeded"`
	UncoveredSchema    []string       `json:"uncovered_schema_tables"`
	StaleManifest      []string       `json:"stale_manifest_tables"`
}

func main() {
	var (
		dsn             string
		fixturesDir     string
		profile         string
		environment     string
		dryRun          bool
		confirm         bool
		reportPath      string
		requireCoverage bool
	)

	flag.StringVar(&dsn, "dsn", os.Getenv("DATABASE_URL"), "PostgreSQL DSN; defaults to DATABASE_URL")
	flag.StringVar(&fixturesDir, "fixtures", "fixtures/db", "directory containing table_manifest.json and profile fixtures")
	flag.StringVar(&profile, "profile", "baseline", "fixture profile name without .json")
	flag.StringVar(&environment, "environment", os.Getenv("DBSEED_ENV"), "required non-production environment: development or test")
	flag.BoolVar(&dryRun, "dry-run", false, "validate and execute in a transaction that is rolled back")
	flag.BoolVar(&confirm, "confirm-non-production", false, "required acknowledgement that this operates only on development or test databases")
	flag.StringVar(&reportPath, "report", "", "optional JSON coverage report path")
	flag.BoolVar(&requireCoverage, "require-complete-coverage", true, "fail when a schema table has no policy in table_manifest.json")
	flag.Parse()

	if !confirm {
		fatalf("%s is required", confirmationFlag)
	}
	if environment != "development" && environment != "test" {
		fatalf("--environment must be development or test; got %q", environment)
	}
	if strings.TrimSpace(dsn) == "" {
		fatalf("DATABASE_URL or --dsn is required")
	}
	if likelyProductionDSN(dsn) {
		fatalf("refusing a DSN that appears to target production")
	}

	manifestPath := filepath.Join(fixturesDir, "table_manifest.json")
	fixturePath := filepath.Join(fixturesDir, profile+".json")
	m, err := loadManifest(manifestPath)
	if err != nil {
		fatalf("loading manifest: %v", err)
	}
	f, rawFixture, err := loadFixture(fixturePath)
	if err != nil {
		fatalf("loading fixture: %v", err)
	}
	if f.Profile != profile {
		fatalf("fixture profile %q does not match --profile %q", f.Profile, profile)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fatalf("opening PostgreSQL connection: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		fatalf("connecting to PostgreSQL: %v", err)
	}
	if err := requireNonProductionDatabase(db, environment); err != nil {
		fatalf("database safety preflight: %v", err)
	}
	if err := requireMigratedSchema(db); err != nil {
		fatalf("schema preflight: %v", err)
	}

	schemaTables, err := discoverSchemaTables(db)
	if err != nil {
		fatalf("discovering schema tables: %v", err)
	}
	report, policyByTable := makeCoverageReport(m, f, rawFixture, environment, dryRun, schemaTables)
	if requireCoverage && len(report.UncoveredSchema) > 0 {
		fatalf("manifest does not classify %d schema table(s): %s", len(report.UncoveredSchema), strings.Join(report.UncoveredSchema, ", "))
	}
	if len(report.StaleManifest) > 0 {
		fatalf("manifest references %d missing schema table(s): %s", len(report.StaleManifest), strings.Join(report.StaleManifest, ", "))
	}

	tx, err := db.Begin()
	if err != nil {
		fatalf("starting seed transaction: %v", err)
	}
	defer tx.Rollback()

	for _, ft := range f.Tables {
		policy, ok := policyByTable[ft.Table]
		if !ok {
			fatalf("fixture table %q is not in table_manifest.json", ft.Table)
		}
		if policy.Mode != "fixture" {
			fatalf("fixture table %q has policy mode %q; only fixture tables may be seeded", ft.Table, policy.Mode)
		}
		if err := validateFixtureTable(db, ft); err != nil {
			fatalf("validating fixture table %q: %v", ft.Table, err)
		}
		for _, row := range ft.Rows {
			if err := insertIdempotent(tx, ft, row); err != nil {
				fatalf("seeding table %q: %v", ft.Table, err)
			}
		}
		report.Seeded = append(report.Seeded, tableResult{Table: ft.Table, Rows: len(ft.Rows)})
	}

	if dryRun {
		if err := tx.Rollback(); err != nil {
			fatalf("rolling back dry-run transaction: %v", err)
		}
	} else if err := tx.Commit(); err != nil {
		fatalf("committing seed transaction: %v", err)
	}

	if reportPath != "" {
		if err := writeReport(reportPath, report); err != nil {
			fatalf("writing report: %v", err)
		}
	}

	fmt.Printf("dbseed completed profile=%s dry_run=%t fixture_tables=%d fixture_rows=%d schema_tables=%d\n",
		profile, dryRun, len(report.Seeded), fixtureRowCount(report.Seeded), report.SchemaTableCount)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "dbseed: "+format+"\n", args...)
	os.Exit(1)
}

func likelyProductionDSN(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return true
	}
	candidate := strings.ToLower(u.Host + "/" + u.Path)
	return strings.Contains(candidate, "production") || strings.Contains(candidate, "prod")
}

func loadManifest(path string) (manifest, error) {
	var m manifest
	data, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Version != 1 || len(m.Tables) == 0 {
		return m, errors.New("manifest must be version 1 and declare at least one table")
	}
	seen := map[string]bool{}
	for _, p := range m.Tables {
		if !validIdentifier(p.Table) || seen[p.Table] {
			return m, fmt.Errorf("invalid or duplicate table policy %q", p.Table)
		}
		if p.Mode != "fixture" && p.Mode != "live" && p.Mode != "external" && p.Mode != "security" {
			return m, fmt.Errorf("table %q has unsupported mode %q", p.Table, p.Mode)
		}
		seen[p.Table] = true
	}
	return m, nil
}

func loadFixture(path string) (fixture, []byte, error) {
	var f fixture
	data, err := os.ReadFile(path)
	if err != nil {
		return f, nil, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, nil, err
	}
	if f.Version != 1 || f.Profile == "" || f.Source == "" {
		return f, nil, errors.New("fixture must be version 1 and include profile and source")
	}
	seen := map[string]bool{}
	for _, table := range f.Tables {
		if !validIdentifier(table.Table) || seen[table.Table] || len(table.Rows) == 0 {
			return f, nil, fmt.Errorf("fixture table %q is invalid, duplicate, or empty", table.Table)
		}
		if len(table.ConflictColumns) == 0 {
			return f, nil, fmt.Errorf("fixture table %q must declare conflict_columns", table.Table)
		}
		seen[table.Table] = true
	}
	return f, data, nil
}

func requireNonProductionDatabase(db *sql.DB, environment string) error {
	var databaseName string
	if err := db.QueryRow("SELECT current_database()").Scan(&databaseName); err != nil {
		return err
	}
	if !isNonProductionDatabaseName(databaseName) {
		return fmt.Errorf("database %q is not marked as development, test, sandbox, or local", databaseName)
	}
	return nil
}

func isNonProductionDatabaseName(name string) bool {
	name = strings.ToLower(name)
	for _, marker := range []string{"dev", "test", "sandbox", "local"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func requireMigratedSchema(db *sql.DB) error {
	var exists sql.NullString
	if err := db.QueryRow("SELECT to_regclass('public.schema_migrations')").Scan(&exists); err != nil {
		return err
	}
	if !exists.Valid || exists.String == "" {
		return errors.New("schema_migrations is absent; run migrations before dbseed")
	}
	var applied int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&applied); err != nil {
		return err
	}
	if applied == 0 {
		return errors.New("no migrations are recorded; run migrations before dbseed")
	}
	return nil
}

func discoverSchemaTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_type = 'BASE TABLE'
		  AND table_name <> 'schema_migrations'
		ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		result = append(result, table)
	}
	return result, rows.Err()
}

func makeCoverageReport(m manifest, f fixture, rawFixture []byte, environment string, dryRun bool, schemaTables []string) (coverageReport, map[string]tablePolicy) {
	policies := make(map[string]tablePolicy, len(m.Tables))
	modeCounts := map[string]int{"fixture": 0, "live": 0, "external": 0, "security": 0}
	for _, p := range m.Tables {
		policies[p.Table] = p
		modeCounts[p.Mode]++
	}
	schemaSet := map[string]bool{}
	for _, table := range schemaTables {
		schemaSet[table] = true
	}
	uncovered := make([]string, 0)
	stale := make([]string, 0)
	for _, table := range schemaTables {
		if _, ok := policies[table]; !ok {
			uncovered = append(uncovered, table)
		}
	}
	for table := range policies {
		if !schemaSet[table] {
			stale = append(stale, table)
		}
	}
	sort.Strings(uncovered)
	sort.Strings(stale)
	digest := sha256.Sum256(rawFixture)
	return coverageReport{
		GeneratedAt:        time.Now().UTC(),
		Environment:        environment,
		Profile:            f.Profile,
		FixtureSHA256:      fmt.Sprintf("%x", digest[:]),
		DryRun:             dryRun,
		SchemaTableCount:   len(schemaTables),
		ManifestTableCount: len(m.Tables),
		ModeCounts:         modeCounts,
		Seeded:             make([]tableResult, 0),
		UncoveredSchema:    uncovered,
		StaleManifest:      stale,
	}, policies
}

func validateFixtureTable(db *sql.DB, ft fixtureTable) error {
	if !validIdentifier(ft.Table) {
		return errors.New("invalid table identifier")
	}
	columns, err := schemaColumns(db, ft.Table)
	if err != nil {
		return err
	}
	for _, conflict := range ft.ConflictColumns {
		if !validIdentifier(conflict) || !columns[conflict] {
			return fmt.Errorf("conflict column %q is not a column of %s", conflict, ft.Table)
		}
	}
	for rowNumber, row := range ft.Rows {
		if len(row) == 0 {
			return fmt.Errorf("row %d is empty", rowNumber)
		}
		for column := range row {
			if !validIdentifier(column) || !columns[column] {
				return fmt.Errorf("row %d has unknown column %q", rowNumber, column)
			}
		}
		for _, conflict := range ft.ConflictColumns {
			if _, ok := row[conflict]; !ok {
				return fmt.Errorf("row %d does not include conflict column %q", rowNumber, conflict)
			}
		}
	}
	return nil
}

func schemaColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT column_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		columns[col] = true
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("table %q does not exist", table)
	}
	return columns, rows.Err()
}

func insertIdempotent(tx *sql.Tx, ft fixtureTable, row map[string]any) error {
	columns := make([]string, 0, len(row))
	for col := range row {
		columns = append(columns, col)
	}
	sort.Strings(columns)
	values := make([]any, 0, len(columns))
	placeholders := make([]string, 0, len(columns))
	quotedColumns := make([]string, 0, len(columns))
	for i, col := range columns {
		quotedColumns = append(quotedColumns, quoteIdentifier(col))
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		value, err := normalizeValue(row[col])
		if err != nil {
			return fmt.Errorf("column %s: %w", col, err)
		}
		values = append(values, value)
	}
	conflicts := make([]string, 0, len(ft.ConflictColumns))
	for _, col := range ft.ConflictColumns {
		conflicts = append(conflicts, quoteIdentifier(col))
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO NOTHING",
		quoteIdentifier(ft.Table), strings.Join(quotedColumns, ", "), strings.Join(placeholders, ", "), strings.Join(conflicts, ", "))
	_, err := tx.Exec(query, values...)
	return err
}

func normalizeValue(value any) (any, error) {
	switch v := value.(type) {
	case nil, string, bool, float64:
		return v, nil
	case map[string]any, []any:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	default:
		return nil, fmt.Errorf("unsupported JSON value type %T", value)
	}
}

func validIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if !(r == '_' || r >= 'a' && r <= 'z' || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func writeReport(path string, report coverageReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func fixtureRowCount(results []tableResult) int {
	total := 0
	for _, result := range results {
		total += result.Rows
	}
	return total
}
