package main

// public_incidents.go — Public (unauthenticated) voter complaint & incident
// channel (R5-073 / H4-03).
//
// Voters previously had NO way to report an incident: POST /incidents is
// officer-only and the citizen portal is read-only. This file adds a public
// submission path that routes straight into the officer incident pipeline
// (the `incidents` table), hardened for anonymous internet exposure:
//   - per-IP rate limiting (fail-closed via the platform rate limiter)
//   - optional captcha proof (reCAPTCHA / hCaptcha) when a secret is
//     configured — recommended for production
//   - strict type/severity whitelists and length caps
//   - reporter contact is OPTIONAL (anonymous tips are valid)
//
// The same persistence helper backs the USSD/IVR/WhatsApp voter channels so
// every public report lands in one triage queue with a `source` marker.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	publicIncidentRateLimit  = 5
	publicIncidentRateWindow = time.Hour
	publicIncidentMaxDescLen = 2000
)

// publicIncidentTypes is the closed vocabulary a public reporter may use.
// Anything else is rejected — public input never writes free-form incident
// types into the officer pipeline.
var publicIncidentTypes = map[string]bool{
	"violence":              true,
	"ballot_box_snatching":  true,
	"vote_buying":           true,
	"voter_intimidation":    true,
	"missing_materials":     true,
	"late_opening":          true,
	"overvoting":            true,
	"result_falsification":  true,
	"accessibility_barrier": true,
	"ussd_report":           true,
	"ivr_report":            true,
	"whatsapp_report":       true,
	"other":                 true,
}

// publicIncidentSeverities caps what an anonymous reporter may claim —
// 'critical' stays an officer-only triage decision.
var publicIncidentSeverities = map[string]bool{"low": true, "medium": true, "high": true}

type publicIncidentRequest struct {
	IncidentType    string `json:"incident_type" validate:"required,max=64"`
	Description     string `json:"description" validate:"required,min=10"`
	PollingUnitCode string `json:"polling_unit_code,omitempty" validate:"omitempty,max=64"`
	Severity        string `json:"severity,omitempty" validate:"omitempty,max=16"`
	ReporterName    string `json:"reporter_name,omitempty" validate:"omitempty,max=120"`
	ReporterPhone   string `json:"reporter_phone,omitempty" validate:"omitempty,max=32"`
	ReporterEmail   string `json:"reporter_email,omitempty" validate:"omitempty,max=120"`
	CaptchaToken    string `json:"captcha_token,omitempty" validate:"omitempty,max=2048"`
}

// persistPublicIncident inserts a public-channel incident into the shared
// officer triage pipeline and returns its public reference ID. A non-nil
// error means the report was NOT recorded — callers must never confirm
// receipt in that case. Shared by the web, USSD, IVR and WhatsApp channels.
func persistPublicIncident(incidentType, description, severity, puCode, reporterPhone, reporterName, reporterEmail, source string) (string, error) {
	if db == nil {
		return "", fmt.Errorf("database unavailable")
	}
	incidentType = strings.ToLower(strings.TrimSpace(incidentType))
	if !publicIncidentTypes[incidentType] {
		return "", fmt.Errorf("unsupported incident type %q", incidentType)
	}
	description = strings.TrimSpace(description)
	// The web handler enforces a stricter 10-char floor via its validate tag;
	// USSD/IVR channels (constrained keypads) are allowed shorter reports.
	if len(description) < 3 {
		return "", fmt.Errorf("description too short")
	}
	if len(description) > publicIncidentMaxDescLen {
		description = description[:publicIncidentMaxDescLen]
	}
	severity = strings.ToLower(strings.TrimSpace(severity))
	if !publicIncidentSeverities[severity] {
		severity = "medium"
	}
	puCode = strings.TrimSpace(puCode)
	if puCode != "" {
		var exists int
		if err := db.QueryRow("SELECT 1 FROM polling_units WHERE code=?", puCode).Scan(&exists); err != nil {
			return "", fmt.Errorf("unknown polling unit code")
		}
	}
	var electionID int
	if err := db.QueryRow(`SELECT id FROM elections ORDER BY election_date DESC, id DESC LIMIT 1`).Scan(&electionID); err != nil {
		return "", fmt.Errorf("no election context for incident report: %w", err)
	}
	var id int64
	err := db.QueryRow(`INSERT INTO incidents (election_id, polling_unit_code, incident_type, description, severity, reporter_name, reporter_phone, reporter_email, source)
		VALUES (?,?,?,?,?,?,?,?,?) RETURNING id`,
		electionID, puCode, incidentType, description, severity,
		strings.TrimSpace(reporterName), strings.TrimSpace(reporterPhone), strings.TrimSpace(reporterEmail), source).Scan(&id)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("INC-%d", id), nil
}

