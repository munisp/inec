package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// R4-53 remediation: the web (inec-frontend api.ts) and mobile
// (inec-mobile src/lib/api.ts) clients POST crash/error telemetry to
// /api/v1/errors/frontend, which previously had no route — every report was
// silently 404'd. These handlers receive, sanitize, cap and store those
// reports, and expose them to admins for triage.
//
// The ingest endpoint is intentionally unauthenticated (a client may crash
// before login) but is strictly rate-limited per IP and size-capped.

const (
	frontendErrorMaxBody    = 8 << 10 // 8 KiB — telemetry payloads are small
	frontendErrorMaxField   = 512     // per-string-field cap
	frontendErrorMaxStack   = 4096    // stack/context cap
	frontendErrorRateLimit  = 30      // reports per IP per minute
	frontendErrorMaxResults = 200     // admin listing page cap
)

type frontendErrorReport struct {
	App       string `json:"app"`     // "web" | "mobile" | other client identifier
	Message   string `json:"message"` // required
	Stack     string `json:"stack"`
	URL       string `json:"url"` // page/route where the error occurred
	Component string `json:"component"`
	Severity  string `json:"severity"` // info | warning | error | fatal
	Context   string `json:"context"`  // arbitrary JSON-ish detail, stored as text
	UserAgent string `json:"user_agent"`
}

// clip bounds s to max bytes without splitting multi-byte runes mid-rune in a
// way that would produce invalid UTF-8 (strings are truncated at byte level,
// then any trailing invalid sequence is dropped by ToValidUTF8).
func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "")
}

// sanitize strips control characters (except newline/tab inside stacks) so
// stored values cannot smuggle terminal escapes or log-forging newlines in
// single-line fields.
func sanitizeField(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func validSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info", "warning", "error", "fatal":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "error"
	}
}

// handleFrontendErrorReport ingests a client error report.
// POST /api/v1/errors/frontend — unauthenticated, rate-limited, size-capped.
func handleFrontendErrorReport(w http.ResponseWriter, r *http.Request) {
	ip := getClientIP(r)
	if !rateLimiter.allow(ip+":/api/v1/errors/frontend", frontendErrorRateLimit, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	var rep frontendErrorReport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, frontendErrorMaxBody)).Decode(&rep); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	rep.Message = clip(sanitizeField(rep.Message), frontendErrorMaxField)
	if rep.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	rep.App = clip(sanitizeField(rep.App), 32)
	rep.Stack = clip(sanitizeField(rep.Stack), frontendErrorMaxStack)
	rep.URL = clip(sanitizeField(rep.URL), frontendErrorMaxField)
	rep.Component = clip(sanitizeField(rep.Component), frontendErrorMaxField)
	rep.Context = clip(sanitizeField(rep.Context), frontendErrorMaxStack)
	rep.UserAgent = clip(sanitizeField(rep.UserAgent), frontendErrorMaxField)
	rep.Severity = validSeverity(rep.Severity)

	// Best-effort persistence; the table is created by migration 000028. A
	// missing table (unmigrated dev DB) must not break clients — telemetry is
	// best-effort by contract — so failures are logged, not surfaced.
	if _, err := db.ExecContext(r.Context(),
		`INSERT INTO frontend_errors (app, severity, message, stack, url, component, context, user_agent, client_ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullIfEmpty(rep.App), rep.Severity, rep.Message, nullIfEmpty(rep.Stack),
		nullIfEmpty(rep.URL), nullIfEmpty(rep.Component), nullIfEmpty(rep.Context),
		nullIfEmpty(rep.UserAgent), nullIfEmpty(ip),
	); err != nil {
		log.Warn().Err(err).Str("client_ip", ip).Msg("frontend_errors: insert failed (migration 000028 applied?)")
	}

	log.Info().
		Str("app", rep.App).
		Str("severity", rep.Severity).
		Str("message", rep.Message).
		Str("url", rep.URL).
		Str("component", rep.Component).
		Str("client_ip", ip).
		Msg("frontend error report")

	writeJSON(w, http.StatusAccepted, M{"received": true})
}

// handleFrontendErrorList returns recent reports for admin triage.
// GET /api/v1/errors/frontend — admin only.
func handleFrontendErrorList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryContext(r.Context(),
		`SELECT id, app, severity, message, url, component, user_agent, client_ip, created_at
		 FROM frontend_errors ORDER BY id DESC LIMIT ?`, frontendErrorMaxResults)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "frontend_errors store unavailable")
		return
	}
	defer rows.Close()

	reports := []M{}
	for rows.Next() {
		var id int64
		var app, severity, message, url, component, userAgent, clientIP, createdAt string
		if rows.Scan(&id, &app, &severity, &message, &url, &component, &userAgent, &clientIP, &createdAt) == nil {
			reports = append(reports, M{
				"id": id, "app": app, "severity": severity, "message": message,
				"url": url, "component": component, "user_agent": userAgent,
				"client_ip": clientIP, "created_at": createdAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, M{"reports": reports, "total": len(reports)})
}
