package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
	"github.com/rs/zerolog/log"
)

// ═══════════════════════════════════════════════════════════════════════════════
// INEC Platform Enhancements — 25 Features
// Integrated with: Kafka, Dapr, Fluvio, Temporal, PostgreSQL, Keycloak,
// Permify, Redis, Mojaloop, OpenSearch, OpenAppSec, APISIX, TigerBeetle, Lakehouse
// ═══════════════════════════════════════════════════════════════════════════════

// ─── Types ───────────────────────────────────────────────────────────────────

type CommandCenterState struct {
	mu              sync.RWMutex
	escalationRules []EscalationRule
	loadShedLevel   int // 0=off, 1=shed LOW, 2=shed MEDIUM, 3=shed HIGH
}

// ── Multi-Channel Notification Dispatch ──

// NotificationChannel represents a dispatch channel (email, sms, webhook, push).
type NotificationChannel string

const (
	ChannelEmail   NotificationChannel = "email"
	ChannelSMS     NotificationChannel = "sms"
	ChannelWebhook NotificationChannel = "webhook"
	ChannelPush    NotificationChannel = "push"
)

// NotificationDispatchRequest defines a multi-channel notification payload.
type NotificationDispatchRequest struct {
	Title      string                `json:"title"`
	Body       string                `json:"body"`
	Channels   []NotificationChannel `json:"channels"`
	Recipients []string              `json:"recipients"`
	Priority   string                `json:"priority"`
	ElectionID int                   `json:"election_id"`
	Metadata   map[string]string     `json:"metadata,omitempty"`
}

// NotificationDispatchResult summarizes dispatch outcomes per channel.
type NotificationDispatchResult struct {
	Channel  NotificationChannel `json:"channel"`
	Status   string              `json:"status"`
	Sent     int                 `json:"sent"`
	Failed   int                 `json:"failed"`
	Duration string              `json:"duration"`
}

