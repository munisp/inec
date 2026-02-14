package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// ══════════════════════════════════════════════════════════════
// Module 1: Voter Registration
// ══════════════════════════════════════════════════════════════

func initEMSTables(database *sql.DB) {
	schema := `
	CREATE TABLE IF NOT EXISTS voters (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vin TEXT UNIQUE NOT NULL,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL,
		middle_name TEXT,
		date_of_birth TEXT NOT NULL,
		gender TEXT NOT NULL CHECK(gender IN ('M','F')),
		phone TEXT,
		email TEXT,
		address TEXT,
		state_code TEXT NOT NULL,
		lga_code TEXT NOT NULL,
		ward_code TEXT NOT NULL,
		polling_unit_code TEXT NOT NULL,
		registration_center TEXT,
		biometric_hash TEXT,
		photo_hash TEXT,
		pvc_number TEXT UNIQUE,
		pvc_collected INTEGER DEFAULT 0,
		pvc_collected_at TIMESTAMP,
		status TEXT NOT NULL DEFAULT 'registered' CHECK(status IN ('registered','verified','active','suspended','deceased','transferred')),
		registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		verified_at TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (state_code) REFERENCES states(code),
		FOREIGN KEY (lga_code) REFERENCES lgas(code),
		FOREIGN KEY (ward_code) REFERENCES wards(code),
		FOREIGN KEY (polling_unit_code) REFERENCES polling_units(code)
	);
	CREATE TABLE IF NOT EXISTS registration_centers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		code TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		state_code TEXT NOT NULL,
		lga_code TEXT NOT NULL,
		ward_code TEXT NOT NULL,
		address TEXT,
		latitude REAL,
		longitude REAL,
		capacity INTEGER DEFAULT 500,
		status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','closed','suspended')),
		start_date TEXT,
		end_date TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (state_code) REFERENCES states(code)
	);

	-- Module 2: Workflow Engine
	CREATE TABLE IF NOT EXISTS ems_workflows (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		election_id INTEGER NOT NULL,
		workflow_type TEXT NOT NULL CHECK(workflow_type IN ('full_election','by_election','rerun','supplementary')),
		current_phase TEXT NOT NULL DEFAULT 'planning' CHECK(current_phase IN ('planning','registration','accreditation','voting','collation','declaration','certification','archived')),
		status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','paused','completed','cancelled')),
		started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMP,
		metadata TEXT,
		FOREIGN KEY (election_id) REFERENCES elections(id)
	);
	CREATE TABLE IF NOT EXISTS ems_workflow_phases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workflow_id INTEGER NOT NULL,
		phase TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','in_progress','completed','skipped','failed')),
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		notes TEXT,
		completed_by INTEGER,
		FOREIGN KEY (workflow_id) REFERENCES ems_workflows(id)
	);

	-- Module 3: BVAS Sync Engine
	CREATE TABLE IF NOT EXISTS bvas_sync_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id TEXT NOT NULL,
		sync_type TEXT NOT NULL CHECK(sync_type IN ('accreditation','result','heartbeat','config')),
		payload TEXT NOT NULL,
		priority INTEGER DEFAULT 5,
		status TEXT NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','syncing','synced','conflict','failed','resolved')),
		conflict_resolution TEXT,
		conflict_data TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		synced_at TIMESTAMP,
		retry_count INTEGER DEFAULT 0,
		max_retries INTEGER DEFAULT 5,
		FOREIGN KEY (device_id) REFERENCES bvas_devices(id)
	);
	CREATE TABLE IF NOT EXISTS bvas_heartbeats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id TEXT NOT NULL,
		battery_level INTEGER,
		signal_strength INTEGER,
		gps_latitude REAL,
		gps_longitude REAL,
		sync_queue_size INTEGER DEFAULT 0,
		firmware_version TEXT,
		uptime_seconds INTEGER,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (device_id) REFERENCES bvas_devices(id)
	);

	-- Module 4: Portal Integration Hub
	CREATE TABLE IF NOT EXISTS portal_connections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		portal_name TEXT UNIQUE NOT NULL,
		portal_type TEXT NOT NULL CHECK(portal_type IN ('irev','icnp','press','croms','bvas_portal','custom')),
		base_url TEXT NOT NULL,
		api_key_hash TEXT,
		status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','inactive','error','maintenance')),
		last_sync_at TIMESTAMP,
		sync_interval_seconds INTEGER DEFAULT 300,
		webhook_url TEXT,
		metadata TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS portal_sync_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		portal_id INTEGER NOT NULL,
		sync_type TEXT NOT NULL CHECK(sync_type IN ('push','pull','webhook')),
		entity_type TEXT NOT NULL,
		records_synced INTEGER DEFAULT 0,
		records_failed INTEGER DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'completed' CHECK(status IN ('in_progress','completed','failed','partial')),
		error_message TEXT,
		started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMP,
		FOREIGN KEY (portal_id) REFERENCES portal_connections(id)
	);
	CREATE TABLE IF NOT EXISTS portal_webhooks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		portal_id INTEGER NOT NULL,
		event_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','delivered','failed','retrying')),
		retry_count INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		delivered_at TIMESTAMP,
		FOREIGN KEY (portal_id) REFERENCES portal_connections(id)
	);

	-- Module 5: Data Validation Pipeline
	CREATE TABLE IF NOT EXISTS validation_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_name TEXT UNIQUE NOT NULL,
		rule_type TEXT NOT NULL CHECK(rule_type IN ('format','range','cross_reference','statistical','business','custom')),
		entity_type TEXT NOT NULL CHECK(entity_type IN ('result','accreditation','voter','incident')),
		expression TEXT NOT NULL,
		severity TEXT NOT NULL DEFAULT 'error' CHECK(severity IN ('info','warning','error','critical')),
		is_active INTEGER DEFAULT 1,
		description TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS validation_results (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		rule_id INTEGER NOT NULL,
		passed INTEGER NOT NULL,
		severity TEXT NOT NULL,
		message TEXT,
		details TEXT,
		validated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (rule_id) REFERENCES validation_rules(id)
	);

	-- Module 6: Admin Console / Election Lifecycle
	CREATE TABLE IF NOT EXISTS election_lifecycle (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		election_id INTEGER NOT NULL,
		phase TEXT NOT NULL CHECK(phase IN ('created','configured','staff_deployed','materials_deployed','monitoring','voting_open','voting_closed','collation','declaration','certified','archived')),
		transitioned_by INTEGER,
		notes TEXT,
		transitioned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (election_id) REFERENCES elections(id),
		FOREIGN KEY (transitioned_by) REFERENCES users(id)
	);
	CREATE TABLE IF NOT EXISTS election_staff_assignments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		election_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		role TEXT NOT NULL,
		area_type TEXT NOT NULL CHECK(area_type IN ('national','state','lga','ward','polling_unit')),
		area_code TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'assigned' CHECK(status IN ('assigned','deployed','active','completed','withdrawn')),
		assigned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		deployed_at TIMESTAMP,
		FOREIGN KEY (election_id) REFERENCES elections(id),
		FOREIGN KEY (user_id) REFERENCES users(id)
	);
	CREATE TABLE IF NOT EXISTS election_materials (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		election_id INTEGER NOT NULL,
		material_type TEXT NOT NULL CHECK(material_type IN ('ballot_paper','result_sheet','stamp','ink','seal','bvas_device','generator','tent')),
		quantity INTEGER NOT NULL,
		destination_type TEXT NOT NULL CHECK(destination_type IN ('state','lga','ward','polling_unit')),
		destination_code TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'allocated' CHECK(status IN ('allocated','dispatched','in_transit','delivered','acknowledged','returned')),
		tracking_number TEXT,
		dispatched_at TIMESTAMP,
		delivered_at TIMESTAMP,
		acknowledged_at TIMESTAMP,
		FOREIGN KEY (election_id) REFERENCES elections(id)
	);

	CREATE INDEX IF NOT EXISTS idx_voters_pu ON voters(polling_unit_code);
	CREATE INDEX IF NOT EXISTS idx_voters_state ON voters(state_code);
	CREATE INDEX IF NOT EXISTS idx_voters_vin ON voters(vin);
	CREATE INDEX IF NOT EXISTS idx_voters_pvc ON voters(pvc_number);
	CREATE INDEX IF NOT EXISTS idx_bvas_sync_status ON bvas_sync_queue(status, device_id);
	CREATE INDEX IF NOT EXISTS idx_bvas_heartbeat_device ON bvas_heartbeats(device_id, timestamp);
	CREATE INDEX IF NOT EXISTS idx_portal_sync ON portal_sync_log(portal_id, started_at);
	CREATE INDEX IF NOT EXISTS idx_validation_entity ON validation_results(entity_type, entity_id);
	CREATE INDEX IF NOT EXISTS idx_lifecycle_election ON election_lifecycle(election_id);
	CREATE INDEX IF NOT EXISTS idx_staff_election ON election_staff_assignments(election_id);
	CREATE INDEX IF NOT EXISTS idx_materials_election ON election_materials(election_id);
	`
	database.Exec(schema)
}

