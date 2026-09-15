package main

// Election lifecycle core (W2): result declaration (R5-011), the electoral
// rules engine (R5-019), correction/supersession (R5-016), rerun and
// supplementary elections (R5-018), submission lookup/idempotency (R5-025),
// and presiding-officer handover (R5-026).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// nilIfEmpty maps an empty string to a SQL NULL argument.
func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// hasHardViolation reports whether any EC8A violation is an arithmetic
// inconsistency (rules 3 and 4) that can never be overridden — the numbers
// literally do not add up. Turnout/overvoting/biometric anomalies (rules
// 1, 2, 5, 6, 7) are soft: they may be real and must be recordable with an
// officer override + mandatory review (R5-017).
func hasHardViolation(violations []string) bool {
	for _, v := range violations {
		if strings.HasPrefix(v, "valid_votes (") || strings.HasPrefix(v, "sum of party votes (") {
			return true
		}
	}
	return false
}

// puInRerunScope reports whether a polling unit is covered by the declared
// scope of a rerun/supplementary/by-election (R5-018).
func puInRerunScope(ctx context.Context, electionID int, puCode string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM rerun_scopes rs
		WHERE rs.election_id = $1 AND (
			(rs.scope_type = 'polling_unit' AND rs.area_code = $2) OR
			(rs.scope_type = 'ward' AND rs.area_code = (SELECT ward_code FROM polling_units WHERE code = $2)) OR
			(rs.scope_type = 'lga' AND rs.area_code = (SELECT w.lga_code FROM polling_units pu JOIN wards w ON w.code = pu.ward_code WHERE pu.code = $2))
		)`, electionID, puCode).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ── Declaration (R5-011) + electoral rules engine (R5-019) ──

// declarationAssessment is the output of the declaration rules engine.
type declarationAssessment struct {
	Complete        bool              `json:"complete"`
	TotalPUs        int               `json:"total_pus"`
	FinalizedPUs    int               `json:"finalized_pus"`
	DisputedPUs     int               `json:"disputed_pus"`
	VoidedPUs       int               `json:"voided_pus"`
	MissingPUs      int               `json:"missing_pus"`
	OpenDisputes    int               `json:"open_disputes"`
	PartyTotals     map[string]int64  `json:"party_totals"`
	WinnerParty     string            `json:"winner_party,omitempty"`
	WinnerVotes     int64             `json:"winner_votes,omitempty"`
	RunnerUpParty   string            `json:"runner_up_party,omitempty"`
	RunnerUpVotes   int64             `json:"runner_up_votes,omitempty"`
	Margin          int64             `json:"margin"`
	Tie             bool              `json:"tie"`
	AffectedVoters  int64             `json:"affected_voters"`
	Inconclusive    bool              `json:"inconclusive"`
	InconclusiveWhy string            `json:"inconclusive_reason,omitempty"`
	SpreadRequired  bool              `json:"spread_required"`
	SpreadMet       bool              `json:"spread_met"`
	SpreadStates    int               `json:"spread_states_qualified"`
	SpreadThreshold int               `json:"spread_threshold"`
}

// assessDeclaration runs the completeness gate, winner computation, and the
// margin-of-lead / constitutional-spread rules for an election.
func assessDeclaration(ctx context.Context, electionID int) (*declarationAssessment, error) {
	a := &declarationAssessment{PartyTotals: map[string]int64{}}

	// Election + type.
	var elType, elStatus string
	if err := db.QueryRowContext(ctx, "SELECT election_type, status FROM elections WHERE id=$1", electionID).Scan(&elType, &elStatus); err != nil {
		return nil, fmt.Errorf("election not found")
	}

	// Completeness gate (R5-011): every polling unit must be accounted for by
	// a canonical result that is finalized — or formally disputed/voided and
	// therefore flagged for separate handling. Results of rerun children are
	// merged via their scope.
	childIDs := electionChildIDs(ctx, electionID)
	scopeIDs := append([]int{electionID}, childIDs...)
	placeholders := make([]string, len(scopeIDs))
	args := make([]interface{}, len(scopeIDs))
	for i, id := range scopeIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	idList := strings.Join(placeholders, ",")

	var statusRows *sql.Rows
	statusRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT status, COUNT(DISTINCT polling_unit_code) FROM results
		WHERE election_id IN (%s) AND status NOT IN ('superseded')
		GROUP BY status`, idList), args...)
	if err != nil {
		return nil, err
	}
	defer statusRows.Close()
	counts := map[string]int{}
	for statusRows.Next() {
		var st string
		var n int
		if statusRows.Scan(&st, &n) == nil {
			counts[st] = n
		}
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM polling_units").Scan(&a.TotalPUs); err != nil {
		return nil, err
	}
	a.FinalizedPUs = counts["finalized"]
	a.DisputedPUs = counts["disputed"]
	a.VoidedPUs = counts["voided"]
	accounted := a.FinalizedPUs + a.DisputedPUs + a.VoidedPUs
	if accounted > a.TotalPUs {
		accounted = a.TotalPUs
	}
	a.MissingPUs = a.TotalPUs - accounted
	a.Complete = a.MissingPUs == 0

	// Open disputes block declaration (R5-020/R5-021).
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FROM disputes WHERE election_id IN (%s)
		AND status NOT IN ('resolved','dismissed')`, idList), args...).Scan(&a.OpenDisputes); err != nil {
		return nil, err
	}

	// Winner computation over the canonical (finalized-only, rerun-merged)
	// national totals.
	totals, _, _, err := canonicalPartyTotals(ctx, electionID, "national", "NG")
	if err != nil {
		return nil, err
	}
	a.PartyTotals = totals
	type pv struct {
		party string
		votes int64
	}
	var ranked []pv
	for p, v := range totals {
		ranked = append(ranked, pv{p, v})
	}
	for i := 0; i < len(ranked); i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].votes > ranked[i].votes {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}
	if len(ranked) > 0 {
		a.WinnerParty, a.WinnerVotes = ranked[0].party, ranked[0].votes
	}
	if len(ranked) > 1 {
		a.RunnerUpParty, a.RunnerUpVotes = ranked[1].party, ranked[1].votes
		a.Margin = a.WinnerVotes - a.RunnerUpVotes
		a.Tie = a.Margin == 0
	}

	// Margin-of-lead principle (R5-019): if the registered voters of PUs
	// whose results were cancelled (voided) or remain disputed exceed the
	// winner's margin, the outcome is inconclusive and a supplementary
	// election is required before declaration.
	var affected int64
	db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(SUM(pu.registered_voters),0)
		FROM results r JOIN polling_units pu ON pu.code = r.polling_unit_code
		WHERE r.election_id IN (%s) AND r.status IN ('voided','disputed')`, idList), args...).Scan(&affected)
	a.AffectedVoters = affected
	if len(ranked) > 1 && affected > a.Margin {
		a.Inconclusive = true
		a.InconclusiveWhy = fmt.Sprintf("margin of lead (%d) is smaller than registered voters in cancelled/disputed PUs (%d) — supplementary election required", a.Margin, affected)
	}
	if a.Tie {
		a.Inconclusive = true
		a.InconclusiveWhy = "tie between leading candidates — runoff required"
	}

	// Constitutional spread (R5-019): a presidential winner needs >= 25% of
	// the votes in at least two-thirds of all states + the FCT, else runoff.
	a.SpreadRequired = elType == "presidential"
	if a.SpreadRequired && a.WinnerParty != "" {
		var stateCount int
		db.QueryRowContext(ctx, "SELECT COUNT(*) FROM states").Scan(&stateCount)
		// Two-thirds of the federation (36 states + FCT), rounded up.
		a.SpreadThreshold = (2*stateCount + 2) / 3
		stateTotals := map[string]int64{}
		winnerByState := map[string]int64{}
		rows2, err := db.QueryContext(ctx, canonicalResultsCTE+`
			SELECT l.state_code, rps.party_code, COALESCE(SUM(rps.votes),0)
			FROM canonical c
			JOIN result_party_scores rps ON rps.result_id = c.result_id
			JOIN polling_units pu ON pu.code = c.pu_code
			JOIN wards w ON w.code = pu.ward_code
			JOIN lgas l ON l.code = w.lga_code
			GROUP BY l.state_code, rps.party_code`, electionID)
		if err != nil {
			return nil, err
		}
		defer rows2.Close()
		for rows2.Next() {
			var sc, pc string
			var v int64
			if rows2.Scan(&sc, &pc, &v) == nil {
				stateTotals[sc] += v
				if pc == a.WinnerParty {
					winnerByState[sc] += v
				}
			}
		}
		qualified := 0
		fctQualified := false
		for sc, tot := range stateTotals {
			if tot > 0 && winnerByState[sc]*4 >= tot {
				qualified++
				if sc == "FCT" {
					fctQualified = true
				}
			}
		}
		a.SpreadStates = qualified
		a.SpreadMet = qualified >= a.SpreadThreshold && (fctQualified || qualified > a.SpreadThreshold)
		if !a.SpreadMet {
			a.Inconclusive = true
			a.InconclusiveWhy = fmt.Sprintf("constitutional spread not met: winner reached 25%% in %d states (need >=%d incl. FCT) — runoff required", qualified, a.SpreadThreshold)
		}
	} else {
		a.SpreadMet = true
	}

	return a, nil
}

