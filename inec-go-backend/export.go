package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// handleExportResults exports election results in CSV or JSON format.
func handleExportResults(w http.ResponseWriter, r *http.Request) {
	format := queryParam(r, "format", "json")
	electionID := queryParamInt(r, "election_id", 1)
	stateCode := queryParam(r, "state_code", "")

	query := `SELECT r.polling_unit_code, pu.name as pu_name, w.name as ward_name,
		l.name as lga_name, r.status, r.total_valid_votes, r.rejected_votes, r.total_votes_cast,
		r.accredited_voters, r.submitted_at
		FROM results r
		JOIN polling_units pu ON r.polling_unit_code = pu.code
		JOIN wards w ON pu.ward_code = w.code
		JOIN lgas l ON w.lga_code = l.code
		WHERE r.election_id = $1`
	args := []interface{}{electionID}
	if stateCode != "" {
		query += " AND l.state_code = $2"
		args = append(args, stateCode)
	}
	query += " ORDER BY r.polling_unit_code"

	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	type ResultRow struct {
		PollingUnitCode string `json:"polling_unit_code" csv:"polling_unit_code"`
		PUName          string `json:"pu_name" csv:"pu_name"`
		WardName        string `json:"ward_name" csv:"ward_name"`
		LGAName         string `json:"lga_name" csv:"lga_name"`
		Status          string `json:"status" csv:"status"`
		TotalValid      int    `json:"total_valid_votes" csv:"total_valid_votes"`
		Rejected        int    `json:"rejected_votes" csv:"rejected_votes"`
		TotalCast       int    `json:"total_votes_cast" csv:"total_votes_cast"`
		Accredited      int    `json:"accredited_voters" csv:"accredited_voters"`
		SubmittedAt     string `json:"submitted_at" csv:"submitted_at"`
	}

	var results []ResultRow
	for rows.Next() {
		var row ResultRow
		if err := rows.Scan(&row.PollingUnitCode, &row.PUName, &row.WardName, &row.LGAName,
			&row.Status, &row.TotalValid, &row.Rejected, &row.TotalCast,
			&row.Accredited, &row.SubmittedAt); err != nil {
			continue
		}
		results = append(results, row)
	}

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=results_election_%d_%s.csv", electionID, time.Now().Format("20060102")))
		writer := csv.NewWriter(w)
		writer.Write([]string{"polling_unit_code", "pu_name", "ward_name", "lga_name", "status",
			"total_valid_votes", "rejected_votes", "total_votes_cast", "accredited_voters", "submitted_at"})
		for _, row := range results {
			writer.Write([]string{
				row.PollingUnitCode, row.PUName, row.WardName, row.LGAName, row.Status,
				fmt.Sprintf("%d", row.TotalValid), fmt.Sprintf("%d", row.Rejected),
				fmt.Sprintf("%d", row.TotalCast), fmt.Sprintf("%d", row.Accredited), row.SubmittedAt,
			})
		}
		writer.Flush()
	default:
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=results_election_%d_%s.json", electionID, time.Now().Format("20060102")))
		writeJSON(w, 200, M{
			"election_id": electionID,
			"exported_at": time.Now().UTC().Format(time.RFC3339),
			"total":       len(results),
			"results":     results,
		})
	}
}

