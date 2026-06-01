package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// INEC Form EC8A — Polling Unit Result Sheet validation
type FormEC8A struct {
	ElectionID          int    `json:"election_id" validate:"required,gt=0"`
	PollingUnitCode     string `json:"polling_unit_code" validate:"required"`
	PresidingOfficerID  string `json:"presiding_officer_id" validate:"required"`
	RegisteredVoters    int    `json:"registered_voters" validate:"required,gt=0"`
	AccreditedVoters    int    `json:"accredited_voters" validate:"required,gte=0"`
	TotalVotesPolled    int    `json:"total_votes_polled" validate:"required,gte=0"`
	RejectedBallots     int    `json:"rejected_ballots" validate:"gte=0"`
	TotalValidVotes     int    `json:"total_valid_votes" validate:"required,gte=0"`
	PartyResults        []PartyVoteEntry `json:"party_results" validate:"required,min=1,dive"`
	BVASSerialNumber    string `json:"bvas_serial_number"`
	BiometricMatchCount int    `json:"biometric_match_count" validate:"gte=0"`
	SubmittedAt         string `json:"submitted_at"`
}

type PartyVoteEntry struct {
	PartyCode string `json:"party_code" validate:"required"`
	Votes     int    `json:"votes" validate:"gte=0"`
}

// ValidateEC8A enforces INEC-specific business rules on the result sheet.
func ValidateEC8A(form *FormEC8A) []string {
	var violations []string

	// Rule 1: Accredited voters cannot exceed registered voters
	if form.AccreditedVoters > form.RegisteredVoters {
		violations = append(violations, fmt.Sprintf(
			"accredited_voters (%d) exceeds registered_voters (%d)",
			form.AccreditedVoters, form.RegisteredVoters))
	}

	// Rule 2: Total votes polled cannot exceed accredited voters
	if form.TotalVotesPolled > form.AccreditedVoters {
		violations = append(violations, fmt.Sprintf(
			"total_votes_polled (%d) exceeds accredited_voters (%d)",
			form.TotalVotesPolled, form.AccreditedVoters))
	}

	// Rule 3: Valid votes + rejected ballots must equal total votes polled
	sumCheck := form.TotalValidVotes + form.RejectedBallots
	if sumCheck != form.TotalVotesPolled {
		violations = append(violations, fmt.Sprintf(
			"valid_votes (%d) + rejected_ballots (%d) = %d, but total_votes_polled = %d",
			form.TotalValidVotes, form.RejectedBallots, sumCheck, form.TotalVotesPolled))
	}

	// Rule 4: Sum of party results must equal total valid votes
	partySum := 0
	for _, pr := range form.PartyResults {
		partySum += pr.Votes
	}
	if partySum != form.TotalValidVotes {
		violations = append(violations, fmt.Sprintf(
			"sum of party votes (%d) does not equal total_valid_votes (%d)",
			partySum, form.TotalValidVotes))
	}

	// Rule 5: No party can receive more votes than accredited voters
	for _, pr := range form.PartyResults {
		if pr.Votes > form.AccreditedVoters {
			violations = append(violations, fmt.Sprintf(
				"party %s has %d votes, exceeding accredited_voters (%d)",
				pr.PartyCode, pr.Votes, form.AccreditedVoters))
		}
	}

	// Rule 6: Turnout sanity (flag if over 95%)
	if form.RegisteredVoters > 0 {
		turnout := float64(form.AccreditedVoters) / float64(form.RegisteredVoters) * 100
		if turnout > 95 {
			violations = append(violations, fmt.Sprintf(
				"unusually high turnout: %.1f%% (accredited: %d, registered: %d)",
				turnout, form.AccreditedVoters, form.RegisteredVoters))
		}
	}

	// Rule 7: Biometric match rate check
	if form.BiometricMatchCount > 0 && form.AccreditedVoters > 0 {
		matchRate := float64(form.BiometricMatchCount) / float64(form.AccreditedVoters) * 100
		if matchRate < 80 {
			violations = append(violations, fmt.Sprintf(
				"low biometric match rate: %.1f%% (%d/%d)",
				matchRate, form.BiometricMatchCount, form.AccreditedVoters))
		}
	}

	return violations
}