// handleDeclareResult declares the outcome of an election (R5-011).
// POST /elections/{id}/declare {notes?, declare_inconclusive?}
func handleDeclareResult(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardRole(w, r, "admin", "returning_officer")
	if !ok {
		return
	}
	role, _ := claims["role"].(string)
	// Permission-guarded: declare_result (Permify grant or RBAC map). Admin
	// grants are managed through permify_relationships.
	if !checkPermission(role, "declare_result") {
		writeError(w, 403, "permission denied: declare_result required")
		return
	}
	id := mux.Vars(r)["id"]
	var req struct {
		Notes               string `json:"notes"`
		DeclareInconclusive bool   `json:"declare_inconclusive"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ctx := r.Context()
	username, _ := claims["username"].(string)

	// Lock the election row: declaration is a single, serialized act.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, 500, "database transaction error")
		return
	}
	defer tx.Rollback()
	var elStatus string
	var alreadyDeclared sql.NullTime
	if err := tx.QueryRowContext(ctx, "SELECT status, declared_at FROM elections WHERE id=$1 FOR UPDATE", id).Scan(&elStatus, &alreadyDeclared); err != nil {
		writeError(w, 404, "election not found")
		return
	}
	if alreadyDeclared.Valid {
		writeError(w, 409, "election result has already been declared")
		return
	}
	if elStatus != "collating" && elStatus != "closed" && elStatus != "disputed" {
		writeError(w, 422, fmt.Sprintf("election must be in collating/closed state to declare (current: %s)", elStatus))
		return
	}

	var electionID int
	fmt.Sscanf(id, "%d", &electionID)
	a, err := assessDeclaration(ctx, electionID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	// Completeness gate — fail closed.
	if !a.Complete {
		writeJSON(w, 422, M{
			"error":       "declaration blocked: not all polling units are accounted for",
			"assessment":  a,
			"missing_pus": a.MissingPUs,
		})
		return
	}
	if a.OpenDisputes > 0 {
		writeJSON(w, 422, M{
			"error":         "declaration blocked: unresolved disputes remain",
			"assessment":    a,
			"open_disputes": a.OpenDisputes,
		})
		return
	}
	// Electoral rules — margin-of-lead / tie / spread (R5-019).
	if a.Inconclusive && !req.DeclareInconclusive {
		writeJSON(w, 422, M{
			"error":      "election is inconclusive under the margin-of-lead/spread rules",
			"reason":     a.InconclusiveWhy,
			"assessment": a,
			"next_step":  "create a supplementary/rerun election (POST /elections/{id}/reruns) or explicitly declare inconclusive",
		})
		return
	}
	if a.WinnerParty == "" && !req.DeclareInconclusive {
		writeJSON(w, 422, M{"error": "no finalized results — nothing to declare", "assessment": a})
		return
	}

	outcome := "winner_declared"
	if req.DeclareInconclusive || a.Inconclusive {
		outcome = "inconclusive"
	}
	payload := M{
		"outcome":        outcome,
		"winner_party":   nil,
		"party_totals":   a.PartyTotals,
		"margin":         a.Margin,
		"assessment":     a,
		"declared_by":    username,
		"declared_at":    time.Now().UTC().Format(time.RFC3339),
		"election_id":    electionID,
		"notes":          req.Notes,
	}
	if outcome == "winner_declared" {
		payload["winner_party"] = a.WinnerParty
		payload["winner_votes"] = a.WinnerVotes
		payload["runner_up_party"] = a.RunnerUpParty
		payload["runner_up_votes"] = a.RunnerUpVotes
	}
	payloadJSON, _ := json.Marshal(payload)

	if _, err := tx.ExecContext(ctx, `UPDATE elections SET status='declared', declared_at=NOW(),
		declared_by=$2, winner_payload=$3, declaration_notes=$4, updated_at=NOW() WHERE id=$1`,
		id, username, string(payloadJSON), req.Notes); err != nil {
		writeError(w, 500, "failed to persist declaration")
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO election_state_log (election_id, from_state, to_state, event, actor, created_at)
		VALUES ($1,$2,'declared','declare',$3,CURRENT_TIMESTAMP)`, id, elStatus, username); err != nil {
		writeError(w, 500, "failed to record declaration in state log")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "failed to commit declaration")
		return
	}

	logAudit("RESULT_DECLARED", "election", id, claimUserID(claims), map[string]interface{}{
		"outcome": outcome, "winner_party": payload["winner_party"], "margin": a.Margin,
		"declared_by": username, "notes": req.Notes,
	})
	if mwHub != nil && mwHub.Kafka != nil {
		mwHub.Kafka.Produce(ctx, KafkaMessage{
			Topic: "inec.election.lifecycle",
			Key:   id,
			Value: map[string]interface{}{"event": "result_declared", "election_id": electionID, "outcome": outcome, "winner": payload["winner_party"]},
		})
	}
	writeJSON(w, 200, M{"status": "declared", "outcome": outcome, "declaration": payload})
}

