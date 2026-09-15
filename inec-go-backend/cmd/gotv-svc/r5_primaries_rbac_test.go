// R5-039 regression tests: primaries management routes must be gated by the
// server-derived GOTV role (set by the auth middleware, R5-036). FAILS on
// the pre-fix code (no role predicates existed at all).
package main

import (
	"github.com/gorilla/mux"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestR5039_RequirePrimaryRole(t *testing.T) {
	called := false
	inner := func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}
	handler := requirePrimaryRole(inner, RolePartyAdmin)

	cases := []struct {
		name     string
		role     string
		wantCode int
		wantCall bool
	}{
		{"no role (unauthenticated membership) fails closed", "", http.StatusUnauthorized, false},
		{"party_admin allowed", "party_admin", 200, true},
		{"coordinator denied from returning-officer op", "coordinator", http.StatusForbidden, false},
		{"field_worker denied", "field_worker", http.StatusForbidden, false},
		{"observer denied (audit-read only)", "observer", http.StatusForbidden, false},
		{"analyst denied", "analyst", http.StatusForbidden, false},
		{"unknown role denied", "superadmin", http.StatusForbidden, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest("POST", "/gotv/primaries/rounds/1/tally", nil)
			if tc.role != "" {
				req.Header.Set("X-GOTV-Role", tc.role)
			}
			w := httptest.NewRecorder()
			handler(w, req)
			if w.Code != tc.wantCode {
				t.Errorf("code=%d, want %d", w.Code, tc.wantCode)
			}
			if called != tc.wantCall {
				t.Errorf("handler called=%v, want %v", called, tc.wantCall)
			}
		})
	}
}

func TestR5039_DeskGroupAdmitsCoordinator(t *testing.T) {
	called := false
	handler := requirePrimaryRole(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}, RolePartyAdmin, RoleCoordinator)
	req := httptest.NewRequest("POST", "/gotv/primaries/delegates", nil)
	req.Header.Set("X-GOTV-Role", "coordinator")
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != 200 || !called {
		t.Fatalf("registration-desk op should admit coordinator, got %d", w.Code)
	}
}

// TestR5039_PrimariesRoutesUnauthenticatedRejected: end-to-end through the
// registered routes — an authenticated-but-roleless caller (party identity
// present, no server-issued role) is rejected from every management verb.
func TestR5039_PrimariesRoutesUnauthenticatedRejected(t *testing.T) {
	r := mux.NewRouter()
	// Simulate the auth middleware having authenticated a party identity
	// WITHOUT issuing a role (no membership row — fail closed, R5-036).
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			r.Header.Del("X-GOTV-Role") // middleware strips client value
			r.Header.Set("X-GOTV-Party-ID", "1")
			r.Header.Set("X-GOTV-User", "user@party.ng")
			next(w, r)
		}
	}
	registerPrimaryRoutes(r, auth)

	management := []struct{ method, path string }{
		{"POST", "/gotv/primaries/aspirants"},
		{"POST", "/gotv/primaries/delegates"},
		{"POST", "/gotv/primaries/delegates/d1/accredit"},
		{"POST", "/gotv/primaries/rounds"},
		{"POST", "/gotv/primaries/rounds/1/open"},
		{"POST", "/gotv/primaries/rounds/1/tally"},
		{"POST", "/gotv/primaries/rounds/1/certify"},
		{"POST", "/gotv/primaries/crypto/keys"},
		{"POST", "/gotv/primaries/crypto/decrypt"},
		{"POST", "/gotv/primaries/disputes/1/resolve"},
	}
	for _, tc := range management {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		// Client ALSO spoofs a role header — middleware must have stripped it.
		req.Header.Set("X-GOTV-Role", "party_admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: roleless caller got %d, want 401", tc.method, tc.path, w.Code)
		}
	}
}
