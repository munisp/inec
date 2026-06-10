// GOTV Service — independently deployable voter mobilization service.
// Handles: party campaigns, contacts, volunteers, pledges, ride-to-polls, turnout.
// Every endpoint is party-scoped with row-level isolation.
//
// Usage:
//   go run ./cmd/gotv-svc --port=8097 --db=postgres://...
package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"inec-go-backend/internal/gotv"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	svc        *gotv.Service
	wsHub      *gotv.WSHub
	dispatcher *gotv.DispatchEngine
	webhooks   *gotv.WebhookManager
	authMid    *gotv.AuthMiddleware
)

func main() {
	port := flag.Int("port", 8103, "HTTP port")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
	encKey := flag.String("enc-key", os.Getenv("GOTV_ENCRYPTION_KEY"), "AES-256 encryption key (64 hex chars)")
	authURL := flag.String("auth-url", os.Getenv("AUTH_SERVICE_URL"), "auth-svc base URL for JWT validation")
	devMode := flag.Bool("dev", os.Getenv("GOTV_DEV_MODE") == "true", "enable dev mode (relaxed auth)")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	if *dbURL == "" {
		*dbURL = "postgres://ngapp:ngapp123@localhost:5432/ngapp?sslmode=disable"
	}

	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatal().Err(err).Msg("Database ping failed")
	}

	svc = gotv.NewService(db, *encKey)
	if err := svc.InitTables(context.Background()); err != nil {
		log.Warn().Err(err).Msg("GOTV table init had issues (non-fatal if tables exist)")
	}

	// Initialize subsystems
	wsHub = gotv.NewWSHub(10000, 5)
	go wsHub.Run()

	dispatcher = gotv.NewDispatchEngine(db, wsHub, 10)
	dispatcher.RegisterAdapter(&gotv.LogAdapter{}) // default; real adapters configured via env
	if smsKey := os.Getenv("AFRICASTALKING_API_KEY"); smsKey != "" {
		dispatcher.RegisterAdapter(gotv.NewSMSAdapter("africastalking",
			"https://api.africastalking.com/version1", smsKey, os.Getenv("AFRICASTALKING_SENDER")))
	}
	if pushKey := os.Getenv("FCM_SERVER_KEY"); pushKey != "" {
		dispatcher.RegisterAdapter(gotv.NewPushAdapter(pushKey, os.Getenv("FCM_PROJECT_ID")))
	}
	if waToken := os.Getenv("WHATSAPP_TOKEN"); waToken != "" {
		dispatcher.RegisterAdapter(gotv.NewWhatsAppAdapter(
			"https://graph.facebook.com/v18.0", waToken, os.Getenv("WHATSAPP_PHONE_ID")))
	}

	webhooks = gotv.NewWebhookManager(db)
	if err := webhooks.InitTables(context.Background()); err != nil {
		log.Warn().Err(err).Msg("Webhook table init had issues")
	}
	webhookCtx, webhookCancel := context.WithCancel(context.Background())
	defer webhookCancel()
	webhooks.StartRetryWorker(webhookCtx)

	authMid = gotv.NewAuthMiddleware(db, gotv.AuthConfig{
		AuthServiceURL: *authURL,
		DevMode:        *devMode,
	})
	auth := authMid.Wrap // shorthand

	r := mux.NewRouter()

	// Health
	r.HandleFunc("/health", handleHealth).Methods("GET")

	// WebSocket (real-time events)
	r.HandleFunc("/gotv/ws", handleWebSocket).Methods("GET")

	// Campaigns
	r.HandleFunc("/gotv/campaigns", auth(handleListCampaigns)).Methods("GET")
	r.HandleFunc("/gotv/campaigns", auth(handleCreateCampaign)).Methods("POST")
	r.HandleFunc("/gotv/campaigns/{id}", auth(handleGetCampaign)).Methods("GET")
	r.HandleFunc("/gotv/campaigns/{id}", auth(handleUpdateCampaign)).Methods("PATCH")
	r.HandleFunc("/gotv/campaigns/{id}/launch", auth(handleLaunchCampaign)).Methods("POST")

	// Contacts
	r.HandleFunc("/gotv/contacts", auth(handleListContacts)).Methods("GET")
	r.HandleFunc("/gotv/contacts", auth(handleCreateContact)).Methods("POST")
	r.HandleFunc("/gotv/contacts/import", auth(handleImportContacts)).Methods("POST")
	r.HandleFunc("/gotv/contacts/{id}", auth(handleGetContact)).Methods("GET")
	r.HandleFunc("/gotv/contacts/{id}/opt-out", auth(handleOptOut)).Methods("POST")

	// Volunteers
	r.HandleFunc("/gotv/volunteers", auth(handleListVolunteers)).Methods("GET")
	r.HandleFunc("/gotv/volunteers", auth(handleCreateVolunteer)).Methods("POST")
	r.HandleFunc("/gotv/volunteers/{id}/checkin", auth(handleVolunteerCheckin)).Methods("POST")
	r.HandleFunc("/gotv/volunteers/{id}/location", auth(handleVolunteerLocation)).Methods("POST")

	// Pledges
	r.HandleFunc("/gotv/pledges", auth(handleListPledges)).Methods("GET")
	r.HandleFunc("/gotv/pledges", auth(handleCreatePledge)).Methods("POST")
	r.HandleFunc("/gotv/pledges/{id}", auth(handleUpdatePledge)).Methods("PATCH")

	// Ride-to-polls
	r.HandleFunc("/gotv/rides", auth(handleListRides)).Methods("GET")
	r.HandleFunc("/gotv/rides", auth(handleCreateRide)).Methods("POST")
	r.HandleFunc("/gotv/rides/{id}/match", auth(handleMatchRide)).Methods("POST")
	r.HandleFunc("/gotv/rides/{id}/status", auth(handleUpdateRideStatus)).Methods("PATCH")

	// Webhooks
	r.HandleFunc("/gotv/webhooks", auth(handleListWebhooks)).Methods("GET")
	r.HandleFunc("/gotv/webhooks", auth(handleCreateWebhook)).Methods("POST")
	r.HandleFunc("/gotv/webhooks/{id}", auth(handleDeleteWebhook)).Methods("DELETE")

	// Geospatial data endpoints (for map visualization)
	r.HandleFunc("/gotv/geo/volunteers", auth(handleGeoVolunteers)).Methods("GET")
	r.HandleFunc("/gotv/geo/rides", auth(handleGeoRides)).Methods("GET")
	r.HandleFunc("/gotv/geo/coverage", auth(handleGeoCoverage)).Methods("GET")
	r.HandleFunc("/gotv/geo/canvass-trails", auth(handleGeoCanvassTrails)).Methods("GET")

	// Canvasser field workflow
	r.HandleFunc("/gotv/canvass/walklist", auth(handleCanvassWalklist)).Methods("GET")
	r.HandleFunc("/gotv/canvass/knock", auth(handleCanvassDoorKnock)).Methods("POST")
	r.HandleFunc("/gotv/canvass/shift/start", auth(handleCanvassShiftStart)).Methods("POST")
	r.HandleFunc("/gotv/canvass/shift/end", auth(handleCanvassShiftEnd)).Methods("POST")

	// Turnout (public aggregate data)
	r.HandleFunc("/gotv/turnout/{election_id}", handleTurnout).Methods("GET")

	// Dashboard
	r.HandleFunc("/gotv/dashboard", auth(handleDashboard)).Methods("GET")

	addr := fmt.Sprintf(":%d", *port)
	srv := &http.Server{Addr: addr, Handler: r, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}

	go func() {
		log.Info().Str("addr", addr).Msg("GOTV service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("GOTV service shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// ─── WebSocket Handler ─────────────────────────────────────────────────────

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	partyIDStr := r.URL.Query().Get("party_id")
	partyID, _ := strconv.Atoi(partyIDStr)
	if partyID == 0 {
		partyID = 1
	}
	wsHub.HandleWS(w, r, partyID)
}

func getParty(r *http.Request) (int, string) {
	pid, _ := strconv.Atoi(r.Header.Get("X-GOTV-Party-ID"))
	return pid, r.Header.Get("X-GOTV-User")
}

// ─── Health ────────────────────────────────────────────────────────────────

func handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, map[string]interface{}{
		"service": "gotv-svc",
		"status":  "healthy",
		"version": "1.0.0",
	})
}

