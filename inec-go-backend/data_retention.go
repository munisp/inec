package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ── Retention worker (R4-57) ───────────────────────────────────────────────
// RunDataRetention previously had zero call sites — purge policies were
// defined but never executed. This wires it into startup as a daily ticker,
// following the fabric_anchor.go worker pattern.
//
// Failure modes:
//   - A failing policy is logged and skipped; other policies still run
//     (per-policy isolation inside RunDataRetention).
//   - RETENTION_WORKER_ENABLED=false disables the worker entirely.
//   - RETENTION_DRY_RUN=true logs what would be purged without deleting.
//
// R5-055 fixes:
//   - Policy timestamp columns now match the live schema (audit_log."timestamp",
//     stakeholder_incidents.reported_at) so retention is actually evaluated.
//   - "ArchiveFirst" performs a REAL export: expired rows are written to a
//     JSONL file under RETENTION_ARCHIVE_DIR, checksummed, and registered in
//     retention_archive_log BEFORE any delete. If the export fails, the delete
//     is skipped (fail-closed — data is never destroyed without a copy).
//   - Legal hold: audit_log and stakeholder_incidents carry a 7-year legal
//     hold (Electoral Act / NDPR). They are NEVER deleted by this worker —
//     only exported and registered (export-then-mark). Additional holds can be
//     placed via the legal_holds table (migration 000033).
//   - Deletes run in batches to avoid long table locks mid-election.
//
// R5-069: active_sessions default retention raised from 90 days to the audit
// window (2555d) — session rows are the only IP/device linkage for
// audit_log.user_id forensics; purging them at 90d made post-incident actor
// reconstruction impossible long before the audit window closed.

var (
	dataRetentionWorkerStartOnce sync.Once
	dataRetentionWorkerStopOnce  sync.Once
	dataRetentionStopChannel     chan struct{}
)

func dataRetentionWorkerEnabled() bool {
	return os.Getenv("RETENTION_WORKER_ENABLED") != "false"
}

func startDataRetentionWorker() {
	if !dataRetentionWorkerEnabled() {
		log.Info().Msg("data retention worker disabled (RETENTION_WORKER_ENABLED=false)")
		return
	}
	dataRetentionWorkerStartOnce.Do(func() {
		dataRetentionStopChannel = make(chan struct{})
		go func() {
			dryRun := os.Getenv("RETENTION_DRY_RUN") == "true"
			// First sweep shortly after startup, then daily.
			first := time.NewTimer(1 * time.Minute)
			defer first.Stop()
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-first.C:
					RunDataRetention(dryRun)
				case <-ticker.C:
					RunDataRetention(dryRun)
				case <-dataRetentionStopChannel:
					return
				}
			}
		}()
		log.Info().Bool("dry_run", os.Getenv("RETENTION_DRY_RUN") == "true").Msg("data retention worker started (daily)")
	})
}

func stopDataRetentionWorker() {
	dataRetentionWorkerStopOnce.Do(func() {
		if dataRetentionStopChannel != nil {
			close(dataRetentionStopChannel)
		}
	})
}

// DataRetentionPolicy defines purge rules for each data category.
type DataRetentionPolicy struct {
	Name          string
	Table         string
	TimestampCol  string
	RetentionDays int
	ArchiveFirst  bool // If true, export to JSONL file before purging
	LegalHold     bool // If true, NEVER delete — export-then-mark only
	Description   string
}

func defaultRetentionPolicies() []DataRetentionPolicy {
	biometricDays := envIntOr("RETENTION_BIOMETRIC_DAYS", 365)      // 1 year for biometric data
	auditDays := envIntOr("RETENTION_AUDIT_DAYS", 2555)             // 7 years for audit trail (legal)
	sessionDays := envIntOr("RETENTION_SESSION_DAYS", 2555)         // R5-069: align with 7yr audit window
	trackingDays := envIntOr("RETENTION_TRACKING_DAYS", 180)        // 6 months for GPS tracking
	geoEventDays := envIntOr("RETENTION_GEO_EVENT_DAYS", 365)       // 1 year for geo events
	notificationDays := envIntOr("RETENTION_NOTIFICATION_DAYS", 90) // 90 days for notifications
	jobLogDays := envIntOr("RETENTION_JOB_LOG_DAYS", 90)            // 90 days for background job logs
	crowdAlertDays := envIntOr("RETENTION_CROWD_ALERT_DAYS", 180)   // 6 months for crowd alerts
	incidentDays := envIntOr("RETENTION_INCIDENT_DAYS", 2555)       // 7 years for incidents (legal)

	return []DataRetentionPolicy{
		{Name: "biometric_verifications", Table: "biometric_verifications", TimestampCol: "verified_at", RetentionDays: biometricDays, ArchiveFirst: true, Description: "Biometric verification logs"},
		{Name: "active_sessions", Table: "active_sessions", TimestampCol: "created_at", RetentionDays: sessionDays, Description: "User sessions (actor context for audit trail — kept for the full audit window, R5-069)"},
		{Name: "official_tracking_history", Table: "official_tracking_history", TimestampCol: "recorded_at", RetentionDays: trackingDays, ArchiveFirst: true, Description: "Official GPS tracking history"},
		{Name: "geo_events", Table: "geo_events", TimestampCol: "created_at", RetentionDays: geoEventDays, Description: "Geospatial events"},
		{Name: "crowd_alerts", Table: "crowd_alerts", TimestampCol: "created_at", RetentionDays: crowdAlertDays, Description: "Crowd density alerts"},
		// R5-055: audit_log live column is "timestamp" (migrations/000021:56), NOT created_at.
		{Name: "audit_log", Table: "audit_log", TimestampCol: `"timestamp"`, RetentionDays: auditDays, ArchiveFirst: true, LegalHold: true, Description: "Blockchain audit trail (7yr legal hold — export-then-mark, never deleted)"},
		// R5-055: stakeholder_incidents live column is reported_at (migrations/000022:206), NOT created_at.
		{Name: "stakeholder_incidents", Table: "stakeholder_incidents", TimestampCol: "reported_at", RetentionDays: incidentDays, ArchiveFirst: true, LegalHold: true, Description: "Incident reports (7yr legal hold — export-then-mark, never deleted)"},
		{Name: "notification_log", Table: "notifications", TimestampCol: "created_at", RetentionDays: notificationDays, Description: "Push notifications"},
		{Name: "job_logs", Table: "background_jobs", TimestampCol: "created_at", RetentionDays: jobLogDays, Description: "Background job execution logs"},
	}
}

