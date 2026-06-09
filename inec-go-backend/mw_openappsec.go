package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// OpenAppSecClient provides Web Application Firewall (WAF) capabilities:
// request inspection, bot detection, threat intelligence, and IP reputation.
type OpenAppSecClient interface {
	InspectRequest(ctx context.Context, req WAFRequest) (*WAFDecision, error)
	GetThreatLog(ctx context.Context, limit int) ([]WAFEvent, error)
	GetStats(ctx context.Context) (*WAFStats, error)
	AddIPToBlocklist(ctx context.Context, ip, reason string) error
	GetBlocklist(ctx context.Context) ([]BlocklistEntry, error)
	Status() MWStatus
	Close() error
}

type WAFRequest struct {
	SourceIP    string            `json:"source_ip"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	QueryString string            `json:"query_string,omitempty"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body,omitempty"`
	UserAgent   string            `json:"user_agent"`
}

type WAFDecision struct {
	Action       string   `json:"action"` // allow, block, challenge
	ThreatLevel  string   `json:"threat_level"`
	RulesMatched []string `json:"rules_matched"`
	Score        int      `json:"score"`
	RequestID    string   `json:"request_id"`
}

type WAFEvent struct {
	ID          int    `json:"id"`
	RequestID   string `json:"request_id"`
	SourceIP    string `json:"source_ip"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	RuleID      string `json:"rule_id"`
	Action      string `json:"action"`
	ThreatLevel string `json:"threat_level"`
	Details     string `json:"details"`
	Timestamp   string `json:"timestamp"`
}

type WAFStats struct {
	TotalRequests    int            `json:"total_requests"`
	BlockedRequests  int            `json:"blocked_requests"`
	ThreatsByLevel   map[string]int `json:"threats_by_level"`
	TopBlockedIPs    []IPCount      `json:"top_blocked_ips"`
	TopAttackVectors []AttackVector `json:"top_attack_vectors"`
}

type IPCount struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
}

type AttackVector struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type BlocklistEntry struct {
	IP        string `json:"ip"`
	Reason    string `json:"reason"`
	AddedAt   string `json:"added_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// Embedded OpenAppSec WAF with SQL injection, XSS, path traversal detection
type embeddedWAF struct {
	mu        sync.RWMutex
	blocklist map[string]BlocklistEntry
}

func newEmbeddedWAF() *embeddedWAF {
	w := &embeddedWAF{
		blocklist: make(map[string]BlocklistEntry),
	}
	// Load persisted blocklist from DB
	if db != nil {
		rows, err := db.Query(`SELECT ip_address, reason, blocked_at FROM waf_blocklist`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var e BlocklistEntry
				if rows.Scan(&e.IP, &e.Reason, &e.AddedAt) == nil {
					w.blocklist[e.IP] = e
				}
			}
			if len(w.blocklist) > 0 {
				log.Info().Int("count", len(w.blocklist)).Msg("WAF: loaded blocklist from DB")
			}
		}
	}
	return w
}

var sqlInjectionPatterns = []string{
	"' OR ", "' AND ", "UNION SELECT", "UNION ALL SELECT", "DROP TABLE", "DELETE FROM",
	"INSERT INTO", "UPDATE SET", "--", "/*", "*/", "xp_cmdshell",
	"EXEC(", "EXECUTE(", "WAITFOR DELAY", "BENCHMARK(",
	"HAVING ", "GROUP BY", "ORDER BY", "INFORMATION_SCHEMA",
	"LOAD_FILE(", "INTO OUTFILE", "INTO DUMPFILE", "CHAR(",
	"0x", "UNHEX(", "CONCAT(", "SUBSTR(", "SUBSTRING(",
}

var xssPatterns = []string{
	"<script", "javascript:", "onerror=", "onload=", "eval(",
	"document.cookie", "window.location", "innerHTML",
	"onfocus=", "onmouseover=", "onclick=", "onchange=",
	"<iframe", "<object", "<embed", "<svg", "vbscript:",
	"expression(", "url(", "import(", "<img src",
}

var pathTraversalPatterns = []string{
	"../", "..\\", "%2e%2e", "%252e%252e",
	"/etc/passwd", "/etc/shadow", "cmd.exe",
	"/proc/self", "/dev/null", "\\windows\\system32",
	"%00", "%0a", "%0d",
}