// ─── Campaigns ─────────────────────────────────────────────────────────────

func handleListCampaigns(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	status := r.URL.Query().Get("status")

	query := "SELECT campaign_id, name, campaign_type, status, target_state, total_contacts, contacts_reached, created_by, created_at FROM gotv_campaigns WHERE party_id=$1"
	args := []interface{}{pid}
	if status != "" {
		query += " AND status=$2"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC LIMIT 100"

	rows, err := svc.DB.Query(query, args...)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	campaigns := []map[string]interface{}{}
	for rows.Next() {
		var cid, name, ctype, cstatus, createdBy string
		var tState sql.NullString
		var totalContacts, reached int
		var createdAt time.Time
		if err := rows.Scan(&cid, &name, &ctype, &cstatus, &tState, &totalContacts, &reached, &createdBy, &createdAt); err != nil {
			continue
		}
		campaigns = append(campaigns, map[string]interface{}{
			"campaign_id":     cid,
			"name":            name,
			"campaign_type":   ctype,
			"status":          cstatus,
			"target_state":    nullVal(tState),
			"total_contacts":  totalContacts,
			"contacts_reached": reached,
			"created_by":      createdBy,
			"created_at":      createdAt,
		})
	}
	jsonResp(w, map[string]interface{}{"campaigns": campaigns, "total": len(campaigns)})
}

func handleCreateCampaign(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		Name            string `json:"name"`
		CampaignType    string `json:"campaign_type"`
		Description     string `json:"description"`
		TargetState     string `json:"target_state"`
		TargetLGA       string `json:"target_lga"`
		MessageTemplate string `json:"message_template"`
		MessageVariantB string `json:"message_variant_b"`
		ABSplitPct      int    `json:"ab_split_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.CampaignType == "" {
		jsonErr(w, "name and campaign_type required", http.StatusBadRequest)
		return
	}
	validTypes := map[string]bool{"sms": true, "ussd": true, "push": true, "whatsapp": true, "email": true, "door_to_door": true, "phone_bank": true, "ride_to_polls": true}
	if !validTypes[req.CampaignType] {
		jsonErr(w, "invalid campaign_type", http.StatusBadRequest)
		return
	}
	if req.ABSplitPct == 0 {
		req.ABSplitPct = 50
	}

	campaignID := "gotv-camp-" + uuid.New().String()[:8]
	_, err := svc.DB.Exec(
		`INSERT INTO gotv_campaigns (campaign_id, party_id, name, description, campaign_type, target_state, target_lga, message_template, message_variant_b, ab_split_pct, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		campaignID, pid, req.Name, req.Description, req.CampaignType,
		nullStr(req.TargetState), nullStr(req.TargetLGA),
		req.MessageTemplate, nullStr(req.MessageVariantB), req.ABSplitPct, user,
	)
	if err != nil {
		log.Error().Err(err).Msg("GOTV: create campaign failed")
		jsonErr(w, "create failed", http.StatusInternalServerError)
		return
	}

	svc.Audit(pid, user, "create_campaign", "campaign", campaignID)
	w.WriteHeader(http.StatusCreated)
	jsonResp(w, map[string]interface{}{"campaign_id": campaignID, "status": "draft"})
}

func handleGetCampaign(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	id := mux.Vars(r)["id"]

	var name, ctype, status, createdBy string
	var desc, tState, tLGA, msgTemplate sql.NullString
	var totalContacts, reached, responded, abSplit int
	var createdAt time.Time

	err := svc.DB.QueryRow(
		`SELECT name, description, campaign_type, status, target_state, target_lga,
		        message_template, ab_split_pct, total_contacts, contacts_reached,
		        contacts_responded, created_by, created_at
		 FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2`,
		id, pid,
	).Scan(&name, &desc, &ctype, &status, &tState, &tLGA, &msgTemplate, &abSplit,
		&totalContacts, &reached, &responded, &createdBy, &createdAt)

	if err == sql.ErrNoRows {
		jsonErr(w, "campaign not found", http.StatusNotFound)
		return
	} else if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}

	jsonResp(w, map[string]interface{}{
		"campaign_id":       id,
		"name":              name,
		"description":       nullVal(desc),
		"campaign_type":     ctype,
		"status":            status,
		"target_state":      nullVal(tState),
		"target_lga":        nullVal(tLGA),
		"message_template":  nullVal(msgTemplate),
		"ab_split_pct":      abSplit,
		"total_contacts":    totalContacts,
		"contacts_reached":  reached,
		"contacts_responded": responded,
		"created_by":        createdBy,
		"created_at":        createdAt,
	})
}

