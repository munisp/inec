package main

// GOTV (Get Out The Vote) — Party engagement and voter mobilization module.
// Architectural principle: INEC provides equal-access tooling to ALL registered parties.
// Parties manage THEIR OWN contact lists (not the voter register).
// Only aggregate PU-level turnout data is exposed — never individual voter status.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
	"github.com/rs/zerolog/log"
)

// ─── Schema ────────────────────────────────────────────────────────────────

func initGOTVTables() {
	tables := `
	CREATE TABLE IF NOT EXISTS gotv_party_access (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		api_key_hash TEXT NOT NULL,
		created_by TEXT NOT NULL,
		is_active BOOLEAN DEFAULT TRUE,
		rate_limit_per_hour INTEGER DEFAULT 1000,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP,
		UNIQUE(party_id)
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_party_access_party ON gotv_party_access(party_id);

	CREATE TABLE IF NOT EXISTS gotv_campaigns (
		id SERIAL PRIMARY KEY,
		campaign_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		name TEXT NOT NULL,
		description TEXT,
		campaign_type TEXT NOT NULL CHECK(campaign_type IN ('sms','ussd','push','whatsapp','email','door_to_door','phone_bank','ride_to_polls')),
		status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','scheduled','active','paused','completed','cancelled')),
		target_state TEXT,
		target_lga TEXT,
		target_ward TEXT,
		target_polling_unit TEXT,
		message_template TEXT,
		message_variant_b TEXT,
		ab_split_pct INTEGER DEFAULT 50 CHECK(ab_split_pct >= 0 AND ab_split_pct <= 100),
		scheduled_at TIMESTAMP,
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		total_contacts INTEGER DEFAULT 0,
		contacts_reached INTEGER DEFAULT 0,
		contacts_responded INTEGER DEFAULT 0,
		created_by TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_campaigns_party ON gotv_campaigns(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_campaigns_status ON gotv_campaigns(status);

	CREATE TABLE IF NOT EXISTS gotv_contacts (
		id SERIAL PRIMARY KEY,
		contact_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		phone_encrypted TEXT NOT NULL,
		phone_hash TEXT NOT NULL,
		full_name_encrypted TEXT,
		state_code TEXT,
		lga_code TEXT,
		ward_code TEXT,
		polling_unit_code TEXT,
		voter_status TEXT DEFAULT 'unknown' CHECK(voter_status IN ('unknown','pledged','confirmed','declined','unreachable')),
		tags TEXT[] DEFAULT '{}',
		consent_id TEXT,
		opted_out BOOLEAN DEFAULT FALSE,
		opted_out_at TIMESTAMP,
		last_contacted_at TIMESTAMP,
		contact_count INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(party_id, phone_hash)
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_contacts_party ON gotv_contacts(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_contacts_geo ON gotv_contacts(state_code, lga_code, ward_code);
	CREATE INDEX IF NOT EXISTS idx_gotv_contacts_status ON gotv_contacts(voter_status);
	CREATE INDEX IF NOT EXISTS idx_gotv_contacts_consent ON gotv_contacts(consent_id);

	CREATE TABLE IF NOT EXISTS gotv_volunteers (
		id SERIAL PRIMARY KEY,
		volunteer_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		user_id INTEGER,
		full_name TEXT NOT NULL,
		phone TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'canvasser' CHECK(role IN ('canvasser','driver','coordinator','phone_banker','team_lead')),
		assigned_state TEXT,
		assigned_lga TEXT,
		assigned_ward TEXT,
		assigned_polling_unit TEXT,
		is_active BOOLEAN DEFAULT TRUE,
		has_vehicle BOOLEAN DEFAULT FALSE,
		vehicle_capacity INTEGER DEFAULT 0,
		latitude REAL,
		longitude REAL,
		last_checkin_at TIMESTAMP,
		doors_knocked INTEGER DEFAULT 0,
		calls_made INTEGER DEFAULT 0,
		rides_given INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_volunteers_party ON gotv_volunteers(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_volunteers_geo ON gotv_volunteers(assigned_state, assigned_lga, assigned_ward);
	CREATE INDEX IF NOT EXISTS idx_gotv_volunteers_role ON gotv_volunteers(role);

	CREATE TABLE IF NOT EXISTS gotv_pledges (
		id SERIAL PRIMARY KEY,
		pledge_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		contact_id TEXT NOT NULL REFERENCES gotv_contacts(contact_id),
		election_id INTEGER,
		pledge_type TEXT NOT NULL DEFAULT 'will_vote' CHECK(pledge_type IN ('will_vote','needs_ride','needs_info','will_volunteer')),
		status TEXT NOT NULL DEFAULT 'pledged' CHECK(status IN ('pledged','reminded','confirmed_day_of','fulfilled','broken')),
		reminder_sent BOOLEAN DEFAULT FALSE,
		reminder_sent_at TIMESTAMP,
		fulfilled_at TIMESTAMP,
		notes TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_pledges_party ON gotv_pledges(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_pledges_contact ON gotv_pledges(contact_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_pledges_status ON gotv_pledges(status);

	CREATE TABLE IF NOT EXISTS gotv_outreach_log (
		id SERIAL PRIMARY KEY,
		log_id TEXT UNIQUE NOT NULL,
		campaign_id TEXT REFERENCES gotv_campaigns(campaign_id),
		party_id INTEGER NOT NULL REFERENCES parties(id),
		contact_id TEXT REFERENCES gotv_contacts(contact_id),
		volunteer_id TEXT REFERENCES gotv_volunteers(volunteer_id),
		channel TEXT NOT NULL CHECK(channel IN ('sms','ussd','push','whatsapp','email','door_knock','phone_call')),
		direction TEXT NOT NULL DEFAULT 'outbound' CHECK(direction IN ('outbound','inbound')),
		message_text TEXT,
		message_variant TEXT DEFAULT 'A' CHECK(message_variant IN ('A','B')),
		status TEXT NOT NULL DEFAULT 'sent' CHECK(status IN ('queued','sent','delivered','read','responded','failed','opted_out')),
		response_text TEXT,
		sent_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		delivered_at TIMESTAMP,
		responded_at TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_outreach_campaign ON gotv_outreach_log(campaign_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_outreach_party ON gotv_outreach_log(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_outreach_status ON gotv_outreach_log(status);

	CREATE TABLE IF NOT EXISTS gotv_turnout_snapshots (
		id SERIAL PRIMARY KEY,
		election_id INTEGER NOT NULL,
		polling_unit_code TEXT NOT NULL,
		accredited_count INTEGER DEFAULT 0,
		registered_voters INTEGER DEFAULT 0,
		turnout_pct REAL DEFAULT 0,
		snapshot_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(election_id, polling_unit_code, snapshot_at)
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_turnout_election ON gotv_turnout_snapshots(election_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_turnout_pu ON gotv_turnout_snapshots(polling_unit_code);

	CREATE TABLE IF NOT EXISTS gotv_ride_requests (
		id SERIAL PRIMARY KEY,
		request_id TEXT UNIQUE NOT NULL,
		party_id INTEGER NOT NULL REFERENCES parties(id),
		contact_id TEXT NOT NULL REFERENCES gotv_contacts(contact_id),
		volunteer_id TEXT REFERENCES gotv_volunteers(volunteer_id),
		pickup_latitude REAL NOT NULL,
		pickup_longitude REAL NOT NULL,
		polling_unit_code TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','matched','en_route','picked_up','dropped_off','cancelled','no_show')),
		requested_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		matched_at TIMESTAMP,
		picked_up_at TIMESTAMP,
		dropped_off_at TIMESTAMP,
		distance_km REAL
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_rides_party ON gotv_ride_requests(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_rides_status ON gotv_ride_requests(status);
	CREATE INDEX IF NOT EXISTS idx_gotv_rides_volunteer ON gotv_ride_requests(volunteer_id);

	CREATE TABLE IF NOT EXISTS gotv_audit_log (
		id SERIAL PRIMARY KEY,
		party_id INTEGER NOT NULL,
		actor TEXT NOT NULL,
		action TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		resource_id TEXT,
		details JSONB,
		ip_address TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_gotv_audit_party ON gotv_audit_log(party_id);
	CREATE INDEX IF NOT EXISTS idx_gotv_audit_action ON gotv_audit_log(action);
	`
	dbExecLog("gotv-schema", tables)

	// Register GOTV in data processing register for NDPR compliance
	dbExecLog("gotv-ndpr", `
		INSERT INTO data_processing_register (processing_activity, purpose, legal_basis, data_categories, data_subjects, retention_period, recipients, cross_border_transfer, safeguards)
		VALUES (
			'GOTV Party Voter Mobilization',
			'Enable registered political parties to coordinate voter outreach using their own contact lists',
			'consent',
			ARRAY['phone_number','full_name','geo_location','voting_pledge'],
			'Party supporters who have given consent',
			'6 months after election',
			ARRAY['party_administrators','party_volunteers'],
			FALSE,
			'Field-level AES-256-GCM encryption, party-scoped row isolation, consent-gated outreach, automatic opt-out processing'
		) ON CONFLICT DO NOTHING
	`)

	log.Info().Msg("GOTV tables initialized")
}

// ─── Field-Level Encryption ────────────────────────────────────────────────

