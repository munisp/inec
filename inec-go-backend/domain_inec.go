package main

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
)

// INEC Form EC8A — Polling Unit Result Sheet validation
type FormEC8A struct {
	ElectionID          int              `json:"election_id" validate:"required,gt=0"`
	PollingUnitCode     string           `json:"polling_unit_code" validate:"required"`
	PresidingOfficerID  string           `json:"presiding_officer_id" validate:"required"`
	RegisteredVoters    int              `json:"registered_voters" validate:"required,gt=0"`
	AccreditedVoters    int              `json:"accredited_voters" validate:"required,gte=0"`
	TotalVotesPolled    int              `json:"total_votes_polled" validate:"required,gte=0"`
	RejectedBallots     int              `json:"rejected_ballots" validate:"gte=0"`
	TotalValidVotes     int              `json:"total_valid_votes" validate:"required,gte=0"`
	PartyResults        []PartyVoteEntry `json:"party_results" validate:"required,min=1,dive"`
	BVASSerialNumber    string           `json:"bvas_serial_number"`
	BiometricMatchCount int              `json:"biometric_match_count" validate:"gte=0"`
	SubmittedAt         string           `json:"submitted_at"`
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
//
// SECURITY (R5-037): this path previously accepted results from ANY staff
// role for ANY polling unit in ANY election state. It now enforces the same
// controls as the canonical submit path (handleSubmitResult): only roles
// that may CREATE PU results (presiding officers; admins for supervised
// backfill — collation officers are explicitly excluded), state tenancy
// from verified JWT claims, and an election that is open for result capture
// ('active'/'voting', rerun-scoped where applicable). Rejections are
// audit-logged.
func handleSubmitEC8A(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardAuth(w, r)
	if !ok {
		return
	}
	if role, _ := claims["role"].(string); role != "admin" && role != "presiding_officer" {
		logAudit("EC8A_SUBMIT_REJECTED", "result", "", claimUserID(claims),
			map[string]interface{}{"reason": "role_not_permitted", "role": role})
		writeError(w, http.StatusForbidden, "role not permitted to create polling-unit results")
		return
	}
	var form FormEC8A
	if err := decodeAndValidate(r, &form); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	violations := ValidateEC8A(&form)
	if len(violations) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, M{
			"error":      "Form EC8A validation failed",
			"violations": violations,
			"status":     "rejected",
		})
		return
	}

	// R5-037: the election must be open for result capture — 'active'
	// (pre-poll-open operational window) or 'voting' per the W2 lifecycle
	// semantics; rerun/supplementary elections restrict to declared scope.
	var elStatus, elKind string
	if err := dbQueryRowCtx(r.Context(), "SELECT status, election_kind FROM elections WHERE id=?", form.ElectionID).Scan(&elStatus, &elKind); err != nil {
		writeError(w, http.StatusBadRequest, "Election not found")
		return
	}
	if elStatus != "active" && elStatus != "voting" {
		logAudit("EC8A_SUBMIT_REJECTED", "result", form.PollingUnitCode, claimUserID(claims),
			map[string]interface{}{"reason": "election_not_open", "election_id": form.ElectionID, "status": elStatus})
		writeError(w, http.StatusConflict, fmt.Sprintf("Election not open for result capture (status: %s)", elStatus))
		return
	}
	if elKind != "general" {
		inScope, scopeErr := puInRerunScope(r.Context(), form.ElectionID, form.PollingUnitCode)
		if scopeErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify rerun scope")
			return
		}
		if !inScope {
			logAudit("EC8A_SUBMIT_REJECTED", "result", form.PollingUnitCode, claimUserID(claims),
				map[string]interface{}{"reason": "outside_rerun_scope", "election_id": form.ElectionID})
			writeError(w, http.StatusForbidden, "polling unit is not in the declared scope of this "+elKind+" election")
			return
		}
	}

	// R5-037: state tenancy — officers may only submit for polling units in
	// their assigned state (from verified JWT claims, never the request).
	if !enforceStateTenancy(w, r, jwt.MapClaims(claims), form.PollingUnitCode) {
		logAudit("EC8A_SUBMIT_REJECTED", "result", form.PollingUnitCode, claimUserID(claims),
			map[string]interface{}{"reason": "outside_state_tenancy", "election_id": form.ElectionID})
		return
	}
	// R5-043: presiding officers are bound to their assigned polling unit.
	if !enforceOfficerPUBinding(w, r, jwt.MapClaims(claims), form.ElectionID, form.PollingUnitCode) {
		return
	}

	formPayload, err := canonicalJSON(form)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to canonicalize EC8A form")
		return
	}
	formHash := sha256Hex(formPayload)
	userID := claimUserID(claims)

	// A result with evidence is immutable. Corrections must be made through a
	// reconciliation case rather than by silently overwriting the source form.
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database transaction failed")
		return
	}
	defer tx.Rollback()
	var existingResultID int64
	existingErr := tx.QueryRowContext(r.Context(), convertPlaceholders("SELECT id FROM results WHERE election_id=? AND polling_unit_code=?"), form.ElectionID, form.PollingUnitCode).Scan(&existingResultID)
	if existingErr == nil {
		var eventCount int
		if err := tx.QueryRowContext(r.Context(), convertPlaceholders("SELECT COUNT(*) FROM result_evidence_events WHERE result_id=?"), existingResultID).Scan(&eventCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to inspect existing result evidence")
			return
		}
		if eventCount > 0 {
			writeError(w, http.StatusConflict, "an evidence-bearing result already exists; open a reconciliation case instead of overwriting it")
			return
		}
		writeError(w, http.StatusConflict, "a legacy result already exists for this polling unit and requires supervised migration")
		return
	}
	if existingErr != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "failed to inspect existing result")
		return
	}

	resultID, err := integrityInsertReturningID(r.Context(), tx, `INSERT INTO results
		(election_id, polling_unit_code, presiding_officer_id, status, total_valid_votes, rejected_votes,
		total_votes_cast, accredited_voters, ec8a_hash, submitted_at)
		VALUES (?, ?, ?, 'pending', ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		form.ElectionID, form.PollingUnitCode, nullIntArg(userID), form.TotalValidVotes, form.RejectedBallots,
		form.TotalVotesPolled, form.AccreditedVoters, "sha256:"+formHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to insert result: %v", err))
		return
	}
	for _, pr := range form.PartyResults {
		if _, err := tx.ExecContext(r.Context(), convertPlaceholders(`INSERT INTO result_party_scores (result_id, party_code, votes)
			VALUES (?, ?, ?)`), resultID, pr.PartyCode, pr.Votes); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to insert score for party %s: %v", pr.PartyCode, err))
			return
		}
	}
	policyVersionID, err := requirePolicyVersion(r.Context(), tx, form.ElectionID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if _, err := recordIntegrityEventTx(r.Context(), tx, integrityEventInput{
		ResultID:        resultID,
		EventType:       "RESULT_SUBMITTED",
		PolicyVersionID: policyVersionID,
		Visibility:      integrityVisibilityObserver,
		CreatedBy:       userID,
		PublicPayload: M{
			"polling_unit_code": form.PollingUnitCode,
			"status":            "pending",
			"total_votes_cast":  form.TotalVotesPolled,
		},
		PrivatePayload: M{
			"ec8a_form": form,
			"ec8a_hash": "sha256:" + formHash,
		},
	}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "EC8A evidence could not be recorded: "+err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "commit failed")
		return
	}
	// R5-037: EC8A submissions enter the security audit trail (this path was
	// previously invisible — no tenancy, no election-state check, no audit).
	logAudit("EC8A_SUBMITTED", "result", form.PollingUnitCode, userID,
		map[string]interface{}{"election_id": form.ElectionID, "result_id": resultID, "path": "/inec/ec8a/submit"})

	if mwHub != nil && mwHub.Kafka != nil {
		mwHub.Kafka.Produce(r.Context(), KafkaMessage{
			Topic: TopicResultSubmitted,
			Key:   form.PollingUnitCode,
			Value: map[string]interface{}{
				"result_id":           resultID,
				"election_id":         form.ElectionID,
				"polling_unit":        form.PollingUnitCode,
				"total_valid_votes":   form.TotalValidVotes,
				"party_count":         len(form.PartyResults),
				"evidence_content_id": formHash,
			},
		})
	}
	if mwHub != nil && mwHub.Redis != nil {
		cacheKey := fmt.Sprintf("ec8a:%d:%s", form.ElectionID, form.PollingUnitCode)
		mwHub.Redis.Set(r.Context(), cacheKey, string(formPayload), 30*time.Minute)
	}
	writeJSON(w, http.StatusCreated, M{
		"id":         resultID,
		"status":     "accepted",
		"message":    "Form EC8A submitted with immutable evidence provenance",
		"ec8a_hash":  "sha256:" + formHash,
		"violations": []string{},
	})
}

// --- Hierarchical Collation ---

// R5-014: ONE canonical collation semantics for the whole platform.
// Official collation totals count ONLY finalized results. Disputed results
// are excluded from totals and reported separately; pending/validated
// results are provisional progress, never part of official figures.
// Rerun/supplementary child elections contribute their scoped finalized
// results, replacing the parent result for any PU they cover (INEC
// supplementary-election semantics: rerun results merge into parent totals).
//
// Every collation path (hierarchical API, dashboard collation, evidence
// bundle builder, election-svc, persisted rollups) must use
// canonicalResultsCTE / canonicalPartyTotals — never a hand-rolled filter.
const canonicalResultStatus = "finalized"

// canonicalResultsCTE is the shared SQL fragment computing, for election $1,
// the canonical per-PU result set: the parent's own finalized results plus
// scoped finalized results of rerun/supplementary/by-election children
// (child result replaces the parent result for the same PU).
const canonicalResultsCTE = `
WITH child_elections AS (
	SELECT id FROM elections
	WHERE parent_election_id = $1 AND election_kind IN ('rerun','supplementary','by_election') AND status <> 'cancelled'
),
scoped_pus AS (
	SELECT rs.election_id AS child_id, pu.code AS pu_code
	FROM rerun_scopes rs JOIN child_elections ce ON ce.id = rs.election_id
	JOIN polling_units pu ON rs.scope_type = 'polling_unit' AND pu.code = rs.area_code
	UNION
	SELECT rs.election_id, pu.code
	FROM rerun_scopes rs JOIN child_elections ce ON ce.id = rs.election_id
	JOIN polling_units pu ON rs.scope_type = 'ward' AND pu.ward_code = rs.area_code
	UNION
	SELECT rs.election_id, pu.code
	FROM rerun_scopes rs JOIN child_elections ce ON ce.id = rs.election_id
	JOIN wards ww ON rs.scope_type = 'lga' AND ww.lga_code = rs.area_code
	JOIN polling_units pu ON pu.ward_code = ww.code
),
child_results AS (
	SELECT sp.pu_code, r.id AS result_id
	FROM scoped_pus sp
	JOIN results r ON r.election_id = sp.child_id AND r.polling_unit_code = sp.pu_code AND r.status = '` + canonicalResultStatus + `'
),
parent_results AS (
	SELECT r.polling_unit_code AS pu_code, r.id AS result_id
	FROM results r
	WHERE r.election_id = $1 AND r.status = '` + canonicalResultStatus + `'
	  AND NOT EXISTS (SELECT 1 FROM child_results cr WHERE cr.pu_code = r.polling_unit_code)
),
canonical AS (
	SELECT pu_code, result_id FROM parent_results
	UNION ALL
	SELECT pu_code, result_id FROM child_results
)`

// collationGeoFilter maps a collation level to the SQL predicate restricting
// polling units to the requested area. $2 is the area code.
func collationGeoFilter(level string) (string, error) {
	switch level {
	case "ward":
		return "pu.ward_code = $2", nil
	case "lga":
		return "w.lga_code = $2", nil
	case "state":
		return "l.state_code = $2", nil
	case "national":
		return "TRUE", nil
	}
	return "", fmt.Errorf("invalid collation level: must be ward, lga, state, or national")
}

// canonicalPartyTotals computes official party totals for an election at a
// geographic level using the canonical semantics (finalized only, rerun
// children merged). Returns party totals, total votes, and PU count.
func canonicalPartyTotals(ctx context.Context, electionID int, level, areaCode string) (map[string]int64, int64, int, error) {
	filter, err := collationGeoFilter(level)
	if err != nil {
		return nil, 0, 0, err
	}
	q := canonicalResultsCTE + `
SELECT rps.party_code, COALESCE(SUM(rps.votes),0) AS total, COUNT(DISTINCT c.pu_code) AS pu_count
FROM canonical c
JOIN result_party_scores rps ON rps.result_id = c.result_id
JOIN polling_units pu ON pu.code = c.pu_code
JOIN wards w ON w.code = pu.ward_code
JOIN lgas l ON l.code = w.lga_code
WHERE ` + filter + `
GROUP BY rps.party_code`
	args := []interface{}{electionID}
	if level != "national" {
		args = append(args, areaCode)
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	totals := map[string]int64{}
	var totalVotes int64
	puCount := 0
	for rows.Next() {
		var party string
		var total int64
		var cnt int
		if err := rows.Scan(&party, &total, &cnt); err != nil {
			return nil, 0, 0, err
		}
		totals[party] = total
		totalVotes += total
		puCount = cnt
	}
	return totals, totalVotes, puCount, rows.Err()
}

// resultStatusCounts returns per-status PU counts for an election at a level
// (provisional progress + disputed flagging, reported alongside totals).
func resultStatusCounts(ctx context.Context, electionID int, level, areaCode string) (map[string]int, int, error) {
	filter, err := collationGeoFilter(level)
	if err != nil {
		return nil, 0, err
	}
	q := `SELECT COALESCE(r.status,'none'), COUNT(DISTINCT pu.code)
	FROM polling_units pu
	JOIN wards w ON w.code = pu.ward_code
	JOIN lgas l ON l.code = w.lga_code
	LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = $1 AND r.status <> 'superseded'
	WHERE ` + filter + `
	GROUP BY COALESCE(r.status,'none')`
	args := []interface{}{electionID}
	if level != "national" {
		args = append(args, areaCode)
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	counts := map[string]int{}
	total := 0
	for rows.Next() {
		var st string
		var cnt int
		if err := rows.Scan(&st, &cnt); err != nil {
			return nil, 0, err
		}
		counts[st] += cnt
		total += cnt
	}
	return counts, total, rows.Err()
}

// electionChildIDs returns IDs of active rerun/supplementary child elections.
func electionChildIDs(ctx context.Context, electionID int) []int {
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM elections WHERE parent_election_id = $1 AND election_kind IN ('rerun','supplementary','by_election') AND status <> 'cancelled'`, electionID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// electionIDsWithChildren returns electionID plus its rerun/supplementary
// children — the set of elections whose submissions belong to this contest.
func electionIDsWithChildren(ctx context.Context, electionID int) []int {
	ids := append([]int{electionID}, electionChildIDs(ctx, electionID)...)
	// A rerun election itself also rolls up into its parent, but when queried
	// directly it reports only its own scope.
	return ids
}

type CollationLevel struct {
	Level       string           `json:"level"`
	Code        string           `json:"code"`
	Name        string           `json:"name"`
	PartyTotals map[string]int64 `json:"party_totals"`
	TotalVotes  int64            `json:"total_votes"`
	ChildCount  int              `json:"child_count"`
	// R5-014: provisional progress and disputed PUs are reported separately
	// from the official (finalized-only) totals.
	StatusCounts map[string]int   `json:"status_counts"`
	TotalPUs     int              `json:"total_pus"`
	DisputedPUs  int              `json:"disputed_pus"`
	Status       string           `json:"status"`
	CollatedAt   string           `json:"collated_at"`
	CollatedBy   string           `json:"collated_by"`
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
	return collateLevel(ctx, electionID, "ward", wardCode)
}

func collateLGA(ctx context.Context, electionID int, lgaCode string) (*CollationLevel, error) {
	return collateLevel(ctx, electionID, "lga", lgaCode)
}

func collateState(ctx context.Context, electionID int, stateCode string) (*CollationLevel, error) {
	return collateLevel(ctx, electionID, "state", stateCode)
}

func collateNational(ctx context.Context, electionID int) (*CollationLevel, error) {
	return collateLevel(ctx, electionID, "national", "NG")
}

// collateLevel is the single canonical collation computation (R5-014):
// finalized results only, rerun children merged, disputed flagged separately.
func collateLevel(ctx context.Context, electionID int, level, areaCode string) (*CollationLevel, error) {
	partyTotals, totalVotes, puCount, err := canonicalPartyTotals(ctx, electionID, level, areaCode)
	if err != nil {
		return nil, err
	}
	counts, totalPUs, err := resultStatusCounts(ctx, electionID, level, areaCode)
	if err != nil {
		return nil, err
	}
	status := "in_progress"
	if totalPUs > 0 && counts[canonicalResultStatus] >= totalPUs-counts["voided"] {
		status = "completed"
	}
	if counts["disputed"] > 0 {
		status = "disputed"
	}
	return &CollationLevel{
		Level:        level,
		Code:         areaCode,
		PartyTotals:  partyTotals,
		TotalVotes:   totalVotes,
		ChildCount:   puCount,
		StatusCounts: counts,
		TotalPUs:     totalPUs,
		DisputedPUs:  counts["disputed"],
		Status:       status,
		CollatedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// --- Persisted collation rollups (R5-015) ---

// collationAreaName resolves the display name for a collation area.
func collationAreaName(ctx context.Context, level, areaCode string) string {
	var name string
	var q string
	switch level {
	case "ward":
		q = "SELECT name FROM wards WHERE code=$1"
	case "lga":
		q = "SELECT name FROM lgas WHERE code=$1"
	case "state":
		q = "SELECT name FROM states WHERE code=$1"
	default:
		return "Federal"
	}
	if err := db.QueryRowContext(ctx, q, areaCode).Scan(&name); err != nil {
		return areaCode
	}
	return name
}

// persistCollationRollup computes the canonical collation for (level, area)
// and durably upserts it into collation_results + collation_party_scores.
// This is the freeze point sign-off and the LGA→state→national write path.
func persistCollationRollup(ctx context.Context, electionID int, level, areaCode string) error {
	if _, err := collationGeoFilter(level); err != nil {
		return err
	}
	c, err := collateLevel(ctx, electionID, level, areaCode)
	if err != nil {
		return err
	}
	// Registered/accredited/cast aggregates for the area.
	var registered, accredited, cast, rejected int64
	filter, _ := collationGeoFilter(level)
	aggQ := canonicalResultsCTE + `
SELECT COALESCE(SUM(pu.registered_voters),0), COALESCE(SUM(r.accredited_voters),0),
       COALESCE(SUM(r.total_votes_cast),0), COALESCE(SUM(r.rejected_votes),0)
FROM canonical c
JOIN results r ON r.id = c.result_id
JOIN polling_units pu ON pu.code = c.pu_code
JOIN wards w ON w.code = pu.ward_code
JOIN lgas l ON l.code = w.lga_code
WHERE ` + filter
	aggArgs := []interface{}{electionID}
	if level != "national" {
		aggArgs = append(aggArgs, areaCode)
	}
	if err := db.QueryRowContext(ctx, aggQ, aggArgs...).Scan(&registered, &accredited, &cast, &rejected); err != nil {
		return err
	}
	// Total registered voters covers ALL PUs in the area, not just reported ones.
	var areaRegistered int64
	regQ := `SELECT COALESCE(SUM(pu.registered_voters),0) FROM polling_units pu
		JOIN wards w ON w.code = pu.ward_code JOIN lgas l ON l.code = w.lga_code WHERE ` +
		strings.Replace(filter, "$2", "$1", 1)
	if level == "national" {
		if err := db.QueryRowContext(ctx, regQ).Scan(&areaRegistered); err == nil {
			registered = areaRegistered
		}
	} else if err := db.QueryRowContext(ctx, regQ, areaCode).Scan(&areaRegistered); err == nil {
		registered = areaRegistered
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var collationID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO collation_results (election_id, level, area_code, area_name,
			total_registered_voters, total_accredited_voters, total_valid_votes,
			total_rejected_votes, total_votes_cast, polling_units_reported,
			polling_units_total, status, last_updated)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,CURRENT_TIMESTAMP)
		ON CONFLICT (election_id, level, area_code) DO UPDATE SET
			area_name = EXCLUDED.area_name,
			total_registered_voters = EXCLUDED.total_registered_voters,
			total_accredited_voters = EXCLUDED.total_accredited_voters,
			total_valid_votes = EXCLUDED.total_valid_votes,
			total_rejected_votes = EXCLUDED.total_rejected_votes,
			total_votes_cast = EXCLUDED.total_votes_cast,
			polling_units_reported = EXCLUDED.polling_units_reported,
			polling_units_total = EXCLUDED.polling_units_total,
			status = EXCLUDED.status,
			last_updated = CURRENT_TIMESTAMP
		RETURNING id`,
		electionID, level, areaCode, collationAreaName(ctx, level, areaCode),
		registered, accredited, c.TotalVotes, rejected, cast,
		c.ChildCount, c.TotalPUs, c.Status).Scan(&collationID)
	if err != nil {
		return fmt.Errorf("persist collation rollup: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM collation_party_scores WHERE collation_result_id=$1", collationID); err != nil {
		return err
	}
	for party, votes := range c.PartyTotals {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO collation_party_scores (collation_result_id, party_code, votes) VALUES ($1,$2,$3)",
			collationID, party, votes); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// rollupCollationHierarchy persists ward→LGA→state→national rollups for the
// whole election (the real hierarchical write path of R5-015). When wardCode
// is non-empty only the chain containing that ward is refreshed.
func rollupCollationHierarchy(ctx context.Context, electionID int, wardCode string) error {
	type area struct{ level, code string }
	var wards, lgas, states []string
	if wardCode != "" {
		wards = []string{wardCode}
		var lga string
		db.QueryRowContext(ctx, "SELECT lga_code FROM wards WHERE code=$1", wardCode).Scan(&lga)
		if lga != "" {
			lgas = []string{lga}
			var st string
			db.QueryRowContext(ctx, "SELECT state_code FROM lgas WHERE code=$1", lga).Scan(&st)
			if st != "" {
				states = []string{st}
			}
		}
	} else {
		for q, dst := range map[string]*[]string{
			"SELECT code FROM wards":  &wards,
			"SELECT code FROM lgas":   &lgas,
			"SELECT code FROM states": &states,
		} {
			rows, err := db.QueryContext(ctx, q)
			if err != nil {
				return err
			}
			for rows.Next() {
				var code string
				if rows.Scan(&code) == nil {
					*dst = append(*dst, code)
				}
			}
			rows.Close()
		}
	}
	var areas []area
	for _, w := range wards {
		areas = append(areas, area{"ward", w})
	}
	for _, l := range lgas {
		areas = append(areas, area{"lga", l})
	}
	for _, s := range states {
		areas = append(areas, area{"state", s})
	}
	areas = append(areas, area{"national", "NG"})
	for _, a := range areas {
		if err := persistCollationRollup(ctx, electionID, a.level, a.code); err != nil {
			return fmt.Errorf("rollup %s/%s: %w", a.level, a.code, err)
		}
	}
	return nil
}

// handlePersistCollation triggers a durable collation rollup write.
// POST /inec/collation/persist  {election_id, level?, code?}
// Without level/code it rolls up the full ward→LGA→state→national hierarchy.
func handlePersistCollation(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardWrite(w, r, "collate_results", "admin", "collation_officer", "returning_officer")
	if !ok {
		return
	}
	var req struct {
		ElectionID int    `json:"election_id" validate:"required,gt=0"`
		Level      string `json:"level"`
		Code       string `json:"code"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ctx := r.Context()
	var elStatus string
	if err := db.QueryRowContext(ctx, "SELECT status FROM elections WHERE id=$1", req.ElectionID).Scan(&elStatus); err != nil {
		writeError(w, 404, "election not found")
		return
	}
	if req.Level != "" {
		if _, err := collationGeoFilter(req.Level); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if req.Code == "" {
			writeError(w, 400, "code is required when level is given")
			return
		}
		if err := persistCollationRollup(ctx, req.ElectionID, req.Level, req.Code); err != nil {
			writeError(w, 500, err.Error())
			return
		}
	} else if err := rollupCollationHierarchy(ctx, req.ElectionID, ""); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	username, _ := claims["username"].(string)
	logAudit("COLLATION_PERSISTED", "election", fmt.Sprintf("%d", req.ElectionID), claimUserID(claims),
		map[string]interface{}{"level": req.Level, "code": req.Code, "actor": username})
	writeJSON(w, 200, M{"status": "persisted", "election_id": req.ElectionID, "level": req.Level, "code": req.Code})
}

// --- Ballot Reconciliation ---

type ReconciliationResult struct {
	PollingUnitCode  string  `json:"polling_unit_code"`
	RegisteredVoters int     `json:"registered_voters"`
	AccreditedVoters int     `json:"accredited_voters"`
	TotalBallots     int     `json:"total_ballots"`
	ValidBallots     int     `json:"valid_ballots"`
	RejectedBallots  int     `json:"rejected_ballots"`
	Discrepancy      int     `json:"discrepancy"`
	DiscrepancyPct   float64 `json:"discrepancy_pct"`
	Status           string  `json:"status"`
}

// handleBallotReconciliation verifies that ballot counts add up across polling units.
func handleBallotReconciliation(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)
	stateCode := queryParam(r, "state_code", "")

	query := `SELECT pu.code, pu.registered_voters,
		COALESCE(SUM(rps.votes), 0) as total_votes,
		COUNT(DISTINCT rps.id) as result_count
		FROM polling_units pu
		LEFT JOIN results r ON pu.code = r.polling_unit_code AND r.election_id = $1
		LEFT JOIN result_party_scores rps ON rps.result_id = r.id
		JOIN wards w ON pu.ward_code = w.code
		JOIN lgas l ON w.lga_code = l.code
		WHERE 1=1`

	args := []interface{}{electionID}
	if stateCode != "" {
		query += " AND l.state_code = $2"
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

	// Get PostgreSQL totals — canonical semantics (R5-014): finalized only.
	rows, err := db.QueryContext(ctx,
		`SELECT rps.party_code, SUM(rps.votes)
		 FROM results r
		 JOIN result_party_scores rps ON rps.result_id = r.id
		 WHERE r.election_id = $1 AND r.status = '`+canonicalResultStatus+`'
		 GROUP BY rps.party_code ORDER BY SUM(rps.votes) DESC`, electionID)
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
		"status":        status,
		"pg_totals":     pgTotals,
		"tb_totals":     tbTotals,
		"mismatches":    mismatches,
		"election_id":   electionID,
		"reconciled_at": time.Now().UTC().Format(time.RFC3339),
	})
}
