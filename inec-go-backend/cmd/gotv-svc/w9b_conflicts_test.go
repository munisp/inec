package main

// W8 handoff (PG-backed): parked GOTV sync conflicts are ingested
// idempotently, listed, and resolved (accept_client applies the parked
// door-knock; accept_server closes the conflict) — nothing silently dropped.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func mustJSON(t *testing.T, v interface{}) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return &buf
}

func conflictCall(t *testing.T, handler func(http.ResponseWriter, *http.Request), method, path string, body interface{}, vars map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, mustJSON(t, body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("X-GOTV-Party-ID", "1")
	req.Header.Set("X-GOTV-User", "w9b-vol")
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestSyncConflictIngestAndResolve(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}
	localID := fmt.Sprintf("local-%d", time.Now().UnixNano())
	conflict := map[string]interface{}{
		"local_id": localID, "contact_id": "w9b-contact", "shift_id": "shift-1",
		"outcome": "pledged", "notes": "voter pledged at door",
		"latitude": 6.5, "longitude": 3.4,
		"timestamp":       time.Now().UTC().Add(-96 * time.Hour).Format(time.RFC3339), // genuinely old (device was offline)
		"server_knock_id": "knock-existing",
		"conflict_reason": "server returned 409 duplicate",
	}
	ingest := func() *httptest.ResponseRecorder {
		return conflictCall(t, handleMobileSyncConflictsIngest, "POST", "/gotv/mobile/sync/conflicts",
			map[string]interface{}{"conflicts": []interface{}{conflict}}, nil)
	}

	// 1. Ingest → accepted, persisted pending.
	rec := ingest()
	if rec.Code != 200 {
		t.Fatalf("ingest returned %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Accepted int `json:"accepted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Accepted != 1 {
		t.Fatalf("expected accepted=1, got %s (err=%v)", rec.Body.String(), err)
	}

	// 2. Re-ingest same local_id → duplicate (idempotent), exactly one row.
	rec = ingest()
	var resp2 struct {
		Duplicates int `json:"duplicates"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp2)
	if resp2.Duplicates != 1 {
		t.Fatalf("re-ingest must be idempotent duplicate, got %s", rec.Body.String())
	}
	var rows int
	if err := dbConn.QueryRow(
		`SELECT COUNT(*) FROM gotv_sync_conflicts WHERE party_id=1 AND volunteer_id='w9b-vol' AND local_id=$1`, localID,
	).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("expected exactly 1 conflict row, rows=%d err=%v", rows, err)
	}

	// 3. List pending shows it.
	rec = conflictCall(t, handleMobileSyncConflictsList, "GET", "/gotv/mobile/sync/conflicts?status=pending", nil, nil)
	var list struct {
		Conflicts []struct {
			ConflictID string `json:"conflict_id"`
			Status     string `json:"status"`
		} `json:"conflicts"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	var conflictID string
	for _, c := range list.Conflicts {
		if c.Status == "pending" {
			conflictID = c.ConflictID
		}
	}
	if conflictID == "" {
		t.Fatalf("pending conflict not listed: %s", rec.Body.String())
	}

	// 4. Resolve accept_client → parked knock applied, conflict closed.
	rec = conflictCall(t, handleMobileSyncConflictResolve, "POST",
		"/gotv/mobile/sync/conflicts/"+conflictID+"/resolve",
		map[string]interface{}{"resolution": "accept_client", "note": "verified with volunteer"},
		map[string]string{"id": conflictID})
	if rec.Code != 200 {
		t.Fatalf("resolve returned %d: %s", rec.Code, rec.Body.String())
	}
	var status, knockContact string
	if err := dbConn.QueryRow(`SELECT status FROM gotv_sync_conflicts WHERE conflict_id=$1`, conflictID).Scan(&status); err != nil {
		t.Fatalf("read conflict: %v", err)
	}
	if status != "resolved_client" {
		t.Fatalf("expected resolved_client, got %s", status)
	}
	if err := dbConn.QueryRow(
		`SELECT contact_id FROM gotv_door_knocks WHERE party_id=1 AND volunteer_id='w9b-vol' AND contact_id='w9b-contact' ORDER BY knocked_at DESC LIMIT 1`,
	).Scan(&knockContact); err != nil {
		t.Fatalf("accept_client must apply the parked door-knock: %v", err)
	}

	// 5. Double resolve → 409 (state machine, not blind update).
	rec = conflictCall(t, handleMobileSyncConflictResolve, "POST",
		"/gotv/mobile/sync/conflicts/"+conflictID+"/resolve",
		map[string]interface{}{"resolution": "accept_server"},
		map[string]string{"id": conflictID})
	if rec.Code != 409 {
		t.Fatalf("double resolve must be 409, got %d: %s", rec.Code, rec.Body.String())
	}

	// 6. Invalid conflict (unparseable timestamp) is rejected per-record.
	rec = conflictCall(t, handleMobileSyncConflictsIngest, "POST", "/gotv/mobile/sync/conflicts",
		map[string]interface{}{"conflicts": []interface{}{map[string]interface{}{
			"local_id": "bad-1", "contact_id": "c", "outcome": "home", "timestamp": "not-a-time",
		}}}, nil)
	var resp3 struct {
		Invalid int `json:"invalid"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp3)
	if resp3.Invalid != 1 {
		t.Fatalf("invalid payload must be counted, got %s", rec.Body.String())
	}
}
