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
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var svc *gotv.Service

func main() {
	port := flag.Int("port", 8103, "HTTP port")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
	encKey := flag.String("enc-key", os.Getenv("GOTV_ENCRYPTION_KEY"), "AES-256 encryption key (64 hex chars)")
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

	r := mux.NewRouter()

	// Health
	r.HandleFunc("/health", handleHealth).Methods("GET")

	// Campaigns
	r.HandleFunc("/gotv/campaigns", authMW(handleListCampaigns)).Methods("GET")
	r.HandleFunc("/gotv/campaigns", authMW(handleCreateCampaign)).Methods("POST")
	r.HandleFunc("/gotv/campaigns/{id}", authMW(handleGetCampaign)).Methods("GET")
	r.HandleFunc("/gotv/campaigns/{id}", authMW(handleUpdateCampaign)).Methods("PATCH")
	r.HandleFunc("/gotv/campaigns/{id}/launch", authMW(handleLaunchCampaign)).Methods("POST")

	// Contacts
	r.HandleFunc("/gotv/contacts", authMW(handleListContacts)).Methods("GET")
	r.HandleFunc("/gotv/contacts", authMW(handleCreateContact)).Methods("POST")
	r.HandleFunc("/gotv/contacts/import", authMW(handleImportContacts)).Methods("POST")
	r.HandleFunc("/gotv/contacts/{id}", authMW(handleGetContact)).Methods("GET")
	r.HandleFunc("/gotv/contacts/{id}/opt-out", authMW(handleOptOut)).Methods("POST")

	// Volunteers
	r.HandleFunc("/gotv/volunteers", authMW(handleListVolunteers)).Methods("GET")
	r.HandleFunc("/gotv/volunteers", authMW(handleCreateVolunteer)).Methods("POST")
	r.HandleFunc("/gotv/volunteers/{id}/checkin", authMW(handleVolunteerCheckin)).Methods("POST")

	// Pledges
	r.HandleFunc("/gotv/pledges", authMW(handleListPledges)).Methods("GET")
	r.HandleFunc("/gotv/pledges", authMW(handleCreatePledge)).Methods("POST")
	r.HandleFunc("/gotv/pledges/{id}", authMW(handleUpdatePledge)).Methods("PATCH")

	// Ride-to-polls
	r.HandleFunc("/gotv/rides", authMW(handleListRides)).Methods("GET")
	r.HandleFunc("/gotv/rides", authMW(handleCreateRide)).Methods("POST")
	r.HandleFunc("/gotv/rides/{id}/match", authMW(handleMatchRide)).Methods("POST")
	r.HandleFunc("/gotv/rides/{id}/status", authMW(handleUpdateRideStatus)).Methods("PATCH")

	// Turnout (public aggregate data)
	r.HandleFunc("/gotv/turnout/{election_id}", handleTurnout).Methods("GET")

	// Dashboard
	r.HandleFunc("/gotv/dashboard", authMW(handleDashboard)).Methods("GET")

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

// ─── Auth Middleware ────────────────────────────────────────────────────────

func authMW(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract party from JWT or X-Party-ID header
		// In standalone mode, accept X-Party-ID for inter-service calls
		partyIDStr := r.Header.Get("X-Party-ID")
		if partyIDStr == "" {
			// Try to extract from Bearer token (simplified for standalone)
			auth := r.Header.Get("Authorization")
			if auth == "" {
				jsonErr(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			// For standalone mode, accept any Bearer token and use X-Party-ID
			partyIDStr = "1" // default for dev
		}

		partyID, err := strconv.Atoi(partyIDStr)
		if err != nil {
			jsonErr(w, "invalid party_id", http.StatusBadRequest)
			return
		}

		r.Header.Set("X-GOTV-Party-ID", strconv.Itoa(partyID))
		r.Header.Set("X-GOTV-User", r.Header.Get("X-User"))
		if r.Header.Get("X-GOTV-User") == "" {
			r.Header.Set("X-GOTV-User", "system")
		}
		next(w, r)
	}
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

	imported, skipped := 0, 0
	for {
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

	svc.Audit(pid, user, "import_contacts", "contacts", fmt.Sprintf("imported=%d,skipped=%d", imported, skipped))
	jsonResp(w, map[string]interface{}{"imported": imported, "skipped": skipped})
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