func handleUpdateCampaign(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		Name            *string `json:"name"`
		Description     *string `json:"description"`
		MessageTemplate *string `json:"message_template"`
		MessageVariantB *string `json:"message_variant_b"`
		ABSplitPct      *int    `json:"ab_split_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Only allow updates on draft campaigns
	var status string
	svc.DB.QueryRow("SELECT status FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2", id, pid).Scan(&status)
	if status != "draft" {
		jsonErr(w, "can only update draft campaigns", http.StatusConflict)
		return
	}

	sets := []string{"updated_at=NOW()"}
	args := []interface{}{}
	idx := 1

	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("name=$%d", idx))
		args = append(args, *req.Name)
		idx++
	}
	if req.Description != nil {
		sets = append(sets, fmt.Sprintf("description=$%d", idx))
		args = append(args, *req.Description)
		idx++
	}
	if req.MessageTemplate != nil {
		sets = append(sets, fmt.Sprintf("message_template=$%d", idx))
		args = append(args, *req.MessageTemplate)
		idx++
	}
	if req.ABSplitPct != nil {
		sets = append(sets, fmt.Sprintf("ab_split_pct=$%d", idx))
		args = append(args, *req.ABSplitPct)
		idx++
	}

	args = append(args, id, pid)
	query := fmt.Sprintf("UPDATE gotv_campaigns SET %s WHERE campaign_id=$%d AND party_id=$%d",
		strings.Join(sets, ", "), idx, idx+1)

	svc.DB.Exec(query, args...)
	svc.Audit(pid, user, "update_campaign", "campaign", id)
	jsonResp(w, map[string]interface{}{"updated": true})
}

func handleLaunchCampaign(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]

	var status string
	svc.DB.QueryRow("SELECT status FROM gotv_campaigns WHERE campaign_id=$1 AND party_id=$2", id, pid).Scan(&status)
	if status != "draft" && status != "scheduled" {
		jsonErr(w, "campaign must be draft or scheduled to launch", http.StatusConflict)
		return
	}

	svc.DB.Exec("UPDATE gotv_campaigns SET status='active', started_at=NOW() WHERE campaign_id=$1 AND party_id=$2", id, pid)

	// Launch async dispatch
	if err := dispatcher.LaunchCampaign(context.Background(), id, pid); err != nil {
		log.Warn().Err(err).Str("campaign", id).Msg("dispatch engine launch failed (campaign set active)")
	}

	// Emit webhook
	if webhooks != nil {
		webhooks.Emit(pid, gotv.EventCampaignCompleted, map[string]interface{}{"campaign_id": id, "action": "launched"})
	}

	svc.Audit(pid, user, "launch_campaign", "campaign", id)
	jsonResp(w, map[string]interface{}{"launched": true, "campaign_id": id})
}

// ─── Contacts ──────────────────────────────────────────────────────────────

func handleListContacts(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	status := r.URL.Query().Get("status")
	state := r.URL.Query().Get("state")
	limit := 100

	query := "SELECT contact_id, phone_encrypted, full_name_encrypted, state_code, lga_code, voter_status, tags, opted_out, created_at FROM gotv_contacts WHERE party_id=$1"
	args := []interface{}{pid}
	argIdx := 2

	if status != "" {
		query += fmt.Sprintf(" AND voter_status=$%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	if state != "" {
		query += fmt.Sprintf(" AND state_code=$%d", argIdx)
		args = append(args, state)
		argIdx++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)

	rows, err := svc.DB.Query(query, args...)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	contacts := []map[string]interface{}{}
	for rows.Next() {
		var cid, phoneEnc, vStatus string
		var nameEnc, stCode, lgaCode sql.NullString
		var tags pq.StringArray
		var optedOut bool
		var createdAt time.Time
		if err := rows.Scan(&cid, &phoneEnc, &nameEnc, &stCode, &lgaCode, &vStatus, &tags, &optedOut, &createdAt); err != nil {
			continue
		}
		// Decrypt phone for masked display
		phone, _ := svc.Decrypt(phoneEnc)
		masked := maskPhone(phone)
		var fullName string
		if nameEnc.Valid {
			fullName, _ = svc.Decrypt(nameEnc.String)
		}

		contacts = append(contacts, map[string]interface{}{
			"contact_id":   cid,
			"phone_masked": masked,
			"full_name":    fullName,
			"state_code":   nullVal(stCode),
			"lga_code":     nullVal(lgaCode),
			"voter_status": vStatus,
			"tags":         []string(tags),
			"opted_out":    optedOut,
			"created_at":   createdAt,
		})
	}
	jsonResp(w, map[string]interface{}{"contacts": contacts, "total": len(contacts)})
}

func handleCreateContact(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		Phone     string   `json:"phone"`
		FullName  string   `json:"full_name"`
		StateCode string   `json:"state_code"`
		LGACode   string   `json:"lga_code"`
		ConsentID string   `json:"consent_id"`
		Tags      []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Phone == "" {
		jsonErr(w, "phone required", http.StatusBadRequest)
		return
	}

	phoneEnc, err := svc.Encrypt(req.Phone)
	if err != nil {
		jsonErr(w, "encryption failed", http.StatusInternalServerError)
		return
	}
	pHash := svc.PhoneHash(req.Phone)

	var nameEnc sql.NullString
	if req.FullName != "" {
		enc, err := svc.Encrypt(req.FullName)
		if err == nil {
			nameEnc = sql.NullString{String: enc, Valid: true}
		}
	}

	contactID := "gotv-contact-" + uuid.New().String()[:8]
	_, err = svc.DB.Exec(
		`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, full_name_encrypted, state_code, lga_code, tags, consent_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		contactID, pid, phoneEnc, pHash, nameEnc, nullStr(req.StateCode), nullStr(req.LGACode),
		pq.StringArray(req.Tags), nullStr(req.ConsentID),
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			jsonErr(w, "contact already exists for this party", http.StatusConflict)
			return
		}
		jsonErr(w, "create failed", http.StatusInternalServerError)
		return
	}

	svc.Audit(pid, user, "create_contact", "contact", contactID)
	w.WriteHeader(http.StatusCreated)
	jsonResp(w, map[string]interface{}{"contact_id": contactID})
}

