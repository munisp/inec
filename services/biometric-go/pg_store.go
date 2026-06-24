// PostgreSQL persistence layer for the ABIS pipeline.
// Replaces ALL in-memory maps (gallery, LSH index, BVAS devices, sessions, audit).

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// PGStore provides PostgreSQL-backed persistence for all biometric data.
type PGStore struct {
	pool *pgxpool.Pool
}

func NewPGStore(ctx context.Context, connString string) (*PGStore, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parse pg config: %w", err)
	}
	cfg.MaxConns = 50
	cfg.MinConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to pg: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping pg: %w", err)
	}

	store := &PGStore{pool: pool}
	if err := store.migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

func (s *PGStore) migrate(ctx context.Context) error {
	// Tables are created by the shared migration script; ensure they exist
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS biometric_gallery (
			id BIGSERIAL PRIMARY KEY,
			voter_vin TEXT NOT NULL,
			modality TEXT NOT NULL,
			template_hash TEXT NOT NULL,
			quality_score DOUBLE PRECISION NOT NULL,
			minutiae_json JSONB,
			embedding DOUBLE PRECISION[],
			iris_code BYTEA,
			iris_mask BYTEA,
			enrolled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (voter_vin, modality)
		);
		CREATE TABLE IF NOT EXISTS lsh_index (
			id BIGSERIAL PRIMARY KEY,
			table_num INTEGER NOT NULL,
			hash_value BIGINT NOT NULL,
			voter_vin TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_lsh_lookup ON lsh_index (table_num, hash_value);
		CREATE TABLE IF NOT EXISTS bvas_devices (
			device_id TEXT PRIMARY KEY,
			firmware_version TEXT NOT NULL,
			fingerprint_fap TEXT NOT NULL DEFAULT 'FAP30',
			supported_modalities TEXT[] NOT NULL DEFAULT '{}',
			has_nfc BOOLEAN NOT NULL DEFAULT FALSE,
			has_secure_element BOOLEAN NOT NULL DEFAULT FALSE,
			tls_version TEXT NOT NULL DEFAULT 'TLS1.3',
			quality_threshold DOUBLE PRECISION NOT NULL DEFAULT 0.7,
			max_template_size INTEGER NOT NULL DEFAULT 102400,
			status TEXT NOT NULL DEFAULT 'active',
			last_heartbeat TIMESTAMPTZ,
			location_lat DOUBLE PRECISION,
			location_lng DOUBLE PRECISION,
			location_accuracy DOUBLE PRECISION,
			assigned_pu TEXT,
			registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			total_captures BIGINT NOT NULL DEFAULT 0,
			successful_captures BIGINT NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS bvas_capture_sessions (
			session_id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL,
			voter_vin TEXT NOT NULL,
			modality TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'capturing',
			quality_score DOUBLE PRECISION,
			nfiq2_score INTEGER,
			image_width INTEGER,
			image_height INTEGER,
			error_code TEXT,
			processing_ms BIGINT,
			started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMPTZ
		);
		CREATE TABLE IF NOT EXISTS abis_audit_log (
			id TEXT PRIMARY KEY,
			operation TEXT NOT NULL,
			voter_vin TEXT NOT NULL,
			modality TEXT NOT NULL,
			detail TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	return err
}

// ─── Gallery Operations ─────────────────────────────────────────

func (s *PGStore) AddToGallery(ctx context.Context, tmpl *TemplateData) error {
	minutiaeJSON, _ := json.Marshal(tmpl.Minutiae)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO biometric_gallery (voter_vin, modality, template_hash, quality_score, minutiae_json, embedding, iris_code, iris_mask)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (voter_vin, modality) DO UPDATE SET
			template_hash = EXCLUDED.template_hash,
			quality_score = EXCLUDED.quality_score,
			minutiae_json = EXCLUDED.minutiae_json,
			embedding = EXCLUDED.embedding,
			iris_code = EXCLUDED.iris_code,
			iris_mask = EXCLUDED.iris_mask,
			enrolled_at = NOW()`,
		tmpl.VoterVIN, tmpl.Modality, tmpl.TemplateHash, tmpl.QualityScore,
		minutiaeJSON, tmpl.Embedding, tmpl.IrisCode, tmpl.IrisMask,
	)
	return err
}

func (s *PGStore) GallerySize(ctx context.Context) int {
	var count int
	err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM biometric_gallery").Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (s *PGStore) GetTemplateByKey(ctx context.Context, voterVIN, modality string) (*TemplateData, error) {
	var tmpl TemplateData
	var minutiaeJSON []byte

	err := s.pool.QueryRow(ctx, `
		SELECT voter_vin, modality, template_hash, quality_score, minutiae_json, embedding, iris_code, iris_mask
		FROM biometric_gallery WHERE voter_vin = $1 AND modality = $2`,
		voterVIN, modality,
	).Scan(&tmpl.VoterVIN, &tmpl.Modality, &tmpl.TemplateHash, &tmpl.QualityScore,
		&minutiaeJSON, &tmpl.Embedding, &tmpl.IrisCode, &tmpl.IrisMask)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if minutiaeJSON != nil {
		json.Unmarshal(minutiaeJSON, &tmpl.Minutiae)
	}
	return &tmpl, nil
}

// ─── LSH Index Operations ───────────────────────────────────────

func (s *PGStore) InsertLSH(ctx context.Context, voterVIN string, tableNum int, hashValue uint64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO lsh_index (table_num, hash_value, voter_vin) VALUES ($1, $2, $3)`,
		tableNum, int64(hashValue), voterVIN,
	)
	return err
}

func (s *PGStore) QueryLSH(ctx context.Context, tableNum int, hashValue uint64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT voter_vin FROM lsh_index WHERE table_num = $1 AND hash_value = $2`,
		tableNum, int64(hashValue),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vins []string
	for rows.Next() {
		var vin string
		if err := rows.Scan(&vin); err != nil {
			continue
		}
		vins = append(vins, vin)
	}
	return vins, nil
}

// ─── BVAS Device Operations ─────────────────────────────────────

func (s *PGStore) RegisterDevice(ctx context.Context, device *BVASDevice) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO bvas_devices (device_id, firmware_version, fingerprint_fap, supported_modalities, has_nfc, has_secure_element, tls_version, quality_threshold, max_template_size, status, last_heartbeat, assigned_pu, registered_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (device_id) DO UPDATE SET
			firmware_version = EXCLUDED.firmware_version,
			status = EXCLUDED.status,
			last_heartbeat = EXCLUDED.last_heartbeat`,
		device.DeviceID, device.FirmwareVersion, device.FingerprintFAPLevel,
		device.SupportedModalities, device.NFCCapable, device.SecureElement != "",
		device.TLSVersion, device.QualityThreshold, device.MaxTemplateSize,
		string(device.Status), device.LastHeartbeat, device.AssignedPollingUnit, device.RegisteredAt,
	)
	return err
}

func (s *PGStore) GetDevice(ctx context.Context, deviceID string) (*BVASDevice, error) {
	var d BVASDevice
	var status string
	var hasSecure bool
	err := s.pool.QueryRow(ctx, `
		SELECT device_id, firmware_version, fingerprint_fap, supported_modalities,
		       has_nfc, has_secure_element, tls_version, quality_threshold,
		       max_template_size, status, last_heartbeat, assigned_pu, registered_at,
		       total_captures, successful_captures
		FROM bvas_devices WHERE device_id = $1`, deviceID,
	).Scan(&d.DeviceID, &d.FirmwareVersion, &d.FingerprintFAPLevel,
		&d.SupportedModalities, &d.NFCCapable, &hasSecure,
		&d.TLSVersion, &d.QualityThreshold, &d.MaxTemplateSize,
		&status, &d.LastHeartbeat, &d.AssignedPollingUnit, &d.RegisteredAt,
		&d.TotalCaptures, &d.SuccessfulCaptures)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("device not found: %s", deviceID)
	}
	if err != nil {
		return nil, err
	}
	d.Status = BVASDeviceStatus(status)
	if hasSecure {
		d.SecureElement = "hardware"
	}
	return &d, nil
}