// dispatchNotification sends a notification through a single channel.
func dispatchNotification(ctx context.Context, channel NotificationChannel, req NotificationDispatchRequest) NotificationDispatchResult {
	t0 := time.Now()
	result := NotificationDispatchResult{Channel: channel, Status: "sent", Sent: len(req.Recipients)}

	switch channel {
	case ChannelEmail:
		// Dispatch via configured SMTP or email service (Dapr binding)
		emailURL := envOrDefault("EMAIL_SERVICE_URL", "")
		if emailURL != "" {
			body, _ := json.Marshal(map[string]interface{}{
				"to": req.Recipients, "subject": req.Title, "body": req.Body, "priority": req.Priority,
			})
			httpReq, _ := http.NewRequestWithContext(ctx, "POST", emailURL+"/send", bytes.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(httpReq)
			if err != nil {
				result.Status = "failed"
				result.Failed = len(req.Recipients)
				result.Sent = 0
			} else {
				resp.Body.Close()
			}
		} else {
			log.Info().Str("channel", "email").Int("recipients", len(req.Recipients)).Msg("email dispatch (no EMAIL_SERVICE_URL configured, logged only)")
		}

	case ChannelSMS:
		// Dispatch via configured SMS gateway
		smsURL := envOrDefault("SMS_GATEWAY_URL", "")
		if smsURL != "" {
			for _, recipient := range req.Recipients {
				body, _ := json.Marshal(map[string]interface{}{
					"to": recipient, "message": fmt.Sprintf("%s: %s", req.Title, req.Body),
				})
				httpReq, _ := http.NewRequestWithContext(ctx, "POST", smsURL+"/send", bytes.NewReader(body))
				httpReq.Header.Set("Content-Type", "application/json")
				client := &http.Client{Timeout: 10 * time.Second}
				resp, err := client.Do(httpReq)
				if err != nil {
					result.Failed++
					result.Sent--
				} else {
					resp.Body.Close()
				}
			}
		} else {
			log.Info().Str("channel", "sms").Int("recipients", len(req.Recipients)).Msg("sms dispatch (no SMS_GATEWAY_URL configured, logged only)")
		}

	case ChannelWebhook:
		// Dispatch to registered webhook URLs
		webhookURL := envOrDefault("WEBHOOK_DISPATCH_URL", "")
		if webhookURL != "" {
			body, _ := json.Marshal(map[string]interface{}{
				"event": "notification", "title": req.Title, "body": req.Body,
				"recipients": req.Recipients, "priority": req.Priority,
				"election_id": req.ElectionID, "metadata": req.Metadata,
				"timestamp": time.Now().UTC(),
			})
			httpReq, _ := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(httpReq)
			if err != nil {
				result.Status = "failed"
				result.Failed = len(req.Recipients)
				result.Sent = 0
			} else {
				resp.Body.Close()
			}
		} else {
			log.Info().Str("channel", "webhook").Msg("webhook dispatch (no WEBHOOK_DISPATCH_URL configured, logged only)")
		}

	case ChannelPush:
		// Use existing Kafka event bus for push notifications
		if mwHub != nil && mwHub.Kafka != nil {
			mwHub.Kafka.Produce(ctx, KafkaMessage{
				Topic: "inec.notifications.push",
				Key:   req.Priority,
				Value: map[string]interface{}{
					"title": req.Title, "body": req.Body,
					"recipients": req.Recipients, "priority": req.Priority,
				},
				Timestamp: time.Now(),
			})
		}
	}

	result.Duration = fmt.Sprintf("%.1fms", float64(time.Since(t0).Microseconds())/1000.0)
	return result
}

// handleNotificationDispatch sends notifications through multiple channels simultaneously.
func handleNotificationDispatch(w http.ResponseWriter, r *http.Request) {
	var req NotificationDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Title == "" || req.Body == "" {
		writeError(w, 400, "title and body required")
		return
	}
	if len(req.Channels) == 0 {
		req.Channels = []NotificationChannel{ChannelPush}
	}
	if len(req.Recipients) == 0 {
		req.Recipients = []string{"all"}
	}
	if req.Priority == "" {
		req.Priority = "normal"
	}

	// Dispatch to all channels concurrently
	var wg sync.WaitGroup
	results := make([]NotificationDispatchResult, len(req.Channels))
	for i, ch := range req.Channels {
		wg.Add(1)
		go func(idx int, channel NotificationChannel) {
			defer wg.Done()
			results[idx] = dispatchNotification(r.Context(), channel, req)
		}(i, ch)
	}
	wg.Wait()

	// Persist to DB
	channelsJSON, _ := json.Marshal(req.Channels)
	recipientsJSON, _ := json.Marshal(req.Recipients)
	resultsJSON, _ := json.Marshal(results)
	if _, err := dbExecCtx(r.Context(), `INSERT INTO push_notifications (title, body, recipients, channel, priority, election_id, status, sent_at) VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		req.Title, req.Body, string(recipientsJSON), string(channelsJSON), req.Priority, req.ElectionID, string(resultsJSON)); err != nil {
		writeError(w, 500, "failed to persist notification record")
		return
	}

	totalSent, totalFailed := 0, 0
	for _, res := range results {
		totalSent += res.Sent
		totalFailed += res.Failed
	}

	writeJSON(w, 201, M{
		"channels": results, "total_sent": totalSent, "total_failed": totalFailed,
		"message": "Notification dispatched across channels",
	})
}

type StateVelocity struct {
	StateCode     string  `json:"state_code"`
	StateName     string  `json:"state_name"`
	TotalPUs      int     `json:"total_pus"`
	ReportedPUs   int     `json:"reported_pus"`
	CompletionPct float64 `json:"completion_pct"`
	StalledPUs    int     `json:"stalled_pus"`
	ETAComplete   string  `json:"eta_complete"`
	Status        string  `json:"status"`
}

type EscalationRule struct {
	Name      string        `json:"name"`
	Condition string        `json:"condition"`
	Level     string        `json:"level"`
	Action    string        `json:"action"`
	Cooldown  time.Duration `json:"cooldown"`
	LastFired time.Time     `json:"-"`
}

var cmdCenter = &CommandCenterState{}

// ─── Schema Init ─────────────────────────────────────────────────────────────

func initPlatformEnhancements(database interface{}) {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS command_alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			level TEXT NOT NULL, state_code TEXT, message TEXT NOT NULL,
			auto_action TEXT, resolved INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS escalation_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			rule_name TEXT NOT NULL, level TEXT NOT NULL, state_code TEXT,
			action_taken TEXT, details TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS mfa_totp (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL UNIQUE, secret TEXT NOT NULL,
			verified INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS mfa_webauthn (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL, credential_id TEXT NOT NULL,
			public_key TEXT NOT NULL, sign_count INTEGER DEFAULT 0,
			device_name TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS mfa_sms_otp (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL, phone TEXT NOT NULL,
			code TEXT NOT NULL, expires_at TIMESTAMP NOT NULL,
			used INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS mfa_settings (
			user_id INTEGER PRIMARY KEY,
			totp_enabled INTEGER DEFAULT 0, webauthn_enabled INTEGER DEFAULT 0,
			sms_enabled INTEGER DEFAULT 0, enforce_on_write INTEGER DEFAULT 1,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS result_signatures (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			result_id INTEGER NOT NULL UNIQUE, officer_pubkey TEXT NOT NULL,
			signature TEXT NOT NULL, prev_hash TEXT,
			result_hash TEXT NOT NULL, chain_position INTEGER DEFAULT 0,
			signed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS citizen_verifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pu_code TEXT NOT NULL, ip_hash TEXT,
			verified_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS media_api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			api_key TEXT NOT NULL UNIQUE, org_name TEXT NOT NULL,
			contact_email TEXT, rate_limit INTEGER DEFAULT 600,
			is_active INTEGER DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS geofenced_submissions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			result_id INTEGER, officer_lat REAL NOT NULL, officer_lng REAL NOT NULL,
			pu_lat REAL, pu_lng REAL, distance_meters REAL,
			within_boundary INTEGER DEFAULT 0, override_by INTEGER,
			override_reason TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS anomaly_escalations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			anomaly_id TEXT, severity TEXT NOT NULL, state_code TEXT, pu_code TEXT,
			action_taken TEXT, escalated_to TEXT, collation_paused INTEGER DEFAULT 0,
			resolved INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS election_templates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			election_type TEXT NOT NULL, template_name TEXT NOT NULL,
			party_count INTEGER DEFAULT 18, form_fields TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS election_archive (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			election_id INTEGER NOT NULL, archived_data TEXT NOT NULL,
			checksum TEXT NOT NULL, is_immutable INTEGER DEFAULT 1,
			archived_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS data_classification (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			table_name TEXT NOT NULL, column_name TEXT NOT NULL,
			classification TEXT NOT NULL, residency_zone TEXT DEFAULT 'NG',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(table_name, column_name))`,
		`CREATE TABLE IF NOT EXISTS observer_photo_verifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			observer_id INTEGER, pu_code TEXT, photo_hash TEXT,
			gps_lat REAL, gps_lng REAL, timestamp_watermark TEXT,
			consensus_score REAL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS predictive_analytics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			election_id INTEGER, state_code TEXT,
			predicted_turnout REAL, confidence REAL DEFAULT 0.8,
			model_version TEXT DEFAULT 'v1',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS biometric_quality_scores (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			capture_id TEXT, modality TEXT,
			blur_score REAL, exposure_score REAL, angle_score REAL,
			overall_quality REAL, guidance TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
	}
	for _, ddl := range tables {
		dbExecLog("enhancements", ddl)
	}

	// R5-054: bring SQLite dev databases to the production column set for
	// result_signatures (PostgreSQL gains these via migration 000024). ALTERs
	// are idempotent — "duplicate column" errors are expected and ignored.
	for _, col := range []string{
		`ALTER TABLE result_signatures ADD COLUMN algorithm TEXT`,
		`ALTER TABLE result_signatures ADD COLUMN signature_hash TEXT`,
		`ALTER TABLE result_signatures ADD COLUMN signer_id INTEGER`,
		`ALTER TABLE result_signatures ADD COLUMN signer_role TEXT`,
		`ALTER TABLE result_signatures ADD COLUMN verification_status TEXT`,
	} {
		if _, err := db.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			log.Warn().Err(err).Msg("result_signatures column upgrade skipped")
		}
	}
	// R5-054: append-only parity for SQLite dev/test (PostgreSQL enforces the
	// same via triggers in migration 000033). Executed standalone because
	// trigger syntax does not survive execMulti's semicolon splitter.
	if !usePostgres {
		for _, stmt := range []string{
			`CREATE TRIGGER IF NOT EXISTS trg_result_signatures_no_update BEFORE UPDATE ON result_signatures BEGIN SELECT RAISE(ABORT, 'result_signatures is append-only'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_result_signatures_no_delete BEFORE DELETE ON result_signatures BEGIN SELECT RAISE(ABORT, 'result_signatures is append-only'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_audit_log_no_update BEFORE UPDATE ON audit_log BEGIN SELECT RAISE(ABORT, 'audit_log is append-only (7-year legal hold)'); END`,
			`CREATE TRIGGER IF NOT EXISTS trg_audit_log_no_delete BEFORE DELETE ON audit_log BEGIN SELECT RAISE(ABORT, 'audit_log is append-only (7-year legal hold)'); END`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				log.Warn().Err(err).Msg("failed to install SQLite append-only trigger")
			}
		}
	}

	// Seed election templates
	dbExecLog("seed", `INSERT OR IGNORE INTO election_templates (election_type, template_name, party_count, form_fields) VALUES
		('presidential', 'Presidential Election EC8A', 18, '{"fields":["accredited","votes_cast","rejected_ballots"]}'),
		('gubernatorial', 'Gubernatorial Election EC8A', 12, '{"fields":["accredited","votes_cast","rejected_ballots"]}'),
		('senatorial', 'Senatorial Election EC8A', 10, '{"fields":["accredited","votes_cast"]}'),
		('house_of_reps', 'House of Reps EC8A', 10, '{"fields":["accredited","votes_cast"]}'),
		('state_assembly', 'State Assembly EC8A', 8, '{"fields":["accredited","votes_cast"]}'),
		('local_government', 'Local Government EC8A', 5, '{"fields":["accredited","votes_cast"]}')`)

	// Seed data classifications
	dbExecLog("seed", `INSERT OR IGNORE INTO data_classification (table_name, column_name, classification) VALUES
		('voters', 'full_name', 'CONFIDENTIAL'), ('voters', 'phone', 'SECRET'),
		('voters', 'date_of_birth', 'CONFIDENTIAL'), ('voters', 'vin', 'INTERNAL'),
		('biometric_profiles', 'template_data', 'SECRET'), ('biometric_profiles', 'face_template', 'SECRET'),
		('results', 'total_votes', 'PUBLIC'), ('results', 'accredited_voters', 'PUBLIC'),
		('results', 'polling_unit_code', 'PUBLIC'), ('elections', 'title', 'PUBLIC'),
		('audit_log', 'details', 'INTERNAL'), ('users', 'password_hash', 'SECRET')`)

	// Persist escalation rules in DB
	dbExecLog("enhancements", `CREATE TABLE IF NOT EXISTS escalation_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE, condition TEXT NOT NULL,
		level TEXT NOT NULL, action TEXT NOT NULL,
		cooldown_seconds INTEGER DEFAULT 300,
		enabled INTEGER DEFAULT 1,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`)
	dbExecLog("seed", `INSERT OR IGNORE INTO escalation_rules (name, condition, level, action, cooldown_seconds) VALUES
		('stalled_pu_warn', 'stalled>100', 'WARN', 'notify_state_rec', 300),
		('stalled_pu_critical', 'stalled>500', 'CRITICAL', 'pause_collation', 600),
		('zero_submissions', 'no_submissions_30m', 'EMERGENCY', 'notify_chairman', 1800)`)

	// Persist load shedding level
	dbExecLog("enhancements", `CREATE TABLE IF NOT EXISTS command_center_config (
		key TEXT PRIMARY KEY, value TEXT NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`)
	dbExecLog("seed", `INSERT OR IGNORE INTO command_center_config (key, value) VALUES ('load_shedding_level', '0')`)

	// Load escalation rules from DB
	loadEscalationRulesFromDB()
	loadLoadSheddingFromDB()

	log.Info().Msg("Platform enhancements initialized (25 features)")
}

func loadEscalationRulesFromDB() {
	rows, err := db.Query(`SELECT name, condition, level, action, cooldown_seconds FROM escalation_rules WHERE enabled=1`)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load escalation rules from DB, using defaults")
		cmdCenter.escalationRules = []EscalationRule{
			{Name: "stalled_pu_warn", Condition: "stalled>100", Level: "WARN", Action: "notify_state_rec", Cooldown: 5 * time.Minute},
			{Name: "stalled_pu_critical", Condition: "stalled>500", Level: "CRITICAL", Action: "pause_collation", Cooldown: 10 * time.Minute},
			{Name: "zero_submissions", Condition: "no_submissions_30m", Level: "EMERGENCY", Action: "notify_chairman", Cooldown: 30 * time.Minute},
		}
		return
	}
	defer rows.Close()
	var rules []EscalationRule
	for rows.Next() {
		var r EscalationRule
		var cooldownSec int
		if err := rows.Scan(&r.Name, &r.Condition, &r.Level, &r.Action, &cooldownSec); err != nil {
			continue
		}
		r.Cooldown = time.Duration(cooldownSec) * time.Second
		rules = append(rules, r)
	}
	if len(rules) > 0 {
		cmdCenter.mu.Lock()
		cmdCenter.escalationRules = rules
		cmdCenter.mu.Unlock()
		log.Info().Int("count", len(rules)).Msg("Escalation rules loaded from DB")
	} else {
		cmdCenter.escalationRules = []EscalationRule{
			{Name: "stalled_pu_warn", Condition: "stalled>100", Level: "WARN", Action: "notify_state_rec", Cooldown: 5 * time.Minute},
			{Name: "stalled_pu_critical", Condition: "stalled>500", Level: "CRITICAL", Action: "pause_collation", Cooldown: 10 * time.Minute},
			{Name: "zero_submissions", Condition: "no_submissions_30m", Level: "EMERGENCY", Action: "notify_chairman", Cooldown: 30 * time.Minute},
		}
	}
}

func loadLoadSheddingFromDB() {
	row := db.QueryRow(`SELECT value FROM command_center_config WHERE key='load_shedding_level'`)
	var val string
	if err := row.Scan(&val); err == nil {
		if l, err := strconv.Atoi(val); err == nil && l >= 0 && l <= 3 {
			cmdCenter.loadShedLevel = l
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// #1: Real-Time Election Command Center
// ═══════════════════════════════════════════════════════════════════════════════

func handleCommandCenterLive(w http.ResponseWriter, r *http.Request) {
	states := computeStateVelocities(r.Context())
	alerts := getActiveAlerts(r.Context())
	newAlerts := runEscalationChecks(states)
	alerts = append(alerts, newAlerts...)

	totalPUs, reportedPUs, stalledPUs := 0, 0, 0
	for _, s := range states {
		totalPUs += s.TotalPUs
		reportedPUs += s.ReportedPUs
		stalledPUs += s.StalledPUs
	}
	pct := 0.0
	if totalPUs > 0 {
		pct = float64(reportedPUs) / float64(totalPUs) * 100
	}
	writeJSON(w, 200, M{
		"timestamp": time.Now().UTC(), "states": states, "alerts": alerts,
		"overall_pus": totalPUs, "reported_pus": reportedPUs,
		"stalled_pus": stalledPUs, "completion_pct": pct,
		"load_shedding": cmdCenter.loadShedLevel,
	})
}

func handleCommandCenterSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ctx := r.Context()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			states := computeStateVelocities(ctx)
			data, _ := json.Marshal(M{"states": states, "timestamp": time.Now().UTC()})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func handleCommandCenterAlerts(w http.ResponseWriter, r *http.Request) {
	rows, err := dbQueryCtx(r.Context(), `SELECT id, level, COALESCE(state_code,'') as state_code, message, COALESCE(auto_action,'') as auto_action, resolved, created_at FROM command_alerts ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		writeError(w, 500, "query error")
		return
	}
	writeJSON(w, 200, M{"alerts": scanRows(rows)})
}

func handleEscalationConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		cmdCenter.mu.RLock()
		defer cmdCenter.mu.RUnlock()
		writeJSON(w, 200, M{"rules": cmdCenter.escalationRules})
		return
	}
	var rules []EscalationRule
	if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
		writeError(w, 400, "invalid rules")
		return
	}
	cmdCenter.mu.Lock()
	cmdCenter.escalationRules = rules
	cmdCenter.mu.Unlock()
	// Persist to DB
	for _, rule := range rules {
		cooldownSec := int(rule.Cooldown.Seconds())
		if cooldownSec == 0 {
			cooldownSec = 300
		}
		if _, err := dbExecCtx(r.Context(), `INSERT OR REPLACE INTO escalation_rules (name, condition, level, action, cooldown_seconds) VALUES (?,?,?,?,?)`,
			rule.Name, rule.Condition, rule.Level, rule.Action, cooldownSec); err != nil {
			writeError(w, 500, "failed to persist escalation rules")
			return
		}
	}
	writeJSON(w, 200, M{"status": "updated", "count": len(rules)})
}

// ═══════════════════════════════════════════════════════════════════════════════
// #25: Adaptive Load Shedding
// ═══════════════════════════════════════════════════════════════════════════════

func handleLoadShedding(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		writeJSON(w, 200, M{"level": cmdCenter.loadShedLevel, "descriptions": M{
			"0": "off", "1": "shed analytics", "2": "shed all reads", "3": "submissions only",
		}})
		return
	}
	var body struct {
		Level int `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Level < 0 || body.Level > 3 {
		writeError(w, 400, "level must be 0-3")
		return
	}
	cmdCenter.loadShedLevel = body.Level
	// Persist to DB
	if _, err := dbExecCtx(r.Context(), `INSERT OR REPLACE INTO command_center_config (key, value, updated_at) VALUES ('load_shedding_level', ?, CURRENT_TIMESTAMP)`, strconv.Itoa(body.Level)); err != nil {
		writeError(w, 500, "failed to persist load shedding level")
		return
	}
	if mwHub != nil && mwHub.Kafka != nil {
		mwHub.Kafka.Produce(r.Context(), KafkaMessage{Topic: "command-center.load-shedding", Key: "level", Value: M{"level": body.Level}, Timestamp: time.Now()})
	}
	writeJSON(w, 200, M{"status": "updated", "level": body.Level})
}

func loadSheddingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		level := cmdCenter.loadShedLevel
		if level == 0 {
			next.ServeHTTP(w, r)
			return
		}
		prio := classifyPriority(r.URL.Path)
		if prio == "SYSTEM" {
			next.ServeHTTP(w, r)
			return
		}
		if level >= 3 && prio != "CRITICAL" {
			writeError(w, 503, "only result submissions accepted")
			return
		}
		if level >= 2 && (prio == "LOW" || prio == "MEDIUM") {
			writeError(w, 503, "non-essential requests paused")
			return
		}
		if level >= 1 && prio == "LOW" {
			writeError(w, 503, "analytics paused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func classifyPriority(path string) string {
	if path == "/healthz" || path == "/readiness" || strings.HasPrefix(path, "/auth/") || path == "/load-shedding" || path == "/metrics" {
		return "SYSTEM"
	}
	if strings.Contains(path, "/results/submit") || strings.Contains(path, "/ec8a/submit") || strings.Contains(path, "/ingestion/submit") {
		return "CRITICAL"
	}
	if strings.Contains(path, "/collation") || strings.Contains(path, "/observer/") || strings.Contains(path, "/biometric/") {
		return "HIGH"
	}
	if strings.Contains(path, "/dashboard/") || strings.Contains(path, "/elections") {
		return "MEDIUM"
	}
	return "LOW"
}

// ═══════════════════════════════════════════════════════════════════════════════
// #3: Multi-Factor Authentication (TOTP + WebAuthn + SMS OTP)
// ═══════════════════════════════════════════════════════════════════════════════

func handleMFASetupTOTP(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	if userID == 0 {
		writeError(w, 401, "auth required")
		return
	}
	secretBytes := make([]byte, 20)
	if err := fillRandom(secretBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "secure random source is unavailable")
		return
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secretBytes)
	if _, err := dbExecCtx(r.Context(), `INSERT OR REPLACE INTO mfa_totp (user_id, secret, verified) VALUES (?,?,0)`, userID, secret); err != nil {
		writeError(w, 500, "failed to store MFA secret")
		return
	}

	row, _ := querySingleRowCtx(r.Context(), `SELECT username FROM users WHERE id=?`, userID)
	username := "officer"
	if row != nil {
		username = fmt.Sprint(row["username"])
	}
	otpauth := fmt.Sprintf("otpauth://totp/INEC:%s?secret=%s&issuer=INEC&digits=6&period=30", username, secret)
	writeJSON(w, 200, M{"secret": secret, "otpauth_uri": otpauth})
}

func handleMFAVerifyTOTP(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Code) != 6 {
		writeError(w, 400, "6-digit code required")
		return
	}
	row, err := querySingleRowCtx(r.Context(), `SELECT secret FROM mfa_totp WHERE user_id=?`, userID)
	if err != nil {
		writeError(w, 404, "TOTP not set up")
		return
	}
	if !validateTOTP(fmt.Sprint(row["secret"]), body.Code) {
		writeError(w, 401, "invalid TOTP code")
		return
	}
	if _, err := dbExecCtx(r.Context(), `UPDATE mfa_totp SET verified=1 WHERE user_id=?`, userID); err != nil {
		writeError(w, 500, "failed to enable TOTP")
		return
	}
	if _, err := dbExecCtx(r.Context(), `INSERT OR REPLACE INTO mfa_settings (user_id, totp_enabled, updated_at) VALUES (?,1,CURRENT_TIMESTAMP)`, userID); err != nil {
		writeError(w, 500, "failed to update MFA settings")
		return
	}
	writeJSON(w, 200, M{"status": "totp_enabled"})
}

func handleMFAChallenge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID int    `json:"user_id"`
		Code   string `json:"code"`
		Method string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	switch body.Method {
	case "totp":
		row, err := querySingleRowCtx(r.Context(), `SELECT secret FROM mfa_totp WHERE user_id=? AND verified=1`, body.UserID)
		if err != nil {
			writeError(w, 404, "TOTP not configured")
			return
		}
		if !validateTOTP(fmt.Sprint(row["secret"]), body.Code) {
			writeError(w, 401, "invalid code")
			return
		}
	case "sms":
		row, err := querySingleRowCtx(r.Context(), `SELECT code FROM mfa_sms_otp WHERE user_id=? AND used=0 ORDER BY created_at DESC LIMIT 1`, body.UserID)
		if err != nil || fmt.Sprint(row["code"]) != body.Code {
			writeError(w, 401, "invalid SMS OTP")
			return
		}
		if _, err := dbExecCtx(r.Context(), `UPDATE mfa_sms_otp SET used=1 WHERE user_id=? AND code=?`, body.UserID, body.Code); err != nil {
			writeError(w, 500, "failed to consume SMS OTP")
			return
		}
	default:
		writeError(w, 400, "method must be totp or sms")
		return
	}
	writeJSON(w, 200, M{"status": "verified", "method": body.Method, "mfa_passed": true})
}

func handleMFASendSMS(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID int `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "user_id required")
		return
	}
	row, err := querySingleRowCtx(r.Context(), `SELECT phone FROM users WHERE id=?`, body.UserID)
	if err != nil {
		writeError(w, 404, "user not found")
		return
	}
	phone := fmt.Sprint(row["phone"])
	if phone == "" || phone == "<nil>" {
		writeError(w, 400, "no phone registered")
		return
	}
	codeBytes := make([]byte, 3)
	if err := fillRandom(codeBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "secure random source is unavailable")
		return
	}
	code := fmt.Sprintf("%06d", (int(codeBytes[0])<<16|int(codeBytes[1])<<8|int(codeBytes[2]))%1000000)
	if _, err := dbExecCtx(r.Context(), `INSERT INTO mfa_sms_otp (user_id, phone, code, expires_at) VALUES (?,?,?,?)`,
		body.UserID, phone, code, time.Now().Add(5*time.Minute)); err != nil {
		writeError(w, 500, "failed to store SMS OTP")
		return
	}
	if mwHub != nil && mwHub.Dapr != nil {
		mwHub.Dapr.PublishEvent(r.Context(), "sms-gateway", "send", map[string]string{"to": phone, "message": "INEC MFA code: " + code})
	}
	writeJSON(w, 200, M{"status": "sent", "phone_mask": phoneMask(phone), "expires_in": "5m"})
}

