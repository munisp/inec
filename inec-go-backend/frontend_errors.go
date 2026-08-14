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
	Source    string `json:"source"`  // legacy alias for App (inec-frontend / inec-mobile)
	Message   string `json:"message"` // required (single-report form)
	Stack     string `json:"stack"`
	URL       string `json:"url"` // page/route where the error occurred
	Component string `json:"component"`
	// ComponentStack is the React error-boundary variant of Context.
	ComponentStack string `json:"componentStack"`
	Severity       string `json:"severity"` // info | warning | error | fatal
	Context        string `json:"context"`  // arbitrary JSON-ish detail, stored as text
	UserAgent      string `json:"user_agent"`
	Timestamp      string `json:"timestamp"`
	// Errors carries the batched form used by both web (lib/utils.ts flushErrors)
	// and mobile (src/lib/api.ts reportApiError): {source, errors:[...]}.
	Errors []frontendErrorItem `json:"errors"`
}

type frontendErrorItem struct {
	Message   string `json:"message"`
	Page      string `json:"page"`
	URL       string `json:"url"`
	UserAgent string `json:"user_agent"`
	Timestamp string `json:"timestamp"`
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

	// Normalize the batched {source, errors:[...]} form (used by both the web
	// and mobile telemetry clients) into individual reports.
	if rep.App == "" {
		rep.App = rep.Source
	}
	if len(rep.Errors) > 0 {
		const maxBatch = 20
		items := rep.Errors
		if len(items) > maxBatch {
			items = items[:maxBatch]
		}
		stored := 0
		for _, it := range items {
			msg := clip(sanitizeField(it.Message), frontendErrorMaxField)
			if msg == "" {
				continue
			}
			page := it.Page
			if page == "" {
				page = it.URL
			}
			storeFrontendError(r, clip(sanitizeField(rep.App), 32), "error", msg, "",
				clip(sanitizeField(page), frontendErrorMaxField), "",
				clip(sanitizeField(it.Timestamp), frontendErrorMaxField),
				clip(sanitizeField(it.UserAgent), frontendErrorMaxField), ip)
			stored++
		}
		if stored == 0 {
			writeError(w, http.StatusBadRequest, "errors entries require a message")
			return
		}
		writeJSON(w, http.StatusAccepted, M{"received": true, "stored": stored})
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
	if rep.Context == "" {
		rep.Context = clip(sanitizeField(rep.ComponentStack), frontendErrorMaxStack)
	}
	rep.UserAgent = clip(sanitizeField(rep.UserAgent), frontendErrorMaxField)
	rep.Severity = validSeverity(rep.Severity)

	storeFrontendError(r, rep.App, rep.Severity, rep.Message, rep.Stack, rep.URL,
		rep.Component, rep.Context, rep.UserAgent, ip)

	writeJSON(w, http.StatusAccepted, M{"received": true})
}

// storeFrontendError persists one sanitized report and emits a structured log
// line. Best-effort: the table is created by migration 000028; a missing table
// (unmigrated dev DB) must not break clients — telemetry is best-effort by
// contract — so failures are logged, not surfaced.
func storeFrontendError(r *http.Request, app, severity, message, stack, url, component, context, userAgent, ip string) {
	if _, err := db.ExecContext(r.Context(),
		`INSERT INTO frontend_errors (app, severity, message, stack, url, component, context, user_agent, client_ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullIfEmpty(app), severity, message, nullIfEmpty(stack),
		nullIfEmpty(url), nullIfEmpty(component), nullIfEmpty(context),
		nullIfEmpty(userAgent), nullIfEmpty(ip),
	); err != nil {
		log.Warn().Err(err).Str("client_ip", ip).Msg("frontend_errors: insert failed (migration 000028 applied?)")
	}

	log.Info().
		Str("app", app).
		Str("severity", severity).
		Str("message", message).
		Str("url", url).
		Str("component", component).
		Str("client_ip", ip).
		Msg("frontend error report")
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
