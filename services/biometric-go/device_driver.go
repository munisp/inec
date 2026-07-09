package main

// Biometric capture device-driver interface.
//
// Certified biometric SDKs (Innovatrics, Neurotechnology, DERMALOG) and the
// physical BVAS pucks ship proprietary, licence-gated libraries that cannot be
// vendored here. This file defines the stable driver contract the platform
// programs against, plus an open, dependency-free REFERENCE driver so the
// end-to-end capture path is real and testable without vendor hardware.
//
// Integration model (the "spool" pattern used by air-gapped, certified SDKs):
// a vendor capture agent runs next to the device, performs the certified
// acquisition + template extraction, and drops a completed capture as two files
// into a spool directory:
//     <spool>/<session>.tmpl   ISO/IEC 19794-2 fingerprint template (or 19794-6 iris / 19794-5 face)
//     <spool>/<session>.json   capture metadata (quality, NFIQ2, dpi, dimensions)
// The platform picks these up through CaptureDriver, persists them, and matches
// via the ABIS pipeline. Swapping in a linked SDK only means providing another
// CaptureDriver implementation — no caller changes.
//
// Selection is via environment:
//     BIOMETRIC_DRIVER = spool (default) | none
//     BIOMETRIC_SPOOL_DIR = /var/lib/inec/bvas-spool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DeviceInfo describes a capture device discovered by a driver.
type DeviceInfo struct {
	DeviceID     string   `json:"device_id"`
	Model        string   `json:"model"`
	Modalities   []string `json:"modalities"`
	FAPLevel     string   `json:"fap_level"`
	FirmwareVer  string   `json:"firmware_version"`
	NFCCapable   bool     `json:"nfc_capable"`
	SerialNumber string   `json:"serial_number"`
}

// CaptureRequest parameters a single acquisition.
type CaptureRequest struct {
	DeviceID  string
	SessionID string
	Modality  string // fingerprint | face | iris
	Finger    string // e.g. right_thumb (fingerprint only)
	TimeoutMs int
}

// CaptureResult is a completed, quality-scored acquisition.
type CaptureResult struct {
	SessionID    string    `json:"session_id"`
	Modality     string    `json:"modality"`
	Template     []byte    `json:"-"`             // ISO/IEC 19794 template bytes
	TemplateKind string    `json:"template_kind"` // e.g. ISO-19794-2
	Quality      float64   `json:"quality"`       // 0..1
	NFIQ2        int       `json:"nfiq2"`         // 0..100 (fingerprint)
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	DPI          int       `json:"dpi"`
	CapturedAt   time.Time `json:"captured_at"`
}

// CaptureDriver is the contract every device backend implements.
type CaptureDriver interface {
	Name() string
	Enumerate() ([]DeviceInfo, error)
	// Capture blocks until an acquisition completes, the timeout elapses, or an
	// error occurs. Implementations must return quality-scored template bytes.
	Capture(req CaptureRequest) (*CaptureResult, error)
	Close() error
}

// NewCaptureDriver selects a driver from the environment.
func NewCaptureDriver() (CaptureDriver, error) {
	switch os.Getenv("BIOMETRIC_DRIVER") {
	case "", "spool":
		dir := envOrDefaultDriver("BIOMETRIC_SPOOL_DIR", "/var/lib/inec/bvas-spool")
		return NewSpoolDriver(dir)
	case "none":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown BIOMETRIC_DRIVER %q", os.Getenv("BIOMETRIC_DRIVER"))
	}
}

func envOrDefaultDriver(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// --- Spool reference driver -------------------------------------------------

type SpoolDriver struct {
	dir string
}

func NewSpoolDriver(dir string) (*SpoolDriver, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("spool dir %q: %w", dir, err)
	}
	return &SpoolDriver{dir: dir}, nil
}

func (d *SpoolDriver) Name() string { return "spool" }

