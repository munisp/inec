package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// DisputeStatus tracks the lifecycle of a result dispute.
type DisputeStatus string

const (
	DisputeStatusFiled       DisputeStatus = "filed"
	DisputeStatusUnderReview DisputeStatus = "under_review"
	DisputeStatusEscalated   DisputeStatus = "escalated"
	DisputeStatusResolved    DisputeStatus = "resolved"
	DisputeStatusDismissed   DisputeStatus = "dismissed"
)

type Dispute struct {
	ID              int           `json:"id"`
	ElectionID      int           `json:"election_id"`
	PollingUnitCode string        `json:"polling_unit_code"`
	FiledBy         string        `json:"filed_by"`
	Party           string        `json:"party"`
	Category        string        `json:"category"`
	Description     string        `json:"description"`
	Evidence        []string      `json:"evidence"`
	Status          DisputeStatus `json:"status"`
	AssignedTo      string        `json:"assigned_to"`
	Resolution      string        `json:"resolution"`
	ResolvedBy      string        `json:"resolved_by"`
	FiledAt         string        `json:"filed_at"`
	ResolvedAt      string        `json:"resolved_at"`
	Priority        string        `json:"priority"`
}

var disputeCategories = []string{
	"overvoting",
	"ballot_stuffing",
	"voter_intimidation",
	"result_falsification",
	"unauthorized_persons",
	"voting_machine_tampering",
	"multiple_voting",
	"missing_results",
	"procedural_violation",
	"other",
}

func initDisputeSchema() {
	dbExecLog("schema", `CREATE TABLE IF NOT EXISTS disputes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		election_id INTEGER NOT NULL,
		polling_unit_code TEXT,
		filed_by TEXT NOT NULL,
		party TEXT,
		category TEXT NOT NULL,
		description TEXT NOT NULL,
		evidence TEXT,
		status TEXT NOT NULL DEFAULT 'filed',
		assigned_to TEXT,
		resolution TEXT,
		resolved_by TEXT,
		filed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		resolved_at TIMESTAMP,
		priority TEXT DEFAULT 'medium',
		FOREIGN KEY (election_id) REFERENCES elections(id)
	)`)
	dbExecLog("schema", `CREATE TABLE IF NOT EXISTS dispute_comments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		dispute_id INTEGER NOT NULL,
		author TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (dispute_id) REFERENCES disputes(id)
	)`)
}

// handleFileDispute creates a new dispute for a polling unit or election result.
func handleFileDispute(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin", "officer", "observer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}

	var req struct {
		ElectionID      int      `json:"election_id" validate:"required"`
		PollingUnitCode string   `json:"polling_unit_code"`
		ResultID        *int     `json:"result_id"`
		Party           string   `json:"party"`
		Category        string   `json:"category" validate:"required"`
		Description     string   `json:"description" validate:"required"`
		Evidence        []string `json:"evidence"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	// Validate category
	validCategory := false
	for _, c := range disputeCategories {
		if c == req.Category {
			validCategory = true
			break
		}
	}
	if !validCategory {
		writeError(w, 400, fmt.Sprintf("invalid category: must be one of %v", disputeCategories))
		return
	}

	// Check election exists
	var elStatus string
	err = db.QueryRow("SELECT status FROM elections WHERE id=?", req.ElectionID).Scan(&elStatus)
	if err != nil {
		writeError(w, 404, "election not found")
		return
	}

	// Priority classification based on category
	priority := "medium"
	highPriority := map[string]bool{
		"result_falsification":     true,
		"ballot_stuffing":          true,
		"voting_machine_tampering": true,
	}
	if highPriority[req.Category] {
		priority = "high"
	}

	username, _ := claims["username"].(string)
	evidenceJSON, _ := json.Marshal(req.Evidence)

	// R5-020: a dispute may be linked to the specific result it challenges.
	if req.ResultID != nil {
		var rElection int
		if err := db.QueryRow("SELECT election_id FROM results WHERE id=?", *req.ResultID).Scan(&rElection); err != nil {
			writeError(w, 400, "referenced result not found")
			return
		}
		if rElection != req.ElectionID {
			writeError(w, 400, "referenced result belongs to a different election")
			return
		}
	}

	result, err := db.Exec(
		`INSERT INTO disputes (election_id, polling_unit_code, filed_by, party, category, description, evidence, priority, result_id)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		req.ElectionID, req.PollingUnitCode, username, req.Party, req.Category,
		req.Description, string(evidenceJSON), priority, req.ResultID,
	)
	if err != nil {
		writeError(w, 500, "failed to file dispute")
		return
	}

	disputeID, _ := result.LastInsertId()

	// Publish event for webhook/Kafka
	dispatchWebhook("dispute.filed", M{
		"dispute_id": disputeID, "election_id": req.ElectionID,
		"category": req.Category, "priority": priority,
	})

	log.Info().Int64("dispute_id", disputeID).Str("category", req.Category).Str("priority", priority).Msg("dispute filed")

	writeJSON(w, 201, M{
		"dispute_id": disputeID,
		"status":     "filed",
		"priority":   priority,
		"message":    "Dispute filed successfully. It will be reviewed by a returning officer.",
	})
}