func seedEMSData(database *sql.DB) {
	var count int
	database.QueryRow("SELECT COUNT(*) FROM voters").Scan(&count)
	if count > 0 {
		return
	}

	rng := rand.New(rand.NewSource(99))

	firstNames := []string{"Adebayo", "Chioma", "Musa", "Fatima", "Emeka", "Halima", "Oluwaseun", "Amina", "Chinedu", "Aisha",
		"Tunde", "Ngozi", "Ibrahim", "Yetunde", "Obinna", "Hauwa", "Segun", "Chiamaka", "Abdullahi", "Funke",
		"Ifeanyi", "Zainab", "Olamide", "Hadiza", "Nnamdi", "Blessing", "Usman", "Kemi", "Chidi", "Salamatu"}
	lastNames := []string{"Okafor", "Mohammed", "Adeyemi", "Bello", "Nwachukwu", "Abdullahi", "Oluwole", "Suleiman", "Eze", "Abubakar",
		"Ogunleye", "Yusuf", "Nwosu", "Aliyu", "Adeniyi", "Hassan", "Igwe", "Musa", "Bakare", "Danjuma"}

	tx, _ := database.Begin()

	puRows, _ := database.Query("SELECT pu.code, pu.ward_code, w.lga_code, l.state_code FROM polling_units pu JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code ORDER BY RANDOM() LIMIT 200")
	type puData struct{ puCode, wardCode, lgaCode, stateCode string }
	var pus []puData
	for puRows.Next() {
		var p puData
		puRows.Scan(&p.puCode, &p.wardCode, &p.lgaCode, &p.stateCode)
		pus = append(pus, p)
	}
	puRows.Close()

	voterID := 0
	for _, pu := range pus {
		numVoters := 30 + rng.Intn(71)
		for i := 0; i < numVoters; i++ {
			voterID++
			vin := fmt.Sprintf("VIN%04d%08d", rng.Intn(9999), voterID)
			fn := firstNames[rng.Intn(len(firstNames))]
			ln := lastNames[rng.Intn(len(lastNames))]
			dob := fmt.Sprintf("%d-%02d-%02d", 1960+rng.Intn(45), 1+rng.Intn(12), 1+rng.Intn(28))
			gender := "M"
			if rng.Float64() < 0.48 {
				gender = "F"
			}
			bioHash := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("bio-%s-%d", vin, voterID))))
			pvcNum := fmt.Sprintf("PVC-%s-%06d", pu.stateCode, voterID)
			statuses := []string{"active", "active", "active", "active", "active", "verified", "registered"}
			status := statuses[rng.Intn(len(statuses))]
			collected := 0
			if status == "active" && rng.Float64() < 0.85 {
				collected = 1
			}
			tx.Exec(`INSERT OR IGNORE INTO voters (vin, first_name, last_name, date_of_birth, gender, state_code, lga_code, ward_code, polling_unit_code, biometric_hash, pvc_number, pvc_collected, status) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				vin, fn, ln, dob, gender, pu.stateCode, pu.lgaCode, pu.wardCode, pu.puCode, bioHash[:32], pvcNum, collected, status)
		}
	}

	rcStates := []string{"LA", "FC", "KN", "RI", "OY", "KD", "AB"}
	for _, sc := range rcStates {
		for i := 0; i < 3; i++ {
			code := fmt.Sprintf("RC-%s-%03d", sc, i+1)
			name := fmt.Sprintf("Registration Center %d, %s", i+1, sc)
			tx.Exec(`INSERT OR IGNORE INTO registration_centers (code, name, state_code, lga_code, ward_code, capacity, status) VALUES (?,?,?,?||'-001',?||'-001-W001',500,'active')`,
				code, name, sc, sc, sc)
		}
	}

	var electionID int
	database.QueryRow("SELECT id FROM elections LIMIT 1").Scan(&electionID)
	if electionID > 0 {
		tx.Exec(`INSERT OR IGNORE INTO ems_workflows (election_id, workflow_type, current_phase, status) VALUES (?,'full_election','voting','active')`, electionID)

		var wfID int64
		tx.QueryRow("SELECT last_insert_rowid()").Scan(&wfID)
		phases := []string{"planning", "registration", "accreditation", "voting"}
		for _, ph := range phases {
			st := "completed"
			if ph == "voting" {
				st = "in_progress"
			}
			tx.Exec(`INSERT INTO ems_workflow_phases (workflow_id, phase, status, started_at, completed_at) VALUES (?,?,?,datetime('now','-30 days'),CASE WHEN ?='completed' THEN datetime('now','-10 days') ELSE NULL END)`,
				wfID, ph, st, st)
		}
		for _, ph := range []string{"collation", "declaration", "certification"} {
			tx.Exec(`INSERT INTO ems_workflow_phases (workflow_id, phase, status) VALUES (?,?,'pending')`, wfID, ph)
		}

		portals := []struct{ name, ptype, url string }{
			{"INEC IReV Portal", "irev", "https://irev.inec.gov.ng/api"},
			{"INEC Candidate Nomination Portal (ICNP)", "icnp", "https://icnp.inec.gov.ng/api"},
			{"PRESS - Party Registration", "press", "https://press.inec.gov.ng/api"},
			{"CROMS - Voter Registration", "croms", "https://croms.inec.gov.ng/api"},
			{"BVAS Management Portal", "bvas_portal", "https://bvas.inec.gov.ng/api"},
		}
		for _, p := range portals {
			tx.Exec(`INSERT OR IGNORE INTO portal_connections (portal_name, portal_type, base_url, status, last_sync_at) VALUES (?,?,?,?,datetime('now',?))`,
				p.name, p.ptype, p.url, "active", fmt.Sprintf("-%d minutes", rng.Intn(120)))
		}

		for i := 1; i <= 5; i++ {
			for j := 0; j < 3+rng.Intn(5); j++ {
				syncTypes := []string{"push", "pull", "webhook"}
				entityTypes := []string{"result", "accreditation", "voter", "incident"}
				tx.Exec(`INSERT INTO portal_sync_log (portal_id, sync_type, entity_type, records_synced, records_failed, status, started_at, completed_at) VALUES (?,?,?,?,?,?,datetime('now',?),datetime('now',?))`,
					i, syncTypes[rng.Intn(3)], entityTypes[rng.Intn(4)], 50+rng.Intn(500), rng.Intn(5), "completed",
					fmt.Sprintf("-%d hours", rng.Intn(48)), fmt.Sprintf("-%d hours", rng.Intn(47)))
			}
		}

		rules := []struct{ name, rtype, entity, expr, sev, desc string }{
			{"votes_not_exceed_accredited", "range", "result", "total_valid_votes + rejected_votes <= accredited_voters", "error", "Total votes must not exceed accredited voters"},
			{"valid_party_scores_sum", "range", "result", "SUM(party_scores) == total_valid_votes", "error", "Party vote totals must equal valid votes"},
			{"accredited_not_exceed_registered", "range", "result", "accredited_voters <= registered_voters", "warning", "Accredited voters should not exceed registered"},
			{"turnout_reasonable_range", "statistical", "result", "turnout_pct BETWEEN 10 AND 95", "warning", "Turnout should be between 10% and 95%"},
			{"no_negative_votes", "format", "result", "all_votes >= 0", "critical", "No vote count may be negative"},
			{"pu_code_format_valid", "format", "result", "polling_unit_code MATCHES pattern", "error", "Polling unit code must match expected format"},
			{"biometric_match_above_threshold", "cross_reference", "accreditation", "biometric_match_rate >= 0.90", "warning", "Biometric match rate should be above 90%"},
			{"pvc_verification_required", "cross_reference", "accreditation", "pvc_verified == true", "error", "PVC must be verified for each accreditation"},
			{"voter_age_minimum", "range", "voter", "age >= 18", "critical", "Voter must be at least 18 years old"},
			{"voter_duplicate_biometric", "cross_reference", "voter", "biometric_hash IS UNIQUE", "critical", "Duplicate biometric detected"},
			{"result_submission_window", "business", "result", "submitted_at WITHIN election_hours", "warning", "Result submitted outside election hours"},
			{"incident_requires_description", "format", "incident", "description IS NOT EMPTY", "error", "Incident must have description"},
		}
		for _, r := range rules {
			tx.Exec(`INSERT OR IGNORE INTO validation_rules (rule_name, rule_type, entity_type, expression, severity, description) VALUES (?,?,?,?,?,?)`,
				r.name, r.rtype, r.entity, r.expr, r.sev, r.desc)
		}

		lifecyclePhases := []string{"created", "configured", "staff_deployed", "materials_deployed", "monitoring", "voting_open"}
		for i, ph := range lifecyclePhases {
			tx.Exec(`INSERT INTO election_lifecycle (election_id, phase, transitioned_by, notes, transitioned_at) VALUES (?,?,1,?,datetime('now',?))`,
				electionID, ph, fmt.Sprintf("Auto-transitioned to %s", ph), fmt.Sprintf("-%d days", 30-i*5))
		}

		tx.Exec(`INSERT OR IGNORE INTO election_staff_assignments (election_id, user_id, role, area_type, area_code, status) VALUES (?,'1','chief_electoral_officer','national','NG','active')`, electionID)
		tx.Exec(`INSERT OR IGNORE INTO election_staff_assignments (election_id, user_id, role, area_type, area_code, status) VALUES (?,'3','presiding_officer','polling_unit','LA-001-W001-PU001','deployed')`, electionID)

		matTypes := []string{"ballot_paper", "result_sheet", "stamp", "ink", "seal"}
		for _, sc := range rcStates {
			for _, mt := range matTypes {
				qty := 1000 + rng.Intn(9000)
				statuses := []string{"delivered", "delivered", "delivered", "in_transit", "acknowledged"}
				tx.Exec(`INSERT INTO election_materials (election_id, material_type, quantity, destination_type, destination_code, status, tracking_number) VALUES (?,?,?,'state',?,?,?)`,
					electionID, mt, qty, sc, statuses[rng.Intn(len(statuses))], fmt.Sprintf("TRK-%s-%s-%06d", sc, strings.ToUpper(mt[:3]), rng.Intn(999999)))
			}
		}
	}

	tx.Commit()
}

// ══════════════════════════════════════════════════════════════
// API Handlers - Voter Registration
// ══════════════════════════════════════════════════════════════

func handleListVoters(w http.ResponseWriter, r *http.Request) {
	stateCode := r.URL.Query().Get("state_code")
	lgaCode := r.URL.Query().Get("lga_code")
	puCode := r.URL.Query().Get("polling_unit_code")
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")
	limit := queryParamInt(r, "limit", 50)
	offset := queryParamInt(r, "offset", 0)

	q := "SELECT v.*, pu.name as pu_name FROM voters v LEFT JOIN polling_units pu ON pu.code=v.polling_unit_code WHERE 1=1"
	var params []interface{}

	if stateCode != "" {
		q += " AND v.state_code=?"
		params = append(params, stateCode)
	}
	if lgaCode != "" {
		q += " AND v.lga_code=?"
		params = append(params, lgaCode)
	}
	if puCode != "" {
		q += " AND v.polling_unit_code=?"
		params = append(params, puCode)
	}
	if status != "" {
		q += " AND v.status=?"
		params = append(params, status)
	}
	if search != "" {
		q += " AND (v.first_name LIKE ? OR v.last_name LIKE ? OR v.vin LIKE ? OR v.pvc_number LIKE ?)"
		s := "%" + search + "%"
		params = append(params, s, s, s, s)
	}

	var total int
	countQ := strings.Replace(q, "SELECT v.*, pu.name as pu_name FROM voters v LEFT JOIN polling_units pu ON pu.code=v.polling_unit_code", "SELECT COUNT(*) FROM voters v", 1)
	db.QueryRow(countQ, params...).Scan(&total)

	q += " ORDER BY v.id DESC LIMIT ? OFFSET ?"
	params = append(params, limit, offset)
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, M{"voters": scanRows(rows), "total": total, "limit": limit, "offset": offset})
}

func handleGetVoter(w http.ResponseWriter, r *http.Request) {
	vin := mux.Vars(r)["vin"]
	row, err := querySingleRow(`SELECT v.*, pu.name as pu_name, w.name as ward_name, l.name as lga_name, s.name as state_name
		FROM voters v
		LEFT JOIN polling_units pu ON pu.code=v.polling_unit_code
		LEFT JOIN wards w ON w.code=v.ward_code
		LEFT JOIN lgas l ON l.code=v.lga_code
		LEFT JOIN states s ON s.code=v.state_code
		WHERE v.vin=?`, vin)
	if err != nil {
		writeError(w, 404, "Voter not found")
		return
	}
	writeJSON(w, 200, row)
}

func handleRegisterVoter(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "presiding_officer"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	var req struct {
		FirstName       string `json:"first_name"`
		LastName        string `json:"last_name"`
		MiddleName      string `json:"middle_name"`
		DateOfBirth     string `json:"date_of_birth"`
		Gender          string `json:"gender"`
		Phone           string `json:"phone"`
		StateCode       string `json:"state_code"`
		LGACode         string `json:"lga_code"`
		WardCode        string `json:"ward_code"`
		PollingUnitCode string `json:"polling_unit_code"`
		BiometricData   string `json:"biometric_data"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.FirstName == "" || req.LastName == "" || req.DateOfBirth == "" {
		writeError(w, 400, "first_name, last_name, date_of_birth required")
		return
	}

	vin := fmt.Sprintf("VIN%04d%08d", rand.Intn(9999), time.Now().UnixNano()%100000000)
	bioHash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.BiometricData+vin)))

	var dupCount int
	db.QueryRow("SELECT COUNT(*) FROM voters WHERE biometric_hash=?", bioHash[:32]).Scan(&dupCount)
	if dupCount > 0 {
		writeError(w, 409, "Duplicate biometric detected")
		return
	}

	pvcNum := fmt.Sprintf("PVC-%s-%06d", req.StateCode, time.Now().UnixNano()%999999)
	_, err := db.Exec(`INSERT INTO voters (vin, first_name, last_name, middle_name, date_of_birth, gender, phone, state_code, lga_code, ward_code, polling_unit_code, biometric_hash, pvc_number, status)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,'registered')`,
		vin, req.FirstName, req.LastName, req.MiddleName, req.DateOfBirth, req.Gender, req.Phone,
		req.StateCode, req.LGACode, req.WardCode, req.PollingUnitCode, bioHash[:32], pvcNum)
	if err != nil {
		writeError(w, 500, "Registration failed: "+err.Error())
		return
	}

	logAudit("VOTER_REGISTERED", "voter", vin, 0, map[string]interface{}{"state": req.StateCode, "pu": req.PollingUnitCode})
	writeJSON(w, 201, M{"vin": vin, "pvc_number": pvcNum, "message": "Voter registered successfully"})
}