// normalizeInput decodes URL encoding, hex encoding, and Unicode escapes
// to prevent bypass via encoded payloads.
func normalizeInput(input string) string {
	// First pass: percent-decode
	decoded := input
	for i := 0; i < 3; i++ { // Up to 3 rounds of decoding (double/triple encoding)
		prev := decoded
		if d, err := url.QueryUnescape(decoded); err == nil {
			decoded = d
		}
		if decoded == prev {
			break
		}
	}
	// Remove null bytes
	decoded = strings.ReplaceAll(decoded, "\x00", "")
	// Normalize whitespace (collapse multiple spaces, tabs, newlines)
	decoded = strings.Join(strings.Fields(decoded), " ")
	return decoded
}

func (w *embeddedWAF) InspectRequest(ctx context.Context, req WAFRequest) (*WAFDecision, error) {
	decision := &WAFDecision{
		Action:      "allow",
		ThreatLevel: "none",
		Score:       0,
		RequestID:   fmt.Sprintf("waf-%d", time.Now().UnixNano()),
	}

	// Check blocklist
	w.mu.RLock()
	if _, blocked := w.blocklist[req.SourceIP]; blocked {
		w.mu.RUnlock()
		decision.Action = "block"
		decision.ThreatLevel = "critical"
		decision.RulesMatched = append(decision.RulesMatched, "IP_BLOCKLIST")
		decision.Score = 100
		w.logEvent(ctx, decision, req)
		return decision, nil
	}
	w.mu.RUnlock()

	// Normalize input to prevent encoding-based bypasses
	rawStr := req.Path + " " + req.Body + " " + req.UserAgent + " " + req.QueryString
	checkStr := strings.ToUpper(normalizeInput(rawStr))

	// SQL Injection detection
	for _, pattern := range sqlInjectionPatterns {
		if strings.Contains(checkStr, strings.ToUpper(pattern)) {
			decision.RulesMatched = append(decision.RulesMatched, "SQLI_"+pattern)
			decision.Score += 40
		}
	}

	// XSS detection
	for _, pattern := range xssPatterns {
		if strings.Contains(checkStr, strings.ToUpper(pattern)) {
			decision.RulesMatched = append(decision.RulesMatched, "XSS_DETECTED")
			decision.Score += 30
		}
	}

	// Path traversal detection
	for _, pattern := range pathTraversalPatterns {
		if strings.Contains(checkStr, strings.ToUpper(pattern)) {
			decision.RulesMatched = append(decision.RulesMatched, "PATH_TRAVERSAL")
			decision.Score += 50
		}
	}

	// Determine action based on score
	switch {
	case decision.Score >= 80:
		decision.Action = "block"
		decision.ThreatLevel = "critical"
	case decision.Score >= 40:
		decision.Action = "block"
		decision.ThreatLevel = "high"
	case decision.Score >= 20:
		decision.Action = "challenge"
		decision.ThreatLevel = "medium"
	case decision.Score > 0:
		decision.ThreatLevel = "low"
	}

	if decision.Score > 0 {
		w.logEvent(ctx, decision, req)
	}

	return decision, nil
}

