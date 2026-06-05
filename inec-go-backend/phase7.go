package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

func initPhase7Tables(database *sql.DB) {
	schema := `
	-- Module 1: Enhanced Biometric Verification System
	CREATE TABLE IF NOT EXISTS biometric_profiles (
		id SERIAL PRIMARY KEY,
		voter_vin TEXT NOT NULL,
		fingerprint_hash TEXT,
		facial_hash TEXT,
		iris_hash TEXT,
		modalities_enrolled TEXT NOT NULL DEFAULT 'fingerprint',
		quality_score REAL DEFAULT 0,
		enrollment_device TEXT,
		enrollment_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_verified_at TIMESTAMP,
		match_count INTEGER DEFAULT 0,
		duplicate_flag INTEGER DEFAULT 0,
		duplicate_matched_vin TEXT,
		status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','suspended','flagged','revoked')),
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS biometric_verifications (
		id SERIAL PRIMARY KEY,
		voter_vin TEXT NOT NULL,
		device_id TEXT,
		modality TEXT NOT NULL CHECK(modality IN ('fingerprint','facial','iris','multi_modal')),
		match_score REAL NOT NULL,
		threshold REAL DEFAULT 0.85,
		result TEXT NOT NULL CHECK(result IN ('match','no_match','uncertain','spoof_detected')),
		latency_ms INTEGER,
		polling_unit_code TEXT,
		election_id INTEGER,
		verified_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS abis_duplicate_checks (
		id SERIAL PRIMARY KEY,
		source_vin TEXT NOT NULL,
		candidate_vin TEXT,
		similarity_score REAL NOT NULL,
		modality TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','confirmed_duplicate','false_positive','resolved')),
		reviewed_by INTEGER,
		reviewed_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Module 2: Blockchain-Enhanced Result Transmission
	CREATE TABLE IF NOT EXISTS blockchain_results (
		id SERIAL PRIMARY KEY,
		result_id INTEGER NOT NULL,
		ec8a_hash TEXT NOT NULL,
		prev_hash TEXT NOT NULL DEFAULT '',
		block_index INTEGER NOT NULL,
		nonce INTEGER DEFAULT 0,
		block_hash TEXT NOT NULL,
		merkle_root TEXT,
		level TEXT NOT NULL CHECK(level IN ('polling_unit','ward','lga','state','national')),
		smart_contract_id TEXT,
		validation_status TEXT NOT NULL DEFAULT 'pending' CHECK(validation_status IN ('pending','validated','rejected','disputed')),
		validator_count INTEGER DEFAULT 0,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS smart_contracts (
		id SERIAL PRIMARY KEY,
		contract_id TEXT UNIQUE NOT NULL,
		contract_type TEXT NOT NULL CHECK(contract_type IN ('pu_validation','ward_aggregation','lga_aggregation','state_aggregation','national_declaration')),
		level TEXT NOT NULL,
		area_code TEXT NOT NULL,
		election_id INTEGER NOT NULL,
		conditions TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','executed','failed','expired')),
		executed_at TIMESTAMP,
		result_hash TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS blockchain_audit_trail (
		id SERIAL PRIMARY KEY,
		action TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		actor TEXT,
		prev_state TEXT,
		new_state TEXT,
		tx_hash TEXT NOT NULL,
		block_ref INTEGER,
		ip_address TEXT,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Module 3: Training & Capacity Building Platform
	CREATE TABLE IF NOT EXISTS training_courses (
		id SERIAL PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT,
		course_type TEXT NOT NULL CHECK(course_type IN ('vr_simulation','gamified','video','interactive','assessment')),
		target_role TEXT NOT NULL,
		difficulty TEXT NOT NULL DEFAULT 'beginner' CHECK(difficulty IN ('beginner','intermediate','advanced','expert')),
		duration_minutes INTEGER DEFAULT 60,
		passing_score INTEGER DEFAULT 70,
		modules_count INTEGER DEFAULT 1,
		is_mandatory INTEGER DEFAULT 0,
		is_active INTEGER DEFAULT 1,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS training_enrollments (
		id SERIAL PRIMARY KEY,
		user_id INTEGER NOT NULL,
		course_id INTEGER NOT NULL,
		progress_percent REAL DEFAULT 0,
		current_module INTEGER DEFAULT 1,
		score INTEGER,
		status TEXT NOT NULL DEFAULT 'enrolled' CHECK(status IN ('enrolled','in_progress','completed','failed','expired')),
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		certificate_hash TEXT,
		enrolled_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS training_certificates (
		id SERIAL PRIMARY KEY,
		enrollment_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		course_id INTEGER NOT NULL,
		certificate_id TEXT UNIQUE NOT NULL,
		blockchain_hash TEXT NOT NULL,
		score INTEGER NOT NULL,
		issued_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP,
		verification_url TEXT,
		FOREIGN KEY (enrollment_id) REFERENCES training_enrollments(id)
	);
	CREATE TABLE IF NOT EXISTS training_vr_scenarios (
		id SERIAL PRIMARY KEY,
		course_id INTEGER NOT NULL,
		scenario_name TEXT NOT NULL,
		scenario_type TEXT NOT NULL CHECK(scenario_type IN ('election_day','emergency','crowd_control','result_collation','equipment_setup','conflict_resolution')),
		description TEXT,
		max_score INTEGER DEFAULT 100,
		avg_completion_time INTEGER,
		difficulty TEXT DEFAULT 'intermediate',
		is_active INTEGER DEFAULT 1,
		FOREIGN KEY (course_id) REFERENCES training_courses(id)
	);

	-- Module 4: Electoral Stakeholder Engagement System
	CREATE TABLE IF NOT EXISTS stakeholders (
		id SERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		organization TEXT,
		stakeholder_type TEXT NOT NULL CHECK(stakeholder_type IN ('party_agent','observer','media','cso','diplomat','security','candidate','legal')),
		email TEXT,
		phone TEXT,
		credential_id TEXT UNIQUE,
		credential_qr TEXT,
		nfc_tag TEXT,
		accreditation_status TEXT NOT NULL DEFAULT 'pending' CHECK(accreditation_status IN ('pending','approved','rejected','suspended','expired')),
		election_id INTEGER,
		assigned_area TEXT,
		photo_url TEXT,
		registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS stakeholder_incidents (
		id SERIAL PRIMARY KEY,
		reporter_id INTEGER NOT NULL,
		incident_type TEXT NOT NULL CHECK(incident_type IN ('violence','intimidation','ballot_stuffing','equipment_failure','process_violation','other')),
		description TEXT NOT NULL,
		severity TEXT NOT NULL DEFAULT 'medium' CHECK(severity IN ('low','medium','high','critical')),
		latitude REAL,
		longitude REAL,
		polling_unit_code TEXT,
		media_urls TEXT,
		status TEXT NOT NULL DEFAULT 'reported' CHECK(status IN ('reported','acknowledged','investigating','resolved','escalated','dismissed')),
		assigned_to INTEGER,
		resolution_notes TEXT,
		reported_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		resolved_at TIMESTAMP,
		FOREIGN KEY (reporter_id) REFERENCES stakeholders(id)
	);
	CREATE TABLE IF NOT EXISTS grievances (
		id SERIAL PRIMARY KEY,
		stakeholder_id INTEGER NOT NULL,
		grievance_type TEXT NOT NULL CHECK(grievance_type IN ('result_dispute','process_complaint','staff_misconduct','access_denial','equipment_issue','other')),
		subject TEXT NOT NULL,
		description TEXT NOT NULL,
		evidence_urls TEXT,
		priority TEXT NOT NULL DEFAULT 'normal' CHECK(priority IN ('low','normal','high','urgent')),
		status TEXT NOT NULL DEFAULT 'filed' CHECK(status IN ('filed','under_review','hearing_scheduled','resolved','appealed','dismissed')),
		assigned_to TEXT,
		resolution TEXT,
		filed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		resolved_at TIMESTAMP,
		FOREIGN KEY (stakeholder_id) REFERENCES stakeholders(id)
	);
	CREATE TABLE IF NOT EXISTS push_notifications (
		id SERIAL PRIMARY KEY,
		target_type TEXT NOT NULL CHECK(target_type IN ('all','stakeholder_type','individual','area')),
		target_value TEXT,
		title TEXT NOT NULL,
		body TEXT NOT NULL,
		notification_type TEXT NOT NULL DEFAULT 'info' CHECK(notification_type IN ('info','alert','update','emergency')),
		sent_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		read_count INTEGER DEFAULT 0,
		total_recipients INTEGER DEFAULT 0
	);

	-- Module 5: AI-Powered Election Monitoring & Analytics
	CREATE TABLE IF NOT EXISTS ai_predictions (
		id SERIAL PRIMARY KEY,
		prediction_type TEXT NOT NULL CHECK(prediction_type IN ('turnout','resource','security_threat','sentiment','misinformation')),
		target_area TEXT NOT NULL,
		target_level TEXT NOT NULL CHECK(target_level IN ('national','state','lga','ward','polling_unit')),
		predicted_value REAL NOT NULL,
		confidence REAL NOT NULL,
		model_name TEXT NOT NULL,
		features_used TEXT,
		election_id INTEGER,
		predicted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS sentiment_analysis (
		id SERIAL PRIMARY KEY,
		source TEXT NOT NULL CHECK(source IN ('twitter','facebook','news','radio','whatsapp','other')),
		content_snippet TEXT,
		sentiment TEXT NOT NULL CHECK(sentiment IN ('positive','negative','neutral','mixed')),
		score REAL NOT NULL,
		topics TEXT,
		location TEXT,
		language TEXT DEFAULT 'en',
		election_id INTEGER,
		analyzed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS misinformation_alerts (
		id SERIAL PRIMARY KEY,
		content TEXT NOT NULL,
		source_platform TEXT,
		source_url TEXT,
		classification TEXT NOT NULL CHECK(classification IN ('fake_result','false_claim','manipulated_media','impersonation','incitement','other')),
		confidence REAL NOT NULL,
		severity TEXT NOT NULL DEFAULT 'medium' CHECK(severity IN ('low','medium','high','critical')),
		reach_estimate INTEGER DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'detected' CHECK(status IN ('detected','verified','debunked','monitoring','escalated')),
		fact_check TEXT,
		detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS security_threats (
		id SERIAL PRIMARY KEY,
		threat_type TEXT NOT NULL CHECK(threat_type IN ('violence','protest','road_blockage','device_theft','cyber_attack','impersonation','other')),
		location TEXT NOT NULL,
		latitude REAL,
		longitude REAL,
		severity TEXT NOT NULL DEFAULT 'medium' CHECK(severity IN ('low','medium','high','critical')),
		confidence REAL DEFAULT 0.5,
		source TEXT,
		description TEXT,
		affected_pus INTEGER DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','monitoring','mitigated','resolved','false_alarm')),
		detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		resolved_at TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS cv_monitoring (
		id SERIAL PRIMARY KEY,
		camera_id TEXT NOT NULL,
		polling_unit_code TEXT,
		event_type TEXT NOT NULL CHECK(event_type IN ('crowd_size','queue_length','suspicious_activity','equipment_status','accessibility_issue')),
		value REAL,
		description TEXT,
		frame_url TEXT,
		confidence REAL DEFAULT 0.8,
		detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_bio_voter ON biometric_profiles(voter_vin);
	CREATE INDEX IF NOT EXISTS idx_bio_verif ON biometric_verifications(voter_vin, verified_at);
	CREATE INDEX IF NOT EXISTS idx_blockchain_result ON blockchain_results(result_id);
	CREATE INDEX IF NOT EXISTS idx_smart_contract ON smart_contracts(election_id, level);
	CREATE INDEX IF NOT EXISTS idx_training_enroll ON training_enrollments(user_id, course_id);
	CREATE INDEX IF NOT EXISTS idx_stakeholder_type ON stakeholders(stakeholder_type);
	CREATE INDEX IF NOT EXISTS idx_grievance_status ON grievances(status);
	CREATE INDEX IF NOT EXISTS idx_ai_pred ON ai_predictions(prediction_type, target_area);
	CREATE INDEX IF NOT EXISTS idx_sentiment ON sentiment_analysis(sentiment, analyzed_at);
	CREATE INDEX IF NOT EXISTS idx_misinfo ON misinformation_alerts(status, detected_at);
	CREATE INDEX IF NOT EXISTS idx_security ON security_threats(status, severity);
	`
	execMulti(database, schema)
}