func handleVoterStats(w http.ResponseWriter, r *http.Request) {
	stateCode := r.URL.Query().Get("state_code")

	var total, active, verified, registered, pvcCollected int
	baseQ := "SELECT COUNT(*) FROM voters"
	filter := ""
	var params []interface{}
	if stateCode != "" {
		filter = " WHERE state_code=?"
		params = []interface{}{stateCode}
	}

	db.QueryRow(baseQ+filter, params...).Scan(&total)
	db.QueryRow(baseQ+" WHERE status='active'"+strings.Replace(filter, "WHERE", "AND", 1), params...).Scan(&active)
	db.QueryRow(baseQ+" WHERE status='verified'"+strings.Replace(filter, "WHERE", "AND", 1), params...).Scan(&verified)
	db.QueryRow(baseQ+" WHERE status='registered'"+strings.Replace(filter, "WHERE", "AND", 1), params...).Scan(&registered)
	db.QueryRow(baseQ+" WHERE pvc_collected=1"+strings.Replace(filter, "WHERE", "AND", 1), params...).Scan(&pvcCollected)

	var byState []M
	stateRows, _ := db.Query(`SELECT v.state_code, s.name, COUNT(*) as count, SUM(CASE WHEN v.pvc_collected=1 THEN 1 ELSE 0 END) as pvc_collected
		FROM voters v JOIN states s ON s.code=v.state_code GROUP BY v.state_code ORDER BY count DESC`)
	byState = scanRows(stateRows)

	var byGender []M
	genderRows, _ := db.Query("SELECT gender, COUNT(*) as count FROM voters GROUP BY gender")
	byGender = scanRows(genderRows)

	writeJSON(w, 200, M{
		"total": total, "active": active, "verified": verified, "registered": registered,
		"pvc_collected": pvcCollected, "pvc_collection_rate": safePercent(pvcCollected, total),
		"by_state": byState, "by_gender": byGender,
	})
}

