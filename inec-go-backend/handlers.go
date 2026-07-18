package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// ── Elections ──

func handleListElections(w http.ResponseWriter, r *http.Request) {
	rows, err := dbQueryCtx(r.Context(), "SELECT * FROM elections ORDER BY election_date DESC")
	if err != nil {
		writeError(w, 500, "query failed")
		return
	}
	writeJSON(w, 200, scanRows(rows))
}

func handleGetElection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	row, err := querySingleRow("SELECT * FROM elections WHERE id=?", id)
	if err != nil {
		writeError(w, 404, "election not found")
		return
	}
	writeJSON(w, 200, row)
}

func handleCreateElection(w http.ResponseWriter, r *http.Request) {
	var req ElectionCreate
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Status == "" {
		// FSM initial state is "draft"; "upcoming" is legacy vocabulary.
		req.Status = "draft"
	}

	res, err := dbExecCtx(r.Context(), "INSERT INTO elections (title, election_type, election_date, status, description) VALUES (?,?,?,?,?)",
		req.Title, req.ElectionType, req.ElectionDate, req.Status, req.Description)
	if err != nil {
		writeError(w, 500, "failed to create election")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 200, M{"id": id})
}

// ── Results ──

func handleSubmitResult(w http.ResponseWriter, r *http.Request) {
	var req ResultSubmission
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	// Idempotency check
	var existingID int
	err := dbQueryRowCtx(r.Context(), "SELECT id FROM results WHERE idempotency_key=?", req.IdempotencyKey).Scan(&existingID)
	if err == nil {
		writeJSON(w, 200, M{"id": existingID, "idempotent": true})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		writeError(w, 500, "transaction start failed")
		return
	}
	res, err := tx.Exec("INSERT INTO results (election_id, polling_unit_code, party_code, votes, idempotency_key, status) VALUES (?,?,?,?,?,'pending')",
		req.ElectionID, req.PollingUnitCode, req.PartyCode, req.Votes, req.IdempotencyKey)
	if err != nil {
		tx.Rollback()
		writeError(w, 500, "failed to submit result")
		return
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "transaction commit failed")
		return
	}
	writeJSON(w, 201, M{"id": id})
}

func handleListResults(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	rows, _ := dbQueryCtx(r.Context(), "SELECT * FROM results WHERE election_id=? ORDER BY id DESC LIMIT 500", eid)
	writeJSON(w, 200, scanRows(rows))
}

func handleGetResult(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	row, err := querySingleRow("SELECT * FROM results WHERE id=?", id)
	if err != nil {
		writeError(w, 404, "result not found")
		return
	}
	writeJSON(w, 200, row)
}

func handleValidateResult(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	dbExecCtx(r.Context(), "UPDATE results SET status='validated', updated_at=CURRENT_TIMESTAMP WHERE id=?", id)
	writeJSON(w, 200, M{"message": "result validated"})
}

func handleFinalizeResult(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	dbExecCtx(r.Context(), "UPDATE results SET status='finalized', updated_at=CURRENT_TIMESTAMP WHERE id=?", id)
	writeJSON(w, 200, M{"message": "result finalized"})
}