func handleMFAStatus(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	row, _ := querySingleRowCtx(r.Context(), `SELECT totp_enabled, webauthn_enabled, sms_enabled FROM mfa_settings WHERE user_id=?`, userID)
	if row == nil {
		writeJSON(w, 200, M{"mfa_enabled": false, "totp": false, "webauthn": false, "sms": false})
		return
	}
	writeJSON(w, 200, M{
		"mfa_enabled": row["totp_enabled"] == int64(1) || row["sms_enabled"] == int64(1),
		"totp":        row["totp_enabled"] == int64(1), "webauthn": row["webauthn_enabled"] == int64(1),
		"sms": row["sms_enabled"] == int64(1),
	})
}

func handleMFAWebAuthnRegister(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	var body struct {
		CredentialID string `json:"credential_id"`
		PublicKey    string `json:"public_key"`
		DeviceName   string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	if _, err := dbExecCtx(r.Context(), `INSERT INTO mfa_webauthn (user_id, credential_id, public_key, device_name) VALUES (?,?,?,?)`,
		userID, body.CredentialID, body.PublicKey, body.DeviceName); err != nil {
		writeError(w, 500, "failed to register WebAuthn credential")
		return
	}
	if _, err := dbExecCtx(r.Context(), `INSERT OR REPLACE INTO mfa_settings (user_id, webauthn_enabled, updated_at) VALUES (?,1,CURRENT_TIMESTAMP)`, userID); err != nil {
		writeError(w, 500, "failed to update MFA settings")
		return
	}
	writeJSON(w, 200, M{"status": "registered", "device": body.DeviceName})
}

// ═══════════════════════════════════════════════════════════════════════════════
// #4 & #6: Cryptographic Result Chain + Citizen Verification Portal
// ═══════════════════════════════════════════════════════════════════════════════

func handleCitizenVerify(w http.ResponseWriter, r *http.Request) {
	puCode := r.URL.Query().Get("pu_code")
	state := r.URL.Query().Get("state")
	lga := r.URL.Query().Get("lga")
	if puCode == "" && state == "" && lga == "" {
		writeError(w, 400, "provide pu_code, state, or lga")
		return
	}
	var query string
	var args []interface{}
	if puCode != "" {
		query = `SELECT r.id, r.election_id, r.polling_unit_code, r.accredited_voters, r.total_votes, r.status, r.submitted_at, pu.pu_name, pu.state_code FROM results r LEFT JOIN polling_units pu ON r.polling_unit_code=pu.pu_code WHERE r.polling_unit_code=? ORDER BY r.submitted_at DESC LIMIT 1`
		args = []interface{}{puCode}
	} else if lga != "" {
		query = `SELECT r.id, r.polling_unit_code, r.total_votes, r.status, pu.pu_name, pu.state_code FROM results r LEFT JOIN polling_units pu ON r.polling_unit_code=pu.pu_code WHERE pu.lga_code=? LIMIT 50`
		args = []interface{}{lga}
	} else {
		query = `SELECT r.id, r.polling_unit_code, r.total_votes, r.status, pu.pu_name FROM results r LEFT JOIN polling_units pu ON r.polling_unit_code=pu.pu_code WHERE pu.state_code=? LIMIT 100`
		args = []interface{}{state}
	}
	rows, err := dbQueryCtx(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, "database error")
		return
	}
	results := scanRows(rows)
	for _, result := range results {
		rid := fmt.Sprint(result["id"])
		ps, _ := dbQueryCtx(r.Context(), `SELECT party_code, votes FROM party_scores WHERE result_id=? ORDER BY votes DESC`, rid)
		if ps != nil {
			result["party_scores"] = scanRows(ps)
		}
	}
	ipHash := sha256.Sum256([]byte(r.RemoteAddr))
	key := puCode
	if key == "" {
		key = state + ":" + lga
	}
	dbExecCtx(r.Context(), `INSERT INTO citizen_verifications (pu_code, ip_hash) VALUES (?,?)`, key, hex.EncodeToString(ipHash[:8]))
	writeJSON(w, 200, M{"results": results, "count": len(results), "verified_at": time.Now().UTC(), "source": "INEC Official"})
}

