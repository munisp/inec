// BVAS (Bimodal Voter Accreditation System) device protocol.
//
// ALL STATE PERSISTED TO POSTGRESQL — zero in-memory storage.
//
// Manages BVAS device lifecycle:
// - Device registration and capability reporting
// - Capture session management
// - Heartbeat and connectivity monitoring
// - Firmware version tracking

package main

import (
	"context"
	"fmt"
	"time"
)

type BVASDeviceStatus string

const (
	DeviceActive         BVASDeviceStatus = "active"
	DeviceMaintenance    BVASDeviceStatus = "maintenance"
	DeviceDecommissioned BVASDeviceStatus = "decommissioned"
	DeviceOffline        BVASDeviceStatus = "offline"
)

type BVASDevice struct {
	DeviceID            string           `json:"device_id"`
	FirmwareVersion     string           `json:"firmware_version"`
	SupportedModalities []string         `json:"supported_modalities"`
	FingerprintSensor   string           `json:"fingerprint_sensor"`
	FingerprintFAPLevel string           `json:"fingerprint_fap_level"`
	CameraResolution    string           `json:"camera_resolution"`
	IrisSensor          string           `json:"iris_sensor"`
	NFCCapable          bool             `json:"nfc_capable"`
	SecureElement       string           `json:"secure_element"`
	TLSVersion          string           `json:"tls_version"`
	MaxTemplateSize     int              `json:"max_template_size"`
	QualityThreshold    float64          `json:"quality_threshold"`
	Status              BVASDeviceStatus `json:"status"`
	AssignedPollingUnit string           `json:"assigned_polling_unit"`
	LastCalibrated      time.Time        `json:"last_calibrated"`
	LastHeartbeat       time.Time        `json:"last_heartbeat"`
	RegisteredAt        time.Time        `json:"registered_at"`
	TotalCaptures       int64            `json:"total_captures"`
	SuccessfulCaptures  int64            `json:"successful_captures"`
	Location            *DeviceLocation  `json:"location,omitempty"`
}

type DeviceLocation struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Accuracy  float64   `json:"accuracy_meters"`
	Timestamp time.Time `json:"timestamp"`
}

type CaptureSession struct {
	SessionID       string    `json:"session_id"`
	DeviceID        string    `json:"device_id"`
	VoterVIN        string    `json:"voter_vin"`
	Modality        string    `json:"modality"`
	CaptureQuality  float64   `json:"capture_quality"`
	NFIQ2Score      int       `json:"nfiq2_score"`
	CaptureAttempts int       `json:"capture_attempts"`
	MaxAttempts     int       `json:"max_attempts"`
	ImageWidth      int       `json:"image_width"`
	ImageHeight     int       `json:"image_height"`
	ImageDPI        int       `json:"image_dpi"`
	Status          string    `json:"status"`
	ErrorCode       string    `json:"error_code,omitempty"`
	ProcessingMs    int64     `json:"processing_time_ms"`
	CreatedAt       time.Time `json:"created_at"`
}

// BVASDeviceRegistry — all state in PostgreSQL via PGStore.
type BVASDeviceRegistry struct {
	store *PGStore
}

func NewBVASDeviceRegistry(store *PGStore) *BVASDeviceRegistry {
	return &BVASDeviceRegistry{store: store}
}

func (r *BVASDeviceRegistry) RegisterDevice(device BVASDevice) error {
	if device.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if device.FirmwareVersion == "" {
		return fmt.Errorf("firmware_version is required")
	}

	if !isValidFirmware(device.FirmwareVersion) {
		return fmt.Errorf("unsupported firmware version: %s", device.FirmwareVersion)
	}

	// Set defaults
	if device.FingerprintFAPLevel == "" {
		device.FingerprintFAPLevel = "FAP30"
	}
	if device.TLSVersion == "" {
		device.TLSVersion = "TLS1.3"
	}
	if device.QualityThreshold == 0 {
		device.QualityThreshold = 0.7
	}
	if device.MaxTemplateSize == 0 {
		device.MaxTemplateSize = 100 * 1024 // 100KB
	}

	device.Status = DeviceActive
	device.RegisteredAt = time.Now()
	device.LastHeartbeat = time.Now()

	return r.store.RegisterDevice(context.Background(), &device)
}

func (r *BVASDeviceRegistry) GetDevice(deviceID string) (*BVASDevice, error) {
	return r.store.GetDevice(context.Background(), deviceID)
}

func (r *BVASDeviceRegistry) Heartbeat(deviceID string, location *DeviceLocation) error {
	var lat, lng, accuracy *float64
	if location != nil {
		lat = &location.Latitude
		lng = &location.Longitude
		accuracy = &location.Accuracy
	}
	return r.store.UpdateDeviceHeartbeat(context.Background(), deviceID, lat, lng, accuracy)
}

func (r *BVASDeviceRegistry) StartCapture(deviceID, voterVIN, modality string) (*CaptureSession, error) {
	ctx := context.Background()
	d, err := r.store.GetDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	if d.Status != DeviceActive {
		return nil, fmt.Errorf("device %s is not active (status: %s)", deviceID, d.Status)
	}

	// Check modality support
	supported := false
	for _, m := range d.SupportedModalities {
		if m == modality {
			supported = true
			break
		}
	}
	if !supported {
		return nil, fmt.Errorf("device %s does not support modality: %s", deviceID, modality)
	}

	session := &CaptureSession{
		SessionID:   fmt.Sprintf("cap-%d", time.Now().UnixNano()),
		DeviceID:    deviceID,
		VoterVIN:    voterVIN,
		Modality:    modality,
		MaxAttempts: 3,
		ImageDPI:    500,
		Status:      "initiated",
		CreatedAt:   time.Now(),
	}

	if err := r.store.CreateCaptureSession(ctx, session); err != nil {
		return nil, err
	}

	return session, nil
}

func (r *BVASDeviceRegistry) CompleteCapture(sessionID string, quality float64, nfiq2 int, width, height int) error {
	ctx := context.Background()
	session, err := r.store.GetCaptureSession(ctx, sessionID)
	if err != nil {
		return err
	}

	// Determine status based on quality
	d, err := r.store.GetDevice(ctx, session.DeviceID)
	if err != nil {
		return err
	}

	status := "capturing"
	errCode := ""
	if quality >= d.QualityThreshold {
		status = "captured"
	} else {
		status = "quality_failed"
		errCode = fmt.Sprintf("quality_%.2f_below_%.2f", quality, d.QualityThreshold)
	}

	return r.store.CompleteCaptureSession(ctx, sessionID, quality, nfiq2, width, height, status, errCode)
}

func (r *BVASDeviceRegistry) GetStats() map[string]interface{} {
	return r.store.GetDeviceStats(context.Background())
}

func isValidFirmware(version string) bool {
	if len(version) == 0 {
		return false
	}
	return true
}