func handleImportContacts(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)

	// Rate limiting: max 10,000 contacts per day per party
	var importedToday int
	svc.DB.QueryRow(
		"SELECT COALESCE(SUM(import_count),0) FROM gotv_import_log WHERE party_id=$1 AND imported_at > NOW() - INTERVAL '24 hours'",
		pid).Scan(&importedToday)
	if importedToday >= 10000 {
		jsonErr(w, "daily import limit reached (10,000 contacts/day)", http.StatusTooManyRequests)
		return
	}

	r.ParseMultipartForm(10 << 20) // 10MB max
	file, _, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, "file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		jsonErr(w, "invalid CSV", http.StatusBadRequest)
		return
	}

	// Map header columns
	colMap := map[string]int{}
	for i, h := range header {
		colMap[strings.ToLower(strings.TrimSpace(h))] = i
	}
	phoneCol, ok := colMap["phone"]
	if !ok {
		jsonErr(w, "CSV must have 'phone' column", http.StatusBadRequest)
		return
	}

	// Enforce remaining daily quota
	remaining := 10000 - importedToday
	imported, skipped, anomalyFlags := 0, 0, 0
	for {
		// Enforce per-request quota from daily remaining
		if imported >= remaining {
			break
		}

		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) <= phoneCol {
			skipped++
			continue
		}

		phone := strings.TrimSpace(record[phoneCol])
		if phone == "" {
			skipped++
			continue
		}

		phoneEnc, err := svc.Encrypt(phone)
		if err != nil {
			skipped++
			continue
		}
		pHash := svc.PhoneHash(phone)
		contactID := "gotv-contact-" + uuid.New().String()[:8]

		var nameEnc sql.NullString
		if nameCol, ok := colMap["name"]; ok && len(record) > nameCol {
			if enc, err := svc.Encrypt(strings.TrimSpace(record[nameCol])); err == nil {
				nameEnc = sql.NullString{String: enc, Valid: true}
			}
		}

		var stateCode, lgaCode sql.NullString
		if sc, ok := colMap["state"]; ok && len(record) > sc {
			stateCode = sql.NullString{String: strings.TrimSpace(record[sc]), Valid: true}
		}
		if lc, ok := colMap["lga"]; ok && len(record) > lc {
			lgaCode = sql.NullString{String: strings.TrimSpace(record[lc]), Valid: true}
		}

		_, err = svc.DB.Exec(
			`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, full_name_encrypted, state_code, lga_code)
			 VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (party_id, phone_hash) DO NOTHING`,
			contactID, pid, phoneEnc, pHash, nameEnc, stateCode, lgaCode,
		)
		if err != nil {
			skipped++
		} else {
			imported++
		}
	}

	// Record in import log for rate limiting
	svc.DB.Exec("INSERT INTO gotv_import_log (party_id, import_count) VALUES ($1,$2)", pid, imported)

	// Anomaly detection: flag if >90% of phones are unknown status (suspicious bulk import)
	if imported > 100 {
		var unknownCount int
		svc.DB.QueryRow(
			"SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND voter_status='unknown' AND created_at > NOW() - INTERVAL '1 hour'",
			pid).Scan(&unknownCount)
		var totalRecent int
		svc.DB.QueryRow(
			"SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND created_at > NOW() - INTERVAL '1 hour'",
			pid).Scan(&totalRecent)
		if totalRecent > 0 && float64(unknownCount)/float64(totalRecent) > 0.9 {
			anomalyFlags = 1
			svc.Audit(pid, user, "anomaly_detected", "import", fmt.Sprintf("high_unknown_ratio=%d/%d", unknownCount, totalRecent))
		}
	}

	svc.Audit(pid, user, "import_contacts", "contacts", fmt.Sprintf("imported=%d,skipped=%d,anomaly=%d", imported, skipped, anomalyFlags))
	jsonResp(w, map[string]interface{}{
		"imported":       imported,
		"skipped":        skipped,
		"anomaly_flagged": anomalyFlags > 0,
		"daily_remaining": remaining - imported,
	})
}

func handleGetContact(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	id := mux.Vars(r)["id"]

	var phoneEnc, vStatus string
	var nameEnc, stCode, lgaCode, consentID sql.NullString
	var tags pq.StringArray
	var optedOut bool
	var contactCount int
	var createdAt time.Time

	err := svc.DB.QueryRow(
		`SELECT phone_encrypted, full_name_encrypted, state_code, lga_code, voter_status, tags, opted_out, consent_id, contact_count, created_at
		 FROM gotv_contacts WHERE contact_id=$1 AND party_id=$2`, id, pid,
	).Scan(&phoneEnc, &nameEnc, &stCode, &lgaCode, &vStatus, &tags, &optedOut, &consentID, &contactCount, &createdAt)

	if err != nil {
		jsonErr(w, "contact not found", http.StatusNotFound)
		return
	}

	phone, _ := svc.Decrypt(phoneEnc)
	var fullName string
	if nameEnc.Valid {
		fullName, _ = svc.Decrypt(nameEnc.String)
	}

	jsonResp(w, map[string]interface{}{
		"contact_id":    id,
		"phone_masked":  maskPhone(phone),
		"full_name":     fullName,
		"state_code":    nullVal(stCode),
		"lga_code":      nullVal(lgaCode),
		"voter_status":  vStatus,
		"tags":          []string(tags),
		"opted_out":     optedOut,
		"consent_id":    nullVal(consentID),
		"contact_count": contactCount,
		"created_at":    createdAt,
	})
}

func handleOptOut(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	svc.DB.Exec("UPDATE gotv_contacts SET opted_out=TRUE, opted_out_at=NOW() WHERE contact_id=$1 AND party_id=$2", id, pid)
	svc.Audit(pid, user, "opt_out", "contact", id)
	jsonResp(w, map[string]interface{}{"opted_out": true})
}

// ─── Volunteers ────────────────────────────────────────────────────────────

func handleListVolunteers(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	rows, err := svc.DB.Query(
		`SELECT volunteer_id, full_name, role, is_active, has_vehicle, doors_knocked, calls_made, rides_given, created_at
		 FROM gotv_volunteers WHERE party_id=$1 ORDER BY created_at DESC LIMIT 200`, pid)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	vols := []map[string]interface{}{}
	for rows.Next() {
		var vid, name, role string
		var isActive, hasVehicle bool
		var doors, calls, rides int
		var createdAt time.Time
		if err := rows.Scan(&vid, &name, &role, &isActive, &hasVehicle, &doors, &calls, &rides, &createdAt); err != nil {
			continue
		}
		vols = append(vols, map[string]interface{}{
			"volunteer_id":   vid,
			"full_name":      name,
			"role":           role,
			"is_active":      isActive,
			"has_vehicle":    hasVehicle,
			"doors_knocked":  doors,
			"calls_made":     calls,
			"rides_given":    rides,
			"created_at":     createdAt,
		})
	}
	jsonResp(w, map[string]interface{}{"volunteers": vols, "total": len(vols)})
}

func handleCreateVolunteer(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		FullName   string  `json:"full_name"`
		Phone      string  `json:"phone"`
		Role       string  `json:"role"`
		State      string  `json:"assigned_state"`
		LGA        string  `json:"assigned_lga"`
		HasVehicle bool    `json:"has_vehicle"`
		Capacity   int     `json:"vehicle_capacity"`
		Latitude   float64 `json:"latitude"`
		Longitude  float64 `json:"longitude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.FullName == "" || req.Phone == "" {
		jsonErr(w, "full_name and phone required", http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = "canvasser"
	}

	vid := "gotv-vol-" + uuid.New().String()[:8]
	_, err := svc.DB.Exec(
		`INSERT INTO gotv_volunteers (volunteer_id, party_id, full_name, phone, role, assigned_state, assigned_lga, has_vehicle, vehicle_capacity, latitude, longitude)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		vid, pid, req.FullName, req.Phone, req.Role, nullStr(req.State), nullStr(req.LGA),
		req.HasVehicle, req.Capacity, req.Latitude, req.Longitude,
	)
	if err != nil {
		jsonErr(w, "create failed", http.StatusInternalServerError)
		return
	}

	svc.Audit(pid, user, "create_volunteer", "volunteer", vid)
	w.WriteHeader(http.StatusCreated)
	jsonResp(w, map[string]interface{}{"volunteer_id": vid})
}

func handleVolunteerCheckin(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	svc.DB.Exec("UPDATE gotv_volunteers SET latitude=$1, longitude=$2, last_checkin_at=NOW() WHERE volunteer_id=$3 AND party_id=$4",
		req.Latitude, req.Longitude, id, pid)
	jsonResp(w, map[string]interface{}{"checked_in": true})
}