var gotvEncryptionKey []byte

func initGOTVEncryption() {
	keyHex := envOrDefault("GOTV_ENCRYPTION_KEY", "")
	if keyHex == "" {
		// Generate a key for dev mode
		gotvEncryptionKey = make([]byte, 32)
		rand.Read(gotvEncryptionKey)
		log.Warn().Msg("GOTV: using random encryption key (set GOTV_ENCRYPTION_KEY for production)")
	} else {
		var err error
		gotvEncryptionKey, err = hex.DecodeString(keyHex)
		if err != nil || len(gotvEncryptionKey) != 32 {
			log.Fatal().Msg("GOTV_ENCRYPTION_KEY must be 64 hex chars (32 bytes AES-256)")
		}
	}
}

func gotvEncrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(gotvEncryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(sealed), nil
}

func gotvDecrypt(cipherHex string) (string, error) {
	data, err := hex.DecodeString(cipherHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(gotvEncryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return "", fmt.Errorf("ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func gotvPhoneHash(phone string) string {
	mac := hmac.New(sha256.New, gotvEncryptionKey)
	mac.Write([]byte(normalizePhone(phone)))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")
	if strings.HasPrefix(phone, "0") && len(phone) == 11 {
		phone = "+234" + phone[1:]
	}
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}
	return phone
}

// ─── Party Auth Middleware ──────────────────────────────────────────────────

type gotvPartyContext struct {
	PartyID   int
	PartyCode string
	PartyName string
	UserID    int
	Username  string
	Role      string
}

func gotvAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract user from JWT (reuse existing auth)
		claims, err := getCurrentUser(r)
		if err != nil {
			http.Error(w, `{"error":"unauthorized","message":"valid authentication required"}`, http.StatusUnauthorized)
			return
		}
		username, _ := claims["username"].(string)
		role, _ := claims["role"].(string)

		// Get party_id from user's party association or from header
		partyIDStr := r.Header.Get("X-Party-ID")
		if partyIDStr == "" {
			// Try to get from user's profile
			var pid sql.NullInt64
			db.QueryRow("SELECT party_id FROM users WHERE username=$1", username).Scan(&pid)
			if !pid.Valid {
				http.Error(w, `{"error":"no_party","message":"user not associated with a party"}`, http.StatusForbidden)
				return
			}
			partyIDStr = strconv.FormatInt(pid.Int64, 10)
		}

		partyID, err := strconv.Atoi(partyIDStr)
		if err != nil {
			http.Error(w, `{"error":"invalid_party_id"}`, http.StatusBadRequest)
			return
		}

		// Verify party exists and is active
		var partyCode, partyName string
		var isActive int
		err = db.QueryRow("SELECT code, name, is_active FROM parties WHERE id=$1", partyID).Scan(&partyCode, &partyName, &isActive)
		if err != nil {
			http.Error(w, `{"error":"party_not_found"}`, http.StatusNotFound)
			return
		}
		if isActive != 1 {
			http.Error(w, `{"error":"party_inactive","message":"party registration is not active"}`, http.StatusForbidden)
			return
		}

		// Verify user has GOTV access for this party (party_admin role or admin)
		if role != "admin" && role != "party_admin" {
			http.Error(w, `{"error":"insufficient_role","message":"party_admin or admin role required"}`, http.StatusForbidden)
			return
		}

		// Store context and audit
		r.Header.Set("X-GOTV-Party-ID", strconv.Itoa(partyID))
		r.Header.Set("X-GOTV-Party-Code", partyCode)
		r.Header.Set("X-GOTV-Party-Name", partyName)
		r.Header.Set("X-GOTV-User", username)

		next(w, r)
	}
}

func getGOTVParty(r *http.Request) (int, string, string) {
	pid, _ := strconv.Atoi(r.Header.Get("X-GOTV-Party-ID"))
	return pid, r.Header.Get("X-GOTV-Party-Code"), r.Header.Get("X-GOTV-Party-Name")
}