// ── Result signing (R5-054): real ed25519 asymmetric signatures ─────────────
//
// The previous implementation stored sha256(resultHash:prevHash:unixTime:
// caller_supplied_officer_pubkey) — every input public, no private key — and
// INSERT OR REPLACE silently overwrote history. Signatures are now produced
// with an ed25519 private key held only by the server (RESULT_SIGNING_KEY,
// hex-encoded 32-byte seed or 64-byte private key, expected from an HSM/KMS in
// production). Records are append-only (UNIQUE(result_id) + no-update/no-delete
// triggers, migration 000033) and the verify endpoint exposes the signer
// identity and re-verifies the signature cryptographically.

var (
	resultSigningKeyOnce sync.Once
	resultSigningPriv    ed25519.PrivateKey
	resultSigningPubHex  string
	// resultSignChainMu serializes prev-hash read + insert so concurrent
	// signing cannot fork the signature chain in-process; UNIQUE(chain_position)
	// (migration 000034) enforces the same across replicas.
	resultSignChainMu sync.Mutex
)

func resultSigningKey() (ed25519.PrivateKey, string) {
	resultSigningKeyOnce.Do(func() {
		keyHex := strings.TrimSpace(os.Getenv("RESULT_SIGNING_KEY"))
		if keyHex == "" {
			if isProductionLike() {
				log.Fatal().Msg("RESULT_SIGNING_KEY must be set in production/staging (hex-encoded ed25519 seed or private key from HSM/KMS)")
			}
			// Dev-only: deterministic key for local development, mirroring the
			// BIOMETRIC_VAULT_MASTER_KEY fail-closed pattern.
			log.Warn().Msg("RESULT_SIGNING_KEY not set — using dev-only deterministic signing key (NOT FOR PRODUCTION)")
			seed := sha256.Sum256([]byte("DEV-ONLY-RESULT-SIGNING-KEY-NOT-FOR-PRODUCTION"))
			resultSigningPriv = ed25519.NewKeyFromSeed(seed[:])
		} else {
			raw, err := hex.DecodeString(keyHex)
			if err != nil || (len(raw) != ed25519.SeedSize && len(raw) != ed25519.PrivateKeySize) {
				log.Fatal().Msg("RESULT_SIGNING_KEY must be a hex-encoded ed25519 seed (64 hex chars) or private key (128 hex chars)")
			}
			if len(raw) == ed25519.SeedSize {
				resultSigningPriv = ed25519.NewKeyFromSeed(raw)
			} else {
				resultSigningPriv = ed25519.PrivateKey(raw)
			}
		}
		resultSigningPubHex = hex.EncodeToString(resultSigningPriv.Public().(ed25519.PublicKey))
	})
	return resultSigningPriv, resultSigningPubHex
}

// resultSignaturePreimage is the exact, canonical byte string that is signed.
// It uses only values stored verbatim in result_signatures so verification can
// recompute it without ambiguity.
func resultSignaturePreimage(resultHash, prevHash string, signerID int) string {
	return fmt.Sprintf("inec-result-signature:v1:%s:%s:%d", resultHash, prevHash, signerID)
}

func enhStr(m M, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
		return true
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "duplicate key")
}

func handleCitizenVerifySignature(w http.ResponseWriter, r *http.Request) {
	rid := r.URL.Query().Get("result_id")
	if rid == "" {
		writeError(w, 400, "result_id required")
		return
	}
	result, err := querySingleRowCtx(r.Context(), `SELECT * FROM results WHERE id=?`, rid)
	if err != nil {
		writeError(w, 404, "result not found")
		return
	}
	sig, err := querySingleRowCtx(r.Context(), `SELECT * FROM result_signatures WHERE result_id=?`, rid)
	if err != nil {
		writeJSON(w, 200, M{"result": result, "signed": false})
		return
	}
	currentHash := computeResultHash(result)

	// R5-054: cryptographically re-verify the ed25519 signature against the
	// stored preimage and expose the signer identity.
	_, pubHex := resultSigningKey()
	signatureValid := false
	if sigBytes, derr := hex.DecodeString(enhStr(sig, "signature")); derr == nil {
		if pubBytes, perr := hex.DecodeString(enhStr(sig, "officer_pubkey")); perr == nil && len(pubBytes) == ed25519.PublicKeySize {
			preimage := resultSignaturePreimage(enhStr(sig, "result_hash"), enhStr(sig, "prev_hash"), enhToInt(sig["signer_id"]))
			signatureValid = ed25519.Verify(ed25519.PublicKey(pubBytes), []byte(preimage), sigBytes)
		}
	}
	writeJSON(w, 200, M{
		"result": result, "signed": true, "signature": sig,
		"hash_valid":       currentHash == enhStr(sig, "result_hash"),
		"signature_valid":  signatureValid,
		"algorithm":        "ed25519",
		"signer_id":        enhToInt(sig["signer_id"]),
		"signer_role":      enhStr(sig, "signer_role"),
		"signer_pubkey":    enhStr(sig, "officer_pubkey"),
		"server_pubkey":    pubHex,
		"tamper_detected":  currentHash != enhStr(sig, "result_hash") || !signatureValid,
	})
}

func handleSignResult(w http.ResponseWriter, r *http.Request) {
	// The route is already writeAuth-gated; requireRole additionally binds the
	// signer identity to the authenticated officer for non-repudiation.
	claims, err := requireRole(r, "admin", "presiding_officer", "collation_officer")
	if err != nil {
		writeError(w, 403, "signing requires an authenticated election officer")
		return
	}
	signerID := extractUserID(r)
	signerRole, _ := claims["role"].(string)

	var body struct {
		ResultID int `json:"result_id"`
		// OfficerKey is accepted for backward compatibility but ignored — the
		// signature is produced with the server-held ed25519 key, never with
		// caller-supplied material.
		OfficerKey string `json:"officer_pubkey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	result, err := querySingleRowCtx(r.Context(), `SELECT * FROM results WHERE id=?`, body.ResultID)
	if err != nil {
		writeError(w, 404, "result not found")
		return
	}
	priv, pubHex := resultSigningKey()

	// Serialize prev-hash read + insert so concurrent signers cannot fork the
	// chain (cross-replica safety: UNIQUE(chain_position), migration 000034).
	resultSignChainMu.Lock()
	defer resultSignChainMu.Unlock()

	prevRow, _ := querySingleRowCtx(r.Context(), `SELECT result_hash, chain_position FROM result_signatures ORDER BY chain_position DESC LIMIT 1`)
	prevHash := ""
	chainPos := 0
	if prevRow != nil {
		prevHash = enhStr(prevRow, "result_hash")
		chainPos = enhToInt(prevRow["chain_position"]) + 1
	}
	resultHash := computeResultHash(result)
	sig := ed25519.Sign(priv, []byte(resultSignaturePreimage(resultHash, prevHash, signerID)))
	sigHex := hex.EncodeToString(sig)
	sigDigest := sha256.Sum256(sig)

	// Append-only: a plain INSERT. A second signature for the same result is
	// rejected (409) — history is never overwritten.
	if _, err := dbExecCtx(r.Context(), `INSERT INTO result_signatures
		(result_id, officer_pubkey, signature, prev_hash, result_hash, chain_position, algorithm, signature_hash, signer_id, signer_role, verification_status)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		body.ResultID, pubHex, sigHex, prevHash, resultHash, chainPos,
		"ed25519", hex.EncodeToString(sigDigest[:]), signerID, signerRole, "verified"); err != nil {
		if isUniqueViolation(err) {
			writeError(w, 409, "result already signed — signature records are append-only and cannot be overwritten")
			return
		}
		log.Error().Err(err).Int("result_id", body.ResultID).Msg("failed to persist result signature")
		writeError(w, 500, "failed to persist result signature")
		return
	}
	if mwHub != nil && mwHub.Kafka != nil {
		if perr := mwHub.Kafka.Produce(r.Context(), KafkaMessage{Topic: "result-chain.signed", Key: fmt.Sprint(body.ResultID), Value: M{"result_id": body.ResultID, "hash": resultHash, "chain_position": chainPos, "signer_id": signerID}, Timestamp: time.Now()}); perr != nil {
			log.Error().Err(perr).Int("result_id", body.ResultID).Msg("SECURITY: result-signature event publish failed")
		}
	}
	// TigerBeetle is intentionally not used for result-signature activity. It is
	// reserved for independently approved device logistics/reimbursement commitments
	// and must not carry result, voter, ballot, or electoral outcome information.
	writeJSON(w, 200, M{"status": "signed", "signature": sigHex, "result_hash": resultHash, "chain_position": chainPos,
		"algorithm": "ed25519", "signer_id": signerID, "signer_pubkey": pubHex})
}

func handleResultQRData(w http.ResponseWriter, r *http.Request) {
	rid := r.URL.Query().Get("result_id")
	if rid == "" {
		writeError(w, 400, "result_id required")
		return
	}
	sig, err := querySingleRowCtx(r.Context(), `SELECT result_hash, chain_position FROM result_signatures WHERE result_id=?`, rid)
	if err != nil {
		writeError(w, 404, "result not signed")
		return
	}
	hash := fmt.Sprint(sig["result_hash"])
	short := hash
	if len(short) > 16 {
		short = short[:16]
	}
	writeJSON(w, 200, M{"qr_data": fmt.Sprintf("INEC:VERIFY:%s:%s:%v", rid, short, sig["chain_position"]), "verify_url": "/citizen/verify/signature?result_id=" + rid})
}

