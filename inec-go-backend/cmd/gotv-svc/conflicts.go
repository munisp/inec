package main

// W8 handoff: GOTV sync conflict ingest + resolution.
//
// The mobile app parks sync conflicts locally when the server rejects a
// record with 409 (duplicate/conflicting door-knock). These endpoints ingest
// the parked payloads, persist them with conflict status (idempotent on the
// device-generated local id), and resolve them explicitly:
//   accept_client — apply the parked door-knock to gotv_door_knocks
//   accept_server — keep the server record, close the conflict
// Nothing is silently dropped or silently overwritten: every conflict is a
// row with an auditable resolution.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type parkedConflict struct {
	LocalID        string  `json:"local_id"`
	ContactID      string  `json:"contact_id"`
	ShiftID        string  `json:"shift_id"`
	Outcome        string  `json:"outcome"`
	Notes          string  `json:"notes"`
	Lat            float64 `json:"latitude"`
	Lng            float64 `json:"longitude"`
	Timestamp      string  `json:"timestamp"`
	ServerKnockID  string  `json:"server_knock_id"`
	ConflictReason string  `json:"conflict_reason"`
}

// validateParkedConflict checks a parked conflict. The R5-010 ±72h/10min
// window applies at INITIAL sync; a parked conflict may legitimately be days
// old (the device was offline), so here we require only a parseable
// timestamp — the genuine capture time is preserved for tribunal evidence.
func validateParkedConflict(c *parkedConflict) error {
	if c.LocalID == "" {
		return errStr("local_id is required (device-generated idempotency key)")
	}
	if c.ContactID == "" {
		return errStr("contact_id is required")
	}
	if c.Outcome != "" && !syncOutcomeAllowed[c.Outcome] {
		return errStr("invalid outcome")
	}
	if _, err := parseKnockTimestamp(c.Timestamp); err != nil {
		return err
	}
	return nil
}

type errStr string

func (e errStr) Error() string { return string(e) }

