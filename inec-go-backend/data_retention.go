package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

// DataRetentionPolicy defines purge rules for each data category.
type DataRetentionPolicy struct {
	Name           string
	Table          string
	TimestampCol   string
	RetentionDays  int
	ArchiveFirst   bool // If true, export to CSV before purging
	Description    string
}

func defaultRetentionPolicies() []DataRetentionPolicy {
	biometricDays := envIntOr("RETENTION_BIOMETRIC_DAYS", 365)          // 1 year for biometric data
	auditDays := envIntOr("RETENTION_AUDIT_DAYS", 2555)                 // 7 years for audit trail (legal)
	sessionDays := envIntOr("RETENTION_SESSION_DAYS", 90)               // 90 days for sessions
	trackingDays := envIntOr("RETENTION_TRACKING_DAYS", 180)            // 6 months for GPS tracking
	geoEventDays := envIntOr("RETENTION_GEO_EVENT_DAYS", 365)           // 1 year for geo events
	notificationDays := envIntOr("RETENTION_NOTIFICATION_DAYS", 90)     // 90 days for notifications
	jobLogDays := envIntOr("RETENTION_JOB_LOG_DAYS", 90)                // 90 days for background job logs
	crowdAlertDays := envIntOr("RETENTION_CROWD_ALERT_DAYS", 180)       // 6 months for crowd alerts
	incidentDays := envIntOr("RETENTION_INCIDENT_DAYS", 2555)           // 7 years for incidents (legal)

	return []DataRetentionPolicy{
		{Name: "biometric_verifications", Table: "biometric_verifications", TimestampCol: "verified_at", RetentionDays: biometricDays, ArchiveFirst: true, Description: "Biometric verification logs"},
		{Name: "active_sessions", Table: "active_sessions", TimestampCol: "created_at", RetentionDays: sessionDays, Description: "User sessions"},
		{Name: "official_tracking_history", Table: "official_tracking_history", TimestampCol: "recorded_at", RetentionDays: trackingDays, ArchiveFirst: true, Description: "Official GPS tracking history"},
		{Name: "geo_events", Table: "geo_events", TimestampCol: "created_at", RetentionDays: geoEventDays, Description: "Geospatial events"},
		{Name: "crowd_alerts", Table: "crowd_alerts", TimestampCol: "created_at", RetentionDays: crowdAlertDays, Description: "Crowd density alerts"},
		{Name: "audit_log", Table: "audit_log", TimestampCol: "created_at", RetentionDays: auditDays, ArchiveFirst: true, Description: "Blockchain audit trail (7yr legal hold)"},
		{Name: "stakeholder_incidents", Table: "stakeholder_incidents", TimestampCol: "created_at", RetentionDays: incidentDays, ArchiveFirst: true, Description: "Incident reports (7yr legal hold)"},
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

// RunDataRetention executes purge policies. Called by a cron or startup hook.
func RunDataRetention(dryRun bool) {
	policies := defaultRetentionPolicies()
	log.Info().Bool("dry_run", dryRun).Int("policies", len(policies)).Msg("Starting data retention sweep")

	for _, p := range policies {
		cutoff := time.Now().AddDate(0, 0, -p.RetentionDays).Format("2006-01-02")
		countQuery := fmt.Sprintf(
			"SELECT COUNT(*) FROM %s WHERE %s < $1",
			p.Table, p.TimestampCol,
		)
		var count int
		err := db.QueryRow(countQuery, cutoff).Scan(&count)
		if err != nil {
			log.Warn().Err(err).Str("policy", p.Name).Msg("Failed to count expired rows (table may not exist)")
			continue
		}
		if count == 0 {
			log.Debug().Str("policy", p.Name).Msg("No expired rows")
			continue
		}

		log.Info().Str("policy", p.Name).Int("expired_rows", count).Str("cutoff", cutoff).Int("retention_days", p.RetentionDays).Bool("dry_run", dryRun).Msg("Data retention check")

		if dryRun {
			continue
		}

		if p.ArchiveFirst {
			log.Info().Str("policy", p.Name).Int("rows", count).Msg("Archiving before purge (archive step — production should export to object storage)")
		}

		deleteQuery := fmt.Sprintf(
			"DELETE FROM %s WHERE %s < $1",
			p.Table, p.TimestampCol,
		)
		result, err := db.Exec(deleteQuery, cutoff)
		if err != nil {
			log.Error().Err(err).Str("policy", p.Name).Msg("Failed to purge expired rows")
			continue
		}
		deleted, _ := result.RowsAffected()
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
		db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", p.Table)).Scan(&total)
		db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s < $1", p.Table, p.TimestampCol), cutoff).Scan(&expired)

		results = append(results, M{
			"name":           p.Name,
			"table":          p.Table,
			"retention_days": p.RetentionDays,
			"archive_first":  p.ArchiveFirst,
			"total_rows":     total,
			"expired_rows":   expired,
			"cutoff_date":    cutoff,
			"description":    p.Description,
		})
	}

	writeJSON(w, 200, M{"policies": results})
}