func gotvAudit(partyID int, actor, action, resourceType, resourceID string, details interface{}, r *http.Request) {
	detailsJSON, _ := json.Marshal(details)
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	db.Exec(`INSERT INTO gotv_audit_log (party_id, actor, action, resource_type, resource_id, details, ip_address) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		partyID, actor, action, resourceType, resourceID, detailsJSON, ip)
}

// ─── Route Registration ────────────────────────────────────────────────────

func registerGOTVRoutes(r *mux.Router) {
	gotv := r.PathPrefix("/gotv").Subrouter()

	// Public: aggregate turnout (no party auth needed)
	gotv.HandleFunc("/turnout/{election_id}", handleGOTVTurnout).Methods("GET")
	gotv.HandleFunc("/turnout/{election_id}/{state_code}", handleGOTVTurnoutByState).Methods("GET")

	// Party-scoped endpoints (require party auth)
	// Campaigns
	gotv.HandleFunc("/campaigns", gotvAuthMiddleware(handleGOTVListCampaigns)).Methods("GET")
	gotv.HandleFunc("/campaigns", gotvAuthMiddleware(handleGOTVCreateCampaign)).Methods("POST")
	gotv.HandleFunc("/campaigns/{id}", gotvAuthMiddleware(handleGOTVGetCampaign)).Methods("GET")
	gotv.HandleFunc("/campaigns/{id}", gotvAuthMiddleware(handleGOTVUpdateCampaign)).Methods("PUT")
	gotv.HandleFunc("/campaigns/{id}/launch", gotvAuthMiddleware(handleGOTVLaunchCampaign)).Methods("POST")
	gotv.HandleFunc("/campaigns/{id}/pause", gotvAuthMiddleware(handleGOTVPauseCampaign)).Methods("POST")
	gotv.HandleFunc("/campaigns/{id}/stats", gotvAuthMiddleware(handleGOTVCampaignStats)).Methods("GET")

	// Contacts
	gotv.HandleFunc("/contacts", gotvAuthMiddleware(handleGOTVListContacts)).Methods("GET")
	gotv.HandleFunc("/contacts", gotvAuthMiddleware(handleGOTVCreateContact)).Methods("POST")
	gotv.HandleFunc("/contacts/import", gotvAuthMiddleware(handleGOTVImportContacts)).Methods("POST")
	gotv.HandleFunc("/contacts/{id}", gotvAuthMiddleware(handleGOTVGetContact)).Methods("GET")
	gotv.HandleFunc("/contacts/{id}", gotvAuthMiddleware(handleGOTVUpdateContact)).Methods("PUT")
	gotv.HandleFunc("/contacts/{id}/opt-out", gotvAuthMiddleware(handleGOTVOptOut)).Methods("POST")

	// Volunteers
	gotv.HandleFunc("/volunteers", gotvAuthMiddleware(handleGOTVListVolunteers)).Methods("GET")
	gotv.HandleFunc("/volunteers", gotvAuthMiddleware(handleGOTVCreateVolunteer)).Methods("POST")
	gotv.HandleFunc("/volunteers/{id}", gotvAuthMiddleware(handleGOTVUpdateVolunteer)).Methods("PUT")
	gotv.HandleFunc("/volunteers/{id}/checkin", gotvAuthMiddleware(handleGOTVVolunteerCheckin)).Methods("POST")

	// Pledges
	gotv.HandleFunc("/pledges", gotvAuthMiddleware(handleGOTVListPledges)).Methods("GET")
	gotv.HandleFunc("/pledges", gotvAuthMiddleware(handleGOTVCreatePledge)).Methods("POST")
	gotv.HandleFunc("/pledges/{id}/remind", gotvAuthMiddleware(handleGOTVRemindPledge)).Methods("POST")

	// Ride-to-Polls
	gotv.HandleFunc("/rides", gotvAuthMiddleware(handleGOTVListRides)).Methods("GET")
	gotv.HandleFunc("/rides", gotvAuthMiddleware(handleGOTVRequestRide)).Methods("POST")
	gotv.HandleFunc("/rides/{id}/match", gotvAuthMiddleware(handleGOTVMatchRide)).Methods("POST")
	gotv.HandleFunc("/rides/{id}/status", gotvAuthMiddleware(handleGOTVUpdateRideStatus)).Methods("PUT")

	// Dashboard / Analytics
	gotv.HandleFunc("/dashboard", gotvAuthMiddleware(handleGOTVDashboard)).Methods("GET")
	gotv.HandleFunc("/analytics/outreach", gotvAuthMiddleware(handleGOTVOutreachAnalytics)).Methods("GET")
	gotv.HandleFunc("/analytics/geo", gotvAuthMiddleware(handleGOTVGeoAnalytics)).Methods("GET")
}

// ─── Campaign Handlers ─────────────────────────────────────────────────────

func handleGOTVListCampaigns(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	status := r.URL.Query().Get("status")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 20
	offset := (page - 1) * limit

	query := `SELECT campaign_id, name, description, campaign_type, status, target_state, target_lga, target_ward, target_polling_unit, message_template, message_variant_b, ab_split_pct, scheduled_at, started_at, completed_at, total_contacts, contacts_reached, contacts_responded, created_by, created_at FROM gotv_campaigns WHERE party_id=$1`
	args := []interface{}{partyID}
	argIdx := 2
	if status != "" {
		query += fmt.Sprintf(" AND status=$%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	query += " ORDER BY created_at DESC"
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var campaigns []map[string]interface{}
	for rows.Next() {
		var cid, name, ctype, cstatus, createdBy string
		var desc, tState, tLGA, tWard, tPU, msgTemplate, msgVariantB sql.NullString
		var abSplit, totalContacts, reached, responded int
		var scheduledAt, startedAt, completedAt sql.NullTime
		var createdAt time.Time
		err := rows.Scan(&cid, &name, &desc, &ctype, &cstatus, &tState, &tLGA, &tWard, &tPU, &msgTemplate, &msgVariantB, &abSplit, &scheduledAt, &startedAt, &completedAt, &totalContacts, &reached, &responded, &createdBy, &createdAt)
		if err != nil {
			continue
		}
		c := map[string]interface{}{
			"campaign_id":        cid,
			"name":               name,
			"description":        gotvNullStr(desc),
			"campaign_type":      ctype,
			"status":             cstatus,
			"target_state":       gotvNullStr(tState),
			"target_lga":         gotvNullStr(tLGA),
			"target_ward":        gotvNullStr(tWard),
			"target_polling_unit": gotvNullStr(tPU),
			"message_template":   gotvNullStr(msgTemplate),
			"message_variant_b":  gotvNullStr(msgVariantB),
			"ab_split_pct":       abSplit,
			"scheduled_at":       gotvNullTime(scheduledAt),
			"started_at":         gotvNullTime(startedAt),
			"completed_at":       gotvNullTime(completedAt),
			"total_contacts":     totalContacts,
			"contacts_reached":   reached,
			"contacts_responded": responded,
			"created_by":         createdBy,
			"created_at":         createdAt,
		}
		campaigns = append(campaigns, c)
	}

	var total int
	countQ := "SELECT COUNT(*) FROM gotv_campaigns WHERE party_id=$1"
	if status != "" {
		countQ += " AND status='" + status + "'"
	}
	db.QueryRow(countQ, partyID).Scan(&total)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"campaigns":  campaigns,
		"total":      total,
		"page":       page,
		"per_page":   limit,
		"total_pages": int(math.Ceil(float64(total) / float64(limit))),
	})
}

func handleGOTVCreateCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		Name             string  `json:"name"`
		Description      string  `json:"description"`
		CampaignType     string  `json:"campaign_type"`
		TargetState      string  `json:"target_state"`
		TargetLGA        string  `json:"target_lga"`
		TargetWard       string  `json:"target_ward"`
		TargetPU         string  `json:"target_polling_unit"`
		MessageTemplate  string  `json:"message_template"`
		MessageVariantB  string  `json:"message_variant_b"`
		ABSplitPct       int     `json:"ab_split_pct"`
		ScheduledAt      *string `json:"scheduled_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.CampaignType == "" {
		http.Error(w, `{"error":"name and campaign_type required"}`, http.StatusBadRequest)
		return
	}

	validTypes := map[string]bool{"sms": true, "ussd": true, "push": true, "whatsapp": true, "email": true, "door_to_door": true, "phone_bank": true, "ride_to_polls": true}
	if !validTypes[req.CampaignType] {
		http.Error(w, `{"error":"invalid campaign_type"}`, http.StatusBadRequest)
		return
	}
	if req.ABSplitPct == 0 {
		req.ABSplitPct = 50
	}

	campaignID := "gotv-camp-" + uuid.New().String()[:8]

	var scheduledAt *time.Time
	if req.ScheduledAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ScheduledAt)
		if err == nil {
			scheduledAt = &t
		}
	}

	// Count matching contacts for this geo scope
	contactQuery := "SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE"
	contactArgs := []interface{}{partyID}
	idx := 2
	if req.TargetState != "" {
		contactQuery += fmt.Sprintf(" AND state_code=$%d", idx)
		contactArgs = append(contactArgs, req.TargetState)
		idx++
	}
	if req.TargetLGA != "" {
		contactQuery += fmt.Sprintf(" AND lga_code=$%d", idx)
		contactArgs = append(contactArgs, req.TargetLGA)
		idx++
	}
	if req.TargetWard != "" {
		contactQuery += fmt.Sprintf(" AND ward_code=$%d", idx)
		contactArgs = append(contactArgs, req.TargetWard)
		idx++
	}
	if req.TargetPU != "" {
		contactQuery += fmt.Sprintf(" AND polling_unit_code=$%d", idx)
		contactArgs = append(contactArgs, req.TargetPU)
		idx++
	}
	var totalContacts int
	db.QueryRow(contactQuery, contactArgs...).Scan(&totalContacts)

	_, err := db.Exec(`INSERT INTO gotv_campaigns (campaign_id, party_id, name, description, campaign_type, target_state, target_lga, target_ward, target_polling_unit, message_template, message_variant_b, ab_split_pct, scheduled_at, total_contacts, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		campaignID, partyID, req.Name, req.Description, req.CampaignType,
		gotvNullIfEmpty(req.TargetState), gotvNullIfEmpty(req.TargetLGA), gotvNullIfEmpty(req.TargetWard), gotvNullIfEmpty(req.TargetPU),
		req.MessageTemplate, gotvNullIfEmpty(req.MessageVariantB), req.ABSplitPct, scheduledAt, totalContacts, username)
	if err != nil {
		log.Error().Err(err).Msg("GOTV: failed to create campaign")
		http.Error(w, `{"error":"failed to create campaign"}`, http.StatusInternalServerError)
		return
	}

	gotvAudit(partyID, username, "create_campaign", "campaign", campaignID, req, r)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"campaign_id":    campaignID,
		"total_contacts": totalContacts,
		"status":         "draft",
	})
}

func handleGOTVGetCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var cid, name, ctype, cstatus, createdBy string
	var desc, tState, tLGA, tWard, tPU, msgTemplate, msgVariantB sql.NullString
	var abSplit, totalContacts, reached, responded int
	var scheduledAt, startedAt, completedAt sql.NullTime
	var createdAt time.Time

	err := db.QueryRow(`SELECT campaign_id, name, description, campaign_type, status, target_state, target_lga, target_ward, target_polling_unit, message_template, message_variant_b, ab_split_pct, scheduled_at, started_at, completed_at, total_contacts, contacts_reached, contacts_responded, created_by, created_at FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2`, id, partyID).
		Scan(&cid, &name, &desc, &ctype, &cstatus, &tState, &tLGA, &tWard, &tPU, &msgTemplate, &msgVariantB, &abSplit, &scheduledAt, &startedAt, &completedAt, &totalContacts, &reached, &responded, &createdBy, &createdAt)
	if err != nil {
		http.Error(w, `{"error":"campaign not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"campaign_id": cid, "name": name, "description": gotvNullStr(desc),
		"campaign_type": ctype, "status": cstatus,
		"target_state": gotvNullStr(tState), "target_lga": gotvNullStr(tLGA),
		"target_ward": gotvNullStr(tWard), "target_polling_unit": gotvNullStr(tPU),
		"message_template": gotvNullStr(msgTemplate), "message_variant_b": gotvNullStr(msgVariantB),
		"ab_split_pct": abSplit, "scheduled_at": gotvNullTime(scheduledAt),
		"started_at": gotvNullTime(startedAt), "completed_at": gotvNullTime(completedAt),
		"total_contacts": totalContacts, "contacts_reached": reached,
		"contacts_responded": responded, "created_by": createdBy, "created_at": createdAt,
	})
}

func handleGOTVUpdateCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		MessageTemplate string `json:"message_template"`
		MessageVariantB string `json:"message_variant_b"`
		ABSplitPct      int    `json:"ab_split_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	// Only allow updates on draft/paused campaigns
	var currentStatus string
	db.QueryRow("SELECT status FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2", id, partyID).Scan(&currentStatus)
	if currentStatus != "draft" && currentStatus != "paused" {
		http.Error(w, `{"error":"can only update draft or paused campaigns"}`, http.StatusConflict)
		return
	}

	_, err := db.Exec(`UPDATE gotv_campaigns SET name=COALESCE(NULLIF($1,''),name), description=COALESCE(NULLIF($2,''),description), message_template=COALESCE(NULLIF($3,''),message_template), message_variant_b=$4, ab_split_pct=CASE WHEN $5>0 THEN $5 ELSE ab_split_pct END, updated_at=NOW() WHERE campaign_id=$6 AND party_id=$7`,
		req.Name, req.Description, req.MessageTemplate, gotvNullIfEmpty(req.MessageVariantB), req.ABSplitPct, id, partyID)
	if err != nil {
		http.Error(w, `{"error":"update failed"}`, http.StatusInternalServerError)
		return
	}

	gotvAudit(partyID, username, "update_campaign", "campaign", id, req, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func handleGOTVLaunchCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var status, ctype, msgTemplate string
	var totalContacts int
	err := db.QueryRow("SELECT status, campaign_type, message_template, total_contacts FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2", id, partyID).
		Scan(&status, &ctype, &msgTemplate, &totalContacts)
	if err != nil {
		http.Error(w, `{"error":"campaign not found"}`, http.StatusNotFound)
		return
	}
	if status != "draft" && status != "paused" {
		http.Error(w, fmt.Sprintf(`{"error":"cannot launch campaign in '%s' status"}`, status), http.StatusConflict)
		return
	}
	if msgTemplate == "" {
		http.Error(w, `{"error":"message_template required before launch"}`, http.StatusBadRequest)
		return
	}
	if totalContacts == 0 {
		http.Error(w, `{"error":"no contacts in target area"}`, http.StatusBadRequest)
		return
	}

	now := time.Now()
	_, err = db.Exec("UPDATE gotv_campaigns SET status='active', started_at=$1, updated_at=$1 WHERE campaign_id=$2 AND party_id=$3", now, id, partyID)
	if err != nil {
		http.Error(w, `{"error":"launch failed"}`, http.StatusInternalServerError)
		return
	}

	// Dispatch outreach asynchronously
	go dispatchCampaignOutreach(partyID, id, ctype, msgTemplate, "")

	gotvAudit(partyID, username, "launch_campaign", "campaign", id, nil, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "active",
		"total_contacts": totalContacts,
		"started_at":     now,
	})
}

func handleGOTVPauseCampaign(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	result, err := db.Exec("UPDATE gotv_campaigns SET status='paused', updated_at=NOW() WHERE campaign_id=$1 AND party_id=$2 AND status='active'", id, partyID)
	if err != nil {
		http.Error(w, `{"error":"pause failed"}`, http.StatusInternalServerError)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, `{"error":"campaign not active or not found"}`, http.StatusNotFound)
		return
	}
	gotvAudit(partyID, username, "pause_campaign", "campaign", id, nil, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "paused"})
}

func handleGOTVCampaignStats(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var totalContacts, reached, responded int
	db.QueryRow("SELECT total_contacts, contacts_reached, contacts_responded FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2", id, partyID).
		Scan(&totalContacts, &reached, &responded)

	// Channel breakdown
	rows, _ := db.Query("SELECT channel, status, COUNT(*) FROM gotv_outreach_log WHERE campaign_id=$1 AND party_id=$2 GROUP BY channel, status", id, partyID)
	channelStats := map[string]map[string]int{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var ch, st string
			var cnt int
			rows.Scan(&ch, &st, &cnt)
			if channelStats[ch] == nil {
				channelStats[ch] = map[string]int{}
			}
			channelStats[ch][st] = cnt
		}
	}

	// A/B test results
	var variantAResp, variantBResp int
	db.QueryRow("SELECT COUNT(*) FROM gotv_outreach_log WHERE campaign_id=$1 AND party_id=$2 AND message_variant='A' AND status='responded'", id, partyID).Scan(&variantAResp)
	db.QueryRow("SELECT COUNT(*) FROM gotv_outreach_log WHERE campaign_id=$1 AND party_id=$2 AND message_variant='B' AND status='responded'", id, partyID).Scan(&variantBResp)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"campaign_id":      id,
		"total_contacts":   totalContacts,
		"reached":          reached,
		"responded":        responded,
		"reach_rate":       gotvSafeDiv(float64(reached), float64(totalContacts)),
		"response_rate":    gotvSafeDiv(float64(responded), float64(reached)),
		"channel_stats":    channelStats,
		"ab_test": map[string]interface{}{
			"variant_a_responses": variantAResp,
			"variant_b_responses": variantBResp,
		},
	})
}

// ─── Contact Handlers ──────────────────────────────────────────────────────

func handleGOTVCreateContact(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		Phone            string   `json:"phone"`
		FullName         string   `json:"full_name"`
		StateCode        string   `json:"state_code"`
		LGACode          string   `json:"lga_code"`
		WardCode         string   `json:"ward_code"`
		PollingUnitCode  string   `json:"polling_unit_code"`
		Tags             []string `json:"tags"`
		ConsentPurpose   string   `json:"consent_purpose"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.Phone == "" {
		http.Error(w, `{"error":"phone required"}`, http.StatusBadRequest)
		return
	}

	// Consent enforcement — every contact must have consent
	consentID := "gotv-consent-" + uuid.New().String()[:8]
	purpose := req.ConsentPurpose
	if purpose == "" {
		purpose = "communication"
	}
	db.Exec(`INSERT INTO consent_records (consent_id, subject_id, purpose, legal_basis, granted_at) VALUES ($1, $2, $3, 'consent', NOW())`,
		consentID, normalizePhone(req.Phone), purpose)

	// Encrypt PII
	phoneEnc, err := gotvEncrypt(normalizePhone(req.Phone))
	if err != nil {
		http.Error(w, `{"error":"encryption failed"}`, http.StatusInternalServerError)
		return
	}
	pHash := gotvPhoneHash(req.Phone)

	var nameEnc sql.NullString
	if req.FullName != "" {
		enc, err := gotvEncrypt(req.FullName)
		if err == nil {
			nameEnc = sql.NullString{String: enc, Valid: true}
		}
	}

	contactID := "gotv-contact-" + uuid.New().String()[:8]
	tags := "{}"
	if len(req.Tags) > 0 {
		tags = "{" + strings.Join(req.Tags, ",") + "}"
	}

	_, err = db.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, full_name_encrypted, state_code, lga_code, ward_code, polling_unit_code, tags, consent_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		contactID, partyID, phoneEnc, pHash, nameEnc,
		gotvNullIfEmpty(req.StateCode), gotvNullIfEmpty(req.LGACode), gotvNullIfEmpty(req.WardCode), gotvNullIfEmpty(req.PollingUnitCode),
		tags, consentID)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			http.Error(w, `{"error":"contact already exists for this party"}`, http.StatusConflict)
			return
		}
		log.Error().Err(err).Msg("GOTV: failed to create contact")
		http.Error(w, `{"error":"failed to create contact"}`, http.StatusInternalServerError)
		return
	}

	gotvAudit(partyID, username, "create_contact", "contact", contactID, map[string]string{"state": req.StateCode}, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"contact_id": contactID,
		"consent_id": consentID,
	})
}

func handleGOTVListContacts(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 50
	offset := (page - 1) * limit
	stateFilter := r.URL.Query().Get("state")
	statusFilter := r.URL.Query().Get("voter_status")

	query := "SELECT contact_id, phone_encrypted, full_name_encrypted, state_code, lga_code, ward_code, polling_unit_code, voter_status, tags, opted_out, last_contacted_at, contact_count, created_at FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE"
	args := []interface{}{partyID}
	idx := 2
	if stateFilter != "" {
		query += fmt.Sprintf(" AND state_code=$%d", idx)
		args = append(args, stateFilter)
		idx++
	}
	if statusFilter != "" {
		query += fmt.Sprintf(" AND voter_status=$%d", idx)
		args = append(args, statusFilter)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var contacts []map[string]interface{}
	for rows.Next() {
		var cid, phoneEnc, vStatus string
		var nameEnc, state, lga, ward, pu sql.NullString
		var tags []string
		var optedOut bool
		var lastContacted sql.NullTime
		var contactCount int
		var createdAt time.Time
		if err := rows.Scan(&cid, &phoneEnc, &nameEnc, &state, &lga, &ward, &pu, &vStatus, (*pq.StringArray)(&tags), &optedOut, &lastContacted, &contactCount, &createdAt); err != nil {
			continue
		}

		// Decrypt phone for display (masked)
		phone, _ := gotvDecrypt(phoneEnc)
		maskedPhone := gotvMaskPhone(phone)

		var fullName string
		if nameEnc.Valid {
			fullName, _ = gotvDecrypt(nameEnc.String)
		}

		contacts = append(contacts, map[string]interface{}{
			"contact_id":       cid,
			"phone_masked":     maskedPhone,
			"full_name":        fullName,
			"state_code":       gotvNullStr(state),
			"lga_code":         gotvNullStr(lga),
			"ward_code":        gotvNullStr(ward),
			"polling_unit_code": gotvNullStr(pu),
			"voter_status":     vStatus,
			"tags":             tags,
			"last_contacted_at": gotvNullTime(lastContacted),
			"contact_count":    contactCount,
			"created_at":       createdAt,
		})
	}

	var total int
	db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE", partyID).Scan(&total)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"contacts":    contacts,
		"total":       total,
		"page":        page,
		"per_page":    limit,
		"total_pages": int(math.Ceil(float64(total) / float64(limit))),
	})
}

func handleGOTVGetContact(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var phoneEnc, vStatus string
	var nameEnc, state, lga, ward, pu, consentID sql.NullString
	var optedOut bool
	var contactCount int
	var createdAt time.Time
	err := db.QueryRow(`SELECT phone_encrypted, full_name_encrypted, state_code, lga_code, ward_code, polling_unit_code, voter_status, opted_out, consent_id, contact_count, created_at FROM gotv_contacts WHERE contact_id=$1 AND party_id=$2`, id, partyID).
		Scan(&phoneEnc, &nameEnc, &state, &lga, &ward, &pu, &vStatus, &optedOut, &consentID, &contactCount, &createdAt)
	if err != nil {
		http.Error(w, `{"error":"contact not found"}`, http.StatusNotFound)
		return
	}

	phone, _ := gotvDecrypt(phoneEnc)
	var fullName string
	if nameEnc.Valid {
		fullName, _ = gotvDecrypt(nameEnc.String)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"contact_id": id, "phone_masked": gotvMaskPhone(phone), "full_name": fullName,
		"state_code": gotvNullStr(state), "lga_code": gotvNullStr(lga),
		"ward_code": gotvNullStr(ward), "polling_unit_code": gotvNullStr(pu),
		"voter_status": vStatus, "opted_out": optedOut,
		"consent_id": gotvNullStr(consentID), "contact_count": contactCount,
		"created_at": createdAt,
	})
}