// handleSubmitEC8A processes a Form EC8A submission with full validation.
func handleSubmitEC8A(w http.ResponseWriter, r *http.Request) {
	var form FormEC8A
	if err := decodeAndValidate(r, &form); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	violations := ValidateEC8A(&form)
	if len(violations) > 0 {
		writeJSON(w, 422, M{
			"error":      "Form EC8A validation failed",
			"violations": violations,
			"status":     "rejected",
		})
		return
	}

	// Persist each party result
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "database transaction failed")
		return
	}
	defer tx.Rollback()

	for _, pr := range form.PartyResults {
		_, err := tx.ExecContext(r.Context(),
			`INSERT INTO results (election_id, polling_unit_code, party_code, votes, status, submitted_by, submitted_at)
			 VALUES ($1, $2, $3, $4, 'pending', $5, $6)`,
			form.ElectionID, form.PollingUnitCode, pr.PartyCode, pr.Votes, form.PresidingOfficerID, time.Now())
		if err != nil {
			writeError(w, 500, fmt.Sprintf("failed to insert result for party %s: %v", pr.PartyCode, err))
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeError(w, 500, "commit failed")
		return
	}

	// Emit Kafka event
	if mwHub != nil && mwHub.Kafka != nil {
		mwHub.Kafka.Produce(r.Context(), KafkaMessage{
			Topic: TopicResultSubmitted,
			Key:   form.PollingUnitCode,
			Value: map[string]interface{}{
				"election_id":      form.ElectionID,
				"polling_unit":     form.PollingUnitCode,
				"total_valid_votes": form.TotalValidVotes,
				"party_count":      len(form.PartyResults),
			},
		})
	}

	// Cache in Redis
	if mwHub != nil && mwHub.Redis != nil {
		cacheKey := fmt.Sprintf("ec8a:%d:%s", form.ElectionID, form.PollingUnitCode)
		data, _ := json.Marshal(form)
		mwHub.Redis.Set(r.Context(), cacheKey, string(data), 30*time.Minute)
	}

	writeJSON(w, 201, M{
		"status":     "accepted",
		"message":    "Form EC8A submitted and validated",
		"violations": []string{},
	})
}

// --- Hierarchical Collation ---

type CollationLevel struct {
	Level       string             `json:"level"`
	Code        string             `json:"code"`
	Name        string             `json:"name"`
	PartyTotals map[string]int64   `json:"party_totals"`
	TotalVotes  int64              `json:"total_votes"`
	ChildCount  int                `json:"child_count"`
	Status      string             `json:"status"`
	CollatedAt  string             `json:"collated_at"`
	CollatedBy  string             `json:"collated_by"`
}

// handleHierarchicalCollation performs collation at ward → LGA → state → national levels.
func handleHierarchicalCollation(w http.ResponseWriter, r *http.Request) {
	level := queryParam(r, "level", "state")
	code := queryParam(r, "code", "")
	electionID := queryParamInt(r, "election_id", 1)

	var result *CollationLevel
	var err error

	switch level {
	case "ward":
		result, err = collateWard(r.Context(), electionID, code)
	case "lga":
		result, err = collateLGA(r.Context(), electionID, code)
	case "state":
		result, err = collateState(r.Context(), electionID, code)
	case "national":
		result, err = collateNational(r.Context(), electionID)
	default:
		writeError(w, 400, "invalid collation level: must be ward, lga, state, or national")
		return
	}

	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, result)
}

func collateWard(ctx context.Context, electionID int, wardCode string) (*CollationLevel, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT r.party_code, SUM(r.votes) as total, COUNT(DISTINCT r.polling_unit_code) as pu_count
		 FROM results r
		 JOIN polling_units pu ON r.polling_unit_code = pu.code
		 WHERE r.election_id = $1 AND pu.ward_code = $2 AND r.status IN ('pending','verified')
		 GROUP BY r.party_code`, electionID, wardCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return buildCollation("ward", wardCode, rows)
}

func collateLGA(ctx context.Context, electionID int, lgaCode string) (*CollationLevel, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT r.party_code, SUM(r.votes) as total, COUNT(DISTINCT r.polling_unit_code)
		 FROM results r
		 JOIN polling_units pu ON r.polling_unit_code = pu.code
		 WHERE r.election_id = $1 AND pu.lga_code = $2 AND r.status IN ('pending','verified')
		 GROUP BY r.party_code`, electionID, lgaCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return buildCollation("lga", lgaCode, rows)
}

func collateState(ctx context.Context, electionID int, stateCode string) (*CollationLevel, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT r.party_code, SUM(r.votes) as total, COUNT(DISTINCT r.polling_unit_code)
		 FROM results r
		 JOIN polling_units pu ON r.polling_unit_code = pu.code
		 WHERE r.election_id = $1 AND pu.state_code = $2 AND r.status IN ('pending','verified')
		 GROUP BY r.party_code`, electionID, stateCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return buildCollation("state", stateCode, rows)
}

func collateNational(ctx context.Context, electionID int) (*CollationLevel, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT r.party_code, SUM(r.votes) as total, COUNT(DISTINCT r.polling_unit_code)
		 FROM results r
		 WHERE r.election_id = $1 AND r.status IN ('pending','verified')
		 GROUP BY r.party_code`, electionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return buildCollation("national", "NG", rows)
}