func seedPhase7Data(database *sql.DB) {
	var count int
	database.QueryRow("SELECT COUNT(*) FROM biometric_profiles").Scan(&count)
	if count > 0 {
		return
	}

	rng := NewSecureRng()
	tx, _ := database.Begin()

	voterRows, err := database.Query("SELECT vin, biometric_hash FROM voters ORDER BY RANDOM() LIMIT 500")
	var vins, bioHashes []string
	if err == nil {
		for voterRows.Next() {
			var v, b string
			voterRows.Scan(&v, &b)
			vins = append(vins, v)
			bioHashes = append(bioHashes, b)
		}
		voterRows.Close()
	}
	_ = bioHashes

	for i, vin := range vins {
		fpHash := fmt.Sprintf("%x", sha256.Sum256([]byte("fp-"+vin)))[:32]
		faceHash := fmt.Sprintf("%x", sha256.Sum256([]byte("face-"+vin)))[:32]
		irisHash := ""
		modalities := "fingerprint,facial"
		if rng.Float64() < 0.3 {
			irisHash = fmt.Sprintf("%x", sha256.Sum256([]byte("iris-"+vin)))[:32]
			modalities = "fingerprint,facial,iris"
		}
		quality := 0.7 + rng.Float64()*0.3
		dupFlag := 0
		dupVin := ""
		if rng.Float64() < 0.02 && i > 0 {
			dupFlag = 1
			dupVin = vins[rng.Intn(i)]
		}
		tx.Exec(`INSERT INTO biometric_profiles (voter_vin, fingerprint_hash, facial_hash, iris_hash, modalities_enrolled, quality_score, enrollment_device, duplicate_flag, duplicate_matched_vin, status) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			vin, fpHash, faceHash, irisHash, modalities, quality,
			fmt.Sprintf("BVAS-%03d", rng.Intn(500)+1), dupFlag, dupVin, "active")

		for j := 0; j < 1+rng.Intn(3); j++ {
			mods := []string{"fingerprint", "facial", "multi_modal"}
			mod := mods[rng.Intn(len(mods))]
			score := 0.6 + rng.Float64()*0.4
			result := "match"
			if score < 0.85 {
				result = "no_match"
			}
			if rng.Float64() < 0.01 {
				result = "spoof_detected"
			}
			tx.Exec(`INSERT INTO biometric_verifications (voter_vin, device_id, modality, match_score, result, latency_ms, verified_at) VALUES (?,?,?,?,?,?,NOW() + CAST(? AS INTERVAL))`,
				vin, fmt.Sprintf("BVAS-%03d", rng.Intn(500)+1), mod, score, result,
				50+rng.Intn(200), fmt.Sprintf("-%d hours", rng.Intn(72)))
		}
	}

	for i := 0; i < 15 && len(vins) > 0; i++ {
		src := vins[rng.Intn(len(vins))]
		cand := vins[rng.Intn(len(vins))]
		sim := 0.7 + rng.Float64()*0.3
		statuses := []string{"pending", "confirmed_duplicate", "false_positive", "resolved"}
		tx.Exec(`INSERT INTO abis_duplicate_checks (source_vin, candidate_vin, similarity_score, modality, status) VALUES (?,?,?,?,?)`,
			src, cand, sim, "fingerprint", statuses[rng.Intn(len(statuses))])
	}

	var electionID int
	database.QueryRow("SELECT id FROM elections LIMIT 1").Scan(&electionID)

	resultRows, err2 := database.Query("SELECT id FROM results ORDER BY id LIMIT 200")
	var resultIDs []int
	if err2 == nil {
		for resultRows.Next() {
			var rid int
			resultRows.Scan(&rid)
			resultIDs = append(resultIDs, rid)
		}
		resultRows.Close()
	}

	prevHash := "0000000000000000000000000000000000000000000000000000000000000000"
	for i, rid := range resultIDs {
		ec8aData := fmt.Sprintf("EC8A-RESULT-%d-ELECTION-%d-TIMESTAMP-%d", rid, electionID, time.Now().Unix())
		ec8aHash := fmt.Sprintf("%x", sha256.Sum256([]byte(ec8aData)))
		blockData := fmt.Sprintf("%d-%s-%s", i, prevHash, ec8aHash)
		blockHash := fmt.Sprintf("%x", sha256.Sum256([]byte(blockData)))
		merkle := fmt.Sprintf("%x", sha256.Sum256([]byte(ec8aHash+blockHash)))
		levels := []string{"polling_unit", "polling_unit", "polling_unit", "ward", "lga"}
		valStatus := []string{"validated", "validated", "validated", "pending"}
		tx.Exec(`INSERT INTO blockchain_results (result_id, ec8a_hash, prev_hash, block_index, block_hash, merkle_root, level, validation_status, validator_count) VALUES (?,?,?,?,?,?,?,?,?)`,
			rid, ec8aHash, prevHash, i, blockHash, merkle[:32], levels[rng.Intn(len(levels))],
			valStatus[rng.Intn(len(valStatus))], rng.Intn(5)+1)
		prevHash = blockHash
	}

	contracts := []struct{ ctype, level, area string }{
		{"pu_validation", "polling_unit", "PU-001"},
		{"ward_aggregation", "ward", "WARD-001"},
		{"lga_aggregation", "lga", "LGA-001"},
		{"state_aggregation", "state", "LA"},
		{"national_declaration", "national", "NG"},
	}
	for i, c := range contracts {
		cid := fmt.Sprintf("SC-%04d-%s", i+1, c.ctype)
		status := "active"
		if rng.Float64() < 0.3 {
			status = "executed"
		}
		tx.Exec(`INSERT INTO smart_contracts (contract_id, contract_type, level, area_code, election_id, conditions, status) VALUES (?,?,?,?,?,?,?)`,
			cid, c.ctype, c.level, c.area, electionID,
			`{"min_validators":3,"threshold":0.95,"timeout_hours":24}`, status)
	}

	for i := 0; i < 50; i++ {
		actions := []string{"result_uploaded", "result_validated", "hash_verified", "contract_executed", "dispute_raised"}
		entities := []string{"result", "smart_contract", "voter", "election"}
		txHash := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("tx-%d-%d", i, time.Now().UnixNano()))))
		tx.Exec(`INSERT INTO blockchain_audit_trail (action, entity_type, entity_id, actor, tx_hash, timestamp) VALUES (?,?,?,?,?,NOW() + CAST(? AS INTERVAL))`,
			actions[rng.Intn(len(actions))], entities[rng.Intn(len(entities))],
			fmt.Sprintf("%d", rng.Intn(200)+1), fmt.Sprintf("user_%d", rng.Intn(10)+1),
			txHash, fmt.Sprintf("-%d hours", rng.Intn(168)))
	}

	courses := []struct {
		title, ctype, role, diff string
		dur, pass, mods          int
		mandatory                bool
	}{
		{"Election Day Procedures", "vr_simulation", "presiding_officer", "intermediate", 120, 80, 6, true},
		{"BVAS Operation & Troubleshooting", "interactive", "ad_hoc_staff", "beginner", 90, 70, 5, true},
		{"Result Collation Process", "vr_simulation", "collation_officer", "advanced", 150, 85, 8, true},
		{"Voter Accreditation Protocol", "gamified", "ad_hoc_staff", "beginner", 60, 65, 4, false},
		{"Emergency Response Scenarios", "vr_simulation", "presiding_officer", "expert", 180, 90, 10, true},
		{"Electoral Law Fundamentals", "video", "all", "beginner", 45, 60, 3, false},
		{"Conflict De-escalation", "vr_simulation", "security", "intermediate", 90, 75, 5, false},
		{"Accessibility & Inclusive Voting", "interactive", "presiding_officer", "beginner", 60, 70, 4, true},
		{"Digital Literacy for BVAS", "gamified", "ad_hoc_staff", "beginner", 75, 65, 5, false},
		{"Senior Returning Officer Training", "vr_simulation", "returning_officer", "expert", 240, 90, 12, true},
	}
	for _, c := range courses {
		m := 0
		if c.mandatory {
			m = 1
		}
		tx.Exec(`INSERT INTO training_courses (title, course_type, target_role, difficulty, duration_minutes, passing_score, modules_count, is_mandatory) VALUES (?,?,?,?,?,?,?,?)`,
			c.title, c.ctype, c.role, c.diff, c.dur, c.pass, c.mods, m)
	}

	for i := 0; i < 80; i++ {
		uid := rng.Intn(50) + 1
		cid := rng.Intn(10) + 1
		progress := float64(rng.Intn(101))
		score := rng.Intn(101)
		status := "in_progress"
		if progress >= 100 {
			status = "completed"
			if score < 70 {
				status = "failed"
			}
		}
		tx.Exec(`INSERT INTO training_enrollments (user_id, course_id, progress_percent, current_module, score, status, started_at, completed_at) VALUES (?,?,?,?,?,?,NOW() + CAST(? AS INTERVAL),CASE WHEN ?='completed' THEN NOW() + CAST(? AS INTERVAL) ELSE NULL END)`,
			uid, cid, progress, 1+rng.Intn(5), score, status,
			fmt.Sprintf("-%d days", rng.Intn(30)), status, fmt.Sprintf("-%d days", rng.Intn(10)))

		if status == "completed" && score >= 70 {
			certID := fmt.Sprintf("CERT-%04d-%04d-%s", uid, cid, time.Now().Format("20060102"))
			certHash := fmt.Sprintf("%x", sha256.Sum256([]byte(certID)))
			tx.Exec(`INSERT INTO training_certificates (enrollment_id, user_id, course_id, certificate_id, blockchain_hash, score) VALUES (?,?,?,?,?,?)`,
				i+1, uid, cid, certID, certHash, score)
		}
	}

	vrScenarios := []struct {
		cid         int
		name, stype string
	}{
		{1, "Standard Polling Day", "election_day"},
		{1, "Equipment Malfunction", "equipment_setup"},
		{3, "Multi-Level Collation", "result_collation"},
		{5, "Security Breach Response", "emergency"},
		{5, "Crowd Surge Management", "crowd_control"},
		{7, "Agent Dispute Resolution", "conflict_resolution"},
		{10, "Full Election Simulation", "election_day"},
	}
	for _, s := range vrScenarios {
		tx.Exec(`INSERT INTO training_vr_scenarios (course_id, scenario_name, scenario_type, max_score, avg_completion_time) VALUES (?,?,?,100,?)`,
			s.cid, s.name, s.stype, 30+rng.Intn(60))
	}

	stTypes := []string{"party_agent", "observer", "media", "cso", "diplomat", "security", "candidate", "legal"}
	orgs := []string{"APC", "PDP", "LP", "NNPP", "EU-EOM", "Commonwealth", "TMG", "YIAGA", "Channels TV", "BBC Africa",
		"NDI", "IRI", "INEC Legal", "Nigeria Police", "DSS", "Premium Times", "Punch News", "Guardian NG"}
	for i := 0; i < 120; i++ {
		sType := stTypes[rng.Intn(len(stTypes))]
		org := orgs[rng.Intn(len(orgs))]
		credID := fmt.Sprintf("CRED-%s-%04d", strings.ToUpper(sType[:3]), i+1)
		qr := fmt.Sprintf("https://inec.ng/verify/%s", credID)
		statuses := []string{"approved", "approved", "approved", "pending", "suspended"}
		firstNames := []string{"Adebayo", "Chukwuma", "Fatima", "Ibrahim", "Ngozi", "Olumide", "Aisha", "Emeka", "Hauwa", "Tunde", "Chioma", "Musa", "Amina", "Chidi", "Binta", "Segun", "Halima", "Obiora", "Zainab", "Femi"}
		lastNames := []string{"Okafor", "Mohammed", "Adeyemi", "Bello", "Nwosu", "Ibrahim", "Ogunleye", "Abubakar", "Eze", "Yusuf", "Adeniyi", "Suleiman", "Okoro", "Aliyu", "Bakare", "Danladi", "Onyeka", "Hassan", "Adeleke", "Usman"}
		sName := fmt.Sprintf("%s %s", firstNames[rng.Intn(len(firstNames))], lastNames[rng.Intn(len(lastNames))])
		tx.Exec(`INSERT INTO stakeholders (name, organization, stakeholder_type, credential_id, credential_qr, accreditation_status, election_id) VALUES (?,?,?,?,?,?,?)`,
			sName, org, sType, credID, qr,
			statuses[rng.Intn(len(statuses))], electionID)
	}

	incTypes := []string{"violence", "intimidation", "ballot_stuffing", "equipment_failure", "process_violation", "other"}
	for i := 0; i < 35; i++ {
		repID := rng.Intn(120) + 1
		sev := []string{"low", "medium", "high", "critical"}
		stat := []string{"reported", "acknowledged", "investigating", "resolved", "escalated"}
		incDescs := []string{
			"Thugs disrupted voting at polling unit and scattered ballot papers",
			"Unknown persons attempted to intimidate voters outside polling station",
			"BVAS device malfunctioned during accreditation process",
			"Presiding officer refused to allow party agents into polling booth",
			"Suspected ballot box snatching reported near market area",
			"Voters turned away despite having valid PVCs",
			"Collation officer observed altering figures on result sheet",
			"Armed men blocked access road to polling unit",
			"BVAS fingerprint reader not recognizing registered voters",
			"Party agents distributing money to voters near polling station",
			"Underage voters observed in queue at polling unit",
			"Electoral materials arrived 3 hours late at polling station",
			"Result sheet figures do not match BVAS accreditation count",
			"Polling unit opened late due to missing INEC officials",
			"Voters with disability denied assistance at polling booth",
		}
		tx.Exec(`INSERT INTO stakeholder_incidents (reporter_id, incident_type, description, severity, latitude, longitude, status, reported_at) VALUES (?,?,?,?,?,?,?,NOW() + CAST(? AS INTERVAL))`,
			repID, incTypes[rng.Intn(len(incTypes))],
			incDescs[rng.Intn(len(incDescs))],
			sev[rng.Intn(len(sev))],
			6.0+rng.Float64()*7, 3.0+rng.Float64()*12,
			stat[rng.Intn(len(stat))],
			fmt.Sprintf("-%d hours", rng.Intn(48)))
	}

	gTypes := []string{"result_dispute", "process_complaint", "staff_misconduct", "access_denial", "equipment_issue", "other"}
	for i := 0; i < 20; i++ {
		sid := rng.Intn(120) + 1
		pri := []string{"low", "normal", "high", "urgent"}
		stat := []string{"filed", "under_review", "hearing_scheduled", "resolved", "dismissed"}
		gSubjects := []string{
			"Disputed result in Ward III collation centre",
			"Complaint about staff misconduct during accreditation",
			"Request for recount at polling unit level",
			"Denial of observer access to collation centre",
			"Allegation of result falsification at LGA level",
			"BVAS malfunction affected voter turnout",
			"Late opening of polls disenfranchised voters",
			"Unauthorized persons present during vote counting",
			"Missing result sheets from two polling units",
			"Party agent removed from polling station without cause",
		}
		gDescriptions := []string{
			"The announced figures differ from what was recorded on the EC8A form at the polling unit level. We request an immediate recount.",
			"An INEC ad-hoc staff was observed directing voters to specific candidates. Multiple witnesses have provided statements.",
			"The BVAS device rejected valid PVCs for over 50 registered voters. Technical support was not available on time.",
			"Our accredited observers were prevented from entering the collation centre by security personnel without explanation.",
			"The figures announced at the LGA collation centre do not match the sum of ward-level results.",
			"Polling unit opened 4 hours late. Many registered voters left and could not return to vote.",
			"Unauthorized individuals were seen handling ballot boxes during transportation to the collation centre.",
			"Two result sheets from polling units in this ward are missing and unaccounted for in the collation.",
			"Our party agent was forcibly removed from the polling station after raising objections about irregularities.",
			"The presiding officer allowed voting to continue past the official closing time without authorization.",
		}
		tx.Exec(`INSERT INTO grievances (stakeholder_id, grievance_type, subject, description, priority, status, filed_at) VALUES (?,?,?,?,?,?,NOW() + CAST(? AS INTERVAL))`,
			sid, gTypes[rng.Intn(len(gTypes))],
			gSubjects[rng.Intn(len(gSubjects))],
			gDescriptions[rng.Intn(len(gDescriptions))],
			pri[rng.Intn(len(pri))], stat[rng.Intn(len(stat))],
			fmt.Sprintf("-%d hours", rng.Intn(72)))
	}

	notifs := []struct{ ttype, tval, title, body, ntype string }{
		{"all", "", "Voting Commences", "Polls are now open nationwide", "alert"},
		{"stakeholder_type", "observer", "Observer Briefing", "Pre-election briefing at 7:00 AM", "info"},
		{"stakeholder_type", "party_agent", "Agent Credentials", "Collect your credentials at RAC offices", "update"},
		{"all", "", "Security Advisory", "Report suspicious activities to security personnel", "emergency"},
		{"stakeholder_type", "media", "Media Guidelines", "Updated media access guidelines published", "info"},
	}
	for _, n := range notifs {
		tx.Exec(`INSERT INTO push_notifications (target_type, target_value, title, body, notification_type, total_recipients, read_count) VALUES (?,?,?,?,?,?,?)`,
			n.ttype, n.tval, n.title, n.body, n.ntype, 50+rng.Intn(200), rng.Intn(100))
	}

	states := []string{"LA", "FC", "KN", "RI", "OY", "KD", "AB", "AN", "BO", "EN", "OG", "ED", "BA", "SO", "NI"}
	predTypes := []string{"turnout", "resource", "security_threat", "sentiment"}
	for _, st := range states {
		for _, pt := range predTypes {
			val := rng.Float64() * 100
			conf := 0.6 + rng.Float64()*0.4
			tx.Exec(`INSERT INTO ai_predictions (prediction_type, target_area, target_level, predicted_value, confidence, model_name, election_id) VALUES (?,?,?,?,?,?,?)`,
				pt, st, "state", val, conf, "xgboost_v2", electionID)
		}
	}

	sentiments := []string{"positive", "negative", "neutral", "mixed"}
	sources := []string{"twitter", "facebook", "news", "whatsapp"}
	topics := []string{"election security", "BVAS performance", "voter turnout", "result credibility", "INEC preparedness"}
	for i := 0; i < 200; i++ {
		sentimentSnippets := []string{
			"BVAS working perfectly at my polling unit. Smooth process so far #NigeriaDecides",
			"Why is INEC always late with materials? This is unacceptable! #ElectionDay",
			"Kudos to INEC staff for maintaining order at our polling station",
			"Voter turnout looking impressive in Lagos today. Democracy is alive!",
			"Concerned about security situation in some northern states #NigeriaElection",
			"The new electronic voting process is much better than before. Well done INEC",
			"Still waiting in queue after 4 hours. When will they start accreditation?",
			"Peaceful voting in my area. No incidents reported. #NigeriaDecides2027",
			"BVAS rejected my fingerprint twice before it worked. Stressful experience",
			"International observers impressed with the level of transparency this election",
			"Why are party agents being denied access to the collation centre?",
			"The result from my polling unit matches what I saw. Transparent process",
			"Reports of thugs causing chaos at several polling units in Rivers State",
			"Congratulations to all Nigerians who came out to vote. Democracy wins!",
			"INEC should explain why results are delayed in these key states",
		}
		tx.Exec(`INSERT INTO sentiment_analysis (source, content_snippet, sentiment, score, topics, location, election_id, analyzed_at) VALUES (?,?,?,?,?,?,?,NOW() + CAST(? AS INTERVAL))`,
			sources[rng.Intn(len(sources))],
			sentimentSnippets[rng.Intn(len(sentimentSnippets))],
			sentiments[rng.Intn(len(sentiments))],
			-1+rng.Float64()*2,
			topics[rng.Intn(len(topics))],
			states[rng.Intn(len(states))],
			electionID,
			fmt.Sprintf("-%d hours", rng.Intn(48)))
	}

	for i := 0; i < 12; i++ {
		classif := []string{"fake_result", "false_claim", "manipulated_media", "impersonation", "incitement"}
		sev := []string{"low", "medium", "high", "critical"}
		stat := []string{"detected", "verified", "debunked", "monitoring"}
		misinfoContent := []string{
			"Viral post claims INEC server was hacked and results altered in favor of ruling party",
			"Fabricated screenshot of INEC chairman endorsing a candidate shared on WhatsApp",
			"False claim that election has been postponed in 5 northern states",
			"Doctored video showing ballot stuffing claimed to be from Lagos polling unit",
			"Fake news claiming BVAS machines have been programmed to reject opposition voters",
			"Manipulated image showing military preventing voters in South East",
			"False report claiming electoral commissioner fled the country",
			"Fabricated result sheets circulating on social media claiming premature results",
			"Deepfake video of candidate making inflammatory statement during voting",
			"False claim that international observers declared the election invalid",
			"Manipulated audio of INEC official discussing vote rigging plan",
			"Fake social media account impersonating REC posting false state results",
		}
		tx.Exec(`INSERT INTO misinformation_alerts (content, source_platform, classification, confidence, severity, reach_estimate, status, fact_check) VALUES (?,?,?,?,?,?,?,?)`,
			misinfoContent[rng.Intn(len(misinfoContent))],
			sources[rng.Intn(len(sources))],
			classif[rng.Intn(len(classif))],
			0.6+rng.Float64()*0.4,
			sev[rng.Intn(len(sev))],
			rng.Intn(50000),
			stat[rng.Intn(len(stat))],
			fmt.Sprintf("Fact check: claim #%d is %s", i+1, []string{"false", "misleading", "out of context"}[rng.Intn(3)]))
	}

	threatTypes := []string{"violence", "protest", "road_blockage", "device_theft", "cyber_attack"}
	for i := 0; i < 18; i++ {
		sev := []string{"low", "medium", "high", "critical"}
		stat := []string{"active", "monitoring", "mitigated", "resolved"}
		tx.Exec(`INSERT INTO security_threats (threat_type, location, latitude, longitude, severity, confidence, affected_pus, status, description) VALUES (?,?,?,?,?,?,?,?,?)`,
			threatTypes[rng.Intn(len(threatTypes))],
			states[rng.Intn(len(states))],
			6.0+rng.Float64()*7, 3.0+rng.Float64()*12,
			sev[rng.Intn(len(sev))],
			0.5+rng.Float64()*0.5,
			rng.Intn(20),
			stat[rng.Intn(len(stat))],
			fmt.Sprintf("Security threat assessment #%d", i+1))
	}

	cvEvents := []string{"crowd_size", "queue_length", "suspicious_activity", "equipment_status"}
	for i := 0; i < 30; i++ {
		tx.Exec(`INSERT INTO cv_monitoring (camera_id, event_type, value, description, confidence, detected_at) VALUES (?,?,?,?,?,NOW() + CAST(? AS INTERVAL))`,
			fmt.Sprintf("CAM-%03d", rng.Intn(100)+1),
			cvEvents[rng.Intn(len(cvEvents))],
			rng.Float64()*100,
			fmt.Sprintf("CV detection event #%d", i+1),
			0.7+rng.Float64()*0.3,
			fmt.Sprintf("-%d minutes", rng.Intn(360)))
	}

	tx.Commit()
}

// ══════════════════════════════════════════════════════════════
// Module 1: Enhanced Biometric Verification System
// ══════════════════════════════════════════════════════════════

func handleBiometricStats(w http.ResponseWriter, r *http.Request) {
	var total, enrolled, multiModal, duplicates, spoofs int
	var avgQuality float64
	db.QueryRow("SELECT COUNT(*) FROM biometric_profiles").Scan(&total)
	db.QueryRow("SELECT COUNT(*) FROM biometric_profiles WHERE status='active'").Scan(&enrolled)
	db.QueryRow("SELECT COUNT(*) FROM biometric_profiles WHERE modalities_enrolled LIKE '%iris%'").Scan(&multiModal)
	db.QueryRow("SELECT COUNT(*) FROM biometric_profiles WHERE duplicate_flag=1").Scan(&duplicates)
	db.QueryRow("SELECT AVG(quality_score) FROM biometric_profiles").Scan(&avgQuality)
	db.QueryRow("SELECT COUNT(*) FROM biometric_verifications WHERE result='spoof_detected'").Scan(&spoofs)

	var totalVerif, matches, noMatches int
	var avgLatency float64
	db.QueryRow("SELECT COUNT(*) FROM biometric_verifications").Scan(&totalVerif)
	db.QueryRow("SELECT COUNT(*) FROM biometric_verifications WHERE result='match'").Scan(&matches)
	db.QueryRow("SELECT COUNT(*) FROM biometric_verifications WHERE result='no_match'").Scan(&noMatches)
	db.QueryRow("SELECT COALESCE(AVG(latency_ms),0) FROM biometric_verifications").Scan(&avgLatency)

	byModality := []M{}
	rows, _ := db.Query("SELECT modality, COUNT(*), AVG(match_score) FROM biometric_verifications GROUP BY modality")
	for rows.Next() {
		var mod string
		var cnt int
		var avg float64
		rows.Scan(&mod, &cnt, &avg)
		byModality = append(byModality, M{"modality": mod, "count": cnt, "avg_score": avg})
	}
	rows.Close()

	writeJSON(w, 200, M{
		"total_profiles": total, "enrolled_active": enrolled, "multi_modal": multiModal,
		"duplicates_flagged": duplicates, "spoof_detections": spoofs,
		"avg_quality": avgQuality, "total_verifications": totalVerif,
		"matches": matches, "no_matches": noMatches, "avg_latency_ms": avgLatency,
		"match_rate":  safePercent(matches, totalVerif),
		"by_modality": byModality,
	})
}

func handleBiometricVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VIN      string `json:"vin"`
		Modality string `json:"modality"`
		Template string `json:"template"`
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.VIN == "" || req.Modality == "" {
		writeError(w, 400, "vin and modality required")
		return
	}

	var profile struct{ fpHash, faceHash, irisHash string }
	err := db.QueryRow("SELECT fingerprint_hash, facial_hash, COALESCE(iris_hash,'') FROM biometric_profiles WHERE voter_vin=?", req.VIN).Scan(&profile.fpHash, &profile.faceHash, &profile.irisHash)
	if err != nil {
		writeError(w, 404, "biometric profile not found")
		return
	}

	start := time.Now()

	// Use the ABIS engine for real template-based verification when vault has data
	probeData, vaultErr := biometricVault.RetrieveTemplate(req.VIN, req.Modality)
	var score float64
	var result, algo string
	if vaultErr == nil && len(probeData) > 0 {
		matchResult := abisEngine.Verify(req.VIN, req.Modality, probeData)
		score = matchResult.Score
		result = matchResult.Decision
		algo = matchResult.Algorithm
	} else {
		// Deterministic hash-based comparison from stored profile hashes
		var storedHash string
		switch req.Modality {
		case "fingerprint":
			storedHash = profile.fpHash
		case "facial":
			storedHash = profile.faceHash
		case "iris":
			storedHash = profile.irisHash
		}
		probeHash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.VIN+req.Modality+req.Template)))
		score = hashSimilarity(storedHash, probeHash)
		algo = "hash_comparison"
		if score >= 0.85 {
			result = "match"
		} else {
			result = "no_match"
		}
	}

	latency := int(time.Since(start).Milliseconds())
	if latency < 1 {
		latency = 1
	}

	dbExecLog("biometric_verificati", `INSERT INTO biometric_verifications (voter_vin, device_id, modality, match_score, result, latency_ms) VALUES (?,?,?,?,?,?)`,
		req.VIN, req.DeviceID, req.Modality, score, result, latency)
	dbExecLog("biometric_profiles", `UPDATE biometric_profiles SET last_verified_at=CURRENT_TIMESTAMP, match_count=match_count+1 WHERE voter_vin=?`, req.VIN)

	writeJSON(w, 200, M{
		"vin": req.VIN, "modality": req.Modality, "match_score": score,
		"result": result, "latency_ms": latency, "threshold": 0.85,
		"algorithm": algo,
	})
}

// hashSimilarity computes deterministic similarity between two hex hash strings
func hashSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1.0
	}
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	matching := 0
	for i := 0; i < minLen; i++ {
		if a[i] == b[i] {
			matching++
		}
	}
	return float64(matching) / float64(minLen)
}

func handleABISDuplicates(w http.ResponseWriter, r *http.Request) {
	status := queryParam(r, "status", "")
	q := "SELECT id, source_vin, candidate_vin, similarity_score, modality, status, created_at FROM abis_duplicate_checks"
	args := []interface{}{}
	if status != "" {
		q += " WHERE status=?"
		args = append(args, status)
	}
	q += " ORDER BY created_at DESC LIMIT 50"
	rows, _ := db.Query(q, args...)
	defer rows.Close()

	checks := []M{}
	for rows.Next() {
		var id int
		var src, cand, mod, st string
		var sim float64
		var created string
		rows.Scan(&id, &src, &cand, &sim, &mod, &st, &created)
		checks = append(checks, M{"id": id, "source_vin": src, "candidate_vin": cand, "similarity_score": sim, "modality": mod, "status": st, "created_at": created})
	}

	var pending, confirmed, falsePos int
	db.QueryRow("SELECT COUNT(*) FROM abis_duplicate_checks WHERE status='pending'").Scan(&pending)
	db.QueryRow("SELECT COUNT(*) FROM abis_duplicate_checks WHERE status='confirmed_duplicate'").Scan(&confirmed)
	db.QueryRow("SELECT COUNT(*) FROM abis_duplicate_checks WHERE status='false_positive'").Scan(&falsePos)

	writeJSON(w, 200, M{"checks": checks, "pending": pending, "confirmed": confirmed, "false_positives": falsePos})
}

func handleABISResolve(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Status == "" {
		writeError(w, 400, "status required")
		return
	}
	dbExecLog("abis_duplicate_check", "UPDATE abis_duplicate_checks SET status=?, reviewed_at=CURRENT_TIMESTAMP WHERE id=?", req.Status, id)
	writeJSON(w, 200, M{"status": "updated", "id": id, "new_status": req.Status})
}

func handleBiometricProfiles(w http.ResponseWriter, r *http.Request) {
	limit := queryParamInt(r, "limit", 50)
	offset := queryParamInt(r, "offset", 0)
	rows, _ := db.Query("SELECT voter_vin, modalities_enrolled, quality_score, duplicate_flag, status, enrollment_date, last_verified_at, match_count FROM biometric_profiles ORDER BY enrollment_date DESC LIMIT ? OFFSET ?", limit, offset)
	defer rows.Close()

	profiles := []M{}
	for rows.Next() {
		var vin, mods, status, enrolled string
		var quality float64
		var dup, matchCt int
		var lastVer sql.NullString
		rows.Scan(&vin, &mods, &quality, &dup, &status, &enrolled, &lastVer, &matchCt)
		profiles = append(profiles, M{"voter_vin": vin, "modalities": mods, "quality_score": quality, "duplicate_flag": dup > 0, "status": status, "enrolled": enrolled, "last_verified": lastVer.String, "match_count": matchCt})
	}
	var total int
	db.QueryRow("SELECT COUNT(*) FROM biometric_profiles").Scan(&total)
	writeJSON(w, 200, M{"profiles": profiles, "total": total})
}

// ══════════════════════════════════════════════════════════════
// Module 2: Blockchain-Enhanced Result Transmission
// ══════════════════════════════════════════════════════════════

func handleBlockchainStats(w http.ResponseWriter, r *http.Request) {
	var totalBlocks, validated, pending, disputed int
	db.QueryRow("SELECT COUNT(*) FROM blockchain_results").Scan(&totalBlocks)
	db.QueryRow("SELECT COUNT(*) FROM blockchain_results WHERE validation_status='validated'").Scan(&validated)
	db.QueryRow("SELECT COUNT(*) FROM blockchain_results WHERE validation_status='pending'").Scan(&pending)
	db.QueryRow("SELECT COUNT(*) FROM blockchain_results WHERE validation_status='disputed'").Scan(&disputed)

	var totalContracts, activeContracts, executedContracts int
	db.QueryRow("SELECT COUNT(*) FROM smart_contracts").Scan(&totalContracts)
	db.QueryRow("SELECT COUNT(*) FROM smart_contracts WHERE status='active'").Scan(&activeContracts)
	db.QueryRow("SELECT COUNT(*) FROM smart_contracts WHERE status='executed'").Scan(&executedContracts)

	var auditEntries int
	db.QueryRow("SELECT COUNT(*) FROM blockchain_audit_trail").Scan(&auditEntries)

	byLevel := []M{}
	rows, _ := db.Query("SELECT level, COUNT(*), SUM(CASE WHEN validation_status='validated' THEN 1 ELSE 0 END) FROM blockchain_results GROUP BY level")
	for rows.Next() {
		var level string
		var cnt, val int
		rows.Scan(&level, &cnt, &val)
		byLevel = append(byLevel, M{"level": level, "total": cnt, "validated": val})
	}
	rows.Close()

	writeJSON(w, 200, M{
		"total_blocks": totalBlocks, "validated": validated, "pending": pending,
		"disputed": disputed, "integrity_rate": safePercent(validated, totalBlocks),
		"smart_contracts": M{"total": totalContracts, "active": activeContracts, "executed": executedContracts},
		"audit_entries":   auditEntries, "by_level": byLevel,
	})
}

func handleBlockchainChain(w http.ResponseWriter, r *http.Request) {
	limit := queryParamInt(r, "limit", 50)
	rows, _ := db.Query("SELECT id, result_id, ec8a_hash, prev_hash, block_index, block_hash, merkle_root, level, validation_status, validator_count, timestamp FROM blockchain_results ORDER BY block_index DESC LIMIT ?", limit)
	defer rows.Close()

	blocks := []M{}
	for rows.Next() {
		var id, resultID, blockIdx, nValidators int
		var ec8a, prev, blockHash, merkle, level, status, ts string
		rows.Scan(&id, &resultID, &ec8a, &prev, &blockIdx, &blockHash, &merkle, &level, &status, &nValidators, &ts)
		blocks = append(blocks, M{
			"id": id, "result_id": resultID, "ec8a_hash": ec8a, "prev_hash": prev[:16] + "...",
			"block_index": blockIdx, "block_hash": blockHash[:16] + "...", "merkle_root": merkle,
			"level": level, "validation_status": status, "validators": nValidators, "timestamp": ts,
		})
	}
	writeJSON(w, 200, M{"blocks": blocks})
}

func handleSmartContracts(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT id, contract_id, contract_type, level, area_code, election_id, conditions, status, executed_at, created_at FROM smart_contracts ORDER BY created_at DESC")
	defer rows.Close()

	contracts := []M{}
	for rows.Next() {
		var id, eid int
		var cid, ctype, level, area, conds, status, created string
		var execAt sql.NullString
		rows.Scan(&id, &cid, &ctype, &level, &area, &eid, &conds, &status, &execAt, &created)
		contracts = append(contracts, M{
			"id": id, "contract_id": cid, "type": ctype, "level": level,
			"area_code": area, "election_id": eid, "conditions": conds,
			"status": status, "executed_at": execAt.String, "created_at": created,
		})
	}
	writeJSON(w, 200, M{"contracts": contracts})
}

func handleBlockchainVerifyResult(w http.ResponseWriter, r *http.Request) {
	resultID := mux.Vars(r)["result_id"]
	var br struct {
		ec8a, prev, blockHash, merkle, level, status string
		idx, validators                              int
	}
	err := db.QueryRow("SELECT ec8a_hash, prev_hash, block_hash, merkle_root, level, validation_status, block_index, validator_count FROM blockchain_results WHERE result_id=?", resultID).Scan(
		&br.ec8a, &br.prev, &br.blockHash, &br.merkle, &br.level, &br.status, &br.idx, &br.validators)
	if err != nil {
		writeError(w, 404, "blockchain record not found for this result")
		return
	}

	recomputed := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d-%s-%s", br.idx, br.prev, br.ec8a))))
	valid := recomputed == br.blockHash

	writeJSON(w, 200, M{
		"result_id": resultID, "block_index": br.idx, "ec8a_hash": br.ec8a,
		"block_hash": br.blockHash, "recomputed_hash": recomputed,
		"hash_valid": valid, "level": br.level, "validation_status": br.status,
		"validators": br.validators, "integrity": valid,
	})
}

func handleBlockchainAuditTrail(w http.ResponseWriter, r *http.Request) {
	limit := queryParamInt(r, "limit", 50)
	rows, _ := db.Query("SELECT id, action, entity_type, entity_id, actor, tx_hash, timestamp FROM blockchain_audit_trail ORDER BY timestamp DESC LIMIT ?", limit)
	defer rows.Close()

	entries := []M{}
	for rows.Next() {
		var id int
		var action, etype, eid, actor, txHash, ts string
		rows.Scan(&id, &action, &etype, &eid, &actor, &txHash, &ts)
		entries = append(entries, M{"id": id, "action": action, "entity_type": etype, "entity_id": eid, "actor": actor, "tx_hash": txHash[:16] + "...", "timestamp": ts})
	}
	writeJSON(w, 200, M{"entries": entries})
}

// ══════════════════════════════════════════════════════════════
// Module 3: Training & Capacity Building Platform
// ══════════════════════════════════════════════════════════════

func handleTrainingCourses(w http.ResponseWriter, r *http.Request) {
	role := queryParam(r, "role", "")
	q := "SELECT id, title, description, course_type, target_role, difficulty, duration_minutes, passing_score, modules_count, is_mandatory, is_active FROM training_courses"
	args := []interface{}{}
	if role != "" {
		q += " WHERE target_role=? OR target_role='all'"
		args = append(args, role)
	}
	q += " ORDER BY is_mandatory DESC, title"
	rows, _ := db.Query(q, args...)
	defer rows.Close()

	courses := []M{}
	for rows.Next() {
		var id, dur, pass, mods, mand, active int
		var title, desc, ctype, trole, diff string
		rows.Scan(&id, &title, &desc, &ctype, &trole, &diff, &dur, &pass, &mods, &mand, &active)

		var enrolled, completed, avgScore int
		db.QueryRow("SELECT COUNT(*) FROM training_enrollments WHERE course_id=?", id).Scan(&enrolled)
		db.QueryRow("SELECT COUNT(*) FROM training_enrollments WHERE course_id=? AND status='completed'", id).Scan(&completed)
		db.QueryRow("SELECT COALESCE(AVG(score),0) FROM training_enrollments WHERE course_id=? AND status='completed'", id).Scan(&avgScore)

		courses = append(courses, M{
			"id": id, "title": title, "description": desc, "course_type": ctype,
			"target_role": trole, "difficulty": diff, "duration_minutes": dur,
			"passing_score": pass, "modules_count": mods, "is_mandatory": mand > 0,
			"is_active": active > 0, "enrolled": enrolled, "completed": completed, "avg_score": avgScore,
		})
	}
	writeJSON(w, 200, M{"courses": courses})
}

func handleTrainingStats(w http.ResponseWriter, r *http.Request) {
	var totalCourses, totalEnrollments, completed, failed, inProgress int
	var avgScore float64
	db.QueryRow("SELECT COUNT(*) FROM training_courses WHERE is_active=1").Scan(&totalCourses)
	db.QueryRow("SELECT COUNT(*) FROM training_enrollments").Scan(&totalEnrollments)
	db.QueryRow("SELECT COUNT(*) FROM training_enrollments WHERE status='completed'").Scan(&completed)
	db.QueryRow("SELECT COUNT(*) FROM training_enrollments WHERE status='failed'").Scan(&failed)
	db.QueryRow("SELECT COUNT(*) FROM training_enrollments WHERE status='in_progress'").Scan(&inProgress)
	db.QueryRow("SELECT COALESCE(AVG(score),0) FROM training_enrollments WHERE status='completed'").Scan(&avgScore)

	var totalCerts int
	db.QueryRow("SELECT COUNT(*) FROM training_certificates").Scan(&totalCerts)

	var vrScenarios int
	db.QueryRow("SELECT COUNT(*) FROM training_vr_scenarios WHERE is_active=1").Scan(&vrScenarios)

	byType := []M{}
	rows, _ := db.Query("SELECT course_type, COUNT(*) FROM training_courses GROUP BY course_type")
	for rows.Next() {
		var ct string
		var cnt int
		rows.Scan(&ct, &cnt)
		byType = append(byType, M{"type": ct, "count": cnt})
	}
	rows.Close()

	writeJSON(w, 200, M{
		"total_courses": totalCourses, "total_enrollments": totalEnrollments,
		"completed": completed, "failed": failed, "in_progress": inProgress,
		"completion_rate": safePercent(completed, totalEnrollments),
		"avg_score":       avgScore, "certificates_issued": totalCerts,
		"vr_scenarios": vrScenarios, "by_type": byType,
	})
}

func handleTrainingEnrollments(w http.ResponseWriter, r *http.Request) {
	courseID := queryParam(r, "course_id", "")
	q := `SELECT e.id, e.user_id, e.course_id, c.title, e.progress_percent, e.score, e.status, e.enrolled_at
		FROM training_enrollments e JOIN training_courses c ON c.id=e.course_id`
	args := []interface{}{}
	if courseID != "" {
		q += " WHERE e.course_id=?"
		args = append(args, courseID)
	}
	q += " ORDER BY e.enrolled_at DESC LIMIT 100"
	rows, _ := db.Query(q, args...)
	defer rows.Close()

	enrollments := []M{}
	for rows.Next() {
		var id, uid, cid int
		var title, status, enrolled string
		var progress float64
		var score sql.NullInt64
		rows.Scan(&id, &uid, &cid, &title, &progress, &score, &status, &enrolled)
		enrollments = append(enrollments, M{
			"id": id, "user_id": uid, "course_id": cid, "course_title": title,
			"progress": progress, "score": score.Int64, "status": status, "enrolled_at": enrolled,
		})
	}
	writeJSON(w, 200, M{"enrollments": enrollments})
}

func handleTrainingCertificates(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query(`SELECT tc.id, tc.certificate_id, tc.user_id, tc.course_id, c.title, tc.blockchain_hash, tc.score, tc.issued_at
		FROM training_certificates tc JOIN training_courses c ON c.id=tc.course_id ORDER BY tc.issued_at DESC LIMIT 100`)
	defer rows.Close()

	certs := []M{}
	for rows.Next() {
		var id, uid, cid, score int
		var certID, title, bhash, issued string
		rows.Scan(&id, &certID, &uid, &cid, &title, &bhash, &score, &issued)
		certs = append(certs, M{
			"id": id, "certificate_id": certID, "user_id": uid, "course_id": cid,
			"course_title": title, "blockchain_hash": bhash[:16] + "...", "score": score, "issued_at": issued,
		})
	}
	writeJSON(w, 200, M{"certificates": certs})
}

func handleVRScenarios(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query(`SELECT vs.id, vs.course_id, c.title, vs.scenario_name, vs.scenario_type, vs.max_score, vs.avg_completion_time, vs.difficulty
		FROM training_vr_scenarios vs JOIN training_courses c ON c.id=vs.course_id WHERE vs.is_active=1`)
	defer rows.Close()

	scenarios := []M{}
	for rows.Next() {
		var id, cid, maxScore, avgTime int
		var ctitle, name, stype, diff string
		rows.Scan(&id, &cid, &ctitle, &name, &stype, &maxScore, &avgTime, &diff)
		scenarios = append(scenarios, M{
			"id": id, "course_id": cid, "course_title": ctitle, "name": name,
			"type": stype, "max_score": maxScore, "avg_completion_minutes": avgTime, "difficulty": diff,
		})
	}
	writeJSON(w, 200, M{"scenarios": scenarios})
}

// ══════════════════════════════════════════════════════════════
// Module 4: Electoral Stakeholder Engagement System
// ══════════════════════════════════════════════════════════════

func handleStakeholderStats(w http.ResponseWriter, r *http.Request) {
	var total, approved, pending, suspended int
	db.QueryRow("SELECT COUNT(*) FROM stakeholders").Scan(&total)
	db.QueryRow("SELECT COUNT(*) FROM stakeholders WHERE accreditation_status='approved'").Scan(&approved)
	db.QueryRow("SELECT COUNT(*) FROM stakeholders WHERE accreditation_status='pending'").Scan(&pending)
	db.QueryRow("SELECT COUNT(*) FROM stakeholders WHERE accreditation_status='suspended'").Scan(&suspended)

	byType := []M{}
	rows, _ := db.Query("SELECT stakeholder_type, COUNT(*) FROM stakeholders GROUP BY stakeholder_type ORDER BY COUNT(*) DESC")
	for rows.Next() {
		var st string
		var cnt int
		rows.Scan(&st, &cnt)
		byType = append(byType, M{"type": st, "count": cnt})
	}
	rows.Close()

	var totalIncidents, resolved, critical int
	db.QueryRow("SELECT COUNT(*) FROM stakeholder_incidents").Scan(&totalIncidents)
	db.QueryRow("SELECT COUNT(*) FROM stakeholder_incidents WHERE status='resolved'").Scan(&resolved)
	db.QueryRow("SELECT COUNT(*) FROM stakeholder_incidents WHERE severity='critical'").Scan(&critical)

	var totalGrievances, gResolved int
	db.QueryRow("SELECT COUNT(*) FROM grievances").Scan(&totalGrievances)
	db.QueryRow("SELECT COUNT(*) FROM grievances WHERE status='resolved'").Scan(&gResolved)

	writeJSON(w, 200, M{
		"total_stakeholders": total, "approved": approved, "pending": pending, "suspended": suspended,
		"by_type":    byType,
		"incidents":  M{"total": totalIncidents, "resolved": resolved, "critical": critical},
		"grievances": M{"total": totalGrievances, "resolved": gResolved},
	})
}

func handleListStakeholders(w http.ResponseWriter, r *http.Request) {
	sType := queryParam(r, "type", "")
	status := queryParam(r, "status", "")
	limit := queryParamInt(r, "limit", 50)

	q := "SELECT id, name, organization, stakeholder_type, credential_id, credential_qr, accreditation_status, registered_at FROM stakeholders WHERE 1=1"
	args := []interface{}{}
	if sType != "" {
		q += " AND stakeholder_type=?"
		args = append(args, sType)
	}
	if status != "" {
		q += " AND accreditation_status=?"
		args = append(args, status)
	}
	q += " ORDER BY registered_at DESC LIMIT ?"
	args = append(args, limit)

	rows, _ := db.Query(q, args...)
	defer rows.Close()

	stakeholders := []M{}
	for rows.Next() {
		var id int
		var name, org, stype, credID, qr, accStatus, regAt string
		rows.Scan(&id, &name, &org, &stype, &credID, &qr, &accStatus, &regAt)
		stakeholders = append(stakeholders, M{
			"id": id, "name": name, "organization": org, "type": stype,
			"credential_id": credID, "credential_qr": qr,
			"accreditation_status": accStatus, "registered_at": regAt,
		})
	}
	writeJSON(w, 200, M{"stakeholders": stakeholders})
}

func handleStakeholderIncidents(w http.ResponseWriter, r *http.Request) {
	severity := queryParam(r, "severity", "")
	status := queryParam(r, "status", "")
	q := `SELECT si.id, s.name, si.incident_type, si.description, si.severity, si.latitude, si.longitude, si.status, si.reported_at
		FROM stakeholder_incidents si JOIN stakeholders s ON s.id=si.reporter_id WHERE 1=1`
	args := []interface{}{}
	if severity != "" {
		q += " AND si.severity=?"
		args = append(args, severity)
	}
	if status != "" {
		q += " AND si.status=?"
		args = append(args, status)
	}
	q += " ORDER BY si.reported_at DESC LIMIT 100"
	rows, _ := db.Query(q, args...)
	defer rows.Close()

	incidents := []M{}
	for rows.Next() {
		var id int
		var name, itype, desc, sev, stat, reported string
		var lat, lng float64
		rows.Scan(&id, &name, &itype, &desc, &sev, &lat, &lng, &stat, &reported)
		incidents = append(incidents, M{
			"id": id, "reporter": name, "type": itype, "description": desc,
			"severity": sev, "latitude": lat, "longitude": lng,
			"status": stat, "reported_at": reported,
		})
	}
	writeJSON(w, 200, M{"incidents": incidents})
}

func handleListGrievances(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query(`SELECT g.id, s.name, g.grievance_type, g.subject, g.priority, g.status, g.filed_at
		FROM grievances g JOIN stakeholders s ON s.id=g.stakeholder_id ORDER BY g.filed_at DESC LIMIT 50`)
	defer rows.Close()

	grievances := []M{}
	for rows.Next() {
		var id int
		var name, gtype, subject, priority, status, filed string
		rows.Scan(&id, &name, &gtype, &subject, &priority, &status, &filed)
		grievances = append(grievances, M{
			"id": id, "stakeholder": name, "type": gtype, "subject": subject,
			"priority": priority, "status": status, "filed_at": filed,
		})
	}
	writeJSON(w, 200, M{"grievances": grievances})
}

func handlePushNotifications(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT id, target_type, target_value, title, body, notification_type, sent_at, read_count, total_recipients FROM push_notifications ORDER BY sent_at DESC LIMIT 50")
	defer rows.Close()

	notifs := []M{}
	for rows.Next() {
		var id, readCt, totalRecip int
		var ttype, tval, title, body, ntype, sent string
		rows.Scan(&id, &ttype, &tval, &title, &body, &ntype, &sent, &readCt, &totalRecip)
		notifs = append(notifs, M{
			"id": id, "target_type": ttype, "target_value": tval, "title": title,
			"body": body, "type": ntype, "sent_at": sent, "read_count": readCt, "total_recipients": totalRecip,
		})
	}
	writeJSON(w, 200, M{"notifications": notifs})
}

func handleSendNotification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetType  string `json:"target_type"`
		TargetValue string `json:"target_value"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		Type        string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Title == "" || req.Body == "" {
		writeError(w, 400, "title and body required")
		return
	}
	if req.Type == "" {
		req.Type = "info"
	}
	if req.TargetType == "" {
		req.TargetType = "all"
	}

	var recipients int
	switch req.TargetType {
	case "all":
		db.QueryRow("SELECT COUNT(*) FROM stakeholders WHERE accreditation_status='approved'").Scan(&recipients)
	case "stakeholder_type":
		db.QueryRow("SELECT COUNT(*) FROM stakeholders WHERE stakeholder_type=? AND accreditation_status='approved'", req.TargetValue).Scan(&recipients)
	default:
		recipients = 1
	}

	dbExecLog("push_notifications", `INSERT INTO push_notifications (target_type, target_value, title, body, notification_type, total_recipients) VALUES (?,?,?,?,?,?)`,
		req.TargetType, req.TargetValue, req.Title, req.Body, req.Type, recipients)
	writeJSON(w, 201, M{"status": "sent", "recipients": recipients})
}

// ══════════════════════════════════════════════════════════════
// Module 5: AI-Powered Election Monitoring & Analytics
// ══════════════════════════════════════════════════════════════

func handleAIMonitoringDashboard(w http.ResponseWriter, r *http.Request) {
	var totalPredictions, turnoutPreds, securityPreds int
	db.QueryRow("SELECT COUNT(*) FROM ai_predictions").Scan(&totalPredictions)
	db.QueryRow("SELECT COUNT(*) FROM ai_predictions WHERE prediction_type='turnout'").Scan(&turnoutPreds)
	db.QueryRow("SELECT COUNT(*) FROM ai_predictions WHERE prediction_type='security_threat'").Scan(&securityPreds)

	var totalSentiment, positive, negative int
	db.QueryRow("SELECT COUNT(*) FROM sentiment_analysis").Scan(&totalSentiment)
	db.QueryRow("SELECT COUNT(*) FROM sentiment_analysis WHERE sentiment='positive'").Scan(&positive)
	db.QueryRow("SELECT COUNT(*) FROM sentiment_analysis WHERE sentiment='negative'").Scan(&negative)

	var totalMisinfo, detected, debunked int
	db.QueryRow("SELECT COUNT(*) FROM misinformation_alerts").Scan(&totalMisinfo)
	db.QueryRow("SELECT COUNT(*) FROM misinformation_alerts WHERE status='detected'").Scan(&detected)
	db.QueryRow("SELECT COUNT(*) FROM misinformation_alerts WHERE status='debunked'").Scan(&debunked)

	var totalThreats, activeThreats, criticalThreats int
	db.QueryRow("SELECT COUNT(*) FROM security_threats").Scan(&totalThreats)
	db.QueryRow("SELECT COUNT(*) FROM security_threats WHERE status='active'").Scan(&activeThreats)
	db.QueryRow("SELECT COUNT(*) FROM security_threats WHERE severity='critical'").Scan(&criticalThreats)

	var cvEvents int
	db.QueryRow("SELECT COUNT(*) FROM cv_monitoring").Scan(&cvEvents)

	writeJSON(w, 200, M{
		"predictions": M{"total": totalPredictions, "turnout": turnoutPreds, "security": securityPreds},
		"sentiment": M{"total": totalSentiment, "positive": positive, "negative": negative,
			"positive_rate": safePercent(positive, totalSentiment)},
		"misinformation":   M{"total": totalMisinfo, "detected": detected, "debunked": debunked},
		"security_threats": M{"total": totalThreats, "active": activeThreats, "critical": criticalThreats},
		"cv_monitoring":    M{"total_events": cvEvents},
	})
}

func handleAIPredictions(w http.ResponseWriter, r *http.Request) {
	predType := queryParam(r, "type", "")
	q := "SELECT id, prediction_type, target_area, target_level, predicted_value, confidence, model_name, predicted_at FROM ai_predictions"
	args := []interface{}{}
	if predType != "" {
		q += " WHERE prediction_type=?"
		args = append(args, predType)
	}
	q += " ORDER BY confidence DESC LIMIT 100"
	rows, _ := db.Query(q, args...)
	defer rows.Close()

	preds := []M{}
	for rows.Next() {
		var id int
		var ptype, area, level, model, ts string
		var value, conf float64
		rows.Scan(&id, &ptype, &area, &level, &value, &conf, &model, &ts)
		preds = append(preds, M{
			"id": id, "type": ptype, "target_area": area, "target_level": level,
			"predicted_value": value, "confidence": conf, "model": model, "predicted_at": ts,
		})
	}
	writeJSON(w, 200, M{"predictions": preds})
}

func handleSentimentAnalysis(w http.ResponseWriter, r *http.Request) {
	bySentiment := []M{}
	rows, _ := db.Query("SELECT sentiment, COUNT(*), AVG(score) FROM sentiment_analysis GROUP BY sentiment")
	for rows.Next() {
		var s string
		var cnt int
		var avg float64
		rows.Scan(&s, &cnt, &avg)
		bySentiment = append(bySentiment, M{"sentiment": s, "count": cnt, "avg_score": avg})
	}
	rows.Close()

	bySource := []M{}
	rows2, _ := db.Query("SELECT source, COUNT(*) FROM sentiment_analysis GROUP BY source ORDER BY COUNT(*) DESC")
	for rows2.Next() {
		var src string
		var cnt int
		rows2.Scan(&src, &cnt)
		bySource = append(bySource, M{"source": src, "count": cnt})
	}
	rows2.Close()

	trending := []M{}
	rows3, _ := db.Query("SELECT topics, COUNT(*) as c FROM sentiment_analysis GROUP BY topics ORDER BY c DESC LIMIT 10")
	for rows3.Next() {
		var topic string
		var cnt int
		rows3.Scan(&topic, &cnt)
		trending = append(trending, M{"topic": topic, "mentions": cnt})
	}
	rows3.Close()

	recent := []M{}
	rows4, _ := db.Query("SELECT source, content_snippet, sentiment, score, topics, location, analyzed_at FROM sentiment_analysis ORDER BY analyzed_at DESC LIMIT 20")
	for rows4.Next() {
		var src, content, sent, topics, loc, ts string
		var score float64
		rows4.Scan(&src, &content, &sent, &score, &topics, &loc, &ts)
		recent = append(recent, M{"source": src, "content": content, "sentiment": sent, "score": score, "topics": topics, "location": loc, "analyzed_at": ts})
	}
	rows4.Close()

	writeJSON(w, 200, M{"by_sentiment": bySentiment, "by_source": bySource, "trending_topics": trending, "recent": recent})
}

func handleMisinformationAlerts(w http.ResponseWriter, r *http.Request) {
	status := queryParam(r, "status", "")
	q := "SELECT id, content, source_platform, classification, confidence, severity, reach_estimate, status, fact_check, detected_at FROM misinformation_alerts"
	args := []interface{}{}
	if status != "" {
		q += " WHERE status=?"
		args = append(args, status)
	}
	q += " ORDER BY detected_at DESC"
	rows, _ := db.Query(q, args...)
	defer rows.Close()

	alerts := []M{}
	for rows.Next() {
		var id, reach int
		var content, platform, classif, sev, stat, factCheck, detected string
		var conf float64
		rows.Scan(&id, &content, &platform, &classif, &conf, &sev, &reach, &stat, &factCheck, &detected)
		alerts = append(alerts, M{
			"id": id, "content": content, "platform": platform, "classification": classif,
			"confidence": conf, "severity": sev, "reach_estimate": reach,
			"status": stat, "fact_check": factCheck, "detected_at": detected,
		})
	}

	byClassif := []M{}
	rows2, _ := db.Query("SELECT classification, COUNT(*) FROM misinformation_alerts GROUP BY classification")
	for rows2.Next() {
		var c string
		var cnt int
		rows2.Scan(&c, &cnt)
		byClassif = append(byClassif, M{"classification": c, "count": cnt})
	}
	rows2.Close()

	writeJSON(w, 200, M{"alerts": alerts, "by_classification": byClassif})
}

func handleSecurityThreats(w http.ResponseWriter, r *http.Request) {
	status := queryParam(r, "status", "")
	q := "SELECT id, threat_type, location, latitude, longitude, severity, confidence, affected_pus, status, description, detected_at FROM security_threats"
	args := []interface{}{}
	if status != "" {
		q += " WHERE status=?"
		args = append(args, status)
	}
	q += " ORDER BY detected_at DESC"
	rows, _ := db.Query(q, args...)
	defer rows.Close()

	threats := []M{}
	for rows.Next() {
		var id, affected int
		var ttype, loc, sev, stat, desc, detected string
		var lat, lng, conf float64
		rows.Scan(&id, &ttype, &loc, &lat, &lng, &sev, &conf, &affected, &stat, &desc, &detected)
		threats = append(threats, M{
			"id": id, "type": ttype, "location": loc, "latitude": lat, "longitude": lng,
			"severity": sev, "confidence": conf, "affected_pus": affected,
			"status": stat, "description": desc, "detected_at": detected,
		})
	}

	bySeverity := []M{}
	rows2, _ := db.Query("SELECT severity, COUNT(*) FROM security_threats GROUP BY severity")
	for rows2.Next() {
		var s string
		var cnt int
		rows2.Scan(&s, &cnt)
		bySeverity = append(bySeverity, M{"severity": s, "count": cnt})
	}
	rows2.Close()

	writeJSON(w, 200, M{"threats": threats, "by_severity": bySeverity})
}

func handleCVMonitoring(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT id, camera_id, event_type, value, description, confidence, detected_at FROM cv_monitoring ORDER BY detected_at DESC LIMIT 50")
	defer rows.Close()

	events := []M{}
	for rows.Next() {
		var id int
		var cam, etype, desc, detected string
		var val, conf float64
		rows.Scan(&id, &cam, &etype, &val, &desc, &conf, &detected)
		events = append(events, M{
			"id": id, "camera_id": cam, "event_type": etype, "value": val,
			"description": desc, "confidence": conf, "detected_at": detected,
		})
	}

	byType := []M{}
	rows2, _ := db.Query("SELECT event_type, COUNT(*), AVG(confidence) FROM cv_monitoring GROUP BY event_type")
	for rows2.Next() {
		var et string
		var cnt int
		var avg float64
		rows2.Scan(&et, &cnt, &avg)
		byType = append(byType, M{"type": et, "count": cnt, "avg_confidence": avg})
	}
	rows2.Close()

	writeJSON(w, 200, M{"events": events, "by_type": byType})
}

// ══════════════════════════════════════════════════════════════
// WRITE/CREATE APIs for AI Monitoring Features
// ══════════════════════════════════════════════════════════════

func handleCreateAIPrediction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PredictionType string  `json:"prediction_type"`
		TargetArea     string  `json:"target_area"`
		TargetLevel    string  `json:"target_level"`
		PredictedValue float64 `json:"predicted_value"`
		Confidence     float64 `json:"confidence"`
		ModelName      string  `json:"model_name"`
		ElectionID     int     `json:"election_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.PredictionType == "" || req.TargetArea == "" {
		writeError(w, 400, "prediction_type and target_area required")
		return
	}
	validTypes := map[string]bool{"turnout": true, "security_threat": true, "result_anomaly": true, "violence_risk": true, "logistics": true}
	if !validTypes[req.PredictionType] {
		writeError(w, 400, "prediction_type must be one of: turnout, security_threat, result_anomaly, violence_risk, logistics")
		return
	}
	if req.Confidence < 0 || req.Confidence > 1 {
		writeError(w, 400, "confidence must be between 0 and 1")
		return
	}
	if req.ModelName == "" {
		req.ModelName = "xgboost-v1.0"
	}
	if req.ElectionID == 0 {
		req.ElectionID = 1
	}
	id := insertReturningID(db, `INSERT INTO ai_predictions (prediction_type, target_area, target_level, predicted_value, confidence, model_name, election_id) VALUES (?,?,?,?,?,?,?)`,
		req.PredictionType, req.TargetArea, req.TargetLevel, req.PredictedValue, req.Confidence, req.ModelName, req.ElectionID)
	logAudit("AI_PREDICTION_CREATED", "ai_prediction", fmt.Sprintf("%d", id), 0, map[string]interface{}{"type": req.PredictionType, "area": req.TargetArea})
	writeJSON(w, 201, M{"id": id, "message": "Prediction recorded"})
}

func handleCreateSentimentEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source     string  `json:"source"`
		Content    string  `json:"content"`
		Sentiment  string  `json:"sentiment"`
		Score      float64 `json:"score"`
		Topics     string  `json:"topics"`
		Location   string  `json:"location"`
		ElectionID int     `json:"election_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Source == "" || req.Content == "" {
		writeError(w, 400, "source and content required")
		return
	}
	validSentiments := map[string]bool{"positive": true, "negative": true, "neutral": true, "mixed": true}
	if req.Sentiment != "" && !validSentiments[req.Sentiment] {
		writeError(w, 400, "sentiment must be one of: positive, negative, neutral, mixed")
		return
	}
	if req.Sentiment == "" {
		// Auto-classify: simple keyword-based sentiment as baseline
		negWords := []string{"violence", "fraud", "rigged", "stolen", "corrupt", "danger", "threat", "attack"}
		posWords := []string{"peaceful", "transparent", "fair", "successful", "smooth", "organized"}
		lower := strings.ToLower(req.Content)
		negCount, posCount := 0, 0
		for _, w := range negWords {
			if strings.Contains(lower, w) {
				negCount++
			}
		}
		for _, w := range posWords {
			if strings.Contains(lower, w) {
				posCount++
			}
		}
		if negCount > posCount {
			req.Sentiment = "negative"
			req.Score = -0.5 - float64(negCount)*0.1
		} else if posCount > negCount {
			req.Sentiment = "positive"
			req.Score = 0.5 + float64(posCount)*0.1
		} else {
			req.Sentiment = "neutral"
			req.Score = 0.0
		}
	}
	if req.Score < -1 {
		req.Score = -1
	}
	if req.Score > 1 {
		req.Score = 1
	}
	if req.ElectionID == 0 {
		req.ElectionID = 1
	}
	id := insertReturningID(db, `INSERT INTO sentiment_analysis (source, content_snippet, sentiment, score, topics, location, election_id, analyzed_at) VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		req.Source, req.Content, req.Sentiment, req.Score, req.Topics, req.Location, req.ElectionID)
	writeJSON(w, 201, M{"id": id, "sentiment": req.Sentiment, "score": req.Score, "message": "Sentiment recorded"})
}

func handleCreateMisinformationAlert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content        string  `json:"content"`
		SourcePlatform string  `json:"source_platform"`
		Classification string  `json:"classification"`
		Confidence     float64 `json:"confidence"`
		Severity       string  `json:"severity"`
		ReachEstimate  int     `json:"reach_estimate"`
		FactCheck      string  `json:"fact_check"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Content == "" || req.SourcePlatform == "" {
		writeError(w, 400, "content and source_platform required")
		return
	}
	validClassifications := map[string]bool{"fake_result": true, "false_claim": true, "manipulated_media": true, "impersonation": true, "incitement": true, "other": true}
	if req.Classification != "" && !validClassifications[req.Classification] {
		writeError(w, 400, "classification must be one of: fake_result, false_claim, manipulated_media, impersonation, incitement, other")
		return
	}
	if req.Classification == "" {
		req.Classification = "other"
	}
	if req.Severity == "" {
		req.Severity = "medium"
	}
	if req.Confidence == 0 {
		req.Confidence = 0.5
	}
	id := insertReturningID(db, `INSERT INTO misinformation_alerts (content, source_platform, classification, confidence, severity, reach_estimate, status, fact_check) VALUES (?,?,?,?,?,?,?,?)`,
		req.Content, req.SourcePlatform, req.Classification, req.Confidence, req.Severity, req.ReachEstimate, "detected", req.FactCheck)
	logAudit("MISINFO_ALERT_CREATED", "misinformation", fmt.Sprintf("%d", id), 0, map[string]interface{}{"platform": req.SourcePlatform, "classification": req.Classification})
	writeJSON(w, 201, M{"id": id, "status": "detected", "message": "Misinformation alert created"})
}

func handleUpdateMisinformationAlert(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Status    string `json:"status"`
		FactCheck string `json:"fact_check"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	validStatuses := map[string]bool{"detected": true, "investigating": true, "confirmed": true, "debunked": true, "escalated": true}
	if req.Status != "" && !validStatuses[req.Status] {
		writeError(w, 400, "status must be one of: detected, investigating, confirmed, debunked, escalated")
		return
	}
	if req.Status != "" {
		dbExecLog("misinformation_alert", "UPDATE misinformation_alerts SET status=? WHERE id=?", req.Status, id)
	}
	if req.FactCheck != "" {
		dbExecLog("misinformation_alert", "UPDATE misinformation_alerts SET fact_check=? WHERE id=?", req.FactCheck, id)
	}
	logAudit("MISINFO_ALERT_UPDATED", "misinformation", id, 0, map[string]interface{}{"status": req.Status})
	writeJSON(w, 200, M{"id": id, "status": req.Status, "message": "Alert updated"})
}

func handleCreateSecurityThreat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ThreatType  string  `json:"threat_type"`
		Location    string  `json:"location"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Severity    string  `json:"severity"`
		Confidence  float64 `json:"confidence"`
		AffectedPUs int     `json:"affected_pus"`
		Description string  `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.ThreatType == "" || req.Location == "" {
		writeError(w, 400, "threat_type and location required")
		return
	}
	if req.Severity == "" {
		req.Severity = "medium"
	}
	if req.Confidence == 0 {
		req.Confidence = 0.5
	}
	id := insertReturningID(db, `INSERT INTO security_threats (threat_type, location, latitude, longitude, severity, confidence, affected_pus, status, description) VALUES (?,?,?,?,?,?,?,?,?)`,
		req.ThreatType, req.Location, req.Latitude, req.Longitude, req.Severity, req.Confidence, req.AffectedPUs, "active", req.Description)
	logAudit("SECURITY_THREAT_CREATED", "security_threat", fmt.Sprintf("%d", id), 0, map[string]interface{}{"type": req.ThreatType, "severity": req.Severity})
	writeJSON(w, 201, M{"id": id, "status": "active", "message": "Security threat logged"})
}

func handleUpdateSecurityThreat(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	validStatuses := map[string]bool{"active": true, "monitoring": true, "mitigated": true, "resolved": true, "false_alarm": true}
	if !validStatuses[req.Status] {
		writeError(w, 400, "status must be one of: active, monitoring, mitigated, resolved, false_alarm")
		return
	}
	dbExecLog("security_threats", "UPDATE security_threats SET status=? WHERE id=?", req.Status, id)
	logAudit("SECURITY_THREAT_UPDATED", "security_threat", id, 0, map[string]interface{}{"status": req.Status})
	writeJSON(w, 200, M{"id": id, "status": req.Status, "message": "Threat status updated"})
}

func handleCreateCVEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CameraID    string  `json:"camera_id"`
		EventType   string  `json:"event_type"`
		Value       float64 `json:"value"`
		Description string  `json:"description"`
		Confidence  float64 `json:"confidence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.CameraID == "" || req.EventType == "" {
		writeError(w, 400, "camera_id and event_type required")
		return
	}
	validEvents := map[string]bool{"crowd_density": true, "queue_length": true, "suspicious_activity": true, "ballot_tampering": true, "unauthorized_access": true, "violence_detected": true}
	if !validEvents[req.EventType] {
		writeError(w, 400, "event_type must be one of: crowd_density, queue_length, suspicious_activity, ballot_tampering, unauthorized_access, violence_detected")
		return
	}
	if req.Confidence == 0 {
		req.Confidence = 0.8
	}
	id := insertReturningID(db, `INSERT INTO cv_monitoring (camera_id, event_type, value, description, confidence) VALUES (?,?,?,?,?)`,
		req.CameraID, req.EventType, req.Value, req.Description, req.Confidence)
	writeJSON(w, 201, M{"id": id, "camera_id": req.CameraID, "event_type": req.EventType, "message": "CV event recorded"})
}