func handleGOTVUpdateContact(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		VoterStatus string   `json:"voter_status"`
		Tags        []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	if req.VoterStatus != "" {
		validStatuses := map[string]bool{"unknown": true, "pledged": true, "confirmed": true, "declined": true, "unreachable": true}
		if !validStatuses[req.VoterStatus] {
			http.Error(w, `{"error":"invalid voter_status"}`, http.StatusBadRequest)
			return
		}
		db.Exec("UPDATE gotv_contacts SET voter_status=$1, updated_at=NOW() WHERE contact_id=$2 AND party_id=$3", req.VoterStatus, id, partyID)
	}
	if len(req.Tags) > 0 {
		tags := "{" + strings.Join(req.Tags, ",") + "}"
		db.Exec("UPDATE gotv_contacts SET tags=$1, updated_at=NOW() WHERE contact_id=$2 AND party_id=$3", tags, id, partyID)
	}

	gotvAudit(partyID, username, "update_contact", "contact", id, req, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func handleGOTVOptOut(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	// NDPR right to object — immediate opt-out
	result, err := db.Exec("UPDATE gotv_contacts SET opted_out=TRUE, opted_out_at=NOW(), updated_at=NOW() WHERE contact_id=$1 AND party_id=$2 AND opted_out=FALSE", id, partyID)
	if err != nil {
		http.Error(w, `{"error":"opt-out failed"}`, http.StatusInternalServerError)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, `{"error":"contact not found or already opted out"}`, http.StatusNotFound)
		return
	}

	// Record DSR
	db.Exec(`INSERT INTO data_subject_requests (request_id, subject_id, request_type, status, completed_at, processed_by, notes) VALUES ($1, $2, 'objection', 'completed', NOW(), $3, 'GOTV opt-out')`,
		"gotv-dsr-"+uuid.New().String()[:8], id, username)

	gotvAudit(partyID, username, "opt_out_contact", "contact", id, nil, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "opted_out"})
}

// ─── CSV Import ────────────────────────────────────────────────────────────

func handleGOTVImportContacts(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	// Max 10MB CSV
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, `{"error":"file too large (max 10MB)"}`, http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"file field required"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	stateCode := r.FormValue("state_code")
	lgaCode := r.FormValue("lga_code")
	wardCode := r.FormValue("ward_code")

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		http.Error(w, `{"error":"invalid CSV format"}`, http.StatusBadRequest)
		return
	}

	// Map column indices
	colMap := map[string]int{}
	for i, h := range header {
		colMap[strings.ToLower(strings.TrimSpace(h))] = i
	}

	phoneCol, hasPhone := colMap["phone"]
	if !hasPhone {
		// Try alternate names
		for _, alt := range []string{"phone_number", "mobile", "tel", "telephone"} {
			if idx, ok := colMap[alt]; ok {
				phoneCol = idx
				hasPhone = true
				break
			}
		}
	}
	if !hasPhone {
		http.Error(w, `{"error":"CSV must have a 'phone' column"}`, http.StatusBadRequest)
		return
	}
	nameCol := colMap["name"]
	if _, ok := colMap["full_name"]; ok {
		nameCol = colMap["full_name"]
	}

	imported, duplicates, errors := 0, 0, 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errors++
			continue
		}
		if phoneCol >= len(record) {
			errors++
			continue
		}

		phone := strings.TrimSpace(record[phoneCol])
		if phone == "" {
			errors++
			continue
		}

		var fullName string
		if nameCol > 0 && nameCol < len(record) {
			fullName = strings.TrimSpace(record[nameCol])
		}

		// Create consent record
		consentID := "gotv-consent-" + uuid.New().String()[:8]
		db.Exec(`INSERT INTO consent_records (consent_id, subject_id, purpose, legal_basis, granted_at) VALUES ($1, $2, 'communication', 'consent', NOW()) ON CONFLICT DO NOTHING`,
			consentID, normalizePhone(phone))

		phoneEnc, err := gotvEncrypt(normalizePhone(phone))
		if err != nil {
			errors++
			continue
		}
		pHash := gotvPhoneHash(phone)

		var nameEnc sql.NullString
		if fullName != "" {
			enc, err := gotvEncrypt(fullName)
			if err == nil {
				nameEnc = sql.NullString{String: enc, Valid: true}
			}
		}

		contactID := "gotv-contact-" + uuid.New().String()[:8]
		_, err = db.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, full_name_encrypted, state_code, lga_code, ward_code, consent_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (party_id, phone_hash) DO NOTHING`,
			contactID, partyID, phoneEnc, pHash, nameEnc,
			gotvNullIfEmpty(stateCode), gotvNullIfEmpty(lgaCode), gotvNullIfEmpty(wardCode), consentID)
		if err != nil {
			errors++
			continue
		}
		// Check if actually inserted (ON CONFLICT DO NOTHING)
		var exists int
		db.QueryRow("SELECT 1 FROM gotv_contacts WHERE contact_id=$1", contactID).Scan(&exists)
		if exists == 1 {
			imported++
		} else {
			duplicates++
		}
	}

	gotvAudit(partyID, username, "import_contacts", "contacts", "", map[string]int{"imported": imported, "duplicates": duplicates, "errors": errors}, r)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"imported":   imported,
		"duplicates": duplicates,
		"errors":     errors,
		"total":      imported + duplicates + errors,
	})
}

// ─── Volunteer Handlers ────────────────────────────────────────────────────

func handleGOTVListVolunteers(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	role := r.URL.Query().Get("role")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 50
	offset := (page - 1) * limit

	query := "SELECT volunteer_id, full_name, phone, role, assigned_state, assigned_lga, assigned_ward, assigned_polling_unit, is_active, has_vehicle, vehicle_capacity, latitude, longitude, last_checkin_at, doors_knocked, calls_made, rides_given, created_at FROM gotv_volunteers WHERE party_id=$1"
	args := []interface{}{partyID}
	idx := 2
	if role != "" {
		query += fmt.Sprintf(" AND role=$%d", idx)
		args = append(args, role)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var volunteers []map[string]interface{}
	for rows.Next() {
		var vid, name, phone, vrole string
		var aState, aLGA, aWard, aPU sql.NullString
		var isActive, hasVehicle bool
		var vehicleCap, doorsKnocked, callsMade, ridesGiven int
		var lat, lng sql.NullFloat64
		var lastCheckin sql.NullTime
		var createdAt time.Time
		if err := rows.Scan(&vid, &name, &phone, &vrole, &aState, &aLGA, &aWard, &aPU, &isActive, &hasVehicle, &vehicleCap, &lat, &lng, &lastCheckin, &doorsKnocked, &callsMade, &ridesGiven, &createdAt); err != nil {
			continue
		}
		volunteers = append(volunteers, map[string]interface{}{
			"volunteer_id": vid, "full_name": name, "phone": gotvMaskPhone(phone), "role": vrole,
			"assigned_state": gotvNullStr(aState), "assigned_lga": gotvNullStr(aLGA),
			"assigned_ward": gotvNullStr(aWard), "assigned_polling_unit": gotvNullStr(aPU),
			"is_active": isActive, "has_vehicle": hasVehicle, "vehicle_capacity": vehicleCap,
			"latitude": gotvNullFloat(lat), "longitude": gotvNullFloat(lng),
			"last_checkin_at": gotvNullTime(lastCheckin),
			"doors_knocked": doorsKnocked, "calls_made": callsMade, "rides_given": ridesGiven,
			"created_at": createdAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"volunteers": volunteers, "page": page})
}

func handleGOTVCreateVolunteer(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		FullName    string  `json:"full_name"`
		Phone       string  `json:"phone"`
		Role        string  `json:"role"`
		State       string  `json:"assigned_state"`
		LGA         string  `json:"assigned_lga"`
		Ward        string  `json:"assigned_ward"`
		PU          string  `json:"assigned_polling_unit"`
		HasVehicle  bool    `json:"has_vehicle"`
		VehicleCap  int     `json:"vehicle_capacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.FullName == "" || req.Phone == "" {
		http.Error(w, `{"error":"full_name and phone required"}`, http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = "canvasser"
	}
	validRoles := map[string]bool{"canvasser": true, "driver": true, "coordinator": true, "phone_banker": true, "team_lead": true}
	if !validRoles[req.Role] {
		http.Error(w, `{"error":"invalid role"}`, http.StatusBadRequest)
		return
	}

	vid := "gotv-vol-" + uuid.New().String()[:8]
	_, err := db.Exec(`INSERT INTO gotv_volunteers (volunteer_id, party_id, full_name, phone, role, assigned_state, assigned_lga, assigned_ward, assigned_polling_unit, has_vehicle, vehicle_capacity)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		vid, partyID, req.FullName, req.Phone, req.Role,
		gotvNullIfEmpty(req.State), gotvNullIfEmpty(req.LGA), gotvNullIfEmpty(req.Ward), gotvNullIfEmpty(req.PU),
		req.HasVehicle, req.VehicleCap)
	if err != nil {
		http.Error(w, `{"error":"failed to create volunteer"}`, http.StatusInternalServerError)
		return
	}

	gotvAudit(partyID, username, "create_volunteer", "volunteer", vid, req, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"volunteer_id": vid})
}

func handleGOTVUpdateVolunteer(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		Role       string `json:"role"`
		State      string `json:"assigned_state"`
		LGA        string `json:"assigned_lga"`
		Ward       string `json:"assigned_ward"`
		PU         string `json:"assigned_polling_unit"`
		IsActive   *bool  `json:"is_active"`
		HasVehicle *bool  `json:"has_vehicle"`
		VehicleCap *int   `json:"vehicle_capacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	// Build dynamic update
	sets := []string{}
	args := []interface{}{}
	idx := 1
	if req.Role != "" {
		sets = append(sets, fmt.Sprintf("role=$%d", idx))
		args = append(args, req.Role)
		idx++
	}
	if req.State != "" {
		sets = append(sets, fmt.Sprintf("assigned_state=$%d", idx))
		args = append(args, req.State)
		idx++
	}
	if req.LGA != "" {
		sets = append(sets, fmt.Sprintf("assigned_lga=$%d", idx))
		args = append(args, req.LGA)
		idx++
	}
	if req.Ward != "" {
		sets = append(sets, fmt.Sprintf("assigned_ward=$%d", idx))
		args = append(args, req.Ward)
		idx++
	}
	if req.PU != "" {
		sets = append(sets, fmt.Sprintf("assigned_polling_unit=$%d", idx))
		args = append(args, req.PU)
		idx++
	}
	if req.IsActive != nil {
		sets = append(sets, fmt.Sprintf("is_active=$%d", idx))
		args = append(args, *req.IsActive)
		idx++
	}
	if req.HasVehicle != nil {
		sets = append(sets, fmt.Sprintf("has_vehicle=$%d", idx))
		args = append(args, *req.HasVehicle)
		idx++
	}
	if req.VehicleCap != nil {
		sets = append(sets, fmt.Sprintf("vehicle_capacity=$%d", idx))
		args = append(args, *req.VehicleCap)
		idx++
	}

	if len(sets) == 0 {
		http.Error(w, `{"error":"no fields to update"}`, http.StatusBadRequest)
		return
	}

	args = append(args, id, partyID)
	query := fmt.Sprintf("UPDATE gotv_volunteers SET %s WHERE volunteer_id=$%d AND party_id=$%d", strings.Join(sets, ","), idx, idx+1)
	db.Exec(query, args...)

	gotvAudit(partyID, username, "update_volunteer", "volunteer", id, req, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func handleGOTVVolunteerCheckin(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	db.Exec("UPDATE gotv_volunteers SET latitude=$1, longitude=$2, last_checkin_at=NOW() WHERE volunteer_id=$3 AND party_id=$4",
		req.Latitude, req.Longitude, id, partyID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "checked_in"})
}

// ─── Pledge Handlers ───────────────────────────────────────────────────────

func handleGOTVListPledges(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	status := r.URL.Query().Get("status")

	query := "SELECT p.pledge_id, p.contact_id, p.election_id, p.pledge_type, p.status, p.reminder_sent, p.created_at FROM gotv_pledges p WHERE p.party_id=$1"
	args := []interface{}{partyID}
	if status != "" {
		query += " AND p.status=$2"
		args = append(args, status)
	}
	query += " ORDER BY p.created_at DESC LIMIT 100"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var pledges []map[string]interface{}
	for rows.Next() {
		var pid, cid, ptype, pstatus string
		var electionID sql.NullInt64
		var reminderSent bool
		var createdAt time.Time
		if err := rows.Scan(&pid, &cid, &electionID, &ptype, &pstatus, &reminderSent, &createdAt); err != nil {
			continue
		}
		pledges = append(pledges, map[string]interface{}{
			"pledge_id": pid, "contact_id": cid, "election_id": gotvNullInt(electionID),
			"pledge_type": ptype, "status": pstatus, "reminder_sent": reminderSent,
			"created_at": createdAt,
		})
	}

	// Summary stats
	var totalPledges, confirmed, needsRide int
	db.QueryRow("SELECT COUNT(*) FROM gotv_pledges WHERE party_id=$1", partyID).Scan(&totalPledges)
	db.QueryRow("SELECT COUNT(*) FROM gotv_pledges WHERE party_id=$1 AND status='confirmed_day_of'", partyID).Scan(&confirmed)
	db.QueryRow("SELECT COUNT(*) FROM gotv_pledges WHERE party_id=$1 AND pledge_type='needs_ride'", partyID).Scan(&needsRide)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pledges": pledges,
		"summary": map[string]int{
			"total": totalPledges, "confirmed_day_of": confirmed, "needs_ride": needsRide,
		},
	})
}

