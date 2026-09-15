package main

// R5-010: client door-knock timestamps must be validated (±72h/10min) and
// never silently trusted.

import (
	"testing"
	"time"
)

func TestValidateKnockTimestamp(t *testing.T) {
	now := time.Now().UTC()

	// Absent timestamp → nil (server NOW() applies), no error.
	if v, err := validateKnockTimestamp(""); err != nil || v != nil {
		t.Fatalf("empty timestamp: got %v, %v", v, err)
	}

	// Recent valid timestamps accepted.
	for _, raw := range []string{
		now.Add(-1 * time.Hour).Format(time.RFC3339),
		now.Add(-71 * time.Hour).Format(time.RFC3339Nano),
		now.Add(5 * time.Minute).Format(time.RFC3339),
		now.Add(-2 * time.Hour).Format("2006-01-02 15:04:05"),
	} {
		if _, err := validateKnockTimestamp(raw); err != nil {
			t.Fatalf("valid timestamp %q rejected: %v", raw, err)
		}
	}

	// Out-of-window and garbage timestamps rejected.
	for _, raw := range []string{
		now.Add(-73 * time.Hour).Format(time.RFC3339),  // too old
		now.Add(11 * time.Minute).Format(time.RFC3339), // too far in future
		now.Add(24 * time.Hour).Format(time.RFC3339),   // forged future
		"not-a-timestamp",
		"2020-13-45T99:99:99Z",
	} {
		if _, err := validateKnockTimestamp(raw); err == nil {
			t.Fatalf("invalid timestamp %q accepted", raw)
		}
	}

	// Normalization: accepted timestamps come back in UTC RFC3339.
	v, err := validateKnockTimestamp(now.Add(-30 * time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if _, perr := time.Parse(time.RFC3339, v.(string)); perr != nil {
		t.Fatalf("normalized value not RFC3339: %v", v)
	}
}