func (w *embeddedWAF) logEvent(ctx context.Context, decision *WAFDecision, req WAFRequest) {
	if db == nil {
		return
	}
	rulesJSON, _ := json.Marshal(decision.RulesMatched)
	db.ExecContext(ctx,
		`INSERT INTO mw_waf_events (request_id, source_ip, method, path, rule_id, action, threat_level, details)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		decision.RequestID, req.SourceIP, req.Method, req.Path,
		string(rulesJSON), decision.Action, decision.ThreatLevel,
		fmt.Sprintf("score=%d", decision.Score))
}

func (w *embeddedWAF) GetThreatLog(ctx context.Context, limit int) ([]WAFEvent, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, request_id, source_ip, method, path, rule_id, action, threat_level, details, created_at
		 FROM mw_waf_events ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []WAFEvent
	for rows.Next() {
		var ev WAFEvent
		if err := rows.Scan(&ev.ID, &ev.RequestID, &ev.SourceIP, &ev.Method, &ev.Path,
			&ev.RuleID, &ev.Action, &ev.ThreatLevel, &ev.Details, &ev.Timestamp); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events, nil
}

func (w *embeddedWAF) GetStats(ctx context.Context) (*WAFStats, error) {
	stats := &WAFStats{
		ThreatsByLevel: make(map[string]int),
	}

	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mw_waf_events`).Scan(&stats.TotalRequests)
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mw_waf_events WHERE action='block'`).Scan(&stats.BlockedRequests)

	rows, _ := db.QueryContext(ctx, `SELECT threat_level, COUNT(*) FROM mw_waf_events GROUP BY threat_level`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var level string
			var count int
			rows.Scan(&level, &count)
			stats.ThreatsByLevel[level] = count
		}
	}

	rows2, _ := db.QueryContext(ctx,
		`SELECT source_ip, COUNT(*) as cnt FROM mw_waf_events WHERE action='block' GROUP BY source_ip ORDER BY cnt DESC LIMIT 10`)
	if rows2 != nil {
		defer rows2.Close()
		for rows2.Next() {
			var ip string
			var count int
			rows2.Scan(&ip, &count)
			stats.TopBlockedIPs = append(stats.TopBlockedIPs, IPCount{IP: ip, Count: count})
		}
	}

	return stats, nil
}

func (w *embeddedWAF) AddIPToBlocklist(ctx context.Context, ip, reason string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	entry := BlocklistEntry{
		IP:      ip,
		Reason:  reason,
		AddedAt: time.Now().UTC().Format(time.RFC3339),
	}
	w.blocklist[ip] = entry
	// Persist to DB
	if db != nil {
		_, err := db.Exec(`INSERT INTO waf_blocklist (ip_address, reason, blocked_at) VALUES (?, ?, ?)
			ON CONFLICT(ip_address) DO UPDATE SET reason=excluded.reason, blocked_at=excluded.blocked_at`,
			ip, reason, entry.AddedAt)
		if err != nil {
			log.Warn().Err(err).Str("ip", ip).Msg("waf: failed to persist blocklist entry")
		}
	}
	return nil
}

func (w *embeddedWAF) GetBlocklist(ctx context.Context) ([]BlocklistEntry, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var entries []BlocklistEntry
	for _, e := range w.blocklist {
		entries = append(entries, e)
	}
	return entries, nil
}

func (w *embeddedWAF) Status() MWStatus {
	return MWStatus{
		Name:      "OpenAppSec",
		Connected: true,
		Mode:      "embedded (rule-based WAF)",
		Details:   "SQLi, XSS, path traversal detection",
	}
}

func (w *embeddedWAF) Close() error { return nil }

// --- Real OpenAppSec HTTP client ---

type openAppSecHTTPClient struct {
	baseURL string
	client  *ResilientHTTPClient
}

func (o *openAppSecHTTPClient) InspectRequest(ctx context.Context, req WAFRequest) (*WAFDecision, error) {
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/v1/inspect", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var decision WAFDecision
	json.NewDecoder(resp.Body).Decode(&decision)
	return &decision, nil
}

func (o *openAppSecHTTPClient) GetThreatLog(ctx context.Context, limit int) ([]WAFEvent, error) {
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/v1/threats?limit=%d", o.baseURL, limit), nil)
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Events []WAFEvent `json:"events"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Events, nil
}

func (o *openAppSecHTTPClient) GetStats(ctx context.Context) (*WAFStats, error) {
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/api/v1/stats", nil)
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var stats WAFStats
	json.NewDecoder(resp.Body).Decode(&stats)
	return &stats, nil
}