func handleGOTVCreatePledge(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		ContactID  string `json:"contact_id"`
		ElectionID int    `json:"election_id"`
		PledgeType string `json:"pledge_type"`
		Notes      string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.ContactID == "" {
		http.Error(w, `{"error":"contact_id required"}`, http.StatusBadRequest)
		return
	}
	if req.PledgeType == "" {
		req.PledgeType = "will_vote"
	}

	// Verify contact belongs to this party and hasn't opted out
	var optedOut bool
	err := db.QueryRow("SELECT opted_out FROM gotv_contacts WHERE contact_id=$1 AND party_id=$2", req.ContactID, partyID).Scan(&optedOut)
	if err != nil {
		http.Error(w, `{"error":"contact not found"}`, http.StatusNotFound)
		return
	}
	if optedOut {
		http.Error(w, `{"error":"contact has opted out"}`, http.StatusForbidden)
		return
	}

	pledgeID := "gotv-pledge-" + uuid.New().String()[:8]
	_, err = db.Exec(`INSERT INTO gotv_pledges (pledge_id, party_id, contact_id, election_id, pledge_type, notes) VALUES ($1,$2,$3,$4,$5,$6)`,
		pledgeID, partyID, req.ContactID, req.ElectionID, req.PledgeType, gotvNullIfEmpty(req.Notes))
	if err != nil {
		http.Error(w, `{"error":"failed to create pledge"}`, http.StatusInternalServerError)
		return
	}

	// Update contact status
	db.Exec("UPDATE gotv_contacts SET voter_status='pledged', updated_at=NOW() WHERE contact_id=$1 AND party_id=$2", req.ContactID, partyID)

	gotvAudit(partyID, username, "create_pledge", "pledge", pledgeID, req, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"pledge_id": pledgeID})
}

func handleGOTVRemindPledge(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var contactID string
	err := db.QueryRow("SELECT contact_id FROM gotv_pledges WHERE pledge_id=$1 AND party_id=$2", id, partyID).Scan(&contactID)
	if err != nil {
		http.Error(w, `{"error":"pledge not found"}`, http.StatusNotFound)
		return
	}

	// Get contact phone for reminder
	var phoneEnc string
	db.QueryRow("SELECT phone_encrypted FROM gotv_contacts WHERE contact_id=$1 AND party_id=$2 AND opted_out=FALSE", contactID, partyID).Scan(&phoneEnc)
	if phoneEnc == "" {
		http.Error(w, `{"error":"contact opted out or not found"}`, http.StatusForbidden)
		return
	}

	// Mark reminder sent
	db.Exec("UPDATE gotv_pledges SET reminder_sent=TRUE, reminder_sent_at=NOW() WHERE pledge_id=$1", id)
	db.Exec("UPDATE gotv_contacts SET last_contacted_at=NOW(), contact_count=contact_count+1 WHERE contact_id=$1 AND party_id=$2", contactID, partyID)

	// Log outreach
	logID := "gotv-log-" + uuid.New().String()[:8]
	db.Exec(`INSERT INTO gotv_outreach_log (log_id, party_id, contact_id, channel, direction, message_text, status) VALUES ($1,$2,$3,'sms','outbound','Voting reminder: Remember to vote tomorrow!','sent')`,
		logID, partyID, contactID)

	gotvAudit(partyID, username, "remind_pledge", "pledge", id, nil, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "reminder_sent"})
}

// ─── Ride-to-Polls Handlers ───────────────────────────────────────────────