func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func retentionArchiveDir() string {
	if d := os.Getenv("RETENTION_ARCHIVE_DIR"); d != "" {
		return d
	}
	return "retention_archive"
}

// legalHoldActive reports whether a table is under an active legal hold —
// either statically in the policy or dynamically via the legal_holds table
// (migration 000033). A missing legal_holds table (pre-migration dev DBs) is
// treated as "no dynamic hold"; the static policy flag remains authoritative.
func legalHoldActive(p DataRetentionPolicy) bool {
	if p.LegalHold {
		return true
	}
	var n int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM legal_holds WHERE table_name = ? AND released_at IS NULL", p.Table,
	).Scan(&n)
	if err != nil {
		return false
	}
	return n > 0
}

// archiveExpiredRows performs the REAL archive step (R5-055): it exports every
// expired row to a JSONL file under RETENTION_ARCHIVE_DIR, computes a SHA-256
// of the export, and registers the export in retention_archive_log. It returns
// the number of rows exported. On any failure the caller must NOT delete.
func archiveExpiredRows(p DataRetentionPolicy, cutoff string, legalHold bool) (int, string, error) {
	rows, err := db.Query(fmt.Sprintf(
		"SELECT * FROM %s WHERE %s < ? ORDER BY id", p.Table, p.TimestampCol,
	), cutoff) // #nosec G201 -- table/column come from hardcoded policy literals
	if err != nil {
		return 0, "", fmt.Errorf("reading expired rows: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, "", fmt.Errorf("reading columns: %w", err)
	}

	dir := filepath.Join(retentionArchiveDir(), p.Table)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return 0, "", fmt.Errorf("creating archive dir: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(dir, fmt.Sprintf("%s_%s_%s.jsonl", p.Table, cutoff, stamp))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, "", fmt.Errorf("creating archive file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	count := 0
	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return count, path, fmt.Errorf("scanning row: %w", err)
		}
		rec := make(map[string]interface{}, len(cols))
		for i, c := range cols {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			rec[c] = v
		}
		line, err := json.Marshal(rec)
		if err != nil {
			return count, path, fmt.Errorf("marshalling row: %w", err)
		}
		line = append(line, '\n')
		if _, err := f.Write(line); err != nil {
			return count, path, fmt.Errorf("writing archive: %w", err)
		}
		h.Write(line)
		count++
	}
	if err := rows.Err(); err != nil {
		return count, path, fmt.Errorf("iterating rows: %w", err)
	}
	if err := f.Sync(); err != nil {
		return count, path, fmt.Errorf("syncing archive: %w", err)
	}
	sum := hex.EncodeToString(h.Sum(nil))

	holdFlag := 0
	if legalHold {
		holdFlag = 1
	}
	// Register the export; a registration failure is logged loudly but the
	// export file itself is the authoritative copy and already durable.
	if _, err := db.Exec(
		`INSERT INTO retention_archive_log (table_name, policy_name, cutoff_date, row_count, file_path, sha256, legal_hold) VALUES (?,?,?,?,?,?,?)`,
		p.Table, p.Name, cutoff, count, path, sum, holdFlag,
	); err != nil {
		log.Error().Err(err).Str("policy", p.Name).Str("file", path).
			Msg("archive export written but retention_archive_log registration failed")
	}
	log.Info().Str("policy", p.Name).Int("rows", count).Str("file", path).Str("sha256", sum).
		Bool("legal_hold", legalHold).Msg("Retention archive export completed")
	return count, path, nil
}

