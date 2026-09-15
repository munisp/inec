// R5-039 regression tests: primaries management routes must be gated by the
// server-derived GOTV role (set by the auth middleware, R5-036). FAILS on
// the pre-fix code (no role predicates existed at all).
package main

import (
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