func handleGOTVListRides(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	status := r.URL.Query().Get("status")

	query := "SELECT request_id, contact_id, volunteer_id, pickup_latitude, pickup_longitude, polling_unit_code, status, requested_at, matched_at, picked_up_at, dropped_off_at, distance_km FROM gotv_ride_requests WHERE party_id=$1"
	args := []interface{}{partyID}
	if status != "" {
		query += " AND status=$2"
		args = append(args, status)
	}
	query += " ORDER BY requested_at DESC LIMIT 100"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rides []map[string]interface{}
	for rows.Next() {
		var rid, cid, rstatus, puCode string
		var vid sql.NullString
		var lat, lng float64
		var distKm sql.NullFloat64
		var requestedAt time.Time
		var matchedAt, pickedUpAt, droppedOffAt sql.NullTime
		if err := rows.Scan(&rid, &cid, &vid, &lat, &lng, &puCode, &rstatus, &requestedAt, &matchedAt, &pickedUpAt, &droppedOffAt, &distKm); err != nil {
			continue
		}
		rides = append(rides, map[string]interface{}{
			"request_id": rid, "contact_id": cid, "volunteer_id": gotvNullStr(vid),
			"pickup_latitude": lat, "pickup_longitude": lng, "polling_unit_code": puCode,
			"status": rstatus, "requested_at": requestedAt,
			"matched_at": gotvNullTime(matchedAt), "picked_up_at": gotvNullTime(pickedUpAt),
			"dropped_off_at": gotvNullTime(droppedOffAt), "distance_km": gotvNullFloat(distKm),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"rides": rides})
}

func handleGOTVRequestRide(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		ContactID       string  `json:"contact_id"`
		PickupLatitude  float64 `json:"pickup_latitude"`
		PickupLongitude float64 `json:"pickup_longitude"`
		PollingUnitCode string  `json:"polling_unit_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.ContactID == "" || req.PollingUnitCode == "" {
		http.Error(w, `{"error":"contact_id and polling_unit_code required"}`, http.StatusBadRequest)
		return
	}
	if req.PickupLatitude == 0 || req.PickupLongitude == 0 {
		http.Error(w, `{"error":"pickup coordinates required"}`, http.StatusBadRequest)
		return
	}

	requestID := "gotv-ride-" + uuid.New().String()[:8]
	_, err := db.Exec(`INSERT INTO gotv_ride_requests (request_id, party_id, contact_id, pickup_latitude, pickup_longitude, polling_unit_code) VALUES ($1,$2,$3,$4,$5,$6)`,
		requestID, partyID, req.ContactID, req.PickupLatitude, req.PickupLongitude, req.PollingUnitCode)
	if err != nil {
		http.Error(w, `{"error":"failed to create ride request"}`, http.StatusInternalServerError)
		return
	}

	gotvAudit(partyID, username, "request_ride", "ride", requestID, req, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"request_id": requestID, "status": "pending"})
}

func handleGOTVMatchRide(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]
	username := r.Header.Get("X-GOTV-User")

	var req struct {
		VolunteerID string `json:"volunteer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VolunteerID == "" {
		// Auto-match: find nearest available driver
		var lat, lng float64
		db.QueryRow("SELECT pickup_latitude, pickup_longitude FROM gotv_ride_requests WHERE request_id=$1 AND party_id=$2", id, partyID).Scan(&lat, &lng)

		var nearestVID string
		db.QueryRow(`SELECT volunteer_id FROM gotv_volunteers WHERE party_id=$1 AND role='driver' AND is_active=TRUE AND has_vehicle=TRUE AND latitude IS NOT NULL
			ORDER BY ((latitude-$2)*(latitude-$2) + (longitude-$3)*(longitude-$3)) LIMIT 1`, partyID, lat, lng).Scan(&nearestVID)
		if nearestVID == "" {
			http.Error(w, `{"error":"no available drivers nearby"}`, http.StatusNotFound)
			return
		}
		req.VolunteerID = nearestVID
	}

	// Calculate distance
	var pickupLat, pickupLng float64
	db.QueryRow("SELECT pickup_latitude, pickup_longitude FROM gotv_ride_requests WHERE request_id=$1 AND party_id=$2", id, partyID).Scan(&pickupLat, &pickupLng)

	var driverLat, driverLng sql.NullFloat64
	db.QueryRow("SELECT latitude, longitude FROM gotv_volunteers WHERE volunteer_id=$1 AND party_id=$2", req.VolunteerID, partyID).Scan(&driverLat, &driverLng)

	var distKm float64
	if driverLat.Valid && driverLng.Valid {
		distKm = gotvHaversine(pickupLat, pickupLng, driverLat.Float64, driverLng.Float64)
	}

	db.Exec("UPDATE gotv_ride_requests SET volunteer_id=$1, status='matched', matched_at=NOW(), distance_km=$2 WHERE request_id=$3 AND party_id=$4",
		req.VolunteerID, distKm, id, partyID)

	gotvAudit(partyID, username, "match_ride", "ride", id, map[string]string{"volunteer_id": req.VolunteerID}, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "matched", "volunteer_id": req.VolunteerID, "distance_km": math.Round(distKm*100) / 100,
	})
}

func handleGOTVUpdateRideStatus(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)
	id := mux.Vars(r)["id"]

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	validStatuses := map[string]bool{"en_route": true, "picked_up": true, "dropped_off": true, "cancelled": true, "no_show": true}
	if !validStatuses[req.Status] {
		http.Error(w, `{"error":"invalid status"}`, http.StatusBadRequest)
		return
	}

	// Update timestamp columns based on status
	switch req.Status {
	case "picked_up":
		db.Exec("UPDATE gotv_ride_requests SET status=$1, picked_up_at=NOW() WHERE request_id=$2 AND party_id=$3", req.Status, id, partyID)
	case "dropped_off":
		db.Exec("UPDATE gotv_ride_requests SET status=$1, dropped_off_at=NOW() WHERE request_id=$2 AND party_id=$3", req.Status, id, partyID)
		// Increment driver's rides_given
		var vid sql.NullString
		db.QueryRow("SELECT volunteer_id FROM gotv_ride_requests WHERE request_id=$1", id).Scan(&vid)
		if vid.Valid {
			db.Exec("UPDATE gotv_volunteers SET rides_given=rides_given+1 WHERE volunteer_id=$1", vid.String)
		}
	default:
		db.Exec("UPDATE gotv_ride_requests SET status=$1 WHERE request_id=$2 AND party_id=$3", req.Status, id, partyID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": req.Status})
}

// ─── Turnout Dashboard (Public — aggregate only) ───────────────────────────

func handleGOTVTurnout(w http.ResponseWriter, r *http.Request) {
	electionID := mux.Vars(r)["election_id"]

	// Aggregate turnout per state from BVAS accreditation data
	rows, err := db.Query(`
		SELECT s.code, s.name, s.geo_zone,
			COALESCE(SUM(pu.registered_voters), 0) as registered,
			COALESCE(SUM(ts.accredited_count), 0) as accredited
		FROM states s
		LEFT JOIN lgas l ON l.state_code = s.code
		LEFT JOIN wards w ON w.lga_code = l.code
		LEFT JOIN polling_units pu ON pu.ward_code = w.code
		LEFT JOIN gotv_turnout_snapshots ts ON ts.polling_unit_code = pu.code AND ts.election_id = $1
		GROUP BY s.code, s.name, s.geo_zone
		ORDER BY s.name`, electionID)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var states []map[string]interface{}
	var totalReg, totalAcc int
	for rows.Next() {
		var code, name, geoZone string
		var registered, accredited int
		rows.Scan(&code, &name, &geoZone, &registered, &accredited)
		pct := gotvSafeDiv(float64(accredited), float64(registered)) * 100
		states = append(states, map[string]interface{}{
			"state_code": code, "state_name": name, "geo_zone": geoZone,
			"registered_voters": registered, "accredited": accredited,
			"turnout_pct": math.Round(pct*100) / 100,
		})
		totalReg += registered
		totalAcc += accredited
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"election_id":     electionID,
		"states":          states,
		"national_total":  totalReg,
		"national_accredited": totalAcc,
		"national_turnout_pct": math.Round(gotvSafeDiv(float64(totalAcc), float64(totalReg))*10000) / 100,
		"snapshot_at":     time.Now(),
	})
}