// captchaConfigured reports whether a captcha secret is configured for the
// public incident endpoint (PUBLIC_INCIDENT_CAPTCHA_SECRET).
func captchaConfigured() bool {
	return strings.TrimSpace(os.Getenv("PUBLIC_INCIDENT_CAPTCHA_SECRET")) != ""
}

// verifyCaptcha performs a real server-to-server verification of a captcha
// token against the configured provider. Provider selection:
// PUBLIC_INCIDENT_CAPTCHA_PROVIDER = "recaptcha" (default) | "hcaptcha".
func verifyCaptcha(remoteIP, token string) (bool, error) {
	secret := strings.TrimSpace(os.Getenv("PUBLIC_INCIDENT_CAPTCHA_SECRET"))
	if secret == "" {
		return true, nil // not configured — enforced by rate limiting instead
	}
	if strings.TrimSpace(token) == "" {
		return false, nil
	}
	verifyURL := "https://www.google.com/recaptcha/api/siteverify"
	if strings.EqualFold(os.Getenv("PUBLIC_INCIDENT_CAPTCHA_PROVIDER"), "hcaptcha") {
		verifyURL = "https://hcaptcha.com/siteverify"
	}
	form := url.Values{"secret": {secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(verifyURL, "application/x-www-form-urlencoded", bytes.NewBufferString(form.Encode()))
	if err != nil {
		return false, fmt.Errorf("captcha verification request failed: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, fmt.Errorf("captcha verification decode failed: %w", err)
	}
	return out.Success, nil
}

// handlePublicIncident is the no-auth-wall public complaint/incident
// submission endpoint (rate-limited, captcha-gated when configured).
// Route registration (W2, main.go):
//
//	r.HandleFunc("/public/incidents", handlePublicIncident).Methods("POST")
func handlePublicIncident(w http.ResponseWriter, r *http.Request) {
	ip := getClientIP(r)
	if rateLimiter != nil && !rateLimiter.allow("public-incident:"+ip, publicIncidentRateLimit, publicIncidentRateWindow) {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(publicIncidentRateWindow.Seconds())))
		writeError(w, 429, "rate_limited")
		return
	}

	var req publicIncidentRequest
	if !decodeAndValidateBody(w, r, &req) {
		return // decodeAndValidateBody already wrote the error
	}

	ok, err := verifyCaptcha(ip, req.CaptchaToken)
	if err != nil {
		log.Error().Err(err).Msg("public incident: captcha verification error")
		writeError(w, 502, "captcha verification unavailable")
		return
	}
	if !ok {
		writeError(w, 403, "captcha verification failed")
		return
	}

	ref, perr := persistPublicIncident(req.IncidentType, req.Description, req.Severity,
		req.PollingUnitCode, req.ReporterPhone, req.ReporterName, req.ReporterEmail, "public_web")
	if perr != nil {
		if strings.Contains(perr.Error(), "unknown polling unit") || strings.Contains(perr.Error(), "unsupported incident type") || strings.Contains(perr.Error(), "description too short") {
			writeError(w, 400, perr.Error())
			return
		}
		log.Error().Err(perr).Msg("public incident: persistence failed")
		writeError(w, 500, "failed to record incident")
		return
	}

	auditWrite("PUBLIC_INCIDENT_RECEIVED", "incident", ref, r, map[string]interface{}{
		"type":   req.IncidentType,
		"source": "public_web",
		"pu":     req.PollingUnitCode != "",
	})
	writeJSON(w, 201, M{
		"reference": ref,
		"message":   "Your report has been received and routed to INEC incident triage.",
		"captcha":   captchaConfigured(),
	})
}

// handlePublicIncidentMeta advertises channel capabilities (whether captcha
// is required) so the frontend can render the right form. No auth required.
func handlePublicIncidentMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, M{
		"captcha_required": captchaConfigured(),
		"incident_types":   []string{"violence", "ballot_box_snatching", "vote_buying", "voter_intimidation", "missing_materials", "late_opening", "overvoting", "result_falsification", "accessibility_barrier", "other"},
		"rate_limit":       M{"limit": publicIncidentRateLimit, "window_seconds": int(publicIncidentRateWindow.Seconds())},
	})
}