// ═══════════════════════════════════════════════════════════════════════════════
// #23: Real-Time Media API + #8: PDF Reports + #11: OpenAPI 3.1
// ═══════════════════════════════════════════════════════════════════════════════

func handleMediaStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ctx := r.Context()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	snap := buildMediaSnapshot(r.Context())
	data, _ := json.Marshal(snap)
	fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
	flusher.Flush()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u := buildMediaSnapshot(ctx)
			d, _ := json.Marshal(u)
			fmt.Fprintf(w, "event: update\ndata: %s\n\n", d)
			flusher.Flush()
		}
	}
}

func handleMediaWidget(w http.ResponseWriter, r *http.Request) {
	snap := buildMediaSnapshot(r.Context())
	snap["widget_type"] = r.URL.Query().Get("type")
	snap["branding"] = M{"name": "INEC Nigeria", "footer": "Official Results"}
	writeJSON(w, 200, snap)
}

func handleExportPDFReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	reportType := r.URL.Query().Get("type")
	format := r.URL.Query().Get("format")
	if reportType == "" {
		reportType = "summary"
	}
	var data M
	var title string
	switch reportType {
	case "summary":
		title = "INEC Election Summary Report"
		tr, _ := querySingleRowCtx(ctx, `SELECT COUNT(*) as c FROM results`)
		tv, _ := querySingleRowCtx(ctx, `SELECT COALESCE(SUM(total_votes),0) as t FROM results`)
		tp, _ := querySingleRowCtx(ctx, `SELECT COUNT(*) as c FROM polling_units`)
		pp, _ := dbQueryCtx(ctx, `SELECT party_code, SUM(votes) as tv FROM party_scores GROUP BY party_code ORDER BY tv DESC LIMIT 10`)
		stateRows, _ := dbQueryCtx(ctx, `SELECT pu.state_code, COUNT(DISTINCT pu.code) as total_pus,
			COUNT(DISTINCT r.polling_unit_code) as reported_pus
			FROM polling_units pu LEFT JOIN results r ON pu.code=r.polling_unit_code
			GROUP BY pu.state_code ORDER BY pu.state_code`)
		data = M{"title": title, "results_count": tr, "total_votes": tv, "total_pus": tp,
			"parties": scanRows(pp), "states": scanRows(stateRows), "generated": time.Now().UTC()}
	case "observer":
		title = "Observer Report (OSCE/ODIHR Format)"
		reps, _ := dbQueryCtx(ctx, `SELECT * FROM observer_reports ORDER BY created_at DESC`)
		incidents, _ := dbQueryCtx(ctx, `SELECT * FROM incidents ORDER BY created_at DESC`)
		data = M{"title": title, "reports": scanRows(reps), "incidents": scanRows(incidents), "generated": time.Now().UTC()}
	case "gazette":
		title = "Official Gazette — Election Results"
		results, _ := dbQueryCtx(ctx, `SELECT r.*, pu.pu_name, pu.state_code FROM results r LEFT JOIN polling_units pu ON r.polling_unit_code=pu.code ORDER BY r.submitted_at DESC`)
		scores, _ := dbQueryCtx(ctx, `SELECT party_code, SUM(votes) as total_votes FROM party_scores GROUP BY party_code ORDER BY total_votes DESC`)
		data = M{"title": title, "certified_by": "INEC", "results": scanRows(results), "party_totals": scanRows(scores), "generated": time.Now().UTC()}
	default:
		title = reportType + " Report"
		data = M{"title": title, "generated": time.Now().UTC()}
	}
	if mwHub != nil && mwHub.OpenSearch != nil {
		mwHub.OpenSearch.Index(ctx, "reports", fmt.Sprintf("report-%d", time.Now().Unix()), data)
	}
	// R5-053: route through the hash-chained audit writer (no NULL block_hash bypass).
	logAuditCtx(ctx, "export_report", "report", reportType, extractUserID(r), M{"type": reportType, "format": format})
	if format == "html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		renderHTMLReport(w, title, data)
		return
	}
	writeJSON(w, 200, data)
}