func buildCollation(level, code string, rows *sql.Rows) (*CollationLevel, error) {
	partyTotals := make(map[string]int64)
	var totalVotes int64
	childCount := 0

	for rows.Next() {
		var party string
		var total int64
		var puCount int
		if err := rows.Scan(&party, &total, &puCount); err != nil {
			continue
		}
		partyTotals[party] = total
		totalVotes += total
		childCount = puCount
	}

	return &CollationLevel{
		Level:       level,
		Code:        code,
		PartyTotals: partyTotals,
		TotalVotes:  totalVotes,
		ChildCount:  childCount,
		Status:      "collated",
		CollatedAt:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// --- Ballot Reconciliation ---

type ReconciliationResult struct {
	PollingUnitCode string  `json:"polling_unit_code"`
	RegisteredVoters int    `json:"registered_voters"`
	AccreditedVoters int    `json:"accredited_voters"`
	TotalBallots     int    `json:"total_ballots"`
	ValidBallots     int    `json:"valid_ballots"`
	RejectedBallots  int    `json:"rejected_ballots"`
	Discrepancy      int    `json:"discrepancy"`
	DiscrepancyPct   float64 `json:"discrepancy_pct"`
	Status           string `json:"status"`
}

// handleBallotReconciliation verifies that ballot counts add up across polling units.
func handleBallotReconciliation(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)
	stateCode := queryParam(r, "state_code", "")

	query := `SELECT pu.code, pu.registered_voters,
		COALESCE(SUM(r.votes), 0) as total_votes,
		COUNT(r.id) as result_count
		FROM polling_units pu
		LEFT JOIN results r ON pu.code = r.polling_unit_code AND r.election_id = $1
		WHERE 1=1`

	args := []interface{}{electionID}
	if stateCode != "" {
		query += " AND pu.state_code = $2"
		args = append(args, stateCode)
	}
	query += " GROUP BY pu.code, pu.registered_voters ORDER BY pu.code"

	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var results []ReconciliationResult
	var totalDiscrepancies int

	for rows.Next() {
		var code string
		var registered, totalVotes, resultCount int
		if err := rows.Scan(&code, &registered, &totalVotes, &resultCount); err != nil {
			continue
		}

		discrepancy := 0
		if totalVotes > registered {
			discrepancy = totalVotes - registered
		}
		discPct := 0.0
		if registered > 0 {
			discPct = math.Round(float64(discrepancy)/float64(registered)*10000) / 100
		}

		status := "ok"
		if discPct > 5 {
			status = "flagged"
			totalDiscrepancies++
		}

		results = append(results, ReconciliationResult{
			PollingUnitCode:  code,
			RegisteredVoters: registered,
			TotalBallots:     totalVotes,
			ValidBallots:     totalVotes,
			Discrepancy:      discrepancy,
			DiscrepancyPct:   discPct,
			Status:           status,
		})
	}

	writeJSON(w, 200, M{
		"reconciliation": results,
		"total_units":    len(results),
		"flagged":        totalDiscrepancies,
		"election_id":    electionID,
	})
}

// --- Dual-Ledger Reconciliation ---

// handleDualLedgerReconciliation compares PostgreSQL results with TigerBeetle ledger.
func handleDualLedgerReconciliation(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)
	ctx := r.Context()

	// Get PostgreSQL totals
	rows, err := db.QueryContext(ctx,
		`SELECT party_code, SUM(votes) FROM results WHERE election_id = $1 AND status IN ('pending','verified')
		 GROUP BY party_code ORDER BY SUM(votes) DESC`, electionID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	pgTotals := make(map[string]int64)
	for rows.Next() {
		var party string
		var total int64
		rows.Scan(&party, &total)
		pgTotals[party] = total
	}

	// Get TigerBeetle ledger totals (via account lookups)
	tbTotals := make(map[string]int64)
	if mwHub != nil && mwHub.TigerBeetle != nil {
		for party := range pgTotals {
			acct, err := mwHub.TigerBeetle.GetAccount(ctx, "election-"+party)
			if err == nil && acct != nil {
				tbTotals[party] = acct.CreditsPosted
			}
		}
	}

	// Compare
	var mismatches []M
	matched := true
	for party, pgTotal := range pgTotals {
		tbTotal := tbTotals[party]
		if pgTotal != tbTotal {
			matched = false
			mismatches = append(mismatches, M{
				"party":      party,
				"pg_total":   pgTotal,
				"tb_total":   tbTotal,
				"difference": pgTotal - tbTotal,
			})
		}
	}

	status := "PASS"
	if !matched {
		status = "MISMATCH"
	}

	log.Info().Str("status", status).Int("parties", len(pgTotals)).Msg("dual-ledger reconciliation")

	writeJSON(w, 200, M{
		"status":      status,
		"pg_totals":   pgTotals,
		"tb_totals":   tbTotals,
		"mismatches":  mismatches,
		"election_id": electionID,
		"reconciled_at": time.Now().UTC().Format(time.RFC3339),
	})
}