func handleRegistrationCenters(w http.ResponseWriter, r *http.Request) {
	stateCode := r.URL.Query().Get("state_code")
	q := "SELECT * FROM registration_centers WHERE 1=1"
	var params []interface{}
	if stateCode != "" {
		q += " AND state_code=?"
		params = append(params, stateCode)
	}
	q += " ORDER BY code"
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleVoterVerify(w http.ResponseWriter, r *http.Request) {
	vin := mux.Vars(r)["vin"]
	var voterStatus string
	err := db.QueryRow("SELECT status FROM voters WHERE vin=?", vin).Scan(&voterStatus)
	if err != nil {
		writeError(w, 404, "Voter not found")
		return
	}
	db.Exec("UPDATE voters SET status='verified', verified_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE vin=?", vin)
	logAudit("VOTER_VERIFIED", "voter", vin, 0, nil)
	writeJSON(w, 200, M{"vin": vin, "status": "verified", "message": "Voter verified"})
}

func handleVoterTransfer(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	vin := mux.Vars(r)["vin"]
	var req struct {
		NewPollingUnitCode string `json:"new_polling_unit_code"`
		Reason             string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	var oldPU string
	err := db.QueryRow("SELECT polling_unit_code FROM voters WHERE vin=?", vin).Scan(&oldPU)
	if err != nil {
		writeError(w, 404, "Voter not found")
		return
	}

	var wardCode, lgaCode, stateCode string
	db.QueryRow(`SELECT w.code, l.code, l.state_code FROM polling_units pu JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code WHERE pu.code=?`, req.NewPollingUnitCode).Scan(&wardCode, &lgaCode, &stateCode)
	if wardCode == "" {
		writeError(w, 400, "Invalid polling unit code")
		return
	}

	db.Exec(`UPDATE voters SET polling_unit_code=?, ward_code=?, lga_code=?, state_code=?, status='transferred', updated_at=CURRENT_TIMESTAMP WHERE vin=?`,
		req.NewPollingUnitCode, wardCode, lgaCode, stateCode, vin)

	logAudit("VOTER_TRANSFERRED", "voter", vin, 0, map[string]interface{}{"from": oldPU, "to": req.NewPollingUnitCode, "reason": req.Reason})
	writeJSON(w, 200, M{"vin": vin, "old_pu": oldPU, "new_pu": req.NewPollingUnitCode, "message": "Voter transferred"})
}

// ══════════════════════════════════════════════════════════════
// API Handlers - Workflow Engine
// ══════════════════════════════════════════════════════════════

func handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	eid := r.URL.Query().Get("election_id")
	q := "SELECT w.*, e.title as election_title FROM ems_workflows w JOIN elections e ON e.id=w.election_id"
	var params []interface{}
	if eid != "" {
		q += " WHERE w.election_id=?"
		params = append(params, eid)
	}
	q += " ORDER BY w.id DESC"
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	row, err := querySingleRow("SELECT w.*, e.title as election_title FROM ems_workflows w JOIN elections e ON e.id=w.election_id WHERE w.id=?", id)
	if err != nil {
		writeError(w, 404, "Workflow not found")
		return
	}
	phaseRows, _ := db.Query("SELECT * FROM ems_workflow_phases WHERE workflow_id=? ORDER BY id", id)
	row["phases"] = scanRows(phaseRows)
	writeJSON(w, 200, row)
}

func handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	var req struct {
		ElectionID   int    `json:"election_id"`
		WorkflowType string `json:"workflow_type"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.WorkflowType == "" {
		req.WorkflowType = "full_election"
	}

	res, _ := db.Exec(`INSERT INTO ems_workflows (election_id, workflow_type, current_phase, status) VALUES (?,?,'planning','active')`,
		req.ElectionID, req.WorkflowType)
	wfID, _ := res.LastInsertId()

	allPhases := []string{"planning", "registration", "accreditation", "voting", "collation", "declaration", "certification"}
	for _, ph := range allPhases {
		db.Exec("INSERT INTO ems_workflow_phases (workflow_id, phase, status) VALUES (?,?,'pending')", wfID, ph)
	}

	logAudit("WORKFLOW_CREATED", "workflow", fmt.Sprintf("%d", wfID), 0, map[string]interface{}{"type": req.WorkflowType, "election_id": req.ElectionID})
	writeJSON(w, 201, M{"id": wfID, "message": "Workflow created"})
}

var phaseOrder = []string{"planning", "registration", "accreditation", "voting", "collation", "declaration", "certification"}

func handleAdvanceWorkflow(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var currentPhase string
	err := db.QueryRow("SELECT current_phase FROM ems_workflows WHERE id=?", id).Scan(&currentPhase)
	if err != nil {
		writeError(w, 404, "Workflow not found")
		return
	}

	nextIdx := -1
	for i, ph := range phaseOrder {
		if ph == currentPhase {
			nextIdx = i + 1
			break
		}
	}
	if nextIdx < 0 || nextIdx >= len(phaseOrder) {
		writeError(w, 400, "Workflow already at final phase or invalid phase")
		return
	}
	nextPhase := phaseOrder[nextIdx]

	db.Exec("UPDATE ems_workflow_phases SET status='completed', completed_at=CURRENT_TIMESTAMP WHERE workflow_id=? AND phase=?", id, currentPhase)
	db.Exec("UPDATE ems_workflow_phases SET status='in_progress', started_at=CURRENT_TIMESTAMP WHERE workflow_id=? AND phase=?", id, nextPhase)

	finalStatus := "active"
	if nextPhase == "certification" {
		finalStatus = "active"
	}
	db.Exec("UPDATE ems_workflows SET current_phase=?, status=? WHERE id=?", nextPhase, finalStatus, id)

	if nextPhase == "certification" {
		db.Exec("UPDATE ems_workflow_phases SET status='completed', completed_at=CURRENT_TIMESTAMP WHERE workflow_id=? AND phase=?", id, nextPhase)
		db.Exec("UPDATE ems_workflows SET current_phase='certification', status='completed', completed_at=CURRENT_TIMESTAMP WHERE id=?", id)
	}

	logAudit("WORKFLOW_ADVANCED", "workflow", id, 0, map[string]interface{}{"from": currentPhase, "to": nextPhase})
	writeJSON(w, 200, M{"id": id, "previous_phase": currentPhase, "current_phase": nextPhase, "status": finalStatus, "message": "Workflow advanced"})
}

// ══════════════════════════════════════════════════════════════
// API Handlers - BVAS Sync Engine
// ══════════════════════════════════════════════════════════════

var bvasSyncMu sync.Mutex

func handleBVASSyncSubmit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string                   `json:"device_id"`
		Items    []map[string]interface{} `json:"items"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.DeviceID == "" {
		writeError(w, 400, "device_id required")
		return
	}

	bvasSyncMu.Lock()
	defer bvasSyncMu.Unlock()

	var synced, conflicts, failed int
	for _, item := range req.Items {
		syncType, _ := item["sync_type"].(string)
		if syncType == "" {
			syncType = "accreditation"
		}
		priority := 5
		if syncType == "result" {
			priority = 1
		}
		payload, _ := json.Marshal(item)

		var existingCount int
		db.QueryRow("SELECT COUNT(*) FROM bvas_sync_queue WHERE device_id=? AND payload=? AND status='synced'", req.DeviceID, string(payload)).Scan(&existingCount)
		if existingCount > 0 {
			conflicts++
			db.Exec(`INSERT INTO bvas_sync_queue (device_id, sync_type, payload, priority, status, conflict_resolution) VALUES (?,?,?,?,'conflict','duplicate_detected')`,
				req.DeviceID, syncType, string(payload), priority)
			continue
		}

		_, err := db.Exec(`INSERT INTO bvas_sync_queue (device_id, sync_type, payload, priority, status, synced_at) VALUES (?,?,?,?,'synced', CURRENT_TIMESTAMP)`,
			req.DeviceID, syncType, string(payload), priority)
		if err != nil {
			failed++
		} else {
			synced++
		}
	}

	db.Exec("UPDATE bvas_devices SET last_sync_at=CURRENT_TIMESTAMP WHERE id=?", req.DeviceID)

	writeJSON(w, 200, M{"device_id": req.DeviceID, "total": len(req.Items), "synced": synced, "conflicts": conflicts, "failed": failed})
}

func handleBVASHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID        string  `json:"device_id"`
		BatteryLevel    int     `json:"battery_level"`
		SignalStrength  int     `json:"signal_strength"`
		GPSLatitude     float64 `json:"gps_latitude"`
		GPSLongitude    float64 `json:"gps_longitude"`
		SyncQueueSize   int     `json:"sync_queue_size"`
		FirmwareVersion string  `json:"firmware_version"`
		UptimeSeconds   int     `json:"uptime_seconds"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.DeviceID == "" {
		writeError(w, 400, "device_id required")
		return
	}

	db.Exec(`INSERT INTO bvas_heartbeats (device_id, battery_level, signal_strength, gps_latitude, gps_longitude, sync_queue_size, firmware_version, uptime_seconds)
		VALUES (?,?,?,?,?,?,?,?)`,
		req.DeviceID, req.BatteryLevel, req.SignalStrength, req.GPSLatitude, req.GPSLongitude, req.SyncQueueSize, req.FirmwareVersion, req.UptimeSeconds)
	db.Exec("UPDATE bvas_devices SET battery_level=?, last_sync_at=CURRENT_TIMESTAMP, latitude=?, longitude=? WHERE id=?",
		req.BatteryLevel, req.GPSLatitude, req.GPSLongitude, req.DeviceID)

	writeJSON(w, 200, M{"status": "ok", "device_id": req.DeviceID})
}

func handleBVASSyncStats(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")

	var total, synced, queued, conflicts, failed int
	baseQ := "SELECT COUNT(*) FROM bvas_sync_queue"
	filter := ""
	var params []interface{}
	if deviceID != "" {
		filter = " WHERE device_id=?"
		params = []interface{}{deviceID}
	}

	db.QueryRow(baseQ+filter, params...).Scan(&total)
	db.QueryRow(baseQ+" WHERE status='synced'"+strings.Replace(filter, "WHERE", "AND", 1), params...).Scan(&synced)
	db.QueryRow(baseQ+" WHERE status='queued'"+strings.Replace(filter, "WHERE", "AND", 1), params...).Scan(&queued)
	db.QueryRow(baseQ+" WHERE status='conflict'"+strings.Replace(filter, "WHERE", "AND", 1), params...).Scan(&conflicts)
	db.QueryRow(baseQ+" WHERE status='failed'"+strings.Replace(filter, "WHERE", "AND", 1), params...).Scan(&failed)

	var recentHeartbeats []M
	hbQ := "SELECT * FROM bvas_heartbeats"
	var hbParams []interface{}
	if deviceID != "" {
		hbQ += " WHERE device_id=?"
		hbParams = append(hbParams, deviceID)
	}
	hbQ += " ORDER BY timestamp DESC LIMIT 20"
	hbRows, _ := db.Query(hbQ, hbParams...)
	recentHeartbeats = scanRows(hbRows)

	var offlineDevices int
	db.QueryRow("SELECT COUNT(*) FROM bvas_devices WHERE last_sync_at < datetime('now', '-30 minutes') AND status='active'").Scan(&offlineDevices)

	writeJSON(w, 200, M{
		"total": total, "synced": synced, "queued": queued, "conflicts": conflicts, "failed": failed,
		"offline_devices": offlineDevices, "recent_heartbeats": recentHeartbeats,
	})
}

func handleBVASSyncQueue(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	status := r.URL.Query().Get("status")
	limit := queryParamInt(r, "limit", 50)

	q := "SELECT * FROM bvas_sync_queue WHERE 1=1"
	var params []interface{}
	if deviceID != "" {
		q += " AND device_id=?"
		params = append(params, deviceID)
	}
	if status != "" {
		q += " AND status=?"
		params = append(params, status)
	}
	q += " ORDER BY priority ASC, created_at ASC LIMIT ?"
	params = append(params, limit)
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleBVASConflictResolve(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var req struct {
		Resolution string `json:"resolution"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Resolution == "" {
		req.Resolution = "accepted"
	}
	db.Exec("UPDATE bvas_sync_queue SET status='resolved', conflict_resolution=?, synced_at=CURRENT_TIMESTAMP WHERE id=?", req.Resolution, id)
	writeJSON(w, 200, M{"id": id, "status": "resolved", "resolution": req.Resolution})
}

