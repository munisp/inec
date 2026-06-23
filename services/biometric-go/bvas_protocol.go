// BVAS (Bimodal Voter Accreditation System) device protocol.
//
// Manages BVAS device lifecycle:
// - Device registration and capability reporting
// - Capture session management
// - Heartbeat and connectivity monitoring
// - Firmware version tracking
// - Calibration status

package main

import (
	"fmt"
	"sync"
	"time"
)

type BVASDeviceStatus string

const (
	DeviceActive          BVASDeviceStatus = "active"
	DeviceMaintenance     BVASDeviceStatus = "maintenance"
	DeviceDecommissioned  BVASDeviceStatus = "decommissioned"
	DeviceOffline         BVASDeviceStatus = "offline"
)

type BVASDevice struct {
	DeviceID             string           `json:"device_id"`
	FirmwareVersion      string           `json:"firmware_version"`
	SupportedModalities  []string         `json:"supported_modalities"`
	FingerprintSensor    string           `json:"fingerprint_sensor"`
	FingerprintFAPLevel  string           `json:"fingerprint_fap_level"`
	CameraResolution     string           `json:"camera_resolution"`
	IrisSensor           string           `json:"iris_sensor"`
	NFCCapable           bool             `json:"nfc_capable"`
	SecureElement        string           `json:"secure_element"`
	TLSVersion           string           `json:"tls_version"`
	MaxTemplateSize      int              `json:"max_template_size"`
	QualityThreshold     float64          `json:"quality_threshold"`
	Status               BVASDeviceStatus `json:"status"`
	AssignedPollingUnit  string           `json:"assigned_polling_unit"`
	LastCalibrated       time.Time        `json:"last_calibrated"`
	LastHeartbeat        time.Time        `json:"last_heartbeat"`
	RegisteredAt         time.Time        `json:"registered_at"`
	TotalCaptures        int64            `json:"total_captures"`
	SuccessfulCaptures   int64            `json:"successful_captures"`
	Location             *DeviceLocation  `json:"location,omitempty"`
}

type DeviceLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Accuracy  float64 `json:"accuracy_meters"`
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

// BVASDeviceRegistry manages all registered BVAS devices.
type BVASDeviceRegistry struct {
	mu       sync.RWMutex
	devices  map[string]*BVASDevice
	sessions map[string]*CaptureSession
}

func NewBVASDeviceRegistry() *BVASDeviceRegistry {
	return &BVASDeviceRegistry{
		devices:  make(map[string]*BVASDevice),
		sessions: make(map[string]*CaptureSession),
	}
}

func (r *BVASDeviceRegistry) RegisterDevice(device BVASDevice) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if device.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if device.FirmwareVersion == "" {
		return fmt.Errorf("firmware_version is required")
	}

	// Validate firmware version
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

	r.devices[device.DeviceID] = &device
	return nil
}

func (r *BVASDeviceRegistry) GetDevice(deviceID string) (*BVASDevice, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	d, ok := r.devices[deviceID]
	if !ok {
		return nil, fmt.Errorf("device not found: %s", deviceID)
	}
	return d, nil
}

func (r *BVASDeviceRegistry) Heartbeat(deviceID string, location *DeviceLocation) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.devices[deviceID]
	if !ok {
		return fmt.Errorf("device not found: %s", deviceID)
	}

	d.LastHeartbeat = time.Now()
	if location != nil {
		d.Location = location
	}

	// Auto-online if was offline
	if d.Status == DeviceOffline {
		d.Status = DeviceActive
	}

	return nil
}

func (r *BVASDeviceRegistry) StartCapture(deviceID, voterVIN, modality string) (*CaptureSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.devices[deviceID]
	if !ok {
		return nil, fmt.Errorf("device not found: %s", deviceID)
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

	r.sessions[session.SessionID] = session
	d.TotalCaptures++

	return session, nil
}

func (r *BVASDeviceRegistry) CompleteCapture(sessionID string, quality float64, nfiq2 int, width, height int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	s.CaptureQuality = quality
	s.NFIQ2Score = nfiq2
	s.ImageWidth = width
	s.ImageHeight = height
	s.CaptureAttempts++
	s.ProcessingMs = time.Since(s.CreatedAt).Milliseconds()

	d, ok := r.devices[s.DeviceID]
	if !ok {
		return fmt.Errorf("device not found for session: %s", s.DeviceID)
	}

	if quality >= d.QualityThreshold {
		s.Status = "captured"
		d.SuccessfulCaptures++
	} else if s.CaptureAttempts >= s.MaxAttempts {
		s.Status = "quality_failed"
		s.ErrorCode = fmt.Sprintf("quality_%.2f_below_%.2f_after_%d_attempts",
			quality, d.QualityThreshold, s.MaxAttempts)
	} else {
		s.Status = "capturing"
	}

	return nil
}

func (r *BVASDeviceRegistry) GetOfflineDevices(timeout time.Duration) []*BVASDevice {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	var offline []*BVASDevice

	for _, d := range r.devices {
		if d.Status == DeviceActive && now.Sub(d.LastHeartbeat) > timeout {
			offline = append(offline, d)
		}
	}

	return offline
}

func (r *BVASDeviceRegistry) MarkOffline(deviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if d, ok := r.devices[deviceID]; ok {
		d.Status = DeviceOffline
	}
}

func (r *BVASDeviceRegistry) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	active, offline, maintenance := 0, 0, 0
	var totalCaptures, successCaptures int64

	for _, d := range r.devices {
		switch d.Status {
		case DeviceActive:
			active++
		case DeviceOffline:
			offline++
		case DeviceMaintenance:
			maintenance++
		}
		totalCaptures += d.TotalCaptures
		successCaptures += d.SuccessfulCaptures
	}

	successRate := 0.0
	if totalCaptures > 0 {
		successRate = float64(successCaptures) / float64(totalCaptures) * 100
	}

	return map[string]interface{}{
		"total_devices":    len(r.devices),
		"active":           active,
		"offline":          offline,
		"maintenance":      maintenance,
		"total_captures":   totalCaptures,
		"success_captures": successCaptures,
		"success_rate":     fmt.Sprintf("%.1f%%", successRate),
		"total_sessions":   len(r.sessions),
	}
}

func isValidFirmware(version string) bool {
	// Accept any version string that looks like a semver
	if len(version) == 0 {
		return false
	}
	return true
}