// handleGetDeclaration returns the persisted declaration record.
func handleGetDeclaration(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var declaredBy, payload, notes sql.NullString
	var declaredAt sql.NullTime
	var status string
	err := db.QueryRowContext(r.Context(),
		"SELECT status, declared_at, declared_by, winner_payload, declaration_notes FROM elections WHERE id=$1", id).
		Scan(&status, &declaredAt, &declaredBy, &payload, &notes)
	if err != nil {
		writeError(w, 404, "election not found")
		return
	}
	if !declaredAt.Valid {
		writeJSON(w, 200, M{"declared": false, "status": status})
		return
	}
	var winner map[string]interface{}
	if payload.Valid {
		json.Unmarshal([]byte(payload.String), &winner)
	}
	writeJSON(w, 200, M{
		"declared": true, "status": status, "declared_at": declaredAt.Time,
		"declared_by": declaredBy.String, "winner": winner, "notes": notes.String,
	})
}

// handleDeclarationAssessment exposes the rules engine read-only (pre-declaration check).
func handleDeclarationAssessment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var electionID int
	fmt.Sscanf(id, "%d", &electionID)
	a, err := assessDeclaration(r.Context(), electionID)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

// ── Correction / supersession (R5-016) ──

// handleCorrectResult supersedes a wrong result with a corrected capture.
// History is never mutated: the old result is marked 'superseded', the new
// result is linked via supersedes_result_id and enters the normal
// validation pipeline; a result_corrections row + audit entry record who
// corrected what, when, and why. Corrections may reference a dispute.
// POST /results/{id}/correct {party_scores, accredited_voters, rejected_votes, reason, dispute_id?}
func handleCorrectResult(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardRole(w, r, "admin", "collation_officer", "returning_officer")
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]
	var req struct {
		PartyScores []struct {
			PartyCode string `json:"party_code"`
			Votes     int    `json:"votes"`
		} `json:"party_scores"`
		AccreditedVoters int    `json:"accredited_voters"`
		RejectedVotes    int    `json:"rejected_votes"`
		Reason           string `json:"reason"`
		DisputeID        *int   `json:"dispute_id"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Reason == "" {
		writeError(w, 400, "reason is required for a correction")
		return
	}
	if len(req.PartyScores) == 0 {
		writeError(w, 400, "party_scores are required")
		return
	}
	ctx := r.Context()
	username, _ := claims["username"].(string)
	uid := claimUserID(claims)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, 500, "database transaction error")
		return
	}
	defer tx.Rollback()

	var oldStatus, puCode string
	var electionID, regVoters int
	err = tx.QueryRowContext(ctx, `
		SELECT r.status, r.polling_unit_code, r.election_id, pu.registered_voters
		FROM results r JOIN polling_units pu ON pu.code = r.polling_unit_code
		WHERE r.id = $1 FOR UPDATE OF r`, id).Scan(&oldStatus, &puCode, &electionID, &regVoters)
	if err != nil {
		writeError(w, 404, "result not found")
		return
	}
	if oldStatus == "superseded" || oldStatus == "voided" {
		writeError(w, 409, fmt.Sprintf("result is already %s and cannot be corrected; correct the canonical result instead", oldStatus))
		return
	}

	// Validate the corrected figures (hard arithmetic rules are never
	// overridable; soft anomalies are permitted in a supervised correction).
	totalValid := 0
	partyEntries := make([]PartyVoteEntry, len(req.PartyScores))
	for i, ps := range req.PartyScores {
		totalValid += ps.Votes
		partyEntries[i] = PartyVoteEntry{PartyCode: ps.PartyCode, Votes: ps.Votes}
	}
	form := &FormEC8A{
		ElectionID:       electionID,
		PollingUnitCode:  puCode,
		RegisteredVoters: regVoters,
		AccreditedVoters: req.AccreditedVoters,
		TotalVotesPolled: totalValid + req.RejectedVotes,
		RejectedBallots:  req.RejectedVotes,
		TotalValidVotes:  totalValid,
		PartyResults:     partyEntries,
	}
	if violations := ValidateEC8A(form); hasHardViolation(violations) {
		writeError(w, 422, "corrected figures are arithmetically inconsistent: "+strings.Join(violations, "; "))
		return
	}

	// If a dispute triggered this correction, verify it exists and is linked
	// to this election/PU.
	if req.DisputeID != nil {
		var dElection int
		var dStatus string
		if err := tx.QueryRowContext(ctx, "SELECT election_id, status FROM disputes WHERE id=$1", *req.DisputeID).Scan(&dElection, &dStatus); err != nil {
			writeError(w, 400, "referenced dispute not found")
			return
		}
		if dElection != electionID {
			writeError(w, 400, "referenced dispute belongs to a different election")
			return
		}
	}

	ec8aHash := computeEC8AHash(electionID, puCode, partyEntries, req.AccreditedVoters, req.RejectedVotes)
	// Supersede FIRST: the partial unique index admits only one canonical
	// result per (election, PU), so the old row must become history before
	// its replacement is inserted.
	if _, err := tx.ExecContext(ctx, "UPDATE results SET status='superseded' WHERE id=$1", id); err != nil {
		writeError(w, 500, "failed to supersede old result")
		return
	}
	var newResultID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO results (election_id, polling_unit_code, presiding_officer_id, status,
			total_valid_votes, rejected_votes, total_votes_cast, accredited_voters,
			ec8a_hash, tigerbeetle_transfer_id, tigerbeetle_status, hyperledger_status,
			supersedes_result_id, correction_reason)
		VALUES ($1,$2,$3,'pending',$4,$5,$6,$7,$8,NULL,'NOT_APPLICABLE','PENDING',$9,$10)
		RETURNING id`,
		electionID, puCode, uid, totalValid, req.RejectedVotes, totalValid+req.RejectedVotes,
		req.AccreditedVoters, ec8aHash, id, req.Reason).Scan(&newResultID)
	if err != nil {
		writeError(w, 500, "failed to record corrected result: "+err.Error())
		return
	}
	for _, ps := range req.PartyScores {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO result_party_scores (result_id, party_code, votes) VALUES ($1,$2,$3)",
			newResultID, ps.PartyCode, ps.Votes); err != nil {
			writeError(w, 500, "failed to save corrected party scores")
			return
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO result_corrections (election_id, polling_unit_code, superseded_result_id, new_result_id, reason, dispute_id, corrected_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		electionID, puCode, id, newResultID, req.Reason, req.DisputeID, username); err != nil {
		writeError(w, 500, "failed to record correction trail")
		return
	}
	// Evidence chain on the corrected result (fail-closed, same as submit).
	policyVersionID, err := requirePolicyVersion(ctx, tx, electionID)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	if _, err := recordIntegrityEventTx(ctx, tx, integrityEventInput{
		ResultID:        newResultID,
		EventType:       "RESULT_CORRECTED",
		PolicyVersionID: policyVersionID,
		Visibility:      integrityVisibilityObserver,
		CreatedBy:       uid,
		PublicPayload:   M{"polling_unit_code": puCode, "status": "pending", "supersedes": id},
		PrivatePayload:  M{"reason": req.Reason, "party_scores": req.PartyScores, "dispute_id": req.DisputeID},
	}); err != nil {
		writeError(w, 503, "correction evidence could not be recorded: "+err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "failed to commit correction")
		return
	}

	logAudit("RESULT_CORRECTED", "result", id, uid, map[string]interface{}{
		"polling_unit": puCode, "election_id": electionID, "new_result_id": newResultID,
		"reason": req.Reason, "dispute_id": req.DisputeID, "old_status": oldStatus,
	})
	invalidateCollationCache(electionID)
	writeJSON(w, 201, M{
		"status": "corrected", "superseded_result_id": id, "new_result_id": newResultID,
		"new_status": "pending",
		"message":    "Correction recorded: old result superseded (history preserved), corrected result pending validation",
	})
}