func handleEnrollTraining(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "presiding_officer", "collation_officer", "returning_officer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	var req struct {
		CourseID int `json:"course_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.CourseID == 0 {
		writeError(w, 400, "course_id required")
		return
	}
	// Verify course exists
	var courseExists int
	db.QueryRow("SELECT COUNT(*) FROM training_courses WHERE id=? AND is_active=1", req.CourseID).Scan(&courseExists)
	if courseExists == 0 {
		writeError(w, 404, "Course not found or inactive")
		return
	}
	userSub, _ := user["sub"].(string)
	userID, _ := strconv.Atoi(userSub)

	// Check if already enrolled
	var existing int
	db.QueryRow("SELECT COUNT(*) FROM training_enrollments WHERE user_id=? AND course_id=?", userID, req.CourseID).Scan(&existing)
	if existing > 0 {
		writeError(w, 409, "Already enrolled in this course")
		return
	}
	id := insertReturningID(db, `INSERT INTO training_enrollments (user_id, course_id, status, progress_percent, enrolled_at) VALUES (?,?,?,?,CURRENT_TIMESTAMP)`,
		userID, req.CourseID, "in_progress", 0)
	writeJSON(w, 201, M{"id": id, "course_id": req.CourseID, "status": "in_progress", "message": "Enrolled successfully"})
}

func handleCompleteTraining(w http.ResponseWriter, r *http.Request) {
	user, err := requireRole(r, "admin", "presiding_officer", "collation_officer", "returning_officer")
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	enrollmentID := mux.Vars(r)["id"]
	var req struct {
		Score int `json:"score"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	userSub, _ := user["sub"].(string)
	userID, _ := strconv.Atoi(userSub)

	// Verify enrollment belongs to user
	var courseID, passingScore int
	err2 := db.QueryRow("SELECT te.course_id, tc.passing_score FROM training_enrollments te JOIN training_courses tc ON tc.id=te.course_id WHERE te.id=? AND te.user_id=?", enrollmentID, userID).Scan(&courseID, &passingScore)
	if err2 != nil {
		writeError(w, 404, "Enrollment not found")
		return
	}
	status := "completed"
	if req.Score < passingScore {
		status = "failed"
	}
	dbExecLog("training_enrollments", "UPDATE training_enrollments SET status=?, score=?, progress_percent=100, completed_at=CURRENT_TIMESTAMP WHERE id=?", status, req.Score, enrollmentID)

	// Issue certificate if passed
	if status == "completed" {
		certID := fmt.Sprintf("CERT-%s-%d-%d", time.Now().Format("20060102"), userID, courseID)
		blockchainHash := fmt.Sprintf("%x", sha256.Sum256([]byte(certID)))
		eID, _ := strconv.Atoi(enrollmentID)
		dbExecLog("training_certificate", `INSERT INTO training_certificates (enrollment_id, user_id, course_id, certificate_id, blockchain_hash, score, issued_at, expires_at) VALUES (?,?,?,?,?,?,CURRENT_TIMESTAMP, datetime('now', '+1 year'))`,
			eID, userID, courseID, certID, blockchainHash, req.Score)
		writeJSON(w, 200, M{"enrollment_id": enrollmentID, "status": status, "score": req.Score, "certificate_id": certID, "message": "Course completed and certificate issued"})
		return
	}
	writeJSON(w, 200, M{"enrollment_id": enrollmentID, "status": status, "score": req.Score, "passing_score": passingScore, "message": "Course completed but score below passing threshold"})
}