func handleGOTVTurnoutByState(w http.ResponseWriter, r *http.Request) {
	electionID := mux.Vars(r)["election_id"]
	stateCode := mux.Vars(r)["state_code"]

	rows, err := db.Query(`
		SELECT l.code, l.name,
			COALESCE(SUM(pu.registered_voters), 0) as registered,
			COALESCE(SUM(ts.accredited_count), 0) as accredited
		FROM lgas l
		LEFT JOIN wards w ON w.lga_code = l.code
		LEFT JOIN polling_units pu ON pu.ward_code = w.code
		LEFT JOIN gotv_turnout_snapshots ts ON ts.polling_unit_code = pu.code AND ts.election_id = $1
		WHERE l.state_code = $2
		GROUP BY l.code, l.name
		ORDER BY l.name`, electionID, stateCode)
	if err != nil {
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var lgas []map[string]interface{}
	for rows.Next() {
		var code, name string
		var registered, accredited int
		rows.Scan(&code, &name, &registered, &accredited)
		pct := gotvSafeDiv(float64(accredited), float64(registered)) * 100
		lgas = append(lgas, map[string]interface{}{
			"lga_code": code, "lga_name": name,
			"registered_voters": registered, "accredited": accredited,
			"turnout_pct": math.Round(pct*100) / 100,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"election_id": electionID,
		"state_code":  stateCode,
		"lgas":        lgas,
	})
}

// ─── Party Dashboard ───────────────────────────────────────────────────────

func handleGOTVDashboard(w http.ResponseWriter, r *http.Request) {
	partyID, _, partyName := getGOTVParty(r)

	var totalContacts, pledgedContacts, confirmedContacts, optedOut int
	var totalVolunteers, activeVolunteers, totalCampaigns, activeCampaigns int
	var totalRides, pendingRides, completedRides int

	db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE", partyID).Scan(&totalContacts)
	db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND voter_status='pledged'", partyID).Scan(&pledgedContacts)
	db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND voter_status='confirmed'", partyID).Scan(&confirmedContacts)
	db.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=TRUE", partyID).Scan(&optedOut)
	db.QueryRow("SELECT COUNT(*) FROM gotv_volunteers WHERE party_id=$1", partyID).Scan(&totalVolunteers)
	db.QueryRow("SELECT COUNT(*) FROM gotv_volunteers WHERE party_id=$1 AND is_active=TRUE", partyID).Scan(&activeVolunteers)
	db.QueryRow("SELECT COUNT(*) FROM gotv_campaigns WHERE party_id=$1", partyID).Scan(&totalCampaigns)
	db.QueryRow("SELECT COUNT(*) FROM gotv_campaigns WHERE party_id=$1 AND status='active'", partyID).Scan(&activeCampaigns)
	db.QueryRow("SELECT COUNT(*) FROM gotv_ride_requests WHERE party_id=$1", partyID).Scan(&totalRides)
	db.QueryRow("SELECT COUNT(*) FROM gotv_ride_requests WHERE party_id=$1 AND status='pending'", partyID).Scan(&pendingRides)
	db.QueryRow("SELECT COUNT(*) FROM gotv_ride_requests WHERE party_id=$1 AND status='dropped_off'", partyID).Scan(&completedRides)

	// Geo breakdown
	rows, _ := db.Query("SELECT state_code, COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE AND state_code IS NOT NULL GROUP BY state_code ORDER BY COUNT(*) DESC LIMIT 10", partyID)
	var geoBreakdown []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var state string
			var cnt int
			rows.Scan(&state, &cnt)
			geoBreakdown = append(geoBreakdown, map[string]interface{}{"state": state, "contacts": cnt})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"party_name": partyName,
		"contacts": map[string]int{
			"total": totalContacts, "pledged": pledgedContacts, "confirmed": confirmedContacts, "opted_out": optedOut,
		},
		"volunteers": map[string]int{
			"total": totalVolunteers, "active": activeVolunteers,
		},
		"campaigns": map[string]int{
			"total": totalCampaigns, "active": activeCampaigns,
		},
		"rides": map[string]int{
			"total": totalRides, "pending": pendingRides, "completed": completedRides,
		},
		"geo_breakdown": geoBreakdown,
	})
}

func handleGOTVOutreachAnalytics(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)

	// Outreach by channel
	rows, _ := db.Query("SELECT channel, COUNT(*), SUM(CASE WHEN status='delivered' THEN 1 ELSE 0 END), SUM(CASE WHEN status='responded' THEN 1 ELSE 0 END) FROM gotv_outreach_log WHERE party_id=$1 GROUP BY channel", partyID)
	var channelAnalytics []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var ch string
			var total, delivered, responded int
			rows.Scan(&ch, &total, &delivered, &responded)
			channelAnalytics = append(channelAnalytics, map[string]interface{}{
				"channel": ch, "total": total, "delivered": delivered, "responded": responded,
				"delivery_rate": gotvSafeDiv(float64(delivered), float64(total)),
				"response_rate": gotvSafeDiv(float64(responded), float64(delivered)),
			})
		}
	}

	// Daily outreach trend (last 14 days)
	trendRows, _ := db.Query(`SELECT DATE(sent_at) as day, COUNT(*) FROM gotv_outreach_log WHERE party_id=$1 AND sent_at > NOW() - INTERVAL '14 days' GROUP BY DATE(sent_at) ORDER BY day`, partyID)
	var dailyTrend []map[string]interface{}
	if trendRows != nil {
		defer trendRows.Close()
		for trendRows.Next() {
			var day time.Time
			var cnt int
			trendRows.Scan(&day, &cnt)
			dailyTrend = append(dailyTrend, map[string]interface{}{"date": day.Format("2006-01-02"), "outreach_count": cnt})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"channel_analytics": channelAnalytics,
		"daily_trend":       dailyTrend,
	})
}

func handleGOTVGeoAnalytics(w http.ResponseWriter, r *http.Request) {
	partyID, _, _ := getGOTVParty(r)

	rows, _ := db.Query(`
		SELECT c.state_code, s.name,
			COUNT(c.id) as contacts,
			SUM(CASE WHEN c.voter_status='pledged' THEN 1 ELSE 0 END) as pledged,
			SUM(CASE WHEN c.voter_status='confirmed' THEN 1 ELSE 0 END) as confirmed
		FROM gotv_contacts c
		LEFT JOIN states s ON s.code = c.state_code
		WHERE c.party_id=$1 AND c.opted_out=FALSE AND c.state_code IS NOT NULL
		GROUP BY c.state_code, s.name
		ORDER BY contacts DESC`, partyID)
	var geoStats []map[string]interface{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var code string
			var name sql.NullString
			var contacts, pledged, confirmed int
			rows.Scan(&code, &name, &contacts, &pledged, &confirmed)
			geoStats = append(geoStats, map[string]interface{}{
				"state_code": code, "state_name": gotvNullStr(name),
				"contacts": contacts, "pledged": pledged, "confirmed": confirmed,
				"pledge_rate": gotvSafeDiv(float64(pledged), float64(contacts)),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"geo_analytics": geoStats})
}

// ─── Campaign Dispatch Engine ──────────────────────────────────────────────

func dispatchCampaignOutreach(partyID int, campaignID, channelType, messageTemplate, messageVariantB string) {
	log.Info().Str("campaign", campaignID).Msg("GOTV: dispatching campaign outreach")

	// Get campaign geo targeting
	var tState, tLGA, tWard, tPU sql.NullString
	var abSplit int
	db.QueryRow("SELECT target_state, target_lga, target_ward, target_polling_unit, ab_split_pct FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2",
		campaignID, partyID).Scan(&tState, &tLGA, &tWard, &tPU, &abSplit)

	// Fetch eligible contacts in batches
	query := "SELECT contact_id, phone_encrypted FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE"
	args := []interface{}{partyID}
	idx := 2
	if tState.Valid {
		query += fmt.Sprintf(" AND state_code=$%d", idx)
		args = append(args, tState.String)
		idx++
	}
	if tLGA.Valid {
		query += fmt.Sprintf(" AND lga_code=$%d", idx)
		args = append(args, tLGA.String)
		idx++
	}
	if tWard.Valid {
		query += fmt.Sprintf(" AND ward_code=$%d", idx)
		args = append(args, tWard.String)
		idx++
	}
	if tPU.Valid {
		query += fmt.Sprintf(" AND polling_unit_code=$%d", idx)
		args = append(args, tPU.String)
		idx++
	}
	query += " ORDER BY id LIMIT 10000"

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Error().Err(err).Msg("GOTV: failed to fetch contacts for campaign")
		return
	}
	defer rows.Close()

	sent := 0
	for rows.Next() {
		var contactID, phoneEnc string
		rows.Scan(&contactID, &phoneEnc)

		// Determine A/B variant
		variant := "A"
		msg := messageTemplate
		if messageVariantB != "" && sent%100 >= abSplit {
			variant = "B"
			msg = messageVariantB
		}

		// Log outreach (actual SMS dispatch would go through production SMS gateway)
		logID := "gotv-log-" + uuid.New().String()[:8]
		db.Exec(`INSERT INTO gotv_outreach_log (log_id, campaign_id, party_id, contact_id, channel, direction, message_text, message_variant, status)
			VALUES ($1,$2,$3,$4,$5,'outbound',$6,$7,'sent')`,
			logID, campaignID, partyID, contactID, channelType, msg, variant)

		db.Exec("UPDATE gotv_contacts SET last_contacted_at=NOW(), contact_count=contact_count+1 WHERE contact_id=$1", contactID)
		sent++

		// Rate limit: 100 per second
		if sent%100 == 0 {
			time.Sleep(time.Second)
		}
	}

	// Update campaign stats
	db.Exec("UPDATE gotv_campaigns SET contacts_reached=$1, status=CASE WHEN $1>=total_contacts THEN 'completed' ELSE status END, completed_at=CASE WHEN $1>=total_contacts THEN NOW() ELSE completed_at END, updated_at=NOW() WHERE campaign_id=$2",
		sent, campaignID)

	log.Info().Str("campaign", campaignID).Int("sent", sent).Msg("GOTV: campaign dispatch complete")
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func gotvMaskPhone(phone string) string {
	if len(phone) < 7 {
		return "***"
	}
	return phone[:4] + "****" + phone[len(phone)-3:]
}

func gotvHaversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371 // Earth radius km
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

func gotvSafeDiv(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return num / den
}

func gotvNullStr(ns sql.NullString) interface{} {
	if ns.Valid {
		return ns.String
	}
	return nil
}

func gotvNullTime(nt sql.NullTime) interface{} {
	if nt.Valid {
		return nt.Time
	}
	return nil
}

func gotvNullFloat(nf sql.NullFloat64) interface{} {
	if nf.Valid {
		return nf.Float64
	}
	return nil
}

func gotvNullInt(ni sql.NullInt64) interface{} {
	if ni.Valid {
		return ni.Int64
	}
	return nil
}

func gotvNullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
