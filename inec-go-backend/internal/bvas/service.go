// Package bvas provides Biometric Voter Authentication System device management,
// accreditation workflows, and device fleet monitoring as a bounded service.
package bvas

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// DeviceStatus represents a BVAS device's operational state.
type DeviceStatus string

const (
	StatusActive       DeviceStatus = "active"
	StatusInactive     DeviceStatus = "inactive"
	StatusMaintenance  DeviceStatus = "maintenance"
	StatusDecommissioned DeviceStatus = "decommissioned"
)

// Device represents a BVAS device.
type Device struct {
	ID              string       `json:"id"`
	SerialNumber    string       `json:"serial_number"`
	Status          DeviceStatus `json:"status"`
	PollingUnitCode string       `json:"polling_unit_code,omitempty"`
	FirmwareVersion string       `json:"firmware_version"`
	BatteryLevel    int          `json:"battery_level"`
	LastSyncAt      *time.Time   `json:"last_sync_at,omitempty"`
	AssignedTo      string       `json:"assigned_to,omitempty"`
}

// AccreditationResult represents the outcome of a voter accreditation.
type AccreditationResult struct {
	ID              string    `json:"id"`
	DeviceID        string    `json:"device_id"`
	ElectionID      int       `json:"election_id"`
	VoterVIN        string    `json:"voter_vin"`
	PollingUnitCode string    `json:"polling_unit_code"`
	BiometricMatch  bool      `json:"biometric_match"`
	MatchScore      float64   `json:"match_score"`
	Status          string    `json:"status"` // accredited, rejected, flagged
	Timestamp       time.Time `json:"timestamp"`
}

// Service provides BVAS operations.
type Service struct {
	db *sql.DB
}

// NewService creates a new BVAS service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// GetDevice retrieves a BVAS device by ID.
func (s *Service) GetDevice(ctx context.Context, deviceID string) (*Device, error) {
	var d Device
	err := s.db.QueryRowContext(ctx,
		`SELECT id, serial_number, status, COALESCE(polling_unit_code,''),
		 COALESCE(firmware_version,''), COALESCE(battery_level,0), last_sync_at, COALESCE(assigned_to,'')
		 FROM bvas_devices WHERE id = $1`, deviceID).
		Scan(&d.ID, &d.SerialNumber, &d.Status, &d.PollingUnitCode,
			&d.FirmwareVersion, &d.BatteryLevel, &d.LastSyncAt, &d.AssignedTo)
	if err != nil {
		return nil, fmt.Errorf("device not found: %s", deviceID)
	}
	return &d, nil
}

// ListDevices returns all BVAS devices, optionally filtered by status.
func (s *Service) ListDevices(ctx context.Context, statusFilter string) ([]Device, error) {
	query := `SELECT id, serial_number, status, COALESCE(polling_unit_code,''),
	           COALESCE(firmware_version,''), COALESCE(battery_level,0), last_sync_at, COALESCE(assigned_to,'')
	           FROM bvas_devices`
	var args []interface{}
	if statusFilter != "" {
		query += " WHERE status = $1"
		args = append(args, statusFilter)
	}
	query += " ORDER BY id"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var devices []Device
	for rows.Next() {
		var d Device
		if rows.Scan(&d.ID, &d.SerialNumber, &d.Status, &d.PollingUnitCode,
			&d.FirmwareVersion, &d.BatteryLevel, &d.LastSyncAt, &d.AssignedTo) == nil {
			devices = append(devices, d)
		}
	}
	return devices, nil
}

// Accredit performs voter accreditation through a BVAS device.
func (s *Service) Accredit(ctx context.Context, deviceID string, electionID int, voterVIN, puCode string, matchScore float64) (*AccreditationResult, error) {
	// Validate device status
	device, err := s.GetDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if device.Status != StatusActive {
		return nil, fmt.Errorf("device %s is '%s' — must be 'active' for accreditation", deviceID, device.Status)
	}

	// Validate election status
	var electionStatus string
	err = s.db.QueryRowContext(ctx, `SELECT status FROM elections WHERE id = $1`, electionID).Scan(&electionStatus)
	if err != nil {
		return nil, fmt.Errorf("election %d not found", electionID)
	}
	if electionStatus != "voting" && electionStatus != "active" {
		return nil, fmt.Errorf("election is '%s' — accreditation only during 'voting'/'active'", electionStatus)
	}

	// Determine accreditation status
	status := "rejected"
	if matchScore >= 0.75 {
		status = "accredited"
	}

	result := &AccreditationResult{
		ID:              fmt.Sprintf("accr_%d", time.Now().UnixNano()),
		DeviceID:        deviceID,
		ElectionID:      electionID,
		VoterVIN:        voterVIN,
		PollingUnitCode: puCode,
		BiometricMatch:  matchScore >= 0.75,
		MatchScore:      matchScore,
		Status:          status,
		Timestamp:       time.Now(),
	}

	// Persist
	s.db.ExecContext(ctx,
		`INSERT INTO bvas_accreditations (id, device_id, election_id, voter_vin, polling_unit_code, biometric_match, match_score, status, timestamp)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		result.ID, result.DeviceID, result.ElectionID, result.VoterVIN,
		result.PollingUnitCode, result.BiometricMatch, result.MatchScore, result.Status, result.Timestamp)

	log.Info().Str("device", deviceID).Str("vin", voterVIN).Str("status", status).Msg("Accreditation completed")
	return result, nil
}

// FleetStats returns device fleet statistics.
func (s *Service) FleetStats(ctx context.Context) (map[string]interface{}, error) {
	var total, active, inactive, maintenance int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bvas_devices`).Scan(&total)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bvas_devices WHERE status='active'`).Scan(&active)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bvas_devices WHERE status='inactive'`).Scan(&inactive)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bvas_devices WHERE status='maintenance'`).Scan(&maintenance)

	return map[string]interface{}{
		"total":       total,
		"active":      active,
		"inactive":    inactive,
		"maintenance": maintenance,
		"utilization_pct": func() float64 {
			if total == 0 {
				return 0
			}
			return float64(active) / float64(total) * 100
		}(),
	}, nil
}