// handleExportVoters exports voter registration data.
func handleExportVoters(w http.ResponseWriter, r *http.Request) {
	format := queryParam(r, "format", "json")
	stateCode := queryParam(r, "state_code", "")
	limit := queryParamInt(r, "limit", 10000)

	query := `SELECT v.vin, v.full_name, v.date_of_birth, v.gender, v.phone,
		v.polling_unit_code, v.status, v.created_at
		FROM voters v`
	args := []interface{}{}
	if stateCode != "" {
		query += ` JOIN polling_units pu ON v.polling_unit_code = pu.code
			JOIN wards w ON pu.ward_code = w.code
			JOIN lgas l ON w.lga_code = l.code
			WHERE l.state_code = $1`
		args = append(args, stateCode)
	}
	query += fmt.Sprintf(" ORDER BY v.id LIMIT %d", limit)

	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	type VoterRow struct {
		VIN             string `json:"vin"`
		FullName        string `json:"full_name"`
		DOB             string `json:"date_of_birth"`
		Gender          string `json:"gender"`
		Phone           string `json:"phone"`
		PollingUnitCode string `json:"polling_unit_code"`
		Status          string `json:"status"`
		CreatedAt       string `json:"created_at"`
	}

	var voters []VoterRow
	for rows.Next() {
		var v VoterRow
		if err := rows.Scan(&v.VIN, &v.FullName, &v.DOB, &v.Gender, &v.Phone,
			&v.PollingUnitCode, &v.Status, &v.CreatedAt); err != nil {
			continue
		}
		voters = append(voters, v)
	}

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=voters_export.csv")
		writer := csv.NewWriter(w)
		writer.Write([]string{"vin", "full_name", "date_of_birth", "gender", "phone", "polling_unit_code", "status", "created_at"})
		for _, v := range voters {
			writer.Write([]string{v.VIN, v.FullName, v.DOB, v.Gender, v.Phone, v.PollingUnitCode, v.Status, v.CreatedAt})
		}
		writer.Flush()
	default:
		writeJSON(w, 200, M{"total": len(voters), "voters": voters, "exported_at": time.Now().UTC().Format(time.RFC3339)})
	}
}

// handleExportCollation exports collation results at any level.
func handleExportCollation(w http.ResponseWriter, r *http.Request) {
	level := queryParam(r, "level", "state")
	electionID := queryParamInt(r, "election_id", 1)

	rows, err := db.QueryContext(r.Context(),
		`SELECT rps.party_code, SUM(rps.votes) as total_votes, COUNT(DISTINCT r.polling_unit_code) as pu_count
		 FROM results r
		 JOIN result_party_scores rps ON rps.result_id = r.id
		 WHERE r.election_id = $1 AND r.status IN ('pending','validated','finalized')
		 GROUP BY rps.party_code ORDER BY total_votes DESC`, electionID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	type PartyTotal struct {
		PartyCode  string `json:"party_code"`
		TotalVotes int64  `json:"total_votes"`
		PUCount    int    `json:"pu_count"`
	}
	var parties []PartyTotal
	for rows.Next() {
		var p PartyTotal
		if rows.Scan(&p.PartyCode, &p.TotalVotes, &p.PUCount) == nil {
			parties = append(parties, p)
		}
	}

	format := queryParam(r, "format", "json")
	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=collation_export.csv")
		writer := csv.NewWriter(w)
		writer.Write([]string{"party_code", "total_votes", "polling_units_counted"})
		for _, p := range parties {
			writer.Write([]string{p.PartyCode, fmt.Sprintf("%d", p.TotalVotes), fmt.Sprintf("%d", p.PUCount)})
		}
		writer.Flush()
	default:
		writeJSON(w, 200, M{
			"level":       level,
			"election_id": electionID,
			"parties":     parties,
			"exported_at": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// handleAuditExport exports the audit trail.
func handleAuditExport(w http.ResponseWriter, r *http.Request) {
	limit := queryParamInt(r, "limit", 1000)
	category := queryParam(r, "category", "")

	query := "SELECT action, entity_type, entity_id, actor, details, created_at FROM audit_log"
	args := []interface{}{}
	if category != "" {
		query += " WHERE action LIKE $1"
		args = append(args, category+"%")
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)

	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var entries []M
	for rows.Next() {
		var action, entityType, entityID, actor, details, createdAt string
		if rows.Scan(&action, &entityType, &entityID, &actor, &details, &createdAt) == nil {
			entries = append(entries, M{
				"action": action, "entity_type": entityType, "entity_id": entityID,
				"actor": actor, "details": details, "created_at": createdAt,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=audit_trail.json")
	json.NewEncoder(w).Encode(M{"total": len(entries), "entries": entries, "exported_at": time.Now().UTC().Format(time.RFC3339)})
}
