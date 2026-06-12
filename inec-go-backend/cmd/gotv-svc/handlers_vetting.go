package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// ─── Volunteer Vetting ─────────────────────────────────────────────────────
//
// Lifecycle: pending → nin_verified → trained → approved → active → suspended
//
// Steps:
//  1. Volunteer registers (status=pending)
//  2. Admin or automated NIN/BVN check (status=nin_verified)
//  3. Training completion flagged (status=trained)
//  4. Coordinator approves (status=approved → active)
//  5. Can be suspended/reactivated by admin

func handleGetVolunteerVetting(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	id := mux.Vars(r)["id"]

	var vid, name, role, vettingStatus string
	var ninVerified, trainingCompleted, backgroundCleared bool
	var ninVerifiedAt, trainingCompletedAt, approvedAt, suspendedAt sql.NullTime
	var approvedBy, suspendedReason sql.NullString

	err := svc.DB.QueryRow(`
		SELECT volunteer_id, full_name, role, vetting_status,
		       nin_verified, nin_verified_at,
		       training_completed, training_completed_at,
		       background_cleared, approved_by, approved_at,
		       suspended_reason, suspended_at
		FROM gotv_volunteers WHERE volunteer_id=$1 AND party_id=$2`, id, pid,
	).Scan(&vid, &name, &role, &vettingStatus,
		&ninVerified, &ninVerifiedAt,
		&trainingCompleted, &trainingCompletedAt,
		&backgroundCleared, &approvedBy, &approvedAt,
		&suspendedReason, &suspendedAt)

	if err != nil {
		jsonErr(w, "volunteer not found", http.StatusNotFound)
		return
	}

	result := map[string]interface{}{
		"volunteer_id":         vid,
		"full_name":            name,
		"role":                 role,
		"vetting_status":       vettingStatus,
		"nin_verified":         ninVerified,
		"training_completed":   trainingCompleted,
		"background_cleared":   backgroundCleared,
	}
	if ninVerifiedAt.Valid {
		result["nin_verified_at"] = ninVerifiedAt.Time
	}
	if trainingCompletedAt.Valid {
		result["training_completed_at"] = trainingCompletedAt.Time
	}
	if approvedBy.Valid {
		result["approved_by"] = approvedBy.String
	}
	if approvedAt.Valid {
		result["approved_at"] = approvedAt.Time
	}
	if suspendedReason.Valid {
		result["suspended_reason"] = suspendedReason.String
	}
	if suspendedAt.Valid {
		result["suspended_at"] = suspendedAt.Time
	}
	jsonResp(w, result)
}

func handleVerifyNIN(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		NIN    string `json:"nin"`
		Result string `json:"result"` // "pass" or "fail"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.NIN == "" {
		jsonErr(w, "nin required", http.StatusBadRequest)
		return
	}

	passed := req.Result != "fail"
	newStatus := "nin_verified"
	if !passed {
		newStatus = "nin_failed"
	}

	res, err := svc.DB.Exec(`
		UPDATE gotv_volunteers
		SET nin_verified=$1, nin_verified_at=NOW(), vetting_status=$2
		WHERE volunteer_id=$3 AND party_id=$4 AND vetting_status='pending'`,
		passed, newStatus, id, pid)
	if err != nil {
		jsonErr(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, "volunteer not found or not in pending status", http.StatusNotFound)
		return
	}

	svc.Audit(pid, user, "verify_nin", "volunteer", id)
	publishEvent(TopicGOTVVolunteerEvent, id, map[string]interface{}{
		"event": "nin_verified", "volunteer_id": id, "passed": passed,
	})
	jsonResp(w, map[string]interface{}{"nin_verified": passed, "vetting_status": newStatus})
}

