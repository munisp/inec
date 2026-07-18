package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// ── User Management CRUD ──

func handleListUsers(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	role := r.URL.Query().Get("role")
	stateCode := r.URL.Query().Get("state_code")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "50"
	}

	query := "SELECT id, username, full_name, role, staff_id, state_code, kyc_status, created_at FROM users WHERE 1=1"
	args := []interface{}{}
	argIdx := 1

	if search != "" {
		query += " AND (LOWER(username) LIKE $" + strconv.Itoa(argIdx) + " OR LOWER(full_name) LIKE $" + strconv.Itoa(argIdx) + ")"
		args = append(args, "%"+strings.ToLower(search)+"%")
		argIdx++
	}
	if role != "" {
		query += " AND role=$" + strconv.Itoa(argIdx)
		args = append(args, role)
		argIdx++
	}
	if stateCode != "" {
		query += " AND state_code=$" + strconv.Itoa(argIdx)
		args = append(args, stateCode)
		argIdx++
	}

	query += " ORDER BY created_at DESC LIMIT $" + strconv.Itoa(argIdx)
	args = append(args, limit)

	rows, err := dbQueryCtx(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, "Failed to query users")
		return
	}
	users := scanRows(rows)

	// Count total
	countQuery := "SELECT COUNT(*) FROM users"
	var total int
	row := db.QueryRowContext(r.Context(), countQuery)
	if err := row.Scan(&total); err != nil {
		total = len(users)
	}

	writeJSON(w, 200, M{"users": users, "total": total})
}

func handleGetUser(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	rows, err := dbQueryCtx(r.Context(), "SELECT id, username, full_name, role, staff_id, state_code, kyc_status, created_at FROM users WHERE id=$1", id)
	if err != nil {
		writeError(w, 500, "Failed to query user")
		return
	}
	users := scanRows(rows)
	if len(users) == 0 {
		writeError(w, 404, "User not found")
		return
	}
	writeJSON(w, 200, users[0])
}

func handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		FullName string `json:"full_name"`
		Password string `json:"password"`
		Role     string `json:"role"`
		StaffID  string `json:"staff_id"`
		State    string `json:"state_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Username == "" || req.Password == "" || req.Role == "" {
		writeError(w, 400, "username, password, and role are required")
		return
	}

	hash := hashPassword(req.Password)
	result, err := db.ExecContext(r.Context(),
		"INSERT INTO users (username, full_name, password_hash, role, staff_id, state_code) VALUES ($1,$2,$3,$4,$5,$6)",
		req.Username, req.FullName, hash, req.Role, req.StaffID, req.State)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			writeError(w, 409, "Username already exists")
			return
		}
		writeError(w, 500, "Failed to create user")
		return
	}
	id, _ := result.LastInsertId()
	auditWrite("USER_CREATED", "user", strconv.FormatInt(id, 10), r, map[string]interface{}{"username": req.Username, "role": req.Role})
	writeJSON(w, 201, M{"message": "User created", "user_id": id})
}

func handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		FullName string `json:"full_name"`
		Role     string `json:"role"`
		StaffID  string `json:"staff_id"`
		State    string `json:"state_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}

	sets := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.FullName != "" {
		sets = append(sets, "full_name=$"+strconv.Itoa(argIdx))
		args = append(args, req.FullName)
		argIdx++
	}
	if req.Role != "" {
		sets = append(sets, "role=$"+strconv.Itoa(argIdx))
		args = append(args, req.Role)
		argIdx++
	}
	if req.StaffID != "" {
		sets = append(sets, "staff_id=$"+strconv.Itoa(argIdx))
		args = append(args, req.StaffID)
		argIdx++
	}
	if req.State != "" {
		sets = append(sets, "state_code=$"+strconv.Itoa(argIdx))
		args = append(args, req.State)
		argIdx++
	}
	if len(sets) == 0 {
		writeError(w, 400, "no fields to update")
		return
	}

	args = append(args, id)
	query := "UPDATE users SET " + strings.Join(sets, ", ") + " WHERE id=$" + strconv.Itoa(argIdx)
	dbExecCtx(r.Context(), query, args...)
	auditWrite("USER_UPDATED", "user", id, r, map[string]interface{}{"fields": sets})
	writeJSON(w, 200, M{"message": "User updated"})
}

func handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	dbExecCtx(r.Context(), "DELETE FROM users WHERE id=$1", id)
	auditWrite("USER_DELETED", "user", id, r, nil)
	writeJSON(w, 200, M{"message": "User deleted"})
}

// ── Election Delete ──

func handleDeleteElection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	// Check if there are results for this election
	var count int
	row := db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=$1", id)
	if err := row.Scan(&count); err == nil && count > 0 {
		writeError(w, 409, "Cannot delete election with existing results. Delete results first.")
		return
	}
	dbExecCtx(r.Context(), "DELETE FROM elections WHERE id=$1", id)
	auditWrite("ELECTION_DELETED", "election", id, r, nil)
	writeJSON(w, 200, M{"message": "Election deleted"})
}