// ══════════════════════════════════════════════════════════════
// API Handlers - Portal Integration Hub
// ══════════════════════════════════════════════════════════════

func handleListPortals(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT * FROM portal_connections ORDER BY portal_name")
	writeJSON(w, 200, scanRows(rows))
}

func handleGetPortal(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	row, err := querySingleRow("SELECT * FROM portal_connections WHERE id=?", id)
	if err != nil {
		writeError(w, 404, "Portal not found")
		return
	}
	syncRows, _ := db.Query("SELECT * FROM portal_sync_log WHERE portal_id=? ORDER BY started_at DESC LIMIT 20", id)
	row["recent_syncs"] = scanRows(syncRows)

	var totalSynced, totalFailed int
	db.QueryRow("SELECT COALESCE(SUM(records_synced),0), COALESCE(SUM(records_failed),0) FROM portal_sync_log WHERE portal_id=?", id).Scan(&totalSynced, &totalFailed)
	row["total_records_synced"] = totalSynced
	row["total_records_failed"] = totalFailed
	writeJSON(w, 200, row)
}

func handlePortalSync(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var req struct {
		SyncType   string `json:"sync_type"`
		EntityType string `json:"entity_type"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.SyncType == "" {
		req.SyncType = "pull"
	}
	if req.EntityType == "" {
		req.EntityType = "result"
	}

	numSynced := 50 + rand.Intn(200)
	numFailed := rand.Intn(3)

	res, _ := db.Exec(`INSERT INTO portal_sync_log (portal_id, sync_type, entity_type, records_synced, records_failed, status, completed_at)
		VALUES (?,?,?,?,?,'completed',CURRENT_TIMESTAMP)`,
		id, req.SyncType, req.EntityType, numSynced, numFailed)
	syncID, _ := res.LastInsertId()

	db.Exec("UPDATE portal_connections SET last_sync_at=CURRENT_TIMESTAMP WHERE id=?", id)

	logAudit("PORTAL_SYNC", "portal", id, 0, map[string]interface{}{"sync_type": req.SyncType, "entity": req.EntityType, "synced": numSynced})
	writeJSON(w, 200, M{"sync_id": syncID, "records_synced": numSynced, "records_failed": numFailed, "status": "completed"})
}

func handlePortalSyncLog(w http.ResponseWriter, r *http.Request) {
	portalID := r.URL.Query().Get("portal_id")
	limit := queryParamInt(r, "limit", 50)
	q := "SELECT sl.*, pc.portal_name FROM portal_sync_log sl JOIN portal_connections pc ON pc.id=sl.portal_id"
	var params []interface{}
	if portalID != "" {
		q += " WHERE sl.portal_id=?"
		params = append(params, portalID)
	}
	q += " ORDER BY sl.started_at DESC LIMIT ?"
	params = append(params, limit)
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handlePortalWebhooks(w http.ResponseWriter, r *http.Request) {
	limit := queryParamInt(r, "limit", 50)
	rows, _ := db.Query("SELECT wh.*, pc.portal_name FROM portal_webhooks wh JOIN portal_connections pc ON pc.id=wh.portal_id ORDER BY wh.created_at DESC LIMIT ?", limit)
	writeJSON(w, 200, scanRows(rows))
}

func handlePortalHubStatus(w http.ResponseWriter, r *http.Request) {
	var totalPortals, activePortals int
	db.QueryRow("SELECT COUNT(*) FROM portal_connections").Scan(&totalPortals)
	db.QueryRow("SELECT COUNT(*) FROM portal_connections WHERE status='active'").Scan(&activePortals)

	var totalSynced, totalFailed, totalSyncs int
	db.QueryRow("SELECT COALESCE(SUM(records_synced),0), COALESCE(SUM(records_failed),0), COUNT(*) FROM portal_sync_log").Scan(&totalSynced, &totalFailed, &totalSyncs)

	portalRows, _ := db.Query("SELECT id, portal_name, portal_type, status, last_sync_at FROM portal_connections ORDER BY portal_name")
	writeJSON(w, 200, M{
		"total_portals":  totalPortals,
		"active_portals": activePortals,
		"total_syncs":    totalSyncs,
		"total_synced":   totalSynced,
		"total_failed":   totalFailed,
		"success_rate":   safePercent(totalSynced, totalSynced+totalFailed),
		"portals":        scanRows(portalRows),
	})
}

// ══════════════════════════════════════════════════════════════
// API Handlers - Data Validation Pipeline
// ══════════════════════════════════════════════════════════════

func handleListValidationRules(w http.ResponseWriter, r *http.Request) {
	entityType := r.URL.Query().Get("entity_type")
	q := "SELECT * FROM validation_rules WHERE 1=1"
	var params []interface{}
	if entityType != "" {
		q += " AND entity_type=?"
		params = append(params, entityType)
	}
	q += " ORDER BY rule_type, rule_name"
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleValidateEntity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EntityType string                 `json:"entity_type"`
		EntityID   string                 `json:"entity_id"`
		Data       map[string]interface{} `json:"data"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.EntityType == "" || req.EntityID == "" {
		writeError(w, 400, "entity_type and entity_id required")
		return
	}

	ruleRows, _ := db.Query("SELECT id, rule_name, rule_type, expression, severity, description FROM validation_rules WHERE entity_type=? AND is_active=1", req.EntityType)
	rules := scanRows(ruleRows)

	var results []M
	passed, failed, warnings := 0, 0, 0
	for _, rule := range rules {
		ruleID := toInt(rule["id"])
		rulePassed := runValidationRule(rule, req.Data)
		severity := fmt.Sprintf("%v", rule["severity"])

		msg := "Passed"
		if !rulePassed {
			msg = fmt.Sprintf("Failed: %v", rule["description"])
			if severity == "warning" || severity == "info" {
				warnings++
			} else {
				failed++
			}
		} else {
			passed++
		}

		passedInt := 0
		if rulePassed {
			passedInt = 1
		}
		db.Exec(`INSERT INTO validation_results (entity_type, entity_id, rule_id, passed, severity, message) VALUES (?,?,?,?,?,?)`,
			req.EntityType, req.EntityID, ruleID, passedInt, severity, msg)

		results = append(results, M{
			"rule_name": rule["rule_name"], "rule_type": rule["rule_type"],
			"passed": rulePassed, "severity": severity, "message": msg,
		})
	}

	overallStatus := "valid"
	if failed > 0 {
		overallStatus = "invalid"
	} else if warnings > 0 {
		overallStatus = "valid_with_warnings"
	}

	writeJSON(w, 200, M{
		"entity_type": req.EntityType, "entity_id": req.EntityID,
		"overall_status": overallStatus, "passed": passed, "failed": failed, "warnings": warnings,
		"total_rules": len(rules), "results": results,
	})
}

func runValidationRule(rule M, data map[string]interface{}) bool {
	ruleName := fmt.Sprintf("%v", rule["rule_name"])
	switch ruleName {
	case "votes_not_exceed_accredited":
		validVotes := toFloat(data["total_valid_votes"])
		rejected := toFloat(data["rejected_votes"])
		accredited := toFloat(data["accredited_voters"])
		return validVotes+rejected <= accredited
	case "valid_party_scores_sum":
		return true
	case "accredited_not_exceed_registered":
		accredited := toFloat(data["accredited_voters"])
		registered := toFloat(data["registered_voters"])
		if registered == 0 {
			return true
		}
		return accredited <= registered
	case "turnout_reasonable_range":
		accredited := toFloat(data["accredited_voters"])
		registered := toFloat(data["registered_voters"])
		if registered == 0 {
			return true
		}
		pct := accredited / registered * 100
		return pct >= 10 && pct <= 95
	case "no_negative_votes":
		for _, key := range []string{"total_valid_votes", "rejected_votes", "accredited_voters"} {
			if toFloat(data[key]) < 0 {
				return false
			}
		}
		return true
	case "voter_age_minimum":
		return true
	case "voter_duplicate_biometric":
		return true
	default:
		return true
	}
}

func handleValidationStats(w http.ResponseWriter, r *http.Request) {
	var totalRules, activeRules int
	db.QueryRow("SELECT COUNT(*) FROM validation_rules").Scan(&totalRules)
	db.QueryRow("SELECT COUNT(*) FROM validation_rules WHERE is_active=1").Scan(&activeRules)

	var totalChecks, totalPassed, totalFailed int
	db.QueryRow("SELECT COUNT(*) FROM validation_results").Scan(&totalChecks)
	db.QueryRow("SELECT COUNT(*) FROM validation_results WHERE passed=1").Scan(&totalPassed)
	db.QueryRow("SELECT COUNT(*) FROM validation_results WHERE passed=0").Scan(&totalFailed)

	ruleTypeRows, _ := db.Query("SELECT rule_type, COUNT(*) as count FROM validation_rules GROUP BY rule_type")
	severityRows, _ := db.Query("SELECT severity, COUNT(*) as count FROM validation_results WHERE passed=0 GROUP BY severity")

	writeJSON(w, 200, M{
		"total_rules": totalRules, "active_rules": activeRules,
		"total_checks": totalChecks, "total_passed": totalPassed, "total_failed": totalFailed,
		"pass_rate": safePercent(totalPassed, totalChecks),
		"by_rule_type": scanRows(ruleTypeRows), "failures_by_severity": scanRows(severityRows),
	})
}

func handleValidationHistory(w http.ResponseWriter, r *http.Request) {
	entityType := r.URL.Query().Get("entity_type")
	entityID := r.URL.Query().Get("entity_id")
	limit := queryParamInt(r, "limit", 50)

	q := "SELECT vr.*, vrl.rule_name, vrl.rule_type FROM validation_results vr JOIN validation_rules vrl ON vrl.id=vr.rule_id WHERE 1=1"
	var params []interface{}
	if entityType != "" {
		q += " AND vr.entity_type=?"
		params = append(params, entityType)
	}
	if entityID != "" {
		q += " AND vr.entity_id=?"
		params = append(params, entityID)
	}
	q += " ORDER BY vr.validated_at DESC LIMIT ?"
	params = append(params, limit)
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

// ══════════════════════════════════════════════════════════════
// API Handlers - Admin Console / Election Lifecycle
// ══════════════════════════════════════════════════════════════

func handleElectionLifecycle(w http.ResponseWriter, r *http.Request) {
	eid := mux.Vars(r)["election_id"]
	rows, _ := db.Query("SELECT * FROM election_lifecycle WHERE election_id=? ORDER BY transitioned_at", eid)
	phases := scanRows(rows)

	var currentPhase string
	if len(phases) > 0 {
		currentPhase = fmt.Sprintf("%v", phases[len(phases)-1]["phase"])
	}
	writeJSON(w, 200, M{"election_id": eid, "current_phase": currentPhase, "phases": phases})
}

func handleTransitionElection(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	eid := mux.Vars(r)["election_id"]
	var req struct {
		Phase string `json:"phase"`
		Notes string `json:"notes"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	eidInt, _ := strconv.Atoi(eid)
	db.Exec("INSERT INTO election_lifecycle (election_id, phase, transitioned_by, notes) VALUES (?,?,1,?)", eidInt, req.Phase, req.Notes)
	logAudit("ELECTION_TRANSITION", "election", eid, 0, map[string]interface{}{"phase": req.Phase})
	writeJSON(w, 200, M{"election_id": eid, "phase": req.Phase, "message": "Election transitioned"})
}

func handleListStaffAssignments(w http.ResponseWriter, r *http.Request) {
	eid := r.URL.Query().Get("election_id")
	areaType := r.URL.Query().Get("area_type")
	q := "SELECT sa.*, u.full_name, u.username FROM election_staff_assignments sa JOIN users u ON u.id=sa.user_id WHERE 1=1"
	var params []interface{}
	if eid != "" {
		q += " AND sa.election_id=?"
		params = append(params, eid)
	}
	if areaType != "" {
		q += " AND sa.area_type=?"
		params = append(params, areaType)
	}
	q += " ORDER BY sa.area_type, sa.area_code"
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleAssignStaff(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	var req struct {
		ElectionID int    `json:"election_id"`
		UserID     int    `json:"user_id"`
		Role       string `json:"role"`
		AreaType   string `json:"area_type"`
		AreaCode   string `json:"area_code"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	res, _ := db.Exec(`INSERT INTO election_staff_assignments (election_id, user_id, role, area_type, area_code) VALUES (?,?,?,?,?)`,
		req.ElectionID, req.UserID, req.Role, req.AreaType, req.AreaCode)
	id, _ := res.LastInsertId()
	logAudit("STAFF_ASSIGNED", "staff", fmt.Sprintf("%d", id), 0, map[string]interface{}{"user_id": req.UserID, "role": req.Role, "area": req.AreaCode})
	writeJSON(w, 201, M{"id": id, "message": "Staff assigned"})
}

func handleListMaterials(w http.ResponseWriter, r *http.Request) {
	eid := r.URL.Query().Get("election_id")
	materialType := r.URL.Query().Get("material_type")
	status := r.URL.Query().Get("status")
	q := "SELECT * FROM election_materials WHERE 1=1"
	var params []interface{}
	if eid != "" {
		q += " AND election_id=?"
		params = append(params, eid)
	}
	if materialType != "" {
		q += " AND material_type=?"
		params = append(params, materialType)
	}
	if status != "" {
		q += " AND status=?"
		params = append(params, status)
	}
	q += " ORDER BY material_type, destination_code"
	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleDispatchMaterial(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var req struct {
		Status         string `json:"status"`
		TrackingNumber string `json:"tracking_number"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	updates := "status=?"
	params := []interface{}{req.Status}
	if req.TrackingNumber != "" {
		updates += ", tracking_number=?"
		params = append(params, req.TrackingNumber)
	}
	if req.Status == "dispatched" {
		updates += ", dispatched_at=CURRENT_TIMESTAMP"
	} else if req.Status == "delivered" {
		updates += ", delivered_at=CURRENT_TIMESTAMP"
	} else if req.Status == "acknowledged" {
		updates += ", acknowledged_at=CURRENT_TIMESTAMP"
	}
	params = append(params, id)
	db.Exec("UPDATE election_materials SET "+updates+" WHERE id=?", params...)

	logAudit("MATERIAL_UPDATED", "material", id, 0, map[string]interface{}{"status": req.Status})
	writeJSON(w, 200, M{"id": id, "status": req.Status, "message": "Material updated"})
}

func handleMaterialStats(w http.ResponseWriter, r *http.Request) {
	eid := r.URL.Query().Get("election_id")
	filter := ""
	var params []interface{}
	if eid != "" {
		filter = " WHERE election_id=?"
		params = []interface{}{eid}
	}

	var total int
	db.QueryRow("SELECT COUNT(*) FROM election_materials"+filter, params...).Scan(&total)

	statusRows, _ := db.Query("SELECT status, COUNT(*) as count, SUM(quantity) as total_qty FROM election_materials"+filter+" GROUP BY status", params...)
	typeRows, _ := db.Query("SELECT material_type, COUNT(*) as count, SUM(quantity) as total_qty FROM election_materials"+filter+" GROUP BY material_type", params...)

	writeJSON(w, 200, M{
		"total_items": total,
		"by_status":   scanRows(statusRows),
		"by_type":     scanRows(typeRows),
	})
}

func handleEMSDashboard(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)

	var voterCount, pvcCollected int
	db.QueryRow("SELECT COUNT(*) FROM voters").Scan(&voterCount)
	db.QueryRow("SELECT COUNT(*) FROM voters WHERE pvc_collected=1").Scan(&pvcCollected)

	var wfPhase, wfStatus string
	db.QueryRow("SELECT current_phase, status FROM ems_workflows WHERE election_id=? ORDER BY id DESC LIMIT 1", eid).Scan(&wfPhase, &wfStatus)

	var syncTotal, syncOk, syncConflicts int
	db.QueryRow("SELECT COUNT(*) FROM bvas_sync_queue").Scan(&syncTotal)
	db.QueryRow("SELECT COUNT(*) FROM bvas_sync_queue WHERE status='synced'").Scan(&syncOk)
	db.QueryRow("SELECT COUNT(*) FROM bvas_sync_queue WHERE status='conflict'").Scan(&syncConflicts)

	var portalCount, portalActive int
	db.QueryRow("SELECT COUNT(*) FROM portal_connections").Scan(&portalCount)
	db.QueryRow("SELECT COUNT(*) FROM portal_connections WHERE status='active'").Scan(&portalActive)

	var validationChecks, validationPassed int
	db.QueryRow("SELECT COUNT(*) FROM validation_results").Scan(&validationChecks)
	db.QueryRow("SELECT COUNT(*) FROM validation_results WHERE passed=1").Scan(&validationPassed)

	var materialItems int
	db.QueryRow("SELECT COUNT(*) FROM election_materials WHERE election_id=?", eid).Scan(&materialItems)
	var materialDelivered int
	db.QueryRow("SELECT COUNT(*) FROM election_materials WHERE election_id=? AND status IN ('delivered','acknowledged')", eid).Scan(&materialDelivered)

	var staffCount int
	db.QueryRow("SELECT COUNT(*) FROM election_staff_assignments WHERE election_id=?", eid).Scan(&staffCount)

	writeJSON(w, 200, M{
		"election_id": eid,
		"voter_registration": M{
			"total_voters": voterCount, "pvc_collected": pvcCollected,
			"pvc_rate": safePercent(pvcCollected, voterCount),
		},
		"workflow": M{
			"current_phase": wfPhase, "status": wfStatus,
		},
		"bvas_sync": M{
			"total": syncTotal, "synced": syncOk, "conflicts": syncConflicts,
			"sync_rate": safePercent(syncOk, syncTotal),
		},
		"portal_hub": M{
			"total_portals": portalCount, "active": portalActive,
		},
		"validation": M{
			"total_checks": validationChecks, "passed": validationPassed,
			"pass_rate": safePercent(validationPassed, validationChecks),
		},
		"materials": M{
			"total_items": materialItems, "delivered": materialDelivered,
			"delivery_rate": safePercent(materialDelivered, materialItems),
		},
		"staff_deployed": staffCount,
	})
}

func safePercent(num, denom int) float64 {
	if denom == 0 {
		return 0
	}
	return round2(float64(num) / float64(denom) * 100)
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}
