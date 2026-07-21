// Package election provides election lifecycle management, FSM transitions,
// result submission, and collation logic as a bounded service context.
package election

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// State represents an election's lifecycle state.
type State string

const (
	StateDraft         State = "draft"
	StateScheduled     State = "scheduled"
	StatePreparation   State = "preparation"
	StateAccreditation State = "accreditation"
	StateVoting        State = "voting"
	StateCollation     State = "collation"
	StateDeclared      State = "declared"
	StateSuspended     State = "suspended"
	StateCancelled     State = "cancelled"
)

// ValidTransitions defines the allowed state machine transitions.
var ValidTransitions = map[State][]State{
	StateDraft:         {StateScheduled, StateCancelled},
	StateScheduled:     {StatePreparation, StateSuspended, StateCancelled},
	StatePreparation:   {StateAccreditation, StateSuspended},
	StateAccreditation: {StateVoting, StateSuspended},
	StateVoting:        {StateCollation, StateSuspended},
	StateCollation:     {StateDeclared, StateSuspended},
	StateSuspended:     {StateVoting, StateCollation, StateCancelled},
}

// Election represents a single election.
type Election struct {
	ID          int        `json:"id"`
	Title       string     `json:"title"`
	Type        string     `json:"type"`
	State       State      `json:"state"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	DeclaredAt  *time.Time `json:"declared_at,omitempty"`
	TotalPUs    int        `json:"total_polling_units"`
	ResultsIn   int        `json:"results_received"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Result represents a polling unit result submission.
type Result struct {
	ID               int            `json:"id"`
	ElectionID       int            `json:"election_id"`
	PollingUnitCode  string         `json:"polling_unit_code"`
	PollingUnitName  string         `json:"polling_unit_name"`
	State            string         `json:"state"`
	LGA              string         `json:"lga"`
	Ward             string         `json:"ward"`
	PartyVotes       map[string]int `json:"party_votes"`
	TotalVotes       int            `json:"total_votes"`
	RejectedVotes    int            `json:"rejected_votes"`
	AccreditedVoters int            `json:"accredited_voters"`
	Status           string         `json:"status"` // pending, validated, finalized, disputed
	SubmittedBy      int            `json:"submitted_by"`
	SubmittedAt      time.Time      `json:"submitted_at"`
	ValidatedAt      *time.Time     `json:"validated_at,omitempty"`
	FinalizedAt      *time.Time     `json:"finalized_at,omitempty"`
}

// CollationSummary provides aggregated results.
type CollationSummary struct {
	ElectionID     int            `json:"election_id"`
	TotalPUs       int            `json:"total_polling_units"`
	ResultsIn      int            `json:"results_received"`
	TotalVotes     int            `json:"total_votes"`
	RejectedVotes  int            `json:"rejected_votes"`
	PartyTotals    map[string]int `json:"party_totals"`
	StateBreakdown []StateResult  `json:"state_breakdown"`
	Completion     float64        `json:"completion_percentage"`
}

// StateResult is collation per state.
type StateResult struct {
	State       string         `json:"state"`
	PUsReported int            `json:"polling_units_reported"`
	TotalPUs    int            `json:"total_polling_units"`
	TotalVotes  int            `json:"total_votes"`
	PartyVotes  map[string]int `json:"party_votes"`
}

// Service provides election management operations.
type Service struct {
	db *sql.DB
}

// NewService creates a new election service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// List returns all elections.
func (s *Service) List(ctx context.Context) ([]Election, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.title, COALESCE(e.election_type,'general'), e.status,
		        e.election_date, e.created_at,
		        COALESCE(e.total_registered_voters, 0),
		        (SELECT COUNT(*) FROM results r WHERE r.election_id = e.id)
		 FROM elections e ORDER BY e.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query elections: %w", err)
	}
	defer rows.Close()

	var elections []Election
	for rows.Next() {
		var e Election
		var electionDate string
		if err := rows.Scan(&e.ID, &e.Title, &e.Type, &e.State,
			&electionDate, &e.CreatedAt, &e.TotalPUs, &e.ResultsIn); err != nil {
			continue
		}
		if t, err := time.Parse("2006-01-02", electionDate); err == nil {
			e.ScheduledAt = t
		} else {
			e.ScheduledAt = e.CreatedAt
		}
		elections = append(elections, e)
	}
	return elections, nil
}

// Get retrieves a single election by ID.
func (s *Service) Get(ctx context.Context, id int) (*Election, error) {
	var e Election
	var electionDate string
	err := s.db.QueryRowContext(ctx,
		`SELECT e.id, e.title, COALESCE(e.election_type,'general'), e.status,
		        e.election_date, e.created_at,
		        COALESCE(e.total_registered_voters, 0),
		        (SELECT COUNT(*) FROM results r WHERE r.election_id = e.id)
		 FROM elections e WHERE e.id = $1`, id).
		Scan(&e.ID, &e.Title, &e.Type, &e.State,
			&electionDate, &e.CreatedAt, &e.TotalPUs, &e.ResultsIn)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("election not found")
	}
	if err != nil {
		return nil, err
	}
	if t, err := time.Parse("2006-01-02", electionDate); err == nil {
		e.ScheduledAt = t
	} else {
		e.ScheduledAt = e.CreatedAt
	}
	return &e, nil
}