func renderHTMLReport(w http.ResponseWriter, title string, data M) {
	safeTitle := html.EscapeString(title)
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>%s</title>
<style>body{font-family:Arial,sans-serif;margin:40px;color:#333}
h1{color:#006837;border-bottom:3px solid #006837;padding-bottom:10px}
table{border-collapse:collapse;width:100%%}
th,td{border:1px solid #ddd;padding:8px;text-align:left}
th{background:#006837;color:white}
.footer{margin-top:40px;font-size:12px;color:#666}
</style></head><body>`, safeTitle)
	fmt.Fprintf(w, `<h1>%s</h1>`, safeTitle)
	fmt.Fprintf(w, `<p>Generated: %v</p>`, html.EscapeString(fmt.Sprintf("%v", data["generated"])))
	if parties, ok := data["parties"].([]M); ok && len(parties) > 0 {
		fmt.Fprintf(w, `<h2>Party Results</h2><table><tr><th>Party</th><th>Total Votes</th></tr>`)
		for _, p := range parties {
			fmt.Fprintf(w, `<tr><td>%s</td><td>%v</td></tr>`, html.EscapeString(fmt.Sprintf("%v", p["party_code"])), p["tv"])
		}
		fmt.Fprintf(w, `</table>`)
	}
	fmt.Fprintf(w, `<div class="footer"><p>Independent National Electoral Commission (INEC) — Official Document</p>
<p>This report was generated from the INEC election management system.</p></div></body></html>`)
}

func handleOpenAPIDocs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, M{
		"openapi": "3.1.0",
		"info": M{"title": "INEC Election Platform API", "version": "2.0.0",
			"description": "Comprehensive API for election management with 400+ endpoints"},
		"servers":    []M{{"url": "/", "description": "Current"}},
		"paths":      buildOpenAPIPaths(),
		"tags":       buildOpenAPITags(),
		"components": M{"securitySchemes": M{"bearerAuth": M{"type": "http", "scheme": "bearer"}, "apiKeyAuth": M{"type": "apiKey", "in": "header", "name": "X-API-Key"}}},
	})
}

func buildOpenAPIPaths() M {
	p := M{}
	eps := [][4]string{
		{"get", "/healthz", "Health", "Deep health check"},
		{"post", "/auth/login", "Auth", "Login"},
		{"post", "/auth/mfa/totp/setup", "MFA", "Setup TOTP"},
		{"post", "/auth/mfa/challenge", "MFA", "MFA challenge"},
		{"get", "/elections", "Elections", "List elections"},
		{"post", "/results/submit", "Results", "Submit result"},
		{"get", "/command-center/live", "CommandCenter", "Live command center"},
		{"get", "/citizen/verify", "CitizenPortal", "Verify results (public)"},
		{"get", "/media/stream", "MediaAPI", "SSE stream for media"},
		{"get", "/ai/anomalies", "AI", "Anomaly detection"},
		{"get", "/blockchain/stats", "Blockchain", "Blockchain stats"},
		{"get", "/biometric/stats", "Biometrics", "Biometric stats"},
		{"get", "/middleware/status", "Middleware", "Middleware status"},
		{"get", "/predictive/analytics", "Analytics", "Predictive analytics"},
	}
	for _, e := range eps {
		if p[e[1]] == nil {
			p[e[1]] = M{}
		}
		p[e[1]].(M)[e[0]] = M{"tags": []string{e[2]}, "summary": e[3], "responses": M{"200": M{"description": "OK"}}}
	}
	return p
}

func buildOpenAPITags() []M {
	return []M{
		{"name": "Health"}, {"name": "Auth"}, {"name": "MFA"}, {"name": "Elections"},
		{"name": "Results"}, {"name": "CommandCenter"}, {"name": "CitizenPortal"},
		{"name": "MediaAPI"}, {"name": "AI"}, {"name": "Blockchain"}, {"name": "Biometrics"},
		{"name": "Middleware"}, {"name": "Analytics"},
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// #7: Anomaly Escalation + #9: Biometric Quality + #10: Role Rate Limiting
// #12: Geo-fenced Submissions + #16: Predictive Analytics
// #17: Multi-Election + #18: Observer Enhancements + #20: Data Sovereignty
// ═══════════════════════════════════════════════════════════════════════════════

func handleGeoFencedSubmit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ResultID   int     `json:"result_id"`
		OfficerLat float64 `json:"officer_lat"`
		OfficerLng float64 `json:"officer_lng"`
		PUCode     string  `json:"pu_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	puRow, err := querySingleRowCtx(r.Context(), `SELECT latitude, longitude FROM polling_units WHERE pu_code=?`, body.PUCode)
	if err != nil {
		writeError(w, 404, "PU not found")
		return
	}
	puLat := enhToFloat(puRow["latitude"])
	puLng := enhToFloat(puRow["longitude"])
	dist := haversineDist(body.OfficerLat, body.OfficerLng, puLat, puLng)
	ok := dist <= 500
	dbExecCtx(r.Context(), `INSERT INTO geofenced_submissions (result_id, officer_lat, officer_lng, pu_lat, pu_lng, distance_meters, within_boundary) VALUES (?,?,?,?,?,?,?)`,
		body.ResultID, body.OfficerLat, body.OfficerLng, puLat, puLng, dist, bToI(ok))
	if !ok && mwHub != nil && mwHub.OpenSearch != nil {
		mwHub.OpenSearch.Index(r.Context(), "geofence-violations", fmt.Sprintf("gv-%d", time.Now().UnixNano()), M{"result_id": body.ResultID, "distance": dist})
	}
	writeJSON(w, 200, M{"allowed": ok, "distance_m": dist, "max_allowed_m": 500})
}

func handleGeoFenceOverride(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SubmissionID int    `json:"submission_id"`
		Reason       string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	uid := extractUserID(r)
	dbExecCtx(r.Context(), `UPDATE geofenced_submissions SET override_by=?, override_reason=?, within_boundary=1 WHERE id=?`, uid, body.Reason, body.SubmissionID)
	writeJSON(w, 200, M{"status": "override_approved"})
}

func handleAnomalyEscalation(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		rows, _ := dbQueryCtx(r.Context(), `SELECT * FROM anomaly_escalations WHERE resolved=0 ORDER BY created_at DESC LIMIT 50`)
		writeJSON(w, 200, M{"escalations": scanRows(rows)})
		return
	}
	var body struct {
		AnomalyID string `json:"anomaly_id"`
		Severity  string `json:"severity"`
		StateCode string `json:"state_code"`
		PUCode    string `json:"pu_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	action, escalatedTo, paused := "logged", "dashboard", 0
	switch body.Severity {
	case "WARN":
		action, escalatedTo = "notify_state_rec", "state_rec"
		if mwHub != nil && mwHub.Dapr != nil {
			mwHub.Dapr.PublishEvent(r.Context(), "escalation", "warn", M{"state": body.StateCode})
		}
	case "CRITICAL":
		action, escalatedTo, paused = "pause_collation", "supervisor", 1
		if mwHub != nil && mwHub.Kafka != nil {
			mwHub.Kafka.Produce(r.Context(), KafkaMessage{Topic: "escalation.critical", Key: body.StateCode, Value: M{"state": body.StateCode, "action": "pause"}, Timestamp: time.Now()})
		}
		if mwHub != nil && mwHub.Temporal != nil {
			mwHub.Temporal.StartWorkflow(r.Context(), WorkflowInput{WorkflowID: "escalation-" + body.AnomalyID, WorkflowType: "anomaly-escalation", TaskQueue: "escalation", Input: map[string]interface{}{"severity": body.Severity}})
		}
	case "EMERGENCY":
		action, escalatedTo, paused = "notify_chairman", "chairman", 1
	}
	dbExecCtx(r.Context(), `INSERT INTO anomaly_escalations (anomaly_id, severity, state_code, pu_code, action_taken, escalated_to, collation_paused) VALUES (?,?,?,?,?,?,?)`,
		body.AnomalyID, body.Severity, body.StateCode, body.PUCode, action, escalatedTo, paused)
	writeJSON(w, 200, M{"status": "escalated", "severity": body.Severity, "action": action, "collation_paused": paused == 1})
}

func handleBiometricQualityCheck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CaptureID string  `json:"capture_id"`
		Modality  string  `json:"modality"`
		Blur      float64 `json:"blur_score"`
		Exposure  float64 `json:"exposure"`
		Angle     float64 `json:"angle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	quality := (body.Blur + body.Exposure + body.Angle) / 3.0
	var guidance []string
	if body.Blur < 0.5 {
		guidance = append(guidance, "Image blurry — hold steady")
	}
	if body.Exposure < 0.3 {
		guidance = append(guidance, "Too dark — improve lighting")
	} else if body.Exposure > 0.9 {
		guidance = append(guidance, "Overexposed — reduce light")
	}
	if body.Modality == "face" && body.Angle < 0.6 {
		guidance = append(guidance, "Look directly at camera")
	}
	if body.Modality == "fingerprint" && body.Blur < 0.4 {
		guidance = append(guidance, "Press finger firmly for 2 seconds")
	}
	passed := quality >= 0.6 && len(guidance) == 0
	if len(guidance) == 0 {
		guidance = []string{"Quality acceptable — proceed"}
	}
	dbExecCtx(r.Context(), `INSERT INTO biometric_quality_scores (capture_id, modality, blur_score, exposure_score, angle_score, overall_quality, guidance) VALUES (?,?,?,?,?,?,?)`,
		body.CaptureID, body.Modality, body.Blur, body.Exposure, body.Angle, quality, strings.Join(guidance, "; "))
	writeJSON(w, 200, M{"quality_score": quality, "passed": passed, "guidance": guidance})
}

func handlePredictiveAnalytics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	eid := r.URL.Query().Get("election_id")
	if eid == "" {
		eid = "1"
	}
	tp, _ := querySingleRowCtx(ctx, `SELECT COUNT(*) as c FROM polling_units`)
	rp, _ := querySingleRowCtx(ctx, `SELECT COUNT(DISTINCT polling_unit_code) as c FROM results WHERE election_id=?`, eid)
	total := enhToInt(tp["c"])
	reported := enhToInt(rp["c"])
	pct := 0.0
	if total > 0 {
		pct = float64(reported) / float64(total) * 100
	}
	tr, _ := querySingleRowCtx(ctx, `SELECT AVG(CAST(total_votes AS REAL)/NULLIF(accredited_voters,0)) as t FROM results WHERE election_id=? AND accredited_voters>0`, eid)
	turnout := 0.0
	if tr != nil {
		if t, ok := tr["t"].(float64); ok {
			turnout = t * 100
		}
	}
	if mwHub != nil && mwHub.Lakehouse != nil {
		mwHub.Lakehouse.Query(ctx, LakehouseQuery{
			Query:      "INSERT INTO predictions (election_id, turnout, completion_pct) VALUES ($1, $2, $3)",
			Parameters: M{"$1": eid, "$2": turnout, "$3": pct},
		})
	}
	// Compute confidence from data coverage
	confidence := 0.0
	if total > 0 {
		confidence = float64(reported) / float64(total)
		if confidence > 0.95 {
			confidence = 0.95
		}
	}
	// Velocity-based ETA
	rateRow, _ := querySingleRowCtx(ctx, `SELECT MIN(submitted_at) as first, MAX(submitted_at) as last FROM results WHERE election_id=?`, eid)
	var eta string
	if rateRow != nil && reported > 0 {
		firstStr := fmt.Sprint(rateRow["first"])
		lastStr := fmt.Sprint(rateRow["last"])
		if firstT, err1 := time.Parse(time.RFC3339, firstStr); err1 == nil {
			if lastT, err2 := time.Parse(time.RFC3339, lastStr); err2 == nil {
				elapsed := lastT.Sub(firstT)
				if elapsed > 0 && reported < total {
					remaining := total - reported
					perUnit := elapsed / time.Duration(reported)
					etaTime := time.Now().Add(perUnit * time.Duration(remaining))
					eta = etaTime.Format(time.RFC3339)
				}
			}
		}
	}
	// Per-state predictions
	stateRows, _ := dbQueryCtx(ctx, `SELECT pu.state_code, COUNT(DISTINCT pu.code) as total,
		COUNT(DISTINCT r.polling_unit_code) as reported
		FROM polling_units pu
		LEFT JOIN results r ON pu.code=r.polling_unit_code AND r.election_id=?
		GROUP BY pu.state_code ORDER BY pu.state_code`, eid)
	var statePredictions []M
	for _, sr := range scanRows(stateRows) {
		st := enhToInt(sr["total"])
		srep := enhToInt(sr["reported"])
		sp := 0.0
		if st > 0 {
			sp = float64(srep) / float64(st) * 100
		}
		statePredictions = append(statePredictions, M{
			"state_code": sr["state_code"], "total_pus": st, "reported_pus": srep,
			"completion_pct": sp,
		})
	}
	writeJSON(w, 200, M{"election_id": eid, "completion_pct": pct, "predicted_turnout": turnout,
		"total_pus": total, "reported_pus": reported, "model": "linear_velocity_v1",
		"confidence": confidence, "eta_complete": eta, "state_predictions": statePredictions})
}

func handleElectionTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		rows, _ := dbQueryCtx(r.Context(), `SELECT * FROM election_templates ORDER BY election_type`)
		writeJSON(w, 200, M{"templates": scanRows(rows)})
		return
	}
	var body struct {
		ElectionType string `json:"election_type"`
		TemplateName string `json:"template_name"`
		PartyCount   int    `json:"party_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	dbExecCtx(r.Context(), `INSERT INTO election_templates (election_type, template_name, party_count) VALUES (?,?,?)`, body.ElectionType, body.TemplateName, body.PartyCount)
	writeJSON(w, 200, M{"status": "created"})
}

func handleElectionArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		rows, _ := dbQueryCtx(r.Context(), `SELECT id, election_id, checksum, archived_at FROM election_archive ORDER BY archived_at DESC`)
		writeJSON(w, 200, M{"archives": scanRows(rows)})
		return
	}
	var body struct {
		ElectionID int `json:"election_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	rows, _ := dbQueryCtx(r.Context(), `SELECT * FROM results WHERE election_id=? ORDER BY id LIMIT 50000`, body.ElectionID)
	data, _ := json.Marshal(scanRows(rows))
	h := sha256.Sum256(data)
	cs := hex.EncodeToString(h[:])
	dbExecCtx(r.Context(), `INSERT INTO election_archive (election_id, archived_data, checksum) VALUES (?,?,?)`, body.ElectionID, string(data), cs)
	writeJSON(w, 200, M{"status": "archived", "checksum": cs})
}

func handleDataClassification(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		rows, _ := dbQueryCtx(r.Context(), `SELECT * FROM data_classification ORDER BY table_name`)
		writeJSON(w, 200, M{"classifications": scanRows(rows)})
		return
	}
	var body struct {
		Table  string `json:"table_name"`
		Column string `json:"column_name"`
		Class  string `json:"classification"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	dbExecCtx(r.Context(), `INSERT OR REPLACE INTO data_classification (table_name, column_name, classification) VALUES (?,?,?)`, body.Table, body.Column, body.Class)
	writeJSON(w, 200, M{"status": "classified"})
}

// ── NDPR right-to-erasure (R5-060 completeness, R5-067 lifecycle) ───────────
//
// Previously erasure blanked only biometric_profiles and left voter PII,
// biometric_templates ciphertext, and the Rust vault (templates + cancelable
// seeds) intact — and its audit row bypassed the hash chain. Erasure now:
//   1. pseudonymizes voters PII (NOT NULL columns get '[ERASED]'/''; the row
//      and electoral-geography keys survive because the Electoral Act requires
//      the register itself to be retained — documented retention conflict);
//   2. deletes biometric_templates ciphertext;
//   3. blanks biometric_profiles;
//   4. hard-deletes the Rust vault templates + cancelable transforms via the
//      new /vault/erase route (failure is reported, never silently claimed);
//   5. writes the audit entry through the hash-chained logAuditCtx.

func biometricVaultErase(ctx context.Context, vin string) (int64, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("BIOMETRIC_SERVICE_URL")), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8091"
	}
	apiKey := strings.TrimSpace(os.Getenv("BIOMETRIC_VAULT_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("VAULT_API_TOKEN"))
	}
	if apiKey == "" {
		return 0, fmt.Errorf("BIOMETRIC_VAULT_API_KEY not configured — cannot erase vault templates")
	}
	payload, _ := json.Marshal(M{"voter_vin": vin})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/vault/erase", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("vault erase call failed: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		RecordsDeleted int64  `json:"records_deleted"`
		Error          string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("vault erase response decode: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("vault erase rejected (%d): %s", resp.StatusCode, out.Error)
	}
	return out.RecordsDeleted, nil
}

// executeDataErasure performs the full erasure and returns a per-step summary.
func executeDataErasure(ctx context.Context, vin, reason string, actorID int) M {
	summary := M{"vin": vin, "reason": reason, "steps": M{}}
	steps := summary["steps"].(M)

	if _, err := dbExecCtx(ctx, `UPDATE voters SET first_name='[ERASED]', last_name='[ERASED]', middle_name=NULL,
		date_of_birth='', phone=NULL, email=NULL, address=NULL, biometric_hash=NULL, photo_hash=NULL, updated_at=CURRENT_TIMESTAMP
		WHERE vin=?`, vin); err != nil {
		steps["voters_pii"] = "error: " + err.Error()
	} else {
		steps["voters_pii"] = "pseudonymized"
	}
	if _, err := dbExecCtx(ctx, `DELETE FROM biometric_templates WHERE voter_vin=?`, vin); err != nil {
		steps["biometric_templates"] = "error: " + err.Error()
	} else {
		steps["biometric_templates"] = "deleted"
	}
	if _, err := dbExecCtx(ctx, `UPDATE biometric_profiles SET template_data='[ERASED]', face_template='[ERASED]' WHERE voter_vin=?`, vin); err != nil {
		steps["biometric_profiles"] = "error: " + err.Error()
	} else {
		steps["biometric_profiles"] = "blanked"
	}
	if deleted, err := biometricVaultErase(ctx, vin); err != nil {
		steps["rust_vault"] = "error: " + err.Error()
	} else {
		steps["rust_vault"] = fmt.Sprintf("deleted %d records", deleted)
	}

	// Chained audit entry (R5-053: no more NULL block_hash bypass).
	logAuditCtx(ctx, "data_erasure", "voter", vin, actorID, M{"reason": reason, "framework": "NDPR", "steps": steps})
	return summary
}

// handleDataErasure is the legacy immediate-erasure endpoint (admin-only via
// route guard). New integrations should use the dual-control lifecycle below.
func handleDataErasure(w http.ResponseWriter, r *http.Request) {
	var body struct {
		VIN    string `json:"vin"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.VIN == "" {
		writeError(w, 400, "vin and reason required")
		return
	}
	actorID := extractUserID(r)
	summary := executeDataErasure(r.Context(), body.VIN, body.Reason, actorID)
	// Record an executed lifecycle row when the table exists (best effort —
	// pre-migration dev databases may lack it).
	if _, err := dbExecCtx(r.Context(), `INSERT INTO data_erasure_requests (vin, reason, requested_by, status, reviewed_by, reviewed_at, executed_at, review_notes)
		VALUES (?,?,?,'executed',?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'immediate admin erasure via /data/erasure')`,
		body.VIN, body.Reason, actorID, actorID); err != nil {
		log.Warn().Err(err).Msg("data_erasure_requests insert skipped (table missing?)")
	}
	summary["status"] = "erased"
	writeJSON(w, 200, summary)
}

// handleDataErasureRequest creates a pending erasure request (R5-067 dual
// control: the requester cannot approve their own request).
func handleDataErasureRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		VIN    string `json:"vin"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.VIN == "" || body.Reason == "" {
		writeError(w, 400, "vin and reason required")
		return
	}
	actorID := extractUserID(r)
	res, err := dbExecCtx(r.Context(), `INSERT INTO data_erasure_requests (vin, reason, requested_by) VALUES (?,?,?)`,
		body.VIN, body.Reason, actorID)
	if err != nil {
		writeError(w, 500, "failed to record erasure request (data_erasure_requests table required — migration 000033)")
		return
	}
	id, _ := res.LastInsertId()
	logAuditCtx(r.Context(), "data_erasure_request", "voter", body.VIN, actorID, M{"reason": body.Reason})
	writeJSON(w, 201, M{"status": "pending", "request_id": id, "vin": body.VIN})
}

// handleDataErasureReview approves (and executes) or rejects a pending request.
func handleDataErasureReview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RequestID int    `json:"request_id"`
		Decision  string `json:"decision"` // "approve" | "reject"
		Notes     string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Decision != "approve" && body.Decision != "reject") {
		writeError(w, 400, "request_id and decision (approve|reject) required")
		return
	}
	reviewerID := extractUserID(r)
	row, err := querySingleRowCtx(r.Context(), `SELECT * FROM data_erasure_requests WHERE id=?`, body.RequestID)
	if err != nil {
		writeError(w, 404, "erasure request not found")
		return
	}
	if enhStr(row, "status") != "pending" {
		writeError(w, 409, "request already "+enhStr(row, "status"))
		return
	}
	// Dual control: requester may not review their own request.
	if enhToInt(row["requested_by"]) == reviewerID && reviewerID != 0 {
		writeError(w, 403, "dual control: the requester cannot review their own erasure request")
		return
	}
	vin := enhStr(row, "vin")
	if body.Decision == "reject" {
		dbExecCtx(r.Context(), `UPDATE data_erasure_requests SET status='rejected', reviewed_by=?, reviewed_at=CURRENT_TIMESTAMP, review_notes=? WHERE id=?`,
			reviewerID, body.Notes, body.RequestID)
		logAuditCtx(r.Context(), "data_erasure_rejected", "voter", vin, reviewerID, M{"request_id": body.RequestID, "notes": body.Notes})
		writeJSON(w, 200, M{"status": "rejected", "request_id": body.RequestID})
		return
	}
	summary := executeDataErasure(r.Context(), vin, enhStr(row, "reason"), reviewerID)
	dbExecCtx(r.Context(), `UPDATE data_erasure_requests SET status='executed', reviewed_by=?, reviewed_at=CURRENT_TIMESTAMP, executed_at=CURRENT_TIMESTAMP, review_notes=? WHERE id=?`,
		reviewerID, body.Notes, body.RequestID)
	summary["status"] = "executed"
	summary["request_id"] = body.RequestID
	writeJSON(w, 200, summary)
}

// handleDataErasureRequests lists erasure requests (optionally ?status=pending).
func handleDataErasureRequests(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	q := `SELECT * FROM data_erasure_requests`
	var args []interface{}
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY requested_at DESC LIMIT 200`
	rows, err := dbQueryCtx(r.Context(), q, args...)
	if err != nil {
		writeError(w, 500, "data_erasure_requests table required (migration 000033)")
		return
	}
	writeJSON(w, 200, M{"requests": scanRows(rows)})
}

func handleObserverPhotoVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ObserverID int     `json:"observer_id"`
		PUCode     string  `json:"pu_code"`
		PhotoHash  string  `json:"photo_hash"`
		GPSLat     float64 `json:"gps_lat"`
		GPSLng     float64 `json:"gps_lng"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	row, _ := querySingleRowCtx(r.Context(), `SELECT COUNT(*) as c FROM observer_photo_verifications WHERE pu_code=?`, body.PUCode)
	cnt := 0
	if row != nil {
		cnt = enhToInt(row["c"])
	}
	consensus := float64(cnt+1) / 3.0
	if consensus > 1 {
		consensus = 1
	}
	dbExecCtx(r.Context(), `INSERT INTO observer_photo_verifications (observer_id, pu_code, photo_hash, gps_lat, gps_lng, timestamp_watermark, consensus_score) VALUES (?,?,?,?,?,?,?)`,
		body.ObserverID, body.PUCode, body.PhotoHash, body.GPSLat, body.GPSLng, time.Now().UTC().Format(time.RFC3339), consensus)
	if mwHub != nil && mwHub.OpenSearch != nil {
		mwHub.OpenSearch.Index(r.Context(), "observer-photos", fmt.Sprintf("photo-%d-%s", body.ObserverID, body.PUCode), M{"consensus": consensus})
	}
	writeJSON(w, 200, M{"consensus_score": consensus, "observers_count": cnt + 1, "full_consensus": consensus >= 1})
}

// handleOfflineConflictResolve (R5-063) — the previous implementation compared
// client-supplied timestamps, wrote one audit row, and returned a "merged"
// object while changing no server state. Now the resolution is real:
//   - the authoritative server version is read from the database, never from
//     the client-echoed server_data;
//   - server_wins / last_writer_wins against a still-mutable ('pending') result
//     atomically applies the winning vote figures to the results table;
//   - conflicts on validated/finalized results are refused (409) and must go
//     through the reconciliation-case machinery (R5-016 immutability);
//   - every resolution is audit-logged through the hash chain (R5-053).
func handleOfflineConflictResolve(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "presiding_officer", "collation_officer"); err != nil {
		writeError(w, 403, "conflict resolution requires an election officer")
		return
	}
	var body struct {
		ResultID int    `json:"result_id"`
		Local    M      `json:"local_data"`
		Server   M      `json:"server_data"`
		Strategy string `json:"strategy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ResultID <= 0 {
		writeError(w, 400, "result_id and a valid strategy are required")
		return
	}
	if body.Strategy != "last_writer_wins" && body.Strategy != "server_wins" && body.Strategy != "local_wins" {
		writeError(w, 400, "strategy must be one of: last_writer_wins, server_wins, local_wins")
		return
	}

	serverRow, err := querySingleRowCtx(r.Context(), `SELECT * FROM results WHERE id=?`, body.ResultID)
	if err != nil {
		writeError(w, 404, "result not found")
		return
	}
	status := enhStr(serverRow, "status")
	if status != "pending" {
		writeError(w, 409, fmt.Sprintf("result is %s — immutable; open a reconciliation case (/results/%d/reconciliation) instead of offline conflict resolution", status, body.ResultID))
		return
	}

	winner := "server"
	if body.Strategy == "local_wins" ||
		(body.Strategy == "last_writer_wins" && fmt.Sprint(body.Local["submitted_at"]) > enhStr(serverRow, "submitted_at")) {
		winner = "local"
	}

	updated := false
	if winner == "local" {
		// Apply only the scalar vote fields the offline client can authoritatively
		// carry; anything absent keeps the server value.
		totalValid := enhToInt(serverRow["total_valid_votes"])
		rejected := enhToInt(serverRow["rejected_votes"])
		totalCast := enhToInt(serverRow["total_votes_cast"])
		accredited := enhToInt(serverRow["accredited_voters"])
		if v, ok := body.Local["total_valid_votes"]; ok {
			totalValid = enhToInt(v)
		}
		if v, ok := body.Local["rejected_votes"]; ok {
			rejected = enhToInt(v)
		}
		if v, ok := body.Local["total_votes_cast"]; ok {
			totalCast = enhToInt(v)
		}
		if v, ok := body.Local["accredited_voters"]; ok {
			accredited = enhToInt(v)
		}
		if totalCast < totalValid+rejected {
			writeError(w, 422, "local_data fails EC8A arithmetic (total_votes_cast < total_valid_votes + rejected_votes) — refusing to apply")
			return
		}
		if _, err := dbExecCtx(r.Context(), `UPDATE results SET total_valid_votes=?, rejected_votes=?, total_votes_cast=?, accredited_voters=? WHERE id=? AND status='pending'`,
			totalValid, rejected, totalCast, accredited, body.ResultID); err != nil {
			writeError(w, 500, "failed to apply conflict resolution")
			return
		}
		updated = true
	}

	logAuditCtx(r.Context(), "conflict_resolution", "result", fmt.Sprint(body.ResultID), extractUserID(r),
		M{"strategy": body.Strategy, "winner": winner, "applied": updated})
	if mwHub != nil && mwHub.Fluvio != nil {
		if ferr := mwHub.Fluvio.Produce(r.Context(), "conflict-resolution", FluvioRecord{Topic: "conflict-resolution", Key: fmt.Sprint(body.ResultID), Value: M{"result_id": body.ResultID, "strategy": body.Strategy, "winner": winner}, Timestamp: time.Now()}); ferr != nil {
			log.Error().Err(ferr).Int("result_id", body.ResultID).Msg("conflict-resolution event publish failed")
		}
	}
	writeJSON(w, 200, M{"status": "resolved", "strategy": body.Strategy, "winner": winner, "applied": updated})
}

func handleIVRVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone  string `json:"phone_number"`
		PUCode string `json:"pu_code"`
		Lang   string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	if body.Lang == "" {
		body.Lang = "en"
	}
	result, err := querySingleRowCtx(r.Context(), `SELECT total_votes, accredited_voters FROM results WHERE polling_unit_code=? ORDER BY submitted_at DESC LIMIT 1`, body.PUCode)
	if err != nil {
		writeJSON(w, 200, M{"tts_message": "No results found", "language": body.Lang})
		return
	}
	msgs := map[string]string{
		"en": fmt.Sprintf("Polling unit %s: %v total votes, %v accredited.", body.PUCode, result["total_votes"], result["accredited_voters"]),
		"ha": fmt.Sprintf("Wurin zabe %s: Jimlar kuri'u %v.", body.PUCode, result["total_votes"]),
		"yo": fmt.Sprintf("Aaye idibo %s: Iye ibo %v.", body.PUCode, result["total_votes"]),
		"ig": fmt.Sprintf("Ebe ntuli aka %s: Votu %v.", body.PUCode, result["total_votes"]),
	}
	msg := msgs[body.Lang]
	if msg == "" {
		msg = msgs["en"]
	}
	writeJSON(w, 200, M{"tts_message": msg, "language": body.Lang, "result": result})
}