func handleCompleteTraining(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		TrainingModule string `json:"training_module"`
		Score          int    `json:"score"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	res, err := svc.DB.Exec(`
		UPDATE gotv_volunteers
		SET training_completed=TRUE, training_completed_at=NOW(), vetting_status='trained'
		WHERE volunteer_id=$1 AND party_id=$2 AND vetting_status='nin_verified'`,
		id, pid)
	if err != nil {
		jsonErr(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, "volunteer not found or NIN not yet verified", http.StatusNotFound)
		return
	}

	svc.Audit(pid, user, "complete_training", "volunteer", id)
	jsonResp(w, map[string]interface{}{"training_completed": true, "vetting_status": "trained"})
}

func handleApproveVolunteer(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]

	res, err := svc.DB.Exec(`
		UPDATE gotv_volunteers
		SET vetting_status='approved', approved_by=$1, approved_at=NOW(), is_active=TRUE
		WHERE volunteer_id=$2 AND party_id=$3 AND vetting_status IN ('trained','nin_verified')`,
		user, id, pid)
	if err != nil {
		jsonErr(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, "volunteer not found or not ready for approval", http.StatusNotFound)
		return
	}

	svc.Audit(pid, user, "approve_volunteer", "volunteer", id)
	publishEvent(TopicGOTVVolunteerEvent, id, map[string]interface{}{
		"event": "volunteer_approved", "volunteer_id": id, "approved_by": user,
	})
	jsonResp(w, map[string]interface{}{"approved": true, "vetting_status": "approved"})
}

func handleRejectVolunteer(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	res, err := svc.DB.Exec(`
		UPDATE gotv_volunteers
		SET vetting_status='rejected', is_active=FALSE, suspended_reason=$1, suspended_at=NOW()
		WHERE volunteer_id=$2 AND party_id=$3 AND vetting_status NOT IN ('rejected','suspended')`,
		req.Reason, id, pid)
	if err != nil {
		jsonErr(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, "volunteer not found or already rejected", http.StatusNotFound)
		return
	}

	svc.Audit(pid, user, "reject_volunteer", "volunteer", id)
	jsonResp(w, map[string]interface{}{"rejected": true})
}

func handleSuspendVolunteer(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	res, err := svc.DB.Exec(`
		UPDATE gotv_volunteers
		SET vetting_status='suspended', is_active=FALSE, suspended_reason=$1, suspended_at=NOW()
		WHERE volunteer_id=$2 AND party_id=$3 AND vetting_status IN ('approved','active')`,
		req.Reason, id, pid)
	if err != nil {
		jsonErr(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, "volunteer not found or not active", http.StatusNotFound)
		return
	}

	svc.Audit(pid, user, "suspend_volunteer", "volunteer", id)
	jsonResp(w, map[string]interface{}{"suspended": true})
}

func handleListVettingPipeline(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	statusFilter := r.URL.Query().Get("status")

	query := `
		SELECT volunteer_id, full_name, phone, role, vetting_status,
		       nin_verified, training_completed, background_cleared,
		       assigned_state, assigned_lga, created_at
		FROM gotv_volunteers WHERE party_id=$1`
	args := []interface{}{pid}

	if statusFilter != "" {
		query += " AND vetting_status=$2"
		args = append(args, statusFilter)
	}
	query += " ORDER BY created_at DESC LIMIT 500"

	rows, err := svc.DB.Query(query, args...)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var pipeline []map[string]interface{}
	var counts = map[string]int{
		"pending": 0, "nin_verified": 0, "nin_failed": 0, "trained": 0,
		"approved": 0, "rejected": 0, "suspended": 0,
	}

	for rows.Next() {
		var vid, name, phone, role, vs string
		var ninV, trainC, bgC bool
		var state, lga sql.NullString
		var createdAt time.Time
		rows.Scan(&vid, &name, &phone, &role, &vs, &ninV, &trainC, &bgC, &state, &lga, &createdAt)

		counts[vs]++
		pipeline = append(pipeline, map[string]interface{}{
			"volunteer_id":       vid,
			"full_name":          name,
			"phone":              phone,
			"role":               role,
			"vetting_status":     vs,
			"nin_verified":       ninV,
			"training_completed": trainC,
			"background_cleared": bgC,
			"assigned_state":     nullStrVal(state),
			"assigned_lga":       nullStrVal(lga),
			"created_at":         createdAt,
		})
	}

	jsonResp(w, map[string]interface{}{
		"volunteers": pipeline,
		"total":      len(pipeline),
		"counts":     counts,
	})
}

// ─── Task Assignment ───────────────────────────────────────────────────────
//
// Tasks are discrete work items assigned to volunteers:
//   - door_knock: canvass N doors in a ward
//   - phone_call: call N contacts
//   - ride_duty: be on standby as driver for a time block
//   - event_setup: set up rally/town hall materials
//   - data_collection: collect survey responses

func handleListTasks(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	statusFilter := r.URL.Query().Get("status")
	volunteerFilter := r.URL.Query().Get("volunteer_id")

	query := `
		SELECT t.task_id, t.task_type, t.title, t.description, t.status,
		       t.volunteer_id, COALESCE(v.full_name,''), t.ward_code, t.state_code, t.lga_code,
		       t.target_count, t.completed_count, t.priority, t.due_date,
		       t.started_at, t.completed_at, t.created_at
		FROM gotv_tasks t
		LEFT JOIN gotv_volunteers v ON t.volunteer_id = v.volunteer_id
		WHERE t.party_id=$1`
	args := []interface{}{pid}
	idx := 2

	if statusFilter != "" {
		query += fmt.Sprintf(" AND t.status=$%d", idx)
		args = append(args, statusFilter)
		idx++
	}
	if volunteerFilter != "" {
		query += fmt.Sprintf(" AND t.volunteer_id=$%d", idx)
		args = append(args, volunteerFilter)
		idx++
	}
	query += " ORDER BY t.priority DESC, t.due_date ASC LIMIT 500"

	rows, err := svc.DB.Query(query, args...)
	if err != nil {
		jsonErr(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tasks []map[string]interface{}
	for rows.Next() {
		var taskID, taskType, title, desc, status, volID, volName string
		var wardCode, stateCode, lgaCode sql.NullString
		var targetCount, completedCount, priority int
		var dueDate sql.NullTime
		var startedAt, completedAt sql.NullTime
		var createdAt time.Time
		rows.Scan(&taskID, &taskType, &title, &desc, &status,
			&volID, &volName, &wardCode, &stateCode, &lgaCode,
			&targetCount, &completedCount, &priority, &dueDate,
			&startedAt, &completedAt, &createdAt)

		t := map[string]interface{}{
			"task_id":         taskID,
			"task_type":       taskType,
			"title":           title,
			"description":     desc,
			"status":          status,
			"volunteer_id":    volID,
			"volunteer_name":  volName,
			"ward_code":       nullStrVal(wardCode),
			"state_code":      nullStrVal(stateCode),
			"lga_code":        nullStrVal(lgaCode),
			"target_count":    targetCount,
			"completed_count": completedCount,
			"priority":        priority,
			"created_at":      createdAt,
		}
		if dueDate.Valid {
			t["due_date"] = dueDate.Time
		}
		if startedAt.Valid {
			t["started_at"] = startedAt.Time
		}
		if completedAt.Valid {
			t["completed_at"] = completedAt.Time
		}
		tasks = append(tasks, t)
	}
	jsonResp(w, map[string]interface{}{"tasks": tasks, "total": len(tasks)})
}

func handleCreateTask(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		TaskType    string `json:"task_type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		VolunteerID string `json:"volunteer_id"`
		WardCode    string `json:"ward_code"`
		StateCode   string `json:"state_code"`
		LGACode     string `json:"lga_code"`
		TargetCount int    `json:"target_count"`
		Priority    int    `json:"priority"`
		DueDate     string `json:"due_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}

	validTypes := map[string]bool{
		"door_knock": true, "phone_call": true, "ride_duty": true,
		"event_setup": true, "data_collection": true, "voter_registration": true,
		"materials_distribution": true, "monitoring": true,
	}
	if !validTypes[req.TaskType] {
		jsonErr(w, "invalid task_type", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		jsonErr(w, "title required", http.StatusBadRequest)
		return
	}
	if req.Priority < 1 || req.Priority > 5 {
		req.Priority = 3
	}
	if req.TargetCount <= 0 {
		req.TargetCount = 1
	}

	taskID := "task-" + uuid.New().String()[:8]
	status := "unassigned"
	if req.VolunteerID != "" {
		status = "assigned"
	}

	var dueDate sql.NullTime
	if req.DueDate != "" {
		if t, err := time.Parse("2006-01-02", req.DueDate); err == nil {
			dueDate = sql.NullTime{Time: t, Valid: true}
		}
	}

	_, err := svc.DB.Exec(`
		INSERT INTO gotv_tasks (task_id, party_id, task_type, title, description,
		       volunteer_id, ward_code, state_code, lga_code,
		       target_count, priority, due_date, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		taskID, pid, req.TaskType, req.Title, req.Description,
		nullStr(req.VolunteerID), nullStr(req.WardCode), nullStr(req.StateCode), nullStr(req.LGACode),
		req.TargetCount, req.Priority, dueDate, status)
	if err != nil {
		jsonErr(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	svc.Audit(pid, user, "create_task", "task", taskID)
	publishEvent("gotv-tasks", taskID, map[string]interface{}{
		"event": "task_created", "task_id": taskID, "task_type": req.TaskType,
		"volunteer_id": req.VolunteerID, "ward": req.WardCode,
	})

	w.WriteHeader(http.StatusCreated)
	jsonResp(w, map[string]interface{}{"task_id": taskID, "status": status})
}

func handleAssignTask(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	taskID := mux.Vars(r)["id"]

	var req struct {
		VolunteerID string `json:"volunteer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VolunteerID == "" {
		jsonErr(w, "volunteer_id required", http.StatusBadRequest)
		return
	}

	// Verify volunteer is approved/active
	var vs string
	err := svc.DB.QueryRow(
		"SELECT vetting_status FROM gotv_volunteers WHERE volunteer_id=$1 AND party_id=$2",
		req.VolunteerID, pid).Scan(&vs)
	if err != nil {
		jsonErr(w, "volunteer not found", http.StatusNotFound)
		return
	}
	if vs != "approved" && vs != "active" {
		jsonErr(w, "volunteer not yet approved (status: "+vs+")", http.StatusConflict)
		return
	}

	res, err := svc.DB.Exec(`
		UPDATE gotv_tasks SET volunteer_id=$1, status='assigned'
		WHERE task_id=$2 AND party_id=$3 AND status IN ('unassigned','assigned')`,
		req.VolunteerID, taskID, pid)
	if err != nil {
		jsonErr(w, "assign failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, "task not found or already started", http.StatusNotFound)
		return
	}

	svc.Audit(pid, user, "assign_task", "task", taskID)
	jsonResp(w, map[string]interface{}{"assigned": true, "volunteer_id": req.VolunteerID})
}

func handleUpdateTaskStatus(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	taskID := mux.Vars(r)["id"]

	var req struct {
		Status         string `json:"status"`
		CompletedCount int    `json:"completed_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}

	validStatuses := map[string]bool{
		"in_progress": true, "completed": true, "cancelled": true, "blocked": true,
	}
	if !validStatuses[req.Status] {
		jsonErr(w, "invalid status", http.StatusBadRequest)
		return
	}

	var timeCol string
	switch req.Status {
	case "in_progress":
		timeCol = ", started_at=COALESCE(started_at, NOW())"
	case "completed":
		timeCol = ", completed_at=NOW()"
	}

	updateQ := fmt.Sprintf(
		"UPDATE gotv_tasks SET status=$1, completed_count=GREATEST(completed_count, $2)%s WHERE task_id=$3 AND party_id=$4",
		timeCol)
	res, err := svc.DB.Exec(updateQ, req.Status, req.CompletedCount, taskID, pid)
	if err != nil {
		jsonErr(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, "task not found", http.StatusNotFound)
		return
	}

	svc.Audit(pid, user, "update_task_status", "task", taskID)
	jsonResp(w, map[string]interface{}{"updated": true, "status": req.Status})
}

func handleAutoAssignTasks(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)

	// Find unassigned tasks and match to available approved volunteers
	// by role compatibility and location proximity
	rows, err := svc.DB.Query(`
		SELECT task_id, task_type, state_code, lga_code, ward_code, priority
		FROM gotv_tasks
		WHERE party_id=$1 AND status='unassigned'
		ORDER BY priority DESC, created_at ASC`, pid)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	roleMap := map[string][]string{
		"door_knock":              {"canvasser", "team_lead"},
		"phone_call":              {"caller", "phone_banker", "canvasser"},
		"ride_duty":               {"driver"},
		"event_setup":             {"coordinator", "team_lead", "canvasser"},
		"data_collection":         {"canvasser", "observer"},
		"voter_registration":      {"canvasser", "team_lead"},
		"materials_distribution":  {"coordinator", "canvasser"},
		"monitoring":              {"observer", "coordinator"},
	}

	assigned := 0
	for rows.Next() {
		var taskID, taskType string
		var stateCode, lgaCode, wardCode sql.NullString
		var priority int
		rows.Scan(&taskID, &taskType, &stateCode, &lgaCode, &wardCode, &priority)

		roles := roleMap[taskType]
		if len(roles) == 0 {
			continue
		}

		// Build role IN clause
		rolePlaceholders := ""
		roleArgs := []interface{}{pid}
		for i, r := range roles {
			if i > 0 {
				rolePlaceholders += ","
			}
			rolePlaceholders += fmt.Sprintf("$%d", i+2)
			roleArgs = append(roleArgs, r)
		}

		// Find an approved volunteer with matching role and fewest assigned tasks
		volQuery := fmt.Sprintf(`
			SELECT v.volunteer_id FROM gotv_volunteers v
			LEFT JOIN gotv_tasks t ON t.volunteer_id = v.volunteer_id AND t.status IN ('assigned','in_progress')
			WHERE v.party_id=$1 AND v.vetting_status IN ('approved','active') AND v.is_active=TRUE
			  AND v.role IN (%s)`, rolePlaceholders)

		idx := len(roleArgs) + 1
		if stateCode.Valid && stateCode.String != "" {
			volQuery += fmt.Sprintf(" AND v.assigned_state=$%d", idx)
			roleArgs = append(roleArgs, stateCode.String)
			idx++
		}

		volQuery += " GROUP BY v.volunteer_id ORDER BY COUNT(t.task_id) ASC LIMIT 1"

		var volID string
		if err := svc.DB.QueryRow(volQuery, roleArgs...).Scan(&volID); err != nil {
			continue
		}

		svc.DB.Exec("UPDATE gotv_tasks SET volunteer_id=$1, status='assigned' WHERE task_id=$2", volID, taskID)
		assigned++
	}

	svc.Audit(pid, user, "auto_assign_tasks", "task", fmt.Sprintf("%d tasks", assigned))
	jsonResp(w, map[string]interface{}{"auto_assigned": assigned})
}

// ─── Location Assignment ───────────────────────────────────────────────────
//
// Assigns volunteers to specific locations (state/LGA/ward/polling unit).
// Supports capacity planning: tracks how many volunteers per location.

func handleAssignLocation(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		State       string `json:"state"`
		LGA         string `json:"lga"`
		Ward        string `json:"ward"`
		PollingUnit string `json:"polling_unit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.State == "" {
		jsonErr(w, "state required", http.StatusBadRequest)
		return
	}

	res, err := svc.DB.Exec(`
		UPDATE gotv_volunteers
		SET assigned_state=$1, assigned_lga=$2, assigned_ward=$3, assigned_polling_unit=$4
		WHERE volunteer_id=$5 AND party_id=$6`,
		req.State, nullStr(req.LGA), nullStr(req.Ward), nullStr(req.PollingUnit), id, pid)
	if err != nil {
		jsonErr(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, "volunteer not found", http.StatusNotFound)
		return
	}

	// Update territory record if ward is specified
	if req.Ward != "" {
		svc.DB.Exec(`
			INSERT INTO gotv_territories (territory_id, party_id, volunteer_id, ward_code, status)
			VALUES ($1, $2, $3, $4, 'assigned')
			ON CONFLICT (party_id, ward_code, volunteer_id) DO UPDATE SET status='assigned'`,
			"terr-"+uuid.New().String()[:8], pid, id, req.Ward)
	}

	svc.Audit(pid, user, "assign_location", "volunteer", id)
	jsonResp(w, map[string]interface{}{
		"assigned":     true,
		"state":        req.State,
		"lga":          req.LGA,
		"ward":         req.Ward,
		"polling_unit": req.PollingUnit,
	})
}

func handleBulkAssignLocations(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)

	var req struct {
		Assignments []struct {
			VolunteerID string `json:"volunteer_id"`
			State       string `json:"state"`
			LGA         string `json:"lga"`
			Ward        string `json:"ward"`
			PollingUnit string `json:"polling_unit"`
		} `json:"assignments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}

	success := 0
	for _, a := range req.Assignments {
		res, err := svc.DB.Exec(`
			UPDATE gotv_volunteers
			SET assigned_state=$1, assigned_lga=$2, assigned_ward=$3, assigned_polling_unit=$4
			WHERE volunteer_id=$5 AND party_id=$6`,
			a.State, nullStr(a.LGA), nullStr(a.Ward), nullStr(a.PollingUnit), a.VolunteerID, pid)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			success++
		}
	}

	svc.Audit(pid, user, "bulk_assign_locations", "volunteer", fmt.Sprintf("%d/%d", success, len(req.Assignments)))
	jsonResp(w, map[string]interface{}{"assigned": success, "total": len(req.Assignments)})
}

func handleLocationCapacity(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	stateFilter := r.URL.Query().Get("state")

	query := `
		SELECT assigned_state, assigned_lga, assigned_ward,
		       COUNT(*) as volunteer_count,
		       SUM(CASE WHEN role='canvasser' THEN 1 ELSE 0 END) as canvassers,
		       SUM(CASE WHEN role='driver' THEN 1 ELSE 0 END) as drivers,
		       SUM(CASE WHEN role='caller' OR role='phone_banker' THEN 1 ELSE 0 END) as callers,
		       SUM(CASE WHEN role='coordinator' OR role='team_lead' THEN 1 ELSE 0 END) as coordinators,
		       SUM(CASE WHEN role='observer' THEN 1 ELSE 0 END) as observers
		FROM gotv_volunteers
		WHERE party_id=$1 AND is_active=TRUE AND assigned_state IS NOT NULL`
	args := []interface{}{pid}

	if stateFilter != "" {
		query += " AND assigned_state=$2"
		args = append(args, stateFilter)
	}
	query += " GROUP BY assigned_state, assigned_lga, assigned_ward ORDER BY assigned_state, assigned_lga, assigned_ward"

	rows, err := svc.DB.Query(query, args...)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var locations []map[string]interface{}
	for rows.Next() {
		var state string
		var lga, ward sql.NullString
		var volCount, canvassers, drivers, callers, coordinators, observers int
		rows.Scan(&state, &lga, &ward, &volCount, &canvassers, &drivers, &callers, &coordinators, &observers)
		locations = append(locations, map[string]interface{}{
			"state":        state,
			"lga":          nullStrVal(lga),
			"ward":         nullStrVal(ward),
			"total":        volCount,
			"canvassers":   canvassers,
			"drivers":      drivers,
			"callers":      callers,
			"coordinators": coordinators,
			"observers":    observers,
		})
	}
	jsonResp(w, map[string]interface{}{"locations": locations, "total": len(locations)})
}

func handleAutoAssignLocations(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)

	// Find approved volunteers with no location assignment and distribute
	// them across wards based on contact density
	rows, err := svc.DB.Query(`
		SELECT volunteer_id, role FROM gotv_volunteers
		WHERE party_id=$1 AND vetting_status IN ('approved','active') AND is_active=TRUE
		  AND (assigned_state IS NULL OR assigned_state='')
		ORDER BY created_at ASC`, pid)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var unassigned []struct{ id, role string }
	for rows.Next() {
		var id, role string
		rows.Scan(&id, &role)
		unassigned = append(unassigned, struct{ id, role string }{id, role})
	}
	rows.Close()

	if len(unassigned) == 0 {
		jsonResp(w, map[string]interface{}{"auto_assigned": 0, "message": "no unassigned approved volunteers"})
		return
	}

	// Get ward contact density (highest contact count wards with fewest volunteers)
	wardRows, err := svc.DB.Query(`
		SELECT c.state_code, c.lga_code, t.ward_code, t.contact_count,
		       COUNT(v.volunteer_id) as current_volunteers
		FROM gotv_territories t
		LEFT JOIN gotv_contacts c ON c.state_code = SUBSTRING(t.ward_code, 1, 2) AND c.party_id=$1
		LEFT JOIN gotv_volunteers v ON v.assigned_ward = t.ward_code AND v.party_id=$1 AND v.is_active=TRUE
		WHERE t.party_id=$1
		GROUP BY c.state_code, c.lga_code, t.ward_code, t.contact_count
		ORDER BY (t.contact_count - COUNT(v.volunteer_id) * 50) DESC
		LIMIT 100`, pid)
	if err != nil {
		// Fallback: assign to states with most contacts
		wardRows, err = svc.DB.Query(`
			SELECT state_code, '' as lga, state_code as ward, COUNT(*) as contacts, 0 as vols
			FROM gotv_contacts WHERE party_id=$1
			GROUP BY state_code ORDER BY COUNT(*) DESC LIMIT 37`, pid)
		if err != nil {
			jsonErr(w, "query failed", http.StatusInternalServerError)
			return
		}
	}
	defer wardRows.Close()

	type ward struct{ state, lga, code string; contacts, vols int }
	var wards []ward
	for wardRows.Next() {
		var w ward
		wardRows.Scan(&w.state, &w.lga, &w.code, &w.contacts, &w.vols)
		if w.state != "" {
			wards = append(wards, w)
		}
	}

	assigned := 0
	for i, vol := range unassigned {
		if len(wards) == 0 {
			break
		}
		w := wards[i%len(wards)]
		svc.DB.Exec(`
			UPDATE gotv_volunteers SET assigned_state=$1, assigned_lga=$2, assigned_ward=$3
			WHERE volunteer_id=$4 AND party_id=$5`,
			w.state, w.lga, w.code, vol.id, pid)
		assigned++
	}

	svc.Audit(pid, user, "auto_assign_locations", "volunteer", fmt.Sprintf("%d assigned", assigned))
	jsonResp(w, map[string]interface{}{"auto_assigned": assigned, "total_unassigned": len(unassigned)})
}

// Helper
func nullStrVal(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