func (s *PGStore) UpdateDeviceHeartbeat(ctx context.Context, deviceID string, lat, lng, accuracy *float64) error {
	if lat != nil {
		_, err := s.pool.Exec(ctx, `
			UPDATE bvas_devices SET last_heartbeat = NOW(), status = 'active',
			location_lat = $2, location_lng = $3, location_accuracy = $4
			WHERE device_id = $1`, deviceID, *lat, *lng, *accuracy)
		return err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE bvas_devices SET last_heartbeat = NOW(), status = 'active' WHERE device_id = $1`, deviceID)
	return err
}

func (s *PGStore) ListDevices(ctx context.Context) ([]*BVASDevice, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT device_id, firmware_version, fingerprint_fap, supported_modalities,
		       has_nfc, has_secure_element, tls_version, quality_threshold,
		       max_template_size, status, last_heartbeat, assigned_pu, registered_at,
		       total_captures, successful_captures
		FROM bvas_devices ORDER BY registered_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*BVASDevice
	for rows.Next() {
		var d BVASDevice
		var status string
		var hasSecure bool
		if err := rows.Scan(&d.DeviceID, &d.FirmwareVersion, &d.FingerprintFAPLevel,
			&d.SupportedModalities, &d.NFCCapable, &hasSecure,
			&d.TLSVersion, &d.QualityThreshold, &d.MaxTemplateSize,
			&status, &d.LastHeartbeat, &d.AssignedPollingUnit, &d.RegisteredAt,
			&d.TotalCaptures, &d.SuccessfulCaptures); err != nil {
			continue
		}
		d.Status = BVASDeviceStatus(status)
		devices = append(devices, &d)
	}
	return devices, nil
}

func (s *PGStore) GetDeviceStats(ctx context.Context) map[string]interface{} {
	var total, active, offline, maintenance int
	var totalCaptures, successCaptures int64

	s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM bvas_devices").Scan(&total)
	s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM bvas_devices WHERE status = 'active'").Scan(&active)
	s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM bvas_devices WHERE status = 'offline'").Scan(&offline)
	s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM bvas_devices WHERE status = 'maintenance'").Scan(&maintenance)
	s.pool.QueryRow(ctx, "SELECT COALESCE(SUM(total_captures), 0) FROM bvas_devices").Scan(&totalCaptures)
	s.pool.QueryRow(ctx, "SELECT COALESCE(SUM(successful_captures), 0) FROM bvas_devices").Scan(&successCaptures)

	var totalSessions int
	s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM bvas_capture_sessions").Scan(&totalSessions)

	successRate := 0.0
	if totalCaptures > 0 {
		successRate = float64(successCaptures) / float64(totalCaptures) * 100
	}

	return map[string]interface{}{
		"total_devices":    total,
		"active":           active,
		"offline":          offline,
		"maintenance":      maintenance,
		"total_captures":   totalCaptures,
		"success_captures": successCaptures,
		"success_rate":     fmt.Sprintf("%.1f%%", successRate),
		"total_sessions":   totalSessions,
	}
}

// ─── Capture Session Operations ─────────────────────────────────

func (s *PGStore) CreateCaptureSession(ctx context.Context, session *CaptureSession) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO bvas_capture_sessions (session_id, device_id, voter_vin, modality, status)
		VALUES ($1, $2, $3, $4, $5)`,
		session.SessionID, session.DeviceID, session.VoterVIN, session.Modality, session.Status,
	)
	if err == nil {
		// Increment device total captures
		s.pool.Exec(ctx, "UPDATE bvas_devices SET total_captures = total_captures + 1 WHERE device_id = $1", session.DeviceID)
	}
	return err
}

