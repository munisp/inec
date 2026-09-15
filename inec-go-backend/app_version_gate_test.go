package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareAppVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.4.0", "1.4.0", 0},
		{"1.4.0", "1.4.1", -1},
		{"1.10.0", "1.4.9", 1}, // numeric, not lexical
		{"1.4", "1.4.0", 0},    // missing segment == 0
		{"2.0.0", "1.99.99", 1},
		{"garbage", "1.0.0", -1}, // unparseable = older (fail closed)
		{"v1.2.3", "1.2.3", 0},
	}
	for _, c := range cases {
		if got := compareAppVersions(c.a, c.b); got != c.want {
			t.Errorf("compareAppVersions(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestMinAppVersionGate(t *testing.T) {
	prev := minAppVersion
	defer func() { minAppVersion = prev }()
	minAppVersion = "1.4.0"

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	h := minAppVersionMiddleware(ok)

	// Below minimum → 426 with min_version body.
	req := httptest.NewRequest("GET", "/inec/results", nil)
	req.Header.Set("X-App-Version", "1.3.9")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUpgradeRequired {
		t.Fatalf("below-min version: got %d, want 426", w.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("426 body not JSON: %v", err)
	}
	if body["min_version"] != "1.4.0" || body["error"] == nil {
		t.Fatalf("426 body missing error/min_version: %v", body)
	}

	// At/above minimum → pass.
	for _, v := range []string{"1.4.0", "1.4.1", "2.0.0"} {
		req = httptest.NewRequest("GET", "/inec/results", nil)
		req.Header.Set("X-App-Version", v)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 204 {
			t.Fatalf("version %s: got %d, want 204", v, w.Code)
		}
	}

	// No header (browser/gateway traffic) → unaffected.
	req = httptest.NewRequest("GET", "/inec/results", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("no header: got %d, want 204", w.Code)
	}

	// Unparseable version → treated as below minimum (fail closed).
	req = httptest.NewRequest("GET", "/inec/results", nil)
	req.Header.Set("X-App-Version", "dev-build")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUpgradeRequired {
		t.Fatalf("unparseable version: got %d, want 426", w.Code)
	}
}

func TestMinAppVersionGateUnsetNonProd(t *testing.T) {
	prev := minAppVersion
	defer func() { minAppVersion = prev }()
	minAppVersion = ""
	// Non-production (test binary): fail-open.
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	req := httptest.NewRequest("GET", "/inec/results", nil)
	req.Header.Set("X-App-Version", "0.0.1")
	w := httptest.NewRecorder()
	minAppVersionMiddleware(ok).ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("unset minimum outside production must fail open: got %d", w.Code)
	}
}