// ── Stakeholder Create ──

func handleCreateStakeholder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgName string `json:"org_name"`
		Type    string `json:"type"`
		Contact string `json:"contact_person"`
		Email   string `json:"email"`
		Phone   string `json:"phone"`
		State   string `json:"state_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.OrgName == "" || req.Type == "" {
		writeError(w, 400, "org_name and type are required")
		return
	}

	result, err := db.ExecContext(r.Context(),
		"INSERT INTO stakeholders (org_name, type, contact_person, email, phone, state_code, status) VALUES ($1,$2,$3,$4,$5,$6,'pending')",
		req.OrgName, req.Type, req.Contact, req.Email, req.Phone, req.State)
	if err != nil {
		writeError(w, 500, "Failed to create stakeholder")
		return
	}
	id, _ := result.LastInsertId()
	auditWrite("STAKEHOLDER_CREATED", "stakeholder", strconv.FormatInt(id, 10), r, map[string]interface{}{"org_name": req.OrgName, "type": req.Type})
	writeJSON(w, 201, M{"message": "Stakeholder registered", "id": id})
}

// ── Webhook Update ──

func handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
		Active *bool    `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}

	sets := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.URL != "" {
		sets = append(sets, "url=$"+strconv.Itoa(argIdx))
		args = append(args, req.URL)
		argIdx++
	}
	if req.Events != nil {
		evJSON, _ := json.Marshal(req.Events)
		sets = append(sets, "events=$"+strconv.Itoa(argIdx))
		args = append(args, string(evJSON))
		argIdx++
	}
	if req.Active != nil {
		// is_active is an INTEGER column (1/0), not boolean.
		active := 0
		if *req.Active {
			active = 1
		}
		sets = append(sets, "is_active=$"+strconv.Itoa(argIdx))
		args = append(args, active)
		argIdx++
	}
	if len(sets) == 0 {
		writeError(w, 400, "no fields to update")
		return
	}
	args = append(args, id)
	query := "UPDATE webhook_subscriptions SET " + strings.Join(sets, ", ") + " WHERE id=$" + strconv.Itoa(argIdx)
	if _, err := dbExecCtx(r.Context(), query, args...); err != nil {
		writeError(w, 500, "failed to update webhook")
		return
	}
	writeJSON(w, 200, M{"message": "Webhook updated"})
}

// ── Training Course Create ──

func handleCreateCourse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title      string `json:"title"`
		Type       string `json:"course_type"`
		TargetRole string `json:"target_role"`
		Duration   int    `json:"duration_hours"`
		Mandatory  bool   `json:"is_mandatory"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Title == "" || req.Type == "" {
		writeError(w, 400, "title and course_type are required")
		return
	}

	result, err := db.ExecContext(r.Context(),
		"INSERT INTO training_courses (title, course_type, target_role, duration_hours, is_mandatory, status) VALUES ($1,$2,$3,$4,$5,'active')",
		req.Title, req.Type, req.TargetRole, req.Duration, req.Mandatory)
	if err != nil {
		writeError(w, 500, "Failed to create course")
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, 201, M{"message": "Course created", "course_id": id})
}

// ── Grievance Create ──

func handleCreateGrievance(w http.ResponseWriter, r *http.Request) {
	user, err := getCurrentUser(r)
	if err != nil {
		writeError(w, 401, "Not authenticated")
		return
	}
	var req struct {
		Category    string `json:"category"`
		Description string `json:"description"`
		ElectionID  int    `json:"election_id"`
		PUCode      string `json:"polling_unit_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Description == "" {
		writeError(w, 400, "description is required")
		return
	}

	result, err2 := db.ExecContext(r.Context(),
		"INSERT INTO grievances (filed_by, category, description, election_id, polling_unit_code, status, priority) VALUES ($1,$2,$3,$4,$5,'filed','medium')",
		user["id"], req.Category, req.Description, req.ElectionID, req.PUCode)
	if err2 != nil {
		writeError(w, 500, "Failed to file grievance")
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, 201, M{"message": "Grievance filed", "id": id})
}

// handleUpdateElection applies partial updates (title, status, description) to an election.
func handleUpdateElection(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	var updates []string
	var vals []interface{}
	if v, ok := req["title"]; ok && v != nil {
		updates = append(updates, "title=?")
		vals = append(vals, v)
	}
	if v, ok := req["status"]; ok && v != nil {
		updates = append(updates, "status=?")
		vals = append(vals, v)
	}
	if v, ok := req["description"]; ok && v != nil {
		updates = append(updates, "description=?")
		vals = append(vals, v)
	}
	if len(updates) == 0 {
		writeError(w, 400, "No fields to update")
		return
	}
	updates = append(updates, "updated_at=CURRENT_TIMESTAMP")
	vals = append(vals, id)
	dbExecCtx(r.Context(), "UPDATE elections SET "+strings.Join(updates, ",")+" WHERE id=?", vals...)
	auditWrite("ELECTION_UPDATED", "election", id, r, req)
	writeJSON(w, 200, M{"message": "Election updated"})
}