// Transition performs a state machine transition on an election.
func (s *Service) Transition(ctx context.Context, electionID int, targetState State, userID int) error {
	election, err := s.Get(ctx, electionID)
	if err != nil {
		return err
	}

	// Validate transition
	allowed, ok := ValidTransitions[election.State]
	if !ok {
		return fmt.Errorf("no transitions from state %s", election.State)
	}
	valid := false
	for _, s := range allowed {
		if s == targetState {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid transition: %s -> %s", election.State, targetState)
	}

	// Execute transition
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`UPDATE elections SET status = $1, updated_at = NOW() WHERE id = $2`, targetState, electionID)
	if err != nil {
		return err
	}

	// Record transition in lifecycle log
	_, err = tx.ExecContext(ctx,
		`INSERT INTO election_lifecycle (election_id, from_state, to_state, transitioned_by, reason)
		 VALUES ($1, $2, $3, $4, $5)`,
		electionID, election.State, targetState, userID, fmt.Sprintf("FSM: %s -> %s", election.State, targetState))
	if err != nil {
		return err
	}

	// Handle state-specific logic (update timestamp on key transitions)
	switch targetState {
	case StateVoting, StateDeclared:
		_, _ = tx.ExecContext(ctx, `UPDATE elections SET updated_at = NOW() WHERE id = $1`, electionID)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	log.Info().Int("election_id", electionID).Str("from", string(election.State)).Str("to", string(targetState)).Msg("Election state transitioned")
	return nil
}

// SubmitResult records a polling unit result.
func (s *Service) SubmitResult(ctx context.Context, result *Result) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO results (election_id, polling_unit_code, state, lga, ward,
		 total_votes, rejected_votes, accredited_voters, status, submitted_by, submitted_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', $9, NOW())`,
		result.ElectionID, result.PollingUnitCode, result.State, result.LGA, result.Ward,
		result.TotalVotes, result.RejectedVotes, result.AccreditedVoters, result.SubmittedBy)
	if err != nil {
		return fmt.Errorf("insert result: %w", err)
	}

	// Update election updated_at timestamp
	_, _ = tx.ExecContext(ctx,
		`UPDATE elections SET updated_at = NOW() WHERE id = $1`,
		result.ElectionID)

	return tx.Commit()
}

// Stats returns election statistics (results received, total PUs, completion).
func (s *Service) Stats(ctx context.Context, electionID int) (map[string]interface{}, error) {
	e, err := s.Get(ctx, electionID)
	if err != nil {
		return nil, err
	}
	completion := 0.0
	if e.TotalPUs > 0 {
		completion = float64(e.ResultsIn) / float64(e.TotalPUs) * 100
	}
	return map[string]interface{}{
		"election_id":    electionID,
		"state":          e.State,
		"total_pus":      e.TotalPUs,
		"results_in":     e.ResultsIn,
		"completion_pct": completion,
	}, nil
}

// ListResults returns all results for an election.
func (s *Service) ListResults(ctx context.Context, electionID int) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, election_id, polling_unit_code, COALESCE(state,''), COALESCE(lga,''), COALESCE(ward,''),
		        total_votes, COALESCE(rejected_votes,0), COALESCE(accredited_voters,0),
		        status, COALESCE(submitted_by,0), submitted_at
		 FROM results WHERE election_id = $1 ORDER BY submitted_at DESC`, electionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.ElectionID, &r.PollingUnitCode, &r.State, &r.LGA, &r.Ward,
			&r.TotalVotes, &r.RejectedVotes, &r.AccreditedVoters, &r.Status, &r.SubmittedBy, &r.SubmittedAt); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}

// Collate computes aggregated results for an election.
func (s *Service) Collate(ctx context.Context, electionID int) (*CollationSummary, error) {
	election, err := s.Get(ctx, electionID)
	if err != nil {
		return nil, err
	}

	summary := &CollationSummary{
		ElectionID:  electionID,
		TotalPUs:    election.TotalPUs,
		ResultsIn:   election.ResultsIn,
		PartyTotals: make(map[string]int),
	}

	if election.TotalPUs > 0 {
		summary.Completion = float64(election.ResultsIn) / float64(election.TotalPUs) * 100
	}

	// Aggregate totals
	err = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(total_votes), 0), COALESCE(SUM(rejected_votes), 0)
		 FROM results WHERE election_id = $1`, electionID).
		Scan(&summary.TotalVotes, &summary.RejectedVotes)
	if err != nil {
		return nil, err
	}

	// Party breakdown from the canonical result_party_scores relation.
	// Election ownership lives on results, so aggregate through the result link
	// instead of querying the retired result_parties table.
	rows, err := s.db.QueryContext(ctx,
		`SELECT rps.party_code, COALESCE(SUM(rps.votes), 0)
		 FROM result_party_scores rps
		 JOIN results r ON r.id = rps.result_id
		 WHERE r.election_id = $1
		 GROUP BY rps.party_code
		 ORDER BY SUM(rps.votes) DESC`, electionID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var party string
			var votes int
			if rows.Scan(&party, &votes) == nil {
				summary.PartyTotals[party] = votes
			}
		}
	}

	return summary, nil
}