func (o *openAppSecHTTPClient) AddIPToBlocklist(ctx context.Context, ip, reason string) error {
	body, _ := json.Marshal(map[string]string{"ip": ip, "reason": reason})
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/v1/blocklist", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (o *openAppSecHTTPClient) GetBlocklist(ctx context.Context) ([]BlocklistEntry, error) {
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/api/v1/blocklist", nil)
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Entries []BlocklistEntry `json:"entries"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Entries, nil
}

func (o *openAppSecHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/api/v1/health", nil)
	lat, err := measureLatency(func() error {
		resp, e := o.client.Client.Do(httpReq)
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "OpenAppSec", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "OpenAppSec", Connected: true, Mode: "external", Latency: fmtLatency(lat)}
}

func (o *openAppSecHTTPClient) Close() error { return nil }

func initOpenAppSecClient() OpenAppSecClient {
	baseURL := os.Getenv("OPENAPPSEC_URL")
	if baseURL != "" {
		client := &openAppSecHTTPClient{
			baseURL: baseURL,
			client:  NewResilientHTTPClient("openappsec"),
		}
		s := client.Status()
		if s.Connected {
			log.Info().Str("url", baseURL).Msg("OpenAppSec connected via HTTP")
			return client
		}
		log.Warn().Str("url", baseURL).Msg("OpenAppSec unreachable, falling back to embedded WAF")
	}
	env := os.Getenv("APP_ENV")
	if env == "production" || env == "staging" {
		log.Fatal().Msg("OpenAppSec WAF is REQUIRED in production/staging. Set OPENAPPSEC_URL")
	}
	log.Warn().Msg("OpenAppSec using embedded rule-based WAF (DEV ONLY)")
	return newEmbeddedWAF()
}

// WAF middleware for HTTP request inspection
func wafMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mwHub == nil || mwHub.OpenAppSec == nil {
			next.ServeHTTP(w, r)
			return
		}

		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}

		// Build full inspection path: URL path + decoded query params
		fullPath := r.URL.Path
		if r.URL.RawQuery != "" {
			decoded, err := url.QueryUnescape(r.URL.RawQuery)
			if err == nil {
				fullPath += "?" + decoded
			} else {
				fullPath += "?" + r.URL.RawQuery
			}
		}

		// Read request body for POST/PUT/PATCH inspection (limit to 64KB)
		// Skip body inspection for multipart/form-data (file uploads contain binary
		// content that triggers false positives on pattern matching).
		var bodyStr string
		ct := r.Header.Get("Content-Type")
		isMultipart := strings.HasPrefix(ct, "multipart/form-data")
		if !isMultipart && (r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH") {
			bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, 65536))
			r.Body.Close()
			bodyStr = string(bodyBytes)
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		wafReq := WAFRequest{
			SourceIP:  ip,
			Method:    r.Method,
			Path:      fullPath,
			UserAgent: r.UserAgent(),
			Body:      bodyStr,
		}

		decision, err := mwHub.OpenAppSec.InspectRequest(r.Context(), wafReq)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		if decision.Action == "block" {
			writeJSON(w, 403, M{
				"error":        "request blocked by WAF",
				"threat_level": decision.ThreatLevel,
				"request_id":   decision.RequestID,
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// HTTP handlers
func handleWAFInspect(w http.ResponseWriter, r *http.Request) {
	var req WAFRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	decision, err := mwHub.OpenAppSec.InspectRequest(r.Context(), req)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, decision)
}

func handleWAFThreatLog(w http.ResponseWriter, r *http.Request) {
	limit := queryParamInt(r, "limit", 50)
	events, err := mwHub.OpenAppSec.GetThreatLog(r.Context(), limit)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if events == nil {
		events = []WAFEvent{}
	}
	writeJSON(w, 200, M{"events": events, "count": len(events)})
}

func handleWAFStats(w http.ResponseWriter, r *http.Request) {
	stats, err := mwHub.OpenAppSec.GetStats(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, stats)
}

func handleWAFBlocklist(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			IP     string `json:"ip"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, 400, "invalid request body")
			return
		}
		if err := mwHub.OpenAppSec.AddIPToBlocklist(r.Context(), req.IP, req.Reason); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 201, M{"blocked": true, "ip": req.IP})
		return
	}

	entries, err := mwHub.OpenAppSec.GetBlocklist(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if entries == nil {
		entries = []BlocklistEntry{}
	}
	writeJSON(w, 200, M{"blocklist": entries})
}

func handleWAFStatus(w http.ResponseWriter, r *http.Request) {
	status := mwHub.OpenAppSec.Status()
	writeJSON(w, 200, M{
		"name":      status.Name,
		"connected": status.Connected,
		"mode":      status.Mode,
		"details":   status.Details,
		"rules":     []string{"SQL_INJECTION", "XSS", "PATH_TRAVERSAL", "IP_BLOCKLIST"},
	})
}