// handleListDisputes returns disputes with optional filters.
func handleListDisputes(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 0)
	status := queryParam(r, "status", "")
	priority := queryParam(r, "priority", "")

	query := "SELECT id, election_id, COALESCE(polling_unit_code,''), filed_by, COALESCE(party,''), category, description, COALESCE(evidence,'[]'), status, COALESCE(assigned_to,''), COALESCE(resolution,''), COALESCE(resolved_by,''), filed_at, COALESCE(resolved_at,filed_at), priority, COALESCE(result_id,0), COALESCE(outcome,'') FROM disputes WHERE 1=1"
	args := []interface{}{}

	if electionID > 0 {
		query += " AND election_id=?"
		args = append(args, electionID)
	}
	if status != "" {
		query += " AND status=?"
		args = append(args, status)
	}
	if priority != "" {
		query += " AND priority=?"
		args = append(args, priority)
	}
	query += " ORDER BY filed_at DESC LIMIT 100"

	rows, err := db.Query(query, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var disputes []M
	for rows.Next() {
		var id, elID, resultID int
		var puCode, filedBy, party, category, description, evidenceStr, statusStr string
		var assignedTo, resolution, resolvedBy, filedAt, resolvedAt, prio, outcome string
		if rows.Scan(&id, &elID, &puCode, &filedBy, &party, &category, &description,
			&evidenceStr, &statusStr, &assignedTo, &resolution, &resolvedBy, &filedAt, &resolvedAt, &prio, &resultID, &outcome) == nil {

			var evidence []string
			json.Unmarshal([]byte(evidenceStr), &evidence)

			disputes = append(disputes, M{
				"id": id, "election_id": elID, "polling_unit_code": puCode,
				"filed_by": filedBy, "party": party, "category": category,
				"description": description, "evidence": evidence,
				"status": statusStr, "assigned_to": assignedTo,
				"resolution": resolution, "resolved_by": resolvedBy,
				"filed_at": filedAt, "resolved_at": resolvedAt, "priority": prio,
				"result_id": resultID, "outcome": outcome,
			})
		}
	}

	writeJSON(w, 200, M{"disputes": disputes, "total": len(disputes)})
}

// disputeOutcomes is the controlled remediation vocabulary (R5-020).
var disputeOutcomes = map[string]bool{
	"upheld":             true, // dispute valid; result stays disputed pending correction
	"dismissed":          true, // dispute rejected; result restored to its prior status
	"correction_ordered": true, // result must be corrected via the correction flow
	"result_annulled":    true, // result voided; PU may be re-captured or re-run
	"rerun_ordered":      true, // result voided and a rerun/supplementary is required
}