// purgeExpiredRows deletes expired rows in batches so a large purge cannot
// hold a table lock through an election-day write burst.
func purgeExpiredRows(p DataRetentionPolicy, cutoff string) (int64, error) {
	const batchSize = 500
	var total int64
	for {
		res, err := db.Exec(fmt.Sprintf(
			"DELETE FROM %s WHERE id IN (SELECT id FROM %s WHERE %s < ? LIMIT %d)",
			p.Table, p.Table, p.TimestampCol, batchSize,
		), cutoff) // #nosec G201 -- table/column come from hardcoded policy literals
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < batchSize {
			return total, nil
		}
	}
}

// RunDataRetention executes purge policies. Called by a cron or startup hook.
func RunDataRetention(dryRun bool) {
	policies := defaultRetentionPolicies()
	log.Info().Bool("dry_run", dryRun).Int("policies", len(policies)).Msg("Starting data retention sweep")

	for _, p := range policies {
		cutoff := time.Now().AddDate(0, 0, -p.RetentionDays).Format("2006-01-02")
		countQuery := fmt.Sprintf(
			"SELECT COUNT(*) FROM %s WHERE %s < ?",
			p.Table, p.TimestampCol,
		) // #nosec G201 -- table/column come from hardcoded policy literals
		var count int
		err := db.QueryRow(countQuery, cutoff).Scan(&count)
		if err != nil {
			// R5-055: a mis-keyed column must never again masquerade as "table
			// may not exist" — log the real error at error level so compliance
			// dashboards surface it.
			log.Error().Err(err).Str("policy", p.Name).Str("table", p.Table).Str("column", p.TimestampCol).
				Msg("RETENTION POLICY ERROR: expired-row count failed (check policy column mapping)")
			continue
		}
		if count == 0 {
			log.Debug().Str("policy", p.Name).Msg("No expired rows")
			continue
		}

		held := legalHoldActive(p)
		log.Info().Str("policy", p.Name).Int("expired_rows", count).Str("cutoff", cutoff).
			Int("retention_days", p.RetentionDays).Bool("dry_run", dryRun).Bool("legal_hold", held).
			Msg("Data retention check")

		if dryRun {
			continue
		}

		// Export first — for ArchiveFirst policies and for every legal-hold
		// table (export-then-mark: the archive + registry row are the mark).
		if p.ArchiveFirst || held {
			exported, _, aerr := archiveExpiredRows(p, cutoff, held)
			if aerr != nil {
				log.Error().Err(aerr).Str("policy", p.Name).
					Msg("Retention archive export FAILED — skipping delete (fail-closed: no data destroyed without a copy)")
				continue
			}
			if exported != count {
				log.Warn().Str("policy", p.Name).Int("counted", count).Int("exported", exported).
					Msg("Archive export row count differs from pre-count (concurrent writes)")
			}
		}

		if held {
			// LEGAL HOLD (R5-055): never DELETE audit_log / stakeholder_incidents
			// (or any table with an active legal_holds row). The export above is
			// the preservation copy; the rows themselves stay immutable.
			log.Info().Str("policy", p.Name).Str("table", p.Table).Int("expired_rows", count).
				Msg("LEGAL HOLD ACTIVE: expired rows exported and retained — deletion forbidden")
			continue
		}

		deleted, derr := purgeExpiredRows(p, cutoff)
		if derr != nil {
			log.Error().Err(derr).Str("policy", p.Name).Msg("Failed to purge expired rows")
			continue
		}
		log.Info().Str("policy", p.Name).Int64("deleted", deleted).Msg("Purged expired rows")
	}
}

// handleDataRetentionStatus returns the current retention configuration and row counts.
func handleDataRetentionStatus(w http.ResponseWriter, r *http.Request) {
	policies := defaultRetentionPolicies()
	results := make([]M, 0, len(policies))

	for _, p := range policies {
		cutoff := time.Now().AddDate(0, 0, -p.RetentionDays).Format("2006-01-02")
		var total, expired int
		var totalErr, expiredErr error
		totalErr = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", p.Table)).Scan(&total)          // #nosec G201 -- hardcoded table literal
		expiredErr = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s < ?", p.Table, p.TimestampCol), cutoff).Scan(&expired) // #nosec G201 -- hardcoded literals

		entry := M{
			"name":           p.Name,
			"table":          p.Table,
			"timestamp_col":  p.TimestampCol,
			"retention_days": p.RetentionDays,
			"archive_first":  p.ArchiveFirst,
			"legal_hold":     legalHoldActive(p),
			"cutoff_date":    cutoff,
			"description":    p.Description,
		}
		// R5-055: surface mapping errors instead of reporting false health.
		if totalErr != nil || expiredErr != nil {
			entry["status"] = "error"
			if totalErr != nil {
				entry["error"] = totalErr.Error()
			} else {
				entry["error"] = expiredErr.Error()
			}
		} else {
			entry["status"] = "ok"
			entry["total_rows"] = total
			entry["expired_rows"] = expired
		}
		results = append(results, entry)
	}

	writeJSON(w, 200, M{"policies": results})
}
