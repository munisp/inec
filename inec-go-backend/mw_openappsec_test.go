package main

import (
	"context"
	"testing"
)

func TestEmbeddedWAFBlocksXSSInDecodedQuery(t *testing.T) {
	waf := newEmbeddedWAF()
	decision, err := waf.InspectRequest(context.Background(), WAFRequest{
		SourceIP: "127.0.0.1",
		Method:   "GET",
		Path:     "/elections?search=<script>alert(1)</script>",
	})
	if err != nil {
		t.Fatalf("inspect request: %v", err)
	}
	if decision.Action != "block" || decision.ThreatLevel != "high" {
		t.Fatalf("XSS decision = %#v, want high-severity block", decision)
	}
	if decision.Score < 40 {
		t.Fatalf("XSS score = %d, want at least blocking threshold", decision.Score)
	}
}
