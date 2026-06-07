package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"

	"github.com/rs/zerolog/log"
)

// seedComprehensive seeds ALL tables needed for every page to display realistic data.
// Called after all init*() and seed*() functions in main.go.
func seedComprehensive(db *sql.DB) {
	var marker int
	db.QueryRow("SELECT COUNT(*) FROM webhook_subscriptions").Scan(&marker)
	if marker > 0 {
		return
	}
	log.Info().Msg("Seeding comprehensive data for all pages...")
	rand := NewSecureRng()

	// ── Incidents (IncidentsPage) — stakeholder_incidents ──
	// Schema: reporter_id, incident_type (enum), description, severity, latitude, longitude, polling_unit_code, media_urls, status (enum)
	incidentTypes := []string{"violence", "intimidation", "ballot_stuffing", "equipment_failure", "process_violation", "other"}
	incidentStatuses := []string{"reported", "acknowledged", "investigating", "resolved", "escalated", "dismissed"}
	incidentDescs := []string{
		"Observer reported unsealed ballot box at polling unit",
		"Fingerprint scanner not responding on BVAS device",
		"Polling unit opened 2 hours late due to materials delay",
		"Group intimidating voters near PU entrance",
		"EC8A figures don't match BVAS accreditation count",
		"Non-INEC staff found handling sensitive materials",
		"BVAS unable to transmit results for 4+ hours",
		"Multiple underage voting attempts flagged by BVAS",
		"Armed individuals snatched ballot box and fled",
		"Votes cast exceed accredited voters by 47",
	}
	for i, desc := range incidentDescs {
		stIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stIdx]
		lat := 4.0 + rand.Float64()*10
		lng := 2.5 + rand.Float64()*12
		puCode := fmt.Sprintf("%s-001-W001-PU%03d", state.Code, rand.Intn(3)+1)
		sev := []string{"low", "medium", "high", "critical"}[i%4]
		dbExecLog("seed_incident", `INSERT INTO stakeholder_incidents (reporter_id, incident_type, description, severity, latitude, longitude, polling_unit_code, status) VALUES (?,?,?,?,?,?,?,?)`,
			rand.Intn(3)+1, incidentTypes[i%len(incidentTypes)], desc, sev, lat, lng, puCode, incidentStatuses[i%len(incidentStatuses)])
	}

	// ── Disputes (DisputeResolutionPage) ──
	// Schema: election_id, polling_unit_code, filed_by TEXT, party, category, description, evidence, status, priority
	disputeData := []struct{ desc, status, category, party string }{
		{"Declared figures differ from BVAS count by 200+ votes", "filed", "result_dispute", "PDP"},
		{"Party agent was denied entry to collation center", "under_review", "procedural", "LP"},
		{"EC8A form shows signs of physical alteration", "escalated", "result_dispute", "APC"},
		{"52 voters without BVAS verification were allowed to vote", "under_review", "voter_eligibility", "NNPP"},
		{"Mathematical error in ward collation adding party scores", "resolved", "result_dispute", "PDP"},
		{"Sensitive materials arrived 3 hours after scheduled start", "dismissed", "procedural", "LP"},
	}
	priorities := []string{"low", "medium", "high", "critical"}
	for i, d := range disputeData {
		stIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stIdx]
		puCode := fmt.Sprintf("%s-001-W001-PU%03d", state.Code, rand.Intn(3)+1)
		dbExecLog("seed_dispute", `INSERT INTO disputes (election_id, polling_unit_code, filed_by, party, category, description, status, priority) VALUES (?,?,?,?,?,?,?,?)`,
			1, puCode, "admin", d.party, d.category, d.desc, d.status, priorities[i%len(priorities)])
	}

	// ── Dispute Comments ──
	// Schema: dispute_id, author TEXT, content TEXT
	dbExecLog("seed_dc1", `INSERT INTO dispute_comments (dispute_id, author, content) VALUES (1,'admin','Initial investigation opened. Requesting BVAS logs from field officer.')`)
	dbExecLog("seed_dc2", `INSERT INTO dispute_comments (dispute_id, author, content) VALUES (1,'observer','BVAS logs retrieved. Discrepancy confirmed at 213 votes.')`)
	dbExecLog("seed_dc3", `INSERT INTO dispute_comments (dispute_id, author, content) VALUES (3,'admin','Physical examination of EC8A ordered by Resident Electoral Commissioner.')`)

	// ── Observer Reports (ObserverMonitoringPage) ──
	// Schema: observer_id, polling_unit_code, election_id, report_type, description, latitude, longitude, status
	reportTypes := []string{"observation", "observation", "observation", "incident", "observation"}
	for i := 0; i < 25; i++ {
		stIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stIdx]
		puCode := fmt.Sprintf("%s-001-W001-PU%03d", state.Code, rand.Intn(3)+1)
		rType := reportTypes[rand.Intn(len(reportTypes))]
		lat := 4.0 + rand.Float64()*10
		lng := 2.5 + rand.Float64()*12
		desc := fmt.Sprintf("Field observation at %s (%s): voting process %s", puCode, state.Name, []string{"orderly", "minor delays", "well-organized", "issues noted"}[rand.Intn(4)])
		dbExecLog("seed_observer_report", `INSERT INTO observer_reports (observer_id, polling_unit_code, election_id, report_type, description, latitude, longitude, status) VALUES (?,?,?,?,?,?,?,?)`,
			2, puCode, 1, rType, desc, lat, lng, []string{"pending", "reviewed", "pending"}[rand.Intn(3)])
	}

	// ── Observer Check-ins ──
	// Schema: observer_id, polling_unit_code, latitude, longitude, device_info, within_geofence
	for i := 0; i < 15; i++ {
		stIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stIdx]
		lat := 4.0 + rand.Float64()*10
		lng := 2.5 + rand.Float64()*12
		dbExecLog("seed_observer_checkin", `INSERT INTO observer_check_ins (observer_id, polling_unit_code, latitude, longitude, device_info, within_geofence) VALUES (?,?,?,?,?,?)`,
			2, fmt.Sprintf("%s-001-W001-PU001", state.Code), lat, lng, fmt.Sprintf("SM-G998B Android 14 BVAS-%s", state.Code), 1)
	}

	// ── Webhook Subscriptions (WebhookManagementPage) ──
	// Schema: url, events TEXT, secret TEXT, is_active INTEGER, created_by TEXT
	webhooks := []struct{ url, events string }{
		{"https://irev.inecnigeria.org/webhook/results", "result.submitted,result.finalized"},
		{"https://api.channels.tv/election/webhook", "result.finalized,election.status_changed"},
		{"https://yiaga.org/api/wtv/webhook", "incident.created,observer.report"},
		{"https://eu-eom.europa.eu/api/ng/webhook", "observer.report,election.status_changed"},
		{"https://api.premiumtimes.com/elections/webhook", "result.submitted,result.finalized"},
	}
	for _, wh := range webhooks {
		dbExecLog("seed_webhook", `INSERT INTO webhook_subscriptions (url, events, secret, is_active, created_by) VALUES (?,?,?,?,?)`,
			wh.url, wh.events, fmt.Sprintf("whsec_%x", sha256.Sum256([]byte(wh.url)))[:20], 1, "admin")
	}

	// ── Stakeholders (StakeholderPage) ──
	// Schema: name, organization, stakeholder_type (enum), email, phone, accreditation_status (enum), election_id, assigned_area
	stakeData := []struct{ name, org, stype, email, phone, area, status string }{
		{"Mahmud Yakubu", "INEC", "security", "chairman@inecnigeria.org", "+2349010000001", "FCT", "approved"},
		{"Festus Okoye", "INEC", "legal", "fokoye@inecnigeria.org", "+2349010000002", "FCT", "approved"},
		{"Samson Itodo", "YIAGA Africa", "observer", "sitodo@yiaga.org", "+2348010000003", "LA", "approved"},
		{"Clement Nwankwo", "TMG", "observer", "cnwankwo@tmg.org", "+2348010000004", "AB", "approved"},
		{"Kadaria Ahmed", "Channels TV", "media", "kahmed@channels.tv", "+2348020000001", "LA", "approved"},
		{"Tony Orilade", "NTA", "media", "torilade@nta.ng", "+2348020000002", "FCT", "approved"},
		{"Chidi Odinkalu", "Open Society", "cso", "codinkalu@osf.org", "+2348010000005", "AB", "approved"},
		{"Idayat Hassan", "CDD", "cso", "ihassan@cddwestafrica.org", "+2348010000006", "FCT", "approved"},
		{"Peter Obi", "Labour Party", "candidate", "pobi@lp.ng", "+2348030000001", "AN", "approved"},
		{"Sarah Johnson", "EU EOM", "diplomat", "sjohnson@eeas.europa.eu", "+234801000007", "FCT", "pending"},
	}
	for _, s := range stakeData {
		dbExecLog("seed_stakeholder", `INSERT INTO stakeholders (name, organization, stakeholder_type, email, phone, accreditation_status, election_id, assigned_area) VALUES (?,?,?,?,?,?,?,?)`,
			s.name, s.org, s.stype, s.email, s.phone, s.status, 1, s.area)
	}

	// ── Training Courses (TrainingPage) ──
	// Schema: title, description, course_type (enum), target_role, difficulty, duration_minutes, passing_score, modules_count, is_mandatory, is_active
	courseData := []struct {
		title, desc, ctype, role, diff string
		dur, score, modules           int
		mandatory                     int
	}{
		{"BVAS Operation & Troubleshooting", "Comprehensive training on BVAS device operation", "interactive", "officer", "intermediate", 480, 80, 8, 1},
		{"Election Day Procedures", "Step-by-step guide for presiding officers", "video", "officer", "beginner", 360, 70, 6, 1},
		{"Result Collation & Transmission", "EC8A completion and IReV upload procedures", "interactive", "officer", "advanced", 240, 85, 5, 1},
		{"Observer Accreditation Protocol", "Guidelines for accrediting election observers", "video", "observer", "beginner", 180, 70, 3, 0},
		{"VR Election Simulation", "Virtual reality simulation of election day scenarios", "vr_simulation", "officer", "advanced", 120, 75, 4, 0},
		{"Anti-Fraud Detection", "Identifying common fraud patterns in elections", "gamified", "officer", "intermediate", 240, 80, 5, 1},
		{"Disability & Inclusion", "Accessible voting for PWDs and vulnerable groups", "video", "officer", "beginner", 120, 70, 3, 0},
		{"Cybersecurity Awareness", "Security protocols for INEC digital systems", "assessment", "admin", "expert", 300, 90, 7, 1},
	}
	for _, c := range courseData {
		dbExecLog("seed_course", `INSERT INTO training_courses (title, description, course_type, target_role, difficulty, duration_minutes, passing_score, modules_count, is_mandatory, is_active) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			c.title, c.desc, c.ctype, c.role, c.diff, c.dur, c.score, c.modules, c.mandatory, 1)
	}

	// ── Training Enrollments ──
	// Schema: user_id, course_id, progress_percent, current_module, score, status (enum)
	enrollStatuses := []string{"enrolled", "in_progress", "in_progress", "completed", "completed"}
	for i := 0; i < 20; i++ {
		courseID := rand.Intn(8) + 1
		userID := rand.Intn(3) + 1
		progress := float64(rand.Intn(101))
		module := rand.Intn(8) + 1
		status := enrollStatuses[rand.Intn(len(enrollStatuses))]
		score := 0
		if status == "completed" {
			progress = 100
			score = 70 + rand.Intn(31)
		}
		dbExecLog("seed_enrollment", `INSERT INTO training_enrollments (user_id, course_id, progress_percent, current_module, score, status) VALUES (?,?,?,?,?,?)`,
			userID, courseID, progress, module, score, status)
	}

	// ── Training Certificates (for completed enrollments) ──
	// Schema: enrollment_id, user_id, course_id, certificate_id, blockchain_hash, score
	for i := 1; i <= 8; i++ {
		certID := fmt.Sprintf("INEC-CERT-2027-%04d", i)
		bHash := fmt.Sprintf("%x", sha256.Sum256([]byte(certID)))
		dbExecLog("seed_cert", `INSERT INTO training_certificates (enrollment_id, user_id, course_id, certificate_id, blockchain_hash, score) VALUES (?,?,?,?,?,?)`,
			i, rand.Intn(3)+1, rand.Intn(8)+1, certID, bHash, 70+rand.Intn(31))
	}

	// ── Geofenced Submissions (GeofencingPage) ──
	// Schema: result_id, officer_lat, officer_lng, pu_lat, pu_lng, distance_meters, within_boundary, override_by, override_reason
	for i := 0; i < 20; i++ {
		officerLat := 4.0 + rand.Float64()*10
		officerLng := 2.5 + rand.Float64()*12
		puLat := officerLat + (rand.Float64()-0.5)*0.005
		puLng := officerLng + (rand.Float64()-0.5)*0.005
		dist := rand.Float64() * 500
		within := 0
		if dist < 200 {
			within = 1
		}
		dbExecLog("seed_geofence", `INSERT INTO geofenced_submissions (result_id, officer_lat, officer_lng, pu_lat, pu_lng, distance_meters, within_boundary) VALUES (?,?,?,?,?,?,?)`,
			rand.Intn(800)+1, officerLat, officerLng, puLat, puLng, dist, within)
	}

	// ── GPS Spoof Events (GeofencingPage) ──
	// Schema: device_id, lat, lng, confidence, indicators, detected_at
	for i := 0; i < 5; i++ {
		dbExecLog("seed_gps_spoof", `INSERT INTO gps_spoof_events (device_id, lat, lng, confidence, indicators) VALUES (?,?,?,?,?)`,
			fmt.Sprintf("BVAS-LA-%03d", rand.Intn(50)), 6.55+rand.Float64()*0.1, 3.30+rand.Float64()*0.1,
			0.7+rand.Float64()*0.3, `["mock_location_enabled","accuracy_anomaly"]`)
	}

	// ── AI Predictions (PredictiveAnalyticsPage + AIMonitoringPage) ──
	// Schema: prediction_type (enum), target_area, target_level (enum), predicted_value, confidence, model_name, features_used, election_id
	aiPredTypes := []string{"turnout", "resource", "security_threat", "sentiment", "misinformation"}
	targetLevels := []string{"national", "state", "lga"}
	for i := 0; i < 40; i++ {
		stIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stIdx]
		pType := aiPredTypes[rand.Intn(len(aiPredTypes))]
		level := targetLevels[rand.Intn(len(targetLevels))]
		confidence := 0.6 + rand.Float64()*0.4
		value := rand.Float64() * 100
		dbExecLog("seed_prediction", `INSERT INTO ai_predictions (prediction_type, target_area, target_level, predicted_value, confidence, model_name, features_used, election_id) VALUES (?,?,?,?,?,?,?,?)`,
			pType, state.Code, level, value, confidence, "xgboost-turnout-v2", `["historical_turnout","demographics","security_index"]`, 1)
	}

	// ── Anomaly Escalations (AnomalyDetectionPage) ──
	// Schema: anomaly_id, severity, state_code, pu_code, action_taken, escalated_to, collation_paused, resolved
	anomSeverities := []string{"WARN", "CRITICAL", "EMERGENCY"}
	for i := 0; i < 15; i++ {
		stIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stIdx]
		puCode := fmt.Sprintf("%s-001-W%03d-PU%03d", state.Code, rand.Intn(5)+1, rand.Intn(3)+1)
		sev := anomSeverities[rand.Intn(len(anomSeverities))]
		resolved := rand.Intn(2)
		dbExecLog("seed_anomaly", `INSERT INTO anomaly_escalations (anomaly_id, severity, state_code, pu_code, action_taken, escalated_to, collation_paused, resolved) VALUES (?,?,?,?,?,?,?,?)`,
			fmt.Sprintf("ANOM-%04d", i+1), sev, state.Code, puCode, "auto_flagged", "state_rec", 0, resolved)
	}

	// ── SMS Delivery Log (SMSVerificationPage) ──
	// Schema: provider, message_id, phone, message, direction, status, cost
	smsStatuses := []string{"delivered", "delivered", "delivered", "queued", "failed"}
	for i := 0; i < 20; i++ {
		phone := fmt.Sprintf("+234%d", 8000000000+int64(rand.Intn(999999999)))
		stIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stIdx]
		puCode := fmt.Sprintf("%s-001-W001-PU001", state.Code)
		msgID := fmt.Sprintf("SM%x", sha256.Sum256([]byte(fmt.Sprintf("sms-%d", i))))[:18]
		status := smsStatuses[rand.Intn(len(smsStatuses))]
		dbExecLog("seed_sms", `INSERT INTO sms_delivery_log (provider, message_id, phone, message, direction, status, cost) VALUES (?,?,?,?,?,?,?)`,
			"twilio", msgID, phone, fmt.Sprintf("INEC Result Verification for PU %s: APC 245, PDP 198, LP 156", puCode),
			"outbound", status, 0.05)
	}

	// ── Citizen Verifications (CitizenPortalPage) ──
	// Schema: pu_code, ip_hash
	for i := 0; i < 30; i++ {
		stIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stIdx]
		puCode := fmt.Sprintf("%s-001-W001-PU%03d", state.Code, rand.Intn(3)+1)
		ipHash := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("ip-%d", i))))[:16]
		dbExecLog("seed_citizen_verify", `INSERT INTO citizen_verifications (pu_code, ip_hash) VALUES (?,?)`,
			puCode, ipHash)
	}

	// ── Media API Keys (PublicAPIPage) ──
	// Schema: api_key, org_name, contact_email, rate_limit, is_active
	mediaOrgs := []struct{ org, email string }{
		{"Channels Television", "api@channels.tv"},
		{"Premium Times", "api@premiumtimes.com"},
		{"The Punch Newspapers", "api@punchng.com"},
		{"BBC Africa Service", "api@bbc.co.uk"},
		{"Thomson Reuters", "api@reuters.com"},
	}
	for _, m := range mediaOrgs {
		apiKey := fmt.Sprintf("mk_%x", sha256.Sum256([]byte(m.org)))[:24]
		dbExecLog("seed_media_key", `INSERT INTO media_api_keys (api_key, org_name, contact_email, rate_limit, is_active) VALUES (?,?,?,?,?)`,
			apiKey, m.org, m.email, 1000, 1)
	}

	// ── Predictive Analytics per state (PredictiveAnalyticsPage) ──
	// Schema: election_id, state_code, predicted_turnout, confidence, model_version
	for _, state := range nigeriaStates {
		turnout := 30 + rand.Float64()*50
		dbExecLog("seed_predictive", `INSERT INTO predictive_analytics (election_id, state_code, predicted_turnout, confidence, model_version) VALUES (?,?,?,?,?)`,
			1, state.Code, turnout, 0.7+rand.Float64()*0.3, "ensemble-v3")
	}

	// ── Sentiment Analysis (AIMonitoringPage) ──
	// Schema: source (enum), content_snippet, sentiment (enum), score, topics, location, language, election_id
	sentSources := []string{"twitter", "facebook", "news", "whatsapp", "radio"}
	sentiments := []string{"positive", "negative", "neutral", "mixed"}
	snippets := []string{
		"INEC doing a good job with BVAS technology this election",
		"Still waiting at PU, materials not yet arrived!",
		"Results from my LGA look accurate and match what we saw",
		"Why is IReV so slow? We need real-time results!",
		"Kudos to INEC staff for working tirelessly",
		"Reports of intimidation in some areas, security needed",
	}
	for i := 0; i < 30; i++ {
		source := sentSources[rand.Intn(len(sentSources))]
		sentiment := sentiments[rand.Intn(len(sentiments))]
		score := -1.0 + rand.Float64()*2.0
		snippet := snippets[rand.Intn(len(snippets))]
		stIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stIdx]
		dbExecLog("seed_sentiment", `INSERT INTO sentiment_analysis (source, content_snippet, sentiment, score, topics, location, language, election_id) VALUES (?,?,?,?,?,?,?,?)`,
			source, snippet, sentiment, score, `["election","INEC","voting"]`, state.Name, "en", 1)
	}

	// ── Misinformation Alerts (AIMonitoringPage) ──
	// Schema: content, source_platform, source_url, classification (enum), confidence, severity, reach_estimate, status, fact_check
	misinfoData := []struct{ content, platform, class, factCheck string }{
		{"Fake results circulating claiming party X won with 90%", "twitter", "fake_result", "Official results not yet declared for this LGA"},
		{"False claim that INEC cancelled election in Lagos", "facebook", "false_claim", "No cancellation order issued. Voting continues normally"},
		{"Manipulated video showing ballot stuffing", "whatsapp", "manipulated_media", "Video is from 2019 election, not 2027"},
		{"Account impersonating INEC Chairman posting fake results", "twitter", "impersonation", "Official INEC account verified — this is a fake"},
		{"Posts inciting violence at polling units", "facebook", "incitement", "Content reported to platform for removal"},
	}
	misinfoStatuses := []string{"detected", "verified", "debunked", "monitoring", "escalated"}
	misinfoSeverities := []string{"low", "medium", "high", "critical"}
	for i, m := range misinfoData {
		dbExecLog("seed_misinfo", `INSERT INTO misinformation_alerts (content, source_platform, source_url, classification, confidence, severity, reach_estimate, status, fact_check) VALUES (?,?,?,?,?,?,?,?,?)`,
			m.content, m.platform, fmt.Sprintf("https://%s.com/post/%d", m.platform, 100000+rand.Intn(900000)),
			m.class, 0.7+rand.Float64()*0.3, misinfoSeverities[i%len(misinfoSeverities)],
			rand.Intn(100000)+1000, misinfoStatuses[i%len(misinfoStatuses)], m.factCheck)
	}

	// ── Security Threats (ProductionPage + ScaleHealthPage) ──
	// Schema: threat_type (enum), location, latitude, longitude, severity, confidence, source, description, affected_pus, status
	threatTypes := []string{"violence", "protest", "road_blockage", "device_theft", "cyber_attack", "impersonation"}
	threatStatuses := []string{"active", "monitoring", "mitigated", "resolved", "false_alarm"}
	for i := 0; i < 15; i++ {
		stIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stIdx]
		tType := threatTypes[rand.Intn(len(threatTypes))]
		lat := 4.0 + rand.Float64()*10
		lng := 2.5 + rand.Float64()*12
		dbExecLog("seed_threat", `INSERT INTO security_threats (threat_type, location, latitude, longitude, severity, confidence, source, description, affected_pus, status) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			tType, state.Name, lat, lng,
			misinfoSeverities[rand.Intn(len(misinfoSeverities))], 0.5+rand.Float64()*0.5,
			"intelligence", fmt.Sprintf("Security threat: %s reported in %s", tType, state.Name),
			rand.Intn(20), threatStatuses[rand.Intn(len(threatStatuses))])
	}

	// ── Push Notifications (Mobile) ──
	// Schema: user_id, device_token, title, body, data, topic, status
	notifData := []struct{ title, body, topic string }{
		{"Result Update: Lagos State", "Presidential results from Ikeja LGA have been collated", "results"},
		{"Security Alert: Kano", "Security incident reported near polling units in Kano Municipal", "security"},
		{"BVAS Sync Required", "Your BVAS device has pending sync items. Please connect to network", "device"},
		{"Election Day Reminder", "Polling units open at 8:30am. Ensure all materials are ready", "operations"},
		{"Collation Complete: Abuja", "FCT ward-level collation complete. View results on IReV", "results"},
	}
	for i := 0; i < 15; i++ {
		n := notifData[i%len(notifData)]
		userID := rand.Intn(3) + 1
		dbExecLog("seed_push", `INSERT INTO push_notifications (user_id, device_token, title, body, topic, status) VALUES (?,?,?,?,?,?)`,
			userID, fmt.Sprintf("ExponentPushToken[%x]", sha256.Sum256([]byte(fmt.Sprintf("tok-%d", i))))[:40],
			n.title, n.body, n.topic, "delivered")
	}

	// ── Command Alerts (CommandCenterPage) ──
	// Schema: level, state_code, message, auto_action, resolved
	alertData := []struct{ level, stateCode, msg, action string }{
		{"WARN", "LA", "15 polling units in Lagos reporting zero submissions after 2 hours", "notify_state_rec"},
		{"CRITICAL", "KN", "Anomaly surge detected: 8 statistical outliers in Kano", "pause_collation"},
		{"WARN", "RI", "BVAS connectivity drops below 50% in Rivers State", "notify_state_rec"},
		{"EMERGENCY", "BO", "No submissions received from entire Borno LGA for 4 hours", "notify_chairman"},
		{"WARN", "OY", "Turnout prediction deviation exceeds 20% threshold in Oyo", "notify_state_rec"},
		{"CRITICAL", "DE", "Security threat escalation in Delta — 3 incidents in 1 hour", "pause_collation"},
		{"WARN", "EN", "Over-voting flagged at 4 polling units in Enugu", "notify_state_rec"},
		{"WARN", "AB", "Result transmission delay exceeding 3 hours in Abia", "notify_state_rec"},
	}
	for _, a := range alertData {
		dbExecLog("seed_cmd_alert", `INSERT INTO command_alerts (level, state_code, message, auto_action, resolved) VALUES (?,?,?,?,?)`,
			a.level, a.stateCode, a.msg, a.action, 0)
	}

	log.Info().Msg("Comprehensive seed complete — all page data populated")
}
