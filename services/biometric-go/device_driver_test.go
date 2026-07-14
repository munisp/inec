package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSpoolDriverCaptureRoundtrip(t *testing.T) {
	dir := t.TempDir()
	drv, err := NewSpoolDriver(dir)
	if err != nil {
		t.Fatalf("NewSpoolDriver: %v", err)
	}
	defer drv.Close()

	session := "cap-test-001"
	// Simulate the vendor capture agent dropping a completed acquisition while
	// Capture is blocking.
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.WriteFile(filepath.Join(dir, session+".tmpl"), []byte("ISO19794-2-TEMPLATE-BYTES"), 0o640)
		meta, _ := json.Marshal(spoolMeta{
			Quality: 0.91, NFIQ2: 62, Width: 512, Height: 512, DPI: 500, TemplateKind: "ISO-19794-2",
		})
		os.WriteFile(filepath.Join(dir, session+".json"), meta, 0o640)
	}()

	res, err := drv.Capture(CaptureRequest{
		SessionID: session, Modality: "fingerprint", Finger: "right_thumb", TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(res.Template) == 0 {
		t.Fatal("expected non-empty template bytes")
	}
	if res.Quality != 0.91 || res.NFIQ2 != 62 || res.DPI != 500 {
		t.Fatalf("metadata not parsed: %+v", res)
	}
	if res.TemplateKind != "ISO-19794-2" {
		t.Fatalf("unexpected template kind: %s", res.TemplateKind)
	}
	// Spool files must be consumed after a successful read.
	if fileExists(filepath.Join(dir, session+".tmpl")) {
		t.Fatal("template file was not consumed")
	}
}

func TestSpoolDriverCaptureTimeout(t *testing.T) {
	dir := t.TempDir()
	drv, _ := NewSpoolDriver(dir)
	defer drv.Close()

	_, err := drv.Capture(CaptureRequest{SessionID: "never", Modality: "fingerprint", TimeoutMs: 400})
	if err == nil {
		t.Fatal("expected timeout error when no capture is delivered")
	}
}

func TestSpoolDriverEnumerate(t *testing.T) {
	dir := t.TempDir()
	devDir := filepath.Join(dir, "BVAS-001")
	os.MkdirAll(devDir, 0o750)
	info, _ := json.Marshal(DeviceInfo{
		DeviceID: "BVAS-001", Model: "DERMALOG ZF1", Modalities: []string{"fingerprint", "face"},
		FAPLevel: "FAP30", NFCCapable: true,
	})
	os.WriteFile(filepath.Join(devDir, "device.json"), info, 0o640)

	drv, _ := NewSpoolDriver(dir)
	devs, err := drv.Enumerate()
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}
	if len(devs) != 1 || devs[0].DeviceID != "BVAS-001" || !devs[0].NFCCapable {
		t.Fatalf("unexpected enumerate result: %+v", devs)
	}
}
