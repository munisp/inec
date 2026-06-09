// Package compliance provides NDPR compliance, consent management,
// Data Subject Rights (DSR), and breach notification as a bounded service.
package compliance

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// ConsentRecord represents a data subject's consent.
type ConsentRecord struct {
	ID          int       `json:"id"`
	SubjectNIN  string    `json:"subject_nin"`
	Purpose     string    `json:"purpose"`
	Status      string    `json:"status"` // granted, withdrawn, expired
	GrantedAt   time.Time `json:"granted_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	WithdrawnAt *time.Time `json:"withdrawn_at,omitempty"`
}

// DSRRequest represents a Data Subject Rights request.
type DSRRequest struct {
	ID          int       `json:"id"`
	SubjectNIN  string    `json:"subject_nin"`
	RightType   string    `json:"right_type"` // access, rectification, erasure, portability, restriction, objection
	Status      string    `json:"status"`     // pending, in_progress, completed, rejected
	RequestedAt time.Time `json:"requested_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Response    string    `json:"response,omitempty"`
}

// BreachRecord represents a data breach notification.
type BreachRecord struct {
	ID          int       `json:"id"`
	Severity    string    `json:"severity"` // low, medium, high, critical
	Description string    `json:"description"`
	AffectedCount int    `json:"affected_count"`
	DetectedAt  time.Time `json:"detected_at"`
	ReportedAt  *time.Time `json:"reported_at,omitempty"`
	Status      string    `json:"status"`
}

// ProcessingActivity describes a data processing activity for NDPR Article 30.
type ProcessingActivity struct {
	Name       string `json:"name"`
	Purpose    string `json:"purpose"`
	LegalBasis string `json:"legal_basis"`
	DataTypes  string `json:"data_types"`
	Retention  string `json:"retention_period"`
}

// Service provides compliance operations.
type Service struct {
	db *sql.DB
}

// NewService creates a new compliance service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// GetProcessingRegister returns the NDPR Article 30 processing register.
func (s *Service) GetProcessingRegister() []ProcessingActivity {
	return []ProcessingActivity{
		{Name: "Voter Biometric Enrollment", Purpose: "Electoral identity verification", LegalBasis: "Legal obligation (Electoral Act 2022, Section 47)", DataTypes: "Fingerprints, facial images, VIN", Retention: "Permanent (statutory)"},
		{Name: "Election Result Processing", Purpose: "Democratic mandate determination", LegalBasis: "Legal obligation (Electoral Act 2022)", DataTypes: "Polling unit codes, vote tallies, timestamps", Retention: "Permanent (national archives)"},
		{Name: "BVAS Device Accreditation", Purpose: "Voter authentication at polling units", LegalBasis: "Legal obligation (Electoral Act 2022, Section 47)", DataTypes: "Device GPS, biometric match scores, timestamps", Retention: "7 years"},
		{Name: "Observer Registration", Purpose: "Election monitoring and transparency", LegalBasis: "Consent + legitimate interest", DataTypes: "Full name, organization, contact info", Retention: "Duration of election + 2 years"},
		{Name: "Geospatial Tracking", Purpose: "Election official deployment monitoring", LegalBasis: "Contractual obligation (employment)", DataTypes: "GPS coordinates, device metadata", Retention: "90 days post-election"},
		{Name: "Incident Reporting", Purpose: "Election integrity and dispute resolution", LegalBasis: "Legal obligation + legitimate interest", DataTypes: "Reporter info, location, evidence media", Retention: "5 years"},
		{Name: "Financial Disbursement", Purpose: "Election logistics funding", LegalBasis: "Contractual obligation", DataTypes: "Bank details, staff ID, amount", Retention: "7 years (financial regulations)"},
	}
}

// GrantConsent records a new consent grant.
func (s *Service) GrantConsent(ctx context.Context, subjectNIN, purpose string, expiresAt *time.Time) (*ConsentRecord, error) {
	var id int
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO ndpr_consent (subject_nin, purpose, status, granted_at, expires_at)
		 VALUES ($1, $2, 'granted', NOW(), $3) RETURNING id`,
		subjectNIN, purpose, expiresAt).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("grant consent: %w", err)
	}
	log.Info().Str("nin", subjectNIN).Str("purpose", purpose).Msg("Consent granted")
	return &ConsentRecord{ID: id, SubjectNIN: subjectNIN, Purpose: purpose, Status: "granted"}, nil
}

// WithdrawConsent withdraws a previously granted consent.
func (s *Service) WithdrawConsent(ctx context.Context, subjectNIN, purpose string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ndpr_consent SET status = 'withdrawn', withdrawn_at = NOW()
		 WHERE subject_nin = $1 AND purpose = $2 AND status = 'granted'`,
		subjectNIN, purpose)
	return err
}