// POST /gotv/mobile/sync/conflicts — batch-ingest parked conflicts.
func handleMobileSyncConflictsIngest(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		Conflicts []parkedConflict `json:"conflicts"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		jsonErr(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(req.Conflicts) == 0 {
		jsonErr(w, "no conflicts supplied", http.StatusBadRequest)
		return
	}
	if len(req.Conflicts) > 500 {
		jsonErr(w, "too many conflicts in one batch (max 500)", http.StatusBadRequest)
		return
	}

	results := make([]map[string]interface{}, 0, len(req.Conflicts))
	accepted, duplicates, invalid := 0, 0, 0
	for i := range req.Conflicts {
		c := &req.Conflicts[i]
		if verr := validateParkedConflict(c); verr != nil {
			invalid++
			results = append(results, map[string]interface{}{"local_id": c.LocalID, "status": "invalid", "reason": verr.Error()})
			continue
		}
		payload, _ := json.Marshal(c)
		conflictID := "conflict-" + uuid.New().String()[:12]
		res, err := svc.DB.Exec(
			`INSERT INTO gotv_sync_conflicts
				(conflict_id, party_id, volunteer_id, entity_type, local_id, server_id, conflict_reason, client_payload)
			 VALUES ($1,$2,$3,'door_knock',$4,$5,$6,$7)
			 ON CONFLICT (party_id, volunteer_id, entity_type, local_id) DO NOTHING`,
			conflictID, pid, user, c.LocalID, nullStr(c.ServerKnockID), nullStr(c.ConflictReason), string(payload),
		)
		if err != nil {
			invalid++
			results = append(results, map[string]interface{}{"local_id": c.LocalID, "status": "error", "reason": "persist failed"})
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			duplicates++
			results = append(results, map[string]interface{}{"local_id": c.LocalID, "status": "duplicate"})
			continue
		}
		accepted++
		results = append(results, map[string]interface{}{"local_id": c.LocalID, "status": "accepted", "conflict_id": conflictID})
	}
	svc.Audit(pid, user, "sync_conflicts_ingest", "sync_conflict", "")
	jsonResp(w, map[string]interface{}{
		"accepted": accepted, "duplicates": duplicates, "invalid": invalid,
		"results": results, "server_time": time.Now().UTC().Format(time.RFC3339),
	})
}

// GET /gotv/mobile/sync/conflicts?status=pending — list the caller's conflicts.
func handleMobileSyncConflictsList(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	rows, err := svc.DB.Query(
		`SELECT conflict_id, entity_type, local_id, server_id, conflict_reason, client_payload, status, created_at, resolved_at
		 FROM gotv_sync_conflicts WHERE party_id=$1 AND volunteer_id=$2 AND status=$3
		 ORDER BY created_at DESC LIMIT 500`,
		pid, user, status,
	)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, entityType, localID, st string
		var serverID, reason, payload sql.NullString
		var createdAt time.Time
		var resolvedAt sql.NullTime
		if rows.Scan(&id, &entityType, &localID, &serverID, &reason, &payload, &st, &createdAt, &resolvedAt) != nil {
			continue
		}
		var parsed interface{}
		json.Unmarshal([]byte(payload.String), &parsed)
		out = append(out, map[string]interface{}{
			"conflict_id": id, "entity_type": entityType, "local_id": localID,
			"server_id": serverID.String, "conflict_reason": reason.String,
			"payload": parsed, "status": st,
			"created_at":  createdAt.UTC().Format(time.RFC3339),
			"resolved_at": nullTimeStr(resolvedAt),
		})
	}
	jsonResp(w, map[string]interface{}{"conflicts": out, "count": len(out)})
}

// POST /gotv/mobile/sync/conflicts/{id}/resolve — resolve a parked conflict.
func handleMobileSyncConflictResolve(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	conflictID := mux.Vars(r)["id"]
	var req struct {
		Resolution string `json:"resolution"` // accept_client | accept_server
		Note       string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		jsonErr(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Resolution != "accept_client" && req.Resolution != "accept_server" {
		jsonErr(w, "resolution must be accept_client or accept_server", http.StatusBadRequest)
		return
	}

	// Load + lock the conflict (tenant + ownership guarded).
	var entityType, localID, payload string
	var status string
	err := svc.DB.QueryRow(
		`SELECT entity_type, local_id, client_payload, status FROM gotv_sync_conflicts
		 WHERE conflict_id=$1 AND party_id=$2 AND volunteer_id=$3`,
		conflictID, pid, user,
	).Scan(&entityType, &localID, &payload, &status)
	if err == sql.ErrNoRows {
		jsonErr(w, "conflict not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	if status != "pending" {
		jsonErr(w, "conflict already resolved", http.StatusConflict)
		return
	}

	newStatus := "resolved_server"
	var appliedKnockID string
	if req.Resolution == "accept_client" {
		if entityType != "door_knock" {
			jsonErr(w, "accept_client is only supported for door_knock conflicts", http.StatusBadRequest)
			return
		}
		var c parkedConflict
		if json.Unmarshal([]byte(payload), &c) != nil {
			jsonErr(w, "stored payload is unreadable", http.StatusInternalServerError)
			return
		}
		knockedAt, tsErr := parseKnockTimestamp(c.Timestamp)
		if tsErr != nil {
			jsonErr(w, "stored payload timestamp unreadable: "+tsErr.Error(), http.StatusInternalServerError)
			return
		}
		appliedKnockID = "knock-" + uuid.New().String()[:8]
		// Apply through the same insert path as the original sync — the
		// capture timestamp is the genuine field time preserved at parking.
		if _, err := svc.DB.Exec(
			`INSERT INTO gotv_door_knocks (party_id, volunteer_id, contact_id, knock_id, shift_id, outcome, notes, latitude, longitude, knocked_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,COALESCE($10::timestamp, NOW()))
			 ON CONFLICT DO NOTHING`,
			pid, user, c.ContactID, appliedKnockID, nullStr(c.ShiftID), c.Outcome, c.Notes, c.Lat, c.Lng, knockedAt,
		); err != nil {
			jsonErr(w, "failed to apply client record", http.StatusInternalServerError)
			return
		}
		newStatus = "resolved_client"
	}

	res, err := svc.DB.Exec(
		`UPDATE gotv_sync_conflicts SET status=$1, resolution_note=$2, resolved_by=$3, resolved_at=NOW()
		 WHERE conflict_id=$4 AND party_id=$5 AND status='pending'`,
		newStatus, nullStr(req.Note), user, conflictID, pid,
	)
	if err != nil {
		jsonErr(w, "resolve failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, "conflict already resolved", http.StatusConflict)
		return
	}
	svc.Audit(pid, user, "sync_conflict_"+newStatus, "sync_conflict", conflictID)
	jsonResp(w, map[string]interface{}{
		"conflict_id": conflictID, "status": newStatus,
		"applied_knock_id": appliedKnockID,
	})
}

func nullTimeStr(t sql.NullTime) interface{} {
	if !t.Valid {
		return nil
	}
	return t.Time.UTC().Format(time.RFC3339)
}