// handleResolveDispute resolves or escalates a dispute.
// R5-033: the whole transition runs in one transaction with FOR UPDATE so
// concurrent resolutions serialize. R5-027: every transition is audit-logged.
// R5-020: resolution outcomes remediate the linked result (restore, void,
// or order correction/rerun) instead of leaving figures untouched.
func handleResolveDispute(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}

	disputeID := mux.Vars(r)["id"]

	var req struct {
		Action     string `json:"action" validate:"required"`
		Resolution string `json:"resolution"`
		Outcome    string `json:"outcome"`
		AssignTo   string `json:"assign_to"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	resolvedBy, _ := claims["username"].(string)
	uid := claimUserID(claims)

	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "database transaction error")
		return
	}
	defer tx.Rollback()

	var currentStatus string
	var electionID int
	var resultID sql.NullInt64
	var puCode sql.NullString
	err = tx.QueryRowContext(r.Context(),
		"SELECT status, election_id, result_id, COALESCE(polling_unit_code,'') FROM disputes WHERE id=? FOR UPDATE",
		disputeID).Scan(&currentStatus, &electionID, &resultID, &puCode)
	if err != nil {
		writeError(w, 404, "dispute not found")
		return
	}

	var newStatus DisputeStatus
	remediation := ""

	switch req.Action {
	case "review":
		if currentStatus != string(DisputeStatusFiled) {
			writeError(w, 422, "can only review disputes in 'filed' status")
			return
		}
		newStatus = DisputeStatusUnderReview
		if _, err := tx.ExecContext(r.Context(), "UPDATE disputes SET status=?, assigned_to=? WHERE id=?",
			string(newStatus), req.AssignTo, disputeID); err != nil {
			writeError(w, 500, "failed to update dispute")
			return
		}

	case "escalate":
		if currentStatus != string(DisputeStatusUnderReview) && currentStatus != string(DisputeStatusFiled) {
			writeError(w, 422, "can only escalate disputes in 'filed' or 'under_review' status")
			return
		}
		newStatus = DisputeStatusEscalated
		if _, err := tx.ExecContext(r.Context(), "UPDATE disputes SET status=?, assigned_to=? WHERE id=?",
			string(newStatus), req.AssignTo, disputeID); err != nil {
			writeError(w, 500, "failed to update dispute")
			return
		}

	case "resolve", "dismiss":
		if currentStatus == string(DisputeStatusResolved) || currentStatus == string(DisputeStatusDismissed) {
			writeError(w, 409, "dispute is already closed")
			return
		}
		if req.Resolution == "" {
			writeError(w, 400, "resolution is required when resolving or dismissing a dispute")
			return
		}
		outcome := req.Outcome
		if outcome == "" {
			if req.Action == "dismiss" {
				outcome = "dismissed"
			} else {
				outcome = "upheld"
			}
		}
		if !disputeOutcomes[outcome] {
			writeError(w, 400, fmt.Sprintf("invalid outcome: must be one of upheld, dismissed, correction_ordered, result_annulled, rerun_ordered"))
			return
		}
		if req.Action == "dismiss" && outcome != "dismissed" {
			writeError(w, 400, "dismiss action requires outcome 'dismissed'")
			return
		}
		if outcome == "dismissed" {
			newStatus = DisputeStatusDismissed
		} else {
			newStatus = DisputeStatusResolved
		}

		// Remediate the linked result (R5-020): re-sync results.status with
		// the dispute outcome — never leave a result stranded in 'disputed'.
		if resultID.Valid {
			var rStatus string
			var validatedAt sql.NullTime
			if err := tx.QueryRowContext(r.Context(),
				"SELECT status, validated_at FROM results WHERE id=? FOR UPDATE", resultID.Int64).
				Scan(&rStatus, &validatedAt); err == nil {
				switch outcome {
				case "dismissed":
					// Restore the result to its pre-dispute status.
					if rStatus == "disputed" {
						restore := "pending"
						if validatedAt.Valid {
							restore = "validated"
						}
						if _, err := tx.ExecContext(r.Context(), "UPDATE results SET status=? WHERE id=?", restore, resultID.Int64); err == nil {
							remediation = "result_restored_to_" + restore
						}
					}
				case "result_annulled", "rerun_ordered":
					// Annul: result becomes voided history; the PU can be
					// re-captured or included in a rerun scope.
					if rStatus != "superseded" && rStatus != "voided" {
						if _, err := tx.ExecContext(r.Context(), "UPDATE results SET status='voided' WHERE id=?", resultID.Int64); err == nil {
							remediation = "result_voided"
							logAudit("RESULT_ANNULLED", "result", fmt.Sprintf("%d", resultID.Int64), uid, map[string]interface{}{
								"dispute_id": disputeID, "outcome": outcome, "prior_status": rStatus,
							})
						}
					}
				case "correction_ordered":
					remediation = "correction_required"
				case "upheld":
					remediation = "result_remains_disputed"
				}
			}
		}

		if _, err := tx.ExecContext(r.Context(),
			"UPDATE disputes SET status=?, resolution=?, outcome=?, resolved_by=?, resolved_at=CURRENT_TIMESTAMP WHERE id=?",
			string(newStatus), req.Resolution, outcome, resolvedBy, disputeID); err != nil {
			writeError(w, 500, "failed to update dispute")
			return
		}

		// When the last dispute closes, the election may proceed to
		// declaration — log it so operators know the path is clear.
		var openDisputes int
		tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM disputes WHERE election_id=? AND status NOT IN ('resolved','dismissed')", electionID).Scan(&openDisputes)
		if openDisputes == 0 {
			log.Info().Int("election_id", electionID).Msg("all disputes resolved, election can be finalized")
		}

	default:
		writeError(w, 400, "action must be: review, escalate, resolve, or dismiss")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, 500, "failed to commit dispute transition")
		return
	}

	// R5-027: dispute lifecycle transitions are part of the tamper-evident trail.
	logAudit("DISPUTE_"+strings.ToUpper(req.Action), "dispute", disputeID, uid, map[string]interface{}{
		"election_id": electionID, "from_status": currentStatus, "to_status": string(newStatus),
		"resolution": req.Resolution, "outcome": req.Outcome, "remediation": remediation,
		"result_id": resultID.Int64, "polling_unit_code": puCode.String,
	})

	dispatchWebhook("dispute.updated", M{
		"dispute_id": disputeID, "action": req.Action, "new_status": string(newStatus),
	})

	writeJSON(w, 200, M{
		"dispute_id":  disputeID,
		"status":      string(newStatus),
		"action":      req.Action,
		"remediation": remediation,
		"message":     fmt.Sprintf("Dispute %s successfully", req.Action+"d"),
	})
}

// handleDisputeComments adds a comment to a dispute.
func handleDisputeComment(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin", "officer", "observer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}

	disputeID := mux.Vars(r)["id"]
	var req struct {
		Content string `json:"content" validate:"required"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	author, _ := claims["username"].(string)
	result, err := db.Exec("INSERT INTO dispute_comments (dispute_id, author, content) VALUES (?,?,?)",
		disputeID, author, req.Content)
	if err != nil {
		writeError(w, 500, "failed to add comment")
		return
	}

	commentID, _ := result.LastInsertId()
	writeJSON(w, 201, M{"comment_id": commentID, "status": "added"})
}