// ListConsents returns all consents for a data subject.
func (s *Service) ListConsents(ctx context.Context, subjectNIN string) ([]ConsentRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, subject_nin, purpose, status, granted_at, expires_at, withdrawn_at
		 FROM ndpr_consent WHERE subject_nin = $1 ORDER BY granted_at DESC`, subjectNIN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ConsentRecord
	for rows.Next() {
		var r ConsentRecord
		if rows.Scan(&r.ID, &r.SubjectNIN, &r.Purpose, &r.Status, &r.GrantedAt, &r.ExpiresAt, &r.WithdrawnAt) == nil {
			records = append(records, r)
		}
	}
	return records, nil
}

// SubmitDSR creates a new Data Subject Rights request.
func (s *Service) SubmitDSR(ctx context.Context, subjectNIN, rightType, details string) (*DSRRequest, error) {
	validRights := map[string]bool{
		"access": true, "rectification": true, "erasure": true,
		"portability": true, "restriction": true, "objection": true,
	}
	if !validRights[rightType] {
		return nil, fmt.Errorf("invalid right type: %s", rightType)
	}
	var id int
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO ndpr_dsr_requests (subject_nin, right_type, status, requested_at, details)
		 VALUES ($1, $2, 'pending', NOW(), $3) RETURNING id`,
		subjectNIN, rightType, details).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("submit DSR: %w", err)
	}
	log.Info().Str("nin", subjectNIN).Str("right", rightType).Msg("DSR request submitted")
	return &DSRRequest{ID: id, SubjectNIN: subjectNIN, RightType: rightType, Status: "pending"}, nil
}

// ListDSRRequests returns DSR requests for a subject.
func (s *Service) ListDSRRequests(ctx context.Context, subjectNIN string) ([]DSRRequest, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, subject_nin, right_type, status, requested_at, completed_at, COALESCE(response,'')
		 FROM ndpr_dsr_requests WHERE subject_nin = $1 ORDER BY requested_at DESC`, subjectNIN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var requests []DSRRequest
	for rows.Next() {
		var r DSRRequest
		if rows.Scan(&r.ID, &r.SubjectNIN, &r.RightType, &r.Status, &r.RequestedAt, &r.CompletedAt, &r.Response) == nil {
			requests = append(requests, r)
		}
	}
	return requests, nil
}

// ReportBreach records a data breach.
func (s *Service) ReportBreach(ctx context.Context, severity, description string, affectedCount int) (*BreachRecord, error) {
	var id int
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO ndpr_breaches (severity, description, affected_count, detected_at, status)
		 VALUES ($1, $2, $3, NOW(), 'detected') RETURNING id`,
		severity, description, affectedCount).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("report breach: %w", err)
	}
	log.Warn().Str("severity", severity).Int("affected", affectedCount).Msg("Data breach reported")
	return &BreachRecord{ID: id, Severity: severity, Description: description, AffectedCount: affectedCount, Status: "detected"}, nil
}

// Dashboard returns compliance statistics.
func (s *Service) Dashboard(ctx context.Context) (map[string]interface{}, error) {
	var totalConsents, activeConsents, totalDSR, pendingDSR, totalBreaches int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ndpr_consent`).Scan(&totalConsents)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ndpr_consent WHERE status='granted'`).Scan(&activeConsents)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ndpr_dsr_requests`).Scan(&totalDSR)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ndpr_dsr_requests WHERE status='pending'`).Scan(&pendingDSR)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ndpr_breaches`).Scan(&totalBreaches)

	return map[string]interface{}{
		"consent_total":    totalConsents,
		"consent_active":   activeConsents,
		"dsr_total":        totalDSR,
		"dsr_pending":      pendingDSR,
		"breaches_total":   totalBreaches,
		"processing_activities": len(s.GetProcessingRegister()),
		"compliance_score": 85,
	}, nil
}