// ─── Pledges ───────────────────────────────────────────────────────────────

func handleListPledges(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	rows, err := svc.DB.Query(
		`SELECT pledge_id, contact_id, election_id, pledge_type, status, reminder_sent, created_at
		 FROM gotv_pledges WHERE party_id=$1 ORDER BY created_at DESC LIMIT 200`, pid)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	pledges := []map[string]interface{}{}
	for rows.Next() {
		var pledgeID, contactID, ptype, pstatus string
		var electionID sql.NullInt64
		var reminderSent bool
		var createdAt time.Time
		if err := rows.Scan(&pledgeID, &contactID, &electionID, &ptype, &pstatus, &reminderSent, &createdAt); err != nil {
			continue
		}
		pledges = append(pledges, map[string]interface{}{
			"pledge_id":     pledgeID,
			"contact_id":    contactID,
			"election_id":   nullValInt(electionID),
			"pledge_type":   ptype,
			"status":        pstatus,
			"reminder_sent": reminderSent,
			"created_at":    createdAt,
		})
	}
	jsonResp(w, map[string]interface{}{"pledges": pledges, "total": len(pledges)})
}

func handleCreatePledge(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		ContactID  string `json:"contact_id"`
		ElectionID int    `json:"election_id"`
		PledgeType string `json:"pledge_type"`
		Notes      string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ContactID == "" {
		jsonErr(w, "contact_id required", http.StatusBadRequest)
		return
	}
	if req.PledgeType == "" {
		req.PledgeType = "will_vote"
	}

	pledgeID := "gotv-pledge-" + uuid.New().String()[:8]
	_, err := svc.DB.Exec(
		`INSERT INTO gotv_pledges (pledge_id, party_id, contact_id, election_id, pledge_type, notes)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		pledgeID, pid, req.ContactID, req.ElectionID, req.PledgeType, nullStr(req.Notes),
	)
	if err != nil {
		jsonErr(w, "create failed", http.StatusInternalServerError)
		return
	}

	// Update contact voter_status
	svc.DB.Exec("UPDATE gotv_contacts SET voter_status='pledged' WHERE contact_id=$1 AND party_id=$2", req.ContactID, pid)
	svc.Audit(pid, user, "create_pledge", "pledge", pledgeID)
	w.WriteHeader(http.StatusCreated)
	jsonResp(w, map[string]interface{}{"pledge_id": pledgeID})
}

func handleUpdatePledge(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}
	validStatuses := map[string]bool{"pledged": true, "reminded": true, "confirmed_day_of": true, "fulfilled": true, "broken": true}
	if !validStatuses[req.Status] {
		jsonErr(w, "invalid status", http.StatusBadRequest)
		return
	}

	var fulfilledClause string
	if req.Status == "fulfilled" {
		fulfilledClause = ", fulfilled_at=NOW()"
	}
	svc.DB.Exec(fmt.Sprintf("UPDATE gotv_pledges SET status=$1%s WHERE pledge_id=$2 AND party_id=$3", fulfilledClause), req.Status, id, pid)
	svc.Audit(pid, user, "update_pledge", "pledge", id)
	jsonResp(w, map[string]interface{}{"updated": true})
}

// ─── Rides ─────────────────────────────────────────────────────────────────

func handleListRides(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	rows, err := svc.DB.Query(
		`SELECT request_id, contact_id, volunteer_id, polling_unit_code, status, distance_km, requested_at
		 FROM gotv_ride_requests WHERE party_id=$1 ORDER BY requested_at DESC LIMIT 200`, pid)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	rides := []map[string]interface{}{}
	for rows.Next() {
		var rid, cid, puCode, rstatus string
		var vid sql.NullString
		var distKm sql.NullFloat64
		var requestedAt time.Time
		if err := rows.Scan(&rid, &cid, &vid, &puCode, &rstatus, &distKm, &requestedAt); err != nil {
			continue
		}
		rides = append(rides, map[string]interface{}{
			"request_id":        rid,
			"contact_id":        cid,
			"volunteer_id":      nullVal(vid),
			"polling_unit_code": puCode,
			"status":            rstatus,
			"distance_km":       nullValFloat(distKm),
			"requested_at":      requestedAt,
		})
	}
	jsonResp(w, map[string]interface{}{"rides": rides, "total": len(rides)})
}

func handleCreateRide(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		ContactID       string  `json:"contact_id"`
		PickupLatitude  float64 `json:"pickup_latitude"`
		PickupLongitude float64 `json:"pickup_longitude"`
		PollingUnitCode string  `json:"polling_unit_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ContactID == "" || req.PollingUnitCode == "" {
		jsonErr(w, "contact_id and polling_unit_code required", http.StatusBadRequest)
		return
	}

	rid := "gotv-ride-" + uuid.New().String()[:8]
	_, err := svc.DB.Exec(
		`INSERT INTO gotv_ride_requests (request_id, party_id, contact_id, pickup_latitude, pickup_longitude, polling_unit_code)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		rid, pid, req.ContactID, req.PickupLatitude, req.PickupLongitude, req.PollingUnitCode,
	)
	if err != nil {
		jsonErr(w, "create failed", http.StatusInternalServerError)
		return
	}
	svc.Audit(pid, user, "create_ride", "ride", rid)
	w.WriteHeader(http.StatusCreated)
	jsonResp(w, map[string]interface{}{"request_id": rid})
}

func handleMatchRide(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]

	var pickupLat, pickupLng float64
	var contactID string
	err := svc.DB.QueryRow(
		"SELECT contact_id, pickup_latitude, pickup_longitude FROM gotv_ride_requests WHERE request_id=$1 AND party_id=$2 AND status='pending'",
		id, pid,
	).Scan(&contactID, &pickupLat, &pickupLng)
	if err != nil {
		jsonErr(w, "ride not found or already matched", http.StatusNotFound)
		return
	}

	// Find nearest available driver
	var driverID string
	var driverLat, driverLng float64
	err = svc.DB.QueryRow(
		`SELECT volunteer_id, latitude, longitude FROM gotv_volunteers
		 WHERE party_id=$1 AND is_active=TRUE AND has_vehicle=TRUE AND latitude IS NOT NULL
		 ORDER BY (latitude-$2)*(latitude-$2) + (longitude-$3)*(longitude-$3) LIMIT 1`,
		pid, pickupLat, pickupLng,
	).Scan(&driverID, &driverLat, &driverLng)

	if err != nil {
		jsonErr(w, "no available drivers", http.StatusNotFound)
		return
	}

	dist := haversineKm(pickupLat, pickupLng, driverLat, driverLng)
	svc.DB.Exec(
		"UPDATE gotv_ride_requests SET volunteer_id=$1, status='matched', matched_at=NOW(), distance_km=$2 WHERE request_id=$3",
		driverID, dist, id,
	)
	svc.DB.Exec("UPDATE gotv_volunteers SET rides_given=rides_given+1 WHERE volunteer_id=$1", driverID)
	svc.Audit(pid, user, "match_ride", "ride", id)

	jsonResp(w, map[string]interface{}{
		"matched":      true,
		"volunteer_id": driverID,
		"distance_km":  math.Round(dist*100) / 100,
	})
}

func handleUpdateRideStatus(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		Status string `json:"status"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	validStatuses := map[string]bool{"en_route": true, "picked_up": true, "dropped_off": true, "cancelled": true, "no_show": true}
	if !validStatuses[req.Status] {
		jsonErr(w, "invalid status", http.StatusBadRequest)
		return
	}

	var timeCol string
	switch req.Status {
	case "picked_up":
		timeCol = ", picked_up_at=NOW()"
	case "dropped_off":
		timeCol = ", dropped_off_at=NOW()"
	}

	svc.DB.Exec(fmt.Sprintf("UPDATE gotv_ride_requests SET status=$1%s WHERE request_id=$2 AND party_id=$3", timeCol), req.Status, id, pid)
	svc.Audit(pid, user, "update_ride_status", "ride", id)
	jsonResp(w, map[string]interface{}{"updated": true})
}

// ─── Turnout (public) ──────────────────────────────────────────────────────

func handleTurnout(w http.ResponseWriter, r *http.Request) {
	electionID := mux.Vars(r)["election_id"]
	rows, err := svc.DB.Query(
		`SELECT pu.state_code, COUNT(DISTINCT pu.polling_unit_id) AS pus,
		        COALESCE(SUM(pu.registered_voters), 0) AS registered,
		        COALESCE(SUM(r.accredited_voters), 0) AS accredited
		 FROM polling_units pu
		 LEFT JOIN results r ON r.polling_unit_id = pu.polling_unit_id AND r.election_id=$1
		 GROUP BY pu.state_code ORDER BY pu.state_code`, electionID)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var states []map[string]interface{}
	var totalReg, totalAcc int64
	for rows.Next() {
		var stateCode string
		var pus int
		var reg, acc int64
		if err := rows.Scan(&stateCode, &pus, &reg, &acc); err != nil {
			continue
		}
		totalReg += reg
		totalAcc += acc
		pct := 0.0
		if reg > 0 {
			pct = math.Round(float64(acc)/float64(reg)*10000) / 100
		}
		states = append(states, map[string]interface{}{
			"state_code":    stateCode,
			"polling_units": pus,
			"registered":    reg,
			"accredited":    acc,
			"turnout_pct":   pct,
		})
	}

	natPct := 0.0
	if totalReg > 0 {
		natPct = math.Round(float64(totalAcc)/float64(totalReg)*10000) / 100
	}

	jsonResp(w, map[string]interface{}{
		"election_id":          electionID,
		"total_registered":     totalReg,
		"total_accredited":     totalAcc,
		"national_turnout_pct": natPct,
		"by_state":             states,
	})
}

// ─── Dashboard ─────────────────────────────────────────────────────────────

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)

	var totalContacts, totalVolunteers, totalPledges, activeCampaigns, pendingRides int
	svc.DB.QueryRow("SELECT COUNT(*) FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE", pid).Scan(&totalContacts)
	svc.DB.QueryRow("SELECT COUNT(*) FROM gotv_volunteers WHERE party_id=$1 AND is_active=TRUE", pid).Scan(&totalVolunteers)
	svc.DB.QueryRow("SELECT COUNT(*) FROM gotv_pledges WHERE party_id=$1", pid).Scan(&totalPledges)
	svc.DB.QueryRow("SELECT COUNT(*) FROM gotv_campaigns WHERE party_id=$1 AND status='active'", pid).Scan(&activeCampaigns)
	svc.DB.QueryRow("SELECT COUNT(*) FROM gotv_ride_requests WHERE party_id=$1 AND status='pending'", pid).Scan(&pendingRides)

	jsonResp(w, map[string]interface{}{
		"party_id":          pid,
		"total_contacts":    totalContacts,
		"total_volunteers":  totalVolunteers,
		"total_pledges":     totalPledges,
		"active_campaigns":  activeCampaigns,
		"pending_rides":     pendingRides,
	})
}

