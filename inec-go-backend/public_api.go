package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

func initPublicAPITables(database *sql.DB) {
	database.Exec(`CREATE TABLE IF NOT EXISTS api_keys (
		id SERIAL PRIMARY KEY,
		key_hash TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		owner TEXT NOT NULL,
		permissions TEXT NOT NULL DEFAULT 'read',
		rate_limit INTEGER NOT NULL DEFAULT 100,
		is_active INTEGER NOT NULL DEFAULT 1,
		last_used_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	database.Exec(`CREATE TABLE IF NOT EXISTS api_usage (
		id SERIAL PRIMARY KEY,
		api_key_id INTEGER,
		endpoint TEXT NOT NULL,
		method TEXT NOT NULL,
		status_code INTEGER,
		response_ms REAL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	database.Exec(`CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash)`)
	database.Exec(`CREATE INDEX IF NOT EXISTS idx_api_usage_key ON api_usage(api_key_id)`)

	var count int
	database.QueryRow("SELECT COUNT(*) FROM api_keys").Scan(&count)
	if count == 0 {
		b := make([]byte, 32)
		rand.Read(b)
		demoKey := "inec_" + hex.EncodeToString(b[:16])
		database.Exec(`INSERT INTO api_keys (key_hash, name, owner, permissions, rate_limit)
			VALUES (?, 'Demo API Key', 'system', 'read', 1000)`, demoKey)
	}
}

func apiKeyAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			key = r.URL.Query().Get("api_key")
		}
		if key == "" {
			writeJSON(w, 401, M{"error": "API key required. Pass X-API-Key header or api_key query param."})
			return
		}

		var keyID int
		var name, permissions string
		var rateLimit int
		var isActive int
		err := db.QueryRow("SELECT id, name, permissions, rate_limit, is_active FROM api_keys WHERE key_hash=?", key).
			Scan(&keyID, &name, &permissions, &rateLimit, &isActive)
		if err != nil || isActive == 0 {
			writeJSON(w, 403, M{"error": "Invalid or inactive API key"})
			return
		}

		if !rateLimiter.allow(fmt.Sprintf("apikey:%d", keyID), rateLimit, time.Minute) {
			writeJSON(w, 429, M{"error": "Rate limit exceeded", "limit": rateLimit, "window": "1 minute"})
			return
		}

		db.Exec("UPDATE api_keys SET last_used_at=CURRENT_TIMESTAMP WHERE id=?", keyID)

		start := time.Now()
		next.ServeHTTP(w, r)
		elapsed := time.Since(start).Seconds() * 1000
		db.Exec(`INSERT INTO api_usage (api_key_id, endpoint, method, status_code, response_ms)
			VALUES (?,?,?,200,?)`, keyID, r.URL.Path, r.Method, elapsed)
	}
}