// ── Auth handlers ──

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username" validate:"required,min=3"`
		Password string `json:"password" validate:"required,min=8"`
		FullName string `json:"full_name" validate:"required"`
		Role     string `json:"role"`
		StaffID  string `json:"staff_id"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	// Role-lock: only public and observer can self-register.
	// Staff roles (officers, admins) are provisioned by an administrator.
	if req.Role != "" && req.Role != "public" && req.Role != "observer" {
		writeError(w, 403, "self-registration is only allowed for public/observer roles")
		return
	}
	if req.Role == "" {
		req.Role = "public"
	}

	hash, err := hashPassword(req.Password)
	if err != nil {
		writeError(w, 500, "password hashing failed")
		return
	}

	var staffID interface{}
	if req.StaffID != "" {
		staffID = req.StaffID
	}
	res, err := dbExecCtx(r.Context(), "INSERT INTO users (username, password_hash, full_name, role, staff_id) VALUES (?,?,?,?,?)",
		req.Username, hash, req.FullName, req.Role, staffID)
	if err != nil {
		writeError(w, 409, "username already exists")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 201, M{"id": id, "username": req.Username, "role": req.Role})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username" validate:"required"`
		Password string `json:"password" validate:"required"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	var id int
	var hash, role, fullName string
	var staffID sql.NullString
	err := dbQueryRowCtx(r.Context(), "SELECT id, password_hash, role, full_name, staff_id FROM users WHERE username=?", req.Username).
		Scan(&id, &hash, &role, &fullName, &staffID)
	if err != nil || !checkPassword(hash, req.Password) {
		writeError(w, 401, "invalid credentials")
		return
	}

	token, err := generateToken(id, req.Username, role)
	if err != nil {
		writeError(w, 500, "token generation failed")
		return
	}
	dbExecCtx(r.Context(), "UPDATE users SET login_count=COALESCE(login_count,0)+1, last_login=CURRENT_TIMESTAMP WHERE id=?", id)
	writeJSON(w, 200, M{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   3600,
		"user": M{
			"id": id, "username": req.Username, "role": role,
			"full_name": fullName, "staff_id": nullStr(staffID),
		},
	})
}

func handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	claims, err := decodeToken(req.Token)
	if err != nil {
		writeError(w, 401, "invalid token")
		return
	}
	sub, _ := claims["sub"]
	username, _ := claims["username"]
	role, _ := claims["role"]
	uid, _ := strconv.Atoi(fmt.Sprintf("%v", sub))
	token, err := generateToken(uid, fmt.Sprintf("%v", username), fmt.Sprintf("%v", role))
	if err != nil {
		writeError(w, 500, "token generation failed")
		return
	}
	writeJSON(w, 200, M{"access_token": token, "token_type": "Bearer", "expires_in": 3600})
}

// handleRefreshToken issues a new access+refresh token pair from a valid refresh token.
func handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, 400, "refresh_token is required")
		return
	}
	claims, err := decodeToken(req.RefreshToken)
	if err != nil {
		writeError(w, 401, "invalid or expired refresh token")
		return
	}
	tokenType, _ := claims["type"].(string)
	if tokenType != "refresh" {
		writeError(w, 401, "not a refresh token")
		return
	}
	// Issue new access + refresh tokens
	baseClaims := map[string]interface{}{
		"sub": claims["sub"], "username": claims["username"], "role": claims["role"], "full_name": claims["full_name"],
	}
	newAccess, _ := createAccessToken(baseClaims)
	newRefresh, _ := createRefreshToken(baseClaims)
	writeJSON(w, 200, M{
		"access_token": newAccess, "refresh_token": newRefresh, "token_type": "bearer", "expires_in": 3600,
	})
}

// handleMe returns the currently authenticated user's claims.
func handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := getCurrentUser(r)
	if err != nil {
		writeError(w, 401, "Not authenticated")
		return
	}
	writeJSON(w, 200, user)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, M{"message": "logged out"})
}

// ── Users ──

func handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req UserPromotion
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	dbExecCtx(r.Context(), "UPDATE users SET role=? WHERE id=?", req.Role, id)
	writeJSON(w, 200, M{"message": "role updated"})
}

// ── Geo / tiles / export ──

func handleGeoPollingUnits(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	sc := r.URL.Query().Get("state_code")

	cacheKey := fmt.Sprintf("geo_pus_%d_%s", eid, sc)
	if cached, err := cacheGet(cacheKey); err == nil && cached != "" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(200)
		w.Write([]byte(cached))
		return
	}

	q := `SELECT pu.code, pu.name, pu.latitude as lat, pu.longitude as lon,
		w.name as ward_name, l.name as lga_name, l.state_code, s.name as state_name,
		COALESCE(r.status,'no_result') as status, pu.registered_voters, r.id as result_id
		FROM polling_units pu JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code
		JOIN states s ON s.code=l.state_code LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?`
	params := []interface{}{eid}
	if sc != "" {
		q += " WHERE l.state_code=?"
		params = append(params, sc)
	}
	rows, _ := dbQueryCtx(r.Context(), q, params...)
	pus := scanRows(rows)

	stateRows, _ := dbQueryCtx(r.Context(), "SELECT code, name, geo_zone FROM states ORDER BY name")
	states := scanRows(stateRows)

	// batch party scores for all result ids
	puPsMap := map[interface{}][]M{}
	rids := make([]interface{}, 0)
	ridPH := make([]string, 0)
	for _, pu := range pus {
		if rid, ok := pu["result_id"]; ok && rid != nil {
			rids = append(rids, rid)
			ridPH = append(ridPH, "?")
		}
	}
	if len(rids) > 0 {
		puPsRows, _ := dbQueryCtx(r.Context(), fmt.Sprintf(`SELECT rps.result_id, rps.party_code, p.abbreviation, p.color, rps.votes
			FROM result_party_scores rps JOIN parties p ON p.code=rps.party_code WHERE rps.result_id IN (%s) ORDER BY rps.votes DESC`,
			strings.Join(ridPH, ",")), rids...)
		if puPsRows != nil {
			for puPsRows.Next() {
				var rid interface{}
				var pc, abbr, clr string
				var v int64
				if puPsRows.Scan(&rid, &pc, &abbr, &clr, &v) == nil {
					puPsMap[rid] = append(puPsMap[rid], M{"party_code": pc, "abbreviation": abbr, "color": clr, "votes": v})
				}
			}
			puPsRows.Close()
		}
	}
	for i, pu := range pus {
		if rid, ok := pu["result_id"]; ok && rid != nil {
			if ps, ok := puPsMap[rid]; ok {
				pus[i]["party_scores"] = ps
			} else {
				pus[i]["party_scores"] = []M{}
			}
		} else {
			pus[i]["party_scores"] = []M{}
		}
	}
	w.Header().Set("X-Cache", "MISS")
	cacheSet(cacheKey, M{"states": states, "polling_units": pus}, 15*time.Second)
	writeJSON(w, 200, M{"states": states, "polling_units": pus})
}

func handlePUTile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	z, _ := strconv.Atoi(vars["z"])
	x, _ := strconv.Atoi(vars["x"])
	y, _ := strconv.Atoi(vars["y"])
	eid := queryParamInt(r, "election_id", 1)

	n := math.Pow(2, float64(z))
	lonMin := float64(x)/n*360.0 - 180.0
	lonMax := float64(x+1)/n*360.0 - 180.0
	latMax := math.Atan(math.Sinh(math.Pi*(1-2*float64(y)/n))) * 180.0 / math.Pi
	latMin := math.Atan(math.Sinh(math.Pi*(1-2*float64(y+1)/n))) * 180.0 / math.Pi

	rows, err := dbQueryCtx(r.Context(), `SELECT pu.code, pu.name, pu.latitude as lat, pu.longitude as lon,
		COALESCE(r.status, 'no_result') as status,
		r.submitted_at as submitted_at,
		COALESCE(EXTRACT(EPOCH FROM r.submitted_at)::INTEGER, 0) as submitted_ts
		FROM polling_units pu LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
		WHERE pu.longitude BETWEEN ? AND ? AND pu.latitude BETWEEN ? AND ? LIMIT 10000`,
		eid, lonMin, lonMax, latMin, latMax)
	if err != nil {
		w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
		w.Write(encodeMVTEmpty())
		return
	}

	tile := encodeMVTTile(rows, z, x, y, lonMin, latMin, lonMax, latMax)

	h := sha256.Sum256(tile)
	etag := `W/"` + hex.EncodeToString(h[:]) + `"`
	if inm := r.Header.Get("If-None-Match"); inm == etag {
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=600, stale-while-revalidate=1200")
	w.Write(tile)
}

func handleExportCSV(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	sc := r.URL.Query().Get("state_code")

	q := `SELECT pu.code, pu.name, w.name as ward_name, l.name as lga_name, l.state_code, s.name as state_name,
		COALESCE(r.status,'no_result') as status, pu.registered_voters,
		COALESCE(r.total_valid_votes,0) as total_valid_votes,
		COALESCE(r.total_votes_cast,0) as total_votes_cast, pu.latitude, pu.longitude, r.submitted_at
		FROM polling_units pu JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code
		JOIN states s ON s.code=l.state_code LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?`
	params := []interface{}{eid}
	if sc != "" {
		q += " WHERE l.state_code=?"
		params = append(params, sc)
	}
	rows, _ := dbQueryCtx(r.Context(), q, params...)
	defer rows.Close()

	filename := "polling_units"
	if sc != "" {
		filename += "_" + sc
	}
	filename += ".csv"

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Cache-Control", "no-store")

	writer := csv.NewWriter(w)
	writer.Write([]string{"code", "name", "ward_name", "lga_name", "state_code", "state_name", "status", "registered_voters", "total_valid_votes", "total_votes_cast", "latitude", "longitude", "submitted_at"})

	cols, _ := rows.Columns()
	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		rows.Scan(ptrs...)
		record := make([]string, len(cols))
		for i, v := range vals {
			if v == nil {
				record[i] = ""
			} else {
				record[i] = fmt.Sprintf("%v", v)
			}
		}
		writer.Write(record)
	}
	writer.Flush()
}

func handleExportGeoJSON(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	sc := r.URL.Query().Get("state_code")

	q := `SELECT pu.code, pu.name, pu.latitude, pu.longitude,
		w.name as ward_name, l.name as lga_name, l.state_code, s.name as state_name,
		COALESCE(r.status,'no_result') as status, r.submitted_at
		FROM polling_units pu JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code
		JOIN states s ON s.code=l.state_code LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?`
	params := []interface{}{eid}
	if sc != "" {
		q += " WHERE l.state_code=?"
		params = append(params, sc)
	}
	rows, err := dbQueryCtx(r.Context(), q, params...)
	if err != nil {
		writeError(w, 500, "query failed")
		return
	}
	defer rows.Close()

	filename := "polling_units"
	if sc != "" {
		filename += "_" + sc
	}
	filename += ".geojson"

	w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Cache-Control", "no-store")

	w.Write([]byte(`{"type":"FeatureCollection","features":[`))
	first := true
	enc := json.NewEncoder(w)
	for rows.Next() {
		var code, name, wardName, lgaName, stateCode, stateName, status string
		var lat, lon float64
		var submittedAt sql.NullString
		if err := rows.Scan(&code, &name, &lat, &lon, &wardName, &lgaName, &stateCode, &stateName, &status, &submittedAt); err != nil {
			continue
		}
		if lon == 0 && lat == 0 {
			continue
		}
		if !first {
			w.Write([]byte(","))
		}
		first = false
		sa := ""
		if submittedAt.Valid {
			sa = submittedAt.String
		}
		enc.Encode(M{
			"type":     "Feature",
			"geometry": M{"type": "Point", "coordinates": []float64{lon, lat}},
			"properties": M{
				"code": code, "name": name, "ward": wardName,
				"lga": lgaName, "state": stateName,
				"status": status, "submitted_at": sa,
			},
		})
	}
	w.Write([]byte("]}"))
}

// ── Dashboard ──

func handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)

	cacheKey := fmt.Sprintf("dashboard_stats_%d", eid)
	if cached, err := cacheGet(cacheKey); err == nil && cached != "" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(200)
		w.Write([]byte(cached))
		return
	}

	election, err := querySingleRow("SELECT * FROM elections WHERE id=?", eid)
	if err != nil {
		writeJSON(w, 200, M{"error": "Election not found"})
		return
	}

	var totalPUs, resultsReceived int
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM polling_units").Scan(&totalPUs)
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=?", eid).Scan(&resultsReceived)

	statusRows, _ := dbQueryCtx(r.Context(), "SELECT status, COUNT(*) as count FROM results WHERE election_id=? GROUP BY status", eid)
	statusMap := make(map[string]int)
	for statusRows.Next() {
		var s string
		var c int
		statusRows.Scan(&s, &c)
		statusMap[s] = c
	}
	statusRows.Close()

	var validV, rejectedV, castV, accreditedV sql.NullInt64
	dbQueryRowCtx(r.Context(), `SELECT SUM(total_valid_votes), SUM(rejected_votes), SUM(total_votes_cast), SUM(accredited_voters)
		FROM results WHERE election_id=? AND status IN ('finalized','validated')`, eid).Scan(&validV, &rejectedV, &castV, &accreditedV)

	psRows, _ := dbQueryCtx(r.Context(), `SELECT rps.party_code, p.name as party_name, p.color, p.abbreviation, SUM(rps.votes) as total_votes
		FROM result_party_scores rps JOIN results r ON r.id=rps.result_id JOIN parties p ON p.code=rps.party_code
		WHERE r.election_id=? AND r.status IN ('finalized','validated') GROUP BY rps.party_code, p.name, p.color, p.abbreviation ORDER BY total_votes DESC`, eid)
	partyScores := scanRows(psRows)

	srRows, _ := dbQueryCtx(r.Context(), `SELECT s.code, s.name, s.geo_zone, COUNT(r.id) as results_count, SUM(r.total_valid_votes) as total_votes
		FROM states s LEFT JOIN lgas l ON l.state_code=s.code LEFT JOIN wards w ON w.lga_code=l.code
		LEFT JOIN polling_units pu ON pu.ward_code=w.code
		LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
		GROUP BY s.code, s.name, s.geo_zone, s.capital ORDER BY s.name`, eid)
	stateResults := scanRows(srRows)

	var tbPosted, hlConfirmed int
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=? AND tigerbeetle_status='POSTED'", eid).Scan(&tbPosted)
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM results WHERE election_id=? AND hyperledger_status='CONFIRMED'", eid).Scan(&hlConfirmed)

	zRows, _ := dbQueryCtx(r.Context(), `SELECT s.geo_zone, SUM(r.total_valid_votes) as total_votes, COUNT(r.id) as results_count
		FROM results r JOIN polling_units pu ON pu.code=r.polling_unit_code
		JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code JOIN states s ON s.code=l.state_code
		WHERE r.election_id=? AND r.status IN ('finalized','validated') GROUP BY s.geo_zone`, eid)
	zoneResults := scanRows(zRows)

	comp := 0.0
	if totalPUs > 0 {
		comp = math.Round(float64(resultsReceived)/float64(totalPUs)*10000) / 100
	}
	variance := 0.0
	if resultsReceived > 0 {
		variance = math.Round(math.Abs(float64(tbPosted-hlConfirmed))/float64(resultsReceived)*1000000) / 10000
	}

	result := M{
		"election": election, "total_polling_units": totalPUs, "results_received": resultsReceived,
		"completion_percentage": comp,
		"status_breakdown": M{
			"finalized": statusMap["finalized"], "validated": statusMap["validated"],
			"pending": statusMap["pending"], "disputed": statusMap["disputed"], "voided": statusMap["voided"],
		},
		"vote_totals":  M{"valid": nullInt(validV), "rejected": nullInt(rejectedV), "cast": nullInt(castV), "accredited": nullInt(accreditedV)},
		"party_scores": partyScores, "state_results": stateResults, "zone_results": zoneResults,
		"dual_ledger": M{"tigerbeetle_posted": tbPosted, "hyperledger_confirmed": hlConfirmed, "total_results": resultsReceived, "reconciliation_variance": variance},
	}

	cacheSet(cacheKey, result, 15*time.Second)
	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, 200, result)
}

func handleLiveFeed(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	limit := queryParamInt(r, "limit", 20)
	rows, _ := dbQueryCtx(r.Context(), `SELECT r.id, r.polling_unit_code, r.status, r.total_votes_cast,
		r.tigerbeetle_status, r.hyperledger_status, r.submitted_at,
		pu.name as pu_name, w.name as ward_name, l.name as lga_name, s.name as state_name, s.code as state_code
		FROM results r JOIN polling_units pu ON pu.code=r.polling_unit_code
		JOIN wards w ON w.code=pu.ward_code JOIN lgas l ON l.code=w.lga_code JOIN states s ON s.code=l.state_code
		WHERE r.election_id=? ORDER BY r.submitted_at DESC LIMIT ?`, eid, limit)
	writeJSON(w, 200, scanRows(rows))
}

func collationPartyScoresBatch(ctx context.Context, groupCol string, groupCodes []string, eid int) map[string][]M {
	if len(groupCodes) == 0 {
		return map[string][]M{}
	}
	placeholders := make([]string, len(groupCodes))
	args := make([]interface{}, 0, len(groupCodes)+1)
	for i, c := range groupCodes {
		placeholders[i] = "?"
		args = append(args, c)
	}
	args = append(args, eid)

	var q string
	switch groupCol {
	case "state_code":
		q = fmt.Sprintf(`SELECT l.state_code as group_code, rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
			FROM result_party_scores rps JOIN results res ON res.id=rps.result_id
			JOIN polling_units pu ON pu.code=res.polling_unit_code JOIN wards w ON w.code=pu.ward_code
			JOIN lgas l ON l.code=w.lga_code JOIN parties p ON p.code=rps.party_code
			WHERE l.state_code IN (%s) AND res.election_id=? AND res.status IN ('finalized','validated')
			GROUP BY l.state_code, rps.party_code, p.abbreviation, p.color ORDER BY l.state_code, total_votes DESC`,
			strings.Join(placeholders, ","))
	case "lga_code":
		q = fmt.Sprintf(`SELECT w.lga_code as group_code, rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
			FROM result_party_scores rps JOIN results res ON res.id=rps.result_id
			JOIN polling_units pu ON pu.code=res.polling_unit_code JOIN wards w ON w.code=pu.ward_code
			JOIN parties p ON p.code=rps.party_code
			WHERE w.lga_code IN (%s) AND res.election_id=? AND res.status IN ('finalized','validated')
			GROUP BY w.lga_code, rps.party_code, p.abbreviation, p.color ORDER BY w.lga_code, total_votes DESC`,
			strings.Join(placeholders, ","))
	case "ward_code":
		q = fmt.Sprintf(`SELECT pu.ward_code as group_code, rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
			FROM result_party_scores rps JOIN results res ON res.id=rps.result_id
			JOIN polling_units pu ON pu.code=res.polling_unit_code JOIN parties p ON p.code=rps.party_code
			WHERE pu.ward_code IN (%s) AND res.election_id=? AND res.status IN ('finalized','validated')
			GROUP BY pu.ward_code, rps.party_code, p.abbreviation, p.color ORDER BY pu.ward_code, total_votes DESC`,
			strings.Join(placeholders, ","))
	default:
		return map[string][]M{}
	}

	rows, err := dbQueryCtx(ctx, q, args...)
	if err != nil {
		return map[string][]M{}
	}
	result := map[string][]M{}
	for rows.Next() {
		var groupCode, partyCode, abbreviation, color string
		var totalVotes int64
		if rows.Scan(&groupCode, &partyCode, &abbreviation, &color, &totalVotes) == nil {
			result[groupCode] = append(result[groupCode], M{
				"party_code": partyCode, "abbreviation": abbreviation,
				"color": color, "total_votes": totalVotes,
			})
		}
	}
	rows.Close()
	return result
}

func handleCollation(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	level := queryParam(r, "level", "state")
	parentCode := r.URL.Query().Get("parent_code")

	cacheKey := fmt.Sprintf("collation_%s_%d_%s", level, eid, parentCode)
	if cached, err := cacheGet(cacheKey); err == nil && cached != "" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(200)
		w.Write([]byte(cached))
		return
	}

	switch level {
	case "state":
		rows, _ := dbQueryCtx(r.Context(), `SELECT s.code, s.name, s.geo_zone,
			COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
			SUM(r.total_valid_votes) as total_valid_votes, SUM(r.rejected_votes) as rejected_votes,
			SUM(r.total_votes_cast) as total_votes_cast
			FROM states s LEFT JOIN lgas l ON l.state_code=s.code LEFT JOIN wards w ON w.lga_code=l.code
			LEFT JOIN polling_units pu ON pu.ward_code=w.code
			LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
			GROUP BY s.code, s.name, s.geo_zone, s.capital ORDER BY s.name`, eid)
		results := scanRows(rows)
		codes := make([]string, len(results))
		for i, res := range results {
			codes[i], _ = res["code"].(string)
		}
		psMap := collationPartyScoresBatch(r.Context(), "state_code", codes, eid)
		for i, res := range results {
			code, _ := res["code"].(string)
			if ps, ok := psMap[code]; ok {
				results[i]["party_scores"] = ps
			} else {
				results[i]["party_scores"] = []M{}
			}
		}
		w.Header().Set("X-Cache", "MISS")
		cacheSet(cacheKey, results, 15*time.Second)
		writeJSON(w, 200, results)

	case "lga":
		rows, _ := dbQueryCtx(r.Context(), `SELECT l.code, l.name,
			COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
			SUM(r.total_valid_votes) as total_valid_votes, SUM(r.rejected_votes) as rejected_votes,
			SUM(r.total_votes_cast) as total_votes_cast
			FROM lgas l LEFT JOIN wards w ON w.lga_code=l.code LEFT JOIN polling_units pu ON pu.ward_code=w.code
			LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
			WHERE l.state_code=? GROUP BY l.code, l.name ORDER BY l.name`, eid, parentCode)
		results := scanRows(rows)
		codes := make([]string, len(results))
		for i, res := range results {
			codes[i], _ = res["code"].(string)
		}
		psMap := collationPartyScoresBatch(r.Context(), "lga_code", codes, eid)
		for i, res := range results {
			code, _ := res["code"].(string)
			if ps, ok := psMap[code]; ok {
				results[i]["party_scores"] = ps
			} else {
				results[i]["party_scores"] = []M{}
			}
		}
		w.Header().Set("X-Cache", "MISS")
		cacheSet(cacheKey, results, 15*time.Second)
		writeJSON(w, 200, results)

	case "ward":
		rows, _ := dbQueryCtx(r.Context(), `SELECT w.code, w.name,
			COUNT(DISTINCT pu.code) as total_pus, COUNT(DISTINCT r.id) as reported_pus,
			SUM(r.total_valid_votes) as total_valid_votes, SUM(r.rejected_votes) as rejected_votes,
			SUM(r.total_votes_cast) as total_votes_cast
			FROM wards w LEFT JOIN polling_units pu ON pu.ward_code=w.code
			LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
			WHERE w.lga_code=? GROUP BY w.code, w.name ORDER BY w.name`, eid, parentCode)
		results := scanRows(rows)
		codes := make([]string, len(results))
		for i, res := range results {
			codes[i], _ = res["code"].(string)
		}
		psMap := collationPartyScoresBatch(r.Context(), "ward_code", codes, eid)
		for i, res := range results {
			code, _ := res["code"].(string)
			if ps, ok := psMap[code]; ok {
				results[i]["party_scores"] = ps
			} else {
				results[i]["party_scores"] = []M{}
			}
		}
		w.Header().Set("X-Cache", "MISS")
		cacheSet(cacheKey, results, 15*time.Second)
		writeJSON(w, 200, results)

	case "pu":
		rows, _ := dbQueryCtx(r.Context(), `SELECT pu.code, pu.name, pu.registered_voters,
			r.id as result_id, r.status, r.total_valid_votes, r.rejected_votes,
			r.total_votes_cast, r.accredited_voters, r.tigerbeetle_status, r.hyperledger_status
			FROM polling_units pu LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
			WHERE pu.ward_code=? ORDER BY pu.name`, eid, parentCode)
		results := scanRows(rows)
		rids := make([]interface{}, 0, len(results))
		ridPlaceholders := make([]string, 0, len(results))
		for _, res := range results {
			if rid, ok := res["result_id"]; ok && rid != nil {
				rids = append(rids, rid)
				ridPlaceholders = append(ridPlaceholders, "?")
			}
		}
		psMap := map[interface{}][]M{}
		if len(rids) > 0 {
			psRows, _ := dbQueryCtx(r.Context(), fmt.Sprintf(`SELECT rps.result_id, rps.party_code, p.abbreviation, p.color, rps.votes
				FROM result_party_scores rps JOIN parties p ON p.code=rps.party_code
				WHERE rps.result_id IN (%s) ORDER BY rps.result_id, rps.votes DESC`,
				strings.Join(ridPlaceholders, ",")), rids...)
			if psRows != nil {
				for psRows.Next() {
					var rid interface{}
					var partyCode, abbreviation, color string
					var votes int64
					if psRows.Scan(&rid, &partyCode, &abbreviation, &color, &votes) == nil {
						psMap[rid] = append(psMap[rid], M{
							"party_code": partyCode, "abbreviation": abbreviation,
							"color": color, "votes": votes,
						})
					}
				}
				psRows.Close()
			}
		}
		for i, res := range results {
			if rid, ok := res["result_id"]; ok && rid != nil {
				if ps, ok := psMap[rid]; ok {
					results[i]["party_scores"] = ps
				} else {
					results[i]["party_scores"] = []M{}
				}
			} else {
				results[i]["party_scores"] = []M{}
			}
		}
		w.Header().Set("X-Cache", "MISS")
		cacheSet(cacheKey, results, 15*time.Second)
		writeJSON(w, 200, results)

	default:
		writeJSON(w, 200, []M{})
	}
}

func handlePostClientMetric(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	ua := r.Header.Get("User-Agent")
	ip := r.RemoteAddr
	event, _ := payload["event"].(string)
	if event == "" {
		event = "unknown"
	}
	dataJSON, _ := json.Marshal(payload["data"])
	dbExecCtx(r.Context(), "INSERT INTO metrics_client (ts, event, data, ua, ip) VALUES (?,?,?,?,?)",
		time.Now().UTC().Format(time.RFC3339)+"Z", event, string(dataJSON), ua, ip)
	writeJSON(w, 200, M{"ok": true})
}

func handleRecentClientMetrics(w http.ResponseWriter, r *http.Request) {
	limit := queryParamInt(r, "limit", 50)
	rows, _ := dbQueryCtx(r.Context(), "SELECT * FROM metrics_client ORDER BY id DESC LIMIT ?", limit)
	writeJSON(w, 200, scanRows(rows))
}

// ── Audit ──

func handleAuditTrail(w http.ResponseWriter, r *http.Request) {
	et := r.URL.Query().Get("entity_type")
	eid := r.URL.Query().Get("entity_id")
	action := r.URL.Query().Get("action")
	limit := queryParamInt(r, "limit", 50)
	offset := queryParamInt(r, "offset", 0)

	q := "SELECT a.*, u.username, u.full_name FROM audit_log a LEFT JOIN users u ON u.id=a.user_id WHERE 1=1"
	var params []interface{}
	if et != "" {
		q += " AND a.entity_type=?"
		params = append(params, et)
	}
	if eid != "" {
		q += " AND a.entity_id=?"
		params = append(params, eid)
	}
	if action != "" {
		q += " AND a.action=?"
		params = append(params, action)
	}
	countQ := strings.Replace(q, "SELECT a.*, u.username, u.full_name", "SELECT COUNT(*) as total", 1)
	var total int
	dbQueryRowCtx(r.Context(), countQ, params...).Scan(&total)

	q += " ORDER BY a.timestamp DESC LIMIT ? OFFSET ?"
	params = append(params, limit, offset)
	rows, _ := dbQueryCtx(r.Context(), q, params...)
	writeJSON(w, 200, M{"total": total, "entries": scanRows(rows)})
}

func handleVerifyResult(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	rows, _ := dbQueryCtx(r.Context(), "SELECT * FROM audit_log WHERE entity_type='result' AND entity_id=? ORDER BY timestamp ASC", id)
	entries := scanRows(rows)

	chainValid := true
	for i := 1; i < len(entries); i++ {
		prev, _ := entries[i]["prev_block_hash"].(string)
		hash, _ := entries[i-1]["block_hash"].(string)
		if prev != hash {
			chainValid = false
			break
		}
	}

	resRow, err := querySingleRow("SELECT * FROM results WHERE id=?", id)
	var dualLedger interface{}
	if err == nil {
		dualLedger = M{
			"tigerbeetle_status":      resRow["tigerbeetle_status"],
			"hyperledger_status":      resRow["hyperledger_status"],
			"tigerbeetle_transfer_id": resRow["tigerbeetle_transfer_id"],
			"hyperledger_tx_id":       resRow["hyperledger_tx_id"],
		}
	}

	writeJSON(w, 200, M{
		"result_id": id, "audit_entries": entries, "chain_valid": chainValid,
		"result_status": resRow, "dual_ledger": dualLedger,
	})
}

func handleAuditStats(w http.ResponseWriter, r *http.Request) {
	rows, _ := dbQueryCtx(r.Context(), "SELECT action, COUNT(*) as count FROM audit_log GROUP BY action ORDER BY count DESC")
	actionCounts := scanRows(rows)
	var total int
	dbQueryRowCtx(r.Context(), "SELECT COUNT(*) FROM audit_log").Scan(&total)
	var latestHash sql.NullString
	dbQueryRowCtx(r.Context(), "SELECT block_hash FROM audit_log ORDER BY id DESC LIMIT 1").Scan(&latestHash)
	writeJSON(w, 200, M{"total_entries": total, "action_counts": actionCounts, "latest_block_hash": nullStr(latestHash)})
}

// ── Incidents ──

func handleCreateIncident(w http.ResponseWriter, r *http.Request) {
	user, err := getCurrentUser(r)
	if err != nil {
		writeError(w, 401, "Not authenticated")
		return
	}
	var req IncidentReport
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Severity == "" {
		req.Severity = "medium"
	}
	userSub, _ := user["sub"].(string)
	uid, _ := strconv.Atoi(userSub)

	tx, txErr := db.Begin()
	if txErr != nil {
		writeError(w, 500, "transaction start failed")
		return
	}
	res, insErr := tx.Exec("INSERT INTO incidents (election_id, polling_unit_code, reported_by, incident_type, description, severity) VALUES (?,?,?,?,?,?)",
		req.ElectionID, req.PollingUnitCode, uid, req.IncidentType, req.Description, req.Severity)
	if insErr != nil {
		tx.Rollback()
		writeError(w, 500, "failed to create incident")
		return
	}
	lid, _ := res.LastInsertId()
	if commitErr := tx.Commit(); commitErr != nil {
		writeError(w, 500, "transaction commit failed")
		return
	}
	auditWrite("INCIDENT_CREATED", "incident", fmt.Sprintf("%d", lid), r, map[string]interface{}{"type": req.IncidentType, "severity": req.Severity})

	go func() {
		ctx := context.Background()
		if err := mwHub.Kafka.Produce(ctx, KafkaMessage{
			Topic: TopicIncidentReport,
			Key:   fmt.Sprintf("incident-%d", lid),
			Value: map[string]interface{}{"id": lid, "election_id": req.ElectionID, "type": req.IncidentType, "severity": req.Severity},
		}); err != nil {
			log.Error().Err(err).Str("topic", TopicIncidentReport).Msg("Kafka produce failed")
		} else {
			log.Info().Str("topic", TopicIncidentReport).Int64("id", lid).Msg("Kafka produce success")
		}
	}()

	writeJSON(w, 200, M{"id": lid, "message": "Incident reported"})
}

func handleListIncidents(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	status := r.URL.Query().Get("status")
	severity := r.URL.Query().Get("severity")
	limit := queryParamInt(r, "limit", 50)
	offset := queryParamInt(r, "offset", 0)

	q := "SELECT i.*, u.full_name as reporter_name FROM incidents i LEFT JOIN users u ON u.id=i.reported_by WHERE i.election_id=?"
	params := []interface{}{eid}
	if status != "" {
		q += " AND i.status=?"
		params = append(params, status)
	}
	if severity != "" {
		q += " AND i.severity=?"
		params = append(params, severity)
	}
	q += " ORDER BY i.reported_at DESC LIMIT ? OFFSET ?"
	params = append(params, limit, offset)
	rows, _ := dbQueryCtx(r.Context(), q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleUpdateIncident(w http.ResponseWriter, r *http.Request) {
	if _, err := getCurrentUser(r); err != nil {
		writeError(w, 401, "Not authenticated")
		return
	}
	id := mux.Vars(r)["id"]
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Status == "" {
		req.Status = r.URL.Query().Get("status")
	}
	if req.Status == "" {
		writeError(w, 400, "status is required")
		return
	}
	resolved := ""
	if req.Status == "resolved" {
		resolved = ", resolved_at=CURRENT_TIMESTAMP"
	}
	dbExecCtx(r.Context(), "UPDATE incidents SET status=?"+resolved+" WHERE id=?", req.Status, id)
	auditWrite("INCIDENT_UPDATED", "incident", id, r, map[string]interface{}{"status": req.Status})
	writeJSON(w, 200, M{"message": "Incident updated"})
}

// ── Parties ──

func handleListParties(w http.ResponseWriter, r *http.Request) {
	rows, _ := dbQueryCtx(r.Context(), "SELECT * FROM parties WHERE is_active=1 ORDER BY name")
	writeJSON(w, 200, scanRows(rows))
}

// ── Helpers ──

func nullStr(ns sql.NullString) interface{} {
	if ns.Valid {
		return ns.String
	}
	return nil
}

func nullInt(ni sql.NullInt64) int64 {
	if ni.Valid {
		return ni.Int64
	}
	return 0
}

func scanRows(rows *sql.Rows) []M {
	if rows == nil {
		return []M{}
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return []M{}
	}
	var result []M
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := make(M, len(cols))
		for i, col := range cols {
			v := vals[i]
			switch vv := v.(type) {
			case []byte:
				row[col] = string(vv)
			default:
				row[col] = vv
			}
		}
		result = append(result, row)
	}
	if result == nil {
		return []M{}
	}
	return result
}