// ─── Volunteer Location (real-time GPS) ────────────────────────────────────

func handleVolunteerLocation(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	id := mux.Vars(r)["id"]
	var req struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Battery   int     `json:"battery"`
		Speed     float64 `json:"speed_kmh"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}

	svc.DB.Exec(
		"UPDATE gotv_volunteers SET latitude=$1, longitude=$2, last_checkin_at=NOW() WHERE volunteer_id=$3 AND party_id=$4",
		req.Latitude, req.Longitude, id, pid)

	// Broadcast to WebSocket
	if wsHub != nil {
		wsHub.Broadcast("volunteer.location", pid, map[string]interface{}{
			"volunteer_id": id,
			"latitude":     req.Latitude,
			"longitude":    req.Longitude,
			"battery":      req.Battery,
			"speed_kmh":    req.Speed,
		})
	}

	jsonResp(w, map[string]interface{}{"updated": true})
}

// ─── Geospatial Data Endpoints (for map visualization) ─────────────────────

func handleGeoVolunteers(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	rows, err := svc.DB.Query(
		`SELECT volunteer_id, full_name, role, latitude, longitude, is_active,
		        has_vehicle, vehicle_capacity, doors_knocked, calls_made, rides_given,
		        last_checkin_at
		 FROM gotv_volunteers WHERE party_id=$1 AND latitude IS NOT NULL AND longitude IS NOT NULL`,
		pid)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type geoVol struct {
		ID              string      `json:"id"`
		Name            string      `json:"name"`
		Role            string      `json:"role"`
		Lat             float64     `json:"lat"`
		Lng             float64     `json:"lng"`
		Active          bool        `json:"active"`
		HasVehicle      bool        `json:"has_vehicle"`
		VehicleCapacity int         `json:"vehicle_capacity"`
		DoorsKnocked    int         `json:"doors_knocked"`
		CallsMade       int         `json:"calls_made"`
		RidesGiven      int         `json:"rides_given"`
		LastCheckin     interface{} `json:"last_checkin"`
	}
	var vols []geoVol
	for rows.Next() {
		var v geoVol
		var lastCheckin sql.NullTime
		if err := rows.Scan(&v.ID, &v.Name, &v.Role, &v.Lat, &v.Lng, &v.Active,
			&v.HasVehicle, &v.VehicleCapacity, &v.DoorsKnocked, &v.CallsMade, &v.RidesGiven,
			&lastCheckin); err != nil {
			continue
		}
		if lastCheckin.Valid {
			v.LastCheckin = lastCheckin.Time
		}
		vols = append(vols, v)
	}
	jsonResp(w, map[string]interface{}{"volunteers": vols, "total": len(vols)})
}

func handleGeoRides(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	rows, err := svc.DB.Query(
		`SELECT rr.request_id, rr.contact_id, rr.volunteer_id, rr.polling_unit_code,
		        rr.pickup_latitude, rr.pickup_longitude, rr.status, rr.distance_km,
		        COALESCE(pu.latitude, 0), COALESCE(pu.longitude, 0)
		 FROM gotv_ride_requests rr
		 LEFT JOIN polling_units pu ON pu.unique_id = rr.polling_unit_code
		 WHERE rr.party_id=$1 AND rr.status IN ('pending','matched','en_route','picked_up')`,
		pid)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type geoRide struct {
		ID        string  `json:"id"`
		ContactID string  `json:"contact_id"`
		VolID     *string `json:"volunteer_id"`
		PUCode    string  `json:"pu_code"`
		PickupLat float64 `json:"pickup_lat"`
		PickupLng float64 `json:"pickup_lng"`
		PULat     float64 `json:"pu_lat"`
		PULng     float64 `json:"pu_lng"`
		Status    string  `json:"status"`
		DistKm    float64 `json:"distance_km"`
	}
	var rides []geoRide
	for rows.Next() {
		var r geoRide
		var volID sql.NullString
		var distKm sql.NullFloat64
		if err := rows.Scan(&r.ID, &r.ContactID, &volID, &r.PUCode,
			&r.PickupLat, &r.PickupLng, &r.Status, &distKm, &r.PULat, &r.PULng); err != nil {
			continue
		}
		if volID.Valid {
			r.VolID = &volID.String
		}
		if distKm.Valid {
			r.DistKm = distKm.Float64
		}
		rides = append(rides, r)
	}
	jsonResp(w, map[string]interface{}{"rides": rides, "total": len(rides)})
}

func handleGeoCoverage(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)

	// H3-style hex coverage: count contacts and volunteers per state/lga
	type coverageHex struct {
		State      string `json:"state"`
		LGA        string `json:"lga"`
		Contacts   int    `json:"contacts"`
		Volunteers int    `json:"volunteers"`
		Score      float64 `json:"score"` // 0.0 (no coverage) to 1.0 (fully covered)
	}

	rows, err := svc.DB.Query(`
		SELECT COALESCE(c.state_code,'unknown') AS state,
		       COALESCE(c.lga_code,'unknown') AS lga,
		       COUNT(DISTINCT c.contact_id) AS contacts,
		       COUNT(DISTINCT v.volunteer_id) AS volunteers
		FROM gotv_contacts c
		LEFT JOIN gotv_volunteers v ON v.party_id = c.party_id
		  AND v.assigned_state = c.state_code AND v.is_active = TRUE
		WHERE c.party_id = $1 AND c.opted_out = FALSE
		GROUP BY c.state_code, c.lga_code
		ORDER BY contacts DESC
		LIMIT 500`, pid)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var hexes []coverageHex
	for rows.Next() {
		var h coverageHex
		if err := rows.Scan(&h.State, &h.LGA, &h.Contacts, &h.Volunteers); err != nil {
			continue
		}
		if h.Contacts > 0 {
			h.Score = math.Min(float64(h.Volunteers)/float64(h.Contacts)*100, 1.0)
		}
		hexes = append(hexes, h)
	}
	jsonResp(w, map[string]interface{}{"coverage": hexes, "total": len(hexes)})
}

func handleGeoCanvassTrails(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	volID := r.URL.Query().Get("volunteer_id")

	query := `SELECT volunteer_id, latitude, longitude, outcome, recorded_at
	          FROM gotv_door_knocks WHERE party_id=$1`
	args := []interface{}{pid}
	if volID != "" {
		query += " AND volunteer_id=$2"
		args = append(args, volID)
	}
	query += " ORDER BY recorded_at DESC LIMIT 5000"

	rows, err := svc.DB.Query(query, args...)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type trail struct {
		VolID   string    `json:"volunteer_id"`
		Lat     float64   `json:"lat"`
		Lng     float64   `json:"lng"`
		Outcome string    `json:"outcome"`
		Time    time.Time `json:"time"`
	}
	var trails []trail
	for rows.Next() {
		var t trail
		if err := rows.Scan(&t.VolID, &t.Lat, &t.Lng, &t.Outcome, &t.Time); err != nil {
			continue
		}
		trails = append(trails, t)
	}
	jsonResp(w, map[string]interface{}{"trails": trails, "total": len(trails)})
}

// ─── Canvasser Field Workflow ──────────────────────────────────────────────

func handleCanvassWalklist(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	volID := r.URL.Query().Get("volunteer_id")
	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, _ := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)

	// Get volunteer's assigned area
	var assignedState, assignedLGA, assignedWard string
	svc.DB.QueryRow(
		"SELECT COALESCE(assigned_state,''), COALESCE(assigned_lga,''), COALESCE(assigned_ward,'') FROM gotv_volunteers WHERE volunteer_id=$1 AND party_id=$2",
		volID, pid).Scan(&assignedState, &assignedLGA, &assignedWard)

	// Fetch contacts in assigned area, sorted by proximity if lat/lng provided
	query := `SELECT contact_id, phone_encrypted, full_name_encrypted, state_code, lga_code, ward_code, voter_status
	          FROM gotv_contacts WHERE party_id=$1 AND opted_out=FALSE`
	args := []interface{}{pid}
	idx := 2
	if assignedState != "" {
		query += fmt.Sprintf(" AND state_code=$%d", idx)
		args = append(args, assignedState)
		idx++
	}
	if assignedLGA != "" {
		query += fmt.Sprintf(" AND lga_code=$%d", idx)
		args = append(args, assignedLGA)
		idx++
	}
	query += " ORDER BY created_at LIMIT 200"

	rows, err := svc.DB.Query(query, args...)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type walkItem struct {
		ContactID string `json:"contact_id"`
		Phone     string `json:"phone_masked"`
		Name      string `json:"full_name"`
		State     string `json:"state"`
		LGA       string `json:"lga"`
		Ward      string `json:"ward"`
		Status    string `json:"voter_status"`
	}
	var list []walkItem
	for rows.Next() {
		var cid, phoneEnc, vStatus string
		var nameEnc, st, lga, ward sql.NullString
		if err := rows.Scan(&cid, &phoneEnc, &nameEnc, &st, &lga, &ward, &vStatus); err != nil {
			continue
		}
		phone, _ := svc.Decrypt(phoneEnc)
		name := ""
		if nameEnc.Valid {
			name, _ = svc.Decrypt(nameEnc.String)
		}
		list = append(list, walkItem{
			ContactID: cid, Phone: maskPhone(phone), Name: name,
			State: st.String, LGA: lga.String, Ward: ward.String, Status: vStatus,
		})
	}

	_ = lat
	_ = lng
	jsonResp(w, map[string]interface{}{"walklist": list, "total": len(list)})
}

func handleCanvassDoorKnock(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		VolunteerID string  `json:"volunteer_id"`
		ContactID   string  `json:"contact_id"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Outcome     string  `json:"outcome"` // home, not_home, refused, pledged, already_voted
		Notes       string  `json:"notes"`
		SpeedKmh    float64 `json:"speed_kmh"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}

	validOutcomes := map[string]bool{"home": true, "not_home": true, "refused": true, "pledged": true, "already_voted": true}
	if !validOutcomes[req.Outcome] {
		jsonErr(w, "invalid outcome", http.StatusBadRequest)
		return
	}

	// Anti-fraud: flag if speed > 30 km/h (driving, not walking)
	isSuspicious := req.SpeedKmh > 30

	_, err := svc.DB.Exec(
		`INSERT INTO gotv_door_knocks
		 (party_id, volunteer_id, contact_id, latitude, longitude, outcome, notes, speed_kmh, is_suspicious, recorded_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())`,
		pid, req.VolunteerID, req.ContactID, req.Latitude, req.Longitude,
		req.Outcome, req.Notes, req.SpeedKmh, isSuspicious)
	if err != nil {
		jsonErr(w, "failed to record knock", http.StatusInternalServerError)
		return
	}

	// Update volunteer stats
	svc.DB.Exec("UPDATE gotv_volunteers SET doors_knocked=doors_knocked+1 WHERE volunteer_id=$1 AND party_id=$2",
		req.VolunteerID, pid)

	// Update contact status if pledged
	if req.Outcome == "pledged" {
		svc.DB.Exec("UPDATE gotv_contacts SET voter_status='pledged', updated_at=NOW() WHERE contact_id=$1 AND party_id=$2",
			req.ContactID, pid)
	}

	// Broadcast canvass event
	if wsHub != nil {
		wsHub.Broadcast("canvass.log", pid, map[string]interface{}{
			"volunteer_id": req.VolunteerID,
			"contact_id":   req.ContactID,
			"outcome":      req.Outcome,
			"latitude":     req.Latitude,
			"longitude":    req.Longitude,
			"suspicious":   isSuspicious,
		})
	}

	svc.Audit(pid, user, "door_knock", "canvass", req.ContactID)
	w.WriteHeader(http.StatusCreated)
	jsonResp(w, map[string]interface{}{"recorded": true, "suspicious": isSuspicious})
}

func handleCanvassShiftStart(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		VolunteerID string  `json:"volunteer_id"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}

	_, err := svc.DB.Exec(
		`INSERT INTO gotv_shifts (party_id, volunteer_id, start_lat, start_lng, started_at)
		 VALUES ($1,$2,$3,$4,NOW())`,
		pid, req.VolunteerID, req.Latitude, req.Longitude)
	if err != nil {
		jsonErr(w, "failed to start shift", http.StatusInternalServerError)
		return
	}

	svc.DB.Exec("UPDATE gotv_volunteers SET is_active=TRUE, latitude=$1, longitude=$2, last_checkin_at=NOW() WHERE volunteer_id=$3 AND party_id=$4",
		req.Latitude, req.Longitude, req.VolunteerID, pid)

	svc.Audit(pid, user, "shift_start", "canvass", req.VolunteerID)
	w.WriteHeader(http.StatusCreated)
	jsonResp(w, map[string]interface{}{"shift_started": true})
}