func handlePublicAPIElections(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, title, election_type, election_date, status, created_at FROM elections ORDER BY election_date DESC`)
	if err != nil {
		writeError(w, 500, "query failed")
		return
	}
	defer rows.Close()
	elections := []M{}
	for rows.Next() {
		var id int
		var name, etype, date, status, created string
		rows.Scan(&id, &name, &etype, &date, &status, &created)
		elections = append(elections, M{"id": id, "name": name, "type": etype, "date": date, "status": status, "created_at": created})
	}
	writeJSON(w, 200, M{"data": elections, "count": len(elections), "api_version": "v1"})
}

func handlePublicAPIResults(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)
	stateCode := queryParam(r, "state_code", "")
	status := queryParam(r, "status", "")
	limit := queryParamInt(r, "limit", 50)
	offset := queryParamInt(r, "offset", 0)

	query := `SELECT r.id, r.election_id, r.polling_unit_code, pu.name as pu_name,
		l.state_code, w.lga_code,
		r.total_votes_cast, r.total_valid_votes, r.rejected_votes,
		r.accredited_voters, r.status, r.submitted_at
		FROM results r
		JOIN polling_units pu ON r.polling_unit_code=pu.code
		JOIN wards w ON pu.ward_code=w.code
		JOIN lgas l ON w.lga_code=l.code
		WHERE r.election_id=?`
	args := []interface{}{electionID}

	if stateCode != "" {
		query += " AND l.state_code=?"
		args = append(args, stateCode)
	}
	if status != "" {
		query += " AND r.status=?"
		args = append(args, status)
	}
	query += " ORDER BY r.submitted_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		writeError(w, 500, "query failed")
		return
	}
	defer rows.Close()

	results := []M{}
	for rows.Next() {
		var id, eid, totalCast, totalValid, rejected, accredited int
		var puCode, puName, sc, lc, st, submitted string
		rows.Scan(&id, &eid, &puCode, &puName, &sc, &lc, &totalCast, &totalValid, &rejected, &accredited, &st, &submitted)
		results = append(results, M{
			"id": id, "election_id": eid, "polling_unit_code": puCode,
			"polling_unit_name": puName, "state_code": sc, "lga_code": lc,
			"total_votes_cast": totalCast, "total_valid_votes": totalValid,
			"rejected_votes": rejected, "accredited_voters": accredited,
			"status": st, "submitted_at": submitted,
		})
	}

	var total int
	db.QueryRow("SELECT COUNT(*) FROM results WHERE election_id=?", electionID).Scan(&total)

	writeJSON(w, 200, M{
		"data": results, "count": len(results), "total": total,
		"limit": limit, "offset": offset, "api_version": "v1",
	})
}

func handlePublicAPIResultDetail(w http.ResponseWriter, r *http.Request) {
	id := muxVarInt(r, "id")
	var rid, eid, totalCast, totalValid, rejected, accredited int
	var puCode, st, submitted, tbStatus, hlStatus string
	err := db.QueryRow(`SELECT id, election_id, polling_unit_code, total_votes_cast,
		total_valid_votes, rejected_votes, accredited_voters, status, submitted_at,
		tigerbeetle_status, hyperledger_status
		FROM results WHERE id=?`, id).Scan(
		&rid, &eid, &puCode, &totalCast, &totalValid, &rejected, &accredited,
		&st, &submitted, &tbStatus, &hlStatus)
	if err != nil {
		writeError(w, 404, "result not found")
		return
	}

	voteRows, _ := db.Query(`SELECT p.abbreviation, rps.votes FROM result_party_scores rps
		JOIN parties p ON rps.party_code=p.code WHERE rps.result_id=? ORDER BY rps.votes DESC`, id)
	defer voteRows.Close()
	votes := []M{}
	for voteRows.Next() {
		var party string
		var v int
		voteRows.Scan(&party, &v)
		votes = append(votes, M{"party": party, "votes": v})
	}

	writeJSON(w, 200, M{
		"data": M{
			"id": rid, "election_id": eid, "polling_unit_code": puCode,
			"total_votes_cast": totalCast, "total_valid_votes": totalValid,
			"rejected_votes": rejected, "accredited_voters": accredited,
			"status": st, "submitted_at": submitted,
			"tigerbeetle_status": tbStatus, "hyperledger_status": hlStatus,
			"party_votes": votes,
		},
		"api_version": "v1",
	})
}

func handlePublicAPIStates(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT code, name, geo_zone, capital FROM states ORDER BY name")
	defer rows.Close()
	states := []M{}
	for rows.Next() {
		var code, name, zone, capital string
		rows.Scan(&code, &name, &zone, &capital)
		states = append(states, M{"code": code, "name": name, "geo_zone": zone, "capital": capital})
	}
	writeJSON(w, 200, M{"data": states, "count": len(states), "api_version": "v1"})
}

func handlePublicAPIPollingUnits(w http.ResponseWriter, r *http.Request) {
	stateCode := queryParam(r, "state_code", "")
	lgaCode := queryParam(r, "lga_code", "")
	limit := queryParamInt(r, "limit", 100)
	offset := queryParamInt(r, "offset", 0)

	query := `SELECT pu.code, pu.name, l.state_code, w.lga_code, pu.ward_code, pu.registered_voters, pu.latitude, pu.longitude
		FROM polling_units pu
		JOIN wards w ON pu.ward_code=w.code
		JOIN lgas l ON w.lga_code=l.code
		WHERE 1=1`
	args := []interface{}{}
	if stateCode != "" {
		query += " AND l.state_code=?"
		args = append(args, stateCode)
	}
	if lgaCode != "" {
		query += " AND w.lga_code=?"
		args = append(args, lgaCode)
	}
	query += " ORDER BY pu.code LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, _ := db.Query(query, args...)
	defer rows.Close()
	pus := []M{}
	for rows.Next() {
		var code, name, sc, lc, wc string
		var reg int
		var lat, lng float64
		rows.Scan(&code, &name, &sc, &lc, &wc, &reg, &lat, &lng)
		pus = append(pus, M{"code": code, "name": name, "state_code": sc, "lga_code": lc,
			"ward_code": wc, "registered_voters": reg, "latitude": lat, "longitude": lng})
	}
	writeJSON(w, 200, M{"data": pus, "count": len(pus), "limit": limit, "offset": offset, "api_version": "v1"})
}

func handlePublicAPICollation(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)
	level := queryParam(r, "level", "national")
	parentCode := queryParam(r, "parent_code", "")

	var query string
	var args []interface{}

	switch level {
	case "state":
		query = `SELECT l.state_code as code, s.name,
			COUNT(DISTINCT r.id) as results_count,
			COALESCE(SUM(r.total_valid_votes),0) as total_votes
			FROM results r
			JOIN polling_units pu ON r.polling_unit_code=pu.code
			JOIN wards w ON pu.ward_code=w.code
			JOIN lgas l ON w.lga_code=l.code
			JOIN states s ON l.state_code=s.code
			WHERE r.election_id=? GROUP BY l.state_code ORDER BY total_votes DESC`
		args = []interface{}{electionID}
	default:
		query = `SELECT p.abbreviation as party, p.color,
			SUM(rps.votes) as total_votes
			FROM result_party_scores rps
			JOIN results r ON rps.result_id=r.id
			JOIN parties p ON rps.party_code=p.code
			WHERE r.election_id=?`
		args = []interface{}{electionID}
		if parentCode != "" {
			query += ` AND r.polling_unit_code IN (SELECT pu.code FROM polling_units pu
				JOIN wards w ON pu.ward_code=w.code
				JOIN lgas l ON w.lga_code=l.code WHERE l.state_code=?)`
			args = append(args, parentCode)
		}
		query += " GROUP BY p.code ORDER BY total_votes DESC"
	}

	rows, _ := db.Query(query, args...)
	defer rows.Close()
	data := []M{}
	cols, _ := rows.Columns()
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		row := M{}
		for i, col := range cols {
			row[col] = vals[i]
		}
		data = append(data, row)
	}

	writeJSON(w, 200, M{"data": data, "level": level, "election_id": electionID, "api_version": "v1"})
}

func handlePublicAPIKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Name  string `json:"name"`
			Owner string `json:"owner"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, 400, "invalid JSON"); return }
		if req.Name == "" || req.Owner == "" {
			writeError(w, 400, "name and owner required")
			return
		}
		b := make([]byte, 32)
		rand.Read(b)
		key := "inec_" + hex.EncodeToString(b[:16])
		db.Exec(`INSERT INTO api_keys (key_hash, name, owner, permissions, rate_limit)
			VALUES (?, ?, ?, 'read', 100)`, key, req.Name, req.Owner)
		writeJSON(w, 201, M{"api_key": key, "name": req.Name, "owner": req.Owner,
			"permissions": "read", "rate_limit": 100, "note": "Store this key securely. It cannot be retrieved later."})
		return
	}

	rows, _ := db.Query(`SELECT id, name, owner, permissions, rate_limit, is_active, last_used_at, created_at
		FROM api_keys ORDER BY created_at DESC`)
	defer rows.Close()
	keys := []M{}
	for rows.Next() {
		var id, rl, active int
		var name, owner, perms string
		var lastUsed, created sql.NullString
		rows.Scan(&id, &name, &owner, &perms, &rl, &active, &lastUsed, &created)
		keys = append(keys, M{
			"id": id, "name": name, "owner": owner, "permissions": perms,
			"rate_limit": rl, "is_active": active == 1,
			"last_used_at": lastUsed.String, "created_at": created.String,
		})
	}
	writeJSON(w, 200, M{"data": keys, "count": len(keys)})
}