// ═══════════════════════════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════════════════════════

func computeStateVelocities(ctx context.Context) []*StateVelocity {
	rows, err := dbQueryCtx(ctx, `SELECT s.code as state_code, s.name as state_name,
		(SELECT COUNT(*) FROM polling_units pu2
		 JOIN wards w2 ON pu2.ward_code=w2.code
		 JOIN lgas l2 ON w2.lga_code=l2.code
		 WHERE l2.state_code=s.code) as total_pus,
		(SELECT COUNT(DISTINCT r.polling_unit_code) FROM results r
		 JOIN polling_units pu ON r.polling_unit_code=pu.code
		 JOIN wards w ON pu.ward_code=w.code
		 JOIN lgas l ON w.lga_code=l.code
		 WHERE l.state_code=s.code) as reported_pus
		FROM states s ORDER BY s.name`)
	if err != nil {
		return nil
	}
	var states []*StateVelocity
	for _, row := range scanRows(rows) {
		sv := &StateVelocity{
			StateCode: fmt.Sprint(row["state_code"]), StateName: fmt.Sprint(row["state_name"]),
			TotalPUs: enhToInt(row["total_pus"]), ReportedPUs: enhToInt(row["reported_pus"]),
		}
		if sv.TotalPUs > 0 {
			sv.CompletionPct = float64(sv.ReportedPUs) / float64(sv.TotalPUs) * 100
		}
		sv.StalledPUs = sv.TotalPUs - sv.ReportedPUs
		switch {
		case sv.CompletionPct >= 90:
			sv.Status = "green"
		case sv.CompletionPct >= 50:
			sv.Status = "amber"
		default:
			sv.Status = "red"
		}
		if sv.ReportedPUs == 0 {
			sv.ETAComplete = "not started"
		} else if sv.CompletionPct >= 100 {
			sv.ETAComplete = "complete"
		} else {
			sv.ETAComplete = "in progress"
		}
		states = append(states, sv)
	}
	return states
}

func getActiveAlerts(ctx context.Context) []M {
	rows, err := dbQueryCtx(ctx, `SELECT id, level, COALESCE(state_code,'') as state_code, message FROM command_alerts WHERE resolved=0 ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return nil
	}
	return scanRows(rows)
}

func runEscalationChecks(states []*StateVelocity) []M {
	cmdCenter.mu.Lock()
	defer cmdCenter.mu.Unlock()
	var alerts []M
	now := time.Now()
	for _, s := range states {
		for i := range cmdCenter.escalationRules {
			rule := &cmdCenter.escalationRules[i]
			if now.Sub(rule.LastFired) < rule.Cooldown {
				continue
			}
			triggered := false
			switch rule.Name {
			case "stalled_pu_warn":
				triggered = s.StalledPUs > 100 && s.CompletionPct > 10
			case "stalled_pu_critical":
				triggered = s.StalledPUs > 500 && s.CompletionPct > 10
			case "zero_submissions":
				triggered = s.ReportedPUs == 0 && s.TotalPUs > 100
			}
			if triggered {
				msg := fmt.Sprintf("[%s] %s in %s: %d stalled, %.1f%% done", rule.Level, rule.Name, s.StateName, s.StalledPUs, s.CompletionPct)
				alerts = append(alerts, M{"level": rule.Level, "state": s.StateCode, "message": msg, "action": rule.Action})
				rule.LastFired = now
				dbExecCtx(context.Background(), `INSERT INTO escalation_log (rule_name, level, state_code, action_taken, details) VALUES (?,?,?,?,?)`,
					rule.Name, rule.Level, s.StateCode, rule.Action, msg)
				if mwHub != nil && mwHub.Kafka != nil {
					mwHub.Kafka.Produce(context.Background(), KafkaMessage{Topic: "command-center.escalation", Key: s.StateCode, Value: M{"level": rule.Level, "state": s.StateCode, "msg": msg}, Timestamp: time.Now()})
				}
			}
		}
	}
	return alerts
}

func buildMediaSnapshot(ctx context.Context) M {
	tv, _ := querySingleRowCtx(ctx, `SELECT COALESCE(SUM(total_votes),0) as t FROM results`)
	rc, _ := querySingleRowCtx(ctx, `SELECT COUNT(*) as c FROM results`)
	pp, _ := dbQueryCtx(ctx, `SELECT party_code, SUM(votes) as tv FROM party_scores GROUP BY party_code ORDER BY tv DESC LIMIT 20`)
	return M{"timestamp": time.Now().UTC(), "total_votes": tv["t"], "results_count": rc["c"], "parties": scanRows(pp), "source": "INEC Official"}
}

func validateTOTP(secret, code string) bool {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return false
	}
	now := time.Now().Unix() / 30
	for off := int64(-1); off <= 1; off++ {
		if genTOTP(key, now+off) == code {
			return true
		}
	}
	return false
}

func genTOTP(key []byte, counter int64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))
	mac := hmac.New(sha256.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1000000)
}

func fillRandom(b []byte) error {
	_, err := cryptorand.Read(b)
	return err
}

func computeResultHash(result M) string {
	data := fmt.Sprintf("%v:%v:%v:%v", result["election_id"], result["polling_unit_code"], result["total_votes"], result["submitted_at"])
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func extractUserID(r *http.Request) int {
	claims, ok := getUserFromContext(r)
	if !ok {
		return 0
	}
	for _, key := range []string{"user_id", "sub"} {
		switch value := claims[key].(type) {
		case float64:
			if value >= 1 && value == float64(int(value)) {
				return int(value)
			}
		case int:
			if value >= 1 {
				return value
			}
		case string:
			if value, err := strconv.Atoi(value); err == nil && value >= 1 {
				return value
			}
		}
	}
	return 0
}

func phoneMask(p string) string {
	if len(p) < 6 {
		return "***"
	}
	return p[:3] + strings.Repeat("*", len(p)-5) + p[len(p)-2:]
}

func haversineDist(lat1, lng1, lat2, lng2 float64) float64 {
	toRad := 3.14159265359 / 180
	dLat := (lat2 - lat1) * toRad
	dLng := (lng2 - lng1) * toRad
	a := sinA(dLat/2)*sinA(dLat/2) + cosA(lat1*toRad)*cosA(lat2*toRad)*sinA(dLng/2)*sinA(dLng/2)
	return 6371000 * 2 * atanA(sqrtA(a), sqrtA(1-a))
}

func sinA(x float64) float64 { return x - x*x*x/6 + x*x*x*x*x/120 }
func cosA(x float64) float64 { return 1 - x*x/2 + x*x*x*x/24 }
func atanA(y, x float64) float64 {
	if x == 0 {
		if y > 0 {
			return 1.5708
		}
		return -1.5708
	}
	return y / x
}
func sqrtA(x float64) float64 {
	if x <= 0 {
		return 0
	}
	g := x / 2
	for i := 0; i < 10; i++ {
		g = (g + x/g) / 2
	}
	return g
}

func enhToInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(val)
		return n
	}
	return 0
}

func enhToFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	}
	return 0
}

func bToI(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Role-based rate limiting middleware (#10)
func roleBasedRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public/monitoring paths are already rate-limited by rateLimitMiddleware
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		role := "public"
		if claims, ok := getUserFromContext(r); ok {
			if rv, ok := claims["role"].(string); ok {
				role = rv
			}
		}
		limit := 60
		switch role {
		case "observer":
			limit = 120
		case "presiding_officer", "collation_officer":
			limit = 300
		case "admin":
			limit = 1000
		}
		key := fmt.Sprintf("role:%s:%s", role, r.RemoteAddr)
		if !rateLimiter.allow(key, limit, time.Minute) {
			w.Header().Set("X-RateLimit-Limit", fmt.Sprint(limit))
			writeError(w, 429, fmt.Sprintf("rate limit: %d/min for %s", limit, role))
			return
		}
		next.ServeHTTP(w, r)
	})
}
