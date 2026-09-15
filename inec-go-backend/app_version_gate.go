package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// W8 server gate (R5 mobile hardening): minimum supported app version.
// Mobile clients send X-App-Version on every request; a client below
// MIN_APP_VERSION is rejected loudly with 426 Upgrade Required so a
// compromised/vulnerable build cannot keep talking to election APIs.
//
// Config: MIN_APP_VERSION (e.g. "1.4.0").
//   - set:     enforced for every request carrying X-App-Version.
//   - unset:   fail-open in non-production (dev convenience, warned at
//     startup); fail-CLOSED in production — startup logs a SECURITY
//     error and versioned requests are rejected rather than silently
//     skipping the control.
var minAppVersion = strings.TrimSpace(os.Getenv("MIN_APP_VERSION"))

func initAppVersionGate() {
	if minAppVersion == "" {
		if isProductionLike() {
			log.Error().Msg("SECURITY: MIN_APP_VERSION not set in production — app-version gate will REJECT versioned mobile requests (fail closed)")
		} else {
			log.Warn().Msg("MIN_APP_VERSION not set — app-version gate disabled outside production")
		}
	}
}

// compareAppVersions compares dotted numeric versions ("1.4.0" vs "1.10").
// Returns -1/0/+1. Unparseable input compares as OLDER (fail closed).
func compareAppVersions(a, b string) int {
	pa, okA := parseAppVersion(a)
	pb, okB := parseAppVersion(b)
	if !okA && !okB {
		return 0
	}
	if !okA {
		return -1
	}
	if !okB {
		return 1
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func parseAppVersion(v string) ([]int, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// minAppVersionMiddleware rejects requests whose X-App-Version is below the
// configured minimum. Requests without the header (browsers, USSD/SMS
// gateways, server integrations) are unaffected.
func minAppVersionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := r.Header.Get("X-App-Version")
		if v == "" {
			next.ServeHTTP(w, r)
			return
		}
		if minAppVersion == "" {
			if isProductionLike() {
				// Fail closed: the control is unconfigured in production, so
				// no version claim is trusted.
				writeAppVersionRejected(w, "app version policy unavailable", "")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if compareAppVersions(v, minAppVersion) < 0 {
			log.Warn().Str("app_version", v).Str("min_version", minAppVersion).
				Str("path", r.URL.Path).Str("remote", r.RemoteAddr).
				Msg("rejected outdated app version")
			writeAppVersionRejected(w, "app version below the minimum supported version — please update", minAppVersion)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeAppVersionRejected(w http.ResponseWriter, msg, min string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUpgradeRequired) // 426
	body := M{"error": msg}
	if min != "" {
		body["min_version"] = min
	}
	json.NewEncoder(w).Encode(body)
}