func (s *PGStore) CompleteCaptureSession(ctx context.Context, sessionID string, quality float64, nfiq2, width, height int, status, errCode string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE bvas_capture_sessions SET status = $2, quality_score = $3, nfiq2_score = $4,
		       image_width = $5, image_height = $6, error_code = $7,
		       processing_ms = EXTRACT(EPOCH FROM (NOW() - started_at)) * 1000,
		       completed_at = NOW()
		WHERE session_id = $1`, sessionID, status, quality, nfiq2, width, height, errCode)
	if err == nil && status == "captured" {
		// Get device_id and increment successful captures
		var deviceID string
		s.pool.QueryRow(ctx, "SELECT device_id FROM bvas_capture_sessions WHERE session_id = $1", sessionID).Scan(&deviceID)
		if deviceID != "" {
			s.pool.Exec(ctx, "UPDATE bvas_devices SET successful_captures = successful_captures + 1 WHERE device_id = $1", deviceID)
		}
	}
	return err
}

func (s *PGStore) GetCaptureSession(ctx context.Context, sessionID string) (*CaptureSession, error) {
	var cs CaptureSession
	err := s.pool.QueryRow(ctx, `
		SELECT session_id, device_id, voter_vin, modality, status, COALESCE(quality_score, 0), COALESCE(nfiq2_score, 0)
		FROM bvas_capture_sessions WHERE session_id = $1`, sessionID,
	).Scan(&cs.SessionID, &cs.DeviceID, &cs.VoterVIN, &cs.Modality, &cs.Status, &cs.CaptureQuality, &cs.NFIQ2Score)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return &cs, err
}

// ─── Audit Log Operations ───────────────────────────────────────

func (s *PGStore) LogAudit(ctx context.Context, operation, voterVIN, modality, detail string) {
	id := fmt.Sprintf("audit-%d", time.Now().UnixNano())
	_, err := s.pool.Exec(ctx, `
		INSERT INTO abis_audit_log (id, operation, voter_vin, modality, detail) VALUES ($1, $2, $3, $4, $5)`,
		id, operation, voterVIN, modality, detail,
	)
	if err != nil {
		log.Error().Err(err).Str("operation", operation).Msg("failed to log audit")
	}
}

func (s *PGStore) GetRecentAudit(ctx context.Context, limit int) []AuditEntry {
	rows, err := s.pool.Query(ctx, `
		SELECT id, operation, voter_vin, modality, COALESCE(detail, ''), created_at
		FROM abis_audit_log ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Operation, &e.VoterVIN, &e.Modality, &e.Detail, &e.Timestamp); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

func (s *PGStore) Close() {
	s.pool.Close()
}
