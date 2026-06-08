package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// seedAllTables fills every remaining empty table with realistic Nigerian election data.
// Called after seedComprehensive() in main.go.
func seedAllTables(database *sql.DB) {
	var marker int
	database.QueryRow("SELECT COUNT(*) FROM voters").Scan(&marker)
	if marker > 0 {
		return // already seeded
	}
	log.Info().Msg("Seeding all remaining tables with comprehensive data...")
	rand := NewSecureRng()
	now := time.Now().Format("2006-01-02 15:04:05")
	_ = now

	// ── Voters (VoterRegPage) ──
	nigerianNames := []struct{ first, last string }{
		{"Adebayo", "Ogunleye"}, {"Chinelo", "Nwosu"}, {"Musa", "Bello"},
		{"Aisha", "Ibrahim"}, {"Emeka", "Okafor"}, {"Funke", "Adeyemi"},
		{"Hassan", "Usman"}, {"Ifeoma", "Eze"}, {"Jide", "Adewale"},
		{"Kemi", "Bakare"}, {"Lawal", "Abdullahi"}, {"Ngozi", "Chukwu"},
		{"Olumide", "Fasanya"}, {"Patience", "Etim"}, {"Rasheed", "Mustapha"},
		{"Sade", "Akintola"}, {"Tunde", "Afolabi"}, {"Uche", "Nnamdi"},
		{"Victoria", "Okoro"}, {"Wasiu", "Alabi"}, {"Yetunde", "Badmus"},
		{"Zainab", "Garba"}, {"Chukwuma", "Onwueme"}, {"Damilola", "Oladipo"},
		{"Binta", "Suleiman"}, {"Grace", "Ekong"}, {"Ibrahim", "Danjuma"},
		{"Joy", "Ikechukwu"}, {"Kunle", "Adekunle"}, {"Mary", "Okonkwo"},
	}
	states := []string{"FC", "LA", "KN", "RI", "OY", "EN", "BO", "KD", "AN", "DE", "PL", "OS", "KW", "IM", "ED", "AB", "BA", "AD", "AK", "CR"}
	for i, n := range nigerianNames {
		vin := fmt.Sprintf("VIN%019d", 1000000000+i)
		phone := fmt.Sprintf("+234%d", 8010000000+i)
		stCode := states[i%len(states)]
		puCode := fmt.Sprintf("%s-001-W001-PU%03d", stCode, (i%3)+1)
		age := 22 + rand.Intn(50)
		dbExecLog("seed_voter",
			`INSERT INTO voters (vin, first_name, last_name, date_of_birth, gender, phone, state_code, lga_code, ward_code, polling_unit_code, registration_status, biometric_captured, photo_url, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW()) ON CONFLICT DO NOTHING`,
			vin, n.first, n.last, fmt.Sprintf("%d-01-15", 2026-age), []string{"M", "F"}[i%2], phone, stCode,
			fmt.Sprintf("%s-001", stCode), fmt.Sprintf("%s-001-W001", stCode), puCode,
			[]string{"active", "active", "active", "suspended", "active"}[i%5], true, fmt.Sprintf("https://photos.inec.ng/%s.jpg", vin))
	}

	// ── Elections (ElectionsPage) ──
	var elCount int
	database.QueryRow("SELECT COUNT(*) FROM elections").Scan(&elCount)
	if elCount == 0 {
		dbExecLog("seed_el1", `INSERT INTO elections (title, election_type, election_date, status, state_code, description) VALUES ($1,$2,$3,$4,$5,$6)`,
			"2027 Presidential Election", "presidential", "2027-02-25", "active", "", "General election for President and Vice President of Nigeria")
		dbExecLog("seed_el2", `INSERT INTO elections (title, election_type, election_date, status, state_code, description) VALUES ($1,$2,$3,$4,$5,$6)`,
			"2027 Governorship Election - Lagos", "governorship", "2027-03-11", "scheduled", "LA", "Lagos State governorship election")
		dbExecLog("seed_el3", `INSERT INTO elections (title, election_type, election_date, status, state_code, description) VALUES ($1,$2,$3,$4,$5,$6)`,
			"2027 Senatorial Election - FCT", "senatorial", "2027-02-25", "completed", "FC", "FCT senatorial district election")
	}

	// ── Users (UserManagementPage) ──
	var userCount int
	database.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
	if userCount == 0 {
		users := []struct{ username, email, role, name string }{
			{"admin", "admin@inecnigeria.org", "admin", "System Administrator"},
			{"observer1", "observer1@yiaga.org", "observer", "Samson Itodo"},
			{"officer1", "officer1@inecnigeria.org", "officer", "Amina Bello"},
			{"collation1", "collation1@inecnigeria.org", "collation_officer", "Chukwudi Eze"},
			{"media1", "media1@channels.tv", "media", "Kadaria Ahmed"},
		}
		for _, u := range users {
			pwHash := fmt.Sprintf("%x", sha256.Sum256([]byte("admin123")))
			dbExecLog("seed_user",
				`INSERT INTO users (username, email, role, password_hash, full_name, is_active, login_count, created_at) VALUES ($1,$2,$3,$4,$5,true,0,NOW()) ON CONFLICT DO NOTHING`,
				u.username, u.email, u.role, pwHash, u.name)
		}
	}

	// ── Parties ──
	var partyCount int
	database.QueryRow("SELECT COUNT(*) FROM parties").Scan(&partyCount)
	if partyCount == 0 {
		parties := []struct{ code, name, abbr, color string }{
			{"APC", "All Progressives Congress", "APC", "#0000FF"},
			{"PDP", "Peoples Democratic Party", "PDP", "#FF0000"},
			{"LP", "Labour Party", "LP", "#00FF00"},
			{"NNPP", "New Nigeria People's Party", "NNPP", "#FFA500"},
			{"SDP", "Social Democratic Party", "SDP", "#FFD700"},
			{"APGA", "All Progressives Grand Alliance", "APGA", "#008000"},
			{"ADC", "African Democratic Congress", "ADC", "#800080"},
			{"YPP", "Young Progressive Party", "YPP", "#FF69B4"},
		}
		for _, p := range parties {
			dbExecLog("seed_party",
				`INSERT INTO parties (code, name, abbreviation, color, logo_url) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
				p.code, p.name, p.abbr, p.color, fmt.Sprintf("https://inec.ng/logos/%s.png", p.code))
		}
	}

	// ── BVAS Devices (BVASSyncPage) ──
	var bvasCount int
	database.QueryRow("SELECT COUNT(*) FROM bvas_devices").Scan(&bvasCount)
	if bvasCount == 0 {
		for i := 0; i < 50; i++ {
			stCode := states[i%len(states)]
			deviceID := fmt.Sprintf("BVAS-%s-%03d", stCode, i+1)
			puCode := fmt.Sprintf("%s-001-W001-PU%03d", stCode, (i%3)+1)
			dbExecLog("seed_bvas",
				`INSERT INTO bvas_devices (device_id, serial_number, firmware_version, assigned_pu, status, battery_level, last_sync, sim_provider, imei) VALUES ($1,$2,$3,$4,$5,$6,NOW(),$7,$8) ON CONFLICT DO NOTHING`,
				deviceID, fmt.Sprintf("SN%012d", 100000+i), "v4.2.1", puCode,
				[]string{"active", "active", "active", "syncing", "offline"}[i%5],
				50+rand.Intn(50),
				[]string{"MTN", "Airtel", "Glo", "9Mobile"}[i%4],
				fmt.Sprintf("35%013d", 4000000000000+int64(i)))
		}
	}

	// ── BVAS Heartbeats ──
	for i := 0; i < 20; i++ {
		stCode := states[i%len(states)]
		deviceID := fmt.Sprintf("BVAS-%s-%03d", stCode, i+1)
		dbExecLog("seed_bvas_hb",
			`INSERT INTO bvas_heartbeats (device_id, battery_level, signal_strength, gps_lat, gps_lng, firmware_version, status) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
			deviceID, 50+rand.Intn(50), -50-rand.Intn(40),
			4.0+rand.Float64()*10, 2.5+rand.Float64()*12,
			"v4.2.1", "online")
	}

	// ── BVAS Sync Queue ──
	for i := 0; i < 10; i++ {
		stCode := states[i%len(states)]
		deviceID := fmt.Sprintf("BVAS-%s-%03d", stCode, i+1)
		dbExecLog("seed_bvas_sync",
			`INSERT INTO bvas_sync_queue (device_id, data_type, payload_size, priority, status, retry_count, max_retries) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
			deviceID, []string{"accreditation", "result", "biometric"}[i%3],
			1024+rand.Intn(50000), []string{"high", "medium", "low"}[i%3],
			[]string{"pending", "synced", "failed", "pending"}[i%4], rand.Intn(3), 5)
	}

	// ── BVAS Capture Sessions ──
	for i := 0; i < 10; i++ {
		stCode := states[i%len(states)]
		deviceID := fmt.Sprintf("BVAS-%s-%03d", stCode, i+1)
		dbExecLog("seed_bvas_cap",
			`INSERT INTO bvas_capture_sessions (device_id, voter_vin, capture_type, quality_score, latitude, longitude, attempt_number, success, duration_ms) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`,
			deviceID, fmt.Sprintf("VIN%019d", 1000000000+i), []string{"fingerprint", "facial", "iris"}[i%3],
			0.7+rand.Float64()*0.3, 4.0+rand.Float64()*10, 2.5+rand.Float64()*12,
			1, true, 500+rand.Intn(3000))
	}

	// ── BVAS Device Capabilities ──
	for i := 0; i < 10; i++ {
		stCode := states[i%len(states)]
		deviceID := fmt.Sprintf("BVAS-%s-%03d", stCode, i+1)
		dbExecLog("seed_bvas_cap2",
			`INSERT INTO bvas_device_capabilities (device_id, fingerprint_sensor, facial_camera, iris_scanner, nfc_reader, gps_module, sim_slots, battery_capacity_mah, screen_size_inch, os_version, storage_gb, ram_mb, connectivity) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT DO NOTHING`,
			deviceID, true, true, false, true, true, 2, 5000, 5.5, "Android 12 BVAS", 64, 4096, "4G LTE")
	}

	// ── BVAS Location Logs ──
	for i := 0; i < 15; i++ {
		stCode := states[i%len(states)]
		deviceID := fmt.Sprintf("BVAS-%s-%03d", stCode, i+1)
		dbExecLog("seed_bvas_loc",
			`INSERT INTO bvas_location_logs (device_id, latitude, longitude, accuracy, altitude) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			deviceID, 4.0+rand.Float64()*10, 2.5+rand.Float64()*12, 5.0+rand.Float64()*20, 100+rand.Float64()*500)
	}

	// ── Biometric Profiles ──
	for i := 0; i < 15; i++ {
		vin := fmt.Sprintf("VIN%019d", 1000000000+i)
		dbExecLog("seed_bio_profile",
			`INSERT INTO biometric_profiles (voter_id, vin, enrollment_status, fingerprint_quality, facial_quality, iris_quality, last_verified, verification_count, enrollment_center, operator_id, device_id, photo_hash) VALUES ($1,$2,$3,$4,$5,$6,NOW(),$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`,
			i+1, vin, "enrolled", 0.85+rand.Float64()*0.15, 0.80+rand.Float64()*0.2, 0.0,
			rand.Intn(5), fmt.Sprintf("RC-%s-001", states[i%len(states)]),
			fmt.Sprintf("OP-%03d", i+1), fmt.Sprintf("BVAS-%s-%03d", states[i%len(states)], i+1),
			fmt.Sprintf("%x", sha256.Sum256([]byte(vin)))[:32])
	}

	// ── Biometric Templates ──
	for i := 0; i < 15; i++ {
		vin := fmt.Sprintf("VIN%019d", 1000000000+i)
		dbExecLog("seed_bio_template",
			`INSERT INTO biometric_templates (profile_id, modality, template_format, quality_score, nfiq_score, capture_device, is_active, version) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
			i+1, "fingerprint", "ISO_19794_2", 0.85+rand.Float64()*0.15, 1+rand.Intn(3),
			fmt.Sprintf("BVAS-%s-%03d", states[i%len(states)], i+1), true, 1)
		_ = vin
	}

	// ── Biometric Verifications ──
	for i := 0; i < 20; i++ {
		dbExecLog("seed_bio_verify",
			`INSERT INTO biometric_verifications (profile_id, modality, match_score, threshold, result, device_id, operator_id, latitude, longitude) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`,
			(i%15)+1, []string{"fingerprint", "facial"}[i%2], 0.7+rand.Float64()*0.3, 0.85,
			[]string{"match", "match", "match", "no_match", "match"}[i%5],
			fmt.Sprintf("BVAS-%s-%03d", states[i%len(states)], (i%10)+1),
			fmt.Sprintf("OP-%03d", (i%5)+1), 4.0+rand.Float64()*10, 2.5+rand.Float64()*12)
	}

	// ── Biometric Quality Scores ──
	for i := 0; i < 15; i++ {
		dbExecLog("seed_bio_quality",
			`INSERT INTO biometric_quality_scores (template_id, nfiq_score, iso_quality, uniformity, contrast, sharpness, minutiae_count) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
			i+1, 1+rand.Intn(3), 0.8+rand.Float64()*0.2, 0.75+rand.Float64()*0.25,
			0.7+rand.Float64()*0.3, 0.8+rand.Float64()*0.2, 30+rand.Intn(50))
	}

	// ── Biometric Match Log ──
	for i := 0; i < 10; i++ {
		dbExecLog("seed_bio_match",
			`INSERT INTO biometric_match_log (template_id_a, template_id_b, modality, match_score, threshold, is_match) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			(i%15)+1, ((i+5)%15)+1, "fingerprint", 0.5+rand.Float64()*0.5, 0.85,
			[]bool{true, false, true, true, false}[i%5])
	}

	// ── Collation Results ──
	for i := 0; i < 15; i++ {
		stCode := states[i%len(states)]
		dbExecLog("seed_collation",
			`INSERT INTO collation_results (election_id, level, area_code, area_name, total_registered, total_accredited, total_valid, total_rejected, status, collation_officer_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`,
			1, []string{"ward", "lga", "state"}[i%3],
			fmt.Sprintf("%s-001-W%03d", stCode, (i%5)+1),
			fmt.Sprintf("Ward %d, %s", (i%5)+1, stCode),
			5000+rand.Intn(15000), 3000+rand.Intn(8000),
			2500+rand.Intn(6000), 50+rand.Intn(200),
			[]string{"pending", "in_progress", "completed", "verified"}[i%4], 1)
	}

	// ── Collation Party Scores ──
	partyList := []string{"APC", "PDP", "LP", "NNPP", "SDP", "APGA", "ADC", "YPP"}
	for i := 0; i < 15; i++ {
		for _, p := range partyList[:4] {
			dbExecLog("seed_coll_party",
				`INSERT INTO collation_party_scores (collation_id, party_code, votes) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
				i+1, p, 200+rand.Intn(2000))
		}
	}

	// ── Escalation Rules (CommandCenterPage) ──
	escalationRules := []struct{ name, condition, action, severity string }{
		{"Zero Submission Alert", "pu_submissions == 0 AND hours_since_open > 2", "notify_state_rec", "warning"},
		{"Anomaly Surge", "anomaly_count > 5 AND time_window < 1h", "pause_collation", "critical"},
		{"BVAS Connectivity Drop", "bvas_online_pct < 50", "notify_state_rec", "warning"},
		{"Over-voting Detection", "votes_cast > accredited_voters", "flag_and_pause", "critical"},
		{"Result Transmission Delay", "transmission_delay > 3h", "escalate_to_chairman", "emergency"},
	}
	for _, er := range escalationRules {
		dbExecLog("seed_esc_rule",
			`INSERT INTO escalation_rules (name, condition_expr, action_type, severity, is_active, cooldown_minutes) VALUES ($1,$2,$3,$4,true,$5) ON CONFLICT DO NOTHING`,
			er.name, er.condition, er.action, er.severity, 30)
	}

	// ── Escalation Log ──
	for i := 0; i < 8; i++ {
		dbExecLog("seed_esc_log",
			`INSERT INTO escalation_log (rule_id, triggered_by, state_code, details, resolved) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			(i%5)+1, "system", states[i%len(states)],
			fmt.Sprintf("Auto-triggered by rule at %s", states[i%len(states)]),
			[]bool{true, false, true, false}[i%4])
	}

	// ── Command Center Config ──
	var ccCount int
	database.QueryRow("SELECT COUNT(*) FROM command_center_config").Scan(&ccCount)
	if ccCount == 0 {
		dbExecLog("seed_cc_config", `INSERT INTO command_center_config (key, value) VALUES ($1,$2)`, "load_shedding_enabled", "false")
		dbExecLog("seed_cc_config2", `INSERT INTO command_center_config (key, value) VALUES ($1,$2)`, "auto_escalation_enabled", "true")
		dbExecLog("seed_cc_config3", `INSERT INTO command_center_config (key, value) VALUES ($1,$2)`, "max_concurrent_transmissions", "5000")
	}

	// ── Command Alerts ──
	var caCount int
	database.QueryRow("SELECT COUNT(*) FROM command_alerts").Scan(&caCount)
	if caCount == 0 {
		alerts := []struct{ level, stCode, msg, action string }{
			{"WARN", "LA", "15 PUs in Lagos reporting zero submissions after 2 hours", "notify_state_rec"},
			{"CRITICAL", "KN", "Anomaly surge: 8 statistical outliers in Kano", "pause_collation"},
			{"WARN", "RI", "BVAS connectivity below 50% in Rivers", "notify_state_rec"},
			{"EMERGENCY", "BO", "No submissions from entire Borno LGA for 4 hours", "notify_chairman"},
			{"WARN", "OY", "Turnout deviation exceeds 20% threshold in Oyo", "notify_state_rec"},
			{"CRITICAL", "DE", "Security threat escalation in Delta — 3 incidents in 1h", "pause_collation"},
		}
		for _, a := range alerts {
			dbExecLog("seed_cmd_alert2", `INSERT INTO command_alerts (level, state_code, message, auto_action, resolved) VALUES ($1,$2,$3,$4,$5)`,
				a.level, a.stCode, a.msg, a.action, 0)
		}
	}

	// ── Validation Rules & Results (DataValidationPage) ──
	rules := []struct{ name, desc, category, severity string }{
		{"over_voting_check", "Total votes must not exceed accredited voters", "arithmetic", "critical"},
		{"turnout_threshold", "Turnout above 95% flagged as suspicious", "statistical", "warning"},
		{"party_sum_check", "Sum of party votes must equal total valid votes", "arithmetic", "critical"},
		{"duplicate_result", "Check for duplicate results from same PU", "integrity", "critical"},
		{"geo_boundary_check", "Submission GPS must be within PU geofence", "spatial", "warning"},
		{"bvas_timestamp", "BVAS accreditation time must precede result time", "temporal", "warning"},
		{"zero_result_check", "Flag PUs with zero votes for all parties", "statistical", "info"},
		{"ec8a_consistency", "EC8A physical count must match BVAS digital count", "integrity", "critical"},
	}
	for _, r := range rules {
		dbExecLog("seed_val_rule",
			`INSERT INTO validation_rules (name, description, category, severity, is_active, auto_flag, query_template) VALUES ($1,$2,$3,$4,true,true,$5) ON CONFLICT DO NOTHING`,
			r.name, r.desc, r.category, r.severity, fmt.Sprintf("SELECT * FROM results WHERE %s", r.name))
	}
	for i := 0; i < 20; i++ {
		dbExecLog("seed_val_result",
			`INSERT INTO validation_results (rule_id, polling_unit_code, election_id, result_id, status, severity, details) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
			(i%8)+1, fmt.Sprintf("%s-001-W001-PU%03d", states[i%len(states)], (i%3)+1),
			1, (i%800)+1,
			[]string{"passed", "passed", "failed", "warning", "passed"}[i%5],
			rules[i%8].severity,
			fmt.Sprintf("Rule '%s' %s for PU", rules[i%8].name, []string{"passed", "passed", "failed"}[i%3]))
	}

	// ── Document Analyses (DocumentAIPage) ──
	for i := 0; i < 10; i++ {
		dbExecLog("seed_doc_analysis",
			`INSERT INTO document_analyses (document_type, file_url, status, confidence_score, extracted_data, validation_status, anomaly_flags, processing_time_ms) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
			[]string{"ec8a", "ec8b", "ec8c", "voter_register"}[i%4],
			fmt.Sprintf("https://docs.inec.ng/ec8a/%s-001-PU%03d.pdf", states[i%len(states)], i+1),
			[]string{"completed", "processing", "completed", "failed"}[i%4],
			0.85+rand.Float64()*0.15,
			`{"total_valid": 450, "total_rejected": 12, "accredited": 500}`,
			[]string{"valid", "valid", "discrepancy", "valid"}[i%4],
			[]string{"[]", "[]", `["digit_mismatch"]`, "[]"}[i%4],
			500+rand.Intn(3000))
	}

	// ── Dedup Jobs & Candidates (DuplicateDetectionPage) ──
	for i := 0; i < 5; i++ {
		dbExecLog("seed_dedup_job",
			`INSERT INTO dedup_jobs (job_id, job_type, modality, status, total_comparisons, duplicates_found, started_at, threshold, algorithm, batch_size) VALUES ($1,$2,$3,$4,$5,$6,NOW(),$7,$8,$9) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("DEDUP-%04d", i+1), []string{"full_scan", "incremental"}[i%2],
			"fingerprint", []string{"completed", "running", "completed"}[i%3],
			50000+rand.Intn(200000), rand.Intn(50),
			0.85, "minutiae_matching", 1000)
	}
	for i := 0; i < 10; i++ {
		dbExecLog("seed_dedup_cand",
			`INSERT INTO dedup_candidates (job_id, profile_id_a, profile_id_b, modality, match_score, status, review_notes) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
			(i%5)+1, (i%15)+1, ((i+3)%15)+1, "fingerprint",
			0.85+rand.Float64()*0.15,
			[]string{"pending", "confirmed_duplicate", "false_positive", "pending"}[i%4],
			"Awaiting manual review")
	}

	// ── Election Materials (ElectionsPage) ──
	materials := []struct{ name, category string }{
		{"BVAS Device", "equipment"}, {"EC8A Form", "form"}, {"EC8B Form", "form"},
		{"Ballot Papers", "consumable"}, {"Indelible Ink", "consumable"},
		{"Result Sheet (EC8C)", "form"}, {"Voter Register", "document"},
		{"Security Seal", "security"}, {"Tamper-Evident Bag", "security"},
		{"Ballot Box", "equipment"},
	}
	for i, m := range materials {
		dbExecLog("seed_material",
			`INSERT INTO election_materials (election_id, name, category, quantity_allocated, quantity_deployed, quantity_returned, status, tracking_code) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
			1, m.name, m.category, 50000+rand.Intn(100000), 45000+rand.Intn(50000),
			40000+rand.Intn(45000),
			[]string{"deployed", "deployed", "returned", "deployed"}[i%4],
			fmt.Sprintf("TRK-%s-%04d", m.category[:3], i+1))
	}

	// ── Election Lifecycle & State Log ──
	lifecyclePhases := []string{"planning", "registration", "campaign", "voting", "collation", "declaration"}
	for i, phase := range lifecyclePhases {
		dbExecLog("seed_lifecycle",
			`INSERT INTO election_lifecycle (election_id, phase, status, started_at) VALUES ($1,$2,$3,NOW() - ($4 * INTERVAL '1 day')) ON CONFLICT DO NOTHING`,
			1, phase, []string{"completed", "completed", "completed", "in_progress", "pending", "pending"}[i], 30-i*5)
	}
	for i := 0; i < 5; i++ {
		dbExecLog("seed_state_log",
			`INSERT INTO election_state_log (election_id, from_state, to_state, changed_by, reason) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			1, lifecyclePhases[i], lifecyclePhases[i+1], "admin", "Phase transition approved by REC")
	}

	// ── EMS Workflows (WorkflowPage) ──
	workflows := []struct{ name, wtype, status string }{
		{"Material Distribution - Lagos", "material_distribution", "active"},
		{"Staff Deployment - Kano", "staff_deployment", "completed"},
		{"Result Collation - National", "result_collation", "active"},
		{"Voter Registration - FCT", "voter_registration", "completed"},
	}
	for _, wf := range workflows {
		dbExecLog("seed_workflow",
			`INSERT INTO ems_workflows (name, workflow_type, status, created_by, election_id) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			wf.name, wf.wtype, wf.status, "admin", 1)
	}
	for i := 0; i < 8; i++ {
		dbExecLog("seed_wf_phase",
			`INSERT INTO ems_workflow_phases (workflow_id, phase_name, sequence_order, status, assigned_to) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			(i%4)+1, fmt.Sprintf("Phase %d", (i%3)+1), (i%3)+1,
			[]string{"completed", "in_progress", "pending"}[i%3], "officer1")
	}

	// ── Staff Assignments ──
	roles := []string{"presiding_officer", "asst_presiding", "poll_clerk", "security", "supervisor"}
	for i := 0; i < 15; i++ {
		dbExecLog("seed_staff",
			`INSERT INTO election_staff_assignments (election_id, user_id, role, polling_unit_code, state_code, status) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			1, (i%5)+1, roles[i%5],
			fmt.Sprintf("%s-001-W001-PU%03d", states[i%len(states)], (i%3)+1),
			states[i%len(states)], "deployed")
	}

	// ── MFA Settings ──
	dbExecLog("seed_mfa", `INSERT INTO mfa_settings (user_id, method, is_enabled, backup_codes) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		1, "totp", true, `["12345678","23456789","34567890"]`)
	dbExecLog("seed_mfa2", `INSERT INTO mfa_settings (user_id, method, is_enabled, backup_codes) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		2, "sms", true, `["11111111","22222222"]`)

	// ── KYC Verifications ──
	for i := 0; i < 5; i++ {
		vin := fmt.Sprintf("VIN%019d", 1000000000+i)
		dbExecLog("seed_kyc",
			`INSERT INTO kyc_verifications (user_id, verification_type, status, document_type, document_number, verified_by, risk_score, expiry_date) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
			i+1, "identity", []string{"verified", "verified", "pending", "verified", "failed"}[i],
			"national_id", fmt.Sprintf("NIN%012d", 10000000+i),
			"system", rand.Float64()*0.3,
			"2028-12-31")
		_ = vin
	}

	// ── KYB Verifications ──
	var kybCount int
	database.QueryRow("SELECT COUNT(*) FROM kyb_verifications").Scan(&kybCount)
	if kybCount == 0 {
		dbExecLog("seed_kyb",
			`INSERT INTO kyb_verifications (entity_name, entity_type, rc_number, status, compliance_score, risk_level, tin, verified_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			"INEC Nigeria", "government_agency", "RC-000001", "approved", 100, "low", "TIN0000001", "system")
		dbExecLog("seed_kyb2",
			`INSERT INTO kyb_verifications (entity_name, entity_type, rc_number, status, compliance_score, risk_level, tin, verified_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			"YIAGA Africa", "civil_society", "RC-123456", "approved", 85, "low", "TIN1234567", "admin")
	}

	// ── Portal Connections (PortalsPage) ──
	portals := []struct{ name, ptype, status, url string }{
		{"IReV Portal", "result_viewing", "connected", "https://irev.inecnigeria.org"},
		{"YIAGA WTV", "observer", "connected", "https://wtv.yiaga.org"},
		{"EU EOM", "observer", "connected", "https://eu-eom.europa.eu/ng"},
		{"Channels TV", "media", "connected", "https://api.channels.tv"},
		{"Premium Times", "media", "connected", "https://api.premiumtimes.com"},
	}
	for _, p := range portals {
		dbExecLog("seed_portal",
			`INSERT INTO portal_connections (name, portal_type, status, endpoint_url, api_key_hash, sync_interval_seconds, last_sync) VALUES ($1,$2,$3,$4,$5,$6,NOW()) ON CONFLICT DO NOTHING`,
			p.name, p.ptype, p.status, p.url,
			fmt.Sprintf("%x", sha256.Sum256([]byte(p.name)))[:32], 300)
	}

	// ── Portal Webhooks ──
	for i := 0; i < 5; i++ {
		dbExecLog("seed_portal_wh",
			`INSERT INTO portal_webhooks (portal_id, event_type, endpoint_url, is_active, secret_hash) VALUES ($1,$2,$3,true,$4) ON CONFLICT DO NOTHING`,
			i+1, "result.finalized", fmt.Sprintf("https://webhook.%d.example.com/inec", i+1),
			fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("wh-%d", i))))[:24])
	}

	// ── API Keys (PublicAPIPage) ──
	for i := 0; i < 5; i++ {
		keyHash := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("apikey-%d", i))))
		dbExecLog("seed_apikey",
			`INSERT INTO api_key_metadata (key_hash, name, owner, permissions, rate_limit, is_active, environment, last_used) VALUES ($1,$2,$3,$4,$5,true,$6,NOW()) ON CONFLICT DO NOTHING`,
			keyHash[:32], fmt.Sprintf("API Key - %s", []string{"IReV", "YIAGA", "EU EOM", "Channels", "Dev"}[i]),
			[]string{"irev", "yiaga", "eu-eom", "channels", "developer"}[i],
			`["results:read","elections:read"]`, 1000+i*500,
			[]string{"production", "production", "production", "production", "sandbox"}[i])
	}

	// ── API Usage ──
	for i := 0; i < 20; i++ {
		dbExecLog("seed_api_usage",
			`INSERT INTO api_usage (key_id, endpoint, method, status_code, response_time_ms) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			(i%5)+1, []string{"/api/results", "/api/elections", "/api/pus", "/api/states"}[i%4],
			"GET", []int{200, 200, 200, 429, 200}[i%5], 50+rand.Intn(500))
	}

	// ── Blockchain Audit Trail ──
	for i := 0; i < 10; i++ {
		txHash := fmt.Sprintf("0x%x", sha256.Sum256([]byte(fmt.Sprintf("tx-%d", i))))[:66]
		dbExecLog("seed_bc_audit",
			`INSERT INTO blockchain_audit_trail (tx_hash, block_number, action, entity_type, entity_id, data_hash, channel, verified) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
			txHash, 1000+i, []string{"result_submitted", "result_verified", "result_collated"}[i%3],
			"result", fmt.Sprintf("RES-%04d", i+1),
			fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("data-%d", i))))[:32],
			"inec-results", true)
	}

	// ── Fabric Blockchain ──
	for i := 0; i < 5; i++ {
		dbExecLog("seed_fabric_block",
			`INSERT INTO fabric_blocks (block_number, channel, data_hash, prev_hash, tx_count) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			1000+i, "inec-results",
			fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("block-%d", i))))[:32],
			fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("prev-%d", i))))[:32],
			5+rand.Intn(20))
	}
	for i := 0; i < 10; i++ {
		dbExecLog("seed_fabric_tx",
			`INSERT INTO fabric_transactions (tx_id, block_number, channel, chaincode, function_name, args, creator_msp, endorsers, status, data_hash) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("TX-%06d", i+1), 1000+(i/2), "inec-results", "result_chaincode",
			"submitResult", fmt.Sprintf(`["PU-%03d","{\"votes\":{\"APC\":500}}"]`, i+1),
			"INECMSP", `["peer0.inec","peer1.inec"]`, "VALID",
			fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("txdata-%d", i))))[:32])
	}

	// ── Security Audit Events ──
	for i := 0; i < 10; i++ {
		dbExecLog("seed_sec_audit",
			`INSERT INTO security_audit_events (event_type, user_id, ip_address, user_agent, details, severity) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			[]string{"login", "login_failed", "permission_denied", "data_export", "config_change"}[i%5],
			(i%5)+1, fmt.Sprintf("102.89.%d.%d", rand.Intn(255), rand.Intn(255)),
			"Mozilla/5.0 BVAS/4.2.1", fmt.Sprintf("Event %d detail", i+1),
			[]string{"info", "warning", "warning", "info", "critical"}[i%5])
	}

	// ── Data Classification & Encryption ──
	var dcCount int
	database.QueryRow("SELECT COUNT(*) FROM data_classification").Scan(&dcCount)
	if dcCount == 0 {
		fields := []struct{ name, table, level string; encrypted bool }{
			{"vin", "voters", "pii", true}, {"first_name", "voters", "pii", false},
			{"last_name", "voters", "pii", false}, {"date_of_birth", "voters", "pii", true},
			{"phone", "voters", "pii", true}, {"biometric_data", "biometric_templates", "sensitive", true},
			{"password_hash", "users", "sensitive", true}, {"photo_url", "voters", "pii", false},
			{"nin", "voters", "pii", true}, {"address", "voters", "pii", false},
			{"email", "users", "pii", false}, {"api_key", "api_keys", "sensitive", true},
			{"match_score", "biometric_verifications", "internal", false},
			{"result_data", "results", "public", false},
			{"device_imei", "bvas_devices", "internal", false},
			{"gps_coordinates", "bvas_location_logs", "internal", false},
		}
		for _, f := range fields {
			dbExecLog("seed_dc",
				`INSERT INTO data_classification (field_name, table_name, classification, is_encrypted, retention_days) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
				f.name, f.table, f.level, f.encrypted, 365*7)
		}
	}

	// ── Data Encryption Keys ──
	for i := 0; i < 3; i++ {
		dbExecLog("seed_dek",
			`INSERT INTO data_encryption_keys (key_id, algorithm, purpose, is_active, rotation_interval_days) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("DEK-%04d", i+1), "AES-256-GCM",
			[]string{"data_at_rest", "data_in_transit", "biometric_vault"}[i], true, 90)
	}

	// ── Middleware State & Events ──
	mwNames := []string{"redis", "kafka", "elasticsearch", "vault", "keycloak", "dapr", "tigerbeetle", "fluvio", "permify", "meilisearch", "waf", "ipfs", "mojaloop"}
	for _, mw := range mwNames {
		dbExecLog("seed_mw_state",
			`INSERT INTO mw_state (name, status, last_check) VALUES ($1,$2,NOW()) ON CONFLICT DO NOTHING`,
			mw, "embedded_fallback")
	}
	for i := 0; i < 20; i++ {
		dbExecLog("seed_mw_event",
			`INSERT INTO mw_events (middleware, event_type, severity, message, details) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			mwNames[i%len(mwNames)], []string{"health_check", "connection", "error", "recovery"}[i%4],
			[]string{"info", "info", "warning", "info"}[i%4],
			fmt.Sprintf("%s %s event", mwNames[i%len(mwNames)], []string{"healthy", "connected", "timeout", "recovered"}[i%4]),
			`{"latency_ms": 12, "retry_count": 0}`)
	}

	// ── TigerBeetle Ledger ──
	dbExecLog("seed_tb_acc1", `INSERT INTO tb_accounts (account_id, ledger, code, debit_balance, credit_balance, currency, description) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		"TB-ACC-001", 1, 100, 0, 50000000, "NGN", "INEC Operating Budget")
	dbExecLog("seed_tb_acc2", `INSERT INTO tb_accounts (account_id, ledger, code, debit_balance, credit_balance, currency, description) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		"TB-ACC-002", 1, 200, 25000000, 0, "NGN", "Election Materials Fund")
	for i := 0; i < 5; i++ {
		dbExecLog("seed_tb_xfer",
			`INSERT INTO tb_transfers (transfer_id, debit_account, credit_account, amount, ledger, code, description, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("XFER-%04d", i+1), "TB-ACC-001", "TB-ACC-002",
			1000000+rand.Intn(5000000), 1, 300, "Material procurement payment", "committed")
	}

	// ── Registration Centers ──
	for i := 0; i < 10; i++ {
		stCode := states[i%len(states)]
		dbExecLog("seed_reg_center",
			`INSERT INTO registration_centers (name, state_code, lga_code, address, capacity, status, latitude, longitude, operating_hours, supervisor_id, equipment_status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("RC %s-%03d", stCode, i+1), stCode, fmt.Sprintf("%s-001", stCode),
			fmt.Sprintf("Registration Center, %s", stCode),
			500+rand.Intn(1500), "active", 4.0+rand.Float64()*10, 2.5+rand.Float64()*12,
			"08:00-17:00", (i%5)+1, "operational")
	}

	// ── USSD Sessions ──
	for i := 0; i < 10; i++ {
		dbExecLog("seed_ussd",
			`INSERT INTO ussd_sessions (session_id, phone, state, last_input) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("USSD-%06d", i+1), fmt.Sprintf("+234%d", 8010000000+i),
			[]string{"menu", "check_result", "completed", "verify_vin"}[i%4], "*141#")
	}

	// ── SMS Verifications ──
	for i := 0; i < 5; i++ {
		dbExecLog("seed_sms_verify",
			`INSERT INTO sms_verifications (phone, code, purpose, verified, attempts) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("+234%d", 8010000000+i), fmt.Sprintf("%06d", 100000+rand.Intn(899999)),
			"voter_registration", []bool{true, false, true}[i%3], rand.Intn(3))
	}

	// ── Observer Alert Rules & Photo Verifications ──
	for i := 0; i < 5; i++ {
		dbExecLog("seed_obs_rule",
			`INSERT INTO observer_alert_rules (name, condition_expr, severity, action, is_active, cooldown_minutes) VALUES ($1,$2,$3,$4,true,$5) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("Observer Rule %d", i+1),
			[]string{"missed_checkin > 2h", "outside_geofence", "no_report > 4h", "device_offline", "suspicious_pattern"}[i],
			[]string{"warning", "critical", "warning", "critical", "info"}[i],
			"notify_coordinator", 60)
	}

	// ── Geo Events & Official Tracking History ──
	for i := 0; i < 10; i++ {
		dbExecLog("seed_geo_event",
			`INSERT INTO geo_events (event_type, polling_unit_code, latitude, longitude, payload) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			[]string{"official_move", "crowd_alert", "geofence_breach", "drone_position"}[i%4],
			fmt.Sprintf("%s-001-W001-PU001", states[i%len(states)]),
			4.0+rand.Float64()*10, 2.5+rand.Float64()*12,
			`{"activity":"patrolling","speed":2.5}`)
	}
	for i := 0; i < 15; i++ {
		stCode := states[i%len(states)]
		dbExecLog("seed_track_hist",
			`INSERT INTO official_tracking_history (staff_id, role, latitude, longitude, pu_code, activity, battery_pct, speed_kmh, heading, accuracy_m, recorded_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW() - ($11 * INTERVAL '1 minute')) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("INEC-PO-%03d", i+1), roles[i%5],
			4.0+rand.Float64()*10, 2.5+rand.Float64()*12,
			fmt.Sprintf("%s-001-W001-PU%03d", stCode, (i%3)+1),
			[]string{"deployed", "monitoring", "accrediting", "counting", "transmitting"}[i%5],
			50+rand.Intn(50), rand.Float64()*5, rand.Float64()*360, 5+rand.Float64()*20, i*10)
	}

	// ── Incidents (separate table) ──
	for i := 0; i < 10; i++ {
		dbExecLog("seed_incident2",
			`INSERT INTO incidents (title, description, severity, status, polling_unit_code, reported_by, latitude, longitude) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("Incident at %s PU", states[i%len(states)]),
			"Election day incident requiring attention",
			[]string{"low", "medium", "high", "critical"}[i%4],
			[]string{"reported", "investigating", "resolved", "escalated"}[i%4],
			fmt.Sprintf("%s-001-W001-PU%03d", states[i%len(states)], (i%3)+1),
			"officer1", 4.0+rand.Float64()*10, 2.5+rand.Float64()*12)
	}

	// ── Export Audit Log ──
	for i := 0; i < 5; i++ {
		dbExecLog("seed_export_audit",
			`INSERT INTO export_audit_log (user_id, export_type, format, record_count) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			1, []string{"results", "voters", "incidents", "audit_trail", "analytics"}[i],
			[]string{"csv", "xlsx", "pdf", "json", "csv"}[i], 100+rand.Intn(5000))
	}

	// ── WAF Blocklist ──
	dbExecLog("seed_waf1", `INSERT INTO waf_blocklist (ip_address, reason, expires_at) VALUES ($1,$2,NOW() + INTERVAL '24 hours') ON CONFLICT DO NOTHING`,
		"192.168.1.100", "Brute force login attempt")
	dbExecLog("seed_waf2", `INSERT INTO waf_blocklist (ip_address, reason, expires_at) VALUES ($1,$2,NOW() + INTERVAL '24 hours') ON CONFLICT DO NOTHING`,
		"10.0.0.50", "SQL injection attempt detected")

	// ── Liveness Checks ──
	for i := 0; i < 10; i++ {
		dbExecLog("seed_liveness",
			`INSERT INTO liveness_checks (profile_id, challenge_type, result, confidence, device_id, attempt_number) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			(i%15)+1, []string{"blink", "head_turn", "smile", "random_pose"}[i%4],
			[]string{"pass", "pass", "fail", "pass"}[i%4],
			0.8+rand.Float64()*0.2, fmt.Sprintf("BVAS-%s-%03d", states[i%len(states)], i+1), 1)
	}

	// ── Result Signatures ──
	for i := 0; i < 10; i++ {
		dbExecLog("seed_result_sig",
			`INSERT INTO result_signatures (result_id, signer_id, signature_hash, signer_role, verification_status, algorithm) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			(i%800)+1, (i%5)+1,
			fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("sig-%d", i))))[:64],
			roles[i%5], "verified", "Ed25519")
	}

	// ── Offline Sync Queue ──
	for i := 0; i < 5; i++ {
		dbExecLog("seed_offline_sync",
			`INSERT INTO offline_sync_queue (device_id, data_type, payload, priority, status, retry_count) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("BVAS-%s-%03d", states[i%len(states)], i+1),
			[]string{"result", "accreditation", "incident"}[i%3],
			`{"pu_code":"FC-001-W001-PU001","data":"..."}`,
			[]string{"high", "medium", "low"}[i%3],
			[]string{"pending", "synced", "failed"}[i%3], rand.Intn(3))
	}

	// ── Model Metrics (ML Dashboard) ──
	models := []string{"xgboost_anomaly", "gat_gnn", "cdcn_liveness", "paddleocr_ec8a"}
	for _, m := range models {
		dbExecLog("seed_model_metric",
			`INSERT INTO model_metrics (model_name, accuracy, precision_score, recall, f1_score) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			m, 0.9+rand.Float64()*0.1, 0.9+rand.Float64()*0.1,
			0.85+rand.Float64()*0.15, 0.9+rand.Float64()*0.1)
	}

	// ── Ingestion Jobs (Lakehouse) ──
	for i := 0; i < 5; i++ {
		dbExecLog("seed_ingest",
			`INSERT INTO ingestion_jobs (job_id, source, source_type, status, records_total, records_processed, records_failed, tier, output_path) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("INGEST-%04d", i+1),
			[]string{"bvas_results", "accreditations", "voter_register", "incidents", "geo_events"}[i],
			"batch", []string{"completed", "running", "completed", "failed", "completed"}[i],
			10000+rand.Intn(50000), 9500+rand.Intn(500), rand.Intn(100),
			[]string{"bronze", "silver", "gold"}[i%3],
			fmt.Sprintf("/data/lakehouse/%s/data.parquet", []string{"bronze", "silver", "gold"}[i%3]))
	}

	log.Info().Msg("All-tables seed complete — every page now has data")
}