func handleCanvassShiftEnd(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		VolunteerID string  `json:"volunteer_id"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}

	svc.DB.Exec(
		`UPDATE gotv_shifts SET end_lat=$1, end_lng=$2, ended_at=NOW()
		 WHERE volunteer_id=$3 AND party_id=$4 AND ended_at IS NULL`,
		req.Latitude, req.Longitude, req.VolunteerID, pid)

	// Count knocks during this shift
	var knocks int
	svc.DB.QueryRow(
		`SELECT COUNT(*) FROM gotv_door_knocks
		 WHERE volunteer_id=$1 AND party_id=$2 AND recorded_at >= (
		   SELECT started_at FROM gotv_shifts WHERE volunteer_id=$1 AND party_id=$2 ORDER BY started_at DESC LIMIT 1
		 )`, req.VolunteerID, pid).Scan(&knocks)

	svc.Audit(pid, user, "shift_end", "canvass", req.VolunteerID)
	jsonResp(w, map[string]interface{}{"shift_ended": true, "doors_knocked": knocks})
}

// ─── Webhook Management ────────────────────────────────────────────────────

func handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	pid, _ := getParty(r)
	rows, err := svc.DB.Query(
		"SELECT id, url, event_types, is_active, failure_count, created_at FROM gotv_webhooks WHERE party_id=$1",
		pid)
	if err != nil {
		jsonErr(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type wh struct {
		ID           int      `json:"id"`
		URL          string   `json:"url"`
		EventTypes   []string `json:"event_types"`
		IsActive     bool     `json:"is_active"`
		FailureCount int      `json:"failure_count"`
		CreatedAt    string   `json:"created_at"`
	}
	var hooks []wh
	for rows.Next() {
		var h wh
		var events pq.StringArray
		var createdAt time.Time
		if err := rows.Scan(&h.ID, &h.URL, &events, &h.IsActive, &h.FailureCount, &createdAt); err != nil {
			continue
		}
		h.EventTypes = events
		h.CreatedAt = createdAt.Format(time.RFC3339)
		hooks = append(hooks, h)
	}
	jsonResp(w, map[string]interface{}{"webhooks": hooks, "total": len(hooks)})
}

func handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	var req struct {
		URL        string   `json:"url"`
		Secret     string   `json:"secret"`
		EventTypes []string `json:"event_types"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.URL == "" || req.Secret == "" || len(req.EventTypes) == 0 {
		jsonErr(w, "url, secret, and event_types required", http.StatusBadRequest)
		return
	}

	_, err := svc.DB.Exec(
		"INSERT INTO gotv_webhooks (party_id, url, secret, event_types) VALUES ($1,$2,$3,$4)",
		pid, req.URL, req.Secret, pq.Array(req.EventTypes))
	if err != nil {
		jsonErr(w, "create failed", http.StatusInternalServerError)
		return
	}

	svc.Audit(pid, user, "create_webhook", "webhook", req.URL)
	w.WriteHeader(http.StatusCreated)
	jsonResp(w, map[string]interface{}{"created": true})
}

func handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	pid, user := getParty(r)
	id := mux.Vars(r)["id"]
	svc.DB.Exec("DELETE FROM gotv_webhooks WHERE id=$1 AND party_id=$2", id, pid)
	svc.Audit(pid, user, "delete_webhook", "webhook", id)
	jsonResp(w, map[string]interface{}{"deleted": true})
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func jsonResp(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullVal(ns sql.NullString) interface{} {
	if ns.Valid {
		return ns.String
	}
	return nil
}

func nullValInt(ni sql.NullInt64) interface{} {
	if ni.Valid {
		return ni.Int64
	}
	return nil
}

func nullValFloat(nf sql.NullFloat64) interface{} {
	if nf.Valid {
		return nf.Float64
	}
	return nil
}

func maskPhone(phone string) string {
	if len(phone) < 7 {
		return "***"
	}
	return phone[:4] + "****" + phone[len(phone)-3:]
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}