// handleListCorrections returns the correction trail for an election or PU.
func handleListCorrections(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 0)
	puCode := r.URL.Query().Get("polling_unit_code")
	q := `SELECT id, election_id, polling_unit_code, superseded_result_id, new_result_id, reason,
		COALESCE(dispute_id,0), corrected_by, corrected_at FROM result_corrections WHERE 1=1`
	var args []interface{}
	if electionID > 0 {
		q += fmt.Sprintf(" AND election_id=$%d", len(args)+1)
		args = append(args, electionID)
	}
	if puCode != "" {
		q += fmt.Sprintf(" AND polling_unit_code=$%d", len(args)+1)
		args = append(args, puCode)
	}
	q += " ORDER BY corrected_at DESC LIMIT 200"
	rows, err := db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	corrections := []M{}
	for rows.Next() {
		var id, eid, oldID, newID, disputeID int
		var pu, reason, by, at string
		var correctedAt time.Time
		if rows.Scan(&id, &eid, &pu, &oldID, &newID, &reason, &disputeID, &by, &correctedAt) == nil {
			_ = at
			corrections = append(corrections, M{
				"id": id, "election_id": eid, "polling_unit_code": pu,
				"superseded_result_id": oldID, "new_result_id": newID,
				"reason": reason, "dispute_id": disputeID, "corrected_by": by, "corrected_at": correctedAt,
			})
		}
	}
	writeJSON(w, 200, M{"corrections": corrections, "total": len(corrections)})
}

