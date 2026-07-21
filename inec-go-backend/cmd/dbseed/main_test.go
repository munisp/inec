package main

import (
	"path/filepath"
	"testing"
)

func fixtureRoot() string {
	return filepath.Join("..", "..", "..", "fixtures", "db")
}

func TestBundledBaselineFixtureMatchesManifest(t *testing.T) {
	m, err := loadManifest(filepath.Join(fixtureRoot(), "table_manifest.json"))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	f, _, err := loadFixture(filepath.Join(fixtureRoot(), "baseline.json"))
	if err != nil {
		t.Fatalf("load baseline fixture: %v", err)
	}
	if f.Profile != "baseline" {
		t.Fatalf("profile = %q, want baseline", f.Profile)
	}
	policies := map[string]tablePolicy{}
	for _, policy := range m.Tables {
		policies[policy.Table] = policy
	}
	for _, table := range f.Tables {
		policy, ok := policies[table.Table]
		if !ok {
			t.Fatalf("fixture table %q has no policy", table.Table)
		}
		if policy.Mode != "fixture" {
			t.Fatalf("fixture table %q policy = %q, want fixture", table.Table, policy.Mode)
		}
		if len(table.Rows) == 0 {
			t.Fatalf("fixture table %q is empty", table.Table)
		}
	}
}

func TestIdentifierAndProductionDSNSafety(t *testing.T) {
	for _, value := range []string{"states", "result_party_scores", "v1"} {
		if !validIdentifier(value) {
			t.Errorf("validIdentifier(%q) = false", value)
		}
	}
	for _, value := range []string{"states;DROP", "public.states", "1states", "bad-name", ""} {
		if validIdentifier(value) {
			t.Errorf("validIdentifier(%q) = true", value)
		}
	}
	for _, dsn := range []string{
		"postgres://app:pw@prod-db.internal/inec?sslmode=require",
		"postgres://app:pw@db.internal/inec_production?sslmode=require",
	} {
		if !likelyProductionDSN(dsn) {
			t.Errorf("likelyProductionDSN(%q) = false", dsn)
		}
	}
	if likelyProductionDSN("postgres://app:pw@localhost/inec_test?sslmode=disable") {
		t.Error("test DSN was rejected")
	}
}

func TestQuoteIdentifier(t *testing.T) {
	if got, want := quoteIdentifier("result_party_scores"), `"result_party_scores"`; got != want {
		t.Fatalf("quoteIdentifier() = %q, want %q", got, want)
	}
}