func handlePublicAPIUsage(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query(`SELECT au.endpoint, au.method, COUNT(*) as calls,
		ROUND(AVG(au.response_ms),2) as avg_ms,
		MAX(au.created_at) as last_call
		FROM api_usage au
		GROUP BY au.endpoint, au.method
		ORDER BY calls DESC LIMIT 50`)
	defer rows.Close()
	data := []M{}
	for rows.Next() {
		var endpoint, method, lastCall string
		var calls int
		var avgMs float64
		rows.Scan(&endpoint, &method, &calls, &avgMs, &lastCall)
		data = append(data, M{"endpoint": endpoint, "method": method, "calls": calls, "avg_ms": avgMs, "last_call": lastCall})
	}

	var totalCalls int
	db.QueryRow("SELECT COUNT(*) FROM api_usage").Scan(&totalCalls)

	writeJSON(w, 200, M{"data": data, "total_calls": totalCalls})
}

func handlePublicAPIDocs(w http.ResponseWriter, r *http.Request) {
	docs := M{
		"openapi": "3.0.3",
		"info": M{
			"title":       "INEC Election Platform Public API",
			"version":     "1.0.0",
			"description": "Public API for third-party verification and monitoring of Nigerian election results",
		},
		"servers": []M{{"url": "/api/v1"}},
		"security": []M{{"ApiKeyAuth": []string{}}},
		"components": M{
			"securitySchemes": M{
				"ApiKeyAuth": M{
					"type": "apiKey", "in": "header", "name": "X-API-Key",
				},
			},
		},
		"paths": M{
			"/elections": M{
				"get": M{"summary": "List all elections", "tags": []string{"Elections"},
					"responses": M{"200": M{"description": "List of elections"}}},
			},
			"/results": M{
				"get": M{"summary": "List results with filtering", "tags": []string{"Results"},
					"parameters": []M{
						{"name": "election_id", "in": "query", "schema": M{"type": "integer"}},
						{"name": "state_code", "in": "query", "schema": M{"type": "string"}},
						{"name": "status", "in": "query", "schema": M{"type": "string"}},
						{"name": "limit", "in": "query", "schema": M{"type": "integer", "default": 50}},
						{"name": "offset", "in": "query", "schema": M{"type": "integer", "default": 0}},
					},
					"responses": M{"200": M{"description": "Paginated results"}}},
			},
			"/results/{id}": M{
				"get": M{"summary": "Get result detail with party votes", "tags": []string{"Results"},
					"parameters": []M{{"name": "id", "in": "path", "required": true, "schema": M{"type": "integer"}}},
					"responses": M{"200": M{"description": "Result detail"}}},
			},
			"/states": M{
				"get": M{"summary": "List all states", "tags": []string{"Geography"},
					"responses": M{"200": M{"description": "List of states"}}},
			},
			"/polling-units": M{
				"get": M{"summary": "List polling units with filtering", "tags": []string{"Geography"},
					"parameters": []M{
						{"name": "state_code", "in": "query", "schema": M{"type": "string"}},
						{"name": "lga_code", "in": "query", "schema": M{"type": "string"}},
						{"name": "limit", "in": "query", "schema": M{"type": "integer", "default": 100}},
					},
					"responses": M{"200": M{"description": "Paginated polling units"}}},
			},
			"/collation": M{
				"get": M{"summary": "Get collation data", "tags": []string{"Collation"},
					"parameters": []M{
						{"name": "election_id", "in": "query", "schema": M{"type": "integer"}},
						{"name": "level", "in": "query", "schema": M{"type": "string", "enum": []string{"national", "state"}}},
					},
					"responses": M{"200": M{"description": "Collation data"}}},
			},
			"/ai/anomalies": M{
				"get": M{"summary": "AI-powered anomaly detection", "tags": []string{"AI Analytics"},
					"parameters": []M{
						{"name": "election_id", "in": "query", "schema": M{"type": "integer"}},
						{"name": "severity", "in": "query", "schema": M{"type": "string"}},
					},
					"responses": M{"200": M{"description": "Anomaly detection results with Benford analysis"}}},
			},
			"/ai/integrity": M{
				"get": M{"summary": "Election integrity score (0-100)", "tags": []string{"AI Analytics"},
					"parameters": []M{{"name": "election_id", "in": "query", "schema": M{"type": "integer"}}},
					"responses": M{"200": M{"description": "Composite integrity score"}}},
			},
		},
	}

	if strings.Contains(r.URL.Path, ".json") || r.URL.Query().Get("format") == "json" {
		writeJSON(w, 200, docs)
	} else {
		writeJSON(w, 200, docs)
	}
}

func muxVarInt(r *http.Request, key string) int {
	v := mux.Vars(r)[key]
	var i int
	fmt.Sscanf(v, "%d", &i)
	return i
}