// ── Rerun / supplementary / by-elections (R5-018) ──

// handleCreateRerun creates a rerun/supplementary/by-election scoped to a
// parent election and a set of polling units, wards, or LGAs. Results of the
// child merge into the parent's canonical collation (rerun results replace
// the parent result for scoped PUs; supplementary results add PUs that had
// no valid result).
// POST /elections/{id}/reruns {kind, title?, election_date, scope:[{scope_type, area_code, reason}]}
func handleCreateRerun(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardRole(w, r, "admin")
	if !ok {
		return
	}
	parentID := mux.Vars(r)["id"]
	var req struct {
		Kind         string `json:"kind" validate:"required"`
		Title        string `json:"title"`
		ElectionDate string `json:"election_date" validate:"required"`
		Scope        []struct {
			ScopeType string `json:"scope_type" validate:"required"`
			AreaCode  string `json:"area_code" validate:"required"`
			Reason    string `json:"reason"`
		} `json:"scope" validate:"required,min=1"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	switch req.Kind {
	case "rerun", "supplementary", "by_election":
	default:
		writeError(w, 400, "kind must be rerun, supplementary, or by_election")
		return
	}
	if _, err := time.Parse("2006-01-02", req.ElectionDate); err != nil {
		writeError(w, 400, "election_date must be YYYY-MM-DD")
		return
	}
	ctx := r.Context()
	username, _ := claims["username"].(string)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, 500, "database transaction error")
		return
	}
	defer tx.Rollback()

	var parentType, parentTitle, parentStatus string
	if err := tx.QueryRowContext(ctx,
		"SELECT election_type, title, status FROM elections WHERE id=$1 FOR UPDATE", parentID).
		Scan(&parentType, &parentTitle, &parentStatus); err != nil {
		writeError(w, 404, "parent election not found")
		return
	}
	if parentStatus == "cancelled" || parentStatus == "draft" {
		writeError(w, 422, "cannot create a rerun for a "+parentStatus+" election")
		return
	}
	if req.Title == "" {
		req.Title = parentTitle + " (" + strings.ReplaceAll(req.Kind, "_", " ") + ")"
	}

	var rerunID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO elections (title, election_type, election_date, status, election_kind, parent_election_id)
		VALUES ($1,$2,$3,'scheduled',$4,$5) RETURNING id`,
		req.Title, parentType, req.ElectionDate, req.Kind, parentID).Scan(&rerunID)
	if err != nil {
		writeError(w, 500, "failed to create "+req.Kind+" election: "+err.Error())
		return
	}
	for _, sc := range req.Scope {
		switch sc.ScopeType {
		case "polling_unit", "ward", "lga":
		default:
			writeError(w, 400, "scope_type must be polling_unit, ward, or lga")
			return
		}
		// Verify the scoped area exists (fail closed on typos).
		var exists int
		var checkQ string
		switch sc.ScopeType {
		case "polling_unit":
			checkQ = "SELECT COUNT(*) FROM polling_units WHERE code=$1"
		case "ward":
			checkQ = "SELECT COUNT(*) FROM wards WHERE code=$1"
		case "lga":
			checkQ = "SELECT COUNT(*) FROM lgas WHERE code=$1"
		}
		if err := tx.QueryRowContext(ctx, checkQ, sc.AreaCode).Scan(&exists); err != nil || exists == 0 {
			writeError(w, 400, fmt.Sprintf("scoped %s %q not found", sc.ScopeType, sc.AreaCode))
			return
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO rerun_scopes (election_id, scope_type, area_code, reason, created_by) VALUES ($1,$2,$3,$4,$5)",
			rerunID, sc.ScopeType, sc.AreaCode, sc.Reason, username); err != nil {
			writeError(w, 500, "failed to record rerun scope")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "failed to commit rerun")
		return
	}
	logAudit("RERUN_CREATED", "election", fmt.Sprintf("%d", rerunID), claimUserID(claims), map[string]interface{}{
		"kind": req.Kind, "parent_election_id": parentID, "scope": req.Scope,
	})
	writeJSON(w, 201, M{
		"id": rerunID, "kind": req.Kind, "parent_election_id": parentID,
		"status": "scheduled", "scope_entries": len(req.Scope),
		"message": "Rerun created. Results submitted for scoped polling units merge into the parent election's collation.",
	})
}