// Enumerate treats each subdirectory of the spool as a device the vendor agent
// registered by writing a device.json descriptor.
func (d *SpoolDriver) Enumerate() ([]DeviceInfo, error) {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return nil, err
	}
	var devices []DeviceInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		descriptor := filepath.Join(d.dir, e.Name(), "device.json")
		raw, rerr := os.ReadFile(descriptor)
		if rerr != nil {
			continue
		}
		var info DeviceInfo
		if json.Unmarshal(raw, &info) == nil {
			if info.DeviceID == "" {
				info.DeviceID = e.Name()
			}
			devices = append(devices, info)
		}
	}
	return devices, nil
}

type spoolMeta struct {
	Quality      float64 `json:"quality"`
	NFIQ2        int     `json:"nfiq2"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	DPI          int     `json:"dpi"`
	TemplateKind string  `json:"template_kind"`
}

// Capture polls for the vendor agent to drop <session>.tmpl + <session>.json.
func (d *SpoolDriver) Capture(req CaptureRequest) (*CaptureResult, error) {
	if req.SessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}
	// Guard against path traversal: the session ID becomes a filename in the
	// spool dir, so it must be a single path segment with no separators.
	if req.SessionID != filepath.Base(req.SessionID) ||
		strings.ContainsAny(req.SessionID, `/\`) || strings.Contains(req.SessionID, "..") {
		return nil, fmt.Errorf("invalid session_id %q", req.SessionID)
	}
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tmplPath := filepath.Join(d.dir, req.SessionID+".tmpl")
	metaPath := filepath.Join(d.dir, req.SessionID+".json")

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fileExists(tmplPath) && fileExists(metaPath) {
			tmpl, err := os.ReadFile(tmplPath)
			if err != nil {
				return nil, err
			}
			metaRaw, err := os.ReadFile(metaPath)
			if err != nil {
				return nil, err
			}
			var m spoolMeta
			if err := json.Unmarshal(metaRaw, &m); err != nil {
				return nil, fmt.Errorf("bad capture metadata: %w", err)
			}
			if len(tmpl) == 0 {
				return nil, fmt.Errorf("empty template for session %s", req.SessionID)
			}
			kind := m.TemplateKind
			if kind == "" {
				kind = "ISO-19794-2"
			}
			// Consume the spool files so they aren't re-read.
			_ = os.Remove(tmplPath)
			_ = os.Remove(metaPath)
			return &CaptureResult{
				SessionID:    req.SessionID,
				Modality:     req.Modality,
				Template:     tmpl,
				TemplateKind: kind,
				Quality:      m.Quality,
				NFIQ2:        m.NFIQ2,
				Width:        m.Width,
				Height:       m.Height,
				DPI:          m.DPI,
				CapturedAt:   time.Now().UTC(),
			}, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("capture timed out after %s waiting for session %s", timeout, req.SessionID)
}

func (d *SpoolDriver) Close() error { return nil }

// --- Registry integration ---------------------------------------------------

// CaptureFromDevice runs a real, driver-backed acquisition end-to-end: it opens
// a capture session, blocks on the device driver for a quality-scored template,
// then persists the completion. Returns the template bytes for enrolment/match.
func (r *BVASDeviceRegistry) CaptureFromDevice(deviceID, voterVIN, modality, finger string, timeoutMs int) (*CaptureSession, *CaptureResult, error) {
	if r.driver == nil {
		return nil, nil, fmt.Errorf("no capture driver configured")
	}
	session, err := r.StartCapture(deviceID, voterVIN, modality)
	if err != nil {
		return nil, nil, err
	}
	res, err := r.driver.Capture(CaptureRequest{
		DeviceID:  deviceID,
		SessionID: session.SessionID,
		Modality:  modality,
		Finger:    finger,
		TimeoutMs: timeoutMs,
	})
	if err != nil {
		// Record the failed attempt so the session isn't left dangling.
		_ = r.store.CompleteCaptureSession(context.Background(), session.SessionID,
			0, 0, 0, 0, "error", "device_capture_failed")
		return session, nil, err
	}
	if cerr := r.CompleteCapture(session.SessionID, res.Quality, res.NFIQ2, res.Width, res.Height); cerr != nil {
		return session, res, cerr
	}
	return session, res, nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Size() > 0
}