// handleDisputeStats returns dispute statistics for an election.
func handleDisputeStats(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 0)

	query := "SELECT status, priority, COUNT(*) as cnt FROM disputes"
	args := []interface{}{}
	if electionID > 0 {
		query += " WHERE election_id=?"
		args = append(args, electionID)
	}
	query += " GROUP BY status, priority"

	rows, err := db.Query(query, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	byStatus := map[string]int{}
	byPriority := map[string]int{}
	total := 0
	for rows.Next() {
		var status, priority string
		var cnt int
		if rows.Scan(&status, &priority, &cnt) == nil {
			byStatus[status] += cnt
			byPriority[priority] += cnt
			total += cnt
		}
	}

	writeJSON(w, 200, M{
		"total":       total,
		"by_status":   byStatus,
		"by_priority": byPriority,
		"categories":  disputeCategories,
	})
}

// --- Push Notification Service ---

type PushNotification struct {
	DeviceToken string                 `json:"device_token"`
	Title       string                 `json:"title"`
	Body        string                 `json:"body"`
	Data        map[string]interface{} `json:"data"`
	Topic       string                 `json:"topic"`
	Priority    string                 `json:"priority"`
}

func initPushNotificationSchema() {
	dbExecLog("schema", `CREATE TABLE IF NOT EXISTS push_devices (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		device_token TEXT NOT NULL UNIQUE,
		platform TEXT NOT NULL DEFAULT 'android',
		app_version TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_active TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		is_active INTEGER DEFAULT 1,
		FOREIGN KEY (user_id) REFERENCES users(id)
	)`)
	dbExecLog("schema", `CREATE TABLE IF NOT EXISTS push_notifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		device_token TEXT,
		title TEXT NOT NULL,
		body TEXT NOT NULL,
		data TEXT,
		topic TEXT,
		status TEXT DEFAULT 'pending',
		sent_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
}

// handleRegisterDevice registers a device for push notifications.
func handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin", "officer", "observer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}

	var req struct {
		DeviceToken string `json:"device_token" validate:"required"`
		Platform    string `json:"platform"`
		AppVersion  string `json:"app_version"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	platform := strings.ToLower(req.Platform)
	if platform == "" {
		platform = "android"
	}
	if platform != "android" && platform != "ios" && platform != "web" {
		writeError(w, 400, "platform must be: android, ios, or web")
		return
	}

	userID, _ := claims["sub"].(string)

	// Upsert device registration
	_, err = db.Exec(`INSERT INTO push_devices (user_id, device_token, platform, app_version, last_active)
		VALUES (?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(device_token) DO UPDATE SET last_active=CURRENT_TIMESTAMP, is_active=1, app_version=?`,
		userID, req.DeviceToken, platform, req.AppVersion, req.AppVersion)
	if err != nil {
		writeError(w, 500, "failed to register device")
		return
	}

	writeJSON(w, 200, M{"status": "registered", "platform": platform})
}

// Push notification send is handled by handleSendPushNotification in phase7.go.

// handleNotificationHistory returns sent notifications for a user.
func handleNotificationHistory(w http.ResponseWriter, r *http.Request) {
	claims, err := requireRole(r, "admin", "officer", "observer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}

	userID, _ := claims["sub"].(string)
	limit := queryParamInt(r, "limit", 50)

	rows, err := db.Query(`SELECT n.id, n.title, n.body, COALESCE(n.topic,''), n.status, n.created_at
		FROM push_notifications n
		JOIN push_devices d ON n.device_token = d.device_token
		WHERE d.user_id=? ORDER BY n.created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		writeJSON(w, 200, M{"notifications": []M{}, "total": 0})
		return
	}
	defer rows.Close()

	var notifications []M
	for rows.Next() {
		var id int
		var title, body, topic, status, createdAt string
		if rows.Scan(&id, &title, &body, &topic, &status, &createdAt) == nil {
			notifications = append(notifications, M{
				"id": id, "title": title, "body": body,
				"topic": topic, "status": status, "created_at": createdAt,
			})
		}
	}

	writeJSON(w, 200, M{"notifications": notifications, "total": len(notifications)})
}

// rowScanner is an interface for *sql.Rows to allow mock in tests.
type rowScanner interface{}