// handleListReruns lists rerun/supplementary children of an election.
func handleListReruns(w http.ResponseWriter, r *http.Request) {
	parentID := mux.Vars(r)["id"]
	rows, err := db.QueryContext(r.Context(), `
		SELECT id, title, election_kind, election_date, status FROM elections
		WHERE parent_election_id=$1 ORDER BY created_at DESC`, parentID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	reruns := []M{}
	for rows.Next() {
		var id int
		var title, kind, date, status string
		if rows.Scan(&id, &title, &kind, &date, &status) == nil {
			scopeRows, _ := db.QueryContext(r.Context(),
				"SELECT scope_type, area_code, COALESCE(reason,'') FROM rerun_scopes WHERE election_id=$1", id)
			scope := []M{}
			if scopeRows != nil {
				for scopeRows.Next() {
					var st, ac, reason string
					if scopeRows.Scan(&st, &ac, &reason) == nil {
						scope = append(scope, M{"scope_type": st, "area_code": ac, "reason": reason})
					}
				}
				scopeRows.Close()
			}
			reruns = append(reruns, M{"id": id, "title": title, "kind": kind, "election_date": date, "status": status, "scope": scope})
		}
	}
	writeJSON(w, 200, M{"reruns": reruns, "total": len(reruns)})
}

// ── Submission lookup (R5-025) ──

// handleResultLookup lets a client reconcile after a crash: fetch the
// canonical result for (election_id, polling_unit_code) without guessing
// whether a retry succeeded.
// GET /results/lookup?election_id=&polling_unit_code=
func handleResultLookup(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 0)
	puCode := r.URL.Query().Get("polling_unit_code")
	if electionID == 0 || puCode == "" {
		writeError(w, 400, "election_id and polling_unit_code are required")
		return
	}
	var id int64
	var status, submittedAt string
	var idemKey sql.NullString
	err := db.QueryRowContext(r.Context(), `
		SELECT id, status, submitted_at::text, idempotency_key FROM results
		WHERE election_id=$1 AND polling_unit_code=$2 AND status NOT IN ('superseded','voided')`,
		electionID, puCode).Scan(&id, &status, &submittedAt, &idemKey)
	if err != nil {
		writeJSON(w, 200, M{"submitted": false, "election_id": electionID, "polling_unit_code": puCode})
		return
	}
	resp := M{"submitted": true, "result_id": id, "status": status, "submitted_at": submittedAt,
		"election_id": electionID, "polling_unit_code": puCode}
	if idemKey.Valid {
		resp["idempotency_key"] = idemKey.String
	}
	writeJSON(w, 200, resp)
}