func handleResolveGrievance(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Resolution string `json:"resolution"`
		Status     string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	validStatuses := map[string]bool{"under_review": true, "resolved": true, "escalated": true, "dismissed": true}
	if req.Status != "" && !validStatuses[req.Status] {
		writeError(w, 400, "status must be one of: under_review, resolved, escalated, dismissed")
		return
	}
	if req.Status == "" {
		req.Status = "resolved"
	}
	dbExecLog("grievances", "UPDATE grievances SET status=?, resolution=?, resolved_at=CURRENT_TIMESTAMP WHERE id=?", req.Status, req.Resolution, id)
	logAudit("GRIEVANCE_RESOLVED", "grievance", id, 0, map[string]interface{}{"status": req.Status})
	writeJSON(w, 200, M{"id": id, "status": req.Status, "message": "Grievance updated"})
}

func handleSendPushNotification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title      string   `json:"title"`
		Body       string   `json:"body"`
		Recipients []string `json:"recipients"`
		Channel    string   `json:"channel"`
		Priority   string   `json:"priority"`
		ElectionID int      `json:"election_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Title == "" || req.Body == "" {
		writeError(w, 400, "title and body required")
		return
	}
	if req.Channel == "" {
		req.Channel = "push"
	}
	if req.Priority == "" {
		req.Priority = "normal"
	}
	if len(req.Recipients) == 0 {
		req.Recipients = []string{"all"}
	}
	recipientsJSON, _ := json.Marshal(req.Recipients)
	id := insertReturningID(db, `INSERT INTO push_notifications (title, body, recipients, channel, priority, election_id, status, sent_at) VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		req.Title, req.Body, string(recipientsJSON), req.Channel, req.Priority, req.ElectionID, "sent")
	writeJSON(w, 201, M{"id": id, "recipients": len(req.Recipients), "status": "sent", "message": "Notification dispatched"})
}