// ── Presiding-officer handover (R5-026) ──

// handleStaffHandover records an authorized mid-election officer replacement
// for a polling unit: the old assignment is ended, a new one created, and the
// change audit-logged. Device re-pointing is handled by the device gateway
// (W1 handoff); this endpoint is the personnel-of-record change.
// POST /ems/assignments/handover {election_id, polling_unit_code, from_user_id, to_user_id, role, reason}
func handleStaffHandover(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardRole(w, r, "admin")
	if !ok {
		return
	}
	var req struct {
		ElectionID      int    `json:"election_id" validate:"required"`
		PollingUnitCode string `json:"polling_unit_code" validate:"required"`
		FromUserID      int    `json:"from_user_id" validate:"required"`
		ToUserID        int    `json:"to_user_id" validate:"required"`
		Role            string `json:"role"`
		Reason          string `json:"reason" validate:"required"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Role == "" {
		req.Role = "presiding_officer"
	}
	ctx := r.Context()
	username, _ := claims["username"].(string)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, 500, "database transaction error")
		return
	}
	defer tx.Rollback()

	// Verify both officers exist and the outgoing officer is actually assigned.
	var toActive int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE id=$1 AND is_active=1", req.ToUserID).Scan(&toActive); err != nil || toActive == 0 {
		writeError(w, 400, "replacement officer not found or inactive")
		return
	}
	var assignmentID int
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM staff_assignments
		WHERE election_id=$1 AND polling_unit_code=$2 AND user_id=$3
		ORDER BY id DESC LIMIT 1 FOR UPDATE`,
		req.ElectionID, req.PollingUnitCode, req.FromUserID).Scan(&assignmentID)
	if err != nil {
		writeError(w, 404, "no active assignment for the outgoing officer at this polling unit")
		return
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM staff_assignments WHERE id=$1", assignmentID); err != nil {
		writeError(w, 500, "failed to end old assignment")
		return
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO staff_assignments (user_id, election_id, role, polling_unit_code)
		VALUES ($1,$2,$3,$4)`, req.ToUserID, req.ElectionID, req.Role, req.PollingUnitCode); err != nil {
		writeError(w, 500, "failed to create new assignment")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "failed to commit handover")
		return
	}
	logAudit("OFFICER_HANDOVER", "polling_unit", req.PollingUnitCode, claimUserID(claims), map[string]interface{}{
		"election_id": req.ElectionID, "from_user_id": req.FromUserID, "to_user_id": req.ToUserID,
		"role": req.Role, "reason": req.Reason, "authorized_by": username,
	})
	writeJSON(w, 200, M{
		"status": "handover_complete", "polling_unit_code": req.PollingUnitCode,
		"from_user_id": req.FromUserID, "to_user_id": req.ToUserID,
		"message": "Officer handover recorded. Re-point the replacement device via the device gateway enrollment flow.",
	})
}

// ── Party-agent countersigning (R5-032) ──

// handleSignResultAgent captures a party agent's signature — or formal
// refusal — on a PU result, as required on the EC8A. POST /results/{id}/agent-sign
func handleSignResultAgent(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardRole(w, r, "admin", "presiding_officer", "collation_officer")
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]
	var req struct {
		PartyCode string `json:"party_code" validate:"required"`
		AgentName string `json:"agent_name" validate:"required"`
		Decision  string `json:"decision" validate:"required"`
		Reason    string `json:"reason"`
	}
	if err := decodeAndValidate(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Decision != "signed" && req.Decision != "refused" {
		writeError(w, 400, "decision must be signed or refused")
		return
	}
	if req.Decision == "refused" && req.Reason == "" {
		writeError(w, 400, "a refusal must state its reason")
		return
	}
	var resultStatus string
	if err := db.QueryRowContext(r.Context(), "SELECT status FROM results WHERE id=$1", id).Scan(&resultStatus); err != nil {
		writeError(w, 404, "result not found")
		return
	}
	if resultStatus == "superseded" || resultStatus == "voided" {
		writeError(w, 409, "cannot countersign a "+resultStatus+" result; sign the canonical result")
		return
	}
	username, _ := claims["username"].(string)
	if _, err := db.ExecContext(r.Context(), `
		INSERT INTO result_agent_signatures (result_id, party_code, agent_name, decision, reason, signed_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (result_id, party_code) DO UPDATE SET agent_name=EXCLUDED.agent_name,
			decision=EXCLUDED.decision, reason=EXCLUDED.reason, signed_by=EXCLUDED.signed_by, signed_at=now()`,
		id, req.PartyCode, req.AgentName, req.Decision, req.Reason, username); err != nil {
		writeError(w, 500, "failed to record agent signature")
		return
	}
	logAudit("RESULT_AGENT_"+strings.ToUpper(req.Decision), "result", id, claimUserID(claims), map[string]interface{}{
		"party_code": req.PartyCode, "agent_name": req.AgentName, "reason": req.Reason,
	})
	writeJSON(w, 201, M{"result_id": id, "party_code": req.PartyCode, "decision": req.Decision})
}
